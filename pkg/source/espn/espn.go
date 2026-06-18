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

var supportedTables = []string{"teams", "scoreboard", "events", "competitors", "standings", "news"}

type tableConfig struct {
	columns     []schema.Column
	primaryKeys []string
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

func (s *ESPNSource) HandlesIncrementality() bool {
	return false
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

func (s *ESPNSource) tables() map[string]tableConfig {
	return map[string]tableConfig{
		"teams": {
			primaryKeys: []string{"id"},
			columns:     teamColumns,
			fetch:       s.fetchTeams,
		},
		"scoreboard": {
			primaryKeys: []string{"id"},
			columns:     scoreboardColumns,
			fetch:       s.fetchScoreboard,
		},
		"events": {
			primaryKeys: []string{"id"},
			columns:     eventColumns,
			fetch:       s.fetchScoreboard,
		},
		"competitors": {
			primaryKeys: []string{"event_id", "competition_id", "team_id"},
			columns:     competitorColumns,
			fetch:       s.fetchCompetitors,
		},
		"standings": {
			primaryKeys: []string{"league_id", "group_id", "season", "team_id"},
			columns:     standingColumns,
			fetch:       s.fetchStandings,
		},
		"news": {
			primaryKeys: []string{"id"},
			columns:     newsColumns,
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
		items = selectColumns(items, cfg.columns)

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cfg.columns, opts.ExcludeColumns)
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
				out = append(out, flattenTeam(nestedMap(teamItem, "team")))
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
		out = append(out, flattenEvent(event))
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
				out = append(out, flattenCompetitor(event, competitionObj, competitorObj))
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
		out = append(out, flattenArticle(article))
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

func flattenTeam(team map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":                 team["id"],
		"uid":                team["uid"],
		"slug":               team["slug"],
		"abbreviation":       team["abbreviation"],
		"name":               team["name"],
		"display_name":       team["displayName"],
		"short_display_name": team["shortDisplayName"],
		"location":           team["location"],
		"nickname":           team["nickname"],
		"color":              team["color"],
		"alternate_color":    team["alternateColor"],
		"is_active":          team["isActive"],
		"is_all_star":        team["isAllStar"],
		"logo":               teamLogo(team),
		"links":              team["links"],
		"logos":              team["logos"],
		"team":               team,
	}
	return normalizeMap(out)
}

func flattenEvent(event map[string]interface{}) map[string]interface{} {
	competition := firstMap(event["competitions"])
	venue := nestedMap(competition, "venue")
	statusType := nestedMap(nestedMap(competition, "status"), "type")
	home := findCompetitor(competition, "home")
	away := findCompetitor(competition, "away")
	homeTeam := nestedMap(home, "team")
	awayTeam := nestedMap(away, "team")
	out := map[string]interface{}{
		"id":                     event["id"],
		"uid":                    event["uid"],
		"date":                   event["date"],
		"name":                   event["name"],
		"short_name":             event["shortName"],
		"season_year":            nestedMap(event, "season")["year"],
		"season_type":            nestedMap(event, "season")["type"],
		"week_number":            nestedMap(event, "week")["number"],
		"status_type_id":         statusType["id"],
		"status_type_name":       statusType["name"],
		"status_type_state":      statusType["state"],
		"status_type_completed":  statusType["completed"],
		"venue_id":               venue["id"],
		"venue_name":             venue["fullName"],
		"home_team_id":           homeTeam["id"],
		"home_team_name":         homeTeam["displayName"],
		"home_team_abbreviation": homeTeam["abbreviation"],
		"home_score":             home["score"],
		"away_team_id":           awayTeam["id"],
		"away_team_name":         awayTeam["displayName"],
		"away_team_abbreviation": awayTeam["abbreviation"],
		"away_score":             away["score"],
		"competitions":           event["competitions"],
		"event":                  event,
	}
	return normalizeMap(out)
}

func flattenCompetitor(event, competition, competitor map[string]interface{}) map[string]interface{} {
	team := nestedMap(competitor, "team")
	out := map[string]interface{}{
		"event_id":                event["id"],
		"competition_id":          competition["id"],
		"team_id":                 competitor["id"],
		"uid":                     competitor["uid"],
		"type":                    competitor["type"],
		"order":                   competitor["order"],
		"home_away":               competitor["homeAway"],
		"winner":                  competitor["winner"],
		"score":                   competitor["score"],
		"curated_rank":            competitor["curatedRank"],
		"team_uid":                team["uid"],
		"team_location":           team["location"],
		"team_name":               team["name"],
		"team_display_name":       team["displayName"],
		"team_short_display_name": team["shortDisplayName"],
		"team_abbreviation":       team["abbreviation"],
		"team_color":              team["color"],
		"team_alternate_color":    team["alternateColor"],
		"team_logo":               teamLogo(team),
		"records":                 competitor["records"],
		"statistics":              competitor["statistics"],
		"linescores":              competitor["linescores"],
		"team":                    team,
		"competitor":              competitor,
	}
	return normalizeMap(out)
}

func flattenStanding(root, group, entry map[string]interface{}) map[string]interface{} {
	team := nestedMap(entry, "team")
	stats := statsByName(interfaceSlice(entry["stats"]))
	out := map[string]interface{}{
		"league_id":           root["id"],
		"league_name":         root["name"],
		"league_abbreviation": root["abbreviation"],
		"group_id":            group["id"],
		"group_name":          group["name"],
		"group_abbreviation":  group["abbreviation"],
		"season":              nestedMap(group, "standings")["season"],
		"season_type":         nestedMap(group, "standings")["seasonType"],
		"team_id":             team["id"],
		"team_uid":            team["uid"],
		"team_name":           team["displayName"],
		"team_abbreviation":   team["abbreviation"],
		"rank":                statValue(stats, "rank"),
		"playoff_seed":        statValue(stats, "playoffSeed"),
		"wins":                statValue(stats, "wins"),
		"losses":              statValue(stats, "losses"),
		"ties":                statValue(stats, "ties"),
		"win_percent":         statValue(stats, "winPercent"),
		"points_for":          statValue(stats, "pointsFor"),
		"points_against":      statValue(stats, "pointsAgainst"),
		"point_differential":  statValue(stats, "pointDifferential"),
		"games_behind":        statValue(stats, "gamesBehind"),
		"streak":              statDisplay(stats, "streak"),
		"overall_record":      statDisplay(stats, "overall"),
		"stats":               entry["stats"],
		"team":                team,
		"standings_group":     group,
	}
	return normalizeMap(out)
}

func flattenArticle(article map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":            article["id"],
		"now_id":        article["nowId"],
		"content_key":   article["contentKey"],
		"type":          article["type"],
		"headline":      article["headline"],
		"description":   article["description"],
		"published":     article["published"],
		"last_modified": article["lastModified"],
		"premium":       article["premium"],
		"byline":        article["byline"],
		"link":          nestedMap(nestedMap(article, "links"), "web")["href"],
		"image":         firstImageURL(article["images"]),
		"categories":    article["categories"],
		"images":        article["images"],
		"article":       article,
	}
	return normalizeMap(out)
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
		*out = append(*out, flattenStanding(root, group, entry))
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

func findCompetitor(competition map[string]interface{}, homeAway string) map[string]interface{} {
	for _, rawCompetitor := range interfaceSlice(competition["competitors"]) {
		competitor, ok := rawCompetitor.(map[string]interface{})
		if ok && valueString(competitor["homeAway"]) == homeAway {
			return competitor
		}
	}
	return map[string]interface{}{}
}

func statsByName(raw []interface{}) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{}, len(raw))
	for _, rawStat := range raw {
		stat, ok := rawStat.(map[string]interface{})
		if !ok {
			continue
		}
		name := valueString(stat["name"])
		if name == "" {
			name = valueString(stat["type"])
		}
		if name != "" {
			out[name] = stat
		}
	}
	return out
}

func statValue(stats map[string]map[string]interface{}, name string) interface{} {
	if stat, ok := stats[name]; ok {
		return stat["value"]
	}
	return nil
}

func statDisplay(stats map[string]map[string]interface{}, name string) interface{} {
	if stat, ok := stats[name]; ok {
		if display := stat["displayValue"]; display != nil {
			return display
		}
		return stat["summary"]
	}
	return nil
}

func teamLogo(team map[string]interface{}) interface{} {
	if logo := team["logo"]; logo != nil {
		return logo
	}
	logos := interfaceSlice(team["logos"])
	if len(logos) == 0 {
		return nil
	}
	first, ok := logos[0].(map[string]interface{})
	if !ok {
		return nil
	}
	return first["href"]
}

func firstImageURL(value interface{}) interface{} {
	image := firstMap(value)
	if image == nil {
		return nil
	}
	if url := image["url"]; url != nil {
		return url
	}
	return image["href"]
}

func firstMap(value interface{}) map[string]interface{} {
	items := interfaceSlice(value)
	if len(items) == 0 {
		return map[string]interface{}{}
	}
	first, ok := items[0].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return first
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

func reachedLimit(items []map[string]interface{}, opts source.ReadOptions) bool {
	return opts.Limit > 0 && len(items) >= opts.Limit
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
	col("uid", schema.TypeString),
	col("slug", schema.TypeString),
	col("abbreviation", schema.TypeString),
	col("name", schema.TypeString),
	col("display_name", schema.TypeString),
	col("short_display_name", schema.TypeString),
	col("location", schema.TypeString),
	col("nickname", schema.TypeString),
	col("color", schema.TypeString),
	col("alternate_color", schema.TypeString),
	col("is_active", schema.TypeBoolean),
	col("is_all_star", schema.TypeBoolean),
	col("logo", schema.TypeString),
	col("links", schema.TypeJSON),
	col("logos", schema.TypeJSON),
	col("team", schema.TypeJSON),
}

var scoreboardColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("uid", schema.TypeString),
	col("date", schema.TypeTimestampTZ),
	col("name", schema.TypeString),
	col("short_name", schema.TypeString),
	col("season_year", schema.TypeInt64),
	col("season_type", schema.TypeInt64),
	col("week_number", schema.TypeInt64),
	col("status_type_id", schema.TypeString),
	col("status_type_name", schema.TypeString),
	col("status_type_state", schema.TypeString),
	col("status_type_completed", schema.TypeBoolean),
	col("venue_id", schema.TypeInt64),
	col("venue_name", schema.TypeString),
	col("home_team_id", schema.TypeInt64),
	col("home_team_name", schema.TypeString),
	col("home_team_abbreviation", schema.TypeString),
	col("home_score", schema.TypeInt64),
	col("away_team_id", schema.TypeInt64),
	col("away_team_name", schema.TypeString),
	col("away_team_abbreviation", schema.TypeString),
	col("away_score", schema.TypeInt64),
	col("competitions", schema.TypeJSON),
	col("event", schema.TypeJSON),
}

var eventColumns = scoreboardColumns

var competitorColumns = []schema.Column{
	col("event_id", schema.TypeInt64),
	col("competition_id", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("uid", schema.TypeString),
	col("type", schema.TypeString),
	col("order", schema.TypeInt64),
	col("home_away", schema.TypeString),
	col("winner", schema.TypeBoolean),
	col("score", schema.TypeInt64),
	col("curated_rank", schema.TypeInt64),
	col("team_uid", schema.TypeString),
	col("team_location", schema.TypeString),
	col("team_name", schema.TypeString),
	col("team_display_name", schema.TypeString),
	col("team_short_display_name", schema.TypeString),
	col("team_abbreviation", schema.TypeString),
	col("team_color", schema.TypeString),
	col("team_alternate_color", schema.TypeString),
	col("team_logo", schema.TypeString),
	col("records", schema.TypeJSON),
	col("statistics", schema.TypeJSON),
	col("linescores", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("competitor", schema.TypeJSON),
}

var standingColumns = []schema.Column{
	col("league_id", schema.TypeInt64),
	col("league_name", schema.TypeString),
	col("league_abbreviation", schema.TypeString),
	col("group_id", schema.TypeInt64),
	col("group_name", schema.TypeString),
	col("group_abbreviation", schema.TypeString),
	col("season", schema.TypeInt64),
	col("season_type", schema.TypeInt64),
	col("team_id", schema.TypeInt64),
	col("team_uid", schema.TypeString),
	col("team_name", schema.TypeString),
	col("team_abbreviation", schema.TypeString),
	col("rank", schema.TypeFloat64),
	col("playoff_seed", schema.TypeFloat64),
	col("wins", schema.TypeFloat64),
	col("losses", schema.TypeFloat64),
	col("ties", schema.TypeFloat64),
	col("win_percent", schema.TypeFloat64),
	col("points_for", schema.TypeFloat64),
	col("points_against", schema.TypeFloat64),
	col("point_differential", schema.TypeFloat64),
	col("games_behind", schema.TypeFloat64),
	col("streak", schema.TypeString),
	col("overall_record", schema.TypeString),
	col("stats", schema.TypeJSON),
	col("team", schema.TypeJSON),
	col("standings_group", schema.TypeJSON),
}

var newsColumns = []schema.Column{
	col("id", schema.TypeInt64),
	col("now_id", schema.TypeString),
	col("content_key", schema.TypeString),
	col("type", schema.TypeString),
	col("headline", schema.TypeString),
	col("description", schema.TypeString),
	col("published", schema.TypeTimestampTZ),
	col("last_modified", schema.TypeTimestampTZ),
	col("premium", schema.TypeBoolean),
	col("byline", schema.TypeString),
	col("link", schema.TypeString),
	col("image", schema.TypeString),
	col("categories", schema.TypeJSON),
	col("images", schema.TypeJSON),
	col("article", schema.TypeJSON),
}
