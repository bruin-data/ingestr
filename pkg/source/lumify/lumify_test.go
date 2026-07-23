package lumify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestLumifyMissingAPIKey(t *testing.T) {
	src := NewLumifySource()
	err := src.Connect(context.Background(), "lumify://")
	require.ErrorContains(t, err, "api_key")
}

func TestLumifyConnectStripsBearerPrefix(t *testing.T) {
	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=Bearer%20lmfy-test"))
	require.Equal(t, "lmfy-test", src.apiKey)
}

func TestLumifyReadTeamsUsesAuthSportAndPagination(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/teams", r.URL.Path)
		require.Equal(t, "Bearer lmfy-test", r.Header.Get("Authorization"))
		require.Equal(t, "nba", r.URL.Query().Get("sport"))
		requests = append(requests, r.URL.RawQuery)

		switch r.URL.Query().Get("after_id") {
		case "":
			_, _ = fmt.Fprint(w, `{"data":[{"id":1,"slug":"lal","name":"Los Angeles Lakers","sport":"nba"}],"has_more":true,"next_after_id":1}`)
		case "1":
			_, _ = fmt.Fprint(w, `{"data":[{"id":2,"slug":"bos","name":"Boston Celtics","sport":"nba"}],"has_more":false,"next_after_id":null}`)
		default:
			t.Fatalf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	defer server.Close()

	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=lmfy-test&sport=nba&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "replace", string(table.Strategy()))

	records := readAll(t, table)
	require.Len(t, requests, 2)
	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())
	require.Equal(t, "1", fmt.Sprint(decodeUnknown(t, records[0], "id", 0)))
	require.Equal(t, "2", fmt.Sprint(decodeUnknown(t, records[1], "id", 0)))
}

func TestLumifyReadSportsPreservesNestedLeagues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/sports", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"sports":[{"id":1,"slug":"nba","name":"NBA","is_team_sport":true,"leagues":[{"id":10,"slug":"nba","name":"NBA","abbr":"NBA"}]}],"total":1}`)
	}))
	defer server.Close()

	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=lmfy-test&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "sports"})
	require.NoError(t, err)

	record := readOnce(t, table)
	require.EqualValues(t, 1, record.NumRows())
	require.Subset(t, columnNames(record), []string{"id", "slug", "leagues"})
	leagues := decodeUnknown(t, record, "leagues", 0).([]any)
	require.Len(t, leagues, 1)
	require.Equal(t, "NBA", leagues[0].(map[string]any)["name"])
}

func TestLumifyReadLeaguesFlattensNestedSport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/sports", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"sports":[{"id":1,"slug":"nba","name":"NBA","leagues":[{"id":10,"slug":"nba","name":"NBA"}]},{"id":2,"slug":"nfl","name":"NFL","leagues":[{"id":20,"slug":"nfl","name":"NFL"}]}],"total":2}`)
	}))
	defer server.Close()

	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=lmfy-test&sport=nba&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "leagues"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())

	record := readOnce(t, table)
	require.EqualValues(t, 1, record.NumRows())
	require.Subset(t, columnNames(record), []string{"id", "slug", "sport_id", "sport_slug", "sport_name"})
	require.Equal(t, "10", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	require.Equal(t, "nba", fmt.Sprint(decodeUnknown(t, record, "sport_slug", 0)))
}

func TestLumifyReadEventsUsesDateWindowAndIncludeScores(t *testing.T) {
	var gotFrom, gotTo, includeScores string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/events", r.URL.Path)
		gotFrom = r.URL.Query().Get("from")
		gotTo = r.URL.Query().Get("to")
		includeScores = r.URL.Query().Get("include_scores")
		_, _ = fmt.Fprint(w, `{"events":[{"id":99,"name":"Lakers vs Celtics","sport":"nba","starts_at":"2026-07-20T00:00:00Z","status":"scheduled","participants":[{"role":"home","name":"Lakers"}]}],"total":1,"next_after_id":null}`)
	}))
	defer server.Close()

	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=lmfy-test&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "events"})
	require.NoError(t, err)
	require.Equal(t, "merge", string(table.Strategy()))

	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(context.Background(), source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.Equal(t, "2026-07-20T00:00:00Z", gotFrom)
	require.Equal(t, "2026-07-21T00:00:00Z", gotTo)
	require.Equal(t, "true", includeScores)
	require.EqualValues(t, 1, result.Batch.NumRows())
	require.Equal(t, "99", fmt.Sprint(decodeUnknown(t, result.Batch, "id", 0)))
	participants := decodeUnknown(t, result.Batch, "participants", 0).([]any)
	require.Equal(t, "Lakers", participants[0].(map[string]any)["name"])
}

func TestLumifyReadReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	src := NewLumifySource()
	require.NoError(t, src.Connect(context.Background(), "lumify://?api_key=lmfy-test&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "authentication failed")
}

func TestLumifyRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("lumify")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.NotNil(t, src)
	require.Contains(t, src.Schemes(), "lumify")
}

func TestLumifyUnsupportedTable(t *testing.T) {
	src := NewLumifySource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "odds"})
	require.ErrorContains(t, err, "unsupported table")
}

func TestEventWindowsChunksLongIntervals(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	windows, err := eventWindows(&start, &end)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(windows), 2)
	require.True(t, windows[0][0].Equal(start))
	require.True(t, windows[len(windows)-1][1].Equal(end))
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

func readOnce(t *testing.T, table source.SourceTable) arrow.RecordBatch {
	t.Helper()
	records := readAll(t, table)
	require.Len(t, records, 1)
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
