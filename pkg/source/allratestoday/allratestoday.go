// Package allratestoday is a source for the official exchange rate tables that central banks
// and tax authorities publish, as served by the keyless open endpoints of allratestoday.com.
//
// Each table is the latest one the institution has published (for example the RBI reference
// rate, the NBP table A middle rates or the Bank of England spot rates). The open endpoints
// serve only the latest table, so the rates table is delete+insert on date: a scheduled run
// accumulates one table per publication.
package allratestoday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
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
	baseURL = "https://allratestoday.com/api/open/"
	// The open endpoints are rate limited per IP; a run issues one or two requests, so this
	// only matters for a misconfigured loop.
	rateLimit      = 2
	rateLimitBurst = 2
)

var supportedTables = []string{
	"central_banks",
	"central_bank_rates",
}

// bankCodePattern keeps the bank code to the slug form the API uses, so the value from the
// table name can never change the request path.
var bankCodePattern = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

var bankFields = []schema.Column{
	{Name: "code", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: false},
	{Name: "latest_date", DataType: schema.TypeDate, Nullable: true},
	{Name: "cadence", DataType: schema.TypeString, Nullable: true},
	{Name: "stale", DataType: schema.TypeBoolean, Nullable: true},
}

var rateFields = []schema.Column{
	{Name: "bank", DataType: schema.TypeString, Nullable: false},
	{Name: "date", DataType: schema.TypeDate, Nullable: false},
	{Name: "base", DataType: schema.TypeString, Nullable: false},
	{Name: "quote", DataType: schema.TypeString, Nullable: false},
	{Name: "rate_type", DataType: schema.TypeString, Nullable: false},
	{Name: "rate", DataType: schema.TypeFloat64, Nullable: false},
}

type Source struct {
	client *ingestrhttp.Client
}

func New() *Source { return &Source{} }

func (s *Source) HandlesIncrementality() bool { return true }

func (s *Source) Schemes() []string { return []string{"allratestoday"} }

func (s *Source) Connect(ctx context.Context, uri string) error {
	if !strings.HasPrefix(uri, "allratestoday://") {
		return fmt.Errorf("invalid allratestoday URI: must start with allratestoday://")
	}

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
	)
	config.Debug("[ALLRATESTODAY] Connected")
	return nil
}

func (s *Source) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// parseTableName splits "central_bank_rates:rbi" into the table and the bank code.
func parseTableName(name string) (table, bank string, err error) {
	table = name
	if strings.Contains(name, ":") {
		parts := strings.SplitN(name, ":", 2)
		table = parts[0]
		bank = strings.ToLower(strings.TrimSpace(parts[1]))
	}

	if !isValidTable(table) {
		return "", "", fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	if table == "central_bank_rates" {
		if bank == "" {
			return "", "", fmt.Errorf("central_bank_rates needs a bank code, e.g. --source-table 'central_bank_rates:ecb' (list them with the central_banks table)")
		}
		if !bankCodePattern.MatchString(bank) {
			return "", "", fmt.Errorf("invalid bank code %q", bank)
		}
	}

	return table, bank, nil
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, bank, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}

	tableSchema, primaryKeys := getSchema(tableName)

	strategy := config.StrategyReplace
	var incrementalKey, partitionBy string
	if tableName == "central_bank_rates" {
		strategy = config.StrategyDeleteInsert
		incrementalKey = "date"
		partitionBy = "date"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		TablePartitionBy:    partitionBy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, bank, opts)
		},
	}, nil
}

func getSchema(table string) (*schema.TableSchema, []string) {
	var columns []schema.Column
	var primaryKeys []string

	switch table {
	case "central_banks":
		columns = bankFields
		primaryKeys = []string{"code"}
	case "central_bank_rates":
		columns = rateFields
		primaryKeys = []string{"bank", "date", "base", "quote", "rate_type"}
	}

	return &schema.TableSchema{Columns: columns, PrimaryKeys: primaryKeys}, primaryKeys
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *Source) read(ctx context.Context, table, bank string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "central_banks":
			err = s.readCentralBanks(ctx, opts, results)
		case "central_bank_rates":
			err = s.readCentralBankRates(ctx, bank, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *Source) get(ctx context.Context, path string) ([]byte, error) {
	resp, err := s.client.R(ctx).Get(path)
	if err != nil {
		return nil, fmt.Errorf("allratestoday request to %s failed: %w", path, err)
	}
	if !resp.IsSuccess() {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(resp.Body(), &apiErr) == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("allratestoday request to %s failed with status %d: %s", path, resp.StatusCode(), apiErr.Error)
		}
		return nil, fmt.Errorf("allratestoday request to %s failed with status %d", path, resp.StatusCode())
	}
	return resp.Body(), nil
}

func (s *Source) readCentralBanks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	body, err := s.get(ctx, "central-banks")
	if err != nil {
		return err
	}

	var result struct {
		Sources []struct {
			Code    string `json:"code"`
			Name    string `json:"name"`
			Latest  string `json:"latest"`
			Cadence string `json:"cadence"`
			Stale   *bool  `json:"stale"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse central-banks response: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Sources))
	for _, src := range result.Sources {
		row := map[string]interface{}{
			"code":        src.Code,
			"name":        src.Name,
			"latest_date": nil,
			"cadence":     nil,
			"stale":       nil,
		}
		if src.Latest != "" {
			row["latest_date"] = src.Latest
		}
		if src.Cadence != "" {
			row["cadence"] = src.Cadence
		}
		if src.Stale != nil {
			row["stale"] = *src.Stale
		}
		items = append(items, row)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i]["code"].(string) < items[j]["code"].(string)
	})

	if err := emit(items, bankFields, opts, results); err != nil {
		return err
	}
	config.Debug("[ALLRATESTODAY] Fetched %d central banks", len(items))
	return nil
}

func (s *Source) readCentralBankRates(ctx context.Context, bank string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	body, err := s.get(ctx, "central-bank/"+url.PathEscape(bank))
	if err != nil {
		return err
	}

	var result struct {
		Bank     string `json:"bank"`
		RateDate string `json:"rate_date"`
		Rates    []struct {
			Base  string   `json:"base"`
			Quote string   `json:"quote"`
			Type  string   `json:"type"`
			Value *float64 `json:"value"`
		} `json:"rates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse central-bank response for %s: %w", bank, err)
	}
	if result.RateDate == "" {
		return fmt.Errorf("central-bank response for %s has no rate_date", bank)
	}

	items := make([]map[string]interface{}, 0, len(result.Rates))
	for _, r := range result.Rates {
		if r.Value == nil || r.Base == "" || r.Quote == "" {
			continue
		}
		rateType := r.Type
		if rateType == "" {
			rateType = "reference"
		}
		items = append(items, map[string]interface{}{
			"bank":      bank,
			"date":      result.RateDate,
			"base":      strings.ToUpper(r.Base),
			"quote":     strings.ToUpper(r.Quote),
			"rate_type": rateType,
			"rate":      *r.Value,
		})
	}

	if err := emit(items, rateFields, opts, results); err != nil {
		return err
	}
	config.Debug("[ALLRATESTODAY] Fetched %d rates from %s for %s", len(items), bank, result.RateDate)
	return nil
}

// emit sends items as Arrow batches, splitting them when opts.MaxBatchBytes is set.
func emit(items []map[string]interface{}, fields []schema.Column, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	var batch []map[string]interface{}
	var accBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, fields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert rows to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		batch = nil
		accBytes = 0
		return nil
	}
	for _, row := range items {
		if opts.MaxBatchBytes > 0 {
			rowBytes := arrowconv.RowBytes(row)
			if len(batch) > 0 && accBytes+rowBytes > opts.MaxBatchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
			accBytes += rowBytes
		}
		batch = append(batch, row)
	}
	return flush()
}

var _ source.Source = (*Source)(nil)
