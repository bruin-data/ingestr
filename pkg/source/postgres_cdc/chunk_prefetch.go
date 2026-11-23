package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const snapshotChunkPageBytes = int64(16 << 20)

type chunkReadResult struct {
	chunk snapshotChunk
	spool *os.File
	err   error
}

type chunkPrefetch struct {
	started   time.Time
	after     []string
	completed chan chunkReadResult
	cancel    context.CancelFunc
}

func (r *chunkReader) startChunkRead(ctx context.Context, table source.SourceTableInfo, minimum pglogrepl.LSN) *chunkPrefetch {
	ctx, cancel := context.WithCancel(ctx)
	state := r.state.Tables[table.Name]
	prefetch := &chunkPrefetch{
		started: time.Now(), after: slices.Clone(state.After),
		completed: make(chan chunkReadResult, 1), cancel: cancel,
	}
	upper, slot := slices.Clone(state.Upper), r.state.Slot
	pageSize, maxBytes := snapshotChunkPageLimits(r.opts.PageSize, r.options.ChunkSize, r.opts.MaxBatchBytes)
	go func() {
		spool, err := os.CreateTemp("", "ingestr-snapshot-chunk-*.arrow")
		if err != nil {
			prefetch.completed <- chunkReadResult{err: fmt.Errorf("failed to create snapshot chunk spool: %w", err)}
			return
		}
		writer := ipc.NewWriter(spool, ipc.WithSchema(buildArrowSchema(table.Schema.Columns)))
		chunk, err := r.source.readSnapshotChunk(ctx, table, slot, prefetch.after, upper, r.options.ChunkSize, pageSize, maxBytes, r.options.ChunkTimeout, writer.Write)
		err = errors.Join(err, writer.Close())
		if err == nil && chunk.Boundary < minimum {
			err = fmt.Errorf("snapshot chunk boundary %s precedes applied WAL %s", chunk.Boundary, minimum)
		}
		if err == nil {
			_, err = spool.Seek(0, io.SeekStart)
		}
		if err != nil {
			err = errors.Join(err, closeChunkSpool(spool))
			spool = nil
		}
		prefetch.completed <- chunkReadResult{chunk: chunk, spool: spool, err: err}
	}()
	return prefetch
}

func snapshotChunkPageLimits(pageSize, chunkSize int, maxBytes int64) (int, int64) {
	if pageSize <= 0 {
		pageSize = chunkSize
	}
	if maxBytes <= 0 || maxBytes > snapshotChunkPageBytes {
		maxBytes = snapshotChunkPageBytes
	}
	return min(pageSize, chunkSize), maxBytes
}

func (p *chunkPrefetch) close() error {
	p.cancel()
	read := <-p.completed
	return closeChunkSpool(read.spool)
}

func closeChunkSpool(spool *os.File) error {
	if spool == nil {
		return nil
	}
	return errors.Join(spool.Close(), os.Remove(spool.Name()))
}

func (r *chunkReader) emitChunk(ctx context.Context, table source.SourceTableInfo, prefetch *chunkPrefetch, read chunkReadResult, timing *source.CDCChunkTiming, deferMerge bool) error {
	metadata := &source.CDCSnapshotChunk{After: prefetch.after, Last: read.chunk.LastKey, Timing: timing, OrderedIntegerKeys: chunkKeysOrderedIntegers(table), DeferMerge: deferMerge}
	if err := r.emit(ctx, source.RecordBatchResult{TableName: table.Name, CDCSnapshotChunk: metadata}); err != nil {
		return err
	}
	if err := readChunkSpool(ctx, read.spool, func(batch arrow.RecordBatch) error {
		batch.Retain()
		return r.emit(ctx, source.RecordBatchResult{TableName: table.Name, Batch: batch})
	}); err != nil {
		return err
	}
	return r.emit(ctx, source.RecordBatchResult{TableName: table.Name, CDCSnapshotChunkEnd: metadata})
}

func chunkKeysOrderedIntegers(table source.SourceTableInfo) bool {
	if table.Schema == nil || len(table.PrimaryKeys) == 0 {
		return false
	}
	for _, key := range table.PrimaryKeys {
		found := false
		for _, column := range table.Schema.Columns {
			if column.Name != key {
				continue
			}
			if column.Unsigned {
				return false
			}
			switch column.DataType {
			case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
				found = true
			default:
				return false
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func readChunkSpool(ctx context.Context, spool io.Reader, consume func(arrow.RecordBatch) error) error {
	reader, err := ipc.NewReader(spool, ipc.WithBodySizeLimit(0))
	if err != nil {
		return fmt.Errorf("failed to open snapshot chunk spool: %w", err)
	}
	defer reader.Release()
	for reader.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(reader.RecordBatch()); err != nil {
			return err
		}
	}
	if err := reader.Err(); err != nil {
		return fmt.Errorf("failed to read snapshot chunk spool: %w", err)
	}
	return nil
}
