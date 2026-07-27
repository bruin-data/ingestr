package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// inPlaceReplaceStrategy implements replace by emptying the destination
// table in place and loading the replacement rows from staging. It is selected
// internally for destinations where replacing the table object would break
// dependent views, grants, or foreign keys.
//
// Rows are first written to a staging table and only copied into the target
// after the source read succeeds. Destinations can finalize the replacement
// atomically; otherwise the target is truncated and populated from staging.
// With primary keys, staging is deduplicated unless the source guarantees that
// its effective primary keys are unique.
//
// Destinations without atomic finalization retain these tradeoffs:
//   - Non-atomic: the target is empty between TRUNCATE and the final insert,
//     so concurrent readers may see an empty result set.
//   - No rollback: if the insert fails after truncate, the target is left empty.
type inPlaceReplaceStrategy struct{}

// NewInPlaceReplaceStrategy returns the replace implementation used by
// destinations that must preserve the target table object.
func NewInPlaceReplaceStrategy() WriteStrategy {
	return &inPlaceReplaceStrategy{}
}

func (s *inPlaceReplaceStrategy) Name() config.IncrementalStrategy {
	return config.StrategyReplace
}

func (s *inPlaceReplaceStrategy) Validate(cfg *config.IngestConfig) error {
	return nil
}

func (s *inPlaceReplaceStrategy) RequiresPrimaryKey() bool {
	return false
}

func (s *inPlaceReplaceStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *inPlaceReplaceStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	truncator, ok := job.Destination.(destination.TruncateCapable)
	if !ok {
		return fmt.Errorf("destination does not support in-place replace")
	}
	return s.executeWithStaging(ctx, job, truncator)
}

func (s *inPlaceReplaceStrategy) executeWithStaging(ctx context.Context, job *IngestionJob, truncator destination.TruncateCapable) error {
	atomicWriter, supportsAtomicFinalize := job.Destination.(destination.AtomicTruncateInsertStagingWriter)
	stagingInserter, supportsStagingInsert := job.Destination.(destination.StagingTableInserter)
	stagingInsertPreparer, supportsPreparedStagingInsert := job.Destination.(destination.StagingTableInsertPreparer)
	stagingMergePreparer, supportsPreparedStagingMerge := job.Destination.(destination.StagingTableMergePreparer)
	if len(job.Config.PrimaryKeys) > 0 && !supportsAtomicFinalize && !job.Destination.SupportsMergeStrategy() {
		return fmt.Errorf("destination does not support deduplicated in-place replace (merge not supported)")
	}
	if len(job.Config.PrimaryKeys) == 0 && !supportsAtomicFinalize && !supportsStagingInsert && !supportsPreparedStagingInsert {
		return fmt.Errorf("destination does not support keyless in-place replace from staging")
	}

	targetTable := job.Config.DestTable
	stagingTable := managedStagingTableName(job.Destination, targetTable, "ti", job.Config.StagingDataset)
	output.Statusf("[REPLACE] %s | Using in-place replacement with staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       targetTable,
		Schema:      destination.DestinationTableSchema(job.Schema),
		DropFirst:   false,
		PrimaryKeys: job.Config.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table: %w", err)
	}

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:        stagingTable,
		Schema:       job.Schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		PartitionBy:  job.Config.PartitionBy,
		ClusterBy:    job.Config.ClusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table: %w", err)
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readOpts := source.ReadOptions{
		IncrementalKey:                  job.Config.IncrementalKey,
		IntervalStart:                   job.Config.IntervalStart,
		IntervalEnd:                     job.Config.IntervalEnd,
		ExtractPartitionBy:              job.Config.ExtractPartitionBy,
		ExtractPartitionInterval:        job.Config.ExtractPartitionInterval,
		ExtractPartitionNumericInterval: job.Config.ExtractPartitionNumericInterval,
		ExtractPartitionAuto:            job.Config.ExtractPartitionAuto,
		PageSize:                        job.Config.PageSize,
		Limit:                           job.Config.SQLLimit,
		ExcludeColumns:                  job.Config.SQLExcludeColumns,
		Parallelism:                     parallelism,
		Schema:                          job.SourceSchema,
		CDCSlotSuffix:                   job.Config.CDCSlotSuffix,
		CDCLegacySlotSuffix:             job.Config.CDCLegacySlotSuffix,
		FullRefresh:                     job.Config.FullRefresh,
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      job.Config.EffectiveDestinationParallelism(),
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
	}); err != nil {
		return fmt.Errorf("failed to write to staging: %w", err)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}

	stagingPrimaryKeysUnique := effectivePrimaryKeysGuaranteedUnique(job)
	incrementalKey := mergeIncrementalKeyForSchema(job.Schema, job.Config.IncrementalKey)
	insertOpts := destination.InsertFromStagingOptions{
		StagingTable: stagingTable,
		TargetTable:  targetTable,
		Columns:      job.Schema.ColumnNames(),
	}
	mergeOpts := destination.MergeOptions{
		StagingTable:             stagingTable,
		TargetTable:              targetTable,
		PrimaryKeys:              job.Config.PrimaryKeys,
		StagingPrimaryKeysUnique: stagingPrimaryKeysUnique,
		Columns:                  job.Schema.ColumnNames(),
		IncrementalKey:           incrementalKey,
		Schema:                   job.Schema,
	}
	var preparedStagingWrite destination.PreparedStagingTableWrite
	if !supportsAtomicFinalize {
		if len(job.Config.PrimaryKeys) > 0 && supportsPreparedStagingMerge {
			preparedStagingWrite, err = stagingMergePreparer.PrepareMergeTable(ctx, mergeOpts)
			if err != nil {
				return fmt.Errorf("failed to prepare merge from staging: %w", err)
			}
		} else if len(job.Config.PrimaryKeys) == 0 && supportsPreparedStagingInsert {
			preparedStagingWrite, err = stagingInsertPreparer.PrepareInsertFromStaging(ctx, insertOpts)
			if err != nil {
				return fmt.Errorf("failed to prepare insert from staging: %w", err)
			}
		}
	}
	if supportsAtomicFinalize {
		config.Debug("[REPLACE] Executing atomic in-place replacement from staging")
		if err := atomicWriter.TruncateInsertFromStaging(ctx, destination.TruncateInsertFromStagingOptions{
			StagingTable:             stagingTable,
			TargetTable:              targetTable,
			PrimaryKeys:              job.Config.PrimaryKeys,
			StagingPrimaryKeysUnique: stagingPrimaryKeysUnique,
			Columns:                  job.Schema.ColumnNames(),
			IncrementalKey:           incrementalKey,
		}); err != nil {
			return fmt.Errorf("failed to atomically insert from staging: %w", err)
		}
	} else {
		if err := truncator.TruncateTable(ctx, targetTable); err != nil {
			return fmt.Errorf("failed to truncate target: %w", err)
		}
		if preparedStagingWrite != nil {
			if err := preparedStagingWrite.Execute(ctx); err != nil {
				return fmt.Errorf("failed to insert from staging: %w", err)
			}
		} else if len(job.Config.PrimaryKeys) > 0 {
			if err := job.Destination.MergeTable(ctx, mergeOpts); err != nil {
				return fmt.Errorf("failed to insert from staging: %w", err)
			}
		} else if err := stagingInserter.InsertFromStaging(ctx, insertOpts); err != nil {
			return fmt.Errorf("failed to insert from staging: %w", err)
		}
	}

	if !job.Config.KeepStaging {
		if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
			config.Debug("[REPLACE] Warning: failed to drop staging table: %v", err)
		}
	}

	return nil
}
