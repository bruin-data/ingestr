//go:build integration

package integration

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestStarRocksGetTableSchemaReadsArrayElements(t *testing.T) {
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
}
