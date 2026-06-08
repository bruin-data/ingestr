package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type MergeStrategy struct{}

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
	stagingTable := GenerateStagingTableName(job.Config.DestTable, "merge", job.Config.StagingDataset)
	fmt.Printf("[MERGE] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)
	isCDC := hasCDCColumns(job.Schema)
	if isCDC {
		if cdcAware, ok := job.Destination.(destination.CDCMergeAware); !ok || !cdcAware.SupportsCDCMerge() {
			fmt.Printf("Warning: CDC data detected but the destination does not support CDC-aware merge; deleted rows will be inserted as regular data with _cdc_deleted=true instead of being processed as deletes\n")
		}
	}

	destSchema := destination.DestinationTableSchema(job.Schema)

	// Ensure destination table exists (don't drop it)
	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      destSchema,
		DropFirst:   false,
		PrimaryKeys: job.Config.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table: %w", err)
	}

	// Create staging table with same schema
	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:        stagingTable,
		Schema:       job.Schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		CDCMode:      isCDC, // Allow NULLs for CDC deletes in staging
		PartitionBy:  job.Config.PartitionBy,
		ClusterBy:    job.Config.ClusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table: %w", err)
	}

	// Read from source
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
		CDCResumeLSN:   job.Config.CDCResumeLSN,  // For CDC incremental resume
		CDCSlotSuffix:  job.Config.CDCSlotSuffix, // Destination-aware slot suffix
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Apply batch transformation for discard_row/discard_value modes
	records, err = job.ApplyBatchTransformation(ctx, records)
	if err != nil {
		return fmt.Errorf("failed to apply batch transformation: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	// Write to staging table using parallel writes
	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		return fmt.Errorf("failed to write to staging: %w", err)
	}

	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
	}

	// Perform merge: UPDATE existing + INSERT new
	// Note: We only use source columns here. Destination-only columns (removed columns)
	// will naturally receive NULL for new rows and remain unchanged for existing rows.
	config.Debug("[MERGE] Executing merge operation")
	if err := job.Destination.MergeTable(ctx, destination.MergeOptions{
		StagingTable: stagingTable,
		TargetTable:  job.Config.DestTable,
		PrimaryKeys:  job.Config.PrimaryKeys,
		Columns:      job.Schema.ColumnNames(),
	}); err != nil {
		return fmt.Errorf("failed to merge data: %w", err)
	}

	// Drop staging table (skip when KeepStaging is set for test inspection).
	if !job.Config.KeepStaging {
		if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
			config.Debug("[MERGE] Warning: failed to drop staging table: %v", err)
		}
	}

	return nil
}

// ExecuteMultiTable implements multi-table merge strategy for CDC sources.
func (s *MergeStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}

	config.Debug("[STRATEGY] Multi-table merge with %d tables", len(job.Tables))

	anyTableHasCDC := false
	for _, tableInfo := range job.Tables {
		if hasCDCColumns(tableInfo.Schema) {
			anyTableHasCDC = true
			break
		}
	}
	if anyTableHasCDC {
		if cdcAware, ok := job.Destination.(destination.CDCMergeAware); !ok || !cdcAware.SupportsCDCMerge() {
			fmt.Printf("Warning: CDC data detected but the destination does not support CDC-aware merge; deleted rows will be inserted as regular data with _cdc_deleted=true instead of being processed as deletes\n")
		}
	}

	stagingTables := make(map[string]string)
	tableConfigs := make(map[string]destination.TableWriteConfig)
	var mu sync.Mutex

	var wg sync.WaitGroup
	errChan := make(chan error, len(job.Tables)*2)

	for _, tableInfo := range job.Tables {
		wg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer wg.Done()

			destTable := job.GetDestTableName(ti.Name)
			stagingTable := GenerateStagingTableName(destTable, "merge", job.Config.StagingDataset)
			isCDC := hasCDCColumns(ti.Schema)

			tableDestSchema := destination.DestinationTableSchema(ti.Schema)

			if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
				Table:       destTable,
				Schema:      tableDestSchema,
				DropFirst:   false,
				PrimaryKeys: ti.PrimaryKeys,
			}); err != nil {
				errChan <- fmt.Errorf("failed to prepare destination table %s: %w", ti.Name, err)
				return
			}

			if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
				Table:        stagingTable,
				Schema:       ti.Schema,
				DropFirst:    true,
				PrimaryKeys:  nil,
				CDCMode:      isCDC, // Make non-PK columns nullable for CDC staging tables
				ExpiresAfter: destination.ManagedStagingTTL,
			}); err != nil {
				errChan <- fmt.Errorf("failed to prepare staging table for %s: %w", ti.Name, err)
				return
			}

			mu.Lock()
			stagingTables[ti.Name] = stagingTable
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:   stagingTable,
				Schema:      ti.Schema,
				PrimaryKeys: nil,
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

	records, err := job.Source.ReadAll(ctx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:   parallelism,
			PageSize:      job.Config.PageSize,
			Limit:         job.Config.SQLLimit,
			CDCSlotSuffix: job.Config.CDCSlotSuffix,
		},
		CDCResumeLSNs: job.CDCResumeLSNs,
	})
	if err != nil {
		for _, stagingTable := range stagingTables {
			_ = job.Destination.DropTable(ctx, stagingTable)
		}
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := multitable.Write(ctx, job.Destination, records, destination.MultiTableWriteOptions{
		TableConfigs:     tableConfigs,
		Parallelism:      parallelism,
		StagingTable:     true,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		for _, stagingTable := range stagingTables {
			_ = job.Destination.DropTable(ctx, stagingTable)
		}
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}

	mergeErrChan := make(chan error, len(job.Tables))
	var mergeWg sync.WaitGroup

	for _, tableInfo := range job.Tables {
		mergeWg.Add(1)
		go func(ti source.SourceTableInfo) {
			defer mergeWg.Done()

			destTable := job.GetDestTableName(ti.Name)
			stagingTable := stagingTables[ti.Name]

			if err := job.Destination.MergeTable(ctx, destination.MergeOptions{
				StagingTable: stagingTable,
				TargetTable:  destTable,
				PrimaryKeys:  ti.PrimaryKeys,
				Columns:      ti.Schema.ColumnNames(),
			}); err != nil {
				mergeErrChan <- fmt.Errorf("failed to merge table %s: %w", ti.Name, err)
				return
			}

			if !job.Config.KeepStaging {
				if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
					config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", stagingTable, err)
				}
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
			if err := job.Destination.DropTable(ctx, stagingTable); err != nil {
				config.Debug("[MERGE] Warning: failed to drop staging table %s: %v", stagingTable, err)
			}
		}
		return mergeErr
	}

	return nil
}

// hasCDCColumns checks if a schema has CDC columns (specifically _cdc_deleted).
// This is used to detect CDC sources for special merge handling.
func hasCDCColumns(s *schema.TableSchema) bool {
	if s == nil {
		return false
	}
	for _, col := range s.Columns {
		if col.Name == destination.CDCDeletedColumn {
			return true
		}
	}
	return false
}
