package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
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

func TestESPNHandlesIncrementality(t *testing.T) {
	src := NewESPNSource()
	require.True(t, src.HandlesIncrementality())
}

func TestESPNTableDefaults(t *testing.T) {
	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://"))

	want := map[string]struct {
		pks      []string
		strategy config.IncrementalStrategy
	}{
		"teams":       {[]string{"id"}, config.StrategyReplace},
		"scoreboard":  {[]string{"id"}, config.StrategyMerge},
		"competitors": {[]string{"event_id", "competition_id", "team_id"}, config.StrategyMerge},
		"standings":   {[]string{"league_id", "group_id", "season", "team_id"}, config.StrategyReplace},
		"news":        {[]string{"id"}, config.StrategyMerge},
	}
	for name, expected := range want {
		table, err := src.GetTable(context.Background(), source.TableRequest{Name: name})
		require.NoError(t, err, "GetTable(%s)", name)
		require.Equal(t, expected.pks, table.PrimaryKeys(), "%s primary keys", name)
		require.Equal(t, expected.strategy, table.Strategy(), "%s strategy", name)
		require.False(t, table.HasKnownSchema(), "%s should not have a known schema (uses schema inference)", name)
	}
}

func TestESPNEventsTableRemoved(t *testing.T) {
	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://"))
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "events"})
	require.ErrorContains(t, err, "unsupported table")
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
	require.Equal(t, "22", stringAt(t, result.Batch, "id", 0))
	require.Equal(t, "Arizona Cardinals", stringAt(t, result.Batch, "displayName", 0))
	require.Equal(t, "1", stringAt(t, result.Batch, "id", 1))
}

func TestESPNScoreboardEmitsRawEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/apis/site/v2/sports/football/nfl/scoreboard", r.URL.Path)
		require.Equal(t, "20260910-20260912", r.URL.Query().Get("dates"))
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		_, _ = fmt.Fprint(w, scoreboardPayload())
	}))
	defer server.Close()

	src := NewESPNSource()
	require.NoError(t, src.Connect(context.Background(), "espn://?dates=20260910-20260912&base_url="+url.QueryEscape(server.URL)))
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "scoreboard"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{Limit: 1})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	require.Equal(t, "401872656", stringAt(t, result.Batch, "id", 0))
	require.Equal(t, "New England Patriots at Seattle Seahawks", stringAt(t, result.Batch, "name", 0))
	// nested competitions should still be present (as JSON) — schema inference will retype later.
	require.True(t, hasField(result.Batch, "competitions"), "competitions field should exist")
}

func TestESPNCompetitorsFanOutAndCarryPKs(t *testing.T) {
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
	require.Equal(t, "401872656", stringAt(t, result.Batch, "event_id", 0))
	require.Equal(t, "401872656", stringAt(t, result.Batch, "competition_id", 0))
	require.Equal(t, "26", stringAt(t, result.Batch, "team_id", 0))
	require.Equal(t, "17", stringAt(t, result.Batch, "team_id", 1))
	require.Equal(t, "home", stringAt(t, result.Batch, "homeAway", 0))
	require.Equal(t, "away", stringAt(t, result.Batch, "homeAway", 1))
}

func TestESPNStandingsWalksChildrenAndCarriesPKs(t *testing.T) {
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
	require.Equal(t, "9", stringAt(t, result.Batch, "league_id", 0))
	require.Equal(t, "8", stringAt(t, result.Batch, "group_id", 0))
	require.Equal(t, "17", stringAt(t, result.Batch, "team_id", 0))
	// season is a number in the payload, schema inference will store it as integer JSON.
	require.True(t, hasField(result.Batch, "season"))
	// stats array preserved
	require.True(t, hasField(result.Batch, "stats"))
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
	require.Equal(t, "Are the Cowboys legit contenders this season?", stringAt(t, result.Batch, "headline", 0))
	// raw nested links preserved
	require.True(t, hasField(result.Batch, "links"))
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

// hasField reports whether the record's schema contains the given field name.
func hasField(batch arrow.RecordBatch, name string) bool {
	for _, f := range batch.Schema().Fields() {
		if f.Name == name {
			return true
		}
	}
	return false
}

// stringAt returns the value at (column, row) as a Go string, regardless of how
// it is stored. Values produced by ItemsToArrowRecordWithSchema(nil) use the
// "unknown" Arrow extension type whose storage is a string of JSON-encoded data.
// We unwrap the extension, then strip the outer quotes for JSON string values so
// callers can compare against natural string literals.
func stringAt(t *testing.T, batch arrow.RecordBatch, name string, row int) string {
	t.Helper()
	for i, f := range batch.Schema().Fields() {
		if f.Name != name {
			continue
		}
		col := batch.Column(i)
		if ext, ok := col.(array.ExtensionArray); ok {
			storage, sok := ext.Storage().(*array.String)
			require.True(t, sok, "unknown ext storage should be *array.String")
			raw := storage.Value(row)
			if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
				var s string
				require.NoError(t, json.Unmarshal([]byte(raw), &s))
				return s
			}
			return raw
		}
		switch a := col.(type) {
		case *array.String:
			return a.Value(row)
		case *array.LargeString:
			return a.Value(row)
		default:
			return a.ValueStr(row)
		}
	}
	t.Fatalf("column %q not found in schema", name)
	return ""
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
