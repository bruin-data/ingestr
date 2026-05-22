package asana

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://app.asana.com/api/1.0"
	// Asana API rate limit: 150 req/min (free tier). Using ~80% → 2.0 req/s.
	rateLimit          = 2.0
	rateLimitBurst     = 5
	maxPageSize        = 100
	defaultParallelism = 5

	workspaceOptFields = "gid,name,is_organization,resource_type,email_domains"
	projectOptFields   = "gid,name,owner,current_status,custom_fields,default_view,due_date,due_on,is_template,created_at,modified_at,start_on,archived,public,members,followers,color,notes,icon,permalink_url,workspace,team,resource_type,current_status_update,custom_field_settings,completed,completed_at,completed_by,created_from_template,project_brief"
	sectionOptFields   = "gid,resource_type,name,created_at,project,projects"
	tagOptFields       = "gid,resource_type,created_at,followers,name,color,notes,permalink_url,workspace"
	taskOptFields      = "gid,resource_type,name,approval_status,assignee_status,created_at,assignee,start_on,start_at,due_on,due_at,completed,completed_at,completed_by,modified_at,dependencies,dependents,external,notes,num_subtasks,resource_subtype,followers,parent,permalink_url,tags,workspace,custom_fields,project,memberships,memberships.project.name,memberships.section.name"
	storyOptFields     = "gid,resource_type,created_at,created_by,resource_subtype,text,is_pinned,assignee,dependency,follower,new_section,old_section,new_text_value,old_text_value,preview,project,source,story,tag,target,task,sticker_name,custom_field,type"
	teamOptFields      = "gid,resource_type,name,description,organization,permalink_url,visibility"
	userOptFields      = "gid,resource_type,name,email,photo,workspaces"
)

var supportedTables = []string{
	"workspaces",
	"projects",
	"sections",
	"tags",
	"tasks",
	"stories",
	"teams",
	"users",
}

type AsanaSource struct {
	client      *httpclient.Client
	workspaceID string
}

func NewAsanaSource() *AsanaSource {
	return &AsanaSource{}
}

func (s *AsanaSource) HandlesIncrementality() bool {
	return false
}

func (s *AsanaSource) Schemes() []string {
	return []string{"asana"}
}

func parseURI(uri string) (workspaceID, token string, err error) {
	if !strings.HasPrefix(uri, "asana://") {
		return "", "", fmt.Errorf("invalid asana URI: must start with asana://")
	}

	rest := strings.TrimPrefix(uri, "asana://")

	var queryStr string
	workspaceID, queryStr, _ = strings.Cut(rest, "?")

	values, parseErr := url.ParseQuery(queryStr)
	if parseErr != nil {
		return "", "", fmt.Errorf("failed to parse asana URI: %w", parseErr)
	}

	token = values.Get("access_token")
	if token == "" {
		return "", "", fmt.Errorf("access_token is required in asana URI: asana://<workspace_id>?access_token=<token>")
	}
	if workspaceID == "" {
		return "", "", fmt.Errorf("workspace_id is required in asana URI: asana://<workspace_id>?access_token=<token>")
	}

	return workspaceID, token, nil
}

func (s *AsanaSource) Connect(ctx context.Context, uri string) error {
	workspaceID, token, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.workspaceID = workspaceID

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewBearerAuth(token)),
	)

	config.Debug("[ASANA] Connected successfully")
	return nil
}

func (s *AsanaSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AsanaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	var primaryKeys []string

	if tableName == "tasks" {
		primaryKeys = []string{"gid"}
		incrementalKey = "modified_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("asana source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	return slices.Contains(supportedTables, table)
}

func (s *AsanaSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "workspaces":
			err = s.readWorkspaces(ctx, opts, results)
		case "projects":
			err = s.readProjects(ctx, opts, results)
		case "sections":
			err = s.readSections(ctx, opts, results)
		case "tags":
			err = s.readTags(ctx, opts, results)
		case "tasks":
			err = s.readTasks(ctx, opts, results)
		case "stories":
			err = s.readStories(ctx, opts, results)
		case "teams":
			err = s.readTeams(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *AsanaSource) readWorkspaces(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading workspaces")
	return s.paginateAndSend(ctx, "/workspaces", opts, results, map[string]string{
		"opt_fields": workspaceOptFields,
	})
}

func (s *AsanaSource) readProjects(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading projects")
	return s.paginateAndSend(ctx, fmt.Sprintf("/workspaces/%s/projects", s.workspaceID), opts, results, map[string]string{
		"opt_fields": projectOptFields,
	})
}

func (s *AsanaSource) readSections(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading sections")
	projectGIDs, err := s.fetchProjectGIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch projects for sections: %w", err)
	}
	return s.runParallel(ctx, projectGIDs, func(projGID string) error {
		return s.paginateAndSend(ctx, fmt.Sprintf("/projects/%s/sections", projGID), opts, results, map[string]string{
			"opt_fields": sectionOptFields,
		})
	})
}

func (s *AsanaSource) readTags(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading tags")
	return s.paginateAndSend(ctx, fmt.Sprintf("/workspaces/%s/tags", s.workspaceID), opts, results, map[string]string{
		"opt_fields": tagOptFields,
	})
}

func (s *AsanaSource) readTasks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading tasks")
	projectGIDs, err := s.fetchProjectGIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch projects for tasks: %w", err)
	}
	return s.runParallel(ctx, projectGIDs, func(projGID string) error {
		params := map[string]string{
			"project":    projGID,
			"opt_fields": taskOptFields,
		}
		if opts.IntervalStart != nil {
			params["modified_since"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		}
		return s.paginateAndSend(ctx, "/tasks", opts, results, params)
	})
}

func (s *AsanaSource) readStories(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading stories")
	taskGIDs, err := s.fetchTaskGIDs(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to fetch tasks for stories: %w", err)
	}
	return s.runParallel(ctx, taskGIDs, func(taskGID string) error {
		return s.paginateAndSend(ctx, fmt.Sprintf("/tasks/%s/stories", taskGID), opts, results, map[string]string{
			"opt_fields": storyOptFields,
		})
	})
}

func (s *AsanaSource) runParallel(ctx context.Context, ids []string, fn func(string) error) error {
	sem := make(chan struct{}, defaultParallelism)
	errc := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if err := fn(id); err != nil {
				select {
				case errc <- err:
					cancel()
				default:
				}
			}
		})
	}
	wg.Wait()

	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}

func (s *AsanaSource) readTeams(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading teams")
	return s.paginateAndSend(ctx, fmt.Sprintf("/workspaces/%s/teams", s.workspaceID), opts, results, map[string]string{
		"opt_fields": teamOptFields,
	})
}

func (s *AsanaSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[ASANA] reading users")
	return s.paginateAndSend(ctx, fmt.Sprintf("/workspaces/%s/users", s.workspaceID), opts, results, map[string]string{
		"opt_fields": userOptFields,
	})
}

func (s *AsanaSource) paginateAndSend(
	ctx context.Context,
	endpoint string,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	extraParams map[string]string,
) error {
	totalSent := 0
	offset := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("limit", fmt.Sprintf("%d", maxPageSize))

		for k, v := range extraParams {
			req.SetQueryParam(k, v)
		}

		if offset != "" {
			req.SetQueryParam("offset", offset)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("asana %s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var result struct {
			Data     []map[string]any `json:"data"`
			NextPage *struct {
				Offset string `json:"offset"`
			} `json:"next_page"`
		}
		if err := resp.JSON(&result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items := result.Data
		if len(items) == 0 {
			break
		}

		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
		}

		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)

		config.Debug("[ASANA] fetched %d records from %s (total: %d)", len(items), endpoint, totalSent)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			break
		}

		if result.NextPage == nil || result.NextPage.Offset == "" {
			break
		}

		offset = result.NextPage.Offset
	}

	if totalSent == 0 {
		config.Debug("[ASANA] no records found for %s", endpoint)
	}

	return nil
}

func extractGIDs(ctx context.Context, ch <-chan source.RecordBatchResult) ([]string, error) {
	var gids []string
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res, ok := <-ch:
			if !ok {
				return gids, nil
			}
			if res.Err != nil {
				return nil, res.Err
			}
			idIdx := res.Batch.Schema().FieldIndices("gid")
			if len(idIdx) > 0 {
				col := res.Batch.Column(idIdx[0])
				if ext, ok := col.(array.ExtensionArray); ok {
					col = ext.Storage()
				}
				for i := 0; i < col.Len(); i++ {
					raw, ok := schemainfer.StringValueAt(col, i)
					if !ok {
						continue
					}
					decoded, err := schemainfer.DecodeUnknownValue(raw)
					if err != nil {
						gids = append(gids, raw)
						continue
					}
					if gid, ok := decoded.(string); ok {
						gids = append(gids, gid)
					}
				}
			}
			res.Batch.Release()
		}
	}
}

func (s *AsanaSource) fetchWorkspaceGIDs(_ context.Context) ([]string, error) {
	return []string{s.workspaceID}, nil
}

func (s *AsanaSource) fetchProjectGIDs(ctx context.Context) ([]string, error) {
	workspaceGIDs, err := s.fetchWorkspaceGIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workspaces: %w", err)
	}

	var projectGIDs []string
	for _, wsGID := range workspaceGIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ch := make(chan source.RecordBatchResult, 8)
		go func(ws string) {
			defer close(ch)
			if err := s.paginateAndSend(ctx, fmt.Sprintf("/workspaces/%s/projects", ws), source.ReadOptions{}, ch, map[string]string{"opt_fields": "gid"}); err != nil {
				ch <- source.RecordBatchResult{Err: err}
			}
		}(wsGID)

		gids, err := extractGIDs(ctx, ch)
		if err != nil {
			return nil, err
		}
		projectGIDs = append(projectGIDs, gids...)
	}

	return projectGIDs, nil
}

func (s *AsanaSource) fetchTaskGIDs(ctx context.Context, opts source.ReadOptions) ([]string, error) {
	projectGIDs, err := s.fetchProjectGIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch projects: %w", err)
	}

	var taskGIDs []string
	for _, projGID := range projectGIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		params := map[string]string{
			"project":    projGID,
			"opt_fields": "gid",
		}
		if opts.IntervalStart != nil {
			params["modified_since"] = opts.IntervalStart.UTC().Format(time.RFC3339)
		}

		ch := make(chan source.RecordBatchResult, 8)
		go func(p map[string]string) {
			defer close(ch)
			if err := s.paginateAndSend(ctx, "/tasks", opts, ch, p); err != nil {
				ch <- source.RecordBatchResult{Err: err}
			}
		}(params)

		gids, err := extractGIDs(ctx, ch)
		if err != nil {
			return nil, err
		}
		taskGIDs = append(taskGIDs, gids...)
	}

	return taskGIDs, nil
}
