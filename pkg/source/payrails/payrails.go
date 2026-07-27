package payrails

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	stagingBaseURL    = "https://api.staging.payrails.io"
	productionBaseURL = "https://api.payrails.io"

	maxPageSize = 100

	rateLimit      = 8.0
	rateLimitBurst = 5

	tokenExpiryBuffer = 60 * time.Second
)

var supportedTables = map[string]bool{
	"payments":    true,
	"instruments": true,
	"executions":  true,
}

var workflowCodePattern = regexp.MustCompile(`^[a-z][-A-Za-z0-9]*$`)

type PayrailsSource struct {
	client *httpclient.Client
	cfg    *payrailsConfig

	token       string
	tokenExpiry time.Time
}

type payrailsConfig struct {
	clientID     string
	clientSecret string
	baseURL      string
	certPath     string
	keyPath      string
	certBase64   string
	keyBase64    string
}

func NewPayrailsSource() *PayrailsSource {
	return &PayrailsSource{}
}

func (s *PayrailsSource) Schemes() []string {
	return []string{"payrails"}
}

func (s *PayrailsSource) HandlesIncrementality() bool {
	return true
}

func (s *PayrailsSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.cfg = cfg

	opts := []httpclient.Option{
		httpclient.WithBaseURL(cfg.baseURL),
		httpclient.WithTimeout(60 * time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(3, 2*time.Second, 30*time.Second),
		httpclient.WithRetryCondition(func(resp *httpclient.Response, err error) bool {
			return err == nil && resp != nil && resp.StatusCode() == 429
		}),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	}
	switch {
	case cfg.certBase64 != "":
		certPEM, err := base64.StdEncoding.DecodeString(cfg.certBase64)
		if err != nil {
			return fmt.Errorf("payrails: invalid cert_base64: %w", err)
		}
		keyPEM, err := base64.StdEncoding.DecodeString(cfg.keyBase64)
		if err != nil {
			return fmt.Errorf("payrails: invalid key_base64: %w", err)
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return fmt.Errorf("payrails: invalid client certificate: %w", err)
		}
		opts = append(opts, httpclient.WithClientCertificate(cert))
	case cfg.certPath != "":
		opts = append(opts, httpclient.WithClientCert(cfg.certPath, cfg.keyPath))
	}

	s.client = httpclient.New(opts...)

	config.Debug("[PAYRAILS] Connected successfully (%s, mTLS=%t)", cfg.baseURL, cfg.certPath != "" || cfg.certBase64 != "")
	return nil
}

func (s *PayrailsSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (*payrailsConfig, error) {
	if !strings.HasPrefix(uri, "payrails://") {
		return nil, fmt.Errorf("invalid payrails URI: must start with payrails://")
	}

	rest := strings.TrimPrefix(strings.TrimPrefix(uri, "payrails://"), "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse payrails URI query: %w", err)
	}

	cfg := &payrailsConfig{
		clientID:     values.Get("client_id"),
		clientSecret: values.Get("client_secret"),
		certPath:     values.Get("cert_path"),
		keyPath:      values.Get("key_path"),
		certBase64:   values.Get("cert_base64"),
		keyBase64:    values.Get("key_base64"),
	}
	if cfg.clientID == "" {
		return nil, fmt.Errorf("client_id is required in payrails URI")
	}
	if cfg.clientSecret == "" {
		return nil, fmt.Errorf("client_secret is required in payrails URI")
	}
	if (cfg.certPath == "") != (cfg.keyPath == "") {
		return nil, fmt.Errorf("payrails mTLS requires both cert_path and key_path (or neither)")
	}
	if (cfg.certBase64 == "") != (cfg.keyBase64 == "") {
		return nil, fmt.Errorf("payrails mTLS requires both cert_base64 and key_base64 (or neither)")
	}

	if raw := values.Get("base_url"); raw != "" {
		u, perr := url.Parse(raw)
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid base_url %q in payrails URI: must be an absolute http(s) URL", raw)
		}
		cfg.baseURL = strings.TrimRight(raw, "/")
		return cfg, nil
	}

	switch values.Get("environment") {
	case "", "production":
		cfg.baseURL = productionBaseURL
	case "sandbox", "staging":
		cfg.baseURL = stagingBaseURL
	default:
		return nil, fmt.Errorf("invalid environment %q in payrails URI: must be 'production' or 'sandbox'", values.Get("environment"))
	}

	return cfg, nil
}

func (s *PayrailsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	table, workflowCodes := parseTableName(req.Name)
	if !supportedTables[table] {
		return nil, fmt.Errorf("invalid payrails table %q, supported tables are: instruments, payments, executions", table)
	}

	// Executions has no server-side date filter, so it is filtered client-side on
	// updatedAt to capture status changes; payments/instruments filter server-side
	// on createdAt.
	incrementalKey := "createdAt"
	if table == "executions" {
		incrementalKey = "updatedAt"
	}

	return &source.DynamicSourceTable{
		TableName:           table,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("payrails source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, table, workflowCodes, opts)
		},
	}, nil
}

func parseTableName(name string) (table string, workflowCodes []string) {
	parts := strings.SplitN(name, ":", 2)
	table = parts[0]
	if len(parts) == 2 {
		for _, c := range strings.Split(parts[1], ",") {
			if c = strings.TrimSpace(c); c != "" {
				workflowCodes = append(workflowCodes, c)
			}
		}
	}
	return table, workflowCodes
}

func (s *PayrailsSource) read(ctx context.Context, table string, workflowCodes []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "payments":
			err = s.readPayments(ctx, opts, results)
		case "instruments":
			err = s.readInstruments(ctx, opts, results)
		case "executions":
			err = s.readExecutions(ctx, workflowCodes, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *PayrailsSource) readPayments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PAYRAILS] reading payments")
	params := [][2]string{
		{"page[size]", strconv.Itoa(maxPageSize)},
		{"includeInstrument", "true"},
		{"includeProviderAccountDisplayName", "true"},
		{"includePaymentToken", "true"},
		{"includeHolderReference", "true"},
	}
	if f := createdAtFilter(opts.IntervalStart, opts.IntervalEnd); f != "" {
		params = append(params, [2]string{"filter[createdAt]", f})
	}
	return s.paginate(ctx, "/payment/payments", params, func(items []map[string]interface{}) error {
		return sendItems(results, items, opts)
	})
}

func (s *PayrailsSource) readInstruments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[PAYRAILS] reading instruments")
	params := [][2]string{
		{"page[size]", strconv.Itoa(maxPageSize)},
		{"includeHolderReference", "true"},
	}
	if f := createdAtFilter(opts.IntervalStart, opts.IntervalEnd); f != "" {
		params = append(params, [2]string{"filter[createdAt]", f})
	}
	return s.paginate(ctx, "/payment/instruments", params, func(items []map[string]interface{}) error {
		return sendItems(results, items, opts)
	})
}

func (s *PayrailsSource) readExecutions(ctx context.Context, workflowCodes []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(workflowCodes) == 0 {
		return fmt.Errorf("executions requires at least one workflow code: Payrails has no endpoint to list workflows, so specify them on the table, e.g. \"executions:code1,code2\"")
	}
	for _, code := range workflowCodes {
		if !workflowCodePattern.MatchString(code) {
			return fmt.Errorf("invalid workflow_code %q: must match %s", code, workflowCodePattern.String())
		}
	}

	for _, code := range workflowCodes {
		config.Debug("[PAYRAILS] reading executions for workflow %q", code)
		params := [][2]string{{"page[size]", strconv.Itoa(maxPageSize)}}
		path := "/merchant/workflows/" + code + "/executions"
		// No createdAt/updatedAt query filter exists, so the interval is applied
		// client-side on updatedAt.
		err := s.paginate(ctx, path, params, func(items []map[string]interface{}) error {
			filtered := filterItemsByInterval(items, "updatedAt", opts.IntervalStart, opts.IntervalEnd)
			for _, item := range filtered {
				item["workflow_code"] = code
			}
			return sendItems(results, filtered, opts)
		})
		if err != nil {
			return fmt.Errorf("failed to fetch executions for workflow %q: %w", code, err)
		}
	}
	return nil
}

func (s *PayrailsSource) paginate(ctx context.Context, basePath string, params [][2]string, onItems func([]map[string]interface{}) error) error {
	target := basePath
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, nextRef, err := s.getList(ctx, target, params)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			if err := onItems(items); err != nil {
				return err
			}
		}

		next := resolveNextTarget(basePath, nextRef)
		if next == "" {
			return nil
		}
		target = next
		params = nil
	}
}

func resolveNextTarget(basePath, next string) string {
	switch {
	case next == "":
		return ""
	case strings.HasPrefix(next, "http"), strings.HasPrefix(next, "/"):
		return next
	case strings.HasPrefix(next, "?"):
		return basePath + next
	default:
		return basePath + "?" + next
	}
}

// getList fetches one page and returns its items plus the next-page reference.
// payments/executions paginate via links.next, instruments via paging.next.
func (s *PayrailsSource) getList(ctx context.Context, target string, params [][2]string) (items []map[string]interface{}, next string, err error) {
	body, err := s.getRaw(ctx, target, params, false)
	if err != nil {
		return nil, "", err
	}

	var out struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
		Results []map[string]interface{} `json:"results"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, "", fmt.Errorf("failed to decode response from %s: %w", target, err)
	}

	next = out.Links.Next
	if next == "" {
		next = out.Paging.Next
	}
	return out.Results, next, nil
}

func (s *PayrailsSource) getRaw(ctx context.Context, target string, params [][2]string, retried bool) ([]byte, error) {
	token, err := s.getToken(ctx)
	if err != nil {
		return nil, err
	}

	req := s.client.R(ctx).SetHeader("Authorization", "Bearer "+token)
	for _, kv := range params {
		req = req.SetQueryParam(kv[0], kv[1])
	}

	resp, err := req.Get(target)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", target, err)
	}

	if resp.StatusCode() == 401 && !retried {
		s.token = ""
		s.tokenExpiry = time.Time{}
		return s.getRaw(ctx, target, params, true)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("request to %s failed with status %d: %s", target, resp.StatusCode(), resp.String())
	}
	return resp.Body(), nil
}

func (s *PayrailsSource) getToken(ctx context.Context) (string, error) {
	if s.token != "" && time.Now().Before(s.tokenExpiry.Add(-tokenExpiryBuffer)) {
		return s.token, nil
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	resp, err := s.client.R(ctx).
		SetHeader("x-api-key", s.cfg.clientSecret).
		SetResult(&tokenResp).
		Post("/auth/token/" + url.PathEscape(s.cfg.clientID))
	if err != nil {
		return "", fmt.Errorf("failed to request access token: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("failed to request access token: status %d: %s", resp.StatusCode(), resp.String())
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("payrails access token response did not include access_token")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	s.token = tokenResp.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return s.token, nil
}

func sendItems(results chan<- source.RecordBatchResult, items []map[string]interface{}, opts source.ReadOptions) error {
	if len(items) == 0 {
		return nil
	}
	rec, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return err
	}
	results <- source.RecordBatchResult{Batch: rec}
	return nil
}

func createdAtFilter(start, end *time.Time) string {
	switch {
	case start != nil && end != nil:
		return fmt.Sprintf("[%s,%s)", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	case start != nil:
		return fmt.Sprintf("[%s", start.UTC().Format(time.RFC3339))
	case end != nil:
		return fmt.Sprintf("%s)", end.UTC().Format(time.RFC3339))
	default:
		return ""
	}
}

func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if field == "" || (start == nil && end == nil) {
		return items
	}
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		s, ok := item[field].(string)
		if !ok || s == "" {
			filtered = append(filtered, item)
			continue
		}
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			filtered = append(filtered, item)
			continue
		}
		ts = ts.UTC()
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

var _ source.Source = (*PayrailsSource)(nil)
