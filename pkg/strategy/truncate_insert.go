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

// TruncateInsertStrategy empties the destination table in place and writes new
// rows into it. Unlike ReplaceStrategy (which drops + recreates via a staging
// swap), this preserves the table definition and any dependent objects
// (views, grants, foreign keys).
//
// When primary keys are configured, rows are first written to a staging table
// and then deduplicated by PK into the truncated target via the destination's
// existing merge SQL. This tolerates sources that emit the same PK more than
// once (e.g., page-based pagination over a live table). Without PKs, dedup is
// not possible and rows are written directly into the truncated target.
//
// Destinations implementing AtomicTruncateInsertWriter replace schema and rows
// together. Other destinations retain the traditional truncate-then-write
// behavior, where readers may observe an empty table and failures cannot roll
// back the truncate.
//
// Additional tradeoffs:
//   - Schema evolution is applied in place before rows are replaced, preserving
//     dependent objects while allowing supported column additions and widening.
//   - ClickHouse caveat: ClickHouse's merge implementation relies on
//     ReplacingMergeTree semantics for dedup rather than pre-filtering, so
//     duplicate PKs in staging will only be collapsed if the target table is
//     a ReplacingMergeTree.
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
	if atomicWriter, ok := job.Destination.(destination.AtomicTruncateInsertWriter); ok {
		return s.executeAtomic(ctx, job, atomicWriter)
	}
	truncator, ok := job.Destination.(destination.TruncateCapable)
	if !ok {
		return fmt.Errorf("destination does not support truncate+insert strategy; use replace instead")
	}

	if len(job.Config.PrimaryKeys) > 0 {
		return s.executeWithStaging(ctx, job, truncator)
	}
	return s.executeDirect(ctx, job, truncator)
}

func (s *TruncateInsertStrategy) executeAtomic(ctx context.Context, job *IngestionJob, writer destination.AtomicTruncateInsertWriter) error {
	targetTable := job.Config.DestTable
	existingSchema, err := job.Destination.GetTableSchema(ctx, targetTable)
	if err != nil {
		return fmt.Errorf("failed to inspect destination table before truncate+insert: %w", err)
	}
	targetExisted := existingSchema != nil
	ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:                  targetTable,
		Schema:                 destination.DestinationTableSchema(job.Schema),
		DropFirst:              false,
		PrimaryKeys:            job.Config.PrimaryKeys,
		PartitionBy:            job.Config.PartitionBy,
		ClusterBy:              job.Config.ClusterBy,
		PreserveExistingLayout: true,
		OwnershipToken:         ownershipToken,
	}); err != nil {
		if ownershipToken != "" {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, true, ownershipToken)
		}
		return fmt.Errorf("failed to prepare table: %w", err)
	}

	atomicEvolution, ok := job.Destination.(destination.AtomicTruncateInsertSchemaEvolver)
	if !ok || !atomicEvolution.EvolvesTruncateInsertSchemaAtomically() {
		if err := job.ApplyEvolution(ctx); err != nil {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
			return fmt.Errorf("failed to apply schema evolution: %w", err)
		}
	}
	expectedIncarnation := ""
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, targetTable); err != nil {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
			return fmt.Errorf("failed to bind CDC destination before truncate+insert extraction: %w", err)
		}
		expectedIncarnation = job.CDCStateManager.BoundDestinationIncarnation(job.Config.SourceTable)
		if expectedIncarnation == "" {
			return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", targetTable)
		}
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, source.ReadOptions{
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
	})
	if err != nil {
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to get records: %w", err)
	}
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := writer.TruncateInsertRecords(readCtx, records, destination.WriteOptions{
		Table:                  targetTable,
		Schema:                 job.Schema,
		PrimaryKeys:            job.Config.PrimaryKeys,
		Parallelism:            parallelism,
		StagingBucket:          job.Config.StagingBucket,
		LoaderFileSize:         job.Config.LoaderFileSize,
		LoaderFileFormat:       job.Config.LoaderFileFormat,
		PreStaged:              job.PreStaged,
		DeduplicatePrimaryKeys: len(job.Config.PrimaryKeys) > 0,
		IncrementalKey:         mergeIncrementalKeyForSchema(job.Schema, job.Config.IncrementalKey),
		SkipCDCResume:          job.CDCStateManager != nil,
		CDCExpectedIncarnation: expectedIncarnation,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to atomically truncate and insert data: %w", err)
	}
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, targetTable); err != nil {
			return fmt.Errorf("failed to revalidate CDC destination after truncate+insert: %w", err)
		}
	}
	return nil
}

func (s *TruncateInsertStrategy) executeDirect(ctx context.Context, job *IngestionJob, truncator destination.TruncateCapable) error {
	targetTable := job.Config.DestTable
	config.Debug("[TRUNCATE+INSERT] Target table: %s (direct path, no PKs)", targetTable)
	existingSchema, err := job.Destination.GetTableSchema(ctx, targetTable)
	if err != nil {
		return fmt.Errorf("failed to inspect destination table before truncate+insert: %w", err)
	}
	targetExisted := existingSchema != nil
	ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:          targetTable,
		Schema:         destination.DestinationTableSchema(job.Schema),
		DropFirst:      false,
		PrimaryKeys:    job.Config.PrimaryKeys,
		PartitionBy:    job.Config.PartitionBy,
		ClusterBy:      job.Config.ClusterBy,
		OwnershipToken: ownershipToken,
	}); err != nil {
		if ownershipToken != "" {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, true, ownershipToken)
		}
		return fmt.Errorf("failed to prepare table: %w", err)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to apply schema evolution: %w", err)
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

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, readOpts)
	if err != nil {
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := truncator.TruncateTable(ctx, targetTable); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to truncate table: %w", err)
	}

	if err := job.Destination.WriteParallel(readCtx, records, destination.WriteOptions{
		Table:            targetTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

func (s *TruncateInsertStrategy) executeWithStaging(ctx context.Context, job *IngestionJob, truncator destination.TruncateCapable) error {
	if !job.Destination.SupportsMergeStrategy() {
		return fmt.Errorf("destination does not support deduplicated truncate+insert (merge not supported); use replace instead")
	}

	targetTable := job.Config.DestTable
	stagingTable := managedStagingTableName(job.Destination, targetTable, "ti", job.Config.StagingDataset)
	output.Statusf("[TRUNCATE+INSERT] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)
	existingSchema, err := job.Destination.GetTableSchema(ctx, targetTable)
	if err != nil {
		return fmt.Errorf("failed to inspect destination table before truncate+insert: %w", err)
	}
	targetExisted := existingSchema != nil
	ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:          targetTable,
		Schema:         destination.DestinationTableSchema(job.Schema),
		DropFirst:      false,
		PrimaryKeys:    job.Config.PrimaryKeys,
		PartitionBy:    job.Config.PartitionBy,
		ClusterBy:      job.Config.ClusterBy,
		OwnershipToken: ownershipToken,
	}); err != nil {
		if ownershipToken != "" {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, true, ownershipToken)
		}
		return fmt.Errorf("failed to prepare destination table: %w", err)
	}
	if !job.Config.KeepStaging {
		defer dropManagedStagingDetached(ctx, job.Destination, stagingTable, "TRUNCATE+INSERT")
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
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
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

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, readOpts)
	if err != nil {
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := job.Destination.WriteParallel(readCtx, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to write to staging: %w", err)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}

	if err := truncator.TruncateTable(ctx, targetTable); err != nil {
		return fmt.Errorf("failed to truncate target: %w", err)
	}

	config.Debug("[TRUNCATE+INSERT] Executing deduplicated insert via merge from staging")
	if err := job.Destination.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   stagingTable,
		TargetTable:    targetTable,
		PrimaryKeys:    job.Config.PrimaryKeys,
		Columns:        destination.DestinationColumns(job.Schema.ColumnNames()),
		IncrementalKey: mergeIncrementalKeyForSchema(job.Schema, job.Config.IncrementalKey),
		Schema:         job.Schema,
	}); err != nil {
		return fmt.Errorf("failed to insert from staging: %w", err)
	}

	return nil
}
