package strategy

import (
	"context"
	"fmt"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/multitable"
	"github.com/bruin-data/ingestr/pkg/source"
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
		CDCSlotSuffix:                   job.Config.CDCSlotSuffix,
		CDCSnapshotReplace:              isCDC && supportsCDCSnapshotReplace(job.Destination),
		FullRefresh:                     job.Config.FullRefresh,
	}

	records, err := job.GetRecords(ctx, readOpts)
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
	}
	var writeErr error
	if isCDC {
		_, writeErr = destination.WriteWithTruncateBoundaries(ctx, job.Destination, records, writeOpts)
	} else {
		writeErr = job.Destination.WriteParallel(ctx, records, writeOpts)
	}
	if writeErr != nil {
		return fmt.Errorf("failed to write data: %w", writeErr)
	}

	return nil
}

// ExecuteMultiTable implements multi-table append strategy for CDC sources.
func (s *AppendStrategy) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
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
			isCDC := hasCDCColumns(ti.Schema)
			prepSchema := destination.DestinationTableSchema(ti.Schema)
			if isCDC {
				prepSchema = ti.Schema
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

			mu.Lock()
			tableConfigs[ti.Name] = destination.TableWriteConfig{
				DestTable:   destTable,
				Schema:      ti.Schema,
				PrimaryKeys: ti.PrimaryKeys,
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

	records, err := job.ReadAll(ctx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:        parallelism,
			PageSize:           job.Config.PageSize,
			Limit:              job.Config.SQLLimit,
			CDCSlotSuffix:      job.Config.CDCSlotSuffix,
			CDCSnapshotReplace: anyTableHasCDC && supportsCDCSnapshotReplace(job.Destination),
			FullRefresh:        job.Config.FullRefresh,
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
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		return fmt.Errorf("failed to write multi-table data: %w", err)
	}

	return nil
}
