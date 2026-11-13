//go:build integration

package clickhouse_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	_ "github.com/bruin-data/ingestr/internal/testutil/quiettc"
	destpkg "github.com/bruin-data/ingestr/pkg/destination"
	chdest "github.com/bruin-data/ingestr/pkg/destination/clickhouse"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/mysql"
	_ "github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	mysqlmodule "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NOTE: These tests use ClickHouse as both source and destination because:
// 1. The DuckDB ADBC driver has known issues (SIGSEGV crashes, type conversion errors)
// 2. This is consistent with how the destination conformance tests work
// 3. The native ClickHouse driver (clickhouse-go/v2) works reliably
//
// The destination functionality is properly tested this way - data flows from
// ClickHouse source → pipeline → ClickHouse destination using different tables.

const (
	clickhouseUser     = "default"
	clickhousePassword = "clickhouse"
	clickhouseDB       = "testdb"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		_, _ = os.Stderr.WriteString("skipping clickhouse destination integration tests: Docker provider is not available/healthy\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func startClickHouseContainer(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := chmodule.Run(
		ctx,
		"clickhouse/clickhouse-server:24.3",
		chmodule.WithDatabase(clickhouseDB),
		chmodule.WithUsername(clickhouseUser),
		chmodule.WithPassword(clickhousePassword),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("9000/tcp"),
				wait.ForHTTP("/ping").WithPort("8123/tcp").WithStatusCodeMatcher(func(status int) bool {
					return status == 200
				}),
			).WithDeadline(120*time.Second),
		),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("clickhouse://%s:%s@%s:%s/%s",
		clickhouseUser, clickhousePassword, host, port.Port(), clickhouseDB)

	return container, uri, nil
}

func TestClickHouseDestination_Replace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_replace_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 10)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_replace_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	count := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 10, count, "expected 10 rows after replace")
}

func TestClickHouseDestination_Append(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_append_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 5)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_append_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyAppend,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count1 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 5, count1, "expected 5 rows after first append")

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count2 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 10, count2, "expected 10 rows after second append")
}

func TestClickHouseDestination_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_merge_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 5)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_merge_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	count1 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 5, count1, "expected 5 rows after initial load")

	insertClickHouseRow(t, ctx, uri, sourceTable, 6, "User_6", 63.0, true)

	cfg2 := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg2).Run(ctx))

	optimizeClickHouseTable(t, ctx, uri, destTable)

	count2 := countClickHouseRows(t, ctx, uri, destTable)
	assert.Equal(t, 6, count2, "expected 6 rows after merge")
}

func TestClickHouseDestination_DeleteInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	targetTable := fmt.Sprintf("%s.dest_delete_insert_%d", clickhouseDB, time.Now().UnixNano())
	stagingTable := fmt.Sprintf("%s.staging_delete_insert_%d", clickhouseDB, time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, targetTable)
	defer cleanupClickHouseTable(ctx, uri, stagingTable)

	createClickHouseTable(t, ctx, uri, targetTable)
	insertClickHouseRow(t, ctx, uri, targetTable, 1, "User_1", 10.5, false)
	insertClickHouseRow(t, ctx, uri, targetTable, 2, "User_2", 21.0, true)
	insertClickHouseRow(t, ctx, uri, targetTable, 3, "User_3", 31.5, false)
	insertClickHouseRow(t, ctx, uri, targetTable, 4, "User_4", 42.0, true)

	createClickHouseTable(t, ctx, uri, stagingTable)
	insertClickHouseRow(t, ctx, uri, stagingTable, 2, "Updated_2", 22.0, true)
	insertClickHouseRow(t, ctx, uri, stagingTable, 3, "Updated_3a", 33.0, false)
	insertClickHouseRow(t, ctx, uri, stagingTable, 3, "Updated_3b", 33.5, false)
	insertClickHouseRow(t, ctx, uri, stagingTable, 5, "User_5", 52.5, false)

	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	require.NoError(t, dest.DeleteInsertTable(ctx, destpkg.DeleteInsertOptions{
		StagingTable:       stagingTable,
		TargetTable:        targetTable,
		IncrementalKey:     "id",
		IncrementalKeyType: schema.TypeInt64,
		IntervalStart:      int64(2),
		IntervalEnd:        int64(5),
		Columns:            []string{"id", "name", "score", "active"},
		PrimaryKeys:        []string{"id"},
	}))

	assert.Equal(t, 4, countClickHouseRows(t, ctx, uri, targetTable))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 1))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 2))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 3))
	assert.Equal(t, 0, countClickHouseRowsByID(t, ctx, uri, targetTable, 4))
	assert.Equal(t, 1, countClickHouseRowsByID(t, ctx, uri, targetTable, 5))
}

// Regression: before GetTableSchema read back decimal precision/scale, the
// destination column came back as Decimal(38, 0). Widening a real Decimal(38, 9)
// source against that asked for precision 47 and failed every sync after the first.
func TestClickHouseDestination_MergeDecimalTwice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_decimal_%d", time.Now().UnixNano())
	createClickHouseDecimalTable(t, ctx, uri, sourceTable)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	destTable := fmt.Sprintf("%s.dest_decimal_%d", clickhouseDB, time.Now().UnixNano())

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		SourceTable:         sourceTable,
		DestURI:             uri,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx), "first merge should succeed")
	require.NoError(t, pipeline.New(cfg).Run(ctx), "second merge must not fail on decimal widening")
}

func TestClickHouseDestination_GetTableSchemaDecimalAndArray(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	table := fmt.Sprintf("%s.dest_schema_%d", clickhouseDB, time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, table)

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)
	db := clickhouse.OpenDB(opts)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id Int64,
			amount Nullable(Decimal(38, 9)),
			tags Array(String),
			created_at Nullable(DateTime64(6, 'UTC')),
			seen_on Date
		) ENGINE = MergeTree() ORDER BY id`, table))
	require.NoError(t, err)
	_ = db.Close()

	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	ts, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.NotNil(t, ts)

	cols := make(map[string]schema.Column, len(ts.Columns))
	for _, c := range ts.Columns {
		cols[c.Name] = c
	}

	amount := cols["amount"]
	assert.Equal(t, schema.TypeDecimal, amount.DataType)
	assert.Equal(t, 38, amount.Precision)
	assert.Equal(t, 9, amount.Scale)
	assert.True(t, amount.Nullable)

	tags := cols["tags"]
	assert.Equal(t, schema.TypeArray, tags.DataType)
	assert.Equal(t, schema.TypeString, tags.ArrayType)

	createdAt := cols["created_at"]
	assert.Equal(t, schema.TypeTimestampTZ, createdAt.DataType)
	assert.True(t, createdAt.Nullable)

	seenOn := cols["seen_on"]
	assert.Equal(t, schema.TypeDate, seenOn.DataType)
}

// Regression for the "missing tables look existing" concern: system.columns
// returns zero rows (not an error) for a table that does not exist, and
// GetTableSchema must translate that to a nil schema so first-run preparation
// creates the table instead of treating it as existing.
func TestClickHouseDestination_GetTableSchemaMissingTableReturnsNil(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	ts, err := dest.GetTableSchema(ctx, clickhouseDB+".does_not_exist_xyz")
	require.NoError(t, err)
	assert.Nil(t, ts, "missing table must return a nil schema, not an empty one")
}

func createClickHouseDecimalTable(t *testing.T, ctx context.Context, uri string, table string) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id Int64,
			amount Decimal(38, 9)
		) ENGINE = MergeTree()
		ORDER BY id
	`, table))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s VALUES (1, 123.456789), (2, 999.111)", table))
	require.NoError(t, err)
}

func setupClickHouseSourceTable(t *testing.T, ctx context.Context, uri string, table string, numRows int) {
	t.Helper()

	createClickHouseTable(t, ctx, uri, table)

	for i := 1; i <= numRows; i++ {
		insertClickHouseRow(t, ctx, uri, table, i, fmt.Sprintf("User_%d", i), float64(i)*10.5, i%2 == 0)
	}
}

func createClickHouseTable(t *testing.T, ctx context.Context, uri string, table string) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id Int64,
			name String,
			score Float64,
			active Bool
		) ENGINE = MergeTree()
		ORDER BY id
	`, table)

	_, err = db.ExecContext(ctx, createSQL)
	require.NoError(t, err)
}

func insertClickHouseRow(t *testing.T, ctx context.Context, uri string, table string, id int, name string, score float64, active bool) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s VALUES (%d, '%s', %f, %s)",
		table, id, name, score, boolStr(active),
	)
	_, err = db.ExecContext(ctx, insertSQL)
	require.NoError(t, err)
}

func countClickHouseRows(t *testing.T, ctx context.Context, uri, table string) int {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}

func countClickHouseRowsByID(t *testing.T, ctx context.Context, uri, table string, id int) int {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE id = %d", table, id)).Scan(&count)
	require.NoError(t, err)
	return count
}

func optimizeClickHouseTable(t *testing.T, ctx context.Context, uri, table string) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(ctx, fmt.Sprintf("OPTIMIZE TABLE %s FINAL", table))
}

func cleanupClickHouseTable(ctx context.Context, uri string, table string) {
	opts, err := clickhouse.ParseDSN(uri)
	if err != nil {
		return
	}

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestClickHouseDestination_SCD2NullTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	container, uri, err := startClickHouseContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	for _, joinUseNulls := range []int{0, 1} {
		t.Run(fmt.Sprintf("join_use_nulls=%d", joinUseNulls), func(t *testing.T) {
			ctx := clickhouse.Context(t.Context(), clickhouse.WithSettings(clickhouse.Settings{"join_use_nulls": joinUseNulls}))
			dest := chdest.NewClickHouseDestination()
			require.NoError(t, dest.Connect(ctx, uri))
			defer func() { _ = dest.Close(context.Background()) }()
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf("SELECT throwIf(getSetting('join_use_nulls') != %d)", joinUseNulls)))
			target := fmt.Sprintf("scd2_target_%d", joinUseNulls)
			stage := fmt.Sprintf("scd2_stage_%d", joinUseNulls)
			for _, table := range []string{target, stage} {
				require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
tenant UInt8, id UInt8, value Nullable(String),
_scd_valid_from DateTime64(6), _scd_valid_to Nullable(DateTime64(6)), _scd_is_current UInt8
) ENGINE = MergeTree ORDER BY (tenant, id)`, table)))
			}
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
(1, 1, NULL, '2026-01-01', NULL, 1), (1, 2, NULL, '2026-01-01', NULL, 1),
(1, 3, 'gone', '2026-01-01', NULL, 1), (1, 4, 'old', '2026-01-01', NULL, 1),
(2, 1, 'keep', '2026-01-01', NULL, 1)`, target)))
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
(1, 0, 'new', '2026-02-03 04:05:06.123456', NULL, 1),
(1, 1, 'new', '2026-02-03 04:05:06.123456', NULL, 1),
(1, 2, NULL, '2026-02-03 04:05:06.123456', NULL, 1),
(1, 4, NULL, '2026-02-03 04:05:06.123456', NULL, 1),
(2, 1, 'keep', '2026-02-03 04:05:06.123456', NULL, 1)`, stage)))
			require.NoError(t, dest.SCD2Table(ctx, destpkg.SCD2Options{
				TargetTable: target, StagingTable: stage, Columns: []string{"tenant", "id", "value"},
				PrimaryKeys: []string{"tenant", "id"}, Timestamp: time.Date(2026, 2, 3, 4, 5, 6, 123456000, time.UTC),
			}))

			opts, err := clickhouse.ParseDSN(uri)
			require.NoError(t, err)
			db := clickhouse.OpenDB(opts)
			defer func() { _ = db.Close() }()
			rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT tenant, id, ifNull(value, '<NULL>'),
_scd_is_current, ifNull(toString(_scd_valid_to), '') FROM %s ORDER BY tenant, id, _scd_is_current`, target))
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			var got []string
			for rows.Next() {
				var tenant, id, current uint8
				var value, validTo string
				require.NoError(t, rows.Scan(&tenant, &id, &value, &current, &validTo))
				got = append(got, fmt.Sprintf("%d/%d %s %d %s", tenant, id, value, current, validTo))
			}
			require.NoError(t, rows.Err())
			require.Equal(t, []string{
				"1/0 new 1 ",
				"1/1 <NULL> 0 2026-02-03 04:05:06.123456", "1/1 new 1 ",
				"1/2 <NULL> 1 ",
				"1/3 gone 0 2026-02-03 04:05:06.123456",
				"1/4 old 0 2026-02-03 04:05:06.123456", "1/4 <NULL> 1 ",
				"2/1 keep 1 ",
			}, got)
		})
	}
}

func TestClickHouseDestination_ArraysKeepNullElements(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	table := fmt.Sprintf("%s.dest_arrays_%d", clickhouseDB, time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, table)
	ts := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "nums", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
		{Name: "scores", DataType: schema.TypeArray, ArrayType: schema.TypeFloat64, Nullable: true},
		{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true},
	}}
	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	for run := int64(0); run < 2; run++ {
		require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{Table: table, Schema: ts}))
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: arraysWithNulls(run * 2)}
		close(records)
		require.NoError(t, dest.Write(ctx, records, destpkg.WriteOptions{Table: table, Schema: ts}))

		readBack, err := dest.GetTableSchema(ctx, table)
		require.NoError(t, err)
		comparison, err := schemaevolution.Compare(ts, readBack, nil)
		require.NoError(t, err)
		require.False(t, comparison.HasChanges, "%+v", comparison.Changes)
	}

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)
	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT id, toString(nums), toString(scores), toString(tags) FROM %s ORDER BY id", table))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id int64
		var nums, scores, tags string
		require.NoError(t, rows.Scan(&id, &nums, &scores, &tags))
		got = append(got, fmt.Sprintf("%d %s %s %s", id, nums, scores, tags))
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{
		"0 [1,NULL,3] [1.5,NULL] ['a',NULL]",
		"1 [] [] []",
		"2 [1,NULL,3] [1.5,NULL] ['a',NULL]",
		"3 [] [] []",
	}, got)
}

// arraysWithNulls builds two rows: arrays with a NULL element, then NULL arrays.
func arraysWithNulls(firstID int64) arrow.RecordBatch {
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "nums", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64), Nullable: true},
		{Name: "scores", Type: arrow.ListOf(arrow.PrimitiveTypes.Float64), Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
	}, nil))
	defer b.Release()
	b.Field(0).(*array.Int64Builder).AppendValues([]int64{firstID, firstID + 1}, nil)
	nums, scores, tags := b.Field(1).(*array.ListBuilder), b.Field(2).(*array.ListBuilder), b.Field(3).(*array.ListBuilder)
	nums.Append(true)
	nums.ValueBuilder().(*array.Int64Builder).AppendValues([]int64{1, 0, 3}, []bool{true, false, true})
	scores.Append(true)
	scores.ValueBuilder().(*array.Float64Builder).AppendValues([]float64{1.5, 0}, []bool{true, false})
	tags.Append(true)
	tags.ValueBuilder().(*array.StringBuilder).AppendValues([]string{"a", ""}, []bool{true, false})
	nums.AppendNull()
	scores.AppendNull()
	tags.AppendNull()
	return b.NewRecordBatch()
}

func TestClickHouseDestination_CDC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dest := chdest.NewClickHouseDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	// 1. GetMaxCDCLSN on missing table and non-CDC table.
	lsn, err := dest.GetMaxCDCLSN(ctx, clickhouseDB+".cdc_missing")
	require.NoError(t, err)
	require.Empty(t, lsn)

	plainTable := fmt.Sprintf("%s.plain_no_cdc_%d", clickhouseDB, time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, plainTable)
	require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{
		Table: plainTable,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		}},
		PrimaryKeys: []string{"id"},
	}))
	lsn, err = dest.GetMaxCDCLSN(ctx, plainTable)
	require.NoError(t, err)
	require.Empty(t, lsn)

	// 2. Managed CDC state, claims, run lease, conditional truncate/swap/evolution.
	stateTable := "_bruin_staging.cdc_state_it"
	claimTable := "_bruin_staging.cdc_targets_it"
	targetTable := fmt.Sprintf("%s.cdc_users_%d", clickhouseDB, time.Now().UnixNano())
	stageTable := fmt.Sprintf("_bruin_staging.cdc_users_stage_%d", time.Now().UnixNano())
	swapStage := fmt.Sprintf("_bruin_staging.cdc_users_swap_%d", time.Now().UnixNano())
	defer cleanupClickHouseTable(ctx, uri, targetTable)
	defer cleanupClickHouseTable(ctx, uri, stageTable)
	defer cleanupClickHouseTable(ctx, uri, swapStage)

	require.NoError(t, dest.PrepareCDCStateTable(ctx, stateTable, &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "event_id", DataType: schema.TypeString},
			{Name: "state_version", DataType: schema.TypeString},
			{Name: "connector_id", DataType: schema.TypeString},
			{Name: "source_table", DataType: schema.TypeString},
			{Name: "destination_table", DataType: schema.TypeString},
			{Name: "state_kind", DataType: schema.TypeString},
			{Name: "state_generation", DataType: schema.TypeInt64},
			{Name: "state_status", DataType: schema.TypeString},
			{Name: destpkg.CDCLSNColumn, DataType: schema.TypeString},
			{Name: "recorded_at", DataType: schema.TypeTimestampTZ},
		},
		PrimaryKeys: []string{"connector_id", "event_id"},
	}))
	require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, stateTable))

	require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{
		Table: claimTable,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "destination_table", DataType: schema.TypeString},
			{Name: "connector_id", DataType: schema.TypeString},
			{Name: "claimed_at", DataType: schema.TypeTimestampTZ},
		}},
		PrimaryKeys: []string{"destination_table"},
	}))

	lease, err := dest.AcquireManagedCDCRunLease(ctx, "ch-it-conn")
	require.NoError(t, err)
	require.NoError(t, lease.Err())

	fullSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
			{Name: "bio", DataType: schema.TypeString},
			{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString},
			{Name: destpkg.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destpkg.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destpkg.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destpkg.CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}
	targetSchema := destpkg.DestinationTableSchema(fullSchema)

	incarnation, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destpkg.CDCTargetClaim{
		DestinationTable: targetTable,
		ConnectorID:      "conn-1",
		SourceTable:      "public.users",
	}, destpkg.PrepareOptions{
		Table:       targetTable,
		Schema:      targetSchema,
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
		CDCKeys:     []string{"id"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, incarnation)

	err = dest.ClaimCDCTarget(ctx, claimTable, destpkg.CDCTargetClaim{
		DestinationTable: targetTable,
		ConnectorID:      "conn-2",
		SourceTable:      "public.users",
	})
	require.ErrorContains(t, err, "already claimed")

	// Prepare target again with RequirePrimaryKeyMatch.
	require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{
		Table:                  targetTable,
		Schema:                 targetSchema,
		PrimaryKeys:            []string{"id"},
		CDCMode:                true,
		CDCKeys:                []string{"id"},
		RequirePrimaryKeyMatch: true,
	}))

	// Write state events and check LoadCDCState, LoadCDCStateFence, DeleteCDCStateEvents.
	stateCols := []schema.Column{
		{Name: "event_id", DataType: schema.TypeString},
		{Name: "state_version", DataType: schema.TypeString},
		{Name: "connector_id", DataType: schema.TypeString},
		{Name: "source_table", DataType: schema.TypeString},
		{Name: "destination_table", DataType: schema.TypeString},
		{Name: "state_kind", DataType: schema.TypeString},
		{Name: "state_generation", DataType: schema.TypeInt64},
		{Name: "state_status", DataType: schema.TypeString},
		{Name: destpkg.CDCLSNColumn, DataType: schema.TypeString},
		{Name: "recorded_at", DataType: schema.TypeTimestampTZ},
	}
	writeStateRow := func(eventID, kind string, gen int64, status, pos string) {
		rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
			{Name: "event_id", Type: arrow.BinaryTypes.String},
			{Name: "state_version", Type: arrow.BinaryTypes.String},
			{Name: "connector_id", Type: arrow.BinaryTypes.String},
			{Name: "source_table", Type: arrow.BinaryTypes.String},
			{Name: "destination_table", Type: arrow.BinaryTypes.String},
			{Name: "state_kind", Type: arrow.BinaryTypes.String},
			{Name: "state_generation", Type: arrow.PrimitiveTypes.Int64},
			{Name: "state_status", Type: arrow.BinaryTypes.String},
			{Name: destpkg.CDCLSNColumn, Type: arrow.BinaryTypes.String},
			{Name: "recorded_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}},
		}, nil))
		defer rb.Release()
		rb.Field(0).(*array.StringBuilder).Append(eventID)
		rb.Field(1).(*array.StringBuilder).Append("1")
		rb.Field(2).(*array.StringBuilder).Append("conn-1")
		rb.Field(3).(*array.StringBuilder).Append("public.users")
		rb.Field(4).(*array.StringBuilder).Append(targetTable)
		rb.Field(5).(*array.StringBuilder).Append(kind)
		rb.Field(6).(*array.Int64Builder).Append(gen)
		rb.Field(7).(*array.StringBuilder).Append(status)
		rb.Field(8).(*array.StringBuilder).Append(pos)
		rb.Field(9).(*array.TimestampBuilder).Append(arrow.Timestamp(time.Now().UnixMicro()))
		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: rb.NewRecordBatch()}
		close(ch)
		require.NoError(t, dest.Write(ctx, ch, destpkg.WriteOptions{Table: stateTable, Schema: &schema.TableSchema{Columns: stateCols}}))
	}
	writeStateRow("run-1", "run", 1, "completed", "0001")
	writeStateRow("run-2", "run", 2, "in_progress", "0002")
	writeStateRow("pos-1", "position", 2, "completed", "0002")

	entries, err := dest.LoadCDCState(ctx, stateTable, "conn-1")
	require.NoError(t, err)
	require.Len(t, entries, 3)

	fence, err := dest.LoadCDCStateFence(ctx, stateTable, "conn-1")
	require.NoError(t, err)
	require.Equal(t, int64(2), fence.Generation)
	require.Equal(t, []string{"run-2"}, fence.RunEventIDs)

	require.NoError(t, dest.DeleteCDCStateEvents(ctx, stateTable, "conn-1", []string{"run-1"}))
	entries, err = dest.LoadCDCState(ctx, stateTable, "conn-1")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// 3. Helper to write CDC batches to staging and merge into target.
	writeCDCStaging := func(rows []struct {
		id        int64
		name      *string
		bio       *string
		tags      []string
		lsn       string
		deleted   bool
		unchanged string
	},
	) {
		require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{
			Table:     stageTable,
			Schema:    fullSchema,
			DropFirst: true,
			CDCMode:   true,
			CDCKeys:   []string{"id"},
		}))
		rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "bio", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
			{Name: destpkg.CDCLSNColumn, Type: arrow.BinaryTypes.String},
			{Name: destpkg.CDCDeletedColumn, Type: arrow.FixedWidthTypes.Boolean},
			{Name: destpkg.CDCSyncedAtColumn, Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}},
			{Name: destpkg.CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String},
		}, nil))
		defer rb.Release()
		now := arrow.Timestamp(time.Now().UnixMicro())
		for _, r := range rows {
			rb.Field(0).(*array.Int64Builder).Append(r.id)
			if r.name == nil {
				rb.Field(1).(*array.StringBuilder).AppendNull()
			} else {
				rb.Field(1).(*array.StringBuilder).Append(*r.name)
			}
			if r.bio == nil {
				rb.Field(2).(*array.StringBuilder).AppendNull()
			} else {
				rb.Field(2).(*array.StringBuilder).Append(*r.bio)
			}
			lb := rb.Field(3).(*array.ListBuilder)
			if r.tags == nil {
				lb.AppendNull()
			} else {
				lb.Append(true)
				vb := lb.ValueBuilder().(*array.StringBuilder)
				for _, tag := range r.tags {
					vb.Append(tag)
				}
			}
			rb.Field(4).(*array.StringBuilder).Append(r.lsn)
			rb.Field(5).(*array.BooleanBuilder).Append(r.deleted)
			rb.Field(6).(*array.TimestampBuilder).Append(now)
			rb.Field(7).(*array.StringBuilder).Append(r.unchanged)
		}
		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: rb.NewRecordBatch()}
		close(ch)
		require.NoError(t, dest.Write(ctx, ch, destpkg.WriteOptions{Table: stageTable, Schema: fullSchema}))
	}

	strPtr := func(s string) *string { return &s }

	// Batch 1: Initial inserts (id=1, id=2).
	writeCDCStaging([]struct {
		id        int64
		name      *string
		bio       *string
		tags      []string
		lsn       string
		deleted   bool
		unchanged string
	}{
		{id: 1, name: strPtr("alice"), bio: strPtr("original-bio"), tags: []string{"t1", "t2"}, lsn: "0001", unchanged: "[]"},
		{id: 2, name: strPtr("bob"), bio: strPtr("bob-bio"), tags: []string{"b1"}, lsn: "0001", unchanged: "[]"},
	})
	require.NoError(t, dest.MergeTable(ctx, destpkg.MergeOptions{
		TargetTable:            targetTable,
		StagingTable:           stageTable,
		Columns:                fullSchema.ColumnNames(),
		PrimaryKeys:            []string{"id"},
		CDCExpectedIncarnation: incarnation,
	}))

	maxLSN, err := dest.GetMaxCDCLSN(ctx, targetTable)
	require.NoError(t, err)
	require.Equal(t, "0001", maxLSN)

	// Batch 2: TOAST update on id=1 (bio & tags unchanged), explicit NULL on id=2, insert+delete on id=3, delete-only unknown id=99, stale replay id=1 at lsn=0000.
	writeCDCStaging([]struct {
		id        int64
		name      *string
		bio       *string
		tags      []string
		lsn       string
		deleted   bool
		unchanged string
	}{
		{id: 1, name: strPtr("alice-stale"), bio: strPtr("stale-bio"), lsn: "0000", unchanged: "[]"},
		{id: 1, name: strPtr("alice-v2"), bio: nil, tags: nil, lsn: "0002", unchanged: `["Bio","TAGS"]`},
		{id: 2, name: strPtr("bob-v2"), bio: nil, tags: []string{"b2"}, lsn: "0002", unchanged: "[]"},
		{id: 3, name: strPtr("charlie"), bio: strPtr("c-bio"), tags: []string{"c1"}, lsn: "0002", unchanged: "[]"},
		{id: 3, name: nil, bio: nil, tags: nil, lsn: "0003", deleted: true, unchanged: "[]"},
		{id: 99, name: nil, bio: nil, tags: nil, lsn: "0002", deleted: true, unchanged: "[]"},
	})
	require.NoError(t, dest.MergeTable(ctx, destpkg.MergeOptions{
		TargetTable:            targetTable,
		StagingTable:           stageTable,
		Columns:                fullSchema.ColumnNames(),
		PrimaryKeys:            []string{"id"},
		CDCExpectedIncarnation: incarnation,
	}))

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)
	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	queryRows := func() []string {
		rows, qErr := db.QueryContext(ctx, fmt.Sprintf("SELECT id, ifNull(name, '<NULL>'), ifNull(bio, '<NULL>'), toString(tags), _cdc_lsn, _cdc_deleted FROM %s FINAL ORDER BY id", targetTable))
		require.NoError(t, qErr)
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var id int64
			var name, bio, tags, rowLSN string
			var del bool
			require.NoError(t, rows.Scan(&id, &name, &bio, &tags, &rowLSN, &del))
			out = append(out, fmt.Sprintf("%d|%s|%s|%s|%s|%v", id, name, bio, tags, rowLSN, del))
		}
		require.NoError(t, rows.Err())
		return out
	}

	require.Equal(t, []string{
		"1|alice-v2|original-bio|['t1','t2']|0002|false",
		"2|bob-v2|<NULL>|['b2']|0002|false",
		"3|charlie|c-bio|['c1']|0003|true",
	}, queryRows())

	// Batch 3: Hard delete id=1 and id=3.
	writeCDCStaging([]struct {
		id        int64
		name      *string
		bio       *string
		tags      []string
		lsn       string
		deleted   bool
		unchanged string
	}{
		{id: 1, name: nil, bio: nil, tags: nil, lsn: "0004", deleted: true, unchanged: "[]"},
		{id: 3, name: nil, bio: nil, tags: nil, lsn: "0004", deleted: true, unchanged: "[]"},
	})
	require.NoError(t, dest.MergeTable(ctx, destpkg.MergeOptions{
		TargetTable:            targetTable,
		StagingTable:           stageTable,
		Columns:                fullSchema.ColumnNames(),
		PrimaryKeys:            []string{"id"},
		CDCHardDelete:          true,
		CDCExpectedIncarnation: incarnation,
	}))
	require.Equal(t, []string{
		"2|bob-v2|<NULL>|['b2']|0002|false",
	}, queryRows())
	var rawCount uint64
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE id IN (1, 3)", targetTable)).Scan(&rawCount))
	require.Zero(t, rawCount)

	// 4. Conditional schema evolution, conditional truncate, and conditional swap.
	_, evolvedInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, targetTable, &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{Type: schemaevolution.ChangeAddColumn, ColumnName: "notes", NewColumn: schema.Column{Name: "notes", DataType: schema.TypeString, Nullable: true}},
		},
	}, incarnation)
	require.NoError(t, err)
	require.Equal(t, incarnation, evolvedInc)

	require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, targetTable, incarnation))
	require.Empty(t, queryRows())

	require.NoError(t, dest.PrepareTable(ctx, destpkg.PrepareOptions{
		Table:       swapStage,
		Schema:      targetSchema,
		DropFirst:   true,
		PrimaryKeys: []string{"id"},
		CDCMode:     true,
		CDCKeys:     []string{"id"},
	}))
	stageInc, resInc, err := dest.CDCConditionalSwapIncarnations(ctx, targetTable, swapStage)
	require.NoError(t, err)
	require.NoError(t, dest.SwapTable(ctx, destpkg.SwapOptions{
		TargetTable:                   targetTable,
		StagingTable:                  swapStage,
		CDCExpectedIncarnation:        incarnation,
		CDCExpectedStagingIncarnation: stageInc,
		CDCExpectedResultIncarnation:  resInc,
	}))
	afterSwapInc, exists, err := dest.CDCTargetIncarnation(ctx, targetTable)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, resInc, afterSwapInc)

	require.NoError(t, lease.Release())
}

func startPostgresCDCContainerForClickHouse(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
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
	return container, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
}

func startMySQLCDCContainerForClickHouse(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	container, err := mysqlmodule.Run(
		ctx,
		"mysql:8.0",
		mysqlmodule.WithDatabase("testdb"),
		mysqlmodule.WithUsername("root"),
		mysqlmodule.WithPassword("testpassword"),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.Cmd = []string{
				"--server-id=17777",
				"--log-bin=mysql-bin",
				"--gtid-mode=ON",
				"--enforce-gtid-consistency=ON",
				"--binlog-format=ROW",
				"--binlog-row-image=FULL",
				"--default-time-zone=+03:00",
			}
			return nil
		}),
	)
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)
	return container, fmt.Sprintf("mysql://root:testpassword@%s:%s/testdb", host, port.Port())
}

func mysqlDSNForClickHouseTest(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	pass, _ := u.User.Password()
	db := strings.TrimPrefix(u.Path, "/")
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", u.User.Username(), pass, u.Host, db)
}

func TestClickHouseDestination_CDC_FencingAndLease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)
	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	d1 := chdest.NewClickHouseDestination()
	require.NoError(t, d1.Connect(ctx, uri))
	defer func() { _ = d1.Close(ctx) }()

	d2 := chdest.NewClickHouseDestination()
	require.NoError(t, d2.Connect(ctx, uri))
	defer func() { _ = d2.Close(ctx) }()

	t.Run("AcquireManagedCDCRunLease contention and release", func(t *testing.T) {
		connID := fmt.Sprintf("ch-lease-contend-%d", time.Now().UnixNano())
		lease1, err := d1.AcquireManagedCDCRunLease(ctx, connID)
		require.NoError(t, err)
		require.NoError(t, lease1.Err())

		contendCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()
		_, err = d2.AcquireManagedCDCRunLease(contendCtx, connID)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		require.NoError(t, lease1.Release())

		lease2, err := d2.AcquireManagedCDCRunLease(ctx, connID)
		require.NoError(t, err)
		require.NoError(t, lease2.Err())
		require.NoError(t, lease2.Release())
	})

	t.Run("AcquireManagedCDCRunLease expired takeover and stolen loss notification", func(t *testing.T) {
		connID := fmt.Sprintf("ch-lease-expired-%d", time.Now().UnixNano())
		sum := sha256.Sum256([]byte(connID))
		leaseTable := "_bruin_staging.cdc_lease_" + hex.EncodeToString(sum[:10])

		_, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `_bruin_staging`")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf(
			"CREATE TABLE %s (`owner` String, `heartbeat` DateTime64(6, 'UTC')) ENGINE = MergeTree() ORDER BY (`owner`) COMMENT 'ingestr-cdc-lease:dead-holder'",
			leaseTable,
		))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf(
			"INSERT INTO %s (`owner`, `heartbeat`) VALUES ('dead-holder', now64(6) - INTERVAL 60 SECOND)",
			leaseTable,
		))
		require.NoError(t, err)

		tookOver, err := d2.AcquireManagedCDCRunLease(ctx, connID)
		require.NoError(t, err)
		require.NoError(t, tookOver.Err())
		require.NoError(t, tookOver.Release())

		// Acquire active lease and simulate external takeover so monitor() observes loss.
		stolenID := fmt.Sprintf("ch-lease-stolen-%d", time.Now().UnixNano())
		stolenSum := sha256.Sum256([]byte(stolenID))
		stolenName := "cdc_lease_" + hex.EncodeToString(stolenSum[:10])
		activeLease, err := d1.AcquireManagedCDCRunLease(ctx, stolenID)
		require.NoError(t, err)

		tmpName := stolenName + "_stolen_tmp"
		_, err = db.ExecContext(ctx, fmt.Sprintf("RENAME TABLE `_bruin_staging`.`%s` TO `_bruin_staging`.`%s`", stolenName, tmpName))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `_bruin_staging`.`%s`", tmpName))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf(
			"CREATE TABLE `_bruin_staging`.`%s` (`owner` String, `heartbeat` DateTime64(6, 'UTC')) ENGINE = MergeTree() ORDER BY (`owner`) COMMENT 'ingestr-cdc-lease:thief'",
			stolenName,
		))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `_bruin_staging`.`%s`", stolenName))
		})

		select {
		case <-activeLease.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for lease loss detection on ClickHouse")
		}
		require.ErrorIs(t, activeLease.Err(), source.ErrConnectorLeaseLost)
		require.ErrorContains(t, activeLease.Release(), "release ClickHouse CDC run lease")
	})

	t.Run("ClaimAndPrepareEmptyCDCTarget rollback and validation", func(t *testing.T) {
		claimTable := fmt.Sprintf("_bruin_staging.cdc_claims_rb_%d", time.Now().UnixNano())
		target := fmt.Sprintf("%s.cdc_rb_target_%d", clickhouseDB, time.Now().UnixNano())
		defer cleanupClickHouseTable(ctx, uri, target)

		require.NoError(t, d1.PrepareTable(ctx, destpkg.PrepareOptions{
			Table: claimTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "destination_table", DataType: schema.TypeString},
				{Name: "connector_id", DataType: schema.TypeString},
				{Name: "claimed_at", DataType: schema.TypeTimestampTZ},
			}},
			PrimaryKeys: []string{"destination_table"},
		}))

		// Force CREATE TABLE failure after ClaimCDCTarget by passing a nonexistent PK column.
		_, err := d1.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destpkg.CDCTargetClaim{
			DestinationTable: target,
			ConnectorID:      "conn-bad",
			SourceTable:      "public.t",
		}, destpkg.PrepareOptions{
			Table:       target,
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
			PrimaryKeys: []string{"nonexistent_pk"},
			CDCMode:     true,
		})
		require.Error(t, err)

		// Claim must have been rolled back so a valid connector can claim & prepare the target.
		inc, err := d1.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destpkg.CDCTargetClaim{
			DestinationTable: target,
			ConnectorID:      "conn-good",
			SourceTable:      "public.t",
		}, destpkg.PrepareOptions{
			Table: target,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: destpkg.CDCLSNColumn, DataType: schema.TypeString},
				{Name: destpkg.CDCDeletedColumn, DataType: schema.TypeBoolean},
			}},
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
			CDCKeys:     []string{"id"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, inc)

		// Existing table must be rejected by ClaimAndPrepareEmptyCDCTarget.
		_, err = d1.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destpkg.CDCTargetClaim{
			DestinationTable: target,
			ConnectorID:      "conn-good",
			SourceTable:      "public.t",
		}, destpkg.PrepareOptions{
			Table:       target,
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
		})
		require.ErrorContains(t, err, "already exists")
	})

	t.Run("Conditional merge, snapshot range, null unchanged cols, truncate, swap, evolution fencing", func(t *testing.T) {
		target := fmt.Sprintf("%s.cdc_fence_%d", clickhouseDB, time.Now().UnixNano())
		stage := fmt.Sprintf("_bruin_staging.cdc_fence_stage_%d", time.Now().UnixNano())
		swapStage := fmt.Sprintf("_bruin_staging.cdc_fence_swap_%d", time.Now().UnixNano())
		sameDBSwap := fmt.Sprintf("%s.cdc_fence_sameswap_%d", clickhouseDB, time.Now().UnixNano())
		defer cleanupClickHouseTable(ctx, uri, target)
		defer cleanupClickHouseTable(ctx, uri, stage)
		defer cleanupClickHouseTable(ctx, uri, swapStage)
		defer cleanupClickHouseTable(ctx, uri, sameDBSwap)

		fullSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "title", DataType: schema.TypeString, Nullable: false},
				{Name: "score", DataType: schema.TypeInt32, Nullable: false},
				{Name: destpkg.CDCLSNColumn, DataType: schema.TypeString},
				{Name: destpkg.CDCDeletedColumn, DataType: schema.TypeBoolean},
				{Name: destpkg.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
				{Name: destpkg.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
			},
			PrimaryKeys: []string{"id"},
		}
		targetSchema := destpkg.DestinationTableSchema(fullSchema)

		require.NoError(t, d1.PrepareTable(ctx, destpkg.PrepareOptions{
			Table:       target,
			Schema:      targetSchema,
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
			CDCKeys:     []string{"id"},
		}))
		inc, exists, err := d1.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		staleInc := inc + "_stale"

		writeStage := func(rows []struct {
			id        int64
			title     *string
			score     int32
			lsn       string
			deleted   bool
			unchanged *string
		},
		) {
			require.NoError(t, d1.PrepareTable(ctx, destpkg.PrepareOptions{
				Table:     stage,
				Schema:    fullSchema,
				DropFirst: true,
				CDCMode:   true,
				CDCKeys:   []string{"id"},
			}))
			rb := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
				{Name: "id", Type: arrow.PrimitiveTypes.Int64},
				{Name: "title", Type: arrow.BinaryTypes.String, Nullable: true},
				{Name: "score", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
				{Name: destpkg.CDCLSNColumn, Type: arrow.BinaryTypes.String},
				{Name: destpkg.CDCDeletedColumn, Type: arrow.FixedWidthTypes.Boolean},
				{Name: destpkg.CDCSyncedAtColumn, Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}},
				{Name: destpkg.CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String, Nullable: true},
			}, nil))
			defer rb.Release()
			now := arrow.Timestamp(time.Now().UnixMicro())
			for _, r := range rows {
				rb.Field(0).(*array.Int64Builder).Append(r.id)
				if r.title == nil {
					rb.Field(1).(*array.StringBuilder).AppendNull()
				} else {
					rb.Field(1).(*array.StringBuilder).Append(*r.title)
				}
				rb.Field(2).(*array.Int32Builder).Append(r.score)
				rb.Field(3).(*array.StringBuilder).Append(r.lsn)
				rb.Field(4).(*array.BooleanBuilder).Append(r.deleted)
				rb.Field(5).(*array.TimestampBuilder).Append(now)
				if r.unchanged == nil {
					rb.Field(6).(*array.StringBuilder).AppendNull()
				} else {
					rb.Field(6).(*array.StringBuilder).Append(*r.unchanged)
				}
			}
			ch := make(chan source.RecordBatchResult, 1)
			ch <- source.RecordBatchResult{Batch: rb.NewRecordBatch()}
			close(ch)
			require.NoError(t, d1.Write(ctx, ch, destpkg.WriteOptions{Table: stage, Schema: fullSchema}))
		}

		strPtr := func(s string) *string { return &s }

		// Seed ids 1..4 via snapshot merge.
		writeStage([]struct {
			id        int64
			title     *string
			score     int32
			lsn       string
			deleted   bool
			unchanged *string
		}{
			{id: 1, title: strPtr("one"), score: 10, lsn: "0001"},
			{id: 2, title: strPtr("two"), score: 20, lsn: "0001"},
			{id: 3, title: strPtr("three"), score: 30, lsn: "0001"},
			{id: 4, title: strPtr("four"), score: 40, lsn: "0001"},
		})
		require.NoError(t, d1.MergeTable(ctx, destpkg.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			Columns:                fullSchema.ColumnNames(),
			PrimaryKeys:            []string{"id"},
			CDCSnapshot:            true,
			CDCExpectedIncarnation: inc,
		}))

		// Update id=1 with NULL _cdc_unchanged_cols -> must materialize new title/score, not preserve old.
		writeStage([]struct {
			id        int64
			title     *string
			score     int32
			lsn       string
			deleted   bool
			unchanged *string
		}{
			{id: 1, title: strPtr("one-v2"), score: 11, lsn: "0002", unchanged: nil},
		})
		// Stale incarnation & lost lease must fence MergeTable.
		err = d1.MergeTable(ctx, destpkg.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			Columns:                fullSchema.ColumnNames(),
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: staleInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		lostCtx, cancelLost := context.WithCancelCause(ctx)
		cancelLost(source.ErrConnectorLeaseLost)
		err = d1.MergeTable(lostCtx, destpkg.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			Columns:                fullSchema.ColumnNames(),
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: inc,
		})
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)

		require.NoError(t, d1.MergeTable(ctx, destpkg.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			Columns:                fullSchema.ColumnNames(),
			PrimaryKeys:            []string{"id"},
			CDCExpectedIncarnation: inc,
		}))

		// Snapshot range merge (After: [1], Last: [3]) must only overwrite keys in (1, 3].
		writeStage([]struct {
			id        int64
			title     *string
			score     int32
			lsn       string
			deleted   bool
			unchanged *string
		}{
			{id: 2, title: strPtr("two-snap"), score: 22, lsn: "0003"},
			{id: 3, title: strPtr("three-snap"), score: 33, lsn: "0003"},
		})
		require.NoError(t, d1.MergeTable(ctx, destpkg.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			Columns:                fullSchema.ColumnNames(),
			PrimaryKeys:            []string{"id"},
			Schema:                 fullSchema,
			CDCSnapshot:            true,
			CDCSnapshotRange:       &destpkg.CDCSnapshotRange{After: []string{"1"}, Last: []string{"3"}},
			CDCExpectedIncarnation: inc,
		}))

		var gotTitle string
		var gotScore int32
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT title, score FROM %s FINAL WHERE id = 1", target)).Scan(&gotTitle, &gotScore))
		require.Equal(t, "one-v2", gotTitle)
		require.Equal(t, int32(11), gotScore)
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT title, score FROM %s FINAL WHERE id = 2", target)).Scan(&gotTitle, &gotScore))
		require.Equal(t, "two-snap", gotTitle)
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT title, score FROM %s FINAL WHERE id = 4", target)).Scan(&gotTitle, &gotScore))
		require.Equal(t, "four", gotTitle)

		// ApplySchemaEvolutionIfIncarnation fencing + AddColumn + AlterType + Nullability relaxation.
		evolveTarget := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false, IsPrimaryKey: true},
				{Name: "title", DataType: schema.TypeString, Nullable: true},
				{Name: "score", DataType: schema.TypeInt64, Nullable: true},
				{Name: "extra", DataType: schema.TypeString, Nullable: true},
			},
			PrimaryKeys: []string{"id"},
		}
		curSchema, err := d1.GetTableSchema(ctx, target)
		require.NoError(t, err)
		cmp, err := schemaevolution.Compare(evolveTarget, curSchema, nil)
		require.NoError(t, err)
		require.True(t, cmp.HasChanges)

		_, _, err = d1.ApplySchemaEvolutionIfIncarnation(ctx, target, cmp, staleInc)
		require.ErrorContains(t, err, "physical incarnation changed")
		_, _, err = d1.ApplySchemaEvolutionIfIncarnation(lostCtx, target, cmp, inc)
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)

		_, evolvedInc, err := d1.ApplySchemaEvolutionIfIncarnation(ctx, target, cmp, inc)
		require.NoError(t, err)
		require.Equal(t, inc, evolvedInc)

		afterEvolve, err := d1.GetTableSchema(ctx, target)
		require.NoError(t, err)
		colMap := make(map[string]schema.Column, len(afterEvolve.Columns))
		for _, c := range afterEvolve.Columns {
			colMap[c.Name] = c
		}
		require.Equal(t, schema.TypeInt64, colMap["score"].DataType)
		require.True(t, colMap["score"].Nullable)
		require.True(t, colMap["title"].Nullable)
		require.Equal(t, schema.TypeString, colMap["extra"].DataType)

		// TruncateCDCTableIfIncarnation fencing.
		require.Error(t, d1.TruncateCDCTableIfIncarnation(ctx, target, ""))
		require.ErrorContains(t, d1.TruncateCDCTableIfIncarnation(ctx, target, staleInc), "physical incarnation changed")
		require.ErrorIs(t, d1.TruncateCDCTableIfIncarnation(lostCtx, target, inc), source.ErrConnectorLeaseLost)
		require.Equal(t, 4, countClickHouseRows(t, ctx, uri, target))

		// MergeCDCTablesAtomically with Truncate: true.
		writeStage([]struct {
			id        int64
			title     *string
			score     int32
			lsn       string
			deleted   bool
			unchanged *string
		}{
			{id: 10, title: strPtr("ten"), score: 100, lsn: "0005"},
		})
		err = d1.MergeCDCTablesAtomically(ctx, []destpkg.CDCAtomicTableMerge{{
			Truncate: true,
			Options: destpkg.MergeOptions{
				TargetTable:            target,
				StagingTable:           stage,
				Columns:                fullSchema.ColumnNames(),
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: staleInc,
			},
		}})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.Equal(t, 4, countClickHouseRows(t, ctx, uri, target))

		require.NoError(t, d1.MergeCDCTablesAtomically(ctx, []destpkg.CDCAtomicTableMerge{{
			Truncate: true,
			Options: destpkg.MergeOptions{
				TargetTable:            target,
				StagingTable:           stage,
				Columns:                fullSchema.ColumnNames(),
				PrimaryKeys:            []string{"id"},
				CDCExpectedIncarnation: inc,
			},
		}}))
		require.Equal(t, 1, countClickHouseRows(t, ctx, uri, target))

		// SwapTable fencing (cross-DB and same-DB).
		require.NoError(t, d1.PrepareTable(ctx, destpkg.PrepareOptions{
			Table:       swapStage,
			Schema:      targetSchema,
			DropFirst:   true,
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
			CDCKeys:     []string{"id"},
		}))
		stageInc, resInc, err := d1.CDCConditionalSwapIncarnations(ctx, target, swapStage)
		require.NoError(t, err)

		require.ErrorContains(t, d1.SwapTable(ctx, destpkg.SwapOptions{
			TargetTable:                   target,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        staleInc,
			CDCExpectedStagingIncarnation: stageInc,
			CDCExpectedResultIncarnation:  resInc,
		}), "physical incarnation changed")
		require.ErrorContains(t, d1.SwapTable(ctx, destpkg.SwapOptions{
			TargetTable:                   target,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        inc,
			CDCExpectedStagingIncarnation: stageInc + "_stale",
			CDCExpectedResultIncarnation:  resInc,
		}), "physical incarnation changed")
		require.ErrorIs(t, d1.SwapTable(lostCtx, destpkg.SwapOptions{
			TargetTable:                   target,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        inc,
			CDCExpectedStagingIncarnation: stageInc,
			CDCExpectedResultIncarnation:  resInc,
		}), source.ErrConnectorLeaseLost)
		require.Equal(t, 1, countClickHouseRows(t, ctx, uri, target))

		require.NoError(t, d1.SwapTable(ctx, destpkg.SwapOptions{
			TargetTable:                   target,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        inc,
			CDCExpectedStagingIncarnation: stageInc,
			CDCExpectedResultIncarnation:  resInc,
		}))
		curInc, exists, err := d1.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, resInc, curInc)

		// Same-DB swap.
		require.NoError(t, d1.PrepareTable(ctx, destpkg.PrepareOptions{
			Table:       sameDBSwap,
			Schema:      targetSchema,
			DropFirst:   true,
			PrimaryKeys: []string{"id"},
			CDCMode:     true,
			CDCKeys:     []string{"id"},
		}))
		sameStageInc, sameResInc, err := d1.CDCConditionalSwapIncarnations(ctx, target, sameDBSwap)
		require.NoError(t, err)
		require.NoError(t, d1.SwapTable(ctx, destpkg.SwapOptions{
			TargetTable:                   target,
			StagingTable:                  sameDBSwap,
			CDCExpectedIncarnation:        curInc,
			CDCExpectedStagingIncarnation: sameStageInc,
			CDCExpectedResultIncarnation:  sameResInc,
		}))
		curInc2, exists, err := d1.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, sameResInc, curInc2)
	})
}

func TestClickHouseDestination_PostgresCDC_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	chContainer, chURI, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = chContainer.Terminate(ctx) })

	pgContainer, pgURI := startPostgresCDCContainerForClickHouse(t, ctx)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	pool, err := pgxpool.New(ctx, pgURI)
	require.NoError(t, err)
	defer pool.Close()

	opts, err := clickhouse.ParseDSN(chURI)
	require.NoError(t, err)
	chDB := clickhouse.OpenDB(opts)
	defer func() { _ = chDB.Close() }()

	pgExec := func(q string) {
		t.Helper()
		_, execErr := pool.Exec(ctx, q)
		require.NoError(t, execErr)
	}

	t.Run("SoftDelete TOAST unchanged_cols evolution truncate and full refresh", func(t *testing.T) {
		schemaName := fmt.Sprintf("ch_pg_soft_%d", time.Now().UnixNano())
		destDB := schemaName
		t.Cleanup(func() {
			_, _ = chDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+destDB)
		})

		pgExec(fmt.Sprintf(`
			CREATE SCHEMA %[1]s;
			CREATE TABLE %[1]s.items (
				id BIGINT PRIMARY KEY,
				payload TEXT,
				n BIGINT,
				active BOOLEAN,
				amount NUMERIC(18,4) NOT NULL,
				doc JSONB,
				tags TEXT[],
				ts TIMESTAMP,
				tstz TIMESTAMPTZ,
				d DATE,
				u UUID
			);
			ALTER TABLE %[1]s.items ALTER COLUMN payload SET STORAGE EXTERNAL;
			ALTER TABLE %[1]s.items ALTER COLUMN doc SET STORAGE EXTERNAL;
			INSERT INTO %[1]s.items
			SELECT g,
			       repeat('abcdefghij', 500),
			       g,
			       g %% 2 = 0,
			       g / 4.0,
			       jsonb_build_object('n', g, 'large', repeat('json-value', 500)),
			       ARRAY['t1', 't2'],
			       '2026-01-02 03:04:05.123456'::timestamp,
			       '2026-01-02 03:04:05.123456+00'::timestamptz,
			       '2026-01-02'::date,
			       '11111111-2222-3333-4444-555555555555'::uuid
			FROM generate_series(1, 25) g;

			CREATE TABLE %[1]s.composite (tenant BIGINT, id BIGINT, value TEXT, PRIMARY KEY (tenant, id));
			INSERT INTO %[1]s.composite VALUES (1, 1, 'a'), (2, 1, 'b');

			CREATE TABLE %[1]s.empty (id BIGINT PRIMARY KEY, value TEXT);
			CREATE PUBLICATION %[1]s FOR TABLE %[1]s.items, %[1]s.composite, %[1]s.empty;
		`, schemaName))

		cfg := config.DefaultConfig()
		cfg.CDCTableNaming = config.TableNamingTable
		cfg.CDCDeleteBehavior = config.CDCDeleteSoft
		cfg.SourceURI = strings.Replace(pgURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + schemaName + "&dest_schema=" + destDB + "&slot=" + schemaName
		cfg.DestURI = chURI
		cfg.IncrementalStrategy = config.StrategyMerge

		run := func() {
			t.Helper()
			require.NoError(t, pipeline.New(cfg).Run(ctx))
		}
		liveCount := func(table string) int {
			t.Helper()
			var n int
			require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM `%s`.`%s` FINAL WHERE NOT _cdc_deleted", destDB, table)).Scan(&n))
			return n
		}

		// Initial snapshot.
		run()
		require.Equal(t, 25, liveCount("items"))
		require.Equal(t, 2, liveCount("composite"))
		require.Equal(t, 0, liveCount("empty"))

		dest := chdest.NewClickHouseDestination()
		require.NoError(t, dest.Connect(ctx, chURI))
		defer func() { _ = dest.Close(ctx) }()
		maxLSN, err := dest.GetMaxCDCLSN(ctx, destDB+".items")
		require.NoError(t, err)
		require.NotEmpty(t, maxLSN)

		// Verify ClaimCDCTarget rejects a conflicting connector for the claimed target.
		err = dest.ClaimCDCTarget(ctx, "_bruin_staging._ingestr_cdc_targets", destpkg.CDCTargetClaim{
			DestinationTable: destDB + ".items",
			ConnectorID:      "intruder-connector",
			SourceTable:      schemaName + ".items",
		})
		require.ErrorContains(t, err, "already claimed")

		// Incremental DML: TOAST-unchanged updates, explicit NULL, deletes, insert+delete, composite, empty, rolled-back tx.
		pgExec(fmt.Sprintf(`
			UPDATE %[1]s.items SET n = n + 1000, active = NOT active WHERE id <= 5;
			UPDATE %[1]s.items SET n = n + 1000 WHERE id <= 5;
			UPDATE %[1]s.items SET payload = NULL WHERE id = 6;
			DELETE FROM %[1]s.items WHERE id BETWEEN 7 AND 10;
			INSERT INTO %[1]s.items VALUES (26, 'ephemeral', 26, true, 6.5000, '{"n":26}', ARRAY['x'], '2026-02-01', '2026-02-01+00', '2026-02-01', '11111111-2222-3333-4444-555555555555');
			UPDATE %[1]s.items SET n = 260 WHERE id = 26;
			DELETE FROM %[1]s.items WHERE id = 26;
			UPDATE %[1]s.composite SET value = 'changed' WHERE tenant = 2;
			DELETE FROM %[1]s.composite WHERE tenant = 1;
			INSERT INTO %[1]s.empty VALUES (1, 'first');
		`, schemaName))
		pgExec("BEGIN; UPDATE " + schemaName + ".items SET payload = 'rolled-back' WHERE id = 1; ROLLBACK")

		run()
		require.Equal(t, 21, liveCount("items"))
		require.Equal(t, 1, liveCount("composite"))
		require.Equal(t, 1, liveCount("empty"))

		var preserved int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT count() FROM `%s`.`items` FINAL WHERE id <= 5 AND length(payload) = 5000 AND n = id + 2000 AND active = (id %% 2 != 0) AND length(JSONExtractString(doc, 'large')) = 5000 AND NOT _cdc_deleted",
			destDB,
		)).Scan(&preserved))
		require.Equal(t, 5, preserved)

		var payloadIsNull uint8
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT payload IS NULL FROM `%s`.`items` FINAL WHERE id = 6", destDB)).Scan(&payloadIsNull))
		require.Equal(t, uint8(1), payloadIsNull)

		var tombstoneCount int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM `%s`.`items` FINAL WHERE id IN (7, 8, 9, 10, 26) AND _cdc_deleted", destDB)).Scan(&tombstoneCount))
		require.Equal(t, 5, tombstoneCount)

		// Schema evolution: ADD COLUMN + DROP NOT NULL + update with NULL.
		pgExec(fmt.Sprintf(`
			ALTER TABLE %[1]s.items ADD COLUMN extra TEXT;
			ALTER TABLE %[1]s.items ALTER COLUMN amount DROP NOT NULL;
			UPDATE %[1]s.items SET extra = 'evolved', amount = NULL WHERE id = 1;
		`, schemaName))
		run()

		var extra string
		var amountNull uint8
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT ifNull(extra, ''), amount IS NULL FROM `%s`.`items` FINAL WHERE id = 1", destDB)).Scan(&extra, &amountNull))
		require.Equal(t, "evolved", extra)
		require.Equal(t, uint8(1), amountNull)

		// Source TRUNCATE triggers conditional truncate/snapshot invalidation.
		pgExec(fmt.Sprintf("TRUNCATE %[1]s.composite; INSERT INTO %[1]s.composite VALUES (9, 9, 'post-truncate');", schemaName))
		run()
		require.Equal(t, 1, liveCount("composite"))
		var tenant int64
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT tenant FROM `%s`.`composite` FINAL WHERE NOT _cdc_deleted", destDB)).Scan(&tenant))
		require.Equal(t, int64(9), tenant)

		// FullRefresh exercises conditional swap on ClickHouse.
		singleCfg := config.DefaultConfig()
		singleCfg.SourceURI = strings.Replace(pgURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + schemaName + "_single&slot=" + schemaName + "_single"
		singleCfg.SourceTable = schemaName + ".empty"
		singleCfg.DestURI = chURI
		singleCfg.DestTable = destDB + ".empty_single"
		singleCfg.IncrementalStrategy = config.StrategyMerge
		pgExec(fmt.Sprintf("CREATE PUBLICATION %[1]s_single FOR TABLE %[1]s.empty", schemaName))
		require.NoError(t, pipeline.New(singleCfg).Run(ctx))
		singleCfg.FullRefresh = true
		require.NoError(t, pipeline.New(singleCfg).Run(ctx))
		require.Equal(t, 1, liveCount("empty_single"))
	})

	t.Run("HardDelete", func(t *testing.T) {
		schemaName := fmt.Sprintf("ch_pg_hard_%d", time.Now().UnixNano())
		destDB := schemaName
		t.Cleanup(func() {
			_, _ = chDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+destDB)
		})

		pgExec(fmt.Sprintf(`
			CREATE SCHEMA %[1]s;
			CREATE TABLE %[1]s.items (id BIGINT PRIMARY KEY, name TEXT);
			INSERT INTO %[1]s.items SELECT g, 'row_' || g FROM generate_series(1, 10) g;
			CREATE PUBLICATION %[1]s FOR TABLE %[1]s.items;
		`, schemaName))

		cfg := config.DefaultConfig()
		cfg.CDCTableNaming = config.TableNamingTable
		cfg.CDCDeleteBehavior = config.CDCDeleteHard
		cfg.SourceURI = strings.Replace(pgURI, "postgres://", "postgres+cdc://", 1) + "&publication=" + schemaName + "&dest_schema=" + destDB + "&slot=" + schemaName
		cfg.DestURI = chURI
		cfg.IncrementalStrategy = config.StrategyMerge

		require.NoError(t, pipeline.New(cfg).Run(ctx))
		require.Equal(t, 10, countClickHouseRows(t, ctx, chURI, destDB+".items"))

		pgExec(fmt.Sprintf(`
			UPDATE %[1]s.items SET name = 'updated_1' WHERE id = 1;
			DELETE FROM %[1]s.items WHERE id BETWEEN 2 AND 4;
			INSERT INTO %[1]s.items VALUES (11, 'row_11');
			INSERT INTO %[1]s.items VALUES (12, 'ephemeral');
			DELETE FROM %[1]s.items WHERE id = 12;
		`, schemaName))
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		var total, deletedPresent int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM `%s`.`items`", destDB)).Scan(&total))
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM `%s`.`items` WHERE id IN (2, 3, 4, 12)", destDB)).Scan(&deletedPresent))
		require.Equal(t, 8, total)
		require.Equal(t, 0, deletedPresent)

		var name1 string
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT name FROM `%s`.`items` FINAL WHERE id = 1", destDB)).Scan(&name1))
		require.Equal(t, "updated_1", name1)
	})
}

func TestClickHouseDestination_MySQLCDC_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	chContainer, chURI, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = chContainer.Terminate(ctx) })

	mysqlContainer, mysqlURI := startMySQLCDCContainerForClickHouse(t, ctx)
	t.Cleanup(func() { _ = mysqlContainer.Terminate(ctx) })

	mysqlDB, err := sql.Open("mysql", mysqlDSNForClickHouseTest(mysqlURI))
	require.NoError(t, err)
	defer func() { _ = mysqlDB.Close() }()

	opts, err := clickhouse.ParseDSN(chURI)
	require.NoError(t, err)
	chDB := clickhouse.OpenDB(opts)
	defer func() { _ = chDB.Close() }()

	buildMySQLCDCURI := func(serverID string) string {
		u, parseErr := url.Parse(mysqlURI)
		require.NoError(t, parseErr)
		u.Scheme = "mysql+cdc"
		q := u.Query()
		q.Set("mode", "batch")
		q.Set("server_id", serverID)
		u.RawQuery = q.Encode()
		return u.String()
	}

	t.Run("SoftDelete types schema evolution and full refresh", func(t *testing.T) {
		destTable := fmt.Sprintf("%s.mysql_cdc_soft_%d", clickhouseDB, time.Now().UnixNano())
		defer cleanupClickHouseTable(ctx, chURI, destTable)

		_, err := mysqlDB.ExecContext(ctx, `CREATE TABLE items_soft (
			id INT NOT NULL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			value INT NULL,
			big_unsigned BIGINT UNSIGNED NULL,
			amount DECIMAL(18,4) NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `INSERT INTO items_soft (id, name, value, big_unsigned, amount, updated_at) VALUES
			(1, 'item1', 100, 42, 12.3456, '2026-01-02 03:04:05'),
			(2, 'item2', 200, 18446744073709551615, 99.9900, '2026-01-02 03:04:05'),
			(3, 'item3', 300, NULL, NULL, '2026-01-02 03:04:05')`)
		require.NoError(t, err)

		cfg := config.DefaultConfig()
		cfg.SourceURI = buildMySQLCDCURI("18901")
		cfg.SourceTable = "items_soft"
		cfg.DestURI = chURI
		cfg.DestTable = destTable
		cfg.CDCDeleteBehavior = config.CDCDeleteSoft
		cfg.IncrementalStrategy = config.StrategyMerge

		require.NoError(t, pipeline.New(cfg).Run(ctx))
		require.Equal(t, 3, countClickHouseRows(t, ctx, chURI, destTable))

		_, err = mysqlDB.ExecContext(ctx, `UPDATE items_soft SET value = 150 WHERE id = 1`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `DELETE FROM items_soft WHERE id = 2`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `UPDATE items_soft SET name = 'item3_v2', value = 333 WHERE id = 3`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `DELETE FROM items_soft WHERE id = 3`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `INSERT INTO items_soft (id, name, value, big_unsigned, amount, updated_at) VALUES (4, 'twin', 400, 18446744073709551615, 1.2500, '2026-01-02 03:04:05')`)
		require.NoError(t, err)

		require.NoError(t, pipeline.New(cfg).Run(ctx))

		var val1 int32
		var del1 bool
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT value, _cdc_deleted FROM %s FINAL WHERE id = 1", destTable)).Scan(&val1, &del1))
		require.Equal(t, int32(150), val1)
		require.False(t, del1)

		var del2, del3 bool
		var name3 string
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT _cdc_deleted FROM %s FINAL WHERE id = 2", destTable)).Scan(&del2))
		require.True(t, del2)
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT name, _cdc_deleted FROM %s FINAL WHERE id = 3", destTable)).Scan(&name3, &del3))
		require.Equal(t, "item3_v2", name3)
		require.True(t, del3)

		var bigU string
		var distinctTS int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT toString(big_unsigned) FROM %s FINAL WHERE id = 4", destTable)).Scan(&bigU))
		require.Equal(t, "18446744073709551615", bigU)
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count(DISTINCT updated_at) FROM %s FINAL WHERE id IN (2, 4)", destTable)).Scan(&distinctTS))
		require.Equal(t, 1, distinctTS)

		// MySQL CDC refuses DDL on captured tables until --full-refresh rebuilds the destination.
		_, err = mysqlDB.ExecContext(ctx, `ALTER TABLE items_soft ADD COLUMN notes VARCHAR(100) NULL`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `ALTER TABLE items_soft MODIFY COLUMN name VARCHAR(100) NULL`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `INSERT INTO items_soft (id, name, value, notes) VALUES (5, NULL, 500, 'evolved')`)
		require.NoError(t, err)

		err = pipeline.New(cfg).Run(ctx)
		require.ErrorContains(t, err, "--full-refresh")

		// Full refresh rebuilds with evolved schema and exercises conditional swap.
		cfg.FullRefresh = true
		require.NoError(t, pipeline.New(cfg).Run(ctx))

		var nameIsNull uint8
		var notes string
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT name IS NULL, ifNull(notes, '') FROM %s FINAL WHERE id = 5", destTable)).Scan(&nameIsNull, &notes))
		require.Equal(t, uint8(1), nameIsNull)
		require.Equal(t, "evolved", notes)

		var live int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s FINAL WHERE NOT _cdc_deleted", destTable)).Scan(&live))
		require.Equal(t, 3, live)
	})

	t.Run("HardDelete", func(t *testing.T) {
		destTable := fmt.Sprintf("%s.mysql_cdc_hard_%d", clickhouseDB, time.Now().UnixNano())
		defer cleanupClickHouseTable(ctx, chURI, destTable)

		_, err := mysqlDB.ExecContext(ctx, `CREATE TABLE items_hard (
			id INT NOT NULL PRIMARY KEY,
			name VARCHAR(100) NOT NULL
		)`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `INSERT INTO items_hard VALUES (1, 'a'), (2, 'b'), (3, 'c')`)
		require.NoError(t, err)

		cfg := config.DefaultConfig()
		cfg.SourceURI = buildMySQLCDCURI("18902")
		cfg.SourceTable = "items_hard"
		cfg.DestURI = chURI
		cfg.DestTable = destTable
		cfg.CDCDeleteBehavior = config.CDCDeleteHard
		cfg.IncrementalStrategy = config.StrategyMerge

		require.NoError(t, pipeline.New(cfg).Run(ctx))
		require.Equal(t, 3, countClickHouseRows(t, ctx, chURI, destTable))

		_, err = mysqlDB.ExecContext(ctx, `UPDATE items_hard SET name = 'a2' WHERE id = 1`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `DELETE FROM items_hard WHERE id = 2`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `INSERT INTO items_hard VALUES (4, 'd')`)
		require.NoError(t, err)
		_, err = mysqlDB.ExecContext(ctx, `DELETE FROM items_hard WHERE id = 4`)
		require.NoError(t, err)

		require.NoError(t, pipeline.New(cfg).Run(ctx))

		var total, deletedPresent int
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s", destTable)).Scan(&total))
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT count() FROM %s WHERE id IN (2, 4)", destTable)).Scan(&deletedPresent))
		require.Equal(t, 2, total)
		require.Equal(t, 0, deletedPresent)

		var name1 string
		require.NoError(t, chDB.QueryRowContext(ctx, fmt.Sprintf("SELECT name FROM %s FINAL WHERE id = 1", destTable)).Scan(&name1))
		require.Equal(t, "a2", name1)
	})
}
