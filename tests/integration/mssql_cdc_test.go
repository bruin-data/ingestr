//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mssqlURIForDatabase(t *testing.T, baseURI, scheme, dbName string, params map[string]string) string {
	t.Helper()

	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.Scheme = scheme
	u.Path = "/" + dbName
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// setupMSSQLCDCDatabase creates a dedicated database on the shared SQL Server
// container, enables CDC on it, and returns a connection to it along with the
// database name. The database is dropped on cleanup.
func setupMSSQLCDCDatabase(t *testing.T, ctx context.Context) (string, *sql.DB) {
	t.Helper()

	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	adminDB := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = adminDB.Close() })

	dbName := fmt.Sprintf("cdc_%s", uniqueSuffix())
	_, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE [%s]", dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE [%s]", dbName))
	})

	db := openMSSQLTestDB(t, mssqlURIForDatabase(t, mssqlDest.uri, "mssql", dbName, nil))
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, "EXEC sys.sp_cdc_enable_db")
	require.NoError(t, err)

	return dbName, db
}

func enableMSSQLCDCOnTable(t *testing.T, ctx context.Context, db *sql.DB, tableName string) {
	t.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"EXEC sys.sp_cdc_enable_table @source_schema = N'dbo', @source_name = N'%s', @role_name = NULL, @supports_net_changes = 0",
		tableName,
	))
	require.NoError(t, err)
}

// waitForMSSQLCDCRows polls the change table until the CDC capture job has
// harvested at least `want` change rows. The capture job scans every few
// seconds, so changes are not visible to CDC functions immediately.
func waitForMSSQLCDCRows(t *testing.T, ctx context.Context, db *sql.DB, captureInstance string, want int) {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	query := fmt.Sprintf("SELECT COUNT(*) FROM cdc.[%s_CT]", captureInstance)
	var count int
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(ctx, query).Scan(&count); err == nil && count >= want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d CDC change rows in %s (have %d); is SQL Server Agent running?", want, captureInstance, count)
}

func createMSSQLCDCItemsTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, `CREATE TABLE dbo.items (
		id INT NOT NULL PRIMARY KEY,
		name NVARCHAR(100) NOT NULL,
		value INT NULL
	)`)
	require.NoError(t, err)

	enableMSSQLCDCOnTable(t, ctx, db, "items")

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (1, N'item1', 100), (2, N'item2', 200), (3, N'item3', 300)`)
	require.NoError(t, err)
}

type cdcRow struct {
	name    string
	value   int64
	deleted bool
}

func TestMSSQLCDC_SnapshotAndIncremental_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)
	createMSSQLCDCItemsTable(t, ctx, db)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 3)

	tmpDir, err := os.MkdirTemp("", "mssql_cdc_duckdb_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	duckdbPath := tmpDir + "/test.duckdb"

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"mode": "batch"}),
		SourceTable: "dbo.items",
		DestURI:     "duckdb:///" + duckdbPath,
		DestTable:   "items_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// String values must not be scanned through the adbc_generic driver: it
	// hands database/sql strings that alias Arrow buffers owned by the C
	// driver, which dangle once the reader advances. Compare strings in SQL
	// and only scan scalar results instead.
	queryDuckCount := func(query string) int64 {
		duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckdbPath)
		require.NoError(t, err)
		defer func() { _ = duck.Close() }()

		var v int64
		require.NoError(t, duck.QueryRow(query).Scan(&v))
		return v
	}

	assert.EqualValues(t, 3, queryDuckCount(`SELECT COUNT(*) FROM items_dest`), "should have 3 rows from snapshot")
	assert.EqualValues(t, 0, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE "_cdc_deleted"`), "no snapshot row should be deleted")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 1 AND name = 'item1' AND value = 100`))
	firstLSNRank := queryDuckCount(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM items_dest`)
	assert.EqualValues(t, 1, firstLSNRank, "snapshot rows share one LSN stamp")

	// Plain insert/update/delete picked up incrementally.
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 2`)
	require.NoError(t, err)
	// 3 inserts + 1 insert + update (old+new image) + 1 delete = 7
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 7)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.EqualValues(t, 4, queryDuckCount(`SELECT COUNT(*) FROM items_dest`))
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 1 AND value = 150 AND NOT "_cdc_deleted"`), "update should be applied")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 2 AND value = 200 AND "_cdc_deleted"`), "delete should be soft-applied, keeping last values")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 4 AND name = 'item4' AND value = 400 AND NOT "_cdc_deleted"`), "insert should be applied")
	assert.Greater(t, queryDuckCount(`SELECT COUNT(DISTINCT "_cdc_lsn") FROM items_dest`), firstLSNRank, "LSN should advance after incremental sync")

	// Multiple changes to the same PK within one sync window: the latest
	// change must win and a trailing delete must be honored.
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET name = N'item3_final', value = 999 WHERE id = 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (5, N'item5', 500)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 555 WHERE id = 5`)
	require.NoError(t, err)
	// + update (2) + delete (1) + insert (1) + update (2) = 13
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 13)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.EqualValues(t, 5, queryDuckCount(`SELECT COUNT(*) FROM items_dest`))
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 3 AND name = 'item3_final' AND value = 999 AND "_cdc_deleted"`), "update+delete in one window must end deleted with the last update's values")
	assert.EqualValues(t, 1, queryDuckCount(`SELECT COUNT(*) FROM items_dest WHERE id = 5 AND name = 'item5' AND value = 555 AND NOT "_cdc_deleted"`), "insert+update in one window must keep the updated value")
}

func TestMSSQLCDC_SnapshotAndIncremental_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)
	createMSSQLCDCItemsTable(t, ctx, db)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 3)

	destSchema := uniqueSchemaName(t, "mssql_cdc")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })
	destTable := destSchema + ".items_dest"

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"mode": "batch"}),
		SourceTable: "dbo.items",
		DestURI:     pgDest.uri,
		DestTable:   destTable,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	queryRows := func() map[int64]cdcRow {
		rows, err := pg.QueryContext(ctx, fmt.Sprintf(`SELECT id, name, value, "_cdc_deleted" FROM %q.items_dest ORDER BY id`, destSchema))
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()

		out := map[int64]cdcRow{}
		for rows.Next() {
			var id, value int64
			var name string
			var deleted bool
			require.NoError(t, rows.Scan(&id, &name, &value, &deleted))
			out[id] = cdcRow{name: name, value: value, deleted: deleted}
		}
		require.NoError(t, rows.Err())
		return out
	}

	rows := queryRows()
	require.Len(t, rows, 3, "should have 3 rows from snapshot")
	var firstMaxLSN string
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %q.items_dest`, destSchema)).Scan(&firstMaxLSN))

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 2`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET name = N'item3_final', value = 999 WHERE id = 3`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 3`)
	require.NoError(t, err)
	// 3 + insert (1) + update (2) + delete (1) + update (2) + delete (1) = 10
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 10)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows = queryRows()
	require.Len(t, rows, 4)
	assert.Equal(t, int64(150), rows[1].value, "update should be applied")
	assert.True(t, rows[2].deleted, "delete should be soft-applied")
	assert.Equal(t, cdcRow{name: "item3_final", value: 999, deleted: true}, rows[3], "update+delete in one window must keep latest values and end deleted")
	assert.False(t, rows[4].deleted, "insert should be applied")

	var secondMaxLSN string
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT MAX("_cdc_lsn") FROM %q.items_dest`, destSchema)).Scan(&secondMaxLSN))
	assert.Greater(t, secondMaxLSN, firstMaxLSN, "LSN should advance after incremental sync")
}

// TestMSSQLCDC_FreshCaptureInstanceResume reproduces a harvest-lag bug: when a
// table is snapshotted right after sp_cdc_enable_table, sys.fn_cdc_get_max_lsn
// can still be below the new capture instance's start_lsn. The snapshot stamp
// must be clamped up to start_lsn, otherwise the next run considers the resume
// LSN invalid, re-snapshots, and silently drops upstream deletes.
func TestMSSQLCDC_FreshCaptureInstanceResume_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)

	_, err := db.ExecContext(ctx, `CREATE TABLE dbo.fresh (id INT NOT NULL PRIMARY KEY, v INT NOT NULL)`)
	require.NoError(t, err)
	enableMSSQLCDCOnTable(t, ctx, db, "fresh")
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.fresh (id, v) VALUES (1, 10), (2, 20)`)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "mssql_cdc_fresh_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	duckdbPath := tmpDir + "/test.duckdb"

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"mode": "batch"}),
		SourceTable: "dbo.fresh",
		DestURI:     "duckdb:///" + duckdbPath,
		DestTable:   "fresh_dest",
	}
	// Snapshot immediately, before the capture job has harvested anything.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err = db.ExecContext(ctx, `UPDATE dbo.fresh SET v = 11 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.fresh WHERE id = 2`)
	require.NoError(t, err)
	// 2 inserts + update (2) + delete (1) = 5
	waitForMSSQLCDCRows(t, ctx, db, "dbo_fresh", 5)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckdbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = duck.Close() })

	var v int64
	var deleted bool
	require.NoError(t, duck.QueryRow(`SELECT v, "_cdc_deleted" FROM fresh_dest WHERE id = 1`).Scan(&v, &deleted))
	assert.Equal(t, int64(11), v, "update after fresh snapshot must be applied")
	assert.False(t, deleted)

	require.NoError(t, duck.QueryRow(`SELECT v, "_cdc_deleted" FROM fresh_dest WHERE id = 2`).Scan(&v, &deleted))
	assert.True(t, deleted, "delete after fresh snapshot must be applied")
}

func TestMSSQLCDC_MultiTable_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)
	createMSSQLCDCItemsTable(t, ctx, db)

	_, err := db.ExecContext(ctx, `CREATE TABLE dbo.orders (
		order_id BIGINT NOT NULL PRIMARY KEY,
		item_id INT NOT NULL,
		qty INT NOT NULL
	)`)
	require.NoError(t, err)
	enableMSSQLCDCOnTable(t, ctx, db, "orders")
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.orders (order_id, item_id, qty) VALUES (1001, 1, 2), (1002, 2, 1)`)
	require.NoError(t, err)

	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 3)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_orders", 2)

	destSchema := uniqueSchemaName(t, "mssql_cdc_mt")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI: mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{
			"mode":        "batch",
			"dest_schema": destSchema,
		}),
		DestURI: pgDest.uri,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	var count int
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q."dbo.items"`, destSchema)).Scan(&count))
	assert.Equal(t, 3, count, "items snapshot")
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q."dbo.orders"`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count, "orders snapshot")

	_, err = db.ExecContext(ctx, `UPDATE dbo.orders SET qty = 99 WHERE order_id = 1001`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.orders WHERE order_id = 1002`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	// orders: 2 inserts + update (2) + delete (1) = 5; items: 3 + insert (1) = 4
	waitForMSSQLCDCRows(t, ctx, db, "dbo_orders", 5)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 4)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var qty int
	var deleted bool
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT qty, "_cdc_deleted" FROM %q."dbo.orders" WHERE order_id = 1001`, destSchema)).Scan(&qty, &deleted))
	assert.Equal(t, 99, qty, "orders update should be applied")
	assert.False(t, deleted)

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT "_cdc_deleted" FROM %q."dbo.orders" WHERE order_id = 1002`, destSchema)).Scan(&deleted))
	assert.True(t, deleted, "orders delete should be soft-applied")

	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q."dbo.items"`, destSchema)).Scan(&count))
	assert.Equal(t, 4, count, "items insert should be applied")
}
