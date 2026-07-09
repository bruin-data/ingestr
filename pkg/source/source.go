package source

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type ReadOptions struct {
	IncrementalKey                  string
	IncrementalKeyDataType          schema.DataType
	IntervalStart                   *time.Time
	IntervalEnd                     *time.Time
	ExtractPartitionBy              string
	ExtractPartitionInterval        time.Duration
	ExtractPartitionNumericInterval int64
	ExtractPartitionAuto            bool
	ExtractPartitionStart           *time.Time
	ExtractPartitionEnd             *time.Time
	ExtractPartitionNumericStart    *int64
	ExtractPartitionNumericEnd      *int64
	ExtractPartitionEndInclusive    bool
	ExtractPartitionIsNull          bool
	ExtractPartitionKind            ExtractPartitionKind
	ExtractPartitionDataType        schema.DataType
	RecordBatchBufferSize           int
	PageSize                        int
	Limit                           int
	ExcludeColumns                  []string
	Parallelism                     int
	Schema                          *schema.TableSchema // Optional: if provided, Read will skip GetSchema call
	CDCResumeLSN                    string              // Optional: for CDC sources, resume from this LSN (skip snapshot)
	CDCSlotSuffix                   string              // Optional: suffix for auto-generated replication slot names (dest-aware)
	CDCSnapshotReplace              bool                // Consumer can apply a full-snapshot replacement boundary
	FullRefresh                     bool
	Columns                         string // Optional: column definitions for schema-less sources (e.g., "id:bigint,name:text")
	Streaming                       bool   // Continuous mode: never exit on caught-up/idle; attach cumulative CommitTokens
	FlushInterval                   time.Duration
	FlushRecords                    int
}

func RecordBatchBufferSize(opts ReadOptions, defaultSize int) int {
	if opts.RecordBatchBufferSize > 0 {
		return opts.RecordBatchBufferSize
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

type RecordBatchResult struct {
	Batch     arrow.RecordBatch
	Err       error
	TableName string // Source table name for multi-table sources (empty for single-table)
	Truncate  bool   // Empty the destination table before applying subsequent results

	// CommitToken is an opaque, cumulative position marker for streaming mode.
	// Committing a token via StreamCommitter acknowledges everything emitted up
	// to and including the batch that carried it. Nil means no feedback needed.
	CommitToken any

	// TableInfo announces a table that appeared after the read started (e.g. a
	// table created on the source mid-stream). Consumers that prepared their
	// per-table state upfront use it to provision the destination before any
	// batches for the table arrive. Batch may be nil on an announcement.
	TableInfo *SourceTableInfo
}

// CDCStateCommitToken carries destination-managed CDC state alongside the
// source's native acknowledgement token. Position is the latest globally safe
// source position. SnapshotPositions marks source tables whose full snapshot is
// included in all preceding results.
type CDCStateCommitToken struct {
	SourceCommitToken any
	Position          string
	SnapshotPositions map[string]string
}

// CDCStateProvider exposes the state produced by a completed batch CDC read.
// The pipeline persists it only after the destination write succeeds.
type CDCStateProvider interface {
	CDCState() CDCStateCommitToken
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

type ExtractPartitioningProvider interface {
	SupportsExtractPartitioning() bool
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

// CDCBatchFinalizer is an optional capability for CDC sources that perform a
// final bookkeeping step after a successful batch run (e.g. confirming the
// replication slot's flush position). The pipeline calls FinalizeBatch only
// after the destination write is durable, so the source may safely confirm the
// position it streamed up to.
type CDCBatchFinalizer interface {
	FinalizeBatch(ctx context.Context) error
}

// LagReporter is an optional capability for streaming sources that can report
// how far the durable destination position trails the source's latest change.
// Implementations must answer from lock-free atomics: the metrics layer polls
// this on every scrape, concurrently with the replication loop.
type LagReporter interface {
	// ReplicationLag returns a point-in-time snapshot. ok is false when lag is
	// not yet meaningful (not streaming, or no server position observed).
	ReplicationLag() (LagSnapshot, bool)
}

// LagSnapshot is a self-consistent lag reading. BytesBehind and SecondsBehind
// are nil when the engine cannot express that dimension: Postgres exposes no
// per-LSN timestamp, and MongoDB/SQL Server logs have no comparable byte offset.
type LagSnapshot struct {
	Source          string
	BytesBehind     *uint64
	SecondsBehind   *float64
	ServerPosition  string
	DurablePosition string
	CaughtUp        bool
	UpdatedAt       time.Time
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
