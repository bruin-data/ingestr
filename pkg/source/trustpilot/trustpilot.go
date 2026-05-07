package trustpilot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL          = "https://api.trustpilot.com/v1"
	pageSize         = 100
	defaultRateLimit = 2.5
	defaultBurst     = 2
)

type TrustpilotSource struct {
	client         *gonghttp.Client
	businessUnitID string
	apiKey         string
}

func NewTrustpilotSource() *TrustpilotSource {
	return &TrustpilotSource{}
}

func (s *TrustpilotSource) Schemes() []string {
	return []string{"trustpilot"}
}

func (s *TrustpilotSource) Connect(ctx context.Context, uri string) error {
	businessUnitID, apiKey, err := parseTrustpilotURI(uri)
	if err != nil {
		return err
	}

	s.businessUnitID = businessUnitID
	s.apiKey = apiKey

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(defaultRateLimit, defaultBurst),
		gonghttp.WithDebug(config.DebugMode),
	)

	config.Debug("[TRUSTPILOT] Connected successfully")
	return nil
}

func parseTrustpilotURI(uri string) (businessUnitID, apiKey string, err error) {
	if !strings.HasPrefix(uri, "trustpilot://") {
		return "", "", fmt.Errorf("invalid trustpilot URI: must start with trustpilot://")
	}

	rest := strings.TrimPrefix(uri, "trustpilot://")

	parts := strings.SplitN(rest, "?", 2)
	businessUnitID = parts[0]
	if businessUnitID == "" {
		return "", "", fmt.Errorf("invalid trustpilot URI: business_unit_id is required")
	}

	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid trustpilot URI: api_key query parameter is required")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("failed to parse trustpilot URI query: %w", err)
	}

	apiKey = values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("invalid trustpilot URI: api_key query parameter is required")
	}

	return businessUnitID, apiKey, nil
}

func (s *TrustpilotSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *TrustpilotSource) HandlesIncrementality() bool {
	return true
}

func (s *TrustpilotSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	table := req.Name

	return &source.DynamicSourceTable{
		TableName:           table,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "updated_at",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("trustpilot source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, table, opts)
		},
	}, nil
}

func (s *TrustpilotSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error

		switch table {
		case "reviews":
			err = s.readReviews(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s, supported tables are: reviews", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *TrustpilotSource) readReviews(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startTime, err := toTime(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("interval_start is required for trustpilot reviews")
	}
	endTime, err := toTime(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("interval_end is required for trustpilot reviews")
	}

	endpoint := fmt.Sprintf("/business-units/%s/reviews", s.businessUnitID)
	startDateStr := startTime.UTC().Format(time.RFC3339)
	endDateStr := endTime.UTC().Format(time.RFC3339)

	config.Debug("[TRUSTPILOT] Fetching reviews from %s to %s", startDateStr, endDateStr)

	page := 1
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var result map[string]interface{}
		resp, err := s.client.R(ctx).
			SetQueryParam("apikey", s.apiKey).
			SetQueryParam("startDateTime", startDateStr).
			SetQueryParam("endDateTime", endDateStr).
			SetQueryParam("perPage", strconv.Itoa(pageSize)).
			SetQueryParam("page", strconv.Itoa(page)).
			SetResult(&result).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch reviews: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("API returned status %d for %s", resp.StatusCode(), endpoint)
		}

		reviewsRaw, ok := result["reviews"].([]interface{})
		if !ok || len(reviewsRaw) == 0 {
			break
		}

		var reviews []map[string]interface{}
		for _, r := range reviewsRaw {
			if review, ok := r.(map[string]interface{}); ok {
				reviews = append(reviews, review)
			}
		}

		if len(reviews) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(reviews, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert reviews to Arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[TRUSTPILOT] Sent page %d with %d reviews", page, len(reviews))
		}

		if len(reviewsRaw) < pageSize {
			break
		}

		page++
	}

	return nil
}

func toTime(v any) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("value is nil")
	}
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		if t != nil {
			return *t, nil
		}
		return time.Time{}, fmt.Errorf("value is nil")
	default:
		return time.Time{}, fmt.Errorf("unsupported type %T", v)
	}
}

var _ source.Source = (*TrustpilotSource)(nil)
