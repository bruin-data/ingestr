package strategy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/sync/errgroup"
)

func directCDCSnapshotBatchEnabled(cfg *config.IngestConfig, dest destination.Destination) bool {
	return destination.SupportsDirectCDCSnapshot(dest) && !cfg.KeepStaging && !cfg.CDCChunkedSnapshot &&
		len(cfg.Mask) == 0 && cfg.Columns == "" && cfg.IncrementalPredicate == ""
}

type cdcSnapshotBatchWriter struct {
	dest                destination.Destination
	cfg                 *config.IngestConfig
	manager             *CDCStateManager
	sourceTable         string
	expectedIncarnation string
	table               mergeTableParams
	stagingPrepared     bool
	truncateTarget      bool
	snapshotBoundary    string
	retireSnapshot      bool
	snapshotDirect      *bool
	snapshotStaging     bool
}

func (w *cdcSnapshotBatchWriter) write(ctx context.Context, records <-chan source.RecordBatchResult, cancelInput context.CancelFunc) (retErr error) {
	defer func() {
		if retErr != nil {
			cancelInput()
			drainAndReleaseUntil(records, streamAbortDrainTimeout)
		}
	}()
	var pending *source.RecordBatchResult
	for {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			releaseCDCSnapshotBatchResult(pending)
			return err
		}
		var res source.RecordBatchResult
		if pending != nil {
			res, pending = *pending, nil
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case next, ok := <-records:
				if !ok {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := w.finishSnapshot(ctx); err != nil {
						return err
					}
					return w.finishStaging(ctx)
				}
				res = next
			}
		}
		if res.Err != nil {
			releaseCDCSnapshotBatchResult(&res)
			return res.Err
		}
		if res.CDCSnapshotChunk != nil || res.CDCSnapshotChunkEnd != nil {
			releaseCDCSnapshotBatchResult(&res)
			return fmt.Errorf("unexpected chunk boundary in batch CDC snapshot of %s", w.sourceTable)
		}
		if res.Truncate {
			releaseCDCSnapshotBatchResult(&res)
			if err := w.finishSnapshot(ctx); err != nil {
				return err
			}
			if res.CDCSnapshotPosition != "" && !res.CDCWALTruncate {
				if err := w.finishStaging(ctx); err != nil {
					return err
				}
				if err := w.beginSnapshot(ctx, res); err != nil {
					return err
				}
			} else {
				if w.stagingPrepared {
					if err := destination.ApplyTruncate(ctx, w.dest, w.table.StagingTable); err != nil {
						return err
					}
				}
				w.truncateTarget = true
			}
			continue
		}
		if res.Batch == nil {
			if token, ok := res.CommitToken.(source.CDCStateCommitToken); ok && w.manager != nil && w.snapshotBoundary != "" {
				if err := w.persistProgress(ctx, token); err != nil {
					return err
				}
			}
			continue
		}
		if res.CDCSnapshot {
			if w.snapshotBoundary == "" {
				res.Batch.Release()
				return fmt.Errorf("CDC snapshot rows for %s arrived without a replacement boundary", w.sourceTable)
			}
			if w.snapshotDirect == nil {
				eligible, err := destination.CanWriteDirectCDCSnapshot(ctx, w.dest, w.table.DestTable, w.table.Schema, w.expectedIncarnation)
				if err != nil {
					res.Batch.Release()
					return fmt.Errorf("check direct CDC snapshot support for %s: %w", w.sourceTable, err)
				}
				w.snapshotDirect = &eligible
			}
		} else {
			if err := w.finishSnapshot(ctx); err != nil {
				res.Batch.Release()
				return err
			}
		}
		directSnapshot := res.CDCSnapshot && *w.snapshotDirect
		if !directSnapshot {
			if err := w.prepareStaging(ctx); err != nil {
				res.Batch.Release()
				return err
			}
			w.snapshotStaging = res.CDCSnapshot
		}
		opts := destination.WriteOptions{
			Table: w.table.DestTable, Schema: w.table.Schema, PrimaryKeys: w.table.PrimaryKeys,
			Parallelism: w.cfg.EffectiveDestinationParallelism(), CDCSnapshot: directSnapshot,
			SkipCDCResume:          res.CDCSnapshot,
			CDCExpectedIncarnation: w.expectedIncarnation,
			StagingBucket:          w.cfg.StagingBucket, LoaderFileSize: w.cfg.LoaderFileSize, LoaderFileFormat: w.cfg.LoaderFileFormat,
		}
		if !directSnapshot {
			opts.Table, opts.StagingTable = w.table.StagingTable, true
			opts.PrimaryKeys, opts.CDCExpectedIncarnation = nil, ""
		} else if w.manager != nil {
			opts.CDCSnapshotProgress = w.persistProgress
		}
		var err error
		pending, err = writeCDCSnapshotBatchSegment(ctx, w.dest, records, res, opts)
		if err != nil {
			return fmt.Errorf("failed to write CDC batch for %s: %w", w.sourceTable, err)
		}
	}
}

func (w *cdcSnapshotBatchWriter) persistProgress(ctx context.Context, token source.CDCStateCommitToken) error {
	progress, ok := token.SnapshotProgress[w.sourceTable]
	if !ok {
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := w.manager.BindDestinationIncarnation(ctx, w.sourceTable, w.table.DestTable); err != nil {
		return err
	}
	if err := w.manager.Persist(ctx, source.CDCStateCommitToken{
		SnapshotProgress: map[string]source.CDCSnapshotProgress{w.sourceTable: progress},
	}); err != nil {
		return fmt.Errorf("persist committed snapshot progress for %s: %w", w.sourceTable, err)
	}
	return source.ConnectorLeaseLoss(ctx)
}

func (w *cdcSnapshotBatchWriter) beginSnapshot(ctx context.Context, marker source.RecordBatchResult) error {
	if w.manager != nil {
		if marker.CDCSnapshotResumed {
			if err := w.manager.ResumeSnapshot(ctx, w.sourceTable, w.table.DestTable, ""); err != nil {
				return err
			}
		} else if err := w.manager.InvalidateSnapshot(ctx, w.sourceTable, w.table.DestTable, ""); err != nil {
			return err
		}
		if err := w.manager.BindDestinationIncarnation(ctx, w.sourceTable, w.table.DestTable); err != nil {
			return err
		}
		bound, err := w.manager.BoundDestinationIncarnation(w.sourceTable)
		if err != nil {
			return err
		}
		if w.expectedIncarnation != "" && w.expectedIncarnation != bound {
			return fmt.Errorf("CDC destination for %s changed before its snapshot", w.sourceTable)
		}
		w.expectedIncarnation = bound
	} else if marker.CDCSnapshotResumed {
		return fmt.Errorf("resuming the snapshot of %s requires destination-managed CDC state", w.sourceTable)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	w.snapshotBoundary = marker.CDCSnapshotPosition
	if replacer, ok := destination.SupportsCDCSnapshotSoftReplace(w.dest); ok && w.manager != nil {
		if marker.CDCSnapshotResumed {
			w.retireSnapshot = true
			return nil
		}
		boundary, valid := canonicalPostgresRowLSN(marker.CDCSnapshotPosition)
		if valid {
			maxLSN, err := replacer.MaxCDCRowLSN(ctx, w.table.DestTable)
			if err != nil {
				return fmt.Errorf("read CDC position before replacing snapshot of %s: %w", w.sourceTable, err)
			}
			current, valid := canonicalPostgresRowLSN(maxLSN)
			if maxLSN == "" || valid && current < boundary {
				w.retireSnapshot = true
				return nil
			}
		}
	}
	if marker.CDCSnapshotResumed {
		return nil
	}
	return w.truncate(ctx)
}

func (w *cdcSnapshotBatchWriter) finishSnapshot(ctx context.Context) error {
	if w.snapshotStaging {
		if err := w.finishStaging(ctx); err != nil {
			return err
		}
	}
	if !w.retireSnapshot {
		w.snapshotBoundary = ""
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	replacer, _ := destination.SupportsCDCSnapshotSoftReplace(w.dest)
	if err := replacer.RetireCDCRowsBefore(ctx, destination.CDCRetireOptions{
		Table: w.table.DestTable, PrimaryKeys: w.table.PrimaryKeys, Boundary: w.snapshotBoundary,
		SyncedAt: time.Now().UTC(), ExpectedIncarnation: w.expectedIncarnation, HardDelete: w.cfg.HardDeletesCDC(),
	}); err != nil {
		return fmt.Errorf("retire rows missing from snapshot of %s: %w", w.sourceTable, err)
	}
	w.retireSnapshot, w.snapshotBoundary = false, ""
	return source.ConnectorLeaseLoss(ctx)
}

func (w *cdcSnapshotBatchWriter) prepareStaging(ctx context.Context) error {
	if w.stagingPrepared {
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := w.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: w.table.StagingTable, Schema: w.table.Schema, DropFirst: true,
		CDCMode: w.table.IsCDC, CDCKeys: w.table.PrimaryKeys, PartitionBy: w.table.PartitionBy,
		ClusterBy: w.table.ClusterBy, ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("prepare CDC staging table %s: %w", w.table.StagingTable, err)
	}
	w.stagingPrepared = true
	return source.ConnectorLeaseLoss(ctx)
}

func (w *cdcSnapshotBatchWriter) truncate(ctx context.Context) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	var err error
	if w.expectedIncarnation != "" && destination.SupportsCDCConditionalTruncate(w.dest) {
		err = destination.ApplyCDCTruncateIfIncarnation(ctx, w.dest, w.table.DestTable, w.expectedIncarnation)
	} else {
		err = destination.ApplyCDCTruncate(ctx, w.dest, w.table.DestTable)
	}
	if err != nil {
		return fmt.Errorf("reset CDC target %s: %w", w.table.DestTable, err)
	}
	return source.ConnectorLeaseLoss(ctx)
}

func (w *cdcSnapshotBatchWriter) finishStaging(ctx context.Context) error {
	if w.truncateTarget {
		if w.manager != nil && !w.manager.neverSnapshot {
			if err := w.manager.InvalidateBatchSnapshot(ctx, w.sourceTable, w.table.DestTable); err != nil {
				return err
			}
		}
		if err := w.truncate(ctx); err != nil {
			return err
		}
		w.truncateTarget = false
	}
	if !w.stagingPrepared {
		return source.ConnectorLeaseLoss(ctx)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	opts := mergeStagingOptions(w.dest, w.table.StagingTable, w.table.DestTable, w.table.PrimaryKeys,
		w.table.Schema, w.cfg.IncrementalKey, w.cfg.IncrementalPredicate, w.expectedIncarnation, w.cfg.HardDeletesCDC())
	opts.CDCSnapshot = w.snapshotStaging
	if err := w.dest.MergeTable(ctx, opts); err != nil {
		return fmt.Errorf("merge staged CDC rows for %s: %w", w.sourceTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := w.dest.DropTable(ctx, w.table.StagingTable); err != nil {
		config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", w.table.StagingTable, err)
	}
	w.stagingPrepared = false
	w.snapshotStaging = false
	return source.ConnectorLeaseLoss(ctx)
}

type cdcSnapshotBatchBoundary struct {
	next *source.RecordBatchResult
	err  error
}

func writeCDCSnapshotBatchSegment(ctx context.Context, dest destination.Destination, records <-chan source.RecordBatchResult, first source.RecordBatchResult, opts destination.WriteOptions) (*source.RecordBatchResult, error) {
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	segment := make(chan source.RecordBatchResult)
	done := make(chan cdcSnapshotBatchBoundary, 1)
	go func() {
		defer close(segment)
		res := first
		for {
			select {
			case segment <- res:
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
			if res.Err != nil || res.Truncate || res.CDCSnapshotChunk != nil || res.CDCSnapshotChunkEnd != nil ||
				res.Batch != nil && res.CDCSnapshot != first.CDCSnapshot {
				done <- cdcSnapshotBatchBoundary{next: &res}
				return
			}
		}
	}()
	writeErr := dest.WriteParallel(writeCtx, segment, opts)
	if writeErr != nil {
		cancel()
	}
	boundary := <-done
	if writeErr != nil {
		releaseCDCSnapshotBatchResult(boundary.next)
		return nil, writeErr
	}
	return boundary.next, boundary.err
}

func releaseCDCSnapshotBatchResult(result *source.RecordBatchResult) {
	if result != nil && result.Batch != nil {
		result.Batch.Release()
	}
}

func writeCDCSnapshotBatchTables(ctx context.Context, job *MultiTableIngestionJob, stagingTables map[string]string, records <-chan source.RecordBatchResult, cancelRead context.CancelFunc) error {
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancelOnce sync.Once
	cancelInput := func() {
		cancelOnce.Do(func() {
			cancel()
			cancelRead()
		})
	}
	tables := make([]string, 0, len(job.Tables))
	for _, table := range job.Tables {
		tables = append(tables, table.Name)
	}
	router := multitable.NewRouter(tables, 8)
	router.Route(writeCtx, records, true)
	group, groupCtx := errgroup.WithContext(writeCtx)
	for _, table := range job.Tables {
		group.Go(func() error {
			target := job.GetDestTableName(table.Name)
			expected := ""
			if job.CDCStateManager != nil {
				var err error
				expected, err = job.CDCStateManager.BoundDestinationIncarnation(table.Name)
				if err != nil {
					cancelInput()
					return err
				}
			}
			ch := router.GetChannel(table.Name)
			var err error
			if isAppendOnlyCDCTable(table) {
				_, err = destination.WriteWithTruncateBoundariesAfterCancel(groupCtx, job.Destination, ch, destination.WriteOptions{
					Table: target, Schema: table.Schema, Parallelism: job.Config.EffectiveDestinationParallelism(),
					CDCExpectedIncarnation: expected,
				}, cancelInput)
			} else {
				writer := cdcSnapshotBatchWriter{
					dest: job.Destination, cfg: job.Config, manager: job.CDCStateManager,
					sourceTable: table.Name, expectedIncarnation: expected,
					table: mergeTableParams{
						DestTable: target, StagingTable: stagingTables[table.Name],
						Schema: table.Schema, PrimaryKeys: table.PrimaryKeys, IsCDC: hasCDCColumns(table.Schema),
					},
				}
				err = writer.write(groupCtx, ch, cancelInput)
			}
			if err != nil {
				cancelInput()
				return fmt.Errorf("write CDC table %s: %w", table.Name, err)
			}
			return nil
		})
	}
	err := group.Wait()
	router.Wait()
	return errors.Join(err, router.Err(), ctx.Err())
}

type cdcSnapshotBatchContent struct {
	mu     sync.Mutex
	tables map[string]struct{ snapshot, wal bool }
}

func (c *cdcSnapshotBatchContent) onlySnapshot(table string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	content := c.tables[table]
	return content.snapshot && !content.wal
}

func trackCDCSnapshotBatchContent(ctx context.Context, records <-chan source.RecordBatchResult, singleTable bool) (<-chan source.RecordBatchResult, *cdcSnapshotBatchContent) {
	content := &cdcSnapshotBatchContent{tables: make(map[string]struct{ snapshot, wal bool })}
	out := make(chan source.RecordBatchResult)
	go func() {
		defer close(out)
		for res := range records {
			tableName := res.TableName
			if singleTable {
				tableName = ""
			}
			content.mu.Lock()
			table := content.tables[tableName]
			if res.Truncate {
				table.snapshot, table.wal = res.CDCSnapshotPosition != "" && !res.CDCWALTruncate, false
			}
			if res.Batch != nil && res.Batch.NumRows() > 0 {
				table.snapshot = table.snapshot || res.CDCSnapshot
				table.wal = table.wal || !res.CDCSnapshot
			}
			content.tables[tableName] = table
			content.mu.Unlock()
			select {
			case out <- res:
			case <-ctx.Done():
				releaseCDCSnapshotBatchResult(&res)
				drainAndReleaseUntil(records, streamAbortDrainTimeout)
				return
			}
		}
	}()
	return out, content
}
