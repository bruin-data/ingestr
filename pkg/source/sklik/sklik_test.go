package sklik

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestFlattenReportExpandsEntityRefs(t *testing.T) {
	raw := json.RawMessage(`{"report":[{
		"query":"kavovar deloni",
		"campaign":{"id":3456225,"name":"[SEA] Brand"},
		"group":{"id":115928034,"name":"Espresso"},
		"keyword":{"id":2590421096,"name":"kavovar","matchType":"phrase"},
		"stats":[{"date":20260701,"clicks":3,"impressions":11}]
	}]}`)

	rows, err := flattenReport(raw)
	if err != nil {
		t.Fatalf("flattenReport: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]

	for _, nested := range []string{"campaign", "group", "keyword"} {
		if _, ok := row[nested]; ok {
			t.Errorf("%q survived flattening as a nested object", nested)
		}
	}
	for col, want := range map[string]any{
		"campaign_id":       float64(3456225),
		"campaign_name":     "[SEA] Brand",
		"group_id":          float64(115928034),
		"keyword_id":        float64(2590421096),
		"keyword_matchType": "phrase",
		"query":             "kavovar deloni",
	} {
		if got := row[col]; got != want {
			t.Errorf("%s = %v (%T), want %v", col, got, got, want)
		}
	}
	if _, ok := row["date"]; !ok {
		t.Error("date missing; the per-day stat was not merged in")
	}
}

func TestSearchQueriesIsCampaignScoped(t *testing.T) {
	tc := supportedTables["search_queries"]
	if !tc.scopeToCampaigns {
		t.Fatal("search_queries lost scopeToCampaigns; the report will silently return 0 rows")
	}
	var hasKeywordID bool
	for _, pk := range tc.primaryKeys {
		if pk == "keyword_id" {
			hasKeywordID = true
		}
	}
	if !hasKeywordID {
		t.Error("keyword_id dropped from the primary key; rows for the same query will overwrite each other")
	}
}

func TestEmitStampsAccountOnEveryRow(t *testing.T) {
	s := &Source{accountID: 123456}
	items := []map[string]any{
		{"id": 527122, "name": "Rezervacni system"},
		{"id": 855744, "name": "cz-anketa_GNR-FRE-EXT"},
	}

	ctx := context.Background()
	results := make(chan source.RecordBatchResult, 1)
	if err := s.emit(ctx, items, source.ReadOptions{}, results); err != nil {
		t.Fatalf("emit: %v", err)
	}

	for i, row := range items {
		got, ok := row[accountColumn]
		if !ok {
			t.Fatalf("row %d has no %s; it is unattributable once landed", i, accountColumn)
		}
		if got != "123456" {
			t.Errorf("row %d %s = %v (%T), want the string \"123456\" so the seed join is String↔String", i, accountColumn, got, got)
		}
	}
}

func TestResolveAccountIDPrefersExplicitManagedAccount(t *testing.T) {
	s := &Source{userID: 987}
	if err := s.resolveAccountID(context.Background()); err != nil {
		t.Fatalf("resolveAccountID: %v", err)
	}
	if s.accountID != 987 {
		t.Errorf("accountID = %d, want the explicitly targeted managed account 987", s.accountID)
	}

	owner := &Source{client: stubClient(map[string]string{
		"client.loginByToken": `{"status":200,"session":"s1"}`,
		"client.get":          `{"status":200,"session":"s2","user":{"userId":555}}`,
	})}
	if err := owner.resolveAccountID(context.Background()); err != nil {
		t.Fatalf("resolveAccountID via client.get: %v", err)
	}
	if owner.accountID != 555 {
		t.Errorf("accountID = %d, want the token owner's 555 from client.get", owner.accountID)
	}
}

func TestResolveAccountIDRefusesZero(t *testing.T) {
	s := &Source{client: stubClient(map[string]string{
		"client.loginByToken": `{"status":200,"session":"s1"}`,
		"client.get":          `{"status":200,"session":"s2","user":{}}`,
	})}
	err := s.resolveAccountID(context.Background())
	if err == nil {
		t.Fatal("resolveAccountID accepted a missing userId; rows would be stamped 0")
	}
	if s.accountID != 0 {
		t.Errorf("accountID = %d, want it left unset after refusing", s.accountID)
	}
}

func stubClient(byMethod map[string]string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		parts := strings.Split(r.URL.Path, "/")
		body, ok := byMethod[parts[len(parts)-1]]
		if !ok {
			body = `{"status":404,"statusMessage":"unstubbed method"}`
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchQueryColumnsExcludeCTR(t *testing.T) {
	for _, c := range searchQueryColumns {
		if c == "ctr" {
			t.Fatal("ctr is rejected by queries.readReport and 400s the entire report")
		}
	}
}

func TestAccountIsNotInPrimaryKeys(t *testing.T) {
	for name, tc := range supportedTables {
		for _, pk := range tc.primaryKeys {
			if pk == accountColumn {
				t.Errorf("%s has %s in its primary key %v", name, accountColumn, tc.primaryKeys)
			}
		}
	}
}
