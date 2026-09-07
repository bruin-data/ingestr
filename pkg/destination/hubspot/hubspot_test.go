package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedRequest struct {
	path   string
	inputs []batchInput
}

// newBatchServer captures every batch request and replies with success.
func newBatchServer(t *testing.T) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer pat-eu1-token", r.Header.Get("Authorization"))

		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body struct {
			Inputs []batchInput `json:"inputs"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))

		mu.Lock()
		reqs = append(reqs, capturedRequest{path: r.URL.Path, inputs: body.Inputs})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(server.Close)
	return server, &reqs
}

func connectTestDestination(t *testing.T, serverURL string) *HubSpotDestination {
	t.Helper()
	uri := "hubspot://?api_key=pat-eu1-token&endpoint=" + url.QueryEscape(serverURL)
	d := NewHubSpotDestination()
	require.NoError(t, d.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	return d
}

func contactBatch() arrow.RecordBatch {
	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "firstname", Type: arrow.BinaryTypes.String},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"hasan@x.com", "ali@x.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"hasan", "ali"}, nil)
	b.Field(2).(*array.Int64Builder).AppendValues([]int64{25, 30}, nil)
	return b.NewRecordBatch()
}

func TestUpsertContacts(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/contacts/batch/upsert", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 2)
	assert.Equal(t, "email", inputs[0].IDProperty)
	assert.Equal(t, "hasan@x.com", inputs[0].ID)
	assert.Equal(t, map[string]string{"firstname": "hasan", "age": "25"}, inputs[0].Properties)
	// The id column is not duplicated into properties.
	assert.NotContains(t, inputs[0].Properties, "email")
}

func TestUpdateRoutesToBatchUpdate(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "record_id", Type: arrow.BinaryTypes.String},
		{Name: "firstname", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"12345"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"Ann"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?id_property=hs_object_id&id_column=record_id"}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/companies/batch/update", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 1)
	// Update matches on the record id alone: id set, no idProperty.
	assert.Equal(t, "12345", inputs[0].ID)
	assert.Empty(t, inputs[0].IDProperty)
	assert.NotContains(t, inputs[0].Properties, "record_id")
	assert.Equal(t, "Ann", inputs[0].Properties["firstname"])
}

func TestAssociationDefaultRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/crm/v4/associations/contacts/companies/batch/associate/default", r.URL.Path)
		var body struct{ Inputs []associationInput }
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Len(t, body.Inputs, 1)
		assert.Equal(t, "111", body.Inputs[0].From.ID)
		assert.Equal(t, "222", body.Inputs[0].To.ID)
		assert.Empty(t, body.Inputs[0].Types)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "contact_id", Type: arrow.BinaryTypes.String},
		{Name: "company_id", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"111"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"222"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=companies&from_id_column=contact_id&to_id_column=company_id"}))
}

func TestAssociationLabeledRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/crm/v4/associations/contacts/companies/batch/create", r.URL.Path)
		var body struct{ Inputs []associationInput }
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Len(t, body.Inputs, 1)
		require.Len(t, body.Inputs[0].Types, 1)
		assert.Equal(t, 279, body.Inputs[0].Types[0].AssociationTypeID)
		assert.Equal(t, "HUBSPOT_DEFINED", body.Inputs[0].Types[0].AssociationCategory)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "contact_id", Type: arrow.BinaryTypes.String},
		{Name: "company_id", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"111"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"222"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=companies&from_id_column=contact_id&to_id_column=company_id&association_type=279"}))
}

func TestAssociationRequiresColumns(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=companies"})
	require.ErrorContains(t, err, "from_id_column and to_id_column")
}

func TestAssociationRequiresFromTo(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from_id_column=a&to_id_column=b"})
	require.ErrorContains(t, err, "from= and to=")
}

func TestCreateWhenNoIDProperty(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts"}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/contacts/batch/create", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 2)
	assert.Empty(t, inputs[0].IDProperty)
	assert.Empty(t, inputs[0].ID)
	// Without an id_property every column becomes a property, including email.
	assert.Equal(t, "hasan@x.com", inputs[0].Properties["email"])
}

func TestIDColumnDiffersFromProperty(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "user_email", Type: arrow.BinaryTypes.String},
		{Name: "firstname", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"A"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email&id_column=user_email"}))

	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 1)
	assert.Equal(t, "email", inputs[0].IDProperty)
	assert.Equal(t, "a@x.com", inputs[0].ID)
	assert.NotContains(t, inputs[0].Properties, "user_email")
}

func TestBatchChunking(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{{Name: "email", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	emails := make([]string, 250)
	for i := range emails {
		emails[i] = "u" + strings.Repeat("x", 0) + string(rune('a'+i%26)) + "@x.com"
	}
	b.Field(0).(*array.StringBuilder).AppendValues(emails, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"}))

	require.Len(t, *reqs, 3) // 100 + 100 + 50
	assert.Len(t, (*reqs)[0].inputs, 100)
	assert.Len(t, (*reqs)[2].inputs, 50)
}

func TestNullIDRowsSkipped(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "firstname", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com", ""}, []bool{true, false})
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"A", "B"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"}))

	require.Len(t, *reqs, 1)
	assert.Len(t, (*reqs)[0].inputs, 1)
}

func TestMissingIDColumnFailsFast(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=missing_col"})
	require.ErrorContains(t, err, "id column \"missing_col\" not found")
}

func TestRejectedRecordsFailByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","numErrors":1,"errors":[{"status":"error","category":"VALIDATION_ERROR","message":"Property values were not valid: email","context":{"email":["bad"]}}]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"})
	require.ErrorContains(t, err, "hubspot rejected 1 contacts record(s)")
	require.ErrorContains(t, err, "VALIDATION_ERROR")
}

func TestRejectedRecordsSkippedWithOnErrorSkip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","numErrors":1,"errors":[{"category":"VALIDATION_ERROR","message":"bad"}]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email&on_error=skip"}))
}

func TestHardFailureStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"invalid token"}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"})
	require.ErrorContains(t, err, "status 401")
}

func TestAuthFailureAbortsEvenWithOnErrorSkip(t *testing.T) {
	// Auth failures are not record-level; on_error=skip must not swallow them.
	// (429/5xx are also non-record-level but get retried by the client, so this
	// test uses the non-retried auth codes.)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"message":"nope"}`)
		}))
		d := connectTestDestination(t, server.URL)

		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: contactBatch()}
		close(records)
		err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email&on_error=skip"})
		require.Errorf(t, err, "status %d should abort even with on_error=skip", status)
		server.Close()
	}
}

func TestTimestampPreservesMicroseconds(t *testing.T) {
	ts := arrow.Timestamp(1_700_000_000_123_456) // microseconds since epoch
	b := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Microsecond})
	defer b.Release()
	b.Append(ts)
	arr := b.NewArray()
	defer arr.Release()

	v, ok := propertyValue(arr, 0)
	require.True(t, ok)
	assert.Contains(t, v, ".123456", "microsecond precision should be preserved, got %q", v)
}

func TestStrategySupport(t *testing.T) {
	d := NewHubSpotDestination()
	assert.True(t, d.SupportsAppendStrategy())
	assert.True(t, d.SupportsReplaceStrategy())
	assert.False(t, d.SupportsMergeStrategy())
	assert.False(t, d.SupportsDeleteInsertStrategy())
	assert.False(t, d.SupportsSCD2Strategy())
	assert.False(t, d.SupportsAtomicSwap())
}

func TestInvalidURI(t *testing.T) {
	d := NewHubSpotDestination()
	require.ErrorContains(t, d.Connect(context.Background(), "hubspot://"), "api_key or service_key is required")
	require.ErrorContains(t, d.Connect(context.Background(), "postgres://x"), "must start with hubspot://")
}

func TestIDColumnRequiresIDProperty(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_column=email"})
	require.ErrorContains(t, err, "id_column requires id_property")
}

// newPropertyServer answers the property batch-read endpoint, echoing back only
// the requested names that exist in props, so PrepareTable validation can be
// exercised offline.
func newPropertyServer(t *testing.T, props []string) *httptest.Server {
	t.Helper()
	known := make(map[string]bool, len(props))
	for _, p := range props {
		known[p] = true
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/batch/read") {
			var req struct {
				Inputs []struct {
					Name string `json:"name"`
				} `json:"inputs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			var results []string
			for _, in := range req.Inputs {
				if known[in.Name] {
					results = append(results, fmt.Sprintf(`{"name":%q}`, in.Name))
				}
			}
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[`+strings.Join(results, ",")+`]}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func schemaWithColumns(names ...string) *schema.TableSchema {
	cols := make([]schema.Column, len(names))
	for i, n := range names {
		cols[i] = schema.Column{Name: n}
	}
	return &schema.TableSchema{Columns: cols}
}

func TestPrepareTableRejectsUnknownProperty(t *testing.T) {
	server := newPropertyServer(t, []string{"email", "firstname"})
	d := connectTestDestination(t, server.URL)

	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts",
		Schema: schemaWithColumns("email", "firstname", "favorite_color"),
	})
	require.ErrorContains(t, err, "favorite_color")
	require.ErrorContains(t, err, "not properties")
}

func TestPrepareTableChunksLargeSchema(t *testing.T) {
	// >100 columns must be validated in multiple batch/read requests, not one
	// oversized request that HubSpot would reject.
	var calls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		var req struct {
			Inputs []struct {
				Name string `json:"name"`
			} `json:"inputs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		require.LessOrEqual(t, len(req.Inputs), 100, "each batch/read must be <= 100 inputs")
		var results []string
		for _, in := range req.Inputs {
			results = append(results, fmt.Sprintf(`{"name":%q}`, in.Name))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[`+strings.Join(results, ",")+`]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	cols := make([]string, 150)
	for i := range cols {
		cols[i] = fmt.Sprintf("col_%d", i)
	}
	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts",
		Schema: schemaWithColumns(cols...),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "150 columns should be validated in 2 chunked requests")
}

func TestPrepareTablePassesForKnownProperties(t *testing.T) {
	server := newPropertyServer(t, []string{"email", "firstname"})
	d := connectTestDestination(t, server.URL)

	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts",
		Schema: schemaWithColumns("email", "firstname"),
	}))
}

func TestPrepareTableIgnoresIDColumnAndDecorations(t *testing.T) {
	server := newPropertyServer(t, []string{"email", "firstname"})
	d := connectTestDestination(t, server.URL)

	// user_email is the id column and the _ingestr_* columns are decorations;
	// none of them need to exist as HubSpot properties.
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts?id_property=email&id_column=user_email",
		Schema: schemaWithColumns("user_email", "firstname", "_ingestr_loaded_at", "_ingestr_run_id"),
	}))
}

func TestPrepareTableAllowsNonUniqueIDProperty(t *testing.T) {
	// domain exists on companies but is not a unique-value identifier. Because the
	// hasUniqueValue flag is unreliable, PrepareTable warns rather than blocking;
	// HubSpot rejects at write time if it truly isn't upsertable.
	server := newPropertyServer(t, []string{"domain", "name"})
	d := connectTestDestination(t, server.URL)

	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "companies?id_property=domain&id_column=company_domain",
		Schema: schemaWithColumns("company_domain", "name"),
	})
	require.NoError(t, err)
}

func TestPrepareTableUpdateModeSkipsUniqueCheck(t *testing.T) {
	// hs_object_id routes to update-by-id and needs no unique property.
	server := newPropertyServer(t, []string{"name", "city"})
	d := connectTestDestination(t, server.URL)

	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "companies?id_property=hs_object_id&id_column=company_id",
		Schema: schemaWithColumns("company_id", "name", "city"),
	}))
}

func TestPrepareTableValidatesIDProperty(t *testing.T) {
	server := newPropertyServer(t, []string{"firstname"})
	d := connectTestDestination(t, server.URL)

	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts?id_property=email&id_column=user_email",
		Schema: schemaWithColumns("user_email", "firstname"),
	})
	require.ErrorContains(t, err, "id_property")
}

func TestPrepareTableSkipsWhenSchemaNil(t *testing.T) {
	server := newPropertyServer(t, nil)
	d := connectTestDestination(t, server.URL)

	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{Table: "contacts"}))
}

func TestPrepareTableBestEffortOnFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "contacts",
		Schema: schemaWithColumns("email", "favorite_color"),
	}))
}

func conflictServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"status":"error","category":"CONFLICT","message":"Contact already exists. Existing ID: 123"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCreateConflictFailsWithHint(t *testing.T) {
	d := connectTestDestination(t, conflictServer(t).URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts"})
	require.ErrorContains(t, err, "already exists")
	require.ErrorContains(t, err, "id_property")
}

func TestCreateConflictSkippedWithOnErrorSkip(t *testing.T) {
	d := connectTestDestination(t, conflictServer(t).URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?on_error=skip"}))
}

func TestValidationErrorWithErrorsArraySkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"status":"error","category":"VALIDATION_ERROR","message":"invalid","errors":[{"category":"INVALID_EMAIL","message":"bad email"}]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email&on_error=skip"}))
}
