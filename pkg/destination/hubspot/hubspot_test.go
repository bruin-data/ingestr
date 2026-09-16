package hubspot

import (
	"bytes"
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
	"github.com/bruin-data/ingestr/internal/output"
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

func companyKeyedBatch() arrow.RecordBatch {
	s := arrow.NewSchema([]arrow.Field{
		{Name: "external_id", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"EXT-1"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"Acme"}, nil)
	return b.NewRecordBatch()
}

func TestPrimaryKeyBecomesUpsertKey(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// companies has no built-in key; a single --primary-key supplies both the
	// id_property and the id column, so it upserts on external_id.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: companyKeyedBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table:       "companies",
		PrimaryKeys: []string{"external_id"},
	}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/companies/batch/upsert", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 1)
	assert.Equal(t, "external_id", inputs[0].IDProperty)
	assert.Equal(t, "EXT-1", inputs[0].ID)
	assert.NotContains(t, inputs[0].Properties, "external_id")
}

func TestDestTableIDPropertyOverridesPrimaryKey(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// An explicit dest-table id_property wins over --primary-key.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table:       "contacts?id_property=email",
		PrimaryKeys: []string{"firstname"},
	}))

	inputs := (*reqs)[0].inputs
	assert.Equal(t, "email", inputs[0].IDProperty)
	assert.Equal(t, "hasan@x.com", inputs[0].ID)
}

func TestCompositePrimaryKeyErrors(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// HubSpot upserts on a single property, so a composite key is an error.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: companyKeyedBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{
		Table:       "companies",
		PrimaryKeys: []string{"external_id", "name"},
	})
	require.ErrorContains(t, err, "composite primary key")
}

func TestCompositePrimaryKeyOverriddenByIDProperty(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// An explicit id_property resolves the ambiguity, so no error.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: companyKeyedBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table:       "companies?id_property=external_id",
		PrimaryKeys: []string{"external_id", "name"},
	}))

	assert.Equal(t, "/crm/v3/objects/companies/batch/upsert", (*reqs)[0].path)
}

func TestCustomObjectNameResolvedToID(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/crm/v3/schemas":
			_, _ = io.WriteString(w, `{"results":[{"objectTypeId":"2-123","name":"building","fullyQualifiedName":"p1_building","labels":{"singular":"Building","plural":"Buildings"}}]}`)
		case "/crm/v3/objects/buildings/batch/create":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"status":"error","message":"Unable to infer object type from: buildings"}`)
		default: // resolved id path
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"Tower A"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "buildings"}))

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, paths, "/crm/v3/objects/buildings/batch/create") // first attempt
	assert.Contains(t, paths, "/crm/v3/schemas")                        // resolution
	assert.Contains(t, paths, "/crm/v3/objects/2-123/batch/create")     // retry with id
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

func TestUpdateNullIDRowsAreCreated(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "record_id", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	// First row has a record id (update); second has none (create, not skipped).
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"12345", ""}, []bool{true, false})
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"Existing", "Brand New"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?id_property=hs_object_id&id_column=record_id"}))

	byPath := map[string][]batchInput{}
	for _, r := range *reqs {
		byPath[r.path] = r.inputs
	}
	require.Contains(t, byPath, "/crm/v3/objects/companies/batch/update")
	require.Contains(t, byPath, "/crm/v3/objects/companies/batch/create")

	upd := byPath["/crm/v3/objects/companies/batch/update"]
	require.Len(t, upd, 1)
	assert.Equal(t, "12345", upd[0].ID)
	assert.Equal(t, "Existing", upd[0].Properties["name"])

	cre := byPath["/crm/v3/objects/companies/batch/create"]
	require.Len(t, cre, 1)
	assert.Empty(t, cre[0].ID)
	assert.Empty(t, cre[0].IDProperty)
	assert.Equal(t, "Brand New", cre[0].Properties["name"])
}

func TestUpdateNotFoundIDsAreCreated(t *testing.T) {
	// Emulates HubSpot: batch update and read fail atomically (empty results +
	// OBJECT_NOT_FOUND) when any id is missing; only "111" exists.
	existing := map[string]bool{"111": true}
	var mu sync.Mutex
	var updateReqs [][]string
	var createInputs []batchInput

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				ID         string            `json:"id"`
				Properties map[string]string `json:"properties"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		ids := make([]string, len(body.Inputs))
		allExist := true
		for i, in := range body.Inputs {
			ids[i] = in.ID
			if in.ID != "" && !existing[in.ID] {
				allExist = false
			}
		}
		notFound := func() {
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[],"errors":[{"category":"OBJECT_NOT_FOUND","message":"missing"}],"numErrors":1}`)
		}

		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/batch/read"):
			if !allExist {
				notFound()
				return
			}
			parts := make([]string, len(ids))
			for i, id := range ids {
				parts[i] = fmt.Sprintf(`{"id":%q}`, id)
			}
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[`+strings.Join(parts, ",")+`]}`)
		case strings.HasSuffix(r.URL.Path, "/batch/update"):
			updateReqs = append(updateReqs, ids)
			if !allExist {
				notFound()
				return
			}
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		case strings.HasSuffix(r.URL.Path, "/batch/create"):
			for _, in := range body.Inputs {
				createInputs = append(createInputs, batchInput{ID: in.ID, Properties: in.Properties})
			}
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "record_id", Type: arrow.BinaryTypes.String},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"111", "999"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"Exists", "Ghost"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?id_property=hs_object_id&id_column=record_id"}))

	mu.Lock()
	defer mu.Unlock()
	// The not-found id was created (id cleared), not left as a failure.
	require.Len(t, createInputs, 1)
	assert.Empty(t, createInputs[0].ID)
	assert.Equal(t, "Ghost", createInputs[0].Properties["name"])
	// The existing id was updated on the retry after partitioning.
	require.NotEmpty(t, updateReqs)
	assert.Equal(t, []string{"111"}, updateReqs[len(updateReqs)-1])
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

func TestAssociationDerivesIDColumns(t *testing.T) {
	// from/to id columns omitted: derived as contact_id / company_id.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/crm/v4/associations/contacts/companies/batch/associate/default", r.URL.Path)
		var body struct{ Inputs []associationInput }
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Len(t, body.Inputs, 1)
		assert.Equal(t, "111", body.Inputs[0].From.ID)
		assert.Equal(t, "222", body.Inputs[0].To.ID)
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
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=companies"}))
}

func TestNullOmittedByDefaultClearedWithFlag(t *testing.T) {
	for _, tc := range []struct {
		name       string
		table      string
		wantInProp bool
	}{
		{"default omits null", "contacts?id_property=email", false},
		{"clear_nulls sends empty", "contacts?id_property=email&clear_nulls=true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var props map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Inputs []batchInput }
				_ = json.NewDecoder(r.Body).Decode(&body)
				require.Len(t, body.Inputs, 1)
				props = body.Inputs[0].Properties
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
			}))
			t.Cleanup(server.Close)
			d := connectTestDestination(t, server.URL)

			s := arrow.NewSchema([]arrow.Field{
				{Name: "email", Type: arrow.BinaryTypes.String},
				{Name: "firstname", Type: arrow.BinaryTypes.String},
			}, nil)
			b := array.NewRecordBuilder(memory.DefaultAllocator, s)
			defer b.Release()
			b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@b.com"}, nil)
			b.Field(1).(*array.StringBuilder).AppendValues([]string{""}, []bool{false}) // null firstname

			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
			close(records)
			require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: tc.table}))

			_, present := props["firstname"]
			assert.Equal(t, tc.wantInProp, present)
			if tc.wantInProp {
				assert.Equal(t, "", props["firstname"])
			}
		})
	}
}

func TestArchiveByRecordID(t *testing.T) {
	var archived []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/crm/v3/objects/companies/batch/archive", r.URL.Path)
		var body struct {
			Inputs []struct {
				ID string `json:"id"`
			} `json:"inputs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, in := range body.Inputs {
			archived = append(archived, in.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{{Name: "company_id", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"111", "222"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?operation=archive&id_column=company_id"}))
	assert.ElementsMatch(t, []string{"111", "222"}, archived)
}

func TestArchiveByBusinessKey(t *testing.T) {
	var archived []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/crm/v3/objects/contacts/batch/read":
			var body struct {
				IDProperty string `json:"idProperty"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "email", body.IDProperty)
			_, _ = io.WriteString(w, `{"results":[{"id":"111","properties":{"email":"a@b.com"}}]}`)
		case "/crm/v3/objects/contacts/batch/archive":
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{{Name: "email", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@b.com"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?operation=archive&id_column=email&id_property=email"}))
	assert.Equal(t, []string{"111"}, archived)
}

func TestArchiveRequiresIDColumn(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)
	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?operation=archive"})
	require.ErrorContains(t, err, "operation=archive requires id_column")
}

func TestInvalidOperation(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)
	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?operation=frobnicate"})
	require.ErrorContains(t, err, "invalid operation")
}

func TestAssociationArchiveRemovesLinks(t *testing.T) {
	var removed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/crm/v4/associations/contacts/companies/batch/archive", r.URL.Path)
		var body struct{ Inputs []associationInput }
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Len(t, body.Inputs, 1)
		assert.Equal(t, "111", body.Inputs[0].From.ID)
		assert.Equal(t, "222", body.Inputs[0].To.ID)
		assert.Empty(t, body.Inputs[0].Types)
		removed = true
		w.WriteHeader(http.StatusNoContent)
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
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=companies&operation=archive"}))
	assert.True(t, removed)
}

func TestAssociationResolvesBusinessKeys(t *testing.T) {
	// from_id_property/to_id_property: the columns hold emails/domains that are
	// resolved to record ids via batch/read before the association is created.
	var associated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/crm/v3/objects/contacts/batch/read":
			var body struct {
				IDProperty string `json:"idProperty"`
				Inputs     []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "email", body.IDProperty)
			require.Len(t, body.Inputs, 1)
			assert.Equal(t, "a@b.com", body.Inputs[0].ID)
			_, _ = io.WriteString(w, `{"results":[{"id":"111","properties":{"email":"a@b.com"}}]}`)
		case "/crm/v3/objects/companies/batch/read":
			var body struct {
				IDProperty string `json:"idProperty"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "domain", body.IDProperty)
			_, _ = io.WriteString(w, `{"results":[{"id":"222","properties":{"domain":"acme.com"}}]}`)
		case "/crm/v4/associations/contacts/companies/batch/associate/default":
			associated = true
			var body struct{ Inputs []associationInput }
			_ = json.NewDecoder(r.Body).Decode(&body)
			require.Len(t, body.Inputs, 1)
			assert.Equal(t, "111", body.Inputs[0].From.ID)
			assert.Equal(t, "222", body.Inputs[0].To.ID)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "contact_email", Type: arrow.BinaryTypes.String},
		{Name: "company_domain", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@b.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"acme.com"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table: "associations?from=contacts&to=companies&from_id_column=contact_email&from_id_property=email&to_id_column=company_domain&to_id_property=domain",
	}))
	assert.True(t, associated)
}

func TestAssociationSkipsUnresolvedBusinessKeys(t *testing.T) {
	// An unresolvable business key (no matching record) skips that row rather
	// than sending a bad association.
	prevOut, prevErr, prevMode := output.Current()
	var buf bytes.Buffer
	output.Init(&buf, &buf, output.ModeText)
	t.Cleanup(func() { output.Init(prevOut, prevErr, prevMode) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/crm/v3/objects/contacts/batch/read":
			// Neither email exists, so read returns an empty result set.
			_, _ = io.WriteString(w, `{"results":[]}`)
		case "/crm/v4/associations/contacts/companies/batch/associate/default":
			t.Fatal("should not associate when no keys resolve")
		default:
			_, _ = io.WriteString(w, `{"results":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "contact_email", Type: arrow.BinaryTypes.String},
		{Name: "company_id", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"missing1@b.com", "missing2@b.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"222", "333"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table: "associations?from=contacts&to=companies&from_id_column=contact_email&from_id_property=email&to_id_column=company_id",
	}))
	assert.Contains(t, buf.String(), "missing id")
}

func TestAssociationWarnsOnIgnoredPrimaryKey(t *testing.T) {
	prevOut, prevErr, prevMode := output.Current()
	var buf bytes.Buffer
	output.Init(&buf, &buf, output.ModeText)
	t.Cleanup(func() { output.Init(prevOut, prevErr, prevMode) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	// A primary key is meaningless for associations: it is ignored with a warning.
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{
		Table:       "associations?from=contacts&to=companies",
		PrimaryKeys: []string{"contact_id"},
	}))

	assert.Contains(t, buf.String(), "associations ignore id_property")
}

func TestAssociationCustomObjectRequiresColumns(t *testing.T) {
	// The custom object is not in the schemas response, so its column can't be
	// derived from a label either and the run asks for explicit columns.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=2-253062429&to=companies"})
	require.ErrorContains(t, err, "from_id_column and to_id_column")
}

func TestAssociationDerivesCustomObjectColumnFromLabel(t *testing.T) {
	// A custom object addressed by objectTypeId derives its column from the
	// singular label ("Building" -> building_id) via the schemas API.
	var associated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/schemas":
			_, _ = io.WriteString(w, `{"results":[{"objectTypeId":"2-253062429","name":"building","fullyQualifiedName":"p1_building","labels":{"singular":"Building","plural":"Buildings"}}]}`)
		case r.URL.Path == "/crm/v4/associations/2-253062429/companies/batch/associate/default":
			associated = true
			var body struct{ Inputs []associationInput }
			_ = json.NewDecoder(r.Body).Decode(&body)
			require.Len(t, body.Inputs, 1)
			assert.Equal(t, "b1", body.Inputs[0].From.ID)
			assert.Equal(t, "c1", body.Inputs[0].To.ID)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "building_id", Type: arrow.BinaryTypes.String},
		{Name: "company_id", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"b1"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"c1"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=2-253062429&to=companies"}))
	assert.True(t, associated)
}

func TestAssociationSameObjectRequiresColumns(t *testing.T) {
	server, _ := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// Both sides derive to contact_id, which would collide.
	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "associations?from=contacts&to=contacts"})
	require.ErrorContains(t, err, "same id column")
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

	// companies has no built-in unique property, so it creates by default.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies"}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/companies/batch/create", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 2)
	assert.Empty(t, inputs[0].IDProperty)
	assert.Empty(t, inputs[0].ID)
	// Without an id_property every column becomes a property, including email.
	assert.Equal(t, "hasan@x.com", inputs[0].Properties["email"])
}

func TestDefaultUpsertForKnownObject(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	// contacts has a built-in writable unique property (email), so it upserts
	// on it by default without the caller setting id_property.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts"}))

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/crm/v3/objects/contacts/batch/upsert", (*reqs)[0].path)
	inputs := (*reqs)[0].inputs
	require.Len(t, inputs, 2)
	assert.Equal(t, "email", inputs[0].IDProperty)
	assert.Equal(t, "hasan@x.com", inputs[0].ID)
	// The id column is not resent as a property.
	assert.NotContains(t, inputs[0].Properties, "email")
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

func TestNullUpsertKeyRowsAreCreated(t *testing.T) {
	server, reqs := newBatchServer(t)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "firstname", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	// First row upserts on email; second has a null email and is created.
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com", ""}, []bool{true, false})
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"A", "B"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "contacts?id_property=email"}))

	byPath := map[string][]batchInput{}
	for _, r := range *reqs {
		byPath[r.path] = r.inputs
	}
	upsert := byPath["/crm/v3/objects/contacts/batch/upsert"]
	require.Len(t, upsert, 1)
	assert.Equal(t, "a@x.com", upsert[0].ID)

	create := byPath["/crm/v3/objects/contacts/batch/create"]
	require.Len(t, create, 1)
	assert.Empty(t, create[0].ID)
	assert.Empty(t, create[0].IDProperty)
	assert.Equal(t, "B", create[0].Properties["firstname"])
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
	// (429/5xx are retried by the client, so this test uses non-retried auth codes.)
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

	// companies has no default id_property, so id_column alone is an error.
	records := make(chan source.RecordBatchResult)
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?id_column=domain"})
	require.ErrorContains(t, err, "id_column requires id_property")
}

// newPropertyServer answers the property batch-read endpoint, echoing back only
// the requested names that exist in props, so PrepareTable can be tested offline.
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

func TestPrepareTableWarnsCreateOnlyWithUniqueProperties(t *testing.T) {
	prevOut, prevErr, prevMode := output.Current()
	var buf bytes.Buffer
	output.Init(&buf, &buf, output.ModeText)
	t.Cleanup(func() { output.Init(prevOut, prevErr, prevMode) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/companies":
			_, _ = io.WriteString(w, `{"results":[{"name":"name","hasUniqueValue":false},{"name":"external_id","hasUniqueValue":true}]}`)
		default:
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[{"name":"name"}]}`)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	// companies has no default key and no id_property, so it creates; the unique
	// external_id property is surfaced as a hint.
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:  "companies",
		Schema: schemaWithColumns("name"),
	}))
	assert.Contains(t, buf.String(), "external_id")
	assert.Contains(t, buf.String(), "will be created")
}

func TestConflictErrorListsUniqueProperties(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/companies":
			_, _ = io.WriteString(w, `{"results":[{"name":"external_id","hasUniqueValue":true}]}`)
		case strings.HasSuffix(r.URL.Path, "/batch/create"):
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"category":"CONFLICT","message":"already exists"}`)
		default:
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	d := connectTestDestination(t, server.URL)

	s := arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"Acme"}, nil)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "companies"})
	require.ErrorContains(t, err, "external_id")
	require.ErrorContains(t, err, "unique properties on companies")
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
	// domain is not a unique-value identifier, but hasUniqueValue is unreliable, so
	// PrepareTable warns rather than blocking; HubSpot rejects at write time if so.
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

	// companies creates by default, so a conflict surfaces the upsert hint.
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	err := d.Write(context.Background(), records, destination.WriteOptions{Table: "companies"})
	require.ErrorContains(t, err, "already exists")
	require.ErrorContains(t, err, "id_property")
}

func TestCreateConflictSkippedWithOnErrorSkip(t *testing.T) {
	d := connectTestDestination(t, conflictServer(t).URL)

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: contactBatch()}
	close(records)
	require.NoError(t, d.Write(context.Background(), records, destination.WriteOptions{Table: "companies?on_error=skip"}))
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
