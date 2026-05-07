package revenuecat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.revenuecat.com/v2"

	// RevenueCat rate limits per domain per minute:
	//   Customer Information: 480 req/min → (480 * 0.8) / 60 = 6.4
	//   Project Configuration: 60 req/min → (60 * 0.8) / 60 = 0.8
	rateLimitCustomer      = 6.4
	rateLimitCustomerBurst = 5
	rateLimitProject       = 0.8
	rateLimitProjectBurst  = 5

	maxPageSize     = 1000
	parallelWorkers = 4
)

var supportedTables = []string{
	"projects",
	"customers",
	"products",
	"entitlements",
	"offerings",
}

type credentials struct {
	apiKey    string
	projectID string
}

type RevenueCatSource struct {
	apiKey         string
	projectID      string
	projectClient  *ingestrhttp.Client // projects, products, entitlements, offerings (60 req/min)
	customerClient *ingestrhttp.Client // customers, subscriptions, purchases (480 req/min)
}

func NewRevenueCatSource() *RevenueCatSource {
	return &RevenueCatSource{}
}

func (s *RevenueCatSource) HandlesIncrementality() bool {
	return false
}

func (s *RevenueCatSource) Schemes() []string {
	return []string{"revenuecat"}
}

func (s *RevenueCatSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseRevenueCatURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = creds.apiKey
	s.projectID = creds.projectID

	commonOpts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("Authorization", "Bearer "+s.apiKey),
	}

	projectOpts := append(commonOpts[:len(commonOpts):len(commonOpts)], ingestrhttp.WithRateLimiter(rateLimitProject, rateLimitProjectBurst))
	s.projectClient = ingestrhttp.New(projectOpts...)

	customerOpts := append(commonOpts[:len(commonOpts):len(commonOpts)], ingestrhttp.WithRateLimiter(rateLimitCustomer, rateLimitCustomerBurst))
	s.customerClient = ingestrhttp.New(customerOpts...)

	config.Debug("[REVENUECAT] Connected successfully")
	return nil
}

func (s *RevenueCatSource) Close(ctx context.Context) error {
	var firstErr error
	if s.projectClient != nil {
		if err := s.projectClient.Close(); err != nil {
			firstErr = err
		}
	}
	if s.customerClient != nil {
		if err := s.customerClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func parseRevenueCatURI(uri string) (credentials, error) {
	if !strings.HasPrefix(uri, "revenuecat://") {
		return credentials{}, fmt.Errorf("invalid revenuecat URI: must start with revenuecat://")
	}

	rest := strings.TrimPrefix(uri, "revenuecat://")
	if rest == "" || rest == "?" {
		return credentials{}, fmt.Errorf("api_key is required in revenuecat URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return credentials{}, fmt.Errorf("failed to parse revenuecat URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return credentials{}, fmt.Errorf("api_key is required in revenuecat URI")
	}

	projectID := values.Get("project_id")

	return credentials{
		apiKey:    apiKey,
		projectID: projectID,
	}, nil
}

func (s *RevenueCatSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	if tableName != "projects" && s.projectID == "" {
		return nil, fmt.Errorf("project_id is required for %s resource", tableName)
	}

	strategy := config.StrategyMerge
	if tableName == "customers" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "",
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("revenuecat source does not have a predefined schema; schema inference is required")
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

func (s *RevenueCatSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "projects":
			err = s.readProjects(ctx, opts, results)
		case "customers":
			err = s.readCustomers(ctx, opts, results)
		case "products":
			err = s.readProducts(ctx, opts, results)
		case "entitlements":
			err = s.readEntitlements(ctx, opts, results)
		case "offerings":
			err = s.readOfferings(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

type paginatedResponse struct {
	Items    []json.RawMessage `json:"items"`
	NextPage string            `json:"next_page"`
}

func (s *RevenueCatSource) fetchPages(ctx context.Context, client *ingestrhttp.Client, endpoint string, onPage func(items []map[string]interface{}) error) error {
	nextCursor := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := client.R(ctx).
			SetQueryParam("limit", fmt.Sprintf("%d", maxPageSize))

		if nextCursor != "" {
			req.SetQueryParam("starting_after", nextCursor)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var page paginatedResponse
		decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
		decoder.UseNumber()
		if err := decoder.Decode(&page); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := make([]map[string]interface{}, 0, len(page.Items))
		for _, raw := range page.Items {
			var item map[string]interface{}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&item); err != nil {
				return fmt.Errorf("failed to parse item from %s: %w", endpoint, err)
			}
			items = append(items, item)
		}

		if len(items) > 0 {
			if err := onPage(items); err != nil {
				return err
			}
		}

		if page.NextPage == "" {
			break
		}

		cursor := extractStartingAfter(page.NextPage)
		if cursor == "" {
			break
		}
		nextCursor = cursor
	}

	return nil
}

func (s *RevenueCatSource) paginate(ctx context.Context, client *ingestrhttp.Client, endpoint string) ([]map[string]interface{}, error) {
	var allItems []map[string]interface{}
	err := s.fetchPages(ctx, client, endpoint, func(items []map[string]interface{}) error {
		allItems = append(allItems, items...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allItems, nil
}

func (s *RevenueCatSource) paginateAndSend(ctx context.Context, client *ingestrhttp.Client, endpoint string, timestampFields []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	totalSent := 0
	err := s.fetchPages(ctx, client, endpoint, func(items []map[string]interface{}) error {
		for _, item := range items {
			convertTimestampsToISO(item, timestampFields)
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[REVENUECAT] %s: sent %d items (total: %d)", endpoint, len(items), totalSent)
		return nil
	})
	if err != nil {
		return err
	}
	config.Debug("[REVENUECAT] %s: finished with %d total items", endpoint, totalSent)
	return nil
}

func extractStartingAfter(nextPageURL string) string {
	idx := strings.Index(nextPageURL, "starting_after=")
	if idx == -1 {
		return ""
	}
	rest := nextPageURL[idx+len("starting_after="):]
	if ampIdx := strings.Index(rest, "&"); ampIdx != -1 {
		return rest[:ampIdx]
	}
	return rest
}

func convertTimestampsToISO(item map[string]interface{}, fields []string) {
	for _, field := range fields {
		val, ok := item[field]
		if !ok || val == nil {
			continue
		}

		var ms int64
		switch v := val.(type) {
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				continue
			}
			ms = n
		case float64:
			ms = int64(v)
		default:
			continue
		}

		t := time.UnixMilli(ms).UTC()
		item[field] = t.Format("2006-01-02T15:04:05.000Z07:00")
	}
}

func (s *RevenueCatSource) readProjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REVENUECAT] reading projects")
	return s.paginateAndSend(ctx, s.projectClient, "/projects", []string{"created_at"}, opts, results)
}

func (s *RevenueCatSource) readCustomers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REVENUECAT] reading customers")

	endpoint := fmt.Sprintf("/projects/%s/customers", s.projectID)
	customers, err := s.paginate(ctx, s.customerClient, endpoint)
	if err != nil {
		return err
	}

	if len(customers) == 0 {
		config.Debug("[REVENUECAT] No customers found")
		return nil
	}

	config.Debug("[REVENUECAT] Found %d customers, fetching nested resources", len(customers))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type customerResult struct {
		customer map[string]interface{}
		err      error
	}

	customerChan := make(chan map[string]interface{}, len(customers))
	for _, c := range customers {
		customerChan <- c
	}
	close(customerChan)

	resultChan := make(chan customerResult, parallelWorkers)

	var wg sync.WaitGroup
	for i := 0; i < parallelWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for customer := range customerChan {
				if ctx.Err() != nil {
					return
				}

				enriched, err := s.enrichCustomerWithNested(ctx, customer)
				select {
				case resultChan <- customerResult{customer: enriched, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var batch []map[string]interface{}
	totalSent := 0

	for res := range resultChan {
		if res.err != nil {
			cancel()
			return res.err
		}
		if res.customer == nil {
			continue
		}

		batch = append(batch, res.customer)

		if len(batch) >= maxPageSize {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				cancel()
				return fmt.Errorf("failed to convert customers to Arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(batch)
			config.Debug("[REVENUECAT] Sent %d customer records (total: %d)", len(batch), totalSent)
			batch = nil
		}
	}

	if len(batch) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert customers to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(batch)
		config.Debug("[REVENUECAT] Sent %d customer records (total: %d)", len(batch), totalSent)
	}

	config.Debug("[REVENUECAT] Finished reading customers: %d total", totalSent)
	return nil
}

func (s *RevenueCatSource) enrichCustomerWithNested(ctx context.Context, customer map[string]interface{}) (map[string]interface{}, error) {
	customerID, _ := customer["id"].(string)
	if customerID == "" {
		return nil, fmt.Errorf("customer missing id field")
	}

	convertTimestampsToISO(customer, []string{"first_seen_at", "last_seen_at"})

	subsEndpoint := fmt.Sprintf("/projects/%s/customers/%s/subscriptions", s.projectID, customerID)
	purchasesEndpoint := fmt.Sprintf("/projects/%s/customers/%s/purchases", s.projectID, customerID)

	type nestedResult struct {
		items []map[string]interface{}
		err   error
		name  string
	}

	ch := make(chan nestedResult, 2)

	go func() {
		items, err := s.paginate(ctx, s.customerClient, subsEndpoint)
		if err != nil {
			ch <- nestedResult{err: fmt.Errorf("failed to fetch subscriptions for customer %s: %w", customerID, err), name: "subscriptions"}
			return
		}
		for _, item := range items {
			convertTimestampsToISO(item, []string{"purchased_at", "expires_at", "grace_period_expires_at"})
		}
		ch <- nestedResult{items: items, name: "subscriptions"}
	}()

	go func() {
		items, err := s.paginate(ctx, s.customerClient, purchasesEndpoint)
		if err != nil {
			ch <- nestedResult{err: fmt.Errorf("failed to fetch purchases for customer %s: %w", customerID, err), name: "purchases"}
			return
		}
		for _, item := range items {
			convertTimestampsToISO(item, []string{"purchased_at", "expires_at"})
		}
		ch <- nestedResult{items: items, name: "purchases"}
	}()

	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			return nil, res.err
		}
		if res.items == nil {
			res.items = []map[string]interface{}{}
		}
		asIface := make([]interface{}, len(res.items))
		for j, item := range res.items {
			asIface[j] = item
		}
		customer[res.name] = asIface
	}

	return customer, nil
}

func (s *RevenueCatSource) readProducts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REVENUECAT] reading products")
	endpoint := fmt.Sprintf("/projects/%s/products", s.projectID)
	return s.paginateAndSend(ctx, s.projectClient, endpoint, nil, opts, results)
}

func (s *RevenueCatSource) readEntitlements(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REVENUECAT] reading entitlements")
	endpoint := fmt.Sprintf("/projects/%s/entitlements", s.projectID)
	return s.paginateAndSend(ctx, s.projectClient, endpoint, nil, opts, results)
}

func (s *RevenueCatSource) readOfferings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[REVENUECAT] reading offerings")
	endpoint := fmt.Sprintf("/projects/%s/offerings", s.projectID)
	return s.paginateAndSend(ctx, s.projectClient, endpoint, nil, opts, results)
}
