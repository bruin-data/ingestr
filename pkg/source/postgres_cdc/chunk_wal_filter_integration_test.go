//go:build integration

package postgres_cdc

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestChunkedSnapshotDefersFutureWritesAcrossResume(t *testing.T) {
	f := newSerialSnapshotFixture(t)
	f.opts.PageSize = 1
	f.opts.CDCChunkedSnapshot.ChunkSize = 20
	f.opts.CDCChunkedSnapshot.Execution = "parallel"
	_, err := f.pool.Exec(f.ctx, "INSERT INTO public.serial_items SELECT i, 'initial' FROM generate_series(10, 40) i")
	require.NoError(t, err)
	first, table := f.open(t)
	ctx, stop := context.WithCancel(f.ctx)
	defer stop()
	results, err := table.Read(ctx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	mutated, snapshot, checkpointed := false, false, false
	var walKeys []int64
	for result := range results {
		require.NoError(t, result.Err)
		if result.CDCSnapshotChunk != nil {
			snapshot = true
			require.Equal(t, []string{"20"}, result.CDCSnapshotChunk.Last)
		}
		if result.Batch != nil && snapshot && !mutated {
			// Twenty one-row pages keep emission blocked before the catchup
			// barrier while these changes commit after the exported snapshot.
			_, err = f.pool.Exec(f.ctx, `
				UPDATE public.serial_items SET value = 'copied update' WHERE id = 3;
				UPDATE public.serial_items SET value = 'future update' WHERE id = 25;
				DELETE FROM public.serial_items WHERE id IN (26, 28);
				UPDATE public.serial_items SET id = 28, value = 'moved forward' WHERE id = 4;
				DELETE FROM public.serial_items WHERE id = 28;
				UPDATE public.serial_items SET id = 0, value = 'moved backward' WHERE id = 29;
				UPDATE public.serial_items SET id = 41, value = 'moved beyond upper' WHERE id = 30;
				INSERT INTO public.serial_items VALUES (50, 'inserted beyond upper');
				UPDATE public.serial_items SET value = 'upper update' WHERE id = 40;
			`)
			require.NoError(t, err)
			mutated = true
		}
		if result.CDCSnapshotChunkEnd != nil {
			snapshot = false
		}
		if result.Batch != nil && !snapshot {
			walKeys = append(walKeys, result.Batch.Column(0).(*array.Int64).Int64Values()...)
		}
		sink.apply(t, result)
		if result.CDCFlush {
			checkpoint := result.CommitToken.(source.CDCStateCommitToken).SourceCommitToken.(*chunkedCommit)
			require.NoError(t, first.CommitStream(f.ctx, result.CommitToken))
			if len(checkpoint.state.Tables[serialSnapshotTable].After) > 0 {
				checkpointed = true
				stop()
				break
			}
		}
	}
	require.True(t, mutated)
	require.True(t, checkpointed)
	require.NotContains(t, walKeys, int64(25), "a later snapshot must supply this complete update")
	require.NotContains(t, walKeys, int64(40), "the fixed upper key is included in a later snapshot")
	for _, id := range []int64{0, 3, 4, 26, 28, 29, 30, 41, 50} {
		require.Contains(t, walKeys, id, "deletes, key moves and writes beyond the upper bound must be merged")
	}
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, first.Close(f.ctx))
	for id, row := range sink {
		if row.deleted {
			delete(sink, id)
		}
	}
	second, resumedTable := f.open(t)
	results, err = resumedTable.Read(f.ctx, f.opts)
	require.NoError(t, err)
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if result.CDCFlush {
			require.NoError(t, second.CommitStream(f.ctx, result.CommitToken))
		}
	}
	rows, err := f.pool.Query(f.ctx, "SELECT id, value FROM public.serial_items")
	require.NoError(t, err)
	defer rows.Close()
	expected := make(map[int64]string)
	for rows.Next() {
		var id int64
		var value string
		require.NoError(t, rows.Scan(&id, &value))
		expected[id] = value
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, sink.rows())
}
