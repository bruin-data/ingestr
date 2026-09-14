package hubspot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	"github.com/jinzhu/inflection"
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

	// objectTypeIDs caches custom object name -> objectTypeId resolutions so a
	// friendly name (e.g. "buildings") maps to its "2-…" id after one lookup.
	mu            sync.Mutex
	objectTypeIDs map[string]string
}

func NewHubSpotDestination() *HubSpotDestination {
	return &HubSpotDestination{objectTypeIDs: map[string]string{}}
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
		// Batch endpoints are POST but safe to retry on 429/5xx; without this
		// resty treats them as non-idempotent and skips retries. resty's default
		// backoff already honors HubSpot's Retry-After header on 429.
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
	IDColumn   string `mapstructure:"id_column"`
	OnError    string `mapstructure:"on_error"`
	// Operation selects the write mode: "upsert" (default) writes records;
	// "archive" soft-deletes them.
	Operation string `mapstructure:"operation"`
	// ClearNulls sends "" for null source cells to clear the field, instead of
	// the default of omitting them (leaving the existing value untouched).
	ClearNulls bool `mapstructure:"clear_nulls"`
	// Association mode ("associations" dest-table): link From records to To records.
	From                string `mapstructure:"from"`
	To                  string `mapstructure:"to"`
	FromIDColumn        string `mapstructure:"from_id_column"`
	ToIDColumn          string `mapstructure:"to_id_column"`
	FromIDProperty      string `mapstructure:"from_id_property"`
	ToIDProperty        string `mapstructure:"to_id_property"`
	AssociationType     string `mapstructure:"association_type"`
	AssociationCategory string `mapstructure:"association_category"`
}

// recordIDProperty is HubSpot's built-in record id; matching on it routes writes
// to the batch update endpoint, as it is not a unique-value property upsert accepts.
const recordIDProperty = "hs_object_id"

// defaultUpsertProperty maps object types that ship with a built-in writable
// unique property to that property, so they upsert by default when the caller
// does not set id_property. Verified against a live HubSpot account; objects
// without a built-in unique key (companies, deals, quotes) are absent and create
// by default. An explicit id_property always overrides this.
var defaultUpsertProperty = map[string]string{
	"contacts":          "email",
	"products":          "hs_sku",
	"line_items":        "hs_external_id",
	"tickets":           "hs_external_object_ids",
	"commerce_payments": "hs_external_reference_id",
}

// shaper turns a source row into a HubSpot batch input for one object type.
type shaper struct {
	objectType string
	// idProperty is the HubSpot property to match records on. Empty creates
	// records; hs_object_id updates them by record id; any other property upserts.
	idProperty string
	// idColumn is the source column supplying the idProperty value.
	idColumn    string
	exclude     map[string]bool
	onErrorSkip bool
	// archive soft-deletes the records (or association links) instead of writing.
	archive bool
	// clearNulls sends "" for null cells to clear the field, rather than omitting.
	clearNulls bool

	// Association mode fields (set when associateTo is non-empty).
	associateTo string
	fromColumn  string
	toColumn    string
	// fromProperty/toProperty name the HubSpot property the from/to column values
	// match on. Empty means the column already holds record ids (hs_object_id);
	// set means the values are a business key resolved to record ids first.
	fromProperty        string
	toProperty          string
	associationType     int
	associationCategory string
}

// associate reports whether the shaper links records instead of writing properties.
func (s *shaper) associate() bool { return s.associateTo != "" }

// matchesRecords reports whether writes carry an id to match existing records
// (either upsert or update), as opposed to plain create.
func (s *shaper) matchesRecords() bool { return s.idProperty != "" }

// update matches existing records by their HubSpot record id via batch update.
func (s *shaper) update() bool { return s.idProperty == recordIDProperty }

// upsert matches on a unique-value property via batch upsert (create-or-update).
func (s *shaper) upsert() bool { return s.matchesRecords() && !s.update() }

func parseShaper(table string, primaryKeys []string) (*shaper, error) {
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

	if p.OnError != "" && p.OnError != "skip" && p.OnError != "fail" {
		return nil, fmt.Errorf("invalid on_error %q: must be \"fail\" (default) or \"skip\"", p.OnError)
	}
	archive, err := parseOperation(p.Operation)
	if err != nil {
		return nil, err
	}
	onErrorSkip := p.OnError == "skip"

	if objectType == "associations" {
		// Associations link records by from/to id columns; a match property or
		// primary key has no meaning here, so warn and ignore it.
		if p.IDProperty != "" || p.IDColumn != "" || len(primaryKeys) > 0 {
			output.Warnf("Warning: hubspot associations ignore id_property/id_column/--primary-key; use from_id_column and to_id_column instead\n")
		}
		return parseAssociationShaper(p, archive)
	}

	if archive {
		return parseArchiveShaper(objectType, p, primaryKeys, onErrorSkip)
	}

	// Precedence for the match property: an explicit id_property on the dest-table,
	// then a single --primary-key, then the object's built-in unique property.
	// Nothing found means create. A single primary key also supplies the id column.
	idProperty := p.IDProperty
	if idProperty == "" {
		switch {
		case len(primaryKeys) == 1:
			idProperty = primaryKeys[0]
		case len(primaryKeys) > 1:
			return nil, fmt.Errorf("hubspot: cannot match on a composite primary key [%s]; HubSpot upserts on a single property — set id_property=<property>, use a single --primary-key, or remove the primary key to create records", strings.Join(primaryKeys, ", "))
		default:
			idProperty = defaultUpsertProperty[objectType]
		}
	}
	idColumn := p.IDColumn
	if idColumn == "" {
		idColumn = idProperty
	}
	if idProperty == "" && p.IDColumn != "" {
		return nil, fmt.Errorf("hubspot: id_column requires id_property to be set on the dest-table")
	}

	// ingestr's own decoration columns are never sent as HubSpot properties; they
	// are added after the source read, so --sql-exclude-columns cannot drop them.
	exclude := map[string]bool{
		naming.IngestrLoadedAtColumn: true,
		naming.IngestrRunIDColumn:    true,
	}

	return &shaper{
		objectType:  objectType,
		idProperty:  idProperty,
		idColumn:    idColumn,
		exclude:     exclude,
		onErrorSkip: onErrorSkip,
		clearNulls:  p.ClearNulls,
	}, nil
}

// parseOperation validates the operation param, reporting whether it archives.
func parseOperation(op string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "", "upsert":
		return false, nil
	case "archive":
		return true, nil
	default:
		return false, fmt.Errorf("invalid operation %q: must be \"upsert\" (default) or \"archive\"", op)
	}
}

// parseArchiveShaper builds a shaper that soft-deletes records. It matches by the
// id column (record ids by default, or a unique property resolved to record ids
// when id_property is set). The built-in upsert defaults are not applied, so a
// wrong key never archives by, say, every contact's email.
func parseArchiveShaper(objectType string, p tableParams, primaryKeys []string, onErrorSkip bool) (*shaper, error) {
	idProperty := p.IDProperty
	if idProperty == "" {
		switch {
		case len(primaryKeys) == 1:
			idProperty = primaryKeys[0]
		case len(primaryKeys) > 1:
			return nil, fmt.Errorf("hubspot: cannot archive on a composite primary key [%s]; use a single --primary-key or id_column", strings.Join(primaryKeys, ", "))
		}
	}
	idColumn := p.IDColumn
	if idColumn == "" {
		idColumn = idProperty
	}
	if idColumn == "" {
		return nil, fmt.Errorf("hubspot: operation=archive requires id_column (the source column holding the record id, or a unique property value with id_property set)")
	}
	return &shaper{
		objectType:  objectType,
		idProperty:  idProperty,
		idColumn:    idColumn,
		onErrorSkip: onErrorSkip,
		archive:     true,
	}, nil
}

// defaultAssociationIDColumn derives an id column from an object type by
// singularizing it and appending "_id" (e.g. "companies" -> "company_id"). It
// returns "" for custom object ids (an objectTypeId like "2-123" or a
// fully-qualified "p123_car"), which have no meaningful singular form.
func defaultAssociationIDColumn(objectType string) string {
	ot := strings.ToLower(strings.TrimSpace(objectType))
	if ot == "" || strings.ContainsAny(ot, "-") || (strings.HasPrefix(ot, "p") && strings.Contains(ot, "_")) {
		return ""
	}
	return inflection.Singular(ot) + "_id"
}

// parseAssociationShaper builds a shaper for the "associations" dest-table, which
// links From records to To records rather than writing properties.
func parseAssociationShaper(p tableParams, archive bool) (*shaper, error) {
	if p.From == "" || p.To == "" {
		return nil, fmt.Errorf("hubspot: associations dest-table requires from= and to= object types")
	}
	// Default the id columns from the object names when not given, e.g.
	// from=contacts -> contact_id. Custom objects have no singular, so their
	// column must be set explicitly.
	fromColumn := p.FromIDColumn
	if fromColumn == "" {
		fromColumn = defaultAssociationIDColumn(p.From)
	}
	toColumn := p.ToIDColumn
	if toColumn == "" {
		toColumn = defaultAssociationIDColumn(p.To)
	}
	// Columns that can't be derived from the object name (custom objects
	// addressed by an objectTypeId or fully-qualified name) stay empty here and
	// are resolved from the object's singular label later, once a client exists.
	if fromColumn != "" && fromColumn == toColumn {
		return nil, fmt.Errorf("hubspot: from and to resolve to the same id column %q; set from_id_column and to_id_column explicitly", fromColumn)
	}
	assocType := 0
	category := ""
	if p.AssociationType != "" {
		n, err := strconv.Atoi(p.AssociationType)
		if err != nil {
			return nil, fmt.Errorf("hubspot: association_type must be a numeric association type id, got %q", p.AssociationType)
		}
		assocType = n
		if category = p.AssociationCategory; category == "" {
			category = "HUBSPOT_DEFINED"
		}
	}
	return &shaper{
		objectType:          strings.ToLower(p.From),
		associateTo:         strings.ToLower(p.To),
		fromColumn:          fromColumn,
		toColumn:            toColumn,
		fromProperty:        p.FromIDProperty,
		toProperty:          p.ToIDProperty,
		associationType:     assocType,
		associationCategory: category,
		onErrorSkip:         p.OnError == "skip",
		archive:             archive,
	}, nil
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
	return fmt.Errorf("hubspot: id column %q not found in source (available: %s); set id_column= on the dest-table", s.idColumn, strings.Join(names, ", "))
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

type associationTypeSpec struct {
	AssociationCategory string `json:"associationCategory"`
	AssociationTypeID   int    `json:"associationTypeId"`
}

// shapeAssociation builds one association link, or ok=false when either id is
// missing. When fromResolve/toResolve are non-nil the column values are business
// keys looked up in the map for their record id; an unresolved key skips the row.
func (s *shaper) shapeAssociation(record arrow.RecordBatch, colIndex map[string]int, row int, fromResolve, toResolve map[string]string) (associationInput, bool) {
	fromVal, okFrom := propertyValue(record.Column(colIndex[s.fromColumn]), row)
	toVal, okTo := propertyValue(record.Column(colIndex[s.toColumn]), row)
	if !okFrom || fromVal == "" || !okTo || toVal == "" {
		return associationInput{}, false
	}
	if fromResolve != nil {
		id, ok := fromResolve[fromVal]
		if !ok {
			return associationInput{}, false
		}
		fromVal = id
	}
	if toResolve != nil {
		id, ok := toResolve[toVal]
		if !ok {
			return associationInput{}, false
		}
		toVal = id
	}
	in := associationInput{From: associationRef{ID: fromVal}, To: associationRef{ID: toVal}}
	if s.associationType != 0 && !s.archive {
		in.Types = []associationTypeSpec{{AssociationCategory: s.associationCategory, AssociationTypeID: s.associationType}}
	}
	return in, true
}

// batchInput is one record in a HubSpot batch create/upsert/update request.
type batchInput struct {
	IDProperty string            `json:"idProperty,omitempty"`
	ID         string            `json:"id,omitempty"`
	Properties map[string]string `json:"properties"`
}

// shapeRow builds the batch input and the endpoint action for one row. A row
// whose match value is missing cannot be matched to an existing record, so it is
// created rather than skipped (create-on-missing), for both upsert and update.
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
		} else if s.clearNulls && col.IsNull(row) {
			// Explicitly clear the field rather than leaving it untouched.
			props[name] = ""
		}
	}

	in := batchInput{Properties: props}
	if !s.matchesRecords() {
		return in, "create", true
	}

	idVal, ok := propertyValue(record.Column(colIndex[s.idColumn]), row)
	if !ok || idVal == "" {
		// No match value: the row can't target an existing record, so create it.
		return in, "create", true
	}

	in.ID = idVal
	if s.upsert() {
		// Upsert matches on a named property; update matches on the record id
		// alone, so it carries id without idProperty.
		in.IDProperty = s.idProperty
		return in, "upsert", true
	}
	return in, "update", true
}

// primaryKeysFor resolves the primary keys a run carries. The replace/append
// strategies only put them on WriteOptions.PrimaryKeys when deduplicating, so
// the schema's PrimaryKeys (always populated) is the reliable fallback.
func primaryKeysFor(explicit []string, sch *schema.TableSchema) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if sch != nil {
		return sch.PrimaryKeys
	}
	return nil
}

func (d *HubSpotDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table, primaryKeysFor(opts.PrimaryKeys, opts.Schema))
	if err != nil {
		return err
	}
	if sh.associate() {
		if err := d.resolveAssociationColumns(ctx, sh); err != nil {
			return err
		}
	}

	var totalRows int64
	var skipped atomic.Int64
	var rejects rejectionLog
	for result := range records {
		if result.Err != nil {
			return result.Err
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
			return err
		}
		totalRows += rows
	}

	warnSkipped(&skipped, sh)
	config.Debug("[HUBSPOT DEST] Wrote %d %s record(s)", totalRows, sh.objectType)
	return reportRejections(sh, &rejects)
}

func (d *HubSpotDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table, primaryKeysFor(opts.PrimaryKeys, opts.Schema))
	if err != nil {
		return err
	}
	if sh.associate() {
		if err := d.resolveAssociationColumns(ctx, sh); err != nil {
			return err
		}
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 || parallelism > defaultParallelism {
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
		return err
	}

	warnSkipped(&skipped, sh)
	return reportRejections(sh, &rejects)
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

	rows := int(record.NumRows())
	var written int64
	// Rows are grouped by endpoint action so a single batch stays homogeneous.
	// Update mode may yield both "update" (id present) and "create" (id absent).
	batches := make(map[string][]batchInput, 2)

	flush := func(action string) error {
		batch := batches[action]
		if len(batch) == 0 {
			return nil
		}
		// Update re-routes ids that don't exist to create; other actions post directly.
		send := d.send
		if action == "update" {
			send = func(ctx context.Context, sh *shaper, items []batchInput, _ string, rejects *rejectionLog) error {
				return d.sendUpdate(ctx, sh, items, rejects)
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
}

// isRecordLevelStatus reports whether a failed status is a per-record data problem
// (validation or conflict) that on_error=skip may tolerate. Auth, rate-limit, and
// server failures are not record-level and must always abort the run.
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
	return batchResult{ok: ok, status: status, category: body.Category, rejections: rejections}
}

// objectNotFoundCategory is the error category HubSpot returns from batch update
// (and read) when an id does not exist. The whole batch fails atomically.
const objectNotFoundCategory = "OBJECT_NOT_FOUND"

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

// schemaObject is one entry from the CRM schemas API used to resolve a friendly
// object name (or objectTypeId) to its id and singular label.
type schemaObject struct {
	ObjectTypeID       string `json:"objectTypeId"`
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName"`
	Labels             struct {
		Singular string `json:"singular"`
		Plural   string `json:"plural"`
	} `json:"labels"`
}

// matches reports whether the schema entry is addressed by the given lowercased
// name, matching on its id, internal name, fully-qualified name, or labels.
func (s schemaObject) matches(lname string) bool {
	return lname == strings.ToLower(s.ObjectTypeID) || lname == strings.ToLower(s.Name) ||
		lname == strings.ToLower(s.FullyQualifiedName) ||
		lname == strings.ToLower(s.Labels.Singular) || lname == strings.ToLower(s.Labels.Plural)
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

// associationColumnFromSchema derives an id column for a custom object side from
// its singular label (e.g. objectTypeId "2-123" -> "Building" -> "building_id").
func (d *HubSpotDestination) associationColumnFromSchema(ctx context.Context, name string) (string, bool) {
	schemas, err := d.fetchSchemas(ctx)
	if err != nil {
		return "", false
	}
	lname := strings.ToLower(name)
	for _, s := range schemas {
		if !s.matches(lname) {
			continue
		}
		label := s.Labels.Singular
		if label == "" {
			label = s.Name
		}
		col := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(label), " ", "_"))
		if col == "" {
			return "", false
		}
		return col + "_id", true
	}
	return "", false
}

// resolveAssociationColumns fills any from/to id column that could not be derived
// from the object name string, by looking up the object's singular label via the
// schemas API. It runs once before writing, so the shaper is not mutated from the
// parallel write goroutines.
func (d *HubSpotDestination) resolveAssociationColumns(ctx context.Context, sh *shaper) error {
	if sh.fromColumn == "" {
		if col, ok := d.associationColumnFromSchema(ctx, sh.objectType); ok {
			sh.fromColumn = col
		}
	}
	if sh.toColumn == "" {
		if col, ok := d.associationColumnFromSchema(ctx, sh.associateTo); ok {
			sh.toColumn = col
		}
	}
	if sh.fromColumn == "" || sh.toColumn == "" {
		return fmt.Errorf("hubspot: associations dest-table requires from_id_column and to_id_column (could not derive them for the custom object type)")
	}
	if sh.fromColumn == sh.toColumn {
		return fmt.Errorf("hubspot: from and to resolve to the same id column %q; set from_id_column and to_id_column explicitly", sh.fromColumn)
	}
	return nil
}

// postBatch posts one chunk to the given batch action endpoint. If HubSpot can't
// infer the object type from a custom object name, it resolves the name to an
// objectTypeId once and retries.
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
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": items}).Post(endpoint)
	if err != nil {
		return batchResult{}, fmt.Errorf("hubspot %s request failed: %w", action, err)
	}
	return parseBatchResponse(resp), nil
}

// send posts one chunk to the given batch action endpoint (create, upsert, or
// update) and records any per-record errors HubSpot returns.
func (d *HubSpotDestination) send(ctx context.Context, sh *shaper, items []batchInput, action string, rejects *rejectionLog) error {
	res, err := d.postBatch(ctx, sh, items, action)
	if err != nil {
		return err
	}
	return d.handleBatchResult(ctx, sh, res, items, action, rejects)
}

// handleBatchResult aborts, warns, or records rejections from a batch response.
func (d *HubSpotDestination) handleBatchResult(ctx context.Context, sh *shaper, res batchResult, items []batchInput, action string, rejects *rejectionLog) error {
	if len(res.rejections) == 0 {
		return nil
	}
	// on_error=skip only tolerates record-level rejections; auth/rate/server
	// failures abort so an entire dropped batch never looks like success.
	if !res.ok && (!sh.onErrorSkip || !isRecordLevelStatus(res.status)) {
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
	rejects.add(res.rejections)
	return nil
}

// sendUpdate posts an update chunk and, when HubSpot reports that some record
// ids do not exist, re-routes those rows to create. Because batch update fails
// atomically, the whole chunk is re-partitioned: existing ids are updated,
// missing ids are created.
func (d *HubSpotDestination) sendUpdate(ctx context.Context, sh *shaper, items []batchInput, rejects *rejectionLog) error {
	res, err := d.postBatch(ctx, sh, items, "update")
	if err != nil {
		return err
	}
	if !res.hasNotFound() {
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
		if err := d.send(ctx, sh, updates, "update", rejects); err != nil {
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

// partitionExisting returns which ids exist in HubSpot. Batch read fails
// atomically when any id is missing, so the set is bisected to isolate the
// missing ids without a per-record read on the common all-present path.
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

// batchReadFound reports how many of the given ids exist, via the batch read
// endpoint. HubSpot returns every requested record when all exist, or an empty
// result set with an OBJECT_NOT_FOUND error when any id is missing.
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

// resolveKeysToIDs maps business-key values to HubSpot record ids via batch read.
// Batch read fails atomically when any key is missing, so the set is bisected to
// resolve the present keys and drop the missing ones without a per-record read.
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
		if len(found) == len(sub) {
			return nil
		}
		var remaining []string
		for _, k := range sub {
			if _, ok := found[k]; !ok {
				remaining = append(remaining, k)
			}
		}
		if len(remaining) <= 1 {
			return nil // the single remaining key is missing
		}
		mid := len(remaining) / 2
		if err := recurse(remaining[:mid]); err != nil {
			return err
		}
		return recurse(remaining[mid:])
	}
	if err := recurse(uniq); err != nil {
		return nil, err
	}
	return out, nil
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
			out[v] = r.ID
		}
	}
	return out, nil
}

// writeArchiveBatch soft-deletes records in chunks of batchLimit. Ids come from
// the id column directly, or are resolved from a unique property first.
func (d *HubSpotDestination) writeArchiveBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	idIdx, ok := colIndex[sh.idColumn]
	if !ok {
		return 0, fmt.Errorf("hubspot: id column %q not found in source for archive", sh.idColumn)
	}

	var resolve map[string]string
	if sh.idProperty != "" && sh.idProperty != recordIDProperty {
		var err error
		resolve, err = d.resolveKeysToIDs(ctx, sh.objectType, sh.idProperty, columnValues(record.Column(idIdx)))
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
		if resolve != nil {
			id, found := resolve[val]
			if !found {
				skipped.Add(1)
				continue
			}
			val = id
		}
		batch = append(batch, val)
		if len(batch) == batchLimit {
			if err := flush(); err != nil {
				return written, err
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
	if len(res.rejections) == 0 {
		return nil
	}
	if !res.ok && (!sh.onErrorSkip || !isRecordLevelStatus(res.status)) {
		return fmt.Errorf("hubspot archive %s returned status %d: %s", sh.objectType, res.status, res.rejections[0].message)
	}
	output.Warnf("Warning: hubspot rejected %d of %d %s archive(s) in this batch; first error: %s\n", len(res.rejections), len(ids), sh.objectType, res.rejections[0].message)
	rejects.add(res.rejections)
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

	// When a side matches on a business key, resolve that column's values to
	// record ids once per batch before linking.
	var fromResolve, toResolve map[string]string
	if sh.fromProperty != "" {
		var err error
		fromResolve, err = d.resolveKeysToIDs(ctx, sh.objectType, sh.fromProperty, columnValues(record.Column(colIndex[sh.fromColumn])))
		if err != nil {
			return 0, err
		}
	}
	if sh.toProperty != "" {
		var err error
		toResolve, err = d.resolveKeysToIDs(ctx, sh.associateTo, sh.toProperty, columnValues(record.Column(colIndex[sh.toColumn])))
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
		item, ok := sh.shapeAssociation(record, colIndex, row, fromResolve, toResolve)
		if !ok {
			skipped.Add(1)
			continue
		}
		batch = append(batch, item)
		if len(batch) == batchLimit {
			if err := flush(); err != nil {
				return written, err
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
	case sh.archive:
		verb = "archive"
	case sh.associationType != 0:
		verb = "create"
	}
	res, err := d.postAssociations(ctx, sh.objectType, sh.associateTo, verb, items)
	if err != nil {
		return err
	}
	// Custom object names on either side may need resolving to their ids.
	if res.isInferError() {
		d.resolveObjectTypeID(ctx, sh.objectType)
		d.resolveObjectTypeID(ctx, sh.associateTo)
		res, err = d.postAssociations(ctx, sh.objectType, sh.associateTo, verb, items)
		if err != nil {
			return err
		}
	}
	if len(res.rejections) == 0 {
		return nil
	}
	pair := fmt.Sprintf("%s->%s", sh.objectType, sh.associateTo)
	if !res.ok && (!sh.onErrorSkip || !isRecordLevelStatus(res.status)) {
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
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": items}).Post(endpoint)
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
	category string
	message  string
	context  json.RawMessage
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

// reportRejections fails the run (or warns, under on_error=skip) with each
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
		fmt.Fprintf(&b, "\n  - (%s) %s: %s", items[i].category, items[i].message, string(items[i].context))
	}
	if len(items) > shown {
		fmt.Fprintf(&b, "\n  ... and %d more", len(items)-shown)
	}

	if sh.onErrorSkip {
		output.Warnf("Warning: %s\n", b.String())
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
	sh, err := parseShaper(opts.Table, primaryKeysFor(opts.PrimaryKeys, opts.Schema))
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

// listUniqueProperties returns the object's upsertable unique property names.
// It is best-effort: an empty result (lookup failed or the scope is missing)
// just means no hint is shown.
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

// WantsLogicalPrimaryKeys makes the strategies forward the run's primary keys so
// a single --primary-key (or source-defined key) can drive upserts.
func (d *HubSpotDestination) WantsLogicalPrimaryKeys() bool { return true }

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
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02"), true
	case *array.Timestamp:
		return a.Value(idx).ToTime(timestampUnit(a)).UTC().Format(time.RFC3339Nano), true
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

var _ destination.Destination = (*HubSpotDestination)(nil)
