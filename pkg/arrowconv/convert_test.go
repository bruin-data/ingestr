package arrowconv

import (
	"encoding/json"
	"math"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/araddon/dateparse"
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
		{name: "pre-2001 seconds (9 digits)", input: 999999999, wantUsec: 999999999_000_000},
		{name: "largest seconds", input: 99999999999, wantUsec: 99999999999_000_000},
		{name: "smallest milliseconds", input: 100000000000, wantUsec: 100000000000_000},
		{name: "pre-2001 milliseconds (12 digits)", input: 946684800000, wantUsec: 946684800000_000},
		{name: "largest milliseconds", input: 99999999999999, wantUsec: 99999999999999_000},
		{name: "smallest microseconds", input: 100000000000000, wantUsec: 100000000000000},
		{name: "pre-2001 microseconds (15 digits)", input: 946684800000000, wantUsec: 946684800000000},
		{name: "largest microseconds", input: 99999999999999999, wantUsec: 99999999999999999},
		{name: "smallest nanoseconds", input: 100000000000000000, wantUsec: 100000000000000},
		{name: "pre-2001 nanoseconds (18 digits)", input: 946684800000000000, wantUsec: 946684800000000},
		{name: "max int64", input: math.MaxInt64, wantUsec: math.MaxInt64 / 1000},
		{name: "negative seconds", input: -86400, wantUsec: -86400_000_000},
		{name: "negative milliseconds", input: -946684800000, wantUsec: -946684800000_000},
		{name: "negative microseconds", input: -946684800000000, wantUsec: -946684800000000},
		{name: "negative nanoseconds", input: -946684800000000000, wantUsec: -946684800000000},
		{name: "negative nanoseconds with remainder", input: -946684800000000001, wantUsec: -946684800000001},
		{name: "min int64", input: math.MinInt64, wantUsec: time.Unix(0, math.MinInt64).UnixMicro()},
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
			name:     "string 9-digit unix seconds",
			val:      "999999999",
			wantUsec: time.Unix(999999999, 0).UnixMicro(),
		},
		{
			name:     "string 12-digit unix milliseconds",
			val:      "946684800000",
			wantUsec: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro(),
		},
		{
			name:     "string 15-digit unix microseconds",
			val:      "946684800000000",
			wantUsec: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro(),
		},
		{
			name:     "string 18-digit unix nanoseconds",
			val:      "946684800000000000",
			wantUsec: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro(),
		},
		{
			name:     "string negative unix seconds",
			val:      "-86400",
			wantUsec: time.Date(1969, 12, 31, 0, 0, 0, 0, time.UTC).UnixMicro(),
		},
		{
			name:     "string yyyyMMdd keeps date parsing",
			val:      "20200101",
			wantUsec: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro(),
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

func TestUnixFloatToMicroseconds(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want int64
	}{
		{name: "fractional seconds", in: 1700000000.5, want: 1700000000_500_000},
		{name: "fractional seconds with float noise", in: 1700000000.123, want: 1700000000_123_000},
		{name: "pre-2001 fractional seconds", in: 946684800.75, want: 946684800_750_000},
		{name: "fractional milliseconds", in: 1700000000000.5, want: 1700000000000_500},
		{name: "pre-2001 fractional milliseconds", in: 946684800000.25, want: 946684800000_250},
		{name: "sub-microsecond fraction dropped", in: 946684800000000.4, want: 946684800000000},
		{name: "below one second", in: 0.5, want: 500_000},
		{name: "negative below one second", in: -0.25, want: -250_000},
		{name: "negative fractional seconds", in: -1.5, want: -1_500_000},
		{name: "whole seconds", in: 1700000000, want: UnixToMicroseconds(1700000000)},
		{name: "whole milliseconds", in: 946684800000, want: UnixToMicroseconds(946684800000)},
		{name: "whole nanoseconds", in: 1.7e18, want: UnixToMicroseconds(int64(1.7e18))},
		{name: "zero", in: 0, want: 0},
		{name: "positive infinity keeps integer path", in: math.Inf(1), want: UnixToMicroseconds(int64(math.Inf(1)))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, unixFloatToMicroseconds(tt.in))
		})
	}
}

func TestEpochStringToMicroseconds(t *testing.T) {
	y2k := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		in     string
		want   int64
		wantOK bool
	}{
		{name: "zero", in: "0", want: 0, wantOK: true},
		{name: "one second", in: "1", want: 1_000_000, wantOK: true},
		{name: "one day seconds", in: "86400", want: 86400 * 1_000_000, wantOK: true},
		{name: "9-digit seconds", in: "999999999", want: time.Unix(999999999, 0).UnixMicro(), wantOK: true},
		{name: "y2k seconds", in: "946684800", want: y2k.UnixMicro(), wantOK: true},
		{name: "10-digit seconds", in: "1700000000", want: 1700000000 * 1_000_000, wantOK: true},
		{name: "largest seconds", in: "99999999999", want: 99999999999 * 1_000_000, wantOK: true},
		{name: "smallest milliseconds", in: "100000000000", want: 100000000000 * 1000, wantOK: true},
		{name: "12-digit milliseconds", in: "946684800000", want: y2k.UnixMicro(), wantOK: true},
		{name: "13-digit milliseconds", in: "1700000000000", want: 1700000000000 * 1000, wantOK: true},
		{name: "largest milliseconds", in: "99999999999999", want: 99999999999999 * 1000, wantOK: true},
		{name: "smallest microseconds", in: "100000000000000", want: 100000000000000, wantOK: true},
		{name: "15-digit microseconds", in: "946684800000000", want: y2k.UnixMicro(), wantOK: true},
		{name: "largest microseconds", in: "99999999999999999", want: 99999999999999999, wantOK: true},
		{name: "smallest nanoseconds", in: "100000000000000000", want: 100000000000000, wantOK: true},
		{name: "18-digit nanoseconds", in: "946684800000000000", want: y2k.UnixMicro(), wantOK: true},
		{name: "19-digit nanoseconds", in: "1700000000000000000", want: 1700000000000000, wantOK: true},
		{name: "max int64", in: "9223372036854775807", want: math.MaxInt64 / 1000, wantOK: true},
		{name: "leading zeros", in: "000946684800", want: y2k.UnixMicro(), wantOK: true},
		{name: "negative seconds", in: "-86400", want: -86400 * 1_000_000, wantOK: true},
		{name: "negative milliseconds", in: "-946684800000", want: -y2k.UnixMicro(), wantOK: true},
		{name: "negative microseconds", in: "-946684800000000", want: -y2k.UnixMicro(), wantOK: true},
		{name: "negative nanoseconds", in: "-946684800000000000", want: -y2k.UnixMicro(), wantOK: true},
		{name: "positive nanoseconds with remainder", in: "946684800000000999", want: y2k.UnixMicro(), wantOK: true},
		{name: "negative nanoseconds with remainder", in: "-946684800000000001", want: time.Unix(0, -946684800000000001).UnixMicro(), wantOK: true},
		{name: "min int64", in: "-9223372036854775808"},
		{name: "overflows int64", in: "99999999999999999999"},
		{name: "explicit plus sign", in: "+123"},
		{name: "surrounding spaces", in: " 123 "},
		{name: "empty", in: ""},
		{name: "minus only", in: "-"},
		{name: "decimal", in: "1.5"},
		{name: "scientific", in: "1e9"},
		{name: "trailing letters", in: "123abc"},
		{name: "text", in: "not-a-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := epochStringToMicroseconds(tt.in)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got,
					"got %s, want %s",
					time.UnixMicro(got).UTC().Format(time.RFC3339Nano),
					time.UnixMicro(tt.want).UTC().Format(time.RFC3339Nano))
			}
		})
	}
}

// Values dateparse already understood must convert exactly as before the epoch fallback existed.
func TestAppendValue_TimestampBuilder_DateparseResultsUnchanged(t *testing.T) {
	inputs := []string{
		"1700000000", "1772431961", "9999999999",
		"1700000000000", "1772431961778",
		"1700000000000000", "1772431961778123",
		"1700000000000000000", "1772431961778123456",
		"19700101", "20200101", "20260302",
		"20200101123045", "19991231235959",
		"2026-03-02", "2026-03-02T06:12:41Z", "2026-03-02 06:12:41.778",
		"2026/03/02", "03/02/2026", "Mon, 02 Mar 2026 06:12:41 +0000",
	}

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			parsed, err := dateparse.ParseAny(in)
			require.NoError(t, err, "input must be one dateparse accepts")

			builder := array.NewTimestampBuilder(memory.DefaultAllocator, tsType)
			defer builder.Release()
			AppendValue(builder, in)
			arr := builder.NewArray().(*array.Timestamp)
			defer arr.Release()

			require.False(t, arr.IsNull(0))
			assert.Equal(t, parsed.UnixMicro(), int64(arr.Value(0)))
		})
	}
}

func TestAppendValue_TimestampBuilder_NonEpochStringsStayNull(t *testing.T) {
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	for _, in := range []string{"", " ", "not-a-date", "1e9", "+123", " 123 ", "123abc", "-", "99999999999999999999"} {
		t.Run(in, func(t *testing.T) {
			builder := array.NewTimestampBuilder(memory.DefaultAllocator, tsType)
			defer builder.Release()
			AppendValue(builder, in)
			arr := builder.NewArray().(*array.Timestamp)
			defer arr.Release()

			assert.True(t, arr.IsNull(0))
		})
	}
}

func TestAppendValue_TimestampBuilder_PreY2KNumbers(t *testing.T) {
	y2k := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}

	tests := []struct {
		name string
		val  interface{}
		want time.Time
	}{
		{name: "json.Number seconds", val: json.Number("946684800"), want: y2k},
		{name: "json.Number milliseconds", val: json.Number("946684800000"), want: y2k},
		{name: "json.Number microseconds", val: json.Number("946684800000000"), want: y2k},
		{name: "json.Number nanoseconds", val: json.Number("946684800000000000"), want: y2k},
		{name: "json.Number float milliseconds", val: json.Number("946684800000.0"), want: y2k},
		{name: "int64 milliseconds", val: int64(946684800000), want: y2k},
		{name: "int milliseconds", val: 946684800000, want: y2k},
		{name: "float64 milliseconds", val: float64(946684800000), want: y2k},
		{name: "int64 negative seconds", val: int64(-86400), want: time.Date(1969, 12, 31, 0, 0, 0, 0, time.UTC)},
		{name: "float64 fractional seconds", val: 946684800.5, want: y2k.Add(500 * time.Millisecond)},
		{name: "float64 fractional milliseconds", val: 946684800000.5, want: y2k.Add(500 * time.Microsecond)},
		{name: "json.Number fractional seconds", val: json.Number("946684800.25"), want: y2k.Add(250 * time.Millisecond)},
		{name: "json.Number exponent seconds", val: json.Number("9.466848e8"), want: y2k},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := array.NewTimestampBuilder(memory.DefaultAllocator, tsType)
			defer builder.Release()
			AppendValue(builder, tt.val)
			arr := builder.NewArray().(*array.Timestamp)
			defer arr.Release()

			require.False(t, arr.IsNull(0))
			assert.Equal(t, tt.want, time.UnixMicro(int64(arr.Value(0))).UTC())
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

func TestAppendValue_Decimal256(t *testing.T) {
	dt := &arrow.Decimal256Type{Precision: 40, Scale: 25}

	bigUnscaled, _ := new(big.Int).SetString("12345678901234567890123456789012345", 10)

	tests := []struct {
		name     string
		val      any
		wantNull bool
		wantStr  string
	}{
		{name: "string", val: "123456789012345.1234567890123456789012345", wantStr: "123456789012345.1234567890123456789012345"},
		{name: "big.Int unscaled", val: bigUnscaled, wantStr: "1234567890.1234567890123456789012345"},
		{name: "json.Number", val: json.Number("1.5"), wantStr: "1.5000000000000000000000000"},
		{name: "empty string", val: "", wantNull: true},
		{name: "garbage", val: "xyz", wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := array.NewDecimal256Builder(memory.NewGoAllocator(), dt)
			AppendValue(b, tt.val)
			arr := b.NewArray().(*array.Decimal256)
			defer arr.Release()

			require.Equal(t, 1, arr.Len())
			if tt.wantNull {
				assert.True(t, arr.IsNull(0))
				return
			}
			assert.False(t, arr.IsNull(0))
			assert.Equal(t, tt.wantStr, arr.Value(0).ToString(dt.Scale))
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

func TestParseDecimal128BytesFast(t *testing.T) {
	inputs := []string{"0", "-1", "+5", "15716.10", ".5", "5.", "00042.10"}
	for _, input := range inputs {
		fromBytes, ok := ParseDecimal128BytesFast([]byte(input), 15, 2)
		if !ok {
			t.Fatalf("ParseDecimal128BytesFast(%q) unexpectedly fell back", input)
		}
		fromString, ok := parseDecimal128Fast(input, 15, 2)
		if !ok || fromBytes != fromString {
			t.Fatalf("byte parse for %q = %v; string parse = %v, %v", input, fromBytes, fromString, ok)
		}
	}
}

func TestRowBytes(t *testing.T) {
	// RowBytes is a cheap approximate sizer: sum of key + value content lengths,
	// ignoring JSON structure (punctuation/number formatting).
	if got := RowBytes(map[string]interface{}{}); got != 0 {
		t.Errorf("empty row = %d, want 0", got)
	}
	// key length + string value length
	if got := RowBytes(map[string]interface{}{"id": "hello"}); got != int64(len("id")+len("hello")) {
		t.Errorf("got %d, want %d", got, len("id")+len("hello"))
	}
	// scalars: nil=0, bool=1, number=8 (flat)
	if got := RowBytes(map[string]interface{}{"b": true, "n": 3.14, "z": nil}); got != int64(1+1+1+8+1+0) {
		t.Errorf("scalars got %d, want 12", got)
	}
	// recurses into nested arrays and objects
	nested := map[string]interface{}{"a": []interface{}{"xy", "z"}, "m": map[string]interface{}{"k": "vv"}}
	want := int64(len("a") + (2 + 1) + len("m") + (len("k") + 2))
	if got := RowBytes(nested); got != want {
		t.Errorf("nested got %d, want %d", got, want)
	}
	// larger content -> larger size, and always under json.Marshal (no punctuation)
	small := map[string]interface{}{"p": "x"}
	big := map[string]interface{}{"p": "xxxxxxxxxxxxxxxxxxxx"}
	if RowBytes(big) <= RowBytes(small) {
		t.Error("larger payload should produce a larger size")
	}
	raw, _ := json.Marshal(big)
	if RowBytes(big) >= int64(len(raw)) {
		t.Error("cheap estimate should under-count json.Marshal (no punctuation)")
	}
}

func TestValueBytes(t *testing.T) {
	// ValueBytes is the per-value sizer SQL sources call per scanned column; it
	// must agree with the per-value contribution RowBytes uses internally.
	if got := ValueBytes(nil); got != 0 {
		t.Errorf("nil = %d, want 0", got)
	}
	if got := ValueBytes(true); got != 1 {
		t.Errorf("bool = %d, want 1", got)
	}
	if got := ValueBytes(int64(42)); got != 8 {
		t.Errorf("number = %d, want 8", got)
	}
	if got := ValueBytes("hello"); got != int64(len("hello")) {
		t.Errorf("string = %d, want %d", got, len("hello"))
	}
	if got := ValueBytes([]byte("abcd")); got != 4 {
		t.Errorf("bytes = %d, want 4", got)
	}
	// larger content -> larger size
	if ValueBytes("xxxxxxxxxx") <= ValueBytes("x") {
		t.Error("larger value should produce a larger size")
	}
	// RowBytes of a single-column row equals key length + ValueBytes(value)
	if got, want := RowBytes(map[string]interface{}{"k": "value"}), int64(len("k"))+ValueBytes("value"); got != want {
		t.Errorf("RowBytes/ValueBytes disagree: got %d, want %d", got, want)
	}
}
