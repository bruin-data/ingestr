package postgres_cdc

import (
	"encoding/binary"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgtype"
)

// TableChange represents a decoded change for a specific table.
type TableChange struct {
	TableName string
	Change    Change
}

// streamedChange is one buffered change of an in-progress streamed
// transaction (protocol v2), tagged with the subtransaction xid that produced
// it so a Stream Abort can discard exactly that subtransaction's changes.
type streamedChange struct {
	xid uint32
	tc  TableChange
}

// MultiTableDecoder decodes pgoutput messages for multiple tables. It
// understands protocol v2 streaming of large in-progress transactions
// (Stream Start/Stop/Commit/Abort) and, when the stream runs with the
// `binary 'true'` option, binary-format tuple data.
type MultiTableDecoder struct {
	tableSchemas   map[string]*schema.TableSchema // schema name.table name -> schema
	relations      map[uint32]*RelationInfo
	targetRelIDs   map[uint32]string // relation ID -> full table name
	pendingChanges []TableChange
	currentTxLSN   pglogrepl.LSN
	typeMap        *pgtype.Map

	// Protocol v2 streaming state. Between Stream Start and Stream Stop,
	// change messages carry a subtransaction xid and are buffered per
	// top-level transaction until its Stream Commit (or dropped on abort).
	inStream    bool
	streamXid   uint32 // top-level xid of the current stream segment
	msgXid      uint32 // xid carried by the message being decoded
	walStart    pglogrepl.LSN
	streamed    map[uint32][]streamedChange
	streamedLow map[uint32]pglogrepl.LSN // lowest WAL position buffered per top-level xid
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
		typeMap:      pgtype.NewMap(),
		streamed:     make(map[uint32][]streamedChange),
		streamedLow:  make(map[uint32]pglogrepl.LSN),
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

// Decode decodes a WAL message and, on a (stream) commit, returns the
// transaction's changes grouped per target table.
func (d *MultiTableDecoder) Decode(data []byte, lsn pglogrepl.LSN) ([]DecodedChanges, error) {
	if len(data) == 0 {
		return nil, nil
	}

	msgType := data[0]
	data = data[1:]
	d.walStart = lsn

	switch msgType {
	case msgTypeStreamStart:
		return nil, d.handleStreamStart(data)
	case msgTypeStreamStop:
		d.inStream = false
		return nil, nil
	case msgTypeStreamCommit:
		return d.handleStreamCommit(data)
	case msgTypeStreamAbort:
		return nil, d.handleStreamAbort(data)
	}

	// Inside a stream segment, change messages carry the subtransaction xid
	// between the type byte and the message body.
	if d.inStream {
		switch msgType {
		case msgTypeRelation, msgTypeType, msgTypeInsert, msgTypeUpdate, msgTypeDelete, msgTypeTruncate:
			if len(data) < 4 {
				return nil, fmt.Errorf("streamed message missing xid")
			}
			d.msgXid = binary.BigEndian.Uint32(data[:4])
			data = data[4:]
		}
	}

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

func (d *MultiTableDecoder) handleTruncate(data []byte) error {
	relationIDs, err := parseTruncateRelationIDs(data)
	if err != nil {
		return err
	}
	for _, relID := range relationIDs {
		tableName := d.targetRelIDs[relID]
		if tableName == "" {
			continue
		}
		d.appendChange(TableChange{TableName: tableName, Change: Change{
			Operation: "TRUNCATE",
			LSN:       d.currentTxLSN,
		}})
	}
	return nil
}

func (d *MultiTableDecoder) handleStreamStart(data []byte) error {
	if len(data) < 5 {
		return fmt.Errorf("stream start message too short")
	}
	d.streamXid = binary.BigEndian.Uint32(data[:4])
	d.inStream = true
	return nil
}

// handleStreamCommit emits the buffered changes of a streamed transaction,
// stamped with the commit LSN from the message — the same stamp a
// non-streamed transaction gets from its Begin payload, keeping delivered
// LSNs monotonic for the per-table filter and resume state.
func (d *MultiTableDecoder) handleStreamCommit(data []byte) ([]DecodedChanges, error) {
	if len(data) < 4+1+8 {
		return nil, fmt.Errorf("stream commit message too short")
	}
	xid := binary.BigEndian.Uint32(data[:4])
	commitLSN := pglogrepl.LSN(binary.BigEndian.Uint64(data[5:13]))

	buffered := d.streamed[xid]
	delete(d.streamed, xid)
	delete(d.streamedLow, xid)
	if len(buffered) == 0 {
		return nil, nil
	}

	var groups []DecodedChanges
	groupIdx := make(map[string]int)
	for _, sc := range buffered {
		tc := sc.tc
		if d.tableSchemas[tc.TableName] == nil {
			continue
		}
		tc.Change.LSN = commitLSN
		idx, ok := groupIdx[tc.TableName]
		if !ok {
			idx = len(groups)
			groupIdx[tc.TableName] = idx
			groups = append(groups, DecodedChanges{TableName: tc.TableName, LSN: commitLSN})
		}
		groups[idx].Changes = append(groups[idx].Changes, tc.Change)
	}
	return groups, nil
}

// handleStreamAbort discards a streamed transaction's buffered changes: the
// whole transaction when the aborted xid is the top-level one, otherwise just
// the aborted subtransaction's changes.
func (d *MultiTableDecoder) handleStreamAbort(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("stream abort message too short")
	}
	xid := binary.BigEndian.Uint32(data[:4])
	subXid := binary.BigEndian.Uint32(data[4:8])

	if xid == subXid {
		delete(d.streamed, xid)
		delete(d.streamedLow, xid)
		return nil
	}

	buffered := d.streamed[xid]
	kept := buffered[:0]
	for _, sc := range buffered {
		if sc.xid != subXid {
			kept = append(kept, sc)
		}
	}
	if len(kept) == 0 {
		delete(d.streamed, xid)
		delete(d.streamedLow, xid)
		return nil
	}
	// The stale (lower) low-water stamp is kept: it can only make the safe
	// commit position more conservative, never skip data.
	d.streamed[xid] = kept
	return nil
}

// appendChange routes a decoded change either into the current transaction's
// pending buffer or, inside a protocol v2 stream segment, into the per-xid
// stream buffer.
func (d *MultiTableDecoder) appendChange(tc TableChange) {
	if d.inStream {
		if _, ok := d.streamedLow[d.streamXid]; !ok {
			d.streamedLow[d.streamXid] = d.walStart
		}
		d.streamed[d.streamXid] = append(d.streamed[d.streamXid], streamedChange{xid: d.msgXid, tc: tc})
		return
	}
	d.pendingChanges = append(d.pendingChanges, tc)
}

// StreamedLowWater returns the lowest WAL position of any buffered in-progress
// streamed transaction; false when none are buffered.
func (d *MultiTableDecoder) StreamedLowWater() (pglogrepl.LSN, bool) {
	var min pglogrepl.LSN
	found := false
	for _, lsn := range d.streamedLow {
		if !found || lsn < min {
			min = lsn
			found = true
		}
	}
	return min, found
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

	values, err := parseTupleData(data, rel, tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.appendChange(TableChange{
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
		oldValues, err = parseTupleData(data, rel, tableSchema, d.typeMap)
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

	values, err := parseTupleData(data, rel, tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse new tuple: %w", err)
	}

	d.appendChange(TableChange{
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

	values, err := parseTupleData(data, rel, tableSchema, d.typeMap)
	if err != nil {
		return fmt.Errorf("failed to parse tuple data: %w", err)
	}

	d.appendChange(TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "DELETE",
			LSN:       d.currentTxLSN,
			Values:    values,
		},
	})

	return nil
}
