package strategy

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/google/uuid"
)

// replaceShouldDedup reports whether deduplicated replace applies for the
// destination: it can merge and isn't ClickHouse (which deduplicates natively
// via its table engine).
func replaceShouldDedup(dest destination.Destination, primaryKeys []string) bool {
	return len(primaryKeys) > 0 &&
		dest.SupportsMergeStrategy() &&
		dest.GetScheme() != "clickhouse"
}

func directReplaceShouldDedup(dest destination.Destination, primaryKeys []string) bool {
	provider, ok := dest.(destination.DirectReplaceDeduplicator)
	return ok && provider.SupportsDirectReplaceDeduplication() && replaceShouldDedup(dest, primaryKeys)
}

func sourcePrimaryKeysSafeForReplaceFastPath(job *IngestionJob) bool {
	return sourcePrimaryKeysGuaranteedUnique(job) &&
		!primaryKeyValuesMayChange(job)
}

func sourcePrimaryKeysGuaranteedUnique(job *IngestionJob) bool {
	provider, ok := job.Table.(source.PrimaryKeyUniquenessProvider)
	if !ok || !provider.PrimaryKeysUnique() || job.SourceSchema == nil {
		return false
	}
	if !slices.Equal(job.Table.PrimaryKeys(), job.SourceSchema.PrimaryKeys) {
		return false
	}

	return slices.Equal(mappedSourcePrimaryKeys(job), job.Config.PrimaryKeys) &&
		!nonPrimaryKeyMapsToDestinationPrimaryKey(job)
}

func mappedSourcePrimaryKeys(job *IngestionJob) []string {
	primaryKeys := append([]string(nil), job.SourceSchema.PrimaryKeys...)
	if job.ColumnRenamer == nil || !job.ColumnRenamer.HasRenames() {
		return primaryKeys
	}

	mapping := job.ColumnRenamer.Mapping()
	for i, pk := range primaryKeys {
		if mapped, ok := mapping[pk]; ok {
			primaryKeys[i] = mapped
		}
	}
	return primaryKeys
}

func nonPrimaryKeyMapsToDestinationPrimaryKey(job *IngestionJob) bool {
	sourcePrimaryKeys := make(map[string]bool, len(job.SourceSchema.PrimaryKeys))
	for _, pk := range job.SourceSchema.PrimaryKeys {
		sourcePrimaryKeys[pk] = true
	}
	destinationPrimaryKeys := make(map[string]bool, len(job.Config.PrimaryKeys))
	for _, pk := range job.Config.PrimaryKeys {
		destinationPrimaryKeys[pk] = true
	}

	var mapping map[string]string
	if job.ColumnRenamer != nil && job.ColumnRenamer.HasRenames() {
		mapping = job.ColumnRenamer.Mapping()
	}

	for _, col := range job.SourceSchema.Columns {
		if sourcePrimaryKeys[col.Name] {
			continue
		}
		name := col.Name
		if mapped, ok := mapping[name]; ok {
			name = mapped
		}
		if destinationPrimaryKeys[name] {
			return true
		}
	}
	return false
}

func primaryKeyValuesMayChange(job *IngestionJob) bool {
	if len(job.Config.PrimaryKeys) == 0 {
		return false
	}
	if job.TypeCaster != nil {
		return true
	}
	if job.ColumnMasker != nil && intersects(job.Config.PrimaryKeys, job.ColumnMasker.MaskedColumns()) {
		return true
	}
	if whitespaceTrimmerMayChangePrimaryKeys(job) {
		return true
	}

	mode, err := schemaevolution.ParseContractMode(job.Config.SchemaContract)
	if err == nil && mode == schemaevolution.ContractDiscardValue && schemaChangesTouchPrimaryKeys(job.SchemaComparison, job.Config.PrimaryKeys) {
		return true
	}
	return false
}

func whitespaceTrimmerMayChangePrimaryKeys(job *IngestionJob) bool {
	if job.WhitespaceTrimmer == nil || job.Schema == nil {
		return false
	}

	primaryKeys := make(map[string]bool, len(job.Config.PrimaryKeys))
	for _, key := range job.Config.PrimaryKeys {
		primaryKeys[key] = true
	}
	for _, column := range job.Schema.Columns {
		if primaryKeys[column.Name] && (column.DataType == schema.TypeString || column.DataType == schema.TypeUUID) {
			return true
		}
	}
	return false
}

func schemaChangesTouchPrimaryKeys(comparison *schemaevolution.SchemaComparison, primaryKeys []string) bool {
	if comparison == nil || !comparison.HasChanges {
		return false
	}

	pkSet := make(map[string]bool, len(primaryKeys))
	for _, pk := range primaryKeys {
		pkSet[pk] = true
	}
	for _, change := range comparison.Changes {
		if pkSet[change.ColumnName] {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	set := make(map[string]bool, len(left))
	for _, v := range left {
		set[v] = true
	}
	for _, v := range right {
		if set[v] {
			return true
		}
	}
	return false
}

// deduplicateStaging collapses duplicate primary keys from rawTable into a
// normalised table in the target's schema, drops rawTable, and returns the
// normalised table to swap into the target. Callers swap the returned table with
// nil primary keys, since it already carries the PK and lives in the target's
// schema (so the swap is a same-schema atomic rename rather than a second
// recreate+copy). Staging tables are cleaned up on error.
func deduplicateStaging(ctx context.Context, dest destination.Destination, rawTable, targetTable, stagingDataset, incrementalKey string, tableSchema *schema.TableSchema, primaryKeys []string, partitionBy string, clusterBy []string, skipCDCResume bool) (string, error) {
	normalised := GenerateNormalisedStagingTableName(targetTable, stagingDataset)
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        normalised,
		Schema:       tableSchema,
		DropFirst:    true,
		PrimaryKeys:  primaryKeys,
		PartitionBy:  partitionBy,
		ClusterBy:    clusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		for _, table := range []string{rawTable, normalised} {
			dropReplaceStagingDetached(ctx, dest, table)
		}
		return "", fmt.Errorf("failed to prepare normalised staging table: %w", err)
	}
	if err := dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   rawTable,
		TargetTable:    normalised,
		PrimaryKeys:    primaryKeys,
		Columns:        tableSchema.ColumnNames(),
		IncrementalKey: incrementalKey,
		Schema:         tableSchema,
		SkipCDCResume:  skipCDCResume,
	}); err != nil {
		for _, t := range []string{rawTable, normalised} {
			dropReplaceStagingDetached(ctx, dest, t)
		}
		return "", fmt.Errorf("failed to deduplicate staging table: %w", err)
	}
	dropReplaceStagingDetached(ctx, dest, rawTable)
	return normalised, nil
}

type ReplaceStrategy struct{}

type ownedPreparedTarget struct {
	table          string
	ownershipToken string
}

func replaceStagingTableName(dest destination.Destination, targetTable, stagingDataset string) string {
	policy := defaultReplaceStagingPolicy()
	if provider, ok := dest.(destination.ReplaceStagingPolicyProvider); ok {
		policy = provider.ReplaceStagingPolicy()
	}
	return GenerateReplaceStagingTableName(targetTable, "staging", stagingDataset, policy)
}

func (s *ReplaceStrategy) Name() config.IncrementalStrategy {
	return config.StrategyReplace
}

func (s *ReplaceStrategy) Validate(cfg *config.IngestConfig) error {
	return nil
}

func (s *ReplaceStrategy) RequiresPrimaryKey() bool {
	return false
}

func (s *ReplaceStrategy) RequiresIncrementalKey() bool {
	return false
}

func (s *ReplaceStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	// Check if destination supports atomic swap (staging pattern)
	// File-based destinations like CSV, Parquet, and Blobstore write directly to target
	useStaging := job.Destination.SupportsAtomicSwap()
	if useStaging && job.CDCStateManager != nil {
		capability, ok := job.Destination.(destination.CDCConditionalSwapCapable)
		if !ok || !capability.SupportsCDCConditionalSwap() {
			return fmt.Errorf("destination scheme %q cannot atomically fence managed CDC table replacement", job.Destination.GetScheme())
		}
	}

	targetTable := job.Config.DestTable
	replaceSchema := destination.DestinationTableSchema(job.Schema)
	writeTable := targetTable
	targetExisted := true
	if useStaging {
		writeTable = replaceStagingTableName(job.Destination, targetTable, job.Config.StagingDataset)
		output.Statusf("[STRATEGY] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), writeTable)
	} else {
		config.Debug("[STRATEGY] Direct write to target (no staging): %s", writeTable)
		existingSchema, err := job.Destination.GetTableSchema(ctx, targetTable)
		if err != nil {
			return fmt.Errorf("failed to inspect destination table before replace: %w", err)
		}
		targetExisted = existingSchema != nil
	}

	// Deduplicated replace: Load a PK-free staging table so duplicate keys can land.
	dedup := false
	if !sourcePrimaryKeysSafeForReplaceFastPath(job) {
		if useStaging {
			dedup = replaceShouldDedup(job.Destination, job.Config.PrimaryKeys)
		} else {
			dedup = directReplaceShouldDedup(job.Destination, job.Config.PrimaryKeys)
		}
	}
	stagingPrimaryKeys := job.Config.PrimaryKeys
	if useStaging && dedup {
		stagingPrimaryKeys = nil
	}
	ownershipToken := ""
	if !useStaging && !targetExisted {
		if _, ok := job.Destination.(destination.OwnedTableDropper); ok {
			ownershipToken = uuid.NewString()
		}
	}

	prepareOpts := destination.PrepareOptions{
		Table:          writeTable,
		Schema:         replaceSchema,
		DropFirst:      true,
		PrimaryKeys:    stagingPrimaryKeys,
		PartitionBy:    job.Config.PartitionBy,
		ClusterBy:      job.Config.ClusterBy,
		OwnershipToken: ownershipToken,
	}
	if useStaging {
		prepareOpts.ExpiresAfter = destination.ManagedStagingTTL
	}
	cleanupStaging := useStaging && !job.Config.KeepStaging
	cleanupTable := writeTable
	defer func() {
		if cleanupStaging {
			dropReplaceStagingDetached(ctx, job.Destination, cleanupTable)
		}
	}()

	if err := job.Destination.PrepareTable(ctx, prepareOpts); err != nil {
		if !useStaging && ownershipToken != "" {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		}
		return fmt.Errorf("failed to prepare table: %w", err)
	}
	if useStaging && job.CDCStateManager != nil {
		if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
			Table: targetTable, Schema: replaceSchema, DropFirst: false,
			PrimaryKeys: job.Config.PrimaryKeys, PartitionBy: job.Config.PartitionBy, ClusterBy: job.Config.ClusterBy,
		}); err != nil {
			return fmt.Errorf("failed to prepare managed replacement target: %w", err)
		}
	}
	expectedIncarnation := ""
	if job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, targetTable); err != nil {
			return fmt.Errorf("failed to bind replacement destination before extraction: %w", err)
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
		if !useStaging {
			cleanupFailedOwnedDirectReplace(ctx, job.Destination, targetTable, !targetExisted, ownershipToken)
		}
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	records = transformer.Wrap(records, transformer.NewSafeTypeCaster(replaceSchema.ToArrowSchema()))

	writeOpts := destination.WriteOptions{
		Table:                  writeTable,
		Schema:                 replaceSchema,
		PrimaryKeys:            job.Config.PrimaryKeys,
		Parallelism:            job.Config.EffectiveDestinationParallelism(),
		StagingTable:           useStaging,
		StagingBucket:          job.Config.StagingBucket,
		LoaderFileSize:         job.Config.LoaderFileSize,
		LoaderFileFormat:       job.Config.LoaderFileFormat,
		PreStaged:              job.PreStaged,
		DeduplicatePrimaryKeys: dedup && !useStaging,
		IncrementalKey:         job.Config.IncrementalKey,
		SkipCDCResume:          job.CDCStateManager != nil,
	}
	if !useStaging {
		writeOpts.CDCExpectedIncarnation = expectedIncarnation
	}
	if useStaging {
		if atomicWriter, ok := job.Destination.(destination.AtomicCommitWriter); ok && atomicWriter.SupportsAtomicCommitWrites() {
			writeOpts.AtomicCommit = true
		}
	}

	var countedRows atomic.Int64
	if !writeOpts.AtomicCommit && writeOpts.PreStaged == nil {
		records = wrapWithRowCount(readCtx, records, &countedRows)
	}

	if err := job.Destination.WriteParallel(readCtx, records, writeOpts); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		return fmt.Errorf("failed to write data: %w", err)
	}
	if !useStaging && job.CDCStateManager != nil {
		if err := job.CDCStateManager.BindDestinationIncarnation(ctx, job.Config.SourceTable, targetTable); err != nil {
			return fmt.Errorf("failed to bind replaced destination table: %w", err)
		}
	}

	// Only swap tables if using staging pattern
	if useStaging {
		if !writeOpts.AtomicCommit {
			if verifier, ok := job.Destination.(destination.ExactRowCountWaiter); ok {
				expectedRows := countedRows.Load()
				if writeOpts.PreStaged != nil {
					expectedRows = writeOpts.PreStaged.RowCount()
				}
				if expectedRows > 0 {
					if err := verifier.WaitForExactRowCount(ctx, writeTable, expectedRows); err != nil {
						return fmt.Errorf("failed to verify staging table row count: %w", err)
					}
				}
			}
		}
		swapTable := writeTable
		swapPrimaryKeys := job.Config.PrimaryKeys
		if dedup {
			cleanupStaging = false
			normalised, err := deduplicateStaging(ctx, job.Destination, writeTable, targetTable,
				job.Config.StagingDataset, job.Config.IncrementalKey, replaceSchema,
				job.Config.PrimaryKeys, job.Config.PartitionBy, job.Config.ClusterBy, job.CDCStateManager != nil)
			if err != nil {
				return err
			}
			swapTable = normalised
			swapPrimaryKeys = nil
			cleanupTable = normalised
			cleanupStaging = !job.Config.KeepStaging
		}
		publishedIncarnation := ""
		if job.CDCStateManager != nil {
			publishedIncarnation, err = job.CDCStateManager.DestinationIncarnationForPublication(ctx, swapTable)
			if err != nil {
				return fmt.Errorf("failed to identify replacement table before publication: %w", err)
			}
		}

		if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
			StagingTable:                  swapTable,
			TargetTable:                   targetTable,
			PrimaryKeys:                   swapPrimaryKeys,
			IncrementalKey:                job.Config.IncrementalKey,
			Schema:                        replaceSchema,
			CDCExpectedIncarnation:        expectedIncarnation,
			CDCExpectedStagingIncarnation: publishedIncarnation,
		}); err != nil {
			return fmt.Errorf("failed to swap tables: %w", err)
		}
		if job.CDCStateManager != nil {
			if err := job.CDCStateManager.BindPublishedDestinationIncarnation(ctx, job.Config.SourceTable, targetTable, expectedIncarnation, publishedIncarnation); err != nil {
				return fmt.Errorf("failed to bind replaced destination table: %w", err)
			}
		}
		cleanupStaging = false
	}

	return nil
}

func dropReplaceStagingDetached(ctx context.Context, dest destination.Destination, table string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := dest.DropTable(cleanupCtx, table); err != nil {
		config.Debug("[REPLACE] Warning: failed to drop staging table %s: %v", table, err)
	}
}

func cleanupFailedDirectReplace(ctx context.Context, dest destination.Destination, table string, createdByRun bool) {
	if !createdByRun {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := dest.DropTable(cleanupCtx, table); err != nil {
		config.Debug("[REPLACE] Warning: failed to remove target created by unsuccessful replace: %v", err)
	}
}

func newTargetOwnershipToken(dest destination.Destination, targetExisted bool) string {
	if targetExisted {
		return ""
	}
	if _, ok := dest.(destination.OwnedTableDropper); ok {
		return uuid.NewString()
	}
	return ""
}

func cleanupFailedOwnedDirectReplace(
	ctx context.Context,
	dest destination.Destination,
	table string,
	createdByRun bool,
	ownershipToken string,
) {
	if !createdByRun || ownershipToken == "" {
		return
	}
	dropper, ok := dest.(destination.OwnedTableDropper)
	if !ok {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := dropper.DropTableIfOwned(cleanupCtx, table, ownershipToken); err != nil {
		config.Debug("[REPLACE] Warning: refused or failed owned target cleanup: %v", err)
	}
}

func wrapWithRowCount(ctx context.Context, records <-chan source.RecordBatchResult, totalRows *atomic.Int64) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult, cap(records))
	drainTimeout := canceledSourceDrainTimeout
	go func() {
		defer close(out)
		for {
			var result source.RecordBatchResult
			var ok bool
			select {
			case result, ok = <-records:
				if !ok {
					return
				}
			case <-ctx.Done():
				<-startBoundedRecordDrain(records, drainTimeout)
				return
			}
			if result.Err == nil && result.Batch != nil {
				totalRows.Add(result.Batch.NumRows())
			}
			select {
			case out <- result:
			case <-ctx.Done():
				if result.Batch != nil {
					result.Batch.Release()
				}
				<-startBoundedRecordDrain(records, drainTimeout)
				return
			}
		}
	}()
	return out
}

// ExecuteMultiTable implements multi-table replace strategy for CDC sources.
func (s *ReplaceStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}

	useStaging := job.Destination.SupportsAtomicSwap()
	if useStaging && job.CDCStateManager != nil {
		capability, ok := job.Destination.(destination.CDCConditionalSwapCapable)
		if !ok || !capability.SupportsCDCConditionalSwap() {
			return fmt.Errorf("destination scheme %q cannot atomically fence managed CDC table replacement", job.Destination.GetScheme())
		}
	}
	config.Debug("[STRATEGY] Multi-table replace with %d tables, staging=%v", len(job.Tables), useStaging)

	stagingTables := make(map[string]string)
	tableConfigs := make(map[string]destination.TableWriteConfig)
	expectedIncarnations := make(map[string]string)
	createdDirectTargets := make(map[string]ownedPreparedTarget)
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables))

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)
			writeSchema := job.WriteSchemaFor(ti.Name, ti.Schema)
			writeTable := destTable
			if useStaging {
				writeTable = replaceStagingTableName(job.Destination, destTable, job.Config.StagingDataset)
			}
			targetExisted := true
			if !useStaging {
				existingSchema, err := job.Destination.GetTableSchema(ctx, destTable)
				if err != nil {
					errChan <- fmt.Errorf("failed to inspect destination table %s before replace: %w", ti.Name, err)
					return
				}
				targetExisted = existingSchema != nil
			}
			ownershipToken := newTargetOwnershipToken(job.Destination, targetExisted)

			dedup := replaceShouldDedup(job.Destination, ti.PrimaryKeys)
			if !useStaging {
				dedup = directReplaceShouldDedup(job.Destination, ti.PrimaryKeys)
			}

			prepareOpts := destination.PrepareOptions{
				Table:          writeTable,
				Schema:         writeSchema,
				DropFirst:      true,
				PrimaryKeys:    ti.PrimaryKeys,
				OwnershipToken: ownershipToken,
			}
			if useStaging && dedup {
				prepareOpts.PrimaryKeys = nil
			}
			if useStaging {
				prepareOpts.ExpiresAfter = destination.ManagedStagingTTL
				mu.Lock()
				stagingTables[ti.Name] = writeTable
				mu.Unlock()
			}
			if !useStaging && !targetExisted && ownershipToken != "" {
				mu.Lock()
				createdDirectTargets[ti.Name] = ownedPreparedTarget{table: writeTable, ownershipToken: ownershipToken}
				mu.Unlock()
			}

			if err := job.Destination.PrepareTable(ctx, prepareOpts); err != nil {
				errChan <- fmt.Errorf("failed to prepare table %s: %w", ti.Name, err)
				return
			}
			if useStaging && job.CDCStateManager != nil {
				if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
					Table: destTable, Schema: writeSchema, DropFirst: false, PrimaryKeys: ti.PrimaryKeys,
				}); err != nil {
					errChan <- fmt.Errorf("failed to prepare managed replacement target %s: %w", ti.Name, err)
					return
				}
			}
			expectedIncarnation := ""
			if job.CDCStateManager != nil {
				if err := job.CDCStateManager.BindDestinationIncarnation(ctx, ti.Name, destTable); err != nil {
					errChan <- fmt.Errorf("failed to bind replacement destination table %s before extraction: %w", ti.Name, err)
					return
				}
				expectedIncarnation = job.CDCStateManager.BoundDestinationIncarnation(ti.Name)
				if expectedIncarnation == "" {
					errChan <- fmt.Errorf("managed CDC destination table %s has no bound physical incarnation", ti.Name)
					return
				}
			}

			mu.Lock()
			if !useStaging && !targetExisted && ownershipToken == "" {
				createdDirectTargets[ti.Name] = ownedPreparedTarget{table: writeTable}
			}
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:              writeTable,
				Schema:                 writeSchema,
				PrimaryKeys:            ti.PrimaryKeys,
				DeduplicatePrimaryKeys: !useStaging && dedup,
				IncrementalKey:         writeSchema.IncrementalKey,
				CDCMode:                hasCDCColumns(writeSchema),
				SkipCDCResume:          job.CDCStateManager != nil,
				CDCExpectedIncarnation: expectedIncarnation,
			}
			expectedIncarnations[ti.Name] = expectedIncarnation
			mu.Unlock()
		}(tableInfo)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
		return err
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:         parallelism,
			PageSize:            job.Config.PageSize,
			Limit:               job.Config.SQLLimit,
			CDCSlotSuffix:       job.Config.CDCSlotSuffix,
			CDCLegacySlotSuffix: job.Config.CDCLegacySlotSuffix,
			FullRefresh:         job.Config.FullRefresh,
		},
		CDCResumeLSNs: job.CDCResumeLSNs,
	})
	if err != nil {
		cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}
	if !useStaging {
		// Once writers are launched, any error can be a lost response after a
		// durable direct replacement. Ownership alone cannot distinguish that
		// from a pre-commit failure, so preserve all attempted targets.
		createdDirectTargets = nil
	}

	if err := multitable.Write(readCtx, job.Destination, records, destination.MultiTableWriteOptions{
		TableConfigs:       tableConfigs,
		Parallelism:        job.Config.EffectiveDestinationParallelism(),
		StagingTable:       useStaging,
		StagingBucket:      job.Config.StagingBucket,
		LoaderFileSize:     job.Config.LoaderFileSize,
		LoaderFileFormat:   job.Config.LoaderFileFormat,
		CancelSource:       cancelRead,
		CancelDrainTimeout: canceledSourceDrainTimeout,
	}); err != nil {
		cancelRead()
		<-startBoundedRecordDrain(records, canceledSourceDrainTimeout)
		cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}
	if !useStaging && job.CDCStateManager != nil {
		for _, tableInfo := range job.Tables {
			if err := job.CDCStateManager.BindDestinationIncarnation(ctx, tableInfo.Name, job.GetDestTableName(tableInfo.Name)); err != nil {
				return fmt.Errorf("failed to bind replaced destination table %s: %w", tableInfo.Name, err)
			}
		}
	}

	if useStaging {
		for _, tableInfo := range job.Tables {
			writeSchema := job.WriteSchemaFor(tableInfo.Name, tableInfo.Schema)
			destTable := job.GetDestTableName(tableInfo.Name)
			stagingTable := stagingTables[tableInfo.Name]
			swapTable := stagingTable
			swapPrimaryKeys := tableInfo.PrimaryKeys
			if replaceShouldDedup(job.Destination, tableInfo.PrimaryKeys) {
				normalised, err := deduplicateStaging(ctx, job.Destination, stagingTable, destTable,
					job.Config.StagingDataset, writeSchema.IncrementalKey, writeSchema, tableInfo.PrimaryKeys, "", nil, job.CDCStateManager != nil)
				if err != nil {
					cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
					return fmt.Errorf("failed to deduplicate table %s: %w", tableInfo.Name, err)
				}
				swapTable = normalised
				swapPrimaryKeys = nil
				stagingTables[tableInfo.Name] = normalised
			}
			publishedIncarnation := ""
			if job.CDCStateManager != nil {
				publishedIncarnation, err = job.CDCStateManager.DestinationIncarnationForPublication(ctx, swapTable)
				if err != nil {
					cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
					return fmt.Errorf("failed to identify replacement table %s before publication: %w", tableInfo.Name, err)
				}
			}

			if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
				StagingTable:                  swapTable,
				TargetTable:                   destTable,
				PrimaryKeys:                   swapPrimaryKeys,
				Schema:                        writeSchema,
				CDCExpectedIncarnation:        expectedIncarnations[tableInfo.Name],
				CDCExpectedStagingIncarnation: publishedIncarnation,
			}); err != nil {
				cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
				return fmt.Errorf("failed to swap table %s: %w", tableInfo.Name, err)
			}
			delete(stagingTables, tableInfo.Name)
			if job.CDCStateManager != nil {
				if err := job.CDCStateManager.BindPublishedDestinationIncarnation(ctx, tableInfo.Name, destTable, expectedIncarnations[tableInfo.Name], publishedIncarnation); err != nil {
					cleanupFailedMultiTableReplace(ctx, job.Destination, useStaging, stagingTables, createdDirectTargets)
					return fmt.Errorf("failed to bind replaced destination table %s: %w", tableInfo.Name, err)
				}
			}
		}
	}

	return nil
}

func cleanupFailedMultiTableReplace(
	ctx context.Context,
	dest destination.Destination,
	useStaging bool,
	stagingTables map[string]string,
	createdDirectTargets map[string]ownedPreparedTarget,
) {
	if useStaging {
		for _, table := range stagingTables {
			cleanupFailedDirectReplace(ctx, dest, table, true)
		}
		return
	}
	for _, target := range createdDirectTargets {
		cleanupFailedOwnedDirectReplace(ctx, dest, target.table, true, target.ownershipToken)
	}
}
