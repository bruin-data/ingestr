package deel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantKey         string
		wantEnvironment string
		wantErr         bool
	}{
		{name: "production default", uri: "deel://?api_key=token-123", wantKey: "token-123", wantEnvironment: "production"},
		{name: "production explicit", uri: "deel://?api_key=token-123&environment=production", wantKey: "token-123", wantEnvironment: "production"},
		{name: "sandbox", uri: "deel://?api_key=token-123&environment=sandbox", wantKey: "token-123", wantEnvironment: "sandbox"},
		{name: "without question mark", uri: "deel://api_key=token-123", wantKey: "token-123", wantEnvironment: "production"},
		{name: "missing key", uri: "deel://?environment=sandbox", wantErr: true},
		{name: "invalid environment", uri: "deel://?api_key=token-123&environment=staging", wantErr: true},
		{name: "wrong scheme", uri: "stripe://?api_key=token-123", wantErr: true},
		{name: "empty", uri: "deel://", wantErr: true},
		{name: "empty query", uri: "deel://?", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.apiKey != tt.wantKey {
				t.Errorf("api key = %q, want %q", creds.apiKey, tt.wantKey)
			}
			if creds.environment != tt.wantEnvironment {
				t.Errorf("environment = %q, want %q", creds.environment, tt.wantEnvironment)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTableNames() {
		if !isValidTable(table) {
			t.Errorf("isValidTable(%q) = false, want true", table)
		}
	}

	for _, table := range []string{"", "unknown", "People", "CONTRACTS", "contract"} {
		if isValidTable(table) {
			t.Errorf("isValidTable(%q) = true, want false", table)
		}
	}
}

func TestReviewedEndpointMetadata(t *testing.T) {
	if got := directTables["ats_offers"].pageSizeParam; got != "" {
		t.Errorf("ats_offers page size parameter = %q, want empty", got)
	}
	if got := directTables["invoices"].query["status"]; got != "all" {
		t.Errorf("invoices status = %q, want all", got)
	}
	if got := fanoutTables["contract_amendments"].endpoint.pagination; got != paginationCursor {
		t.Errorf("contract_amendments pagination = %v, want paginationCursor", got)
	}

	expectedVersions := map[string]string{
		"ats_interviews": "2026-06-03",
		"ats_openings":   "2026-06-15",
		"ats_teams":      "2026-06-24",
	}
	for table, want := range expectedVersions {
		if got := directTables[table].version; got != want {
			t.Errorf("%s version = %q, want %q", table, got, want)
		}
	}

	for _, table := range []string{"ats_candidates", "ats_jobs", "ats_email_templates", "ats_interviews"} {
		if !directTables[table].clientFilter {
			t.Errorf("%s must apply the interval end client-side", table)
		}
		if !directTables[table].startParamExclusive {
			t.Errorf("%s must overlap its exclusive updated_after lower bound", table)
		}
	}

	for _, table := range []string{"timesheets", "invoice_adjustments", "invoices", "payments", "refund_statements"} {
		if !directTables[table].clientFilter {
			t.Errorf("%s must apply exact timestamp bounds client-side", table)
		}
	}

	if got := directTables["seniorities"].query["is_eor_contract"]; got != "false" {
		t.Errorf("seniorities is_eor_contract = %q, want false", got)
	}
	wantContractTypes := []string{"peo", "global_payroll", "hris_direct_employee", "eor", "employee", "independent_contractor"}
	if got := directTables["adjustment_categories"].queryValues["contract_types"]; !slices.Equal(got, wantContractTypes) {
		t.Errorf("adjustment_categories contract_types = %v, want %v", got, wantContractTypes)
	}
	if got := directTables["hourly_report_root_presets"].queryValues; len(got) != 0 {
		t.Errorf("hourly_report_root_presets query values = %v, want none", got)
	}
	for _, table := range []string{"contract_custom_field_values", "contract_payment_cycles", "eor_benefits"} {
		if !fanoutTables[table].skipNotFound {
			t.Errorf("%s must treat a missing child collection as empty", table)
		}
	}
	if !fanoutTables["payroll_cycles"].endpoint.clientFilter {
		t.Error("payroll_cycles must enforce exact bounds across overlapping date queries")
	}
}

func TestFlattenReportCell(t *testing.T) {
	items := []map[string]any{
		{"contractId": map[string]any{"type": "text", "currentValue": "contract-123"}},
		{"contractId": "already-flat"},
		{"contractId": map[string]any{"type": "text"}},
	}

	flattenReportCell(items, "contractId")

	if got := items[0]["contractId"]; got != "contract-123" {
		t.Errorf("flattened contractId = %v, want contract-123", got)
	}
	if got := items[1]["contractId"]; got != "already-flat" {
		t.Errorf("flat contractId = %v, want already-flat", got)
	}
	if _, ok := items[2]["contractId"].(map[string]any); !ok {
		t.Errorf("cell without currentValue should remain unchanged")
	}
}

func TestSendItemsStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sendItems(ctx, []map[string]any{{"id": "one"}}, endpointMeta{}, source.ReadOptions{}, make(chan source.RecordBatchResult))
	if err != context.Canceled {
		t.Fatalf("sendItems returned %v, want context.Canceled", err)
	}
}

func TestSendErrorStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sendResult(ctx, make(chan source.RecordBatchResult), source.RecordBatchResult{Err: context.Canceled})
	if err != context.Canceled {
		t.Fatalf("sendResult returned %v, want context.Canceled", err)
	}
}

func TestDecodeObjectUsesNumber(t *testing.T) {
	result, err := decodeObject([]byte(`{"data":[{"id":9007199254740993}]}`))
	if err != nil {
		t.Fatalf("decodeObject returned an error: %v", err)
	}

	items, err := extractItems(result, "data", "")
	if err != nil {
		t.Fatalf("extractItems returned an error: %v", err)
	}
	id, ok := items[0]["id"].(json.Number)
	if !ok {
		t.Fatalf("id has type %T, want json.Number", items[0]["id"])
	}
	if id.String() != "9007199254740993" {
		t.Errorf("id = %s, want 9007199254740993", id)
	}
}

func TestExtractItems(t *testing.T) {
	tests := []struct {
		name           string
		body           map[string]any
		path           string
		primitiveField string
		want           []map[string]any
	}{
		{
			name: "array", path: "data",
			body: map[string]any{"data": []any{map[string]any{"id": "one"}, map[string]any{"id": "two"}}},
			want: []map[string]any{{"id": "one"}, {"id": "two"}},
		},
		{
			name: "singleton", path: "data",
			body: map[string]any{"data": map[string]any{"id": "one"}},
			want: []map[string]any{{"id": "one"}},
		},
		{
			name: "nested", path: "data.rows",
			body: map[string]any{"data": map[string]any{"rows": []any{map[string]any{"id": "one"}}}},
			want: []map[string]any{{"id": "one"}},
		},
		{
			name: "primitives", path: "data", primitiveField: "name",
			body: map[string]any{"data": []any{"paid", "unpaid"}},
			want: []map[string]any{{"name": "paid"}, {"name": "unpaid"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractItems(tt.body, tt.path, tt.primitiveField)
			if err != nil {
				t.Fatalf("extractItems returned an error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d items, want %d", len(got), len(tt.want))
			}
			for i := range got {
				for key, want := range tt.want[i] {
					if got[i][key] != want {
						t.Errorf("item %d %s = %v, want %v", i, key, got[i][key], want)
					}
				}
			}
		})
	}
}

func TestFilterItemsByInterval(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	items := []map[string]any{
		{"id": "before", "updated_at": "2025-12-31T23:59:59Z"},
		{"id": "at-start", "updated_at": "2026-01-01T00:00:00Z"},
		{"id": "inside", "updated_at": "2026-01-15T12:00:00.123456Z"},
		{"id": "at-end", "updated_at": "2026-02-01T00:00:00Z"},
		{"id": "missing"},
	}

	got := filterItemsByInterval(items, "updated_at", &start, &end)
	if len(got) != 3 || got[0]["id"] != "at-start" || got[1]["id"] != "inside" || got[2]["id"] != "missing" {
		t.Fatalf("filterItemsByInterval returned %v", got)
	}
	if got := filterItemsByInterval(items, "", &start, &end); len(got) != len(items) {
		t.Errorf("empty field returned %d items, want %d", len(got), len(items))
	}
}

func TestFetchPages(t *testing.T) {
	tests := []struct {
		name       string
		meta       endpointMeta
		responses  []string
		wantParams []map[string]string
	}{
		{
			name: "page cursor",
			meta: endpointMeta{path: "/records", pagination: paginationPageCursor, pageSize: 2, pageSizeParam: "limit", cursorParam: "after_cursor"},
			responses: []string{
				`{"data":[{"id":"one"},{"id":"two"}],"page":{"cursor":"next-page"}}`,
				`{"data":[{"id":"three"}],"page":{"cursor":null}}`,
			},
			wantParams: []map[string]string{{"limit": "2"}, {"limit": "2", "after_cursor": "next-page"}},
		},
		{
			name: "offset",
			meta: endpointMeta{path: "/records", pagination: paginationOffset, pageSize: 2, pageSizeParam: "limit", offsetParam: "offset"},
			responses: []string{
				`{"data":[{"id":"one"},{"id":"two"}]}`,
				`{"data":[{"id":"three"}]}`,
			},
			wantParams: []map[string]string{{"limit": "2", "offset": "0"}, {"limit": "2", "offset": "2"}},
		},
		{
			name: "nested cursor",
			meta: endpointMeta{path: "/records", pagination: paginationDataNextCursor, cursorParam: "cursor", dataPath: "data.rows"},
			responses: []string{
				`{"data":{"rows":[{"id":"one"}],"next_cursor":"next-page"}}`,
				`{"data":{"rows":[{"id":"two"}],"next_cursor":null}}`,
			},
			wantParams: []map[string]string{{}, {"cursor": "next-page"}},
		},
		{
			name: "top-level cursor",
			meta: endpointMeta{path: "/records", pagination: paginationCursor, pageSize: 2, pageSizeParam: "limit", cursorParam: "cursor"},
			responses: []string{
				`{"data":[{"id":"one"},{"id":"two"}],"cursor":"next-page"}`,
				`{"data":[{"id":"three"}],"cursor":null}`,
			},
			wantParams: []map[string]string{{"limit": "2"}, {"limit": "2", "cursor": "next-page"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestIndex := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requestIndex >= len(tt.responses) {
					t.Errorf("unexpected request %d", requestIndex)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
					return
				}
				for key, want := range tt.wantParams[requestIndex] {
					if got := r.URL.Query().Get(key); got != want {
						t.Errorf("request %d query %s = %q, want %q", requestIndex, key, got, want)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.responses[requestIndex]))
				requestIndex++
			}))
			defer server.Close()

			s := NewDeelSource()
			s.client = httpclient.New(httpclient.WithBaseURL(server.URL))
			t.Cleanup(func() {
				if err := s.client.Close(); err != nil {
					t.Errorf("failed to close HTTP client: %v", err)
				}
			})

			var ids []string
			err := s.fetchPages(context.Background(), tt.meta, source.ReadOptions{}, func(items []map[string]any) error {
				for _, item := range items {
					ids = append(ids, stringValue(item["id"]))
				}
				return nil
			})
			if err != nil {
				t.Fatalf("fetchPages returned an error: %v", err)
			}
			if requestIndex != len(tt.responses) {
				t.Errorf("made %d requests, want %d", requestIndex, len(tt.responses))
			}
			if len(ids) != 3 && tt.name != "nested cursor" {
				t.Errorf("received %d records, want 3", len(ids))
			}
			if tt.name == "nested cursor" && len(ids) != 2 {
				t.Errorf("received %d records, want 2", len(ids))
			}
		})
	}
}

func TestFetchPagesSendsRepeatedQueryValues(t *testing.T) {
	wantStatuses := []string{"PENDING", "COMPLETED", "DISMISSED", "FAILED"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query()["statuses"]; !slices.Equal(got, wantStatuses) {
			t.Errorf("statuses = %v, want %v", got, wantStatuses)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	s := NewDeelSource()
	s.client = httpclient.New(httpclient.WithBaseURL(server.URL))
	t.Cleanup(func() {
		if err := s.client.Close(); err != nil {
			t.Errorf("failed to close HTTP client: %v", err)
		}
	})

	if err := s.fetchPages(context.Background(), directTables["organization_tasks"], source.ReadOptions{}, func(items []map[string]any) error {
		return nil
	}); err != nil {
		t.Fatalf("fetchPages returned an error: %v", err)
	}
}

func TestSetIntervalParams(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	nonMidnightEnd := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)
	midnightEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	for _, tt := range []struct {
		name     string
		meta     endpointMeta
		end      time.Time
		wantFrom string
		wantTo   string
	}{
		{
			name: "date range broadens non-midnight end",
			meta: endpointMeta{intervalStartParam: "from", intervalEndParam: "to", intervalFormat: intervalDate},
			end:  nonMidnightEnd, wantFrom: "2026-01-02", wantTo: "2026-01-04",
		},
		{
			name: "date range preserves midnight end",
			meta: endpointMeta{intervalStartParam: "from", intervalEndParam: "to", intervalFormat: intervalDate},
			end:  midnightEnd, wantFrom: "2026-01-02", wantTo: "2026-01-03",
		},
		{
			name: "datetime preserves fractional seconds",
			meta: endpointMeta{intervalStartParam: "from", intervalEndParam: "to", intervalFormat: intervalDateTime},
			end:  nonMidnightEnd.Add(987654321), wantFrom: "2026-01-02T03:04:05.123456789Z", wantTo: "2026-01-03T12:00:00.987654321Z",
		},
		{
			name: "exclusive start overlaps",
			meta: endpointMeta{intervalStartParam: "from", intervalEndParam: "to", intervalFormat: intervalDateTime, startParamExclusive: true},
			end:  nonMidnightEnd, wantFrom: "2026-01-02T03:04:04.123456789Z", wantTo: "2026-01-03T12:00:00Z",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var queryValues map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queryValues = map[string]string{
					"from": r.URL.Query().Get("from"),
					"to":   r.URL.Query().Get("to"),
				}
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer server.Close()

			s := NewDeelSource()
			s.client = httpclient.New(httpclient.WithBaseURL(server.URL))
			t.Cleanup(func() {
				if err := s.client.Close(); err != nil {
					t.Errorf("failed to close HTTP client: %v", err)
				}
			})

			meta := tt.meta
			meta.path = "/records"
			err := s.fetchPages(context.Background(), meta, source.ReadOptions{IntervalStart: &start, IntervalEnd: &tt.end}, func(items []map[string]any) error {
				return nil
			})
			if err != nil {
				t.Fatalf("fetchPages returned an error: %v", err)
			}
			if queryValues["from"] != tt.wantFrom || queryValues["to"] != tt.wantTo {
				t.Errorf("query values = %v, want from=%q to=%q", queryValues, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestOffboardingPagination(t *testing.T) {
	payload := `{"limit":2,"pagination":{"progressStatusWeight":1,"referenceDate":"2026-01-01T00:00:00Z","contractId":"contract-123"},"sortOrder":"ASC"}`
	cursor := base64.RawURLEncoding.EncodeToString([]byte(payload))

	params, err := decodeOffboardingCursor(cursor)
	if err != nil {
		t.Fatalf("decodeOffboardingCursor returned an error: %v", err)
	}
	if params["contractId"] != "contract-123" || params["progressStatusWeight"] != "1" {
		t.Fatalf("decoded parameters = %v", params)
	}

	requestIndex := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestIndex == 1 {
			if got := r.URL.Query().Get("pagination[contractId]"); got != "contract-123" {
				t.Errorf("pagination[contractId] = %q, want contract-123", got)
			}
			if got := r.URL.Query().Get("pagination[progressStatusWeight]"); got != "1" {
				t.Errorf("pagination[progressStatusWeight] = %q, want 1", got)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if requestIndex == 0 {
			_, _ = w.Write([]byte(`{"data":[{"id":"one"}],"page":{"cursor":"` + cursor + `"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"two"}],"page":{"cursor":null}}`))
		}
		requestIndex++
	}))
	defer server.Close()

	s := NewDeelSource()
	s.client = httpclient.New(httpclient.WithBaseURL(server.URL))
	t.Cleanup(func() {
		if err := s.client.Close(); err != nil {
			t.Errorf("failed to close HTTP client: %v", err)
		}
	})

	meta := endpointMeta{path: "/offboarding", pagination: paginationOffboarding, pageSize: 2, pageSizeParam: "limit"}
	var count int
	err = s.fetchPages(context.Background(), meta, source.ReadOptions{}, func(items []map[string]any) error {
		count += len(items)
		return nil
	})
	if err != nil {
		t.Fatalf("fetchPages returned an error: %v", err)
	}
	if requestIndex != 2 || count != 2 {
		t.Errorf("made %d requests and received %d rows, want 2 and 2", requestIndex, count)
	}
}

func TestPayrollIntervals(t *testing.T) {
	start := time.Date(2023, 3, 1, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	intervals := payrollIntervals(source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})

	if len(intervals) != 7 {
		t.Fatalf("payrollIntervals returned %d intervals, want 7", len(intervals))
	}
	for i, interval := range intervals {
		requestOpts := payrollRequestOptions(interval)
		if requestOpts.IntervalEnd.After(interval.IntervalStart.AddDate(1, 0, 0)) {
			t.Errorf("request interval %d is longer than one year: %s to %s", i, interval.IntervalStart, requestOpts.IntervalEnd)
		}
		if i > 0 {
			wantStart := *intervals[i-1].IntervalEnd
			if !interval.IntervalStart.Equal(wantStart) {
				t.Errorf("interval %d starts at %s, want %s", i, interval.IntervalStart, wantStart)
			}
		}
	}
	if !intervals[0].IntervalStart.Equal(start) || !intervals[len(intervals)-1].IntervalEnd.Equal(end) {
		t.Errorf("intervals do not cover the requested bounds")
	}
}

func TestStringValue(t *testing.T) {
	for _, tt := range []struct {
		input any
		want  string
	}{
		{input: "abc", want: "abc"},
		{input: json.Number("9007199254740993"), want: "9007199254740993"},
		{input: float64(42), want: "42"},
		{input: nil, want: ""},
	} {
		if got := stringValue(tt.input); got != tt.want {
			t.Errorf("stringValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func BenchmarkExtractItems(b *testing.B) {
	data := make([]any, 100)
	for i := range data {
		data[i] = map[string]any{"id": strconv.Itoa(i)}
	}
	body := map[string]any{"data": data}
	b.ResetTimer()
	for range b.N {
		_, _ = extractItems(body, "data", "")
	}
}
