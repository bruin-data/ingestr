package source

import (
	"context"
	"sync/atomic"
	"time"
)

type CDCChunkedSnapshotValidator interface {
	ValidateCDCChunkedSnapshot(context.Context, []SourceTableInfo) error
}

// CDCChunkTiming collects destination work until the chunk checkpoint is acknowledged.
type CDCChunkTiming struct {
	LoadNanos  atomic.Int64
	MergeNanos atomic.Int64
}

// CDCSnapshotChunk brackets complete snapshot rows, without interleaved WAL.
// Bounds follow the table's primary key order and describe (After, Last].
type CDCSnapshotChunk struct {
	After  []string
	Last   []string
	Timing *CDCChunkTiming
	// OrderedIntegerKeys certifies source key ordering is signed integer order.
	OrderedIntegerKeys bool
	// DeferMerge stages a serial chunk and leaves it unmerged. The consumer
	// must not acknowledge progress for a deferred chunk.
	DeferMerge bool
}

// CDCChunkedSnapshotOptions enables short, resumable PostgreSQL snapshot reads.
// The consumer must make each CDCFlush boundary durable before acknowledging it.
type CDCChunkedSnapshotOptions struct {
	Execution      string // Defaults to serial.
	ChunkSize      int
	ChunkTimeout   time.Duration
	WALStaging     string
	WALBufferBytes int64
	Continuous     bool
	RefreshID      string
	Store          CDCChunkStore
	// FlushRecords is the serial staging window. Zero merges and checkpoints
	// every chunk. Parallel execution ignores it.
	FlushRecords int64
}
