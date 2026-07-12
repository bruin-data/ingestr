package strategy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
)

type AppendStrategy struct{}

func (s *AppendStrategy) Name() config.IncrementalStrategy {
	return config.StrategyAppend
}

func (s *AppendStrategy) Validate(cfg *config.IngestConfig) error {
	return nil
}

func (s *AppendStrategy) RequiresPrimaryKey() bool {
	return false
}

func (s *AppendStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *AppendStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	config.Debug("[APPEND] Writing to table: %s", job.Config.DestTable)

	// CDC change batches carry the otherwise staging-only _cdc_unchanged_cols
	// column; append lands batches directly, so the destination table must keep
	// it, and CDCMode relaxes NOT NULL since rows are change events.
	isCDC := hasCDCColumns(job.Schema)
	prepSchema := destination.DestinationTableSchema(job.Schema)
	if isCDC {
		prepSchema = job.Schema
	}
	if publisher, ok := job.Destination.(destination.AtomicSnapshotPublisher); ok &&
		job.CDCStateManager != nil && isCDC && !job.Config.Stream &&
		(job.Config.FullRefresh || strings.TrimSpace(job.Config.CDCResumeLSN) == "") {
		return s.executeAtomicSnapshot(ctx, job, publisher, prepSchema)
	}

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      prepSchema,
		DropFirst:   false,
		PrimaryKeys: job.Schema.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
		CDCMode:     isCDC,
	}); err != nil {
		return fmt.Errorf("failed to prepare table: %w", err)
	}
	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
			return fmt.Errorf("failed to bind CDC destination before append: %w", err)
		}
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
		CDCResumeLSN:                    job.Config.CDCResumeLSN,
		CDCResumeIncarnation:            job.Config.CDCResumeIncarnation,
		CDCResumeSchemaFingerprint:      job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix:                   job.Config.CDCSlotSuffix,
		CDCLegacySlotSuffix:             job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace:              isCDC && supportsCDCSnapshotReplace(job.Destination),
		CDCStableDataBatches:            isCDC && job.CDCStateManager != nil,
		FullRefresh:                     job.Config.FullRefresh,
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	writeOpts := destination.WriteOptions{
		Table:            job.Config.DestTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
		SkipCDCResume:    job.CDCStateManager != nil,
	}
	if job.CDCStateManager != nil {
		writeOpts.CDCExpectedIncarnation = job.CDCStateManager.BoundDestinationIncarnation(job.Config.SourceTable)
		if writeOpts.CDCExpectedIncarnation == "" {
			return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", job.Config.DestTable)
		}
	}
	var writeErr error
	if isCDC && job.CDCStateManager != nil {
		_, writeErr = writeDurableCDCAppendRecords(readCtx, job.Destination, records, writeOpts, job.Config.SourceTable)
	} else if isCDC {
		_, writeErr = destination.WriteWithTruncateBoundaries(readCtx, job.Destination, records, writeOpts)
	} else {
		writeErr = job.Destination.WriteParallel(readCtx, records, writeOpts)
	}
	if writeErr != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to write data: %w", writeErr)
	}

	return nil
}

func (s *AppendStrategy) executeAtomicSnapshot(
	ctx context.Context,
	job *IngestionJob,
	publisher destination.AtomicSnapshotPublisher,
	targetSchema *schema.TableSchema,
) error {
	existingSchema, err := job.Destination.GetTableSchema(ctx, job.Config.DestTable)
	if err != nil {
		return fmt.Errorf("failed to inspect destination table before atomic append snapshot: %w", err)
	}
	targetExisted := existingSchema != nil
	ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)
	if !targetExisted && ownershipToken == "" {
		return fmt.Errorf("destination %s cannot safely clean up a failed atomic append target", job.Destination.GetScheme())
	}
	cleanupNewTarget := !targetExisted && ownershipToken != ""
	defer func() {
		if cleanupNewTarget {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, job.Config.DestTable, true, ownershipToken)
		}
	}()
	if !targetExisted {
		if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
			Table: job.Config.DestTable, Schema: targetSchema, PrimaryKeys: job.Schema.PrimaryKeys,
			PartitionBy: job.Config.PartitionBy, ClusterBy: job.Config.ClusterBy, CDCMode: true,
			OwnershipToken: ownershipToken,
		}); err != nil {
			return fmt.Errorf("failed to prepare owned target for atomic append snapshot: %w", err)
		}
	}
	if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
		return fmt.Errorf("failed to bind CDC destination before atomic append snapshot: %w", err)
	}
	expectedIncarnation := job.CDCStateManager.BoundDestinationIncarnation(job.Config.SourceTable)
	if expectedIncarnation == "" {
		return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", job.Config.DestTable)
	}
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, source.ReadOptions{
		IncrementalKey: job.Config.IncrementalKey, IntervalStart: job.Config.IntervalStart, IntervalEnd: job.Config.IntervalEnd,
		ExtractPartitionBy: job.Config.ExtractPartitionBy, ExtractPartitionInterval: job.Config.ExtractPartitionInterval,
		ExtractPartitionNumericInterval: job.Config.ExtractPartitionNumericInterval, ExtractPartitionAuto: job.Config.ExtractPartitionAuto,
		PageSize: job.Config.PageSize, Limit: job.Config.SQLLimit, ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism: parallelism, Schema: job.SourceSchema, CDCResumeLSN: job.Config.CDCResumeLSN,
		CDCResumeIncarnation: job.Config.CDCResumeIncarnation, CDCResumeSchemaFingerprint: job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix: job.Config.CDCSlotSuffix, CDCLegacySlotSuffix: job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace: true, FullRefresh: job.Config.FullRefresh,
	})
	if err != nil {
		return fmt.Errorf("failed to get atomic append snapshot records: %w", err)
	}
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	if err := consumeAtomicSnapshotBoundary(readCtx, job, records); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return err
	}
	boundary := &atomicBatchSnapshotBoundary{}
	attemptID := uuid.NewString()
	opts := destination.AtomicSnapshotOptions{
		Table: job.Config.DestTable, Schema: job.Schema, TargetSchema: targetSchema,
		PrimaryKeys: job.Schema.PrimaryKeys, PartitionBy: job.Config.PartitionBy, ClusterBy: job.Config.ClusterBy,
		Parallelism: parallelism, AttemptID: attemptID, SkipCDCResume: true,
		CDCExpectedIncarnation: expectedIncarnation,
	}
	if err := publisher.BeginAtomicSnapshot(ctx, opts); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		abortAtomicAppendSnapshot(ctx, job.Destination, opts)
		return fmt.Errorf("failed to begin atomic append snapshot: %w", err)
	}
	records = observeAtomicBatchSnapshot(readCtx, records, boundary)
	publishAttempted := false
	defer func() {
		if !publishAttempted {
			abortAtomicAppendSnapshot(ctx, job.Destination, opts)
		}
	}()
	if err := publisher.WriteAtomicSnapshot(readCtx, records, destination.WriteOptions{
		Table: job.Config.DestTable, Schema: job.Schema, PrimaryKeys: job.Schema.PrimaryKeys,
		Parallelism: parallelism, AtomicSnapshotAttemptID: attemptID, SkipCDCResume: true,
		CDCExpectedIncarnation: expectedIncarnation, PreStaged: job.PreStaged,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to stage atomic append snapshot: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	opts.CommitToken, opts.CDCResumeLSN = boundary.values()
	publishAttempted = true
	cleanupNewTarget = false
	if err := publisher.PublishAtomicSnapshot(ctx, opts); err != nil {
		return fmt.Errorf("failed to publish atomic append snapshot: %w", err)
	}
	if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
		return fmt.Errorf("failed to revalidate published append snapshot: %w", err)
	}
	return nil
}

func abortAtomicAppendSnapshot(ctx context.Context, dest destination.Destination, opts destination.AtomicSnapshotOptions) {
	aborter, ok := dest.(destination.AtomicSnapshotAborter)
	if !ok {
		return
	}
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = aborter.AbortAtomicSnapshot(abortCtx, opts)
}

// ExecuteMultiTable implements multi-table append strategy for CDC sources.
func (s *AppendStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}
	if publisher, canPublish := job.Destination.(destination.AtomicSnapshotPublisher); canPublish && job.CDCStateManager != nil && !job.Config.Stream {
		needsSnapshot := job.Config.FullRefresh
		for _, table := range job.Tables {
			if hasCDCColumns(table.Schema) && strings.TrimSpace(job.CDCResumeLSNs[table.Name]) == "" {
				needsSnapshot = true
			}
		}
		if needsSnapshot {
			direct, canWriteDirect := job.Destination.(destination.DirectMergeWriter)
			if !canWriteDirect {
				return fmt.Errorf("destination %s cannot safely combine atomic append snapshots with resumed multi-table writes", job.Destination.GetScheme())
			}
			appendJob := MultiTableIngestionJob{
				Config:             job.Config,
				Source:             job.Source,
				Destination:        job.Destination,
				Tables:             append([]source.SourceTableInfo(nil), job.Tables...),
				TableDestNames:     job.TableDestNames,
				Tracker:            job.Tracker,
				CDCResumeLSNs:      job.CDCResumeLSNs,
				EvolutionPlans:     job.EvolutionPlans,
				CDCStateManager:    job.CDCStateManager,
				WhitespaceTrimmer:  job.WhitespaceTrimmer,
				LoadTimestamp:      job.LoadTimestamp,
				ColumnRenamers:     job.ColumnRenamers,
				NormalizeTableInfo: job.NormalizeTableInfo,
			}
			for i := range appendJob.Tables {
				appendJob.Tables[i].PrimaryKeys = nil
			}
			return (&MergeStrategy{}).executeAtomicMultiTableBatch(ctx, &appendJob, publisher, direct)
		}
	}

	config.Debug("[STRATEGY] Multi-table append with %d tables", len(job.Tables))

	tableConfigs := make(map[string]destination.TableWriteConfig)
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables))

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)

			if err := job.ApplyEvolutionFor(ctx, ti.Name); err != nil {
				errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
				return
			}

			// See Execute: CDC change batches land directly and carry the
			// staging-only _cdc_unchanged_cols column.
			writeSchema := job.WriteSchemaFor(ti.Name, ti.Schema)
			isCDC := hasCDCColumns(writeSchema)
			prepSchema := destination.DestinationTableSchema(writeSchema)
			if isCDC {
				prepSchema = writeSchema
			}

			if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
				Table:       destTable,
				Schema:      prepSchema,
				DropFirst:   false,
				PrimaryKeys: ti.PrimaryKeys,
				CDCMode:     isCDC,
			}); err != nil {
				errChan <- fmt.Errorf("failed to prepare table %s: %w", ti.Name, err)
				return
			}
			if job.CDCStateManager != nil {
				if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
					errChan <- fmt.Errorf("failed to bind CDC destination table %s: %w", ti.Name, err)
					return
				}
			}

			expectedIncarnation := ""
			if job.CDCStateManager != nil {
				expectedIncarnation = job.CDCStateManager.BoundDestinationIncarnation(ti.Name)
				if expectedIncarnation == "" {
					errChan <- fmt.Errorf("managed CDC destination table %s has no bound physical incarnation", ti.Name)
					return
				}
			}
			mu.Lock()
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:              destTable,
				Schema:                 writeSchema,
				PrimaryKeys:            ti.PrimaryKeys,
				CDCMode:                isCDC,
				SkipCDCResume:          job.CDCStateManager != nil,
				CDCExpectedIncarnation: expectedIncarnation,
			}
			mu.Unlock()
		}(tableInfo)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	anyTableHasCDC := false
	for _, table := range job.Tables {
		if hasCDCColumns(table.Schema) {
			anyTableHasCDC = true
			break
		}
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	resumeIncarnations, resumeSchemas := cdcResumeMetadata(job.Tables)
	records, err := job.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:          parallelism,
			PageSize:             job.Config.PageSize,
			Limit:                job.Config.SQLLimit,
			CDCSlotSuffix:        job.Config.CDCSlotSuffix,
			CDCLegacySlotSuffix:  job.Config.CDCLegacySlotSuffix,
			CDCSnapshotReplace:   anyTableHasCDC && supportsCDCSnapshotReplace(job.Destination),
			CDCStableDataBatches: anyTableHasCDC && job.CDCStateManager != nil,
			FullRefresh:          job.Config.FullRefresh,
		},
		CDCResumeLSNs:               job.CDCResumeLSNs,
		CDCResumeIncarnations:       resumeIncarnations,
		CDCResumeSchemaFingerprints: resumeSchemas,
	})
	if err != nil {
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	records = applyMultiTableSnapshotInvalidations(readCtx, job, records)

	writeOptions := destination.MultiTableWriteOptions{
		TableConfigs:       tableConfigs,
		Parallelism:        parallelism,
		StagingBucket:      job.Config.StagingBucket,
		LoaderFileSize:     job.Config.LoaderFileSize,
		LoaderFileFormat:   job.Config.LoaderFileFormat,
		CancelSource:       cancelRead,
		CancelDrainTimeout: canceledSourceDrainTimeout,
	}
	if job.CDCStateManager != nil {
		writeOptions.TableWriter = func(
			writeCtx context.Context,
			sourceTable string,
			tableRecords <-chan source.RecordBatchResult,
			opts destination.WriteOptions,
		) (bool, error) {
			if !tableConfigs[sourceTable].CDCMode {
				return false, job.Destination.WriteParallel(writeCtx, tableRecords, opts)
			}
			return writeDurableCDCAppendRecords(writeCtx, job.Destination, tableRecords, opts, sourceTable)
		}
	}
	if err := multitable.Write(ctx, job.Destination, records, writeOptions); err != nil {
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}

	return nil
}

func writeDurableCDCAppendRecords(
	ctx context.Context,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
	sourceTable string,
) (bool, error) {
	truncated := false
	for {
		var result source.RecordBatchResult
		var ok bool
		select {
		case <-ctx.Done():
			return truncated, ctx.Err()
		case result, ok = <-records:
			if !ok {
				return truncated, nil
			}
		}
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return truncated, result.Err
		}
		if result.SnapshotInvalidation != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return truncated, fmt.Errorf("unexpected snapshot invalidation for %s after CDC append routing", sourceTable)
		}
		if result.Truncate {
			if result.Batch != nil {
				result.Batch.Release()
			}
			if !result.CDCWALTruncate {
				return truncated, fmt.Errorf("managed CDC append replacement snapshots for table %s require atomic snapshot publication", opts.Table)
			}
			if err := destination.ApplyCDCTruncateIfIncarnation(ctx, dest, opts.Table, opts.CDCExpectedIncarnation); err != nil {
				return truncated, err
			}
			truncated = true
			continue
		}
		if result.TableName == "" {
			result.TableName = sourceTable
		}
		batch := make(chan source.RecordBatchResult, 1)
		batch <- result
		close(batch)
		if err := writeDurableKeylessCDCRecords(ctx, dest, batch, opts); err != nil {
			drainAndRelease(batch)
			return truncated, err
		}
	}
}
