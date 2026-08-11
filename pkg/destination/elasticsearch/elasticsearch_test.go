package elasticsearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestinationWritesArrowRowsAsBulkDocuments(t *testing.T) {
	var callsMu sync.Mutex
	var calls []string
	var bulkBody string
	var bulkContentType string
	var bulkReadErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		defer callsMu.Unlock()

		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodHead && r.URL.Path == "/events":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/events":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/_bulk":
			bulkContentType = r.Header.Get("Content-Type")
			body, err := io.ReadAll(r.Body)
			bulkReadErr = err
			bulkBody = string(body)
			_, _ = io.WriteString(w, `{
				"errors": false,
				"items": [
					{"index": {"status": 201}},
					{"index": {"status": 201}}
				]
			}`)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	d := connectTestDestination(t, server.URL)
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:     "public.events",
		DropFirst: true,
	}))

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: destinationTestBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "public.events"}))

	callsMu.Lock()
	defer callsMu.Unlock()
	require.NoError(t, bulkReadErr)
	assert.Equal(t, "application/x-ndjson", bulkContentType)
	assert.Equal(t, []string{
		"GET /",
		"HEAD /events",
		"DELETE /events",
		"POST /_bulk",
	}, calls)

	// the _bulk wire format requires a terminating newline
	assert.True(t, strings.HasSuffix(bulkBody, "\n"))

	lines := strings.Split(strings.TrimSpace(bulkBody), "\n")
	require.Len(t, lines, 4)
	assert.JSONEq(t, `{"index":{"_index":"events","_id":"user-1"}}`, lines[0])
	assert.JSONEq(t, `{"id":"user-1","name":"Alice","score":10,"active":true}`, lines[1])
	assert.JSONEq(t, `{"index":{"_index":"events","_id":"user-2"}}`, lines[2])
	assert.JSONEq(t, `{"id":"user-2","name":"Bob","score":20,"active":false}`, lines[3])
}

func TestDestinationReturnsBulkItemFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/_bulk" {
			_, _ = io.WriteString(w, `{
				"errors": true,
				"items": [
					{"index": {"status": 201}},
					{"index": {"status": 400, "error": {"reason": "invalid document"}}}
				]
			}`)
			return
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()

	d := connectTestDestination(t, server.URL)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: destinationTestBatch()}
	close(records)

	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "events"})
	require.ErrorContains(t, err, "elasticsearch bulk insert: 1 of 2 documents failed")
}

func connectTestDestination(t *testing.T, serverURL string) *ElasticsearchDestination {
	t.Helper()

	clientURI := strings.Replace(serverURL, "http://", "elasticsearch://", 1) + "?secure=false"
	d := NewElasticsearchDestination()
	require.NoError(t, d.Connect(context.Background(), clientURI))
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	return d
}

func destinationTestBatch() arrow.RecordBatch {
	testSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "score", Type: arrow.PrimitiveTypes.Int64},
		{Name: "active", Type: arrow.FixedWidthTypes.Boolean},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, testSchema)
	defer builder.Release()

	builder.Field(0).(*array.StringBuilder).AppendValues([]string{"user-1", "user-2"}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"Alice", "Bob"}, nil)
	builder.Field(2).(*array.Int64Builder).AppendValues([]int64{10, 20}, nil)
	builder.Field(3).(*array.BooleanBuilder).AppendValues([]bool{true, false}, nil)
	return builder.NewRecordBatch()
}
