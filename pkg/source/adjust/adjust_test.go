package adjust

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func timePtr(t time.Time) *time.Time { return &t }

func TestParseAdjustURI(t *testing.T) {
	tests := []struct {
		name             string
		uri              string
		wantKey          string
		wantLookBackDays string
		wantErr          bool
	}{
		{
			name:    "valid URI with api_key only",
			uri:     "adjust://?api_key=test-key-123",
			wantKey: "test-key-123",
		},
		{
			name:             "valid URI with api_key and lookback_days",
			uri:              "adjust://?api_key=my-key&lookback_days=60",
			wantKey:          "my-key",
			wantLookBackDays: "60",
		},
		{
			name:    "missing scheme",
			uri:     "http://?api_key=test-key",
			wantErr: true,
		},
		{
			name:    "no query params",
			uri:     "adjust://",
			wantErr: true,
		},
		{
			name:    "missing api_key",
			uri:     "adjust://?lookback_days=30",
			wantErr: true,
		},
		{
			name:    "empty api_key",
			uri:     "adjust://?api_key=",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseAdjustURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, creds.apiKey)
			assert.Equal(t, tt.wantLookBackDays, creds.lookBackDays)
		})
	}
}

func TestParseTableSpec(t *testing.T) {
	tests := []struct {
		name            string
		table           string
		wantBase        string
		wantAppTokens   string
		wantAttribution string
		wantErr         bool
	}{
		{name: "no app_token", table: "campaigns", wantBase: "campaigns"},
		{name: "single token", table: "campaigns:abc123", wantBase: "campaigns", wantAppTokens: "abc123"},
		{name: "multiple tokens", table: "creatives:abc123,def456", wantBase: "creatives", wantAppTokens: "abc123,def456"},
		{name: "events with token", table: "events:tok1", wantBase: "events", wantAppTokens: "tok1"},
		{name: "colon form is app token only", table: "creatives:click", wantBase: "creatives", wantAppTokens: "click"},
		{name: "query attribution", table: "creatives?attribution_types=click,engaged_ad", wantBase: "creatives", wantAttribution: "click,engaged_ad"},
		{name: "query token and attribution", table: "creatives?app_token=abc123&attribution_types=click", wantBase: "creatives", wantAppTokens: "abc123", wantAttribution: "click"},
		{name: "query repeated keys", table: "campaigns?app_token=abc&app_token=def&attribution_types=click&attribution_types=impression", wantBase: "campaigns", wantAppTokens: "abc,def", wantAttribution: "click,impression"},
		{name: "query unknown key", table: "campaigns?foo=bar", wantErr: true},
		{name: "custom table untouched", table: "custom:day:installs", wantBase: "custom:day:installs"},
		{name: "custom with filters untouched", table: "custom:day:installs:app_token__in=abc", wantBase: "custom:day:installs:app_token__in=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, tokens, attribution, err := parseTableSpec(tt.table)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBase, base)
			assert.Equal(t, tt.wantAppTokens, tokens)
			assert.Equal(t, tt.wantAttribution, attribution)
		})
	}
}

func TestParseCustomTable(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantDims   string
		wantMets   string
		wantFilter map[string]string
		wantErr    bool
	}{
		{
			name:     "valid with day dimension",
			table:    "custom:day,campaign:installs,clicks",
			wantDims: "day,campaign",
			wantMets: "installs,clicks",
		},
		{
			name:       "valid with filters",
			table:      "custom:day,campaign:installs:app_token=abc123",
			wantDims:   "day,campaign",
			wantMets:   "installs",
			wantFilter: map[string]string{"app_token": "abc123"},
		},
		{
			name:     "valid with hour dimension",
			table:    "custom:hour:installs",
			wantDims: "hour",
			wantMets: "installs",
		},
		{
			name:     "valid with week dimension",
			table:    "custom:week,country:clicks",
			wantDims: "week,country",
			wantMets: "clicks",
		},
		{
			name:     "valid with month dimension",
			table:    "custom:month:cost",
			wantDims: "month",
			wantMets: "cost",
		},
		{
			name:     "valid with quarter dimension",
			table:    "custom:quarter:installs",
			wantDims: "quarter",
			wantMets: "installs",
		},
		{
			name:     "valid with year dimension",
			table:    "custom:year:installs",
			wantDims: "year",
			wantMets: "installs",
		},
		{
			name:    "missing required time dimension",
			table:   "custom:campaign,country:installs",
			wantErr: true,
		},
		{
			name:    "empty dimensions",
			table:   "custom::installs",
			wantErr: true,
		},
		{
			name:    "empty metrics",
			table:   "custom:day:",
			wantErr: true,
		},
		{
			name:    "invalid format - only one part",
			table:   "custom:day",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dims, mets, filters, err := parseCustomTable(tt.table)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDims, dims)
			assert.Equal(t, tt.wantMets, mets)
			if tt.wantFilter != nil {
				assert.Equal(t, tt.wantFilter, filters)
			}
		})
	}
}

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		expect map[string]string
	}{
		{
			name:   "single key-value",
			raw:    "app_token=abc123",
			expect: map[string]string{"app_token": "abc123"},
		},
		{
			name:   "multiple keys",
			raw:    "app_token=abc123,country=us",
			expect: map[string]string{"app_token": "abc123", "country": "us"},
		},
		{
			name:   "multi-value key",
			raw:    "country=us,gb,de",
			expect: map[string]string{"country": "us,gb,de"},
		},
		{
			name:   "mixed single and multi-value",
			raw:    "app_token=abc,country=us,gb,network=facebook",
			expect: map[string]string{"app_token": "abc", "country": "us,gb", "network": "facebook"},
		},
		{
			name:   "empty string",
			raw:    "",
			expect: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFilters(tt.raw)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestBuildDatePeriod(t *testing.T) {
	tests := []struct {
		name              string
		lookBackDays      string
		intervalStart     *time.Time
		intervalEnd       *time.Time
		wantContains      []string
		wantErr           bool
		wantExpandedStart *time.Time
	}{
		{
			name:              "default lookback of 30 days",
			lookBackDays:      "",
			intervalStart:     timePtr(time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)),
			intervalEnd:       timePtr(time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)),
			wantContains:      []string{"2025-01-01", "2025-02-15"},
			wantExpandedStart: timePtr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:              "custom lookback of 60 days",
			lookBackDays:      "60",
			intervalStart:     timePtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
			intervalEnd:       timePtr(time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)),
			wantContains:      []string{"2024-12-31", "2025-03-15"},
			wantExpandedStart: timePtr(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:              "lookback with time.Time interval",
			lookBackDays:      "10",
			intervalStart:     timePtr(time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)),
			intervalEnd:       timePtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
			wantContains:      []string{"2025-01-10", "2025-02-01"},
			wantExpandedStart: timePtr(time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:         "no interval defaults to now minus lookback_days",
			lookBackDays: "30",
			wantContains: []string{":"},
		},
		{
			name:          "start after end returns error",
			lookBackDays:  "0",
			intervalStart: timePtr(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
			intervalEnd:   timePtr(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AdjustSource{lookBackDays: tt.lookBackDays}
			opts := &source.ReadOptions{
				IntervalStart: tt.intervalStart,
				IntervalEnd:   tt.intervalEnd,
			}
			result, err := s.buildDatePeriod(opts)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			for _, want := range tt.wantContains {
				assert.Contains(t, result, want)
			}

			if tt.wantExpandedStart != nil {
				assert.Equal(t, *tt.wantExpandedStart, *opts.IntervalStart, "IntervalStart should be expanded by lookback days")
			}
		})
	}
}

func TestBuildTypeHintColumns(t *testing.T) {
	tests := []struct {
		name       string
		dimensions string
		metrics    string
		wantCols   []schema.Column
	}{
		{
			name:       "known dimensions and metrics",
			dimensions: "day,campaign",
			metrics:    "installs,cost",
			wantCols: []schema.Column{
				{Name: "day", DataType: schema.TypeDate},
				{Name: "campaign", DataType: schema.TypeString},
				{Name: "installs", DataType: schema.TypeInt64},
				{Name: "cost", DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
			},
		},
		{
			name:       "hour dimension",
			dimensions: "hour",
			metrics:    "clicks",
			wantCols: []schema.Column{
				{Name: "hour", DataType: schema.TypeTimestampTZ},
				{Name: "clicks", DataType: schema.TypeInt64},
			},
		},
		{
			name:       "unknown fields are skipped",
			dimensions: "day,unknown_dim",
			metrics:    "installs,unknown_metric",
			wantCols: []schema.Column{
				{Name: "day", DataType: schema.TypeDate},
				{Name: "installs", DataType: schema.TypeInt64},
			},
		},
		{
			name:       "all unknown returns empty",
			dimensions: "foo",
			metrics:    "bar",
			wantCols:   nil,
		},
		{
			name:       "default campaign dimensions are typed",
			dimensions: "app,app_token,store_type,channel,country",
			metrics:    "",
			wantCols: []schema.Column{
				{Name: "app", DataType: schema.TypeString},
				{Name: "app_token", DataType: schema.TypeString},
				{Name: "store_type", DataType: schema.TypeString},
				{Name: "channel", DataType: schema.TypeString},
				{Name: "country", DataType: schema.TypeString},
			},
		},
		{
			name:       "revenue cohort metrics are decimal by prefix",
			dimensions: "",
			metrics:    "all_revenue_total_d0,ad_revenue_total_d21,revenue_total_d90",
			wantCols: []schema.Column{
				{Name: "all_revenue_total_d0", DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
				{Name: "ad_revenue_total_d21", DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
				{Name: "revenue_total_d90", DataType: schema.TypeDecimal, Precision: 38, Scale: 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := buildTypeHintColumns(tt.dimensions, tt.metrics)
			assert.Equal(t, tt.wantCols, cols)
		})
	}
}

func TestBuildDefaultMetrics(t *testing.T) {
	metrics := buildDefaultMetrics()

	assert.Equal(t, []string{"installs", "network_cost"}, metrics[:2])

	// Every cohort day must have all three revenue variants (D21 used to be
	// asymmetric — only all_revenue_total_d21 was present).
	for _, day := range revenueCohortDays {
		for _, prefix := range revenueMetricPrefixes {
			assert.Contains(t, metrics, prefix+strconv.Itoa(day))
		}
	}

	assert.Contains(t, metrics, "revenue_total_d120")
	assert.Contains(t, metrics, "ad_revenue_total_d120")
	assert.Contains(t, metrics, "all_revenue_total_d120")

	// Every metric must resolve to a type hint so no column falls back to
	// schema inference.
	for _, m := range metrics {
		_, ok := lookupTypeHint(m)
		assert.Truef(t, ok, "metric %q has no type hint", m)
	}
}

func TestIsValidTable(t *testing.T) {
	tests := []struct {
		name  string
		table string
		valid bool
	}{
		{"events", "events", true},
		{"campaigns", "campaigns", true},
		{"creatives", "creatives", true},
		{"custom prefix", "custom:day:installs", true},
		{"custom bare", "custom", false},
		{"unsupported", "unknown_table", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidTable(tt.table))
		})
	}
}

func TestGetTable_CustomMergeKeyPriority(t *testing.T) {
	s := NewAdjustSource()

	tests := []struct {
		name         string
		table        string
		wantMergeKey string
		wantPKs      []string
	}{
		{
			name:         "hour has highest priority",
			table:        "custom:year,hour,day:installs",
			wantMergeKey: "hour",
			wantPKs:      []string{"year", "hour", "day"},
		},
		{
			name:         "day is second priority",
			table:        "custom:year,day,campaign:installs",
			wantMergeKey: "day",
			wantPKs:      []string{"year", "day", "campaign"},
		},
		{
			name:         "week is third priority",
			table:        "custom:week,campaign:installs",
			wantMergeKey: "week",
			wantPKs:      []string{"week", "campaign"},
		},
		{
			name:         "single required dimension",
			table:        "custom:month:clicks",
			wantMergeKey: "month",
			wantPKs:      []string{"month"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			require.NoError(t, err)

			dst := table.(*source.DynamicSourceTable)
			assert.Equal(t, tt.wantMergeKey, dst.TableIncrementalKey)
			assert.Equal(t, tt.wantPKs, dst.TablePrimaryKeys)
			assert.Equal(t, config.StrategyDeleteInsert, dst.TableStrategy)
		})
	}
}

func TestGetTable_Strategies(t *testing.T) {
	s := NewAdjustSource()

	tests := []struct {
		name     string
		table    string
		strategy config.IncrementalStrategy
		wantPKs  []string
		wantKey  string
	}{
		{"events is replace", "events", config.StrategyReplace, []string{"id"}, ""},
		{"campaigns is merge", "campaigns", config.StrategyMerge, defaultPrimaryKeys, "day"},
		{"creatives is merge", "creatives", config.StrategyMerge, creativePrimaryKeys, "day"},
		{"custom is delete-insert", "custom:day:installs", config.StrategyDeleteInsert, []string{"day"}, "day"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			require.NoError(t, err)
			dst := table.(*source.DynamicSourceTable)
			assert.Equal(t, tt.strategy, dst.TableStrategy)
			assert.Equal(t, tt.wantPKs, dst.TablePrimaryKeys)
			assert.Equal(t, tt.wantKey, dst.TableIncrementalKey)
		})
	}
}

func TestGetTable_AttributionTypesGuard(t *testing.T) {
	s := NewAdjustSource()

	tests := []struct {
		name    string
		table   string
		wantErr bool
	}{
		{"campaigns accepts attribution_types", "campaigns?attribution_types=click", false},
		{"creatives accepts attribution_types", "creatives?attribution_types=click,engaged_ad", false},
		{"events rejects attribution_types", "events?attribution_types=click", true},
		{"events still accepts app_token", "events?app_token=abc123", false},
		{"unknown attribution_types value is left to Adjust", "campaigns?attribution_types=impresion", false},
		{"custom accepts attribution_types via filters", "custom:day,campaign:installs:attribution_types=click,engaged_ad", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
