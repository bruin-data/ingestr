//go:build integration

package clickhouse_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // Register ADBC driver
	chsource "github.com/bruin-data/ingestr/pkg/source/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	chmodule "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/wait"
)

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
		_, _ = os.Stderr.WriteString("skipping clickhouse source integration tests: Docker provider is not available/healthy\n")
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

func TestClickHouseSource_ToDuckDB_Replace(t *testing.T) {
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

	duckdbPath := fmt.Sprintf("/tmp/ch_source_test_%d.duckdb", time.Now().UnixNano())
	defer func() { _ = os.Remove(duckdbPath) }()

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             fmt.Sprintf("duckdb:///%s", duckdbPath),
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyReplace,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM dest_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count, "expected 10 rows in destination")

	var nameRaw []byte
	err = db.QueryRow("SELECT name FROM dest_table WHERE id = 1").Scan(&nameRaw)
	require.NoError(t, err)
	assert.Equal(t, "User_1", string(nameRaw))
}

func TestClickHouseSource_ToDuckDB_IncrementalAppend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_incr_%d", time.Now().UnixNano())
	setupClickHouseSourceTableWithTimestamp(t, ctx, uri, sourceTable)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	duckdbPath := fmt.Sprintf("/tmp/ch_source_incr_%d.duckdb", time.Now().UnixNano())
	defer func() { _ = os.Remove(duckdbPath) }()
	duckdbURI := fmt.Sprintf("duckdb:///%s", duckdbPath)

	start1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end1 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             duckdbURI,
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyAppend,
		IncrementalKey:      "updated_at",
		IntervalStart:       &start1,
		IntervalEnd:         &end1,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	count1 := countDuckDBRows(t, duckdbPath, "dest_table")
	assert.Equal(t, 2, count1, "expected 2 rows for first interval (Jan 1-3)")

	start2 := time.Date(2024, 1, 3, 0, 0, 1, 0, time.UTC)
	end2 := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)

	cfg2 := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             duckdbURI,
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyAppend,
		IncrementalKey:      "updated_at",
		IntervalStart:       &start2,
		IntervalEnd:         &end2,
	}

	require.NoError(t, pipeline.New(cfg2).Run(ctx))

	count2 := countDuckDBRows(t, duckdbPath, "dest_table")
	assert.Equal(t, 4, count2, "expected 4 rows after second incremental load")
}

func TestClickHouseSource_ToDuckDB_WithLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_limit_%d", time.Now().UnixNano())
	setupClickHouseSourceTable(t, ctx, uri, sourceTable, 100)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	duckdbPath := fmt.Sprintf("/tmp/ch_source_limit_%d.duckdb", time.Now().UnixNano())
	defer func() { _ = os.Remove(duckdbPath) }()

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             fmt.Sprintf("duckdb:///%s", duckdbPath),
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyReplace,
		SQLLimit:            25,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	count := countDuckDBRows(t, duckdbPath, "dest_table")
	assert.Equal(t, 25, count, "expected 25 rows with SQL limit")
}

func TestClickHouseSource_ToDuckDB_Merge(t *testing.T) {
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

	duckdbPath := fmt.Sprintf("/tmp/ch_source_merge_%d.duckdb", time.Now().UnixNano())
	defer func() { _ = os.Remove(duckdbPath) }()
	duckdbURI := fmt.Sprintf("duckdb:///%s", duckdbPath)

	cfg := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             duckdbURI,
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyReplace,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	count1 := countDuckDBRows(t, duckdbPath, "dest_table")
	assert.Equal(t, 5, count1, "expected 5 rows after initial load")

	insertClickHouseSimpleRow(t, ctx, uri, sourceTable, 6, "User_6", 63.0, true)

	cfg2 := &config.IngestConfig{
		SourceURI:           uri,
		DestURI:             duckdbURI,
		SourceTable:         sourceTable,
		DestTable:           "dest_table",
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}

	require.NoError(t, pipeline.New(cfg2).Run(ctx))

	count2 := countDuckDBRows(t, duckdbPath, "dest_table")
	assert.Equal(t, 6, count2, "expected 6 rows after merge (5 original + 1 new)")

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckdbPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var nameRaw []byte
	err = db.QueryRow("SELECT name FROM dest_table WHERE id = 6").Scan(&nameRaw)
	require.NoError(t, err)
	assert.Equal(t, "User_6", string(nameRaw), "expected new row with id=6")
}

func TestClickHouseSource_ReadArrayTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container, uri, err := startClickHouseContainer(ctx)
	if err != nil {
		t.Skipf("failed to start ClickHouse container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	sourceTable := fmt.Sprintf("source_array_types_%d", time.Now().UnixNano())
	first := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	second := time.Date(2024, 1, 2, 13, 30, 0, 0, time.UTC)
	setupClickHouseArrayTypesTable(t, ctx, uri, sourceTable, first, second)
	defer cleanupClickHouseTable(ctx, uri, sourceTable)

	src := chsource.NewClickHouseSource()
	require.NoError(t, src.Connect(ctx, uri))
	defer func() { _ = src.Close(ctx) }()

	table, err := src.GetTable(ctx, source.TableRequest{Name: sourceTable})
	require.NoError(t, err)

	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	require.Len(t, tableSchema.Columns, 11)
	requireArrayColumn(t, tableSchema.Columns[1], schema.TypeTimestamp, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[2], schema.TypeTimestamp, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[3], schema.TypeString, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[4], schema.TypeInt32, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[5], schema.TypeBoolean, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[6], schema.TypeDecimal, 18, 5)
	requireArrayColumn(t, tableSchema.Columns[7], schema.TypeUUID, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[8], schema.TypeString, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[9], schema.TypeString, 0, 0)
	requireArrayColumn(t, tableSchema.Columns[10], schema.TypeString, 0, 0)

	records, err := table.Read(ctx, source.ReadOptions{PageSize: 10})
	require.NoError(t, err)

	var batch arrow.RecordBatch
	for res := range records {
		require.NoError(t, res.Err)
		if batch == nil {
			batch = res.Batch
			continue
		}
		res.Batch.Release()
	}
	require.NotNil(t, batch)
	defer batch.Release()

	require.Equal(t, int64(1), batch.NumRows())
	requireTimestampList(t, batch.Column(1), []*time.Time{&first, &second})
	requireTimestampList(t, batch.Column(2), []*time.Time{&first, nil, &second})
	requireStringList(t, batch.Column(3), []*string{stringPtr("alpha"), nil, stringPtr("omega")})
	requireInt32List(t, batch.Column(4), []*int32{int32Ptr(1), nil, int32Ptr(65535)})
	requireBoolList(t, batch.Column(5), []*bool{boolPtr(true), nil, boolPtr(false)})
	requireDecimalList(t, batch.Column(6), []string{"1234567", "", "8900001"})
	requireStringList(t, batch.Column(7), []*string{
		stringPtr("11111111-1111-1111-1111-111111111111"),
		nil,
		stringPtr("22222222-2222-2222-2222-222222222222"),
	})
	requireStringList(t, batch.Column(8), []*string{stringPtr("127.0.0.1"), nil, stringPtr("8.8.8.8")})
	requireStringList(t, batch.Column(9), []*string{stringPtr("Click\x00\x00\x00\x00\x00"), nil, stringPtr("House\x00\x00\x00\x00\x00")})
	requireStringList(t, batch.Column(10), []*string{stringPtr("click"), nil, stringPtr("house")})
}

func countDuckDBRows(t *testing.T, dbPath, table string) int {
	t.Helper()
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", dbPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}

func setupClickHouseSourceTable(t *testing.T, ctx context.Context, uri string, table string, numRows int) {
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

	for i := 1; i <= numRows; i++ {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s VALUES (%d, 'User_%d', %f, %s)",
			table, i, i, float64(i)*10.5, boolStr(i%2 == 0),
		)
		_, err = db.ExecContext(ctx, insertSQL)
		require.NoError(t, err)
	}
}

func setupClickHouseSourceTableWithTimestamp(t *testing.T, ctx context.Context, uri string, table string) {
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
			active Bool,
			updated_at DateTime64(6)
		) ENGINE = MergeTree()
		ORDER BY id
	`, table)

	_, err = db.ExecContext(ctx, createSQL)
	require.NoError(t, err)

	testData := []struct {
		id        int
		name      string
		score     float64
		active    bool
		updatedAt string
	}{
		{1, "User_1", 10.5, true, "2024-01-01 12:00:00"},
		{2, "User_2", 21.0, false, "2024-01-02 12:00:00"},
		{3, "User_3", 31.5, true, "2024-01-03 12:00:00"},
		{4, "User_4", 42.0, false, "2024-01-04 12:00:00"},
		{5, "User_5", 52.5, true, "2024-01-05 12:00:00"},
	}

	for _, d := range testData {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s VALUES (%d, '%s', %f, %s, '%s')",
			table, d.id, d.name, d.score, boolStr(d.active), d.updatedAt,
		)
		_, err = db.ExecContext(ctx, insertSQL)
		require.NoError(t, err)
	}
}

func setupClickHouseArrayTypesTable(t *testing.T, ctx context.Context, uri string, table string, first, second time.Time) {
	t.Helper()

	opts, err := clickhouse.ParseDSN(uri)
	require.NoError(t, err)

	db := clickhouse.OpenDB(opts)
	defer func() { _ = db.Close() }()

	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id UInt8,
			offer_departure Array(DateTime),
			nullable_departure Array(Nullable(DateTime)),
			tags Array(Nullable(String)),
			scores Array(Nullable(UInt16)),
			flags Array(Nullable(Bool)),
			prices Array(Nullable(Decimal(18,5))),
			uuids Array(Nullable(UUID)),
			ips Array(Nullable(IPv4)),
			fixed_names Array(Nullable(FixedString(10))),
			event_types Array(Nullable(Enum8('click' = 1, 'house' = 2)))
		) ENGINE = MergeTree()
		ORDER BY id
	`, table)

	_, err = db.ExecContext(ctx, createSQL)
	require.NoError(t, err)

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s VALUES (
			1,
			[toDateTime('%s'), toDateTime('%s')],
			[toDateTime('%s'), NULL, toDateTime('%s')],
			['alpha', NULL, 'omega'],
			[1, NULL, 65535],
			[true, NULL, false],
			[toDecimal64(12.34567, 5), NULL, toDecimal64(89.00001, 5)],
			[toUUID('11111111-1111-1111-1111-111111111111'), NULL, toUUID('22222222-2222-2222-2222-222222222222')],
			[toIPv4('127.0.0.1'), NULL, toIPv4('8.8.8.8')],
			['Click', NULL, 'House'],
			['click', NULL, 'house']
		)`,
		table,
		first.Format("2006-01-02 15:04:05"),
		second.Format("2006-01-02 15:04:05"),
		first.Format("2006-01-02 15:04:05"),
		second.Format("2006-01-02 15:04:05"),
	)
	_, err = db.ExecContext(ctx, insertSQL)
	require.NoError(t, err)
}

func requireArrayColumn(t *testing.T, col schema.Column, arrayType schema.DataType, precision, scale int) {
	t.Helper()
	require.Equal(t, schema.TypeArray, col.DataType)
	require.Equal(t, arrayType, col.ArrayType)
	require.Equal(t, precision, col.Precision)
	require.Equal(t, scale, col.Scale)
}

func requireTimestampList(t *testing.T, col arrow.Array, want []*time.Time) {
	t.Helper()
	list, ok := col.(*array.List)
	require.True(t, ok)
	require.False(t, list.IsNull(0))
	values := list.ListValues().(*array.Timestamp)
	require.Equal(t, len(want), values.Len())
	for i, expected := range want {
		if expected == nil {
			assert.True(t, values.IsNull(i))
			continue
		}
		assert.False(t, values.IsNull(i))
		assert.Equal(t, arrow.Timestamp(expected.UnixMicro()), values.Value(i))
	}
}

func requireStringList(t *testing.T, col arrow.Array, want []*string) {
	t.Helper()
	list, ok := col.(*array.List)
	require.True(t, ok)
	require.False(t, list.IsNull(0))
	values := list.ListValues().(*array.String)
	require.Equal(t, len(want), values.Len())
	for i, expected := range want {
		if expected == nil {
			assert.True(t, values.IsNull(i))
			continue
		}
		assert.False(t, values.IsNull(i))
		assert.Equal(t, *expected, values.Value(i))
	}
}

func requireInt32List(t *testing.T, col arrow.Array, want []*int32) {
	t.Helper()
	list, ok := col.(*array.List)
	require.True(t, ok)
	require.False(t, list.IsNull(0))
	values := list.ListValues().(*array.Int32)
	require.Equal(t, len(want), values.Len())
	for i, expected := range want {
		if expected == nil {
			assert.True(t, values.IsNull(i))
			continue
		}
		assert.False(t, values.IsNull(i))
		assert.Equal(t, *expected, values.Value(i))
	}
}

func requireBoolList(t *testing.T, col arrow.Array, want []*bool) {
	t.Helper()
	list, ok := col.(*array.List)
	require.True(t, ok)
	require.False(t, list.IsNull(0))
	values := list.ListValues().(*array.Boolean)
	require.Equal(t, len(want), values.Len())
	for i, expected := range want {
		if expected == nil {
			assert.True(t, values.IsNull(i))
			continue
		}
		assert.False(t, values.IsNull(i))
		assert.Equal(t, *expected, values.Value(i))
	}
}

func requireDecimalList(t *testing.T, col arrow.Array, wantRawScaled []string) {
	t.Helper()
	list, ok := col.(*array.List)
	require.True(t, ok)
	require.False(t, list.IsNull(0))
	values := list.ListValues().(*array.Decimal128)
	require.Equal(t, len(wantRawScaled), values.Len())
	for i, expected := range wantRawScaled {
		if expected == "" {
			assert.True(t, values.IsNull(i))
			continue
		}
		assert.False(t, values.IsNull(i))
		assert.Equal(t, expected, decimal128.Num(values.Value(i)).BigInt().String())
	}
}

func stringPtr(v string) *string { return &v }

func int32Ptr(v int32) *int32 { return &v }

func boolPtr(v bool) *bool { return &v }

func insertClickHouseSimpleRow(t *testing.T, ctx context.Context, uri string, table string, id int, name string, score float64, active bool) {
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
