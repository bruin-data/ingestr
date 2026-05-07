package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/source"
)

// RecordTransformer transforms Arrow record batches.
type RecordTransformer interface {
	// Transform processes a batch and returns a transformed batch.
	// The caller is responsible for releasing the returned batch.
	Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error)

	// OutputSchema returns the schema of transformed batches.
	OutputSchema(inputSchema *arrow.Schema) *arrow.Schema
}

// Wrap wraps a record channel with transformation.
func Wrap(records <-chan source.RecordBatchResult, t RecordTransformer) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)
	go func() {
		defer close(out)
		for result := range records {
			if result.Batch != nil && result.Err == nil {
				transformed, err := t.Transform(result.Batch)
				result.Batch.Release()
				if err != nil {
					result.Err = err
					result.Batch = nil
				} else {
					result.Batch = transformed
				}
			}
			out <- result
		}
	}()
	return out
}
