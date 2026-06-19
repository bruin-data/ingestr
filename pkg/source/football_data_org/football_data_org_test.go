package football_data_org

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

func TestFootballDataOrgMissingAPIKey(t *testing.T) {
	src := NewFootballDataOrgSource()
	err := src.Connect(context.Background(), "football-data://")
	require.ErrorContains(t, err, "api_key")
}

func TestFootballDataOrgRejectsInvalidSeasonAndMatchday(t *testing.T) {
	src := NewFootballDataOrgSource()
	err := src.Connect(context.Background(), "football-data://?api_key=test&season=26")
	require.ErrorContains(t, err, "season")

	err = src.Connect(context.Background(), "football-data://?api_key=test&matchday=final")
	require.ErrorContains(t, err, "matchday")
}

func TestFootballDataOrgReadTeamsPreservesNestedObjects(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/teams", r.URL.Path)
		require.Equal(t, "2026", r.URL.Query().Get("season"))
		_, _ = fmt.Fprint(w, teamsPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())
	require.Equal(t, "merge", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{})
	defer record.Release()

	require.EqualValues(t, 2, record.NumRows())
	require.Equal(t, "764", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	require.Equal(t, "Brazil", decodeUnknown(t, record, "name", 0))
	require.Equal(t, "Maracana", decodeUnknown(t, record, "venue", 0))
	// Nested objects are preserved as JSON, not flattened into top-level columns.
	require.NotContains(t, columnNames(record), "area_id")
	area := decodeUnknown(t, record, "area", 0).(map[string]interface{})
	require.Equal(t, "Brazil", area["name"])
}

func TestFootballDataOrgMatchesUsesFiltersAndOptionalUnfoldHeaders(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/matches", r.URL.Path)
		require.Equal(t, "2026", r.URL.Query().Get("season"))
		require.Equal(t, "1", r.URL.Query().Get("matchday"))
		require.Equal(t, "SCHEDULED", r.URL.Query().Get("status"))
		require.Equal(t, "2026-06-11", r.URL.Query().Get("dateFrom"))
		require.Equal(t, "2026-06-12", r.URL.Query().Get("dateTo"))
		require.Equal(t, "GROUP_STAGE", r.URL.Query().Get("stage"))
		require.Equal(t, "GROUP_A", r.URL.Query().Get("group"))
		require.Equal(t, "true", r.Header.Get("X-Unfold-Goals"))
		require.Equal(t, "true", r.Header.Get("X-Unfold-Bookings"))
		_, _ = fmt.Fprint(w, matchesPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	uri := "football-data://?api_key=test-token&matchday=1&status=SCHEDULED&date_from=2026-06-11&date_to=2026-06-12&stage=GROUP_STAGE&group=GROUP_A&unfold_goals=true&unfold_bookings=true&base_url=" + url.QueryEscape(server.URL)
	require.NoError(t, src.Connect(context.Background(), uri))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)
	require.Equal(t, "merge", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{})
	defer record.Release()

	require.EqualValues(t, 1, record.NumRows())
	require.Equal(t, "4001", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	require.NotContains(t, columnNames(record), "home_team_id")
	homeTeam := decodeUnknown(t, record, "homeTeam", 0).(map[string]interface{})
	require.Equal(t, "764", fmt.Sprint(homeTeam["id"]))
}

func TestFootballDataOrgMatchesAppliesIngestionInterval(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/matches", r.URL.Path)
		require.Equal(t, "2026-06-11", r.URL.Query().Get("dateFrom"))
		require.Equal(t, "2026-06-20", r.URL.Query().Get("dateTo"))
		_, _ = fmt.Fprint(w, matchesPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)

	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	record := readOnce(t, table, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	defer record.Release()
	require.EqualValues(t, 1, record.NumRows())
}

func TestFootballDataOrgMatchesURIFiltersOverrideInterval(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Explicit URI date filters take precedence over the ingestion interval.
		require.Equal(t, "2026-07-01", r.URL.Query().Get("dateFrom"))
		require.Equal(t, "2026-07-10", r.URL.Query().Get("dateTo"))
		_, _ = fmt.Fprint(w, matchesPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	uri := "football-data://?api_key=test-token&date_from=2026-07-01&date_to=2026-07-10&base_url=" + url.QueryEscape(server.URL)
	require.NoError(t, src.Connect(context.Background(), uri))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)

	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	record := readOnce(t, table, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
	defer record.Release()
	require.EqualValues(t, 1, record.NumRows())
}

func TestFootballDataOrgStandingsEmitsTableRowsRaw(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/standings", r.URL.Path)
		_, _ = fmt.Fprint(w, standingsPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "group_standings"})
	require.NoError(t, err)
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{})
	defer record.Release()

	require.EqualValues(t, 1, record.NumRows())
	require.Equal(t, "GROUP_A", decodeUnknown(t, record, "group_name", 0))
	require.Equal(t, "764", fmt.Sprint(decodeUnknown(t, record, "team_id", 0)))
	// The full standing row is kept as a nested object instead of flattened.
	require.NotContains(t, columnNames(record), "points")
	standing := decodeUnknown(t, record, "standing", 0).(map[string]interface{})
	require.Equal(t, "3", fmt.Sprint(standing["points"]))
}

func TestFootballDataOrgStadiumsDeriveAndDeduplicate(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/competitions/WC/teams":
			_, _ = fmt.Fprint(w, teamsPayload())
		case "/competitions/WC/matches":
			_, _ = fmt.Fprint(w, matchesPayload())
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "stadiums"})
	require.NoError(t, err)
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{})
	defer record.Release()

	require.EqualValues(t, 2, record.NumRows())
	require.ElementsMatch(t, []string{"Maracana", "MetLife Stadium"}, []string{
		fmt.Sprint(decodeUnknown(t, record, "venue", 0)),
		fmt.Sprint(decodeUnknown(t, record, "venue", 1)),
	})
}

func TestFootballDataOrgPlayersHydrateTeamDetails(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/competitions/WC/teams":
			_, _ = fmt.Fprint(w, teamsPayload())
		case "/teams/764":
			_, _ = fmt.Fprint(w, teamDetailPayload(764, "Brazil", "BRA", 10, "Forward One"))
		case "/teams/773":
			_, _ = fmt.Fprint(w, teamDetailPayload(773, "France", "FRA", 11, "Forward Two"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "players"})
	require.NoError(t, err)
	require.Equal(t, []string{"team_id", "id"}, table.PrimaryKeys())
	require.Equal(t, "replace", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{Limit: 1})
	defer record.Release()

	require.EqualValues(t, 1, record.NumRows())
	require.Equal(t, "764", fmt.Sprint(decodeUnknown(t, record, "team_id", 0)))
	require.Equal(t, "10", fmt.Sprint(decodeUnknown(t, record, "id", 0)))
	require.NotContains(t, columnNames(record), "first_name")
	// The raw player object is kept as JSON and carries the richer fields the
	// /teams/<id> detail endpoint returns beyond the embedded squad.
	player := decodeUnknown(t, record, "player", 0).(map[string]interface{})
	require.Equal(t, "Forward One", player["name"])
	require.Equal(t, "Forward", player["firstName"])
	require.Equal(t, "10", fmt.Sprint(player["shirtNumber"]))
}

func TestFootballDataOrgMatchEventsUseUnfoldHeaders(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/matches", r.URL.Path)
		require.Equal(t, "true", r.Header.Get("X-Unfold-Goals"))
		require.Equal(t, "true", r.Header.Get("X-Unfold-Bookings"))
		require.Equal(t, "true", r.Header.Get("X-Unfold-Subs"))
		_, _ = fmt.Fprint(w, matchesPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "match_events"})
	require.NoError(t, err)
	require.Equal(t, []string{"event_key"}, table.PrimaryKeys())
	require.Equal(t, "merge", string(table.Strategy()))

	record := readOnce(t, table, source.ReadOptions{})
	defer record.Release()

	require.EqualValues(t, 3, record.NumRows())
	require.Subset(t, columnNames(record), []string{"event_key", "match_id", "event_type", "event_index"})
	require.Equal(t, "goal", decodeUnknown(t, record, "event_type", 0))
	require.Equal(t, "booking", decodeUnknown(t, record, "event_type", 1))
	require.Equal(t, "substitution", decodeUnknown(t, record, "event_type", 2))
	require.NotEmpty(t, decodeUnknown(t, record, "event_key", 0))
}

func TestFootballDataOrgReadRespectsExcludeColumns(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, teamsPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)

	record := readOnce(t, table, source.ReadOptions{Limit: 1, ExcludeColumns: []string{"area", "lastUpdated"}})
	defer record.Release()

	require.EqualValues(t, 1, record.NumRows())
	require.NotContains(t, columnNames(record), "area")
	require.NotContains(t, columnNames(record), "lastUpdated")
}

func TestFootballDataOrgReadReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"The resource is restricted."}`, http.StatusForbidden)
	}))
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.ErrorContains(t, result.Err, "authentication or plan access failed")
}

func TestFootballDataOrgRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("football-data")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.NotNil(t, src)
	require.Contains(t, src.Schemes(), "football-data")
}

func TestFootballDataOrgUnsupportedTable(t *testing.T) {
	src := NewFootballDataOrgSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "odds"})
	require.ErrorContains(t, err, "unsupported table")
}

func readOnce(t *testing.T, table source.SourceTable, opts source.ReadOptions) arrow.RecordBatch {
	t.Helper()
	results, err := table.Read(context.Background(), opts)
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	return result.Batch
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

func footballDataServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-token", r.Header.Get("X-Auth-Token"))
		handler(w, r)
	}))
}

func teamsPayload() string {
	return `{"competition":{"id":2000,"name":"FIFA World Cup","code":"WC","type":"CUP","emblem":"wc.png"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"teams":[{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png","address":"Rio","website":"https://cbf.example","founded":1914,"clubColors":"Yellow / Green","venue":"Maracana","lastUpdated":"2025-12-01T00:00:00Z","area":{"id":2032,"name":"Brazil","code":"BRA","flag":"br.png"},"squad":[{"id":10,"name":"Forward One","position":"Attacker","dateOfBirth":"1999-01-02","nationality":"Brazil"}]},{"id":773,"name":"France","shortName":"France","tla":"FRA","crest":"fra.png","address":"Paris","website":"https://fff.example","founded":1919,"clubColors":"Blue","venue":"Maracana","lastUpdated":"2025-12-01T00:00:00Z","area":{"id":2081,"name":"France","code":"FRA","flag":"fr.png"},"squad":[{"id":11,"name":"Forward Two","position":"Attacker","dateOfBirth":"2000-02-02","nationality":"France"}]}]}`
}

func standingsPayload() string {
	return `{"competition":{"id":2000,"name":"FIFA World Cup","code":"WC"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"standings":[{"stage":"GROUP_STAGE","type":"TOTAL","group":"GROUP_A","table":[{"position":1,"team":{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png"},"playedGames":1,"form":"W","won":1,"draw":0,"lost":0,"points":3,"goalsFor":2,"goalsAgainst":0,"goalDifference":2}]}]}`
}

func matchesPayload() string {
	return `{"filters":{"season":"2026"},"resultSet":{"count":1},"matches":[{"id":4001,"utcDate":"2026-06-11T20:00:00Z","status":"SCHEDULED","minute":null,"injuryTime":null,"attendance":null,"venue":"MetLife Stadium","matchday":1,"stage":"GROUP_STAGE","group":"GROUP_A","lastUpdated":"2025-12-01T00:00:00Z","competition":{"id":2000,"name":"FIFA World Cup","code":"WC"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"homeTeam":{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png"},"awayTeam":{"id":773,"name":"France","shortName":"France","tla":"FRA","crest":"fra.png"},"score":{"winner":null,"duration":"REGULAR","fullTime":{"home":null,"away":null},"halfTime":{"home":null,"away":null},"regularTime":{"home":null,"away":null},"extraTime":{"home":null,"away":null},"penalties":{"home":null,"away":null}},"referees":[{"id":1,"name":"Ref Name","type":"REFEREE","nationality":"USA"}],"goals":[{"minute":23,"injuryTime":null,"type":"REGULAR","team":{"id":764,"name":"Brazil"},"scorer":{"id":10,"name":"Forward One"},"assist":{"id":12,"name":"Creator"},"score":{"home":1,"away":0}}],"bookings":[{"minute":40,"card":"YELLOW","team":{"id":773,"name":"France"},"player":{"id":11,"name":"Forward Two"}}],"substitutions":[{"minute":65,"team":{"id":764,"name":"Brazil"},"playerOut":{"id":10,"name":"Forward One"},"playerIn":{"id":20,"name":"Fresh Legs"}}]}]}`
}

// teamDetailPayload mirrors the /teams/<id> detail endpoint, whose squad
// members carry richer fields (firstName, lastName, shirtNumber, ...) than the
// squad embedded in the competition teams response.
func teamDetailPayload(teamID int, teamName, tla string, playerID int, playerName string) string {
	return fmt.Sprintf(`{"id":%d,"name":%q,"tla":%q,"squad":[{"id":%d,"name":%q,"firstName":"Forward","lastName":"One","dateOfBirth":"1999-01-02","nationality":%q,"position":"Attacker","shirtNumber":10,"marketValue":1000000,"contract":{"start":"2022-07","until":"2026-06"}}]}`, teamID, teamName, tla, playerID, playerName, teamName)
}
