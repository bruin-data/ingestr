package ripestat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr string
	}{
		{name: "empty", uri: "ripestat://"},
		{name: "wrong scheme", uri: "https://stat.ripe.net", wantErr: "must be ripestat://"},
		{name: "host", uri: "ripestat://AS3333", wantErr: "parameters on the source table"},
		{name: "path", uri: "ripestat:///AS3333", wantErr: "parameters on the source table"},
		{name: "parameters", uri: "ripestat://?resource=AS3333", wantErr: "parameters on the source table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestParseTable(t *testing.T) {
	endpoint, params, err := parseTable("announced-prefixes?resource=AS3333&sourceapp=my_app&data_overload_limit=custom&tag=one&tag=two")
	require.NoError(t, err)

	assert.Equal(t, "announced-prefixes", endpoint)
	assert.Equal(t, "AS3333", params.Get("resource"))
	assert.Equal(t, "my_app", params.Get("sourceapp"))
	assert.Equal(t, "custom", params.Get("data_overload_limit"))
	assert.Equal(t, []string{"one", "two"}, params["tag"])
}

func TestParseTableDefaults(t *testing.T) {
	endpoint, params, err := parseTable("example-resources")
	require.NoError(t, err)

	assert.Equal(t, "example-resources", endpoint)
	assert.Equal(t, "ingestr", params.Get("sourceapp"))
	assert.Equal(t, "ignore", params.Get("data_overload_limit"))
}

func TestParseTableRejectsCallback(t *testing.T) {
	_, _, err := parseTable("as-overview?resource=AS3333&callback=handle")
	require.ErrorContains(t, err, "callback is not supported")
}

func TestGetTableUsesRequestedStrategyAndPrimaryKeys(t *testing.T) {
	s := NewRIPEstatSource()
	table, err := s.GetTable(context.Background(), source.TableRequest{
		Name:        "announced-prefixes?resource=AS3333",
		Strategy:    config.StrategyAppend,
		PrimaryKeys: []string{"resource"},
	})
	require.NoError(t, err)

	assert.Equal(t, config.StrategyAppend, table.Strategy())
	assert.Equal(t, []string{"resource"}, table.PrimaryKeys())
}

func TestIsValidEndpoint(t *testing.T) {
	assert.True(t, isValidEndpoint("announced-prefixes"))
	assert.True(t, isValidEndpoint("as-overview"))
	assert.False(t, isValidEndpoint(""))
	assert.False(t, isValidEndpoint("AS-overview"))
	assert.False(t, isValidEndpoint("../meta"))
	assert.False(t, isValidEndpoint("as_overview"))
}

func TestApplyIntervalParams(t *testing.T) {
	start := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("test", 2*60*60))
	end := time.Date(2026, time.January, 3, 4, 5, 6, 0, time.FixedZone("test", 2*60*60))
	original := map[string][]string{
		"resource":  {"AS3333"},
		"starttime": {"old-start"},
		"endtime":   {"old-end"},
	}

	params, err := applyIntervalParams("announced-prefixes", original, source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-01-02T01:04:05Z", params.Get("starttime"))
	assert.Equal(t, "2026-01-03T02:05:06Z", params.Get("endtime"))
	assert.Equal(t, []string{"old-start"}, original["starttime"])
	assert.Equal(t, []string{"old-end"}, original["endtime"])
}

func TestApplyIntervalParamsUnsupportedEndpoint(t *testing.T) {
	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	_, err := applyIntervalParams("as-overview", make(map[string][]string), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.ErrorContains(t, err, "does not support starttime/endtime")
}

func TestApplyIntervalParamsRequiresBothBounds(t *testing.T) {
	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)

	_, err := applyIntervalParams("announced-prefixes", make(map[string][]string), source.ReadOptions{IntervalStart: &start})
	require.ErrorContains(t, err, "require both --interval-start and --interval-end")
}

func TestDecodeDataUsesNumber(t *testing.T) {
	item, err := decodeData(json.RawMessage(`{"asn":9007199254740993}`))
	require.NoError(t, err)

	value, ok := item["asn"].(json.Number)
	require.True(t, ok)
	assert.Equal(t, "9007199254740993", value.String())
}

func TestRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/data/as-overview/data.json", r.URL.Path)
		assert.Equal(t, "AS3333", r.URL.Query().Get("resource"))
		assert.Equal(t, "ingestr", r.URL.Query().Get("sourceapp"))
		assert.Equal(t, "ignore", r.URL.Query().Get("data_overload_limit"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "ok",
			"status_code": 200,
			"data": map[string]interface{}{
				"resource":  "3333",
				"announced": true,
				"block": map[string]interface{}{
					"name": "test block",
				},
			},
		})
	}))
	defer server.Close()

	s := NewRIPEstatSource()
	require.NoError(t, s.Connect(context.Background(), "ripestat://"))
	defer func() { require.NoError(t, s.Close(context.Background())) }()
	require.NoError(t, s.client.Close())
	s.client = httpclient.New(httpclient.WithBaseURL(server.URL), httpclient.WithDisableRetry())

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "as-overview?resource=AS3333"})
	require.NoError(t, err)
	assert.Equal(t, "as-overview", table.Name())
	assert.False(t, table.HasKnownSchema())

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	defer result.Batch.Release()
	assert.Equal(t, int64(1), result.Batch.NumRows())
	assert.Equal(t, int64(3), result.Batch.NumCols())
}

func TestFetchAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "error",
			"status_code": 400,
			"messages":    [][]string{{"error", "resource is required"}},
			"data":        map[string]interface{}{},
		})
	}))
	defer server.Close()

	s := NewRIPEstatSource()
	s.client = httpclient.New(httpclient.WithBaseURL(server.URL), httpclient.WithDisableRetry())
	defer func() { require.NoError(t, s.client.Close()) }()

	_, err := s.fetch(context.Background(), "as-overview", make(map[string][]string))
	require.ErrorContains(t, err, "returned API status \"error\" (400)")
}
