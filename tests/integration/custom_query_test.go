//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomQuery_PostgresToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "cq_src")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	setupCustomQuerySourceData(t, ctx, sourceURI, sourceSchema)

	tmpFile, err := os.CreateTemp("", "cq_pg_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	tests := []struct {
		name          string
		query         string
		destTable     string
		expectedCount int
		verify        func(t *testing.T, db *sql.DB)
	}{
		{
			name:          "select all",
			query:         fmt.Sprintf("SELECT * FROM %s", pqTable(sourceSchema, "orders")),
			destTable:     "all_orders",
			expectedCount: 100,
			verify: func(t *testing.T, db *sql.DB) {
				var id int
				var amount float64
				var status string
				err := db.QueryRow("SELECT id, amount, status FROM all_orders ORDER BY id LIMIT 1").Scan(&id, &amount, &status)
				require.NoError(t, err)
				assert.Greater(t, id, 0)
				assert.Greater(t, amount, 0.0)
				assert.Contains(t, []string{"active", "inactive"}, status)
			},
		},
		{
			name:          "filtered query",
			query:         fmt.Sprintf("SELECT * FROM %s WHERE status = 'active'", pqTable(sourceSchema, "orders")),
			destTable:     "active_orders",
			expectedCount: 50,
			verify: func(t *testing.T, db *sql.DB) {
				// All statuses should be 'active'
				var distinctCount int
				err := db.QueryRow("SELECT COUNT(DISTINCT status) FROM active_orders").Scan(&distinctCount)
				require.NoError(t, err)
				assert.Equal(t, 1, distinctCount)

				var status string
				err = db.QueryRow("SELECT DISTINCT status FROM active_orders").Scan(&status)
				require.NoError(t, err)
				assert.Equal(t, "active", status)
			},
		},
		{
			name:          "aggregation query",
			query:         fmt.Sprintf("SELECT status, COUNT(*) as cnt, SUM(amount) as total FROM %s GROUP BY status", pqTable(sourceSchema, "orders")),
			destTable:     "order_stats",
			expectedCount: 2,
			verify: func(t *testing.T, db *sql.DB) {
				var status string
				var cnt int
				var total float64
				err := db.QueryRow("SELECT status, cnt, total FROM order_stats WHERE status = 'active'").Scan(&status, &cnt, &total)
				require.NoError(t, err)
				assert.Equal(t, "active", status)
				assert.Equal(t, 50, cnt)
				assert.Greater(t, total, 0.0)
			},
		},
		{
			name:          "select specific columns",
			query:         fmt.Sprintf("SELECT id, amount FROM %s WHERE amount > 500", pqTable(sourceSchema, "orders")),
			destTable:     "high_value",
			expectedCount: 50,
			verify: func(t *testing.T, db *sql.DB) {
				// Verify only 2 columns exist
				rows, err := db.Query("SELECT * FROM high_value LIMIT 1")
				require.NoError(t, err)
				cols, err := rows.Columns()
				require.NoError(t, err)
				_ = rows.Close()
				cols = withoutLoadTimestampColumns(cols)
				assert.Equal(t, 2, len(cols), "should only have id and amount columns")

				var minAmount float64
				err = db.QueryRow("SELECT MIN(amount) FROM high_value").Scan(&minAmount)
				require.NoError(t, err)
				assert.Greater(t, minAmount, 500.0, "all amounts should be > 500")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "query:" + tt.query,
				DestURI:             destURI,
				DestTable:           tt.destTable,
				IncrementalStrategy: config.StrategyReplace,
			}
			require.NoError(t, cfg.Validate())

			p := pipeline.New(cfg)
			err := p.Run(ctx)
			require.NoError(t, err)

			db, err := sql.Open("sqlite3", tmpFile.Name())
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			var count int
			err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tt.destTable)).Scan(&count)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, count)

			if tt.verify != nil {
				tt.verify(t, db)
			}
		})
	}
}

func TestCustomQuery_PostgresToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "cq_src")
	destSchema := uniqueSchemaName(t, "cq_dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	setupCustomQuerySourceData(t, ctx, sourceURI, sourceSchema)

	query := fmt.Sprintf("SELECT id, amount, status FROM %s WHERE status = 'active' AND amount > 500", pqTable(sourceSchema, "orders"))
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:" + query,
		DestURI:             destURI,
		DestTable:           destSchema + ".filtered_orders",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err := p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "filtered_orders"))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count)

	// Verify column types and values
	var id int
	var amount float64
	var status string
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT id, amount, status FROM %s ORDER BY id LIMIT 1", pqTable(destSchema, "filtered_orders"))).Scan(&id, &amount, &status)
	require.NoError(t, err)
	assert.Greater(t, id, 0, "id should be a positive integer")
	assert.Greater(t, amount, 500.0, "amount should be > 500")
	assert.Equal(t, "active", status, "status should be 'active'")

	// Verify all amounts are > 500
	var minAmount float64
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT MIN(amount) FROM %s", pqTable(destSchema, "filtered_orders"))).Scan(&minAmount)
	require.NoError(t, err)
	assert.Greater(t, minAmount, 500.0, "all amounts should be > 500")
}

func TestCustomQuery_PostgresPartitionedExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "cq_partition_src")
	destSchema := uniqueSchemaName(t, "cq_partition_dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})
	setupCustomQuerySourceData(t, ctx, sourceURI, sourceSchema)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(99 * time.Minute)
	query := fmt.Sprintf(`
		SELECT
			o.id,
			o.amount,
			o.status,
			o.created_at AS partition_ts,
			o.created_at AS updated_at
		FROM %s AS o
		JOIN (VALUES ('active'), ('inactive')) AS allowed(status)
			ON allowed.status = o.status
		WHERE o.created_at >= :interval_start
			AND o.created_at <= :interval_end
	`, pqTable(sourceSchema, "orders"))
	cfg := config.DefaultConfig()
	cfg.SourceURI = sourceURI
	cfg.SourceTable = "query:" + query
	cfg.DestURI = destURI
	cfg.DestTable = destSchema + ".partitioned_orders"
	cfg.IncrementalStrategy = config.StrategyAppend
	cfg.IncrementalKey = "updated_at"
	cfg.IntervalStart = &start
	cfg.IntervalEnd = &end
	cfg.ExtractPartitionBy = "partition_ts"
	cfg.ExtractPartitionInterval = 20 * time.Minute
	cfg.ExtractParallelism = 3

	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count, distinctCount, boundaryCount int
	err = db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*), COUNT(DISTINCT id), COUNT(*) FILTER (WHERE id IN (1, 100))
		FROM %s
	`, pqTable(destSchema, "partitioned_orders"))).Scan(&count, &distinctCount, &boundaryCount)
	require.NoError(t, err)
	assert.Equal(t, 100, count)
	assert.Equal(t, 100, distinctCount)
	assert.Equal(t, 2, boundaryCount)
}

func TestCustomQuery_WithIntervalParams(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	sourceSchema := uniqueSchemaName(t, "cq_int")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, sourceURI, sourceSchema) })

	setupCustomQuerySourceData(t, ctx, sourceURI, sourceSchema)

	tmpFile, err := os.CreateTemp("", "cq_interval_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 0, 50, 0, 0, time.UTC)

	query := fmt.Sprintf(
		"SELECT * FROM %s WHERE created_at >= :interval_start AND created_at < :interval_end",
		pqTable(sourceSchema, "orders"),
	)

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:" + query,
		DestURI:             destURI,
		DestTable:           "interval_orders",
		IncrementalStrategy: config.StrategyReplace,
		IntervalStart:       &start,
		IntervalEnd:         &end,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM interval_orders").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count)

	// Verify all rows are within the interval
	var minTSStr, maxTSStr string
	err = db.QueryRow("SELECT MIN(created_at), MAX(created_at) FROM interval_orders").Scan(&minTSStr, &maxTSStr)
	require.NoError(t, err)
	minTS, err := time.Parse("2006-01-02 15:04:05.000000", minTSStr)
	require.NoError(t, err)
	maxTS, err := time.Parse("2006-01-02 15:04:05.000000", maxTSStr)
	require.NoError(t, err)
	assert.False(t, minTS.Before(start), "min timestamp should be >= interval_start")
	assert.True(t, maxTS.Before(end), "max timestamp should be < interval_end")
}

func TestCustomQuery_DuckDBToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	duckDBPath := filepath.Join(tmpDir, fmt.Sprintf("cq_source_%d.duckdb", time.Now().UnixNano()))
	sqlitePath := filepath.Join(tmpDir, fmt.Sprintf("cq_dest_%d.db", time.Now().UnixNano()))

	setupDuckDBCustomQueryData(t, duckDBPath)

	sourceURI := fmt.Sprintf("duckdb:///%s", duckDBPath)
	destURI := fmt.Sprintf("sqlite:///%s", sqlitePath)

	tests := []struct {
		name          string
		query         string
		destTable     string
		expectedCount int
		verify        func(t *testing.T, db *sql.DB)
	}{
		{
			name:          "filtered select",
			query:         "SELECT * FROM products WHERE price > 500",
			destTable:     "expensive_products",
			expectedCount: 50,
			verify: func(t *testing.T, db *sql.DB) {
				var id int
				var name, category string
				var price float64
				err := db.QueryRow("SELECT id, name, price, category FROM expensive_products ORDER BY id LIMIT 1").Scan(&id, &name, &price, &category)
				require.NoError(t, err)
				assert.Greater(t, id, 0)
				assert.NotEmpty(t, name)
				assert.Greater(t, price, 500.0)
				assert.Equal(t, "electronics", category)

				var minPrice float64
				err = db.QueryRow("SELECT MIN(price) FROM expensive_products").Scan(&minPrice)
				require.NoError(t, err)
				assert.Greater(t, minPrice, 500.0)
			},
		},
		{
			name:          "aggregation",
			query:         "SELECT category, COUNT(*) as cnt, AVG(price) as avg_price FROM products GROUP BY category",
			destTable:     "category_stats",
			expectedCount: 2,
			verify: func(t *testing.T, db *sql.DB) {
				var category string
				var cnt int
				var avgPrice float64
				err := db.QueryRow("SELECT category, cnt, avg_price FROM category_stats ORDER BY category LIMIT 1").Scan(&category, &cnt, &avgPrice)
				require.NoError(t, err)
				assert.NotEmpty(t, category)
				assert.Equal(t, 50, cnt)
				assert.Greater(t, avgPrice, 0.0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "query:" + tt.query,
				DestURI:             destURI,
				DestTable:           tt.destTable,
				IncrementalStrategy: config.StrategyReplace,
			}
			require.NoError(t, cfg.Validate())

			p := pipeline.New(cfg)
			err := p.Run(ctx)
			require.NoError(t, err)

			db, err := sql.Open("sqlite3", sqlitePath)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			var count int
			err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tt.destTable)).Scan(&count)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, count)

			if tt.verify != nil {
				tt.verify(t, db)
			}
		})
	}
}

func TestCustomQuery_MySQLToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("MySQL container not available")
	}

	ctx := context.Background()
	setupMySQLCustomQueryData(t, ctx, mysqlDest.uri)

	tmpFile, err := os.CreateTemp("", "cq_mysql_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &config.IngestConfig{
		SourceURI:           mysqlDest.uri,
		SourceTable:         "query:SELECT * FROM cq_test_products WHERE price > 500",
		DestURI:             destURI,
		DestTable:           "mysql_expensive",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM mysql_expensive").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count)

	// Verify types: id is numeric, name is text, price > 500
	var id int
	var name string
	var price float64
	err = db.QueryRow("SELECT id, name, price FROM mysql_expensive ORDER BY id LIMIT 1").Scan(&id, &name, &price)
	require.NoError(t, err)
	assert.Greater(t, id, 0)
	assert.NotEmpty(t, name)
	assert.Greater(t, price, 500.0)

	var minPrice float64
	err = db.QueryRow("SELECT MIN(price) FROM mysql_expensive").Scan(&minPrice)
	require.NoError(t, err)
	assert.Greater(t, minPrice, 500.0, "all prices should be > 500")
}

func TestCustomQuery_MSSQLToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("MSSQL container not available")
	}

	ctx := context.Background()
	setupMSSQLCustomQueryData(t, ctx, mssqlDest.uri)

	tmpFile, err := os.CreateTemp("", "cq_mssql_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &config.IngestConfig{
		SourceURI:           mssqlDest.uri,
		SourceTable:         "query:SELECT * FROM dbo.cq_test_products WHERE price > 500",
		DestURI:             destURI,
		DestTable:           "mssql_expensive",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM mssql_expensive").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 50, count)

	// Verify types: id is numeric, name is text, price > 500
	var id int
	var name string
	var price float64
	err = db.QueryRow("SELECT id, name, price FROM mssql_expensive ORDER BY id LIMIT 1").Scan(&id, &name, &price)
	require.NoError(t, err)
	assert.Greater(t, id, 0)
	assert.NotEmpty(t, name)
	assert.Greater(t, price, 500.0)

	var minPrice float64
	err = db.QueryRow("SELECT MIN(price) FROM mssql_expensive").Scan(&minPrice)
	require.NoError(t, err)
	assert.Greater(t, minPrice, 500.0, "all prices should be > 500")
}

func TestCustomQuery_ClickHouseToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if chDest.uri == "" {
		t.Skip("ClickHouse container not available")
	}

	ctx := context.Background()
	setupClickHouseCustomQueryData(t, ctx, chDest.uri)

	tmpFile, err := os.CreateTemp("", "cq_ch_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	t.Run("filtered select", func(t *testing.T) {
		query := fmt.Sprintf("SELECT * FROM %s.cq_test_products WHERE price > 500", clickhouseDB)
		cfg := &config.IngestConfig{
			SourceURI:           chDest.uri,
			SourceTable:         "query:" + query,
			DestURI:             destURI,
			DestTable:           "ch_expensive",
			IncrementalStrategy: config.StrategyReplace,
		}
		require.NoError(t, cfg.Validate())

		p := pipeline.New(cfg)
		err := p.Run(ctx)
		require.NoError(t, err)

		db, err := sql.Open("sqlite3", tmpFile.Name())
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM ch_expensive").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 50, count)

		// Verify types: id is numeric, name is text, price > 500, category is text
		var id int
		var name, category string
		var price float64
		err = db.QueryRow("SELECT id, name, price, category FROM ch_expensive ORDER BY id LIMIT 1").Scan(&id, &name, &price, &category)
		require.NoError(t, err)
		assert.Greater(t, id, 0)
		assert.NotEmpty(t, name)
		assert.Greater(t, price, 500.0)
		assert.Equal(t, "electronics", category)

		var minPrice float64
		err = db.QueryRow("SELECT MIN(price) FROM ch_expensive").Scan(&minPrice)
		require.NoError(t, err)
		assert.Greater(t, minPrice, 500.0, "all prices should be > 500")
	})

	t.Run("aggregation", func(t *testing.T) {
		query := fmt.Sprintf("SELECT category, toInt64(count()) as cnt, avg(price) as avg_price FROM %s.cq_test_products GROUP BY category", clickhouseDB)
		cfg := &config.IngestConfig{
			SourceURI:           chDest.uri,
			SourceTable:         "query:" + query,
			DestURI:             destURI,
			DestTable:           "ch_category_stats",
			IncrementalStrategy: config.StrategyReplace,
		}
		require.NoError(t, cfg.Validate())

		p := pipeline.New(cfg)
		err := p.Run(ctx)
		require.NoError(t, err)

		db, err := sql.Open("sqlite3", tmpFile.Name())
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM ch_category_stats").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify types: category is text, cnt is numeric, avg_price is numeric
		var category string
		var cnt int
		var avgPrice float64
		err = db.QueryRow("SELECT category, cnt, avg_price FROM ch_category_stats ORDER BY category LIMIT 1").Scan(&category, &cnt, &avgPrice)
		require.NoError(t, err)
		assert.NotEmpty(t, category)
		assert.Equal(t, 50, cnt)
		assert.Greater(t, avgPrice, 0.0)
	})
}

func TestCustomQuery_DatabricksToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	databricksURI := os.Getenv("GONG_TEST_DATABRICKS_URI")
	if databricksURI == "" {
		t.Skip("Set GONG_TEST_DATABRICKS_URI to run Databricks custom query tests")
	}

	ctx := context.Background()
	tmpFile, err := os.CreateTemp("", "cq_databricks_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	databricksTable := os.Getenv("GONG_TEST_DATABRICKS_TABLE")
	if databricksTable == "" {
		t.Skip("Set GONG_TEST_DATABRICKS_TABLE to the fully qualified table name (e.g. catalog.schema.table)")
	}

	cfg := &config.IngestConfig{
		SourceURI:           databricksURI,
		SourceTable:         fmt.Sprintf("query:SELECT * FROM %s LIMIT 10", databricksTable),
		DestURI:             destURI,
		DestTable:           "databricks_result",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM databricks_result").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestCustomQuery_AthenaToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	athenaURI := os.Getenv("GONG_TEST_ATHENA_URI")
	if athenaURI == "" {
		t.Skip("Set GONG_TEST_ATHENA_URI to run Athena custom query tests")
	}

	ctx := context.Background()
	tmpFile, err := os.CreateTemp("", "cq_athena_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	athenaTable := os.Getenv("GONG_TEST_ATHENA_TABLE")
	if athenaTable == "" {
		t.Skip("Set GONG_TEST_ATHENA_TABLE to the fully qualified table name (e.g. database.table)")
	}

	cfg := &config.IngestConfig{
		SourceURI:           athenaURI,
		SourceTable:         fmt.Sprintf("query:SELECT * FROM %s LIMIT 10", athenaTable),
		DestURI:             destURI,
		DestTable:           "athena_result",
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	err = p.Run(ctx)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM athena_result").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestCustomQuery_MergeStrategy_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := sharedPostgresURI(t, "source")
	destURI := sharedPostgresURI(t, "dest")
	sourceSchema := uniqueSchemaName(t, "cq_merge_src")
	destSchema := uniqueSchemaName(t, "cq_merge_dst")
	ensurePostgresSchema(t, ctx, sourceURI, sourceSchema)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() {
		dropPostgresSchema(t, ctx, sourceURI, sourceSchema)
		dropPostgresSchema(t, ctx, destURI, destSchema)
	})

	srcDB, err := sql.Open("pgx", sourceURI)
	require.NoError(t, err)
	defer func() { _ = srcDB.Close() }()

	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
		id INTEGER PRIMARY KEY,
		name VARCHAR(50),
		score INTEGER
	)`, pqTable(sourceSchema, "players")))
	require.NoError(t, err)

	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, name, score) VALUES
		(1, 'Alice', 100),
		(2, 'Bob', 200),
		(3, 'Charlie', 300)
	`, pqTable(sourceSchema, "players")))
	require.NoError(t, err)

	// Run 1: initial load with custom query + merge
	query := fmt.Sprintf("SELECT id, name, score FROM %s WHERE score >= 200", pqTable(sourceSchema, "players"))
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:" + query,
		DestURI:             destURI,
		DestTable:           destSchema + ".players",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	require.NoError(t, p.Run(ctx))

	destDB, err := sql.Open("pgx", destURI)
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	var count int
	err = destDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "players"))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "initial merge should insert 2 rows (Bob, Charlie)")

	// Update source: change Bob's score, add a new player
	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET score = 250 WHERE id = 2`, pqTable(sourceSchema, "players")))
	require.NoError(t, err)
	_, err = srcDB.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, name, score) VALUES (4, 'Diana', 400)`, pqTable(sourceSchema, "players")))
	require.NoError(t, err)

	// Run 2: merge again — should upsert Bob and insert Diana
	cfg2 := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:" + query,
		DestURI:             destURI,
		DestTable:           destSchema + ".players",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, cfg2.Validate())

	p2 := pipeline.New(cfg2)
	require.NoError(t, p2.Run(ctx))

	err = destDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", pqTable(destSchema, "players"))).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "after second merge should have 3 rows (Bob, Charlie, Diana)")

	// Verify Bob's score was updated
	var score int
	err = destDB.QueryRowContext(ctx, fmt.Sprintf("SELECT score FROM %s WHERE id = 2", pqTable(destSchema, "players"))).Scan(&score)
	require.NoError(t, err)
	assert.Equal(t, 250, score, "Bob's score should be updated to 250")
}

func TestCustomQuery_MergeStrategy_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	duckDBPath := filepath.Join(tmpDir, "cq_merge_source.duckdb")
	sqlitePath := filepath.Join(tmpDir, "cq_merge_dest.db")

	sourceURI := fmt.Sprintf("duckdb:///%s", duckDBPath)
	destURI := fmt.Sprintf("sqlite:///%s", sqlitePath)

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	if err != nil {
		t.Skipf("DuckDB ADBC driver not available: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		CREATE TABLE items (
			id INTEGER,
			name VARCHAR,
			quantity INTEGER
		)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	rows, err = db.QueryContext(ctx, `
		INSERT INTO items VALUES (1, 'Widget', 10), (2, 'Gadget', 20), (3, 'Doohickey', 30)
	`)
	require.NoError(t, err)
	_ = rows.Close()
	_ = db.Close()

	// Run 1: initial merge
	cfg := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:SELECT * FROM items WHERE quantity >= 20",
		DestURI:             destURI,
		DestTable:           "items",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, cfg.Validate())

	p := pipeline.New(cfg)
	require.NoError(t, p.Run(ctx))

	destDB, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)

	var count int
	err = destDB.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "initial merge should have 2 rows")
	_ = destDB.Close()

	// Update source: change Gadget quantity, add new item
	db, err = sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	rows, err = db.QueryContext(ctx, `UPDATE items SET quantity = 25 WHERE id = 2`)
	require.NoError(t, err)
	_ = rows.Close()
	rows, err = db.QueryContext(ctx, `INSERT INTO items VALUES (4, 'Thingamajig', 40)`)
	require.NoError(t, err)
	_ = rows.Close()
	_ = db.Close()

	// Run 2: merge again
	cfg2 := &config.IngestConfig{
		SourceURI:           sourceURI,
		SourceTable:         "query:SELECT * FROM items WHERE quantity >= 20",
		DestURI:             destURI,
		DestTable:           "items",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, cfg2.Validate())

	p2 := pipeline.New(cfg2)
	require.NoError(t, p2.Run(ctx))

	destDB, err = sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	defer func() { _ = destDB.Close() }()

	err = destDB.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "after second merge should have 3 rows")

	var quantity int
	err = destDB.QueryRow("SELECT quantity FROM items WHERE id = 2").Scan(&quantity)
	require.NoError(t, err)
	assert.Equal(t, 25, quantity, "Gadget quantity should be updated to 25")
}

// --- Setup helpers ---

func setupCustomQuerySourceData(t *testing.T, ctx context.Context, uri string, schemaName string) {
	t.Helper()

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id SERIAL,
		amount NUMERIC(10,2),
		status VARCHAR(20),
		created_at TIMESTAMP
	)`, pqTable(schemaName, "orders")))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (amount, status, created_at)
		SELECT
			500.50 + i,
			'active',
			'2024-01-01'::timestamp + (i || ' minutes')::interval
		FROM generate_series(0, 49) AS i
	`, pqTable(schemaName, "orders")))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (amount, status, created_at)
		SELECT
			100.00 + i,
			'inactive',
			'2024-01-01'::timestamp + ((50 + i) || ' minutes')::interval
		FROM generate_series(0, 49) AS i
	`, pqTable(schemaName, "orders")))
	require.NoError(t, err)
}

func setupDuckDBCustomQueryData(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", dbPath))
	if err != nil {
		t.Skipf("DuckDB ADBC driver not available: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `
		CREATE TABLE IF NOT EXISTS products (
			id INTEGER,
			name VARCHAR,
			price DOUBLE,
			category VARCHAR
		)
	`)
	require.NoError(t, err)
	_ = rows.Close()

	rows, err = db.QueryContext(ctx, `
		INSERT INTO products
		SELECT
			i as id,
			'Product ' || i as name,
			CASE WHEN i <= 50 THEN 500.0 + i ELSE 100.0 + i END as price,
			CASE WHEN i <= 50 THEN 'electronics' ELSE 'clothing' END as category
		FROM generate_series(1, 100) AS t(i)
	`)
	require.NoError(t, err)
	_ = rows.Close()
}

func setupMySQLCustomQueryData(t *testing.T, ctx context.Context, uri string) {
	t.Helper()

	dsn := mysqlDSN(uri)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cq_test_products (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(100),
		price DECIMAL(10,2)
	)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `TRUNCATE TABLE cq_test_products`)
	require.NoError(t, err)

	for i := 1; i <= 100; i++ {
		price := float64(100 + i)
		if i <= 50 {
			price = float64(500 + i)
		}
		_, err = db.ExecContext(ctx, `INSERT INTO cq_test_products (name, price) VALUES (?, ?)`,
			fmt.Sprintf("Product %d", i), price)
		require.NoError(t, err)
	}
}

func setupClickHouseCustomQueryData(t *testing.T, ctx context.Context, uri string) {
	t.Helper()

	conn, err := openClickHouseConn(uri)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_ = conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.cq_test_products", clickhouseDB))
	err = conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s.cq_test_products (
			id Int32,
			name String,
			price Float64,
			category String
		) ENGINE = MergeTree() ORDER BY id
	`, clickhouseDB))
	require.NoError(t, err)

	for i := 1; i <= 100; i++ {
		price := float64(100 + i)
		if i <= 50 {
			price = float64(500 + i)
		}
		category := "clothing"
		if i <= 50 {
			category = "electronics"
		}
		err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO %s.cq_test_products VALUES (?, ?, ?, ?)", clickhouseDB),
			int32(i), fmt.Sprintf("Product %d", i), price, category)
		require.NoError(t, err)
	}
}

func setupMSSQLCustomQueryData(t *testing.T, ctx context.Context, uri string) {
	t.Helper()

	connStr := mssqlConnString(uri)
	db, err := sql.Open("sqlserver", connStr)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS dbo.cq_test_products`)
	_, err = db.ExecContext(ctx, `CREATE TABLE dbo.cq_test_products (
		id INT IDENTITY(1,1) PRIMARY KEY,
		name NVARCHAR(100),
		price DECIMAL(10,2)
	)`)
	require.NoError(t, err)

	for i := 1; i <= 100; i++ {
		price := float64(100 + i)
		if i <= 50 {
			price = float64(500 + i)
		}
		_, err = db.ExecContext(ctx, `INSERT INTO dbo.cq_test_products (name, price) VALUES (@p1, @p2)`,
			fmt.Sprintf("Product %d", i), price)
		require.NoError(t, err)
	}
}
