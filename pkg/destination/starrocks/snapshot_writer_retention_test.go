package starrocks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSnapshotWriterRetiresCheckpointedWindows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, server := newSnapshotTestDestination(t)
		opts := snapshotWriteOptions(1)
		persisted := make(chan struct{}, 1)
		opts.CDCSnapshotProgress = func(_ context.Context, token source.CDCStateCommitToken) error {
			require.Len(t, token.SnapshotProgress["events"].Ranges, 1024)
			persisted <- struct{}{}
			return nil
		}
		s := &snapshotWriter{
			dest: d, table: "events", opts: opts, limits: *d.snapshotLimits, workers: 1,
			limiter: &snapshotLoadLimiter{limit: 1, changed: make(chan struct{})},
			stats:   &snapshotWriteStats{},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		records := make(chan source.RecordBatchResult)
		done := make(chan error, 1)
		go func() { done <- s.run(ctx, records) }()
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		maxRetained := 0
		for id := range 32 {
			ranges := make([]source.CDCSnapshotRangeProgress, 1024)
			for i := range ranges {
				ranges[i] = source.CDCSnapshotRangeProgress{ID: fmt.Sprint(i), Key: []string{fmt.Sprint(id)}}
			}
			records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
			records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
				SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1", Ranges: ranges}},
			}}
			<-persisted
			synctest.Wait()
			s.mu.Lock()
			maxRetained = max(maxRetained, len(s.windows))
			s.mu.Unlock()
		}
		close(records)
		require.NoError(t, <-done)
		require.LessOrEqual(t, maxRetained, 1, "only the open window should remain after each checkpoint")
		require.Empty(t, s.windows, "completed snapshots must release every tracked window and its range plan")
		require.Equal(t, 32, server.commits)
		mem.AssertSize(t, 0)
	})
}

func TestSnapshotWriterRetainsWindowAfterCheckpointFailure(t *testing.T) {
	d, _ := newSnapshotTestDestination(t)
	opts := snapshotWriteOptions(1)
	want := errors.New("checkpoint unavailable")
	opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error { return want }
	s := &snapshotWriter{dest: d, table: "events", opts: opts, workers: 1, stats: &snapshotWriteStats{}}
	window := s.newWindow(0)
	window.token = &source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}
	require.ErrorIs(t, s.checkpointWindow(t.Context(), window), want)
	require.Equal(t, []*snapshotWindow{window}, s.windows)
	require.NotNil(t, window.token)
}
