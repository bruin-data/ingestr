package okta

import (
	"net/http"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantDomain string
		wantKey    string
		wantErr    bool
	}{
		{"host style", "okta://dev-123.okta.com?api_key=abc", "dev-123.okta.com", "abc", false},
		{"domain param", "okta://?domain=dev-123.okta.com&api_key=abc", "dev-123.okta.com", "abc", false},
		{"strips scheme in host", "okta://https://dev-123.okta.com/?api_key=abc", "dev-123.okta.com", "abc", false},
		{"missing api_key", "okta://dev-123.okta.com", "", "", true},
		{"missing domain", "okta://?api_key=abc", "", "", true},
		{"wrong scheme", "https://dev-123.okta.com?api_key=abc", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, key, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if domain != tt.wantDomain || key != tt.wantKey {
				t.Fatalf("got (%q, %q), want (%q, %q)", domain, key, tt.wantDomain, tt.wantKey)
			}
		})
	}
}

func TestTableRegistryCoversSupportedTables(t *testing.T) {
	for _, tbl := range supportedTables {
		if _, ok := tableRegistry[tbl]; !ok {
			t.Errorf("supported table %q missing from tableRegistry", tbl)
		}
	}
	if len(tableRegistry) != len(supportedTables) {
		t.Errorf("tableRegistry has %d entries, supportedTables has %d", len(tableRegistry), len(supportedTables))
	}
	for _, meta := range tableRegistry {
		if len(meta.primaryKeys) == 0 {
			t.Errorf("table meta %+v has no primary keys", meta)
		}
	}
}

func TestUpdatedExpr(t *testing.T) {
	start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)

	tests := []struct {
		name string
		opts source.ReadOptions
		want string
	}{
		{"no interval", source.ReadOptions{}, ""},
		{"start only", source.ReadOptions{IntervalStart: &start}, `lastUpdated ge "2024-01-02T03:04:05.000Z"`},
		{"start and end", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, `lastUpdated ge "2024-01-02T03:04:05.000Z" and lastUpdated lt "2024-02-03T04:05:06.000Z"`},
		{"end only", source.ReadOptions{IntervalEnd: &end}, `lastUpdated lt "2024-02-03T04:05:06.000Z"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updatedExpr("lastUpdated", tt.opts); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdatedExprOrAll(t *testing.T) {
	start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	// No interval -> catch-all that matches every row.
	if got := updatedExprOrAll("lastUpdated", source.ReadOptions{}); got != `lastUpdated gt "1970-01-01T00:00:00.000Z"` {
		t.Fatalf("full-load: got %q", got)
	}
	// With interval -> same as updatedExpr.
	opts := source.ReadOptions{IntervalStart: &start}
	if got := updatedExprOrAll("lastMembershipUpdated", opts); got != `lastMembershipUpdated ge "2024-01-02T03:04:05.000Z"` {
		t.Fatalf("incremental: got %q", got)
	}
}

func TestNextLink(t *testing.T) {
	h := http.Header{}
	h.Add("Link", `<https://dev-123.okta.com/api/v1/users?limit=200>; rel="self"`)
	h.Add("Link", `<https://dev-123.okta.com/api/v1/users?limit=200&after=00uABC>; rel="next"`)
	if got := nextLink(h); got != "https://dev-123.okta.com/api/v1/users?limit=200&after=00uABC" {
		t.Fatalf("got %q", got)
	}

	// Multiple entries in a single header value.
	single := http.Header{}
	single.Add("Link", `<https://x/self>; rel="self", <https://x/next>; rel="next"`)
	if got := nextLink(single); got != "https://x/next" {
		t.Fatalf("combined header: got %q", got)
	}

	// No next link.
	last := http.Header{}
	last.Add("Link", `<https://x/self>; rel="self"`)
	if got := nextLink(last); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)
	items := []map[string]interface{}{
		{"id": "before", "lastUpdated": "2024-01-05T00:00:00.000Z"},
		{"id": "in", "lastUpdated": "2024-01-15T00:00:00.000Z"},
		{"id": "at-end", "lastUpdated": "2024-01-20T00:00:00.000Z"},
		{"id": "after", "lastUpdated": "2024-01-25T00:00:00.000Z"},
		{"id": "no-ts"},
	}
	got := filterItemsByInterval(items, "lastUpdated", &start, &end)
	gotIDs := map[string]bool{}
	for _, it := range got {
		gotIDs[it["id"].(string)] = true
	}
	if !gotIDs["in"] || !gotIDs["no-ts"] {
		t.Fatalf("expected 'in' and 'no-ts' retained, got %v", gotIDs)
	}
	if gotIDs["before"] || gotIDs["at-end"] || gotIDs["after"] {
		t.Fatalf("expected before/at-end/after excluded, got %v", gotIDs)
	}
}

func TestEnvelopeExtract(t *testing.T) {
	extract := envelopeExtract("roles")
	items, err := extract([]byte(`{"roles":[{"id":"r1"},{"id":"r2"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 || items[0]["id"] != "r1" {
		t.Fatalf("got %+v", items)
	}

	// Missing key yields no items, no error.
	empty, err := extract([]byte(`{}`))
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty, got %+v err %v", empty, err)
	}
}

func TestRolesNextPage(t *testing.T) {
	body := []byte(`{"roles":[{"id":"r1"}],"_links":{"next":{"href":"https://dev-123.okta.com/api/v1/iam/roles?after=00rABC"}}}`)
	if got := rolesNextPage(body, nil); got != "https://dev-123.okta.com/api/v1/iam/roles?after=00rABC" {
		t.Fatalf("got %q", got)
	}

	// Last page has no next link.
	last := []byte(`{"roles":[{"id":"r1"}]}`)
	if got := rolesNextPage(last, nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
