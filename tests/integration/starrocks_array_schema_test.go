//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestStarRocksGetTableSchemaReadsArrayElements(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Parallel()
	ctx := context.Background()
	dsn, uri := sharedStarRocks(t)
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

	execEventually(t, db, "CREATE DATABASE IF NOT EXISTS arrays")
	execEventually(t, db, `CREATE TABLE arrays.t (id INT, amounts ARRAY<DECIMAL(5,2)>, ids ARRAY<INT>)
		DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')`)

	tableSchema, err := dest.GetTableSchema(ctx, "arrays.t")
	require.NoError(t, err)
	require.Len(t, tableSchema.Columns, 3)
	amounts, ids := tableSchema.Columns[1], tableSchema.Columns[2]
	require.Equal(t, schema.TypeArray, amounts.DataType)
	require.Equal(t, schema.TypeDecimal, amounts.ArrayType)
	require.Equal(t, 5, amounts.Precision)
	require.Equal(t, 2, amounts.Scale)
	require.Equal(t, schema.TypeArray, ids.DataType)
	require.Equal(t, schema.TypeInt32, ids.ArrayType)

	srcPath := filepath.Join(t.TempDir(), "src.duckdb")
	src := openDuckDBForTest(t, srcPath)
	execDuckDB(t, src, "CREATE TABLE src (id INTEGER PRIMARY KEY, amounts DECIMAL(5,2)[])")
	execDuckDB(t, src, "INSERT INTO src VALUES (1, [1.25, -3.5])")
	require.NoError(t, src.Close())
	for range 2 {
		p := pipeline.New(&config.IngestConfig{
			SourceURI:           fmt.Sprintf("duckdb:///%s", srcPath),
			SourceTable:         "main.src",
			DestURI:             u.String(),
			DestTable:           "arrays.merged",
			IncrementalStrategy: "merge",
			PrimaryKeys:         []string{"id"},
		})
		require.NoError(t, p.Run(ctx))
	}
	var first, second string
	require.NoError(t, db.QueryRow("SELECT amounts[1], amounts[2] FROM arrays.merged WHERE id = 1").Scan(&first, &second))
	require.Equal(t, []string{"1.25", "-3.50"}, []string{first, second})
}
