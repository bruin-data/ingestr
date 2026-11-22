package postgres_cdc

import (
	"context"
	"errors"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

func (r *chunkReader) copySerialChunk(ctx context.Context, table source.SourceTableInfo) (retErr error) {
	state := r.state.Tables[table.Name]
	prefetch := r.prefetch
	if prefetch == nil {
		prefetch = r.startChunkRead(ctx, table, r.state.Applied)
	}
	r.prefetch = nil
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
	chunk := read.chunk
	state.Upper = chunk.UpperKey
	if chunk.LastKey != nil {
		state.After = chunk.LastKey
	}
	state.Done = chunk.Done
	if r.options.FlushRecords > 0 {
		r.serialWindowRows += chunk.Rows
	}
	// The next read overlaps this upload. It starts after a merge, because a
	// merge can outlast the snapshot.
	deferMerge := r.options.FlushRecords > 0 && !state.Done && r.serialWindowRows < r.options.FlushRecords
	if deferMerge {
		r.prefetch = r.startChunkRead(ctx, table, r.state.Applied)
	}
	timing := &source.CDCChunkTiming{}
	if err := r.emitChunk(ctx, table, prefetch, read, timing, deferMerge); err != nil {
		return err
	}
	if deferMerge {
		logChunkTiming(table.Name, prefetch, chunk, timing, 0, 0, 0)
		return nil
	}
	// Copy progress is durable independently of CDC progress. The slot must
	// retain the original replay boundary until every table has been copied.
	checkpointStart := time.Now()
	if err := r.checkpoint(ctx, r.state.Applied, nil, timing); err != nil {
		return err
	}
	r.serialWindowRows = 0
	logChunkTiming(table.Name, prefetch, chunk, timing, 0, time.Since(checkpointStart), 0)
	return nil
}
