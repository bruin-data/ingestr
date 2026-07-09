package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
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

// SchemaRetargeter is implemented by transformers pinned to a schema that must
// follow mid-stream schema announcements (RecordBatchResult.TableInfo) from
// sources that rebuild their stream around DDL.
type SchemaRetargeter interface {
	RetargetSchema(s *schema.TableSchema)
}

// Wrap wraps a record channel with transformation.
func Wrap(records <-chan source.RecordBatchResult, t RecordTransformer) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)
	go func() {
		defer close(out)
		for result := range records {
			// A schema announcement precedes the batches decoded in the new
			// shape; retarget schema-pinned transformers before those arrive.
			if result.Err == nil && result.TableInfo != nil && result.TableInfo.Schema != nil {
				if r, ok := t.(SchemaRetargeter); ok {
					r.RetargetSchema(result.TableInfo.Schema)
				}
			}
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
