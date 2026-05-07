package fluxx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/net/publicsuffix"
)

const (
	fluxxOAuthTokenPath = "/oauth/token"
	fluxxAPIV2Path      = "/api/rest/v2"
)

type FluxxSource struct {
	client       *gonghttp.Client
	instance     string
	clientID     string
	clientSecret string
	accessToken  string
	baseURL      string
}

type parsedTableSpec struct {
	resources    []string
	customFields map[string][]string
}

func NewFluxxSource() *FluxxSource {
	return &FluxxSource{}
}

func (s *FluxxSource) Schemes() []string {
	return []string{"fluxx"}
}

func (s *FluxxSource) Connect(ctx context.Context, uri string) error {
	instance, clientID, clientSecret, err := parseFluxxURI(uri)
	if err != nil {
		return err
	}

	s.baseURL, err = getBaseURL(instance)
	if err != nil {
		return fmt.Errorf("failed to determine base URL: %w", err)
	}

	s.instance = instance
	s.clientID = clientID
	s.clientSecret = clientSecret

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(s.baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
	)

	config.Debug("[FLUXX] Connected to: %s (base URL: %s)", uri, s.baseURL)
	return nil
}

func parseFluxxURI(uri string) (instance, clientID, clientSecret string, err error) {
	uriLower := strings.ToLower(uri)
	if strings.Contains(uriLower, "http://") || strings.Contains(uriLower, "https://") {
		return "", "", "", fmt.Errorf("invalid Fluxx URI format: do not include http:// or https:// in the URI")
	}

	if !strings.HasPrefix(uri, "fluxx://") {
		return "", "", "", fmt.Errorf("invalid fluxx URI: must start with fluxx://")
	}

	rest := strings.TrimPrefix(uri, "fluxx://")
	if rest == "" || rest == "?" {
		return "", "", "", fmt.Errorf("instance is required in the URI (e.g., fluxx://mycompany.preprod)")
	}

	parts := strings.SplitN(rest, "?", 2)
	instance = parts[0]

	if instance == "" {
		return "", "", "", fmt.Errorf("instance is required in the URI (e.g., fluxx://mycompany.preprod)")
	}

	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("client_id and client_secret are required in URI query parameters")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse fluxx URI query: %w", err)
	}

	clientID = values.Get("client_id")
	if clientID == "" {
		return "", "", "", fmt.Errorf("client_id in the URI is required to connect to Fluxx")
	}

	clientSecret = values.Get("client_secret")
	if clientSecret == "" {
		return "", "", "", fmt.Errorf("client_secret in the URI is required to connect to Fluxx")
	}

	return instance, clientID, clientSecret, nil
}

func (s *FluxxSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FluxxSource) HandlesIncrementality() bool {
	return true
}

func (s *FluxxSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	if tableName == "" {
		return nil, fmt.Errorf("table name is required for fluxx source")
	}

	parsed, err := parseTableSpec(tableName)
	if err != nil {
		return nil, err
	}

	for _, resourceName := range parsed.resources {
		if _, exists := fluxxResources[resourceName]; !exists {
			supported := make([]string, 0, len(fluxxResources))
			for name := range fluxxResources {
				supported = append(supported, name)
			}
			sort.Strings(supported)
			return nil, fmt.Errorf("unsupported table: %s (supported: %s)", resourceName, strings.Join(supported, ", "))
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("fluxx source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, parsed, opts)
		},
	}, nil
}

func parseTableSpec(table string) (*parsedTableSpec, error) {
	if table == "" {
		return nil, fmt.Errorf("table specification is required")
	}

	result := &parsedTableSpec{
		customFields: make(map[string][]string),
	}

	if strings.Contains(table, ":") && strings.Count(table, ":") == 1 {
		parts := strings.SplitN(table, ":", 2)
		resourceName := strings.TrimSpace(parts[0])
		fieldList := strings.TrimSpace(parts[1])

		if resourceName == "" {
			return nil, fmt.Errorf("resource name is required in table specification")
		}

		fields := strings.Split(fieldList, ",")
		trimmedFields := make([]string, 0, len(fields))
		for _, f := range fields {
			trimmed := strings.TrimSpace(f)
			if trimmed != "" {
				trimmedFields = append(trimmedFields, trimmed)
			}
		}

		if len(trimmedFields) == 0 {
			return nil, fmt.Errorf("at least one field is required in custom field specification")
		}

		result.resources = []string{resourceName}
		result.customFields[resourceName] = trimmedFields
	} else {
		resources := strings.Split(table, ",")
		trimmedResources := make([]string, 0, len(resources))
		for _, r := range resources {
			trimmed := strings.TrimSpace(r)
			if trimmed != "" {
				trimmedResources = append(trimmedResources, trimmed)
			}
		}

		if len(trimmedResources) == 0 {
			return nil, fmt.Errorf("at least one resource is required in table specification")
		}

		result.resources = trimmedResources
	}

	return result, nil
}

type resourceTask struct {
	name            string
	endpoint        string
	columns         []schema.Column
	fieldsToExtract map[string]schema.Column
}

func columnsToMap(columns []schema.Column) map[string]schema.Column {
	m := make(map[string]schema.Column, len(columns))
	for _, c := range columns {
		m[c.Name] = c
	}
	return m
}

func (s *FluxxSource) read(ctx context.Context, parsed *parsedTableSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if len(parsed.resources) == 0 {
		resources := make([]string, 0, len(fluxxResources))
		for resource := range fluxxResources {
			resources = append(resources, resource)
		}
		parsed.resources = resources
	}

	// If custom_fields is nil, initialize as empty map
	if parsed.customFields == nil {
		parsed.customFields = make(map[string][]string)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		s.accessToken, err = s.getAccessToken(ctx)
		if err != nil {
			results <- source.RecordBatchResult{
				Err: fmt.Errorf("failed to get access token: %w", err),
			}
			return
		}

		config.Debug("[FLUXX] Reading resources: %v", parsed.resources)
		config.Debug("[FLUXX] Custom fields: %v", parsed.customFields)

		parallelism := opts.Parallelism
		if parallelism <= 0 {
			parallelism = 5
		}

		var tasks []resourceTask
		for _, resourceName := range parsed.resources {
			if _, exists := fluxxResources[resourceName]; !exists {
				config.Debug("[FLUXX] Skipping unknown resource: %s", resourceName)
				continue
			}

			resourceConfig := fluxxResources[resourceName]
			columns := resourceConfig.Columns
			fieldsToExtract := columnsToMap(columns)

			if customFieldNames, hasCustomFields := parsed.customFields[resourceName]; hasCustomFields {
				// Always include 'id' field for primary key
				hasID := false
				for _, name := range customFieldNames {
					if name == "id" {
						hasID = true
						break
					}
				}
				if !hasID {
					customFieldNames = append([]string{"id"}, customFieldNames...)
				}

				var filteredColumns []schema.Column
				for _, fieldName := range customFieldNames {
					if col, exists := fieldsToExtract[fieldName]; exists {
						filteredColumns = append(filteredColumns, col)
					} else {
						filteredColumns = append(filteredColumns, schema.Column{
							Name:     fieldName,
							DataType: schema.TypeUnknown,
							Nullable: true,
						})
					}
				}
				columns = filteredColumns
				fieldsToExtract = columnsToMap(columns)
			}

			tasks = append(tasks, resourceTask{
				name:            resourceName,
				endpoint:        resourceConfig.Endpoint,
				fieldsToExtract: fieldsToExtract,
				columns:         columns,
			})
		}

		if len(tasks) == 0 {
			return
		}

		workerCtx, cancelWorkers := context.WithCancel(ctx)
		defer cancelWorkers()

		taskChan := make(chan resourceTask, len(tasks))
		errChan := make(chan error, 1)

		workerCount := parallelism
		if workerCount > len(tasks) {
			workerCount = len(tasks)
		}

		config.Debug("[FLUXX] Starting %d parallel workers for %d resources", workerCount, len(tasks))

		var wg sync.WaitGroup
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for task := range taskChan {
					select {
					case <-workerCtx.Done():
						return
					default:
					}

					if err := s.readResource(workerCtx, task.name, task.endpoint, s.accessToken, task.columns, task.fieldsToExtract, results, opts); err != nil {
						select {
						case errChan <- fmt.Errorf("failed to read resource %s: %w", task.name, err):
						default:
						}
						cancelWorkers()
						return
					}
				}
			}()
		}

		go func() {
			defer close(taskChan)
			for _, task := range tasks {
				select {
				case taskChan <- task:
				case <-workerCtx.Done():
					return
				}
			}
		}()

		wg.Wait()
		close(errChan)

		if err := <-errChan; err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *FluxxSource) readResource(ctx context.Context, resourceName, endpoint, accessToken string, columns []schema.Column, fieldsToExtract map[string]schema.Column, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
	var fieldNames []string
	if len(fieldsToExtract) > 0 {
		fieldNames = make([]string, 0, len(fieldsToExtract))
		for fieldName := range fieldsToExtract {
			fieldNames = append(fieldNames, fieldName)
		}
	}

	// Marshal field names once before the loop
	var colsJSONStr string
	if len(fieldNames) > 0 {
		colsJSON, err := json.Marshal(fieldNames)
		if err != nil {
			return fmt.Errorf("failed to marshal field names: %w", err)
		}
		colsJSONStr = string(colsJSON)
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	apiPageSize := 100
	if batchSize < apiPageSize {
		apiPageSize = batchSize
	}

	totalLimit := opts.Limit
	totalSent := 0
	var allItems []map[string]interface{}
	page := 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if totalLimit > 0 && totalSent >= totalLimit {
			break
		}

		params := map[string]string{
			"page":     fmt.Sprintf("%d", page),
			"per_page": fmt.Sprintf("%d", apiPageSize),
		}

		if colsJSONStr != "" {
			params["cols"] = colsJSONStr
		}

		response, err := s.fluxxAPIRequest(ctx, endpoint, "GET", params, nil)
		if err != nil {
			config.Debug("[FLUXX] API request failed: %v", err)
			return fmt.Errorf("API request failed: %w", err)
		}

		if response == nil {
			config.Debug("[FLUXX] No response from API")
			break
		}

		// Get the first available key from records
		records, ok := response["records"].(map[string]interface{})
		if !ok || len(records) == 0 {
			break
		}

		// Pick the first key available in records
		var items []interface{}
		for _, value := range records {
			if itemsList, ok := value.([]interface{}); ok {
				items = itemsList
				break
			}
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			if totalLimit > 0 && totalSent+len(allItems) >= totalLimit {
				break
			}

			if itemMap, ok := item.(map[string]interface{}); ok {
				allItems = append(allItems, normalizeFluxxItem(itemMap, fieldsToExtract))
			}

			if len(allItems) >= batchSize {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, columns, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert to Arrow: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				totalSent += len(allItems)
				allItems = nil
			}
		}

		// Check if there are more pages
		perPage, ok := response["per_page"].(float64)
		if !ok || perPage == 0 || len(items) < int(perPage) {
			break
		}

		page++
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, columns, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[FLUXX] Resource %s: %d items", resourceName, totalSent)
	return nil
}

func (s *FluxxSource) fluxxAPIRequest(ctx context.Context, endpoint, method string, params map[string]string, data map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/%s", fluxxAPIV2Path, endpoint)

	req := s.client.R(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", s.accessToken)).
		SetHeader("Content-Type", "application/json")

	if params != nil {
		req.SetQueryParams(params)
	}

	if data != nil {
		req.SetBody(data)
	}

	var resp *gonghttp.Response
	var err error

	// Since method is GET by default, we can use the Get method directly
	resp, err = req.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode(), resp.String())
	}

	if len(resp.Body()) == 0 {
		return make(map[string]interface{}), nil
	}

	var result map[string]interface{}
	if err := resp.JSON(&result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return result, nil
}

func normalizeFluxxItem(item map[string]interface{}, fieldsToExtract map[string]schema.Column) map[string]interface{} {
	if len(fieldsToExtract) == 0 {
		return item
	}

	normalized := make(map[string]interface{})

	for fieldName, col := range fieldsToExtract {
		if value, exists := item[fieldName]; exists {
			if floatVal, ok := value.(float64); ok {
				normalized[fieldName] = roundTo4Decimals(floatVal)
			} else if col.DataType == schema.TypeJSON {
				if value == nil {
					normalized[fieldName] = nil
				} else if strVal, ok := value.(string); ok && strVal == "" {
					normalized[fieldName] = nil
				} else if isSliceOrMap(value) {
					normalized[fieldName] = value
				} else {
					normalized[fieldName] = []interface{}{value}
				}
			} else if col.DataType == schema.TypeDate || col.DataType == schema.TypeTimestamp || col.DataType == schema.TypeTimestampTZ || col.DataType == schema.TypeString {
				if strVal, ok := value.(string); ok && strVal == "" {
					normalized[fieldName] = nil
				} else {
					normalized[fieldName] = value
				}
			} else {
				normalized[fieldName] = value
			}
		} else {
			normalized[fieldName] = nil
		}
	}

	if id, exists := item["id"]; exists {
		normalized["id"] = id
	}

	return normalized
}

func roundTo4Decimals(val float64) float64 {
	multiplier := 10000.0
	return math.Round(val*multiplier) / multiplier
}

func isSliceOrMap(value interface{}) bool {
	switch value.(type) {
	case []interface{}:
		return true
	case []map[string]interface{}:
		return true
	case map[string]interface{}:
		return true
	default:
		return false
	}
}

/*
Get the base URL for Fluxx API.

If instance has a valid TLD (e.g., 'acme.fluxx.io'), use it as full domain.
Otherwise, append '.fluxxlabs.com' for backward compatibility.
Preserves the original scheme (http/https) if provided, defaults to https.

Examples:
  - "mycompany" -> "https://mycompany.fluxxlabs.com"
  - "mycompany.preprod" -> "https://mycompany.preprod.fluxxlabs.com"
  - "acme.fluxx.io" -> "https://acme.fluxx.io"
  - "http://acme.fluxx.io" -> "http://acme.fluxx.io"
*/
func getBaseURL(instance string) (string, error) {
	parsed, err := url.Parse(instance)
	if err != nil {
		parsed = &url.URL{Host: instance}
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "https"
	}
	host := parsed.Host
	if host == "" {
		host = instance
	}
	if strings.Contains(host, ":") {
		host = strings.Split(host, ":")[0]
	}
	suffix, _ := publicsuffix.PublicSuffix(host)
	if suffix != "" && suffix != host {
		return fmt.Sprintf("%s://%s", scheme, host), nil
	}
	return fmt.Sprintf("%s://%s.fluxxlabs.com", scheme, host), nil
}

func (s *FluxxSource) getAccessToken(ctx context.Context) (string, error) {
	if s.accessToken != "" {
		return s.accessToken, nil
	}

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}

	tokenURL := fmt.Sprintf("%s%s", s.baseURL, fluxxOAuthTokenPath)

	resp, err := s.client.R(ctx).
		SetFormData(map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     s.clientID,
			"client_secret": s.clientSecret,
		}).
		SetResult(&tokenData).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("failed to request access token: %w", err)
	}

	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("HTTP error: %d - %s", resp.StatusCode(), resp.String())
	}

	if tokenData.AccessToken == "" {
		return "", fmt.Errorf("access_token not found in response")
	}

	return tokenData.AccessToken, nil
}

var _ source.Source = (*FluxxSource)(nil)
