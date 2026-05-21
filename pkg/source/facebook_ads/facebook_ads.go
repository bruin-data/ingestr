package facebook_ads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://graph.facebook.com/v21.0"

	maxPageSize   = 100
	maxIDPageSize = 500

	// Facebook Marketing API rate limits depend on the app's access tier:
	//   - development_access: stricter limits, shared with other apps on the account
	//   - standard_access: max score of 9000 per 300s window, read calls cost 1 point each (~30 req/s)
	// We keep this conservative to work safely on both tiers.
	rateLimit      = 5
	rateLimitBurst = 3

	retryAttempts = 12
	retryBackoff  = 2 * time.Second
	retryMaxWait  = 120 * time.Second
)

// Facebook API error codes that are transient and should be retried.
// See: https://developers.facebook.com/docs/graph-api/guides/error-handling/
var retryableErrorCodes = map[int]bool{
	1:   true, // Unknown error
	2:   true, // Temporary issue
	4:   true, // Too many calls
	17:  true, // Too many calls to account
	32:  true, // Page request limit reached
	341: true, // Temporarily blocked for policies violations
	613: true, // Calls to this API have exceeded the rate limit

	// Async/batch error codes
	80000: true, 80001: true, 80002: true, 80003: true,
	80004: true, 80005: true, 80006: true,
	800008: true, 800009: true, 80014: true,
}

func facebookRetryCondition(resp *gonghttp.Response, err error) bool {
	if resp == nil {
		return false
	}

	var fbErr struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(resp.Body(), &fbErr); jsonErr != nil {
		return false
	}

	if retryableErrorCodes[fbErr.Error.Code] {
		config.Debug("[FACEBOOK_ADS] Retrying due to error code %d: %s", fbErr.Error.Code, fbErr.Error.Message)
		return true
	}

	return false
}

var supportedTables = []string{
	"campaigns",
	"ad_sets",
	"ads",
	"ad_creatives",
	"leads",
	"facebook_insights",
}

type FacebookAdsSource struct {
	accessToken string
	accountID   string
	client      *gonghttp.Client
}

func NewFacebookAdsSource() *FacebookAdsSource {
	return &FacebookAdsSource{}
}

func (s *FacebookAdsSource) HandlesIncrementality() bool {
	return true
}

func (s *FacebookAdsSource) Schemes() []string {
	return []string{"facebookads"}
}

func (s *FacebookAdsSource) Connect(ctx context.Context, uri string) error {
	accessToken, accountID, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = accessToken
	s.accountID = accountID

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRetry(retryAttempts, retryBackoff, retryMaxWait),
		gonghttp.WithRetryCondition(facebookRetryCondition),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(s.accessToken)),
	)

	config.Debug("[FACEBOOK_ADS] Connected to account: %s", s.accountID)
	return nil
}

func (s *FacebookAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FacebookAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	var tableName string
	var tableAccountIDs []string
	var ic *insightsConfig

	if strings.HasPrefix(req.Name, "facebook_insights") || strings.HasPrefix(req.Name, "ads_insights") {
		tableName = "facebook_insights"
		var parsedIC insightsConfig
		var err error
		if strings.HasPrefix(req.Name, "ads_insights") {
			// Treat "ads_insights_X" as "facebook_insights:ads_insights_X"
			tableAccountIDs, parsedIC, err = parseInsightsTableName("facebook_insights:" + req.Name)
		} else {
			tableAccountIDs, parsedIC, err = parseInsightsTableName(req.Name)
		}
		if err != nil {
			return nil, err
		}
		ic = &parsedIC
	} else {
		tableName, tableAccountIDs = parseTableName(req.Name)
	}

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	accountIDs := s.resolveAccountIDs(tableAccountIDs)
	if len(accountIDs) == 0 {
		return nil, fmt.Errorf("account_id is required: provide it in the URI (?account_id=...) or table name (e.g. campaigns:1234567890)")
	}

	incrementalKey := ""
	strategy := config.StrategyMerge
	primaryKeys := []string{"id"}

	switch tableName {
	case "campaigns", "ad_sets", "ads":
		incrementalKey = "updated_time"
	case "leads":
		incrementalKey = "created_time"
		primaryKeys = []string{"id", "created_time"}
	case "facebook_insights":
		incrementalKey = "date_start"
		primaryKeys = ic.buildPrimaryKeys()
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("facebook_ads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			if ic != nil {
				return s.readInsightsStream(ctx, accountIDs, *ic, opts)
			}
			return s.read(ctx, tableName, accountIDs, opts)
		},
	}, nil
}

// parseTableName splits "campaigns:1234567890,9876543210" into ("campaigns", ["1234567890", "9876543210"]).
// If no colon is present, the account IDs slice is empty.
func parseTableName(raw string) (table string, accountIDs []string) {
	parts := strings.SplitN(raw, ":", 2)
	table = parts[0]
	if len(parts) == 2 && parts[1] != "" {
		for _, id := range strings.Split(parts[1], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				accountIDs = append(accountIDs, id)
			}
		}
	}
	return table, accountIDs
}

// parseInsightsTableName parses the complex insights table name format.
// Supported formats:
//   - facebook_insights
//   - facebook_insights:ads_insights_age_and_gender
//   - facebook_insights:ads_insights_country:impressions,clicks,spend
//   - facebook_insights:age,gender:impressions,clicks,spend
//   - facebook_insights:campaign,age:impressions,clicks,spend
//   - facebook_insights_with_account_ids:123,456
//   - facebook_insights_with_account_ids:123,456:ads_insights_age_and_gender
//   - facebook_insights_with_account_ids:123,456:ads_insights:impressions,clicks
func parseInsightsTableName(raw string) (accountIDs []string, ic insightsConfig, err error) {
	parts := strings.Split(raw, ":")
	prefix := parts[0]
	parts = parts[1:]

	if prefix == "facebook_insights_with_account_ids" {
		if len(parts) == 0 {
			return nil, ic, fmt.Errorf("account IDs required for facebook_insights_with_account_ids")
		}
		for _, id := range strings.Split(parts[0], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				accountIDs = append(accountIDs, id)
			}
		}
		parts = parts[1:]
	}

	if len(parts) == 0 {
		return accountIDs, ic, nil
	}

	first := parts[0]
	if _, ok := predefinedBreakdowns[first]; ok {
		ic.breakdown = first
		if len(parts) > 1 {
			for _, f := range strings.Split(parts[1], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					ic.fields = append(ic.fields, f)
				}
			}
		}
	} else {
		for _, d := range strings.Split(first, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if validLevels[d] {
				ic.level = d
			} else {
				ic.dimensions = append(ic.dimensions, d)
			}
		}
		if len(parts) > 1 {
			for _, f := range strings.Split(parts[1], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					ic.fields = append(ic.fields, f)
				}
			}
		}
	}

	return accountIDs, ic, nil
}

// resolveAccountIDs returns the account IDs from the table name if provided,
// otherwise falls back to the one from the URI. Ensures the "act_" prefix on each.
func (s *FacebookAdsSource) resolveAccountIDs(tableAccountIDs []string) []string {
	ids := tableAccountIDs
	if len(ids) == 0 && s.accountID != "" {
		ids = []string{s.accountID}
	}
	resolved := make([]string, 0, len(ids))
	for _, id := range ids {
		if !strings.HasPrefix(id, "act_") {
			id = "act_" + id
		}
		resolved = append(resolved, id)
	}
	return resolved
}

// accountEndpoint builds an API path like "/{account_id}/campaigns".
func (s *FacebookAdsSource) accountEndpoint(accountID, edge string) string {
	return fmt.Sprintf("/%s/%s", accountID, edge)
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func parseURI(uri string) (accessToken, accountID string, err error) {
	var rest string
	switch {
	case strings.HasPrefix(uri, "facebookads://"):
		rest = strings.TrimPrefix(uri, "facebookads://")
	default:
		return "", "", fmt.Errorf("invalid facebook_ads URI: must start with facebookads://")
	}
	parts := strings.SplitN(rest, "?", 2)

	if len(parts) < 2 {
		return "", "", fmt.Errorf("facebook_ads URI must include query parameters (facebook_ads://?access_token=...)")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("failed to parse facebook_ads URI query: %w", err)
	}

	accessToken = values.Get("access_token")
	if accessToken == "" {
		return "", "", fmt.Errorf("access_token is required in facebook_ads URI")
	}

	accountID = values.Get("account_id")
	if accountID != "" && !strings.HasPrefix(accountID, "act_") {
		accountID = "act_" + accountID
	}

	return accessToken, accountID, nil
}

func (s *FacebookAdsSource) read(ctx context.Context, table string, accountIDs []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "campaigns":
			err = s.readCampaigns(ctx, accountIDs, opts, results)
		case "ad_sets":
			err = s.readAdSets(ctx, accountIDs, opts, results)
		case "ads":
			err = s.readAds(ctx, accountIDs, opts, results)
		case "ad_creatives":
			err = s.readAdCreatives(ctx, accountIDs, opts, results)
		case "leads":
			err = s.readLeads(ctx, accountIDs, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

var defaultFields = []string{
	"id", "updated_time", "created_time", "name", "status", "effective_status",
}

var campaignFields = strings.Join(append(
	defaultFields,
	"objective", "start_time", "stop_time", "daily_budget", "lifetime_budget",
), ",")

var adSetFields = strings.Join(append(
	defaultFields,
	"campaign_id", "start_time", "end_time", "daily_budget", "lifetime_budget",
	"optimization_goal", "promoted_object", "billing_event", "bid_amount", "bid_strategy", "targeting",
), ",")

var adFields = strings.Join(append(
	defaultFields,
	"adset_id", "campaign_id", "creative", "targeting", "tracking_specs", "conversion_specs",
), ",")

var adCreativeFields = "id,name,status,thumbnail_url,object_story_spec,effective_object_story_id,call_to_action_type,object_type,template_url,url_tags,instagram_actor_id,product_set_id"

var leadFields = "id,created_time,ad_id,ad_name,adset_id,adset_name,campaign_id,campaign_name,form_id,field_data"

var defaultInsightsFields = strings.Join([]string{
	"campaign_id", "adset_id", "ad_id", "date_start", "date_stop",
	"reach", "impressions", "frequency", "clicks", "unique_clicks",
	"ctr", "unique_ctr", "cpc", "cpm", "cpp", "spend",
	"actions", "action_values", "cost_per_action_type",
	"website_ctr", "account_currency",
	"ad_click_actions", "ad_name", "adset_name", "campaign_name",
	"full_view_impressions", "full_view_reach",
	"inline_link_click_ctr", "outbound_clicks", "social_spend",
	"conversions", "video_thruplay_watched_actions",
}, ",")

type insightsConfig struct {
	breakdown  string   // predefined breakdown name (e.g. "ads_insights_age_and_gender")
	dimensions []string // custom dimensions (e.g. ["age", "gender"])
	fields     []string // custom fields (nil = use defaults)
	level      string   // account/campaign/adset/ad
}

func (ic *insightsConfig) buildPrimaryKeys() []string {
	level := ic.level
	if level == "" {
		level = "ad"
	}

	var base []string
	switch level {
	case "account":
		base = []string{"account_id", "date_start"}
	case "campaign":
		base = []string{"campaign_id", "date_start"}
	case "adset":
		base = []string{"campaign_id", "adset_id", "date_start"}
	default: // "ad"
		base = []string{"campaign_id", "adset_id", "ad_id", "date_start"}
	}

	var dims []string
	if ic.breakdown != "" {
		if bds, ok := predefinedBreakdowns[ic.breakdown]; ok {
			dims = bds
		}
	} else if len(ic.dimensions) > 0 {
		dims = ic.dimensions
	}
	if len(dims) > 0 {
		base = append(base, dims...)
	}
	return base
}

func (ic *insightsConfig) requiredAPIFields() []string {
	level := ic.level
	if level == "" {
		level = "ad"
	}
	switch level {
	case "account":
		return []string{"account_id", "date_start", "date_stop"}
	case "campaign":
		return []string{"campaign_id", "date_start", "date_stop"}
	case "adset":
		return []string{"campaign_id", "adset_id", "date_start", "date_stop"}
	default:
		return []string{"campaign_id", "adset_id", "ad_id", "date_start", "date_stop"}
	}
}

var predefinedBreakdowns = map[string][]string{
	"ads_insights":                     nil,
	"ads_insights_age_and_gender":      {"age", "gender"},
	"ads_insights_country":             {"country"},
	"ads_insights_platform_and_device": {"publisher_platform", "platform_position", "impression_device"},
	"ads_insights_region":              {"region"},
	"ads_insights_dma":                 {"dma"},
	"ads_insights_hourly_advertiser":   {"hourly_stats_aggregated_by_advertiser_time_zone"},
}

var validLevels = map[string]bool{
	"account":  true,
	"campaign": true,
	"adset":    true,
	"ad":       true,
}

// invalidInsightsFields are breakdown dimension names that must be removed from the
// fields parameter when used as breakdowns, to avoid requesting them in both places.
var invalidInsightsFields = map[string]bool{
	"impression_device":  true,
	"publisher_platform": true,
	"platform_position":  true,
	"age":                true,
	"gender":             true,
	"country":            true,
	"placement":          true,
	"region":             true,
	"dma":                true,
	"hourly_stats_aggregated_by_advertiser_time_zone": true,
}

type edgeConfig struct {
	edge            string // API edge name (e.g. "campaigns", "adsets")
	fields          string // comma-separated fields to request
	filterField     string // filtering field (e.g. "campaign.updated_time")
	useUpdatedSince bool   // use updated_since query param instead of filtering (ad_sets, ads)
	endTimeField    string // field name for client-side end-date filtering (e.g. "updated_time")
}

type facebookPageResponse struct {
	Data   []map[string]interface{} `json:"data"`
	Paging *struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

func (s *FacebookAdsSource) readCampaigns(ctx context.Context, accountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readEdgeForAccounts(ctx, accountIDs, edgeConfig{
		edge:        "campaigns",
		fields:      campaignFields,
		filterField: "campaign.updated_time",
	}, opts, results)
}

func (s *FacebookAdsSource) readAdSets(ctx context.Context, accountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readEdgeForAccounts(ctx, accountIDs, edgeConfig{
		edge:            "adsets",
		fields:          adSetFields,
		useUpdatedSince: true,
		endTimeField:    "updated_time",
	}, opts, results)
}

func (s *FacebookAdsSource) readAds(ctx context.Context, accountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readEdgeForAccounts(ctx, accountIDs, edgeConfig{
		edge:        "ads",
		fields:      adFields,
		filterField: "ad.updated_time",
	}, opts, results)
}

func (s *FacebookAdsSource) readAdCreatives(ctx context.Context, accountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, accountID := range accountIDs {
		// Phase 1: collect unique creative IDs via lightweight /ads?fields=creative{id} (limit=500, tiny payload)
		creativeIDs, err := s.listCreativeIDs(ctx, accountID, opts)
		if err != nil {
			return err
		}
		config.Debug("[FACEBOOK_ADS] Found %d unique creative IDs for account %s", len(creativeIDs), accountID)

		// Phase 2: batch-fetch full creative data using /?ids=id1,id2,...
		total := 0
		const batchSize = 50
		for i := 0; i < len(creativeIDs); i += batchSize {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			end := i + batchSize
			if end > len(creativeIDs) {
				end = len(creativeIDs)
			}
			batch := creativeIDs[i:end]

			resp, err := s.client.R(ctx).
				SetQueryParam("ids", strings.Join(batch, ",")).
				SetQueryParam("fields", adCreativeFields).
				Get(baseURL)
			if err != nil {
				return fmt.Errorf("failed to batch-fetch creatives: %w", err)
			}

			logUsageHeaders(resp)

			if !resp.IsSuccess() {
				return fmt.Errorf("failed to batch-fetch creatives with status %d: %s", resp.StatusCode(), resp.String())
			}

			// Response is a map of id -> creative object
			var batchResult map[string]map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &batchResult); err != nil {
				return fmt.Errorf("failed to parse batch creatives response: %w", err)
			}

			var items []map[string]interface{}
			for _, creative := range batchResult {
				items = append(items, creative)
			}

			if opts.Limit > 0 && total+len(items) > opts.Limit {
				items = items[:opts.Limit-total]
			}

			if len(items) > 0 {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, nil)
				if err != nil {
					return fmt.Errorf("failed to convert ad creatives to Arrow: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				total += len(items)
			}

			config.Debug("[FACEBOOK_ADS] Fetched creatives batch %d-%d of %d", i+1, end, len(creativeIDs))

			if opts.Limit > 0 && total >= opts.Limit {
				config.Debug("[FACEBOOK_ADS] Reached limit of %d ad creatives", opts.Limit)
				return nil
			}
		}

		config.Debug("[FACEBOOK_ADS] Fetched %d unique ad creatives for account %s", total, accountID)
	}
	return nil
}

func (s *FacebookAdsSource) listCreativeIDs(ctx context.Context, accountID string, opts source.ReadOptions) ([]string, error) {
	seen := make(map[string]bool)
	var ids []string
	var afterCursor string

	filtering := buildFiltering("ad.updated_time", opts)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("fields", "creative{id}").
			SetQueryParam("limit", strconv.Itoa(maxIDPageSize))

		if afterCursor != "" {
			req.SetQueryParam("after", afterCursor)
		}

		if filtering != "" {
			req.SetQueryParam("filtering", filtering)
		}

		resp, err := req.Get(s.accountEndpoint(accountID, "ads"))
		if err != nil {
			return nil, fmt.Errorf("failed to list creative IDs: %w", err)
		}

		logUsageHeaders(resp)

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to list creative IDs with status %d: %s", resp.StatusCode(), resp.String())
		}

		var page facebookPageResponse
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return nil, fmt.Errorf("failed to parse creative IDs response: %w", err)
		}

		if len(page.Data) == 0 {
			break
		}

		for _, ad := range page.Data {
			creative, ok := ad["creative"].(map[string]interface{})
			if !ok {
				continue
			}
			id, ok := creative["id"].(string)
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}

		config.Debug("[FACEBOOK_ADS] Listed %d unique creative IDs so far (%d ads scanned)", len(ids), len(seen))

		if page.Paging == nil || page.Paging.Next == "" {
			break
		}
		afterCursor = page.Paging.Cursors.After
	}

	return ids, nil
}

func (s *FacebookAdsSource) readLeads(ctx context.Context, accountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for _, accountID := range accountIDs {
		// Step 1: find lead gen campaigns (OUTCOME_LEADS or LEAD_GENERATION objective)
		leadCampaignIDs, err := s.listLeadGenCampaignIDs(ctx, accountID)
		if err != nil {
			return fmt.Errorf("failed to list lead gen campaigns: %w", err)
		}

		if len(leadCampaignIDs) == 0 {
			config.Debug("[FACEBOOK_ADS] No lead gen campaigns found for account %s, skipping leads", accountID)
			continue
		}

		config.Debug("[FACEBOOK_ADS] Found %d lead gen campaigns for account %s", len(leadCampaignIDs), accountID)

		// Step 2: list ads from those campaigns only
		adIDs, err := s.listAdIDsForCampaigns(ctx, accountID, leadCampaignIDs, opts)
		if err != nil {
			return fmt.Errorf("failed to list ads for leads: %w", err)
		}

		if len(adIDs) == 0 {
			config.Debug("[FACEBOOK_ADS] No ads found in lead gen campaigns for account %s", accountID)
			continue
		}

		config.Debug("[FACEBOOK_ADS] Found %d ads in lead gen campaigns for account %s, fetching leads", len(adIDs), accountID)

		// Step 3: fetch leads for those ads
		var wg sync.WaitGroup
		sem := make(chan struct{}, rateLimitBurst)
		errs := make(chan error, len(adIDs))

		for _, adID := range adIDs {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := s.paginateAndSend(ctx, id, edgeConfig{
					edge:        "leads",
					fields:      leadFields,
					filterField: "time_created",
				}, opts, results); err != nil {
					errs <- err
				}
			}(adID)
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			return err
		}
	}
	return nil
}

func (s *FacebookAdsSource) listLeadGenCampaignIDs(ctx context.Context, accountID string) ([]string, error) {
	var campaignIDs []string
	var afterCursor string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("fields", "id,objective").
			SetQueryParam("limit", strconv.Itoa(maxIDPageSize))

		if afterCursor != "" {
			req.SetQueryParam("after", afterCursor)
		}

		resp, err := req.Get(s.accountEndpoint(accountID, "campaigns"))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch campaigns: %w", err)
		}

		logUsageHeaders(resp)

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to fetch campaigns with status %d: %s", resp.StatusCode(), resp.String())
		}

		var page facebookPageResponse
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return nil, fmt.Errorf("failed to parse campaigns response: %w", err)
		}

		if len(page.Data) == 0 {
			break
		}

		for _, item := range page.Data {
			objective, _ := item["objective"].(string)
			if objective == "OUTCOME_LEADS" || objective == "LEAD_GENERATION" {
				if id, ok := item["id"].(string); ok {
					campaignIDs = append(campaignIDs, id)
				}
			}
		}

		if page.Paging == nil || page.Paging.Next == "" {
			break
		}
		afterCursor = page.Paging.Cursors.After
	}

	return campaignIDs, nil
}

func (s *FacebookAdsSource) listAdIDsForCampaigns(ctx context.Context, accountID string, campaignIDs []string, opts source.ReadOptions) ([]string, error) {
	var adIDs []string
	var afterCursor string

	filtering := buildFiltering("ad.updated_time", opts)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("fields", "id").
			SetQueryParam("limit", strconv.Itoa(maxIDPageSize))

		if afterCursor != "" {
			req.SetQueryParam("after", afterCursor)
		}

		// Combine date filtering with campaign filtering
		var allFilters []map[string]string
		if filtering != "" {
			var dateFilters []map[string]string
			if err := json.Unmarshal([]byte(filtering), &dateFilters); err != nil {
				return nil, fmt.Errorf("failed to parse date filtering: %w", err)
			}
			allFilters = append(allFilters, dateFilters...)
		}
		allFilters = append(allFilters, map[string]string{
			"field": "campaign.id", "operator": "IN", "value": "[" + strings.Join(campaignIDs, ",") + "]",
		})
		combined, err := json.Marshal(allFilters)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal filters: %w", err)
		}
		req.SetQueryParam("filtering", string(combined))

		resp, err := req.Get(s.accountEndpoint(accountID, "ads"))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch ads: %w", err)
		}

		logUsageHeaders(resp)

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to fetch ads with status %d: %s", resp.StatusCode(), resp.String())
		}

		var page facebookPageResponse
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return nil, fmt.Errorf("failed to parse ads response: %w", err)
		}

		if len(page.Data) == 0 {
			break
		}

		for _, item := range page.Data {
			if id, ok := item["id"].(string); ok {
				adIDs = append(adIDs, id)
			}
		}

		if page.Paging == nil || page.Paging.Next == "" {
			break
		}
		afterCursor = page.Paging.Cursors.After
	}

	config.Debug("[FACEBOOK_ADS] Listed %d ads in lead gen campaigns for account %s", len(adIDs), accountID)
	return adIDs, nil
}

func (s *FacebookAdsSource) readInsightsStream(ctx context.Context, accountIDs []string, ic insightsConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	var fieldList []string
	var breakdowns string
	hasBreakdowns := false

	if ic.breakdown != "" {
		if bds, ok := predefinedBreakdowns[ic.breakdown]; ok && len(bds) > 0 {
			breakdowns = strings.Join(bds, ",")
			hasBreakdowns = true
		}
		if len(ic.fields) > 0 {
			fieldList = ic.fields
		}
	} else if len(ic.dimensions) > 0 {
		breakdowns = strings.Join(ic.dimensions, ",")
		hasBreakdowns = true
		if len(ic.fields) > 0 {
			fieldList = ic.fields
		}
	} else if len(ic.fields) > 0 {
		fieldList = ic.fields
	}

	if len(fieldList) == 0 {
		fieldList = strings.Split(defaultInsightsFields, ",")
	}

	// Always include the ID/date fields required for merge PKs at the given level
	existing := make(map[string]bool, len(fieldList))
	for _, f := range fieldList {
		existing[f] = true
	}
	for _, rf := range ic.requiredAPIFields() {
		if !existing[rf] {
			fieldList = append(fieldList, rf)
		}
	}

	if hasBreakdowns {
		fieldList = filterInvalidInsightsFields(fieldList)
	}

	fields := strings.Join(fieldList, ",")

	go func() {
		defer close(results)

		var wg sync.WaitGroup
		sem := make(chan struct{}, rateLimitBurst)
		errs := make(chan error, len(accountIDs))

		for _, accountID := range accountIDs {
			wg.Add(1)
			go func(accID string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := s.fetchInsights(ctx, accID, fields, breakdowns, ic.level, opts, results); err != nil {
					errs <- err
				}
			}(accountID)
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			results <- source.RecordBatchResult{Err: err}
			return
		}
	}()

	return results, nil
}

const insightsChunkDays = 1

const (
	asyncPollInitial = 3 * time.Second
	asyncPollMax     = 20 * time.Second
	asyncStartMax    = 30 * time.Minute
	asyncFinishMax   = 4 * time.Hour
)

func (s *FacebookAdsSource) fetchInsights(ctx context.Context, accountID, fields, breakdowns, level string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FACEBOOK_ADS] Reading insights for account %s (breakdowns=%s, level=%s)", accountID, breakdowns, level)

	chunks := buildTimeChunks(opts.IntervalStart, opts.IntervalEnd, insightsChunkDays)
	totalSent := 0

	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.fetchInsightsAsync(ctx, accountID, fields, breakdowns, level, chunk, opts, &totalSent, results); err != nil {
			return err
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[FACEBOOK_ADS] Reached limit of %d records across chunks", opts.Limit)
			return nil
		}
	}

	config.Debug("[FACEBOOK_ADS] Finished reading insights for %s", accountID)
	return nil
}

type asyncReportResponse struct {
	ReportRunID string `json:"report_run_id"`
}

type asyncReportStatus struct {
	ID                 string `json:"id"`
	AsyncStatus        string `json:"async_status"`
	AsyncPercentComple int    `json:"async_percent_completion"`
}

func (s *FacebookAdsSource) fetchInsightsAsync(ctx context.Context, accountID, fields, breakdowns, level, timeRange string, opts source.ReadOptions, totalSent *int, results chan<- source.RecordBatchResult) error {
	config.Debug("[FACEBOOK_ADS] Creating async insights job for %s (time_range=%s)", accountID, timeRange)

	// 1. Create async report job via POST
	req := s.client.R(ctx).
		SetQueryParam("fields", fields).
		SetQueryParam("time_increment", "1")

	if breakdowns != "" {
		req.SetQueryParam("breakdowns", breakdowns)
	}
	if level == "" {
		level = "ad"
	}
	req.SetQueryParam("level", level)
	if timeRange != "" {
		req.SetQueryParam("time_range", timeRange)
	}

	resp, err := req.Post(s.accountEndpoint(accountID, "insights"))
	if err != nil {
		return fmt.Errorf("failed to create insights job: %w", err)
	}

	logUsageHeaders(resp)

	if !resp.IsSuccess() {
		return fmt.Errorf("failed to create insights job with status %d: %s", resp.StatusCode(), resp.String())
	}

	var report asyncReportResponse
	if err := json.Unmarshal(resp.Body(), &report); err != nil {
		return fmt.Errorf("failed to parse insights job response: %w", err)
	}

	if report.ReportRunID == "" {
		return fmt.Errorf("no report_run_id returned for insights job")
	}

	config.Debug("[FACEBOOK_ADS] Async job created: %s", report.ReportRunID)

	// 2. Poll until job completes
	if err := s.pollAsyncJob(ctx, report.ReportRunID); err != nil {
		return fmt.Errorf("insights job %s failed: %w", report.ReportRunID, err)
	}

	// 3. Fetch results
	return s.fetchAsyncResults(ctx, report.ReportRunID, timeRange, opts, totalSent, results)
}

func (s *FacebookAdsSource) pollAsyncJob(ctx context.Context, reportRunID string) error {
	pollInterval := asyncPollInitial
	startedAt := time.Now()
	jobStarted := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}

		resp, err := s.client.R(ctx).Get(baseURL + "/" + reportRunID)
		if err != nil {
			return fmt.Errorf("failed to poll job: %w", err)
		}

		logUsageHeaders(resp)

		if !resp.IsSuccess() {
			return fmt.Errorf("failed to poll job with status %d: %s", resp.StatusCode(), resp.String())
		}

		var status asyncReportStatus
		if err := json.Unmarshal(resp.Body(), &status); err != nil {
			return fmt.Errorf("failed to parse job status: %w", err)
		}

		config.Debug("[FACEBOOK_ADS] Job %s: %s (%d%%)", reportRunID, status.AsyncStatus, status.AsyncPercentComple)

		switch status.AsyncStatus {
		case "Job Completed":
			return nil
		case "Job Failed", "Job Skipped":
			return fmt.Errorf("job ended with status: %s", status.AsyncStatus)
		case "Job Not Started":
			if !jobStarted && time.Since(startedAt) > asyncStartMax {
				return fmt.Errorf("job did not start within %v", asyncStartMax)
			}
		default:
			jobStarted = true
			if time.Since(startedAt) > asyncFinishMax {
				return fmt.Errorf("job did not finish within %v", asyncFinishMax)
			}
		}

		// Exponential backoff capped at asyncPollMax
		pollInterval *= 2
		if pollInterval > asyncPollMax {
			pollInterval = asyncPollMax
		}
	}
}

// isTransientInsightsError reports whether a 400 response body indicates that
// the async insights job has not finished materializing results yet
// (error_subcode 1815107 / code 2637 with is_transient: true). Facebook can
// return this for a short window even after async_status="Job Completed".
func isTransientInsightsError(body []byte) bool {
	var env struct {
		Error struct {
			Code         int  `json:"code"`
			ErrorSubcode int  `json:"error_subcode"`
			IsTransient  bool `json:"is_transient"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return env.Error.ErrorSubcode == 1815107 || (env.Error.Code == 2637 && env.Error.IsTransient)
}

func (s *FacebookAdsSource) fetchAsyncResults(ctx context.Context, reportRunID, timeRange string, opts source.ReadOptions, totalSent *int, results chan<- source.RecordBatchResult) error {
	var afterCursor string
	batchNum := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp *gonghttp.Response
		// Facebook sometimes reports async_status="Job Completed" but the
		// /insights endpoint still returns 400 with error_subcode 1815107
		// (is_transient: true) for a brief window. Retry with backoff.
		backoff := 5 * time.Second
		const maxFetchAttempts = 8
		var lastErr error
		for attempt := 1; attempt <= maxFetchAttempts; attempt++ {
			req := s.client.R(ctx).
				SetQueryParam("limit", strconv.Itoa(maxIDPageSize))

			if afterCursor != "" {
				req.SetQueryParam("after", afterCursor)
			}

			r, err := req.Get(baseURL + "/" + reportRunID + "/insights")
			if err != nil {
				return fmt.Errorf("failed to fetch insights results: %w", err)
			}

			logUsageHeaders(r)

			if r.IsSuccess() {
				resp = r
				break
			}

			if r.StatusCode() == 400 && isTransientInsightsError(r.Body()) {
				lastErr = fmt.Errorf("status %d: %s", r.StatusCode(), r.String())
				if attempt == maxFetchAttempts {
					break
				}
				config.Debug("[FACEBOOK_ADS] transient insights error on job %s (attempt %d/%d), retrying in %s", reportRunID, attempt, maxFetchAttempts, backoff)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoff):
				}
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			}

			return fmt.Errorf("failed to fetch insights results with status %d: %s", r.StatusCode(), r.String())
		}
		if resp == nil {
			return fmt.Errorf("failed to fetch insights results after %d attempts: %v", maxFetchAttempts, lastErr)
		}

		var page facebookPageResponse
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return fmt.Errorf("failed to parse insights results: %w", err)
		}

		if len(page.Data) == 0 {
			break
		}

		items := page.Data
		if opts.Limit > 0 && *totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-*totalSent]
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert insights to Arrow: %w", err)
			}

			batchNum++
			*totalSent += len(items)
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[FACEBOOK_ADS] insights %s: sent batch %d (%d rows)", timeRange, batchNum, len(items))
		}

		if opts.Limit > 0 && *totalSent >= opts.Limit {
			config.Debug("[FACEBOOK_ADS] Reached limit of %d records", opts.Limit)
			return nil
		}

		if page.Paging == nil || page.Paging.Next == "" {
			break
		}

		afterCursor = page.Paging.Cursors.After
	}

	return nil
}

// buildTimeChunks splits the interval into daily windows.
// If no interval is provided, returns a single chunk with no time bounds.
func buildTimeChunks(intervalStart, intervalEnd *time.Time, chunkDays int) []string {
	var start, end time.Time
	if intervalStart != nil {
		start = *intervalStart
	}
	if intervalEnd != nil {
		end = *intervalEnd
	}

	if start.IsZero() && end.IsZero() {
		// no time range — single chunk without time_range
		return []string{""}
	}

	now := time.Now()
	if start.IsZero() {
		start = now.AddDate(0, -37, 0) // Facebook retention period
	}
	if end.IsZero() {
		end = now
	}

	var chunks []string
	for current := start; !current.After(end); current = current.AddDate(0, 0, chunkDays) {
		chunkEnd := current.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}

		tr, _ := json.Marshal(map[string]string{
			"since": current.Format("2006-01-02"),
			"until": chunkEnd.Format("2006-01-02"),
		})
		chunks = append(chunks, string(tr))
		config.Debug("[FACEBOOK_ADS] time chunk: %s", string(tr))
	}

	return chunks
}

func toTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

func (s *FacebookAdsSource) readEdgeForAccounts(ctx context.Context, accountIDs []string, ec edgeConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, rateLimitBurst)
	errs := make(chan error, len(accountIDs))

	for _, accountID := range accountIDs {
		wg.Add(1)
		go func(accID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := s.paginateAndSend(ctx, accID, ec, opts, results); err != nil {
				errs <- err
			}
		}(accountID)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		return err
	}
	return nil
}

func (s *FacebookAdsSource) paginateAndSend(ctx context.Context, accountID string, ec edgeConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FACEBOOK_ADS] Reading %s for account %s", ec.edge, accountID)

	totalSent := 0
	batchNum := 0
	var afterCursor string

	var updatedSince string
	if ec.useUpdatedSince && opts.IntervalStart != nil {
		updatedSince = strconv.FormatInt(opts.IntervalStart.Unix(), 10)
	}

	filtering := buildFiltering(ec.filterField, opts)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("fields", ec.fields).
			SetQueryParam("limit", strconv.Itoa(maxPageSize))

		if afterCursor != "" {
			req.SetQueryParam("after", afterCursor)
		}

		if ec.useUpdatedSince {
			if updatedSince != "" {
				req.SetQueryParam("updated_since", updatedSince)
			}
		} else if filtering != "" {
			req.SetQueryParam("filtering", filtering)
		}

		resp, err := req.Get(s.accountEndpoint(accountID, ec.edge))
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", ec.edge, err)
		}

		logUsageHeaders(resp)

		if !resp.IsSuccess() {
			return fmt.Errorf("failed to fetch %s with status %d: %s", ec.edge, resp.StatusCode(), resp.String())
		}

		var page facebookPageResponse
		if err := json.Unmarshal(resp.Body(), &page); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", ec.edge, err)
		}

		if len(page.Data) == 0 {
			break
		}

		items := page.Data

		if ec.useUpdatedSince && ec.endTimeField != "" && opts.IntervalEnd != nil {
			items = filterByEndTime(items, ec.endTimeField, opts.IntervalEnd)
		}

		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", ec.edge, err)
			}

			batchNum++
			totalSent += len(items)
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[FACEBOOK_ADS] %s: sent batch %d, total rows: %d", ec.edge, batchNum, totalSent)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[FACEBOOK_ADS] Reached limit of %d records", opts.Limit)
			break
		}

		if page.Paging == nil || page.Paging.Next == "" {
			break
		}

		afterCursor = page.Paging.Cursors.After
	}

	config.Debug("[FACEBOOK_ADS] Finished reading %s, total records: %d", ec.edge, totalSent)
	return nil
}

func buildFiltering(filterField string, opts source.ReadOptions) string {
	if filterField == "" || (opts.IntervalStart == nil && opts.IntervalEnd == nil) {
		return ""
	}

	var filters []map[string]string

	if ts := toUnixTimestamp(opts.IntervalStart); ts != "" {
		// Facebook API doesn't support GREATER_THAN_OR_EQUAL on time fields,
		// so we subtract 1 second to make GREATER_THAN effectively inclusive.
		if v, err := strconv.ParseInt(ts, 10, 64); err == nil {
			ts = strconv.FormatInt(v-1, 10)
		}
		filters = append(filters, map[string]string{
			"field":    filterField,
			"operator": "GREATER_THAN",
			"value":    ts,
		})
		config.Debug("[FACEBOOK_ADS] Filter: %s => %s ", filterField, ts)
	}

	if ts := toUnixTimestamp(opts.IntervalEnd); ts != "" {
		// Facebook API doesn't support LESS_THAN_OR_EQUAL on time fields.
		// When the end date is given as a date (midnight), shift to end-of-day (+ 86400s)
		// so that the entire day is included. Otherwise shift +1s for timestamp precision.
		if v, err := strconv.ParseInt(ts, 10, 64); err == nil {
			if v%86400 == 0 {
				ts = strconv.FormatInt(v+86400, 10)
			} else {
				ts = strconv.FormatInt(v+1, 10)
			}
		}
		filters = append(filters, map[string]string{
			"field":    filterField,
			"operator": "LESS_THAN",
			"value":    ts,
		})
		config.Debug("[FACEBOOK_ADS] Filter: %s <= %s ", filterField, ts)
	}

	if len(filters) == 0 {
		return ""
	}

	b, err := json.Marshal(filters)
	if err != nil {
		return ""
	}
	return string(b)
}

// filterByEndTime removes items whose endTimeField is after the given end time.
// Facebook API returns timestamps as ISO 8601 strings (e.g. "2026-01-15T10:30:00+0000").
func filterByEndTime(items []map[string]interface{}, field string, endTime *time.Time) []map[string]interface{} {
	end := toTime(endTime)
	if end.IsZero() {
		return items
	}

	// If end is midnight, include the entire day.
	if end.Hour() == 0 && end.Minute() == 0 && end.Second() == 0 {
		end = end.AddDate(0, 0, 1)
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		val, ok := item[field]
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		str, ok := val.(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		t := parseFacebookTime(str)
		if t.IsZero() || t.Before(end) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// parseFacebookTime parses Facebook API timestamp formats.
func parseFacebookTime(s string) time.Time {
	formats := []string{
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func toUnixTimestamp(v *time.Time) string {
	if v == nil || v.IsZero() {
		return ""
	}
	return strconv.FormatInt(v.Unix(), 10)
}

func filterInvalidInsightsFields(fields []string) []string {
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if !invalidInsightsFields[f] {
			result = append(result, f)
		}
	}
	return result
}

func logUsageHeaders(resp *gonghttp.Response) {
	if h := resp.Header().Get("X-App-Usage"); h != "" {
		config.Debug("[FACEBOOK_ADS] X-App-Usage: %s", h)
	}
	if h := resp.Header().Get("X-Ad-Account-Usage"); h != "" {
		config.Debug("[FACEBOOK_ADS] X-Ad-Account-Usage: %s", h)
	}
	if h := resp.Header().Get("X-Business-Use-Case-Usage"); h != "" {
		config.Debug("[FACEBOOK_ADS] X-Business-Use-Case-Usage: %s", h)
	}
}

var _ source.Source = (*FacebookAdsSource)(nil)
