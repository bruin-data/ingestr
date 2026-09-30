package attio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
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
	defaultBaseURL = "https://api.attio.com/v2"

	// Attio budgets 25 writes and 100 reads per second per workspace separately.
	writeRateLimit = 20.0
	readRateLimit  = 80.0
	rateLimitBurst = 5

	retryCount   = 5
	retryWait    = 1 * time.Second
	retryMaxWait = 30 * time.Second

	// Each row is one request, so throughput comes from concurrent requests;
	// the rate limiters, not this pool, bound the request rate.
	rowWorkers = 24

	// recordIDAttr is Attio's server-assigned record id.
	recordIDAttr = "record_id"

	queryPageSize = 500

	notFoundCode    = "NOT_FOUND"
	malformedIDCode = "MALFORMED_ID"
	conflictCode    = "concurrent_write_conflict"
	conflictRetries = 4
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// retryBackoff is the wait before re-sending a row after a write conflict or a
// failed delete; a var so tests don't sleep.
var retryBackoff = func(attempt int) time.Duration { return 500 * time.Millisecond << attempt }

type AttioDestination struct {
	client  *httpclient.Client
	reads   *httpclient.Client
	baseURL string

	mu      sync.Mutex
	metas   map[string]*objectMeta
	objects []objectInfo
}

func NewAttioDestination() *AttioDestination {
	return &AttioDestination{baseURL: defaultBaseURL, metas: map[string]*objectMeta{}}
}

func (d *AttioDestination) Schemes() []string {
	return []string{"attio"}
}

func parseURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid attio URI: %w", err)
	}
	if parsed.Scheme != "attio" {
		return "", fmt.Errorf("invalid attio URI: must start with attio://")
	}
	apiKey := parsed.Query().Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in attio URI")
	}
	return apiKey, nil
}

func (d *AttioDestination) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseURI(uri)
	if err != nil {
		return err
	}

	newClient := func(rps float64) *httpclient.Client {
		return httpclient.New(
			httpclient.WithBaseURL(d.baseURL),
			httpclient.WithTimeout(60*time.Second),
			httpclient.WithRateLimiter(rps, rateLimitBurst),
			httpclient.WithRetry(retryCount, retryWait, retryMaxWait),
			// Assert, update, delete and query are idempotent; create opts out per request.
			httpclient.WithAllowNonIdempotentRetry(),
			httpclient.WithAuth(httpclient.NewBearerAuth(apiKey)),
			httpclient.WithDebug(config.DebugMode),
			httpclient.WithHeader("Content-Type", "application/json"),
			httpclient.WithHeader("Accept", "application/json"),
		)
	}
	d.client = newClient(writeRateLimit)
	d.reads = newClient(readRateLimit)

	return d.checkToken(ctx)
}

// checkToken fails at connect on a revoked token or one without write access,
// which would otherwise surface as a reject on every row.
func (d *AttioDestination) checkToken(ctx context.Context) error {
	resp, err := d.reads.R(ctx).Get("/self")
	if err != nil {
		return fmt.Errorf("attio: failed to reach the API: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("attio: the api_key was not accepted: %w", parseAPIError(resp))
	}
	var self struct {
		Active        bool   `json:"active"`
		Scope         string `json:"scope"`
		WorkspaceSlug string `json:"workspace_slug"`
	}
	if err := json.Unmarshal(resp.Body(), &self); err != nil {
		return fmt.Errorf("attio: failed to parse token info: %w", err)
	}
	if !self.Active {
		return errors.New("attio: the api_key is invalid or has been revoked")
	}
	if self.Scope != "" && !slices.Contains(strings.Fields(self.Scope), "record_permission:read-write") {
		return fmt.Errorf("attio: the api_key can't write records; give the access token the \"Records: Read-write\" scope (record_permission:read-write). Current scopes: %s", self.Scope)
	}
	config.Debug("[ATTIO DEST] Connected to workspace %s", self.WorkspaceSlug)
	return nil
}

func (d *AttioDestination) Close(_ context.Context) error {
	var errs []error
	for _, c := range []*httpclient.Client{d.client, d.reads} {
		if c != nil {
			errs = append(errs, c.Close())
		}
	}
	return errors.Join(errs...)
}

// tableParams are the options carried on the --dest-table string, e.g.
// "people?matching_attribute=email_addresses".
type tableParams struct {
	MatchingAttribute string `mapstructure:"matching_attribute"`
}

type shaper struct {
	object string
	// matchAttr is the attribute rows are matched on; empty creates records.
	matchAttr        string
	matchColumn      string
	defaultColumn    bool
	matchMultiselect bool
	numericKey       bool
	rejectMode       string
	writeNulls       bool
	updateOnly       bool
	createOnly       bool
	archive          bool
	mirror           bool
	// labelColumns (append) are the --primary-key columns; they only name a rejected row.
	labelColumns []string
	meta         *objectMeta
	// seen holds the folded match values of every source row (mirror); written
	// holds the record ids the run wrote, which the sweep never deletes.
	seen      *sync.Map
	written   *sync.Map
	sawSource atomic.Bool
}

func (s *shaper) skip() bool     { return s.rejectMode == string(config.RejectSkip) }
func (s *shaper) failFast() bool { return s.rejectMode == string(config.RejectFailFast) }
func (s *shaper) byRecordID() bool {
	return s.matchAttr == recordIDAttr
}

func (s *shaper) upsert() bool {
	return s.matchAttr != "" && !s.updateOnly && !s.archive && !s.createOnly
}

func (s *shaper) isMatchColumn(name string) bool {
	return s.matchColumn != "" && (name == s.matchColumn || (s.defaultColumn && strings.EqualFold(name, s.matchColumn)))
}

// skipColumn reports columns never written as attribute values.
func (s *shaper) skipColumn(name string) bool {
	return name == naming.IngestrLoadedAtColumn || name == naming.IngestrRunIDColumn ||
		s.isMatchColumn(name) || strings.EqualFold(name, recordIDAttr)
}

func (s *shaper) slug() string {
	if s.meta != nil {
		return s.meta.slug
	}
	return s.object
}

// matchValue renders a match value in the form used for lookups and correlation.
func (s *shaper) matchValue(v interface{}) string {
	str := strings.TrimSpace(stringValue(v))
	if s.numericKey {
		if n, ok := canonicalNumber(str); ok {
			return n
		}
	}
	if s.byRecordID() {
		return strings.ToLower(str)
	}
	return str
}

func parseShaper(table, strategy string, primaryKeys []string, rejectMode string, writeNulls bool) (*shaper, error) {
	var p tableParams
	path, _, err := tablespec.Parse(table, &p)
	if err != nil {
		return nil, err
	}
	object := strings.TrimSpace(path)
	if i := strings.LastIndex(object, "."); i >= 0 {
		object = object[i+1:]
	}
	if object == "" {
		return nil, fmt.Errorf("attio dest-table must be an object slug, e.g. \"people\", \"companies\", \"deals\"")
	}
	matchAttr := strings.TrimSpace(p.MatchingAttribute)
	sh := &shaper{object: object, rejectMode: rejectMode, writeNulls: writeNulls}

	switch config.IncrementalStrategy(strategy) {
	case config.StrategyAppend:
		if matchAttr != "" {
			return nil, fmt.Errorf("attio: append always creates records and never matches, so matching_attribute=%s would be ignored; use --incremental-strategy merge to upsert on it, or remove matching_attribute", matchAttr)
		}
		sh.createOnly = true
		sh.labelColumns = primaryKeys
		return sh, nil
	case config.StrategyUpdate, config.StrategyDelete:
		sh.updateOnly = strategy == string(config.StrategyUpdate)
		sh.archive = strategy == string(config.StrategyDelete)
		if matchAttr == "" {
			matchAttr = recordIDAttr
		}
		sh.matchAttr = matchAttr
		sh.matchColumn, err = sourceColumn(primaryKeys, matchAttr)
		sh.defaultColumn = len(primaryKeys) == 0
		return sh, err
	case config.StrategyMerge, config.StrategyReplace:
		if matchAttr == "" {
			return nil, fmt.Errorf("attio: %s needs a unique attribute to match on — set matching_attribute=<attribute> on the dest-table (e.g. people?matching_attribute=email_addresses)", strategy)
		}
		if strings.EqualFold(matchAttr, recordIDAttr) {
			return nil, fmt.Errorf("attio: %s cannot match on record_id — Attio assigns record ids and can't create a record at a given one; use --incremental-strategy update to update records by id, or set matching_attribute=<unique attribute> to upsert", strategy)
		}
		sh.matchAttr = matchAttr
		sh.matchColumn, err = sourceColumn(primaryKeys, "")
		if err != nil {
			return nil, err
		}
		if strategy == string(config.StrategyReplace) {
			sh.mirror = true
			sh.seen = &sync.Map{}
			sh.written = &sync.Map{}
		}
		return sh, nil
	default:
		return nil, fmt.Errorf("attio does not support the %q strategy; use merge, update, append, delete, or replace", strategy)
	}
}

func sourceColumn(primaryKeys []string, defaultColumn string) (string, error) {
	switch len(primaryKeys) {
	case 1:
		return primaryKeys[0], nil
	case 0:
		if defaultColumn != "" {
			return defaultColumn, nil
		}
		return "", fmt.Errorf("attio: this strategy needs the source column holding the match value — pass --primary-key")
	default:
		return "", fmt.Errorf("attio: cannot match on a composite primary key [%s]; pass a single --primary-key", strings.Join(primaryKeys, ", "))
	}
}

func primaryKeysFor(explicit []string, sch *schema.TableSchema) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if sch != nil {
		return sch.PrimaryKeys
	}
	return nil
}

// applyMeta resolves the object's slug and the match attribute's canonical
// slug and type. Best-effort: PrepareTable already warned if it is unavailable.
func (d *AttioDestination) applyMeta(ctx context.Context, sh *shaper) {
	meta, err := d.describe(ctx, sh.object)
	if err != nil {
		config.Debug("[ATTIO DEST] no attributes for %s (%v); using names as given", sh.object, err)
		return
	}
	sh.meta = meta
	if sh.matchAttr == "" || sh.byRecordID() {
		return
	}
	if a, ok := meta.attr(sh.matchAttr); ok {
		sh.matchAttr = a.APISlug
		sh.numericKey = numericTypes[a.Type]
		sh.matchMultiselect = a.IsMultiselect
	}
}

func (d *AttioDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	sh, err := parseShaper(opts.Table, opts.Strategy, primaryKeysFor(opts.PrimaryKeys, opts.Schema), opts.RejectMode, opts.WriteNulls)
	if err != nil {
		return err
	}
	d.applyMeta(ctx, sh)
	workers := rowWorkers

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
		err := d.writeBatch(ctx, sh, record, workers, &skipped, &rejects)
		record.Release()
		if err != nil {
			writeErr = err
			break
		}
	}
	if writeErr != nil {
		if rejErr := reportRejections(sh, &rejects); rejErr != nil {
			return errors.Join(writeErr, rejErr)
		}
		return writeErr
	}

	if n := skipped.Load(); n > 0 {
		output.Warnf("Warning: attio skipped %d %s record(s) with a missing %s value\n", n, sh.slug(), sh.matchColumn)
	}
	return d.finalizeAndReport(ctx, sh, workers, &rejects)
}

// WriteParallel sends rows concurrently within each batch, so batches are
// consumed in order by a single reader.
func (d *AttioDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.Write(ctx, records, opts)
}

const (
	kindPlain = iota
	kindRef
	kindName
)

type colTarget struct {
	idx         int
	attr        string
	kind        int
	multiselect bool
	refObject   string
	refField    string
	// refValueKey is the key a nested match value takes, by the target
	// attribute's type: [{"domain": "acme.com"}] or [{"value": "A-1"}].
	refValueKey string
}

// nestedValueKey maps an attribute type to the object key its value is written
// under inside a record reference.
func nestedValueKey(attrType, slug string) string {
	switch {
	case attrType == "domain" || (attrType == "" && slug == "domains"):
		return "domain"
	case attrType == "email-address" || (attrType == "" && slug == "email_addresses"):
		return "email_address"
	case attrType == "phone-number":
		return "original_phone_number"
	default:
		return "value"
	}
}

var nameFields = map[string]bool{"first_name": true, "last_name": true, "full_name": true}

// dottedColumn splits "company.domains" or "owner.people.email_addresses" into
// the attribute, the optional target object and the field.
func dottedColumn(name string) (rel, typ, field string, ok bool) {
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
	return rel, typ, field, rel != "" && field != ""
}

// columnPlan maps each written source column to its attribute.
func (d *AttioDestination) columnPlan(ctx context.Context, s *shaper, record arrow.RecordBatch) []colTarget {
	plan := make([]colTarget, 0, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		if s.skipColumn(name) {
			continue
		}
		if rel, typ, field, ok := dottedColumn(name); ok {
			c := colTarget{idx: i, attr: rel, kind: kindRef, refObject: typ, refField: field}
			if s.meta != nil {
				if a, found := s.meta.attr(rel); found {
					c.attr = a.APISlug
					c.multiselect = a.IsMultiselect
					if a.Type == "personal-name" {
						c.kind = kindName
						c.refField = strings.ToLower(field)
					}
					if targets := s.meta.refTargets[strings.ToLower(rel)]; typ == "" && len(targets) == 1 {
						c.refObject = targets[0]
					}
				}
			}
			if c.kind == kindRef {
				if strings.EqualFold(field, recordIDAttr) {
					c.refField = recordIDAttr
				}
				attrType := ""
				if c.refObject != "" {
					if target, err := d.describe(ctx, c.refObject); err == nil {
						if a, found := target.attr(field); found {
							c.refField, attrType = a.APISlug, a.Type
						}
					}
				}
				c.refValueKey = nestedValueKey(attrType, c.refField)
			}
			plan = append(plan, c)
			continue
		}
		c := colTarget{idx: i, attr: name, kind: kindPlain}
		if s.meta != nil {
			if a, found := s.meta.attr(name); found {
				c.attr = a.APISlug
				c.multiselect = a.IsMultiselect
			}
		}
		plan = append(plan, c)
	}
	return plan
}

func wrap(v interface{}, multiselect bool) interface{} {
	if _, isList := v.([]interface{}); isList || !multiselect {
		return v
	}
	return []interface{}{v}
}

// shapeValues builds the values object for one row. Null cells clear the
// attribute (an empty list) under --write-nulls, except on create.
func (s *shaper) shapeValues(record arrow.RecordBatch, plan []colTarget, row int, create bool) map[string]interface{} {
	clear := s.writeNulls && !create
	values := make(map[string]interface{}, len(plan))
	refs := map[string][]interface{}{}
	nullRefs := map[string]bool{}
	nullNames := map[string]bool{}
	names := map[string]map[string]string{}
	for _, c := range plan {
		v, ok := cellValue(record.Column(c.idx), row)
		switch c.kind {
		case kindPlain:
			if ok {
				values[c.attr] = wrap(v, c.multiselect)
			} else if clear {
				values[c.attr] = []interface{}{}
			}
		case kindRef:
			if !ok {
				nullRefs[c.attr] = true
				continue
			}
			items, isList := v.([]interface{})
			if !isList {
				items = []interface{}{v}
			}
			for _, item := range items {
				ref := map[string]interface{}{"target_object": c.refObject}
				if c.refField == recordIDAttr {
					ref["target_record_id"] = stringValue(item)
				} else {
					ref[c.refField] = []interface{}{map[string]interface{}{c.refValueKey: item}}
				}
				refs[c.attr] = append(refs[c.attr], ref)
			}
		case kindName:
			if !ok {
				nullNames[c.attr] = true
				continue
			}
			if names[c.attr] == nil {
				names[c.attr] = map[string]string{}
			}
			names[c.attr][c.refField] = stringValue(v)
		}
	}
	for attr, list := range refs {
		values[attr] = list
	}
	if clear {
		for attr := range nullRefs {
			if _, set := refs[attr]; !set {
				values[attr] = []interface{}{}
			}
		}
		for attr := range nullNames {
			if _, set := names[attr]; !set {
				values[attr] = []interface{}{}
			}
		}
	}
	for attr, parts := range names {
		full := parts["full_name"]
		if full == "" {
			full = strings.TrimSpace(parts["first_name"] + " " + parts["last_name"])
		}
		values[attr] = map[string]interface{}{"first_name": parts["first_name"], "last_name": parts["last_name"], "full_name": full}
	}
	return values
}

// rowTask is one API call. Tasks sharing a key run in order on the same worker,
// so two rows for one record never race.
type rowTask struct {
	key string
	run func(ctx context.Context) error
}

func runTasks(ctx context.Context, workers int, tasks []rowTask) error {
	if len(tasks) == 0 {
		return nil
	}
	workers = min(workers, len(tasks))
	queues := make([][]rowTask, workers)
	for i, t := range tasks {
		slot := i % workers
		if t.key != "" {
			h := fnv.New32a()
			_, _ = h.Write([]byte(t.key))
			slot = int(h.Sum32() % uint32(workers))
		}
		queues[slot] = append(queues[slot], t)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for _, q := range queues {
		wg.Add(1)
		go func(q []rowTask) {
			defer wg.Done()
			for _, t := range q {
				if ctx.Err() != nil {
					return
				}
				if err := t.run(ctx); err != nil {
					once.Do(func() { firstErr = err })
					cancel()
					return
				}
			}
		}(q)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (d *AttioDestination) writeBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, workers int, skipped *atomic.Int64, rejects *rejectionLog) error {
	sh.sawSource.Store(true)
	keyIdx := -1
	if sh.matchColumn != "" {
		idx, err := sh.findMatchColumn(record)
		if err != nil {
			return err
		}
		keyIdx = idx
	}

	rows := int(record.NumRows())
	keys := make([]string, rows)
	if keyIdx >= 0 {
		col := record.Column(keyIdx)
		for row := 0; row < rows; row++ {
			if v, ok := cellValue(col, row); ok {
				keys[row] = sh.matchValue(v)
			}
		}
	}
	if sh.mirror {
		for _, k := range keys {
			if k != "" {
				sh.seen.Store(matchKey(k), struct{}{})
			}
		}
	}

	if sh.archive || sh.updateOnly {
		return d.writeMatchedBatch(ctx, sh, record, keys, workers, skipped, rejects)
	}

	plan := d.columnPlan(ctx, sh, record)
	tasks := make([]rowTask, 0, rows)
	for row := 0; row < rows; row++ {
		key := keys[row]
		if sh.createOnly || key == "" {
			values := sh.shapeValues(record, plan, row, true)
			label := sh.label(record, row)
			tasks = append(tasks, rowTask{run: func(ctx context.Context) error {
				return d.sendRow(ctx, sh, "create", "", values, label, rejects)
			}})
			continue
		}
		values := sh.shapeValues(record, plan, row, false)
		values[sh.matchAttr] = wrap(matchCell(record.Column(keyIdx), row, sh), sh.matchMultiselect)
		tasks = append(tasks, rowTask{key: matchKey(key), run: func(ctx context.Context) error {
			return d.sendRow(ctx, sh, "upsert", "", values, sh.matchAttr+"="+key, rejects)
		}})
	}
	return runTasks(ctx, workers, tasks)
}

// matchCell is the value sent for the match attribute: numbers stay numeric.
func matchCell(arr arrow.Array, row int, sh *shaper) interface{} {
	v, _ := cellValue(arr, row)
	if sh.numericKey {
		if n, ok := canonicalNumber(stringValue(v)); ok {
			return json.Number(n)
		}
	}
	return v
}

// writeMatchedBatch updates or deletes existing records: by record id directly,
// or by resolving the match attribute to every record that holds the value.
func (d *AttioDestination) writeMatchedBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, keys []string, workers int, skipped *atomic.Int64, rejects *rejectionLog) error {
	resolved := map[string][]string{}
	if !sh.byRecordID() {
		var err error
		if resolved, err = d.resolveKeys(ctx, sh, keys, workers); err != nil {
			return err
		}
	}

	var plan []colTarget
	if !sh.archive {
		plan = d.columnPlan(ctx, sh, record)
	}
	action := "update"
	if sh.archive {
		action = "delete"
	}

	var tasks []rowTask
	for row, key := range keys {
		if key == "" {
			skipped.Add(1)
			continue
		}
		ident := sh.matchAttr + "=" + key
		ids := []string{key}
		if sh.byRecordID() {
			if !uuidPattern.MatchString(key) {
				if err := sh.reject(rejects, rejection{code: malformedIDCode, message: fmt.Sprintf("%q is not an Attio record id", key), identifier: ident}); err != nil {
					return err
				}
				continue
			}
		} else {
			ids = resolved[key]
			if len(ids) == 0 {
				if err := sh.reject(rejects, rejection{code: notFoundCode, message: fmt.Sprintf("no %s record found with %s=%q", sh.slug(), sh.matchAttr, key), identifier: ident}); err != nil {
					return err
				}
				continue
			}
		}
		var values map[string]interface{}
		if !sh.archive {
			values = sh.shapeValues(record, plan, row, false)
		}
		for _, id := range ids {
			tasks = append(tasks, rowTask{key: id, run: func(ctx context.Context) error {
				return d.sendRow(ctx, sh, action, id, values, ident, rejects)
			}})
		}
	}
	return runTasks(ctx, workers, tasks)
}

func (s *shaper) findMatchColumn(record arrow.RecordBatch) (int, error) {
	names := make([]string, 0, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		if record.ColumnName(i) == s.matchColumn {
			return i, nil
		}
		names = append(names, record.ColumnName(i))
	}
	if s.defaultColumn {
		for i, name := range names {
			if strings.EqualFold(name, s.matchColumn) {
				return i, nil
			}
		}
	}
	slices.Sort(names)
	return -1, fmt.Errorf("attio: match column %q not found in source (available: %s); pass --primary-key to name the column holding the %s value", s.matchColumn, strings.Join(names, ", "), s.matchAttr)
}

// label names a created row by its --primary-key values for reject lines.
func (s *shaper) label(record arrow.RecordBatch, row int) string {
	if len(s.labelColumns) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.labelColumns))
	for _, c := range s.labelColumns {
		idx := -1
		for i := 0; i < int(record.NumCols()); i++ {
			if record.ColumnName(i) == c {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ""
		}
		v, ok := cellValue(record.Column(idx), row)
		if !ok {
			return ""
		}
		parts = append(parts, c+"="+stringValue(v))
	}
	return strings.Join(parts, ", ")
}

// resolveKeys maps each distinct match value to the ids of the records holding
// it. Attio's filter ignores case but a unique text attribute doesn't, so an
// exact match wins and the case-folded matches are only a fallback.
func (d *AttioDestination) resolveKeys(ctx context.Context, sh *shaper, keys []string, workers int) (map[string][]string, error) {
	out := map[string][]string{}
	var mu sync.Mutex
	var tasks []rowTask
	seen := map[string]bool{}
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		var filterValue interface{} = k
		if sh.numericKey {
			if _, ok := canonicalNumber(k); !ok {
				continue
			}
			filterValue = json.Number(k)
		}
		tasks = append(tasks, rowTask{key: matchKey(k), run: func(ctx context.Context) error {
			var ids, exact []string
			err := d.queryRecords(ctx, sh.slug(), map[string]interface{}{sh.matchAttr: filterValue}, func(rec queriedRecord) {
				ids = append(ids, rec.ID.RecordID)
				for _, v := range storedValues(rec.Values[sh.matchAttr]) {
					if sh.matchValue(v) == k {
						exact = append(exact, rec.ID.RecordID)
						break
					}
				}
			})
			if err != nil {
				return fmt.Errorf("attio: failed to look up %s records by %s: %w", sh.slug(), sh.matchAttr, err)
			}
			if len(exact) > 0 {
				ids = exact
			}
			mu.Lock()
			out[k] = ids
			mu.Unlock()
			return nil
		}})
	}
	if err := runTasks(ctx, workers, tasks); err != nil {
		return nil, err
	}
	return out, nil
}

type queriedRecord struct {
	ID struct {
		RecordID string `json:"record_id"`
	} `json:"id"`
	Values map[string]json.RawMessage `json:"values"`
}

// queryRecords pages through the records matching filter (nil lists all).
func (d *AttioDestination) queryRecords(ctx context.Context, object string, filter map[string]interface{}, fn func(queriedRecord)) error {
	for offset := 0; ; offset += queryPageSize {
		body := map[string]interface{}{"limit": queryPageSize, "offset": offset}
		if filter != nil {
			body["filter"] = filter
		}
		resp, err := d.reads.R(ctx).SetBody(body).Post("/objects/" + url.PathEscape(object) + "/records/query")
		if err != nil {
			return err
		}
		if !resp.IsSuccess() {
			return parseAPIError(resp)
		}
		var parsed struct {
			Data []queriedRecord `json:"data"`
		}
		if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
			return fmt.Errorf("failed to parse records: %w", err)
		}
		for _, rec := range parsed.Data {
			fn(rec)
		}
		if len(parsed.Data) < queryPageSize {
			return nil
		}
	}
}

// sendRow performs one write and records its outcome: a per-record failure is a
// rejection, anything else aborts the run.
func (d *AttioDestination) sendRow(ctx context.Context, sh *shaper, action, recordID string, values map[string]interface{}, ident string, rejects *rejectionLog) error {
	base := "/objects/" + url.PathEscape(sh.slug()) + "/records"
	deleteRetries := 0
	for attempt := 0; ; attempt++ {
		req := d.client.R(ctx)
		if values != nil {
			req = req.SetBody(map[string]interface{}{"data": map[string]interface{}{"values": values}})
		}
		var resp *httpclient.Response
		var err error
		switch action {
		case "create":
			// Create is not idempotent: retrying after a server-side commit would
			// duplicate the record, so only a 429 (never processed) is retried.
			resp, err = req.SetRetryOnRateLimitOnly().Post(base)
		case "upsert":
			resp, err = req.SetQueryParam("matching_attribute", sh.matchAttr).Put(base)
		case "update":
			resp, err = req.Put(base + "/" + url.PathEscape(recordID))
		case "delete":
			// Retried here rather than by the client, so a 404 after a failed
			// attempt reads as already deleted instead of a missing record.
			resp, err = req.SetRetryOnRateLimitOnly().Delete(base + "/" + url.PathEscape(recordID))
			if (err != nil || resp.StatusCode() >= 500) && deleteRetries < retryCount {
				if err := sleepCtx(ctx, retryBackoff(deleteRetries)); err != nil {
					return err
				}
				deleteRetries++
				continue
			}
			if err == nil && resp.StatusCode() == 404 && deleteRetries > 0 {
				return nil
			}
		}
		if err != nil {
			return fmt.Errorf("attio %s %s request failed: %w", action, sh.slug(), err)
		}
		if resp.IsSuccess() {
			if sh.written != nil {
				var parsed struct {
					Data queriedRecord `json:"data"`
				}
				if json.Unmarshal(resp.Body(), &parsed) == nil && parsed.Data.ID.RecordID != "" {
					sh.written.Store(parsed.Data.ID.RecordID, struct{}{})
				}
			}
			return nil
		}

		apiErr := parseAPIError(resp)
		apiErr.message = d.nameAttributes(apiErr.message)
		if apiErr.code == conflictCode && attempt < conflictRetries {
			config.Debug("[ATTIO DEST] retrying %s %s after %s (attempt %d)", action, ident, conflictCode, attempt+1)
			if err := sleepCtx(ctx, retryBackoff(attempt)); err != nil {
				return err
			}
			continue
		}
		if rej, ok := rowRejection(action, apiErr); ok {
			rej.identifier = ident
			return sh.reject(rejects, rej)
		}
		return fmt.Errorf("attio %s %s failed: %w", action, sh.slug(), apiErr)
	}
}

var quotedUUID = regexp.MustCompile(`"([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})"`)

// nameAttributes follows each attribute id in an Attio error message with the
// attribute's object, slug, title and type, since the bare id means nothing to a user.
func (d *AttioDestination) nameAttributes(msg string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return quotedUUID.ReplaceAllStringFunc(msg, func(quoted string) string {
		id := strings.Trim(quoted, `"`)
		for _, m := range d.metas {
			a, ok := m.byID[id]
			if !ok {
				continue
			}
			parts := []string{m.slug + "." + a.APISlug}
			if a.Title != "" {
				parts = append(parts, strconv.Quote(a.Title))
			}
			parts = append(parts, a.Type)
			return quoted + " (" + strings.Join(parts, ", ") + ")"
		}
		return quoted
	})
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// systemicCodes fail every row alike (plan or feature limits), so they abort
// the run instead of producing one reject per row.
var systemicCodes = map[string]bool{"particle_gate_violation": true}

// rowRejection reports whether an error belongs to the record rather than the
// run. A 404 is a missing record only when the request addressed one by id.
func rowRejection(action string, e *apiError) (rejection, bool) {
	switch {
	case systemicCodes[e.code]:
		return rejection{}, false
	case e.status == 404 && (action == "update" || action == "delete"):
		return rejection{code: notFoundCode, message: e.message}, true
	case e.status == 400 || e.status == 409 || e.status == 422:
		return rejection{code: e.code, message: e.message}, true
	default:
		return rejection{}, false
	}
}

type apiError struct {
	status  int
	typ     string
	code    string
	message string
}

func (e *apiError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("status %d: %s: %s", e.status, e.code, e.message)
	}
	return fmt.Sprintf("status %d: %s", e.status, e.message)
}

func parseAPIError(resp *httpclient.Response) *apiError {
	var body struct {
		Type             string `json:"type"`
		Code             string `json:"code"`
		Message          string `json:"message"`
		ValidationErrors []struct {
			Path    []interface{} `json:"path"`
			Message string        `json:"message"`
		} `json:"validation_errors"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err == nil && (body.Code != "" || body.Message != "") {
		msg := body.Message
		// The top-level message is generic ("Body payload validation error."); the
		// per-field details say what was wrong.
		for _, v := range body.ValidationErrors {
			parts := make([]string, 0, len(v.Path))
			for _, p := range v.Path {
				parts = append(parts, fmt.Sprint(p))
			}
			if len(parts) > 0 {
				msg += fmt.Sprintf(" %s: %s.", strings.Join(parts, "."), v.Message)
			} else {
				msg += " " + v.Message
			}
		}
		return &apiError{status: resp.StatusCode(), typ: body.Type, code: body.Code, message: msg}
	}
	return &apiError{status: resp.StatusCode(), message: resp.String()}
}

// finalizeAndReport runs the mirror sweep and reports rejects. Under fail mode a
// run with rejects skips the destructive sweep.
func (d *AttioDestination) finalizeAndReport(ctx context.Context, sh *shaper, workers int, rejects *rejectionLog) error {
	if sh.mirror && !sh.skip() && rejects.len() > 0 {
		return reportRejections(sh, rejects)
	}
	if err := d.finalizeMirror(ctx, sh, workers, rejects); err != nil {
		if rejErr := reportRejections(sh, rejects); rejErr != nil {
			return errors.Join(err, rejErr)
		}
		return err
	}
	return reportRejections(sh, rejects)
}

// finalizeMirror deletes every record whose match value was not in the source.
func (d *AttioDestination) finalizeMirror(ctx context.Context, sh *shaper, workers int, rejects *rejectionLog) error {
	if !sh.mirror {
		return nil
	}
	if !sh.sawSource.Load() {
		output.Warnf("Warning: attio replace (mirror) of %s: source produced 0 rows; skipping the delete sweep so an empty extract does not remove every record. Use --incremental-strategy delete to remove records intentionally.\n", sh.slug())
		return nil
	}
	if rejects.len() > 0 && !anyStored(sh.written) {
		output.Warnf("Warning: attio replace (mirror) of %s: every source row was rejected; skipping the delete sweep so a load that wrote nothing does not remove existing records.\n", sh.slug())
		return nil
	}

	var stale []string
	err := d.queryRecords(ctx, sh.slug(), nil, func(rec queriedRecord) {
		id := rec.ID.RecordID
		if _, ok := sh.written.Load(id); ok || id == "" {
			return
		}
		for _, v := range storedValues(rec.Values[sh.matchAttr]) {
			if _, ok := sh.seen.Load(matchKey(sh.matchValue(v))); ok {
				return
			}
		}
		stale = append(stale, id)
	})
	if err != nil {
		return fmt.Errorf("attio mirror: failed to list existing %s records: %w", sh.slug(), err)
	}
	if len(stale) == 0 {
		return nil
	}
	config.Debug("[ATTIO DEST] mirror deleting %d stale %s record(s)", len(stale), sh.slug())

	tasks := make([]rowTask, 0, len(stale))
	for _, id := range stale {
		tasks = append(tasks, rowTask{key: id, run: func(ctx context.Context) error {
			return d.sendRow(ctx, sh, "delete", id, nil, recordIDAttr+"="+id, rejects)
		}})
	}
	return runTasks(ctx, workers, tasks)
}

func anyStored(m *sync.Map) bool {
	found := false
	m.Range(func(_, _ any) bool {
		found = true
		return false
	})
	return found
}

const maxReportedRejections = 100

type rejection struct {
	code       string
	message    string
	identifier string
}

type rejectionLog struct {
	mu    sync.Mutex
	items []rejection
}

func (l *rejectionLog) add(r rejection) {
	l.mu.Lock()
	l.items = append(l.items, r)
	l.mu.Unlock()
}

func (l *rejectionLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

// reject records a rejected row, or aborts under fail_fast.
func (s *shaper) reject(rejects *rejectionLog, r rejection) error {
	if s.failFast() {
		id := ""
		if r.identifier != "" {
			id = " [" + r.identifier + "]"
		}
		return fmt.Errorf("attio rejected a %s record%s: (%s) %s", s.slug(), id, r.code, r.message)
	}
	rejects.add(r)
	return nil
}

func reportRejections(sh *shaper, l *rejectionLog) error {
	l.mu.Lock()
	items := slices.Clone(l.items)
	l.mu.Unlock()
	if len(items) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "attio rejected %d %s record(s):", len(items), sh.slug())
	for i := 0; i < min(len(items), maxReportedRejections); i++ {
		id := ""
		if items[i].identifier != "" {
			id = " [" + items[i].identifier + "]"
		}
		fmt.Fprintf(&b, "\n  - (%s)%s %s", items[i].code, id, items[i].message)
	}
	if len(items) > maxReportedRejections {
		fmt.Fprintf(&b, "\n  ... and %d more", len(items)-maxReportedRejections)
	}
	if sh.createOnly && slices.ContainsFunc(items, func(r rejection) bool { return strings.Contains(strings.ToLower(r.message), "uniqu") }) {
		b.WriteString("\n  hint: use --incremental-strategy merge with matching_attribute=<unique attribute> to update existing records instead of creating them")
	}

	if sh.skip() {
		output.Deferf("Warning: %s\n", b.String())
		return nil
	}
	return errors.New(b.String())
}

func (d *AttioDestination) SwapTable(_ context.Context, _ destination.SwapOptions) error {
	return errors.New("attio destination does not support atomic swap")
}

func (d *AttioDestination) MergeTable(_ context.Context, _ destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for attio destination; set matching_attribute=<unique attribute> on the dest-table to upsert")
}

func (d *AttioDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for attio destination")
}

func (d *AttioDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for attio destination")
}

func (d *AttioDestination) DropTable(_ context.Context, _ string) error {
	return errors.New("attio destination does not support dropping data")
}

func (d *AttioDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for attio destination")
}

func (d *AttioDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for attio destination")
}

func (d *AttioDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *AttioDestination) GetScheme() string { return "attio" }

// IsReverseETL marks Attio as a reverse-ETL destination.
func (d *AttioDestination) IsReverseETL() {}

// RequiresVerbatimColumns keeps dotted reference columns (company.domains) intact;
// attribute slugs are matched case-insensitively instead.
func (d *AttioDestination) RequiresVerbatimColumns() {}

// RequiresExplicitStrategy: the framework default "replace" would mirror the
// object and permanently delete records not in the source.
func (d *AttioDestination) RequiresExplicitStrategy() {}

func (d *AttioDestination) SupportsReplaceStrategy() bool      { return true }
func (d *AttioDestination) SupportsAppendStrategy() bool       { return true }
func (d *AttioDestination) SupportsMergeStrategy() bool        { return false }
func (d *AttioDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *AttioDestination) SupportsSCD2Strategy() bool         { return false }
func (d *AttioDestination) SupportsAtomicSwap() bool           { return false }

var (
	_ destination.Destination           = (*AttioDestination)(nil)
	_ destination.ReverseETLDestination = (*AttioDestination)(nil)
)
