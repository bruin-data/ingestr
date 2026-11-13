//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

// TestBigQuerySource_RESTReadAPIMatchesStorage reads the same table through
// read_api=storage and read_api=rest and requires identical Arrow output.
// Uses the GONG_TEST_BIGQUERY_* env vars described in bigquery_dedup_test.go.
func TestBigQuerySource_RESTReadAPIMatchesStorage(t *testing.T) {
	dest, client, project, dataset := bqDedupSetup(t)
	_ = dest.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	table := fmt.Sprintf("read_api_types_%d", time.Now().UnixNano())
	qualified := fmt.Sprintf("`%s.%s.%s`", project, dataset, table)
	bqRunQuery(t, ctx, client, fmt.Sprintf(`CREATE TABLE %s (
		id INT64 NOT NULL,
		c_bool BOOL, c_float FLOAT64, c_string STRING, c_bytes BYTES,
		c_timestamp TIMESTAMP, c_datetime DATETIME, c_date DATE, c_time TIME,
		c_numeric NUMERIC, c_numeric_p NUMERIC(10, 2), c_bignumeric BIGNUMERIC,
		c_json JSON, c_geography GEOGRAPHY, c_interval INTERVAL, c_range RANGE<DATE>,
		c_struct STRUCT<x STRING, nested STRUCT<z FLOAT64>>,
		c_array ARRAY<INT64>, c_array_struct ARRAY<STRUCT<k STRING>>
	)`, qualified))
	bqRunQuery(t, ctx, client, fmt.Sprintf(`INSERT INTO %s VALUES
		(1, TRUE, 1.5, 'héllo', b'\x01\x02\x03',
		 TIMESTAMP '2023-11-14 22:13:20.123456 UTC', DATETIME '2024-01-02 03:04:05.123456', DATE '1969-12-31', TIME '12:34:56.789012',
		 NUMERIC '-123.456789012', NUMERIC '12.34', BIGNUMERIC '1.23456789012345678901234567890123456789',
		 JSON '{"a":1}', ST_GEOGPOINT(1, 2), INTERVAL '1-2 3 4:5:6.789' YEAR TO SECOND, RANGE<DATE> '[2024-01-01, UNBOUNDED)',
		 STRUCT('x1', STRUCT(2.5)), [1, 2], [STRUCT('k1'), STRUCT(CAST(NULL AS STRING))]),
		(2, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, [], [])`, qualified))

	baseURI := os.Getenv("GONG_TEST_BIGQUERY_URI")
	tableName := dataset + "." + table

	storage := readBigQueryRows(t, ctx, withReadAPI(baseURI, "storage"), tableName, source.ReadOptions{})
	rest := readBigQueryRows(t, ctx, withReadAPI(baseURI, "rest"), tableName, source.ReadOptions{})
	requireSameArrowSchema(t, storage.schema, rest.schema)
	require.Equal(t, storage.rows, rest.rows)
	require.Len(t, rest.rows, 2)

	start := time.Date(2023, 11, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 11, 15, 0, 0, 0, 0, time.UTC)
	incremental := source.ReadOptions{IncrementalKey: "c_timestamp", IntervalStart: &start, IntervalEnd: &end, ExcludeColumns: []string{"c_geography"}}
	storageFiltered := readBigQueryRows(t, ctx, withReadAPI(baseURI, "storage"), tableName, incremental)
	restFiltered := readBigQueryRows(t, ctx, withReadAPI(baseURI, "rest"), tableName, incremental)
	requireSameArrowSchema(t, storageFiltered.schema, restFiltered.schema)
	require.Equal(t, storageFiltered.rows, restFiltered.rows)
	require.Len(t, restFiltered.rows, 1)

	customQuery := fmt.Sprintf("query:SELECT id, c_struct, c_array FROM %s", qualified)
	restQuery := readBigQueryRows(t, ctx, withReadAPI(baseURI, "rest"), customQuery, source.ReadOptions{})
	require.Len(t, restQuery.rows, 2)
	require.Equal(t, arrow.STRUCT, restQuery.schema.Field(1).Type.ID())
	require.Equal(t, arrow.LIST, restQuery.schema.Field(2).Type.ID())
}

func withReadAPI(baseURI, readAPI string) string {
	sep := "?"
	if strings.Contains(baseURI, "?") {
		sep = "&"
	}
	return baseURI + sep + "read_api=" + readAPI
}

type bigQueryReadResult struct {
	schema *arrow.Schema
	rows   []string
}

// readBigQueryRows returns each row as a JSON-ish string, sorted, so storage
// stream order does not matter.
func readBigQueryRows(t *testing.T, ctx context.Context, sourceURI, tableName string, opts source.ReadOptions) bigQueryReadResult {
	t.Helper()
	src, err := uri.DefaultRegistry.GetSource(sourceURI)
	require.NoError(t, err)
	require.NoError(t, src.Connect(ctx, sourceURI))
	defer func() { _ = src.Close(context.Background()) }()

	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	records, err := table.Read(ctx, opts)
	require.NoError(t, err)

	var result bigQueryReadResult
	for record := range records {
		require.NoError(t, record.Err)
		if record.Batch == nil {
			continue
		}
		if result.schema == nil {
			result.schema = record.Batch.Schema()
		}
		for row := 0; row < int(record.Batch.NumRows()); row++ {
			values := make([]string, record.Batch.NumCols())
			for col := range values {
				values[col] = arrowValueJSON(record.Batch.Column(col), row)
			}
			result.rows = append(result.rows, "["+strings.Join(values, ",")+"]")
		}
		record.Batch.Release()
	}
	sort.Strings(result.rows)
	return result
}

func arrowValueJSON(arr arrow.Array, row int) string {
	slice := array.NewSlice(arr, int64(row), int64(row+1))
	defer slice.Release()
	b, err := slice.MarshalJSON()
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSuffix(strings.TrimPrefix(string(b), "["), "]")
}

func requireSameArrowSchema(t *testing.T, want, got *arrow.Schema) {
	t.Helper()
	require.NotNil(t, want)
	require.NotNil(t, got)
	require.Equal(t, want.NumFields(), got.NumFields(), "want %s\ngot  %s", want, got)
	for i := range want.Fields() {
		w, g := want.Field(i), got.Field(i)
		require.Equal(t, w.Name, g.Name)
		require.Truef(t, arrow.TypeEqual(w.Type, g.Type), "column %s: storage %s, rest %s", w.Name, w.Type, g.Type)
		require.Equal(t, w.Nullable, g.Nullable, "column %s nullability", w.Name)
		wExt, _ := w.Metadata.GetValue("ARROW:extension:name")
		gExt, _ := g.Metadata.GetValue("ARROW:extension:name")
		require.Equal(t, wExt, gExt, "column %s extension name", w.Name)
	}
}
