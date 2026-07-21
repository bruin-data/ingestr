package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantAPIKey   string
		wantCodexKey string
		wantErr      string
	}{
		{name: "platform key", uri: "openai://?api_key=sk-admin-test", wantAPIKey: "sk-admin-test"},
		{name: "codex key", uri: "openai://?codex_api_key=sk-codex-test", wantCodexKey: "sk-codex-test"},
		{name: "both keys", uri: "openai://?api_key=sk-admin-test&codex_api_key=sk-codex-test", wantAPIKey: "sk-admin-test", wantCodexKey: "sk-codex-test"},
		{name: "encoded", uri: "openai://?api_key=key%2Bvalue&codex_api_key=codex%2Bvalue", wantAPIKey: "key+value", wantCodexKey: "codex+value"},
		{name: "wrong scheme", uri: "https://?api_key=test", wantErr: "must start with openai://"},
		{name: "missing keys", uri: "openai://", wantErr: "at least one of api_key or codex_api_key is required"},
		{name: "empty keys", uri: "openai://?api_key=&codex_api_key=", wantErr: "at least one of api_key or codex_api_key is required"},
		{name: "host is rejected", uri: "openai://host?api_key=test", wantErr: "credentials must be query parameters"},
		{name: "unknown parameter", uri: "openai://?api_key=test&workspace_id=ws", wantErr: "unknown openai URI parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credentials, err := parseURI(tt.uri)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantAPIKey, credentials.apiKey)
			require.Equal(t, tt.wantCodexKey, credentials.codexAPIKey)
		})
	}
}

func TestParseTableSpec(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantPath  string
		wantGroup []string
		wantErr   string
	}{
		{name: "default grouping", table: "api_usage", wantPath: "api_usage", wantGroup: []string{"user_id"}},
		{name: "custom grouping", table: "api_usage?group_by=user_id,model", wantPath: "api_usage", wantGroup: []string{"user_id", "model"}},
		{name: "unknown table", table: "codex_usage", wantPath: "codex_usage"},
		{name: "unknown parameter", table: "api_usage?bucket_width=1h", wantErr: "unknown table parameter"},
		{name: "invalid grouping", table: "api_usage?group_by=organization_id", wantErr: "invalid group_by field"},
		{name: "duplicate grouping", table: "api_usage?group_by=user_id,user_id", wantErr: "duplicate group_by field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, params, err := parseTableSpec(tt.table)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPath, path)
			require.Equal(t, tt.wantGroup, params.GroupBy)
		})
	}
}

func TestGetTable(t *testing.T) {
	src := NewOpenAISource()
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "api_usage?group_by=user_id,model"})
	require.NoError(t, err)
	require.Equal(t, "api_usage", table.Name())
	require.Equal(t, "bucket_start", table.IncrementalKey())
	require.Equal(t, "bucket_start", table.(source.PartitionedTable).PartitionBy())
	require.Equal(t, config.StrategyDeleteInsert, table.Strategy())
	require.False(t, table.HasKnownSchema())
}

func TestReadAPIUsagePaginatesAndPreservesLargeIntegers(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	var requests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, usageEndpoint, r.URL.Path)
		require.Equal(t, "Bearer sk-admin-test", r.Header.Get("Authorization"))
		require.Equal(t, fmt.Sprint(start.Unix()), r.URL.Query().Get("start_time"))
		require.Equal(t, fmt.Sprint(end.Unix()), r.URL.Query().Get("end_time"))
		require.Equal(t, "1d", r.URL.Query().Get("bucket_width"))
		require.Equal(t, "31", r.URL.Query().Get("limit"))
		require.Equal(t, []string{"user_id", "model"}, r.URL.Query()["group_by"])

		switch r.URL.Query().Get("page") {
		case "":
			_, _ = fmt.Fprintf(w, `{"object":"page","data":[{"object":"bucket","start_time":%d,"end_time":%d,"results":[{"object":"organization.usage.completions.result","input_tokens":9007199254740993,"output_tokens":2,"user_id":"user-1","model":"gpt-test"}]}],"has_more":true,"next_page":"page-2"}`, start.Unix(), start.AddDate(0, 0, 1).Unix())
		case "page-2":
			_, _ = fmt.Fprintf(w, `{"object":"page","data":[{"object":"bucket","start_time":%d,"end_time":%d,"results":[{"object":"organization.usage.completions.result","input_tokens":3,"output_tokens":4,"user_id":"user-2","model":"gpt-test"}]}],"has_more":false}`, start.AddDate(0, 0, 1).Unix(), end.Unix())
		default:
			t.Fatalf("unexpected page cursor %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	src := newOpenAISource(server.URL)
	require.NoError(t, src.Connect(context.Background(), "openai://?api_key=sk-admin-test&codex_api_key=sk-codex-test"))
	t.Cleanup(func() { require.NoError(t, src.Close(context.Background())) })

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "api_usage?group_by=user_id,model"})
	require.NoError(t, err)
	results, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	require.NoError(t, err)

	var records []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		records = append(records, result.Batch)
	}
	require.Equal(t, 2, requests)
	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())
	require.Equal(t, "bucket_start", records[0].Schema().Field(0).Name)
	require.Equal(t, arrow.TIMESTAMP, records[0].Schema().Field(0).Type.ID())
	require.Equal(t, "bucket_end", records[0].Schema().Field(1).Name)
	require.Equal(t, arrow.TIMESTAMP, records[0].Schema().Field(1).Type.ID())
	for _, record := range records {
		record.Release()
	}
}

func TestAPIUsageRequiresPlatformKey(t *testing.T) {
	src := NewOpenAISource()
	require.NoError(t, src.Connect(context.Background(), "openai://?codex_api_key=sk-codex-test"))
	t.Cleanup(func() { require.NoError(t, src.Close(context.Background())) })

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "api_usage"})
	require.NoError(t, err)
	_, err = table.Read(context.Background(), source.ReadOptions{})
	require.ErrorContains(t, err, "api_key is required for table api_usage")
}

func TestDecodeUsagePageUsesJSONNumber(t *testing.T) {
	page, err := decodeUsagePage([]byte(`{"data":[{"start_time":1,"end_time":2,"results":[{"input_tokens":9007199254740993}]}],"has_more":false}`))
	require.NoError(t, err)

	value, ok := page.Data[0].Results[0]["input_tokens"].(json.Number)
	require.True(t, ok)
	require.Equal(t, json.Number("9007199254740993"), value)
}

func TestUsageIntervalRejectsInvalidRange(t *testing.T) {
	start := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	end := start
	_, _, err := usageInterval(source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	require.ErrorContains(t, err, "end must be after interval start")
}

func TestUnsupportedTable(t *testing.T) {
	src := NewOpenAISource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "codex_usage"})
	require.ErrorContains(t, err, "unsupported table")
}

func TestRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("openai")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.Contains(t, src.Schemes(), "openai")
}
