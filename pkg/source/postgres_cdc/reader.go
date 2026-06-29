package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

type CDCReader struct {
	source      *PostgresCDCSource
	tableName   string
	tableSchema *schema.TableSchema
	cdcConfig   CDCConfig
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

		// Check if we should resume from a specific LSN (incremental mode)
		if opts.CDCResumeLSN != "" {
			slotName := r.cdcConfig.SlotName
			if slotName == "" {
				slotName = generateSlotName(r.tableName, r.cdcConfig.Publication, opts.CDCSlotSuffix)
			}

			// Verify the slot still exists before trying to resume
			if _, exists, _ := checkSlotExists(ctx, r.source.queryPool, slotName); exists {
				config.Debug("[CDC] Resuming from LSN: %s (skipping snapshot)", opts.CDCResumeLSN)
				resumeLSN, err := parseStoredPostgresLSN(opts.CDCResumeLSN)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to parse resume LSN: %w", err)}
					return
				}

				if err := r.streamChanges(ctx, resumeLSN, slotName, results, opts); err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)}
					return
				}
				return
			}

			config.Debug("[CDC] Replication slot %s not found, falling back to full snapshot", slotName)
		}

		// Full mode: Phase 1: Snapshot
		config.Debug("[CDC] Starting snapshot phase for %s", r.tableName)
		snapshot, err := NewSnapshot(r.source, r.tableName, r.tableSchema, r.cdcConfig, opts.CDCSlotSuffix)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to create snapshot: %w", err)}
			return
		}

		snapshotLSN, slotName, err := snapshot.Execute(ctx, results, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("snapshot failed: %w", err)}
			return
		}

		config.Debug("[CDC] Snapshot completed at LSN: %s", snapshotLSN)

		// Phase 2: Stream changes from snapshot LSN using the same slot
		if err := r.streamChanges(ctx, snapshotLSN, slotName, results, opts); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("streaming failed: %w", err)}
			return
		}
	}()

	return results, nil
}

func (r *CDCReader) streamChanges(ctx context.Context, startLSN pglogrepl.LSN, slotName string, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
	// --stream forces continuous mode regardless of the URI ?mode= param.
	mode := r.cdcConfig.Mode
	if opts.Streaming {
		mode = ModeStream
	}

	// For batch mode, get the current WAL LSN as our target before starting
	var targetLSN pglogrepl.LSN
	if mode == ModeBatch {
		var lsnStr string
		err := r.source.queryPool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsnStr)
		if err != nil {
			return fmt.Errorf("failed to get current WAL LSN: %w", err)
		}
		targetLSN, err = pglogrepl.ParseLSN(lsnStr)
		if err != nil {
			return fmt.Errorf("failed to parse current WAL LSN: %w", err)
		}
		config.Debug("[CDC] Batch mode: will stream until LSN %s", targetLSN)
	}

	config.Debug("[CDC] Starting streaming from LSN: %s", startLSN)

	// Use the slot created during snapshot
	cdcConfigWithSlot := r.cdcConfig
	cdcConfigWithSlot.SlotName = slotName

	repl, err := NewReplicator(r.source, r.tableName, r.tableSchema, cdcConfigWithSlot, startLSN, opts.Streaming)
	if err != nil {
		return fmt.Errorf("failed to create replicator: %w", err)
	}
	defer func() { _ = repl.Close(ctx) }()

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	accum := newBatchAccumulator(batchSize)
	pkNames := r.tableSchema.PrimaryKeys
	accum.transform = func(_ string, batch arrow.RecordBatch) arrow.RecordBatch {
		return forwardFillUnchanged(batch, pkNames)
	}

	err = streamLoop(ctx, repl, mode, targetLSN, batchSize, accum, results, opts.Streaming)
	if err == nil && mode == ModeBatch {
		// Record the caught-up position so FinalizeBatch can confirm it to the
		// slot once the destination write is durable.
		caughtUp := repl.CurrentLSN()
		r.source.recordCaughtUpLSN(caughtUp)
		// Keep the walsender alive while the destination drains the results
		// channel. FinalizeBatch will stop it before sending the final
		// WALFlush-bearing standby update.
		r.source.startKeepalive(ctx, caughtUp)
	}
	return err
}

func parseStoredPostgresLSN(raw string) (pglogrepl.LSN, error) {
	normalized := strings.Trim(raw, " \t\r\n\x00'\"")
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
	NextBatch(ctx context.Context, batchSize int) (arrow.RecordBatch, pglogrepl.LSN, bool, error)
	CurrentLSN() pglogrepl.LSN
	PendingLowWater() (pglogrepl.LSN, bool)
}

// streamLoop pulls batches from repl and feeds them through the accumulator,
// flushing only when the replicator reports a genuine idle period. Treating
// every batch-less call as idle would flush after every transaction and defeat
// accumulation entirely (see hadActivity in Replicator.NextBatch).
//
// When streaming, each flushed batch carries a CommitToken (a safe LSN) so the
// pipeline can confirm the replication slot only after the data is durable.
func streamLoop(ctx context.Context, repl batchReplicator, mode CDCMode, targetLSN pglogrepl.LSN, batchSize int, accum *batchAccumulator, results chan<- source.RecordBatchResult, streaming bool) error {
	var token tokenFunc
	if streaming {
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

		batch, lsn, hadActivity, err := repl.NextBatch(ctx, batchSize)
		if err != nil {
			accum.flushAll(results, token)
			return fmt.Errorf("failed to get next batch: %w", err)
		}

		if batch != nil && batch.NumRows() > 0 {
			accum.add("", batch, lsn)
		}

		accum.flushReady(results, token)

		// For batch mode, check if we've caught up to the target LSN
		if mode == ModeBatch && targetLSN > 0 {
			currentLSN := repl.CurrentLSN()
			if currentLSN >= targetLSN {
				config.Debug("[CDC] Batch mode: reached target LSN %s (current: %s)", targetLSN, currentLSN)
				accum.flushAll(results, token)
				return nil
			}
		}

		// When idle (no WAL activity), flush any pending batches. Buffered
		// non-commit messages and keepalives count as activity, so we keep
		// accumulating across transactions instead of flushing each one.
		if !hadActivity {
			accum.flushAll(results, token)
			time.Sleep(100 * time.Millisecond)
		}
	}
}
