package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

type SCD2Strategy struct{}

func (s *SCD2Strategy) Name() config.IncrementalStrategy {
	return config.StrategySCD2
}

func (s *SCD2Strategy) Validate(cfg *config.IngestConfig) error {
	if len(cfg.PrimaryKeys) == 0 {
		return fmt.Errorf("scd2 strategy requires at least one primary_key")
	}
	if strings.TrimSpace(cfg.ExtractPartitionBy) != "" && strings.TrimSpace(cfg.IncrementalKey) == "" {
		return fmt.Errorf("scd2 strategy with extract partitioning requires an incremental_key")
	}
	return nil
}

func (s *SCD2Strategy) RequiresPrimaryKey() bool {
	return true
}

func (s *SCD2Strategy) RequiresIncrementalKey() bool {
	return false
}

func (s *SCD2Strategy) Execute(ctx context.Context, job *IngestionJob) error {
	if hasCDCColumns(job.Schema) {
		return fmt.Errorf("scd2 strategy is not supported for CDC records; use merge or replace")
	}
	// Capture a single timestamp at the start of the process for consistency
	processTimestamp := time.Now().UTC()

	// Generate staging table name
	stagingTable := managedStagingTableName(job.Destination, job.Config.DestTable, "scd2", job.Config.StagingDataset)
	output.Statusf("[SCD2] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), stagingTable)
	config.Debug("[SCD2] Using process timestamp: %s", processTimestamp.Format(time.RFC3339Nano))

	// Extend schema with SCD columns
	extendedSchema := extendSchemaWithSCDColumns(job.Schema)
	destExtendedSchema := extendSchemaWithSCDColumns(destination.DestinationTableSchema(job.Schema))

	// Ensure destination table exists with extended schema (don't drop it)
	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:       job.Config.DestTable,
		Schema:      destExtendedSchema,
		DropFirst:   false,
		PrimaryKeys: nil, // SCD2 tables shouldn't have PKs since we have multiple versions
		PartitionBy: job.Config.PartitionBy,
		ClusterBy:   job.Config.ClusterBy,
	}); err != nil {
		return fmt.Errorf("failed to prepare destination table: %w", err)
	}
	if !job.Config.KeepStaging {
		defer dropManagedStagingDetached(ctx, job.Destination, stagingTable, "SCD2")
	}

	// Create staging table with same extended schema
	if err := job.Destination.PrepareTable(ctx, destination.PrepareOptions{
		Table:        stagingTable,
		Schema:       extendedSchema,
		DropFirst:    true,
		PrimaryKeys:  nil,
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
		FullRefresh:                     job.Config.FullRefresh,
	}

	records, err := job.GetRecords(ctx, readOpts)
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	// Add SCD columns to records using transformer
	adder := transformer.NewColumnAdder(
		transformer.ColumnSpec{
			Column:    schema.Column{Name: "_scd_valid_from", DataType: schema.TypeTimestampTZ, Nullable: false},
			Generator: func(i int, n int64) interface{} { return processTimestamp },
		},
		transformer.ColumnSpec{
			Column:    schema.Column{Name: "_scd_valid_to", DataType: schema.TypeTimestampTZ, Nullable: true},
			Generator: func(i int, n int64) interface{} { return nil },
		},
		transformer.ColumnSpec{
			Column:    schema.Column{Name: "_scd_is_current", DataType: schema.TypeBoolean, Nullable: false},
			Generator: func(i int, n int64) interface{} { return true },
		},
	)
	records = transformer.Wrap(records, adder)

	// Wrap channel with progress tracker if provided
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	// Write to staging table using parallel writes. The write schema must match
	// the staging table, which was created with the SCD columns
	if err := job.Destination.WriteParallel(ctx, records, destination.WriteOptions{
		Table:            stagingTable,
		Schema:           extendedSchema,
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

	// Perform SCD2 merge
	config.Debug("[SCD2] Executing SCD2 merge operation")
	if err := job.Destination.SCD2Table(ctx, destination.SCD2Options{
		StagingTable:   stagingTable,
		TargetTable:    job.Config.DestTable,
		PrimaryKeys:    job.Config.PrimaryKeys,
		Columns:        destination.DestinationColumns(job.Schema.ColumnNames()),
		IncrementalKey: job.Config.IncrementalKey,
		Timestamp:      processTimestamp,
		Schema:         job.Schema,
	}); err != nil {
		return fmt.Errorf("failed to perform SCD2 merge: %w", err)
	}

	return nil
}

func extendSchemaWithSCDColumns(original *schema.TableSchema) *schema.TableSchema {
	scdColumns := []schema.Column{
		{Name: "_scd_valid_from", DataType: schema.TypeTimestampTZ, Nullable: false},
		{Name: "_scd_valid_to", DataType: schema.TypeTimestampTZ, Nullable: true},
		{Name: "_scd_is_current", DataType: schema.TypeBoolean, Nullable: false},
	}

	// Skip SCD columns that already exist (case-insensitive).
	existing := make(map[string]bool, len(original.Columns))
	for _, c := range original.Columns {
		existing[strings.ToLower(c.Name)] = true
	}
	toAdd := make([]schema.Column, 0, len(scdColumns))
	for _, c := range scdColumns {
		if !existing[strings.ToLower(c.Name)] {
			toAdd = append(toAdd, c)
		}
	}

	extended := &schema.TableSchema{
		Name:        original.Name,
		Schema:      original.Schema,
		Columns:     make([]schema.Column, len(original.Columns)+len(toAdd)),
		PrimaryKeys: append([]string(nil), original.PrimaryKeys...),
	}

	copy(extended.Columns, original.Columns)
	copy(extended.Columns[len(original.Columns):], toAdd)

	return extended
}
