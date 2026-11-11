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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestFiniteInitialSnapshotResumeRepairsWALOrReplaces(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	for _, single := range []bool{false, true} {
		t.Run(fmt.Sprintf("single_table_%t", single), func(t *testing.T) {
			testFiniteInitialSnapshotResume(t, ctx, pool, uri, single)
		})
	}
}

func testFiniteInitialSnapshotResume(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uri string, single bool) {
	for _, slotState := range []string{"retained", "advanced", "missing"} {
		t.Run(slotState, func(t *testing.T) {
			name := fmt.Sprintf("finite_resume_%t_%s", single, slotState)
			publication, slot := name+"_pub", name+"_slot"
			_, err := pool.Exec(ctx, fmt.Sprintf(`
				CREATE TABLE %s (id bigint PRIMARY KEY, value text NOT NULL);
				ALTER TABLE %s REPLICA IDENTITY FULL;
				INSERT INTO %s SELECT id, 'value-' || id FROM generate_series(1, 12) id;
				CREATE TABLE %s_other (id bigint PRIMARY KEY, value text NOT NULL);
				INSERT INTO %s_other VALUES (1000, 'unselected');
				CREATE PUBLICATION %s FOR TABLE %s, %s_other;
			`, name, name, name, name, name, publication, name, name))
			require.NoError(t, err)
			tableName := publicationTableFullName("public", name)
			opts := source.MultiTableReadOptions{
				ReadOptions: source.ReadOptions{
					PageSize: 1, Parallelism: 1, CDCManagedState: true, CDCSnapshotReplace: true, CDCSnapshotProgress: true,
					CDCSlotSuffix: "finite",
				},
				KnownTables: []string{tableName},
			}
			connectionURI := strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=" + publication
			if single {
				slot = generateSlotName(tableName, publication, opts.CDCSlotSuffix)
			} else {
				connectionURI += "&slot=" + slot
			}
			t.Cleanup(func() {
				waitReplicationSlotReleased(ctx, pool, slot)
				_, err := pool.Exec(ctx, "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1", slot)
				require.NoError(t, err)
			})
			connect := func() *PostgresCDCSource {
				src := NewPostgresCDCSource()
				require.NoError(t, src.Connect(ctx, connectionURI))
				return src
			}
			read := func(readCtx context.Context, src *PostgresCDCSource) (<-chan source.RecordBatchResult, error) {
				if !single {
					return src.ReadAll(readCtx, opts)
				}
				table, err := src.GetTable(readCtx, source.TableRequest{Name: tableName})
				require.NoError(t, err)
				return table.Read(readCtx, opts.ReadOptions)
			}
			first := connect()
			t.Cleanup(func() { _ = first.Close(context.Background()) })
			readCtx, stop := context.WithCancel(ctx)
			results, err := read(readCtx, first)
			require.NoError(t, err)
			sink := make(chunkRecoverySink)
			var saved source.CDCSnapshotProgress
			for result := range results {
				require.NoError(t, result.Err)
				require.Nil(t, result.TableInfo, "finite reads must not announce unselected tables")
				if result.Batch != nil {
					require.Equal(t, tableName, result.TableName)
				}
				sink.apply(t, result)
				if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok {
					progress := token.SnapshotProgress[tableName]
					if len(progress.Ranges) == 1 && len(progress.Ranges[0].Key) == 1 && progress.Ranges[0].Key[0] == "4" {
						saved = progress.Clone()
						stop()
						break
					}
				}
			}
			stop()
			for result := range results {
				if result.Batch != nil {
					result.Batch.Release()
				}
			}
			require.NoError(t, first.Close(context.Background()))
			require.NotEmpty(t, saved.Attempt, "finite snapshots must emit resumable progress before completion")
			require.Len(t, sink.rows(), 4)
			boundary, err := pglogrepl.ParseLSN(saved.Boundary)
			require.NoError(t, err)
			retained, err := snapshotResumeWALAvailable(ctx, pool, slot, boundary)
			require.NoError(t, err)
			require.True(t, retained, "interrupted finite reads must retain the original WAL boundary")
			_, err = pool.Exec(ctx, fmt.Sprintf(`
				UPDATE %s SET value = 'updated-prefix' WHERE id = 1;
				DELETE FROM %s WHERE id = 2;
				UPDATE %s SET id = 0, value = 'moved-backward' WHERE id = 8;
				UPDATE %s SET id = 30, value = 'moved-forward' WHERE id = 3;
				INSERT INTO %s VALUES (-1, 'new-prefix');
				UPDATE %s SET value = 'updated-suffix' WHERE id = 9;
			`, name, name, name, name, name, name))
			require.NoError(t, err)
			switch slotState {
			case "advanced":
				_, err = pool.Exec(ctx, "SELECT pg_replication_slot_advance($1, pg_current_wal_lsn())", slot)
				require.NoError(t, err)
			case "missing":
				_, err = pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slot)
				require.NoError(t, err)
			}
			resumed := connect()
			defer func() { _ = resumed.Close(context.Background()) }()
			opts.Parallelism = 4
			opts.CDCSnapshotResumes = map[string]source.CDCSnapshotProgress{tableName: saved}
			opts.CDCSnapshotResume = &saved
			results, err = read(ctx, resumed)
			require.NoError(t, err)
			markers, walRows := 0, int64(0)
			var snapshotKeys []string
			for result := range results {
				require.NoError(t, result.Err)
				require.Nil(t, result.TableInfo)
				if result.Truncate {
					markers++
					require.Equal(t, slotState == "retained", result.CDCSnapshotResumed)
				}
				if result.Batch != nil {
					require.Equal(t, tableName, result.TableName)
					if result.CDCSnapshot {
						ids := result.Batch.Column(result.Batch.Schema().FieldIndices("id")[0])
						for i := 0; i < ids.Len(); i++ {
							snapshotKeys = append(snapshotKeys, ids.ValueStr(i))
						}
					} else {
						walRows += result.Batch.NumRows()
					}
				}
				sink.apply(t, result)
			}
			require.Equal(t, 1, markers)
			expected := map[int64]string{
				-1: "new-prefix", 0: "moved-backward", 1: "updated-prefix", 4: "value-4", 5: "value-5", 6: "value-6",
				7: "value-7", 9: "updated-suffix", 10: "value-10", 11: "value-11", 12: "value-12", 30: "moved-forward",
			}
			require.Equal(t, expected, sink.rows(), "resume must repair changed, deleted, inserted, and moved keys behind the durable cursor")
			if slotState == "retained" {
				require.ElementsMatch(t, []string{"5", "6", "7", "9", "10", "11", "12", "30"}, snapshotKeys,
					"a changed reader count must preserve the saved range cursor")
				require.Positive(t, walRows, "resuming without a completed table still requires WAL repair")
			} else {
				require.Len(t, snapshotKeys, len(expected), "unavailable original WAL requires a complete replacement snapshot")
				require.Zero(t, walRows, "fresh finite snapshots finish at the exported snapshot boundary")
			}
			require.NotEmpty(t, resumed.CDCState().Position)
			require.Equal(t, slot, resumed.caughtUpSlot, "single-table reads must retain their persistent slot name")
			require.Len(t, resumed.CDCState().SnapshotPositions, 1)
			require.NoError(t, resumed.FinalizeBatch(ctx))
			if !single {
				return
			}
			checkpoint, err := parseStoredPostgresLSN(resumed.CDCState().Position)
			require.NoError(t, err)
			require.NoError(t, resumed.Close(context.Background()))
			_, err = pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET value = 'after-checkpoint' WHERE id = 1; UPDATE %s_other SET value = 'still-unselected'`, name, name))
			require.NoError(t, err)
			incremental := connect()
			defer func() { _ = incremental.Close(context.Background()) }()
			opts.CDCResumeLSN = FormatChangeLSN(checkpoint, 42)
			opts.CDCResumeIncarnation = saved.Incarnation
			opts.CDCResumeSchemaFingerprint = saved.SchemaFingerprint
			results, err = read(ctx, incremental)
			require.NoError(t, err)
			for result := range results {
				require.NoError(t, result.Err)
				require.False(t, result.Truncate)
				require.False(t, result.CDCSnapshot, "a completed row-level LSN resumes WAL without another snapshot")
				require.Nil(t, result.TableInfo)
				if result.Batch != nil {
					require.Equal(t, tableName, result.TableName)
				}
				sink.apply(t, result)
			}
			expected[1] = "after-checkpoint"
			require.Equal(t, expected, sink.rows())
			require.Equal(t, slot, incremental.caughtUpSlot)
			require.NoError(t, incremental.FinalizeBatch(ctx))
		})
	}
}
