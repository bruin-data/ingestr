package google_sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type sheetsRequest struct {
	method, path, body, response string
	query                        url.Values
	status                       int
}

func sheetsHTTPDestination(t *testing.T, requests []sheetsRequest) *GoogleSheetsDestination {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		if !assert.Less(t, i, len(requests), "unexpected request: %s %s", r.Method, r.URL) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		want := requests[i]
		assert.Equal(t, want.method, r.Method)
		assert.Equal(t, want.path, r.URL.Path)
		query := r.URL.Query()
		query.Del("alt")
		query.Del("prettyPrint")
		if want.query == nil {
			assert.Empty(t, query)
		} else {
			assert.Equal(t, want.query, query)
		}
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		if want.body == "" {
			assert.Empty(t, body)
		} else {
			assert.JSONEq(t, want.body, string(body))
			assert.Contains(t, r.Header.Get("Content-Type"), "application/json")
		}
		w.Header().Set("Content-Type", "application/json")
		if want.status != 0 {
			w.WriteHeader(want.status)
		}
		_, err = io.WriteString(w, want.response)
		assert.NoError(t, err)
	}))
	t.Cleanup(func() {
		server.Close()
		assert.Equal(t, int64(len(requests)), calls.Load(), "request count")
	})
	client, err := sheets.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	return &GoogleSheetsDestination{client: client}
}

const (
	spreadsheetPath = "/v4/spreadsheets/book-123"
	valuesPath      = spreadsheetPath + "/values/'O''Brien Sales'"
	existingSheet   = `{"sheets":[{"properties":{"sheetId":0,"title":"O'Brien Sales"}}]}`
)

func TestPrepareTableHTTP(t *testing.T) {
	get := sheetsRequest{method: "GET", path: spreadsheetPath, response: existingSheet}
	clear := sheetsRequest{method: "POST", path: valuesPath + ":clear", body: `{}`, response: `{}`}
	header := sheetsRequest{method: "PUT", path: valuesPath + "!A1", body: `{"values":[["id","name"]]}`, query: url.Values{"valueInputOption": {"RAW"}}, response: `{"updatedRows":1}`}
	read := sheetsRequest{method: "GET", path: valuesPath, response: `{}`}
	create := sheetsRequest{method: "POST", path: spreadsheetPath + ":batchUpdate", body: `{"requests":[{"addSheet":{"properties":{"title":"O'Brien Sales"}}}]}`, response: `{"replies":[{"addSheet":{"properties":{"sheetId":42}}}]}`}
	for _, tc := range []struct {
		name     string
		replace  bool
		requests []sheetsRequest
	}{
		{"replace", true, []sheetsRequest{get, clear, header}},
		{"append empty", false, []sheetsRequest{get, read, header}},
		{"append populated", false, []sheetsRequest{get, {method: "GET", path: valuesPath, response: `{"values":[["id","name"],[7,"old"]]}`}}},
		{"create tab", false, []sheetsRequest{{method: "GET", path: spreadsheetPath, response: `{"sheets":[{"properties":{"title":"Other"}}]}`}, create, read, header}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := destination.PrepareOptions{Table: "book-123.O'Brien Sales", DropFirst: tc.replace, Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}, {Name: "name", DataType: schema.TypeString}}}}
			t.Run("success", func(t *testing.T) {
				d := sheetsHTTPDestination(t, tc.requests)
				require.NoError(t, d.PrepareTable(context.Background(), opts))
			})
			for i := range tc.requests {
				t.Run(fmt.Sprintf("error at request %d", i+1), func(t *testing.T) {
					requests := append([]sheetsRequest(nil), tc.requests[:i+1]...)
					requests[i].status = http.StatusForbidden
					requests[i].response = `{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`
					d := sheetsHTTPDestination(t, requests)
					err := d.PrepareTable(context.Background(), opts)
					assertSheetsAPIError(t, err, "failed to", http.StatusForbidden)
				})
			}
		})
	}
}

func assertSheetsAPIError(t *testing.T, err error, message string, code int) {
	t.Helper()
	require.ErrorContains(t, err, message)
	var apiErr *googleapi.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, code, apiErr.Code)
	assert.Equal(t, "permission denied", apiErr.Message)
}

func TestWriteHTTP(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
			defer pool.AssertSize(t, 0)
			arrowSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "text", Type: arrow.BinaryTypes.String, Nullable: true}, {Name: "active", Type: arrow.FixedWidthTypes.Boolean}}, nil)
			builder := array.NewRecordBuilder(pool, arrowSchema)
			defer builder.Release()
			var rows [][]interface{}
			for i := 0; i < 5001; i++ {
				builder.Field(0).(*array.Int64Builder).Append(int64(i + 10))
				text := fmt.Sprintf("=SUM(A%d:B%d)", i, i+3)
				if i%3 == 0 {
					builder.Field(1).AppendNull()
					text = ""
				} else {
					builder.Field(1).(*array.StringBuilder).Append(text)
				}
				builder.Field(2).(*array.BooleanBuilder).Append(i%2 == 0)
				rows = append(rows, []interface{}{i + 10, text, i%2 == 0})
			}
			batch := builder.NewRecordBatch()
			empty := builder.NewRecordBatch()
			requests := make([]sheetsRequest, 0, 2)
			for _, chunk := range [][][]interface{}{rows[:5000], rows[5000:]} {
				body, err := json.Marshal(map[string]interface{}{"values": chunk})
				require.NoError(t, err)
				requests = append(requests, sheetsRequest{method: "POST", path: valuesPath + ":append", body: string(body), query: url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}, response: `{"updates":{"updatedRows":1}}`})
			}
			d := sheetsHTTPDestination(t, requests)
			d.spreadsheetID, d.sheetName = "book-123", "O'Brien Sales"
			records := make(chan source.RecordBatchResult, 2)
			records <- source.RecordBatchResult{Batch: empty}
			records <- source.RecordBatchResult{Batch: batch}
			close(records)
			write := d.Write
			if parallel {
				write = d.WriteParallel
			}
			require.NoError(t, write(context.Background(), records, destination.WriteOptions{Parallelism: 4}))
		})
	}
}

func TestWriteHTTPErrorStopsChunks(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer pool.AssertSize(t, 0)
	builder := array.NewRecordBuilder(pool, arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil))
	defer builder.Release()
	for i := 0; i < 10001; i++ {
		builder.Field(0).(*array.Int64Builder).Append(7)
	}
	rows := make([][]int, 5000)
	for i := range rows {
		rows[i] = []int{7}
	}
	body, err := json.Marshal(map[string]interface{}{"values": rows})
	require.NoError(t, err)
	request := sheetsRequest{method: "POST", path: valuesPath + ":append", body: string(body), query: url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}, response: `{}`}
	failed := request
	failed.status = http.StatusForbidden
	failed.response = `{"error":{"code":403,"message":"permission denied"}}`
	d := sheetsHTTPDestination(t, []sheetsRequest{request, failed})
	d.spreadsheetID, d.sheetName = "book-123", "O'Brien Sales"
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: builder.NewRecordBatch()}
	close(records)
	err = d.Write(context.Background(), records, destination.WriteOptions{})
	assertSheetsAPIError(t, err, `failed to write batch 1: failed to append rows to sheet "O'Brien Sales"`, http.StatusForbidden)
}

func TestDropTableHTTP(t *testing.T) {
	for _, tc := range []struct {
		name     string
		requests []sheetsRequest
	}{
		{"missing", []sheetsRequest{{method: "GET", path: spreadsheetPath, response: `{"sheets":[{"properties":{"title":"Other"}}]}`}}},
		{"last tab", []sheetsRequest{{method: "GET", path: spreadsheetPath, response: existingSheet}, {method: "POST", path: valuesPath + ":clear", body: `{}`, response: `{}`}}},
		{"delete zero ID", []sheetsRequest{{method: "GET", path: spreadsheetPath, response: `{"sheets":[{"properties":{"title":"Other","sheetId":13}},{"properties":{"title":"O'Brien Sales","sheetId":0}}]}`}, {method: "POST", path: spreadsheetPath + ":batchUpdate", body: `{"requests":[{"deleteSheet":{"sheetId":0}}]}`, response: `{"replies":[{}]}`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sheetsHTTPDestination(t, tc.requests)
			require.NoError(t, d.DropTable(context.Background(), "book-123.O'Brien Sales"))
			for i := range tc.requests {
				t.Run(fmt.Sprintf("error at request %d", i+1), func(t *testing.T) {
					requests := append([]sheetsRequest(nil), tc.requests[:i+1]...)
					requests[i].status = http.StatusForbidden
					requests[i].response = `{"error":{"code":403,"message":"permission denied"}}`
					d := sheetsHTTPDestination(t, requests)
					assertSheetsAPIError(t, d.DropTable(context.Background(), "book-123.O'Brien Sales"), "failed to", http.StatusForbidden)
				})
			}
		})
	}
}
