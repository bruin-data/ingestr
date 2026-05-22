package slack

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
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL     = "https://slack.com/api/"
	maxPageSize = 1000
)

type tableMeta struct {
	primaryKeys []string
	strategy    config.IncrementalStrategy
}

var supportedTables = map[string]tableMeta{
	"channels":    {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"users":       {primaryKeys: []string{"id"}, strategy: config.StrategyReplace},
	"access_logs": {primaryKeys: []string{"user_id"}, strategy: config.StrategyAppend},
}

var messagesMeta = tableMeta{
	primaryKeys: []string{"ts", "channel"},
	strategy:    config.StrategyMerge,
}

type SlackSource struct {
	client *httpclient.Client
	apiKey string
}

func NewSlackSource() *SlackSource {
	return &SlackSource{}
}

func (s *SlackSource) Schemes() []string {
	return []string{"slack"}
}

func (s *SlackSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseSlackURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(0.33, 1),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[SLACK] Connected successfully")
	return nil
}

func parseSlackURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "slack://") {
		return "", fmt.Errorf("invalid slack URI: must start with slack://")
	}

	rest := strings.TrimPrefix(uri, "slack://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in slack URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse slack URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in slack URI")
	}

	return apiKey, nil
}

func (s *SlackSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *SlackSource) HandlesIncrementality() bool {
	return true
}

func (s *SlackSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	meta, ok := supportedTables[tableName]
	if !ok {
		if strings.HasPrefix(tableName, "messages:") {
			meta = messagesMeta
		} else {
			tables := make([]string, 0, len(supportedTables))
			for t := range supportedTables {
				tables = append(tables, t)
			}
			return nil, fmt.Errorf("unsupported table: %s (supported: %v, messages:<channel_id,...>)", tableName, tables)
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: "",
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("slack source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *SlackSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch {
		case table == "channels":
			err = s.readChannels(ctx, opts, results)
		case table == "users":
			err = s.readUsers(ctx, opts, results)
		case table == "access_logs":
			err = s.readAccessLogs(ctx, opts, results)
		case strings.HasPrefix(table, "messages:"):
			channels := strings.Split(strings.TrimPrefix(table, "messages:"), ",")
			err = s.readMessages(ctx, opts, channels, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *SlackSource) getPages(ctx context.Context, resource, responsePath string, params map[string]interface{}, tsFields map[string]bool, extraContext map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	nextCursor := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		queryParams := make(map[string]string)
		for k, v := range params {
			queryParams[k] = fmt.Sprintf("%v", v)
		}
		queryParams["limit"] = fmt.Sprintf("%d", maxPageSize)
		if nextCursor != "" {
			queryParams["cursor"] = nextCursor
		}

		resp, err := s.client.R(ctx).
			SetHeader("Authorization", "Bearer "+s.apiKey).
			SetQueryParams(queryParams).
			Get(resource)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", resource, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("slack API %s returned status %d: %s", resource, resp.StatusCode(), resp.String())
		}

		var response map[string]interface{}
		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", resource, err)
		}

		if ok, _ := response["ok"].(bool); !ok {
			errMsg, _ := response["error"].(string)
			return fmt.Errorf("slack API %s error: %s", resource, errMsg)
		}

		rawItems, _ := response[responsePath].([]interface{})
		if len(rawItems) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(rawItems))
		for _, raw := range rawItems {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			convertTimestamps(item, tsFields)
			for k, v := range extraContext {
				item[k] = v
			}
			items = append(items, item)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", resource, err)
			}
			results <- source.RecordBatchResult{Batch: record}
			config.Debug("[SLACK] Sent %d items from %s", len(items), resource)
		}

		nextCursor = ""
		if meta, ok := response["response_metadata"].(map[string]interface{}); ok {
			nextCursor, _ = meta["next_cursor"].(string)
		}
		if nextCursor == "" {
			break
		}
	}

	return nil
}

func (s *SlackSource) readChannels(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.getPages(ctx, "conversations.list", "channels", nil, map[string]bool{"created": true, "updated": true}, nil, opts, results)
}

func (s *SlackSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.getPages(ctx, "users.list", "members", map[string]interface{}{"include_locale": true}, map[string]bool{"created": true, "updated": true}, nil, opts, results)
}

func (s *SlackSource) readAccessLogs(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	params := map[string]interface{}{}
	if opts.IntervalEnd != nil {
		params["before"] = fmt.Sprintf("%d", opts.IntervalEnd.Unix())
	}

	return s.getPages(ctx, "team.accessLogs", "logins", params, map[string]bool{"date_first": true, "date_last": true}, nil, opts, results)
}

func (s *SlackSource) readMessages(ctx context.Context, opts source.ReadOptions, selectedChannels []string, results chan<- source.RecordBatchResult) error {
	startTS := "0"
	endTS := ""

	if opts.IntervalStart != nil {
		startTS = fmt.Sprintf("%d", opts.IntervalStart.Unix())
	} else {
		startTS = fmt.Sprintf("%d", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	}

	if opts.IntervalEnd != nil {
		endTS = fmt.Sprintf("%d", opts.IntervalEnd.Unix())
	}

	selected := make(map[string]struct{}, len(selectedChannels))
	for _, sel := range selectedChannels {
		selected[sel] = struct{}{}
	}

	type channelInfo struct {
		id   string
		name string
	}

	fetchChannels := make(chan channelInfo)
	fetchErr := make(chan error, 1)

	go func() {
		defer close(fetchChannels)
		nextCursor := ""
		for {
			qp := map[string]string{
				"limit": fmt.Sprintf("%d", maxPageSize),
			}
			if nextCursor != "" {
				qp["cursor"] = nextCursor
			}

			resp, err := s.client.R(ctx).
				SetHeader("Authorization", "Bearer "+s.apiKey).
				SetQueryParams(qp).
				Get("conversations.list")
			if err != nil {
				fetchErr <- fmt.Errorf("failed to fetch channel list: %w", err)
				return
			}

			var body map[string]interface{}
			if err := json.Unmarshal(resp.Body(), &body); err != nil {
				fetchErr <- fmt.Errorf("failed to parse channel list response: %w", err)
				return
			}

			if ok, _ := body["ok"].(bool); !ok {
				errMsg, _ := body["error"].(string)
				fetchErr <- fmt.Errorf("slack API conversations.list error: %s", errMsg)
				return
			}

			rawChannels, _ := body["channels"].([]interface{})
			for _, raw := range rawChannels {
				ch, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := ch["name"].(string)
				id, _ := ch["id"].(string)
				_, byName := selected[name]
				_, byID := selected[id]
				if byName || byID {
					fetchChannels <- channelInfo{id: id, name: name}
				}
			}

			nextCursor = ""
			if meta, ok := body["response_metadata"].(map[string]interface{}); ok {
				nextCursor, _ = meta["next_cursor"].(string)
			}
			if nextCursor == "" {
				return
			}
		}
	}()

	const maxWorkers = 2
	workerChan := make(chan channelInfo, maxWorkers)
	errs := make(chan error, len(selectedChannels))

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ch := range workerChan {
				params := map[string]interface{}{
					"channel": ch.id,
					"oldest":  startTS,
				}
				if endTS != "" {
					params["latest"] = endTS
				}

				err := s.getPages(ctx, "conversations.history", "messages", params,
					map[string]bool{"ts": true, "thread_ts": true, "latest_reply": true},
					map[string]interface{}{"channel": ch.name},
					opts, results)
				if err != nil {
					errs <- fmt.Errorf("failed to read messages for channel %s: %w", ch.name, err)
				}
			}
		}()
	}

	for ch := range fetchChannels {
		workerChan <- ch
	}
	close(workerChan)

	wg.Wait()
	close(errs)

	select {
	case err := <-fetchErr:
		return err
	default:
	}

	for err := range errs {
		return err
	}

	return nil
}

func convertTimestamps(obj map[string]interface{}, fields map[string]bool) {
	for key, val := range obj {
		if fields[key] {
			convertTimestamp(obj, key)
			continue
		}
		switch v := val.(type) {
		case map[string]interface{}:
			convertTimestamps(v, fields)
		case []interface{}:
			for _, elem := range v {
				if m, ok := elem.(map[string]interface{}); ok {
					convertTimestamps(m, fields)
				}
			}
		}
	}
}

func convertTimestamp(m map[string]interface{}, key string) {
	val, exists := m[key]
	if !exists {
		return
	}
	var ts float64
	switch v := val.(type) {
	case float64:
		ts = v
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return
		}
		ts = parsed
	default:
		return
	}
	if ts > 1e10 {
		ms := int64(ts + 0.5)
		m[key] = time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond)).UTC().Format(time.RFC3339Nano)
	} else {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * 1e9)
		m[key] = time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
	}
}

var _ source.Source = (*SlackSource)(nil)
