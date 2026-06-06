package predictionmarkets

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchRowsDoesNotApplyDefaultPageCap(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests <= 12 {
			_, _ = fmt.Fprintf(w, `[{"id":"%d"}]`, requests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	api := &JSONAPISource{
		Scheme: "test",
		Params: map[string][]string{},
		Client: NewClient(server.URL, 0, 0),
	}
	defer func() { _ = api.Close(context.Background()) }()

	rows, err := api.FetchRows(context.Background(), TableSpec{
		Name:         "items",
		Path:         "/items",
		Columns:      []schema.Column{{Name: "id", DataType: schema.TypeString, Nullable: true}},
		Pagination:   PaginationOffset,
		LimitParam:   "limit",
		LimitDefault: 1,
		OffsetParam:  "offset",
	}, source.ReadOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("got %d rows, want 12", len(rows))
	}
	if requests != 13 {
		t.Fatalf("got %d requests, want 13", requests)
	}
}

func TestFetchRowsErrorsWhenExplicitMaxPagesReached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1"}]`))
	}))
	defer server.Close()

	api := &JSONAPISource{
		Scheme: "test",
		Params: map[string][]string{},
		Client: NewClient(server.URL, 0, 0),
	}
	defer func() { _ = api.Close(context.Background()) }()

	_, err := api.FetchRows(context.Background(), TableSpec{
		Name:         "items",
		Path:         "/items",
		Columns:      []schema.Column{{Name: "id", DataType: schema.TypeString, Nullable: true}},
		Pagination:   PaginationOffset,
		LimitParam:   "limit",
		LimitDefault: 1,
		OffsetParam:  "offset",
		MaxPages:     2,
	}, source.ReadOptions{})
	if err == nil {
		t.Fatal("expected max page limit error")
	}
	if !strings.Contains(err.Error(), "reached max page limit of 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildQuerySkipsPathParams(t *testing.T) {
	api := &JSONAPISource{
		Scheme: "test",
		Params: map[string][]string{
			"ticker": {"ABC"},
			"depth":  {"10"},
		},
	}

	query := api.buildQuery(TableSpec{
		Name:           "orderbook",
		Path:           "/markets/{ticker}/orderbook",
		QueryParams:    []string{"ticker", "depth"},
		RequiredParams: []string{"ticker"},
	}, source.ReadOptions{})

	if got := query.Get("ticker"); got != "" {
		t.Fatalf("ticker query param = %q, want empty", got)
	}
	if got := query.Get("depth"); got != "10" {
		t.Fatalf("depth query param = %q, want 10", got)
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
