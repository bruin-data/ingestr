package quickbooks

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
	// QuickBooks Online API: 500 requests/min per realm (company).
	// Using ~6.67 req/s (~400/min) to stay safely under the limit.
	rateLimit      = 6.67
	rateLimitBurst = 5
	// QuickBooks Query API supports MAXRESULTS up to 1000.
	maxPageSize = 1000

	sandboxBaseURL    = "https://sandbox-quickbooks.api.intuit.com"
	productionBaseURL = "https://quickbooks.api.intuit.com"
	tokenURL          = "https://oauth.platform.intuit.com/oauth2/v1/tokens/bearer"
)

var supportedTables = []string{
	"customers",
	"invoices",
	"accounts",
	"vendors",
	"payments",
}

// tableMapping maps plural table names to singular API object names used in QuickBooks queries.
var tableMapping = map[string]string{
	"customers": "Customer",
	"invoices":  "Invoice",
	"accounts":  "Account",
	"vendors":   "Vendor",
	"payments":  "Payment",
}

type QuickBooksSource struct {
	client       *gonghttp.Client
	companyID    string
	minorVersion string
}

func NewQuickBooksSource() *QuickBooksSource {
	return &QuickBooksSource{}
}

func (s *QuickBooksSource) HandlesIncrementality() bool {
	return true
}

func (s *QuickBooksSource) Schemes() []string {
	return []string{"quickbooks"}
}

func (s *QuickBooksSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	baseURL := productionBaseURL
	if creds.environment == "sandbox" {
		baseURL = sandboxBaseURL
	}

	accessToken := creds.accessToken
	if accessToken == "" && creds.refreshToken != "" {
		token, err := refreshAccessToken(ctx, creds.clientID, creds.clientSecret, creds.refreshToken)
		if err != nil {
			return fmt.Errorf("failed to refresh access token: %w", err)
		}
		accessToken = token
	}

	if accessToken == "" {
		return fmt.Errorf("either access_token or refresh_token must be provided")
	}

	s.companyID = creds.companyID
	s.minorVersion = creds.minorVersion

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(accessToken)),
		gonghttp.WithHeader("Accept", "application/json"),
	)

	config.Debug("[QUICKBOOKS] Connected to company: %s (env: %s)", creds.companyID, creds.environment)
	return nil
}

func (s *QuickBooksSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *QuickBooksSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "lastupdatedtime",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("quickbooks source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *QuickBooksSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "customers":
			err = s.readCustomers(ctx, opts, results)
		case "invoices":
			err = s.readInvoices(ctx, opts, results)
		case "accounts":
			err = s.readAccounts(ctx, opts, results)
		case "vendors":
			err = s.readVendors(ctx, opts, results)
		case "payments":
			err = s.readPayments(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

type quickbooksCredentials struct {
	companyID    string
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	environment  string
	minorVersion string
}

func parseURI(uri string) (quickbooksCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return quickbooksCredentials{}, fmt.Errorf("invalid quickbooks URI: %w", err)
	}

	if parsed.Scheme != "quickbooks" {
		return quickbooksCredentials{}, fmt.Errorf("invalid quickbooks URI: must start with quickbooks://")
	}

	q := parsed.Query()

	companyID := q.Get("company_id")
	if companyID == "" {
		return quickbooksCredentials{}, fmt.Errorf("company_id is required in quickbooks URI")
	}

	clientID := q.Get("client_id")
	if clientID == "" {
		return quickbooksCredentials{}, fmt.Errorf("client_id is required in quickbooks URI")
	}

	clientSecret := q.Get("client_secret")
	if clientSecret == "" {
		return quickbooksCredentials{}, fmt.Errorf("client_secret is required in quickbooks URI")
	}

	refreshToken := q.Get("refresh_token")
	accessToken := q.Get("access_token")

	if refreshToken == "" && accessToken == "" {
		return quickbooksCredentials{}, fmt.Errorf("either refresh_token or access_token is required in quickbooks URI")
	}

	env := q.Get("environment")
	if env == "" {
		env = "production"
	}

	if env != "production" && env != "sandbox" {
		return quickbooksCredentials{}, fmt.Errorf("environment must be 'production' or 'sandbox', got %q", env)
	}

	return quickbooksCredentials{
		companyID:    companyID,
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
		accessToken:  accessToken,
		environment:  env,
		minorVersion: q.Get("minor_version"),
	}, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// refreshAccessToken exchanges a refresh token for a new access token.
func refreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error) {
	client := gonghttp.New(
		gonghttp.WithTimeout(30*time.Second),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(clientID, clientSecret)),
	)
	defer func() { _ = client.Close() }()

	resp, err := client.R(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json").
		SetBody(fmt.Sprintf("grant_type=refresh_token&refresh_token=%s", url.QueryEscape(refreshToken))).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}

	if !resp.IsSuccess() {
		return "", fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(resp.Body(), &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok || accessToken == "" {
		return "", fmt.Errorf("no access_token in token response")
	}

	config.Debug("[QUICKBOOKS] Successfully refreshed access token")
	return accessToken, nil
}

// buildQuery constructs a QuickBooks Query API query string.
// The query uses SQL-like syntax: SELECT * FROM <Object> WHERE <conditions> ORDERBY <field> ASC STARTPOSITION <pos> MAXRESULTS <max>
func buildQuery(objectName string, startPos int, opts source.ReadOptions) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SELECT * FROM %s", objectName)

	var conditions []string
	if opts.IntervalStart != nil {
		conditions = append(conditions, fmt.Sprintf("MetaData.LastUpdatedTime >= '%s'", opts.IntervalStart.UTC().Format("2006-01-02T15:04:05-07:00")))
	}
	if opts.IntervalEnd != nil {
		conditions = append(conditions, fmt.Sprintf("MetaData.LastUpdatedTime < '%s'", opts.IntervalEnd.UTC().Format("2006-01-02T15:04:05-07:00")))
	}

	if len(conditions) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	sb.WriteString(" ORDERBY MetaData.LastUpdatedTime ASC")
	fmt.Fprintf(&sb, " STARTPOSITION %d MAXRESULTS %d", startPos, maxPageSize)

	return sb.String()
}

// normalizeItem extracts MetaData.LastUpdatedTime as a top-level "lastupdatedtime" field
// and renames "Id" to "id" to match ingestr conventions.
func normalizeItem(item map[string]any) map[string]any {
	// Extract lastupdatedtime from MetaData
	if meta, ok := item["MetaData"].(map[string]any); ok {
		if lut, ok := meta["LastUpdatedTime"]; ok {
			item["lastupdatedtime"] = lut
		}
	}

	// Rename Id to id
	if id, ok := item["Id"]; ok {
		item["id"] = id
		delete(item, "Id")
	}

	return item
}

// paginateAndSend queries the QuickBooks API with offset-based pagination and streams results.
func (s *QuickBooksSource) paginateAndSend(ctx context.Context, tableName, objectName string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startPos := 1
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		query := buildQuery(objectName, startPos, opts)
		config.Debug("[QUICKBOOKS] query: %s", query)

		endpoint := fmt.Sprintf("/v3/company/%s/query", s.companyID)
		req := s.client.R(ctx).SetQueryParam("query", query)
		if s.minorVersion != "" {
			req = req.SetQueryParam("minorversion", s.minorVersion)
		}
		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("quickbooks %s returned status %d: %s", tableName, resp.StatusCode(), resp.String())
		}

		var result map[string]any
		if err := jsonUseNumber(resp.Body(), &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", tableName, err)
		}

		qr, ok := result["QueryResponse"].(map[string]any)
		if !ok {
			break
		}

		rawItems, ok := qr[objectName].([]any)
		if !ok || len(rawItems) == 0 {
			break
		}

		rows := make([]map[string]any, 0, len(rawItems))
		for _, raw := range rawItems {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, normalizeItem(item))
		}

		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for %s: %w", tableName, err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(rows)
		}

		if len(rawItems) < maxPageSize {
			break
		}

		startPos += maxPageSize
	}

	config.Debug("[QUICKBOOKS] finished reading %s: %d total records", tableName, totalProcessed)
	return nil
}

func (s *QuickBooksSource) readCustomers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[QUICKBOOKS] reading customers")
	return s.paginateAndSend(ctx, "customers", tableMapping["customers"], opts, results)
}

func (s *QuickBooksSource) readInvoices(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[QUICKBOOKS] reading invoices")
	return s.paginateAndSend(ctx, "invoices", tableMapping["invoices"], opts, results)
}

func (s *QuickBooksSource) readAccounts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[QUICKBOOKS] reading accounts")
	return s.paginateAndSend(ctx, "accounts", tableMapping["accounts"], opts, results)
}

func (s *QuickBooksSource) readVendors(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[QUICKBOOKS] reading vendors")
	return s.paginateAndSend(ctx, "vendors", tableMapping["vendors"], opts, results)
}

func (s *QuickBooksSource) readPayments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[QUICKBOOKS] reading payments")
	return s.paginateAndSend(ctx, "payments", tableMapping["payments"], opts, results)
}
