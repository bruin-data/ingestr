package starrocks

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type snapshotCommittedMetrics struct {
	rows  int64
	bytes uint64
}

func snapshotPageMetrics(batch arrow.RecordBatch) snapshotCommittedMetrics {
	stats := snapshotCommittedMetrics{rows: batch.NumRows()}
	for _, column := range batch.Columns() {
		stats.bytes += column.Data().SizeInBytes()
	}
	return stats
}

func snapshotMetricsProgress() source.RecordBatchResult {
	return source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}},
	}}
}

func TestSnapshotWriterReportsCommittedMetricsWhileInputOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		batch := snapshotTestPage(mem, 0, 3)
		want := snapshotPageMetrics(batch)
		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{Batch: batch, CDCSnapshot: true}
		records <- snapshotMetricsProgress()
		opts := snapshotWriteOptions(1)
		var persisted atomic.Bool
		opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
			persisted.Store(true)
			return nil
		}
		reported := make(chan snapshotCommittedMetrics, 4)
		opts.CDCSnapshotCommitted = func(_ context.Context, rows int64, bytes uint64) error {
			if !persisted.Load() {
				return errors.New("metrics reported before snapshot progress was persisted")
			}
			server.mu.Lock()
			visible := server.visible == 1 && server.commits == 1
			server.mu.Unlock()
			if !visible {
				return errors.New("metrics reported before snapshot rows became visible")
			}
			reported <- snapshotCommittedMetrics{rows: rows, bytes: bytes}
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- d.writeSnapshot(ctx, records, opts) }()
		time.Sleep(2 * d.snapshotLimits.idleInterval)
		synctest.Wait()
		require.Empty(t, reported, "loading a request must not publish committed metrics")
		select {
		case got := <-reported:
			require.Equal(t, want, got)
		case err := <-done:
			t.Fatalf("snapshot writer stopped while input was open: %v", err)
		case <-time.After(2 * d.snapshotLimits.commitInterval):
			t.Fatal("snapshot metrics did not advance while input remained open")
		}
		close(records)
		require.NoError(t, <-done)
		require.Empty(t, reported, "closing the segment must not report the committed window twice")
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterCountsOriginalPageOnceAcrossWindows(t *testing.T) {
	for _, workers := range []int{1, 4} {
		for _, retry := range []bool{false, true} {
			t.Run(fmt.Sprintf("workers=%d/retry=%t", workers, retry), func(t *testing.T) {
				d, server := newSnapshotTestDestination(t)
				d.snapshotLimits.requestTarget = 1
				d.snapshotLimits.transactionBytes = 1024
				if retry {
					server.failLoad = 1
				}
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				batch := snapshotTestPage(mem, 0, 100)
				want := snapshotPageMetrics(batch)
				records := make(chan source.RecordBatchResult, 1)
				records <- source.RecordBatchResult{Batch: batch, CDCSnapshot: true}
				close(records)
				opts := snapshotWriteOptions(workers)
				var reported []snapshotCommittedMetrics
				opts.CDCSnapshotCommitted = func(_ context.Context, rows int64, bytes uint64) error {
					server.mu.Lock()
					defer server.mu.Unlock()
					var committedRows int
					for _, tx := range server.transactions {
						if tx.committed {
							committedRows += len(tx.rows)
						}
					}
					if committedRows != int(want.rows) || server.visible != server.commits {
						return errors.New("page metrics reported before every transaction containing the page became visible")
					}
					reported = append(reported, snapshotCommittedMetrics{rows: rows, bytes: bytes})
					return nil
				}
				require.NoError(t, d.writeSnapshot(t.Context(), records, opts))
				require.Greater(t, server.commits, 1, "the Arrow page must span multiple transaction windows")
				require.Equal(t, []snapshotCommittedMetrics{want}, reported)
				mem.AssertSize(t, 0)
			})
		}
	}
}

func TestSnapshotWriterFailedWindowDoesNotReportMetrics(t *testing.T) {
	for _, phase := range []string{"visibility", "persistence"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, server := newSnapshotTestDestination(t)
				wantErr := errors.New("snapshot " + phase + " failed")
				waitForPublish := d.loader.waitForPublish
				var visibilityCalls atomic.Int64
				d.loader.waitForPublish = func(ctx context.Context, database string, id int64) error {
					if visibilityCalls.Add(1) == 2 && phase == "visibility" {
						return wantErr
					}
					return waitForPublish(ctx, database, id)
				}
				opts := snapshotWriteOptions(1)
				var persisted atomic.Int64
				opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
					if persisted.Add(1) == 2 && phase == "persistence" {
						return wantErr
					}
					return nil
				}
				reported := make(chan snapshotCommittedMetrics, 4)
				opts.CDCSnapshotCommitted = func(_ context.Context, rows int64, bytes uint64) error {
					reported <- snapshotCommittedMetrics{rows: rows, bytes: bytes}
					return nil
				}
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				batch := snapshotTestPage(mem, 0, 3)
				want := snapshotPageMetrics(batch)
				records := make(chan source.RecordBatchResult, 2)
				records <- source.RecordBatchResult{Batch: batch, CDCSnapshot: true}
				records <- snapshotMetricsProgress()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- d.writeSnapshot(ctx, records, opts) }()
				select {
				case got := <-reported:
					require.Equal(t, want, got)
				case err := <-done:
					t.Fatalf("snapshot writer stopped before its first committed window: %v", err)
				case <-time.After(2 * d.snapshotLimits.commitInterval):
					t.Fatal("first committed window did not report metrics")
				}
				records <- source.RecordBatchResult{Batch: snapshotTestPage(mem, 3, 11), CDCSnapshot: true}
				records <- snapshotMetricsProgress()
				close(records)
				require.ErrorIs(t, <-done, wantErr)
				require.Equal(t, 2, server.commits)
				require.Empty(t, reported, "a failed window must not add to previously committed metrics")
				mem.AssertSize(t, 0)
			})
		})
	}
}
