package api_football

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestAPIFootballMissingAPIKey(t *testing.T) {
	src := NewAPIFootballSource()
	err := src.Connect(context.Background(), "api-football://")
	require.ErrorContains(t, err, "api_key")
}

func TestAPIFootballRejectsInvalidLeagueAndSeason(t *testing.T) {
	src := NewAPIFootballSource()
	err := src.Connect(context.Background(), "api-football://?api_key=test&league=worldcup")
	require.ErrorContains(t, err, "league")

	err = src.Connect(context.Background(), "api-football://?api_key=test&season=26")
	require.ErrorContains(t, err, "season")
}

func TestAPIFootballReadTeamsUsesAuthLeagueAndSeason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/teams", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-apisports-key"))
		require.Equal(t, "1", r.URL.Query().Get("league"))
		require.Equal(t, "2026", r.URL.Query().Get("season"))
		_, _ = fmt.Fprint(w, `{"get":"teams","parameters":{"league":"1","season":"2026"},"errors":[],"results":1,"paging":{"current":1,"total":1},"response":[{"team":{"id":50,"name":"Brazil","code":"BRA","country":"Brazil","founded":1914,"national":true,"logo":"https://example.com/bra.png"},"venue":{"id":10,"name":"Maracana","address":"Rua","city":"Rio de Janeiro","capacity":78838,"surface":"grass","image":"https://example.com/venue.png"}}]}`)
	}))
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	require.ElementsMatch(t, []string{"id", "team", "venue"}, columnNames(record))
	require.Equal(t, "50", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	// Nested objects are preserved as JSON, not flattened into top-level columns.
	team := decodeUnknown(t, record, "team", 0).(map[string]interface{})
	require.Equal(t, "Brazil", team["name"])
}

func TestAPIFootballPlayersPaginates(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/players", r.URL.Path)
		pages = append(pages, r.URL.Query().Get("page"))
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":2},"response":[{"player":{"id":1,"name":"Player One","firstname":"Player","lastname":"One","age":29,"birth":{"date":"1997-01-01","place":"A","country":"B"},"nationality":"B","height":"180 cm","weight":"75 kg","injured":false,"photo":"p1"},"statistics":[{"team":{"id":50,"name":"Brazil","logo":"bra"},"games":{"position":"Attacker","number":10,"captain":true,"appearences":1,"lineups":1,"minutes":90,"rating":"7.2"},"goals":{"total":1,"assists":0,"saves":null},"cards":{"yellow":0,"yellowred":0,"red":0}}]}]}`)
		case "2":
			_, _ = fmt.Fprint(w, `{"errors":[],"paging":{"current":2,"total":2},"response":[{"player":{"id":2,"name":"Player Two","firstname":"Player","lastname":"Two","age":24,"birth":{"date":"2002-02-02"},"nationality":"C","height":"175 cm","weight":"70 kg","injured":false,"photo":"p2"},"statistics":[]}]}`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "players"})
	require.NoError(t, err)
	require.Equal(t, "merge", string(table.Strategy()))

	// players streams one batch per page.
	records := readAll(t, table)

	require.Equal(t, []string{"1", "2"}, pages)
	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())
	require.ElementsMatch(t, []string{"id", "player", "statistics"}, columnNames(records[0]))
	require.Equal(t, "1", fmt.Sprint(decodeUnknown(t, records[0], "id", 0)))
	require.Equal(t, "2", fmt.Sprint(decodeUnknown(t, records[1], "id", 0)))
}

func TestAPIFootballMatchesPreserveNestedObjects(t *testing.T) {
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fixtures", r.URL.Path)
		_, _ = fmt.Fprint(w, fixturesPayload())
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&timezone=America/New_York&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)
	require.Equal(t, "merge", string(table.Strategy()))

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	require.ElementsMatch(t, []string{"id", "fixture", "league", "teams", "goals", "score"}, columnNames(record))
	require.Equal(t, "1001", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	teams := decodeUnknown(t, record, "teams", 0).(map[string]interface{})
	home := teams["home"].(map[string]interface{})
	require.Equal(t, "50", fmt.Sprint(home["id"]))
}

func TestAPIFootballStadiumsDeriveFromFixturesAndHydrateVenues(t *testing.T) {
	var requested []string
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/fixtures":
			_, _ = fmt.Fprint(w, fixturesPayload())
		case "/venues":
			require.Equal(t, "200", r.URL.Query().Get("id"))
			_, _ = fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":1},"response":[{"id":200,"name":"MetLife Stadium","address":"1 MetLife Stadium Dr","city":"East Rutherford","country":"USA","capacity":82500,"surface":"grass","image":"stadium.png"}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "stadiums"})
	require.NoError(t, err)

	record := readOnce(t, table)

	require.Len(t, requested, 2)
	require.EqualValues(t, 1, record.NumRows())
	require.Equal(t, "200", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	require.Equal(t, "MetLife Stadium", decodeUnknown(t, record, "name", 0))
}

func TestAPIFootballMatchEventsFanOutFromFixtures(t *testing.T) {
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fixtures":
			_, _ = fmt.Fprint(w, fixturesPayload())
		case "/fixtures/events":
			require.Equal(t, "1001", r.URL.Query().Get("fixture"))
			_, _ = fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":1},"response":[{"time":{"elapsed":23,"extra":null},"team":{"id":50,"name":"Brazil","logo":"bra"},"player":{"id":9,"name":"Forward"},"assist":{"id":10,"name":"Creator"},"type":"Goal","detail":"Normal Goal","comments":null}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "match_events"})
	require.NoError(t, err)
	require.Equal(t, []string{"event_key"}, table.PrimaryKeys())

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	require.Subset(t, columnNames(record), []string{"event_key", "fixture_id", "time", "team", "player", "assist", "type", "detail"})
	require.Equal(t, "1001", fmt.Sprint(decodeUnknown(t, record, "fixture_id", 0)))
	require.Equal(t, "Goal", decodeUnknown(t, record, "type", 0))
	require.NotEmpty(t, decodeUnknown(t, record, "event_key", 0))
}

func TestAPIFootballUnsupportedTable(t *testing.T) {
	src := NewAPIFootballSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "odds"})
	require.ErrorContains(t, err, "unsupported table")
}

// readAll drains every streamed batch and registers cleanup for them.
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

// readOnce drains all batches and asserts exactly one was emitted.
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

// decodeUnknown reads a value from an inference-driven Unknown column, which
// stores each value as a JSON-encoded string in extension-array storage.
func decodeUnknown(t *testing.T, record arrow.RecordBatch, col string, row int) interface{} {
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

func fixtureServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("x-apisports-key"))
		handler(w, r)
	}))
}

func fixturesPayload() string {
	return `{"errors":[],"paging":{"current":1,"total":1},"response":[{"fixture":{"id":1001,"referee":"Ref Name","timezone":"UTC","date":"2026-06-11T00:00:00+00:00","timestamp":1781136000,"periods":{"first":1781136000,"second":1781139600},"venue":{"id":200,"name":"MetLife Stadium","city":"East Rutherford"},"status":{"long":"Not Started","short":"NS","elapsed":null,"extra":null}},"league":{"id":1,"name":"World Cup","country":"World","season":2026,"round":"Group Stage - 1"},"teams":{"home":{"id":50,"name":"Brazil","logo":"bra","winner":null},"away":{"id":51,"name":"France","logo":"fra","winner":null}},"goals":{"home":null,"away":null},"score":{"halftime":{"home":null,"away":null},"fulltime":{"home":null,"away":null},"extratime":{"home":null,"away":null},"penalty":{"home":null,"away":null}}}]}`
}
