package bigquery

import (
	"testing"

	"cloud.google.com/go/bigquery"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestMapBigQueryNestedNumeric(t *testing.T) {
	dt, p, s, at := MapBigQueryToDataType("ARRAY<NUMERIC(5,2)>")
	assert.Equal(t, schema.TypeArray, dt)
	assert.Equal(t, schema.TypeDecimal, at)
	assert.Equal(t, 5, p)
	assert.Equal(t, 2, s)

	dt, _, _, _ = MapBigQueryToDataType("STRUCT<a NUMERIC(5,2)>")
	assert.Equal(t, schema.TypeJSON, dt)
}

func TestMapBigQueryRepeatedField(t *testing.T) {
	dt, p, s, at := mapBigQueryFieldToDataType(&bigquery.FieldSchema{Type: bigquery.NumericFieldType, Precision: 10, Scale: 2, Repeated: true})
	assert.Equal(t, schema.TypeArray, dt)
	assert.Equal(t, schema.TypeDecimal, at)
	assert.Equal(t, 10, p)
	assert.Equal(t, 2, s)

	dt, _, _, _ = mapBigQueryFieldToDataType(&bigquery.FieldSchema{Type: bigquery.RecordFieldType, Repeated: true})
	assert.Equal(t, schema.TypeJSON, dt)
}

func TestMapBigQueryUnparameterizedNumeric(t *testing.T) {
	_, p, s, _ := mapBigQueryFieldToDataType(&bigquery.FieldSchema{Type: bigquery.NumericFieldType})
	assert.Equal(t, 38, p)
	assert.Equal(t, 9, s)
}

func TestMapBigQueryArrowTypeNames(t *testing.T) {
	tests := []struct {
		arrowType string
		want      schema.DataType
		precision int
		scale     int
	}{
		{"bool", schema.TypeBoolean, 0, 0},
		{"int64", schema.TypeInt64, 0, 0},
		{"float64", schema.TypeFloat64, 0, 0},
		{"decimal(38, 9)", schema.TypeDecimal, 38, 9},
		{"decimal256(76, 38)", schema.TypeDecimal, 76, 38},
		{"utf8", schema.TypeString, 0, 0},
		{"binary", schema.TypeBinary, 0, 0},
		{"date32", schema.TypeDate, 0, 0},
		{"time64[us]", schema.TypeTime, 0, 0},
		{"timestamp[us]", schema.TypeTimestamp, 0, 0},
		{"timestamp[us, tz=UTC]", schema.TypeTimestampTZ, 0, 0},
	}
	for _, tt := range tests {
		dt, p, s, _ := MapBigQueryToDataType(tt.arrowType)
		assert.Equal(t, tt.want, dt, tt.arrowType)
		assert.Equal(t, tt.precision, p, tt.arrowType)
		assert.Equal(t, tt.scale, s, tt.arrowType)
	}
}
