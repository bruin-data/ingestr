package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/databuffer"
)

// TypeCaster casts record batch columns to match a target Arrow schema.
// Used when --columns specifies type overrides for known-schema sources.
type TypeCaster struct {
	targetSchema *arrow.Schema
	safe         bool
}

func NewTypeCaster(targetSchema *arrow.Schema) *TypeCaster {
	return &TypeCaster{targetSchema: targetSchema}
}

func NewSafeTypeCaster(targetSchema *arrow.Schema) *TypeCaster {
	return &TypeCaster{targetSchema: targetSchema, safe: true}
}

func (tc *TypeCaster) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if batch.Schema().Equal(tc.targetSchema) {
		batch.Retain()
		return batch, nil
	}
	return databuffer.CastRecordToSchema(batch, tc.targetSchema, tc.safe)
}

func (tc *TypeCaster) OutputSchema(_ *arrow.Schema) *arrow.Schema {
	return tc.targetSchema
}
