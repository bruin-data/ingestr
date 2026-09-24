//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const applovinShapedCSV = `Date,Ad Unit ID,Ad Unit Name,Waterfall,Ad Format,Placement,Country,Device Type,IDFA,IDFV,User ID,sessionId,Network,Network.Name,Revenue,Custom-Data
2026-05-05,abc123,banner_main,default,BANNER,top,US,iPhone,,fa-1,user-1,sess-1,AdMob,admob_us,1.50,extra-1
2026-05-05,def456,reward_main,default,REWARD,bottom,GB,iPad,,fa-2,user-2,sess-2,Meta,meta_uk,2.25,extra-2
2026-05-05,ghi789,inter_main,default,INTER,full,DE,iPhone,,fa-3,user-3,sess-3,Unity,unity_de,0.75,extra-3
`

func runCSVtoDuckDB(t *testing.T, csvBody string, mutate func(*config.IngestConfig)) string {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvBody), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
	}
	if mutate != nil {
		mutate(cfg)
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	return duckDBPath
}

func TestAppLovinAssetShape_OverridesAndTypesAppliedAcrossRenames(t *testing.T) {
	duckDBPath := runCSVtoDuckDB(t, applovinShapedCSV, func(cfg *config.IngestConfig) {
		cfg.Columns = "ad_format:string," +
			"ad_unit_id:string," +
			"ad_unit_name:string," +
			"waterfall:string," +
			"placement:string," +
			"country:string," +
			"device_type:string," +
			"idfa:string," +
			"idfv:string," +
			"user_id:string," +
			"session_id:string," +
			"network:string," +
			"network_name:string," +
			"revenue:double," +
			"custom_data:string," +
			"date:date"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	requireLoadTimestampColumn(t, types)
	userTypes := withoutLoadTimestampTypes(types)
	require.Equal(t, 16, len(userTypes), "got: %v", types)
	assert.Equal(t, "VARCHAR", types["ad_format"])
	assert.Equal(t, "VARCHAR", types["ad_unit_id"])
	assert.Equal(t, "VARCHAR", types["device_type"])
	assert.Equal(t, "VARCHAR", types["user_id"])
	assert.Equal(t, "VARCHAR", types["session_id"])
	assert.Equal(t, "VARCHAR", types["network_name"])
	assert.Equal(t, "DOUBLE", types["revenue"])
	assert.Equal(t, "VARCHAR", types["custom_data"])
	assert.Equal(t, "DATE", types["date"])
	assert.Equal(t, 3, readDuckDBRowCount(t, duckDBPath, "main.input"))
}

func TestSnakeCase_NoDuplicateAppended(t *testing.T) {
	cases := []struct {
		name     string
		csv      string
		override string
	}{
		{"override snake matches source space", "Ad Format,Country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source camelCase", "adFormat,country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source hyphen", "ad-format,country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source dot", "ad.format,country\nINTER,US\n", "ad_format:string"},
		{"override source-form matches space source", "Ad Format,Country\nINTER,US\n", "Ad Format:string"},
		{"override snake already matches snake source", "ad_format,country\nINTER,US\n", "ad_format:string"},
		{"override matches multi-word with extra source col", "Ad Unit ID,Country\nabc,US\n", "ad_unit_id:bigint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			duckDBPath := runCSVtoDuckDB(t, tc.csv, func(cfg *config.IngestConfig) {
				cfg.Columns = tc.override + ",country:string"
			})
			types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
			requireLoadTimestampColumn(t, types)
			require.Equal(t, 2, len(withoutLoadTimestampTypes(types)), "got: %v", types)
		})
	}
}

func TestSnakeCase_TrulyMissingOverrideColumnIsStillAppended(t *testing.T) {
	csv := "Ad Format,Country\nINTER,US\nBANNER,GB\n"
	duckDBPath := runCSVtoDuckDB(t, csv, func(cfg *config.IngestConfig) {
		cfg.Columns = "ad_format:string,country:string,extra_col:bigint"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	requireLoadTimestampColumn(t, types)
	require.Equal(t, 3, len(withoutLoadTimestampTypes(types)), "got: %v", types)
	assert.Equal(t, "VARCHAR", types["ad_format"])
	assert.Equal(t, "VARCHAR", types["country"])
	assert.Equal(t, "BIGINT", types["extra_col"])
}

func TestDirectNaming_OverrideInSourceFormStillWorks(t *testing.T) {
	csv := "Ad Format,Country,Revenue\nINTER,US,1.5\nBANNER,GB,2.0\n"
	duckDBPath := runCSVtoDuckDB(t, csv, func(cfg *config.IngestConfig) {
		cfg.SchemaNaming = "direct"
		cfg.Columns = "Ad Format:string,Country:string,Revenue:double"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	requireLoadTimestampColumn(t, types)
	require.Equal(t, 3, len(withoutLoadTimestampTypes(types)))
	assert.Equal(t, "DOUBLE", types["revenue"], "got: %v", types)
}

func TestInvalidSchemaNaming_FailsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("Ad Format,Country\nINTER,US\n"), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
		SchemaNaming:        "bogus",
		Columns:             "ad_format:string",
	}
	if err := cfg.Validate(); err != nil {
		return
	}
	err := pipeline.New(cfg).Run(ctx)
	require.Error(t, err)
}

// namingSourceCase seeds a source table whose columns need renaming (camelCase,
// PascalCase or UPPERCASE); update then changes row 2 and inserts row 3.
type namingSourceCase struct {
	sourceURI   string
	sourceTable string
	primaryKey  string
	setup       func(t *testing.T)
	update      func(t *testing.T)
}

type namingSourceRow struct {
	name string
	city string
}

// runNamingSourceRoundTrip covers a replace run into a new table and a merge
// run into the existing one; the source must read with its own column names.
func runNamingSourceRoundTrip(t *testing.T, c namingSourceCase) {
	t.Helper()
	ctx := context.Background()
	c.setup(t)
	duckDBPath := filepath.Join(t.TempDir(), "out.duckdb")
	run := func(strategy config.IncrementalStrategy) {
		cfg := &config.IngestConfig{
			SourceURI:           c.sourceURI,
			SourceTable:         c.sourceTable,
			DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
			DestTable:           "main.orders",
			IncrementalStrategy: strategy,
			PrimaryKeys:         []string{c.primaryKey},
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, pipeline.New(cfg).Run(ctx))
	}

	run(config.StrategyReplace)
	requireNamingSourceRows(t, duckDBPath, map[int64]namingSourceRow{
		1: {"ann", "Berlin"},
		2: {"bob", "Paris"},
	})

	c.update(t)
	run(config.StrategyMerge)
	requireNamingSourceRows(t, duckDBPath, map[int64]namingSourceRow{
		1: {"ann", "Berlin"},
		2: {"bob-upd", "Paris"},
		3: {"cem", "Rome"},
	})
}

func requireNamingSourceRows(t *testing.T, duckDBPath string, want map[int64]namingSourceRow) {
	t.Helper()
	cols := make([]string, 0, 3)
	for name := range withoutLoadTimestampTypes(readDuckDBColumnTypes(t, duckDBPath, "main.orders")) {
		cols = append(cols, name)
	}
	sort.Strings(cols)
	require.Equal(t, []string{"customer_name", "order_id", "ship_city"}, cols)

	db := openDuckDBForTest(t, duckDBPath)
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT CAST(order_id AS BIGINT), CAST(customer_name AS VARCHAR), CAST(ship_city AS VARCHAR) FROM main.orders`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	got := map[int64]namingSourceRow{}
	for rows.Next() {
		var id int64
		var name, city sql.NullString
		require.NoError(t, rows.Scan(&id, &name, &city))
		require.True(t, name.Valid && city.Valid, "row %d arrived with NULL values", id)
		got[id] = namingSourceRow{name.String, city.String}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, want, got)
}

func execAll(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err, stmt)
	}
}

func TestNamingSource_DuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	srcPath := filepath.Join(t.TempDir(), "src.duckdb")
	// The adbc_generic driver rejects Exec here, so statements go through Query.
	withDB := func(t *testing.T, statements ...string) {
		db := openDuckDBForTest(t, srcPath)
		defer func() { _ = db.Close() }()
		for _, stmt := range statements {
			rows, err := db.Query(stmt)
			require.NoError(t, err, stmt)
			require.NoError(t, rows.Close())
		}
	}
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   fmt.Sprintf("duckdb:///%s", srcPath),
		sourceTable: "main.orders",
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			withDB(t,
				`CREATE TABLE main.orders ("orderId" BIGINT PRIMARY KEY, "CustomerName" VARCHAR, "SHIP_CITY" VARCHAR)`,
				`INSERT INTO main.orders VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')`)
		},
		update: func(t *testing.T) {
			withDB(t,
				`UPDATE main.orders SET "CustomerName" = 'bob-upd' WHERE "orderId" = 2`,
				`INSERT INTO main.orders VALUES (3, 'cem', 'Rome')`)
		},
	})
}

func TestNamingSource_SQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	srcPath := filepath.Join(t.TempDir(), "src.db")
	withDB := func(t *testing.T, statements ...string) {
		db, err := sql.Open("sqlite3", srcPath)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		execAll(t, db, statements...)
	}
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   fmt.Sprintf("sqlite:///%s", srcPath),
		sourceTable: "orders",
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			withDB(t,
				`CREATE TABLE orders ("orderId" INTEGER PRIMARY KEY, "CustomerName" TEXT, "SHIP_CITY" TEXT)`,
				`INSERT INTO orders VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')`)
		},
		update: func(t *testing.T) {
			withDB(t,
				`UPDATE orders SET "CustomerName" = 'bob-upd' WHERE "orderId" = 2`,
				`INSERT INTO orders VALUES (3, 'cem', 'Rome')`)
		},
	})
}

func TestNamingSource_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgSource.uri == "" {
		t.Skip("shared postgres source container not available")
	}
	db, err := sql.Open("pgx", pgSource.uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   pgSource.uri,
		sourceTable: "public.naming_orders",
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			execAll(t, db,
				`DROP TABLE IF EXISTS public.naming_orders`,
				`CREATE TABLE public.naming_orders ("orderId" BIGINT PRIMARY KEY, "CustomerName" TEXT, "SHIP_CITY" TEXT)`,
				`INSERT INTO public.naming_orders VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')`)
		},
		update: func(t *testing.T) {
			execAll(t, db,
				`UPDATE public.naming_orders SET "CustomerName" = 'bob-upd' WHERE "orderId" = 2`,
				`INSERT INTO public.naming_orders VALUES (3, 'cem', 'Rome')`)
		},
	})
}

func TestNamingSource_MySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("shared mysql container not available")
	}
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   mysqlDest.uri,
		sourceTable: "naming_orders",
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			execAll(t, db,
				`DROP TABLE IF EXISTS naming_orders`,
				`CREATE TABLE naming_orders (orderId BIGINT PRIMARY KEY, CustomerName VARCHAR(50), SHIP_CITY VARCHAR(50))`,
				`INSERT INTO naming_orders VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')`)
		},
		update: func(t *testing.T) {
			execAll(t, db,
				`UPDATE naming_orders SET CustomerName = 'bob-upd' WHERE orderId = 2`,
				`INSERT INTO naming_orders VALUES (3, 'cem', 'Rome')`)
		},
	})
}

func TestNamingSource_MSSQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("shared mssql container not available")
	}
	db, err := sql.Open("sqlserver", mssqlConnString(mssqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   mssqlDest.uri,
		sourceTable: "dbo.naming_orders",
		primaryKey:  "OrderId",
		setup: func(t *testing.T) {
			execAll(t, db,
				`DROP TABLE IF EXISTS dbo.naming_orders`,
				`CREATE TABLE dbo.naming_orders (OrderId BIGINT PRIMARY KEY, CustomerName NVARCHAR(50), SHIP_CITY NVARCHAR(50))`,
				`INSERT INTO dbo.naming_orders VALUES (1, N'ann', N'Berlin'), (2, N'bob', N'Paris')`)
		},
		update: func(t *testing.T) {
			execAll(t, db,
				`UPDATE dbo.naming_orders SET CustomerName = N'bob-upd' WHERE OrderId = 2`,
				`INSERT INTO dbo.naming_orders VALUES (3, N'cem', N'Rome')`)
		},
	})
}

func TestNamingSource_ClickHouse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if chDest.uri == "" {
		t.Skip("shared clickhouse container not available")
	}
	ctx := context.Background()
	conn, err := openClickHouseConn(chDest.uri)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	table := clickhouseDB + ".naming_orders"
	exec := func(t *testing.T, statements ...string) {
		for _, stmt := range statements {
			require.NoError(t, conn.Exec(ctx, stmt), stmt)
		}
	}
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   chDest.uri,
		sourceTable: table,
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			exec(t,
				"DROP TABLE IF EXISTS "+table,
				"CREATE TABLE "+table+" (orderId Int64, CustomerName String, SHIP_CITY String) ENGINE = MergeTree ORDER BY orderId",
				"INSERT INTO "+table+" VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')")
		},
		update: func(t *testing.T) {
			exec(t,
				"ALTER TABLE "+table+" UPDATE CustomerName = 'bob-upd' WHERE orderId = 2 SETTINGS mutations_sync = 2",
				"INSERT INTO "+table+" VALUES (3, 'cem', 'Rome')")
		},
	})
}

func TestNamingSource_Oracle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if oracleDest.uri == "" {
		t.Skip("shared oracle container not available")
	}
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	// Unquoted Oracle identifiers are stored UPPERCASE, the default shape of
	// every Oracle table.
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   oracleDest.uri,
		sourceTable: strings.ToUpper(oracleUser) + ".NAMING_ORDERS",
		primaryKey:  "ORDER_ID",
		setup: func(t *testing.T) {
			_, _ = db.Exec(`DROP TABLE NAMING_ORDERS PURGE`)
			execAll(t, db,
				`CREATE TABLE NAMING_ORDERS (ORDER_ID NUMBER(10) PRIMARY KEY, "customerName" VARCHAR2(50), SHIP_CITY VARCHAR2(50))`,
				`INSERT INTO NAMING_ORDERS VALUES (1, 'ann', 'Berlin')`,
				`INSERT INTO NAMING_ORDERS VALUES (2, 'bob', 'Paris')`)
		},
		update: func(t *testing.T) {
			execAll(t, db,
				`UPDATE NAMING_ORDERS SET "customerName" = 'bob-upd' WHERE ORDER_ID = 2`,
				`INSERT INTO NAMING_ORDERS VALUES (3, 'cem', 'Rome')`)
		},
	})
}

// TestNamingSource_BigQuery needs GONG_TEST_BIGQUERY_URI and
// GONG_TEST_BIGQUERY_PROJECT; it creates and drops a temporary dataset.
func TestNamingSource_BigQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	uri := os.Getenv("GONG_TEST_BIGQUERY_URI")
	project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
	if uri == "" || project == "" {
		t.Skip("set GONG_TEST_BIGQUERY_URI and GONG_TEST_BIGQUERY_PROJECT to run")
	}
	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, project)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	dataset := fmt.Sprintf("ingestr_naming_%d", time.Now().UnixNano())
	meta := &bigquery.DatasetMetadata{Location: os.Getenv("GONG_TEST_BIGQUERY_LOCATION")}
	require.NoError(t, client.Dataset(dataset).Create(ctx, meta))
	t.Cleanup(func() { _ = client.Dataset(dataset).DeleteWithContents(context.Background()) })

	query := func(t *testing.T, sqlText string) {
		q := client.Query(sqlText)
		q.Location = meta.Location
		job, err := q.Run(ctx)
		require.NoError(t, err, sqlText)
		status, err := job.Wait(ctx)
		require.NoError(t, err, sqlText)
		require.NoError(t, status.Err(), sqlText)
	}
	table := dataset + ".orders"
	runNamingSourceRoundTrip(t, namingSourceCase{
		sourceURI:   uri,
		sourceTable: table,
		primaryKey:  "orderId",
		setup: func(t *testing.T) {
			query(t, "CREATE TABLE "+table+" (orderId INT64, CustomerName STRING, SHIP_CITY STRING)")
			query(t, "INSERT INTO "+table+" VALUES (1, 'ann', 'Berlin'), (2, 'bob', 'Paris')")
		},
		update: func(t *testing.T) {
			query(t, "UPDATE "+table+" SET CustomerName = 'bob-upd' WHERE orderId = 2")
			query(t, "INSERT INTO "+table+" VALUES (3, 'cem', 'Rome')")
		},
	})
}
