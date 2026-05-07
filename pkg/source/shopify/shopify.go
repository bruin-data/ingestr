package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
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
	apiVersion     = "2025-01"
	defaultBatch   = 250
	maxPageSize    = 250
	rateLimit      = 4
	rateLimitBurst = 2
)

var supportedTables = []string{
	"orders",
	"customers",
	"products",
	"discounts",
	"inventory_items",
	"events",
	"transactions",
	"balance",
	"price_rules",
}

var orderFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "admin_graphql_api_id", DataType: schema.TypeString, Nullable: true},
	{Name: "app_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "billing_address", DataType: schema.TypeJSON, Nullable: true},
	{Name: "browser_ip", DataType: schema.TypeString, Nullable: true},
	{Name: "buyer_accepts_marketing", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "cancel_reason", DataType: schema.TypeString, Nullable: true},
	{Name: "cancelled_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "cart_token", DataType: schema.TypeString, Nullable: true},
	{Name: "checkout_token", DataType: schema.TypeString, Nullable: true},
	{Name: "client_details", DataType: schema.TypeJSON, Nullable: true},
	{Name: "closed_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "company", DataType: schema.TypeJSON, Nullable: true},
	{Name: "confirmation_number", DataType: schema.TypeString, Nullable: true},
	{Name: "confirmed", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "contact_email", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "current_subtotal_price", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "current_subtotal_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "current_total_additional_fees_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "current_total_discounts", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "current_total_discounts_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "current_total_duties_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "current_total_price", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "current_total_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "current_total_tax", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "current_total_tax_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "customer", DataType: schema.TypeJSON, Nullable: true},
	{Name: "customer_locale", DataType: schema.TypeString, Nullable: true},
	{Name: "discount_applications", DataType: schema.TypeJSON, Nullable: true},
	{Name: "discount_codes", DataType: schema.TypeJSON, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "estimated_taxes", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "financial_status", DataType: schema.TypeString, Nullable: true},
	{Name: "fulfillment_status", DataType: schema.TypeString, Nullable: true},
	{Name: "fulfillments", DataType: schema.TypeJSON, Nullable: true},
	{Name: "gateway", DataType: schema.TypeString, Nullable: true},
	{Name: "landing_site", DataType: schema.TypeString, Nullable: true},
	{Name: "line_items", DataType: schema.TypeJSON, Nullable: true},
	{Name: "location_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "merchant_of_record_app_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "note", DataType: schema.TypeString, Nullable: true},
	{Name: "note_attributes", DataType: schema.TypeJSON, Nullable: true},
	{Name: "number", DataType: schema.TypeInt64, Nullable: true},
	{Name: "order_number", DataType: schema.TypeInt64, Nullable: true},
	{Name: "order_status_url", DataType: schema.TypeString, Nullable: true},
	{Name: "original_total_additional_fees_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "original_total_duties_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "payment_gateway_names", DataType: schema.TypeJSON, Nullable: true},
	{Name: "payment_terms", DataType: schema.TypeJSON, Nullable: true},
	{Name: "phone", DataType: schema.TypeString, Nullable: true},
	{Name: "po_number", DataType: schema.TypeString, Nullable: true},
	{Name: "presentment_currency", DataType: schema.TypeString, Nullable: true},
	{Name: "processed_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "referring_site", DataType: schema.TypeString, Nullable: true},
	{Name: "refunds", DataType: schema.TypeJSON, Nullable: true},
	{Name: "shipping_address", DataType: schema.TypeJSON, Nullable: true},
	{Name: "shipping_lines", DataType: schema.TypeJSON, Nullable: true},
	{Name: "source_identifier", DataType: schema.TypeString, Nullable: true},
	{Name: "source_name", DataType: schema.TypeString, Nullable: true},
	{Name: "source_url", DataType: schema.TypeString, Nullable: true},
	{Name: "subtotal_price", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "subtotal_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "tags", DataType: schema.TypeString, Nullable: true},
	{Name: "tax_exempt", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "tax_lines", DataType: schema.TypeJSON, Nullable: true},
	{Name: "taxes_included", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "test", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "token", DataType: schema.TypeString, Nullable: true},
	{Name: "total_discounts", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_discounts_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_line_items_price", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_line_items_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_outstanding", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_price", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_shipping_price_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_tax", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_tax_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_tip_received", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "total_weight", DataType: schema.TypeDecimal, Precision: 38, Scale: 9, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "user_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "duties_included", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "merchant_business_entity_id", DataType: schema.TypeString, Nullable: true},
	{Name: "total_cash_rounding_payment_adjustment_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "total_cash_rounding_refund_adjustment_set", DataType: schema.TypeJSON, Nullable: true},
	{Name: "checkout_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "reference", DataType: schema.TypeString, Nullable: true},
}

var customerFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "admin_graphql_api_id", DataType: schema.TypeString, Nullable: true},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "first_name", DataType: schema.TypeString, Nullable: true},
	{Name: "last_name", DataType: schema.TypeString, Nullable: true},
	{Name: "phone", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
	{Name: "note", DataType: schema.TypeString, Nullable: true},
	{Name: "tags", DataType: schema.TypeString, Nullable: true},
	{Name: "tax_exempt", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "tax_exemptions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "verified_email", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "multipass_identifier", DataType: schema.TypeString, Nullable: true},
	{Name: "accepts_marketing", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "accepts_marketing_updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "marketing_opt_in_level", DataType: schema.TypeString, Nullable: true},
	{Name: "locale", DataType: schema.TypeString, Nullable: true},
	{Name: "orders_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "total_spent", DataType: schema.TypeString, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "default_address", DataType: schema.TypeJSON, Nullable: true},
	{Name: "addresses", DataType: schema.TypeJSON, Nullable: true},
	{Name: "email_marketing_consent", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sms_marketing_consent", DataType: schema.TypeJSON, Nullable: true},
	{Name: "last_order_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "last_order_name", DataType: schema.TypeString, Nullable: true},
}

var productFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: true},
	{Name: "available_publications_count", DataType: schema.TypeJSON, Nullable: true},
	{Name: "category", DataType: schema.TypeJSON, Nullable: true},
	{Name: "compare_at_price_range", DataType: schema.TypeJSON, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "default_cursor", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "description_html", DataType: schema.TypeString, Nullable: true},
	{Name: "handle", DataType: schema.TypeString, Nullable: true},
	{Name: "metafields", DataType: schema.TypeJSON, Nullable: true},
	{Name: "options", DataType: schema.TypeJSON, Nullable: true},
	{Name: "price_range_v2", DataType: schema.TypeJSON, Nullable: true},
	{Name: "product_type", DataType: schema.TypeString, Nullable: true},
	{Name: "published_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "requires_selling_plan", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "tags", DataType: schema.TypeString, Nullable: true},
	{Name: "template_suffix", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "total_inventory", DataType: schema.TypeInt64, Nullable: true},
	{Name: "tracks_inventory", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "variants_count", DataType: schema.TypeJSON, Nullable: true},
	{Name: "variants_first250", DataType: schema.TypeJSON, Nullable: true},
	{Name: "vendor", DataType: schema.TypeString, Nullable: true},
}

var inventoryItemFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "cost", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "country_code_of_origin", DataType: schema.TypeString, Nullable: true},
	{Name: "province_code_of_origin", DataType: schema.TypeString, Nullable: true},
	{Name: "harmonized_system_code", DataType: schema.TypeString, Nullable: true},
	{Name: "duplicate_sku_count", DataType: schema.TypeInt64, Nullable: true},
	{Name: "inventory_history_url", DataType: schema.TypeString, Nullable: true},
	{Name: "legacy_resource_id", DataType: schema.TypeString, Nullable: true},
	{Name: "measurement", DataType: schema.TypeJSON, Nullable: true},
	{Name: "requires_shipping", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "sku", DataType: schema.TypeString, Nullable: true},
	{Name: "tracked", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "tracked_editable", DataType: schema.TypeJSON, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "inventory_levels", DataType: schema.TypeJSON, Nullable: true},
	{Name: "variant", DataType: schema.TypeJSON, Nullable: true},
}

var eventFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "subject_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "subject_type", DataType: schema.TypeString, Nullable: true},
	{Name: "verb", DataType: schema.TypeString, Nullable: true},
	{Name: "arguments", DataType: schema.TypeJSON, Nullable: true},
	{Name: "message", DataType: schema.TypeString, Nullable: true},
	{Name: "author", DataType: schema.TypeString, Nullable: true},
	{Name: "description", DataType: schema.TypeString, Nullable: true},
	{Name: "path", DataType: schema.TypeString, Nullable: true},
}

var discountFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: true},
	{Name: "discount", DataType: schema.TypeJSON, Nullable: true},
	{Name: "metafields_first250", DataType: schema.TypeJSON, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var transactionFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "type", DataType: schema.TypeString, Nullable: true},
	{Name: "test", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "payout_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "payout_status", DataType: schema.TypeString, Nullable: true},
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "amount", DataType: schema.TypeString, Nullable: true},
	{Name: "fee", DataType: schema.TypeString, Nullable: true},
	{Name: "net", DataType: schema.TypeString, Nullable: true},
	{Name: "source_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "source_type", DataType: schema.TypeString, Nullable: true},
	{Name: "source_order_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "source_order_transaction_id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "processed_at", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var balanceFields = []schema.Column{
	{Name: "currency", DataType: schema.TypeString, Nullable: true},
	{Name: "amount", DataType: schema.TypeString, Nullable: true},
}

var priceRuleFields = []schema.Column{
	{Name: "id", DataType: schema.TypeInt64, Nullable: true},
	{Name: "admin_graphql_api_id", DataType: schema.TypeString, Nullable: true},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "value_type", DataType: schema.TypeString, Nullable: true},
	{Name: "value", DataType: schema.TypeString, Nullable: true},
	{Name: "customer_selection", DataType: schema.TypeString, Nullable: true},
	{Name: "target_type", DataType: schema.TypeString, Nullable: true},
	{Name: "target_selection", DataType: schema.TypeString, Nullable: true},
	{Name: "allocation_method", DataType: schema.TypeString, Nullable: true},
	{Name: "allocation_limit", DataType: schema.TypeInt64, Nullable: true},
	{Name: "once_per_customer", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "usage_limit", DataType: schema.TypeInt64, Nullable: true},
	{Name: "starts_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "ends_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "entitled_product_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "entitled_variant_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "entitled_collection_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "entitled_country_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_product_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_variant_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_collection_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "customer_segment_prerequisite_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_customer_ids", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_subtotal_range", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_quantity_range", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_shipping_price_range", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_to_entitlement_quantity_ratio", DataType: schema.TypeJSON, Nullable: true},
	{Name: "prerequisite_to_entitlement_purchase", DataType: schema.TypeJSON, Nullable: true},
}

const tokenURLFormat = "https://%s/admin/oauth/access_token"

type ShopifySource struct {
	store        string
	apiKey       string
	clientID     string
	clientSecret string
	client       *gonghttp.Client
	restClient   *gonghttp.Client
	baseURL      string
	restBaseURL  string
}

func NewShopifySource() *ShopifySource {
	return &ShopifySource{}
}

func (s *ShopifySource) Schemes() []string {
	return []string{"shopify"}
}

func (s *ShopifySource) Connect(ctx context.Context, uri string) error {
	params, err := parseShopifyURI(uri)
	if err != nil {
		return err
	}

	s.store = params.store
	s.apiKey = params.apiKey
	s.clientID = params.clientID
	s.clientSecret = params.clientSecret

	if s.apiKey == "" {
		if s.clientID == "" || s.clientSecret == "" {
			return fmt.Errorf("either api_key or both client_id and client_secret must be provided")
		}
		config.Debug("[SHOPIFY] No api_key provided, exchanging client credentials for access token")
		token, err := s.getShopifyAccessToken(ctx, s.store, s.clientID, s.clientSecret)
		if err != nil {
			return fmt.Errorf("failed to get Shopify access token: %w", err)
		}
		s.apiKey = token
	}

	s.baseURL = fmt.Sprintf("https://%s/admin/api/%s/graphql.json", s.store, apiVersion)
	s.restBaseURL = fmt.Sprintf("https://%s/admin/api/%s", s.store, apiVersion)

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(s.baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewAPIKeyAuth("X-Shopify-Access-Token", s.apiKey, true)),
	)

	s.restClient = gonghttp.New(
		gonghttp.WithBaseURL(s.restBaseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewAPIKeyAuth("X-Shopify-Access-Token", s.apiKey, true)),
	)

	config.Debug("[SHOPIFY] Connected to store: %s", s.store)
	return nil
}

func (s *ShopifySource) getShopifyAccessToken(ctx context.Context, store, clientID, clientSecret string) (string, error) {
	tokenURL := fmt.Sprintf(tokenURLFormat, store)

	client := gonghttp.New(
		gonghttp.WithTimeout(30 * time.Second),
	)
	defer func() { _ = client.Close() }()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}

	resp, err := client.R(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormData(map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     clientID,
			"client_secret": clientSecret,
		}).
		SetResult(&tokenResp).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}

	if !resp.IsSuccess() {
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

type shopifyURIParams struct {
	store        string
	apiKey       string
	clientID     string
	clientSecret string
}

func parseShopifyURI(uri string) (shopifyURIParams, error) {
	if !strings.HasPrefix(uri, "shopify://") {
		return shopifyURIParams{}, fmt.Errorf("invalid shopify URI: must start with shopify://")
	}

	rest := strings.TrimPrefix(uri, "shopify://")
	parts := strings.SplitN(rest, "?", 2)
	store := parts[0]

	if store == "" {
		return shopifyURIParams{}, fmt.Errorf("shopify store is required in URI (shopify://store.myshopify.com?api_key=...)")
	}

	if !strings.Contains(store, ".myshopify.com") {
		store = store + ".myshopify.com"
	}

	if len(parts) < 2 {
		return shopifyURIParams{}, fmt.Errorf("api_key or client_id/client_secret parameters are required in URI")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return shopifyURIParams{}, fmt.Errorf("failed to parse shopify URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	clientID := values.Get("client_id")
	clientSecret := values.Get("client_secret")

	if apiKey == "" && (clientID == "" || clientSecret == "") {
		return shopifyURIParams{}, fmt.Errorf("either api_key or both client_id and client_secret are required in URI")
	}

	return shopifyURIParams{
		store:        store,
		apiKey:       apiKey,
		clientID:     clientID,
		clientSecret: clientSecret,
	}, nil
}

func (s *ShopifySource) Close(ctx context.Context) error {
	if s.restClient != nil {
		_ = s.restClient.Close()
	}
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *ShopifySource) HandlesIncrementality() bool {
	return true
}

func (s *ShopifySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableSchema, err := s.getSchema(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	tableName := req.Name

	incrementalKey := "updated_at"
	strategy := config.StrategyMerge
	switch tableName {
	case "events":
		incrementalKey = "created_at"
	case "transactions":
		incrementalKey = "id"
	case "balance":
		incrementalKey = ""
		strategy = config.StrategySCD2
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tableSchema.PrimaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *ShopifySource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	var fields []schema.Column
	primaryKeys := []string{"id"}
	switch table {
	case "orders":
		fields = orderFields
	case "customers":
		fields = customerFields
	case "products":
		fields = productFields
	case "discounts":
		fields = discountFields
	case "inventory_items":
		fields = inventoryItemFields
	case "events":
		fields = eventFields
	case "transactions":
		fields = transactionFields
	case "balance":
		fields = balanceFields
		primaryKeys = []string{"currency"}
	case "price_rules":
		fields = priceRuleFields
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	return &schema.TableSchema{
		Name:        table,
		Columns:     fields,
		PrimaryKeys: primaryKeys,
	}, nil
}

func (s *ShopifySource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if !isValidTable(table) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", table, strings.Join(supportedTables, ", "))
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "orders":
			err = s.readOrders(ctx, opts, results)
		case "customers":
			err = s.readCustomers(ctx, opts, results)
		case "products":
			err = s.readProducts(ctx, opts, results)
		case "discounts":
			err = s.readDiscounts(ctx, opts, results)
		case "inventory_items":
			err = s.readInventoryItems(ctx, opts, results)
		case "events":
			err = s.readEvents(ctx, opts, results)
		case "transactions":
			err = s.readTransactions(ctx, opts, results)
		case "balance":
			err = s.readBalance(ctx, opts, results)
		case "price_rules":
			err = s.readPriceRules(ctx, opts, results)
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
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (s *ShopifySource) executeGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	config.Debug("[SHOPIFY] Executing GraphQL query")

	var resp graphQLResponse
	httpResp, err := s.client.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetResult(&resp).
		Post("")
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}

	if !httpResp.IsSuccess() {
		return nil, fmt.Errorf("graphql request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	if len(resp.Errors) > 0 {
		var errMsgs []string
		for _, e := range resp.Errors {
			errMsgs = append(errMsgs, e.Message)
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(errMsgs, "; "))
	}

	return resp.Data, nil
}

func (s *ShopifySource) readOrders(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading orders")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	endpoint := fmt.Sprintf("/orders.json?status=any&limit=%d", pageSize)

	if opts.IntervalStart != nil {
		endpoint += "&updated_at_min=" + url.QueryEscape(opts.IntervalStart.Format(time.RFC3339))
	}

	if opts.IntervalEnd != nil {
		endpoint += "&updated_at_max=" + url.QueryEscape(opts.IntervalEnd.Format(time.RFC3339))
	}

	paginator := gonghttp.NewLinkHeaderPaginator(endpoint)
	totalSent := 0
	batchNum := 0

	for paginator.HasNext() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp struct {
			Orders []map[string]interface{} `json:"orders"`
		}

		httpResp, err := paginator.NextPage(ctx, s.restClient, &resp)
		if err != nil {
			return fmt.Errorf("failed to fetch orders: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("orders request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Orders) == 0 {
			break
		}

		var items []map[string]interface{}
		for _, order := range resp.Orders {
			items = append(items, s.transformOrder(order))

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, orderFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert orders to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending orders batch %d with %d records", batchNum, len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
	}

	config.Debug("[SHOPIFY] Finished reading orders, total records: %d", totalSent)
	return nil
}

func (s *ShopifySource) transformOrder(order map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getIntPtr(order, "id")
	result["admin_graphql_api_id"] = getStringPtr(order, "admin_graphql_api_id")
	result["app_id"] = getIntPtr(order, "app_id")
	result["billing_address"] = order["billing_address"]
	result["browser_ip"] = getStringPtr(order, "browser_ip")
	result["buyer_accepts_marketing"] = getBoolPtr(order, "buyer_accepts_marketing")
	result["cancel_reason"] = getStringPtr(order, "cancel_reason")
	result["cancelled_at"] = parseTimestampPtr(getString(order, "cancelled_at"))
	result["cart_token"] = getStringPtr(order, "cart_token")
	result["checkout_token"] = getStringPtr(order, "checkout_token")
	result["client_details"] = order["client_details"]
	result["closed_at"] = parseTimestampPtr(getString(order, "closed_at"))
	result["company"] = order["company"]
	result["confirmation_number"] = getStringPtr(order, "confirmation_number")
	result["confirmed"] = getBoolPtr(order, "confirmed")
	result["contact_email"] = getStringPtr(order, "contact_email")
	result["created_at"] = parseTimestampPtr(getString(order, "created_at"))
	result["currency"] = getStringPtr(order, "currency")
	result["current_subtotal_price"] = order["current_subtotal_price"]
	result["current_subtotal_price_set"] = order["current_subtotal_price_set"]
	result["current_total_additional_fees_set"] = order["current_total_additional_fees_set"]
	result["current_total_discounts"] = order["current_total_discounts"]
	result["current_total_discounts_set"] = order["current_total_discounts_set"]
	result["current_total_duties_set"] = order["current_total_duties_set"]
	result["current_total_price"] = order["current_total_price"]
	result["current_total_price_set"] = order["current_total_price_set"]
	result["current_total_tax"] = order["current_total_tax"]
	result["current_total_tax_set"] = order["current_total_tax_set"]
	result["customer"] = normalizeUTCTimestamps(order["customer"])
	result["customer_locale"] = getStringPtr(order, "customer_locale")
	result["discount_applications"] = order["discount_applications"]
	result["discount_codes"] = order["discount_codes"]
	result["email"] = getStringPtr(order, "email")
	result["estimated_taxes"] = getBoolPtr(order, "estimated_taxes")
	result["financial_status"] = getStringPtr(order, "financial_status")
	result["fulfillment_status"] = getStringPtr(order, "fulfillment_status")
	result["fulfillments"] = order["fulfillments"]
	result["gateway"] = getStringPtr(order, "gateway")
	result["landing_site"] = getStringPtr(order, "landing_site")
	result["line_items"] = order["line_items"]
	result["location_id"] = getIntPtr(order, "location_id")
	result["merchant_of_record_app_id"] = getIntPtr(order, "merchant_of_record_app_id")
	result["name"] = getStringPtr(order, "name")
	result["note"] = getStringPtr(order, "note")
	result["note_attributes"] = order["note_attributes"]
	result["number"] = getIntPtr(order, "number")
	result["order_number"] = getIntPtr(order, "order_number")
	result["order_status_url"] = getStringPtr(order, "order_status_url")
	result["original_total_additional_fees_set"] = order["original_total_additional_fees_set"]
	result["original_total_duties_set"] = order["original_total_duties_set"]
	result["payment_gateway_names"] = order["payment_gateway_names"]
	result["payment_terms"] = order["payment_terms"]
	result["phone"] = getStringPtr(order, "phone")
	result["po_number"] = getStringPtr(order, "po_number")
	result["presentment_currency"] = getStringPtr(order, "presentment_currency")
	result["processed_at"] = parseTimestampPtr(getString(order, "processed_at"))
	result["referring_site"] = getStringPtr(order, "referring_site")
	result["refunds"] = order["refunds"]
	result["shipping_address"] = order["shipping_address"]
	result["shipping_lines"] = order["shipping_lines"]
	result["source_identifier"] = getStringPtr(order, "source_identifier")
	result["source_name"] = getStringPtr(order, "source_name")
	result["source_url"] = getStringPtr(order, "source_url")
	result["subtotal_price"] = order["subtotal_price"]
	result["subtotal_price_set"] = order["subtotal_price_set"]
	result["tags"] = getStringPtr(order, "tags")
	result["tax_exempt"] = getBoolPtr(order, "tax_exempt")
	result["tax_lines"] = order["tax_lines"]
	result["taxes_included"] = getBoolPtr(order, "taxes_included")
	result["test"] = getBoolPtr(order, "test")
	result["token"] = getStringPtr(order, "token")
	result["total_discounts"] = order["total_discounts"]
	result["total_discounts_set"] = order["total_discounts_set"]
	result["total_line_items_price"] = order["total_line_items_price"]
	result["total_line_items_price_set"] = order["total_line_items_price_set"]
	result["total_outstanding"] = order["total_outstanding"]
	result["total_price"] = order["total_price"]
	result["total_price_set"] = order["total_price_set"]
	result["total_shipping_price_set"] = order["total_shipping_price_set"]
	result["total_tax"] = order["total_tax"]
	result["total_tax_set"] = order["total_tax_set"]
	result["total_tip_received"] = order["total_tip_received"]
	result["total_weight"] = order["total_weight"]
	result["updated_at"] = parseTimestampPtr(getString(order, "updated_at"))
	result["user_id"] = getIntPtr(order, "user_id")
	result["duties_included"] = getBoolPtr(order, "duties_included")
	result["merchant_business_entity_id"] = getStringPtr(order, "merchant_business_entity_id")
	result["total_cash_rounding_payment_adjustment_set"] = order["total_cash_rounding_payment_adjustment_set"]
	result["total_cash_rounding_refund_adjustment_set"] = order["total_cash_rounding_refund_adjustment_set"]
	result["checkout_id"] = getIntPtr(order, "checkout_id")
	result["reference"] = getStringPtr(order, "reference")

	return result
}

func (s *ShopifySource) readCustomers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading customers")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	endpoint := fmt.Sprintf("/customers.json?limit=%d&order=updated_at+asc", pageSize)

	if opts.IntervalStart != nil {
		endpoint += "&updated_at_min=" + url.QueryEscape(opts.IntervalStart.Format(time.RFC3339))
	}

	if opts.IntervalEnd != nil {
		endpoint += "&updated_at_max=" + url.QueryEscape(opts.IntervalEnd.Format(time.RFC3339))
	}

	paginator := gonghttp.NewLinkHeaderPaginator(endpoint)
	totalSent := 0
	batchNum := 0

	for paginator.HasNext() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp struct {
			Customers []map[string]interface{} `json:"customers"`
		}

		httpResp, err := paginator.NextPage(ctx, s.restClient, &resp)
		if err != nil {
			return fmt.Errorf("failed to fetch customers: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("customers request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Customers) == 0 {
			break
		}

		var items []map[string]interface{}
		for _, customer := range resp.Customers {
			items = append(items, s.transformCustomer(customer))

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, customerFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert customers to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending customers batch %d with %d records", batchNum, len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
	}

	config.Debug("[SHOPIFY] Finished reading customers, total records: %d", totalSent)
	return nil
}

func (s *ShopifySource) transformCustomer(customer map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getIntPtr(customer, "id")
	result["admin_graphql_api_id"] = getStringPtr(customer, "admin_graphql_api_id")
	result["email"] = getStringPtr(customer, "email")
	result["first_name"] = getStringPtr(customer, "first_name")
	result["last_name"] = getStringPtr(customer, "last_name")
	result["phone"] = getStringPtr(customer, "phone")
	result["created_at"] = parseTimestampPtr(getString(customer, "created_at"))
	result["updated_at"] = parseTimestampPtr(getString(customer, "updated_at"))
	result["state"] = getStringPtr(customer, "state")
	result["note"] = getStringPtr(customer, "note")
	result["tags"] = getStringPtr(customer, "tags")
	result["tax_exempt"] = getBoolPtr(customer, "tax_exempt")
	result["tax_exemptions"] = customer["tax_exemptions"]
	result["verified_email"] = getBoolPtr(customer, "verified_email")
	result["multipass_identifier"] = getStringPtr(customer, "multipass_identifier")
	result["accepts_marketing"] = getBoolPtr(customer, "accepts_marketing")
	result["accepts_marketing_updated_at"] = parseTimestampPtr(getString(customer, "accepts_marketing_updated_at"))
	result["marketing_opt_in_level"] = getStringPtr(customer, "marketing_opt_in_level")
	result["locale"] = getStringPtr(customer, "locale")
	result["orders_count"] = getIntPtr(customer, "orders_count")
	result["total_spent"] = getStringPtr(customer, "total_spent")
	result["currency"] = getStringPtr(customer, "currency")
	result["default_address"] = customer["default_address"]
	result["addresses"] = customer["addresses"]
	result["email_marketing_consent"] = customer["email_marketing_consent"]
	result["sms_marketing_consent"] = customer["sms_marketing_consent"]
	result["last_order_id"] = getIntPtr(customer, "last_order_id")
	result["last_order_name"] = getStringPtr(customer, "last_order_name")

	return result
}

func (s *ShopifySource) readProducts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading products")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	query := `
query GetProducts($first: Int!, $after: String, $query: String) {
  products(first: $first, after: $after, query: $query, sortKey: UPDATED_AT, reverse: false) {
    edges {
      cursor
      node {
        availablePublicationsCount {
          count
          precision
        }
        category {
          id
        }
        compareAtPriceRange {
          maxVariantCompareAtPrice {
            amount
            currencyCode
          }
          minVariantCompareAtPrice {
            amount
            currencyCode
          }
        }
        createdAt
        defaultCursor
        description
        descriptionHtml
        handle
        id
        metafields(first: 250) {
          nodes {
            id
            key
            value
          }
        }
        options {
          linkedMetafield {
            key
            namespace
          }
          name
          optionValues {
            hasVariants
            id
            linkedMetafieldValue
            name
          }
          values
          id
          position
        }
        priceRangeV2 {
          maxVariantPrice {
            amount
            currencyCode
          }
          minVariantPrice {
            amount
            currencyCode
          }
        }
        productType
        publishedAt
        requiresSellingPlan
        status
        tags
        templateSuffix
        totalInventory
        title
        tracksInventory
        updatedAt
        vendor
        variantsCount {
          count
          precision
        }
        variantsFirst250: variants(first: 250) {
          nodes {
            id
            sku
          }
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

	return s.paginateAndSend(ctx, opts, results, query, "products", pageSize, productFields, s.transformProduct)
}

func (s *ShopifySource) transformProduct(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getStringPtr(node, "id")
	result["available_publications_count"] = node["availablePublicationsCount"]
	result["category"] = node["category"]
	result["compare_at_price_range"] = node["compareAtPriceRange"]
	result["created_at"] = parseTimestampPtr(getString(node, "createdAt"))
	result["default_cursor"] = getStringPtr(node, "defaultCursor")
	result["description"] = getStringPtr(node, "description")
	result["description_html"] = getStringPtr(node, "descriptionHtml")
	result["handle"] = getStringPtr(node, "handle")
	result["metafields"] = extractNodes(node, "metafields")
	result["options"] = node["options"]
	result["price_range_v2"] = node["priceRangeV2"]
	result["product_type"] = getStringPtr(node, "productType")
	result["published_at"] = parseTimestampPtr(getString(node, "publishedAt"))
	result["requires_selling_plan"] = getBoolPtr(node, "requiresSellingPlan")
	result["status"] = getStringPtr(node, "status")
	result["tags"] = formatTagsAsJSONArrayPtr(node["tags"])
	result["template_suffix"] = getStringPtr(node, "templateSuffix")
	result["title"] = getStringPtr(node, "title")
	result["total_inventory"] = getIntPtr(node, "totalInventory")
	result["tracks_inventory"] = getBoolPtr(node, "tracksInventory")
	result["updated_at"] = parseTimestampPtr(getString(node, "updatedAt"))
	result["variants_count"] = node["variantsCount"]
	result["variants_first250"] = extractNodes(node, "variantsFirst250")
	result["vendor"] = getStringPtr(node, "vendor")

	return result
}

func (s *ShopifySource) readDiscounts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading discounts")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	query := `
query GetDiscounts($first: Int!, $after: String, $query: String) {
  discountNodes(first: $first, after: $after, query: $query, sortKey: UPDATED_AT, reverse: false) {
    edges {
      cursor
      node {
        id
        metafields(first: 250) {
          edges {
            node {
              id
              namespace
              key
              value
              type
            }
          }
        }
        discount {
          ... on DiscountCodeBasic {
            __typename
            appliesOncePerCustomer
            asyncUsageCount
            codes(first: 250) { edges { node { id code } } }
            codesCount { count precision }
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            customerGets {
              appliesOnOneTimePurchase
              appliesOnSubscription
              items {
                __typename
                ... on DiscountProducts {
                  productsFirst250: products(first: 250) { edges { node { id } } }
                  productVariantsFirst250: productVariants(first: 250) { edges { node { id } } }
                }
                ... on DiscountCollections {
                  collectionsFirst250: collections(first: 250) { edges { node { id } } }
                }
                ... on AllDiscountItems { allItems }
              }
              value {
                __typename
                ... on DiscountPercentage { percentage }
                ... on DiscountAmount { amount { amount currencyCode } appliesOnEachItem }
              }
            }
            customerSelection {
              __typename
              ... on DiscountCustomerAll { allCustomers }
            }
            discountClass
            endsAt
            hasTimelineComment
            minimumRequirement {
              __typename
              ... on DiscountMinimumQuantity { greaterThanOrEqualToQuantity }
              ... on DiscountMinimumSubtotal { greaterThanOrEqualToSubtotal { amount currencyCode } }
            }
            recurringCycleLimit
            shareableUrls { url title }
            shortSummary
            startsAt
            status
            summary
            title
            totalSales { amount currencyCode }
            updatedAt
            usageLimit
          }
          ... on DiscountCodeBxgy {
            __typename
            asyncUsageCount
            codes(first: 250) { edges { node { id code } } }
            codesCount { count precision }
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            discountClass
            endsAt
            hasTimelineComment
            startsAt
            status
            summary
            title
            totalSales { amount currencyCode }
            updatedAt
            usageLimit
          }
          ... on DiscountCodeFreeShipping {
            __typename
            appliesOncePerCustomer
            asyncUsageCount
            codes(first: 250) { edges { node { id code } } }
            codesCount { count precision }
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            discountClass
            endsAt
            hasTimelineComment
            startsAt
            status
            summary
            title
            totalSales { amount currencyCode }
            updatedAt
            usageLimit
          }
          ... on DiscountAutomaticBasic {
            __typename
            asyncUsageCount
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            customerGets {
              appliesOnOneTimePurchase
              appliesOnSubscription
              items {
                __typename
                ... on DiscountProducts {
                  productsFirst250: products(first: 250) { edges { node { id } } }
                  productVariantsFirst250: productVariants(first: 250) { edges { node { id } } }
                }
                ... on DiscountCollections {
                  collectionsFirst250: collections(first: 250) { edges { node { id } } }
                }
                ... on AllDiscountItems { allItems }
              }
              value {
                __typename
                ... on DiscountPercentage { percentage }
                ... on DiscountAmount { amount { amount currencyCode } appliesOnEachItem }
              }
            }
            discountClass
            endsAt
            shortSummary
            startsAt
            status
            summary
            title
            updatedAt
          }
          ... on DiscountAutomaticBxgy {
            __typename
            asyncUsageCount
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            discountClass
            endsAt
            startsAt
            status
            summary
            title
            updatedAt
          }
          ... on DiscountAutomaticFreeShipping {
            __typename
            asyncUsageCount
            combinesWith { orderDiscounts productDiscounts shippingDiscounts }
            createdAt
            discountClass
            endsAt
            startsAt
            status
            summary
            title
            updatedAt
          }
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

	return s.paginateAndSend(ctx, opts, results, query, "discountNodes", pageSize, discountFields, s.transformDiscount)
}

func (s *ShopifySource) transformDiscount(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getStringPtr(node, "id")

	if discount, ok := node["discount"].(map[string]interface{}); ok {
		result["updated_at"] = parseTimestampPtr(getString(discount, "updatedAt"))
	}
	result["discount"] = normalizeUTCTimestampsAtKeys(
		flattenGraphQLEdges(node["discount"]),
		map[string]bool{"createdAt": true, "updatedAt": true},
	)

	metafields := flattenGraphQLEdges(node["metafields"])
	if metafields == nil {
		metafields = []interface{}{}
	}
	result["metafields_first250"] = metafields

	return result
}

func (s *ShopifySource) readInventoryItems(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading inventory items")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	query := `
query GetInventoryItems($first: Int!, $after: String, $query: String) {
  inventoryItems(first: $first, after: $after, query: $query) {
    edges {
      cursor
      node {
        id
        legacyResourceId
        sku
        tracked
        trackedEditable { locked reason }
        duplicateSkuCount
        requiresShipping
        createdAt
        updatedAt
        unitCost { amount currencyCode }
        countryCodeOfOrigin
        provinceCodeOfOrigin
        harmonizedSystemCode
        measurement {
          id
          weight { unit value }
        }
        inventoryLevels(first: 50) {
          edges {
            node {
              id
              quantities(names: ["available", "committed", "damaged", "incoming", "on_hand", "quality_control", "reserved", "safety_stock"]) {
                name
                quantity
              }
              location { id name }
            }
          }
        }
        variant {
          id
          legacyResourceId
          title
          sku
          barcode
          price
          compareAtPrice
          position
          inventoryQuantity
          availableForSale
          taxable
          taxCode
          requiresComponents
          createdAt
          updatedAt
          inventoryPolicy
          selectedOptions { name value }
          sellableOnlineQuantity
          sellingPlanGroupsCount { count precision }
          product { id }
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`

	return s.paginateAndSend(ctx, opts, results, query, "inventoryItems", pageSize, inventoryItemFields, s.transformInventoryItem)
}

func (s *ShopifySource) transformInventoryItem(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getStringPtr(node, "id")
	result["created_at"] = parseTimestampPtr(getString(node, "createdAt"))
	result["duplicate_sku_count"] = getIntPtr(node, "duplicateSkuCount")
	result["legacy_resource_id"] = getStringPtr(node, "legacyResourceId")
	result["measurement"] = node["measurement"]
	result["requires_shipping"] = getBoolPtr(node, "requiresShipping")
	result["sku"] = getStringPtr(node, "sku")
	result["tracked"] = getBoolPtr(node, "tracked")
	result["tracked_editable"] = node["trackedEditable"]
	result["updated_at"] = parseTimestampPtr(getString(node, "updatedAt"))
	result["variant"] = normalizeUTCTimestamps(node["variant"])

	if unitCost, ok := node["unitCost"].(map[string]interface{}); ok {
		result["cost"] = getFloatPtr(unitCost, "amount")
	}
	result["country_code_of_origin"] = getStringPtr(node, "countryCodeOfOrigin")
	result["province_code_of_origin"] = getStringPtr(node, "provinceCodeOfOrigin")
	result["harmonized_system_code"] = getStringPtr(node, "harmonizedSystemCode")
	result["inventory_levels"] = extractEdges(node, "inventoryLevels")

	if legacyID := getString(node, "legacyResourceId"); legacyID != "" && s.store != "" {
		url := fmt.Sprintf("https://%s/admin/products/inventory/%s/inventory_history", s.store, legacyID)
		result["inventory_history_url"] = &url
	}

	return result
}

func (s *ShopifySource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading events")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	endpoint := fmt.Sprintf("/events.json?limit=%d", pageSize)

	if opts.IntervalStart != nil {
		endpoint += "&created_at_min=" + url.QueryEscape(opts.IntervalStart.Format(time.RFC3339))
	}

	if opts.IntervalEnd != nil {
		endpoint += "&created_at_max=" + url.QueryEscape(opts.IntervalEnd.Format(time.RFC3339))
	}

	paginator := gonghttp.NewLinkHeaderPaginator(endpoint)
	totalSent := 0
	batchNum := 0

	for paginator.HasNext() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp struct {
			Events []map[string]interface{} `json:"events"`
		}

		httpResp, err := paginator.NextPage(ctx, s.restClient, &resp)
		if err != nil {
			return fmt.Errorf("failed to fetch events: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("events request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Events) == 0 {
			break
		}

		var items []map[string]interface{}
		for _, event := range resp.Events {
			items = append(items, s.transformEvent(event))
			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, eventFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert events to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending events batch %d with %d records", batchNum, len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
	}

	config.Debug("[SHOPIFY] Finished reading events, total records: %d", totalSent)
	return nil
}

func (s *ShopifySource) transformEvent(event map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getIntPtr(event, "id")
	result["subject_id"] = getIntPtr(event, "subject_id")
	result["created_at"] = parseTimestampPtr(getString(event, "created_at"))
	result["subject_type"] = getStringPtr(event, "subject_type")
	result["verb"] = getStringPtr(event, "verb")
	result["arguments"] = event["arguments"]
	result["message"] = getStringPtr(event, "message")
	result["author"] = getStringPtr(event, "author")
	result["description"] = getStringPtr(event, "description")
	result["path"] = getStringPtr(event, "path")

	return result
}

func (s *ShopifySource) readTransactions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading transactions")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	endpoint := fmt.Sprintf("/shopify_payments/balance/transactions.json?limit=%d", pageSize)

	paginator := gonghttp.NewLinkHeaderPaginator(endpoint)
	totalSent := 0
	batchNum := 0
	stop := false

	for paginator.HasNext() && !stop {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp struct {
			Transactions []map[string]interface{} `json:"transactions"`
		}

		httpResp, err := paginator.NextPage(ctx, s.restClient, &resp)
		if err != nil {
			return fmt.Errorf("failed to fetch transactions: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("transactions request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.Transactions) == 0 {
			break
		}

		var items []map[string]interface{}
		hasInterval := opts.IntervalStart != nil || opts.IntervalEnd != nil
		for _, txn := range resp.Transactions {
			processedAtStr := getString(txn, "processed_at")

			// Results are returned ordered by processed_at DESC (newest first), per Shopify docs.
			// Stop early once we walk past IntervalStart; skip rows newer than IntervalEnd.
			if processedAtStr != "" {
				if pa, err := time.Parse(time.RFC3339, processedAtStr); err == nil {
					if opts.IntervalStart != nil && pa.Before(*opts.IntervalStart) {
						stop = true
						break
					}
					if opts.IntervalEnd != nil && pa.After(*opts.IntervalEnd) {
						continue
					}
				} else if hasInterval {
					config.Debug("[SHOPIFY] transaction id=%v has unparseable processed_at %q; emitted without interval check", txn["id"], processedAtStr)
				}
			} else if hasInterval {
				config.Debug("[SHOPIFY] transaction id=%v has empty processed_at; emitted without interval check", txn["id"])
			}

			item := map[string]interface{}{
				"id":                          getIntPtr(txn, "id"),
				"type":                        getStringPtr(txn, "type"),
				"test":                        getBoolPtr(txn, "test"),
				"payout_id":                   getIntPtr(txn, "payout_id"),
				"payout_status":               getStringPtr(txn, "payout_status"),
				"currency":                    getStringPtr(txn, "currency"),
				"amount":                      getStringPtr(txn, "amount"),
				"fee":                         getStringPtr(txn, "fee"),
				"net":                         getStringPtr(txn, "net"),
				"source_id":                   getIntPtr(txn, "source_id"),
				"source_type":                 getStringPtr(txn, "source_type"),
				"source_order_id":             getIntPtr(txn, "source_order_id"),
				"source_order_transaction_id": getIntPtr(txn, "source_order_transaction_id"),
				"processed_at":                parseTimestampPtr(processedAtStr),
			}
			items = append(items, item)

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, transactionFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert transactions to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending transactions batch %d with %d records", batchNum, len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
	}

	config.Debug("[SHOPIFY] Finished reading transactions, total records: %d", totalSent)
	return nil
}

func (s *ShopifySource) readBalance(ctx context.Context, _ source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading balance")

	var resp struct {
		Balance []map[string]interface{} `json:"balance"`
	}

	httpResp, err := s.restClient.R(ctx).
		SetResult(&resp).
		Get("/shopify_payments/balance.json")
	if err != nil {
		return fmt.Errorf("failed to fetch balance: %w", err)
	}

	if !httpResp.IsSuccess() {
		return fmt.Errorf("balance request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	if len(resp.Balance) == 0 {
		config.Debug("[SHOPIFY] No balance data")
		return nil
	}

	var items []map[string]interface{}
	for _, b := range resp.Balance {
		item := map[string]interface{}{
			"currency": getStringPtr(b, "currency"),
			"amount":   getStringPtr(b, "amount"),
		}
		items = append(items, item)
	}

	normalizeItems(items)
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, balanceFields, nil)
	if err != nil {
		return fmt.Errorf("failed to convert balance to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[SHOPIFY] Finished reading balance, total records: %d", len(items))
	return nil
}

func buildIncrementalQuery(opts source.ReadOptions, timestampField string) string {
	if opts.IntervalStart == nil && opts.IntervalEnd == nil {
		return ""
	}

	var parts []string
	if opts.IntervalStart != nil {
		parts = append(parts, fmt.Sprintf("%s:>='%s'", timestampField, opts.IntervalStart.Format(time.RFC3339)))
	}

	if opts.IntervalEnd != nil {
		parts = append(parts, fmt.Sprintf("%s:<='%s'", timestampField, opts.IntervalEnd.Format(time.RFC3339)))
	}

	return strings.Join(parts, " ")
}

func (s *ShopifySource) paginateAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	query, rootField string,
	pageSize int,
	columns []schema.Column,
	transform func(map[string]interface{}) map[string]interface{},
) error {
	var cursor *string
	totalSent := 0
	batchNum := 0

	timestampField := "updated_at"
	if rootField == "events" {
		timestampField = "created_at"
	}
	filterQuery := buildIncrementalQuery(opts, timestampField)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		variables := map[string]interface{}{
			"first": pageSize,
		}
		if cursor != nil {
			variables["after"] = *cursor
		}
		if filterQuery != "" {
			variables["query"] = filterQuery
		}

		data, err := s.executeGraphQL(ctx, query, variables)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", rootField, err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", rootField, err)
		}

		connectionData, ok := result[rootField].(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected response format for %s", rootField)
		}

		edges, ok := connectionData["edges"].([]interface{})
		if !ok || len(edges) == 0 {
			config.Debug("[SHOPIFY] No more %s to fetch", rootField)
			break
		}

		var items []map[string]interface{}
		for _, edge := range edges {
			edgeMap, ok := edge.(map[string]interface{})
			if !ok {
				continue
			}
			node, ok := edgeMap["node"].(map[string]interface{})
			if !ok {
				continue
			}

			transformed := transform(node)
			items = append(items, transformed)

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, columns, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", rootField, err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending batch %d with %d %s", batchNum, len(items), rootField)
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[SHOPIFY] Reached limit of %d records", opts.Limit)
			break
		}

		pageInfoData, ok := connectionData["pageInfo"].(map[string]interface{})
		if !ok {
			break
		}

		hasNextPage, _ := pageInfoData["hasNextPage"].(bool)
		if !hasNextPage {
			break
		}

		endCursor, ok := pageInfoData["endCursor"].(string)
		if !ok || endCursor == "" {
			break
		}
		cursor = &endCursor
	}

	config.Debug("[SHOPIFY] Finished reading %s, total records: %d", rootField, totalSent)
	return nil
}

// Helper functions for data extraction

func getStringPtr(m map[string]interface{}, key string) *string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}

func getIntPtr(m map[string]interface{}, key string) *int64 {
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			i := int64(val)
			return &i
		case int64:
			return &val
		case int:
			i := int64(val)
			return &i
		}
	}
	return nil
}

func getFloatPtr(m map[string]interface{}, key string) *float64 {
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return &val
		case int64:
			f := float64(val)
			return &f
		case int:
			f := float64(val)
			return &f
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return &f
			}
		}
	}
	return nil
}

func getBoolPtr(m map[string]interface{}, key string) *bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

func parseTimestampPtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extractLegacyIDPtr(node map[string]interface{}) *int64 {
	if legacyID, ok := node["legacyResourceId"].(string); ok && legacyID != "" {
		if id, err := strconv.ParseInt(legacyID, 10, 64); err == nil {
			return &id
		}
	}
	return extractIDFromGIDPtr(getString(node, "id"))
}

func extractIDFromGIDPtr(gid string) *int64 {
	if gid == "" {
		return nil
	}
	parts := strings.Split(gid, "/")
	if len(parts) > 0 {
		if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil {
			return &id
		}
	}
	return nil
}

func extractEdges(node map[string]interface{}, key string) interface{} {
	if conn, ok := node[key].(map[string]interface{}); ok {
		if edges, ok := conn["edges"].([]interface{}); ok {
			var items []interface{}
			for _, edge := range edges {
				if edgeMap, ok := edge.(map[string]interface{}); ok {
					if nodeData, ok := edgeMap["node"]; ok {
						items = append(items, nodeData)
					}
				}
			}
			return items
		}
	}
	if arr, ok := node[key].([]interface{}); ok {
		return arr
	}
	return nil
}

func extractNodes(node map[string]interface{}, key string) interface{} {
	if conn, ok := node[key].(map[string]interface{}); ok {
		if nodes, ok := conn["nodes"].([]interface{}); ok {
			return nodes
		}
	}
	return nil
}

func normalizeItems(items []map[string]interface{}) {
	for _, item := range items {
		for key, val := range item {
			item[key] = derefValue(val)
		}
	}
}

func derefValue(val interface{}) interface{} {
	if val == nil {
		return nil
	}

	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	return rv.Interface()
}

func formatTagsAsJSONArrayPtr(v interface{}) *string {
	arr, ok := v.([]interface{})
	if !ok {
		if s, ok := v.(string); ok && s != "" {
			return &s
		}
		return nil
	}
	strs := make([]string, 0, len(arr))
	for _, t := range arr {
		if s, ok := t.(string); ok {
			strs = append(strs, s)
		}
	}
	bytes, err := json.Marshal(strs)
	if err != nil {
		return nil
	}
	s := string(bytes)
	return &s
}

func formatTagsPtr(v interface{}) *string {
	if v == nil {
		return nil
	}
	if tags, ok := v.([]interface{}); ok {
		if len(tags) == 0 {
			return nil
		}
		var strs []string
		for _, t := range tags {
			if s, ok := t.(string); ok {
				strs = append(strs, s)
			}
		}
		if len(strs) == 0 {
			return nil
		}
		result := strings.Join(strs, ", ")
		return &result
	}
	if s, ok := v.(string); ok && s != "" {
		return &s
	}
	return nil
}

func (s *ShopifySource) readPriceRules(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[SHOPIFY] Reading price_rules")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultBatch
	}

	endpoint := fmt.Sprintf("/price_rules.json?limit=%d&order=updated_at+asc", pageSize)

	if opts.IntervalStart != nil {
		endpoint += "&updated_at_min=" + url.QueryEscape(opts.IntervalStart.Format(time.RFC3339))
	}

	if opts.IntervalEnd != nil {
		endpoint += "&updated_at_max=" + url.QueryEscape(opts.IntervalEnd.Format(time.RFC3339))
	}

	paginator := gonghttp.NewLinkHeaderPaginator(endpoint)
	totalSent := 0
	batchNum := 0

	for paginator.HasNext() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var resp struct {
			PriceRules []map[string]interface{} `json:"price_rules"`
		}

		httpResp, err := paginator.NextPage(ctx, s.restClient, &resp)
		if err != nil {
			return fmt.Errorf("failed to fetch price_rules: %w", err)
		}

		if !httpResp.IsSuccess() {
			return fmt.Errorf("price_rules request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
		}

		if len(resp.PriceRules) == 0 {
			break
		}

		var items []map[string]interface{}
		for _, pr := range resp.PriceRules {
			items = append(items, s.transformPriceRule(pr))

			if opts.Limit > 0 && totalSent+len(items) >= opts.Limit {
				break
			}
		}

		if len(items) > 0 {
			normalizeItems(items)
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, priceRuleFields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert price_rules to Arrow: %w", err)
			}

			batchNum++
			config.Debug("[SHOPIFY] Sending price_rules batch %d with %d records", batchNum, len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}
	}

	config.Debug("[SHOPIFY] Finished reading price_rules, total records: %d", totalSent)
	return nil
}

func (s *ShopifySource) transformPriceRule(pr map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getIntPtr(pr, "id")
	result["admin_graphql_api_id"] = getStringPtr(pr, "admin_graphql_api_id")
	result["title"] = getStringPtr(pr, "title")
	result["value_type"] = getStringPtr(pr, "value_type")
	result["value"] = getStringPtr(pr, "value")
	result["customer_selection"] = getStringPtr(pr, "customer_selection")
	result["target_type"] = getStringPtr(pr, "target_type")
	result["target_selection"] = getStringPtr(pr, "target_selection")
	result["allocation_method"] = getStringPtr(pr, "allocation_method")
	result["allocation_limit"] = getIntPtr(pr, "allocation_limit")
	result["once_per_customer"] = getBoolPtr(pr, "once_per_customer")
	result["usage_limit"] = getIntPtr(pr, "usage_limit")
	result["starts_at"] = parseTimestampPtr(getString(pr, "starts_at"))
	result["ends_at"] = parseTimestampPtr(getString(pr, "ends_at"))
	result["created_at"] = parseTimestampPtr(getString(pr, "created_at"))
	result["updated_at"] = parseTimestampPtr(getString(pr, "updated_at"))
	result["entitled_product_ids"] = pr["entitled_product_ids"]
	result["entitled_variant_ids"] = pr["entitled_variant_ids"]
	result["entitled_collection_ids"] = pr["entitled_collection_ids"]
	result["entitled_country_ids"] = pr["entitled_country_ids"]
	result["prerequisite_product_ids"] = pr["prerequisite_product_ids"]
	result["prerequisite_variant_ids"] = pr["prerequisite_variant_ids"]
	result["prerequisite_collection_ids"] = pr["prerequisite_collection_ids"]
	result["customer_segment_prerequisite_ids"] = pr["customer_segment_prerequisite_ids"]
	result["prerequisite_customer_ids"] = pr["prerequisite_customer_ids"]
	result["prerequisite_subtotal_range"] = pr["prerequisite_subtotal_range"]
	result["prerequisite_quantity_range"] = pr["prerequisite_quantity_range"]
	result["prerequisite_shipping_price_range"] = pr["prerequisite_shipping_price_range"]
	result["prerequisite_to_entitlement_quantity_ratio"] = pr["prerequisite_to_entitlement_quantity_ratio"]
	result["prerequisite_to_entitlement_purchase"] = pr["prerequisite_to_entitlement_purchase"]

	return result
}

// flattenGraphQLEdges walks v and unwraps GraphQL connection objects of shape
// {"edges":[{"node":X}, ...]} to a flat array of nodes [X, X, ...], matching
// ingestr serialization. Returns [] (not nil) for empty connections.
func flattenGraphQLEdges(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		if edges, ok := x["edges"].([]interface{}); ok {
			out := make([]interface{}, 0, len(edges))
			canFlatten := true
			for _, e := range edges {
				em, ok := e.(map[string]interface{})
				if !ok {
					canFlatten = false
					break
				}
				node, ok := em["node"]
				if !ok {
					canFlatten = false
					break
				}
				out = append(out, flattenGraphQLEdges(node))
			}
			if canFlatten {
				return out
			}
		}
		for k, val := range x {
			x[k] = flattenGraphQLEdges(val)
		}
		return x
	case []interface{}:
		for i, val := range x {
			x[i] = flattenGraphQLEdges(val)
		}
		return x
	default:
		return x
	}
}

// normalizeUTCTimestamps walks v, parses any ISO-8601 timestamp strings, and
// re-emits them in UTC with the "+00:00" suffix to match ingestr's format.
// Handles inputs ending in "Z" or any timezone offset (e.g. "-05:00").
func normalizeUTCTimestamps(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			x[k] = normalizeUTCTimestamps(val)
		}
		return x
	case []interface{}:
		for i, val := range x {
			x[i] = normalizeUTCTimestamps(val)
		}
		return x
	case string:
		if len(x) >= 20 && x[10] == 'T' {
			if t, err := time.Parse(time.RFC3339, x); err == nil {
				return t.UTC().Format("2006-01-02T15:04:05") + "+00:00"
			}
		}
		return x
	default:
		return x
	}
}

// normalizeUTCTimestampsAtKeys walks v and converts ISO-8601 timestamp strings
// to UTC "+00:00" form ONLY when the field key is in keys. Used to mirror
// ingestr's selective normalization (e.g. createdAt/updatedAt are converted
// but user-set fields like startsAt/endsAt are left as-is).
func normalizeUTCTimestampsAtKeys(v interface{}, keys map[string]bool) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			if keys[k] {
				if s, ok := val.(string); ok && len(s) >= 20 && s[10] == 'T' {
					if t, err := time.Parse(time.RFC3339, s); err == nil {
						x[k] = t.UTC().Format("2006-01-02T15:04:05") + "+00:00"
						continue
					}
				}
			}
			x[k] = normalizeUTCTimestampsAtKeys(val, keys)
		}
		return x
	case []interface{}:
		for i, val := range x {
			x[i] = normalizeUTCTimestampsAtKeys(val, keys)
		}
		return x
	default:
		return x
	}
}

var _ source.Source = (*ShopifySource)(nil)
