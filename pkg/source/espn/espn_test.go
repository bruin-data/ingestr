package espn

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

func TestESPNParseURI(t *testing.T) {
	cfg, err := parseURI("espn://")
	require.NoError(t, err)
	require.Equal(t, defaultSport, cfg.sport)
	require.Equal(t, defaultLeague, cfg.league)
	require.Equal(t, defaultLimit, cfg.limit)
	require.Equal(t, defaultBaseURL, cfg.baseURL)

	cfg, err = parseURI("espn://?sport=basketball&league=nba&dates=20260101-20260131&season=2026&limit=25")
	require.NoError(t, err)
	require.Equal(t, "basketball", cfg.sport)
	require.Equal(t, "nba", cfg.league)
	require.Equal(t, "20260101-20260131", cfg.dates)
	require.Equal(t, "2026", cfg.season)
	require.Equal(t, 25, cfg.limit)

	_, err = parseURI("espn://?limit=zero")
	require.ErrorContains(t, err, "limit")
}

func TestESPNReadTeams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/site/v2/sports/football/nfl/teams", r.URL.Path)
		_, _ = fmt.Fprint(w, teamsPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?base_url="+url.QueryEscape(server.URL)))
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
	require.EqualValues(t, 22, ids.Value(0))
	names := result.Batch.Column(5).(*array.String)
	require.Equal(t, "Arizona Cardinals", names.Value(0))
}

func TestESPNScoreboardUsesDatesAndFlattensEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/site/v2/sports/football/nfl/scoreboard", r.URL.Path)
		require.Equal(t, "20260910-20260912", r.URL.Query().Get("dates"))
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		_, _ = fmt.Fprint(w, scoreboardPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?dates=20260910-20260912&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "events"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{Limit: 1})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	eventIDs := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 401872656, eventIDs.Value(0))
	homeTeamIDs := result.Batch.Column(14).(*array.Int64)
	require.EqualValues(t, 26, homeTeamIDs.Value(0))
	awayScores := result.Batch.Column(21).(*array.Int64)
	require.EqualValues(t, 0, awayScores.Value(0))
}

func TestESPNCompetitorsFanOutFromScoreboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/site/v2/sports/football/nfl/scoreboard", r.URL.Path)
		_, _ = fmt.Fprint(w, scoreboardPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "competitors"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 2, result.Batch.NumRows())
	teamIDs := result.Batch.Column(2).(*array.Int64)
	require.EqualValues(t, 26, teamIDs.Value(0))
	require.EqualValues(t, 17, teamIDs.Value(1))
	homeAway := result.Batch.Column(6).(*array.String)
	require.Equal(t, "home", homeAway.Value(0))
	require.Equal(t, "away", homeAway.Value(1))
}

func TestESPNStandingsWalksChildren(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/v2/sports/football/nfl/standings", r.URL.Path)
		require.Equal(t, "2025", r.URL.Query().Get("season"))
		_, _ = fmt.Fprint(w, standingsPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?season=2025&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "standings"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	teamIDs := result.Batch.Column(8).(*array.Int64)
	require.EqualValues(t, 17, teamIDs.Value(0))
	wins := result.Batch.Column(14).(*array.Float64)
	require.EqualValues(t, 14, wins.Value(0))
	streak := result.Batch.Column(22).(*array.String)
	require.Equal(t, "W3", streak.Value(0))
}

func TestESPNNews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/site/v2/sports/football/nfl/news", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		_, _ = fmt.Fprint(w, newsPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?limit=1&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "news"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	headlines := result.Batch.Column(4).(*array.String)
	require.Equal(t, "Are the Cowboys legit contenders this season?", headlines.Value(0))
	links := result.Batch.Column(10).(*array.String)
	require.Equal(t, "https://www.espn.com/video/clip/_/id/49094173/test", links.Value(0))
}

func TestESPNRegistryLookup(t *testing.T) {
	constructor, err := registry.Default.GetSourceConstructor("espn")
	require.NoError(t, err)
	src, ok := constructor().(source.Source)
	require.True(t, ok)
	require.Contains(t, src.Schemes(), "espn")
}

func TestESPNUnsupportedTable(t *testing.T) {
	src := NewESPNSource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "roster"})
	require.ErrorContains(t, err, "unsupported table")
}

func teamsPayload() string {
	return `{"sports":[{"id":"20","leagues":[{"id":"28","name":"National Football League","teams":[{"team":{"id":"22","uid":"s:20~l:28~t:22","slug":"arizona-cardinals","abbreviation":"ARI","name":"Cardinals","displayName":"Arizona Cardinals","shortDisplayName":"Cardinals","location":"Arizona","nickname":"Cardinals","color":"a40227","alternateColor":"ffffff","isActive":true,"isAllStar":false,"logos":[{"href":"https://example.com/ari.png"}],"links":[{"href":"https://example.com/ari"}]}},{"team":{"id":"1","uid":"s:20~l:28~t:1","slug":"atlanta-falcons","abbreviation":"ATL","name":"Falcons","displayName":"Atlanta Falcons","shortDisplayName":"Falcons","location":"Atlanta","nickname":"Falcons","isActive":true,"logos":[{"href":"https://example.com/atl.png"}]}}]}]}]}`
}

func scoreboardPayload() string {
	return `{"events":[{"id":"401872656","uid":"s:20~l:28~e:401872656","date":"2026-09-10T00:20Z","name":"New England Patriots at Seattle Seahawks","shortName":"NE @ SEA","season":{"year":2026,"type":2},"week":{"number":1},"competitions":[{"id":"401872656","date":"2026-09-10T00:20Z","venue":{"id":"3673","fullName":"Lumen Field"},"status":{"type":{"id":"1","name":"STATUS_SCHEDULED","state":"pre","completed":false}},"competitors":[{"id":"26","uid":"s:20~l:28~t:26","type":"team","order":0,"homeAway":"home","winner":false,"score":"0","curatedRank":99,"team":{"id":"26","uid":"s:20~l:28~t:26","location":"Seattle","name":"Seahawks","abbreviation":"SEA","displayName":"Seattle Seahawks","shortDisplayName":"Seahawks","color":"002a5c","alternateColor":"69be28","logo":"https://example.com/sea.png"},"records":[{"summary":"0-0"}],"statistics":[]},{"id":"17","uid":"s:20~l:28~t:17","type":"team","order":1,"homeAway":"away","winner":false,"score":"0","curatedRank":99,"team":{"id":"17","uid":"s:20~l:28~t:17","location":"New England","name":"Patriots","abbreviation":"NE","displayName":"New England Patriots","shortDisplayName":"Patriots","color":"002a5c","alternateColor":"c60c30","logo":"https://example.com/ne.png"},"records":[{"summary":"0-0"}],"statistics":[]}]}]}]}`
}

func standingsPayload() string {
	return `{"uid":"s:20~l:28~g:9","id":"9","name":"National Football League","abbreviation":"NFL","children":[{"uid":"s:20~l:28~g:8","id":"8","name":"American Football Conference","abbreviation":"AFC","standings":{"season":2025,"seasonType":2,"entries":[{"team":{"id":"17","uid":"s:20~l:28~t:17","displayName":"New England Patriots","abbreviation":"NE"},"stats":[{"name":"wins","value":14.0,"displayValue":"14"},{"name":"losses","value":3.0,"displayValue":"3"},{"name":"ties","value":0.0,"displayValue":"0"},{"name":"winPercent","value":0.8235294,"displayValue":".824"},{"name":"pointsFor","value":490.0,"displayValue":"490"},{"name":"pointsAgainst","value":320.0,"displayValue":"320"},{"name":"pointDifferential","value":170.0,"displayValue":"+170"},{"name":"playoffSeed","value":2.0,"displayValue":"2"},{"name":"gamesBehind","value":0.0,"displayValue":"-"},{"name":"streak","value":3.0,"displayValue":"W3"},{"id":"0","name":"overall","type":"total","summary":"14-3","displayValue":"14-3"}]}]}}]}`
}

func newsPayload() string {
	return `{"articles":[{"id":49094173,"nowId":"1-49094173","contentKey":"49094173-1-293-1","type":"Media","headline":"Are the Cowboys legit contenders this season?","description":"Are the Cowboys legit contenders this season?","lastModified":"2026-06-17T13:21:31Z","published":"2026-06-17T13:21:31Z","premium":false,"byline":"ESPN","images":[{"url":"https://example.com/image.jpg"}],"categories":[{"type":"league","description":"NFL"}],"links":{"web":{"href":"https://www.espn.com/video/clip/_/id/49094173/test"}}}]}`
}
