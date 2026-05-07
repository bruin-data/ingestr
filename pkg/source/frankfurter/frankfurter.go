package frankfurter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL        = "https://api.frankfurter.dev/v1/"
	rateLimit      = 10
	rateLimitBurst = 5
)

var supportedTables = []string{
	"currencies",
	"latest",
	"exchange_rates",
}

var currencyFields = []schema.Column{
	{Name: "currency_code", DataType: schema.TypeString, Nullable: false},
	{Name: "currency_name", DataType: schema.TypeString, Nullable: false},
}

var rateFields = []schema.Column{
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "currency_code", DataType: schema.TypeString, Nullable: false},
	{Name: "base_currency", DataType: schema.TypeString, Nullable: false},
	{Name: "rate", DataType: schema.TypeFloat64, Nullable: false},
}

type FrankfurterSource struct {
	client *ingestrhttp.Client
	base   string
}

func NewFrankfurterSource() *FrankfurterSource {
	return &FrankfurterSource{}
}

func (s *FrankfurterSource) HandlesIncrementality() bool {
	return true
}

func (s *FrankfurterSource) Schemes() []string {
	return []string{"frankfurter"}
}

func parseFrankfurterURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "frankfurter://") {
		return "", fmt.Errorf("invalid frankfurter URI: must start with frankfurter://")
	}

	rest := strings.TrimPrefix(uri, "frankfurter://")
	parts := strings.SplitN(rest, "?", 2)

	base := "EUR"
	if len(parts) == 2 {
		values, err := url.ParseQuery(parts[1])
		if err != nil {
			return "", fmt.Errorf("failed to parse frankfurter URI query: %w", err)
		}
		if b := values.Get("base"); b != "" {
			base = strings.ToUpper(b)
		}
	}

	return base, nil
}

func (s *FrankfurterSource) Connect(ctx context.Context, uri string) error {
	base, err := parseFrankfurterURI(uri)
	if err != nil {
		return err
	}

	s.base = base
	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
	)
	config.Debug("[FRANKFURTER] Connected successfully, base currency: %s", s.base)
	return nil
}

func (s *FrankfurterSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FrankfurterSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	tableSchema, primaryKeys := s.getSchema(tableName)

	incrementalKey := ""
	strategy := config.StrategyReplace

	switch tableName {
	case "latest":
		strategy = config.StrategyMerge
	case "exchange_rates":
		incrementalKey = "date"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *FrankfurterSource) getSchema(table string) (*schema.TableSchema, []string) {
	var columns []schema.Column
	var primaryKeys []string

	switch table {
	case "currencies":
		columns = currencyFields
		primaryKeys = []string{}
	case "latest":
		columns = rateFields
		primaryKeys = []string{"date", "currency_code", "base_currency"}
	case "exchange_rates":
		columns = rateFields
		primaryKeys = []string{"date", "currency_code", "base_currency"}
	}

	return &schema.TableSchema{
		Columns:     columns,
		PrimaryKeys: primaryKeys,
	}, primaryKeys
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *FrankfurterSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "currencies":
			err = s.readCurrencies(ctx, opts, results)
		case "latest":
			err = s.readLatest(ctx, opts, results)
		case "exchange_rates":
			err = s.readExchangeRates(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *FrankfurterSource) readCurrencies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRANKFURTER] Fetching currencies")

	resp, err := s.client.R(ctx).Get("currencies")
	if err != nil {
		return fmt.Errorf("failed to fetch currencies: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("currencies request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var currencies map[string]string
	if err := json.Unmarshal(resp.Body(), &currencies); err != nil {
		return fmt.Errorf("failed to parse currencies response: %w", err)
	}

	var items []map[string]interface{}
	for code, name := range currencies {
		items = append(items, map[string]interface{}{
			"currency_code": code,
			"currency_name": name,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i]["currency_code"].(string) < items[j]["currency_code"].(string)
	})

	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, currencyFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert currencies to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[FRANKFURTER] Fetched %d currencies", len(items))
	return nil
}

func (s *FrankfurterSource) readLatest(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRANKFURTER] Fetching latest rates")

	resp, err := s.client.R(ctx).Get(fmt.Sprintf("latest?base=%s", s.base))
	if err != nil {
		return fmt.Errorf("failed to fetch latest rates: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("latest rates request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return fmt.Errorf("failed to parse latest rates response: %w", err)
	}

	items := s.flattenRates(result.Date, result.Base, result.Rates)

	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, rateFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert latest rates to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[FRANKFURTER] Fetched %d latest rates", len(items))
	return nil
}

func (s *FrankfurterSource) readExchangeRates(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FRANKFURTER] Fetching exchange rates")

	now := time.Now().UTC()
	startDate := toDateString(opts.IntervalStart)
	if startDate == "" {
		startDate = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	endDate := toDateString(opts.IntervalEnd)
	if endDate == "" {
		endDate = now.Format("2006-01-02")
	}

	endpoint := fmt.Sprintf("%s..%s?base=%s", startDate, endDate, s.base)
	config.Debug("[FRANKFURTER] Fetching exchange rates from %s to %s", startDate, endDate)

	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch exchange rates: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("exchange rates request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var result struct {
		Base  string                        `json:"base"`
		Rates map[string]map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return fmt.Errorf("failed to parse exchange rates response: %w", err)
	}

	var dates []string
	for date := range result.Rates {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	var allItems []map[string]interface{}
	for _, date := range dates {
		rates := result.Rates[date]
		allItems = append(allItems, s.flattenRates(date, result.Base, rates)...)
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, rateFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert exchange rates to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[FRANKFURTER] Fetched %d exchange rate records", len(allItems))
	return nil
}

func (s *FrankfurterSource) flattenRates(date, base string, rates map[string]float64) []map[string]interface{} {
	var items []map[string]interface{}

	// Add base currency row with rate 1.0
	items = append(items, map[string]interface{}{
		"date":          date,
		"currency_code": base,
		"base_currency": base,
		"rate":          1.0,
	})

	codes := make([]string, 0, len(rates))
	for code := range rates {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	for _, code := range codes {
		items = append(items, map[string]interface{}{
			"date":          date,
			"currency_code": code,
			"base_currency": base,
			"rate":          rates[code],
		})
	}
	return items
}

func toDateString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case time.Time:
		return t.Format("2006-01-02")
	case *time.Time:
		if t != nil {
			return t.Format("2006-01-02")
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed.Format("2006-01-02")
		}
		return t
	}
	return ""
}

var _ source.Source = (*FrankfurterSource)(nil)
