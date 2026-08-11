package transformer

import (
	"runtime"

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

// WrapParallel is Wrap with the Transform calls spread across a bounded worker
// pool. Output order is preserved. The transformer must be safe for concurrent
// Transform calls on distinct batches; schema retargeting announcements act as
// a barrier (all in-flight batches finish first), so RetargetSchema never runs
// concurrently with Transform.
func WrapParallel(records <-chan source.RecordBatchResult, t RecordTransformer, workers int) <-chan source.RecordBatchResult {
	if workers <= 1 {
		return Wrap(records, t)
	}

	out := make(chan source.RecordBatchResult)
	pending := make(chan chan source.RecordBatchResult, workers)
	sem := make(chan struct{}, workers)

	transform := func(result source.RecordBatchResult) source.RecordBatchResult {
		transformed, err := t.Transform(result.Batch)
		result.Batch.Release()
		if err != nil {
			result.Err = err
			result.Batch = nil
		} else {
			result.Batch = transformed
		}
		return result
	}

	go func() {
		defer close(pending)
		for result := range records {
			slot := make(chan source.RecordBatchResult, 1)

			isAnnouncement := result.Err == nil && result.TableInfo != nil && result.TableInfo.Schema != nil
			switch {
			case isAnnouncement:
				for range workers {
					sem <- struct{}{}
				}
				if r, ok := t.(SchemaRetargeter); ok {
					r.RetargetSchema(result.TableInfo.Schema)
				}
				if result.Batch != nil {
					result = transform(result)
				}
				slot <- result
				for range workers {
					<-sem
				}
			case result.Batch != nil && result.Err == nil:
				sem <- struct{}{}
				go func(result source.RecordBatchResult) {
					defer func() { <-sem }()
					slot <- transform(result)
				}(result)
			default:
				slot <- result
			}

			pending <- slot
		}
	}()

	go func() {
		defer close(out)
		for slot := range pending {
			out <- <-slot
		}
	}()

	return out
}

// ParallelWorkers returns the worker count for WrapParallel, bounded to keep
// per-batch memory amplification in check.
func ParallelWorkers() int {
	return min(runtime.GOMAXPROCS(0), 8)
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
