package netsuite

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	maxSuiteQLPageSize     = 1000
	defaultSuiteQLPageSize = 1000
	rateLimit              = 2
	rateLimitBurst         = 2
)

type NetSuiteSource struct {
	client *Client
}

func NewNetSuiteSource() *NetSuiteSource {
	return &NetSuiteSource{}
}

func (s *NetSuiteSource) Schemes() []string {
	return []string{"netsuite"}
}

func (s *NetSuiteSource) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseURI(rawURI)
	if err != nil {
		return err
	}

	auth, closers, err := cfg.authProvider()
	if err != nil {
		return err
	}

	s.client = NewClient(cfg.baseURL, auth)
	for _, closer := range closers {
		s.client.AddCloser(closer)
	}

	config.Debug("[NETSUITE] Connected to account %s", cfg.accountID)
	return nil
}

func (s *NetSuiteSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *NetSuiteSource) HandlesIncrementality() bool {
	return false
}

func (s *NetSuiteSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if _, ok := source.IsCustomQuery(req.Name); ok {
		return source.CustomQueryTable(req, s.ExecuteCustomQuery)
	}

	tableName := strings.TrimSpace(req.Name)
	if tableName == "" {
		return nil, fmt.Errorf("table name is required for netsuite source")
	}

	primaryKeys := req.PrimaryKeys
	if len(primaryKeys) == 0 {
		primaryKeys = []string{"id"}
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("netsuite source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readTable(ctx, tableName, opts)
		},
	}, nil
}

func (s *NetSuiteSource) readTable(ctx context.Context, tableName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	query := buildSuiteQL(tableName, opts)
	return s.readSuiteQL(ctx, query, opts)
}

func (s *NetSuiteSource) ExecuteCustomQuery(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	if query == "" {
		return nil, fmt.Errorf("netsuite custom query cannot be empty")
	}
	return s.readSuiteQL(ctx, query, opts)
}

func (s *NetSuiteSource) readSuiteQL(ctx context.Context, query string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("netsuite source is not connected")
	}

	pageSize := normalizePageSize(opts.PageSize, opts.Limit)
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		config.Debug("[NETSUITE] SuiteQL query: %s", query)
		offset := 0
		totalSent := 0

		for {
			select {
			case <-ctx.Done():
				results <- source.RecordBatchResult{Err: ctx.Err()}
				return
			default:
			}

			resp, err := s.client.SuiteQL(ctx, query, pageSize, offset)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}

			items := normalizeItems(resp.Items)
			if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
				items = items[:opts.Limit-totalSent]
			}

			if len(items) > 0 {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to build arrow record for netsuite query: %w", err)}
					return
				}

				select {
				case <-ctx.Done():
					results <- source.RecordBatchResult{Err: ctx.Err()}
					return
				case results <- source.RecordBatchResult{Batch: record}:
				}

				totalSent += len(items)
			}

			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[NETSUITE] Reached limit of %d rows", opts.Limit)
				return
			}
			if !resp.HasMore || resp.Count == 0 {
				config.Debug("[NETSUITE] Finished query with %d rows", totalSent)
				return
			}

			offset += pageSize
		}
	}()

	return results, nil
}

type uriConfig struct {
	accountID     string
	baseURL       string
	accessToken   string
	clientID      string
	certificateID string
	privateKeyPEM []byte
	scopes        []string
	algorithm     string
}

func parseURI(rawURI string) (uriConfig, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return uriConfig{}, fmt.Errorf("invalid netsuite URI: %w", err)
	}
	if parsed.Scheme != "netsuite" {
		return uriConfig{}, fmt.Errorf("invalid netsuite URI: must start with netsuite://")
	}

	values := parsed.Query()
	accountID := values.Get("account_id")
	if accountID == "" {
		accountID = parsed.Host
	}
	if accountID == "" && values.Get("base_url") == "" {
		return uriConfig{}, fmt.Errorf("account_id is required in netsuite URI")
	}

	baseURL := values.Get("base_url")
	if baseURL == "" {
		baseURL = buildBaseURL(accountID)
	}
	baseURL, err = validateBaseURL(baseURL)
	if err != nil {
		return uriConfig{}, err
	}

	privateKeyPEM, err := readPrivateKey(values.Get("private_key"), values.Get("private_key_path"))
	if err != nil {
		return uriConfig{}, err
	}

	cfg := uriConfig{
		accountID:     accountID,
		baseURL:       baseURL,
		accessToken:   values.Get("access_token"),
		clientID:      values.Get("client_id"),
		certificateID: firstNonEmpty(values.Get("certificate_id"), values.Get("kid")),
		privateKeyPEM: privateKeyPEM,
		scopes:        parseScopes(values.Get("scope")),
		algorithm:     firstNonEmpty(values.Get("algorithm"), defaultJWTAlgorithm),
	}

	if cfg.accessToken == "" {
		if cfg.clientID == "" {
			return uriConfig{}, fmt.Errorf("access_token or client_id is required in netsuite URI")
		}
		if cfg.certificateID == "" {
			return uriConfig{}, fmt.Errorf("certificate_id is required for netsuite client credentials auth")
		}
		if len(cfg.privateKeyPEM) == 0 {
			return uriConfig{}, fmt.Errorf("private_key or private_key_path is required for netsuite client credentials auth")
		}
	}

	return cfg, nil
}

func (c uriConfig) authProvider() (AuthProvider, []interface{ Close() error }, error) {
	if c.accessToken != "" {
		return NewStaticTokenProvider(c.accessToken), nil, nil
	}

	provider, err := NewClientCredentialsProvider(ClientCredentialsConfig{
		TokenURL:      buildTokenURL(c.baseURL),
		ClientID:      c.clientID,
		CertificateID: c.certificateID,
		PrivateKeyPEM: c.privateKeyPEM,
		Scopes:        c.scopes,
		Algorithm:     c.algorithm,
	})
	if err != nil {
		return nil, nil, err
	}
	return provider, []interface{ Close() error }{provider}, nil
}

func buildSuiteQL(tableName string, opts source.ReadOptions) string {
	query := fmt.Sprintf("SELECT * FROM %s", strings.TrimSpace(tableName))

	var conditions []string
	if opts.IncrementalKey != "" {
		if opts.IntervalStart != nil {
			conditions = append(conditions, fmt.Sprintf("%s >= %s", opts.IncrementalKey, suiteQLTimestampLiteral(*opts.IntervalStart)))
		}
		if opts.IntervalEnd != nil {
			conditions = append(conditions, fmt.Sprintf("%s < %s", opts.IncrementalKey, suiteQLTimestampLiteral(*opts.IntervalEnd)))
		}
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
		query += " ORDER BY " + opts.IncrementalKey + " ASC"
	}

	return query
}

func suiteQLTimestampLiteral(t time.Time) string {
	return fmt.Sprintf("TO_TIMESTAMP_TZ('%s', 'YYYY-MM-DD\"T\"HH24:MI:SS.FF TZH:TZM')", t.UTC().Format("2006-01-02T15:04:05.000 -07:00"))
}

func normalizePageSize(pageSize, limit int) int {
	if pageSize <= 0 || pageSize > maxSuiteQLPageSize {
		pageSize = defaultSuiteQLPageSize
	}
	if limit > 0 && limit < pageSize {
		return limit
	}
	return pageSize
}

func normalizeItems(items []map[string]interface{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row := make(map[string]interface{}, len(item))
		for key, value := range item {
			if strings.EqualFold(key, "links") {
				continue
			}
			row[key] = value
		}
		rows = append(rows, row)
	}
	return rows
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ source.Source = (*NetSuiteSource)(nil)
