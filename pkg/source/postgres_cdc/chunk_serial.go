package postgres_cdc

import (
	"context"
	"errors"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

func (r *chunkReader) copySerialChunk(ctx context.Context, table source.SourceTableInfo) (retErr error) {
	state := r.state.Tables[table.Name]
	prefetch := r.startChunkRead(ctx, table, r.state.Applied)
	var read chunkReadResult
	select {
	case read = <-prefetch.completed:
	case <-ctx.Done():
		return errors.Join(ctx.Err(), prefetch.close())
	}
	defer prefetch.cancel()
	defer func() { retErr = errors.Join(retErr, closeChunkSpool(read.spool)) }()
	if read.err != nil {
		return read.err
	}
	timing := &source.CDCChunkTiming{}
	if err := r.emitChunk(ctx, table, prefetch, read, timing); err != nil {
		return err
	}
	chunk := read.chunk
	state.Upper = chunk.UpperKey
	if chunk.LastKey != nil {
		state.After = chunk.LastKey
	}
	state.Done = chunk.Done
	// Copy progress is durable independently of CDC progress. The slot must
	// retain the original replay boundary until every table has been copied.
	checkpointStart := time.Now()
	if err := r.checkpoint(ctx, r.state.Applied, nil, timing); err != nil {
		return err
	}
	logChunkTiming(table.Name, prefetch, chunk, timing, 0, time.Since(checkpointStart))
	return nil
}
