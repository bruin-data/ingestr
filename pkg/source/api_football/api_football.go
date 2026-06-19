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
	// api-sports.io free tier allows 10 req/min; rateLimit is ~80% of that per
	// second, with a burst small enough that the first minute stays under the cap.
	rateLimit      = 0.13
	rateLimitBurst = 2
)

type tableConfig struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
	read           func(context.Context, source.ReadOptions, chan<- source.RecordBatchResult) error
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
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
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
	return true
}

func (s *APIFootballSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tables := s.tables()
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: teams, stadiums, group_standings, matches, players, match_events", req.Name)
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       cfg.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("api-football source relies on schema inference")
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
			strategy:    config.StrategyReplace,
			read:        s.readTeams,
		},
		"stadiums": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyMerge,
			read:        s.readStadiums,
		},
		"group_standings": {
			primaryKeys: []string{"league_id", "season", "group_name", "team_id"},
			strategy:    config.StrategyMerge,
			read:        s.readStandings,
		},
		"matches": {
			primaryKeys: []string{"id"},
			strategy:    config.StrategyMerge,
			read:        s.readMatches,
		},
		"players": {
			primaryKeys: []string{"id"},
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

func (s *APIFootballSource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)
		if err := cfg.read(ctx, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// sendBatch converts a page of items to an Arrow record and streams it to the
// results channel. Empty pages are skipped so no zero-row batch is emitted.
func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert api-football data to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *APIFootballSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading teams")
	payload, err := s.get(ctx, "/teams", map[string]string{
		"league": s.league,
		"season": s.season,
	})
	if err != nil {
		return err
	}
	items, err := extractResponse(payload)
	if err != nil {
		return fmt.Errorf("malformed api-football response from /teams: %w", err)
	}

	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"id":    nestedMap(item, "team")["id"],
			"team":  item["team"],
			"venue": item["venue"],
		})
	}
	return sendBatch(out, opts, results)
}

func (s *APIFootballSource) readStadiums(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading stadiums")
	fixtures, err := s.fetchFixtures(ctx, opts)
	if err != nil {
		return err
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
		fallbacks[id] = venue
	}
	sort.Strings(ids)

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := s.get(ctx, "/venues", map[string]string{"id": id})
		if err != nil {
			return err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return fmt.Errorf("malformed api-football response from /venues: %w", err)
		}
		var venue map[string]interface{}
		if len(items) == 0 {
			venue = fallbacks[id]
		} else {
			venue = items[0]
		}
		if err := sendBatch([]map[string]interface{}{venue}, opts, results); err != nil {
			return err
		}
	}
	return nil
}

func (s *APIFootballSource) readStandings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading group_standings")
	payload, err := s.get(ctx, "/standings", map[string]string{
		"league": s.league,
		"season": s.season,
	})
	if err != nil {
		return err
	}
	items, err := extractResponse(payload)
	if err != nil {
		return fmt.Errorf("malformed api-football response from /standings: %w", err)
	}

	out := make([]map[string]interface{}, 0)
	for _, item := range items {
		league := nestedMap(item, "league")
		// Keep the league object raw, minus the standings array it embeds
		// (which would otherwise duplicate the whole table in every row).
		leagueHeader := make(map[string]interface{}, len(league))
		for key, value := range league {
			if key == "standings" {
				continue
			}
			leagueHeader[key] = value
		}
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
				out = append(out, map[string]interface{}{
					"league_id":  league["id"],
					"season":     league["season"],
					"group_name": standing["group"],
					"team_id":    nestedMap(standing, "team")["id"],
					"league":     leagueHeader,
					"standing":   standing,
				})
			}
		}
	}
	return sendBatch(out, opts, results)
}

func (s *APIFootballSource) readMatches(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading matches")
	fixtures, err := s.fetchFixtures(ctx, opts)
	if err != nil {
		return err
	}
	out := make([]map[string]interface{}, 0, len(fixtures))
	for _, item := range fixtures {
		out = append(out, map[string]interface{}{
			"id":      nestedMap(item, "fixture")["id"],
			"fixture": item["fixture"],
			"league":  item["league"],
			"teams":   item["teams"],
			"goals":   item["goals"],
			"score":   item["score"],
		})
	}
	return sendBatch(out, opts, results)
}

func (s *APIFootballSource) readPlayers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading players")
	page := 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := s.get(ctx, "/players", map[string]string{
			"league": s.league,
			"season": s.season,
			"page":   strconv.Itoa(page),
		})
		if err != nil {
			return err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return fmt.Errorf("malformed api-football response from /players: %w", err)
		}
		out := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			out = append(out, map[string]interface{}{
				"id":         nestedMap(item, "player")["id"],
				"player":     item["player"],
				"statistics": item["statistics"],
			})
		}
		if err := sendBatch(out, opts, results); err != nil {
			return err
		}
		current, total := paging(payload)
		if total == 0 || current >= total {
			break
		}
		page++
	}
	return nil
}

func (s *APIFootballSource) readMatchEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[API-FOOTBALL] reading match_events")
	fixtures, err := s.fetchFixtures(ctx, opts)
	if err != nil {
		return err
	}

	for _, fixture := range fixtures {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		fixtureObj := nestedMap(fixture, "fixture")
		fixtureID := valueString(fixtureObj["id"])
		if fixtureID == "" {
			continue
		}
		payload, err := s.get(ctx, "/fixtures/events", map[string]string{"fixture": fixtureID})
		if err != nil {
			return err
		}
		items, err := extractResponse(payload)
		if err != nil {
			return fmt.Errorf("malformed api-football response from /fixtures/events: %w", err)
		}
		out := make([]map[string]interface{}, 0, len(items))
		for idx, item := range items {
			row := map[string]interface{}{
				"event_key":  makeEventKey(fixtureID, idx, item),
				"fixture_id": fixtureObj["id"],
			}
			for key, value := range item {
				row[key] = value
			}
			out = append(out, row)
		}
		if err := sendBatch(out, opts, results); err != nil {
			return err
		}
	}
	return nil
}

func (s *APIFootballSource) fetchFixtures(ctx context.Context, opts source.ReadOptions) ([]map[string]interface{}, error) {
	params := map[string]string{
		"league": s.league,
		"season": s.season,
	}
	if s.timezone != "" {
		params["timezone"] = s.timezone
	}
	// /fixtures filters server-side via from/to (YYYY-MM-DD, used together);
	// apply the interval so fixture-derived tables stay scoped to the window.
	if opts.IntervalStart != nil && opts.IntervalEnd != nil {
		params["from"] = opts.IntervalStart.UTC().Format("2006-01-02")
		params["to"] = opts.IntervalEnd.UTC().Format("2006-01-02")
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
