package solidgate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

var subscriptionSchema = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "started_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "expired_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "next_charge_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "payment_type", DataType: schema.TypeString, Nullable: true},
	{Name: "trial", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "cancelled_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "cancellation_requested_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "cancel_code", DataType: schema.TypeString, Nullable: true},
	{Name: "cancel_message", DataType: schema.TypeString, Nullable: true},
	{Name: "customer", DataType: schema.TypeJSON, Nullable: true},
	{Name: "product", DataType: schema.TypeJSON, Nullable: true},
	{Name: "invoices", DataType: schema.TypeJSON, Nullable: true},
}

var apmOrderSchema = []schema.Column{
	{Name: "order_id", DataType: schema.TypeString, Nullable: false},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "order_description", DataType: schema.TypeString, Nullable: true},
	{Name: "method", DataType: schema.TypeString, Nullable: true},
	{Name: "amount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "processing_amount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "processing_currency", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_account_id", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_email", DataType: schema.TypeString, Nullable: true},
	{Name: "ip_address", DataType: schema.TypeString, Nullable: true},
	{Name: "geo_country", DataType: schema.TypeString, Nullable: true},
	{Name: "error_code", DataType: schema.TypeString, Nullable: true},
	{Name: "transactions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "order_metadata", DataType: schema.TypeJSON, Nullable: true},
}

var cardOrderSchema = []schema.Column{
	{Name: "order_id", DataType: schema.TypeString, Nullable: false},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "order_description", DataType: schema.TypeString, Nullable: true},
	{Name: "psp_order_id", DataType: schema.TypeString, Nullable: true},
	{Name: "provider_payment_id", DataType: schema.TypeString, Nullable: true},
	{Name: "amount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "processing_amount", DataType: schema.TypeInt64, Nullable: true},
	{Name: "processing_currency", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "payment_type", DataType: schema.TypeString, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "is_secured", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "routing", DataType: schema.TypeJSON, Nullable: true},
	{Name: "customer_account_id", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_email", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_first_name", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_last_name", DataType: schema.TypeString, Nullable: true},
	{Name: "ip_address", DataType: schema.TypeString, Nullable: true},
	{Name: "mid", DataType: schema.TypeString, Nullable: true},
	{Name: "traffic_source", DataType: schema.TypeString, Nullable: true},
	{Name: "platform", DataType: schema.TypeString, Nullable: true},
	{Name: "geo_country", DataType: schema.TypeString, Nullable: true},
	{Name: "error_code", DataType: schema.TypeString, Nullable: true},
	{Name: "transactions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "order_metadata", DataType: schema.TypeJSON, Nullable: true},
	{Name: "fraudulent", DataType: schema.TypeBoolean, Nullable: true},
}

var financialEntrySchema = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "order_id", DataType: schema.TypeString, Nullable: true},
	{Name: "external_psp_order_id", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "transaction_datetime_provider", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "transaction_datetime_utc", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "accounting_date", DataType: schema.TypeDate, Nullable: true},
	{Name: "amount", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "amount_in_major_units", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "currency_minor_units", DataType: schema.TypeInt64, Nullable: true},
	{Name: "payout_amount", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "payout_amount_in_major_units", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "payout_currency", DataType: schema.TypeString, Nullable: true},
	{Name: "payout_currency_minor_units", DataType: schema.TypeInt64, Nullable: true},
	{Name: "record_type_key", DataType: schema.TypeString, Nullable: true},
	{Name: "provider", DataType: schema.TypeString, Nullable: true},
	{Name: "payment_method", DataType: schema.TypeString, Nullable: true},
	{Name: "card_brand", DataType: schema.TypeString, Nullable: true},
	{Name: "geo_country", DataType: schema.TypeString, Nullable: true},
	{Name: "issuing_country", DataType: schema.TypeString, Nullable: true},
	{Name: "transaction_id", DataType: schema.TypeString, Nullable: true},
	{Name: "chargeback_id", DataType: schema.TypeString, Nullable: true},
	{Name: "legal_entity", DataType: schema.TypeString, Nullable: true},
}

const (
	baseURL         = "https://reports.solidgate.com/api/v1"
	retryStatusCode = 204
	dateFormat      = "2006-01-02 15:04:05"
	defaultPageSize = 100
)

type SolidGateSource struct {
	client    *httpclient.Client
	publicKey string
	secretKey string
}

func NewSolidGateSource() *SolidGateSource {
	return &SolidGateSource{}
}

func (s *SolidGateSource) Schemes() []string {
	return []string{"solidgate"}
}

func (s *SolidGateSource) Connect(ctx context.Context, uri string) error {
	publicKey, secretKey, err := parseSolidGateURI(uri)
	if err != nil {
		return err
	}

	s.publicKey = publicKey
	s.secretKey = secretKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithRetryCondition(func(resp *httpclient.Response, err error) bool {
			return resp != nil && resp.StatusCode() == retryStatusCode
		}),
	)

	config.Debug("[SOLIDGATE] Connected successfully")
	return nil
}

func parseSolidGateURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "solidgate://") {
		return "", "", fmt.Errorf("invalid solidgate URI: must start with solidgate://")
	}

	rest := strings.TrimPrefix(uri, "solidgate://")
	if rest == "" || rest == "?" {
		return "", "", fmt.Errorf("public_key and secret_key are required in solidgate URI")
	}

	rest = strings.TrimPrefix(rest, "?")
	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse solidgate URI query parameters: %v", err)
	}

	publicKey := values.Get("public_key")
	if publicKey == "" {
		return "", "", fmt.Errorf("public_key is required in solidgate URI")
	}

	secretKey := values.Get("secret_key")
	if secretKey == "" {
		return "", "", fmt.Errorf("secret_key is required in solidgate URI")
	}

	return publicKey, secretKey, nil
}

func (s *SolidGateSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SolidGateSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	tableColumns   []schema.Column
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

var supportedTables = map[string]tableMeta{
	"subscriptions":     {tableColumns: subscriptionSchema, primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge},
	"apm_orders":        {tableColumns: apmOrderSchema, primaryKeys: []string{"order_id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge},
	"card_orders":       {tableColumns: cardOrderSchema, primaryKeys: []string{"order_id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge},
	"financial_entries": {tableColumns: financialEntrySchema, primaryKeys: []string{"id"}, incrementalKey: "created_at", strategy: config.StrategyMerge},
}

func (s *SolidGateSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	meta, exists := supportedTables[tableName]
	if !exists {
		tables := make([]string, 0, len(supportedTables))
		for t := range supportedTables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s, supported tables: %v", tableName, tables)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		TablePartitionBy:    "created_at",
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("solidgate source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *SolidGateSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "subscriptions":
			err = s.readSubscriptions(ctx, opts, results)
		case "apm_orders":
			err = s.readApmOrders(ctx, opts, results)
		case "card_orders":
			err = s.readCardOrders(ctx, opts, results)
		case "financial_entries":
			err = s.readFinancialEntries(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SolidGateSource) signature(jsonBody string) string {
	data := s.publicKey + jsonBody + s.publicKey
	mac := hmac.New(sha512.New, []byte(s.secretKey))
	mac.Write([]byte(data))
	hexHash := hex.EncodeToString(mac.Sum(nil))
	return base64.StdEncoding.EncodeToString([]byte(hexHash))
}

func (s *SolidGateSource) postWithAuth(ctx context.Context, endpoint string, body map[string]any) ([]byte, error) {
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	jsonBody := string(jsonBytes)
	sig := s.signature(jsonBody)

	var rawResp json.RawMessage
	resp, err := s.client.R(ctx).
		SetHeader("merchant", s.publicKey).
		SetHeader("signature", sig).
		SetHeader("Content-Type", "application/json").
		SetBody(jsonBody).
		SetResult(&rawResp).
		Post(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", endpoint, err)
	}

	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("solidgate API error %d: %s", resp.StatusCode(), resp.String())
	}

	return rawResp, nil
}

func (s *SolidGateSource) paginateTable(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, tableName, endpoint, itemsKey string, tableSchema []schema.Column) error {
	startTime, err := toTime(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("date_from is required for solidgate %s: provide --interval-start", tableName)
	}

	endTime, err := toTime(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("date_to is required for solidgate %s: provide --interval-end", tableName)
	}

	body := map[string]any{
		"limit":     defaultPageSize,
		"date_from": startTime.UTC().Format(dateFormat),
		"date_to":   endTime.UTC().Format(dateFormat),
	}

	totalSent := 0
	batchNum := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rawResp, err := s.postWithAuth(ctx, endpoint, body)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}

		var page map[string]json.RawMessage
		if err := json.Unmarshal(rawResp, &page); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", tableName, err)
		}

		var items []map[string]any
		if raw, ok := page[itemsKey]; ok {
			switch itemsKey {
			case "subscriptions":
				var itemMap map[string]map[string]any
				if err := json.Unmarshal(raw, &itemMap); err != nil {
					return fmt.Errorf("failed to parse %s items: %w", tableName, err)
				}
				for _, v := range itemMap {
					items = append(items, v)
				}
			case "orders":
				if err := json.Unmarshal(raw, &items); err != nil {
					return fmt.Errorf("failed to parse %s items: %w", tableName, err)
				}
			}
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, tableSchema, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
			}

			batchNum++
			totalSent += len(items)
			config.Debug("[SOLIDGATE] Sending batch %d with %d %s (total: %d)", batchNum, len(items), tableName, totalSent)
			results <- source.RecordBatchResult{Batch: record}

			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[SOLIDGATE] Reached limit of %d %s", opts.Limit, tableName)
				break
			}
		}

		var nextIterator string
		if raw, ok := page["metadata"]; ok {
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(raw, &metadata); err == nil {
				if iter, ok := metadata["next_page_iterator"]; ok {
					_ = json.Unmarshal(iter, &nextIterator)
				}
			}
		}

		if nextIterator == "" {
			break
		}

		body["next_page_iterator"] = nextIterator
	}

	if totalSent == 0 {
		config.Debug("[SOLIDGATE] No %s found", tableName)
	}

	return nil
}

func (s *SolidGateSource) readSubscriptions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginateTable(ctx, opts, results, "subscriptions", "/subscriptions", "subscriptions", subscriptionSchema)
}

func (s *SolidGateSource) readApmOrders(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginateTable(ctx, opts, results, "apm_orders", "/apm-orders", "orders", apmOrderSchema)
}

func (s *SolidGateSource) readCardOrders(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginateTable(ctx, opts, results, "card_orders", "/card-orders", "orders", cardOrderSchema)
}

func (s *SolidGateSource) readFinancialEntries(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startTime, err := toTime(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("date_from is required for solidgate financial_entries: provide --interval-start")
	}

	endTime, err := toTime(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("date_to is required for solidgate financial_entries: provide --interval-end")
	}

	body := map[string]any{
		"limit":     defaultPageSize,
		"date_from": startTime.UTC().Format(dateFormat),
		"date_to":   endTime.UTC().Format(dateFormat),
	}

	rawResp, err := s.postWithAuth(ctx, "/finance/financial_entries", body)
	if err != nil {
		return fmt.Errorf("failed to request financial entries report: %w", err)
	}

	var reportResp struct {
		URL string `json:"report_url"`
	}
	if err := json.Unmarshal(rawResp, &reportResp); err != nil {
		return fmt.Errorf("failed to parse financial entries report response: %w", err)
	}
	if reportResp.URL == "" {
		return fmt.Errorf("financial entries report response missing url field")
	}

	config.Debug("[SOLIDGATE] Financial entries report URL: %s", reportResp.URL)

	csvBytes, err := s.pollReportURL(ctx, reportResp.URL)
	if err != nil {
		return fmt.Errorf("failed to download financial entries report: %w", err)
	}

	items, err := parseCSV(csvBytes)
	if err != nil {
		return fmt.Errorf("failed to parse financial entries CSV: %w", err)
	}

	if len(items) == 0 {
		config.Debug("[SOLIDGATE] No financial_entries found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, financialEntrySchema, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert financial_entries to Arrow: %w", err)
	}

	config.Debug("[SOLIDGATE] Sending %d financial_entries", len(items))
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *SolidGateSource) pollReportURL(ctx context.Context, reportURL string) ([]byte, error) {
	sig := s.signature("")

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetHeader("merchant", s.publicKey).
			SetHeader("signature", sig).
			Get(reportURL)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode() == retryStatusCode {
			config.Debug("[SOLIDGATE] Report not ready yet, retrying...")
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if resp.StatusCode() >= 400 {
			return nil, fmt.Errorf("report download error %d: %s", resp.StatusCode(), resp.String())
		}

		return resp.Body(), nil
	}
}

func parseCSV(data []byte) ([]map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	headers, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	var items []map[string]any
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		empty := true
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}

		item := make(map[string]any, len(headers))
		for i, h := range headers {
			if i < len(row) {
				item[h] = row[i]
			}
		}
		items = append(items, item)
	}

	return items, nil
}

func toTime(v any) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("nil value")
	}
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		if t == nil {
			return time.Time{}, fmt.Errorf("nil pointer")
		}
		return *t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported type %T", v)
}

var _ source.Source = (*SolidGateSource)(nil)
