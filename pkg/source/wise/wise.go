package wise

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL        = "https://api.transferwise.com"
	maxPageSize    = 100
	rateLimit      = 6.67 // Wise allows 500 req/min => (500*0.8)/60 ≈ 6.67
	rateLimitBurst = 5
)

var supportedTables = []string{
	"profiles",
	"transfers",
	"balances",
}

type WiseSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewWiseSource() *WiseSource {
	return &WiseSource{}
}

func (s *WiseSource) HandlesIncrementality() bool {
	return true
}

func (s *WiseSource) Schemes() []string {
	return []string{"wise"}
}

func (s *WiseSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("Authorization", "Bearer "+s.apiKey),
	)
	config.Debug("[WISE] Connected successfully")
	return nil
}

func (s *WiseSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "wise://") {
		return "", fmt.Errorf("invalid wise URI: must start with wise://")
	}

	rest := strings.TrimPrefix(uri, "wise://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in wise URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse wise URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in wise URI")
	}

	return apiKey, nil
}

func (s *WiseSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if !isValidTable(req.Name) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyMerge

	switch req.Name {
	case "transfers":
		incrementalKey = "created"
	case "balances":
		incrementalKey = "modificationTime"
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("wise source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
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

func (s *WiseSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "profiles":
			err = s.readProfiles(ctx, opts, results)
		case "transfers":
			err = s.readTransfers(ctx, opts, results)
		case "balances":
			err = s.readBalances(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *WiseSource) fetchProfileIDs(ctx context.Context) ([]json.Number, error) {
	resp, err := s.client.R(ctx).Get("/v2/profiles")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profiles: %w", err)
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("wise API /v2/profiles returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var profiles []map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(string(resp.Body())))
	decoder.UseNumber()
	if err := decoder.Decode(&profiles); err != nil {
		return nil, fmt.Errorf("failed to parse profiles response: %w", err)
	}

	var ids []json.Number
	for _, p := range profiles {
		if id, ok := p["id"].(json.Number); ok {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

func (s *WiseSource) readProfiles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[WISE] reading profiles")

	resp, err := s.client.R(ctx).Get("/v2/profiles")
	if err != nil {
		return fmt.Errorf("failed to fetch profiles: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("wise API /v2/profiles returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var profiles []map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(string(resp.Body())))
	decoder.UseNumber()
	if err := decoder.Decode(&profiles); err != nil {
		return fmt.Errorf("failed to parse profiles response: %w", err)
	}

	if len(profiles) == 0 {
		config.Debug("[WISE] No profiles found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(profiles, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert profiles to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[WISE] Sent %d profile records", len(profiles))
	return nil
}

func (s *WiseSource) readTransfers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[WISE] reading transfers")

	profileIDs, err := s.fetchProfileIDs(ctx)
	if err != nil {
		return err
	}

	if opts.IntervalStart == nil {
		return fmt.Errorf("wise transfers requires --interval-start to be set")
	}

	startDate := opts.IntervalStart.Format("2006-01-02")
	endDate := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	if opts.IntervalEnd != nil {
		endDate = opts.IntervalEnd.AddDate(0, 0, 1).Format("2006-01-02")
	}

	totalSent := 0

	for _, profileID := range profileIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		offset := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			req := s.client.R(ctx).
				SetQueryParam("profile", profileID.String()).
				SetQueryParam("limit", fmt.Sprintf("%d", maxPageSize)).
				SetQueryParam("offset", fmt.Sprintf("%d", offset)).
				SetQueryParam("createdDateStart", startDate).
				SetQueryParam("createdDateEnd", endDate)

			resp, err := req.Get("/v1/transfers")
			if err != nil {
				return fmt.Errorf("failed to fetch transfers for profile %s: %w", profileID.String(), err)
			}

			if !resp.IsSuccess() {
				return fmt.Errorf("wise API /v1/transfers returned status %d: %s", resp.StatusCode(), resp.String())
			}

			var transfers []map[string]interface{}
			decoder := json.NewDecoder(strings.NewReader(string(resp.Body())))
			decoder.UseNumber()
			if err := decoder.Decode(&transfers); err != nil {
				return fmt.Errorf("failed to parse transfers response: %w", err)
			}

			if len(transfers) == 0 {
				break
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema(transfers, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert transfers to Arrow: %w", err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(transfers)
			config.Debug("[WISE] Profile %s: sent %d transfers (total: %d)", profileID.String(), len(transfers), totalSent)

			if len(transfers) < maxPageSize {
				break
			}
			offset += maxPageSize
		}
	}

	config.Debug("[WISE] Total transfers sent: %d", totalSent)
	return nil
}

func filterBalances(balances []map[string]any, intervalStart, intervalEnd *time.Time) []map[string]any {
	if intervalStart == nil && intervalEnd == nil {
		return balances
	}

	var filtered []map[string]any
	for _, balance := range balances {
		modTimeStr, ok := balance["modificationTime"].(string)
		if !ok {
			config.Debug("[WISE] balance record missing modificationTime, skipping: id=%v", balance["id"])
			continue
		}

		modTime, err := time.Parse(time.RFC3339, modTimeStr)
		if err != nil {
			modTime, err = time.Parse("2006-01-02T15:04:05.999Z", modTimeStr)
			if err != nil {
				config.Debug("[WISE] Failed to parse modificationTime: %s", modTimeStr)
				continue
			}
		}

		if intervalStart != nil && modTime.Before(*intervalStart) {
			continue
		}
		if intervalEnd != nil && modTime.After(*intervalEnd) {
			continue
		}

		filtered = append(filtered, balance)
	}
	return filtered
}

func (s *WiseSource) readBalances(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[WISE] reading balances")

	profileIDs, err := s.fetchProfileIDs(ctx)
	if err != nil {
		return err
	}

	var allBalances []map[string]interface{}

	for _, profileID := range profileIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		endpoint := fmt.Sprintf("/v4/profiles/%s/balances", profileID.String())
		resp, err := s.client.R(ctx).
			SetQueryParam("types", "STANDARD,SAVINGS").
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch balances for profile %s: %w", profileID.String(), err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("wise API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var balances []map[string]interface{}
		decoder := json.NewDecoder(strings.NewReader(string(resp.Body())))
		decoder.UseNumber()
		if err := decoder.Decode(&balances); err != nil {
			return fmt.Errorf("failed to parse balances response: %w", err)
		}

		allBalances = append(allBalances, filterBalances(balances, opts.IntervalStart, opts.IntervalEnd)...)
	}

	if len(allBalances) == 0 {
		config.Debug("[WISE] No balances found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(allBalances, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert balances to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[WISE] Sent %d balance records", len(allBalances))
	return nil
}
