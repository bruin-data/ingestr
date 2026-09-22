package hubspot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/bruin-data/ingestr/pkg/destination"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablespec"
)

const (
	baseURL = "https://api.hubapi.com"

	// HubSpot CRM batch endpoints accept up to 100 inputs per call.
	batchLimit = 100

	// HubSpot CRM endpoints allow ~10 req/s per token on free/trial tiers.
	rateLimit      = 9.0
	rateLimitBurst = 5

	retryCount   = 10
	retryWait    = 1 * time.Second
	retryMaxWait = 1 * time.Minute

	defaultParallelism = 3
)

type hsConfig struct {
	apiKey string
	// endpoint overrides the API base URL (tests or a self-hosted proxy).
	endpoint string
}

type HubSpotDestination struct {
	client *httpclient.Client

	// objectTypeIDs caches custom object name -> objectTypeId lookups; propUnique
	// caches "<objectType>/<property>" -> hasUniqueValue.
	mu            sync.Mutex
	objectTypeIDs map[string]string
	propUnique    map[string]bool
}

func NewHubSpotDestination() *HubSpotDestination {
	return &HubSpotDestination{objectTypeIDs: map[string]string{}, propUnique: map[string]bool{}}
}

func (d *HubSpotDestination) Schemes() []string {
	return []string{"hubspot"}
}

func parseURI(uri string) (hsConfig, error) {
	if !strings.HasPrefix(uri, "hubspot://") {
		return hsConfig{}, fmt.Errorf("invalid hubspot URI: must start with hubspot://")
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return hsConfig{}, fmt.Errorf("invalid hubspot URI: %w", err)
	}

	params := parsed.Query()
	apiKey := params.Get("api_key")
	serviceKey := params.Get("service_key")
	switch {
	case apiKey != "" && serviceKey != "" && apiKey != serviceKey:
		return hsConfig{}, fmt.Errorf("provide either api_key or service_key in hubspot URI, not both")
	case apiKey == "" && serviceKey != "":
		apiKey = serviceKey
	case apiKey == "" && serviceKey == "":
		return hsConfig{}, fmt.Errorf("api_key or service_key is required in hubspot URI")
	}

	return hsConfig{apiKey: apiKey, endpoint: params.Get("endpoint")}, nil
}

func (d *HubSpotDestination) Connect(_ context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	base := baseURL
	if cfg.endpoint != "" {
		base = cfg.endpoint
	}

	d.client = httpclient.New(
		httpclient.WithBaseURL(base),
		httpclient.WithTimeout(2*time.Minute),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
		// Retry the POST batch endpoints on 429/5xx; resty honors Retry-After.
		httpclient.WithAllowNonIdempotentRetry(),
		httpclient.WithAuth(httpclient.NewBearerAuth(cfg.apiKey)),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("Content-Type", "application/json"),
		httpclient.WithHeader("Accept", "application/json"),
	)

	config.Debug("[HUBSPOT DEST] Connected")
	return nil
}

func (d *HubSpotDestination) Close(_ context.Context) error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// tableParams are the record-shaping options carried on the --dest-table string,
// e.g. "contacts?id_property=email" or "deals".
type tableParams struct {
	IDProperty string `mapstructure:"id_property"`
	// WriteNulls writes source NULLs through (as "") to clear the field, instead
	// of the default of omitting them (leaving the existing value untouched).
	WriteNulls bool `mapstructure:"write_nulls"`
	// Association mode ("associations" dest-table): link From records to To records.
	From                string `mapstructure:"from"`
	To                  string `mapstructure:"to"`
	FromIDProperty      string `mapstructure:"from_id_property"`
	ToIDProperty        string `mapstructure:"to_id_property"`
	AssociationType     string `mapstructure:"association_type"`
	AssociationCategory string `mapstructure:"association_category"`
	// Label names the association type/label; resolved to an associationTypeId.
	Label string `mapstructure:"label"`
}

// recordIDProperty is HubSpot's built-in record id; matching on it routes writes
// to the batch update endpoint, as it is not a unique-value property upsert accepts.
const recordIDProperty = "hs_object_id"

// shaper turns a source row into a HubSpot batch input for one object type.
type shaper struct {
	objectType string
	// idProperty is the HubSpot property to match records on. Empty creates
	// records; hs_object_id updates them by record id; any other property upserts.
	idProperty string
	// idColumn is the source column supplying the idProperty value.
	idColumn string
	exclude  map[string]bool
	// rejectMode is fail_fast | fail | skip (empty = fail). It governs how a row
	// the API can't apply is handled.
	rejectMode string
	// archive soft-deletes the records (or association links) instead of writing.
	archive bool
	// updateOnly (strategy=update) updates existing records and never creates;
	// createOnly (strategy=append) always creates and never matches.
	updateOnly bool
	createOnly bool
	// mirror (strategy=replace) upserts every source row, then archives HubSpot
	// records whose idProperty value was not in the source (seen; concurrency-safe).
	mirror bool
	seen   *sync.Map
	// created holds the record ids this run inserted via create (keyless mirror
	// rows have no match value to record in `seen`), so the finalize sweep does not
	// archive a record the same run just wrote. Concurrency-safe.
	created *sync.Map
	// sawSource records whether the source delivered any (non-empty) batch. A mirror
	// whose source produced 0 rows (a transient empty extract, an over-restrictive
	// filter) must not archive every record — that would turn a hiccup into a full
	// wipe. Set once per run, read at finalize.
	sawSource atomic.Bool
	// seenLinks (association mirror) maps a From record id to its source To ids,
	// so finalize can remove links the source did not declare.
	seenLinks *sync.Map
	// writeNulls sends "" for null cells to clear the field, rather than omitting.
	writeNulls bool
	// searchMatch (update/delete, non-unique idProperty): locate records via the
	// Search API and update/archive every match, not one-to-one via batch update.
	searchMatch bool

	// Association mode fields (set when associateTo is non-empty).
	associateTo string
	fromColumn  string
	toColumn    string
	// fromProperty/toProperty name the HubSpot property the values match on;
	// empty means the column already holds record ids (hs_object_id).
	fromProperty        string
	toProperty          string
	associationType     int
	associationCategory string
	associationLabel    string
}

// associate reports whether the shaper links records instead of writing properties.
func (s *shaper) associate() bool { return s.associateTo != "" }

// skip reports whether rejected rows are collected and the run still succeeds.
func (s *shaper) skip() bool { return s.rejectMode == string(config.RejectSkip) }

// failFast reports whether the run aborts on the first rejected row.
func (s *shaper) failFast() bool { return s.rejectMode == string(config.RejectFailFast) }

// matchesRecords reports whether writes carry an id to match existing records
// (either upsert or update), as opposed to plain create.
func (s *shaper) matchesRecords() bool { return s.idProperty != "" }

// update matches existing records by their HubSpot record id via batch update.
func (s *shaper) update() bool { return s.idProperty == recordIDProperty }

// upsert matches on a unique-value property via batch upsert (create-or-update).
func (s *shaper) upsert() bool { return s.matchesRecords() && !s.update() }

// parseShaper builds the record/association shaper from --incremental-strategy,
// the dest-table (object + id_property), --primary-key, and the run flags.
func parseShaper(table, strategy string, primaryKeys []string, rejectMode string, writeNulls bool) (*shaper, error) {
	var p tableParams
	path, _, err := tablespec.Parse(table, &p)
	if err != nil {
		return nil, err
	}

	// Tolerate an optional schema qualifier ("hubspot.contacts" -> "contacts").
	objectType := strings.TrimSpace(path)
	if i := strings.LastIndex(objectType, "."); i >= 0 {
		objectType = objectType[i+1:]
	}
	objectType = strings.ToLower(objectType)
	if objectType == "" {
		return nil, fmt.Errorf("hubspot dest-table must be a CRM object type, e.g. \"contacts\", \"companies\", \"deals\"")
	}

	// Associations use an "obj1+obj2" dest-table (or the legacy "associations").
	if objectType == "associations" || strings.Contains(objectType, "+") {
		return parseAssociationShaper(objectType, p, strategy, primaryKeys, rejectMode)
	}

	// ingestr's own decoration columns are never sent as HubSpot properties.
	exclude := map[string]bool{
		naming.IngestrLoadedAtColumn: true,
		naming.IngestrRunIDColumn:    true,
	}

	switch strategy {
	case string(config.StrategyDelete):
		return parseArchiveShaper(objectType, p, primaryKeys, rejectMode)

	case string(config.StrategyAppend):
		// Create-only: no match, always create.
		return &shaper{
			objectType: objectType,
			exclude:    exclude,
			rejectMode: rejectMode,
			createOnly: true,
			writeNulls: writeNulls || p.WriteNulls,
		}, nil
	}

	// The match property (HubSpot side) comes only from id_property; update defaults
	// to the record id, merge/replace require it explicitly. --primary-key is
	// source-side only and never names the property.
	updateOnly := strategy == string(config.StrategyUpdate)
	mirror := strategy == string(config.StrategyReplace)
	idProperty := p.IDProperty
	if idProperty == "" && updateOnly {
		idProperty = recordIDProperty
	}
	if idProperty == "" {
		return nil, fmt.Errorf("hubspot: %s needs a match property — set id_property=<property> on the dest-table", strategy)
	}
	// Source column supplying the match value: a single --primary-key. update
	// defaults it to the hs_object_id column (record-id match); merge/replace
	// require it explicitly, since their match key differs per object.
	defaultColumn := ""
	if updateOnly {
		defaultColumn = recordIDProperty
	}
	idColumn, err := sourceIDColumn(primaryKeys, defaultColumn)
	if err != nil {
		return nil, err
	}

	sh := &shaper{
		objectType: objectType,
		idProperty: idProperty,
		idColumn:   idColumn,
		exclude:    exclude,
		rejectMode: rejectMode,
		updateOnly: updateOnly,
		mirror:     mirror,
		writeNulls: writeNulls || p.WriteNulls,
	}
	if mirror {
		sh.seen = &sync.Map{}
		sh.created = &sync.Map{}
	}
	return sh, nil
}

// sourceIDColumn returns the source column holding the match value from a single
// --primary-key. When none is given it falls back to defaultColumn (used by
// update/delete, which default to the hs_object_id column); an empty default
// means the strategy requires an explicit --primary-key.
func sourceIDColumn(primaryKeys []string, defaultColumn string) (string, error) {
	switch len(primaryKeys) {
	case 1:
		return primaryKeys[0], nil
	case 0:
		if defaultColumn != "" {
			return defaultColumn, nil
		}
		return "", fmt.Errorf("hubspot: this strategy needs a source key column — pass --primary-key")
	default:
		return "", fmt.Errorf("hubspot: cannot match on a composite primary key [%s]; pass a single --primary-key", strings.Join(primaryKeys, ", "))
	}
}

// parseArchiveShaper builds a shaper that soft-deletes records, matched by the id
// column (record ids, or a unique property resolved to ids when id_property is set).
func parseArchiveShaper(objectType string, p tableParams, primaryKeys []string, rejectMode string) (*shaper, error) {
	// Match property comes only from id_property; defaults to the record id.
	idProperty := p.IDProperty
	if idProperty == "" {
		idProperty = recordIDProperty
	}
	// delete defaults the source column to the hs_object_id column too.
	idColumn, err := sourceIDColumn(primaryKeys, recordIDProperty)
	if err != nil {
		return nil, err
	}
	return &shaper{
		objectType: objectType,
		idProperty: idProperty,
		idColumn:   idColumn,
		rejectMode: rejectMode,
		archive:    true,
	}, nil
}

// parseAssociationShaper builds a shaper for an "obj1+obj2" dest-table, linking
// obj1->obj2 by positional --primary-key/id_property; strategy = add/unlink/mirror.
func parseAssociationShaper(objectType string, p tableParams, strategy string, primaryKeys []string, rejectMode string) (*shaper, error) {
	from, to := p.From, p.To
	if from == "" && to == "" && strings.Contains(objectType, "+") {
		parts := strings.SplitN(objectType, "+", 2)
		from, to = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("hubspot: association dest-table must be 'obj1+obj2' (or associations?from=&to=)")
	}

	switch strategy {
	case string(config.StrategyMerge), string(config.StrategyDelete), string(config.StrategyReplace):
	default:
		return nil, fmt.Errorf("hubspot: associations support merge (add), delete (unlink) and replace (mirror), not %q", strategy)
	}
	archive := strategy == string(config.StrategyDelete)
	mirror := strategy == string(config.StrategyReplace)

	// Source key columns come only from --primary-key k1,k2 (obj1 then obj2).
	if len(primaryKeys) != 2 {
		return nil, fmt.Errorf("hubspot: associations need two source key columns — pass --primary-key k1,k2 (obj1 then obj2)")
	}
	fromColumn, toColumn := primaryKeys[0], primaryKeys[1]
	if fromColumn == toColumn {
		return nil, fmt.Errorf("hubspot: the two association keys are the same column %q; pass distinct --primary-key k1,k2", fromColumn)
	}

	// Match properties: id_property=fromProp,toProp positionally (empty = record id).
	fromProperty, toProperty := p.FromIDProperty, p.ToIDProperty
	if p.IDProperty != "" {
		props := strings.SplitN(p.IDProperty, ",", 2)
		fromProperty = strings.TrimSpace(props[0])
		if len(props) == 2 {
			toProperty = strings.TrimSpace(props[1])
		}
	}

	assocType := 0
	// An explicit association_category is honored as-is; an empty one is resolved
	// from the type id at connect time (a numeric type may name a user- or
	// integration-defined label, not a HUBSPOT_DEFINED one).
	category := p.AssociationCategory
	if p.AssociationType != "" {
		n, err := strconv.Atoi(p.AssociationType)
		if err != nil {
			return nil, fmt.Errorf("hubspot: association_type must be a numeric association type id, got %q", p.AssociationType)
		}
		assocType = n
	}
	sh := &shaper{
		objectType:          strings.ToLower(from),
		associateTo:         strings.ToLower(to),
		fromColumn:          fromColumn,
		toColumn:            toColumn,
		fromProperty:        fromProperty,
		toProperty:          toProperty,
		associationType:     assocType,
		associationCategory: category,
		associationLabel:    p.Label,
		rejectMode:          rejectMode,
		archive:             archive,
		mirror:              mirror,
	}
	if mirror {
		sh.seenLinks = &sync.Map{}
	}
	return sh, nil
}

// validateColumns fails fast when the source is missing a column the mode needs.
func (s *shaper) validateColumns(record arrow.RecordBatch) error {
	if !s.matchesRecords() {
		return nil
	}
	for i := 0; i < int(record.NumCols()); i++ {
		if record.ColumnName(i) == s.idColumn {
			return nil
		}
	}
	names := make([]string, 0, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		names = append(names, record.ColumnName(i))
	}
	return fmt.Errorf("hubspot: id column %q not found in source (available: %s); pass --primary-key to name the match column", s.idColumn, strings.Join(names, ", "))
}

// validateAssociationColumns fails fast when a from/to id column is missing.
func (s *shaper) validateAssociationColumns(colIndex map[string]int) error {
	for _, col := range []string{s.fromColumn, s.toColumn} {
		if _, ok := colIndex[col]; !ok {
			names := make([]string, 0, len(colIndex))
			for name := range colIndex {
				names = append(names, name)
			}
			return fmt.Errorf("hubspot: association column %q not found in source (available: %s)", col, strings.Join(names, ", "))
		}
	}
	return nil
}

// associationInput is one record link in a v4 batch association request.
type associationInput struct {
	From  associationRef        `json:"from"`
	To    associationRef        `json:"to"`
	Types []associationTypeSpec `json:"types,omitempty"`
}

type associationRef struct {
	ID string `json:"id"`
}

// associationArchiveInput is one link removal. The v4 batch/archive endpoint
// (unlike batch/create) expects "to" as an array of refs, not a single ref.
type associationArchiveInput struct {
	From associationRef   `json:"from"`
	To   []associationRef `json:"to"`
}

type associationTypeSpec struct {
	AssociationCategory string `json:"associationCategory"`
	AssociationTypeID   int    `json:"associationTypeId"`
}

// cellValues returns a row's value(s): a list column yields every element (array
// keys), a scalar one value; nulls and empty strings are dropped.
func cellValues(arr arrow.Array, idx int) []string {
	if arr.IsNull(idx) {
		return nil
	}
	if _, isExt := arr.DataType().(arrow.ExtensionType); isExt {
		// JSON columns (Mongo/JSONL) are string-backed extension arrays. A JSON
		// array value explodes to one key per element, matching native lists; any
		// other JSON value is a single key.
		if v, ok := propertyValue(arr, idx); ok && v != "" {
			return jsonArrayElements(v)
		}
		return nil
	}
	if lst, ok := arr.(array.ListLike); ok {
		start, end := lst.ValueOffsets(idx)
		vals := lst.ListValues()
		out := make([]string, 0, int(end-start))
		for i := int(start); i < int(end); i++ {
			if v, ok := propertyValue(vals, i); ok && v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	if v, ok := propertyValue(arr, idx); ok && v != "" {
		return []string{v}
	}
	return nil
}

// jsonArrayElements returns each element of a JSON array string as its own value
// (scalars stringified, nested values re-encoded). A non-array JSON value is
// returned unchanged as a single value.
func jsonArrayElements(v string) []string {
	if !strings.HasPrefix(strings.TrimSpace(v), "[") {
		return []string{v}
	}
	dec := json.NewDecoder(strings.NewReader(v))
	dec.UseNumber()
	var elems []interface{}
	if err := dec.Decode(&elems); err != nil {
		return []string{v}
	}
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		switch x := e.(type) {
		case nil:
			// skip null elements
		case string:
			if x != "" {
				out = append(out, x)
			}
		case bool:
			out = append(out, strconv.FormatBool(x))
		case json.Number:
			out = append(out, x.String())
		default:
			out = append(out, jsonString(x))
		}
	}
	return out
}

// allCellValues flattens a whole column (scalars, or every element of list
// cells) for business-key resolution.
func allCellValues(arr arrow.Array) []string {
	var out []string
	for i := 0; i < arr.Len(); i++ {
		out = append(out, cellValues(arr, i)...)
	}
	return out
}

// shapeAssociation builds the link(s) for one row plus any not-found rejections:
// array keys explode to the cartesian product (deduped), and fromResolve/toResolve
// map each business key to every matching record id (a non-unique key links all of
// them, not just one). A side that matches on a business key but whose (non-empty)
// value resolves to no record is a reject handled per --reject-mode, not a silent
// skip. An empty cell yields neither a link nor a reject (the caller skips it).
func (s *shaper) shapeAssociation(record arrow.RecordBatch, colIndex map[string]int, row int, fromResolve, toResolve map[string][]string) ([]associationInput, []string, []rejection) {
	fromVals := cellValues(record.Column(colIndex[s.fromColumn]), row)
	toVals := cellValues(record.Column(colIndex[s.toColumn]), row)
	if len(fromVals) == 0 {
		return nil, nil, nil
	}
	// Outside a mirror an empty To cell is a plain skip: there is nothing to link
	// and no existing links to reconcile away.
	if len(toVals) == 0 && !s.mirror {
		return nil, nil, nil
	}

	// Resolve each side's values to record ids once, collecting not-founds so a
	// bad key surfaces as a reject rather than vanishing.
	var unresolved []rejection
	resolveSide := func(vals []string, resolve map[string][]string, objectType, property string) []string {
		ids := make([]string, 0, len(vals))
		for _, v := range vals {
			if resolve == nil {
				ids = append(ids, v)
				continue
			}
			if matched, ok := resolve[matchKey(v)]; ok && len(matched) > 0 {
				ids = append(ids, matched...)
			} else {
				unresolved = append(unresolved, rejection{
					category:   objectNotFoundCategory,
					message:    fmt.Sprintf("no %s found with %s=%q", objectType, property, v),
					identifier: property + "=" + v,
				})
			}
		}
		return ids
	}
	fromIDs := resolveSide(fromVals, fromResolve, s.objectType, s.fromProperty)

	// Mirror + empty To cell: the From is present but wants no links, so report it
	// for the mirror finalizer to unlink everything it currently has.
	if len(toVals) == 0 {
		return nil, fromIDs, unresolved
	}
	toIDs := resolveSide(toVals, toResolve, s.associateTo, s.toProperty)

	var out []associationInput
	seen := make(map[string]bool, len(fromIDs)*len(toIDs))
	for _, f := range fromIDs {
		for _, t := range toIDs {
			if key := f + "\x00" + t; seen[key] {
				continue
			} else {
				seen[key] = true
			}
			in := associationInput{From: associationRef{ID: f}, To: associationRef{ID: t}}
			if s.associationType != 0 {
				// Types drive both create (which label to add) and a labeled delete
				// (which label to remove via labels/archive) — an unlabeled archive
				// omits them and removes every association type between the pair.
				in.Types = []associationTypeSpec{{AssociationCategory: s.associationCategory, AssociationTypeID: s.associationType}}
			}
			out = append(out, in)
		}
	}
	return out, nil, unresolved
}

// batchInput is one record in a HubSpot batch create/upsert/update request.
type batchInput struct {
	IDProperty string            `json:"idProperty,omitempty"`
	ID         string            `json:"id,omitempty"`
	Properties map[string]string `json:"properties"`
}

// shapeRow builds the batch input and endpoint action for one row. A row with a
// missing match value is created rather than skipped (create-on-missing).
func (s *shaper) shapeRow(record arrow.RecordBatch, colIndex map[string]int, row int) (batchInput, string, bool) {
	props := make(map[string]string)
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		if name == s.idColumn || s.exclude[name] {
			continue
		}
		col := record.Column(i)
		if v, ok := propertyValue(col, row); ok {
			props[name] = v
		} else if s.writeNulls && col.IsNull(row) {
			// Explicitly clear the field rather than leaving it untouched.
			props[name] = ""
		}
	}

	in := batchInput{Properties: props}
	if s.createOnly || !s.matchesRecords() {
		return in, "create", true
	}

	idVal, ok := propertyValue(record.Column(colIndex[s.idColumn]), row)
	if !ok || idVal == "" {
		if s.updateOnly {
			// Update-only can't create; a row without a match value is skipped.
			return batchInput{}, "", false
		}
		// No match value: the row can't target an existing record, so create it.
		// In a mirror the created record's id is tracked (writeBatch) so the
		// finalize sweep does not archive what this run just wrote.
		return in, "create", true
	}

	in.ID = idVal
	if s.updateOnly {
		// Update by the match property (batch update accepts idProperty). Not
		// found is reported as a reject; it is never re-routed to create.
		if s.idProperty != recordIDProperty {
			in.IDProperty = s.idProperty
		}
		return in, "update", true
	}
	if s.upsert() {
		// Upsert matches on a named property; update-by-record-id carries id alone.
		in.IDProperty = s.idProperty
		return in, "upsert", true
	}
	return in, "update", true
}

// primaryKeysFor resolves the run's primary keys, falling back to the schema's
// (always populated) when WriteOptions carries none.
func primaryKeysFor(explicit []string, sch *schema.TableSchema) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if sch != nil {
		return sch.PrimaryKeys
	}
	return nil
}

// drainRecords releases every remaining batch so a source producer goroutine is
// never left blocked on a send after the consumer stops early.
func drainRecords(records <-chan source.RecordBatchResult) {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
}

// reportWithWriteErr surfaces the records rejected before a hard failure alongside
// that failure, so a 5xx/auth error late in the run doesn't hide the reject list.
func reportWithWriteErr(sh *shaper, rejects *rejectionLog, writeErr error) error {
	if rejErr := reportRejections(sh, rejects); rejErr != nil {
		return errors.Join(writeErr, rejErr)
	}
	return writeErr
}

func (d *HubSpotDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), opts.RejectMode, opts.WriteNulls)
	if err != nil {
		drainRecords(records)
		return err
	}
	if sh.associate() {
		if err := d.resolveAssociationType(ctx, sh); err != nil {
			drainRecords(records)
			return err
		}
	} else if err := d.resolveMatchMode(ctx, sh); err != nil {
		drainRecords(records)
		return err
	}

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
		drainRecords(records)
		return reportWithWriteErr(sh, &rejects, writeErr)
	}

	warnSkipped(&skipped, sh)
	config.Debug("[HUBSPOT DEST] Wrote %d %s record(s)", totalRows, sh.objectType)
	return d.finalizeAndReport(ctx, sh, &rejects)
}

func (d *HubSpotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), opts.RejectMode, opts.WriteNulls)
	if err != nil {
		drainRecords(records)
		return err
	}
	if sh.associate() {
		if err := d.resolveAssociationType(ctx, sh); err != nil {
			drainRecords(records)
			return err
		}
	} else if err := d.resolveMatchMode(ctx, sh); err != nil {
		drainRecords(records)
		return err
	}

	// HubSpot caps effective write concurrency at defaultParallelism to respect API
	// rate limits. opts.Parallelism carries the framework default (extract
	// parallelism) when the user set no --destination-parallelism, so capping it is
	// routine and logged at debug level rather than warned about on every run.
	parallelism := opts.Parallelism
	if parallelism <= 0 || parallelism > defaultParallelism {
		if parallelism > defaultParallelism {
			config.Debug("[HUBSPOT DEST] capping write parallelism from %d to %d for API rate limits", parallelism, defaultParallelism)
		}
		parallelism = defaultParallelism
	}

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
				if ctx.Err() != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					continue
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
	// A worker that returned early on error (e.g. the sole worker at
	// --destination-parallelism 1) leaves the channel undrained; release anything
	// still queued so the producer goroutine can't block forever on a send. In the
	// success path the channel is already closed and drained, so this is a no-op.
	drainRecords(records)
	if err := <-errs; err != nil {
		return reportWithWriteErr(sh, &rejects, err)
	}

	warnSkipped(&skipped, sh)
	return d.finalizeAndReport(ctx, sh, &rejects)
}

// finalizeAndReport runs the mirror archive pass (if any) and reports rejected
// records. Two safety rules:
//   - In fail mode (fail_fast has already returned earlier; skip opts in) a run
//     with any rejected row skips the destructive archive entirely — a failed run
//     must not also delete records the user still has to reconcile.
//   - If the archive pass itself errors, the rejections collected so far are still
//     reported so they are not silently lost behind the hard failure.
func (d *HubSpotDestination) finalizeAndReport(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
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

// finalizeMirror completes a record replace (mirror): lists the object's records
// and archives any whose idProperty value was not in the source. No-op otherwise.
func (d *HubSpotDestination) finalizeMirror(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
	if !sh.mirror {
		return nil
	}
	if sh.associate() {
		return d.finalizeAssociationMirror(ctx, sh, rejects)
	}
	if sh.seen == nil {
		return nil
	}
	// A source that produced 0 rows must not archive the entire object — that turns
	// a transient empty extract (upstream hiccup, over-restrictive filter) into an
	// irreversible full wipe. Skip the sweep and warn; an intentional clear-out is
	// what --incremental-strategy delete is for.
	if !sh.sawSource.Load() {
		output.Warnf("Warning: hubspot replace (mirror) of %s: source produced 0 rows; skipping the archive sweep so an empty extract does not delete every record. Use --incremental-strategy delete to remove records intentionally.\n", sh.objectType)
		return nil
	}

	stale, err := d.listStaleIDs(ctx, sh)
	if err != nil {
		return fmt.Errorf("hubspot mirror: failed to list existing %s records: %w", sh.objectType, err)
	}
	if len(stale) == 0 {
		return nil
	}
	config.Debug("[HUBSPOT DEST] mirror archiving %d stale %s record(s)", len(stale), sh.objectType)

	for start := 0; start < len(stale); start += batchLimit {
		end := min(start+batchLimit, len(stale))
		if err := d.sendArchive(ctx, sh, stale[start:end], rejects); err != nil {
			return err
		}
	}
	return nil
}

// listStaleIDs paginates the object's records and returns the hs_object_id of
// each whose idProperty value is not in the source; missing-value records skip.
func (d *HubSpotDestination) listStaleIDs(ctx context.Context, sh *shaper) ([]string, error) {
	var stale []string
	after := ""
	for {
		req := d.client.R(ctx).
			SetQueryParam("limit", "100").
			SetQueryParam("properties", sh.idProperty).
			SetQueryParam("archived", "false")
		if after != "" {
			req = req.SetQueryParam("after", after)
		}
		endpoint := fmt.Sprintf("/crm/v3/objects/%s", url.PathEscape(d.effectiveObjectType(sh.objectType)))
		resp, err := req.Get(endpoint)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != 200 && resp.StatusCode() != 207 && strings.Contains(strings.ToLower(resp.String()), "infer object type") {
			if _, ok := d.resolveObjectTypeID(ctx, sh.objectType); ok {
				endpoint = fmt.Sprintf("/crm/v3/objects/%s", url.PathEscape(d.effectiveObjectType(sh.objectType)))
				resp, err = req.Get(endpoint)
				if err != nil {
					return nil, err
				}
			}
		}
		if resp.StatusCode() != 200 {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode(), resp.String())
		}

		var parsed struct {
			Results []struct {
				ID         string            `json:"id"`
				Properties map[string]string `json:"properties"`
			} `json:"results"`
			Paging struct {
				Next struct {
					After string `json:"after"`
				} `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
			return nil, err
		}

		for _, r := range parsed.Results {
			// A record this run just created (e.g. a keyless source row that has no
			// match value to appear in `seen`) must survive its own run's sweep.
			if sh.created != nil {
				if _, ok := sh.created.Load(r.ID); ok {
					continue
				}
			}
			key := r.Properties[sh.idProperty]
			if sh.idProperty == recordIDProperty {
				key = r.ID
			}
			// Any record whose match value is not in the source is archived —
			// including records with no value for the property (their empty key is
			// never in seen). replace is a full mirror of the object type.
			// Compare through matchKey: HubSpot normalizes some returned values
			// (e.g. lowercased email), so a raw compare would miss a record we just
			// wrote and archive it. (Identity for record ids, which aren't normalized.)
			if _, ok := sh.seen.Load(matchKey(key)); !ok {
				stale = append(stale, r.ID)
			}
		}

		after = parsed.Paging.Next.After
		if after == "" {
			break
		}
	}
	return stale, nil
}

// finalizeAssociationMirror completes an association replace (mirror): for every
// source From record, removes live links to the target not declared in the source.
func (d *HubSpotDestination) finalizeAssociationMirror(ctx context.Context, sh *shaper, rejects *rejectionLog) error {
	if sh.seenLinks == nil {
		return nil
	}
	var rangeErr error
	sh.seenLinks.Range(func(key, val interface{}) bool {
		fromID := key.(string)
		want := val.(*sync.Map)

		live, err := d.listAssociationsFor(ctx, sh, fromID)
		if err != nil {
			rangeErr = fmt.Errorf("hubspot mirror: failed to list associations for %s %s: %w", sh.objectType, fromID, err)
			return false
		}

		// A labeled mirror removes only the managed label via labels/archive (which
		// carries the types); an unlabeled one removes every type via archive.
		verb := "archive"
		if sh.associationType != 0 {
			verb = "labels/archive"
		}
		var stale []associationInput
		for _, toID := range live {
			if _, ok := want.Load(toID); !ok {
				in := associationInput{From: associationRef{ID: fromID}, To: associationRef{ID: toID}}
				if sh.associationType != 0 {
					in.Types = []associationTypeSpec{{AssociationCategory: sh.associationCategory, AssociationTypeID: sh.associationType}}
				}
				stale = append(stale, in)
			}
		}
		if len(stale) == 0 {
			return true
		}
		config.Debug("[HUBSPOT DEST] mirror unlinking %d stale %s->%s association(s) for %s", len(stale), sh.objectType, sh.associateTo, fromID)
		for start := 0; start < len(stale); start += batchLimit {
			end := min(start+batchLimit, len(stale))
			res, err := d.postAssociations(ctx, sh.objectType, sh.associateTo, verb, stale[start:end])
			if err != nil {
				rangeErr = err
				return false
			}
			if len(res.rejections) > 0 {
				if (!res.ok && !isRecordLevelStatus(res.status)) || sh.failFast() {
					rangeErr = fmt.Errorf("hubspot mirror unlink %s->%s returned status %d: %s", sh.objectType, sh.associateTo, res.status, res.rejections[0].message)
					return false
				}
				output.Warnf("Warning: hubspot rejected %d %s->%s unlink(s) during mirror; first error: %s\n", len(res.rejections), sh.objectType, sh.associateTo, res.rejections[0].message)
				rejects.add(res.rejections)
			}
		}
		return true
	})
	return rangeErr
}

// listAssociationsFor returns the To record ids currently linked from a single
// From record, paginating the v4 associations endpoint.
func (d *HubSpotDestination) listAssociationsFor(ctx context.Context, sh *shaper, fromID string) ([]string, error) {
	var ids []string
	after := ""
	for {
		req := d.client.R(ctx).SetQueryParam("limit", "500")
		if after != "" {
			req = req.SetQueryParam("after", after)
		}
		endpoint := fmt.Sprintf("/crm/v4/objects/%s/%s/associations/%s",
			url.PathEscape(d.effectiveObjectType(sh.objectType)), url.PathEscape(fromID), url.PathEscape(d.effectiveObjectType(sh.associateTo)))
		resp, err := req.Get(endpoint)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != 200 {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode(), resp.String())
		}
		var parsed struct {
			Results []struct {
				ToObjectID       json.Number `json:"toObjectId"`
				AssociationTypes []struct {
					TypeID int `json:"typeId"`
				} `json:"associationTypes"`
			} `json:"results"`
			Paging struct {
				Next struct {
					After string `json:"after"`
				} `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
			return nil, err
		}
		for _, r := range parsed.Results {
			s := r.ToObjectID.String()
			if s == "" {
				continue
			}
			// A labeled mirror manages only its own label: a link that does not carry
			// the managed type is left untouched (never treated as stale), so other
			// labels between the same pair survive reconciliation.
			if sh.associationType != 0 {
				managed := false
				for _, t := range r.AssociationTypes {
					if t.TypeID == sh.associationType {
						managed = true
						break
					}
				}
				if !managed {
					continue
				}
			}
			ids = append(ids, s)
		}
		after = parsed.Paging.Next.After
		if after == "" {
			break
		}
	}
	return ids, nil
}

func warnSkipped(skipped *atomic.Int64, sh *shaper) {
	n := skipped.Load()
	if n == 0 {
		return
	}
	if sh.associate() {
		output.Warnf("Warning: hubspot skipped %d %s association(s) with a missing id value\n", n, sh.objectType)
		return
	}
	output.Warnf("Warning: hubspot skipped %d %s record(s) with a missing %s value\n", n, sh.objectType, sh.idColumn)
}

// writeBatch shapes a record batch and sends it in chunks of batchLimit.
func (d *HubSpotDestination) writeBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	// Reached only with a non-empty batch (0-row batches are filtered by the
	// caller), so this marks that the source actually produced data — the mirror
	// finalize sweep relies on it to tell an empty extract from a full mirror.
	sh.sawSource.Store(true)
	colIndex := make(map[string]int, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		colIndex[record.ColumnName(i)] = i
	}

	if sh.associate() {
		return d.writeAssociationBatch(ctx, sh, record, colIndex, skipped, rejects)
	}

	if sh.archive {
		return d.writeArchiveBatch(ctx, sh, record, colIndex, skipped, rejects)
	}

	if err := sh.validateColumns(record); err != nil {
		return 0, err
	}

	// Update on a non-unique property: locate every match via Search, then update
	// each by record id (batch update can't match a non-unique property).
	if sh.updateOnly && sh.searchMatch {
		return d.writeSearchUpdateBatch(ctx, sh, record, colIndex, skipped, rejects)
	}

	rows := int(record.NumRows())
	if sh.mirror && sh.seen != nil {
		idCol := record.Column(colIndex[sh.idColumn])
		for row := 0; row < rows; row++ {
			if v, ok := propertyValue(idCol, row); ok && v != "" {
				sh.seen.Store(matchKey(v), struct{}{})
			}
		}
	}
	var written int64
	// Rows are grouped by endpoint action so a single batch stays homogeneous.
	// Update mode may yield both "update" (id present) and "create" (id absent).
	batches := make(map[string][]batchInput, 2)

	flush := func(action string) error {
		batch := batches[action]
		if len(batch) == 0 {
			return nil
		}
		// Update-by-record-id (merge) re-routes ids that don't exist to create;
		// update-only records them as rejects instead; other actions post directly.
		send := d.send
		if action == "update" {
			if sh.updateOnly {
				send = func(ctx context.Context, sh *shaper, items []batchInput, _ string, rejects *rejectionLog) error {
					return d.sendUpdateOnly(ctx, sh, items, rejects)
				}
			} else {
				send = func(ctx context.Context, sh *shaper, items []batchInput, _ string, rejects *rejectionLog) error {
					return d.sendUpdate(ctx, sh, items, rejects)
				}
			}
		}
		if err := send(ctx, sh, batch, action, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batches[action] = batch[:0]
		return nil
	}

	for row := 0; row < rows; row++ {
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

// batchResult holds the interpreted outcome of a HubSpot batch response.
type batchResult struct {
	ok         bool
	status     int
	category   string
	rejections []rejection
	// createdIDs are the record ids HubSpot returned for the batch (create/upsert),
	// used by a mirror to protect just-written records from its archive sweep.
	createdIDs []string
}

// isRecordLevelStatus reports whether a failed status is a per-record data problem
// (validation/conflict) that --reject-mode=skip may tolerate; others abort the run.
func isRecordLevelStatus(status int) bool {
	return status == 400 || status == 409
}

// parseBatchResponse reads a batch response into per-record rejections and whether
// the whole request succeeded (200/201/207); other statuses become one rejection.
func parseBatchResponse(resp *httpclient.Response) batchResult {
	var body struct {
		Category string `json:"category"`
		Message  string `json:"message"`
		Errors   []struct {
			Category string          `json:"category"`
			Message  string          `json:"message"`
			Context  json.RawMessage `json:"context"`
		} `json:"errors"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	_ = json.Unmarshal(resp.Body(), &body)

	status := resp.StatusCode()
	// 204 No Content is the success status for batch/archive.
	ok := status == 200 || status == 201 || status == 204 || status == 207
	rejections := make([]rejection, 0, len(body.Errors))
	for _, e := range body.Errors {
		rejections = append(rejections, rejection{category: e.Category, message: e.Message, context: e.Context})
	}
	if !ok && len(rejections) == 0 {
		msg := body.Message
		if msg == "" {
			msg = resp.String()
		}
		rejections = append(rejections, rejection{category: body.Category, message: msg})
	}
	var createdIDs []string
	for _, r := range body.Results {
		if r.ID != "" {
			createdIDs = append(createdIDs, r.ID)
		}
	}
	return batchResult{ok: ok, status: status, category: body.Category, rejections: rejections, createdIDs: createdIDs}
}

// objectNotFoundCategory is the error category HubSpot returns from batch update
// (and read) when an id does not exist. The whole batch fails atomically.
const objectNotFoundCategory = "OBJECT_NOT_FOUND"

// isSystemic reports whether a whole-batch failure is structural rather than
// per-record — e.g. an upsert against a non-unique id_property, which HubSpot
// rejects for every row identically. Such a failure must abort the run: bisecting
// it just amplifies requests, and tolerating it under --reject-mode=skip would
// report success while writing nothing.
func (r batchResult) isSystemic() bool {
	if r.ok || len(r.rejections) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(r.rejections[0].message), "non-unique")
}

// systemicBatchError returns a fatal error when a whole-batch failure is
// structural (see isSystemic) — such a batch must abort rather than be bisected
// into per-record rejects that would look tolerable under --reject-mode=skip.
func systemicBatchError(action, objectType string, res batchResult) error {
	if !res.isSystemic() {
		return nil
	}
	return fmt.Errorf("hubspot %s %s failed for the whole batch (not a per-record error): %s", action, objectType, res.rejections[0].message)
}

// hasNotFound reports whether the batch failed because some ids do not exist.
func (r batchResult) hasNotFound() bool {
	for _, rj := range r.rejections {
		if rj.category == objectNotFoundCategory {
			return true
		}
	}
	return false
}

// isInferError reports whether HubSpot rejected the request because it could not
// resolve the object type from a friendly name (custom objects need an id).
func (r batchResult) isInferError() bool {
	for _, rj := range r.rejections {
		if strings.Contains(strings.ToLower(rj.message), "infer object type") {
			return true
		}
	}
	return false
}

// effectiveObjectType returns the resolved objectTypeId for a name when one has
// been cached, otherwise the name unchanged.
func (d *HubSpotDestination) effectiveObjectType(name string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if id, ok := d.objectTypeIDs[name]; ok {
		return id
	}
	return name
}

// schemaObject is one entry from the CRM schemas API used to resolve a custom
// object's name (or objectTypeId) to its objectTypeId.
type schemaObject struct {
	ObjectTypeID       string `json:"objectTypeId"`
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName"`
}

// matches reports whether the schema entry is addressed by the given lowercased
// name, matching only on identifiers HubSpot guarantees unique: the objectTypeId,
// the internal name, and the fully-qualified name. Display labels (singular /
// plural) are deliberately not matched — HubSpot does not enforce them to be
// unique across schemas, so a label could resolve to an arbitrary object.
func (s schemaObject) matches(lname string) bool {
	return lname == strings.ToLower(s.ObjectTypeID) || lname == strings.ToLower(s.Name) ||
		lname == strings.ToLower(s.FullyQualifiedName)
}

// resolveMatchMode sets sh.searchMatch when update/delete match on a non-unique
// property (Search path); a lookup failure keeps the unique/batch path.
func (d *HubSpotDestination) resolveMatchMode(ctx context.Context, sh *shaper) error {
	if (!sh.updateOnly && !sh.archive) || sh.idProperty == "" || sh.idProperty == recordIDProperty {
		return nil
	}
	unique, err := d.propertyIsUnique(ctx, sh.objectType, sh.idProperty)
	if err != nil {
		config.Debug("[HUBSPOT DEST] could not determine uniqueness of %s.%s (%v); assuming unique", sh.objectType, sh.idProperty, err)
		return nil
	}
	sh.searchMatch = !unique
	if sh.searchMatch {
		config.Debug("[HUBSPOT DEST] %s is non-unique; matching via Search API", sh.idProperty)
	}
	return nil
}

// propertyIsUnique reports whether a property is defined with a unique value
// constraint (hasUniqueValue). Results are cached per object type + property.
func (d *HubSpotDestination) propertyIsUnique(ctx context.Context, objectType, property string) (bool, error) {
	cacheKey := objectType + "/" + property
	d.mu.Lock()
	if v, ok := d.propUnique[cacheKey]; ok {
		d.mu.Unlock()
		return v, nil
	}
	d.mu.Unlock()

	endpoint := fmt.Sprintf("/crm/v3/properties/%s/%s", url.PathEscape(d.effectiveObjectType(objectType)), url.PathEscape(property))
	resp, err := d.client.R(ctx).Get(endpoint)
	if err != nil {
		return false, err
	}
	if resp.StatusCode() != 200 && strings.Contains(strings.ToLower(resp.String()), "infer object type") {
		if _, ok := d.resolveObjectTypeID(ctx, objectType); ok {
			endpoint = fmt.Sprintf("/crm/v3/properties/%s/%s", url.PathEscape(d.effectiveObjectType(objectType)), url.PathEscape(property))
			resp, err = d.client.R(ctx).Get(endpoint)
			if err != nil {
				return false, err
			}
		}
	}
	if resp.StatusCode() != 200 {
		return false, fmt.Errorf("status %d", resp.StatusCode())
	}
	var body struct {
		HasUniqueValue bool `json:"hasUniqueValue"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return false, err
	}

	d.mu.Lock()
	d.propUnique[cacheKey] = body.HasUniqueValue
	d.mu.Unlock()
	return body.HasUniqueValue, nil
}

func (d *HubSpotDestination) fetchSchemas(ctx context.Context) ([]schemaObject, error) {
	resp, err := d.client.R(ctx).Get("/crm/v3/schemas")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode())
	}
	var body struct {
		Results []schemaObject `json:"results"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, err
	}
	return body.Results, nil
}

// resolveObjectTypeID looks up a custom object's objectTypeId by name via the
// schemas API and caches it. Returns ok=false if not found.
func (d *HubSpotDestination) resolveObjectTypeID(ctx context.Context, name string) (string, bool) {
	d.mu.Lock()
	if id, ok := d.objectTypeIDs[name]; ok {
		d.mu.Unlock()
		return id, true
	}
	d.mu.Unlock()

	schemas, err := d.fetchSchemas(ctx)
	if err != nil {
		return "", false
	}
	lname := strings.ToLower(name)
	for _, s := range schemas {
		if s.matches(lname) {
			d.mu.Lock()
			d.objectTypeIDs[name] = s.ObjectTypeID
			d.mu.Unlock()
			return s.ObjectTypeID, true
		}
	}
	return "", false
}

// resolveAssociationType pins down the association type and category once, before
// the writes start: a label name is resolved to its type id and category; a
// numeric type id given without a category has its real category looked up (a
// custom label is USER_/INTEGRATOR_DEFINED, so defaulting to HUBSPOT_DEFINED would
// make create/labels-archive match the wrong type).
func (d *HubSpotDestination) resolveAssociationType(ctx context.Context, sh *shaper) error {
	switch {
	case sh.associationLabel != "" && sh.associationType == 0:
		typeID, category, err := d.resolveAssociationLabel(ctx, sh.objectType, sh.associateTo, sh.associationLabel)
		if err != nil {
			return err
		}
		sh.associationType = typeID
		sh.associationCategory = category
	case sh.associationType != 0 && sh.associationCategory == "":
		category, err := d.resolveAssociationCategory(ctx, sh.objectType, sh.associateTo, sh.associationType)
		if err != nil {
			return err
		}
		sh.associationCategory = category
	}
	return nil
}

// associationTypes fetches the association type definitions (type id, category,
// label) for the from->to object pair via the v4 labels endpoint.
func (d *HubSpotDestination) associationTypes(ctx context.Context, from, to string) ([]associationTypeDef, error) {
	endpoint := fmt.Sprintf("/crm/v4/associations/%s/%s/labels",
		url.PathEscape(d.effectiveObjectType(from)), url.PathEscape(d.effectiveObjectType(to)))
	resp, err := d.client.R(ctx).Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("hubspot: failed to fetch association labels for %s->%s: %w", from, to, err)
	}
	// A non-2xx carries a JSON error body that would otherwise parse as an empty
	// (successful) result, silently dropping the label/category lookup — surface it.
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("hubspot: failed to fetch association labels for %s->%s: status %d: %s", from, to, resp.StatusCode(), resp.String())
	}
	var body struct {
		Results []associationTypeDef `json:"results"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("hubspot: failed to parse association labels for %s->%s: %w", from, to, err)
	}
	return body.Results, nil
}

type associationTypeDef struct {
	Category string `json:"category"`
	TypeID   int    `json:"typeId"`
	Label    string `json:"label"`
}

// resolveAssociationLabel looks up a label name's associationTypeId for the
// from->to object pair via the v4 labels endpoint.
func (d *HubSpotDestination) resolveAssociationLabel(ctx context.Context, from, to, label string) (int, string, error) {
	types, err := d.associationTypes(ctx, from, to)
	if err != nil {
		return 0, "", err
	}
	var available []string
	for _, r := range types {
		if strings.EqualFold(strings.TrimSpace(r.Label), strings.TrimSpace(label)) {
			category := r.Category
			if category == "" {
				category = "USER_DEFINED"
			}
			return r.TypeID, category, nil
		}
		if r.Label != "" {
			available = append(available, r.Label)
		}
	}
	return 0, "", fmt.Errorf("hubspot: association label %q not found for %s->%s (available: %s)", label, from, to, strings.Join(available, ", "))
}

// resolveAssociationCategory finds the category for a numeric association type id.
// The type list may omit the built-in unlabeled types, so an unmatched id falls
// back to HUBSPOT_DEFINED (the category those built-ins use).
func (d *HubSpotDestination) resolveAssociationCategory(ctx context.Context, from, to string, typeID int) (string, error) {
	types, err := d.associationTypes(ctx, from, to)
	if err != nil {
		return "", err
	}
	for _, r := range types {
		if r.TypeID == typeID && r.Category != "" {
			return r.Category, nil
		}
	}
	return "HUBSPOT_DEFINED", nil
}

// postBatch posts one chunk to the batch action endpoint, resolving a custom
// object name to its objectTypeId and retrying once on an infer error.
func (d *HubSpotDestination) postBatch(ctx context.Context, sh *shaper, items []batchInput, action string) (batchResult, error) {
	res, err := d.doPostBatch(ctx, sh.objectType, items, action)
	if err != nil {
		return batchResult{}, err
	}
	if res.isInferError() {
		if _, ok := d.resolveObjectTypeID(ctx, sh.objectType); ok {
			return d.doPostBatch(ctx, sh.objectType, items, action)
		}
	}
	return res, nil
}

func (d *HubSpotDestination) doPostBatch(ctx context.Context, objectType string, items []batchInput, action string) (batchResult, error) {
	endpoint := fmt.Sprintf("/crm/v3/objects/%s/batch/%s", url.PathEscape(d.effectiveObjectType(objectType)), action)
	req := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": items})
	// "create" is the only non-idempotent action (update/upsert match by id or a
	// unique property). Retrying it after a server-side commit would duplicate
	// records, so only retry it on a 429 (throttled, never processed).
	if action == "create" {
		req = req.SetRetryOnRateLimitOnly()
	}
	resp, err := req.Post(endpoint)
	if err != nil {
		return batchResult{}, fmt.Errorf("hubspot %s request failed: %w", action, err)
	}
	return parseBatchResponse(resp), nil
}

// send posts one chunk to a batch action endpoint. A whole-batch 400/409 (HubSpot
// batches aren't atomic) is bisected so valid rows still land; rejects follow reject-mode.
func (d *HubSpotDestination) send(ctx context.Context, sh *shaper, items []batchInput, action string, rejects *rejectionLog) error {
	res, err := d.postBatch(ctx, sh, items, action)
	if err != nil {
		return err
	}
	// A mirror must not archive records it just created (keyless rows have no match
	// value to record in `seen`), so remember the ids HubSpot assigned this run.
	if sh.created != nil && action == "create" {
		for _, id := range res.createdIDs {
			sh.created.Store(id, struct{}{})
		}
	}
	// A structural failure (e.g. upsert on a non-unique id_property) fails every
	// row identically — abort instead of bisecting into per-record rejects that
	// would look like success under --reject-mode=skip.
	if err := systemicBatchError(action, sh.objectType, res); err != nil {
		return err
	}
	if !res.ok && isRecordLevelStatus(res.status) && len(items) > 1 {
		mid := len(items) / 2
		if err := d.send(ctx, sh, items[:mid], action, rejects); err != nil {
			return err
		}
		return d.send(ctx, sh, items[mid:], action, rejects)
	}
	return d.handleBatchResult(ctx, sh, res, items, action, rejects)
}

// handleBatchResult aborts, warns, or records rejections from a batch response.
func (d *HubSpotDestination) handleBatchResult(ctx context.Context, sh *shaper, res batchResult, items []batchInput, action string, rejects *rejectionLog) error {
	if len(res.rejections) == 0 {
		return nil
	}
	// --reject-mode=skip only tolerates record-level rejections; auth/rate/server
	// failures abort so an entire dropped batch never looks like success.
	if (!res.ok && !isRecordLevelStatus(res.status)) || sh.failFast() {
		hint := ""
		if res.category == "CONFLICT" || res.status == 409 {
			hint = "; set id_property=<property> on the dest-table to upsert existing records instead of creating them"
			if unique := d.listUniqueProperties(ctx, sh.objectType); len(unique) > 0 {
				hint = fmt.Sprintf("; set id_property=<property> to upsert existing records instead of creating them (unique properties on %s: %s)", sh.objectType, strings.Join(unique, ", "))
			}
		}
		return fmt.Errorf("hubspot %s %s returned status %d: %s%s", action, sh.objectType, res.status, res.rejections[0].message, hint)
	}

	output.Warnf("Warning: hubspot rejected %d of %d %s record(s) in this batch; first error: %s\n", len(res.rejections), len(items), sh.objectType, res.rejections[0].message)
	rejects.add(annotate(res.rejections, items))
	return nil
}

// sendUpdate posts an update chunk; since batch update fails atomically on any
// missing id, it re-partitions into existing ids (update) and missing ids (create).
func (d *HubSpotDestination) sendUpdate(ctx context.Context, sh *shaper, items []batchInput, rejects *rejectionLog) error {
	res, err := d.postBatch(ctx, sh, items, "update")
	if err != nil {
		return err
	}
	if err := systemicBatchError("update", sh.objectType, res); err != nil {
		return err
	}
	if !res.hasNotFound() {
		// A validation 400 rejects the whole batch, so bisect to isolate the bad
		// row(s) and land the valid remainder (HubSpot batches aren't atomic).
		if !res.ok && isRecordLevelStatus(res.status) && len(items) > 1 {
			mid := len(items) / 2
			if err := d.sendUpdate(ctx, sh, items[:mid], rejects); err != nil {
				return err
			}
			return d.sendUpdate(ctx, sh, items[mid:], rejects)
		}
		return d.handleBatchResult(ctx, sh, res, items, "update", rejects)
	}

	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	existing, err := d.partitionExisting(ctx, sh.objectType, ids)
	if err != nil {
		// Fall back to the original not-found rejection if existence can't be resolved.
		return d.handleBatchResult(ctx, sh, res, items, "update", rejects)
	}

	var updates, creates []batchInput
	for _, it := range items {
		if existing[it.ID] {
			updates = append(updates, it)
			continue
		}
		it.ID = ""
		creates = append(creates, it)
	}
	if len(updates) > 0 {
		// Route confirmed-existing ids through the not-found-tolerant update path:
		// if one is archived by another actor between the existence read and this
		// re-send (TOCTOU), the resulting 404 must become a per-record reject under
		// --reject-mode=skip, not a fatal abort (a generic send treats 404 as
		// systemic since it is not a record-level 400/409).
		if err := d.sendUpdateOnly(ctx, sh, updates, rejects); err != nil {
			return err
		}
	}
	if len(creates) > 0 {
		if err := d.send(ctx, sh, creates, "create", rejects); err != nil {
			return err
		}
	}
	return nil
}

// sendUpdateOnly posts an update chunk for the update-only strategy. Batch update
// fails atomically on any missing id, so the chunk is bisected to land valid rows
// while missing ids become rejections honoring reject-mode (never re-routed to create).
func (d *HubSpotDestination) sendUpdateOnly(ctx context.Context, sh *shaper, items []batchInput, rejects *rejectionLog) error {
	res, err := d.postBatch(ctx, sh, items, "update")
	if err != nil {
		return err
	}
	if err := systemicBatchError("update", sh.objectType, res); err != nil {
		return err
	}
	// A missing id or a validation error rejects the whole batch atomically, so
	// bisect to isolate the bad row(s) and land the valid remainder.
	if (res.hasNotFound() || (!res.ok && isRecordLevelStatus(res.status))) && len(items) > 1 {
		mid := len(items) / 2
		if err := d.sendUpdateOnly(ctx, sh, items[:mid], rejects); err != nil {
			return err
		}
		return d.sendUpdateOnly(ctx, sh, items[mid:], rejects)
	}
	if !res.hasNotFound() {
		return d.handleBatchResult(ctx, sh, res, items, "update", rejects)
	}
	if sh.failFast() {
		return fmt.Errorf("hubspot update %s: record %q not found: %s", sh.objectType, items[0].ID, res.rejections[0].message)
	}
	output.Warnf("Warning: hubspot rejected %d of %d %s record(s) in this batch; first error: %s\n", len(res.rejections), len(items), sh.objectType, res.rejections[0].message)
	rejects.add(annotate(res.rejections, items))
	return nil
}

// partitionExisting returns which ids exist in HubSpot. Batch read fails
// atomically on any missing id, so the set is bisected to isolate the missing ones.
func (d *HubSpotDestination) partitionExisting(ctx context.Context, objectType string, ids []string) (map[string]bool, error) {
	existing := make(map[string]bool, len(ids))
	var recurse func(sub []string) error
	recurse = func(sub []string) error {
		if len(sub) == 0 {
			return nil
		}
		found, err := d.batchReadFound(ctx, objectType, sub)
		if err != nil {
			return err
		}
		if found == len(sub) {
			for _, id := range sub {
				existing[id] = true
			}
			return nil
		}
		if len(sub) == 1 {
			return nil // the single id is missing
		}
		mid := len(sub) / 2
		if err := recurse(sub[:mid]); err != nil {
			return err
		}
		return recurse(sub[mid:])
	}
	if err := recurse(ids); err != nil {
		return nil, err
	}
	return existing, nil
}

// isRecordsAbsent404 reports whether a batch/read 404 body is HubSpot's
// "requested records don't exist" response (category OBJECT_NOT_FOUND) rather than
// a 404 from a misconfigured object type or endpoint. Only the former may be
// treated as "none found"; the latter must surface as a hard error. The structured
// `category` field is matched exactly — a substring scan of the raw body would
// also fire if OBJECT_NOT_FOUND appeared in some other field (e.g. a context blob).
func isRecordsAbsent404(bodyText string) bool {
	var body struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(bodyText), &body); err != nil {
		return false
	}
	return body.Category == objectNotFoundCategory
}

// batchReadFound reports how many of the given ids exist via batch read, which
// returns all records when they exist or OBJECT_NOT_FOUND when any is missing.
func (d *HubSpotDestination) batchReadFound(ctx context.Context, objectType string, ids []string) (int, error) {
	inputs := make([]map[string]string, len(ids))
	for i, id := range ids {
		inputs[i] = map[string]string{"id": id}
	}
	endpoint := fmt.Sprintf("/crm/v3/objects/%s/batch/read", url.PathEscape(d.effectiveObjectType(objectType)))
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": inputs, "properties": []string{recordIDProperty}, "archived": false}).Post(endpoint)
	if err != nil {
		return 0, err
	}
	// A batch where every id is missing can come back 404 with category
	// OBJECT_NOT_FOUND (or 207 with an empty results set); both mean "none found",
	// not a hard error — so the merge re-route can still create them. A 404 for any
	// other reason (unknown object type, wrong endpoint) must still surface.
	if resp.StatusCode() == 404 {
		if !isRecordsAbsent404(resp.String()) {
			return 0, fmt.Errorf("status 404: %s", resp.String())
		}
		return 0, nil
	}
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 {
		return 0, fmt.Errorf("status %d", resp.StatusCode())
	}
	var body struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return 0, err
	}
	return len(body.Results), nil
}

// columnValues collects the non-null string values of a column for business-key
// resolution.
func columnValues(arr arrow.Array) []string {
	values := make([]string, 0, arr.Len())
	for i := 0; i < arr.Len(); i++ {
		if v, ok := propertyValue(arr, i); ok && v != "" {
			values = append(values, v)
		}
	}
	return values
}

// resolveAssociationSide maps each business-key value to every matching record id
// for one side of an association. A non-unique property is resolved via Search (so
// a shared key links all its records); a unique one via batch read (one id). When
// uniqueness can't be determined, it assumes unique — the same fallback as the
// update/delete match path.
func (d *HubSpotDestination) resolveAssociationSide(ctx context.Context, objectType, property string, values []string) (map[string][]string, error) {
	unique, err := d.propertyIsUnique(ctx, objectType, property)
	if err != nil {
		config.Debug("[HUBSPOT DEST] could not determine uniqueness of %s.%s (%v); assuming unique", objectType, property, err)
		unique = true
	}
	if !unique {
		return d.searchIDsByProperty(ctx, objectType, property, values)
	}
	single, err := d.resolveKeysToIDs(ctx, objectType, property, values)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(single))
	for k, v := range single {
		out[k] = []string{v}
	}
	return out, nil
}

// resolveKeysToIDs maps business-key values to record ids via batch read, which
// fails atomically on any missing key, so the set is bisected to drop missing ones.
func (d *HubSpotDestination) resolveKeysToIDs(ctx context.Context, objectType, idProperty string, keys []string) (map[string]string, error) {
	seen := make(map[string]bool, len(keys))
	uniq := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" && !seen[k] {
			seen[k] = true
			uniq = append(uniq, k)
		}
	}
	out := make(map[string]string, len(uniq))
	var recurse func(sub []string) error
	recurse = func(sub []string) error {
		if len(sub) == 0 {
			return nil
		}
		found, err := d.batchReadByProperty(ctx, objectType, idProperty, sub)
		if err != nil {
			return err
		}
		for k, v := range found {
			out[k] = v
		}
		// found is keyed by matchKey (HubSpot may normalize the stored value), so
		// compare each source key through matchKey — otherwise a mixed-case key that
		// did resolve looks unresolved and triggers needless bisected re-reads.
		var remaining []string
		for _, k := range sub {
			if _, ok := found[matchKey(k)]; !ok {
				remaining = append(remaining, k)
			}
		}
		if len(remaining) <= 1 {
			return nil // no remaining, or the single remaining key is missing
		}
		mid := len(remaining) / 2
		if err := recurse(remaining[:mid]); err != nil {
			return err
		}
		return recurse(remaining[mid:])
	}
	// batch/read caps inputs at 100, so chunk before matching; recurse still
	// bisects each chunk to drop missing keys.
	const readValueChunk = 100
	for start := 0; start < len(uniq); start += readValueChunk {
		end := min(start+readValueChunk, len(uniq))
		if err := recurse(uniq[start:end]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// matchKey normalizes a business-key value for resolve-map lookups. HubSpot
// normalizes some property values it stores and returns (e.g. it lowercases
// email), so a resolve map keyed on the returned value would miss when the caller
// looks up the original source value. Folding case and surrounding whitespace on
// both the map key and the lookup keeps them aligned. (Genuinely lossy
// normalizations, such as phone formatting, still can't be correlated back.)
func matchKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// batchReadByProperty reads records matched on a non-id property and returns a
// map of the property value to the record id for those that exist.
func (d *HubSpotDestination) batchReadByProperty(ctx context.Context, objectType, idProperty string, keys []string) (map[string]string, error) {
	inputs := make([]map[string]string, len(keys))
	for i, k := range keys {
		inputs[i] = map[string]string{"id": k}
	}
	body := map[string]interface{}{"idProperty": idProperty, "inputs": inputs, "properties": []string{idProperty}, "archived": false}
	endpoint := fmt.Sprintf("/crm/v3/objects/%s/batch/read", url.PathEscape(d.effectiveObjectType(objectType)))
	resp, err := d.client.R(ctx).SetBody(body).Post(endpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 && strings.Contains(strings.ToLower(resp.String()), "infer object type") {
		if _, ok := d.resolveObjectTypeID(ctx, objectType); ok {
			endpoint = fmt.Sprintf("/crm/v3/objects/%s/batch/read", url.PathEscape(d.effectiveObjectType(objectType)))
			resp, err = d.client.R(ctx).SetBody(body).Post(endpoint)
			if err != nil {
				return nil, err
			}
		}
	}
	// A chunk whose keys are all absent from HubSpot comes back 404 with category
	// OBJECT_NOT_FOUND — that means "none found", not a hard error, so callers can
	// reject the missing keys per --reject-mode instead of aborting the whole run.
	// A 404 for any other reason (unknown object type, wrong endpoint) is a real
	// misconfiguration and must surface, or --reject-mode=skip would report a
	// hollow success having written nothing (matches batchReadFound).
	if resp.StatusCode() == 404 {
		if !isRecordsAbsent404(resp.String()) {
			return nil, fmt.Errorf("status 404: %s", resp.String())
		}
		return map[string]string{}, nil
	}
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 {
		return nil, fmt.Errorf("status %d", resp.StatusCode())
	}
	var parsed struct {
		Results []struct {
			ID         string            `json:"id"`
			Properties map[string]string `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(parsed.Results))
	for _, r := range parsed.Results {
		if v, ok := r.Properties[idProperty]; ok {
			out[matchKey(v)] = r.ID
		}
	}
	return out, nil
}

// searchIDsByProperty maps each value to ALL record ids whose property equals it
// (CRM Search, chunked IN filter, paginated) — for non-unique matches.
func (d *HubSpotDestination) searchIDsByProperty(ctx context.Context, objectType, property string, values []string) (map[string][]string, error) {
	seen := make(map[string]bool, len(values))
	uniq := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			uniq = append(uniq, v)
		}
	}

	out := make(map[string][]string, len(uniq))
	const searchValueChunk = 100
	for start := 0; start < len(uniq); start += searchValueChunk {
		end := min(start+searchValueChunk, len(uniq))
		if err := d.searchChunk(ctx, objectType, property, uniq[start:end], out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// searchChunk runs one paginated Search request for a chunk of values, appending
// each hit's record id to out under its property value.
func (d *HubSpotDestination) searchChunk(ctx context.Context, objectType, property string, values []string, out map[string][]string) error {
	after := ""
	for {
		filter := map[string]interface{}{"propertyName": property, "operator": "IN", "values": values}
		body := map[string]interface{}{
			"filterGroups": []map[string]interface{}{{"filters": []map[string]interface{}{filter}}},
			"properties":   []string{property},
			"limit":        100,
		}
		if after != "" {
			body["after"] = after
		}
		endpoint := fmt.Sprintf("/crm/v3/objects/%s/search", url.PathEscape(d.effectiveObjectType(objectType)))
		resp, err := d.client.R(ctx).SetBody(body).Post(endpoint)
		if err != nil {
			return err
		}
		if resp.StatusCode() != 200 && strings.Contains(strings.ToLower(resp.String()), "infer object type") {
			if _, ok := d.resolveObjectTypeID(ctx, objectType); ok {
				endpoint = fmt.Sprintf("/crm/v3/objects/%s/search", url.PathEscape(d.effectiveObjectType(objectType)))
				resp, err = d.client.R(ctx).SetBody(body).Post(endpoint)
				if err != nil {
					return err
				}
			}
		}
		if resp.StatusCode() != 200 {
			return fmt.Errorf("hubspot search %s returned status %d: %s", objectType, resp.StatusCode(), resp.String())
		}

		var parsed struct {
			Results []struct {
				ID         string            `json:"id"`
				Properties map[string]string `json:"properties"`
			} `json:"results"`
			Paging struct {
				Next struct {
					After string `json:"after"`
				} `json:"next"`
			} `json:"paging"`
		}
		if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
			return err
		}
		for _, r := range parsed.Results {
			if v, ok := r.Properties[property]; ok && r.ID != "" {
				k := matchKey(v)
				// Case-variant source values collapse to one bucket, and pagination
				// can repeat a hit; don't queue the same record id twice.
				if slices.Contains(out[k], r.ID) {
					continue
				}
				out[k] = append(out[k], r.ID)
			}
		}

		after = parsed.Paging.Next.After
		if after == "" {
			return nil
		}
	}
}

// writeSearchUpdateBatch updates records matched on a non-unique property: Search
// resolves each value to all matching ids; no match is a not-found reject.
func (d *HubSpotDestination) writeSearchUpdateBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	idIdx := colIndex[sh.idColumn]
	resolve, err := d.searchIDsByProperty(ctx, sh.objectType, sh.idProperty, columnValues(record.Column(idIdx)))
	if err != nil {
		return 0, err
	}

	rows := int(record.NumRows())
	batch := make([]batchInput, 0, min(rows, batchLimit))
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// Matched records are updated by their record id (no idProperty).
		// sendUpdateOnly tolerates a record deleted between Search and update
		// (TOCTOU 404) as a per-record not-found reject under --reject-mode,
		// rather than aborting the whole run like generic send would.
		if err := d.sendUpdateOnly(ctx, sh, batch, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for row := 0; row < rows; row++ {
		item, _, ok := sh.shapeRow(record, colIndex, row)
		if !ok {
			skipped.Add(1)
			continue
		}
		val, _ := propertyValue(record.Column(idIdx), row)
		ids := resolve[matchKey(val)]
		if len(ids) == 0 {
			if sh.failFast() {
				return written, fmt.Errorf("hubspot: no %s found with %s=%q", sh.objectType, sh.idProperty, val)
			}
			rejects.add([]rejection{{category: objectNotFoundCategory, message: fmt.Sprintf("no %s found with %s=%q", sh.objectType, sh.idProperty, val)}})
			continue
		}
		for _, id := range ids {
			batch = append(batch, batchInput{ID: id, Properties: item.Properties})
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

// writeArchiveBatch soft-deletes records in chunks of batchLimit. Ids come from
// the id column directly, or are resolved from a unique/non-unique property first.
func (d *HubSpotDestination) writeArchiveBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	idIdx, ok := colIndex[sh.idColumn]
	if !ok {
		return 0, fmt.Errorf("hubspot: id column %q not found in source for archive", sh.idColumn)
	}

	// Resolve match values to record ids: non-unique (searchMatch) via Search
	// (many per value), unique via batch read (one); hs_object_id/empty needs none.
	var resolve map[string][]string
	if sh.searchMatch {
		var err error
		resolve, err = d.searchIDsByProperty(ctx, sh.objectType, sh.idProperty, columnValues(record.Column(idIdx)))
		if err != nil {
			return 0, err
		}
	} else if sh.idProperty != "" && sh.idProperty != recordIDProperty {
		single, err := d.resolveKeysToIDs(ctx, sh.objectType, sh.idProperty, columnValues(record.Column(idIdx)))
		if err != nil {
			return 0, err
		}
		resolve = make(map[string][]string, len(single))
		for k, v := range single {
			resolve[k] = []string{v}
		}
	}

	rows := int(record.NumRows())
	batch := make([]string, 0, min(rows, batchLimit))
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := d.sendArchive(ctx, sh, batch, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for row := 0; row < rows; row++ {
		val, ok := propertyValue(record.Column(idIdx), row)
		if !ok || val == "" {
			skipped.Add(1)
			continue
		}
		ids := []string{val}
		if resolve != nil {
			found, ok := resolve[matchKey(val)]
			if !ok || len(found) == 0 {
				// Value present but no record matched: a not-found reject, per --reject-mode.
				if sh.failFast() {
					return written, fmt.Errorf("hubspot: no %s found with %s=%q to archive", sh.objectType, sh.idProperty, val)
				}
				rejects.add([]rejection{{category: objectNotFoundCategory, message: fmt.Sprintf("no %s found with %s=%q to archive", sh.objectType, sh.idProperty, val)}})
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

// sendArchive posts one chunk to the object batch archive endpoint, resolving a
// custom object name to its id on an infer error.
func (d *HubSpotDestination) sendArchive(ctx context.Context, sh *shaper, ids []string, rejects *rejectionLog) error {
	res, err := d.postArchive(ctx, sh.objectType, ids)
	if err != nil {
		return err
	}
	if res.isInferError() {
		if _, ok := d.resolveObjectTypeID(ctx, sh.objectType); ok {
			res, err = d.postArchive(ctx, sh.objectType, ids)
			if err != nil {
				return err
			}
		}
	}
	if err := systemicBatchError("archive", sh.objectType, res); err != nil {
		return err
	}
	// A whole-batch 400 rejects every id atomically, so bisect to isolate the bad
	// id(s) and land the valid remainder (HubSpot batches aren't atomic).
	if !res.ok && isRecordLevelStatus(res.status) && len(ids) > 1 {
		mid := len(ids) / 2
		if err := d.sendArchive(ctx, sh, ids[:mid], rejects); err != nil {
			return err
		}
		return d.sendArchive(ctx, sh, ids[mid:], rejects)
	}
	if len(res.rejections) == 0 {
		return nil
	}
	if (!res.ok && !isRecordLevelStatus(res.status)) || sh.failFast() {
		return fmt.Errorf("hubspot archive %s returned status %d: %s", sh.objectType, res.status, res.rejections[0].message)
	}
	output.Warnf("Warning: hubspot rejected %d of %d %s archive(s) in this batch; first error: %s\n", len(res.rejections), len(ids), sh.objectType, res.rejections[0].message)
	archiveItems := make([]batchInput, len(ids))
	for i, id := range ids {
		archiveItems[i] = batchInput{IDProperty: recordIDProperty, ID: id}
	}
	rejects.add(annotate(res.rejections, archiveItems))
	return nil
}

func (d *HubSpotDestination) postArchive(ctx context.Context, objectType string, ids []string) (batchResult, error) {
	inputs := make([]map[string]string, len(ids))
	for i, id := range ids {
		inputs[i] = map[string]string{"id": id}
	}
	endpoint := fmt.Sprintf("/crm/v3/objects/%s/batch/archive", url.PathEscape(d.effectiveObjectType(objectType)))
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": inputs}).Post(endpoint)
	if err != nil {
		return batchResult{}, fmt.Errorf("hubspot archive request failed: %w", err)
	}
	return parseBatchResponse(resp), nil
}

// writeAssociationBatch links records in chunks of batchLimit.
func (d *HubSpotDestination) writeAssociationBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	if err := sh.validateAssociationColumns(colIndex); err != nil {
		return 0, err
	}

	// When a side matches on a business key, resolve that column's values to record
	// ids once per batch before linking. A non-unique property resolves via Search
	// (every matching id, so the row links all of them); a unique one via batch read.
	var fromResolve, toResolve map[string][]string
	if sh.fromProperty != "" {
		var err error
		fromResolve, err = d.resolveAssociationSide(ctx, sh.objectType, sh.fromProperty, allCellValues(record.Column(colIndex[sh.fromColumn])))
		if err != nil {
			return 0, err
		}
	}
	if sh.toProperty != "" {
		var err error
		toResolve, err = d.resolveAssociationSide(ctx, sh.associateTo, sh.toProperty, allCellValues(record.Column(colIndex[sh.toColumn])))
		if err != nil {
			return 0, err
		}
	}

	rows := int(record.NumRows())
	batch := make([]associationInput, 0, min(rows, batchLimit))
	var written int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := d.sendAssociations(ctx, sh, batch, rejects); err != nil {
			return err
		}
		written += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for row := 0; row < rows; row++ {
		items, mirrorFroms, unresolved := sh.shapeAssociation(record, colIndex, row, fromResolve, toResolve)
		if len(unresolved) > 0 {
			if sh.failFast() {
				return written, fmt.Errorf("hubspot: %s", unresolved[0].message)
			}
			rejects.add(unresolved)
		}
		// Mirror rows whose From resolved but whose To cell was empty register the
		// From with an empty desired set so finalization unlinks all its links.
		if sh.mirror && sh.seenLinks != nil {
			for _, from := range mirrorFroms {
				sh.seenLinks.LoadOrStore(from, &sync.Map{})
			}
		}
		if len(items) == 0 {
			// Only a genuinely empty match cell is a skip; an unresolved key was
			// already recorded as a reject, and a mirror-clear did real work.
			if len(unresolved) == 0 && len(mirrorFroms) == 0 {
				skipped.Add(1)
			}
			continue
		}
		for _, item := range items {
			if sh.mirror && sh.seenLinks != nil {
				set, _ := sh.seenLinks.LoadOrStore(item.From.ID, &sync.Map{})
				set.(*sync.Map).Store(item.To.ID, struct{}{})
			}
			batch = append(batch, item)
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

// sendAssociations posts one chunk to the v4 batch association endpoint.
func (d *HubSpotDestination) sendAssociations(ctx context.Context, sh *shaper, items []associationInput, rejects *rejectionLog) error {
	verb := "associate/default"
	switch {
	case sh.archive && sh.associationType != 0:
		// Remove only the specified label, not every association type between the
		// pair — labels/archive takes the same {from,to,types} shape as create.
		verb = "labels/archive"
	case sh.archive:
		verb = "archive"
	case sh.associationType != 0:
		verb = "create"
	}
	res, err := d.postAssociations(ctx, sh.objectType, sh.associateTo, verb, items)
	if err != nil {
		return err
	}
	// Custom object names on either side may need resolving to their ids. Only
	// retry if at least one side actually resolved, otherwise the re-POST would
	// hit the identical infer error.
	if res.isInferError() {
		_, okFrom := d.resolveObjectTypeID(ctx, sh.objectType)
		_, okTo := d.resolveObjectTypeID(ctx, sh.associateTo)
		if okFrom || okTo {
			res, err = d.postAssociations(ctx, sh.objectType, sh.associateTo, verb, items)
			if err != nil {
				return err
			}
		}
	}
	if err := systemicBatchError(verb, sh.objectType+"->"+sh.associateTo, res); err != nil {
		return err
	}
	// A whole-batch 400 rejects every link atomically, so bisect to isolate the bad
	// row(s) and land the valid remainder (HubSpot batches aren't atomic).
	if !res.ok && isRecordLevelStatus(res.status) && len(items) > 1 {
		mid := len(items) / 2
		if err := d.sendAssociations(ctx, sh, items[:mid], rejects); err != nil {
			return err
		}
		return d.sendAssociations(ctx, sh, items[mid:], rejects)
	}
	if len(res.rejections) == 0 {
		return nil
	}
	pair := fmt.Sprintf("%s->%s", sh.objectType, sh.associateTo)
	if (!res.ok && !isRecordLevelStatus(res.status)) || sh.failFast() {
		return fmt.Errorf("hubspot %s associate returned status %d: %s", pair, res.status, res.rejections[0].message)
	}

	output.Warnf("Warning: hubspot rejected %d of %d %s association(s) in this batch; first error: %s\n", len(res.rejections), len(items), pair, res.rejections[0].message)
	rejects.add(res.rejections)
	return nil
}

// postAssociations posts one chunk to the v4 batch association endpoint,
// resolving custom object names to their ids via the cache.
func (d *HubSpotDestination) postAssociations(ctx context.Context, from, to, verb string, items []associationInput) (batchResult, error) {
	endpoint := fmt.Sprintf("/crm/v4/associations/%s/%s/batch/%s", url.PathEscape(d.effectiveObjectType(from)), url.PathEscape(d.effectiveObjectType(to)), verb)
	var body interface{}
	if verb == "archive" {
		// Unlabeled archive removes every association type between the pair and takes
		// "to" as an array. Labeled removal (labels/archive) and create both take the
		// {from,to,types} associationInput shape directly, handled by the else branch.
		inputs := make([]associationArchiveInput, len(items))
		for i, it := range items {
			inputs[i] = associationArchiveInput{From: it.From, To: []associationRef{it.To}}
		}
		body = map[string]interface{}{"inputs": inputs}
	} else {
		body = map[string]interface{}{"inputs": items}
	}
	resp, err := d.client.R(ctx).SetBody(body).Post(endpoint)
	if err != nil {
		return batchResult{}, fmt.Errorf("hubspot associate request failed: %w", err)
	}
	return parseBatchResponse(resp), nil
}

// maxReportedRejections caps how many rejected records are listed in the final
// report so a large failure does not produce an unbounded message.
const maxReportedRejections = 100

// rejection is one error HubSpot returned for a batch.
type rejection struct {
	category   string
	message    string
	context    json.RawMessage
	identifier string // which record, e.g. "email=a@x.com" (best-effort)
}

// key names the record for a reject line, e.g. "email=a@x.com". Empty for a
// create (append) row, which carries no match value.
func (in batchInput) key() string {
	if in.ID == "" {
		return ""
	}
	prop := in.IDProperty
	if prop == "" {
		prop = "id"
	}
	return prop + "=" + in.ID
}

// annotate attaches the offending record's key to each rejection when the
// mapping is unambiguous: one rejection per item, or a single-item (bisected)
// chunk where every rejection belongs to that one record.
func annotate(rejs []rejection, items []batchInput) []rejection {
	switch {
	case len(rejs) == len(items):
		for i := range rejs {
			rejs[i].identifier = items[i].key()
		}
	case len(items) == 1:
		for i := range rejs {
			rejs[i].identifier = items[0].key()
		}
	}
	return rejs
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
	fmt.Fprintf(&b, "hubspot rejected %d %s record(s):", len(items), sh.objectType)
	shown := min(len(items), maxReportedRejections)
	for i := 0; i < shown; i++ {
		id := ""
		if items[i].identifier != "" {
			id = " [" + items[i].identifier + "]"
		}
		fmt.Fprintf(&b, "\n  - (%s)%s %s: %s", items[i].category, id, items[i].message, string(items[i].context))
	}
	if len(items) > shown {
		fmt.Fprintf(&b, "\n  ... and %d more", len(items)-shown)
	}
	// A conflict means the record already exists; nudge toward upsert.
	for _, it := range items {
		if it.category == "CONFLICT" {
			b.WriteString("\n  hint: set id_property=<property> on the dest-table to upsert existing records instead of creating them")
			break
		}
	}

	if sh.skip() {
		// Held back so it prints after the run summary, not buried mid-run above
		// the metrics table (fail mode already surfaces last, as the returned error).
		output.Deferf("Warning: %s\n", b.String())
		return nil
	}
	return errors.New(b.String())
}

// PrepareTable validates every source column maps to a real HubSpot property
// before any record is sent, so a schema mismatch fails fast with a clear error.
func (d *HubSpotDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Schema == nil {
		return nil
	}
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), "", false)
	if err != nil {
		return err
	}
	// Association and archive modes write no properties, so there is nothing to
	// validate here.
	if sh.associate() || sh.archive {
		return nil
	}

	// Create-only load: if the object has unique properties the caller could have
	// matched on, surface them so they can opt into upsert instead of duplicating.
	if !sh.matchesRecords() {
		if unique := d.listUniqueProperties(ctx, sh.objectType); len(unique) > 0 {
			output.Warnf("Warning: hubspot %s records will be created (no id_property set); to update existing records set id_property=<property> (unique properties available: %s)\n", sh.objectType, strings.Join(unique, ", "))
		}
	}

	// Property names each row carries: source columns minus the id column and
	// decorations, plus the upsert id_property (update needs no property validation).
	candidates := make([]string, 0, len(opts.Schema.Columns))
	for _, col := range opts.Schema.Columns {
		if col.Name == sh.idColumn || sh.exclude[col.Name] {
			continue
		}
		candidates = append(candidates, col.Name)
	}
	checkNames := candidates
	if sh.upsert() {
		checkNames = append(append([]string{}, candidates...), sh.idProperty)
	}
	if len(checkNames) == 0 {
		return nil
	}

	existing, err := d.checkProperties(ctx, sh.objectType, checkNames)
	if err != nil {
		// Validation is best-effort: a missing scope or custom object without a
		// readable schema should not block an otherwise valid load.
		output.Warnf("Warning: hubspot could not verify %s properties (%v); skipping column validation\n", sh.objectType, err)
		return nil
	}

	var unknown []string
	for _, name := range candidates {
		if !existing[name] {
			unknown = append(unknown, name)
		}
	}
	if sh.upsert() && !existing[sh.idProperty] {
		unknown = append(unknown, sh.idProperty+" (id_property)")
	}
	if len(unknown) > 0 {
		return fmt.Errorf("hubspot: source columns are not properties on %q: [%s]; create them in HubSpot or remove them from the source and re-run", sh.objectType, strings.Join(unknown, ", "))
	}
	// Whether id_property is actually upsertable is left to HubSpot: its
	// hasUniqueValue flag is unreliable, so a bad key surfaces as a clear write error.
	return nil
}

// checkProperties reports which requested names exist as properties on an object
// type, via the batch read endpoint.
func (d *HubSpotDestination) checkProperties(ctx context.Context, objectType string, names []string) (map[string]bool, error) {
	// batch/read caps inputs at 100, so a source with >100 columns must be chunked
	// or the whole validation 4xx's and is skipped, deferring missing properties to
	// per-row rejects at write time.
	const propReadChunk = 100
	existing := make(map[string]bool, len(names))
	for start := 0; start < len(names); start += propReadChunk {
		end := min(start+propReadChunk, len(names))
		found, err := d.checkPropertiesChunk(ctx, objectType, names[start:end])
		if err != nil {
			return nil, err
		}
		for n := range found {
			existing[n] = true
		}
	}
	return existing, nil
}

func (d *HubSpotDestination) checkPropertiesChunk(ctx context.Context, objectType string, names []string) (map[string]bool, error) {
	inputs := make([]map[string]string, len(names))
	for i, n := range names {
		inputs[i] = map[string]string{"name": n}
	}
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": inputs, "archived": false}).
		Post(fmt.Sprintf("/crm/v3/properties/%s/batch/read", url.PathEscape(d.effectiveObjectType(objectType))))
	if err != nil {
		return nil, err
	}
	// A custom object name can't be inferred here either; resolve to its id and retry.
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 && strings.Contains(strings.ToLower(resp.String()), "infer object type") {
		if _, ok := d.resolveObjectTypeID(ctx, objectType); ok {
			resp, err = d.client.R(ctx).SetBody(map[string]interface{}{"inputs": inputs, "archived": false}).
				Post(fmt.Sprintf("/crm/v3/properties/%s/batch/read", url.PathEscape(d.effectiveObjectType(objectType))))
			if err != nil {
				return nil, err
			}
		}
	}
	// 200 = all found, 207 = some names missing; both return the resolved
	// properties in results[], so unknown names are those absent from it.
	if resp.StatusCode() != 200 && resp.StatusCode() != 207 {
		return nil, fmt.Errorf("status %d", resp.StatusCode())
	}
	var body struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(body.Results))
	for _, p := range body.Results {
		existing[p.Name] = true
	}
	return existing, nil
}

// listUniqueProperties returns the object's upsertable unique property names,
// best-effort: an empty result just means no hint is shown.
func (d *HubSpotDestination) listUniqueProperties(ctx context.Context, objectType string) []string {
	resp, err := d.client.R(ctx).Get(fmt.Sprintf("/crm/v3/properties/%s", url.PathEscape(d.effectiveObjectType(objectType))))
	if err != nil || (resp.StatusCode() != 200 && resp.StatusCode() != 207) {
		return nil
	}
	var body struct {
		Results []struct {
			Name           string `json:"name"`
			HasUniqueValue bool   `json:"hasUniqueValue"`
		} `json:"results"`
	}
	if json.Unmarshal(resp.Body(), &body) != nil {
		return nil
	}
	var unique []string
	for _, p := range body.Results {
		if p.HasUniqueValue {
			unique = append(unique, p.Name)
		}
	}
	return unique
}

func (d *HubSpotDestination) SwapTable(_ context.Context, _ destination.SwapOptions) error {
	return errors.New("hubspot destination does not support atomic swap")
}

func (d *HubSpotDestination) MergeTable(_ context.Context, _ destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for hubspot destination; set id_property=<property> on the dest-table to upsert on append/replace")
}

func (d *HubSpotDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for hubspot destination")
}

func (d *HubSpotDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for hubspot destination")
}

func (d *HubSpotDestination) DropTable(_ context.Context, _ string) error {
	return errors.New("hubspot destination does not support dropping data")
}

func (d *HubSpotDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for hubspot destination")
}

func (d *HubSpotDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for hubspot destination")
}

func (d *HubSpotDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *HubSpotDestination) GetScheme() string { return "hubspot" }

// IsReverseETL marks HubSpot as a reverse-ETL destination, enabling the RETL
// flags, rename-only --columns, and the update/delete strategies.
func (d *HubSpotDestination) IsReverseETL() {}

// RequiresExplicitStrategy marks HubSpot as needing an explicit
// --incremental-strategy: its framework-default "replace" mirrors the object and
// archives records not in the source, too destructive to inherit silently.
func (d *HubSpotDestination) RequiresExplicitStrategy() {}

// SupportsReplaceStrategy is true because HubSpot has no destructive delete;
// replace degrades to a full write, which upserts when id_property is set.
func (d *HubSpotDestination) SupportsReplaceStrategy() bool      { return true }
func (d *HubSpotDestination) SupportsAppendStrategy() bool       { return true }
func (d *HubSpotDestination) SupportsMergeStrategy() bool        { return false }
func (d *HubSpotDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *HubSpotDestination) SupportsSCD2Strategy() bool         { return false }
func (d *HubSpotDestination) SupportsAtomicSwap() bool           { return false }

// timestampUnit reports the array's declared time unit, defaulting to
// microseconds when the type is not the expected timestamp type.
func timestampUnit(a *array.Timestamp) arrow.TimeUnit {
	if tt, ok := a.DataType().(*arrow.TimestampType); ok {
		return tt.Unit
	}
	return arrow.Microsecond
}

// propertyValue converts a source cell to the string HubSpot property values
// require, returning ok=false for nulls (which are omitted from the record).
func propertyValue(arr arrow.Array, idx int) (string, bool) {
	if arr.IsNull(idx) {
		return "", false
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			if str, ok := val.(string); ok {
				return str, true
			}
			return jsonString(val), true
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return strconv.FormatBool(a.Value(idx)), true
	case *array.Int8:
		return strconv.FormatInt(int64(a.Value(idx)), 10), true
	case *array.Int16:
		return strconv.FormatInt(int64(a.Value(idx)), 10), true
	case *array.Int32:
		return strconv.FormatInt(int64(a.Value(idx)), 10), true
	case *array.Int64:
		return strconv.FormatInt(a.Value(idx), 10), true
	case *array.Uint8:
		return strconv.FormatUint(uint64(a.Value(idx)), 10), true
	case *array.Uint16:
		return strconv.FormatUint(uint64(a.Value(idx)), 10), true
	case *array.Uint32:
		return strconv.FormatUint(uint64(a.Value(idx)), 10), true
	case *array.Uint64:
		return strconv.FormatUint(a.Value(idx), 10), true
	case *array.Float32:
		return strconv.FormatFloat(float64(a.Value(idx)), 'f', -1, 32), true
	case *array.Float64:
		return strconv.FormatFloat(a.Value(idx), 'f', -1, 64), true
	case *array.String:
		return a.Value(idx), true
	case *array.LargeString:
		return a.Value(idx), true
	case *array.Decimal128:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			return val.ToString(dt.Scale), true
		}
		return val.ToString(0), true
	case *array.Decimal256:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal256Type); ok {
			return val.ToString(dt.Scale), true
		}
		return val.ToString(0), true
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Timestamp:
		// HubSpot datetime properties store millisecond precision; microsecond
		// fractional seconds can be rejected, so truncate before formatting.
		return a.Value(idx).ToTime(timestampUnit(a)).UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano), true
	case *array.Struct, array.ListLike:
		return jsonString(arrowToValue(arr, idx)), true
	default:
		v := arrowutil.Value(arr, idx)
		if v == nil {
			return "", false
		}
		if str, ok := v.(string); ok {
			return str, true
		}
		return fmt.Sprintf("%v", v), true
	}
}

func jsonString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// arrowToValue decodes nested struct/list values so they can be JSON-encoded
// into a single HubSpot property string.
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
	_ destination.Destination           = (*HubSpotDestination)(nil)
	_ destination.ReverseETLDestination = (*HubSpotDestination)(nil)
)
