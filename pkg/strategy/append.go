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

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      destination.DestinationTableSchema(job.Schema),
		DropFirst:   false,
		PrimaryKeys: job.Schema.PrimaryKeys,
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
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

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            job.Config.DestTable,
		Schema:           job.Schema,
		Parallelism:      parallelism,
		StagingBucket:    job.Config.StagingBucket,
		LoaderFileSize:   job.Config.LoaderFileSize,
		LoaderFileFormat: job.Config.LoaderFileFormat,
	}); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
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

			if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
				Table:       destTable,
				Schema:      destination.DestinationTableSchema(ti.Schema),
				DropFirst:   false,
				PrimaryKeys: ti.PrimaryKeys,
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

	records = job.ApplyBatchTransformation(records)

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
