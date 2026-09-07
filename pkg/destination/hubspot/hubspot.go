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
}

func NewHubSpotDestination() *HubSpotDestination {
	return &HubSpotDestination{}
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
	// Association mode ("associations" dest-table): link From records to To records.
	From                string `mapstructure:"from"`
	To                  string `mapstructure:"to"`
	FromIDColumn        string `mapstructure:"from_id_column"`
	ToIDColumn          string `mapstructure:"to_id_column"`
	AssociationType     string `mapstructure:"association_type"`
	AssociationCategory string `mapstructure:"association_category"`
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
	idColumn    string
	exclude     map[string]bool
	onErrorSkip bool

	// Association mode fields (set when associateTo is non-empty).
	associateTo         string
	fromColumn          string
	toColumn            string
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

func parseShaper(table string) (*shaper, error) {
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

	if objectType == "associations" {
		return parseAssociationShaper(p)
	}

	idColumn := p.IDColumn
	if idColumn == "" {
		idColumn = p.IDProperty
	}
	if p.IDProperty == "" && p.IDColumn != "" {
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
		idProperty:  p.IDProperty,
		idColumn:    idColumn,
		exclude:     exclude,
		onErrorSkip: p.OnError == "skip",
	}, nil
}

// parseAssociationShaper builds a shaper for the "associations" dest-table, which
// links From records to To records rather than writing properties.
func parseAssociationShaper(p tableParams) (*shaper, error) {
	if p.From == "" || p.To == "" {
		return nil, fmt.Errorf("hubspot: associations dest-table requires from= and to= object types")
	}
	if p.FromIDColumn == "" || p.ToIDColumn == "" {
		return nil, fmt.Errorf("hubspot: associations dest-table requires from_id_column and to_id_column")
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
		fromColumn:          p.FromIDColumn,
		toColumn:            p.ToIDColumn,
		associationType:     assocType,
		associationCategory: category,
		onErrorSkip:         p.OnError == "skip",
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

// shapeAssociation builds one association link, or ok=false when either id is missing.
func (s *shaper) shapeAssociation(record arrow.RecordBatch, colIndex map[string]int, row int) (associationInput, bool) {
	fromVal, okFrom := propertyValue(record.Column(colIndex[s.fromColumn]), row)
	toVal, okTo := propertyValue(record.Column(colIndex[s.toColumn]), row)
	if !okFrom || fromVal == "" || !okTo || toVal == "" {
		return associationInput{}, false
	}
	in := associationInput{From: associationRef{ID: fromVal}, To: associationRef{ID: toVal}}
	if s.associationType != 0 {
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

// shape builds the batch input for one row, or returns ok=false when a matched
// row has no id value and cannot be matched to a record.
func (s *shaper) shape(record arrow.RecordBatch, colIndex map[string]int, row int) (batchInput, bool) {
	props := make(map[string]string)
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		if name == s.idColumn || s.exclude[name] {
			continue
		}
		if v, ok := propertyValue(record.Column(i), row); ok {
			props[name] = v
		}
	}

	in := batchInput{Properties: props}
	if s.matchesRecords() {
		idVal, ok := propertyValue(record.Column(colIndex[s.idColumn]), row)
		if !ok || idVal == "" {
			return batchInput{}, false
		}
		in.ID = idVal
		// Upsert matches on a named property; update matches on the record id
		// alone, so it carries id without idProperty.
		if s.upsert() {
			in.IDProperty = s.idProperty
		}
	}
	return in, true
}

func (d *HubSpotDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table)
	if err != nil {
		return err
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
	sh, err := parseShaper(opts.Table)
	if err != nil {
		return err
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

	if err := sh.validateColumns(record); err != nil {
		return 0, err
	}

	rows := int(record.NumRows())
	batch := make([]batchInput, 0, min(rows, batchLimit))
	var written int64

	for row := 0; row < rows; row++ {
		item, ok := sh.shape(record, colIndex, row)
		if !ok {
			skipped.Add(1)
			continue
		}
		batch = append(batch, item)
		if len(batch) == batchLimit {
			if err := d.send(ctx, sh, batch, rejects); err != nil {
				return written, err
			}
			written += int64(len(batch))
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := d.send(ctx, sh, batch, rejects); err != nil {
			return written, err
		}
		written += int64(len(batch))
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
	ok := status == 200 || status == 201 || status == 207
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

// send posts one chunk to the batch create, upsert, or update endpoint and
// records any per-record errors HubSpot returns.
func (d *HubSpotDestination) send(ctx context.Context, sh *shaper, items []batchInput, rejects *rejectionLog) error {
	action := "create"
	switch {
	case sh.update():
		action = "update"
	case sh.upsert():
		action = "upsert"
	}
	endpoint := fmt.Sprintf("/crm/v3/objects/%s/batch/%s", url.PathEscape(sh.objectType), action)

	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": items}).Post(endpoint)
	if err != nil {
		return fmt.Errorf("hubspot %s request failed: %w", action, err)
	}

	res := parseBatchResponse(resp)
	if len(res.rejections) == 0 {
		return nil
	}
	// on_error=skip only tolerates record-level rejections; auth/rate/server
	// failures abort so an entire dropped batch never looks like success.
	if !res.ok && !(sh.onErrorSkip && isRecordLevelStatus(res.status)) {
		hint := ""
		if res.category == "CONFLICT" || res.status == 409 {
			hint = "; set id_property=<property> on the dest-table to upsert existing records instead of creating them"
		}
		return fmt.Errorf("hubspot %s %s returned status %d: %s%s", action, sh.objectType, res.status, res.rejections[0].message, hint)
	}

	output.Warnf("Warning: hubspot rejected %d of %d %s record(s) in this batch; first error: %s\n", len(res.rejections), len(items), sh.objectType, res.rejections[0].message)
	rejects.add(res.rejections)
	return nil
}

// writeAssociationBatch links records in chunks of batchLimit.
func (d *HubSpotDestination) writeAssociationBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, colIndex map[string]int, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	if err := sh.validateAssociationColumns(colIndex); err != nil {
		return 0, err
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
		item, ok := sh.shapeAssociation(record, colIndex, row)
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
	if sh.associationType != 0 {
		verb = "create"
	}
	endpoint := fmt.Sprintf("/crm/v4/associations/%s/%s/batch/%s", url.PathEscape(sh.objectType), url.PathEscape(sh.associateTo), verb)

	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": items}).Post(endpoint)
	if err != nil {
		return fmt.Errorf("hubspot associate request failed: %w", err)
	}

	res := parseBatchResponse(resp)
	if len(res.rejections) == 0 {
		return nil
	}
	pair := fmt.Sprintf("%s->%s", sh.objectType, sh.associateTo)
	if !res.ok && !(sh.onErrorSkip && isRecordLevelStatus(res.status)) {
		return fmt.Errorf("hubspot %s associate returned status %d: %s", pair, res.status, res.rejections[0].message)
	}

	output.Warnf("Warning: hubspot rejected %d of %d %s association(s) in this batch; first error: %s\n", len(res.rejections), len(items), pair, res.rejections[0].message)
	rejects.add(res.rejections)
	return nil
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
	sh, err := parseShaper(opts.Table)
	if err != nil {
		return err
	}
	// Association mode writes no properties, so there is nothing to validate here.
	if sh.associate() {
		return nil
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
	endpoint := fmt.Sprintf("/crm/v3/properties/%s/batch/read", url.PathEscape(objectType))
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"inputs": inputs, "archived": false}).Post(endpoint)
	if err != nil {
		return nil, err
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
