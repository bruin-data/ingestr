package arrowconv

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
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

// Regression: Decimal128Builder was the only numeric builder in this file
// missing a json.Number branch. Schema-inferred sources (JSONL, MongoDB)
// decode JSON numbers as json.Number via UseNumber(); any DECIMAL/NUMERIC
// destination column silently received NULL through that path. The
// delegation to the existing string branch must also inherit the
// big.Float fallback for scientific notation.
func TestAppendValue_Decimal128_JSONNumber(t *testing.T) {
	dt := &arrow.Decimal128Type{Precision: 38, Scale: 0}

	tests := []struct {
		name     string
		val      json.Number
		wantNull bool
		wantBigI string // decimal representation expected via BigInt()
	}{
		{name: "simple integer", val: json.Number("1"), wantBigI: "1"},
		{name: "large positive", val: json.Number("42"), wantBigI: "42"},
		{name: "negative", val: json.Number("-7"), wantBigI: "-7"},
		{name: "scientific notation (uses big.Float fallback)", val: json.Number("1.5e10"), wantBigI: "15000000000"},
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
