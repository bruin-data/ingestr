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

func TestMapBigQueryArrowListTypes(t *testing.T) {
	dt, p, s, at := MapBigQueryToDataType("list<item: decimal(38, 9), nullable>")
	assert.Equal(t, schema.TypeArray, dt)
	assert.Equal(t, schema.TypeDecimal, at)
	assert.Equal(t, 38, p)
	assert.Equal(t, 9, s)

	dt, _, _, at = MapBigQueryToDataType("list<item: int64, nullable>")
	assert.Equal(t, schema.TypeArray, dt)
	assert.Equal(t, schema.TypeInt64, at)
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
