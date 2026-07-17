package adapty

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAdaptyURI(t *testing.T) {
	tests := []struct {
		name          string
		uri           string
		wantKey       string
		wantLookback  int
		wantTimezone  string
		wantErrSubstr string
	}{
		{
			name:         "defaults",
			uri:          "adapty://?api_key=secret_live_test",
			wantKey:      "secret_live_test",
			wantLookback: 30,
			wantTimezone: "UTC",
		},
		{
			name:         "custom lookback and timezone",
			uri:          "adapty://?api_key=secret&lookback_days=0&timezone=Europe%2FIstanbul",
			wantKey:      "secret",
			wantLookback: 0,
			wantTimezone: "Europe/Istanbul",
		},
		{name: "wrong scheme", uri: "https://example.com", wantErrSubstr: "must start with adapty://"},
		{name: "missing slashes", uri: "adapty:?api_key=secret", wantErrSubstr: "must start with adapty://"},
		{name: "host is rejected", uri: "adapty://secret", wantErrSubstr: "credentials must be query parameters"},
		{name: "missing key", uri: "adapty://?lookback_days=5", wantErrSubstr: "api_key is required"},
		{name: "negative lookback", uri: "adapty://?api_key=x&lookback_days=-1", wantErrSubstr: "non-negative integer"},
		{name: "invalid lookback", uri: "adapty://?api_key=x&lookback_days=many", wantErrSubstr: "non-negative integer"},
		{name: "invalid timezone", uri: "adapty://?api_key=x&timezone=Moon%2FSea", wantErrSubstr: "invalid timezone"},
		{name: "unknown parameter", uri: "adapty://?api_key=x&foo=bar", wantErrSubstr: "unknown adapty URI parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseAdaptyURI(tt.uri)
			if tt.wantErrSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, creds.apiKey)
			assert.Equal(t, tt.wantLookback, creds.lookbackDays)
			assert.Equal(t, tt.wantTimezone, creds.timezone)
			assert.Equal(t, tt.wantTimezone, creds.location.String())
		})
	}
}

func TestSupportedTables(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), table)
	}
	for _, table := range []string{"", "cohort", "transactions", "profiles", "webhooks"} {
		assert.False(t, isValidTable(table), table)
	}
}

func TestParseTableSpec(t *testing.T) {
	tests := []struct {
		name          string
		table         string
		wantName      string
		assertParams  func(*testing.T, tableParams)
		wantErrSubstr string
	}{
		{
			name:     "analytics with filters",
			table:    "analytics?chart_id=revenue&store=app_store,play_store&country=us&segmentation=country",
			wantName: "analytics",
			assertParams: func(t *testing.T, params tableParams) {
				assert.Equal(t, "revenue", params.ChartID)
				assert.Equal(t, []string{"app_store", "play_store"}, params.Store)
				assert.Equal(t, []string{"us"}, params.Country)
				assert.Equal(t, "country", params.Segmentation)
			},
		},
		{
			name:     "cohort numeric parameters",
			table:    "cohorts?period_type=days&renewal_days=0,3,7&prediction_months=12",
			wantName: "cohorts",
			assertParams: func(t *testing.T, params tableParams) {
				assert.Equal(t, []int{0, 3, 7}, params.renewalDayValues)
				assert.Equal(t, 12, params.PredictionMonths)
			},
		},
		{
			name:     "conversion null starting state",
			table:    "conversion?from_period=null&to_period=paid",
			wantName: "conversion",
			assertParams: func(t *testing.T, params tableParams) {
				assert.True(t, params.fromPeriodSet)
				assert.Equal(t, "null", params.FromPeriod)
			},
		},
		{
			name:     "placement type",
			table:    "placements?placement_type=paywall",
			wantName: "placements",
			assertParams: func(t *testing.T, params tableParams) {
				assert.Equal(t, "paywall", params.PlacementType)
			},
		},
		{name: "paywalls", table: "paywalls", wantName: "paywalls"},
		{name: "analytics requires chart", table: "analytics", wantErrSubstr: "requires chart_id"},
		{name: "invalid chart", table: "analytics?chart_id=purchases", wantErrSubstr: "invalid chart_id"},
		{name: "conversion requires from", table: "conversion?to_period=paid", wantErrSubstr: "requires from_period"},
		{name: "conversion requires to", table: "conversion?from_period=trial", wantErrSubstr: "requires to_period"},
		{name: "placements requires type", table: "placements", wantErrSubstr: "requires placement_type"},
		{name: "invalid placement type", table: "placements?placement_type=flow", wantErrSubstr: "invalid placement_type"},
		{name: "unknown parameter", table: "retention?unknown=value", wantErrSubstr: "unknown table parameter"},
		{name: "parameter on paywalls", table: "paywalls?state=live", wantErrSubstr: "does not accept table parameters"},
		{name: "invalid comparison", table: "ltv?compare_date=2024-01-01", wantErrSubstr: "exactly two dates"},
		{name: "invalid renewal day", table: "cohorts?renewal_days=one", wantErrSubstr: "non-negative integers"},
		{name: "invalid prediction", table: "cohorts?prediction_months=5", wantErrSubstr: "invalid prediction_months"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, params, err := parseTableSpec(tt.table)
			if tt.wantErrSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
			if tt.assertParams != nil {
				tt.assertParams(t, params)
			}
		})
	}
}

func TestGetTableConfiguration(t *testing.T) {
	src := &AdaptySource{}
	tests := []struct {
		name           string
		request        string
		strategy       config.IncrementalStrategy
		primaryKeys    []string
		incrementalKey string
		partitionBy    string
	}{
		{name: "analytics", request: "analytics?chart_id=revenue", strategy: config.StrategyDeleteInsert, incrementalKey: "date", partitionBy: "date"},
		{name: "cohorts", request: "cohorts", strategy: config.StrategyDeleteInsert, incrementalKey: "date", partitionBy: "date"},
		{name: "placements", request: "placements?placement_type=paywall", strategy: config.StrategyReplace},
		{name: "paywalls", request: "paywalls", strategy: config.StrategyMerge, primaryKeys: []string{"paywall_id"}, incrementalKey: "updated_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, err := src.GetTable(context.Background(), source.TableRequest{Name: tt.request})
			require.NoError(t, err)
			assert.Equal(t, tt.strategy, table.Strategy())
			assert.Equal(t, tt.primaryKeys, table.PrimaryKeys())
			assert.Equal(t, tt.incrementalKey, table.IncrementalKey())
			assert.False(t, table.HasKnownSchema())
			if partitioned, ok := table.(source.PartitionedTable); ok {
				assert.Equal(t, tt.partitionBy, partitioned.PartitionBy())
			}
		})
	}
}

func TestBuildMetricRequest(t *testing.T) {
	params := tableParams{
		ChartID:             "refund_money",
		DateType:            "purchase_date",
		Segmentation:        "country",
		Store:               []string{"app_store"},
		Country:             []string{"us", "gb"},
		AttributionCampaign: []string{"summer"},
	}
	body := buildMetricRequest("analytics", params, "2025-06-01")

	assert.Equal(t, "refund_money", body["chart_id"])
	assert.Equal(t, "day", body["period_unit"])
	assert.Equal(t, "json", body["format"])
	assert.Equal(t, "purchase_date", body["date_type"])
	filters := body["filters"].(map[string]any)
	assert.Equal(t, []string{"2025-06-01", "2025-06-01"}, filters["date"])
	assert.Equal(t, []string{"app_store"}, filters["store"])
	assert.Equal(t, []string{"us", "gb"}, filters["country"])
	assert.Equal(t, []string{"summer"}, filters["attribution_campaign"])
}

func TestMetricRowsPreserveNestedData(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"revenue": map[string]any{
				"value": json.Number("12.34"),
				"data":  []any{map[string]any{"value": json.Number("12.34")}},
			},
		},
	}
	rows, err := metricRows("analytics", tableParams{ChartID: "revenue"}, "2025-01-02", payload)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "2025-01-02", rows[0]["date"])
	assert.Equal(t, "revenue", rows[0]["metric"])
	assert.Equal(t, "revenue", rows[0]["chart_id"])
	assert.IsType(t, []any{}, rows[0]["data"])
	assert.IsType(t, json.Number(""), rows[0]["value"])

	ltvRows, err := metricRows("ltv", tableParams{}, "2025-01-02", map[string]any{
		"revenue":     map[string]any{"data": []any{}},
		"proceeds":    map[string]any{"data": []any{}},
		"net_revenue": map[string]any{"data": []any{}},
	})
	require.NoError(t, err)
	assert.Len(t, ltvRows, 3)
	assert.Equal(t, "net_revenue", ltvRows[2]["accounting_type"])

	for _, tt := range []struct {
		table   string
		payload map[string]any
	}{
		{table: "analytics", payload: map[string]any{"data": nil}},
		{table: "cohorts", payload: map[string]any{}},
		{table: "funnel", payload: map[string]any{"data": nil}},
	} {
		rows, err := metricRows(tt.table, tableParams{}, "2025-01-02", tt.payload)
		require.NoError(t, err)
		assert.Empty(t, rows)
	}

	_, err = metricRows("analytics", tableParams{}, "2025-01-02", map[string]any{"data": "invalid"})
	assert.ErrorContains(t, err, "data is not an object")
}

func TestDecodeJSONUseNumber(t *testing.T) {
	var payload map[string]any
	require.NoError(t, decodeJSONUseNumber([]byte(`{"integer":9007199254740993,"decimal":12.34}`), &payload))
	assert.Equal(t, json.Number("9007199254740993"), payload["integer"])
	assert.Equal(t, json.Number("12.34"), payload["decimal"])
	assert.Error(t, decodeJSONUseNumber([]byte(`{} {}`), &payload))
}

func TestResolveDateRangePreservesExplicitInterval(t *testing.T) {
	start := time.Date(2025, 1, 10, 16, 30, 0, 0, time.FixedZone("offset", 2*60*60))
	end := time.Date(2025, 1, 12, 23, 0, 0, 0, time.UTC)
	originalStart := start
	src := &AdaptySource{lookbackDays: 3, location: time.UTC}
	opts := source.ReadOptions{IntervalStart: &start, IntervalEnd: &end}

	gotStart, gotEnd, err := src.resolveDateRange(&opts)
	require.NoError(t, err)
	assert.Equal(t, "2025-01-10", gotStart.Format(adaptyDateLayout))
	assert.Equal(t, "2025-01-12", gotEnd.Format(adaptyDateLayout))
	assert.Equal(t, originalStart, *opts.IntervalStart)
}

func TestReadAnalyticsHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/api/v1/client-api/metrics/analytics/", request.URL.Path)
		assert.Equal(t, "Api-Key test-secret", request.Header.Get("Authorization"))
		assert.Equal(t, "UTC", request.Header.Get("Adapty-Tz"))

		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		assert.NoError(t, decoder.Decode(&body))
		assert.Equal(t, "revenue", body["chart_id"])
		filters := body["filters"].(map[string]any)
		assert.Equal(t, []any{"2025-03-04", "2025-03-04"}, filters["date"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"revenue":{"value":12.5,"data":[{"value":12.5}]},"proceeds":{"value":8.0,"data":[]}}}`))
	}))
	defer server.Close()

	client := testAdaptyClient(server.URL)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	src := &AdaptySource{analyticsClient: client, lookbackDays: 0, location: time.UTC}
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "analytics?chart_id=revenue"})
	require.NoError(t, err)
	date := time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &date, IntervalEnd: &date, Limit: 1})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	defer batches[0].Release()
	require.EqualValues(t, 2, batches[0].NumRows())
	assert.Equal(t, arrow.FixedWidthTypes.Date32, batches[0].Schema().Field(0).Type)
	dateColumn := batches[0].Column(batches[0].Schema().FieldIndices("date")[0]).(*array.Date32)
	assert.Equal(t, arrow.Date32FromTime(date), dateColumn.Value(0))
	metricColumn := batches[0].Column(batches[0].Schema().FieldIndices("metric")[0]).(*array.String)
	assert.Equal(t, "proceeds", metricColumn.Value(0))
	assert.Equal(t, "revenue", metricColumn.Value(1))
}

func TestReadPaywallsPaginatesAndFiltersUpdatedAt(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "/api/v2/server-side-api/paywalls/", request.URL.Path)
		assert.Equal(t, "2", request.URL.Query().Get("page[size]"))
		assert.Equal(t, "Api-Key test-secret", request.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		switch request.URL.Query().Get("page[number]") {
		case "1":
			_, _ = w.Write([]byte(`{"data":[{"paywall_id":"old","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","products":[]},{"paywall_id":"included","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-03T00:00:00Z","products":[]}]}`))
		case "2":
			_, _ = w.Write([]byte(`{"data":[{"paywall_id":"new","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-05T00:00:00Z","products":[]}],"meta":{"pagination":{"count":3,"page":2,"pages":2}}}`))
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := testAdaptyClient(server.URL)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	src := &AdaptySource{serverClient: client}
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "paywalls"})
	require.NoError(t, err)
	start := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end, PageSize: 2})
	require.NoError(t, err)

	batches := collectBatches(t, results)
	require.Len(t, batches, 1)
	defer batches[0].Release()
	require.EqualValues(t, 1, batches[0].NumRows())
	idColumn := batches[0].Column(batches[0].Schema().FieldIndices("paywall_id")[0]).(*array.String)
	assert.Equal(t, "included", idColumn.Value(0))
	assert.EqualValues(t, 2, requests.Load())
	assert.Equal(t, schema.DataTypeToArrowType(schema.Column{DataType: schema.TypeTimestampTZ}), batches[0].Schema().Field(batches[0].Schema().FieldIndices("updated_at")[0]).Type)
}

func testAdaptyClient(baseURL string) *ingestrhttp.Client {
	return ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithDisableRetry(),
		ingestrhttp.WithAuth(ingestrhttp.NewAPIKeyAuth("Authorization", "Api-Key test-secret", true)),
		ingestrhttp.WithHeaders(map[string]string{"Content-Type": "application/json", "Adapty-Tz": "UTC"}),
	)
}

func collectBatches(t *testing.T, results <-chan source.RecordBatchResult) []arrow.RecordBatch {
	t.Helper()
	var batches []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			batches = append(batches, result.Batch)
		}
	}
	return batches
}
