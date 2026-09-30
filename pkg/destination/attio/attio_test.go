package attio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func init() {
	retryBackoff = func(int) time.Duration { return 0 }
}

type fakeAttr struct {
	Slug        string
	Type        string
	Unique      bool
	Multiselect bool
	ReadOnly    bool
	Targets     []string
	Title       string
}

// fakeAttio is an in-memory Attio workspace with one "people" object and a
// "companies" object, enough to exercise every write path.
type fakeAttio struct {
	mu       sync.Mutex
	attrs    map[string][]fakeAttr
	records  map[string]map[string]map[string][]string
	nextID   int
	requests []string
	// hook may answer a request itself (method, path, body); true = handled.
	hook  func(w http.ResponseWriter, r *http.Request, body string) bool
	scope string
}

func newFake() *fakeAttio {
	return &fakeAttio{
		scope: "record_permission:read-write object_configuration:read",
		attrs: map[string][]fakeAttr{
			"people": {
				{Slug: "email_addresses", Type: "email-address", Unique: true, Multiselect: true},
				{Slug: "name", Type: "personal-name"},
				{Slug: "job_title", Type: "text", Title: "Job title"},
				{Slug: "external_id", Type: "text", Unique: true},
				{Slug: "score", Type: "number"},
				{Slug: "tags", Type: "select", Multiselect: true},
				{Slug: "company", Type: "record-reference", Targets: []string{"companies"}},
				{Slug: "related", Type: "record-reference", Multiselect: true, Targets: []string{"companies", "people"}},
				{Slug: "created_at", Type: "timestamp", ReadOnly: true},
			},
			"companies": {
				{Slug: "domains", Type: "domain", Unique: true, Multiselect: true},
				{Slug: "name", Type: "text"},
			},
		},
		records: map[string]map[string]map[string][]string{"people": {}, "companies": {}},
	}
}

func fakeAttrID(object string, i int) string {
	return fmt.Sprintf("%08d-0000-4000-8000-%012d", len(object), i)
}

func (f *fakeAttio) attr(object, slug string) (fakeAttr, bool) {
	for _, a := range f.attrs[object] {
		if a.Slug == slug {
			return a, true
		}
	}
	return fakeAttr{}, false
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]interface{}{"status_code": status, "type": "invalid_request_error", "code": code, "message": msg})
}

// flatten turns a written value into the strings the fake stores.
func flatten(v interface{}) []string {
	switch x := v.(type) {
	case []interface{}:
		var out []string
		for _, item := range x {
			out = append(out, flatten(item)...)
		}
		return out
	case map[string]interface{}:
		b, _ := json.Marshal(x)
		return []string{string(b)}
	case nil:
		return nil
	default:
		return []string{fmt.Sprint(x)}
	}
}

func (f *fakeAttio) render(object, id string) map[string]interface{} {
	values := map[string]interface{}{}
	for slug, vals := range f.records[object][id] {
		a, _ := f.attr(object, slug)
		field := "value"
		switch a.Type {
		case "email-address":
			field = "email_address"
		case "domain":
			field = "domain"
		}
		list := []map[string]interface{}{}
		for _, v := range vals {
			list = append(list, map[string]interface{}{field: v})
		}
		values[slug] = list
	}
	return map[string]interface{}{"id": map[string]string{"record_id": id}, "values": values}
}

func (f *fakeAttio) findBy(object, slug, value string) []string {
	var ids []string
	for id, rec := range f.records[object] {
		for _, v := range rec[slug] {
			if strings.EqualFold(v, value) {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func (f *fakeAttio) apply(object, id string, values map[string]interface{}) {
	rec := f.records[object][id]
	for slug, v := range values {
		vals := flatten(v)
		if len(vals) == 0 {
			delete(rec, slug)
			continue
		}
		rec[slug] = vals
	}
}

func (f *fakeAttio) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" "+body)
	w.Header().Set("Content-Type", "application/json")
	if f.hook != nil && f.hook(w, r, body) {
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "self" {
		writeJSON(w, 200, map[string]interface{}{"active": true, "scope": f.scope, "workspace_slug": "test"})
		return
	}
	if parts[0] != "objects" {
		apiErr(w, 404, "not_found", "unknown path")
		return
	}
	if len(parts) == 1 {
		var data []map[string]interface{}
		for _, slug := range []string{"companies", "people"} {
			noun := map[string]string{"people": "Person", "companies": "Company"}[slug]
			data = append(data, map[string]interface{}{"id": map[string]string{"object_id": "id-" + slug}, "api_slug": slug, "singular_noun": noun, "plural_noun": noun + "s"})
		}
		writeJSON(w, 200, map[string]interface{}{"data": data})
		return
	}
	object := parts[1]
	if _, ok := f.attrs[object]; !ok {
		apiErr(w, 404, "not_found", "object not found")
		return
	}
	switch {
	case len(parts) == 2:
		writeJSON(w, 200, map[string]interface{}{"data": map[string]interface{}{"id": map[string]string{"object_id": "id-" + object}, "api_slug": object}})
	case len(parts) == 3 && parts[2] == "attributes":
		var data []map[string]interface{}
		for i, a := range f.attrs[object] {
			var ids []string
			for _, t := range a.Targets {
				ids = append(ids, "id-"+t)
			}
			data = append(data, map[string]interface{}{
				"id": map[string]string{"attribute_id": fakeAttrID(object, i)}, "title": a.Title,
				"api_slug": a.Slug, "type": a.Type, "is_unique": a.Unique, "is_multiselect": a.Multiselect,
				"is_writable": !a.ReadOnly, "config": map[string]interface{}{"record_reference": map[string]interface{}{"allowed_object_ids": ids}},
			})
		}
		writeJSON(w, 200, map[string]interface{}{"data": data})
	case len(parts) == 4 && parts[3] == "query":
		var q struct {
			Filter map[string]interface{} `json:"filter"`
			Limit  int                    `json:"limit"`
			Offset int                    `json:"offset"`
		}
		_ = json.Unmarshal(raw, &q)
		var ids []string
		if q.Filter == nil {
			for id := range f.records[object] {
				ids = append(ids, id)
			}
			sort.Strings(ids)
		} else {
			for slug, v := range q.Filter {
				ids = f.findBy(object, slug, fmt.Sprint(v))
			}
		}
		end := min(len(ids), q.Offset+q.Limit)
		var data []map[string]interface{}
		for _, id := range ids[min(q.Offset, len(ids)):end] {
			data = append(data, f.render(object, id))
		}
		writeJSON(w, 200, map[string]interface{}{"data": data})
	case len(parts) == 3 && r.Method == http.MethodPost:
		values := decodeValues(raw)
		for slug, v := range values {
			if a, _ := f.attr(object, slug); a.Unique {
				for _, val := range flatten(v) {
					if len(f.findBy(object, slug, val)) > 0 {
						apiErr(w, 400, "uniqueness_conflict", fmt.Sprintf("value %q for unique attribute %s already exists", val, slug))
						return
					}
				}
			}
		}
		f.nextID++
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.nextID)
		f.records[object][id] = map[string][]string{}
		f.apply(object, id, values)
		writeJSON(w, 200, map[string]interface{}{"data": f.render(object, id)})
	case len(parts) == 3 && r.Method == http.MethodPut:
		values := decodeValues(raw)
		match := r.URL.Query().Get("matching_attribute")
		var id string
		for _, val := range flatten(values[match]) {
			if found := f.findBy(object, match, val); len(found) > 0 {
				id = found[0]
			}
		}
		if id == "" {
			f.nextID++
			id = fmt.Sprintf("00000000-0000-0000-0000-%012d", f.nextID)
			f.records[object][id] = map[string][]string{}
		}
		f.apply(object, id, values)
		writeJSON(w, 200, map[string]interface{}{"data": f.render(object, id)})
	case len(parts) == 4:
		id := parts[3]
		if _, ok := f.records[object][id]; !ok {
			apiErr(w, 404, "not_found", "Record with ID \""+id+"\" not found.")
			return
		}
		if r.Method == http.MethodDelete {
			delete(f.records[object], id)
			writeJSON(w, 200, map[string]interface{}{})
			return
		}
		f.apply(object, id, decodeValues(raw))
		writeJSON(w, 200, map[string]interface{}{"data": f.render(object, id)})
	default:
		apiErr(w, 404, "not_found", "unknown path")
	}
}

func decodeValues(raw []byte) map[string]interface{} {
	var body struct {
		Data struct {
			Values map[string]interface{} `json:"values"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Data.Values
}

func (f *fakeAttio) seed(object string, values map[string][]string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.nextID)
	f.records[object][id] = values
	return id
}

func (f *fakeAttio) count(object string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records[object])
}

func (f *fakeAttio) get(object, id string) map[string][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[object][id]
}

func (f *fakeAttio) requestsMatching(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func newDest(t *testing.T, f *fakeAttio) *AttioDestination {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(server.Close)
	d := NewAttioDestination()
	d.baseURL = server.URL
	if err := d.Connect(context.Background(), "attio://?api_key=key"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

func stringBatch(cols map[string][]string, order []string) <-chan source.RecordBatchResult {
	fields := make([]arrow.Field, 0, len(order))
	for _, name := range order {
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	for i, name := range order {
		bld := b.Field(i).(*array.StringBuilder)
		for _, v := range cols[name] {
			if v == "" {
				bld.AppendNull()
				continue
			}
			bld.Append(v)
		}
	}
	ch := make(chan source.RecordBatchResult, 1)
	ch <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(ch)
	return ch
}

func emptyBatches() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}

func opts(table, strategy string, pk ...string) destination.WriteOptions {
	return destination.WriteOptions{Table: table, Strategy: strategy, PrimaryKeys: pk, Schema: &schema.TableSchema{}, WriteNulls: true}
}

func TestParseShaper(t *testing.T) {
	tests := []struct {
		name, table, strategy string
		pk                    []string
		wantErr               string
	}{
		{"merge needs matching_attribute", "people", "merge", []string{"email"}, "set matching_attribute"},
		{"replace needs matching_attribute", "people", "replace", []string{"email"}, "set matching_attribute"},
		{"merge needs primary key", "people?matching_attribute=email_addresses", "merge", nil, "pass --primary-key"},
		{"merge refuses record_id", "people?matching_attribute=record_id", "merge", []string{"id"}, "cannot match on record_id"},
		{"append refuses matching_attribute", "people?matching_attribute=email_addresses", "append", nil, "append always creates"},
		{"composite key", "people?matching_attribute=email_addresses", "merge", []string{"a", "b"}, "composite primary key"},
		{"unknown param", "people?external_id=x", "merge", []string{"a"}, "external_id"},
		{"unsupported strategy", "people", "scd2", nil, "does not support"},
		{"empty object", "?matching_attribute=x", "update", nil, "object slug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShaper(tt.table, tt.strategy, tt.pk, "", false)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	sh, err := parseShaper("attio.people", "update", nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if sh.object != "people" || sh.matchAttr != recordIDAttr || sh.matchColumn != recordIDAttr || !sh.defaultColumn {
		t.Fatalf("update defaults = %+v, want record_id matched from a record_id column", sh)
	}
	sh, err = parseShaper("people?matching_attribute=external_id", "delete", nil, "", false)
	if err != nil || sh.matchColumn != "external_id" || !sh.archive {
		t.Fatalf("delete shaper = %+v, %v", sh, err)
	}
}

func TestConnectChecksToken(t *testing.T) {
	f := newFake()
	f.scope = "record_permission:read object_configuration:read"
	server := httptest.NewServer(http.HandlerFunc(f.handle))
	defer server.Close()

	d := NewAttioDestination()
	d.baseURL = server.URL
	if err := d.Connect(context.Background(), "attio://?api_key=k"); err == nil || !strings.Contains(err.Error(), "can't write records") {
		t.Fatalf("read-only token: error = %v", err)
	}

	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		writeJSON(w, 200, map[string]interface{}{"active": false})
		return true
	}
	if err := d.Connect(context.Background(), "attio://?api_key=k"); err == nil || !strings.Contains(err.Error(), "invalid or has been revoked") {
		t.Fatalf("inactive token: error = %v", err)
	}
	if err := d.Connect(context.Background(), "attio://"); err == nil || !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("missing api_key: error = %v", err)
	}
}

func TestMergeUpsertsByMatchingAttribute(t *testing.T) {
	f := newFake()
	existing := f.seed("people", map[string][]string{"email_addresses": {"ada@example.com"}, "job_title": {"Engineer"}, "score": {"1"}})
	d := newDest(t, f)

	rows := stringBatch(map[string][]string{
		"email":     {"ADA@example.com", "bob@example.com", ""},
		"job_title": {"CTO", "", "Nobody"},
		"score":     {"", "7", ""},
	}, []string{"email", "job_title", "score"})
	if err := d.WriteParallel(context.Background(), rows, opts("people?matching_attribute=email_addresses", "merge", "email")); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if got := f.get("people", existing); got["job_title"][0] != "CTO" || got["score"] != nil {
		t.Fatalf("existing record = %v, want job_title updated and score cleared", got)
	}
	if f.count("people") != 3 {
		t.Fatalf("records = %d, want 3 (one updated, one upserted, one keyless created)", f.count("people"))
	}
	asserts := f.requestsMatching("PUT /objects/people/records?matching_attribute=email_addresses")
	if len(asserts) != 2 {
		t.Fatalf("asserts = %v, want 2", asserts)
	}
	for _, a := range asserts {
		if !strings.Contains(a, `"email_addresses":["`) {
			t.Fatalf("assert body %s: multiselect match value must be wrapped in a list", a)
		}
	}
	creates := f.requestsMatching("POST /objects/people/records? ")
	if len(creates) != 1 || strings.Contains(creates[0], `"score":[]`) {
		t.Fatalf("keyless create = %v, want one create without cleared nulls", creates)
	}
}

func TestMergeWithoutWriteNullsOmitsNulls(t *testing.T) {
	f := newFake()
	id := f.seed("people", map[string][]string{"email_addresses": {"ada@example.com"}, "score": {"1"}})
	d := newDest(t, f)
	o := opts("people?matching_attribute=email_addresses", "merge", "email")
	o.WriteNulls = false
	rows := stringBatch(map[string][]string{"email": {"ada@example.com"}, "score": {""}}, []string{"email", "score"})
	if err := d.Write(context.Background(), rows, o); err != nil {
		t.Fatal(err)
	}
	if got := f.get("people", id)["score"]; len(got) != 1 {
		t.Fatalf("score = %v, want it kept", got)
	}
}

func TestUpdateByRecordID(t *testing.T) {
	f := newFake()
	id := f.seed("people", map[string][]string{"job_title": {"old"}})
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{
		"RECORD_ID": {strings.ToUpper(id), "00000000-0000-0000-0000-999999999999", "not-a-uuid", ""},
		"job_title": {"new", "x", "y", "z"},
	}, []string{"RECORD_ID", "job_title"})
	err := d.Write(context.Background(), rows, opts("people", "update"))
	if err == nil {
		t.Fatal("expected rejects")
	}
	for _, want := range []string{"(NOT_FOUND) [record_id=00000000-0000-0000-0000-999999999999]", "(MALFORMED_ID) [record_id=not-a-uuid]"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if got := f.get("people", id)["job_title"]; got[0] != "new" {
		t.Fatalf("job_title = %v", got)
	}
	if f.count("people") != 1 {
		t.Fatal("update must never create")
	}
}

func TestUpdateByAttributeUpdatesEveryMatch(t *testing.T) {
	f := newFake()
	a := f.seed("people", map[string][]string{"job_title": {"eng"}, "score": {"1"}})
	b := f.seed("people", map[string][]string{"job_title": {"eng"}, "score": {"1"}})
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{"job_title": {"eng", "ghost"}, "score": {"5", "6"}}, []string{"job_title", "score"})
	err := d.Write(context.Background(), rows, opts("people?matching_attribute=job_title", "update"))
	if err == nil || !strings.Contains(err.Error(), `no people record found with job_title="ghost"`) {
		t.Fatalf("error = %v, want ghost rejected", err)
	}
	for _, id := range []string{a, b} {
		if got := f.get("people", id); got["score"][0] != "5" || got["job_title"][0] != "eng" {
			t.Fatalf("record %s = %v", id, got)
		}
	}
	for _, r := range f.requestsMatching("PUT /objects/people/records/") {
		if strings.Contains(r, "job_title") {
			t.Fatalf("match attribute written back: %s", r)
		}
	}
}

func TestDeleteByAttributeAndID(t *testing.T) {
	f := newFake()
	a := f.seed("people", map[string][]string{"external_id": {"E-1"}})
	b := f.seed("people", map[string][]string{"external_id": {"E-2"}})
	keep := f.seed("people", map[string][]string{"external_id": {"E-3"}})
	d := newDest(t, f)

	if err := d.Write(context.Background(), stringBatch(map[string][]string{"external_id": {"E-1"}}, []string{"external_id"}), opts("people?matching_attribute=external_id", "delete")); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), stringBatch(map[string][]string{"record_id": {b}}, []string{"record_id"}), opts("people", "delete")); err != nil {
		t.Fatal(err)
	}
	if f.get("people", a) != nil || f.get("people", b) != nil || f.get("people", keep) == nil {
		t.Fatalf("records left = %d, want only %s", f.count("people"), keep)
	}
}

func TestAppendCreatesAndLabelsRejects(t *testing.T) {
	f := newFake()
	f.seed("people", map[string][]string{"external_id": {"E-1"}})
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{"external_id": {"E-1", "E-2"}}, []string{"external_id"})
	err := d.Write(context.Background(), rows, opts("people", "append", "external_id"))
	if err == nil || !strings.Contains(err.Error(), "(uniqueness_conflict) [external_id=E-1]") || !strings.Contains(err.Error(), "hint: use --incremental-strategy merge") {
		t.Fatalf("error = %v", err)
	}
	if f.count("people") != 2 {
		t.Fatalf("records = %d, want E-2 created", f.count("people"))
	}
}

func TestRejectNamesAttributeIDs(t *testing.T) {
	f := newFake()
	id := fakeAttrID("people", 2)
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/records") {
			apiErr(w, 400, "missing_value", `Required value for attribute with ID "`+id+`" was not provided.`)
			return true
		}
		return false
	}
	d := newDest(t, f)
	err := d.Write(context.Background(), stringBatch(map[string][]string{"job_title": {"x"}}, []string{"job_title"}), opts("people", "append"))
	want := `attribute with ID "` + id + `" (people.job_title, "Job title", text) was not provided`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %s", err, want)
	}
}

func TestCreateIsNotRetriedOn5xx(t *testing.T) {
	f := newFake()
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/records") {
			apiErr(w, 500, "internal", "boom")
			return true
		}
		return false
	}
	d := newDest(t, f)
	err := d.Write(context.Background(), stringBatch(map[string][]string{"job_title": {"x"}}, []string{"job_title"}), opts("people", "append"))
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v", err)
	}
	if n := len(f.requestsMatching("POST /objects/people/records? ")); n != 1 {
		t.Fatalf("create sent %d times, want 1", n)
	}
}

func TestRejectModes(t *testing.T) {
	bad := func(f *fakeAttio) {
		f.hook = func(w http.ResponseWriter, r *http.Request, body string) bool {
			if r.Method == http.MethodPut && strings.Contains(body, "bad") {
				apiErr(w, 400, "validation_type", "bad value")
				return true
			}
			return false
		}
	}
	rows := func() <-chan source.RecordBatchResult {
		return stringBatch(map[string][]string{"ext": {"E-1", "E-2", "E-3"}, "job_title": {"ok", "bad", "ok"}}, []string{"ext", "job_title"})
	}
	table := "people?matching_attribute=external_id"

	f := newFake()
	bad(f)
	o := opts(table, "merge", "ext")
	o.RejectMode = "skip"
	if err := newDest(t, f).Write(context.Background(), rows(), o); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if f.count("people") != 2 {
		t.Fatalf("skip wrote %d, want 2", f.count("people"))
	}

	f = newFake()
	bad(f)
	err := newDest(t, f).Write(context.Background(), rows(), opts(table, "merge", "ext"))
	if err == nil || !strings.Contains(err.Error(), "(validation_type) [external_id=E-2] bad value") || f.count("people") != 2 {
		t.Fatalf("fail: err=%v count=%d", err, f.count("people"))
	}

	f = newFake()
	bad(f)
	o.RejectMode = "fail_fast"
	o.Parallelism = 1
	err = newDest(t, f).Write(context.Background(), rows(), o)
	if err == nil || !strings.Contains(err.Error(), "attio rejected a people record [external_id=E-2]") {
		t.Fatalf("fail_fast: %v", err)
	}
}

func TestAuthErrorAbortsUnderSkip(t *testing.T) {
	f := newFake()
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodPut {
			writeJSON(w, 403, map[string]interface{}{"status_code": 403, "type": "auth_error", "code": "unauthorized", "message": "no access"})
			return true
		}
		return false
	}
	o := opts("people?matching_attribute=external_id", "merge", "ext")
	o.RejectMode = "skip"
	err := newDest(t, f).Write(context.Background(), stringBatch(map[string][]string{"ext": {"E-1", "E-2"}}, []string{"ext"}), o)
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("error = %v, want the run aborted", err)
	}
}

func TestConcurrentWriteConflictIsRetried(t *testing.T) {
	f := newFake()
	var conflicts int
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodPut && conflicts < 2 {
			conflicts++
			apiErr(w, 409, conflictCode, "try again")
			return true
		}
		return false
	}
	err := newDest(t, f).Write(context.Background(), stringBatch(map[string][]string{"ext": {"E-1"}}, []string{"ext"}), opts("people?matching_attribute=external_id", "merge", "ext"))
	if err != nil || f.count("people") != 1 {
		t.Fatalf("err=%v count=%d", err, f.count("people"))
	}
}

func TestReplaceMirrors(t *testing.T) {
	f := newFake()
	kept := f.seed("people", map[string][]string{"external_id": {"e-1"}})
	stale := f.seed("people", map[string][]string{"external_id": {"E-9"}})
	noKey := f.seed("people", map[string][]string{"job_title": {"orphan"}})
	d := newDest(t, f)

	rows := stringBatch(map[string][]string{"ext": {"E-1", "E-2", ""}, "job_title": {"a", "b", "keyless"}}, []string{"ext", "job_title"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "replace", "ext")); err != nil {
		t.Fatal(err)
	}
	if f.get("people", kept) == nil || f.get("people", stale) != nil || f.get("people", noKey) != nil {
		t.Fatalf("mirror kept the wrong records")
	}
	if f.count("people") != 3 {
		t.Fatalf("records = %d, want E-1, E-2 and the keyless create", f.count("people"))
	}
}

func TestReplaceMirrorsCaseSensitiveTextKey(t *testing.T) {
	f := newFake()
	kept := f.seed("people", map[string][]string{"external_id": {"A-1"}})
	stale := f.seed("people", map[string][]string{"external_id": {"a-1"}})
	d := newDest(t, f)

	rows := stringBatch(map[string][]string{"ext": {"A-1"}}, []string{"ext"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "replace", "ext")); err != nil {
		t.Fatal(err)
	}
	if f.get("people", kept) == nil || f.get("people", stale) != nil {
		t.Fatalf("records left = %d, want only A-1", f.count("people"))
	}
}

func TestReplaceGuards(t *testing.T) {
	f := newFake()
	f.seed("people", map[string][]string{"external_id": {"E-9"}})
	d := newDest(t, f)
	if err := d.Write(context.Background(), emptyBatches(), opts("people?matching_attribute=external_id", "replace", "ext")); err != nil {
		t.Fatal(err)
	}
	if f.count("people") != 1 {
		t.Fatal("empty source must not sweep")
	}

	f.hook = func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPut && strings.Contains(body, "E-2") {
			apiErr(w, 400, "validation_type", "bad")
			return true
		}
		return false
	}
	rows := stringBatch(map[string][]string{"ext": {"E-1", "E-2"}}, []string{"ext"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "replace", "ext")); err == nil {
		t.Fatal("expected the reject to fail the run")
	}
	if len(f.findBy("people", "external_id", "E-9")) != 1 {
		t.Fatal("a failed replace must not sweep")
	}

	o := opts("people?matching_attribute=external_id", "replace", "ext")
	o.RejectMode = "skip"
	if err := d.Write(context.Background(), stringBatch(map[string][]string{"ext": {"E-2"}}, []string{"ext"}), o); err != nil {
		t.Fatal(err)
	}
	if len(f.findBy("people", "external_id", "E-9")) != 1 {
		t.Fatal("an all-rejected replace must not sweep")
	}
}

func prepare(t *testing.T, d *AttioDestination, table, strategy string, pk []string, cols ...string) error {
	t.Helper()
	sch := &schema.TableSchema{PrimaryKeys: pk}
	for _, c := range cols {
		sch.Columns = append(sch.Columns, schema.Column{Name: c, DataType: schema.TypeString})
	}
	return d.PrepareTable(context.Background(), destination.PrepareOptions{Table: table, Strategy: strategy, Schema: sch, PrimaryKeys: pk})
}

func TestPrepareTableValidates(t *testing.T) {
	d := newDest(t, newFake())
	tests := []struct {
		name, table, strategy string
		pk                    []string
		cols                  []string
		wantErr               string
	}{
		{"unknown object suggests slug", "Person", "append", nil, nil, "did you mean people"},
		{"unknown object lists slugs", "nope", "append", nil, nil, "available: companies, people"},
		{"missing match attribute", "people?matching_attribute=mail", "merge", []string{"e"}, nil, "not an attribute on people"},
		{"non-unique upsert key", "people?matching_attribute=job_title", "merge", []string{"e"}, nil, "is not unique"},
		{"unknown column", "people", "append", nil, []string{"job_title", "nickname"}, "[nickname]"},
		{"read-only column", "people", "append", nil, []string{"created_at"}, "read-only attributes on people: [created_at]"},
		{"dotted non-reference", "people", "append", nil, []string{"job_title.x"}, "not a record reference"},
		{"untyped multi-target reference", "people", "append", nil, []string{"related.domains"}, "name the object"},
		{"wrong target object", "people", "append", nil, []string{"company.people.email_addresses"}, "can point to companies"},
		{"bad name part", "people", "append", nil, []string{"name.middle_name"}, "name columns are"},
		{"link by missing attribute", "people", "append", nil, []string{"company.nope"}, "companies has no attribute nope"},
		{"link by non-unique attribute", "people", "append", nil, []string{"company.name"}, "name is not unique on companies"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := prepare(t, d, tt.table, tt.strategy, tt.pk, tt.cols...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	ok := []string{"EMAIL_ADDRESSES", "name.first_name", "name.last_name", "company.domains", "related.people.record_id", "record_id"}
	if err := prepare(t, d, "people", "append", nil, ok...); err != nil {
		t.Fatalf("valid columns: %v", err)
	}
	if err := prepare(t, d, "people?matching_attribute=email_addresses", "merge", []string{"customer_email"}, "customer_email", "job_title"); err != nil {
		t.Fatalf("valid merge: %v", err)
	}
}

func TestShapeValues(t *testing.T) {
	f := newFake()
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{
		"ext":                       {"E-1"},
		"name.first_name":           {"Ada"},
		"name.last_name":            {"Lovelace"},
		"company.domains":           {"acme.com"},
		"related.people.record_id":  {"00000000-0000-0000-0000-000000000042"},
		"related.Companies.domains": {"other.com"},
		"TAGS":                      {"vip"},
	}, []string{"ext", "name.first_name", "name.last_name", "company.domains", "related.people.record_id", "related.Companies.domains", "TAGS"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "merge", "ext")); err != nil {
		t.Fatal(err)
	}
	put := f.requestsMatching("PUT ")
	if len(put) != 1 {
		t.Fatalf("requests = %v", put)
	}
	body := put[0][strings.Index(put[0], "{"):]
	values := decodeValues([]byte(body))
	want := map[string]string{
		"name":        `{"first_name":"Ada","full_name":"Ada Lovelace","last_name":"Lovelace"}`,
		"company":     `[{"domains":[{"domain":"acme.com"}],"target_object":"companies"}]`,
		"tags":        `["vip"]`,
		"external_id": `"E-1"`,
	}
	for k, w := range want {
		b, _ := json.Marshal(values[k])
		if string(b) != w {
			t.Fatalf("%s = %s, want %s", k, b, w)
		}
	}
	related, _ := json.Marshal(values["related"])
	for _, w := range []string{`{"target_object":"people","target_record_id":"00000000-0000-0000-0000-000000000042"}`, `{"domains":[{"domain":"other.com"}],"target_object":"companies"}`} {
		if !strings.Contains(string(related), w) {
			t.Fatalf("related = %s, missing %s", related, w)
		}
	}
}

func TestCellValue(t *testing.T) {
	mem := memory.DefaultAllocator
	fb := array.NewFloat64Builder(mem)
	fb.AppendValues([]float64{1.5, math.NaN(), math.Inf(1)}, nil)
	floats := fb.NewArray()
	if v, ok := cellValue(floats, 0); !ok || v != json.Number("1.5") {
		t.Fatalf("1.5 = %v %v", v, ok)
	}
	for i := 1; i < 3; i++ {
		if _, ok := cellValue(floats, i); ok {
			t.Fatalf("non-finite float %d must be treated as null", i)
		}
	}

	tb := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Microsecond})
	tb.Append(arrow.Timestamp(time.Date(2024, 1, 2, 3, 4, 5, 6000, time.UTC).UnixMicro()))
	if v, _ := cellValue(tb.NewArray(), 0); v != "2024-01-02T03:04:05.000006Z" {
		t.Fatalf("timestamp = %v", v)
	}

	lb := array.NewListBuilder(mem, arrow.BinaryTypes.String)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	lb.Append(true)
	vb.Append("a")
	vb.Append("b")
	if v, _ := cellValue(lb.NewArray(), 0); fmt.Sprint(v) != "[a b]" {
		t.Fatalf("list = %v", v)
	}
}

func TestNumericMatchValue(t *testing.T) {
	sh := &shaper{matchAttr: "score", numericKey: true}
	for in, want := range map[string]string{"1001.00": "1001", "1001.50": "1001.5", "1.0000000000000000001": "1.0000000000000000001", "-0.25": "-0.25"} {
		if got := sh.matchValue(in); got != want {
			t.Fatalf("matchValue(%s) = %q, want %s", in, got, want)
		}
	}
	if got := storedValues(json.RawMessage(`[{"value": 1001}]`)); len(got) != 1 || got[0] != "1001" {
		t.Fatalf("storedValues = %v", got)
	}
}

func TestDescribeFailureStopsTheRun(t *testing.T) {
	f := newFake()
	f.seed("people", map[string][]string{"external_id": {"E-1"}})
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if strings.HasSuffix(r.URL.Path, "/attributes") {
			apiErr(w, 400, "invalid_request", "cannot read attributes")
			return true
		}
		return false
	}
	d := newDest(t, f)
	o := opts("people?matching_attribute=external_id", "replace", "external_id")
	sch := &schema.TableSchema{Columns: []schema.Column{{Name: "external_id"}}}
	if err := d.PrepareTable(context.Background(), destination.PrepareOptions{Table: o.Table, Strategy: o.Strategy, PrimaryKeys: o.PrimaryKeys, Schema: sch}); err == nil || !strings.Contains(err.Error(), "cannot describe the people object") {
		t.Fatalf("PrepareTable error = %v", err)
	}
	err := d.Write(context.Background(), stringBatch(map[string][]string{"external_id": {"E-2"}}, []string{"external_id"}), o)
	if err == nil || !strings.Contains(err.Error(), "cannot describe the people object") {
		t.Fatalf("Write error = %v", err)
	}
	if f.count("people") != 1 || len(f.requestsMatching("DELETE ")) != 0 {
		t.Fatalf("records = %d, deletes = %d; want nothing written", f.count("people"), len(f.requestsMatching("DELETE ")))
	}
}

func TestRetriedDeleteThatLandedIsNotRejected(t *testing.T) {
	f := newFake()
	id := f.seed("people", map[string][]string{"external_id": {"E-1"}})
	failed := false
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodDelete && !failed {
			failed = true
			delete(f.records["people"], id)
			apiErr(w, 502, "bad_gateway", "upstream timeout")
			return true
		}
		return false
	}
	d := newDest(t, f)
	err := d.Write(context.Background(), stringBatch(map[string][]string{"record_id": {id}}, []string{"record_id"}), opts("people", "delete"))
	if err != nil {
		t.Fatalf("delete that landed before the retry: %v", err)
	}
	if n := len(f.requestsMatching("DELETE ")); n != 2 {
		t.Fatalf("deletes sent = %d, want 2", n)
	}
}

func TestDeleteSendsEachRecordOnce(t *testing.T) {
	f := newFake()
	ada := f.seed("people", map[string][]string{"email_addresses": {"a@x.com", "b@x.com"}})
	bob := f.seed("people", map[string][]string{"email_addresses": {"bob@x.com"}})
	d := newDest(t, f)

	rows := stringBatch(map[string][]string{"email": {"a@x.com", "b@x.com", "bob@x.com", "bob@x.com"}}, []string{"email"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=email_addresses", "delete", "email")); err != nil {
		t.Fatalf("delete with repeated records: %v", err)
	}
	if f.get("people", ada) != nil || f.get("people", bob) != nil {
		t.Fatalf("records left = %d, want 0", f.count("people"))
	}
	if n := len(f.requestsMatching("DELETE ")); n != 2 {
		t.Fatalf("deletes sent = %d, want 2", n)
	}
}

func TestDeleteOfRecordGoneInEarlierBatchIsNotRejected(t *testing.T) {
	f := newFake()
	f.seed("people", map[string][]string{"email_addresses": {"a@x.com", "b@x.com"}})
	d := newDest(t, f)

	rows := make(chan source.RecordBatchResult, 2)
	for _, email := range []string{"a@x.com", "B@x.com"} {
		rows <- <-stringBatch(map[string][]string{"email": {email}}, []string{"email"})
	}
	close(rows)
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=email_addresses", "delete", "email")); err != nil {
		t.Fatalf("delete across batches: %v", err)
	}
	if n := len(f.requestsMatching("DELETE ")); n != 1 || f.count("people") != 0 {
		t.Fatalf("deletes sent = %d, records left = %d; want 1 and 0", n, f.count("people"))
	}
}

func TestRejectedDeleteIsReportedForEveryRow(t *testing.T) {
	f := newFake()
	f.seed("people", map[string][]string{"email_addresses": {"a@x.com", "b@x.com"}})
	f.hook = func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if r.Method == http.MethodDelete {
			apiErr(w, 400, "validation_type", "record is locked")
			return true
		}
		return false
	}
	d := newDest(t, f)

	rows := make(chan source.RecordBatchResult, 2)
	rows <- <-stringBatch(map[string][]string{"email": {"a@x.com", "b@x.com"}}, []string{"email"})
	rows <- <-stringBatch(map[string][]string{"email": {"B@x.com"}}, []string{"email"})
	close(rows)
	err := d.Write(context.Background(), rows, opts("people?matching_attribute=email_addresses", "delete", "email"))
	for _, want := range []string{"[email_addresses=a@x.com]", "[email_addresses=b@x.com]", "[email_addresses=B@x.com]", "attio rejected 3"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to contain %s", err, want)
		}
	}
	if n := len(f.requestsMatching("DELETE ")); n != 1 || f.count("people") != 1 {
		t.Fatalf("deletes sent = %d, records = %d; want 1 and the record kept", n, f.count("people"))
	}
}

func TestPartialNameIsWrittenWithoutWriteNulls(t *testing.T) {
	f := newFake()
	id := f.seed("people", map[string][]string{"external_id": {"E-1"}, "name": {"Ada Lovelace"}})
	d := newDest(t, f)
	o := opts("people?matching_attribute=external_id", "merge", "ext")
	o.WriteNulls = false

	rows := stringBatch(map[string][]string{"ext": {"E-1"}, "name.first_name": {"Augusta"}}, []string{"ext", "name.first_name"})
	if err := d.Write(context.Background(), rows, o); err != nil {
		t.Fatal(err)
	}
	if got := f.get("people", id)["name"]; len(got) != 1 || !strings.Contains(got[0], `"first_name":"Augusta"`) || !strings.Contains(got[0], `"last_name":""`) {
		t.Fatalf("name = %v, want Augusta with an empty last name", got)
	}
}

func TestNullNamePartsClearName(t *testing.T) {
	f := newFake()
	id := f.seed("people", map[string][]string{"external_id": {"E-1"}, "name": {"x"}})
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{"ext": {"E-1"}, "name.first_name": {""}, "name.last_name": {""}}, []string{"ext", "name.first_name", "name.last_name"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "merge", "ext")); err != nil {
		t.Fatal(err)
	}
	if got := f.get("people", id)["name"]; got != nil {
		t.Fatalf("name = %v, want cleared", got)
	}
}

func TestUpdateByCaseSensitiveKeyPrefersExactMatch(t *testing.T) {
	f := newFake()
	upper := f.seed("people", map[string][]string{"external_id": {"CASE-1"}})
	lower := f.seed("people", map[string][]string{"external_id": {"case-1"}})
	folded := f.seed("people", map[string][]string{"email_addresses": {"ada@example.com"}})
	d := newDest(t, f)
	rows := stringBatch(map[string][]string{"external_id": {"case-1"}, "job_title": {"lower only"}}, []string{"external_id", "job_title"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "update")); err != nil {
		t.Fatal(err)
	}
	if f.get("people", lower)["job_title"] == nil || f.get("people", upper)["job_title"] != nil {
		t.Fatalf("exact match must win: upper=%v lower=%v", f.get("people", upper), f.get("people", lower))
	}

	rows = stringBatch(map[string][]string{"job_title": {"LOWER ONLY"}, "score": {"3"}}, []string{"job_title", "score"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=job_title", "update")); err != nil {
		t.Fatalf("non-unique text key keeps the folded fallback: %v", err)
	}

	rows = stringBatch(map[string][]string{"external_id": {"Case-1"}, "job_title": {"neither"}}, []string{"external_id", "job_title"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=external_id", "update")); err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("text key without an exact match: err = %v, want NOT_FOUND", err)
	}

	rows = stringBatch(map[string][]string{"email_addresses": {"ADA@EXAMPLE.COM"}, "job_title": {"folded"}}, []string{"email_addresses", "job_title"})
	if err := d.Write(context.Background(), rows, opts("people?matching_attribute=email_addresses", "update")); err != nil {
		t.Fatal(err)
	}
	if got := f.get("people", folded)["job_title"]; len(got) != 1 || got[0] != "folded" {
		t.Fatalf("folded fallback: %v", got)
	}
}
