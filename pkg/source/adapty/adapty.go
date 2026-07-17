package adapty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	analyticsBaseURL    = "https://api-admin.adapty.io"
	serverBaseURL       = "https://api.adapty.io"
	defaultLookbackDays = 30
	// Adapty allows 2 analytics requests/second and 40,000 server-side
	// requests/minute. Keep both independent limiters at 80% of those limits.
	analyticsRateLimit   = 1.6
	serverRateLimit      = 533.33
	defaultResultBuffer  = 8
	defaultPaywallPage   = 100
	maximumPaywallPages  = 100000
	adaptyDateLayout     = "2006-01-02"
	analyticsContentType = "application/json"
)

var supportedTables = []string{
	"analytics",
	"cohorts",
	"conversion",
	"funnel",
	"ltv",
	"retention",
	"placements",
	"paywalls",
}

var metricTables = map[string]string{
	"analytics":  "/api/v1/client-api/metrics/analytics/",
	"cohorts":    "/api/v1/client-api/metrics/cohort/",
	"conversion": "/api/v1/client-api/metrics/conversion/",
	"funnel":     "/api/v1/client-api/metrics/funnel/",
	"ltv":        "/api/v1/client-api/metrics/ltv/",
	"retention":  "/api/v1/client-api/metrics/retention/",
}

var commonMetricParams = []string{
	"compare_date",
	"store",
	"country",
	"store_product_id",
	"duration",
	"attribution_source",
	"attribution_status",
	"attribution_channel",
	"attribution_campaign",
	"attribution_adgroup",
	"attribution_adset",
	"attribution_creative",
	"offer_category",
	"offer_type",
	"offer_id",
}

type AdaptySource struct {
	analyticsClient *ingestrhttp.Client
	serverClient    *ingestrhttp.Client
	lookbackDays    int
	location        *time.Location
}

type adaptyCredentials struct {
	apiKey       string
	lookbackDays int
	timezone     string
	location     *time.Location
}

type tableParams struct {
	CompareDate         []string `mapstructure:"compare_date"`
	Store               []string `mapstructure:"store"`
	Country             []string `mapstructure:"country"`
	StoreProductID      []string `mapstructure:"store_product_id"`
	Duration            []string `mapstructure:"duration"`
	AttributionSource   []string `mapstructure:"attribution_source"`
	AttributionStatus   []string `mapstructure:"attribution_status"`
	AttributionChannel  []string `mapstructure:"attribution_channel"`
	AttributionCampaign []string `mapstructure:"attribution_campaign"`
	AttributionAdgroup  []string `mapstructure:"attribution_adgroup"`
	AttributionAdset    []string `mapstructure:"attribution_adset"`
	AttributionCreative []string `mapstructure:"attribution_creative"`
	OfferCategory       []string `mapstructure:"offer_category"`
	OfferType           []string `mapstructure:"offer_type"`
	OfferID             []string `mapstructure:"offer_id"`
	ChartID             string   `mapstructure:"chart_id"`
	DateType            string   `mapstructure:"date_type"`
	Segmentation        string   `mapstructure:"segmentation"`
	PeriodType          string   `mapstructure:"period_type"`
	ValueType           string   `mapstructure:"value_type"`
	ValueField          string   `mapstructure:"value_field"`
	AccountingType      string   `mapstructure:"accounting_type"`
	RenewalDays         []string `mapstructure:"renewal_days"`
	PredictionMonths    int      `mapstructure:"prediction_months"`
	FromPeriod          string   `mapstructure:"from_period"`
	ToPeriod            string   `mapstructure:"to_period"`
	ShowValueAs         string   `mapstructure:"show_value_as"`
	UseTrial            bool     `mapstructure:"use_trial"`
	PlacementType       string   `mapstructure:"placement_type"`

	fromPeriodSet    bool
	renewalDayValues []int
}

func NewAdaptySource() *AdaptySource {
	return &AdaptySource{}
}

func (s *AdaptySource) Schemes() []string {
	return []string{"adapty"}
}

func (s *AdaptySource) HandlesIncrementality() bool {
	return true
}

func (s *AdaptySource) Connect(ctx context.Context, uri string) error {
	creds, err := parseAdaptyURI(uri)
	if err != nil {
		return err
	}

	s.lookbackDays = creds.lookbackDays
	s.location = creds.location
	auth := ingestrhttp.NewAPIKeyAuth("Authorization", "Api-Key "+creds.apiKey, true)

	s.analyticsClient = ingestrhttp.New(
		ingestrhttp.WithBaseURL(analyticsBaseURL),
		ingestrhttp.WithTimeout(2*time.Minute),
		ingestrhttp.WithRateLimiter(analyticsRateLimit, 1),
		ingestrhttp.WithAuth(auth),
		ingestrhttp.WithAllowNonIdempotentRetry(),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeaders(map[string]string{
			"Accept":       analyticsContentType,
			"Content-Type": analyticsContentType,
			"Adapty-Tz":    creds.timezone,
		}),
	)
	s.serverClient = ingestrhttp.New(
		ingestrhttp.WithBaseURL(serverBaseURL),
		ingestrhttp.WithTimeout(2*time.Minute),
		ingestrhttp.WithRateLimiter(serverRateLimit, 5),
		ingestrhttp.WithAuth(auth),
		ingestrhttp.WithDebug(config.DebugMode),
	)

	config.Debug("[ADAPTY] Connected successfully")
	return nil
}

func parseAdaptyURI(uri string) (adaptyCredentials, error) {
	if !strings.HasPrefix(uri, "adapty://") {
		return adaptyCredentials{}, fmt.Errorf("invalid adapty URI: must start with adapty://")
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return adaptyCredentials{}, fmt.Errorf("failed to parse adapty URI: %w", err)
	}
	if parsed.Scheme != "adapty" {
		return adaptyCredentials{}, fmt.Errorf("invalid adapty URI: must start with adapty://")
	}
	if parsed.Host != "" || parsed.Path != "" {
		return adaptyCredentials{}, fmt.Errorf("invalid adapty URI: credentials must be query parameters (adapty://?api_key=...)")
	}

	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return adaptyCredentials{}, fmt.Errorf("failed to parse adapty URI query: %w", err)
	}
	for key := range values {
		switch key {
		case "api_key", "lookback_days", "timezone":
		default:
			return adaptyCredentials{}, fmt.Errorf("unknown adapty URI parameter: %s", key)
		}
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return adaptyCredentials{}, fmt.Errorf("api_key is required in adapty URI (adapty://?api_key=...)")
	}

	lookbackDays := defaultLookbackDays
	if raw := values.Get("lookback_days"); raw != "" {
		lookbackDays, err = strconv.Atoi(raw)
		if err != nil || lookbackDays < 0 {
			return adaptyCredentials{}, fmt.Errorf("lookback_days must be a non-negative integer")
		}
	}

	timezone := values.Get("timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return adaptyCredentials{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	return adaptyCredentials{
		apiKey:       apiKey,
		lookbackDays: lookbackDays,
		timezone:     timezone,
		location:     location,
	}, nil
}

func (s *AdaptySource) Close(ctx context.Context) error {
	var errs []error
	if s.analyticsClient != nil {
		errs = append(errs, s.analyticsClient.Close())
	}
	if s.serverClient != nil {
		errs = append(errs, s.serverClient.Close())
	}
	return errors.Join(errs...)
}

func (s *AdaptySource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, params, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}
	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	table := &source.DynamicSourceTable{
		TableName:   tableName,
		KnownSchema: false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("adapty source does not have a predefined schema; schema inference is required")
		},
	}

	switch tableName {
	case "paywalls":
		table.TablePrimaryKeys = []string{"paywall_id"}
		table.TableIncrementalKey = "updated_at"
		table.TableStrategy = config.StrategyMerge
		table.ReadFn = func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readPaywalls(ctx, opts)
		}
	case "placements":
		table.TablePrimaryKeys = req.PrimaryKeys
		table.TableStrategy = config.StrategyReplace
		table.ReadFn = func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readPlacements(ctx, params, opts)
		}
	default:
		table.TablePrimaryKeys = req.PrimaryKeys
		table.TableIncrementalKey = "date"
		table.TableStrategy = config.StrategyDeleteInsert
		table.TablePartitionBy = "date"
		table.ReadFn = func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.readMetricTable(ctx, tableName, params, opts)
		}
	}

	return table, nil
}

func isValidTable(table string) bool {
	for _, supported := range supportedTables {
		if table == supported {
			return true
		}
	}
	return false
}

func parseTableSpec(raw string) (string, tableParams, error) {
	path, values, hasParams, err := tablespec.Split(raw)
	if err != nil {
		return "", tableParams{}, err
	}
	if !isValidTable(path) {
		return path, tableParams{}, nil
	}

	allowed := allowedParams(path)
	if hasParams {
		if len(allowed) == 0 {
			return "", tableParams{}, fmt.Errorf("%s does not accept table parameters", path)
		}
		if err := tablespec.ValidateKeys(values, allowed...); err != nil {
			return "", tableParams{}, err
		}
	}

	var params tableParams
	if _, _, err := tablespec.Parse(raw, &params, tablespec.WithListSeparator(",")); err != nil {
		return "", tableParams{}, err
	}
	params.fromPeriodSet = values.Has("from_period")
	if err := validateTableParams(path, &params); err != nil {
		return "", tableParams{}, err
	}
	return path, params, nil
}

func allowedParams(table string) []string {
	var specific []string
	switch table {
	case "analytics":
		specific = []string{"chart_id", "date_type", "segmentation"}
	case "cohorts":
		specific = []string{"period_type", "value_type", "value_field", "accounting_type", "renewal_days", "prediction_months"}
	case "conversion":
		specific = []string{"from_period", "to_period", "date_type", "segmentation"}
	case "funnel":
		specific = []string{"show_value_as", "segmentation"}
	case "ltv":
		specific = []string{"period_type", "segmentation"}
	case "retention":
		specific = []string{"segmentation", "use_trial"}
	case "placements":
		return []string{"placement_type"}
	case "paywalls":
		return nil
	}
	return append(append([]string{}, commonMetricParams...), specific...)
}

func validateTableParams(table string, params *tableParams) error {
	if len(params.CompareDate) > 0 {
		if len(params.CompareDate) != 2 {
			return fmt.Errorf("compare_date must contain exactly two dates")
		}
		for _, value := range params.CompareDate {
			if _, err := time.Parse(adaptyDateLayout, value); err != nil {
				return fmt.Errorf("compare_date contains invalid date %q; expected YYYY-MM-DD", value)
			}
		}
	}

	if err := validateEnum("date_type", params.DateType, "purchase_date", "profile_install_date"); err != nil {
		return err
	}
	if err := validateEnum("period_type", params.PeriodType, "renewals", "days"); err != nil {
		return err
	}

	switch table {
	case "analytics":
		if params.ChartID == "" {
			return fmt.Errorf("analytics requires chart_id")
		}
		if err := validateEnum("chart_id", params.ChartID,
			"revenue", "mrr", "arr", "arppu", "subscriptions_active", "subscriptions_new",
			"subscriptions_renewal_cancelled", "subscriptions_expired", "trials_active", "trials_new",
			"trials_renewal_cancelled", "trials_expired", "grace_period", "billing_issue", "refund_events",
			"refund_money", "non_subscriptions", "arpu", "installs"); err != nil {
			return err
		}
	case "cohorts":
		if err := validateEnum("value_type", params.ValueType, "absolute", "relative"); err != nil {
			return err
		}
		if err := validateEnum("value_field", params.ValueField, "revenue", "arppu", "arpu", "arpas", "subscribers", "subscriptions"); err != nil {
			return err
		}
		if err := validateEnum("accounting_type", params.AccountingType, "revenue", "proceeds", "net_revenue"); err != nil {
			return err
		}
		if params.PredictionMonths != 0 && !containsInt([]int{3, 6, 9, 12, 18, 24}, params.PredictionMonths) {
			return fmt.Errorf("invalid prediction_months %d (supported: 3, 6, 9, 12, 18, 24)", params.PredictionMonths)
		}
		for _, raw := range params.RenewalDays {
			day, err := strconv.Atoi(raw)
			if err != nil || day < 0 {
				return fmt.Errorf("renewal_days must contain non-negative integers")
			}
			params.renewalDayValues = append(params.renewalDayValues, day)
		}
	case "conversion":
		if !params.fromPeriodSet {
			return fmt.Errorf("conversion requires from_period (use from_period=null for no starting state)")
		}
		if params.ToPeriod == "" {
			return fmt.Errorf("conversion requires to_period")
		}
	case "funnel":
		if err := validateEnum("show_value_as", params.ShowValueAs, "absolute", "relative", "both"); err != nil {
			return err
		}
	case "placements":
		if params.PlacementType == "" {
			return fmt.Errorf("placements requires placement_type")
		}
		if err := validateEnum("placement_type", params.PlacementType, "paywall", "onboarding"); err != nil {
			return err
		}
	}
	return nil
}

func validateEnum(name, value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q (supported: %s)", name, value, strings.Join(allowed, ", "))
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *AdaptySource) readMetricTable(ctx context.Context, table string, params tableParams, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, defaultResultBuffer))
	start, end, err := s.resolveDateRange(&opts)
	if err != nil {
		close(results)
		return nil, err
	}

	go func() {
		defer close(results)

		for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
			if err := ctx.Err(); err != nil {
				emitError(ctx, results, err)
				return
			}

			dateString := date.Format(adaptyDateLayout)
			body := buildMetricRequest(table, params, dateString)
			response, err := s.analyticsClient.R(ctx).SetBody(body).Post(metricTables[table])
			if err != nil {
				emitError(ctx, results, fmt.Errorf("failed to fetch %s for %s: %w", table, dateString, err))
				return
			}
			if !response.IsSuccess() {
				emitError(ctx, results, fmt.Errorf("failed to fetch %s for %s: status %d: %s", table, dateString, response.StatusCode(), response.String()))
				return
			}

			var payload map[string]any
			if err := decodeJSONUseNumber(response.Body(), &payload); err != nil {
				emitError(ctx, results, fmt.Errorf("failed to parse %s response for %s: %w", table, dateString, err))
				return
			}
			rows, err := metricRows(table, params, dateString, payload)
			if err != nil {
				emitError(ctx, results, err)
				return
			}
			if len(rows) > 0 {
				if err := emitRows(ctx, results, rows, metricColumns(table), opts.ExcludeColumns); err != nil {
					emitError(ctx, results, fmt.Errorf("failed to emit %s rows for %s: %w", table, dateString, err))
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *AdaptySource) resolveDateRange(opts *source.ReadOptions) (time.Time, time.Time, error) {
	location := s.location
	if location == nil {
		location = time.UTC
	}
	now := dayInLocation(time.Now(), location)
	start := now.AddDate(0, 0, -s.lookbackDays)
	end := now

	if opts.IntervalStart != nil {
		start = dayInLocation(*opts.IntervalStart, location)
	}
	if opts.IntervalEnd != nil {
		end = dayInLocation(*opts.IntervalEnd, location)
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("adapty interval start (%s) must not be after interval end (%s)", start.Format(adaptyDateLayout), end.Format(adaptyDateLayout))
	}
	return start, end, nil
}

func dayInLocation(value time.Time, location *time.Location) time.Time {
	value = value.In(location)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
}

func buildMetricRequest(table string, params tableParams, date string) map[string]any {
	body := map[string]any{
		"filters":     buildMetricFilters(params, date),
		"period_unit": "day",
		"format":      "json",
	}

	switch table {
	case "analytics":
		body["chart_id"] = params.ChartID
		setString(body, "date_type", params.DateType)
		setString(body, "segmentation", params.Segmentation)
	case "cohorts":
		setString(body, "period_type", params.PeriodType)
		setString(body, "value_type", params.ValueType)
		setString(body, "value_field", params.ValueField)
		setString(body, "accounting_type", params.AccountingType)
		if len(params.renewalDayValues) > 0 {
			body["renewal_days"] = params.renewalDayValues
		}
		if params.PredictionMonths > 0 {
			body["prediction_months"] = params.PredictionMonths
		}
	case "conversion":
		if params.FromPeriod == "" || params.FromPeriod == "null" {
			body["from_period"] = nil
		} else {
			body["from_period"] = params.FromPeriod
		}
		body["to_period"] = params.ToPeriod
		setString(body, "date_type", params.DateType)
		setString(body, "segmentation", params.Segmentation)
	case "funnel":
		setString(body, "show_value_as", params.ShowValueAs)
		setString(body, "segmentation", params.Segmentation)
	case "ltv":
		setString(body, "period_type", params.PeriodType)
		setString(body, "segmentation", params.Segmentation)
	case "retention":
		setString(body, "segmentation", params.Segmentation)
		if params.UseTrial {
			body["use_trial"] = true
		}
	}
	return body
}

func buildMetricFilters(params tableParams, date string) map[string]any {
	filters := map[string]any{"date": []string{date, date}}
	setStrings(filters, "compare_date", params.CompareDate)
	setStrings(filters, "store", params.Store)
	setStrings(filters, "country", params.Country)
	setStrings(filters, "store_product_id", params.StoreProductID)
	setStrings(filters, "duration", params.Duration)
	setStrings(filters, "attribution_source", params.AttributionSource)
	setStrings(filters, "attribution_status", params.AttributionStatus)
	setStrings(filters, "attribution_channel", params.AttributionChannel)
	setStrings(filters, "attribution_campaign", params.AttributionCampaign)
	setStrings(filters, "attribution_adgroup", params.AttributionAdgroup)
	setStrings(filters, "attribution_adset", params.AttributionAdset)
	setStrings(filters, "attribution_creative", params.AttributionCreative)
	setStrings(filters, "offer_category", params.OfferCategory)
	setStrings(filters, "offer_type", params.OfferType)
	setStrings(filters, "offer_id", params.OfferID)
	return filters
}

func setString(target map[string]any, key, value string) {
	if value != "" {
		target[key] = value
	}
}

func setStrings(target map[string]any, key string, values []string) {
	if len(values) > 0 {
		target[key] = values
	}
}

func metricRows(table string, params tableParams, date string, payload map[string]any) ([]map[string]any, error) {
	switch table {
	case "analytics":
		rawData, exists := payload["data"]
		if !exists || rawData == nil {
			return nil, nil
		}
		data, ok := rawData.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected analytics response: data is not an object")
		}
		keys := make([]string, 0, len(data))
		for key := range data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		rows := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			item, ok := data[key].(map[string]any)
			if !ok {
				continue
			}
			row := cloneMap(item)
			row["date"] = date
			row["metric"] = key
			row["chart_id"] = params.ChartID
			rows = append(rows, row)
		}
		return rows, nil
	case "cohorts", "funnel":
		rawData, exists := payload["data"]
		if !exists || rawData == nil {
			return nil, nil
		}
		data, ok := rawData.([]any)
		if !ok {
			return nil, fmt.Errorf("unexpected %s response: data is not an array", table)
		}
		rows := make([]map[string]any, 0, len(data))
		for _, value := range data {
			item, ok := value.(map[string]any)
			if !ok {
				continue
			}
			row := cloneMap(item)
			row["date"] = date
			rows = append(rows, row)
		}
		return rows, nil
	case "conversion", "retention":
		row := cloneMap(payload)
		row["date"] = date
		return []map[string]any{row}, nil
	case "ltv":
		rows := make([]map[string]any, 0, 3)
		for _, accountingType := range []string{"revenue", "proceeds", "net_revenue"} {
			item, ok := payload[accountingType].(map[string]any)
			if !ok {
				continue
			}
			row := cloneMap(item)
			row["date"] = date
			row["accounting_type"] = accountingType
			rows = append(rows, row)
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unsupported metric table: %s", table)
	}
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+2)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func metricColumns(table string) []schema.Column {
	columns := []schema.Column{{Name: "date", DataType: schema.TypeDate, Nullable: false}}
	if table == "analytics" {
		columns = append(
			columns,
			schema.Column{Name: "metric", DataType: schema.TypeString, Nullable: false},
			schema.Column{Name: "chart_id", DataType: schema.TypeString, Nullable: false},
		)
	}
	if table == "ltv" {
		columns = append(columns, schema.Column{Name: "accounting_type", DataType: schema.TypeString, Nullable: false})
	}
	return columns
}

func (s *AdaptySource) readPlacements(ctx context.Context, params tableParams, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, 1))
	go func() {
		defer close(results)

		body := map[string]any{"filters": map[string]any{"placement_type": params.PlacementType}}
		response, err := s.analyticsClient.R(ctx).SetBody(body).Post("/api/v1/client-api/exports/placements/")
		if err != nil {
			emitError(ctx, results, fmt.Errorf("failed to fetch placements: %w", err))
			return
		}
		if !response.IsSuccess() {
			emitError(ctx, results, fmt.Errorf("failed to fetch placements: status %d: %s", response.StatusCode(), response.String()))
			return
		}

		var payload struct {
			Data []map[string]any `json:"data"`
		}
		if err := decodeJSONUseNumber(response.Body(), &payload); err != nil {
			emitError(ctx, results, fmt.Errorf("failed to parse placements response: %w", err))
			return
		}
		if len(payload.Data) == 0 {
			return
		}
		if err := emitRows(ctx, results, payload.Data, nil, opts.ExcludeColumns); err != nil {
			emitError(ctx, results, fmt.Errorf("failed to emit placements: %w", err))
		}
	}()
	return results, nil
}

func (s *AdaptySource) readPaywalls(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, source.RecordBatchBufferSize(opts, defaultResultBuffer))
	go func() {
		defer close(results)

		pageSize := opts.PageSize
		if pageSize <= 0 || pageSize > defaultPaywallPage {
			pageSize = defaultPaywallPage
		}
		for page := 1; page <= maximumPaywallPages; page++ {
			if err := ctx.Err(); err != nil {
				emitError(ctx, results, err)
				return
			}

			response, err := s.serverClient.R(ctx).
				SetQueryParam("page[number]", strconv.Itoa(page)).
				SetQueryParam("page[size]", strconv.Itoa(pageSize)).
				Get("/api/v2/server-side-api/paywalls/")
			if err != nil {
				emitError(ctx, results, fmt.Errorf("failed to fetch paywalls page %d: %w", page, err))
				return
			}
			if !response.IsSuccess() {
				emitError(ctx, results, fmt.Errorf("failed to fetch paywalls page %d: status %d: %s", page, response.StatusCode(), response.String()))
				return
			}

			var payload struct {
				Data []map[string]any `json:"data"`
				Meta struct {
					Pagination struct {
						Page  int `json:"page"`
						Pages int `json:"pages"`
					} `json:"pagination"`
				} `json:"meta"`
			}
			if err := decodeJSONUseNumber(response.Body(), &payload); err != nil {
				emitError(ctx, results, fmt.Errorf("failed to parse paywalls page %d: %w", page, err))
				return
			}
			if len(payload.Data) == 0 {
				return
			}

			rows, err := filterPaywallsByUpdatedAt(payload.Data, opts.IntervalStart, opts.IntervalEnd)
			if err != nil {
				emitError(ctx, results, err)
				return
			}
			if len(rows) > 0 {
				if err := emitRows(ctx, results, rows, paywallColumns(), opts.ExcludeColumns); err != nil {
					emitError(ctx, results, fmt.Errorf("failed to emit paywalls page %d: %w", page, err))
					return
				}
			}
			if len(payload.Data) < pageSize || (payload.Meta.Pagination.Pages > 0 && payload.Meta.Pagination.Pages <= page) {
				return
			}
		}

		emitError(ctx, results, fmt.Errorf("paywalls pagination exceeded %d pages", maximumPaywallPages))
	}()
	return results, nil
}

func filterPaywallsByUpdatedAt(rows []map[string]any, start, end *time.Time) ([]map[string]any, error) {
	if start == nil && end == nil {
		return rows, nil
	}

	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		raw, ok := row["updated_at"].(string)
		if !ok || raw == "" {
			return nil, fmt.Errorf("paywall response is missing updated_at")
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid paywall updated_at %q: %w", raw, err)
		}
		if start != nil && updatedAt.Before(*start) {
			continue
		}
		if end != nil && updatedAt.After(*end) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered, nil
}

func paywallColumns() []schema.Column {
	return []schema.Column{
		{Name: "paywall_id", DataType: schema.TypeString, Nullable: false},
		{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: false},
		{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: false},
	}
}

func decodeJSONUseNumber(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("response contains multiple JSON values")
	}
	return nil
}

func emitRows(ctx context.Context, results chan<- source.RecordBatchResult, rows []map[string]any, columns []schema.Column, excludeColumns []string) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, columns, excludeColumns)
	if err != nil {
		return err
	}
	select {
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	}
}

func emitError(ctx context.Context, results chan<- source.RecordBatchResult, err error) {
	select {
	case results <- source.RecordBatchResult{Err: err}:
	case <-ctx.Done():
	}
}
