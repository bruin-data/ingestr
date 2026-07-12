package destination

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const ManagedStagingTTL = 24 * time.Hour

type PrepareOptions struct {
	Table                  string
	Schema                 *schema.TableSchema
	DropFirst              bool
	PrimaryKeys            []string
	PartitionBy            string   // Column to partition by (BigQuery)
	ClusterBy              []string // Columns to cluster by (BigQuery)
	CDCMode                bool     // If true, make non-PK columns nullable for staging tables (CDC delete handling)
	ExpiresAfter           time.Duration
	PreserveExistingLayout bool // Leave an existing table's properties, partition spec, and sort order unchanged.
	TableProperties        map[string]string
	OwnershipToken         string
}

type WriteOptions struct {
	Table  string
	Schema *schema.TableSchema
	// TargetSchema is the visible schema produced by an atomic replacement when
	// Schema contains staging-only columns needed while processing the input.
	TargetSchema     *schema.TableSchema
	PrimaryKeys      []string
	Parallelism      int
	AtomicCommit     bool
	StagingTable     bool
	StagingBucket    string
	LoaderFileSize   int
	LoaderFileFormat string
	// CommitToken identifies the exact payload in this write. Destinations that
	// support durable token commits use it to make exact replays idempotent.
	CommitToken source.DurableID
	// CDCResumeLSN is the source position made durable by this write. It must
	// become visible atomically with the data commit.
	CDCResumeLSN string
	// SkipCDCResume prevents destinations from inferring a resume cursor from
	// CDC columns. It is used for durable snapshot chunks that are individually
	// idempotent but cannot safely advance the source checkpoint yet.
	SkipCDCResume bool
	// CDCExpectedIncarnation fences a managed CDC write to the physical target
	// that was bound before the source read began.
	CDCExpectedIncarnation string
	// DeduplicatePrimaryKeys requests one output row per configured primary
	// key. IncrementalKey selects the greatest value; arrival order breaks ties.
	DeduplicatePrimaryKeys bool
	IncrementalKey         string
	// AtomicSnapshotAttemptID identifies the logical snapshot attempt. It must
	// remain stable across retries of that attempt.
	AtomicSnapshotAttemptID string

	// PreStaged holds load files written during extract by a PreStager
	// destination. When set, the destination loads these files instead of
	// consuming the records channel (which will be empty).
	PreStaged PreStagedData
}

type Transaction interface {
	Exec(ctx context.Context, sql string, args ...interface{}) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// MergeOptions contains parameters for merge operations.
type MergeOptions struct {
	StagingTable         string
	TargetTable          string
	PrimaryKeys          []string
	Columns              []string
	IncrementalKey       string
	IncrementalPredicate string
	Schema               *schema.TableSchema
	CommitToken          source.DurableID
	CDCResumeLSN         string
	SkipCDCResume        bool
	// CDCExpectedIncarnation fences a managed CDC merge to the physical target
	// that was bound before the source read began.
	CDCExpectedIncarnation string
	Parallelism            int
}

// DeleteInsertOptions contains parameters for delete+insert operations.
type DeleteInsertOptions struct {
	StagingTable       string
	TargetTable        string
	IncrementalKey     string
	IncrementalKeyType schema.DataType
	IntervalStart      interface{}
	IntervalEnd        interface{}
	Columns            []string
	PrimaryKeys        []string
}

type SwapOptions struct {
	StagingTable                  string
	TargetTable                   string
	PrimaryKeys                   []string
	IncrementalKey                string
	Schema                        *schema.TableSchema
	CDCExpectedIncarnation        string
	CDCExpectedStagingIncarnation string
}

// SCD2Options contains parameters for SCD2 (Slowly Changing Dimensions Type 2) operations.
type SCD2Options struct {
	StagingTable   string
	TargetTable    string
	PrimaryKeys    []string
	Columns        []string  // All original columns (excluding SCD columns)
	IncrementalKey string    // Optional: for optimization (skip soft-delete if set)
	Timestamp      time.Time // Single timestamp for the entire operation (used for _scd_valid_from and _scd_valid_to)
	Schema         *schema.TableSchema
}

type Destination interface {
	Schemes() []string
	Connect(ctx context.Context, uri string) error
	Close(ctx context.Context) error
	PrepareTable(ctx context.Context, opts PrepareOptions) error
	Write(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error
	WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error
	SwapTable(ctx context.Context, opts SwapOptions) error
	MergeTable(ctx context.Context, opts MergeOptions) error
	DeleteInsertTable(ctx context.Context, opts DeleteInsertOptions) error
	SCD2Table(ctx context.Context, opts SCD2Options) error
	DropTable(ctx context.Context, table string) error
	Exec(ctx context.Context, sql string, args ...interface{}) error
	BeginTransaction(ctx context.Context) (Transaction, error)

	// GetTableSchema returns the current schema of a table, or nil if table doesn't exist.
	GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error)

	// GetScheme returns the primary URI scheme for this destination (for dialect lookup).
	GetScheme() string

	// Strategy support methods
	SupportsReplaceStrategy() bool
	SupportsAppendStrategy() bool
	SupportsMergeStrategy() bool
	SupportsDeleteInsertStrategy() bool
	SupportsSCD2Strategy() bool

	// SupportsAtomicSwap returns true if the destination supports atomic table swaps.
	// If false, the replace strategy will skip staging and write directly to the target.
	SupportsAtomicSwap() bool
}

// IncrementalPredicateSupport is implemented by destinations whose MergeTable
// honors MergeOptions.IncrementalPredicate; others silently ignore it, so the
// pipeline rejects the flag for them.
type IncrementalPredicateSupport interface {
	SupportsIncrementalPredicate() bool
}

// SchemaEvolutionColumnNormalizer lets a destination compare logical source
// types using the canonical types it can recover from its physical schema.
type SchemaEvolutionColumnNormalizer interface {
	NormalizeSchemaEvolutionColumn(schema.Column) schema.Column
}

// TableWriteConfig contains per-table write configuration for multi-table writes.
type TableWriteConfig struct {
	DestTable              string
	Schema                 *schema.TableSchema
	PrimaryKeys            []string
	DeduplicatePrimaryKeys bool
	IncrementalKey         string
	IncrementalPredicate   string
	CDCMode                bool
	SkipCDCResume          bool
	CDCExpectedIncarnation string
}

// MultiTableWriteOptions configures multi-table write behavior.
type MultiTableWriteOptions struct {
	TableConfigs     map[string]TableWriteConfig // source table name → config
	Parallelism      int                         // concurrent writers per table
	StagingTable     bool
	StagingBucket    string
	LoaderFileSize   int
	LoaderFileFormat string
	// CancelSource stops the producer when a per-table writer fails. The writer
	// drains records after invoking it so producer-owned resources can unwind.
	CancelSource       context.CancelFunc
	CancelDrainTimeout time.Duration
	// TableWriter overrides the destination's per-table write path. Its boolean
	// result reports whether it applied at least one source truncate boundary.
	TableWriter func(context.Context, string, <-chan source.RecordBatchResult, WriteOptions) (bool, error)
}

// CDCResumeProvider is an optional interface that destinations can implement
// to support CDC resume functionality. If implemented, the pipeline will query
// the destination for its latest durable CDC position.
type CDCResumeProvider interface {
	// GetMaxCDCLSN returns the latest durable CDC cursor, or an empty string if
	// no CDC data or checkpoint has committed.
	GetMaxCDCLSN(ctx context.Context, table string) (string, error)
}

const CDCStateStatusComplete = "complete"

// CDCStateEntry is one append-only event from the shared destination CDC state
// table. Consumers reduce entries by source table, state kind, and generation.
type CDCStateEntry struct {
	EventID          string
	SourceTable      string
	DestinationTable string
	StateKind        string
	Generation       int64
	Status           string
	Position         string
	RecordedAt       time.Time
}

// CDCStateReader loads all state events for one logical connector from the
// destination's shared managed-state table.
type CDCStateReader interface {
	LoadCDCState(ctx context.Context, table, connectorID string) ([]CDCStateEntry, error)
}

// CDCStateFence identifies every run sentinel at a connector's latest run
// generation. Multiple event IDs mean ownership of that generation conflicts.
type CDCStateFence struct {
	Generation  int64
	RunEventIDs []string
}

// CDCStateFenceReader loads only the latest-generation run sentinels needed to
// fence checkpoint writes, without scanning snapshot or checkpoint events.
type CDCStateFenceReader interface {
	LoadCDCStateFence(ctx context.Context, table, connectorID string) (CDCStateFence, error)
}

// CDCStateWriter persists CDC state using destination-specific durability
// guarantees that may be stronger than ordinary data writes.
type CDCStateWriter interface {
	WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error
}

type CDCTargetClaim struct {
	DestinationTable string
	ConnectorID      string
	SourceTable      string
}

func (c CDCTargetClaim) OwnerID() (string, error) {
	if c.ConnectorID == "" || c.SourceTable == "" {
		return "", fmt.Errorf("CDC target claim requires non-empty connector and source table identifiers")
	}
	return CDCTargetOwnerID(c.ConnectorID, c.SourceTable), nil
}

func CDCTargetOwnerID(connectorID, sourceTable string) string {
	sum := sha256.Sum256([]byte(connectorID + "\x00" + sourceTable))
	return hex.EncodeToString(sum[:])
}

// CDCTargetKey encodes identifier components without collisions between dots
// inside quoted identifiers and qualification separators.
func CDCTargetKey(components ...string) string {
	var key strings.Builder
	for _, component := range components {
		encoded := hex.EncodeToString([]byte(component))
		key.WriteString(strconv.Itoa(len(encoded)))
		key.WriteByte(':')
		key.WriteString(encoded)
	}
	return key.String()
}

func CDCTargetKeyDigest(components ...string) string {
	sum := sha256.Sum256([]byte(CDCTargetKey(components...)))
	return hex.EncodeToString(sum[:])
}

// CDCTargetClaimer durably assigns one canonical destination table to one CDC
// connector. Claims are permanent, idempotent for the current owner, and must
// atomically reject a different connector even across concurrent processes.
type CDCTargetClaimer interface {
	ClaimCDCTarget(ctx context.Context, claimTable string, claim CDCTargetClaim) error
}

// CDCLateTargetClaimPreparer atomically reserves an absent CDC target and
// creates it empty. If the target already exists or the reservation conflicts,
// neither the target nor its claim may be changed.
type CDCLateTargetClaimPreparer interface {
	ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim CDCTargetClaim, opts PrepareOptions) (incarnation string, err error)
}

// CDCConditionalTruncater empties a CDC target only when its current physical
// identity is exactly the expected incarnation. The comparison and truncate
// must be protected by one destination-level transaction or lock.
type CDCConditionalTruncater interface {
	TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error
}

// CDCConditionalSwapCapable advertises that SwapTable enforces both expected
// target and staging incarnations in the same atomic operation that replaces
// the target.
type CDCConditionalSwapCapable interface {
	SupportsCDCConditionalSwap() bool
}

// CDCTargetIncarnationInitializer establishes destination-specific physical
// identity metadata after a target claim succeeds. It must not be called while
// validating an unclaimed target.
type CDCTargetIncarnationInitializer interface {
	EnsureCDCTargetIncarnation(ctx context.Context, table string) (incarnation string, exists bool, err error)
}

// CDCTargetIdentityProvider resolves a configured target to the destination's
// canonical physical namespace. Managed CDC uses it to distinguish unqualified
// targets that resolve differently under different connection identities.
type CDCTargetIdentityProvider interface {
	CanonicalCDCTarget(ctx context.Context, table string) (string, error)
}

// CDCTargetIncarnationProvider returns an opaque identity for the current
// physical table. The identity must change when the table is dropped,
// recreated, or otherwise replaced outside the CDC run.
type CDCTargetIncarnationProvider interface {
	CDCTargetIncarnation(ctx context.Context, table string) (incarnation string, exists bool, err error)
}

// ManagedCDCWriteFencer is implemented by destinations whose row writes,
// merges, and table swaps atomically reject a stale CDCExpectedIncarnation.
// Reading the target incarnation separately is not sufficient because the
// target can be replaced between that read and the DML commit.
type ManagedCDCWriteFencer interface {
	SupportsManagedCDCWriteFencing() bool
}

// ManagedCDCStateValidator checks destination-specific durability requirements
// before a managed CDC run acquires a lease or mutates destination state.
type ManagedCDCStateValidator interface {
	ValidateManagedCDCState() error
}

// ManagedCDCTargetValidator checks requirements that depend on the resolved
// destination table, such as a database-scoped SQL compatibility level.
type ManagedCDCTargetValidator interface {
	ValidateManagedCDCTarget(ctx context.Context, table string) error
}

// CDCStatePruner removes superseded events belonging to one connector. State
// managers append replacement events before pruning, so cleanup is retryable
// and never participates in the durability decision for a checkpoint.
type CDCStatePruner interface {
	DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error
}

// CDCStatePruneBatchSizer advertises how many exact event IDs a destination
// can accept in one state-pruning phase. Implementations may chunk internally.
type CDCStatePruneBatchSizer interface {
	CDCStatePruneBatchSize() int
}

// DurableCommitTokenWriter is an optional interface for destinations that can
// durably record a source flush token without changing table rows. Streaming
// uses it when a source position advances during an otherwise empty flush.
type DurableCommitTokenWriter interface {
	CommitWriteToken(ctx context.Context, table string, commitToken source.DurableID, cdcResumeLSN string) error
}

// IdempotentCommitTokenWriter is implemented by destinations whose row-write
// path atomically records WriteOptions.CommitToken and skips the rows when the
// same token is retried. Keyless CDC streaming requires this because it has no
// row identity with which to deduplicate a replayed transaction.
type IdempotentCommitTokenWriter interface {
	SupportsIdempotentCommitTokenWrites() bool
}

// AtomicSnapshotOptions describes a streamed source snapshot whose pages must
// remain hidden until its final durable source position is available.
type AtomicSnapshotOptions struct {
	Table         string
	Schema        *schema.TableSchema
	TargetSchema  *schema.TableSchema
	PrimaryKeys   []string
	PartitionBy   string
	ClusterBy     []string
	Parallelism   int
	CommitToken   source.DurableID
	CDCResumeLSN  string
	SkipCDCResume bool
	// CDCExpectedIncarnation fences publication to the physical target that was
	// bound before the source snapshot began.
	CDCExpectedIncarnation string
	// AttemptID must be non-empty and stable across every retry of one logical
	// snapshot. A new logical snapshot must use a new value.
	AttemptID string
}

// AtomicSnapshotPublisher is implemented by destinations that can durably
// stage snapshot pages and publish the completed snapshot in one table commit.
// It does not provide atomic publication across multiple destination tables.
// Streaming falls back to its historical truncate-and-page behavior when this
// interface is not implemented. Begin and write must durably finish before
// returning success and be idempotent for one AttemptID. Publish retries for a
// sealed attempt use the original commit token, source boundary, and target
// incarnation fence. Once publication becomes durable, retrying publish for
// that AttemptID must return the original published incarnation without
// duplicating rows or replacing a newer attempt, including when the original
// success response was lost.
type AtomicSnapshotPublisher interface {
	BeginAtomicSnapshot(ctx context.Context, opts AtomicSnapshotOptions) error
	EvolveAtomicSnapshot(ctx context.Context, opts AtomicSnapshotOptions) error
	WriteAtomicSnapshot(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error
	// PublishAtomicSnapshot returns the exact physical incarnation made visible
	// by the commit. Managed CDC uses it to bind state without trusting a
	// post-publication identity lookup that could race with an external replace.
	PublishAtomicSnapshot(ctx context.Context, opts AtomicSnapshotOptions) (publishedIncarnation string, err error)
}

// AtomicSnapshotAborter reclaims an owned snapshot stage only when the caller
// knows publication was never attempted. Abort must be idempotent because a
// successful response can be lost before the durable attempt is retired. It is
// separate from AtomicSnapshotPublisher so existing publishers remain compatible.
type AtomicSnapshotAborter interface {
	AbortAtomicSnapshot(ctx context.Context, opts AtomicSnapshotOptions) error
}

// AtomicSnapshotDiscarder retires an obsolete sealed attempt whose publish
// result may have been lost. It must delete only an unpublished stage and must
// be an idempotent no-op when that AttemptID was already published. It must
// never remove or replace a visible target.
type AtomicSnapshotDiscarder interface {
	DiscardAtomicSnapshot(ctx context.Context, opts AtomicSnapshotOptions) error
}

type OwnedTableDropper interface {
	DropTableIfOwned(ctx context.Context, table, ownershipToken string) error
}

// ConnectionChecker is an optional end-to-end connection check. Unlike
// Connect, it verifies that the destination can create, write, read, and clean
// up data using the configured credentials and storage.
type ConnectionChecker interface {
	CheckConnection(ctx context.Context) error
}

// CDCMergeAware is an optional interface that destinations can implement
// to indicate they handle CDC deletes during merge operations.
type CDCMergeAware interface {
	SupportsCDCMerge() bool
}

// AtomicCommitWriter is an optional interface for destinations that can make a
// write atomically visible after the write phase completes.
type AtomicCommitWriter interface {
	SupportsAtomicCommitWrites() bool
}

// DirectReplaceDeduplicator is implemented by destinations that can collapse
// duplicate primary keys as part of a direct replace write. Destinations must
// not opt in unless the deduplication and data replacement commit atomically.
type DirectReplaceDeduplicator interface {
	SupportsDirectReplaceDeduplication() bool
}

// MultiTableNamer is an optional interface for destinations that need full
// control over multi-table destination naming (e.g. BigQuery, which keys off the
// connection dataset). Destinations without it use DefaultMultiTableName.
type MultiTableNamer interface {
	DestTableName(destSchema, sourceTable string) string
}

// DefaultMultiTableName builds the destination table name for a multi-table
// source table on destinations that don't implement MultiTableNamer.
//
// When a destination schema is configured, all tables are funneled into it and
// any source-schema qualifier is flattened into the table name
// ("dbo.orders" -> "<destSchema>.dbo_orders"), yielding an unambiguous two-part
// name. Without a destination schema the source name is mirrored verbatim
// ("dbo.orders" -> "dbo.orders"), recreating the source's schema layout on the
// destination. Flattening matters because a name like "<destSchema>.dbo.orders"
// is otherwise indistinguishable from a catalog.schema.table reference.
func DefaultMultiTableName(destSchema, sourceTable string) string {
	if destSchema == "" {
		return sourceTable
	}
	return destSchema + "." + strings.ReplaceAll(sourceTable, ".", "_")
}

// ExactRowCountWaiter is an optional interface for destinations that can
// verify when a table has become query-consistent at an exact row count.
type ExactRowCountWaiter interface {
	WaitForExactRowCount(ctx context.Context, table string, expectedRows int64) error
}

// TruncateCapable is an optional interface for destinations that can empty a
// table in place without dropping it. Used by the truncate+insert strategy so
// dependent objects (views, grants, foreign keys) survive the reload.
type TruncateCapable interface {
	TruncateTable(ctx context.Context, table string) error
}

// CDCTruncateCapable empties a CDC target without replacing its physical table
// identity. Native TRUNCATE implementations may otherwise use drop/create.
type CDCTruncateCapable interface {
	TruncateCDCTable(ctx context.Context, table string) error
}

// AtomicTruncateInsertWriter replaces all table rows in one destination
// commit. Strategies prefer it over a separate truncate followed by writes so
// readers never observe an empty or partially reloaded target.
type AtomicTruncateInsertWriter interface {
	TruncateInsertRecords(ctx context.Context, records <-chan source.RecordBatchResult, opts WriteOptions) error
}

// AtomicTruncateInsertBoundaryAware acknowledges that TruncateInsertRecords
// treats every Truncate marker as a replacement boundary: rows received before
// the marker must not be present in the final atomic commit.
type AtomicTruncateInsertBoundaryAware interface {
	SupportsTruncateInsertBoundaries() bool
}

// AtomicTruncateInsertStagingAware acknowledges that TruncateInsertRecords
// can consume staging-only input columns while publishing only TargetSchema.
type AtomicTruncateInsertStagingAware interface {
	SupportsTruncateInsertStagingColumns() bool
}

// AtomicTruncateInsertSchemaEvolver marks an atomic truncate+insert writer that
// commits supported schema evolution and the replacement rows together.
type AtomicTruncateInsertSchemaEvolver interface {
	EvolvesTruncateInsertSchemaAtomically() bool
}

// ConcurrentFlusher is an optional interface for destinations whose
// write+merge cycles for *different* tables can safely run concurrently
// (connection-pool backed databases, where each cycle uses its own
// connections and transactions). The streaming flush loop uses it to merge
// multiple tables in parallel instead of sequentially. Destinations without
// it — or returning a value < 2 — are flushed one table at a time.
type ConcurrentFlusher interface {
	MaxConcurrentFlushes() int
}

// DirectMergeWriter atomically merges record batches into a target without a
// remote staging-table write/read round trip.
type DirectMergeWriter interface {
	MergeRecords(ctx context.Context, records <-chan source.RecordBatchResult, writeOpts WriteOptions, mergeOpts MergeOptions) error
}

// ReplaceStagingPlacement declares the default schema placement for replace
// staging tables.
type ReplaceStagingPlacement string

const (
	// ReplaceStagingManagedSchema stages in a destination-managed schema such as
	// _bruin_staging.
	ReplaceStagingManagedSchema ReplaceStagingPlacement = "managed_schema"
	// ReplaceStagingTargetSchema stages in the target table's schema by default.
	ReplaceStagingTargetSchema ReplaceStagingPlacement = "target_schema"
)

// ReplaceStagingPolicy declares how replace should choose staging table names
// for a destination.
type ReplaceStagingPolicy struct {
	DefaultPlacement     ReplaceStagingPlacement
	DefaultManagedSchema string
	DefaultTargetSchema  string
}

// ReplaceStagingPolicyProvider lets a destination declare where replace
// staging tables should live while keeping strategy orchestration generic.
type ReplaceStagingPolicyProvider interface {
	ReplaceStagingPolicy() ReplaceStagingPolicy
}

// ManagedStagingPolicyProvider lets a destination declare where strategy-owned
// staging tables should live when the user did not configure a staging dataset.
type ManagedStagingPolicyProvider interface {
	ManagedStagingPolicy() ReplaceStagingPolicy
}

// ManagedCDCStateCatalogProvider supplies the connected catalog/project when
// a CDC state table has no target anchor from which to derive one.
type ManagedCDCStateCatalogProvider interface {
	ManagedCDCStateCatalog() string
}
