package arrowconv

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func columnIndex(record arrow.RecordBatch, name string) int {
	for i := 0; i < int(record.NumCols()); i++ {
		if record.ColumnName(i) == name {
			return i
		}
	}
	return -1
}

func TestItemsToArrowRecordWithSchema_ExcludeColumns(t *testing.T) {
	items := []map[string]interface{}{
		{
			"a":     "x",
			"b":     float64(1),
			"extra": "ignored",
		},
	}
	cols := []schema.Column{
		{Name: "a", DataType: schema.TypeString, Nullable: true},
		{Name: "b", DataType: schema.TypeInt64, Nullable: true},
	}

	record, err := ItemsToArrowRecordWithSchema(items, cols, []string{"b", "extra"})
	require.NoError(t, err)
	require.NotNil(t, record)

	assert.Equal(t, int64(1), record.NumRows())
	assert.Equal(t, 1, int(record.NumCols()))
	assert.Equal(t, "a", record.ColumnName(0))
	assert.Equal(t, -1, columnIndex(record, "b"))
	assert.Equal(t, -1, columnIndex(record, "extra"))
}

func TestItemsToArrowRecordWithSchema_ExtraColumnDefaultsToUnknown(t *testing.T) {
	items := []map[string]interface{}{
		{
			"a":     "x",
			"extra": 1.5,
		},
		{
			"a":     "y",
			"extra": 2.0,
		},
	}
	cols := []schema.Column{
		{Name: "a", DataType: schema.TypeString, Nullable: true},
	}

	record, err := ItemsToArrowRecordWithSchema(items, cols, nil)
	require.NoError(t, err)
	require.NotNil(t, record)

	assert.Equal(t, int64(2), record.NumRows())
	assert.Equal(t, 2, int(record.NumCols()))
	assert.Equal(t, 0, columnIndex(record, "a"))
	assert.Equal(t, 1, columnIndex(record, "extra"))

	field := record.Schema().Field(1)
	assert.True(t, arrow.TypeEqual(field.Type, schema.UnknownArrowType))

	ext, ok := record.Column(1).(array.ExtensionArray)
	require.True(t, ok)
	storage := ext.Storage().(*array.String)
	assert.Equal(t, "1.5", storage.Value(0))
	assert.Equal(t, "2", storage.Value(1))
}

func TestItemsToArrowRecordWithSchema_EmptyItems(t *testing.T) {
	record, err := ItemsToArrowRecordWithSchema([]map[string]interface{}{}, []schema.Column{{Name: "a", DataType: schema.TypeString}}, nil)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, int64(0), record.NumRows())
	assert.Equal(t, 1, int(record.NumCols()))
	assert.Equal(t, "a", record.ColumnName(0))
}

func TestItemsToArrowRecordWithSchema_ExcludeAll(t *testing.T) {
	items := []map[string]interface{}{
		{"a": "x"},
	}
	cols := []schema.Column{
		{Name: "a", DataType: schema.TypeString, Nullable: true},
	}
	record, err := ItemsToArrowRecordWithSchema(items, cols, []string{"a"})
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, int64(0), record.NumRows())
	assert.Equal(t, 0, int(record.NumCols()))
}

// Test that a nil field in the items results in a JSON column.
func TestItemsToArrowRecordWithSchema_UnknownNilField(t *testing.T) {
	items := []map[string]interface{}{
		{"a": "x", "extra": nil},
	}
	cols := []schema.Column{
		{Name: "a", DataType: schema.TypeString, Nullable: true},
	}

	record, err := ItemsToArrowRecordWithSchema(items, cols, nil)
	require.NoError(t, err)
	require.NotNil(t, record)

	idx := columnIndex(record, "extra")
	require.NotEqual(t, -1, idx)
	field := record.Schema().Field(idx)
	assert.True(t, arrow.TypeEqual(field.Type, schema.UnknownArrowType))
}

func TestUnixToMicroseconds(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		wantUsec int64
	}{
		{
			name:     "seconds (10 digits)",
			input:    1771583633,
			wantUsec: 1771583633_000_000,
		},
		{
			name:     "milliseconds (13 digits)",
			input:    1771583633045,
			wantUsec: 1771583633045_000,
		},
		{
			name:     "microseconds (16 digits)",
			input:    1771583633045000,
			wantUsec: 1771583633045000,
		},
		{
			name:     "nanoseconds (19 digits)",
			input:    1771583633045000000,
			wantUsec: 1771583633045000,
		},
		{
			name:     "zero",
			input:    0,
			wantUsec: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnixToMicroseconds(tt.input)
			assert.Equal(t, tt.wantUsec, got)
		})
	}
}

func TestUnixToMicroseconds_RoundTrip(t *testing.T) {
	// Known timestamp: 2026-02-20 10:33:53.045 UTC
	expected := time.Date(2026, 2, 20, 10, 33, 53, 45_000_000, time.UTC)

	ms := expected.UnixMilli() // 1771583633045
	us := expected.UnixMicro() // 1771583633045000

	// Milliseconds and microseconds preserve sub-second precision
	assert.Equal(t, expected, time.UnixMicro(UnixToMicroseconds(ms)).UTC())
	assert.Equal(t, expected, time.UnixMicro(UnixToMicroseconds(us)).UTC())

	// Seconds lose sub-second precision (expected)
	sec := expected.Unix() // 1771583633
	truncated := time.Date(2026, 2, 20, 10, 33, 53, 0, time.UTC)
	assert.Equal(t, truncated, time.UnixMicro(UnixToMicroseconds(sec)).UTC())
}

func TestAppendValue_TimestampBuilder(t *testing.T) {
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	mem := memory.DefaultAllocator

	// Known timestamp: 2026-03-02 06:12:41.778 UTC
	expected := time.Date(2026, 3, 2, 6, 12, 41, 778_000_000, time.UTC)
	unixMs := expected.UnixMilli() // 1772431961778
	unixSec := expected.Unix()     // 1772431961
	expectedUsec := expected.UnixMicro()

	tests := []struct {
		name     string
		val      interface{}
		wantUsec int64
		wantNull bool
	}{
		{
			name:     "time.Time",
			val:      expected,
			wantUsec: expectedUsec,
		},
		{
			name:     "*time.Time",
			val:      &expected,
			wantUsec: expectedUsec,
		},
		{
			name:     "float64 milliseconds",
			val:      float64(unixMs),
			wantUsec: expectedUsec,
		},
		{
			name:     "int64 milliseconds",
			val:      unixMs,
			wantUsec: expectedUsec,
		},
		{
			name:     "int64 seconds",
			val:      unixSec,
			wantUsec: time.Unix(unixSec, 0).UnixMicro(),
		},
		{
			name:     "int seconds",
			val:      int(unixSec),
			wantUsec: time.Unix(unixSec, 0).UnixMicro(),
		},
		{
			name:     "json.Number milliseconds",
			val:      json.Number("1772431961778"),
			wantUsec: expectedUsec,
		},
		{
			name:     "string unix milliseconds",
			val:      "1772431961778",
			wantUsec: expectedUsec,
		},
		{
			name:     "string ISO timestamp",
			val:      "2026-03-02T06:12:41.778Z",
			wantUsec: expectedUsec,
		},
		{
			name:     "nil",
			val:      nil,
			wantNull: true,
		},
		{
			name:     "unparseable string",
			val:      "not-a-date",
			wantNull: true,
		},
		{
			name:     "nil *time.Time",
			val:      (*time.Time)(nil),
			wantNull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := array.NewTimestampBuilder(mem, tsType)
			defer builder.Release()

			AppendValue(builder, tt.val)
			arr := builder.NewArray().(*array.Timestamp)
			defer arr.Release()

			require.Equal(t, 1, arr.Len())
			if tt.wantNull {
				assert.True(t, arr.IsNull(0), "expected null")
			} else {
				assert.False(t, arr.IsNull(0), "expected non-null")
				got := int64(arr.Value(0))
				assert.Equal(t, tt.wantUsec, got,
					"got %s, want %s",
					time.UnixMicro(got).UTC().Format(time.RFC3339Nano),
					time.UnixMicro(tt.wantUsec).UTC().Format(time.RFC3339Nano))
			}
		})
	}
}

func TestAppendValue_ListTimestampBuilder(t *testing.T) {
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
	builder := array.NewListBuilder(memory.DefaultAllocator, tsType)
	defer builder.Release()

	first := time.Date(2026, 6, 10, 12, 0, 0, 123_000_000, time.UTC)
	second := time.Date(2026, 6, 10, 13, 30, 0, 456_000_000, time.UTC)

	AppendValue(builder, []time.Time{first, second})

	arr := builder.NewArray().(*array.List)
	defer arr.Release()

	require.Equal(t, 1, arr.Len())
	assert.False(t, arr.IsNull(0))

	values := arr.ListValues().(*array.Timestamp)
	require.Equal(t, 2, values.Len())
	assert.Equal(t, arrow.Timestamp(first.UnixMicro()), values.Value(0))
	assert.Equal(t, arrow.Timestamp(second.UnixMicro()), values.Value(1))
}

func TestAppendValue_ListNullableScalarElements(t *testing.T) {
	textBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.String)
	defer textBuilder.Release()
	alpha := "alpha"
	omega := "omega"
	AppendValue(textBuilder, []*string{&alpha, nil, &omega})

	textArr := textBuilder.NewArray().(*array.List)
	defer textArr.Release()
	textValues := textArr.ListValues().(*array.String)
	require.Equal(t, 3, textValues.Len())
	assert.Equal(t, "alpha", textValues.Value(0))
	assert.True(t, textValues.IsNull(1))
	assert.Equal(t, "omega", textValues.Value(2))

	intBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.PrimitiveTypes.Int32)
	defer intBuilder.Release()
	one := uint16(1)
	two := uint16(2)
	AppendValue(intBuilder, []*uint16{&one, nil, &two})

	intArr := intBuilder.NewArray().(*array.List)
	defer intArr.Release()
	intValues := intArr.ListValues().(*array.Int32)
	require.Equal(t, 3, intValues.Len())
	assert.Equal(t, int32(1), intValues.Value(0))
	assert.True(t, intValues.IsNull(1))
	assert.Equal(t, int32(2), intValues.Value(2))

	boolBuilder := array.NewListBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Boolean)
	defer boolBuilder.Release()
	yes := true
	no := false
	AppendValue(boolBuilder, []*bool{&yes, nil, &no})

	boolArr := boolBuilder.NewArray().(*array.List)
	defer boolArr.Release()
	boolValues := boolArr.ListValues().(*array.Boolean)
	require.Equal(t, 3, boolValues.Len())
	assert.True(t, boolValues.Value(0))
	assert.True(t, boolValues.IsNull(1))
	assert.False(t, boolValues.Value(2))
}

func TestAppendValue_ListStringerElements(t *testing.T) {
	builder := array.NewListBuilder(memory.DefaultAllocator, arrow.BinaryTypes.String)
	defer builder.Release()

	firstUUID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	secondUUID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	AppendValue(builder, []*uuid.UUID{&firstUUID, nil, &secondUUID})
	AppendValue(builder, []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("8.8.8.8")})

	arr := builder.NewArray().(*array.List)
	defer arr.Release()
	values := arr.ListValues().(*array.String)
	require.Equal(t, 5, values.Len())
	assert.Equal(t, firstUUID.String(), values.Value(0))
	assert.True(t, values.IsNull(1))
	assert.Equal(t, secondUUID.String(), values.Value(2))
	assert.Equal(t, "127.0.0.1", values.Value(3))
	assert.Equal(t, "8.8.8.8", values.Value(4))
}

func TestAppendValue_ListDecimalBuilder(t *testing.T) {
	dt := &arrow.Decimal128Type{Precision: 18, Scale: 5}
	builder := array.NewListBuilder(memory.DefaultAllocator, dt)
	defer builder.Release()

	first := decimal.RequireFromString("12.34567")
	second := decimal.RequireFromString("89.00001")
	AppendValue(builder, []*decimal.Decimal{&first, nil, &second})

	arr := builder.NewArray().(*array.List)
	defer arr.Release()
	values := arr.ListValues().(*array.Decimal128)
	require.Equal(t, 3, values.Len())
	assert.Equal(t, "1234567", decimal128.Num(values.Value(0)).BigInt().String())
	assert.True(t, values.IsNull(1))
	assert.Equal(t, "8900001", decimal128.Num(values.Value(2)).BigInt().String())
}

func TestAppendValue_Decimal128_JSONNumber(t *testing.T) {
	dt := &arrow.Decimal128Type{Precision: 38, Scale: 0}

	tests := []struct {
		name     string
		val      json.Number
		wantNull bool
		wantBigI string
	}{
		{name: "simple integer", val: json.Number("1"), wantBigI: "1"},
		{name: "large positive", val: json.Number("42"), wantBigI: "42"},
		{name: "negative", val: json.Number("-7"), wantBigI: "-7"},
		{name: "scientific notation", val: json.Number("1.5e10"), wantBigI: "15000000000"},
		{name: "empty string", val: json.Number(""), wantNull: true},
		{name: "garbage", val: json.Number("xyz"), wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := array.NewDecimal128Builder(memory.NewGoAllocator(), dt)
			AppendValue(b, tt.val)
			arr := b.NewArray().(*array.Decimal128)
			defer arr.Release()

			require.Equal(t, 1, arr.Len())
			if tt.wantNull {
				assert.True(t, arr.IsNull(0), "expected null for input %q", string(tt.val))
				return
			}
			assert.False(t, arr.IsNull(0), "got null for input %q", string(tt.val))
			gotBigI := decimal128.Num(arr.Value(0)).BigInt().String()
			assert.Equal(t, tt.wantBigI, gotBigI)
		})
	}
}

func TestParseDecimal128Fast_MatchesFromString(t *testing.T) {
	inputs := []string{
		"0", "1", "-1", "+5", "15716.10", "0.02", "-0.08", "1234567890.99",
		"9999999999999.99", "0.10", ".5", "5.", "-123.45", "00042.10",
	}
	for _, s := range inputs {
		got, ok := parseDecimal128Fast(s, 15, 2)
		if !ok {
			t.Errorf("parseDecimal128Fast(%q) unexpectedly fell back", s)
			continue
		}
		want, err := decimal128.FromString(s, 15, 2)
		if err != nil {
			t.Fatalf("FromString(%q) error: %v", s, err)
		}
		if got != want {
			t.Errorf("parseDecimal128Fast(%q) = %v; want %v", s, got, want)
		}
	}
}

func TestParseDecimal128Fast_FallsBack(t *testing.T) {
	inputs := []string{
		"",                    // no digits
		"-",                   // sign only
		".",                   // dot only
		"1e5",                 // exponent
		"1.234",               // more fractional digits than scale → needs rounding
		"abc",                 // not a number
		"1.2.3",               // double dot
		"1234567890123456789", // > 18 digits
		"10000000000000.00",   // exceeds precision 15 after scaling
		"999999999999999",     // 15 digits + scale-2 padding exceeds precision 15
		"nan",
	}
	for _, s := range inputs {
		if _, ok := parseDecimal128Fast(s, 15, 2); ok {
			t.Errorf("parseDecimal128Fast(%q) should have fallen back", s)
		}
	}
}

func TestAppendValueRecursiveAndFixedBuilders(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	structType := arrow.StructOf(
		arrow.Field{Name: "Name", Type: arrow.BinaryTypes.String, Nullable: true},
		arrow.Field{Name: "count", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	)
	structBuilder := array.NewStructBuilder(mem, structType)
	AppendValue(structBuilder, map[string]interface{}{"name": "alpha", "count": int64(7)})
	structArray := structBuilder.NewStructArray()
	structBuilder.Release()
	require.Equal(t, "alpha", structArray.Field(0).(*array.String).Value(0))
	require.Equal(t, int64(7), structArray.Field(1).(*array.Int64).Value(0))
	structArray.Release()

	mapBuilder := array.NewMapBuilder(mem, arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int64, false)
	AppendValue(mapBuilder, map[string]int64{"b": 2, "a": 1})
	mapArray := mapBuilder.NewMapArray()
	mapBuilder.Release()
	require.Equal(t, []string{"a", "b"}, []string{mapArray.Keys().(*array.String).Value(0), mapArray.Keys().(*array.String).Value(1)})
	require.Equal(t, []int64{1, 2}, []int64{mapArray.Items().(*array.Int64).Value(0), mapArray.Items().(*array.Int64).Value(1)})
	mapArray.Release()

	fixedBinaryBuilder := array.NewFixedSizeBinaryBuilder(mem, &arrow.FixedSizeBinaryType{ByteWidth: 4})
	AppendValue(fixedBinaryBuilder, []byte{1, 2, 3, 4})
	AppendValue(fixedBinaryBuilder, []byte{1, 2})
	fixedBinary := fixedBinaryBuilder.NewFixedSizeBinaryArray()
	fixedBinaryBuilder.Release()
	require.Equal(t, []byte{1, 2, 3, 4}, fixedBinary.Value(0))
	require.True(t, fixedBinary.IsNull(1))
	fixedBinary.Release()

	fixedListBuilder := array.NewFixedSizeListBuilder(mem, 2, arrow.PrimitiveTypes.Int64)
	AppendValue(fixedListBuilder, []int64{3, 4})
	AppendValue(fixedListBuilder, []int64{5})
	fixedList := fixedListBuilder.NewListArray()
	fixedListBuilder.Release()
	require.False(t, fixedList.IsNull(0))
	require.True(t, fixedList.IsNull(1))
	values := fixedList.ListValues().(*array.Int64)
	require.Equal(t, []int64{3, 4}, []int64{values.Value(0), values.Value(1)})
	fixedList.Release()
}
