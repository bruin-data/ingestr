package postgres_cdc

import (
	"context"
	"fmt"
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
				resumeLSN, err := pglogrepl.ParseLSN(opts.CDCResumeLSN)
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
	// For batch mode, get the current WAL LSN as our target before starting
	var targetLSN pglogrepl.LSN
	if r.cdcConfig.Mode == ModeBatch {
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

	repl, err := NewReplicator(r.source, r.tableName, r.tableSchema, cdcConfigWithSlot, startLSN)
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

	return streamLoop(ctx, repl, r.cdcConfig.Mode, targetLSN, batchSize, accum, results)
}

// batchReplicator is the subset of *Replicator that streamLoop depends on,
// allowing the accumulation loop to be tested without a live connection.
type batchReplicator interface {
	NextBatch(ctx context.Context, batchSize int) (arrow.RecordBatch, bool, error)
	CurrentLSN() pglogrepl.LSN
}

// streamLoop pulls batches from repl and feeds them through the accumulator,
// flushing only when the replicator reports a genuine idle period. Treating
// every batch-less call as idle would flush after every transaction and defeat
// accumulation entirely (see hadActivity in Replicator.NextBatch).
func streamLoop(ctx context.Context, repl batchReplicator, mode CDCMode, targetLSN pglogrepl.LSN, batchSize int, accum *batchAccumulator, results chan<- source.RecordBatchResult) error {
	for {
		select {
		case <-ctx.Done():
			config.Debug("[CDC] Context cancelled, stopping stream")
			accum.flushAll(results)
			return ctx.Err()
		default:
		}

		batch, hadActivity, err := repl.NextBatch(ctx, batchSize)
		if err != nil {
			accum.flushAll(results)
			return fmt.Errorf("failed to get next batch: %w", err)
		}

		if batch != nil && batch.NumRows() > 0 {
			accum.add("", batch)
		}

		accum.flushReady(results)

		// For batch mode, check if we've caught up to the target LSN
		if mode == ModeBatch && targetLSN > 0 {
			currentLSN := repl.CurrentLSN()
			if currentLSN >= targetLSN {
				config.Debug("[CDC] Batch mode: reached target LSN %s (current: %s)", targetLSN, currentLSN)
				accum.flushAll(results)
				return nil
			}
		}

		// When idle (no WAL activity), flush any pending batches. Buffered
		// non-commit messages and keepalives count as activity, so we keep
		// accumulating across transactions instead of flushing each one.
		if !hadActivity {
			accum.flushAll(results)
			time.Sleep(100 * time.Millisecond)
		}
	}
}
