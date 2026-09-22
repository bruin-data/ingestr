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
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func colIndexOf(record arrow.RecordBatch) map[string]int {
	idx := make(map[string]int, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		idx[record.ColumnName(i)] = i
	}
	return idx
}

func stringBatch(cols map[string][]string, order []string) arrow.RecordBatch {
	fields := make([]arrow.Field, len(order))
	for i, name := range order {
		fields[i] = arrow.Field{Name: name, Type: arrow.BinaryTypes.String}
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	for i, name := range order {
		b.Field(i).(*array.StringBuilder).AppendValues(cols[name], nil)
	}
	return b.NewRecordBatch()
}

func TestCellValuesExplodesJSONArray(t *testing.T) {
	b := schema.NewJSONBuilder(memory.DefaultAllocator)
	b.Append(`["1","2","3"]`)
	b.Append(`42`)
	b.Append(`["a",null,"b"]`)
	b.Append(`[]`)
	arr := b.NewArray()
	defer arr.Release()

	assert.Equal(t, []string{"1", "2", "3"}, cellValues(arr, 0), "a JSON array explodes to one key per element")
	assert.Equal(t, []string{"42"}, cellValues(arr, 1), "a scalar JSON value is a single key")
	assert.Equal(t, []string{"a", "b"}, cellValues(arr, 2), "null elements are skipped")
	assert.Empty(t, cellValues(arr, 3), "an empty JSON array yields no keys")
}

// TestDeleteResolvesCaseInsensitiveMatchValue: HubSpot lowercases email, so the
// value it returns from batch/read differs from the source value. The client-side
// resolve map (used by delete/associations) must still match, not report a false
// OBJECT_NOT_FOUND.
func TestDeleteResolvesCaseInsensitiveMatchValue(t *testing.T) {
	var mu sync.Mutex
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v3/properties/contacts/email":
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		case "/crm/v3/objects/contacts/batch/read":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[{"id":"1","properties":{"email":"foo@bar.com"}}]}`)
		case "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"Foo@Bar.com"}}, []string{"email"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "delete", PrimaryKeys: []string{"email"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"1"}, archived, "source Foo@Bar.com resolves to the record HubSpot returns as foo@bar.com")
}

// TestMirrorDoesNotArchiveNormalizedMatchValue: replace mirrors by comparing the
// source key against HubSpot's returned key. HubSpot lowercases email, so a raw
// compare would miss the record just written and archive it. The just-written
// record must survive; only a genuinely absent record is archived.
func TestMirrorDoesNotArchiveNormalizedMatchValue(t *testing.T) {
	var mu sync.Mutex
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crm/v3/objects/contacts/batch/upsert":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		case r.URL.Path == "/crm/v3/objects/contacts" && r.Method == http.MethodGet:
			// "1" is the record we just wrote (email lowercased); "2" is stale.
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[{"id":"1","properties":{"email":"john@acme.com"}},{"id":"2","properties":{"email":"old@x.com"}}]}`)
		case r.URL.Path == "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"John@Acme.com"}, "firstname": {"J"}}, []string{"email", "firstname"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "replace", PrimaryKeys: []string{"email"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"2"}, archived, "the just-written record must not be archived; only the genuinely stale one is")
}

// TestReplaceFailModeSkipsArchive: in fail mode a mirror run with a rejected row
// must NOT perform the destructive archive pass — a failed run leaves the object
// unchanged beyond what was written, so no list/archive calls happen.
func TestReplaceFailModeSkipsArchive(t *testing.T) {
	var mu sync.Mutex
	var listed, archived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crm/v3/objects/contacts/batch/upsert":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"status":"error","category":"VALIDATION_ERROR","message":"bad row"}`)
		case r.URL.Path == "/crm/v3/objects/contacts" && r.Method == http.MethodGet:
			mu.Lock()
			listed = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[]}`)
		case r.URL.Path == "/crm/v3/objects/contacts/batch/archive":
			mu.Lock()
			archived = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "firstname": {"A"}}, []string{"email", "firstname"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "replace", PrimaryKeys: []string{"email"}, RejectMode: "fail",
	})
	require.Error(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.False(t, listed, "fail-mode mirror with rejects must not list records")
	assert.False(t, archived, "fail-mode mirror with rejects must not archive")
}

// TestSearchUpdateNotFoundToleratedUnderSkip: a record deleted between Search and
// the update (TOCTOU 404) must be a per-record reject under --reject-mode skip,
// not abort the whole run.
func TestSearchUpdateNotFoundToleratedUnderSkip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v3/properties/contacts/jobtitle":
			_, _ = io.WriteString(w, `{"hasUniqueValue":false}`) // non-unique -> Search path
		case "/crm/v3/objects/contacts/search":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[{"id":"1","properties":{"jobtitle":"Eng"}}]}`)
		case "/crm/v3/objects/contacts/batch/update":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"status":"error","category":"OBJECT_NOT_FOUND","message":"not found"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"jobtitle": {"Eng"}, "firstname": {"E"}}, []string{"jobtitle", "firstname"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=jobtitle", Strategy: "update", PrimaryKeys: []string{"jobtitle"}, RejectMode: "skip",
	})
	require.NoError(t, err, "a TOCTOU not-found under --reject-mode skip must be tolerated, not abort the run")
}

// TestHardErrorSurfacesEarlierRejects: in fail mode, a hard 5xx on a later batch
// must not hide the records already rejected by earlier batches — both appear in
// the returned error.
func TestHardErrorSurfacesEarlierRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crm/v3/objects/contacts/batch/upsert" {
			raw, _ := io.ReadAll(r.Body)
			if strings.Contains(string(raw), "a@x.com") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"bad row A"}`)
				return
			}
			// 401 is a hard, non-retried failure (5xx would retry for ~minutes).
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"boom"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	recA := stringBatch(map[string][]string{"email": {"a@x.com"}}, []string{"email"})
	recB := stringBatch(map[string][]string{"email": {"b@x.com"}}, []string{"email"})
	err := d.Write(context.Background(), feed(recA, recB), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "merge", PrimaryKeys: []string{"email"}, RejectMode: "fail",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401", "the hard failure is surfaced")
	assert.Contains(t, err.Error(), "bad row A", "the earlier reject is not lost behind it")
}

// TestSearchDedupsRepeatedIDs: a record returned more than once (case-variant
// values collapsing to one bucket, or pagination overlap) is queued only once.
func TestSearchDedupsRepeatedIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"results":[{"id":"1","properties":{"jobtitle":"Eng"}},{"id":"1","properties":{"jobtitle":"eng"}}]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	out, err := d.searchIDsByProperty(context.Background(), "contacts", "jobtitle", []string{"Eng", "eng"})
	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, out["eng"], "the same record id must not be queued twice")
}

// TestSystemicUpsertErrorAbortsEvenUnderSkip: an upsert against a non-unique
// id_property fails the whole batch structurally. It must abort (not bisect into
// per-record rejects that report success under skip while writing nothing), and
// must not amplify into many single-row requests.
func TestSystemicUpsertErrorAbortsEvenUnderSkip(t *testing.T) {
	var mu sync.Mutex
	var upsertCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crm/v3/objects/contacts/batch/upsert" {
			mu.Lock()
			upsertCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"Unable to perform update/upsert by non-unique 0-1 property phone"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"phone": {"1", "2", "3", "4"}}, []string{"phone"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=phone", Strategy: "merge", PrimaryKeys: []string{"phone"}, RejectMode: "skip",
	})
	require.Error(t, err, "a systemic upsert error must abort even under --reject-mode skip")
	assert.Contains(t, err.Error(), "non-unique")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, upsertCalls, "systemic error must not bisect into per-row requests")
}

// TestBatchReadByPropertyTreats404AsNoneFound: HubSpot returns 404 for a batch
// read whose keys are all absent. That means "none found", not a hard error, so
// resolveKeysToIDs must return an empty map (callers then reject the missing keys
// per --reject-mode) rather than aborting the whole run.
func TestBatchReadByPropertyTreats404AsNoneFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status":"error","message":"Not found"}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	got, err := d.resolveKeysToIDs(context.Background(), "contacts", "email", []string{"missing@x.com"})
	require.NoError(t, err, "a 404 (no keys found) must not be a hard error")
	assert.Empty(t, got)
}

// TestSystemicUpdateErrorAbortsEvenUnderSkip mirrors the upsert case for the
// batch/update path: a structural "non-unique" failure aborts without bisecting.
func TestSystemicUpdateErrorAbortsEvenUnderSkip(t *testing.T) {
	var mu sync.Mutex
	var updateCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/contacts/email":
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		case r.URL.Path == "/crm/v3/objects/contacts/batch/update":
			mu.Lock()
			updateCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"Unable to perform update by non-unique property email"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com", "b@x.com"}}, []string{"email"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "update", PrimaryKeys: []string{"email"}, RejectMode: "skip",
	})
	require.Error(t, err, "a systemic update error must abort even under --reject-mode skip")
	assert.Contains(t, err.Error(), "non-unique")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, updateCalls, "systemic error must not bisect into per-row requests")
}

// TestSystemicArchiveErrorAborts: a structural failure on the batch/archive path
// aborts rather than bisecting into per-record rejects that look tolerable.
func TestSystemicArchiveErrorAborts(t *testing.T) {
	var mu sync.Mutex
	var archiveCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crm/v3/objects/contacts/batch/archive" {
			mu.Lock()
			archiveCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"non-unique object reference"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"hs_object_id": {"1", "2"}}, []string{"hs_object_id"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts", Strategy: "delete", PrimaryKeys: []string{"hs_object_id"}, RejectMode: "skip",
	})
	require.Error(t, err, "a systemic archive error must abort even under --reject-mode skip")
	assert.Contains(t, err.Error(), "non-unique")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, archiveCalls, "systemic error must not bisect into per-row requests")
}

// TestSystemicAssociationErrorAborts: a structural failure on the association
// batch aborts rather than bisecting.
func TestSystemicAssociationErrorAborts(t *testing.T) {
	var mu sync.Mutex
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/batch/associate/default") {
			mu.Lock()
			calls++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"non-unique association spec"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"1", "2"}, "company_id": {"9", "8"}}, []string{"contact_id", "company_id"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts+companies", Strategy: "merge", PrimaryKeys: []string{"contact_id", "company_id"}, RejectMode: "skip",
	})
	require.Error(t, err, "a systemic association error must abort even under --reject-mode skip")
	assert.Contains(t, err.Error(), "non-unique")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, calls, "systemic association error must not bisect into per-row requests")
}

// TestLabeledAssociationDeleteUsesLabelsArchive: a delete with a label must remove
// only that association type via labels/archive (which carries {from,to,types}),
// not every type between the pair via the unlabeled archive endpoint.
func TestLabeledAssociationDeleteUsesLabelsArchive(t *testing.T) {
	var mu sync.Mutex
	var path string
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		path = r.URL.Path
		_ = json.Unmarshal(raw, &captured)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"111"}, "company_id": {"222"}}, []string{"contact_id", "company_id"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts+companies?association_type=280",
		Strategy:    "delete",
		PrimaryKeys: []string{"contact_id", "company_id"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/crm/v4/associations/contacts/companies/batch/labels/archive", path)
	inputs, ok := captured["inputs"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputs, 1)
	first := inputs[0].(map[string]interface{})
	// labels/archive takes a single "to" object plus the label in "types".
	assert.Equal(t, "222", first["to"].(map[string]interface{})["id"])
	assert.Equal(t, "111", first["from"].(map[string]interface{})["id"])
	types, ok := first["types"].([]interface{})
	require.True(t, ok, "a labeled delete must carry the association type")
	require.Len(t, types, 1)
	assert.Equal(t, float64(280), types[0].(map[string]interface{})["associationTypeId"])
}

// TestNonUniqueAssociationKeyLinksAll: when an association side matches on a
// non-unique property, the row links every record sharing that key (via Search),
// not just one (last-wins batch read).
func TestNonUniqueAssociationKeyLinksAll(t *testing.T) {
	var mu sync.Mutex
	var links [][2]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/contacts/email":
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/companies/domain":
			_, _ = io.WriteString(w, `{"hasUniqueValue":false}`)
		case r.URL.Path == "/crm/v3/objects/contacts/batch/read":
			_, _ = io.WriteString(w, `{"results":[{"id":"111","properties":{"email":"a@x.com"}}]}`)
		case r.URL.Path == "/crm/v3/objects/companies/search":
			_, _ = io.WriteString(w, `{"results":[{"id":"888","properties":{"domain":"acme.com"}},{"id":"999","properties":{"domain":"acme.com"}}]}`)
		case r.URL.Path == "/crm/v4/associations/contacts/companies/batch/associate/default":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []map[string]interface{} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				from := in["from"].(map[string]interface{})["id"].(string)
				to := in["to"].(map[string]interface{})["id"].(string)
				links = append(links, [2]string{from, to})
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "domain": {"acme.com"}}, []string{"email", "domain"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts+companies?id_property=email,domain",
		Strategy:    "merge",
		PrimaryKeys: []string{"email", "domain"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, [][2]string{{"111", "888"}, {"111", "999"}}, links)
}

func TestParseShaperStrategies(t *testing.T) {
	t.Run("merge upserts on the given match property", func(t *testing.T) {
		sh, err := parseShaper("contacts?id_property=email", "merge", []string{"email"}, "", false)
		require.NoError(t, err)
		assert.Equal(t, "email", sh.idProperty)
		assert.Equal(t, "email", sh.idColumn)
		assert.True(t, sh.upsert())
		assert.False(t, sh.updateOnly)
		assert.False(t, sh.mirror)
	})

	t.Run("merge requires an explicit match property", func(t *testing.T) {
		_, err := parseShaper("contacts", "merge", []string{"email"}, "", false)
		require.ErrorContains(t, err, "needs a match property")
	})

	t.Run("merge and replace need a --primary-key", func(t *testing.T) {
		for _, strat := range []string{"merge", "replace"} {
			_, err := parseShaper("contacts?id_property=email", strat, nil, "", false)
			require.ErrorContains(t, err, "needs a source key column", strat)
		}
	})

	t.Run("update and delete default the source column to hs_object_id", func(t *testing.T) {
		for _, strat := range []string{"update", "delete"} {
			sh, err := parseShaper("contacts", strat, nil, "", false)
			require.NoError(t, err, strat)
			assert.Equal(t, recordIDProperty, sh.idColumn, strat)
			assert.Equal(t, recordIDProperty, sh.idProperty, strat)
		}
	})

	t.Run("update is update-only", func(t *testing.T) {
		sh, err := parseShaper("contacts?id_property=email", "update", []string{"email"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.updateOnly)
		assert.Equal(t, "email", sh.idProperty)
	})

	t.Run("update without id_property defaults the match property to record id", func(t *testing.T) {
		sh, err := parseShaper("companies", "update", []string{"hs_object_id"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.updateOnly)
		assert.Equal(t, recordIDProperty, sh.idProperty)
		assert.Equal(t, "hs_object_id", sh.idColumn)
	})

	t.Run("append is create-only", func(t *testing.T) {
		sh, err := parseShaper("contacts", "append", nil, "", false)
		require.NoError(t, err)
		assert.True(t, sh.createOnly)
		assert.False(t, sh.matchesRecords())
	})

	t.Run("delete archives, --primary-key names the source column", func(t *testing.T) {
		sh, err := parseShaper("contacts", "delete", []string{"record_id"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.archive)
		assert.Equal(t, "record_id", sh.idColumn)
		assert.Equal(t, recordIDProperty, sh.idProperty)
	})

	t.Run("replace mirrors", func(t *testing.T) {
		sh, err := parseShaper("products?id_property=sku", "replace", []string{"sku"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.mirror)
		assert.NotNil(t, sh.seen)
		assert.Equal(t, "sku", sh.idProperty)
	})

	t.Run("replace requires an explicit match property", func(t *testing.T) {
		_, err := parseShaper("contacts", "replace", []string{"email"}, "", false)
		require.ErrorContains(t, err, "needs a match property")
	})

	t.Run("id_property sets the match property", func(t *testing.T) {
		sh, err := parseShaper("contacts?id_property=customer_id", "merge", []string{"customer_id"}, "", false)
		require.NoError(t, err)
		assert.Equal(t, "customer_id", sh.idProperty)
	})

	t.Run("composite primary key rejected", func(t *testing.T) {
		_, err := parseShaper("contacts?id_property=email", "merge", []string{"a", "b"}, "", false)
		require.ErrorContains(t, err, "composite primary key")
	})
}

func TestParseShaperAssociations(t *testing.T) {
	t.Run("obj+obj merge", func(t *testing.T) {
		sh, err := parseShaper("contacts+companies", "merge", []string{"email", "company_id"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.associate())
		assert.Equal(t, "contacts", sh.objectType)
		assert.Equal(t, "companies", sh.associateTo)
		assert.Equal(t, "email", sh.fromColumn)
		assert.Equal(t, "company_id", sh.toColumn)
		assert.False(t, sh.archive)
		assert.False(t, sh.mirror)
	})

	t.Run("delete unlinks", func(t *testing.T) {
		sh, err := parseShaper("contacts+companies", "delete", []string{"email", "company_id"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.archive)
	})

	t.Run("replace mirrors links", func(t *testing.T) {
		sh, err := parseShaper("contacts+companies", "replace", []string{"email", "company_id"}, "", false)
		require.NoError(t, err)
		assert.True(t, sh.mirror)
		assert.NotNil(t, sh.seenLinks)
	})

	t.Run("append not supported for associations", func(t *testing.T) {
		_, err := parseShaper("contacts+companies", "append", []string{"a", "b"}, "", false)
		require.ErrorContains(t, err, "associations support")
	})

	t.Run("positional id_property", func(t *testing.T) {
		sh, err := parseShaper("contacts+companies?id_property=email,domain", "merge", []string{"e", "c"}, "", false)
		require.NoError(t, err)
		assert.Equal(t, "email", sh.fromProperty)
		assert.Equal(t, "domain", sh.toProperty)
	})
}

func TestShapeRow(t *testing.T) {
	t.Run("upsert carries id property and value", func(t *testing.T) {
		sh, err := parseShaper("contacts?id_property=email", "merge", []string{"email"}, "", false)
		require.NoError(t, err)
		rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "name": {"A"}}, []string{"email", "name"})
		defer rec.Release()
		in, action, ok := sh.shapeRow(rec, colIndexOf(rec), 0)
		require.True(t, ok)
		assert.Equal(t, "upsert", action)
		assert.Equal(t, "email", in.IDProperty)
		assert.Equal(t, "a@x.com", in.ID)
		assert.Equal(t, "A", in.Properties["name"])
		assert.NotContains(t, in.Properties, "email")
	})

	t.Run("update-only skips row without match value", func(t *testing.T) {
		sh, err := parseShaper("contacts", "update", []string{"email"}, "", false)
		require.NoError(t, err)
		rec := stringBatch(map[string][]string{"email": {""}, "name": {"A"}}, []string{"email", "name"})
		defer rec.Release()
		_, _, ok := sh.shapeRow(rec, colIndexOf(rec), 0)
		assert.False(t, ok)
	})

	t.Run("append always creates", func(t *testing.T) {
		sh, err := parseShaper("contacts", "append", nil, "", false)
		require.NoError(t, err)
		rec := stringBatch(map[string][]string{"email": {"a@x.com"}}, []string{"email"})
		defer rec.Release()
		in, action, ok := sh.shapeRow(rec, colIndexOf(rec), 0)
		require.True(t, ok)
		assert.Equal(t, "create", action)
		assert.Empty(t, in.ID)
		assert.Equal(t, "a@x.com", in.Properties["email"])
	})

	t.Run("write-nulls emits empty string", func(t *testing.T) {
		sh, err := parseShaper("contacts?id_property=email", "merge", []string{"email"}, "", true)
		require.NoError(t, err)
		s := arrow.NewSchema([]arrow.Field{
			{Name: "email", Type: arrow.BinaryTypes.String},
			{Name: "phone", Type: arrow.BinaryTypes.String},
		}, nil)
		b := array.NewRecordBuilder(memory.DefaultAllocator, s)
		defer b.Release()
		b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com"}, nil)
		b.Field(1).(*array.StringBuilder).AppendValues([]string{""}, []bool{false})
		rec := b.NewRecordBatch()
		defer rec.Release()
		in, _, ok := sh.shapeRow(rec, colIndexOf(rec), 0)
		require.True(t, ok)
		phone, present := in.Properties["phone"]
		assert.True(t, present)
		assert.Equal(t, "", phone)
	})
}

// TestMultiSelectPassthrough pins that a multi-select value is sent verbatim: no
// semicolon logic, so "A;B" (overwrite) and ";A" (append) reach HubSpot unchanged.
func TestMultiSelectPassthrough(t *testing.T) {
	sh, err := parseShaper("contacts?id_property=email", "update", []string{"email"}, "", false)
	require.NoError(t, err)

	for _, val := range []string{"CHAMPION;DECISION_MAKER", ";BLOCKER"} {
		rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "hs_buying_role": {val}}, []string{"email", "hs_buying_role"})
		item, _, ok := sh.shapeRow(rec, colIndexOf(rec), 0)
		rec.Release()
		require.True(t, ok)
		assert.Equal(t, val, item.Properties["hs_buying_role"], "multi-select value must pass through unchanged")
	}
}

func TestCellValues(t *testing.T) {
	s := arrow.NewSchema([]arrow.Field{
		{Name: "ids", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	lb := b.Field(0).(*array.ListBuilder)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	lb.Append(true)
	vb.AppendValues([]string{"1", "2", "3"}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	got := cellValues(rec.Column(0), 0)
	assert.Equal(t, []string{"1", "2", "3"}, got)
}

func TestShapeAssociationCartesian(t *testing.T) {
	sh, err := parseShaper("contacts+companies", "merge", []string{"email", "company_ids"}, "", false)
	require.NoError(t, err)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "company_ids", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"c1"}, nil)
	lb := b.Field(1).(*array.ListBuilder)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	lb.Append(true)
	vb.AppendValues([]string{"co1", "co2"}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	// fromProperty/toProperty empty => values are treated as record ids directly.
	items, _, unresolved := sh.shapeAssociation(rec, colIndexOf(rec), 0, nil, nil)
	require.Empty(t, unresolved)
	require.Len(t, items, 2)
	assert.Equal(t, "c1", items[0].From.ID)
	assert.Equal(t, "co1", items[0].To.ID)
	assert.Equal(t, "co2", items[1].To.ID)
}

func TestShapeAssociationDedups(t *testing.T) {
	sh, err := parseShaper("contacts+companies", "merge", []string{"email", "company_ids"}, "", false)
	require.NoError(t, err)

	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "company_ids", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"c1"}, nil)
	lb := b.Field(1).(*array.ListBuilder)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	lb.Append(true)
	// Duplicate company id in the array must collapse to a single link.
	vb.AppendValues([]string{"co1", "co1", "co2"}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()

	items, _, unresolved := sh.shapeAssociation(rec, colIndexOf(rec), 0, nil, nil)
	require.Empty(t, unresolved)
	require.Len(t, items, 2)
	assert.Equal(t, "co1", items[0].To.ID)
	assert.Equal(t, "co2", items[1].To.ID)
}

// TestShapeAssociationUnresolvedKeyIsReject: a non-empty match value that resolves
// to no record is a not-found reject (handled per --reject-mode), while an empty
// cell yields neither a link nor a reject (a skip).
func TestShapeAssociationUnresolvedKeyIsReject(t *testing.T) {
	sh, err := parseShaper("contacts+companies?id_property=email,hs_object_id", "merge", []string{"email", "company_id"}, "", false)
	require.NoError(t, err)

	t.Run("unresolved to-side is a not-found reject", func(t *testing.T) {
		rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "company_id": {"999"}}, []string{"email", "company_id"})
		defer rec.Release()
		// email resolves to a contact id; company 999 does not exist.
		items, _, unresolved := sh.shapeAssociation(rec, colIndexOf(rec), 0,
			map[string][]string{"a@x.com": {"111"}}, map[string][]string{})
		require.Empty(t, items)
		require.Len(t, unresolved, 1)
		assert.Equal(t, objectNotFoundCategory, unresolved[0].category)
		assert.Contains(t, unresolved[0].message, "companies")
		assert.Contains(t, unresolved[0].message, `hs_object_id="999"`)
	})

	t.Run("both sides resolve => a link and no reject", func(t *testing.T) {
		rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "company_id": {"222"}}, []string{"email", "company_id"})
		defer rec.Release()
		items, _, unresolved := sh.shapeAssociation(rec, colIndexOf(rec), 0,
			map[string][]string{"a@x.com": {"111"}}, map[string][]string{"222": {"888"}})
		require.Empty(t, unresolved)
		require.Len(t, items, 1)
		assert.Equal(t, "111", items[0].From.ID)
		assert.Equal(t, "888", items[0].To.ID)
	})

	t.Run("empty cell is a skip, not a reject", func(t *testing.T) {
		rec := stringBatch(map[string][]string{"email": {""}, "company_id": {"222"}}, []string{"email", "company_id"})
		defer rec.Release()
		items, _, unresolved := sh.shapeAssociation(rec, colIndexOf(rec), 0,
			map[string][]string{}, map[string][]string{"222": {"888"}})
		require.Empty(t, items)
		require.Empty(t, unresolved)
	})
}

// --- server-backed behavior ---

func connectTest(t *testing.T, serverURL string) *HubSpotDestination {
	t.Helper()
	d := NewHubSpotDestination()
	uri := "hubspot://?api_key=tok&endpoint=" + url.QueryEscape(serverURL)
	require.NoError(t, d.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, d.Close(context.Background())) })
	return d
}

func feed(records ...arrow.RecordBatch) <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult, len(records))
	for _, r := range records {
		ch <- source.RecordBatchResult{Batch: r}
	}
	close(ch)
	return ch
}

func TestWriteUpsertPostsBatch(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "name": {"A"}}, []string{"email", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts?id_property=email",
		Strategy:    "merge",
		PrimaryKeys: []string{"email"},
	}))

	require.Len(t, paths, 1)
	assert.Equal(t, "/crm/v3/objects/contacts/batch/upsert", paths[0])
}

// TestPropertyColumnCarriedToUpsert: the destination sends every non-key column
// as a property under its (post-rename) name — the shaper honors whatever column
// names the batch carries, which is how --columns renames land here.
func TestPropertyColumnCarriedToUpsert(t *testing.T) {
	var mu sync.Mutex
	var captured []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/upsert") {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []map[string]interface{} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			captured = append(captured, body.Inputs...)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	// "jobtitle" stands in for a column the pipeline renamed before the write.
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "jobtitle": {"VP Eng"}}, []string{"email", "jobtitle"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "merge", PrimaryKeys: []string{"email"},
	}))

	require.Len(t, captured, 1)
	props := captured[0]["properties"].(map[string]interface{})
	assert.Equal(t, "VP Eng", props["jobtitle"])
	assert.NotContains(t, props, "email", "the match column is not resent as a property")
}

func TestAssociationArchiveUsesArrayShape(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(raw, &captured)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"111"}, "company_id": {"222"}}, []string{"contact_id", "company_id"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts+companies",
		Strategy:    "delete",
		PrimaryKeys: []string{"contact_id", "company_id"},
	}))

	inputs, ok := captured["inputs"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputs, 1)
	first := inputs[0].(map[string]interface{})
	// "to" must be an array of refs for the batch/archive endpoint.
	to, ok := first["to"].([]interface{})
	require.True(t, ok, "archive 'to' must be an array")
	require.Len(t, to, 1)
	assert.Equal(t, "222", to[0].(map[string]interface{})["id"])
	assert.Equal(t, "111", first["from"].(map[string]interface{})["id"])
}

func TestMirrorArchivesStaleRecords(t *testing.T) {
	var mu sync.Mutex
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/objects/contacts":
			// Two contacts exist; only k@x.com is in the source.
			_, _ = io.WriteString(w, `{"results":[
				{"id":"1","properties":{"email":"k@x.com"}},
				{"id":"2","properties":{"email":"stale@x.com"}}
			]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"k@x.com"}, "name": {"K"}}, []string{"email", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts?id_property=email",
		Strategy:    "replace",
		PrimaryKeys: []string{"email"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"2"}, archived)
}

// mirrorKeylessServer lists three records: one in the source, one with a stale
// key, and one with no value for the match property. Records the archived ids.
func mirrorKeylessServer(t *testing.T, archived *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/objects/contacts":
			_, _ = io.WriteString(w, `{"results":[
				{"id":"1","properties":{"email":"k@x.com"}},
				{"id":"2","properties":{"email":"stale@x.com"}},
				{"id":"3","properties":{"email":""}}
			]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				*archived = append(*archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMirrorKeylessRecords: replace is a full mirror — a record with no value for
// the match property (id 3) is archived along with the stale keyed one (id 2),
// since neither is in the source.
func TestMirrorKeylessRecords(t *testing.T) {
	var archived []string
	srv := mirrorKeylessServer(t, &archived)
	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"k@x.com"}, "name": {"K"}}, []string{"email", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "replace", PrimaryKeys: []string{"email"},
	}))
	assert.ElementsMatch(t, []string{"2", "3"}, archived, "stale and keyless records are both archived")
}

// TestPlanUseCases walks the plan's "Use Cases" page and pins the shaper parseShaper
// produces per case; out-of-scope cases are noted in comments, not exercised.
func TestPlanUseCases(t *testing.T) {
	type want struct {
		idProperty string
		updateOnly bool
		createOnly bool
		archive    bool
		mirror     bool
		associate  bool
	}
	cases := []struct {
		num      int
		desc     string
		table    string
		strategy string
		pks      []string
		want     want
	}{
		// 1. Lead score — update existing contacts by email (built-in unique).
		{1, "lead score", "contacts?id_property=email", "update", []string{"email"}, want{idProperty: "email", updateOnly: true}},
		// 2. Last product activity — update by HubSpot record id (default for update).
		{2, "product activity", "contacts?id_property=hs_object_id", "update", []string{"hs_object_id"}, want{idProperty: "hs_object_id", updateOnly: true}},
		// 3. Flag every contact in an account — update on a non-unique property; at
		//    write time resolveMatchMode uses the Search API (see TestSearchUpdate...).
		{3, "account flags", "companies?id_property=name", "update", []string{"company_name"}, want{idProperty: "name", updateOnly: true}},
		// 4. New signups — create-only, no matching (pair with --incremental-key).
		{4, "new signups", "contacts", "append", nil, want{createOnly: true}},
		// 5. GDPR permanent delete — OUT OF SCOPE: only archive (soft) is supported.
		// 6. Company enrichment — upsert on a custom unique property.
		{6, "company enrichment", "companies?id_property=company_id", "merge", []string{"company_id"}, want{idProperty: "company_id"}},
		// 7. Plan/contract level — update-only on a custom unique property.
		{7, "contracts", "companies?id_property=company_id", "update", []string{"company_id"}, want{idProperty: "company_id", updateOnly: true}},
		// 8. Support tickets — upsert on a custom unique property.
		{8, "support tickets", "tickets?id_property=ticket_id", "merge", []string{"ticket_id"}, want{idProperty: "ticket_id"}},
		// 9. Line items to deals — association (add).
		{9, "line items to deals", "line_items+deals", "merge", []string{"line_id", "deal_id"}, want{associate: true}},
		// 10. Product catalog — upsert on SKU.
		{10, "product catalog", "products?id_property=sku", "merge", []string{"sku"}, want{idProperty: "sku"}},
		// 11. Marketing event attendance — fuzzy (two keys + upsert); modeled as an
		//     association elsewhere. Not pinned here.
		// 12. Orders (native commerce object) — upsert.
		{12, "orders", "orders?id_property=order_id", "merge", []string{"order_id"}, want{idProperty: "order_id"}},
		// 13-15. Custom objects — upsert on a custom unique property.
		{13, "subscriptions", "subscriptions?id_property=subscription_id", "merge", []string{"subscription_id"}, want{idProperty: "subscription_id"}},
		{14, "installed assets", "installed_assets?id_property=asset_id", "merge", []string{"asset_id"}, want{idProperty: "asset_id"}},
		{15, "projects", "projects?id_property=project_id", "merge", []string{"project_id"}, want{idProperty: "project_id"}},
		// 16. Contact-company map — association (add).
		{16, "contact-company map", "contacts+companies", "merge", []string{"email", "company_id"}, want{associate: true}},
		// 17. Labeled association with a per-row role column — OUT OF SCOPE: only a
		//     fixed &label=<name> is supported, not a per-row label column.
		// 18. Unassociate — association archive.
		{18, "unassociate", "contacts+companies", "delete", []string{"email", "company_id"}, want{associate: true, archive: true}},
		// 19. Sync relationships — association mirror.
		{19, "sync relationships", "contacts+companies", "replace", []string{"email", "company_id"}, want{associate: true, mirror: true}},
		// 20-22. List membership — OUT OF SCOPE (lists not implemented).
		// 23. Merge duplicate records — OUT OF SCOPE (HubSpot record-merge API).
		// 24. Fill empty fields — update-only by email.
		{24, "fill empty fields", "contacts?id_property=email", "update", []string{"email"}, want{idProperty: "email", updateOnly: true}},
		// 25. Archive inactive records — delete (archive).
		{25, "archive inactive", "contacts?id_property=email", "delete", []string{"email"}, want{idProperty: "email", archive: true}},
		// 26. One-time historical backfill — upsert.
		{26, "historical backfill", "contacts?id_property=email", "merge", []string{"email"}, want{idProperty: "email"}},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("uc%02d_%s", tc.num, tc.desc), func(t *testing.T) {
			sh, err := parseShaper(tc.table, tc.strategy, tc.pks, "", false)
			require.NoError(t, err)
			assert.Equal(t, tc.want.associate, sh.associate(), "associate")
			assert.Equal(t, tc.want.archive, sh.archive, "archive")
			assert.Equal(t, tc.want.mirror, sh.mirror, "mirror")
			if !tc.want.associate {
				assert.Equal(t, tc.want.idProperty, sh.idProperty, "idProperty")
				assert.Equal(t, tc.want.updateOnly, sh.updateOnly, "updateOnly")
				assert.Equal(t, tc.want.createOnly, sh.createOnly, "createOnly")
			}
		})
	}
}

// TestPlanUseCaseDefaults pins match-property defaults: the match property comes
// only from id_property (required for merge/replace, defaulting to hs_object_id for
// update/delete); --primary-key is source-side only and never names the property.
func TestPlanUseCaseDefaults(t *testing.T) {
	// merge/replace need an explicit id_property; a --primary-key does not supply it.
	_, err := parseShaper("contacts", "merge", nil, "", false)
	require.ErrorContains(t, err, "needs a match property")

	_, err = parseShaper("products", "merge", []string{"sku"}, "", false)
	require.ErrorContains(t, err, "needs a match property")

	_, err = parseShaper("contacts", "replace", []string{"email"}, "", false)
	require.ErrorContains(t, err, "needs a match property")

	// --primary-key names the source column, not the match property.
	sh, err := parseShaper("contacts?id_property=email", "merge", []string{"user_email"}, "", false)
	require.NoError(t, err)
	assert.Equal(t, "email", sh.idProperty)
	assert.Equal(t, "user_email", sh.idColumn)

	// update with no id_property -> hs_object_id (--primary-key names the source column).
	sh, err = parseShaper("companies", "update", []string{"company_id"}, "", false)
	require.NoError(t, err)
	assert.Equal(t, recordIDProperty, sh.idProperty)

	// delete with no id_property -> hs_object_id.
	sh, err = parseShaper("companies", "delete", []string{"company_id"}, "", false)
	require.NoError(t, err)
	assert.Equal(t, recordIDProperty, sh.idProperty)
}

// TestSearchUpdateNonUniqueProperty covers use case 3: update on a non-unique
// property — ingestr Searches for every match and updates each by record id.
func TestSearchUpdateNonUniqueProperty(t *testing.T) {
	var mu sync.Mutex
	var updated []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/contacts/company_name":
			_, _ = io.WriteString(w, `{"name":"company_name","hasUniqueValue":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/search":
			// Two contacts share the company_name "Acme".
			_, _ = io.WriteString(w, `{"total":2,"results":[
				{"id":"11","properties":{"company_name":"Acme"}},
				{"id":"22","properties":{"company_name":"Acme"}}
			]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/batch/update":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []map[string]interface{} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			updated = append(updated, body.Inputs...)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"company_name": {"Acme"}, "account_flag": {"VIP"}}, []string{"company_name", "account_flag"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts?id_property=company_name",
		Strategy:    "update",
		PrimaryKeys: []string{"company_name"},
	}))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, updated, 2, "both matching records should be updated")
	ids := []string{updated[0]["id"].(string), updated[1]["id"].(string)}
	assert.ElementsMatch(t, []string{"11", "22"}, ids)
	for _, in := range updated {
		props := in["properties"].(map[string]interface{})
		assert.Equal(t, "VIP", props["account_flag"])
		assert.NotContains(t, in, "idProperty")
	}
}

// TestSearchDeleteNonUniqueProperty covers archiving every record matched on a
// non-unique property.
func TestSearchDeleteNonUniqueProperty(t *testing.T) {
	var mu sync.Mutex
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/crm/v3/properties/contacts/company_name":
			_, _ = io.WriteString(w, `{"hasUniqueValue":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/search":
			_, _ = io.WriteString(w, `{"results":[
				{"id":"11","properties":{"company_name":"Acme"}},
				{"id":"22","properties":{"company_name":"Acme"}}
			]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"company_name": {"Acme"}}, []string{"company_name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts?id_property=company_name",
		Strategy:    "delete",
		PrimaryKeys: []string{"company_name"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"11", "22"}, archived)
}

// TestUniquePropertyUsesBatchNotSearch confirms a unique property keeps the
// batch path (no Search call).
func TestUniquePropertyUsesBatchNotSearch(t *testing.T) {
	var mu sync.Mutex
	searched := false
	updatePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if strings.Contains(r.URL.Path, "/search") {
			searched = true
		}
		if strings.HasSuffix(r.URL.Path, "/batch/update") {
			updatePath = r.URL.Path
		}
		mu.Unlock()
		switch r.URL.Path {
		case "/crm/v3/properties/contacts/email":
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "name": {"A"}}, []string{"email", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts?id_property=email",
		Strategy:    "update",
		PrimaryKeys: []string{"email"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.False(t, searched, "unique property must not use Search")
	assert.Equal(t, "/crm/v3/objects/contacts/batch/update", updatePath)
}

// deleteNotFoundServer resolves "exists@x.com" to id "1" and leaves other emails
// unresolved (unique property, so no Search); archive always succeeds.
func deleteNotFoundServer(t *testing.T, archived *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v3/properties/contacts/email":
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		case "/crm/v3/objects/contacts/batch/read":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			var res []string
			for _, in := range body.Inputs {
				if in.ID == "exists@x.com" {
					res = append(res, `{"id":"1","properties":{"email":"exists@x.com"}}`)
				}
			}
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[`+strings.Join(res, ",")+`]}`)
		case "/crm/v3/objects/contacts/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				*archived = append(*archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDeleteNotFoundRejectModes: a delete whose match value resolves to no record
// is a not-found reject handled per --reject-mode, not a silent skip.
func TestDeleteNotFoundRejectModes(t *testing.T) {
	rows := func() arrow.RecordBatch {
		return stringBatch(map[string][]string{"email": {"exists@x.com", "missing@x.com"}}, []string{"email"})
	}

	t.Run("fail (default) errors but archives the found one", func(t *testing.T) {
		var archived []string
		srv := deleteNotFoundServer(t, &archived)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=email", Strategy: "delete", RejectMode: "fail",
			PrimaryKeys: []string{"email"},
		})
		require.Error(t, err)
		assert.Equal(t, []string{"1"}, archived)
	})

	t.Run("skip succeeds and still archives the found one", func(t *testing.T) {
		var archived []string
		srv := deleteNotFoundServer(t, &archived)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=email", Strategy: "delete", RejectMode: "skip",
			PrimaryKeys: []string{"email"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, archived)
	})

	t.Run("fail_fast errors", func(t *testing.T) {
		var archived []string
		srv := deleteNotFoundServer(t, &archived)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=email", Strategy: "delete", RejectMode: "fail_fast",
			PrimaryKeys: []string{"email"},
		})
		require.Error(t, err)
	})
}

// archiveBadIDServer rejects the whole archive batch with a 400 when the bad id
// "999" is present, mirroring HubSpot's atomic batch failure.
func archiveBadIDServer(t *testing.T, archived *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm/v3/objects/contacts/batch/archive" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				ID string `json:"id"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		for _, in := range body.Inputs {
			if in.ID == "999" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"invalid id"}`)
				return
			}
		}
		mu.Lock()
		for _, in := range body.Inputs {
			*archived = append(*archived, in.ID)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDeleteBadIDBisectsBatch: a whole-batch archive 400 must bisect so the valid
// id is still archived instead of the entire chunk being dropped.
func TestDeleteBadIDBisectsBatch(t *testing.T) {
	rows := func() arrow.RecordBatch {
		return stringBatch(map[string][]string{"hs_object_id": {"1", "999"}}, []string{"hs_object_id"})
	}

	t.Run("skip archives the valid id", func(t *testing.T) {
		var archived []string
		srv := archiveBadIDServer(t, &archived)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "delete", RejectMode: "skip",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, archived)
	})

	t.Run("fail errors but archives the valid id", func(t *testing.T) {
		var archived []string
		srv := archiveBadIDServer(t, &archived)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "delete", RejectMode: "fail",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.Error(t, err)
		assert.Equal(t, []string{"1"}, archived)
	})
}

// TestResolveKeysChunksToBatchReadLimit: resolving business-key values to record
// ids must chunk to HubSpot's 100-input batch/read cap. A single >100-input read
// returns 400 (as HubSpot does), which would abort the whole write.
func TestResolveKeysChunksToBatchReadLimit(t *testing.T) {
	var mu sync.Mutex
	maxInputs := 0
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v3/properties/contacts/email":
			// Unique property -> delete resolves values via batch/read, not Search.
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"hasUniqueValue":true}`)
		case "/crm/v3/objects/contacts/batch/read":
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			if len(body.Inputs) > maxInputs {
				maxInputs = len(body.Inputs)
			}
			mu.Unlock()
			if len(body.Inputs) > 100 {
				// HubSpot rejects an over-cap batch/read atomically.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"too many inputs"}`)
				return
			}
			var res []string
			for _, in := range body.Inputs {
				res = append(res, fmt.Sprintf(`{"id":"rec_%s","properties":{"email":%q}}`, in.ID, in.ID))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[`+strings.Join(res, ",")+`]}`)
		case "/crm/v3/objects/contacts/batch/archive":
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				archived = append(archived, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	// 150 distinct emails -> must span at least two batch/read chunks.
	emails := make([]string, 150)
	for i := range emails {
		emails[i] = fmt.Sprintf("u%03d@x.com", i)
	}
	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": emails}, []string{"email"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "delete", PrimaryKeys: []string{"email"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, maxInputs, 100, "batch/read must be chunked to <=100 inputs")
	assert.Len(t, archived, 150, "every resolved record is archived")
}

// associateBadPairServer rejects the whole v4 associate batch with a 400 whenever
// it still contains the bad from-id "999", and records the pairs it accepts.
func associateBadPairServer(t *testing.T, associated *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm/v4/associations/contacts/companies/batch/associate/default" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				From struct {
					ID string `json:"id"`
				} `json:"from"`
				To struct {
					ID string `json:"id"`
				} `json:"to"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		for _, in := range body.Inputs {
			if in.From.ID == "999" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"invalid association"}`)
				return
			}
		}
		mu.Lock()
		for _, in := range body.Inputs {
			*associated = append(*associated, in.From.ID+"->"+in.To.ID)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAssociationBadPairBisectsBatch: a whole-batch associate 400 must bisect so the
// valid link is still made instead of the entire chunk being dropped.
func TestAssociationBadPairBisectsBatch(t *testing.T) {
	rows := func() arrow.RecordBatch {
		return stringBatch(map[string][]string{"contact_id": {"1", "999"}, "company_id": {"10", "20"}}, []string{"contact_id", "company_id"})
	}

	t.Run("skip links the valid pair", func(t *testing.T) {
		var associated []string
		srv := associateBadPairServer(t, &associated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts+companies", Strategy: "merge",
			PrimaryKeys: []string{"contact_id", "company_id"}, RejectMode: "skip",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1->10"}, associated)
	})

	t.Run("fail errors but links the valid pair", func(t *testing.T) {
		var associated []string
		srv := associateBadPairServer(t, &associated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts+companies", Strategy: "merge",
			PrimaryKeys: []string{"contact_id", "company_id"}, RejectMode: "fail",
		})
		require.Error(t, err)
		assert.Equal(t, []string{"1->10"}, associated)
	})
}

// TestAssociationMirrorEmptyToClearsFrom: in a replace (mirror), a From whose To
// cell is empty is registered with an empty desired set, so finalization unlinks
// every association it currently has — the same full-mirror contract as records.
// A stale link on a From that does have a desired To is still unlinked too.
func TestAssociationMirrorEmptyToClearsFrom(t *testing.T) {
	var mu sync.Mutex
	var created, unlinked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v4/associations/contacts/companies/batch/associate/default":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []associationInput `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				created = append(created, in.From.ID+"->"+in.To.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		case "/crm/v4/objects/contacts/1/associations/companies":
			// From "1" wants only company 10, so live company 11 is stale.
			_, _ = io.WriteString(w, `{"results":[{"toObjectId":"10"},{"toObjectId":"11"}]}`)
		case "/crm/v4/objects/contacts/2/associations/companies":
			// From "2" had an empty To cell, so every live link is stale.
			_, _ = io.WriteString(w, `{"results":[{"toObjectId":"20"}]}`)
		case "/crm/v4/associations/contacts/companies/batch/archive":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []associationArchiveInput `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			for _, in := range body.Inputs {
				for _, to := range in.To {
					unlinked = append(unlinked, in.From.ID+"->"+to.ID)
				}
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"1", "2"}, "company_id": {"10", ""}},
		[]string{"contact_id", "company_id"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:       "contacts+companies",
		Strategy:    "replace",
		PrimaryKeys: []string{"contact_id", "company_id"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"1->10"}, created, "only the non-empty pair is linked")
	assert.ElementsMatch(t, []string{"1->11", "2->20"}, unlinked,
		"stale link on From 1 and every link on the empty-To From 2 are unlinked")
}

// TestCreate5xxAbortsWithoutRetry: a create (non-idempotent) must not retry on a
// 5xx — a retry could duplicate a batch HubSpot already committed. The run aborts
// with the error instead of silently dropping the batch or retrying it.
func TestCreate5xxAbortsWithoutRetry(t *testing.T) {
	var mu sync.Mutex
	var createHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/create") {
			mu.Lock()
			createHits++
			mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"status":"error","message":"boom"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "firstname": {"A"}},
		[]string{"email", "firstname"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table:    "contacts",
		Strategy: "append",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, createHits, "create must be sent exactly once — no 5xx retry")
}

// TestMergeByRecordIDPartitionsExistingAndMissing (finding 5): merge on
// hs_object_id splits a mixed batch — existing ids update, missing ids create.
func TestMergeByRecordIDPartitionsExistingAndMissing(t *testing.T) {
	var mu sync.Mutex
	var updated, created []string
	readStatus := http.StatusMultiStatus
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				ID string `json:"id"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		hasMissing := false
		for _, in := range body.Inputs {
			if in.ID == "999" {
				hasMissing = true
			}
		}
		switch r.URL.Path {
		case "/crm/v3/objects/contacts/batch/update":
			if hasMissing {
				// Whole-batch not-found triggers the partition re-route.
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"category":"OBJECT_NOT_FOUND","message":"not found"}`)
				return
			}
			mu.Lock()
			for _, in := range body.Inputs {
				updated = append(updated, in.ID)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		case "/crm/v3/objects/contacts/batch/read":
			// "1" exists, "999" does not.
			var res []string
			for _, in := range body.Inputs {
				if in.ID == "1" {
					res = append(res, `{"id":"1"}`)
				}
			}
			w.WriteHeader(readStatus)
			_, _ = io.WriteString(w, `{"results":[`+strings.Join(res, ",")+`]}`)
		case "/crm/v3/objects/contacts/batch/create":
			mu.Lock()
			created = append(created, "created")
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"hs_object_id": {"1", "999"}, "name": {"A", "B"}}, []string{"hs_object_id", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=hs_object_id", Strategy: "merge", PrimaryKeys: []string{"hs_object_id"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"1"}, updated, "existing id updated")
	assert.Len(t, created, 1, "missing id routed to create")
}

// TestMergeByRecordIDAllMissing404 (finding 5 hardening): when batch-read returns
// 404 for an all-missing set, every id routes to create rather than erroring.
func TestMergeByRecordIDAllMissing404(t *testing.T) {
	var mu sync.Mutex
	created := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crm/v3/objects/contacts/batch/update":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"category":"OBJECT_NOT_FOUND","message":"not found"}`)
		case "/crm/v3/objects/contacts/batch/read":
			w.WriteHeader(http.StatusNotFound) // all missing -> 404
			_, _ = io.WriteString(w, `{"category":"OBJECT_NOT_FOUND"}`)
		case "/crm/v3/objects/contacts/batch/create":
			mu.Lock()
			created++
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"hs_object_id": {"111", "222"}, "name": {"A", "B"}}, []string{"hs_object_id", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=hs_object_id", Strategy: "merge", PrimaryKeys: []string{"hs_object_id"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, created, 1, "all-missing batch must route to create, not error")
}

// TestRejectModeWriteNullsCross exercises the two RETL run flags together: for
// every --reject-mode, --write-nulls decides whether a null cell is sent as ""
// (clear) or omitted, independently of the reject policy.
func TestRejectModeWriteNullsCross(t *testing.T) {
	for _, rm := range []string{"fail", "fail_fast", "skip"} {
		for _, wn := range []bool{false, true} {
			name := rm
			if wn {
				name += "/write-nulls"
			} else {
				name += "/omit-nulls"
			}
			t.Run(name, func(t *testing.T) {
				var mu sync.Mutex
				var captured []map[string]interface{}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/batch/upsert") {
						raw, _ := io.ReadAll(r.Body)
						var body struct {
							Inputs []map[string]interface{} `json:"inputs"`
						}
						_ = json.Unmarshal(raw, &body)
						mu.Lock()
						captured = append(captured, body.Inputs...)
						mu.Unlock()
					}
					w.WriteHeader(http.StatusOK)
					_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
				}))
				t.Cleanup(srv.Close)

				d := connectTest(t, srv.URL)
				// phone is null; email present.
				s := arrow.NewSchema([]arrow.Field{
					{Name: "email", Type: arrow.BinaryTypes.String},
					{Name: "phone", Type: arrow.BinaryTypes.String},
				}, nil)
				b := array.NewRecordBuilder(memory.DefaultAllocator, s)
				b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com"}, nil)
				b.Field(1).(*array.StringBuilder).AppendValues([]string{""}, []bool{false})
				rec := b.NewRecordBatch()
				b.Release()

				err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
					Table: "contacts?id_property=email", Strategy: "merge",
					RejectMode: rm, WriteNulls: wn, PrimaryKeys: []string{"email"},
				})
				require.NoError(t, err)
				require.Len(t, captured, 1)
				props := captured[0]["properties"].(map[string]interface{})
				phone, present := props["phone"]
				if wn {
					assert.True(t, present, "write-nulls: phone should be sent")
					assert.Equal(t, "", phone, "write-nulls: phone should be empty string")
				} else {
					assert.False(t, present, "omit-nulls: phone should be absent")
				}
			})
		}
	}
}

// updateNotFoundServer answers batch/update with an atomic OBJECT_NOT_FOUND
// whenever the chunk still contains the missing id "999", and records the ids of
// any chunk it accepts. This forces sendUpdateOnly to bisect down to singletons.
func updateNotFoundServer(t *testing.T, updated *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm/v3/objects/contacts/batch/update" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				ID string `json:"id"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		for _, in := range body.Inputs {
			if in.ID == "999" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"category":"OBJECT_NOT_FOUND","message":"not found"}`)
				return
			}
		}
		mu.Lock()
		for _, in := range body.Inputs {
			*updated = append(*updated, in.ID)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestUpdateOnlyNotFoundRejectModes: update-only must not re-route missing ids to
// create and must honor --reject-mode instead of aborting the whole run — a single
// missing id used to hard-error regardless, dropping the valid rows.
func TestUpdateOnlyNotFoundRejectModes(t *testing.T) {
	rows := func() arrow.RecordBatch {
		return stringBatch(map[string][]string{"hs_object_id": {"1", "999"}, "name": {"A", "B"}}, []string{"hs_object_id", "name"})
	}

	t.Run("fail errors but updates the found one", func(t *testing.T) {
		var updated []string
		srv := updateNotFoundServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "update", RejectMode: "fail",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.Error(t, err)
		assert.Equal(t, []string{"1"}, updated)
	})

	t.Run("skip succeeds and still updates the found one", func(t *testing.T) {
		var updated []string
		srv := updateNotFoundServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "update", RejectMode: "skip",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, updated)
	})

	t.Run("fail_fast errors", func(t *testing.T) {
		var updated []string
		srv := updateNotFoundServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "update", RejectMode: "fail_fast",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.Error(t, err)
	})
}

// updateValidationServer rejects the whole update batch with a validation 400 when
// the bad id "999" is present, mirroring HubSpot's atomic validation failure.
func updateValidationServer(t *testing.T, updated *[]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm/v3/objects/contacts/batch/update" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Inputs []struct {
				ID string `json:"id"`
			} `json:"inputs"`
		}
		_ = json.Unmarshal(raw, &body)
		for _, in := range body.Inputs {
			if in.ID == "999" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"invalid property"}`)
				return
			}
		}
		mu.Lock()
		for _, in := range body.Inputs {
			*updated = append(*updated, in.ID)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestUpdateValidationBisectsBatch: a validation 400 rejects the whole update batch,
// so the writer must bisect to land the valid rows instead of dropping them all.
func TestUpdateValidationBisectsBatch(t *testing.T) {
	rows := func() arrow.RecordBatch {
		return stringBatch(map[string][]string{"hs_object_id": {"1", "999"}, "name": {"A", "B"}}, []string{"hs_object_id", "name"})
	}

	t.Run("update-only skip lands the valid row", func(t *testing.T) {
		var updated []string
		srv := updateValidationServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "update", RejectMode: "skip",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, updated)
	})

	t.Run("merge skip lands the valid row", func(t *testing.T) {
		var updated []string
		srv := updateValidationServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "merge", RejectMode: "skip",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, updated)
	})

	t.Run("fail names the rejected record by record id", func(t *testing.T) {
		var updated []string
		srv := updateValidationServer(t, &updated)
		d := connectTest(t, srv.URL)
		err := d.Write(context.Background(), feed(rows()), destination.WriteOptions{
			Table: "contacts?id_property=hs_object_id", Strategy: "update", RejectMode: "fail",
			PrimaryKeys: []string{"hs_object_id"},
		})
		require.Error(t, err)
		// hs_object_id is the record id, sent as "id".
		assert.Contains(t, err.Error(), "[id=999]", "the reject line names which record failed")
	})

	t.Run("fail names the rejected record by match property", func(t *testing.T) {
		// Upsert (merge) carries idProperty=email, so a validation 400 on the bad
		// value names the reject as email=999, not id=999. Bisects to the singleton.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []struct {
					ID string `json:"id"`
				} `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			for _, in := range body.Inputs {
				if in.ID == "999" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"category":"VALIDATION_ERROR","message":"invalid property"}`)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}))
		t.Cleanup(srv.Close)

		d := connectTest(t, srv.URL)
		rec := stringBatch(map[string][]string{"email": {"a@x.com", "999"}, "name": {"A", "B"}}, []string{"email", "name"})
		err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
			Table: "contacts?id_property=email", Strategy: "merge", RejectMode: "fail",
			PrimaryKeys: []string{"email"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[email=999]", "the reject line names the offending match value")
	})
}

// TestSourceMatchColumnViaPrimaryKey: --primary-key names the source column that
// carries the match value, distinct from id_property (the HubSpot property matched
// on), for the case where the source column is not named like the property.
func TestSourceMatchColumnViaPrimaryKey(t *testing.T) {
	var mu sync.Mutex
	var captured []batchInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/upsert") {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []batchInput `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			captured = append(captured, body.Inputs...)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	// The match value lives in "user_email", not a column literally named "email".
	rec := stringBatch(map[string][]string{"user_email": {"a@x.com"}, "name": {"A"}}, []string{"user_email", "name"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "merge", PrimaryKeys: []string{"user_email"},
	}))

	require.Len(t, captured, 1)
	assert.Equal(t, "email", captured[0].IDProperty)
	assert.Equal(t, "a@x.com", captured[0].ID, "match value is read from the --primary-key column")
	assert.NotContains(t, captured[0].Properties, "user_email", "the match column is not resent as a property")
	assert.Equal(t, "A", captured[0].Properties["name"])
}

// TestWriteNullsParamHonored: write_nulls=true on the dest-table (not just the
// --write-nulls flag) must send a null cell as an empty string to clear it.
func TestWriteNullsParamHonored(t *testing.T) {
	var mu sync.Mutex
	var captured []batchInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/upsert") {
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Inputs []batchInput `json:"inputs"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			captured = append(captured, body.Inputs...)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	s := arrow.NewSchema([]arrow.Field{
		{Name: "email", Type: arrow.BinaryTypes.String},
		{Name: "phone", Type: arrow.BinaryTypes.String},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, s)
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"a@x.com"}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{""}, []bool{false})
	rec := b.NewRecordBatch()
	b.Release()

	// No WriteNulls flag: the behavior comes purely from the dest-table param.
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email&write_nulls=true", Strategy: "merge", PrimaryKeys: []string{"email"},
	}))

	require.Len(t, captured, 1)
	phone, present := captured[0].Properties["phone"]
	assert.True(t, present, "write_nulls param: null phone should be sent")
	assert.Equal(t, "", phone, "write_nulls param: null phone should be an empty string")
}

// TestPropertyValueDecimal256: high-precision decimals (precision >38) map to
// Decimal256 per repo convention; without the case they serialized as a Go struct.
func TestPropertyValueDecimal256(t *testing.T) {
	dt := &arrow.Decimal256Type{Precision: 50, Scale: 4}
	bld := array.NewDecimal256Builder(memory.DefaultAllocator, dt)
	defer bld.Release()
	n, err := decimal256.FromString("12345.6789", dt.Precision, dt.Scale)
	require.NoError(t, err)
	bld.Append(n)
	arr := bld.NewArray()
	defer arr.Release()

	val, ok := propertyValue(arr, 0)
	require.True(t, ok)
	assert.Equal(t, "12345.6789", val)
}

func TestStrategySupport(t *testing.T) {
	d := NewHubSpotDestination()
	assert.True(t, destination.IsReverseETL(d))
	assert.True(t, d.SupportsAppendStrategy())
	assert.True(t, d.SupportsReplaceStrategy())
}

func TestInvalidURI(t *testing.T) {
	d := NewHubSpotDestination()
	require.ErrorContains(t, d.Connect(context.Background(), "not://x"), "must start with hubspot://")
	require.ErrorContains(t, d.Connect(context.Background(), "hubspot://?x=y"), "api_key or service_key is required")
}

// hsSchema builds a string-typed TableSchema for PrepareTable tests.
func hsSchema(cols ...string) *schema.TableSchema {
	c := make([]schema.Column, len(cols))
	for i, name := range cols {
		c[i] = schema.Column{Name: name, DataType: schema.TypeString}
	}
	return &schema.TableSchema{Name: "t", Columns: c}
}

// propertiesReadServer answers /crm/v3/properties/<obj>/batch/read as if only the
// named properties exist, and 200-OKs anything else. Records the paths it saw.
func propertiesReadServer(t *testing.T, existing ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/batch/read") {
			var body struct {
				Inputs []struct {
					Name string `json:"name"`
				} `json:"inputs"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			want := map[string]bool{}
			for _, e := range existing {
				want[e] = true
			}
			var results []map[string]string
			for _, in := range body.Inputs {
				if want[in.Name] {
					results = append(results, map[string]string{"name": in.Name})
				}
			}
			out, _ := json.Marshal(map[string]interface{}{"results": results})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(out)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestPrepareTableRejectsUnknownProperty(t *testing.T) {
	srv, _ := propertiesReadServer(t, "email", "jobtitle")
	d := connectTest(t, srv.URL)
	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Schema:      hsSchema("email", "jobtitle", "bogus_prop"),
		Table:       "contacts?id_property=email",
		Strategy:    "merge",
		PrimaryKeys: []string{"email"},
	})
	require.ErrorContains(t, err, "not properties")
	require.ErrorContains(t, err, "bogus_prop")
}

func TestPrepareTableAllPropertiesKnown(t *testing.T) {
	srv, _ := propertiesReadServer(t, "email", "jobtitle")
	d := connectTest(t, srv.URL)
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Schema:      hsSchema("email", "jobtitle"),
		Table:       "contacts?id_property=email",
		Strategy:    "merge",
		PrimaryKeys: []string{"email"},
	}))
}

func TestPrepareTablePropertyCheckIsBestEffort(t *testing.T) {
	// A failed property read (e.g. missing scope) must not block a valid load.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/read") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := connectTest(t, srv.URL)
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Schema:      hsSchema("email", "jobtitle"),
		Table:       "contacts?id_property=email",
		Strategy:    "merge",
		PrimaryKeys: []string{"email"},
	}))
}

func TestPrepareTableArchiveSkipsPropertyCheck(t *testing.T) {
	srv, paths := propertiesReadServer(t)
	d := connectTest(t, srv.URL)
	require.NoError(t, d.PrepareTable(context.Background(), destination.PrepareOptions{
		Schema:      hsSchema("hs_object_id"),
		Table:       "contacts",
		Strategy:    "delete",
		PrimaryKeys: []string{"hs_object_id"},
	}))
	assert.Empty(t, *paths, "archive mode writes no properties, so nothing is validated")
}

func TestCustomObjectRecordResolvesNameViaSchemas(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/crm/v3/schemas":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[{"objectTypeId":"2-123","name":"license","fullyQualifiedName":"p123_license"}]}`)
		case strings.HasPrefix(r.URL.Path, "/crm/v3/objects/license/"):
			// Custom object addressed by name cannot be inferred: force a retry.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"status":"error","message":"Unable to infer object type from: license","category":"VALIDATION_ERROR"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"sku": {"A1"}, "seats": {"5"}}, []string{"sku", "seats"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "license?id_property=sku", Strategy: "merge", PrimaryKeys: []string{"sku"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, paths, "/crm/v3/schemas")
	assert.Contains(t, paths, "/crm/v3/objects/2-123/batch/upsert", "retried on the resolved objectTypeId")
}

func TestCustomObjectRecordByObjectTypeIDDirect(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"sku": {"A1"}, "seats": {"5"}}, []string{"sku", "seats"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "2-456?id_property=sku", Strategy: "merge", PrimaryKeys: []string{"sku"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, paths, "/crm/v3/objects/2-456/batch/upsert")
	assert.NotContains(t, paths, "/crm/v3/schemas", "an explicit objectTypeId needs no schema lookup")
}

func TestCustomObjectAssociationResolvesName(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/crm/v3/schemas":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[{"objectTypeId":"2-123","name":"license"}]}`)
		case strings.Contains(r.URL.Path, "/associations/contacts/license/"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"status":"error","message":"Unable to infer object type from: license","category":"VALIDATION_ERROR"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
		}
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"111"}, "license_id": {"222"}}, []string{"contact_id", "license_id"})
	require.NoError(t, d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts+license", Strategy: "merge", PrimaryKeys: []string{"contact_id", "license_id"},
	}))

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, paths, "/crm/v3/schemas")
	assert.Contains(t, paths, "/crm/v4/associations/contacts/2-123/batch/associate/default", "retried on the resolved objectTypeId")
}

func TestAssociationLabelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/labels") {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"results":[{"category":"USER_DEFINED","typeId":42,"label":"Partner"}]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"contact_id": {"111"}, "company_id": {"222"}}, []string{"contact_id", "company_id"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts+companies?label=NoSuchLabel", Strategy: "merge", PrimaryKeys: []string{"contact_id", "company_id"},
	})
	require.ErrorContains(t, err, "association label")
	require.ErrorContains(t, err, "NoSuchLabel")
	require.ErrorContains(t, err, "Partner", "the error lists the labels that do exist")
}

func TestFatalStatusPropagates(t *testing.T) {
	// A non-record-level failure (here 403, e.g. a bad token) must abort the write
	// rather than be swallowed as if the batch succeeded. 5xx is left out on
	// purpose: the client retries it 10x with backoff, which would stall the test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/upsert") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"status":"error","message":"forbidden","category":"FORBIDDEN"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}, "name": {"A"}}, []string{"email", "name"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts?id_property=email", Strategy: "merge", PrimaryKeys: []string{"email"},
	})
	require.ErrorContains(t, err, "403", "a non-record-level failure aborts rather than silently dropping the batch")
}

// TestAppendConflictHintsUpsert: an append (create-only) hitting a unique-property
// 409 is a per-record reject, and the end-of-run report nudges toward id_property
// even in fail mode (not just fail_fast).
func TestAppendConflictHintsUpsert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/batch/create") {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"category":"CONFLICT","message":"Contact already exists"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"COMPLETE","results":[]}`)
	}))
	t.Cleanup(srv.Close)

	d := connectTest(t, srv.URL)
	rec := stringBatch(map[string][]string{"email": {"a@x.com"}}, []string{"email"})
	err := d.Write(context.Background(), feed(rec), destination.WriteOptions{
		Table: "contacts", Strategy: "append", RejectMode: "fail",
	})
	require.ErrorContains(t, err, "hint")
	require.ErrorContains(t, err, "id_property")
}
