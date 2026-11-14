package appleads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReportTableName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantBase    string
		wantGran    string
		wantGroupBy []string
		wantErr     string
	}{
		{
			name:     "base table only no granularity",
			input:    "campaign_reports",
			wantBase: "campaign_reports",
			wantGran: "",
		},
		{
			name:     "explicit daily",
			input:    "campaign_reports:daily",
			wantBase: "campaign_reports",
			wantGran: "DAILY",
		},
		{
			name:     "hourly",
			input:    "ad_group_reports:hourly",
			wantBase: "ad_group_reports",
			wantGran: "HOURLY",
		},
		{
			name:     "weekly",
			input:    "keyword_reports:weekly",
			wantBase: "keyword_reports",
			wantGran: "WEEKLY",
		},
		{
			name:     "monthly",
			input:    "search_term_reports:monthly",
			wantBase: "search_term_reports",
			wantGran: "MONTHLY",
		},
		{
			name:     "case insensitive granularity",
			input:    "campaign_reports:HOURLY",
			wantBase: "campaign_reports",
			wantGran: "HOURLY",
		},
		{
			name:    "invalid granularity",
			input:   "campaign_reports:yearly",
			wantErr: `invalid granularity "yearly"`,
		},
		{
			name:        "daily with single groupBy",
			input:       "campaign_reports:daily:countryOrRegion",
			wantBase:    "campaign_reports",
			wantGran:    "DAILY",
			wantGroupBy: []string{"countryOrRegion"},
		},
		{
			name:        "hourly with multiple groupBy",
			input:       "campaign_reports:hourly:ageRange,gender",
			wantBase:    "campaign_reports",
			wantGran:    "HOURLY",
			wantGroupBy: []string{"ageRange", "gender"},
		},
		{
			name:        "all valid groupBy fields",
			input:       "campaign_reports:daily:countryOrRegion,ageRange,gender,deviceClass,adminArea,locality,countryCode",
			wantBase:    "campaign_reports",
			wantGran:    "DAILY",
			wantGroupBy: []string{"countryOrRegion", "ageRange", "gender", "deviceClass", "adminArea", "locality", "countryCode"},
		},
		{
			name:    "invalid groupBy field",
			input:   "campaign_reports:daily:invalidField",
			wantErr: `invalid groupBy field "invalidField"`,
		},
		{
			name:    "one valid one invalid groupBy",
			input:   "campaign_reports:daily:gender,bogus",
			wantErr: `invalid groupBy field "bogus"`,
		},
		{
			name:        "no granularity with groupBy (double colon)",
			input:       "campaign_reports::countryOrRegion",
			wantBase:    "campaign_reports",
			wantGran:    "",
			wantGroupBy: []string{"countryOrRegion"},
		},
		{
			name:        "no granularity with multiple groupBy",
			input:       "campaign_reports::ageRange,gender",
			wantBase:    "campaign_reports",
			wantGran:    "",
			wantGroupBy: []string{"ageRange", "gender"},
		},
		{
			name:    "groupBy without double colon fails as invalid granularity",
			input:   "campaign_reports:countryOrRegion",
			wantErr: `invalid granularity "countryOrRegion"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReportTableName(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBase, got.baseTable)
			assert.Equal(t, tt.wantGran, got.granularity)
			assert.Equal(t, tt.wantGroupBy, got.groupBy)
		})
	}
}

func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}

func TestReportDateRange(t *testing.T) {
	t.Run("defaults per granularity", func(t *testing.T) {
		for _, tc := range []struct {
			gran     string
			lookback int
		}{
			{"HOURLY", 7},
			{"DAILY", 90},
			{"WEEKLY", 365},
			{"MONTHLY", 730},
		} {
			t.Run(tc.gran, func(t *testing.T) {
				start, end, err := reportDateRange(source.ReadOptions{}, tc.gran)
				require.NoError(t, err)
				now := time.Now().UTC()
				assert.Equal(t, dateStr(now), dateStr(end))
				expected := now.AddDate(0, 0, -tc.lookback)
				assert.Equal(t, dateStr(expected), dateStr(start))
			})
		}
	})

	t.Run("respects IntervalStart and IntervalEnd", func(t *testing.T) {
		now := time.Now().UTC()
		s := now.AddDate(0, 0, -30)
		e := now.AddDate(0, 0, -10)
		start, end, err := reportDateRange(source.ReadOptions{
			IntervalStart: &s,
			IntervalEnd:   &e,
		}, "DAILY")
		require.NoError(t, err)
		assert.Equal(t, dateStr(s), dateStr(start))
		assert.Equal(t, dateStr(e), dateStr(end))
	})

	t.Run("errors when start too old for HOURLY", func(t *testing.T) {
		now := time.Now().UTC()
		tooOld := now.AddDate(0, 0, -60)
		_, _, err := reportDateRange(source.ReadOptions{
			IntervalStart: &tooOld,
		}, "HOURLY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too far in the past")
		assert.Contains(t, err.Error(), "HOURLY")
	})

	t.Run("errors when start too old for DAILY", func(t *testing.T) {
		now := time.Now().UTC()
		tooOld := now.AddDate(0, 0, -200)
		_, _, err := reportDateRange(source.ReadOptions{
			IntervalStart: &tooOld,
		}, "DAILY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too far in the past")
		assert.Contains(t, err.Error(), "DAILY")
	})
}

func TestParseURI(t *testing.T) {
	t.Run("valid URI with key_base64", func(t *testing.T) {
		uri := "appleads://?client_id=cid&team_id=tid&key_id=kid&org_id=111,222&key_base64=dGVzdC1rZXk="
		cfg, err := parseURI(uri)
		require.NoError(t, err)
		assert.Equal(t, "cid", cfg.clientID)
		assert.Equal(t, "tid", cfg.teamID)
		assert.Equal(t, "kid", cfg.keyID)
		assert.Equal(t, []string{"111", "222"}, cfg.orgIDs)
		assert.Equal(t, "test-key", cfg.privateKey)
	})

	t.Run("missing client_id", func(t *testing.T) {
		_, err := parseURI("appleads://?team_id=tid&key_id=kid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client_id")
	})

	t.Run("missing team_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&key_id=kid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "team_id")
	})

	t.Run("missing key_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key_id")
	})

	t.Run("missing org_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&key_id=kid&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "org_id")
	})

	t.Run("missing both key_path and key_base64", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&key_id=kid&org_id=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key_path or key_base64")
	})

	t.Run("invalid scheme", func(t *testing.T) {
		_, err := parseURI("postgres://host/db")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid appleads URI")
	})

	t.Run("ad_account_id", func(t *testing.T) {
		cfg, err := parseURI("appleads://?client_id=c&team_id=t&key_id=k&ad_account_id=20472490,333&key_base64=dGVzdA==")
		require.NoError(t, err)
		assert.Equal(t, []string{"20472490", "333"}, cfg.orgIDs)
	})

	t.Run("ad_account_id wins over org_id", func(t *testing.T) {
		cfg, err := parseURI("appleads://?client_id=c&team_id=t&key_id=k&ad_account_id=1&org_id=2&key_base64=dGVzdA==")
		require.NoError(t, err)
		assert.Equal(t, []string{"1"}, cfg.orgIDs)
	})

	t.Run("single org_id", func(t *testing.T) {
		cfg, err := parseURI("appleads://?client_id=c&team_id=t&key_id=k&org_id=999&key_base64=dGVzdA==")
		require.NoError(t, err)
		assert.Equal(t, []string{"999"}, cfg.orgIDs)
	})
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %q to be valid", table)
	}
	assert.False(t, isValidTable("keywords"))
	assert.False(t, isValidTable("search_terms"))
	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable("campaigns:daily"))
	assert.False(t, isValidTable("campaign_reports:daily"))
}

func TestAppleAdsByteCap(t *testing.T) {
	wide := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := []map[string]interface{}{}
		for i := 0; i < 50; i++ {
			rows = append(rows, map[string]interface{}{"id": i, "name": wide})
		}
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Filters    []map[string]interface{} `json:"filters"`
			Pagination struct {
				Offset int `json:"offset"`
			} `json:"pagination"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Pagination.Offset > 0 || len(req.Filters) > 0 {
			rows = []map[string]interface{}{}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result":     rows,
			"pagination": map[string]interface{}{"offset": req.Pagination.Offset, "pageSize": len(rows)},
		})
	}))
	defer srv.Close()

	run := func(max int64) (int64, int64) {
		s := &AppleAdsSource{
			orgIDs:      []string{"org1"},
			accessToken: "test-token",
			tokenExpiry: time.Now().Add(time.Hour),
			client:      httpclient.New(httpclient.WithBaseURL(srv.URL)),
		}
		results, err := s.read(context.Background(), "campaigns", source.ReadOptions{MaxBatchBytes: max})
		if err != nil {
			t.Fatal(err)
		}
		var b, rw int64
		for res := range results {
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			b++
			rw += res.Batch.NumRows()
			res.Batch.Release()
		}
		return b, rw
	}

	offB, offR := run(0)
	onB, onR := run(4096)
	if offB != 1 {
		t.Fatalf("cap-off batches=%d want 1", offB)
	}
	if onB <= 1 {
		t.Fatalf("cap-on batches=%d want >1", onB)
	}
	if offR != onR || offR != 50 {
		t.Fatalf("row mismatch off=%d on=%d", offR, onR)
	}
}

type recordedCall struct {
	path    string
	context string
	body    map[string]interface{}
}

// fakePlatformAPI answers like the Platform API: lists end with an empty page
// and carry no total count; reports come back as result.rows.
func fakePlatformAPI(t *testing.T, handle func(path string, body map[string]interface{}) interface{}) (*httptest.Server, *[]recordedCall) {
	t.Helper()
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, recordedCall{path: r.URL.Path, context: r.Header.Get("X-AP-Context"), body: body})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(handle(r.URL.Path, body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testSource(srvURL string) *AppleAdsSource {
	return &AppleAdsSource{
		orgIDs:      []string{"20472490"},
		accessToken: "test-token",
		tokenExpiry: time.Now().Add(time.Hour),
		client:      httpclient.New(httpclient.WithBaseURL(srvURL)),
	}
}

func hasDeletedFilter(body map[string]interface{}) bool {
	filters, _ := body["filters"].([]interface{})
	for _, f := range filters {
		m := f.(map[string]interface{})
		if m["field"] == "deleted" && m["operator"] == "EQUALS" && m["value"] == true {
			return true
		}
	}
	return false
}

func offsetOf(body map[string]interface{}) int {
	p, _ := body["pagination"].(map[string]interface{})
	o, _ := p["offset"].(float64)
	return int(o)
}

func collect(t *testing.T, results <-chan source.RecordBatchResult) []map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	for res := range results {
		require.NoError(t, res.Err)
		for i := 0; i < int(res.Batch.NumRows()); i++ {
			row := map[string]interface{}{}
			for c := 0; c < int(res.Batch.NumCols()); c++ {
				row[res.Batch.ColumnName(c)] = strings.Trim(fmt.Sprint(res.Batch.Column(c).GetOneForMarshal(i)), `"`)
			}
			rows = append(rows, row)
		}
		res.Batch.Release()
	}
	return rows
}

func TestCampaignsIncludeDeletedAndUseAdAccountContext(t *testing.T) {
	srv, calls := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		require.Equal(t, "/campaigns/query", path)
		if offsetOf(body) > 0 {
			return map[string]interface{}{"result": []interface{}{}, "pagination": map[string]interface{}{"offset": offsetOf(body), "pageSize": 0}}
		}
		if hasDeletedFilter(body) {
			return map[string]interface{}{"result": []interface{}{map[string]interface{}{"id": 2, "deleted": true, "modificationTime": "2026-10-03T07:30:23.826"}}}
		}
		return map[string]interface{}{"result": []interface{}{map[string]interface{}{"id": 1, "deleted": false, "modificationTime": "2026-10-03T07:30:23.826"}}}
	})

	results, err := testSource(srv.URL).read(context.Background(), "campaigns", source.ReadOptions{})
	require.NoError(t, err)
	rows := collect(t, results)

	require.Len(t, rows, 2)
	assert.Equal(t, "20472490", rows[0]["orgId"])
	for _, c := range *calls {
		assert.Equal(t, "adAccountId=20472490", c.context)
	}
}

func TestEntityIncrementalWindowIsAppliedToResults(t *testing.T) {
	srv, _ := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		if offsetOf(body) > 0 || hasDeletedFilter(body) {
			return map[string]interface{}{"result": []interface{}{}}
		}
		return map[string]interface{}{"result": []interface{}{
			map[string]interface{}{"id": 1, "modificationTime": "2026-04-20T10:00:00.000"},
			map[string]interface{}{"id": 2, "modificationTime": "2026-04-21T10:00:00.000"},
			map[string]interface{}{"id": 3, "modificationTime": "2026-04-22T10:00:00.000"},
		}}
	})
	start := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)

	results, err := testSource(srv.URL).read(context.Background(), "campaigns", source.ReadOptions{
		IncrementalKey: "modificationTime", IntervalStart: &start, IntervalEnd: &end,
	})
	require.NoError(t, err)
	rows := collect(t, results)

	require.Len(t, rows, 1)
	assert.Equal(t, "2", rows[0]["id"])
}

func TestAdGroupsAreQueriedPerCampaign(t *testing.T) {
	srv, calls := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		if offsetOf(body) > 0 || hasDeletedFilter(body) {
			return map[string]interface{}{"result": []interface{}{}}
		}
		switch path {
		case "/campaigns/query":
			return map[string]interface{}{"result": []interface{}{map[string]interface{}{"id": 2144799001}}}
		case "/adgroups/query":
			return map[string]interface{}{"result": []interface{}{map[string]interface{}{"id": 7, "campaignId": 2144799001}}}
		}
		t.Fatalf("unexpected path %s", path)
		return nil
	})

	results, err := testSource(srv.URL).read(context.Background(), "ad_groups", source.ReadOptions{})
	require.NoError(t, err)
	rows := collect(t, results)

	require.Len(t, rows, 1)
	var filtered bool
	for _, c := range *calls {
		if c.path != "/adgroups/query" {
			continue
		}
		f := c.body["filters"].([]interface{})[0].(map[string]interface{})
		assert.Equal(t, "campaignId", f["field"])
		assert.Equal(t, float64(2144799001), f["value"])
		filtered = true
	}
	assert.True(t, filtered)
}

func TestCampaignReportMapsMetadataIDToCampaignID(t *testing.T) {
	srv, calls := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		require.Equal(t, "/reports/apps/campaigns/query", path)
		return map[string]interface{}{
			"result": map[string]interface{}{"rows": []interface{}{map[string]interface{}{
				"metadata": map[string]interface{}{"id": 2144799001, "name": "JP_Search"},
				"granularMetrics": []interface{}{
					map[string]interface{}{"date": "2026-10-03", "impressions": 100, "localSpend": map[string]interface{}{"amount": "7673", "currency": "JPY"}},
					map[string]interface{}{"date": "2026-10-04", "impressions": 200, "localSpend": map[string]interface{}{"amount": "0", "currency": "JPY"}},
				},
			}}},
			"pagination": map[string]interface{}{"offset": 0, "pageSize": 1, "totalCount": 1},
		}
	})
	start := time.Now().UTC().AddDate(0, 0, -10)
	end := time.Now().UTC().AddDate(0, 0, -8)

	results, err := testSource(srv.URL).readReport(context.Background(), "campaign_reports", source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}, reportTableConfig{baseTable: "campaign_reports", granularity: "DAILY"})
	require.NoError(t, err)
	rows := collect(t, results)

	require.Len(t, rows, 2)
	assert.Equal(t, "2144799001", rows[0]["campaignId"])
	assert.Equal(t, "20472490", rows[0]["orgId"])
	require.Len(t, *calls, 1)
	_, hasFilters := (*calls)[0].body["filters"]
	assert.False(t, hasFilters, "campaign reports must not send a campaignId filter")
	tr := (*calls)[0].body["timeRange"].(map[string]interface{})
	assert.Equal(t, "DAILY", tr["granularity"])
	assert.Equal(t, "UTC", tr["timeZone"])
}

func TestEmptyReportReadsAsNoRows(t *testing.T) {
	srv, _ := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		return map[string]interface{}{"result": map[string]interface{}{}}
	})
	results, err := testSource(srv.URL).readReport(context.Background(), "campaign_reports", source.ReadOptions{}, reportTableConfig{baseTable: "campaign_reports"})
	require.NoError(t, err)
	assert.Empty(t, collect(t, results))
}

func TestSingleDayReportOmitsGranularityAndReadsTotals(t *testing.T) {
	day := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	srv, calls := fakePlatformAPI(t, func(path string, body map[string]interface{}) interface{} {
		return map[string]interface{}{"result": map[string]interface{}{"rows": []interface{}{map[string]interface{}{
			"metadata":     map[string]interface{}{"id": 2144799001},
			"totalMetrics": map[string]interface{}{"impressions": 42},
		}}}}
	})

	results, err := testSource(srv.URL).readReport(context.Background(), "campaign_reports", source.ReadOptions{IntervalStart: &day, IntervalEnd: &day}, reportTableConfig{baseTable: "campaign_reports", granularity: "DAILY"})
	require.NoError(t, err)
	rows := collect(t, results)

	require.Len(t, rows, 1)
	assert.Equal(t, day.Format("2006-01-02"), rows[0]["date"])
	assert.Equal(t, "42", rows[0]["impressions"])
	require.Len(t, *calls, 1)
	_, hasGranularity := (*calls)[0].body["timeRange"].(map[string]interface{})["granularity"]
	assert.False(t, hasGranularity)
}

func TestReportChunksAreInclusiveAndDoNotOverlap(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	chunks := reportChunks(start, end, "HOURLY")
	require.Len(t, chunks, 3)
	assert.Equal(t, "2026-01-07", dateStr(chunks[0].end))
	assert.Equal(t, "2026-01-08", dateStr(chunks[1].start))
	assert.Equal(t, "2026-01-20", dateStr(chunks[2].end))
}

func TestGroupByDropsEmptyMetricsOption(t *testing.T) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	withGroup := reportRequestBody(reportTableConfig{groupBy: []string{"countryOrRegion"}}, dateRange{day, day.AddDate(0, 0, 5)}, nil, 0)
	_, hasOptions := withGroup["options"]
	assert.False(t, hasOptions)
	plain := reportRequestBody(reportTableConfig{}, dateRange{day, day.AddDate(0, 0, 5)}, nil, 0)
	assert.Equal(t, map[string]interface{}{"includeRows": []string{"EMPTY_METRICS"}}, plain["options"])
}
