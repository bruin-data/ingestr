package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// TruncateInsertStrategy empties the destination table in place and writes new
// rows directly into it. Unlike ReplaceStrategy (which drops + recreates via a
// staging swap), this preserves the table definition and any dependent objects
// (views, grants, foreign keys).
//
// Tradeoffs the user has already accepted by opting in:
//   - Non-atomic: the table is empty for the duration of the load, so
//     concurrent readers may see an empty result set.
//   - No rollback: if the insert fails after truncate, the target is left empty.
//   - Schema drift: the existing table's schema is preserved as-is; this
//     strategy does not drop and recreate to pick up schema changes.
type TruncateInsertStrategy struct{}

func (s *TruncateInsertStrategy) Name() config.IncrementalStrategy {
	return config.StrategyTruncateInsert
}

func (s *TruncateInsertStrategy) Validate(cfg *config.IngestConfig) error {
	return nil
}

func (s *TruncateInsertStrategy) RequiresPrimaryKey() bool {
	return false
}

func (s *TruncateInsertStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *TruncateInsertStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	truncator, ok := job.Destination.(destination.TruncateCapable)
	if !ok {
		return fmt.Errorf("destination does not support truncate+insert strategy; use replace instead")
	}

	targetTable := job.Config.DestTable
	config.Debug("[TRUNCATE+INSERT] Target table: %s", targetTable)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       targetTable,
		Schema:      job.Schema,
		DropFirst:   false,
		PrimaryKeys: job.Config.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare table: %w", err)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}

	if err := truncator.TruncateTable(ctx, targetTable); err != nil {
		return fmt.Errorf("failed to truncate table: %w", err)
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readOpts := source.ReadOptions{
		IncrementalKey: job.Config.IncrementalKey,
		IntervalStart:  job.Config.IntervalStart,
		IntervalEnd:    job.Config.IntervalEnd,
		PageSize:       job.Config.PageSize,
		Limit:          job.Config.SQLLimit,
		ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism:    parallelism,
		Schema:         job.SourceSchema,
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	records, err = job.ApplyBatchTransformation(ctx, records)
	if err != nil {
		return fmt.Errorf("failed to apply batch transformation: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            targetTable,
		Parallelism:      parallelism,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}
