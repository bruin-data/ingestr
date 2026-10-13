package bigquery

import (
	"testing"

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
