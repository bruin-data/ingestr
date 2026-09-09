//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc" // register adbc_generic for DuckDB read-back
	"github.com/bruin-data/ingestr/pkg/source/couchdb"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	couchDBUser     = "ingestr_admin"
	couchDBPassword = "synthetic_password"
	couchDBPort     = "5984/tcp"
	couchDBDatabase = "integration_docs"
)

func TestCouchDBToDuckDBReplace(t *testing.T) {
	requireDocker(t)
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, sourceURI, httpBase := startCouchDBContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	couchDBRequest(t, ctx, httpBase, http.MethodPut, "/"+couchDBDatabase, nil, http.StatusCreated)
	couchDBPutDocument(t, ctx, httpBase, "_design/ignored", map[string]any{
		"views": map[string]any{"all": map[string]any{"map": "function (doc) { emit(doc._id, 1); }"}},
	})
	couchDBPutDocument(t, ctx, httpBase, "a", map[string]any{
		"name": "alpha", "sequence": int64(9007199254740993),
		"profile": map[string]any{"city": "Oslo", "tags": []string{"a", "b"}}, "nullable": nil, "secret": "omit-me",
	})
	couchDBPutDocument(t, ctx, httpBase, "b", map[string]any{
		"name": "beta", "sequence": int64(2),
		"profile": map[string]any{"city": "Lima", "tags": []string{"c"}}, "nullable": "present", "secret": "omit-me",
	})
	couchDBPutDocument(t, ctx, httpBase, "c", map[string]any{
		"name": "gamma", "sequence": int64(3),
		"profile": map[string]any{"city": "Tokyo", "tags": []string{}}, "nullable": nil, "secret": "omit-me",
	})
	deletedRevision := couchDBPutDocument(t, ctx, httpBase, "d-deleted", map[string]any{"name": "tombstone"})
	couchDBRequest(t, ctx, httpBase, http.MethodDelete, "/"+couchDBDatabase+"/d-deleted?rev="+deletedRevision, nil, http.StatusOK)

	src := couchdb.NewCouchDBSource()
	require.NoError(t, src.Connect(ctx, sourceURI))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	_, err := src.GetTable(ctx, source.TableRequest{Name: "missing_database"})
	require.ErrorContains(t, err, "HTTP 404")
	table, err := src.GetTable(ctx, source.TableRequest{Name: couchDBDatabase})
	require.NoError(t, err)
	require.Equal(t, []string{"_id"}, table.PrimaryKeys())
	_, err = src.GetTable(ctx, source.TableRequest{Name: couchDBDatabase, IncrementalKey: "updated_at"})
	require.ErrorContains(t, err, "does not support incremental keys")
	now := time.Now()
	for _, opts := range []source.ReadOptions{{IncrementalKey: "updated_at"}, {IntervalStart: &now}, {IntervalEnd: &now}} {
		_, err := table.Read(ctx, opts)
		require.ErrorContains(t, err, "does not support incremental filtering")
	}
	limited, err := table.Read(ctx, source.ReadOptions{PageSize: 2, Limit: 2})
	require.NoError(t, err)
	var limitedRows int64
	for result := range limited {
		require.NoError(t, result.Err)
		limitedRows += result.Batch.NumRows()
		result.Batch.Release()
	}
	require.Equal(t, int64(2), limitedRows)
	for _, capBytes := range []int64{0, 1} {
		batches, err := table.Read(ctx, source.ReadOptions{PageSize: 2, MaxBatchBytes: capBytes, ExcludeColumns: []string{"secret"}})
		require.NoError(t, err)
		var counts []int64
		for result := range batches {
			require.NoError(t, result.Err)
			counts = append(counts, result.Batch.NumRows())
			require.Empty(t, result.Batch.Schema().FieldIndices("secret"))
			result.Batch.Release()
		}
		if capBytes == 0 {
			require.Equal(t, []int64{1, 2}, counts)
		} else {
			require.Equal(t, []int64{1, 1, 1}, counts)
		}
	}
	couchDBRequest(t, ctx, httpBase, http.MethodPut, "/empty_database", nil, http.StatusCreated)
	empty, err := src.GetTable(ctx, source.TableRequest{Name: "empty_database"})
	require.NoError(t, err)
	batches, err := empty.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)
	for result := range batches {
		if result.Batch != nil {
			result.Batch.Release()
		}
		t.Fatalf("expected no batches for empty database, got error %v", result.Err)
	}

	duckPath := filepath.Join(t.TempDir(), "couchdb.duckdb")
	cfg := config.DefaultConfig()
	cfg.SourceURI = sourceURI
	cfg.SourceTable = couchDBDatabase
	cfg.DestURI = "duckdb:///" + duckPath
	cfg.DestTable = "main.documents"
	cfg.IncrementalStrategy = config.StrategyReplace
	cfg.IncrementalStrategyExplicit = true
	cfg.PageSize = 2
	cfg.MaxBatchBytes = 180
	cfg.SQLExcludeColumns = []string{"secret"}
	cfg.NoLoadTimestamp = true
	cfg.NoRunID = true
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	duck, err := sql.Open("adbc_generic", "driver=duckdb;path="+duckPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = duck.Close() })

	assertCouchDBRows(t, ctx, duck, []string{"a", "b", "c"})
	var sequence int64
	var profileJSON string
	var nullable sql.NullString
	require.NoError(t, duck.QueryRowContext(ctx,
		`SELECT sequence, CAST(profile AS VARCHAR), nullable FROM main.documents WHERE _id = 'a'`,
	).Scan(&sequence, &profileJSON, &nullable))
	require.Equal(t, int64(9007199254740993), sequence)
	require.False(t, nullable.Valid)
	var profile map[string]any
	require.NoError(t, json.Unmarshal([]byte(profileJSON), &profile))
	require.Equal(t, "Oslo", profile["city"])
	require.Equal(t, []any{"a", "b"}, profile["tags"])

	var secretColumns int
	require.NoError(t, duck.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'main' AND table_name = 'documents' AND column_name = 'secret'`).Scan(&secretColumns))
	require.Zero(t, secretColumns)

	aRevision := couchDBDocumentRevision(t, ctx, httpBase, "a")
	couchDBPutDocument(t, ctx, httpBase, "a", map[string]any{
		"_rev": aRevision, "name": "alpha-updated", "sequence": int64(9007199254740993),
		"profile": map[string]any{"city": "Bergen", "tags": []string{"updated"}}, "nullable": nil, "secret": "still-omitted",
	})
	bRevision := couchDBDocumentRevision(t, ctx, httpBase, "b")
	couchDBRequest(t, ctx, httpBase, http.MethodDelete, "/"+couchDBDatabase+"/b?rev="+bRevision, nil, http.StatusOK)

	require.NoError(t, duck.Close())
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	duck, err = sql.Open("adbc_generic", "driver=duckdb;path="+duckPath)
	require.NoError(t, err)
	assertCouchDBRows(t, ctx, duck, []string{"a", "c"})
	var name, updatedProfile string
	require.NoError(t, duck.QueryRowContext(ctx, `SELECT name, CAST(profile AS VARCHAR) FROM main.documents WHERE _id = 'a'`).Scan(&name, &updatedProfile))
	require.Equal(t, "alpha-updated", name)
	require.JSONEq(t, `{"city":"Bergen","tags":["updated"]}`, updatedProfile)
	for _, id := range []string{"a", "c"} {
		rev := couchDBDocumentRevision(t, ctx, httpBase, id)
		couchDBRequest(t, ctx, httpBase, http.MethodDelete, "/"+couchDBDatabase+"/"+id+"?rev="+rev, nil, http.StatusOK)
	}
	require.NoError(t, duck.Close())
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	duck, err = sql.Open("adbc_generic", "driver=duckdb;path="+duckPath)
	require.NoError(t, err)
	assertCouchDBRows(t, ctx, duck, nil)
}

func startCouchDBContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string, string) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "couchdb:3.4.3",
			ExposedPorts: []string{couchDBPort},
			Env: map[string]string{
				"COUCHDB_USER": couchDBUser, "COUCHDB_PASSWORD": couchDBPassword,
			},
			WaitingFor: wait.ForHTTP("/_up").WithPort(couchDBPort).WithBasicAuth(couchDBUser, couchDBPassword).
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, couchDBPort)
	require.NoError(t, err)
	address := net.JoinHostPort(host, port.Port())
	return container, fmt.Sprintf("couchdb://%s:%s@%s", couchDBUser, couchDBPassword, address), "http://" + address
}

func couchDBPutDocument(t *testing.T, ctx context.Context, base, id string, document map[string]any) string {
	t.Helper()
	response := couchDBRequest(t, ctx, base, http.MethodPut, "/"+couchDBDatabase+"/"+id, document, http.StatusCreated)
	var result struct {
		Rev string `json:"rev"`
	}
	require.NoError(t, json.Unmarshal(response, &result))
	require.NotEmpty(t, result.Rev)
	return result.Rev
}

func couchDBDocumentRevision(t *testing.T, ctx context.Context, base, id string) string {
	t.Helper()
	response := couchDBRequest(t, ctx, base, http.MethodGet, "/"+couchDBDatabase+"/"+id, nil, http.StatusOK)
	var result struct {
		Rev string `json:"_rev"`
	}
	require.NoError(t, json.Unmarshal(response, &result))
	require.NotEmpty(t, result.Rev)
	return result.Rev
}

func couchDBRequest(t *testing.T, ctx context.Context, base, method, path string, body any, status int) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	require.NoError(t, err)
	request.SetBasicAuth(couchDBUser, couchDBPassword)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, status, response.StatusCode, string(payload))
	return payload
}

func assertCouchDBRows(t *testing.T, ctx context.Context, duck *sql.DB, expected []string) {
	t.Helper()
	rows, err := duck.QueryContext(ctx, `SELECT _id FROM main.documents ORDER BY _id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var actual []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		actual = append(actual, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, actual)
}
