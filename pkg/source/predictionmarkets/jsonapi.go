package predictionmarkets

import (
	"context"
	"encoding/json"
	"fmt"
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
)

const (
	defaultPageLimit = 100
)

type PaginationKind string

const (
	PaginationNone   PaginationKind = ""
	PaginationCursor PaginationKind = "cursor"
	PaginationOffset PaginationKind = "offset"
	PaginationKeyset PaginationKind = "keyset"
	PaginationBefore PaginationKind = "before"
	PaginationPage   PaginationKind = "page"
	PaginationTime   PaginationKind = "time"
)

type TableSpec struct {
	Name               string
	BaseURL            string
	Path               string
	ResultPath         []string
	QueryParams        []string
	RequiredParams     []string
	Columns            []schema.Column
	PrimaryKeys        []string
	IncrementalKey     string
	Strategy           config.IncrementalStrategy
	Pagination         PaginationKind
	LimitParam         string
	LimitDefault       int
	LimitMax           int
	CursorParam        string
	CursorPath         []string
	OffsetParam        string
	BeforeParam        string
	BeforeField        string
	PageParam          string
	TimeParam          string
	TimeField          string
	IntervalStartParam string
	IntervalEndParam   string
	IntervalRFC3339    bool
	IntervalUnixMillis bool
	RequireInterval    bool
	MaxPages           int
}

type JSONAPISource struct {
	Scheme  string
	Tables  map[string]TableSpec
	Params  url.Values
	Client  *ingestrhttp.Client
	Clients map[string]*ingestrhttp.Client
}

func ParseURI(rawURI, scheme string) (url.Values, error) {
	if !strings.HasPrefix(rawURI, scheme+"://") {
		return nil, fmt.Errorf("invalid %s URI: must start with %s://", scheme, scheme)
	}

	rest := strings.TrimPrefix(rawURI, scheme+"://")
	if rest == "" || rest == "?" {
		return url.Values{}, nil
	}

	if strings.HasPrefix(rest, "?") {
		rest = strings.TrimPrefix(rest, "?")
	} else if idx := strings.Index(rest, "?"); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		return url.Values{}, nil
	}

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s URI query: %w", scheme, err)
	}
	return values, nil
}

func NewClient(baseURL string, rateLimit float64, burst int) *ingestrhttp.Client {
	opts := []ingestrhttp.Option{
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60 * time.Second),
		ingestrhttp.WithDebug(config.DebugMode),
	}
	if rateLimit > 0 && burst > 0 {
		opts = append(opts, ingestrhttp.WithRateLimiter(rateLimit, burst))
	}
	return ingestrhttp.New(opts...)
}

func (s *JSONAPISource) Close(ctx context.Context) error {
	for _, client := range s.Clients {
		if client != nil {
			_ = client.Close()
		}
	}
	if s.Client != nil {
		return s.Client.Close()
	}
	return nil
}

func (s *JSONAPISource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	spec, ok := s.Tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(SortedTableNames(s.Tables), ", "))
	}

	strategy := spec.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	return &source.DynamicSourceTable{
		TableName:           spec.Name,
		TablePrimaryKeys:    spec.PrimaryKeys,
		TableIncrementalKey: spec.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         true,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return &schema.TableSchema{
				Name:           spec.Name,
				Columns:        spec.Columns,
				PrimaryKeys:    spec.PrimaryKeys,
				IncrementalKey: spec.IncrementalKey,
			}, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.ReadSpec(ctx, spec, opts)
		},
	}, nil
}

func (s *JSONAPISource) ReadSpec(ctx context.Context, spec TableSpec, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		rows, err := s.FetchRows(ctx, spec, opts)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
			return
		}
		if len(rows) == 0 {
			return
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(rows, spec.Columns, opts.ExcludeColumns)
		if err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert %s to Arrow: %w", spec.Name, err)}
			return
		}
		results <- source.RecordBatchResult{Batch: record}
	}()

	return results, nil
}

func (s *JSONAPISource) FetchRows(ctx context.Context, spec TableSpec, opts source.ReadOptions) ([]map[string]interface{}, error) {
	if err := s.validateRequired(spec); err != nil {
		return nil, err
	}
	if spec.RequireInterval && (opts.IntervalStart == nil || opts.IntervalEnd == nil) {
		return nil, fmt.Errorf("table %s requires --interval-start and --interval-end", spec.Name)
	}

	maxRows := opts.Limit

	var rows []map[string]interface{}
	var cursor string
	offset := 0
	page := 1
	before := ""
	timeCursor := ""

	for pageNo := 0; ; pageNo++ {
		if spec.MaxPages > 0 && pageNo >= spec.MaxPages {
			return nil, fmt.Errorf("table %s reached max page limit of %d before pagination completed", spec.Name, spec.MaxPages)
		}

		query := s.buildQuery(spec, opts)
		switch spec.Pagination {
		case PaginationCursor, PaginationKeyset:
			if cursor != "" {
				query.Set(spec.CursorParam, cursor)
			}
		case PaginationOffset:
			if spec.OffsetParam != "" {
				query.Set(spec.OffsetParam, strconv.Itoa(offset))
			}
		case PaginationPage:
			if spec.PageParam != "" {
				query.Set(spec.PageParam, strconv.Itoa(page))
			}
		case PaginationBefore:
			if before != "" {
				query.Set(spec.BeforeParam, before)
			}
		case PaginationTime:
			if timeCursor != "" {
				query.Set(spec.TimeParam, timeCursor)
			}
		}

		endpoint, err := s.path(spec)
		if err != nil {
			return nil, err
		}

		var payload interface{}
		client, err := s.clientFor(spec)
		if err != nil {
			return nil, err
		}
		resp, err := client.R(ctx).SetQueryParamValues(query).Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s: %w", spec.Name, err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("%s request failed with status %d: %s", spec.Name, resp.StatusCode(), resp.String())
		}
		if err := json.Unmarshal(resp.Body(), &payload); err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", spec.Name, err)
		}

		pageRows, err := rowsFromPayload(payload, spec.ResultPath)
		if err != nil {
			return nil, fmt.Errorf("failed to extract %s rows: %w", spec.Name, err)
		}
		if len(pageRows) == 0 {
			break
		}

		for _, raw := range pageRows {
			rows = append(rows, projectRow(raw, spec.Columns))
			if maxRows > 0 && len(rows) >= maxRows {
				return rows[:maxRows], nil
			}
		}

		switch spec.Pagination {
		case PaginationCursor, PaginationKeyset:
			next := stringAt(payload, spec.CursorPath)
			if next == "" || next == cursor {
				return rows, nil
			}
			cursor = next
		case PaginationOffset:
			offset += len(pageRows)
			if len(pageRows) < limitFor(spec, opts) {
				return rows, nil
			}
		case PaginationPage:
			page++
			if len(pageRows) < limitFor(spec, opts) {
				return rows, nil
			}
		case PaginationBefore:
			next := valueAsString(pageRows[len(pageRows)-1][spec.BeforeField])
			if next == "" || next == before {
				return rows, nil
			}
			before = next
		case PaginationTime:
			next := valueAsString(pageRows[len(pageRows)-1][spec.TimeField])
			if next == "" || next == timeCursor {
				return rows, nil
			}
			timeCursor = next
		default:
			return rows, nil
		}
	}

	return rows, nil
}

func (s *JSONAPISource) clientFor(spec TableSpec) (*ingestrhttp.Client, error) {
	if spec.BaseURL != "" && s.Clients != nil {
		if client := s.Clients[spec.BaseURL]; client != nil {
			return client, nil
		}
	}
	if s.Client != nil {
		return s.Client, nil
	}
	return nil, fmt.Errorf("no HTTP client configured for table %s", spec.Name)
}

func (s *JSONAPISource) validateRequired(spec TableSpec) error {
	for _, name := range spec.RequiredParams {
		if s.Params.Get(name) == "" {
			return fmt.Errorf("table %s requires URI parameter %q", spec.Name, name)
		}
	}
	return nil
}

func (s *JSONAPISource) buildQuery(spec TableSpec, opts source.ReadOptions) url.Values {
	query := url.Values{}
	pathParams := pathParamNames(spec)
	for _, name := range spec.QueryParams {
		if pathParams[name] {
			continue
		}
		if values, ok := s.Params[name]; ok {
			for _, value := range values {
				if value != "" {
					query.Add(name, value)
				}
			}
		}
	}
	if spec.LimitParam != "" {
		query.Set(spec.LimitParam, strconv.Itoa(limitFor(spec, opts)))
	}
	if opts.IntervalStart != nil && spec.IntervalStartParam != "" {
		query.Set(spec.IntervalStartParam, formatInterval(*opts.IntervalStart, spec.IntervalRFC3339, spec.IntervalUnixMillis))
	}
	if opts.IntervalEnd != nil && spec.IntervalEndParam != "" {
		query.Set(spec.IntervalEndParam, formatInterval(*opts.IntervalEnd, spec.IntervalRFC3339, spec.IntervalUnixMillis))
	}
	return query
}

func pathParamNames(spec TableSpec) map[string]bool {
	params := make(map[string]bool, len(spec.RequiredParams))
	for _, name := range spec.RequiredParams {
		if strings.Contains(spec.Path, "{"+name+"}") {
			params[name] = true
		}
	}
	return params
}

func formatInterval(t time.Time, rfc3339, millis bool) string {
	if rfc3339 {
		return t.UTC().Format(time.RFC3339)
	}
	if millis {
		return strconv.FormatInt(t.UnixMilli(), 10)
	}
	return strconv.FormatInt(t.Unix(), 10)
}

func (s *JSONAPISource) path(spec TableSpec) (string, error) {
	path := spec.Path
	for _, name := range spec.RequiredParams {
		placeholder := "{" + name + "}"
		if strings.Contains(path, placeholder) {
			value := s.Params.Get(name)
			if value == "" {
				return "", fmt.Errorf("table %s requires URI parameter %q", spec.Name, name)
			}
			path = strings.ReplaceAll(path, placeholder, url.PathEscape(value))
		}
	}
	return path, nil
}

func limitFor(spec TableSpec, opts source.ReadOptions) int {
	limit := spec.LimitDefault
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if opts.PageSize > 0 && (spec.LimitMax <= 0 || opts.PageSize <= spec.LimitMax) {
		limit = opts.PageSize
	}
	if opts.Limit > 0 && opts.Limit < limit {
		limit = opts.Limit
	}
	if spec.LimitMax > 0 && limit > spec.LimitMax {
		limit = spec.LimitMax
	}
	return limit
}

func rowsFromPayload(payload interface{}, path []string) ([]map[string]interface{}, error) {
	current := payload
	for _, key := range path {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("path %s is not an object", strings.Join(path, "."))
		}
		current = obj[key]
	}

	switch v := current.(type) {
	case []interface{}:
		rows := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			row, ok := item.(map[string]interface{})
			if !ok {
				rows = append(rows, map[string]interface{}{"value": item})
				continue
			}
			rows = append(rows, row)
		}
		return rows, nil
	case map[string]interface{}:
		return []map[string]interface{}{v}, nil
	case nil:
		return nil, nil
	default:
		return []map[string]interface{}{{"value": v}}, nil
	}
}

func projectRow(raw map[string]interface{}, columns []schema.Column) map[string]interface{} {
	row := make(map[string]interface{}, len(columns))
	for _, col := range columns {
		if col.Name == "raw" {
			row[col.Name] = mustJSON(raw)
			continue
		}
		if value, ok := raw[col.Name]; ok {
			row[col.Name] = value
		}
	}
	return row
}

func mustJSON(value interface{}) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(b)
}

func stringAt(payload interface{}, path []string) string {
	current := payload
	for _, key := range path {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = obj[key]
	}
	return valueAsString(current)
}

func valueAsString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func SortedTableNames(tables map[string]TableSpec) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(values []string) {
	sort.Strings(values)
}
