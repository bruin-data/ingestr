package snowflake

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	sfauth "github.com/bruin-data/ingestr/pkg/snowflake"
	"github.com/bruin-data/ingestr/pkg/source"
	srcadbc "github.com/bruin-data/ingestr/pkg/source/adbc"
	sf "github.com/snowflakedb/gosnowflake"
)

// Default parallelism settings - can be overridden via environment variables:
//
//	SNOWFLAKE_FETCH_CONCURRENCY - number of concurrent batch fetches (default: 10)
//	SNOWFLAKE_PREFETCH_BUFFER - size of prefetch buffer (default: 20)
var (
	defaultFetchConcurrency = getEnvInt("SNOWFLAKE_FETCH_CONCURRENCY", 10)
	prefetchBufferSize      = getEnvInt("SNOWFLAKE_PREFETCH_BUFFER", 20)
)

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return defaultVal
}

// ReadWithStorageAPI implements adbc.StorageReader interface.
// Uses native gosnowflake Arrow batch API for maximum performance.
// This bypasses ADBC overhead and uses Snowflake's native Arrow streaming.
func (d *Dialect) ReadWithStorageAPI(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if d.uri == "" {
		return nil, fmt.Errorf("connection URI not available")
	}

	config.Debug("[SNOWFLAKE-NATIVE] Using native gosnowflake Arrow batch API")

	// Get or create native gosnowflake connection
	db, err := d.getNativeDB()
	if err != nil {
		return nil, err
	}

	// Build the query
	columns := srcadbc.FilterColumns(opts.Schema.Columns, opts.ExcludeColumns)
	schemaName, tableName := d.ParseTableName(table)
	fullTable := fmt.Sprintf("%s.%s", d.QuoteIdentifier(schemaName), d.QuoteIdentifier(tableName))
	query := srcadbc.BuildSelectQuery(fullTable, columns, opts, d.QuoteIdentifier)
	config.Debug("[SNOWFLAKE-NATIVE] Query: %s", query)

	// Create Arrow batch context with microsecond timestamps (our standard)
	arrowCtx := sf.WithArrowBatches(ctx)
	arrowCtx = sf.WithArrowBatchesTimestampOption(arrowCtx, sf.UseMicrosecondTimestamp)
	// Note: WithHigherPrecision changes decimal handling - don't use it for simpler type mapping

	// Attribute the extract read via Snowflake's QUERY_TAG (Snowflake strips
	// leading comments, so the destination-style tag is used instead of Prepend).
	if tag, ok := annotation.QueryTag(annotation.WithStep(ctx, annotation.StepExtract)); ok {
		arrowCtx = sf.WithQueryTag(arrowCtx, tag)
	}

	// Get raw connection for Arrow batch access
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Execute query via raw connection to get Arrow batches
	var rows driver.Rows
	err = conn.Raw(func(driverConn interface{}) error {
		queryer, ok := driverConn.(driver.QueryerContext)
		if !ok {
			return fmt.Errorf("connection does not support QueryerContext")
		}
		var queryErr error
		rows, queryErr = queryer.QueryContext(arrowCtx, query, nil)
		return queryErr
	})
	if err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Get Arrow batches
	sfRows, ok := rows.(sf.SnowflakeRows)
	if !ok {
		_ = rows.Close()
		_ = conn.Close()
		_ = db.Close()
		return nil, fmt.Errorf("unexpected rows type, expected SnowflakeRows")
	}

	batches, err := sfRows.GetArrowBatches()
	if err != nil {
		_ = rows.Close()
		_ = conn.Close()
		_ = db.Close()
		return nil, fmt.Errorf("failed to get Arrow batches: %w", err)
	}

	config.Debug("[SNOWFLAKE-NATIVE] Got %d Arrow batches to fetch (concurrency: %d)", len(batches), defaultFetchConcurrency)

	// Build target schema
	targetSchema := srcadbc.BuildArrowSchema(columns)

	results := make(chan source.RecordBatchResult, 16)

	go func() {
		defer close(results)
		defer func() {
			_ = rows.Close()
			_ = conn.Close()
			// Don't close db - it's cached in the dialect for reuse
		}()

		startTotal := time.Now()
		totalRows := int64(0)
		batchNum := 0
		mem := memory.NewGoAllocator()

		// Use parallel batch fetching for performance
		// This matches ADBC's prefetch_concurrency behavior
		type fetchedBatch struct {
			idx     int
			records *[]arrow.RecordBatch
			err     error
		}

		// Channel to receive fetched batches (buffered for prefetching)
		fetchedChan := make(chan fetchedBatch, prefetchBufferSize)

		// Semaphore to limit concurrent fetches
		semaphore := make(chan struct{}, defaultFetchConcurrency)

		// Start parallel fetching
		var fetchWg sync.WaitGroup
		for batchIdx, batch := range batches {
			fetchWg.Add(1)
			go func(idx int, b *sf.ArrowBatch) {
				defer fetchWg.Done()

				// Acquire semaphore slot
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				records, err := b.Fetch()
				fetchedChan <- fetchedBatch{idx: idx, records: records, err: err}
			}(batchIdx, batch)
		}

		// Close channel when all fetches complete
		go func() {
			fetchWg.Wait()
			close(fetchedChan)
		}()

		// Process fetched batches in order
		pendingBatches := make(map[int]fetchedBatch)
		nextIdx := 0

		for fetched := range fetchedChan {
			if fetched.err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch batch %d: %w", fetched.idx, fetched.err)}
				return
			}

			// Store the batch
			pendingBatches[fetched.idx] = fetched

			// Process all consecutive ready batches
			for {
				batch, ok := pendingBatches[nextIdx]
				if !ok {
					break
				}
				delete(pendingBatches, nextIdx)
				nextIdx++

				if batch.records == nil || len(*batch.records) == 0 {
					continue
				}

				// Process each record in the batch
				for _, srcRecord := range *batch.records {
					if srcRecord.NumRows() == 0 {
						continue
					}

					convertedRecord, err := convertRecord(mem, srcRecord, targetSchema, columns)
					if err != nil {
						results <- source.RecordBatchResult{Err: fmt.Errorf("conversion failed: %w", err)}
						return
					}

					batchNum++
					rowCount := convertedRecord.NumRows()
					totalRows += rowCount

					results <- source.RecordBatchResult{Batch: convertedRecord}
				}
			}
		}

		elapsed := time.Since(startTotal)
		rowsPerSec := float64(totalRows) / elapsed.Seconds()
		config.Debug("[SNOWFLAKE-NATIVE] Total: %d rows in %d batches, time: %v (%.0f rows/sec)",
			totalRows, batchNum, elapsed, rowsPerSec)
	}()

	return results, nil
}

// buildGosnowflakeDSN converts our URI format to a gosnowflake DSN string.
// Supports all auth methods: password, key-pair, OAuth, external browser.
func buildGosnowflakeDSN(uri string) (string, error) {
	auth, err := sfauth.ParseURI(uri)
	if err != nil {
		return "", err
	}
	return auth.ToDSN()
}

// SetURI stores the connection URI and pre-initializes the native connection.
// This is called early, allowing the connection to be reused for schema fetching and data reading.
func (d *Dialect) SetURI(uri string) {
	d.uri = uri
	// Pre-initialize native connection in background for faster first query
	go func() {
		_, _ = d.getNativeDB()
	}()
}

// getNativeDB returns a cached native gosnowflake connection, creating one if needed.
// This avoids opening a new connection for each read operation.
// Thread-safe via sync.Once.
func (d *Dialect) getNativeDB() (*sql.DB, error) {
	d.nativeOnce.Do(func() {
		dsn, err := buildGosnowflakeDSN(d.uri)
		if err != nil {
			d.nativeErr = fmt.Errorf("failed to build DSN: %w", err)
			return
		}

		db, err := sql.Open("snowflake", dsn)
		if err != nil {
			d.nativeErr = fmt.Errorf("failed to open snowflake connection: %w", err)
			return
		}

		// Actually establish connection (sql.Open is lazy)
		if err := db.Ping(); err != nil {
			d.nativeErr = fmt.Errorf("failed to connect to snowflake: %w", err)
			_ = db.Close()
			return
		}

		d.nativeDB = db
	})

	return d.nativeDB, d.nativeErr
}

// convertRecord converts a native Arrow record to match the target schema.
func convertRecord(mem memory.Allocator, src arrow.RecordBatch, targetSchema *arrow.Schema, columns []schema.Column) (arrow.RecordBatch, error) {
	numRows := src.NumRows()
	if numRows == 0 {
		return array.NewRecordBatch(targetSchema, nil, 0), nil
	}

	numCols := len(columns)
	srcNumCols := int(src.NumCols())
	if srcNumCols != numCols {
		return nil, fmt.Errorf("column count mismatch: got %d, expected %d", srcNumCols, numCols)
	}

	arrays := make([]arrow.Array, numCols)

	for i := range columns {
		srcArr := src.Column(i)
		targetType := targetSchema.Field(i).Type

		convertedArr, err := convertArray(mem, srcArr, targetType, columns[i], int(numRows))
		if err != nil {
			for j := 0; j < i; j++ {
				arrays[j].Release()
			}
			return nil, fmt.Errorf("failed to convert column %s: %w", columns[i].Name, err)
		}
		arrays[i] = convertedArr
	}

	record := array.NewRecordBatch(targetSchema, arrays, numRows)

	for _, arr := range arrays {
		arr.Release()
	}

	return record, nil
}

// convertArray converts a source Arrow array to the target type.
// Uses bulk operations where possible for performance.
func convertArray(mem memory.Allocator, src arrow.Array, targetType arrow.DataType, col schema.Column, numRows int) (arrow.Array, error) {
	// Try fast path with bulk copy for matching or compatible types
	if result := tryBulkConvert(mem, src, targetType, numRows); result != nil {
		return result, nil
	}

	// Slow path: row-by-row conversion for incompatible types
	builder := array.NewBuilder(mem, targetType)
	defer builder.Release()

	for i := 0; i < numRows; i++ {
		if src.IsNull(i) {
			builder.AppendNull()
			continue
		}

		if err := appendConvertedValue(builder, src, i, col); err != nil {
			return nil, err
		}
	}

	return builder.NewArray(), nil
}

// tryBulkConvert attempts fast bulk conversion for compatible types.
func tryBulkConvert(mem memory.Allocator, src arrow.Array, targetType arrow.DataType, numRows int) arrow.Array {
	// String to String - most common case
	if _, ok := targetType.(*arrow.StringType); ok {
		switch s := src.(type) {
		case *array.String:
			return copyStringArray(mem, s)
		case *array.LargeString:
			return copyLargeStringToString(mem, s)
		}
	}

	// Int64 to Int64 (handle various int types)
	if _, ok := targetType.(*arrow.Int64Type); ok {
		switch s := src.(type) {
		case *array.Int64:
			return copyInt64Array(mem, s)
		case *array.Int32:
			return convertInt32ToInt64(mem, s)
		case *array.Int16:
			return convertInt16ToInt64(mem, s)
		case *array.Int8:
			return convertInt8ToInt64(mem, s)
		case *array.Decimal128:
			return convertDecimal128ToInt64(mem, s)
		}
	}

	// Float64 to Float64
	if _, ok := targetType.(*arrow.Float64Type); ok {
		switch s := src.(type) {
		case *array.Float64:
			return copyFloat64Array(mem, s)
		case *array.Decimal128:
			return convertDecimal128ToFloat64(mem, s)
		}
	}

	// Boolean to Boolean
	if _, ok := targetType.(*arrow.BooleanType); ok {
		if s, ok := src.(*array.Boolean); ok {
			return copyBooleanArray(mem, s)
		}
	}

	// Timestamp to Timestamp (with unit conversion)
	if targetTs, ok := targetType.(*arrow.TimestampType); ok {
		if srcTs, ok := src.(*array.Timestamp); ok {
			return convertTimestampArray(mem, srcTs, targetTs)
		}
	}

	// Decimal128 to Decimal128 (handle various source int types)
	if targetDec, ok := targetType.(*arrow.Decimal128Type); ok {
		switch s := src.(type) {
		case *array.Decimal128:
			return copyDecimal128Array(mem, s, targetDec)
		case *array.Int8:
			return convertInt8ToDecimal128(mem, s, targetDec)
		case *array.Int16:
			return convertInt16ToDecimal128(mem, s, targetDec)
		case *array.Int32:
			return convertInt32ToDecimal128(mem, s, targetDec)
		case *array.Int64:
			return convertInt64ToDecimal128(mem, s, targetDec)
		}
	}

	// Date32 to Date32
	if _, ok := targetType.(*arrow.Date32Type); ok {
		if s, ok := src.(*array.Date32); ok {
			return copyDate32Array(mem, s)
		}
	}

	// Binary to Binary
	if _, ok := targetType.(*arrow.BinaryType); ok {
		if s, ok := src.(*array.Binary); ok {
			return copyBinaryArray(mem, s)
		}
	}

	return nil
}

// Bulk copy functions

func copyStringArray(mem memory.Allocator, src *array.String) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(src.Value(i))
		}
	}
	return b.NewArray()
}

func copyLargeStringToString(mem memory.Allocator, src *array.LargeString) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(src.Value(i))
		}
	}
	return b.NewArray()
}

func copyInt64Array(mem memory.Allocator, src *array.Int64) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()

	values := src.Int64Values()
	b.Reserve(len(values))

	if src.NullN() == 0 {
		b.AppendValues(values, nil)
	} else {
		valid := make([]bool, len(values))
		for i := range valid {
			valid[i] = src.IsValid(i)
		}
		b.AppendValues(values, valid)
	}
	return b.NewArray()
}

func convertInt32ToInt64(mem memory.Allocator, src *array.Int32) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(int64(src.Value(i)))
		}
	}
	return b.NewArray()
}

func convertInt16ToInt64(mem memory.Allocator, src *array.Int16) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(int64(src.Value(i)))
		}
	}
	return b.NewArray()
}

func convertInt8ToInt64(mem memory.Allocator, src *array.Int8) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(int64(src.Value(i)))
		}
	}
	return b.NewArray()
}

func convertDecimal128ToInt64(mem memory.Allocator, src *array.Decimal128) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	scale := int32(src.DataType().(*arrow.Decimal128Type).Scale)

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			dec := src.Value(i)
			b.Append(int64(dec.ToFloat64(scale)))
		}
	}
	return b.NewArray()
}

func convertDecimal128ToFloat64(mem memory.Allocator, src *array.Decimal128) arrow.Array {
	b := array.NewFloat64Builder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	scale := int32(src.DataType().(*arrow.Decimal128Type).Scale)

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			dec := src.Value(i)
			b.Append(dec.ToFloat64(scale))
		}
	}
	return b.NewArray()
}

func copyFloat64Array(mem memory.Allocator, src *array.Float64) arrow.Array {
	b := array.NewFloat64Builder(mem)
	defer b.Release()

	values := src.Float64Values()
	b.Reserve(len(values))

	if src.NullN() == 0 {
		b.AppendValues(values, nil)
	} else {
		valid := make([]bool, len(values))
		for i := range valid {
			valid[i] = src.IsValid(i)
		}
		b.AppendValues(values, valid)
	}
	return b.NewArray()
}

func copyBooleanArray(mem memory.Allocator, src *array.Boolean) arrow.Array {
	b := array.NewBooleanBuilder(mem)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(src.Value(i))
		}
	}
	return b.NewArray()
}

func convertTimestampArray(mem memory.Allocator, src *array.Timestamp, targetType *arrow.TimestampType) arrow.Array {
	b := array.NewTimestampBuilder(mem, targetType)
	defer b.Release()

	srcUnit := src.DataType().(*arrow.TimestampType).Unit
	targetUnit := targetType.Unit

	srcValues := src.TimestampValues()
	n := len(srcValues)
	b.Reserve(n)

	// If units match, fast bulk copy
	if srcUnit == targetUnit {
		if src.NullN() == 0 {
			b.AppendValues(srcValues, nil)
		} else {
			valid := make([]bool, n)
			for i := range valid {
				valid[i] = src.IsValid(i)
			}
			b.AppendValues(srcValues, valid)
		}
		return b.NewArray()
	}

	// Units differ - convert
	var multiply, divide int64 = 1, 1
	switch srcUnit {
	case arrow.Second:
		switch targetUnit {
		case arrow.Millisecond:
			multiply = 1000
		case arrow.Microsecond:
			multiply = 1000000
		case arrow.Nanosecond:
			multiply = 1000000000
		}
	case arrow.Millisecond:
		switch targetUnit {
		case arrow.Second:
			divide = 1000
		case arrow.Microsecond:
			multiply = 1000
		case arrow.Nanosecond:
			multiply = 1000000
		}
	case arrow.Microsecond:
		switch targetUnit {
		case arrow.Second:
			divide = 1000000
		case arrow.Millisecond:
			divide = 1000
		case arrow.Nanosecond:
			multiply = 1000
		}
	case arrow.Nanosecond:
		switch targetUnit {
		case arrow.Second:
			divide = 1000000000
		case arrow.Millisecond:
			divide = 1000000
		case arrow.Microsecond:
			divide = 1000
		}
	}

	converted := make([]arrow.Timestamp, n)
	for i, v := range srcValues {
		converted[i] = arrow.Timestamp((int64(v) * multiply) / divide)
	}

	if src.NullN() == 0 {
		b.AppendValues(converted, nil)
	} else {
		valid := make([]bool, n)
		for i := range valid {
			valid[i] = src.IsValid(i)
		}
		b.AppendValues(converted, valid)
	}
	return b.NewArray()
}

func copyDecimal128Array(mem memory.Allocator, src *array.Decimal128, targetType *arrow.Decimal128Type) arrow.Array {
	b := array.NewDecimal128Builder(mem, targetType)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(src.Value(i))
		}
	}
	return b.NewArray()
}

func convertInt8ToDecimal128(mem memory.Allocator, src *array.Int8, targetType *arrow.Decimal128Type) arrow.Array {
	b := array.NewDecimal128Builder(mem, targetType)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(decimal128.FromI64(int64(src.Value(i))))
		}
	}
	return b.NewArray()
}

func convertInt16ToDecimal128(mem memory.Allocator, src *array.Int16, targetType *arrow.Decimal128Type) arrow.Array {
	b := array.NewDecimal128Builder(mem, targetType)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(decimal128.FromI64(int64(src.Value(i))))
		}
	}
	return b.NewArray()
}

func convertInt32ToDecimal128(mem memory.Allocator, src *array.Int32, targetType *arrow.Decimal128Type) arrow.Array {
	b := array.NewDecimal128Builder(mem, targetType)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(decimal128.FromI64(int64(src.Value(i))))
		}
	}
	return b.NewArray()
}

func convertInt64ToDecimal128(mem memory.Allocator, src *array.Int64, targetType *arrow.Decimal128Type) arrow.Array {
	b := array.NewDecimal128Builder(mem, targetType)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(decimal128.FromI64(src.Value(i)))
		}
	}
	return b.NewArray()
}

func copyDate32Array(mem memory.Allocator, src *array.Date32) arrow.Array {
	b := array.NewDate32Builder(mem)
	defer b.Release()

	values := src.Date32Values()
	b.Reserve(len(values))

	if src.NullN() == 0 {
		b.AppendValues(values, nil)
	} else {
		valid := make([]bool, len(values))
		for i := range valid {
			valid[i] = src.IsValid(i)
		}
		b.AppendValues(values, valid)
	}
	return b.NewArray()
}

func copyBinaryArray(mem memory.Allocator, src *array.Binary) arrow.Array {
	b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	defer b.Release()
	b.Reserve(src.Len())

	for i := 0; i < src.Len(); i++ {
		if src.IsNull(i) {
			b.AppendNull()
		} else {
			b.Append(src.Value(i))
		}
	}
	return b.NewArray()
}

// appendConvertedValue extracts a value from src at index i and appends it to builder.
func appendConvertedValue(builder array.Builder, src arrow.Array, i int, col schema.Column) error {
	switch b := builder.(type) {
	case *array.BooleanBuilder:
		if boolArr, ok := src.(*array.Boolean); ok {
			b.Append(boolArr.Value(i))
		} else {
			b.AppendNull()
		}

	case *array.Int16Builder:
		b.Append(int16(getInt64Value(src, i)))

	case *array.Int32Builder:
		b.Append(int32(getInt64Value(src, i)))

	case *array.Int64Builder:
		b.Append(getInt64Value(src, i))

	case *array.Float32Builder:
		b.Append(float32(getFloat64Value(src, i)))

	case *array.Float64Builder:
		b.Append(getFloat64Value(src, i))

	case *array.StringBuilder:
		appendStringValue(b, src, i)

	case *array.BinaryBuilder:
		appendBinaryValue(b, src, i)

	case *array.Date32Builder:
		appendDateValue(b, src, i)

	case *array.Time64Builder:
		appendTimeValue(b, src, i)

	case *array.TimestampBuilder:
		appendTimestampValue(b, src, i)

	case *array.Decimal128Builder:
		appendDecimalValue(b, src, i, col)

	default:
		if sb, ok := builder.(*array.StringBuilder); ok {
			sb.Append(fmt.Sprintf("%v", extractValue(src, i)))
		} else {
			builder.AppendNull()
		}
	}

	return nil
}

func getInt64Value(src arrow.Array, i int) int64 {
	switch arr := src.(type) {
	case *array.Int8:
		return int64(arr.Value(i))
	case *array.Int16:
		return int64(arr.Value(i))
	case *array.Int32:
		return int64(arr.Value(i))
	case *array.Int64:
		return arr.Value(i)
	case *array.Uint8:
		return int64(arr.Value(i))
	case *array.Uint16:
		return int64(arr.Value(i))
	case *array.Uint32:
		return int64(arr.Value(i))
	case *array.Uint64:
		return int64(arr.Value(i))
	case *array.Float32:
		return int64(arr.Value(i))
	case *array.Float64:
		return int64(arr.Value(i))
	case *array.Decimal128:
		dec := arr.Value(i)
		scale := int32(arr.DataType().(*arrow.Decimal128Type).Scale)
		return int64(dec.ToFloat64(scale))
	default:
		return 0
	}
}

func getFloat64Value(src arrow.Array, i int) float64 {
	switch arr := src.(type) {
	case *array.Float32:
		return float64(arr.Value(i))
	case *array.Float64:
		return arr.Value(i)
	case *array.Int64:
		return float64(arr.Value(i))
	case *array.Decimal128:
		dec := arr.Value(i)
		return dec.ToFloat64(int32(arr.DataType().(*arrow.Decimal128Type).Scale))
	default:
		return 0
	}
}

func appendStringValue(b *array.StringBuilder, src arrow.Array, i int) {
	switch arr := src.(type) {
	case *array.String:
		b.Append(arr.Value(i))
	case *array.LargeString:
		b.Append(arr.Value(i))
	case *array.Binary:
		b.Append(string(arr.Value(i)))
	default:
		b.Append(fmt.Sprintf("%v", extractValue(src, i)))
	}
}

func appendBinaryValue(b *array.BinaryBuilder, src arrow.Array, i int) {
	switch arr := src.(type) {
	case *array.Binary:
		b.Append(arr.Value(i))
	case *array.String:
		b.Append([]byte(arr.Value(i)))
	default:
		b.AppendNull()
	}
}

func appendDateValue(b *array.Date32Builder, src arrow.Array, i int) {
	switch arr := src.(type) {
	case *array.Date32:
		b.Append(arr.Value(i))
	case *array.Date64:
		b.Append(arrow.Date32(arr.Value(i) / (24 * 60 * 60 * 1000)))
	case *array.Timestamp:
		ts := arr.Value(i)
		unit := arr.DataType().(*arrow.TimestampType).Unit
		var secs int64
		switch unit {
		case arrow.Second:
			secs = int64(ts)
		case arrow.Millisecond:
			secs = int64(ts) / 1000
		case arrow.Microsecond:
			secs = int64(ts) / 1000000
		case arrow.Nanosecond:
			secs = int64(ts) / 1000000000
		}
		days := secs / (24 * 60 * 60)
		b.Append(arrow.Date32(days))
	default:
		b.AppendNull()
	}
}

func appendTimeValue(b *array.Time64Builder, src arrow.Array, i int) {
	switch arr := src.(type) {
	case *array.Time64:
		b.Append(arr.Value(i))
	case *array.Time32:
		b.Append(arrow.Time64(arr.Value(i)) * 1000000)
	default:
		b.AppendNull()
	}
}

func appendTimestampValue(b *array.TimestampBuilder, src arrow.Array, i int) {
	switch arr := src.(type) {
	case *array.Timestamp:
		ts := arr.Value(i)
		srcUnit := arr.DataType().(*arrow.TimestampType).Unit
		var micros int64
		switch srcUnit {
		case arrow.Second:
			micros = int64(ts) * 1000000
		case arrow.Millisecond:
			micros = int64(ts) * 1000
		case arrow.Microsecond:
			micros = int64(ts)
		case arrow.Nanosecond:
			micros = int64(ts) / 1000
		}
		b.Append(arrow.Timestamp(micros))
	case *array.Int64:
		b.Append(arrow.Timestamp(arr.Value(i)))
	default:
		b.AppendNull()
	}
}

func appendDecimalValue(b *array.Decimal128Builder, src arrow.Array, i int, col schema.Column) {
	switch arr := src.(type) {
	case *array.Decimal128:
		b.Append(arr.Value(i))
	case *array.Float64:
		val := arr.Value(i)
		precision := int32(col.Precision)
		scale := int32(col.Scale)
		if precision == 0 {
			precision = 38
		}
		dec, err := decimal128.FromFloat64(val, precision, scale)
		if err != nil {
			b.AppendNull()
		} else {
			b.Append(dec)
		}
	case *array.Int64:
		val := arr.Value(i)
		dec := decimal128.FromI64(val)
		b.Append(dec)
	default:
		b.AppendNull()
	}
}

func extractValue(arr arrow.Array, i int) interface{} {
	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(i)
	case *array.Int8:
		return a.Value(i)
	case *array.Int16:
		return a.Value(i)
	case *array.Int32:
		return a.Value(i)
	case *array.Int64:
		return a.Value(i)
	case *array.Float32:
		return a.Value(i)
	case *array.Float64:
		return a.Value(i)
	case *array.String:
		return a.Value(i)
	case *array.Binary:
		return a.Value(i)
	default:
		return nil
	}
}
