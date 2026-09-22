package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// executeReverseETL runs any strategy against a reverse-ETL destination: it reads
// the source and streams each batch to Write, tagged with the strategy + policies.
func executeReverseETL(ctx context.Context, job *IngestionJob, strategyName config.IncrementalStrategy) error {
	config.Debug("[REVERSE-ETL] strategy=%s dest-table=%s", strategyName, job.Config.DestTable)

	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      destination.DestinationTableSchema(job.Schema),
		PrimaryKeys: job.Schema.PrimaryKeys,
		Strategy:    string(strategyName),
	}); err != nil {
		return fmt.Errorf("failed to prepare table: %w", err)
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	records, err := job.GetRecords(ctx, source.ReadOptions{
		IncrementalKey: job.Config.IncrementalKey,
		IntervalStart:  job.Config.IntervalStart,
		IntervalEnd:    job.Config.IntervalEnd,
		PageSize:       job.Config.PageSize,
		MaxBatchBytes:  job.Config.MaxBatchBytes,
		Limit:          job.Config.SQLLimit,
		ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism:    parallelism,
		Schema:         job.SourceSchema,
		FullRefresh:    job.Config.FullRefresh,
	})
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	rejectMode := string(job.Config.RejectMode)
	if rejectMode == "" {
		rejectMode = string(config.RejectFail)
	}

	// Reverse-ETL defaults to clearing the field when a source cell is null; only an
	// explicit --write-nulls=false opts into omitting nulls (leaving the value as-is).
	writeNulls := job.Config.WriteNulls || !job.Config.WriteNullsSet

	writeOpts := destination.WriteOptions{
		Table:       job.Config.DestTable,
		Schema:      job.Schema,
		PrimaryKeys: job.Schema.PrimaryKeys,
		Parallelism: job.Config.EffectiveDestinationParallelism(),
		Strategy:    string(strategyName),
		RejectMode:  rejectMode,
		WriteNulls:  writeNulls,
	}
	// WriteParallel dispatches up to Parallelism batches concurrently; the
	// destination's own rate limiter still bounds the actual request rate.
	if err := job.Destination.WriteParallel(ctx, records, writeOpts); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}
	return nil
}

// validateReverseETLReject checks the reject-mode value is one we understand.
// Applies to every RETL strategy.
func validateReverseETLReject(cfg *config.IngestConfig) error {
	switch cfg.RejectMode {
	case "", config.RejectFailFast, config.RejectFail, config.RejectSkip:
		return nil
	default:
		return fmt.Errorf("invalid --reject-mode %q: use fail_fast, fail, or skip", cfg.RejectMode)
	}
}
