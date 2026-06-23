package football_data_org

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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
	// football-data.org's free tier allows 10 requests/minute.
	rateLimit      = (10 * 0.8) / 60.0
	rateLimitBurst = 5
)

type tableConfig struct {
	primaryKeys []string
	strategy    config.IncrementalStrategy
	read        func(context.Context, source.ReadOptions, chan<- source.RecordBatchResult) error
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
	return []string{"footballdata"}
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
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
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
	if parsed.Scheme != "footballdata" {
		return uriConfig{}, fmt.Errorf("invalid football-data URI: must start with footballdata://")
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
	return true
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
		TableStrategy:    cfg.strategy,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("football-data source relies on schema inference")
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
			strategy:    config.StrategyMerge,
			read:        s.readTeams,
		},
		"stadiums": {
			primaryKeys: []string{"venue_key"},
			strategy:    config.StrategyReplace,
			read:        s.readStadiums,
		},
		"group_standings": {
			primaryKeys: []string{"competition_id", "season_id", "stage", "standing_type", "group_name", "team_id"},
			strategy:    config.StrategyReplace,
			read:        s.readStandings,
		},
		"matches": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyMerge,
			read:        s.readMatches,
		},
		"players": {
			primaryKeys: []string{"team_id", "id"},
			strategy:    config.StrategyReplace,
			read:        s.readPlayers,
		},
		"match_events": {
			primaryKeys: []string{"event_key"},
			strategy:    config.StrategyMerge,
			read:        s.readMatchEvents,
		},
	}
}

func (s *FootballDataOrgSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)
		if err := cfg.read(ctx, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// sendBatch converts a set of items to an Arrow record and streams it to the
// results channel. Empty batches are skipped so no zero-row record is emitted.
func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert football-data data to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *FootballDataOrgSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return err
	}
	items, err := extractArray(payload, "teams")
	if err != nil {
		return fmt.Errorf("malformed football-data response from teams: %w", err)
	}

	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		out = append(out, item)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			out = out[:opts.Limit]
			break
		}
	}
	return sendBatch(out, opts, results)
}

func (s *FootballDataOrgSource) readStadiums(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	items := make([]map[string]interface{}, 0)
	seen := map[string]bool{}

	teamsPayload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return err
	}
	teams, err := extractArray(teamsPayload, "teams")
	if err != nil {
		return fmt.Errorf("malformed football-data response from teams: %w", err)
	}
	for _, team := range teams {
		if err := ctx.Err(); err != nil {
			return err
		}
		venueName := strings.TrimSpace(valueString(team["venue"]))
		if venueName == "" {
			continue
		}
		key := makeVenueKey(venueName)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, map[string]interface{}{
			"venue_key":      key,
			"venue":          venueName,
			"source_context": "team",
			"team_id":        team["id"],
			"match_id":       nil,
			"raw":            team,
		})
		if opts.Limit > 0 && len(items) >= opts.Limit {
			return sendBatch(items[:opts.Limit], opts, results)
		}
	}

	// stadiums uses the replace strategy, so it always derives from the full
	// match set. Do not pass the ingestion interval through here, otherwise the
	// snapshot would be silently scoped to the window.
	matches, err := s.fetchRawMatches(ctx, source.ReadOptions{}, unfoldConfig{})
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return err
		}
		venueName := strings.TrimSpace(valueString(match["venue"]))
		if venueName == "" {
			continue
		}
		key := makeVenueKey(venueName)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, map[string]interface{}{
			"venue_key":      key,
			"venue":          venueName,
			"source_context": "match",
			"team_id":        nil,
			"match_id":       match["id"],
			"raw":            match,
		})
		if opts.Limit > 0 && len(items) >= opts.Limit {
			return sendBatch(items[:opts.Limit], opts, results)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return valueString(items[i]["venue_key"]) < valueString(items[j]["venue_key"])
	})
	return sendBatch(items, opts, results)
}

func (s *FootballDataOrgSource) readStandings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "standings"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return err
	}
	standings, err := extractArray(payload, "standings")
	if err != nil {
		return fmt.Errorf("malformed football-data response from standings: %w", err)
	}

	competition := nestedMap(payload, "competition")
	season := nestedMap(payload, "season")
	out := make([]map[string]interface{}, 0)
	for _, standing := range standings {
		if err := ctx.Err(); err != nil {
			return err
		}
		table := interfaceSlice(standing["table"])
		for _, rawRow := range table {
			if err := ctx.Err(); err != nil {
				return err
			}
			row, ok := rawRow.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, map[string]interface{}{
				"competition_id": competition["id"],
				"season_id":      season["id"],
				"stage":          standing["stage"],
				"standing_type":  standing["type"],
				"group_name":     standing["group"],
				"team_id":        nestedMap(row, "team")["id"],
				"standing":       row,
				"competition":    competition,
				"season":         season,
			})
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return sendBatch(out[:opts.Limit], opts, results)
			}
		}
	}
	return sendBatch(out, opts, results)
}

func (s *FootballDataOrgSource) readMatches(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	matches, err := s.fetchRawMatches(ctx, opts, s.unfold)
	if err != nil {
		return err
	}

	out := make([]map[string]interface{}, 0, len(matches))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return err
		}
		out = append(out, match)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return sendBatch(out[:opts.Limit], opts, results)
		}
	}
	return sendBatch(out, opts, results)
}

func (s *FootballDataOrgSource) readPlayers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	payload, err := s.get(ctx, competitionEndpoint(s.competition, "teams"), map[string]string{"season": s.season}, unfoldConfig{})
	if err != nil {
		return err
	}
	teams, err := extractArray(payload, "teams")
	if err != nil {
		return fmt.Errorf("malformed football-data response from teams: %w", err)
	}

	// The teams table already exposes the squad embedded in the competition
	// response. Hydrate each team through /teams/<id> instead, so players carry
	// the richer per-player fields (firstName, lastName, shirtNumber,
	// marketValue, contract) that the embedded squad omits.
	out := make([]map[string]interface{}, 0)
	for _, team := range teams {
		if err := ctx.Err(); err != nil {
			return err
		}
		teamID := valueString(team["id"])
		if teamID == "" {
			continue
		}
		detail, err := s.get(ctx, "/teams/"+url.PathEscape(teamID), nil, unfoldConfig{})
		if err != nil {
			return err
		}
		squad := interfaceSlice(detail["squad"])
		for _, rawPlayer := range squad {
			player, ok := rawPlayer.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, map[string]interface{}{
				"id":      player["id"],
				"team_id": team["id"],
				"player":  player,
			})
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return sendBatch(out[:opts.Limit], opts, results)
			}
		}
	}
	return sendBatch(out, opts, results)
}

func (s *FootballDataOrgSource) readMatchEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	matches, err := s.fetchRawMatches(ctx, opts, unfoldConfig{goals: true, bookings: true, subs: true})
	if err != nil {
		return err
	}

	out := make([]map[string]interface{}, 0)
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return err
		}
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
				out = append(out, makeEvent(matchID, group.name, idx, event))
				if opts.Limit > 0 && len(out) >= opts.Limit {
					return sendBatch(out[:opts.Limit], opts, results)
				}
			}
		}
		substitutions := interfaceSlice(firstNonNil(match["substitutions"], match["subs"]))
		for idx, rawEvent := range substitutions {
			event, ok := rawEvent.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, makeEvent(matchID, "substitution", idx, event))
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return sendBatch(out[:opts.Limit], opts, results)
			}
		}
	}
	return sendBatch(out, opts, results)
}

func (s *FootballDataOrgSource) fetchRawMatches(ctx context.Context, opts source.ReadOptions, unfold unfoldConfig) ([]map[string]interface{}, error) {
	params := map[string]string{"season": s.season}
	if s.filters.matchday != "" {
		params["matchday"] = s.filters.matchday
	}
	if s.filters.status != "" {
		params["status"] = s.filters.status
	}
	if s.filters.stage != "" {
		params["stage"] = s.filters.stage
	}
	if s.filters.group != "" {
		params["group"] = s.filters.group
	}
	// The matches endpoint supports server-side date filtering via dateFrom/dateTo
	// (YYYY-MM-DD). Date scoping is driven entirely by the ingestion interval.
	if opts.IntervalStart != nil && opts.IntervalEnd != nil {
		params["dateFrom"] = opts.IntervalStart.UTC().Format("2006-01-02")
		params["dateTo"] = opts.IntervalEnd.UTC().Format("2006-01-02")
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

	req := s.client.R(ctx)
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

	var payload map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &payload); err != nil {
		return nil, fmt.Errorf("malformed football-data response from %s: %w", endpoint, err)
	}
	return normalizeMap(payload), nil
}

// jsonUseNumber decodes JSON while preserving large integers as json.Number,
// so IDs and counts are not silently degraded through float64.
func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
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

func makeEvent(matchID, eventType string, index int, event map[string]interface{}) map[string]interface{} {
	row := make(map[string]interface{}, len(event)+4)
	for key, value := range event {
		row[key] = value
	}
	// Set the synthetic keys last so a raw field of the same name can't
	// overwrite them — the primary key (event_key) must be stable.
	row["event_key"] = makeEventKey(matchID, eventType, index, event)
	row["match_id"] = matchID
	row["event_type"] = eventType
	row["event_index"] = index
	return row
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
