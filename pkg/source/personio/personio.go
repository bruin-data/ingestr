package personio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL            = "https://api.personio.de/v1/"
	defaultPageSize    = 200
	defaultParallelism = 5
)

type PersonioSource struct {
	client       *gonghttp.Client
	clientID     string
	clientSecret string
	token        string
}

func NewPersonioSource() *PersonioSource {
	return &PersonioSource{}
}

func (s *PersonioSource) Schemes() []string {
	return []string{"personio"}
}

func (s *PersonioSource) Connect(ctx context.Context, uri string) error {
	clientID, clientSecret, err := parsePersonioURI(uri)
	if err != nil {
		return err
	}

	s.clientID = clientID
	s.clientSecret = clientSecret

	s.token, err = s.getToken(ctx, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("failed to authenticate with Personio: %w", err)
	}

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(s.token)),
	)

	config.Debug("[PERSONIO] Connected successfully")
	return nil
}

func parsePersonioURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "personio://") {
		return "", "", fmt.Errorf("invalid personio URI: must start with personio://")
	}

	rest := strings.TrimPrefix(uri, "personio://")
	if rest == "" || rest == "?" {
		return "", "", fmt.Errorf("client_id and client_secret are required in personio URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse personio URI query: %w", err)
	}

	clientID := values.Get("client_id")
	if clientID == "" {
		return "", "", fmt.Errorf("client_id is required in personio URI")
	}

	clientSecret := values.Get("client_secret")
	if clientSecret == "" {
		return "", "", fmt.Errorf("client_secret is required in personio URI")
	}

	return clientID, clientSecret, nil
}

func (s *PersonioSource) getToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	authClient := gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(30*time.Second),
	)
	defer func() { _ = authClient.Close() }()

	resp, err := authClient.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetBody(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
		}).
		Post("auth")
	if err != nil {
		return "", fmt.Errorf("auth request failed: %w", err)
	}

	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("auth failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return "", fmt.Errorf("failed to parse auth response: %w", err)
	}

	data, _ := response["data"].(map[string]interface{})
	token, _ := data["token"].(string)
	if token == "" {
		return "", fmt.Errorf("auth response missing token")
	}

	config.Debug("[PERSONIO] Authenticated successfully")
	return token, nil
}

func (s *PersonioSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PersonioSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

var supportedTables = map[string]tableMeta{
	"employees":                  {primaryKeys: []string{"id"}, incrementalKey: "last_modified_at", strategy: config.StrategyMerge},
	"absence_types":              {primaryKeys: []string{"id"}, incrementalKey: "", strategy: config.StrategyReplace},
	"absences":                   {primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge},
	"attendances":                {primaryKeys: []string{"id"}, incrementalKey: "updated_at", strategy: config.StrategyMerge},
	"projects":                   {primaryKeys: []string{"id"}, incrementalKey: "", strategy: config.StrategyReplace},
	"document_categories":        {primaryKeys: []string{"id"}, incrementalKey: "", strategy: config.StrategyReplace},
	"employees_absences_balance": {primaryKeys: []string{"employee_id", "id"}, incrementalKey: "", strategy: config.StrategyMerge},
	"custom_reports_list":        {primaryKeys: []string{"id"}, incrementalKey: "", strategy: config.StrategyReplace},
	"custom_reports":             {primaryKeys: []string{"report_id", "item_id"}, incrementalKey: "", strategy: config.StrategyMerge},
}

func (s *PersonioSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	meta, exists := supportedTables[tableName]
	if !exists {
		tables := make([]string, 0, len(supportedTables))
		for t := range supportedTables {
			tables = append(tables, t)
		}
		return nil, fmt.Errorf("unsupported table: %s, supported tables: %v", tableName, tables)
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("personio source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *PersonioSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "employees":
			err = s.readEmployees(ctx, opts, results)
		case "absence_types":
			err = s.readAbsenceTypes(ctx, opts, results)
		case "absences":
			err = s.readAbsences(ctx, opts, results)
		case "attendances":
			err = s.readAttendances(ctx, opts, results)
		case "projects":
			err = s.readProjects(ctx, opts, results)
		case "document_categories":
			err = s.readDocumentCategories(ctx, opts, results)
		case "employees_absences_balance":
			err = s.readEmployeesAbsencesBalance(ctx, opts, results)
		case "custom_reports_list":
			err = s.readCustomReportsList(ctx, opts, results)
		case "custom_reports":
			err = s.readCustomReports(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

type fetchAllConfig struct {
	resource     string
	params       map[string]string
	offsetByPage bool
	transformFn  func([]map[string]interface{}) []map[string]interface{}
}

func (s *PersonioSource) paginate(ctx context.Context, cfg fetchAllConfig, pageSize int, handlePage func(items []map[string]interface{}) (done bool, err error)) error {
	startVal := 0
	if cfg.offsetByPage {
		startVal = 1
	}
	offset := startVal
	page := startVal
	startsFromZero := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("offset", strconv.Itoa(offset)).
			SetQueryParam("page", strconv.Itoa(page))

		for k, v := range cfg.params {
			req.SetQueryParam(k, v)
		}

		resp, err := req.Get(cfg.resource)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", cfg.resource, err)
		}

		if resp.StatusCode() >= 400 {
			return fmt.Errorf("personio API error %d for %s: %s", resp.StatusCode(), cfg.resource, resp.String())
		}

		var pageResp struct {
			Success  bool                     `json:"success"`
			Data     []map[string]interface{} `json:"data"`
			Metadata *struct {
				TotalElements int `json:"total_elements"`
				CurrentPage   int `json:"current_page"`
				TotalPages    int `json:"total_pages"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(resp.Body(), &pageResp); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", cfg.resource, err)
		}

		if !pageResp.Success {
			return fmt.Errorf("personio API returned success=false for %s", cfg.resource)
		}

		if len(pageResp.Data) == 0 {
			break
		}

		items := pageResp.Data
		if cfg.transformFn != nil {
			items = cfg.transformFn(items)
		}

		if len(items) == 0 {
			break
		}

		done, err := handlePage(items)
		if err != nil {
			return err
		}
		if done {
			break
		}

		if pageResp.Metadata == nil {
			break
		}

		if pageResp.Metadata.CurrentPage == 0 {
			startsFromZero = true
		}

		lastPage := pageResp.Metadata.TotalPages
		if startsFromZero {
			lastPage--
		}
		if pageResp.Metadata.CurrentPage >= lastPage {
			break
		}

		if cfg.offsetByPage {
			offset++
		} else {
			offset += pageSize
		}
		page++
	}

	return nil
}

func (s *PersonioSource) fetchAll(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, cfg fetchAllConfig) error {
	totalSent := 0
	batchNum := 0

	return s.paginate(ctx, cfg, defaultPageSize, func(items []map[string]interface{}) (bool, error) {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return true, fmt.Errorf("failed to convert %s to Arrow: %w", cfg.resource, err)
		}

		batchNum++
		totalSent += len(items)
		config.Debug("[PERSONIO] Sending batch %d with %d %s (total: %d)", batchNum, len(items), cfg.resource, totalSent)
		results <- source.RecordBatchResult{Batch: record}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[PERSONIO] Reached limit of %d %s", opts.Limit, cfg.resource)
			return true, nil
		}
		return false, nil
	})
}

func (s *PersonioSource) readEmployees(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{"limit": strconv.Itoa(defaultPageSize)}
	if t, err := toTime(opts.IntervalStart); err == nil {
		params["updated_since"] = t.Format("2006-01-02T15:04:05")
	}

	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:    "company/employees",
		params:      params,
		transformFn: flattenEmployeeAttributes,
	})
}

func (s *PersonioSource) readAbsenceTypes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:    "company/time-off-types",
		params:      map[string]string{"limit": strconv.Itoa(defaultPageSize)},
		transformFn: extractAttributes,
	})
}

func (s *PersonioSource) readAbsences(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{"limit": strconv.Itoa(defaultPageSize)}
	if t, err := toTime(opts.IntervalStart); err == nil {
		params["updated_since"] = t.Format("2006-01-02T15:04:05")
	}

	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:     "company/time-offs",
		params:       params,
		offsetByPage: true,
		transformFn:  extractAttributes,
	})
}

func (s *PersonioSource) readAttendances(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startDate, err := toTime(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("start_date is required for personio attendances: provide --interval-start")
	}

	endDate, err := toTime(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("end_date is required for personio attendances: provide --interval-end")
	}

	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource: "company/attendances",
		params: map[string]string{
			"limit":          strconv.Itoa(defaultPageSize),
			"start_date":     startDate.Format("2006-01-02"),
			"end_date":       endDate.Format("2006-01-02"),
			"updated_from":   startDate.Format("2006-01-02T15:04:05"),
			"includePending": "true",
		},
		transformFn: extractAttributesAddID,
	})
}

func (s *PersonioSource) readProjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:     "company/attendances/projects",
		offsetByPage: false,
		transformFn:  extractAttributesAddID,
	})
}

func (s *PersonioSource) readDocumentCategories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:     "company/document-categories",
		offsetByPage: false,
		transformFn:  extractAttributesAddID,
	})
}

func (s *PersonioSource) fetchIDs(ctx context.Context, cfg fetchAllConfig, idChan chan<- string) error {
	return s.paginate(ctx, cfg, defaultPageSize, func(items []map[string]interface{}) (bool, error) {
		for _, item := range items {
			if id, ok := item["id"]; ok {
				idChan <- fmt.Sprintf("%v", id)
			}
		}
		return false, nil
	})
}

func (s *PersonioSource) readEmployeesAbsencesBalance(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	parallelism := defaultParallelism
	if opts.Parallelism > 0 {
		parallelism = opts.Parallelism
	}

	idChan := make(chan string, defaultParallelism)
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for empID := range idChan {
				select {
				case <-workerCtx.Done():
					return
				default:
				}
				err := s.fetchAll(workerCtx, opts, results, fetchAllConfig{
					resource: "company/employees/" + empID + "/absences/balance",
					transformFn: func(items []map[string]interface{}) []map[string]interface{} {
						for j := range items {
							items[j]["employee_id"] = empID
						}
						return items
					},
				})
				if err != nil {
					select {
					case errChan <- fmt.Errorf("failed to fetch balance for employee %s: %w", empID, err):
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

	fetchErr := s.fetchIDs(workerCtx, fetchAllConfig{
		resource:    "company/employees",
		params:      map[string]string{"limit": strconv.Itoa(defaultPageSize)},
		transformFn: flattenEmployeeAttributes,
	}, idChan)
	close(idChan)

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
	}

	if fetchErr != nil {
		return fmt.Errorf("failed to fetch employees for absences balance: %w", fetchErr)
	}

	return nil
}

func (s *PersonioSource) readCustomReportsList(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.fetchAll(ctx, opts, results, fetchAllConfig{
		resource:     "company/custom-reports/reports",
		offsetByPage: false,
		transformFn:  extractAttributes,
	})
}

func (s *PersonioSource) readCustomReports(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	parallelism := defaultParallelism
	if opts.Parallelism > 0 {
		parallelism = opts.Parallelism
	}

	idChan := make(chan string, defaultParallelism)
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for reportID := range idChan {
				select {
				case <-workerCtx.Done():
					return
				default:
				}
				err := s.fetchAll(workerCtx, opts, results, fetchAllConfig{
					resource:     "company/custom-reports/reports/" + reportID,
					params:       map[string]string{"limit": strconv.Itoa(defaultPageSize)},
					offsetByPage: true,
					transformFn: func(items []map[string]interface{}) []map[string]interface{} {
						return convertCustomReportItems(items, reportID)
					},
				})
				if err != nil {
					select {
					case errChan <- fmt.Errorf("failed to fetch custom report %s: %w", reportID, err):
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

	fetchErr := s.fetchIDs(workerCtx, fetchAllConfig{
		resource:    "company/custom-reports/reports",
		transformFn: extractAttributes,
	}, idChan)
	close(idChan)

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
	}

	if fetchErr != nil {
		return fmt.Errorf("failed to fetch custom reports list: %w", fetchErr)
	}

	return nil
}

func convertCustomReportItems(items []map[string]interface{}, reportID string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, item := range items {
		attrs, ok := item["attributes"].(map[string]interface{})
		if !ok {
			continue
		}
		reportItems, ok := attrs["items"].([]interface{})
		if !ok {
			continue
		}
		for _, ri := range reportItems {
			riMap, ok := ri.(map[string]interface{})
			if !ok {
				continue
			}

			row := map[string]interface{}{
				"report_id": reportID,
			}

			riAttrs, _ := riMap["attributes"].([]interface{})
			delete(riMap, "attributes")
			if id, ok := riMap["id"]; ok {
				row["item_id"] = id
			} else {
				row["item_id"] = fmt.Sprintf("%s_%d", reportID, len(out))
			}

			for _, a := range riAttrs {
				aMap, ok := a.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := aMap["attribute_id"].(string)
				if name == "" {
					continue
				}
				row[name] = aMap["value"]
			}

			out = append(out, row)
		}
	}
	return out
}

func flattenEmployeeAttributes(items []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		attrs, ok := item["attributes"].(map[string]interface{})
		if !ok {
			continue
		}
		row := make(map[string]interface{})
		for _, v := range attrs {
			attr, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := attr["universal_id"].(string)
			if name == "" {
				label, _ := attr["label"].(string)
				name = strings.ToLower(strings.ReplaceAll(label, " ", "_"))
			}
			if name == "" {
				continue
			}
			row[name] = attr["value"]
		}
		out = append(out, row)
	}
	return out
}

func extractAttributes(items []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if attrs, ok := item["attributes"].(map[string]interface{}); ok {
			out = append(out, attrs)
		}
	}
	return out
}

func extractAttributesAddID(items []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row := map[string]interface{}{}
		if attrs, ok := item["attributes"].(map[string]interface{}); ok {
			for k, v := range attrs {
				row[k] = v
			}
		}
		if id, ok := item["id"]; ok {
			row["id"] = id
		}
		out = append(out, row)
	}
	return out
}

func toTime(v interface{}) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("nil value")
	}
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		if t == nil {
			return time.Time{}, fmt.Errorf("nil pointer")
		}
		return *t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported type %T", v)
}

var _ source.Source = (*PersonioSource)(nil)
