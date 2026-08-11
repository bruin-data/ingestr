package destination

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

// ErrPreStageUnsupported is returned by PreStager.NewPreStageWriter when the
// destination cannot pre-stage load files for the current configuration
// (e.g. BigQuery configured with the storage_write load method).
var ErrPreStageUnsupported = errors.New("pre-staging is not supported for this configuration")

// PreStageOptions configures extract-time load-file staging.
type PreStageOptions struct {
	// Table is the destination table name, used only for load-file naming.
	Table string

	// KeyTransform maps a source column name to the destination column name
	// assumed at extract time. The pipeline verifies the assumption after
	// schema inference and discards the pre-staged files on mismatch.
	KeyTransform func(string) string

	// LoadTimestampColumn, when non-empty, is injected into every staged row
	// with LoadTimestamp as its value.
	LoadTimestampColumn string
	LoadTimestamp       time.Time

	// StagingTable indicates the write phase targets a fresh staging table.
	// Destinations may use it to pick file chunking policies.
	StagingTable bool

	StagingBucket    string
	LoaderFileSize   int
	LoaderFileFormat string
	Parallelism      int
}

// PreStagedData is an opaque handle to load files staged during extract.
// It is passed back to the destination through WriteOptions.PreStaged.
type PreStagedData interface {
	// RowCount reports the number of rows staged.
	RowCount() int64
	// Close removes the staged files. Safe to call multiple times.
	Close()
}

// PreStageWriter consumes record batches during extract and stages them as
// destination-native load files. Append must not retain the batch.
type PreStageWriter interface {
	Append(ctx context.Context, batch arrow.RecordBatch) error
	// Finish finalizes the staged files. It returns nil data when no rows
	// were staged.
	Finish() (PreStagedData, error)
	// Discard aborts staging and removes any files written so far.
	Discard()
}

// PreStager is an optional destination interface: destinations that can load
// from pre-staged files (written concurrently with the source extract) allow
// the pipeline to skip the buffer-replay write path for schema-inferred
// sources.
type PreStager interface {
	NewPreStageWriter(ctx context.Context, opts PreStageOptions) (PreStageWriter, error)
}
