package postgres_cdc

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
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
)

// Tuple data format
const (
	tupleDataNull      = 'n'
	tupleDataUnchanged = 'u'
	tupleDataText      = 't'
	tupleDataBinary    = 'b'
)

type RelationInfo struct {
	RelationID uint32
	Namespace  string
	Name       string
	Columns    []RelationColumn
}

type RelationColumn struct {
	Flags    uint8
	Name     string
	DataType uint32
	TypeMod  int32
}

type Change struct {
	Operation string // "INSERT", "UPDATE", "DELETE"
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
}

func NewDecoder(tableSchema *schema.TableSchema, schemaName, tableName string) *Decoder {
	return &Decoder{
		tableSchema:  tableSchema,
		targetSchema: schemaName,
		targetTable:  tableName,
		relations:    make(map[uint32]*RelationInfo),
	}
}

// Decode decodes a WAL message and, on commit, returns the transaction's
// decoded changes (filled and compacted). Arrow materialization happens once
// per flush window in the accumulator, not per transaction.
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
		config.Debug("[CDC] Ignoring TRUNCATE message")
		return nil, nil
	case msgTypeOrigin:
		return nil, nil
	case msgTypeType:
		return nil, nil
	default:
		config.Debug("[CDC] Unknown message type: %c", msgType)
		return nil, nil
	}
}

func (d *Decoder) handleRelation(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("relation message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	namespace, n := readString(data)
	data = data[n:]

	name, n := readString(data)
	data = data[n:]

	// Skip replica identity
	if len(data) < 1 {
		return fmt.Errorf("relation message missing replica identity")
	}
	data = data[1:]

	// Number of columns
	if len(data) < 2 {
		return fmt.Errorf("relation message missing column count")
	}
	numCols := binary.BigEndian.Uint16(data[:2])
	data = data[2:]

	columns := make([]RelationColumn, numCols)
	for i := uint16(0); i < numCols; i++ {
		if len(data) < 1 {
			return fmt.Errorf("relation message column flags truncated")
		}
		flags := data[0]
		data = data[1:]

		colName, n := readString(data)
		data = data[n:]

		if len(data) < 4 {
			return fmt.Errorf("relation message column type truncated")
		}
		dataType := binary.BigEndian.Uint32(data[:4])
		data = data[4:]

		if len(data) < 4 {
			return fmt.Errorf("relation message column typemod truncated")
		}
		typeMod := int32(binary.BigEndian.Uint32(data[:4]))
		data = data[4:]

		columns[i] = RelationColumn{
			Flags:    flags,
			Name:     colName,
			DataType: dataType,
			TypeMod:  typeMod,
		}
	}

	rel := &RelationInfo{
		RelationID: relID,
		Namespace:  namespace,
		Name:       name,
		Columns:    columns,
	}

	d.relations[relID] = rel

	// Check if this is our target table
	if namespace == d.targetSchema && name == d.targetTable {
		d.targetRelID = relID
		config.Debug("[CDC] Found target relation: %s.%s (ID: %d)", namespace, name, relID)
	}

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

func (d *Decoder) handleCommit() ([]Change, error) {
	if len(d.pendingChanges) == 0 {
		return nil, nil
	}

	d.applyIntraBatchFill()
	d.compactPendingChanges()

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

	values, err := d.parseTupleData(data, rel)
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
		oldValues, err = d.parseTupleData(data, rel)
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

	values, err := d.parseTupleData(data, rel)
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

	values, err := d.parseTupleData(data, rel)
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

func (d *Decoder) applyIntraBatchFill() {
	applyIntraBatchFill(d.pendingChanges, d.tableSchema)
}

func (d *Decoder) compactPendingChanges() {
	if len(d.pendingChanges) < 2 {
		return
	}

	if len(d.tableSchema.PrimaryKeys) == 0 {
		return
	}

	pkIndices := make([]int, len(d.tableSchema.PrimaryKeys))
	for i, pk := range d.tableSchema.PrimaryKeys {
		idx := -1
		for colIdx, col := range d.tableSchema.Columns {
			if col.Name == pk {
				idx = colIdx
				break
			}
		}
		if idx < 0 {
			return
		}
		pkIndices[i] = idx
	}

	type entry struct {
		change Change
		index  int
	}

	latestNonDeleted := make(map[string]entry)
	latestDeleted := make(map[string]entry)

	for i, change := range d.pendingChanges {
		key := d.pkKey(change, pkIndices, i)
		if change.Operation == "DELETE" {
			latestDeleted[key] = entry{change: change, index: i}
		} else {
			latestNonDeleted[key] = entry{change: change, index: i}
		}
	}

	combined := make([]entry, 0, len(latestNonDeleted)+len(latestDeleted))
	for _, e := range latestNonDeleted {
		combined = append(combined, e)
	}
	for _, e := range latestDeleted {
		combined = append(combined, e)
	}

	sort.Slice(combined, func(i, j int) bool {
		return combined[i].index < combined[j].index
	})

	d.pendingChanges = make([]Change, len(combined))
	for i, e := range combined {
		d.pendingChanges[i] = e.change
	}
}

func (d *Decoder) pkKey(change Change, pkIndices []int, changeIndex int) string {
	return pkKeyFromRow(change.Values, change.OldValues, pkIndices, changeIndex)
}

func (d *Decoder) parseTupleData(data []byte, rel *RelationInfo) ([]interface{}, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("tuple data too short")
	}

	numCols := binary.BigEndian.Uint16(data[:2])
	data = data[2:]

	values := make([]interface{}, numCols)

	for i := uint16(0); i < numCols; i++ {
		if len(data) < 1 {
			return nil, fmt.Errorf("tuple data truncated at column %d", i)
		}

		colType := data[0]
		data = data[1:]

		switch colType {
		case tupleDataNull:
			values[i] = nil
		case tupleDataUnchanged:
			values[i] = tupleUnchangedMarker
		case tupleDataText:
			if len(data) < 4 {
				return nil, fmt.Errorf("text length truncated")
			}
			length := binary.BigEndian.Uint32(data[:4])
			data = data[4:]

			if len(data) < int(length) {
				return nil, fmt.Errorf("text data truncated")
			}
			textVal := string(data[:length])
			data = data[length:]

			// Convert text to appropriate type based on schema column
			if int(i) < sourceColumnCount(d.tableSchema) {
				col := d.tableSchema.Columns[i]
				values[i] = convertTextValue(textVal, col)
			} else {
				values[i] = textVal
			}
		case tupleDataBinary:
			if len(data) < 4 {
				return nil, fmt.Errorf("binary length truncated")
			}
			length := binary.BigEndian.Uint32(data[:4])
			data = data[4:]

			if len(data) < int(length) {
				return nil, fmt.Errorf("binary data truncated")
			}
			// Copy: decoded changes are buffered across the flush window and
			// must not alias the WAL message buffer.
			values[i] = append([]byte(nil), data[:length]...)
			data = data[length:]
		default:
			return nil, fmt.Errorf("unknown tuple data type: %c", colType)
		}
	}

	return values, nil
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
