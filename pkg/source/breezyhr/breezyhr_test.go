package breezyhr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantKey     string
		wantCompany string
		wantErr     bool
	}{
		{name: "valid", uri: "breezyhr://?api_key=breezy_pat_abc.def&company_id=c1", wantKey: "breezy_pat_abc.def", wantCompany: "c1"},
		{name: "without question mark", uri: "breezyhr://api_key=k1&company_id=c1", wantKey: "k1", wantCompany: "c1"},
		{name: "missing key", uri: "breezyhr://?company_id=c1", wantErr: true},
		{name: "missing company", uri: "breezyhr://?api_key=k1", wantErr: true},
		{name: "empty", uri: "breezyhr://", wantErr: true},
		{name: "wrong scheme", uri: "trello://?api_key=k1&company_id=c1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, company, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey || company != tt.wantCompany {
				t.Errorf("got (%q, %q), want (%q, %q)", key, company, tt.wantKey, tt.wantCompany)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false", table)
		}
	}
	for _, table := range []string{"", "Candidates", "users", "candidate"} {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true", table)
		}
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	items := []map[string]interface{}{
		{"_id": "before", "updated_date": "2026-01-01T23:59:59.999Z"},
		{"_id": "at-start", "updated_date": "2026-01-02T00:00:00.000Z"},
		{"_id": "inside", "updated_date": "2026-01-02T12:00:00.500Z"},
		{"_id": "at-end", "updated_date": "2026-01-03T00:00:00.000Z"},
		{"_id": "after-end", "updated_date": "2026-01-03T00:00:00.001Z"},
		{"_id": "no-date"},
	}
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	got := filterItemsByInterval(items, "updated_date", &start, &end)
	var ids []string
	for _, item := range got {
		ids = append(ids, item["_id"].(string))
	}
	want := []string{"at-start", "inside", "at-end", "no-date"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v, want %v", ids, want)
		}
	}

	if all := filterItemsByInterval(items, "updated_date", nil, nil); len(all) != len(items) {
		t.Errorf("no interval returned %d items, want %d", len(all), len(items))
	}
}

// fakeBreezy serves the positions endpoint plus the handlers in routes, matched by
// path suffix, and records every request path with its query.
func fakeBreezy(t *testing.T, positions string, routes map[string]http.HandlerFunc) (*BreezyHRSource, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/positions") {
			if r.URL.Query().Get("state") == "published" || r.URL.Query().Get("state") == "closed" {
				_, _ = w.Write([]byte(positions))
				return
			}
			_, _ = w.Write([]byte("[]"))
			return
		}
		for suffix, h := range routes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				h(w, r)
				return
			}
		}
		t.Errorf("unexpected request %s", r.URL)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	s := &BreezyHRSource{companyID: "c", client: httpclient.New(httpclient.WithBaseURL(srv.URL), httpclient.WithDisableRetry())}
	return s, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), requests...)
	}
}

func readColumn(t *testing.T, s *BreezyHRSource, table, column string) []string {
	t.Helper()
	results, err := s.read(context.Background(), table, source.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for r := range results {
		if r.Err != nil {
			t.Fatalf("read %s: %v", table, r.Err)
		}
		idx := r.Batch.Schema().FieldIndices(column)
		if len(idx) == 0 {
			t.Fatalf("%s has no %s column", table, column)
		}
		col := r.Batch.Column(idx[0])
		if ext, ok := col.(array.ExtensionArray); ok {
			col = ext.Storage()
		}
		for i := 0; i < col.Len(); i++ {
			values = append(values, strings.Trim(col.ValueStr(i), `"`))
		}
	}
	return values
}

func ids(prefix string, from, to int) []map[string]any {
	var out []map[string]any
	for i := from; i < to; i++ {
		out = append(out, map[string]any{"_id": fmt.Sprintf("%s%d", prefix, i)})
	}
	return out
}

func bulkEcho(t *testing.T, extra func(id string) []map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"candidate_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.IDs) == 0 || len(body.IDs) > 50 {
			t.Errorf("bulk got %d ids, want 1-50", len(body.IDs))
		}
		var out []map[string]any
		for _, id := range body.IDs {
			c := map[string]any{"_id": id}
			if extra != nil {
				c["stream"] = extra(id)
			}
			out = append(out, c)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": out})
	}
}

func assertUnique(t *testing.T, got []string, want int) {
	t.Helper()
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] {
			t.Errorf("%s loaded twice", v)
		}
		seen[v] = true
	}
	if len(got) != want {
		t.Errorf("got %d rows, want %d", len(got), want)
	}
}

func TestReadCandidatesLoadsEachCandidateOnce(t *testing.T) {
	// p1 is listed under two states, as when it closes mid-run; p2 has no candidate access.
	s, requests := fakeBreezy(t, `[{"_id":"p1","candidate_access":true},{"_id":"p2","candidate_access":false}]`, map[string]http.HandlerFunc{
		"/p1/candidates/bulk": bulkEcho(t, nil),
		"/p1/candidates": func(w http.ResponseWriter, r *http.Request) {
			all := ids("c", 0, 120)
			if r.URL.Query().Has("page_size") {
				t.Error("candidates were paged; the unpaged list has no pages for updates to shift")
				var page int
				_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
				all = all[min((page-1)*50, 120):min(page*50, 120)]
			}
			_ = json.NewEncoder(w).Encode(all)
		},
	})

	assertUnique(t, readColumn(t, s, "candidates", "_id"), 120)
	for _, r := range requests() {
		if strings.Contains(r, "/p2/") {
			t.Errorf("requested %s on a position without candidate access", r)
		}
	}
}

func TestReadCandidatesPagesByCreationWhenUnpagedListFails(t *testing.T) {
	s, _ := fakeBreezy(t, `[{"_id":"p1","candidate_access":true}]`, map[string]http.HandlerFunc{
		"/p1/candidates/bulk": bulkEcho(t, nil),
		"/p1/candidates": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if !q.Has("page_size") {
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
			if q.Get("sort") != "created" {
				t.Errorf("fallback sorted by %q, want created", q.Get("sort"))
			}
			// A candidate added mid-run pushes c49 onto page 2 as well.
			pages := map[string][]map[string]any{"1": ids("c", 0, 50), "2": ids("c", 49, 99), "3": ids("c", 99, 109)}
			_ = json.NewEncoder(w).Encode(pages[q.Get("page")])
		},
	})

	assertUnique(t, readColumn(t, s, "candidates", "_id"), 109)
}

func TestReadCandidateStreamRereadsTruncatedStreams(t *testing.T) {
	event := func(id string, first bool) map[string]any {
		return map[string]any{"_id": id, "first_activity": first}
	}
	events := func(prefix string, from, to int) []map[string]any {
		var out []map[string]any
		for i := from; i < to; i++ {
			out = append(out, event(fmt.Sprintf("%s%d", prefix, i), false))
		}
		return out
	}
	s, requests := fakeBreezy(t, `[{"_id":"p1","candidate_access":true}]`, map[string]http.HandlerFunc{
		"/p1/candidates/bulk": bulkEcho(t, func(id string) []map[string]any {
			switch id {
			case "full":
				return []map[string]any{event("full-e1", false), event("full-e0", true)}
			default: // 30 events and no first_activity: a filtered, truncated stream
				return events(id+"-e", 0, 30)
			}
		}),
		"/p1/candidates": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"_id":"full"},{"_id":"long"},{"_id":"gone"}]`))
		},
		"/candidate/long/stream": func(w http.ResponseWriter, r *http.Request) {
			// A short first page isn't the end; a new event repeats long-e29 on page 2.
			page := events("long-e", 0, 30)
			if r.URL.Query().Get("skip") == "50" {
				page = append(events("long-e", 29, 48), event("long-e48", true))
			}
			_ = json.NewEncoder(w).Encode(page)
		},
		"/candidate/gone/stream": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPreconditionFailed)
		},
	})

	got := readColumn(t, s, "candidate_stream", "_id")
	assertUnique(t, got, 2+49+30)
	for _, r := range requests() {
		if strings.Contains(r, "/candidate/full/") {
			t.Errorf("re-read a complete stream: %s", r)
		}
	}
}
