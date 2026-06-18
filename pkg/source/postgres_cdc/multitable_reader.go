package postgres_cdc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
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
	// Reconcile each table's effective merge keys into its schema so the decoder,
	// compaction, and unchanged-TOAST fill all key off the same keys the
	// destination merge uses. SourceTableInfo.PrimaryKeys is authoritative and
	// may carry user-provided keys that the schema's detected keys lack.
	for i := range tables {
		if len(tables[i].PrimaryKeys) > 0 && tables[i].Schema != nil {
			tables[i].Schema.PrimaryKeys = tables[i].PrimaryKeys
		}
	}
	return &MultiTableCDCReader{
		source:        src,
		tables:        tables,
		cdcConfig:     cdcConfig,
		resumeLSNs:    resumeLSNs,
		processedLSNs: make(map[string]pglogrepl.LSN),
		slotSuffix:    slotSuffix,
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
				config.Debug("[CDC] Multi-table: resuming from LSN %s with per-table filtering", minResumeLSN)
			}
		}

		if needsSnapshot {
			config.Debug("[CDC] Multi-table: performing full snapshot (some tables have no existing data)")
			if err := r.executeSnapshot(ctx, results, opts); err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
				return
			}
		}

		var startLSN pglogrepl.LSN
		if needsSnapshot {
			// After snapshot, we need to get the slot's current LSN
			var err error
			startLSN, err = r.getSlotLSN(ctx, slotName)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to get slot LSN: %w", err)}
				return
			}
		} else {
			startLSN = minResumeLSN
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

		if err := r.streamChanges(ctx, startLSN, targetLSN, slotName, results, opts); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)}
			return
		}
	}()

	return results, nil
}

// determineResumeStrategy determines if we need a full snapshot or can resume.
// Returns (needsSnapshot, minLSN).
//
// Only requires that at least one table has a resume LSN. Tables missing from
// the resume map (e.g. empty tables that produced no data during snapshot) will
// accept all WAL changes via ShouldFilterChange, which returns false for
// unknown tables.
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

	// Log tables without resume LSNs. These were likely empty during the
	// previous snapshot. ShouldFilterChange returns false for tables not in
	// processedLSNs, so all WAL changes for them will be accepted.
	for _, table := range r.tables {
		if _, ok := r.resumeLSNs[table.Name]; !ok {
			config.Debug("[CDC] Table %s has no resume LSN (empty in destination), will accept all WAL changes", table.Name)
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

	// Snapshot each table using the exported snapshot
	for _, table := range r.tables {
		config.Debug("[CDC] Snapshotting table: %s", table.Name)
		if err := r.snapshotTable(ctx, table, result.SnapshotName, snapshotLSN, results, opts); err != nil {
			return fmt.Errorf("failed to snapshot table %s: %w", table.Name, err)
		}
		// Initialize processed LSN for this table
		r.updateProcessedLSN(table.Name, snapshotLSN)
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
func (r *MultiTableCDCReader) streamChanges(ctx context.Context, startLSN pglogrepl.LSN, targetLSN pglogrepl.LSN, slotName string, results chan<- source.RecordBatchResult, opts source.MultiTableReadOptions) error {
	config.Debug("[CDC] Multi-table streaming from LSN: %s", startLSN)

	cdcConfigWithSlot := r.cdcConfig
	cdcConfigWithSlot.SlotName = slotName

	// Create multi-table replicator
	repl, err := NewMultiTableReplicator(r.source, r.tables, cdcConfigWithSlot, startLSN, r, opts.Streaming)
	if err != nil {
		return fmt.Errorf("failed to create replicator: %w", err)
	}
	defer func() { _ = repl.Close(ctx) }()

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	accum := newBatchAccumulator(batchSize)
	pkByTable := make(map[string][]string, len(r.tables))
	for _, t := range r.tables {
		pks := t.PrimaryKeys
		if len(pks) == 0 && t.Schema != nil {
			pks = t.Schema.PrimaryKeys
		}
		pkByTable[t.Name] = pks
	}
	accum.transform = func(tableName string, batch arrow.RecordBatch) arrow.RecordBatch {
		return forwardFillUnchanged(batch, pkByTable[tableName])
	}

	// In streaming mode, batches carry a CommitToken (safe LSN) so the pipeline
	// confirms the slot only after the data is durable. targetLSN is 0 in
	// streaming mode, so the catch-up exit below never triggers.
	var token tokenFunc
	if opts.Streaming {
		token = func() any { return safeCommitLSN(repl, accum) }
	}

	for {
		select {
		case <-ctx.Done():
			config.Debug("[CDC] Context cancelled, stopping stream")
			accum.flushAll(results, token)
			return ctx.Err()
		default:
		}

		// Get next batch (may be from any table)
		batch, tableName, lsn, hadActivity, err := repl.NextBatch(ctx, batchSize)
		if err != nil {
			accum.flushAll(results, token)
			return fmt.Errorf("failed to get next batch: %w", err)
		}

		if batch != nil && batch.NumRows() > 0 {
			accum.add(tableName, batch, lsn)
		}

		// Flush tables that have accumulated enough rows
		accum.flushReady(results, token)

		// For batch mode, check if we've caught up to the target LSN
		if targetLSN > 0 {
			currentLSN := repl.CurrentLSN()
			if currentLSN >= targetLSN {
				config.Debug("[CDC] Batch mode: reached target LSN %s (current: %s)", targetLSN, currentLSN)
				accum.flushAll(results, token)
				return nil
			}
		}

		// When idle (no WAL activity), flush any pending batches
		if !hadActivity {
			accum.flushAll(results, token)
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (r *MultiTableCDCReader) dropSlot(ctx context.Context, slotName string) error {
	_, err := r.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slotName)
	return err
}

func (r *MultiTableCDCReader) getSlotLSN(ctx context.Context, slotName string) (pglogrepl.LSN, error) {
	var lsnStr string
	err := r.source.queryPool.QueryRow(ctx, `
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
