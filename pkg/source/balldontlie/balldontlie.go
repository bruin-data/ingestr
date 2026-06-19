package balldontlie

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
	defaultBaseURL = "https://api.balldontlie.io"
	defaultSeason  = "2026"
	defaultPerPage = 100
	// BallDontLie's free tier allows 5 req/min; rateLimit is ~80% of that per second.
	rateLimit      = 0.066
	rateLimitBurst = 2
)

type tableConfig struct {
	endpoint    string
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

type BallDontLieSource struct {
	client  *httpclient.Client
	apiKey  string
	season  string
	baseURL string
}

func NewBallDontLieSource() *BallDontLieSource {
	return &BallDontLieSource{}
}

func (s *BallDontLieSource) Schemes() []string {
	return []string{"balldontlie"}
}

func (s *BallDontLieSource) Connect(ctx context.Context, uri string) error {
	apiKey, season, baseURL, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.season = season
	s.baseURL = baseURL
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithHeader("Authorization", apiKey),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func parseURI(raw string) (apiKey, season, baseURL string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse balldontlie URI: %w", err)
	}
	if parsed.Scheme != "balldontlie" {
		return "", "", "", fmt.Errorf("invalid balldontlie URI: must start with balldontlie://")
	}

	values := parsed.Query()
	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", "", fmt.Errorf("invalid balldontlie URI: api_key query parameter is required")
	}

	season = values.Get("season")
	if season == "" {
		season = defaultSeason
	}
	switch season {
	case "2018", "2022", "2026":
	default:
		return "", "", "", fmt.Errorf("invalid balldontlie URI: season must be one of 2018, 2022, 2026")
	}

	baseURL = strings.TrimRight(values.Get("base_url"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return apiKey, season, baseURL, nil
}

func (s *BallDontLieSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *BallDontLieSource) HandlesIncrementality() bool {
	return true
}

func (s *BallDontLieSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: teams, stadiums, group_standings, matches, players, rosters, match_lineups, match_events, player_match_stats, team_match_stats, match_shots, match_momentum, match_best_players, match_avg_positions, match_team_form", req.Name)
	}

	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: cfg.primaryKeys,
		TableStrategy:    cfg.strategy,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("balldontlie source relies on schema inference")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func (s *BallDontLieSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)
		if err := s.stream(ctx, cfg, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// stream paginates the endpoint and sends one Arrow batch per page to the
// results channel, rather than accumulating every page into a single slice.
func (s *BallDontLieSource) stream(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("balldontlie source is not connected")
	}
	config.Debug("[BALLDONTLIE] reading %s", cfg.endpoint)

	perPage := defaultPerPage
	if opts.PageSize > 0 && opts.PageSize < perPage {
		perPage = opts.PageSize
	}

	var cursor string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var payload map[string]interface{}
		req := s.client.R(ctx).
			SetQueryParam("seasons[]", s.season).
			SetQueryParam("per_page", strconv.Itoa(perPage)).
			SetResult(&payload)
		if cursor != "" {
			req.SetQueryParam("cursor", cursor)
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch balldontlie endpoint %s: %w", cfg.endpoint, err)
		}
		if err := checkResponse(cfg.endpoint, resp); err != nil {
			return err
		}
		if len(payload) == 0 {
			if err := resp.JSON(&payload); err != nil {
				return fmt.Errorf("malformed balldontlie response from %s: %w", cfg.endpoint, err)
			}
		}

		pageItems, err := extractData(payload)
		if err != nil {
			return fmt.Errorf("malformed balldontlie response from %s: %w", cfg.endpoint, err)
		}
		if err := sendBatch(pageItems, opts, results); err != nil {
			return err
		}

		nextCursor := extractNextCursor(payload)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return nil
}

// sendBatch converts a page of items to an Arrow record and streams it to the
// results channel. Empty pages are skipped so no zero-row batch is emitted.
func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert balldontlie data to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("balldontlie API authentication or plan access failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusTooManyRequests:
		return fmt.Errorf("balldontlie API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("balldontlie API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

func extractData(payload map[string]interface{}) ([]map[string]interface{}, error) {
	raw, ok := payload["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("missing data array")
	}
	items := make([]map[string]interface{}, 0, len(raw))
	for i, rawItem := range raw {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("data item %d is not an object", i)
		}
		items = append(items, normalizeMap(item))
	}
	return items, nil
}

func extractNextCursor(payload map[string]interface{}) string {
	meta, ok := payload["meta"].(map[string]interface{})
	if !ok {
		return ""
	}
	next, ok := meta["next_cursor"]
	if !ok || next == nil {
		return ""
	}
	switch v := next.(type) {
	case string:
		return v
	case float64:
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(int64(v), 10)
	case int:
		if v == 0 {
			return ""
		}
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
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

var tables = map[string]tableConfig{
	"teams": {
		endpoint:    "/fifa/worldcup/v1/teams",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"stadiums": {
		endpoint:    "/fifa/worldcup/v1/stadiums",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"group_standings": {
		endpoint:    "/fifa/worldcup/v1/group_standings",
		primaryKeys: []string{"season_year", "team_id"},
		strategy:    config.StrategyReplace,
	},
	"matches": {
		endpoint:    "/fifa/worldcup/v1/matches",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"players": {
		endpoint:    "/fifa/worldcup/v1/players",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"rosters": {
		endpoint:    "/fifa/worldcup/v1/rosters",
		primaryKeys: []string{"season_year", "team_id", "player_id"},
		strategy:    config.StrategyReplace,
	},
	"match_lineups": {
		endpoint:    "/fifa/worldcup/v1/match_lineups",
		primaryKeys: []string{"match_id", "team_id", "player_id"},
		strategy:    config.StrategyReplace,
	},
	"match_events": {
		endpoint:    "/fifa/worldcup/v1/match_events",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"player_match_stats": {
		endpoint:    "/fifa/worldcup/v1/player_match_stats",
		primaryKeys: []string{"match_id", "player_id"},
		strategy:    config.StrategyReplace,
	},
	"team_match_stats": {
		endpoint:    "/fifa/worldcup/v1/team_match_stats",
		primaryKeys: []string{"match_id", "team_id"},
		strategy:    config.StrategyReplace,
	},
	"match_shots": {
		endpoint:    "/fifa/worldcup/v1/match_shots",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"match_momentum": {
		endpoint:    "/fifa/worldcup/v1/match_momentum",
		primaryKeys: []string{"match_id", "minute"},
		strategy:    config.StrategyReplace,
	},
	"match_best_players": {
		endpoint:    "/fifa/worldcup/v1/match_best_players",
		primaryKeys: []string{"match_id", "player_id"},
		strategy:    config.StrategyReplace,
	},
	"match_avg_positions": {
		endpoint:    "/fifa/worldcup/v1/match_avg_positions",
		primaryKeys: []string{"match_id", "player_id"},
		strategy:    config.StrategyReplace,
	},
	"match_team_form": {
		endpoint:    "/fifa/worldcup/v1/match_team_form",
		primaryKeys: []string{"match_id", "team_id"},
		strategy:    config.StrategyReplace,
	},
}

var _ source.Source = (*BallDontLieSource)(nil)
