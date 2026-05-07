package pinterest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.pinterest.com/v5"
	// Pinterest API rate limit: 1000 calls/min for standard read endpoints.
	// Using 80% of that: (1000 * 0.8) / 60 ≈ 13.3 req/s.
	rateLimit      = 13.3
	rateLimitBurst = 5
	maxPageSize    = 200
	maxPages       = 500
)

var supportedTables = []string{
	"pins",
	"boards",
}

type PinterestSource struct {
	accessToken string
	client      *gonghttp.Client
}

func NewPinterestSource() *PinterestSource {
	return &PinterestSource{}
}

func (s *PinterestSource) HandlesIncrementality() bool {
	return true
}

func (s *PinterestSource) Schemes() []string {
	return []string{"pinterest"}
}

func (s *PinterestSource) Connect(ctx context.Context, uri string) error {
	accessToken, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.accessToken = accessToken

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("Authorization", "Bearer "+s.accessToken),
		gonghttp.WithHeader("Accept", "application/json"),
	)

	config.Debug("[PINTEREST] Connected successfully")
	return nil
}

func (s *PinterestSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "pinterest://") {
		return "", fmt.Errorf("invalid pinterest URI: must start with pinterest://")
	}

	rest := strings.TrimPrefix(uri, "pinterest://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("access_token is required in pinterest URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse pinterest URI query: %w", err)
	}

	accessToken := values.Get("access_token")
	if accessToken == "" {
		return "", fmt.Errorf("access_token is required in pinterest URI")
	}

	return accessToken, nil
}

func (s *PinterestSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "created_at",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("pinterest source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func (s *PinterestSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "pins":
			err = s.readPins(ctx, opts, results)
		case "boards":
			err = s.readBoards(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

type pinterestResponse struct {
	Items    []map[string]any `json:"items"`
	Bookmark string           `json:"bookmark"`
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func (s *PinterestSource) paginateAndSend(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	totalSent := 0
	bookmark := ""
	page := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page_size", fmt.Sprintf("%d", maxPageSize))
		if bookmark != "" {
			req.SetQueryParam("bookmark", bookmark)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("pinterest %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		var body pinterestResponse
		if err := jsonUseNumber(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(body.Items) == 0 {
			break
		}

		items := filterByInterval(body.Items, opts)

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", label, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalSent += len(items)
			config.Debug("[PINTEREST] %s: sent %d records (total: %d)", label, len(items), totalSent)
		}

		if body.Bookmark == "" {
			break
		}

		page++
		if page >= maxPages {
			config.Debug("[PINTEREST] %s: reached maxPages limit (%d)", label, maxPages)
			break
		}

		bookmark = body.Bookmark
	}

	config.Debug("[PINTEREST] finished reading %s: %d total records", label, totalSent)
	return nil
}

func filterByInterval(items []map[string]any, opts source.ReadOptions) []map[string]any {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return items
	}

	var filtered []map[string]any
	for _, item := range items {
		ts, ok := item["created_at"].(string)
		if !ok {
			config.Debug("[PINTEREST] item %v has no created_at field, including in results", item["id"])
			filtered = append(filtered, item)
			continue
		}

		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			config.Debug("[PINTEREST] item %v has unparseable created_at %q, including in results", item["id"], ts)
			filtered = append(filtered, item)
			continue
		}

		if opts.IntervalStart != nil && !t.After(*opts.IntervalStart) {
			continue
		}
		if opts.IntervalEnd != nil && t.After(*opts.IntervalEnd) {
			continue
		}

		filtered = append(filtered, item)
	}

	return filtered
}

func (s *PinterestSource) readPins(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PINTEREST] reading pins")
	return s.paginateAndSend(ctx, "/pins", "pins", opts, results)
}

func (s *PinterestSource) readBoards(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PINTEREST] reading boards")
	return s.paginateAndSend(ctx, "/boards", "boards", opts, results)
}
