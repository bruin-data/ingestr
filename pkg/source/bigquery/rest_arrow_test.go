package bigquery

import (
	"math/big"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildRESTBatch(t *testing.T, fields bigquery.Schema, rows ...[]bigquery.Value) arrow.RecordBatch {
	t.Helper()
	b, err := newRESTBatchBuilder(fields)
	require.NoError(t, err)
	defer b.release()
	for _, row := range rows {
		require.NoError(t, b.appendRow(row))
	}
	batch := b.flush()
	require.NotNil(t, batch)
	t.Cleanup(batch.Release)
	return batch
}

func TestRESTArrowSchemaMatchesStorageForAllTypes(t *testing.T) {
	fields := bigquery.Schema{
		{Name: "id", Type: bigquery.IntegerFieldType, Required: true},
		{Name: "b", Type: bigquery.BooleanFieldType},
		{Name: "f", Type: bigquery.FloatFieldType},
		{Name: "s", Type: bigquery.StringFieldType},
		{Name: "by", Type: bigquery.BytesFieldType},
		{Name: "ts", Type: bigquery.TimestampFieldType},
		{Name: "dt", Type: bigquery.DateTimeFieldType},
		{Name: "d", Type: bigquery.DateFieldType},
		{Name: "t", Type: bigquery.TimeFieldType},
		{Name: "n", Type: bigquery.NumericFieldType, Precision: 10, Scale: 2},
		{Name: "bn", Type: bigquery.BigNumericFieldType},
		{Name: "j", Type: bigquery.JSONFieldType},
		{Name: "g", Type: bigquery.GeographyFieldType},
		{Name: "iv", Type: bigquery.IntervalFieldType},
		{Name: "r", Type: bigquery.RangeFieldType, RangeElementType: &bigquery.RangeElementType{Type: bigquery.DateFieldType}},
		{Name: "rec", Type: bigquery.RecordFieldType, Schema: bigquery.Schema{
			{Name: "x", Type: bigquery.StringFieldType},
			{Name: "inner", Type: bigquery.RecordFieldType, Schema: bigquery.Schema{{Name: "z", Type: bigquery.FloatFieldType}}},
		}},
		{Name: "arr", Type: bigquery.IntegerFieldType, Repeated: true},
		{Name: "recs", Type: bigquery.RecordFieldType, Repeated: true, Schema: bigquery.Schema{{Name: "k", Type: bigquery.StringFieldType}}},
	}

	got, err := restArrowSchema(fields)
	require.NoError(t, err)
	want := storageAllTypesSchema()
	assert.Truef(t, want.Equal(got), "want %s\ngot  %s", want, got)
}

func TestRESTArrowRepeatedJSONAndGeographyKeepMetadataOnListField(t *testing.T) {
	got, err := restArrowSchema(bigquery.Schema{
		{Name: "js", Type: bigquery.JSONFieldType, Repeated: true},
		{Name: "rec", Type: bigquery.RecordFieldType, Schema: bigquery.Schema{{Name: "geo", Type: bigquery.GeographyFieldType}}},
	})
	require.NoError(t, err)

	js := got.Field(0)
	assert.True(t, arrow.TypeEqual(arrow.ListOf(arrow.BinaryTypes.String), js.Type))
	assert.True(t, js.Metadata.Equal(jsonMetadata()))

	child := got.Field(1).Type.(*arrow.StructType).Field(0)
	assert.True(t, child.Metadata.Equal(geographyMetadata()))
}

func TestRESTArrowConvertsTemporalEdgeValues(t *testing.T) {
	fields := bigquery.Schema{
		{Name: "ts", Type: bigquery.TimestampFieldType},
		{Name: "dt", Type: bigquery.DateTimeFieldType},
		{Name: "d", Type: bigquery.DateFieldType},
		{Name: "t", Type: bigquery.TimeFieldType},
	}
	batch := buildRESTBatch(t, fields,
		[]bigquery.Value{
			time.Date(1900, 1, 1, 0, 0, 0, 1000, time.FixedZone("x", 3600)),
			civil.DateTime{Date: civil.Date{Year: 1, Month: 1, Day: 1}, Time: civil.Time{Nanosecond: 999}},
			civil.Date{Year: 1, Month: 1, Day: 1},
			civil.Time{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999000},
		},
		[]bigquery.Value{
			time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC),
			civil.DateTime{Date: civil.Date{Year: 9999, Month: 12, Day: 31}, Time: civil.Time{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999000}},
			civil.Date{Year: 9999, Month: 12, Day: 31},
			civil.Time{},
		},
	)

	ts := batch.Column(0).(*array.Timestamp)
	assert.Equal(t, arrow.Timestamp(time.Date(1899, 12, 31, 23, 0, 0, 1000, time.UTC).UnixMicro()), ts.Value(0))
	assert.Equal(t, arrow.Timestamp(253402300799999999), ts.Value(1))

	dt := batch.Column(1).(*array.Timestamp)
	assert.Equal(t, arrow.Timestamp(-62135596800000000), dt.Value(0), "sub-microsecond DATETIME digits are dropped")
	assert.Equal(t, arrow.Timestamp(253402300799999999), dt.Value(1))

	d := batch.Column(2).(*array.Date32)
	assert.Equal(t, arrow.Date32(-719162), d.Value(0))
	assert.Equal(t, arrow.Date32(2932896), d.Value(1))

	tm := batch.Column(3).(*array.Time64)
	assert.Equal(t, arrow.Time64(86399999999), tm.Value(0))
	assert.Equal(t, arrow.Time64(0), tm.Value(1))
}

func TestRESTArrowConvertsDecimalExtremes(t *testing.T) {
	maxNumeric, _ := new(big.Rat).SetString("99999999999999999999999999999.999999999")
	maxBigNumeric, _ := new(big.Rat).SetString("578960446186580977117854925043439539266.34992332820282019728792003956564819967")
	minBigNumeric, _ := new(big.Rat).SetString("-578960446186580977117854925043439539266.34992332820282019728792003956564819968")

	batch := buildRESTBatch(t, bigquery.Schema{
		{Name: "n", Type: bigquery.NumericFieldType},
		{Name: "bn", Type: bigquery.BigNumericFieldType},
	},
		[]bigquery.Value{maxNumeric, maxBigNumeric},
		[]bigquery.Value{new(big.Rat), minBigNumeric},
	)

	n := batch.Column(0).(*array.Decimal128)
	assert.Equal(t, "99999999999999999999999999999.999999999", n.Value(0).ToString(9))
	assert.Equal(t, "0", n.Value(1).ToString(0))

	bn := batch.Column(1).(*array.Decimal256)
	maxScaled, _ := new(big.Int).SetString("57896044618658097711785492504343953926634992332820282019728792003956564819967", 10)
	minScaled, _ := new(big.Int).SetString("-57896044618658097711785492504343953926634992332820282019728792003956564819968", 10)
	assert.Equal(t, 0, maxScaled.Cmp(bn.Value(0).BigInt()), bn.Value(0).BigInt().String())
	assert.Equal(t, 0, minScaled.Cmp(bn.Value(1).BigInt()), bn.Value(1).BigInt().String())
}

func TestRESTArrowConvertsNegativeInterval(t *testing.T) {
	batch := buildRESTBatch(t, bigquery.Schema{{Name: "iv", Type: bigquery.IntervalFieldType}},
		[]bigquery.Value{&bigquery.IntervalValue{Years: -1, Months: -2, Days: 5, Hours: -1, Minutes: -2, Seconds: -3, SubSecondNanos: -4}},
	)
	got := batch.Column(0).(*array.MonthDayNanoInterval).Value(0)
	assert.Equal(t, arrow.MonthDayNanoInterval{Months: -14, Days: 5, Nanoseconds: -3723000000004}, got)
}

func TestRESTArrowRejectsUnexpectedValues(t *testing.T) {
	tooPrecise, _ := new(big.Rat).SetString("1.0000000001")
	tests := []struct {
		name  string
		field *bigquery.FieldSchema
		value bigquery.Value
		want  string
	}{
		{name: "string for integer", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.IntegerFieldType}, value: "1", want: "unexpected value of type string for BigQuery type INTEGER"},
		{name: "time for datetime", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.DateTimeFieldType}, value: time.Now(), want: "unexpected value of type time.Time for BigQuery type DATETIME"},
		{name: "scalar for repeated", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.IntegerFieldType, Repeated: true}, value: int64(1), want: "unexpected value of type int64"},
		{name: "numeric too precise", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.NumericFieldType}, value: tooPrecise, want: "more fractional digits than the column scale"},
		{name: "bignumeric overflow", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.BigNumericFieldType}, value: new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 300)), want: "overflows decimal256"},
		{name: "record arity", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.RecordFieldType, Schema: bigquery.Schema{{Name: "a", Type: bigquery.StringFieldType}}}, value: []bigquery.Value{"a", "b"}, want: "RECORD has 2 values, expected 1"},
		{name: "interval overflow", field: &bigquery.FieldSchema{Name: "c", Type: bigquery.IntervalFieldType}, value: &bigquery.IntervalValue{Hours: 87840000}, want: "overflows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := newRESTBatchBuilder(bigquery.Schema{tt.field})
			require.NoError(t, err)
			defer b.release()
			err = b.appendRow([]bigquery.Value{tt.value})
			require.Error(t, err)
			assert.Contains(t, err.Error(), `column "c"`)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRESTArrowRejectsUnsupportedType(t *testing.T) {
	_, err := restArrowSchema(bigquery.Schema{{Name: "c", Type: bigquery.FieldType("FUTURE")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `column "c": unsupported BigQuery type FUTURE`)
}

func TestRESTBatchBuilderFlushesOnRowsAndBytes(t *testing.T) {
	b, err := newRESTBatchBuilder(bigquery.Schema{{Name: "s", Type: bigquery.StringFieldType}})
	require.NoError(t, err)
	defer b.release()

	require.NoError(t, b.appendRow([]bigquery.Value{"abcd"}))
	assert.False(t, b.full(2, 0))
	assert.True(t, b.full(0, 4))
	require.NoError(t, b.appendRow([]bigquery.Value{nil}))
	assert.True(t, b.full(2, 0))

	batch := b.flush()
	require.NotNil(t, batch)
	defer batch.Release()
	assert.EqualValues(t, 2, batch.NumRows())
	assert.Nil(t, b.flush())
}
