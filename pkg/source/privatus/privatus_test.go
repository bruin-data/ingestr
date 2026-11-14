package privatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantKey string
		wantIDs []string
		wantRPH int
		wantErr string
	}{
		{name: "token only", uri: "privatus://?api_key=pat_abc", wantKey: "pat_abc", wantRPH: defaultRequestsPerHour},
		{name: "site ids", uri: "privatus://?api_key=pat_abc&site_ids=pa_AB12, pa_CD34", wantKey: "pat_abc", wantIDs: []string{"pa_AB12", "pa_CD34"}, wantRPH: defaultRequestsPerHour},
		{name: "empty site ids are skipped", uri: "privatus://?api_key=pat_abc&site_ids=pa_AB12,,", wantKey: "pat_abc", wantIDs: []string{"pa_AB12"}, wantRPH: defaultRequestsPerHour},
		{name: "requests per hour", uri: "privatus://?api_key=pat_abc&requests_per_hour=5000", wantKey: "pat_abc", wantRPH: 5000},
		{name: "wrong scheme", uri: "plausible://?api_key=pat_abc", wantErr: "must start with privatus://"},
		{name: "missing token", uri: "privatus://", wantErr: "api_key is required"},
		{name: "blank token", uri: "privatus://?api_key=%20", wantErr: "api_key is required"},
		{name: "bad site id", uri: "privatus://?api_key=pat_abc&site_ids=pa_AB/../x", wantErr: "invalid site id"},
		{name: "site id without prefix", uri: "privatus://?api_key=pat_abc&site_ids=AB12", wantErr: "invalid site id"},
		{name: "requests per hour not a number", uri: "privatus://?api_key=pat_abc&requests_per_hour=lots", wantErr: "requests_per_hour"},
		{name: "requests per hour zero", uri: "privatus://?api_key=pat_abc&requests_per_hour=0", wantErr: "requests_per_hour"},
		{name: "requests per hour too high", uri: "privatus://?api_key=pat_abc&requests_per_hour=100001", wantErr: "requests_per_hour"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, conn.apiKey)
			assert.Equal(t, tt.wantIDs, conn.siteIDs)
			assert.Equal(t, tt.wantRPH, conn.requestsPerHour)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), table)
	}
	for _, table := range []string{"", "Sites", "visitors", "page"} {
		assert.False(t, isValidTable(table), table)
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		in        string
		wantTable string
		wantDim   string
		wantPK    []string
		wantErr   bool
	}{
		{in: "workspaces", wantTable: "workspaces", wantPK: []string{"id"}},
		{in: "sites", wantTable: "sites", wantPK: []string{"id"}},
		{in: "goals", wantTable: "goals", wantPK: []string{"id"}},
		{in: "daily_stats", wantTable: "daily_stats", wantPK: []string{"site_id", "date"}},
		{in: "pages", wantTable: "pages", wantDim: "page", wantPK: []string{"site_id", "date", "value"}},
		{in: "referrers", wantTable: "referrers", wantDim: "referrer", wantPK: []string{"site_id", "date", "value"}},
		{in: "events", wantTable: "events", wantDim: "event", wantPK: []string{"site_id", "date", "value"}},
		{in: "goal_conversions", wantTable: "goal_conversions", wantDim: "goal", wantPK: []string{"site_id", "date", "goal_id"}},
		{in: "breakdown:country", wantTable: "breakdown", wantDim: "country", wantPK: []string{"site_id", "date", "value"}},
		{in: "breakdown:prop:plan", wantTable: "breakdown", wantDim: "prop:plan", wantPK: []string{"site_id", "date", "value"}},
		{in: "breakdown", wantErr: true},
		{in: "breakdown:", wantErr: true},
		{in: "breakdown:../stats", wantErr: true},
		{in: "breakdown:Country", wantErr: true},
		{in: "pages:extra", wantErr: true},
		{in: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			spec, err := parseTableName(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, spec.table)
			assert.Equal(t, tt.wantDim, spec.dimension)
			assert.Equal(t, tt.wantPK, spec.primaryKeys())
		})
	}
}

func TestDateRange(t *testing.T) {
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	day := func(s string) *time.Time {
		v, err := time.Parse(dateLayout, s)
		require.NoError(t, err)
		return &v
	}

	start, end, err := dateRange(source.ReadOptions{}, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-29", start.Format(dateLayout))
	assert.Equal(t, "2026-10-28", end.Format(dateLayout))

	start, end, err = dateRange(source.ReadOptions{IntervalStart: day("2026-10-01"), IntervalEnd: day("2026-10-03")}, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-01", start.Format(dateLayout))
	assert.Equal(t, "2026-10-03", end.Format(dateLayout))

	_, end, err = dateRange(source.ReadOptions{IntervalStart: day("2026-10-01"), IntervalEnd: day("2027-01-01")}, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-28", end.Format(dateLayout))

	_, _, err = dateRange(source.ReadOptions{IntervalStart: day("2026-10-05"), IntervalEnd: day("2026-10-01")}, now)
	require.Error(t, err)
}

func TestGetTableStrategy(t *testing.T) {
	s := NewPrivatusSource()
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: "sites"})
	require.NoError(t, err)
	dyn := tbl.(*source.DynamicSourceTable)
	assert.Equal(t, config.StrategyReplace, dyn.TableStrategy)
	assert.Empty(t, dyn.TableIncrementalKey)

	tbl, err = s.GetTable(context.Background(), source.TableRequest{Name: "breakdown:country"})
	require.NoError(t, err)
	dyn = tbl.(*source.DynamicSourceTable)
	assert.Equal(t, config.StrategyMerge, dyn.TableStrategy)
	assert.Equal(t, "date", dyn.TableIncrementalKey)
}

type fakeAPI struct {
	mu       sync.Mutex
	requests []string
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer pat_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/workspaces.json":
			writeJSON(t, w, map[string]interface{}{
				"data": []map[string]interface{}{{"id": "ws_ONE", "name": "Main"}},
				"meta": map[string]interface{}{"page": 1, "per_page": 100, "total_pages": 1},
			})
		case r.URL.Path == "/workspaces/ws_ONE/sites.json":
			writeJSON(t, w, map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "pa_AAA", "domain": "a.example", "snippet": "<script>", "ingest_key_prefix": nil},
					{"id": "pa_BBB", "domain": "b.example", "snippet": "<script>", "ingest_key_prefix": nil},
				},
				"meta": map[string]interface{}{"page": 1, "per_page": 100, "total_pages": 1},
			})
		case strings.HasSuffix(r.URL.Path, "/goals.json"):
			assert.Equal(t, "all", q.Get("status"))
			writeJSON(t, w, map[string]interface{}{
				"data": []map[string]interface{}{{"id": "goal_X", "name": "Signup", "stats": map[string]interface{}{"completions": 3}}},
				"meta": map[string]interface{}{"page": 1, "per_page": 100, "total_pages": 1},
			})
		case strings.HasSuffix(r.URL.Path, "/stats/timeseries.json"):
			assert.Equal(t, "custom", q.Get("period"))
			assert.Equal(t, "day", q.Get("interval"))
			from, _ := time.Parse(dateLayout, q.Get("from"))
			to, _ := time.Parse(dateLayout, q.Get("to"))
			var series []map[string]interface{}
			for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
				series = append(series, map[string]interface{}{"date": d.Format(dateLayout) + "T00:00:00Z", "visitors": 10, "views_per_visit": 1.5})
			}
			writeJSON(t, w, map[string]interface{}{"data": map[string]interface{}{"interval": "day", "series": series}})
		case strings.Contains(r.URL.Path, "/breakdown/"):
			assert.Equal(t, q.Get("from"), q.Get("to"))
			assert.Equal(t, "1000", q.Get("limit"))
			page := q.Get("page")
			rows := []map[string]interface{}{{"value": "/p" + page, "pageviews": 4}}
			hasMore := page == "1"
			if strings.HasSuffix(r.URL.Path, "/goal.json") {
				rows = []map[string]interface{}{{"value": "Signup", "goal_id": "goal_X", "completions": 2}}
				hasMore = false
			}
			writeJSON(t, w, map[string]interface{}{"data": map[string]interface{}{"has_more": hasMore, "rows": rows}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func newTestSource(t *testing.T, token string, siteIDs ...string) (*PrivatusSource, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{}
	server := httptest.NewServer(api.handler(t))
	t.Cleanup(server.Close)

	s := NewPrivatusSource()
	s.siteIDs = siteIDs
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(server.URL),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(token)),
		ingestrhttp.WithDisableRetry(),
	)
	t.Cleanup(func() { _ = s.client.Close() })
	return s, api
}

func readRows(t *testing.T, s *PrivatusSource, table string, opts source.ReadOptions) ([]map[string]string, error) {
	t.Helper()
	tbl, err := s.GetTable(context.Background(), source.TableRequest{Name: table})
	require.NoError(t, err)
	results, err := tbl.Read(context.Background(), opts)
	require.NoError(t, err)

	var rows []map[string]string
	var readErr error
	for res := range results {
		if res.Err != nil {
			readErr = res.Err
			continue
		}
		rec := res.Batch
		for i := 0; i < int(rec.NumRows()); i++ {
			row := map[string]string{}
			for c := 0; c < int(rec.NumCols()); c++ {
				col := rec.Column(c)
				if col.IsNull(i) {
					continue
				}
				if ext, ok := col.(array.ExtensionArray); ok {
					v, _ := schemainfer.StringValueAt(ext.Storage(), i)
					if unquoted, err := strconv.Unquote(v); err == nil {
						v = unquoted
					}
					row[rec.ColumnName(c)] = v
					continue
				}
				row[rec.ColumnName(c)] = col.ValueStr(i)
			}
			rows = append(rows, row)
		}
		rec.Release()
	}
	return rows, readErr
}

func interval(from, to string) source.ReadOptions {
	start, _ := time.Parse(dateLayout, from)
	end, _ := time.Parse(dateLayout, to)
	return source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}
}

func TestReadSitesDropsSetupFieldsAndFilters(t *testing.T) {
	s, _ := newTestSource(t, "pat_test", "pa_BBB")
	rows, err := readRows(t, s, "sites", source.ReadOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "pa_BBB", rows[0]["id"])
	_, hasSnippet := rows[0]["snippet"]
	assert.False(t, hasSnippet)
}

func TestReadWorkspaces(t *testing.T) {
	s, _ := newTestSource(t, "pat_test")
	rows, err := readRows(t, s, "workspaces", source.ReadOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "ws_ONE", rows[0]["id"])
}

func TestReadGoalsAddsSiteAndDropsStats(t *testing.T) {
	s, _ := newTestSource(t, "pat_test")
	rows, err := readRows(t, s, "goals", source.ReadOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "pa_AAA", rows[0]["site_id"])
	assert.Equal(t, "pa_BBB", rows[1]["site_id"])
	_, hasStats := rows[0]["stats"]
	assert.False(t, hasStats)
}

func TestReadDailyStatsSplitsWindows(t *testing.T) {
	s, api := newTestSource(t, "pat_test", "pa_AAA")
	rows, err := readRows(t, s, "daily_stats", interval("2026-01-01", "2026-04-30"))
	require.NoError(t, err)
	assert.Len(t, rows, 120)
	assert.Equal(t, "2026-01-01", rows[0]["date"])
	assert.Equal(t, "pa_AAA", rows[0]["site_id"])
	assert.Equal(t, "2026-04-30", rows[119]["date"])

	var windows []string
	for _, req := range api.requests {
		if strings.Contains(req, "timeseries") {
			windows = append(windows, req)
		}
	}
	require.Len(t, windows, 2)
	assert.Contains(t, windows[0], "from=2026-01-01")
	assert.Contains(t, windows[0], "to=2026-03-31")
	assert.Contains(t, windows[1], "from=2026-04-01")
	assert.Contains(t, windows[1], "to=2026-04-30")
}

func TestReadBreakdownPerDayWithPages(t *testing.T) {
	s, api := newTestSource(t, "pat_test", "pa_AAA", "pa_BBB")
	rows, err := readRows(t, s, "pages", interval("2026-10-01", "2026-10-02"))
	require.NoError(t, err)
	// 2 sites x 2 days x 2 pages, one row each.
	assert.Len(t, rows, 8)
	assert.Equal(t, map[string]string{"site_id": "pa_AAA", "date": "2026-10-01", "value": "/p1", "pageviews": "4"}, rows[0])
	for _, req := range api.requests {
		assert.NotContains(t, req, "workspaces", "site_ids skips site discovery")
	}
}

func TestReadGoalConversions(t *testing.T) {
	s, _ := newTestSource(t, "pat_test", "pa_AAA")
	rows, err := readRows(t, s, "goal_conversions", interval("2026-10-01", "2026-10-01"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "goal_X", rows[0]["goal_id"])
}

func TestReadBreakdownDiscoversSites(t *testing.T) {
	s, _ := newTestSource(t, "pat_test")
	rows, err := readRows(t, s, "breakdown:country", interval("2026-10-01", "2026-10-01"))
	require.NoError(t, err)
	assert.Len(t, rows, 4)
}

func TestReadReportsAPIError(t *testing.T) {
	s, _ := newTestSource(t, "pat_wrong")
	_, err := readRows(t, s, "sites", source.ReadOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "workspaces")
}

func TestBatcherHonorsMaxBatchBytes(t *testing.T) {
	results := make(chan source.RecordBatchResult, 100)
	b := newBatcher(source.ReadOptions{MaxBatchBytes: 1}, results)
	for i := 0; i < 3; i++ {
		require.NoError(t, b.add(map[string]interface{}{"value": fmt.Sprintf("row-%d", i)}))
	}
	require.NoError(t, b.flush())
	close(results)
	batches := 0
	for res := range results {
		require.NoError(t, res.Err)
		assert.EqualValues(t, 1, res.Batch.NumRows())
		res.Batch.Release()
		batches++
	}
	assert.Equal(t, 3, batches)
}
