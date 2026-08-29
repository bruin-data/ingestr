//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSourcePipelineFileFormatsAndAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	parquetPath := filepath.Join(t.TempDir(), "users.parquet")
	writeSeedParquet(t, parquetPath, 2)
	parquetBody, err := os.ReadFile(parquetPath)
	require.NoError(t, err)

	type endpoint struct {
		contentType string
		body        []byte
		authorized  func(*http.Request) bool
	}

	endpoints := map[string]endpoint{
		"/public/users.csv": {
			contentType: "text/csv",
			body:        []byte("id,name\n1,user_1\n2,user_2\n"),
			authorized: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == ""
			},
		},
		"/api/json": {
			contentType: "application/octet-stream",
			body:        []byte(`[{"id":1,"name":"user_1"},{"id":2,"name":"user_2"}]`),
			authorized: func(r *http.Request) bool {
				return r.Header.Get("X-API-Key") == "json-secret"
			},
		},
		"/api/jsonl": {
			contentType: "application/x-ndjson",
			body:        []byte("{\"id\":1,\"name\":\"user_1\"}\n{\"id\":2,\"name\":\"user_2\"}\n"),
			authorized: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == "Bearer jsonl-token"
			},
		},
		"/private/users.parquet": {
			contentType: "application/vnd.apache.parquet",
			body:        parquetBody,
			authorized: func(r *http.Request) bool {
				username, password, ok := r.BasicAuth()
				return ok && username == "parquet-user" && password == "parquet-password"
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint, ok := endpoints[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if !endpoint.authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", endpoint.contentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(endpoint.body)))
		_, _ = w.Write(endpoint.body)
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name        string
		sourceURI   string
		sourceTable string
	}{
		{
			name:        "CSV without authentication selected by URL extension",
			sourceURI:   server.URL + "/public/users.csv",
			sourceTable: "users",
		},
		{
			name:        "JSON with API key header and explicit format hint",
			sourceURI:   server.URL + "/api/json#ingestr:header.X-API-Key=json-secret",
			sourceTable: "users#json",
		},
		{
			name:        "JSONL with bearer authentication selected by content type",
			sourceURI:   server.URL + "/api/jsonl#ingestr:bearer_token=jsonl-token",
			sourceTable: "users",
		},
		{
			name:        "Parquet with basic authentication selected by URL extension",
			sourceURI:   server.URL + "/private/users.parquet#ingestr:basic_user=parquet-user&basic_password=parquet-password",
			sourceTable: "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			duckDBPath := filepath.Join(t.TempDir(), "out.duckdb")
			cfg := &config.IngestConfig{
				SourceURI:           tt.sourceURI,
				SourceTable:         tt.sourceTable,
				DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
				DestTable:           "main.users",
				IncrementalStrategy: config.StrategyReplace,
			}

			require.NoError(t, cfg.Validate())
			require.NoError(t, pipeline.New(cfg).Run(ctx))
			assert.Equal(t, 2, readDuckDBRowCount(t, duckDBPath, "main.users"))

			db := openDuckDBForTest(t, duckDBPath)
			defer func() { _ = db.Close() }()

			rows, err := db.QueryContext(ctx, "SELECT id, name FROM main.users ORDER BY id")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()

			var actual []string
			for rows.Next() {
				var id int64
				var name string
				require.NoError(t, rows.Scan(&id, &name))
				actual = append(actual, fmt.Sprintf("%d:%s", id, name))
			}
			require.NoError(t, rows.Err())
			assert.Equal(t, []string{"1:user_1", "2:user_2"}, actual)
		})
	}
}
