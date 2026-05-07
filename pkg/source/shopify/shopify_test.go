package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseShopifyURI(t *testing.T) {
	tests := []struct {
		name             string
		uri              string
		wantStore        string
		wantKey          string
		wantClientID     string
		wantClientSecret string
		wantErr          bool
	}{
		{
			name:      "full store URL with api_key",
			uri:       "shopify://my-store.myshopify.com?api_key=shpkey_12345",
			wantStore: "my-store.myshopify.com",
			wantKey:   "shpkey_12345",
			wantErr:   false,
		},
		{
			name:      "short store name (auto-append myshopify.com)",
			uri:       "shopify://my-store?api_key=shpkey_12345",
			wantStore: "my-store.myshopify.com",
			wantKey:   "shpkey_12345",
			wantErr:   false,
		},
		{
			name:             "client credentials",
			uri:              "shopify://my-store?client_id=my_id&client_secret=my_secret",
			wantStore:        "my-store.myshopify.com",
			wantClientID:     "my_id",
			wantClientSecret: "my_secret",
			wantErr:          false,
		},
		{
			name:    "missing all auth params",
			uri:     "shopify://my-store.myshopify.com",
			wantErr: true,
		},
		{
			name:    "empty api_key without client credentials",
			uri:     "shopify://my-store.myshopify.com?api_key=",
			wantErr: true,
		},
		{
			name:    "client_id without client_secret",
			uri:     "shopify://my-store.myshopify.com?client_id=my_id",
			wantErr: true,
		},
		{
			name:    "client_secret without client_id",
			uri:     "shopify://my-store.myshopify.com?client_secret=my_secret",
			wantErr: true,
		},
		{
			name:    "missing store",
			uri:     "shopify://?api_key=shpkey_12345",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			uri:     "postgres://my-store?api_key=shpkey_12345",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := parseShopifyURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStore, params.store)
			assert.Equal(t, tt.wantKey, params.apiKey)
			assert.Equal(t, tt.wantClientID, params.clientID)
			assert.Equal(t, tt.wantClientSecret, params.clientSecret)
		})
	}
}

func TestShopifySource_Schemes(t *testing.T) {
	s := NewShopifySource()
	schemes := s.Schemes()
	assert.Equal(t, []string{"shopify"}, schemes)
}

func TestShopifySource_GetTable(t *testing.T) {
	s := NewShopifySource()

	tests := []struct {
		table          string
		wantErr        bool
		wantColumns    int
		wantPrimaryKey []string
	}{
		{"orders", false, len(orderFields), []string{"id"}},
		{"customers", false, len(customerFields), []string{"id"}},
		{"products", false, len(productFields), []string{"id"}},
		{"discounts", false, len(discountFields), []string{"id"}},
		{"inventory_items", false, len(inventoryItemFields), []string{"id"}},
		{"events", false, len(eventFields), []string{"id"}},
		{"transactions", false, len(transactionFields), []string{"id"}},
		{"balance", false, len(balanceFields), []string{"currency"}},
		{"invalid", true, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, table)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, table)

			schema, err := table.GetSchema(context.Background())
			require.NoError(t, err)
			assert.Len(t, schema.Columns, tt.wantColumns)
			assert.Equal(t, tt.table, schema.Name)
			assert.Equal(t, tt.wantPrimaryKey, table.PrimaryKeys())
		})
	}
}

func TestIsValidTable(t *testing.T) {
	tests := []struct {
		table string
		valid bool
	}{
		{"orders", true},
		{"customers", true},
		{"products", true},
		{"discounts", true},
		{"inventory_items", true},
		{"events", true},
		{"transactions", true},
		{"balance", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidTable(tt.table))
		})
	}
}

func TestBuildIncrementalQuery(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	later := time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		opts   source.ReadOptions
		field  string
		expect string
	}{
		{
			name:   "no interval",
			opts:   source.ReadOptions{},
			field:  "updated_at",
			expect: "",
		},
		{
			name:   "start only",
			opts:   source.ReadOptions{IntervalStart: &now},
			field:  "updated_at",
			expect: "updated_at:>='2024-01-15T12:00:00Z'",
		},
		{
			name:   "end only",
			opts:   source.ReadOptions{IntervalEnd: &later},
			field:  "updated_at",
			expect: "updated_at:<='2024-01-20T12:00:00Z'",
		},
		{
			name:   "both start and end",
			opts:   source.ReadOptions{IntervalStart: &now, IntervalEnd: &later},
			field:  "updated_at",
			expect: "updated_at:>='2024-01-15T12:00:00Z' updated_at:<='2024-01-20T12:00:00Z'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildIncrementalQuery(tt.opts, tt.field)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestTransformOrder(t *testing.T) {
	s := NewShopifySource()

	order := map[string]interface{}{
		"id":                   float64(123),
		"admin_graphql_api_id": "gid://shopify/Order/123",
		"name":                 "#1001",
		"email":                "test@example.com",
		"created_at":           "2024-01-15T12:00:00Z",
		"updated_at":           "2024-01-15T12:00:00Z",
		"currency":             "USD",
		"financial_status":     "paid",
		"fulfillment_status":   "unfulfilled",
		"confirmed":            true,
		"test":                 false,
		"order_number":         float64(1001),
		"total_price":          "100.00",
	}

	result := s.transformOrder(order)

	require.NotNil(t, result["id"])
	assert.Equal(t, int64(123), *result["id"].(*int64))
	require.NotNil(t, result["admin_graphql_api_id"])
	assert.Equal(t, "gid://shopify/Order/123", *result["admin_graphql_api_id"].(*string))
	require.NotNil(t, result["name"])
	assert.Equal(t, "#1001", *result["name"].(*string))
	require.NotNil(t, result["order_number"])
	assert.Equal(t, int64(1001), *result["order_number"].(*int64))
	require.NotNil(t, result["email"])
	assert.Equal(t, "test@example.com", *result["email"].(*string))
	require.NotNil(t, result["currency"])
	assert.Equal(t, "USD", *result["currency"].(*string))
	require.NotNil(t, result["financial_status"])
	assert.Equal(t, "paid", *result["financial_status"].(*string))
	require.NotNil(t, result["fulfillment_status"])
	assert.Equal(t, "unfulfilled", *result["fulfillment_status"].(*string))
	require.NotNil(t, result["confirmed"])
	assert.Equal(t, true, *result["confirmed"].(*bool))
	require.NotNil(t, result["test"])
	assert.Equal(t, false, *result["test"].(*bool))
	require.NotNil(t, result["total_price"])
	assert.Equal(t, "100.00", result["total_price"].(string))

	createdAt, ok := result["created_at"].(*time.Time)
	require.True(t, ok)
	require.NotNil(t, createdAt)
	assert.Equal(t, 2024, createdAt.Year())
	assert.Equal(t, time.Month(1), createdAt.Month())
	assert.Equal(t, 15, createdAt.Day())
}

func TestTransformCustomer(t *testing.T) {
	s := NewShopifySource()

	customer := map[string]interface{}{
		"id":                   float64(456),
		"admin_graphql_api_id": "gid://shopify/Customer/456",
		"email":                "customer@example.com",
		"first_name":           "John",
		"last_name":            "Doe",
		"created_at":           "2024-01-10T08:00:00Z",
		"updated_at":           "2024-01-12T10:30:00Z",
		"state":                "enabled",
		"orders_count":         float64(5),
		"total_spent":          "500.00",
		"currency":             "EUR",
	}

	result := s.transformCustomer(customer)

	require.NotNil(t, result["id"])
	assert.Equal(t, int64(456), *result["id"].(*int64))
	require.NotNil(t, result["admin_graphql_api_id"])
	assert.Equal(t, "gid://shopify/Customer/456", *result["admin_graphql_api_id"].(*string))
	require.NotNil(t, result["email"])
	assert.Equal(t, "customer@example.com", *result["email"].(*string))
	require.NotNil(t, result["first_name"])
	assert.Equal(t, "John", *result["first_name"].(*string))
	require.NotNil(t, result["last_name"])
	assert.Equal(t, "Doe", *result["last_name"].(*string))
	require.NotNil(t, result["state"])
	assert.Equal(t, "enabled", *result["state"].(*string))
	require.NotNil(t, result["orders_count"])
	assert.Equal(t, int64(5), *result["orders_count"].(*int64))
	require.NotNil(t, result["total_spent"])
	assert.Equal(t, "500.00", *result["total_spent"].(*string))
	require.NotNil(t, result["currency"])
	assert.Equal(t, "EUR", *result["currency"].(*string))
}

func TestTransformProduct(t *testing.T) {
	s := NewShopifySource()

	node := map[string]interface{}{
		"id":                  "gid://shopify/Product/789",
		"title":               "Test Product",
		"handle":              "test-product",
		"description":         "A great product",
		"descriptionHtml":     "<p>A great product</p>",
		"vendor":              "Test Vendor",
		"productType":         "Clothing",
		"status":              "ACTIVE",
		"tags":                []interface{}{"tag1", "tag2"},
		"createdAt":           "2024-01-05T15:00:00Z",
		"updatedAt":           "2024-01-08T09:30:00Z",
		"publishedAt":         "2024-01-06T10:00:00Z",
		"defaultCursor":       "abc123",
		"totalInventory":      float64(42),
		"tracksInventory":     true,
		"requiresSellingPlan": false,
		"variantsCount":       map[string]interface{}{"count": float64(3), "precision": "EXACT"},
		"metafields": map[string]interface{}{
			"nodes": []interface{}{
				map[string]interface{}{"id": "gid://shopify/Metafield/1", "key": "k", "value": "v"},
			},
		},
		"variantsFirst250": map[string]interface{}{
			"nodes": []interface{}{
				map[string]interface{}{"id": "gid://shopify/ProductVariant/1", "sku": "SKU1"},
			},
		},
	}

	result := s.transformProduct(node)

	require.NotNil(t, result["id"])
	assert.Equal(t, "gid://shopify/Product/789", *result["id"].(*string))
	require.NotNil(t, result["title"])
	assert.Equal(t, "Test Product", *result["title"].(*string))
	require.NotNil(t, result["handle"])
	assert.Equal(t, "test-product", *result["handle"].(*string))
	require.NotNil(t, result["description"])
	assert.Equal(t, "A great product", *result["description"].(*string))
	require.NotNil(t, result["description_html"])
	assert.Equal(t, "<p>A great product</p>", *result["description_html"].(*string))
	require.NotNil(t, result["vendor"])
	assert.Equal(t, "Test Vendor", *result["vendor"].(*string))
	require.NotNil(t, result["product_type"])
	assert.Equal(t, "Clothing", *result["product_type"].(*string))
	require.NotNil(t, result["status"])
	assert.Equal(t, "ACTIVE", *result["status"].(*string))
	require.NotNil(t, result["tags"])
	assert.Equal(t, `["tag1","tag2"]`, *result["tags"].(*string))
	require.NotNil(t, result["default_cursor"])
	assert.Equal(t, "abc123", *result["default_cursor"].(*string))
	require.NotNil(t, result["total_inventory"])
	assert.Equal(t, int64(42), *result["total_inventory"].(*int64))
	require.NotNil(t, result["tracks_inventory"])
	assert.Equal(t, true, *result["tracks_inventory"].(*bool))
	require.NotNil(t, result["requires_selling_plan"])
	assert.Equal(t, false, *result["requires_selling_plan"].(*bool))
	require.NotNil(t, result["metafields"])
	assert.Len(t, result["metafields"].([]interface{}), 1)
	require.NotNil(t, result["variants_first250"])
	assert.Len(t, result["variants_first250"].([]interface{}), 1)
}

func TestShopifySource_ReadWithMockServer(t *testing.T) {
	mockResponse := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{
				"id":                   float64(1),
				"admin_graphql_api_id": "gid://shopify/Order/1",
				"name":                 "#1001",
				"email":                "test@example.com",
				"created_at":           "2024-01-15T12:00:00Z",
				"updated_at":           "2024-01-15T12:00:00Z",
				"currency":             "USD",
				"financial_status":     "paid",
				"fulfillment_status":   "unfulfilled",
				"confirmed":            true,
				"test":                 false,
				"order_number":         float64(1001),
				"total_price":          "100.00",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "test-token", r.Header.Get("X-Shopify-Access-Token"))

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mockResponse); err != nil {
			t.Logf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	s := &ShopifySource{
		store:       "test-store.myshopify.com",
		apiKey:      "test-token",
		baseURL:     server.URL,
		restBaseURL: server.URL,
	}

	s.client = createTestClient(server.URL, s.apiKey)
	s.restClient = createTestClient(server.URL, s.apiKey)

	ctx := context.Background()
	table, err := s.GetTable(ctx, source.TableRequest{Name: "orders"})
	require.NoError(t, err)

	results, err := table.Read(ctx, source.ReadOptions{
		PageSize: 10,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for result := range results {
		batches = append(batches, result)
	}

	require.Len(t, batches, 1)
	assert.NoError(t, batches[0].Err)
	assert.NotNil(t, batches[0].Batch)
	assert.Equal(t, int64(1), batches[0].Batch.NumRows())

	record := batches[0].Batch
	idx := columnIndex(record, "order_number")
	require.NotEqual(t, -1, idx)
	col := record.Column(idx).(*array.Int64)
	assert.False(t, col.IsNull(0))
	assert.Equal(t, int64(1001), col.Value(0))
}

func TestShopifySource_InvalidTable(t *testing.T) {
	s := NewShopifySource()
	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "invalid_table"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported table")
}

func createTestClient(baseURL, apiKey string) *gonghttp.Client {
	return gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(10*time.Second),
		gonghttp.WithAuth(gonghttp.NewAPIKeyAuth("X-Shopify-Access-Token", apiKey, true)),
	)
}

func columnIndex(record arrow.RecordBatch, name string) int {
	for i := 0; i < int(record.NumCols()); i++ {
		if record.ColumnName(i) == name {
			return i
		}
	}
	return -1
}

func TestParseTimestampPtr(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectNil bool
	}{
		{
			name:      "valid RFC3339",
			input:     "2024-01-15T12:00:00Z",
			expectNil: false,
		},
		{
			name:      "empty string",
			input:     "",
			expectNil: true,
		},
		{
			name:      "invalid format",
			input:     "not-a-date",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimestampPtr(tt.input)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestFormatTagsPtr(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		expect    *string
		expectNil bool
	}{
		{
			name:      "array of tags",
			input:     []interface{}{"tag1", "tag2", "tag3"},
			expect:    stringPtr("tag1, tag2, tag3"),
			expectNil: false,
		},
		{
			name:      "string tags",
			input:     "already a string",
			expect:    stringPtr("already a string"),
			expectNil: false,
		},
		{
			name:      "nil",
			input:     nil,
			expectNil: true,
		},
		{
			name:      "empty array",
			input:     []interface{}{},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTagsPtr(tt.input)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expect, *result)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestExtractLegacyIDPtr(t *testing.T) {
	tests := []struct {
		name      string
		node      map[string]interface{}
		expect    *int64
		expectNil bool
	}{
		{
			name: "with legacyResourceId",
			node: map[string]interface{}{
				"id":               "gid://shopify/Order/123456789",
				"legacyResourceId": "123456789",
			},
			expect:    int64Ptr(123456789),
			expectNil: false,
		},
		{
			name: "fallback to extracting from GID",
			node: map[string]interface{}{
				"id": "gid://shopify/Customer/987654321",
			},
			expect:    int64Ptr(987654321),
			expectNil: false,
		},
		{
			name:      "empty node",
			node:      map[string]interface{}{},
			expectNil: true,
		},
		{
			name: "invalid legacyResourceId",
			node: map[string]interface{}{
				"id":               "gid://shopify/Product/555",
				"legacyResourceId": "not-a-number",
			},
			expect:    int64Ptr(555),
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractLegacyIDPtr(tt.node)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expect, *result)
			}
		})
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}

func TestExtractIDFromGIDPtr(t *testing.T) {
	tests := []struct {
		name      string
		gid       string
		expect    *int64
		expectNil bool
	}{
		{
			name:      "valid GID",
			gid:       "gid://shopify/Order/123456789",
			expect:    int64Ptr(123456789),
			expectNil: false,
		},
		{
			name:      "empty GID",
			gid:       "",
			expectNil: true,
		},
		{
			name:      "invalid GID format",
			gid:       "not-a-valid-gid",
			expectNil: true,
		},
		{
			name:      "GID with non-numeric ID",
			gid:       "gid://shopify/Product/abc",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIDFromGIDPtr(tt.gid)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expect, *result)
			}
		})
	}
}

func newTransactionsTestSource(serverURL, token string) *ShopifySource {
	s := &ShopifySource{
		store:       "test-store.myshopify.com",
		apiKey:      token,
		baseURL:     serverURL,
		restBaseURL: serverURL,
	}
	s.client = createTestClient(serverURL, token)
	s.restClient = createTestClient(serverURL, token)
	return s
}

func makeTxn(id int64, processedAt, amount string) map[string]interface{} {
	return map[string]interface{}{
		"id":           float64(id),
		"type":         "charge",
		"test":         false,
		"currency":     "USD",
		"amount":       amount,
		"fee":          "1.00",
		"net":          "9.00",
		"processed_at": processedAt,
	}
}

func collectTransactionAmounts(t *testing.T, batches []source.RecordBatchResult) []string {
	t.Helper()
	var amounts []string
	for _, b := range batches {
		require.NoError(t, b.Err)
		require.NotNil(t, b.Batch)
		idx := columnIndex(b.Batch, "amount")
		require.NotEqual(t, -1, idx, "amount column missing")
		col, ok := b.Batch.Column(idx).(*array.String)
		require.True(t, ok, "amount column should be Arrow String, got %T", b.Batch.Column(idx))
		for i := 0; i < col.Len(); i++ {
			if col.IsNull(i) {
				amounts = append(amounts, "")
				continue
			}
			amounts = append(amounts, col.Value(i))
		}
	}
	return amounts
}

func TestReadTransactions_IntervalEndSkipsNewer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]interface{}{"transactions": []interface{}{
			makeTxn(1, "2024-03-15T12:00:00Z", "10.00"), // newer than IntervalEnd -> skipped
			makeTxn(2, "2024-03-10T12:00:00Z", "20.00"), // in range
			makeTxn(3, "2024-03-05T12:00:00Z", "30.00"), // in range
		}}
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	defer server.Close()

	s := newTransactionsTestSource(server.URL, "test-token")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "transactions"})
	require.NoError(t, err)

	end := time.Date(2024, 3, 12, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(ctx, source.ReadOptions{
		PageSize:    10,
		IntervalEnd: &end,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}

	assert.Equal(t, []string{"20.00", "30.00"}, collectTransactionAmounts(t, batches))
}

func TestReadTransactions_IntervalStartStopsEarly(t *testing.T) {
	var requests int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")

		switch count {
		case 1:
			body := map[string]interface{}{"transactions": []interface{}{
				makeTxn(1, "2024-03-15T12:00:00Z", "10.00"), // in range
				makeTxn(2, "2024-03-01T12:00:00Z", "20.00"), // older than IntervalStart -> stop
				makeTxn(3, "2024-02-25T12:00:00Z", "30.00"), // never reached
			}}
			// Server still advertises a next page; the source must stop client-side
			// without following it because the page already crossed IntervalStart.
			w.Header().Set("Link", fmt.Sprintf(`<%s/shopify_payments/balance/transactions.json?limit=10&page_info=should_not_fetch>; rel="next"`, server.URL))
			require.NoError(t, json.NewEncoder(w).Encode(body))
		default:
			t.Fatalf("source must stop paginating once a row is older than IntervalStart; got request %d", count)
		}
	}))
	defer server.Close()

	s := newTransactionsTestSource(server.URL, "test-token")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "transactions"})
	require.NoError(t, err)

	start := time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC)
	results, err := table.Read(ctx, source.ReadOptions{
		PageSize:      10,
		IntervalStart: &start,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}

	assert.Equal(t, []string{"10.00"}, collectTransactionAmounts(t, batches))
	assert.Equal(t, int32(1), atomic.LoadInt32(&requests), "source must not fetch the next page once an older-than-IntervalStart row is seen")
}

// TestReadTransactions_IntervalHourPrecision verifies the interval filter compares full
// timestamps (not just dates), and that IntervalStart/IntervalEnd are inclusive boundaries.
func TestReadTransactions_IntervalHourPrecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// All rows on the same UTC day, sorted DESC by processed_at (Shopify ordering).
		body := map[string]interface{}{"transactions": []interface{}{
			makeTxn(1, "2024-03-10T18:00:00Z", "18.00"), // newer than end (17:00) -> skipped
			makeTxn(2, "2024-03-10T17:00:00Z", "17.00"), // exactly at end -> kept (inclusive)
			makeTxn(3, "2024-03-10T15:30:00Z", "15.30"), // inside window -> kept
			makeTxn(4, "2024-03-10T10:00:00Z", "10.00"), // exactly at start -> kept (inclusive)
			makeTxn(5, "2024-03-10T09:59:59Z", "09.59"), // 1s older than start -> stop
			makeTxn(6, "2024-03-10T08:00:00Z", "08.00"), // never reached
		}}
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	defer server.Close()

	s := newTransactionsTestSource(server.URL, "test-token")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "transactions"})
	require.NoError(t, err)

	start := time.Date(2024, 3, 10, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 10, 17, 0, 0, 0, time.UTC)
	results, err := table.Read(ctx, source.ReadOptions{
		PageSize:      10,
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}

	assert.Equal(t, []string{"17.00", "15.30", "10.00"}, collectTransactionAmounts(t, batches))
}

// TestReadTransactions_EmitsRowsWithUnparseableProcessedAt verifies fail-open behavior:
// rows with empty or malformed processed_at must still be emitted when an interval is set,
// rather than silently dropped. The merge strategy on `id` deduplicates on re-runs.
func TestReadTransactions_EmitsRowsWithUnparseableProcessedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]interface{}{"transactions": []interface{}{
			makeTxn(1, "2024-03-10T15:00:00Z", "15.00"), // in window -> kept
			makeTxn(2, "not-a-timestamp", "99.00"),      // unparseable -> kept (fail-open)
			makeTxn(3, "", "88.00"),                     // empty -> kept (fail-open)
			makeTxn(4, "2024-03-10T11:00:00Z", "11.00"), // in window -> kept
		}}
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	defer server.Close()

	s := newTransactionsTestSource(server.URL, "test-token")
	ctx := context.Background()

	table, err := s.GetTable(ctx, source.TableRequest{Name: "transactions"})
	require.NoError(t, err)

	start := time.Date(2024, 3, 10, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 10, 17, 0, 0, 0, time.UTC)
	results, err := table.Read(ctx, source.ReadOptions{
		PageSize:      10,
		IntervalStart: &start,
		IntervalEnd:   &end,
	})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for r := range results {
		batches = append(batches, r)
	}

	assert.Equal(t, []string{"15.00", "99.00", "88.00", "11.00"}, collectTransactionAmounts(t, batches),
		"rows with unparseable/empty processed_at must be emitted even when an interval is set")
}
