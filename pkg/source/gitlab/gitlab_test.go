package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestGitLabMissingAccessToken(t *testing.T) {
	src := NewGitLabSource()
	err := src.Connect(context.Background(), "gitlab://")
	require.ErrorContains(t, err, "access_token")
}

func TestGitLabReadProjectsUsesAuthAndPagination(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/projects", r.URL.Path)
		require.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
		require.Equal(t, "true", r.URL.Query().Get("membership"))
		requests = append(requests, r.URL.RawQuery)

		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = fmt.Fprint(w, `[{"id":1,"name":"client-go","path_with_namespace":"acme/client-go","star_count":99}]`)
		case "2":
			w.Header().Set("X-Next-Page", "")
			_, _ = fmt.Fprint(w, `[{"id":2,"name":"server-go","path_with_namespace":"acme/server-go","star_count":3}]`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	src := NewGitLabSource()
	require.NoError(t, src.Connect(context.Background(), "gitlab://?access_token=test-token&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "projects"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "merge", string(table.Strategy()))
	require.Equal(t, "updated_at", table.IncrementalKey())

	records := readAll(t, table)

	require.Len(t, requests, 2)
	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())
	require.Equal(t, "1", fmt.Sprint(decodeUnknown(t, records[0], "id", 0)))
	require.Equal(t, "2", fmt.Sprint(decodeUnknown(t, records[1], "id", 0)))
}

func TestGitLabIssuesFanOutPerProject(t *testing.T) {
	var mu sync.Mutex
	issueFilterQueries := map[string]url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "")
		switch r.URL.Path {
		case "/projects":
			// project discovery: membership=true, two projects
			require.Equal(t, "true", r.URL.Query().Get("membership"))
			_, _ = fmt.Fprint(w, `[{"id":1,"name":"a"},{"id":2,"name":"b"}]`)
		case "/projects/1/issues":
			mu.Lock()
			issueFilterQueries["1"] = r.URL.Query()
			mu.Unlock()
			_, _ = fmt.Fprint(w, `[{"id":10,"iid":1,"project_id":1,"title":"Bug","labels":["bug"],"assignees":[{"id":5}],"updated_at":"2026-06-01T00:00:00Z"}]`)
		case "/projects/2/issues":
			mu.Lock()
			issueFilterQueries["2"] = r.URL.Query()
			mu.Unlock()
			_, _ = fmt.Fprint(w, `[{"id":20,"iid":1,"project_id":2,"title":"Feature","updated_at":"2026-06-02T00:00:00Z"}]`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	src := NewGitLabSource()
	require.NoError(t, src.Connect(context.Background(), "gitlab://?access_token=test-token&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "issues"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "merge", string(table.Strategy()))
	require.Equal(t, "updated_at", table.IncrementalKey())

	start := mustTime(t, "2026-05-01T00:00:00Z")
	end := mustTime(t, "2026-06-15T00:00:00Z")
	results, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	require.NoError(t, err)

	// One batch per project (fan-out); collect ids across batches.
	var ids []string
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch == nil {
			continue
		}
		for i := 0; i < int(result.Batch.NumRows()); i++ {
			ids = append(ids, fmt.Sprint(decodeUnknown(t, result.Batch, "id", i)))
		}
		result.Batch.Release()
	}
	require.ElementsMatch(t, []string{"10", "20"}, ids)

	// Each per-project request carried the interval + ordering, and no global scope.
	require.Len(t, issueFilterQueries, 2)
	for _, q := range issueFilterQueries {
		require.Equal(t, "2026-05-01T00:00:00Z", q.Get("updated_after"))
		require.Equal(t, "2026-06-15T00:00:00Z", q.Get("updated_before"))
		require.Equal(t, "updated_at", q.Get("order_by"))
		require.Empty(t, q.Get("scope"))
	}
}

func TestGitLabReadRespectsExcludeColumns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "")
		_, _ = fmt.Fprint(w, `[{"id":1,"name":"g","web_url":"https://gitlab.com/g"}]`)
	}))
	defer server.Close()

	src := NewGitLabSource()
	require.NoError(t, src.Connect(context.Background(), "gitlab://?access_token=test-token&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "groups"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{ExcludeColumns: []string{"web_url"}})
	require.NoError(t, err)
	record := drainOne(t, results)
	require.NotContains(t, columnNames(record), "web_url")
}

func TestGitLabReadReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	src := NewGitLabSource()
	require.NoError(t, src.Connect(context.Background(), "gitlab://?access_token=test-token&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "projects"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "authentication or access failed")
}

func TestGitLabRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("gitlab")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.NotNil(t, src)
	require.Contains(t, src.Schemes(), "gitlab")
}

func TestGitLabUnsupportedTable(t *testing.T) {
	src := NewGitLabSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "pipelines"})
	require.ErrorContains(t, err, "unsupported table")
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}

func readAll(t *testing.T, table source.SourceTable) []arrow.RecordBatch {
	t.Helper()
	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	var records []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			records = append(records, result.Batch)
		}
	}
	t.Cleanup(func() {
		for _, r := range records {
			r.Release()
		}
	})
	return records
}

func drainOne(t *testing.T, results <-chan source.RecordBatchResult) arrow.RecordBatch {
	t.Helper()
	var records []arrow.RecordBatch
	for result := range results {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			records = append(records, result.Batch)
		}
	}
	require.Len(t, records, 1)
	t.Cleanup(func() { records[0].Release() })
	return records[0]
}

func columnNames(record arrow.RecordBatch) []string {
	names := make([]string, 0, record.Schema().NumFields())
	for _, f := range record.Schema().Fields() {
		names = append(names, f.Name)
	}
	return names
}

func decodeUnknown(t *testing.T, record arrow.RecordBatch, col string, row int) any {
	t.Helper()
	idx := -1
	for i, f := range record.Schema().Fields() {
		if f.Name == col {
			idx = i
			break
		}
	}
	require.GreaterOrEqualf(t, idx, 0, "column %q not found", col)

	ext, ok := record.Column(idx).(array.ExtensionArray)
	require.Truef(t, ok, "column %q is not an extension array", col)
	raw, ok := schemainfer.StringValueAt(ext.Storage(), row)
	require.Truef(t, ok, "column %q storage is not string-backed", col)
	value, err := schemainfer.DecodeUnknownValue(raw)
	require.NoError(t, err)
	return value
}
