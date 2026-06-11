package db2

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMapDb2ToDataType(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  schema.DataType
		precision int
		scale     int
	}{
		{name: "smallint", input: "SMALLINT", wantType: schema.TypeInt16},
		{name: "integer", input: "INTEGER", wantType: schema.TypeInt32},
		{name: "bigint", input: "BIGINT", wantType: schema.TypeInt64},
		{name: "real", input: "REAL", wantType: schema.TypeFloat32},
		{name: "double", input: "DOUBLE PRECISION", wantType: schema.TypeFloat64},
		{name: "decimal", input: "DECIMAL(12, 4)", wantType: schema.TypeDecimal, precision: 12, scale: 4},
		{name: "varchar", input: "VARCHAR", wantType: schema.TypeString},
		{name: "bit data", input: "VARCHAR FOR BIT DATA", wantType: schema.TypeBinary},
		{name: "blob", input: "BLOB", wantType: schema.TypeBinary},
		{name: "date", input: "DATE", wantType: schema.TypeDate},
		{name: "time", input: "TIME", wantType: schema.TypeTime},
		{name: "timestamp", input: "TIMESTAMP", wantType: schema.TypeTimestamp},
		{name: "boolean", input: "BOOLEAN", wantType: schema.TypeBoolean},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotPrecision, gotScale, arrayType := MapDb2ToDataType(tt.input)
			require.Equal(t, tt.wantType, gotType)
			require.Equal(t, tt.precision, gotPrecision)
			require.Equal(t, tt.scale, gotScale)
			require.Equal(t, schema.TypeUnknown, arrayType)
		})
	}
}

func TestMapDb2SQLTypeToDataType(t *testing.T) {
	gotType, precision, scale, arrayType := MapDb2SQLTypeToDataType(db2SQLTypeNDecimal, 0, 11, 2)
	require.Equal(t, schema.TypeDecimal, gotType)
	require.Equal(t, 11, precision)
	require.Equal(t, 2, scale)
	require.Equal(t, schema.TypeUnknown, arrayType)

	gotType, _, _, _ = MapDb2SQLTypeToDataType(db2SQLTypeNVarchar, 20, 0, 0)
	require.Equal(t, schema.TypeString, gotType)
}
