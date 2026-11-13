package bigquery

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// The Storage Read API tags JSON and GEOGRAPHY string columns with these
// (unregistered) extension names; REST batches carry the same field metadata.
const (
	arrowExtensionNameKey      = "ARROW:extension:name"
	storageJSONExtensionName   = "google:sqlType:json"
	storageGeographyExtension  = "google:sqlType:geography"
	storageNumericPrecision    = 38
	storageNumericScale        = 9
	storageBigNumericPrecision = 76
	storageBigNumericScale     = 38
)

var (
	unixEpochDate         = civil.Date{Year: 1970, Month: time.January, Day: 1}
	numericScaleFactor    = new(big.Int).Exp(big.NewInt(10), big.NewInt(storageNumericScale), nil)
	bigNumericScaleFactor = new(big.Int).Exp(big.NewInt(10), big.NewInt(storageBigNumericScale), nil)
	minInt256             = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
)

// restArrowSchema maps a BigQuery schema to the Arrow schema the Storage Read
// API produces for it, so REST and Storage reads emit identical batches.
func restArrowSchema(fields bigquery.Schema) (*arrow.Schema, error) {
	arrowFields := make([]arrow.Field, len(fields))
	for i, field := range fields {
		af, err := restArrowField(field)
		if err != nil {
			return nil, err
		}
		arrowFields[i] = af
	}
	return arrow.NewSchema(arrowFields, nil), nil
}

func restArrowField(field *bigquery.FieldSchema) (arrow.Field, error) {
	dt, err := restArrowElementType(field)
	if err != nil {
		return arrow.Field{}, fmt.Errorf("column %q: %w", field.Name, err)
	}
	if field.Repeated {
		dt = arrow.ListOf(dt)
	}

	af := arrow.Field{Name: field.Name, Type: dt, Nullable: !field.Required}
	switch field.Type {
	case bigquery.JSONFieldType:
		af.Metadata = arrow.NewMetadata([]string{arrowExtensionNameKey}, []string{storageJSONExtensionName})
	case bigquery.GeographyFieldType:
		af.Metadata = arrow.NewMetadata([]string{arrowExtensionNameKey}, []string{storageGeographyExtension})
	}
	return af, nil
}

func restArrowElementType(field *bigquery.FieldSchema) (arrow.DataType, error) {
	switch field.Type {
	case bigquery.StringFieldType, bigquery.JSONFieldType, bigquery.GeographyFieldType:
		return arrow.BinaryTypes.String, nil
	case bigquery.BytesFieldType:
		return arrow.BinaryTypes.Binary, nil
	case bigquery.IntegerFieldType:
		return arrow.PrimitiveTypes.Int64, nil
	case bigquery.FloatFieldType:
		return arrow.PrimitiveTypes.Float64, nil
	case bigquery.BooleanFieldType:
		return arrow.FixedWidthTypes.Boolean, nil
	case bigquery.TimestampFieldType:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, nil
	case bigquery.DateTimeFieldType:
		return &arrow.TimestampType{Unit: arrow.Microsecond}, nil
	case bigquery.DateFieldType:
		return arrow.FixedWidthTypes.Date32, nil
	case bigquery.TimeFieldType:
		return arrow.FixedWidthTypes.Time64us, nil
	case bigquery.NumericFieldType:
		return &arrow.Decimal128Type{Precision: storageNumericPrecision, Scale: storageNumericScale}, nil
	case bigquery.BigNumericFieldType:
		return &arrow.Decimal256Type{Precision: storageBigNumericPrecision, Scale: storageBigNumericScale}, nil
	case bigquery.IntervalFieldType:
		return arrow.FixedWidthTypes.MonthDayNanoInterval, nil
	case bigquery.RecordFieldType:
		children := make([]arrow.Field, len(field.Schema))
		for i, child := range field.Schema {
			af, err := restArrowField(child)
			if err != nil {
				return nil, err
			}
			children[i] = af
		}
		return arrow.StructOf(children...), nil
	case bigquery.RangeFieldType:
		if field.RangeElementType == nil {
			return nil, errors.New("RANGE column has no element type")
		}
		elem, err := restArrowElementType(&bigquery.FieldSchema{Type: field.RangeElementType.Type})
		if err != nil {
			return nil, err
		}
		return arrow.StructOf(
			arrow.Field{Name: "start", Type: elem, Nullable: true},
			arrow.Field{Name: "end", Type: elem, Nullable: true},
		), nil
	default:
		return nil, fmt.Errorf("unsupported BigQuery type %s", field.Type)
	}
}

// restBatchBuilder accumulates REST rows into Arrow record batches.
type restBatchBuilder struct {
	fields  bigquery.Schema
	schema  *arrow.Schema
	builder *array.RecordBuilder
	rows    int64
	bytes   int64
}

func newRESTBatchBuilder(fields bigquery.Schema) (*restBatchBuilder, error) {
	arrowSchema, err := restArrowSchema(fields)
	if err != nil {
		return nil, err
	}
	return &restBatchBuilder{
		fields:  fields,
		schema:  arrowSchema,
		builder: array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema),
	}, nil
}

// appendRow appends one row; values must be ordered like the builder fields.
func (b *restBatchBuilder) appendRow(values []bigquery.Value) error {
	if len(values) != len(b.fields) {
		return fmt.Errorf("BigQuery row has %d values, expected %d", len(values), len(b.fields))
	}
	for i, field := range b.fields {
		if err := appendRESTValue(b.builder.Field(i), field, values[i]); err != nil {
			return fmt.Errorf("column %q: %w", field.Name, err)
		}
		b.bytes += restValueBytes(values[i])
	}
	b.rows++
	return nil
}

func (b *restBatchBuilder) full(batchSize int, maxBatchBytes int64) bool {
	return (batchSize > 0 && b.rows >= int64(batchSize)) || (maxBatchBytes > 0 && b.bytes >= maxBatchBytes)
}

// flush returns the buffered rows as a batch, or nil when empty.
func (b *restBatchBuilder) flush() arrow.RecordBatch {
	if b.rows == 0 {
		return nil
	}
	b.rows = 0
	b.bytes = 0
	return b.builder.NewRecordBatch()
}

func (b *restBatchBuilder) release() {
	b.builder.Release()
}

func appendRESTValue(builder array.Builder, field *bigquery.FieldSchema, value bigquery.Value) error {
	if value == nil {
		builder.AppendNull()
		return nil
	}
	if !field.Repeated {
		return appendRESTScalar(builder, field, value)
	}

	items, ok := value.([]bigquery.Value)
	if !ok {
		return unexpectedRESTValue(field, value)
	}
	lb, ok := builder.(*array.ListBuilder)
	if !ok {
		return fmt.Errorf("unexpected Arrow builder %T for REPEATED column", builder)
	}
	elem := *field
	elem.Repeated = false
	lb.Append(true)
	for _, item := range items {
		if err := appendRESTValue(lb.ValueBuilder(), &elem, item); err != nil {
			return err
		}
	}
	return nil
}

func appendRESTScalar(builder array.Builder, field *bigquery.FieldSchema, value bigquery.Value) error {
	switch b := builder.(type) {
	case *array.StringBuilder:
		v, ok := value.(string)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(v)
	case *array.BinaryBuilder:
		v, ok := value.([]byte)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(v)
	case *array.Int64Builder:
		v, ok := value.(int64)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(v)
	case *array.Float64Builder:
		v, ok := value.(float64)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(v)
	case *array.BooleanBuilder:
		v, ok := value.(bool)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(v)
	case *array.TimestampBuilder:
		micros, err := restTimestampMicros(field, value)
		if err != nil {
			return err
		}
		b.Append(arrow.Timestamp(micros))
	case *array.Date32Builder:
		v, ok := value.(civil.Date)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(arrow.Date32(v.DaysSince(unixEpochDate)))
	case *array.Time64Builder:
		v, ok := value.(civil.Time)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		b.Append(arrow.Time64(civilTimeMicros(v)))
	case *array.Decimal128Builder:
		scaled, err := scaledRat(field, value, numericScaleFactor)
		if err != nil {
			return err
		}
		if scaled.BitLen() > 127 {
			return fmt.Errorf("%s value overflows decimal128", field.Type)
		}
		b.Append(decimal128.FromBigInt(scaled))
	case *array.Decimal256Builder:
		scaled, err := scaledRat(field, value, bigNumericScaleFactor)
		if err != nil {
			return err
		}
		num, err := bigIntToDecimal256(scaled)
		if err != nil {
			return fmt.Errorf("%s value %w", field.Type, err)
		}
		b.Append(num)
	case *array.MonthDayNanoIntervalBuilder:
		v, ok := value.(*bigquery.IntervalValue)
		if !ok {
			return unexpectedRESTValue(field, value)
		}
		interval, err := intervalToMonthDayNano(v)
		if err != nil {
			return err
		}
		b.Append(interval)
	case *array.StructBuilder:
		if field.Type == bigquery.RangeFieldType {
			return appendRESTRange(b, field, value)
		}
		return appendRESTRecord(b, field, value)
	default:
		return fmt.Errorf("unsupported Arrow builder %T for BigQuery type %s", builder, field.Type)
	}
	return nil
}

func appendRESTRecord(b *array.StructBuilder, field *bigquery.FieldSchema, value bigquery.Value) error {
	values, ok := value.([]bigquery.Value)
	if !ok {
		return unexpectedRESTValue(field, value)
	}
	if len(values) != len(field.Schema) {
		return fmt.Errorf("RECORD has %d values, expected %d", len(values), len(field.Schema))
	}
	b.Append(true)
	for i, child := range field.Schema {
		if err := appendRESTValue(b.FieldBuilder(i), child, values[i]); err != nil {
			return fmt.Errorf("field %q: %w", child.Name, err)
		}
	}
	return nil
}

func appendRESTRange(b *array.StructBuilder, field *bigquery.FieldSchema, value bigquery.Value) error {
	v, ok := value.(*bigquery.RangeValue)
	if !ok {
		return unexpectedRESTValue(field, value)
	}
	elem := &bigquery.FieldSchema{Name: field.Name, Type: field.RangeElementType.Type}
	b.Append(true)
	if err := appendRESTValue(b.FieldBuilder(0), elem, v.Start); err != nil {
		return err
	}
	return appendRESTValue(b.FieldBuilder(1), elem, v.End)
}

func restTimestampMicros(field *bigquery.FieldSchema, value bigquery.Value) (int64, error) {
	switch v := value.(type) {
	case time.Time:
		if field.Type == bigquery.TimestampFieldType {
			return v.UnixMicro(), nil
		}
	case civil.DateTime:
		if field.Type == bigquery.DateTimeFieldType {
			return v.In(time.UTC).UnixMicro(), nil
		}
	}
	return 0, unexpectedRESTValue(field, value)
}

func civilTimeMicros(t civil.Time) int64 {
	d := time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute +
		time.Duration(t.Second)*time.Second + time.Duration(t.Nanosecond)
	return d.Microseconds()
}

// scaledRat returns value*factor, which must be an integer: BigQuery NUMERIC
// and BIGNUMERIC values never have more fractional digits than their scale.
func scaledRat(field *bigquery.FieldSchema, value bigquery.Value, factor *big.Int) (*big.Int, error) {
	r, ok := value.(*big.Rat)
	if !ok || r == nil {
		return nil, unexpectedRESTValue(field, value)
	}
	num := new(big.Int).Mul(r.Num(), factor)
	quo, rem := new(big.Int).QuoRem(num, r.Denom(), new(big.Int))
	if rem.Sign() != 0 {
		return nil, fmt.Errorf("%s value %s has more fractional digits than the column scale", field.Type, r.RatString())
	}
	return quo, nil
}

// bigIntToDecimal256 is decimal256.FromBigInt without its panic, also
// accepting -2^255 (the minimum BIGNUMERIC), whose magnitude needs 256 bits.
func bigIntToDecimal256(v *big.Int) (decimal256.Num, error) {
	if v.BitLen() <= 255 {
		return decimal256.FromBigInt(v), nil
	}
	if v.Cmp(minInt256) == 0 {
		return decimal256.FromBigInt(new(big.Int).Add(v, big.NewInt(1))).Sub(decimal256.FromI64(1)), nil
	}
	return decimal256.Num{}, errors.New("overflows decimal256")
}

func intervalToMonthDayNano(v *bigquery.IntervalValue) (arrow.MonthDayNanoInterval, error) {
	seconds := (int64(v.Hours)*60+int64(v.Minutes))*60 + int64(v.Seconds)
	if seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
		return arrow.MonthDayNanoInterval{}, fmt.Errorf("INTERVAL %s overflows the Arrow month_day_nano time part", v.String())
	}
	return arrow.MonthDayNanoInterval{
		Months:      v.Years*12 + v.Months,
		Days:        v.Days,
		Nanoseconds: seconds*int64(time.Second) + int64(v.SubSecondNanos),
	}, nil
}

func unexpectedRESTValue(field *bigquery.FieldSchema, value bigquery.Value) error {
	return fmt.Errorf("unexpected value of type %T for BigQuery type %s", value, field.Type)
}

// restValueBytes approximates a value's in-memory size for --max-batch-bytes.
func restValueBytes(value bigquery.Value) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case string:
		return int64(len(v))
	case []byte:
		return int64(len(v))
	case []bigquery.Value:
		var total int64
		for _, item := range v {
			total += restValueBytes(item)
		}
		return total
	case *big.Rat:
		return 32
	case *bigquery.RangeValue:
		return restValueBytes(v.Start) + restValueBytes(v.End)
	default:
		return 8
	}
}
