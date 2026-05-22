package docebo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	doceboOAuthTokenPath = "/oauth2/token"
	doceboRetryAttempts  = 12
	doceboRetryBackoff   = 10 * time.Second
	doceboRetryMaxWait   = 120 * time.Second
	defaultParallelism   = 5
	defaultPageSize      = 200
	defaultRateLimit     = 3.0
	defaultBurst         = 2
)

func doceboRetryCondition(resp *httpclient.Response, err error) bool {
	if resp == nil {
		return false
	}
	code := resp.StatusCode()
	return code == http.StatusTooManyRequests || code == http.StatusInternalServerError ||
		code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

type DoceboSource struct {
	client       *httpclient.Client
	baseURL      string
	clientID     string
	clientSecret string
	username     string
	password     string
	accessToken  string
}

func NewDoceboSource() *DoceboSource {
	return &DoceboSource{}
}

func (s *DoceboSource) Schemes() []string {
	return []string{"docebo"}
}

func (s *DoceboSource) Connect(ctx context.Context, uri string) error {
	baseURL, clientID, clientSecret, username, password, err := parseDoceboURI(uri)
	if err != nil {
		return err
	}

	s.baseURL = strings.TrimSuffix(baseURL, "/")
	s.clientID = clientID
	s.clientSecret = clientSecret
	s.username = username
	s.password = password

	s.client = httpclient.New(
		httpclient.WithBaseURL(s.baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRetry(doceboRetryAttempts, doceboRetryBackoff, doceboRetryMaxWait),
		httpclient.WithRetryCondition(doceboRetryCondition),
		httpclient.WithRateLimiter(defaultRateLimit, defaultBurst),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[DOCEBO] Connected to: %s (base URL: %s)", uri, s.baseURL)
	return nil
}

func parseDoceboURI(uri string) (baseURL, clientID, clientSecret, username, password string, err error) {
	if !strings.HasPrefix(uri, "docebo://") {
		return "", "", "", "", "", fmt.Errorf("invalid docebo URI: must start with docebo://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("failed to parse docebo URI: %w", err)
	}

	sourceParams := parsed.Query()

	baseURL = sourceParams.Get("base_url")
	if baseURL == "" {
		return "", "", "", "", "", fmt.Errorf("base_url is required to connect to Docebo")
	}

	clientID = sourceParams.Get("client_id")
	if clientID == "" {
		return "", "", "", "", "", fmt.Errorf("client_id is required to connect to Docebo")
	}

	clientSecret = sourceParams.Get("client_secret")
	if clientSecret == "" {
		return "", "", "", "", "", fmt.Errorf("client_secret is required to connect to Docebo")
	}

	username = sourceParams.Get("username")
	password = sourceParams.Get("password")

	return baseURL, clientID, clientSecret, username, password, nil
}

func (s *DoceboSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *DoceboSource) HandlesIncrementality() bool {
	return true
}

func (s *DoceboSource) getAccessToken(ctx context.Context) (string, error) {
	if s.accessToken != "" {
		return s.accessToken, nil
	}

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}

	tokenURL := fmt.Sprintf("%s%s", s.baseURL, doceboOAuthTokenPath)

	formData := map[string]string{
		"client_id":     s.clientID,
		"client_secret": s.clientSecret,
		"grant_type":    "client_credentials",
		"scope":         "api",
	}
	if s.username != "" && s.password != "" {
		formData["username"] = s.username
		formData["password"] = s.password
		formData["grant_type"] = "password"
	}

	resp, err := s.client.R(ctx).
		SetFormData(formData).
		SetResult(&tokenData).
		Post(tokenURL)
	if err != nil {
		return "", fmt.Errorf("failed to request access token: %w", err)
	}

	if resp.StatusCode() >= 400 {
		return "", fmt.Errorf("HTTP error: %d - %s", resp.StatusCode(), resp.String())
	}

	if tokenData.AccessToken == "" {
		return "", fmt.Errorf("failed to obtain access token from Docebo: access_token missing in response")
	}

	s.accessToken = tokenData.AccessToken
	return s.accessToken, nil
}

func (s *DoceboSource) fetchSurveyAnswers(ctx context.Context, pollID, courseID string) (map[string]interface{}, error) {
	token, err := s.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/learn/v1/survey/%s/answer", s.baseURL, pollID)
	resp, err := s.client.R(ctx).
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", token)).
		SetHeader("Content-Type", "application/json").
		SetQueryParam("id_course", courseID).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("get %s: HTTP %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Get "data" field like Python does: response.json().get("data", {})
	data, _ := response["data"].(map[string]interface{})
	if data == nil {
		return make(map[string]interface{}), nil
	}

	return normalizeDoceboDates(data), nil
}

func (s *DoceboSource) getPaginatedData(ctx context.Context, endpoint string, pageSize int, params map[string]string, fn func(batch []map[string]interface{}) error) error {
	token, err := s.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	if pageSize <= 0 {
		pageSize = 200
	}

	page := 1
	baseURL := fmt.Sprintf("%s/%s", s.baseURL, endpoint)
	for {
		queryParams := map[string]string{
			"page":      fmt.Sprintf("%d", page),
			"page_size": fmt.Sprintf("%d", pageSize),
		}
		for k, v := range params {
			queryParams[k] = v
		}
		resp, err := s.client.R(ctx).
			SetHeader("Authorization", fmt.Sprintf("Bearer %s", token)).
			SetHeader("Content-Type", "application/json").
			SetQueryParams(queryParams).
			Get(baseURL)
		if err != nil {
			return fmt.Errorf("get %s: %w", baseURL, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("get %s: HTTP %d: %s", baseURL, resp.StatusCode(), resp.String())
		}
		var data interface{}
		if err := json.Unmarshal(resp.Body(), &data); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}

		// Handle paginated response structure
		hasMore := false
		switch v := data.(type) {
		case map[string]interface{}:
			dataVal := v["data"]
			// Most Docebo endpoints return data in this structure
			if dataObj, ok := dataVal.(map[string]interface{}); ok {
				if items, ok := dataObj["items"].([]interface{}); ok {
					batch := make([]map[string]interface{}, 0, len(items))
					for _, it := range items {
						if m, ok := it.(map[string]interface{}); ok {
							batch = append(batch, normalizeDoceboDates(m)) // Normalize dates for each item before calling the callback
						}
					}
					if len(batch) > 0 {
						if err := fn(batch); err != nil {
							return err
						}
					}
					// Check for more pages
					hasMore, _ = dataObj["has_more_data"].(bool)
					if totalPages, ok := dataObj["total_page_count"].(float64); ok && hasMore && float64(page) >= totalPages {
						hasMore = false
					}
				} else {
					hasMore = false
				}
				// Some endpoints might return data directly as a list
			} else if list, ok := dataVal.([]interface{}); ok {
				batch := make([]map[string]interface{}, 0, len(list))
				for _, it := range list {
					if m, ok := it.(map[string]interface{}); ok {
						batch = append(batch, normalizeDoceboDates(m)) // Normalize dates for each item before calling the callback
					}
				}
				if len(batch) > 0 {
					if err := fn(batch); err != nil {
						return err
					}
				}
				// For direct list responses, check if we got a full page
				hasMore = len(list) == pageSize
			} else if _, hasQuestions := v["questions"]; hasQuestions {
				// Survey answer endpoint returns questions and answers at root level (not paginated)
				if err := fn([]map[string]interface{}{v}); err != nil {
					return err
				}
				hasMore = false
			}
		// Some endpoints might return items directly
		case []interface{}:
			batch := make([]map[string]interface{}, 0, len(v))
			for _, it := range v {
				if m, ok := it.(map[string]interface{}); ok {
					batch = append(batch, normalizeDoceboDates(m))
				}
			}
			if len(batch) > 0 {
				if err := fn(batch); err != nil {
					return err
				}
			}
			hasMore = len(v) == pageSize
		}
		if !hasMore {
			break
		}
		page++
	}
	return nil
}

type parallelFetchConfig struct {
	TaskIDs      []string
	PageSize     int
	Endpoint     func(taskID string) string
	Params       func(taskID string) map[string]string
	ProcessBatch func(batch []map[string]interface{}, taskID string)
	TableName    string
}

type parallelFetchResult struct {
	batch []map[string]interface{}
	err   error
}

func (s *DoceboSource) runParallelFetch(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, cfg parallelFetchConfig) error {
	if len(cfg.TaskIDs) == 0 {
		return nil
	}
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	parallelism := defaultParallelism
	taskChan := make(chan string, len(cfg.TaskIDs))
	resultChan := make(chan parallelFetchResult, parallelism*2)

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for taskID := range taskChan {
				select {
				case <-workerCtx.Done():
					return
				default:
				}
				endpoint := cfg.Endpoint(taskID)
				params := cfg.Params(taskID)
				err := s.getPaginatedData(workerCtx, endpoint, pageSize, params, func(batch []map[string]interface{}) error {
					cfg.ProcessBatch(batch, taskID)
					select {
					case resultChan <- parallelFetchResult{batch: batch}:
					case <-workerCtx.Done():
						return workerCtx.Err()
					}
					return nil
				})
				if err != nil {
					config.Debug("[DOCEBO] error fetching %s for %s: %v", cfg.TableName, taskID, err)
					select {
					case resultChan <- parallelFetchResult{err: err}:
					case <-workerCtx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(taskChan)
		for _, id := range cfg.TaskIDs {
			select {
			case taskChan <- id:
			case <-workerCtx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var cols []schema.Column
	if res, ok := doceboResources[cfg.TableName]; ok {
		cols = res.Columns
	}
	for result := range resultChan {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if result.err != nil {
			continue
		}
		if len(result.batch) == 0 {
			continue
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(result.batch, cols, opts.ExcludeColumns)
		if err != nil {
			return err
		}
		results <- source.RecordBatchResult{Batch: rec}
	}
	return nil
}

func (s *DoceboSource) readPaginatedTable(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, endpoint string, params map[string]string, defaultPageSize int, tableName string) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	var cols []schema.Column
	if res, ok := doceboResources[tableName]; ok {
		cols = res.Columns
	}
	return s.getPaginatedData(ctx, endpoint, pageSize, params, func(batch []map[string]interface{}) error {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(batch, cols, opts.ExcludeColumns)
		if err != nil {
			return err
		}
		results <- source.RecordBatchResult{Batch: rec}
		return nil
	})
}

// Date fields that might contain '0000-00-00'
// Add more fields as needed for different resources
var doceboDateFields = map[string]bool{
	"last_access_date":  true,
	"last_update":       true,
	"creation_date":     true,
	"date_begin":        true, // Course field
	"date_end":          true, // Course field
	"date_publish":      true, // Course field
	"date_unpublish":    true, // Course field
	"updated_at":        true, // Course field
	"enrollment_date":   true, // Enrollment field
	"completion_date":   true, // Enrollment field
	"date_assigned":     true, // Assignment field
	"date_completed":    true, // Completion field
	"survey_date":       true, // Survey field
	"start_date":        true, // Course/Plan field
	"end_date":          true, // Course/Plan field
	"date_created":      true, // Generic creation date
	"created_on":        true, // Learning plan field
	"updated_on":        true, // Learning plan field
	"date_modified":     true, // Generic modification date
	"expire_date":       true, // Expiration field
	"date_last_updated": true, // Update date
	"date":              true, // Generic date field (used in survey answers)
	"active_from":       true, // Course enrollment
	"active_until":      true, // Course enrollment
	"date_complete":     true, // Course enrollment
}

func normalizeDoceboDates(item map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(item))
	for k, v := range item {
		if doceboDateFields[k] {
			out[k] = normalizeDateField(v)
		} else {
			out[k] = v
		}
	}
	return out
}

var doceboDateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
}

func normalizeDateField(date_value interface{}) interface{} {
	if date_value == nil {
		return nil
	}
	// Handle datetime objects - pass through
	if t, ok := date_value.(time.Time); ok {
		return t
	}
	// Handle string dates
	if s, ok := date_value.(string); ok {
		// Handle '0000-00-00' or '0000-00-00 00:00:00'
		if strings.HasPrefix(s, "0000-00-00") {
			return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		// Handle other invalid date formats
		if s == "" || s == "0" || s == "null" || s == "NULL" {
			return nil
		}
		// Try to parse valid date strings
		for _, layout := range doceboDateLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	// Return the original value for other types
	return date_value
}

func (s *DoceboSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.IncrementalKey != "" {
		return nil, fmt.Errorf("incremental loads are not yet supported for Docebo")
	}

	tableName := req.Name
	if tableName == "" {
		return nil, fmt.Errorf("table name is required for docebo source")
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("docebo source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *DoceboSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "courses":
			err = s.readCourses(ctx, opts, results)
		case "user_fields":
			err = s.readUserFields(ctx, opts, results)
		case "branches":
			err = s.readBranches(ctx, opts, results)
		case "groups":
			err = s.readGroups(ctx, opts, results)
		case "group_members":
			err = s.readGroupMembers(ctx, opts, results)
		case "course_fields":
			err = s.readCourseFields(ctx, opts, results)
		case "learning_objects":
			err = s.readLearningObjects(ctx, opts, results)
		case "learning_plans":
			err = s.readLearningPlans(ctx, opts, results)
		case "learning_plan_enrollments":
			err = s.readLearningPlanEnrollments(ctx, opts, results)
		case "learning_plan_course_enrollments":
			err = s.readLearningPlanCourseEnrollments(ctx, opts, results)
		case "course_enrollments":
			err = s.readCourseEnrollments(ctx, opts, results)
		case "sessions":
			err = s.readSessions(ctx, opts, results)
		case "categories":
			err = s.readCategories(ctx, opts, results)
		case "certifications":
			err = s.readCertifications(ctx, opts, results)
		case "external_training":
			err = s.readExternalTraining(ctx, opts, results)
		case "survey_answers":
			err = s.readSurveyAnswers(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s (supported: users, courses, user_fields, branches, groups, group_members, course_fields, learning_objects, learning_plans, learning_plan_enrollments, learning_plan_course_enrollments, course_enrollments, sessions, categories, certifications, external_training, survey_answers)", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *DoceboSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "manage/v1/user", nil, 200, "users")
}

func (s *DoceboSource) readCourses(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learn/v1/courses", nil, 200, "courses")
}

// Phase 1: Core User and Organization Resources
func (s *DoceboSource) readUserFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "manage/v1/user_fields", nil, 200, "user_fields")
}

func (s *DoceboSource) readBranches(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "manage/v1/orgchart", nil, 200, "branches")
}

// Phase 2: Group Management
func (s *DoceboSource) readGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "audiences/v1/audience", nil, 200, "groups")
}

func (s *DoceboSource) readGroupMembers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	var allGroups []map[string]interface{}
	err := s.getPaginatedData(ctx, "audiences/v1/audience", pageSize, nil, func(batch []map[string]interface{}) error {
		allGroups = append(allGroups, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var groupIDs []string
	for _, group := range allGroups {
		groupID := ""
		if v := group["group_id"]; v != nil {
			groupID = fmt.Sprint(v)
		} else if v := group["audience_id"]; v != nil {
			groupID = fmt.Sprint(v)
		} else if v := group["id"]; v != nil {
			groupID = fmt.Sprint(v)
		}
		if groupID != "" {
			groupIDs = append(groupIDs, groupID)
		}
	}
	return s.runParallelFetch(ctx, opts, results, parallelFetchConfig{
		TaskIDs:  groupIDs,
		PageSize: pageSize,
		Endpoint: func(id string) string { return fmt.Sprintf("manage/v1/group/%s/members", id) },
		Params:   func(string) map[string]string { return nil },
		ProcessBatch: func(batch []map[string]interface{}, id string) {
			for _, m := range batch {
				m["group_id"] = id
			}
		},
		TableName: "group_members",
	})
}

// Phase 3: Advanced Course Resources
func (s *DoceboSource) readCourseFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learn/v1/courses/field", nil, 200, "course_fields")
}

func (s *DoceboSource) readLearningObjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	var allCourses []map[string]interface{}
	err := s.getPaginatedData(ctx, "learn/v1/courses", pageSize, nil, func(batch []map[string]interface{}) error {
		allCourses = append(allCourses, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var courseIDs []string
	for _, course := range allCourses {
		courseID := ""
		if v := course["id_course"]; v != nil {
			courseID = fmt.Sprint(v)
		} else if v := course["course_id"]; v != nil {
			courseID = fmt.Sprint(v)
		}
		if courseID != "" {
			courseIDs = append(courseIDs, courseID)
		}
	}
	return s.runParallelFetch(ctx, opts, results, parallelFetchConfig{
		TaskIDs:  courseIDs,
		PageSize: pageSize,
		Endpoint: func(id string) string { return fmt.Sprintf("learn/v1/courses/%s/los", id) },
		Params:   func(string) map[string]string { return nil },
		ProcessBatch: func(batch []map[string]interface{}, id string) {
			for _, m := range batch {
				if _, ok := m["course_id"]; !ok {
					m["course_id"] = id
				}
			}
		},
		TableName: "learning_objects",
	})
}

// Phase 4: Learning Plans
func (s *DoceboSource) readLearningPlans(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learningplan/v1/learningplans", nil, 200, "learning_plans")
}

func (s *DoceboSource) readLearningPlanEnrollments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{"extra_fields[]": "enrollment_status"}
	return s.readPaginatedTable(ctx, opts, results, "learningplan/v1/learningplans/enrollments", params, 200, "learning_plan_enrollments")
}

func (s *DoceboSource) readLearningPlanCourseEnrollments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	var allPlans []map[string]interface{}
	err := s.getPaginatedData(ctx, "learningplan/v1/learningplans", pageSize, nil, func(batch []map[string]interface{}) error {
		allPlans = append(allPlans, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var planIDs []string
	for _, plan := range allPlans {
		planID := ""
		if v := plan["id_path"]; v != nil {
			planID = fmt.Sprint(v)
		} else if v := plan["learning_plan_id"]; v != nil {
			planID = fmt.Sprint(v)
		} else if v := plan["id"]; v != nil {
			planID = fmt.Sprint(v)
		}
		if planID != "" {
			planIDs = append(planIDs, planID)
		}
	}
	params := map[string]string{"enrollment_level[]": "student"}
	return s.runParallelFetch(ctx, opts, results, parallelFetchConfig{
		TaskIDs:  planIDs,
		PageSize: pageSize,
		Endpoint: func(id string) string { return fmt.Sprintf("learningplan/v1/learningplans/%s/courses/enrollments", id) },
		Params:   func(string) map[string]string { return params },
		ProcessBatch: func(batch []map[string]interface{}, id string) {
			for _, m := range batch {
				m["learning_plan_id"] = id
			}
		},
		TableName: "learning_plan_course_enrollments",
	})
}

// Phase 5: Enrollments
func (s *DoceboSource) readCourseEnrollments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	var allCourses []map[string]interface{}
	err := s.getPaginatedData(ctx, "learn/v1/courses", pageSize, nil, func(batch []map[string]interface{}) error {
		allCourses = append(allCourses, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var courseIDs []string
	for _, course := range allCourses {
		courseID := ""
		if v := course["id_course"]; v != nil {
			courseID = fmt.Sprint(v)
		} else if v := course["course_id"]; v != nil {
			courseID = fmt.Sprint(v)
		}
		if courseID != "" {
			courseIDs = append(courseIDs, courseID)
		}
	}
	params := map[string]string{"extra_fields[]": "enrollment_status"}
	return s.runParallelFetch(ctx, opts, results, parallelFetchConfig{
		TaskIDs:  courseIDs,
		PageSize: pageSize,
		Endpoint: func(id string) string { return fmt.Sprintf("course/v1/courses/%s/enrollments", id) },
		Params:   func(string) map[string]string { return params },
		ProcessBatch: func(batch []map[string]interface{}, id string) {
			for _, m := range batch {
				m["course_id"] = id
			}
		},
		TableName: "course_enrollments",
	})
}

func (s *DoceboSource) readSessions(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	var allCourses []map[string]interface{}
	err := s.getPaginatedData(ctx, "learn/v1/courses", pageSize, nil, func(batch []map[string]interface{}) error {
		allCourses = append(allCourses, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var courseIDs []string
	for _, course := range allCourses {
		courseID := ""
		if v := course["id_course"]; v != nil {
			courseID = fmt.Sprint(v)
		} else if v := course["course_id"]; v != nil {
			courseID = fmt.Sprint(v)
		}
		if courseID != "" {
			courseIDs = append(courseIDs, courseID)
		}
	}
	return s.runParallelFetch(ctx, opts, results, parallelFetchConfig{
		TaskIDs:  courseIDs,
		PageSize: pageSize,
		Endpoint: func(id string) string { return fmt.Sprintf("learn/v1/courses/%s/sessions", id) },
		Params:   func(string) map[string]string { return nil },
		ProcessBatch: func(batch []map[string]interface{}, id string) {
			for _, m := range batch {
				m["course_id"] = id
			}
		},
		TableName: "sessions",
	})
}

func (s *DoceboSource) readCategories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learn/v1/categories", nil, 200, "categories")
}

func (s *DoceboSource) readCertifications(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learn/v1/certification", nil, 200, "certifications")
}

func (s *DoceboSource) readExternalTraining(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.readPaginatedTable(ctx, opts, results, "learn/v1/external_training", nil, 200, "external_training")
}

func (s *DoceboSource) readSurveyAnswers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	pageSize := defaultPageSize
	if pageSize <= 0 {
		pageSize = 200
	}

	var allCourses []map[string]interface{}
	err := s.getPaginatedData(ctx, "learn/v1/courses", pageSize, nil, func(batch []map[string]interface{}) error {
		allCourses = append(allCourses, batch...)
		return nil
	})
	if err != nil {
		return err
	}

	var courseIDs []string
	for _, c := range allCourses {
		courseID := ""
		if v := c["id_course"]; v != nil {
			courseID = fmt.Sprint(v)
		} else if v := c["course_id"]; v != nil {
			courseID = fmt.Sprint(v)
		}
		if courseID != "" {
			courseIDs = append(courseIDs, courseID)
		}
	}

	parallelism := defaultParallelism
	type pollInfo struct {
		courseID string
		pollID   string
		title    string
	}

	courseChan := make(chan string, parallelism*2)
	pollChan := make(chan pollInfo, parallelism*4)
	type surveyResult struct {
		rows []map[string]interface{}
		err  error
	}
	surveyResultChan := make(chan surveyResult, parallelism*2)

	// Stage 1 workers: fetch polls for each course
	var loWg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		loWg.Add(1)
		go func() {
			defer loWg.Done()
			for cid := range courseChan {
				select {
				case <-ctx.Done():
					return
				default:
				}
				var los []map[string]interface{}
				err := s.getPaginatedData(ctx, fmt.Sprintf("learn/v1/courses/%s/los", cid), pageSize, nil, func(batch []map[string]interface{}) error {
					los = append(los, batch...)
					return nil
				})
				if err != nil {
					continue
				}
				for _, lo := range los {
					loType, _ := lo["lo_type"].(string)
					objectType, _ := lo["object_type"].(string)
					if loType == "poll" || objectType == "poll" {
						if id := lo["id_resource"]; id != nil {
							title, _ := lo["title"].(string)
							if title == "" {
								title, _ = lo["lo_name"].(string)
							}
							select {
							case pollChan <- pollInfo{
								courseID: cid,
								pollID:   fmt.Sprint(id),
								title:    title,
							}:
							case <-ctx.Done():
								return
							}
						}
					}
				}
			}
		}()
	}

	// Stage 2 workers: fetch survey answers
	var surveyWg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		surveyWg.Add(1)
		go func() {
			defer surveyWg.Done()
			for p := range pollChan {
				select {
				case <-ctx.Done():
					return
				default:
				}
				courseID, loID, pollTitle := p.courseID, p.pollID, p.title
				if loID == "" || courseID == "" {
					continue
				}
				surveyData, err := s.fetchSurveyAnswers(ctx, loID, courseID)
				if err != nil {
					continue
				}

				questions, _ := surveyData["questions"].(map[string]interface{})
				answers, _ := surveyData["answers"].([]interface{})

				if questions == nil || answers == nil {
					continue
				}

				var rows []map[string]interface{}
				for _, answerEntry := range answers {
					entry, ok := answerEntry.(map[string]interface{})
					if !ok {
						continue
					}

					answerData, ok := entry["answers"].(map[string]interface{})
					if !ok {
						continue
					}

					dateStr := ""
					if dateVal := entry["date"]; dateVal != nil {
						dateStr = fmt.Sprint(dateVal)
					}

					for questionID, answerList := range answerData {
						questionInfo, _ := questions[questionID].(map[string]interface{})
						questionType := ""
						var questionTitle interface{}
						if questionInfo != nil {
							questionType, _ = questionInfo["type_quest"].(string)
							questionTitle = questionInfo["title_quest"]
						}

						var answers []interface{}
						switch v := answerList.(type) {
						case []interface{}:
							answers = v
						case map[string]interface{}:
							for key := range v {
								answers = append(answers, key)
							}
						default:
							answers = []interface{}{v}
						}

						for _, answer := range answers {
							rows = append(rows, map[string]interface{}{
								"course_id":      courseID,
								"poll_id":        loID,
								"poll_title":     pollTitle,
								"question_id":    questionID,
								"question_type":  questionType,
								"question_title": questionTitle,
								"answer":         answer,
								"date":           dateStr,
							})
						}
					}
				}

				if len(rows) > 0 {
					select {
					case surveyResultChan <- surveyResult{rows: rows}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Feed courses to Stage 1
	go func() {
		defer close(courseChan)
		for _, cid := range courseIDs {
			select {
			case courseChan <- cid:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		loWg.Wait()
		close(pollChan)
	}()

	go func() {
		surveyWg.Wait()
		close(surveyResultChan)
	}()

	var cols []schema.Column
	if res, ok := doceboResources["survey_answers"]; ok {
		cols = res.Columns
	}

	for result := range surveyResultChan {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if result.err != nil || len(result.rows) == 0 {
			continue
		}
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(result.rows, cols, opts.ExcludeColumns)
		if err != nil {
			return err
		}
		results <- source.RecordBatchResult{Batch: rec}
	}
	return nil
}
