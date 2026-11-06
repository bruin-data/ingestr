//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestParallelChunkResumeAcknowledgesBacklogBeforeCatchUp(t *testing.T) {
	t.Run("bootstrap_hard_delete", func(t *testing.T) { testParallelChunkResumeDrain(t, false, true) })
	t.Run("completed_snapshot", func(t *testing.T) { testParallelChunkResumeDrain(t, true, false) })
}

func testParallelChunkResumeDrain(t *testing.T, snapshotComplete, hardDelete bool) {
	t.Helper()
	f := newSerialSnapshotFixture(t)
	f.opts.CDCChunkedSnapshot.Execution = "parallel"
	f.opts.CDCChunkedSnapshot.WALBufferBytes = 64 << 10
	first, table := f.open(t)
	readCtx, stop := context.WithCancel(f.ctx)
	results, err := table.Read(readCtx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	apply := func(result source.RecordBatchResult) {
		sink.apply(t, result)
		if hardDelete {
			for id, row := range sink {
				if row.deleted {
					delete(sink, id)
				}
			}
		}
	}
	firstChunk := f.firstChunk(t, first, results, sink)
	require.NoError(t, first.CommitStream(f.ctx, firstChunk.CommitToken))
	if !snapshotComplete {
		stop()
	}
	for result := range results {
		if snapshotComplete {
			require.NoError(t, result.Err)
			apply(result)
			if result.CDCFlush {
				require.NoError(t, first.CommitStream(f.ctx, result.CommitToken))
			}
		} else if result.Batch != nil {
			result.Batch.Release()
		}
	}
	stop()
	require.NoError(t, first.Close(f.ctx))

	before, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.Equal(t, snapshotComplete, before.complete())
	if !snapshotComplete {
		require.Equal(t, []string{"3"}, before.Tables[serialSnapshotTable].After)
	}
	_, err = f.pool.Exec(f.ctx, `ALTER TABLE public.serial_items ALTER COLUMN value SET STORAGE EXTERNAL;
		DELETE FROM public.serial_items WHERE id IN (2, 8);
		UPDATE public.serial_items SET id = 20, value = 'moved-forward' WHERE id = 3;
		UPDATE public.serial_items SET id = 0, value = 'moved-backward' WHERE id = 9;`)
	require.NoError(t, err)
	// Each transaction carries a wide row; only a small part of the paused
	// backlog fits in the capture buffer before catch-up starts.
	var latest string
	for i := range 30 {
		latest = fmt.Sprintf("update-%d-%s", i, strings.Repeat("x", 50<<10))
		_, err = f.pool.Exec(f.ctx, "UPDATE public.serial_items SET value = $1 WHERE id = 1", latest)
		require.NoError(t, err)
	}
	var rawFinal string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT pg_current_wal_lsn()::text").Scan(&rawFinal))
	final, err := pglogrepl.ParseLSN(rawFinal)
	require.NoError(t, err)
	require.Greater(t, uint64(final-before.Applied), uint64(1<<20), "the pause must accumulate wide-row WAL")

	second, table := f.open(t)
	readCtx, stop = context.WithCancel(f.ctx)
	defer stop()
	results, err = table.Read(readCtx, f.opts)
	require.NoError(t, err)
	var checkpoint *chunkedCommit
	for result := range results {
		require.NoError(t, result.Err)
		apply(result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		require.Empty(t, token.SnapshotPositions, "partial catch-up must not certify a snapshot")
		checkpoint = token.SourceCommitToken.(*chunkedCommit)
		require.Equal(t, before.Tables[serialSnapshotTable].After, checkpoint.state.Tables[serialSnapshotTable].After,
			"catch-up must acknowledge WAL before advancing the current chunk cursor")
		require.Greater(t, checkpoint.state.Applied, before.Applied)
		require.Less(t, checkpoint.state.Applied, final, "the first acknowledgement must precede the end of the backlog")
		persisted, err := loadChunkState(f.ctx, f.store)
		require.NoError(t, err)
		require.Equal(t, before.Applied, persisted.Applied, "unacknowledged progress must not become durable")
		require.NotContains(t, sink.rows(), int64(2), "the checkpoint must include the delete before the next restart")
		if hardDelete {
			require.NotContains(t, sink, int64(2), "hard-delete recovery must work without a destination tombstone")
		}
		require.NoError(t, second.CommitStream(f.ctx, result.CommitToken))
		require.Eventually(t, func() bool {
			var raw string
			if err := f.pool.QueryRow(f.ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = 'serial_slot'").Scan(&raw); err != nil {
				return false
			}
			confirmed, err := pglogrepl.ParseLSN(raw)
			return err == nil && confirmed >= checkpoint.state.Applied
		}, 30*time.Second, 20*time.Millisecond, "the slot must acknowledge incremental catch-up progress")
		t.Logf("paused backlog=%d bytes; acknowledged %s before final %s", final-before.Applied, checkpoint.state.Applied, final)
		stop()
		break
	}
	require.NotNil(t, checkpoint)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, second.Close(f.ctx))

	third, table := f.open(t)
	results, err = table.Read(f.ctx, f.opts)
	require.NoError(t, err)
	for result := range results {
		require.NoError(t, result.Err)
		apply(result)
		if result.CDCFlush {
			require.NoError(t, third.CommitStream(f.ctx, result.CommitToken))
		}
	}
	require.Equal(t, map[int64]string{
		0: "moved-backward", 1: latest, 4: "initial", 5: "initial", 6: "initial", 7: "initial", 20: "moved-forward",
	}, sink.rows(), "replaying the unadvanced chunk must preserve deletes and key moves")
	complete, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.True(t, complete.complete())
	require.GreaterOrEqual(t, complete.Applied, final)
}
