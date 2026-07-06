package source

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
)

type ExtractPartitionWindow struct {
	Start time.Time
	End   time.Time
}

type ExtractNumericPartitionWindow struct {
	Start int64
	End   int64
}

type ExtractPartitionKind int

const (
	ExtractPartitionKindUnknown ExtractPartitionKind = iota
	ExtractPartitionKindTime
	ExtractPartitionKindNumeric
)

type extractPartitionJob struct {
	Window        ExtractPartitionWindow
	NumericWindow ExtractNumericPartitionWindow
	EndInclusive  bool
	IsNull        bool
	Kind          ExtractPartitionKind
}

type ExtractPartitionBounds struct {
	Start        time.Time
	End          time.Time
	NumericStart int64
	NumericEnd   int64
	Kind         ExtractPartitionKind
	HasRange     bool
	HasNulls     bool
}

const (
	extractAutoPartitionsPerWorker = 4
	maxExtractPartitionWindows     = 100000
	extractPartitionReadBufferSize = 1
)

type (
	ExtractPartitionReadFunc   func(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error)
	ExtractPartitionBoundsFunc func(ctx context.Context, opts ReadOptions) (ExtractPartitionBounds, error)
)

func (o ReadOptions) ExtractPartitioningEnabled() bool {
	return o.ExtractPartitionBy != "" || o.ExtractPartitionInterval != 0 || o.ExtractPartitionNumericInterval != 0 || o.ExtractPartitionAuto
}

func (o ReadOptions) ExtractPartitionBoundsDiscoveryEnabled() bool {
	if o.ExtractPartitionKind == ExtractPartitionKindNumeric {
		return true
	}
	return o.IncrementalKey != "" && o.ExtractPartitionBy != "" && !strings.EqualFold(o.IncrementalKey, o.ExtractPartitionBy)
}

func (o ReadOptions) ExtractPartitionEndOperator() string {
	if o.ExtractPartitionEndInclusive {
		return "<="
	}
	return "<"
}

func ValidateExtractPartitionColumn(tableSchema *schema.TableSchema, columnName string) (string, error) {
	col, _, err := extractPartitionColumn(tableSchema, columnName)
	if err != nil {
		return "", err
	}
	return col.Name, nil
}

func tableColumnDataType(tableSchema *schema.TableSchema, columnName string) schema.DataType {
	if tableSchema == nil || columnName == "" {
		return schema.TypeUnknown
	}
	for _, col := range tableSchema.Columns {
		if strings.EqualFold(col.Name, columnName) {
			return col.DataType
		}
	}
	return schema.TypeUnknown
}

func extractPartitionColumn(tableSchema *schema.TableSchema, columnName string) (schema.Column, ExtractPartitionKind, error) {
	if tableSchema == nil {
		return schema.Column{}, ExtractPartitionKindUnknown, fmt.Errorf("extract partitioning requires a known source schema")
	}
	for _, col := range tableSchema.Columns {
		if strings.EqualFold(col.Name, columnName) {
			switch col.DataType {
			case schema.TypeDate, schema.TypeTimestamp, schema.TypeTimestampTZ:
				return col, ExtractPartitionKindTime, nil
			case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
				return col, ExtractPartitionKindNumeric, nil
			default:
				return schema.Column{}, ExtractPartitionKindUnknown, fmt.Errorf("extract partition column %q must be date, timestamp_ntz, timestamp, or integer; got %s", columnName, col.DataType)
			}
		}
	}
	return schema.Column{}, ExtractPartitionKindUnknown, fmt.Errorf("extract partition column %q was not found in source schema", columnName)
}

func ExtractPartitionWindows(start, end time.Time, interval time.Duration) ([]ExtractPartitionWindow, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("extract partition interval must be positive")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("extract partition start must be earlier than end")
	}

	var windows []ExtractPartitionWindow
	for cur := start; cur.Before(end); {
		next := cur.Add(interval)
		if next.After(end) {
			next = end
		}
		if len(windows) >= maxExtractPartitionWindows {
			return nil, fmt.Errorf("extract partitioning would create more than %d windows; increase extract-partition-interval or use auto", maxExtractPartitionWindows)
		}
		windows = append(windows, ExtractPartitionWindow{Start: cur, End: next})
		cur = next
	}
	return windows, nil
}

func ExtractNumericPartitionWindows(start, end, interval int64) ([]ExtractNumericPartitionWindow, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("extract partition numeric interval must be positive")
	}
	if end < start {
		return nil, fmt.Errorf("extract partition numeric start must be earlier than or equal to end")
	}
	if start == end {
		return []ExtractNumericPartitionWindow{{Start: start, End: end}}, nil
	}

	var windows []ExtractNumericPartitionWindow
	for cur := start; cur <= end; {
		next := end
		if cur <= math.MaxInt64-interval {
			next = cur + interval
		}
		if next >= end {
			if len(windows) >= maxExtractPartitionWindows {
				return nil, fmt.Errorf("extract partitioning would create more than %d windows; increase extract-partition-interval or use auto", maxExtractPartitionWindows)
			}
			windows = append(windows, ExtractNumericPartitionWindow{Start: cur, End: end})
			break
		}
		if len(windows) >= maxExtractPartitionWindows {
			return nil, fmt.Errorf("extract partitioning would create more than %d windows; increase extract-partition-interval or use auto", maxExtractPartitionWindows)
		}
		windows = append(windows, ExtractNumericPartitionWindow{Start: cur, End: next})
		cur = next
	}
	return windows, nil
}

func ReadExtractPartitions(ctx context.Context, opts ReadOptions, tableSchema *schema.TableSchema, read ExtractPartitionReadFunc, discover ExtractPartitionBoundsFunc) (<-chan RecordBatchResult, error) {
	if opts.IncrementalKeyDataType == schema.TypeUnknown {
		opts.IncrementalKeyDataType = tableColumnDataType(tableSchema, opts.IncrementalKey)
	}
	if !opts.ExtractPartitioningEnabled() {
		return read(ctx, opts)
	}
	partitionColumn, kind, err := extractPartitionColumn(tableSchema, opts.ExtractPartitionBy)
	if err != nil {
		return nil, err
	}
	opts.ExtractPartitionBy = partitionColumn.Name
	opts.ExtractPartitionKind = kind
	opts.ExtractPartitionDataType = partitionColumn.DataType
	if opts.IntervalStart == nil || opts.IntervalEnd == nil {
		return nil, fmt.Errorf("extract partitioning requires interval start and end")
	}
	if opts.ExtractPartitionAuto {
		if opts.ExtractPartitionInterval != 0 || opts.ExtractPartitionNumericInterval != 0 {
			return nil, fmt.Errorf("extract partition interval must be auto, a duration, or an integer step")
		}
	} else {
		if kind == ExtractPartitionKindTime && opts.ExtractPartitionInterval <= 0 {
			return nil, fmt.Errorf("extract partition interval must be a positive duration for time partition column %q", opts.ExtractPartitionBy)
		}
		if kind == ExtractPartitionKindTime && partitionColumn.DataType == schema.TypeDate && opts.ExtractPartitionInterval < 24*time.Hour {
			return nil, fmt.Errorf("extract partition interval must be at least 24h for date partition column %q", opts.ExtractPartitionBy)
		}
		if kind == ExtractPartitionKindNumeric && opts.ExtractPartitionNumericInterval <= 0 {
			return nil, fmt.Errorf("extract partition interval must be a positive integer for numeric partition column %q", opts.ExtractPartitionBy)
		}
	}
	if kind == ExtractPartitionKindNumeric && strings.TrimSpace(opts.IncrementalKey) == "" {
		return nil, fmt.Errorf("numeric extract partitioning requires an incremental key to discover bounded partition ranges")
	}

	jobsToRun, err := extractPartitionJobs(ctx, opts, discover)
	if err != nil {
		return nil, err
	}
	if len(jobsToRun) == 0 {
		return closedRecordBatchResults(), nil
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	if parallelism > len(jobsToRun) {
		parallelism = len(jobsToRun)
	}

	ctx, cancel := context.WithCancel(ctx)
	out := make(chan RecordBatchResult, parallelism)
	jobs := make(chan extractPartitionJob)

	var wg sync.WaitGroup
	var once sync.Once
	sendErr := func(err error) {
		once.Do(func() {
			select {
			case out <- RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
			cancel()
		})
	}

	wg.Add(parallelism)
	for i := 0; i < parallelism; i++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				windowOpts := opts
				if job.IsNull {
					windowOpts.ExtractPartitionStart = nil
					windowOpts.ExtractPartitionEnd = nil
					windowOpts.ExtractPartitionNumericStart = nil
					windowOpts.ExtractPartitionNumericEnd = nil
					windowOpts.ExtractPartitionEndInclusive = false
					windowOpts.ExtractPartitionIsNull = true
				} else {
					if job.Kind == ExtractPartitionKindNumeric {
						start := job.NumericWindow.Start
						end := job.NumericWindow.End
						windowOpts.ExtractPartitionStart = nil
						windowOpts.ExtractPartitionEnd = nil
						windowOpts.ExtractPartitionNumericStart = &start
						windowOpts.ExtractPartitionNumericEnd = &end
					} else {
						start := job.Window.Start
						end := job.Window.End
						windowOpts.ExtractPartitionStart = &start
						windowOpts.ExtractPartitionEnd = &end
						windowOpts.ExtractPartitionNumericStart = nil
						windowOpts.ExtractPartitionNumericEnd = nil
					}
					windowOpts.ExtractPartitionEndInclusive = job.EndInclusive
					windowOpts.ExtractPartitionIsNull = false
				}
				windowOpts.RecordBatchBufferSize = extractPartitionReadBufferSize
				windowOpts.Parallelism = 1

				records, err := read(ctx, windowOpts)
				if err != nil {
					sendErr(err)
					return
				}

				for result := range records {
					if result.Err != nil {
						releaseRecordBatchResult(result)
						sendErr(result.Err)
						go drainRecordBatchResults(records)
						return
					}
					select {
					case out <- result:
					case <-ctx.Done():
						releaseRecordBatchResult(result)
						go drainRecordBatchResults(records)
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, job := range jobsToRun {
			select {
			case jobs <- job:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		cancel()
		close(out)
	}()

	return out, nil
}

func extractPartitionJobs(ctx context.Context, opts ReadOptions, discover ExtractPartitionBoundsFunc) ([]extractPartitionJob, error) {
	bounds := ExtractPartitionBounds{
		Start:    *opts.IntervalStart,
		End:      *opts.IntervalEnd,
		Kind:     opts.ExtractPartitionKind,
		HasRange: true,
	}
	if opts.ExtractPartitionBoundsDiscoveryEnabled() {
		if discover == nil {
			return nil, fmt.Errorf("extract partition bounds discovery is required for numeric partition columns or when extract partition column differs from incremental key")
		}
		discovered, err := discover(ctx, opts)
		if err != nil {
			return nil, err
		}
		bounds = discovered
		if bounds.Kind == ExtractPartitionKindUnknown {
			bounds.Kind = opts.ExtractPartitionKind
		}
	}

	jobs := make([]extractPartitionJob, 0)
	if bounds.HasRange {
		if bounds.Kind == ExtractPartitionKindNumeric {
			if bounds.NumericEnd < bounds.NumericStart {
				return nil, fmt.Errorf("extract partition numeric bounds end must not be earlier than start")
			}
			interval := opts.ExtractPartitionNumericInterval
			if opts.ExtractPartitionAuto {
				var err error
				interval, err = autoExtractNumericPartitionInterval(bounds.NumericStart, bounds.NumericEnd, opts.Parallelism)
				if err != nil {
					return nil, err
				}
			}
			windows, err := ExtractNumericPartitionWindows(bounds.NumericStart, bounds.NumericEnd, interval)
			if err != nil {
				return nil, err
			}
			for _, window := range windows {
				jobs = append(jobs, extractPartitionJob{
					NumericWindow: window,
					EndInclusive:  window.End == bounds.NumericEnd,
					Kind:          ExtractPartitionKindNumeric,
				})
			}
		} else {
			if bounds.End.Before(bounds.Start) {
				return nil, fmt.Errorf("extract partition bounds end must not be earlier than start")
			}
			if bounds.Start.Equal(bounds.End) {
				jobs = append(jobs, extractPartitionJob{
					Window:       ExtractPartitionWindow{Start: bounds.Start, End: bounds.End},
					EndInclusive: true,
					Kind:         ExtractPartitionKindTime,
				})
			} else {
				interval := opts.ExtractPartitionInterval
				if opts.ExtractPartitionAuto {
					var err error
					interval, err = autoExtractPartitionInterval(bounds.Start, bounds.End, opts.Parallelism)
					if err != nil {
						return nil, err
					}
					if opts.ExtractPartitionDataType == schema.TypeDate && interval < 24*time.Hour {
						interval = 24 * time.Hour
					}
				}
				windows, err := ExtractPartitionWindows(bounds.Start, bounds.End, interval)
				if err != nil {
					return nil, err
				}
				for _, window := range windows {
					jobs = append(jobs, extractPartitionJob{
						Window:       window,
						EndInclusive: window.End.Equal(bounds.End),
						Kind:         ExtractPartitionKindTime,
					})
				}
			}
		}
	}
	if bounds.HasNulls {
		jobs = append(jobs, extractPartitionJob{IsNull: true, Kind: bounds.Kind})
	}
	return jobs, nil
}

func autoExtractPartitionInterval(start, end time.Time, parallelism int) (time.Duration, error) {
	if !start.Before(end) {
		return 0, fmt.Errorf("extract partition start must be earlier than end")
	}

	span := end.Sub(start)
	if span <= 0 {
		return 0, fmt.Errorf("extract partition interval range is too large")
	}

	target := extractAutoPartitionTarget(parallelism)
	interval := time.Duration(ceilDivInt64(int64(span), target))
	if interval < time.Second {
		interval = time.Second
	}
	return interval, nil
}

func autoExtractNumericPartitionInterval(start, end int64, parallelism int) (int64, error) {
	if end < start {
		return 0, fmt.Errorf("extract partition numeric start must be earlier than or equal to end")
	}

	width := new(big.Int).Sub(big.NewInt(end), big.NewInt(start))
	width.Add(width, big.NewInt(1))
	target := big.NewInt(extractAutoPartitionTarget(parallelism))
	step := new(big.Int).Sub(target, big.NewInt(1))
	step.Add(step, width)
	step.Div(step, target)

	if !step.IsInt64() {
		return math.MaxInt64, nil
	}
	if step.Sign() <= 0 {
		return 1, nil
	}
	return step.Int64(), nil
}

func extractAutoPartitionTarget(parallelism int) int64 {
	if parallelism <= 0 {
		parallelism = 4
	}
	if int64(parallelism) > math.MaxInt64/extractAutoPartitionsPerWorker {
		return math.MaxInt64
	}
	return int64(parallelism) * extractAutoPartitionsPerWorker
}

func ceilDivInt64(value, divisor int64) int64 {
	if divisor == 0 {
		return value
	}
	result := value / divisor
	remainder := value % divisor
	if remainder != 0 && ((remainder > 0) == (divisor > 0)) {
		result++
	}
	return result
}

func closedRecordBatchResults() <-chan RecordBatchResult {
	ch := make(chan RecordBatchResult)
	close(ch)
	return ch
}

// drainRecordBatchResults consumes any remaining results from an abandoned
// partition read. Source read goroutines send on a buffered channel without
// guarding on context cancellation, so once a worker stops consuming early (an
// error elsewhere or a cancelled context) the producer would block forever on a
// full channel and leak its goroutine and DB connection. Draining lets it run
// to completion and release its connection.
func drainRecordBatchResults(records <-chan RecordBatchResult) {
	for result := range records {
		releaseRecordBatchResult(result)
	}
}

func releaseRecordBatchResult(result RecordBatchResult) {
	if result.Batch != nil {
		result.Batch.Release()
	}
}

func SQLTimeRangeConditions(column string, start, end *time.Time, endOperator string, quote func(string) string, format func(time.Time) string) []string {
	return sqlTimeRangeConditions(column, start, end, endOperator, quote, format)
}

func sqlTimeRangeConditions(column string, start, end *time.Time, endOperator string, quote func(string) string, format func(time.Time) string) []string {
	if column == "" {
		return nil
	}

	var conditions []string
	if start != nil {
		conditions = append(conditions, fmt.Sprintf("%s >= '%s'", quote(column), format(*start)))
	}
	if end != nil {
		conditions = append(conditions, fmt.Sprintf("%s %s '%s'", quote(column), endOperator, format(*end)))
	}
	return conditions
}

func SQLDateRangeConditions(column string, start, end *time.Time, endOperator string, quote func(string) string) []string {
	return sqlTimeRangeConditions(column, start, end, endOperator, quote, SQLDateFormat)
}

func SQLTemporalRangeConditions(column string, dataType schema.DataType, start, end *time.Time, endOperator string, quote func(string) string, format func(time.Time) string) []string {
	switch dataType {
	case schema.TypeDate:
		return SQLDateRangeConditions(column, start, end, endOperator, quote)
	case schema.TypeTimestamp:
		return SQLTimeRangeConditions(column, start, end, endOperator, quote, NativeSQLTimeFormat)
	default:
		return SQLTimeRangeConditions(column, start, end, endOperator, quote, format)
	}
}

func SQLNumericRangeConditions(column string, start, end *int64, endOperator string, quote func(string) string) []string {
	if column == "" {
		return nil
	}

	var conditions []string
	if start != nil {
		conditions = append(conditions, fmt.Sprintf("%s >= %d", quote(column), *start))
	}
	if end != nil {
		conditions = append(conditions, fmt.Sprintf("%s %s %d", quote(column), endOperator, *end))
	}
	return conditions
}

func SQLExtractPartitionConditions(opts ReadOptions, quote func(string) string, format func(time.Time) string) []string {
	if opts.ExtractPartitionBy == "" {
		return nil
	}
	if opts.ExtractPartitionIsNull {
		return []string{fmt.Sprintf("%s IS NULL", quote(opts.ExtractPartitionBy))}
	}
	if opts.ExtractPartitionKind == ExtractPartitionKindNumeric {
		return SQLNumericRangeConditions(opts.ExtractPartitionBy, opts.ExtractPartitionNumericStart, opts.ExtractPartitionNumericEnd, opts.ExtractPartitionEndOperator(), quote)
	}
	return SQLTemporalRangeConditions(opts.ExtractPartitionBy, opts.ExtractPartitionDataType, opts.ExtractPartitionStart, opts.ExtractPartitionEnd, opts.ExtractPartitionEndOperator(), quote, format)
}

func SQLExtractPartitionBoundsQuery(table, partitionColumn, incrementalKey string, incrementalKeyDataType schema.DataType, intervalStart, intervalEnd *time.Time, quoteIdentifier, quoteTable func(string) string, format func(time.Time) string) string {
	partition := quoteIdentifier(partitionColumn)
	query := fmt.Sprintf("SELECT MIN(%s), MAX(%s), COUNT(*), COUNT(%s) FROM %s", partition, partition, partition, quoteTable(table))
	conditions := SQLTemporalRangeConditions(incrementalKey, incrementalKeyDataType, intervalStart, intervalEnd, "<=", quoteIdentifier, format)
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query
}

func ExtractPartitionBoundsFromValues(kind ExtractPartitionKind, minValue, maxValue any, totalCount, nonNullCount int64) (ExtractPartitionBounds, error) {
	if totalCount == 0 {
		return ExtractPartitionBounds{}, nil
	}

	bounds := ExtractPartitionBounds{Kind: kind, HasNulls: totalCount > nonNullCount}
	if nonNullCount == 0 {
		return bounds, nil
	}

	if kind == ExtractPartitionKindNumeric {
		start, err := SQLIntValue(minValue)
		if err != nil {
			return ExtractPartitionBounds{}, fmt.Errorf("failed to parse minimum extract partition value: %w", err)
		}
		end, err := SQLIntValue(maxValue)
		if err != nil {
			return ExtractPartitionBounds{}, fmt.Errorf("failed to parse maximum extract partition value: %w", err)
		}

		bounds.NumericStart = start
		bounds.NumericEnd = end
		bounds.HasRange = true
		return bounds, nil
	}

	start, err := SQLTimeValue(minValue)
	if err != nil {
		return ExtractPartitionBounds{}, fmt.Errorf("failed to parse minimum extract partition value: %w", err)
	}
	end, err := SQLTimeValue(maxValue)
	if err != nil {
		return ExtractPartitionBounds{}, fmt.Errorf("failed to parse maximum extract partition value: %w", err)
	}

	bounds.Start = start
	bounds.End = ceilTimeToSQLGranularity(end)
	bounds.HasRange = true
	return bounds, nil
}

func SQLIntValue(value any) (int64, error) {
	switch v := value.(type) {
	case nil:
		return 0, fmt.Errorf("value is null")
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return uintToInt64(uint64(v))
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return uintToInt64(v)
	case float32:
		return floatToInt64(float64(v), "float32")
	case float64:
		return floatToInt64(v, "float64")
	case *big.Int:
		if v == nil {
			return 0, fmt.Errorf("value is null")
		}
		if !v.IsInt64() {
			return 0, fmt.Errorf("value %v overflows int64", v)
		}
		return v.Int64(), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	case []byte:
		return strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported value type %T", value)
	}
}

func floatToInt64(v float64, typeName string) (int64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%s value %v is not finite", typeName, v)
	}
	if v != math.Trunc(v) {
		return 0, fmt.Errorf("%s value %v is not an integer", typeName, v)
	}
	if v < float64(math.MinInt64) || v >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("%s value %v overflows int64", typeName, v)
	}
	return int64(v), nil
}

func uintToInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("value %d overflows int64", v)
	}
	return int64(v), nil
}

func SQLTimeValue(value any) (time.Time, error) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, fmt.Errorf("value is null")
	case time.Time:
		return v, nil
	case string:
		return parseSQLTimeString(v)
	case []byte:
		return parseSQLTimeString(string(v))
	default:
		return time.Time{}, fmt.Errorf("unsupported value type %T", value)
	}
}

func parseSQLTimeString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time value %q", value)
}

func DefaultSQLTimeFormat(t time.Time) string {
	return t.Truncate(time.Microsecond).Format("2006-01-02 15:04:05.000000-07:00")
}

func NativeSQLTimeFormat(t time.Time) string {
	t = t.Truncate(time.Microsecond)
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format("2006-01-02 15:04:05.000000")
}

func SQLDateFormat(t time.Time) string {
	return t.Format("2006-01-02")
}

// ceilTimeToSQLGranularity rounds t up to the microsecond granularity of
// DefaultSQLTimeFormat. A discovered maximum is used as an inclusive `<=` bound
// on the final partition; without rounding up, the formatter could truncate a
// sub-microsecond maximum and drop rows in that final interval.
func ceilTimeToSQLGranularity(t time.Time) time.Time {
	truncated := t.Truncate(time.Microsecond)
	if truncated.Equal(t) {
		return t
	}
	return truncated.Add(time.Microsecond)
}
