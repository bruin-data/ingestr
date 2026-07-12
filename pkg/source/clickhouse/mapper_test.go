package clickhouse

import (
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestMapClickHouseToDataType_ArrayWrappers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantType  schema.DataType
		wantArray schema.DataType
	}{
		{
			name:      "array low cardinality string",
			input:     "Array(LowCardinality(String))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeString,
		},
		{
			name:      "array nullable string",
			input:     "Array(Nullable(String))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeString,
		},
		{
			name:      "nullable array low cardinality string",
			input:     "Nullable(Array(LowCardinality(String)))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeString,
		},
		{
			name:      "array datetime",
			input:     "Array(DateTime)",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeTimestamp,
		},
		{
			name:      "array datetime64 timezone",
			input:     "Array(DateTime64(3, 'Europe/Moscow'))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeTimestamp,
		},
		{
			name:      "array datetime timezone",
			input:     "Array(DateTime('UTC'))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeTimestamp,
		},
		{
			name:      "array decimal",
			input:     "Array(Decimal(18,5))",
			wantType:  schema.TypeArray,
			wantArray: schema.TypeDecimal,
		},
		{
			name:      "low cardinality nullable string",
			input:     "LowCardinality(Nullable(String))",
			wantType:  schema.TypeString,
			wantArray: schema.TypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotType, gotPrecision, gotScale, gotArray := MapClickHouseToDataType(tt.input)

			require.Equal(t, tt.wantType, gotType)
			if tt.input == "Array(Decimal(18,5))" {
				require.Equal(t, 18, gotPrecision)
				require.Equal(t, 5, gotScale)
			} else {
				require.Equal(t, 0, gotPrecision)
				require.Equal(t, 0, gotScale)
			}
			require.Equal(t, tt.wantArray, gotArray)
		})
	}
}

func TestMapClickHouseDecimal256ArrayFailsSchemaValidation(t *testing.T) {
	dataType, precision, scale, arrayType := MapClickHouseToDataType("Array(Decimal256(76,38))")
	require.Equal(t, schema.TypeArray, dataType)
	require.Equal(t, schema.TypeDecimal, arrayType)
	require.Equal(t, 76, precision)
	require.Equal(t, 38, scale)

	err := schemainfer.ValidateSchema(&schema.TableSchema{Columns: []schema.Column{{
		Name: "amounts", DataType: dataType, ArrayType: arrayType, Precision: precision, Scale: scale,
	}}})
	require.ErrorContains(t, err, "amounts.element")
	require.ErrorContains(t, err, "decimal precision 76")
}

func TestNativeScanTarget_ArrayWrappers(t *testing.T) {
	t.Parallel()

	tests := []string{
		"Array(LowCardinality(String))",
		"Nullable(Array(LowCardinality(String)))",
	}

	wantType := reflect.TypeOf(new([]string))

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, wantType, reflect.TypeOf(nativeScanTarget(input, false)))
		})
	}
}

func TestNativeScanTarget_ArrayScalarTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		wantType reflect.Type
	}{
		{
			input:    "Array(Bool)",
			wantType: reflect.TypeOf(new([]bool)),
		},
		{
			input:    "Array(Nullable(Bool))",
			wantType: reflect.TypeOf(new([]*bool)),
		},
		{
			input:    "Array(Int8)",
			wantType: reflect.TypeOf(new([]int8)),
		},
		{
			input:    "Array(Nullable(Int16))",
			wantType: reflect.TypeOf(new([]*int16)),
		},
		{
			input:    "Array(UInt8)",
			wantType: reflect.TypeOf(new([]uint8)),
		},
		{
			input:    "Array(Nullable(UInt16))",
			wantType: reflect.TypeOf(new([]*uint16)),
		},
		{
			input:    "Array(UInt64)",
			wantType: reflect.TypeOf(new([]uint64)),
		},
		{
			input:    "Array(Int128)",
			wantType: reflect.TypeOf(new([]*big.Int)),
		},
		{
			input:    "Array(Nullable(UInt256))",
			wantType: reflect.TypeOf(new([]*big.Int)),
		},
		{
			input:    "Array(Float32)",
			wantType: reflect.TypeOf(new([]float32)),
		},
		{
			input:    "Array(Nullable(Float64))",
			wantType: reflect.TypeOf(new([]*float64)),
		},
		{
			input:    "Array(String)",
			wantType: reflect.TypeOf(new([]string)),
		},
		{
			input:    "Array(Nullable(String))",
			wantType: reflect.TypeOf(new([]*string)),
		},
		{
			input:    "Array(FixedString(10))",
			wantType: reflect.TypeOf(new([]string)),
		},
		{
			input:    "Array(Nullable(Enum8('click' = 1, 'house' = 2)))",
			wantType: reflect.TypeOf(new([]*string)),
		},
		{
			input:    "Array(Decimal(18,5))",
			wantType: reflect.TypeOf(new([]decimal.Decimal)),
		},
		{
			input:    "Array(Nullable(Decimal(18,5)))",
			wantType: reflect.TypeOf(new([]*decimal.Decimal)),
		},
		{
			input:    "Array(UUID)",
			wantType: reflect.TypeOf(new([]uuid.UUID)),
		},
		{
			input:    "Array(Nullable(UUID))",
			wantType: reflect.TypeOf(new([]*uuid.UUID)),
		},
		{
			input:    "Array(IPv4)",
			wantType: reflect.TypeOf(new([]net.IP)),
		},
		{
			input:    "Array(Nullable(IPv6))",
			wantType: reflect.TypeOf(new([]*net.IP)),
		},
		{
			input:    "Array(Date)",
			wantType: reflect.TypeOf(new([]time.Time)),
		},
		{
			input:    "Array(DateTime)",
			wantType: reflect.TypeOf(new([]time.Time)),
		},
		{
			input:    "Array(DateTime64(3, 'Europe/Moscow'))",
			wantType: reflect.TypeOf(new([]time.Time)),
		},
		{
			input:    "Array(Nullable(DateTime))",
			wantType: reflect.TypeOf(new([]*time.Time)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.wantType, reflect.TypeOf(nativeScanTarget(tt.input, false)))
		})
	}
}

func TestExtractValue_ArrayUint8(t *testing.T) {
	t.Parallel()

	values := []uint8{1, 2, 255}
	got := extractValue(&values)

	require.Equal(t, values, got)
}
