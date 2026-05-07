package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/pipeline"
	_ "github.com/bruin-data/gong/pkg/source/adbc" // Register ADBC driver
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDBUser     = "test"
	testDBPassword = "test"
	testDBName     = "testdb"
)

// TestPostgresToPostgres tests ingestion from PostgreSQL to PostgreSQL
func TestPostgresToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "src")
	destSchema := uniqueSchemaName(t, "dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	// Setup source data
	setupPostgresSourceData(t, ctx, sourceURI, sourceSchema, "users")

	// Run ingestion
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	// Validate results
	validatePostgresResults(t, ctx, destURI, destSchema, "users", 100)
}

// TestPostgresToPostgresWithMerge tests merge strategy
func TestPostgresToPostgresWithMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "src")
	destSchema := uniqueSchemaName(t, "dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	// Setup source data with primary key
	setupPostgresSourceDataWithPK(t, ctx, sourceURI, sourceSchema, "users")

	// First run - insert initial data
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".users",
		DestURI:             destURI,
		DestTable:           destSchema + ".users",
		IncrementalStrategy: "merge",
		PrimaryKeys:         []string{"id"},
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err, "First pipeline run should succeed")

	// Validate initial data
	validatePostgresResults(t, ctx, destURI, destSchema, "users", 50)

	// Update source data
	updatePostgresSourceData(t, ctx, sourceURI, sourceSchema, "users")

	// Second run - merge updated data
	p2 := pipeline.New(cfg)
	err = p2.Run(ctx)
	require.NoError(t, err, "Second pipeline run should succeed")

	// Validate merged data (same count, but updated values)
	validatePostgresMergedResults(t, ctx, destURI, destSchema, "users")
}

// TestPostgresToSQLite tests ingestion from PostgreSQL to SQLite
func TestPostgresToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	// Create temp SQLite file
	tmpFile, err := os.CreateTemp("", "test_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	// Setup source data
	setupPostgresSourceData(t, ctx, sourceURI, sourceSchema, "users")

	// Run ingestion
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".users",
		DestURI:             destURI,
		DestTable:           "users",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	// Validate SQLite results
	validateSQLiteResults(t, tmpFile.Name(), 100)
}

// TestDuckDBToSQLite tests ingestion from DuckDB to SQLite
func TestDuckDBToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create temp file paths using t.TempDir for cross-platform compatibility
	tmpDir := t.TempDir()
	duckDBPath := filepath.Join(tmpDir, fmt.Sprintf("source_%d.duckdb", time.Now().UnixNano()))
	sqlitePath := filepath.Join(tmpDir, fmt.Sprintf("dest_%d.db", time.Now().UnixNano()))

	sourceURI := fmt.Sprintf("duckdb:///%s", duckDBPath)
	destURI := fmt.Sprintf("sqlite:///%s", sqlitePath)

	// Setup DuckDB source data
	setupDuckDBSourceData(t, ctx, duckDBPath)

	// Run ingestion
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "main.test_data",
		DestURI:             destURI,
		DestTable:           "test_data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	// Validate SQLite results
	validateSQLiteTestDataResults(t, sqlitePath, 200)
}

// TestDuckDBToPostgres tests ingestion from DuckDB to PostgreSQL
func TestDuckDBToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create temp DuckDB file path using t.TempDir for cross-platform compatibility
	tmpDir := t.TempDir()
	duckDBPath := filepath.Join(tmpDir, fmt.Sprintf("source_pg_%d.duckdb", time.Now().UnixNano()))

	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "dst")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	sourceURI := fmt.Sprintf("duckdb:///%s", duckDBPath)

	// Setup DuckDB source data
	setupDuckDBSourceData(t, ctx, duckDBPath)

	// Run ingestion
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "main.test_data",
		DestURI:             destURI,
		DestTable:           destSchema + ".test_data",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err, "Pipeline should run without errors")

	// Validate PostgreSQL results
	validatePostgresTestDataResults(t, ctx, destURI, destSchema, "test_data", 200)
}

// TestDuckDBWithPrimaryKeyAutoDetection tests that primary keys are auto-detected
func TestDuckDBWithPrimaryKeyAutoDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create temp file paths using t.TempDir for cross-platform compatibility
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, fmt.Sprintf("source_pk_%d.duckdb", time.Now().UnixNano()))
	destPath := filepath.Join(tmpDir, fmt.Sprintf("dest_pk_%d.db", time.Now().UnixNano()))

	sourceURI := fmt.Sprintf("duckdb:///%s", sourcePath)
	destURI := fmt.Sprintf("sqlite:///%s", destPath)

	// Setup DuckDB with primary key
	setupDuckDBWithPrimaryKey(t, ctx, sourcePath)

	// Run ingestion with merge (requires PK)
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "main.pk_table",
		DestURI:             destURI,
		DestTable:           "pk_table",
		IncrementalStrategy: "merge",
		// Don't specify PrimaryKeys - should be auto-detected
	}

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err, "Pipeline should auto-detect PKs and run merge")

	// Validate results
	validateSQLitePKResults(t, destPath, 50)
}

// TestPostgresToPostgresCamelCaseColumns reproduces a bug where the naming
// convention renames source columns (e.g. FirstName → first_name) in-place,
// and the renamed names are then used in the source SELECT query, causing:
//
//	ERROR: column "first_name" does not exist (SQLSTATE 42703)
func TestPostgresToPostgresCamelCaseColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "src")
	destSchema := uniqueSchemaName(t, "dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	// Create source table with camelCase column names (quoted to preserve case)
	db, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s ("Id" SERIAL, "FirstName" VARCHAR(100), "LastName" VARCHAR(100), "CreatedAt" TIMESTAMP)`,
		pqTable(sourceSchema, "contacts"),
	))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s ("FirstName", "LastName", "CreatedAt")
		 SELECT 'First' || i, 'Last' || i, NOW() FROM generate_series(1, 10) AS i`,
		pqTable(sourceSchema, "contacts"),
	))
	require.NoError(t, err)

	// Run pipeline with default naming convention (snake_case)
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         sourceSchema + ".contacts",
		DestURI:             destURI,
		DestTable:           destSchema + ".contacts",
		IncrementalStrategy: "replace",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err, "Pipeline with camelCase source columns should succeed")

	// Validate destination has data with snake_case column names
	destDB, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	err = destDB.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s`, pqTable(destSchema, "contacts"),
	)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count, "destination should have 10 rows")
}

func setupPostgresSourceData(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pqIdent(schemaName)))
	require.NoError(t, err)

	// Create table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+pqTable(schemaName, tableName)+` (
			id SERIAL,
			name VARCHAR(100),
			email VARCHAR(200),
			age INTEGER,
			salary NUMERIC(10,2),
			is_active BOOLEAN,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Insert test data
	_, err = db.ExecContext(ctx, `
		INSERT INTO `+pqTable(schemaName, tableName)+` (name, email, age, salary, is_active, created_at)
		SELECT
			'User ' || i,
			'user' || i || '@example.com',
			18 + (i % 50),
			50000 + (random() * 100000)::numeric(10,2),
			(i % 3 <> 0),
			'2024-01-01'::timestamp + (i || ' minutes')::interval
		FROM generate_series(1, 100) AS i
	`)
	require.NoError(t, err)

	t.Log("Source PostgreSQL data setup complete")
}

func setupPostgresSourceDataWithPK(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pqIdent(schemaName)))
	require.NoError(t, err)

	// Create table with primary key
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+pqTable(schemaName, tableName)+` (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			email VARCHAR(200),
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	// Insert test data
	_, err = db.ExecContext(ctx, `
		INSERT INTO `+pqTable(schemaName, tableName)+` (name, email)
		SELECT
			'User ' || i,
			'user' || i || '@example.com'
		FROM generate_series(1, 50) AS i
	`)
	require.NoError(t, err)

	t.Log("Source PostgreSQL data with PK setup complete")
}

func updatePostgresSourceData(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Update some records
	_, err = db.ExecContext(ctx, `
		UPDATE `+pqTable(schemaName, tableName)+` SET name = 'Updated User ' || id, updated_at = NOW()
		WHERE id <= 25
	`)
	require.NoError(t, err)

	t.Log("Source PostgreSQL data updated")
}

func validatePostgresResults(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string, expectedCount int) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(schemaName, tableName))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expectedCount, count, "Row count mismatch")

	t.Logf("Validated %d rows in destination PostgreSQL", count)
}

func validatePostgresMergedResults(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Check total count is still 50
	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(schemaName, tableName))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count, "Row count should remain 50 after merge")

	// Check updated records
	var updatedCount int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE name LIKE 'Updated User %%'", pqTable(schemaName, tableName))).Scan(&updatedCount)
	require.NoError(t, err)
	assert.Equal(t, 25, updatedCount, "Should have 25 updated records")

	t.Logf("Validated merge results: %d total, %d updated", count, updatedCount)
}

func validatePostgresTestDataResults(t *testing.T, ctx context.Context, uri string, schemaName string, tableName string, expectedCount int) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(schemaName, tableName))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expectedCount, count, "Row count mismatch")

	t.Logf("Validated %d rows in destination PostgreSQL test_data table", count)
}

func validateSQLiteResults(t *testing.T, dbPath string, expectedCount int) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expectedCount, count, "Row count mismatch")

	t.Logf("Validated %d rows in destination SQLite", count)
}

func validateSQLiteTestDataResults(t *testing.T, dbPath string, expectedCount int) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_data").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expectedCount, count, "Row count mismatch")

	t.Logf("Validated %d rows in destination SQLite test_data table", count)
}

func validateSQLitePKResults(t *testing.T, dbPath string, expectedCount int) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM pk_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, expectedCount, count, "Row count mismatch")

	t.Logf("Validated %d rows in destination SQLite pk_table", count)
}

func setupDuckDBSourceData(t *testing.T, ctx context.Context, dbPath string) {
	t.Helper()

	// Use DuckDB CLI to set up data (or use the duckdb Go driver)
	db, err := sql.Open("pgx", "") // Placeholder - we'll use command execution
	if err != nil {
		// Fall back to command-line approach
		t.Log("Setting up DuckDB via ADBC driver")
	}
	if db != nil {
		_ = db.Close()
	}

	// For now, use the ADBC driver directly
	setupDuckDBViaADBC(t, dbPath)
}

func setupDuckDBViaADBC(t *testing.T, dbPath string) {
	t.Helper()

	ctx := context.Background()

	// We need to use the ADBC driver to create DuckDB data
	// Import the adbc package to set up DuckDB
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", dbPath))
	if err != nil {
		t.Skipf("DuckDB ADBC driver not available: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	// Create table - Note: ADBC sqldriver requires Query for DDL, not Exec
	rows, err := db.QueryContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_data (
			id INTEGER,
			name VARCHAR,
			value DOUBLE,
			created_at TIMESTAMP
		)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	// Insert test data
	rows, err = db.QueryContext(ctx, `
		INSERT INTO test_data
		SELECT
			i as id,
			'Item ' || i as name,
			random() * 1000 as value,
			'2024-01-01'::timestamp + (i || ' hours')::interval as created_at
		FROM generate_series(1, 200) AS t(i)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	t.Log("DuckDB source data setup complete")
}

func setupDuckDBWithPrimaryKey(t *testing.T, ctx context.Context, dbPath string) {
	t.Helper()

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", dbPath))
	if err != nil {
		t.Skipf("DuckDB ADBC driver not available: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	// Create table with primary key - Note: ADBC sqldriver requires Query for DDL, not Exec
	rows, err := db.QueryContext(ctx, `
		CREATE TABLE IF NOT EXISTS pk_table (
			id INTEGER PRIMARY KEY,
			name VARCHAR,
			value DOUBLE
		)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	// Insert test data
	rows, err = db.QueryContext(ctx, `
		INSERT INTO pk_table
		SELECT
			i as id,
			'Item ' || i as name,
			random() * 1000 as value
		FROM generate_series(1, 50) AS t(i)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	t.Log("DuckDB source data with PK setup complete")
}
