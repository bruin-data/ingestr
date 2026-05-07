package fireflies

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
	ingestrhttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	graphQLBaseURL   = "https://api.fireflies.ai/graphql"
	maxPageSize      = 50 // Fireflies API limit is 50
	rateLimit        = 5
	rateLimitBurst   = 3
	maxAnalyticsDays = 30
	parallelWorkers  = 4 // Number of parallel API requests
)

var supportedTables = []string{
	"transcripts",
	"users",
	"user_groups",
	"channels",
	"bites",
	"contacts",
	"active_meetings",
	"analytics",
}

var transcriptFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "date", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "duration", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "transcript_url", DataType: schema.TypeString, Nullable: true},
	{Name: "audio_url", DataType: schema.TypeString, Nullable: true},
	{Name: "video_url", DataType: schema.TypeString, Nullable: true},
	{Name: "meeting_link", DataType: schema.TypeString, Nullable: true},
	{Name: "host_email", DataType: schema.TypeString, Nullable: true},
	{Name: "organizer_email", DataType: schema.TypeString, Nullable: true},
	{Name: "participants", DataType: schema.TypeJSON, Nullable: true},
	{Name: "fireflies_users", DataType: schema.TypeJSON, Nullable: true},
	{Name: "calendar_id", DataType: schema.TypeString, Nullable: true},
	{Name: "cal_id", DataType: schema.TypeString, Nullable: true},
	{Name: "calendar_type", DataType: schema.TypeString, Nullable: true},
	{Name: "channels", DataType: schema.TypeJSON, Nullable: true},
	{Name: "speakers", DataType: schema.TypeJSON, Nullable: true},
	{Name: "analytics", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sentences", DataType: schema.TypeJSON, Nullable: true},
	{Name: "meeting_info", DataType: schema.TypeJSON, Nullable: true},
	{Name: "meeting_attendees", DataType: schema.TypeJSON, Nullable: true},
	{Name: "meeting_attendance", DataType: schema.TypeJSON, Nullable: true},
	{Name: "summary", DataType: schema.TypeJSON, Nullable: true},
	{Name: "user", DataType: schema.TypeJSON, Nullable: true},
	{Name: "apps_preview", DataType: schema.TypeJSON, Nullable: true},
	{Name: "_errors", DataType: schema.TypeJSON, Nullable: true},
}

var userFields = []schema.Column{
	{Name: "user_id", DataType: schema.TypeString, Nullable: false},
	{Name: "email", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "num_transcripts", DataType: schema.TypeInt64, Nullable: true},
	{Name: "recent_transcript", DataType: schema.TypeString, Nullable: true},
	{Name: "recent_meeting", DataType: schema.TypeString, Nullable: true},
	{Name: "minutes_consumed", DataType: schema.TypeInt64, Nullable: true},
	{Name: "is_admin", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "integrations", DataType: schema.TypeJSON, Nullable: true},
	{Name: "user_groups", DataType: schema.TypeJSON, Nullable: true},
}

var userGroupFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "handle", DataType: schema.TypeString, Nullable: true},
	{Name: "members", DataType: schema.TypeJSON, Nullable: true},
}

var channelFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "is_private", DataType: schema.TypeBoolean, Nullable: true},
	{Name: "created_by", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "members", DataType: schema.TypeJSON, Nullable: true},
}

var biteFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "transcript_id", DataType: schema.TypeString, Nullable: true},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "thumbnail", DataType: schema.TypeString, Nullable: true},
	{Name: "preview", DataType: schema.TypeString, Nullable: true},
	{Name: "status", DataType: schema.TypeString, Nullable: true},
	{Name: "summary", DataType: schema.TypeString, Nullable: true},
	{Name: "user_id", DataType: schema.TypeString, Nullable: true},
	{Name: "start_time", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "end_time", DataType: schema.TypeFloat64, Nullable: true},
	{Name: "summary_status", DataType: schema.TypeString, Nullable: true},
	{Name: "media_type", DataType: schema.TypeString, Nullable: true},
	{Name: "created_at", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "created_from", DataType: schema.TypeJSON, Nullable: true},
	{Name: "captions", DataType: schema.TypeJSON, Nullable: true},
	{Name: "sources", DataType: schema.TypeJSON, Nullable: true},
	{Name: "privacies", DataType: schema.TypeJSON, Nullable: true},
	{Name: "user", DataType: schema.TypeJSON, Nullable: true},
}

var contactFields = []schema.Column{
	{Name: "email", DataType: schema.TypeString, Nullable: false},
	{Name: "name", DataType: schema.TypeString, Nullable: true},
	{Name: "picture", DataType: schema.TypeString, Nullable: true},
	{Name: "last_meeting_date", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var activeMeetingFields = []schema.Column{
	{Name: "id", DataType: schema.TypeString, Nullable: false},
	{Name: "title", DataType: schema.TypeString, Nullable: true},
	{Name: "organizer_email", DataType: schema.TypeString, Nullable: true},
	{Name: "meeting_link", DataType: schema.TypeString, Nullable: true},
	{Name: "start_time", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "end_time", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "privacy", DataType: schema.TypeString, Nullable: true},
	{Name: "state", DataType: schema.TypeString, Nullable: true},
}

var analyticsFields = []schema.Column{
	{Name: "start_time", DataType: schema.TypeTimestampTZ, Nullable: false},
	{Name: "end_time", DataType: schema.TypeTimestampTZ, Nullable: false},
	{Name: "team", DataType: schema.TypeJSON, Nullable: true},
	{Name: "users", DataType: schema.TypeJSON, Nullable: true},
}

type FirefliesSource struct {
	apiKey string
	client *ingestrhttp.Client
}

func NewFirefliesSource() *FirefliesSource {
	return &FirefliesSource{}
}

func (s *FirefliesSource) HandlesIncrementality() bool {
	return true
}

func (s *FirefliesSource) Schemes() []string {
	return []string{"fireflies"}
}

func (s *FirefliesSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAPIKeyFromURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey

	s.client = ingestrhttp.New(
		ingestrhttp.WithBaseURL(graphQLBaseURL),
		ingestrhttp.WithTimeout(60*time.Second),
		ingestrhttp.WithRateLimiter(rateLimit, rateLimitBurst),
		ingestrhttp.WithDebug(config.DebugMode),
		ingestrhttp.WithHeader("Authorization", "Bearer "+s.apiKey),
		ingestrhttp.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[FIREFLIES] Connected successfully")
	return nil
}

func parseAPIKeyFromURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "fireflies://") {
		return "", fmt.Errorf("invalid fireflies URI: must start with fireflies://")
	}

	rest := strings.TrimPrefix(uri, "fireflies://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in URI query parameters")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse fireflies URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key query parameter is required")
	}

	return apiKey, nil
}

func (s *FirefliesSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *FirefliesSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	// Handle analytics with granularity suffix (e.g., analytics:DAY)
	if strings.HasPrefix(tableName, "analytics:") {
		tableName = "analytics"
	}

	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	tableSchema, err := s.getSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	incrementalKey := ""
	primaryKeys := tableSchema.PrimaryKeys
	strategy := config.StrategyReplace

	switch tableName {
	case "transcripts":
		incrementalKey = "date"
		strategy = config.StrategyMerge
	case "analytics":
		incrementalKey = "end_time"
		strategy = config.StrategyMerge
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return tableSchema, nil
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *FirefliesSource) getSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	var fields []schema.Column
	var primaryKeys []string

	switch table {
	case "transcripts":
		fields = transcriptFields
		primaryKeys = []string{"id"}
	case "users":
		fields = userFields
		primaryKeys = []string{"user_id"}
	case "user_groups":
		fields = userGroupFields
		primaryKeys = []string{"id"}
	case "channels":
		fields = channelFields
		primaryKeys = []string{"id"}
	case "bites":
		fields = biteFields
		primaryKeys = []string{"id"}
	case "contacts":
		fields = contactFields
		primaryKeys = []string{"email"}
	case "active_meetings":
		fields = activeMeetingFields
		primaryKeys = []string{"id"}
	case "analytics":
		fields = analyticsFields
		primaryKeys = []string{"start_time", "end_time"}
	default:
		return nil, fmt.Errorf("unsupported table: %s", table)
	}

	return &schema.TableSchema{
		Name:        table,
		Columns:     fields,
		PrimaryKeys: primaryKeys,
	}, nil
}

type dateRange struct {
	start *time.Time
	end   *time.Time
}

func extractDateRange(opts source.ReadOptions) dateRange {
	var dr dateRange
	if opts.IntervalStart != nil {
		dr.start = opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		dr.end = opts.IntervalEnd
	}
	// Log actual values, not pointer addresses
	startStr := "<nil>"
	endStr := "<nil>"
	if dr.start != nil {
		startStr = dr.start.Format(time.RFC3339)
	}
	if dr.end != nil {
		endStr = dr.end.Format(time.RFC3339)
	}
	config.Debug("[FIREFLIES] Date range - start: %s, end: %s", startStr, endStr)
	return dr
}

func (s *FirefliesSource) fetchAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	tableName, query, dataKey string,
	fields []schema.Column,
	transform func(map[string]interface{}) map[string]interface{},
) error {
	data, err := s.executeGraphQL(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", tableName, err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("failed to parse %s response: %w", tableName, err)
	}

	items, ok := result[dataKey].([]interface{})
	if !ok {
		// Handle single object response (e.g., user endpoint)
		if item, ok := result[dataKey].(map[string]interface{}); ok {
			items = []interface{}{item}
		} else {
			config.Debug("[FIREFLIES] No %s found or unexpected format", tableName)
			return nil
		}
	}

	if len(items) == 0 {
		config.Debug("[FIREFLIES] No %s found", tableName)
		return nil
	}

	var transformed []map[string]interface{}
	for _, item := range items {
		if itemMap, ok := item.(map[string]interface{}); ok {
			transformed = append(transformed, transform(itemMap))
		}
	}

	if len(transformed) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(transformed, fields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
		}
		config.Debug("[FIREFLIES] Sending %d %s records", len(transformed), tableName)
		results <- source.RecordBatchResult{Batch: record}
	}

	return nil
}

func (s *FirefliesSource) paginateAndSend(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	tableName, query, dataKey string,
	fields []schema.Column,
	extraVars map[string]interface{},
	transform func(map[string]interface{}) map[string]interface{},
) error {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = maxPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	skip := 0
	totalSent := 0
	batchNum := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		variables := make(map[string]interface{})
		for k, v := range extraVars {
			variables[k] = v
		}
		variables["limit"] = pageSize
		variables["skip"] = skip

		config.Debug("[FIREFLIES] Fetching %s with variables: %+v", tableName, variables)

		data, err := s.executeGraphQL(ctx, query, variables)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", tableName, err)
		}

		items, ok := result[dataKey].([]interface{})
		if !ok || len(items) == 0 {
			break
		}

		config.Debug("[FIREFLIES] Got %d items", len(items))

		var transformed []map[string]interface{}
		for _, item := range items {
			if itemMap, ok := item.(map[string]interface{}); ok {
				transformed = append(transformed, transform(itemMap))
			}

			if opts.Limit > 0 && totalSent+len(transformed) >= opts.Limit {
				break
			}
		}

		if len(transformed) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(transformed, fields, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
			}

			batchNum++
			config.Debug("[FIREFLIES] Sending batch %d with %d %s", batchNum, len(transformed), tableName)
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(transformed)
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			config.Debug("[FIREFLIES] Reached limit of %d records", opts.Limit)
			break
		}

		if len(items) < pageSize {
			break
		}

		skip += len(items)
	}

	config.Debug("[FIREFLIES] Finished reading %s, total records: %d", tableName, totalSent)
	return nil
}

func (s *FirefliesSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	// Handle analytics with granularity
	tableName := table
	if strings.HasPrefix(table, "analytics:") {
		tableName = "analytics"
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch tableName {
		case "transcripts":
			err = s.readTranscripts(ctx, opts, results)
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "user_groups":
			err = s.readUserGroups(ctx, opts, results)
		case "channels":
			err = s.readChannels(ctx, opts, results)
		case "bites":
			err = s.readBites(ctx, opts, results)
		case "contacts":
			err = s.readContacts(ctx, opts, results)
		case "active_meetings":
			err = s.readActiveMeetings(ctx, opts, results)
		case "analytics":
			err = s.readAnalytics(ctx, table, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string        `json:"message"`
	Path    []interface{} `json:"path,omitempty"`
}

func (s *FirefliesSource) executeGraphQL(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	data, _, err := s.executeGraphQLWithErrors(ctx, query, variables)
	return data, err
}

func (s *FirefliesSource) executeGraphQLWithErrors(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, []graphQLError, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	config.Debug("[FIREFLIES] Executing GraphQL query")

	var resp graphQLResponse
	httpResp, err := s.client.R(ctx).
		SetBody(reqBody).
		SetResult(&resp).
		Post("")
	if err != nil {
		return nil, nil, fmt.Errorf("graphql request failed: %w", err)
	}

	if !httpResp.IsSuccess() {
		return nil, nil, fmt.Errorf("graphql request failed with status %d: %s", httpResp.StatusCode(), httpResp.String())
	}

	// Only fail if there are errors AND no data at all
	if len(resp.Errors) > 0 && len(resp.Data) == 0 {
		var errMsgs []string
		for _, e := range resp.Errors {
			errMsgs = append(errMsgs, e.Message)
		}
		return nil, resp.Errors, fmt.Errorf("graphql errors: %s", strings.Join(errMsgs, "; "))
	}

	// Log warnings if there are partial errors
	if len(resp.Errors) > 0 {
		config.Debug("[FIREFLIES] GraphQL response has %d partial errors (data still available)", len(resp.Errors))
	}

	return resp.Data, resp.Errors, nil
}

const transcriptsQuery = `
query Transcripts(
  $limit: Int
  $skip: Int
  $fromDate: DateTime
  $toDate: DateTime
) {
  transcripts(
    limit: $limit
    skip: $skip
    fromDate: $fromDate
    toDate: $toDate
  ) {
    id
    title
    date
    duration
    transcript_url
    audio_url
    video_url
    meeting_link
    host_email
    organizer_email
    participants
    fireflies_users
    calendar_id
    cal_id
    calendar_type
    channels {
      id
    }
    speakers {
      id
      name
    }
    analytics {
      sentiments {
        negative_pct
        neutral_pct
        positive_pct
      }
      categories {
        questions
        date_times
        metrics
        tasks
      }
      speakers {
        speaker_id
        name
        duration
        duration_pct
        word_count
        words_per_minute
        longest_monologue
        monologues_count
        filler_words
        questions
      }
    }
    sentences {
      index
      speaker_name
      speaker_id
      text
      raw_text
      start_time
      end_time
      ai_filters {
        task
        pricing
        metric
        question
        date_and_time
        text_cleanup
        sentiment
      }
    }
    meeting_info {
      fred_joined
      silent_meeting
      summary_status
    }
    meeting_attendees {
      displayName
      email
      phoneNumber
      name
      location
    }
    meeting_attendance {
      name
      join_time
      leave_time
    }
    summary {
      keywords
      action_items
      outline
      shorthand_bullet
      overview
      bullet_gist
      gist
      short_summary
      short_overview
      meeting_type
      topics_discussed
      transcript_chapters
    }
    user {
      user_id
      email
      name
      num_transcripts
      recent_meeting
      minutes_consumed
      is_admin
      integrations
    }
    apps_preview {
      outputs {
        transcript_id
        user_id
        app_id
        created_at
        title
        prompt
        response
      }
    }
  }
}
`

const usersQuery = `
query Users {
  users {
    user_id
    email
    name
    num_transcripts
    recent_transcript
    recent_meeting
    minutes_consumed
    is_admin
    integrations
    user_groups { id name handle members { user_id first_name last_name email } }
  }
}
`

const userGroupsQuery = `
query UserGroups {
  user_groups {
    id
    name
    handle
    members { user_id first_name last_name email }
  }
}
`

const channelsQuery = `
query Channels {
  channels {
    id
    title
    is_private
    created_by
    created_at
    updated_at
    members { user_id email name }
  }
}
`

const bitesQuery = `
query Bites($my_team: Boolean, $limit: Int, $skip: Int) {
  bites(my_team: $my_team, limit: $limit, skip: $skip) {
    transcript_id
    name
    id
    thumbnail
    preview
    status
    summary
    user_id
    start_time
    end_time
    summary_status
    media_type
    created_at
    created_from { description duration id name type }
    captions { end_time index speaker_id speaker_name start_time text }
    sources { src type }
    privacies
    user { first_name last_name picture name id }
  }
}
`

const contactsQuery = `
query Contacts {
  contacts {
    email
    name
    picture
    last_meeting_date
  }
}
`

const activeMeetingsQuery = `
query ActiveMeetings {
  active_meetings {
    id
    title
    organizer_email
    meeting_link
    start_time
    end_time
    privacy
    state
  }
}
`

const analyticsQuery = `
query Analytics($startTime: String!, $endTime: String!) {
  analytics(start_time: $startTime, end_time: $endTime) {
    team {
      conversation {
        average_filler_words average_filler_words_diff_pct
        average_monologues_count average_monologues_count_diff_pct
        average_questions average_questions_diff_pct
        average_sentiments { negative_pct neutral_pct positive_pct }
        average_silence_duration average_silence_duration_diff_pct
        average_talk_listen_ratio average_words_per_minute
        longest_monologue_duration_sec longest_monologue_duration_diff_pct
        total_filler_words total_filler_words_diff_pct
        total_meeting_notes_count total_meetings_count
        total_monologues_count total_monologues_diff_pct teammates_count
        total_questions total_questions_diff_pct
        total_silence_duration total_silence_duration_diff_pct
      }
      meeting {
        count count_diff_pct duration duration_diff_pct
        average_count average_count_diff_pct average_duration average_duration_diff_pct
      }
    }
    users {
      user_id user_name user_email
      conversation {
        talk_listen_pct talk_listen_ratio
        total_silence_duration total_silence_duration_compare_to
        total_silence_pct total_silence_ratio
        total_speak_duration total_speak_duration_with_user total_word_count
        user_filler_words user_filler_words_compare_to user_filler_words_diff_pct
        user_longest_monologue_sec user_longest_monologue_compare_to user_longest_monologue_diff_pct
        user_monologues_count user_monologues_count_compare_to user_monologues_count_diff_pct
        user_questions user_questions_compare_to user_questions_diff_pct
        user_speak_duration user_word_count
        user_words_per_minute user_words_per_minute_compare_to user_words_per_minute_diff_pct
      }
      meeting {
        count count_diff count_diff_compared_to count_diff_pct
        duration duration_diff duration_diff_compared_to duration_diff_pct
      }
    }
  }
}
`

func (s *FirefliesSource) readTranscripts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading transcripts with parallel fetching")
	dr := extractDateRange(opts)

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = maxPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	// Result type for page fetch
	type pageResult struct {
		items     []map[string]interface{}
		pageIndex int
		hasMore   bool
		err       error
	}

	// Prepare base variables
	baseVars := make(map[string]interface{})
	if dr.start != nil {
		baseVars["fromDate"] = dr.start.Format("2006-01-02T15:04:05.000Z")
	}
	if dr.end != nil {
		baseVars["toDate"] = dr.end.Format("2006-01-02T15:04:05.000Z")
	}

	// Fetch a single page
	fetchPage := func(pageIndex int) pageResult {
		skip := pageIndex * pageSize
		variables := make(map[string]interface{})
		for k, v := range baseVars {
			variables[k] = v
		}
		variables["limit"] = pageSize
		variables["skip"] = skip

		data, gqlErrors, err := s.executeGraphQLWithErrors(ctx, transcriptsQuery, variables)
		if err != nil {
			return pageResult{pageIndex: pageIndex, err: err}
		}

		errorsByIndex := buildErrorMap(gqlErrors, "transcripts")

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return pageResult{pageIndex: pageIndex, err: err}
		}

		items, ok := result["transcripts"].([]interface{})
		if !ok || len(items) == 0 {
			return pageResult{pageIndex: pageIndex, hasMore: false}
		}

		var transformed []map[string]interface{}
		for i, item := range items {
			if itemMap, ok := item.(map[string]interface{}); ok {
				t := s.transformTranscript(itemMap)
				if errPaths, hasErrors := errorsByIndex[i]; hasErrors {
					t["_errors"] = errPaths
				} else {
					t["_errors"] = nil
				}
				transformed = append(transformed, t)
			}
		}

		return pageResult{
			items:     transformed,
			pageIndex: pageIndex,
			hasMore:   len(items) >= pageSize,
		}
	}

	// Use parallel fetching with speculative execution
	totalSent := 0
	currentPage := 0
	hasMore := true

	for hasMore {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pageChan := make(chan int, parallelWorkers)
		resultChan := make(chan pageResult, parallelWorkers)

		var wg sync.WaitGroup
		for i := 0; i < parallelWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for pageIdx := range pageChan {
					select {
					case <-ctx.Done():
						resultChan <- pageResult{pageIndex: pageIdx, err: ctx.Err()}
						return
					default:
					}
					resultChan <- fetchPage(pageIdx)
				}
			}()
		}

		// Queue pages to fetch
		pagesToFetch := parallelWorkers
		go func() {
			for i := 0; i < pagesToFetch; i++ {
				pageChan <- currentPage + i
			}
			close(pageChan)
		}()

		// Wait and close
		go func() {
			wg.Wait()
			close(resultChan)
		}()

		// Collect results in order
		pageResults := make([]pageResult, pagesToFetch)
		for res := range resultChan {
			if res.err != nil {
				return res.err
			}
			idx := res.pageIndex - currentPage
			if idx >= 0 && idx < pagesToFetch {
				pageResults[idx] = res
			}
		}

		// Process results in order
		for _, res := range pageResults {
			if len(res.items) > 0 {
				if opts.Limit > 0 && totalSent+len(res.items) > opts.Limit {
					// Trim to limit
					remaining := opts.Limit - totalSent
					res.items = res.items[:remaining]
				}

				record, err := arrowconv.ItemsToArrowRecordWithSchema(res.items, transcriptFields, opts.ExcludeColumns)
				if err != nil {
					return fmt.Errorf("failed to convert transcripts to Arrow: %w", err)
				}
				results <- source.RecordBatchResult{Batch: record}
				totalSent += len(res.items)
				config.Debug("[FIREFLIES] Sent batch with %d transcripts (total: %d)", len(res.items), totalSent)
			}

			if !res.hasMore {
				hasMore = false
				break
			}

			if opts.Limit > 0 && totalSent >= opts.Limit {
				hasMore = false
				break
			}
		}

		currentPage += pagesToFetch
	}

	config.Debug("[FIREFLIES] Finished reading transcripts, total records: %d", totalSent)
	return nil
}

// buildErrorMap extracts error paths grouped by item index
// Returns map[itemIndex][]string where []string contains the field paths that had errors
func buildErrorMap(errors []graphQLError, rootField string) map[int][]string {
	result := make(map[int][]string)

	for _, err := range errors {
		if len(err.Path) < 2 {
			continue
		}

		// Check if this error is for our root field
		if field, ok := err.Path[0].(string); !ok || field != rootField {
			continue
		}

		// Get the index (second element in path)
		var index int
		switch v := err.Path[1].(type) {
		case float64:
			index = int(v)
		case int:
			index = v
		default:
			continue
		}

		// Get the field name (third element in path if exists)
		if len(err.Path) >= 3 {
			if fieldName, ok := err.Path[2].(string); ok {
				result[index] = append(result[index], fieldName)
			}
		}
	}

	return result
}

func (s *FirefliesSource) transformTranscript(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getString(node, "id")
	result["title"] = getStringPtr(node, "title")
	result["date"] = parseFirefliesTimestamp(node["date"])
	result["duration"] = getFloatPtr(node, "duration")
	result["transcript_url"] = getStringPtr(node, "transcript_url")
	result["audio_url"] = getStringPtr(node, "audio_url")
	result["video_url"] = getStringPtr(node, "video_url")
	result["meeting_link"] = getStringPtr(node, "meeting_link")
	result["host_email"] = getStringPtr(node, "host_email")
	result["organizer_email"] = getStringPtr(node, "organizer_email")
	result["participants"] = node["participants"]
	result["fireflies_users"] = node["fireflies_users"]
	result["calendar_id"] = getStringPtr(node, "calendar_id")
	result["cal_id"] = getStringPtr(node, "cal_id")
	result["calendar_type"] = getStringPtr(node, "calendar_type")
	result["channels"] = node["channels"]
	result["speakers"] = node["speakers"]
	result["analytics"] = node["analytics"]
	result["sentences"] = node["sentences"]
	result["meeting_info"] = node["meeting_info"]
	result["meeting_attendees"] = node["meeting_attendees"]
	result["meeting_attendance"] = node["meeting_attendance"]
	result["summary"] = node["summary"]
	result["user"] = node["user"]
	result["apps_preview"] = node["apps_preview"]

	return result
}

func (s *FirefliesSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading users")
	return s.fetchAndSend(ctx, opts, results, "users", usersQuery, "users", userFields, s.transformUser)
}

func (s *FirefliesSource) transformUser(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["user_id"] = getString(node, "user_id")
	result["email"] = getStringPtr(node, "email")
	result["name"] = getStringPtr(node, "name")
	result["num_transcripts"] = getIntPtr(node, "num_transcripts")
	result["recent_transcript"] = getStringPtr(node, "recent_transcript")
	result["recent_meeting"] = getStringPtr(node, "recent_meeting")
	result["minutes_consumed"] = getIntPtr(node, "minutes_consumed")
	result["is_admin"] = getBoolPtr(node, "is_admin")
	result["integrations"] = node["integrations"]
	result["user_groups"] = node["user_groups"]

	return result
}

func (s *FirefliesSource) readUserGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading user groups")
	return s.fetchAndSend(ctx, opts, results, "user_groups", userGroupsQuery, "user_groups", userGroupFields, s.transformUserGroup)
}

func (s *FirefliesSource) transformUserGroup(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getString(node, "id")
	result["name"] = getStringPtr(node, "name")
	result["handle"] = getStringPtr(node, "handle")
	result["members"] = node["members"]

	return result
}

func (s *FirefliesSource) readChannels(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading channels")
	return s.fetchAndSend(ctx, opts, results, "channels", channelsQuery, "channels", channelFields, s.transformChannel)
}

func (s *FirefliesSource) transformChannel(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getString(node, "id")
	result["title"] = getStringPtr(node, "title")
	result["is_private"] = getBoolPtr(node, "is_private")
	result["created_by"] = getStringPtr(node, "created_by")
	result["created_at"] = parseFirefliesTimestamp(node["created_at"])
	result["updated_at"] = parseFirefliesTimestamp(node["updated_at"])
	result["members"] = node["members"]

	return result
}

func (s *FirefliesSource) readBites(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading bites")
	variables := map[string]interface{}{
		"my_team": true,
	}
	return s.paginateAndSend(ctx, opts, results, "bites", bitesQuery, "bites",
		biteFields, variables, s.transformBite)
}

func (s *FirefliesSource) transformBite(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getString(node, "id")
	result["transcript_id"] = getStringPtr(node, "transcript_id")
	result["name"] = getStringPtr(node, "name")
	result["thumbnail"] = getStringPtr(node, "thumbnail")
	result["preview"] = getStringPtr(node, "preview")
	result["status"] = getStringPtr(node, "status")
	result["summary"] = getStringPtr(node, "summary")
	result["user_id"] = getStringPtr(node, "user_id")
	result["start_time"] = getFloatPtr(node, "start_time")
	result["end_time"] = getFloatPtr(node, "end_time")
	result["summary_status"] = getStringPtr(node, "summary_status")
	result["media_type"] = getStringPtr(node, "media_type")
	result["created_at"] = parseTimestampPtr(getString(node, "created_at"))
	result["created_from"] = node["created_from"]
	result["captions"] = node["captions"]
	result["sources"] = node["sources"]
	result["privacies"] = node["privacies"]
	result["user"] = node["user"]

	return result
}

func (s *FirefliesSource) readContacts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading contacts")
	return s.fetchAndSend(ctx, opts, results, "contacts", contactsQuery, "contacts", contactFields, s.transformContact)
}

func (s *FirefliesSource) transformContact(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["email"] = getString(node, "email")
	result["name"] = getStringPtr(node, "name")
	result["picture"] = getStringPtr(node, "picture")
	result["last_meeting_date"] = parseTimestampPtr(getString(node, "last_meeting_date"))

	return result
}

func (s *FirefliesSource) readActiveMeetings(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading active meetings")
	return s.fetchAndSend(ctx, opts, results, "active_meetings", activeMeetingsQuery, "active_meetings", activeMeetingFields, s.transformActiveMeeting)
}

func (s *FirefliesSource) transformActiveMeeting(node map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["id"] = getString(node, "id")
	result["title"] = getStringPtr(node, "title")
	result["organizer_email"] = getStringPtr(node, "organizer_email")
	result["meeting_link"] = getStringPtr(node, "meeting_link")
	result["start_time"] = parseTimestampPtr(getString(node, "start_time"))
	result["end_time"] = parseTimestampPtr(getString(node, "end_time"))
	result["privacy"] = getStringPtr(node, "privacy")
	result["state"] = getStringPtr(node, "state")

	return result
}

func (s *FirefliesSource) readAnalytics(ctx context.Context, table string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[FIREFLIES] Reading analytics with parallel fetching")

	granularity := ""
	if strings.Contains(table, ":") {
		parts := strings.SplitN(table, ":", 2)
		if len(parts) == 2 {
			granularity = strings.ToUpper(parts[1])
		}
	}

	dr := extractDateRange(opts)
	startTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Now().UTC()

	if dr.start != nil {
		startTime = *dr.start
	}
	if dr.end != nil {
		endTime = *dr.end
	}

	// Build list of time chunks
	type timeChunk struct {
		start time.Time
		end   time.Time
	}
	var chunks []timeChunk
	currentStart := startTime
	for currentStart.Before(endTime) {
		chunkEnd := s.calculateChunkEnd(currentStart, endTime, granularity)
		chunks = append(chunks, timeChunk{start: currentStart, end: chunkEnd})
		currentStart = chunkEnd
	}

	config.Debug("[FIREFLIES] Created %d time chunks for parallel fetching", len(chunks))

	// Result type for worker results
	type fetchResult struct {
		item  map[string]interface{}
		index int
		err   error
	}

	chunkChan := make(chan struct {
		chunk timeChunk
		index int
	}, len(chunks))
	resultChan := make(chan fetchResult, len(chunks))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < parallelWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range chunkChan {
				select {
				case <-ctx.Done():
					resultChan <- fetchResult{err: ctx.Err(), index: job.index}
					return
				default:
				}

				variables := map[string]interface{}{
					"startTime": job.chunk.start.Format(time.RFC3339),
					"endTime":   job.chunk.end.Format(time.RFC3339),
				}

				data, err := s.executeGraphQL(ctx, analyticsQuery, variables)
				if err != nil {
					config.Debug("[FIREFLIES] Error fetching analytics chunk: %v", err)
					resultChan <- fetchResult{index: job.index}
					continue
				}

				var result map[string]interface{}
				if err := json.Unmarshal(data, &result); err != nil {
					resultChan <- fetchResult{index: job.index}
					continue
				}

				if analytics, ok := result["analytics"].(map[string]interface{}); ok {
					item := s.transformAnalytics(analytics, job.chunk.start, job.chunk.end)
					resultChan <- fetchResult{item: item, index: job.index}
				} else {
					config.Debug("[FIREFLIES] No analytics data for chunk %d", job.index)
					resultChan <- fetchResult{index: job.index}
				}
			}
		}()
	}

	// Send chunks to workers
	go func() {
		for i, chunk := range chunks {
			chunkChan <- struct {
				chunk timeChunk
				index int
			}{chunk: chunk, index: i}
		}
		close(chunkChan)
	}()

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results (maintain order)
	resultItems := make([]map[string]interface{}, len(chunks))
	for res := range resultChan {
		if res.err != nil {
			return res.err
		}
		if res.item != nil {
			resultItems[res.index] = res.item
		}
	}

	// Filter out nil results and send
	var allItems []map[string]interface{}
	for _, item := range resultItems {
		if item != nil {
			allItems = append(allItems, item)
		}
	}

	if len(allItems) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(allItems, analyticsFields, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert analytics to Arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[FIREFLIES] Sent %d analytics records", len(allItems))
	return nil
}

func (s *FirefliesSource) calculateChunkEnd(start, end time.Time, granularity string) time.Time {
	var chunkEnd time.Time

	switch granularity {
	case "HOUR":
		chunkEnd = start.Add(time.Hour).Truncate(time.Hour)
	case "DAY":
		chunkEnd = start.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	case "MONTH":
		chunkEnd = time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	default:
		chunkEnd = start.AddDate(0, 0, maxAnalyticsDays)
	}

	if chunkEnd.After(end) {
		chunkEnd = end
	}

	return chunkEnd
}

func (s *FirefliesSource) transformAnalytics(node map[string]interface{}, startTime, endTime time.Time) map[string]interface{} {
	result := make(map[string]interface{})

	result["start_time"] = &startTime
	result["end_time"] = &endTime
	result["team"] = node["team"]
	result["users"] = node["users"]

	return result
}

func getString(m map[string]interface{}, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case fmt.Stringer:
		return v.String()
	}
	return ""
}

func getStringPtr(m map[string]interface{}, key string) interface{} {
	val, ok := m[key]
	if !ok || val == nil {
		return nil
	}

	switch v := val.(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
		return nil
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case fmt.Stringer:
		return v.String()
	default:
		return nil
	}
}

func getIntPtr(m map[string]interface{}, key string) interface{} {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return nil
}

func getFloatPtr(m map[string]interface{}, key string) interface{} {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return nil
}

func getBoolPtr(m map[string]interface{}, key string) interface{} {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return nil
}

func parseTimestampPtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func parseFirefliesTimestamp(val interface{}) *time.Time {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case float64:
		// Fireflies returns date as milliseconds timestamp
		t := time.Unix(int64(v)/1000, 0).UTC()
		return &t
	case string:
		return parseTimestampPtr(v)
	}
	return nil
}

var _ source.Source = (*FirefliesSource)(nil)
