package postgres_cdc

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgtype"
)

// pgoutput message types
const (
	msgTypeRelation = 'R'
	msgTypeBegin    = 'B'
	msgTypeCommit   = 'C'
	msgTypeInsert   = 'I'
	msgTypeUpdate   = 'U'
	msgTypeDelete   = 'D'
	msgTypeTruncate = 'T'
	msgTypeOrigin   = 'O'
	msgTypeType     = 'Y'

	// Protocol v2 streaming of large in-progress transactions.
	msgTypeStreamStart  = 'S'
	msgTypeStreamStop   = 'E'
	msgTypeStreamCommit = 'c'
	msgTypeStreamAbort  = 'A'
)

// Tuple data format
const (
	tupleDataNull      = 'n'
	tupleDataUnchanged = 'u'
	tupleDataText      = 't'
	tupleDataBinary    = 'b'
)

type Change struct {
	Operation string // "INSERT", "UPDATE", "DELETE", "TRUNCATE"
	LSN       pglogrepl.LSN
	Values    []interface{}
	OldValues []interface{} // For UPDATE/DELETE with replica identity
}

type Decoder struct {
	tableSchema    *schema.TableSchema
	targetSchema   string
	targetTable    string
	relations      map[uint32]*RelationInfo
	targetRelID    uint32
	pendingChanges []Change
	currentTxLSN   pglogrepl.LSN
	typeMap        *pgtype.Map
}

func NewDecoder(tableSchema *schema.TableSchema, schemaName, tableName string) *Decoder {
	return &Decoder{
		tableSchema:  tableSchema,
		targetSchema: schemaName,
		targetTable:  tableName,
		relations:    make(map[uint32]*RelationInfo),
		typeMap:      pgtype.NewMap(),
	}
}

// Decode decodes a WAL message and, on commit, returns the transaction's
// decoded changes. Unchanged-TOAST fill, per-key compaction, and Arrow
// materialization all happen once per flush window in the accumulator, not
// per transaction.
func (d *Decoder) Decode(data []byte, lsn pglogrepl.LSN) ([]Change, error) {
	if len(data) == 0 {
		return nil, nil
	}

	msgType := data[0]
	data = data[1:]

	switch msgType {
	case msgTypeRelation:
		return nil, d.handleRelation(data)
	case msgTypeBegin:
		return nil, d.handleBegin(data)
	case msgTypeCommit:
		return d.handleCommit()
	case msgTypeInsert:
		return nil, d.handleInsert(data)
	case msgTypeUpdate:
		return nil, d.handleUpdate(data)
	case msgTypeDelete:
		return nil, d.handleDelete(data)
	case msgTypeTruncate:
		return nil, d.handleTruncate(data)
	case msgTypeOrigin:
		return nil, nil
	case msgTypeType:
		return nil, nil
	default:
		config.Debug("[CDC] Unknown message type: %c", msgType)
		return nil, nil
	}
}

func (d *Decoder) handleTruncate(data []byte) error {
	relationIDs, err := parseTruncateRelationIDs(data)
	if err != nil {
		return err
	}
	for _, relID := range relationIDs {
		if relID == d.targetRelID {
			d.pendingChanges = append(d.pendingChanges, Change{Operation: "TRUNCATE", LSN: d.currentTxLSN})
			break
		}
	}
	return nil
}

func parseTruncateRelationIDs(data []byte) ([]uint32, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("truncate message too short")
	}
	count := int(binary.BigEndian.Uint32(data[:4]))
	data = data[5:] // relation count followed by cascade/restart-identity flags
	if count < 0 || len(data) < count*4 {
		return nil, fmt.Errorf("truncate message contains %d relations but only %d bytes", count, len(data))
	}
	relationIDs := make([]uint32, count)
	for i := range relationIDs {
		relationIDs[i] = binary.BigEndian.Uint32(data[i*4 : i*4+4])
	}
	return relationIDs, nil
}

func (d *Decoder) handleRelation(data []byte) error {
	rel, err := parseRelationMessage(data)
	if err != nil {
		return err
	}

	isTarget := rel.Namespace == d.targetSchema && rel.Name == d.targetTable
	if isTarget {
		d.targetRelID = rel.RelationID
		config.Debug("[CDC] Found target relation: %s.%s (ID: %d)", rel.Namespace, rel.Name, rel.RelationID)
	} else if d.targetRelID != 0 && rel.RelationID == d.targetRelID {
		// Table renamed mid-stream; keep decoding it by relation ID.
		isTarget = true
	}
	if isTarget {
		prev := d.relations[rel.RelationID]
		if err := mapRelationToSchema(rel, prev, d.tableSchema, d.targetSchema+"."+d.targetTable); err != nil {
			// Do not store rel on error: a rebuilt stream must retry against the
			// last accepted relation so schema-change detection remains stable.
			return err
		}
	}

	d.relations[rel.RelationID] = rel
	return nil
}

// handleBegin stamps the transaction with the commit ("final") LSN from the
// Begin payload; see MultiTableDecoder.handleBegin for why the Begin record's
// own WAL position must not be used.
func (d *Decoder) handleBegin(data []byte) error {
	d.pendingChanges = nil
	if len(data) < 8 {
		return fmt.Errorf("begin message too short")
	}
	d.currentTxLSN = pglogrepl.LSN(binary.BigEndian.Uint64(data[:8]))
	return nil
}

// CurrentTxLSN returns the LSN of the transaction currently being decoded. It
// remains valid after a COMMIT until the next BEGIN, so callers can read the
// LSN of the batch just returned by Decode.
func (d *Decoder) CurrentTxLSN() pglogrepl.LSN {
	return d.currentTxLSN
}

// InFlightTxLSN returns the LSN of a transaction whose changes have been
// decoded but not yet emitted (BEGIN seen, COMMIT not yet processed). The bool
// is false when no transaction is mid-flight.
func (d *Decoder) InFlightTxLSN() (pglogrepl.LSN, bool) {
	if len(d.pendingChanges) == 0 {
		return 0, false
	}
	return d.currentTxLSN, true
}

// handleCommit hands the transaction's raw changes to the caller.
// Unchanged-TOAST fill and per-key compaction run once over the accumulator's
// whole flush window (batchAccumulator.flushTable), which subsumes the
// per-commit passes.
func (d *Decoder) handleCommit() ([]Change, error) {
	if len(d.pendingChanges) == 0 {
		return nil, nil
	}

	changes := d.pendingChanges
	d.pendingChanges = nil
	return changes, nil
}

func (d *Decoder) handleInsert(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("insert message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	// Skip if not our target table
	if relID != d.targetRelID {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	// Skip 'N' marker for new tuple
	if len(data) < 1 || data[0] != 'N' {
		return fmt.Errorf("expected 'N' marker in insert message")
	}
	data = data[1:]

	values, err := parseTupleData(data, rel, d.tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, Change{
		Operation: "INSERT",
		LSN:       d.currentTxLSN,
		Values:    values,
	})

	return nil
}

func (d *Decoder) handleUpdate(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("update message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	if relID != d.targetRelID {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	var oldValues []interface{}

	// Check for key ('K') or old tuple ('O') marker
	if len(data) > 0 && (data[0] == 'K' || data[0] == 'O') {
		data = data[1:]
		var err error
		oldValues, err = parseTupleData(data, rel, d.tableSchema, d.typeMap)
		if err != nil {
			return fmt.Errorf("failed to parse old tuple: %w", err)
		}
		// Skip past the tuple data
		data = skipTupleData(data)
	}

	// New tuple marker
	if len(data) < 1 || data[0] != 'N' {
		return fmt.Errorf("expected 'N' marker in update message")
	}
	data = data[1:]

	values, err := parseTupleData(data, rel, d.tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse new tuple: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, Change{
		Operation: "UPDATE",
		LSN:       d.currentTxLSN,
		Values:    values,
		OldValues: oldValues,
	})

	return nil
}

func (d *Decoder) handleDelete(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("delete message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	if relID != d.targetRelID {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	// Key ('K') or old tuple ('O') marker
	if len(data) < 1 || (data[0] != 'K' && data[0] != 'O') {
		return fmt.Errorf("expected 'K' or 'O' marker in delete message")
	}
	data = data[1:]

	values, err := parseTupleData(data, rel, d.tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, Change{
		Operation: "DELETE",
		LSN:       d.currentTxLSN,
		Values:    values,
	})

	return nil
}

func readString(data []byte) (string, int) {
	for i, b := range data {
		if b == 0 {
			return string(data[:i]), i + 1
		}
	}
	return string(data), len(data)
}

func skipTupleData(data []byte) []byte {
	if len(data) < 2 {
		return data
	}

	numCols := binary.BigEndian.Uint16(data[:2])
	data = data[2:]

	for i := uint16(0); i < numCols; i++ {
		if len(data) < 1 {
			return data
		}

		colType := data[0]
		data = data[1:]

		switch colType {
		case tupleDataNull, tupleDataUnchanged:
			// No additional data
		case tupleDataText, tupleDataBinary:
			if len(data) < 4 {
				return data
			}
			length := binary.BigEndian.Uint32(data[:4])
			data = data[4:]
			if len(data) < int(length) {
				return data
			}
			data = data[length:]
		}
	}

	return data
}

func convertTextValue(text string, col schema.Column) interface{} {
	switch col.DataType {
	case schema.TypeBoolean:
		return text == "t" || text == "true" || text == "1"
	case schema.TypeInt16:
		if v, err := strconv.ParseInt(text, 10, 16); err == nil {
			return int16(v)
		}
		return nil
	case schema.TypeInt32:
		if v, err := strconv.ParseInt(text, 10, 32); err == nil {
			return int32(v)
		}
		return nil
	case schema.TypeInt64:
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			return v
		}
		return nil
	case schema.TypeFloat32:
		if v, err := strconv.ParseFloat(text, 32); err == nil {
			return float32(v)
		}
		return nil
	case schema.TypeFloat64:
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			return v
		}
		return nil
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		// PostgreSQL timestamp format
		formats := []string{
			"2006-01-02 15:04:05.999999-07",
			"2006-01-02 15:04:05.999999+00",
			"2006-01-02 15:04:05.999999",
			"2006-01-02 15:04:05-07",
			"2006-01-02 15:04:05+00",
			"2006-01-02 15:04:05",
			time.RFC3339Nano,
			time.RFC3339,
		}
		for _, format := range formats {
			if t, err := time.Parse(format, text); err == nil {
				return t
			}
		}
		return nil
	case schema.TypeDate:
		if t, err := time.Parse("2006-01-02", text); err == nil {
			return t
		}
		return nil
	case schema.TypeTime:
		formats := []string{
			"15:04:05.999999",
			"15:04:05",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, text); err == nil {
				return t
			}
		}
		return nil
	case schema.TypeDecimal:
		return text // Keep as string for decimal handling
	case schema.TypeBinary:
		// bytea arrives as a hex literal ("\x48...") in text mode; decode it so
		// the destination stores the raw bytes, matching the snapshot path and
		// binary-mode decoding.
		if strings.HasPrefix(text, `\x`) {
			if b, err := hex.DecodeString(text[2:]); err == nil {
				return b
			}
		}
		return []byte(text)
	case schema.TypeArray:
		// Logical replication delivers arrays as Postgres array literals
		// ({a,b}), not JSON arrays, so parse the literal and convert each
		// element by the element type. Returning a []interface{} lets the list
		// builder populate the array; the snapshot path produces the same shape
		// via pgx, keeping streaming and snapshot consistent.
		elems, ok := parsePostgresArrayLiteral(text)
		if !ok {
			return nil
		}
		elemCol := schema.Column{DataType: col.ArrayType, Precision: col.Precision, Scale: col.Scale}
		out := make([]interface{}, len(elems))
		for i, e := range elems {
			if e.isNull {
				out[i] = nil
				continue
			}
			out[i] = convertTextValue(e.value, elemCol)
		}
		return out
	default:
		return text
	}
}
