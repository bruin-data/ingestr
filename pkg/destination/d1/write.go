package d1

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	maxBatchStatements = 50
	batchTargetBytes   = 1 << 20
)

func (d *D1Destination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-records:
			if !ok {
				return nil
			}
			if result.Err != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				return result.Err
			}
			if result.Batch == nil {
				continue
			}
			err := d.writeRecordBatch(ctx, result.Batch, opts.Table)
			result.Batch.Release()
			if err != nil {
				return fmt.Errorf("failed to write D1 batch: %w", err)
			}
		}
	}
}

func (d *D1Destination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	// D1 executes queries on a database serially; parallel requests only add
	// queue pressure and can reorder rows with the same key.
	return d.Write(ctx, records, opts)
}

func (d *D1Destination) writeRecordBatch(ctx context.Context, record arrow.RecordBatch, table string) error {
	quoted, err := quoteTable(table)
	if err != nil {
		return err
	}
	cols := int(record.NumCols())
	if cols == 0 || cols > maxParameters {
		return fmt.Errorf("D1 tables require between 1 and %d columns", maxParameters)
	}
	if record.NumRows() == 0 {
		return nil
	}
	names := make([]string, cols)
	placeholders := make([]string, cols)
	for i, field := range record.Schema().Fields() {
		names[i] = destination.QuoteIdentifier(field.Name)
		placeholders[i] = placeholder(field.Type)
	}
	prefix := "INSERT INTO " + quoted + " (" + strings.Join(names, ", ") + ") VALUES "
	rowSQL := "(" + strings.Join(placeholders, ", ") + ")"
	rowsPerStatement := maxParameters / cols
	var batch []statement
	batchBytes := 0
	flush := func() error {
		if err := d.executeBatch(ctx, batch); err != nil {
			return err
		}
		batch = nil
		batchBytes = 0
		return nil
	}
	for row := 0; row < int(record.NumRows()); {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := min(rowsPerStatement, int(record.NumRows())-row)
		stmt := statement{Params: make([]interface{}, 0, count*cols)}
		rows := make([]string, count)
		for n := 0; n < count; n++ {
			rows[n] = rowSQL
			for col := 0; col < cols; col++ {
				value, err := extractValue(record.Column(col), row+n)
				if err != nil {
					return fmt.Errorf("row %d column %q: %w", row+n+1, record.Schema().Field(col).Name, err)
				}
				stmt.Params = append(stmt.Params, value)
			}
		}
		stmt.SQL = prefix + strings.Join(rows, ", ")
		encoded, err := json.Marshal(stmt)
		if err != nil {
			return fmt.Errorf("failed to encode D1 rows: %w", err)
		}
		if len(batch) > 0 && (len(batch) == maxBatchStatements || batchBytes+len(encoded) > batchTargetBytes) {
			if err := flush(); err != nil {
				return err
			}
		}
		batch = append(batch, stmt)
		batchBytes += len(encoded)
		row += count
	}
	return flush()
}

func placeholder(dt arrow.DataType) string {
	if ext, ok := dt.(arrow.ExtensionType); ok {
		return placeholder(ext.StorageType())
	}
	if dict, ok := dt.(*arrow.DictionaryType); ok {
		return placeholder(dict.ValueType)
	}
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64, arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64, arrow.BOOL:
		// String bindings avoid rounding integer values through the API's JSON
		// number representation before SQLite receives them.
		return "CAST(? AS INTEGER)"
	default:
		return "?"
	}
}

func extractValue(values arrow.Array, index int) (interface{}, error) {
	if values.IsNull(index) {
		return nil, nil
	}
	switch a := values.(type) {
	case array.ExtensionArray:
		return extractValue(a.Storage(), index)
	case *array.Dictionary:
		return extractValue(a.Dictionary(), a.GetValueIndex(index))
	case *array.Boolean:
		if a.Value(index) {
			return "1", nil
		}
		return "0", nil
	case *array.Int8:
		return strconv.FormatInt(int64(a.Value(index)), 10), nil
	case *array.Int16:
		return strconv.FormatInt(int64(a.Value(index)), 10), nil
	case *array.Int32:
		return strconv.FormatInt(int64(a.Value(index)), 10), nil
	case *array.Int64:
		return strconv.FormatInt(a.Value(index), 10), nil
	case *array.Uint8:
		return strconv.FormatUint(uint64(a.Value(index)), 10), nil
	case *array.Uint16:
		return strconv.FormatUint(uint64(a.Value(index)), 10), nil
	case *array.Uint32:
		return strconv.FormatUint(uint64(a.Value(index)), 10), nil
	case *array.Uint64:
		if a.Value(index) > math.MaxInt64 {
			return nil, fmt.Errorf("unsigned integer exceeds D1's signed 64-bit range")
		}
		return strconv.FormatUint(a.Value(index), 10), nil
	case *array.Float32:
		return finiteFloat(float64(a.Value(index)))
	case *array.Float64:
		return finiteFloat(a.Value(index))
	case *array.String:
		return a.Value(index), nil
	case *array.LargeString:
		return a.Value(index), nil
	case *array.Binary:
		return blobValue(a.Value(index)), nil
	case *array.LargeBinary:
		return blobValue(a.Value(index)), nil
	case *array.FixedSizeBinary:
		return blobValue(a.Value(index)), nil
	case *array.Date32:
		return a.Value(index).ToTime().Format("2006-01-02"), nil
	case *array.Date64:
		return a.Value(index).ToTime().Format("2006-01-02"), nil
	case *array.Time32:
		return a.Value(index).FormattedString(a.DataType().(*arrow.Time32Type).Unit), nil
	case *array.Time64:
		return a.Value(index).FormattedString(a.DataType().(*arrow.Time64Type).Unit), nil
	case *array.Timestamp:
		toTime, err := a.DataType().(*arrow.TimestampType).GetToTimeFunc()
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp type: %w", err)
		}
		return toTime(a.Value(index)).UTC().Format("2006-01-02T15:04:05.000000Z"), nil
	case *array.Decimal128:
		return a.Value(index).ToString(a.DataType().(*arrow.Decimal128Type).Scale), nil
	case *array.Decimal256:
		return a.Value(index).ToString(a.DataType().(*arrow.Decimal256Type).Scale), nil
	case *array.List, *array.LargeList, *array.FixedSizeList, *array.Struct, *array.Map:
		data, err := json.Marshal(values.GetOneForMarshal(index))
		if err != nil {
			return nil, fmt.Errorf("failed to encode nested value: %w", err)
		}
		return string(data), nil
	case *array.MonthInterval, *array.DayTimeInterval, *array.MonthDayNanoInterval, *array.Duration:
		return values.ValueStr(index), nil
	default:
		return nil, fmt.Errorf("unsupported Arrow type %s", values.DataType())
	}
}

func finiteFloat(value float64) (interface{}, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return nil, fmt.Errorf("D1 cannot store NaN or infinity")
	}
	return value, nil
}

func blobValue(value []byte) []int {
	result := make([]int, len(value))
	for i, b := range value {
		result[i] = int(b)
	}
	return result
}
