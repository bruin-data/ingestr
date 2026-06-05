//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMSSQLCDC_DuckDB_UpdateDeleteAndSchemaChange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	sourceURI := createMSSQLCDCDatabase(t, ctx)
	db := openMSSQLTestDB(t, sourceURI)
	t.Cleanup(func() { _ = db.Close() })

	_, err := db.ExecContext(ctx, `
		CREATE TABLE dbo.accounts (
			id INT NOT NULL PRIMARY KEY,
			name NVARCHAR(100) NOT NULL,
			balance INT NOT NULL,
			note NVARCHAR(100) NULL
		)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO dbo.accounts (id, name, balance, note) VALUES
			(1, N'alpha', 100, N'a'),
			(2, N'bravo', 200, N'b'),
			(3, N'charlie', 300, N'c')
	`)
	require.NoError(t, err)
	enableMSSQLCDCTable(t, ctx, db, "dbo", "accounts")

	duckPath := filepath.Join(t.TempDir(), "mssql_cdc.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           mssqlCDCURI(sourceURI),
		SourceTable:         "dbo.accounts",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckPath),
		DestTable:           "accounts_dest",
		IncrementalStrategy: config.StrategyMerge,
		SchemaContract:      "evolve",
		PageSize:            2,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	requireDuckDBCount(t, ctx, duckPath, "accounts_dest", 3)
	firstMax := duckDBString(t, ctx, duckPath, `SELECT MAX("_cdc_lsn") FROM accounts_dest`)
	require.NotEmpty(t, firstMax)

	beforeChange := mssqlCDCMaxLSN(t, ctx, db)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE dbo.accounts SET name = N'alpha-updated', balance = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `DELETE FROM dbo.accounts WHERE id = 1`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE dbo.accounts SET balance = 250 WHERE id = 2`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `DELETE FROM dbo.accounts WHERE id = 3`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO dbo.accounts (id, name, balance, note) VALUES (4, N'delta', 400, N'd')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	waitForMSSQLCDCAdvance(t, ctx, db, beforeChange)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	requireDuckDBCount(t, ctx, duckPath, "accounts_dest", 4)
	requireDuckDBCountWhere(t, ctx, duckPath, "accounts_dest", `"_cdc_deleted" = false`, 2)

	var name string
	var balance int
	var deleted bool
	queryDuckDBRow(t, ctx, duckPath, `SELECT name, balance, "_cdc_deleted" FROM accounts_dest WHERE id = 1`, &name, &balance, &deleted)
	assert.Equal(t, "alpha-updated", name)
	assert.Equal(t, 150, balance)
	assert.True(t, deleted)

	queryDuckDBRow(t, ctx, duckPath, `SELECT balance, "_cdc_deleted" FROM accounts_dest WHERE id = 2`, &balance, &deleted)
	assert.Equal(t, 250, balance)
	assert.False(t, deleted)

	disableMSSQLCDCTable(t, ctx, db, "dbo", "accounts", "dbo_accounts")
	_, err = db.ExecContext(ctx, `ALTER TABLE dbo.accounts ADD tier NVARCHAR(20) NULL`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.accounts SET tier = N'gold' WHERE id = 2`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE dbo.accounts DROP COLUMN note`)
	require.NoError(t, err)
	enableMSSQLCDCTable(t, ctx, db, "dbo", "accounts")

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	queryDuckDBRow(t, ctx, duckPath, `SELECT tier FROM accounts_dest WHERE id = 2`, &name)
	assert.Equal(t, "gold", name)
	assert.True(t, duckDBColumnExists(t, ctx, duckPath, "accounts_dest", "tier"))
	assert.True(t, duckDBColumnExists(t, ctx, duckPath, "accounts_dest", "note"), "dropped source columns should be soft-retained in the destination")
}

func TestMSSQLCDC_Postgres_UpdateDeleteResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	sourceURI := createMSSQLCDCDatabase(t, ctx)
	db := openMSSQLTestDB(t, sourceURI)
	t.Cleanup(func() { _ = db.Close() })

	_, err := db.ExecContext(ctx, `
		CREATE TABLE dbo.orders (
			id INT NOT NULL PRIMARY KEY,
			status NVARCHAR(50) NOT NULL,
			amount INT NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO dbo.orders (id, status, amount) VALUES
			(10, N'new', 100),
			(20, N'new', 200)
	`)
	require.NoError(t, err)
	enableMSSQLCDCTable(t, ctx, db, "dbo", "orders")

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "mssql_cdc_pg")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI:           mssqlCDCURI(sourceURI),
		SourceTable:         "dbo.orders",
		DestURI:             destURI,
		DestTable:           destSchema + ".orders_dest",
		IncrementalStrategy: config.StrategyMerge,
		SchemaContract:      "evolve",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pgdb, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgdb.Close() })

	var count int
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."orders_dest"`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count)

	beforeChange := mssqlCDCMaxLSN(t, ctx, db)
	_, err = db.ExecContext(ctx, `
		UPDATE dbo.orders SET status = N'paid', amount = 250 WHERE id = 20;
		DELETE FROM dbo.orders WHERE id = 10;
		INSERT INTO dbo.orders (id, status, amount) VALUES (30, N'new', 300);
	`)
	require.NoError(t, err)
	waitForMSSQLCDCAdvance(t, ctx, db, beforeChange)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."orders_dest"`, destSchema)).Scan(&count))
	assert.Equal(t, 3, count)
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."orders_dest" WHERE "_cdc_deleted" = false`, destSchema)).Scan(&count))
	assert.Equal(t, 2, count)

	var status string
	var amount int
	var deleted bool
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT status, amount, "_cdc_deleted" FROM "%s"."orders_dest" WHERE id = 20`, destSchema)).Scan(&status, &amount, &deleted))
	assert.Equal(t, "paid", status)
	assert.Equal(t, 250, amount)
	assert.False(t, deleted)

	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT "_cdc_deleted" FROM "%s"."orders_dest" WHERE id = 10`, destSchema)).Scan(&deleted))
	assert.True(t, deleted)
}

func TestMSSQLCDC_DuckDB_MultiTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	sourceURI := createMSSQLCDCDatabase(t, ctx)
	db := openMSSQLTestDB(t, sourceURI)
	t.Cleanup(func() { _ = db.Close() })

	_, err := db.ExecContext(ctx, `
		CREATE TABLE dbo.customers (
			id INT NOT NULL PRIMARY KEY,
			name NVARCHAR(100) NOT NULL
		);
		CREATE TABLE dbo.invoices (
			id INT NOT NULL PRIMARY KEY,
			customer_id INT NOT NULL,
			amount INT NOT NULL
		);
		INSERT INTO dbo.customers (id, name) VALUES (1, N'Alice'), (2, N'Bob');
		INSERT INTO dbo.invoices (id, customer_id, amount) VALUES (100, 1, 50), (200, 2, 75);
	`)
	require.NoError(t, err)
	enableMSSQLCDCTable(t, ctx, db, "dbo", "customers")
	enableMSSQLCDCTable(t, ctx, db, "dbo", "invoices")

	duckPath := filepath.Join(t.TempDir(), "mssql_cdc_multitable.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           mssqlCDCURI(sourceURI),
		DestURI:             fmt.Sprintf("duckdb:///%s", duckPath),
		IncrementalStrategy: config.StrategyMerge,
		SchemaContract:      "evolve",
		PageSize:            1,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	requireDuckDBCount(t, ctx, duckPath, `"dbo"."customers"`, 2)
	requireDuckDBCount(t, ctx, duckPath, `"dbo"."invoices"`, 2)

	beforeChange := mssqlCDCMaxLSN(t, ctx, db)
	_, err = db.ExecContext(ctx, `
		UPDATE dbo.customers SET name = N'Alice Cooper' WHERE id = 1;
		DELETE FROM dbo.invoices WHERE id = 100;
		INSERT INTO dbo.invoices (id, customer_id, amount) VALUES (300, 1, 125);
	`)
	require.NoError(t, err)
	waitForMSSQLCDCAdvance(t, ctx, db, beforeChange)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var name string
	queryDuckDBRow(t, ctx, duckPath, `SELECT name FROM "dbo"."customers" WHERE id = 1`, &name)
	assert.Equal(t, "Alice Cooper", name)
	requireDuckDBCountWhere(t, ctx, duckPath, `"dbo"."invoices"`, `"_cdc_deleted" = false`, 2)
	requireDuckDBCountWhere(t, ctx, duckPath, `"dbo"."invoices"`, `id = 100 AND "_cdc_deleted" = true`, 1)
}

func TestMSSQLCDC_Postgres_MultiTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	sourceURI := createMSSQLCDCDatabase(t, ctx)
	db := openMSSQLTestDB(t, sourceURI)
	t.Cleanup(func() { _ = db.Close() })

	sourceSchema := "mtpg_" + uniqueSuffix()
	ensureMSSQLSchema(t, ctx, db, sourceSchema)

	customersTable := sourceSchema + ".customers"
	invoicesTable := sourceSchema + ".invoices"
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id INT NOT NULL PRIMARY KEY,
			name NVARCHAR(100) NOT NULL
		);
		CREATE TABLE %s (
			id INT NOT NULL PRIMARY KEY,
			customer_id INT NOT NULL,
			amount INT NOT NULL
		);
		INSERT INTO %s (id, name) VALUES (1, N'Alice'), (2, N'Bob');
		INSERT INTO %s (id, customer_id, amount) VALUES (100, 1, 50), (200, 2, 75);
	`, quoteTableMSSQL(customersTable), quoteTableMSSQL(invoicesTable), quoteTableMSSQL(customersTable), quoteTableMSSQL(invoicesTable)))
	require.NoError(t, err)
	enableMSSQLCDCTable(t, ctx, db, sourceSchema, "customers")
	enableMSSQLCDCTable(t, ctx, db, sourceSchema, "invoices")

	destURI := sharedPostgresURI(t, "dest")
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, sourceSchema) })

	cfg := &config.IngestConfig{
		SourceURI:           mssqlCDCURI(sourceURI),
		DestURI:             destURI,
		IncrementalStrategy: config.StrategyMerge,
		SchemaContract:      "evolve",
		PageSize:            1,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pgdb, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgdb.Close() })

	var count int
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."customers"`, sourceSchema)).Scan(&count))
	assert.Equal(t, 2, count)
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."invoices"`, sourceSchema)).Scan(&count))
	assert.Equal(t, 2, count)

	beforeChange := mssqlCDCMaxLSN(t, ctx, db)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET name = N'Alice Cooper' WHERE id = 1;
		DELETE FROM %s WHERE id = 100;
		INSERT INTO %s (id, customer_id, amount) VALUES (300, 1, 125);
	`, quoteTableMSSQL(customersTable), quoteTableMSSQL(invoicesTable), quoteTableMSSQL(invoicesTable)))
	require.NoError(t, err)
	waitForMSSQLCDCAdvance(t, ctx, db, beforeChange)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var name string
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT name FROM "%s"."customers" WHERE id = 1`, sourceSchema)).Scan(&name))
	assert.Equal(t, "Alice Cooper", name)
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."invoices" WHERE "_cdc_deleted" = false`, sourceSchema)).Scan(&count))
	assert.Equal(t, 2, count)
	require.NoError(t, pgdb.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."invoices" WHERE id = 100 AND "_cdc_deleted" = true`, sourceSchema)).Scan(&count))
	assert.Equal(t, 1, count)
}

func createMSSQLCDCDatabase(t *testing.T, ctx context.Context) string {
	t.Helper()

	dbName := "cdc_" + uniqueSuffix()
	adminDB := openMSSQLTestDB(t, mssqlDest.uri)
	defer func() { _ = adminDB.Close() }()

	_, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE [%s]", dbName))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET ALLOW_SNAPSHOT_ISOLATION ON", dbName))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET READ_COMMITTED_SNAPSHOT ON WITH ROLLBACK IMMEDIATE", dbName))
	require.NoError(t, err)

	uri := mssqlURIWithDatabase(t, mssqlDest.uri, dbName)
	db := openMSSQLTestDB(t, uri)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, "EXEC sys.sp_cdc_enable_db")
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupDB := openMSSQLTestDB(t, mssqlDest.uri)
		defer func() { _ = cleanupDB.Close() }()
		_, _ = cleanupDB.ExecContext(context.Background(), fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", dbName))
		_, _ = cleanupDB.ExecContext(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS [%s]", dbName))
	})

	return uri
}

func mssqlURIWithDatabase(t *testing.T, rawURI string, database string) string {
	t.Helper()
	parsed, err := url.Parse(rawURI)
	require.NoError(t, err)
	parsed.Path = "/" + database
	return parsed.String()
}

func mssqlCDCURI(rawURI string) string {
	return strings.Replace(rawURI, "mssql://", "mssql+cdc://", 1)
}

func enableMSSQLCDCTable(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName string) {
	t.Helper()
	_, err := db.ExecContext(ctx, "EXEC sys.sp_cdc_enable_table @source_schema = @p1, @source_name = @p2, @role_name = NULL, @supports_net_changes = 0", schemaName, tableName)
	require.NoError(t, err)
	waitForMSSQLCDCTable(t, ctx, db, schemaName+"_"+tableName)
}

func disableMSSQLCDCTable(t *testing.T, ctx context.Context, db *sql.DB, schemaName, tableName, captureInstance string) {
	t.Helper()
	_, err := db.ExecContext(ctx, "EXEC sys.sp_cdc_disable_table @source_schema = @p1, @source_name = @p2, @capture_instance = @p3", schemaName, tableName, captureInstance)
	require.NoError(t, err)
}

func waitForMSSQLCDCTable(t *testing.T, ctx context.Context, db *sql.DB, captureInstance string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var exists bool
		err := db.QueryRowContext(ctx, "SELECT CAST(CASE WHEN EXISTS (SELECT 1 FROM cdc.change_tables WHERE capture_instance = @p1) THEN 1 ELSE 0 END AS bit)", captureInstance).Scan(&exists)
		return err == nil && exists
	}, 30*time.Second, 500*time.Millisecond)
}

func mssqlCDCMaxLSN(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var lsn sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), sys.fn_cdc_get_max_lsn(), 2)").Scan(&lsn))
	if !lsn.Valid {
		return ""
	}
	return strings.ToUpper(lsn.String)
}

func waitForMSSQLCDCAdvance(t *testing.T, ctx context.Context, db *sql.DB, previous string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, _ = db.ExecContext(ctx, "EXEC sys.sp_cdc_scan")
		current := mssqlCDCMaxLSN(t, ctx, db)
		return current != "" && current != previous
	}, 60*time.Second, time.Second)
}

func queryDuckDBRow(t *testing.T, ctx context.Context, path, query string, dest ...any) {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	scanDest := make([]any, len(dest))
	for i := range dest {
		switch dest[i].(type) {
		case *string:
			var raw []byte
			scanDest[i] = &raw
			defer func(i int, rawPtr *[]byte) {
				*(dest[i].(*string)) = string(append([]byte(nil), (*rawPtr)...))
			}(i, &raw)
		default:
			scanDest[i] = dest[i]
		}
	}
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(scanDest...))
}

func duckDBString(t *testing.T, ctx context.Context, path, query string) string {
	t.Helper()
	var got string
	queryDuckDBRow(t, ctx, path, query, &got)
	return got
}

func requireDuckDBCount(t *testing.T, ctx context.Context, path, table string, expected int) {
	t.Helper()
	requireDuckDBCountWhere(t, ctx, path, table, "1 = 1", expected)
}

func requireDuckDBCountWhere(t *testing.T, ctx context.Context, path, table, where string, expected int) {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", table, where)).Scan(&count))
	assert.Equal(t, expected, count)
}

func duckDBColumnExists(t *testing.T, ctx context.Context, path, table, column string) bool {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, fmt.Sprintf("DESCRIBE %s", table))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		var rest []any
		scanDest := []any{&name}
		for range 5 {
			var v any
			rest = append(rest, &v)
			scanDest = append(scanDest, &v)
		}
		if err := rows.Scan(scanDest...); err == nil && strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}
