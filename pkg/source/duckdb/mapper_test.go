package duckdb

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestMapDuckDBArrayOfDecimal(t *testing.T) {
	for _, typ := range []string{"DECIMAL(5,2)[]", "DECIMAL(5,2) LIST"} {
		dt, p, s, at := MapDuckDBToDataType(typ)
		assert.Equal(t, schema.TypeArray, dt, typ)
		assert.Equal(t, schema.TypeDecimal, at, typ)
		assert.Equal(t, 5, p, typ)
		assert.Equal(t, 2, s, typ)
	}
}
