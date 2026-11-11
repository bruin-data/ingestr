//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestSnapshotJSONWireValuesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL snapshot transactions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, uri := startConnectorLeasePostgres(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	poolConfig, err := pgxpool.ParseConfig(uri)
	require.NoError(t, err)
	poolConfig.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE TABLE public.json_snapshot_items (id bigint PRIMARY KEY, document json, binary_document jsonb)`)
	require.NoError(t, err)
	values := []any{
		nil,
		`null`,
		`"quoted \"text\" with \\ slash and \n newline"`,
		`[null, true, false, "text", {"nested": [1, 2]}]`,
		`{ "z": 9007199254740993, "a": 1234567890.12345678901234567890, "empty": {} }`,
		`true`,
		`false`,
		`9007199254740993`,
		`1234567890.12345678901234567890`,
	}
	for i, value := range values {
		_, err = pool.Exec(ctx, `INSERT INTO public.json_snapshot_items VALUES ($1, $2::text::json, $2::text::jsonb)`, i+1, value)
		require.NoError(t, err)
	}

	expected := make(map[int64][2]*string, len(values))
	rows, err := pool.Query(ctx, `SELECT id, document::text, binary_document::text FROM public.json_snapshot_items`)
	require.NoError(t, err)
	for rows.Next() {
		var id int64
		var texts [2]*string
		require.NoError(t, rows.Scan(&id, &texts[0], &texts[1]))
		expected[id] = texts
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, expected, len(values))

	for _, parallelism := range []int{1, 4} {
		t.Run(fmt.Sprintf("readers_%d", parallelism), func(t *testing.T) {
			conn, err := pgx.Connect(ctx, uri)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close(context.Background()) })
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
			var exported string
			require.NoError(t, tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&exported))
			snapshot := &Snapshot{
				source:    &PostgresCDCSource{queryPool: pool},
				tableName: "public.json_snapshot_items",
				tableSchema: addCDCColumns(&schema.TableSchema{
					Columns: []schema.Column{
						{Name: "id", DataType: schema.TypeInt64},
						{Name: "document", DataType: schema.TypeJSON, Nullable: true},
						{Name: "binary_document", DataType: schema.TypeJSON, Nullable: true},
					},
					PrimaryKeys: []string{"id"},
				}),
			}
			results := make(chan source.RecordBatchResult, len(values))
			done := make(chan error, 1)
			go func() {
				done <- snapshot.ReadWithSnapshot(ctx, exported, pglogrepl.LSN(42), results, source.ReadOptions{
					Parallelism: parallelism, PageSize: 2, MaxBatchBytes: 256,
				}, nil, nil)
				close(results)
			}()
			seen := make(map[int64]bool, len(values))
			for result := range results {
				if result.Batch == nil {
					continue
				}
				batch := result.Batch
				func() {
					defer batch.Release()
					ids := batch.Column(0).(*array.Int64)
					for row := 0; row < int(batch.NumRows()); row++ {
						id := ids.Value(row)
						require.False(t, seen[id], "duplicate snapshot row %d", id)
						seen[id] = true
						want, ok := expected[id]
						require.True(t, ok, "unexpected snapshot row %d", id)
						for column, text := range want {
							jsonValues := batch.Column(column + 1).(*schema.JSONArray)
							storage := jsonValues.Storage().(*array.String)
							if text == nil {
								require.True(t, jsonValues.IsNull(row), "SQL NULL for row %d column %d", id, column)
								require.True(t, storage.IsNull(row))
								continue
							}
							require.False(t, jsonValues.IsNull(row), "JSON value for row %d column %d", id, column)
							require.Equal(t, *text, storage.Value(row), "row %d column %d", id, column)
						}
					}
				}()
			}
			require.NoError(t, <-done)
			require.Len(t, seen, len(expected))
		})
	}
}
