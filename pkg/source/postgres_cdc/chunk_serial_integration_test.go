//go:build integration

package postgres_cdc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const serialSnapshotTable = "public.serial_items"

type serialSnapshotFixture struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	uri   string
	store *chunkRecoveryStore
	opts  source.ReadOptions
}

func newSerialSnapshotFixture(t *testing.T) *serialSnapshotFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	container, connString := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.serial_items (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.serial_items REPLICA IDENTITY FULL;
		INSERT INTO public.serial_items SELECT i, 'initial' FROM generate_series(1, 9) i;
		CREATE PUBLICATION serial_pub FOR TABLE public.serial_items;
	`)
	require.NoError(t, err)
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	return &serialSnapshotFixture{
		ctx: ctx, pool: pool, store: store,
		uri: strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=serial_pub&slot=serial_slot",
		opts: source.ReadOptions{
			PageSize: 3, MaxBatchBytes: 1 << 20, Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
			CDCChunkedSnapshot: &source.CDCChunkedSnapshotOptions{
				ChunkSize: 3, ChunkTimeout: 90 * time.Second, Execution: "serial", WALStaging: "inmemory", WALBufferBytes: 1 << 20, Store: store,
			},
		},
	}
}

func (f *serialSnapshotFixture) open(t *testing.T) (*PostgresCDCSource, source.SourceTable) {
	t.Helper()
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(f.ctx, f.uri))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	table, err := src.GetTable(f.ctx, source.TableRequest{Name: serialSnapshotTable})
	require.NoError(t, err)
	f.opts.Schema, err = table.GetSchema(f.ctx)
	require.NoError(t, err)
	return src, table
}

func (f *serialSnapshotFixture) firstChunk(t *testing.T, src *PostgresCDCSource, results <-chan source.RecordBatchResult, sink chunkRecoverySink) source.RecordBatchResult {
	t.Helper()
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		checkpoint := token.SourceCommitToken.(*chunkedCommit)
		require.False(t, checkpoint.state.SnapshotComplete)
		require.Empty(t, token.SnapshotPositions)
		if len(checkpoint.state.Tables[serialSnapshotTable].After) > 0 {
			return result
		}
		require.NoError(t, src.CommitStream(f.ctx, result.CommitToken))
	}
	t.Fatal("serial reader ended before the first chunk checkpoint")
	return source.RecordBatchResult{}
}

func (f *serialSnapshotFixture) assertSnapshotIdle(t *testing.T, initialPID uint32, boundary pglogrepl.LSN) {
	t.Helper()
	var active bool
	var confirmed string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT active, confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = 'serial_slot'`).Scan(&active, &confirmed))
	require.False(t, active, "serial snapshots must not start logical replication before all tables are copied")
	lsn, err := pglogrepl.ParseLSN(confirmed)
	require.NoError(t, err)
	require.Equal(t, boundary, lsn, "snapshot checkpoints must retain the original WAL replay boundary")
	var pinned, streaming int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid = $1 AND backend_xmin IS NOT NULL`, initialPID).Scan(&pinned))
	require.Zero(t, pinned, "the persistent slot's exported snapshot must be released before copying chunks")
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_replication WHERE state = 'streaming'`).Scan(&streaming))
	require.Zero(t, streaming)
}

func TestSerialChunkedSnapshotResumesAndCatchesUp(t *testing.T) {
	f := newSerialSnapshotFixture(t)
	first, table := f.open(t)
	initialPID := first.replConn.PID()
	readCtx, stopRead := context.WithCancel(f.ctx)
	defer stopRead()
	results, err := table.Read(readCtx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	firstChunk := f.firstChunk(t, first, results, sink)
	checkpoint := firstChunk.CommitToken.(source.CDCStateCommitToken).SourceCommitToken.(*chunkedCommit)
	boundary := checkpoint.state.Applied
	require.Equal(t, []string{"3"}, checkpoint.state.Tables[serialSnapshotTable].After)
	require.Equal(t, FormatLSN(boundary), checkpoint.state.Tables[serialSnapshotTable].Boundary)
	require.Equal(t, map[int64]string{1: "initial", 2: "initial", 3: "initial"}, sink.rows())
	f.assertSnapshotIdle(t, initialPID, boundary)

	_, err = f.pool.Exec(f.ctx, `
		UPDATE public.serial_items SET value = 'updated' WHERE id = 1;
		DELETE FROM public.serial_items WHERE id IN (2, 8);
		UPDATE public.serial_items SET id = 8, value = 'moved-forward' WHERE id = 3;
		UPDATE public.serial_items SET id = 0, value = 'moved-backward' WHERE id = 9;
		UPDATE public.serial_items SET value = 'intermediate' WHERE id = 5;
		UPDATE public.serial_items SET value = 'final' WHERE id = 5;
		INSERT INTO public.serial_items VALUES (-1, 'behind-cursor'), (20, 'beyond-upper');
	`)
	require.NoError(t, err)
	var changesBeyondBoundary bool
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT pg_current_wal_lsn() > $1::pg_lsn", boundary.String()).Scan(&changesBeyondBoundary))
	require.True(t, changesBeyondBoundary)
	f.assertSnapshotIdle(t, initialPID, boundary)
	require.NoError(t, first.CommitStream(f.ctx, firstChunk.CommitToken))
	persisted, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.Equal(t, boundary, persisted.Applied)
	require.False(t, persisted.SnapshotComplete)
	require.Equal(t, boundary, first.pos.Committed())
	require.Equal(t, []string{"3"}, persisted.Tables[serialSnapshotTable].After)

	stoppedBeforeSecondMerge := false
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			ids := result.Batch.Column(result.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			require.Equal(t, []int64{4, 5, 6}, ids.Int64Values())
			result.Batch.Release()
			stopRead()
			stoppedBeforeSecondMerge = true
			break
		}
		require.False(t, result.CDCFlush)
	}
	require.True(t, stoppedBeforeSecondMerge)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, first.Close(f.ctx))
	afterCrash, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.Equal(t, persisted, afterCrash)

	switched, switchedTable := f.open(t)
	wrongOptions := f.opts
	parallel := *f.opts.CDCChunkedSnapshot
	parallel.Execution = "parallel"
	wrongOptions.CDCChunkedSnapshot = &parallel
	rejected, err := switchedTable.Read(f.ctx, wrongOptions)
	if err == nil {
		for result := range rejected {
			if result.Batch != nil {
				result.Batch.Release()
			}
			require.False(t, result.Truncate, "mode validation must precede a destination reset")
			require.False(t, result.CDCFlush)
			if result.Err != nil {
				err = result.Err
			}
		}
	}
	require.ErrorContains(t, err, "execution")
	require.NoError(t, switched.Close(f.ctx))
	afterRejectedMode, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.Equal(t, persisted, afterRejectedMode)

	second, table := f.open(t)
	results, err = table.Read(f.ctx, f.opts)
	require.NoError(t, err)
	var resumed, sawResumedChunk, copiedAll bool
	var rowFiveSnapshot string
	previousApplied := boundary
	for result := range results {
		require.NoError(t, result.Err)
		if result.Truncate {
			require.True(t, result.CDCSnapshotResumed, "restart must retain durable incomplete snapshot rows")
			resumed = true
		}
		if result.Batch != nil && !copiedAll {
			batch := result.Batch
			ids := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			positions := batch.Column(batch.Schema().FieldIndices(CDCLSNColumn)[0]).(*array.String)
			if !sawResumedChunk {
				require.Equal(t, []int64{4, 5, 6}, ids.Int64Values(), "restart recopied the acknowledged first chunk")
				sawResumedChunk = true
			}
			for i, id := range ids.Int64Values() {
				if id == 5 {
					rowFiveSnapshot = positions.Value(i)
				}
			}
		}
		sink.apply(t, result)
		if result.CDCFlush {
			token := result.CommitToken.(source.CDCStateCommitToken)
			state := token.SourceCommitToken.(*chunkedCommit).state
			require.GreaterOrEqual(t, state.Applied, previousApplied, "catchup checkpoint progress must be monotonic")
			if !state.SnapshotComplete {
				require.Empty(t, token.SnapshotPositions)
				if !copiedAll {
					require.Equal(t, boundary, state.Applied)
					f.assertSnapshotIdle(t, second.replConn.PID(), boundary)
					copiedAll = state.complete()
				} else {
					require.True(t, state.complete(), "catchup checkpoints require every snapshot chunk to be copied")
				}
			}
			require.NoError(t, second.CommitStream(f.ctx, result.CommitToken))
			previousApplied = state.Applied
		}
	}
	require.True(t, resumed)
	require.True(t, sawResumedChunk)
	require.True(t, copiedAll)
	require.Equal(t, map[int64]string{
		-1: "behind-cursor", 0: "moved-backward", 1: "updated", 4: "initial",
		5: "final", 6: "initial", 7: "initial", 8: "moved-forward", 20: "beyond-upper",
	}, sink.rows())
	require.NotEmpty(t, rowFiveSnapshot)
	require.Equal(t, rowFiveSnapshot, sink[5].position, "older replay must not overwrite a later chunk's snapshot position")
	complete, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.True(t, complete.SnapshotComplete)
	require.True(t, complete.complete())
	require.Greater(t, complete.Applied, boundary)
	var confirmed bool
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT confirmed_flush_lsn >= $1::pg_lsn FROM pg_replication_slots WHERE slot_name = 'serial_slot'", complete.Applied.String()).Scan(&confirmed))
	require.True(t, confirmed, "finite catchup must confirm its durable checkpoint before returning")
}

func TestSerialChunkedSnapshotResumesCopiedTablesBeforeCertification(t *testing.T) {
	f := newSerialSnapshotFixture(t)
	first, table := f.open(t)
	readCtx, stopRead := context.WithCancel(f.ctx)
	defer stopRead()
	results, err := table.Read(readCtx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	firstChunk := f.firstChunk(t, first, results, sink)
	boundary := firstChunk.CommitToken.(source.CDCStateCommitToken).SourceCommitToken.(*chunkedCommit).state.Applied
	_, err = f.pool.Exec(f.ctx, "UPDATE public.serial_items SET value = 'updated' WHERE id = 1; INSERT INTO public.serial_items VALUES (20, 'beyond-upper')")
	require.NoError(t, err)
	require.NoError(t, first.CommitStream(f.ctx, firstChunk.CommitToken))
	var stoppedBeforeCertification bool
	for result := range results {
		require.NoError(t, result.Err)
		sink.apply(t, result)
		if !result.CDCFlush {
			continue
		}
		token := result.CommitToken.(source.CDCStateCommitToken)
		state := token.SourceCommitToken.(*chunkedCommit).state
		if state.SnapshotComplete {
			require.Equal(t, map[string]string{serialSnapshotTable: FormatLSN(boundary)}, token.SnapshotPositions)
			stopRead()
			stoppedBeforeCertification = true
			break
		}
		require.Empty(t, token.SnapshotPositions)
		require.NoError(t, first.CommitStream(f.ctx, result.CommitToken))
	}
	require.True(t, stoppedBeforeCertification)
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, first.Close(f.ctx))
	persisted, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.True(t, persisted.complete(), "all copy cursors must survive interruption before certification")
	require.False(t, persisted.SnapshotComplete)
	rowFourPosition := sink[4].position
	require.NotEmpty(t, rowFourPosition)

	second, table := f.open(t)
	results, err = table.Read(f.ctx, f.opts)
	require.NoError(t, err)
	var resumed, certified bool
	for result := range results {
		require.NoError(t, result.Err)
		if result.Truncate {
			require.True(t, result.CDCSnapshotResumed)
			resumed = true
		}
		if result.Batch != nil {
			ids := result.Batch.Column(result.Batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			for _, id := range ids.Int64Values() {
				require.Contains(t, []int64{1, 20}, id, "a fully copied table must resume WAL replay without another snapshot")
			}
		}
		sink.apply(t, result)
		if result.CDCFlush {
			token := result.CommitToken.(source.CDCStateCommitToken)
			if len(token.SnapshotPositions) > 0 {
				require.Equal(t, map[string]string{serialSnapshotTable: FormatLSN(boundary)}, token.SnapshotPositions)
				certified = true
			}
			require.NoError(t, second.CommitStream(f.ctx, result.CommitToken))
		}
	}
	require.True(t, resumed)
	require.True(t, certified)
	require.Equal(t, rowFourPosition, sink[4].position)
	require.Equal(t, map[int64]string{
		1: "updated", 2: "initial", 3: "initial", 4: "initial", 5: "initial",
		6: "initial", 7: "initial", 8: "initial", 9: "initial", 20: "beyond-upper",
	}, sink.rows())
	complete, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.True(t, complete.SnapshotComplete)
	require.GreaterOrEqual(t, complete.Applied, persisted.Applied)
}

func TestSerialChunkedSnapshotRejectsTruncateDuringCopy(t *testing.T) {
	f := newSerialSnapshotFixture(t)
	src, table := f.open(t)
	results, err := table.Read(f.ctx, f.opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	firstChunk := f.firstChunk(t, src, results, sink)
	boundary := firstChunk.CommitToken.(source.CDCStateCommitToken).SourceCommitToken.(*chunkedCommit).state.Applied
	_, err = f.pool.Exec(f.ctx, "TRUNCATE public.serial_items; INSERT INTO public.serial_items VALUES (4, 'after-truncate')")
	require.NoError(t, err)
	require.NoError(t, src.CommitStream(f.ctx, firstChunk.CommitToken))
	var terminal error
	var copiedAll bool
	for result := range results {
		if result.Err != nil {
			terminal = result.Err
			continue
		}
		sink.apply(t, result)
		if result.CDCFlush {
			token := result.CommitToken.(source.CDCStateCommitToken)
			state := token.SourceCommitToken.(*chunkedCommit).state
			require.False(t, state.SnapshotComplete, "TRUNCATE must not certify a mixed snapshot")
			if !copiedAll {
				require.Equal(t, boundary, state.Applied)
				copiedAll = state.complete()
			}
			require.Empty(t, token.SnapshotPositions)
			require.NoError(t, src.CommitStream(f.ctx, result.CommitToken))
		}
	}
	require.ErrorContains(t, terminal, "truncated during bootstrap")
	persisted, err := loadChunkState(f.ctx, f.store)
	require.NoError(t, err)
	require.False(t, persisted.SnapshotComplete)
	require.GreaterOrEqual(t, persisted.Applied, boundary)
}
