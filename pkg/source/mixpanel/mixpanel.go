package mixpanel

import (
	"bufio"
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
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	// Mixpanel Export/Query API: 60 queries/hour, 3 queries/second for export.
	// Using ~0.8 req/s to stay safely under the hourly limit for sustained pagination.
	rateLimit      = 0.8
	rateLimitBurst = 3
	maxPageSize    = 1000 // Engage API returns up to 1000 profiles per page
)

var supportedTables = []string{
	"events",
	"profiles",
}

var validServers = map[string]bool{
	"us": true,
	"eu": true,
	"in": true,
}

type MixpanelSource struct {
	exportClient    *gonghttp.Client
	engageClient    *gonghttp.Client
	projectID       string
	nullWarningOnce sync.Once
}

func NewMixpanelSource() *MixpanelSource {
	return &MixpanelSource{}
}

func (s *MixpanelSource) HandlesIncrementality() bool {
	return true
}

func (s *MixpanelSource) Schemes() []string {
	return []string{"mixpanel"}
}

func (s *MixpanelSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.projectID = creds.projectID

	exportBaseURL := exportBaseURL(creds.server)
	engageBaseURL := engageBaseURL(creds.server)

	s.exportClient = gonghttp.New(
		gonghttp.WithBaseURL(exportBaseURL),
		gonghttp.WithTimeout(120*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(creds.username, creds.password)),
	)

	s.engageClient = gonghttp.New(
		gonghttp.WithBaseURL(engageBaseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithRateLimiter(rateLimit, rateLimitBurst),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewBasicAuth(creds.username, creds.password)),
	)

	config.Debug("[MIXPANEL] Connected to project %s (server: %s)", creds.projectID, creds.server)
	return nil
}

func (s *MixpanelSource) Close(ctx context.Context) error {
	var firstErr error
	if s.exportClient != nil {
		if err := s.exportClient.Close(); err != nil {
			firstErr = err
		}
	}
	if s.engageClient != nil {
		if err := s.engageClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *MixpanelSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	if !isValidTable(tableName) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	incrementalKey := ""
	switch tableName {
	case "events":
		incrementalKey = "time"
	case "profiles":
		incrementalKey = "last_seen"
	}

	primaryKeys := []string{"distinct_id"}
	if tableName == "events" {
		primaryKeys = []string{"insert_id"}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    primaryKeys,
		TableIncrementalKey: incrementalKey,
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("mixpanel source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, opts)
		},
	}, nil
}

func (s *MixpanelSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "events":
			err = s.readEvents(ctx, opts, results)
		case "profiles":
			err = s.readProfiles(ctx, opts, results)
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

type mixpanelCredentials struct {
	username  string
	password  string
	projectID string
	server    string
}

func parseURI(uri string) (mixpanelCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return mixpanelCredentials{}, fmt.Errorf("invalid mixpanel URI: %w", err)
	}

	if parsed.Scheme != "mixpanel" {
		return mixpanelCredentials{}, fmt.Errorf("invalid mixpanel URI: must start with mixpanel://")
	}

	params := parsed.Query()

	username := params.Get("username")
	password := params.Get("password")
	apiSecret := params.Get("api_secret")

	if username == "" && apiSecret == "" {
		return mixpanelCredentials{}, fmt.Errorf("either username/password or api_secret is required in mixpanel URI")
	}

	if apiSecret != "" && username == "" {
		username = apiSecret
		password = ""
	}

	projectID := params.Get("project_id")
	if projectID == "" && apiSecret == "" {
		return mixpanelCredentials{}, fmt.Errorf("project_id is required in mixpanel URI when using service account authentication")
	}

	server := params.Get("server")
	if server == "" {
		server = "eu"
	}
	if !validServers[server] {
		return mixpanelCredentials{}, fmt.Errorf("invalid server %q: must be one of us, eu, in", server)
	}

	return mixpanelCredentials{
		username:  username,
		password:  password,
		projectID: projectID,
		server:    server,
	}, nil
}

func exportBaseURL(server string) string {
	switch server {
	case "us":
		return "https://data.mixpanel.com"
	case "in":
		return "https://data-in.mixpanel.com"
	default:
		return "https://data-eu.mixpanel.com"
	}
}

func engageBaseURL(server string) string {
	switch server {
	case "us":
		return "https://mixpanel.com"
	case "in":
		return "https://in.mixpanel.com"
	default:
		return "https://eu.mixpanel.com"
	}
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

var eventColumns = []schema.Column{
	{Name: "source_section_rank", DataType: schema.TypeInt64, Nullable: true},
	{Name: "date", DataType: schema.TypeTimestamp, Nullable: true},
}

func (s *MixpanelSource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[MIXPANEL] reading events")

	fromDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Now().UTC()

	if opts.IntervalStart != nil {
		fromDate = *opts.IntervalStart
	}
	if opts.IntervalEnd != nil {
		toDate = *opts.IntervalEnd
	}

	req := s.exportClient.R(ctx).
		SetQueryParam("from_date", fromDate.Format("2006-01-02")).
		SetQueryParam("to_date", toDate.Format("2006-01-02")).
		SetHeader("accept", "text/plain")

	if s.projectID != "" {
		req.SetQueryParam("project_id", s.projectID)
	}

	resp, err := req.Get("/api/2.0/export/")
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("mixpanel events returned status %d: %s", resp.StatusCode(), resp.String())
	}

	scanner := bufio.NewScanner(bytes.NewReader(resp.Body()))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	batch := make([]map[string]interface{}, 0, 500)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event map[string]interface{}
		if err := jsonUseNumber(line, &event); err != nil {
			config.Debug("[MIXPANEL] skipping malformed event line: %v", err)
			continue
		}

		s.flattenProperties(event)

		batch = append(batch, event)

		if len(batch) >= 500 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, eventColumns, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for events: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			batch = batch[:0]
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read events response: %w", err)
	}

	if len(batch) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, eventColumns, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record for events: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}
	}

	config.Debug("[MIXPANEL] finished reading events")
	return nil
}

// flattenProperties extracts properties from the nested "properties" map into the root,
// stripping the "$" prefix from property names, matching ingestr behavior.
func (s *MixpanelSource) flattenProperties(event map[string]interface{}) {
	props, ok := event["properties"].(map[string]interface{})
	if !ok {
		return
	}

	for key, value := range props {
		key = strings.TrimPrefix(key, "$")
		key = naming.ToSnakeCase(key)
		if str, ok := value.(string); ok && (str == "undefined" || str == "<null>") {
			s.nullWarningOnce.Do(func() {
				fmt.Printf("\nWarning: Mixpanel returns \"undefined\" and \"<null>\" as string literals for missing values, these will be converted to NULL\n")
			})
			value = nil
		}
		event[key] = value
	}

	delete(event, "properties")
}

func (s *MixpanelSource) readProfiles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[MIXPANEL] reading profiles")

	page := 0
	sessionID := ""
	totalProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req := s.engageClient.R(ctx).
			SetQueryParam("page", strconv.Itoa(page)).
			SetHeader("accept", "application/json").
			SetHeader("content-type", "application/x-www-form-urlencoded")

		if s.projectID != "" {
			req.SetQueryParam("project_id", s.projectID)
		}
		if sessionID != "" {
			req.SetQueryParam("session_id", sessionID)
		}

		if opts.IntervalStart != nil || opts.IntervalEnd != nil {
			startDT := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			endDT := time.Now().UTC()
			if opts.IntervalStart != nil {
				startDT = *opts.IntervalStart
			}
			if opts.IntervalEnd != nil {
				endDT = *opts.IntervalEnd
			}
			where := fmt.Sprintf(
				`properties["$last_seen"] >= "%s" and properties["$last_seen"] <= "%s"`,
				startDT.Format("2006-01-02T15:04:05"),
				endDT.Format("2006-01-02T15:04:05"),
			)
			req.SetQueryParam("where", where)
		}

		resp, err := req.Post("/api/query/engage")
		if err != nil {
			return fmt.Errorf("failed to fetch profiles: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("mixpanel profiles returned status %d: %s", resp.StatusCode(), resp.String())
		}

		var data map[string]interface{}
		if err := jsonUseNumber(resp.Body(), &data); err != nil {
			return fmt.Errorf("failed to parse profiles response: %w", err)
		}

		rawResults, ok := data["results"].([]interface{})
		if !ok || len(rawResults) == 0 {
			break
		}

		items := make([]map[string]interface{}, 0, len(rawResults))
		for _, raw := range rawResults {
			result, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}

			flattenProfileProperties(result)
			items = append(items, result)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to build arrow record for profiles: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.RecordBatchResult{Batch: record}:
			}

			totalProcessed += len(items)
		}

		if sid, ok := data["session_id"].(string); ok {
			sessionID = sid
		}

		if len(rawResults) < maxPageSize {
			break
		}

		page++
	}

	config.Debug("[MIXPANEL] finished reading profiles: %d total records", totalProcessed)
	return nil
}

// flattenProfileProperties extracts $properties into the root, strips $ prefixes,
// and moves $distinct_id to distinct_id, matching ingestr behavior.
func flattenProfileProperties(profile map[string]interface{}) {
	props, ok := profile["$properties"].(map[string]interface{})
	if ok {
		for key, value := range props {
			if strings.HasPrefix(key, "$") {
				if key == "$last_seen" {
					if ts, ok := value.(string); ok {
						if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
							profile["last_seen"] = parsed.Format(time.RFC3339Nano)
						} else if parsed, err := time.Parse("2006-01-02T15:04:05", ts); err == nil {
							profile["last_seen"] = parsed.Format(time.RFC3339Nano)
						} else {
							profile["last_seen"] = value
						}
					} else {
						profile["last_seen"] = value
					}
				} else {
					profile[key[1:]] = value
				}
			} else {
				profile[key] = value
			}
		}
		delete(profile, "$properties")
	}

	if distinctID, ok := profile["$distinct_id"]; ok {
		profile["distinct_id"] = distinctID
		delete(profile, "$distinct_id")
	}
}
