package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// ChainedTransformer composes multiple transformers into one.
type ChainedTransformer struct {
	transformers []RecordTransformer
}

// Chain creates a new ChainedTransformer from the given transformers.
func Chain(transformers ...RecordTransformer) RecordTransformer {
	return &ChainedTransformer{
		transformers: transformers,
	}
}

// Transform applies all transformers in sequence.
func (c *ChainedTransformer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	current := batch
	current.Retain()

	for _, t := range c.transformers {
		transformed, err := t.Transform(current)
		current.Release()
		if err != nil {
			return nil, err
		}
		current = transformed
	}

	return current, nil
}

// OutputSchema returns the final schema after all transformations.
func (c *ChainedTransformer) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	schema := inputSchema
	for _, t := range c.transformers {
		schema = t.OutputSchema(schema)
	}
	return schema
}
