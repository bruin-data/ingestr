package balldontlie

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestBallDontLieMissingAPIKey(t *testing.T) {
	src := NewBallDontLieSource()
	err := src.Connect(context.Background(), "balldontlie://")
	require.ErrorContains(t, err, "api_key")
}

func TestBallDontLieRejectsInvalidSeason(t *testing.T) {
	src := NewBallDontLieSource()
	err := src.Connect(context.Background(), "balldontlie://?api_key=test&season=2030")
	require.ErrorContains(t, err, "season")
}

func TestBallDontLieReadTeamsUsesAuthSeasonAndPagination(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/teams", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("Authorization"))
		require.Equal(t, "2026", r.URL.Query().Get("seasons[]"))
		requests = append(requests, r.URL.RawQuery)

		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = fmt.Fprint(w, `{"data":[{"id":1,"name":"Argentina","abbreviation":"ARG","country_code":"ARG","confederation":"CONMEBOL"}],"meta":{"next_cursor":2,"per_page":1}}`)
		case "2":
			_, _ = fmt.Fprint(w, `{"data":[{"id":2,"name":"Brazil","abbreviation":"BRA","country_code":"BRA","confederation":"CONMEBOL"}],"meta":{"next_cursor":null,"per_page":1}}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "replace", string(table.Strategy()))

	// One batch is streamed per page.
	records := readAll(t, table)

	require.Len(t, requests, 2)
	require.Len(t, records, 2)
	require.EqualValues(t, 1, records[0].NumRows())
	require.EqualValues(t, 1, records[1].NumRows())
	require.ElementsMatch(t, []string{"id", "name", "abbreviation", "country_code", "confederation"}, columnNames(records[0]))
	require.Equal(t, "1", fmt.Sprint(decodeUnknown(t, records[0], "id", 0)))
	require.Equal(t, "2", fmt.Sprint(decodeUnknown(t, records[1], "id", 0)))
}

func TestBallDontLieReadMatchesPreservesNestedObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/matches", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"data":[{"id":11,"match_number":11,"datetime":"2026-06-14T21:00:00Z","status":"scheduled","season":{"id":3,"year":2026},"stage":{"id":1,"name":"Group Stage","order":1},"group":{"id":6,"name":"F"},"stadium":{"id":4,"name":"AT&T Stadium","city":"Dallas","country":"USA"},"home_team":{"id":21,"name":"Netherlands","abbreviation":"NED"},"away_team":null,"away_team_source":{"placeholder":"Runner-up Group C"},"home_score":null,"away_score":null,"has_extra_time":false,"has_penalty_shootout":false}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	// Nested objects are preserved as JSON, not flattened into top-level columns.
	require.Subset(t, columnNames(record), []string{"id", "season", "home_team", "away_team_source"})
	require.Equal(t, "11", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	season := decodeUnknown(t, record, "season", 0).(map[string]any)
	require.Equal(t, "2026", fmt.Sprint(season["year"]))
	homeTeam := decodeUnknown(t, record, "home_team", 0).(map[string]any)
	require.Equal(t, "Netherlands", homeTeam["name"])
}

func TestBallDontLieReadRostersPreservesNestedAndNormalizesStringNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/rosters", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"data":[{"season":{"id":3,"year":2026},"team_id":21,"player":{"id":9,"name":"Forward Name","short_name":"Forward","position":"FW","date_of_birth":"1999-01-02","country_code":"NED","country_name":"Netherlands","height_cm":184,"jersey_number":"10"},"position":"attacker","appearances":1,"starts":1,"minutes_played":90,"goals":1,"assists":0,"yellow_cards":0,"red_cards":0,"avg_rating":"null"}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "rosters"})
	require.NoError(t, err)
	require.Equal(t, []string{"season_year", "team_id", "player_id"}, table.PrimaryKeys())
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	require.Subset(t, columnNames(record), []string{"season", "team_id", "player", "avg_rating"})
	player := decodeUnknown(t, record, "player", 0).(map[string]any)
	require.Equal(t, "9", fmt.Sprint(player["id"]))
	// The string "null" is normalized to a real null.
	require.True(t, columnIsNull(record, "avg_rating", 0))
}

func TestBallDontLieReadMatchEventsPreservesNestedPlayers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/match_events", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"data":[{"id":700,"match_id":11,"incident_type":"goal","incident_class":"regular","time_minute":23,"added_time":null,"period":"first_half","is_home":true,"player":{"id":9,"name":"Scorer"},"assist_player":{"id":10,"name":"Creator"},"player_in":null,"player_out":null,"home_score":1,"away_score":0,"shootout_sequence":null,"shootout_description":null,"rescinded":false,"reason":null}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "match_events"})
	require.NoError(t, err)
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table)

	require.EqualValues(t, 1, record.NumRows())
	require.Subset(t, columnNames(record), []string{"id", "player", "assist_player", "player_in"})
	require.Equal(t, "700", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	player := decodeUnknown(t, record, "player", 0).(map[string]any)
	require.Equal(t, "9", fmt.Sprint(player["id"]))
	assist := decodeUnknown(t, record, "assist_player", 0).(map[string]any)
	require.Equal(t, "Creator", assist["name"])
}

func TestBallDontLieReadRespectsExcludeColumns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/teams", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"data":[{"id":1,"name":"Argentina","abbreviation":"ARG","country_code":"ARG","confederation":"CONMEBOL"},{"id":2,"name":"Brazil","abbreviation":"BRA","country_code":"BRA","confederation":"CONMEBOL"}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{ExcludeColumns: []string{"confederation"}})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 2, result.Batch.NumRows())
	require.NotContains(t, columnNames(result.Batch), "confederation")
}

func TestBallDontLieReadReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	src := NewBallDontLieSource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "authentication or plan access failed")
}

func TestBallDontLieRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("balldontlie")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.NotNil(t, src)
	require.Contains(t, src.Schemes(), "balldontlie")
}

func TestBallDontLieUnsupportedTable(t *testing.T) {
	src := NewBallDontLieSource()
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

// columnIsNull reports whether the value at the given column/row is null.
func columnIsNull(record arrow.RecordBatch, col string, row int) bool {
	for i, f := range record.Schema().Fields() {
		if f.Name == col {
			return record.Column(i).IsNull(row)
		}
	}
	return false
}

// decodeUnknown reads a value from an inference-driven Unknown column, which
// stores each value as a JSON-encoded string in extension-array storage.
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
