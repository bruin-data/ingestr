package clevertap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
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
	uploadEndpoint = "/1/upload"
	// CleverTap accepts up to 1000 records per /1/upload call.
	uploadBatchSize = 1000
	// Concurrent requests to the Upload API are capped at 3 per account.
	rateLimit          = 3.0
	rateLimitBurst     = 3
	defaultParallelism = 3
)

// validRegions maps a region code to its API host prefix. Europe uses the
// unprefixed host and the dashboard labels it "global", so both are accepted.
var validRegions = map[string]string{
	"eu1":    "",
	"global": "",
	"in1":    "in1",
	"us1":    "us1",
	"sg1":    "sg1",
	"aps3":   "aps3",
	"mec1":   "mec1",
}

// validIDTypes are the identifier fields CleverTap resolves a user by.
var validIDTypes = map[string]bool{
	"identity": true,
	"objectId": true,
	"FBID":     true,
	"GPID":     true,
}

type ctConfig struct {
	accountID string
	passcode  string
	region    string
	// endpoint overrides the region-derived base URL (self-hosted proxy or tests).
	endpoint string
}

type CleverTapDestination struct {
	client *httpclient.Client
}

func NewCleverTapDestination() *CleverTapDestination {
	return &CleverTapDestination{}
}

func (d *CleverTapDestination) Schemes() []string {
	return []string{"clevertap"}
}

func parseURI(uri string) (ctConfig, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ctConfig{}, fmt.Errorf("invalid clevertap URI: %w", err)
	}
	if parsed.Scheme != "clevertap" {
		return ctConfig{}, fmt.Errorf("invalid clevertap URI: must start with clevertap://")
	}

	params := parsed.Query()
	accountID := params.Get("account_id")
	if accountID == "" {
		return ctConfig{}, fmt.Errorf("account_id is required in clevertap URI")
	}
	passcode := params.Get("passcode")
	if passcode == "" {
		return ctConfig{}, fmt.Errorf("passcode is required in clevertap URI")
	}
	region := params.Get("region")
	if region == "" {
		region = "eu1"
	}
	if _, ok := validRegions[region]; !ok {
		return ctConfig{}, fmt.Errorf("invalid region %q: must be one of eu1 (or global), in1, us1, sg1, aps3, mec1", region)
	}

	return ctConfig{accountID: accountID, passcode: passcode, region: region, endpoint: params.Get("endpoint")}, nil
}

// regionBaseURL maps a region code to its API host; Europe is the unprefixed default.
func regionBaseURL(region string) string {
	prefix, ok := validRegions[region]
	if !ok || prefix == "" {
		return "https://api.clevertap.com"
	}
	return fmt.Sprintf("https://%s.api.clevertap.com", prefix)
}

func (d *CleverTapDestination) Connect(ctx context.Context, uri string) error {
	cfg, err := parseURI(uri)
	if err != nil {
		return err
	}

	baseURL := regionBaseURL(cfg.region)
	if cfg.endpoint != "" {
		baseURL = cfg.endpoint
	}

	d.client = httpclient.New(
		httpclient.WithBaseURL(baseURL),
		httpclient.WithTimeout(60*time.Second),
		httpclient.WithRateLimiter(rateLimit, rateLimitBurst),
		httpclient.WithDebug(config.DebugMode),
		httpclient.WithHeader("X-CleverTap-Account-Id", cfg.accountID),
		httpclient.WithHeader("X-CleverTap-Passcode", cfg.passcode),
		httpclient.WithHeader("Content-Type", "application/json; charset=utf-8"),
	)

	config.Debug("[CLEVERTAP DEST] Connected to region %s", cfg.region)
	return nil
}

func (d *CleverTapDestination) Close(_ context.Context) error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// tableParams are the record-shaping options carried on the --dest-table string,
// e.g. "profiles?identity_column=email" or "events?identity_column=user_id&event_name=Charged".
type tableParams struct {
	Identity        string `mapstructure:"identity"`
	IdentityColumn  string `mapstructure:"identity_column"`
	IDType          string `mapstructure:"id_type"`
	TS              string `mapstructure:"ts"`
	EventName       string `mapstructure:"event_name"`
	EventNameColumn string `mapstructure:"event_name_column"`
	OnError         string `mapstructure:"on_error"`
}

// shaper turns a source row into a CleverTap upload record for one dest-table mode.
type shaper struct {
	recordType   string // "profile" or "event"
	identityCol  string
	idValue      string // a constant identifier applied to every row, instead of identityCol
	idType       string
	tsCol        string
	eventName    string
	eventNameCol string
	exclude      map[string]bool
	onErrorSkip  bool
}

func parseShaper(table string) (*shaper, error) {
	var p tableParams
	path, _, err := tablespec.Parse(table, &p, tablespec.WithListSeparator(","))
	if err != nil {
		return nil, err
	}

	// Tolerate an optional schema qualifier ("clevertap.profiles" -> "profiles").
	recordPath := path
	if i := strings.LastIndex(recordPath, "."); i >= 0 {
		recordPath = recordPath[i+1:]
	}

	var recordType string
	switch strings.ToLower(strings.TrimSpace(recordPath)) {
	case "profiles", "profile":
		recordType = "profile"
	case "events", "event":
		recordType = "event"
	default:
		return nil, fmt.Errorf("clevertap dest-table must be \"profiles\" or \"events\", got %q", path)
	}

	identityCol := p.IdentityColumn
	if identityCol != "" && p.Identity != "" {
		return nil, fmt.Errorf("clevertap: set either identity=<constant> or identity_column=<column>, not both")
	}
	if identityCol == "" && p.Identity == "" {
		return nil, fmt.Errorf("clevertap: set identity_column=<column> or identity=<constant> on the dest-table")
	}
	idType := p.IDType
	if idType == "" {
		idType = "identity"
	}
	if !validIDTypes[idType] {
		return nil, fmt.Errorf("invalid id_type %q: must be one of identity, objectId, FBID, GPID", idType)
	}

	if recordType == "event" && p.EventName == "" && p.EventNameColumn == "" {
		return nil, fmt.Errorf("clevertap events require event_name=<name> or event_name_column=<column> on the dest-table")
	}
	if recordType == "profile" && p.TS != "" {
		return nil, fmt.Errorf("clevertap ts is only supported for events; profile records have no timestamp field")
	}

	// Never upload ingestr's own decoration columns as CleverTap attributes; they
	// are added after the source read, so --sql-exclude-columns cannot drop them.
	exclude := map[string]bool{
		naming.IngestrLoadedAtColumn: true,
		naming.IngestrRunIDColumn:    true,
	}

	if p.OnError != "" && p.OnError != "skip" && p.OnError != "fail" {
		return nil, fmt.Errorf("invalid on_error %q: must be \"fail\" (default) or \"skip\"", p.OnError)
	}

	return &shaper{
		recordType:   recordType,
		identityCol:  identityCol,
		idValue:      p.Identity,
		idType:       idType,
		tsCol:        p.TS,
		eventName:    p.EventName,
		eventNameCol: p.EventNameColumn,
		exclude:      exclude,
		onErrorSkip:  p.OnError == "skip",
	}, nil
}

// validateColumns fails fast when the source is missing a column the mode needs.
func (s *shaper) validateColumns(record arrow.RecordBatch) error {
	present := make(map[string]bool, record.NumCols())
	names := make([]string, 0, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		present[name] = true
		names = append(names, name)
	}

	if s.identityCol != "" && !present[s.identityCol] {
		return fmt.Errorf("clevertap: identity column %q not found in source (available: %s); set identity_column= on the dest-table", s.identityCol, strings.Join(names, ", "))
	}
	if s.eventNameCol != "" && !present[s.eventNameCol] {
		return fmt.Errorf("clevertap: event_name_column %q not found in source (available: %s)", s.eventNameCol, strings.Join(names, ", "))
	}
	if s.tsCol != "" && !present[s.tsCol] {
		return fmt.Errorf("clevertap: ts column %q not found in source (available: %s)", s.tsCol, strings.Join(names, ", "))
	}
	return nil
}

// shape builds the upload record for one row, or returns ok=false when the row
// has no identity value and cannot be attached to a user.
func (s *shaper) shape(record arrow.RecordBatch, colIndex map[string]int, row int) (map[string]interface{}, bool) {
	var idVal interface{} = s.idValue
	if s.identityCol != "" {
		idVal = arrowToValue(record.Column(colIndex[s.identityCol]), row)
	}
	if idVal == nil || idVal == "" {
		return nil, false
	}

	item := map[string]interface{}{
		"type":   s.recordType,
		s.idType: idVal,
	}

	if s.tsCol != "" {
		if ts, ok := epochSeconds(record.Column(colIndex[s.tsCol]), row); ok {
			item["ts"] = ts
		}
	}

	data := make(map[string]interface{})
	for i := 0; i < int(record.NumCols()); i++ {
		name := record.ColumnName(i)
		if name == s.identityCol || name == s.tsCol || name == s.eventNameCol || s.exclude[name] {
			continue
		}
		if v := arrowToValue(record.Column(i), row); v != nil {
			data[name] = v
		}
	}

	if s.recordType == "profile" {
		item["profileData"] = data
	} else {
		name := s.eventName
		if s.eventNameCol != "" {
			if v := arrowToValue(record.Column(colIndex[s.eventNameCol]), row); v != nil {
				name = fmt.Sprintf("%v", v)
			}
		}
		item["evtName"] = name
		item["evtData"] = data
	}

	return item, true
}

func (d *CleverTapDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
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
	config.Debug("[CLEVERTAP DEST] Uploaded %d %s record(s)", totalRows, sh.recordType)
	return reportRejections(sh, &rejects)
}

func (d *CleverTapDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
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
	if n := skipped.Load(); n > 0 {
		output.Warnf("Warning: clevertap skipped %d %s record(s) with a missing %s value\n", n, sh.recordType, sh.identityCol)
	}
}

// maxReportedRejections caps how many rejected records are listed in the final
// report so a large failure does not produce an unbounded message.
const maxReportedRejections = 100

// rejection is one record CleverTap refused, with the original data it returned.
type rejection struct {
	code    int
	message string
	record  json.RawMessage
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
// rejected record and its error once all batches have been uploaded.
func reportRejections(sh *shaper, l *rejectionLog) error {
	l.mu.Lock()
	items := l.items
	l.mu.Unlock()
	if len(items) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "clevertap rejected %d %s record(s):", len(items), sh.recordType)
	shown := min(len(items), maxReportedRejections)
	for i := 0; i < shown; i++ {
		fmt.Fprintf(&b, "\n  - (code %d) %s: %s", items[i].code, items[i].message, string(items[i].record))
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

// writeBatch shapes a record batch and uploads it in chunks of uploadBatchSize.
func (d *CleverTapDestination) writeBatch(ctx context.Context, sh *shaper, record arrow.RecordBatch, skipped *atomic.Int64, rejects *rejectionLog) (int64, error) {
	if err := sh.validateColumns(record); err != nil {
		return 0, err
	}

	colIndex := make(map[string]int, record.NumCols())
	for i := 0; i < int(record.NumCols()); i++ {
		colIndex[record.ColumnName(i)] = i
	}

	rows := int(record.NumRows())
	batch := make([]map[string]interface{}, 0, min(rows, uploadBatchSize))
	var uploaded int64

	for row := 0; row < rows; row++ {
		item, ok := sh.shape(record, colIndex, row)
		if !ok {
			skipped.Add(1)
			continue
		}
		batch = append(batch, item)
		if len(batch) == uploadBatchSize {
			if err := d.upload(ctx, sh, batch, rejects); err != nil {
				return uploaded, err
			}
			uploaded += int64(len(batch))
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := d.upload(ctx, sh, batch, rejects); err != nil {
			return uploaded, err
		}
		uploaded += int64(len(batch))
	}

	return uploaded, nil
}

// upload posts one batch to /1/upload, prints a per-batch warning on any
// rejection, and records the rejected data for the final report.
func (d *CleverTapDestination) upload(ctx context.Context, sh *shaper, items []map[string]interface{}, rejects *rejectionLog) error {
	resp, err := d.client.R(ctx).SetBody(map[string]interface{}{"d": items}).Post(uploadEndpoint)
	if err != nil {
		return fmt.Errorf("clevertap upload request failed: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("clevertap upload returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var body struct {
		Status      string `json:"status"`
		Processed   int    `json:"processed"`
		Unprocessed []struct {
			Status string          `json:"status"`
			Code   int             `json:"code"`
			Error  string          `json:"error"`
			Record json.RawMessage `json:"record"`
		} `json:"unprocessed"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return fmt.Errorf("failed to parse clevertap upload response: %w", err)
	}
	if body.Status == "fail" {
		return fmt.Errorf("clevertap upload failed: %s", body.Error)
	}

	if len(body.Unprocessed) > 0 {
		first := body.Unprocessed[0]
		output.Warnf("Warning: clevertap rejected %d of %d record(s) in this batch; first error (code %d): %s\n", len(body.Unprocessed), len(items), first.Code, first.Error)
		batch := make([]rejection, 0, len(body.Unprocessed))
		for _, u := range body.Unprocessed {
			batch = append(batch, rejection{code: u.Code, message: u.Error, record: u.Record})
		}
		rejects.add(batch)
	}

	return nil
}

func (d *CleverTapDestination) PrepareTable(_ context.Context, _ destination.PrepareOptions) error {
	return nil
}

func (d *CleverTapDestination) SwapTable(_ context.Context, _ destination.SwapOptions) error {
	return errors.New("clevertap destination does not support atomic swap")
}

func (d *CleverTapDestination) MergeTable(_ context.Context, _ destination.MergeOptions) error {
	return errors.New("merge strategy is not supported for clevertap destination; CleverTap upserts by identity on append")
}

func (d *CleverTapDestination) DeleteInsertTable(_ context.Context, _ destination.DeleteInsertOptions) error {
	return errors.New("delete+insert strategy is not supported for clevertap destination")
}

func (d *CleverTapDestination) SCD2Table(_ context.Context, _ destination.SCD2Options) error {
	return errors.New("scd2 strategy is not supported for clevertap destination")
}

func (d *CleverTapDestination) DropTable(_ context.Context, _ string) error {
	return errors.New("clevertap destination does not support dropping data")
}

func (d *CleverTapDestination) Exec(_ context.Context, _ string, _ ...interface{}) error {
	return errors.New("exec is not supported for clevertap destination")
}

func (d *CleverTapDestination) BeginTransaction(_ context.Context) (destination.Transaction, error) {
	return nil, errors.New("transactions are not supported for clevertap destination")
}

func (d *CleverTapDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return nil, nil
}

func (d *CleverTapDestination) GetScheme() string { return "clevertap" }

// SupportsReplaceStrategy is true because CleverTap has no destructive delete;
// replace degrades to a full upload, which upserts profiles by identity.
func (d *CleverTapDestination) SupportsReplaceStrategy() bool      { return true }
func (d *CleverTapDestination) SupportsAppendStrategy() bool       { return true }
func (d *CleverTapDestination) SupportsMergeStrategy() bool        { return false }
func (d *CleverTapDestination) SupportsDeleteInsertStrategy() bool { return false }
func (d *CleverTapDestination) SupportsSCD2Strategy() bool         { return false }
func (d *CleverTapDestination) SupportsAtomicSwap() bool           { return false }

// epochSeconds converts a timestamp column value to CleverTap's Unix-seconds ts.
func epochSeconds(arr arrow.Array, idx int) (int64, bool) {
	if arr.IsNull(idx) {
		return 0, false
	}
	switch a := arr.(type) {
	case *array.Timestamp:
		return a.Value(idx).ToTime(arrow.Microsecond).Unix(), true
	case *array.Date32:
		return a.Value(idx).ToTime().Unix(), true
	case *array.Date64:
		return a.Value(idx).ToTime().Unix(), true
	case *array.Int64:
		return a.Value(idx), true
	case *array.Int32:
		return int64(a.Value(idx)), true
	default:
		return 0, false
	}
}

func arrowToValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	if ext, ok := arr.DataType().(arrow.ExtensionType); ok {
		if ext.ExtensionName() == schema.JSONExtensionName {
			val := arrowutil.Value(arr, idx)
			str, ok := val.(string)
			if !ok || str == "" {
				return val
			}
			var decoded interface{}
			if err := json.Unmarshal([]byte(str), &decoded); err != nil {
				return str
			}
			return decoded
		}
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return int64(a.Value(idx))
	case *array.Int16:
		return int64(a.Value(idx))
	case *array.Int32:
		return int64(a.Value(idx))
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint16:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint32:
		return convertUint(uint64(a.Value(idx)))
	case *array.Uint64:
		return convertUint(a.Value(idx))
	case *array.Float32:
		return float64(a.Value(idx))
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Decimal128:
		val := a.Value(idx)
		if dt, ok := a.DataType().(*arrow.Decimal128Type); ok {
			return val.ToString(dt.Scale)
		}
		return val.ToString(0)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Timestamp:
		return a.Value(idx).ToTime(arrow.Microsecond).Format(time.RFC3339Nano)
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
	case array.ExtensionArray:
		return arrowutil.Value(a.Storage(), idx)
	default:
		return arrowutil.Value(arr, idx)
	}
}

func convertUint(v uint64) interface{} {
	if v <= math.MaxInt64 {
		return int64(v)
	}
	return fmt.Sprintf("%d", v)
}

var _ destination.Destination = (*CleverTapDestination)(nil)
