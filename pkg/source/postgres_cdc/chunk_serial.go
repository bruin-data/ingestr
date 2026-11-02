package postgres_cdc

import (
	"context"

	"github.com/bruin-data/ingestr/pkg/source"
)

func (r *chunkReader) copySerialChunk(ctx context.Context, table source.SourceTableInfo) error {
	state := r.state.Tables[table.Name]
	maxBytes := r.opts.MaxBatchBytes
	if maxBytes <= 0 {
		maxBytes = r.options.WALBufferBytes
	}
	chunk, err := r.source.readSnapshotChunk(ctx, table, r.state.Slot, state.After, state.Upper, r.options.ChunkSize, maxBytes, r.options.ChunkTimeout)
	if err != nil {
		return err
	}
	if chunk.Batch != nil {
		if err := r.emit(ctx, source.RecordBatchResult{TableName: table.Name, Batch: chunk.Batch}); err != nil {
			return err
		}
	}
	state.Upper = chunk.UpperKey
	if chunk.LastKey != nil {
		state.After = chunk.LastKey
	}
	state.Done = chunk.Done
	// Copy progress is durable independently of CDC progress. The slot must
	// retain the original replay boundary until every table has been copied.
	return r.checkpoint(ctx, r.state.Applied, nil)
}
