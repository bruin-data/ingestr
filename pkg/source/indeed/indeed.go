package indeed

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL   = "https://apis.indeed.com/ads/v1"
	tokenURL  = "https://apis.indeed.com/oauth/v2/tokens"
	scopes    = "employer.advertising.campaign.read employer.advertising.campaign_report.read employer.advertising.account.read"
	maxPages  = 1000
	perPage   = 500
	workerCnt = 5

	// Indeed uses monthly quota-based rate limits tied to campaign spend.
	// No documented per-second/per-minute limit; using a conservative default.
	rateLimit      = 5.0
	rateLimitBurst = 5

	maxReportPolls  = 10
	reportPollDelay = 5 * time.Second
	defaultLookback = 365 // days
)

var supportedTables = []string{
	"campaigns",
	"campaign_details",
	"campaign_budget",
	"campaign_jobs",
	"campaign_properties",
	"campaign_stats",
	"account",
	"traffic_stats",
}

type IndeedSource struct {
	clientID     string
	clientSecret string
	employerID   string
	client       *httpclient.Client
}

func NewIndeedSource() *IndeedSource {
	return &IndeedSource{}
}

func (s *IndeedSource) HandlesIncrementality() bool {
	return true
}

func (s *IndeedSource) Schemes() []string {
	return []string{"indeed"}
}

func parseURI(uri string) (clientID, clientSecret, employerID string, err error) {
	if !strings.HasPrefix(uri, "indeed://") {
		return "", "", "", fmt.Errorf("invalid indeed URI: must start with indeed://")
	}

	rest := strings.TrimPrefix(uri, "indeed://")
	parts := strings.SplitN(rest, "?", 2)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("indeed URI must include query parameters (indeed://?client_id=...&client_secret=...&employer_id=...)")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse indeed URI query: %w", err)
	}

	clientID = values.Get("client_id")
	clientSecret = values.Get("client_secret")
	employerID = values.Get("employer_id")

	if clientID == "" {
		return "", "", "", fmt.Errorf("client_id is required in indeed URI")
	}
	if clientSecret == "" {
		return "", "", "", fmt.Errorf("client_secret is required in indeed URI")
	}
	if employerID == "" {
		return "", "", "", fmt.Errorf("employer_id is required in indeed URI")
	}

	return clientID, clientSecret, employerID, nil
}

func (s *IndeedSource) getAccessToken(ctx context.Context) (string, error) {
	client := httpclient.New(
		httpclient.WithTimeout(30 * time.Second),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.R(ctx).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormData(map[string]string{
			"client_id":     s.clientID,
			"client_secret": s.clientSecret,
			"grant_type":    "client_credentials",
			"scope":         scopes,
			"employer":      s.employerID,
		}).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(resp.Body(), &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

func (s *IndeedSource) Connect(ctx context.Context, uri string) error {
	clientID, clientSecret, employerID, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.clientID = clientID
	s.clientSecret = clientSecret
	s.employerID = employerID

	token, err := s.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Indeed access token: %w", err)
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(token)),
	)

	config.Debug("[INDEED] Connected successfully")
	return nil
}

func (s *IndeedSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *IndeedSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	var primaryKeys []string
	incrementalKey := ""
	strategy := config.StrategyMerge

	switch tableName {
	case "campaigns":
		primaryKeys = []string{"Id"}
	case "campaign_details":
		primaryKeys = []string{"campaignId"}
	case "campaign_budget":
		primaryKeys = []string{"campaignId"}
	case "campaign_jobs":
		primaryKeys = []string{"campaignId", "jobKey"}
	case "campaign_properties":
		primaryKeys = []string{"campaignId"}
	case "campaign_stats":
		primaryKeys = []string{"campaignId", "Date"}
		incrementalKey = "Date"
	case "account":
		primaryKeys = []string{"employerId", "jobSourceId"}
	case "traffic_stats":
		primaryKeys = []string{"date"}
		incrementalKey = "date"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("indeed source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
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

func (s *IndeedSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "campaigns":
			err = s.readCampaigns(ctx, opts, results)
		case "campaign_details":
			err = s.readCampaignDetails(ctx, opts, results)
		case "campaign_budget":
			err = s.readCampaignBudget(ctx, opts, results)
		case "campaign_jobs":
			err = s.readCampaignJobs(ctx, opts, results)
		case "campaign_properties":
			err = s.readCampaignProperties(ctx, opts, results)
		case "campaign_stats":
			err = s.readCampaignStats(ctx, opts, results)
		case "account":
			err = s.readAccount(ctx, opts, results)
		case "traffic_stats":
			err = s.readTrafficStats(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func sendErr(ctx context.Context, results chan<- source.RecordBatchResult, err error) {
	select {
	case results <- source.RecordBatchResult{Err: err}:
	case <-ctx.Done():
	}
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// fetchAllCampaigns fetches all active campaigns using cursor-based pagination.
func (s *IndeedSource) fetchAllCampaigns(ctx context.Context) ([]map[string]any, error) {
	var campaigns []map[string]any
	start := ""

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("perPage", fmt.Sprintf("%d", perPage)).
			SetQueryParam("status", "ACTIVE")

		if start != "" {
			req.SetQueryParam("start", start)
		}

		resp, err := req.Get("/campaigns")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch campaigns: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("campaigns returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var body struct {
			Data struct {
				Campaigns []map[string]any `json:"Campaigns"`
			} `json:"data"`
			Meta struct {
				Links []struct {
					Rel  string `json:"rel"`
					Href string `json:"href"`
				} `json:"links"`
			} `json:"meta"`
		}
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return nil, fmt.Errorf("failed to parse campaigns response: %w", err)
		}

		if len(body.Data.Campaigns) == 0 {
			break
		}

		campaigns = append(campaigns, body.Data.Campaigns...)

		nextStart := ""
		for _, link := range body.Meta.Links {
			if link.Rel == "next" {
				parsed, err := url.Parse(link.Href)
				if err == nil {
					nextStart = parsed.Query().Get("start")
				}
				break
			}
		}

		if nextStart == "" {
			break
		}
		start = nextStart
	}

	config.Debug("[INDEED] fetched %d campaigns", len(campaigns))
	return campaigns, nil
}

func campaignIDs(campaigns []map[string]any) []string {
	ids := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		if id, ok := c["Id"]; ok {
			ids = append(ids, fmt.Sprintf("%v", id))
		}
	}
	return ids
}

// readCampaigns fetches all active campaigns.
func (s *IndeedSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaigns")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaigns: %w", err)
	}

	if len(campaigns) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(campaigns, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for campaigns: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}

	config.Debug("[INDEED] finished reading campaigns: %d total records", len(campaigns))
	return nil
}

// readCampaignDetails fetches detailed info for each campaign in parallel.
func (s *IndeedSource) readCampaignDetails(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaign_details")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaign IDs for campaign_details: %w", err)
	}

	return s.readCampaignSubResource(ctx, campaignIDs(campaigns), "", "campaign_details", opts, results)
}

// readCampaignBudget fetches budget info for each campaign in parallel.
func (s *IndeedSource) readCampaignBudget(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaign_budget")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaign IDs for campaign_budget: %w", err)
	}

	return s.readCampaignSubResource(ctx, campaignIDs(campaigns), "/budget", "campaign_budget", opts, results)
}

// readCampaignJobs fetches jobs for each campaign in parallel.
func (s *IndeedSource) readCampaignJobs(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaign_jobs")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaign IDs for campaign_jobs: %w", err)
	}

	return s.readCampaignSubResourceEntries(ctx, campaignIDs(campaigns), "/jobDetails", "entries", "campaign_jobs", opts, results)
}

// readCampaignProperties fetches properties for each campaign in parallel.
func (s *IndeedSource) readCampaignProperties(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaign_properties")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaign IDs for campaign_properties: %w", err)
	}

	return s.readCampaignSubResource(ctx, campaignIDs(campaigns), "/properties", "campaign_properties", opts, results)
}

// readCampaignSubResource fetches a single-object sub-resource per campaign in parallel.
// The response is expected at data.<field> or just data if field is empty.
func (s *IndeedSource) readCampaignSubResource(ctx context.Context, campaignIDs []string, pathSuffix, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	sem := make(chan struct{}, workerCnt)
	var wg sync.WaitGroup
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	for _, id := range campaignIDs {
		if workerCtx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(campaignID string) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-workerCtx.Done():
				return
			default:
			}

			endpoint := fmt.Sprintf("/campaigns/%s%s", campaignID, pathSuffix)
			resp, err := s.client.R(workerCtx).Get(endpoint)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to fetch %s for campaign %s: %w", label, campaignID, err))
				return
			}

			if resp.StatusCode() == 404 {
				config.Debug("[INDEED] %s: campaign %s returned 404, skipping", label, campaignID)
				return
			}
			if !resp.IsSuccess() {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("%s for campaign %s returned status %d: %s", label, campaignID, resp.StatusCode(), resp.String()))
				return
			}

			var body struct {
				Data map[string]interface{} `json:"data"`
			}
			if err := jsonUseNumber(resp.Body(), &body); err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to parse %s response for campaign %s: %w", label, campaignID, err))
				return
			}

			if body.Data == nil {
				return
			}

			body.Data["campaignId"] = campaignID

			record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{body.Data}, nil, opts.ExcludeColumns)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to build arrow record for %s campaign %s: %w", label, campaignID, err))
				return
			}

			select {
			case <-workerCtx.Done():
				return
			case results <- source.RecordBatchResult{Batch: record}:
			}

			config.Debug("[INDEED] %s: fetched campaign %s", label, campaignID)
		}(id)
	}

	wg.Wait()
	config.Debug("[INDEED] finished reading %s for %d campaigns", label, len(campaignIDs))
	return nil
}

// readCampaignSubResourceEntries fetches an array sub-resource per campaign in parallel.
// The response is expected at data.<arrayField>[] with campaignId injected.
func (s *IndeedSource) readCampaignSubResourceEntries(ctx context.Context, campaignIDs []string, pathSuffix, arrayField, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	sem := make(chan struct{}, workerCnt)
	var wg sync.WaitGroup
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	for _, id := range campaignIDs {
		if workerCtx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(campaignID string) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-workerCtx.Done():
				return
			default:
			}

			endpoint := fmt.Sprintf("/campaigns/%s%s", campaignID, pathSuffix)
			resp, err := s.client.R(workerCtx).Get(endpoint)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to fetch %s for campaign %s: %w", label, campaignID, err))
				return
			}

			if resp.StatusCode() == 404 {
				config.Debug("[INDEED] %s: campaign %s returned 404, skipping", label, campaignID)
				return
			}
			if !resp.IsSuccess() {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("%s for campaign %s returned status %d: %s", label, campaignID, resp.StatusCode(), resp.String()))
				return
			}

			var raw map[string]interface{}
			if err := jsonUseNumber(resp.Body(), &raw); err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to parse %s response for campaign %s: %w", label, campaignID, err))
				return
			}

			dataObj, _ := raw["data"].(map[string]interface{})
			if dataObj == nil {
				return
			}

			entriesRaw, _ := dataObj[arrayField].([]interface{})
			if len(entriesRaw) == 0 {
				return
			}

			items := make([]map[string]interface{}, 0, len(entriesRaw))
			for _, e := range entriesRaw {
				if m, ok := e.(map[string]interface{}); ok {
					m["campaignId"] = campaignID
					items = append(items, m)
				}
			}

			if len(items) == 0 {
				return
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to build arrow record for %s campaign %s: %w", label, campaignID, err))
				return
			}

			select {
			case <-workerCtx.Done():
				return
			case results <- source.RecordBatchResult{Batch: record}:
			}

			config.Debug("[INDEED] %s: fetched %d entries for campaign %s", label, len(items), campaignID)
		}(id)
	}

	wg.Wait()
	config.Debug("[INDEED] finished reading %s for %d campaigns", label, len(campaignIDs))
	return nil
}

// readCampaignStats fetches daily stats for each campaign in parallel.
// Supports server-side date filtering via startDate/endDate query params.
func (s *IndeedSource) readCampaignStats(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading campaign_stats")

	campaigns, err := s.fetchAllCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch campaign IDs for campaign_stats: %w", err)
	}

	ids := campaignIDs(campaigns)
	startDate, endDate := s.resolveInterval(opts)

	sem := make(chan struct{}, workerCnt)
	var wg sync.WaitGroup
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	for _, id := range ids {
		if workerCtx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(campaignID string) {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-workerCtx.Done():
				return
			default:
			}

			endpoint := fmt.Sprintf("/campaigns/%s/stats", campaignID)
			resp, err := s.client.R(workerCtx).
				SetQueryParam("startDate", startDate).
				SetQueryParam("endDate", endDate).
				Get(endpoint)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to fetch campaign_stats for campaign %s: %w", campaignID, err))
				return
			}

			if resp.StatusCode() == 404 {
				config.Debug("[INDEED] campaign_stats: campaign %s returned 404, skipping", campaignID)
				return
			}
			if !resp.IsSuccess() {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("campaign_stats for campaign %s returned status %d: %s", campaignID, resp.StatusCode(), resp.String()))
				return
			}

			var raw map[string]interface{}
			if err := jsonUseNumber(resp.Body(), &raw); err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to parse campaign_stats response for campaign %s: %w", campaignID, err))
				return
			}

			dataObj, _ := raw["data"].(map[string]interface{})
			if dataObj == nil {
				return
			}

			// The stats may be in data.Stats or data.entries
			var statsRaw []interface{}
			if s, ok := dataObj["Stats"].([]interface{}); ok {
				statsRaw = s
			} else if s, ok := dataObj["entries"].([]interface{}); ok {
				statsRaw = s
			}

			if len(statsRaw) == 0 {
				return
			}

			items := make([]map[string]interface{}, 0, len(statsRaw))
			for _, e := range statsRaw {
				if m, ok := e.(map[string]interface{}); ok {
					m["campaignId"] = campaignID
					items = append(items, m)
				}
			}

			if len(items) == 0 {
				return
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				cancelWorkers()
				sendErr(workerCtx, results, fmt.Errorf("failed to build arrow record for campaign_stats campaign %s: %w", campaignID, err))
				return
			}

			select {
			case <-workerCtx.Done():
				return
			case results <- source.RecordBatchResult{Batch: record}:
			}

			config.Debug("[INDEED] campaign_stats: fetched %d stats for campaign %s", len(items), campaignID)
		}(id)
	}

	wg.Wait()
	config.Debug("[INDEED] finished reading campaign_stats for %d campaigns", len(ids))
	return nil
}

// readAccount fetches the account info and flattens the jobSourceList.
func (s *IndeedSource) readAccount(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading account")

	resp, err := s.client.R(ctx).Get("/account")
	if err != nil {
		return fmt.Errorf("failed to fetch account: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("account returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var raw map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &raw); err != nil {
		return fmt.Errorf("failed to parse account response: %w", err)
	}

	dataObj, _ := raw["data"].(map[string]interface{})
	if dataObj == nil {
		return nil
	}

	employerID := dataObj["employerId"]
	contact := dataObj["contact"]
	company := dataObj["company"]
	email := dataObj["email"]

	jobSources, _ := dataObj["jobSourceList"].([]interface{})
	if len(jobSources) == 0 {
		return nil
	}

	items := make([]map[string]interface{}, 0, len(jobSources))
	for _, js := range jobSources {
		jsMap, ok := js.(map[string]interface{})
		if !ok {
			continue
		}

		row := map[string]interface{}{
			"employerId": employerID,
			"contact":    contact,
			"company":    company,
			"email":      email,
		}

		if id, ok := jsMap["id"]; ok {
			row["jobSourceId"] = id
		}
		if name, ok := jsMap["siteName"]; ok {
			row["jobSourceSiteName"] = name
		}

		items = append(items, row)
	}

	if len(items) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record for account: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case results <- source.RecordBatchResult{Batch: record}:
	}

	config.Debug("[INDEED] account: sent %d records", len(items))
	return nil
}

// readTrafficStats fetches daily traffic stats via async CSV report endpoint.
// Iterates day-by-day from startDate to endDate, polling for each report.
func (s *IndeedSource) readTrafficStats(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[INDEED] reading traffic_stats")

	startDate, endDateStr := s.resolveInterval(opts)
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return fmt.Errorf("failed to parse start date: %w", err)
	}
	end, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return fmt.Errorf("failed to parse end date: %w", err)
	}

	totalProcessed := 0

	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		dateStr := current.Format("2006-01-02")
		nextDay := current.AddDate(0, 0, 1).Format("2006-01-02")

		items, err := s.fetchTrafficStatsForDay(ctx, dateStr, nextDay)
		if err != nil {
			return fmt.Errorf("failed to fetch traffic_stats for %s: %w", dateStr, err)
		}

		if len(items) == 0 {
			continue
		}

		// Inject date field
		for _, item := range items {
			item["date"] = dateStr
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record for traffic_stats %s: %w", dateStr, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}

		totalProcessed += len(items)
		config.Debug("[INDEED] traffic_stats: sent %d records for %s (total: %d)", len(items), dateStr, totalProcessed)
	}

	config.Debug("[INDEED] finished reading traffic_stats: %d total records", totalProcessed)
	return nil
}

func (s *IndeedSource) fetchTrafficStatsForDay(ctx context.Context, startDate, endDate string) ([]map[string]interface{}, error) {
	resp, err := s.client.R(ctx).
		SetQueryParam("startDate", startDate).
		SetQueryParam("endDate", endDate).
		SetQueryParam("v", "8").
		Get("/stats")
	if err != nil {
		return nil, fmt.Errorf("stats request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("stats returned status %d: %s", resp.StatusCode(), resp.String())
	}

	// Check if this is an async report (HTTP 202)
	if resp.StatusCode() == 202 {
		var asyncBody struct {
			Data struct {
				Location string `json:"location"`
			} `json:"data"`
		}
		if err := json.Unmarshal(resp.Body(), &asyncBody); err != nil {
			return nil, fmt.Errorf("failed to parse async stats response: %w", err)
		}

		location := asyncBody.Data.Location
		// Strip /v1 prefix to avoid double-prefix with base URL
		location = strings.TrimPrefix(location, "/v1")

		return s.pollTrafficReport(ctx, location)
	}

	// Direct CSV response
	return parseCSV(resp.Body())
}

func (s *IndeedSource) pollTrafficReport(ctx context.Context, location string) ([]map[string]interface{}, error) {
	for i := 0; i < maxReportPolls; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		time.Sleep(reportPollDelay)

		resp, err := s.client.R(ctx).Get(location)
		if err != nil {
			return nil, fmt.Errorf("poll request failed: %w", err)
		}

		contentType := resp.Header().Get("Content-Type")
		if resp.IsSuccess() && (strings.Contains(contentType, "text/csv") || strings.Contains(contentType, "application/csv")) {
			return parseCSV(resp.Body())
		}

		if resp.StatusCode() == 202 {
			config.Debug("[INDEED] traffic report still processing, poll %d/%d", i+1, maxReportPolls)
			continue
		}

		if !resp.IsSuccess() {
			return nil, fmt.Errorf("poll returned status %d: %s", resp.StatusCode(), resp.String())
		}

		return nil, fmt.Errorf("unexpected poll response status %d content-type %q: %s", resp.StatusCode(), contentType, resp.String())
	}

	return nil, fmt.Errorf("traffic report did not complete after %d polls", maxReportPolls)
}

func parseCSV(data []byte) ([]map[string]interface{}, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	headers, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	var items []map[string]interface{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		item := make(map[string]interface{}, len(headers))
		for i, header := range headers {
			if i < len(row) {
				item[header] = row[i]
			}
		}
		items = append(items, item)
	}

	return items, nil
}

func (s *IndeedSource) resolveInterval(opts source.ReadOptions) (string, string) {
	now := time.Now().UTC()

	startDate := now.AddDate(0, 0, -defaultLookback).Format("2006-01-02")
	endDate := now.Format("2006-01-02")

	if opts.IntervalStart != nil {
		startDate = opts.IntervalStart.Format("2006-01-02")
	}
	if opts.IntervalEnd != nil {
		endDate = opts.IntervalEnd.Format("2006-01-02")
	}

	return startDate, endDate
}
