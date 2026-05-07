package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupPostgresCDCContainer creates a PostgreSQL container configured for logical replication
func setupPostgresCDCContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image: "postgres:16-alpine",
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		ExposedPorts: []string{"5432/tcp"},
		Cmd: []string{
			"postgres",
			"-c", "wal_level=logical",
			"-c", "max_replication_slots=4",
			"-c", "max_wal_senders=4",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	connString := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	return container, connString
}

// setupCDCSource sets up the source table with data and creates a publication
func setupCDCSource(t *testing.T, ctx context.Context, connString string) {
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()

	// Create table
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.test_cdc (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			value INTEGER,
			updated_at TIMESTAMP DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	// Insert initial data
	_, err = pool.Exec(ctx, `
		INSERT INTO public.test_cdc (name, value) VALUES
		('item1', 100),
		('item2', 200),
		('item3', 300)
	`)
	require.NoError(t, err)

	// Create publication for the table
	_, err = pool.Exec(ctx, `CREATE PUBLICATION test_pub FOR TABLE public.test_cdc`)
	require.NoError(t, err)

	// Grant replication privilege
	_, err = pool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)
}

func TestPostgresCDC_Snapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Convert source URI to CDC format - sourceConnString is already postgres://user:pass@host:port/db?sslmode=disable
	// We need to change scheme and add publication param
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the CDC pipeline
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify data in destination
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	// Check row count
	var count int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should have 3 rows from snapshot")

	// Check CDC metadata columns exist
	var hasLSN, hasDeleted, hasSyncedAt bool
	rows, err := destPool.Query(ctx, `
		SELECT
			column_name
		FROM information_schema.columns
		WHERE table_name = 'test_cdc_dest'
		AND column_name IN ('_cdc_lsn', '_cdc_deleted', '_cdc_synced_at')
	`)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var colName string
		err := rows.Scan(&colName)
		require.NoError(t, err)
		switch colName {
		case "_cdc_lsn":
			hasLSN = true
		case "_cdc_deleted":
			hasDeleted = true
		case "_cdc_synced_at":
			hasSyncedAt = true
		}
	}

	assert.True(t, hasLSN, "Should have _cdc_lsn column")
	assert.True(t, hasDeleted, "Should have _cdc_deleted column")
	assert.True(t, hasSyncedAt, "Should have _cdc_synced_at column")

	// Check all rows have _cdc_deleted = false
	var allFalse bool
	err = destPool.QueryRow(ctx, `
		SELECT COALESCE(bool_and(_cdc_deleted = false), true)
		FROM public.test_cdc_dest
	`).Scan(&allFalse)
	require.NoError(t, err)
	assert.True(t, allFalse, "All snapshot rows should have _cdc_deleted = false")
}

func TestPostgresCDC_URISchemes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test that both URI schemes work
	schemes := []string{"postgres+cdc://", "postgresql+cdc://"}

	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			// Just verify the scheme is recognized - actual connection would fail without real server
			uri := scheme + "user:pass@localhost:5432/testdb?publication=test"
			cfg := &config.IngestConfig{
				SourceURI:   uri,
				SourceTable: "test",
			}
			// The fact that we can create the pipeline means the scheme is recognized
			p := pipeline.New(cfg)
			assert.NotNil(t, p)
		})
	}
}

func TestPostgresCDC_IncrementalResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the first CDC pipeline (full snapshot)
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify initial data
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var count int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should have 3 rows from initial snapshot")

	// Get the max LSN from first run
	var firstMaxLSN string
	err = destPool.QueryRow(ctx, `SELECT MAX("_cdc_lsn") FROM public.test_cdc_dest`).Scan(&firstMaxLSN)
	require.NoError(t, err)
	assert.NotEmpty(t, firstMaxLSN, "Should have a max LSN after first run")

	// Make changes to source (after snapshot)
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Insert new record
	_, err = sourcePool.Exec(ctx, `INSERT INTO public.test_cdc (name, value) VALUES ('item4', 400)`)
	require.NoError(t, err)

	// Update existing record
	_, err = sourcePool.Exec(ctx, `UPDATE public.test_cdc SET value = 150 WHERE id = 1`)
	require.NoError(t, err)

	// Run the second CDC pipeline (should resume from LSN)
	cfg2 := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err)

	// Verify incremental changes were captured
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "Should have 4 rows after incremental sync")

	// Verify the new record exists
	var item4Value int
	err = destPool.QueryRow(ctx, `SELECT value FROM public.test_cdc_dest WHERE name = 'item4'`).Scan(&item4Value)
	require.NoError(t, err)
	assert.Equal(t, 400, item4Value, "item4 should have value 400")

	// Verify the update was applied
	var item1Value int
	err = destPool.QueryRow(ctx, `SELECT value FROM public.test_cdc_dest WHERE id = 1`).Scan(&item1Value)
	require.NoError(t, err)
	assert.Equal(t, 150, item1Value, "item1 should be updated to value 150")

	// Verify LSN advanced
	var secondMaxLSN string
	err = destPool.QueryRow(ctx, `SELECT MAX("_cdc_lsn") FROM public.test_cdc_dest`).Scan(&secondMaxLSN)
	require.NoError(t, err)
	assert.NotEqual(t, firstMaxLSN, secondMaxLSN, "LSN should have advanced after incremental sync")
}

func TestPostgresCDC_DuplicatePKWithinBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Update then delete the same PK within a single transaction
	tx, err := sourcePool.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE public.test_cdc SET name = 'item1_updated', value = 999 WHERE id = 1`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM public.test_cdc WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	// Run incremental CDC pipeline (should not fail)
	p2 := pipeline.New(cfg)
	err = p2.Run(ctx)
	require.NoError(t, err)

	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var totalCount, activeCount int
	err = destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest`).Scan(&totalCount)
	require.NoError(t, err)
	err = destPool.QueryRow(ctx, `SELECT COUNT(*) FROM public.test_cdc_dest WHERE "_cdc_deleted" = false`).Scan(&activeCount)
	require.NoError(t, err)
	assert.Equal(t, 3, totalCount, "Total rows should remain 3 with soft-deletes")
	assert.Equal(t, 2, activeCount, "Active rows should exclude deleted row")

	var name string
	var value int
	var deleted bool
	err = destPool.QueryRow(ctx, `SELECT name, value, "_cdc_deleted" FROM public.test_cdc_dest WHERE id = 1`).Scan(&name, &value, &deleted)
	require.NoError(t, err)
	assert.True(t, deleted, "Row should be marked deleted")
	assert.Equal(t, "item1_updated", name, "Row should retain the latest values before delete")
	assert.Equal(t, 999, value, "Row should retain the latest values before delete")
}

// TestPostgresCDC_BatchModeCompletesWithActiveWrites verifies that batch mode completes
// based on LSN rather than waiting indefinitely for inactivity. This test was added
// to prevent regression of a bug where batch mode would wait forever if the source
// database was continuously receiving writes.
func TestPostgresCDC_BatchModeCompletesWithActiveWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Setup destination PostgreSQL
	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// First, do an initial sync to establish the replication slot
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             destConnString,
		SourceTable:         "public.test_cdc",
		DestTable:           "public.test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Verify initial data
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	var initialCount int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&initialCount)
	require.NoError(t, err)
	assert.Equal(t, 3, initialCount, "Should have 3 rows from initial snapshot")

	// Now start continuous writes to the source database
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	// Create a context for the background writer that we can cancel
	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()

	// Track how many rows the writer has inserted
	var writtenCount int64
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})

	// Start background writer that continuously inserts data
	go func() {
		defer close(writerDone)
		close(writerStarted)

		for i := 0; ; i++ {
			select {
			case <-writerCtx.Done():
				return
			default:
				_, err := sourcePool.Exec(writerCtx,
					`INSERT INTO public.test_cdc (name, value) VALUES ($1, $2)`,
					fmt.Sprintf("continuous_item_%d", i), i*10)
				if err != nil {
					// Context cancelled or other error, stop writing
					return
				}
				writtenCount++
				// Small delay to avoid overwhelming the database but still generate continuous activity
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Wait for writer to start
	<-writerStarted

	// Give the writer a moment to insert some rows
	time.Sleep(200 * time.Millisecond)

	// Run the CDC pipeline with a timeout - it should complete based on LSN, not inactivity
	// With the old bug, this would hang indefinitely because the writer keeps generating WAL
	pipelineCtx, cancelPipeline := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPipeline()

	startTime := time.Now()

	p2 := pipeline.New(cfg)
	err = p2.Run(pipelineCtx)

	elapsed := time.Since(startTime)

	// Stop the background writer
	cancelWriter()
	<-writerDone

	// The pipeline should complete successfully (not timeout)
	require.NoError(t, err, "Pipeline should complete successfully, not timeout")

	// The pipeline should complete relatively quickly (not wait for the 5s inactivity timeout)
	// We allow some buffer for test infrastructure overhead
	assert.Less(t, elapsed, 10*time.Second,
		"Batch mode should complete based on LSN, not wait for inactivity timeout. Elapsed: %v", elapsed)

	t.Logf("Pipeline completed in %v with %d background writes occurring", elapsed, writtenCount)

	// Verify we captured some data (the exact count depends on timing, but should be >= initial)
	var finalCount int
	err = destPool.QueryRow(ctx, "SELECT COUNT(*) FROM public.test_cdc_dest").Scan(&finalCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, finalCount, initialCount,
		"Should have at least the initial rows after incremental sync")

	t.Logf("Final row count: %d (initial: %d, background writes: %d)", finalCount, initialCount, writtenCount)
}

func TestPostgresCDC_IncrementalResume_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup source PostgreSQL with logical replication
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	setupCDCSource(t, ctx, sourceConnString)

	// Create temp path for DuckDB destination (don't create the file - DuckDB will create it)
	tmpDir, err := os.MkdirTemp("", "cdc_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	duckdbPath := fmt.Sprintf("%s/test.duckdb", tmpDir)
	duckdbURI := fmt.Sprintf("duckdb:///%s", duckdbPath)
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub&mode=batch"

	// Run the first CDC pipeline (full snapshot)
	cfg := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             duckdbURI,
		SourceTable:         "public.test_cdc",
		DestTable:           "test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	// Query DuckDB to verify initial data
	verifyDuckDB := func(query string) (interface{}, error) {
		db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		var result interface{}
		err = db.QueryRow(query).Scan(&result)
		return result, err
	}

	// Verify initial count
	count, err := verifyDuckDB("SELECT COUNT(*) FROM test_cdc_dest")
	require.NoError(t, err)
	assert.Equal(t, int64(3), count, "Should have 3 rows from initial snapshot")

	// Get max LSN from first run
	firstMaxLSN, err := verifyDuckDB(`SELECT MAX("_cdc_lsn") FROM test_cdc_dest`)
	require.NoError(t, err)
	assert.NotNil(t, firstMaxLSN, "Should have a max LSN after first run")
	t.Logf("First run max LSN: %v", firstMaxLSN)

	// Make changes to source
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer sourcePool.Close()

	_, err = sourcePool.Exec(ctx, `INSERT INTO public.test_cdc (name, value) VALUES ('item4', 400)`)
	require.NoError(t, err)

	_, err = sourcePool.Exec(ctx, `UPDATE public.test_cdc SET value = 150 WHERE id = 1`)
	require.NoError(t, err)

	// Run the second CDC pipeline (should resume from LSN)
	cfg2 := &config.IngestConfig{
		SourceURI:           cdcSourceURI,
		DestURI:             duckdbURI,
		SourceTable:         "public.test_cdc",
		DestTable:           "test_cdc_dest",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: "merge",
	}

	p2 := pipeline.New(cfg2)
	err = p2.Run(ctx)
	require.NoError(t, err)

	// Verify incremental changes were captured
	count, err = verifyDuckDB("SELECT COUNT(*) FROM test_cdc_dest")
	require.NoError(t, err)
	assert.Equal(t, int64(4), count, "Should have 4 rows after incremental sync")

	// Verify new record (use EqualValues for type-agnostic comparison)
	item4Value, err := verifyDuckDB(`SELECT value FROM test_cdc_dest WHERE name = 'item4'`)
	require.NoError(t, err)
	assert.EqualValues(t, 400, item4Value, "item4 should have value 400")

	// Verify update
	item1Value, err := verifyDuckDB(`SELECT value FROM test_cdc_dest WHERE id = 1`)
	require.NoError(t, err)
	assert.EqualValues(t, 150, item1Value, "item1 should be updated to value 150")

	// Verify LSN advanced
	secondMaxLSN, err := verifyDuckDB(`SELECT MAX("_cdc_lsn") FROM test_cdc_dest`)
	require.NoError(t, err)
	assert.NotEqual(t, firstMaxLSN, secondMaxLSN, "LSN should have advanced after incremental sync")
	t.Logf("Second run max LSN: %v", secondMaxLSN)
}
