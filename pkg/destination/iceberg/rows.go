package iceberg

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/google/uuid"
)

// decimalVal and uuidVal keep decimals and UUIDs distinct from plain strings in
// the normalized row representation, so ordering and predicate building can
// treat them by their logical type.
type (
	decimalVal string
	uuidVal    string
)

// scannedTable holds a fully materialized read of an Iceberg table. Rows are
// normalized Go values aligned to Columns order.
type scannedTable struct {
	Columns []string
	ColIdx  map[string]int
	Rows    [][]any
}

func (s *scannedTable) HasColumn(name string) bool {
	_, ok := s.ColIdx[name]
	return ok
}

func (s *scannedTable) Value(row []any, column string) any {
	idx, ok := s.ColIdx[column]
	if !ok {
		return nil
	}
	return row[idx]
}

// scanTableRows reads all rows matching filter into memory.
func scanTableRows(ctx context.Context, tbl *icebergtable.Table, filter iceberggo.BooleanExpression) (*scannedTable, error) {
	out := &scannedTable{ColIdx: make(map[string]int)}
	for _, field := range tbl.Schema().Fields() {
		out.ColIdx[field.Name] = len(out.Columns)
		out.Columns = append(out.Columns, field.Name)
	}
	if tbl.CurrentSnapshot() == nil {
		return out, nil
	}

	batchSchema, itr, err := tbl.Scan(icebergtable.WithRowFilter(filter)).ToArrowRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to scan table: %w", err)
	}

	positions := make([]int, len(batchSchema.Fields()))
	for i, field := range batchSchema.Fields() {
		idx, ok := out.ColIdx[field.Name]
		if !ok {
			return nil, fmt.Errorf("iceberg: scan returned unknown column %q", field.Name)
		}
		positions[i] = idx
	}

	for batch, err := range itr {
		if err != nil {
			return nil, fmt.Errorf("iceberg: failed to read table rows: %w", err)
		}
		numRows := int(batch.NumRows())
		numCols := int(batch.NumCols())
		for r := range numRows {
			row := make([]any, len(out.Columns))
			for c := range numCols {
				value, err := rowValue(batch.Column(c), r)
				if err != nil {
					batch.Release()
					return nil, fmt.Errorf("iceberg: column %q: %w", batchSchema.Field(c).Name, err)
				}
				row[positions[c]] = value
			}
			out.Rows = append(out.Rows, row)
		}
		batch.Release()
	}
	return out, nil
}

// rowValue extracts a normalized Go value: nil, bool, int64 (integers, date
// days, time/timestamp micros), float64, string, []byte, decimalVal, uuidVal
// or []any for lists.
func rowValue(arr arrow.Array, i int) (any, error) {
	if arr.IsNull(i) {
		return nil, nil
	}
	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(i), nil
	case *array.Int8:
		return int64(a.Value(i)), nil
	case *array.Int16:
		return int64(a.Value(i)), nil
	case *array.Int32:
		return int64(a.Value(i)), nil
	case *array.Int64:
		return a.Value(i), nil
	case *array.Uint8:
		return int64(a.Value(i)), nil
	case *array.Uint16:
		return int64(a.Value(i)), nil
	case *array.Uint32:
		return int64(a.Value(i)), nil
	case *array.Uint64:
		return int64(a.Value(i)), nil
	case *array.Float32:
		return float64(a.Value(i)), nil
	case *array.Float64:
		return a.Value(i), nil
	case *array.String:
		return a.Value(i), nil
	case *array.LargeString:
		return a.Value(i), nil
	case *array.Binary:
		return bytes.Clone(a.Value(i)), nil
	case *array.LargeBinary:
		return bytes.Clone(a.Value(i)), nil
	case *array.FixedSizeBinary:
		return bytes.Clone(a.Value(i)), nil
	case *array.Date32:
		return int64(a.Value(i)), nil
	case *array.Date64:
		return int64(a.Value(i)) / 86400000, nil
	case *array.Time32:
		dt := a.DataType().(*arrow.Time32Type)
		return timeToMicros(int64(a.Value(i)), dt.Unit), nil
	case *array.Time64:
		dt := a.DataType().(*arrow.Time64Type)
		return timeToMicros(int64(a.Value(i)), dt.Unit), nil
	case *array.Timestamp:
		dt := a.DataType().(*arrow.TimestampType)
		return timeToMicros(int64(a.Value(i)), dt.Unit), nil
	case *array.Decimal128:
		dt := a.DataType().(*arrow.Decimal128Type)
		return decimalVal(normalizeDecimalString(a.Value(i).ToString(dt.Scale))), nil
	case *array.Decimal256:
		dt := a.DataType().(*arrow.Decimal256Type)
		return decimalVal(normalizeDecimalString(a.Value(i).ToString(dt.Scale))), nil
	case *extensions.UUIDArray:
		return uuidVal(a.Value(i).String()), nil
	case *array.List:
		return listValues(a.ListValues(), int(a.Offsets()[i]), int(a.Offsets()[i+1]))
	case *array.LargeList:
		return listValues(a.ListValues(), int(a.Offsets()[i]), int(a.Offsets()[i+1]))
	case array.ExtensionArray:
		return rowValue(a.Storage(), i)
	case *array.Null:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported value type %s", arr.DataType())
	}
}

func listValues(values arrow.Array, start, end int) (any, error) {
	out := make([]any, 0, end-start)
	for i := start; i < end; i++ {
		v, err := rowValue(values, i)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func timeToMicros(v int64, unit arrow.TimeUnit) int64 {
	switch unit {
	case arrow.Second:
		return v * 1_000_000
	case arrow.Millisecond:
		return v * 1_000
	case arrow.Microsecond:
		return v
	case arrow.Nanosecond:
		return v / 1_000
	default:
		return v
	}
}

func microsToUnit(v int64, unit arrow.TimeUnit) int64 {
	switch unit {
	case arrow.Second:
		return v / 1_000_000
	case arrow.Millisecond:
		return v / 1_000
	case arrow.Microsecond:
		return v
	case arrow.Nanosecond:
		return v * 1_000
	default:
		return v
	}
}

// normalizeDecimalString trims insignificant fraction zeros so equal decimals
// compare equal regardless of the scale they were stored with.
func normalizeDecimalString(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	switch s {
	case "", "-":
		return "0"
	default:
		return s
	}
}

// appendValue writes a normalized value into a builder for the write schema.
func appendValue(b array.Builder, v any) error {
	if v == nil {
		b.AppendNull()
		return nil
	}
	switch bldr := b.(type) {
	case *array.BooleanBuilder:
		val, ok := v.(bool)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.Int8Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(int8(val))
	case *array.Int16Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(int16(val))
	case *array.Int32Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(int32(val))
	case *array.Int64Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.Float32Builder:
		val, ok := asFloat64(v)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(float32(val))
	case *array.Float64Builder:
		val, ok := asFloat64(v)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.StringBuilder:
		val, ok := asString(v)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.LargeStringBuilder:
		val, ok := asString(v)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.BinaryBuilder:
		val, ok := v.([]byte)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.FixedSizeBinaryBuilder:
		val, ok := v.([]byte)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(val)
	case *array.Date32Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(arrow.Date32(val))
	case *array.Time32Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		dt := bldr.Type().(*arrow.Time32Type)
		bldr.Append(arrow.Time32(microsToUnit(val, dt.Unit)))
	case *array.Time64Builder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		dt := bldr.Type().(*arrow.Time64Type)
		bldr.Append(arrow.Time64(microsToUnit(val, dt.Unit)))
	case *array.TimestampBuilder:
		val, ok := v.(int64)
		if !ok {
			return typeMismatch(b, v)
		}
		dt := bldr.Type().(*arrow.TimestampType)
		bldr.Append(arrow.Timestamp(microsToUnit(val, dt.Unit)))
	case *array.Decimal128Builder:
		val, ok := asString(v)
		if !ok {
			return typeMismatch(b, v)
		}
		dt := bldr.Type().(*arrow.Decimal128Type)
		num, err := decimal128.FromString(val, dt.Precision, dt.Scale)
		if err != nil {
			return fmt.Errorf("cannot convert %q to %s: %w", val, dt, err)
		}
		bldr.Append(num)
	case *extensions.UUIDBuilder:
		switch val := v.(type) {
		case uuidVal:
			return bldr.AppendValueFromString(string(val))
		case string:
			return bldr.AppendValueFromString(val)
		case []byte:
			parsed, err := uuid.FromBytes(val)
			if err != nil {
				return fmt.Errorf("cannot convert bytes to uuid: %w", err)
			}
			bldr.Append(parsed)
		default:
			return typeMismatch(b, v)
		}
	case *array.ListBuilder:
		vals, ok := v.([]any)
		if !ok {
			return typeMismatch(b, v)
		}
		bldr.Append(true)
		for _, elem := range vals {
			if err := appendValue(bldr.ValueBuilder(), elem); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported builder type %s", b.Type())
	}
	return nil
}

func typeMismatch(b array.Builder, v any) error {
	return fmt.Errorf("cannot write value of type %T into column type %s", v, b.Type())
}

func asFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

func asString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case decimalVal:
		return string(val), true
	case uuidVal:
		return string(val), true
	default:
		return "", false
	}
}

// buildRecordBatches converts normalized rows into Arrow record batches
// matching the write schema.
func buildRecordBatches(writeSchema *arrow.Schema, rows [][]any) ([]arrow.RecordBatch, error) {
	const chunkSize = 4096

	batches := make([]arrow.RecordBatch, 0, len(rows)/chunkSize+1)
	release := func() {
		for _, b := range batches {
			b.Release()
		}
	}

	for start := 0; start < len(rows); start += chunkSize {
		end := min(start+chunkSize, len(rows))

		builder := array.NewRecordBuilder(memory.DefaultAllocator, writeSchema)
		for _, row := range rows[start:end] {
			for c := range writeSchema.Fields() {
				if err := appendValue(builder.Field(c), row[c]); err != nil {
					builder.Release()
					release()
					return nil, fmt.Errorf("iceberg: column %q: %w", writeSchema.Field(c).Name, err)
				}
			}
		}
		batches = append(batches, builder.NewRecordBatch())
		builder.Release()
	}
	return batches, nil
}

func releaseBatches(batches []arrow.RecordBatch) {
	for _, b := range batches {
		b.Release()
	}
}

// valuesEqual compares two normalized values null-safely (both-nil is equal,
// mirroring IS NOT DISTINCT FROM in the SQL merge implementations).
func valuesEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch av := a.(type) {
	case []byte:
		bv, ok := b.([]byte)
		return ok && bytes.Equal(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// compareOrdered orders two normalized values; ok is false when the values are
// not comparable (callers treat that as a tie).
func compareOrdered(a, b any) (int, bool) {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0, true
		case a == nil:
			return -1, true
		default:
			return 1, true
		}
	}
	switch av := a.(type) {
	case int64:
		switch bv := b.(type) {
		case int64:
			return cmpInt(av, bv), true
		case float64:
			return cmpFloat(float64(av), bv), true
		}
	case float64:
		switch bv := b.(type) {
		case float64:
			return cmpFloat(av, bv), true
		case int64:
			return cmpFloat(av, float64(bv)), true
		}
	case string:
		if bv, ok := b.(string); ok {
			return strings.Compare(av, bv), true
		}
	case bool:
		if bv, ok := b.(bool); ok {
			return cmpBool(av, bv), true
		}
	case []byte:
		if bv, ok := b.([]byte); ok {
			return bytes.Compare(av, bv), true
		}
	case uuidVal:
		if bv, ok := b.(uuidVal); ok {
			return strings.Compare(string(av), string(bv)), true
		}
	case decimalVal:
		if bv, ok := b.(decimalVal); ok {
			ra, okA := new(big.Rat).SetString(string(av))
			rb, okB := new(big.Rat).SetString(string(bv))
			if okA && okB {
				return ra.Cmp(rb), true
			}
		}
	}
	return 0, false
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
}

// encodeRowKey builds an unambiguous composite key from primary key values.
func encodeRowKey(values []any) (string, error) {
	var sb strings.Builder
	for i, v := range values {
		if i > 0 {
			sb.WriteByte(0x1f)
		}
		switch val := v.(type) {
		case nil:
			return "", fmt.Errorf("primary key value is NULL")
		case bool:
			sb.WriteString("b:")
			sb.WriteString(strconv.FormatBool(val))
		case int64:
			sb.WriteString("i:")
			sb.WriteString(strconv.FormatInt(val, 10))
		case string:
			sb.WriteString("s:")
			sb.WriteString(strconv.Itoa(len(val)))
			sb.WriteByte(':')
			sb.WriteString(val)
		case []byte:
			sb.WriteString("x:")
			sb.WriteString(hex.EncodeToString(val))
		case uuidVal:
			sb.WriteString("u:")
			sb.WriteString(string(val))
		case decimalVal:
			sb.WriteString("d:")
			sb.WriteString(string(val))
		default:
			return "", fmt.Errorf("unsupported primary key value type %T", v)
		}
	}
	return sb.String(), nil
}

type predicateOp int

const (
	opEqual predicateOp = iota
	opGreaterThanEqual
	opLessThanEqual
)

func typedPredicate[T iceberggo.LiteralType](op predicateOp, ref iceberggo.UnboundTerm, v T) iceberggo.BooleanExpression {
	switch op {
	case opGreaterThanEqual:
		return iceberggo.GreaterThanEqual(ref, v)
	case opLessThanEqual:
		return iceberggo.LessThanEqual(ref, v)
	default:
		return iceberggo.EqualTo(ref, v)
	}
}

// equalityPredicate builds field = value for a normalized value, using the
// Iceberg column type to pick the literal type.
func equalityPredicate(field iceberggo.NestedField, v any) (iceberggo.BooleanExpression, error) {
	return columnPredicate(opEqual, field, v)
}

// columnPredicate builds field <op> value for a normalized value, using the
// Iceberg column type to pick the literal type.
func columnPredicate(op predicateOp, field iceberggo.NestedField, v any) (iceberggo.BooleanExpression, error) {
	ref := iceberggo.Reference(field.Name)
	switch field.Type.(type) {
	case iceberggo.BooleanType:
		val, ok := v.(bool)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, val), nil
	case iceberggo.Int32Type:
		val, ok := v.(int64)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, int32(val)), nil
	case iceberggo.Int64Type:
		val, ok := v.(int64)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, val), nil
	case iceberggo.Float32Type, iceberggo.Float64Type:
		val, ok := asFloat64(v)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, val), nil
	case iceberggo.StringType:
		val, ok := asString(v)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, val), nil
	case iceberggo.DateType:
		val, ok := v.(int64)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, iceberggo.Date(val)), nil
	case iceberggo.TimestampType, iceberggo.TimestampTzType:
		val, ok := v.(int64)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, iceberggo.Timestamp(val)), nil
	case iceberggo.TimeType:
		val, ok := v.(int64)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, iceberggo.Time(val)), nil
	case iceberggo.UUIDType:
		val, ok := asString(v)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		parsed, err := uuid.Parse(val)
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid uuid value for column %q: %w", field.Name, err)
		}
		return typedPredicate(op, ref, parsed), nil
	case iceberggo.BinaryType, iceberggo.FixedType:
		val, ok := v.([]byte)
		if !ok {
			return nil, predicateTypeErr(field, v)
		}
		return typedPredicate(op, ref, val), nil
	case iceberggo.DecimalType:
		dec, err := decimalLiteral(field, v)
		if err != nil {
			return nil, err
		}
		return typedPredicate(op, ref, dec), nil
	default:
		return nil, fmt.Errorf("iceberg: unsupported predicate column type %s for column %q", field.Type, field.Name)
	}
}

// normalizeBoundValue converts an interval bound supplied by the strategy
// layer (time.Time, date strings, numbers) into the normalized representation
// expected by columnPredicate for the given column.
func normalizeBoundValue(field iceberggo.NestedField, v any) (any, error) {
	switch val := v.(type) {
	case nil:
		return nil, fmt.Errorf("iceberg: interval bound for column %q is nil", field.Name)
	case *time.Time:
		if val == nil {
			return nil, fmt.Errorf("iceberg: interval bound for column %q is nil", field.Name)
		}
		return normalizeBoundValue(field, *val)
	case time.Time:
		switch field.Type.(type) {
		case iceberggo.DateType:
			return val.UTC().Truncate(24*time.Hour).Unix() / 86400, nil
		case iceberggo.TimeType:
			return timeOfDayMicros(val), nil
		default:
			return val.UTC().UnixMicro(), nil
		}
	case int:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		return int64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case bool:
		return val, nil
	case []byte:
		return val, nil
	case string:
		return normalizeStringBound(field, val)
	default:
		return nil, fmt.Errorf("iceberg: unsupported interval bound type %T for column %q", v, field.Name)
	}
}

func normalizeStringBound(field iceberggo.NestedField, val string) (any, error) {
	switch field.Type.(type) {
	case iceberggo.DateType:
		parsed, err := time.Parse("2006-01-02", val)
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid date bound %q for column %q: %w", val, field.Name, err)
		}
		return parsed.Unix() / 86400, nil
	case iceberggo.TimestampType, iceberggo.TimestampTzType:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, val); err == nil {
				return parsed.UTC().UnixMicro(), nil
			}
		}
		return nil, fmt.Errorf("iceberg: invalid timestamp bound %q for column %q", val, field.Name)
	case iceberggo.TimeType:
		for _, layout := range []string{"15:04:05.999999999", "15:04:05", "15:04"} {
			if parsed, err := time.Parse(layout, val); err == nil {
				return timeOfDayMicros(parsed), nil
			}
		}
		return nil, fmt.Errorf("iceberg: invalid time bound %q for column %q", val, field.Name)
	case iceberggo.Int32Type, iceberggo.Int64Type:
		parsed, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid integer bound %q for column %q: %w", val, field.Name, err)
		}
		return parsed, nil
	case iceberggo.Float32Type, iceberggo.Float64Type:
		parsed, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("iceberg: invalid float bound %q for column %q: %w", val, field.Name, err)
		}
		return parsed, nil
	case iceberggo.DecimalType:
		return decimalVal(normalizeDecimalString(val)), nil
	default:
		return val, nil
	}
}

func timeOfDayMicros(val time.Time) int64 {
	return int64(val.Hour())*int64(time.Hour/time.Microsecond) +
		int64(val.Minute())*int64(time.Minute/time.Microsecond) +
		int64(val.Second())*int64(time.Second/time.Microsecond) +
		int64(val.Nanosecond())/int64(time.Microsecond)
}

func predicateTypeErr(field iceberggo.NestedField, v any) error {
	return fmt.Errorf("iceberg: cannot build predicate on column %q (%s) from value of type %T", field.Name, field.Type, v)
}

func decimalLiteral(field iceberggo.NestedField, v any) (iceberggo.Decimal, error) {
	dt, ok := field.Type.(iceberggo.DecimalType)
	if !ok {
		return iceberggo.Decimal{}, predicateTypeErr(field, v)
	}
	str, ok := asString(v)
	if !ok {
		if f, isNum := asFloat64(v); isNum {
			str = strconv.FormatFloat(f, 'f', -1, 64)
		} else {
			return iceberggo.Decimal{}, predicateTypeErr(field, v)
		}
	}
	num, err := decimal128.FromString(str, int32(dt.Precision()), int32(dt.Scale()))
	if err != nil {
		return iceberggo.Decimal{}, fmt.Errorf("iceberg: invalid decimal value %q for column %q: %w", str, field.Name, err)
	}
	return iceberggo.Decimal{Val: num, Scale: dt.Scale()}, nil
}

// keyMatchFilter builds an expression matching rows whose primary key values
// appear in keys. Single-column keys use an IN set; composite keys expand into
// OR-of-AND equality chains.
func keyMatchFilter(iceSchema *iceberggo.Schema, primaryKeys []string, keys [][]any) (iceberggo.BooleanExpression, error) {
	if len(keys) == 0 {
		return iceberggo.AlwaysFalse{}, nil
	}
	fields := make([]iceberggo.NestedField, len(primaryKeys))
	for i, pk := range primaryKeys {
		field, ok := iceSchema.FindFieldByName(pk)
		if !ok {
			return nil, fmt.Errorf("iceberg: primary key column %q not found in table schema", pk)
		}
		fields[i] = field
	}

	if len(primaryKeys) == 1 {
		return singleKeyInFilter(fields[0], keys)
	}

	keyExprs := make([]iceberggo.BooleanExpression, 0, len(keys))
	for _, key := range keys {
		preds := make([]iceberggo.BooleanExpression, 0, len(fields))
		for i, field := range fields {
			pred, err := equalityPredicate(field, key[i])
			if err != nil {
				return nil, err
			}
			preds = append(preds, pred)
		}
		keyExprs = append(keyExprs, andAll(preds))
	}
	return orAll(keyExprs), nil
}

// orAll and andAll build balanced expression trees so large key sets don't
// produce recursion-deep left-leaning chains in binding and evaluation.
func orAll(exprs []iceberggo.BooleanExpression) iceberggo.BooleanExpression {
	switch len(exprs) {
	case 0:
		return iceberggo.AlwaysFalse{}
	case 1:
		return exprs[0]
	default:
		mid := len(exprs) / 2
		return iceberggo.NewOr(orAll(exprs[:mid]), orAll(exprs[mid:]))
	}
}

func andAll(exprs []iceberggo.BooleanExpression) iceberggo.BooleanExpression {
	switch len(exprs) {
	case 0:
		return iceberggo.AlwaysTrue{}
	case 1:
		return exprs[0]
	default:
		mid := len(exprs) / 2
		return iceberggo.NewAnd(andAll(exprs[:mid]), andAll(exprs[mid:]))
	}
}

func singleKeyInFilter(field iceberggo.NestedField, keys [][]any) (iceberggo.BooleanExpression, error) {
	ref := iceberggo.Reference(field.Name)
	switch field.Type.(type) {
	case iceberggo.Int32Type:
		vals, err := collectKeyValues(field, keys, func(v any) (int32, bool) {
			i, ok := v.(int64)
			return int32(i), ok
		})
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	case iceberggo.Int64Type:
		vals, err := collectKeyValues(field, keys, func(v any) (int64, bool) {
			i, ok := v.(int64)
			return i, ok
		})
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	case iceberggo.StringType:
		vals, err := collectKeyValues(field, keys, asString)
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	case iceberggo.DateType:
		vals, err := collectKeyValues(field, keys, func(v any) (iceberggo.Date, bool) {
			i, ok := v.(int64)
			return iceberggo.Date(i), ok
		})
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	case iceberggo.TimestampType, iceberggo.TimestampTzType:
		vals, err := collectKeyValues(field, keys, func(v any) (iceberggo.Timestamp, bool) {
			i, ok := v.(int64)
			return iceberggo.Timestamp(i), ok
		})
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	case iceberggo.UUIDType:
		vals, err := collectKeyValues(field, keys, func(v any) (uuid.UUID, bool) {
			s, ok := asString(v)
			if !ok {
				return uuid.UUID{}, false
			}
			parsed, err := uuid.Parse(s)
			return parsed, err == nil
		})
		if err != nil {
			return nil, err
		}
		return iceberggo.IsIn(ref, vals...), nil
	default:
		// Fall back to OR-of-equality for less common key types.
		preds := make([]iceberggo.BooleanExpression, 0, len(keys))
		for _, key := range keys {
			pred, err := equalityPredicate(field, key[0])
			if err != nil {
				return nil, err
			}
			preds = append(preds, pred)
		}
		return orAll(preds), nil
	}
}

func collectKeyValues[T any](field iceberggo.NestedField, keys [][]any, convert func(any) (T, bool)) ([]T, error) {
	vals := make([]T, 0, len(keys))
	for _, key := range keys {
		v, ok := convert(key[0])
		if !ok {
			return nil, predicateTypeErr(field, key[0])
		}
		vals = append(vals, v)
	}
	return vals, nil
}
