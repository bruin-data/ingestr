package brevo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError string
	}{
		{name: "valid", uri: "brevo://?api_key=xkeysib-abc", wantKey: "xkeysib-abc"},
		{name: "url-encoded key", uri: "brevo://?api_key=a%2Bb", wantKey: "a+b"},
		{name: "wrong scheme", uri: "sendinblue://?api_key=x", wantError: "invalid brevo URI: must start with brevo://"},
		{name: "missing key", uri: "brevo://", wantError: "api_key is required in brevo URI"},
		{name: "empty key", uri: "brevo://?api_key=", wantError: "api_key is required in brevo URI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseURI(tt.uri)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range []string{
		"contacts", "lists", "segments", "folders", "contact_attributes",
		"email_campaigns", "sms_campaigns", "email_templates",
		"transactional_email_events", "transactional_email_reports",
		"blocked_contacts", "companies", "deals", "crm_tasks", "crm_notes", "orders", "products",
		"categories", "sms_events", "whatsapp_events", "whatsapp_campaigns", "custom_events",
	} {
		assert.True(t, isValidTable(table), table)
	}
	for _, table := range []string{"", "Contacts", "campaigns", "events"} {
		assert.False(t, isValidTable(table), table)
	}
}

func day(s string) time.Time {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestResolveDayRange(t *testing.T) {
	now := time.Date(2026, 11, 24, 15, 30, 0, 0, time.UTC)
	ptr := func(t time.Time) *time.Time { return &t }

	start, end, err := resolveDayRange(nil, nil, now)
	require.NoError(t, err)
	assert.Equal(t, day("2026-08-27"), start)
	assert.Equal(t, day("2026-11-24"), end)

	start, end, err = resolveDayRange(ptr(time.Date(2026, 1, 5, 23, 0, 0, 0, time.UTC)), ptr(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)), now)
	require.NoError(t, err)
	assert.Equal(t, day("2026-01-05"), start)
	assert.Equal(t, day("2026-02-01"), end, "end day stays inclusive to match the DATE delete bound")

	_, end, err = resolveDayRange(ptr(day("2026-11-20")), ptr(day("2027-01-01")), now)
	require.NoError(t, err)
	assert.Equal(t, day("2026-11-24"), end, "future end is capped at today")

	start, _, err = resolveDayRange(ptr(time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("+03", 3*3600))), nil, now)
	require.NoError(t, err)
	assert.Equal(t, day("2026-11-01"), start, "a bound with an offset keeps its own calendar date, like the DATE delete bound")

	nyNow := time.Date(2026, 11, 24, 22, 0, 0, 0, time.UTC).In(time.FixedZone("-05", -5*3600))
	_, end, err = resolveDayRange(ptr(day("2026-11-20")), ptr(day("2026-11-25")), nyNow)
	require.NoError(t, err)
	assert.Equal(t, day("2026-11-24"), end, "today is the account's day, not the UTC day")

	_, _, err = resolveDayRange(ptr(day("2026-12-01")), nil, now)
	require.Error(t, err)
}

func TestDecodeItemsKeepsLargeIntegers(t *testing.T) {
	items, err := decodeItems([]byte(`{"contacts":[{"id":9007199254740993}],"count":1}`), "contacts")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, json.Number("9007199254740993"), items[0]["id"])

	items, err = decodeItems([]byte(`{}`), "events")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestSplitDays(t *testing.T) {
	windows := splitDays(day("2026-01-01"), day("2026-03-01"), 30)
	assert.Equal(t, [][2]time.Time{
		{day("2026-01-01"), day("2026-01-30")},
		{day("2026-01-31"), day("2026-03-01")},
	}, windows)

	windows = splitDays(day("2026-01-01"), day("2026-03-02"), 30)
	require.Len(t, windows, 3)
	assert.Equal(t, [2]time.Time{day("2026-03-02"), day("2026-03-02")}, windows[2])

	assert.Equal(t, [][2]time.Time{{day("2026-05-05"), day("2026-05-05")}}, splitDays(day("2026-05-05"), day("2026-05-05"), 90))
}

func TestReadEventsPagesWindowsAndLiftsEventDate(t *testing.T) {
	type call struct{ start, end, offset string }
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account" {
			_, _ = w.Write([]byte(`{"dateTimePreferences":{"timezone":"Europe/Istanbul"}}`))
			return
		}
		q := r.URL.Query()
		assert.Equal(t, "/smtp/statistics/events", r.URL.Path)
		assert.Equal(t, "key", r.Header.Get("api-key"))
		calls = append(calls, call{q.Get("startDate"), q.Get("endDate"), q.Get("offset")})
		w.Header().Set("Content-Type", "application/json")
		if q.Get("startDate") == "2026-01-01" && q.Get("offset") == "0" {
			_, _ = w.Write([]byte(`{"events":[{"date":"2026-01-01T23:59:00.000+03:00","event":"delivered"},{"date":"2026-01-02T00:00:01.000Z","event":"opened"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := tableConfigs["transactional_email_events"]
	cfg.pageSize = 2
	client := httpclient.New(httpclient.WithBaseURL(srv.URL), httpclient.WithAuth(httpclient.NewAPIKeyAuth("api-key", "key", true)))
	s := &BrevoSource{clients: map[rateGroup]*httpclient.Client{groupSMTP: client, groupOther: client}}

	start, end := day("2026-01-01"), day("2026-05-01")
	results := make(chan source.RecordBatchResult, 10)
	require.NoError(t, s.readDayWindows(context.Background(), "transactional_email_events", cfg, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, results))
	close(results)

	assert.Equal(t, []call{
		{"2026-01-01", "2026-03-31", "0"},
		{"2026-01-01", "2026-03-31", "2"},
		{"2026-04-01", "2026-05-01", "0"},
	}, calls)

	var dates []string
	for r := range results {
		require.NoError(t, r.Err)
		idx := r.Batch.Schema().FieldIndices("event_date")
		require.Len(t, idx, 1)
		col := r.Batch.Column(idx[0]).(array.ExtensionArray).Storage().(*array.String)
		for i := 0; i < col.Len(); i++ {
			var d string
			require.NoError(t, json.Unmarshal([]byte(col.Value(i)), &d))
			dates = append(dates, d)
		}
	}
	assert.Equal(t, []string{"2026-01-01", "2026-01-02"}, dates, "event_date keeps the day as Brevo reports it")
}

func TestReadReturnsErrorOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/account" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_parameter"}`))
	}))
	defer srv.Close()

	client := httpclient.New(httpclient.WithBaseURL(srv.URL))
	s := &BrevoSource{clients: map[rateGroup]*httpclient.Client{groupSMTP: client, groupOther: client}}
	start := day("2026-01-01")
	results := make(chan source.RecordBatchResult, 10)
	err := s.readDayWindows(context.Background(), "transactional_email_reports", tableConfigs["transactional_email_reports"], source.ReadOptions{IntervalStart: &start, IntervalEnd: &start}, results)
	require.ErrorContains(t, err, "/smtp/statistics/reports returned status 400")
}

func TestBrevoRetryStrategyUsesResetHeader(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("x-sib-ratelimit-reset", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"lists":[{"id":1}],"count":1}`))
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithBaseURL(srv.URL),
		httpclient.WithRetry(retryCount, time.Millisecond, retryMaxWait),
		httpclient.WithRetryStrategy(brevoRetryStrategy),
	)
	began := time.Now()
	resp, err := client.R(context.Background()).Get("/contacts/lists")
	require.NoError(t, err)
	assert.True(t, resp.IsSuccess())
	assert.Equal(t, 2, attempts)
	// reset=0 waits 1s; the fallback backoff would wait 2s.
	elapsed := time.Since(began)
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	assert.Less(t, elapsed, 1900*time.Millisecond)
}

func TestReadCampaignsIncludesArchivedOnce(t *testing.T) {
	var statuses []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		statuses = append(statuses, status)
		if status == "archive" {
			_, _ = w.Write([]byte(`{"campaigns":[{"id":1,"status":"archive"},{"id":2,"status":"archive"}],"count":2}`))
			return
		}
		_, _ = w.Write([]byte(`{"campaigns":[{"id":1,"status":"draft"}],"count":1}`))
	}))
	defer srv.Close()

	s := &BrevoSource{clients: map[rateGroup]*httpclient.Client{groupOther: httpclient.New(httpclient.WithBaseURL(srv.URL))}}
	results, err := s.read(context.Background(), "email_campaigns", source.ReadOptions{})
	require.NoError(t, err)

	var got []string
	for r := range results {
		require.NoError(t, r.Err)
		idx := r.Batch.Schema().FieldIndices("status")
		require.Len(t, idx, 1)
		col := r.Batch.Column(idx[0]).(array.ExtensionArray).Storage().(*array.String)
		for i := 0; i < col.Len(); i++ {
			var v string
			require.NoError(t, json.Unmarshal([]byte(col.Value(i)), &v))
			got = append(got, v)
		}
	}
	assert.Equal(t, []string{"", "archive"}, statuses)
	assert.Equal(t, []string{"draft", "archive"}, got, "id 1 from the archive pass is skipped as a duplicate")
}

func TestSplitTimeRange(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return v
	}
	ptr := func(t time.Time) *time.Time { return &t }

	w := splitTimeRange(ptr(at("2026-01-01T00:00:00Z")), ptr(at("2026-01-11T00:00:00Z")), time.Time{}, 4)
	assert.Equal(t, [][2]time.Time{
		{at("2026-01-01T00:00:00Z"), at("2026-01-04T23:59:59.999Z")},
		{at("2026-01-05T00:00:00Z"), at("2026-01-08T23:59:59.999Z")},
		{at("2026-01-09T00:00:00Z"), at("2026-01-11T00:00:00Z")},
	}, w, "windows don't overlap and the last one keeps the inclusive interval end")

	w = splitTimeRange(ptr(at("2026-01-01T00:00:00Z")), ptr(at("2026-01-05T00:00:00Z")), time.Time{}, 4)
	assert.Equal(t, [][2]time.Time{{at("2026-01-01T00:00:00Z"), at("2026-01-05T00:00:00Z")}}, w)

	w = splitTimeRange(ptr(at("2026-01-03T00:00:00Z")), nil, at("2026-01-04T12:00:00Z"), 180)
	assert.Equal(t, [][2]time.Time{{at("2026-01-03T00:00:00Z"), at("2026-01-04T12:00:00Z")}}, w)
}

func TestReadCompaniesPagesByNumberAndLiftsModifiedAt(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pages = append(pages, q.Get("page")+"/"+q.Get("offset"))
		if q.Get("page") == "1" {
			_, _ = w.Write([]byte(`{"items":[{"id":"a","attributes":{"last_updated_at":"2026-11-26T07:59:35.774Z"}},{"id":"b","attributes":{"last_updated_at":"2026-11-26T08:00:00Z"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"c","attributes":{"last_updated_at":"2026-11-26T09:00:00Z"}}]}`))
	}))
	defer srv.Close()

	cfg := tableConfigs["companies"]
	cfg.pageSize = 2
	s := &BrevoSource{clients: map[rateGroup]*httpclient.Client{groupOther: httpclient.New(httpclient.WithBaseURL(srv.URL))}}
	results := make(chan source.RecordBatchResult, 10)
	require.NoError(t, s.paginateAndSend(context.Background(), "companies", cfg, nil, nil, source.ReadOptions{}, results))
	close(results)

	assert.Equal(t, []string{"1/", "2/"}, pages)
	var modified []string
	for r := range results {
		require.NoError(t, r.Err)
		idx := r.Batch.Schema().FieldIndices("modifiedAt")
		require.Len(t, idx, 1)
		col := r.Batch.Column(idx[0]).(array.ExtensionArray).Storage().(*array.String)
		for i := 0; i < col.Len(); i++ {
			var v string
			require.NoError(t, json.Unmarshal([]byte(col.Value(i)), &v))
			modified = append(modified, v)
		}
	}
	assert.Equal(t, []string{"2026-11-26T07:59:35.774Z", "2026-11-26T08:00:00Z", "2026-11-26T09:00:00Z"}, modified)
}

func TestDecodeItemsBareArray(t *testing.T) {
	items, err := decodeItems([]byte(`[{"id":"n1"},{"id":"n2"}]`), "")
	require.NoError(t, err)
	assert.Len(t, items, 2)
}
