package strategy

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/source"
)

type ReplaceStrategy struct{}

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
		writeTable = GenerateStagingTableName(targetTable, "staging", job.Config.StagingDataset)
		fmt.Printf("[STRATEGY] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), writeTable)
	} else {
		config.Debug("[STRATEGY] Direct write to target (no staging): %s", writeTable)
	}

	prepareOpts := destination.PrepareOptions{
		Table:       writeTable,
		Schema:      job.Schema,
		DropFirst:   true,
		PrimaryKeys: job.Config.PrimaryKeys,
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

	// Apply batch transformation for discard_row/discard_value modes
	records, err = job.ApplyBatchTransformation(ctx, records)
	if err != nil {
		return fmt.Errorf("failed to apply batch transformation: %w", err)
	}

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	writeOpts := destination.WriteOptions{
		Table:            writeTable,
		Parallelism:      parallelism,
		StagingTable:     useStaging,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}
	if useStaging {
		if atomicWriter, ok := job.Destination.(destination.AtomicCommitWriter); ok && atomicWriter.SupportsAtomicCommitWrites() {
			writeOpts.AtomicCommit = true
		}
	}

	var countedRows atomic.Int64
	if !writeOpts.AtomicCommit {
		records = wrapWithRowCount(records, &countedRows)
	}

	if err := job.Destination.WriteParallel(ctx, records, writeOpts); err != nil {
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
		if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
			StagingTable:   writeTable,
			TargetTable:    targetTable,
			PrimaryKeys:    job.Config.PrimaryKeys,
			IncrementalKey: job.Config.IncrementalKey,
		}); err != nil {
			if dropErr := job.Destination.DropTable(ctx, writeTable); dropErr != nil {
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
				writeTable = GenerateStagingTableName(destTable, "staging", job.Config.StagingDataset)
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

	records, err := job.Source.ReadAll(ctx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism: parallelism,
			PageSize:    job.Config.PageSize,
			Limit:       job.Config.SQLLimit,
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
		Parallelism:      parallelism,
		StagingTable:     useStaging,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
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
			if err := job.Destination.SwapTable(ctx, destination.SwapOptions{
				StagingTable: stagingTable,
				TargetTable:  destTable,
				PrimaryKeys:  tableInfo.PrimaryKeys,
			}); err != nil {
				return fmt.Errorf("failed to swap table %s: %w", tableInfo.Name, err)
			}
		}
	}

	return nil
}
