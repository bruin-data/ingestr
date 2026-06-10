package clickhouse

import (
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
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
			require.Equal(t, 0, gotPrecision)
			require.Equal(t, 0, gotScale)
			require.Equal(t, tt.wantArray, gotArray)
		})
	}
}

func TestNativeScanTarget_ArrayWrappers(t *testing.T) {
	t.Parallel()

	tests := []string{
		"Array(LowCardinality(String))",
		"Array(Nullable(String))",
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
