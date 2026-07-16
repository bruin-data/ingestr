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
)

// replaceShouldDedup reports whether deduplicated replace applies for the
// destination: it can merge and isn't ClickHouse (which deduplicates natively
// via its table engine).
func replaceShouldDedup(dest destination.Destination, primaryKeys []string) bool {
	return len(primaryKeys) > 0 &&
		dest.SupportsMergeStrategy() &&
		dest.GetScheme() != "clickhouse"
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

	mode, err := schemaevolution.ParseContractMode(job.Config.SchemaContract)
	if err == nil && mode == schemaevolution.ContractDiscardValue && schemaChangesTouchPrimaryKeys(job.SchemaComparison, job.Config.PrimaryKeys) {
		return true
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
func deduplicateStaging(ctx context.Context, dest destination.Destination, rawTable, targetTable, stagingDataset, incrementalKey string, tableSchema *schema.TableSchema, primaryKeys []string, partitionBy string, clusterBy []string) (string, error) {
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
		if dropErr := dest.DropTable(ctx, rawTable); dropErr != nil {
			config.Debug("[REPLACE] Warning: failed to drop staging table: %v", dropErr)
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
	}); err != nil {
		for _, t := range []string{rawTable, normalised} {
			if dropErr := dest.DropTable(ctx, t); dropErr != nil {
				config.Debug("[REPLACE] Warning: failed to drop staging table: %v", dropErr)
			}
		}
		return "", fmt.Errorf("failed to deduplicate staging table: %w", err)
	}
	if dropErr := dest.DropTable(ctx, rawTable); dropErr != nil {
		config.Debug("[REPLACE] Warning: failed to drop raw staging table: %v", dropErr)
	}
	return normalised, nil
}

type ReplaceStrategy struct{}

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

	targetTable := job.Config.DestTable
	writeTable := targetTable
	if useStaging {
		writeTable = replaceStagingTableName(job.Destination, targetTable, job.Config.StagingDataset)
		output.Statusf("[STRATEGY] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), writeTable)
	} else {
		config.Debug("[STRATEGY] Direct write to target (no staging): %s", writeTable)
	}

	// Deduplicated replace: Load a PK-free staging table so duplicate keys can land.
	dedup := useStaging &&
		!sourcePrimaryKeysSafeForReplaceFastPath(job) &&
		replaceShouldDedup(job.Destination, job.Config.PrimaryKeys)
	stagingPrimaryKeys := job.Config.PrimaryKeys
	if dedup {
		stagingPrimaryKeys = nil
	}

	prepareOpts := destination.PrepareOptions{
		Table:       writeTable,
		Schema:      job.Schema,
		DropFirst:   true,
		PrimaryKeys: stagingPrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}
	if useStaging {
		prepareOpts.ExpiresAfter = destination.ManagedStagingTTL
	}

	if err := job.Destination.PrepareTable(ctx, prepareOpts); err != nil {
		return fmt.Errorf("failed to prepare table: %w", err)
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
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	writeOpts := destination.WriteOptions{
		Table:            writeTable,
		Schema:           job.Schema,
		Parallelism:      job.Config.EffectiveDestinationParallelism(),
		StagingTable:     useStaging,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		PreStaged:        job.PreStaged,
	}
	if useStaging {
		if atomicWriter, ok := job.Destination.(destination.AtomicCommitWriter); ok && atomicWriter.SupportsAtomicCommitWrites() {
			writeOpts.AtomicCommit = true
		}
	}

	var countedRows atomic.Int64
	if !writeOpts.AtomicCommit && writeOpts.PreStaged == nil {
		records = wrapWithRowCount(records, &countedRows)
	}

	if err := job.Destination.WriteParallel(ctx, records, writeOpts); err != nil {
		cancelRead()
		drainAndReleaseUntil(records, streamAbortDrainTimeout)
		if useStaging {
			if dropErr := job.Destination.DropTable(ctx, writeTable); dropErr != nil {
				config.Debug("[REPLACE] Warning: failed to drop staging table: %v", dropErr)
			}
		}
		return fmt.Errorf("failed to write data: %w", err)
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
						if dropErr := job.Destination.DropTable(ctx, writeTable); dropErr != nil {
							config.Debug("[REPLACE] Warning: failed to drop staging table: %v", dropErr)
						}
						return fmt.Errorf("failed to verify staging table row count: %w", err)
					}
				}
			}
		}
		swapTable := writeTable
		swapPrimaryKeys := job.Config.PrimaryKeys
		if dedup {
			normalised, err := deduplicateStaging(ctx, job.Destination, writeTable, targetTable,
				job.Config.StagingDataset, job.Config.IncrementalKey, job.Schema,
				job.Config.PrimaryKeys, job.Config.PartitionBy, job.Config.ClusterBy)
			if err != nil {
				return err
			}
			swapTable = normalised
			swapPrimaryKeys = nil
		}

		if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
			StagingTable:   swapTable,
			TargetTable:    targetTable,
			PrimaryKeys:    swapPrimaryKeys,
			IncrementalKey: job.Config.IncrementalKey,
			Schema:         job.Schema,
		}); err != nil {
			if dropErr := job.Destination.DropTable(ctx, swapTable); dropErr != nil {
				config.Debug("[REPLACE] Warning: failed to drop staging table: %v", dropErr)
			}
			return fmt.Errorf("failed to swap tables: %w", err)
		}
	}

	return nil
}

func wrapWithRowCount(records <-chan source.RecordBatchResult, totalRows *atomic.Int64) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult, cap(records))
	go func() {
		defer close(out)
		for result := range records {
			if result.Err == nil && result.Batch != nil {
				totalRows.Add(result.Batch.NumRows())
			}
			out <- result
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
	config.Debug("[STRATEGY] Multi-table replace with %d tables, staging=%v", len(job.Tables), useStaging)

	stagingTables := make(map[string]string)
	tableConfigs := make(map[string]destination.TableWriteConfig)
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables))

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)
			writeTable := destTable
			if useStaging {
				writeTable = replaceStagingTableName(job.Destination, destTable, job.Config.StagingDataset)
			}

			prepareOpts := destination.PrepareOptions{
				Table:     writeTable,
				Schema:    ti.Schema,
				DropFirst: true,
			}
			if useStaging {
				prepareOpts.ExpiresAfter = destination.ManagedStagingTTL
			}

			if err := job.Destination.PrepareTable(ctx, prepareOpts); err != nil {
				errChan <- fmt.Errorf("failed to prepare table %s: %w", ti.Name, err)
				return
			}

			mu.Lock()
			stagingTables[ti.Name] = writeTable
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable: writeTable,
				Schema:    ti.Schema,
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
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := multitable.Write(ctx, job.Destination, records, destination.MultiTableWriteOptions{
		TableConfigs:     tableConfigs,
		Parallelism:      job.Config.EffectiveDestinationParallelism(),
		StagingTable:     useStaging,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
		CancelSource:     cancelRead,
	}); err != nil {
		for _, stagingTable := range stagingTables {
			if dropErr := job.Destination.DropTable(ctx, stagingTable); dropErr != nil {
				config.Debug("[REPLACE] Warning: failed to drop staging table %s: %v", stagingTable, dropErr)
			}
		}
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}

	if useStaging {
		for _, tableInfo := range job.Tables {
			destTable := job.GetDestTableName(tableInfo.Name)
			stagingTable := stagingTables[tableInfo.Name]
			swapTable := stagingTable
			swapPrimaryKeys := tableInfo.PrimaryKeys
			if replaceShouldDedup(job.Destination, tableInfo.PrimaryKeys) {
				normalised, err := deduplicateStaging(ctx, job.Destination, stagingTable, destTable,
					job.Config.StagingDataset, tableInfo.Schema.IncrementalKey, tableInfo.Schema, tableInfo.PrimaryKeys, "", nil)
				if err != nil {
					return fmt.Errorf("failed to deduplicate table %s: %w", tableInfo.Name, err)
				}
				swapTable = normalised
				swapPrimaryKeys = nil
			}

			if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
				StagingTable: swapTable,
				TargetTable:  destTable,
				PrimaryKeys:  swapPrimaryKeys,
				Schema:       tableInfo.Schema,
			}); err != nil {
				return fmt.Errorf("failed to swap table %s: %w", tableInfo.Name, err)
			}
		}
	}

	return nil
}
