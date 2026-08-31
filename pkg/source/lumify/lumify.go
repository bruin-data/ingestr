package lumify

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
	defaultBaseURL = "https://lumify.ai"
	defaultPerPage = 100
	// Free-tier keys allow ~20 req/min; stay under that with headroom.
	rateLimit       = 0.25
	rateLimitBurst  = 3
	maxEventSpan    = 90 * 24 * time.Hour
	defaultLookback = 7 * 24 * time.Hour
)

type tableKind string

const (
	kindSports  tableKind = "sports"
	kindLeagues tableKind = "leagues"
	kindSeasons tableKind = "seasons"
	kindTeams   tableKind = "teams"
	kindPlayers tableKind = "players"
	kindEvents  tableKind = "events"
)

type tableConfig struct {
	kind        tableKind
	endpoint    string
	listKey     string
	primaryKeys []string
	strategy    config.IncrementalStrategy
	paginated   bool
	dateWindow  bool
}

type LumifySource struct {
	client  *httpclient.Client
	apiKey  string
	sport   string
	league  string
	baseURL string
}

func NewLumifySource() *LumifySource {
	return &LumifySource{}
}

func (s *LumifySource) Schemes() []string {
	return []string{"lumify"}
}

func (s *LumifySource) Connect(ctx context.Context, uri string) error {
	apiKey, sport, league, baseURL, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey
	s.sport = sport
	s.league = league
	s.baseURL = baseURL
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithHeader("Authorization", "Bearer "+apiKey),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

func parseURI(raw string) (apiKey, sport, league, baseURL string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse lumify URI: %w", err)
	}
	if parsed.Scheme != "lumify" {
		return "", "", "", "", fmt.Errorf("invalid lumify URI: must start with lumify://")
	}

	values := parsed.Query()
	apiKey = strings.TrimSpace(values.Get("api_key"))
	if apiKey == "" {
		return "", "", "", "", fmt.Errorf("invalid lumify URI: api_key query parameter is required")
	}
	// Accept either a bare key or a value that already includes the Bearer prefix.
	apiKey = strings.TrimPrefix(apiKey, "Bearer ")
	apiKey = strings.TrimPrefix(apiKey, "bearer ")

	sport = strings.TrimSpace(values.Get("sport"))
	league = strings.TrimSpace(values.Get("league"))

	baseURL = strings.TrimRight(values.Get("base_url"), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return apiKey, sport, league, baseURL, nil
}

func (s *LumifySource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *LumifySource) HandlesIncrementality() bool {
	return true
}

func (s *LumifySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	cfg, ok := tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s, supported tables are: sports, leagues, seasons, teams, players, events", req.Name)
	}

	return &source.DynamicSourceTable{
		TableName:        req.Name,
		TablePrimaryKeys: cfg.primaryKeys,
		TableStrategy:    cfg.strategy,
		KnownSchema:      false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("lumify source relies on schema inference")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, cfg, opts)
		},
	}, nil
}

func (s *LumifySource) read(ctx context.Context, cfg tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)

	go func() {
		defer close(results)
		var err error
		switch cfg.kind {
		case kindLeagues:
			err = s.streamLeagues(ctx, opts, results)
		default:
			err = s.stream(ctx, cfg, opts, results)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *LumifySource) stream(ctx context.Context, cfg tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("lumify source is not connected")
	}
	config.Debug("[LUMIFY] reading %s", cfg.endpoint)

	perPage := defaultPerPage
	if opts.PageSize > 0 && opts.PageSize < perPage {
		perPage = opts.PageSize
	}

	windows := [][2]*time.Time{{nil, nil}}
	if cfg.dateWindow {
		var err error
		windows, err = eventWindows(opts.IntervalStart, opts.IntervalEnd)
		if err != nil {
			return err
		}
	}

	for _, window := range windows {
		if err := s.streamWindow(ctx, cfg, opts, perPage, window[0], window[1], results); err != nil {
			return err
		}
	}
	return nil
}

func (s *LumifySource) streamWindow(
	ctx context.Context,
	cfg tableConfig,
	opts source.ReadOptions,
	perPage int,
	from, to *time.Time,
	results chan<- source.RecordBatchResult,
) error {
	var afterID string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		var payload map[string]interface{}
		req := s.client.R(ctx).SetResult(&payload)
		if cfg.paginated {
			req.SetQueryParam("limit", strconv.Itoa(perPage))
			if afterID != "" {
				req.SetQueryParam("after_id", afterID)
			}
		}
		if s.sport != "" && (cfg.kind == kindSeasons || cfg.kind == kindTeams || cfg.kind == kindPlayers || cfg.kind == kindEvents) {
			req.SetQueryParam("sport", s.sport)
		}
		if s.league != "" && (cfg.kind == kindTeams || cfg.kind == kindEvents) {
			req.SetQueryParam("league", s.league)
		}
		if from != nil {
			req.SetQueryParam("from", from.UTC().Format(time.RFC3339))
		}
		if to != nil {
			req.SetQueryParam("to", to.UTC().Format(time.RFC3339))
		}
		if cfg.kind == kindEvents {
			req.SetQueryParam("include_scores", "true")
		}

		resp, err := req.Get(cfg.endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch lumify endpoint %s: %w", cfg.endpoint, err)
		}
		if err := checkResponse(cfg.endpoint, resp); err != nil {
			return err
		}
		if len(payload) == 0 {
			if err := resp.JSON(&payload); err != nil {
				return fmt.Errorf("malformed lumify response from %s: %w", cfg.endpoint, err)
			}
		}

		pageItems, err := extractList(payload, cfg.listKey)
		if err != nil {
			return fmt.Errorf("malformed lumify response from %s: %w", cfg.endpoint, err)
		}
		if err := sendBatch(pageItems, opts, results); err != nil {
			return err
		}

		if !cfg.paginated {
			break
		}
		next := extractNextAfterID(payload)
		if next == "" {
			break
		}
		afterID = next
	}
	return nil
}

// streamLeagues reads /v1/sports and emits one row per nested league, promoting
// parent sport identifiers onto each league row.
func (s *LumifySource) streamLeagues(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if s.client == nil {
		return fmt.Errorf("lumify source is not connected")
	}
	config.Debug("[LUMIFY] reading leagues from /v1/sports")

	var payload map[string]interface{}
	resp, err := s.client.R(ctx).SetResult(&payload).Get("/v1/sports")
	if err != nil {
		return fmt.Errorf("failed to fetch lumify endpoint /v1/sports: %w", err)
	}
	if err := checkResponse("/v1/sports", resp); err != nil {
		return err
	}
	if len(payload) == 0 {
		if err := resp.JSON(&payload); err != nil {
			return fmt.Errorf("malformed lumify response from /v1/sports: %w", err)
		}
	}

	sports, err := extractList(payload, "sports")
	if err != nil {
		return fmt.Errorf("malformed lumify response from /v1/sports: %w", err)
	}

	leagues := make([]map[string]interface{}, 0)
	for _, sport := range sports {
		sportID := sport["id"]
		sportSlug, _ := sport["slug"].(string)
		sportName, _ := sport["name"].(string)
		rawLeagues, _ := sport["leagues"].([]interface{})
		for i, raw := range rawLeagues {
			league, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("sport leagues item %d is not an object", i)
			}
			row := normalizeMap(league)
			row["sport_id"] = sportID
			row["sport_slug"] = sportSlug
			row["sport_name"] = sportName
			if s.sport != "" && sportSlug != "" && !strings.EqualFold(sportSlug, s.sport) {
				continue
			}
			leagues = append(leagues, row)
		}
	}
	return sendBatch(leagues, opts, results)
}

func eventWindows(start, end *time.Time) ([][2]*time.Time, error) {
	now := time.Now().UTC()
	var from time.Time
	var to time.Time
	if start != nil {
		from = start.UTC()
	} else {
		from = now.Add(-defaultLookback)
	}
	if end != nil {
		to = end.UTC()
	} else {
		to = now
	}
	if to.Before(from) {
		return nil, fmt.Errorf("invalid interval: end %s is before start %s", to.Format(time.RFC3339), from.Format(time.RFC3339))
	}

	windows := make([][2]*time.Time, 0)
	cursor := from
	for cursor.Before(to) || cursor.Equal(to) {
		chunkEnd := cursor.Add(maxEventSpan)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		startCopy := cursor
		endCopy := chunkEnd
		windows = append(windows, [2]*time.Time{&startCopy, &endCopy})
		if !chunkEnd.Before(to) {
			break
		}
		// Advance just past the previous window end to avoid duplicate boundary rows.
		cursor = chunkEnd.Add(time.Second)
	}
	if len(windows) == 0 {
		windows = append(windows, [2]*time.Time{&from, &to})
	}
	return windows, nil
}

func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert lumify data to Arrow: %w", err)
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
		return fmt.Errorf("lumify API authentication failed for %s (status %d)", endpoint, resp.StatusCode())
	case http.StatusPaymentRequired:
		return fmt.Errorf("lumify API credits exhausted for %s (status 402)", endpoint)
	case http.StatusTooManyRequests:
		return fmt.Errorf("lumify API rate limit exceeded for %s (status 429)", endpoint)
	default:
		return fmt.Errorf("lumify API error for %s (status %d): %s", endpoint, resp.StatusCode(), resp.String())
	}
}

func extractList(payload map[string]interface{}, key string) ([]map[string]interface{}, error) {
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

func extractNextAfterID(payload map[string]interface{}) string {
	next, ok := payload["next_after_id"]
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
	case int64:
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
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
	"sports": {
		kind:        kindSports,
		endpoint:    "/v1/sports",
		listKey:     "sports",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"leagues": {
		kind:        kindLeagues,
		endpoint:    "/v1/sports",
		listKey:     "sports",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"seasons": {
		kind:        kindSeasons,
		endpoint:    "/v1/seasons",
		listKey:     "seasons",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
	},
	"teams": {
		kind:        kindTeams,
		endpoint:    "/v1/teams",
		listKey:     "data",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		paginated:   true,
	},
	"players": {
		kind:        kindPlayers,
		endpoint:    "/v1/players",
		listKey:     "data",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyReplace,
		paginated:   true,
	},
	"events": {
		kind:        kindEvents,
		endpoint:    "/v1/events",
		listKey:     "events",
		primaryKeys: []string{"id"},
		strategy:    config.StrategyMerge,
		paginated:   true,
		dateWindow:  true,
	},
}

var _ source.Source = (*LumifySource)(nil)
