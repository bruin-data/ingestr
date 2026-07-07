package trello

import (
	"bytes"
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
	baseURL            = "https://api.trello.com/1"
	maxPageSize        = 1000
	defaultParallelism = 5
	// Trello allows 100 requests per 10s per token. Every request shares one
	// token, so that is the binding limit; 80% of 10 req/s = 8 req/s.
	rateLimit      = 8.0
	rateLimitBurst = 5
)

var supportedTables = []string{
	"boards",
	"organizations",
	"lists",
	"members",
	"labels",
	"checklists",
	"cards",
	"actions",
}

type TrelloSource struct {
	apiKey string
	token  string
	client *httpclient.Client
}

func NewTrelloSource() *TrelloSource {
	return &TrelloSource{}
}

func (s *TrelloSource) Schemes() []string {
	return []string{"trello"}
}

func (s *TrelloSource) HandlesIncrementality() bool {
	return true
}

func (s *TrelloSource) Connect(ctx context.Context, uri string) error {
	apiKey, token, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey
	s.token = token

	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Accept", "application/json"),
	)
	// Trello authenticates every request with the key+token pair as query params.
	s.client.Resty().SetQueryParams(map[string]string{"key": apiKey, "token": token})

	config.Debug("[TRELLO] Connected successfully")
	return nil
}

func (s *TrelloSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func parseURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "trello://") {
		return "", "", fmt.Errorf("invalid trello URI: must start with trello://")
	}

	rest := strings.TrimPrefix(uri, "trello://")
	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse trello URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in trello URI")
	}
	token := values.Get("token")
	if token == "" {
		return "", "", fmt.Errorf("token is required in trello URI")
	}

	return apiKey, token, nil
}

func (s *TrelloSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	name, _ := parseTableName(tableName)
	if !isValidTable(name) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace
	switch name {
	case "actions":
		incrementalKey = "date"
		strategy = config.StrategyMerge
	case "cards":
		incrementalKey = "dateLastActivity"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("trello source does not have a predefined schema; schema inference is required")
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

// parseTableName splits a source table into its name and an optional board
// filter, e.g. "cards:abc,def" -> ("cards", "abc,def").
func parseTableName(table string) (string, string) {
	parts := strings.SplitN(table, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

// parseBoardIDs turns a comma-separated board filter into a slice; nil means
// "all boards the member can access".
func parseBoardIDs(param string) []string {
	if param == "" {
		return nil
	}
	var ids []string
	for _, p := range strings.Split(param, ",") {
		if v := strings.TrimSpace(p); v != "" {
			ids = append(ids, v)
		}
	}
	return ids
}

func (s *TrelloSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)
	name, param := parseTableName(table)
	boardIDs := parseBoardIDs(param)

	go func() {
		defer close(results)

		var err error
		switch name {
		case "boards":
			if boardIDs != nil {
				err = fmt.Errorf("boards does not accept a board filter")
			} else {
				err = s.readBoards(ctx, opts, results)
			}
		case "organizations":
			if boardIDs != nil {
				err = fmt.Errorf("organizations does not accept a board filter")
			} else {
				err = s.readOrganizations(ctx, opts, results)
			}
		case "lists":
			err = s.readPerBoard(ctx, boardIDs, "lists", "/boards/%s/lists", nil, opts, results)
		case "checklists":
			err = s.readPerBoard(ctx, boardIDs, "checklists", "/boards/%s/checklists", nil, opts, results)
		case "labels":
			err = s.readPerBoard(ctx, boardIDs, "labels", "/boards/%s/labels", map[string]string{"limit": strconv.Itoa(maxPageSize)}, opts, results)
		case "members":
			err = s.readMembers(ctx, boardIDs, opts, results)
		case "cards":
			err = s.readCards(ctx, boardIDs, opts, results)
		case "actions":
			err = s.readActions(ctx, boardIDs, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", name)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *TrelloSource) getArray(ctx context.Context, endpoint string, params map[string]string) ([]map[string]interface{}, error) {
	req := s.client.R(ctx)
	if params != nil {
		req.SetQueryParams(params)
	}
	resp, err := req.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("%s returned status %d: %s", endpoint, resp.StatusCode(), resp.String())
	}

	decoder := json.NewDecoder(bytes.NewReader(resp.Body()))
	decoder.UseNumber()
	var arr []map[string]interface{}
	if err := decoder.Decode(&arr); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", endpoint, err)
	}
	return arr, nil
}

func (s *TrelloSource) sendRecord(ctx context.Context, items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert to Arrow: %w", err)
	}
	select {
	case results <- source.RecordBatchResult{Batch: record}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *TrelloSource) getBoardIDs(ctx context.Context) ([]string, error) {
	boards, err := s.getArray(ctx, "/members/me/boards", map[string]string{"fields": "id"})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch boards: %w", err)
	}
	ids := make([]string, 0, len(boards))
	for _, b := range boards {
		if id, ok := b["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// resolveBoardIDs returns the explicit board filter when provided, otherwise
// every board the authenticated member can access.
func (s *TrelloSource) resolveBoardIDs(ctx context.Context, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	return s.getBoardIDs(ctx)
}

// fanOutBoards runs fn for each board (the explicit filter, or all accessible
// boards), using a bounded worker pool since boards are independent.
func (s *TrelloSource) fanOutBoards(ctx context.Context, explicit []string, opts source.ReadOptions, results chan<- source.RecordBatchResult, fn func(ctx context.Context, boardID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error) error {
	boardIDs, err := s.resolveBoardIDs(ctx, explicit)
	if err != nil {
		return err
	}
	if len(boardIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	boardCh := make(chan string)
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	for i := 0; i < defaultParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for boardID := range boardCh {
				if err := fn(ctx, boardID, opts, results); err != nil {
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	for _, id := range boardIDs {
		select {
		case boardCh <- id:
		case <-ctx.Done():
		}
	}
	close(boardCh)
	wg.Wait()
	close(errs)

	if err := <-errs; err != nil {
		return err
	}
	return nil
}

func (s *TrelloSource) readBoards(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading boards")
	boards, err := s.getArray(ctx, "/members/me/boards", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch boards: %w", err)
	}
	if len(boards) == 0 {
		return nil
	}
	return s.sendRecord(ctx, boards, opts, results)
}

func (s *TrelloSource) readOrganizations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading organizations")
	orgs, err := s.getArray(ctx, "/members/me/organizations", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch organizations: %w", err)
	}
	if len(orgs) == 0 {
		return nil
	}
	return s.sendRecord(ctx, orgs, opts, results)
}

// readPerBoard fetches a single-page board sub-resource (no pagination or
// filtering) across the selected boards.
func (s *TrelloSource) readPerBoard(ctx context.Context, boardIDs []string, label, endpointTmpl string, params map[string]string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading %s", label)
	return s.fanOutBoards(ctx, boardIDs, opts, results, func(ctx context.Context, boardID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
		endpoint := fmt.Sprintf(endpointTmpl, boardID)
		items, err := s.getArray(ctx, endpoint, params)
		if err != nil {
			return fmt.Errorf("failed to fetch %s for board %s: %w", label, boardID, err)
		}
		if len(items) == 0 {
			return nil
		}
		return s.sendRecord(ctx, items, opts, results)
	})
}

// readMembers fetches members across every board and de-duplicates by id,
// since the same member appears on multiple boards.
func (s *TrelloSource) readMembers(ctx context.Context, explicit []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading members")
	boardIDs, err := s.resolveBoardIDs(ctx, explicit)
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	var members []map[string]interface{}
	for _, boardID := range boardIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, err := s.getArray(ctx, fmt.Sprintf("/boards/%s/members", boardID), nil)
		if err != nil {
			return fmt.Errorf("failed to fetch members for board %s: %w", boardID, err)
		}
		for _, m := range items {
			if id, ok := m["id"].(string); ok && !seen[id] {
				seen[id] = true
				members = append(members, m)
			}
		}
	}

	if len(members) == 0 {
		return nil
	}
	return s.sendRecord(ctx, members, opts, results)
}

func (s *TrelloSource) readCards(ctx context.Context, boardIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading cards")
	return s.fanOutBoards(ctx, boardIDs, opts, results, s.readBoardCards)
}

// readBoardCards paginates a board's cards by the before-cursor and applies a
// client-side filter on dateLastActivity, since the cards endpoint only offers
// server-side filtering on creation date.
func (s *TrelloSource) readBoardCards(ctx context.Context, boardID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/boards/%s/cards", boardID)
	before := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := map[string]string{"limit": strconv.Itoa(maxPageSize)}
		if before != "" {
			params["before"] = before
		}
		cards, err := s.getArray(ctx, endpoint, params)
		if err != nil {
			return fmt.Errorf("failed to fetch cards for board %s: %w", boardID, err)
		}
		if len(cards) == 0 {
			break
		}

		filtered := filterItemsByInterval(cards, "dateLastActivity", opts.IntervalStart, opts.IntervalEnd)
		if len(filtered) > 0 {
			if err := s.sendRecord(ctx, filtered, opts, results); err != nil {
				return err
			}
		}

		if len(cards) < maxPageSize {
			break
		}
		id, _ := cards[len(cards)-1]["id"].(string)
		if id == "" {
			break
		}
		before = id
	}
	return nil
}

func (s *TrelloSource) readActions(ctx context.Context, boardIDs []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[TRELLO] reading actions")
	return s.fanOutBoards(ctx, boardIDs, opts, results, s.readBoardActions)
}

// readBoardActions paginates a board's full action history by the before-cursor
// and filters client-side on each action's effective-updated timestamp. Comment
// actions are mutable, and the endpoint's since/before only filter on creation
// date, so a client-side filter on max(date, data.dateLastEdited) is used to
// also catch edits to older comments.
func (s *TrelloSource) readBoardActions(ctx context.Context, boardID string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	endpoint := fmt.Sprintf("/boards/%s/actions", boardID)
	before := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := map[string]string{"limit": strconv.Itoa(maxPageSize)}
		if before != "" {
			params["before"] = before
		}
		actions, err := s.getArray(ctx, endpoint, params)
		if err != nil {
			return fmt.Errorf("failed to fetch actions for board %s: %w", boardID, err)
		}
		if len(actions) == 0 {
			break
		}

		filtered := filterActionsByInterval(actions, opts.IntervalStart, opts.IntervalEnd)
		if len(filtered) > 0 {
			if err := s.sendRecord(ctx, filtered, opts, results); err != nil {
				return err
			}
		}

		if len(actions) < maxPageSize {
			break
		}
		id, _ := actions[len(actions)-1]["id"].(string)
		if id == "" {
			break
		}
		before = id
	}
	return nil
}

func filterItemsByInterval(items []map[string]interface{}, field string, start, end *time.Time) []map[string]interface{} {
	if field == "" || (start == nil && end == nil) {
		return items
	}
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := parseTimestamp(item[field])
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// actionUpdatedTime returns an action's effective-updated timestamp: the
// comment edit time (data.dateLastEdited) when present, otherwise the creation
// date. This lets incremental runs catch edits to older comments.
func actionUpdatedTime(item map[string]interface{}) (time.Time, bool) {
	if data, ok := item["data"].(map[string]interface{}); ok {
		if ts, ok := parseTimestamp(data["dateLastEdited"]); ok {
			return ts, true
		}
	}
	return parseTimestamp(item["date"])
}

// filterActionsByInterval keeps actions whose effective-updated timestamp falls
// within [start, end) (end exclusive). Actions with no parseable timestamp are
// retained.
func filterActionsByInterval(items []map[string]interface{}, start, end *time.Time) []map[string]interface{} {
	if start == nil && end == nil {
		return items
	}
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		ts, ok := actionUpdatedTime(item)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if start != nil && ts.Before(start.UTC()) {
			continue
		}
		if end != nil && !ts.Before(end.UTC()) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func parseTimestamp(raw interface{}) (time.Time, bool) {
	s, ok := raw.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
