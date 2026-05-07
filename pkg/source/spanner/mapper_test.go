package spanner

import (
	"testing"

	sppb "cloud.google.com/go/spanner/apiv1/spannerpb"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMapSpannerCodeToDataType_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		input     *sppb.Type
		wantType  schema.DataType
		wantPrec  int
		wantScale int
		wantArray schema.DataType
	}{
		{"nil", nil, schema.TypeString, 0, 0, schema.TypeUnknown},
		{"BOOL", &sppb.Type{Code: sppb.TypeCode_BOOL}, schema.TypeBoolean, 0, 0, schema.TypeUnknown},
		{"INT64", &sppb.Type{Code: sppb.TypeCode_INT64}, schema.TypeInt64, 0, 0, schema.TypeUnknown},
		{"FLOAT32", &sppb.Type{Code: sppb.TypeCode_FLOAT32}, schema.TypeFloat32, 0, 0, schema.TypeUnknown},
		{"FLOAT64", &sppb.Type{Code: sppb.TypeCode_FLOAT64}, schema.TypeFloat64, 0, 0, schema.TypeUnknown},
		{"NUMERIC", &sppb.Type{Code: sppb.TypeCode_NUMERIC}, schema.TypeDecimal, 38, 9, schema.TypeUnknown},
		{"STRING", &sppb.Type{Code: sppb.TypeCode_STRING}, schema.TypeString, 0, 0, schema.TypeUnknown},
		{"BYTES", &sppb.Type{Code: sppb.TypeCode_BYTES}, schema.TypeBinary, 0, 0, schema.TypeUnknown},
		{"DATE", &sppb.Type{Code: sppb.TypeCode_DATE}, schema.TypeDate, 0, 0, schema.TypeUnknown},
		{"TIMESTAMP", &sppb.Type{Code: sppb.TypeCode_TIMESTAMP}, schema.TypeTimestampTZ, 0, 0, schema.TypeUnknown},
		{"JSON", &sppb.Type{Code: sppb.TypeCode_JSON}, schema.TypeJSON, 0, 0, schema.TypeUnknown},
		{"ARRAY<STRING>", &sppb.Type{Code: sppb.TypeCode_ARRAY, ArrayElementType: &sppb.Type{Code: sppb.TypeCode_STRING}}, schema.TypeArray, 0, 0, schema.TypeString},
		{"ARRAY<INT64>", &sppb.Type{Code: sppb.TypeCode_ARRAY, ArrayElementType: &sppb.Type{Code: sppb.TypeCode_INT64}}, schema.TypeArray, 0, 0, schema.TypeInt64},
		{"ARRAY<FLOAT64>", &sppb.Type{Code: sppb.TypeCode_ARRAY, ArrayElementType: &sppb.Type{Code: sppb.TypeCode_FLOAT64}}, schema.TypeArray, 0, 0, schema.TypeFloat64},
		{"ARRAY<BOOL>", &sppb.Type{Code: sppb.TypeCode_ARRAY, ArrayElementType: &sppb.Type{Code: sppb.TypeCode_BOOL}}, schema.TypeArray, 0, 0, schema.TypeBoolean},
		{"ARRAY nil elem", &sppb.Type{Code: sppb.TypeCode_ARRAY}, schema.TypeArray, 0, 0, schema.TypeString},
		{"unknown code", &sppb.Type{Code: sppb.TypeCode_TYPE_CODE_UNSPECIFIED}, schema.TypeString, 0, 0, schema.TypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, p, s, at := MapSpannerCodeToDataType(tt.input)
			require.Equal(t, tt.wantType, dt, "DataType mismatch")
			require.Equal(t, tt.wantPrec, p, "Precision mismatch")
			require.Equal(t, tt.wantScale, s, "Scale mismatch")
			require.Equal(t, tt.wantArray, at, "ArrayType mismatch")
		})
	}
}
