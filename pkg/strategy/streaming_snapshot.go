package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/metrics"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/google/uuid"
)

func canDirectStreamSnapshot(dest destination.Destination, cfg *config.IngestConfig, st *streamTableState) bool {
	return destination.SupportsDirectCDCSnapshot(dest) && st.isCDC && len(st.primaryKeys) > 0 &&
		st.incrementalPredicate == "" && !cfg.KeepStaging && !cfg.CDCChunkedSnapshot &&
		len(cfg.Mask) == 0 && strings.TrimSpace(cfg.Columns) == ""
}

func (l *flushLoop) canLoadContinuousSnapshot(res source.RecordBatchResult) bool {
	// Concurrent table snapshots interleave pages; keep their shared flush
	// buffer so a table switch does not force a commit after each page.
	if len(l.tables) != 1 {
		return false
	}
	st, ok := l.tableState(res.TableName)
	return ok && res.Batch != nil && res.CDCSnapshot && st.stagingTable != "" && !st.snapshotUseStaging &&
		canDirectStreamSnapshot(l.dest, l.cfg, st) &&
		continuousSnapshotResult(res, res.TableName, l.stateSourceTable(res.TableName))
}

// A snapshot segment ends before any event that needs the flush loop's ordering
// or acknowledgement rules. Range ends only flush the writer's current request.
func continuousSnapshotResult(res source.RecordBatchResult, recordTable, sourceTable string) bool {
	if res.Err != nil || res.TableName != recordTable || res.Truncate || res.CDCWALTruncate ||
		res.CDCFlush || res.TableInfo != nil || res.SnapshotInvalidation != nil || res.SnapshotReset ||
		res.CDCSnapshotChunk != nil || res.CDCSnapshotChunkEnd != nil || res.CDCChunkTiming != nil ||
		res.CDCSnapshotPosition != "" || res.CDCSnapshotResumed || res.CDCSnapshotSpoolIdentity != "" ||
		res.CDCChunkedSnapshotIdentity != "" || res.DurableCommitID != "" || res.DurableCommitPosition != "" ||
		res.DurableCheckpointID != "" || res.DurableCheckpointPosition != "" || res.DurableCheckpointTable != "" {
		return false
	}
	if res.Batch != nil && !res.CDCSnapshot {
		return false
	}
	if res.CommitToken == nil {
		return res.Batch != nil || res.CDCSnapshotRangeEnd
	}
	token, ok := res.CommitToken.(source.CDCStateCommitToken)
	if !ok || token.SourceCommitToken != nil || token.Position != "" || token.DataBatchID != "" ||
		len(token.SnapshotPositions) != 0 || len(token.SnapshotIncarnations) != 0 || len(token.SnapshotSchemas) != 0 ||
		len(token.SnapshotProgress) != 1 {
		return false
	}
	_, ok = token.SnapshotProgress[sourceTable]
	return ok
}

func (l *flushLoop) loadContinuousSnapshot(ctx context.Context, records <-chan source.RecordBatchResult, first source.RecordBatchResult) (*source.RecordBatchResult, error) {
	if l.buffered > 0 || l.tokenDirty {
		if err := l.flush(ctx); err != nil {
			first.Batch.Release()
			return nil, err
		}
	}
	st, _ := l.tableState(first.TableName)
	sourceTable := l.stateSourceTable(first.TableName)
	opts := destination.WriteOptions{
		Table: st.destTable, Schema: st.schema, PrimaryKeys: st.primaryKeys,
		Parallelism: l.cfg.EffectiveDestinationParallelism(), CDCSnapshot: true, SkipCDCResume: true,
		StagingBucket: l.cfg.StagingBucket, LoaderFileSize: l.cfg.LoaderFileSize, LoaderFileFormat: l.cfg.LoaderFileFormat,
	}
	if l.opts.StateManager != nil {
		manager := l.opts.StateManager
		if sourceTable == "" {
			var err error
			sourceTable, err = manager.SourceTableForDestination(st.destTable)
			if err != nil {
				first.Batch.Release()
				return nil, err
			}
		}
		if err := manager.BindDestinationIncarnation(ctx, sourceTable, st.destTable); err != nil {
			first.Batch.Release()
			return nil, err
		}
		var err error
		opts.CDCExpectedIncarnation, err = manager.BoundDestinationIncarnation(sourceTable)
		if err != nil {
			first.Batch.Release()
			return nil, err
		}
		progressWriter := cdcSnapshotBatchWriter{manager: manager, sourceTable: sourceTable, table: mergeTableParams{DestTable: st.destTable}}
		opts.CDCSnapshotProgress = progressWriter.persistProgress
	}
	eligible, err := destination.CanWriteDirectCDCSnapshot(ctx, l.dest, st.destTable, st.schema, opts.CDCExpectedIncarnation)
	if err != nil {
		first.Batch.Release()
		return nil, fmt.Errorf("check direct CDC snapshot target %s: %w", st.destTable, err)
	}
	if !eligible {
		st.snapshotUseStaging = true
		return nil, l.processResult(ctx, first)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		first.Batch.Release()
		return nil, err
	}

	start := time.Now()
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pages := make(chan source.RecordBatchResult)
	done := make(chan cdcSnapshotBatchBoundary, 1)
	var rows int64
	go func() {
		defer close(pages)
		res := first
		for {
			if res.Batch != nil {
				rows += res.Batch.NumRows()
			}
			select {
			case pages <- res:
			case <-writeCtx.Done():
				releaseCDCSnapshotBatchResult(&res)
				done <- cdcSnapshotBatchBoundary{err: writeCtx.Err()}
				return
			}
			select {
			case <-writeCtx.Done():
				done <- cdcSnapshotBatchBoundary{err: writeCtx.Err()}
				return
			case next, ok := <-records:
				if !ok {
					done <- cdcSnapshotBatchBoundary{}
					return
				}
				res = next
			}
			if !continuousSnapshotResult(res, first.TableName, sourceTable) {
				done <- cdcSnapshotBatchBoundary{next: &res}
				return
			}
		}
	}()
	loads := (<-chan source.RecordBatchResult)(pages)
	if col, ok := loadTimestampColumn(st.schema); ok {
		loads = transformer.Wrap(loads, transformer.NewLoadTimestamp(col, start.UTC().Truncate(time.Microsecond)))
	}
	if col, ok := runIDColumn(st.schema); ok {
		loads = transformer.Wrap(loads, transformer.NewRunID(col, strings.ReplaceAll(uuid.NewString(), "-", "")))
	}
	var bytes uint64
	loads = countRecordBatchBytes(loads, &bytes)
	writeErr := l.dest.WriteParallel(writeCtx, loads, opts)
	cancel()
	boundary := <-done
	drainAndRelease(loads)
	if writeErr == nil {
		writeErr = boundary.err
	}
	if writeErr == nil {
		writeErr = connectorLeaseLoss(ctx)
	}
	if writeErr != nil {
		releaseCDCSnapshotBatchResult(boundary.next)
		return nil, fmt.Errorf("streaming snapshot: failed to write table %s: %w", st.destTable, writeErr)
	}
	displayName := first.TableName
	if displayName == "" {
		displayName = st.destTable
	}
	metrics.RecordSync(map[string]metrics.SyncStats{displayName: {Rows: rows, Bytes: bytes}}, time.Now())
	output.Statusf("[STREAM] %s | loaded %d snapshot rows for %s in %s\n", time.Now().Format("15:04:05"), rows, displayName, time.Since(start).Round(time.Millisecond))
	return boundary.next, nil
}

func (l *flushLoop) prepareDeferredStaging(ctx context.Context, st *streamTableState) error {
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := l.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: st.stagingTable, Schema: st.schema, DropFirst: true,
		CDCMode: st.isCDC, CDCKeys: st.primaryKeys, PartitionBy: st.partitionBy,
		ClusterBy: st.clusterBy, ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare CDC staging table %s: %w", st.stagingTable, err)
	}
	st.stagingDeferred = false
	output.Statusf("[STREAM] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), st.stagingTable)
	return connectorLeaseLoss(ctx)
}
