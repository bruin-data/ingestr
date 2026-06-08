package destination

import (
	"context"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const ManagedStagingTTL = 24 * time.Hour

type PrepareOptions struct {
	Table        string
	Schema       *schema.TableSchema
	DropFirst    bool
	PrimaryKeys  []string
	PartitionBy  string   // Column to partition by (BigQuery)
	ClusterBy    []string // Columns to cluster by (BigQuery)
	CDCMode      bool     // If true, make non-PK columns nullable for staging tables (CDC delete handling)
	ExpiresAfter time.Duration
}

type WriteOptions struct {
	Table            string
	Schema           *schema.TableSchema
	PrimaryKeys      []string
	Parallelism      int
	AtomicCommit     bool
	StagingTable     bool
	StagingBucket    string
	LoaderFileSize   int
	LoaderFileFormat string
}

type Transaction interface {
	Exec(ctx context.Context, sql string, args ...interface{}) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// MergeOptions contains parameters for merge operations.
type MergeOptions struct {
	StagingTable   string
	TargetTable    string
	PrimaryKeys    []string
	Columns        []string
	IncrementalKey string
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
	StagingTable   string
	TargetTable    string
	PrimaryKeys    []string
	IncrementalKey string
	Schema         *schema.TableSchema
}

// SCD2Options contains parameters for SCD2 (Slowly Changing Dimensions Type 2) operations.
type SCD2Options struct {
	StagingTable   string
	TargetTable    string
	PrimaryKeys    []string
	Columns        []string  // All original columns (excluding SCD columns)
	IncrementalKey string    // Optional: for optimization (skip soft-delete if set)
	Timestamp      time.Time // Single timestamp for the entire operation (used for _scd_valid_from and _scd_valid_to)
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

// TableWriteConfig contains per-table write configuration for multi-table writes.
type TableWriteConfig struct {
	DestTable   string
	Schema      *schema.TableSchema
	PrimaryKeys []string
}

// MultiTableWriteOptions configures multi-table write behavior.
type MultiTableWriteOptions struct {
	TableConfigs     map[string]TableWriteConfig // source table name → config
	Parallelism      int                         // concurrent writers per table
	StagingTable     bool
	StagingBucket    string
	LoaderFileSize   int
	LoaderFileFormat string
}

// CDCResumeProvider is an optional interface that destinations can implement
// to support CDC resume functionality. If implemented, the pipeline will query
// the destination for the max CDC LSN to resume from.
type CDCResumeProvider interface {
	// GetMaxCDCLSN returns the maximum _cdc_lsn value from the table, or empty string if none found.
	GetMaxCDCLSN(ctx context.Context, table string) (string, error)
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
