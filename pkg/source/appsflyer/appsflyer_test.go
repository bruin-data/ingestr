package appsflyer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAppsflyerURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError bool
	}{
		{
			name:    "valid URI",
			uri:     "appsflyer://?api_key=test_key_123",
			wantKey: "test_key_123",
		},
		{
			name:    "valid URI with extra params",
			uri:     "appsflyer://?api_key=my_key&other=value",
			wantKey: "my_key",
		},
		{
			name:      "missing api_key",
			uri:       "appsflyer://?other=value",
			wantError: true,
		},
		{
			name:      "empty api_key",
			uri:       "appsflyer://?api_key=",
			wantError: true,
		},
		{
			name:      "empty URI",
			uri:       "appsflyer://",
			wantError: true,
		},
		{
			name:      "invalid scheme",
			uri:       "postgres://localhost",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseAppsflyerURI(tt.uri)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKey, key)
			}
		})
	}
}

func TestExcludeMetricsForDateRange(t *testing.T) {
	metrics := []string{
		"cohort_day_1_revenue_per_user",
		"cohort_day_1_total_revenue_per_user",
		"cohort_day_3_revenue_per_user",
		"cohort_day_3_total_revenue_per_user",
	}

	t.Run("all excluded when end date is recent", func(t *testing.T) {
		toDate := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		excluded := excludeMetricsForDateRange(metrics, toDate)
		assert.Equal(t, metrics, excluded)
	})

	t.Run("none excluded when end date is old enough", func(t *testing.T) {
		toDate := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
		excluded := excludeMetricsForDateRange(metrics, toDate)
		assert.Empty(t, excluded)
	})

	t.Run("non-cohort metrics are never excluded", func(t *testing.T) {
		nonCohort := []string{"clicks", "impressions", "cost"}
		toDate := time.Now().UTC().Format("2006-01-02")
		excluded := excludeMetricsForDateRange(nonCohort, toDate)
		assert.Empty(t, excluded)
	})

	t.Run("invalid date returns nil", func(t *testing.T) {
		excluded := excludeMetricsForDateRange(metrics, "not-a-date")
		assert.Nil(t, excluded)
	})
}

func TestStandardizeKeys(t *testing.T) {
	t.Run("lowercases and replaces spaces with underscores", func(t *testing.T) {
		data := []map[string]any{
			{"Key One": 100, "Key Two": 1000},
			{"Key One": 200, "Key Two": 2000, "cohort_day_1_revenue_per_user": 200},
		}
		excludedMetrics := []string{"Key Three"}

		standardized := standardizeKeys(data, excludedMetrics)

		assert.Equal(t, []map[string]any{
			{"key_one": 100, "key_two": 1000, "key_three": nil},
			{"key_one": 200, "key_two": 2000, "key_three": nil, "cohort_day_1_revenue_per_user": 200},
		}, standardized)
	})

	t.Run("excluded metric not overwritten if present", func(t *testing.T) {
		data := []map[string]any{
			{"cost": 5.0, "revenue": 10.0},
		}
		standardized := standardizeKeys(data, []string{"cost"})
		assert.Equal(t, 5.0, standardized[0]["cost"])
	})

	t.Run("empty data", func(t *testing.T) {
		standardized := standardizeKeys(nil, []string{"cost"})
		assert.Empty(t, standardized)
	})
}

func TestFixKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Key One", "key_one"},
		{"UPPERCASE", "uppercase"},
		{"double  space", "double_space"},
		{"already_snake", "already_snake"},
		{"With-Hyphen", "withhyphen"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, fixKey(tt.input))
		})
	}
}

func TestBuildPrimaryKeys(t *testing.T) {
	t.Run("maps dimensions through dimensionResponseMapping", func(t *testing.T) {
		dims := []string{"c", "geo", "app_id", "install_time"}
		keys := buildPrimaryKeys(dims)
		assert.Equal(t, []string{"campaign", "geo", "app_id", "install_time"}, keys)
	})

	t.Run("maps creative dimensions", func(t *testing.T) {
		dims := []string{"c", "geo", "app_id", "install_time", "af_adset_id", "af_adset", "af_ad_id"}
		keys := buildPrimaryKeys(dims)
		assert.Equal(t, []string{"campaign", "geo", "app_id", "install_time", "adset_id", "adset", "ad_id"}, keys)
	})
}

func TestBuildSchemaColumns(t *testing.T) {
	t.Run("maps dimensions and metrics to schema columns", func(t *testing.T) {
		cols := buildSchemaColumns([]string{"c", "app_id"}, []string{"clicks", "cost"})

		assert.Len(t, cols, 4)
		assert.Equal(t, "campaign", cols[0].Name)
		assert.Equal(t, schema.TypeString, cols[0].DataType)
		assert.Equal(t, "app_id", cols[1].Name)
		assert.Equal(t, "clicks", cols[2].Name)
		assert.Equal(t, schema.TypeInt64, cols[2].DataType)
		assert.Equal(t, "cost", cols[3].Name)
		assert.Equal(t, schema.TypeDecimal, cols[3].DataType)
		assert.Equal(t, 30, cols[3].Precision)
		assert.Equal(t, 5, cols[3].Scale)
	})

	t.Run("deduplicates columns", func(t *testing.T) {
		cols := buildSchemaColumns([]string{"c"}, []string{"campaign"})
		assert.Len(t, cols, 1)
		assert.Equal(t, "campaign", cols[0].Name)
	})

	t.Run("unknown columns get TypeUnknown", func(t *testing.T) {
		cols := buildSchemaColumns([]string{"unknown_dim"}, nil)
		assert.Len(t, cols, 1)
		assert.Equal(t, schema.TypeUnknown, cols[0].DataType)
	})
}

func TestGetTable(t *testing.T) {
	s := NewAppsflyerSource()

	t.Run("campaigns table", func(t *testing.T) {
		table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
		require.NoError(t, err)
		assert.Equal(t, "campaigns", table.Name())
		assert.Equal(t, []string{"campaign", "geo", "app_id", "install_time"}, table.PrimaryKeys())
		assert.Equal(t, "install_time", table.IncrementalKey())
	})

	t.Run("creatives table", func(t *testing.T) {
		table, err := s.GetTable(context.Background(), source.TableRequest{Name: "creatives"})
		require.NoError(t, err)
		assert.Equal(t, "creatives", table.Name())
		assert.Equal(t, []string{"campaign", "geo", "app_id", "install_time", "adset_id", "adset", "ad_id"}, table.PrimaryKeys())
	})

	t.Run("unsupported table", func(t *testing.T) {
		_, err := s.GetTable(context.Background(), source.TableRequest{Name: "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported table")
	})

	t.Run("custom table", func(t *testing.T) {
		table, err := s.GetTable(context.Background(), source.TableRequest{Name: "custom:c,geo:clicks,impressions"})
		require.NoError(t, err)
		assert.Equal(t, "custom", table.Name())
		assert.Contains(t, table.PrimaryKeys(), "campaign")
		assert.Contains(t, table.PrimaryKeys(), "geo")
		assert.Contains(t, table.PrimaryKeys(), "install_time")
		assert.Equal(t, "install_time", table.IncrementalKey())
	})

	t.Run("custom table auto-adds install_time", func(t *testing.T) {
		table, err := s.GetTable(context.Background(), source.TableRequest{Name: "custom:c:clicks"})
		require.NoError(t, err)
		assert.Contains(t, table.PrimaryKeys(), "install_time")
	})

	t.Run("custom table invalid format", func(t *testing.T) {
		_, err := s.GetTable(context.Background(), source.TableRequest{Name: "custom:onlyonefield"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid custom table format")
	})
}

func TestReadWithMockServer(t *testing.T) {
	mockData := []map[string]any{
		{
			"Campaign":     "summer_sale",
			"Geo":          "US",
			"App ID":       "com.example.app",
			"Install Time": "2024-06-01",
			"Clicks":       150,
			"Impressions":  5000,
			"Cost":         25.50,
		},
		{
			"Campaign":     "winter_promo",
			"Geo":          "UK",
			"App ID":       "com.example.app",
			"Install Time": "2024-06-02",
			"Clicks":       200,
			"Impressions":  8000,
			"Cost":         40.00,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/master-agg-data/v4/app/all", r.URL.Path)
		assert.Equal(t, "json", r.URL.Query().Get("format"))
		assert.NotEmpty(t, r.URL.Query().Get("from"))
		assert.NotEmpty(t, r.URL.Query().Get("to"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockData)
	}))
	defer server.Close()

	s := &AppsflyerSource{
		apiKey: "test_key",
		client: httpclient.New(
			httpclient.WithBaseURL(server.URL),
			httpclient.WithTimeout(10*time.Second),
		),
	}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
	require.NoError(t, err)

	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	ch, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batches int
	var totalRows int64
	for result := range ch {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		batches++
		totalRows += result.Batch.NumRows()
	}

	assert.Equal(t, 1, batches)
	assert.Equal(t, int64(2), totalRows)
}

func TestReadWithMockServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	s := &AppsflyerSource{
		apiKey: "test_key",
		client: httpclient.New(
			httpclient.WithBaseURL(server.URL),
			httpclient.WithTimeout(10*time.Second),
		),
	}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
	require.NoError(t, err)

	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	ch, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	result := <-ch
	assert.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "500")
}

func TestReadWithMockServerRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Rate limit exceeded"))
	}))
	defer server.Close()

	s := &AppsflyerSource{
		apiKey: "test_key",
		client: httpclient.New(
			httpclient.WithBaseURL(server.URL),
			httpclient.WithTimeout(10*time.Second),
		),
	}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
	require.NoError(t, err)

	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	ch, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	result := <-ch
	assert.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "rate limit")
}
