package balldontlie_fifa

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

func TestBallDontLieFIFAMissingAPIKey(t *testing.T) {
	src := NewBallDontLieFIFASource()
	err := src.Connect(context.Background(), "balldontlie-fifa://")
	require.ErrorContains(t, err, "api_key")
}

func TestBallDontLieFIFARejectsInvalidSeason(t *testing.T) {
	src := NewBallDontLieFIFASource()
	err := src.Connect(context.Background(), "balldontlie-fifa://?api_key=test&season=2030")
	require.ErrorContains(t, err, "season")
}

func TestBallDontLieFIFAReadTeamsUsesAuthSeasonAndPagination(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/teams", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("Authorization"))
		require.Equal(t, "2026", r.URL.Query().Get("seasons[]"))
		requests = append(requests, r.URL.RawQuery)

		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprint(w, `{"data":[{"id":1,"name":"Argentina","abbreviation":"ARG","country_code":"ARG","confederation":"CONMEBOL"}],"meta":{"next_cursor":2,"per_page":1}}`)
		case "2":
			fmt.Fprint(w, `{"data":[{"id":2,"name":"Brazil","abbreviation":"BRA","country_code":"BRA","confederation":"CONMEBOL"}],"meta":{"next_cursor":null,"per_page":1}}`)
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	src := NewBallDontLieFIFASource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie-fifa://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "teams"})
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, table.PrimaryKeys())

	results, err := table.Read(context.Background(), source.ReadOptions{PageSize: 1})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.Len(t, requests, 2)
	require.EqualValues(t, 2, result.Batch.NumRows())
	ids := result.Batch.Column(0).(*array.Int64)
	require.EqualValues(t, 1, ids.Value(0))
	require.EqualValues(t, 2, ids.Value(1))
}

func TestBallDontLieFIFAReadMatchesFlattensNestedObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/fifa/worldcup/v1/matches", r.URL.Path)
		fmt.Fprint(w, `{"data":[{"id":11,"match_number":11,"datetime":"2026-06-14T21:00:00Z","status":"scheduled","season":{"id":3,"year":2026},"stage":{"id":1,"name":"Group Stage","order":1},"group":{"id":6,"name":"F"},"stadium":{"id":4,"name":"AT&T Stadium","city":"Dallas","country":"USA"},"home_team":{"id":21,"name":"Netherlands","abbreviation":"NED"},"away_team":null,"away_team_source":{"placeholder":"Runner-up Group C"},"home_score":null,"away_score":null,"has_extra_time":false,"has_penalty_shootout":false}],"meta":{"next_cursor":null}}`)
	}))
	defer server.Close()

	src := NewBallDontLieFIFASource()
	require.NoError(t, src.Connect(context.Background(), "balldontlie-fifa://?api_key=test-key&base_url="+url.QueryEscape(server.URL)))

	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "matches"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	result := <-results
	require.NoError(t, result.Err)
	defer result.Batch.Release()

	require.EqualValues(t, 1, result.Batch.NumRows())
	seasonYear := result.Batch.Column(5).(*array.Int64)
	require.EqualValues(t, 2026, seasonYear.Value(0))
	homeTeamID := result.Batch.Column(15).(*array.Int64)
	require.EqualValues(t, 21, homeTeamID.Value(0))
	awayTeamID := result.Batch.Column(18).(*array.Int64)
	require.True(t, awayTeamID.IsNull(0))
}

func TestBallDontLieFIFAUnsupportedTable(t *testing.T) {
	src := NewBallDontLieFIFASource()
	_, err := src.GetTable(context.Background(), source.TableRequest{Name: "odds"})
	require.ErrorContains(t, err, "unsupported table")
}
