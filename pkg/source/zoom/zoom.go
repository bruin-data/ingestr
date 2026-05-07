package zoom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	ingestrhttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/schemainfer"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL          = "https://api.zoom.us/v2"
	maxPageSize      = 300
	workerCount      = 5
	rateLimit        = 10 // Zoom Pro tier: Heavy APIs allow 10 req/s
	rateLimitBurst   = 10
	defaultStartDate = "2020-01-26"
)

var supportedTables = []string{
	"users",
	"meetings",
	"participants",
}

type ZoomSource struct {
	clientID     string
	clientSecret string
	accountID    string
	client       *ingestrhttp.Client
}

func NewZoomSource() *ZoomSource {
	return &ZoomSource{}
}

func (s *ZoomSource) HandlesIncrementality() bool {
	return true
}

func (s *ZoomSource) Schemes() []string {
	return []string{"zoom"}
}

func parseZoomURI(uri string) (clientID, clientSecret, accountID string, err error) {
	if !strings.HasPrefix(uri, "zoom://") {
		return "", "", "", fmt.Errorf("invalid zoom URI: must start with zoom://")
	}

	rest := strings.TrimPrefix(uri, "zoom://")
	parts := strings.SplitN(rest, "?", 2)

	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("zoom URI must include query parameters (zoom://?client_id=...&client_secret=...&account_id=...)")
	}

	values, err := url.ParseQuery(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse zoom URI query: %w", err)
	}

	clientID = values.Get("client_id")
	clientSecret = values.Get("client_secret")
	accountID = values.Get("account_id")

	if clientID == "" || clientSecret == "" || accountID == "" {
		return "", "", "", fmt.Errorf("client_id, client_secret, and account_id are required in zoom URI")
	}

	return clientID, clientSecret, accountID, nil
}

func (s *ZoomSource) getAccessToken(ctx context.Context) (string, error) {
	client := ingestrhttp.New(
		ingestrhttp.WithTimeout(30*time.Second),
		ingestrhttp.WithAuth(ingestrhttp.NewBasicAuth(s.clientID, s.clientSecret)),
	)
	defer func() { _ = client.Close() }()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	resp, err := client.R(ctx).
		SetQueryParam("grant_type", "account_credentials").
		SetQueryParam("account_id", s.accountID).
		SetResult(&tokenResp).
		Post("https://zoom.us/oauth/token")
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}

	if !resp.IsSuccess() {
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

func (s *ZoomSource) Connect(ctx context.Context, uri string) error {
	clientID, clientSecret, accountID, err := parseZoomURI(uri)
	if err != nil {
		return err
	}
	s.clientID = clientID
	s.clientSecret = clientSecret
	s.accountID = accountID

	token, err := s.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Zoom access token: %w", err)
	}

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(baseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithAuth(ingestrhttp.NewBearerAuth(token)),
	)
	config.Debug("[ZOOM] Connected successfully")
	return nil
}

func (s *ZoomSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *ZoomSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	strategy := config.StrategyReplace

	switch tableName {
	case "meetings":
		incrementalKey = "start_time"
		strategy = config.StrategyMerge
	case "participants":
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("zoom source does not have a predefined schema; schema inference is required")
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

func (s *ZoomSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "meetings":
			err = s.readMeetings(ctx, opts, results)
		case "participants":
			err = s.readParticipants(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *ZoomSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Zoom] Fetching users")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return s.paginateAndSend(ctx, opts, results, "users", "users", pageSize, nil, nil, false)
}

func (s *ZoomSource) readMeetings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Zoom] Fetching meetings")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	userCh := make(chan source.RecordBatchResult, 8)
	var fetchErr error
	go func() {
		defer close(userCh)
		fetchErr = s.readUsers(ctx, source.ReadOptions{PageSize: maxPageSize}, userCh)
	}()

	var userIDs []string
	for res := range userCh {
		if res.Err != nil {
			return fmt.Errorf("failed to list users for meetings: %w", res.Err)
		}
		idIdx := res.Batch.Schema().FieldIndices("id")
		if len(idIdx) == 0 {
			continue
		}
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
				userIDs = append(userIDs, raw)
				continue
			}
			if id, ok := decoded.(string); ok {
				userIDs = append(userIDs, id)
			}
		}
		res.Batch.Release()
	}
	if fetchErr != nil {
		return fmt.Errorf("failed to list users for meetings: %w", fetchErr)
	}

	config.Debug("[ZOOM] Found %d users for meeting fetch", len(userIDs))

	sem := make(chan struct{}, workerCount)
	errCh := make(chan error, len(userIDs))
	for _, userID := range userIDs {
		sem <- struct{}{}
		go func(uid string) {
			defer func() { <-sem }()
			config.Debug("[ZOOM] Fetching meetings for user %s", uid)
			endpoint := fmt.Sprintf("users/%s/meetings", uid)
			errCh <- s.paginateAndSend(
				ctx, opts, results, endpoint, "meetings", pageSize,
				map[string]string{"type": "scheduled"},
				map[string]interface{}{"zoom_user_id": uid},
				false,
			)
		}(userID)
	}

	for range userIDs {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}

func (s *ZoomSource) readParticipants(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[Zoom] Fetching participants")

	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	userCh := make(chan source.RecordBatchResult, 8)
	var fetchErr error
	go func() {
		defer close(userCh)
		fetchErr = s.readUsers(ctx, source.ReadOptions{PageSize: maxPageSize}, userCh)
	}()

	var userIDs []string
	for res := range userCh {
		if res.Err != nil {
			return fmt.Errorf("failed to list users for participants: %w", res.Err)
		}
		idIdx := res.Batch.Schema().FieldIndices("id")
		if len(idIdx) == 0 {
			continue
		}
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
				userIDs = append(userIDs, raw)
				continue
			}
			if id, ok := decoded.(string); ok {
				userIDs = append(userIDs, id)
			}
		}
		res.Batch.Release()
	}
	if fetchErr != nil {
		return fmt.Errorf("failed to list users for participants: %w", fetchErr)
	}

	config.Debug("[ZOOM] Found %d users for participant fetch", len(userIDs))

	var meetingIDs []string
	sem := make(chan struct{}, workerCount)
	meetingCh := make(chan source.RecordBatchResult, 8)
	var firstErr error
	var errOnce sync.Once
	meetingCtx, meetingCancel := context.WithCancel(ctx)
	defer meetingCancel()

	go func() {
		defer close(meetingCh)
		var wg sync.WaitGroup
		for _, userID := range userIDs {
			if meetingCtx.Err() != nil {
				break
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(uid string) {
				defer func() { <-sem; wg.Done() }()
				config.Debug("[ZOOM] Fetching past meetings for user %s", uid)
				endpoint := fmt.Sprintf("report/users/%s/meetings", uid)
				err := s.paginateAndSend(
					meetingCtx, opts, meetingCh, endpoint, "meetings", pageSize,
					nil,
					map[string]interface{}{"zoom_user_id": uid},
					true,
				)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("failed to fetch past meetings: %w", err)
						meetingCancel()
					})
				}
			}(userID)
		}
		wg.Wait()
	}()

	for res := range meetingCh {
		if res.Err != nil {
			return fmt.Errorf("failed to list past meetings for participants: %w", res.Err)
		}
		idIdx := res.Batch.Schema().FieldIndices("id")
		if len(idIdx) == 0 {
			res.Batch.Release()
			continue
		}
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
				meetingIDs = append(meetingIDs, raw)
				continue
			}
			switch v := decoded.(type) {
			case string:
				meetingIDs = append(meetingIDs, v)
			case json.Number:
				meetingIDs = append(meetingIDs, v.String())
			default:
				meetingIDs = append(meetingIDs, fmt.Sprintf("%v", v))
			}
		}
		res.Batch.Release()
	}

	if firstErr != nil {
		return firstErr
	}

	config.Debug("[ZOOM] Found %d past meetings for participant fetch", len(meetingIDs))

	participantErrCh := make(chan error, len(meetingIDs))
	for _, meetingID := range meetingIDs {
		sem <- struct{}{}
		go func(mid string) {
			defer func() { <-sem }()
			config.Debug("[ZOOM] Fetching participants for meeting %s", mid)
			endpoint := fmt.Sprintf("report/meetings/%s/participants", mid)
			participantErrCh <- s.paginateAndSend(
				ctx, opts, results, endpoint, "participants", pageSize,
				nil,
				map[string]interface{}{"zoom_meeting_id": mid},
				false,
			)
		}(meetingID)
	}

	for range meetingIDs {
		if err := <-participantErrCh; err != nil {
			config.Debug("[ZOOM] Skipping participants for meeting: %v", err)
		}
	}

	return nil
}

func (s *ZoomSource) paginateAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	endpoint string,
	responseKey string,
	pageSize int,
	extraParams map[string]string,
	injectFields map[string]interface{},
	withDates bool,
) error {
	if !withDates {
		return s.paginateEndpoint(ctx, opts, results, endpoint, responseKey, pageSize, extraParams, injectFields, "", "")
	}

	from := toDateString(opts.IntervalStart)
	if from == "" {
		from = defaultStartDate
	}
	to := toDateString(opts.IntervalEnd)
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}

	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil {
		return fmt.Errorf("invalid from date %s: %w", from, err)
	}
	toDate, err := time.Parse("2006-01-02", to)
	if err != nil {
		return fmt.Errorf("invalid to date %s: %w", to, err)
	}

	for chunkStart := fromDate; chunkStart.Before(toDate); {
		chunkEnd := chunkStart.AddDate(0, 1, 0)
		if chunkEnd.After(toDate) {
			chunkEnd = toDate
		}

		config.Debug("[ZOOM] Fetching %s from %s to %s", endpoint, chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"))
		err := s.paginateEndpoint(ctx, opts, results, endpoint, responseKey, pageSize, extraParams, injectFields,
			chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"))
		if err != nil {
			return err
		}

		chunkStart = chunkEnd
	}

	return nil
}

func (s *ZoomSource) paginateEndpoint(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	endpoint string,
	responseKey string,
	pageSize int,
	extraParams map[string]string,
	injectFields map[string]interface{},
	from string,
	to string,
) error {
	totalSent := 0
	batchNum := 0
	nextPageToken := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.client.R(ctx).
			SetQueryParam("page_size", strconv.Itoa(pageSize))

		for k, v := range extraParams {
			req.SetQueryParam(k, v)
		}

		if nextPageToken != "" {
			req.SetQueryParam("next_page_token", nextPageToken)
		}

		if from != "" && to != "" {
			req.SetQueryParam("from", from)
			req.SetQueryParam("to", to)
		}

		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", endpoint, err)
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("failed to fetch %s: status %d: %s", endpoint, resp.StatusCode(), resp.String())
		}

		var raw map[string]json.RawMessage
		if err := resp.JSON(&raw); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", endpoint, err)
		}

		itemsRaw, ok := raw[responseKey]
		if !ok {
			return fmt.Errorf("response missing '%s' field", responseKey)
		}

		var items []map[string]interface{}
		if err := json.Unmarshal(itemsRaw, &items); err != nil {
			return fmt.Errorf("failed to parse %s items: %w", endpoint, err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			for k, v := range injectFields {
				item[k] = v
			}
		}

		if opts.Limit > 0 && totalSent+len(items) > opts.Limit {
			items = items[:opts.Limit-totalSent]
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", endpoint, err)
		}

		batchNum++
		config.Debug("[ZOOM] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), endpoint, totalSent+len(items))
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(items)

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[ZOOM] Reached limit of %d %s", opts.Limit, endpoint)
			break
		}

		var pagination struct {
			NextPageToken string `json:"next_page_token"`
		}
		if tokenRaw, ok := raw["next_page_token"]; ok {
			if err := json.Unmarshal(tokenRaw, &pagination.NextPageToken); err != nil {
				break
			}
		}

		if pagination.NextPageToken == "" {
			break
		}

		nextPageToken = pagination.NextPageToken
	}

	if totalSent == 0 {
		config.Debug("[ZOOM] No %s found", endpoint)
	}

	return nil
}

func toDateString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case time.Time:
		return t.Format("2006-01-02")
	case *time.Time:
		if t != nil {
			return t.Format("2006-01-02")
		}
	case string:
		return t
	}
	return ""
}
