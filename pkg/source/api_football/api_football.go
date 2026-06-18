package api_football

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
	defaultBaseURL = "https://v3.football.api-sports.io"
	defaultLeague  = "1"
	defaultSeason  = "2026"
)

type tableConfig struct {
	columns     []schema.Column
	primaryKeys []string
	fetch       func(context.Context, source.ReadOptions) ([]map[string]interface{}, error)
}

type APIFootballSource struct {
	client   *httpclient.Client
	apiKey   string
	league   string
	season   string
	timezone string
	baseURL  string
}

func NewAPIFootballSource() *APIFootballSource {
	return &APIFootballSource{}
}

func (s *APIFootballSource) Schemes() []string {
	return []string{"api-football"}
}

func (s *APIFootballSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = cfg.apiKey
	s.league = cfg.league
	s.season = cfg.season
	s.timezone = cfg.timezone
	s.baseURL = cfg.baseURL
	s.client = httpclient.New(
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithHeader("x-apisports-key", cfg.apiKey),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

type uriConfig struct {
	apiKey   string
	league   string
	season   string
	timezone string
	baseURL  string
}

func parseURI(raw string) (uriConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return uriConfig{}, fmt.Errorf("failed to parse api-football URI: %w", err)
	}
	if parsed.Scheme != "api-football" {
		return uriConfig{}, fmt.Errorf("invalid api-football URI: must start with api-football://")
	}

	values := parsed.Query()
	cfg := uriConfig{
		apiKey:   values.Get("api_key"),
		league:   values.Get("league"),
		season:   values.Get("season"),
		timezone: values.Get("timezone"),
		baseURL:  strings.TrimRight(values.Get("base_url"), "/"),
	}
	if cfg.apiKey == "" {
		return uriConfig{}, fmt.Errorf("invalid api-football URI: api_key query parameter is required")
	}
	if cfg.league == "" {
		cfg.league = defaultLeague
	}
	if _, err := strconv.Atoi(cfg.league); err != nil {
		return uriConfig{}, fmt.Errorf("invalid api-football URI: league must be an integer")
	}
	if cfg.season == "" {
		cfg.season = defaultSeason
	}
	if len(cfg.season) != 4 {
		return uriConfig{}, fmt.Errorf("invalid api-football URI: season must be a 4-digit year")
	}
	if _, err := strconv.Atoi(cfg.season); err != nil {
		return uriConfig{}, fmt.Errorf("invalid api-football URI: season must be a 4-digit year")
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
	}
	return cfg, nil
}

func (s *APIFootballSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *APIFootballSource) HandlesIncrementality() bool {
	return false
}

func (s *APIFootballSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
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

func (s *APIFootballSource) tables() map[string]tableConfig {
	return map[string]tableConfig{
		"teams": {
			primaryKeys: []string{"id"},
			columns:     teamColumns,
			fetch:       s.fetchTeams,
		},
		"stadiums": {
			primaryKeys: []string{"id"},
			columns:     stadiumColumns,
			fetch:       s.fetchStadiums,
		},
		"group_standings": {
			primaryKeys: []string{"league_id", "season", "group_name", "team_id"},
			columns:     standingColumns,
			fetch:       s.fetchStandings,
		},
		"matches": {
			primaryKeys: []string{"id"},
			columns:     matchColumns,
			fetch:       s.fetchMatches,
		},
		"players": {
			primaryKeys: []string{"id"},
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

func (s *APIFootballSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
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
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert api-football data to Arrow: %w", err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results, nil
}

func (s *APIFootballSource) fetchTeams(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, "/teams", map[string]string{
		"league": s.league,
		"season": s.season,
	})
	if err != nil {
		return nil, err
	}
	items, err := extractResponse(payload)
	if err != nil {
		return nil, fmt.Errorf("malformed api-football response from /teams: %w", err)
	}

	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, flattenTeam(item))
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	return out, nil
}

func (s *APIFootballSource) fetchStadiums(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	fixtures, err := s.fetchFixtures(ctx)
	if err != nil {
		return nil, err
	}

	fallbacks := make(map[string]map[string]interface{})
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, fixture := range fixtures {
		venue := nestedMap(nestedMap(fixture, "fixture"), "venue")
		id := valueString(venue["id"])
		if id == "" {
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		fallbacks[id] = flattenFixtureVenue(venue)
	}
	sort.Strings(ids)

	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		payload, err := s.get(ctx, "/venues", map[string]string{"id": id})
		if err != nil {
			return nil, err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return nil, fmt.Errorf("malformed api-football response from /venues: %w", err)
		}
		if len(items) == 0 {
			out = append(out, fallbacks[id])
		} else {
			out = append(out, flattenVenue(items[0]))
		}
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	return out, nil
}

func (s *APIFootballSource) fetchStandings(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	payload, err := s.get(ctx, "/standings", map[string]string{
		"league": s.league,
		"season": s.season,
	})
	if err != nil {
		return nil, err
	}
	items, err := extractResponse(payload)
	if err != nil {
		return nil, fmt.Errorf("malformed api-football response from /standings: %w", err)
	}

	out := make([]map[string]interface{}, 0)
	for _, item := range items {
		league := nestedMap(item, "league")
		rawStandings, ok := league["standings"].([]interface{})
		if !ok {
			continue
		}
		for _, rawGroup := range rawStandings {
			group, ok := rawGroup.([]interface{})
			if !ok {
				continue
			}
			for _, rawStanding := range group {
				standing, ok := rawStanding.(map[string]interface{})
				if !ok {
					continue
				}
				out = append(out, flattenStanding(league, standing))
				if opts.Limit > 0 && len(out) >= opts.Limit {
					return out[:opts.Limit], nil
				}
			}
		}
	}
	return out, nil
}

func (s *APIFootballSource) fetchMatches(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	fixtures, err := s.fetchFixtures(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(fixtures))
	for _, item := range fixtures {
		out = append(out, flattenFixture(item))
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	return out, nil
}

func (s *APIFootballSource) fetchPlayers(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	page := 1
	out := make([]map[string]interface{}, 0)

	for {
		payload, err := s.get(ctx, "/players", map[string]string{
			"league": s.league,
			"season": s.season,
			"page":   strconv.Itoa(page),
		})
		if err != nil {
			return nil, err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return nil, fmt.Errorf("malformed api-football response from /players: %w", err)
		}
		for _, item := range items {
			out = append(out, flattenPlayer(item))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out[:opts.Limit], nil
			}
		}
		current, total := paging(payload)
		if total == 0 || current >= total {
			break
		}
		page++
	}
	return out, nil
}

func (s *APIFootballSource) fetchMatchEvents(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	fixtures, err := s.fetchFixtures(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0)
	for _, fixture := range fixtures {
		fixtureID := valueString(nestedMap(fixture, "fixture")["id"])
		if fixtureID == "" {
			continue
		}
		payload, err := s.get(ctx, "/fixtures/events", map[string]string{"fixture": fixtureID})
		if err != nil {
			return nil, err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return nil, fmt.Errorf("malformed api-football response from /fixtures/events: %w", err)
		}
		for idx, item := range items {
			out = append(out, flattenEvent(fixtureID, idx, item))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out[:opts.Limit], nil
			}
		}
	}
	return out, nil
}

func (s *APIFootballSource) fetchFixtures(ctx context.Context) ([]map[string]interface{}, error) {
	params := map[string]string{
		"league": s.league,
		"season": s.season,
	}
	if s.timezone != "" {
		params["timezone"] = s.timezone
	}
	payload, err := s.get(ctx, "/fixtures", params)
	if err != nil {
		return nil, err
	}
	items, err := extractResponse(payload)
	if err != nil {
		return nil, fmt.Errorf("malformed api-football response from /fixtures: %w", err)
	}
	return items, nil
}

func (s *APIFootballSource) get(ctx context.Context, endpoint string, params map[string]string) (map[string]interface{}, error) {
	if s.client == nil {
		return nil, fmt.Errorf("api-football source is not connected")
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
		return nil, fmt.Errorf("failed to fetch api-football endpoint %s: %w", endpoint, err)
	}
	if err := checkResponse(endpoint, resp); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		if err := resp.JSON(&payload); err != nil {
			return nil, fmt.Errorf("malformed api-football response from %s: %w", endpoint, err)
		}
	}
	if err := checkAPIError(endpoint, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func checkResponse(endpoint string, resp *httpclient.Response) error {
	if resp.IsSuccess() {
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("api-football API authentication failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusTooManyRequests:
		return fmt.Errorf("api-football API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("api-football API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

func checkAPIError(endpoint string, payload map[string]interface{}) error {
	errorsValue, ok := payload["errors"]
	if !ok || errorsValue == nil {
		return nil
	}
	switch v := errorsValue.(type) {
	case []interface{}:
		if len(v) > 0 {
			return fmt.Errorf("api-football API error for %s: %v", endpoint, v)
		}
	case map[string]interface{}:
		if len(v) > 0 {
			return fmt.Errorf("api-football API error for %s: %v", endpoint, v)
		}
	case string:
		if strings.TrimSpace(v) != "" {
			return fmt.Errorf("api-football API error for %s: %s", endpoint, v)
		}
	}
	return nil
}

func extractResponse(payload map[string]interface{}) ([]map[string]interface{}, error) {
	raw, ok := payload["response"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("missing response array")
	}
	items := make([]map[string]interface{}, 0, len(raw))
	for i, rawItem := range raw {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("response item %d is not an object", i)
		}
		items = append(items, normalizeMap(item))
	}
	return items, nil
}

func paging(payload map[string]interface{}) (current, total int) {
	pg := nestedMap(payload, "paging")
	return valueInt(pg["current"]), valueInt(pg["total"])
}

func flattenTeam(item map[string]interface{}) map[string]interface{} {
	team := nestedMap(item, "team")
	venue := nestedMap(item, "venue")
	out := map[string]interface{}{
		"id":             team["id"],
		"name":           team["name"],
		"code":           team["code"],
		"country":        team["country"],
		"founded":        team["founded"],
		"national":       team["national"],
		"logo":           team["logo"],
		"venue_id":       venue["id"],
		"venue_name":     venue["name"],
		"venue_address":  venue["address"],
		"venue_city":     venue["city"],
		"venue_capacity": venue["capacity"],
		"venue_surface":  venue["surface"],
		"venue_image":    venue["image"],
		"team":           team,
		"venue":          venue,
	}
	return normalizeMap(out)
}

func flattenFixtureVenue(venue map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":    venue["id"],
		"name":  venue["name"],
		"city":  venue["city"],
		"venue": venue,
	}
	return normalizeMap(out)
}

func flattenVenue(item map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":       item["id"],
		"name":     item["name"],
		"address":  item["address"],
		"city":     item["city"],
		"country":  item["country"],
		"capacity": item["capacity"],
		"surface":  item["surface"],
		"image":    item["image"],
		"venue":    item,
	}
	return normalizeMap(out)
}

func flattenStanding(league, standing map[string]interface{}) map[string]interface{} {
	team := nestedMap(standing, "team")
	all := nestedMap(standing, "all")
	home := nestedMap(standing, "home")
	away := nestedMap(standing, "away")
	out := map[string]interface{}{
		"league_id":          league["id"],
		"league_name":        league["name"],
		"league_country":     league["country"],
		"season":             league["season"],
		"group_name":         standing["group"],
		"rank":               standing["rank"],
		"team_id":            team["id"],
		"team_name":          team["name"],
		"team_logo":          team["logo"],
		"points":             standing["points"],
		"goals_diff":         standing["goalsDiff"],
		"form":               standing["form"],
		"status":             standing["status"],
		"description":        standing["description"],
		"all_played":         all["played"],
		"all_win":            all["win"],
		"all_draw":           all["draw"],
		"all_lose":           all["lose"],
		"all_goals_for":      nestedMap(all, "goals")["for"],
		"all_goals_against":  nestedMap(all, "goals")["against"],
		"home_played":        home["played"],
		"home_win":           home["win"],
		"home_draw":          home["draw"],
		"home_lose":          home["lose"],
		"home_goals_for":     nestedMap(home, "goals")["for"],
		"home_goals_against": nestedMap(home, "goals")["against"],
		"away_played":        away["played"],
		"away_win":           away["win"],
		"away_draw":          away["draw"],
		"away_lose":          away["lose"],
		"away_goals_for":     nestedMap(away, "goals")["for"],
		"away_goals_against": nestedMap(away, "goals")["against"],
		"updated_at":         standing["update"],
		"league":             league,
		"team":               team,
		"all":                all,
		"home":               home,
		"away":               away,
	}
	return normalizeMap(out)
}

func flattenFixture(item map[string]interface{}) map[string]interface{} {
	fixture := nestedMap(item, "fixture")
	league := nestedMap(item, "league")
	teams := nestedMap(item, "teams")
	homeTeam := nestedMap(teams, "home")
	awayTeam := nestedMap(teams, "away")
	goals := nestedMap(item, "goals")
	score := nestedMap(item, "score")
	status := nestedMap(fixture, "status")
	venue := nestedMap(fixture, "venue")
	out := map[string]interface{}{
		"id":                   fixture["id"],
		"referee":              fixture["referee"],
		"timezone":             fixture["timezone"],
		"date":                 fixture["date"],
		"timestamp":            fixture["timestamp"],
		"period_first":         nestedMap(fixture, "periods")["first"],
		"period_second":        nestedMap(fixture, "periods")["second"],
		"venue_id":             venue["id"],
		"venue_name":           venue["name"],
		"venue_city":           venue["city"],
		"status_long":          status["long"],
		"status_short":         status["short"],
		"status_elapsed":       status["elapsed"],
		"status_extra":         status["extra"],
		"league_id":            league["id"],
		"league_name":          league["name"],
		"league_country":       league["country"],
		"season":               league["season"],
		"round":                league["round"],
		"home_team_id":         homeTeam["id"],
		"home_team_name":       homeTeam["name"],
		"home_team_logo":       homeTeam["logo"],
		"home_team_winner":     homeTeam["winner"],
		"away_team_id":         awayTeam["id"],
		"away_team_name":       awayTeam["name"],
		"away_team_logo":       awayTeam["logo"],
		"away_team_winner":     awayTeam["winner"],
		"home_goals":           goals["home"],
		"away_goals":           goals["away"],
		"halftime_home_goals":  nestedMap(score, "halftime")["home"],
		"halftime_away_goals":  nestedMap(score, "halftime")["away"],
		"fulltime_home_goals":  nestedMap(score, "fulltime")["home"],
		"fulltime_away_goals":  nestedMap(score, "fulltime")["away"],
		"extratime_home_goals": nestedMap(score, "extratime")["home"],
		"extratime_away_goals": nestedMap(score, "extratime")["away"],
		"penalty_home_goals":   nestedMap(score, "penalty")["home"],
		"penalty_away_goals":   nestedMap(score, "penalty")["away"],
		"fixture":              fixture,
		"league":               league,
		"teams":                teams,
		"goals":                goals,
		"score":                score,
	}
	return normalizeMap(out)
}

func flattenPlayer(item map[string]interface{}) map[string]interface{} {
	player := nestedMap(item, "player")
	statistics := interfaceSlice(item["statistics"])
	firstStat := map[string]interface{}{}
	if len(statistics) > 0 {
		if stat, ok := statistics[0].(map[string]interface{}); ok {
			firstStat = stat
		}
	}
	team := nestedMap(firstStat, "team")
	games := nestedMap(firstStat, "games")
	goals := nestedMap(firstStat, "goals")
	cards := nestedMap(firstStat, "cards")
	out := map[string]interface{}{
		"id":              player["id"],
		"name":            player["name"],
		"firstname":       player["firstname"],
		"lastname":        player["lastname"],
		"age":             player["age"],
		"birth_date":      nestedMap(player, "birth")["date"],
		"birth_place":     nestedMap(player, "birth")["place"],
		"birth_country":   nestedMap(player, "birth")["country"],
		"nationality":     player["nationality"],
		"height":          player["height"],
		"weight":          player["weight"],
		"injured":         player["injured"],
		"photo":           player["photo"],
		"team_id":         team["id"],
		"team_name":       team["name"],
		"team_logo":       team["logo"],
		"position":        games["position"],
		"number":          games["number"],
		"captain":         games["captain"],
		"appearances":     games["appearences"],
		"lineups":         games["lineups"],
		"minutes":         games["minutes"],
		"rating":          games["rating"],
		"goals_total":     goals["total"],
		"goals_assists":   goals["assists"],
		"goals_saves":     goals["saves"],
		"yellow_cards":    cards["yellow"],
		"yellowred_cards": cards["yellowred"],
		"red_cards":       cards["red"],
		"player":          player,
		"team":            team,
		"statistics":      statistics,
	}
	return normalizeMap(out)
}

func flattenEvent(fixtureID string, index int, item map[string]interface{}) map[string]interface{} {
	team := nestedMap(item, "team")
	player := nestedMap(item, "player")
	assist := nestedMap(item, "assist")
	eventKey := makeEventKey(fixtureID, index, item)
	out := map[string]interface{}{
		"event_key":   eventKey,
		"fixture_id":  fixtureID,
		"event_index": index,
		"elapsed":     nestedMap(item, "time")["elapsed"],
		"extra":       nestedMap(item, "time")["extra"],
		"team_id":     team["id"],
		"team_name":   team["name"],
		"team_logo":   team["logo"],
		"player_id":   player["id"],
		"player_name": player["name"],
		"assist_id":   assist["id"],
		"assist_name": assist["name"],
		"type":        item["type"],
		"detail":      item["detail"],
		"comments":    item["comments"],
		"time":        nestedMap(item, "time"),
		"team":        team,
		"player":      player,
		"assist":      assist,
	}
	return normalizeMap(out)
}

func makeEventKey(fixtureID string, index int, item map[string]interface{}) string {
	parts := []string{
		fixtureID,
		strconv.Itoa(index),
		valueString(nestedMap(item, "time")["elapsed"]),
		valueString(nestedMap(item, "time")["extra"]),
		valueString(nestedMap(item, "team")["id"]),
		valueString(nestedMap(item, "player")["id"]),
		valueString(item["type"]),
		valueString(item["detail"]),
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

func valueInt(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
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
	col("code", schema.TypeString),
	col("country", schema.TypeString),
	col("founded", schema.TypeInt64),
	col("national", schema.TypeBoolean),
	col("logo", schema.TypeString),
	col("venue_id", schema.TypeInt64),
	col("venue_name", schema.TypeString),
	col("venue_address", schema.TypeString),
	col("venue_city", schema.TypeString),
	col("venue_capacity", schema.TypeInt64),
	col("venue_surface", schema.TypeString),
	col("venue_image", schema.TypeString),
	col("team", schema.TypeJSON),
	col("venue", schema.TypeJSON),
}

var stadiumColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("name", schema.TypeString),
	col("address", schema.TypeString),
	col("city", schema.TypeString),
	col("country", schema.TypeString),
	col("capacity", schema.TypeInt64),
	col("surface", schema.TypeString),
	col("image", schema.TypeString),
	col("venue", schema.TypeJSON),
}

var standingColumns = []schema.Column{
	col("league_id", schema.TypeInt64),
	col("league_name", schema.TypeString),
	col("league_country", schema.TypeString),
	col("season", schema.TypeInt64),
	col("group_name", schema.TypeString),
	col("rank", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("team_logo", schema.TypeString),
	col("points", schema.TypeInt64),
	col("goals_diff", schema.TypeInt64),
	col("form", schema.TypeString),
	col("status", schema.TypeString),
	col("description", schema.TypeString),
	col("all_played", schema.TypeInt64),
	col("all_win", schema.TypeInt64),
	col("all_draw", schema.TypeInt64),
	col("all_lose", schema.TypeInt64),
	col("all_goals_for", schema.TypeInt64),
	col("all_goals_against", schema.TypeInt64),
	col("home_played", schema.TypeInt64),
	col("home_win", schema.TypeInt64),
	col("home_draw", schema.TypeInt64),
	col("home_lose", schema.TypeInt64),
	col("home_goals_for", schema.TypeInt64),
	col("home_goals_against", schema.TypeInt64),
	col("away_played", schema.TypeInt64),
	col("away_win", schema.TypeInt64),
	col("away_draw", schema.TypeInt64),
	col("away_lose", schema.TypeInt64),
	col("away_goals_for", schema.TypeInt64),
	col("away_goals_against", schema.TypeInt64),
	col("updated_at", schema.TypeTimestampTZ),
	col("league", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("all", schema.TypeJSON),
	col("home", schema.TypeJSON),
	col("away", schema.TypeJSON),
}

var matchColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("referee", schema.TypeString),
	col("timezone", schema.TypeString),
	col("date", schema.TypeTimestampTZ),
	col("timestamp", schema.TypeTimestampTZ),
	col("period_first", schema.TypeTimestampTZ),
	col("period_second", schema.TypeTimestampTZ),
	col("venue_id", schema.TypeInt64),
	col("venue_name", schema.TypeString),
	col("venue_city", schema.TypeString),
	col("status_long", schema.TypeString),
	col("status_short", schema.TypeString),
	col("status_elapsed", schema.TypeInt64),
	col("status_extra", schema.TypeInt64),
	col("league_id", schema.TypeInt64),
	col("league_name", schema.TypeString),
	col("league_country", schema.TypeString),
	col("season", schema.TypeInt64),
	col("round", schema.TypeString),
	col("home_team_id", schema.TypeInt64),
	col("home_team_name", schema.TypeString),
	col("home_team_logo", schema.TypeString),
	col("home_team_winner", schema.TypeBoolean),
	col("away_team_id", schema.TypeInt64),
	col("away_team_name", schema.TypeString),
	col("away_team_logo", schema.TypeString),
	col("away_team_winner", schema.TypeBoolean),
	col("home_goals", schema.TypeInt64),
	col("away_goals", schema.TypeInt64),
	col("halftime_home_goals", schema.TypeInt64),
	col("halftime_away_goals", schema.TypeInt64),
	col("fulltime_home_goals", schema.TypeInt64),
	col("fulltime_away_goals", schema.TypeInt64),
	col("extratime_home_goals", schema.TypeInt64),
	col("extratime_away_goals", schema.TypeInt64),
	col("penalty_home_goals", schema.TypeInt64),
	col("penalty_away_goals", schema.TypeInt64),
	col("fixture", schema.TypeJSON),
	col("league", schema.TypeJSON),
	col("teams", schema.TypeJSON),
	col("goals", schema.TypeJSON),
	col("score", schema.TypeJSON),
}

var playerColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("name", schema.TypeString),
	col("firstname", schema.TypeString),
	col("lastname", schema.TypeString),
	col("age", schema.TypeInt64),
	col("birth_date", schema.TypeDate),
	col("birth_place", schema.TypeString),
	col("birth_country", schema.TypeString),
	col("nationality", schema.TypeString),
	col("height", schema.TypeString),
	col("weight", schema.TypeString),
	col("injured", schema.TypeBoolean),
	col("photo", schema.TypeString),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("team_logo", schema.TypeString),
	col("position", schema.TypeString),
	col("number", schema.TypeInt64),
	col("captain", schema.TypeBoolean),
	col("appearances", schema.TypeInt64),
	col("lineups", schema.TypeInt64),
	col("minutes", schema.TypeInt64),
	col("rating", schema.TypeFloat64),
	col("goals_total", schema.TypeInt64),
	col("goals_assists", schema.TypeInt64),
	col("goals_saves", schema.TypeInt64),
	col("yellow_cards", schema.TypeInt64),
	col("yellowred_cards", schema.TypeInt64),
	col("red_cards", schema.TypeInt64),
	col("player", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("statistics", schema.TypeJSON),
}

var eventColumns = []schema.Column{
	col("event_key", schema.TypeString),
	col("fixture_id", schema.TypeInt64),
	col("event_index", schema.TypeInt64),
	col("elapsed", schema.TypeInt64),
	col("extra", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("team_name", schema.TypeString),
	col("team_logo", schema.TypeString),
	col("player_id", schema.TypeInt64),
	col("player_name", schema.TypeString),
	col("assist_id", schema.TypeInt64),
	col("assist_name", schema.TypeString),
	col("type", schema.TypeString),
	col("detail", schema.TypeString),
	col("comments", schema.TypeString),
	col("time", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("player", schema.TypeJSON),
	col("assist", schema.TypeJSON),
}
