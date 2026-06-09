package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/progress"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

type IngestionJob struct {
	Config       *config.IngestConfig
	Table        source.SourceTable
	Destination  destination.Destination
	Schema       *schema.TableSchema
	SourceSchema *schema.TableSchema // Original source schema without extra ingestr metadata columns
	Tracker      progress.Tracker    // Progress tracker for monitoring ingestion

	// BufferedRecords contains pre-read data for schema-unknown sources.
	// If non-nil, GetRecords() returns this instead of reading from Table.
	BufferedRecords <-chan source.RecordBatchResult

	// SchemaComparison contains the result of comparing source and destination schemas.
	// Used for runtime batch transformation in discard_row and discard_value modes.
	SchemaComparison *schemaevolution.SchemaComparison

	// DestinationSchema is the destination table schema if it exists.
	// Required for row filtering in discard_row mode.
	DestinationSchema *schema.TableSchema

	// ColumnRenamer transforms column names to match the naming convention.
	// Used for ingestr compatibility when destination was created with snake_case naming.
	ColumnRenamer *transformer.ColumnRenamer

	// IngestrColumnFiller adds ingestr metadata columns with "-" values to batches.
	IngestrColumnFiller *schemaevolution.IngestrColumnFiller

	// TypeCaster casts record batch columns to match destination types when
	// --columns specifies type overrides for known-schema sources.
	TypeCaster *transformer.TypeCaster

	// ColumnMasker replaces values in user-specified columns (e.g. passwords).
	ColumnMasker *transformer.ColumnMasker

	// EvolutionPlan holds the deferred schema evolution to apply on the destination.
	EvolutionPlan *schemaevolution.EvolutionPlan
}

// GetRecords returns either buffered records (for schema-unknown sources)
// or reads from the table directly (for schema-known sources).
func (j *IngestionJob) GetRecords(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if j.BufferedRecords != nil {
		return j.BufferedRecords, nil
	}
	return j.Table.Read(ctx, opts)
}

// ApplyEvolution applies the pending schema evolution plan to the destination.
func (j *IngestionJob) ApplyEvolution(ctx context.Context) error {
	if j.EvolutionPlan == nil || !j.EvolutionPlan.HasMigration() {
		return nil
	}
	return j.EvolutionPlan.Apply(ctx, j.Destination)
}

type WriteStrategy interface {
	Name() config.IncrementalStrategy
	Validate(cfg *config.IngestConfig) error
	Execute(ctx context.Context, job *IngestionJob) error
	RequiresPrimaryKey() bool
	RequiresIncrementalKey() bool
}

// MultiTableIngestionJob represents an ingestion job for multiple tables.
// Used by CDC sources that emit data from multiple tables concurrently.
type MultiTableIngestionJob struct {
	Config         *config.IngestConfig
	Source         source.MultiTableSource
	Destination    destination.Destination
	Tables         []source.SourceTableInfo
	TableDestNames map[string]string // source table → dest table mapping
	Tracker        progress.Tracker
	CDCResumeLSNs  map[string]string // Per-table CDC resume LSNs: source table → max LSN already processed
	EvolutionPlans map[string]*schemaevolution.EvolutionPlan
}

// GetDestTableName returns the destination table name for a source table.
func (j *MultiTableIngestionJob) GetDestTableName(sourceTable string) string {
	if mapping, ok := j.TableDestNames[sourceTable]; ok {
		return mapping
	}
	return sourceTable
}

// ApplyEvolution applies the pending schema evolution plan for a source table.
func (j *MultiTableIngestionJob) ApplyEvolution(ctx context.Context, sourceTable string) error {
	if j.EvolutionPlans == nil {
		return nil
	}
	plan := j.EvolutionPlans[sourceTable]
	if plan == nil || !plan.HasMigration() {
		return nil
	}
	return plan.Apply(ctx, j.Destination)
}

// MultiTableStrategy extends WriteStrategy for multi-table sources.
// Strategies that support multi-table CDC should implement this interface.
type MultiTableStrategy interface {
	WriteStrategy
	ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error
}

var registry = make(map[config.IncrementalStrategy]WriteStrategy)

func Register(strategy WriteStrategy) {
	registry[strategy.Name()] = strategy
}

func Get(name config.IncrementalStrategy) (WriteStrategy, error) {
	strategy, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown strategy: %s", name)
	}
	return strategy, nil
}

// ApplyBatchTransformation wraps record batches with contract-based transformation if needed.
// Also applies column renaming if a naming convention is configured.
func (j *IngestionJob) ApplyBatchTransformation(ctx context.Context, records <-chan source.RecordBatchResult) (<-chan source.RecordBatchResult, error) {
	// Cast column types first (for --columns type overrides on known-schema sources)
	if j.TypeCaster != nil {
		records = transformer.Wrap(records, j.TypeCaster)
	}

	// Apply column renaming (if configured)
	if j.ColumnRenamer != nil && j.ColumnRenamer.HasRenames() {
		records = transformer.Wrap(records, j.ColumnRenamer)
	}

	// Apply column masking
	if j.ColumnMasker != nil && j.ColumnMasker.HasMasks() {
		records = transformer.Wrap(records, j.ColumnMasker)
	}

	// Determine if schema contract transformation is needed
	contractMode, err := schemaevolution.ParseContractMode(j.Config.SchemaContract)
	if err != nil {
		return nil, fmt.Errorf("invalid schema contract mode: %w", err)
	}

	var batchTransformer schemaevolution.BatchTransformer

	switch contractMode {
	case schemaevolution.ContractDiscardValue:
		if j.SchemaComparison != nil && j.SchemaComparison.HasChanges && j.DestinationSchema != nil {
			// For discard_value, we need the ORIGINAL schema comparison (including type changes)
			// to know which columns to NULL out, even though we don't apply type migrations
			batchTransformer = schemaevolution.NewDiscardValueTransformer(j.SchemaComparison, j.Schema, j.DestinationSchema)
		}

	case schemaevolution.ContractDiscardRow:
		if j.SchemaComparison != nil && j.SchemaComparison.HasChanges {
			batchTransformer = schemaevolution.NewDiscardRowTransformer(j.Schema, j.DestinationSchema, j.SchemaComparison)
		}
	}

	if batchTransformer != nil {
		records = schemaevolution.TransformBatchStream(ctx, records, batchTransformer)
	}

	if j.IngestrColumnFiller != nil && j.IngestrColumnFiller.HasColumns() {
		records = schemaevolution.TransformBatchStream(ctx, records, j.IngestrColumnFiller)
	}

	return records, nil
}

func init() {
	Register(&ReplaceStrategy{})
	Register(&TruncateInsertStrategy{})
	Register(&AppendStrategy{})
	Register(&MergeStrategy{})
	Register(&DeleteInsertStrategy{})
	Register(&SCD2Strategy{})
}
