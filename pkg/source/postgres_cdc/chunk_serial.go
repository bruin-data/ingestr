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
	rows := int64(0)
	if chunk.Batch != nil {
		rows = chunk.Batch.NumRows()
		if err := r.emit(ctx, source.RecordBatchResult{
			TableName: table.Name,
			Batch:     chunk.Batch,
			CDCUpload: r.opts.FlushRecords > 0,
		}); err != nil {
			return err
		}
	}
	state.Upper = chunk.UpperKey
	if chunk.LastKey != nil {
		state.After = chunk.LastKey
	}
	state.Done = chunk.Done
	r.serialWindowRows += rows
	if r.opts.FlushRecords > 0 && !state.Done && r.serialWindowRows < int64(r.opts.FlushRecords) {
		return nil
	}
	// Copy progress is durable independently of CDC progress. The slot must
	// retain the original replay boundary until every table has been copied.
	if err := r.checkpoint(ctx, r.state.Applied, nil); err != nil {
		return err
	}
	r.serialWindowRows = 0
	return nil
}
