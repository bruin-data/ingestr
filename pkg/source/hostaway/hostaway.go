package hostaway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL = "https://api.hostaway.com/v1"
	// Hostaway API: 15 requests per 10 seconds per IP, 20 per 10s per account.
	rateLimit      = 1.2
	rateLimitBurst = 5
	maxPageSize    = 100
	parallelism    = 5
)

var supportedTables = []string{
	"listings",
	"listing_fee_settings",
	"listing_pricing_settings",
	"listing_agreements",
	"listing_calendars",
	"cancellation_policies",
	"cancellation_policies_airbnb",
	"cancellation_policies_marriott",
	"cancellation_policies_vrbo",
	"reservations",
	"finance_fields",
	"reservation_payment_methods",
	"reservation_rental_agreements",
	"conversations",
	"message_templates",
	"bed_types",
	"property_types",
	"countries",
	"account_tax_settings",
	"user_groups",
	"guest_payment_charges",
	"coupons",
	"webhook_reservations",
	"tasks",
}

type tableConfig struct {
	incrementalKey string
	strategy       config.IncrementalStrategy
	primaryKeys    []string
}

var tableConfigs = map[string]tableConfig{
	"listings":             {incrementalKey: "latestActivityOn", strategy: config.StrategyMerge, primaryKeys: []string{"id"}},
	"listing_fee_settings": {incrementalKey: "updatedOn", strategy: config.StrategyMerge, primaryKeys: []string{"id"}},
}

type HostawaySource struct {
	client *gonghttp.Client
	apiKey string
}

func NewHostawaySource() *HostawaySource {
	return &HostawaySource{}
}

func (s *HostawaySource) HandlesIncrementality() bool {
	return true
}

func (s *HostawaySource) Schemes() []string {
	return []string{"hostaway"}
}

func (s *HostawaySource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(apiKey)),
	)

	config.Debug("[HOSTAWAY] Connected successfully")
	return nil
}

func (s *HostawaySource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "hostaway://") {
		return "", fmt.Errorf("invalid hostaway URI: must start with hostaway://")
	}

	rest := strings.TrimPrefix(uri, "hostaway://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in hostaway URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse hostaway URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in hostaway URI: hostaway://?api_key=<token>")
	}

	return apiKey, nil
}

func (s *HostawaySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	tc, hasConfig := tableConfigs[tableName]
	incrementalKey := ""
	strategy := config.StrategyReplace
	primaryKeys := []string(nil)

	if hasConfig {
		incrementalKey = tc.incrementalKey
		strategy = tc.strategy
		primaryKeys = tc.primaryKeys
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("hostaway source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func (s *HostawaySource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "listings":
			err = s.readListings(ctx, opts, results)
		case "listing_fee_settings":
			err = s.readListingFeeSettings(ctx, opts, results)
		case "listing_pricing_settings":
			err = s.readListingPricingSettings(ctx, opts, results)
		case "listing_agreements":
			err = s.readListingAgreements(ctx, opts, results)
		case "listing_calendars":
			err = s.readListingCalendars(ctx, opts, results)
		case "cancellation_policies":
			err = s.readSingleEndpoint(ctx, "/cancellationPolicies", "cancellation_policies", opts, results)
		case "cancellation_policies_airbnb":
			err = s.readSingleEndpoint(ctx, "/cancellationPolicies/airbnb", "cancellation_policies_airbnb", opts, results)
		case "cancellation_policies_marriott":
			err = s.readSingleEndpoint(ctx, "/cancellationPolicies/marriott", "cancellation_policies_marriott", opts, results)
		case "cancellation_policies_vrbo":
			err = s.readSingleEndpoint(ctx, "/cancellationPolicies/vrbo", "cancellation_policies_vrbo", opts, results)
		case "reservations":
			err = s.readPaginatedEndpoint(ctx, "/reservations", "reservations", opts, results)
		case "finance_fields":
			err = s.readFinanceFields(ctx, opts, results)
		case "reservation_payment_methods":
			err = s.readSingleEndpoint(ctx, "/reservations/paymentMethods", "reservation_payment_methods", opts, results)
		case "reservation_rental_agreements":
			err = s.readReservationRentalAgreements(ctx, opts, results)
		case "conversations":
			err = s.readPaginatedEndpoint(ctx, "/conversations", "conversations", opts, results)
		case "message_templates":
			err = s.readSingleEndpoint(ctx, "/messageTemplates", "message_templates", opts, results)
		case "bed_types":
			err = s.readSingleEndpoint(ctx, "/bedTypes", "bed_types", opts, results)
		case "property_types":
			err = s.readSingleEndpoint(ctx, "/propertyTypes", "property_types", opts, results)
		case "countries":
			err = s.readSingleEndpoint(ctx, "/countries", "countries", opts, results)
		case "account_tax_settings":
			err = s.readSingleEndpoint(ctx, "/accountTaxSettings", "account_tax_settings", opts, results)
		case "user_groups":
			err = s.readSingleEndpoint(ctx, "/userGroups", "user_groups", opts, results)
		case "guest_payment_charges":
			err = s.readPaginatedEndpoint(ctx, "/guestPayments/charges", "guest_payment_charges", opts, results)
		case "coupons":
			err = s.readSingleEndpoint(ctx, "/coupons", "coupons", opts, results)
		case "webhook_reservations":
			err = s.readSingleEndpoint(ctx, "/webhooks/reservations", "webhook_reservations", opts, results)
		case "tasks":
			err = s.readSingleEndpoint(ctx, "/tasks", "tasks", opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

func extractResult(data []byte) ([]map[string]any, error) {
	var envelope map[string]any
	if err := jsonUseNumber(data, &envelope); err != nil {
		return nil, err
	}

	resultRaw, ok := envelope["result"]
	if !ok {
		return nil, fmt.Errorf("missing 'result' field in response")
	}

	switch v := resultRaw.(type) {
	case []any:
		items := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items, nil
	case map[string]any:
		return []map[string]any{v}, nil
	default:
		return nil, nil
	}
}

func (s *HostawaySource) readSingleEndpoint(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading %s", label)

	resp, err := s.client.R(ctx).Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", label, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("hostaway %s returned status %d: %s", label, resp.StatusCode(), resp.String())
	}

	items, err := extractResult(resp.Body())
	if err != nil {
		return fmt.Errorf("failed to parse %s response: %w", label, err)
	}

	if len(items) == 0 {
		config.Debug("[HOSTAWAY] no %s found", label)
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[HOSTAWAY] sent %d %s records", len(items), label)
	return nil
}

func (s *HostawaySource) readPaginatedEndpoint(ctx context.Context, endpoint, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading %s", label)

	offset := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("hostaway %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		items, err := extractResult(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(items) == 0 {
			break
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)
		config.Debug("[HOSTAWAY] %s page at offset %d: sent %d records (total: %d)", label, offset, len(items), totalSent)

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	config.Debug("[HOSTAWAY] finished reading %s: %d total records", label, totalSent)
	return nil
}

func filterByInterval(item map[string]any, field string, start, end *time.Time) bool {
	raw, ok := item[field]
	if !ok || raw == nil || raw == "" {
		if start != nil || end != nil {
			id := item["id"]
			fmt.Printf("[HOSTAWAY] WARNING: skipping record (id=%v) with null/missing %s field\n", id, field)
			return false
		}
		return true
	}

	ts, ok := raw.(string)
	if !ok {
		if start != nil || end != nil {
			id := item["id"]
			fmt.Printf("[HOSTAWAY] WARNING: skipping record (id=%v) with unparseable %s field: %v\n", id, field, raw)
			return false
		}
		return true
	}

	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", ts)
		if err != nil {
			id := item["id"]
			fmt.Printf("[HOSTAWAY] WARNING: skipping record (id=%v) with unparseable %s value: %q\n", id, field, ts)
			return false
		}
	}

	if start != nil && t.Before(*start) {
		return false
	}
	if end != nil && t.After(*end) {
		return false
	}
	return true
}

func (s *HostawaySource) readListings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading listings")
	return s.paginateWithClientFilter(ctx, "/listings", "listings", "latestActivityOn", opts, results)
}

func (s *HostawaySource) paginateWithClientFilter(ctx context.Context, endpoint, label, filterField string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	offset := 0
	totalSent := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", label, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("hostaway %s returned status %d: %s", label, resp.StatusCode(), resp.String())
		}

		items, err := extractResult(resp.Body())
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", label, err)
		}

		if len(items) == 0 {
			break
		}

		var filtered []map[string]any
		for _, item := range items {
			if filterByInterval(item, filterField, opts.IntervalStart, opts.IntervalEnd) {
				filtered = append(filtered, item)
			}
		}

		if len(filtered) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(filtered, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
			}

			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(filtered)
			config.Debug("[HOSTAWAY] %s page at offset %d: sent %d/%d records (total: %d)", label, offset, len(filtered), len(items), totalSent)
		}

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	config.Debug("[HOSTAWAY] finished reading %s: %d total records", label, totalSent)
	return nil
}

func (s *HostawaySource) getAllListingIDs(ctx context.Context) ([]json.Number, error) {
	var ids []json.Number
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/listings")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch listings: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("hostaway /listings returned status %d: %s", resp.StatusCode(), resp.String())
		}

		items, err := extractResult(resp.Body())
		if err != nil {
			return nil, fmt.Errorf("failed to parse listings response: %w", err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if id, ok := item["id"].(json.Number); ok {
				ids = append(ids, id)
			}
		}

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	config.Debug("[HOSTAWAY] found %d listings", len(ids))
	return ids, nil
}

func (s *HostawaySource) getAllReservationIDs(ctx context.Context) ([]json.Number, error) {
	var ids []json.Number
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("limit", strconv.Itoa(maxPageSize)).
			SetQueryParam("offset", strconv.Itoa(offset)).
			Get("/reservations")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch reservations: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("hostaway /reservations returned status %d: %s", resp.StatusCode(), resp.String())
		}

		items, err := extractResult(resp.Body())
		if err != nil {
			return nil, fmt.Errorf("failed to parse reservations response: %w", err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if id, ok := item["id"].(json.Number); ok {
				ids = append(ids, id)
			}
		}

		if len(items) < maxPageSize {
			break
		}

		offset += maxPageSize
	}

	config.Debug("[HOSTAWAY] found %d reservations", len(ids))
	return ids, nil
}

func (s *HostawaySource) readListingFeeSettings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading listing_fee_settings")

	listingIDs, err := s.getAllListingIDs(ctx)
	if err != nil {
		return err
	}

	filter := func(items []map[string]any) []map[string]any {
		var filtered []map[string]any
		for _, item := range items {
			if filterByInterval(item, "updatedOn", opts.IntervalStart, opts.IntervalEnd) {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}

	return s.fetchPerResourceParallel(ctx, listingIDs, "/listingFeeSettings/%s", "listing_fee_settings", opts, results, filter)
}

func (s *HostawaySource) readListingPricingSettings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading listing_pricing_settings")

	listingIDs, err := s.getAllListingIDs(ctx)
	if err != nil {
		return err
	}

	return s.fetchPerResourceParallel(ctx, listingIDs, "/listing/pricingSettings/%s", "listing_pricing_settings", opts, results)
}

func (s *HostawaySource) readListingAgreements(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading listing_agreements")

	listingIDs, err := s.getAllListingIDs(ctx)
	if err != nil {
		return err
	}

	return s.fetchPerResourceParallel(ctx, listingIDs, "/listingAgreement/%s", "listing_agreements", opts, results)
}

func (s *HostawaySource) readListingCalendars(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading listing_calendars")

	listingIDs, err := s.getAllListingIDs(ctx)
	if err != nil {
		return err
	}

	return s.fetchPerResourceParallel(ctx, listingIDs, "/listings/%s/calendar", "listing_calendars", opts, results)
}

func (s *HostawaySource) readFinanceFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading finance_fields")

	reservationIDs, err := s.getAllReservationIDs(ctx)
	if err != nil {
		return err
	}

	return s.fetchPerResourceParallel(ctx, reservationIDs, "/financeField/%s", "finance_fields", opts, results)
}

func (s *HostawaySource) readReservationRentalAgreements(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[HOSTAWAY] reading reservation_rental_agreements")

	reservationIDs, err := s.getAllReservationIDs(ctx)
	if err != nil {
		return err
	}

	return s.fetchPerResourceParallel(ctx, reservationIDs, "/reservations/%s/rentalAgreement", "reservation_rental_agreements", opts, results)
}

func (s *HostawaySource) fetchPerResourceParallel(ctx context.Context, ids []json.Number, endpointPattern, label string, opts source.ReadOptions, results chan<- source.RecordBatchResult, filters ...func([]map[string]any) []map[string]any) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	idCh := make(chan json.Number, len(ids))
	for _, id := range ids {
		idCh <- id
	}
	close(idCh)

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for id := range idCh {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				endpoint := fmt.Sprintf(endpointPattern, id.String())
				resp, err := s.client.R(workerCtx).Get(endpoint)
				if err != nil {
					config.Debug("[HOSTAWAY] failed to fetch %s for id %s: %v", label, id, err)
					continue
				}
				if !resp.IsSuccess() {
					config.Debug("[HOSTAWAY] %s for id %s returned status %d", label, id, resp.StatusCode())
					continue
				}

				items, err := extractResult(resp.Body())
				if err != nil {
					config.Debug("[HOSTAWAY] failed to parse %s for id %s: %v", label, id, err)
					continue
				}

				if len(filters) > 0 && filters[0] != nil {
					items = filters[0](items)
				}

				if len(items) == 0 {
					continue
				}

				record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("failed to convert %s to Arrow: %w", label, err):
					default:
					}
					cancel()
					return
				}

				select {
				case <-workerCtx.Done():
					return
				case results <- source.RecordBatchResult{Batch: record}:
				}
			}
		}()
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
