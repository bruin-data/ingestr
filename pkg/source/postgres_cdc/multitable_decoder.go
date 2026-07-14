package postgres_cdc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

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
// it so a Stream Abort can discard that subtransaction and the buffered suffix
// containing its descendants.
type streamedChange struct {
	XID         uint32
	TableChange TableChange
}

func streamedChangeXID(change streamedChange) uint32 { return change.XID }

// MultiTableDecoder decodes pgoutput messages for multiple tables. It
// understands protocol v2 streaming of large in-progress transactions
// (Stream Start/Stop/Commit/Abort) and, when the stream runs with the
// `binary 'true'` option, binary-format tuple data.
type MultiTableDecoder struct {
	tableSchemas   map[string]*schema.TableSchema // schema name.table name -> schema
	expectedRelIDs map[string]uint32              // full table name -> connect-time relation ID
	relations      map[uint32]*RelationInfo
	targetRelIDs   map[uint32]string // relation ID -> full table name
	pendingChanges *changeSpool[streamedChange]
	committed      *changeSpool[streamedChange]
	committedLSN   pglogrepl.LSN
	currentTxLSN   pglogrepl.LSN
	typeMap        *pgtype.Map
	allowedUnknown map[string]map[string]struct{}
	historicalIDs  map[string]map[uint32]struct{}
	memoryBudget   *byteBudget

	// Protocol v2 streaming state. Between Stream Start and Stream Stop,
	// change messages carry a subtransaction xid and are buffered per
	// top-level transaction until its Stream Commit (or dropped on abort).
	inStream    bool
	streamXid   uint32 // top-level xid of the current stream segment
	msgXid      uint32 // xid carried by the message being decoded
	walStart    pglogrepl.LSN
	streamed    map[uint32]*changeSpool[streamedChange]
	streamedLow map[uint32]pglogrepl.LSN // lowest WAL position buffered per top-level xid
}

func NewMultiTableDecoder(tables []source.SourceTableInfo) *MultiTableDecoder {
	return newMultiTableDecoderWithBudget(tables, newByteBudget(defaultDecoderMemoryBytes))
}

func newMultiTableDecoderWithBudget(tables []source.SourceTableInfo, budget *byteBudget) *MultiTableDecoder {
	tableSchemas := make(map[string]*schema.TableSchema)
	expectedRelIDs := make(map[string]uint32)
	for _, table := range tables {
		tableSchemas[table.Name] = table.Schema
		if oid, err := strconv.ParseUint(table.Incarnation, 10, 32); err == nil {
			expectedRelIDs[table.Name] = uint32(oid)
		}
	}

	return &MultiTableDecoder{
		tableSchemas:   tableSchemas,
		expectedRelIDs: expectedRelIDs,
		relations:      make(map[uint32]*RelationInfo),
		targetRelIDs:   make(map[uint32]string),
		typeMap:        pgtype.NewMap(),
		pendingChanges: newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, budget, streamedChangeXID),
		streamed:       make(map[uint32]*changeSpool[streamedChange]),
		streamedLow:    make(map[uint32]pglogrepl.LSN),
		memoryBudget:   budget,
	}
}

func (d *MultiTableDecoder) InStream() bool {
	return d.inStream
}

// DecodedChanges carries a bounded chunk of one committed transaction's
// decoded changes for a table.
type DecodedChanges struct {
	TableName string
	Changes   []Change
	LSN       pglogrepl.LSN
}

// Decode decodes a WAL message and, on a (stream) commit, returns the first
// bounded chunk grouped per target table.
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
		if rel := d.relations[relID]; rel != nil && rel.Stale {
			continue
		}
		if err := d.appendChange(TableChange{TableName: tableName, Change: Change{
			Operation: "TRUNCATE",
			LSN:       d.currentTxLSN,
		}}); err != nil {
			return err
		}
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
	if buffered == nil {
		return nil, nil
	}
	if buffered.Len() == 0 {
		return nil, buffered.Close()
	}
	if err := buffered.Seal(); err != nil {
		return nil, err
	}
	d.committed = buffered
	d.committedLSN = commitLSN
	return d.DrainCommitted(defaultCommittedDrainChanges)
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
		if buffered := d.streamed[xid]; buffered != nil {
			if err := buffered.Close(); err != nil {
				return err
			}
		}
		delete(d.streamed, xid)
		delete(d.streamedLow, xid)
		return nil
	}

	buffered := d.streamed[xid]
	if buffered == nil {
		return nil
	}
	buffered.ExcludeFrom(subXid)
	if buffered.Len() == 0 {
		if err := buffered.Close(); err != nil {
			return err
		}
		delete(d.streamed, xid)
		delete(d.streamedLow, xid)
		return nil
	}
	// The stale (lower) low-water stamp is kept: it can only make the safe
	// commit position more conservative, never skip data.
	return nil
}

// appendChange routes a decoded change either into the current transaction's
// pending buffer or, inside a protocol v2 stream segment, into the per-xid
// stream buffer.
func (d *MultiTableDecoder) appendChange(tc TableChange) error {
	if d.inStream {
		if _, ok := d.streamedLow[d.streamXid]; !ok {
			d.streamedLow[d.streamXid] = d.walStart
		}
		buffered := d.streamed[d.streamXid]
		if buffered == nil {
			buffered = newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, d.memoryBudget, streamedChangeXID)
			d.streamed[d.streamXid] = buffered
		}
		tc.Change.Sequence = uint64(buffered.Len()+1) * 2
		return buffered.Append(streamedChange{XID: d.msgXid, TableChange: tc})
	}
	tc.Change.Sequence = uint64(d.pendingChanges.Len()+1) * 2
	return d.pendingChanges.Append(streamedChange{TableChange: tc})
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
		if expected := d.expectedRelIDs[tableName]; expected != 0 && rel.RelationID != expected {
			if _, historical := d.historicalIDs[tableName][rel.RelationID]; historical {
				rel.Stale = true
				d.targetRelIDs[rel.RelationID] = tableName
				d.relations[rel.RelationID] = rel
				return nil
			}
			return &TableReincarnatedError{
				Table:    tableName,
				Previous: strconv.FormatUint(uint64(expected), 10),
				Current:  strconv.FormatUint(uint64(rel.RelationID), 10),
			}
		}
		d.targetRelIDs[rel.RelationID] = tableName
		config.Debug("[CDC] Found target relation: %s (ID: %d)", tableName, rel.RelationID)
		if tableSchema := d.tableSchemas[tableName]; tableSchema != nil {
			prev := d.relations[rel.RelationID]
			if err := mapRelationToSchema(rel, prev, tableSchema, tableName, d.allowedUnknown[tableName]); err != nil {
				// Do not store rel on error: a rebuilt stream must retry against the
				// last accepted relation so schema-change detection remains stable.
				return err
			}
		}
	}

	d.relations[rel.RelationID] = rel
	return nil
}

func (d *MultiTableDecoder) AllowUnknownRelationColumns(columns map[string]map[string]struct{}) {
	d.allowedUnknown = columns
}

func (d *MultiTableDecoder) AllowHistoricalRelationIDs(ids map[string]map[uint32]struct{}) {
	d.historicalIDs = ids
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
	if d.committed != nil && d.committed.Len() > 0 {
		return errors.New("received BEGIN before committed transaction was drained")
	}
	if err := d.pendingChanges.Close(); err != nil {
		return err
	}
	d.pendingChanges = newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, d.memoryBudget, streamedChangeXID)
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
	if d.pendingChanges == nil || d.pendingChanges.Len() == 0 {
		return 0, false
	}
	return d.currentTxLSN, true
}

func (d *MultiTableDecoder) handleCommit() ([]DecodedChanges, error) {
	if d.pendingChanges == nil || d.pendingChanges.Len() == 0 {
		return nil, nil
	}
	if err := d.pendingChanges.Seal(); err != nil {
		return nil, err
	}
	d.committed = d.pendingChanges
	d.committedLSN = d.currentTxLSN
	d.pendingChanges = newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, d.memoryBudget, streamedChangeXID)
	return d.DrainCommitted(defaultCommittedDrainChanges)
}

func (d *MultiTableDecoder) HasCommitted() bool {
	return d.committed != nil && d.committed.Len() > 0
}

func (d *MultiTableDecoder) CommittedLowWater() (pglogrepl.LSN, bool) {
	return d.committedLSN, d.HasCommitted()
}

func (d *MultiTableDecoder) DrainCommitted(limit int) ([]DecodedChanges, error) {
	if !d.HasCommitted() {
		return nil, nil
	}
	buffered, err := d.committed.Drain(limit)
	if err != nil {
		return nil, err
	}
	groups := make([]DecodedChanges, 0)
	groupIdx := make(map[string]int)
	for _, sc := range buffered {
		tc := sc.TableChange
		if d.tableSchemas[tc.TableName] == nil {
			continue
		}
		tc.Change.LSN = d.committedLSN
		idx, ok := groupIdx[tc.TableName]
		if !ok {
			idx = len(groups)
			groupIdx[tc.TableName] = idx
			groups = append(groups, DecodedChanges{TableName: tc.TableName, LSN: d.committedLSN})
		}
		groups[idx].Changes = append(groups[idx].Changes, tc.Change)
	}
	if d.committed.Len() == 0 {
		err = d.committed.Close()
		d.committed = nil
	}
	return groups, err
}

func (d *MultiTableDecoder) Close() error {
	var err error
	if d.pendingChanges != nil {
		err = errors.Join(err, d.pendingChanges.Close())
	}
	if d.committed != nil {
		err = errors.Join(err, d.committed.Close())
	}
	for xid, buffered := range d.streamed {
		err = errors.Join(err, buffered.Close())
		delete(d.streamed, xid)
	}
	return err
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
	if rel.Stale {
		return nil
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

	return d.appendChange(TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "INSERT",
			LSN:       d.currentTxLSN,
			Values:    values,
		},
	})
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
	if rel.Stale {
		return nil
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
	markMissingRelationColumnsUnchanged(values, rel)

	return d.appendChange(TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "UPDATE",
			LSN:       d.currentTxLSN,
			Values:    values,
			OldValues: oldValues,
		},
	})
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
	if rel.Stale {
		return nil
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

	return d.appendChange(TableChange{
		TableName: tableName,
		Change: Change{
			Operation: "DELETE",
			LSN:       d.currentTxLSN,
			Values:    values,
		},
	})
}
