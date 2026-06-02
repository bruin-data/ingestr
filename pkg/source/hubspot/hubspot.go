package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"resty.dev/v3"
)

const (
	baseURL = "https://api.hubapi.com/"

	// HubSpot CRM endpoints allow ~10 req/s per auth token on free/trial tiers.
	crmRateLimit      = 9.0
	crmRateLimitBurst = 5
	crmMinRate        = 1.0

	// HubSpot CRM Search APIs (crm/v3/objects/{type}/search) have a separate
	searchRateLimit      = 4.0
	searchRateLimitBurst = 1
	searchMinRate        = 0.5

	retryCount   = 15
	retryWait    = 1 * time.Second
	retryMaxWait = 5 * time.Minute
)

// hubspotRetryStrategy honors the standard Retry-After HTTP header or the
// `retry_after` field HubSpot/Cloudflare returns in JSON error bodies, falling
// back to exponential backoff with equal jitter. Limiter adjustment is handled
// by the response middleware (see attachAdaptiveHooks), not here, so that 429s
// are observed even when no retry is scheduled.
func hubspotRetryStrategy(resp *httpclient.Response, _ error) (time.Duration, error) {
	if resp != nil {
		if v := resp.Header().Get("Retry-After"); v != "" {
			if secs, parseErr := strconv.Atoi(v); parseErr == nil && secs > 0 {
				return jitter(time.Duration(secs) * time.Second), nil
			}
		}
		var body struct {
			RetryAfter int `json:"retry_after"`
		}
		if jsonErr := json.Unmarshal(resp.Body(), &body); jsonErr == nil && body.RetryAfter > 0 {
			return jitter(time.Duration(body.RetryAfter) * time.Second), nil
		}
	}

	attempt := 1
	if resp != nil {
		if a := resp.Attempt(); a > 0 {
			attempt = a
		}
	}
	delay := time.Duration(math.Min(
		float64(retryMaxWait),
		float64(retryWait)*math.Exp2(float64(attempt)),
	))
	return jitter(delay), nil
}

// jitter applies equal-jitter (delay/2 fixed + delay/2 random) to break
// thundering-herd synchronization when many workers retry simultaneously.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(mathrand.Int64N(int64(half)+1))
}

type Hubspotsource struct {
	client         *httpclient.Client
	searchClient   *httpclient.Client
	crmAdaptive    *adaptiveLimiter
	searchAdaptive *adaptiveLimiter
	apiKey         string
}

func NewHubSpotSource() *Hubspotsource {
	return &Hubspotsource{}
}

func (s *Hubspotsource) Schemes() []string {
	return []string{"hubspot"}
}

func (s *Hubspotsource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseHubspotURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	crmLimiter := httpclient.NewRateLimiter(crmRateLimit, crmRateLimitBurst)
	s.crmAdaptive = newAdaptiveLimiter(crmLimiter, crmMinRate, crmRateLimit)

	searchLimiter := httpclient.NewRateLimiter(searchRateLimit, searchRateLimitBurst)
	s.searchAdaptive = newAdaptiveLimiter(searchLimiter, searchMinRate, searchRateLimit)

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(5*time.Minute),
		httpclient.WithRateLimiterInstance(crmLimiter),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		// HubSpot batch endpoints (associations, batch/read) are POST but
		// semantically read-only; without this resty treats them as
		// non-idempotent and skips retries on 429s.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithRetryStrategy(hubspotRetryStrategy),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	attachAdaptiveHooks(s.client, s.crmAdaptive)

	s.searchClient = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(5*time.Minute),
		httpclient.WithRateLimiterInstance(searchLimiter),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		// HubSpot search endpoints are POST but semantically read-only; without
		// this resty treats them as non-idempotent and skips retries entirely.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithRetryStrategy(hubspotRetryStrategy),
		httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	attachAdaptiveHooks(s.searchClient, s.searchAdaptive)

	config.Debug("[HUBSPOT] Connected successfully")
	return nil
}

// attachAdaptiveHooks drives the adaptive limiter from every response, not just
// retried ones: a 429 shrinks the rate (even on the final attempt or when no
// retry is scheduled), and a 2xx counts toward growing it back.
func attachAdaptiveHooks(client *httpclient.Client, adaptive *adaptiveLimiter) {
	client.Resty().AddResponseMiddleware(func(_ *resty.Client, resp *resty.Response) error {
		switch {
		case resp.StatusCode() == http.StatusTooManyRequests:
			adaptive.onThrottle()
		case resp.StatusCode() >= 200 && resp.StatusCode() < 300:
			adaptive.onSuccess()
		}
		return nil
	})
}

func parseHubspotURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "hubspot://") {
		return "", fmt.Errorf("invalid hubspot URI: must start with hubspot://")
	}

	rest := strings.TrimPrefix(uri, "hubspot://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in hubspot URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse hubspot URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in hubspot URI")
	}

	return apiKey, nil
}

func (s *Hubspotsource) Close(ctx context.Context) error {
	var firstErr error
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			firstErr = err
		}
	}
	if s.searchClient != nil {
		if err := s.searchClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Hubspotsource) HandlesIncrementality() bool {
	return true
}

type tableConfig struct {
	ObjectType     string
	Associations   []string
	IncrementalKey string
	DefaultProps   []string
	PrimaryKey     []string
	Strategy       config.IncrementalStrategy
}

var tables = map[string]tableConfig{
	"contacts": {
		ObjectType:     "contacts",
		Associations:   []string{"companies", "deals", "products", "tickets", "quotes"},
		IncrementalKey: "lastmodifieddate",
		DefaultProps:   []string{"createdate", "email", "firstname", "hs_object_id", "lastmodifieddate", "lastname"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"companies": {
		ObjectType:     "companies",
		Associations:   []string{"products"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"createdate", "domain", "hs_lastmodifieddate", "hs_object_id", "name"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"deals": {
		ObjectType:     "deals",
		Associations:   []string{"companies", "contacts", "products", "tickets", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"amount", "closedate", "createdate", "dealname", "dealstage", "hs_lastmodifieddate", "hs_object_id", "pipeline"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"tickets": {
		ObjectType:     "tickets",
		Associations:   []string{"companies", "contacts", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"createdate", "content", "hs_lastmodifieddate", "hs_object_id", "hs_pipeline", "hs_pipeline_stage", "hs_ticket_category", "hs_ticket_priority", "subject"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"products": {
		ObjectType:     "products",
		Associations:   []string{"companies", "contacts", "deals", "tickets", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"createdate", "description", "hs_lastmodifieddate", "hs_object_id", "name", "price"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"quotes": {
		ObjectType:     "quotes",
		Associations:   []string{"companies", "contacts", "deals", "products", "tickets"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_createdate", "hs_expiration_date", "hs_lastmodifieddate", "hs_object_id", "hs_public_url_key", "hs_status", "hs_title"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"calls": {
		ObjectType:     "calls",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_call_body", "hs_call_direction", "hs_call_disposition", "hs_call_duration", "hs_call_from_number", "hs_call_status", "hs_call_title", "hs_call_to_number", "hs_lastmodifieddate", "hs_timestamp"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"emails": {
		ObjectType:     "emails",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_attachment_ids", "hs_email_direction", "hs_email_headers", "hs_email_html", "hs_email_status", "hs_email_subject", "hs_email_text", "hs_timestamp", "hs_lastmodifieddate", "hubspot_owner_id"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"feedback_submissions": {
		ObjectType:     "feedback_submissions",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_createdate", "hs_lastmodifieddate", "hs_object_id", "hs_sentiment", "hs_survey_channel"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"line_items": {
		ObjectType:     "line_items",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"amount", "description", "hs_line_item_currency_code", "hs_recurring_billing_end_date", "hs_recurring_billing_start_date", "hs_lastmodifieddate", "hs_sku", "name", "price", "quantity", "recurringbillingfrequency"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"meetings": {
		ObjectType:     "meetings",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_internal_meeting_notes", "hs_meeting_body", "hs_meeting_end_time", "hs_meeting_external_url", "hs_meeting_location", "hs_meeting_outcome", "hs_meeting_start_time", "hs_meeting_title", "hs_timestamp", "hs_lastmodifieddate", "hubspot_owner_id"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"notes": {
		ObjectType:     "notes",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_attachment_ids", "hs_note_body", "hs_timestamp", "hs_lastmodifieddate", "hubspot_owner_id"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"tasks": {
		ObjectType:     "tasks",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_task_body", "hs_task_priority", "hs_task_status", "hs_task_subject", "hs_task_type", "hs_timestamp", "hs_lastmodifieddate", "hubspot_owner_id"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"carts": {
		ObjectType:     "carts",
		Associations:   []string{"contacts", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_cart_discount", "hs_cart_name", "hs_cart_url", "hs_createdate", "hs_currency_code", "hs_external_cart_id", "hs_external_status", "hs_lastmodifieddate", "hs_object_id", "hs_shipping_cost", "hs_source_store", "hs_tags", "hs_tax", "hs_total_price"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"discounts": {
		ObjectType:     "discounts",
		Associations:   []string{"contacts", "line_items", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_duration", "hs_label", "hs_lastmodifieddate", "hs_sort_order", "hs_type", "hs_value"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"fees": {
		ObjectType:     "fees",
		Associations:   []string{"contacts", "line_items", "companies", "deals", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_label", "hs_lastmodifieddate", "hs_type", "hs_value"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"invoices": {
		ObjectType:     "invoices",
		Associations:   []string{"contacts", "line_items", "companies", "fees", "products", "quotes"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_currency", "hs_due_date", "hs_invoice_date", "hs_lastmodifieddate", "hs_tax_id"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"commerce_payments": {
		ObjectType:     "commerce_payments",
		Associations:   []string{"contacts", "companies", "deals", "quotes", "invoices", "products", "fees"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_currency_code", "hs_customer_email", "hs_fees_amount", "hs_initial_amount", "hs_initiated_date", "hs_internal_comment", "hs_lastmodifieddate", "hs_latest_status", "hs_payment_method_type", "hs_payout_date", "hs_processor_type", "hs_reference_number", "hs_refunds_amount", "hs_billing_address_city", "hs_billing_address_country"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"taxes": {
		ObjectType:     "taxes",
		Associations:   []string{"line_items", "companies", "deals", "products", "quotes", "fees"},
		IncrementalKey: "hs_lastmodifieddate",
		DefaultProps:   []string{"hs_label", "hs_lastmodifieddate", "hs_type", "hs_value"},
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	},
	"owners": {
		ObjectType: "owners",
		PrimaryKey: []string{"id"},
		Strategy:   config.StrategyMerge,
	},
	"schemas": {
		ObjectType: "schemas",
		PrimaryKey: []string{"id"},
		Strategy:   config.StrategyMerge,
	},
	"pipelines": {
		ObjectType: "pipelines",
		PrimaryKey: []string{"object_type", "pipeline_id"},
		Strategy:   config.StrategyReplace,
	},
	"pipeline_stages": {
		ObjectType: "pipeline_stages",
		PrimaryKey: []string{"object_type", "pipeline_id", "stage_id"},
		Strategy:   config.StrategyReplace,
	},
}

const historyPrefix = "property_history:"

var historyExcluded = map[string]struct{}{
	"owners":          {},
	"schemas":         {},
	"pipelines":       {},
	"pipeline_stages": {},
}

// For history tables, create a new entry in the tables map with the same config but no associations or default props, and a different incremental key
func init() {
	baseNames := make([]string, 0, len(tables))
	for name := range tables {
		baseNames = append(baseNames, name)
	}
	for _, name := range baseNames {
		if _, skip := historyExcluded[name]; skip {
			continue
		}
		base := tables[name]
		base.Associations = nil
		base.DefaultProps = nil
		tables[historyPrefix+name] = base
	}
}

func isHistoryTable(name string) bool {
	if !strings.HasPrefix(name, historyPrefix) {
		return false
	}
	_, ok := tables[name]
	return ok
}

// parseHistoryTableName splits an optional ":prop1,prop2,..." allow-list
// suffix off a property_history:* table name. Returns the base name (without
// the suffix) and the parsed property list (nil when no suffix is present).
// Non-history names are returned unchanged with a nil filter.
func parseHistoryTableName(name string) (string, []string) {
	if !strings.HasPrefix(name, historyPrefix) {
		return name, nil
	}

	prefix := historyPrefix
	if strings.HasPrefix(name, historyPrefix+"custom:") {
		prefix = historyPrefix + "custom:"
	}

	rest := strings.TrimPrefix(name, prefix)
	parts := strings.SplitN(rest, ":", 2)
	objectName := parts[0]
	if len(parts) < 2 {
		return name, nil
	}

	raw := strings.Split(parts[1], ",")
	props := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			props = append(props, p)
		}
	}
	if len(props) == 0 {
		return prefix + objectName, nil
	}
	return prefix + objectName, props
}

func parseTableAssocOverride(name string) (string, []string, bool) {
	idx := strings.Index(name, ":")
	if idx < 0 {
		return name, nil, false
	}
	base := name[:idx]
	rest := name[idx+1:]
	out := make([]string, 0)
	for _, a := range strings.Split(rest, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return base, out, true
}

func supportedTableNames() string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func (s *Hubspotsource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	base, historyProps := parseHistoryTableName(tableName)

	isCustom := strings.HasPrefix(base, "custom:")
	isCustomHistory := strings.HasPrefix(base, historyPrefix+"custom:")
	isHistory := strings.HasPrefix(base, historyPrefix)

	var assocOverride []string
	if !isCustom && !isCustomHistory && !isHistory {
		if newBase, override, ok := parseTableAssocOverride(base); ok {
			base = newBase
			assocOverride = override
		}
	}

	cfg, ok := tables[base]
	if !ok && !isCustom && !isCustomHistory {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s, or use 'custom:<objectType>' / 'property_history:custom:<objectType>' for custom objects)", tableName, supportedTableNames())
	}

	if !ok {
		objectName := base
		if isCustomHistory {
			objectName = strings.TrimPrefix(base, historyPrefix+"custom:")
		} else if isCustom {
			objectName = strings.TrimPrefix(base, "custom:")
		}
		objectName = strings.SplitN(objectName, ":", 2)[0]

		cfg = tableConfig{
			PrimaryKey:     []string{"hs_object_id"},
			IncrementalKey: incrementalKeyForObject(objectName),
			Strategy:       config.StrategyMerge,
		}
	}

	primaryKeys := cfg.PrimaryKey
	incrementalKey := cfg.IncrementalKey
	if isHistoryTable(base) || isCustomHistory {
		primaryKeys = []string{"hs_object_id", "property_name", "timestamp"}
		incrementalKey = "timestamp"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       cfg.Strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("hubspot source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, base, historyProps, assocOverride, opts)
		},
	}, nil
}

func (s *Hubspotsource) read(ctx context.Context, table string, historyProps []string, assocOverride []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "owners":
			err = s.readOwners(ctx, opts, results)
		case "schemas":
			err = s.readSchemas(ctx, opts, results)
		case "pipelines":
			err = s.readPipelines(ctx, opts, results)
		case "pipeline_stages":
			err = s.readPipelineStages(ctx, opts, results)
		default:
			if _, ok := tables[table]; ok {
				err = s.readCRMObjects(ctx, table, historyProps, assocOverride, opts, results)
			} else if strings.HasPrefix(table, historyPrefix+"custom:") {
				err = s.readCustomObjectHistory(ctx, table, historyProps, opts, results)
			} else if strings.HasPrefix(table, "custom:") {
				err = s.readCustomObject(ctx, table, opts, results)
			} else {
				err = fmt.Errorf("unsupported table: %s", table)
			}
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

const (
	defaultPageLimit = 200
	getPageLimit     = 100

	archivedAtColumn = "_archived_at"
)

type crmPaging struct {
	Next *struct {
		After string `json:"after"`
		Link  string `json:"link"`
	} `json:"next"`
}

type crmListResponse struct {
	Total   int                      `json:"total"`
	Results []map[string]interface{} `json:"results"`
	Paging  *crmPaging               `json:"paging"`
}

var pipelineObjectTypes = []string{
	"deals", "tickets", "appointments", "courses", "listings",
	"orders", "services", "leads",
}

type rawPipelineStage struct {
	ID               string                 `json:"id"`
	Label            string                 `json:"label"`
	DisplayOrder     int                    `json:"displayOrder"`
	Archived         bool                   `json:"archived"`
	CreatedAt        string                 `json:"createdAt"`
	UpdatedAt        string                 `json:"updatedAt"`
	Metadata         map[string]interface{} `json:"metadata"`
	WritePermissions string                 `json:"writePermissions"`
}

type rawPipeline struct {
	ID               string             `json:"id"`
	Label            string             `json:"label"`
	DisplayOrder     int                `json:"displayOrder"`
	Archived         bool               `json:"archived"`
	CreatedAt        string             `json:"createdAt"`
	UpdatedAt        string             `json:"updatedAt"`
	Stages           []rawPipelineStage `json:"stages"`
	WritePermissions string             `json:"writePermissions"`
}

type objectPipelines struct {
	objectType string
	pipelines  []rawPipeline
}

func (s *Hubspotsource) fetchAllObjectPipelines(ctx context.Context) ([]objectPipelines, []string, error) {
	var out []objectPipelines
	var scopeSkipped []string

	for _, objectType := range pipelineObjectTypes {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		endpoint := fmt.Sprintf("crm/v3/pipelines/%s", objectType)
		var resp struct {
			Results []rawPipeline `json:"results"`
		}
		httpResp, err := s.client.R(ctx).SetResult(&resp).Get(endpoint)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			config.Debug("[HUBSPOT] skipping pipelines for %s: %v", objectType, err)
			continue
		}
		if httpResp.StatusCode() == 404 {
			config.Debug("[HUBSPOT] %s has no pipelines configured", objectType)
			continue
		}
		if httpResp.StatusCode() == 403 {
			scopeSkipped = append(scopeSkipped, objectType)
			continue
		}
		if !httpResp.IsSuccess() {
			return nil, nil, fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, httpResp.StatusCode(), httpResp.String())
		}

		out = append(out, objectPipelines{objectType: objectType, pipelines: resp.Results})
	}

	return out, scopeSkipped, nil
}

func (s *Hubspotsource) readPipelines(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HUBSPOT] reading pipelines")

	objs, scopeSkipped, err := s.fetchAllObjectPipelines(ctx)
	if err != nil {
		return err
	}

	var rows []map[string]interface{}
	for _, op := range objs {
		for _, p := range op.pipelines {
			rows = append(rows, map[string]interface{}{
				"object_type":       op.objectType,
				"pipeline_id":       p.ID,
				"pipeline_name":     p.Label,
				"display_order":     p.DisplayOrder,
				"is_archived":       p.Archived,
				"created_at":        p.CreatedAt,
				"updated_at":        p.UpdatedAt,
				"write_permissions": p.WritePermissions,
				"stage_count":       len(p.Stages),
			})
		}
	}

	if len(scopeSkipped) > 0 {
		fmt.Printf("\n[HUBSPOT] pipelines skipped (insufficient scopes): %s\n", strings.Join(scopeSkipped, ", "))
	}

	if len(rows) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *Hubspotsource) readPipelineStages(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HUBSPOT] reading pipeline_stages")

	objs, scopeSkipped, err := s.fetchAllObjectPipelines(ctx)
	if err != nil {
		return err
	}

	var rows []map[string]interface{}
	for _, op := range objs {
		for _, p := range op.pipelines {
			for _, st := range p.Stages {
				rows = append(rows, map[string]interface{}{
					"object_type":       op.objectType,
					"pipeline_id":       p.ID,
					"stage_id":          st.ID,
					"stage_name":        st.Label,
					"display_order":     st.DisplayOrder,
					"is_archived":       st.Archived,
					"metadata":          st.Metadata,
					"created_at":        st.CreatedAt,
					"updated_at":        st.UpdatedAt,
					"write_permissions": st.WritePermissions,
				})
			}
		}
	}

	if len(scopeSkipped) > 0 {
		fmt.Printf("\n[HUBSPOT] pipeline_stages skipped (insufficient scopes): %s\n", strings.Join(scopeSkipped, ", "))
	}

	if len(rows) == 0 {
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to build arrow record: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *Hubspotsource) fetchPropertyNames(ctx context.Context, tableName string) ([]string, error) {
	endpoint := fmt.Sprintf("crm/v3/properties/%s", tableName)
	config.Debug("[HUBSPOT] Fetching properties for %s", tableName)

	var result struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}

	resp, err := s.client.R(ctx).SetResult(&result).Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch properties for %s: %w", tableName, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	props := make([]string, 0, len(result.Results))
	for _, p := range result.Results {
		props = append(props, p.Name)
	}

	config.Debug("[HUBSPOT] Found %d properties for %s", len(props), tableName)
	return props, nil
}

func (s *Hubspotsource) resolveProperties(ctx context.Context, tableName string, defaultProps []string) []string {
	allProps, err := s.fetchPropertyNames(ctx, tableName)
	if err != nil {
		config.Debug("[HUBSPOT] Failed to fetch properties for %s, using defaults: %v", tableName, err)
		return defaultProps
	}

	seen := make(map[string]struct{}, len(defaultProps)+len(allProps))
	merged := make([]string, 0, len(defaultProps)+len(allProps))

	for _, p := range defaultProps {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			merged = append(merged, p)
		}
	}

	if _, ok := seen["hs_object_id"]; !ok {
		seen["hs_object_id"] = struct{}{}
		merged = append(merged, "hs_object_id")
	}

	for _, p := range allProps {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			merged = append(merged, p)
		}
	}

	return merged
}

func (s *Hubspotsource) paginatedFetch(ctx context.Context, endpoint string, properties []string, associations []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cursor := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", fmt.Sprintf("%d", getPageLimit))

		if len(properties) > 0 {
			req.SetQueryParam("properties", strings.Join(properties, ","))
		}
		if len(associations) > 0 {
			req.SetQueryParam("associations", strings.Join(associations, ","))
		}
		if cursor != "" {
			req.SetQueryParam("after", cursor)
		}

		var listResp crmListResponse
		resp, err := req.SetResult(&listResp).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		if len(listResp.Results) > 0 {
			items := make([]map[string]interface{}, 0, len(listResp.Results))
			for _, item := range listResp.Results {
				items = append(items, flattenCRMResult(item))
			}

			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalProcessed += len(items)
		}

		if listResp.Paging == nil || listResp.Paging.Next == nil {
			break
		}
		cursor = listResp.Paging.Next.After
	}

	config.Debug("[HUBSPOT] Finished reading %s: %d total records", endpoint, totalProcessed)
	return nil
}

func flattenCRMResult(result map[string]interface{}) map[string]interface{} {
	obj, ok := result["properties"].(map[string]interface{})
	if !ok {
		obj = make(map[string]interface{})
		for k, v := range result {
			obj[k] = v
		}
	}

	if _, hasID := obj["id"]; !hasID {
		if id, ok := result["id"]; ok {
			obj["id"] = id
		}
	}

	if assocs, ok := result["associations"].(map[string]interface{}); ok {
		hsObjectID := obj["hs_object_id"]

		for assocType, assocData := range assocs {
			assocMap, ok := assocData.(map[string]interface{})
			if !ok {
				continue
			}
			items, ok := assocMap["results"].([]interface{})
			if !ok {
				continue
			}

			seen := make(map[string]struct{})
			var values []map[string]interface{}
			for _, item := range items {
				entry, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				assocID := fmt.Sprintf("%v", entry["id"])
				key := fmt.Sprintf("%v_%s", hsObjectID, assocID)
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				values = append(values, map[string]interface{}{
					"value":           hsObjectID,
					assocType + "_id": assocID,
				})
			}

			obj[assocType] = values
		}
	}

	return obj
}

func (s *Hubspotsource) readOwners(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginatedFetch(ctx, "crm/v3/owners", nil, nil, opts, results)
}

func (s *Hubspotsource) readSchemas(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginatedFetch(ctx, "crm/v3/schemas", nil, nil, opts, results)
}

func (s *Hubspotsource) resolveCustomObject(ctx context.Context, raw string) (objectTypeID string, objectName string, associations []string, err error) {
	if raw == "" {
		return "", "", nil, fmt.Errorf("invalid custom table format: object type cannot be empty")
	}

	parts := strings.SplitN(raw, ":", 2)
	objectName = parts[0]
	if objectName == "" {
		return "", "", nil, fmt.Errorf("invalid custom table format: object type cannot be empty")
	}

	if len(parts) == 2 && parts[1] != "" {
		associations = strings.Split(parts[1], ",")
	}

	var schemasResp struct {
		Results []struct {
			Name         string `json:"name"`
			ObjectTypeId string `json:"objectTypeId"`
			Labels       struct {
				Plural string `json:"plural"`
			} `json:"labels"`
		} `json:"results"`
	}

	resp, err := s.client.R(ctx).SetResult(&schemasResp).Get("crm/v3/schemas")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to fetch schemas: %w", err)
	}
	if !resp.IsSuccess() {
		return "", "", nil, fmt.Errorf("hubspot API crm/v3/schemas returned status %d: %s", resp.StatusCode(), resp.String())
	}

	nameLower := strings.ToLower(objectName)
	for _, cs := range schemasResp.Results {
		if strings.ToLower(cs.Name) == nameLower || strings.ToLower(cs.Labels.Plural) == nameLower {
			objectTypeID = cs.ObjectTypeId
			break
		}
	}
	if objectTypeID == "" {
		return "", "", nil, fmt.Errorf("no custom object found matching %q", objectName)
	}

	return objectTypeID, objectName, associations, nil
}

func (s *Hubspotsource) readCustomObject(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	raw := strings.TrimPrefix(table, "custom:")
	objectTypeID, objectName, associations, err := s.resolveCustomObject(ctx, raw)
	if err != nil {
		return err
	}

	properties, err := s.fetchPropertyNames(ctx, objectTypeID)
	if err != nil {
		return fmt.Errorf("failed to fetch properties for custom object %s: %w", objectName, err)
	}

	cfg := tableConfig{
		ObjectType:     objectTypeID,
		Associations:   associations,
		IncrementalKey: incrementalKeyForObject(objectName),
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	}

	startMs := timeToMs(opts.IntervalStart)
	if startMs == "" {
		startMs = fmt.Sprintf("%d", epochStart.UnixMilli())
	}

	return s.searchCRMObjects(ctx, cfg, properties, startMs, opts, results)
}

func (s *Hubspotsource) readCustomObjectHistory(ctx context.Context, table string, historyProps []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	raw := strings.TrimPrefix(table, historyPrefix+"custom:")
	objectTypeID, objectName, _, err := s.resolveCustomObject(ctx, raw)
	if err != nil {
		return err
	}

	var properties []string
	if len(historyProps) > 0 {
		properties = historyProps
	} else {
		properties, err = s.fetchPropertyNames(ctx, objectTypeID)
		if err != nil {
			return fmt.Errorf("failed to fetch properties for custom object %s: %w", objectName, err)
		}
	}

	cfg := tableConfig{
		ObjectType:     objectTypeID,
		IncrementalKey: incrementalKeyForObject(objectName),
		PrimaryKey:     []string{"hs_object_id"},
		Strategy:       config.StrategyMerge,
	}

	startMs := timeToMs(opts.IntervalStart)
	if startMs == "" {
		startMs = fmt.Sprintf("%d", epochStart.UnixMilli())
	}

	return s.searchCRMObjectsHistory(ctx, cfg, properties, startMs, opts, results)
}

func incrementalKeyForObject(objectName string) string {
	if strings.ToLower(objectName) == "contacts" {
		return "lastmodifieddate"
	}
	return "hs_lastmodifieddate"
}

var epochStart = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *Hubspotsource) readCRMObjects(ctx context.Context, tableName string, historyProps []string, assocOverride []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	cfg := tables[tableName]
	if assocOverride != nil {
		cfg.Associations = assocOverride
	}

	var properties []string
	if isHistoryTable(tableName) && len(historyProps) > 0 {
		properties = historyProps
	} else {
		properties = s.resolveProperties(ctx, cfg.ObjectType, cfg.DefaultProps)
	}

	startMs := timeToMs(opts.IntervalStart)
	if startMs == "" {
		startMs = fmt.Sprintf("%d", epochStart.UnixMilli())
	}

	if isHistoryTable(tableName) {
		return s.searchCRMObjectsHistory(ctx, cfg, properties, startMs, opts, results)
	}

	if err := s.searchCRMObjects(ctx, cfg, properties, startMs, opts, results); err != nil {
		return err
	}

	return s.sweepArchivedCRMObjects(ctx, cfg, properties, opts, results)
}

func timeToMs(val *time.Time) string {
	if val == nil || val.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d", val.UnixMilli())
}

func (s *Hubspotsource) searchCRMObjects(ctx context.Context, cfg tableConfig, properties []string, startDateMs string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("crm/v3/objects/%s/search", cfg.ObjectType)
	totalProcessed := 0
	endMs := timeToMs(opts.IntervalEnd)
	config.Debug("[HUBSPOT] search %s: interval_start=%s interval_end=%s", cfg.ObjectType, startDateMs, endMs)

	var lastID *string

	for {
		filters := []map[string]interface{}{
			{
				"propertyName": cfg.IncrementalKey,
				"operator":     "GTE",
				"value":        startDateMs,
			},
		}
		if endMs != "" {
			filters = append(filters, map[string]interface{}{
				"propertyName": cfg.IncrementalKey,
				"operator":     "LTE",
				"value":        endMs,
			})
		}
		if lastID != nil {
			filters = append(filters, map[string]interface{}{
				"propertyName": "hs_object_id",
				"operator":     "GT",
				"value":        *lastID,
			})
		}

		body := map[string]interface{}{
			"filterGroups": []map[string]interface{}{
				{
					"filters": filters,
				},
			},
			"properties": properties,
			"sorts": []map[string]interface{}{
				{
					"propertyName": "hs_object_id",
					"direction":    "ASCENDING",
				},
			},
			"limit": defaultPageLimit,
		}

		windowYielded := 0

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var searchResp crmListResponse
			resp, err := s.searchClient.R(ctx).SetBody(body).SetResult(&searchResp).Post(endpoint)
			if err != nil {
				return fmt.Errorf("failed to search %s: %w", endpoint, err)
			}
			if !resp.IsSuccess() {
				return fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
			}

			if len(searchResp.Results) > 0 {
				objects := make([]map[string]interface{}, 0, len(searchResp.Results))
				for _, result := range searchResp.Results {
					obj := flattenCRMResult(result)
					obj[archivedAtColumn] = nil
					objects = append(objects, obj)

					objID := fmt.Sprintf("%v", obj["hs_object_id"])
					if objID == "" || objID == "<nil>" {
						objID = fmt.Sprintf("%v", obj["id"])
					}
					lastID = &objID
				}

				if len(cfg.Associations) > 0 {
					objIDs := make([]string, 0, len(objects))
					for _, obj := range objects {
						id := fmt.Sprintf("%v", obj["hs_object_id"])
						if id == "" || id == "<nil>" {
							id = fmt.Sprintf("%v", obj["id"])
						}
						objIDs = append(objIDs, id)
					}

					type assocResult struct {
						assocType string
						assocMap  map[string][]string
					}

					var wg sync.WaitGroup
					assocCh := make(chan assocResult, len(cfg.Associations))
					for _, assocType := range cfg.Associations {
						wg.Add(1)
						go func(at string) {
							defer wg.Done()
							am, err := s.fetchAssociationsBatch(ctx, cfg.ObjectType, at, objIDs)
							if err != nil {
								config.Debug("[HUBSPOT] Failed to fetch associations %s->%s: %v", cfg.ObjectType, at, err)
								return
							}
							assocCh <- assocResult{assocType: at, assocMap: am}
						}(assocType)
					}
					wg.Wait()
					close(assocCh)

					for res := range assocCh {
						for _, obj := range objects {
							objID := fmt.Sprintf("%v", obj["hs_object_id"])
							if objID == "" || objID == "<nil>" {
								objID = fmt.Sprintf("%v", obj["id"])
							}
							toIDs := res.assocMap[objID]
							var values []map[string]interface{}
							seen := make(map[string]struct{})
							for _, aid := range toIDs {
								key := objID + "_" + aid
								if _, dup := seen[key]; dup {
									continue
								}
								seen[key] = struct{}{}
								values = append(values, map[string]interface{}{
									"value":               objID,
									res.assocType + "_id": aid,
								})
							}
							obj[res.assocType] = values
						}
					}
				}

				record, err := arrowconv.ItemsToArrowRecordWithSchema(objects, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to build arrow record: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				windowYielded += len(objects)
				totalProcessed += len(objects)
			}

			if windowYielded >= 10000 {
				break
			}

			if searchResp.Paging == nil || searchResp.Paging.Next == nil {
				break
			}
			body["after"] = searchResp.Paging.Next.After
		}

		config.Debug("[HUBSPOT] search %s: window done, yielded=%d last_id=%v", cfg.ObjectType, windowYielded, lastID)

		if windowYielded < 10000 {
			break
		}
	}

	config.Debug("[HUBSPOT] Finished searching %s: %d total records", endpoint, totalProcessed)
	return nil
}

func (s *Hubspotsource) sweepArchivedCRMObjects(ctx context.Context, cfg tableConfig, properties []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	listEndpoint := fmt.Sprintf("crm/v3/objects/%s", cfg.ObjectType)
	batchEndpoint := fmt.Sprintf("crm/v3/objects/%s/batch/read", cfg.ObjectType)
	cursor := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", fmt.Sprintf("%d", getPageLimit)).
			SetQueryParam("archived", "true")
		if cursor != "" {
			req.SetQueryParam("after", cursor)
		}

		var listResp crmListResponse
		resp, err := req.SetResult(&listResp).Get(listEndpoint)
		if err != nil {
			return fmt.Errorf("failed to list archived %s: %w", listEndpoint, err)
		}
		if !resp.IsSuccess() {
			if cursor == "" && (resp.StatusCode() == 400 || resp.StatusCode() == 404) {
				config.Debug("[HUBSPOT] Warning: archived sweep skipped for %s (status %d)", cfg.ObjectType, resp.StatusCode())
				return nil
			}
			return fmt.Errorf("hubspot API %s?archived=true returned status %d: %s", listEndpoint, resp.StatusCode(), resp.String())
		}

		if len(listResp.Results) > 0 {
			inputs := make([]map[string]string, 0, len(listResp.Results))
			archivedAtByID := make(map[string]interface{}, len(listResp.Results))
			for _, result := range listResp.Results {
				id := fmt.Sprintf("%v", result["id"])
				if id == "" || id == "<nil>" {
					continue
				}
				inputs = append(inputs, map[string]string{"id": id})
				archivedAtByID[id] = result["archivedAt"]
			}

			if len(inputs) > 0 {
				var batchResp struct {
					Results []map[string]interface{} `json:"results"`
				}
				resp, err := s.client.R(ctx).
					SetQueryParam("archived", "true").
					SetBody(map[string]interface{}{
						"inputs":     inputs,
						"properties": properties,
					}).
					SetResult(&batchResp).
					Post(batchEndpoint)
				if err != nil {
					return fmt.Errorf("failed to batch read archived %s: %w", cfg.ObjectType, err)
				}
				if !resp.IsSuccess() {
					return fmt.Errorf("hubspot API %s?archived=true returned status %d: %s", batchEndpoint, resp.StatusCode(), resp.String())
				}

				if len(batchResp.Results) > 0 {
					objects := make([]map[string]interface{}, 0, len(batchResp.Results))
					objIDs := make([]string, 0, len(batchResp.Results))
					for _, result := range batchResp.Results {
						id := fmt.Sprintf("%v", result["id"])
						if id == "" || id == "<nil>" {
							continue
						}
						obj := flattenCRMResult(result)
						if archivedAt, ok := archivedAtByID[id]; ok {
							obj[archivedAtColumn] = archivedAt
						} else {
							obj[archivedAtColumn] = result["archivedAt"]
						}
						objects = append(objects, obj)
						objIDs = append(objIDs, id)
					}

					if len(cfg.Associations) > 0 {
						type assocResult struct {
							assocType string
							assocMap  map[string][]string
						}

						var wg sync.WaitGroup
						assocCh := make(chan assocResult, len(cfg.Associations))
						for _, assocType := range cfg.Associations {
							wg.Add(1)
							go func(at string) {
								defer wg.Done()
								am, err := s.fetchAssociationsBatch(ctx, cfg.ObjectType, at, objIDs)
								if err != nil {
									config.Debug("[HUBSPOT] Failed to fetch associations %s->%s for archived sweep: %v", cfg.ObjectType, at, err)
									return
								}
								assocCh <- assocResult{assocType: at, assocMap: am}
							}(assocType)
						}
						wg.Wait()
						close(assocCh)

						for res := range assocCh {
							for _, obj := range objects {
								objID := fmt.Sprintf("%v", obj["hs_object_id"])
								if objID == "" || objID == "<nil>" {
									objID = fmt.Sprintf("%v", obj["id"])
								}
								toIDs := res.assocMap[objID]
								var values []map[string]interface{}
								seen := make(map[string]struct{})
								for _, aid := range toIDs {
									key := objID + "_" + aid
									if _, dup := seen[key]; dup {
										continue
									}
									seen[key] = struct{}{}
									values = append(values, map[string]interface{}{
										"value":               objID,
										res.assocType + "_id": aid,
									})
								}
								obj[res.assocType] = values
							}
						}
					}

					record, err := arrowconv.ItemsToArrowRecordWithSchema(objects, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to build arrow record: %w", err)
					}
					results <- source.RecordBatchResult{Batch: record}
					totalProcessed += len(objects)
				}
			}
		}

		if listResp.Paging == nil || listResp.Paging.Next == nil {
			break
		}
		cursor = listResp.Paging.Next.After
	}

	config.Debug("[HUBSPOT] Finished archived sweep %s: %d archived records", listEndpoint, totalProcessed)
	return nil
}

func (s *Hubspotsource) fetchAssociationsBatch(ctx context.Context, fromType string, toType string, objectIDs []string) (map[string][]string, error) {
	if len(objectIDs) == 0 {
		return map[string][]string{}, nil
	}

	endpoint := fmt.Sprintf("crm/v4/associations/%s/%s/batch/read", fromType, toType)
	result := make(map[string][]string)

	for i := 0; i < len(objectIDs); i += batchReadLimit {
		end := i + batchReadLimit
		if end > len(objectIDs) {
			end = len(objectIDs)
		}

		inputs := make([]map[string]string, 0, end-i)
		for _, id := range objectIDs[i:end] {
			inputs = append(inputs, map[string]string{"id": id})
		}

		var batchResp struct {
			Results []struct {
				From struct {
					ID string `json:"id"`
				} `json:"from"`
				To []struct {
					ToObjectId interface{} `json:"toObjectId"`
				} `json:"to"`
			} `json:"results"`
		}

		resp, err := s.client.R(ctx).
			SetBody(map[string]interface{}{"inputs": inputs}).
			SetResult(&batchResp).
			Post(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch associations %s->%s: %w", fromType, toType, err)
		}
		if !resp.IsSuccess() && resp.StatusCode() != 404 && len(inputs) > 1 {
			// HubSpot returns 400 for the whole batch if even one input id is
			// invalid (deleted/archived). Same thing can happen for 429/5xx that
			// survived the HTTP client's retries. Split and recurse so the other
			// ids in the chunk don't silently lose their associations.
			mid := len(inputs) / 2
			left, err := s.fetchAssociationsBatch(ctx, fromType, toType, objectIDs[i:i+mid])
			if err != nil {
				return nil, err
			}
			right, err := s.fetchAssociationsBatch(ctx, fromType, toType, objectIDs[i+mid:end])
			if err != nil {
				return nil, err
			}
			for k, v := range left {
				result[k] = append(result[k], v...)
			}
			for k, v := range right {
				result[k] = append(result[k], v...)
			}
			continue
		}
		if !resp.IsSuccess() {
			config.Debug("[HUBSPOT] association %s->%s skipped id=%v status=%d: %s", fromType, toType, objectIDs[i:end], resp.StatusCode(), resp.String())
			continue
		}

		for _, item := range batchResp.Results {
			fromID := item.From.ID
			if fromID == "" {
				continue
			}
			for _, to := range item.To {
				if to.ToObjectId != nil {
					result[fromID] = append(result[fromID], fmt.Sprintf("%v", to.ToObjectId))
				}
			}
		}
	}

	return result, nil
}

func flattenHistoryRows(result map[string]interface{}) []map[string]interface{} {
	id := ""
	if v, ok := result["id"]; ok {
		id = fmt.Sprintf("%v", v)
	}
	if id == "" {
		if props, ok := result["properties"].(map[string]interface{}); ok {
			if v, ok := props["hs_object_id"]; ok {
				id = fmt.Sprintf("%v", v)
			}
		}
	}

	history, ok := result["propertiesWithHistory"].(map[string]interface{})
	if !ok {
		return nil
	}

	var rows []map[string]interface{}
	for propName, entries := range history {
		list, ok := entries.([]interface{})
		if !ok {
			continue
		}
		for _, e := range list {
			entry, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			row := make(map[string]interface{}, len(entry)+2)
			row["hs_object_id"] = id
			row["property_name"] = propName
			for k, v := range entry {
				row[k] = v
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *Hubspotsource) searchCRMObjectsHistory(ctx context.Context, cfg tableConfig, properties []string, startDateMs string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("crm/v3/objects/%s/search", cfg.ObjectType)
	totalProcessed := 0
	endMs := timeToMs(opts.IntervalEnd)
	config.Debug("[HUBSPOT] search %s (history): interval_start=%s interval_end=%s", cfg.ObjectType, startDateMs, endMs)

	var lastID *string

	for {
		filters := []map[string]interface{}{
			{
				"propertyName": cfg.IncrementalKey,
				"operator":     "GTE",
				"value":        startDateMs,
			},
		}
		if endMs != "" {
			filters = append(filters, map[string]interface{}{
				"propertyName": cfg.IncrementalKey,
				"operator":     "LTE",
				"value":        endMs,
			})
		}
		if lastID != nil {
			filters = append(filters, map[string]interface{}{
				"propertyName": "hs_object_id",
				"operator":     "GT",
				"value":        *lastID,
			})
		}

		body := map[string]interface{}{
			"filterGroups": []map[string]interface{}{
				{
					"filters": filters,
				},
			},
			"properties": []string{"hs_object_id"},
			"sorts": []map[string]interface{}{
				{
					"propertyName": "hs_object_id",
					"direction":    "ASCENDING",
				},
			},
			"limit": defaultPageLimit,
		}

		windowYielded := 0

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var searchResp crmListResponse
			resp, err := s.searchClient.R(ctx).SetBody(body).SetResult(&searchResp).Post(endpoint)
			if err != nil {
				return fmt.Errorf("failed to search %s: %w", endpoint, err)
			}
			if !resp.IsSuccess() {
				return fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
			}

			if len(searchResp.Results) > 0 {
				ids := make([]string, 0, len(searchResp.Results))
				for _, r := range searchResp.Results {
					obj := flattenCRMResult(r)
					objID := fmt.Sprintf("%v", obj["hs_object_id"])
					if objID == "" || objID == "<nil>" {
						objID = fmt.Sprintf("%v", obj["id"])
					}
					ids = append(ids, objID)
					lastID = &objID
				}

				rowCount, err := s.batchReadHistory(ctx, cfg.ObjectType, ids, properties, opts, results)
				if err != nil {
					return err
				}
				windowYielded += len(searchResp.Results)
				totalProcessed += rowCount
			}

			if windowYielded >= 10000 {
				break
			}

			if searchResp.Paging == nil || searchResp.Paging.Next == nil {
				break
			}
			body["after"] = searchResp.Paging.Next.After
		}

		config.Debug("[HUBSPOT] search %s (history): window done, yielded=%d last_id=%v", cfg.ObjectType, windowYielded, lastID)

		if windowYielded < 10000 {
			break
		}
	}

	config.Debug("[HUBSPOT] Finished searching %s (history): %d total rows", endpoint, totalProcessed)
	return nil
}

const (
	batchReadLimit        = 100
	historyBatchReadLimit = 25
)

func (s *Hubspotsource) batchReadHistory(ctx context.Context, objectType string, ids []string, properties []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	endpoint := fmt.Sprintf("crm/v3/objects/%s/batch/read", objectType)
	totalRows := 0

	for i := 0; i < len(ids); i += historyBatchReadLimit {
		end := i + historyBatchReadLimit
		if end > len(ids) {
			end = len(ids)
		}

		inputs := make([]map[string]string, 0, end-i)
		for _, id := range ids[i:end] {
			inputs = append(inputs, map[string]string{"id": id})
		}

		var batchResp struct {
			Results []map[string]interface{} `json:"results"`
		}
		resp, err := s.client.R(ctx).
			SetBody(map[string]interface{}{
				"inputs":                inputs,
				"propertiesWithHistory": properties,
			}).
			SetResult(&batchResp).
			Post(endpoint)
		if err != nil {
			return totalRows, fmt.Errorf("failed to batch read %s with history: %w", objectType, err)
		}
		if !resp.IsSuccess() {
			return totalRows, fmt.Errorf("hubspot API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var rows []map[string]interface{}
		for _, r := range batchResp.Results {
			rows = append(rows, flattenHistoryRows(r)...)
		}
		if len(rows) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, nil, opts.ExcludeColumns)
			if err != nil {
				return totalRows, fmt.Errorf("failed to build arrow record: %w", err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalRows += len(rows)
		}
	}
	return totalRows, nil
}
