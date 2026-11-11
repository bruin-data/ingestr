package postgres_cdc

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func coordinateSnapshotRanges(ctx context.Context, plan []snapshotRange, concurrency int, results chan<- source.RecordBatchResult,
	progress func(context.Context, []snapshotRange) error,
	read func(context.Context, int, snapshotRange, chan<- snapshotRangeResult) error,
) (int64, time.Duration, error) {
	return coordinateSnapshotWorkers(ctx, plan, concurrency, results, progress,
		func(ctx context.Context, _ int, jobs <-chan int, updates chan<- snapshotRangeResult) error {
			for i := range jobs {
				if err := read(ctx, i, plan[i], updates); err != nil {
					return err
				}
			}
			return nil
		})
}

func coordinatorRecord(mem memory.Allocator, id int64) arrow.RecordBatch {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Append(id)
	a := b.NewArray()
	defer a.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{a}, 1)
}

func TestSnapshotCoordinatorOutOfOrderProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	plan := []snapshotRange{{ID: "0", Upper: []string{"10"}}, {ID: "1", After: []string{"10"}}}
	lowMayFinish := make(chan struct{})
	results := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() {
		_, _, err := coordinateSnapshotRanges(ctx, plan, 2, results,
			func(ctx context.Context, ranges []snapshotRange) error {
				if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: ranges}); err != nil {
					return err
				}
				if ranges[1].Complete && ranges[0].LastKey == nil {
					close(lowMayFinish)
				}
				return nil
			},
			func(ctx context.Context, i int, r snapshotRange, updates chan<- snapshotRangeResult) error {
				if i == 0 {
					select {
					case <-lowMayFinish:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				id := int64(1 + i*10)
				batch := coordinatorRecord(mem, id)
				select {
				case updates <- snapshotRangeResult{index: i, batch: batch, lastKey: []string{strconv.FormatInt(id, 10)}}:
				case <-ctx.Done():
					batch.Release()
					return ctx.Err()
				}
				select {
				case updates <- snapshotRangeResult{index: i, done: true}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		done <- err
		close(results)
	}()
	seen := make(map[string]bool)
	var vectors [][]snapshotRange
	for result := range results {
		if result.Batch != nil {
			id := result.Batch.Column(0).(*array.Int64).Value(0)
			seen[strconv.FormatInt(id, 10)] = true
			result.Batch.Release()
		} else {
			vector := result.CommitToken.([]snapshotRange)
			for _, r := range vector {
				if r.LastKey != nil {
					require.True(t, seen[r.LastKey[0]], "progress must follow its covered row")
				}
			}
			vectors = append(vectors, vector)
		}
	}
	require.NoError(t, <-done)
	require.Len(t, vectors, 4)
	require.Nil(t, vectors[1][0].LastKey, "high range completion must leave low range unfinished")
	require.False(t, vectors[1][0].Complete)
	require.True(t, vectors[1][1].Complete)
	require.True(t, vectors[3][0].Complete)
	require.True(t, vectors[3][1].Complete)
	require.Nil(t, plan[1].LastKey, "coordinator must not mutate its saved input plan")
	require.False(t, vectors[0][1].Complete, "published vectors must remain immutable")
}

func TestSnapshotCoordinatorFailureCancelsBlockedForward(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	sent := make(chan struct{})
	wantErr := errors.New("range import failed")
	progressCalls := 0
	_, _, err := coordinateSnapshotRanges(ctx, []snapshotRange{{ID: "0"}, {ID: "1"}}, 2,
		make(chan source.RecordBatchResult),
		func(context.Context, []snapshotRange) error { progressCalls++; return nil },
		func(ctx context.Context, i int, r snapshotRange, updates chan<- snapshotRangeResult) error {
			if i == 1 {
				<-sent
				return wantErr
			}
			batch := coordinatorRecord(mem, 1)
			select {
			case updates <- snapshotRangeResult{index: i, batch: batch, lastKey: []string{"1"}}:
				close(sent)
			case <-ctx.Done():
				batch.Release()
				return ctx.Err()
			}
			<-ctx.Done()
			return ctx.Err()
		})
	require.ErrorIs(t, err, wantErr)
	require.Zero(t, progressCalls)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
}

func TestSnapshotCoordinatorResumeSchedulesOnlyUnfinishedRanges(t *testing.T) {
	plan := []snapshotRange{
		{ID: "0", Upper: []string{"10"}},
		{ID: "1", After: []string{"10"}, Upper: []string{"20"}, Complete: true},
		{ID: "2", After: []string{"20"}, LastKey: []string{"23"}},
	}
	for _, workers := range []int{1, 4} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			read := make(chan snapshotRange, len(plan))
			var last []snapshotRange
			_, _, err := coordinateSnapshotRanges(ctx, plan, workers, nil,
				func(ctx context.Context, ranges []snapshotRange) error { last = ranges; return nil },
				func(ctx context.Context, i int, r snapshotRange, updates chan<- snapshotRangeResult) error {
					read <- r
					updates <- snapshotRangeResult{index: i, done: true}
					return nil
				})
			require.NoError(t, err)
			close(read)
			var scheduled []string
			for r := range read {
				scheduled = append(scheduled, r.ID)
				require.Equal(t, plan[mustRangeIndex(t, r.ID)], r)
			}
			require.ElementsMatch(t, []string{"0", "2"}, scheduled)
			for _, r := range last {
				require.True(t, r.Complete, "empty unfinished ranges must explicitly complete")
			}
		})
	}
}

func mustRangeIndex(t *testing.T, id string) int {
	t.Helper()
	i, err := strconv.Atoi(id)
	require.NoError(t, err)
	return i
}

func TestSnapshotCoordinatorProgressFailureCancelsWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wantErr := errors.New("progress persistence failed")
	_, _, err := coordinateSnapshotRanges(ctx, []snapshotRange{{ID: "0"}, {ID: "1"}}, 2, nil,
		func(context.Context, []snapshotRange) error { return wantErr },
		func(ctx context.Context, i int, r snapshotRange, updates chan<- snapshotRangeResult) error {
			select {
			case updates <- snapshotRangeResult{index: i, done: true}:
			case <-ctx.Done():
				return ctx.Err()
			}
			<-ctx.Done()
			return ctx.Err()
		})
	require.ErrorIs(t, err, wantErr)
}

func TestSnapshotCoordinatorPreservesFullImageFlag(t *testing.T) {
	for _, normal := range []bool{false, true} {
		t.Run(strconv.FormatBool(normal), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			results := make(chan source.RecordBatchResult, 2)
			rows, _, err := coordinateSnapshotRanges(t.Context(), []snapshotRange{{ID: "0"}}, 1, results, nil,
				func(ctx context.Context, index int, _ snapshotRange, updates chan<- snapshotRangeResult) error {
					updates <- snapshotRangeResult{index: index, batch: coordinatorRecord(mem, 1), normalSnapshot: normal}
					updates <- snapshotRangeResult{index: index, done: true, normalSnapshot: normal}
					return nil
				})
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			wantResults := 1
			if normal {
				wantResults++
			}
			require.Len(t, results, wantResults)
			result := <-results
			require.Equal(t, normal, result.CDCSnapshot)
			require.Nil(t, result.CommitToken)
			require.False(t, result.Truncate)
			result.Batch.Release()
			if normal {
				boundary := <-results
				require.True(t, boundary.CDCSnapshotRangeEnd)
				require.False(t, boundary.CDCSnapshot)
				require.Nil(t, boundary.Batch)
				require.Nil(t, boundary.CommitToken, "range EOF must not emit a table completion event")
			}
		})
	}
}

func TestSnapshotCoordinatorManyRangesKeepsProgressAndCompletion(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	plan := make([]snapshotRange, 64)
	for i := range plan {
		plan[i].ID = strconv.Itoa(i)
	}
	results := make(chan source.RecordBatchResult, len(plan)*4)
	var vectors [][]snapshotRange
	rows, _, err := coordinateSnapshotRanges(t.Context(), plan, 4, results,
		func(_ context.Context, ranges []snapshotRange) error {
			vectors = append(vectors, ranges)
			return nil
		},
		func(_ context.Context, index int, _ snapshotRange, updates chan<- snapshotRangeResult) error {
			for key := range 3 {
				updates <- snapshotRangeResult{index: index, batch: coordinatorRecord(mem, int64(index*3+key)), lastKey: []string{strconv.Itoa(key)}, normalSnapshot: true}
			}
			updates <- snapshotRangeResult{index: index, done: true, normalSnapshot: true}
			return nil
		})
	require.NoError(t, err)
	require.EqualValues(t, len(plan)*3, rows)
	require.GreaterOrEqual(t, len(vectors), len(plan))
	require.Less(t, len(vectors), len(plan)*3, "large plans must not clone their progress for every batch")
	last := vectors[len(vectors)-1]
	for _, r := range last {
		require.True(t, r.Complete)
		require.Equal(t, []string{"2"}, r.LastKey)
	}
	completed := 0
	for _, r := range vectors[0] {
		if r.Complete {
			completed++
		}
	}
	require.Less(t, completed, len(plan), "later updates must not change earlier vectors")
	close(results)
	boundaries := 0
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.CDCSnapshotRangeEnd {
			boundaries++
		}
	}
	require.Equal(t, len(plan), boundaries)
}

func TestSnapshotCoordinatorCompletedPlanDoesNotReadOrCompleteTable(t *testing.T) {
	plan := []snapshotRange{{ID: "0", Upper: []string{"10"}, Complete: true}, {ID: "1", After: []string{"10"}, Complete: true}}
	results := make(chan source.RecordBatchResult, 1)
	rows, _, err := coordinateSnapshotRanges(t.Context(), plan, 4, results,
		func(context.Context, []snapshotRange) error {
			t.Error("an unchanged completed plan must not emit worker progress")
			return nil
		},
		func(context.Context, int, snapshotRange, chan<- snapshotRangeResult) error {
			t.Error("completed ranges must not acquire workers")
			return nil
		})
	require.NoError(t, err)
	require.Zero(t, rows)
	require.Empty(t, results, "the caller owns table completion even when all ranges are already complete")
}
