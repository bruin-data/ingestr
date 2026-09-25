package salesforce

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/internal/salesforceauth"
	"github.com/bruin-data/ingestr/pkg/destination"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	// The sObject Collections endpoints accept up to 200 records per call.
	batchLimit = 200

	// Salesforce meters by daily quota rather than per-second, but a burst of
	// concurrent collection calls can still trip the concurrent-request limit.
	rateLimit      = 10.0
	rateLimitBurst = 5

	retryCount   = 5
	retryWait    = 2 * time.Second
	retryMaxWait = 1 * time.Minute

	defaultParallelism = 4

	// recordIDField is Salesforce's server-assigned record id. Matching on it
	// routes writes to the plain update endpoint rather than an upsert.
	recordIDField = "Id"

	// soqlMaxQueryLen bounds a generated "IN" query, URL-encoded, so it stays
	// within the URL length Salesforce accepts for GET /query.
	soqlMaxQueryLen = 7000
)

type SalesforceDestination struct {
	client      *httpclient.Client
	instanceURL string
	apiVersion  string
	loadMethod  string

	// describes caches sObject describe results by lowercased object name.
	mu        sync.Mutex
	describes map[string]*sobjectDescribe
}

func NewSalesforceDestination() *SalesforceDestination {
	return &SalesforceDestination{describes: map[string]*sobjectDescribe{}}
}

func (d *SalesforceDestination) Schemes() []string {
	return []string{"salesforce"}
}

func parseURI(uri string) (salesforceauth.Config, string, error) {
	cfg, err := salesforceauth.ParseURI(uri)
	if err != nil {
		return cfg, "", err
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return cfg, "", fmt.Errorf("failed to parse salesforce URI: %w", err)
	}
	method := parsed.Query().Get("load_method")
	switch method {
	case "":
		method = loadMethodBulk
	case loadMethodREST, loadMethodBulk:
	default:
		return cfg, "", fmt.Errorf("invalid salesforce load_method %q: use %s or %s", method, loadMethodREST, loadMethodBulk)
	}
	return cfg, method, nil
}

func (d *SalesforceDestination) Connect(ctx context.Context, uri string) error {
	cfg, loadMethod, err := parseURI(uri)
	if err != nil {
		return err
	}
	d.loadMethod = loadMethod

	client, err := salesforceauth.Login(ctx, cfg)
	if err != nil {
		return err
	}

	d.instanceURL = strings.TrimRight(client.GetLoc(), "/")
	d.apiVersion = cfg.APIVersion
	d.client = httpclient.New(
		httpclient.WithBaseURL(d.instanceURL),
		httpclient.WithTimeout(2*time.Minute),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		// Upsert/update/delete are idempotent, so the default 429+5xx retry is
		// safe; create opts out per request (see postCollection).
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithAuth(httpclient.NewBearerAuth(client.GetSid())),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Content-Type", "application/json"),
		httpclient.WithHeader("Accept", "application/json"),
	)

	if err := d.checkAPI(ctx); err != nil {
		return err
	}
	config.Debug("[SALESFORCE DEST] Connected to %s (API %s, load_method=%s)", d.instanceURL, d.apiVersion, d.loadMethod)
	return nil
}

// checkAPI fails at connect time on an unusable session or API version, which
// would otherwise surface later as a misleading "no sObject named" error.
func (d *SalesforceDestination) checkAPI(ctx context.Context) error {
	resp, err := d.client.R(ctx).Get(d.dataPath("/"))
	if err != nil {
		return fmt.Errorf("salesforce: failed to reach %s: %w", d.instanceURL, err)
	}
	switch resp.StatusCode() {
	case 200:
		return nil
	case 401:
		return fmt.Errorf("salesforce: authentication failed: %w", parseAPIError(resp))
	case 404:
		return fmt.Errorf("salesforce: API version %s is not available on this org; set api_version to a supported version (default %s)", d.apiVersion, salesforceauth.DefaultAPIVersion)
	default:
		return fmt.Errorf("salesforce: API check failed: %w", parseAPIError(resp))
	}
}

func (d *SalesforceDestination) Close(_ context.Context) error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

func (d *SalesforceDestination) dataPath(suffix string) string {
	return fmt.Sprintf("/services/data/v%s%s", d.apiVersion, suffix)
}

// tableParams are the record-shaping options carried on the --dest-table string,
// e.g. "Contact?external_id=External_Id__c".
type tableParams struct {
	// ExternalID names the Salesforce field records are matched on. It is
	// literally the externalIdFieldName in the upsert URL, so merge and replace
	// require a field marked External ID; update and delete match via SOQL and
	// accept any field.
	ExternalID string `mapstructure:"external_id"`
}

// shaper turns a source row into a Salesforce sObject Collections record.
type shaper struct {
	sobject string
	// idField is the Salesforce field to match records on. Empty creates records;
	// "Id" matches by record id; any other field is an External ID upsert key.
	idField string
	// idColumn is the source column supplying the idField value.
	idColumn string
	exclude  map[string]bool
	// rejectMode is fail_fast | fail | skip (empty = fail).
	rejectMode string
	// archive deletes the records (to the Recycle Bin) instead of writing fields.
	archive bool
	// updateOnly (strategy=update) updates existing records and never creates;
	// createOnly (strategy=append) always creates and never matches.
	updateOnly bool
	createOnly bool
	// mirror (strategy=replace) upserts every source row, then deletes records
	// whose idField value was not in the source (seen; concurrency-safe).
	mirror bool
	seen   *sync.Map
	// writtenIDs holds the record ids Salesforce returned for every row this run
	// wrote. The mirror sweep keeps these by id, so a record the run just created
	// is never deleted even when its stored match value can't be correlated back.
	writtenIDs *sync.Map
	// sawSource records whether the source delivered any non-empty batch. A mirror
	// whose source produced 0 rows must not delete every record.
	sawSource atomic.Bool
	// writeNulls sends JSON null for null cells to clear the field.
	writeNulls bool
	// bulk buffers records into Bulk API 2.0 jobs under load_method=bulk; nil
	// sends them through REST collections.
	bulk *bulkJobs
	// lookupFields maps a lowercased relationship name to its lookup field, so a
	// null dotted cell clears the link (AccountId: null) instead of sending a
	// nested null Salesforce rejects. Nil when the describe was unavailable.
	lookupFields map[string]string
	// numericKey is set when idField is a number field, whose values Salesforce
	// returns as JSON numbers (42.0) and SOQL compares without quotes.
	numericKey bool
	// idKey is set when idField holds record ids, compared in 18-char form.
	idKey bool
	// defaultIDColumn is set when idColumn defaulted to the match field's name,
	// which sources may return in any case (id, ID).
	defaultIDColumn bool
	// parentTypes maps a lowercased polymorphic relationship to the object an
	// untyped column points to (owner -> User).
	parentTypes map[string]string
	// labelColumns (append) are the --primary-key columns; they never match and
	// only name a rejected row.
	labelColumns []string
	// keyPrefix is the sObject's 3-char id prefix; the collections DELETE isn't
	// scoped to an sObject, so direct-Id deletes check it.
	keyPrefix string
}

// matchValue renders a match value for correlation; numeric keys compare in
// canonical form so a source 42 or 42.00 meets Salesforce's 42.0.
func (s *shaper) matchValue(v interface{}) string {
	str := stringValue(v)
	if s.numericKey {
		if n, ok := canonicalNumber(str); ok {
			return n
		}
	}
	if s.idKey {
		return recordID18(str)
	}
	return str
}

// recordID18 expands a 15-char case-sensitive record id to its 18-char form.
func recordID18(id string) string {
	if len(id) != 15 {
		return id
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"
	suffix := make([]byte, 3)
	for chunk := range 3 {
		bits := 0
		for i := range 5 {
			c := id[chunk*5+i]
			switch {
			case c >= 'A' && c <= 'Z':
				bits |= 1 << i
			case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			default:
				return id
			}
		}
		suffix[chunk] = alphabet[bits]
	}
	return id + string(suffix)
}

// isRecordID reports whether s has the shape SOQL accepts for an id: 15 or 18
// alphanumeric characters.
func isRecordID(s string) bool {
	if len(s) != 15 && len(s) != 18 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func canonicalNumber(s string) (string, bool) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok {
		return "", false
	}
	if r.IsInt() {
		return r.Num().String(), true
	}
	return strings.TrimRight(r.FloatString(18), "0"), true
}

// skip reports whether rejected rows are collected and the run still succeeds.
func (s *shaper) skip() bool { return s.rejectMode == string(config.RejectSkip) }

// failFast reports whether the run aborts on the first rejected row.
func (s *shaper) failFast() bool { return s.rejectMode == string(config.RejectFailFast) }

// matchesRecords reports whether writes carry a value to match existing records.
func (s *shaper) matchesRecords() bool { return s.idField != "" }

// byRecordID reports whether the match field is Salesforce's own record id.
func (s *shaper) byRecordID() bool { return strings.EqualFold(s.idField, recordIDField) }

// upsert matches on an External ID field via the collections upsert endpoint.
func (s *shaper) upsert() bool {
	return s.matchesRecords() && !s.byRecordID() && !s.updateOnly && !s.archive
}

// resolvesKeys reports whether match values must be looked up via SOQL before the
// write. Update and delete can only address records by Id, so any other match
// field is resolved first; upsert needs no lookup (Salesforce matches natively).
func (s *shaper) resolvesKeys() bool {
	return (s.updateOnly || s.archive) && s.matchesRecords() && !s.byRecordID()
}

// parseShaper builds the shaper from --incremental-strategy, the dest-table
// (sObject + external_id), --primary-key, and the run flags.
func parseShaper(table, strategy string, primaryKeys []string, rejectMode string, writeNulls bool) (*shaper, error) {
	var p tableParams
	path, _, err := tablespec.Parse(table, &p)
	if err != nil {
		return nil, err
	}
	// Tolerate an optional schema qualifier ("salesforce.Contact" -> "Contact").
	sobject := strings.TrimSpace(path)
	if i := strings.LastIndex(sobject, "."); i >= 0 {
		sobject = sobject[i+1:]
	}
	if sobject == "" {
		return nil, fmt.Errorf("salesforce dest-table must be an sObject API name, e.g. \"Contact\", \"Account\", \"MyObject__c\"")
	}

	// ingestr's own decoration columns are never sent as Salesforce fields, and
	// neither is Id: it is server-assigned and rejected on create and upsert.
	exclude := map[string]bool{
		naming.IngestrLoadedAtColumn: true,
		naming.IngestrRunIDColumn:    true,
	}

	if strategy == string(config.StrategyDelete) {
		return parseDeleteShaper(sobject, p, primaryKeys, rejectMode)
	}

	if strategy == string(config.StrategyAppend) {
		if p.ExternalID != "" {
			return nil, fmt.Errorf("salesforce: append always creates records and never matches, so external_id=%s would be ignored; use --incremental-strategy merge to upsert on it, or remove external_id", p.ExternalID)
		}
		return &shaper{
			sobject:      sobject,
			exclude:      exclude,
			rejectMode:   rejectMode,
			createOnly:   true,
			writeNulls:   writeNulls,
			labelColumns: primaryKeys,
		}, nil
	}

	updateOnly := strategy == string(config.StrategyUpdate)
	mirror := strategy == string(config.StrategyReplace)
	idField := p.ExternalID
	if idField == "" && updateOnly {
		idField = recordIDField
	}
	if idField == "" {
		return nil, fmt.Errorf("salesforce: %s needs a match field — set external_id=<External ID field> on the dest-table", strategy)
	}
	// Salesforce assigns record ids and cannot create a record at a supplied Id,
	// so merge and replace (which both create unmatched source rows) can't key on
	// Id: an unmatched id could never be created, leaving an incomplete write that
	// --reject-mode=skip would report as success.
	if !updateOnly && strings.EqualFold(idField, recordIDField) {
		return nil, fmt.Errorf("salesforce: %s cannot match on external_id=%s — Salesforce cannot create a record at a supplied Id; use --incremental-strategy update to update existing records by Id, or set external_id=<External ID field> to upsert", strategy, recordIDField)
	}

	// Only update defaults the source column; merge and replace require it.
	defaultColumn := ""
	if updateOnly {
		defaultColumn = idField
	}
	idColumn, err := sourceIDColumn(primaryKeys, defaultColumn)
	if err != nil {
		return nil, err
	}

	sh := &shaper{
		sobject:         sobject,
		idField:         idField,
		idColumn:        idColumn,
		defaultIDColumn: len(primaryKeys) == 0,
		exclude:         exclude,
		rejectMode:      rejectMode,
		updateOnly:      updateOnly,
		mirror:          mirror,
		writeNulls:      writeNulls,
	}
	if mirror {
		sh.seen = &sync.Map{}
		sh.writtenIDs = &sync.Map{}
	}
	return sh, nil
}

// parseDeleteShaper builds a shaper that deletes records, matched by the id
// column (record ids, or another field resolved to ids when external_id is set).
func parseDeleteShaper(sobject string, p tableParams, primaryKeys []string, rejectMode string) (*shaper, error) {
	idField := p.ExternalID
	if idField == "" {
		idField = recordIDField
	}
	idColumn, err := sourceIDColumn(primaryKeys, idField)
	if err != nil {
		return nil, err
	}
	return &shaper{
		sobject:         sobject,
		idField:         idField,
		idColumn:        idColumn,
		defaultIDColumn: len(primaryKeys) == 0,
		rejectMode:      rejectMode,
		archive:         true,
	}, nil
}

// sourceIDColumn returns the source column holding the match value from a single
// --primary-key, falling back to defaultColumn (update/delete default it to the
// match field's name). An empty default means the strategy requires one.
func sourceIDColumn(primaryKeys []string, defaultColumn string) (string, error) {
	switch len(primaryKeys) {
	case 1:
		return primaryKeys[0], nil
	case 0:
		if defaultColumn != "" {
			return defaultColumn, nil
		}
		return "", fmt.Errorf("salesforce: this strategy needs a source key column — pass --primary-key")
	default:
		return "", fmt.Errorf("salesforce: cannot match on a composite primary key [%s]; pass a single --primary-key", strings.Join(primaryKeys, ", "))
	}
}

func (s *shaper) isIDColumn(name string) bool {
	return name == s.idColumn || (s.defaultIDColumn && strings.EqualFold(name, s.idColumn))
}

// validateColumns fails fast when the source is missing the match column. A
// defaulted match column is found case-insensitively and aliased in colIndex.
func (s *shaper) validateColumns(colIndex map[string]int) error {
	if !s.matchesRecords() {
		return nil
	}
	if _, ok := colIndex[s.idColumn]; ok {
		return nil
	}
	names := make([]string, 0, len(colIndex))
	for name := range colIndex {
		names = append(names, name)
	}
	if s.defaultIDColumn {
		for _, name := range names {
			if strings.EqualFold(name, s.idColumn) {
				colIndex[s.idColumn] = colIndex[name]
				return nil
			}
		}
	}
	slices.Sort(names)
	return fmt.Errorf("salesforce: id column %q not found in source (available: %s); pass --primary-key to name the match column", s.idColumn, strings.Join(names, ", "))
}

// sfRecord is one record in a sObject Collections request body. Fields live at
// the top level alongside the "attributes" type marker, so it is built as a map.
type sfRecord map[string]interface{}

// key names the record for a reject line, e.g. "External_Id__c=A-1".
func (r sfRecord) key(idField string) string {
	if idField == "" {
		return ""
	}
	v, ok := r[idField]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%s=%v", idField, v)
}

// labelKey names a create-only row by its --primary-key values, e.g.
// "Ext_Id__c=C-1"; empty when any value is missing.
func labelKey(columns []string, value func(column string) string) string {
	parts := make([]string, 0, len(columns))
	for _, c := range columns {
		v := value(c)
		if v == "" {
			return ""
		}
		parts = append(parts, c+"="+v)
	}
	return strings.Join(parts, ", ")
}

// shapeRow builds the record body and endpoint action for one row. A row with no
// match value is created rather than skipped, except under update-only.
func (s *shaper) shapeRow(record arrow.RecordBatch, colIndex map[string]int, row int) (sfRecord, string, bool) {
	out := sfRecord{"attributes": map[string]string{"type": s.sobject}}
	var nullRels []string
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		if s.isIDColumn(name) || s.exclude[name] || strings.EqualFold(name, recordIDField) {
			continue
		}
		col := record.Column(i)
		if v, ok := fieldValue(col, row); ok {
			setField(out, name, v)
		} else if s.writeNulls {
			if _, rel, _, dotted := relationshipColumn(name); dotted {
				nullRels = append(nullRels, rel)
				continue
			}
			out[name] = nil
		}
	}
	s.clearNullRelationships(out, nullRels)
	s.applyDefaultParentTypes(out)

	if s.createOnly || !s.matchesRecords() {
		return out, "create", true
	}

	idVal, ok := fieldValue(record.Column(colIndex[s.idColumn]), row)
	idStr := stringValue(idVal)
	if !ok || idStr == "" {
		if s.updateOnly {
			// Update-only can't create; a row without a match value is skipped.
			return nil, "", false
		}
		return out, "create", true
	}

	if s.byRecordID() {
		out[recordIDField] = idStr
		return out, "update", true
	}
	if s.updateOnly {
		// The match value is resolved to record ids before the write, so the row
		// is addressed by Id; the match field itself is never written back (it may
		// well be read-only, e.g. a formula).
		return out, "update", true
	}
	// Upsert keys on the External ID field, which must be present in the body.
	out[s.idField] = idVal
	return out, "upsert", true
}

// setField places a value on the record, expanding a dotted column name into the
// nested relationship object Salesforce uses to match a lookup by external id
// (e.g. "Account.Ext_Id__c" -> {"Account": {"Ext_Id__c": ...}}). A typed column
// ("Who.Contact.Ext_Id__c") also names the parent's object, which a polymorphic
// lookup needs.
func setField(rec sfRecord, name string, value interface{}) {
	typ, rel, field, ok := relationshipColumn(name)
	if !ok {
		rec[name] = value
		return
	}
	nested, _ := rec[rel].(map[string]interface{})
	if nested == nil {
		nested = map[string]interface{}{}
		rec[rel] = nested
	}
	nested[field] = value
	if typ != "" {
		nested["attributes"] = map[string]string{"type": typ}
	}
}

// relationshipColumn splits a dotted column into the relationship name, the
// optional parent object and the parent's lookup field: "Account.Ext_Id__c" or,
// for a polymorphic lookup, "Who.Contact.Ext_Id__c".
func relationshipColumn(name string) (typ, rel, field string, ok bool) {
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 2:
		rel, field = parts[0], parts[1]
	case 3:
		rel, typ, field = parts[0], parts[1], parts[2]
		if typ == "" {
			return "", "", "", false
		}
	default:
		return "", "", "", false
	}
	return typ, rel, field, rel != "" && field != ""
}

// typedRelationshipGroups maps each relationship that more than one typed
// column addresses (Who.Contact.x and Who.Lead.x) to those columns' indexes.
func typedRelationshipGroups(record arrow.RecordBatch) map[string][]int {
	byRel := map[string][]int{}
	for i := 0; i < int(record.NumCols()); i++ {
		if typ, rel, _, ok := relationshipColumn(record.ColumnName(i)); ok && typ != "" {
			byRel[rel] = append(byRel[rel], i)
		}
	}
	for rel, cols := range byRel {
		if len(cols) < 2 {
			delete(byRel, rel)
		}
	}
	return byRel
}

// polymorphicConflict names the relationship a row sets through more than one
// typed column; a lookup points to one record, so the row can't be written.
func polymorphicConflict(groups map[string][]int, record arrow.RecordBatch, row int) string {
	for rel, cols := range groups {
		set := 0
		for _, i := range cols {
			if !record.Column(i).IsNull(row) {
				set++
			}
		}
		if set > 1 {
			return rel
		}
	}
	return ""
}

// rejectConflict reports a row that sets one polymorphic lookup through several
// typed columns, or aborts under fail_fast.
func (s *shaper) rejectConflict(rel string, record arrow.RecordBatch, colIndex map[string]int, row int, rejects *rejectionLog) error {
	msg := fmt.Sprintf("more than one %s column is set on this row; %s points to one record, so fill only one of them", rel, rel)
	if s.failFast() {
		return fmt.Errorf("salesforce: %s", msg)
	}
	cell := func(column string) string {
		i, ok := colIndex[column]
		if !ok {
			return ""
		}
		v, _ := fieldValue(record.Column(i), row)
		return stringValue(v)
	}
	id := ""
	if s.idColumn != "" {
		if v := cell(s.idColumn); v != "" {
			id = s.idField + "=" + v
		}
	} else {
		id = labelKey(s.labelColumns, cell)
	}
	rejects.add([]rejection{{code: polymorphicConflictCode, message: msg, identifier: id}})
	return nil
}

// applyDefaultParentTypes names the parent object of an untyped polymorphic
// lookup that has a default (Owner is a User unless the column says otherwise).
func (s *shaper) applyDefaultParentTypes(out sfRecord) {
	if len(s.parentTypes) == 0 {
		return
	}
	for k, v := range out {
		nested, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if _, typed := nested["attributes"]; typed {
			continue
		}
		if typ := s.parentTypes[strings.ToLower(k)]; typ != "" {
			nested["attributes"] = map[string]string{"type": typ}
		}
	}
}

// clearNullRelationships unlinks each relationship whose dotted cell was null by
// nulling its lookup field. Salesforce rejects a nested null, so without the
// lookup mapping the cell is omitted and the link left as is. A relationship the
// row also sets elsewhere (a non-null dotted cell or the lookup column) wins.
func (s *shaper) clearNullRelationships(out sfRecord, rels []string) {
	for _, rel := range rels {
		if _, set := out[rel]; set {
			continue
		}
		field, ok := s.lookupFields[strings.ToLower(rel)]
		if !ok {
			continue
		}
		if _, set := out[field]; !set {
			out[field] = nil
		}
	}
}

// primaryKeysFor resolves the run's primary keys, falling back to the schema's
// when WriteOptions carries none.
func primaryKeysFor(explicit []string, sch *schema.TableSchema) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if sch != nil {
		return sch.PrimaryKeys
	}
	return nil
}

// reportWithWriteErr surfaces the records rejected before a hard failure
// alongside that failure, so a late 5xx doesn't hide the reject list.
func reportWithWriteErr(sh *shaper, rejects *rejectionLog, writeErr error) error {
	if rejErr := reportRejections(sh, rejects); rejErr != nil {
		return errors.Join(writeErr, rejErr)
	}
	return writeErr
}

func (d *SalesforceDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// Errors return without draining: executeReverseETL cancels the source read
	// first, so a failed run doesn't wait for the whole extract.
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), opts.RejectMode, opts.WriteNulls)
	if err != nil {
		return err
	}
	if err := d.useBulk(sh); err != nil {
		return err
	}
	d.loadFieldMetadata(ctx, sh)

	var totalRows int64
	var skipped atomic.Int64
	var rejects rejectionLog
	var writeErr error
	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			writeErr = result.Err
			break
		}
		record := result.Batch
		if record == nil {
			continue
		}
		if record.NumRows() == 0 {
			record.Release()
			continue
		}

		rows, err := d.writeBatch(ctx, sh, record, &skipped, &rejects)
		record.Release()
		if err != nil {
			writeErr = err
			break
		}
		totalRows += rows
	}
	if writeErr != nil {
		return reportWithWriteErr(sh, &rejects, writeErr)
	}

	if err := d.flushBulk(ctx, sh, &rejects); err != nil {
		return reportWithWriteErr(sh, &rejects, err)
	}

	warnSkipped(&skipped, sh)
	config.Debug("[SALESFORCE DEST] Wrote %d %s record(s)", totalRows, sh.sobject)
	return d.finalizeAndReport(ctx, sh, &rejects)
}

func (d *SalesforceDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// Bulk jobs run in parallel server-side, so one client-side writer suffices.
	if d.loadMethod == loadMethodBulk {
		return d.Write(ctx, records, opts)
	}
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), opts.RejectMode, opts.WriteNulls)
	if err != nil {
		return err
	}
	d.loadFieldMetadata(ctx, sh)

	// Salesforce caps effective write concurrency at defaultParallelism to stay
	// under its concurrent-request limit. opts.Parallelism carries the framework
	// default when the user set no --destination-parallelism, so capping it is
	// routine and logged at debug level rather than warned about.
	parallelism := opts.Parallelism
	if parallelism <= 0 || parallelism > defaultParallelism {
		if parallelism > defaultParallelism {
			config.Debug("[SALESFORCE DEST] capping write parallelism from %d to %d for API limits", parallelism, defaultParallelism)
		}
		parallelism = defaultParallelism
	}

	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var skipped atomic.Int64
	var rejects rejectionLog
	errs := make(chan error, parallelism)

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for result := range records {
				// Stop promptly once another worker has failed; executeReverseETL
				// cancels the source read and drains the rest.
				if ctx.Err() != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					return
				}
				if result.Err != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					select {
					case errs <- result.Err:
					default:
					}
					cancel()
					return
				}
				record := result.Batch
				if record == nil {
					continue
				}
				if record.NumRows() == 0 {
					record.Release()
					continue
				}
				_, err := d.writeBatch(ctx, sh, record, &skipped, &rejects)
				record.Release()
				if err != nil {
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

	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return reportWithWriteErr(sh, &rejects, err)
	}
	// Caller cancellation leaves no worker error; surface it so executeReverseETL
	// runs its drain and the run isn't reported as successful.
	if err := parentCtx.Err(); err != nil {
		return reportWithWriteErr(sh, &rejects, err)
	}

	warnSkipped(&skipped, sh)
	return d.finalizeAndReport(ctx, sh, &rejects)
}

func warnSkipped(skipped *atomic.Int64, sh *shaper) {
	n := skipped.Load()
	if n == 0 {
		return
	}
	output.Warnf("Warning: salesforce skipped %d %s record(s) with a missing %s value\n", n, sh.sobject, sh.idColumn)
}

// writeBatch shapes a record batch and sends it in chunks of batchLimit.
func (d *SalesforceDestination) writeBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	// Reached only with a non-empty batch, so this marks that the source actually
	// produced data — the mirror sweep relies on it to tell an empty extract apart.
	sh.sawSource.Store(true)
	colIndex := make(map[string]int, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		colIndex[record.ColumnName(i)] = i
	}

	if err := sh.validateColumns(colIndex); err != nil {
		return 0, err
	}

	if sh.archive {
		return d.writeDeleteBatch(ctx, sh, record, colIndex, skipped, rejects)
	}
	if sh.resolvesKeys() {
		return d.writeResolvedUpdateBatch(ctx, sh, record, colIndex, skipped, rejects)
	}

	rows := int(record.NumRows())
	if sh.mirror && sh.seen != nil {
		idCol := record.Column(colIndex[sh.idColumn])
		for row := 0; row < rows; row++ {
			if v, ok := fieldValue(idCol, row); ok {
				if s := sh.matchValue(v); s != "" {
					sh.seen.Store(matchKey(s), struct{}{})
				}
			}
		}
	}

	var written int64
	// Rows are grouped by endpoint action so a single request stays homogeneous;
	// upsert mode may yield both "upsert" (key present) and "create" (key absent).
	batches := make(map[string][]sfRecord, 2)
	flush := func(action string) error {
		batch := batches[action]
		if len(batch) == 0 {
			return nil
		}
		if err := d.sendRecords(ctx, sh, batch, action, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batches[action] = batch[:0]
		return nil
	}

	conflicts := typedRelationshipGroups(record)
	for row := 0; row < rows; row++ {
		if rel := polymorphicConflict(conflicts, record, row); rel != "" {
			if err := sh.rejectConflict(rel, record, colIndex, row, rejects); err != nil {
				return written, err
			}
			continue
		}
		item, action, ok := sh.shapeRow(record, colIndex, row)
		if !ok {
			skipped.Add(1)
			continue
		}
		batches[action] = append(batches[action], item)
		if len(batches[action]) == batchLimit {
			if err := flush(action); err != nil {
				return written, err
			}
		}
	}
	for action := range batches {
		if err := flush(action); err != nil {
			return written, err
		}
	}
	return written, nil
}

// writeResolvedUpdateBatch updates records matched on a field other than Id: SOQL
// resolves each value to every matching record id, then the rows are updated by
// id. A value that matches no record is a not-found reject.
func (d *SalesforceDestination) writeResolvedUpdateBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	idIdx := colIndex[sh.idColumn]
	resolve, err := d.resolveKeysToIDs(ctx, sh, sh.columnValues(record.Column(idIdx)))
	if err != nil {
		return 0, err
	}

	rows := int(record.NumRows())
	batch := make([]sfRecord, 0, min(rows, batchLimit))
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := d.sendRecords(ctx, sh, batch, "update", rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	conflicts := typedRelationshipGroups(record)
	for row := 0; row < rows; row++ {
		if rel := polymorphicConflict(conflicts, record, row); rel != "" {
			if err := sh.rejectConflict(rel, record, colIndex, row, rejects); err != nil {
				return written, err
			}
			continue
		}
		item, _, ok := sh.shapeRow(record, colIndex, row)
		if !ok {
			skipped.Add(1)
			continue
		}
		val, _ := fieldValue(record.Column(idIdx), row)
		key := sh.matchValue(val)
		if key == "" {
			skipped.Add(1)
			continue
		}
		ids := resolve.lookup(key)
		if len(ids) == 0 {
			if sh.failFast() {
				return written, fmt.Errorf("salesforce: no %s found with %s=%q", sh.sobject, sh.idField, key)
			}
			rejects.add([]rejection{{
				code:       notFoundCode,
				message:    fmt.Sprintf("no %s found with %s=%q", sh.sobject, sh.idField, key),
				identifier: sh.idField + "=" + key,
			}})
			continue
		}
		for _, id := range ids {
			// A resolved value may name several records; each gets its own copy so
			// the per-record result still maps back to one input.
			rec := sfRecord{"attributes": map[string]string{"type": sh.sobject}}
			for k, v := range item {
				if k == "attributes" {
					continue
				}
				rec[k] = v
			}
			rec[recordIDField] = id
			batch = append(batch, rec)
			if len(batch) == batchLimit {
				if err := flush(); err != nil {
					return written, err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, nil
}

// writeDeleteBatch deletes records in chunks of batchLimit. Ids come from the id
// column directly, or are resolved from another field via SOQL first.
func (d *SalesforceDestination) writeDeleteBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	idIdx := colIndex[sh.idColumn]

	var resolve resolveMap
	if sh.resolvesKeys() {
		var err error
		resolve, err = d.resolveKeysToIDs(ctx, sh, sh.columnValues(record.Column(idIdx)))
		if err != nil {
			return 0, err
		}
	}

	rows := int(record.NumRows())
	batch := make([]string, 0, min(rows, batchLimit))
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := d.sendDelete(ctx, sh, batch, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for row := 0; row < rows; row++ {
		v, ok := fieldValue(record.Column(idIdx), row)
		val := sh.matchValue(v)
		if !ok || val == "" {
			skipped.Add(1)
			continue
		}
		ids := []string{val}
		// ids are comma-joined on the wire, so a malformed one must not reach it.
		if !resolve.initialized() && (!isRecordID(val) || !strings.HasPrefix(val, sh.keyPrefix)) {
			if sh.failFast() {
				return written, fmt.Errorf("salesforce: %q is not a valid %s record id to delete", val, sh.sobject)
			}
			rejects.add([]rejection{{code: "MALFORMED_ID", message: fmt.Sprintf("%q is not a valid %s record id", val, sh.sobject), identifier: recordIDField + "=" + val}})
			continue
		}
		if resolve.initialized() {
			found := resolve.lookup(val)
			if len(found) == 0 {
				if sh.failFast() {
					return written, fmt.Errorf("salesforce: no %s found with %s=%q to delete", sh.sobject, sh.idField, val)
				}
				rejects.add([]rejection{{
					code:       notFoundCode,
					message:    fmt.Sprintf("no %s found with %s=%q to delete", sh.sobject, sh.idField, val),
					identifier: sh.idField + "=" + val,
				}})
				continue
			}
			ids = found
		}
		for _, id := range ids {
			batch = append(batch, id)
			if len(batch) == batchLimit {
				if err := flush(); err != nil {
					return written, err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, nil
}

// finalizeAndReport runs the mirror delete sweep (if any) and reports rejected
// records. In fail mode a run with any rejected row skips the destructive sweep —
// a failed run must not also delete records the user still has to reconcile.
func (d *SalesforceDestination) finalizeAndReport(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
	if sh.mirror && !sh.skip() && rejects.len() > 0 {
		return reportRejections(sh, rejects)
	}
	if err := d.finalizeMirror(ctx, sh, rejects); err != nil {
		if rejErr := reportRejections(sh, rejects); rejErr != nil {
			return errors.Join(err, rejErr)
		}
		return err
	}
	return reportRejections(sh, rejects)
}

// finalizeMirror completes a replace: queries the sObject and deletes any record
// whose idField value was not in the source. No-op otherwise.
func (d *SalesforceDestination) finalizeMirror(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
	if !sh.mirror || sh.seen == nil {
		return nil
	}
	// A source that produced 0 rows must not delete the entire sObject — that turns
	// a transient empty extract into a mass delete. Skip the sweep and warn.
	if !sh.sawSource.Load() {
		output.Warnf("Warning: salesforce replace (mirror) of %s: source produced 0 rows; skipping the delete sweep so an empty extract does not remove every record. Use --incremental-strategy delete to remove records intentionally.\n", sh.sobject)
		return nil
	}

	stale, err := d.listStaleIDs(ctx, sh)
	if err != nil {
		return fmt.Errorf("salesforce mirror: failed to list existing %s records: %w", sh.sobject, err)
	}
	if len(stale) == 0 {
		return nil
	}
	config.Debug("[SALESFORCE DEST] mirror deleting %d stale %s record(s)", len(stale), sh.sobject)

	for start := 0; start < len(stale); start += batchLimit {
		end := min(start+batchLimit, len(stale))
		if err := d.sendDelete(ctx, sh, stale[start:end], rejects); err != nil {
			return err
		}
	}
	return d.flushBulk(ctx, sh, rejects)
}

// listStaleIDs queries the sObject and returns the Id of every record whose
// idField value is not in the source.
func (d *SalesforceDestination) listStaleIDs(ctx context.Context, sh *shaper) ([]string, error) {
	soql := fmt.Sprintf("SELECT Id, %s FROM %s", sh.idField, sh.sobject)
	var stale []string
	err := d.querySOQL(ctx, soql, func(rec map[string]interface{}) {
		id := stringValue(rec["Id"])
		if id == "" {
			return
		}
		// A record this run just wrote (e.g. a keyless row with no match value)
		// must survive its own run's sweep.
		if sh.writtenIDs != nil {
			if _, ok := sh.writtenIDs.Load(id); ok {
				return
			}
		}
		// Any record whose match value is not in the source is deleted — including
		// records with no value for the field (their empty key is never in seen).
		// Compare through matchKey: Salesforce comparisons are case-insensitive, so
		// a raw compare could miss a record we just wrote.
		if _, ok := sh.seen.Load(matchKey(sh.matchValue(recordValue(rec, sh.idField)))); !ok {
			stale = append(stale, id)
		}
	})
	if err != nil {
		return nil, err
	}
	return stale, nil
}

// recordResult is one entry of a sObject Collections response, positionally
// aligned with the request's records.
type recordResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Errors  []struct {
		StatusCode string   `json:"statusCode"`
		Message    string   `json:"message"`
		Fields     []string `json:"fields"`
	} `json:"errors"`
}

// notFoundCode is the reject code used for a match value that resolved to no
// record. Salesforce's own equivalents are reported verbatim from its response.
const notFoundCode = "NOT_FOUND"

const entityDeletedCode = "ENTITY_IS_DELETED"

// apiError reports a whole-request failure. Salesforce returns HTTP 200 with
// per-record results whenever allOrNone is false, so any other status is
// structural (bad object or field, expired session, governor limit) and must
// abort the run rather than be tolerated as per-record rejects.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("status %d: %s: %s", e.status, e.code, e.message)
	}
	return fmt.Sprintf("status %d: %s", e.status, e.message)
}

// parseAPIError turns a non-2xx response into a structural error. Salesforce
// reports these as a list of {errorCode, message} objects.
func parseAPIError(resp *httpclient.Response) *apiError {
	var list []struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(resp.Body(), &list); err == nil && len(list) > 0 {
		return &apiError{status: resp.StatusCode(), code: list[0].ErrorCode, message: list[0].Message}
	}
	return &apiError{status: resp.StatusCode(), message: resp.String()}
}

// postCollection sends one chunk to the sObject Collections endpoint and returns
// the per-record results.
func (d *SalesforceDestination) postCollection(ctx context.Context, sh *shaper, records []sfRecord, action string) ([]recordResult, error) {
	body := map[string]interface{}{"allOrNone": false, "records": records}
	req := d.client.R(ctx).SetBody(body)

	var resp *httpclient.Response
	var err error
	switch action {
	case "create":
		// Create is the only non-idempotent action; retrying it after a server-side
		// commit would duplicate records, so only retry on a 429 (never processed).
		resp, err = req.SetRetryOnRateLimitOnly().Post(d.dataPath("/composite/sobjects"))
	case "update":
		resp, err = req.Patch(d.dataPath("/composite/sobjects"))
	case "upsert":
		resp, err = req.Patch(d.dataPath(fmt.Sprintf("/composite/sobjects/%s/%s",
			url.PathEscape(sh.sobject), url.PathEscape(sh.idField))))
	default:
		return nil, fmt.Errorf("salesforce: unknown write action %q", action)
	}
	if err != nil {
		return nil, fmt.Errorf("salesforce %s request failed: %w", action, err)
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("salesforce %s %s failed: %w", action, sh.sobject, parseAPIError(resp))
	}

	var results []recordResult
	if err := json.Unmarshal(resp.Body(), &results); err != nil {
		return nil, fmt.Errorf("salesforce %s %s: failed to parse response: %w", action, sh.sobject, err)
	}
	return results, nil
}

// sendRecords posts one chunk and turns its per-record failures into rejections.
func (d *SalesforceDestination) sendRecords(ctx context.Context, sh *shaper, records []sfRecord, action string, rejects *rejectionLog) error {
	if sh.bulk != nil {
		return d.bufferBulk(ctx, sh, action, records, rejects)
	}
	keys := make([]string, len(records))
	for i, r := range records {
		keys[i] = r.key(sh.idField)
		if keys[i] == "" {
			keys[i] = r.key(recordIDField)
		}
		if keys[i] == "" && len(sh.labelColumns) > 0 {
			keys[i] = labelKey(sh.labelColumns, func(c string) string {
				if v, ok := r[c]; ok && v != nil {
					return fmt.Sprint(v)
				}
				return ""
			})
		}
	}
	return withLockRetry(ctx, sh, len(records),
		func(pending []int) ([]recordResult, error) {
			return d.postCollection(ctx, sh, pick(records, pending), action)
		},
		func(pending []int, results []recordResult) error {
			return d.collectResults(sh, results, pick(keys, pending), action, rejects)
		})
}

// lockErrorCode is Salesforce's transient row-lock failure, typically parallel
// writes to children of the same parent. The record was not written, so
// re-sending it is safe even for create.
const lockErrorCode = "UNABLE_TO_LOCK_ROW"

// polymorphicConflictCode tags a row that sets one polymorphic lookup through
// more than one typed column.
const polymorphicConflictCode = "POLYMORPHIC_CONFLICT"

const lockRetries = 4

// lockBackoff is the wait before re-sending locked records; a var so tests
// don't sleep.
var lockBackoff = func(attempt int) time.Duration { return time.Second << attempt }

// withLockRetry sends the inputs (by index) and hands every final result to
// collect. Records rejected with UNABLE_TO_LOCK_ROW are re-sent on their own with
// backoff; after lockRetries attempts they are collected like any reject.
func withLockRetry(ctx context.Context, sh *shaper, n int, send func(pending []int) ([]recordResult, error), collect func(pending []int, results []recordResult) error) error {
	pending := make([]int, n)
	for i := range pending {
		pending[i] = i
	}
	for attempt := 0; ; attempt++ {
		results, err := send(pending)
		if err != nil {
			return err
		}
		if len(results) != len(pending) {
			return fmt.Errorf("salesforce %s: sent %d record(s) but got %d result(s)", sh.sobject, len(pending), len(results))
		}
		var done, retry []int
		var doneResults []recordResult
		for i, res := range results {
			if attempt < lockRetries && isLockError(res) {
				retry = append(retry, pending[i])
				continue
			}
			done = append(done, pending[i])
			doneResults = append(doneResults, res)
		}
		if err := collect(done, doneResults); err != nil {
			return err
		}
		if len(retry) == 0 {
			return nil
		}
		config.Debug("[SALESFORCE DEST] retrying %d %s record(s) after %s (attempt %d)", len(retry), sh.sobject, lockErrorCode, attempt+1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockBackoff(attempt)):
		}
		pending = retry
	}
}

func isLockError(res recordResult) bool {
	return !res.Success && len(res.Errors) > 0 && res.Errors[0].StatusCode == lockErrorCode
}

func pick[T any](items []T, idx []int) []T {
	out := make([]T, len(idx))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}

// collectResults records the ids Salesforce wrote and converts each failed entry
// into a rejection, keyed positionally to its input record.
func (d *SalesforceDestination) collectResults(sh *shaper, results []recordResult, keys []string, action string, rejects *rejectionLog) error {
	var failed []rejection
	for i, res := range results {
		// A retried delete whose first attempt committed sees its records as deleted.
		if action == "delete" && !res.Success && len(res.Errors) > 0 && res.Errors[0].StatusCode == entityDeletedCode {
			continue
		}
		if res.Success {
			// A mirror must not delete records it just wrote; remember every id
			// Salesforce returned so the sweep keeps them regardless of whether
			// their stored match value correlates back through `seen`.
			if sh.writtenIDs != nil && res.ID != "" {
				sh.writtenIDs.Store(res.ID, struct{}{})
			}
			continue
		}
		key := ""
		if i < len(keys) {
			key = keys[i]
		}
		rej := rejection{identifier: key, message: "record rejected without an error message"}
		if len(res.Errors) > 0 {
			rej.code = res.Errors[0].StatusCode
			rej.message = res.Errors[0].Message
			rej.fields = res.Errors[0].Fields
		}
		if sh.failFast() {
			return fmt.Errorf("salesforce %s %s rejected record %s: (%s) %s", action, sh.sobject, key, rej.code, rej.message)
		}
		failed = append(failed, rej)
	}
	if len(failed) == 0 {
		return nil
	}
	output.Warnf("Warning: salesforce rejected %d of %d %s record(s) in this batch; first error: %s\n", len(failed), len(results), sh.sobject, failed[0].message)
	rejects.add(failed)
	return orgLimitError(sh, failed)
}

// orgLimitErrors are reported per record but hit every row alike, so the run
// aborts regardless of --reject-mode instead of rejecting the rest one by one.
var orgLimitErrors = map[string]string{
	"STORAGE_LIMIT_EXCEEDED": "the org is out of data storage; free some up (deleted records count until the Recycle Bin is emptied) and re-run",
}

func orgLimitError(sh *shaper, failed []rejection) error {
	for _, rej := range failed {
		if hint, ok := orgLimitErrors[rej.code]; ok {
			return fmt.Errorf("salesforce %s: aborting on %s — %s", sh.sobject, rej.code, hint)
		}
	}
	return nil
}

// sendDelete removes one chunk of record ids. Salesforce moves them to the
// Recycle Bin, from which they can be restored.
func (d *SalesforceDestination) sendDelete(ctx context.Context, sh *shaper, ids []string, rejects *rejectionLog) error {
	if sh.bulk != nil {
		records := make([]sfRecord, len(ids))
		for i, id := range ids {
			records[i] = sfRecord{recordIDField: id}
		}
		return d.bufferBulk(ctx, sh, "delete", records, rejects)
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = recordIDField + "=" + id
	}
	return withLockRetry(ctx, sh, len(ids),
		func(pending []int) ([]recordResult, error) {
			return d.deleteCollection(ctx, sh, pick(ids, pending))
		},
		func(pending []int, results []recordResult) error {
			return d.collectResults(sh, results, pick(keys, pending), "delete", rejects)
		})
}

func (d *SalesforceDestination) deleteCollection(ctx context.Context, sh *shaper, ids []string) ([]recordResult, error) {
	resp, err := d.client.R(ctx).
		SetQueryParam("ids", strings.Join(ids, ",")).
		SetQueryParam("allOrNone", "false").
		Delete(d.dataPath("/composite/sobjects"))
	if err != nil {
		return nil, fmt.Errorf("salesforce delete request failed: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("salesforce delete %s failed: %w", sh.sobject, parseAPIError(resp))
	}
	var results []recordResult
	if err := json.Unmarshal(resp.Body(), &results); err != nil {
		return nil, fmt.Errorf("salesforce delete %s: failed to parse response: %w", sh.sobject, err)
	}
	return results, nil
}

// columnValues collects the non-empty string values of a column for key
// resolution.
func (s *shaper) columnValues(arr arrow.Array) []string {
	values := make([]string, 0, arr.Len())
	for i := 0; i < arr.Len(); i++ {
		if v, ok := fieldValue(arr, i); ok {
			if s := s.matchValue(v); s != "" {
				values = append(values, s)
			}
		}
	}
	return values
}

// matchKey normalizes a match value for resolve-map lookups. SOQL string
// comparison is case-insensitive, so a value returned by a query may differ in
// case from the source value that found it.
func matchKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// resolveMap maps a match value to the record id(s) it resolved to. Each entry is
// stored under the exact value and its folded (matchKey) form; lookup prefers the
// exact match so two case-distinct values keep their own records, and falls back
// to the folded key because SOQL matches case-insensitively.
type resolveMap struct {
	exact  map[string][]string
	folded map[string][]string
}

func newResolveMap() resolveMap {
	return resolveMap{exact: map[string][]string{}, folded: map[string][]string{}}
}

func (m resolveMap) initialized() bool { return m.exact != nil }

func (m resolveMap) add(value, id string) {
	if !slices.Contains(m.exact[value], id) {
		m.exact[value] = append(m.exact[value], id)
	}
	fk := matchKey(value)
	if !slices.Contains(m.folded[fk], id) {
		m.folded[fk] = append(m.folded[fk], id)
	}
}

// lookup returns the ids value resolved to: an exact hit first, else the folded key.
func (m resolveMap) lookup(value string) []string {
	if ids, ok := m.exact[value]; ok {
		return ids
	}
	return m.folded[matchKey(value)]
}

// resolveKeysToIDs maps match values to record ids with a chunked SOQL IN query.
func (d *SalesforceDestination) resolveKeysToIDs(ctx context.Context, sh *shaper, values []string) (resolveMap, error) {
	sobject, field := sh.sobject, sh.idField
	seen := make(map[string]bool, len(values))
	uniq := make([]string, 0, len(values))
	for _, v := range values {
		// A non-numeric value can't match a number field, and must not reach the
		// unquoted literal below.
		if sh.numericKey {
			if _, ok := canonicalNumber(v); !ok {
				continue
			}
		}
		// SOQL fails the whole query on a malformed id, so such a value is left
		// unresolved and its row rejected as not found.
		if sh.idKey && !isRecordID(v) {
			continue
		}
		if v != "" && !seen[v] {
			seen[v] = true
			uniq = append(uniq, v)
		}
	}

	out := newResolveMap()
	prefix := fmt.Sprintf("SELECT Id, %s FROM %s WHERE %s IN (", field, sobject, field)
	for _, chunk := range soqlChunks(uniq, len(url.QueryEscape(prefix+")"))) {
		quoted := make([]string, len(chunk))
		for i, v := range chunk {
			if sh.numericKey {
				quoted[i] = v
				continue
			}
			quoted[i] = "'" + escapeSOQL(v) + "'"
		}
		soql := prefix + strings.Join(quoted, ",") + ")"
		err := d.querySOQL(ctx, soql, func(rec map[string]interface{}) {
			id := stringValue(rec["Id"])
			if id == "" {
				return
			}
			out.add(sh.matchValue(recordValue(rec, field)), id)
		})
		if err != nil {
			return resolveMap{}, err
		}
	}
	return out, nil
}

// soqlChunks groups values so each generated IN clause stays under the query
// length Salesforce accepts, and never exceeds batchLimit values.
func soqlChunks(values []string, overhead int) [][]string {
	var chunks [][]string
	var current []string
	length := overhead
	for _, v := range values {
		cost := len(url.QueryEscape("'"+escapeSOQL(v)+"'")) + 3 // plus an encoded comma
		if len(current) > 0 && (length+cost > soqlMaxQueryLen || len(current) >= batchLimit) {
			chunks = append(chunks, current)
			current = nil
			length = overhead
		}
		current = append(current, v)
		length += cost
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// escapeSOQL escapes the characters that would otherwise terminate or alter a
// quoted SOQL string literal.
func escapeSOQL(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return r.Replace(v)
}

// recordValue reads a field from a query result, which is keyed by the field's
// canonical API name whatever case the query used.
func recordValue(rec map[string]interface{}, field string) interface{} {
	if v, ok := rec[field]; ok {
		return v
	}
	for k, v := range rec {
		if strings.EqualFold(k, field) {
			return v
		}
	}
	return nil
}

// querySOQL runs a SOQL query and invokes fn for every record across all pages.
func (d *SalesforceDestination) querySOQL(ctx context.Context, soql string, fn func(map[string]interface{})) error {
	endpoint := d.dataPath("/query")
	req := d.client.R(ctx).SetQueryParam("q", soql)
	for {
		resp, err := req.Get(endpoint)
		if err != nil {
			return fmt.Errorf("salesforce query failed: %w", err)
		}
		if resp.StatusCode() != 200 {
			return fmt.Errorf("salesforce query failed: %w", parseAPIError(resp))
		}
		var parsed struct {
			Records        []map[string]interface{} `json:"records"`
			Done           bool                     `json:"done"`
			NextRecordsURL string                   `json:"nextRecordsUrl"`
		}
		// UseNumber keeps large numeric keys out of float64's exponent form.
		dec := json.NewDecoder(bytes.NewReader(resp.Body()))
		dec.UseNumber()
		if err := dec.Decode(&parsed); err != nil {
			return fmt.Errorf("salesforce query: failed to parse response: %w", err)
		}
		for _, rec := range parsed.Records {
			fn(rec)
		}
		if parsed.Done || parsed.NextRecordsURL == "" {
			return nil
		}
		// Subsequent pages are addressed by a full path that already carries its
		// own cursor, so the original q parameter must not be repeated.
		endpoint = parsed.NextRecordsURL
		req = d.client.R(ctx)
	}
}

// maxReportedRejections caps how many rejected records are listed in the final
// report so a large failure does not produce an unbounded message.
const maxReportedRejections = 100

// rejection is one record Salesforce refused, or one match value that resolved
// to no record.
type rejection struct {
	code       string
	message    string
	fields     []string
	identifier string // which record, e.g. "External_Id__c=A-1"
}

// rejectionLog collects rejected records across every batch so they can be
// reported together at the end of the run. It is safe for concurrent use.
type rejectionLog struct {
	mu    sync.Mutex
	items []rejection
}

func (l *rejectionLog) add(items []rejection) {
	l.mu.Lock()
	l.items = append(l.items, items...)
	l.mu.Unlock()
}

func (l *rejectionLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

// reportRejections fails the run (or warns, under --reject-mode=skip) with each
// rejected record once all batches have been sent.
func reportRejections(sh *shaper, l *rejectionLog) error {
	l.mu.Lock()
	items := l.items
	l.mu.Unlock()
	if len(items) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "salesforce rejected %d %s record(s):", len(items), sh.sobject)
	shown := min(len(items), maxReportedRejections)
	for i := 0; i < shown; i++ {
		id := ""
		if items[i].identifier != "" {
			id = " [" + items[i].identifier + "]"
		}
		fields := ""
		if len(items[i].fields) > 0 {
			fields = " (fields: " + strings.Join(items[i].fields, ", ") + ")"
		}
		fmt.Fprintf(&b, "\n  - (%s)%s %s%s", items[i].code, id, items[i].message, fields)
	}
	if len(items) > shown {
		fmt.Fprintf(&b, "\n  ... and %d more", len(items)-shown)
	}
	for _, it := range items {
		if strings.Contains(it.message, "Duplicate external id specified") {
			// Salesforce rejects every row sharing a key within one request.
			fmt.Fprintf(&b, "\n  hint: the source has the same %s value on more than one row; de-duplicate it on that column", sh.idField)
			break
		}
		if sh.createOnly && (it.code == "DUPLICATE_VALUE" || it.code == "DUPLICATES_DETECTED") {
			b.WriteString("\n  hint: use --incremental-strategy merge with external_id=<External ID field> to update existing records instead of creating them")
			break
		}
	}

	if sh.skip() {
		// Held back so it prints after the run summary rather than buried mid-run
		// above the metrics table (fail mode surfaces last as the returned error).
		output.Deferf("Warning: %s\n", b.String())
		return nil
	}
	return errors.New(b.String())
}

// sobjectField is one field from an sObject describe.
type sobjectField struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	Createable       bool     `json:"createable"`
	Updateable       bool     `json:"updateable"`
	ExternalID       bool     `json:"externalId"`
	IDLookup         bool     `json:"idLookup"`
	RelationshipName string   `json:"relationshipName"`
	ReferenceTo      []string `json:"referenceTo"`
}

type sobjectDescribe struct {
	Fields    []sobjectField `json:"fields"`
	KeyPrefix string         `json:"keyPrefix"`
	// byName indexes Fields by lowercased API name; relationships maps a
	// lowercased relationship name (the dotted column prefix, e.g. "account") to
	// its lookup field (e.g. "AccountId").
	byName        map[string]sobjectField
	relationships map[string]string
	// parentTypes maps a lowercased polymorphic relationship to the object an
	// untyped column defaults to: Owner can be a User or a Queue, and a lookup by
	// email or username means a User.
	parentTypes map[string]string
}

func (s *sobjectDescribe) index() {
	s.byName = make(map[string]sobjectField, len(s.Fields))
	s.relationships = make(map[string]string)
	s.parentTypes = make(map[string]string)
	for _, f := range s.Fields {
		s.byName[strings.ToLower(f.Name)] = f
		if f.RelationshipName != "" {
			s.relationships[strings.ToLower(f.RelationshipName)] = f.Name
			if f.Name == "OwnerId" && len(f.ReferenceTo) > 1 && slices.Contains(f.ReferenceTo, "User") {
				s.parentTypes[strings.ToLower(f.RelationshipName)] = "User"
			}
		}
	}
}

// unknownSObjectError explains a dest-table that names no sObject. The usual
// cause is a display label ("Product") instead of the API name ("Product2"), so
// the org's object list is searched for a matching label to suggest.
func (d *SalesforceDestination) unknownSObjectError(ctx context.Context, name string) error {
	base := fmt.Sprintf("salesforce: no sObject named %q; --dest-table takes the object's API name (e.g. Contact, Product2, Invoice__c), not its display label", name)
	resp, err := d.client.R(ctx).Get(d.dataPath("/sobjects"))
	if err != nil || resp.StatusCode() != 200 {
		return errors.New(base)
	}
	var global struct {
		SObjects []struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			LabelPlural string `json:"labelPlural"`
		} `json:"sobjects"`
	}
	if json.Unmarshal(resp.Body(), &global) != nil {
		return errors.New(base)
	}
	var matches []string
	for _, o := range global.SObjects {
		if strings.EqualFold(o.Label, name) || strings.EqualFold(o.LabelPlural, name) {
			matches = append(matches, o.Name)
		}
	}
	if len(matches) == 0 {
		return errors.New(base)
	}
	return fmt.Errorf("%s — %q is the label of %s", base, name, strings.Join(matches, ", "))
}

// loadFieldMetadata applies the describe to the shaper: the match field's
// canonical name (query results are keyed by it, whatever case the user typed),
// whether it is numeric, and the lookup fields that let null dotted cells clear
// a link. Best-effort: PrepareTable already warned if the describe is unavailable.
func (d *SalesforceDestination) loadFieldMetadata(ctx context.Context, sh *shaper) {
	if sh.archive && !sh.matchesRecords() {
		return
	}
	desc, err := d.describe(ctx, sh.sobject)
	if err != nil {
		config.Debug("[SALESFORCE DEST] no describe for %s (%v); using field names as given", sh.sobject, err)
		return
	}
	if f, ok := desc.byName[strings.ToLower(sh.idField)]; ok && sh.matchesRecords() {
		sh.idField = f.Name
		sh.numericKey = numericFieldTypes[f.Type]
		sh.idKey = f.Type == "id" || f.Type == "reference"
	}
	if sh.archive {
		sh.keyPrefix = desc.KeyPrefix
	}
	if !sh.archive && sh.writeNulls {
		sh.lookupFields = desc.relationships
	}
	if !sh.archive {
		sh.parentTypes = desc.parentTypes
	}
}

var numericFieldTypes = map[string]bool{"int": true, "long": true, "double": true, "currency": true, "percent": true}

// unquotedSOQLTypes are field types SOQL compares as bare literals in a format
// the source value can't be trusted to match, so they can't be resolved keys.
var unquotedSOQLTypes = map[string]bool{"date": true, "datetime": true, "time": true, "boolean": true}

// describe fetches and caches an sObject's field metadata.
func (d *SalesforceDestination) describe(ctx context.Context, sobject string) (*sobjectDescribe, error) {
	key := strings.ToLower(sobject)
	d.mu.Lock()
	if cached, ok := d.describes[key]; ok {
		d.mu.Unlock()
		return cached, nil
	}
	d.mu.Unlock()

	resp, err := d.client.R(ctx).Get(d.dataPath(fmt.Sprintf("/sobjects/%s/describe", url.PathEscape(sobject))))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 {
		return nil, parseAPIError(resp)
	}
	var parsed sobjectDescribe
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}
	parsed.index()

	d.mu.Lock()
	d.describes[key] = &parsed
	d.mu.Unlock()
	return &parsed, nil
}

// PrepareTable validates the dest-table and every source column against the
// sObject's fields before any record is sent, so a schema mismatch fails fast.
func (d *SalesforceDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return nil
	}
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), "", false)
	if err != nil {
		return err
	}
	if len(sh.labelColumns) > 0 {
		output.Warnf("Warning: append always creates new %s records and never matches on the primary key [%s]; it only labels rejected rows. Use --incremental-strategy merge with external_id=<field> to update existing records\n", sh.sobject, strings.Join(sh.labelColumns, ", "))
	}

	desc, err := d.describe(ctx, sh.sobject)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.status == 404 {
			return d.unknownSObjectError(ctx, sh.sobject)
		}
		// Validation is best-effort: a missing permission should not block an
		// otherwise valid load. Salesforce rejects bad fields per record anyway.
		output.Warnf("Warning: salesforce could not describe %s (%v); skipping column validation\n", sh.sobject, err)
		return nil
	}

	// Upsert keys on an External ID field; Salesforce rejects anything else, so
	// catch it before the first batch rather than per record.
	if sh.upsert() {
		f, ok := desc.byName[strings.ToLower(sh.idField)]
		if !ok {
			return fmt.Errorf("salesforce: external_id %q is not a field on %s", sh.idField, sh.sobject)
		}
		if !f.ExternalID && !f.IDLookup {
			return fmt.Errorf("salesforce: external_id %q is not an External ID field on %s; upsert can only match on an External ID field or a lookup field such as Email (available: %s)",
				sh.idField, sh.sobject, strings.Join(externalIDFields(desc), ", "))
		}
	}
	// Delete and update by a non-Id field resolve through SOQL, which only needs
	// the field to exist.
	if sh.resolvesKeys() {
		f, ok := desc.byName[strings.ToLower(sh.idField)]
		if !ok {
			return fmt.Errorf("salesforce: external_id %q is not a field on %s", sh.idField, sh.sobject)
		}
		if unquotedSOQLTypes[f.Type] {
			return fmt.Errorf("salesforce: cannot match on external_id %q: %s fields are not supported as a match field; use a text, number or id field", sh.idField, f.Type)
		}
	}
	if sh.archive {
		return nil
	}

	var unknown, notWritable, untyped, badType []string
	for _, col := range opts.Schema.Columns {
		name := col.Name
		if sh.isIDColumn(name) || sh.exclude[name] || strings.EqualFold(name, recordIDField) {
			continue
		}
		// A dotted column addresses a lookup by external id; only the relationship
		// name is knowable here, the far field belongs to the referenced object.
		if typ, rel, field, ok := relationshipColumn(name); ok {
			lookup, known := desc.relationships[strings.ToLower(rel)]
			if !known {
				unknown = append(unknown, name)
				continue
			}
			lookupField := desc.byName[strings.ToLower(lookup)]
			if !fieldWritable(lookupField, sh) {
				notWritable = append(notWritable, name)
				continue
			}
			targets := lookupField.ReferenceTo
			switch {
			case typ == "" && len(targets) > 1 && desc.parentTypes[strings.ToLower(rel)] == "":
				untyped = append(untyped, fmt.Sprintf("%s (%s can point to %s; e.g. %s.%s.%s)", name, rel, summarizeTargets(targets), rel, targets[0], field))
			case typ != "" && len(targets) > 0 && !slices.ContainsFunc(targets, func(t string) bool { return strings.EqualFold(t, typ) }):
				badType = append(badType, fmt.Sprintf("%s (%s can point to %s)", name, rel, summarizeTargets(targets)))
			}
			continue
		}
		f, ok := desc.byName[strings.ToLower(name)]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if !fieldWritable(f, sh) {
			notWritable = append(notWritable, name)
		}
	}
	if len(untyped) > 0 {
		return fmt.Errorf("salesforce: polymorphic lookups on %q need the parent object in the column name, as Relationship.Object.Field: %s", sh.sobject, strings.Join(untyped, "; "))
	}
	if len(badType) > 0 {
		return fmt.Errorf("salesforce: lookup columns on %q name an object the lookup can't point to: %s", sh.sobject, strings.Join(badType, "; "))
	}
	if len(unknown) > 0 {
		return fmt.Errorf("salesforce: source columns are not fields on %q: [%s]; create them in Salesforce, rename them onto real fields with --columns, or drop them from the source (e.g. --sql-exclude-columns) and re-run", sh.sobject, strings.Join(unknown, ", "))
	}
	if len(notWritable) > 0 {
		return fmt.Errorf("salesforce: source columns are read-only on %q: [%s]; drop them from the source (e.g. --sql-exclude-columns) and re-run", sh.sobject, strings.Join(notWritable, ", "))
	}
	return nil
}

// summarizeTargets lists a lookup's possible parents, shortened for lookups
// like Task.What that can point to dozens of objects.
func summarizeTargets(targets []string) string {
	const shown = 5
	if len(targets) <= shown {
		return strings.Join(targets, ", ")
	}
	return fmt.Sprintf("%s, … (%d objects)", strings.Join(targets[:shown], ", "), len(targets))
}

// fieldWritable reports whether the strategy can write the field: update needs
// it updateable, create needs it createable, and upsert (which may do either)
// needs both.
func fieldWritable(f sobjectField, sh *shaper) bool {
	switch {
	case sh.updateOnly:
		return f.Updateable
	case sh.createOnly:
		return f.Createable
	default:
		return f.Createable && f.Updateable
	}
}

func externalIDFields(desc *sobjectDescribe) []string {
	var out []string
	for _, f := range desc.Fields {
		if (f.ExternalID || f.IDLookup) && !strings.EqualFold(f.Name, recordIDField) {
			out = append(out, f.Name)
		}
	}
	if len(out) == 0 {
		return []string{"none defined"}
	}
	return out
}

func (d *SalesforceDestination) SwapTable(_ context.Context, _ destination.SwapOptions) error {
	return errors.New("salesforce destination does not support atomic swap")
}

func (d *SalesforceDestination) MergeTable(_ context.Context, _ destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for salesforce destination; set external_id=<External ID field> on the dest-table to upsert")
}

func (d *SalesforceDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for salesforce destination")
}

func (d *SalesforceDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for salesforce destination")
}

func (d *SalesforceDestination) DropTable(_ context.Context, _ string) error {
	return errors.New("salesforce destination does not support dropping data")
}

func (d *SalesforceDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for salesforce destination")
}

func (d *SalesforceDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for salesforce destination")
}

func (d *SalesforceDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *SalesforceDestination) GetScheme() string { return "salesforce" }

// IsReverseETL marks Salesforce as a reverse-ETL destination, enabling the RETL
// flags, rename-only --columns, and the update/delete strategies.
func (d *SalesforceDestination) IsReverseETL() {}

// RequiresVerbatimColumns keeps mixed-case field names (FirstName) from being
// snake_cased before they reach the API.
func (d *SalesforceDestination) RequiresVerbatimColumns() {}

// RequiresExplicitStrategy marks Salesforce as needing an explicit
// --incremental-strategy: the framework default "replace" mirrors the sObject
// and deletes records not in the source, too destructive to inherit silently.
func (d *SalesforceDestination) RequiresExplicitStrategy() {}

// SupportsReplaceStrategy is true because replace degrades to a full upsert plus
// a mirror sweep rather than a table drop.
func (d *SalesforceDestination) SupportsReplaceStrategy() bool      { return true }
func (d *SalesforceDestination) SupportsAppendStrategy() bool       { return true }
func (d *SalesforceDestination) SupportsMergeStrategy() bool        { return false }
func (d *SalesforceDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *SalesforceDestination) SupportsSCD2Strategy() bool         { return false }
func (d *SalesforceDestination) SupportsAtomicSwap() bool           { return false }

// timestampUnit reports the array's declared time unit, defaulting to
// microseconds when the type is not the expected timestamp type.
func timestampUnit(a *array.Timestamp) arrow.TimeUnit {
	if tt, ok := a.DataType().(*arrow.TimestampType); ok {
		return tt.Unit
	}
	return arrow.Microsecond
}

// stringValue renders a shaped field value as the string used for match keys.
func stringValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

// fieldValue converts a source cell to the JSON value a Salesforce field takes,
// returning ok=false for nulls (which are omitted unless --write-nulls clears
// them). Unlike string-only APIs, Salesforce expects native JSON types.
func fieldValue(arr arrow.Array, idx int) (interface{}, bool) {
	if arr.IsNull(idx) {
		return nil, false
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			if str, ok := val.(string); ok {
				return strings.Clone(str), true
			}
			return jsonString(val), true
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx), true
	case *array.Int8:
		return int64(a.Value(idx)), true
	case *array.Int16:
		return int64(a.Value(idx)), true
	case *array.Int32:
		return int64(a.Value(idx)), true
	case *array.Int64:
		return a.Value(idx), true
	case *array.Uint8:
		return int64(a.Value(idx)), true
	case *array.Uint16:
		return int64(a.Value(idx)), true
	case *array.Uint32:
		return int64(a.Value(idx)), true
	case *array.Uint64:
		return json.Number(strconv.FormatUint(a.Value(idx), 10)), true
	case *array.Float32:
		return floatValue(float64(a.Value(idx)), 32)
	case *array.Float64:
		return floatValue(a.Value(idx), 64)
	case *array.Decimal128:
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			return json.Number(a.Value(idx).ToString(dt.Scale)), true
		}
		return json.Number(a.Value(idx).ToString(0)), true
	case *array.Decimal256:
		if dt, ok := a.DataType().(*arrow.Decimal256Type); ok {
			return json.Number(a.Value(idx).ToString(dt.Scale)), true
		}
		return json.Number(a.Value(idx).ToString(0)), true
	// Cloned: Arrow strings alias the batch buffer, and bulk mode and the mirror's
	// seen set keep values after the batch is released.
	case *array.String:
		return strings.Clone(a.Value(idx)), true
	case *array.LargeString:
		return strings.Clone(a.Value(idx)), true
	case *array.Binary:
		// Salesforce blob fields (e.g. ContentVersion.VersionData) take base64.
		return base64.StdEncoding.EncodeToString(a.Value(idx)), true
	case *array.LargeBinary:
		return base64.StdEncoding.EncodeToString(a.Value(idx)), true
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Timestamp:
		// Salesforce expects ISO-8601 and rejects microsecond fractions; it stores
		// whole seconds anyway.
		return a.Value(idx).ToTime(timestampUnit(a)).UTC().Format("2006-01-02T15:04:05.000Z"), true
	case *array.Time32:
		return a.Value(idx).ToTime(a.DataType().(*arrow.Time32Type).Unit).Format("15:04:05.000Z"), true
	case *array.Time64:
		return a.Value(idx).ToTime(a.DataType().(*arrow.Time64Type).Unit).Format("15:04:05.000Z"), true
	case *array.Struct, array.ListLike:
		return jsonString(arrowToValue(arr, idx)), true
	default:
		v := arrowutil.Value(arr, idx)
		if v == nil {
			return nil, false
		}
		if str, ok := v.(string); ok {
			return strings.Clone(str), true
		}
		return fmt.Sprintf("%v", v), true
	}
}

// floatValue treats NaN and ±Inf as null: JSON cannot encode them, and one would
// fail the whole request instead of the row.
func floatValue(f float64, bitSize int) (interface{}, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, false
	}
	return json.Number(strconv.FormatFloat(f, 'f', -1, bitSize)), true
}

func jsonString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// arrowToValue decodes nested struct/list values so they can be JSON-encoded
// into a single Salesforce text field.
func arrowToValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}
	switch a := arr.(type) {
	case *array.Struct:
		structType := a.DataType().(*arrow.StructType)
		fields := structType.Fields()
		result := make(map[string]interface{}, len(fields))
		for i, field := range fields {
			result[field.Name] = arrowToValue(a.Field(i), idx)
		}
		return result
	case array.ListLike:
		start, end := a.ValueOffsets(idx)
		values := a.ListValues()
		list := make([]interface{}, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			list = append(list, arrowToValue(values, i))
		}
		return list
	default:
		return arrowutil.Value(arr, idx)
	}
}

var (
	_ destination.Destination           = (*SalesforceDestination)(nil)
	_ destination.ReverseETLDestination = (*SalesforceDestination)(nil)
)
