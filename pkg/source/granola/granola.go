package granola

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL         = "https://public-api.granola.ai"
	maxPageSize     = 30
	defaultPageSize = 30
)

var supportedTables = []string{
	"notes",
	"folders",
}

var noteFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "owner", DataType: schema.TypeJSON, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "object", DataType: schema.TypeString, Nullable: true},
	{Name: "web_url", DataType: schema.TypeString, Nullable: true},
	{Name: "calendar_event", DataType: schema.TypeJSON, Nullable: true},
	{Name: "attendees", DataType: schema.TypeJSON, Nullable: true},
	{Name: "folder_membership", DataType: schema.TypeJSON, Nullable: true},
	{Name: "summary_text", DataType: schema.TypeString, Nullable: true},
	{Name: "summary_markdown", DataType: schema.TypeString, Nullable: true},
	{Name: "transcript", DataType: schema.TypeJSON, Nullable: true},
}

var folderFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "parent_folder_id", DataType: schema.TypeString, Nullable: true},
}

type GranolaSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewGranolaSource() *GranolaSource {
	return &GranolaSource{}
}

func (s *GranolaSource) HandlesIncrementality() bool {
	return true
}

func (s *GranolaSource) Schemes() []string {
	return []string{"granola"}
}

func (s *GranolaSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseGranolaURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRetry(5, 2*time.Second, 30*time.Second),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(apiKey)),
	)

	config.Debug("[GRANOLA] Connected successfully")
	return nil
}

func (s *GranolaSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseGranolaURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "granola://") {
		return "", fmt.Errorf("invalid granola URI: must start with granola://")
	}

	rest := strings.TrimPrefix(uri, "granola://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in granola URI (granola://?api_key=...)")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse granola URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in granola URI (granola://?api_key=...)")
	}

	return apiKey, nil
}

func (s *GranolaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", tableName, strings.Join(supportedTables, ", "))
	}

	tableSchema, err := getSchema(tableName)
	if err != nil {
		return nil, err
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	if tableName == "notes" {
		incrementalKey = "updated_at"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    tableSchema.PrimaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func isValidTable(table string) bool {
	for _, supported := range supportedTables {
		if table == supported {
			return true
		}
	}
	return false
}

func getSchema(table string) (*schema.TableSchema, error) {
	switch table {
	case "notes":
		return &schema.TableSchema{
			Name:           table,
			Columns:        noteFields,
			PrimaryKeys:    []string{"id"},
			IncrementalKey: "updated_at",
		}, nil
	case "folders":
		return &schema.TableSchema{
			Name:        table,
			Columns:     folderFields,
			PrimaryKeys: []string{"id"},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}
}

func (s *GranolaSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "notes":
			err = s.readNotes(ctx, opts, results)
		case "folders":
			err = s.readFolders(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *GranolaSource) readNotes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]string{}
	if opts.IntervalStart != nil {
		params["updated_after"] = opts.IntervalStart.UTC().Format(time.RFC3339)
	}

	return s.paginate(ctx, "notes", "notes", params, noteFields, opts, results)
}

func (s *GranolaSource) readFolders(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginate(ctx, "folders", "folders", nil, folderFields, opts, results)
}

type listResponse struct {
	Notes   []map[string]interface{} `json:"notes"`
	Folders []map[string]interface{} `json:"folders"`
	HasMore bool                     `json:"hasMore"`
	Cursor  *string                  `json:"cursor"`
}

func (s *GranolaSource) paginate(
	ctx context.Context,
	endpoint string,
	responseKey string,
	params map[string]string,
	fields []schema.Column,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
) error {
	pageSize := resolvePageSize(opts)
	totalSent := 0
	var cursor string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reqParams := map[string]string{
			"page_size": fmt.Sprintf("%d", pageSize),
		}
		for k, v := range params {
			reqParams[k] = v
		}
		if cursor != "" {
			reqParams["cursor"] = cursor
		}

		config.Debug("[GRANOLA] Fetching %s with params: %+v", endpoint, reqParams)
		resp, err := s.client.R(ctx).SetQueryParams(reqParams).Get("/v1/" + endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("failed to fetch %s: status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var body listResponse
		if err := resp.JSON(&body); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		items, err := responseItems(body, responseKey)
		if err != nil {
			return err
		}
		items = filterItemsByInterval(items, opts.IncrementalKey, opts.IntervalStart, opts.IntervalEnd)
		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}
		if responseKey == "notes" && len(items) > 0 {
			items, err = s.hydrateNotes(ctx, items)
			if err != nil {
				return err
			}
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, fields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			return nil
		}
		if !body.HasMore || body.Cursor == nil || *body.Cursor == "" {
			return nil
		}
		cursor = *body.Cursor
	}
}

func (s *GranolaSource) hydrateNotes(ctx context.Context, notes []map[string]interface{}) ([]map[string]interface{}, error) {
	hydrated := make([]map[string]interface{}, 0, len(notes))
	for _, note := range notes {
		noteID, ok := note["id"].(string)
		if !ok || noteID == "" {
			hydrated = append(hydrated, note)
			continue
		}

		detail, err := s.fetchNoteDetail(ctx, noteID)
		if err != nil {
			return nil, err
		}
		for k, v := range note {
			if _, exists := detail[k]; !exists {
				detail[k] = v
			}
		}
		hydrated = append(hydrated, detail)
	}
	return hydrated, nil
}

func (s *GranolaSource) fetchNoteDetail(ctx context.Context, noteID string) (map[string]interface{}, error) {
	resp, err := s.client.R(ctx).
		SetQueryParam("include", "transcript").
		Get("/v1/notes/" + url.PathEscape(noteID))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch note %s: %w", noteID, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("failed to fetch note %s: status %d: %s", noteID, resp.StatusCode(), resp.String())
	}

	var detail map[string]interface{}
	if err := resp.JSON(&detail); err != nil {
		return nil, fmt.Errorf("failed to parse note %s response: %w", noteID, err)
	}
	return detail, nil
}

func resolvePageSize(opts source.ReadOptions) int {
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}
	return pageSize
}

func responseItems(body listResponse, responseKey string) ([]map[string]interface{}, error) {
	switch responseKey {
	case "notes":
		return body.Notes, nil
	case "folders":
		return body.Folders, nil
	default:
		return nil, fmt.Errorf("unsupported response key: %s", responseKey)
	}
}

func filterItemsByInterval(items []map[string]interface{}, incrementalKey string, start, end *time.Time) []map[string]interface{} {
	if incrementalKey == "" || (start == nil && end == nil) {
		return items
	}

	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		raw, ok := item[incrementalKey]
		if !ok || raw == nil {
			filtered = append(filtered, item)
			continue
		}

		t, ok := parseTimestamp(raw)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && t.Before(*start) {
			continue
		}
		if end != nil && t.After(*end) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseTimestamp(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	default:
		return time.Time{}, false
	}
}
