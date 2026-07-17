package source

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
)

var ErrConnectorLeaseLost = errors.New("connector lease lost")

type connectorLeaseGuardKey struct{}

// ConnectorLeaseGuard keeps connector ownership observable independently of
// context cancellation. This matters during graceful shutdown, where the run
// context is already cancelled but a detached final flush is still allowed.
type ConnectorLeaseGuard struct {
	lease ConnectorLease
}

func NewConnectorLeaseGuard(lease ConnectorLease) *ConnectorLeaseGuard {
	return &ConnectorLeaseGuard{lease: lease}
}

func (g *ConnectorLeaseGuard) Done() <-chan struct{} {
	if g == nil || g.lease == nil {
		return nil
	}
	return g.lease.Done()
}

func (g *ConnectorLeaseGuard) Err() error {
	if g == nil || g.lease == nil {
		return nil
	}
	select {
	case <-g.lease.Done():
		cause := g.lease.Err()
		if cause == nil {
			cause = errors.New("connector lease was lost")
		}
		return errors.Join(ErrConnectorLeaseLost, cause)
	default:
		return nil
	}
}

func WithConnectorLeaseGuard(ctx context.Context, guard *ConnectorLeaseGuard) context.Context {
	return context.WithValue(ctx, connectorLeaseGuardKey{}, guard)
}

func ConnectorLeaseGuardFromContext(ctx context.Context) *ConnectorLeaseGuard {
	guard, _ := ctx.Value(connectorLeaseGuardKey{}).(*ConnectorLeaseGuard)
	return guard
}

func ConnectorLeaseLoss(ctx context.Context) error {
	if guard := ConnectorLeaseGuardFromContext(ctx); guard != nil {
		if err := guard.Err(); err != nil {
			return err
		}
	}
	if cause := context.Cause(ctx); errors.Is(cause, ErrConnectorLeaseLost) {
		return cause
	}
	return nil
}

// WithoutCancelWithConnectorLease detaches ordinary cancellation while
// retaining values and connector fencing. The returned context is cancelled
// immediately if ownership is lost.
func WithoutCancelWithConnectorLease(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	guard := ConnectorLeaseGuardFromContext(ctx)
	if guard == nil {
		return context.WithCancel(detached)
	}
	guarded, cancel := context.WithCancelCause(detached)
	go func() {
		select {
		case <-guard.Done():
			cancel(guard.Err())
		case <-guarded.Done():
		}
	}()
	return guarded, func() {
		cancel(context.Canceled)
	}
}

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
	CDCResumeIncarnation            string              // Source table incarnation that authorized CDCResumeLSN
	CDCResumeSchemaFingerprint      string              // Source schema fingerprint that authorized CDCResumeLSN
	CDCSlotSuffix                   string              // Optional: suffix for auto-generated replication slot names (dest-aware)
	CDCLegacySlotSuffix             string              // Optional: prior auto-slot suffix used only for upgrade resume
	CDCSnapshotReplace              bool                // Consumer can apply a full-snapshot replacement boundary
	CDCStableDataBatches            bool                // Consumer requires source-stable CDC data-write identities
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

// DurableID is an exact, replay-stable payload or checkpoint identity. The
// empty value means that the result has no durable identity.
type DurableID string

type RecordBatchResult struct {
	Batch     arrow.RecordBatch
	Err       error
	TableName string // Source table name for multi-table sources (empty for single-table)
	Truncate  bool   // Empty the destination table before applying subsequent results
	// CDCWALTruncate distinguishes a replicated source TRUNCATE from the
	// destination reset emitted before a replacement snapshot.
	CDCWALTruncate bool

	// CommitToken is an opaque, cumulative position marker for streaming mode.
	// Committing a token via StreamCommitter acknowledges everything emitted up
	// to and including the batch that carried it. Nil means no feedback needed.
	CommitToken any

	// DurableCommitID identifies the exact data payload carried by this result.
	// It must be stable when that payload is replayed across processes and
	// reconnects. Cumulative watermarks and connection-local acknowledgement
	// handles must leave it empty.
	DurableCommitID DurableID

	// DurableCommitPosition is the resumable source position made durable by
	// DurableCommitID. It is separate because a payload identity may include a
	// kind/table prefix that is not a valid source cursor.
	DurableCommitPosition string

	// DurableCheckpointID and DurableCheckpointPosition identify a global
	// source low-watermark that is safe to persist for tables without rows in
	// the current flush. They may differ from this batch's payload identity.
	// DurableCheckpointTable limits the checkpoint to one source table; an
	// empty value means the checkpoint is safe for every table in the stream.
	DurableCheckpointID       DurableID
	DurableCheckpointPosition string
	DurableCheckpointTable    string

	// SnapshotReset marks the start of a fresh snapshot attempt for TableName.
	// Consumers must empty the destination table before accepting subsequent
	// snapshot rows so an interrupted older attempt cannot contaminate it.
	SnapshotReset bool

	// TableInfo announces a table that appeared after the read started (e.g. a
	// table created on the source mid-stream). Consumers that prepared their
	// per-table state upfront use it to provision the destination before any
	// batches for the table arrive. Batch may be nil on an announcement.
	TableInfo *SourceTableInfo

	// SnapshotInvalidation announces that an existing destination table is
	// about to be replaced by a new snapshot. Consumers must durably invalidate
	// the prior snapshot marker before processing subsequent results.
	SnapshotInvalidation *CDCSnapshotInvalidation
}

type CDCSnapshotInvalidation struct {
	TableName         string
	Incarnation       string
	SchemaFingerprint string
}

// CDCStateCommitToken carries destination-managed CDC state alongside the
// source's native acknowledgement token. Position is the latest globally safe
// source position. DataBatchID is an optional source-stable identity for the
// exact data batch carried by the result. SnapshotPositions marks source tables
// whose full snapshot is included in all preceding results; the matching
// incarnation and schema maps bind those snapshots to the exact source table
// shape.
type CDCStateCommitToken struct {
	SourceCommitToken    any
	Position             string
	DataBatchID          DurableID
	SnapshotPositions    map[string]string
	SnapshotIncarnations map[string]string
	SnapshotSchemas      map[string]string
}

// CDCStateProvider exposes the state produced by a completed batch CDC read.
// The pipeline persists it only after the destination write succeeds.
type CDCStateProvider interface {
	CDCState() CDCStateCommitToken
}

// TableIncarnationProvider exposes an opaque source-native identity that
// changes when a same-name table is dropped and recreated.
type TableIncarnationProvider interface {
	TableIncarnation(ctx context.Context, table string) (string, error)
}

// TableSchemaFingerprintProvider exposes a source-native schema identity used
// to invalidate snapshot state after offline DDL that keeps the table identity.
type TableSchemaFingerprintProvider interface {
	TableSchemaFingerprint(ctx context.Context, table string) (string, error)
}

// ConnectorLease fences one logical connector across processes for the full
// ingestion run. Release must be safe to call more than once.
type ConnectorLease interface {
	Done() <-chan struct{}
	Err() error
	Release() error
}

type ConnectorIdentity struct {
	Database  string
	Connector string
}

// ConnectorIdentityProvider exposes the effective source identity after the
// driver has resolved environment, service-file, and connection defaults.
type ConnectorIdentityProvider interface {
	ConnectorIdentity(ctx context.Context) (ConnectorIdentity, error)
}

type ConnectorPreflightOptions struct {
	Streaming bool
}

// ConnectorPreflightValidator performs non-mutating compatibility checks after
// the source connection resolves server capabilities and before the pipeline
// connects to or mutates a destination.
type ConnectorPreflightValidator interface {
	ValidateConnectorPreflight(ctx context.Context, opts ConnectorPreflightOptions) error
}

type ConnectorLeaseOptions struct {
	ConnectorID      string
	SlotSuffix       string
	LegacySlotSuffix string
	SourceTable      string
}

// ConnectorLeaser is implemented by sources that can serialize connector runs
// using a source-side lock independent of destination transaction semantics.
type ConnectorLeaser interface {
	AcquireConnectorLease(ctx context.Context, opts ConnectorLeaseOptions) (ConnectorLease, error)
}

type ConnectorPreparer interface {
	PrepareConnector(ctx context.Context) error
}

// TableExistenceChecker distinguishes a confirmed missing source table from a
// transient schema lookup failure before managed state is invalidated.
type TableExistenceChecker interface {
	TableExists(ctx context.Context, table string) (bool, error)
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

// SnapshotResetEmitter identifies sources whose record stream can begin a
// fresh snapshot with SnapshotReset control records.
type SnapshotResetEmitter interface {
	EmitsSnapshotResets() bool
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
	Name              string
	Schema            *schema.TableSchema
	PrimaryKeys       []string
	DestSchema        string
	Incarnation       string
	SchemaFingerprint string
}

// MultiTableReadOptions extends ReadOptions for multi-table reads.
type MultiTableReadOptions struct {
	ReadOptions
	Tables                      []string          // Filter to specific tables (empty = all tables)
	KnownTables                 []string          // Tables already prepared by the pipeline before ReadAll
	CDCResumeLSNs               map[string]string // Per-table CDC resume LSNs: table name → max LSN already processed
	CDCResumeIncarnations       map[string]string
	CDCResumeSchemaFingerprints map[string]string
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

// CDCLegacySlotFinalizer removes an obsolete automatic replication slot after
// a successful managed-state cutover. The pipeline calls it only after the
// destination write, state persistence, and batch finalization are durable.
type CDCLegacySlotFinalizer interface {
	FinalizeLegacySlot(ctx context.Context) error
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
