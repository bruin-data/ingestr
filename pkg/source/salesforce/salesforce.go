package salesforce

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/simpleforce/simpleforce"
)

const (
	defaultAPIVersion = "59.0"
)

type salesforceSource struct {
	client      *simpleforce.Client
	httpClient  *httpclient.Client
	instanceURL string
	sessionID   string
	useBulkAPI  bool
	sfUser      string
	sfPassword  string
	sfToken     string
	sfUrl       string
}

func NewSalesforceSource() *salesforceSource {
	return &salesforceSource{}
}

func (s *salesforceSource) Schemes() []string {
	return []string{"salesforce"}
}

func (s *salesforceSource) Connect(ctx context.Context, uri string) error {
	sfUser, sfPassword, sfToken, sfDomain, err := parseSalesforceURI(uri)
	if err != nil {
		return err
	}
	s.sfUser = sfUser
	s.sfPassword = sfPassword
	s.sfToken = sfToken
	s.sfUrl = fmt.Sprintf("https://%s.salesforce.com/", sfDomain)

	s.client = simpleforce.NewClient(s.sfUrl, simpleforce.DefaultClientID, defaultAPIVersion)

	if s.client == nil {
		return fmt.Errorf("failed to create Salesforce client")
	}
	err = s.client.LoginPassword(s.sfUser, s.sfPassword, s.sfToken)
	if err != nil {
		return fmt.Errorf("failed to login to Salesforce: %w", err)
	}

	s.sessionID = s.client.GetSid()
	s.instanceURL = s.client.GetLoc()
	s.useBulkAPI = true
	s.httpClient = httpclient.New(
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithAuth(httpclient.NewAPIKeyAuth("X-SFDC-Session", s.sessionID, true)),
		httpclient.WithRateLimiter(5, 1),
	)

	config.Debug("[SALESFORCE] Connected successfully")
	return nil
}

func parseSalesforceURI(uri string) (sfUser, sfPassword, sfToken, sfUrl string, err error) {
	if !strings.HasPrefix(uri, "salesforce://") {
		return "", "", "", "", fmt.Errorf("invalid salesforce URI: must start with salesforce://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse salesforce URI: %w", err)
	}

	params := parsed.Query()
	sfUser = params.Get("username")
	sfPassword = params.Get("password")
	sfToken = params.Get("token")
	sfDomain := params.Get("domain")

	if sfUser == "" {
		return "", "", "", "", fmt.Errorf("sfUser is required for Salesforce")
	}
	if sfPassword == "" {
		return "", "", "", "", fmt.Errorf("sfPassword is required for Salesforce")
	}
	if sfToken == "" {
		return "", "", "", "", fmt.Errorf("sfToken is required for Salesforce")
	}
	if sfDomain == "" {
		return "", "", "", "", fmt.Errorf("sfUrl is required for Salesforce")
	}

	return sfUser, sfPassword, sfToken, sfDomain, nil
}

func (s *salesforceSource) Close(ctx context.Context) error {
	s.client = nil
	if s.httpClient != nil {
		return s.httpClient.Close()
	}
	return nil
}

func (s *salesforceSource) HandlesIncrementality() bool {
	return true
}

type tableMeta struct {
	sobject        string
	strategy       config.IncrementalStrategy
	pk             []string
	replicationKey string
}

var salesforceTableMeta = map[string]tableMeta{
	"user":                     {"User", config.StrategyReplace, nil, ""},
	"user_history":             {"UserHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"user_role":                {"UserRole", config.StrategyReplace, nil, ""},
	"opportunity":              {"Opportunity", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"opportunity_history":      {"OpportunityHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"opportunity_line_item":    {"OpportunityLineItem", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"opportunity_contact_role": {"OpportunityContactRole", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"account":                  {"Account", config.StrategyMerge, []string{"Id"}, "LastModifiedDate"},
	"account_history":          {"AccountHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"case":                     {"Case", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"case_history":             {"CaseHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"contact":                  {"Contact", config.StrategyReplace, []string{"Id"}, ""},
	"contact_history":          {"ContactHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"lead":                     {"Lead", config.StrategyReplace, []string{"Id"}, ""},
	"lead_history":             {"LeadHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"campaign":                 {"Campaign", config.StrategyReplace, []string{"Id"}, ""},
	"campaign_history":         {"CampaignHistory", config.StrategyAppend, []string{"Id"}, "CreatedDate"},
	"campaign_member":          {"CampaignMember", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"product":                  {"Product2", config.StrategyReplace, []string{"Id"}, ""},
	"pricebook":                {"Pricebook2", config.StrategyReplace, []string{"Id"}, ""},
	"pricebook_entry":          {"PricebookEntry", config.StrategyReplace, []string{"Id"}, ""},
	"task":                     {"Task", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
	"event":                    {"Event", config.StrategyMerge, []string{"Id"}, "SystemModstamp"},
}

func (s *salesforceSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	if tableName == "" {
		return nil, fmt.Errorf("table name is required for salesforce source")
	}

	meta, known := salesforceTableMeta[tableName]
	if !known && !strings.HasPrefix(tableName, "custom:") {
		supported := slices.Sorted(maps.Keys(salesforceTableMeta))
		return nil, fmt.Errorf("unsupported table: %s (supported: %s, or use 'custom:<object_name>' for custom objects)", req.Name, strings.Join(supported, ", "))
	}

	strategy := config.StrategyReplace
	replicationKey := ""
	pk := req.PrimaryKeys
	if known {
		strategy = meta.strategy
		if len(meta.pk) > 0 {
			pk = meta.pk
		}
		replicationKey = meta.replicationKey
	}

	// Allow callers to override the built-in incremental key (e.g. via
	// --incremental-key). When provided, switch to an incremental merge keyed
	// on that column instead of the default. The column is validated against
	// the object's datetime fields at read time.
	if req.IncrementalKey != "" {
		replicationKey = req.IncrementalKey
		
		// History tables are append only so merge strategy is not applicable
		if strings.HasSuffix(tableName, "_history") {
			strategy = config.StrategyAppend
		} else {
			strategy = config.StrategyMerge
		}

		// Merge requires primary keys. Every Salesforce object exposes an "Id"
		// field, so fall back to it when neither the table metadata nor the
		// caller supplied explicit PKs.
		if len(pk) == 0 {
			pk = []string{"Id"}
		}
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    pk,
		TableIncrementalKey: replicationKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("salesforce source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, replicationKey, opts)
		},
	}, nil
}

func (s *salesforceSource) read(ctx context.Context, tableName, replicationKey string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var sobject string
		if meta, ok := salesforceTableMeta[tableName]; ok {
			sobject = meta.sobject
		} else {
			sobject = strings.TrimPrefix(tableName, "custom:")
		}

		// Filter incrementally only when a replication key is in play; otherwise
		// fetch the full object.
		var lastState interface{}
		if replicationKey != "" {
			lastState = opts.IntervalStart
		}

		if err := s.getRecords(ctx, sobject, lastState, replicationKey, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *salesforceSource) getRecords(ctx context.Context, sobject string, lastState interface{}, replicationKey string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	obj := s.client.SObject(sobject)
	meta := obj.Describe()
	if meta == nil {
		return fmt.Errorf("salesforce describe failed for %s", sobject)
	}
	fieldsRaw, ok := (*meta)["fields"].([]interface{})
	if !ok {
		return fmt.Errorf("salesforce describe did not return fields for %s", sobject)
	}

	compoundFields := make(map[string]bool)
	for _, f := range fieldsRaw {
		field, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		if compoundName, ok := field["compoundFieldName"].(string); ok && compoundName != "" && compoundName != "Name" {
			compoundFields[compoundName] = true
		}
	}

	dateFields := make(map[string]bool)
	for _, f := range fieldsRaw {
		field, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		fieldType, _ := field["type"].(string)
		fieldName, _ := field["name"].(string)
		if fieldType == "datetime" && fieldName != "" {
			dateFields[fieldName] = true
		}
	}

	var fields []string
	for _, f := range fieldsRaw {
		field, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		fieldName, _ := field["name"].(string)
		if fieldName != "" && !compoundFields[fieldName] {
			fields = append(fields, fieldName)
		}
	}

	// Incremental loading filters and orders by a datetime column, so the
	// replication key must resolve to one of the object's datetime fields.
	// Normalise to the field's canonical casing for the SOQL query.
	if replicationKey != "" {
		canonicalKey := ""
		for name := range dateFields {
			if strings.EqualFold(name, replicationKey) {
				canonicalKey = name
				break
			}
		}
		if canonicalKey == "" {
			return fmt.Errorf("incremental key %q is not a datetime field on %s; salesforce incremental loading requires a datetime column such as SystemModstamp or LastModifiedDate", replicationKey, sobject)
		}
		replicationKey = canonicalKey
	}

	predicate := ""
	orderBy := ""
	if replicationKey != "" {
		soqlDate := soqlTimestamp(lastState)
		if soqlDate != "" {
			predicate = fmt.Sprintf("WHERE %s > %s", replicationKey, soqlDate)
		}
		orderBy = fmt.Sprintf("ORDER BY %s ASC", replicationKey)
	}

	query := fmt.Sprintf("SELECT %s FROM %s %s %s", strings.Join(fields, ", "), sobject, predicate, orderBy)
	config.Debug("[SALESFORCE] Query: %s", query)

	if s.useBulkAPI {
		err := s.getRecordsBulk(ctx, sobject, query, dateFields, opts, results)
		if err == nil {
			fmt.Printf("\n[SALESFORCE] Fetched %s using Bulk API\n", sobject)
			return nil
		}
		s.useBulkAPI = false
		config.Debug("[SALESFORCE] Bulk API failed for %s: %v", sobject, err)
	}

	err := s.getRecordsREST(ctx, query, dateFields, opts, results, sobject)
	if err != nil {
		return err
	}
	fmt.Printf("\n[SALESFORCE] Fetched %s using REST API, since your account does not have Bulk API enabled\n", sobject)
	return nil
}

func (s *salesforceSource) processRecords(records []map[string]interface{}, dateFields map[string]bool) {
	for i, record := range records {
		rec := make(map[string]interface{}, len(record))
		for k, v := range record {
			if k == "attributes" || strings.HasPrefix(k, "__") {
				continue
			}
			if dateFields[k] && v != nil {
				switch val := v.(type) {
				case float64:
					rec[k] = time.UnixMilli(int64(val))
				case string:
					if t, err := dateparse.ParseAny(val); err == nil {
						rec[k] = t
					} else {
						rec[k] = v
					}
				default:
					rec[k] = v
				}
			} else {
				rec[k] = v
			}
		}
		records[i] = rec
	}
}

func (s *salesforceSource) getRecordsBulk(ctx context.Context, sobject, query string, dateFields map[string]bool, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	jobID, err := s.bulkCreateJob(ctx, sobject)
	if err != nil {
		return fmt.Errorf("failed to create bulk job: %w", err)
	}
	config.Debug("[SALESFORCE] Created bulk job: %s", jobID)

	batchID, err := s.bulkAddQueryBatch(ctx, jobID, query)
	if err != nil {
		return fmt.Errorf("failed to add batch: %w", err)
	}
	config.Debug("[SALESFORCE] Added batch: %s", batchID)

	if err := s.bulkCloseJob(ctx, jobID); err != nil {
		return fmt.Errorf("failed to close bulk job: %w", err)
	}

	if err := s.bulkPollBatch(ctx, jobID, batchID); err != nil {
		return fmt.Errorf("bulk query batch failed: %w", err)
	}

	nRecords := 0
	err = s.bulkGetQueryResults(ctx, jobID, batchID, func(records []map[string]interface{}) error {
		if len(records) == 0 {
			return nil
		}
		s.processRecords(records, dateFields)
		arrowRec, err := arrowconv.ItemsToArrowRecordWithSchema(records, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert batch to arrow: %w", err)
		}
		results <- source.RecordBatchResult{Batch: arrowRec}
		nRecords += len(records)
		return nil
	})
	if err != nil {
		return err
	}

	config.Debug("[SALESFORCE] Fetched %d records from %s via Bulk API", nRecords, sobject)
	return nil
}

func (s *salesforceSource) getRecordsREST(ctx context.Context, query string, dateFields map[string]bool, opts source.ReadOptions, results chan<- source.RecordBatchResult, sobject string) error {
	nRecords := 0

	result, err := s.client.Query(query)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	for {
		batch := make([]map[string]interface{}, 0, len(result.Records))
		for _, record := range result.Records {
			batch = append(batch, record)
		}

		if len(batch) > 0 {
			s.processRecords(batch, dateFields)
			arrowRec, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert batch to arrow: %w", err)
			}
			results <- source.RecordBatchResult{Batch: arrowRec}
			nRecords += len(batch)
		}

		if result.Done || result.NextRecordsURL == "" {
			break
		}

		result, err = s.client.Query(result.NextRecordsURL)
		if err != nil {
			return fmt.Errorf("failed to fetch next page: %w", err)
		}
	}

	config.Debug("[SALESFORCE] Fetched %d records from %s via REST API", nRecords, sobject)
	return nil
}

func (s *salesforceSource) bulkCreateJob(ctx context.Context, sobject string) (string, error) {
	endpoint := fmt.Sprintf("%s/services/async/%s/job", s.instanceURL, defaultAPIVersion)

	var result struct {
		ID string `json:"id"`
	}
	resp, err := s.httpClient.R(ctx).
		SetBody(map[string]string{
			"operation":   "query",
			"object":      sobject,
			"contentType": "JSON",
		}).
		SetResult(&result).
		Post(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to create job: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("create job returned %d: %s", resp.StatusCode(), resp.String())
	}
	return result.ID, nil
}

func (s *salesforceSource) bulkAddQueryBatch(ctx context.Context, jobID, soqlQuery string) (string, error) {
	endpoint := fmt.Sprintf("%s/services/async/%s/job/%s/batch", s.instanceURL, defaultAPIVersion, jobID)

	var result struct {
		ID string `json:"id"`
	}
	resp, err := s.httpClient.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(soqlQuery).
		SetResult(&result).
		Post(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to add batch: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("add batch returned %d: %s", resp.StatusCode(), resp.String())
	}
	return result.ID, nil
}

func (s *salesforceSource) bulkCloseJob(ctx context.Context, jobID string) error {
	endpoint := fmt.Sprintf("%s/services/async/%s/job/%s", s.instanceURL, defaultAPIVersion, jobID)

	resp, err := s.httpClient.R(ctx).
		SetBody(map[string]string{"state": "Closed"}).
		Post(endpoint)
	if err != nil {
		return fmt.Errorf("failed to close job: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("close job returned %d: %s", resp.StatusCode(), resp.String())
	}
	return nil
}

func (s *salesforceSource) bulkPollBatch(ctx context.Context, jobID, batchID string) error {
	endpoint := fmt.Sprintf("%s/services/async/%s/job/%s/batch/%s", s.instanceURL, defaultAPIVersion, jobID, batchID)
	pollInterval := 2 * time.Second
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("bulk batch %s polling timed out after 10 minutes", batchID)
		}
		var result struct {
			State string `json:"state"`
		}
		resp, err := s.httpClient.R(ctx).SetResult(&result).Get(endpoint)
		if err != nil {
			return fmt.Errorf("failed to poll batch: %w", err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("poll batch returned %d: %s", resp.StatusCode(), resp.String())
		}

		config.Debug("[SALESFORCE] Bulk batch %s state: %s", batchID, result.State)

		switch result.State {
		case "Completed":
			return nil
		case "Failed":
			return fmt.Errorf("bulk batch %s failed", batchID)
		case "NotProcessed":
			return fmt.Errorf("bulk batch %s was not processed", batchID)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}

		if pollInterval < 30*time.Second {
			pollInterval = min(pollInterval*2, 30*time.Second)
		}
	}
}

func (s *salesforceSource) bulkGetQueryResults(ctx context.Context, jobID, batchID string, fn func([]map[string]interface{}) error) error {
	baseEndpoint := fmt.Sprintf("%s/services/async/%s/job/%s/batch/%s/result", s.instanceURL, defaultAPIVersion, jobID, batchID)

	var resultIDs []string
	resp, err := s.httpClient.R(ctx).SetResult(&resultIDs).Get(baseEndpoint)
	if err != nil {
		return fmt.Errorf("failed to get result IDs: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("get result IDs returned %d: %s", resp.StatusCode(), resp.String())
	}

	config.Debug("[SALESFORCE] Got %d result sets", len(resultIDs))

	for _, resultID := range resultIDs {
		var records []map[string]interface{}
		resp, err := s.httpClient.R(ctx).SetResult(&records).Get(fmt.Sprintf("%s/%s", baseEndpoint, resultID))
		if err != nil {
			return fmt.Errorf("failed to fetch result %s: %w", resultID, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("fetch result returned %d: %s", resp.StatusCode(), resp.String())
		}

		if err := fn(records); err != nil {
			return err
		}
	}

	return nil
}

func soqlTimestamp(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case *time.Time:
		if val == nil || val.IsZero() {
			return ""
		}
		return val.UTC().Format(time.RFC3339)
	default:
		return ""
	}
}
