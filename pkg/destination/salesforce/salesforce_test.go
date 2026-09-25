package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// capture records the requests a test server received.
type capture struct {
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

func (c *capture) add(r recordedRequest) {
	c.mu.Lock()
	c.requests = append(c.requests, r)
	c.mu.Unlock()
}

func (c *capture) all() []recordedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recordedRequest(nil), c.requests...)
}

// byPath returns every request whose path ends with suffix.
func (c *capture) byPath(suffix string) []recordedRequest {
	var out []recordedRequest
	for _, r := range c.all() {
		if strings.HasSuffix(r.path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, body string) bool

// newDest spins up a fake Salesforce and returns a connected destination. The
// handler may claim a request; unclaimed ones get a generic success response.
func newDest(t *testing.T, cap *capture, handler handlerFunc) (*SalesforceDestination, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		cap.add(recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: body})
		w.Header().Set("Content-Type", "application/json")
		if handler != nil && handler(w, r, body) {
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	d := NewSalesforceDestination()
	if err := d.Connect(context.Background(), "salesforce://?access_token=tok&load_method=rest&domain="+server.URL); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d, server
}

// successResults answers a collections request with n successful results.
func successResults(w http.ResponseWriter, n int, idPrefix string) {
	out := make([]map[string]interface{}, n)
	for i := range out {
		out[i] = map[string]interface{}{"id": fmt.Sprintf("%s%03d", idPrefix, i), "success": true, "errors": []any{}}
	}
	b, _ := json.Marshal(out)
	_, _ = w.Write(b)
}

// countRecords returns how many records a collections request body carried.
func countRecords(t *testing.T, body string) int {
	t.Helper()
	var parsed struct {
		Records []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("failed to parse request body %q: %v", body, err)
	}
	return len(parsed.Records)
}

// stringBatch builds a single-batch channel from string columns.
func stringBatch(t *testing.T, cols map[string][]string, order []string) <-chan source.RecordBatchResult {
	t.Helper()
	fields := make([]arrow.Field, 0, len(order))
	for _, name := range order {
		fields = append(fields, arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true})
	}
	sch := arrow.NewSchema(fields, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, sch)
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
	rec := b.NewRecordBatch()

	ch := make(chan source.RecordBatchResult, 1)
	ch <- source.RecordBatchResult{Batch: rec}
	close(ch)
	return ch
}

func writeOpts(table, strategy string, pk []string) destination.WriteOptions {
	return destination.WriteOptions{
		Table:       table,
		Strategy:    strategy,
		PrimaryKeys: pk,
		Schema:      &schema.TableSchema{},
	}
}

func TestParseShaperMergeReplaceRequireExplicitKeys(t *testing.T) {
	for _, strategy := range []string{"merge", "replace"} {
		_, err := parseShaper("Contact", strategy, []string{"ext"}, "", false)
		if err == nil || !strings.Contains(err.Error(), "needs a match field") {
			t.Fatalf("%s without external_id: error = %v, want a missing-external_id error", strategy, err)
		}
		_, err = parseShaper("Contact?external_id=Ext__c", strategy, nil, "", false)
		if err == nil || !strings.Contains(err.Error(), "pass --primary-key") {
			t.Fatalf("%s without --primary-key: error = %v, want a missing-source-column error", strategy, err)
		}
	}
}

func TestSourceColumnDefaultsToMatchFieldName(t *testing.T) {
	sh, err := parseShaper("Contact?external_id=Department", "update", nil, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if sh.idColumn != "Department" {
		t.Fatalf("update idColumn = %q, want Department", sh.idColumn)
	}
	sh, err = parseShaper("Contact?external_id=Ext__c", "delete", nil, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if sh.idColumn != "Ext__c" {
		t.Fatalf("delete idColumn = %q, want Ext__c", sh.idColumn)
	}
}

func TestParseShaperMergeRejectsRecordID(t *testing.T) {
	_, err := parseShaper("Contact?external_id=Id", "merge", []string{"sf_id"}, "", false)
	if err == nil || !strings.Contains(err.Error(), "cannot create a record at a supplied Id") {
		t.Fatalf("error = %v, want a rejection of external_id=Id", err)
	}
}

func TestParseShaperReplaceRejectsRecordID(t *testing.T) {
	_, err := parseShaper("Contact?external_id=Id", "replace", []string{"sf_id"}, "", false)
	if err == nil || !strings.Contains(err.Error(), "cannot create a record at a supplied Id") {
		t.Fatalf("error = %v, want a rejection of external_id=Id", err)
	}
}

func TestParseShaperUpdateDefaultsToRecordID(t *testing.T) {
	sh, err := parseShaper("Contact", "update", nil, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if sh.idField != recordIDField || sh.idColumn != recordIDField {
		t.Fatalf("idField=%q idColumn=%q, want both %q", sh.idField, sh.idColumn, recordIDField)
	}
	if !sh.updateOnly || sh.resolvesKeys() {
		t.Fatalf("update by Id should not resolve keys: %+v", sh)
	}
}

func TestParseShaperDeleteDefaultsToRecordID(t *testing.T) {
	sh, err := parseShaper("Contact", "delete", nil, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if !sh.archive || sh.idField != recordIDField {
		t.Fatalf("unexpected shaper: %+v", sh)
	}
}

func TestParseShaperRejectsCompositeKey(t *testing.T) {
	_, err := parseShaper("Contact?external_id=Ext__c", "merge", []string{"a", "b"}, "", false)
	if err == nil || !strings.Contains(err.Error(), "composite primary key") {
		t.Fatalf("error = %v, want a composite-key rejection", err)
	}
}

func TestParseShaperExternalID(t *testing.T) {
	sh, err := parseShaper("Contact?external_id=Ext__c", "merge", []string{"ext"}, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if sh.idField != "Ext__c" {
		t.Fatalf("idField = %q, want Ext__c", sh.idField)
	}
	// external_id is the only match parameter; another destination's spelling
	// must be rejected rather than silently ignored.
	if _, err := parseShaper("Contact?id_property=Ext__c", "merge", []string{"ext"}, "", false); err == nil {
		t.Fatal("id_property should be rejected as an unknown table parameter")
	}
}

func TestParseShaperStripsSchemaQualifier(t *testing.T) {
	sh, err := parseShaper("salesforce.Contact", "append", nil, "", false)
	if err != nil {
		t.Fatalf("parseShaper returned error: %v", err)
	}
	if sh.sobject != "Contact" {
		t.Fatalf("sobject = %q, want Contact", sh.sobject)
	}
}

func TestMergeUpsertsAgainstExternalIDEndpoint(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1", "A-2"},
		"FirstName": {"Ada", "Grace"},
	}, []string{"ext_id", "FirstName"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reqs := cap.byPath("/composite/sobjects/Contact/Ext__c")
	if len(reqs) != 1 {
		t.Fatalf("got %d upsert request(s), want 1 (all: %+v)", len(reqs), cap.all())
	}
	if reqs[0].method != http.MethodPatch {
		t.Fatalf("method = %q, want PATCH", reqs[0].method)
	}

	var parsed struct {
		AllOrNone bool                     `json:"allOrNone"`
		Records   []map[string]interface{} `json:"records"`
	}
	if err := json.Unmarshal([]byte(reqs[0].body), &parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	if parsed.AllOrNone {
		t.Fatal("allOrNone = true, want false so valid rows still land")
	}
	if len(parsed.Records) != 2 {
		t.Fatalf("got %d records, want 2", len(parsed.Records))
	}
	// The External ID must ride along in the body under its Salesforce name, not
	// the source column name.
	if parsed.Records[0]["Ext__c"] != "A-1" {
		t.Fatalf("Ext__c = %v, want A-1 (record: %v)", parsed.Records[0]["Ext__c"], parsed.Records[0])
	}
	if _, ok := parsed.Records[0]["ext_id"]; ok {
		t.Fatalf("source key column leaked into the record: %v", parsed.Records[0])
	}
	if parsed.Records[0]["FirstName"] != "Ada" {
		t.Fatalf("FirstName = %v, want Ada", parsed.Records[0]["FirstName"])
	}
	if parsed.Records[0]["attributes"].(map[string]interface{})["type"] != "Contact" {
		t.Fatalf("attributes = %v, want type Contact", parsed.Records[0]["attributes"])
	}
}

func TestAppendCreatesRecords(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPost {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"LastName": {"Lovelace"}}, []string{"LastName"})
	if err := d.Write(context.Background(), records, writeOpts("Contact", "append", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || reqs[0].method != http.MethodPost {
		t.Fatalf("got %+v, want a single POST to /composite/sobjects", reqs)
	}
}

func TestUpdateByRecordIDPatchesCollection(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"Id":        {"003000000000001"},
		"FirstName": {"Ada"},
	}, []string{"Id", "FirstName"})

	if err := d.Write(context.Background(), records, writeOpts("Contact", "update", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || reqs[0].method != http.MethodPatch {
		t.Fatalf("got %+v, want a single PATCH to /composite/sobjects", reqs)
	}
	var parsed struct {
		Records []map[string]interface{} `json:"records"`
	}
	_ = json.Unmarshal([]byte(reqs[0].body), &parsed)
	if parsed.Records[0]["Id"] != "003000000000001" {
		t.Fatalf("Id = %v, want the source id", parsed.Records[0]["Id"])
	}
}

func TestUpdateByExternalFieldResolvesEveryMatch(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if strings.HasSuffix(r.URL.Path, "/query") {
			// The one source value names two records, so both must be updated.
			_, _ = w.Write([]byte(`{"done":true,"records":[
				{"Id":"003000000000001","Company__c":"Acme"},
				{"Id":"003000000000002","Company__c":"Acme"}
			]}`))
			return true
		}
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"company":  {"Acme"},
		"Industry": {"Tech"},
	}, []string{"company", "Industry"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Company__c", "update", []string{"company"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	queries := cap.byPath("/query")
	if len(queries) != 1 {
		t.Fatalf("got %d quer(ies), want 1", len(queries))
	}
	if !strings.Contains(queries[0].query, "Company__c+IN+%28%27Acme%27%29") &&
		!strings.Contains(queries[0].query, "Company__c%20IN%20%28%27Acme%27%29") {
		t.Fatalf("query = %q, want an IN filter on Company__c", queries[0].query)
	}

	patches := cap.byPath("/composite/sobjects")
	if len(patches) != 1 {
		t.Fatalf("got %d patch request(s), want 1", len(patches))
	}
	var parsed struct {
		Records []map[string]interface{} `json:"records"`
	}
	_ = json.Unmarshal([]byte(patches[0].body), &parsed)
	if len(parsed.Records) != 2 {
		t.Fatalf("got %d records, want 2 (one per matched id)", len(parsed.Records))
	}
	ids := []string{parsed.Records[0]["Id"].(string), parsed.Records[1]["Id"].(string)}
	if ids[0] != "003000000000001" || ids[1] != "003000000000002" {
		t.Fatalf("ids = %v, want both matched records", ids)
	}
	// The row is addressed by Id, so the match field is not written back — it may
	// well be read-only.
	if _, ok := parsed.Records[0]["Company__c"]; ok {
		t.Fatalf("record = %v, want the match field left out of the update body", parsed.Records[0])
	}
}

func TestUpdateUnmatchedValueIsRejected(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if strings.HasSuffix(r.URL.Path, "/query") {
			_, _ = w.Write([]byte(`{"done":true,"records":[]}`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"company":  {"Nowhere"},
		"Industry": {"Tech"},
	}, []string{"company", "Industry"})

	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Company__c", "update", []string{"company"}))
	if err == nil || !strings.Contains(err.Error(), "no Contact found with Company__c") {
		t.Fatalf("error = %v, want a not-found rejection", err)
	}
	if len(cap.byPath("/composite/sobjects")) != 0 {
		t.Fatal("an unmatched row must not be written")
	}
}

func TestRejectModeSkipSucceedsWithRejections(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			_, _ = w.Write([]byte(`[
				{"id":"003000000000001","success":true,"errors":[]},
				{"success":false,"errors":[{"statusCode":"REQUIRED_FIELD_MISSING","message":"Required fields are missing: [LastName]","fields":["LastName"]}]}
			]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1", "A-2"},
		"FirstName": {"Ada", "Grace"},
	}, []string{"ext_id", "FirstName"})

	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})
	opts.RejectMode = "skip"
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error under --reject-mode=skip: %v", err)
	}
}

func TestRejectModeFailReportsRejections(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			_, _ = w.Write([]byte(`[
				{"id":"003000000000001","success":true,"errors":[]},
				{"success":false,"errors":[{"statusCode":"REQUIRED_FIELD_MISSING","message":"missing LastName","fields":["LastName"]}]}
			]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1", "A-2"},
		"FirstName": {"Ada", "Grace"},
	}, []string{"ext_id", "FirstName"})

	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"}))
	if err == nil {
		t.Fatal("Write returned nil, want the rejection report")
	}
	// The reject must name the record it belongs to, by position in the batch.
	if !strings.Contains(err.Error(), "Ext__c=A-2") {
		t.Fatalf("error = %v, want it to identify the second record", err)
	}
	if !strings.Contains(err.Error(), "REQUIRED_FIELD_MISSING") {
		t.Fatalf("error = %v, want the Salesforce status code", err)
	}
}

func TestNon200AbortsRegardlessOfRejectMode(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`[{"errorCode":"INVALID_FIELD","message":"No such column 'Nope__c'"}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":  {"A-1"},
		"Nope__c": {"x"},
	}, []string{"ext_id", "Nope__c"})

	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})
	opts.RejectMode = "skip"
	err := d.Write(context.Background(), records, opts)
	if err == nil || !strings.Contains(err.Error(), "INVALID_FIELD") {
		t.Fatalf("error = %v, want a structural failure that skip cannot tolerate", err)
	}
}

// contactDescribe answers the describe that delete by Id reads the id prefix from.
func contactDescribe(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/describe") {
		return false
	}
	_, _ = w.Write([]byte(`{"keyPrefix":"003","fields":[{"name":"Id","type":"id"}]}`))
	return true
}

func TestDeleteByRecordID(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if contactDescribe(w, r) {
			return true
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`[{"id":"003000000000001","success":true,"errors":[]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"Id": {"003000000000001"}}, []string{"Id"})
	if err := d.Write(context.Background(), records, writeOpts("Contact", "delete", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || reqs[0].method != http.MethodDelete {
		t.Fatalf("got %+v, want a single DELETE", reqs)
	}
	if !strings.Contains(reqs[0].query, "ids=003000000000001") {
		t.Fatalf("query = %q, want the record id", reqs[0].query)
	}
	if !strings.Contains(reqs[0].query, "allOrNone=false") {
		t.Fatalf("query = %q, want allOrNone=false", reqs[0].query)
	}
}

func TestDeleteMatchesDefaultIDColumnCaseInsensitively(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if contactDescribe(w, r) {
			return true
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`[{"id":"003000000000001","success":true,"errors":[]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"id": {"003000000000001"}}, []string{"id"})
	if err := d.Write(context.Background(), records, writeOpts("Contact", "delete", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || !strings.Contains(reqs[0].query, "ids=003000000000001") {
		t.Fatalf("got %+v, want a DELETE of the lowercase id column's value", reqs)
	}
}

func TestDeleteRejectsMalformedIDLocally(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if contactDescribe(w, r) {
			return true
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`[{"id":"003000000000001","success":true,"errors":[]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"Id": {"a,b", "003000000000001"}}, []string{"Id"})
	opts := writeOpts("Contact", "delete", nil)
	opts.RejectMode = "skip"
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || strings.Contains(reqs[0].query, "a%2Cb") || strings.Contains(reqs[0].query, "a,b") {
		t.Fatalf("got %+v, want one DELETE without the malformed id", reqs)
	}
}

func TestDeleteRejectsIDOfAnotherSObject(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/describe"):
			_, _ = w.Write([]byte(`{"keyPrefix":"00Q","fields":[{"name":"Id","type":"id"}]}`))
			return true
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`[{"id":"00Q000000000001","success":true,"errors":[]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"Id": {"003000000000001", "00Q000000000001"}}, []string{"Id"})
	opts := writeOpts("Lead", "delete", nil)
	opts.RejectMode = "skip"
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || strings.Contains(reqs[0].query, "003000000000001") {
		t.Fatalf("got %+v, want one DELETE without the Contact id", reqs)
	}
}

func TestDeleteByIDRefusesWithoutIDPrefix(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, nil)

	records := stringBatch(t, map[string][]string{"Id": {"003000000000001"}}, []string{"Id"})
	err := d.Write(context.Background(), records, writeOpts("Contact", "delete", nil))
	if err == nil || !strings.Contains(err.Error(), "refusing to delete by id") {
		t.Fatalf("error = %v, want delete by id refused when the describe is unavailable", err)
	}
	if reqs := cap.byPath("/composite/sobjects"); len(reqs) != 0 {
		t.Fatalf("got %+v, want no DELETE", reqs)
	}
}

func TestDeleteTreatsAlreadyDeletedAsSuccess(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if contactDescribe(w, r) {
			return true
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`[{"success":false,"errors":[{"statusCode":"ENTITY_IS_DELETED","message":"entity is deleted"}]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{"Id": {"003000000000001"}}, []string{"Id"})
	if err := d.Write(context.Background(), records, writeOpts("Contact", "delete", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
}

func TestReplaceMirrorDeletesRecordsAbsentFromSource(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/query"):
			_, _ = w.Write([]byte(`{"done":true,"records":[
				{"Id":"003000000000001","Ext__c":"A-1"},
				{"Id":"003000000000009","Ext__c":"GONE"},
				{"Id":"003000000000010","Ext__c":null}
			]}`))
			return true
		case r.Method == http.MethodPatch:
			successResults(w, countRecords(t, body), "003")
			return true
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`[{"id":"003000000000009","success":true,"errors":[]},{"id":"003000000000010","success":true,"errors":[]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1"},
		"FirstName": {"Ada"},
	}, []string{"ext_id", "FirstName"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "replace", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	deletes := cap.byPath("/composite/sobjects")
	var del *recordedRequest
	for i := range deletes {
		if deletes[i].method == http.MethodDelete {
			del = &deletes[i]
		}
	}
	if del == nil {
		t.Fatalf("no delete issued; requests: %+v", cap.all())
	}
	ids := del.query
	if strings.Contains(ids, "003000000000001") {
		t.Fatalf("query = %q, must not delete a record present in the source", ids)
	}
	if !strings.Contains(ids, "003000000000009") {
		t.Fatalf("query = %q, want the stale record deleted", ids)
	}
	// The upsert returned id 003000 for the written row; a record whose match
	// value is empty is still stale and gets swept.
	if !strings.Contains(ids, "003000000000010") {
		t.Fatalf("query = %q, want the value-less record swept too", ids)
	}
}

func TestReplaceMirrorSkipsSweepWhenSourceIsEmpty(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, nil)

	ch := make(chan source.RecordBatchResult)
	close(ch)
	if err := d.Write(context.Background(), ch, writeOpts("Contact?external_id=Ext__c", "replace", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	for _, r := range cap.all() {
		if r.method == http.MethodDelete {
			t.Fatalf("an empty source must not delete anything; requests: %+v", cap.all())
		}
	}
}

func TestReplaceMirrorSkipsSweepWhenRowsWereRejected(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/query"):
			_, _ = w.Write([]byte(`{"done":true,"records":[{"Id":"003000000000009","Ext__c":"GONE"}]}`))
			return true
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`[{"success":false,"errors":[{"statusCode":"REQUIRED_FIELD_MISSING","message":"missing LastName"}]}]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1"},
		"FirstName": {"Ada"},
	}, []string{"ext_id", "FirstName"})

	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "replace", []string{"ext_id"}))
	if err == nil {
		t.Fatal("Write returned nil, want the rejection report")
	}
	for _, r := range cap.all() {
		if r.method == http.MethodDelete {
			t.Fatalf("a failed mirror run must not delete records; requests: %+v", cap.all())
		}
	}
}

func TestBatchesAreChunkedAtTheCollectionLimit(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	n := batchLimit + 5
	ext := make([]string, n)
	names := make([]string, n)
	for i := range ext {
		ext[i] = fmt.Sprintf("A-%d", i)
		names[i] = "n"
	}
	records := stringBatch(t, map[string][]string{"ext_id": ext, "FirstName": names}, []string{"ext_id", "FirstName"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	reqs := cap.byPath("/composite/sobjects/Contact/Ext__c")
	if len(reqs) != 2 {
		t.Fatalf("got %d request(s), want 2 chunks", len(reqs))
	}
	if got := countRecords(t, reqs[0].body); got != batchLimit {
		t.Fatalf("first chunk carried %d records, want %d", got, batchLimit)
	}
	if got := countRecords(t, reqs[1].body); got != 5 {
		t.Fatalf("second chunk carried %d records, want 5", got)
	}
}

func TestMissingMatchColumnFailsFast(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, nil)

	records := stringBatch(t, map[string][]string{"FirstName": {"Ada"}}, []string{"FirstName"})
	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"}))
	if err == nil || !strings.Contains(err.Error(), `id column "ext_id" not found`) {
		t.Fatalf("error = %v, want a missing-column error", err)
	}
}

func TestWriteNullsClearsFields(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1"},
		"FirstName": {""},
	}, []string{"ext_id", "FirstName"})

	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})
	opts.WriteNulls = true
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	body := cap.byPath("/composite/sobjects/Contact/Ext__c")[0].body
	if !strings.Contains(body, `"FirstName":null`) {
		t.Fatalf("body = %s, want an explicit null to clear the field", body)
	}
}

func TestWriteNullsOffOmitsFields(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":    {"A-1"},
		"FirstName": {""},
	}, []string{"ext_id", "FirstName"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	body := cap.byPath("/composite/sobjects/Contact/Ext__c")[0].body
	if strings.Contains(body, "FirstName") {
		t.Fatalf("body = %s, want the null field omitted", body)
	}
}

func TestPrepareTableRejectsUnknownAndReadOnlyColumns(t *testing.T) {
	describe := `{"fields":[
		{"name":"Id","createable":false,"updateable":false},
		{"name":"Ext__c","createable":true,"updateable":true,"externalId":true,"idLookup":true},
		{"name":"FirstName","createable":true,"updateable":true},
		{"name":"CreatedDate","createable":false,"updateable":false},
		{"name":"AccountId","createable":true,"updateable":true,"relationshipName":"Account"},
		{"name":"CampaignId","createable":true,"updateable":false,"relationshipName":"Campaign"},
		{"name":"Birthdate","type":"date","createable":true,"updateable":true}
	]}`
	prepare := func(opts destination.PrepareOptions) error {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		return d.PrepareTable(context.Background(), opts)
	}

	t.Run("non-updateable lookup column", func(t *testing.T) {
		err := prepare(destination.PrepareOptions{
			Table:       "Contact?external_id=Ext__c",
			Strategy:    "merge",
			PrimaryKeys: []string{"ext_id"},
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "ext_id"}, {Name: "Campaign.Ext__c"}}},
		})
		if err == nil || !strings.Contains(err.Error(), "Campaign.Ext__c") {
			t.Fatalf("error = %v, want the non-updateable lookup reported", err)
		}
		err = prepare(destination.PrepareOptions{
			Table:    "Contact",
			Strategy: "append",
			Schema:   &schema.TableSchema{Columns: []schema.Column{{Name: "Campaign.Ext__c"}}},
		})
		if err != nil {
			t.Fatalf("append: unexpected error %v", err)
		}
	})

	t.Run("date match field", func(t *testing.T) {
		err := prepare(destination.PrepareOptions{
			Table:    "Contact?external_id=Birthdate",
			Strategy: "update",
			Schema:   &schema.TableSchema{Columns: []schema.Column{{Name: "Birthdate"}}},
		})
		if err == nil || !strings.Contains(err.Error(), "date fields are not supported") {
			t.Fatalf("error = %v, want the date match field rejected", err)
		}
	})

	t.Run("unknown column", func(t *testing.T) {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		err := d.PrepareTable(context.Background(), destination.PrepareOptions{
			Table:       "Contact?external_id=Ext__c",
			Strategy:    "merge",
			PrimaryKeys: []string{"ext_id"},
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "ext_id"}, {Name: "Nope__c"}}},
		})
		if err == nil || !strings.Contains(err.Error(), "Nope__c") {
			t.Fatalf("error = %v, want the unknown column reported", err)
		}
	})

	t.Run("read-only column", func(t *testing.T) {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		err := d.PrepareTable(context.Background(), destination.PrepareOptions{
			Table:       "Contact?external_id=Ext__c",
			Strategy:    "merge",
			PrimaryKeys: []string{"ext_id"},
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "ext_id"}, {Name: "CreatedDate"}}},
		})
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("error = %v, want the read-only column reported", err)
		}
	})

	t.Run("Id and relationship columns accepted", func(t *testing.T) {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		err := d.PrepareTable(context.Background(), destination.PrepareOptions{
			Table:       "Contact?external_id=Ext__c",
			Strategy:    "merge",
			PrimaryKeys: []string{"ext_id"},
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "ext_id"}, {Name: "FirstName"}, {Name: "Id"}, {Name: "Account.Ext__c"},
			}},
		})
		if err != nil {
			t.Fatalf("PrepareTable returned error: %v", err)
		}
	})

	t.Run("non external id upsert key", func(t *testing.T) {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		err := d.PrepareTable(context.Background(), destination.PrepareOptions{
			Table:       "Contact?external_id=FirstName",
			Strategy:    "merge",
			PrimaryKeys: []string{"name"},
			Schema:      &schema.TableSchema{Columns: []schema.Column{{Name: "name"}}},
		})
		if err == nil || !strings.Contains(err.Error(), "not an External ID field") {
			t.Fatalf("error = %v, want an External-ID requirement error", err)
		}
	})
}

func TestRelationshipColumnNests(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"ext_id":         {"A-1"},
		"Account.Ext__c": {"ACC-9"},
	}, []string{"ext_id", "Account.Ext__c"})

	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext_id"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	body := cap.byPath("/composite/sobjects/Contact/Ext__c")[0].body
	var parsed struct {
		Records []map[string]interface{} `json:"records"`
	}
	_ = json.Unmarshal([]byte(body), &parsed)
	account, ok := parsed.Records[0]["Account"].(map[string]interface{})
	if !ok || account["Ext__c"] != "ACC-9" {
		t.Fatalf("record = %v, want Account nested with its external id", parsed.Records[0])
	}
}

func TestSOQLChunksRespectLengthAndCount(t *testing.T) {
	values := make([]string, batchLimit+10)
	for i := range values {
		values[i] = "v"
	}
	chunks := soqlChunks(values, 50)
	if len(chunks) != 2 || len(chunks[0]) != batchLimit {
		t.Fatalf("chunks = %d with first of %d, want 2 with first of %d", len(chunks), len(chunks[0]), batchLimit)
	}

	long := []string{strings.Repeat("x", soqlMaxQueryLen), strings.Repeat("y", soqlMaxQueryLen)}
	if got := len(soqlChunks(long, 50)); got != 2 {
		t.Fatalf("long values produced %d chunk(s), want 2", got)
	}
}

func TestEscapeSOQL(t *testing.T) {
	if got := escapeSOQL(`O'Brien\x`); got != `O\'Brien\\x` {
		t.Fatalf("escapeSOQL = %q, want quote and backslash escaped", got)
	}
}

func TestFieldValueTypes(t *testing.T) {
	sch := arrow.NewSchema([]arrow.Field{
		{Name: "b", Type: arrow.FixedWidthTypes.Boolean},
		{Name: "i", Type: arrow.PrimitiveTypes.Int64},
		{Name: "f", Type: arrow.PrimitiveTypes.Float64},
		{Name: "t", Type: &arrow.TimestampType{Unit: arrow.Microsecond}},
		{Name: "d", Type: arrow.FixedWidthTypes.Date32},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, sch)
	defer b.Release()
	b.Field(0).(*array.BooleanBuilder).Append(true)
	b.Field(1).(*array.Int64Builder).Append(42)
	b.Field(2).(*array.Float64Builder).Append(1.5)
	b.Field(3).(*array.TimestampBuilder).Append(arrow.Timestamp(1_700_000_000_123_456))
	b.Field(4).(*array.Date32Builder).Append(arrow.Date32FromTime(mustDate(t, "2024-01-15")))
	rec := b.NewRecordBatch()
	defer rec.Release()

	want := []interface{}{true, int64(42), json.Number("1.5"), "2023-11-14T22:13:20.123Z", "2024-01-15"}
	for i, expected := range want {
		got, ok := fieldValue(rec.Column(i), 0)
		if !ok {
			t.Fatalf("column %d returned ok=false", i)
		}
		if got != expected {
			t.Fatalf("column %d = %#v, want %#v", i, got, expected)
		}
	}
}

func TestDestinationCapabilities(t *testing.T) {
	d := NewSalesforceDestination()
	if !destination.IsReverseETL(d) {
		t.Fatal("salesforce must be a reverse-ETL destination")
	}
	if !destination.RequiresExplicitStrategy(d) {
		t.Fatal("salesforce must require an explicit --incremental-strategy")
	}
	if d.SupportsAtomicSwap() {
		t.Fatal("salesforce has no atomic swap")
	}
	if d.GetScheme() != "salesforce" {
		t.Fatalf("GetScheme = %q", d.GetScheme())
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", s, err)
	}
	return ts
}

func TestResolveMapPrefersExactThenFolds(t *testing.T) {
	m := newResolveMap()
	m.add("ABC", "001")
	m.add("abc", "002")

	// A value stored under both cases resolves to its own record, not both: a
	// case-sensitive External ID keeps distinct records distinct.
	if got := m.lookup("ABC"); len(got) != 1 || got[0] != "001" {
		t.Fatalf("lookup(ABC) = %v, want [001]", got)
	}
	// A source value Salesforce stored in a different case still correlates,
	// because SOQL matched it case-insensitively.
	if got := m.lookup("AbC"); len(got) != 2 {
		t.Fatalf("lookup(AbC) = %v, want both records via the folded key", got)
	}
}

const contactDescribeWithAccount = `{"fields":[
	{"name":"Id","idLookup":true},
	{"name":"Ext__c","externalId":true,"idLookup":true,"createable":true,"updateable":true},
	{"name":"LastName","createable":true,"updateable":true},
	{"name":"AccountId","createable":true,"updateable":true,"relationshipName":"Account"}
]}`

// upsertBodies runs a merge of the given rows and returns each record sent.
func upsertBodies(t *testing.T, cols map[string][]string, order []string, writeNulls bool) []map[string]interface{} {
	t.Helper()
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if strings.HasSuffix(r.URL.Path, "/describe") {
			_, _ = w.Write([]byte(contactDescribeWithAccount))
			return true
		}
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})
	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})
	opts.WriteNulls = writeNulls
	if err := d.Write(context.Background(), stringBatch(t, cols, order), opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	reqs := cap.byPath("/composite/sobjects/Contact/Ext__c")
	if len(reqs) != 1 {
		t.Fatalf("got %d upsert request(s), want 1", len(reqs))
	}
	var parsed struct {
		Records []map[string]interface{} `json:"records"`
	}
	if err := json.Unmarshal([]byte(reqs[0].body), &parsed); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}
	return parsed.Records
}

func TestNullRelationshipCellClearsTheLookupField(t *testing.T) {
	recs := upsertBodies(t, map[string][]string{
		"ext":            {"C-1", "C-2"},
		"Account.Ext__c": {"", "ACC-2"},
	}, []string{"ext", "Account.Ext__c"}, true)

	// A nested {"Account": {"Ext__c": null}} is rejected by Salesforce with
	// MISSING_ARGUMENT, so the null must become AccountId: null.
	if _, nested := recs[0]["Account"]; nested {
		t.Fatalf("record = %v, must not send a nested null relationship", recs[0])
	}
	if v, ok := recs[0]["AccountId"]; !ok || v != nil {
		t.Fatalf("record = %v, want AccountId: null to unlink", recs[0])
	}
	account, ok := recs[1]["Account"].(map[string]interface{})
	if !ok || account["Ext__c"] != "ACC-2" {
		t.Fatalf("record = %v, want the non-null row still linked by external id", recs[1])
	}
	if _, cleared := recs[1]["AccountId"]; cleared {
		t.Fatalf("record = %v, a linked row must not also clear AccountId", recs[1])
	}
}

func TestNullRelationshipCellIsOmittedWithoutWriteNulls(t *testing.T) {
	recs := upsertBodies(t, map[string][]string{
		"ext":            {"C-1", "C-2"},
		"Account.Ext__c": {"", "ACC-2"},
	}, []string{"ext", "Account.Ext__c"}, false)
	if _, ok := recs[0]["AccountId"]; ok {
		t.Fatalf("record = %v, --write-nulls=false must leave the link untouched", recs[0])
	}
	if _, ok := recs[0]["Account"]; ok {
		t.Fatalf("record = %v, --write-nulls=false must leave the link untouched", recs[0])
	}
}

func TestExplicitLookupValueWinsOverNullRelationshipCell(t *testing.T) {
	recs := upsertBodies(t, map[string][]string{
		"ext":            {"C-1"},
		"AccountId":      {"001000000000001"},
		"Account.Ext__c": {""},
	}, []string{"ext", "AccountId", "Account.Ext__c"}, true)
	if recs[0]["AccountId"] != "001000000000001" {
		t.Fatalf("record = %v, a set AccountId must not be cleared by a null dotted cell", recs[0])
	}
}

func TestUnknownSObjectSuggestsTheAPIName(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sobjects/Product/describe"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`[{"errorCode":"NOT_FOUND","message":"The requested resource does not exist"}]`))
			return true
		case strings.HasSuffix(r.URL.Path, "/sobjects"):
			_, _ = w.Write([]byte(`{"sobjects":[
				{"name":"Product2","label":"Product","labelPlural":"Products"},
				{"name":"Invoice__c","label":"Invoice","labelPlural":"Invoices"}
			]}`))
			return true
		}
		return false
	})
	err := d.PrepareTable(context.Background(), destination.PrepareOptions{
		Table:    "Product",
		Strategy: "append",
		Schema:   &schema.TableSchema{Columns: []schema.Column{{Name: "Name"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `"Product" is the label of Product2`) {
		t.Fatalf("error = %v, want a fail-fast pointing at Product2", err)
	}
}

func noLockBackoff(t *testing.T) {
	t.Helper()
	prev := lockBackoff
	lockBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { lockBackoff = prev })
}

// lockedThenOK fails the named external id with UNABLE_TO_LOCK_ROW for the first
// `times` requests it appears in, and succeeds every other record.
func lockedThenOK(t *testing.T, lockedKey string, times int) (handlerFunc, *[]int) {
	t.Helper()
	var mu sync.Mutex
	seenLocked := 0
	var sizes []int
	return func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method != http.MethodPatch {
			return false
		}
		var parsed struct {
			Records []map[string]interface{} `json:"records"`
		}
		_ = json.Unmarshal([]byte(body), &parsed)
		mu.Lock()
		defer mu.Unlock()
		sizes = append(sizes, len(parsed.Records))
		out := make([]map[string]interface{}, len(parsed.Records))
		for i, rec := range parsed.Records {
			if rec["Ext__c"] == lockedKey && seenLocked < times {
				seenLocked++
				out[i] = map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"statusCode": "UNABLE_TO_LOCK_ROW", "message": "unable to obtain exclusive access to this record"}}}
				continue
			}
			out[i] = map[string]interface{}{"id": fmt.Sprintf("003%03d", i), "success": true, "errors": []any{}}
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
		return true
	}, &sizes
}

func TestLockedRecordsAreRetriedAlone(t *testing.T) {
	noLockBackoff(t)
	handler, sizes := lockedThenOK(t, "A-2", 2)
	var cap capture
	d, _ := newDest(t, &cap, handler)

	records := stringBatch(t, map[string][]string{
		"ext":       {"A-1", "A-2", "A-3"},
		"FirstName": {"x", "y", "z"},
	}, []string{"ext", "FirstName"})
	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})); err != nil {
		t.Fatalf("a transient lock must not fail the run: %v", err)
	}
	// First request carries all three; the two retries carry only the locked one.
	if got := *sizes; len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("request sizes = %v, want [3 1 1]", got)
	}
}

func TestLockedRecordIsRejectedAfterRetriesRunOut(t *testing.T) {
	noLockBackoff(t)
	handler, sizes := lockedThenOK(t, "A-2", 100)
	var cap capture
	d, _ := newDest(t, &cap, handler)

	records := stringBatch(t, map[string][]string{
		"ext":       {"A-1", "A-2"},
		"FirstName": {"x", "y"},
	}, []string{"ext", "FirstName"})
	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"}))
	if err == nil || !strings.Contains(err.Error(), "UNABLE_TO_LOCK_ROW") || !strings.Contains(err.Error(), "Ext__c=A-2") {
		t.Fatalf("error = %v, want the still-locked record reported", err)
	}
	if got := len(*sizes); got != lockRetries+1 {
		t.Fatalf("sent %d request(s), want %d (1 + %d retries)", got, lockRetries+1, lockRetries)
	}
}

// fakeBulk is a Bulk API 2.0 job server; reject names the error for a row, or
// "" to accept it.
type fakeBulk struct {
	mu      sync.Mutex
	jobs    []map[string]string
	uploads []string
	state   string
	errMsg  string
	reject  func(row map[string]string) string
}

func (f *fakeBulk) handler(t *testing.T) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, body string) bool {
		path := r.URL.Path
		i := strings.Index(path, "/jobs/ingest")
		if i < 0 {
			return false
		}
		rest := strings.Trim(path[i+len("/jobs/ingest"):], "/")
		parts := strings.Split(rest, "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && rest == "":
			var job map[string]string
			if err := json.Unmarshal([]byte(body), &job); err != nil {
				t.Fatalf("bad job body %q: %v", body, err)
			}
			f.jobs = append(f.jobs, job)
			f.uploads = append(f.uploads, "")
			_, _ = fmt.Fprintf(w, `{"id":"750J%d","state":"Open"}`, len(f.jobs)-1)
		case r.Method == http.MethodPut && len(parts) == 2 && parts[1] == "batches":
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
				t.Errorf("upload Content-Type = %q, want text/csv", ct)
			}
			f.uploads[f.jobIndex(parts[0])] = body
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && len(parts) == 1:
			ok, failed := f.results(parts[0])
			nOK, nFailed := len(ok)-1, len(failed)-1
			state := f.state
			if state == "" {
				state = "JobComplete"
			}
			_, _ = fmt.Fprintf(w, `{"id":%q,"state":%q,"errorMessage":%q,"numberRecordsProcessed":%d,"numberRecordsFailed":%d}`,
				parts[0], state, f.errMsg, nOK+nFailed, nFailed)
		case r.Method == http.MethodGet && len(parts) == 2:
			ok, failed := f.results(parts[0])
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Sforce-Locator", "null")
			if parts[1] == "successfulResults" {
				_, _ = w.Write([]byte(strings.Join(ok, "")))
			} else {
				_, _ = w.Write([]byte(strings.Join(failed, "")))
			}
		default:
			t.Errorf("unexpected bulk request %s %s", r.Method, path)
		}
		return true
	}
}

func (f *fakeBulk) jobIndex(id string) int {
	var n int
	_, _ = fmt.Sscanf(id, "750J%d", &n)
	return n
}

// results renders the result CSVs as lines, each starting with its header.
func (f *fakeBulk) results(id string) (ok, failed []string) {
	rows := parseCSV(f.uploads[f.jobIndex(id)])
	ok = []string{"\"sf__Id\",\"sf__Created\",rest\n"}
	failed = []string{"\"sf__Id\",\"sf__Error\",Ext__c,Id\n"}
	for i, row := range rows {
		if f.reject != nil {
			if msg := f.reject(row); msg != "" {
				failed = append(failed, fmt.Sprintf("\"\",%q,%q,%q\n", msg, row["Ext__c"], row["Id"]))
				continue
			}
		}
		ok = append(ok, fmt.Sprintf("\"003B%03d\",\"true\",x\n", i))
	}
	return ok, failed
}

func parseCSV(data string) []map[string]string {
	var rows []map[string]string
	_ = readCSVRows([]byte(data), func(row map[string]string) { rows = append(rows, row) })
	return rows
}

func newBulkDest(t *testing.T, cap *capture, fb *fakeBulk, extra handlerFunc) *SalesforceDestination {
	t.Helper()
	bulk := fb.handler(t)
	d, _ := newDest(t, cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if extra != nil && extra(w, r, body) {
			return true
		}
		if strings.Contains(r.URL.Path, "/composite/") {
			t.Errorf("load_method=bulk must not call REST collections: %s %s", r.Method, r.URL.Path)
		}
		return bulk(w, r, body)
	})
	d.loadMethod = loadMethodBulk
	return d
}

func TestParseURILoadMethod(t *testing.T) {
	base := "salesforce://?access_token=tok&domain=example.my.salesforce.com"
	for uri, want := range map[string]string{base: loadMethodBulk, base + "&load_method=rest": loadMethodREST, base + "&load_method=bulk": loadMethodBulk} {
		_, got, err := parseURI(uri)
		if err != nil || got != want {
			t.Fatalf("parseURI(%q) = %q, %v; want %q", uri, got, err, want)
		}
	}
	if _, _, err := parseURI(base + "&load_method=bulk2"); err == nil || !strings.Contains(err.Error(), "load_method") {
		t.Fatalf("error = %v, want an invalid load_method error", err)
	}
}

func TestBulkMergeUploadsCSVToUpsertJob(t *testing.T) {
	var cap capture
	fb := &fakeBulk{}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{
		"ext":            {"A-1", "A-2"},
		"FirstName":      {"Ada", ""},
		"Account.Ext__c": {"ACC-1", "ACC-2"},
		"Title":          {"multi\nline, \"quoted\"", "CTO"},
	}, []string{"ext", "FirstName", "Account.Ext__c", "Title"})
	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})
	opts.WriteNulls = true
	if err := d.WriteParallel(context.Background(), records, opts); err != nil {
		t.Fatalf("WriteParallel returned error: %v", err)
	}

	if len(fb.jobs) != 1 {
		t.Fatalf("created %d job(s), want 1", len(fb.jobs))
	}
	job := fb.jobs[0]
	if job["object"] != "Contact" || job["operation"] != "upsert" || job["externalIdFieldName"] != "Ext__c" || job["contentType"] != "CSV" {
		t.Fatalf("job = %v", job)
	}
	want := "Account.Ext__c,Ext__c,FirstName,Title\n" +
		"ACC-1,A-1,Ada,\"multi\nline, \"\"quoted\"\"\"\n" +
		"ACC-2,A-2,#N/A,CTO\n"
	if fb.uploads[0] != want {
		t.Fatalf("uploaded CSV:\n%s\nwant:\n%s", fb.uploads[0], want)
	}
}

func TestBulkOmitsNullsWithoutWriteNulls(t *testing.T) {
	var cap capture
	fb := &fakeBulk{}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{
		"Id":        {"003A", "003B"},
		"FirstName": {"Ada", ""},
	}, []string{"Id", "FirstName"})
	if err := d.Write(context.Background(), records, writeOpts("Contact", "update", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if fb.jobs[0]["operation"] != "update" {
		t.Fatalf("job = %v, want an update job", fb.jobs[0])
	}
	// An empty cell leaves the field untouched.
	if want := "FirstName,Id\nAda,003A\n,003B\n"; fb.uploads[0] != want {
		t.Fatalf("uploaded CSV = %q, want %q", fb.uploads[0], want)
	}
}

func TestBulkFailedRowsBecomeRejections(t *testing.T) {
	var cap capture
	fb := &fakeBulk{reject: func(row map[string]string) string {
		if row["Ext__c"] == "A-2" {
			return "INVALID_FIELD:Foreign key external ID: acc-9 not found for field Ext__c in entity Account:Account --"
		}
		return ""
	}}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{
		"ext":       {"A-1", "A-2", "A-3"},
		"FirstName": {"x", "y", "z"},
	}, []string{"ext", "FirstName"})
	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"}))
	if err == nil {
		t.Fatal("want the rejected row reported")
	}
	for _, s := range []string{"rejected 1 Contact", "(INVALID_FIELD) [Ext__c=A-2] Foreign key external ID: acc-9 not found", "(fields: Account)"} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error = %v, want it to contain %q", err, s)
		}
	}
}

func TestBulkJobFailureAbortsRun(t *testing.T) {
	var cap capture
	fb := &fakeBulk{state: "Failed", errMsg: "InvalidBatch : Field name not found : Nope__c"}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{"LastName": {"x"}}, []string{"LastName"})
	opts := writeOpts("Contact", "append", nil)
	opts.RejectMode = "skip"
	err := d.Write(context.Background(), records, opts)
	if err == nil || !strings.Contains(err.Error(), "Field name not found : Nope__c") || !strings.Contains(err.Error(), "insert Contact job 750J0 failed") {
		t.Fatalf("error = %v, want the job failure even under skip", err)
	}
}

func TestBulkRejectsFailFast(t *testing.T) {
	var cap capture
	d := newBulkDest(t, &cap, &fakeBulk{}, nil)
	connectRequests := len(cap.all())
	records := stringBatch(t, map[string][]string{"LastName": {"x"}}, []string{"LastName"})
	opts := writeOpts("Contact", "append", nil)
	opts.RejectMode = "fail_fast"
	err := d.Write(context.Background(), records, opts)
	if err == nil || !strings.Contains(err.Error(), "fail_fast needs load_method=rest") {
		t.Fatalf("error = %v, want fail_fast rejected under load_method=bulk", err)
	}
	if n := len(cap.all()) - connectRequests; n != 0 {
		t.Fatalf("sent %d request(s), want none", n)
	}
}

func TestBulkReplaceMirrorDeletesStaleRecordsInADeleteJob(t *testing.T) {
	var cap capture
	fb := &fakeBulk{}
	d := newBulkDest(t, &cap, fb, func(w http.ResponseWriter, r *http.Request, _ string) bool {
		if !strings.HasSuffix(r.URL.Path, "/query") {
			return false
		}
		// 003B000 is the id the upsert job returned; its stored value differs in case.
		_, _ = w.Write([]byte(`{"done":true,"records":[
			{"Id":"003B000","Ext__c":"a-1"},
			{"Id":"003OLD","Ext__c":"GONE"}
		]}`))
		return true
	})

	records := stringBatch(t, map[string][]string{
		"ext":       {"A-1"},
		"FirstName": {"Ada"},
	}, []string{"ext", "FirstName"})
	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Ext__c", "replace", []string{"ext"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if len(fb.jobs) != 2 || fb.jobs[1]["operation"] != "delete" {
		t.Fatalf("jobs = %v, want an upsert then a delete", fb.jobs)
	}
	if want := "Id\n003OLD\n"; fb.uploads[1] != want {
		t.Fatalf("delete CSV = %q, want %q", fb.uploads[1], want)
	}
}

func TestParseBulkError(t *testing.T) {
	got := parseBulkError("INVALID_FIELD:Failed to deserialize field at col 3. Due to, 'x' is not a valid value for the type xsd:date:Birthdate --")
	if got.code != "INVALID_FIELD" || got.message != "Failed to deserialize field at col 3. Due to, 'x' is not a valid value for the type xsd:date" || len(got.fields) != 1 || got.fields[0] != "Birthdate" {
		t.Fatalf("parseBulkError = %+v", got)
	}
	got = parseBulkError("REQUIRED_FIELD_MISSING:Required fields are missing: [LastName]:LastName --")
	if got.code != "REQUIRED_FIELD_MISSING" || got.message != "Required fields are missing: [LastName]" || got.fields[0] != "LastName" {
		t.Fatalf("parseBulkError = %+v", got)
	}
}

func TestConnectRejectsUnusableSessionOrAPIVersion(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusUnauthorized: "authentication failed: status 401: INVALID_SESSION_ID",
		http.StatusNotFound:     "API version 59.0 is not available",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`[{"errorCode":"INVALID_SESSION_ID","message":"Session expired or invalid"}]`))
		}))
		d := NewSalesforceDestination()
		err := d.Connect(context.Background(), "salesforce://?access_token=tok&domain="+server.URL)
		server.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("status %d: Connect error = %v, want %q", status, err, want)
		}
	}
}

func TestDuplicateKeyHintMatchesTheCause(t *testing.T) {
	upsert := &shaper{sobject: "Contact", idField: "Ext__c"}
	var dup rejectionLog
	dup.add([]rejection{{code: "DUPLICATE_VALUE", message: "Duplicate external id specified: a-1", identifier: "Ext__c=A-1"}})
	err := reportRejections(upsert, &dup)
	if err == nil || !strings.Contains(err.Error(), "same Ext__c value on more than one row") || strings.Contains(err.Error(), "incremental-strategy merge") {
		t.Fatalf("error = %v, want the de-duplicate hint", err)
	}

	create := &shaper{sobject: "Contact", createOnly: true}
	var dupRule rejectionLog
	dupRule.add([]rejection{{code: "DUPLICATES_DETECTED", message: "Use one of these records?"}})
	if err := reportRejections(create, &dupRule); err == nil || !strings.Contains(err.Error(), "incremental-strategy merge with external_id") {
		t.Fatalf("error = %v, want the upsert hint on create", err)
	}
}

func TestStorageLimitAbortsRegardlessOfRejectMode(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method != http.MethodPatch {
			return false
		}
		n := countRecords(t, body)
		out := make([]map[string]interface{}, n)
		for i := range out {
			out[i] = map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"statusCode": "STORAGE_LIMIT_EXCEEDED", "message": "storage limit exceeded"}}}
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
		return true
	})

	ext := make([]string, 450)
	for i := range ext {
		ext[i] = fmt.Sprintf("A-%d", i)
	}
	records := stringBatch(t, map[string][]string{"ext": ext}, []string{"ext"})
	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})
	opts.RejectMode = "skip"
	err := d.Write(context.Background(), records, opts)
	if err == nil || !strings.Contains(err.Error(), "out of data storage") {
		t.Fatalf("error = %v, want the storage limit to abort even under skip", err)
	}
	if n := len(cap.byPath("/composite/sobjects/Contact/Ext__c")); n != 1 {
		t.Fatalf("sent %d upsert request(s), want 1 (abort after the first)", n)
	}
}

// describeHandler answers the sObject describe with the given fields.
func describeHandler(w http.ResponseWriter, r *http.Request, fields string) bool {
	if !strings.HasSuffix(r.URL.Path, "/describe") {
		return false
	}
	_, _ = w.Write([]byte(`{"fields":` + fields + `}`))
	return true
}

func TestResolveReadsQueryResultsUnderTheCanonicalFieldName(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if describeHandler(w, r, `[{"name":"Email","type":"email"}]`) {
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/query") {
			_, _ = w.Write([]byte(`{"done":true,"records":[{"Id":"003000000000001","Email":"a@x.com"}]}`))
			return true
		}
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})
	records := stringBatch(t, map[string][]string{"email": {"a@x.com"}, "Title": {"CTO"}}, []string{"email", "Title"})
	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=email", "update", []string{"email"})); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n := len(cap.byPath("/composite/sobjects")); n != 1 {
		t.Fatalf("got %d update request(s), want 1", n)
	}
}

func TestResolveByNumberFieldUsesUnquotedCanonicalValues(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if describeHandler(w, r, `[{"name":"Num__c","type":"double"}]`) {
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/query") {
			_, _ = w.Write([]byte(`{"done":true,"records":[{"Id":"003000000000001","Num__c":12345678.0}]}`))
			return true
		}
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})
	records := stringBatch(t, map[string][]string{"num": {"12345678.00", "abc'"}, "Title": {"CTO", "CEO"}}, []string{"num", "Title"})
	opts := writeOpts("Contact?external_id=Num__c", "update", []string{"num"})
	opts.RejectMode = "skip"
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	queries := cap.byPath("/query")
	if len(queries) != 1 || !strings.Contains(queries[0].query, "%2812345678%29") {
		t.Fatalf("queries = %v, want one unquoted IN (12345678)", queries)
	}
	if n := len(cap.byPath("/composite/sobjects")); n != 1 {
		t.Fatalf("got %d update request(s), want 1", n)
	}
}

func TestCanonicalNumber(t *testing.T) {
	for in, want := range map[string]string{"42": "42", "42.00": "42", "12345678.0": "12345678", "1.2345678E7": "12345678", "-0.50": "-0.5"} {
		if got, ok := canonicalNumber(in); !ok || got != want {
			t.Fatalf("canonicalNumber(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := canonicalNumber("1) OR Name != ("); ok {
		t.Fatal("canonicalNumber accepted a non-number")
	}
}

func TestRecordID18(t *testing.T) {
	for in, want := range map[string]string{"001D000000IqhSL": "001D000000IqhSLIAZ", "001D000000IqhSLIAZ": "001D000000IqhSLIAZ", "not-an-id-15chr": "not-an-id-15chr"} {
		if got := recordID18(in); got != want {
			t.Fatalf("recordID18(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBulkPollWaitStaysPositive(t *testing.T) {
	for attempt := range 100 {
		if w := bulkPollWait(attempt); w <= 0 || w > 10*time.Second {
			t.Fatalf("bulkPollWait(%d) = %v", attempt, w)
		}
	}
}

func TestNonFiniteFloatIsNull(t *testing.T) {
	b := array.NewFloat64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.AppendValues([]float64{math.NaN(), math.Inf(1)}, nil)
	arr := b.NewArray()
	defer arr.Release()
	for i := 0; i < arr.Len(); i++ {
		if _, ok := fieldValue(arr, i); ok {
			t.Fatalf("row %d: non-finite float returned ok=true", i)
		}
	}
}

func TestWriteReturnsWithoutDrainingOnError(t *testing.T) {
	var cap capture
	d := newBulkDest(t, &cap, &fakeBulk{}, nil)
	records := make(chan source.RecordBatchResult)
	opts := writeOpts("Contact", "append", nil)
	opts.RejectMode = "fail_fast"
	done := make(chan error, 1)
	go func() { done <- d.Write(context.Background(), records, opts) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Write returned nil, want the fail_fast error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked draining an open source channel")
	}
}

func TestSOQLChunksBudgetForURLEncoding(t *testing.T) {
	values := make([]string, 150)
	for i := range values {
		values[i] = strings.Repeat("日", 12)
	}
	for _, chunk := range soqlChunks(values, 50) {
		quoted := make([]string, len(chunk))
		for i, v := range chunk {
			quoted[i] = "'" + v + "'"
		}
		if n := len(url.QueryEscape(strings.Join(quoted, ","))); n > soqlMaxQueryLen {
			t.Fatalf("encoded chunk is %d bytes, want <= %d", n, soqlMaxQueryLen)
		}
	}
}

func TestTimeColumnsUseSalesforceTimeFormat(t *testing.T) {
	b := array.NewTime64Builder(memory.DefaultAllocator, &arrow.Time64Type{Unit: arrow.Microsecond})
	defer b.Release()
	b.Append(arrow.Time64((13*3600 + 45*60 + 7) * 1_000_000))
	arr := b.NewArray()
	defer arr.Release()
	if got, _ := fieldValue(arr, 0); got != "13:45:07.000Z" {
		t.Fatalf("time = %#v, want 13:45:07.000Z", got)
	}
}

func TestRecordValueFallsBackToCaseInsensitiveField(t *testing.T) {
	rec := map[string]interface{}{"Ext_Id__c": "A-1"}
	if got := recordValue(rec, "ext_id__c"); got != "A-1" {
		t.Fatalf("recordValue = %v, want A-1", got)
	}
}

func TestUpdateDefaultMatchColumnIsCaseInsensitiveAndNotWritten(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if strings.HasSuffix(r.URL.Path, "/query") {
			_, _ = w.Write([]byte(`{"done":true,"records":[{"Id":"003000000000001","Email":"a@x.com"}]}`))
			return true
		}
		if r.Method == http.MethodPatch {
			successResults(w, countRecords(t, body), "003")
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"email":    {"a@x.com"},
		"Industry": {"Tech"},
	}, []string{"email", "Industry"})
	if err := d.Write(context.Background(), records, writeOpts("Contact?external_id=Email", "update", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	patches := cap.byPath("/composite/sobjects")
	if len(patches) != 1 {
		t.Fatalf("got %d patch request(s), want 1", len(patches))
	}
	if strings.Contains(strings.ToLower(patches[0].body), `"email"`) {
		t.Fatalf("body = %s, want the match column left out of the update body", patches[0].body)
	}
}

func TestUpdateByLookupFieldSkipsMalformedIDs(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/describe"):
			_, _ = w.Write([]byte(`{"fields":[{"name":"Id","type":"id"},{"name":"AccountId","type":"reference","relationshipName":"Account"},{"name":"Title","type":"string","createable":true,"updateable":true}]}`))
		case strings.HasSuffix(r.URL.Path, "/query"):
			_, _ = w.Write([]byte(`{"done":true,"records":[{"Id":"003000000000001AAA","AccountId":"001D000000IqhSLIAZ"}]}`))
		case r.Method == http.MethodPatch:
			successResults(w, countRecords(t, body), "003")
		default:
			return false
		}
		return true
	})

	records := stringBatch(t, map[string][]string{
		"AccountId": {"001D000000IqhSL", "not-an-id"},
		"Title":     {"x", "y"},
	}, []string{"AccountId", "Title"})
	err := d.Write(context.Background(), records, writeOpts("Contact?external_id=AccountId", "update", []string{"AccountId"}))
	if err == nil || !strings.Contains(err.Error(), `AccountId=not-an-id`) || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("error = %v, want only the malformed id rejected as not found", err)
	}
	queries := cap.byPath("/query")
	if len(queries) != 1 || strings.Contains(queries[0].query, "not-an-id") || !strings.Contains(queries[0].query, "001D000000IqhSLIAZ") {
		t.Fatalf("queries = %+v, want one query with the 18-char id and without the malformed value", queries)
	}
	if n := len(cap.byPath("/composite/sobjects")); n != 1 {
		t.Fatalf("sent %d update request(s), want 1 for the matched row", n)
	}
}

func TestNonFiniteFloatFollowsWriteNulls(t *testing.T) {
	for _, writeNulls := range []bool{true, false} {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if r.Method == http.MethodPatch {
				successResults(w, countRecords(t, body), "003")
				return true
			}
			return false
		})
		sch := arrow.NewSchema([]arrow.Field{{Name: "ext", Type: arrow.BinaryTypes.String}, {Name: "Amount", Type: arrow.PrimitiveTypes.Float64}}, nil)
		b := array.NewRecordBuilder(memory.DefaultAllocator, sch)
		b.Field(0).(*array.StringBuilder).Append("A-1")
		b.Field(1).(*array.Float64Builder).Append(math.NaN())
		ch := make(chan source.RecordBatchResult, 1)
		ch <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
		close(ch)
		b.Release()

		opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})
		opts.WriteNulls = writeNulls
		if err := d.Write(context.Background(), ch, opts); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		reqs := cap.byPath("/composite/sobjects/Contact/Ext__c")
		if len(reqs) != 1 {
			t.Fatalf("got %d upsert request(s), want 1", len(reqs))
		}
		var parsed struct {
			Records []map[string]interface{} `json:"records"`
		}
		_ = json.Unmarshal([]byte(reqs[0].body), &parsed)
		v, present := parsed.Records[0]["Amount"]
		if writeNulls && (!present || v != nil) {
			t.Fatalf("write-nulls on: Amount = %v (present %v), want an explicit null", v, present)
		}
		if !writeNulls && present {
			t.Fatalf("write-nulls off: Amount = %v, want it omitted", v)
		}
	}
}

func TestAppendRefusesExternalID(t *testing.T) {
	_, err := parseShaper("Contact?external_id=Ext__c", "append", []string{"Ext__c"}, "", false)
	if err == nil || !strings.Contains(err.Error(), "never matches") || !strings.Contains(err.Error(), "incremental-strategy merge") {
		t.Fatalf("error = %v, want append+external_id refused", err)
	}
}

func TestAppendNamesRejectsByPrimaryKey(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`[
				{"success":false,"errors":[{"statusCode":"DUPLICATE_VALUE","message":"duplicate value found: Ext__c duplicates value on record with id: 003A"}]},
				{"id":"003000000000002","success":true,"errors":[]}
			]`))
			return true
		}
		return false
	})

	records := stringBatch(t, map[string][]string{
		"Ext__c":   {"C-1", "C-2"},
		"LastName": {"Dup", "Fresh"},
	}, []string{"Ext__c", "LastName"})
	err := d.Write(context.Background(), records, writeOpts("Contact", "append", []string{"Ext__c"}))
	if err == nil || !strings.Contains(err.Error(), "(DUPLICATE_VALUE) [Ext__c=C-1]") {
		t.Fatalf("error = %v, want the reject named by the primary key", err)
	}
	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || !strings.Contains(reqs[0].body, `"Ext__c":"C-1"`) {
		t.Fatalf("got %+v, want the primary key column written as a field", reqs)
	}
}

func TestBulkAppendNamesRejectsByPrimaryKey(t *testing.T) {
	var cap capture
	fb := &fakeBulk{reject: func(row map[string]string) string {
		if row["Ext__c"] == "C-1" {
			return "DUPLICATE_VALUE:duplicate value found:Ext__c --"
		}
		return ""
	}}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{
		"Ext__c":   {"C-1", "C-2"},
		"LastName": {"Dup", "Fresh"},
	}, []string{"Ext__c", "LastName"})
	err := d.Write(context.Background(), records, writeOpts("Contact", "append", []string{"Ext__c"}))
	if err == nil || !strings.Contains(err.Error(), "[Ext__c=C-1]") {
		t.Fatalf("error = %v, want the bulk reject named by the primary key", err)
	}
}

func TestBulkRejectsLiteralNullMarker(t *testing.T) {
	var cap capture
	fb := &fakeBulk{}
	d := newBulkDest(t, &cap, fb, nil)

	records := stringBatch(t, map[string][]string{
		"ext":   {"A-1", "A-2"},
		"Title": {"#N/A", "CTO"},
	}, []string{"ext", "Title"})
	opts := writeOpts("Contact?external_id=Ext__c", "merge", []string{"ext"})
	err := d.Write(context.Background(), records, opts)
	if err == nil || !strings.Contains(err.Error(), bulkNullLiteralCode) || !strings.Contains(err.Error(), "Ext__c=A-1") {
		t.Fatalf("error = %v, want the #N/A row rejected", err)
	}
	if len(fb.uploads) != 1 || strings.Contains(fb.uploads[0], "A-1") || !strings.Contains(fb.uploads[0], "A-2") {
		t.Fatalf("uploads = %q, want only the A-2 row sent", fb.uploads)
	}
}

func TestWriteNullsIsFlagOnly(t *testing.T) {
	_, err := parseShaper("Contact?external_id=Ext__c&write_nulls=true", "merge", []string{"Ext__c"}, "", false)
	if err == nil || !strings.Contains(err.Error(), "unknown table parameter(s): write_nulls") {
		t.Fatalf("error = %v, want write_nulls refused as a table parameter", err)
	}
}

func TestRelationshipColumn(t *testing.T) {
	for name, want := range map[string][4]string{
		"Account.Ext_Id__c":        {"", "Account", "Ext_Id__c", "true"},
		"Who.Contact.Ext_Id__c":    {"Contact", "Who", "Ext_Id__c", "true"},
		"Owner.User.Email":         {"User", "Owner", "Email", "true"},
		"Parent__r.Invoice__c.Ext": {"Invoice__c", "Parent__r", "Ext", "true"},
		"Account__r.Ext_Id__c":     {"", "Account__r", "Ext_Id__c", "true"},
		"FirstName":                {"", "", "", "false"},
		"Who..Ext_Id__c":           {"", "", "", "false"},
		"Who.Contact.":             {"Contact", "Who", "", "false"},
		"A.B.C.D":                  {"", "", "", "false"},
	} {
		typ, rel, field, ok := relationshipColumn(name)
		if got := [4]string{typ, rel, field, fmt.Sprint(ok)}; got != want {
			t.Errorf("relationshipColumn(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTypedLookupSendsParentType(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPost {
			successResults(w, countRecords(t, body), "00T")
			return true
		}
		return false
	})
	records := stringBatch(t, map[string][]string{
		"Subject":               {"Call"},
		"Who.Contact.Ext_Id__c": {"C-1"},
	}, []string{"Subject", "Who.Contact.Ext_Id__c"})
	if err := d.Write(context.Background(), records, writeOpts("Task", "append", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	body := cap.byPath("/composite/sobjects")[0].body
	if !strings.Contains(body, `"Who":{"Ext_Id__c":"C-1","attributes":{"type":"Contact"}}`) {
		t.Fatalf("body = %s, want the nested Who with its parent type", body)
	}
}

func TestBulkTypedLookupHeader(t *testing.T) {
	var cap capture
	fb := &fakeBulk{}
	d := newBulkDest(t, &cap, fb, nil)
	records := stringBatch(t, map[string][]string{
		"Subject":               {"Call"},
		"Who.Contact.Ext_Id__c": {"C-1"},
	}, []string{"Subject", "Who.Contact.Ext_Id__c"})
	if err := d.Write(context.Background(), records, writeOpts("Task", "append", nil)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if len(fb.uploads) != 1 || !strings.Contains(fb.uploads[0], "Contact:Who.Ext_Id__c") || strings.Contains(fb.uploads[0], "attributes") {
		t.Fatalf("uploads = %q, want a Contact:Who.Ext_Id__c header", fb.uploads)
	}
}

func TestPolymorphicConflictRejectsRow(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		if r.Method == http.MethodPost {
			successResults(w, countRecords(t, body), "00T")
			return true
		}
		return false
	})
	records := stringBatch(t, map[string][]string{
		"Subject":               {"both", "contact only"},
		"Who.Contact.Ext_Id__c": {"C-1", "C-2"},
		"Who.Lead.Ext_Id__c":    {"L-1", ""},
	}, []string{"Subject", "Who.Contact.Ext_Id__c", "Who.Lead.Ext_Id__c"})
	err := d.Write(context.Background(), records, writeOpts("Task", "append", []string{"Subject"}))
	if err == nil || !strings.Contains(err.Error(), "(POLYMORPHIC_CONFLICT) [Subject=both]") {
		t.Fatalf("error = %v, want the row setting both Who columns rejected", err)
	}
	reqs := cap.byPath("/composite/sobjects")
	if len(reqs) != 1 || countRecords(t, reqs[0].body) != 1 || !strings.Contains(reqs[0].body, "C-2") {
		t.Fatalf("got %+v, want only the single-Who row sent", reqs)
	}
}

func TestPrepareTableChecksPolymorphicLookups(t *testing.T) {
	describe := `{"fields":[
		{"name":"Id","createable":false,"updateable":false},
		{"name":"Subject","createable":true,"updateable":true},
		{"name":"WhoId","createable":true,"updateable":true,"relationshipName":"Who","referenceTo":["Contact","Lead"]},
		{"name":"AccountId","createable":true,"updateable":true,"relationshipName":"Account","referenceTo":["Account"]},
		{"name":"OwnerId","createable":true,"updateable":true,"relationshipName":"Owner","referenceTo":["Group","User"]}
	]}`
	prepare := func(column string) error {
		var cap capture
		d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
			if strings.HasSuffix(r.URL.Path, "/describe") {
				_, _ = w.Write([]byte(describe))
				return true
			}
			return false
		})
		return d.PrepareTable(context.Background(), destination.PrepareOptions{
			Table: "Task", Strategy: "append",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "Subject"}, {Name: column}}},
		})
	}
	for column, want := range map[string]string{
		"Who.Ext_Id__c":          "need the parent object in the column name",
		"Who.Account.Ext_Id__c":  "can point to Contact, Lead",
		"Who.Contact.Ext_Id__c":  "",
		"Who.lead.Ext_Id__c":     "",
		"Account.Ext_Id__c":      "",
		"Account.Account.Ext__c": "",
		"Owner.Email":            "",
		"Owner.Group.Name":       "",
	} {
		err := prepare(column)
		if want == "" && err != nil {
			t.Errorf("%s: unexpected error %v", column, err)
		}
		if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: error = %v, want %q", column, err, want)
		}
	}
}

func TestPolymorphicOwnerDefaultsToUser(t *testing.T) {
	var cap capture
	d, _ := newDest(t, &cap, func(w http.ResponseWriter, r *http.Request, body string) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/describe"):
			_, _ = w.Write([]byte(`{"fields":[{"name":"OwnerId","createable":true,"updateable":true,"relationshipName":"Owner","referenceTo":["Group","User"]}]}`))
			return true
		case r.Method == http.MethodPost:
			successResults(w, countRecords(t, body), "00Q")
			return true
		}
		return false
	})
	records := stringBatch(t, map[string][]string{
		"LastName":         {"Owner Test"},
		"Owner.Email":      {"owner@example.com"},
		"Parent.Group.Key": {"Q-1"},
	}, []string{"LastName", "Owner.Email", "Parent.Group.Key"})
	opts := writeOpts("Lead", "append", nil)
	opts.WriteNulls = false
	if err := d.Write(context.Background(), records, opts); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	body := cap.byPath("/composite/sobjects")[0].body
	if !strings.Contains(body, `"Owner":{"Email":"owner@example.com","attributes":{"type":"User"}}`) {
		t.Fatalf("body = %s, want the untyped Owner sent as a User", body)
	}
	if !strings.Contains(body, `"Parent":{"Key":"Q-1","attributes":{"type":"Group"}}`) {
		t.Fatalf("body = %s, want an explicit object kept as given", body)
	}
}

func TestBulkIsTheDefaultLoadMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	d := NewSalesforceDestination()
	if err := d.Connect(context.Background(), "salesforce://?access_token=tok&domain="+server.URL); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if d.loadMethod != loadMethodBulk {
		t.Fatalf("loadMethod = %q, want bulk when the URI sets none", d.loadMethod)
	}
}
