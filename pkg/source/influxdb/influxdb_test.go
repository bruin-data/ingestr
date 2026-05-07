package influxdb

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInfluxURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantHost   string
		wantToken  string
		wantOrg    string
		wantBucket string
		wantSecure string
		wantUseV3  bool
		wantErr    bool
	}{
		{
			name:       "valid URI with all params",
			uri:        "influxdb://myhost.com:8086?token=mytoken&org=myorg&bucket=mybucket",
			wantHost:   "https://myhost.com:8086",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "true",
			wantUseV3:  false,
		},
		{
			name:       "cloud URL without port omits port",
			uri:        "influxdb://eu-central-1-0.aws.cloud3.influxdata.com?token=mytoken&org=myorg&bucket=mybucket",
			wantHost:   "https://eu-central-1-0.aws.cloud3.influxdata.com",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "true",
			wantUseV3:  false,
		},
		{
			name:       "secure=false uses http with default port 8086",
			uri:        "influxdb://myhost.com?token=mytoken&org=myorg&bucket=mybucket&secure=false",
			wantHost:   "http://myhost.com:8086",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "false",
			wantUseV3:  false,
		},
		{
			name:       "secure=false with explicit port",
			uri:        "influxdb://myhost.com:9999?token=mytoken&org=myorg&bucket=mybucket&secure=false",
			wantHost:   "http://myhost.com:9999",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "false",
			wantUseV3:  false,
		},
		{
			name:       "influxdb3=true enables v3 client",
			uri:        "influxdb://myhost.com?token=mytoken&org=myorg&bucket=mybucket&influxdb3=true",
			wantHost:   "https://myhost.com",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "true",
			wantUseV3:  true,
		},
		{
			name:       "influxdb3 not set defaults to v2",
			uri:        "influxdb://myhost.com?token=mytoken&org=myorg&bucket=mybucket",
			wantHost:   "https://myhost.com",
			wantToken:  "mytoken",
			wantOrg:    "myorg",
			wantBucket: "mybucket",
			wantSecure: "true",
			wantUseV3:  false,
		},
		{
			name:    "missing token",
			uri:     "influxdb://myhost.com?org=myorg&bucket=mybucket",
			wantErr: true,
		},
		{
			name:    "missing org",
			uri:     "influxdb://myhost.com?token=mytoken&bucket=mybucket",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			uri:     "influxdb://myhost.com?token=mytoken&org=myorg",
			wantErr: true,
		},
		{
			name:    "missing host",
			uri:     "influxdb://?token=mytoken&org=myorg&bucket=mybucket",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://myhost.com?token=mytoken&org=myorg&bucket=mybucket",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostURL, token, org, bucket, secure, useV3, err := parseInfluxURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, hostURL)
			assert.Equal(t, tt.wantToken, token)
			assert.Equal(t, tt.wantOrg, org)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantSecure, secure)
			assert.Equal(t, tt.wantUseV3, useV3)
		})
	}
}

func TestBuildSQLQuery(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("basic query", func(t *testing.T) {
		query := buildSQLQuery("cpu_usage", start, end, 0)
		assert.Equal(t, `SELECT * FROM "cpu_usage" WHERE time >= '2024-01-01T00:00:00Z' AND time <= '2024-06-01T00:00:00Z' ORDER BY time ASC`, query)
	})

	t.Run("with limit", func(t *testing.T) {
		query := buildSQLQuery("cpu_usage", start, end, 100)
		assert.Contains(t, query, "LIMIT 100")
	})

	t.Run("escapes double quotes in measurement name", func(t *testing.T) {
		query := buildSQLQuery(`my"table`, start, end, 0)
		assert.Contains(t, query, `"my\"table"`)
	})
}

func TestBuildFluxQuery(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("basic query", func(t *testing.T) {
		query := buildFluxQuery("my-bucket", "cpu", start, end, 0)
		assert.Contains(t, query, `from(bucket: "my-bucket")`)
		assert.Contains(t, query, `range(start: 2024-01-01T00:00:00Z`)
		assert.Contains(t, query, `r._measurement == "cpu"`)
	})

	t.Run("stop is exclusive so adds 1 second", func(t *testing.T) {
		query := buildFluxQuery("b", "m", start, end, 0)
		// Flux stop should be end + 1s to make it inclusive
		assert.Contains(t, query, "stop: 2024-06-01T00:00:01Z")
	})

	t.Run("with limit", func(t *testing.T) {
		query := buildFluxQuery("b", "m", start, end, 50)
		assert.Contains(t, query, "limit(n: 50)")
	})

	t.Run("escapes double quotes", func(t *testing.T) {
		query := buildFluxQuery(`my"bucket`, `my"measurement`, start, end, 0)
		assert.Contains(t, query, `my\"bucket`)
		assert.Contains(t, query, `my\"measurement`)
	})
}

func TestGetTable_StrategyLogic(t *testing.T) {
	s := NewInfluxDBSource()
	ctx := context.Background()

	t.Run("default strategy is append", func(t *testing.T) {
		table, err := s.GetTable(ctx, source.TableRequest{Name: "cpu"})
		require.NoError(t, err)
		assert.Equal(t, config.StrategyAppend, table.Strategy())
	})

	t.Run("user-provided strategy is applied", func(t *testing.T) {
		table, err := s.GetTable(ctx, source.TableRequest{
			Name:     "cpu",
			Strategy: config.StrategyReplace,
		})
		require.NoError(t, err)
		assert.Equal(t, config.StrategyReplace, table.Strategy())
	})

	t.Run("merge strategy is applied when given", func(t *testing.T) {
		table, err := s.GetTable(ctx, source.TableRequest{
			Name:        "cpu",
			Strategy:    config.StrategyMerge,
			PrimaryKeys: []string{"host", "time"},
		})
		require.NoError(t, err)
		assert.Equal(t, config.StrategyMerge, table.Strategy())
	})

	t.Run("incremental key is always time", func(t *testing.T) {
		table, err := s.GetTable(ctx, source.TableRequest{Name: "cpu"})
		require.NoError(t, err)
		assert.Equal(t, "time", table.IncrementalKey())
	})

	t.Run("has no known schema", func(t *testing.T) {
		table, err := s.GetTable(ctx, source.TableRequest{Name: "cpu"})
		require.NoError(t, err)
		assert.False(t, table.HasKnownSchema())
	})
}
