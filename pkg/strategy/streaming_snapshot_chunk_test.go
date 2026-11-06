package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type snapshotChunkTestDestination struct {
	*chunkedAdapterDestination
	write func(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error
	merge func(context.Context, destination.MergeOptions) error
}

func (d *snapshotChunkTestDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if d.write != nil {
		return d.write(ctx, records, opts)
	}
	return d.chunkedAdapterDestination.WriteParallel(ctx, records, opts)
}

func (d *snapshotChunkTestDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if d.merge != nil {
		return d.merge(ctx, opts)
	}
	return d.fakeDestination.MergeTable(ctx, opts)
}

func snapshotChunkTestLoop(t *testing.T) (*flushLoop, *snapshotChunkTestDestination, *CDCStateManager, *fakeCommitter) {
	t.Helper()
	dest := &snapshotChunkTestDestination{chunkedAdapterDestination: &chunkedAdapterDestination{newCDCStateDestination()}}
	manager, err := NewCDCStateManager(dest, "snapshot-chunk", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(t.Context(), "public.items", "raw.items"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{"public.items": "00000000/00000010"}}))
	st := mergeTableState("raw.items")
	st.isCDC = true
	st.schema.Columns = append(st.schema.Columns,
		schema.Column{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		schema.Column{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
	)
	committer := &fakeCommitter{}
	cfg := config.DefaultConfig()
	cfg.CDCDeleteBehavior = config.CDCDeleteHard
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		Strategy: config.StrategyMerge, StateManager: manager, Committer: committer,
		FlushInterval: time.Hour, FlushRecords: 1_000_000,
	}, map[string]*streamTableState{"public.items": st})
	return loop, dest, manager, committer
}

func snapshotChunkTestBatch(mem memory.Allocator, id int64) arrow.RecordBatch {
	builder := array.NewInt64Builder(mem)
	defer builder.Release()
	builder.Append(id)
	ids := builder.NewArray()
	defer ids.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{ids}, 1)
}

func snapshotChunkTestRecords(t *testing.T, mem *memory.CheckedAllocator, orderedIntegerKeys bool) <-chan source.RecordBatchResult {
	t.Helper()
	chunk := &source.CDCSnapshotChunk{After: []string{"1"}, Last: []string{"3"}, OrderedIntegerKeys: orderedIntegerKeys}
	records := make(chan source.RecordBatchResult, 6)
	records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshotChunk: chunk}
	for _, id := range []int64{2, 3} {
		records <- source.RecordBatchResult{TableName: "public.items", Batch: snapshotChunkTestBatch(mem, id)}
	}
	records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshotChunkEnd: chunk}
	records <- source.RecordBatchResult{TableName: "public.items", Batch: snapshotChunkTestBatch(mem, 999)}
	records <- source.RecordBatchResult{CDCFlush: true, CommitToken: source.CDCStateCommitToken{Position: "00000000/00000020", SourceCommitToken: "chunk-1"}}
	close(records)
	t.Cleanup(func() {
		drainAndRelease(records)
		mem.AssertSize(t, 0)
	})
	return records
}

func TestStreamingSnapshotChunkMergesBeforeWALAndAcknowledgment(t *testing.T) {
	for _, orderedIntegerKeys := range []bool{true, false} {
		t.Run(fmt.Sprint(orderedIntegerKeys), func(t *testing.T) {
			loop, dest, manager, committer := snapshotChunkTestLoop(t)
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			records := snapshotChunkTestRecords(t, mem, orderedIntegerKeys)
			var loaded [][]int64
			dest.write = func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
				var ids []int64
				for result := range records {
					ids = append(ids, result.Batch.Column(0).(*array.Int64).Int64Values()...)
					result.Batch.Release()
				}
				loaded = append(loaded, ids)
				empty := make(chan source.RecordBatchResult)
				close(empty)
				return dest.chunkedAdapterDestination.WriteParallel(ctx, empty, opts)
			}
			dest.merge = func(ctx context.Context, opts destination.MergeOptions) error {
				if len(committer.committed()) > 0 {
					return errors.New("source acknowledged before both merges completed")
				}
				if len(dest.mergeCalls) == 1 && len(dest.truncateCalls) != 1 {
					return errors.New("snapshot staging was not cleared before WAL")
				}
				return dest.fakeDestination.MergeTable(ctx, opts)
			}
			loop.opts.Committer = chunkedCommitFunc(func(ctx context.Context, token any) error {
				if len(dest.mergeCalls) != 2 || len(dest.truncateCalls) != 2 {
					return errors.New("source acknowledged before snapshot and WAL were durable")
				}
				position, err := manager.ResumePosition(ctx, "public.items")
				if err != nil || position != "00000000/00000020" {
					return errors.New("source acknowledged before destination checkpoint")
				}
				return committer.CommitStream(ctx, token)
			})
			require.NoError(t, loop.run(t.Context(), records))
			require.Equal(t, [][]int64{{2, 3}, {999}}, loaded)
			require.Len(t, dest.mergeCalls, 2)
			snapshot, wal := dest.mergeCalls[0], dest.mergeCalls[1]
			require.True(t, snapshot.CDCSnapshot)
			require.False(t, snapshot.CDCHardDelete)
			if orderedIntegerKeys {
				require.Equal(t, &destination.CDCSnapshotRange{After: []string{"1"}, Last: []string{"3"}}, snapshot.CDCSnapshotRange)
			} else {
				require.Nil(t, snapshot.CDCSnapshotRange)
			}
			require.False(t, wal.CDCSnapshot)
			require.Nil(t, wal.CDCSnapshotRange)
			require.True(t, wal.CDCHardDelete)
			require.Equal(t, "incarnation:raw.items", snapshot.CDCExpectedIncarnation)
			require.Equal(t, snapshot.CDCExpectedIncarnation, wal.CDCExpectedIncarnation)
			require.Len(t, committer.committed(), 1)
			require.Zero(t, loop.tables["public.items"].snapshotRows)
			mem.AssertSize(t, 0)
		})
	}
}

func TestStreamingSnapshotChunkFailuresDoNotAcknowledge(t *testing.T) {
	for _, failure := range []string{"snapshot load", "snapshot merge", "WAL load", "WAL merge"} {
		t.Run(failure, func(t *testing.T) {
			loop, dest, manager, committer := snapshotChunkTestLoop(t)
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			records := snapshotChunkTestRecords(t, mem, true)
			injected := errors.New(failure + " failed")
			writes, merges := 0, 0
			dest.write = func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
				writes++
				if failure == "snapshot load" && writes == 1 || failure == "WAL load" && writes == 2 {
					return injected
				}
				return dest.chunkedAdapterDestination.WriteParallel(ctx, records, opts)
			}
			dest.merge = func(ctx context.Context, opts destination.MergeOptions) error {
				merges++
				if failure == "snapshot merge" && merges == 1 || failure == "WAL merge" && merges == 2 {
					return injected
				}
				return dest.fakeDestination.MergeTable(ctx, opts)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.ErrorIs(t, loop.run(ctx, records), injected)
			require.Empty(t, committer.committed())
			position, err := manager.ResumePosition(t.Context(), "public.items")
			require.NoError(t, err)
			require.Equal(t, "00000000/00000010", position)
		})
	}
}

func TestStreamingSnapshotChunkPrematureEndCancelsLoad(t *testing.T) {
	loop, dest, _, committer := snapshotChunkTestLoop(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshotChunk: &source.CDCSnapshotChunk{}}
	records <- source.RecordBatchResult{TableName: "public.items", Batch: snapshotChunkTestBatch(mem, 1)}
	close(records)
	t.Cleanup(func() {
		drainAndRelease(records)
		mem.AssertSize(t, 0)
	})
	canceled := make(chan struct{})
	dest.write = func(ctx context.Context, _ <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.ErrorContains(t, loop.run(ctx, records), "source ended before snapshot chunk completion")
	require.NoError(t, ctx.Err(), "chunk load waited for the outer timeout")
	select {
	case <-canceled:
	default:
		t.Fatal("snapshot writer was not canceled and joined")
	}
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, committer.committed())
	mem.AssertSize(t, 0)
}

func TestStreamingSnapshotChunkSourceErrorReleasesItsBatch(t *testing.T) {
	loop, dest, _, committer := snapshotChunkTestLoop(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	readErr := errors.New("snapshot read failed")
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.items", CDCSnapshotChunk: &source.CDCSnapshotChunk{}}
	records <- source.RecordBatchResult{Batch: snapshotChunkTestBatch(mem, 1), Err: readErr}
	close(records)
	require.ErrorIs(t, loop.run(t.Context(), records), readErr)
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, committer.committed())
	mem.AssertSize(t, 0)
}

func TestStreamingSnapshotChunkTargetReplacementRejectsMerge(t *testing.T) {
	loop, dest, _, committer := snapshotChunkTestLoop(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := snapshotChunkTestRecords(t, mem, true)
	dest.write = func(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
		if err := dest.chunkedAdapterDestination.WriteParallel(ctx, records, opts); err != nil {
			return err
		}
		dest.stateMu.Lock()
		dest.incarnations["raw.items"] = "replacement"
		dest.stateMu.Unlock()
		return nil
	}
	dest.merge = func(ctx context.Context, opts destination.MergeOptions) error {
		actual, _, err := dest.CDCTargetIncarnation(ctx, opts.TargetTable)
		if err != nil {
			return err
		}
		if opts.CDCExpectedIncarnation != actual {
			return errors.New("physical incarnation changed")
		}
		return dest.fakeDestination.MergeTable(ctx, opts)
	}
	require.ErrorContains(t, loop.run(t.Context(), records), "physical incarnation changed")
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, committer.committed())
}

func TestStreamingSnapshotChunkTransformedKeysDisableRange(t *testing.T) {
	for _, transformation := range []string{"mask", "column override"} {
		t.Run(transformation, func(t *testing.T) {
			loop, dest, _, _ := snapshotChunkTestLoop(t)
			if transformation == "mask" {
				loop.cfg.Mask = []string{"id:hash"}
			} else {
				loop.cfg.Columns = "id:bigint"
			}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			require.NoError(t, loop.run(t.Context(), snapshotChunkTestRecords(t, mem, true)))
			require.Len(t, dest.mergeCalls, 2)
			require.True(t, dest.mergeCalls[0].CDCSnapshot)
			require.Nil(t, dest.mergeCalls[0].CDCSnapshotRange)
		})
	}
}

func TestStreamingSnapshotChunkShutdownDoesNotFlushQueuedPages(t *testing.T) {
	loop, dest, _, committer := snapshotChunkTestLoop(t)
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	records := snapshotChunkTestRecords(t, mem, true)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, loop.shutdown(ctx, records), context.Canceled)
	require.Empty(t, dest.writeCalls)
	require.Empty(t, dest.mergeCalls)
	require.Empty(t, committer.committed())
	require.Equal(t, 5, len(records))
}
