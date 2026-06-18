package football_data_org

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
	defaultBaseURL     = "https://api.football-data.org/v4"
	defaultCompetition = "WC"
	defaultSeason      = "2026"
)

type tableConfig struct {
	columns     []schema.Column
	primaryKeys []string
	fetch       func(context.Context, source.ReadOptions) ([]map[string]interface{}, error)
}

type FootballDataOrgSource struct {
	client      *httpclient.Client
	apiKey      string
	competition string
	season      string
	baseURL     string
	filters     matchFilters
	unfold      unfoldConfig
}

func NewFootballDataOrgSource() *FootballDataOrgSource {
	return &FootballDataOrgSource{}
}

func (s *FootballDataOrgSource) Schemes() []string {
	return []string{"football-data"}
}

func (s *FootballDataOrgSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = cfg.apiKey
	s.competition = cfg.competition
	s.season = cfg.season
	s.baseURL = cfg.baseURL
	s.filters = cfg.filters
	s.unfold = cfg.unfold
	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithHeader("X-Auth-Token", cfg.apiKey),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

type uriConfig struct {
	apiKey      string
	competition string
	season      string
	baseURL     string
	filters     matchFilters
	unfold      unfoldConfig
}

type matchFilters struct {
	matchday string
	status   string
	dateFrom string
	dateTo   string
	stage    string
	group    string
}

type unfoldConfig struct {
	goals    bool
	bookings bool
	subs     bool
	lineups  bool
}

func parseURI(raw string) (uriConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return uriConfig{}, fmt.Errorf("failed to parse football-data URI: %w", err)
	}
	if parsed.Scheme != "football-data" {
		return uriConfig{}, fmt.Errorf("invalid football-data URI: must start with football-data://")
	}

	values := parsed.Query()
	cfg := uriConfig{
		apiKey:      values.Get("api_key"),
		competition: strings.TrimSpace(values.Get("competition")),
		season:      values.Get("season"),
		baseURL:     strings.TrimRight(values.Get("base_url"), "/"),
		filters: matchFilters{
			matchday: values.Get("matchday"),
			status:   values.Get("status"),
			dateFrom: firstNonEmpty(values.Get("date_from"), values.Get("dateFrom")),
			dateTo:   firstNonEmpty(values.Get("date_to"), values.Get("dateTo")),
			stage:    values.Get("stage"),
			group:    values.Get("group"),
		},
		unfold: unfoldConfig{
			goals:    parseBool(values.Get("unfold_goals")),
			bookings: parseBool(values.Get("unfold_bookings")),
			subs:     parseBool(values.Get("unfold_subs")),
			lineups:  parseBool(values.Get("unfold_lineups")),
		},
	}
	if cfg.apiKey == "" {
		return uriConfig{}, fmt.Errorf("invalid football-data URI: api_key query parameter is required")
	}
	if cfg.competition == "" {
		cfg.competition = defaultCompetition
	}
	if cfg.season == "" {
		cfg.season = defaultSeason
	}
	if len(cfg.season) != 4 {
		return uriConfig{}, fmt.Errorf("invalid football-data URI: season must be a 4-digit year")
	}
	if _, err := strconv.Atoi(cfg.season); err != nil {
		return uriConfig{}, fmt.Errorf("invalid football-data URI: season must be a 4-digit year")
	}
	if cfg.filters.matchday != "" {
		if _, err := strconv.Atoi(cfg.filters.matchday); err != nil {
			return uriConfig{}, fmt.Errorf("invalid football-data URI: matchday must be an integer")
		}
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
	}
	return cfg, nil
}

func (s *FootballDataOrgSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FootballDataOrgSource) HandlesIncrementality() bool {
	return false
}

func (s *FootballDataOrgSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tables := s.tables()
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: teams, stadiums, group_standings, matches, players, match_events", req.Name)
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

func (s *FootballDataOrgSource) tables() map[string]tableConfig {
	return map[string]tableConfig{
		"teams": {
			primaryKeys: []string{"id"},
			columns:     teamColumns,
			fetch:       s.fetchTeams,
		},
		"stadiums": {
			primaryKeys: []string{"venue_key"},
			columns:     stadiumColumns,
			fetch:       s.fetchStadiums,
		},
		"group_standings": {
			primaryKeys: []string{"competition_id", "season_id", "stage", "standing_type", "group_name", "team_id"},
			columns:     standingColumns,
			fetch:       s.fetchStandings,
		},
		"matches": {
			primaryKeys: []string{"id"},
			columns:     matchColumns,
			fetch:       s.fetchMatches,
		},
		"players": {
			primaryKeys: []string{"team_id", "id"},
			columns:     playerColumns,
			fetch:       s.fetchPlayers,
		},
		"match_events": {
			primaryKeys: []string{"event_key"},
			columns:     eventColumns,
			fetch:       s.fetchMatchEvents,
		},
	}
}

func (s *FootballDataOrgSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)

		items, err := cfg.fetch(ctx, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		items = selectColumns(items, cfg.columns)

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cfg.columns, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert football-data data to Arrow: %w", err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results, nil
}

func (s *FootballDataOrgSource) fetchTeams(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return nil, err
	}
	items, err := extractArray(payload, "teams")
	if err != nil {
		return nil, fmt.Errorf("malformed football-data response from teams: %w", err)
	}

	competition := nestedMap(payload, "competition")
	season := nestedMap(payload, "season")
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, flattenTeam(competition, season, item))
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	return out, nil
}

func (s *FootballDataOrgSource) fetchStadiums(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	items := make([]map[string]interface{}, 0)
	seen := map[string]bool{}

	teamsPayload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return nil, err
	}
	teams, err := extractArray(teamsPayload, "teams")
	if err != nil {
		return nil, fmt.Errorf("malformed football-data response from teams: %w", err)
	}
	for _, team := range teams {
		venueName := strings.TrimSpace(valueString(team["venue"]))
		if venueName == "" {
			continue
		}
		key := makeVenueKey(venueName)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, normalizeMap(map[string]interface{}{
			"venue_key":      key,
			"venue_name":     venueName,
			"source_context": "team",
			"team_id":        team["id"],
			"team_name":      team["name"],
			"match_id":       nil,
			"raw":            team,
		}))
		if opts.Limit > 0 && len(items) >= opts.Limit {
			return items[:opts.Limit], nil
		}
	}

	matches, err := s.fetchRawMatches(ctx, unfoldConfig{})
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		venueName := strings.TrimSpace(valueString(match["venue"]))
		if venueName == "" {
			continue
		}
		key := makeVenueKey(venueName)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, normalizeMap(map[string]interface{}{
			"venue_key":      key,
			"venue_name":     venueName,
			"source_context": "match",
			"team_id":        nil,
			"team_name":      nil,
			"match_id":       match["id"],
			"raw":            match,
		}))
		if opts.Limit > 0 && len(items) >= opts.Limit {
			return items[:opts.Limit], nil
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return valueString(items[i]["venue_key"]) < valueString(items[j]["venue_key"])
	})
	return items, nil
}

func (s *FootballDataOrgSource) fetchStandings(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "standings"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return nil, err
	}
	standings, err := extractArray(payload, "standings")
	if err != nil {
		return nil, fmt.Errorf("malformed football-data response from standings: %w", err)
	}

	competition := nestedMap(payload, "competition")
	season := nestedMap(payload, "season")
	out := make([]map[string]interface{}, 0)
	for _, standing := range standings {
		table := interfaceSlice(standing["table"])
		for _, rawRow := range table {
			row, ok := rawRow.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, flattenStanding(competition, season, standing, row))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out[:opts.Limit], nil
			}
		}
	}
	return out, nil
}

func (s *FootballDataOrgSource) fetchMatches(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	matches, err := s.fetchRawMatches(ctx, s.unfold)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(matches))
	for _, match := range matches {
		out = append(out, flattenMatch(match))
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	return out, nil
}

func (s *FootballDataOrgSource) fetchPlayers(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return nil, err
	}
	teams, err := extractArray(payload, "teams")
	if err != nil {
		return nil, fmt.Errorf("malformed football-data response from teams: %w", err)
	}

	out := make([]map[string]interface{}, 0)
	for _, team := range teams {
		teamID := valueString(team["id"])
		if teamID == "" {
			continue
		}
		detail, err := s.get(ctx, "/teams/"+url.PathEscape(teamID), nil, unfoldConfig{})
		if err != nil {
			return nil, err
		}
		squad := interfaceSlice(detail["squad"])
		for _, rawPlayer := range squad {
			player, ok := rawPlayer.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, flattenPlayer(detail, player))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out[:opts.Limit], nil
			}
		}
	}
	return out, nil
}

func (s *FootballDataOrgSource) fetchMatchEvents(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	matches, err := s.fetchRawMatches(ctx, unfoldConfig{goals: true, bookings: true, subs: true})
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0)
	for _, match := range matches {
		matchID := valueString(match["id"])
		eventGroups := []struct {
			name string
			key  string
		}{
			{name: "goal", key: "goals"},
			{name: "booking", key: "bookings"},
		}
		for _, group := range eventGroups {
			events := interfaceSlice(match[group.key])
			for idx, rawEvent := range events {
				event, ok := rawEvent.(map[string]interface{})
				if !ok {
					continue
				}
				out = append(out, flattenEvent(matchID, group.name, idx, event))
				if opts.Limit > 0 && len(out) >= opts.Limit {
					return out[:opts.Limit], nil
				}
			}
		}
		substitutions := interfaceSlice(firstNonNil(match["substitutions"], match["subs"]))
		for idx, rawEvent := range substitutions {
			event, ok := rawEvent.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, flattenEvent(matchID, "substitution", idx, event))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out[:opts.Limit], nil
			}
		}
	}
	return out, nil
}

func (s *FootballDataOrgSource) fetchRawMatches(ctx context.Context, unfold unfoldConfig) ([]map[string]interface{}, error) {
	params := map[string]string{"season": s.season}
	if s.filters.matchday != "" {
		params["matchday"] = s.filters.matchday
	}
	if s.filters.status != "" {
		params["status"] = s.filters.status
	}
	if s.filters.dateFrom != "" {
		params["dateFrom"] = s.filters.dateFrom
	}
	if s.filters.dateTo != "" {
		params["dateTo"] = s.filters.dateTo
	}
	if s.filters.stage != "" {
		params["stage"] = s.filters.stage
	}
	if s.filters.group != "" {
		params["group"] = s.filters.group
	}

	payload, err := s.get(ctx, competitionEndpoint(s.competition, "matches"), params, unfold)
	if err != nil {
		return nil, err
	}
	matches, err := extractArray(payload, "matches")
	if err != nil {
		return nil, fmt.Errorf("malformed football-data response from matches: %w", err)
	}
	return matches, nil
}

func (s *FootballDataOrgSource) get(ctx context.Context, endpoint string, params map[string]string, unfold unfoldConfig) (map[string]interface{}, error) {
	if s.client == nil {
		return nil, fmt.Errorf("football-data source is not connected")
	}

	var payload map[string]interface{}
	req := s.client.R(ctx).SetResult(&payload)
	for key, value := range params {
		if value != "" {
			req.SetQueryParam(key, value)
		}
	}
	applyUnfoldHeaders(req, unfold)

	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch football-data endpoint %s: %w", endpoint, err)
	}
	if err := checkResponse(endpoint, resp); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		if err := resp.JSON(&payload); err != nil {
			return nil, fmt.Errorf("malformed football-data response from %s: %w", endpoint, err)
		}
	}
	return normalizeMap(payload), nil
}

func applyUnfoldHeaders(req *httpclient.Request, unfold unfoldConfig) {
	if unfold.lineups {
		req.SetHeader("X-Unfold-Lineups", "true")
	}
	if unfold.bookings {
		req.SetHeader("X-Unfold-Bookings", "true")
	}
	if unfold.subs {
		req.SetHeader("X-Unfold-Subs", "true")
	}
	if unfold.goals {
		req.SetHeader("X-Unfold-Goals", "true")
	}
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	message := providerErrorMessage(resp.String())
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("football-data API authentication or plan access failed for %s (status %d): %s", endpoint, resp.StatusCode(), message)
	case http.StatusTooManyRequests:
		return fmt.Errorf("football-data API rate limit exceeded for %s (status 429): %s", endpoint, message)
	default:
		return fmt.Errorf("football-data API error for %s (status %d): %s", endpoint, resp.StatusCode(), message)
	}
}

func providerErrorMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "empty response body"
	}
	return body
}

func extractArray(payload map[string]interface{}, key string) ([]map[string]interface{}, error) {
	raw, ok := payload[key].([]interface{})
	if !ok {
		return nil, fmt.Errorf("missing %s array", key)
	}
	items := make([]map[string]interface{}, 0, len(raw))
	for i, rawItem := range raw {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s item %d is not an object", key, i)
		}
		items = append(items, normalizeMap(item))
	}
	return items, nil
}

func flattenTeam(competition, season, team map[string]interface{}) map[string]interface{} {
	area := nestedMap(team, "area")
	out := map[string]interface{}{
		"id":               team["id"],
		"name":             team["name"],
		"short_name":       team["shortName"],
		"tla":              team["tla"],
		"crest":            team["crest"],
		"address":          team["address"],
		"website":          team["website"],
		"founded":          team["founded"],
		"club_colors":      team["clubColors"],
		"venue":            team["venue"],
		"last_updated":     team["lastUpdated"],
		"area_id":          area["id"],
		"area_name":        area["name"],
		"area_code":        area["code"],
		"area_flag":        area["flag"],
		"competition_id":   competition["id"],
		"competition_code": competition["code"],
		"competition_name": competition["name"],
		"season_id":        season["id"],
		"season_start":     season["startDate"],
		"season_end":       season["endDate"],
		"team":             team,
		"area":             area,
	}
	return normalizeMap(out)
}

func flattenStanding(competition, season, standing, row map[string]interface{}) map[string]interface{} {
	team := nestedMap(row, "team")
	out := map[string]interface{}{
		"competition_id":   competition["id"],
		"competition_code": competition["code"],
		"competition_name": competition["name"],
		"season_id":        season["id"],
		"season_start":     season["startDate"],
		"season_end":       season["endDate"],
		"stage":            standing["stage"],
		"standing_type":    standing["type"],
		"group_name":       standing["group"],
		"position":         row["position"],
		"team_id":          team["id"],
		"team_name":        team["name"],
		"team_short_name":  team["shortName"],
		"team_tla":         team["tla"],
		"team_crest":       team["crest"],
		"played_games":     row["playedGames"],
		"form":             row["form"],
		"won":              row["won"],
		"draw":             row["draw"],
		"lost":             row["lost"],
		"points":           row["points"],
		"goals_for":        row["goalsFor"],
		"goals_against":    row["goalsAgainst"],
		"goal_difference":  row["goalDifference"],
		"standing":         standing,
		"team":             team,
		"competition":      competition,
		"season":           season,
	}
	return normalizeMap(out)
}

func flattenMatch(match map[string]interface{}) map[string]interface{} {
	competition := nestedMap(match, "competition")
	season := nestedMap(match, "season")
	homeTeam := nestedMap(match, "homeTeam")
	awayTeam := nestedMap(match, "awayTeam")
	score := nestedMap(match, "score")
	fullTime := nestedMap(score, "fullTime")
	halfTime := nestedMap(score, "halfTime")
	regularTime := nestedMap(score, "regularTime")
	extraTime := nestedMap(score, "extraTime")
	penalties := nestedMap(score, "penalties")
	out := map[string]interface{}{
		"id":                     match["id"],
		"utc_date":               match["utcDate"],
		"status":                 match["status"],
		"minute":                 match["minute"],
		"injury_time":            match["injuryTime"],
		"attendance":             match["attendance"],
		"venue":                  match["venue"],
		"matchday":               match["matchday"],
		"stage":                  match["stage"],
		"group_name":             match["group"],
		"last_updated":           match["lastUpdated"],
		"competition_id":         competition["id"],
		"competition_code":       competition["code"],
		"competition_name":       competition["name"],
		"season_id":              season["id"],
		"season_start":           season["startDate"],
		"season_end":             season["endDate"],
		"home_team_id":           homeTeam["id"],
		"home_team_name":         homeTeam["name"],
		"home_team_short_name":   homeTeam["shortName"],
		"home_team_tla":          homeTeam["tla"],
		"home_team_crest":        homeTeam["crest"],
		"away_team_id":           awayTeam["id"],
		"away_team_name":         awayTeam["name"],
		"away_team_short_name":   awayTeam["shortName"],
		"away_team_tla":          awayTeam["tla"],
		"away_team_crest":        awayTeam["crest"],
		"winner":                 score["winner"],
		"duration":               score["duration"],
		"fulltime_home_goals":    fullTime["home"],
		"fulltime_away_goals":    fullTime["away"],
		"halftime_home_goals":    halfTime["home"],
		"halftime_away_goals":    halfTime["away"],
		"regulartime_home_goals": regularTime["home"],
		"regulartime_away_goals": regularTime["away"],
		"extratime_home_goals":   extraTime["home"],
		"extratime_away_goals":   extraTime["away"],
		"penalty_home_goals":     penalties["home"],
		"penalty_away_goals":     penalties["away"],
		"match":                  match,
		"competition":            competition,
		"season":                 season,
		"home_team":              homeTeam,
		"away_team":              awayTeam,
		"score":                  score,
		"referees":               interfaceSlice(match["referees"]),
		"goals":                  interfaceSlice(match["goals"]),
		"bookings":               interfaceSlice(match["bookings"]),
		"substitutions":          firstNonNil(match["substitutions"], match["subs"]),
	}
	return normalizeMap(out)
}

func flattenPlayer(team, player map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"team_id":       team["id"],
		"team_name":     team["name"],
		"team_tla":      team["tla"],
		"id":            player["id"],
		"name":          player["name"],
		"first_name":    player["firstName"],
		"last_name":     player["lastName"],
		"date_of_birth": player["dateOfBirth"],
		"nationality":   player["nationality"],
		"section":       player["section"],
		"position":      player["position"],
		"shirt_number":  player["shirtNumber"],
		"last_updated":  player["lastUpdated"],
		"player":        player,
		"team":          team,
	}
	return normalizeMap(out)
}

func flattenEvent(matchID, eventType string, index int, event map[string]interface{}) map[string]interface{} {
	team := firstNestedMap(event, "team")
	player := firstNestedMap(event, "player", "scorer")
	assist := firstNestedMap(event, "assist")
	playerIn := firstNestedMap(event, "playerIn")
	playerOut := firstNestedMap(event, "playerOut")
	out := map[string]interface{}{
		"event_key":       makeEventKey(matchID, eventType, index, event),
		"match_id":        matchID,
		"event_type":      eventType,
		"event_index":     index,
		"minute":          firstNonNil(event["minute"], event["time"]),
		"injury_time":     firstNonNil(event["injuryTime"], event["injury_time"]),
		"team_id":         team["id"],
		"team_name":       team["name"],
		"player_id":       player["id"],
		"player_name":     player["name"],
		"assist_id":       assist["id"],
		"assist_name":     assist["name"],
		"player_in_id":    playerIn["id"],
		"player_in_name":  playerIn["name"],
		"player_out_id":   playerOut["id"],
		"player_out_name": playerOut["name"],
		"card":            firstNonNil(event["card"], event["cardType"]),
		"detail":          firstNonNil(event["type"], event["detail"], event["description"]),
		"score":           event["score"],
		"raw":             event,
	}
	return normalizeMap(out)
}

func competitionEndpoint(competition, resource string) string {
	return "/competitions/" + url.PathEscape(competition) + "/" + resource
}

func makeVenueKey(name string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(name), " "))
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func makeEventKey(matchID, eventType string, index int, event map[string]interface{}) string {
	parts := []string{
		matchID,
		eventType,
		strconv.Itoa(index),
		valueString(firstNonNil(event["minute"], event["time"])),
		valueString(firstNonNil(event["injuryTime"], event["injury_time"])),
		valueString(nestedMap(event, "team")["id"]),
		valueString(firstNestedMap(event, "player", "scorer")["id"]),
		valueString(event["type"]),
		valueString(event["card"]),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func nestedMap(item map[string]interface{}, key string) map[string]interface{} {
	raw, ok := item[key].(map[string]interface{})
	if !ok || raw == nil {
		return map[string]interface{}{}
	}
	return raw
}

func firstNestedMap(item map[string]interface{}, keys ...string) map[string]interface{} {
	for _, key := range keys {
		if value := nestedMap(item, key); len(value) > 0 {
			return value
		}
	}
	return map[string]interface{}{}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func valueString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprint(v)
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
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

func col(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

var teamColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("name", schema.TypeString),
	col("short_name", schema.TypeString),
	col("tla", schema.TypeString),
	col("crest", schema.TypeString),
	col("address", schema.TypeString),
	col("website", schema.TypeString),
	col("founded", schema.TypeInt64),
	col("club_colors", schema.TypeString),
	col("venue", schema.TypeString),
	col("last_updated", schema.TypeTimestampTZ),
	col("area_id", schema.TypeInt64),
	col("area_name", schema.TypeString),
	col("area_code", schema.TypeString),
	col("area_flag", schema.TypeString),
	col("competition_id", schema.TypeInt64),
	col("competition_code", schema.TypeString),
	col("competition_name", schema.TypeString),
	col("season_id", schema.TypeInt64),
	col("season_start", schema.TypeDate),
	col("season_end", schema.TypeDate),
	col("team", schema.TypeJSON),
	col("area", schema.TypeJSON),
}

var stadiumColumns = []schema.Column{
	col("venue_key", schema.TypeString),
	col("venue_name", schema.TypeString),
	col("source_context", schema.TypeString),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("match_id", schema.TypeInt64),
	col("raw", schema.TypeJSON),
}

var standingColumns = []schema.Column{
	col("competition_id", schema.TypeInt64),
	col("competition_code", schema.TypeString),
	col("competition_name", schema.TypeString),
	col("season_id", schema.TypeInt64),
	col("season_start", schema.TypeDate),
	col("season_end", schema.TypeDate),
	col("stage", schema.TypeString),
	col("standing_type", schema.TypeString),
	col("group_name", schema.TypeString),
	col("position", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("team_short_name", schema.TypeString),
	col("team_tla", schema.TypeString),
	col("team_crest", schema.TypeString),
	col("played_games", schema.TypeInt64),
	col("form", schema.TypeString),
	col("won", schema.TypeInt64),
	col("draw", schema.TypeInt64),
	col("lost", schema.TypeInt64),
	col("points", schema.TypeInt64),
	col("goals_for", schema.TypeInt64),
	col("goals_against", schema.TypeInt64),
	col("goal_difference", schema.TypeInt64),
	col("standing", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("competition", schema.TypeJSON),
	col("season", schema.TypeJSON),
}

var matchColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("utc_date", schema.TypeTimestampTZ),
	col("status", schema.TypeString),
	col("minute", schema.TypeInt64),
	col("injury_time", schema.TypeInt64),
	col("attendance", schema.TypeInt64),
	col("venue", schema.TypeString),
	col("matchday", schema.TypeInt64),
	col("stage", schema.TypeString),
	col("group_name", schema.TypeString),
	col("last_updated", schema.TypeTimestampTZ),
	col("competition_id", schema.TypeInt64),
	col("competition_code", schema.TypeString),
	col("competition_name", schema.TypeString),
	col("season_id", schema.TypeInt64),
	col("season_start", schema.TypeDate),
	col("season_end", schema.TypeDate),
	col("home_team_id", schema.TypeInt64),
	col("home_team_name", schema.TypeString),
	col("home_team_short_name", schema.TypeString),
	col("home_team_tla", schema.TypeString),
	col("home_team_crest", schema.TypeString),
	col("away_team_id", schema.TypeInt64),
	col("away_team_name", schema.TypeString),
	col("away_team_short_name", schema.TypeString),
	col("away_team_tla", schema.TypeString),
	col("away_team_crest", schema.TypeString),
	col("winner", schema.TypeString),
	col("duration", schema.TypeString),
	col("fulltime_home_goals", schema.TypeInt64),
	col("fulltime_away_goals", schema.TypeInt64),
	col("halftime_home_goals", schema.TypeInt64),
	col("halftime_away_goals", schema.TypeInt64),
	col("regulartime_home_goals", schema.TypeInt64),
	col("regulartime_away_goals", schema.TypeInt64),
	col("extratime_home_goals", schema.TypeInt64),
	col("extratime_away_goals", schema.TypeInt64),
	col("penalty_home_goals", schema.TypeInt64),
	col("penalty_away_goals", schema.TypeInt64),
	col("match", schema.TypeJSON),
	col("competition", schema.TypeJSON),
	col("season", schema.TypeJSON),
	col("home_team", schema.TypeJSON),
	col("away_team", schema.TypeJSON),
	col("score", schema.TypeJSON),
	col("referees", schema.TypeJSON),
	col("goals", schema.TypeJSON),
	col("bookings", schema.TypeJSON),
	col("substitutions", schema.TypeJSON),
}

var playerColumns = []schema.Column{
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("team_tla", schema.TypeString),
	col("id", schema.TypeInt64),
	col("name", schema.TypeString),
	col("first_name", schema.TypeString),
	col("last_name", schema.TypeString),
	col("date_of_birth", schema.TypeDate),
	col("nationality", schema.TypeString),
	col("section", schema.TypeString),
	col("position", schema.TypeString),
	col("shirt_number", schema.TypeInt64),
	col("last_updated", schema.TypeTimestampTZ),
	col("player", schema.TypeJSON),
	col("team", schema.TypeJSON),
}

var eventColumns = []schema.Column{
	col("event_key", schema.TypeString),
	col("match_id", schema.TypeInt64),
	col("event_type", schema.TypeString),
	col("event_index", schema.TypeInt64),
	col("minute", schema.TypeInt64),
	col("injury_time", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("player_id", schema.TypeInt64),
	col("player_name", schema.TypeString),
	col("assist_id", schema.TypeInt64),
	col("assist_name", schema.TypeString),
	col("player_in_id", schema.TypeInt64),
	col("player_in_name", schema.TypeString),
	col("player_out_id", schema.TypeInt64),
	col("player_out_name", schema.TypeString),
	col("card", schema.TypeString),
	col("detail", schema.TypeString),
	col("score", schema.TypeJSON),
	col("raw", schema.TypeJSON),
}
