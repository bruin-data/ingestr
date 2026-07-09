package postgres_cdc

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
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
	rel, err := parseRelationMessage(data)
	if err != nil {
		return err
	}

	// Check if this is one of our target tables
	tableName := fmt.Sprintf("%s.%s", rel.Namespace, rel.Name)
	if _, ok := d.tableSchemas[tableName]; !ok {
		tableName = ""
		// Also try without schema prefix for public schema
		if rel.Namespace == "public" {
			if _, ok := d.tableSchemas[rel.Name]; ok {
				tableName = rel.Name
			}
		}
	}
	if tableName == "" {
		// Table renamed mid-stream; keep decoding it by relation ID.
		tableName = d.targetRelIDs[rel.RelationID]
	}

	if tableName != "" {
		d.targetRelIDs[rel.RelationID] = tableName
		config.Debug("[CDC] Found target relation: %s (ID: %d)", tableName, rel.RelationID)
		if tableSchema := d.tableSchemas[tableName]; tableSchema != nil {
			prev := d.relations[rel.RelationID]
			if err := mapRelationToSchema(rel, prev, tableSchema, tableName); err != nil {
				// Do not store rel on error: a rebuilt stream must retry against the
				// last accepted relation so schema-change detection remains stable.
				return err
			}
		}
	}

	d.relations[rel.RelationID] = rel
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

		applyIntraBatchFill(changes, tableSchema)
		changes = expandUpdates(changes, tableSchema)

		batch, err := d.changesToBatch(changes, tableSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert changes for table %s: %w", tableName, err)
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

	values, err := parseTupleData(data, rel, tableSchema)
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
		oldValues, err = parseTupleData(data, rel, tableSchema)
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

	values, err := parseTupleData(data, rel, tableSchema)
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

	values, err := parseTupleData(data, rel, tableSchema)
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
