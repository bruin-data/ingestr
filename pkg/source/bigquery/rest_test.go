package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

const allTypesSchemaJSON = `{"fields":[
	{"name":"id","type":"INTEGER","mode":"REQUIRED"},
	{"name":"b","type":"BOOLEAN"},
	{"name":"f","type":"FLOAT"},
	{"name":"s","type":"STRING"},
	{"name":"by","type":"BYTES"},
	{"name":"ts","type":"TIMESTAMP"},
	{"name":"dt","type":"DATETIME"},
	{"name":"d","type":"DATE"},
	{"name":"t","type":"TIME"},
	{"name":"n","type":"NUMERIC","precision":"10","scale":"2"},
	{"name":"bn","type":"BIGNUMERIC"},
	{"name":"j","type":"JSON"},
	{"name":"g","type":"GEOGRAPHY"},
	{"name":"iv","type":"INTERVAL"},
	{"name":"r","type":"RANGE","rangeElementType":{"type":"DATE"}},
	{"name":"rec","type":"RECORD","fields":[
		{"name":"x","type":"STRING"},
		{"name":"inner","type":"RECORD","fields":[{"name":"z","type":"FLOAT"}]}
	]},
	{"name":"arr","type":"INTEGER","mode":"REPEATED"},
	{"name":"recs","type":"RECORD","mode":"REPEATED","fields":[{"name":"k","type":"STRING"}]}
]}`

const allTypesRow1 = `{"f":[
	{"v":"1"},{"v":"true"},{"v":"1.5"},{"v":"héllo"},{"v":"AQID"},
	{"v":"1700000000123456"},{"v":"2024-01-02T03:04:05.123456"},{"v":"1969-12-31"},{"v":"12:34:56.789012"},
	{"v":"-123.45"},{"v":"1.23456789012345678901234567890123456789"},
	{"v":"{\"a\":1}"},{"v":"POINT(1 2)"},{"v":"1-2 3 4:5:6.789"},{"v":"[2024-01-01, UNBOUNDED)"},
	{"v":{"f":[{"v":"x1"},{"v":{"f":[{"v":"2.5"}]}}]}},
	{"v":[{"v":"1"},{"v":"2"}]},
	{"v":[{"v":{"f":[{"v":"k1"}]}},{"v":{"f":[{"v":null}]}}]}
]}`

const allTypesRow2 = `{"f":[
	{"v":"2"},{"v":null},{"v":null},{"v":null},{"v":null},
	{"v":null},{"v":null},{"v":null},{"v":null},
	{"v":null},{"v":null},
	{"v":null},{"v":null},{"v":null},{"v":null},
	{"v":null},
	{"v":[]},
	{"v":[]}
]}`

const eventsSchemaJSON = `{"fields":[
	{"name":"id","type":"INTEGER","mode":"REQUIRED"},
	{"name":"updated_at","type":"TIMESTAMP"},
	{"name":"name","type":"STRING"}
]}`

// fakeBigQuery is a minimal BigQuery REST API server. It fails the test on any
// request it does not know, so a Storage Read API or unexpected call surfaces.
type fakeBigQuery struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	requests []string
	queries  []string

	tables map[string]fakeTable
	// queryResult returns the schema and row pages for a submitted query.
	queryResult func(sql string) (schemaJSON string, pages [][]string)
	// dryRunSchema is returned for dry-run jobs.
	dryRunSchema string
	failData     bool
}

type fakeTable struct {
	tableType string
	schema    string
	pages     [][]string
}

func newFakeBigQuery(t *testing.T) *fakeBigQuery {
	f := &fakeBigQuery{t: t, tables: map[string]fakeTable{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBigQuery) connect(t *testing.T, uri string) *restSource {
	t.Helper()
	d := NewDialect()
	d.extraClientOptions = []option.ClientOption{
		option.WithEndpoint(f.srv.URL + "/bigquery/v2/"),
		option.WithHTTPClient(f.srv.Client()),
	}
	s := newRESTSource(d)
	require.NoError(t, s.Connect(context.Background(), uri))
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func (f *fakeBigQuery) recorded() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...), append([]string(nil), f.queries...)
}

func (f *fakeBigQuery) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/bigquery/v2")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+path+"?"+r.URL.RawQuery)
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && len(parts) == 6 && parts[2] == "datasets" && parts[4] == "tables":
		table, ok := f.tables[parts[1]+"."+parts[3]+"."+parts[5]]
		if !ok {
			http.Error(w, `{"error":{"code":404,"message":"Not found: Table"}}`, http.StatusNotFound)
			return
		}
		writeJSON(w, fmt.Sprintf(`{"type":%q,"schema":%s,"tableReference":{"projectId":%q,"datasetId":%q,"tableId":%q}}`,
			table.tableType, table.schema, parts[1], parts[3], parts[5]))
	case r.Method == http.MethodGet && len(parts) == 7 && parts[6] == "data":
		if f.failData {
			http.Error(w, `{"error":{"code":403,"message":"Access Denied: tabledata.list"}}`, http.StatusForbidden)
			return
		}
		table := f.tables[parts[1]+"."+parts[3]+"."+parts[5]]
		writeJSON(w, pageResponse(table.pages, r.URL.Query().Get("pageToken"), ""))
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "queries":
		var req struct {
			Query string `json:"query"`
		}
		f.decode(r, &req)
		sql := f.recordQuery(req.Query)
		schemaJSON, pages := f.queryResult(sql)
		body := pageResponse(pages, "", schemaJSON)
		body = strings.Replace(body, "{", `{"jobComplete":true,"jobReference":{"projectId":"p","jobId":"job1","location":"US"},`, 1)
		writeJSON(w, body)
	case r.Method == http.MethodGet && len(parts) == 4 && parts[2] == "queries":
		f.mu.Lock()
		sql := f.queries[len(f.queries)-1]
		f.mu.Unlock()
		schemaJSON, pages := f.queryResult(sql)
		body := pageResponse(pages, r.URL.Query().Get("pageToken"), schemaJSON)
		writeJSON(w, strings.Replace(body, "{", `{"jobComplete":true,`, 1))
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "jobs":
		var job struct {
			Configuration struct {
				DryRun bool `json:"dryRun"`
				Query  struct {
					Query string `json:"query"`
				} `json:"query"`
			} `json:"configuration"`
		}
		f.decode(r, &job)
		if !job.Configuration.DryRun {
			f.t.Errorf("unexpected non-dry-run job insert")
		}
		f.recordQuery(job.Configuration.Query.Query)
		writeJSON(w, fmt.Sprintf(`{"jobReference":{"projectId":"p","jobId":"dry","location":"US"},"configuration":{"dryRun":true,"query":{"query":"x"}},"status":{"state":"DONE"},"statistics":{"query":{"schema":%s}}}`, f.dryRunSchema))
	default:
		f.t.Errorf("unexpected BigQuery request: %s %s", r.Method, r.URL.String())
		http.Error(w, `{"error":{"code":404,"message":"unexpected"}}`, http.StatusNotFound)
	}
}

func (f *fakeBigQuery) decode(r *http.Request, v any) {
	body, err := io.ReadAll(r.Body)
	require.NoError(f.t, err)
	require.NoError(f.t, json.Unmarshal(body, v))
}

// recordQuery strips and checks the cost-attribution comment.
func (f *fakeBigQuery) recordQuery(query string) string {
	comment, sql, ok := strings.Cut(query, "\n")
	if !ok || !strings.HasPrefix(comment, "-- @bruin.config: ") || !strings.Contains(comment, `"ingestr_step":"extract"`) {
		f.t.Errorf("query is missing the extract annotation: %q", query)
	}
	f.mu.Lock()
	f.queries = append(f.queries, sql)
	f.mu.Unlock()
	return sql
}

// pageResponse returns the page selected by token ("" or "pN" for page N).
func pageResponse(pages [][]string, token string, schemaJSON string) string {
	idx := 0
	if token != "" {
		_, _ = fmt.Sscanf(token, "p%d", &idx)
	}
	var rows []string
	if idx < len(pages) {
		rows = pages[idx]
	}
	total := 0
	for _, p := range pages {
		total += len(p)
	}
	body := fmt.Sprintf(`{"totalRows":"%d","rows":[%s]`, total, strings.Join(rows, ","))
	if idx+1 < len(pages) {
		body += fmt.Sprintf(`,"pageToken":"p%d"`, idx+1)
	}
	if schemaJSON != "" {
		body += `,"schema":` + schemaJSON
	}
	return body + "}"
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func collectBatches(t *testing.T, results <-chan source.RecordBatchResult) []arrow.RecordBatch {
	t.Helper()
	var batches []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		batches = append(batches, result.Batch)
	}
	t.Cleanup(func() {
		for _, b := range batches {
			b.Release()
		}
	})
	return batches
}

func readTable(t *testing.T, s *restSource, name string, opts source.ReadOptions) []arrow.RecordBatch {
	t.Helper()
	ctx := context.Background()
	table, err := s.GetTable(ctx, source.TableRequest{Name: name})
	require.NoError(t, err)
	results, err := table.Read(ctx, opts)
	require.NoError(t, err)
	return collectBatches(t, results)
}

func jsonMetadata() arrow.Metadata {
	return arrow.NewMetadata([]string{"ARROW:extension:name"}, []string{"google:sqlType:json"})
}

func geographyMetadata() arrow.Metadata {
	return arrow.NewMetadata([]string{"ARROW:extension:metadata", "ARROW:extension:name"}, []string{`{"encoding": "WKT"}`, "google:sqlType:geography"})
}

func datetimeMetadata() arrow.Metadata {
	return arrow.NewMetadata([]string{"ARROW:extension:name"}, []string{"google:sqlType:datetime"})
}

func intervalMetadata() arrow.Metadata {
	return arrow.NewMetadata([]string{"ARROW:extension:name"}, []string{"google:sqlType:interval"})
}

func rangeMetadata() arrow.Metadata {
	return arrow.NewMetadata([]string{"google:sqlType"}, []string{"range"})
}

// storageAllTypesSchema is the Arrow schema the Storage Read API returns for
// allTypesSchemaJSON, as observed against live BigQuery (see
// TestBigQuerySource_RESTReadAPIMatchesStorage).
func storageAllTypesSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "b", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
		{Name: "f", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "by", Type: arrow.BinaryTypes.Binary, Nullable: true},
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
		{Name: "dt", Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true, Metadata: datetimeMetadata()},
		{Name: "d", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
		{Name: "t", Type: arrow.FixedWidthTypes.Time64us, Nullable: true},
		{Name: "n", Type: &arrow.Decimal128Type{Precision: 10, Scale: 2}, Nullable: true},
		{Name: "bn", Type: &arrow.Decimal256Type{Precision: 76, Scale: 38}, Nullable: true},
		{Name: "j", Type: arrow.BinaryTypes.String, Nullable: true, Metadata: jsonMetadata()},
		{Name: "g", Type: arrow.BinaryTypes.String, Nullable: true, Metadata: geographyMetadata()},
		{Name: "iv", Type: arrow.FixedWidthTypes.MonthDayNanoInterval, Nullable: true, Metadata: intervalMetadata()},
		{Name: "r", Type: arrow.StructOf(
			arrow.Field{Name: "start", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
			arrow.Field{Name: "end", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
		), Nullable: true, Metadata: rangeMetadata()},
		{Name: "rec", Type: arrow.StructOf(
			arrow.Field{Name: "x", Type: arrow.BinaryTypes.String, Nullable: true},
			arrow.Field{Name: "inner", Type: arrow.StructOf(
				arrow.Field{Name: "z", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
			), Nullable: true},
		), Nullable: true},
		{Name: "arr", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64)},
		{Name: "recs", Type: arrow.ListOf(arrow.StructOf(
			arrow.Field{Name: "k", Type: arrow.BinaryTypes.String, Nullable: true},
		))},
	}, nil)
}

func TestRESTWholeTableReadUsesTableDataList(t *testing.T) {
	f := newFakeBigQuery(t)
	f.tables["p.d.all_types"] = fakeTable{tableType: "TABLE", schema: allTypesSchemaJSON, pages: [][]string{{allTypesRow1}, {allTypesRow2}}}
	s := f.connect(t, "bigquery://p?read_api=rest")

	batches := readTable(t, s, "d.all_types", source.ReadOptions{PageSize: 1})
	require.Len(t, batches, 2)

	want := storageAllTypesSchema()
	for _, batch := range batches {
		assert.Truef(t, want.Equal(batch.Schema()), "schema mismatch:\nwant %s\ngot  %s", want, batch.Schema())
		assert.EqualValues(t, 1, batch.NumRows())
	}

	full := batches[0]
	col := func(name string) arrow.Array {
		idx := full.Schema().FieldIndices(name)
		require.Len(t, idx, 1, name)
		return full.Column(idx[0])
	}
	assert.Equal(t, int64(1), col("id").(*array.Int64).Value(0))
	assert.True(t, col("b").(*array.Boolean).Value(0))
	assert.Equal(t, 1.5, col("f").(*array.Float64).Value(0))
	assert.Equal(t, "héllo", col("s").(*array.String).Value(0))
	assert.Equal(t, []byte{1, 2, 3}, col("by").(*array.Binary).Value(0))
	assert.Equal(t, arrow.Timestamp(1700000000123456), col("ts").(*array.Timestamp).Value(0))
	assert.Equal(t, arrow.Timestamp(time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC).UnixMicro()), col("dt").(*array.Timestamp).Value(0))
	assert.Equal(t, arrow.Date32(-1), col("d").(*array.Date32).Value(0))
	assert.Equal(t, arrow.Time64(45296789012), col("t").(*array.Time64).Value(0))
	assert.Equal(t, decimal128.FromI64(-12345), col("n").(*array.Decimal128).Value(0))
	bn, ok := new(big.Int).SetString("123456789012345678901234567890123456789", 10)
	require.True(t, ok)
	assert.Equal(t, decimal256.FromBigInt(bn), col("bn").(*array.Decimal256).Value(0))
	assert.Equal(t, `{"a":1}`, col("j").(*array.String).Value(0))
	assert.Equal(t, "POINT(1 2)", col("g").(*array.String).Value(0))
	assert.Equal(t, arrow.MonthDayNanoInterval{Months: 14, Days: 3, Nanoseconds: 14706789000000}, col("iv").(*array.MonthDayNanoInterval).Value(0))

	r := col("r").(*array.Struct)
	assert.Equal(t, arrow.Date32(19723), r.Field(0).(*array.Date32).Value(0))
	assert.True(t, r.Field(1).IsNull(0), "UNBOUNDED range end is null")

	rec := col("rec").(*array.Struct)
	assert.Equal(t, "x1", rec.Field(0).(*array.String).Value(0))
	assert.Equal(t, 2.5, rec.Field(1).(*array.Struct).Field(0).(*array.Float64).Value(0))

	arr := col("arr").(*array.List)
	start, end := arr.ValueOffsets(0)
	assert.Equal(t, []int64{1, 2}, arr.ListValues().(*array.Int64).Int64Values()[start:end])

	recs := col("recs").(*array.List)
	start, end = recs.ValueOffsets(0)
	require.EqualValues(t, 2, end-start)
	keys := recs.ListValues().(*array.Struct).Field(0).(*array.String)
	assert.Equal(t, "k1", keys.Value(int(start)))
	assert.True(t, keys.IsNull(int(start)+1))

	nulls := batches[1]
	for i := 1; i < int(nulls.NumCols()); i++ {
		name := nulls.Schema().Field(i).Name
		if name == "arr" || name == "recs" {
			list := nulls.Column(i).(*array.List)
			assert.False(t, list.IsNull(0), "%s: empty REPEATED is an empty list, not NULL", name)
			s, e := list.ValueOffsets(0)
			assert.Equal(t, s, e, name)
			continue
		}
		assert.True(t, nulls.Column(i).IsNull(0), name)
	}

	requests, queries := f.recorded()
	assert.Empty(t, queries, "whole-table reads must not run a query job")
	require.Len(t, requests, 3)
	assert.True(t, strings.HasPrefix(requests[0], "GET /projects/p/datasets/d/tables/all_types?"), requests[0])
	assert.Contains(t, requests[1], "/tables/all_types/data?")
	assert.Contains(t, requests[1], "maxResults=1")
	assert.Contains(t, requests[2], "pageToken=p1")
}

func TestRESTFilteredReadUsesQueryJob(t *testing.T) {
	f := newFakeBigQuery(t)
	f.tables["other.d.events"] = fakeTable{tableType: "TABLE", schema: eventsSchemaJSON}
	f.queryResult = func(sql string) (string, [][]string) {
		return `{"fields":[{"name":"id","type":"INTEGER","mode":"REQUIRED"},{"name":"updated_at","type":"TIMESTAMP"}]}`,
			[][]string{
				{`{"f":[{"v":"1"},{"v":"1700000000000000"}]}`},
				{`{"f":[{"v":"2"},{"v":"1700000001000000"}]}`, `{"f":[{"v":"3"},{"v":"1700000002000000"}]}`},
			}
	}
	s := f.connect(t, "bigquery://p?read_api=rest&location=EU")

	start := time.Date(2023, 11, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 11, 15, 0, 0, 0, 0, time.UTC)
	batches := readTable(t, s, "other.d.events", source.ReadOptions{
		IncrementalKey: "updated_at",
		IntervalStart:  &start,
		IntervalEnd:    &end,
		ExcludeColumns: []string{"NAME"},
		Limit:          2,
	})

	var ids []int64
	for _, batch := range batches {
		assert.Equal(t, []string{"id", "updated_at"}, []string{batch.Schema().Field(0).Name, batch.Schema().Field(1).Name})
		assert.False(t, batch.Schema().Field(0).Nullable, "REQUIRED keeps the table metadata nullability")
		ids = append(ids, batch.Column(0).(*array.Int64).Int64Values()...)
	}
	assert.Equal(t, []int64{1, 2}, ids, "the row limit is enforced client-side as well")

	requests, queries := f.recorded()
	require.Len(t, queries, 1)
	assert.Equal(t, "SELECT `id`, `updated_at` FROM `other`.`d`.`events` WHERE `updated_at` >= \"2023-11-14T00:00:00Z\" AND `updated_at` <= \"2023-11-15T00:00:00Z\" LIMIT 2", queries[0])
	for _, req := range requests {
		assert.NotContains(t, req, "/data?", "filtered reads must not use tabledata.list")
	}
	assert.Contains(t, strings.Join(requests, "\n"), "GET /projects/p/queries/job1?", "later pages come from jobs.getQueryResults")
}

func TestRESTViewReadUsesQueryJob(t *testing.T) {
	f := newFakeBigQuery(t)
	f.tables["p.d.events_view"] = fakeTable{tableType: "VIEW", schema: eventsSchemaJSON}
	f.queryResult = func(sql string) (string, [][]string) {
		return eventsSchemaJSON, [][]string{{`{"f":[{"v":"1"},{"v":null},{"v":"a"}]}`}}
	}
	s := f.connect(t, "bigquery://p?read_api=rest")

	batches := readTable(t, s, "d.events_view", source.ReadOptions{})
	require.Len(t, batches, 1)
	assert.EqualValues(t, 1, batches[0].NumRows())

	_, queries := f.recorded()
	assert.Equal(t, []string{"SELECT `id`, `updated_at`, `name` FROM `p`.`d`.`events_view`"}, queries)
}

func TestRESTExtractPartitionsUseQueryJobs(t *testing.T) {
	f := newFakeBigQuery(t)
	f.tables["p.d.events"] = fakeTable{tableType: "TABLE", schema: eventsSchemaJSON}
	f.queryResult = func(sql string) (string, [][]string) {
		return eventsSchemaJSON, [][]string{{`{"f":[{"v":"1"},{"v":"1700000000000000"},{"v":"a"}]}`}}
	}
	s := f.connect(t, "bigquery://p?read_api=rest")

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	batches := readTable(t, s, "d.events", source.ReadOptions{
		IncrementalKey:           "updated_at",
		IntervalStart:            &start,
		IntervalEnd:              &end,
		ExtractPartitionBy:       "updated_at",
		ExtractPartitionInterval: 24 * time.Hour,
		Parallelism:              1,
	})
	assert.Len(t, batches, 2)

	_, queries := f.recorded()
	assert.ElementsMatch(t, []string{
		"SELECT `id`, `updated_at`, `name` FROM `p`.`d`.`events` WHERE `updated_at` >= \"2026-01-01T00:00:00Z\" AND `updated_at` <= \"2026-01-03T00:00:00Z\" AND `updated_at` >= \"2026-01-01T00:00:00Z\" AND `updated_at` < \"2026-01-02T00:00:00Z\"",
		"SELECT `id`, `updated_at`, `name` FROM `p`.`d`.`events` WHERE `updated_at` >= \"2026-01-01T00:00:00Z\" AND `updated_at` <= \"2026-01-03T00:00:00Z\" AND `updated_at` >= \"2026-01-02T00:00:00Z\" AND `updated_at` <= \"2026-01-03T00:00:00Z\"",
	}, queries)
}

func TestRESTExtractPartitionBoundsDiscovery(t *testing.T) {
	f := newFakeBigQuery(t)
	f.queryResult = func(sql string) (string, [][]string) {
		return `{"fields":[{"name":"f0_","type":"DATE"},{"name":"f1_","type":"DATE"},{"name":"f2_","type":"INTEGER"},{"name":"f3_","type":"INTEGER"}]}`,
			[][]string{{`{"f":[{"v":"2026-01-01"},{"v":"2026-01-05"},{"v":"10"},{"v":"9"}]}`}}
	}
	s := f.connect(t, "bigquery://p?read_api=rest")

	bounds, err := s.queryExtractPartitionBounds(context.Background(), "SELECT 1", source.ExtractPartitionKindTime)
	require.NoError(t, err)
	assert.True(t, bounds.HasRange)
	assert.True(t, bounds.HasNulls)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), bounds.Start)
	assert.Equal(t, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), bounds.End)
}

func TestRESTCustomQuery(t *testing.T) {
	f := newFakeBigQuery(t)
	f.queryResult = func(sql string) (string, [][]string) {
		return `{"fields":[{"name":"x","type":"INTEGER"},{"name":"tags","type":"STRING","mode":"REPEATED"}]}`,
			[][]string{{`{"f":[{"v":"7"},{"v":[{"v":"a"}]}]}`}, {`{"f":[{"v":"8"},{"v":[]}]}`}}
	}
	f.dryRunSchema = `{"fields":[{"name":"x","type":"INTEGER"},{"name":"day","type":"DATE"}]}`
	s := f.connect(t, "bigquery://p?read_api=rest")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "query:SELECT x, tags FROM d.t"})
	require.NoError(t, err)
	results, err := table.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)
	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	assert.EqualValues(t, 2, batches[0].NumRows())
	assert.True(t, arrow.TypeEqual(arrow.ListOf(arrow.BinaryTypes.String), batches[0].Schema().Field(1).Type))

	readSchema, err := table.(source.ReadSchemaProvider).GetReadSchema(ctx, source.ReadOptions{})
	require.NoError(t, err)
	assert.Equal(t, []schema.Column{
		{Name: "x", DataType: schema.TypeInt64, Nullable: true},
		{Name: "day", DataType: schema.TypeDate, Nullable: true},
	}, readSchema.Columns)

	_, queries := f.recorded()
	assert.Equal(t, []string{
		"SELECT x, tags FROM d.t",
		"SELECT * FROM (SELECT x, tags FROM d.t) AS `__ingestr_query` WHERE 1 = 0",
	}, queries)
}

func TestRESTReadErrorsSurfaceWithoutFallback(t *testing.T) {
	f := newFakeBigQuery(t)
	f.tables["p.d.events"] = fakeTable{tableType: "TABLE", schema: eventsSchemaJSON}
	f.failData = true
	s := f.connect(t, "bigquery://p?read_api=rest")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "d.events"})
	require.NoError(t, err)
	results, err := table.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var readErr error
	for result := range results {
		if result.Err != nil {
			readErr = result.Err
		}
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	require.Error(t, readErr)
	assert.Contains(t, readErr.Error(), "Access Denied: tabledata.list")

	requests, queries := f.recorded()
	assert.Empty(t, queries, "a failed REST read must not fall back to another read path")
	assert.Len(t, requests, 2)
}

func TestRESTGetTableMissingTable(t *testing.T) {
	f := newFakeBigQuery(t)
	s := f.connect(t, "bigquery://p?read_api=rest")

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "d.missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get table metadata")
}
