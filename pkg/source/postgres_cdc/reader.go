package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

type CDCReader struct {
	source            *PostgresCDCSource
	tableName         string
	tableSchema       *schema.TableSchema
	cdcConfig         CDCConfig
	allowedUnknown    map[string]struct{}
	incarnation       string
	schemaFingerprint string
}

func NewCDCReader(src *PostgresCDCSource, tableName string, tableSchema *schema.TableSchema, cdcConfig CDCConfig) *CDCReader {
	return &CDCReader{
		source:      src,
		tableName:   tableName,
		tableSchema: tableSchema,
		cdcConfig:   cdcConfig,
	}
}

func (r *CDCReader) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if r.cdcConfig.Mode == ModeBatch && !opts.Streaming {
			if err := validateBatchBarrierSupport(r.source.serverVersion); err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
		}
		incarnation, err := r.source.TableIncarnation(ctx, r.tableName)
		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
			return
		}
		r.incarnation = incarnation
		fingerprint, err := r.source.TableSchemaFingerprint(ctx, r.tableName)
		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
			return
		}
		r.schemaFingerprint = fingerprint
		replacementSnapshot := false
		if opts.CDCResumeLSN != "" && resumeMetadataChanged(opts.CDCResumeIncarnation, opts.CDCResumeSchemaFingerprint, incarnation, fingerprint) {
			config.Debug("[CDC] Resume metadata changed for %s; replacing its snapshot", r.tableName)
			opts.CDCResumeLSN = ""
			replacementSnapshot = true
			tableSchema, err := getTableSchema(ctx, r.source.queryPool, r.tableName)
			if err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			tableSchema = addCDCColumns(tableSchema)
			if len(r.tableSchema.PrimaryKeys) > 0 {
				tableSchema.PrimaryKeys = r.tableSchema.PrimaryKeys
			}
			r.tableSchema = tableSchema
		}

		// Check if we should resume from a specific LSN (incremental mode)
		if opts.CDCResumeLSN != "" {
			slotName := r.cdcConfig.SlotName
			var slotCandidates []string
			if slotName == "" {
				slotName = generateSlotName(r.tableName, r.cdcConfig.Publication, opts.CDCSlotSuffix)
				if opts.CDCLegacySlotSuffix != "" {
					candidate := generateLegacySlotName(r.tableName, r.cdcConfig.Publication, opts.CDCLegacySlotSuffix)
					if legacySlotNameUnambiguous(candidate, opts.CDCLegacySlotSuffix) {
						slotCandidates = append(slotCandidates, candidate)
					}
				}
			}

			// Verify the slot still exists before trying to resume
			resolvedSlot, exists, legacy, err := resolveResumeSlotCandidates(ctx, r.source.queryPool, slotName, slotCandidates...)
			if err != nil {
				_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
				return
			}
			if exists {
				slotName = resolvedSlot
				if legacy {
					r.source.markLegacySlotInUse(slotName)
				}
				if !legacy {
					waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
				}
				config.Debug("[CDC] Resuming from LSN: %s (skipping snapshot)", opts.CDCResumeLSN)
				resumeLSN, err := parseStoredPostgresLSN(opts.CDCResumeLSN)
				if err != nil {
					_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("failed to parse resume LSN: %w", err)})
					return
				}

				if err := r.streamChanges(ctx, resumeLSN, slotName, results, opts, false); err != nil {
					_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)})
					return
				}
				return
			}

			config.Debug("[CDC] Replication slot %s not found, falling back to full snapshot", slotName)
			replacementSnapshot = true
		}

		// Full mode: Phase 1: Snapshot
		if replacementSnapshot && opts.Streaming {
			if err := sendResult(ctx, results, source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
				TableName: r.tableName, Incarnation: incarnation,
			}}); err != nil {
				return
			}
		}
		config.Debug("[CDC] Starting snapshot phase for %s", r.tableName)
		snapshot, err := NewSnapshot(r.source, r.tableName, r.tableSchema, r.cdcConfig, opts.CDCSlotSuffix)
		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("failed to create snapshot: %w", err)})
			return
		}

		snapshotLSN, slotName, err := snapshot.Execute(ctx, results, opts)
		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)})
			return
		}

		config.Debug("[CDC] Snapshot completed at LSN: %s", snapshotLSN)
		incarnation, err = r.source.TableIncarnation(ctx, r.tableName)
		if err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: err})
			return
		}
		r.source.recordSnapshotState(r.tableName, snapshotLSN, incarnation, r.schemaFingerprint)
		r.incarnation = incarnation
		if opts.Streaming {
			token := snapshotCommitToken(snapshotLSN, map[string]string{
				r.tableName: FormatLSN(snapshotLSN),
			})
			token.SnapshotIncarnations = map[string]string{r.tableName: incarnation}
			token.SnapshotSchemas = map[string]string{r.tableName: r.schemaFingerprint}
			if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: token}); err != nil {
				return
			}
		}
		if stopAfterBatchSnapshot(r.cdcConfig.Mode, opts.Streaming) {
			// Keep snapshot rows and post-snapshot partial-TOAST changes in
			// separate destination merges. The slot retains the WAL for the
			// next batch run, which resumes from this exact snapshot boundary.
			r.source.recordCaughtUpLSN(snapshotLSN, slotName, false)
			return
		}

		// Phase 2: Stream changes from snapshot LSN using the same slot
		if err := r.streamChanges(ctx, snapshotLSN, slotName, results, opts, true); err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)})
			return
		}
	}()

	return results, nil
}

func stopAfterBatchSnapshot(mode CDCMode, streaming bool) bool {
	return mode == ModeBatch && !streaming
}

func resumeMetadataChanged(expectedIncarnation, expectedFingerprint, currentIncarnation, currentFingerprint string) bool {
	return expectedIncarnation != "" && expectedIncarnation != currentIncarnation ||
		expectedFingerprint != "" && expectedFingerprint != currentFingerprint
}

func (r *CDCReader) streamChanges(ctx context.Context, startLSN pglogrepl.LSN, slotName string, results chan<- source.RecordBatchResult, opts source.ReadOptions, snapshotBoundary bool) error {
	// --stream forces continuous mode regardless of the URI ?mode= param.
	mode := r.cdcConfig.Mode
	if opts.Streaming {
		mode = ModeStream
	}

	for {
		barrierNonce := ""
		var barrierLSN pglogrepl.LSN
		if mode == ModeBatch {
			var err error
			barrierNonce, barrierLSN, err = emitBatchBarrier(ctx, r.source.queryPool)
			if err != nil {
				return err
			}
			config.Debug("[CDC] Batch mode: emitted logical-decoding barrier at %s", barrierLSN)
		}
		err := r.runStream(ctx, startLSN, slotName, mode, barrierNonce, barrierLSN, results, opts, snapshotBoundary)
		var schemaErr *SchemaChangedError
		var reincarnationErr *TableReincarnatedError
		if err == nil || !opts.Streaming || !errors.As(err, &schemaErr) && !errors.As(err, &reincarnationErr) {
			return err
		}

		// Mid-stream DDL: refresh the schema, announce it so the consumer can
		// evolve the destination, and resume from the slot's confirmed
		// position. The transaction that tripped this was never emitted, so
		// the rebuilt stream re-decodes it against the refreshed schema and
		// none of its data is lost. Batch runs skip this and surface the
		// error instead — a restart heals them the same way.
		if schemaErr != nil {
			fmt.Printf("Schema change detected on table %s (column %q %s); rebuilding stream around the new schema\n", schemaErr.Table, schemaErr.Column, schemaErr.Reason)
		} else {
			fmt.Printf("Source table %s was dropped and recreated; replacing its destination snapshot\n", reincarnationErr.Table)
		}
		startLSN, err = r.rebuildForTableChange(ctx, slotName, results, schemaErr, opts)
		if err != nil {
			return fmt.Errorf("failed to rebuild stream after schema change: %w", err)
		}
		snapshotBoundary = true
	}
}

// runStream runs one replication stream until it terminates: batch mode caught
// up, the context was cancelled, or the replicator failed.
func (r *CDCReader) runStream(ctx context.Context, startLSN pglogrepl.LSN, slotName string, mode CDCMode, barrierNonce string, barrierLSN pglogrepl.LSN, results chan<- source.RecordBatchResult, opts source.ReadOptions, snapshotBoundary bool) (retErr error) {
	config.Debug("[CDC] Starting streaming from LSN: %s", startLSN)

	// Use the slot created during snapshot
	cdcConfigWithSlot := r.cdcConfig
	cdcConfigWithSlot.SlotName = slotName

	repl, err := NewReplicator(r.source, r.tableName, r.tableSchema, cdcConfigWithSlot, startLSN, opts.Streaming, barrierNonce)
	if err != nil {
		return fmt.Errorf("failed to create replicator: %w", err)
	}
	if err := repl.ExpectTableIncarnation(r.incarnation); err != nil {
		return err
	}
	repl.SetSnapshotBoundary(snapshotBoundary)
	repl.ExpectTableSchemaFingerprint(r.schemaFingerprint)
	repl.decoder.AllowUnknownRelationColumns(r.allowedUnknown)
	defer func() { retErr = errors.Join(retErr, repl.Close(ctx)) }()

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	accum := newBatchAccumulator(batchSize, map[string]*schema.TableSchema{"": r.tableSchema})

	err = streamLoop(ctx, repl, mode, batchSize, accum, results, opts.Streaming)
	if err == nil && mode == ModeBatch {
		// Record the caught-up position so FinalizeBatch can confirm it to the
		// slot once the destination write is durable.
		caughtUp := batchCaughtUpLSN(repl.CurrentLSN(), barrierLSN)
		r.source.recordCaughtUpLSN(caughtUp, slotName, true)
		// Keep the walsender alive while the destination drains the results
		// channel. FinalizeBatch will stop it before sending the final
		// WALFlush-bearing standby update.
		r.source.startKeepalive(ctx, caughtUp, startLSN)
	}
	return err
}

// rebuildForSchemaChange re-fetches the table's schema after mid-stream DDL,
// announces it (RecordBatchResult.TableInfo) so the streaming consumer can
// evolve the destination before new-shape batches arrive, and reopens the
// replication connection for a fresh StartReplication on the same slot.
// Returns the LSN to resume from — the slot's confirmed position, so the
// transaction that surfaced the change is re-decoded in full.
func (r *CDCReader) rebuildForTableChange(ctx context.Context, slotName string, results chan<- source.RecordBatchResult, schemaErr *SchemaChangedError, opts source.ReadOptions) (pglogrepl.LSN, error) {
	if schemaErr == nil {
		if err := r.source.reconcilePublication(ctx); err != nil {
			return 0, fmt.Errorf("failed to reconcile publication after table recreation: %w", err)
		}
	}
	tableSchema, err := getTableSchema(ctx, r.source.queryPool, r.tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to refresh schema for table %s: %w", r.tableName, err)
	}
	tableSchema = addCDCColumns(tableSchema)
	// Keep the merge keys the run started with: they may carry user-provided
	// keys that re-detection would drop, and the decoder, compaction, and
	// unchanged-TOAST fill must keep keying off the same columns.
	if len(r.tableSchema.PrimaryKeys) > 0 {
		tableSchema.PrimaryKeys = r.tableSchema.PrimaryKeys
	}
	r.tableSchema = tableSchema
	if schemaErr != nil {
		if r.allowedUnknown == nil {
			r.allowedUnknown = make(map[string]struct{})
		}
		for _, column := range schemaErr.Columns() {
			r.allowedUnknown[column] = struct{}{}
		}
	}

	incarnation, err := r.source.TableIncarnation(ctx, r.tableName)
	if err != nil {
		return 0, err
	}
	r.incarnation = incarnation
	fingerprint, err := r.source.TableSchemaFingerprint(ctx, r.tableName)
	if err != nil {
		return 0, err
	}
	r.schemaFingerprint = fingerprint
	table := source.SourceTableInfo{Name: r.tableName, Schema: tableSchema, PrimaryKeys: tableSchema.PrimaryKeys, Incarnation: incarnation, SchemaFingerprint: fingerprint}
	snapshotLSN, err := r.resnapshotTable(ctx, slotName, table, results, opts)
	if err != nil {
		return 0, err
	}

	if err := r.source.reconnectReplication(ctx); err != nil {
		return 0, err
	}
	waitReplicationSlotReleased(ctx, r.source.queryPool, slotName)
	return snapshotLSN, nil
}

func (r *CDCReader) resnapshotTable(ctx context.Context, slotName string, table source.SourceTableInfo, results chan<- source.RecordBatchResult, opts source.ReadOptions) (pglogrepl.LSN, error) {
	if !opts.CDCSnapshotReplace {
		return 0, fmt.Errorf("source table schema changed, but the destination cannot replace its snapshot safely")
	}
	conn, err := r.source.openReplicationConn(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(ctx) }()
	created, err := pglogrepl.CreateReplicationSlot(ctx, conn, backfillSlotName(slotName), "pgoutput", pglogrepl.CreateReplicationSlotOptions{
		Temporary: true, SnapshotAction: "EXPORT_SNAPSHOT", Mode: pglogrepl.LogicalReplication,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create schema backfill slot: %w", err)
	}
	snapshotLSN, err := pglogrepl.ParseLSN(created.ConsistentPoint)
	if err != nil {
		return 0, fmt.Errorf("failed to parse schema backfill LSN: %w", err)
	}
	if err := sendResult(ctx, results, source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{
		TableName: table.Name, Incarnation: table.Incarnation,
	}}); err != nil {
		return 0, err
	}
	if err := sendResult(ctx, results, source.RecordBatchResult{TableName: table.Name, TableInfo: &table}); err != nil {
		return 0, err
	}
	snapshot := &Snapshot{source: r.source, tableName: table.Name, tableSchema: table.Schema, cdcConfig: r.cdcConfig}
	tableResults := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(tableResults)
		if err := snapshot.readWithSnapshot(ctx, created.SnapshotName, snapshotLSN, tableResults, opts); err != nil {
			_ = sendResult(ctx, tableResults, source.RecordBatchResult{Err: err})
		}
	}()
	for result := range tableResults {
		if result.Err != nil {
			return 0, result.Err
		}
		if err := sendResult(ctx, results, result); err != nil {
			drainRecordBatchResults(tableResults)
			return 0, err
		}
	}
	r.source.recordSnapshotState(table.Name, snapshotLSN, table.Incarnation, table.SchemaFingerprint)
	token := snapshotCommitToken(snapshotLSN, map[string]string{table.Name: FormatLSN(snapshotLSN)})
	token.SnapshotIncarnations = map[string]string{table.Name: table.Incarnation}
	token.SnapshotSchemas = map[string]string{table.Name: table.SchemaFingerprint}
	if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: token}); err != nil {
		return 0, err
	}
	return snapshotLSN, nil
}

func parseStoredPostgresLSN(raw string) (pglogrepl.LSN, error) {
	normalized := strings.Trim(raw, " \t\r\n\x00'\"")
	if parts := strings.Split(normalized, "/"); len(parts) == 3 && len(parts[2]) == 16 && isHexLSN(parts[2]) {
		normalized = strings.Join(parts[:2], "/")
	}
	if len(normalized) == 16 && strings.IndexByte(normalized, '/') == -1 && isHexLSN(normalized) {
		normalized = normalized[:8] + "/" + normalized[8:]
	}

	lsn, err := pglogrepl.ParseLSN(normalized)
	if err != nil {
		return 0, fmt.Errorf("failed to parse LSN %q: %w", raw, err)
	}
	return lsn, nil
}

func isHexLSN(value string) bool {
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

// batchReplicator is the subset of *Replicator that streamLoop depends on,
// allowing the accumulation loop to be tested without a live connection.
type batchReplicator interface {
	NextChanges(ctx context.Context) ([]Change, pglogrepl.LSN, bool, error)
	CurrentLSN() pglogrepl.LSN
	BarrierReached() bool
	PendingLowWater() (pglogrepl.LSN, bool)
}

type singleTableChangeFilter interface {
	ShouldFilterChange(changeLSN pglogrepl.LSN) bool
}

// streamLoop pulls decoded changes from repl and feeds them through the
// accumulator, flushing only when the replicator reports a genuine idle
// period. Treating every change-less call as idle would flush after every
// transaction and defeat accumulation entirely (see hadActivity in
// Replicator.NextChanges).
//
// When streaming, each flushed batch carries a CommitToken (a safe LSN) so the
// pipeline can confirm the replication slot only after the data is durable.
func streamLoop(ctx context.Context, repl batchReplicator, mode CDCMode, batchSize int, accum *batchAccumulator, results chan<- source.RecordBatchResult, streaming bool) error {
	var token tokenFunc
	if streaming {
		token = func() any { return checkpointCommitToken(safeCommitLSN(repl, accum)) }
	}

	// lastIdleToken is the highest LSN already handed to the pipeline via a bare
	// idle commit token, so we only emit one when the caught-up position has
	// actually advanced (instead of every 100ms idle tick).
	var lastIdleToken pglogrepl.LSN
	lastHeartbeat := time.Now()

	for {
		select {
		case <-ctx.Done():
			config.Debug("[CDC] Context cancelled, stopping stream")
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				accum.discard()
				return loss
			}
			if err := accum.flushAllContext(ctx, results, token); err != nil {
				return err
			}
			return ctx.Err()
		default:
		}

		changes, lsn, hadActivity, err := repl.NextChanges(ctx)
		if err != nil {
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				accum.discard()
				return loss
			}
			_ = accum.flushAllContext(ctx, results, token)
			return fmt.Errorf("failed to get next changes: %w", err)
		}

		if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
			accum.discard()
			return loss
		}
		if filter, ok := repl.(singleTableChangeFilter); ok && filter.ShouldFilterChange(lsn) {
			changes = nil
		}
		accum.add("", changes, lsn)

		if err := accum.flushReadyContext(ctx, results, token); err != nil {
			return err
		}

		if mode == ModeBatch && repl.BarrierReached() {
			_, pending := repl.PendingLowWater()
			if !pending {
				config.Debug("[CDC] Batch mode: decoded logical barrier at %s", repl.CurrentLSN())
				if err := accum.flushAllContext(ctx, results, token); err != nil {
					return err
				}
				return nil
			}
		}

		// When idle (no WAL activity), flush any pending batches. Buffered
		// non-commit messages and keepalives count as activity, so we keep
		// accumulating across transactions instead of flushing each one.
		if !hadActivity {
			if err := accum.flushAllContext(ctx, results, token); err != nil {
				return err
			}
			// Confirm the caught-up position so the slot advances over WAL that
			// carried no rows for us; otherwise an idle stream's lag grows forever.
			if streaming {
				lastHeartbeat = maybeEmitStreamHeartbeat(ctx, repl, lastHeartbeat)
				lastIdleToken = emitIdleCommitToken(ctx, repl, accum, results, lastIdleToken)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
