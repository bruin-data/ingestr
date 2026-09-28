//go:build integration

package integration

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestStarRocksDeduplication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	dsn, uri := startStarRocksContainer(ctx, t)
	waitForStarRocksBackend(t, dsn)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := starrocks.NewStarRocksDestination()
	u, err := url.Parse(uri)
	require.NoError(t, err)
	params := u.Query()
	params.Set("replication_num", "1")
	u.RawQuery = params.Encode()
	require.NoError(t, dest.Connect(ctx, u.String()))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
		{Name: "__bruin_dedup_rn", DataType: schema.TypeInt64},
	}}
	// Match the strategies: raw staging has no PK; the merge destination does.
	execEventually(t, db, "CREATE DATABASE IF NOT EXISTS dedup")
	execEventually(t, db, "CREATE TABLE dedup.ready (id BIGINT) DISTRIBUTED BY RANDOM PROPERTIES ('replication_num'='1')")
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: "dedup.raw", Schema: tableSchema,
	}))
	_, err = db.Exec(`INSERT INTO dedup.raw VALUES
		(1, 'v1-old', 10, 100), (1, 'v1-latest', 11, 101), (2, 'v2', 20, 200),
		(3, 'v3-latest', 31, 301), (3, 'v3-old', 30, 300),
		(4, 'v4-null', NULL, 400), (4, 'v4-latest', -5, 401), (4, 'v4-old', -9, 402),
		(5, 'v5-null', NULL, 500)`)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM dedup.raw").Scan(&count))
	require.Equal(t, 9, count, "raw staging must preserve every version")

	for _, incrementalKey := range []string{"score", ""} {
		t.Run("incremental_key="+incrementalKey, func(t *testing.T) {
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table: "dedup.normalized", Schema: tableSchema, PrimaryKeys: []string{"id"}, DropFirst: true,
			}))
			require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
				StagingTable: "dedup.raw", TargetTable: "dedup.normalized",
				PrimaryKeys: []string{"id"}, Columns: tableSchema.ColumnNames(), IncrementalKey: incrementalKey,
			}))
			require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
				StagingTable: "dedup.normalized", TargetTable: "dedup.result", Schema: tableSchema,
			}))
			rows, err := db.Query("SELECT id, name, score, __bruin_dedup_rn FROM dedup.result ORDER BY id")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			var names []string
			var scores []sql.NullFloat64
			var userRanks []int64
			for rows.Next() {
				var id int
				var name string
				var score sql.NullFloat64
				var userRank int64
				require.NoError(t, rows.Scan(&id, &name, &score, &userRank))
				require.Equal(t, len(names)+1, id)
				names = append(names, name)
				scores = append(scores, score)
				userRanks = append(userRanks, userRank)
			}
			require.NoError(t, rows.Err())
			require.Len(t, names, 5)
			if incrementalKey != "" {
				require.Equal(t, []string{"v1-latest", "v2", "v3-latest", "v4-latest", "v5-null"}, names)
				require.Equal(t, []sql.NullFloat64{{Float64: 11, Valid: true}, {Float64: 20, Valid: true}, {Float64: 31, Valid: true}, {Float64: -5, Valid: true}, {}}, scores)
				require.Equal(t, []int64{101, 200, 301, 401, 500}, userRanks)
			}
		})
	}
}
