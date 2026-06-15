package postgres_cdc

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/connredact"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
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

// DecodedBatch contains a decoded batch with its source table name.
type DecodedBatch struct {
	Batch     arrow.RecordBatch
	TableName string
	LSN       pglogrepl.LSN
}

// Decode decodes a WAL message and returns batches for each table that has pending changes.
func (d *MultiTableDecoder) Decode(data []byte, lsn pglogrepl.LSN) ([]DecodedBatch, error) {
	if len(data) == 0 {
		return nil, nil
	}

	msgType := data[0]
	data = data[1:]

	switch msgType {
	case msgTypeRelation:
		return nil, d.handleRelation(data)
	case msgTypeBegin:
		return nil, d.handleBegin(data, lsn)
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

func (d *MultiTableDecoder) handleBegin(data []byte, lsn pglogrepl.LSN) error {
	d.pendingChanges = nil
	d.currentTxLSN = lsn
	return nil
}

func (d *MultiTableDecoder) handleCommit() ([]DecodedBatch, error) {
	if len(d.pendingChanges) == 0 {
		return nil, nil
	}

	// Group changes by table
	changesByTable := make(map[string][]Change)
	for _, tc := range d.pendingChanges {
		changesByTable[tc.TableName] = append(changesByTable[tc.TableName], tc.Change)
	}

	// Create a batch for each table that has changes
	var batches []DecodedBatch
	for tableName, changes := range changesByTable {
		tableSchema := d.tableSchemas[tableName]
		if tableSchema == nil {
			continue
		}

		batch, err := d.changesToBatch(changes, tableSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert changes for table %s: %w", tableName, connredact.Redact("", err))
		}
		if batch != nil {
			batches = append(batches, DecodedBatch{
				Batch:     batch,
				TableName: tableName,
				LSN:       d.currentTxLSN,
			})
		}
	}

	d.pendingChanges = nil
	return batches, nil
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
			values[i] = data[:length]
			data = data[length:]
		default:
			return nil, fmt.Errorf("unknown tuple data type: %c", colType)
		}
	}

	return values, nil
}

func (d *MultiTableDecoder) changesToBatch(changes []Change, tableSchema *schema.TableSchema) (arrow.RecordBatch, error) {
	if len(changes) == 0 {
		return nil, nil
	}

	mem := memory.NewGoAllocator()
	arrowSchema := buildArrowSchema(tableSchema.Columns)

	builders := make([]array.Builder, len(tableSchema.Columns))
	for i, field := range arrowSchema.Fields() {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	syncedAt := time.Now().UTC()
	nSource := sourceColumnCount(tableSchema)

	for i, change := range changes {
		for colIdx := 0; colIdx < nSource; colIdx++ {
			arrowconv.AppendValue(builders[colIdx], resolveColumnValue(change, colIdx))
		}

		builders[nSource].(*array.StringBuilder).Append(FormatLSN(change.LSN))
		builders[nSource+1].(*array.BooleanBuilder).Append(change.Operation == "DELETE")
		perRowSyncedAt := syncedAt.Add(time.Duration(i) * time.Microsecond)
		builders[nSource+2].(*array.TimestampBuilder).Append(arrow.Timestamp(perRowSyncedAt.UnixMicro()))
		builders[nSource+3].(*array.StringBuilder).Append(unchangedColumnsJSON(change, tableSchema.Columns, nSource))
	}

	arrays := make([]arrow.Array, len(builders))
	for i, b := range builders {
		arrays[i] = b.NewArray()
	}

	record := array.NewRecordBatch(arrowSchema, arrays, int64(len(changes)))

	for _, arr := range arrays {
		arr.Release()
	}

	return record, nil
}
