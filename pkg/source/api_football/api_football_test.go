package api_football

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
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
		fmt.Fprint(w, `{"get":"teams","parameters":{"league":"1","season":"2026"},"errors":[],"results":1,"paging":{"current":1,"total":1},"response":[{"team":{"id":50,"name":"Brazil","code":"BRA","country":"Brazil","founded":1914,"national":true,"logo":"https://example.com/bra.png"},"venue":{"id":10,"name":"Maracana","address":"Rua","city":"Rio de Janeiro","capacity":78838,"surface":"grass","image":"https://example.com/venue.png"}}]}`)
	}))
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 50, ids.Value(0))
	names := result.Batch.Column(1).(*array.String)
	require.Equal(t, "Brazil", names.Value(0))
}

func TestAPIFootballPlayersPaginates(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/players", r.URL.Path)
		pages = append(pages, r.URL.Query().Get("page"))
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":2},"response":[{"player":{"id":1,"name":"Player One","firstname":"Player","lastname":"One","age":29,"birth":{"date":"1997-01-01","place":"A","country":"B"},"nationality":"B","height":"180 cm","weight":"75 kg","injured":false,"photo":"p1"},"statistics":[{"team":{"id":50,"name":"Brazil","logo":"bra"},"games":{"position":"Attacker","number":10,"captain":true,"appearences":1,"lineups":1,"minutes":90,"rating":"7.2"},"goals":{"total":1,"assists":0,"saves":null},"cards":{"yellow":0,"yellowred":0,"red":0}}]}]}`)
		case "2":
			fmt.Fprint(w, `{"errors":[],"paging":{"current":2,"total":2},"response":[{"player":{"id":2,"name":"Player Two","firstname":"Player","lastname":"Two","age":24,"birth":{"date":"2002-02-02"},"nationality":"C","height":"175 cm","weight":"70 kg","injured":false,"photo":"p2"},"statistics":[]}]}`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "players"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.Equal(t, []string{"1", "2"}, pages)
	require.EqualValues(t, 2, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 1, ids.Value(0))
	require.EqualValues(t, 2, ids.Value(1))
}

func TestAPIFootballMatchesFlattenNestedObjects(t *testing.T) {
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fixtures", r.URL.Path)
		fmt.Fprint(w, fixturesPayload())
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&timezone=America/New_York&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 1001, ids.Value(0))
	venueIDs := result.Batch.Column(7).(*array.Int64)
	require.EqualValues(t, 200, venueIDs.Value(0))
	homeTeamIDs := result.Batch.Column(19).(*array.Int64)
	require.EqualValues(t, 50, homeTeamIDs.Value(0))
}

func TestAPIFootballStadiumsDeriveFromFixturesAndHydrateVenues(t *testing.T) {
	var requested []string
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/fixtures":
			fmt.Fprint(w, fixturesPayload())
		case "/venues":
			require.Equal(t, "200", r.URL.Query().Get("id"))
			fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":1},"response":[{"id":200,"name":"MetLife Stadium","address":"1 MetLife Stadium Dr","city":"East Rutherford","country":"USA","capacity":82500,"surface":"grass","image":"stadium.png"}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "stadiums"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.Len(t, requested, 2)
	require.EqualValues(t, 1, result.Batch.NumRows())
	names := result.Batch.Column(1).(*array.String)
	require.Equal(t, "MetLife Stadium", names.Value(0))
}

func TestAPIFootballMatchEventsFanOutFromFixtures(t *testing.T) {
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fixtures":
			fmt.Fprint(w, fixturesPayload())
		case "/fixtures/events":
			require.Equal(t, "1001", r.URL.Query().Get("fixture"))
			fmt.Fprint(w, `{"errors":[],"paging":{"current":1,"total":1},"response":[{"time":{"elapsed":23,"extra":null},"team":{"id":50,"name":"Brazil","logo":"bra"},"player":{"id":9,"name":"Forward"},"assist":{"id":10,"name":"Creator"},"type":"Goal","detail":"Normal Goal","comments":null}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer server.Close()

	src := NewAPIFootballSource()
	require.NoError(t, src.Connect(context.Background(), "api-football://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "match_events"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	fixtureIDs := result.Batch.Column(1).(*array.Int64)
	require.EqualValues(t, 1001, fixtureIDs.Value(0))
	eventTypes := result.Batch.Column(12).(*array.String)
	require.Equal(t, "Goal", eventTypes.Value(0))
}

func TestAPIFootballUnsupportedTable(t *testing.T) {
	src := NewAPIFootballSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "odds"})
	require.ErrorContains(t, err, "unsupported table")
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
