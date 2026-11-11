package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// loadSnapshotChunk streams pages into staging, then publishes the snapshot
// before accepting any WAL. Only the subsequent CDCFlush acknowledges progress.
func (l *flushLoop) loadSnapshotChunk(ctx context.Context, records <-chan source.RecordBatchResult, begin source.RecordBatchResult) error {
	if begin.Batch != nil {
		begin.Batch.Release()
		return fmt.Errorf("snapshot chunk start must not contain rows")
	}
	st, ok := l.tableState(begin.TableName)
	if !ok || st.stagingTable == "" || !st.isCDC {
		return fmt.Errorf("snapshot chunk requires a CDC staging table for %q", begin.TableName)
	}
	if l.buffered > 0 || l.tokenDirty {
		if err := l.flush(ctx); err != nil {
			return err
		}
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	expectedIncarnation := ""
	if l.opts.StateManager != nil {
		name := l.stateSourceTable(begin.TableName)
		if name == "" {
			var err error
			name, err = l.opts.StateManager.SourceTableForDestination(st.destTable)
			if err != nil {
				return err
			}
		}
		if err := l.opts.StateManager.BindDestinationIncarnation(ctx, name, st.destTable); err != nil {
			return err
		}
		var err error
		expectedIncarnation, err = l.opts.StateManager.BoundDestinationIncarnation(name)
		if err != nil {
			return err
		}
	}
	l.chunkTiming = begin.CDCSnapshotChunk.Timing
	g, gctx := errgroup.WithContext(ctx)
	pages := make(chan source.RecordBatchResult)
	var end *source.CDCSnapshotChunk
	var rowCount int64
	g.Go(func() error {
		defer close(pages)
		for {
			select {
			case <-gctx.Done():
				return gctx.Err()
			case res, open := <-records:
				if !open {
					return fmt.Errorf("source ended before snapshot chunk completion")
				}
				if res.Err != nil {
					if res.Batch != nil {
						res.Batch.Release()
					}
					return fmt.Errorf("snapshot chunk: %w", res.Err)
				}
				if res.CDCSnapshotChunkEnd != nil && res.Batch == nil && res.TableName == begin.TableName {
					end = res.CDCSnapshotChunkEnd
					return nil
				}
				if res.Batch == nil || res.TableName != begin.TableName || res.CommitToken != nil || res.CDCFlush || res.Truncate || res.CDCSnapshotChunk != nil || res.CDCSnapshotChunkEnd != nil || res.TableInfo != nil || res.SnapshotInvalidation != nil {
					if res.Batch != nil {
						res.Batch.Release()
					}
					return fmt.Errorf("unexpected result inside snapshot chunk for %q", begin.TableName)
				}
				rowCount += res.Batch.NumRows()
				select {
				case pages <- res:
				case <-gctx.Done():
					res.Batch.Release()
					return gctx.Err()
				}
			}
		}
	})
	var bytes uint64
	loads := (<-chan source.RecordBatchResult)(pages)
	if col, ok := loadTimestampColumn(st.schema); ok {
		loads = transformer.Wrap(loads, transformer.NewLoadTimestamp(col, time.Now().UTC().Truncate(time.Microsecond)))
	}
	if col, ok := runIDColumn(st.schema); ok {
		loads = transformer.Wrap(loads, transformer.NewRunID(col, strings.ReplaceAll(uuid.NewString(), "-", "")))
	}
	loads = countRecordBatchBytes(loads, &bytes)
	loadStart := time.Now()
	g.Go(func() error {
		return l.dest.WriteParallel(gctx, loads, destination.WriteOptions{
			Table: st.stagingTable, Schema: st.schema, StagingTable: true,
			Parallelism: l.cfg.EffectiveDestinationParallelism(), StagingBucket: l.cfg.StagingBucket,
			LoaderFileSize: l.cfg.LoaderFileSize, LoaderFileFormat: l.cfg.LoaderFileFormat,
		})
	})
	err := g.Wait()
	drainAndRelease(loads)
	if l.chunkTiming != nil {
		l.chunkTiming.LoadNanos.Add(int64(time.Since(loadStart)))
	}
	if err != nil {
		return fmt.Errorf("failed to stage snapshot chunk for %s: %w", st.destTable, err)
	}
	if end == nil {
		return fmt.Errorf("snapshot chunk is missing its end boundary")
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if rowCount == 0 {
		return nil
	}
	if end.DeferMerge {
		if !st.snapshotWindowOpen {
			st.snapshotWindowAfter = append([]string(nil), end.After...)
			st.snapshotWindowOpen = true
		}
		st.snapshotRows += rowCount
		st.snapshotBytes += bytes
		return connectorLeaseLoss(ctx)
	}
	opts := mergeStagingOptions(l.dest, st.stagingTable, st.destTable, st.primaryKeys, st.schema, st.incrementalKey, st.incrementalPredicate, expectedIncarnation, false)
	opts.CDCSnapshot = true
	// Bounds describe source values; masks and column overrides can change
	// those values or their ordering before they reach staging.
	rangeAfter := end.After
	if st.snapshotWindowOpen {
		rangeAfter = st.snapshotWindowAfter
	}
	if end.OrderedIntegerKeys && len(l.cfg.Mask) == 0 && strings.TrimSpace(l.cfg.Columns) == "" {
		opts.CDCSnapshotRange = &destination.CDCSnapshotRange{After: rangeAfter, Last: end.Last}
	}
	mergeStart := time.Now()
	err = l.dest.MergeTable(ctx, opts)
	if l.chunkTiming != nil {
		l.chunkTiming.MergeNanos.Add(int64(time.Since(mergeStart)))
	}
	if err != nil {
		return fmt.Errorf("failed to merge snapshot chunk for %s: %w", st.destTable, err)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := l.resetStaging(ctx, st); err != nil {
		return fmt.Errorf("failed to reset snapshot staging for %s: %w", st.destTable, err)
	}
	st.snapshotWindowOpen = false
	st.snapshotWindowAfter = nil
	st.snapshotRows += rowCount
	st.snapshotBytes += bytes
	return connectorLeaseLoss(ctx)
}
