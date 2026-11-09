package appleads

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/golang-jwt/jwt/v5"
	"resty.dev/v3"
)

const (
	apiBaseURL         = "https://api.ads.apple.com/v1"
	oauthTokenURL      = "https://appleid.apple.com/auth/oauth2/token"
	jwtAudience        = "https://appleid.apple.com"
	jwtExpDuration     = 30 * 24 * time.Hour
	accessTokenSkew    = 5 * time.Minute
	maxPageSize        = 1000 // query endpoints reject pageSize > 1000
	reportPageSize     = 1000 // report endpoints allow up to 5000
	rateLimit          = 10.0 // Apple returns actual limits in X-Rate-Limit header; 10 req/s is a conservative default
	rateLimitBurst     = 5
	oauthScope         = "searchadsorg"
	contextHeader      = "X-AP-Context"
	defaultParallelism = 5
)

var supportedTables = []string{
	"campaigns",
	"ad_groups",
	"ads",
	"creatives",
	"campaign_reports",
	"ad_group_reports",
	"ad_reports",
}

const queryTimeLayout = "2006-01-02T15:04:05.000"

type AppleAdsSource struct {
	clientID    string
	teamID      string
	keyID       string
	orgIDs      []string
	privateKey  string
	client      *httpclient.Client
	accessToken string
	tokenExpiry time.Time
}

func NewAppleAdsSource() *AppleAdsSource {
	return &AppleAdsSource{}
}

func (s *AppleAdsSource) Schemes() []string {
	return []string{"appleads"}
}

func (s *AppleAdsSource) HandlesIncrementality() bool {
	return true
}

func (s *AppleAdsSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.clientID = cfg.clientID
	s.teamID = cfg.teamID
	s.keyID = cfg.keyID
	s.orgIDs = cfg.orgIDs
	s.privateKey = cfg.privateKey

	if err := s.refreshAccessToken(ctx); err != nil {
		return fmt.Errorf("failed to obtain access token: %w", err)
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(apiBaseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.accessToken)),
		httpclient.WithDebug(config.DebugMode),
	)
	config.Debug("[APPLEADS] Connected successfully (adAccountIds=%v)", s.orgIDs)
	return nil
}

func (s *AppleAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type parsedConfig struct {
	clientID   string
	teamID     string
	keyID      string
	orgIDs     []string
	privateKey string
}

func parseURI(uri string) (parsedConfig, error) {
	var cfg parsedConfig
	if !strings.HasPrefix(uri, "appleads://") {
		return cfg, fmt.Errorf("invalid appleads URI: must start with appleads://")
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse appleads URI: %w", err)
	}
	q := parsed.Query()

	cfg.clientID = q.Get("client_id")
	if cfg.clientID == "" {
		return cfg, fmt.Errorf("client_id is required in appleads URI")
	}
	cfg.teamID = q.Get("team_id")
	if cfg.teamID == "" {
		return cfg, fmt.Errorf("team_id is required in appleads URI")
	}
	cfg.keyID = q.Get("key_id")
	if cfg.keyID == "" {
		return cfg, fmt.Errorf("key_id is required in appleads URI")
	}
	// The Platform API scopes requests by ad account instead of organization. On
	// accounts migrated from v5 the ad account id equals the old org id, so
	// existing URIs with org_id keep working.
	raw := q.Get("ad_account_id")
	if raw == "" {
		raw = q.Get("org_id")
	}
	if raw == "" {
		return cfg, fmt.Errorf("ad_account_id (or org_id) is required in appleads URI (comma-separate for multiple)")
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			cfg.orgIDs = append(cfg.orgIDs, part)
		}
	}

	keyPath := q.Get("key_path")
	keyB64 := q.Get("key_base64")
	if keyPath == "" && keyB64 == "" {
		return cfg, fmt.Errorf("key_path or key_base64 is required in appleads URI")
	}
	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return cfg, fmt.Errorf("failed to read private key file %s: %w", keyPath, err)
		}
		cfg.privateKey = string(data)
	} else {
		decoded, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			return cfg, fmt.Errorf("failed to decode key_base64: %w", err)
		}
		cfg.privateKey = string(decoded)
	}

	return cfg, nil
}

func (s *AppleAdsSource) signJWT() (string, error) {
	privateKey, err := jwt.ParseECPrivateKeyFromPEM([]byte(s.privateKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse EC private key: %w", err)
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    s.teamID,
		Subject:   s.clientID,
		Audience:  jwt.ClaimStrings{jwtAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtExpDuration)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = s.keyID
	return tok.SignedString(privateKey)
}

func (s *AppleAdsSource) refreshAccessToken(ctx context.Context) error {
	assertion, err := s.signJWT()
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", s.clientID)
	form.Set("client_secret", assertion)
	form.Set("scope", oauthScope)

	client := resty.New().SetTimeout(60 * time.Second)
	defer func() {
		_ = client.Close()
	}()

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Host", "appleid.apple.com").
		SetBody(form.Encode()).
		Post(oauthTokenURL)
	if err != nil {
		return fmt.Errorf("oauth token request failed: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("oauth token request returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Bytes(), &body); err != nil {
		return fmt.Errorf("failed to parse oauth token response: %w", err)
	}
	if body.AccessToken == "" {
		return fmt.Errorf("oauth token response missing access_token")
	}

	s.accessToken = body.AccessToken
	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	s.tokenExpiry = time.Now().Add(ttl)
	config.Debug("[APPLEADS] access_token refreshed, expires in %s", ttl)
	return nil
}

func (s *AppleAdsSource) ensureAccessToken(ctx context.Context) error {
	if s.accessToken != "" && time.Until(s.tokenExpiry) > accessTokenSkew {
		return nil
	}
	if err := s.refreshAccessToken(ctx); err != nil {
		return err
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	s.client = httpclient.New(
		httpclient.WithBaseURL(apiBaseURL),
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(httpclient.NewBearerAuth(s.accessToken)),
		httpclient.WithDebug(config.DebugMode),
	)
	return nil
}

var validGranularities = map[string]string{
	"hourly":  "HOURLY",
	"daily":   "DAILY",
	"weekly":  "WEEKLY",
	"monthly": "MONTHLY",
}

var validGroupByFields = map[string]bool{
	"countryOrRegion": true,
	"ageRange":        true,
	"gender":          true,
	"deviceClass":     true,
	"adminArea":       true,
	"locality":        true,
	"countryCode":     true,
}

type reportTableConfig struct {
	baseTable   string
	granularity string
	groupBy     []string
}

func parseReportTableName(raw string) (reportTableConfig, error) {
	parts := strings.SplitN(raw, ":", 3)
	cfg := reportTableConfig{baseTable: parts[0]}

	if len(parts) >= 2 && parts[1] != "" {
		g, ok := validGranularities[strings.ToLower(parts[1])]
		if !ok {
			return cfg, fmt.Errorf("invalid granularity %q (supported: hourly, daily, weekly, monthly)", parts[1])
		}
		cfg.granularity = g
	}

	if len(parts) == 3 && parts[2] != "" {
		for _, field := range strings.Split(parts[2], ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if !validGroupByFields[field] {
				return cfg, fmt.Errorf("invalid groupBy field %q (supported: countryOrRegion, ageRange, gender, deviceClass, adminArea, locality, countryCode)", field)
			}
			cfg.groupBy = append(cfg.groupBy, field)
		}
	}

	return cfg, nil
}

func (s *AppleAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	var rc reportTableConfig

	baseTable := strings.SplitN(tableName, ":", 2)[0]
	if !isValidTable(baseTable) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	_, isReport := reportEndpoints[baseTable]
	primaryKeys := []string{"orgId", "id"}

	if isReport {
		var err error
		rc, err = parseReportTableName(tableName)
		if err != nil {
			return nil, err
		}

		if baseTable == "ad_reports" && rc.granularity == "HOURLY" {
			return nil, fmt.Errorf("ad_reports do not support hourly granularity")
		}

		// Ads reports only support groupBy on countryOrRegion.
		if baseTable == "ad_reports" && len(rc.groupBy) > 0 {
			for _, g := range rc.groupBy {
				if g != "countryOrRegion" {
					return nil, fmt.Errorf("ad_reports only support groupBy on countryOrRegion, got %q", g)
				}
			}
		}

		switch baseTable {
		case "campaign_reports":
			primaryKeys = []string{"orgId", "campaignId"}
		case "ad_group_reports":
			primaryKeys = []string{"orgId", "campaignId", "adGroupId"}
		case "ad_reports":
			primaryKeys = []string{"orgId", "campaignId", "adId"}
		}
		if rc.granularity != "" {
			primaryKeys = append(primaryKeys, "date")
		}
		primaryKeys = append(primaryKeys, rc.groupBy...)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: "modificationTime",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("appleads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			if isReport {
				return s.readReport(ctx, baseTable, opts, rc)
			}
			return s.read(ctx, baseTable, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *AppleAdsSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		if err := s.ensureAccessToken(ctx); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to refresh access token: %w", err)}
			return
		}
		var err error
		switch table {
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "ad_groups":
			err = s.readAdGroups(ctx, opts, results)
		case "ads":
			err = s.readAds(ctx, opts, results)
		case "creatives":
			err = s.readCreatives(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func accountContext(accountID string) string {
	return "adAccountId=" + accountID
}

// queryAll reads a Platform API `POST …/query` list to the end. List responses
// carry no total count and `pagination.pageSize` is the number of items
// returned, so the only reliable end marker is an empty page.
func (s *AppleAdsSource) queryAll(ctx context.Context, accountID, path string, filters []map[string]interface{}, emit func([]map[string]interface{}) error) error {
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		reqBody := map[string]interface{}{
			"pagination": map[string]interface{}{
				"offset":   offset,
				"pageSize": maxPageSize,
			},
		}
		if len(filters) > 0 {
			reqBody["filters"] = filters
		}
		resp, err := s.client.R(ctx).
			SetHeader(contextHeader, accountContext(accountID)).
			SetHeader("Content-Type", "application/json").
			SetBody(reqBody).
			Post(path)
		if err != nil {
			return fmt.Errorf("POST %s (ad account %s) failed: %w", path, accountID, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("POST %s (ad account %s) returned status %d: %s", path, accountID, resp.StatusCode(), resp.String())
		}
		var body struct {
			Result []map[string]interface{} `json:"result"`
		}
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", path, err)
		}
		if len(body.Result) == 0 {
			return nil
		}
		for _, item := range body.Result {
			item["orgId"] = accountID
		}
		if emit != nil {
			if err := emit(body.Result); err != nil {
				return err
			}
		}
		offset += len(body.Result)
	}
}

// queryWithDeleted returns live and soft-deleted entities. The Platform API
// leaves deleted entities out unless they are asked for explicitly, while the
// v5 source always included them.
func (s *AppleAdsSource) queryWithDeleted(ctx context.Context, accountID, path string, filters []map[string]interface{}, emit func([]map[string]interface{}) error) error {
	if err := s.queryAll(ctx, accountID, path, filters, emit); err != nil {
		return err
	}
	deleted := append(append([]map[string]interface{}{}, filters...), map[string]interface{}{
		"field":    "deleted",
		"operator": "EQUALS",
		"value":    true,
	})
	return s.queryAll(ctx, accountID, path, deleted, emit)
}

func campaignFilter(campaignID string) []map[string]interface{} {
	id, err := strconv.ParseInt(campaignID, 10, 64)
	var value interface{} = campaignID
	if err == nil {
		value = id
	}
	return []map[string]interface{}{{"field": "campaignId", "operator": "EQUALS", "value": value}}
}

// clientFilterInterval keeps items whose filterKey falls in (start, end).
// Incremental windows are applied client-side: which fields accept range
// filters varies by endpoint, and entity lists are small.
func clientFilterInterval(items []map[string]interface{}, filterKey string, start, end *time.Time) []map[string]interface{} {
	if filterKey == "" || (start == nil && end == nil) {
		return items
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		raw, ok := it[filterKey].(string)
		if !ok || raw == "" {
			out = append(out, it)
			continue
		}
		ts, err := time.Parse(queryTimeLayout, raw)
		if err != nil {
			out = append(out, it)
			continue
		}
		if start != nil && !ts.After(*start) {
			continue
		}
		if end != nil && !ts.Before(*end) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func extractID(item map[string]interface{}) (string, bool) {
	switch v := item["id"].(type) {
	case string:
		return v, v != ""
	case float64:
		return strconv.FormatInt(int64(v), 10), true
	case json.Number:
		return v.String(), true
	}
	return "", false
}

func (s *AppleAdsSource) fetchCampaignIDs(ctx context.Context, accountID string) ([]string, error) {
	var ids []string
	err := s.queryWithDeleted(ctx, accountID, "/campaigns/query", nil, func(items []map[string]interface{}) error {
		for _, c := range items {
			if id, ok := extractID(c); ok {
				ids = append(ids, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func emitPage(label string, opts source.ReadOptions, results chan<- source.RecordBatchResult, items []map[string]interface{}) error {
	if len(items) == 0 {
		return nil
	}
	flush := func(batch []map[string]interface{}) error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		config.Debug("[APPLEADS] %s: sent %d records", label, len(batch))
		return nil
	}
	if opts.MaxBatchBytes <= 0 {
		return flush(items)
	}
	var batch []map[string]interface{}
	var accBytes int64
	for _, row := range items {
		rowBytes := arrowconv.RowBytes(row)
		if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
			if err := flush(batch); err != nil {
				return err
			}
			batch = nil
			accBytes = 0
		}
		accBytes += rowBytes
		batch = append(batch, row)
	}
	return flush(batch)
}

func (s *AppleAdsSource) emitFiltered(label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) func([]map[string]interface{}) error {
	return func(items []map[string]interface{}) error {
		items = clientFilterInterval(items, opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd)
		return emitPage(label, opts, results, items)
	}
}

func (s *AppleAdsSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[APPLEADS] reading campaigns")
	for _, accountID := range s.orgIDs {
		if err := s.queryWithDeleted(ctx, accountID, "/campaigns/query", nil, s.emitFiltered("campaigns", opts, results)); err != nil {
			return fmt.Errorf("failed to fetch campaigns for ad account %s: %w", accountID, err)
		}
	}
	return nil
}

// readPerCampaign reads an entity that the Platform API scopes by campaignId.
func (s *AppleAdsSource) readPerCampaign(ctx context.Context, label, path string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[APPLEADS] reading %s", label)
	for _, accountID := range s.orgIDs {
		campaignIDs, err := s.fetchCampaignIDs(ctx, accountID)
		if err != nil {
			return fmt.Errorf("failed to list campaigns for %s (ad account %s): %w", label, accountID, err)
		}
		for _, cid := range campaignIDs {
			if err := s.queryWithDeleted(ctx, accountID, path, campaignFilter(cid), s.emitFiltered(label, opts, results)); err != nil {
				return fmt.Errorf("failed to fetch %s for campaign %s (ad account %s): %w", label, cid, accountID, err)
			}
		}
	}
	return nil
}

func (s *AppleAdsSource) readAdGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPerCampaign(ctx, "ad_groups", "/adgroups/query", opts, results)
}

func (s *AppleAdsSource) readAds(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPerCampaign(ctx, "ads", "/ads/query", opts, results)
}

func (s *AppleAdsSource) readCreatives(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[APPLEADS] reading creatives")
	for _, accountID := range s.orgIDs {
		if err := s.queryWithDeleted(ctx, accountID, "/creatives/query", nil, s.emitFiltered("creatives", opts, results)); err != nil {
			return fmt.Errorf("failed to fetch creatives for ad account %s: %w", accountID, err)
		}
	}
	return nil
}

// Apple API constraints per granularity:
//
//	HOURLY  — chunk ≤ 7 days, startTime ≤ 30 days in the past
//	DAILY   — chunk ≤ 90 days, startTime ≤ 90 days in the past
//	WEEKLY  — chunk > 14 days and ≤ 365 days, startTime ≤ 24 months in the past
//	MONTHLY — chunk > 3 months and ≤ 24 months, startTime ≤ 24 months in the past
var granularityMaxDays = map[string]int{
	"HOURLY":  7,
	"DAILY":   90,
	"WEEKLY":  365,
	"MONTHLY": 730,
}

var granularityMaxStartDays = map[string]int{
	"HOURLY":  30,
	"DAILY":   90,
	"WEEKLY":  730,
	"MONTHLY": 730,
}

var granularityMinDays = map[string]int{
	"WEEKLY":  14,
	"MONTHLY": 90,
}

var reportEndpoints = map[string]struct {
	path        string
	perCampaign bool
	idColumn    string
}{
	// Campaign reports take no campaignId filter and return the whole ad account.
	"campaign_reports": {path: "/reports/apps/campaigns/query", idColumn: "campaignId"},
	// Ad group and ad reports require a campaignId filter.
	"ad_group_reports": {path: "/reports/apps/adgroups/query", perCampaign: true, idColumn: "adGroupId"},
	"ad_reports":       {path: "/reports/apps/ads/query", perCampaign: true, idColumn: "adId"},
}

func (s *AppleAdsSource) readReport(ctx context.Context, table string, opts source.ReadOptions, rc reportTableConfig) (<-chan source.RecordBatchResult, error) {
	ep, ok := reportEndpoints[table]
	if !ok {
		return nil, fmt.Errorf("no report endpoint for table %q", table)
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		if err := s.ensureAccessToken(ctx); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to refresh access token: %w", err)}
			return
		}

		config.Debug("[APPLEADS] reading %s report (granularity=%s, groupBy=%v)", table, rc.granularity, rc.groupBy)
		start, end, err := reportDateRange(opts, rc.granularity)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}

		var tasks []reportTask
		for _, accountID := range s.orgIDs {
			if !ep.perCampaign {
				tasks = append(tasks, reportTask{orgID: accountID, endpoint: ep.path, idColumn: ep.idColumn})
				continue
			}
			campaignIDs, err := s.fetchCampaignIDs(ctx, accountID)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to list campaigns for %s report (ad account %s): %w", table, accountID, err)}
				return
			}
			for _, cid := range campaignIDs {
				tasks = append(tasks, reportTask{orgID: accountID, endpoint: ep.path, idColumn: ep.idColumn, filters: campaignFilter(cid)})
			}
		}

		if err := s.fetchReportsParallel(ctx, tasks, rc, start, end, table, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()
	return results, nil
}

func reportDateRange(opts source.ReadOptions, granularity string) (time.Time, time.Time, error) {
	now := time.Now().UTC()

	lookback := granularityMaxDays[granularity]
	if lookback == 0 {
		lookback = 90
	}
	end := now
	start := now.AddDate(0, 0, -lookback)

	if opts.IntervalStart != nil {
		start = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		end = *opts.IntervalEnd
	}

	maxStartDays := granularityMaxStartDays[granularity]
	if maxStartDays > 0 {
		earliest := now.AddDate(0, 0, -maxStartDays)
		if start.Before(earliest) {
			return time.Time{}, time.Time{}, fmt.Errorf("start date %s is too far in the past for %s granularity (max %d days ago, earliest allowed: %s)", start.Format("2006-01-02"), granularity, maxStartDays, earliest.Format("2006-01-02"))
		}
	}

	if minDays := granularityMinDays[granularity]; minDays > 0 {
		days := int(end.Sub(start).Hours() / 24)
		if days <= minDays {
			return time.Time{}, time.Time{}, fmt.Errorf("date range is %d days but %s granularity requires more than %d days", days, granularity, minDays)
		}
	}

	return start, end, nil
}

type dateRange struct{ start, end time.Time }

// reportChunks splits [start, end] into windows the API accepts. Both ends of
// a Platform API timeRange are inclusive, so a window of maxDays covers
// start .. start+maxDays-1 and the next one begins the day after.
func reportChunks(start, end time.Time, granularity string) []dateRange {
	maxDays := granularityMaxDays[granularity]
	if maxDays <= 0 {
		return []dateRange{{start, end}}
	}
	var chunks []dateRange
	for cs := start; !cs.After(end); cs = cs.AddDate(0, 0, maxDays) {
		ce := cs.AddDate(0, 0, maxDays-1)
		if ce.After(end) {
			ce = end
		}
		chunks = append(chunks, dateRange{cs, ce})
	}
	return chunks
}

func reportRequestBody(rc reportTableConfig, chunk dateRange, filters []map[string]interface{}, offset int) map[string]interface{} {
	startDay := chunk.start.UTC().Format("2006-01-02")
	endDay := chunk.end.UTC().Format("2006-01-02")
	timeRange := map[string]interface{}{
		"start":    startDay,
		"end":      endDay,
		"timeZone": "UTC",
	}
	// A one-day window must be sent without granularity; its figures then come
	// back in totalMetrics only.
	if rc.granularity != "" && startDay != endDay {
		timeRange["granularity"] = rc.granularity
	}
	body := map[string]interface{}{
		"timeRange": timeRange,
		"pagination": map[string]interface{}{
			"offset":   offset,
			"pageSize": reportPageSize,
		},
	}
	if len(filters) > 0 {
		body["filters"] = filters
	}
	if len(rc.groupBy) > 0 {
		body["groupBy"] = rc.groupBy
	} else {
		// Same as v5's returnRecordsWithNoMetrics; Apple rejects it together with groupBy.
		body["options"] = map[string]interface{}{"includeRows": []string{"EMPTY_METRICS"}}
	}
	return body
}

type reportRow struct {
	Metadata        map[string]interface{}   `json:"metadata"`
	TotalMetrics    map[string]interface{}   `json:"totalMetrics"`
	GranularMetrics []map[string]interface{} `json:"granularMetrics"`
}

// flattenReportRows turns report rows into one record per (entity, period).
// Report metadata carries the entity's own id as `id`; it is copied to
// idColumn so the primary keys stay campaignId / adGroupId / adId.
func flattenReportRows(rows []reportRow, accountID, idColumn string, rc reportTableConfig, chunk dateRange) []map[string]interface{} {
	singleDay := chunk.start.UTC().Format("2006-01-02") == chunk.end.UTC().Format("2006-01-02")
	var out []map[string]interface{}
	for _, row := range rows {
		var metricsList []map[string]interface{}
		switch {
		case rc.granularity != "" && singleDay:
			if row.TotalMetrics != nil {
				m := make(map[string]interface{}, len(row.TotalMetrics)+1)
				for k, v := range row.TotalMetrics {
					m[k] = v
				}
				if _, ok := m["date"]; !ok {
					m["date"] = chunk.start.UTC().Format("2006-01-02")
				}
				metricsList = []map[string]interface{}{m}
			}
		case rc.granularity != "":
			metricsList = row.GranularMetrics
		case row.TotalMetrics != nil:
			metricsList = []map[string]interface{}{row.TotalMetrics}
		}
		for _, metrics := range metricsList {
			flat := make(map[string]interface{})
			for k, v := range row.Metadata {
				flat[k] = v
			}
			for k, v := range metrics {
				flat[k] = v
			}
			flat["orgId"] = accountID
			if idColumn != "" {
				if id, ok := row.Metadata["id"]; ok {
					flat[idColumn] = id
				}
			}
			for _, g := range rc.groupBy {
				if flat[g] == nil {
					flat[g] = ""
				}
			}
			out = append(out, flat)
		}
	}
	return out
}

func (s *AppleAdsSource) fetchReport(ctx context.Context, task reportTask, rc reportTableConfig, start, end time.Time, emit func([]map[string]interface{}) error) error {
	for _, chunk := range reportChunks(start, end, rc.granularity) {
		offset := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			resp, err := s.client.R(ctx).
				SetHeader(contextHeader, accountContext(task.orgID)).
				SetHeader("Content-Type", "application/json").
				SetBody(reportRequestBody(rc, chunk, task.filters, offset)).
				Post(task.endpoint)
			if err != nil {
				return fmt.Errorf("POST %s (ad account %s) failed: %w", task.endpoint, task.orgID, err)
			}
			if !resp.IsSuccess() {
				return fmt.Errorf("POST %s (ad account %s) returned status %d: %s", task.endpoint, task.orgID, resp.StatusCode(), resp.String())
			}

			// An empty report comes back as `result: {}` with no rows key.
			var respBody struct {
				Result struct {
					Rows []reportRow `json:"rows"`
				} `json:"result"`
				Pagination struct {
					TotalCount *int `json:"totalCount"`
				} `json:"pagination"`
			}
			if err := json.Unmarshal(resp.Body(), &respBody); err != nil {
				return fmt.Errorf("failed to parse %s response: %w", task.endpoint, err)
			}

			got := respBody.Result.Rows
			if len(got) == 0 {
				break
			}
			if rows := flattenReportRows(got, task.orgID, task.idColumn, rc, chunk); len(rows) > 0 {
				if err := emit(rows); err != nil {
					return err
				}
			}
			offset += len(got)
			if total := respBody.Pagination.TotalCount; total != nil && offset >= *total {
				break
			}
			if len(got) < reportPageSize {
				break
			}
		}
	}
	return nil
}

type reportTask struct {
	orgID    string
	endpoint string
	idColumn string
	filters  []map[string]interface{}
}

func (s *AppleAdsSource) fetchReportsParallel(ctx context.Context, tasks []reportTask, rc reportTableConfig, start, end time.Time, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(tasks) == 0 {
		return nil
	}

	parallelism := defaultParallelism
	if len(tasks) < parallelism {
		parallelism = len(tasks)
	}

	taskCh := make(chan reportTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	errCh := make(chan error, 1)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				select {
				case <-workerCtx.Done():
					return
				default:
				}
				err := s.fetchReport(workerCtx, task, rc, start, end,
					func(items []map[string]interface{}) error {
						return emitPage(label, opts, results, items)
					})
				if err != nil {
					select {
					case errCh <- fmt.Errorf("failed to fetch %s %s (org %s): %w", label, task.endpoint, task.orgID, err):
						cancel()
					default:
					}
					return
				}
			}
		}()
	}
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

var _ source.Source = (*AppleAdsSource)(nil)
