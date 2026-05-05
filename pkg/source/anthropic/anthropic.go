package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL         = "https://api.anthropic.com"
	anthropicAPIVer = "2023-06-01"
)

type AnthropicSource struct {
	apiKey string
	client *gonghttp.Client
}

func NewAnthropicSource() *AnthropicSource {
	return &AnthropicSource{}
}

func (s *AnthropicSource) Schemes() []string {
	return []string{"anthropic"}
}

func (s *AnthropicSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAPIKeyFromURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithHeader("x-api-key", s.apiKey),
		gonghttp.WithHeader("anthropic-version", anthropicAPIVer),
	)

	config.Debug("[ANTHROPIC] Connected successfully")
	return nil
}

func parseAPIKeyFromURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "anthropic://") {
		return "", fmt.Errorf("invalid anthropic URI: must start with anthropic://")
	}

	rest := strings.TrimPrefix(uri, "anthropic://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in URI query parameters")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse anthropic URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key query parameter is required")
	}

	if !strings.HasPrefix(apiKey, "sk-ant-admin") {
		return "", fmt.Errorf("api_key must be an Admin API key (starting with sk-ant-admin...)")
	}

	return apiKey, nil
}

func (s *AnthropicSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AnthropicSource) HandlesIncrementality() bool {
	return true
}

func (s *AnthropicSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	var primaryKeys []string
	switch tableName {
	case "organization", "workspaces", "users", "api_keys", "invites":
		primaryKeys = []string{"id"}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       config.StrategyReplace,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("anthropic source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *AnthropicSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "claude_code_usage":
			err = s.readClaudeCodeUsage(ctx, opts, results)
		case "usage_report":
			err = s.readUsageReport(ctx, opts, results)
		case "cost_report":
			err = s.readCostReport(ctx, opts, results)
		case "organization":
			err = s.readOrganization(ctx, opts, results)
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "api_keys":
			err = s.readAPIKeys(ctx, opts, results)
		case "invites":
			err = s.readInvites(ctx, opts, results)
		case "workspace_members":
			err = s.readWorkspaceMembers(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s (supported: claude_code_usage, usage_report, cost_report, organization, workspaces, users, api_keys, invites, workspace_members)", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *AnthropicSource) fetch(ctx context.Context, endpoint string, params map[string]string) (map[string]interface{}, error) {
	config.Debug("[ANTHROPIC] Fetching: %s%s", baseURL, endpoint)

	var result map[string]interface{}
	resp, err := s.client.R(ctx).
		SetQueryParams(params).
		SetResult(&result).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("API returned status %d for %s: %s", resp.StatusCode(), endpoint, resp.String())
	}

	return result, nil
}

func (s *AnthropicSource) readClaudeCodeUsage(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading claude_code_usage")

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	apiLimit := batchSize
	if apiLimit > 1000 {
		apiLimit = 1000
	}

	startDate := time.Now().UTC().AddDate(0, 0, -7)
	endDate := time.Now().UTC()

	if opts.IntervalStart != nil {
		startDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		endDate = *opts.IntervalEnd
	}

	totalLimit := opts.Limit
	totalSent := 0
	var allItems []map[string]interface{}

	for currentDate := startDate; !currentDate.After(endDate); currentDate = currentDate.AddDate(0, 0, 1) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if totalLimit > 0 && totalSent >= totalLimit {
			break
		}

		dateStr := currentDate.Format("2006-01-02")
		var nextPage string

		for {
			params := map[string]string{
				"starting_at": dateStr,
				"limit":       fmt.Sprintf("%d", apiLimit),
			}
			if nextPage != "" {
				params["page"] = nextPage
			}

			resp, err := s.fetch(ctx, "/v1/organizations/usage_report/claude_code", params)
			if err != nil {
				config.Debug("[ANTHROPIC] Error fetching claude_code_usage for %s: %v", dateStr, err)
				break
			}
			if resp == nil {
				break
			}

			data, ok := resp["data"].([]interface{})
			if !ok || len(data) == 0 {
				break
			}

			for _, item := range data {
				if totalLimit > 0 && totalSent+len(allItems) >= totalLimit {
					break
				}

				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				allItems = append(allItems, flattenClaudeCodeUsageItem(itemMap))

				if len(allItems) >= batchSize {
					record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to convert items to Arrow: %w", err)
					}
					results <- source.RecordBatchResult{Batch: record}
					totalSent += len(allItems)
					allItems = nil
				}
			}

			hasMore, _ := resp["has_more"].(bool)
			if !hasMore {
				break
			}

			nextPage, _ = resp["next_page"].(string)
			if nextPage == "" {
				break
			}
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert items to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[ANTHROPIC] Sent %d claude_code_usage records", totalSent)
	return nil
}

func flattenClaudeCodeUsageItem(item map[string]interface{}) map[string]interface{} {
	flat := make(map[string]interface{})

	flat["date"] = item["date"]
	flat["organization_id"] = item["organization_id"]
	flat["customer_type"] = item["customer_type"]
	flat["terminal_type"] = item["terminal_type"]

	if actor, ok := item["actor"].(map[string]interface{}); ok {
		flat["actor_type"] = actor["type"]
		if email, ok := actor["email_address"].(string); ok {
			flat["actor_id"] = email
		} else if apiKey, ok := actor["api_key_name"].(string); ok {
			flat["actor_id"] = apiKey
		}
	}

	if metrics, ok := item["core_metrics"].(map[string]interface{}); ok {
		flat["num_sessions"] = metrics["num_sessions"]
		flat["commits_by_claude_code"] = metrics["commits_by_claude_code"]
		flat["pull_requests_by_claude_code"] = metrics["pull_requests_by_claude_code"]

		if loc, ok := metrics["lines_of_code"].(map[string]interface{}); ok {
			flat["lines_added"] = loc["added"]
			flat["lines_removed"] = loc["removed"]
		}
	}

	if toolActions, ok := item["tool_actions"].(map[string]interface{}); ok {
		if edit, ok := toolActions["edit_tool"].(map[string]interface{}); ok {
			flat["edit_tool_accepted"] = edit["accepted"]
			flat["edit_tool_rejected"] = edit["rejected"]
		}
		if multiEdit, ok := toolActions["multi_edit_tool"].(map[string]interface{}); ok {
			flat["multi_edit_tool_accepted"] = multiEdit["accepted"]
			flat["multi_edit_tool_rejected"] = multiEdit["rejected"]
		}
		if write, ok := toolActions["write_tool"].(map[string]interface{}); ok {
			flat["write_tool_accepted"] = write["accepted"]
			flat["write_tool_rejected"] = write["rejected"]
		}
		if notebook, ok := toolActions["notebook_edit_tool"].(map[string]interface{}); ok {
			flat["notebook_edit_tool_accepted"] = notebook["accepted"]
			flat["notebook_edit_tool_rejected"] = notebook["rejected"]
		}
	}

	var totalInput, totalOutput, totalCacheRead, totalCacheCreation float64
	var totalCost float64
	var modelsUsed []string

	if breakdown, ok := item["model_breakdown"].([]interface{}); ok {
		for _, m := range breakdown {
			model, ok := m.(map[string]interface{})
			if !ok {
				continue
			}

			if modelName, ok := model["model"].(string); ok {
				modelsUsed = append(modelsUsed, modelName)
			}

			if tokens, ok := model["tokens"].(map[string]interface{}); ok {
				if v, ok := tokens["input"].(float64); ok {
					totalInput += v
				}
				if v, ok := tokens["output"].(float64); ok {
					totalOutput += v
				}
				if v, ok := tokens["cache_read"].(float64); ok {
					totalCacheRead += v
				}
				if v, ok := tokens["cache_creation"].(float64); ok {
					totalCacheCreation += v
				}
			}

			if cost, ok := model["estimated_cost"].(map[string]interface{}); ok {
				if amount, ok := cost["amount"].(float64); ok {
					totalCost += amount
				}
			}
		}
	}

	flat["total_input_tokens"] = totalInput
	flat["total_output_tokens"] = totalOutput
	flat["total_cache_read_tokens"] = totalCacheRead
	flat["total_cache_creation_tokens"] = totalCacheCreation
	flat["total_estimated_cost_cents"] = totalCost
	flat["models_used"] = strings.Join(modelsUsed, ",")

	return flat
}

func (s *AnthropicSource) readUsageReport(ctx context.Context, opts source.ReadOptions, resultsChan chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading usage_report")

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	startDate := time.Now().UTC().AddDate(0, -1, 0)
	endDate := time.Now().UTC()

	if opts.IntervalStart != nil {
		startDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		endDate = *opts.IntervalEnd
	}

	totalSent := 0
	var allItems []map[string]interface{}
	var nextPage string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{
			"starting_at":  {startDate.Format(time.RFC3339)},
			"ending_at":    {endDate.Format(time.RFC3339)},
			"bucket_width": {"1d"},
			"limit":        {"31"},
			"group_by[]":   {"model", "api_key_id", "service_tier", "context_window", "inference_geo"},
		}
		if nextPage != "" {
			params.Set("page", nextPage)
		}

		var result map[string]interface{}
		rawResp, err := s.client.R(ctx).SetQueryParamValues(params).SetResult(&result).Get("/v1/organizations/usage_report/messages")
		if err != nil {
			return fmt.Errorf("failed to fetch usage_report: %w", err)
		}
		if rawResp.StatusCode() == http.StatusNotFound {
			break
		}
		if !rawResp.IsSuccess() {
			return fmt.Errorf("API returned status %d for usage_report: %s", rawResp.StatusCode(), rawResp.String())
		}

		data, ok := result["data"].([]interface{})
		if !ok || len(data) == 0 {
			break
		}

		for _, bucket := range data {
			bucketMap, ok := bucket.(map[string]interface{})
			if !ok {
				continue
			}

			startingAt := bucketMap["starting_at"]
			endingAt := bucketMap["ending_at"]

			bucketResults, ok := bucketMap["results"].([]interface{})
			if !ok {
				continue
			}

			for _, resultItem := range bucketResults {
				resultMap, ok := resultItem.(map[string]interface{})
				if !ok {
					continue
				}

				item := flattenUsageReportItem(resultMap, startingAt, endingAt)
				allItems = append(allItems, item)

				if len(allItems) >= batchSize {
					record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to convert items to Arrow: %w", err)
					}
					resultsChan <- source.RecordBatchResult{Batch: record}
					totalSent += len(allItems)
					allItems = nil
				}
			}
		}

		hasMore, _ := result["has_more"].(bool)
		if !hasMore {
			break
		}

		nextPage, _ = result["next_page"].(string)
		if nextPage == "" {
			break
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert items to Arrow: %w", err)
		}
		resultsChan <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[ANTHROPIC] Sent %d usage_report records", totalSent)
	return nil
}

func flattenUsageReportItem(item map[string]interface{}, startingAt, endingAt interface{}) map[string]interface{} {
	flat := make(map[string]interface{})

	flat["bucket_start"] = startingAt
	flat["bucket_end"] = endingAt
	flat["api_key_id"] = item["api_key_id"]
	flat["workspace_id"] = item["workspace_id"]
	flat["model"] = item["model"]
	flat["service_tier"] = item["service_tier"]
	flat["context_window"] = item["context_window"]
	flat["inference_geo"] = item["inference_geo"]
	flat["uncached_input_tokens"] = item["uncached_input_tokens"]
	flat["output_tokens"] = item["output_tokens"]
	flat["cache_read_input_tokens"] = item["cache_read_input_tokens"]

	if cacheCreation, ok := item["cache_creation"].(map[string]interface{}); ok {
		flat["cache_creation_1h_tokens"] = cacheCreation["ephemeral_1h_input_tokens"]
		flat["cache_creation_5m_tokens"] = cacheCreation["ephemeral_5m_input_tokens"]
	}

	if serverTool, ok := item["server_tool_use"].(map[string]interface{}); ok {
		flat["web_search_requests"] = serverTool["web_search_requests"]
	}

	return flat
}

func (s *AnthropicSource) readCostReport(ctx context.Context, opts source.ReadOptions, resultsChan chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading cost_report")

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	startDate := time.Now().UTC().AddDate(0, -1, 0)
	endDate := time.Now().UTC()

	if opts.IntervalStart != nil {
		startDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		endDate = *opts.IntervalEnd
	}

	totalSent := 0
	var allItems []map[string]interface{}
	var nextPage string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{
			"starting_at":  {startDate.Format(time.RFC3339)},
			"ending_at":    {endDate.Format(time.RFC3339)},
			"bucket_width": {"1d"},
			"group_by[]":   {"description", "workspace_id"},
		}
		if nextPage != "" {
			params.Set("page", nextPage)
		}

		var resp map[string]interface{}
		rawResp, err := s.client.R(ctx).SetQueryParamValues(params).SetResult(&resp).Get("/v1/organizations/cost_report")
		if err != nil {
			return fmt.Errorf("failed to fetch cost_report: %w", err)
		}
		if rawResp.StatusCode() == http.StatusNotFound {
			break
		}
		if !rawResp.IsSuccess() {
			return fmt.Errorf("API returned status %d for cost_report: %s", rawResp.StatusCode(), rawResp.String())
		}

		data, ok := resp["data"].([]interface{})
		if !ok || len(data) == 0 {
			break
		}

		for _, bucket := range data {
			bucketMap, ok := bucket.(map[string]interface{})
			if !ok {
				continue
			}

			startingAt := bucketMap["starting_at"]
			endingAt := bucketMap["ending_at"]

			bucketResults, ok := bucketMap["results"].([]interface{})
			if !ok {
				continue
			}

			for _, result := range bucketResults {
				resultMap, ok := result.(map[string]interface{})
				if !ok {
					continue
				}

				resultMap["bucket_start"] = startingAt
				resultMap["bucket_end"] = endingAt
				allItems = append(allItems, resultMap)

				if len(allItems) >= batchSize {
					record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to convert items to Arrow: %w", err)
					}
					resultsChan <- source.RecordBatchResult{Batch: record}
					totalSent += len(allItems)
					allItems = nil
				}
			}
		}

		hasMore, _ := resp["has_more"].(bool)
		if !hasMore {
			break
		}

		nextPage, _ = resp["next_page"].(string)
		if nextPage == "" {
			break
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert items to Arrow: %w", err)
		}
		resultsChan <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[ANTHROPIC] Sent %d cost_report records", totalSent)
	return nil
}

func (s *AnthropicSource) readOrganization(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading organization")

	resp, err := s.fetch(ctx, "/v1/organizations/me", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch organization: %w", err)
	}
	if resp == nil {
		return nil
	}

	items := []map[string]interface{}{resp}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert organization to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[ANTHROPIC] Sent 1 organization record")
	return nil
}

func (s *AnthropicSource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading workspaces")

	return s.readPaginatedList(ctx, opts, results, "/v1/organizations/workspaces", "workspaces")
}

func (s *AnthropicSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading users")

	return s.readPaginatedList(ctx, opts, results, "/v1/organizations/users", "users")
}

func (s *AnthropicSource) readAPIKeys(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading api_keys")

	return s.readPaginatedList(ctx, opts, results, "/v1/organizations/api_keys", "api_keys", flattenCreatedBy)
}

func (s *AnthropicSource) readInvites(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading invites")

	return s.readPaginatedList(ctx, opts, results, "/v1/organizations/invites", "invites")
}

func (s *AnthropicSource) readWorkspaceMembers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ANTHROPIC] Reading workspace_members")

	workspacesResp, err := s.fetch(ctx, "/v1/organizations/workspaces", map[string]string{"limit": "1000"})
	if err != nil {
		return fmt.Errorf("failed to fetch workspaces for members: %w", err)
	}

	if workspacesResp == nil {
		return nil
	}

	workspaces, ok := workspacesResp["data"].([]interface{})
	if !ok {
		return nil
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	totalSent := 0
	var allItems []map[string]interface{}

	for _, ws := range workspaces {
		wsMap, ok := ws.(map[string]interface{})
		if !ok {
			continue
		}

		workspaceID, ok := wsMap["id"].(string)
		if !ok {
			continue
		}

		var afterID string
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			params := map[string]string{"limit": "1000"}
			if afterID != "" {
				params["after_id"] = afterID
			}

			endpoint := fmt.Sprintf("/v1/organizations/workspaces/%s/members", workspaceID)
			resp, err := s.fetch(ctx, endpoint, params)
			if err != nil {
				config.Debug("[ANTHROPIC] Error fetching members for workspace %s: %v", workspaceID, err)
				break
			}
			if resp == nil {
				break
			}

			data, ok := resp["data"].([]interface{})
			if !ok || len(data) == 0 {
				break
			}

			for _, item := range data {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}

				itemMap["workspace_id"] = workspaceID
				allItems = append(allItems, itemMap)

				if len(allItems) >= batchSize {
					record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
					if err != nil {
						return fmt.Errorf("failed to convert items to Arrow: %w", err)
					}
					results <- source.RecordBatchResult{Batch: record}
					totalSent += len(allItems)
					allItems = nil
				}
			}

			hasMore, _ := resp["has_more"].(bool)
			if !hasMore {
				break
			}

			afterID, _ = resp["last_id"].(string)
			if afterID == "" {
				break
			}
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert items to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[ANTHROPIC] Sent %d workspace_members records", totalSent)
	return nil
}

func (s *AnthropicSource) readPaginatedList(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, endpoint, tableName string, transforms ...func(map[string]interface{})) error {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	totalLimit := opts.Limit
	totalSent := 0
	var allItems []map[string]interface{}
	var afterID string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if totalLimit > 0 && totalSent >= totalLimit {
			break
		}

		params := map[string]string{"limit": "1000"}
		if afterID != "" {
			params["after_id"] = afterID
		}

		resp, err := s.fetch(ctx, endpoint, params)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}
		if resp == nil {
			break
		}

		data, ok := resp["data"].([]interface{})
		if !ok || len(data) == 0 {
			break
		}

		for _, item := range data {
			if totalLimit > 0 && totalSent+len(allItems) >= totalLimit {
				break
			}

			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			for _, tf := range transforms {
				tf(itemMap)
			}

			allItems = append(allItems, itemMap)

			if len(allItems) >= batchSize {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert items to Arrow: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				totalSent += len(allItems)
				allItems = nil
			}
		}

		hasMore, _ := resp["has_more"].(bool)
		if !hasMore {
			break
		}

		afterID, _ = resp["last_id"].(string)
		if afterID == "" {
			break
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert items to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(allItems)
	}

	config.Debug("[ANTHROPIC] Sent %d %s records", totalSent, tableName)
	return nil
}

func flattenCreatedBy(item map[string]any) {
	createdBy, ok := item["created_by"].(map[string]any)
	if !ok {
		return
	}
	for k, v := range createdBy {
		item["created_by_"+k] = v
	}
	delete(item, "created_by")
}

var _ source.Source = (*AnthropicSource)(nil)
