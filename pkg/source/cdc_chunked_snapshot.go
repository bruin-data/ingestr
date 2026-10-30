package source

import (
	"context"
	"time"
)

type CDCChunkedSnapshotValidator interface {
	ValidateCDCChunkedSnapshot(context.Context, []SourceTableInfo) error
}

// CDCChunkedSnapshotOptions enables short, resumable PostgreSQL snapshot reads.
// The consumer must make each CDCFlush boundary durable before acknowledging it.
type CDCChunkedSnapshotOptions struct {
	ChunkSize      int
	ChunkTimeout   time.Duration
	WALStaging     string
	WALBufferBytes int64
	Continuous     bool
	Store          CDCChunkStore
}
