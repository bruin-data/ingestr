package starrocks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotWriterEncodingFailurePreventsCheckpoint(t *testing.T) {
	for _, priorVersionPressure := range []bool{false, true} {
		name := "empty transaction"
		if priorVersionPressure {
			name = "version pressure"
		}
		t.Run(name, func(t *testing.T) {
			d, server := newSnapshotTestDestination(t)
			limits := *d.snapshotLimits
			limits.idleInterval = time.Hour
			if priorVersionPressure {
				server.failLoad = 1
			}
			opts := snapshotWriteOptions(1)
			checkpoints := 0
			opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
				checkpoints++
				return nil
			}
			writer := &snapshotWriter{
				dest: d, database: "db", table: "events", opts: opts, limits: limits, workers: 1,
				limiter: &snapshotLoadLimiter{limit: 1, changed: make(chan struct{})},
				stats:   &snapshotWriteStats{},
			}
			window := writer.newWindow(0)
			worker := &snapshotLoadWorker{
				writer: writer, window: window, sealRequests: make(chan struct{}, 1),
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			jobs := make(chan snapshotWriteJob)
			commands := make(chan snapshotWriteCommand, 1)
			completed := make(chan int64, 1)
			done := make(chan error, 1)
			go func() { done <- worker.run(ctx, jobs, commands, completed) }()
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			var sequence int64
			sendPage := func(value string, expectCompletion bool) {
				t.Helper()
				sequence++
				batch := snapshotFailurePage(mem, sequence, value)
				select {
				case jobs <- snapshotWriteJob{sequence: sequence, result: source.RecordBatchResult{Batch: batch, CDCSnapshot: true}}:
				case <-ctx.Done():
					batch.Release()
					t.Fatal(ctx.Err())
				}
				if expectCompletion {
					select {
					case finished := <-completed:
						require.Equal(t, sequence, finished)
					case err := <-done:
						t.Fatalf("worker failed before completing page: %v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			if priorVersionPressure {
				sendPage(strings.Repeat("v", 600), true)
				sendPage(strings.Repeat("v", 600), true)
			}
			sendPage("buffered tail", true)
			completedPage := sequence
			window.token = &source.CDCStateCommitToken{
				SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}},
			}
			sendPage(strings.Repeat("x", 2*limits.requestBytes), false)
			var workerErr error
			select {
			case workerErr = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.ErrorContains(t, workerErr, "a single row")
			require.NoError(t, ctx.Err(), "the outer writer may not have observed the worker failure yet")
			var buffered []struct {
				ID int64 `json:"id"`
			}
			body := append(append([]byte(nil), worker.body.data...), ']')
			require.NoError(t, json.Unmarshal(body, &buffered))
			require.Len(t, buffered, 1)
			require.Equal(t, completedPage, buffered[0].ID)
			if priorVersionPressure {
				require.Equal(t, 1, server.loads)
				require.Len(t, window.parts[0].replay, 2)
			} else {
				require.Zero(t, server.loads)
				require.Nil(t, window.parts[0].tx)
				require.Empty(t, window.parts[0].replay)
			}
			commitErr := writer.commitWindow(ctx, window)
			if commitErr == nil {
				require.NoError(t, writer.checkpointWindow(ctx, window))
			}
			assert.ErrorIs(t, commitErr, workerErr, "a failed worker must not release its window as successfully flushed")
			assert.Zero(t, checkpoints, "progress must not cover the completed page's unflushed tail")
			assert.Zero(t, server.commits)
			writer.rollbackOpen(ctx)
			mem.AssertSize(t, 0)
		})
	}
}

func TestSnapshotWriterClosedJobsPreventsUnsealedCheckpoint(t *testing.T) {
	d, server := newSnapshotTestDestination(t)
	limits := *d.snapshotLimits
	limits.idleInterval = time.Hour
	opts := snapshotWriteOptions(1)
	checkpoints := 0
	opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
		checkpoints++
		return nil
	}
	writer := &snapshotWriter{
		dest: d, database: "db", table: "events", opts: opts, limits: limits, workers: 1,
		stats: &snapshotWriteStats{},
	}
	window := writer.newWindow(0)
	worker := &snapshotLoadWorker{
		writer: writer, window: window, sealRequests: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	jobs := make(chan snapshotWriteJob, 1)
	commands := make(chan snapshotWriteCommand, 1)
	completed := make(chan int64, 1)
	done := make(chan error, 1)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	jobs <- snapshotWriteJob{
		sequence: 1, result: source.RecordBatchResult{Batch: snapshotFailurePage(mem, 1, "buffered tail"), CDCSnapshot: true},
	}
	go func() { done <- worker.run(ctx, jobs, commands, completed) }()
	select {
	case sequence := <-completed:
		require.EqualValues(t, 1, sequence)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	window.token = &source.CDCStateCommitToken{
		SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}},
	}
	close(jobs)
	var workerErr error
	select {
	case workerErr = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, ctx.Err(), "the coordinator can close jobs before the outer errgroup cancels its context")
	require.Greater(t, len(worker.body.data), 1, "the completed page must still have unflushed rows")
	assert.Error(t, workerErr, "stopping before the final seal must fail the worker")
	commitErr := writer.commitWindow(ctx, window)
	if commitErr == nil {
		require.NoError(t, writer.checkpointWindow(ctx, window))
	}
	assert.Error(t, commitErr, "closing jobs must not certify an unsealed window")
	if workerErr != nil {
		assert.ErrorIs(t, commitErr, workerErr)
	}
	assert.Zero(t, checkpoints, "progress must not cover the completed page's unflushed tail")
	assert.Zero(t, server.commits)
	mem.AssertSize(t, 0)
}

func snapshotFailurePage(mem memory.Allocator, id int64, value string) arrow.RecordBatch {
	builder := array.NewRecordBuilder(mem, snapshotWriteOptions(1).Schema.ToArrowSchema())
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(id)
	builder.Field(1).(*array.StringBuilder).Append(value)
	builder.Field(2).(*array.StringBuilder).Append("00000001/00000020")
	builder.Field(3).(*array.BooleanBuilder).Append(false)
	builder.Field(4).(*array.TimestampBuilder).Append(1_000_000)
	builder.Field(5).(*array.StringBuilder).Append("[]")
	return builder.NewRecordBatch()
}
