package predictionmarkets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURI(t *testing.T) {
	values, err := ParseURI("manifold://?userId=abc&limit=10", "manifold")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := values.Get("userId"); got != "abc" {
		t.Fatalf("userId = %q, want abc", got)
	}
	if got := values.Get("limit"); got != "10" {
		t.Fatalf("limit = %q, want 10", got)
	}

	if _, err := ParseURI("kalshi://?ticker=ABC", "polymarket"); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestFetchRowsUsesIntervalParams(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"1","createdTime":1700000000000}]}`))
	}))
	defer server.Close()

	api := &JSONAPISource{
		Scheme: "test",
		Params: map[string][]string{},
		Client: NewClient(server.URL, 0, 0),
	}
	defer func() { _ = api.Close(context.Background()) }()

	start := time.Unix(1700000000, 0).UTC()
	end := start.Add(time.Hour)
	spec := TableSpec{
		Name:               "items",
		Path:               "/items",
		ResultPath:         []string{"items"},
		Columns:            []schema.Column{{Name: "id", DataType: schema.TypeString, Nullable: true}},
		LimitParam:         "limit",
		LimitDefault:       100,
		IntervalStartParam: "afterTime",
		IntervalEndParam:   "beforeTime",
		IntervalUnixMillis: true,
	}

	rows, err := api.FetchRows(context.Background(), spec, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end, Limit: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rawQuery != "afterTime=1700000000000&beforeTime=1700003600000&limit=1" {
		t.Fatalf("query = %q", rawQuery)
	}
}

func TestFetchRowsRequiresParamsAndIntervals(t *testing.T) {
	api := &JSONAPISource{
		Scheme: "test",
		Params: map[string][]string{},
		Client: NewClient("http://127.0.0.1", 0, 0),
	}

	_, err := api.FetchRows(context.Background(), TableSpec{
		Name:           "orderbook",
		Path:           "/book",
		RequiredParams: []string{"token_id"},
	}, source.ReadOptions{})
	if err == nil {
		t.Fatal("expected missing required param error")
	}

	api.Params = map[string][]string{"token_id": {"abc"}}
	_, err = api.FetchRows(context.Background(), TableSpec{
		Name:               "candles",
		Path:               "/candles",
		RequiredParams:     []string{"token_id"},
		IntervalStartParam: "start_ts",
		IntervalEndParam:   "end_ts",
		RequireInterval:    true,
	}, source.ReadOptions{})
	if err == nil {
		t.Fatal("expected missing interval error")
	}
}
