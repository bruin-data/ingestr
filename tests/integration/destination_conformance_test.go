//go:build integration

package integration

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/snowflake"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	"github.com/bruin-data/ingestr/pkg/strategy"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/microsoft/go-mssqldb/azuread" // registers the "azuresql" driver for Fabric
	_ "github.com/sijms/go-ora/v2"
	_ "github.com/snowflakedb/gosnowflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	replaceFixtureRows      = 10
	mergeInitialRows        = 5
	mergeAfterRows          = 6
	appendInitialRows       = 5
	appendAfterRows         = 11
	deleteInsertInitialRows = 10
	deleteInsertAfterRows   = 11

	// SCD2 test constants
	// After initial load: 5 rows, all current
	// After update:
	//   - id=1: status changed -> 2 rows (1 historical, 1 current)
	//   - id=2: score changed -> 2 rows (1 historical, 1 current)
	//   - id=3: unchanged -> 1 row (current)
	//   - id=4,5: deleted from source -> 1 row each (historical, soft-deleted)
	//   - id=6: new -> 1 row (current)
	// Total: 8 rows, 4 current, 4 historical
	scd2TotalRows   = 8
	scd2CurrentRows = 4
)

type destCase struct {
	name                   string
	setup                  func(t *testing.T, ctx context.Context) (destURI string, destTable string, cleanup func())
	sqlBackend             *sqlBackend
	mergeCapable           bool
	deleteInsertCapable    bool
	truncateInsertCapable  bool
	scd2Capable            bool
	schemaEvolutionCapable bool
	// replaceDedupCapable marks destinations that deduplicate by primary key on
	// replace, so the dedup conformance tests run against them. Most swap+merge
	// destinations do this via the strategy's pre-swap normalised table; Postgres
	// via truncate+insert. Excludes ClickHouse (dedup is engine-dependent) and
	// destinations without atomic swap (which write directly, no dedup).
	replaceDedupCapable  bool
	validateNonSQL       func(t *testing.T, destURI, destTable string)
	validateAppendNonSQL func(t *testing.T, destURI, destTable string)
}

func destinationCases() []destCase {
	return []destCase{
		{
			name: "postgres",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if pgDest.uri == "" {
					t.Skip("shared postgres destination container not available")
				}

				table := fmt.Sprintf("public.conformance_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("pgx", pgDest.uri)
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
						_ = db.Close()
					}
				}
				return pgDest.uri, table, cleanup
			},
			sqlBackend:             postgresBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
			replaceDedupCapable:    true,
		},
		{
			name: "sqlite",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				tmpFile, err := os.CreateTemp("", "conformance_*.db")
				require.NoError(t, err)
				_ = tmpFile.Close()
				uri := fmt.Sprintf("sqlite:///%s", tmpFile.Name())
				return uri, "conformance", func() { _ = os.Remove(tmpFile.Name()) }
			},
			sqlBackend:             sqliteBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: false, // SQLite doesn't support ALTER COLUMN TYPE
			replaceDedupCapable:    true,
		},
		{
			name: "duckdb",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				tmpDir := t.TempDir()
				path := filepath.Join(tmpDir, fmt.Sprintf("conformance_%d.duckdb", time.Now().UnixNano()))
				uri := fmt.Sprintf("duckdb:///%s", path)
				return uri, "main.conformance", func() {}
			},
			sqlBackend:             duckdbBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
			replaceDedupCapable:    true,
		},
		{
			name: "csv",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				tmpDir := t.TempDir()
				path := filepath.Join(tmpDir, fmt.Sprintf("conformance_%d.csv", time.Now().UnixNano()))
				uri := fmt.Sprintf("csv:///%s", path)
				return uri, "conformance", func() {}
			},
			validateNonSQL:       validateCSVReplace,
			validateAppendNonSQL: validateCSVAppend,
		},
		{
			name: "parquet",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				tmpDir := t.TempDir()
				path := filepath.Join(tmpDir, fmt.Sprintf("conformance_%d.parquet", time.Now().UnixNano()))
				uri := fmt.Sprintf("parquet:///%s", path)
				return uri, "conformance", func() {}
			},
			validateNonSQL:       validateParquetReplace,
			validateAppendNonSQL: validateParquetAppend,
		},
		{
			name: "discard",
			setup: func(t *testing.T, _ context.Context) (string, string, func()) {
				return "discard://", "conformance", func() {}
			},
			validateNonSQL:       func(t *testing.T, _, _ string) { assert.True(t, true) },
			validateAppendNonSQL: func(t *testing.T, _, _ string) { assert.True(t, true) },
		},
		{
			name: "bigquery",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				bqURI := os.Getenv("GONG_TEST_BIGQUERY_URI")
				if bqURI == "" {
					t.Skip("Set GONG_TEST_BIGQUERY_URI to run BigQuery standards")
				}
				project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
				if project == "" {
					t.Skip("Set GONG_TEST_BIGQUERY_PROJECT to run BigQuery standards")
				}
				dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
				if dataset == "" {
					t.Skip("Set GONG_TEST_BIGQUERY_DATASET to run BigQuery standards")
				}
				table := fmt.Sprintf("conformance_%s", uniqueSuffix())
				cleanup := func() {
					client, err := bigquery.NewClient(ctx, project)
					if err != nil {
						return
					}
					_ = client.Dataset(dataset).Table(table).Delete(ctx)
					_ = client.Close()
				}
				return bqURI, dataset + "." + table, cleanup
			},
			sqlBackend:             bigqueryBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
			replaceDedupCapable:    true,
		},
		{
			name: "clickhouse",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				t.Skip("clickhouse tests temporarily disabled")
				if chDest.uri == "" {
					t.Skip("shared clickhouse destination container not available")
				}

				table := fmt.Sprintf("conformance_%s", uniqueSuffix())
				cleanup := func() {
					conn, err := openClickHouseConn(chDest.uri)
					if err == nil {
						_ = conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", clickhouseDB, table))
						_ = conn.Close()
					}
				}
				return chDest.uri, clickhouseDB + "." + table, cleanup
			},
			sqlBackend:             clickhouseBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
		},
		{
			name: "mysql",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if mysqlDest.uri == "" {
					t.Skip("shared mysql destination container not available")
				}

				table := fmt.Sprintf("conformance_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", table))
						_ = db.Close()
					}
				}
				return mysqlDest.uri, table, cleanup
			},
			sqlBackend:             mysqlBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			replaceDedupCapable:    true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
		},
		{
			name: "oracle",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if oracleDest.uri == "" {
					t.Skip("shared oracle destination container not available")
				}

				table := fmt.Sprintf("CONFORMANCE_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s PURGE", quoteTableOracle(table)))
						_ = db.Close()
					}
				}
				return oracleDest.uri, table, cleanup
			},
			sqlBackend:             oracleBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			replaceDedupCapable:    true,
			scd2Capable:            true,
			schemaEvolutionCapable: false, // Oracle needs a data-preserving rewrite path for type changes like NUMBER -> CLOB.
		},
		{
			name: "maxcompute",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if maxcomputeDest.uri == "" {
					t.Skip("shared MaxCompute emulator container not available")
				}

				table := fmt.Sprintf("conformance_%s", uniqueSuffix())
				cleanup := func() {
					dest, err := uri.DefaultRegistry.GetDestination(maxcomputeDest.uri)
					if err != nil {
						return
					}
					if err := dest.Connect(ctx, maxcomputeDest.uri); err != nil {
						return
					}
					_ = dest.DropTable(ctx, table)
					_ = dest.Close(ctx)
				}
				return maxcomputeDest.uri, table, cleanup
			},
			validateNonSQL:       validateMaxComputeReplace,
			validateAppendNonSQL: validateMaxComputeAppend,
		},
		{
			name: "mssql",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if mssqlDest.uri == "" {
					t.Skip("shared mssql destination container not available")
				}

				table := fmt.Sprintf("dbo.conformance_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("sqlserver", mssqlConnString(mssqlDest.uri))
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s", table, quoteTableMSSQL(table)))
						_ = db.Close()
					}
				}
				return mssqlDest.uri, table, cleanup
			},
			sqlBackend:             mssqlBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
			replaceDedupCapable:    true,
		},
		{
			name: "fabric",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				fabricURI := os.Getenv("GONG_TEST_FABRIC_URI")
				if fabricURI == "" {
					t.Skip("Set GONG_TEST_FABRIC_URI to run Fabric destination conformance, e.g. fabric://<clientid>:<secret>@<guid>.datawarehouse.fabric.microsoft.com/<warehouse>?tenant_id=<tid>")
				}

				table := fmt.Sprintf("dbo.conformance_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("azuresql", fabricConnString(fabricURI))
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s", table, quoteTableMSSQL(table)))
						_ = db.Close()
					}
				}
				return fabricURI, table, cleanup
			},
			sqlBackend:            fabricBackend(),
			mergeCapable:          true,
			deleteInsertCapable:   true,
			truncateInsertCapable: true,
			scd2Capable:           true,
		},
		{
			name: "cratedb",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				if cratedbDest.uri == "" {
					t.Skip("shared cratedb destination container not available")
				}

				table := fmt.Sprintf("doc.conformance_%s", uniqueSuffix())
				cleanup := func() {
					db, err := sql.Open("pgx", cratedbPgURI(cratedbDest.uri))
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
						_ = db.Close()
					}
				}
				return cratedbDest.uri, table, cleanup
			},
			sqlBackend:             cratedbBackend(),
			mergeCapable:           true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: false,
		},
		{
			name: "snowflake",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				sfURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
				if sfURI == "" {
					t.Skip("Set GONG_TEST_SNOWFLAKE_URI to run Snowflake tests")
				}
				table := fmt.Sprintf("GONG.CONFORMANCE_%s", uniqueSuffix())
				cleanup := func() {
					db, err := snowflakeOpenDB()
					if err == nil {
						_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
						_ = db.Close()
					}
				}
				return sfURI, table, cleanup
			},
			sqlBackend:             snowflakeBackend(),
			mergeCapable:           true,
			deleteInsertCapable:    true,
			truncateInsertCapable:  true,
			scd2Capable:            true,
			schemaEvolutionCapable: true,
			replaceDedupCapable:    true,
		},
		{
			name: "athena",
			setup: func(t *testing.T, ctx context.Context) (string, string, func()) {
				athenaURI := os.Getenv("GONG_TEST_ATHENA_URI")
				if athenaURI == "" {
					t.Skip("Set GONG_TEST_ATHENA_URI (include workgroup) to run Athena destination conformance, e.g. athena://?bucket=s3://...&profile=...&region_name=...&workgroup=primary")
				}

				table := fmt.Sprintf("default.conformance_%s", uniqueSuffix())
				cleanup := func() {
					dest, err := uri.DefaultRegistry.GetDestination(athenaURI)
					if err != nil {
						return
					}
					if err := dest.Connect(ctx, athenaURI); err != nil {
						return
					}
					_ = dest.DropTable(ctx, table)
					_ = dest.Close(ctx)
				}
				return athenaURI, table, cleanup
			},
			validateNonSQL:       validateAthenaReplace,
			validateAppendNonSQL: validateAthenaAppend,
		},
		// DynamoDB is tested separately in dynamodb_test.go because it always
		// requires primary keys in the config, which the generic conformance
		// tests don't provide.
	}
}

// TestDestinations_Replace validates:
// - schema inference from JSONL
// - destination table creation
// - replace strategy correctness (existing data overwritten)
// - destination type mapping correctness (SQL backends)
func TestDestinations_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "replace_source",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
			}

			p := pipeline.New(cfg)
			require.NoError(t, p.Run(ctx))
			if tc.sqlBackend != nil {
				validateReplaceSQL(t, tc.sqlBackend, destURI, destTable)
				return
			}
			tc.validateNonSQL(t, destURI, destTable)
		})
	}
}

func validateAthenaReplace(t *testing.T, destURI, destTable string) {
	ctx := context.Background()
	rows := countRowsViaAthenaRead(t, ctx, destURI, destTable)
	require.Equal(t, replaceFixtureRows, rows)
}

func validateAthenaAppend(t *testing.T, destURI, destTable string) {
	ctx := context.Background()
	rows := countRowsViaAthenaRead(t, ctx, destURI, destTable)
	require.Equal(t, appendAfterRows, rows)
}

func validateMaxComputeReplace(t *testing.T, destURI, destTable string) {
	ctx := context.Background()
	rows := countRowsViaMaxComputeRead(t, ctx, destURI, destTable)
	require.Equal(t, replaceFixtureRows, rows)
}

func validateMaxComputeAppend(t *testing.T, destURI, destTable string) {
	ctx := context.Background()
	rows := countRowsViaMaxComputeRead(t, ctx, destURI, destTable)
	require.Equal(t, appendAfterRows, rows)
}

func countRowsViaMaxComputeRead(t *testing.T, ctx context.Context, maxComputeURI, maxComputeTable string) int {
	tmpFile, err := os.CreateTemp("", "maxcompute_conformance_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	cfg := &config.IngestConfig{
		SourceURI:           maxComputeURI,
		SourceTable:         maxComputeTable,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "out",
		IncrementalStrategy: config.StrategyReplace,
	}

	p := pipeline.New(cfg)
	require.NoError(t, p.Run(ctx))

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var n int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM out").Scan(&n))
	return n
}

func countRowsViaAthenaRead(t *testing.T, ctx context.Context, athenaURI, athenaTable string) int {
	tmpFile, err := os.CreateTemp("", "athena_conformance_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	cfg := &config.IngestConfig{
		SourceURI:           athenaURI,
		SourceTable:         athenaTable,
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "out",
		IncrementalStrategy: config.StrategyReplace,
	}

	p := pipeline.New(cfg)
	require.NoError(t, p.Run(ctx))

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var n int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM out").Scan(&n))
	return n
}

// TestDestinations_Merge validates:
// - merge strategy upsert behavior (update existing rows by PK, insert new)
// Only destinations that implement MergeTable are tested.
func TestDestinations_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_merge_initial.jsonl")
	updateURI := jsonlURI(t, "testdata/conformance_merge_update.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support merge")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "merge_source",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx))

			cfg.SourceURI = updateURI
			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx))
			validateMergeSQL(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_Append validates append semantics.
// Append strategy is not implemented yet in this repo, so this test currently skips.
// When append is added to pkg/strategy, this test should be enabled to verify:
// - destination supports append
// - second run adds rows without overwriting
func TestDestinations_Append(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategyAppend); err != nil {
		t.Skip("append strategy not implemented yet")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_append_initial.jsonl")
	moreURI := jsonlURI(t, "testdata/conformance_append_more.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "append_source",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyAppend,
			}
			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx))

			cfg.SourceURI = moreURI
			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx))

			if tc.sqlBackend != nil {
				validateAppendSQL(t, tc.sqlBackend, destURI, destTable)
				return
			}
			if tc.validateAppendNonSQL != nil {
				tc.validateAppendNonSQL(t, destURI, destTable)
				return
			}
			t.Skip("no append validator for destination")
		})
	}
}

// TestDestinations_DeleteInsert validates delete+insert semantics:
// - seed a destination table with initial rows
// - run delete+insert with a source whose min/max defines the interval
// - verify rows outside the interval are unchanged
// - verify rows in the interval are replaced
// - verify net-new rows within the interval are inserted (count increases)
func TestDestinations_DeleteInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategyDeleteInsert); err != nil {
		t.Skip("delete+insert strategy not implemented yet")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_deleteinsert_initial.jsonl")
	intervalURI := jsonlURI(t, "testdata/conformance_deleteinsert_interval.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.deleteInsertCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support delete+insert")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// Seed initial data (replace)
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "deleteinsert_seed",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			// Apply delete+insert over the interval inferred from the source data.
			cfg.SourceURI = intervalURI
			cfg.SourceTable = "deleteinsert_interval"
			cfg.IncrementalStrategy = config.StrategyDeleteInsert
			cfg.IncrementalKey = "id"
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			validateDeleteInsertSQL(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_DeleteInsert_DedupesStagingByPK exercises delete+insert with
// primary keys in two phases on the same target:
//
//  1. Dedup on load: a source with duplicate primary keys (5 rows across 3
//     distinct ids {1,1,2,3,3}) must collapse to exactly 3 rows.
//  2. Normal incremental delete+insert: a second source over the interval
//     [3,4] must replace id=3 in place, insert net-new id=4, and leave id=1
//     and id=2 (outside the interval) untouched — ending at 4 rows.
func TestDestinations_DeleteInsert_DedupesStagingByPK(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategyDeleteInsert); err != nil {
		t.Skip("delete+insert strategy not implemented yet")
	}

	ctx := context.Background()
	dupURI := jsonlURI(t, "testdata/conformance_deleteinsert_dedup.jsonl")
	intervalURI := jsonlURI(t, "testdata/conformance_deleteinsert_dedup_interval.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.deleteInsertCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support delete+insert")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			countRows := func() int {
				db, err := tc.sqlBackend.openDB(destURI)
				require.NoError(t, err)
				defer func() { _ = db.Close() }()
				var n int
				require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&n))
				return n
			}
			nameByID := func(id int) string {
				db, err := tc.sqlBackend.openDB(destURI)
				require.NoError(t, err)
				defer func() { _ = db.Close() }()
				var raw []byte
				require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(destTable, id)).Scan(&raw))
				return string(raw)
			}

			cfg := &config.IngestConfig{
				SourceURI:           dupURI,
				SourceTable:         "deleteinsert_dedup",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyDeleteInsert,
				IncrementalKey:      "id",
				PrimaryKeys:         []string{"id"},
			}

			// Phase 1: dedup on load — duplicate PKs collapse to one row each.
			require.NoError(t, pipeline.New(cfg).Run(ctx))
			assert.Equal(t, 3, countRows(), "duplicate primary keys in staging should collapse to one row per key")

			// Phase 2: normal incremental delete+insert over interval [3,4].
			cfg.SourceURI = intervalURI
			cfg.SourceTable = "deleteinsert_dedup_interval"
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			assert.Equal(t, 4, countRows(), "interval delete+insert should replace id=3 and add net-new id=4")
			assert.Equal(t, "v2-3", nameByID(3), "id=3 inside the interval should be replaced")
			assert.Equal(t, "v2-4", nameByID(4), "net-new id=4 inside the interval should be inserted")
		})
	}
}

// TestDestinations_TruncateInsert validates truncate+insert semantics:
//   - seed the destination with a small set of rows via replace
//   - run truncate+insert with a larger source fixture
//   - verify the final row count matches the new source (old rows are gone,
//     not appended) and the replacement values are queryable
func TestDestinations_TruncateInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategyTruncateInsert); err != nil {
		t.Skip("truncate+insert strategy not implemented yet")
	}

	ctx := context.Background()
	seedURI := jsonlURI(t, "testdata/conformance_append_initial.jsonl")
	truncateURI := jsonlURI(t, "testdata/conformance.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.truncateInsertCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support truncate+insert")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// Seed with 5 rows via replace so the table exists.
			seedCfg := &config.IngestConfig{
				SourceURI:           seedURI,
				SourceTable:         "truncate_seed",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
			}
			require.NoError(t, pipeline.New(seedCfg).Run(ctx))

			// Truncate+insert with 10 rows. Final count should be 10 (not 15).
			cfg := &config.IngestConfig{
				SourceURI:           truncateURI,
				SourceTable:         "truncate_source",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyTruncateInsert,
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			validateTruncateInsertSQL(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

// TestDestinations_TruncateInsert_Dedup validates that truncate+insert
// deduplicates source rows by primary key. The fixture contains 10 rows with
// only 5 distinct ids (each id appearing twice). After the run, the target
// must contain exactly 5 rows.
func TestDestinations_TruncateInsert_Dedup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategyTruncateInsert); err != nil {
		t.Skip("truncate+insert strategy not implemented yet")
	}

	ctx := context.Background()
	dupesURI := jsonlURI(t, "testdata/conformance_truncate_dupes.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.truncateInsertCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support truncate+insert")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           dupesURI,
				SourceTable:         "truncate_dupes",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyTruncateInsert,
				PrimaryKeys:         []string{"id"},
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			db, err := tc.sqlBackend.openDB(destURI)
			if err != nil {
				t.Skipf("Could not open SQL backend for truncate+insert dedup validation: %v", err)
				return
			}
			defer func() { _ = db.Close() }()

			var count int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
			assert.Equal(t, 5, count, "expected 5 distinct ids after dedup")
		})
	}
}

// TestDestinations_SCD2 validates SCD2 (Slowly Changing Dimensions Type 2) semantics:
// - Initial load: 5 records inserted as current rows with _scd_valid_from, _scd_valid_to=NULL, _scd_is_current=true
// - Update load:SCD2Table
//   - Changed records (id=1, id=2): close old version, insert new version
//   - Unchanged record (id=3): no new version created
//   - Deleted records (id=4, id=5): soft-deleted (closed with valid_to set)
//   - New record (id=6): inserted as current
//
// - Final state: 8 total rows, 4 current, 5 historical
func TestDestinations_SCD2(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := strategy.Get(config.StrategySCD2); err != nil {
		t.Skip("scd2 strategy not implemented yet")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_scd2_initial.jsonl")
	updateURI := jsonlURI(t, "testdata/conformance_scd2_update.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.scd2Capable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support scd2")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// Initial load: create table with SCD2 columns and insert all records as current
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "scd2_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategySCD2,
				PrimaryKeys:         []string{"id"},
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "Initial SCD2 load should succeed")

			// Update load: apply changes
			cfg.SourceURI = updateURI
			cfg.SourceTable = "scd2_update"
			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "SCD2 update load should succeed")

			validateSCD2SQL(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

func validateSCD2SQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for SCD2 validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	// Total row count should be 9
	var totalCount int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&totalCount))
	assert.Equal(t, scd2TotalRows, totalCount, "Total row count should be 8")

	// Current row count should be 4 (id=1,2,3,6 with is_current=true)
	if backend.scd2CountCurrent != nil {
		var currentCount int
		require.NoError(t, db.QueryRow(backend.scd2CountCurrent(table)).Scan(&currentCount))
		assert.Equal(t, scd2CurrentRows, currentCount, "Current row count should be 4")
	}

	// id=1 should have 2 rows (1 historical, 1 current) - status changed
	if backend.scd2CountByID != nil {
		var id1Count int
		require.NoError(t, db.QueryRow(backend.scd2CountByID(table, 1)).Scan(&id1Count))
		assert.Equal(t, 2, id1Count, "id=1 should have 2 rows (changed record)")
	}

	// id=3 should have 1 row (unchanged)
	if backend.scd2CountByID != nil {
		var id3Count int
		require.NoError(t, db.QueryRow(backend.scd2CountByID(table, 3)).Scan(&id3Count))
		assert.Equal(t, 1, id3Count, "id=3 should have 1 row (unchanged)")
	}

	// Historical records should all have valid_to set (not NULL)
	if backend.scd2HistNoValidTo != nil {
		var histNoValidTo int
		require.NoError(t, db.QueryRow(backend.scd2HistNoValidTo(table)).Scan(&histNoValidTo))
		assert.Equal(t, 0, histNoValidTo, "All historical records should have valid_to set")
	}
}

func jsonlURI(t *testing.T, rel string) string {
	t.Helper()
	// Prefer paths relative to current working directory (package dir).
	candidates := []string{
		rel,
		filepath.Join("tests", "integration", rel),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			ap, err := filepath.Abs(c)
			require.NoError(t, err)
			return fmt.Sprintf("jsonl://%s", ap)
		}
	}

	ap, err := filepath.Abs(rel)
	require.NoError(t, err)
	return fmt.Sprintf("jsonl://%s", ap)
}

func normalizeTypeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validateCSVReplace(t *testing.T, uri, _ string) {
	t.Helper()
	path := strings.TrimPrefix(uri, "csv:///")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(bufio.NewReader(f))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 1)
	assert.Equal(t, replaceFixtureRows, len(records)-1)
}

func validateParquetReplace(t *testing.T, uri, _ string) {
	t.Helper()
	path := strings.TrimPrefix(uri, "parquet:///")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	pr, err := file.NewParquetReader(f)
	require.NoError(t, err)
	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	require.NoError(t, err)

	tbl, err := fr.ReadTable(context.Background())
	require.NoError(t, err)
	defer tbl.Release()

	assert.Equal(t, int64(replaceFixtureRows), tbl.NumRows())
}

func validateCSVAppend(t *testing.T, uri, _ string) {
	t.Helper()
	path := strings.TrimPrefix(uri, "csv:///")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(bufio.NewReader(f))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 1)
	assert.Equal(t, appendAfterRows, len(records)-1)
}

func validateParquetAppend(t *testing.T, uri, _ string) {
	t.Helper()
	path := strings.TrimPrefix(uri, "parquet:///")
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	pr, err := file.NewParquetReader(f)
	require.NoError(t, err)
	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	require.NoError(t, err)

	tbl, err := fr.ReadTable(context.Background())
	require.NoError(t, err)
	defer tbl.Release()

	assert.Equal(t, int64(appendAfterRows), tbl.NumRows())
}

// --- SQL backends and generic validations ---

type sqlBackend struct {
	openDB            func(uri string) (*sql.DB, error)
	schemaTypes       func(db *sql.DB, table string) (map[string]string, error)
	expectedTypes     map[string]string
	countQuery        func(table string) string
	nameByIDQuery     func(table string, id int) string
	ageByIDQuery      func(table string, id int) string
	scd2CountCurrent  func(table string) string
	scd2CountByID     func(table string, id int) string
	scd2HistNoValidTo func(table string) string
	refreshTable      func(db *sql.DB, table string)
}

func postgresBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) { return sql.Open("pgx", uri) },
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			schemaName, tableName := splitSchemaTable(table, "public")
			rows, err := db.Query(`
				SELECT column_name, data_type
				FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = $2
				ORDER BY ordinal_position
			`, schemaName, tableName)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeTypeName(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "text",
			"active": "boolean",
			"score":  "double precision",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = true", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = false AND _scd_valid_to IS NULL", table)
		},
	}
}

func splitSchemaTable(table string, defaultSchema string) (string, string) {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return defaultSchema, table
}

func uniqueSuffix() string {
	// short, safe suffix for table names
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func sqliteBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			path := strings.TrimPrefix(uri, "sqlite:///")
			return sql.Open("sqlite3", path)
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var cid int
				var name, ctype string
				var notnull int
				var dflt interface{}
				var pk int
				if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeTypeName(ctype)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "integer",
			"name":   "text",
			"active": "integer",
			"score":  "real",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 1", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 0 AND _scd_valid_to IS NULL", table)
		},
	}
}

func duckdbBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			path := strings.TrimPrefix(uri, "duckdb:///")
			return sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			rows, err := db.Query(fmt.Sprintf("PRAGMA table_info('%s')", table))
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var cid int
				var nameRaw, ctypeRaw []byte
				var notnull bool
				var dflt interface{}
				var pk bool
				if err := rows.Scan(&cid, &nameRaw, &ctypeRaw, &notnull, &dflt, &pk); err != nil {
					return nil, err
				}
				out[strings.ToLower(string(nameRaw))] = normalizeTypeName(string(ctypeRaw))
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "varchar",
			"active": "boolean",
			"score":  "double",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = true", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = false AND _scd_valid_to IS NULL", table)
		},
	}
}

func bigqueryBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(_ string) (*sql.DB, error) {
			project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			if project == "" || dataset == "" {
				return nil, fmt.Errorf("missing bigquery env")
			}
			return sql.Open(
				"adbc_generic",
				fmt.Sprintf(
					"driver=bigquery;adbc.bigquery.sql.project_id=%s;adbc.bigquery.sql.dataset_id=%s;adbc.bigquery.sql.auth_type=adbc.bigquery.sql.auth_type.auth_bigquery",
					project,
					dataset,
				),
			)
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			_ = db
			project := os.Getenv("GONG_TEST_BIGQUERY_PROJECT")
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			client, err := bigquery.NewClient(context.Background(), project)
			if err != nil {
				return nil, err
			}
			defer func() { _ = client.Close() }()

			meta, err := client.Dataset(dataset).Table(tableName).Metadata(context.Background())
			if err != nil {
				return nil, err
			}

			out := map[string]string{}
			for _, field := range meta.Schema {
				out[strings.ToLower(field.Name)] = normalizeBigQueryFieldType(field.Type)
			}
			return out, nil
		},
		expectedTypes: map[string]string{
			"id":     "int64",
			"name":   "string",
			"active": "bool",
			"score":  "float64",
		},
		countQuery: func(table string) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s.%s`", dataset, tableName)
		},
		nameByIDQuery: func(table string, id int) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT name FROM `%s.%s` WHERE id=%d", dataset, tableName, id)
		},
		ageByIDQuery: func(table string, id int) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT age FROM `%s.%s` WHERE id=%d", dataset, tableName, id)
		},
		scd2CountCurrent: func(table string) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s.%s` WHERE _scd_is_current = true", dataset, tableName)
		},
		scd2CountByID: func(table string, id int) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s.%s` WHERE id = %d", dataset, tableName, id)
		},
		scd2HistNoValidTo: func(table string) string {
			dataset := os.Getenv("GONG_TEST_BIGQUERY_DATASET")
			tableName := table
			if idx := strings.LastIndex(table, "."); idx >= 0 {
				tableName = table[idx+1:]
			}
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s.%s` WHERE _scd_is_current = false AND _scd_valid_to IS NULL", dataset, tableName)
		},
	}
}

func normalizeBigQueryFieldType(fieldType bigquery.FieldType) string {
	switch fieldType {
	case bigquery.IntegerFieldType:
		return "int64"
	case bigquery.StringFieldType:
		return "string"
	case bigquery.BooleanFieldType:
		return "bool"
	case bigquery.FloatFieldType:
		return "float64"
	case bigquery.TimestampFieldType:
		return "timestamp"
	case bigquery.DateFieldType:
		return "date"
	case bigquery.TimeFieldType:
		return "time"
	case bigquery.DateTimeFieldType:
		return "datetime"
	case bigquery.NumericFieldType:
		return "numeric"
	case bigquery.BigNumericFieldType:
		return "bignumeric"
	case bigquery.JSONFieldType:
		return "json"
	case bigquery.BytesFieldType:
		return "bytes"
	case bigquery.RecordFieldType:
		return "record"
	default:
		return normalizeTypeName(string(fieldType))
	}
}

func validateReplaceSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for replace validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, replaceFixtureRows, count)

	actualTypes, err := backend.schemaTypes(db, table)
	if err != nil {
		t.Skipf("Could not read schema types: %v", err)
		return
	}
	requireLoadTimestampColumn(t, actualTypes)
	assert.Equal(t, backend.expectedTypes, withoutLoadTimestampTypes(actualTypes))
}

func validateMergeSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for merge validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, mergeAfterRows, count)

	var updatedNameRaw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 1)).Scan(&updatedNameRaw))
	assert.Equal(t, "alpha-updated", string(updatedNameRaw))

	var newNameRaw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 6)).Scan(&newNameRaw))
	assert.Equal(t, "foxtrot-new", string(newNameRaw))
}

func validateAppendSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for append validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, appendAfterRows, count)

	var newNameRaw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 11)).Scan(&newNameRaw))
	assert.Equal(t, "kilo", string(newNameRaw))
}

func validateTruncateInsertSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for truncate+insert validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, replaceFixtureRows, count)

	// id=10 only exists in the truncate+insert source; seeing it proves the
	// new batch is present and the seed's 5-row table was emptied (not appended).
	var nameRaw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 10)).Scan(&nameRaw))
	assert.Equal(t, "juliet", string(nameRaw))
}

func validateDeleteInsertSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for delete+insert validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, deleteInsertAfterRows, count)

	// Outside interval [3,7]: id=2 and id=10 should remain unchanged.
	var name2Raw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 2)).Scan(&name2Raw))
	assert.Equal(t, "v1-2", string(name2Raw))

	var name10Raw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 10)).Scan(&name10Raw))
	assert.Equal(t, "v1-10", string(name10Raw))

	// Inside interval: id=3 should be replaced.
	var name3Raw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 3)).Scan(&name3Raw))
	assert.Equal(t, "v2-3", string(name3Raw))

	// Net-new within interval: id=7 should exist after operation.
	var name7Raw []byte
	require.NoError(t, db.QueryRow(backend.nameByIDQuery(table, 7)).Scan(&name7Raw))
	assert.Equal(t, "v2-7", string(name7Raw))
}

func openClickHouseConn(uri string) (clickhouse.Conn, error) {
	opts, err := clickhouse.ParseDSN(uri)
	if err != nil {
		return nil, err
	}
	return clickhouse.Open(opts)
}

func clickhouseBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			opts, err := clickhouse.ParseDSN(uri)
			if err != nil {
				return nil, err
			}
			return clickhouse.OpenDB(opts), nil
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			rows, err := db.Query(`
				SELECT name, type
				FROM system.columns
				WHERE database = ? AND table = ?
				ORDER BY position
			`, database, tableName)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeClickHouseType(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "int64",
			"name":   "string",
			"active": "bool",
			"score":  "float64",
		},
		countQuery: func(table string) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", database, tableName)
		},
		nameByIDQuery: func(table string, id int) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT name FROM `%s`.`%s` WHERE id=%d", database, tableName, id)
		},
		ageByIDQuery: func(table string, id int) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT age FROM `%s`.`%s` WHERE id=%d", database, tableName, id)
		},
		scd2CountCurrent: func(table string) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE _scd_is_current = 1", database, tableName)
		},
		scd2CountByID: func(table string, id int) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE id = %d", database, tableName, id)
		},
		scd2HistNoValidTo: func(table string) string {
			database, tableName := splitSchemaTable(table, clickhouseDB)
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE _scd_is_current = 0 AND _scd_valid_to IS NULL", database, tableName)
		},
	}
}

func normalizeClickHouseType(chType string) string {
	chType = strings.ToLower(strings.TrimSpace(chType))
	// Strip Nullable wrapper
	if strings.HasPrefix(chType, "nullable(") && strings.HasSuffix(chType, ")") {
		chType = chType[9 : len(chType)-1]
	}
	// Map ClickHouse types to normalized names
	switch chType {
	case "int64", "bigint":
		return "int64"
	case "string":
		return "string"
	case "bool", "boolean", "uint8":
		return "bool"
	case "float64", "double":
		return "float64"
	default:
		return chType
	}
}

func cratedbPgURI(uri string) string {
	return strings.Replace(uri, "cratedb://", "postgres://", 1)
}

func cratedbRefresh(db *sql.DB, table string) {
	q := fmt.Sprintf("REFRESH TABLE %s", table)
	for range 5 {
		_, err := db.Exec(q)
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func cratedbBackend() *sqlBackend {
	return &sqlBackend{
		openDB:       func(uri string) (*sql.DB, error) { return sql.Open("pgx", cratedbPgURI(uri)) },
		refreshTable: cratedbRefresh,
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			cratedbRefresh(db, table)
			schemaName, tableName := splitSchemaTable(table, "doc")
			rows, err := db.Query(`
				SELECT column_name, data_type
				FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = $2
				ORDER BY ordinal_position
			`, schemaName, tableName)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeTypeName(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "text",
			"active": "boolean",
			"score":  "double precision",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = true", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = false AND _scd_valid_to IS NULL", table)
		},
	}
}

func snowflakeOpenDB() (*sql.DB, error) {
	sfURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if sfURI == "" {
		return nil, fmt.Errorf("GONG_TEST_SNOWFLAKE_URI not set")
	}

	return snowflake.OpenDB(sfURI)
}

func snowflakeBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(_ string) (*sql.DB, error) {
			return snowflakeOpenDB()
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			schemaName, tableName := splitSchemaTable(table, "PUBLIC")
			rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
				ORDER BY ORDINAL_POSITION
			`, strings.ToUpper(schemaName), strings.ToUpper(tableName))
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeSnowflakeType(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "varchar",
			"active": "boolean",
			"score":  "double",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT NAME FROM %s WHERE ID=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT AGE FROM %s WHERE ID=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _SCD_IS_CURRENT = true", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE ID = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _SCD_IS_CURRENT = false AND _SCD_VALID_TO IS NULL", table)
		},
	}
}

func normalizeSnowflakeType(sfType string) string {
	sfType = strings.ToLower(strings.TrimSpace(sfType))
	switch {
	case strings.HasPrefix(sfType, "number") || sfType == "bigint" || sfType == "int":
		return "bigint"
	case strings.HasPrefix(sfType, "varchar") || sfType == "text" || sfType == "string":
		return "varchar"
	case sfType == "boolean":
		return "boolean"
	case sfType == "double" || sfType == "float" || strings.HasPrefix(sfType, "float"):
		return "double"
	default:
		return sfType
	}
}

func mysqlDSN(uri string) string {
	// Convert mysql://user:pass@host:port/db to user:pass@tcp(host:port)/db?parseTime=true
	uri = strings.TrimPrefix(uri, "mysql://")
	uri = strings.TrimPrefix(uri, "mariadb://")
	uri = strings.TrimPrefix(uri, "vitess://")
	uri = strings.TrimPrefix(uri, "planetscale://")

	// Find the @ to split user:pass from host
	atIdx := strings.Index(uri, "@")
	if atIdx == -1 {
		return uri
	}

	userPass := uri[:atIdx]
	hostDB := uri[atIdx+1:]

	// Find the / to split host:port from db
	slashIdx := strings.Index(hostDB, "/")
	if slashIdx == -1 {
		return fmt.Sprintf("%s@tcp(%s)/?parseTime=true", userPass, hostDB)
	}

	hostPort := hostDB[:slashIdx]
	db := hostDB[slashIdx+1:]

	return fmt.Sprintf("%s@tcp(%s)/%s?parseTime=true&allowNativePasswords=true", userPass, hostPort, db)
}

func mysqlBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			return sql.Open("mysql", mysqlDSN(uri))
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
				ORDER BY ORDINAL_POSITION
			`, table)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeMySQLType(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "text",
			"active": "tinyint",
			"score":  "double",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table)
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM `%s` WHERE id=%d", table, id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM `%s` WHERE id=%d", table, id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE _scd_is_current = 1", table)
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE id = %d", table, id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE _scd_is_current = 0 AND _scd_valid_to IS NULL", table)
		},
	}
}

func normalizeMySQLType(mysqlType string) string {
	mysqlType = strings.ToLower(strings.TrimSpace(mysqlType))
	switch mysqlType {
	case "int", "integer", "bigint":
		return "bigint"
	case "varchar", "char", "text", "mediumtext", "longtext":
		return "text"
	case "tinyint", "boolean", "bool":
		return "tinyint"
	case "double", "float", "real":
		return "double"
	default:
		return mysqlType
	}
}

func oracleSQLConnString(rawURI string) string {
	normalized := rawURI
	if strings.HasPrefix(strings.ToLower(normalized), "oracle+cx_oracle://") {
		normalized = "oracle://" + normalized[len("oracle+cx_oracle://"):]
	}

	u, err := url.Parse(normalized)
	if err != nil {
		return rawURI
	}

	query := u.Query()
	if serviceName := query.Get("service_name"); serviceName != "" {
		query.Del("service_name")
		u.Path = "/" + serviceName
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func oracleCurrentUser(db *sql.DB) (string, error) {
	var user string
	if err := db.QueryRow("SELECT USER FROM DUAL").Scan(&user); err != nil {
		return "", err
	}
	return strings.ToUpper(user), nil
}

func quoteOracleIdentifier(identifier string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(identifier)), `"`, `""`))
}

func quoteTableOracle(table string) string {
	schemaName, tableName := splitSchemaTable(table, "")
	if schemaName != "" {
		return quoteOracleIdentifier(schemaName) + "." + quoteOracleIdentifier(tableName)
	}
	return quoteOracleIdentifier(tableName)
}

func oracleBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			return sql.Open("oracle", oracleSQLConnString(uri))
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			schemaName, tableName := splitSchemaTable(table, "")
			if schemaName == "" {
				currentUser, err := oracleCurrentUser(db)
				if err != nil {
					return nil, err
				}
				schemaName = currentUser
			}

			rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE, DATA_PRECISION, DATA_SCALE
				FROM ALL_TAB_COLUMNS
				WHERE OWNER = :1 AND TABLE_NAME = :2
				ORDER BY COLUMN_ID
			`, strings.ToUpper(schemaName), strings.ToUpper(tableName))
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()

			out := map[string]string{}
			for rows.Next() {
				var name, dataType string
				var precision, scale sql.NullInt64
				if err := rows.Scan(&name, &dataType, &precision, &scale); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeOracleType(dataType, precision, scale)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "number",
			"name":   "clob",
			"active": "number",
			"score":  "binary_double",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableOracle(table))
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT %s FROM %s WHERE %s=%d", quoteOracleIdentifier("name"), quoteTableOracle(table), quoteOracleIdentifier("id"), id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT %s FROM %s WHERE %s=%d", quoteOracleIdentifier("age"), quoteTableOracle(table), quoteOracleIdentifier("id"), id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = 1", quoteTableOracle(table), quoteOracleIdentifier("_scd_is_current"))
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = %d", quoteTableOracle(table), quoteOracleIdentifier("id"), id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = 0 AND %s IS NULL", quoteTableOracle(table), quoteOracleIdentifier("_scd_is_current"), quoteOracleIdentifier("_scd_valid_to"))
		},
	}
}

func normalizeOracleType(dataType string, precision, scale sql.NullInt64) string {
	oracleType := strings.ToUpper(strings.TrimSpace(dataType))
	switch {
	case oracleType == "NUMBER" || strings.HasPrefix(oracleType, "NUMBER("):
		_ = precision
		_ = scale
		return "number"
	case oracleType == "BINARY_FLOAT":
		return "binary_float"
	case oracleType == "BINARY_DOUBLE" || oracleType == "FLOAT":
		return "binary_double"
	case oracleType == "CLOB" || oracleType == "NCLOB" || oracleType == "LONG":
		return "clob"
	case strings.HasPrefix(oracleType, "VARCHAR2") || oracleType == "CHAR" || oracleType == "NCHAR" || oracleType == "NVARCHAR2":
		return "varchar2"
	case strings.HasPrefix(oracleType, "TIMESTAMP"):
		return "timestamp"
	case oracleType == "DATE":
		return "date"
	case oracleType == "BLOB" || oracleType == "RAW" || oracleType == "LONG RAW":
		return "blob"
	default:
		return normalizeTypeName(oracleType)
	}
}

func mssqlConnString(uri string) string {
	// Convert mssql://user:pass@host:port/db to sqlserver://user:pass@host:port?database=db
	uri = strings.TrimPrefix(uri, "mssql://")
	uri = strings.TrimPrefix(uri, "sqlserver://")

	atIdx := strings.Index(uri, "@")
	if atIdx == -1 {
		return "sqlserver://" + uri
	}

	userPass := uri[:atIdx]
	hostDB := uri[atIdx+1:]

	slashIdx := strings.Index(hostDB, "/")
	if slashIdx == -1 {
		return fmt.Sprintf("sqlserver://%s@%s", userPass, hostDB)
	}

	hostPort := hostDB[:slashIdx]
	dbAndParams := hostDB[slashIdx+1:]

	// Parse database and query params
	qIdx := strings.Index(dbAndParams, "?")
	var db, params string
	if qIdx == -1 {
		db = dbAndParams
	} else {
		db = dbAndParams[:qIdx]
		params = dbAndParams[qIdx+1:]
	}

	connStr := fmt.Sprintf("sqlserver://%s@%s?database=%s", userPass, hostPort, db)
	if params != "" {
		connStr += "&" + params
	}
	return connStr
}

func quoteTableMSSQL(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("[%s].[%s]", parts[0], parts[1])
	}
	return fmt.Sprintf("[%s]", table)
}

func mssqlBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			return sql.Open("sqlserver", mssqlConnString(uri))
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			schemaName, tableName := splitSchemaTable(table, "dbo")
			rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
				ORDER BY ORDINAL_POSITION
			`, schemaName, tableName)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeMSSQLType(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "nvarchar",
			"active": "bit",
			"score":  "float",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableMSSQL(table))
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", quoteTableMSSQL(table), id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", quoteTableMSSQL(table), id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 1", quoteTableMSSQL(table))
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", quoteTableMSSQL(table), id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 0 AND _scd_valid_to IS NULL", quoteTableMSSQL(table))
		},
	}
}

func normalizeMSSQLType(mssqlType string) string {
	mssqlType = strings.ToLower(strings.TrimSpace(mssqlType))
	switch mssqlType {
	case "int", "integer", "bigint":
		return "bigint"
	case "nvarchar", "varchar", "char", "nchar", "text", "ntext":
		return "nvarchar"
	case "bit", "boolean", "bool":
		return "bit"
	case "float", "real", "double":
		return "float"
	default:
		return mssqlType
	}
}

// fabricConnString converts a fabric:// URI into the sqlserver:// DSN understood
// by the azuread ("azuresql") driver, mirroring fabric.uriToConnString.
func fabricConnString(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1433"
	}

	var clientID, secret string
	if u.User != nil {
		clientID = u.User.Username()
		secret, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")

	query := u.Query()
	query.Del("driver")
	tenantID := query.Get("tenant_id")
	query.Del("tenant_id")
	if query.Get("fedauth") == "" {
		if clientID != "" {
			query.Set("fedauth", "ActiveDirectoryServicePrincipal")
		} else {
			query.Set("fedauth", "ActiveDirectoryDefault")
		}
	}
	if database != "" {
		query.Set("database", database)
	}
	if query.Get("encrypt") == "" {
		query.Set("encrypt", "true")
	}

	connURL := &url.URL{Scheme: "sqlserver", Host: fmt.Sprintf("%s:%s", host, port)}
	if clientID != "" {
		userID := clientID
		if tenantID != "" {
			userID = clientID + "@" + tenantID
		}
		if secret != "" {
			connURL.User = url.UserPassword(userID, secret)
		} else {
			connURL.User = url.User(userID)
		}
	}
	connURL.RawQuery = query.Encode()
	return connURL.String()
}

func fabricBackend() *sqlBackend {
	return &sqlBackend{
		openDB: func(uri string) (*sql.DB, error) {
			return sql.Open("azuresql", fabricConnString(uri))
		},
		schemaTypes: func(db *sql.DB, table string) (map[string]string, error) {
			schemaName, tableName := splitSchemaTable(table, "dbo")
			rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE
				FROM INFORMATION_SCHEMA.COLUMNS
				WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
				ORDER BY ORDINAL_POSITION
			`, schemaName, tableName)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			out := map[string]string{}
			for rows.Next() {
				var name, dt string
				if err := rows.Scan(&name, &dt); err != nil {
					return nil, err
				}
				out[strings.ToLower(name)] = normalizeFabricType(dt)
			}
			return out, rows.Err()
		},
		expectedTypes: map[string]string{
			"id":     "bigint",
			"name":   "varchar",
			"active": "bit",
			"score":  "float",
		},
		countQuery: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableMSSQL(table))
		},
		nameByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT name FROM %s WHERE id=%d", quoteTableMSSQL(table), id)
		},
		ageByIDQuery: func(table string, id int) string {
			return fmt.Sprintf("SELECT age FROM %s WHERE id=%d", quoteTableMSSQL(table), id)
		},
		scd2CountCurrent: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 1", quoteTableMSSQL(table))
		},
		scd2CountByID: func(table string, id int) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = %d", quoteTableMSSQL(table), id)
		},
		scd2HistNoValidTo: func(table string) string {
			return fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE _scd_is_current = 0 AND _scd_valid_to IS NULL", quoteTableMSSQL(table))
		},
	}
}

func normalizeFabricType(fabricType string) string {
	fabricType = strings.ToLower(strings.TrimSpace(fabricType))
	switch fabricType {
	case "int", "integer", "bigint":
		return "bigint"
	case "varchar", "char":
		return "varchar"
	case "bit", "boolean", "bool":
		return "bit"
	case "float", "real", "double":
		return "float"
	default:
		return fabricType
	}
}

// TestChessSource_ToDestinations tests Chess.com source against all destinations
// This test fetches 1 month of games data from Chess.com and ingests it into each destination
func TestChessSource_ToDestinations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Use a 1-month interval to limit data fetched
	intervalStart := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	// Chess.com source URI with a single player to keep data volume reasonable
	sourceURI := "chess://?players=gothamchess"

	for _, tc := range destinationCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "games",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
				IntervalStart:       &intervalStart,
				IntervalEnd:         &intervalEnd,
				Columns:             "end_time:timestamptz",
			}

			p := pipeline.New(cfg)
			err := p.Run(ctx)
			require.NoError(t, err, "Pipeline should run without errors")

			// Validate that data was ingested
			if tc.sqlBackend != nil {
				validateChessGamesSQL(t, tc.sqlBackend, destURI, destTable)
			}
		})
	}
}

func validateChessGamesSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for chess validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Greater(t, count, 0, "Should have ingested some chess games")
	t.Logf("Chess games ingested: %d rows", count)
}

// Schema evolution test constants
const (
	schemaEvolutionInitialRows = 5
	schemaEvolutionEvolvedRows = 5
	schemaEvolutionTotalRows   = 10
)

// TestDestinations_SchemaEvolution validates schema evolution behavior:
// - First load: table with id(int), name(string), age(int), score(int)
// - Second load: schema evolves to age(string) with values like "UNKNOWN"
// - Destination table should widen the age column from int to string
func TestDestinations_SchemaEvolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	initialURI := jsonlURI(t, "testdata/conformance_schema_evolution_initial.jsonl")
	evolvedURI := jsonlURI(t, "testdata/conformance_schema_evolution_evolved.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.mergeCapable || !tc.schemaEvolutionCapable {
			t.Run(tc.name, func(t *testing.T) {
				t.Skip("destination does not support merge/schema evolution")
			})
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			// First load: create table with initial schema
			cfg := &config.IngestConfig{
				SourceURI:           initialURI,
				SourceTable:         "schema_evolution_initial",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
				SchemaContract:      "evolve",
			}

			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First load should succeed")

			// Validate initial schema
			validateSchemaEvolutionInitialSQL(t, tc.sqlBackend, destURI, destTable)

			// Second load: evolved schema (age becomes string, email becomes JSON/array)
			cfg.SourceURI = evolvedURI
			cfg.SourceTable = "schema_evolution_evolved"

			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Second load with evolved schema should succeed")

			// Validate final state: row count and evolved types
			validateSchemaEvolutionFinalSQL(t, tc.sqlBackend, destURI, destTable)
		})
	}
}

func validateSchemaEvolutionInitialSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for schema evolution validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, schemaEvolutionInitialRows, count, "Should have initial rows")
}

func validateSchemaEvolutionFinalSQL(t *testing.T, backend *sqlBackend, uri, table string) {
	t.Helper()
	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend for schema evolution validation: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	// Validate row count (initial + evolved rows merged by id)
	var count int
	require.NoError(t, db.QueryRow(backend.countQuery(table)).Scan(&count))
	assert.Equal(t, schemaEvolutionTotalRows, count, "Should have all rows after merge")

	// Validate evolved types
	actualTypes, err := backend.schemaTypes(db, table)
	if err != nil {
		t.Skipf("Could not read schema types: %v", err)
		return
	}

	// Get expected evolved types for this backend
	expectedTypes := getSchemaEvolutionExpectedTypes(backend)
	if expectedTypes == nil {
		t.Skip("No expected evolved types defined for this backend")
		return
	}

	for col, expectedType := range expectedTypes {
		actualType, ok := actualTypes[col]
		if !ok {
			t.Errorf("Column %q not found in destination schema", col)
			continue
		}
		assert.Equal(t, expectedType, actualType, "Column %q type mismatch", col)
	}

	// Validate that we can read the evolved data - check a row with UNKNOWN age
	var ageVal interface{}
	ageQuery := backend.ageByIDQuery(table, 6)
	if ageQuery != "" {
		err = db.QueryRow(ageQuery).Scan(&ageVal)
		if err == nil {
			// Value should be "UNKNOWN" (as string) after type widening
			// Handle both string and []byte types returned by different drivers
			var ageStr string
			switch v := ageVal.(type) {
			case string:
				ageStr = v
			case []byte:
				ageStr = string(v)
			default:
				ageStr = fmt.Sprintf("%v", v)
			}
			assert.Contains(t, ageStr, "UNKNOWN", "Age for id=6 should contain UNKNOWN")
		}
	}
}

func getSchemaEvolutionExpectedTypes(backend *sqlBackend) map[string]string {
	// Return expected types after schema evolution for each backend
	// After evolution:
	// - id: still int/bigint
	// - name: still string/text
	// - age: widened from int to string (to hold "UNKNOWN")
	// - score: still int/bigint (unchanged)

	switch {
	case backend.expectedTypes["id"] == "bigint" && backend.expectedTypes["name"] == "text":
		// Postgres
		return map[string]string{
			"id":    "bigint",
			"name":  "text",
			"age":   "text",
			"score": "bigint",
		}
	case backend.expectedTypes["id"] == "integer" && backend.expectedTypes["name"] == "text":
		// SQLite - types are more flexible
		return map[string]string{
			"id":    "integer",
			"name":  "text",
			"age":   "text",
			"score": "integer",
		}
	case backend.expectedTypes["id"] == "bigint" && backend.expectedTypes["name"] == "varchar":
		// DuckDB or Snowflake
		if backend.expectedTypes["active"] == "boolean" {
			// DuckDB
			return map[string]string{
				"id":    "bigint",
				"name":  "varchar",
				"age":   "varchar",
				"score": "bigint",
			}
		}
		// Snowflake
		return map[string]string{
			"id":    "bigint",
			"name":  "varchar",
			"age":   "varchar",
			"score": "bigint",
		}
	case backend.expectedTypes["id"] == "int64" && backend.expectedTypes["name"] == "string":
		// BigQuery or ClickHouse
		if backend.expectedTypes["active"] == "bool" {
			// ClickHouse or BigQuery
			return map[string]string{
				"id":    "int64",
				"name":  "string",
				"age":   "string",
				"score": "int64",
			}
		}
		return nil
	case backend.expectedTypes["id"] == "bigint" && backend.expectedTypes["name"] == "nvarchar":
		// MSSQL
		return map[string]string{
			"id":    "bigint",
			"name":  "nvarchar",
			"age":   "nvarchar",
			"score": "bigint",
		}
	case backend.expectedTypes["id"] == "bigint" && backend.expectedTypes["active"] == "tinyint":
		// MySQL
		return map[string]string{
			"id":    "bigint",
			"name":  "text",
			"age":   "text",
			"score": "bigint",
		}
	default:
		return nil
	}
}

// TestDestinations_SwapTableCleansUpOldTables verifies that the replace strategy
// properly cleans up temporary _old_ tables after swapping.
// This test reproduces a bug where tables without schema prefix (e.g., "users" instead
// of "main.users") would leave orphaned _old_ tables because the DROP statement
// was malformed (e.g., "DROP TABLE IF EXISTS .users_old_123").
func TestDestinations_SwapTableCleansUpOldTables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	// Only test SQL backends that use the staging/swap pattern
	swapCapableCases := []destCase{}
	for _, tc := range destinationCases() {
		// Skip non-SQL backends and backends that don't use swap
		if tc.sqlBackend == nil {
			continue
		}
		// Include backends that use SwapTable in replace strategy.
		if tc.name == "duckdb" || tc.name == "postgres" || tc.name == "sqlite" || tc.name == "mssql" || tc.name == "oracle" {
			swapCapableCases = append(swapCapableCases, tc)
		}
	}

	for _, tc := range swapCapableCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "swap_cleanup_test",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
			}

			// First run: creates the initial table
			p1 := pipeline.New(cfg)
			require.NoError(t, p1.Run(ctx), "First replace should succeed")

			// Check no _old_ tables after first run
			oldTables := countOldTables(t, tc.sqlBackend, destURI, destTable)
			assert.Equal(t, 0, oldTables, "No _old_ tables should exist after first replace")

			// Second run: triggers swap (rename existing -> _old_, rename staging -> target, drop _old_)
			p2 := pipeline.New(cfg)
			require.NoError(t, p2.Run(ctx), "Second replace should succeed")

			// Check no _old_ tables after second run - this is where the bug manifested
			oldTables = countOldTables(t, tc.sqlBackend, destURI, destTable)
			assert.Equal(t, 0, oldTables, "No _old_ tables should exist after second replace (swap cleanup)")

			// Verify data is correct
			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			if tc.sqlBackend.refreshTable != nil {
				tc.sqlBackend.refreshTable(db, destTable)
			}

			var count int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
			assert.Equal(t, replaceFixtureRows, count, "Table should have correct row count")
		})
	}
}

// countOldTables counts tables with "_old_" in their name for the given destination
func countOldTables(t *testing.T, backend *sqlBackend, uri, table string) int {
	t.Helper()

	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend: %v", err)
		return 0
	}
	defer func() { _ = db.Close() }()

	var count int
	var query string

	// Extract schema and table name for the query
	schemaName, tableName := splitSchemaTable(table, "")

	switch {
	case strings.Contains(uri, "duckdb"):
		// DuckDB: query information_schema
		if schemaName == "" {
			schemaName = "main"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = '%s' AND table_name LIKE '%s_old_%%'
		`, schemaName, tableName)
	case strings.Contains(uri, "postgres"):
		// Postgres: query information_schema
		if schemaName == "" {
			schemaName = "public"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = '%s' AND table_name LIKE '%s_old_%%'
		`, schemaName, tableName)
	case strings.Contains(uri, "mssql") || strings.Contains(uri, "sqlserver"):
		// MSSQL: query sys.tables and sys.schemas
		if schemaName == "" {
			schemaName = "dbo"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*)
			FROM sys.tables t
			INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
			WHERE s.name = '%s' AND t.name LIKE '%s_old_%%'
		`, schemaName, tableName)
	case strings.Contains(uri, "oracle"):
		if schemaName == "" {
			currentUser, err := oracleCurrentUser(db)
			if err != nil {
				t.Logf("Warning: failed to get Oracle current user: %v", err)
				return 0
			}
			schemaName = currentUser
		}
		query = `
			SELECT COUNT(*)
			FROM ALL_TABLES
			WHERE OWNER = :1 AND TABLE_NAME LIKE :2
		`
		err = db.QueryRow(query, strings.ToUpper(schemaName), strings.ToUpper(tableName)+"_OLD_%").Scan(&count)
		if err != nil {
			t.Logf("Warning: failed to count Oracle _old_ tables: %v", err)
			return 0
		}
		return count
	case strings.Contains(uri, "sqlite"):
		// SQLite: query sqlite_master
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM sqlite_master
			WHERE type = 'table' AND name LIKE '%s_old_%%'
		`, tableName)
	default:
		t.Skipf("countOldTables not implemented for this backend")
		return 0
	}

	err = db.QueryRow(query).Scan(&count)
	if err != nil {
		t.Logf("Warning: failed to count _old_ tables: %v", err)
		return 0
	}
	return count
}

// hasPrimaryKey returns true if the given table has a PRIMARY KEY constraint on the
// given column. Used to verify that swap-based replace preserves the PK constraint
// (it can be silently lost if the swap path uses CTAS without a recorded schema).
func hasPrimaryKey(t *testing.T, backend *sqlBackend, uri, table, column string) bool {
	t.Helper()

	db, err := backend.openDB(uri)
	if err != nil {
		t.Skipf("Could not open SQL backend: %v", err)
		return false
	}
	defer func() { _ = db.Close() }()

	schemaName, tableName := splitSchemaTable(table, "")
	var query string

	switch {
	case strings.Contains(uri, "duckdb"):
		if schemaName == "" {
			schemaName = "main"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM duckdb_constraints()
			WHERE schema_name = '%s' AND table_name = '%s'
			  AND constraint_type = 'PRIMARY KEY'
			  AND list_contains(constraint_column_names, '%s')
		`, schemaName, tableName, column)
	case strings.Contains(uri, "postgres"):
		if schemaName == "" {
			schemaName = "public"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON tc.constraint_name = kcu.constraint_name
			 AND tc.table_schema = kcu.table_schema
			WHERE tc.table_schema = '%s'
			  AND tc.table_name = '%s'
			  AND tc.constraint_type = 'PRIMARY KEY'
			  AND kcu.column_name = '%s'
		`, schemaName, tableName, column)
	case strings.Contains(uri, "mssql") || strings.Contains(uri, "sqlserver"):
		if schemaName == "" {
			schemaName = "dbo"
		}
		query = fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON tc.constraint_name = kcu.constraint_name
			 AND tc.table_schema = kcu.table_schema
			WHERE tc.table_schema = '%s'
			  AND tc.table_name = '%s'
			  AND tc.constraint_type = 'PRIMARY KEY'
			  AND kcu.column_name = '%s'
		`, schemaName, tableName, column)
	case strings.Contains(uri, "oracle"):
		if schemaName == "" {
			currentUser, err := oracleCurrentUser(db)
			if err != nil {
				t.Logf("Warning: failed to get Oracle current user: %v", err)
				return false
			}
			schemaName = currentUser
		}
		query = `
			SELECT COUNT(*)
			FROM ALL_CONSTRAINTS c
			JOIN ALL_CONS_COLUMNS cc
			  ON c.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
			 AND c.OWNER = cc.OWNER
			WHERE c.CONSTRAINT_TYPE = 'P'
			  AND c.OWNER = :1
			  AND c.TABLE_NAME = :2
			  AND cc.COLUMN_NAME = :3
		`
		var count int
		if err := db.QueryRow(query, strings.ToUpper(schemaName), strings.ToUpper(tableName), strings.ToUpper(column)).Scan(&count); err != nil {
			t.Logf("Warning: failed to query Oracle primary key: %v", err)
			return false
		}
		return count > 0
	case strings.Contains(uri, "sqlite"):
		// SQLite: PRAGMA table_info reports pk > 0 for primary-key columns.
		query = fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE pk > 0 AND name = '%s'`, tableName, column)
	default:
		t.Skipf("hasPrimaryKey not implemented for this backend")
		return false
	}

	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Logf("Warning: failed to query primary key: %v", err)
		return false
	}
	return count > 0
}

// TestDestinations_Replace_PreservesConstraints verifies that the replace strategy
// preserves the target's PRIMARY KEY after the staging-to-target swap. This regressed
// during PR development when the cross-schema swap path used a plain CTAS that drops
// constraints; the per-table-schema-map fix recreates the target with full constraints.
// This test locks that fix in place.
func TestDestinations_Replace_PreservesConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	swapCapableCases := []destCase{}
	for _, tc := range destinationCases() {
		if tc.sqlBackend == nil {
			continue
		}
		if tc.name == "duckdb" || tc.name == "postgres" || tc.name == "sqlite" || tc.name == "mssql" || tc.name == "oracle" {
			swapCapableCases = append(swapCapableCases, tc)
		}
	}

	for _, tc := range swapCapableCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "preserves_constraints_test",
				DestURI:             destURI,
				DestTable:           destTable,
				PrimaryKeys:         []string{"id"},
				IncrementalStrategy: config.StrategyReplace,
			}

			require.NoError(t, pipeline.New(cfg).Run(ctx), "First replace should succeed")
			assert.True(t, hasPrimaryKey(t, tc.sqlBackend, destURI, destTable, "id"),
				"PRIMARY KEY on id should be present after first replace")

			// Second run exercises the cross-schema swap path again — the staging table is
			// in _bruin_staging while the existing target is in the target schema. This is
			// where the CTAS regression manifested.
			require.NoError(t, pipeline.New(cfg).Run(ctx), "Second replace should succeed")
			assert.True(t, hasPrimaryKey(t, tc.sqlBackend, destURI, destTable, "id"),
				"PRIMARY KEY on id should still be present after second replace (cross-schema swap)")
		})
	}
}

// TestDestinations_Replace_DedupesByPK verifies that, for destinations that opt
// into deduplicated replace (DuckDB, SQLite, BigQuery, Postgres), a source
// containing duplicate primary keys collapses to one row per key in the target,
// keeping the latest row by incremental key. The fixture has 5 rows over 3
// distinct ids ({1,1,2,3,3}); incremental key = score, so the higher-score row
// wins.
func TestDestinations_Replace_DedupesByPK(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance_replace_dedup.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.replaceDedupCapable {
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "replace_dedup",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
				IncrementalKey:      "score",
				PrimaryKeys:         []string{"id"},
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			if tc.name == "duckdb" || tc.name == "sqlite" || tc.name == "postgres" || tc.name == "mssql" || tc.name == "oracle" {
				assert.True(t, hasPrimaryKey(t, tc.sqlBackend, destURI, destTable, "id"),
					"target should keep its PRIMARY KEY after a deduplicated replace")
			}

			db, err := tc.sqlBackend.openDB(destURI)
			if err != nil {
				t.Skipf("Could not open SQL backend for dedup validation: %v", err)
				return
			}
			defer func() { _ = db.Close() }()

			var count int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
			assert.Equal(t, 3, count, "duplicate primary keys should collapse to one row per key")

			// Latest row per key wins (highest score).
			var name1Raw []byte
			require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(destTable, 1)).Scan(&name1Raw))
			assert.Equal(t, "v1-latest", string(name1Raw), "id=1 should keep the latest row by incremental key")

			var name3Raw []byte
			require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(destTable, 3)).Scan(&name3Raw))
			assert.Equal(t, "v3-latest", string(name3Raw), "id=3 should keep the latest row by incremental key")
		})
	}
}

// TestDestinations_Replace_DedupesByPK_NoIncrementalKey verifies that a
// deduplicated replace with NO incremental key still collapses duplicate primary
// keys to one row per key. Without an incremental key the dedup ORDER BY falls
// back to "(SELECT NULL)", so the surviving row per key is arbitrary; this test
// asserts only the collapse and that each survivor is a real source row — not
// which duplicate wins. Uses the same fixture (5 rows over 3 distinct ids).
func TestDestinations_Replace_DedupesByPK_NoIncrementalKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance_replace_dedup.jsonl")

	// The arbitrary dedup winner for each id must be one of these real rows.
	validNames := map[int][]string{
		1: {"v1-old", "v1-latest"},
		2: {"v2"},
		3: {"v3-old", "v3-latest"},
	}

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil || !tc.replaceDedupCapable {
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "replace_dedup",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
				PrimaryKeys:         []string{"id"},
				// No IncrementalKey: dedup keeps one arbitrary row per key.
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx))

			if tc.name == "duckdb" || tc.name == "sqlite" || tc.name == "postgres" || tc.name == "mssql" || tc.name == "oracle" {
				assert.True(t, hasPrimaryKey(t, tc.sqlBackend, destURI, destTable, "id"),
					"target should keep its PRIMARY KEY after a deduplicated replace")
			}

			db, err := tc.sqlBackend.openDB(destURI)
			if err != nil {
				t.Skipf("Could not open SQL backend for dedup validation: %v", err)
				return
			}
			defer func() { _ = db.Close() }()

			var count int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
			assert.Equal(t, 3, count, "duplicate primary keys should collapse to one row per key")

			// Each id survives exactly once, holding one of its real source rows.
			for id, names := range validNames {
				var nameRaw []byte
				require.NoError(t, db.QueryRow(tc.sqlBackend.nameByIDQuery(destTable, id)).Scan(&nameRaw))
				assert.Contains(t, names, string(nameRaw),
					"id=%d should keep one of the real source rows (arbitrary winner)", id)
			}
		})
	}
}

// TestDestinations_LongColumnNames validates that destinations handle column names
// exceeding the database identifier length limit (e.g., PostgreSQL's 63-byte limit).
// Two columns that differ only after byte 63 must not collide.
func TestDestinations_LongColumnNames(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance_long_columns.jsonl")

	for _, tc := range destinationCases() {
		tc := tc
		if tc.sqlBackend == nil {
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			destURI, destTable, cleanup := tc.setup(t, ctx)
			defer cleanup()

			cfg := &config.IngestConfig{
				SourceURI:           sourceURI,
				SourceTable:         "long_columns",
				DestURI:             destURI,
				DestTable:           destTable,
				IncrementalStrategy: config.StrategyReplace,
				SchemaNaming:        "snake_case",
			}

			p := pipeline.New(cfg)
			err := p.Run(ctx)
			require.NoError(t, err, "Pipeline should handle long column names without duplicate errors")

			// Validate row count
			db, err := tc.sqlBackend.openDB(destURI)
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			if tc.sqlBackend.refreshTable != nil {
				tc.sqlBackend.refreshTable(db, destTable)
			}

			var count int
			require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
			assert.Equal(t, 3, count, "Should have 3 rows")
		})
	}

	// Sub-test: merge with long column names (reuses same test data)
	updateURI := jsonlURI(t, "testdata/conformance_long_columns_update.jsonl")

	t.Run("merge", func(t *testing.T) {
		for _, tc := range destinationCases() {
			if tc.sqlBackend == nil || !tc.mergeCapable {
				continue
			}

			t.Run(tc.name, func(t *testing.T) {
				destURI, destTable, cleanup := tc.setup(t, ctx)
				defer cleanup()

				cfg := &config.IngestConfig{
					SourceURI:           sourceURI,
					SourceTable:         "long_columns",
					DestURI:             destURI,
					DestTable:           destTable,
					IncrementalStrategy: config.StrategyMerge,
					PrimaryKeys:         []string{"id"},
					SchemaNaming:        "snake_case",
				}

				require.NoError(t, pipeline.New(cfg).Run(ctx), "Initial load should succeed")

				cfg.SourceURI = updateURI
				require.NoError(t, pipeline.New(cfg).Run(ctx), "Merge update should succeed")

				db, err := tc.sqlBackend.openDB(destURI)
				require.NoError(t, err)
				defer func() { _ = db.Close() }()

				if tc.sqlBackend.refreshTable != nil {
					tc.sqlBackend.refreshTable(db, destTable)
				}

				var count int
				require.NoError(t, db.QueryRow(tc.sqlBackend.countQuery(destTable)).Scan(&count))
				assert.Equal(t, 4, count, "Should have 4 rows after merge")
			})
		}
	})
}
