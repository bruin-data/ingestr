//go:build integration

package integration

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	icebergdest "github.com/bruin-data/ingestr/pkg/destination/iceberg"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Iceberg destination cannot be validated through the shared sqlBackend
// helpers (there is no database/sql driver for Iceberg), so these tests mirror
// the destination conformance suite — same fixtures, same expectations — and
// validate by scanning the Iceberg tables directly.

func icebergConformanceDestURI(t *testing.T) string {
	t.Helper()
	return "iceberg+hadoop://" + t.TempDir() + "?" + url.Values{
		"table.write.format.default": {"parquet"},
	}.Encode()
}

func icebergConformanceTable() string {
	return "lake.conformance_" + uniqueSuffix()
}

func readIcebergRows(t *testing.T, ctx context.Context, destURI, tableName string) []map[string]any {
	t.Helper()

	props, err := parseIcebergTestURI(destURI)
	require.NoError(t, err)
	if parsed, err := url.Parse(destURI); err == nil && parsed.Scheme == "iceberg+hadoop" {
		if parsed.Path != "" && parsed.Path != "/" {
			props["warehouse"] = parsed.Path
		}
	}

	cat, err := icebergcatalog.Load(ctx, icebergTestCatalogName(destURI), props)
	require.NoError(t, err)

	tbl, err := cat.LoadTable(ctx, icebergcatalog.ToIdentifier(strings.Split(tableName, ".")...))
	require.NoError(t, err)

	if tbl.CurrentSnapshot() == nil {
		return nil
	}
	arrowTable, err := tbl.Scan().ToArrowTable(ctx)
	require.NoError(t, err)
	defer arrowTable.Release()

	rows := make([]map[string]any, arrowTable.NumRows())
	for i := range rows {
		rows[i] = make(map[string]any, int(arrowTable.NumCols()))
	}

	for c := 0; c < int(arrowTable.NumCols()); c++ {
		name := arrowTable.Schema().Field(c).Name
		offset := 0
		for _, chunk := range arrowTable.Column(c).Data().Chunks() {
			for i := 0; i < chunk.Len(); i++ {
				rows[offset+i][name] = icebergTestValue(t, chunk, i)
			}
			offset += chunk.Len()
		}
	}
	return rows
}

func icebergTestValue(t *testing.T, arr arrow.Array, i int) any {
	t.Helper()
	if arr.IsNull(i) {
		return nil
	}
	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(i)
	case *array.Int32:
		return int64(a.Value(i))
	case *array.Int64:
		return a.Value(i)
	case *array.Float32:
		return float64(a.Value(i))
	case *array.Float64:
		return a.Value(i)
	case *array.String:
		return a.Value(i)
	case *array.LargeString:
		return a.Value(i)
	case *array.Timestamp:
		return int64(a.Value(i))
	case array.ExtensionArray:
		return icebergTestValue(t, a.Storage(), i)
	default:
		t.Fatalf("unsupported arrow type %s in iceberg validation", arr.DataType())
		return nil
	}
}

func icebergRowsByID(rows []map[string]any) map[int64][]map[string]any {
	out := make(map[int64][]map[string]any)
	for _, row := range rows {
		id, ok := row["id"].(int64)
		if !ok {
			continue
		}
		out[id] = append(out[id], row)
	}
	return out
}

func icebergNameByID(t *testing.T, rows []map[string]any, id int64) string {
	t.Helper()
	group := icebergRowsByID(rows)[id]
	require.Lenf(t, group, 1, "expected exactly one row for id=%d", id)
	name, _ := group[0]["name"].(string)
	return name
}

func TestIcebergConformance_Merge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "merge_source",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	cfg.SourceURI = jsonlURI(t, "testdata/conformance_merge_update.jsonl")
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, mergeAfterRows)
	assert.Equal(t, "alpha-updated", icebergNameByID(t, rows, 1))
	assert.Equal(t, "foxtrot-new", icebergNameByID(t, rows, 6))
}

func TestIcebergConformance_DeleteInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_deleteinsert_initial.jsonl"),
		SourceTable:         "deleteinsert_seed",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	cfg.SourceURI = jsonlURI(t, "testdata/conformance_deleteinsert_interval.jsonl")
	cfg.SourceTable = "deleteinsert_interval"
	cfg.IncrementalStrategy = config.StrategyDeleteInsert
	cfg.IncrementalKey = "id"
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, deleteInsertAfterRows)
	assert.Equal(t, "v1-2", icebergNameByID(t, rows, 2), "rows outside the interval must be unchanged")
	assert.Equal(t, "v1-10", icebergNameByID(t, rows, 10), "rows outside the interval must be unchanged")
	assert.Equal(t, "v2-3", icebergNameByID(t, rows, 3), "rows inside the interval must be replaced")
	assert.Equal(t, "v2-7", icebergNameByID(t, rows, 7), "net-new rows inside the interval must be inserted")
}

func TestIcebergConformance_DeleteInsert_DedupesStagingByPK(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_deleteinsert_dedup.jsonl"),
		SourceTable:         "deleteinsert_dedup",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyDeleteInsert,
		IncrementalKey:      "id",
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	assert.Len(t, readIcebergRows(t, ctx, destURI, destTable), 3, "duplicate primary keys in staging should collapse to one row per key")

	cfg.SourceURI = jsonlURI(t, "testdata/conformance_deleteinsert_dedup_interval.jsonl")
	cfg.SourceTable = "deleteinsert_dedup_interval"
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, 4, "interval delete+insert should replace id=3 and add net-new id=4")
	assert.Equal(t, "v2-3", icebergNameByID(t, rows, 3))
	assert.Equal(t, "v2-4", icebergNameByID(t, rows, 4))
}

func TestIcebergConformance_TruncateInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	seedCfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_append_initial.jsonl"),
		SourceTable:         "truncate_seed",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyReplace,
	}
	require.NoError(t, pipeline.New(seedCfg).Run(ctx))

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance.jsonl"),
		SourceTable:         "truncate_source",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyTruncateInsert,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, replaceFixtureRows, "old rows must be gone, not appended")
	assert.Equal(t, "juliet", icebergNameByID(t, rows, 10))
}

func TestIcebergTruncateInsertPipelineSoftensRemovedRequiredColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()
	dest := icebergdest.NewDestination()
	require.NoError(t, dest.Connect(ctx, destURI))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: destTable,
		Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "legacy", DataType: schema.TypeString, Nullable: false},
		}},
	}))
	require.NoError(t, dest.Close(ctx))

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_append_initial.jsonl"),
		SourceTable:         "truncate_soft_remove",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyTruncateInsert,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		require.Contains(t, row, "legacy")
		require.Nil(t, row["legacy"])
	}
	verify := icebergdest.NewDestination()
	require.NoError(t, verify.Connect(ctx, destURI))
	loaded, err := verify.GetTableSchema(ctx, destTable)
	require.NoError(t, err)
	require.NoError(t, verify.Close(ctx))
	var legacyColumn *schema.Column
	for i := range loaded.Columns {
		if loaded.Columns[i].Name == "legacy" {
			legacyColumn = &loaded.Columns[i]
			break
		}
	}
	require.NotNil(t, legacyColumn)
	require.True(t, legacyColumn.Nullable)
}

func TestIcebergConformance_TruncateInsert_Dedup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_truncate_dupes.jsonl"),
		SourceTable:         "truncate_dupes",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyTruncateInsert,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, 5, "expected 5 distinct ids after dedup")
}

func TestIcebergConformance_SCD2(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_scd2_initial.jsonl"),
		SourceTable:         "scd2_initial",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategySCD2,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx), "Initial SCD2 load should succeed")

	cfg.SourceURI = jsonlURI(t, "testdata/conformance_scd2_update.jsonl")
	cfg.SourceTable = "scd2_update"
	require.NoError(t, pipeline.New(cfg).Run(ctx), "SCD2 update load should succeed")

	rows := readIcebergRows(t, ctx, destURI, destTable)
	assert.Len(t, rows, scd2TotalRows)

	current := 0
	historicalMissingValidTo := 0
	for _, row := range rows {
		if row["_scd_is_current"] == true {
			current++
			assert.Nil(t, row["_scd_valid_to"], "current rows must have open validity")
			continue
		}
		if row["_scd_valid_to"] == nil {
			historicalMissingValidTo++
		}
	}
	assert.Equal(t, scd2CurrentRows, current, "current row count")
	assert.Zero(t, historicalMissingValidTo, "all historical records should have valid_to set")

	byID := icebergRowsByID(rows)
	assert.Len(t, byID[1], 2, "id=1 should have 2 rows (changed record)")
	assert.Len(t, byID[3], 1, "id=3 should have 1 row (unchanged)")
	assert.Len(t, byID[6], 1, "id=6 should have 1 row (net-new)")

	for _, row := range byID[4] {
		assert.Equal(t, false, row["_scd_is_current"], "id=4 was removed from the source and must be soft-deleted")
		assert.NotNil(t, row["_scd_valid_to"])
	}
}

// TestIcebergConformance_MergeIdempotent re-runs the same merge input twice and
// asserts the second run does not change row counts or values.
func TestIcebergConformance_MergeIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	destURI := icebergConformanceDestURI(t)
	destTable := icebergConformanceTable()

	cfg := &config.IngestConfig{
		SourceURI:           jsonlURI(t, "testdata/conformance_merge_initial.jsonl"),
		SourceTable:         "merge_source",
		DestURI:             destURI,
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		PrimaryKeys:         []string{"id"},
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	first := readIcebergRows(t, ctx, destURI, destTable)

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	second := readIcebergRows(t, ctx, destURI, destTable)

	require.Len(t, second, len(first))
	firstByID := icebergRowsByID(first)
	for id, group := range icebergRowsByID(second) {
		require.Len(t, group, 1)
		require.Len(t, firstByID[id], 1)
		for _, col := range []string{"name", "active", "score"} {
			assert.Equalf(t, firstByID[id][0][col], group[0][col], "column %s for id=%d changed on idempotent re-merge", col, id)
		}
	}
}
