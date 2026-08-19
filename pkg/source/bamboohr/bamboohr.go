package bamboohr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	employeePageSize = 2500
	locationPageSize = 500
	maxPages         = 10000
	requestTimeout   = 60 * time.Second

	// BambooHR publishes no numeric quota and may throttle frequent requests with
	// 503 + Retry-After. One request per second is a conservative client-side cap.
	rateLimit      = 1.0
	rateLimitBurst = 2
)

var (
	companyDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?$`)
	supportedTables      = []string{
		"employees",
		"employee_directory",
		"employee_fields",
		"users",
		"locations",
		"time_off_requests",
		"time_off_types",
		"time_off_default_hours",
		"time_off_policies",
		"timesheet_entries",
	}
)

type tableMeta struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

var tableMetadata = map[string]tableMeta{
	"employees":              {primaryKeys: []string{"employeeId"}, strategy: config.StrategyReplace},
	"employee_directory":     {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"employee_fields":        {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"users":                  {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"locations":              {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"time_off_requests":      {primaryKeys: []string{"id"}, incrementalKey: "start", strategy: config.StrategyMerge},
	"time_off_types":         {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"time_off_default_hours": {primaryKeys: []string{"name"}, strategy: config.StrategyReplace},
	"time_off_policies":      {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"timesheet_entries":      {primaryKeys: []string{"id"}, incrementalKey: "date", strategy: config.StrategyMerge},
}

type BambooHRSource struct {
	client          *httpclient.Client
	companyTimezone *time.Location
	usesOAuth       bool
	now             func() time.Time
}

type bambooHRCredentials struct {
	companyDomain string
	apiKey        string
	accessToken   string
	timezone      *time.Location
}

type tableParams struct {
	Fields []string `mapstructure:"fields"`
}

func NewBambooHRSource() *BambooHRSource {
	return &BambooHRSource{now: time.Now}
}

func (s *BambooHRSource) Schemes() []string {
	return []string{"bamboohr"}
}

func (s *BambooHRSource) HandlesIncrementality() bool {
	return true
}

func (s *BambooHRSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	var auth httpclient.Authenticator
	if creds.apiKey != "" {
		auth = httpclient.NewBasicAuth(creds.apiKey, "x")
	} else {
		auth = httpclient.NewBearerAuth(creds.accessToken)
	}
	s.client = newHTTPClient(fmt.Sprintf("https://%s.bamboohr.com", creds.companyDomain), auth)
	s.companyTimezone = creds.timezone
	s.usesOAuth = creds.accessToken != ""

	config.Debug("[BAMBOOHR] Connected to company domain: %s", creds.companyDomain)
	return nil
}

func newHTTPClient(baseURL string, auth httpclient.Authenticator, extraOptions ...httpclient.Option) *httpclient.Client {
	options := []httpclient.Option{
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(requestTimeout),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithAuth(auth),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithDebug(config.DebugMode),
	}
	options = append(options, extraOptions...)
	return httpclient.New(options...)
}

func (s *BambooHRSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(rawURI string) (bambooHRCredentials, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return bambooHRCredentials{}, fmt.Errorf("invalid bamboohr URI: %w", err)
	}
	if parsed.Scheme != "bamboohr" {
		return bambooHRCredentials{}, fmt.Errorf("invalid bamboohr URI: must start with bamboohr://")
	}

	companyDomain := strings.TrimSpace(parsed.Host)
	companyDomain = strings.TrimSuffix(companyDomain, ".bamboohr.com")
	if companyDomain == "" {
		return bambooHRCredentials{}, fmt.Errorf("company domain is required in bamboohr URI: bamboohr://<company-domain>?api_key=<api-key>")
	}
	if !companyDomainPattern.MatchString(companyDomain) {
		return bambooHRCredentials{}, fmt.Errorf("invalid BambooHR company domain %q", companyDomain)
	}

	query := parsed.Query()
	for key := range query {
		if key != "api_key" && key != "access_token" && key != "timezone" {
			return bambooHRCredentials{}, fmt.Errorf("unknown bamboohr URI parameter %q (supported: api_key, access_token, timezone)", key)
		}
	}
	apiKey := query.Get("api_key")
	accessToken := query.Get("access_token")
	if apiKey == "" && accessToken == "" {
		return bambooHRCredentials{}, fmt.Errorf("one of api_key or access_token is required in bamboohr URI")
	}
	if apiKey != "" && accessToken != "" {
		return bambooHRCredentials{}, fmt.Errorf("api_key and access_token are mutually exclusive in bamboohr URI")
	}

	var timezone *time.Location
	if timezoneName := query.Get("timezone"); timezoneName != "" {
		timezone, err = time.LoadLocation(timezoneName)
		if err != nil {
			return bambooHRCredentials{}, fmt.Errorf("invalid BambooHR company timezone %q: %w", timezoneName, err)
		}
	}

	return bambooHRCredentials{
		companyDomain: strings.ToLower(companyDomain),
		apiKey:        apiKey,
		accessToken:   accessToken,
		timezone:      timezone,
	}, nil
}

func parseTableSpec(raw string) (string, tableParams, error) {
	var params tableParams
	name, hasParams, err := tablespec.Parse(raw, &params, tablespec.WithListSeparator(","))
	if err != nil {
		return "", tableParams{}, fmt.Errorf("invalid BambooHR table: %w", err)
	}
	if hasParams && name != "employees" {
		return "", tableParams{}, fmt.Errorf("table parameters are only supported for employees")
	}
	return name, params, nil
}

func isValidTable(table string) bool {
	_, ok := tableMetadata[table]
	return ok
}

func (s *BambooHRSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, params, err := parseTableSpec(req.Name)
	if err != nil {
		return nil, err
	}
	meta, ok := tableMetadata[tableName]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}
	if tableName == "locations" && !s.usesOAuth {
		return nil, fmt.Errorf("locations requires access_token authentication with the BambooHR field scope")
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("bamboohr source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, params, opts)
		},
	}, nil
}

func (s *BambooHRSource) read(ctx context.Context, table string, params tableParams, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "employees":
			err = s.readEmployees(ctx, params.Fields, opts, results)
		case "employee_directory":
			err = s.readEmployeeDirectory(ctx, opts, results)
		case "employee_fields":
			err = s.readEmployeeFields(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "locations":
			err = s.readLocations(ctx, opts, results)
		case "time_off_requests":
			err = s.readTimeOffRequests(ctx, opts, results)
		case "time_off_types":
			err = s.readTimeOffTypes(ctx, opts, results)
		case "time_off_default_hours":
			err = s.readTimeOffDefaultHours(ctx, opts, results)
		case "time_off_policies":
			err = s.readTimeOffPolicies(ctx, opts, results)
		case "timesheet_entries":
			err = s.readTimesheetEntries(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			select {
			case results <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return results, nil
}

func (s *BambooHRSource) readEmployees(ctx context.Context, fields []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading employees")
	cursor := ""
	seenCursors := make(map[string]struct{})

	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).SetQueryParam("page[limit]", strconv.Itoa(employeePageSize))
		if cursor != "" {
			req.SetQueryParam("page[after]", cursor)
		}
		if len(fields) > 0 {
			req.SetQueryParam("fields", strings.Join(fields, ","))
		}

		resp, err := req.Get("/api/v1/employees")
		if err != nil {
			return fmt.Errorf("failed to fetch employees: %w", err)
		}
		if err := responseError(resp, "employees"); err != nil {
			return err
		}

		var payload struct {
			Data []map[string]interface{} `json:"data"`
			Meta struct {
				Page struct {
					NextCursor *string `json:"nextCursor"`
				} `json:"page"`
			} `json:"meta"`
		}
		if err := jsonUseNumber(resp.Body(), &payload); err != nil {
			return fmt.Errorf("failed to parse employees response: %w", err)
		}
		if err := sendItems(ctx, "employees", payload.Data, opts, results); err != nil {
			return err
		}

		if payload.Meta.Page.NextCursor == nil || *payload.Meta.Page.NextCursor == "" {
			return nil
		}
		next := *payload.Meta.Page.NextCursor
		if _, exists := seenCursors[next]; exists {
			return fmt.Errorf("employees pagination returned repeated cursor %q", next)
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}

	config.Debug("[BAMBOOHR] employees pagination stopped at maxPages=%d", maxPages)
	return fmt.Errorf("employees pagination exceeded maximum of %d pages", maxPages)
}

func (s *BambooHRSource) readEmployeeDirectory(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading employee_directory")
	resp, err := s.client.R(ctx).
		SetQueryParam("onlyCurrent", "false").
		Get("/api/v1/employees/directory")
	if err != nil {
		return fmt.Errorf("failed to fetch employee_directory: %w", err)
	}
	if resp.StatusCode() == 404 {
		return nil
	}
	if err := responseError(resp, "employee_directory"); err != nil {
		return err
	}

	var payload struct {
		Employees []map[string]interface{} `json:"employees"`
	}
	if err := jsonUseNumber(resp.Body(), &payload); err != nil {
		return fmt.Errorf("failed to parse employee_directory response: %w", err)
	}
	return sendItems(ctx, "employee_directory", payload.Employees, opts, results)
}

func (s *BambooHRSource) readEmployeeFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading employee_fields")
	items, err := s.getArray(ctx, "/api/v1/meta/fields", "employee_fields", nil)
	if err != nil {
		return err
	}
	for _, item := range items {
		if id, ok := item["id"]; ok {
			item["id"] = fmt.Sprint(id)
		}
	}
	return sendItems(ctx, "employee_fields", items, opts, results)
}

func (s *BambooHRSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading users")
	resp, err := s.client.R(ctx).Get("/api/v1/meta/users")
	if err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}
	if err := responseError(resp, "users"); err != nil {
		return err
	}

	var keyedUsers map[string]map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &keyedUsers); err != nil {
		return fmt.Errorf("failed to parse users response: %w", err)
	}
	keys := make([]string, 0, len(keyedUsers))
	for key := range keyedUsers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		item := keyedUsers[key]
		item["id"] = key
		items = append(items, item)
	}
	return sendItems(ctx, "users", items, opts, results)
}

func (s *BambooHRSource) readLocations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading locations")
	for _, archived := range []bool{false, true} {
		if err := s.readLocationState(ctx, archived, opts, results); err != nil {
			return err
		}
	}
	return nil
}

func (s *BambooHRSource) readLocationState(ctx context.Context, archived bool, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := s.client.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetQueryParam("pageSize", strconv.Itoa(locationPageSize)).
			SetQueryParam("filter", fmt.Sprintf("archived eq %t", archived)).
			SetQueryParam("expand", "state,country").
			Get("/api/v1/hris/org/locations")
		if err != nil {
			return fmt.Errorf("failed to fetch locations: %w", err)
		}
		if err := responseError(resp, "locations"); err != nil {
			return err
		}

		var payload struct {
			Data []map[string]interface{} `json:"data"`
			Meta struct {
				TotalPages int `json:"totalPages"`
			} `json:"meta"`
		}
		if err := jsonUseNumber(resp.Body(), &payload); err != nil {
			return fmt.Errorf("failed to parse locations response: %w", err)
		}
		if err := sendItems(ctx, "locations", payload.Data, opts, results); err != nil {
			return err
		}
		if payload.Meta.TotalPages == 0 || page+1 >= payload.Meta.TotalPages {
			return nil
		}
	}

	config.Debug("[BAMBOOHR] locations pagination stopped at maxPages=%d for archived=%t", maxPages, archived)
	return fmt.Errorf("locations pagination exceeded maximum of %d pages for archived=%t", maxPages, archived)
}

func (s *BambooHRSource) readTimeOffRequests(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading time_off_requests")
	start, end, err := timeOffWindow(opts)
	if err != nil {
		return err
	}
	items, err := s.getArray(ctx, "/api/v1/time_off/requests", "time_off_requests", map[string]string{
		"start": start,
		"end":   end,
	})
	if err != nil {
		return err
	}
	return sendItems(ctx, "time_off_requests", items, opts, results)
}

type timeOffTypesResponse struct {
	TimeOffTypes []map[string]interface{} `json:"timeOffTypes"`
	DefaultHours []map[string]interface{} `json:"defaultHours"`
}

func (s *BambooHRSource) getTimeOffTypes(ctx context.Context) (timeOffTypesResponse, error) {
	resp, err := s.client.R(ctx).Get("/api/v1/meta/time_off/types")
	if err != nil {
		return timeOffTypesResponse{}, fmt.Errorf("failed to fetch time_off_types: %w", err)
	}
	if err := responseError(resp, "time_off_types"); err != nil {
		return timeOffTypesResponse{}, err
	}

	var payload timeOffTypesResponse
	if err := jsonUseNumber(resp.Body(), &payload); err != nil {
		return timeOffTypesResponse{}, fmt.Errorf("failed to parse time_off_types response: %w", err)
	}
	return payload, nil
}

func (s *BambooHRSource) readTimeOffTypes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading time_off_types")
	payload, err := s.getTimeOffTypes(ctx)
	if err != nil {
		return err
	}
	return sendItems(ctx, "time_off_types", payload.TimeOffTypes, opts, results)
}

func (s *BambooHRSource) readTimeOffDefaultHours(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading time_off_default_hours")
	payload, err := s.getTimeOffTypes(ctx)
	if err != nil {
		return err
	}
	return sendItems(ctx, "time_off_default_hours", payload.DefaultHours, opts, results)
}

func (s *BambooHRSource) readTimeOffPolicies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading time_off_policies")
	items, err := s.getArray(ctx, "/api/v1/meta/time_off/policies", "time_off_policies", nil)
	if err != nil {
		return err
	}
	return sendItems(ctx, "time_off_policies", items, opts, results)
}

func (s *BambooHRSource) readTimesheetEntries(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[BAMBOOHR] reading timesheet_entries")
	if s.companyTimezone == nil {
		return fmt.Errorf("timezone is required in the BambooHR URI when reading timesheet_entries")
	}
	start, end, err := timesheetWindow(opts, s.now(), s.companyTimezone)
	if err != nil {
		return err
	}
	items, err := s.getArray(ctx, "/api/v1/time_tracking/timesheet_entries", "timesheet_entries", map[string]string{
		"start": start,
		"end":   end,
	})
	if err != nil {
		return err
	}
	return sendItems(ctx, "timesheet_entries", items, opts, results)
}

func (s *BambooHRSource) getArray(ctx context.Context, endpoint, label string, params map[string]string) ([]map[string]interface{}, error) {
	req := s.client.R(ctx)
	if params != nil {
		req.SetQueryParams(params)
	}
	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", label, err)
	}
	if err := responseError(resp, label); err != nil {
		return nil, err
	}

	var items []map[string]interface{}
	if err := jsonUseNumber(resp.Body(), &items); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", label, err)
	}
	return items, nil
}

func responseError(resp *httpclient.Response, label string) error {
	if resp.IsSuccess() {
		return nil
	}
	body := strings.TrimSpace(resp.String())
	message := strings.TrimSpace(resp.Header().Get("X-BambooHR-Error-Message"))
	if message != "" && body != "" {
		return fmt.Errorf("BambooHR %s endpoint returned status %d: %s (%s)", label, resp.StatusCode(), message, body)
	}
	if message != "" {
		return fmt.Errorf("BambooHR %s endpoint returned status %d: %s", label, resp.StatusCode(), message)
	}
	return fmt.Errorf("BambooHR %s endpoint returned status %d: %s", label, resp.StatusCode(), body)
}

func sendItems(ctx context.Context, label string, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	if len(items) == 0 {
		return nil
	}
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: record}:
		return nil
	case <-ctx.Done():
		record.Release()
		return ctx.Err()
	}
}

func jsonUseNumber(data []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func timeOffWindow(opts source.ReadOptions) (string, string, error) {
	start := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
	if opts.IntervalStart != nil {
		start = calendarDate(*opts.IntervalStart)
	}
	if opts.IntervalEnd != nil {
		end = calendarDate(*opts.IntervalEnd)
	}
	if start.After(end) {
		return "", "", fmt.Errorf("invalid time_off_requests interval: start date must not be after end date")
	}
	return start.Format(time.DateOnly), end.Format(time.DateOnly), nil
}

func timesheetWindow(opts source.ReadOptions, now time.Time, companyTimezone *time.Location) (string, string, error) {
	today := calendarDate(now.In(companyTimezone))
	end := today
	if opts.IntervalEnd != nil {
		end = calendarDate(*opts.IntervalEnd)
	}
	start := end.AddDate(0, 0, -364)
	if opts.IntervalStart != nil {
		start = calendarDate(*opts.IntervalStart)
	}

	if start.After(end) {
		return "", "", fmt.Errorf("invalid timesheet_entries interval: start date must not be after end date")
	}
	if end.After(today) {
		return "", "", fmt.Errorf("invalid timesheet_entries interval: end date cannot be in the future")
	}
	oldestAllowed := today.AddDate(0, 0, -364)
	if start.Before(oldestAllowed) {
		return "", "", fmt.Errorf("invalid timesheet_entries interval: BambooHR only exposes the last 365 days")
	}
	if end.Sub(start) > 364*24*time.Hour {
		return "", "", fmt.Errorf("invalid timesheet_entries interval: date range cannot exceed 365 days")
	}

	return start.Format(time.DateOnly), end.Format(time.DateOnly), nil
}

func calendarDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
