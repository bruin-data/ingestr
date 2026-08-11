package okta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
	maxPageSize          = 200
	groupMembersPageSize = 1000
	appUsersPageSize     = 500
	defaultParallelism   = 5
	maxPages             = 10000

	// Core endpoint buckets (/users, /apps, /groups, /devices, /policies) cap at
	// ~600 req/min per token on developer/Integrator orgs; 80% of that is 8 req/s.
	coreRateLimit      = 8.0
	coreRateLimitBurst = 5

	// The System Log (/api/v1/logs) bucket is much lower (~60 req/min); keep the
	// burst small so burst + rateLimit*60 stays under that ceiling.
	logsRateLimit      = 0.8
	logsRateLimitBurst = 2

	// Okta's ~60s rate-limit window outlasts the default retry budget (3
	// attempts, 30s max wait), so a wider one is needed to ride out a 429.
	retryCount   = 5
	retryWait    = 1 * time.Second
	retryMaxWait = 65 * time.Second

	// epochTime seeds "full load" queries; Okta clamps it to whatever history
	// it actually retains, so it always matches everything available.
	epochTime = "1970-01-01T00:00:00.000Z"
)

var supportedTables = []string{
	"users",
	"groups",
	"group_members",
	"applications",
	"application_users",
	"application_groups",
	"system_log_events",
	"devices",
	"policies",
	"policy_rules",
	"roles",
}

// policyTypes enumerates the policy types Okta accepts on the required `type`
// query param. Types unavailable on a given org return a 4xx and are skipped.
var policyTypes = []string{
	"OKTA_SIGN_ON",
	"PASSWORD",
	"MFA_ENROLL",
	"ACCESS_POLICY",
	"PROFILE_ENROLLMENT",
	"IDP_DISCOVERY",
	"POST_AUTH_SESSION",
	"ENTITY_RISK",
	"RESOURCE_ACCESS",
}

type tableMeta struct {
	primaryKeys    []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

var tableRegistry = map[string]tableMeta{
	"users":              {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"groups":             {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"group_members":      {[]string{"group_id", "id"}, "", config.StrategyReplace},
	"applications":       {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"application_users":  {[]string{"app_id", "id"}, "", config.StrategyReplace},
	"application_groups": {[]string{"app_id", "id"}, "", config.StrategyReplace},
	"system_log_events":  {[]string{"uuid"}, "published", config.StrategyMerge},
	"devices":            {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"policies":           {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"policy_rules":       {[]string{"id"}, "lastUpdated", config.StrategyMerge},
	"roles":              {[]string{"id"}, "", config.StrategyReplace},
}

type OktaSource struct {
	domain     string
	apiKey     string
	client     *httpclient.Client
	logsClient *httpclient.Client
}

func NewOktaSource() *OktaSource {
	return &OktaSource{}
}

func (s *OktaSource) Schemes() []string {
	return []string{"okta"}
}

func (s *OktaSource) HandlesIncrementality() bool {
	return true
}

func (s *OktaSource) Connect(ctx context.Context, uri string) error {
	domain, apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}
	s.domain = domain
	s.apiKey = apiKey

	baseURL := fmt.Sprintf("https://%s/api/v1", domain)
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(coreRateLimit, coreRateLimitBurst),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		httpclient.WithHeader("Authorization", "SSWS "+apiKey),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithDebug(config.DebugMode),
	)
	s.logsClient = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(logsRateLimit, logsRateLimitBurst),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		httpclient.WithHeader("Authorization", "SSWS "+apiKey),
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithDebug(config.DebugMode),
	)

	config.Debug("[OKTA] Connected to %s", domain)
	return nil
}

func (s *OktaSource) Close(ctx context.Context) error {
	var err error
	if s.client != nil {
		err = s.client.Close()
	}
	if s.logsClient != nil {
		if e := s.logsClient.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

func parseURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "okta://") {
		return "", "", fmt.Errorf("invalid okta URI: must start with okta://")
	}

	rest := strings.TrimPrefix(uri, "okta://")
	hostPart, query, _ := strings.Cut(rest, "?")

	values, err := url.ParseQuery(query)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse okta URI query: %w", err)
	}

	domain := hostPart
	if domain == "" {
		domain = values.Get("domain")
	}
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")
	if domain == "" {
		return "", "", fmt.Errorf("okta domain is required (e.g. okta://your-org.okta.com?api_key=TOKEN)")
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", "", fmt.Errorf("api_key is required in okta URI")
	}

	return domain, apiKey, nil
}

func (s *OktaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	meta, ok := tableRegistry[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    meta.primaryKeys,
		TableIncrementalKey: meta.incrementalKey,
		TableStrategy:       meta.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("okta source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *OktaSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "users":
			err = s.readUsers(ctx, opts, results)
		case "groups":
			err = s.readGroups(ctx, opts, results)
		case "group_members":
			err = s.readGroupMembers(ctx, opts, results)
		case "applications":
			err = s.readApplications(ctx, opts, results)
		case "application_users":
			err = s.readApplicationChildren(ctx, "users", opts, results)
		case "application_groups":
			err = s.readApplicationChildren(ctx, "groups", opts, results)
		case "system_log_events":
			err = s.readSystemLog(ctx, opts, results)
		case "devices":
			err = s.readDevices(ctx, opts, results)
		case "policies":
			err = s.readPolicies(ctx, opts, results)
		case "policy_rules":
			err = s.readPolicyRules(ctx, opts, results)
		case "roles":
			err = s.readRoles(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

// readSpec describes a single paginated endpoint read.
type readSpec struct {
	client            *httpclient.Client
	initialURL        string
	tolerant          bool // skip (rather than fail) on a non-success status
	extract           func([]byte) ([]map[string]interface{}, error)
	nextPage          func([]byte, http.Header) string // optional; defaults to Link-header cursor
	transform         func(map[string]interface{}) map[string]interface{}
	clientFilterField string // when set, filter each page by this timestamp field
}

func (s *OktaSource) readUsers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading users")
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxPageSize))
	// Only the default no-query list omits DEPROVISIONED users, so always send
	// a `filter` (not the eventually-consistent `search`); it covers a full load too.
	q.Set("filter", updatedExprOrAll("lastUpdated", opts))
	return s.send(ctx, readSpec{client: s.client, initialURL: "/users?" + q.Encode(), extract: flatExtract}, opts, results, "users")
}

func (s *OktaSource) readGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading groups")
	return s.send(ctx, readSpec{client: s.client, initialURL: groupsURL("lastUpdated", opts), extract: flatExtract}, opts, results, "groups")
}

func (s *OktaSource) readDevices(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading devices")
	// Use the plain (strongly consistent) list endpoint, not `search`, and
	// filter client-side since /devices ignores a server-side time filter.
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxPageSize))
	return s.send(ctx, readSpec{client: s.client, initialURL: "/devices?" + q.Encode(), extract: flatExtract, clientFilterField: "lastUpdated"}, opts, results, "devices")
}

func (s *OktaSource) readApplications(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading applications")
	// /apps has no server-side lastUpdated filter, so filter client-side.
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxPageSize))
	return s.send(ctx, readSpec{client: s.client, initialURL: "/apps?" + q.Encode(), extract: flatExtract, clientFilterField: "lastUpdated"}, opts, results, "applications")
}

func (s *OktaSource) readSystemLog(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading system_log_events")
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxPageSize))
	q.Set("sortOrder", "ASCENDING")
	// Always send both `since` and `until`: leaving either unset puts Okta's
	// /logs into "polling" mode (a partial, possibly out-of-order stream).
	since := epochTime
	if opts.IntervalStart != nil {
		since = oktaTime(*opts.IntervalStart)
	}
	q.Set("since", since)
	until := time.Now().UTC()
	if opts.IntervalEnd != nil {
		until = *opts.IntervalEnd
	}
	q.Set("until", oktaTime(until))
	return s.send(ctx, readSpec{client: s.logsClient, initialURL: "/logs?" + q.Encode(), extract: flatExtract}, opts, results, "system_log_events")
}

func (s *OktaSource) readGroupMembers(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading group_members")
	// Full replace: lastMembershipUpdated only tracks add/remove, not a member's
	// own attribute changes, so gate on nothing and snapshot every member.
	groupIDs, err := s.collectIDs(ctx, s.client, groupsURL("lastMembershipUpdated", source.ReadOptions{}), "id", false)
	if err != nil {
		return fmt.Errorf("failed to list groups for group_members: %w", err)
	}
	return s.fanOut(
		ctx, groupIDs, opts, results, "group_members", "",
		func(id string) string {
			return fmt.Sprintf("/groups/%s/users?limit=%d", id, groupMembersPageSize)
		},
		func(groupID string, item map[string]interface{}) map[string]interface{} {
			item["group_id"] = groupID
			return item
		},
	)
}

func (s *OktaSource) readApplicationChildren(ctx context.Context, child string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	label := "application_" + child
	config.Debug("[OKTA] reading %s", label)
	appIDs, err := s.collectIDs(ctx, s.client, "/apps?limit="+strconv.Itoa(maxPageSize), "id", false)
	if err != nil {
		return fmt.Errorf("failed to list apps for %s: %w", label, err)
	}
	limit := maxPageSize
	if child == "users" {
		limit = appUsersPageSize
	}
	// Full replace: an assignment listing has no removal signal, so gate on
	// nothing and snapshot every current assignment (same as group_members).
	return s.fanOut(
		ctx, appIDs, opts, results, label, "",
		func(id string) string {
			return fmt.Sprintf("/apps/%s/%s?limit=%d", id, child, limit)
		},
		func(appID string, item map[string]interface{}) map[string]interface{} {
			item["app_id"] = appID
			return item
		},
	)
}

func (s *OktaSource) readPolicies(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading policies")
	for _, t := range policyTypes {
		q := url.Values{}
		q.Set("type", t)
		q.Set("limit", strconv.Itoa(maxPageSize))
		spec := readSpec{client: s.client, initialURL: "/policies?" + q.Encode(), tolerant: true, extract: flatExtract, clientFilterField: "lastUpdated"}
		if err := s.send(ctx, spec, opts, results, "policies:"+t); err != nil {
			return err
		}
	}
	return nil
}

func (s *OktaSource) readPolicyRules(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading policy_rules")
	var policyIDs []string
	for _, t := range policyTypes {
		q := url.Values{}
		q.Set("type", t)
		q.Set("limit", strconv.Itoa(maxPageSize))
		ids, err := s.collectIDs(ctx, s.client, "/policies?"+q.Encode(), "id", true)
		if err != nil {
			return fmt.Errorf("failed to list policies for policy_rules: %w", err)
		}
		policyIDs = append(policyIDs, ids...)
	}
	return s.fanOut(
		ctx, policyIDs, opts, results, "policy_rules", "lastUpdated",
		func(id string) string {
			return fmt.Sprintf("/policies/%s/rules", id)
		},
		func(policyID string, item map[string]interface{}) map[string]interface{} {
			item["policy_id"] = policyID
			return item
		},
	)
}

func (s *OktaSource) readRoles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[OKTA] reading roles")
	// Unlike every other endpoint here, /iam/roles' next-page cursor is a
	// body-embedded `_links.next.href`, not a Link header.
	spec := readSpec{client: s.client, initialURL: "/iam/roles?limit=" + strconv.Itoa(maxPageSize), extract: envelopeExtract("roles"), nextPage: rolesNextPage}
	return s.send(ctx, spec, opts, results, "roles")
}

func rolesNextPage(body []byte, _ http.Header) string {
	var env struct {
		Links struct {
			Next struct {
				Href string `json:"href"`
			} `json:"next"`
		} `json:"_links"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	return env.Links.Next.Href
}

// send paginates the endpoint described by spec, converting each page to an
// Arrow record and streaming it on results.
func (s *OktaSource) send(ctx context.Context, spec readSpec, opts source.ReadOptions, results chan<- source.RecordBatchResult, label string) error {
	total := 0
	err := s.walk(ctx, spec.client, spec.initialURL, spec.tolerant, spec.extract, spec.nextPage, func(items []map[string]interface{}) error {
		if spec.transform != nil {
			for i := range items {
				items[i] = spec.transform(items[i])
			}
		}
		if spec.clientFilterField != "" {
			items = filterItemsByInterval(items, spec.clientFilterField, opts.IntervalStart, opts.IntervalEnd)
		}
		if len(items) == 0 {
			return nil
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", label, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}
		total += len(items)
		config.Debug("[OKTA] %s: sent %d rows (total: %d)", label, len(items), total)
		return nil
	})
	if err != nil {
		return err
	}
	config.Debug("[OKTA] finished %s: %d rows", label, total)
	return nil
}

// collectIDs paginates an endpoint and returns the value of `field` from every item.
func (s *OktaSource) collectIDs(ctx context.Context, client *httpclient.Client, initialURL, field string, tolerant bool) ([]string, error) {
	var ids []string
	err := s.walk(ctx, client, initialURL, tolerant, flatExtract, nil, func(items []map[string]interface{}) error {
		for _, it := range items {
			if v, ok := it[field].(string); ok && v != "" {
				ids = append(ids, v)
			}
		}
		return nil
	})
	return ids, err
}

// fanOut fetches a child endpoint for each id in parallel, injecting the parent
// id into every row via inject.
func (s *OktaSource) fanOut(ctx context.Context, ids []string, opts source.ReadOptions, results chan<- source.RecordBatchResult, label, clientFilterField string, buildURL func(string) string, inject func(string, map[string]interface{}) map[string]interface{}) error {
	if len(ids) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	idCh := make(chan string)
	errs := make(chan error, 1)
	var wg sync.WaitGroup

	for i := 0; i < defaultParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idCh {
				spec := readSpec{
					client:            s.client,
					initialURL:        buildURL(id),
					extract:           flatExtract,
					clientFilterField: clientFilterField,
					transform: func(item map[string]interface{}) map[string]interface{} {
						return inject(id, item)
					},
				}
				if err := s.send(ctx, spec, opts, results, label); err != nil {
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

	for _, id := range ids {
		select {
		case idCh <- id:
		case <-ctx.Done():
		}
	}
	close(idCh)

	wg.Wait()
	close(errs)

	return <-errs
}

// walk follows Okta's pagination cursor (by default the Link header, or
// nextPage if set) invoking fn for each page.
func (s *OktaSource) walk(ctx context.Context, client *httpclient.Client, initialURL string, tolerant bool, extract func([]byte) ([]map[string]interface{}, error), nextPage func([]byte, http.Header) string, fn func([]map[string]interface{}) error) error {
	if nextPage == nil {
		nextPage = func(_ []byte, h http.Header) string { return nextLink(h) }
	}
	nextURL := initialURL
	pages := 0

	for nextURL != "" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pages++
		if pages > maxPages {
			return fmt.Errorf("okta API %s exceeded max pages (%d); results would be incomplete", initialURL, maxPages)
		}

		resp, err := client.R(ctx).Get(nextURL)
		if err != nil {
			return fmt.Errorf("request to %s failed: %w", nextURL, err)
		}
		if !resp.IsSuccess() {
			// Only the codes Okta returns for an unsupported policy type are
			// tolerated; auth failures, exhausted-retry 429s, and 5xx still error.
			status := resp.StatusCode()
			if tolerant && (status == http.StatusBadRequest || status == http.StatusNotFound) {
				config.Debug("[OKTA] skipping %s: status %d", nextURL, status)
				return nil
			}
			return fmt.Errorf("okta API %s returned status %d: %s", nextURL, status, resp.String())
		}

		body := resp.Body()
		items, err := extract(body)
		if err != nil {
			return fmt.Errorf("failed to parse %s response: %w", nextURL, err)
		}
		if len(items) == 0 {
			break
		}
		if err := fn(items); err != nil {
			return err
		}

		nextURL = nextPage(body, resp.Header())
	}

	return nil
}

func flatExtract(b []byte) ([]map[string]interface{}, error) {
	var items []map[string]interface{}
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func envelopeExtract(key string) func([]byte) ([]map[string]interface{}, error) {
	return func(b []byte) ([]map[string]interface{}, error) {
		var env map[string]json.RawMessage
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
		raw, ok := env[key]
		if !ok {
			return nil, nil
		}
		var items []map[string]interface{}
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
}

func groupsURL(filterField string, opts source.ReadOptions) string {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(maxPageSize))
	q.Set("filter", updatedExprOrAll(filterField, opts))
	return "/groups?" + q.Encode()
}

// updatedExpr builds a SCIM filter expression bounding `field` to the interval
// [start, end). Returns an empty string when no interval is set.
func updatedExpr(field string, opts source.ReadOptions) string {
	var parts []string
	if opts.IntervalStart != nil {
		parts = append(parts, fmt.Sprintf(`%s ge "%s"`, field, oktaTime(*opts.IntervalStart)))
	}
	if opts.IntervalEnd != nil {
		parts = append(parts, fmt.Sprintf(`%s lt "%s"`, field, oktaTime(*opts.IntervalEnd)))
	}
	return strings.Join(parts, " and ")
}

// updatedExprOrAll is updatedExpr, but on a full load returns a catch-all
// `field gt "<epoch>"` so the query never falls back to a lossy default list.
func updatedExprOrAll(field string, opts source.ReadOptions) string {
	if expr := updatedExpr(field, opts); expr != "" {
		return expr
	}
	return fmt.Sprintf(`%s gt "%s"`, field, epochTime)
}

func oktaTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
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

var linkNextPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextLink returns the URL from the Link header entry with rel="next". Okta
// sends multiple Link headers (self, next), so all values are inspected.
func nextLink(h http.Header) string {
	for _, value := range h.Values("Link") {
		for _, part := range strings.Split(value, ",") {
			if m := linkNextPattern.FindStringSubmatch(strings.TrimSpace(part)); m != nil {
				return m[1]
			}
		}
	}
	return ""
}
