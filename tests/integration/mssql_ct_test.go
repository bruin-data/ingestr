//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMSSQLCTDatabase(t *testing.T, ctx context.Context) (string, *sql.DB) {
	t.Helper()

	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	adminDB := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = adminDB.Close() })

	dbName := fmt.Sprintf("ct_%s", uniqueSuffix())
	_, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE [%s]", dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE [%s]", dbName))
	})

	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET ALLOW_SNAPSHOT_ISOLATION ON", dbName))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET CHANGE_TRACKING = ON (CHANGE_RETENTION = 2 DAYS, AUTO_CLEANUP = ON)", dbName))
	require.NoError(t, err)

	db := openMSSQLTestDB(t, mssqlURIForDatabase(t, mssqlDest.uri, "mssql", dbName, nil))
	t.Cleanup(func() { _ = db.Close() })

	return dbName, db
}

func createMSSQLCTItemsTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, `CREATE TABLE dbo.items (
		id INT NOT NULL PRIMARY KEY,
		name NVARCHAR(100) NOT NULL,
		value INT NULL
	)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `ALTER TABLE dbo.items ENABLE CHANGE_TRACKING WITH (TRACK_COLUMNS_UPDATED = OFF)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (1, N'item1', 100), (2, N'item2', 200), (3, N'item3', 300)`)
	require.NoError(t, err)
}

func createEmptyMSSQLCTItemsTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, `CREATE TABLE dbo.items (
		id INT NOT NULL PRIMARY KEY,
		name NVARCHAR(100) NOT NULL,
		value INT NULL
	)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `ALTER TABLE dbo.items ENABLE CHANGE_TRACKING WITH (TRACK_COLUMNS_UPDATED = OFF)`)
	require.NoError(t, err)
}

func TestMSSQLChangeTracking_SnapshotAndIncremental_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCTDatabase(t, ctx)
	createMSSQLCTItemsTable(t, ctx, db)

	tmpDir, err := os.MkdirTemp("", "mssql_ct_duckdb_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	duckdbPath := tmpDir + "/test.duckdb"

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+ct", dbName, nil),
		SourceTable: "dbo.items",
		DestURI:     "duckdb:///" + duckdbPath,
		DestTable:   "items_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	queryDuckCount := func(query string) int64 {
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckdbPath)
		require.NoError(t, err)
		defer func() { _ = duck.Close() }()

		var v int64
		require.NoError(t, duck.QueryRow(query).Scan(&v))
		return v
	}
	queryDuckString := func(query string) string {
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckdbPath)
		require.NoError(t, err)
		defer func() { _ = duck.Close() }()

		var v string
		require.NoError(t, duck.QueryRow(query).Scan(&v))
		return v
	}

	assert.EqualValues(t, 3, queryDuckCount(`SELECT COUNT(*) FROM items_dest`), "should have 3 rows from snapshot")
	assert.EqualValues(t, 0, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE "_cdc_deleted"`), "no snapshot row should be deleted")
	firstVersionCount := queryDuckCount(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM items_dest`)
	assert.EqualValues(t, 1, firstVersionCount, "snapshot rows share one Change Tracking version")

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 2`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.EqualValues(t, 4, queryDuckCount(`SELECT COUNT(*) FROM items_dest`))
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 1 AND value = 150 AND NOT "_cdc_deleted"`), "update should be applied")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 2 AND value = 200 AND "_cdc_deleted"`), "delete should mark existing row deleted")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 4 AND name = 'item4' AND value = 400 AND NOT "_cdc_deleted"`), "insert should be applied")
	assert.Greater(t, queryDuckCount(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM items_dest`), firstVersionCount, "Change Tracking cursor should advance after incremental sync")

	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET name = N'item3_final', value = 999 WHERE id = 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (5, N'item5', 500)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 5`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var currentCTVersion int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT CHANGE_TRACKING_CURRENT_VERSION()`).Scan(&currentCTVersion))

	assert.EqualValues(t, 4, queryDuckCount(`SELECT COUNT(*) FROM items_dest`), "insert+delete inside one CT window should not create a destination row")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 3 AND name = 'item3' AND value = 300 AND "_cdc_deleted"`), "CT delete only carries PKs, so existing row data is preserved")
	assert.EqualValues(t, 0, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 5`))
	assert.Less(t, queryDuckString(`SELECT MAX("_cdc_lsn") FROM items_dest`), fmt.Sprintf("%020d", currentCTVersion), "row-level Change Tracking cursor does not advance for changes that materialize no destination row")
}

func TestMSSQLChangeTracking_EmptyTableSnapshot_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCTDatabase(t, ctx)
	createEmptyMSSQLCTItemsTable(t, ctx, db)

	tmpDir, err := os.MkdirTemp("", "mssql_ct_empty_duckdb_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	duckdbPath := tmpDir + "/test.duckdb"

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+ct", dbName, nil),
		SourceTable: "dbo.items",
		DestURI:     "duckdb:///" + duckdbPath,
		DestTable:   "items_empty_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	queryDuckCount := func(query string) int64 {
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckdbPath)
		require.NoError(t, err)
		defer func() { _ = duck.Close() }()

		var v int64
		require.NoError(t, duck.QueryRow(query).Scan(&v))
		return v
	}

	assert.EqualValues(t, 0, queryDuckCount(`SELECT COUNT(*) FROM items_empty_dest`))

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (1, N'item1', 100)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.EqualValues(t, 0, queryDuckCount(`SELECT COUNT(*) FROM items_empty_dest`), "insert+delete on an empty table should not create a destination row")
}
