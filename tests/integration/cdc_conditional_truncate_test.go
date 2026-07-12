//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/destination/oracle"
	"github.com/stretchr/testify/require"
)

func TestMSSQLConditionalCDCTruncateFencesRecreatedTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("shared SQL Server destination container not available")
	}

	ctx := context.Background()
	table := "cdc_fence_" + uniqueSuffix()
	qualifiedTable := "[dbo].[" + table + "]"
	db := openMSSQLTestDB(t, mssqlDest.uri)
	defer func() { _ = db.Close() }()
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+qualifiedTable) }()
	_, err := db.ExecContext(ctx, "CREATE TABLE "+qualifiedTable+" ([id] BIGINT NOT NULL); INSERT INTO "+qualifiedTable+" VALUES (1), (2)")
	require.NoError(t, err)

	dest := mssql.NewMSSQLDestination()
	require.NoError(t, dest.Connect(ctx, mssqlDest.uri))
	defer func() { _ = dest.Close(ctx) }()
	expected, exists, err := dest.CDCTargetIncarnation(ctx, "dbo."+table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, "dbo."+table, expected))
	requireTableRowCount(t, db, qualifiedTable, 0)
	stable, exists, err := dest.CDCTargetIncarnation(ctx, "dbo."+table)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, expected, stable)

	_, err = db.ExecContext(ctx, "DROP TABLE "+qualifiedTable+"; CREATE TABLE "+qualifiedTable+" ([id] BIGINT NOT NULL); INSERT INTO "+qualifiedTable+" VALUES (99)")
	require.NoError(t, err)
	err = dest.TruncateCDCTableIfIncarnation(ctx, "dbo."+table, expected)
	require.ErrorContains(t, err, "physical incarnation changed")
	requireTableRowCount(t, db, qualifiedTable, 1)
}

func TestMySQLConditionalCDCTruncateFencesRecreatedTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("shared MySQL destination container not available")
	}

	ctx := context.Background()
	table := "cdc_fence_" + uniqueSuffix()
	quotedTable := "`" + table + "`"
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+quotedTable) }()
	_, err = db.ExecContext(ctx, "CREATE TABLE "+quotedTable+" (`id` BIGINT NOT NULL) ENGINE=InnoDB")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO "+quotedTable+" VALUES (1), (2)")
	require.NoError(t, err)

	dest := mysql.NewMySQLDestination()
	require.NoError(t, dest.Connect(ctx, mysqlDest.uri))
	defer func() { _ = dest.Close(ctx) }()
	expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, table, expected))
	requireTableRowCount(t, db, quotedTable, 0)
	stable, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, expected, stable)

	_, err = db.ExecContext(ctx, "DROP TABLE "+quotedTable)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE "+quotedTable+" (`id` BIGINT NOT NULL) ENGINE=InnoDB")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO "+quotedTable+" VALUES (99)")
	require.NoError(t, err)
	err = dest.TruncateCDCTableIfIncarnation(ctx, table, expected)
	require.ErrorContains(t, err, "physical incarnation changed")
	requireTableRowCount(t, db, quotedTable, 1)
}

func TestOracleConditionalCDCTruncateFencesRecreatedTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if oracleDest.uri == "" {
		t.Skip("shared Oracle destination container not available")
	}

	ctx := context.Background()
	table := "cdc_fence_" + uniqueSuffix()
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	require.NoError(t, db.PingContext(ctx))
	defer func() { _ = db.Close() }()
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE "+table+" PURGE") }()
	_, err = db.ExecContext(ctx, "CREATE TABLE "+table+" (id NUMBER(19) NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (1)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (2)")
	require.NoError(t, err)

	dest := oracle.NewOracleDestination()
	require.NoError(t, dest.Connect(ctx, oracleDest.uri))
	defer func() { _ = dest.Close(ctx) }()
	expected, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, table, expected))
	requireTableRowCount(t, db, table, 0)
	stable, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, expected, stable)

	_, err = db.ExecContext(ctx, "DROP TABLE "+table+" PURGE")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE "+table+" (id NUMBER(19) NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (99)")
	require.NoError(t, err)
	err = dest.TruncateCDCTableIfIncarnation(ctx, table, expected)
	require.ErrorContains(t, err, "physical incarnation changed")
	requireTableRowCount(t, db, table, 1)
}

func requireTableRowCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	require.NoError(t, db.QueryRowContext(t.Context(), fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&got))
	require.Equal(t, want, got)
}
