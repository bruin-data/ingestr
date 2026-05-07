package socrata

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const defaultPageSize = 50000

type SocrataSource struct {
	client   *gonghttp.Client
	Domain   string
	AppToken string
	Username string
	Password string
}

func NewSocrataSource() *SocrataSource {
	return &SocrataSource{}
}

func (s *SocrataSource) Schemes() []string {
	return []string{"socrata"}
}

func (s *SocrataSource) Connect(ctx context.Context, uri string) error {
	domain, appToken, username, password, err := parseSocrataURI(uri)
	if err != nil {
		return err
	}
	s.Domain = domain
	s.AppToken = appToken
	s.Username = username
	s.Password = password

	opts := []gonghttp.Option{
		gonghttp.WithBaseURL("https://" + domain + "/resource"),
		gonghttp.WithTimeout(60 * time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("X-App-Token", appToken),
	}

	if username != "" && password != "" {
		opts = append(opts, gonghttp.WithAuth(gonghttp.NewBasicAuth(username, password)))
	}

	s.client = gonghttp.New(opts...)

	config.Debug("[SOCRATA] Connected successfully to %s", domain)
	return nil
}

func parseSocrataURI(uri string) (domain, appToken, username, password string, err error) {
	if !strings.HasPrefix(uri, "socrata://") {
		return "", "", "", "", fmt.Errorf("invalid socrata URI: must start with socrata://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse socrata URI: %w", err)
	}

	domain = parsed.Host
	if domain == "" {
		return "", "", "", "", fmt.Errorf("domain is required in socrata URI (socrata://<domain>?...)")
	}

	values := parsed.Query()
	appToken = values.Get("app_token")
	if appToken == "" {
		return "", "", "", "", fmt.Errorf("app_token is required in socrata URI")
	}

	return domain, appToken, values.Get("username"), values.Get("password"), nil
}

func (s *SocrataSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SocrataSource) HandlesIncrementality() bool {
	return false
}

func (s *SocrataSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	datasetID := req.Name

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           datasetID,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("socrata source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, datasetID, opts)
		},
	}, nil
}

func (s *SocrataSource) read(ctx context.Context, datasetID string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		batchSize := defaultPageSize
		if opts.Limit > 0 && opts.Limit < batchSize {
			batchSize = int(opts.Limit)
		}

		endpoint := fmt.Sprintf("/%s.json", datasetID)
		offset := 0
		totalSent := 0
		batchNum := 0

		for {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			req := s.client.R(ctx).
				SetQueryParam("$limit", fmt.Sprintf("%d", batchSize)).
				SetQueryParam("$offset", fmt.Sprintf("%d", offset))

			if opts.IncrementalKey != "" && opts.IntervalStart != nil {
				whereClause := buildWhereClause(opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd)
				if whereClause != "" {
					req.SetQueryParam("$where", whereClause)
				}
				req.SetQueryParam("$order", opts.IncrementalKey+" ASC")
			}

			var items []map[string]interface{}
			req.SetResult(&items)

			config.Debug("[SOCRATA] Fetching %s offset=%d limit=%d", datasetID, offset, batchSize)

			resp, err := req.Get(endpoint)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch dataset %s: %w", datasetID, err)}
				return
			}

			if resp.IsError() {
				results <- source.RecordBatchResult{Err: fmt.Errorf("socrata API error (status %d): %s", resp.StatusCode(), resp.String())}
				return
			}

			if len(items) == 0 {
				break
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}
				return
			}

			batchNum++
			totalSent += len(items)
			config.Debug("[SOCRATA] Sending batch %d with %d rows (total: %d)", batchNum, len(items), totalSent)
			results <- source.RecordBatchResult{Batch: record}

			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[SOCRATA] Reached limit of %d rows", opts.Limit)
				break
			}

			if len(items) < batchSize {
				break
			}

			offset += batchSize
		}

		if totalSent == 0 {
			config.Debug("[SOCRATA] No rows found for dataset %s", datasetID)
		} else {
			config.Debug("[SOCRATA] Finished reading %d rows from dataset %s", totalSent, datasetID)
		}
	}()

	return results, nil
}

func buildWhereClause(incrementalKey string, intervalStart, intervalEnd interface{}) string {
	var parts []string

	if start := formatTimestamp(intervalStart); start != "" {
		parts = append(parts, fmt.Sprintf("%s >= '%s'", incrementalKey, start))
	}
	if intervalEnd != nil {
		if end := formatTimestamp(intervalEnd); end != "" {
			parts = append(parts, fmt.Sprintf("%s < '%s'", incrementalKey, end))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}

func formatTimestamp(val interface{}) string {
	switch v := val.(type) {
	case time.Time:
		return v.Format("2006-01-02T15:04:05")
	case *time.Time:
		if v != nil {
			return v.Format("2006-01-02T15:04:05")
		}
	case string:
		if v != "" {
			return v
		}
	}
	return ""
}

var _ source.Source = (*SocrataSource)(nil)
