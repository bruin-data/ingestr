package balldontlie_fifa

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
)

type tableConfig struct {
	endpoint    string
	columns     []schema.Column
	primaryKeys []string
	flatten     func(map[string]interface{}) map[string]interface{}
}

type BallDontLieFIFASource struct {
	client  *httpclient.Client
	apiKey  string
	season  string
	baseURL string
}

func NewBallDontLieFIFASource() *BallDontLieFIFASource {
	return &BallDontLieFIFASource{}
}

func (s *BallDontLieFIFASource) Schemes() []string {
	return []string{"balldontlie-fifa"}
}

func (s *BallDontLieFIFASource) Connect(ctx context.Context, uri string) error {
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
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func parseURI(raw string) (apiKey, season, baseURL string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse balldontlie-fifa URI: %w", err)
	}
	if parsed.Scheme != "balldontlie-fifa" {
		return "", "", "", fmt.Errorf("invalid balldontlie-fifa URI: must start with balldontlie-fifa://")
	}

	values := parsed.Query()
	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", "", fmt.Errorf("invalid balldontlie-fifa URI: api_key query parameter is required")
	}

	season = values.Get("season")
	if season == "" {
		season = defaultSeason
	}
	switch season {
	case "2018", "2022", "2026":
	default:
		return "", "", "", fmt.Errorf("invalid balldontlie-fifa URI: season must be one of 2018, 2022, 2026")
	}

	baseURL = strings.TrimRight(values.Get("base_url"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return apiKey, season, baseURL, nil
}

func (s *BallDontLieFIFASource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *BallDontLieFIFASource) HandlesIncrementality() bool {
	return false
}

func (s *BallDontLieFIFASource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: teams, stadiums, group_standings, matches, players, rosters, match_lineups, match_events, player_match_stats, team_match_stats, match_shots, match_momentum, match_best_players, match_avg_positions, match_team_form", req.Name)
	}

	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: cfg.primaryKeys,
		TableStrategy:    config.StrategyReplace,
		KnownSchema:      true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return &schema.TableSchema{
				Name:        req.Name,
				Columns:     cfg.columns,
				PrimaryKeys: cfg.primaryKeys,
			}, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func (s *BallDontLieFIFASource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		items, err := s.fetchAll(ctx, cfg, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cfg.columns, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert balldontlie-fifa data to Arrow: %w", err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results, nil
}

func (s *BallDontLieFIFASource) fetchAll(ctx context.Context, cfg tableConfig, opts source.ReadOptions) ([]map[string]interface{}, error) {
	if s.client == nil {
		return nil, fmt.Errorf("balldontlie-fifa source is not connected")
	}

	perPage := defaultPerPage
	if opts.PageSize > 0 && opts.PageSize < perPage {
		perPage = opts.PageSize
	}

	var cursor string
	items := make([]map[string]interface{}, 0)
	for {
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
			return nil, fmt.Errorf("failed to fetch balldontlie-fifa endpoint %s: %w", cfg.endpoint, err)
		}
		if err := checkResponse(cfg.endpoint, resp); err != nil {
			return nil, err
		}
		if len(payload) == 0 {
			if err := resp.JSON(&payload); err != nil {
				return nil, fmt.Errorf("malformed balldontlie-fifa response from %s: %w", cfg.endpoint, err)
			}
		}

		pageItems, err := extractData(payload, cfg)
		if err != nil {
			return nil, fmt.Errorf("malformed balldontlie-fifa response from %s: %w", cfg.endpoint, err)
		}
		items = append(items, pageItems...)
		if opts.Limit > 0 && len(items) >= opts.Limit {
			return selectColumns(items[:opts.Limit], cfg.columns), nil
		}

		nextCursor := extractNextCursor(payload)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return selectColumns(items, cfg.columns), nil
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("balldontlie-fifa API authentication failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusTooManyRequests:
		return fmt.Errorf("balldontlie-fifa API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("balldontlie-fifa API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

func extractData(payload map[string]interface{}, cfg tableConfig) ([]map[string]interface{}, error) {
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
		if cfg.flatten != nil {
			item = cfg.flatten(item)
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

func selectColumns(items []map[string]interface{}, columns []schema.Column) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		selected := make(map[string]interface{}, len(columns))
		for _, column := range columns {
			if value, ok := item[column.Name]; ok {
				selected[column.Name] = value
			}
		}
		out = append(out, selected)
	}
	return out
}

func flattenStanding(item map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"position":        item["position"],
		"played":          item["played"],
		"won":             item["won"],
		"drawn":           item["drawn"],
		"lost":            item["lost"],
		"goals_for":       item["goals_for"],
		"goals_against":   item["goals_against"],
		"goal_difference": item["goal_difference"],
		"points":          item["points"],
		"season":          item["season"],
		"team":            item["team"],
		"group":           item["group"],
	}
	addNested(out, "season", item["season"], "id", "year")
	addNested(out, "team", item["team"], "id", "name", "abbreviation", "country_code", "confederation")
	addNested(out, "group", item["group"], "id", "name")
	return out
}

func flattenMatch(item map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":                     item["id"],
		"match_number":           item["match_number"],
		"datetime":               item["datetime"],
		"status":                 item["status"],
		"home_team_source":       item["home_team_source"],
		"away_team_source":       item["away_team_source"],
		"home_score":             item["home_score"],
		"away_score":             item["away_score"],
		"home_score_penalties":   item["home_score_penalties"],
		"away_score_penalties":   item["away_score_penalties"],
		"first_half_home_score":  item["first_half_home_score"],
		"first_half_away_score":  item["first_half_away_score"],
		"second_half_home_score": item["second_half_home_score"],
		"second_half_away_score": item["second_half_away_score"],
		"extra_time_home_score":  item["extra_time_home_score"],
		"extra_time_away_score":  item["extra_time_away_score"],
		"has_extra_time":         item["has_extra_time"],
		"has_penalty_shootout":   item["has_penalty_shootout"],
		"round_number":           item["round_number"],
		"round_name":             item["round_name"],
		"home_formation":         item["home_formation"],
		"away_formation":         item["away_formation"],
		"season":                 item["season"],
		"stage":                  item["stage"],
		"group":                  item["group"],
		"stadium":                item["stadium"],
		"home_team":              item["home_team"],
		"away_team":              item["away_team"],
		"referee":                item["referee"],
		"home_manager":           item["home_manager"],
		"away_manager":           item["away_manager"],
	}
	addNested(out, "season", item["season"], "id", "year")
	addNested(out, "stage", item["stage"], "id", "name", "order")
	addNested(out, "group", item["group"], "id", "name")
	addNested(out, "stadium", item["stadium"], "id", "name", "city", "country")
	addNested(out, "home_team", item["home_team"], "id", "name", "abbreviation")
	addNested(out, "away_team", item["away_team"], "id", "name", "abbreviation")
	addNested(out, "referee", item["referee"], "id", "name")
	addNested(out, "home_manager", item["home_manager"], "id", "name")
	addNested(out, "away_manager", item["away_manager"], "id", "name")
	return out
}

func flattenRoster(item map[string]interface{}) map[string]interface{} {
	out := copyKeys(item, "team_id", "position", "appearances", "starts", "minutes_played", "goals", "assists", "yellow_cards", "red_cards", "avg_rating")
	out["season"] = item["season"]
	out["player"] = item["player"]
	addNested(out, "season", item["season"], "id", "year")
	addNested(out, "player", item["player"], "id", "name", "short_name", "position", "date_of_birth", "country_code", "country_name", "height_cm", "jersey_number")
	return out
}

func flattenLineup(item map[string]interface{}) map[string]interface{} {
	out := copyKeys(item, "match_id", "team_id", "is_starter", "is_substitute", "shirt_number", "position", "formation")
	out["player"] = item["player"]
	addNested(out, "player", item["player"], "id", "name", "short_name")
	return out
}

func flattenEvent(item map[string]interface{}) map[string]interface{} {
	out := copyKeys(item, "id", "match_id", "incident_type", "incident_class", "time_minute", "added_time", "period", "is_home", "home_score", "away_score", "shootout_sequence", "shootout_description", "rescinded", "reason")
	out["player"] = item["player"]
	out["assist_player"] = item["assist_player"]
	out["player_in"] = item["player_in"]
	out["player_out"] = item["player_out"]
	addNested(out, "player", item["player"], "id", "name")
	addNested(out, "assist_player", item["assist_player"], "id", "name")
	addNested(out, "player_in", item["player_in"], "id", "name")
	addNested(out, "player_out", item["player_out"], "id", "name")
	return out
}

func addNested(out map[string]interface{}, prefix string, raw interface{}, fields ...string) {
	nested, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for _, field := range fields {
		out[prefix+"_"+field] = nested[field]
	}
}

func copyKeys(item map[string]interface{}, keys ...string) map[string]interface{} {
	out := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		out[key] = item[key]
	}
	return out
}

func col(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

var tables = map[string]tableConfig{
	"teams": {
		endpoint:    "/fifa/worldcup/v1/teams",
		primaryKeys: []string{"id"},
		columns: []schema.Column{
			col("id", schema.TypeInt64),
			col("name", schema.TypeString),
			col("abbreviation", schema.TypeString),
			col("country_code", schema.TypeString),
			col("confederation", schema.TypeString),
		},
	},
	"stadiums": {
		endpoint:    "/fifa/worldcup/v1/stadiums",
		primaryKeys: []string{"id"},
		columns: []schema.Column{
			col("id", schema.TypeInt64),
			col("name", schema.TypeString),
			col("city", schema.TypeString),
			col("country", schema.TypeString),
			col("capacity", schema.TypeInt64),
			col("latitude", schema.TypeFloat64),
			col("longitude", schema.TypeFloat64),
		},
	},
	"group_standings": {
		endpoint:    "/fifa/worldcup/v1/group_standings",
		primaryKeys: []string{"season_year", "team_id"},
		flatten:     flattenStanding,
		columns: []schema.Column{
			col("season_id", schema.TypeInt64),
			col("season_year", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("team_name", schema.TypeString),
			col("team_abbreviation", schema.TypeString),
			col("team_country_code", schema.TypeString),
			col("team_confederation", schema.TypeString),
			col("group_id", schema.TypeInt64),
			col("group_name", schema.TypeString),
			col("position", schema.TypeInt64),
			col("played", schema.TypeInt64),
			col("won", schema.TypeInt64),
			col("drawn", schema.TypeInt64),
			col("lost", schema.TypeInt64),
			col("goals_for", schema.TypeInt64),
			col("goals_against", schema.TypeInt64),
			col("goal_difference", schema.TypeInt64),
			col("points", schema.TypeInt64),
			col("season", schema.TypeJSON),
			col("team", schema.TypeJSON),
			col("group", schema.TypeJSON),
		},
	},
	"matches": {
		endpoint:    "/fifa/worldcup/v1/matches",
		primaryKeys: []string{"id"},
		flatten:     flattenMatch,
		columns:     matchColumns,
	},
	"players": {
		endpoint:    "/fifa/worldcup/v1/players",
		primaryKeys: []string{"id"},
		columns: []schema.Column{
			col("id", schema.TypeInt64),
			col("name", schema.TypeString),
			col("short_name", schema.TypeString),
			col("position", schema.TypeString),
			col("date_of_birth", schema.TypeTimestampTZ),
			col("country_code", schema.TypeString),
			col("country_name", schema.TypeString),
			col("height_cm", schema.TypeInt64),
			col("jersey_number", schema.TypeString),
		},
	},
	"rosters": {
		endpoint:    "/fifa/worldcup/v1/rosters",
		primaryKeys: []string{"season_year", "team_id", "player_id"},
		flatten:     flattenRoster,
		columns: []schema.Column{
			col("season_id", schema.TypeInt64),
			col("season_year", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("player_id", schema.TypeInt64),
			col("player_name", schema.TypeString),
			col("player_short_name", schema.TypeString),
			col("player_position", schema.TypeString),
			col("player_date_of_birth", schema.TypeTimestampTZ),
			col("player_country_code", schema.TypeString),
			col("player_country_name", schema.TypeString),
			col("player_height_cm", schema.TypeInt64),
			col("player_jersey_number", schema.TypeString),
			col("position", schema.TypeString),
			col("appearances", schema.TypeInt64),
			col("starts", schema.TypeInt64),
			col("minutes_played", schema.TypeInt64),
			col("goals", schema.TypeInt64),
			col("assists", schema.TypeInt64),
			col("yellow_cards", schema.TypeInt64),
			col("red_cards", schema.TypeInt64),
			col("avg_rating", schema.TypeFloat64),
			col("season", schema.TypeJSON),
			col("player", schema.TypeJSON),
		},
	},
	"match_lineups": {
		endpoint:    "/fifa/worldcup/v1/match_lineups",
		primaryKeys: []string{"match_id", "team_id", "player_id"},
		flatten:     flattenLineup,
		columns: []schema.Column{
			col("match_id", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("player_id", schema.TypeInt64),
			col("player_name", schema.TypeString),
			col("player_short_name", schema.TypeString),
			col("is_starter", schema.TypeBoolean),
			col("is_substitute", schema.TypeBoolean),
			col("shirt_number", schema.TypeInt64),
			col("position", schema.TypeString),
			col("formation", schema.TypeString),
			col("player", schema.TypeJSON),
		},
	},
	"match_events": {
		endpoint:    "/fifa/worldcup/v1/match_events",
		primaryKeys: []string{"id"},
		flatten:     flattenEvent,
		columns: []schema.Column{
			col("id", schema.TypeInt64),
			col("match_id", schema.TypeInt64),
			col("incident_type", schema.TypeString),
			col("incident_class", schema.TypeString),
			col("time_minute", schema.TypeInt64),
			col("added_time", schema.TypeInt64),
			col("period", schema.TypeString),
			col("is_home", schema.TypeBoolean),
			col("player_id", schema.TypeInt64),
			col("player_name", schema.TypeString),
			col("assist_player_id", schema.TypeInt64),
			col("assist_player_name", schema.TypeString),
			col("player_in_id", schema.TypeInt64),
			col("player_in_name", schema.TypeString),
			col("player_out_id", schema.TypeInt64),
			col("player_out_name", schema.TypeString),
			col("home_score", schema.TypeInt64),
			col("away_score", schema.TypeInt64),
			col("shootout_sequence", schema.TypeInt64),
			col("shootout_description", schema.TypeString),
			col("rescinded", schema.TypeBoolean),
			col("reason", schema.TypeString),
			col("player", schema.TypeJSON),
			col("assist_player", schema.TypeJSON),
			col("player_in", schema.TypeJSON),
			col("player_out", schema.TypeJSON),
		},
	},
	"player_match_stats": {
		endpoint:    "/fifa/worldcup/v1/player_match_stats",
		primaryKeys: []string{"match_id", "player_id"},
		columns:     playerMatchStatsColumns,
	},
	"team_match_stats": {
		endpoint:    "/fifa/worldcup/v1/team_match_stats",
		primaryKeys: []string{"match_id", "team_id"},
		columns:     teamMatchStatsColumns,
	},
	"match_shots": {
		endpoint:    "/fifa/worldcup/v1/match_shots",
		primaryKeys: []string{"id"},
		columns:     matchShotColumns,
	},
	"match_momentum": {
		endpoint:    "/fifa/worldcup/v1/match_momentum",
		primaryKeys: []string{"match_id", "minute"},
		columns: []schema.Column{
			col("match_id", schema.TypeInt64),
			col("minute", schema.TypeFloat64),
			col("value", schema.TypeFloat64),
		},
	},
	"match_best_players": {
		endpoint:    "/fifa/worldcup/v1/match_best_players",
		primaryKeys: []string{"match_id", "player_id"},
		columns: []schema.Column{
			col("match_id", schema.TypeInt64),
			col("player_id", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("is_home", schema.TypeBoolean),
			col("side_rank", schema.TypeInt64),
			col("is_man_of_match", schema.TypeBoolean),
			col("rating", schema.TypeFloat64),
			col("reason", schema.TypeString),
		},
	},
	"match_avg_positions": {
		endpoint:    "/fifa/worldcup/v1/match_avg_positions",
		primaryKeys: []string{"match_id", "player_id"},
		columns: []schema.Column{
			col("match_id", schema.TypeInt64),
			col("player_id", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("is_home", schema.TypeBoolean),
			col("avg_x", schema.TypeFloat64),
			col("avg_y", schema.TypeFloat64),
		},
	},
	"match_team_form": {
		endpoint:    "/fifa/worldcup/v1/match_team_form",
		primaryKeys: []string{"match_id", "team_id"},
		columns: []schema.Column{
			col("match_id", schema.TypeInt64),
			col("team_id", schema.TypeInt64),
			col("is_home", schema.TypeBoolean),
			col("avg_rating", schema.TypeFloat64),
			col("position", schema.TypeInt64),
			col("value", schema.TypeString),
		},
	},
}

var matchColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("match_number", schema.TypeInt64),
	col("datetime", schema.TypeTimestampTZ),
	col("status", schema.TypeString),
	col("season_id", schema.TypeInt64),
	col("season_year", schema.TypeInt64),
	col("stage_id", schema.TypeInt64),
	col("stage_name", schema.TypeString),
	col("stage_order", schema.TypeInt64),
	col("group_id", schema.TypeInt64),
	col("group_name", schema.TypeString),
	col("stadium_id", schema.TypeInt64),
	col("stadium_name", schema.TypeString),
	col("stadium_city", schema.TypeString),
	col("stadium_country", schema.TypeString),
	col("home_team_id", schema.TypeInt64),
	col("home_team_name", schema.TypeString),
	col("home_team_abbreviation", schema.TypeString),
	col("away_team_id", schema.TypeInt64),
	col("away_team_name", schema.TypeString),
	col("away_team_abbreviation", schema.TypeString),
	col("home_team_source", schema.TypeJSON),
	col("away_team_source", schema.TypeJSON),
	col("home_score", schema.TypeInt64),
	col("away_score", schema.TypeInt64),
	col("home_score_penalties", schema.TypeInt64),
	col("away_score_penalties", schema.TypeInt64),
	col("first_half_home_score", schema.TypeInt64),
	col("first_half_away_score", schema.TypeInt64),
	col("second_half_home_score", schema.TypeInt64),
	col("second_half_away_score", schema.TypeInt64),
	col("extra_time_home_score", schema.TypeInt64),
	col("extra_time_away_score", schema.TypeInt64),
	col("has_extra_time", schema.TypeBoolean),
	col("has_penalty_shootout", schema.TypeBoolean),
	col("round_number", schema.TypeInt64),
	col("round_name", schema.TypeString),
	col("home_formation", schema.TypeString),
	col("away_formation", schema.TypeString),
	col("referee_id", schema.TypeInt64),
	col("referee_name", schema.TypeString),
	col("home_manager_id", schema.TypeInt64),
	col("home_manager_name", schema.TypeString),
	col("away_manager_id", schema.TypeInt64),
	col("away_manager_name", schema.TypeString),
	col("season", schema.TypeJSON),
	col("stage", schema.TypeJSON),
	col("group", schema.TypeJSON),
	col("stadium", schema.TypeJSON),
	col("home_team", schema.TypeJSON),
	col("away_team", schema.TypeJSON),
	col("referee", schema.TypeJSON),
	col("home_manager", schema.TypeJSON),
	col("away_manager", schema.TypeJSON),
}

var playerMatchStatsColumns = []schema.Column{
	col("match_id", schema.TypeInt64),
	col("player_id", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("is_home", schema.TypeBoolean),
	col("rating", schema.TypeFloat64),
	col("minutes_played", schema.TypeInt64),
	col("expected_goals", schema.TypeFloat64),
	col("expected_assists", schema.TypeFloat64),
	col("goals", schema.TypeInt64),
	col("assists", schema.TypeInt64),
	col("shots_on_target", schema.TypeInt64),
	col("passes_total", schema.TypeInt64),
	col("passes_accurate", schema.TypeInt64),
	col("key_passes", schema.TypeInt64),
	col("long_balls_total", schema.TypeInt64),
	col("long_balls_accurate", schema.TypeInt64),
	col("crosses_total", schema.TypeInt64),
	col("crosses_accurate", schema.TypeInt64),
	col("dribbles_attempted", schema.TypeInt64),
	col("dribbles_completed", schema.TypeInt64),
	col("tackles", schema.TypeInt64),
	col("tackles_won", schema.TypeInt64),
	col("interceptions", schema.TypeInt64),
	col("clearances", schema.TypeInt64),
	col("blocked_shots", schema.TypeInt64),
	col("duels_won", schema.TypeInt64),
	col("duels_lost", schema.TypeInt64),
	col("aerial_duels_won", schema.TypeInt64),
	col("aerial_duels_lost", schema.TypeInt64),
	col("fouls_committed", schema.TypeInt64),
	col("was_fouled", schema.TypeInt64),
	col("touches", schema.TypeInt64),
	col("possession_lost", schema.TypeInt64),
	col("ball_recoveries", schema.TypeInt64),
	col("big_chances_created", schema.TypeInt64),
	col("big_chances_missed", schema.TypeInt64),
	col("saves", schema.TypeInt64),
	col("saves_inside_box", schema.TypeInt64),
	col("punches", schema.TypeInt64),
	col("high_claims", schema.TypeInt64),
}

var teamMatchStatsColumns = []schema.Column{
	col("match_id", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("is_home", schema.TypeBoolean),
	col("possession_pct", schema.TypeInt64),
	col("expected_goals", schema.TypeFloat64),
	col("big_chances", schema.TypeInt64),
	col("big_chances_missed", schema.TypeInt64),
	col("shots_total", schema.TypeInt64),
	col("shots_on_target", schema.TypeInt64),
	col("shots_off_target", schema.TypeInt64),
	col("shots_blocked", schema.TypeInt64),
	col("shots_inside_box", schema.TypeInt64),
	col("shots_outside_box", schema.TypeInt64),
	col("hit_woodwork", schema.TypeInt64),
	col("corners", schema.TypeInt64),
	col("offsides", schema.TypeInt64),
	col("fouls", schema.TypeInt64),
	col("yellow_cards", schema.TypeInt64),
	col("passes_total", schema.TypeInt64),
	col("passes_accurate", schema.TypeInt64),
	col("passes_final_third", schema.TypeInt64),
	col("long_balls_total", schema.TypeInt64),
	col("long_balls_accurate", schema.TypeInt64),
	col("crosses_total", schema.TypeInt64),
	col("crosses_accurate", schema.TypeInt64),
	col("tackles", schema.TypeInt64),
	col("interceptions", schema.TypeInt64),
	col("clearances", schema.TypeInt64),
	col("saves", schema.TypeInt64),
	col("ground_duels_won", schema.TypeInt64),
	col("ground_duels_total", schema.TypeInt64),
	col("aerial_duels_won", schema.TypeInt64),
	col("aerial_duels_total", schema.TypeInt64),
	col("dribbles_completed", schema.TypeInt64),
	col("dribbles_total", schema.TypeInt64),
	col("throw_ins", schema.TypeInt64),
	col("goal_kicks", schema.TypeInt64),
	col("free_kicks", schema.TypeInt64),
}

var matchShotColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("match_id", schema.TypeInt64),
	col("player_id", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("is_home", schema.TypeBoolean),
	col("shot_type", schema.TypeString),
	col("situation", schema.TypeString),
	col("body_part", schema.TypeString),
	col("goal_type", schema.TypeString),
	col("xg", schema.TypeFloat64),
	col("xgot", schema.TypeFloat64),
	col("player_x", schema.TypeFloat64),
	col("player_y", schema.TypeFloat64),
	col("goal_mouth_x", schema.TypeFloat64),
	col("goal_mouth_y", schema.TypeFloat64),
	col("block_x", schema.TypeFloat64),
	col("block_y", schema.TypeFloat64),
	col("time_minute", schema.TypeInt64),
	col("added_time", schema.TypeInt64),
	col("time_seconds", schema.TypeInt64),
}

var _ source.Source = (*BallDontLieFIFASource)(nil)
