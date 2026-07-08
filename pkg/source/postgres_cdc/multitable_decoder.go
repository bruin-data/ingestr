package postgres_cdc

import (
	"encoding/binary"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

// TableChange represents a decoded change for a specific table.
type TableChange struct {
	TableName string
	Change    Change
}

// MultiTableDecoder decodes pgoutput messages for multiple tables.
type MultiTableDecoder struct {
	tableSchemas   map[string]*schema.TableSchema // schema name.table name -> schema
	relations      map[uint32]*RelationInfo
	targetRelIDs   map[uint32]string // relation ID -> full table name
	pendingChanges []TableChange
	currentTxLSN   pglogrepl.LSN
}

func NewMultiTableDecoder(tables []source.SourceTableInfo) *MultiTableDecoder {
	tableSchemas := make(map[string]*schema.TableSchema)
	for _, table := range tables {
		tableSchemas[table.Name] = table.Schema
	}

	return &MultiTableDecoder{
		tableSchemas: tableSchemas,
		relations:    make(map[uint32]*RelationInfo),
		targetRelIDs: make(map[uint32]string),
	}
}

// DecodedChanges carries one committed transaction's decoded changes for a
// single table. Changes stay plain Go values; Arrow materialization happens
// once per flush window in the accumulator, not per transaction.
type DecodedChanges struct {
	TableName string
	Changes   []Change
	LSN       pglogrepl.LSN
}

// Decode decodes a WAL message and, on commit, returns the transaction's
// changes grouped per target table.
func (d *MultiTableDecoder) Decode(data []byte, lsn pglogrepl.LSN) ([]DecodedChanges, error) {
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

func (d *MultiTableDecoder) handleRelation(data []byte) error {
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

	// Check if this is one of our target tables
	fullName := fmt.Sprintf("%s.%s", namespace, name)
	if _, ok := d.tableSchemas[fullName]; ok {
		d.targetRelIDs[relID] = fullName
		config.Debug("[CDC] Found target relation: %s (ID: %d)", fullName, relID)
	} else {
		// Also try without schema prefix for public schema
		if namespace == "public" {
			if _, ok := d.tableSchemas[name]; ok {
				d.targetRelIDs[relID] = name
				config.Debug("[CDC] Found target relation: %s (ID: %d)", name, relID)
			}
		}
	}

	return nil
}

// handleBegin stamps the transaction with the commit ("final") LSN carried in
// the Begin payload, NOT the Begin record's WAL position. The walsender
// delivers transactions in commit order, but under concurrent writers their
// Begin positions interleave arbitrarily: a transaction that began earlier can
// commit — and be delivered — after one that began later. A begin-position
// stamp is therefore non-monotonic across delivered transactions, and the
// per-table LSN filter (ShouldFilterChange) would treat such a late-committing
// transaction as already processed and silently drop it. Commit LSNs are
// strictly increasing in delivery order, which is exactly what the filter,
// resume state, and slot-confirmation low-water logic require.
func (d *MultiTableDecoder) handleBegin(data []byte) error {
	d.pendingChanges = nil
	if len(data) < 8 {
		return fmt.Errorf("begin message too short")
	}
	d.currentTxLSN = pglogrepl.LSN(binary.BigEndian.Uint64(data[:8]))
	return nil
}

// InFlightTxLSN returns the LSN of a transaction whose changes have been
// decoded but not yet emitted (BEGIN seen, COMMIT not yet processed). The bool
// is false when no transaction is mid-flight.
func (d *MultiTableDecoder) InFlightTxLSN() (pglogrepl.LSN, bool) {
	if len(d.pendingChanges) == 0 {
		return 0, false
	}
	return d.currentTxLSN, true
}

func (d *MultiTableDecoder) handleCommit() ([]DecodedChanges, error) {
	if len(d.pendingChanges) == 0 {
		return nil, nil
	}

	// Group changes by table, preserving arrival order within each table.
	// Unchanged-TOAST fill runs later over the whole flush window (see
	// batchAccumulator.flushTable), which subsumes the per-commit pass.
	var groups []DecodedChanges
	groupIdx := make(map[string]int)
	for _, tc := range d.pendingChanges {
		if d.tableSchemas[tc.TableName] == nil {
			continue
		}
		idx, ok := groupIdx[tc.TableName]
		if !ok {
			idx = len(groups)
			groupIdx[tc.TableName] = idx
			groups = append(groups, DecodedChanges{TableName: tc.TableName, LSN: d.currentTxLSN})
		}
		groups[idx].Changes = append(groups[idx].Changes, tc.Change)
	}

	d.pendingChanges = nil
	return groups, nil
}

func (d *MultiTableDecoder) handleInsert(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("insert message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	// Skip if not a target table
	tableName, ok := d.targetRelIDs[relID]
	if !ok {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	tableSchema := d.tableSchemas[tableName]
	if tableSchema == nil {
		return nil
	}

	// Skip 'N' marker for new tuple
	if len(data) < 1 || data[0] != 'N' {
		return fmt.Errorf("expected 'N' marker in insert message")
	}
	data = data[1:]

	values, err := d.parseTupleData(data, rel, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "INSERT",
			LSN:       d.currentTxLSN,
			Values:    values,
		},
	})

	return nil
}

func (d *MultiTableDecoder) handleUpdate(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("update message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	tableName, ok := d.targetRelIDs[relID]
	if !ok {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	tableSchema := d.tableSchemas[tableName]
	if tableSchema == nil {
		return nil
	}

	var oldValues []interface{}

	// Check for key ('K') or old tuple ('O') marker
	if len(data) > 0 && (data[0] == 'K' || data[0] == 'O') {
		data = data[1:]
		var err error
		oldValues, err = d.parseTupleData(data, rel, tableSchema)
		if err != nil {
			return fmt.Errorf("failed to parse old tuple: %w", err)
		}
		data = skipTupleData(data)
	}

	// New tuple marker
	if len(data) < 1 || data[0] != 'N' {
		return fmt.Errorf("expected 'N' marker in update message")
	}
	data = data[1:]

	values, err := d.parseTupleData(data, rel, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to parse new tuple: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "UPDATE",
			LSN:       d.currentTxLSN,
			Values:    values,
			OldValues: oldValues,
		},
	})

	return nil
}

func (d *MultiTableDecoder) handleDelete(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("delete message too short")
	}

	relID := binary.BigEndian.Uint32(data[:4])
	data = data[4:]

	tableName, ok := d.targetRelIDs[relID]
	if !ok {
		return nil
	}

	rel := d.relations[relID]
	if rel == nil {
		return fmt.Errorf("unknown relation ID: %d", relID)
	}

	tableSchema := d.tableSchemas[tableName]
	if tableSchema == nil {
		return nil
	}

	// Key ('K') or old tuple ('O') marker
	if len(data) < 1 || (data[0] != 'K' && data[0] != 'O') {
		return fmt.Errorf("expected 'K' or 'O' marker in delete message")
	}
	data = data[1:]

	values, err := d.parseTupleData(data, rel, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.pendingChanges = append(d.pendingChanges, TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "DELETE",
			LSN:       d.currentTxLSN,
			Values:    values,
		},
	})

	return nil
}

func (d *MultiTableDecoder) parseTupleData(data []byte, rel *RelationInfo, tableSchema *schema.TableSchema) ([]interface{}, error) {
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
			if int(i) < sourceColumnCount(tableSchema) {
				col := tableSchema.Columns[i]
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
