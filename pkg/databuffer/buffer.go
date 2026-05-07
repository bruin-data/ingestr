// Package databuffer provides data buffering for schema inference.
// When a source has an unknown schema, data must be read and buffered
// before the schema can be inferred, and then replayed for writing.
package databuffer

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/source"
)

// DataBuffer accumulates record batches and allows replay.
// It is used when schema inference requires reading all data before
// creating the destination table.
type DataBuffer interface {
	// Append adds a record batch to the buffer.
	// The batch is retained and must not be modified after this call.
	Append(ctx context.Context, batch arrow.RecordBatch) error

	// Reader returns a channel that replays all buffered batches,
	// cast to the provided target schema.
	// The channel will be closed after all batches have been sent.
	// Each batch should be released by the consumer after use.
	Reader(ctx context.Context, targetSchema *arrow.Schema) (<-chan source.RecordBatchResult, error)

	// Close releases buffer resources.
	// After Close, the buffer should not be used.
	Close() error

	// Stats returns buffer statistics.
	Stats() BufferStats
}

// BufferStats contains statistics about the buffer.
type BufferStats struct {
	BatchCount int64
	RowCount   int64
	BytesUsed  int64
}
