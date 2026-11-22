//go:build integration

package postgres_cdc

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestNormalSnapshotSpoolCapturesThroughBackpressureAndRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	for _, mode := range []string{"destination", "disk", "inmemory"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			container, connString := startConnectorLeasePostgres(t, ctx)
			t.Cleanup(func() { _ = container.Terminate(context.Background()) })
			pool, err := pgxpool.New(ctx, connString)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			_, err = pool.Exec(ctx, `
				CREATE TABLE public.normal_spool_items (id bigint PRIMARY KEY, value text NOT NULL);
				ALTER TABLE public.normal_spool_items REPLICA IDENTITY FULL;
				INSERT INTO public.normal_spool_items SELECT i, 'initial' FROM generate_series(1, 40) i;
				CREATE PUBLICATION normal_spool_pub FOR TABLE public.normal_spool_items;
			`)
			require.NoError(t, err)
			const tableName = "public.normal_spool_items"
			const slot = "normal_spool_slot"
			uri := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=normal_spool_pub&slot=" + slot
			store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
			spool := &source.CDCSnapshotSpoolOptions{
				WALStaging: mode, WALBufferBytes: 1 << 20, Store: store,
			}
			if mode == "disk" {
				spool.Directory = t.TempDir()
			}
			opts := source.ReadOptions{
				PageSize: 2, MaxBatchBytes: 1 << 20, RecordBatchBufferSize: 1, Parallelism: 1,
				Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true, CDCSnapshotSpool: spool,
			}
			open := func() (*PostgresCDCSource, source.SourceTable) {
				src := NewPostgresCDCSource()
				require.NoError(t, src.Connect(ctx, uri))
				t.Cleanup(func() { _ = src.Close(context.Background()) })
				table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
				require.NoError(t, err)
				opts.Schema, err = table.GetSchema(ctx)
				require.NoError(t, err)
				return src, table
			}
			first, table := open()
			readCtx, stopRead := context.WithCancel(ctx)
			defer stopRead()
			results, err := table.Read(readCtx, opts)
			require.NoError(t, err)
			sink := make(chunkRecoverySink)
			var persisted *snapshotSpoolState
			for result := range results {
				require.NoError(t, result.Err)
				sink.apply(t, result)
				if !result.CDCFlush {
					continue
				}
				require.NoError(t, first.CommitStream(ctx, result.CommitToken))
				if len(sink) == 0 {
					continue
				}
				persisted, err = loadSnapshotSpoolState(ctx, store)
				require.NoError(t, err)
				break
			}
			require.NotNil(t, persisted, "snapshot ended before its first data checkpoint")
			require.False(t, persisted.CopyComplete)
			require.False(t, persisted.Complete)
			require.Equal(t, map[int64]string{1: "initial", 2: "initial"}, sink.rows())

			_, err = pool.Exec(ctx, `
				UPDATE public.normal_spool_items SET value = 'updated' WHERE id = 1;
				DELETE FROM public.normal_spool_items WHERE id IN (2, 30);
				UPDATE public.normal_spool_items SET id = 41, value = 'moved-forward' WHERE id = 3;
				UPDATE public.normal_spool_items SET id = 0, value = 'moved-backward' WHERE id = 40;
				UPDATE public.normal_spool_items SET value = 'intermediate' WHERE id = 20;
				UPDATE public.normal_spool_items SET value = 'final' WHERE id = 20;
				INSERT INTO public.normal_spool_items VALUES (-1, 'behind-cursor'), (80, 'beyond-upper');
			`)
			require.NoError(t, err)
			confirmed := func() (pglogrepl.LSN, error) {
				var raw string
				if err := pool.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slot).Scan(&raw); err != nil {
					return 0, err
				}
				return pglogrepl.ParseLSN(raw)
			}
			var captured chunkJournalManifest
			journal := &chunkJournal{epoch: "normal-" + persisted.Epoch}
			if mode == "inmemory" {
				require.Eventually(t, func() bool {
					return first.lag.serverHead.Load() > uint64(persisted.Applied)
				}, 30*time.Second, 20*time.Millisecond, "WAL receiver did not progress while the snapshot consumer was blocked")
				require.Equal(t, persisted.Applied, first.pos.Committed(), "volatile changes must not release source WAL")
				position, err := confirmed()
				require.NoError(t, err)
				require.Equal(t, persisted.Applied, position)
			} else {
				var journalStore source.CDCChunkStore = store
				if mode == "disk" {
					journalStore, err = newSnapshotDiskStore(spool.Directory, persisted.Identity)
					require.NoError(t, err)
				}
				require.Eventually(t, func() bool {
					data, err := journalStore.Load(ctx, journal.manifestKey())
					return err == nil && len(data) > 0 && json.Unmarshal(data, &captured) == nil &&
						captured.Tail > persisted.Consumed && captured.Captured > persisted.Applied &&
						first.pos.Committed() >= captured.Captured
				}, 30*time.Second, 20*time.Millisecond, "WAL was not durably captured while snapshot output was blocked")
				require.Eventually(t, func() bool {
					position, err := confirmed()
					return err == nil && position >= captured.Captured
				}, 30*time.Second, 20*time.Millisecond, "slot feedback did not acknowledge the durable spool")
				t.Logf("snapshot checkpoint=%s, captured and acknowledged WAL=%s", persisted.Applied, captured.Captured)
			}

			stopRead()
			for result := range results {
				if result.Batch != nil {
					result.Batch.Release()
				}
			}
			require.NoError(t, first.Close(ctx))
			afterCrash, err := loadSnapshotSpoolState(ctx, store)
			require.NoError(t, err)
			require.Equal(t, persisted.Tables, afterCrash.Tables, "uncommitted output advanced the durable snapshot cursor")
			require.Equal(t, persisted.Applied, afterCrash.Applied)
			require.Equal(t, persisted.Consumed, afterCrash.Consumed)
			require.Equal(t, persisted.Epoch, afterCrash.Epoch)
			require.False(t, afterCrash.CopyComplete)
			require.False(t, afterCrash.Complete)
			store.resetPageLoads()
			if mode == "disk" {
				diskStore, err := newSnapshotDiskStore(spool.Directory, persisted.Identity)
				require.NoError(t, err)
				manifest, err := diskStore.Load(ctx, journal.manifestKey())
				require.NoError(t, err)
				require.NotEmpty(t, manifest)
				for _, damage := range []string{"missing", "corrupt"} {
					if damage == "missing" {
						require.NoError(t, diskStore.Delete(ctx, journal.manifestKey()))
					} else {
						path := diskStore.(*snapshotDiskStore).path(journal.manifestKey())
						require.NoError(t, os.WriteFile(path, []byte("corrupt manifest"), 0o600))
					}
					rejected, rejectedTable := open()
					rejectedResults, readErr := rejectedTable.Read(ctx, opts)
					if readErr == nil {
						for result := range rejectedResults {
							if result.Batch != nil {
								result.Batch.Release()
								t.Fatal("damaged disk journal produced destination rows")
							}
							if result.Truncate {
								require.True(t, result.CDCSnapshotResumed, "damaged journal must not reset destination rows")
							}
							if result.CDCFlush {
								require.NoError(t, rejected.CommitStream(ctx, result.CommitToken))
							}
							if result.Err != nil {
								readErr = result.Err
							}
						}
					}
					require.ErrorContains(t, readErr, "snapshot WAL", damage)
					require.NoError(t, rejected.Close(ctx))
					afterRejected, err := loadSnapshotSpoolState(ctx, store)
					require.NoError(t, err)
					require.Equal(t, afterCrash, afterRejected, "damaged journal advanced its recovery checkpoint")
					require.NoError(t, diskStore.Save(ctx, journal.manifestKey(), manifest))
				}
			}

			second, table := open()
			results, err = table.Read(ctx, opts)
			require.NoError(t, err)
			var resumed bool
			for result := range results {
				require.NoError(t, result.Err)
				if result.Truncate {
					require.True(t, result.CDCSnapshotResumed, "restart reset the durably copied snapshot prefix")
					resumed = true
				}
				sink.apply(t, result)
				if result.CDCFlush {
					require.NoError(t, second.CommitStream(ctx, result.CommitToken))
				}
			}
			require.True(t, resumed)
			expected := make(map[int64]string)
			rows, err := pool.Query(ctx, "SELECT id, value FROM public.normal_spool_items")
			require.NoError(t, err)
			for rows.Next() {
				var id int64
				var value string
				require.NoError(t, rows.Scan(&id, &value))
				expected[id] = value
			}
			require.NoError(t, rows.Err())
			rows.Close()
			require.Equal(t, expected, sink.rows())
			complete, err := loadSnapshotSpoolState(ctx, store)
			require.NoError(t, err)
			require.True(t, complete.CopyComplete)
			require.True(t, complete.Complete)
			if mode != "inmemory" {
				require.GreaterOrEqual(t, complete.Applied, captured.Captured)
			}
			if mode == "destination" {
				for page := persisted.Consumed; page < captured.Tail; page++ {
					require.True(t, store.pageWasLoaded(journal.pageKey(page)), "restart did not replay the durable journal")
				}
			}
		})
	}
}

func TestNormalSnapshotSpoolStandbyKeepsSnapshotWhileCapturing(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL standby logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	primary, standbyURI := startStandbyPair(t, ctx)
	_, err := primary.Exec(ctx, `
		CREATE TABLE public.standby_spool_items (id bigint PRIMARY KEY, value text NOT NULL);
		ALTER TABLE public.standby_spool_items REPLICA IDENTITY FULL;
		INSERT INTO public.standby_spool_items SELECT i, 'initial' FROM generate_series(1, 40) i;
		CREATE TABLE public.unpublished (id bigserial PRIMARY KEY, value text);
		CREATE PUBLICATION standby_pub FOR TABLE public.standby_spool_items;
	`)
	require.NoError(t, err)
	waitForStandbyReplay(t, ctx, primary, standbyURI)
	standby, err := pgxpool.New(ctx, strings.Replace(strings.Split(standbyURI, "&publication=")[0], "postgres+cdc://", "postgres://", 1))
	require.NoError(t, err)
	t.Cleanup(standby.Close)
	nudgeCtx, stopNudge := context.WithCancel(ctx)
	nudgeDone := make(chan struct{})
	go func() {
		defer close(nudgeDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-nudgeCtx.Done():
				return
			case <-ticker.C:
				_, _ = primary.Exec(nudgeCtx, "SELECT pg_log_standby_snapshot()")
			}
		}
	}()
	defer func() { stopNudge(); <-nudgeDone }()
	const tableName = "public.standby_spool_items"
	const slot = "standby_spool_slot"
	src := connectStandbySource(t, ctx, standbyURI, slot)
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	store := &chunkRecoveryStore{values: make(map[string][]byte), loadedPages: make(map[string]bool)}
	opts := source.ReadOptions{
		PageSize: 2, MaxBatchBytes: 1 << 20, RecordBatchBufferSize: 1, Parallelism: 1, Schema: tableSchema,
		Streaming: true, CDCManagedState: true, CDCSnapshotReplace: true,
		CDCSnapshotSpool: &source.CDCSnapshotSpoolOptions{
			WALStaging: "destination", WALBufferBytes: 1 << 20, Store: store,
		},
	}
	readCtx, stopRead := context.WithCancel(ctx)
	defer stopRead()
	results, err := table.Read(readCtx, opts)
	require.NoError(t, err)
	sink := make(chunkRecoverySink)
	baseline := make(map[int64]string)
	copyComplete := false
	consume := func(result source.RecordBatchResult) {
		require.NoError(t, result.Err)
		if result.Batch != nil && !copyComplete {
			batch := result.Batch
			ids := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			values := batch.Column(batch.Schema().FieldIndices("value")[0]).(*array.String)
			for i := 0; i < int(batch.NumRows()); i++ {
				baseline[ids.Value(i)] = values.Value(i)
			}
		}
		sink.apply(t, result)
		if result.CDCFlush {
			token := result.CommitToken.(source.CDCStateCommitToken)
			checkpoint := token.SourceCommitToken.(*snapshotSpoolCommit)
			copyComplete = checkpoint.state.CopyComplete
			require.NoError(t, src.CommitStream(ctx, result.CommitToken))
		}
	}
	for result := range results {
		consume(result)
		if result.CDCFlush && len(sink) > 0 {
			break
		}
	}
	require.Equal(t, map[int64]string{1: "initial", 2: "initial"}, sink.rows())
	stopRead()
	for result := range results {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.NoError(t, src.Close(ctx))
	src = connectStandbySource(t, ctx, standbyURI, slot)
	table, err = src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	results, err = table.Read(ctx, opts)
	require.NoError(t, err)
	var resumed bool
	for result := range results {
		if result.Truncate {
			require.True(t, result.CDCSnapshotResumed)
			resumed = true
		}
		consume(result)
		if result.CDCFlush && len(sink) > 2 {
			break
		}
	}
	require.True(t, resumed)
	require.Len(t, sink, 4)
	checkpoint, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.False(t, checkpoint.CopyComplete)
	_, err = primary.Exec(ctx, `
		UPDATE public.standby_spool_items SET value = 'updated' WHERE id IN (1, 20);
		DELETE FROM public.standby_spool_items WHERE id IN (2, 30);
		UPDATE public.standby_spool_items SET id = 0, value = 'moved' WHERE id = 40;
		INSERT INTO public.standby_spool_items VALUES (80, 'inserted');
	`)
	require.NoError(t, err)
	writeUnpublished(t, ctx, primary, 40)
	waitForStandbyReplay(t, ctx, primary, standbyURI)
	manifestKey := (&chunkJournal{epoch: "normal-" + checkpoint.Epoch}).manifestKey()
	var captured chunkJournalManifest
	require.Eventually(t, func() bool {
		data, err := store.Load(ctx, manifestKey)
		return err == nil && len(data) > 0 && json.Unmarshal(data, &captured) == nil &&
			captured.Tail > checkpoint.Consumed && captured.Captured > checkpoint.Applied && src.pos.Committed() >= captured.Captured
	}, time.Minute, 20*time.Millisecond, "standby WAL capture stalled behind snapshot backpressure")
	require.Eventually(t, func() bool {
		var confirmed string
		if err := standby.QueryRow(ctx, "SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name = $1", slot).Scan(&confirmed); err != nil {
			return false
		}
		position, err := pglogrepl.ParseLSN(confirmed)
		return err == nil && position >= captured.Captured
	}, time.Minute, 20*time.Millisecond, "standby did not release acknowledged spool WAL")
	var temporarySlots int
	require.NoError(t, standby.QueryRow(ctx, "SELECT count(*) FROM pg_replication_slots WHERE temporary").Scan(&temporarySlots))
	require.Zero(t, temporarySlots, "snapshot exporter must not pin WAL after handoff")
	var retainedWAL int64
	require.NoError(t, standby.QueryRow(ctx, `
		SELECT pg_wal_lsn_diff(pg_last_wal_replay_lsn(), restart_lsn)::bigint
		FROM pg_replication_slots WHERE slot_name = $1
	`, slot).Scan(&retainedWAL))
	require.LessOrEqual(t, retainedWAL, int64(2<<30))
	for result := range results {
		consume(result)
	}
	require.True(t, copyComplete)
	require.Len(t, baseline, 40)
	for id := int64(1); id <= 40; id++ {
		require.Equal(t, "initial", baseline[id], "the exported snapshot changed while WAL was captured")
	}
	expected := make(map[int64]string)
	rows, err := primary.Query(ctx, "SELECT id, value FROM public.standby_spool_items")
	require.NoError(t, err)
	for rows.Next() {
		var id int64
		var value string
		require.NoError(t, rows.Scan(&id, &value))
		expected[id] = value
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, expected, sink.rows())
	complete, err := loadSnapshotSpoolState(ctx, store)
	require.NoError(t, err)
	require.True(t, complete.Complete)
	require.GreaterOrEqual(t, complete.Applied, captured.Captured)
}
