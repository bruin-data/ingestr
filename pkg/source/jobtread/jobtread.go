package jobtread

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL = "https://api.jobtread.com"

	// JobTread does not publicly document rate limits.
	rateLimit      = 3.0
	rateLimitBurst = 3
	maxPageSize    = 100
	maxPages       = 5000
)

var supportedTables = []string{
	"accounts",
	"jobs",
	"contacts",
	"documents",
	"tasks",
	"cost_codes",
	"cost_types",
	"cost_items",
	"locations",
	"custom_fields",
	"daily_logs",
	"time_entries",
	"files",
	"comments",
	"document_payments",
	"cost_groups",
	"events",
}

type jobTreadCredentials struct {
	grantKey       string
	organizationID string
}

type JobTreadSource struct {
	client *httpclient.Client
	creds  jobTreadCredentials
}

func NewJobTreadSource() *JobTreadSource {
	return &JobTreadSource{}
}

func (s *JobTreadSource) HandlesIncrementality() bool {
	return true
}

func (s *JobTreadSource) Schemes() []string {
	return []string{"jobtread"}
}

func (s *JobTreadSource) Connect(ctx context.Context, uri string) error {
	creds, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.creds = creds
	s.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Content-Type", "application/json"),
	)

	config.Debug("[JOBTREAD] Connected with organization ID: %s", s.creds.organizationID)
	return nil
}

func (s *JobTreadSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *JobTreadSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if !isValidTable(req.Name) {
		return nil, fmt.Errorf("unsupported table: %s (supported: %s)", req.Name, strings.Join(supportedTables, ", "))
	}

	strategy := config.StrategyReplace
	var incrementalKey string

	if req.Name == "events" {
		strategy = config.StrategyMerge
		incrementalKey = "createdAt"
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"id"},
		TableStrategy:       strategy,
		TableIncrementalKey: incrementalKey,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("jobtread source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func (s *JobTreadSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "accounts":
			err = s.readAccounts(ctx, opts, results)
		case "jobs":
			err = s.readJobs(ctx, opts, results)
		case "contacts":
			err = s.readContacts(ctx, opts, results)
		case "documents":
			err = s.readDocuments(ctx, opts, results)
		case "tasks":
			err = s.readTasks(ctx, opts, results)
		case "cost_codes":
			err = s.readCostCodes(ctx, opts, results)
		case "cost_types":
			err = s.readCostTypes(ctx, opts, results)
		case "cost_items":
			err = s.readCostItems(ctx, opts, results)
		case "locations":
			err = s.readLocations(ctx, opts, results)
		case "custom_fields":
			err = s.readCustomFields(ctx, opts, results)
		case "daily_logs":
			err = s.readDailyLogs(ctx, opts, results)
		case "time_entries":
			err = s.readTimeEntries(ctx, opts, results)
		case "files":
			err = s.readFiles(ctx, opts, results)
		case "comments":
			err = s.readComments(ctx, opts, results)
		case "document_payments":
			err = s.readDocumentPayments(ctx, opts, results)
		case "cost_groups":
			err = s.readCostGroups(ctx, opts, results)
		case "events":
			err = s.readEvents(ctx, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

var accountFields = []string{
	"id", "name", "type", "isTaxable", "archivedAt", "createdAt", "qboId",
	"primaryContact.id", "primaryContact.name",
	"primaryLocation.id", "primaryLocation.name", "primaryLocation.address",
}

var documentFields = []string{
	"id", "name", "fullName", "number", "type", "status",
	"price", "priceWithTax", "cost", "tax", "taxRate", "taxName", "taxIsLocked",
	"nonRecoverableTax", "nonRecoverableTaxName",
	"amountPaid", "balance",
	"description", "subject", "dueDate", "dueDays", "issueDate",
	"closedAt", "closeMessage", "signedAt",
	"includeInBudget", "isPaymentApplication", "isSimpleSelection", "allowPartialPayments",
	"requireSignature", "externalId", "sourceId",
	"fromName", "fromAddress", "fromEmailAddress", "fromOrganizationName", "fromPhoneNumber",
	"toName", "toAddress", "toEmailAddress", "toOrganizationName", "toPhoneNumber",
	"jobArea", "jobLocationName", "jobLocationAddress", "jobLocationCity",
	"jobLocationState", "jobLocationCountry", "jobLocationCounty",
	"jobLocationPostalCode", "jobLocationStreet", "jobLocationFormattedAddress",
	"createdAt",
	"account.id", "account.name", "job.id", "job.name",
	"closedByUser.id", "closedByUser.name",
	"signedByUser.id", "signedByUser.name",
	"task.id", "task.name",
}

func (s *JobTreadSource) readAccounts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading accounts")
	return s.paginateAndSend(ctx, "accounts", accountFields, opts, results)
}

func (s *JobTreadSource) readJobs(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading jobs")
	fields := []string{
		"id", "name", "number", "description", "closedOn", "createdAt",
		"lineItemsUpdatedAt", "scheduleIsPublished", "priceType",
		"defaultRetainagePercentage", "useSimpleSelections",
		"areas", "folders", "parameters",
		"specificationsDescription", "specificationsFooter", "specificationsKey",
		"location.id", "location.name", "location.address",
	}
	return s.paginateAndSend(ctx, "jobs", fields, opts, results)
}

func (s *JobTreadSource) readContacts(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading contacts")
	fields := []string{"id", "name", "firstName", "lastName", "title", "createdAt", "account.id", "account.name"}
	return s.paginateAndSend(ctx, "contacts", fields, opts, results)
}

func (s *JobTreadSource) readDocuments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading documents")
	return s.paginateAndSend(ctx, "documents", documentFields, opts, results)
}

func (s *JobTreadSource) readTasks(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading tasks")
	fields := []string{
		"id", "name", "description", "progress", "completed", "started", "unstarted",
		"startDate", "startTime", "endDate", "endTime", "startsAt", "endsAt",
		"baselineStartDate", "baselineStartTime", "baselineEndDate", "baselineEndTime",
		"isToDo", "isGroup", "recurrenceRule", "recurrenceSequenceId", "position", "targetType",
		"createdAt",
		"job.id", "job.name", "taskType.id", "taskType.name",
		"parentTask.id", "parentTask.name",
		"location.id", "location.name",
		"account.id", "account.name",
	}
	return s.paginateAndSend(ctx, "tasks", fields, opts, results)
}

func (s *JobTreadSource) readCostCodes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading cost_codes")
	fields := []string{"id", "name", "number", "fullName", "isActive", "createdAt", "parentCostCode.id", "parentCostCode.name"}
	return s.paginateAndSend(ctx, "costCodes", fields, opts, results)
}

func (s *JobTreadSource) readCostTypes(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading cost_types")
	fields := []string{"id", "name", "isTimeTrackable", "isTaxable", "isActive", "margin", "createdAt"}
	return s.paginateAndSend(ctx, "costTypes", fields, opts, results)
}

func (s *JobTreadSource) readCostItems(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading cost_items")
	fields := []string{
		"id", "name", "description", "quantity", "unitCost", "unitPrice",
		"cost", "price", "priceWithTax", "isTaxable", "isSelected",
		"isEditable", "isSpecification", "hasFinalActualCost", "requireSpecificationApproval",
		"quantityFormula", "unitCostFormula", "unitPriceFormula",
		"allowanceType", "globalId", "jobArea", "position", "createdAt",
		"costCode.id", "costCode.name", "costType.id", "costType.name",
		"job.id", "job.name",
		"document.id", "document.name",
		"costGroup.id", "costGroup.name",
		"unit.id", "unit.name",
	}
	return s.paginateAndSend(ctx, "costItems", fields, opts, results)
}

func (s *JobTreadSource) readLocations(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading locations")
	fields := []string{"id", "name", "address", "city", "state", "street", "country", "county", "postalCode", "formattedAddress", "latitude", "longitude", "timeZone", "taxRate", "customTaxRate", "createdAt", "account.id", "account.name", "contact.id", "contact.name"}
	return s.paginateAndSend(ctx, "locations", fields, opts, results)
}

func (s *JobTreadSource) readCustomFields(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading custom_fields")
	fields := []string{"id", "name", "type", "targetType", "position", "defaultValue", "options", "minValuesRequired", "maxValuesAllowed", "createdAt"}
	return s.paginateAndSend(ctx, "customFields", fields, opts, results)
}

func (s *JobTreadSource) readDailyLogs(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading daily_logs")
	fields := []string{"id", "date", "notes", "maxTemperature", "minTemperature", "rainfallAmount", "snowfallAmount", "weatherCondition", "windSpeed", "createdAt", "job.id", "job.name", "job.location.id", "job.location.name", "job.location.address", "user.id", "user.name"}
	return s.paginateAndSend(ctx, "dailyLogs", fields, opts, results)
}

func (s *JobTreadSource) readTimeEntries(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading time_entries")
	fields := []string{"id", "startedAt", "endedAt", "notes", "type", "isApproved", "hourlyRate", "minutes", "cost", "createdAt", "user.id", "user.name", "job.id", "job.name", "costItem.id", "costItem.name"}
	return s.paginateAndSend(ctx, "timeEntries", fields, opts, results)
}

func (s *JobTreadSource) readFiles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading files")
	fields := []string{
		"id", "name", "url", "size", "folder", "type", "description", "createdAt",
		"createdByUser.id", "createdByUser.name",
		"job.id", "job.name",
		"account.id", "account.name",
		"contact.id", "contact.name",
		"document.id", "document.name",
		"location.id", "location.name",
		"task.id", "task.name",
		"dailyLog.id",
	}
	return s.paginateAndSend(ctx, "files", fields, opts, results)
}

func (s *JobTreadSource) readComments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading comments")
	fields := []string{
		"id", "message", "name", "isPinned", "isVisibleToAll",
		"isVisibleToCustomerRoles", "isVisibleToInternalRoles", "isVisibleToVendorRoles",
		"isFromEmail", "targetType", "createdAt",
		"createdByUser.id", "createdByUser.name",
		"parentComment.id",
		"job.id", "job.name",
		"account.id", "account.name",
		"document.id", "document.name",
		"task.id", "task.name",
		"dailyLog.id", "timeEntry.id", "file.id",
	}
	return s.paginateAndSend(ctx, "comments", fields, opts, results)
}

func (s *JobTreadSource) readDocumentPayments(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading document_payments")
	fields := []string{"id", "amount", "createdAt", "document.id", "document.name", "document.type", "payment.id", "payment.amount", "payment.createdAt"}
	return s.paginateAndSend(ctx, "documentPayments", fields, opts, results)
}

func (s *JobTreadSource) readCostGroups(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading cost_groups")
	fields := []string{
		"id", "name", "description", "quantity", "quantityFormula",
		"isSelected", "isSimpleSelection", "position",
		"minSelectionsRequired", "maxSelectionsAllowed",
		"createdAt",
		"parentCostGroup.id", "parentCostGroup.name",
		"job.id", "job.name",
		"document.id", "document.name",
		"unit.id", "unit.name",
	}
	return s.paginateAndSend(ctx, "costGroups", fields, opts, results)
}

func (s *JobTreadSource) readEvents(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[JOBTREAD] reading events")
	fields := []string{"id", "type", "createdAt", "createdByUser.id", "createdByUser.name"}

	var where interface{}
	switch {
	case opts.IntervalStart != nil && opts.IntervalEnd != nil:
		where = map[string]interface{}{
			"and": []interface{}{
				[]interface{}{"createdAt", ">=", opts.IntervalStart.Format(time.RFC3339)},
				[]interface{}{"createdAt", "<", opts.IntervalEnd.Format(time.RFC3339)},
			},
		}
	case opts.IntervalStart != nil:
		where = []interface{}{"createdAt", ">=", opts.IntervalStart.Format(time.RFC3339)}
	case opts.IntervalEnd != nil:
		where = []interface{}{"createdAt", "<", opts.IntervalEnd.Format(time.RFC3339)}
	}

	return s.paginateAndSendWithWhere(ctx, "events", fields, where, opts, results)
}

func (s *JobTreadSource) paginateAndSend(ctx context.Context, paveKey string, fields []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	return s.paginateAndSendWithWhere(ctx, paveKey, fields, nil, opts, results)
}

func (s *JobTreadSource) paginateAndSendWithWhere(ctx context.Context, paveKey string, fields []string, where interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	totalProcessed := 0
	var nextPage *string

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		query := s.buildQuery(paveKey, fields, where, nextPage)

		resp, err := s.client.R(ctx).SetBody(map[string]interface{}{"query": query}).Post("/pave")
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", paveKey, err)
		}
		if !resp.IsSuccess() {
			return fmt.Errorf("jobtread %s returned status %d: %s", paveKey, resp.StatusCode(), resp.String())
		}

		var paveResp map[string]json.RawMessage
		if err := jsonUseNumber(resp.Body(), &paveResp); err != nil {
			return fmt.Errorf("failed to parse %s response: %w", paveKey, err)
		}

		if errRaw, ok := paveResp["errors"]; ok {
			var errs []struct{ Message string }
			if err := json.Unmarshal(errRaw, &errs); err == nil && len(errs) > 0 {
				return fmt.Errorf("jobtread %s query error: %s", paveKey, errs[0].Message)
			}
		}

		nodes, np, err := extractNodes(paveResp, paveKey)
		if err != nil {
			return fmt.Errorf("failed to extract %s nodes: %w", paveKey, err)
		}

		if len(nodes) == 0 {
			break
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(nodes, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to build arrow record for %s: %w", paveKey, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case results <- source.RecordBatchResult{Batch: record}:
		}

		totalProcessed += len(nodes)

		if np == "" {
			break
		}
		nextPage = &np
	}

	config.Debug("[JOBTREAD] finished reading %s: %d total records", paveKey, totalProcessed)
	return nil
}

func (s *JobTreadSource) buildQuery(paveKey string, fields []string, where interface{}, nextPage *string) map[string]interface{} {
	connectionArgs := map[string]interface{}{
		"size": maxPageSize,
	}

	if nextPage != nil {
		connectionArgs["page"] = *nextPage
	}

	if where != nil {
		connectionArgs["where"] = where
	}

	connectionArgs["sortBy"] = []interface{}{
		map[string]interface{}{"field": "createdAt"},
	}

	fieldSelection := buildFieldSelection(fields)

	connection := map[string]interface{}{
		"$":        connectionArgs,
		"nextPage": map[string]interface{}{},
		"nodes":    fieldSelection,
	}

	orgArgs := map[string]interface{}{}
	if s.creds.organizationID != "" {
		orgArgs["id"] = s.creds.organizationID
	}

	return map[string]interface{}{
		"$": map[string]interface{}{
			"grantKey": s.creds.grantKey,
		},
		"organization": map[string]interface{}{
			"$":     orgArgs,
			paveKey: connection,
		},
	}
}

func extractNodes(data map[string]json.RawMessage, paveKey string) ([]map[string]interface{}, string, error) {
	orgRaw, ok := data["organization"]
	if !ok {
		return nil, "", fmt.Errorf("response missing 'organization' field")
	}

	var org map[string]json.RawMessage
	if err := json.Unmarshal(orgRaw, &org); err != nil {
		return nil, "", fmt.Errorf("failed to parse organization: %w", err)
	}

	connRaw, ok := org[paveKey]
	if !ok {
		return nil, "", fmt.Errorf("response missing '%s' field on organization", paveKey)
	}

	var conn struct {
		NextPage *string                  `json:"nextPage"`
		Nodes    []map[string]interface{} `json:"nodes"`
	}
	if err := jsonUseNumber(connRaw, &conn); err != nil {
		return nil, "", fmt.Errorf("failed to parse %s connection: %w", paveKey, err)
	}

	np := ""
	if conn.NextPage != nil {
		np = *conn.NextPage
	}

	return conn.Nodes, np, nil
}

func buildFieldSelection(fields []string) map[string]interface{} {
	root := make(map[string]interface{}, len(fields))
	for _, f := range fields {
		parts := strings.SplitN(f, ".", 2)
		if len(parts) == 1 {
			root[f] = map[string]interface{}{}
			continue
		}
		parent, rest := parts[0], parts[1]
		sub, ok := root[parent].(map[string]interface{})
		if !ok {
			sub = make(map[string]interface{})
			root[parent] = sub
		}
		merged := buildFieldSelection([]string{rest})
		for k, v := range merged {
			sub[k] = v
		}
	}
	return root
}

func isValidTable(table string) bool {
	for _, t := range supportedTables {
		if t == table {
			return true
		}
	}
	return false
}

func parseURI(uri string) (jobTreadCredentials, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return jobTreadCredentials{}, fmt.Errorf("invalid jobtread URI: %w", err)
	}

	if parsed.Scheme != "jobtread" {
		return jobTreadCredentials{}, fmt.Errorf("invalid jobtread URI: must start with jobtread://")
	}

	params := parsed.Query()

	grantKey := params.Get("grant_key")
	if grantKey == "" {
		return jobTreadCredentials{}, fmt.Errorf("grant_key is required in jobtread URI: jobtread://?grant_key=<key>")
	}

	orgID := params.Get("organization_id")
	if orgID == "" {
		return jobTreadCredentials{}, fmt.Errorf("organization_id is required in jobtread URI: jobtread://?grant_key=<key>&organization_id=<id>")
	}

	return jobTreadCredentials{
		grantKey:       grantKey,
		organizationID: orgID,
	}, nil
}

func jsonUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}
