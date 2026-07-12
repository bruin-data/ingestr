package strategy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type MergeStrategy struct{}

var canceledSourceDrainTimeout = time.Second

// mergeTableParams describes one dest/staging table pair for a merge.
type mergeTableParams struct {
	DestTable      string
	StagingTable   string
	Schema         *schema.TableSchema
	PrimaryKeys    []string
	PartitionBy    string
	ClusterBy      []string
	IsCDC          bool
	OwnershipToken string
}

// prepareMergeTables ensures the destination table exists (without dropping it)
// and creates a fresh staging table for it.
func prepareMergeTables(ctx context.Context, dest destination.Destination, p mergeTableParams) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := prepareMergeTarget(ctx, dest, p); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        p.StagingTable,
		Schema:       p.Schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		CDCMode:      p.IsCDC, // Allow NULLs for CDC deletes in staging
		PartitionBy:  p.PartitionBy,
		ClusterBy:    p.ClusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table %s: %w", p.StagingTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	return nil
}

func prepareMergeTarget(ctx context.Context, dest destination.Destination, p mergeTableParams) error {
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:          p.DestTable,
		Schema:         destination.DestinationTableSchema(p.Schema),
		DropFirst:      false,
		PrimaryKeys:    p.PrimaryKeys,
		PartitionBy:    p.PartitionBy,
		ClusterBy:      p.ClusterBy,
		OwnershipToken: p.OwnershipToken,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table %s: %w", p.DestTable, err)
	}
	return nil
}

func mergeStagingIntoWithCommit(ctx context.Context, dest destination.Destination, stagingTable, targetTable string, primaryKeys []string, tableSchema *schema.TableSchema, incrementalKey, incrementalPredicate string, commitToken source.DurableID, cdcResumeLSN string, skipCDCResume bool, expectedIncarnation string) error {
	return dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:           stagingTable,
		TargetTable:            targetTable,
		PrimaryKeys:            primaryKeys,
		Columns:                destination.MergeColumnsFor(dest, tableSchema.ColumnNames()),
		IncrementalKey:         mergeIncrementalKeyForSchema(tableSchema, incrementalKey),
		IncrementalPredicate:   incrementalPredicate,
		Schema:                 tableSchema,
		CommitToken:            commitToken,
		CDCResumeLSN:           cdcResumeLSN,
		SkipCDCResume:          skipCDCResume,
		CDCExpectedIncarnation: expectedIncarnation,
	})
}

func mergeIncrementalKeyForSchema(tableSchema *schema.TableSchema, incrementalKey string) string {
	if incrementalKey == "" || tableSchema == nil {
		return ""
	}
	for _, col := range tableSchema.Columns {
		if col.Name == incrementalKey {
			return col.Name
		}
	}
	for _, col := range tableSchema.Columns {
		if strings.EqualFold(col.Name, incrementalKey) {
			return col.Name
		}
	}
	return ""
}

// isAppendOnlyCDCTable reports whether a CDC table must be ingested as an
// append-only change log: it has no usable row identity (no primary key and no
// replica identity index on the source), so a merge has nothing to match on.
// The source emits its updates as delete+insert pairs (see postgres_cdc
// expandUpdates), making the landed log a self-contained retract stream the
// user applies downstream.
func isAppendOnlyCDCTable(ti source.SourceTableInfo) bool {
	return len(ti.PrimaryKeys) == 0 && hasCDCColumns(ti.Schema)
}

// prepareAppendOnlyCDCTable creates the destination table a keyless CDC table's
// change log lands in directly (no staging, no merge). The full schema is kept
// — including the otherwise staging-only _cdc_unchanged_cols — because raw
// change batches carry it, and CDCMode relaxes NOT NULL since rows are change
// events rather than complete entities.
func prepareAppendOnlyCDCTable(ctx context.Context, dest destination.Destination, table string, tableSchema *schema.TableSchema) error {
	return prepareAppendOnlyCDCTableOwned(ctx, dest, table, tableSchema, "")
}

func prepareAppendOnlyCDCTableOwned(ctx context.Context, dest destination.Destination, table string, tableSchema *schema.TableSchema, ownershipToken string) error {
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:          table,
		Schema:         tableSchema,
		DropFirst:      false,
		CDCMode:        true,
		OwnershipToken: ownershipToken,
	}); err != nil {
		return fmt.Errorf("failed to prepare append-only change-log table %s: %w", table, err)
	}
	return nil
}

// warnIfCDCMergeUnsupported prints a warning when CDC data is headed at a
// destination that can't process deletes during merge.
func warnIfCDCMergeUnsupported(dest destination.Destination) {
	if cdcAware, ok := dest.(destination.CDCMergeAware); !ok || !cdcAware.SupportsCDCMerge() {
		output.Warnf("Warning: CDC data detected but the destination does not support CDC-aware merge; deleted rows will be inserted as regular data with _cdc_deleted=true instead of being processed as deletes\n")
	}
}

func supportsCDCSnapshotReplace(dest destination.Destination) bool {
	if _, ok := dest.(destination.AtomicSnapshotPublisher); ok {
		return true
	}
	_, ok := dest.(destination.TruncateCapable)
	return ok
}

func (s *MergeStrategy) Name() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *MergeStrategy) Validate(cfg *config.IngestConfig) error {
	if len(cfg.PrimaryKeys) == 0 {
		return fmt.Errorf("merge strategy requires at least one primary_key")
	}
	return nil
}

func (s *MergeStrategy) RequiresPrimaryKey() bool {
	return true
}

func (s *MergeStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *MergeStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	// Generate staging table name
	stagingTable := managedStagingTableName(job.Destination, job.Config.DestTable, "merge", job.Config.StagingDataset)
	isCDC := hasCDCColumns(job.Schema)
	if isCDC {
		warnIfCDCMergeUnsupported(job.Destination)
	}
	if isCDC && !job.Config.Stream && (job.Config.FullRefresh || strings.TrimSpace(job.Config.CDCResumeLSN) == "") {
		if publisher, ok := job.Destination.(destination.AtomicSnapshotPublisher); ok && supportsCDCSnapshotReplace(job.Destination) {
			return s.executeAtomicBatchSnapshot(ctx, job, publisher)
		}
	}

	params := mergeTableParams{
		DestTable:    job.Config.DestTable,
		StagingTable: stagingTable,
		Schema:       job.Schema,
		PrimaryKeys:  job.Config.PrimaryKeys,
		PartitionBy:  job.Config.PartitionBy,
		ClusterBy:    job.Config.ClusterBy,
		IsCDC:        isCDC,
	}
	direct, directMerge := job.Destination.(destination.DirectMergeWriter)
	directMerge = directMerge && !isCDC
	if !directMerge && !job.Config.KeepStaging {
		defer dropMergeStagingDetached(ctx, job.Destination, stagingTable)
	}
	var prepareErr error
	if directMerge {
		prepareErr = prepareMergeTarget(ctx, job.Destination, params)
	} else {
		output.Statusf("[MERGE] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)
		prepareErr = prepareMergeTables(ctx, job.Destination, params)
	}
	if prepareErr != nil {
		return prepareErr
	}
	if directMerge && job.CDCStateManager == nil {
		if err := job.ApplyEvolution(ctx); err != nil {
			return fmt.Errorf("failed to apply schema evolution: %w", err)
		}
	}
	expectedIncarnation := ""
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
			return fmt.Errorf("failed to bind CDC destination before merge: %w", err)
		}
		var err error
		expectedIncarnation, err = boundDestinationIncarnation(job.CDCStateManager, job.Config.SourceTable, job.Config.DestTable)
		if err != nil {
			return err
		}
		if directMerge {
			if err := job.ApplyEvolutionIfIncarnation(ctx, expectedIncarnation); err != nil {
				return fmt.Errorf("failed to apply schema evolution: %w", err)
			}
		}
	}

	// Read from source
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
		CDCResumeLSN:                    job.Config.CDCResumeLSN, // For CDC incremental resume
		CDCResumeIncarnation:            job.Config.CDCResumeIncarnation,
		CDCResumeSchemaFingerprint:      job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix:                   job.Config.CDCSlotSuffix, // Destination-aware slot suffix
		CDCLegacySlotSuffix:             job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace:              isCDC && supportsCDCSnapshotReplace(job.Destination),
		FullRefresh:                     job.Config.FullRefresh,
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	if directMerge {
		if err := direct.MergeRecords(readCtx, records, destination.WriteOptions{
			Table:                  job.Config.DestTable,
			Schema:                 job.Schema,
			Parallelism:            job.Config.EffectiveDestinationParallelism(),
			StagingBucket:          job.Config.StagingBucket,
			LoaderFileSize:         job.Config.LoaderFileSize,
			LoaderFileFormat:       job.Config.LoaderFileFormat,
			CDCExpectedIncarnation: expectedIncarnation,
		}, destination.MergeOptions{
			TargetTable:            job.Config.DestTable,
			PrimaryKeys:            job.Config.PrimaryKeys,
			Columns:                destination.MergeColumnsFor(job.Destination, job.Schema.ColumnNames()),
			IncrementalKey:         mergeIncrementalKeyForSchema(job.Schema, job.Config.IncrementalKey),
			IncrementalPredicate:   job.Config.IncrementalPredicate,
			Schema:                 job.Schema,
			Parallelism:            job.Config.EffectiveDestinationParallelism(),
			SkipCDCResume:          job.CDCStateManager != nil,
			CDCExpectedIncarnation: expectedIncarnation,
		}); err != nil {
			cancelRead()
			<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
			return fmt.Errorf("failed to merge records directly: %w", err)
		}
		return nil
	}

	writeOpts := destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      job.Config.EffectiveDestinationParallelism(),
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
		SkipCDCResume:    job.CDCStateManager != nil,
	}
	var sourceTruncated bool
	if isCDC {
		// Source TRUNCATE controls split CDC into ordered segments and clear
		// earlier staged changes.
		sourceTruncated, err = destination.WriteWithTruncateBoundariesAfterCancel(readCtx, job.Destination, records, writeOpts, cancelRead)
	} else {
		err = job.Destination.WriteParallel(readCtx, records, writeOpts)
	}
	if err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to write to staging: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	if err := job.ApplyEvolutionIfIncarnation(ctx, expectedIncarnation); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	// Perform merge: UPDATE existing + INSERT new
	// Note: We only use source columns here. Destination-only columns (removed columns)
	// will naturally receive NULL for new rows and remain unchanged for existing rows.
	config.Debug("[MERGE] Executing merge operation")
	if isCDC && sourceTruncated {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		var truncateErr error
		if job.CDCStateManager != nil {
			truncateErr = destination.ApplyCDCTruncateIfIncarnation(
				ctx,
				job.Destination,
				job.Config.DestTable,
				job.CDCStateManager.BoundDestinationIncarnation(job.Config.SourceTable),
			)
		} else {
			truncateErr = destination.ApplyCDCTruncate(ctx, job.Destination, job.Config.DestTable)
		}
		if truncateErr != nil {
			return truncateErr
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := mergeStagingIntoWithCommit(ctx, job.Destination, stagingTable, job.Config.DestTable, job.Config.PrimaryKeys, job.Schema, job.Config.IncrementalKey, job.Config.IncrementalPredicate,
		"", "", job.CDCStateManager != nil, expectedIncarnation); err != nil {
		return fmt.Errorf("failed to merge data: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	// The deferred cleanup uses a detached, lease-fenced context so cancellation
	// cannot strand a managed staging table.
	return nil
}

func (s *MergeStrategy) executeAtomicBatchSnapshot(
	ctx context.Context,
	job *IngestionJob,
	publisher destination.AtomicSnapshotPublisher,
) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if _, recovered, err := recoverManagedAtomicSnapshotBeforeRead(
		ctx, job.Destination, publisher, job.CDCStateManager, job.Config.SourceTable, job.Config.DestTable,
	); err != nil {
		return err
	} else if recovered {
		return nil
	}
	existingSchema, err := job.Destination.GetTableSchema(ctx, job.Config.DestTable)
	if err != nil {
		return fmt.Errorf("failed to inspect destination table before atomic batch snapshot: %w", err)
	}
	targetExisted := existingSchema != nil
	ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)
	cleanupNewTarget := !targetExisted && ownershipToken != ""
	defer func() {
		if cleanupNewTarget {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, job.Config.DestTable, true, ownershipToken)
		}
	}()
	if err := prepareMergeTarget(ctx, job.Destination, mergeTableParams{
		DestTable: job.Config.DestTable, Schema: job.Schema, PrimaryKeys: job.Config.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy, ClusterBy: job.Config.ClusterBy, IsCDC: hasCDCColumns(job.Schema),
		OwnershipToken: ownershipToken,
	}); err != nil {
		return err
	}
	expectedIncarnation := ""
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, job.Config.DestTable); err != nil {
			return fmt.Errorf("failed to bind CDC destination before atomic snapshot: %w", err)
		}
		expectedIncarnation = job.CDCStateManager.BoundDestinationIncarnation(job.Config.SourceTable)
		if expectedIncarnation == "" {
			return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", job.Config.DestTable)
		}
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, source.ReadOptions{
		IncrementalKey: job.Config.IncrementalKey, IntervalStart: job.Config.IntervalStart, IntervalEnd: job.Config.IntervalEnd,
		PageSize: job.Config.PageSize, Limit: job.Config.SQLLimit, ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism: parallelism, Schema: job.SourceSchema, CDCResumeLSN: job.Config.CDCResumeLSN,
		CDCResumeIncarnation: job.Config.CDCResumeIncarnation, CDCResumeSchemaFingerprint: job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix: job.Config.CDCSlotSuffix, CDCLegacySlotSuffix: job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace: true, FullRefresh: job.Config.FullRefresh,
	})
	if err != nil {
		return fmt.Errorf("failed to get atomic snapshot records: %w", err)
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

	attemptID, err := atomicSnapshotAttemptID(ctx, job.CDCStateManager, job.Config.SourceTable, job.Config.DestTable)
	if err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to establish durable atomic batch snapshot attempt: %w", err)
	}
	targetSchema := destination.DestinationTableSchema(job.Schema)
	if job.EvolutionPlan != nil && job.EvolutionPlan.FinalSchema != nil {
		targetSchema = job.EvolutionPlan.FinalSchema
	}
	opts := destination.AtomicSnapshotOptions{
		Table: job.Config.DestTable, Schema: job.Schema, TargetSchema: targetSchema,
		PrimaryKeys: job.Config.PrimaryKeys, PartitionBy: job.Config.PartitionBy, ClusterBy: job.Config.ClusterBy,
		Parallelism: job.Config.EffectiveDestinationParallelism(), AttemptID: attemptID, SkipCDCResume: job.CDCStateManager != nil,
		CDCExpectedIncarnation: expectedIncarnation,
	}
	if err := publisher.BeginAtomicSnapshot(ctx, opts); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		if aborter, ok := job.Destination.(destination.AtomicSnapshotAborter); ok {
			abortBase, detachCancel := source.WithoutCancelWithConnectorLease(ctx)
			defer detachCancel()
			abortCtx, cancel := context.WithTimeout(abortBase, 30*time.Second)
			defer cancel()
			_ = aborter.AbortAtomicSnapshot(abortCtx, opts)
		}
		return fmt.Errorf("failed to begin atomic batch snapshot: %w", err)
	}
	records = observeAtomicBatchSnapshot(readCtx, records, boundary)
	publishAttempted := false
	sealAttempted := false
	defer func() {
		if publishAttempted || sealAttempted {
			return
		}
		if aborter, ok := job.Destination.(destination.AtomicSnapshotAborter); ok {
			abortBase, detachCancel := source.WithoutCancelWithConnectorLease(ctx)
			defer detachCancel()
			abortCtx, cancel := context.WithTimeout(abortBase, 30*time.Second)
			defer cancel()
			_ = aborter.AbortAtomicSnapshot(abortCtx, opts)
		}
	}()
	if err := publisher.WriteAtomicSnapshot(readCtx, records, destination.WriteOptions{
		Table: job.Config.DestTable, Schema: job.Schema, PrimaryKeys: job.Config.PrimaryKeys,
		Parallelism: job.Config.EffectiveDestinationParallelism(), AtomicSnapshotAttemptID: attemptID, SkipCDCResume: job.CDCStateManager != nil,
		CDCExpectedIncarnation: expectedIncarnation,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to stage atomic batch snapshot: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	opts.CommitToken = atomicSnapshotCommitID(opts.AttemptID)
	opts.CDCResumeLSN = boundary.positionValue()
	if job.CDCStateManager != nil {
		sealAttempted = true
		cleanupNewTarget = false
		if err := job.CDCStateManager.SealAtomicSnapshotAttempt(
			ctx, job.Config.SourceTable, job.Config.DestTable, attemptID, opts.CDCResumeLSN, expectedIncarnation,
		); err != nil {
			return fmt.Errorf("failed to seal durable atomic batch snapshot attempt: %w", err)
		}
	}
	publishAttempted = true
	cleanupNewTarget = false
	publishedIncarnation, err := publisher.PublishAtomicSnapshot(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to publish atomic batch snapshot: %w", err)
	}
	if job.CDCStateManager != nil {
		if publishedIncarnation == "" {
			return fmt.Errorf("atomic batch publication returned no physical incarnation")
		}
		if err := job.CDCStateManager.BindPublishedDestinationIncarnation(
			ctx, job.Config.SourceTable, job.Config.DestTable, expectedIncarnation, publishedIncarnation,
		); err != nil {
			return fmt.Errorf("failed to bind published CDC destination: %w", err)
		}
		if err := completeAtomicSnapshotAttempt(
			ctx, job.CDCStateManager, job.Config.SourceTable, job.Config.DestTable, attemptID,
		); err != nil {
			return fmt.Errorf("failed to complete durable atomic batch snapshot attempt: %w", err)
		}
	}
	return nil
}

func consumeAtomicSnapshotBoundary(ctx context.Context, job *IngestionJob, records <-chan source.RecordBatchResult) error {
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case result, ok := <-records:
			if !ok {
				return fmt.Errorf("CDC snapshot ended before its replacement boundary")
			}
			if result.Err != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				return result.Err
			}
			if result.SnapshotInvalidation != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				if job.CDCStateManager == nil {
					return fmt.Errorf("source requested snapshot invalidation without destination-managed CDC state")
				}
				if err := job.CDCStateManager.InvalidateSnapshotState(
					ctx, job.Config.SourceTable, job.Config.DestTable,
					result.SnapshotInvalidation.Incarnation, result.SnapshotInvalidation.SchemaFingerprint,
				); err != nil {
					return err
				}
				continue
			}
			if result.Truncate && !result.CDCWALTruncate {
				if result.Batch != nil {
					result.Batch.Release()
				}
				return nil
			}
			if result.Batch != nil {
				result.Batch.Release()
			}
			return fmt.Errorf("CDC snapshot did not begin with a replacement boundary")
		}
	}
}

// ExecuteMultiTable implements multi-table merge strategy for CDC sources.
func (s *MergeStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}

	config.Debug("[STRATEGY] Multi-table merge with %d tables", len(job.Tables))

	anyTableHasCDC := false
	managedAppendOnlyTables := make(map[string]struct{})
	for _, tableInfo := range job.Tables {
		if hasCDCColumns(tableInfo.Schema) {
			anyTableHasCDC = true
		}
		if job.CDCStateManager != nil && isAppendOnlyCDCTable(tableInfo) {
			managedAppendOnlyTables[tableInfo.Name] = struct{}{}
		}
	}
	if anyTableHasCDC {
		warnIfCDCMergeUnsupported(job.Destination)
	}
	if anyTableHasCDC && !job.Config.Stream {
		publisher, canPublish := job.Destination.(destination.AtomicSnapshotPublisher)
		direct, canMergeDirectly := job.Destination.(destination.DirectMergeWriter)
		if canPublish && canMergeDirectly {
			return s.executeAtomicMultiTableBatch(ctx, job, publisher, direct)
		}
	}
	if len(managedAppendOnlyTables) > 0 {
		if err := requireIdempotentCommitTokenWrites(job.Destination, "managed keyless CDC merge"); err != nil {
			return err
		}
	}

	stagingTables := make(map[string]string)
	tableConfigs := make(map[string]destination.TableWriteConfig)
	targetIncarnations := make(map[string]string)
	direct, directMerge := job.Destination.(destination.DirectMergeWriter)
	directMerge = directMerge && !anyTableHasCDC
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables)*2)

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)
			writeSchema := job.WriteSchemaFor(ti.Name, ti.Schema)

			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				errChan <- err
				return
			}
			if job.CDCStateManager == nil {
				if err := job.ApplyEvolutionFor(ctx, ti.Name); err != nil {
					errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
					return
				}
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				errChan <- err
				return
			}

			if isAppendOnlyCDCTable(ti) {
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					errChan <- err
					return
				}
				if err := prepareAppendOnlyCDCTable(ctx, job.Destination, destTable, writeSchema); err != nil {
					errChan <- err
					return
				}
				if job.CDCStateManager != nil {
					if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
						errChan <- fmt.Errorf("failed to bind CDC destination table %s: %w", ti.Name, err)
						return
					}
				}
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					errChan <- err
					return
				}
				expectedIncarnation := ""
				if job.CDCStateManager != nil {
					var err error
					expectedIncarnation, err = boundDestinationIncarnation(job.CDCStateManager, ti.Name, destTable)
					if err != nil {
						errChan <- err
						return
					}
					if err := job.ApplyEvolutionForIfIncarnation(ctx, ti.Name, expectedIncarnation); err != nil {
						errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
						return
					}
				}
				mu.Lock()
				targetIncarnations[ti.Name] = expectedIncarnation
				tableConfigs[ti.Name] = destination.TableWriteConfig{
					DestTable:              destTable,
					Schema:                 writeSchema,
					CDCMode:                true,
					SkipCDCResume:          job.CDCStateManager != nil,
					CDCExpectedIncarnation: expectedIncarnation,
				}
				mu.Unlock()
				return
			}
			if directMerge {
				if err := prepareMergeTarget(ctx, job.Destination, mergeTableParams{
					DestTable:   destTable,
					Schema:      writeSchema,
					PrimaryKeys: ti.PrimaryKeys,
					IsCDC:       hasCDCColumns(writeSchema),
				}); err != nil {
					errChan <- err
					return
				}
				expectedIncarnation := ""
				if job.CDCStateManager != nil {
					if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
						errChan <- fmt.Errorf("failed to bind CDC destination table %s: %w", ti.Name, err)
						return
					}
					var err error
					expectedIncarnation, err = boundDestinationIncarnation(job.CDCStateManager, ti.Name, destTable)
					if err != nil {
						errChan <- err
						return
					}
					if err := job.ApplyEvolutionForIfIncarnation(ctx, ti.Name, expectedIncarnation); err != nil {
						errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
						return
					}
				}
				mu.Lock()
				tableConfigs[ti.Name] = destination.TableWriteConfig{
					DestTable:              destTable,
					Schema:                 writeSchema,
					PrimaryKeys:            ti.PrimaryKeys,
					IncrementalKey:         ti.Schema.IncrementalKey,
					IncrementalPredicate:   job.Config.IncrementalPredicate,
					CDCMode:                hasCDCColumns(writeSchema),
					SkipCDCResume:          job.CDCStateManager != nil,
					CDCExpectedIncarnation: expectedIncarnation,
				}
				mu.Unlock()
				return
			}

			stagingTable := managedStagingTableName(job.Destination, destTable, "merge", job.Config.StagingDataset)
			mu.Lock()
			stagingTables[ti.Name] = stagingTable
			mu.Unlock()

			if err := prepareMergeTables(ctx, job.Destination, mergeTableParams{
				DestTable:    destTable,
				StagingTable: stagingTable,
				Schema:       writeSchema,
				PrimaryKeys:  ti.PrimaryKeys,
				IsCDC:        hasCDCColumns(writeSchema), // Make non-PK columns nullable for CDC staging tables
			}); err != nil {
				errChan <- err
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
				var err error
				expectedIncarnation, err = boundDestinationIncarnation(job.CDCStateManager, ti.Name, destTable)
				if err != nil {
					errChan <- err
					return
				}
				if err := job.ApplyEvolutionForIfIncarnation(ctx, ti.Name, expectedIncarnation); err != nil {
					errChan <- fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
					return
				}
			}

			mu.Lock()
			targetIncarnations[ti.Name] = expectedIncarnation
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:     stagingTable,
				Schema:        writeSchema,
				PrimaryKeys:   nil,
				CDCMode:       hasCDCColumns(writeSchema),
				SkipCDCResume: job.CDCStateManager != nil,
			}
			mu.Unlock()
		}(tableInfo)
	}

	wg.Wait()
	close(errChan)

	if err := <-errChan; err != nil {
		for _, stagingTable := range stagingTables {
			dropMergeStagingDetached(ctx, job.Destination, stagingTable)
		}
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
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
			CDCStableDataBatches: len(managedAppendOnlyTables) > 0,
			FullRefresh:          job.Config.FullRefresh,
		},
		CDCResumeLSNs:               job.CDCResumeLSNs,
		CDCResumeIncarnations:       resumeIncarnations,
		CDCResumeSchemaFingerprints: resumeSchemas,
	})
	if err != nil {
		for _, stagingTable := range stagingTables {
			dropMergeStagingDetached(ctx, job.Destination, stagingTable)
		}
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	records = applyMultiTableSnapshotInvalidations(readCtx, job, records)
	if directMerge {
		return writeDirectMultiTableMerge(
			readCtx,
			cancelRead,
			direct,
			job.Destination,
			records,
			job.Tables,
			tableConfigs,
			job.Config.EffectiveDestinationParallelism(),
		)
	}

	writeOptions := destination.MultiTableWriteOptions{
		TableConfigs:       tableConfigs,
		Parallelism:        job.Config.EffectiveDestinationParallelism(),
		StagingTable:       true,
		StagingBucket:      job.Config.StagingBucket,
		LoaderFileSize:     job.Config.LoaderFileSize,
		LoaderFileFormat:   job.Config.LoaderFileFormat,
		CancelSource:       cancelRead,
		CancelDrainTimeout: canceledSourceDrainTimeout,
	}
	if len(managedAppendOnlyTables) > 0 {
		writeOptions.TableWriter = func(
			writeCtx context.Context,
			sourceTable string,
			tableRecords <-chan source.RecordBatchResult,
			opts destination.WriteOptions,
		) (bool, error) {
			if _, appendOnly := managedAppendOnlyTables[sourceTable]; appendOnly {
				opts.StagingTable = false
				return writeDurableCDCAppendRecords(writeCtx, job.Destination, tableRecords, opts, sourceTable, job.CDCStateManager)
			}
			if tableConfigs[sourceTable].CDCMode {
				return destination.WriteWithTruncateBoundaries(writeCtx, job.Destination, tableRecords, opts)
			}
			return false, job.Destination.WriteParallel(writeCtx, tableRecords, opts)
		}
	}
	writeResult, err := multitable.WriteWithResult(readCtx, job.Destination, records, writeOptions)
	if err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		if source.ConnectorLeaseLoss(ctx) == nil {
			for _, stagingTable := range stagingTables {
				if source.ConnectorLeaseLoss(ctx) != nil {
					break
				}
				dropMergeStagingDetached(ctx, job.Destination, stagingTable)
			}
		}
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	mergeErrChan := make(chan error, len(job.Tables))
	var mergeWg sync.WaitGroup

	for _, tableInfo := range job.Tables {
		mergeWg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer mergeWg.Done()

			destTable := job.GetDestTableName(ti.Name)
			writeSchema := job.WriteSchemaFor(ti.Name, ti.Schema)
			stagingTable, ok := stagingTables[ti.Name]
			if !ok {
				// Append-only change-log table: rows were written directly.
				return
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}
			if hasCDCColumns(ti.Schema) && writeResult.TruncatedTables[ti.Name] {
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
					return
				}
				var truncateErr error
				if job.CDCStateManager != nil {
					truncateErr = destination.ApplyCDCTruncateIfIncarnation(
						ctx,
						job.Destination,
						destTable,
						targetIncarnations[ti.Name],
					)
				} else {
					truncateErr = destination.ApplyCDCTruncate(ctx, job.Destination, destTable)
				}
				if truncateErr != nil {
					mergeErrChan <- fmt.Errorf("failed to reset CDC target %s: %w", ti.Name, truncateErr)
					return
				}
				if err := source.ConnectorLeaseLoss(ctx); err != nil {
					mergeErrChan <- err
					return
				}
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}
			expectedIncarnation := ""
			if job.CDCStateManager != nil {
				expectedIncarnation = targetIncarnations[ti.Name]
				if expectedIncarnation == "" {
					mergeErrChan <- fmt.Errorf("managed CDC destination table %s has no bound physical incarnation", ti.Name)
					return
				}
			}
			if err := mergeStagingIntoWithCommit(ctx, job.Destination, stagingTable, destTable, ti.PrimaryKeys, writeSchema, ti.Schema.IncrementalKey, job.Config.IncrementalPredicate,
				"", "", job.CDCStateManager != nil, expectedIncarnation); err != nil {
				mergeErrChan <- fmt.Errorf("failed to merge table %s: %w", ti.Name, err)
				return
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				mergeErrChan <- err
				return
			}

			if !job.Config.KeepStaging {
				dropMergeStagingDetached(ctx, job.Destination, stagingTable)
			}
		}(tableInfo)
	}

	mergeWg.Wait()
	close(mergeErrChan)

	var mergeErr error
	for err := range mergeErrChan {
		if mergeErr == nil {
			mergeErr = err
		}
	}

	if mergeErr != nil {
		for _, stagingTable := range stagingTables {
			dropMergeStagingDetached(ctx, job.Destination, stagingTable)
		}
		return mergeErr
	}

	return nil
}

type atomicMultiTableBatchState struct {
	info                 source.SourceTableInfo
	destTable            string
	writeSchema          *schema.TableSchema
	targetSchema         *schema.TableSchema
	expectedIncarnation  string
	targetExisted        bool
	ownershipToken       string
	targetPrepared       bool
	records              chan source.RecordBatchResult
	snapshot             bool
	walTruncate          bool
	attempt              destination.AtomicSnapshotOptions
	boundary             atomicBatchSnapshotBoundary
	publishAttempted     bool
	sealAttempted        bool
	directWriteAttempted bool
}

type atomicBatchSnapshotBoundary struct {
	mu       sync.Mutex
	position string
}

func (b *atomicBatchSnapshotBoundary) observe(result source.RecordBatchResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if result.DurableCommitID != "" && result.DurableCommitPosition != "" {
		b.position = result.DurableCommitPosition
		return
	}
	if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok {
		position := token.Position
		if len(token.SnapshotPositions) == 1 {
			for _, snapshotPosition := range token.SnapshotPositions {
				position = snapshotPosition
			}
		}
		if position != "" {
			b.position = position
		}
	}
}

func (b *atomicBatchSnapshotBoundary) positionValue() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.position
}

func atomicSnapshotCommitID(attemptID string) source.DurableID {
	if attemptID == "" {
		return ""
	}
	return source.DurableID("atomic-snapshot-v1:" + destination.CDCTargetKey(attemptID))
}

func atomicSnapshotAttemptID(
	ctx context.Context,
	manager *CDCStateManager,
	sourceTable, destTable string,
) (string, error) {
	if manager != nil && manager.dest != nil {
		return manager.AtomicSnapshotAttempt(ctx, sourceTable, destTable)
	}
	return uuid.NewString(), nil
}

func completeAtomicSnapshotAttempt(
	ctx context.Context,
	manager *CDCStateManager,
	sourceTable, destTable, attemptID string,
) error {
	if manager == nil || manager.dest == nil {
		return nil
	}
	return manager.CompleteAtomicSnapshotAttempt(ctx, sourceTable, destTable, attemptID)
}

func recoverManagedAtomicSnapshotBeforeRead(
	ctx context.Context,
	dest destination.Destination,
	publisher destination.AtomicSnapshotPublisher,
	manager *CDCStateManager,
	sourceTable, destTable string,
) (position string, recovered bool, err error) {
	if manager == nil {
		return "", false, nil
	}
	if err := requireManagedAtomicSnapshotAbort(dest, manager); err != nil {
		return "", false, err
	}
	attempt, exists, err := manager.PendingAtomicSnapshotAttempt(sourceTable, destTable)
	if err != nil || !exists {
		return "", false, err
	}
	fullRefresh := manager.FullRefresh()
	currentIncarnation, currentSchemaFingerprint := manager.CurrentSourceMetadata(sourceTable)
	metadataChanged := !attempt.sourceMetadataKnown ||
		attempt.sourceIncarnation != currentIncarnation ||
		attempt.sourceSchemaFingerprint != currentSchemaFingerprint
	if !attempt.ready {
		aborter, ok := dest.(destination.AtomicSnapshotAborter)
		if !ok {
			return "", false, fmt.Errorf("destination %s cannot recover an open atomic snapshot attempt", dest.GetScheme())
		}
		abortCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := aborter.AbortAtomicSnapshot(abortCtx, destination.AtomicSnapshotOptions{
			Table: destTable, AttemptID: attempt.attemptID,
		}); err != nil {
			return "", false, fmt.Errorf("failed to abort open atomic snapshot attempt for %s: %w", sourceTable, err)
		}
		if err := manager.AbandonAtomicSnapshotAttempt(ctx, sourceTable, destTable, attempt.attemptID); err != nil {
			return "", false, fmt.Errorf("failed to retire open atomic snapshot attempt for %s: %w", sourceTable, err)
		}
		return "", false, nil
	}
	if fullRefresh || metadataChanged {
		discarder, ok := dest.(destination.AtomicSnapshotDiscarder)
		if !ok {
			return "", false, fmt.Errorf("destination %s cannot safely discard a sealed atomic snapshot attempt", dest.GetScheme())
		}
		discardCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := discarder.DiscardAtomicSnapshot(discardCtx, destination.AtomicSnapshotOptions{
			Table: destTable, AttemptID: attempt.attemptID,
		}); err != nil {
			return "", false, fmt.Errorf("failed to discard stale sealed atomic snapshot attempt for %s: %w", sourceTable, err)
		}
		if err := manager.AbandonAtomicSnapshotAttempt(ctx, sourceTable, destTable, attempt.attemptID); err != nil {
			return "", false, fmt.Errorf("failed to retire discarded sealed atomic snapshot attempt for %s: %w", sourceTable, err)
		}
		return "", false, nil
	}

	publishedIncarnation, err := publisher.PublishAtomicSnapshot(ctx, destination.AtomicSnapshotOptions{
		Table:                  destTable,
		AttemptID:              attempt.attemptID,
		CommitToken:            atomicSnapshotCommitID(attempt.attemptID),
		CDCResumeLSN:           attempt.resumeLSN,
		SkipCDCResume:          true,
		CDCExpectedIncarnation: attempt.expectedIncarnation,
	})
	if err != nil {
		return "", false, fmt.Errorf("failed to recover sealed atomic snapshot publication for %s: %w", sourceTable, err)
	}
	if publishedIncarnation == "" {
		return "", false, fmt.Errorf("recovered atomic snapshot publication for %s returned no physical incarnation", sourceTable)
	}
	if err := manager.BindRecoveredPublishedDestinationIncarnation(
		ctx, sourceTable, destTable, publishedIncarnation,
	); err != nil {
		return "", false, err
	}
	if err := manager.CompleteAtomicSnapshotAttempt(ctx, sourceTable, destTable, attempt.attemptID); err != nil {
		return "", false, fmt.Errorf("failed to complete recovered atomic snapshot attempt for %s: %w", sourceTable, err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{sourceTable: attempt.resumeLSN},
		SnapshotIncarnations: map[string]string{sourceTable: attempt.sourceIncarnation},
		SnapshotSchemas:      map[string]string{sourceTable: attempt.sourceSchemaFingerprint},
	}); err != nil {
		return "", false, fmt.Errorf("failed to persist recovered atomic snapshot state for %s: %w", sourceTable, err)
	}
	return attempt.resumeLSN, true, nil
}

func requireManagedAtomicSnapshotAbort(dest destination.Destination, manager *CDCStateManager) error {
	if manager == nil {
		return nil
	}
	if _, ok := dest.(destination.AtomicSnapshotAborter); !ok {
		return fmt.Errorf("destination %s cannot safely recover an open managed atomic snapshot attempt", dest.GetScheme())
	}
	return nil
}

func observeAtomicBatchSnapshot(ctx context.Context, records <-chan source.RecordBatchResult, boundary *atomicBatchSnapshotBoundary) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult, cap(records))
	go func() {
		defer close(out)
		for {
			select {
			case result, ok := <-records:
				if !ok {
					return
				}
				boundary.observe(result)
				select {
				case out <- result:
				case <-ctx.Done():
					if result.Batch != nil {
						result.Batch.Release()
					}
					startBoundedRecordDrain(records, canceledSourceDrainTimeout)
					return
				}
			case <-ctx.Done():
				startBoundedRecordDrain(records, canceledSourceDrainTimeout)
				return
			}
		}
	}()
	return out
}

type multiTableSpoolEntry struct {
	result   source.RecordBatchResult
	hasBatch bool
}

type multiTableRecordSpool struct {
	entries       []multiTableSpoolEntry
	buffers       map[string]*databuffer.FileBuffer
	invalidations []source.CDCSnapshotInvalidation
}

type atomicBatchSnapshotReset struct {
	marker              source.RecordBatchResult
	walTruncate         bool
	replacementBoundary bool
}

func spoolMultiTableRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	states map[string]*atomicMultiTableBatchState,
) (*multiTableRecordSpool, map[string]atomicBatchSnapshotReset, error) {
	spool := &multiTableRecordSpool{buffers: make(map[string]*databuffer.FileBuffer)}
	resetTables := make(map[string]atomicBatchSnapshotReset)
	pendingInvalidations := make(map[string]int)
	for {
		select {
		case <-ctx.Done():
			_ = spool.Close()
			return nil, nil, context.Cause(ctx)
		case result, ok := <-records:
			if !ok {
				if len(pendingInvalidations) > 0 {
					tables := make([]string, 0, len(pendingInvalidations))
					for tableName := range pendingInvalidations {
						tables = append(tables, tableName)
					}
					sort.Strings(tables)
					_ = spool.Close()
					return nil, nil, fmt.Errorf("source snapshot invalidation for %s was not followed by a replacement boundary", tables[0])
				}
				return spool, resetTables, nil
			}
			if result.Err != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				_ = spool.Close()
				return nil, nil, result.Err
			}
			if result.SnapshotInvalidation != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				invalidation := *result.SnapshotInvalidation
				if _, known := states[invalidation.TableName]; !known {
					_ = spool.Close()
					return nil, nil, fmt.Errorf("source requested snapshot invalidation for unknown table %q", invalidation.TableName)
				}
				if _, exists := pendingInvalidations[invalidation.TableName]; exists {
					_ = spool.Close()
					return nil, nil, fmt.Errorf("source repeated snapshot invalidation for %s before its replacement boundary", invalidation.TableName)
				}
				spool.invalidations = append(spool.invalidations, invalidation)
				pendingInvalidations[invalidation.TableName] = len(spool.invalidations) - 1
				continue
			}
			if invalidationIndex, waiting := pendingInvalidations[result.TableName]; waiting {
				announcement := result.Batch == nil && result.TableInfo != nil
				if !announcement && (!result.Truncate || result.CDCWALTruncate) {
					if result.Batch != nil {
						result.Batch.Release()
					}
					_ = spool.Close()
					return nil, nil, fmt.Errorf("source snapshot invalidation for %s was not followed by a replacement boundary", result.TableName)
				}
				if announcement {
					merged, err := mergeSnapshotInvalidationMetadata(spool.invalidations[invalidationIndex], result.TableInfo)
					if err != nil {
						_ = spool.Close()
						return nil, nil, err
					}
					spool.invalidations[invalidationIndex] = merged
				} else {
					delete(pendingInvalidations, result.TableName)
				}
			}
			if result.Truncate {
				if _, known := states[result.TableName]; !known {
					if result.Batch != nil {
						result.Batch.Release()
					}
					continue
				}
				reset := resetTables[result.TableName]
				reset.marker = result
				reset.walTruncate = result.CDCWALTruncate
				reset.replacementBoundary = reset.replacementBoundary || !result.CDCWALTruncate
				resetTables[result.TableName] = reset
				if result.Batch != nil {
					result.Batch.Release()
				}
				if buffer := spool.buffers[result.TableName]; buffer != nil {
					if err := buffer.Close(); err != nil {
						_ = spool.Close()
						return nil, nil, fmt.Errorf("failed to reset multi-table CDC spool for %s: %w", result.TableName, err)
					}
					delete(spool.buffers, result.TableName)
				}
				entries := spool.entries[:0]
				for _, entry := range spool.entries {
					if entry.result.TableName != result.TableName {
						entries = append(entries, entry)
					}
				}
				spool.entries = entries
				continue
			}
			entry := multiTableSpoolEntry{result: result, hasBatch: result.Batch != nil}
			entry.result.Batch = nil
			if result.Batch != nil {
				if _, known := states[result.TableName]; !known {
					result.Batch.Release()
					continue
				}
				buffer := spool.buffers[result.TableName]
				if buffer == nil {
					var err error
					buffer, err = databuffer.NewFileBuffer()
					if err != nil {
						result.Batch.Release()
						_ = spool.Close()
						return nil, nil, fmt.Errorf("failed to create multi-table CDC spool for %s: %w", result.TableName, err)
					}
					spool.buffers[result.TableName] = buffer
				}
				if err := buffer.Append(ctx, result.Batch); err != nil {
					result.Batch.Release()
					_ = spool.Close()
					return nil, nil, fmt.Errorf("failed to spool multi-table CDC batch for %s: %w", result.TableName, err)
				}
				result.Batch.Release()
			}
			spool.entries = append(spool.entries, entry)
		}
	}
}

func (s *multiTableRecordSpool) Replay(ctx context.Context, states map[string]*atomicMultiTableBatchState) (<-chan source.RecordBatchResult, error) {
	readers := make(map[string]<-chan source.RecordBatchResult, len(s.buffers))
	for table, buffer := range s.buffers {
		reader, err := buffer.Reader(ctx, states[table].writeSchema.ToArrowSchema())
		if err != nil {
			return nil, fmt.Errorf("failed to replay multi-table CDC spool for %s: %w", table, err)
		}
		readers[table] = reader
	}
	out := make(chan source.RecordBatchResult, 16)
	go func() {
		defer close(out)
		defer func() {
			for _, reader := range readers {
				drainAndRelease(reader)
			}
		}()
		for _, entry := range s.entries {
			result := entry.result
			if entry.hasBatch {
				batchResult, ok := <-readers[result.TableName]
				if !ok {
					result.Err = fmt.Errorf("multi-table CDC spool ended early for %s", result.TableName)
				} else if batchResult.Err != nil {
					result.Err = batchResult.Err
				} else {
					result.Batch = batchResult.Batch
				}
			}
			select {
			case out <- result:
			case <-ctx.Done():
				if result.Batch != nil {
					result.Batch.Release()
				}
				return
			}
		}
	}()
	return out, nil
}

func (s *multiTableRecordSpool) Close() error {
	var err error
	for _, buffer := range s.buffers {
		err = errors.Join(err, buffer.Close())
	}
	return err
}

func (s *MergeStrategy) executeAtomicMultiTableBatch(
	ctx context.Context,
	job *MultiTableIngestionJob,
	publisher destination.AtomicSnapshotPublisher,
	direct destination.DirectMergeWriter,
) error {
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	states := make(map[string]*atomicMultiTableBatchState, len(job.Tables))
	for _, info := range job.Tables {
		destTable := job.GetDestTableName(info.Name)
		writeSchema := job.WriteSchemaFor(info.Name, info.Schema)
		targetSchema := destination.DestinationTableSchema(writeSchema)
		if job.Config.IncrementalStrategy == config.StrategyAppend && hasCDCColumns(writeSchema) {
			targetSchema = writeSchema
		} else if plan := job.EvolutionPlans[info.Name]; plan != nil && plan.FinalSchema != nil {
			targetSchema = plan.FinalSchema
		}
		states[info.Name] = &atomicMultiTableBatchState{info: info, destTable: destTable, writeSchema: writeSchema, targetSchema: targetSchema}
	}
	plannedSnapshotTables := make(map[string]struct{})
	for _, info := range job.Tables {
		if !hasCDCColumns(info.Schema) {
			continue
		}
		if job.Config.FullRefresh || strings.TrimSpace(job.CDCResumeLSNs[info.Name]) == "" {
			plannedSnapshotTables[info.Name] = struct{}{}
		}
	}
	plannedTables := make([]string, 0, len(plannedSnapshotTables))
	for tableName := range plannedSnapshotTables {
		plannedTables = append(plannedTables, tableName)
	}
	sort.Strings(plannedTables)
	for _, tableName := range plannedTables {
		st := states[tableName]
		position, recovered, err := recoverManagedAtomicSnapshotBeforeRead(
			ctx, job.Destination, publisher, job.CDCStateManager, tableName, st.destTable,
		)
		if err != nil {
			return err
		}
		if !recovered {
			continue
		}
		if job.CDCResumeLSNs == nil {
			job.CDCResumeLSNs = make(map[string]string)
		}
		job.CDCResumeLSNs[tableName] = position
		delete(plannedSnapshotTables, tableName)
	}
	if job.CDCStateManager != nil {
		operation := "managed keyless CDC merge"
		if job.Config.IncrementalStrategy == config.StrategyAppend {
			operation = "managed multi-table CDC append"
		}
		for _, info := range job.Tables {
			if !isAppendOnlyCDCTable(info) {
				continue
			}
			if _, snapshot := plannedSnapshotTables[info.Name]; snapshot {
				continue
			}
			if err := requireIdempotentCommitTokenWrites(job.Destination, operation); err != nil {
				return err
			}
			break
		}
	}
	jobSucceeded := false
	defer func() {
		if jobSucceeded {
			return
		}
		aborter, canAbort := job.Destination.(destination.AtomicSnapshotAborter)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cleanupCancel()
		for _, st := range states {
			if st.snapshot && !st.publishAttempted && !st.sealAttempted && canAbort && st.attempt.AttemptID != "" {
				_ = aborter.AbortAtomicSnapshot(cleanupCtx, st.attempt)
			}
			neverMutatedTarget := st.snapshot && !st.publishAttempted && !st.sealAttempted || !st.snapshot && !st.directWriteAttempted
			if st.targetPrepared && neverMutatedTarget && st.ownershipToken != "" {
				cleanupFailedOwnedDirectReplace(cleanupCtx, job.Destination, st.destTable, !st.targetExisted, st.ownershipToken)
			}
		}
	}()
	for _, info := range job.Tables {
		st := states[info.Name]
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		_, snapshot := plannedSnapshotTables[info.Name]
		if !snapshot && job.CDCStateManager == nil {
			if err := job.ApplyEvolutionFor(ctx, info.Name); err != nil {
				return fmt.Errorf("failed to evolve destination table %s before source read: %w", info.Name, err)
			}
		}
		existingSchema, err := job.Destination.GetTableSchema(ctx, st.destTable)
		if err != nil {
			return fmt.Errorf("failed to inspect destination table %s before multi-table CDC: %w", st.destTable, err)
		}
		st.targetExisted = existingSchema != nil
		st.ownershipToken = newTargetOwnershipToken(job.Destination, st.targetExisted)
		st.targetPrepared = !st.targetExisted && st.ownershipToken != ""
		if isAppendOnlyCDCTable(info) {
			if !st.targetExisted {
				if st.ownershipToken == "" {
					return fmt.Errorf("destination %s cannot safely clean up a failed atomic append target", job.Destination.GetScheme())
				}
				if err := prepareAppendOnlyCDCTableOwned(ctx, job.Destination, st.destTable, st.writeSchema, st.ownershipToken); err != nil {
					return err
				}
				st.targetPrepared = true
			}
		} else if err := prepareMergeTarget(ctx, job.Destination, mergeTableParams{
			DestTable: st.destTable, Schema: st.writeSchema, PrimaryKeys: info.PrimaryKeys, IsCDC: hasCDCColumns(st.writeSchema),
			OwnershipToken: st.ownershipToken,
		}); err != nil {
			return err
		} else {
			st.targetPrepared = true
		}
		if job.CDCStateManager != nil && job.CDCStateManager.dest != nil {
			if err := job.CDCStateManager.BindDestinationIncarnation(ctx, info.Name, st.destTable); err != nil {
				return fmt.Errorf("failed to bind CDC destination table %s before source read: %w", info.Name, err)
			}
			st.expectedIncarnation, err = boundDestinationIncarnation(job.CDCStateManager, info.Name, st.destTable)
			if err != nil {
				return err
			}
			if !snapshot {
				if err := job.ApplyEvolutionForIfIncarnation(ctx, info.Name, st.expectedIncarnation); err != nil {
					return fmt.Errorf("failed to evolve destination table %s before source read: %w", info.Name, err)
				}
			}
		}
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	resumeIncarnations, resumeSchemas := cdcResumeMetadata(job.Tables)
	records, err := job.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism: parallelism, PageSize: job.Config.PageSize, Limit: job.Config.SQLLimit,
			CDCSlotSuffix: job.Config.CDCSlotSuffix, CDCLegacySlotSuffix: job.Config.CDCLegacySlotSuffix,
			CDCSnapshotReplace: true, FullRefresh: job.Config.FullRefresh,
			CDCStableDataBatches: managedMultiTableStableDataBatches(job),
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

	spool, resetTables, err := spoolMultiTableRecords(readCtx, records, states)
	if err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to preflight multi-table CDC records: %w", err)
	}
	defer func() { _ = spool.Close() }()
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	for _, invalidation := range spool.invalidations {
		reset, ok := resetTables[invalidation.TableName]
		if !ok || !reset.replacementBoundary {
			return fmt.Errorf("source snapshot invalidation for %s was not followed by a replacement boundary", invalidation.TableName)
		}
	}
	for _, invalidation := range spool.invalidations {
		if job.CDCStateManager == nil {
			return fmt.Errorf("source requested snapshot invalidation without destination-managed CDC state")
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		st := states[invalidation.TableName]
		if err := job.CDCStateManager.InvalidateSnapshotStatePreservingDestination(
			ctx, invalidation.TableName, st.destTable, invalidation.Incarnation, invalidation.SchemaFingerprint,
		); err != nil {
			return err
		}
	}
	for tableName, reset := range resetTables {
		plannedSnapshotTables[tableName] = struct{}{}
		if st, ok := states[tableName]; ok {
			st.walTruncate = reset.walTruncate
			st.boundary.observe(reset.marker)
		}
	}
	records, err = spool.Replay(readCtx, states)
	if err != nil {
		return err
	}
	for tableName := range plannedSnapshotTables {
		if st, ok := states[tableName]; ok {
			attemptID, err := atomicSnapshotAttemptID(ctx, job.CDCStateManager, tableName, st.destTable)
			if err != nil {
				return fmt.Errorf("failed to establish durable atomic snapshot attempt for %s: %w", tableName, err)
			}
			st.snapshot = true
			st.attempt = destination.AtomicSnapshotOptions{
				Table: st.destTable, Schema: st.writeSchema, TargetSchema: st.targetSchema,
				PrimaryKeys: st.info.PrimaryKeys, Parallelism: job.Config.EffectiveDestinationParallelism(), AttemptID: attemptID,
				SkipCDCResume:          job.CDCStateManager != nil,
				CDCExpectedIncarnation: st.expectedIncarnation,
			}
		}
	}
	snapshotTables := make([]string, 0, len(plannedSnapshotTables))
	for tableName := range plannedSnapshotTables {
		snapshotTables = append(snapshotTables, tableName)
	}
	sort.Strings(snapshotTables)
	for _, tableName := range snapshotTables {
		st := states[tableName]
		if !st.snapshot {
			continue
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := publisher.BeginAtomicSnapshot(ctx, st.attempt); err != nil {
			return fmt.Errorf("failed to begin snapshot table %s: %w", tableName, err)
		}
	}

	g, gctx := errgroup.WithContext(readCtx)
	startWorker := func(st *atomicMultiTableBatchState) error {
		st.records = make(chan source.RecordBatchResult, 8)
		g.Go(func() error {
			if st.snapshot {
				observed := observeAtomicBatchSnapshot(gctx, st.records, &st.boundary)
				if err := publisher.WriteAtomicSnapshot(gctx, observed, destination.WriteOptions{
					Table: st.destTable, Schema: st.writeSchema, PrimaryKeys: st.info.PrimaryKeys,
					Parallelism: job.Config.EffectiveDestinationParallelism(), AtomicSnapshotAttemptID: st.attempt.AttemptID,
					SkipCDCResume:          job.CDCStateManager != nil,
					CDCExpectedIncarnation: st.expectedIncarnation,
				}); err != nil {
					drainAndRelease(observed)
					return fmt.Errorf("failed to stage snapshot table %s: %w", st.info.Name, err)
				}
				return nil
			}
			st.directWriteAttempted = true
			if isAppendOnlyCDCTable(st.info) {
				return writeDurableKeylessCDCRecords(gctx, job.Destination, st.records, destination.WriteOptions{
					Table: st.destTable, Schema: st.writeSchema, Parallelism: job.Config.EffectiveDestinationParallelism(),
					SkipCDCResume:          job.CDCStateManager != nil,
					CDCExpectedIncarnation: st.expectedIncarnation,
				})
			}
			return direct.MergeRecords(gctx, st.records, destination.WriteOptions{
				Table: st.destTable, Schema: st.writeSchema, Parallelism: job.Config.EffectiveDestinationParallelism(),
				SkipCDCResume:          job.CDCStateManager != nil,
				CDCExpectedIncarnation: st.expectedIncarnation,
			}, destination.MergeOptions{
				TargetTable: st.destTable, PrimaryKeys: st.info.PrimaryKeys,
				Columns:              destination.MergeColumnsFor(job.Destination, st.writeSchema.ColumnNames()),
				IncrementalKey:       mergeIncrementalKeyForSchema(st.writeSchema, st.writeSchema.IncrementalKey),
				IncrementalPredicate: job.Config.IncrementalPredicate,
				Schema:               st.writeSchema, Parallelism: job.Config.EffectiveDestinationParallelism(),
				SkipCDCResume:          job.CDCStateManager != nil,
				CDCExpectedIncarnation: st.expectedIncarnation,
			})
		})
		return nil
	}

	var dispatchErr error
dispatch:
	for {
		select {
		case <-gctx.Done():
			dispatchErr = context.Cause(gctx)
			break dispatch
		case result, ok := <-records:
			if !ok {
				break dispatch
			}
			if result.Err != nil && result.TableName == "" {
				if result.Batch != nil {
					result.Batch.Release()
				}
				dispatchErr = result.Err
				break dispatch
			}
			st, known := states[result.TableName]
			if !known {
				if result.Batch != nil {
					result.Batch.Release()
				}
				continue
			}
			if result.Truncate {
				if result.Batch != nil {
					result.Batch.Release()
				}
				dispatchErr = fmt.Errorf("unexpected snapshot reset for table %s after multi-table CDC preflight", result.TableName)
				break dispatch
			}
			if st.records == nil {
				if err := startWorker(st); err != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					dispatchErr = err
					break dispatch
				}
			}
			select {
			case st.records <- result:
			case <-gctx.Done():
				if result.Batch != nil {
					result.Batch.Release()
				}
				dispatchErr = context.Cause(gctx)
				break dispatch
			}
		}
	}
	if dispatchErr != nil {
		for _, st := range states {
			if st.records == nil {
				continue
			}
			select {
			case st.records <- source.RecordBatchResult{Err: dispatchErr}:
			case <-gctx.Done():
			}
		}
	}
	for _, st := range states {
		if st.records != nil {
			close(st.records)
		}
	}
	groupErr := g.Wait()
	for _, st := range states {
		if st.records != nil {
			drainAndRelease(st.records)
		}
	}
	if dispatchErr != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return dispatchErr
	}
	if groupErr != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return groupErr
	}

	for _, tableName := range snapshotTables {
		st := states[tableName]
		if !st.snapshot {
			continue
		}
		st.attempt.CommitToken = atomicSnapshotCommitID(st.attempt.AttemptID)
		st.attempt.CDCResumeLSN = st.boundary.positionValue()
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if job.CDCStateManager != nil && job.CDCStateManager.dest != nil {
			st.sealAttempted = true
			if err := job.CDCStateManager.SealAtomicSnapshotAttempt(
				ctx, tableName, st.destTable, st.attempt.AttemptID, st.attempt.CDCResumeLSN, st.expectedIncarnation,
			); err != nil {
				return fmt.Errorf("failed to seal durable atomic snapshot attempt for %s: %w", tableName, err)
			}
		}
		st.publishAttempted = true
		publishedIncarnation, err := publisher.PublishAtomicSnapshot(ctx, st.attempt)
		if err != nil {
			return fmt.Errorf("failed to publish snapshot table %s: %w", tableName, err)
		}
		if job.CDCStateManager != nil && job.CDCStateManager.dest != nil {
			if publishedIncarnation == "" {
				return fmt.Errorf("atomic publication for table %s returned no physical incarnation", tableName)
			}
			if err := job.CDCStateManager.BindPublishedDestinationIncarnation(
				ctx, tableName, st.destTable, st.expectedIncarnation, publishedIncarnation,
			); err != nil {
				return fmt.Errorf("failed to revalidate published CDC destination table %s: %w", tableName, err)
			}
			if err := completeAtomicSnapshotAttempt(
				ctx, job.CDCStateManager, tableName, st.destTable, st.attempt.AttemptID,
			); err != nil {
				return fmt.Errorf("failed to complete durable atomic snapshot attempt for %s: %w", tableName, err)
			}
		}
	}
	if job.CDCStateManager != nil {
		for _, tableName := range snapshotTables {
			st := states[tableName]
			if !st.walTruncate {
				continue
			}
			job.CDCStateManager.RecordBatchSnapshotCompletion(tableName, st.boundary.positionValue())
		}
	}
	jobSucceeded = true
	return nil
}

func managedMultiTableStableDataBatches(job *MultiTableIngestionJob) bool {
	if job.CDCStateManager == nil {
		return false
	}
	if job.Config.IncrementalStrategy == config.StrategyAppend {
		return true
	}
	for _, table := range job.Tables {
		if isAppendOnlyCDCTable(table) {
			return true
		}
	}
	return false
}

func dropMergeStagingDetached(ctx context.Context, dest destination.Destination, table string) {
	if source.ConnectorLeaseLoss(ctx) != nil {
		return
	}
	cleanupBase, detachCancel := source.WithoutCancelWithConnectorLease(ctx)
	defer detachCancel()
	cleanupCtx, cancel := context.WithTimeout(cleanupBase, 30*time.Second)
	defer cancel()
	if err := dest.DropTable(cleanupCtx, table); err != nil {
		config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", table, err)
	}
}

func writeDirectMultiTableMerge(
	ctx context.Context,
	cancelSource context.CancelFunc,
	direct destination.DirectMergeWriter,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	tables []source.SourceTableInfo,
	configs map[string]destination.TableWriteConfig,
	parallelism int,
) error {
	channels := make(map[string]chan source.RecordBatchResult, len(configs))
	g, gctx := errgroup.WithContext(ctx)
	for _, ti := range tables {
		cfg, ok := configs[ti.Name]
		if !ok {
			continue
		}
		ch := make(chan source.RecordBatchResult, 2)
		channels[ti.Name] = ch
		if isAppendOnlyCDCTable(ti) {
			g.Go(func() error {
				return writeDurableKeylessCDCRecords(gctx, dest, ch, destination.WriteOptions{
					Table: cfg.DestTable, Schema: cfg.Schema, Parallelism: parallelism,
					SkipCDCResume: cfg.SkipCDCResume, CDCExpectedIncarnation: cfg.CDCExpectedIncarnation,
				})
			})
			continue
		}
		g.Go(func() error {
			return direct.MergeRecords(gctx, ch, destination.WriteOptions{
				Table: cfg.DestTable, Schema: cfg.Schema, Parallelism: parallelism,
				SkipCDCResume: cfg.SkipCDCResume, CDCExpectedIncarnation: cfg.CDCExpectedIncarnation,
			}, destination.MergeOptions{
				TargetTable:            cfg.DestTable,
				PrimaryKeys:            ti.PrimaryKeys,
				Columns:                destination.MergeColumnsFor(dest, cfg.Schema.ColumnNames()),
				IncrementalKey:         mergeIncrementalKeyForSchema(cfg.Schema, cfg.IncrementalKey),
				IncrementalPredicate:   cfg.IncrementalPredicate,
				Schema:                 cfg.Schema,
				Parallelism:            parallelism,
				SkipCDCResume:          cfg.SkipCDCResume,
				CDCExpectedIncarnation: cfg.CDCExpectedIncarnation,
			})
		})
	}

	var dispatchErr error
	var sourceFailed bool
dispatch:
	for {
		var result source.RecordBatchResult
		var ok bool
		select {
		case result, ok = <-records:
			if !ok {
				break dispatch
			}
		case <-gctx.Done():
			dispatchErr = context.Cause(gctx)
			cancelSource()
			break dispatch
		}
		if result.Err != nil && result.TableName == "" {
			if result.Batch != nil {
				result.Batch.Release()
			}
			dispatchErr = result.Err
			sourceFailed = true
			break
		}
		ch, ok := channels[result.TableName]
		if !ok {
			if result.Batch != nil {
				result.Batch.Release()
			}
			continue
		}
		select {
		case ch <- result:
		case <-gctx.Done():
			if result.Batch != nil {
				result.Batch.Release()
			}
			dispatchErr = context.Cause(gctx)
			break dispatch
		}
	}
	for _, ch := range channels {
		close(ch)
	}
	var sourceDrainDone <-chan struct{}
	if dispatchErr != nil {
		cancelSource()
		sourceDrainDone = startBoundedRecordDrain(records, canceledSourceDrainTimeout)
	}
	groupErr := g.Wait()
	for _, ch := range channels {
		drainAndRelease(ch)
	}
	if sourceDrainDone != nil {
		<-sourceDrainDone
	}
	// A source failure cancels the merge goroutines, so their context-canceled
	// group error must not mask the actual failure.
	if sourceFailed {
		return dispatchErr
	}
	if groupErr != nil {
		return fmt.Errorf("failed to merge multi-table data directly: %w", groupErr)
	}
	return dispatchErr
}

// writeDurableKeylessCDCRecords commits each source transaction independently.
// A keyless change log cannot deduplicate by row identity, so retry safety comes
// from the stable per-transaction DurableCommitID emitted by the CDC source.
func writeDurableKeylessCDCRecords(
	ctx context.Context,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	if err := requireIdempotentCommitTokenWrites(dest, "durable keyless CDC writes"); err != nil {
		return err
	}
	for {
		var result source.RecordBatchResult
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok = <-records:
			if !ok {
				return nil
			}
		}

		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			return result.Err
		}
		commitID := result.DurableCommitID
		commitPosition := result.DurableCommitPosition
		if opts.SkipCDCResume {
			if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok && token.DataBatchID != "" {
				commitID = managedCDCDataWriteID(result.TableName, token.DataBatchID)
				commitPosition = token.Position
			}
		}
		if commitID == "" {
			if result.Batch == nil {
				continue
			}
			result.Batch.Release()
			return fmt.Errorf("keyless CDC batch for table %s has no durable transaction identifier", opts.Table)
		}

		if result.Batch == nil {
			if opts.SkipCDCResume {
				continue
			}
			writer, ok := dest.(destination.DurableCommitTokenWriter)
			if !ok {
				return fmt.Errorf("destination %s cannot persist an empty keyless CDC checkpoint", dest.GetScheme())
			}
			if err := writer.CommitWriteToken(ctx, opts.Table, commitID, commitPosition); err != nil {
				return fmt.Errorf("failed to persist keyless CDC checkpoint for table %s: %w", opts.Table, err)
			}
			continue
		}

		writeOpts := opts
		writeOpts.CommitToken = commitID
		writeOpts.CDCResumeLSN = commitPosition
		if opts.SkipCDCResume {
			writeOpts.CDCResumeLSN = ""
		}
		writeOpts.SkipCDCResume = opts.SkipCDCResume || commitPosition == ""
		batch := make(chan source.RecordBatchResult, 1)
		batch <- source.RecordBatchResult{Batch: result.Batch}
		close(batch)
		if err := dest.WriteParallel(ctx, batch, writeOpts); err != nil {
			drainAndRelease(batch)
			return fmt.Errorf("failed to write durable keyless CDC batch to table %s: %w", opts.Table, err)
		}
	}
}

func requireIdempotentCommitTokenWrites(dest destination.Destination, operation string) error {
	idempotent, ok := dest.(destination.IdempotentCommitTokenWriter)
	if !ok || !idempotent.SupportsIdempotentCommitTokenWrites() {
		return fmt.Errorf("%s requires destination support for idempotent commit-token writes", operation)
	}
	return nil
}

// startBoundedRecordDrain lets a canceled source finish sends that were
// already in flight. Some source producers use blocking sends, so consuming
// only the records immediately available can strand them between sends. The
// deadline also prevents an uncooperative source from blocking shutdown.
func startBoundedRecordDrain(records <-chan source.RecordBatchResult, timeout time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				return
			default:
			}

			select {
			case result, ok := <-records:
				if !ok {
					return
				}
				if result.Batch != nil {
					result.Batch.Release()
				}
			case <-timer.C:
				return
			}
		}
	}()
	return done
}

// hasCDCColumns checks if a schema has CDC columns (specifically _cdc_deleted).
// This is used to detect CDC sources for special merge handling.
func hasCDCColumns(s *schema.TableSchema) bool {
	if s == nil {
		return false
	}
	return destination.HasCDCDeletedColumn(s.ColumnNames())
}
