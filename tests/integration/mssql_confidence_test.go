//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMSSQLSource_TableToSQLite_MergeUsesAutoDetectedPrimaryKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	tableName := fmt.Sprintf("dbo.source_merge_%s", uniqueSuffix())
	dropMSSQLTable(t, ctx, db, tableName)
	t.Cleanup(func() { dropMSSQLTable(t, ctx, db, tableName) })

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INT PRIMARY KEY,
		name NVARCHAR(100) NOT NULL,
		updated_at DATETIME2(6) NOT NULL
	)`, quoteTableMSSQL(tableName)))
	require.NoError(t, err)

	insertRows := []struct {
		id   int
		name string
		ts   time.Time
	}{
		{1, "alpha", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{2, "bravo", time.Date(2024, 1, 1, 0, 10, 0, 0, time.UTC)},
		{3, "charlie", time.Date(2024, 1, 1, 0, 20, 0, 0, time.UTC)},
	}
	for _, row := range insertRows {
		_, err = db.ExecContext(
			ctx,
			fmt.Sprintf("INSERT INTO %s (id, name, updated_at) VALUES (@p1, @p2, @p3)", quoteTableMSSQL(tableName)),
			row.id,
			row.name,
			row.ts,
		)
		require.NoError(t, err)
	}

	tmpFile, err := os.CreateTemp("", "mssql_source_merge_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	cfg := &config.IngestConfig{
		SourceURI:           mssqlDest.uri,
		SourceTable:         strings.TrimPrefix(tableName, "dbo."),
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "merged_rows",
		IncrementalStrategy: config.StrategyMerge,
	}
	require.NoError(t, cfg.Validate())

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err = db.ExecContext(
		ctx,
		fmt.Sprintf("UPDATE %s SET name = @p1, updated_at = @p2 WHERE id = @p3", quoteTableMSSQL(tableName)),
		"alpha-updated",
		time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
		1,
	)
	require.NoError(t, err)
	_, err = db.ExecContext(
		ctx,
		fmt.Sprintf("INSERT INTO %s (id, name, updated_at) VALUES (@p1, @p2, @p3)", quoteTableMSSQL(tableName)),
		4,
		"delta",
		time.Date(2024, 1, 1, 1, 10, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destDB, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	require.NoError(t, destDB.QueryRow("SELECT COUNT(*) FROM merged_rows").Scan(&count))
	assert.Equal(t, 4, count, "merge should update existing rows instead of duplicating them")

	var updatedName string
	require.NoError(t, destDB.QueryRow("SELECT name FROM merged_rows WHERE id = 1").Scan(&updatedName))
	assert.Equal(t, "alpha-updated", updatedName)

	var newName string
	require.NoError(t, destDB.QueryRow("SELECT name FROM merged_rows WHERE id = 4").Scan(&newName))
	assert.Equal(t, "delta", newName)
}

func TestMSSQLSource_TableToSQLite_CustomSchemaFiltersAndExcludeColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	schemaName := fmt.Sprintf("qa_%s", uniqueSuffix())
	tableName := "source_filtered"
	fullTableName := schemaName + "." + tableName

	ensureMSSQLSchema(t, ctx, db, schemaName)
	dropMSSQLTable(t, ctx, db, fullTableName)
	t.Cleanup(func() {
		dropMSSQLTable(t, ctx, db, fullTableName)
		dropMSSQLSchema(t, ctx, db, schemaName)
	})

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INT PRIMARY KEY,
		name NVARCHAR(100) NOT NULL,
		updated_at DATETIME2(6) NOT NULL,
		secret NVARCHAR(100) NULL
	)`, quoteTableMSSQL(fullTableName)))
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		_, err = db.ExecContext(
			ctx,
			fmt.Sprintf("INSERT INTO %s (id, name, updated_at, secret) VALUES (@p1, @p2, @p3, @p4)", quoteTableMSSQL(fullTableName)),
			i,
			fmt.Sprintf("row-%d", i),
			base.Add(time.Duration(i*10)*time.Minute),
			fmt.Sprintf("secret-%d", i),
		)
		require.NoError(t, err)
	}

	tmpFile, err := os.CreateTemp("", "mssql_source_filtered_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	intervalStart := base.Add(15 * time.Minute)
	intervalEnd := base.Add(45 * time.Minute)

	cfg := &config.IngestConfig{
		SourceURI:           mssqlDest.uri,
		SourceTable:         fullTableName,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "filtered_rows",
		IncrementalStrategy: config.StrategyReplace,
		IncrementalKey:      "updated_at",
		IntervalStart:       &intervalStart,
		IntervalEnd:         &intervalEnd,
		SQLExcludeColumns:   []string{"secret"},
		SQLLimit:            2,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destDB, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	require.NoError(t, destDB.QueryRow("SELECT COUNT(*) FROM filtered_rows").Scan(&count))
	assert.Equal(t, 2, count, "custom schema read should honor TOP limit")

	cols := sqliteColumns(t, destDB, "filtered_rows")
	assert.Equal(t, []string{"id", "name", "updated_at"}, cols, "excluded MSSQL columns should be removed from the destination schema")

	var minTS, maxTS string
	require.NoError(t, destDB.QueryRow("SELECT MIN(updated_at), MAX(updated_at) FROM filtered_rows").Scan(&minTS, &maxTS))
	assert.GreaterOrEqual(t, minTS, "2024-01-01 00:20:00", "interval start filter should be applied")
	assert.LessOrEqual(t, maxTS, "2024-01-01 00:40:00", "interval end filter should be applied")
}

func TestMSSQLSource_TableToSQLite_DatabaseQualifiedTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	adminDB := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = adminDB.Close() })

	dbName := fmt.Sprintf("qualified_%s", uniqueSuffix())
	quotedDBName := strings.ReplaceAll(dbName, "]", "]]")
	_, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE [%s]", quotedDBName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE", quotedDBName))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE [%s]", quotedDBName))
	})

	sourceDB := openMSSQLTestDB(t, mssqlURIForDatabase(t, mssqlDest.uri, "mssql", dbName, nil))
	t.Cleanup(func() { _ = sourceDB.Close() })

	tableName := "database_qualified_source"
	_, err = sourceDB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INT PRIMARY KEY,
		name NVARCHAR(100) NOT NULL
	)`, quoteTableMSSQL("dbo."+tableName)))
	require.NoError(t, err)

	_, err = sourceDB.ExecContext(
		ctx,
		fmt.Sprintf("INSERT INTO %s (id, name) VALUES (@p1, @p2), (@p3, @p4)", quoteTableMSSQL("dbo."+tableName)),
		1, "alpha",
		2, "bravo",
	)
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "mssql_source_qualified_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	cfg := &config.IngestConfig{
		SourceURI:           mssqlDest.uri,
		SourceTable:         dbName + ".dbo." + tableName,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "qualified_rows",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destDB, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	require.NoError(t, destDB.QueryRow("SELECT COUNT(*) FROM qualified_rows").Scan(&count))
	assert.Equal(t, 2, count)

	var name string
	require.NoError(t, destDB.QueryRow("SELECT name FROM qualified_rows WHERE id = 2").Scan(&name))
	assert.Equal(t, "bravo", name)
}

func TestAzureSQLSourceScheme_SQLServerContainerToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	tableName := fmt.Sprintf("dbo.azuresql_source_%s", uniqueSuffix())
	dropMSSQLTable(t, ctx, db, tableName)
	t.Cleanup(func() { dropMSSQLTable(t, ctx, db, tableName) })

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INT PRIMARY KEY,
		name NVARCHAR(100) NOT NULL
	)`, quoteTableMSSQL(tableName)))
	require.NoError(t, err)

	_, err = db.ExecContext(
		ctx,
		fmt.Sprintf("INSERT INTO %s (id, name) VALUES (@p1, @p2), (@p3, @p4)", quoteTableMSSQL(tableName)),
		1, "alpha",
		2, "bravo",
	)
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "azuresql_source_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	azureSourceURI := strings.Replace(mssqlDest.uri, "mssql://", "azuresql://", 1)
	cfg := &config.IngestConfig{
		SourceURI:           azureSourceURI,
		SourceTable:         tableName,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "azuresql_rows",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destDB, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	require.NoError(t, destDB.QueryRow("SELECT COUNT(*) FROM azuresql_rows").Scan(&count))
	assert.Equal(t, 2, count)

	var name string
	require.NoError(t, destDB.QueryRow("SELECT name FROM azuresql_rows WHERE id = 2").Scan(&name))
	assert.Equal(t, "bravo", name)
}

func TestMSSQLDestination_Replace_CustomSchemaSwapCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	schemaName := fmt.Sprintf("swap_%s", uniqueSuffix())
	tableName := "replace_target"
	fullTableName := schemaName + "." + tableName

	t.Cleanup(func() {
		dropMSSQLTable(t, ctx, db, fullTableName)
		dropMSSQLSchema(t, ctx, db, schemaName)
	})

	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "mssql_replace_custom_schema",
		DestURI:             mssqlDest.uri,
		DestTable:           fullTableName,
		IncrementalStrategy: config.StrategyReplace,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var schemaCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.schemas WHERE name = @p1", schemaName).Scan(&schemaCount))
	assert.Equal(t, 1, schemaCount, "custom schema should be created automatically")

	oldTables := countOldTables(t, mssqlBackend(), mssqlDest.uri, fullTableName)
	assert.Equal(t, 0, oldTables, "replace swap should clean up _old_ tables in custom schemas")

	var rowCount int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableMSSQL(fullTableName))).Scan(&rowCount))
	assert.Equal(t, replaceFixtureRows, rowCount)
}

func openMSSQLTestDB(t *testing.T, uri string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlserver", mssqlConnString(uri))
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	return db
}

func ensureMSSQLSchema(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '%s') EXEC('CREATE SCHEMA [%s]')",
		strings.ReplaceAll(schemaName, "'", "''"),
		strings.ReplaceAll(schemaName, "]", "]]"),
	))
	require.NoError(t, err)
}

func dropMSSQLSchema(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"IF EXISTS (SELECT * FROM sys.schemas WHERE name = '%s') EXEC('DROP SCHEMA [%s]')",
		strings.ReplaceAll(schemaName, "'", "''"),
		strings.ReplaceAll(schemaName, "]", "]]"),
	))
	require.NoError(t, err)
}

func dropMSSQLTable(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
		strings.ReplaceAll(table, "'", "''"),
		quoteTableMSSQL(table),
	))
	require.NoError(t, err)
}

func sqliteColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt interface{}
		var pk int
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		columns = append(columns, name)
	}
	require.NoError(t, rows.Err())

	return columns
}
