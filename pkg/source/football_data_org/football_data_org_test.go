package football_data_org

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/registry"
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

func TestFootballDataOrgReadTeamsUsesAuthCompetitionAndSeason(t *testing.T) {
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

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 2, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 764, ids.Value(0))
	names := result.Batch.Column(1).(*array.String)
	require.Equal(t, "Brazil", names.Value(0))
	venues := result.Batch.Column(9).(*array.String)
	require.Equal(t, "Maracana", venues.Value(0))
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

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 4001, ids.Value(0))
	homeTeamIDs := result.Batch.Column(17).(*array.Int64)
	require.EqualValues(t, 764, homeTeamIDs.Value(0))
}

func TestFootballDataOrgStandingsFlattenTableRows(t *testing.T) {
	server := footballDataServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/competitions/WC/standings", r.URL.Path)
		_, _ = fmt.Fprint(w, standingsPayload())
	})
	defer server.Close()

	src := NewFootballDataOrgSource()
	require.NoError(t, src.Connect(context.Background(), "football-data://?api_key=test-token&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "group_standings"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	groupNames := result.Batch.Column(8).(*array.String)
	require.Equal(t, "GROUP_A", groupNames.Value(0))
	points := result.Batch.Column(20).(*array.Int64)
	require.EqualValues(t, 3, points.Value(0))
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

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 2, result.Batch.NumRows())
	names := result.Batch.Column(1).(*array.String)
	require.ElementsMatch(t, []string{"Maracana", "MetLife Stadium"}, []string{names.Value(0), names.Value(1)})
}

func TestFootballDataOrgPlayersHydrateTeamSquads(t *testing.T) {
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

	results, err := table.Read(context.Background(), source.ReadOptions{Limit: 1})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	teamIDs := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 764, teamIDs.Value(0))
	playerIDs := result.Batch.Column(3).(*array.Int64)
	require.EqualValues(t, 10, playerIDs.Value(0))
}

func TestFootballDataOrgMatchEventsUseUnfoldHeadersAndFlatten(t *testing.T) {
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

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 3, result.Batch.NumRows())
	eventTypes := result.Batch.Column(2).(*array.String)
	require.Equal(t, "goal", eventTypes.Value(0))
	require.Equal(t, "booking", eventTypes.Value(1))
	require.Equal(t, "substitution", eventTypes.Value(2))
	playerIDs := result.Batch.Column(8).(*array.Int64)
	require.EqualValues(t, 10, playerIDs.Value(0))
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

	results, err := table.Read(context.Background(), source.ReadOptions{Limit: 1, ExcludeColumns: []string{"team", "area"}})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	require.EqualValues(t, len(teamColumns)-2, int(result.Batch.NumCols()))
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

func footballDataServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-token", r.Header.Get("X-Auth-Token"))
		handler(w, r)
	}))
}

func teamsPayload() string {
	return `{"competition":{"id":2000,"name":"FIFA World Cup","code":"WC","type":"CUP","emblem":"wc.png"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"teams":[{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png","address":"Rio","website":"https://cbf.example","founded":1914,"clubColors":"Yellow / Green","venue":"Maracana","lastUpdated":"2025-12-01T00:00:00Z","area":{"id":2032,"name":"Brazil","code":"BRA","flag":"br.png"}},{"id":773,"name":"France","shortName":"France","tla":"FRA","crest":"fra.png","address":"Paris","website":"https://fff.example","founded":1919,"clubColors":"Blue","venue":"Maracana","lastUpdated":"2025-12-01T00:00:00Z","area":{"id":2081,"name":"France","code":"FRA","flag":"fr.png"}}]}`
}

func standingsPayload() string {
	return `{"competition":{"id":2000,"name":"FIFA World Cup","code":"WC"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"standings":[{"stage":"GROUP_STAGE","type":"TOTAL","group":"GROUP_A","table":[{"position":1,"team":{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png"},"playedGames":1,"form":"W","won":1,"draw":0,"lost":0,"points":3,"goalsFor":2,"goalsAgainst":0,"goalDifference":2}]}]}`
}

func matchesPayload() string {
	return `{"filters":{"season":"2026"},"resultSet":{"count":1},"matches":[{"id":4001,"utcDate":"2026-06-11T20:00:00Z","status":"SCHEDULED","minute":null,"injuryTime":null,"attendance":null,"venue":"MetLife Stadium","matchday":1,"stage":"GROUP_STAGE","group":"GROUP_A","lastUpdated":"2025-12-01T00:00:00Z","competition":{"id":2000,"name":"FIFA World Cup","code":"WC"},"season":{"id":2026,"startDate":"2026-06-11","endDate":"2026-07-19"},"homeTeam":{"id":764,"name":"Brazil","shortName":"Brazil","tla":"BRA","crest":"bra.png"},"awayTeam":{"id":773,"name":"France","shortName":"France","tla":"FRA","crest":"fra.png"},"score":{"winner":null,"duration":"REGULAR","fullTime":{"home":null,"away":null},"halfTime":{"home":null,"away":null},"regularTime":{"home":null,"away":null},"extraTime":{"home":null,"away":null},"penalties":{"home":null,"away":null}},"referees":[{"id":1,"name":"Ref Name","type":"REFEREE","nationality":"USA"}],"goals":[{"minute":23,"injuryTime":null,"type":"REGULAR","team":{"id":764,"name":"Brazil"},"scorer":{"id":10,"name":"Forward One"},"assist":{"id":12,"name":"Creator"},"score":{"home":1,"away":0}}],"bookings":[{"minute":40,"card":"YELLOW","team":{"id":773,"name":"France"},"player":{"id":11,"name":"Forward Two"}}],"substitutions":[{"minute":65,"team":{"id":764,"name":"Brazil"},"playerOut":{"id":10,"name":"Forward One"},"playerIn":{"id":20,"name":"Fresh Legs"}}]}]}`
}

func teamDetailPayload(teamID int, teamName, tla string, playerID int, playerName string) string {
	return fmt.Sprintf(`{"id":%d,"name":%q,"tla":%q,"squad":[{"id":%d,"name":%q,"firstName":"Forward","lastName":"One","dateOfBirth":"1999-01-02","nationality":%q,"section":"Offence","position":"Attacker","shirtNumber":10,"lastUpdated":"2025-12-01T00:00:00Z"}]}`, teamID, teamName, tla, playerID, playerName, teamName)
}
