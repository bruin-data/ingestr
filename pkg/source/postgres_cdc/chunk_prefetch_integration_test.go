//go:build integration

package postgres_cdc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestChunkedSnapshotPrefetchKeepsPagesAndCommitsInOrder(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	f := newSerialSnapshotFixture(t)
	f.opts.PageSize = 2
	f.opts.CDCChunkedSnapshot.ChunkSize = 5
	f.opts.CDCChunkedSnapshot.Execution = "parallel"
	src, table := f.open(t)
	results, err := table.Read(f.ctx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	var first source.RecordBatchResult
	var pages []int64
	var position string
	var snapshot bool
	for result := range results {
		require.NoError(t, result.Err)
		if result.CDCSnapshotChunk != nil {
			require.False(t, snapshot)
			snapshot = true
			require.Equal(t, []string{"5"}, result.CDCSnapshotChunk.Last)
		}
		if snapshot && result.Batch != nil {
			pages = append(pages, result.Batch.NumRows())
			positions := result.Batch.Column(result.Batch.Schema().FieldIndices(CDCLSNColumn)[0]).(*array.String)
			for i := 0; i < positions.Len(); i++ {
				if position == "" {
					position = positions.Value(i)
				}
				require.Equal(t, position, positions.Value(i), "all pages must share one exported snapshot")
			}
		}
		if result.CDCSnapshotChunkEnd != nil {
			require.True(t, snapshot)
			snapshot = false
		}
		sink.apply(t, result)
		if !result.CDCFlush {
			continue
		}
		checkpoint := result.CommitToken.(source.CDCStateCommitToken).SourceCommitToken.(*chunkedCommit)
		if len(checkpoint.state.Tables[serialSnapshotTable].After) > 0 {
			first = result
			break
		}
		require.NoError(t, src.CommitStream(f.ctx, result.CommitToken))
	}
	require.True(t, first.CDCFlush)
	require.False(t, snapshot)
	require.Equal(t, []int64{2, 2, 1}, pages)
	require.Eventually(t, func() bool {
		paths, err := filepath.Glob(filepath.Join(temp, "ingestr-snapshot-chunk-*.arrow"))
		if err != nil || len(paths) != 2 {
			return false
		}
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil || info.Size() == 0 {
				return false
			}
		}
		var slots int
		return f.pool.QueryRow(f.ctx, "SELECT count(*) FROM pg_replication_slots WHERE temporary").Scan(&slots) == nil && slots == 0
	}, 30*time.Second, 20*time.Millisecond, "next chunk must finish reading before the preceding checkpoint is acknowledged")
	persisted, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.Empty(t, persisted.Tables[serialSnapshotTable].After)
	_, err = f.pool.Exec(f.ctx, "DELETE FROM public.serial_items WHERE id = 6; UPDATE public.serial_items SET value = 'updated' WHERE id = 7")
	require.NoError(t, err)
	require.NoError(t, src.CommitStream(f.ctx, first.CommitToken))
	sawPrefetchedDeletedRow, checkedNextCheckpoint := false, false
	for result := range results {
		require.NoError(t, result.Err)
		if result.CDCSnapshotChunk != nil {
			snapshot = true
			require.Equal(t, []string{"5"}, result.CDCSnapshotChunk.After)
		}
		if snapshot && result.Batch != nil {
			require.LessOrEqual(t, result.Batch.NumRows(), int64(2))
			ids := result.Batch.Column(result.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			for i := 0; i < ids.Len(); i++ {
				sawPrefetchedDeletedRow = sawPrefetchedDeletedRow || ids.Value(i) == 6
			}
		}
		if result.CDCSnapshotChunkEnd != nil {
			snapshot = false
		}
		sink.apply(t, result)
		if result.CDCFlush {
			if !checkedNextCheckpoint {
				persisted, err = loadChunkState(f.ctx, f.store)
				require.NoError(t, err)
				require.Equal(t, []string{"5"}, persisted.Tables[serialSnapshotTable].After)
				checkedNextCheckpoint = true
			}
			require.NoError(t, src.CommitStream(f.ctx, result.CommitToken))
		}
	}
	require.True(t, sawPrefetchedDeletedRow)
	require.True(t, checkedNextCheckpoint)
	require.Equal(t, map[int64]string{1: "initial", 2: "initial", 3: "initial", 4: "initial", 5: "initial", 7: "updated", 8: "initial", 9: "initial"}, sink.rows())
	paths, err := filepath.Glob(filepath.Join(temp, "ingestr-snapshot-chunk-*.arrow"))
	require.NoError(t, err)
	require.Empty(t, paths)
}
