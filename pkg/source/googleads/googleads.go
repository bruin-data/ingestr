package googleads

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/oauth2adapt"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/shenzhencenter/google-ads-pb/services"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	grpccreds "google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	grpcEndpoint = "googleads.googleapis.com:443"
	adsScope     = "https://www.googleapis.com/auth/adwords"
)

type GoogleAdsSource struct {
	conn            *grpc.ClientConn
	tokenSource     oauth2.TokenSource
	customerIDs     []string
	devToken        string
	loginCustomerID string
	credentialsJSON []byte
}

func NewGoogleAdsSource() *GoogleAdsSource {
	return &GoogleAdsSource{}
}

func (s *GoogleAdsSource) Schemes() []string {
	return []string{"googleads"}
}

func (s *GoogleAdsSource) Connect(ctx context.Context, uri string) error {
	customerIDs, devToken, loginCustomerID, credentialsJSON, err := parseGoogleAdsURI(uri)
	if err != nil {
		return err
	}

	s.customerIDs = customerIDs
	s.devToken = devToken
	s.loginCustomerID = loginCustomerID
	s.credentialsJSON = credentialsJSON

	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		CredentialsJSON: s.credentialsJSON,
		Scopes:          []string{adsScope},
	})
	if err != nil {
		return fmt.Errorf("failed to create credentials: %w", err)
	}

	s.tokenSource = oauth2.ReuseTokenSource(nil, oauth2adapt.TokenSourceFromTokenProvider(creds))

	conn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(grpccreds.NewClientTLSFromCert(nil, "")),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	s.conn = conn

	config.Debug("[GOOGLE_ADS] Connected successfully")
	return nil
}

func (s *GoogleAdsSource) grpcContext(ctx context.Context) (context.Context, error) {
	token, err := s.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	md := metadata.Pairs(
		"authorization", "Bearer "+token.AccessToken,
		"developer-token", s.devToken,
	)
	if s.loginCustomerID != "" {
		md.Append("login-customer-id", s.loginCustomerID)
	}

	return metadata.NewOutgoingContext(ctx, md), nil
}

func parseGoogleAdsURI(uri string) ([]string, string, string, []byte, error) {
	if !strings.HasPrefix(uri, "googleads://") {
		return nil, "", "", nil, fmt.Errorf("invalid google ads URI: must start with googleads://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("invalid google ads URI: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, "", "", nil, fmt.Errorf("customer_id is required in google ads URI")
	}

	customerIDs := splitAndTrim(host, ",")
	if len(customerIDs) == 0 {
		return nil, "", "", nil, fmt.Errorf("customer_id is required in google ads URI")
	}

	params := parsed.Query()

	devToken := params.Get("dev_token")
	if devToken == "" {
		return nil, "", "", nil, fmt.Errorf("dev_token is required in google ads URI")
	}

	loginCustomerID := params.Get("login_customer_id")

	credentialsJSON, err := getCredentials(params)
	if err != nil {
		return nil, "", "", nil, err
	}

	return customerIDs, devToken, loginCustomerID, credentialsJSON, nil
}

// getCredentials reads the service account credentials from the URI. Credentials
// are optional: when neither is provided, it returns nil and the client falls
// back to Application Default Credentials (e.g. the gcloud ADC file on the machine).
func getCredentials(params url.Values) ([]byte, error) {
	if path := params.Get("credentials_path"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read credentials file: %w", err)
		}
		return data, nil
	}

	if b64 := params.Get("credentials_base64"); b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credentials_base64: %w", err)
		}
		return data, nil
	}

	return nil, nil
}

func (s *GoogleAdsSource) Close(ctx context.Context) error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *GoogleAdsSource) HandlesIncrementality() bool {
	return false
}

func (s *GoogleAdsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	primaryKeys := []string{}
	incrementalKey := ""

	switch {
	case strings.HasPrefix(tableName, "gaql_query:"):

	case strings.HasPrefix(tableName, "daily:"):
		spec := strings.TrimPrefix(tableName, "daily:")
		report, _, err := reportFromSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("invalid daily report spec: %w", err)
		}
		primaryKeys = appendUnique(report.PrimaryKeys(), "customer_id")
		incrementalKey = "segments_date"

	default:
		name := tableName
		if strings.Contains(name, ":") {
			name = strings.SplitN(name, ":", 2)[0]
		}
		if report, ok := builtinReports[name]; ok {
			primaryKeys = appendUnique(report.PrimaryKeys(), "customer_id")
			incrementalKey = "segments_date"
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       req.Strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("google ads source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *GoogleAdsSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	start := startDate(opts)
	end := endDate(opts)

	results := make(chan source.RecordBatchResult, 8)
	customerIDs := s.customerIDs

	var query string
	var report *Report
	var specCustomerIDs []string
	var err error

	switch {
	case strings.HasPrefix(table, "gaql_query:"):
		query = strings.TrimPrefix(table, "gaql_query:")
		if opts.IntervalStart == nil {
			start = "1970-01-01"
		}
		query = strings.ReplaceAll(query, ":interval_start", "'"+start+"'")
		query = strings.ReplaceAll(query, ":interval_end", "'"+end+"'")

	case strings.HasPrefix(table, "daily:"):
		spec := strings.TrimPrefix(table, "daily:")
		report, specCustomerIDs, err = reportFromSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("invalid daily report spec: %w", err)
		}
		if len(specCustomerIDs) > 0 {
			customerIDs = specCustomerIDs
		}
		query = report.BuildQuery(start, end)

	case strings.Contains(table, ":"):
		parts := strings.SplitN(table, ":", 2)
		table = parts[0]
		customerIDs = splitAndTrim(parts[1], ",")
		if r, ok := builtinReports[table]; ok {
			report = r
			query = r.BuildQuery(start, end)
		}

	default:
		if r, ok := builtinReports[table]; ok {
			report = r
			query = r.BuildQuery(start, end)
		}
	}

	var cols []schema.Column
	var pks []string
	if report != nil {
		cols = metricsColumns(report.Metrics)
		pks = report.PrimaryKeys()
	}

	if query == "" {
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	go func() {
		defer close(results)

		if report != nil && report.SingleDayFilter {
			days, err := daysBetween(start, end)
			if err != nil {
				results <- source.RecordBatchResult{Err: err}
				return
			}
			for _, customerID := range customerIDs {
				for _, day := range days {
					dayQuery := report.BuildQueryForDay(day)
					if err := s.queryCustomer(ctx, customerID, dayQuery, cols, pks, opts, results); err != nil {
						results <- source.RecordBatchResult{Err: fmt.Errorf("customer %s day %s: %w", customerID, day, err)}
						return
					}
				}
			}
		} else {
			for _, customerID := range customerIDs {
				if err := s.queryCustomer(ctx, customerID, query, cols, pks, opts, results); err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("customer %s: %w", customerID, err)}
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *GoogleAdsSource) queryCustomer(ctx context.Context, customerID string, query string, cols []schema.Column, pks []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	grpcCtx, err := s.grpcContext(ctx)
	if err != nil {
		return err
	}

	svc := services.NewGoogleAdsServiceClient(s.conn)
	stream, err := svc.SearchStream(grpcCtx, &services.SearchGoogleAdsStreamRequest{
		CustomerId: customerID,
		Query:      query,
	})
	if err != nil {
		apiErr := status.Convert(err)
		return fmt.Errorf("google ads gRPC error: code=%s message=%s details=%v", apiErr.Code(), apiErr.Message(), apiErr.Details())
	}

	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: false,
		UseProtoNames:   true,
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			apiErr := status.Convert(err)
			return fmt.Errorf("google ads stream error: code=%s message=%s details=%v", apiErr.Code(), apiErr.Message(), apiErr.Details())
		}

		if len(resp.Results) == 0 {
			continue
		}

		flat := make([]map[string]any, 0, len(resp.Results))
		for _, row := range resp.Results {
			jsonBytes, err := marshaler.Marshal(row)
			if err != nil {
				return fmt.Errorf("failed to marshal protobuf row: %w", err)
			}

			var rawMap map[string]any
			if err := json.Unmarshal(jsonBytes, &rawMap); err != nil {
				return fmt.Errorf("failed to parse row JSON: %w", err)
			}

			r := flattenRow(rawMap)
			r["customer_id"] = customerID
			for _, pk := range pks {
				v, exists := r[pk]
				if !exists || v == nil || v == "" {
					r[pk] = "-"
				}
			}
			flat = append(flat, r)
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(flat, cols, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[GOOGLE_ADS] Fetched data for customer %s", customerID)
	return nil
}

func flattenRow(row map[string]any) map[string]any {
	result := make(map[string]any)
	flattenInto(result, "", row)
	return result
}

func flattenInto(result map[string]any, prefix string, data map[string]any) {
	for key, val := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "_" + key
		}
		switch v := val.(type) {
		case map[string]any:
			flattenInto(result, fullKey, v)
		case []any:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				parts = append(parts, fmt.Sprint(item))
			}
			result[fullKey] = strings.Join(parts, ",")
		default:
			result[fullKey] = val
		}
	}
}

func toDateString(val any, fallback string) string {
	if val == nil {
		return fallback
	}
	switch v := val.(type) {
	case time.Time:
		if v.IsZero() {
			return fallback
		}
		return v.Format("2006-01-02")
	case *time.Time:
		if v == nil || v.IsZero() {
			return fallback
		}
		return v.Format("2006-01-02")
	case string:
		if v == "" {
			return fallback
		}
		return v
	default:
		return fallback
	}
}

func daysBetween(start, end string) ([]string, error) {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return nil, fmt.Errorf("invalid start date %q: %w", start, err)
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, fmt.Errorf("invalid end date %q: %w", end, err)
	}
	var days []string
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format("2006-01-02"))
	}
	return days, nil
}

func startDate(opts source.ReadOptions) string {
	return toDateString(opts.IntervalStart, time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02"))
}

func endDate(opts source.ReadOptions) string {
	return toDateString(opts.IntervalEnd, time.Now().UTC().Format("2006-01-02"))
}

type Report struct {
	Resource        string
	Dimensions      []string
	Metrics         []string
	Segments        []string
	Unfilterable    bool
	SingleDayFilter bool
}

func (r *Report) PrimaryKeys() []string {
	keys := make([]string, 0, 1+len(r.Dimensions)+len(r.Segments))
	keys = append(keys, r.Resource+"_resource_name")
	for _, d := range r.Dimensions {
		keys = append(keys, toColumn(d))
	}
	for _, s := range r.Segments {
		keys = append(keys, toColumn(s))
	}
	return keys
}

func (r *Report) BuildQuery(start, end string) string {
	fields := make([]string, 0, len(r.Segments)+len(r.Dimensions)+len(r.Metrics))
	fields = append(fields, r.Segments...)
	fields = append(fields, r.Dimensions...)
	fields = append(fields, r.Metrics...)

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(fields, ", "), r.Resource)
	if !r.Unfilterable {
		query += fmt.Sprintf(" WHERE segments.date BETWEEN '%s' AND '%s'", start, end)
	}
	return query
}

func (r *Report) BuildQueryForDay(day string) string {
	fields := make([]string, 0, len(r.Segments)+len(r.Dimensions)+len(r.Metrics))
	fields = append(fields, r.Segments...)
	fields = append(fields, r.Dimensions...)
	fields = append(fields, r.Metrics...)

	return fmt.Sprintf("SELECT %s FROM %s WHERE segments.date = '%s'", strings.Join(fields, ", "), r.Resource, day)
}

func toColumn(field string) string {
	return strings.ReplaceAll(field, ".", "_")
}

func reportFromSpec(spec string) (*Report, []string, error) {
	colonCount := strings.Count(spec, ":")
	if colonCount < 2 || colonCount > 3 {
		return nil, nil, fmt.Errorf("invalid report spec format, expected {resource}:{dimensions}:{metrics} or {resource}:{dimensions}:{metrics}:{customer_ids}")
	}

	parts := strings.SplitN(spec, ":", 4)
	resource := parts[0]
	dimensions := parts[1]
	metrics := parts[2]

	if strings.TrimSpace(dimensions) == "" {
		return nil, nil, fmt.Errorf("dimensions are required in report spec")
	}
	if strings.TrimSpace(metrics) == "" {
		return nil, nil, fmt.Errorf("metrics are required in report spec")
	}

	report := &Report{
		Resource: resource,
		Segments: []string{"segments.date"},
	}

	for _, d := range strings.Split(dimensions, ",") {
		d = strings.TrimSpace(d)
		if !strings.Contains(d, ".") {
			return nil, nil, fmt.Errorf("invalid dimension format %q, expected {resource}.{field}", d)
		}
		if strings.HasPrefix(d, "segments.") {
			return nil, nil, fmt.Errorf("segments are not allowed in dimensions: %q", d)
		}
		report.Dimensions = append(report.Dimensions, d)
	}

	for _, m := range strings.Split(metrics, ",") {
		m = strings.TrimSpace(m)
		if !strings.HasPrefix(m, "metrics.") {
			m = "metrics." + m
		}
		report.Metrics = append(report.Metrics, m)
	}

	var customerIDs []string
	if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
		for _, cid := range strings.Split(parts[3], ",") {
			customerIDs = append(customerIDs, strings.TrimSpace(cid))
		}
	}

	return report, customerIDs, nil
}

func appendUnique(slice []string, vals ...string) []string {
	seen := make(map[string]bool, len(slice)+len(vals))
	result := make([]string, 0, len(slice)+len(vals))
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, v := range vals {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func splitAndTrim(s string, sep string) []string {
	parts := strings.Split(s, sep)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func metricsColumns(metrics []string) []schema.Column {
	cols := make([]schema.Column, 0, len(metrics))
	for _, m := range metrics {
		colName := toColumn(m)
		typ, ok := metricsSchema[colName]
		if !ok {
			continue
		}
		cols = append(cols, schema.Column{
			Name:     colName,
			DataType: typ,
			Nullable: true,
		})
	}
	return cols
}

var builtinReports = map[string]*Report{
	"account_report_daily": {
		Resource:   "campaign",
		Dimensions: []string{"customer.id"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
		Segments: []string{"segments.date", "segments.ad_network_type", "segments.device"},
	},
	"campaign_report_daily": {
		Resource:   "campaign",
		Dimensions: []string{"campaign.id", "customer.id"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
		Segments: []string{"segments.date", "segments.ad_network_type", "segments.device"},
	},
	"ad_group_report_daily": {
		Resource:   "ad_group",
		Dimensions: []string{"ad_group.id", "customer.id", "campaign.id"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
		Segments: []string{"segments.date", "segments.ad_network_type", "segments.device"},
	},
	"ad_report_daily": {
		Resource:   "ad_group_ad",
		Dimensions: []string{"ad_group.id", "ad_group_ad.ad.id", "customer.id", "campaign.id"},
		Segments:   []string{"segments.date", "segments.ad_network_type", "segments.device"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
	},
	"audience_report_daily": {
		Resource:   "ad_group_audience_view",
		Dimensions: []string{"ad_group.id", "customer.id", "campaign.id", "ad_group_criterion.criterion_id"},
		Segments:   []string{"segments.date", "segments.ad_network_type", "segments.device"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
	},
	"keyword_report_daily": {
		Resource:   "keyword_view",
		Dimensions: []string{"ad_group.id", "customer.id", "campaign.id", "ad_group_criterion.criterion_id"},
		Segments:   []string{"segments.date", "segments.ad_network_type", "segments.device"},
		Metrics: []string{
			"metrics.active_view_impressions",
			"metrics.active_view_measurability",
			"metrics.active_view_measurable_cost_micros",
			"metrics.active_view_measurable_impressions",
			"metrics.active_view_viewability",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.impressions",
			"metrics.interactions",
			"metrics.interaction_event_types",
			"metrics.view_through_conversions",
		},
	},
	"click_report_daily": {
		Resource: "click_view",
		Dimensions: []string{
			"click_view.gclid",
			"customer.id",
			"ad_group.id",
			"campaign.id",
			"segments.date",
		},
		Metrics:         []string{"metrics.clicks"},
		SingleDayFilter: true,
	},
	"landing_page_report_daily": {
		Resource: "landing_page_view",
		Dimensions: []string{
			"landing_page_view.unexpanded_final_url",
			"landing_page_view.resource_name",
			"customer.id",
			"ad_group.id",
			"campaign.id",
			"segments.date",
		},
		Metrics: []string{
			"metrics.average_cpc",
			"metrics.clicks",
			"metrics.cost_micros",
			"metrics.ctr",
			"metrics.impressions",
			"metrics.mobile_friendly_clicks_percentage",
			"metrics.speed_score",
			"metrics.valid_accelerated_mobile_pages_clicks_percentage",
		},
	},
	"search_keyword_report_daily": {
		Resource: "keyword_view",
		Dimensions: []string{
			"customer.id",
			"ad_group.id",
			"campaign.id",
			"keyword_view.resource_name",
			"ad_group_criterion.criterion_id",
			"segments.date",
		},
		Metrics: []string{
			"metrics.absolute_top_impression_percentage",
			"metrics.average_cpc",
			"metrics.average_cpm",
			"metrics.clicks",
			"metrics.conversions_from_interactions_rate",
			"metrics.conversions_value",
			"metrics.cost_micros",
			"metrics.ctr",
			"metrics.impressions",
			"metrics.top_impression_percentage",
			"metrics.view_through_conversions",
		},
	},
	"search_term_report_daily": {
		Resource: "search_term_view",
		Dimensions: []string{
			"customer.id",
			"ad_group.id",
			"campaign.id",
			"search_term_view.resource_name",
			"search_term_view.search_term",
			"search_term_view.status",
			"segments.date",
		},
		Segments: []string{"segments.search_term_match_type"},
		Metrics: []string{
			"metrics.absolute_top_impression_percentage",
			"metrics.average_cpc",
			"metrics.clicks",
			"metrics.conversions",
			"metrics.conversions_from_interactions_rate",
			"metrics.conversions_from_interactions_value_per_interaction",
			"metrics.cost_micros",
			"metrics.ctr",
			"metrics.impressions",
			"metrics.top_impression_percentage",
			"metrics.view_through_conversions",
		},
	},
	"lead_form_submission_data_report_daily": {
		Resource: "lead_form_submission_data",
		Dimensions: []string{
			"lead_form_submission_data.gclid",
			"lead_form_submission_data.submission_date_time",
			"lead_form_submission_data.lead_form_submission_fields",
			"lead_form_submission_data.custom_lead_form_submission_fields",
			"lead_form_submission_data.resource_name",
			"customer.id",
			"ad_group_ad.ad.id",
			"ad_group.id",
			"campaign.id",
		},
		Unfilterable: true,
	},
	"local_services_lead_report_daily": {
		Resource: "local_services_lead",
		Dimensions: []string{
			"customer.id",
			"local_services_lead.creation_date_time",
			"local_services_lead.contact_details",
			"local_services_lead.credit_details.credit_state",
			"local_services_lead.credit_details.credit_state_last_update_date_time",
			"local_services_lead.lead_charged",
			"local_services_lead.lead_status",
			"local_services_lead.lead_type",
			"local_services_lead.locale",
			"local_services_lead.note.description",
			"local_services_lead.note.edit_date_time",
			"local_services_lead.service_id",
		},
		Unfilterable: true,
	},
	"local_services_lead_conversations_report_daily": {
		Resource: "local_services_lead_conversation",
		Dimensions: []string{
			"customer.id",
			"local_services_lead_conversation.id",
			"local_services_lead_conversation.event_date_time",
			"local_services_lead_conversation.conversation_channel",
			"local_services_lead_conversation.message_details.attachment_urls",
			"local_services_lead_conversation.message_details.text",
			"local_services_lead_conversation.participant_type",
			"local_services_lead_conversation.phone_call_details.call_duration_millis",
			"local_services_lead_conversation.phone_call_details.call_recording_url",
		},
		Unfilterable: true,
	},
}

var metricsSchema = map[string]schema.DataType{
	"metrics_absolute_top_impression_percentage":                                           schema.TypeFloat64,
	"metrics_active_view_cpm":                                                              schema.TypeFloat64,
	"metrics_active_view_ctr":                                                              schema.TypeFloat64,
	"metrics_active_view_impressions":                                                      schema.TypeInt64,
	"metrics_active_view_measurability":                                                    schema.TypeFloat64,
	"metrics_active_view_measurable_cost_micros":                                           schema.TypeInt64,
	"metrics_active_view_measurable_impressions":                                           schema.TypeInt64,
	"metrics_active_view_viewability":                                                      schema.TypeFloat64,
	"metrics_all_conversions":                                                              schema.TypeFloat64,
	"metrics_all_conversions_by_conversion_date":                                           schema.TypeFloat64,
	"metrics_all_conversions_from_click_to_call":                                           schema.TypeFloat64,
	"metrics_all_conversions_from_directions":                                              schema.TypeFloat64,
	"metrics_all_conversions_from_interactions_rate":                                       schema.TypeFloat64,
	"metrics_all_conversions_from_interactions_value_per_interaction":                      schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_click_to_call":                            schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_directions":                               schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_menu":                                     schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_order":                                    schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_other_engagement":                         schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_store_visits":                             schema.TypeFloat64,
	"metrics_all_conversions_from_location_asset_website":                                  schema.TypeFloat64,
	"metrics_all_conversions_from_menu":                                                    schema.TypeFloat64,
	"metrics_all_conversions_from_order":                                                   schema.TypeFloat64,
	"metrics_all_conversions_from_other_engagement":                                        schema.TypeFloat64,
	"metrics_all_conversions_from_store_visit":                                             schema.TypeFloat64,
	"metrics_all_conversions_from_store_website":                                           schema.TypeFloat64,
	"metrics_all_conversions_value":                                                        schema.TypeFloat64,
	"metrics_all_conversions_value_by_conversion_date":                                     schema.TypeFloat64,
	"metrics_all_conversions_value_per_cost":                                               schema.TypeFloat64,
	"metrics_all_new_customer_lifetime_value":                                              schema.TypeFloat64,
	"metrics_asset_best_performance_cost_percentage":                                       schema.TypeFloat64,
	"metrics_asset_best_performance_impression_percentage":                                 schema.TypeFloat64,
	"metrics_asset_good_performance_cost_percentage":                                       schema.TypeFloat64,
	"metrics_asset_good_performance_impression_percentage":                                 schema.TypeFloat64,
	"metrics_asset_learning_performance_cost_percentage":                                   schema.TypeFloat64,
	"metrics_asset_learning_performance_impression_percentage":                             schema.TypeFloat64,
	"metrics_asset_low_performance_cost_percentage":                                        schema.TypeFloat64,
	"metrics_asset_low_performance_impression_percentage":                                  schema.TypeFloat64,
	"metrics_asset_pinned_as_description_position_one_count":                               schema.TypeInt64,
	"metrics_asset_pinned_as_description_position_two_count":                               schema.TypeInt64,
	"metrics_asset_pinned_as_headline_position_one_count":                                  schema.TypeInt64,
	"metrics_asset_pinned_as_headline_position_three_count":                                schema.TypeInt64,
	"metrics_asset_pinned_as_headline_position_two_count":                                  schema.TypeInt64,
	"metrics_asset_pinned_total_count":                                                     schema.TypeInt64,
	"metrics_asset_unrated_performance_cost_percentage":                                    schema.TypeFloat64,
	"metrics_asset_unrated_performance_impression_percentage":                              schema.TypeFloat64,
	"metrics_auction_insight_search_absolute_top_impression_percentage":                    schema.TypeFloat64,
	"metrics_auction_insight_search_impression_share":                                      schema.TypeFloat64,
	"metrics_auction_insight_search_outranking_share":                                      schema.TypeFloat64,
	"metrics_auction_insight_search_overlap_rate":                                          schema.TypeFloat64,
	"metrics_auction_insight_search_position_above_rate":                                   schema.TypeFloat64,
	"metrics_auction_insight_search_top_impression_percentage":                             schema.TypeFloat64,
	"metrics_average_cart_size":                                                            schema.TypeFloat64,
	"metrics_average_cost":                                                                 schema.TypeFloat64,
	"metrics_average_cpc":                                                                  schema.TypeFloat64,
	"metrics_average_cpe":                                                                  schema.TypeFloat64,
	"metrics_average_cpm":                                                                  schema.TypeFloat64,
	"metrics_average_cpv":                                                                  schema.TypeFloat64,
	"metrics_average_impression_frequency_per_user":                                        schema.TypeFloat64,
	"metrics_average_order_value_micros":                                                   schema.TypeInt64,
	"metrics_average_page_views":                                                           schema.TypeFloat64,
	"metrics_average_target_cpa_micros":                                                    schema.TypeInt64,
	"metrics_average_target_roas":                                                          schema.TypeFloat64,
	"metrics_average_time_on_site":                                                         schema.TypeFloat64,
	"metrics_benchmark_average_max_cpc":                                                    schema.TypeFloat64,
	"metrics_benchmark_ctr":                                                                schema.TypeFloat64,
	"metrics_biddable_app_install_conversions":                                             schema.TypeFloat64,
	"metrics_biddable_app_post_install_conversions":                                        schema.TypeFloat64,
	"metrics_bounce_rate":                                                                  schema.TypeFloat64,
	"metrics_clicks":                                                                       schema.TypeInt64,
	"metrics_combined_clicks":                                                              schema.TypeInt64,
	"metrics_combined_clicks_per_query":                                                    schema.TypeFloat64,
	"metrics_combined_queries":                                                             schema.TypeInt64,
	"metrics_content_budget_lost_impression_share":                                         schema.TypeFloat64,
	"metrics_content_impression_share":                                                     schema.TypeFloat64,
	"metrics_content_rank_lost_impression_share":                                           schema.TypeFloat64,
	"metrics_conversion_last_conversion_date":                                              schema.TypeDate,
	"metrics_conversion_last_received_request_date_time":                                   schema.TypeDate,
	"metrics_conversions":                                                                  schema.TypeFloat64,
	"metrics_conversions_by_conversion_date":                                               schema.TypeFloat64,
	"metrics_conversions_from_interactions_rate":                                           schema.TypeFloat64,
	"metrics_conversions_from_interactions_value_per_interaction":                          schema.TypeFloat64,
	"metrics_conversions_value":                                                            schema.TypeFloat64,
	"metrics_conversions_value_by_conversion_date":                                         schema.TypeFloat64,
	"metrics_conversions_value_per_cost":                                                   schema.TypeFloat64,
	"metrics_cost_micros":                                                                  schema.TypeInt64,
	"metrics_cost_of_goods_sold_micros":                                                    schema.TypeInt64,
	"metrics_cost_per_all_conversions":                                                     schema.TypeFloat64,
	"metrics_cost_per_conversion":                                                          schema.TypeFloat64,
	"metrics_cost_per_current_model_attributed_conversion":                                 schema.TypeFloat64,
	"metrics_cross_device_conversions":                                                     schema.TypeFloat64,
	"metrics_cross_device_conversions_value_micros":                                        schema.TypeInt64,
	"metrics_cross_sell_cost_of_goods_sold_micros":                                         schema.TypeInt64,
	"metrics_cross_sell_gross_profit_micros":                                               schema.TypeInt64,
	"metrics_cross_sell_revenue_micros":                                                    schema.TypeInt64,
	"metrics_cross_sell_units_sold":                                                        schema.TypeFloat64,
	"metrics_ctr":                                                                          schema.TypeFloat64,
	"metrics_current_model_attributed_conversions":                                         schema.TypeFloat64,
	"metrics_current_model_attributed_conversions_from_interactions_rate":                  schema.TypeFloat64,
	"metrics_current_model_attributed_conversions_from_interactions_value_per_interaction": schema.TypeFloat64,
	"metrics_current_model_attributed_conversions_value":                                   schema.TypeFloat64,
	"metrics_current_model_attributed_conversions_value_per_cost":                          schema.TypeFloat64,
	"metrics_eligible_impressions_from_location_asset_store_reach":                         schema.TypeInt64,
	"metrics_engagement_rate":                                                              schema.TypeFloat64,
	"metrics_engagements":                                                                  schema.TypeInt64,
	"metrics_general_invalid_click_rate":                                                   schema.TypeFloat64,
	"metrics_general_invalid_clicks":                                                       schema.TypeInt64,
	"metrics_gmail_forwards":                                                               schema.TypeInt64,
	"metrics_gmail_saves":                                                                  schema.TypeInt64,
	"metrics_gmail_secondary_clicks":                                                       schema.TypeInt64,
	"metrics_gross_profit_margin":                                                          schema.TypeFloat64,
	"metrics_gross_profit_micros":                                                          schema.TypeInt64,
	"metrics_historical_creative_quality_score":                                            schema.TypeString,
	"metrics_historical_landing_page_quality_score":                                        schema.TypeString,
	"metrics_historical_quality_score":                                                     schema.TypeInt64,
	"metrics_historical_search_predicted_ctr":                                              schema.TypeString,
	"metrics_hotel_average_lead_value_micros":                                              schema.TypeFloat64,
	"metrics_hotel_commission_rate_micros":                                                 schema.TypeInt64,
	"metrics_hotel_eligible_impressions":                                                   schema.TypeInt64,
	"metrics_hotel_expected_commission_cost":                                               schema.TypeFloat64,
	"metrics_hotel_price_difference_percentage":                                            schema.TypeFloat64,
	"metrics_impressions":                                                                  schema.TypeInt64,
	"metrics_impressions_from_store_reach":                                                 schema.TypeInt64,
	"metrics_interaction_event_types":                                                      schema.TypeString,
	"metrics_interaction_rate":                                                             schema.TypeFloat64,
	"metrics_interactions":                                                                 schema.TypeInt64,
	"metrics_invalid_click_rate":                                                           schema.TypeFloat64,
	"metrics_invalid_clicks":                                                               schema.TypeInt64,
	"metrics_lead_cost_of_goods_sold_micros":                                               schema.TypeInt64,
	"metrics_lead_gross_profit_micros":                                                     schema.TypeInt64,
	"metrics_lead_revenue_micros":                                                          schema.TypeInt64,
	"metrics_lead_units_sold":                                                              schema.TypeFloat64,
	"metrics_linked_entities_count":                                                        schema.TypeInt64,
	"metrics_linked_sample_entities":                                                       schema.TypeString,
	"metrics_message_chat_rate":                                                            schema.TypeFloat64,
	"metrics_message_chats":                                                                schema.TypeInt64,
	"metrics_message_impressions":                                                          schema.TypeInt64,
	"metrics_mobile_friendly_clicks_percentage":                                            schema.TypeFloat64,
	"metrics_new_customer_lifetime_value":                                                  schema.TypeFloat64,
	"metrics_optimization_score_uplift":                                                    schema.TypeFloat64,
	"metrics_optimization_score_url":                                                       schema.TypeString,
	"metrics_orders":                                                                       schema.TypeFloat64,
	"metrics_organic_clicks":                                                               schema.TypeInt64,
	"metrics_organic_clicks_per_query":                                                     schema.TypeFloat64,
	"metrics_organic_impressions":                                                          schema.TypeInt64,
	"metrics_organic_impressions_per_query":                                                schema.TypeFloat64,
	"metrics_organic_queries":                                                              schema.TypeInt64,
	"metrics_percent_new_visitors":                                                         schema.TypeFloat64,
	"metrics_phone_calls":                                                                  schema.TypeInt64,
	"metrics_phone_impressions":                                                            schema.TypeInt64,
	"metrics_phone_through_rate":                                                           schema.TypeFloat64,
	"metrics_publisher_organic_clicks":                                                     schema.TypeInt64,
	"metrics_publisher_purchased_clicks":                                                   schema.TypeInt64,
	"metrics_publisher_unknown_clicks":                                                     schema.TypeInt64,
	"metrics_relative_ctr":                                                                 schema.TypeFloat64,
	"metrics_results_conversions_purchase":                                                 schema.TypeFloat64,
	"metrics_revenue_micros":                                                               schema.TypeInt64,
	"metrics_sample_best_performance_entities":                                             schema.TypeString,
	"metrics_sample_good_performance_entities":                                             schema.TypeString,
	"metrics_sample_learning_performance_entities":                                         schema.TypeString,
	"metrics_sample_low_performance_entities":                                              schema.TypeString,
	"metrics_sample_unrated_performance_entities":                                          schema.TypeString,
	"metrics_search_absolute_top_impression_share":                                         schema.TypeFloat64,
	"metrics_search_budget_lost_absolute_top_impression_share":                             schema.TypeFloat64,
	"metrics_search_budget_lost_impression_share":                                          schema.TypeFloat64,
	"metrics_search_budget_lost_top_impression_share":                                      schema.TypeFloat64,
	"metrics_search_click_share":                                                           schema.TypeFloat64,
	"metrics_search_exact_match_impression_share":                                          schema.TypeFloat64,
	"metrics_search_impression_share":                                                      schema.TypeFloat64,
	"metrics_search_rank_lost_absolute_top_impression_share":                               schema.TypeFloat64,
	"metrics_search_rank_lost_impression_share":                                            schema.TypeFloat64,
	"metrics_search_rank_lost_top_impression_share":                                        schema.TypeFloat64,
	"metrics_search_top_impression_share":                                                  schema.TypeFloat64,
	"metrics_sk_ad_network_installs":                                                       schema.TypeInt64,
	"metrics_sk_ad_network_total_conversions":                                              schema.TypeInt64,
	"metrics_speed_score":                                                                  schema.TypeInt64,
	"metrics_store_visits_last_click_model_attributed_conversions":                         schema.TypeFloat64,
	"metrics_top_impression_percentage":                                                    schema.TypeFloat64,
	"metrics_unique_users":                                                                 schema.TypeInt64,
	"metrics_units_sold":                                                                   schema.TypeFloat64,
	"metrics_valid_accelerated_mobile_pages_clicks_percentage":                             schema.TypeFloat64,
	"metrics_value_per_all_conversions":                                                    schema.TypeFloat64,
	"metrics_value_per_all_conversions_by_conversion_date":                                 schema.TypeFloat64,
	"metrics_value_per_conversion":                                                         schema.TypeFloat64,
	"metrics_value_per_conversions_by_conversion_date":                                     schema.TypeFloat64,
	"metrics_value_per_current_model_attributed_conversion":                                schema.TypeFloat64,
	"metrics_video_quartile_p100_rate":                                                     schema.TypeFloat64,
	"metrics_video_quartile_p25_rate":                                                      schema.TypeFloat64,
	"metrics_video_quartile_p50_rate":                                                      schema.TypeFloat64,
	"metrics_video_quartile_p75_rate":                                                      schema.TypeFloat64,
	"metrics_video_view_rate":                                                              schema.TypeFloat64,
	"metrics_video_view_rate_in_feed":                                                      schema.TypeFloat64,
	"metrics_video_view_rate_in_stream":                                                    schema.TypeFloat64,
	"metrics_video_view_rate_shorts":                                                       schema.TypeFloat64,
	"metrics_video_views":                                                                  schema.TypeInt64,
	"metrics_view_through_conversions":                                                     schema.TypeInt64,
	"metrics_view_through_conversions_from_location_asset_click_to_call":                   schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_directions":                      schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_menu":                            schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_order":                           schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_other_engagement":                schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_store_visits":                    schema.TypeFloat64,
	"metrics_view_through_conversions_from_location_asset_website":                         schema.TypeFloat64,
}
