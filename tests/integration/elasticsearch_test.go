//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestElasticsearchSourceToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if elasticShared.uri == "" {
		t.Skip("shared Elasticsearch container not available")
	}

	ctx := context.Background()
	index := "source_events_" + uniqueSuffix()
	seedElasticsearchDocuments(t, ctx, index, 1005)
	t.Cleanup(func() { deleteElasticsearchIndex(t, context.Background(), index) })

	sqlitePath := filepath.Join(t.TempDir(), "elasticsearch_source.db")
	cfg := config.DefaultConfig()
	cfg.SourceURI = elasticShared.uri
	cfg.SourceTable = index
	cfg.DestURI = "sqlite:///" + sqlitePath
	cfg.DestTable = "events"
	cfg.IncrementalStrategy = config.StrategyReplace
	cfg.IncrementalStrategyExplicit = true
	cfg.NoLoadTimestamp = true
	cfg.Yes = true
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&count))
	require.Equal(t, 1005, count)

	var name string
	var score int64
	var active bool
	require.NoError(t, db.QueryRowContext(
		ctx,
		"SELECT name, score, active FROM events WHERE id = ?",
		"doc-1004",
	).Scan(&name, &score, &active))
	require.Equal(t, "event-1004", name)
	require.Equal(t, int64(1004), score)
	require.True(t, active)
}

func TestElasticsearchDestinationReplace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if elasticShared.uri == "" {
		t.Skip("shared Elasticsearch container not available")
	}

	ctx := context.Background()
	index := "destination_users_" + uniqueSuffix()
	t.Cleanup(func() { deleteElasticsearchIndex(t, context.Background(), index) })

	initialPath := writeElasticsearchJSONL(t, "initial.jsonl",
		`{"id":"user-1","name":"Alice","score":10,"active":true}`,
		`{"id":"user-2","name":"Bob","score":20,"active":false}`,
	)
	runElasticsearchDestinationPipeline(t, ctx, initialPath, index)
	refreshElasticsearchIndex(t, ctx, index)
	require.Equal(t, 2, elasticsearchDocumentCount(t, ctx, index))

	initialDocument := getElasticsearchDocument(t, ctx, index, "user-2")
	require.Equal(t, "user-2", initialDocument["id"])
	require.Equal(t, "Bob", initialDocument["name"])
	require.Equal(t, json.Number("20"), initialDocument["score"])
	initialActive, ok := initialDocument["active"].(bool)
	require.True(t, ok)
	require.False(t, initialActive)

	replacementPath := writeElasticsearchJSONL(t, "replacement.jsonl",
		`{"id":"user-2","name":"Bob Updated","score":30,"active":true}`,
		`{"id":"user-3","name":"Carol","score":40,"active":true}`,
	)
	runElasticsearchDestinationPipeline(t, ctx, replacementPath, index)
	refreshElasticsearchIndex(t, ctx, index)

	require.Equal(t, 2, elasticsearchDocumentCount(t, ctx, index))
	requireElasticsearchDocumentMissing(t, ctx, index, "user-1")
	replacementDocument := getElasticsearchDocument(t, ctx, index, "user-2")
	require.Equal(t, "Bob Updated", replacementDocument["name"])
	require.Equal(t, json.Number("30"), replacementDocument["score"])
	replacementActive, ok := replacementDocument["active"].(bool)
	require.True(t, ok)
	require.True(t, replacementActive)
}

func seedElasticsearchDocuments(t *testing.T, ctx context.Context, index string, count int) {
	t.Helper()

	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for i := 0; i < count; i++ {
		require.NoError(t, encoder.Encode(map[string]any{
			"index": map[string]any{
				"_index": index,
				"_id":    fmt.Sprintf("doc-%04d", i),
			},
		}))
		require.NoError(t, encoder.Encode(map[string]any{
			"name":   fmt.Sprintf("event-%04d", i),
			"score":  i,
			"active": i%2 == 0,
		}))
	}

	response := elasticsearchRequest(t, ctx, http.MethodPost, "/_bulk?refresh=true", &body, "application/x-ndjson", http.StatusOK)
	var result struct {
		Errors bool `json:"errors"`
	}
	decodeElasticsearchResponse(t, response, &result)
	require.False(t, result.Errors)
}

func runElasticsearchDestinationPipeline(t *testing.T, ctx context.Context, sourcePath, index string) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.SourceURI = "jsonl://" + sourcePath
	cfg.SourceTable = "records"
	cfg.DestURI = elasticShared.uri
	cfg.DestTable = index
	cfg.IncrementalStrategy = config.StrategyReplace
	cfg.IncrementalStrategyExplicit = true
	cfg.PrimaryKeys = []string{"id"}
	cfg.NoLoadTimestamp = true
	cfg.Yes = true
	cfg.PageSize = 1
	cfg.ExtractParallelism = 1
	require.NoError(t, pipeline.New(cfg).Run(ctx))
}

func writeElasticsearchJSONL(t *testing.T, name string, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func refreshElasticsearchIndex(t *testing.T, ctx context.Context, index string) {
	t.Helper()
	elasticsearchRequest(t, ctx, http.MethodPost, "/"+url.PathEscape(index)+"/_refresh", nil, "", http.StatusOK)
}

func elasticsearchDocumentCount(t *testing.T, ctx context.Context, index string) int {
	t.Helper()

	response := elasticsearchRequest(t, ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_count", nil, "", http.StatusOK)
	var result struct {
		Count int `json:"count"`
	}
	decodeElasticsearchResponse(t, response, &result)
	return result.Count
}

func getElasticsearchDocument(t *testing.T, ctx context.Context, index, id string) map[string]any {
	t.Helper()

	response := elasticsearchRequest(
		t,
		ctx,
		http.MethodGet,
		"/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id),
		nil,
		"",
		http.StatusOK,
	)
	var result struct {
		Source map[string]any `json:"_source"`
	}
	decodeElasticsearchResponse(t, response, &result)
	return result.Source
}

func requireElasticsearchDocumentMissing(t *testing.T, ctx context.Context, index, id string) {
	t.Helper()
	elasticsearchRequest(
		t,
		ctx,
		http.MethodGet,
		"/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id),
		nil,
		"",
		http.StatusNotFound,
	)
}

func deleteElasticsearchIndex(t *testing.T, ctx context.Context, index string) {
	t.Helper()
	elasticsearchRequest(t, ctx, http.MethodDelete, "/"+url.PathEscape(index), nil, "", http.StatusOK, http.StatusNotFound)
}

func elasticsearchRequest(
	t *testing.T,
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
	contentType string,
	expectedStatuses ...int,
) []byte {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, method, elasticShared.baseURL+path, body)
	require.NoError(t, err)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, expectedStatuses, response.StatusCode, string(responseBody))
	return responseBody
}

func decodeElasticsearchResponse(t *testing.T, response []byte, result any) {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(result))
}
