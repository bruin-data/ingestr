package linkedinads

import (
	"context"
	"fmt"
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
	baseURL               = "https://api.linkedin.com/rest"
	linkedInAPIVersion    = "202601"
	restliProtocolVersion = "2.0.0"
	defaultParallelism    = 5
	maxPageSize           = 1000
	// LinkedIn rejects count > 500 on /insightTagDomains.
	maxPageSizeInsightTagDomains = 500
)

var dailyDateColumns = []schema.Column{
	{Name: "date", DataType: schema.TypeDate, Nullable: true},
}

var monthlyDateColumns = []schema.Column{
	{Name: "start_date", DataType: schema.TypeDate, Nullable: true},
	{Name: "end_date", DataType: schema.TypeDate, Nullable: true},
}

type LinkedInAdsSource struct {
	accessToken string
	accountIDs  []string
	client      *httpclient.Client
	tables      map[string]source.SourceTable
}

type tableReader func(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error

type accountFetchTask struct {
	accountID string
}

type accountFetchResult struct {
	accountID string
	items     []map[string]interface{}
	err       error
}

type accountItemFetcher func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error)

func NewLinkedInAdsSource() *LinkedInAdsSource {
	return &LinkedInAdsSource{}
}

func (s *LinkedInAdsSource) Schemes() []string {
	return []string{"linkedinads"}
}

func (s *LinkedInAdsSource) Connect(ctx context.Context, uri string) error {
	accessToken, accountIDs, err := parseLinkedInAdsURI(uri)
	if err != nil {
		return err
	}

	s.accessToken = accessToken
	s.accountIDs = accountIDs

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(5, 2),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Authorization", fmt.Sprintf("Bearer %s", s.accessToken)),
		httpclient.WithHeader("Linkedin-Version", linkedInAPIVersion),
		httpclient.WithHeader("X-Restli-Protocol-Version", restliProtocolVersion),
	)

	s.tables = s.getTables()
	config.Debug("[LINKEDIN_ADS] Connected successfully")
	return nil
}

func (s *LinkedInAdsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *LinkedInAdsSource) HandlesIncrementality() bool {
	return true
}

func (s *LinkedInAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if strings.HasPrefix(req.Name, "custom:") {
		return s.getCustomAnalyticsTable(req.Name)
	}

	table, ok := s.tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s", req.Name)
	}
	return table, nil
}

func parseLinkedInAdsURI(uri string) (accessToken string, accountIDs []string, err error) {
	if !strings.HasPrefix(uri, "linkedinads://") {
		return "", nil, fmt.Errorf("invalid LinkedIn Ads URI: must start with linkedinads://")
	}

	rest := strings.TrimPrefix(uri, "linkedinads://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse LinkedIn Ads URI query: %w", err)
	}

	accessToken = values.Get("access_token")
	if accessToken == "" {
		return "", nil, fmt.Errorf("access_token is required in URI")
	}

	if ids := values.Get("account_ids"); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				accountIDs = append(accountIDs, id)
			}
		}
	}

	return accessToken, accountIDs, nil
}

func (s *LinkedInAdsSource) getTables() map[string]source.SourceTable {
	schemaFn := func(ctx context.Context) (*schema.TableSchema, error) {
		return nil, fmt.Errorf("LinkedIn Ads source does not have a predefined schema; schema inference is required")
	}

	return map[string]source.SourceTable{
		"ad_accounts": &source.DynamicSourceTable{
			TableName:           "ad_accounts",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readAdAccounts, opts)
			},
		},
		"ad_account_users": &source.DynamicSourceTable{
			TableName:           "ad_account_users",
			TablePrimaryKeys:    []string{"user", "account"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readAdAccountUsers, opts)
			},
		},
		"campaign_groups": &source.DynamicSourceTable{
			TableName:           "campaign_groups",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readCampaignGroups, opts)
			},
		},
		"campaigns": &source.DynamicSourceTable{
			TableName:           "campaigns",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readCampaigns, opts)
			},
		},
		"creatives": &source.DynamicSourceTable{
			TableName:           "creatives",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readCreatives, opts)
			},
		},
		"conversions": &source.DynamicSourceTable{
			TableName:           "conversions",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readConversions, opts)
			},
		},
		"lead_forms": &source.DynamicSourceTable{
			TableName:           "lead_forms",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readLeadForms, opts)
			},
		},
		"lead_form_responses": &source.DynamicSourceTable{
			TableName:           "lead_form_responses",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "submittedAt",
			TableStrategy:       config.StrategyMerge,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readLeadFormResponses, opts)
			},
		},
		"dmp_segments": &source.DynamicSourceTable{
			TableName:           "dmp_segments",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readDmpSegments, opts)
			},
		},
		"insight_tags": &source.DynamicSourceTable{
			TableName:           "insight_tags",
			TablePrimaryKeys:    []string{"id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readInsightTags, opts)
			},
		},
		"insight_tag_domains": &source.DynamicSourceTable{
			TableName:           "insight_tag_domains",
			TablePrimaryKeys:    []string{"domainName", "account_id"},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn:            schemaFn,
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readInsightTagDomains, opts)
			},
		},
	}
}

func normalizePageSize(pageSize int) int {
	if pageSize <= 0 || pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

func normalizeParallelism(parallelism int) int {
	if parallelism <= 0 {
		return defaultParallelism
	}
	return parallelism
}

func (s *LinkedInAdsSource) fetchAdAccounts(ctx context.Context, pageSize int) ([]map[string]interface{}, error) {
	var accounts []map[string]interface{}

	err := s.fetchTokenPagination(ctx, "/adAccounts", map[string]string{
		"q": "search",
	}, pageSize, func(elements []interface{}) error {
		for _, elem := range elements {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if account, ok := elem.(map[string]interface{}); ok {
				accounts = append(accounts, account)
			}
		}
		config.Debug("[LINKEDIN_ADS] Fetched page with %d accounts (total: %d)", len(elements), len(accounts))
		return nil
	})

	return accounts, err
}

func extractAccountIDs(accounts []map[string]interface{}) []string {
	var ids []string
	for _, account := range accounts {
		if id, ok := extractAccountID(account); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// runParallelAccountFetch runs a parallel fetch operation for each account.
func (s *LinkedInAdsSource) runParallelAccountFetch(
	ctx context.Context,
	accountIDs []string,
	parallelism int,
	fetcher accountItemFetcher,
	pageSize int,
	tableName string,
) ([]map[string]interface{}, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}

	taskChan := make(chan accountFetchTask, len(accountIDs))
	resultChan := make(chan accountFetchResult, parallelism*2)

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				items, err := fetcher(workerCtx, task.accountID, pageSize)
				if err != nil {
					config.Debug("[LINKEDIN_ADS] Error fetching %s for account %s: %v", tableName, task.accountID, err)
					select {
					case resultChan <- accountFetchResult{accountID: task.accountID, err: err}:
					case <-workerCtx.Done():
						return
					}
					continue
				}

				if len(items) > 0 {
					select {
					case resultChan <- accountFetchResult{accountID: task.accountID, items: items}:
					case <-workerCtx.Done():
						return
					}
				}
			}
		}()
	}

	// Send tasks
	go func() {
		defer close(taskChan)
		for _, id := range accountIDs {
			select {
			case taskChan <- accountFetchTask{accountID: id}:
			case <-workerCtx.Done():
				return
			}
		}
	}()

	// Close result channel when workers are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var allItems []map[string]interface{}
	for result := range resultChan {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if result.err != nil {
			continue // Error already logged
		}
		allItems = append(allItems, result.items...)
	}

	return allItems, nil
}

// sendAsArrowRecord converts items to Arrow format and sends to results channel.
func sendAsArrowRecord(items []map[string]interface{}, cols []schema.Column, excludeColumns []string, results chan<- source.RecordBatchResult, tableName string) error {
	if len(items) == 0 {
		config.Debug("[LINKEDIN_ADS] No %s found", tableName)
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, excludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[LINKEDIN_ADS] Sent %d %s", len(items), tableName)
	return nil
}

func parseTimeInterval(intervalStart, intervalEnd interface{}) (startTime, endTime time.Time, err error) {
	config.Debug("[LINKEDIN_ADS] parseTimeInterval called with start=%v (%T), end=%v (%T)", intervalStart, intervalStart, intervalEnd, intervalEnd)

	// interval_start is mandatory
	if intervalStart == nil || isNilPointer(intervalStart) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval_start is required for LinkedIn Ads source. Use --interval-start flag (e.g., --interval-start 2024-01-01)")
	}

	// interval_end is mandatory
	if intervalEnd == nil || isNilPointer(intervalEnd) {
		return time.Time{}, time.Time{}, fmt.Errorf("interval_end is required for LinkedIn Ads source. Use --interval-end flag (e.g., --interval-end 2024-12-31)")
	}

	startTime = parseTimestamp(intervalStart, time.Time{})
	endTime = parseTimestamp(intervalEnd, time.Time{})

	if startTime.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid interval_start value")
	}
	if endTime.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid interval_end value")
	}

	config.Debug("[LINKEDIN_ADS] Parsed time: start=%s, end=%s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
	return startTime, endTime, nil
}

// isNilPointer checks if an interface contains a nil pointer
func isNilPointer(v interface{}) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case *time.Time:
		return t == nil
	}
	return false
}

func parseTimestamp(value interface{}, defaultVal time.Time) time.Time {
	if value == nil {
		return defaultVal
	}

	switch v := value.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
	}
	return defaultVal
}

// Table Reader Wrapper
func (s *LinkedInAdsSource) readTable(ctx context.Context, reader tableReader, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if err := reader(ctx, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// Table Readers
func (s *LinkedInAdsSource) readAdAccounts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading ad_accounts")
	config.Debug("[LINKEDIN_ADS] opts.PageSize: %d", opts.PageSize)
	pageSize := normalizePageSize(opts.PageSize)
	config.Debug("[LINKEDIN_ADS] normalized pageSize: %d", pageSize)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}

	return sendAsArrowRecord(accounts, nil, opts.ExcludeColumns, results, "ad_accounts")
}

func (s *LinkedInAdsSource) readAdAccountUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading ad_account_users")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching users with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		encodedURN := strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", accountID), ":", "%3A")

		var users []map[string]interface{}
		err := s.fetchCursorPagination(ctx, "/adAccountUsers", map[string]string{
			"q":        "accounts",
			"accounts": fmt.Sprintf("List(%s)", encodedURN),
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if user, ok := elem.(map[string]interface{}); ok {
					user["account_id"] = accountIDToInt64(accountID)
					users = append(users, user)
				}
			}
			return nil
		})
		return users, err
	}, pageSize, "ad_account_users")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "ad_account_users")
}

func (s *LinkedInAdsSource) readCampaignGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading campaign_groups")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching campaign groups with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		endpoint := fmt.Sprintf("/adAccounts/%s/adCampaignGroups", accountID)

		var items []map[string]interface{}
		err := s.fetchTokenPagination(ctx, endpoint, map[string]string{
			"q": "search",
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "campaign_groups")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "campaign_groups")
}

func (s *LinkedInAdsSource) readCampaigns(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading campaigns")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching campaigns with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		endpoint := fmt.Sprintf("/adAccounts/%s/adCampaigns", accountID)

		var items []map[string]interface{}
		err := s.fetchTokenPagination(ctx, endpoint, map[string]string{
			"q": "search",
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "campaigns")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "campaigns")
}

func (s *LinkedInAdsSource) readCreatives(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading creatives")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching creatives with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		endpoint := fmt.Sprintf("/adAccounts/%s/creatives", accountID)

		var items []map[string]interface{}
		err := s.fetchTokenPagination(ctx, endpoint, map[string]string{
			"q": "criteria",
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "creatives")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "creatives")
}

func (s *LinkedInAdsSource) readConversions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading conversions")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching conversions with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		encodedURN := strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", accountID), ":", "%3A")

		var items []map[string]interface{}
		err := s.fetchCursorPagination(ctx, "/conversions", map[string]string{
			"q":       "account",
			"account": encodedURN,
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "conversions")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "conversions")
}

func (s *LinkedInAdsSource) readLeadForms(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading lead_forms")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching lead forms with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		encodedURN := strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", accountID), ":", "%3A")

		var items []map[string]interface{}
		err := s.fetchCursorPagination(ctx, "/leadForms", map[string]string{
			"q":     "owner",
			"owner": fmt.Sprintf("(sponsoredAccount:%s)", encodedURN),
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "lead_forms")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "lead_forms")
}

func (s *LinkedInAdsSource) readLeadFormResponses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading lead_form_responses")
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	startTime, endTime, err := parseTimeInterval(opts.IntervalStart, opts.IntervalEnd)
	if err != nil {
		return err
	}
	startMillis := startTime.UnixMilli()
	endMillis := endTime.UnixMilli()
	config.Debug("[LINKEDIN_ADS] Time range: %d to %d", startMillis, endMillis)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching lead form responses with parallelism %d", len(accountIDs), parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		encodedURN := strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", accountID), ":", "%3A")

		var items []map[string]interface{}
		err := s.fetchCursorPagination(ctx, "/leadFormResponses", map[string]string{
			"leadType":             "(leadType:SPONSORED)",
			"q":                    "owner",
			"owner":                fmt.Sprintf("(sponsoredAccount:%s)", encodedURN),
			"submittedAtTimeRange": fmt.Sprintf("(start:%d,end:%d)", startMillis, endMillis),
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, "lead_form_responses")
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, "lead_form_responses")
}

func (s *LinkedInAdsSource) readDmpSegments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPerAccountCursor(ctx, opts, results, "/dmpSegments", "dmp_segments")
}

func (s *LinkedInAdsSource) readInsightTags(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPerAccountCursor(ctx, opts, results, "/insightTags", "insight_tags")
}

func (s *LinkedInAdsSource) readInsightTagDomains(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if opts.PageSize <= 0 || opts.PageSize > maxPageSizeInsightTagDomains {
		opts.PageSize = maxPageSizeInsightTagDomains
	}
	return s.readPerAccountCursor(ctx, opts, results, "/insightTagDomains", "insight_tag_domains")
}

func (s *LinkedInAdsSource) readPerAccountCursor(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, endpoint, tableName string) error {
	config.Debug("[LINKEDIN_ADS] Reading %s", tableName)
	pageSize := normalizePageSize(opts.PageSize)
	parallelism := normalizeParallelism(opts.Parallelism)

	accounts, err := s.fetchAdAccounts(ctx, pageSize)
	if err != nil {
		return fmt.Errorf("failed to fetch ad accounts: %w", err)
	}
	accountIDs := extractAccountIDs(accounts)

	config.Debug("[LINKEDIN_ADS] Found %d ad accounts, fetching %s with parallelism %d", len(accountIDs), tableName, parallelism)

	allItems, err := s.runParallelAccountFetch(ctx, accountIDs, parallelism, func(ctx context.Context, accountID string, pageSize int) ([]map[string]interface{}, error) {
		encodedURN := strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", accountID), ":", "%3A")

		var items []map[string]interface{}
		err := s.fetchCursorPagination(ctx, endpoint, map[string]string{
			"q":       "account",
			"account": encodedURN,
		}, pageSize, func(elements []interface{}) error {
			for _, elem := range elements {
				if item, ok := elem.(map[string]interface{}); ok {
					item["account_id"] = accountIDToInt64(accountID)
					items = append(items, item)
				}
			}
			return nil
		})
		return items, err
	}, pageSize, tableName)
	if err != nil {
		return err
	}

	return sendAsArrowRecord(allItems, nil, opts.ExcludeColumns, results, tableName)
}

// ----------------------------------------------------------------------------
// Custom Analytics

func (s *LinkedInAdsSource) getCustomAnalyticsTable(tableName string) (source.SourceTable, error) {
	cfg, err := parseCustomTableName(tableName)
	if err != nil {
		return nil, err
	}

	if len(s.accountIDs) == 0 {
		return nil, fmt.Errorf("account_ids is required in URI for custom analytics tables")
	}

	schemaFn := func(ctx context.Context) (*schema.TableSchema, error) {
		return nil, fmt.Errorf("custom analytics does not have a predefined schema; schema inference is required")
	}

	return &source.DynamicSourceTable{
		TableName:           "custom_reports",
		TablePrimaryKeys:    cfg.primaryKeys,
		TableIncrementalKey: cfg.incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn:            schemaFn,
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readTable(ctx, func(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
				return s.readCustomAnalytics(ctx, cfg, opts, results)
			}, opts)
		},
	}, nil
}

func (s *LinkedInAdsSource) readCustomAnalytics(ctx context.Context, cfg *customAnalyticsConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[LINKEDIN_ADS] Reading custom analytics with pivot=%s, granularity=%s", cfg.pivot, cfg.timeGranularity)

	startDate, endDate, err := parseTimeInterval(opts.IntervalStart, opts.IntervalEnd)
	if err != nil {
		return err
	}

	config.Debug("[LINKEDIN_ADS] Date range: %s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// Generate intervals based on granularity
	intervals, err := findIntervals(startDate, endDate, cfg.timeGranularity)
	if err != nil {
		return err
	}
	config.Debug("[LINKEDIN_ADS] Generated %d intervals", len(intervals))

	var allItems []map[string]interface{}

	for _, interval := range intervals {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		url := constructAnalyticsURL(interval.start, interval.end, s.accountIDs, cfg.metrics, cfg.pivot, cfg.timeGranularity)
		config.Debug("[LINKEDIN_ADS] Fetching analytics: %s", url)

		data, err := s.fetch(ctx, url, nil)
		if err != nil {
			return fmt.Errorf("failed to fetch analytics: %w", err)
		}
		if data == nil {
			continue
		}

		elements, ok := data["elements"].([]interface{})
		if !ok || len(elements) == 0 {
			continue
		}

		items := flattenAnalyticsItems(elements, cfg.pivot, cfg.timeGranularity)
		allItems = append(allItems, items...)
	}

	dateHints := monthlyDateColumns
	if cfg.timeGranularity == timeGranularityDaily {
		dateHints = dailyDateColumns
	}

	return sendAsArrowRecord(allItems, dateHints, opts.ExcludeColumns, results, "custom_reports")
}

var _ source.Source = (*LinkedInAdsSource)(nil)
