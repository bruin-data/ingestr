package databricks

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/databricks/databricks-sdk-go"
	dbsql "github.com/databricks/databricks-sdk-go/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	externalLinkRetryDelay = time.Millisecond
	os.Exit(m.Run())
}

func arrowIPC(t *testing.T, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func arrowStreamServer(t *testing.T, rec arrow.RecordBatch) *httptest.Server {
	t.Helper()
	data := arrowIPC(t, rec)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
}

func recordFromJSON(t *testing.T, sc *arrow.Schema, rows string) arrow.RecordBatch {
	t.Helper()
	rec, _, err := array.RecordFromJSON(memory.NewGoAllocator(), sc, strings.NewReader(rows))
	require.NoError(t, err)
	return rec
}

func collect(t *testing.T, resp *dbsql.StatementResponse, target *arrow.Schema, maxBatchBytes int64) ([]arrow.RecordBatch, error) {
	t.Helper()
	results := make(chan source.RecordBatchResult)
	go func() {
		defer close(results)
		(&DatabricksSource{}).processResults(context.Background(), resp, target, maxBatchBytes, results)
	}()
	var batches []arrow.RecordBatch
	for res := range results {
		if res.Err != nil {
			for _, b := range batches {
				b.Release()
			}
			return nil, res.Err
		}
		batches = append(batches, res.Batch)
	}
	return batches, nil
}

func jsonCell(arr arrow.Array, i int) string {
	return arr.(array.ExtensionArray).Storage().(*array.String).Value(i)
}

func TestProcessResultsConformsArrowTypes(t *testing.T) {
	src := arrow.NewSchema([]arrow.Field{
		{Name: "ti", Type: arrow.PrimitiveTypes.Int8, Nullable: true},
		{Name: "n", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "Etc/UTC"}, Nullable: true},
		{Name: "arr", Type: arrow.ListOfField(arrow.Field{Name: "element", Type: arrow.PrimitiveTypes.Int32, Nullable: true}), Nullable: true},
		{Name: "m", Type: arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int32), Nullable: true},
		{Name: "st", Type: arrow.StructOf(
			arrow.Field{Name: "b", Type: arrow.PrimitiveTypes.Int32},
			arrow.Field{Name: "a", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		), Nullable: true},
	}, nil)
	rec := recordFromJSON(t, src, `[
		{"ti": 7, "n": 1, "s": "", "ts": "2024-01-02T03:04:05.123456", "arr": [1, null], "m": [{"key": "k", "value": 1}], "st": {"b": 2, "a": ["x"]}},
		{"ti": null, "n": null, "s": null, "ts": null, "arr": null, "m": null, "st": null}
	]`)
	defer rec.Release()
	server := arrowStreamServer(t, rec)
	defer server.Close()

	var columns []schema.Column
	for i, typ := range []string{"TINYINT", "INT", "STRING", "TIMESTAMP", "ARRAY<INT>", "MAP<STRING, INT>", "STRUCT<b: INT, a: ARRAY<STRING>>"} {
		dt, p, sc, at := MapDatabricksToDataType(typ)
		columns = append(columns, schema.Column{Name: src.Field(i).Name, DataType: dt, Precision: p, Scale: sc, ArrayType: at, Nullable: true})
	}
	target := buildArrowSchema(columns)

	batches, err := collect(t, &dbsql.StatementResponse{Result: &dbsql.ResultData{
		ExternalLinks: []dbsql.ExternalLink{{ExternalLink: server.URL}},
	}}, target, 0)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	out := batches[0]
	defer out.Release()

	require.True(t, out.Schema().Equal(target))
	require.Equal(t, int64(2), out.NumRows())

	assert.Equal(t, int8(7), out.Column(0).(*array.Int8).Value(0))
	assert.Equal(t, int32(1), out.Column(1).(*array.Int32).Value(0))
	assert.Equal(t, "", out.Column(2).(*array.String).Value(0))
	assert.False(t, out.Column(2).IsNull(0))
	ts := out.Column(3).(*array.Timestamp).Value(0)
	assert.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC).UnixMicro(), int64(ts))
	assert.Equal(t, "[1,null]", out.Column(4).ValueStr(0))
	assert.JSONEq(t, `{"k":1}`, jsonCell(out.Column(5), 0))
	assert.Equal(t, `{"b":2,"a":["x"]}`, jsonCell(out.Column(6), 0))

	for i := 0; i < int(out.NumCols()); i++ {
		assert.True(t, out.Column(i).IsNull(1), "column %d must keep NULL", i)
	}
}

func TestProcessResultsByteCap(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	const rowCount = 60
	rows := make([]string, rowCount)
	for i := range rows {
		rows[i] = `{"a": "0123456789", "b": "0123456789"}`
	}
	rec := recordFromJSON(t, sc, "["+strings.Join(rows, ",")+"]")
	defer rec.Release()
	server := arrowStreamServer(t, rec)
	defer server.Close()

	for _, tc := range []struct {
		name          string
		maxBatchBytes int64
		wantMin       int
		wantMax       int
	}{
		{"cap disabled", 0, 1, 1},
		{"small cap", 200, 2, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batches, err := collect(t, &dbsql.StatementResponse{Result: &dbsql.ResultData{
				ExternalLinks: []dbsql.ExternalLink{{ExternalLink: server.URL}},
			}}, sc, tc.maxBatchBytes)
			require.NoError(t, err)

			total := int64(0)
			for _, b := range batches {
				total += b.NumRows()
				b.Release()
			}
			assert.Equal(t, int64(rowCount), total, "all rows must be preserved")
			assert.GreaterOrEqual(t, len(batches), tc.wantMin)
			assert.LessOrEqual(t, len(batches), tc.wantMax)
		})
	}
}

func TestProcessResultsSkipsRepeatedChunks(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	rec := recordFromJSON(t, sc, `[{"a": 1}, {"a": 2}]`)
	defer rec.Release()
	server := arrowStreamServer(t, rec)
	defer server.Close()

	batches, err := collect(t, &dbsql.StatementResponse{Result: &dbsql.ResultData{
		ExternalLinks: []dbsql.ExternalLink{{ExternalLink: server.URL, ChunkIndex: 0}, {ExternalLink: server.URL, ChunkIndex: 0}},
	}}, sc, 0)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	batches[0].Release()
}

func TestProcessResultsExternalLinkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := collect(t, &dbsql.StatementResponse{Result: &dbsql.ResultData{
		ExternalLinks: []dbsql.ExternalLink{{ExternalLink: server.URL}},
	}}, nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "external link returned status 500")
}

func TestDownloadExternalLinkSendsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-amz-server-side-encryption-customer-key") != "secret-key" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`[["a"]]`))
	}))
	defer server.Close()

	data, err := downloadExternalLink(context.Background(), dbsql.ExternalLink{
		ExternalLink: server.URL,
		HttpHeaders:  map[string]string{"x-amz-server-side-encryption-customer-key": "secret-key"},
	})
	require.NoError(t, err)
	assert.Equal(t, `[["a"]]`, string(data))
}

func TestDownloadExternalLinkErrorOmitsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	link := server.URL + "/chunk?X-Databricks-Signature=topsecret"
	server.Close()

	_, err := downloadExternalLink(context.Background(), dbsql.ExternalLink{ExternalLink: link})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")
	assert.NotContains(t, err.Error(), server.URL)
}

func TestReadFetchesAllExternalLinkChunks(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	rec0 := recordFromJSON(t, sc, `[{"id": 1, "name": "a"}, {"id": 2, "name": "b"}]`)
	defer rec0.Release()
	rec1 := recordFromJSON(t, sc, `[{"id": 3, "name": "c"}]`)
	defer rec1.Release()
	chunk0, chunk1 := arrowIPC(t, rec0), arrowIPC(t, rec1)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.TrimSuffix(r.URL.Path, "/") == "/api/2.0/sql/statements":
			_, _ = fmt.Fprintf(w, `{"statement_id":"s1","status":{"state":"SUCCEEDED"},
				"manifest":{"total_chunk_count":2},
				"result":{"external_links":[{"chunk_index":0,"external_link":"%s/chunk0"}]}}`, server.URL)
		case r.URL.Path == "/api/2.0/sql/statements/s1/result/chunks/1":
			_, _ = fmt.Fprintf(w, `{"external_links":[{"chunk_index":1,"external_link":"%s/chunk1"}]}`, server.URL)
		case r.URL.Path == "/chunk0":
			_, _ = w.Write(chunk0)
		case r.URL.Path == "/chunk1":
			_, _ = w.Write(chunk1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := databricks.NewWorkspaceClient(&databricks.Config{Host: server.URL, Token: "test"})
	require.NoError(t, err)
	s := &DatabricksSource{client: client, httpPath: "/sql/1.0/warehouses/w1", catalog: "main", schemaName: "default"}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}}

	results, err := s.read(context.Background(), "t", tableSchema, source.ReadOptions{})
	require.NoError(t, err)

	var ids []int64
	for res := range results {
		require.NoError(t, res.Err)
		col := res.Batch.Column(0).(*array.Int64)
		for i := 0; i < col.Len(); i++ {
			ids = append(ids, col.Value(i))
		}
		res.Batch.Release()
	}
	assert.Equal(t, []int64{1, 2, 3}, ids)
}

func TestDownloadExternalLinkStallTimeout(t *testing.T) {
	orig := externalLinkStallTimeout
	externalLinkStallTimeout = 100 * time.Millisecond
	defer func() { externalLinkStallTimeout = orig }()

	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyz0123456789")
	for _, tc := range []struct {
		name    string
		handler func(w http.ResponseWriter, release <-chan struct{})
		wantErr bool
	}{
		{"no response", func(w http.ResponseWriter, release <-chan struct{}) { <-release }, true},
		{"stalls mid-body", func(w http.ResponseWriter, release <-chan struct{}) {
			_, _ = w.Write(payload[:10])
			w.(http.Flusher).Flush()
			<-release
		}, true},
		{"slow but steady", func(w http.ResponseWriter, release <-chan struct{}) {
			for i := range payload {
				_, _ = w.Write(payload[i : i+1])
				w.(http.Flusher).Flush()
				time.Sleep(10 * time.Millisecond)
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.handler(w, release)
			}))
			defer server.Close()
			defer close(release)

			start := time.Now()
			data, err := downloadExternalLink(context.Background(), dbsql.ExternalLink{ExternalLink: server.URL})
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no data received")
				assert.Less(t, time.Since(start), 2*time.Second)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, payload, data)
			assert.Greater(t, time.Since(start), externalLinkStallTimeout)
		})
	}
}

func TestDownloadExternalLinkRetriesTransientErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  int
		status    int
		wantErr   bool
		wantCalls int
	}{
		{"recovers after 500s", 2, http.StatusInternalServerError, false, 3},
		{"gives up after max attempts", 5, http.StatusServiceUnavailable, true, externalLinkAttempts},
		{"does not retry 403", 5, http.StatusForbidden, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls <= tc.failures {
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte("ok"))
			}))
			defer server.Close()

			data, err := downloadExternalLink(context.Background(), dbsql.ExternalLink{ExternalLink: server.URL})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "ok", string(data))
			}
			assert.Equal(t, tc.wantCalls, calls)
		})
	}
}

func TestConformArrayListElements(t *testing.T) {
	src := arrow.NewSchema([]arrow.Field{
		{Name: "bins", Type: arrow.ListOf(arrow.BinaryTypes.Binary), Nullable: true},
		{Name: "tss", Type: arrow.ListOf(&arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "Etc/UTC"}), Nullable: true},
	}, nil)
	rec := recordFromJSON(t, src, `[{"bins": ["AAA=", null], "tss": ["2024-01-02T03:04:05.123456"]}, {"bins": null, "tss": null}]`)
	defer rec.Release()

	var columns []schema.Column
	for i, typ := range []string{"ARRAY<BINARY>", "ARRAY<TIMESTAMP>"} {
		dt, p, sc, at := MapDatabricksToDataType(typ)
		columns = append(columns, schema.Column{Name: src.Field(i).Name, DataType: dt, Precision: p, Scale: sc, ArrayType: at, Nullable: true})
	}
	target := buildArrowSchema(columns)

	out, err := conformRecord(memory.NewGoAllocator(), rec, target, nil)
	require.NoError(t, err)
	defer out.Release()
	require.True(t, out.Schema().Equal(target))

	bins := out.Column(0).(*array.List)
	binValues := bins.ListValues().(*array.Binary)
	assert.Equal(t, []byte{0, 0}, binValues.Value(0))
	assert.True(t, binValues.IsNull(1))
	assert.True(t, bins.IsNull(1))

	tss := out.Column(1).(*array.List)
	assert.Equal(t, time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC).UnixMicro(), int64(tss.ListValues().(*array.Timestamp).Value(0)))
	assert.True(t, tss.IsNull(1))
}

func TestProcessResultsFollowsNextChunkLinkWithoutManifest(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	rec0 := recordFromJSON(t, sc, `[{"id": 1}]`)
	defer rec0.Release()
	rec1 := recordFromJSON(t, sc, `[{"id": 2}]`)
	defer rec1.Release()
	chunk0, chunk1 := arrowIPC(t, rec0), arrowIPC(t, rec1)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/2.0/sql/statements/s1/result/chunks/1":
			_, _ = fmt.Fprintf(w, `{"external_links":[{"chunk_index":1,"external_link":"%s/chunk1"}]}`, server.URL)
		case "/chunk0":
			_, _ = w.Write(chunk0)
		case "/chunk1":
			_, _ = w.Write(chunk1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := databricks.NewWorkspaceClient(&databricks.Config{Host: server.URL, Token: "test"})
	require.NoError(t, err)
	resp := &dbsql.StatementResponse{StatementId: "s1", Result: &dbsql.ResultData{
		ExternalLinks: []dbsql.ExternalLink{{
			ChunkIndex:            0,
			ExternalLink:          server.URL + "/chunk0",
			NextChunkIndex:        1,
			NextChunkInternalLink: "/api/2.0/sql/statements/s1/result/chunks/1",
		}},
	}}

	results := make(chan source.RecordBatchResult)
	go func() {
		defer close(results)
		(&DatabricksSource{client: client}).processResults(context.Background(), resp, sc, 0, results)
	}()
	var ids []int64
	for res := range results {
		require.NoError(t, res.Err)
		col := res.Batch.Column(0).(*array.Int64)
		for i := 0; i < col.Len(); i++ {
			ids = append(ids, col.Value(i))
		}
		res.Batch.Release()
	}
	assert.Equal(t, []int64{1, 2}, ids)
}

func TestFormatIntervals(t *testing.T) {
	for _, tc := range []struct {
		qualifier string
		micros    int64
		want      string
	}{
		{"DAY", 86_400_000_000, "INTERVAL '1' DAY"},
		{"DAY", -3 * 86_400_000_000, "INTERVAL '-3' DAY"},
		{"HOUR", 26 * 3_600_000_000, "INTERVAL '26' HOUR"},
		{"MINUTE", -90 * 60_000_000, "INTERVAL '-90' MINUTE"},
		{"SECOND", 4_500_000, "INTERVAL '04.5' SECOND"},
		{"SECOND", -1, "INTERVAL '-00.000001' SECOND"},
		{"SECOND", 75_000_000, "INTERVAL '75' SECOND"},
		{"DAY TO HOUR", 93_600_000_000, "INTERVAL '1 02' DAY TO HOUR"},
		{"DAY TO MINUTE", 93_780_000_000, "INTERVAL '1 02:03' DAY TO MINUTE"},
		{"DAY TO SECOND", 93_784_500_000, "INTERVAL '1 02:03:04.5' DAY TO SECOND"},
		{"DAY TO SECOND", -93_784_123_456, "INTERVAL '-1 02:03:04.123456' DAY TO SECOND"},
		{"DAY TO SECOND", 0, "INTERVAL '0 00:00:00' DAY TO SECOND"},
		{"HOUR TO MINUTE", 93_780_000_000, "INTERVAL '26:03' HOUR TO MINUTE"},
		{"HOUR TO SECOND", 93_784_500_000, "INTERVAL '26:03:04.5' HOUR TO SECOND"},
		{"MINUTE TO SECOND", 5_404_500_000, "INTERVAL '90:04.5' MINUTE TO SECOND"},
		{"MINUTE TO SECOND", -5_404_000_000, "INTERVAL '-90:04' MINUTE TO SECOND"},
	} {
		assert.Equal(t, tc.want, formatDayTimeInterval(tc.micros, tc.qualifier))
	}
	for _, tc := range []struct {
		qualifier string
		months    int64
		want      string
	}{
		{"YEAR", 12, "INTERVAL '1' YEAR"},
		{"YEAR", -24, "INTERVAL '-2' YEAR"},
		{"MONTH", 15, "INTERVAL '15' MONTH"},
		{"YEAR TO MONTH", 14, "INTERVAL '1-2' YEAR TO MONTH"},
		{"YEAR TO MONTH", -14, "INTERVAL '-1-2' YEAR TO MONTH"},
		{"YEAR TO MONTH", 0, "INTERVAL '0-0' YEAR TO MONTH"},
	} {
		assert.Equal(t, tc.want, formatYearMonthInterval(tc.months, tc.qualifier))
	}

	sc := arrow.NewSchema([]arrow.Field{
		{Name: "dt", Type: arrow.FixedWidthTypes.Duration_us, Nullable: true},
		{Name: "ym", Type: arrow.FixedWidthTypes.MonthInterval, Nullable: true},
		{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	rec := recordFromJSON(t, sc, `[{"dt": 86400000000, "ym": {"months": 14}, "n": 1}, {"dt": null, "ym": null, "n": 2}]`)
	defer rec.Release()
	out := formatIntervals(memory.NewGoAllocator(), rec, []string{"DAY", "YEAR TO MONTH", ""})
	defer out.Release()
	assert.Equal(t, "INTERVAL '1' DAY", out.Column(0).(*array.String).Value(0))
	assert.Equal(t, "INTERVAL '1-2' YEAR TO MONTH", out.Column(1).(*array.String).Value(0))
	assert.True(t, out.Column(0).IsNull(1))
	assert.True(t, out.Column(1).IsNull(1))
	assert.Equal(t, int64(2), out.Column(2).(*array.Int64).Value(1))
}
