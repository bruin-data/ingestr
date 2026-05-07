package snapchatads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL            = "https://adsapi.snapchat.com/v1"
	tokenURL           = "https://accounts.snapchat.com/login/oauth2/access_token"
	defaultParallelism = 5
)

const defaultStatsFields = "impressions,spend"

type resourceLevel int

const (
	orgLevel       resourceLevel = iota // Organization-level
	adAccountLevel                      // Ad Account-level
	statsLevel                          // Stats / Measurement Data
)

type SnapchatAdsSource struct {
	client       *gonghttp.Client
	refreshToken string
	clientID     string
	clientSecret string
	orgID        string
}

type resourceConfig struct {
	Level          resourceLevel
	PrimaryKeys    []string
	IncrementalKey string
	Strategy       config.IncrementalStrategy
	Paginated      bool
}

var resourceRegistry = map[string]resourceConfig{
	"organizations":     {Level: orgLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"fundingsources":    {Level: orgLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"billingcenters":    {Level: orgLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"adaccounts":        {Level: orgLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"transactions":      {Level: orgLevel, PrimaryKeys: []string{"id"}, Strategy: config.StrategyReplace},
	"members":           {Level: orgLevel, PrimaryKeys: []string{"id"}, Strategy: config.StrategyReplace},
	"roles":             {Level: orgLevel, PrimaryKeys: []string{"id"}, Strategy: config.StrategyReplace},
	"invoices":          {Level: adAccountLevel, PrimaryKeys: []string{"invoice_id"}, IncrementalKey: "last_modified", Strategy: config.StrategyMerge},
	"campaigns":         {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge, Paginated: true},
	"adsquads":          {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge, Paginated: true},
	"ads":               {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge, Paginated: true},
	"event_details":     {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"creatives":         {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge, Paginated: true},
	"segments":          {Level: adAccountLevel, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Strategy: config.StrategyMerge},
	"campaigns_stats":   {Level: statsLevel, Strategy: config.StrategyMerge},
	"ad_accounts_stats": {Level: statsLevel, Strategy: config.StrategyMerge},
	"ads_stats":         {Level: statsLevel, Strategy: config.StrategyMerge},
	"ad_squads_stats":   {Level: statsLevel, Strategy: config.StrategyMerge},
}

var statsMetricsColumns = []schema.Column{
	{Name: "impressions", DataType: schema.TypeInt64, Nullable: true},
	{Name: "swipes", DataType: schema.TypeInt64, Nullable: true},
	{Name: "view_time_millis", DataType: schema.TypeInt64, Nullable: true},
	{Name: "screen_time_millis", DataType: schema.TypeInt64, Nullable: true},
	{Name: "quartile_1", DataType: schema.TypeInt64, Nullable: true},
	{Name: "quartile_2", DataType: schema.TypeInt64, Nullable: true},
	{Name: "quartile_3", DataType: schema.TypeInt64, Nullable: true},
	{Name: "view_completion", DataType: schema.TypeInt64, Nullable: true},
	{Name: "spend", DataType: schema.TypeInt64, Nullable: true},
	{Name: "coupon_used_local", DataType: schema.TypeInt64, Nullable: true},
	{Name: "coupon_used_usd", DataType: schema.TypeInt64, Nullable: true},
	{Name: "video_views", DataType: schema.TypeInt64, Nullable: true},
}

var statsEntityMap = map[string]struct{ entityType, plural string }{
	"campaigns_stats":   {"campaign", "campaigns"},
	"ad_accounts_stats": {"adaccount", "adaccounts"},
	"ads_stats":         {"ad", "ads"},
	"ad_squads_stats":   {"adsquad", "adsquads"},
}

var granularities = map[string]bool{
	"TOTAL": true, "DAY": true, "HOUR": true, "LIFETIME": true,
}

var breakdowns = map[string]bool{
	"ad": true, "adsquad": true, "campaign": true,
}

var dimensions = map[string]bool{
	"GEO": true, "DEMO": true, "INTEREST": true, "DEVICE": true,
}

var pivots = map[string]bool{
	"country": true, "region": true, "dma": true,
	"gender": true, "age_bucket": true,
	"interest_category_id": true, "interest_category_name": true,
	"operating_system": true, "make": true, "model": true,
}

func NewSnapchatAdsSource() *SnapchatAdsSource {
	return &SnapchatAdsSource{}
}

func (s *SnapchatAdsSource) Schemes() []string {
	return []string{"snapchatads"}
}

func (s *SnapchatAdsSource) Connect(ctx context.Context, uri string) error {
	refreshToken, clientID, clientSecret, orgID, err := parseSnapchatAdsURI(uri)
	if err != nil {
		return err
	}

	s.refreshToken = refreshToken
	s.clientID = clientID
	s.clientSecret = clientSecret
	s.orgID = orgID

	accessToken, err := s.exchangeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to obtain access token: %w", err)
	}

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(10, 10),
		gonghttp.WithRetry(12, 2*time.Second, 120*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(accessToken)),
		gonghttp.WithHeader("Accept", "application/json"),
	)

	config.Debug("[SnapchatAds] Connected successfully")
	return nil
}

func (s *SnapchatAdsSource) exchangeToken(ctx context.Context) (string, error) {
	tokenClient := gonghttp.New(
		gonghttp.WithTimeout(30*time.Second),
		gonghttp.WithRetry(3, 1*time.Second, 10*time.Second),
	)
	defer func() { _ = tokenClient.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
	}

	resp, err := tokenClient.R(ctx).
		SetFormData(map[string]string{
			"refresh_token": s.refreshToken,
			"client_id":     s.clientID,
			"client_secret": s.clientSecret,
			"grant_type":    "refresh_token",
		}).
		SetResult(&result).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("token request returned status %d: %s", resp.StatusCode(), resp.String())
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("no access_token in token response")
	}

	config.Debug("[SnapchatAds] Access token obtained successfully")
	return result.AccessToken, nil
}

func parseSnapchatAdsURI(uri string) (refreshToken, clientID, clientSecret, orgID string, err error) {
	if !strings.HasPrefix(uri, "snapchatads://") {
		return "", "", "", "", fmt.Errorf("invalid snapchat ads URI: must start with snapchatads://")
	}

	rest := strings.TrimPrefix(uri, "snapchatads://")
	if rest == "" || rest == "?" {
		return "", "", "", "", fmt.Errorf("refresh_token, client_id, and client_secret are required in snapchat ads URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse snapchat ads URI query: %w", err)
	}

	refreshToken = values.Get("refresh_token")
	if refreshToken == "" {
		return "", "", "", "", fmt.Errorf("refresh_token is required in snapchat ads URI")
	}

	clientID = values.Get("client_id")
	if clientID == "" {
		return "", "", "", "", fmt.Errorf("client_id is required in snapchat ads URI")
	}

	clientSecret = values.Get("client_secret")
	if clientSecret == "" {
		return "", "", "", "", fmt.Errorf("client_secret is required in snapchat ads URI")
	}

	orgID = values.Get("organization_id")

	return refreshToken, clientID, clientSecret, orgID, nil
}

func (s *SnapchatAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SnapchatAdsSource) HandlesIncrementality() bool {
	return true
}

type statsConfig struct {
	granularity string
	fields      string
	breakdown   string
	dimension   string
	pivot       string
}

func (s *SnapchatAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	parts := strings.SplitN(req.Name, ":", 2)
	resourceName := parts[0]
	param := ""
	if len(parts) == 2 {
		param = parts[1]
	}

	rc, ok := resourceRegistry[resourceName]
	if !ok {
		return nil, fmt.Errorf("unsupported snapchat ads resource: %s", resourceName)
	}

	var adAccountIDs []string
	var sc *statsConfig

	if rc.Level == statsLevel {
		if param == "" {
			return nil, fmt.Errorf(
				"'%s' requires granularity and metrics, use format: %s:<granularity>,<metrics> (e.g. %s:DAY:impressions,spend)",
				resourceName, resourceName, resourceName,
			)
		}
		parsed, err := parseStatsTable(req.Name)
		if err != nil {
			return nil, err
		}
		resourceName = parsed.resourceName
		sc = &parsed.config

		entity := statsEntityMap[resourceName]
		pks := []string{entity.entityType + "_id"}
		if sc.breakdown != "" {
			pks = append(pks, sc.breakdown+"_id")
		}
		pks = append(pks, "start_time", "end_time")
		rc.PrimaryKeys = pks

		if s.orgID == "" {
			return nil, fmt.Errorf("organization_id is required for '%s'", resourceName)
		}
	} else {
		if param != "" {
			for _, id := range strings.Split(param, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					adAccountIDs = append(adAccountIDs, id)
				}
			}
			if len(adAccountIDs) == 0 {
				return nil, fmt.Errorf("ad_account_id must be provided in format '%s:ad_account_id'", resourceName)
			}
		}

		if resourceName != "organizations" && s.orgID == "" {
			return nil, fmt.Errorf("organization_id is required for table '%s'; only 'organizations' does not require organization_id", resourceName)
		}
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    rc.PrimaryKeys,
		TableIncrementalKey: rc.IncrementalKey,
		TableStrategy:       rc.Strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("snapchat ads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, resourceName, adAccountIDs, sc, opts)
		},
	}, nil
}

func joinMapKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

type parsedStatsTable struct {
	resourceName string
	config       statsConfig
}

func parseStatsTable(table string) (*parsedStatsTable, error) {
	segments := strings.Split(table, ":")
	resource := segments[0]

	if len(segments) < 2 || strings.TrimSpace(segments[1]) == "" {
		return nil, fmt.Errorf(
			"stats table requires parameters, format: %s:<dimension-params>:<metrics>", resource,
		)
	}

	tokens := strings.Split(segments[1], ",")

	var gran, brkdwn, dim, pvt string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		upper := strings.ToUpper(tok)
		lower := strings.ToLower(tok)

		switch {
		case granularities[upper]:
			gran = upper
		case breakdowns[lower]:
			brkdwn = lower
		case dimensions[upper]:
			dim = upper
		case pivots[lower]:
			pvt = lower
		default:
			return nil, fmt.Errorf(
				"unrecognized stats parameter '%s'; must be a granularity (%s), breakdown (%s), dimension (%s), or pivot (%s)",
				tok,
				joinMapKeys(granularities),
				joinMapKeys(breakdowns),
				joinMapKeys(dimensions),
				joinMapKeys(pivots),
			)
		}
	}

	if gran == "" {
		return nil, fmt.Errorf(
			"granularity is required for stats table, format: %s:<dimension-params>:<metrics>", resource,
		)
	}

	fields := defaultStatsFields
	if len(segments) >= 3 && strings.TrimSpace(segments[2]) != "" {
		fields = strings.TrimSpace(segments[2])
	}

	return &parsedStatsTable{
		resourceName: resource,
		config: statsConfig{
			granularity: gran,
			fields:      fields,
			breakdown:   brkdwn,
			dimension:   dim,
			pivot:       pvt,
		},
	}, nil
}

func (s *SnapchatAdsSource) read(ctx context.Context, resource string, adAccountIDs []string, sc *statsConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch {
		case resourceRegistry[resource].Level == statsLevel:
			err = s.readStats(ctx, resource, sc, opts, results)
		case resource == "organizations":
			err = s.readOrganizations(ctx, opts, results)
		case resource == "adaccounts":
			err = s.readAdAccounts(ctx, s.orgID, opts, results)
		case resource == "fundingsources":
			err = s.readFundingSources(ctx, s.orgID, opts, results)
		case resource == "billingcenters":
			err = s.readBillingCenters(ctx, s.orgID, opts, results)
		case resource == "transactions":
			err = s.readTransactions(ctx, s.orgID, opts, results)
		case resource == "members":
			err = s.readMembers(ctx, s.orgID, opts, results)
		case resource == "roles":
			err = s.readRoles(ctx, s.orgID, opts, results)
		case resourceRegistry[resource].Level == adAccountLevel:
			err = s.readAdAccountResource(ctx, resource, adAccountIDs, opts, results)
		default:
			err = fmt.Errorf("unsupported resource: %s", resource)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SnapchatAdsSource) fetchItems(ctx context.Context, endpoint, wrapperKey, innerKey string, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	req := s.client.R(ctx)
	if len(params) > 0 {
		req.SetQueryParams(params)
	}
	resp, err := req.Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body(), &raw); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}

	var status string
	_ = json.Unmarshal(raw["request_status"], &status)
	if status != "SUCCESS" && status != "success" {
		return fmt.Errorf("%s request failed with status: %s", endpoint, status)
	}

	wrapperData, ok := raw[wrapperKey]
	if !ok {
		return fmt.Errorf("%s response missing key '%s'", endpoint, wrapperKey)
	}

	var wrapperItems []map[string]json.RawMessage
	if err := json.Unmarshal(wrapperData, &wrapperItems); err != nil {
		return fmt.Errorf("failed to parse %s items: %w", wrapperKey, err)
	}

	items := make([]map[string]interface{}, 0, len(wrapperItems))
	for _, wrapper := range wrapperItems {
		var subStatus string
		_ = json.Unmarshal(wrapper["sub_request_status"], &subStatus)
		if subStatus != "SUCCESS" && subStatus != "success" {
			config.Debug("[SnapchatAds] Skipping item with sub_request_status: %s", subStatus)
			continue
		}

		innerData, ok := wrapper[innerKey]
		if !ok {
			continue
		}

		var item map[string]interface{}
		if err := json.Unmarshal(innerData, &item); err != nil {
			return fmt.Errorf("failed to parse %s item: %w", innerKey, err)
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", wrapperKey, err)
	}
	results <- source.RecordBatchResult{Batch: record}

	config.Debug("[SnapchatAds] Fetched %d items from %s", len(items), endpoint)
	return nil
}

func (s *SnapchatAdsSource) fetchItemsPaginated(ctx context.Context, endpoint, wrapperKey, innerKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{"limit": "1000"}
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).SetQueryParams(params).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body(), &raw); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		var status string
		_ = json.Unmarshal(raw["request_status"], &status)
		if status != "SUCCESS" && status != "success" {
			return fmt.Errorf("%s request failed with status: %s", endpoint, status)
		}

		wrapperData, ok := raw[wrapperKey]
		if !ok {
			return fmt.Errorf("%s response missing key '%s'", endpoint, wrapperKey)
		}

		var wrapperItems []map[string]json.RawMessage
		if err := json.Unmarshal(wrapperData, &wrapperItems); err != nil {
			return fmt.Errorf("failed to parse %s items: %w", wrapperKey, err)
		}

		items := make([]map[string]interface{}, 0, len(wrapperItems))
		for _, wrapper := range wrapperItems {
			var subStatus string
			_ = json.Unmarshal(wrapper["sub_request_status"], &subStatus)
			if subStatus != "SUCCESS" && subStatus != "success" {
				continue
			}
			innerData, ok := wrapper[innerKey]
			if !ok {
				continue
			}
			var item map[string]interface{}
			if err := json.Unmarshal(innerData, &item); err != nil {
				return fmt.Errorf("failed to parse %s item: %w", innerKey, err)
			}
			items = append(items, item)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", wrapperKey, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		config.Debug("[SnapchatAds] Fetched %d items from %s (total: %d)", len(items), endpoint, totalSent)

		var paging struct {
			NextLink string `json:"next_link"`
		}
		if pagingRaw, ok := raw["paging"]; ok {
			_ = json.Unmarshal(pagingRaw, &paging)
		}
		if paging.NextLink == "" {
			break
		}
		parsed, err := url.Parse(paging.NextLink)
		if err != nil {
			break
		}
		cursor := parsed.Query().Get("cursor")
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}

	return nil
}

func (s *SnapchatAdsSource) readOrganizations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.fetchItems(ctx, "/me/organizations", "organizations", "organization", nil, opts, results)
}

func (s *SnapchatAdsSource) readAdAccounts(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/adaccounts", orgID)
	return s.fetchItems(ctx, endpoint, "adaccounts", "adaccount", nil, opts, results)
}

func (s *SnapchatAdsSource) readFundingSources(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/fundingsources", orgID)
	return s.fetchItems(ctx, endpoint, "fundingsources", "fundingsource", nil, opts, results)
}

func (s *SnapchatAdsSource) readBillingCenters(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/billingcenters", orgID)
	return s.fetchItems(ctx, endpoint, "billingcenters", "billingcenter", nil, opts, results)
}

func (s *SnapchatAdsSource) readTransactions(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/transactions", orgID)

	var params map[string]string
	if opts.IntervalStart != nil || opts.IntervalEnd != nil {
		params = make(map[string]string)
		if opts.IntervalStart != nil {
			params["start_time"] = opts.IntervalStart.Format("2006-01-02T15:04:05")
		}
		if opts.IntervalEnd != nil {
			params["end_time"] = opts.IntervalEnd.Format("2006-01-02T15:04:05")
		}
	}

	return s.fetchItems(ctx, endpoint, "transactions", "transaction", params, opts, results)
}

func (s *SnapchatAdsSource) readMembers(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/members", orgID)
	return s.fetchItems(ctx, endpoint, "members", "member", nil, opts, results)
}

func (s *SnapchatAdsSource) readRoles(ctx context.Context, orgID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/organizations/%s/roles", orgID)
	return s.fetchItemsPaginated(ctx, endpoint, "roles", "role", opts, results)
}

func (s *SnapchatAdsSource) readAdAccountResource(ctx context.Context, resource string, adAccountIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(adAccountIDs) == 0 {
		endpoint := fmt.Sprintf("/organizations/%s/adaccounts", s.orgID)
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch ad accounts: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("ad accounts returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body(), &raw); err != nil {
			return fmt.Errorf("failed to parse ad accounts response: %w", err)
		}

		var status string
		_ = json.Unmarshal(raw["request_status"], &status)
		if status != "SUCCESS" && status != "success" {
			return fmt.Errorf("ad accounts request failed with status: %s", status)
		}

		var wrapperItems []map[string]json.RawMessage
		if err := json.Unmarshal(raw["adaccounts"], &wrapperItems); err != nil {
			return fmt.Errorf("failed to parse ad accounts items: %w", err)
		}

		for _, wrapper := range wrapperItems {
			var account map[string]interface{}
			if err := json.Unmarshal(wrapper["adaccount"], &account); err != nil {
				continue
			}
			if id, ok := account["id"].(string); ok && id != "" {
				adAccountIDs = append(adAccountIDs, id)
			}
		}

		config.Debug("[SnapchatAds] Discovered %d ad accounts", len(adAccountIDs))
	}

	innerKey := strings.TrimSuffix(resource, "s")
	paginated := resourceRegistry[resource].Paginated

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	accountCh := make(chan string, defaultParallelism)
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	for i := 0; i < defaultParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for accountID := range accountCh {
				endpoint := fmt.Sprintf("/adaccounts/%s/%s", accountID, resource)
				var err error
				if paginated {
					err = s.fetchItemsPaginated(ctx, endpoint, resource, innerKey, opts, results)
				} else {
					err = s.fetchItems(ctx, endpoint, resource, innerKey, nil, opts, results)
				}
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, id := range adAccountIDs {
		select {
		case accountCh <- id:
		case <-ctx.Done():
		}
	}
	close(accountCh)

	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}

	return nil
}

func (s *SnapchatAdsSource) readStats(ctx context.Context, resource string, sc *statsConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	entity, ok := statsEntityMap[resource]
	if !ok {
		return fmt.Errorf("unsupported stats resource: %s", resource)
	}

	adAccountIDs, err := s.discoverAdAccountIDs(ctx)
	if err != nil {
		return err
	}

	params := map[string]string{
		"granularity": sc.granularity,
		"fields":      sc.fields,
	}
	if sc.granularity == "DAY" || sc.granularity == "HOUR" {
		if opts.IntervalStart != nil {
			params["start_time"] = opts.IntervalStart.Format("2006-01-02T15:04:05.000")
		}
		if opts.IntervalEnd != nil {
			end := *opts.IntervalEnd
			if end != end.Truncate(time.Hour) {
				end = end.Truncate(time.Hour).Add(time.Hour)
			}
			params["end_time"] = end.Format("2006-01-02T15:04:05.000")
		}
	}
	if sc.breakdown != "" {
		params["breakdown"] = sc.breakdown
	}
	if sc.dimension != "" {
		params["dimension"] = sc.dimension
	}
	if sc.pivot != "" {
		params["pivot"] = sc.pivot
	}

	if entity.entityType == "adaccount" {
		for _, accountID := range adAccountIDs {
			endpoint := fmt.Sprintf("/adaccounts/%s/stats", accountID)
			if err := s.fetchStats(ctx, endpoint, params, sc.granularity, opts, results); err != nil {
				return err
			}
		}
		return nil
	}

	for _, accountID := range adAccountIDs {
		entityIDs, err := s.discoverEntityIDs(ctx, accountID, entity.plural, entity.entityType)
		if err != nil {
			return err
		}
		for _, entityID := range entityIDs {
			endpoint := fmt.Sprintf("/%s/%s/stats", entity.plural, entityID)
			if err := s.fetchStats(ctx, endpoint, params, sc.granularity, opts, results); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *SnapchatAdsSource) discoverAdAccountIDs(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("/organizations/%s/adaccounts", s.orgID)
	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("ad accounts returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body(), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse ad accounts response: %w", err)
	}

	var status string
	_ = json.Unmarshal(raw["request_status"], &status)
	if status != "SUCCESS" && status != "success" {
		return nil, fmt.Errorf("ad accounts request failed with status: %s", status)
	}

	var wrapperItems []map[string]json.RawMessage
	if err := json.Unmarshal(raw["adaccounts"], &wrapperItems); err != nil {
		return nil, fmt.Errorf("failed to parse ad accounts items: %w", err)
	}

	var ids []string
	for _, wrapper := range wrapperItems {
		var account map[string]interface{}
		if err := json.Unmarshal(wrapper["adaccount"], &account); err != nil {
			continue
		}
		if id, ok := account["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}

	config.Debug("[SnapchatAds] Discovered %d ad accounts for stats", len(ids))
	return ids, nil
}

func (s *SnapchatAdsSource) discoverEntityIDs(ctx context.Context, accountID, plural, entityType string) ([]string, error) {
	var ids []string
	params := map[string]string{"limit": "1000"}

	for {
		endpoint := fmt.Sprintf("/adaccounts/%s/%s", accountID, plural)
		resp, err := s.client.R(ctx).SetQueryParams(params).Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", plural, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("%s returned status %d: %s", plural, resp.StatusCode(), resp.String())
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body(), &raw); err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", plural, err)
		}

		var wrapperItems []map[string]json.RawMessage
		if err := json.Unmarshal(raw[plural], &wrapperItems); err != nil {
			return nil, fmt.Errorf("failed to parse %s items: %w", plural, err)
		}

		for _, wrapper := range wrapperItems {
			var subStatus string
			_ = json.Unmarshal(wrapper["sub_request_status"], &subStatus)
			if subStatus != "SUCCESS" && subStatus != "success" {
				continue
			}
			var item map[string]interface{}
			if err := json.Unmarshal(wrapper[entityType], &item); err != nil {
				continue
			}
			if id, ok := item["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}

		var paging struct {
			NextLink string `json:"next_link"`
		}
		if pagingRaw, ok := raw["paging"]; ok {
			_ = json.Unmarshal(pagingRaw, &paging)
		}
		if paging.NextLink == "" {
			break
		}
		parsed, err := url.Parse(paging.NextLink)
		if err != nil {
			break
		}
		cursor := parsed.Query().Get("cursor")
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}

	config.Debug("[SnapchatAds] Discovered %d %s for account %s", len(ids), plural, accountID)
	return ids, nil
}

func (s *SnapchatAdsSource) fetchStats(ctx context.Context, endpoint string, params map[string]string, granularity string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	resp, err := s.client.R(ctx).SetQueryParams(params).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch stats %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("stats %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body(), &raw); err != nil {
		return fmt.Errorf("failed to parse stats response: %w", err)
	}

	var status string
	_ = json.Unmarshal(raw["request_status"], &status)
	if status != "SUCCESS" && status != "success" {
		return fmt.Errorf("stats request failed with status: %s", status)
	}

	var items []map[string]interface{}
	if granularity == "TOTAL" || granularity == "LIFETIME" {
		items, err = parseTotalStats(raw)
	} else {
		items, err = parseTimeseriesStats(raw)
	}
	if err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, statsMetricsColumns, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert stats to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}

	config.Debug("[SnapchatAds] Fetched %d stats records from %s", len(items), endpoint)
	return nil
}

func parseTotalStats(raw map[string]json.RawMessage) ([]map[string]interface{}, error) {
	var statsData json.RawMessage
	if d, ok := raw["total_stats"]; ok {
		statsData = d
	} else if d, ok := raw["lifetime_stats"]; ok {
		statsData = d
	} else {
		return nil, nil
	}

	var statItems []map[string]json.RawMessage
	if err := json.Unmarshal(statsData, &statItems); err != nil {
		return nil, fmt.Errorf("failed to parse total stats: %w", err)
	}

	var items []map[string]interface{}
	for _, statItem := range statItems {
		var subStatus string
		_ = json.Unmarshal(statItem["sub_request_status"], &subStatus)
		if subStatus != "SUCCESS" && subStatus != "success" {
			continue
		}

		var statData json.RawMessage
		if d, ok := statItem["total_stat"]; ok {
			statData = d
		} else if d, ok := statItem["lifetime_stat"]; ok {
			statData = d
		} else {
			continue
		}

		var stat map[string]json.RawMessage
		if err := json.Unmarshal(statData, &stat); err != nil {
			continue
		}

		baseRecord := make(map[string]interface{})

		var entityID string
		_ = json.Unmarshal(stat["id"], &entityID)
		var entityType string
		_ = json.Unmarshal(stat["type"], &entityType)

		addMetadataFields(baseRecord, stat, "", "")

		var parentStats map[string]interface{}
		if sRaw, ok := stat["stats"]; ok {
			_ = json.Unmarshal(sRaw, &parentStats)
		}
		for k, v := range parentStats {
			baseRecord[k] = v
		}

		var breakdownStats map[string]json.RawMessage
		if bsRaw, ok := stat["breakdown_stats"]; ok {
			_ = json.Unmarshal(bsRaw, &breakdownStats)
		}

		if len(breakdownStats) > 0 {
			for breakdownType, breakdownItemsRaw := range breakdownStats {
				var breakdownItems []map[string]json.RawMessage
				if err := json.Unmarshal(breakdownItemsRaw, &breakdownItems); err != nil {
					continue
				}
				for _, bItem := range breakdownItems {
					record := make(map[string]interface{})
					addSemanticEntityFields(record, entityType, entityID)

					var breakdownID string
					_ = json.Unmarshal(bItem["id"], &breakdownID)
					addSemanticEntityFields(record, breakdownType, breakdownID)

					record["start_time"] = baseRecord["start_time"]
					record["end_time"] = baseRecord["end_time"]
					record["finalized_data_end_time"] = baseRecord["finalized_data_end_time"]

					var bStats map[string]interface{}
					if sRaw, ok := bItem["stats"]; ok {
						_ = json.Unmarshal(sRaw, &bStats)
					}
					for k, v := range bStats {
						record[k] = v
					}

					normalizeStatsRecord(record)
					items = append(items, record)
				}
			}
		} else {
			addSemanticEntityFields(baseRecord, entityType, entityID)
			normalizeStatsRecord(baseRecord)
			items = append(items, baseRecord)
		}
	}

	return items, nil
}

func parseTimeseriesStats(raw map[string]json.RawMessage) ([]map[string]interface{}, error) {
	tsData, ok := raw["timeseries_stats"]
	if !ok {
		return nil, nil
	}

	var tsItems []map[string]json.RawMessage
	if err := json.Unmarshal(tsData, &tsItems); err != nil {
		return nil, fmt.Errorf("failed to parse timeseries stats: %w", err)
	}

	var items []map[string]interface{}
	for _, tsItem := range tsItems {
		var subStatus string
		_ = json.Unmarshal(tsItem["sub_request_status"], &subStatus)
		if subStatus != "SUCCESS" && subStatus != "success" {
			continue
		}

		var tsStat map[string]json.RawMessage
		if err := json.Unmarshal(tsItem["timeseries_stat"], &tsStat); err != nil {
			continue
		}

		var entityID string
		_ = json.Unmarshal(tsStat["id"], &entityID)
		var entityType string
		_ = json.Unmarshal(tsStat["type"], &entityType)

		var breakdownStats map[string]json.RawMessage
		if bsRaw, ok := tsStat["breakdown_stats"]; ok {
			_ = json.Unmarshal(bsRaw, &breakdownStats)
		}

		if len(breakdownStats) > 0 {
			for breakdownType, breakdownItemsRaw := range breakdownStats {
				var breakdownItems []map[string]json.RawMessage
				if err := json.Unmarshal(breakdownItemsRaw, &breakdownItems); err != nil {
					continue
				}
				for _, bItem := range breakdownItems {
					var breakdownID string
					_ = json.Unmarshal(bItem["id"], &breakdownID)

					var timeseries []map[string]json.RawMessage
					if tsRaw, ok := bItem["timeseries"]; ok {
						_ = json.Unmarshal(tsRaw, &timeseries)
					}

					for _, period := range timeseries {
						record := make(map[string]interface{})
						addSemanticEntityFields(record, entityType, entityID)
						addSemanticEntityFields(record, breakdownType, breakdownID)

						var startTime, endTime string
						_ = json.Unmarshal(period["start_time"], &startTime)
						_ = json.Unmarshal(period["end_time"], &endTime)
						addMetadataFields(record, tsStat, startTime, endTime)
						var stats map[string]interface{}
						if sRaw, ok := period["stats"]; ok {
							_ = json.Unmarshal(sRaw, &stats)
						}
						for k, v := range stats {
							record[k] = v
						}

						normalizeStatsRecord(record)
						items = append(items, record)
					}
				}
			}
		} else {
			var timeseries []map[string]json.RawMessage
			if tsRaw, ok := tsStat["timeseries"]; ok {
				_ = json.Unmarshal(tsRaw, &timeseries)
			}

			for _, period := range timeseries {
				record := make(map[string]interface{})
				addSemanticEntityFields(record, entityType, entityID)

				var startTime, endTime string
				_ = json.Unmarshal(period["start_time"], &startTime)
				_ = json.Unmarshal(period["end_time"], &endTime)
				addMetadataFields(record, tsStat, startTime, endTime)

				var stats map[string]interface{}
				if sRaw, ok := period["stats"]; ok {
					_ = json.Unmarshal(sRaw, &stats)
				}
				for k, v := range stats {
					record[k] = v
				}

				normalizeStatsRecord(record)
				items = append(items, record)
			}
		}
	}

	return items, nil
}

func addSemanticEntityFields(record map[string]interface{}, entityType, entityID string) {
	fieldName := strings.ToLower(entityType) + "_id"
	record[fieldName] = entityID
}

func addMetadataFields(record map[string]interface{}, stat map[string]json.RawMessage, startTime, endTime string) {
	if startTime != "" {
		record["start_time"] = startTime
	} else {
		var st string
		if raw, ok := stat["start_time"]; ok {
			_ = json.Unmarshal(raw, &st)
		}
		if st != "" {
			record["start_time"] = st
		}
	}
	if endTime != "" {
		record["end_time"] = endTime
	} else {
		var et string
		if raw, ok := stat["end_time"]; ok {
			_ = json.Unmarshal(raw, &et)
		}
		if et != "" {
			record["end_time"] = et
		}
	}

	var finalizedEnd string
	if raw, ok := stat["finalized_data_end_time"]; ok {
		_ = json.Unmarshal(raw, &finalizedEnd)
	}
	if finalizedEnd != "" {
		record["finalized_data_end_time"] = finalizedEnd
	}
}

func normalizeStatsRecord(record map[string]interface{}) {
	if v, ok := record["campaign_id"]; !ok || v == nil {
		record["campaign_id"] = "no_campaign_id"
	}

	for _, field := range []string{"adsquad_id", "ad_id"} {
		if _, ok := record[field]; !ok {
			record[field] = nil
		}
	}

	for _, field := range []string{"start_time", "end_time"} {
		if v, ok := record[field]; !ok || v == nil {
			record[field] = "no_" + field
		}
	}
}
