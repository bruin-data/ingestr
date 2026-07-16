package fastspring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
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
	baseURL        = "https://api.fastspring.com"
	maxPageSize    = 50
	maxPages       = 100000
	userAgent      = "ingestr" // mandatory for FastSpring API requests
	rateLimit      = 3.3
	rateLimitBurst = 5
	detailWorkers  = 5
	idColumn       = "id"
)

type tableConfig struct {
	path           string
	resultKey      string
	incrementalKey string
	strategy       config.IncrementalStrategy
	serverFilter   bool // list endpoint accepts begin/end date filters
	noPagination   bool
}

var supportedTables = map[string]tableConfig{
	"orders": {
		path:           "/orders",
		resultKey:      "orders",
		incrementalKey: "changed",
		strategy:       config.StrategyMerge,
		serverFilter:   true,
	},
	"subscriptions": {
		path:           "/subscriptions",
		resultKey:      "subscriptions",
		incrementalKey: "changed",
		strategy:       config.StrategyMerge,
		serverFilter:   true,
	},
	"accounts": {
		path:      "/accounts",
		resultKey: "accounts",
		strategy:  config.StrategyReplace,
	},
	"coupons": {
		path:      "/coupons",
		resultKey: "coupons",
		strategy:  config.StrategyReplace,
	},
	"products": {
		path:         "/products",
		resultKey:    "products",
		strategy:     config.StrategyReplace,
		noPagination: true,
	},
}

type FastspringSource struct {
	client *httpclient.Client
}

func NewFastspringSource() *FastspringSource {
	return &FastspringSource{}
}

func (s *FastspringSource) Schemes() []string {
	return []string{"fastspring"}
}

func (s *FastspringSource) HandlesIncrementality() bool {
	return true
}

func (s *FastspringSource) Connect(ctx context.Context, uri string) error {
	username, password, err := parseFastspringURI(uri)
	if err != nil {
		return err
	}

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBasicAuth(username, password)),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("Content-Type", "application/json"),
		httpclient.WithHeader("User-Agent", userAgent),
	)

	config.Debug("[FASTSPRING] Connected successfully")
	return nil
}

func parseFastspringURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "fastspring://") {
		return "", "", fmt.Errorf("invalid fastspring URI: must start with fastspring://")
	}

	rest := strings.TrimPrefix(uri, "fastspring://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse fastspring URI query: %w", err)
	}

	username := values.Get("username")
	if username == "" {
		return "", "", fmt.Errorf("username is required in fastspring URI")
	}
	password := values.Get("password")
	if password == "" {
		return "", "", fmt.Errorf("password is required in fastspring URI")
	}

	return username, password, nil
}

func (s *FastspringSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func isValidTable(name string) bool {
	_, ok := supportedTables[name]
	return ok
}

func supportedTableNames() string {
	names := make([]string, 0, len(supportedTables)+len(reportTables))
	for name := range supportedTables {
		names = append(names, name)
	}
	for name := range reportTables {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *FastspringSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if st, ok, err := s.resolveReportTable(req); err != nil {
		return nil, err
	} else if ok {
		return st, nil
	}

	tc, ok := supportedTables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported fastspring table %q, supported tables are: %s", req.Name, supportedTableNames())
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{idColumn},
		TableIncrementalKey: tc.incrementalKey,
		TableStrategy:       tc.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("fastspring source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, tc, opts)
		},
	}, nil
}

func (s *FastspringSource) read(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)

		if err := s.readWithDetails(ctx, table, tc, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *FastspringSource) readWithDetails(ctx context.Context, table string, tc tableConfig, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FASTSPRING] reading %s", table)

	ids, err := s.fetchIDs(ctx, tc, opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	fetchDetail := func(id string) error {
		resp, err := s.client.R(ctx).Get(tc.path + "/" + id)
		if err != nil {
			return fmt.Errorf("failed to fetch %s %s: %w", table, id, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("fastspring %s %s request failed with status %d: %s", table, id, resp.StatusCode(), resp.String())
		}
		items, err := parseObjects(resp.Body(), tc.resultKey)
		if err != nil {
			return fmt.Errorf("failed to parse %s details response: %w", table, err)
		}
		if len(items) == 0 {
			return nil
		}
		for _, item := range items {
			item[idColumn] = id
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: rec}:
		}
		return nil
	}

	idCh := make(chan string)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < detailWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idCh {
				if err := fetchDetail(id); err != nil {
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

	for _, id := range ids {
		select {
		case idCh <- id:
		case <-ctx.Done():
		}
	}
	close(idCh)
	wg.Wait()

	select {
	case err := <-errs:
		return err
	default:
	}

	config.Debug("[FASTSPRING] Finished reading %s, detail rows: %d", table, len(ids))
	return nil
}

// fetchIDs paginates a list endpoint and returns the identifier strings it yields.
func (s *FastspringSource) fetchIDs(ctx context.Context, tc tableConfig, opts source.ReadOptions) ([]string, error) {
	params := url.Values{}
	if !tc.noPagination {
		params.Set("limit", strconv.Itoa(maxPageSize))
	}
	if tc.serverFilter {
		if start := toTime(opts.IntervalStart); start != nil {
			params.Set("begin", start.UTC().Format("2006-01-02"))
		}
		if end := toTime(opts.IntervalEnd); end != nil {
			params.Set("end", end.UTC().Format("2006-01-02"))
		}
	}

	var ids []string
	page := 0
	pages := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if page > 0 && !tc.noPagination {
			params.Set("page", strconv.Itoa(page))
		}

		resp, err := s.client.R(ctx).SetQueryParamValues(params).Get(tc.path)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", tc.resultKey, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("fastspring %s request failed with status %d: %s", tc.resultKey, resp.StatusCode(), resp.String())
		}

		raw, nextPage, err := extractItems(resp.Body(), tc.resultKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", tc.resultKey, err)
		}

		ids = append(ids, collectIDs(raw)...)

		if len(raw) == 0 || nextPage <= 0 || nextPage == page {
			break
		}
		pages++
		if pages >= maxPages {
			config.Debug("[FASTSPRING] reached max page guard (%d) for %s", maxPages, tc.resultKey)
			break
		}
		page = nextPage
	}

	return ids, nil
}

func collectIDs(raw []interface{}) []string {
	ids := make([]string, 0, len(raw))
	for _, it := range raw {
		switch v := it.(type) {
		case string:
			ids = append(ids, v)
		case map[string]interface{}:
			if id, ok := v[idColumn].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func extractItems(body []byte, resultKey string) ([]interface{}, int, error) {
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		var arr []interface{}
		if err := dec.Decode(&arr); err != nil {
			return nil, 0, err
		}
		return arr, 0, nil
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var env map[string]interface{}
	if err := dec.Decode(&env); err != nil {
		return nil, 0, err
	}
	if res, _ := env["result"].(string); res == "error" {
		return nil, 0, fmt.Errorf("fastspring api returned an error: %v", env["error"])
	}

	var meta struct {
		NextPage int `json:"nextPage"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, 0, err
	}

	items, _ := env[resultKey].([]interface{})
	return items, meta.NextPage, nil
}

func parseObjects(body []byte, resultKey string) ([]map[string]interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}

	var raw []interface{}
	switch t := v.(type) {
	case []interface{}:
		raw = t
	case map[string]interface{}:
		if arr, ok := t[resultKey].([]interface{}); ok {
			raw = arr
		} else {
			return []map[string]interface{}{t}, nil
		}
	default:
		return nil, nil
	}

	items := make([]map[string]interface{}, 0, len(raw))
	for _, it := range raw {
		if m, ok := it.(map[string]interface{}); ok {
			items = append(items, m)
		}
	}
	return items, nil
}

func toTime(v *time.Time) *time.Time {
	if v == nil || v.IsZero() {
		return nil
	}
	return v
}

const reportPageSize = 1000

type reportConfig struct {
	path           string
	defaultColumns []string
	defaultGroupBy []string
	incrementalKey string
	filterKey      string
}

var reportTables = map[string]reportConfig{
	"subscription_report": {
		path: "/data/v1/subscription",
		defaultColumns: []string{
			"activations", "arr", "average_mrr", "buyer_email", "buyer_id",
			"cancellations", "chargeback_true_false", "churn_type", "company_id",
			"company_name", "country_iso", "country_name", "coupon", "customer_churn",
			"discount", "driving_offer_type", "driving_product_path", "lifetime_value",
			"mrr", "mrr_decrease", "mrr_downgrade", "mrr_growth_rate", "mrr_increase",
			"mrr_paused", "mrr_resumed", "mrr_upgrade", "new_subscribers", "occurred_date",
			"order_id", "product_display_name", "product_id", "product_name", "product_path",
			"purchase_type", "return_true_false", "revenue_churn", "segment", "store_id",
			"store_name", "subscriber_loss", "subscribers", "subscription_id",
			"subscription_period", "subscription_period_end", "subscription_period_start",
			"subscription_quantity", "subscription_start_date", "subscription_status",
			"subscription_true_false", "subscriptions", "sync_date", "transaction_currency",
			"transaction_date", "transaction_day", "transaction_month", "transaction_time_utc",
			"transaction_year",
		},
		defaultGroupBy: []string{"subscription_id", "transaction_date"},
		incrementalKey: "sync_date",
		filterKey:      "syncDate",
	},
	"revenue_report": {
		path: "/data/v1/revenue",
		defaultColumns: []string{
			"Buyer_Email", "Buyer_ID", "Chargeback_True_False", "Company_ID", "Company_Name",
			"Country_ISO", "Country_Name", "Coupon", "Digital_Backup_Fulfillment_Fee",
			"Digital_Backup_Fulfillment_Fee_in_USD", "Digital_Fulfillment_Fee",
			"Digital_Fulfillment_Fee_in_USD", "Discount", "Driving_Offer_Type",
			"Driving_Product_Path", "Fixed_Fee", "Fixed_Fee_in_USD", "Income", "Income_in_USD",
			"Item_ID", "Order_ID", "Physical_Backup_Fulfillment_Fee",
			"Physical_Backup_Fulfillment_Fee_in_USD", "Product_Display_Name", "Product_ID",
			"Product_Name", "Product_Path", "Purchase_Type", "Return_Fee", "Return_Fee_in_USD",
			"Return_True_False", "Segment", "Store_Chargeback_Fee", "Store_ID", "Store_Name",
			"Subscription_Period", "Subscription_Status", "Subscription_True_False", "Tax",
			"Tax_Fee", "Tax_Fee_in_USD", "Tax_in_USD", "Transaction_Amount",
			"Transaction_Amount_in_USD", "Transaction_Currency", "Transaction_Date",
			"Transaction_Day", "Transaction_Fee", "Transaction_Fee_in_USD",
			"Transaction_Item_Count", "Transaction_Month", "Transaction_Rate",
			"Transaction_Year", "Grand_Total_In_USD", "syncDate",
			"countryISO", "Product_Count", "Product_Units",
		},
		defaultGroupBy: []string{"order_id", "transaction_date"},
		incrementalKey: "syncdate",
		filterKey:      "syncDate",
	},
}

func (s *FastspringSource) resolveReportTable(req source.TableRequest) (source.SourceTable, bool, error) {
	parts := strings.Split(req.Name, ":")
	base := parts[0]
	rc, ok := reportTables[base]
	if !ok {
		return nil, false, nil
	}
	if len(parts) > 3 {
		return nil, false, fmt.Errorf("invalid report table %q, expected <report>[:columns[:group_by]]", req.Name)
	}

	columns := rc.defaultColumns
	if len(parts) > 1 && parts[1] != "" {
		columns = splitList(parts[1])
		if len(columns) == 0 {
			return nil, false, fmt.Errorf("invalid report table %q: columns segment %q has no valid column names", req.Name, parts[1])
		}
	}
	groupBy := rc.defaultGroupBy
	if len(parts) > 2 && parts[2] != "" {
		groupBy = splitList(parts[2])
		if len(groupBy) == 0 {
			return nil, false, fmt.Errorf("invalid report table %q: group_by segment %q has no valid fields (merge needs a primary key)", req.Name, parts[2])
		}
	}

	if !containsFold(columns, rc.incrementalKey) {
		columns = append(columns, rc.incrementalKey)
	}
	incrementalKey := rc.incrementalKey

	return &source.DynamicSourceTable{
		TableName:           base,
		TablePrimaryKeys:    groupBy,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("fastspring source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readReport(ctx, base, rc, columns, groupBy, opts)
		},
	}, true, nil
}

func (s *FastspringSource) readReport(ctx context.Context, table string, rc reportConfig, columns, groupBy []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 4)

	go func() {
		defer close(results)
		if err := s.fetchReport(ctx, table, rc, columns, groupBy, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func (s *FastspringSource) fetchReport(ctx context.Context, table string, rc reportConfig, columns, groupBy []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FASTSPRING] reading report %s", table)

	// Delta sync: the sync-date filter limits the report to rows synced on/after the
	// interval start, so each incremental run only pulls what changed since the last
	// run. filterKey is the request body key (case-sensitive: revenue uses "syncDate"),
	// which differs from the lowercased response column in incrementalKey. The filter
	// key must always be present in the request body: the subscription report returns
	// HTTP 500 when it is omitted, even if the filter itself is empty.
	filter := map[string]interface{}{}
	if start := toTime(opts.IntervalStart); start != nil {
		filter[rc.filterKey] = start.UTC().Format("2006-01-02")
	}

	pageNumber := 1
	totalSent := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := map[string]interface{}{
			"reportColumns": columns,
			"groupBy":       groupBy,
			"pageCount":     reportPageSize,
			"pageNumber":    pageNumber,
			"async":         false,
			"filter":        filter,
		}

		resp, err := s.client.R(ctx).SetBody(body).Post(rc.path)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", table, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("fastspring %s request failed with status %d: %s", table, resp.StatusCode(), resp.String())
		}

		dec := json.NewDecoder(bytes.NewReader(resp.Body()))
		dec.UseNumber()
		var env struct {
			Report []map[string]interface{} `json:"report"`
		}
		if err := dec.Decode(&env); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", table, err)
		}
		rows := env.Report
		if len(rows) > 0 {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", table, err)
			}
			totalSent += len(rows)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: rec}:
			}
		}

		if len(rows) < reportPageSize {
			break
		}
		pageNumber++
		if pageNumber > maxPages {
			config.Debug("[FASTSPRING] reached max page guard (%d) for %s", maxPages, table)
			break
		}
	}

	config.Debug("[FASTSPRING] Finished report %s, rows: %d", table, totalSent)
	return nil
}

var _ source.Source = (*FastspringSource)(nil)
