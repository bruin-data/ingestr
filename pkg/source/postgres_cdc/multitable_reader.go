package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// MultiTableCDCReader handles reading CDC changes from multiple tables.
type MultiTableCDCReader struct {
	source        *PostgresCDCSource
	tables        []source.SourceTableInfo
	cdcConfig     CDCConfig
	resumeLSNs    map[string]string        // per-table resume LSNs
	processedLSNs map[string]pglogrepl.LSN // in-memory tracking of last processed LSN per table
	mu            sync.RWMutex             // protects processedLSNs
	slotSuffix    string
}

func NewMultiTableCDCReader(src *PostgresCDCSource, tables []source.SourceTableInfo, cdcConfig CDCConfig, resumeLSNs map[string]string, slotSuffix string) *MultiTableCDCReader {
	reconcileTableKeys(tables)
	return &MultiTableCDCReader{
		source:        src,
		tables:        tables,
		cdcConfig:     cdcConfig,
		resumeLSNs:    resumeLSNs,
		processedLSNs: make(map[string]pglogrepl.LSN),
		slotSuffix:    slotSuffix,
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
}

// Read starts reading CDC changes from all tables in the publication.
func (r *MultiTableCDCReader) Read(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 16)

	go func() {
		defer close(results)

		// Determine if we need snapshot or can resume
		needsSnapshot, minResumeLSN := r.determineResumeStrategy()

		// Resolve slot name
		slotName := r.cdcConfig.SlotName
		if slotName == "" {
			slotName = generateMultiTableSlotName(r.cdcConfig.Publication, r.slotSuffix)
		}

		// If resume strategy says no snapshot needed, verify the slot actually exists
		if !needsSnapshot {
			if _, exists, _ := checkSlotExists(ctx, r.source.queryPool, slotName); !exists {
				config.Debug("[CDC] Multi-table: replication slot %s not found, falling back to full snapshot", slotName)
				needsSnapshot = true
			} else {
				waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
				config.Debug("[CDC] Multi-table: resuming from LSN %s with per-table filtering", minResumeLSN)
			}
		}

		var startLSN pglogrepl.LSN
		if needsSnapshot {
			config.Debug("[CDC] Multi-table: performing full snapshot (some tables have no existing data)")
			if err := r.executeSnapshot(ctx, results, opts); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
				return
			}
			var err error
			startLSN, err = r.getSlotLSN(ctx, slotName)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get slot LSN: %w", err)}
				return
			}
		} else {
			startLSN = minResumeLSN
			// Tables without resume state are not covered by the resumed stream:
			// their pre-existing rows were never snapshotted (typically tables
			// created since the previous run). Backfill them before streaming.
			if newTables := r.tablesWithoutResumeState(); len(newTables) > 0 {
				if err := r.backfillTables(ctx, slotName, newTables, results, opts); err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("backfill failed: %w", err)}
					return
				}
			}
		}

		// For batch mode, get the current WAL LSN as our target.
		// --stream forces continuous mode regardless of the URI ?mode= param.
		var targetLSN pglogrepl.LSN
		if r.cdcConfig.Mode == ModeBatch && !opts.Streaming {
			var err error
			targetLSN, err = r.getCurrentWALLSN(ctx)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get current WAL LSN: %w", err)}
				return
			}
			config.Debug("[CDC] Batch mode: will stream until LSN %s", targetLSN)
		}

		for {
			signal, err := r.streamChanges(ctx, startLSN, targetLSN, slotName, results, opts)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)}
				return
			}
			if signal == nil {
				return
			}
			startLSN, err = r.rebuildStream(ctx, slotName, signal, results, opts)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to rebuild stream: %w", err)}
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
func (r *MultiTableCDCReader) backfillTables(ctx context.Context, slotName string, tables []source.SourceTableInfo, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	conn, err := r.source.openReplicationConn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	tempSlot := backfillSlotName(slotName)
	created, err := pglogrepl.CreateReplicationSlot(
		ctx,
		conn,
		tempSlot,
		"pgoutput",
		pglogrepl.CreateReplicationSlotOptions{
			Temporary:      true,
			SnapshotAction: "EXPORT_SNAPSHOT",
			Mode:           pglogrepl.LogicalReplication,
		},
	)
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
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(snapshotParallelism(opts))
	for i := range tables {
		table := tables[i]
		tableInfo := &tables[i]
		g.Go(func() error {
			fmt.Printf("Backfilling new table %s before streaming its changes\n", table.Name)
			if opts.Streaming {
				// Announce the table so a consumer that prepared its per-table
				// state upfront can provision the destination before data
				// arrives. Per-table ordering (announcement before that
				// table's batches) is preserved inside this goroutine.
				results <- source.RecordBatchResult{TableName: table.Name, TableInfo: tableInfo}
			}
			if err := r.snapshotTable(gctx, table, created.SnapshotName, snapshotLSN, results, opts); err != nil {
				return fmt.Errorf("failed to backfill table %s: %w", table.Name, err)
			}
			r.updateProcessedLSN(table.Name, snapshotLSN)
			r.source.recordSnapshotPosition(table.Name, snapshotLSN)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if opts.Streaming {
		positions := make(map[string]string, len(tables))
		for _, table := range tables {
			positions[table.Name] = FormatLSN(snapshotLSN)
		}
		results <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotPositions: positions}}
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
	newTables     []string
	changedTables []string
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
	if len(signal.newTables) > 0 {
		fmt.Printf("New table(s) detected on source: %s; adding to CDC stream\n", strings.Join(signal.newTables, ", "))
		if err := r.source.reconcilePublication(ctx); err != nil {
			return 0, fmt.Errorf("failed to reconcile publication: %w", err)
		}
	}

	tables, err := r.source.GetTables(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to refresh table list: %w", err)
	}
	reconcileTableKeys(tables)

	prevSchemas := make(map[string]*schema.TableSchema, len(r.tables))
	for _, t := range r.tables {
		prevSchemas[t.Name] = t.Schema
	}
	var added []source.SourceTableInfo
	for _, t := range tables {
		if _, ok := prevSchemas[t.Name]; !ok {
			added = append(added, t)
		}
	}
	r.tables = tables

	if len(added) > 0 {
		if err := r.backfillTables(ctx, slotName, added, results, opts); err != nil {
			return 0, err
		}
	}

	// Announce every previously known table whose refreshed schema no longer
	// matches the shape the consumer prepared — not just the table that
	// tripped the rebuild. GetTables refreshed ALL schemas, so the rebuilt
	// decoders emit the new shape for every table that had DDL, whether or not
	// its Relation message has been seen yet. (Tables added above were
	// announced by backfillTables.)
	for _, idx := range shapeChangedTables(prevSchemas, r.tables) {
		t := &r.tables[idx]
		results <- source.RecordBatchResult{TableName: t.Name, TableInfo: t}
	}

	if err := r.source.reconnectReplication(ctx); err != nil {
		return 0, err
	}
	waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
	return r.getSlotLSN(ctx, slotName)
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
func (r *MultiTableCDCReader) detectNewTables(ctx context.Context) ([]string, error) {
	eligible, err := r.source.listEligibleTableNames(ctx)
	if err != nil {
		return nil, err
	}
	return diffNewTables(r.tables, eligible), nil
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
	result, err := pglogrepl.CreateReplicationSlot(
		ctx,
		r.source.replConn,
		slotName,
		"pgoutput",
		pglogrepl.CreateReplicationSlotOptions{
			Temporary:      false,
			SnapshotAction: "EXPORT_SNAPSHOT",
			Mode:           pglogrepl.LogicalReplication,
		},
	)
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
			if err := r.snapshotTable(gctx, table, result.SnapshotName, snapshotLSN, results, opts); err != nil {
				return fmt.Errorf("failed to snapshot table %s: %w", table.Name, err)
			}
			// Initialize processed LSN for this table
			r.updateProcessedLSN(table.Name, snapshotLSN)
			r.source.recordSnapshotPosition(table.Name, snapshotLSN)
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
		results <- source.RecordBatchResult{CommitToken: snapshotCommitToken(snapshotLSN, positions)}
	}
	return nil
}

// snapshotTable reads all data from a single table using the snapshot.
func (r *MultiTableCDCReader) snapshotTable(ctx context.Context, table source.SourceTableInfo, snapshotName string, lsn pglogrepl.LSN, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	snapshot := &Snapshot{
		source:      r.source,
		tableName:   table.Name,
		tableSchema: table.Schema,
		cdcConfig:   r.cdcConfig,
	}

	// Create a wrapper channel that adds TableName to results
	tableResults := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(tableResults)
		if err := snapshot.readWithSnapshot(ctx, snapshotName, lsn, tableResults, opts.ReadOptions); err != nil {
			tableResults <- source.RecordBatchResult{Err: err}
		}
	}()

	// Forward results with TableName set
	for result := range tableResults {
		if result.Err != nil {
			return result.Err
		}
		result.TableName = table.Name
		results <- result
	}

	return nil
}

// streamChanges streams WAL changes, filtering per-table based on resume LSNs.
// For batch mode, targetLSN specifies the LSN to stop at (0 means no target).
//
// Small WAL transactions (single-row changes) are accumulated per-table and
// flushed as larger batches to avoid overwhelming the destination with
// single-row writes.
//
// In streaming mode it periodically re-checks the source for tables the stream
// should cover but doesn't, and watches for mid-stream schema changes surfaced
// by the decoder. Either way it flushes what it has and returns a signal so
// the caller can rebuild the stream; a nil signal means normal termination.
func (r *MultiTableCDCReader) streamChanges(ctx context.Context, startLSN pglogrepl.LSN, targetLSN pglogrepl.LSN, slotName string, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) (*streamSignal, error) {
	config.Debug("[CDC] Multi-table streaming from LSN: %s", startLSN)

	cdcConfigWithSlot := r.cdcConfig
	cdcConfigWithSlot.SlotName = slotName

	// Create multi-table replicator
	repl, err := NewMultiTableReplicator(r.source, r.tables, cdcConfigWithSlot, startLSN, r, opts.Streaming)
	if err != nil {
		return nil, fmt.Errorf("failed to create replicator: %w", err)
	}
	defer func() { _ = repl.Close(ctx) }()

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	schemas := make(map[string]*schema.TableSchema, len(r.tables))
	for _, t := range r.tables {
		schemas[t.Name] = t.Schema
	}
	accum := newBatchAccumulator(batchSize, schemas)

	// In streaming mode, batches carry a CommitToken (safe LSN) so the pipeline
	// confirms the slot only after the data is durable. targetLSN is 0 in
	// streaming mode, so the catch-up exit below never triggers.
	var token tokenFunc
	if opts.Streaming {
		token = func() any { return checkpointCommitToken(safeCommitLSN(repl, accum)) }
	}

	// lastIdleToken is the highest LSN already handed to the pipeline via a bare
	// idle commit token, so we only emit one when the caught-up position has
	// actually advanced (instead of every 100ms idle tick).
	var lastIdleToken pglogrepl.LSN

	// New-table discovery runs on a timer in streaming mode only: a batch run
	// picks up new tables at its next start, where Connect has already
	// reconciled the publication.
	discover := opts.Streaming && r.cdcConfig.DiscoverInterval > 0
	lastDiscover := time.Now()

	for {
		select {
		case <-ctx.Done():
			config.Debug("[CDC] Context cancelled, stopping stream")
			if err := accum.flushAll(results, token); err != nil {
				return nil, err
			}
			return nil, ctx.Err()
		default:
		}

		if discover && time.Since(lastDiscover) >= r.cdcConfig.DiscoverInterval {
			lastDiscover = time.Now()
			newNames, derr := r.detectNewTables(ctx)
			switch {
			case derr != nil:
				// Discovery is best-effort; a transient catalog query failure
				// must not kill the stream.
				config.Debug("[CDC] New-table discovery failed: %v", derr)
			case len(newNames) > 0:
				// Flush emitted work and hand off for a rebuild. An in-flight
				// transaction still inside the decoder is dropped with the
				// replicator; the slot cannot have confirmed past it, so the
				// rebuilt stream re-decodes it.
				if err := accum.flushAll(results, token); err != nil {
					return nil, err
				}
				return &streamSignal{newTables: newNames}, nil
			}
		}

		// Get the next transaction's changes (may span multiple tables)
		groups, hadActivity, err := repl.NextChanges(ctx)
		if err != nil {
			var schemaErr *SchemaChangedError
			if opts.Streaming && errors.As(err, &schemaErr) {
				// Mid-stream DDL: flush emitted work, drop batches still buffered
				// inside the replicator, and hand off for a rebuild around the
				// refreshed schema.
				// The transaction that tripped this was never emitted, so the
				// rebuilt stream re-decodes it in full. Batch runs skip this and
				// surface the error instead — a restart heals them the same way.
				fmt.Printf("Schema change detected on table %s (column %q %s); rebuilding stream around the new schema\n", schemaErr.Table, schemaErr.Column, schemaErr.Reason)
				if err := accum.flushAll(results, token); err != nil {
					return nil, err
				}
				return &streamSignal{changedTables: []string{schemaErr.Table}}, nil
			}
			_ = accum.flushAll(results, token)
			return nil, fmt.Errorf("failed to get next changes: %w", err)
		}

		for _, g := range groups {
			accum.add(g.TableName, g.Changes, g.LSN)
		}

		// Flush tables that have accumulated enough rows
		if err := accum.flushReady(results, token); err != nil {
			return nil, err
		}

		// For batch mode, check if we've caught up to the target LSN
		if targetLSN > 0 {
			currentLSN := repl.CurrentLSN()
			if currentLSN >= targetLSN {
				config.Debug("[CDC] Batch mode: reached target LSN %s (current: %s)", targetLSN, currentLSN)
				if err := accum.flushAll(results, token); err != nil {
					return nil, err
				}
				// Record the caught-up position so FinalizeBatch can confirm it
				// to the slot once the destination write is durable.
				r.source.recordCaughtUpLSN(currentLSN)
				// Stop the WAL receiver before the keepalive goroutine takes
				// over the replication connection — they must never use it
				// concurrently. Close is idempotent for the deferred call.
				_ = repl.Close(ctx)
				// Keep the walsender alive while the destination drains the
				// results channel. FinalizeBatch will stop it before sending
				// the final WALFlush-bearing standby update.
				r.source.startKeepalive(ctx, currentLSN)
				return nil, nil
			}
		}

		// When idle (no WAL activity), flush any pending batches
		if !hadActivity {
			if err := accum.flushAll(results, token); err != nil {
				return nil, err
			}
			// Confirm the caught-up position so the slot advances over WAL that
			// carried no rows for us; otherwise an idle stream's lag grows forever.
			if opts.Streaming {
				lastIdleToken = emitIdleCommitToken(ctx, repl, accum, results, lastIdleToken)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
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

func (r *MultiTableCDCReader) getCurrentWALLSN(ctx context.Context) (pglogrepl.LSN, error) {
	var lsnStr string
	err := r.source.queryPool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsnStr)
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

// ShouldFilterChange checks if a change should be filtered based on per-table resume LSN.
func (r *MultiTableCDCReader) ShouldFilterChange(tableName string, changeLSN pglogrepl.LSN) bool {
	r.mu.RLock()
	lastProcessed, ok := r.processedLSNs[tableName]
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
	return changeLSN < lastProcessed
}
