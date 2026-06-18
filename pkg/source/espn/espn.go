package espn

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBaseURL = "https://site.api.espn.com"
	defaultSport   = "football"
	defaultLeague  = "nfl"
	defaultLimit   = 100
)

var supportedTables = []string{"teams", "scoreboard", "competitors", "standings", "news"}

type tableConfig struct {
	primaryKeys []string
	strategy    config.IncrementalStrategy
	fetch       func(context.Context, source.ReadOptions) ([]map[string]interface{}, error)
}

type ESPNSource struct {
	client  *httpclient.Client
	sport   string
	league  string
	dates   string
	season  string
	limit   int
	baseURL string
}

func NewESPNSource() *ESPNSource {
	return &ESPNSource{}
}

func (s *ESPNSource) Schemes() []string {
	return []string{"espn"}
}

func (s *ESPNSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.sport = cfg.sport
	s.league = cfg.league
	s.dates = cfg.dates
	s.season = cfg.season
	s.limit = cfg.limit
	s.baseURL = cfg.baseURL
	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(10, 5),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

type uriConfig struct {
	sport   string
	league  string
	dates   string
	season  string
	limit   int
	baseURL string
}

func parseURI(raw string) (uriConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return uriConfig{}, fmt.Errorf("failed to parse espn URI: %w", err)
	}
	if parsed.Scheme != "espn" {
		return uriConfig{}, fmt.Errorf("invalid espn URI: must start with espn://")
	}

	values := parsed.Query()
	cfg := uriConfig{
		sport:   strings.TrimSpace(values.Get("sport")),
		league:  strings.TrimSpace(values.Get("league")),
		dates:   strings.TrimSpace(values.Get("dates")),
		season:  strings.TrimSpace(values.Get("season")),
		limit:   defaultLimit,
		baseURL: strings.TrimRight(values.Get("base_url"), "/"),
	}
	if cfg.sport == "" {
		cfg.sport = defaultSport
	}
	if cfg.league == "" {
		cfg.league = defaultLeague
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
	}
	if limit := values.Get("limit"); limit != "" {
		parsedLimit, err := strconv.Atoi(limit)
		if err != nil || parsedLimit <= 0 {
			return uriConfig{}, fmt.Errorf("invalid espn URI: limit must be a positive integer")
		}
		cfg.limit = parsedLimit
	}
	return cfg, nil
}

func (s *ESPNSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// HandlesIncrementality returns true because the source maps IntervalStart/End
// onto the ESPN `dates` query parameter (see scoreboardDates) — the API itself
// performs the time-window filtering, so the pipeline must not try to filter again.
func (s *ESPNSource) HandlesIncrementality() bool {
	return true
}

func (s *ESPNSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tables := s.tables()
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: %s", req.Name, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: cfg.primaryKeys,
		TableStrategy:    cfg.strategy,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("espn source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func (s *ESPNSource) tables() map[string]tableConfig {
	return map[string]tableConfig{
		"teams": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyReplace,
			fetch:       s.fetchTeams,
		},
		"scoreboard": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyMerge,
			fetch:       s.fetchScoreboard,
		},
		"competitors": {
			primaryKeys: []string{"event_id", "competition_id", "team_id"},
			strategy:    config.StrategyMerge,
			fetch:       s.fetchCompetitors,
		},
		"standings": {
			primaryKeys: []string{"league_id", "group_id", "season", "team_id"},
			strategy:    config.StrategyReplace,
			fetch:       s.fetchStandings,
		},
		"news": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyMerge,
			fetch:       s.fetchNews,
		},
	}
}

func (s *ESPNSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		items, err := cfg.fetch(ctx, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert espn data to Arrow: %w", err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results, nil
}

func (s *ESPNSource) fetchTeams(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, sitePath(s.sport, s.league, "teams"), nil)
	if err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	for _, sport := range interfaceSlice(payload["sports"]) {
		sportObj, ok := sport.(map[string]interface{})
		if !ok {
			continue
		}
		for _, league := range interfaceSlice(sportObj["leagues"]) {
			leagueObj, ok := league.(map[string]interface{})
			if !ok {
				continue
			}
			for _, rawTeam := range interfaceSlice(leagueObj["teams"]) {
				teamItem, ok := rawTeam.(map[string]interface{})
				if !ok {
					continue
				}
				out = append(out, nestedMap(teamItem, "team"))
				if reachedLimit(out, opts) {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

func (s *ESPNSource) fetchScoreboard(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.fetchScoreboardPayload(ctx, opts)
	if err != nil {
		return nil, err
	}

	events, err := extractEvents(payload)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		out = append(out, event)
		if reachedLimit(out, opts) {
			return out, nil
		}
	}
	return out, nil
}

func (s *ESPNSource) fetchCompetitors(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.fetchScoreboardPayload(ctx, opts)
	if err != nil {
		return nil, err
	}

	events, err := extractEvents(payload)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0)
	for _, event := range events {
		for _, competition := range interfaceSlice(event["competitions"]) {
			competitionObj, ok := competition.(map[string]interface{})
			if !ok {
				continue
			}
			for _, competitor := range interfaceSlice(competitionObj["competitors"]) {
				competitorObj, ok := competitor.(map[string]interface{})
				if !ok {
					continue
				}
				row := cloneMap(competitorObj)
				row["event_id"] = event["id"]
				row["competition_id"] = competitionObj["id"]
				row["team_id"] = competitorObj["id"]
				out = append(out, row)
				if reachedLimit(out, opts) {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

func (s *ESPNSource) fetchStandings(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	params := map[string]string{}
	if s.season != "" {
		params["season"] = s.season
	}
	payload, err := s.get(ctx, fmt.Sprintf("/apis/v2/sports/%s/%s/standings", s.sport, s.league), params)
	if err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	walkStandingsGroup(payload, payload, &out, opts)
	return out, nil
}

func (s *ESPNSource) fetchNews(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	limit := s.queryLimit(opts)
	payload, err := s.get(ctx, sitePath(s.sport, s.league, "news"), map[string]string{"limit": strconv.Itoa(limit)})
	if err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	for _, rawArticle := range interfaceSlice(payload["articles"]) {
		article, ok := rawArticle.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, article)
		if reachedLimit(out, opts) {
			return out, nil
		}
	}
	return out, nil
}

func (s *ESPNSource) fetchScoreboardPayload(ctx context.Context, opts source.ReadOptions) (map[string]interface{}, error) {
	params := map[string]string{"limit": strconv.Itoa(s.queryLimit(opts))}
	if dates := s.scoreboardDates(opts); dates != "" {
		params["dates"] = dates
	}
	if s.season != "" {
		params["season"] = s.season
	}
	return s.get(ctx, sitePath(s.sport, s.league, "scoreboard"), params)
}

func (s *ESPNSource) scoreboardDates(opts source.ReadOptions) string {
	if s.dates != "" {
		return s.dates
	}
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return ""
	}
	if opts.IntervalStart != nil && opts.IntervalEnd != nil {
		return opts.IntervalStart.UTC().Format("20060102") + "-" + opts.IntervalEnd.UTC().Format("20060102")
	}
	if opts.IntervalStart != nil {
		return opts.IntervalStart.UTC().Format("20060102")
	}
	return opts.IntervalEnd.UTC().Format("20060102")
}

func (s *ESPNSource) queryLimit(opts source.ReadOptions) int {
	if opts.Limit > 0 && opts.Limit < s.limit {
		return opts.Limit
	}
	return s.limit
}

func (s *ESPNSource) get(ctx context.Context, endpoint string, params map[string]string) (map[string]interface{}, error) {
	if s.client == nil {
		return nil, fmt.Errorf("espn source is not connected")
	}

	var payload map[string]interface{}
	req := s.client.R(ctx).SetResult(&payload)
	for key, value := range params {
		if value != "" {
			req.SetQueryParam(key, value)
		}
	}

	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch espn endpoint %s: %w", endpoint, err)
	}
	if err := checkResponse(endpoint, resp); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		if err := resp.JSON(&payload); err != nil {
			return nil, fmt.Errorf("malformed espn response from %s: %w", endpoint, err)
		}
	}
	return normalizeMap(payload), nil
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("espn API access failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusTooManyRequests:
		return fmt.Errorf("espn API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("espn API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

func sitePath(sport, league, resource string) string {
	return fmt.Sprintf("/apis/site/v2/sports/%s/%s/%s", sport, league, resource)
}

func extractEvents(payload map[string]interface{}) ([]map[string]interface{}, error) {
	raw := interfaceSlice(payload["events"])
	if raw == nil {
		return nil, fmt.Errorf("missing events array")
	}
	events := make([]map[string]interface{}, 0, len(raw))
	for i, rawEvent := range raw {
		event, ok := rawEvent.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("event item %d is not an object", i)
		}
		events = append(events, event)
	}
	return events, nil
}

func walkStandingsGroup(root, group map[string]interface{}, out *[]map[string]interface{}, opts source.ReadOptions) {
	standings := nestedMap(group, "standings")
	for _, rawEntry := range interfaceSlice(standings["entries"]) {
		if reachedLimit(*out, opts) {
			return
		}
		entry, ok := rawEntry.(map[string]interface{})
		if !ok {
			continue
		}
		row := cloneMap(entry)
		row["league_id"] = root["id"]
		row["group_id"] = group["id"]
		row["season"] = standings["season"]
		row["team_id"] = nestedMap(entry, "team")["id"]
		*out = append(*out, row)
	}
	for _, rawChild := range interfaceSlice(group["children"]) {
		if reachedLimit(*out, opts) {
			return
		}
		child, ok := rawChild.(map[string]interface{})
		if !ok {
			continue
		}
		walkStandingsGroup(root, child, out, opts)
	}
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func nestedMap(item map[string]interface{}, key string) map[string]interface{} {
	raw, ok := item[key].(map[string]interface{})
	if !ok || raw == nil {
		return map[string]interface{}{}
	}
	return raw
}

func interfaceSlice(value interface{}) []interface{} {
	raw, ok := value.([]interface{})
	if !ok {
		return nil
	}
	return raw
}

func normalizeMap(item map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(item))
	for key, value := range item {
		out[key] = normalizeValue(value)
	}
	return out
}

func normalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "null") {
			return nil
		}
		return v
	case map[string]interface{}:
		return normalizeMap(v)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = normalizeValue(item)
		}
		return out
	default:
		return value
	}
}

func reachedLimit(items []map[string]interface{}, opts source.ReadOptions) bool {
	return opts.Limit > 0 && len(items) >= opts.Limit
}
