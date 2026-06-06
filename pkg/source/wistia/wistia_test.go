package wistia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWistiaURI(t *testing.T) {
	t.Run("query token", func(t *testing.T) {
		creds, err := parseWistiaURI("wistia://?access_token=abc&api_version=2026-03&base_url=https%3A%2F%2Fexample.com%2Fmodern%2F")
		require.NoError(t, err)
		assert.Equal(t, "abc", creds.accessToken)
		assert.Equal(t, "2026-03", creds.apiVersion)
		assert.Equal(t, "https://example.com/modern", creds.apiURL)
	})

	t.Run("api key alias", func(t *testing.T) {
		creds, err := parseWistiaURI("wistia://?api_key=abc")
		require.NoError(t, err)
		assert.Equal(t, "abc", creds.accessToken)
		assert.Equal(t, defaultAPIVersion, creds.apiVersion)
		assert.Equal(t, defaultBaseURL, creds.apiURL)
	})

	t.Run("bare token", func(t *testing.T) {
		creds, err := parseWistiaURI("wistia://abc123")
		require.NoError(t, err)
		assert.Equal(t, "abc123", creds.accessToken)
	})

	t.Run("missing token", func(t *testing.T) {
		_, err := parseWistiaURI("wistia://?api_version=2026-03")
		require.Error(t, err)
	})
}

func TestGetTable(t *testing.T) {
	src := NewWistiaSource()

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "stats_media_by_date:abc123"})
	require.NoError(t, err)
	assert.Equal(t, "stats_media_by_date:abc123", table.Name())
	assert.Equal(t, []string{"media_id", "date"}, table.PrimaryKeys())
	assert.Equal(t, "date", table.IncrementalKey())
	assert.Equal(t, "date", table.(source.PartitionedTable).PartitionBy())

	table, err = src.GetTable(context.Background(), source.TableRequest{Name: "captions"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, table.PrimaryKeys())

	_, err = src.GetTable(context.Background(), source.TableRequest{Name: "stats_media_by_date"})
	require.Error(t, err)

	_, err = src.GetTable(context.Background(), source.TableRequest{Name: "unknown"})
	require.Error(t, err)
}

func TestReadPaginated(t *testing.T) {
	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		assert.Equal(t, "2026-03", r.Header.Get(apiVersionHeader))
		assert.Equal(t, "/medias", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("per_page"))

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)
		pages = append(pages, page)

		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 1:
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]interface{}{
				{"hashed_id": "a", "name": "A"},
				{"hashed_id": "b", "name": "B"},
			}))
		case 2:
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]interface{}{
				{"hashed_id": "c", "name": "C"},
			}))
		default:
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]interface{}{}))
		}
	}))
	defer server.Close()

	src := NewWistiaSource()
	err := src.Connect(context.Background(), "wistia://?access_token=secret&base_url="+url.QueryEscape(server.URL))
	require.NoError(t, err)
	defer func() { require.NoError(t, src.Close(context.Background())) }()

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "medias"})
	require.NoError(t, err)

	records, err := table.Read(context.Background(), source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	var rows int64
	for result := range records {
		require.NoError(t, result.Err)
		rows += result.Batch.NumRows()
		result.Batch.Release()
	}

	assert.Equal(t, int64(3), rows)
	assert.Equal(t, []int{1, 2}, pages)
}

func TestReadDateFilteredParameterizedTable(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		assert.Equal(t, "/stats/medias/abc123/by_date", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]map[string]interface{}{
			{"date": "2026-01-02", "play_count": 3},
		}))
	}))
	defer server.Close()

	src := NewWistiaSource()
	err := src.Connect(context.Background(), "wistia://?access_token=secret&base_url="+url.QueryEscape(server.URL))
	require.NoError(t, err)
	defer func() { require.NoError(t, src.Close(context.Background())) }()

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "stats_media_by_date:abc123"})
	require.NoError(t, err)

	start := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
	records, err := table.Read(context.Background(), source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batchCount int
	for result := range records {
		require.NoError(t, result.Err)
		batchCount++
		assert.Equal(t, int64(1), result.Batch.NumRows())
		indices := result.Batch.Schema().FieldIndices("media_id")
		require.NotEmpty(t, indices)
		result.Batch.Release()
	}

	assert.Equal(t, 1, batchCount)
	assert.Equal(t, "2026-01-02", gotQuery.Get("start_date"))
	assert.Equal(t, "2026-01-03", gotQuery.Get("end_date"))
}

func TestWistiaDateRange(t *testing.T) {
	defaultingCfg := tableConfigs["stats_account_by_date"]
	nonDefaultingCfg := tableConfigs["stats_media_by_date"]

	start, end := wistiaDateRange(defaultingCfg, source.ReadOptions{})
	require.NotEmpty(t, start)
	require.NotEmpty(t, end)

	startDate, err := time.Parse("2006-01-02", start)
	require.NoError(t, err)
	endDate, err := time.Parse("2006-01-02", end)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, endDate.Sub(startDate))

	start, end = wistiaDateRange(nonDefaultingCfg, source.ReadOptions{})
	assert.Empty(t, start)
	assert.Empty(t, end)

	onlyEnd := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
	start, end = wistiaDateRange(nonDefaultingCfg, source.ReadOptions{IntervalEnd: &onlyEnd})
	assert.Equal(t, "2026-01-02", start)
	assert.Equal(t, "2026-01-03", end)

	onlyStart := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	start, end = wistiaDateRange(nonDefaultingCfg, source.ReadOptions{IntervalStart: &onlyStart})
	assert.Equal(t, "2026-01-02", start)
	require.NotEmpty(t, end)
}
