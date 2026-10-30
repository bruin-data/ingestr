package postgres_cdc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// MultiTableCDCReader handles reading CDC changes from multiple tables.
type MultiTableCDCReader struct {
	source               *PostgresCDCSource
	tables               []source.SourceTableInfo
	cdcConfig            CDCConfig
	resumeLSNs           map[string]string        // per-table resume LSNs
	processedLSNs        map[string]pglogrepl.LSN // in-memory tracking of last processed LSN per table
	snapshotBoundaries   map[string]bool
	allowedUnknown       map[string]map[string]struct{}
	historicalRelIDs     map[string]map[uint32]struct{}
	coverageMissing      map[string]struct{}
	initialAnnouncements []source.SourceTableInfo
	initialInvalidations []source.CDCSnapshotInvalidation
	// snapshotResumes holds interrupted snapshots of tables without resume
	// state; the startup backfill continues them instead of starting over.
	snapshotResumes map[string]source.CDCSnapshotProgress
	mu              sync.RWMutex // protects processedLSNs and snapshotBoundaries
	slotSuffix      string
}

func NewMultiTableCDCReader(src *PostgresCDCSource, tables []source.SourceTableInfo, cdcConfig CDCConfig, resumeLSNs map[string]string, slotSuffix string) *MultiTableCDCReader {
	reconcileTableKeys(tables)
	return &MultiTableCDCReader{
		source:             src,
		tables:             tables,
		cdcConfig:          cdcConfig,
		resumeLSNs:         resumeLSNs,
		processedLSNs:      make(map[string]pglogrepl.LSN),
		snapshotBoundaries: make(map[string]bool),
		allowedUnknown:     make(map[string]map[string]struct{}),
		historicalRelIDs:   make(map[string]map[uint32]struct{}),
		coverageMissing:    make(map[string]struct{}),
		slotSuffix:         slotSuffix,
	}
}

// reconcileTableKeys folds each table's effective merge keys into its schema so
// the decoder, compaction, and unchanged-TOAST fill all key off the same keys
// the destination merge uses. SourceTableInfo.PrimaryKeys is authoritative and
// may carry user-provided keys that the schema's detected keys lack.
func reconcileTableKeys(tables []source.SourceTableInfo) {
	for i := range tables {
		if len(tables[i].PrimaryKeys) > 0 && tables[i].Schema != nil {
			tables[i].Schema.PrimaryKeys = tables[i].PrimaryKeys
		}
	}
}

// GetProcessedLSN returns the last processed LSN for a table (for streaming mode).
func (r *MultiTableCDCReader) GetProcessedLSN(tableName string) pglogrepl.LSN {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.processedLSNs[tableName]
}

// updateProcessedLSN updates the in-memory tracking of processed LSN for a table.
func (r *MultiTableCDCReader) updateProcessedLSN(tableName string, lsn pglogrepl.LSN) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lsn > r.processedLSNs[tableName] {
		r.processedLSNs[tableName] = lsn
	}
	r.snapshotBoundaries[tableName] = false
}

func (r *MultiTableCDCReader) setSnapshotBoundary(tableName string, lsn pglogrepl.LSN) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processedLSNs[tableName] = lsn
	r.snapshotBoundaries[tableName] = true
}

// Read starts reading CDC changes from all tables in the publication.
func (r *MultiTableCDCReader) Read(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	if opts.CDCChunkedSnapshot != nil {
		slot := r.cdcConfig.SlotName
		if slot == "" {
			slot = generateMultiTableSlotName(r.cdcConfig.Publication, r.slotSuffix)
		}
		if len(r.initialAnnouncements) > 0 || len(r.initialInvalidations) > 0 {
			return nil, fmt.Errorf("source table metadata changed before chunked snapshot startup; restart with --full-refresh")
		}
		return r.source.readChunked(ctx, r.tables, slot, r.resumeLSNs, opts.ReadOptions, true)
	}
	results := make(chan source.RecordBatchResult, 16)

	go func() {
		defer close(results)
		if !opts.Streaming {
			if err := validateBatchBarrierSupport(r.source.serverVersion); err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
		}
		for i := range r.initialInvalidations {
			invalidation := r.initialInvalidations[i]
			if err := sendResult(ctx, results, source.RecordBatchResult{SnapshotInvalidation: &invalidation}); err != nil {
				return
			}
		}
		for i := range r.initialAnnouncements {
			table := r.initialAnnouncements[i]
			if err := sendResult(ctx, results, source.RecordBatchResult{TableName: table.Name, TableInfo: &table}); err != nil {
				return
			}
		}

		// Determine if we need snapshot or can resume
		needsSnapshot, minResumeLSN := r.determineResumeStrategy()

		// Resolve slot name
		slotName := r.cdcConfig.SlotName
		if slotName == "" {
			slotName = generateMultiTableSlotName(r.cdcConfig.Publication, r.slotSuffix)
		}

		// If resume strategy says no snapshot needed, verify the slot actually exists
		if !needsSnapshot {
			var slotCandidates []string
			if r.cdcConfig.SlotName == "" && opts.CDCLegacySlotSuffix != "" {
				candidate := generateLegacyMultiTableSlotName(r.cdcConfig.Publication, opts.CDCLegacySlotSuffix)
				if legacySlotNameUnambiguous(candidate, opts.CDCLegacySlotSuffix) {
					slotCandidates = append(slotCandidates, candidate)
				}
			}
			resolvedSlot, exists, legacy, err := resolveResumeSlotCandidates(ctx, r.source.queryPool, slotName, slotCandidates...)
			if err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			if !exists {
				config.Debug("[CDC] Multi-table: replication slot %s not found, falling back to full snapshot", slotName)
				needsSnapshot = true
			} else {
				slotName = resolvedSlot
				if legacy {
					r.source.markLegacySlotInUse(slotName)
				}
				if !legacy {
					waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
				}
				config.Debug("[CDC] Multi-table: resuming from LSN %s with per-table filtering", minResumeLSN)
			}
		}

		var startLSN pglogrepl.LSN
		if needsSnapshot {
			config.Debug("[CDC] Multi-table: performing full snapshot (some tables have no existing data)")
			if err := r.executeSnapshot(ctx, results, opts); err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)})
				return
			}
			var err error
			startLSN, err = r.getSlotLSN(ctx, slotName)
			if err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("failed to get slot LSN: %w", err)})
				return
			}
		} else {
			startLSN = minResumeLSN
			// Tables without resume state are not covered by the resumed stream:
			// their pre-existing rows were never snapshotted (typically tables
			// created since the previous run). Backfill them before streaming.
			if newTables := r.tablesWithoutResumeState(); len(newTables) > 0 {
				resumes := r.snapshotResumes
				r.snapshotResumes = nil
				if err := r.backfillTables(ctx, slotName, newTables, nil, resumes, results, opts); err != nil {
					_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("backfill failed: %w", err)})
					return
				}
			}
		}
		if needsSnapshot && !opts.Streaming {
			r.source.recordCaughtUpLSN(startLSN, slotName, false)
			return
		}

		for {
			var barrier batchBarrier
			if !opts.Streaming {
				var err error
				barrier, err = r.source.beginBatchBarrier(ctx)
				if err != nil {
					_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
					return
				}
			}
			signal, err := r.streamChanges(ctx, startLSN, barrier, slotName, results, opts)
			if err != nil {
				var schemaErr *SchemaChangedError
				if !opts.Streaming || !errors.As(err, &schemaErr) {
					_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)})
					return
				}
				output.Statusf("Schema change detected on table %s (column %q %s); rebuilding stream around the new schema\n", schemaErr.Table, schemaErr.Column, schemaErr.Reason)
				signal = &streamSignal{changedTables: []string{schemaErr.Table}, schemaErrors: []*SchemaChangedError{schemaErr}}
			}
			if signal == nil {
				return
			}
			startLSN, err = r.rebuildStream(ctx, slotName, signal, results, opts)
			if err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("failed to rebuild stream: %w", err)})
				return
			}
		}
	}()

	return results, nil
}

// tablesWithoutResumeState returns tables that have neither a resume LSN nor an
// in-memory processed LSN — on a resumed run, tables with no state in the
// destination, typically created since the previous run. A table that was
// merely empty at the original snapshot re-snapshots as empty, so backfilling
// the whole set is idempotent.
func (r *MultiTableCDCReader) tablesWithoutResumeState() []source.SourceTableInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []source.SourceTableInfo
	for _, t := range r.tables {
		if _, ok := r.resumeLSNs[t.Name]; ok {
			continue
		}
		if _, ok := r.processedLSNs[t.Name]; ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

// backfillTables snapshots tables that joined the stream after the main slot
// was created. A temporary replication slot on a dedicated connection provides
// the consistent point and an exported snapshot: rows existing before that
// point are read here, and the main stream delivers changes at or after it
// (ShouldFilterChange drops the already-snapshotted overlap; the boundary
// transaction is re-emitted and absorbed by the idempotent merge). The
// temporary slot vanishes when the connection closes. Backfill batches carry
// no commit tokens — the temporary slot's consistent point is ahead of the
// main slot, so confirming it would skip WAL not yet streamed.
//
// A table in resumes continues an interrupted snapshot: rows after its last
// durable key are read at this backfill's consistent point, while the table
// keeps the original boundary for stream filtering and certification. Stream
// changes between the two points that the newer rows already reflect are
// rejected by the merge's position ordering.
func (r *MultiTableCDCReader) backfillTables(ctx context.Context, slotName string, tables []source.SourceTableInfo, replacements map[string]struct{}, resumes map[string]source.CDCSnapshotProgress, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	if err := r.source.requireSnapshotAllowed(); err != nil {
		return err
	}
	conn, err := r.source.openReplicationConn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	tempSlot := backfillSlotName(slotName)
	created, err := r.source.createLogicalSlot(ctx, conn, tempSlot, true)
	if err != nil {
		return fmt.Errorf("failed to create backfill slot: %w", err)
	}

	snapshotLSN, err := pglogrepl.ParseLSN(created.ConsistentPoint)
	if err != nil {
		return fmt.Errorf("failed to parse backfill LSN: %w", err)
	}

	config.Debug("[CDC] Backfill slot %s created at LSN %s (snapshot %s) for %d table(s)",
		tempSlot, created.ConsistentPoint, created.SnapshotName, len(tables))

	// The exported snapshot supports concurrent readers: each table imports it
	// into its own repeatable-read transaction while this replication
	// connection keeps it alive.
	boundaries := make(map[string]pglogrepl.LSN, len(tables))
	for _, table := range tables {
		boundaries[table.Name] = snapshotLSN
		if resume, ok := resumes[table.Name]; ok {
			boundary, err := pglogrepl.ParseLSN(resume.Boundary)
			if err != nil {
				return fmt.Errorf("invalid snapshot resume boundary %q for %s: %w", resume.Boundary, table.Name, err)
			}
			boundaries[table.Name] = boundary
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(snapshotParallelism(opts))
	for i := range tables {
		table := tables[i]
		tableInfo := &tables[i]
		g.Go(func() error {
			if resume, ok := resumes[table.Name]; ok {
				output.Statusf("Resuming the interrupted snapshot of %s after key %s\n", table.Name, resume.Key)
			} else {
				output.Statusf("Backfilling new table %s before streaming its changes\n", table.Name)
			}
			if opts.Streaming {
				if _, replacement := replacements[table.Name]; replacement {
					if err := sendResult(gctx, results, source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
						TableName: table.Name, Incarnation: table.Incarnation,
					}}); err != nil {
						return err
					}
				}
				// Announce the table so a consumer that prepared its per-table
				// state upfront can provision the destination before data
				// arrives. Per-table ordering (announcement before that
				// table's batches) is preserved inside this goroutine.
				if err := sendResult(gctx, results, source.RecordBatchResult{TableName: table.Name, TableInfo: tableInfo}); err != nil {
					return err
				}
			}
			var resume *source.CDCSnapshotProgress
			if progress, ok := resumes[table.Name]; ok {
				resume = &progress
			}
			if err := r.snapshotTable(gctx, table, created.SnapshotName, snapshotLSN, resume, results, opts); err != nil {
				return fmt.Errorf("failed to backfill table %s: %w", table.Name, err)
			}
			r.setSnapshotBoundary(table.Name, boundaries[table.Name])
			r.source.recordSnapshotState(table.Name, boundaries[table.Name], table.Incarnation, table.SchemaFingerprint)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if opts.Streaming {
		positions := make(map[string]string, len(tables))
		for _, table := range tables {
			positions[table.Name] = FormatLSN(boundaries[table.Name])
		}
		if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
			SnapshotPositions: positions, SnapshotIncarnations: tableIncarnations(tables), SnapshotSchemas: tableSchemaFingerprints(tables),
		}}); err != nil {
			return err
		}
	}
	return nil
}

// snapshotParallelism bounds how many tables are snapshotted concurrently
// from the same exported snapshot.
func snapshotParallelism(opts source.MultiTableReadOptions) int {
	if opts.Parallelism > 0 {
		return opts.Parallelism
	}
	return 4
}

// backfillSlotName derives a temporary-slot name that cannot collide with the
// main slot even after truncation to Postgres's 63-character limit.
func backfillSlotName(slotName string) string {
	const suffix = "_bf"
	if len(slotName) > 63-len(suffix) {
		slotName = slotName[:63-len(suffix)]
	}
	return slotName + suffix
}

// streamSignal tells Read why streamChanges returned without error: tables
// that appeared on the source mid-stream, or tables whose source schema
// changed mid-stream (DDL). A nil signal means normal termination.
type streamSignal struct {
	newTables          []string
	changedTables      []string
	reincarnatedTables []string
	schemaErrors       []*SchemaChangedError
	historicalRelIDs   map[string][]uint32
}

// rebuildStream incorporates mid-stream source changes: tables that appeared
// (reconcile the managed publication so Postgres starts publishing them, then
// backfill them) and tables whose schema changed (announce the refreshed
// schema so the consumer evolves the destination before their rows arrive).
// The table set is refreshed from the source either way, and the replication
// connection is reopened for a fresh StartReplication on the same persistent
// slot. Returns the LSN to resume streaming from — the slot's confirmed
// position; transactions above it that were already emitted are filtered per
// table by ShouldFilterChange. The transaction that tripped a schema change
// was never emitted, so the rebuilt stream re-decodes it against the
// refreshed schema and none of its data is lost.
func (r *MultiTableCDCReader) rebuildStream(ctx context.Context, slotName string, signal *streamSignal, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) (pglogrepl.LSN, error) {
	if err := r.source.requireSnapshotAllowed(); err != nil {
		return 0, err
	}
	if len(signal.newTables) > 0 || len(signal.reincarnatedTables) > 0 {
		if len(signal.newTables) > 0 {
			output.Statusf("New table(s) detected on source: %s; adding to CDC stream\n", strings.Join(signal.newTables, ", "))
		}
		if len(signal.reincarnatedTables) > 0 {
			output.Statusf("Recreated table(s) detected on source: %s; replacing destination snapshots\n", strings.Join(signal.reincarnatedTables, ", "))
		}
		if err := r.source.reconcilePublication(ctx); err != nil {
			return 0, fmt.Errorf("failed to reconcile publication: %w", err)
		}
	}

	// Re-list without re-validating the selection: a selected table that has
	// disappeared is handled by the coverage-gap path below, not by failing the
	// stream.
	tables, err := r.source.getTables(ctx, false)
	if err != nil {
		return 0, fmt.Errorf("failed to refresh table list: %w", err)
	}
	reconcileTableKeys(tables)

	prevSchemas := make(map[string]*schema.TableSchema, len(r.tables))
	prevIncarnations := make(map[string]string, len(r.tables))
	for _, t := range r.tables {
		prevSchemas[t.Name] = t.Schema
		prevIncarnations[t.Name] = t.Incarnation
	}
	for _, table := range tables {
		if previous, ok := prevSchemas[table.Name]; ok && !sameKeyColumns(previous.PrimaryKeys, table.PrimaryKeys) {
			return 0, fmt.Errorf("replica identity keys changed for %s; restart with --full-refresh to rebuild using keys %v", table.Name, table.PrimaryKeys)
		}
	}
	var added []source.SourceTableInfo
	for _, t := range tables {
		if _, ok := prevSchemas[t.Name]; !ok {
			added = append(added, t)
		}
	}
	changedIndexes := shapeChangedTables(prevSchemas, tables)
	changedSet := make(map[string]struct{}, len(changedIndexes)+len(signal.changedTables)+len(signal.reincarnatedTables))
	for _, idx := range changedIndexes {
		changedSet[tables[idx].Name] = struct{}{}
	}
	for _, tableName := range signal.reincarnatedTables {
		changedSet[tableName] = struct{}{}
	}
	for _, tableName := range signal.changedTables {
		changedSet[tableName] = struct{}{}
	}
	for _, table := range tables {
		if previous := prevIncarnations[table.Name]; previous != "" && table.Incarnation != "" && previous != table.Incarnation {
			changedSet[table.Name] = struct{}{}
		}
	}
	r.tables = tables
	for _, table := range tables {
		if _, changed := changedSet[table.Name]; !changed {
			continue
		}
		for _, column := range replacedColumns(prevSchemas[table.Name], table.Schema) {
			if r.allowedUnknown[table.Name] == nil {
				r.allowedUnknown[table.Name] = make(map[string]struct{})
			}
			r.allowedUnknown[table.Name][column] = struct{}{}
		}
	}
	for _, schemaErr := range signal.schemaErrors {
		for _, table := range tables {
			if table.Name != schemaErr.Table {
				continue
			}
			if r.allowedUnknown[table.Name] == nil {
				r.allowedUnknown[table.Name] = make(map[string]struct{})
			}
			for _, column := range schemaErr.Columns() {
				r.allowedUnknown[table.Name][column] = struct{}{}
			}
		}
	}

	toSnapshot := append([]source.SourceTableInfo(nil), added...)
	for _, table := range tables {
		if _, changed := changedSet[table.Name]; changed {
			toSnapshot = append(toSnapshot, table)
		}
	}
	if len(changedSet) > 0 && !opts.CDCSnapshotReplace {
		return 0, fmt.Errorf("source table schema or incarnation changed, but the destination cannot atomically replace the affected table snapshot")
	}
	if len(toSnapshot) > 0 {
		if err := r.backfillTables(ctx, slotName, toSnapshot, changedSet, nil, results, opts); err != nil {
			return 0, err
		}
	}
	for _, table := range tables {
		previousIncarnation := prevIncarnations[table.Name]
		if previousIncarnation == "" || table.Incarnation == "" || previousIncarnation == table.Incarnation {
			continue
		}
		previous, err := strconv.ParseUint(previousIncarnation, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid historical PostgreSQL table incarnation %q: %w", previousIncarnation, err)
		}
		if r.historicalRelIDs[table.Name] == nil {
			r.historicalRelIDs[table.Name] = make(map[uint32]struct{})
		}
		r.historicalRelIDs[table.Name][uint32(previous)] = struct{}{}
	}
	for tableName, relationIDs := range signal.historicalRelIDs {
		var liveIncarnation string
		for _, table := range tables {
			if table.Name == tableName {
				liveIncarnation = table.Incarnation
				break
			}
		}
		live, err := strconv.ParseUint(liveIncarnation, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid live PostgreSQL table incarnation %q: %w", liveIncarnation, err)
		}
		if r.historicalRelIDs[tableName] == nil {
			r.historicalRelIDs[tableName] = make(map[uint32]struct{})
		}
		for _, relationID := range relationIDs {
			if relationID != uint32(live) {
				r.historicalRelIDs[tableName][relationID] = struct{}{}
			}
		}
	}

	// Announce every previously known table whose refreshed schema no longer
	// matches the shape the consumer prepared — not just the table that
	// tripped the rebuild. GetTables refreshed ALL schemas, so the rebuilt
	// decoders emit the new shape for every table that had DDL, whether or not
	// its Relation message has been seen yet. (Tables added above were
	// announced by backfillTables.)
	// Changed tables were announced by backfillTables before their replacement
	// snapshot batches, preserving destination evolution ordering.

	if err := r.source.reconnectReplication(ctx); err != nil {
		return 0, err
	}
	waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
	return r.getSlotLSN(ctx, slotName)
}

// replacedColumns lists columns of prev that next dropped or retyped. WAL
// replayed from before a rebuild still carries relation messages describing
// them, which must decode as historical instead of triggering another rebuild.
func replacedColumns(prev, next *schema.TableSchema) []string {
	if prev == nil || next == nil {
		return nil
	}
	current := make(map[string]schema.Column, len(next.Columns))
	for _, col := range next.Columns {
		current[col.Name] = col
	}
	var out []string
	for _, col := range prev.Columns {
		now, ok := current[col.Name]
		if !ok || now.DataType != col.DataType || now.ArrayType != col.ArrayType || now.MaxLength != col.MaxLength || now.Precision != col.Precision || now.Scale != col.Scale {
			out = append(out, col.Name)
		}
	}
	return out
}

// shapeChangedTables returns the indices of refreshed tables whose column
// shape differs from the schema previously tracked for them. Tables not in
// prev (newly added) are excluded — the backfill path announces those.
func shapeChangedTables(prev map[string]*schema.TableSchema, refreshed []source.SourceTableInfo) []int {
	var out []int
	for i := range refreshed {
		old, ok := prev[refreshed[i].Name]
		if !ok {
			continue
		}
		if !old.SameColumnShape(refreshed[i].Schema) {
			out = append(out, i)
		}
	}
	return out
}

// detectNewTables lists the tables the stream should currently cover and
// returns those missing from the active table set.
func (r *MultiTableCDCReader) detectTableChanges(ctx context.Context) ([]string, []string, []string, error) {
	if r.source.standby && r.source.managedPublication {
		if err := r.source.checkManagedPublicationOnStandby(ctx); err != nil {
			return nil, nil, nil, err
		}
	}
	eligible, err := r.source.listEligibleTableIncarnations(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	added, reincarnated, err := diffTableIncarnations(r.tables, eligible)
	if err != nil {
		return nil, nil, nil, err
	}
	reincarnatedSet := make(map[string]struct{}, len(reincarnated))
	for _, name := range reincarnated {
		reincarnatedSet[name] = struct{}{}
	}
	for _, name := range updateCoverageGaps(r.tables, eligible, r.coverageMissing) {
		if _, exists := reincarnatedSet[name]; !exists {
			reincarnated = append(reincarnated, name)
			reincarnatedSet[name] = struct{}{}
		}
	}
	var changed []string
	for _, table := range r.tables {
		if _, exists := eligible[table.Name]; !exists {
			continue
		}
		if _, returnedAfterGap := reincarnatedSet[table.Name]; returnedAfterGap {
			continue
		}
		fingerprint, err := r.source.TableSchemaFingerprint(ctx, table.Name)
		if err != nil {
			return nil, nil, nil, err
		}
		if table.SchemaFingerprint != "" && fingerprint != table.SchemaFingerprint {
			changed = append(changed, table.Name)
		}
	}
	sort.Strings(changed)
	sort.Strings(reincarnated)
	return added, changed, reincarnated, nil
}

func updateCoverageGaps(current []source.SourceTableInfo, eligible map[string]string, missing map[string]struct{}) []string {
	var returned []string
	for _, table := range current {
		if _, exists := eligible[table.Name]; !exists {
			missing[table.Name] = struct{}{}
			continue
		}
		if _, wasMissing := missing[table.Name]; wasMissing {
			returned = append(returned, table.Name)
			delete(missing, table.Name)
		}
	}
	sort.Strings(returned)
	return returned
}

func diffTableIncarnations(current []source.SourceTableInfo, eligible map[string]string) ([]string, []string, error) {
	known := make(map[string]string, len(current))
	for _, table := range current {
		known[table.Name] = table.Incarnation
	}
	var added, reincarnated []string
	for name, incarnation := range eligible {
		previous, exists := known[name]
		switch {
		case !exists:
			added = append(added, name)
		case previous != "" && incarnation != "" && previous != incarnation:
			reincarnated = append(reincarnated, name)
		}
	}
	sort.Strings(added)
	sort.Strings(reincarnated)
	return added, reincarnated, nil
}

// diffNewTables returns the names in eligible that are missing from current,
// sorted for deterministic output.
func diffNewTables(current []source.SourceTableInfo, eligible map[string]struct{}) []string {
	known := make(map[string]struct{}, len(current))
	for _, t := range current {
		known[t.Name] = struct{}{}
	}
	var out []string
	for name := range eligible {
		if _, ok := known[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// determineResumeStrategy determines if we need a full snapshot or can resume.
// Returns (needsSnapshot, minLSN).
//
// Only requires that at least one table has a resume LSN. Tables missing from
// the resume map (created since the previous run, or empty ever since the
// original snapshot) are backfilled via a temporary-slot snapshot before
// streaming starts; see tablesWithoutResumeState.
func (r *MultiTableCDCReader) determineResumeStrategy() (bool, pglogrepl.LSN) {
	if len(r.resumeLSNs) == 0 {
		return true, 0 // No resume LSNs = full snapshot
	}

	// Find minimum LSN across tables that have resume data.
	var minLSN pglogrepl.LSN
	first := true
	for tableName, lsnStr := range r.resumeLSNs {
		lsn, err := pglogrepl.ParseLSN(lsnStr)
		if err != nil {
			config.Debug("[CDC] Failed to parse resume LSN for %s: %v, need full snapshot", tableName, err)
			return true, 0
		}
		if first || lsn < minLSN {
			minLSN = lsn
			first = false
		}
	}

	// Initialize processedLSNs from resumeLSNs
	for tableName, lsnStr := range r.resumeLSNs {
		lsn, _ := pglogrepl.ParseLSN(lsnStr)
		r.processedLSNs[tableName] = lsn
	}

	for _, table := range r.tables {
		if _, ok := r.resumeLSNs[table.Name]; !ok {
			config.Debug("[CDC] Table %s has no resume LSN (no destination state), will backfill before streaming", table.Name)
		}
	}

	return false, minLSN
}

// executeSnapshot performs a full snapshot for all tables.
func (r *MultiTableCDCReader) executeSnapshot(ctx context.Context, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	if err := r.source.requireSnapshotAllowed(); err != nil {
		return err
	}
	slotName := r.cdcConfig.SlotName
	if slotName == "" {
		slotName = generateMultiTableSlotName(r.cdcConfig.Publication, r.slotSuffix)
	}

	// Check if slot already exists
	_, exists, err := checkSlotExists(ctx, r.source.queryPool, slotName)
	if err != nil {
		return fmt.Errorf("failed to check existing slot: %w", err)
	}

	if exists {
		config.Debug("[CDC] Dropping existing slot %s to get fresh snapshot", slotName)
		if err := r.dropSlot(ctx, slotName); err != nil {
			return fmt.Errorf("failed to drop existing slot: %w", err)
		}
	}

	config.Debug("[CDC] Creating persistent replication slot: %s", slotName)

	// Create replication slot with snapshot export
	result, err := r.source.createLogicalSlot(ctx, r.source.replConn, slotName, false)
	if err != nil {
		return fmt.Errorf("failed to create replication slot: %w", err)
	}

	snapshotLSN, err := pglogrepl.ParseLSN(result.ConsistentPoint)
	if err != nil {
		return fmt.Errorf("failed to parse LSN: %w", err)
	}

	config.Debug("[CDC] Replication slot created: %s, LSN: %s, Snapshot: %s",
		slotName, result.ConsistentPoint, result.SnapshotName)

	// Snapshot the tables concurrently using the same exported snapshot: each
	// reader imports it into its own repeatable-read transaction, so all see
	// the identical consistent point while the idle replication connection
	// keeps the snapshot alive.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(snapshotParallelism(opts))
	for _, table := range r.tables {
		g.Go(func() error {
			config.Debug("[CDC] Snapshotting table: %s", table.Name)
			if err := r.snapshotTable(gctx, table, result.SnapshotName, snapshotLSN, nil, results, opts); err != nil {
				return fmt.Errorf("failed to snapshot table %s: %w", table.Name, err)
			}
			// Initialize processed LSN for this table
			r.setSnapshotBoundary(table.Name, snapshotLSN)
			r.source.recordSnapshotState(table.Name, snapshotLSN, table.Incarnation, table.SchemaFingerprint)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}
	if opts.Streaming {
		positions := make(map[string]string, len(r.tables))
		for _, table := range r.tables {
			positions[table.Name] = FormatLSN(snapshotLSN)
		}
		token := snapshotCommitToken(snapshotLSN, positions)
		token.SnapshotIncarnations = tableIncarnations(r.tables)
		token.SnapshotSchemas = tableSchemaFingerprints(r.tables)
		if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: token}); err != nil {
			return err
		}
	}
	return nil
}

func tableIncarnations(tables []source.SourceTableInfo) map[string]string {
	incarnations := make(map[string]string, len(tables))
	for _, table := range tables {
		if table.Incarnation != "" {
			incarnations[table.Name] = table.Incarnation
		}
	}
	return incarnations
}

func tableSchemaFingerprints(tables []source.SourceTableInfo) map[string]string {
	fingerprints := make(map[string]string, len(tables))
	for _, table := range tables {
		if table.SchemaFingerprint != "" {
			fingerprints[table.Name] = table.SchemaFingerprint
		}
	}
	return fingerprints
}

// snapshotTable reads all data from a single table using the snapshot. A
// streaming snapshot that the consumer persists state for reads keyed tables
// in key order and reports its progress; resume continues one after its last
// durable key.
func (r *MultiTableCDCReader) snapshotTable(ctx context.Context, table source.SourceTableInfo, snapshotName string, lsn pglogrepl.LSN, resume *source.CDCSnapshotProgress, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	snapshot := &Snapshot{
		source:      r.source,
		tableName:   table.Name,
		tableSchema: table.Schema,
		cdcConfig:   r.cdcConfig,
	}
	if opts.Streaming && opts.CDCManagedState && opts.CDCSnapshotReplace {
		snapshot.progressKeys = resumableSnapshotKeys(table)
	}
	if len(snapshot.progressKeys) > 0 {
		snapshot.progress = source.CDCSnapshotProgress{Boundary: FormatLSN(lsn), Incarnation: table.Incarnation, SchemaFingerprint: table.SchemaFingerprint}
		if resume != nil {
			var key []string
			if err := json.Unmarshal([]byte(resume.Key), &key); err != nil || len(key) != len(snapshot.progressKeys) {
				return fmt.Errorf("invalid snapshot resume key %q for %s", resume.Key, table.Name)
			}
			snapshot.progress.Boundary, snapshot.resumeAfter = resume.Boundary, key
		}
	} else if resume != nil {
		return fmt.Errorf("snapshot of %s cannot resume: its keys are not resumable", table.Name)
	}

	// Create a wrapper channel that adds TableName to results
	tableResults := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(tableResults)
		if err := snapshot.readWithSnapshot(ctx, snapshotName, lsn, tableResults, opts.ReadOptions); err != nil {
			_ = sendResult(ctx, tableResults, source.RecordBatchResult{Err: err})
		}
	}()

	// Forward results with TableName set
	for result := range tableResults {
		if result.Err != nil {
			return result.Err
		}
		result.TableName = table.Name
		if err := sendResult(ctx, results, result); err != nil {
			drainRecordBatchResults(tableResults)
			return err
		}
	}

	return nil
}

// resumableSnapshotKeys returns the keys a snapshot of table can be ordered
// and resumed by: all of them non-null, or none.
func resumableSnapshotKeys(table source.SourceTableInfo) []string {
	if len(table.PrimaryKeys) == 0 || table.Schema == nil {
		return nil
	}
	nullable := make(map[string]bool, len(table.Schema.Columns))
	for _, col := range table.Schema.Columns {
		nullable[col.Name] = col.Nullable
	}
	for _, key := range table.PrimaryKeys {
		if isNullable, ok := nullable[key]; !ok || isNullable {
			return nil
		}
	}
	return table.PrimaryKeys
}

// streamChanges streams WAL changes, filtering per-table based on resume LSNs.
// For batch mode, barrier identifies where this replication phase ends.
//
// Small WAL transactions (single-row changes) are accumulated per-table and
// flushed as larger batches to avoid overwhelming the destination with
// single-row writes.
//
// In streaming mode it periodically re-checks the source for tables the stream
// should cover but doesn't, and watches for mid-stream schema changes surfaced
// by the decoder. Either way it flushes what it has and returns a signal so
// the caller can rebuild the stream; a nil signal means normal termination.
func (r *MultiTableCDCReader) streamChanges(ctx context.Context, startLSN pglogrepl.LSN, barrier batchBarrier, slotName string, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) (retSignal *streamSignal, retErr error) {
	config.Debug("[CDC] Multi-table streaming from LSN: %s", startLSN)

	cdcConfigWithSlot := r.cdcConfig
	cdcConfigWithSlot.SlotName = slotName

	// Create multi-table replicator
	repl, err := NewMultiTableReplicator(r.source, r.tables, cdcConfigWithSlot, startLSN, r, opts.Streaming, barrier)
	if err != nil {
		return nil, fmt.Errorf("failed to create replicator: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, repl.Close(ctx)) }()

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	schemas := make(map[string]*schema.TableSchema, len(r.tables))
	for _, t := range r.tables {
		schemas[t.Name] = t.Schema
	}
	accum := newBatchAccumulator(batchSize, schemas)
	defer func() { retErr = errors.Join(retErr, accum.toast.close()) }()
	if opts.Streaming {
		accum.durable = r.source.pos.Committed
	}

	// In streaming mode, batches carry a CommitToken (safe LSN) so the pipeline
	// confirms the slot only after the data is durable. The barrier is inactive in
	// streaming mode, so the barrier exit below never triggers.
	var token tokenFunc
	if opts.Streaming {
		token = func() any { return checkpointCommitToken(safeCommitLSN(repl, accum)) }
	}
	defer func() {
		if !opts.Streaming {
			return
		}
		var schemaErr *SchemaChangedError
		if retErr != nil && !errors.As(retErr, &schemaErr) {
			return
		}
		if retSignal == nil && schemaErr == nil {
			return
		}
		schemaErrors, err := accum.flushForSchemaRebuild(ctx, results, token, schemaErr)
		if err != nil {
			retSignal, retErr = nil, err
			return
		}
		if retSignal == nil {
			retSignal = &streamSignal{}
		}
		for _, schemaErr := range schemaErrors {
			output.Statusf("Schema change detected on table %s (column %q %s); rebuilding stream around the new schema\n", schemaErr.Table, schemaErr.Column, schemaErr.Reason)
			retSignal.changedTables = append(retSignal.changedTables, schemaErr.Table)
			retSignal.schemaErrors = append(retSignal.schemaErrors, schemaErr)
		}
		retErr = nil
	}()

	// lastIdleToken is the highest LSN already handed to the pipeline via a bare
	// idle commit token, so we only emit one when the caught-up position has
	// actually advanced (instead of every 100ms idle tick).
	var lastIdleToken pglogrepl.LSN
	lastHeartbeat := time.Now()
	lastBusyToken := time.Now()

	// New-table discovery runs on a timer in streaming mode only: a batch run
	// picks up new tables at its next start, where Connect has already
	// reconciled the publication.
	discover := opts.Streaming && r.cdcConfig.DiscoverInterval > 0
	lastDiscover := time.Now()

	for {
		select {
		case <-ctx.Done():
			config.Debug("[CDC] Context cancelled, stopping stream")
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				accum.discard()
				return nil, loss
			}
			if err := accum.flushAllContext(ctx, results, token); err != nil {
				return nil, err
			}
			return nil, ctx.Err()
		default:
		}

		if discover && time.Since(lastDiscover) >= r.cdcConfig.DiscoverInterval && discoveryReady(repl) {
			lastDiscover = time.Now()
			newNames, changed, reincarnated, derr := r.detectTableChanges(ctx)
			switch {
			case derr != nil:
				// Discovery is best-effort; a transient catalog query failure
				// must not kill the stream.
				config.Debug("[CDC] New-table discovery failed: %v", derr)
			case len(newNames) > 0 || len(changed) > 0 || len(reincarnated) > 0:
				// Flush emitted work and hand off for a rebuild. An in-flight
				// transaction still inside the decoder is dropped with the
				// replicator; the slot cannot have confirmed past it, so the
				// rebuilt stream re-decodes it.
				return &streamSignal{newTables: newNames, changedTables: changed, reincarnatedTables: reincarnated}, nil
			}
		}

		// Get the next transaction's changes (may span multiple tables)
		groups, hadActivity, err := repl.NextChanges(ctx)
		if err != nil {
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				accum.discard()
				return nil, loss
			}
			var schemaErr *SchemaChangedError
			if opts.Streaming && errors.As(err, &schemaErr) {
				// Mid-stream DDL: flush emitted work, drop batches still buffered
				// inside the replicator, and hand off for a rebuild around the
				// refreshed schema.
				// The transaction that tripped this was never emitted, so the
				// rebuilt stream re-decodes it in full. Batch runs skip this and
				// surface the error instead — a restart heals them the same way.
				output.Statusf("Schema change detected on table %s (column %q %s); rebuilding stream around the new schema\n", schemaErr.Table, schemaErr.Column, schemaErr.Reason)
				return &streamSignal{changedTables: []string{schemaErr.Table}, schemaErrors: []*SchemaChangedError{schemaErr}}, nil
			}
			var reincarnationErr *TableReincarnatedError
			if opts.Streaming && errors.As(err, &reincarnationErr) {
				relationID, parseErr := strconv.ParseUint(reincarnationErr.Current, 10, 32)
				if parseErr != nil {
					return nil, fmt.Errorf("invalid PostgreSQL relation incarnation %q: %w", reincarnationErr.Current, parseErr)
				}
				return &streamSignal{
					reincarnatedTables: []string{reincarnationErr.Table},
					historicalRelIDs:   map[string][]uint32{reincarnationErr.Table: {uint32(relationID)}},
				}, nil
			}
			_ = accum.flushAllContext(ctx, results, token)
			return nil, fmt.Errorf("failed to get next changes: %w", err)
		}

		if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
			accum.discard()
			return nil, loss
		}
		for _, g := range groups {
			accum.add(g.TableName, g.Changes, g.LSN)
		}

		// Flush tables that have accumulated enough rows
		if err := accum.flushReadyContext(ctx, results, token); err != nil {
			return nil, err
		}
		if opts.Streaming && hadActivity {
			if err := accum.flushStaleContext(ctx, results, token, streamingMaxAge(opts.ReadOptions), time.Now()); err != nil {
				return nil, err
			}
			lastIdleToken, err = maybeEmitBusyCommitToken(ctx, repl, accum, results, lastIdleToken, &lastBusyToken)
			if err != nil {
				return nil, err
			}
		}

		if barrier.active() && repl.BarrierReached() {
			_, pending := repl.PendingLowWater()
			if !pending {
				decodedBarrierLSN := repl.BarrierLSN()
				if decodedBarrierLSN == 0 {
					return nil, fmt.Errorf("batch barrier was reached without a decoded replication LSN")
				}
				config.Debug("[CDC] Batch mode: decoded logical barrier at %s", decodedBarrierLSN)
				if err := accum.flushAllContext(ctx, results, token); err != nil {
					return nil, err
				}
				// Stop the WAL receiver before the keepalive goroutine takes
				// over the replication connection — they must never use it
				// concurrently. Close is idempotent for the deferred call.
				if err := repl.Close(ctx); err != nil {
					return nil, err
				}
				if barrier.nonce != "" {
					warnIfBarrierLSNsDiverge(barrier.sqlLSN, decodedBarrierLSN)
				}
				// The keepalive holds the walsender open until finalization.
				r.source.checkpointBatchBarrier(ctx, decodedBarrierLSN, startLSN, slotName)
				return nil, nil
			}
		}

		// When idle (no WAL activity), flush any pending batches
		if !hadActivity {
			if err := accum.flushAllContext(ctx, results, token); err != nil {
				return nil, err
			}
			// Confirm the caught-up position so the slot advances over WAL that
			// carried no rows for us; otherwise an idle stream's lag grows forever.
			if opts.Streaming {
				lastHeartbeat = maybeEmitStreamHeartbeat(ctx, repl, lastHeartbeat)
				lastIdleToken, err = emitIdleCommitToken(ctx, repl, accum, results, lastIdleToken)
				if err != nil {
					return nil, err
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func discoveryReady(repl interface {
	PendingLowWater() (pglogrepl.LSN, bool)
},
) bool {
	_, pending := repl.PendingLowWater()
	return !pending
}

func (r *MultiTableCDCReader) dropSlot(ctx context.Context, slotName string) error {
	_, err := r.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slotName)
	return err
}

func (r *MultiTableCDCReader) getSlotLSN(ctx context.Context, slotName string) (pglogrepl.LSN, error) {
	return getSlotConfirmedLSN(ctx, r.source.queryPool, slotName)
}

func getSlotConfirmedLSN(ctx context.Context, pool *pgxpool.Pool, slotName string) (pglogrepl.LSN, error) {
	var lsnStr string
	err := pool.QueryRow(ctx, `
		SELECT confirmed_flush_lsn::text
		FROM pg_replication_slots
		WHERE slot_name = $1
	`, slotName).Scan(&lsnStr)
	if err != nil {
		return 0, err
	}
	return pglogrepl.ParseLSN(lsnStr)
}

// generateMultiTableSlotName creates a slot name for multi-table CDC.
func generateMultiTableSlotName(publication, suffix string) string {
	name := fmt.Sprintf("ingestr_mt_%s", publication)
	if suffix != "" {
		name = fmt.Sprintf("%s_%s", name, suffix)
	}
	return truncateSlotName(name)
}

func generateLegacyMultiTableSlotName(publication, suffix string) string {
	name := fmt.Sprintf("ingestr_mt_%s", publication)
	if suffix != "" {
		name = fmt.Sprintf("%s_%s", name, suffix)
	}
	return truncateLegacySlotName(name)
}

// ShouldFilterChange checks if a change should be filtered based on per-table resume LSN.
func (r *MultiTableCDCReader) ShouldFilterChange(tableName string, changeLSN pglogrepl.LSN) bool {
	r.mu.RLock()
	lastProcessed, ok := r.processedLSNs[tableName]
	snapshotBoundary := r.snapshotBoundaries[tableName]
	r.mu.RUnlock()

	if !ok {
		// Table not seen before, accept all changes
		return false
	}

	// Strict less-than is required at the snapshot boundary: the snapshot is
	// exported at the slot's consistent point, and the first transaction that
	// commits afterwards carries a BEGIN LSN equal to that point. It is NOT in
	// the snapshot, so it must be streamed. On resume, processedLSN is the
	// BEGIN LSN of the last already-applied transaction; re-emitting it is
	// harmless because the merge is idempotent by primary key (at-least-once).
	if changeLSN != lastProcessed {
		return changeLSN < lastProcessed
	}
	if snapshotBoundary {
		return false
	}
	for _, table := range r.tables {
		if table.Name == tableName {
			return len(table.PrimaryKeys) == 0
		}
	}
	return false
}
