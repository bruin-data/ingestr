package yfinance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	baseURL   = "https://query2.finance.yahoo.com"
	cookieURL = "https://fc.yahoo.com"
	crumbURL  = "https://query1.finance.yahoo.com/v1/test/getcrumb"
	// Yahoo rejects non-browser user agents with 429 on some endpoints.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

	// Yahoo publishes no rate limit and throttles bursts with 429; stay conservative.
	rateLimit      = 4.0
	rateLimitBurst = 5

	quoteChunkSize = 50
	newsCount      = 50
)

var supportedTables = []string{
	"history",
	"dividends",
	"splits",
	"quotes",
	"info",
	"options",
	"income_statement",
	"balance_sheet",
	"cash_flow",
	"news",
}

var defaultInfoModules = []string{
	"assetProfile",
	"summaryDetail",
	"price",
	"quoteType",
	"defaultKeyStatistics",
	"financialData",
	"calendarEvents",
}

var (
	errSymbolNotFound = errors.New("symbol not found")
	errNoData         = errors.New("no data in range")
)

type YFinanceSource struct {
	client *ingestrhttp.Client

	crumbMu sync.Mutex
	crumb   string
}

func NewYFinanceSource() *YFinanceSource {
	return &YFinanceSource{}
}

func (s *YFinanceSource) HandlesIncrementality() bool {
	return true
}

func (s *YFinanceSource) Schemes() []string {
	return []string{"yfinance"}
}

func parseURI(uri string) error {
	if !strings.HasPrefix(uri, "yfinance://") {
		return fmt.Errorf("invalid yfinance URI: must start with yfinance://")
	}
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("failed to parse yfinance URI: %w", err)
	}
	if len(u.Query()) > 0 {
		return fmt.Errorf("yfinance URI takes no parameters; pass symbols in the table name, e.g. --source-table 'history:AAPL,MSFT'")
	}
	return nil
}

func (s *YFinanceSource) Connect(ctx context.Context, uri string) error {
	if err := parseURI(uri); err != nil {
		return err
	}

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithUserAgent(userAgent),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
	)
	config.Debug("[YFINANCE] Connected")
	return nil
}

func (s *YFinanceSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

type tableSpec struct {
	name      string
	symbols   []string
	interval  string
	prepost   bool
	frequency string
	modules   []string
}

type tableParams struct {
	Symbols   []string `mapstructure:"symbols"`
	Interval  string   `mapstructure:"interval"`
	Prepost   bool     `mapstructure:"prepost"`
	Frequency string   `mapstructure:"frequency"`
	Modules   []string `mapstructure:"modules"`
}

// parseTableSpec accepts "history:AAPL,MSFT", "history?symbols=AAPL,MSFT&interval=1h",
// or both combined ("history:AAPL?interval=1h").
func parseTableSpec(raw string) (tableSpec, error) {
	var p tableParams
	path, _, err := tablespec.Parse(raw, &p, tablespec.WithListSeparator(","))
	if err != nil {
		return tableSpec{}, err
	}

	name, symbolList, _ := strings.Cut(path, ":")
	spec := tableSpec{name: strings.TrimSpace(name)}
	if !isValidTable(spec.name) {
		return tableSpec{}, fmt.Errorf("unsupported table: %s (supported: %s)", spec.name, strings.Join(supportedTables, ", "))
	}

	spec.symbols = normalizeSymbols(append(strings.Split(symbolList, ","), p.Symbols...))
	if len(spec.symbols) == 0 {
		return tableSpec{}, fmt.Errorf("table %s requires at least one symbol, e.g. '%s:AAPL,MSFT' or '%s?symbols=AAPL,MSFT'", spec.name, spec.name, spec.name)
	}

	if spec.name != "history" && (p.Interval != "" || p.Prepost) {
		return tableSpec{}, fmt.Errorf("interval and prepost are only supported for the history table")
	}
	if !isStatementTable(spec.name) && p.Frequency != "" {
		return tableSpec{}, fmt.Errorf("frequency is only supported for the income_statement, balance_sheet and cash_flow tables")
	}
	if spec.name != "info" && len(p.Modules) > 0 {
		return tableSpec{}, fmt.Errorf("modules is only supported for the info table")
	}

	switch {
	case spec.name == "history":
		spec.interval = p.Interval
		if spec.interval == "" {
			spec.interval = "1d"
		}
		if _, ok := intervalLimits[spec.interval]; !ok {
			return tableSpec{}, fmt.Errorf("unsupported interval %q (supported: %s)", spec.interval, strings.Join(supportedIntervals, ", "))
		}
		if p.Prepost && !isIntraday(spec.interval) {
			return tableSpec{}, fmt.Errorf("prepost is only supported for intraday intervals")
		}
		spec.prepost = p.Prepost
	case isStatementTable(spec.name):
		spec.frequency = strings.ToLower(p.Frequency)
		if spec.frequency == "" {
			spec.frequency = "annual"
		}
		valid := []string{"annual", "quarterly", "trailing"}
		if spec.name == "balance_sheet" {
			valid = []string{"annual", "quarterly"}
		}
		if !slices.Contains(valid, spec.frequency) {
			return tableSpec{}, fmt.Errorf("unsupported frequency %q for %s (supported: %s)", spec.frequency, spec.name, strings.Join(valid, ", "))
		}
	case spec.name == "info":
		spec.modules = p.Modules
		if len(spec.modules) == 0 {
			spec.modules = defaultInfoModules
		}
	}

	return spec, nil
}

func normalizeSymbols(raw []string) []string {
	var out []string
	for _, sym := range raw {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym != "" && !slices.Contains(out, sym) {
			out = append(out, sym)
		}
	}
	return out
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func isStatementTable(table string) bool {
	return table == "income_statement" || table == "balance_sheet" || table == "cash_flow"
}

func (s *YFinanceSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}

	var primaryKeys []string
	incrementalKey := ""
	strategy := config.StrategyReplace

	switch spec.name {
	case "history":
		primaryKeys = []string{"symbol", "interval", "date"}
		if isIntraday(spec.interval) {
			primaryKeys = []string{"symbol", "interval", "timestamp"}
		}
		incrementalKey = "timestamp"
		strategy = config.StrategyMerge
	case "dividends", "splits":
		primaryKeys = []string{"symbol", "date"}
		incrementalKey = "date"
		strategy = config.StrategyMerge
	case "quotes", "info":
		primaryKeys = []string{"symbol"}
	case "options":
		primaryKeys = []string{"contractSymbol"}
	case "income_statement", "balance_sheet", "cash_flow":
		primaryKeys = []string{"symbol", "frequency", "as_of_date", "metric"}
		strategy = config.StrategyMerge
	case "news":
		primaryKeys = []string{"uuid"}
		incrementalKey = "providerPublishTime"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           spec.name,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("yfinance schema is inferred from data")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, spec, opts)
		},
	}, nil
}

func (s *YFinanceSource) read(ctx context.Context, spec tableSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch spec.name {
		case "history":
			err = s.readHistory(ctx, spec, opts, results)
		case "dividends":
			err = s.readDividends(ctx, spec, opts, results)
		case "splits":
			err = s.readSplits(ctx, spec, opts, results)
		case "quotes":
			err = s.readQuotes(ctx, spec, opts, results)
		case "info":
			err = s.readInfo(ctx, spec, opts, results)
		case "options":
			err = s.readOptions(ctx, spec, opts, results)
		case "income_statement", "balance_sheet", "cash_flow":
			err = s.readStatement(ctx, spec, opts, results)
		case "news":
			err = s.readNews(ctx, spec, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", spec.name)
		}

		if err != nil {
			select {
			case results <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return results, nil
}

// getCrumb returns the session crumb required by the quote, quoteSummary and options
// endpoints. fc.yahoo.com answers 404 but sets the A3 cookie the crumb is bound to.
func (s *YFinanceSource) getCrumb(ctx context.Context, refresh bool) (string, error) {
	s.crumbMu.Lock()
	defer s.crumbMu.Unlock()

	if s.crumb != "" && !refresh {
		return s.crumb, nil
	}

	if _, err := s.client.R(ctx).Get(cookieURL); err != nil {
		return "", fmt.Errorf("failed to fetch Yahoo session cookie: %w", err)
	}

	resp, err := s.client.R(ctx).SetHeader("Accept", "text/plain").Get(crumbURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Yahoo crumb: %w", err)
	}
	crumb := strings.TrimSpace(resp.String())
	if !resp.IsSuccess() || crumb == "" || strings.ContainsAny(crumb, " <") {
		return "", fmt.Errorf("failed to fetch Yahoo crumb: status %d: %s", resp.StatusCode(), truncate(resp.String()))
	}

	s.crumb = crumb
	config.Debug("[YFINANCE] Obtained crumb")
	return crumb, nil
}

// get performs a GET and decodes the JSON body into out. A 404 is reported as
// errSymbolNotFound so callers can skip unknown or delisted symbols.
func (s *YFinanceSource) get(ctx context.Context, path string, params url.Values, needsCrumb bool, what string, out interface{}) error {
	for attempt := 0; ; attempt++ {
		req := s.client.R(ctx).SetQueryParamValues(params)
		if needsCrumb {
			crumb, err := s.getCrumb(ctx, attempt > 0)
			if err != nil {
				return err
			}
			req.SetQueryParam("crumb", crumb)
		}

		resp, err := req.Get(path)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", what, err)
		}
		if needsCrumb && resp.StatusCode() == http.StatusUnauthorized && attempt == 0 {
			config.Debug("[YFINANCE] Crumb rejected for %s, refreshing", what)
			continue
		}
		if resp.StatusCode() == http.StatusNotFound {
			return fmt.Errorf("%s: %w: %s", what, errSymbolNotFound, truncate(resp.String()))
		}
		// The chart endpoint answers 400 for a window before the symbol's first trade.
		if resp.StatusCode() == http.StatusBadRequest && strings.Contains(resp.String(), "Data doesn't exist") {
			return fmt.Errorf("%s: %w", what, errNoData)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("%s request failed with status %d: %s", what, resp.StatusCode(), truncate(resp.String()))
		}

		dec := json.NewDecoder(bytes.NewReader(resp.Body()))
		dec.UseNumber()
		if err := dec.Decode(out); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", what, err)
		}
		return nil
	}
}

func truncate(s string) string {
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// sendItems emits one response's rows, split into batches bounded by opts.MaxBatchBytes.
func sendItems(ctx context.Context, results chan<- source.RecordBatchResult, items []map[string]interface{}, cols []schema.Column, opts source.ReadOptions, what string) error {
	var batch []map[string]interface{}
	var accBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, cols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", what, err)
		}
		batch = nil
		accBytes = 0
		select {
		case results <- source.RecordBatchResult{Batch: record}:
			return nil
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
	}

	for _, item := range items {
		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(item)
			if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
			accBytes += rowBytes
		}
		batch = append(batch, item)
	}
	return flush()
}

func inInterval(t time.Time, opts source.ReadOptions) bool {
	if opts.IntervalStart != nil && t.Before(*opts.IntervalStart) {
		return false
	}
	if opts.IntervalEnd != nil && !t.Before(*opts.IntervalEnd) {
		return false
	}
	return true
}

var _ source.Source = (*YFinanceSource)(nil)
