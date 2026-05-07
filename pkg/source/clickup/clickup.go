package clickup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL        = "https://api.clickup.com/api/v2"
	maxPageSize    = 100
	rateLimit      = 80.0 / 60.0 // ClickUp allows 100 req/min
	rateLimitBurst = 5
)

var supportedTables = []string{
	"user",
	"teams",
	"spaces",
	"lists",
	"tasks",
}

type ClickUpSource struct {
	apiToken string
	client   *ingestrhttp.Client
}

func NewClickUpSource() *ClickUpSource {
	return &ClickUpSource{}
}

func (s *ClickUpSource) HandlesIncrementality() bool {
	return true
}

func (s *ClickUpSource) Schemes() []string {
	return []string{"clickup"}
}

func (s *ClickUpSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseClickUpURI(uri)
	if err != nil {
		return err
	}
	s.apiToken = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("Authorization", s.apiToken),
	)
	config.Debug("[CLICKUP] Connected successfully")
	return nil
}

func (s *ClickUpSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseClickUpURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "clickup://") {
		return "", fmt.Errorf("invalid clickup URI: must start with clickup://")
	}

	rest := strings.TrimPrefix(uri, "clickup://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in clickup URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse clickup URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in clickup URI")
	}

	return apiKey, nil
}

func (s *ClickUpSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyMerge

	switch tableName {
	case "tasks":
		incrementalKey = "date_updated"
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("clickup source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func (s *ClickUpSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "user":
			err = s.readUser(ctx, opts, results)
		case "teams":
			err = s.readTeams(ctx, opts, results)
		case "spaces":
			err = s.readSpaces(ctx, opts, results)
		case "lists":
			err = s.readLists(ctx, opts, results)
		case "tasks":
			err = s.readTasks(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *ClickUpSource) readUser(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ClickUp] Fetching user")

	resp, err := s.client.R(ctx).Get("/user")
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("clickup API /user returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse user response: %w", err)
	}

	user, ok := body["user"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("clickup API /user: missing 'user' field in response")
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{user}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert user to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[ClickUp] Sent 1 user record")
	return nil
}

func (s *ClickUpSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ClickUp] Fetching teams")

	resp, err := s.client.R(ctx).Get("/team")
	if err != nil {
		return fmt.Errorf("failed to fetch teams: %w", err)
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("clickup API /team returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse teams response: %w", err)
	}

	teamsRaw, ok := body["teams"].([]interface{})
	if !ok {
		return fmt.Errorf("clickup API /team: missing 'teams' field in response")
	}

	teams := make([]map[string]interface{}, 0, len(teamsRaw))
	for _, t := range teamsRaw {
		if team, ok := t.(map[string]interface{}); ok {
			teams = append(teams, team)
		}
	}

	if len(teams) == 0 {
		config.Debug("[ClickUp] No teams found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(teams, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert teams to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[ClickUp] Sent %d team records", len(teams))
	return nil
}

func (s *ClickUpSource) readSpaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ClickUp] Fetching spaces")

	teamIDs, err := s.getTeamIDs(ctx)
	if err != nil {
		return err
	}

	var allSpaces []map[string]interface{}
	for _, teamID := range teamIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		endpoint := fmt.Sprintf("/team/%s/space", teamID)
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch spaces for team %s: %w", teamID, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("clickup API %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var body map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return fmt.Errorf("failed to parse spaces response: %w", err)
		}

		spacesRaw, ok := body["spaces"].([]interface{})
		if !ok {
			continue
		}

		for _, s := range spacesRaw {
			if space, ok := s.(map[string]interface{}); ok {
				allSpaces = append(allSpaces, space)
			}
		}
	}

	if len(allSpaces) == 0 {
		config.Debug("[ClickUp] No spaces found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(allSpaces, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert spaces to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[ClickUp] Sent %d space records", len(allSpaces))
	return nil
}

func (s *ClickUpSource) getTeamIDs(ctx context.Context) ([]string, error) {
	resp, err := s.client.R(ctx).Get("/team")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch teams: %w", err)
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("clickup API /team returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("failed to parse teams response: %w", err)
	}

	teamsRaw, ok := body["teams"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("clickup API /team: missing 'teams' field in response")
	}

	var ids []string
	for _, t := range teamsRaw {
		if team, ok := t.(map[string]interface{}); ok {
			if id, ok := team["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}

	return ids, nil
}

func (s *ClickUpSource) readLists(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ClickUp] Fetching lists")

	allLists, err := s.getAllLists(ctx)
	if err != nil {
		return err
	}

	if len(allLists) == 0 {
		config.Debug("[ClickUp] No lists found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(allLists, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert lists to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[ClickUp] Sent %d list records", len(allLists))
	return nil
}

func (s *ClickUpSource) getSpaceIDs(ctx context.Context) ([]string, error) {
	teamIDs, err := s.getTeamIDs(ctx)
	if err != nil {
		return nil, err
	}

	var spaceIDs []string
	for _, teamID := range teamIDs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		endpoint := fmt.Sprintf("/team/%s/space", teamID)
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch spaces for team %s: %w", teamID, err)
		}

		if !resp.IsSuccess() {
			continue
		}

		var body map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			continue
		}

		spacesRaw, ok := body["spaces"].([]interface{})
		if !ok {
			continue
		}

		for _, s := range spacesRaw {
			if space, ok := s.(map[string]interface{}); ok {
				if id, ok := space["id"].(string); ok {
					spaceIDs = append(spaceIDs, id)
				}
			}
		}
	}

	return spaceIDs, nil
}

func (s *ClickUpSource) getAllLists(ctx context.Context) ([]map[string]interface{}, error) {
	spaceIDs, err := s.getSpaceIDs(ctx)
	if err != nil {
		return nil, err
	}

	var allLists []map[string]interface{}
	for _, spaceID := range spaceIDs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// folderless lists
		endpoint := fmt.Sprintf("/space/%s/list", spaceID)
		resp, err := s.client.R(ctx).Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch folderless lists for space %s: %w", spaceID, err)
		}

		if resp.IsSuccess() {
			var body map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &body); err == nil {
				if listsRaw, ok := body["lists"].([]interface{}); ok {
					for _, l := range listsRaw {
						if list, ok := l.(map[string]interface{}); ok {
							allLists = append(allLists, list)
						}
					}
				}
			}
		}

		// lists inside folders
		folderEndpoint := fmt.Sprintf("/space/%s/folder", spaceID)
		resp, err = s.client.R(ctx).Get(folderEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch folders for space %s: %w", spaceID, err)
		}

		if !resp.IsSuccess() {
			continue
		}

		var folderBody map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &folderBody); err != nil {
			continue
		}

		foldersRaw, ok := folderBody["folders"].([]interface{})
		if !ok {
			continue
		}

		for _, f := range foldersRaw {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			folder, ok := f.(map[string]interface{})
			if !ok {
				continue
			}

			folderID, ok := folder["id"].(string)
			if !ok {
				continue
			}

			listEndpoint := fmt.Sprintf("/folder/%s/list", folderID)
			resp, err := s.client.R(ctx).Get(listEndpoint)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch lists for folder %s: %w", folderID, err)
			}

			if !resp.IsSuccess() {
				continue
			}

			var listBody map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &listBody); err != nil {
				continue
			}

			listsRaw, ok := listBody["lists"].([]interface{})
			if !ok {
				continue
			}

			for _, l := range listsRaw {
				if list, ok := l.(map[string]interface{}); ok {
					allLists = append(allLists, list)
				}
			}
		}
	}

	return allLists, nil
}

func toUnixMillis(v interface{}) (string, bool) {
	switch t := v.(type) {
	case time.Time:
		if !t.IsZero() {
			return strconv.FormatInt(t.UnixMilli(), 10), true
		}
	case *time.Time:
		if t != nil && !t.IsZero() {
			return strconv.FormatInt(t.UnixMilli(), 10), true
		}
	}
	return "", false
}

func (s *ClickUpSource) readTasks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ClickUp] Fetching tasks")

	allLists, err := s.getAllLists(ctx)
	if err != nil {
		return err
	}

	var listIDs []string
	for _, list := range allLists {
		if id, ok := list["id"].(string); ok {
			listIDs = append(listIDs, id)
		}
	}

	config.Debug("[ClickUp] Found %d lists to fetch tasks from", len(listIDs))

	totalSent := 0

	for _, listID := range listIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		page := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			req := s.client.R(ctx).
				SetQueryParam("page", strconv.Itoa(page)).
				SetQueryParam("include_closed", "true")

			if opts.IntervalStart != nil {
				if ms, ok := toUnixMillis(opts.IntervalStart); ok {
					req.SetQueryParam("date_updated_gt", ms)
				}
			}
			if opts.IntervalEnd != nil {
				if ms, ok := toUnixMillis(opts.IntervalEnd); ok {
					req.SetQueryParam("date_updated_lt", ms)
				}
			}

			endpoint := fmt.Sprintf("/list/%s/task", listID)
			resp, err := req.Get(endpoint)
			if err != nil {
				return fmt.Errorf("failed to fetch tasks for list %s page %d: %w", listID, page, err)
			}

			if !resp.IsSuccess() {
				config.Debug("[ClickUp] List %s returned status %d, skipping", listID, resp.StatusCode())
				break
			}

			var body map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &body); err != nil {
				return fmt.Errorf("failed to parse tasks response for list %s: %w", listID, err)
			}

			tasksRaw, ok := body["tasks"].([]interface{})
			if !ok || len(tasksRaw) == 0 {
				break
			}

			var tasks []map[string]interface{}
			for _, t := range tasksRaw {
				if task, ok := t.(map[string]interface{}); ok {
					tasks = append(tasks, task)
				}
			}

			if len(tasks) > 0 {
				record, err := arrowconv.ItemsToArrowRecordWithSchema(tasks, nil, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert tasks to Arrow: %w", err)
				}

				results <- source.RecordBatchResult{Batch: record}
				totalSent += len(tasks)
				config.Debug("[ClickUp] List %s page %d: sent %d tasks (total: %d)", listID, page, len(tasks), totalSent)
			}

			if len(tasksRaw) < maxPageSize {
				break
			}
			page++
		}
	}

	if totalSent == 0 {
		config.Debug("[ClickUp] No tasks found")
	}

	return nil
}
