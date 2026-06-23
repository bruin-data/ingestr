package source

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type ReadOptions struct {
	IncrementalKey string
	IntervalStart  *time.Time
	IntervalEnd    *time.Time
	PageSize       int
	Limit          int
	ExcludeColumns []string
	Parallelism    int
	Schema         *schema.TableSchema // Optional: if provided, Read will skip GetSchema call
	CDCResumeLSN   string              // Optional: for CDC sources, resume from this LSN (skip snapshot)
	CDCSlotSuffix  string              // Optional: suffix for auto-generated replication slot names (dest-aware)
	FullRefresh    bool
	Columns        string // Optional: column definitions for schema-less sources (e.g., "id:bigint,name:text")
	Streaming      bool   // Continuous mode: never exit on caught-up/idle; attach cumulative CommitTokens
	FlushInterval  time.Duration
	FlushRecords   int
}

type RecordBatchResult struct {
	Batch     arrow.RecordBatch
	Err       error
	TableName string // Source table name for multi-table sources (empty for single-table)

	// CommitToken is an opaque, cumulative position marker for streaming mode.
	// Committing a token via StreamCommitter acknowledges everything emitted up
	// to and including the batch that carried it. Nil means no feedback needed.
	CommitToken any
}

// TableRequest contains user-provided configuration for table instantiation.
// The source uses this to resolve final values for the SourceTable.
type TableRequest struct {
	Name           string                     // Required: table name (e.g., "orders" or "public.users")
	IncrementalKey string                     // User-specified incremental key (validated by source)
	PrimaryKeys    []string                   // User-specified PKs (used only if source doesn't define them)
	Strategy       config.IncrementalStrategy // User-specified strategy (used only if source doesn't define it)
	Streaming      bool                       // Table will be read in streaming mode (--stream)
	StrategySet    bool                       // Whether strategy was explicitly supplied by the caller
	FullRefresh    bool                       // Whether the run ignores incremental state and fully refreshes the destination
}

// Source represents a data source that can provide tables.
// Sources handle connection management and return SourceTable instances.
type Source interface {
	Schemes() []string
	Connect(ctx context.Context, uri string) error
	Close(ctx context.Context) error
	GetTable(ctx context.Context, req TableRequest) (SourceTable, error)
	HandlesIncrementality() bool
}

// SourceTable represents a specific table within a source.
// It contains resolved configuration (PKs, strategy, incremental key) and provides
// schema and data reading capabilities.
type SourceTable interface {
	Name() string
	PrimaryKeys() []string
	IncrementalKey() string
	Strategy() config.IncrementalStrategy
	HasKnownSchema() bool
	GetSchema(ctx context.Context) (*schema.TableSchema, error)
	Read(ctx context.Context, opts ReadOptions) (<-chan RecordBatchResult, error)
}

type PrimaryKeyUniquenessProvider interface {
	PrimaryKeysUnique() bool
}

// PartitionedTable is an optional interface that a SourceTable can implement
// to declare a partition key for the destination table.
type PartitionedTable interface {
	PartitionBy() string
}

// SourceTableInfo contains metadata about a table in a multi-table source.
type SourceTableInfo struct {
	Name        string
	Schema      *schema.TableSchema
	PrimaryKeys []string
	DestSchema  string
}

// MultiTableReadOptions extends ReadOptions for multi-table reads.
type MultiTableReadOptions struct {
	ReadOptions
	Tables        []string          // Filter to specific tables (empty = all tables)
	CDCResumeLSNs map[string]string // Per-table CDC resume LSNs: table name → max LSN already processed
}

// StreamingSource is an optional capability for sources that support
// continuous ingestion via the --stream flag.
type StreamingSource interface {
	SupportsStreaming() bool

	// DefaultStreamingStrategy is the write strategy used in streaming mode
	// when the user doesn't specify one (merge for CDC, append for brokers).
	DefaultStreamingStrategy() config.IncrementalStrategy
}

// StreamCommitter is an optional capability for streaming sources that need
// durability feedback. After each successful flush the pipeline calls
// CommitStream with the CommitToken of the last flushed batch; tokens are
// cumulative, so committing a token acknowledges everything emitted up to it.
type StreamCommitter interface {
	CommitStream(ctx context.Context, token any) error
}

// MultiTableSource represents a source that emits data from multiple tables.
// This is used for CDC sources that capture changes across multiple tables.
type MultiTableSource interface {
	Source

	// IsMultiTable returns true if this source emits multiple tables.
	IsMultiTable() bool

	// GetTables returns all tables this source will emit, with their schemas.
	GetTables(ctx context.Context) ([]SourceTableInfo, error)

	// ReadAll starts reading from all tables concurrently.
	// Returns a single channel with batches from all tables, tagged with TableName.
	ReadAll(ctx context.Context, opts MultiTableReadOptions) (<-chan RecordBatchResult, error)
}
