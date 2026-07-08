package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/progress"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/stats"
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
	// If non-nil, GetRecords() transforms this stream instead of reading from Table.
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

	// WhitespaceTrimmer trims string values when --trim-whitespace is enabled.
	WhitespaceTrimmer *transformer.WhitespaceTrimmer

	// LoadTimestamp adds or replaces _ingestr_loaded_at with one timestamp for the job.
	LoadTimestamp *transformer.LoadTimestamp

	// SchemaAligner reorders and fills transformed batches to match the write schema.
	SchemaAligner *transformer.TypeCaster

	// EvolutionPlan holds the deferred schema evolution to apply on the destination.
	EvolutionPlan *schemaevolution.EvolutionPlan

	// StatsCollector records best-effort per-table row counts when --stats is enabled.
	StatsCollector *stats.Collector
}

// GetRecords returns the transformed record stream for this job.
func (j *IngestionJob) GetRecords(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	var records <-chan source.RecordBatchResult
	if j.BufferedRecords != nil {
		records = j.BufferedRecords
	} else {
		var err error
		records, err = j.Table.Read(ctx, opts)
		if err != nil {
			return nil, err
		}
	}
	records, err := j.ApplyBatchTransformation(ctx, records)
	if err != nil {
		return nil, err
	}
	if j.StatsCollector != nil {
		records = j.StatsCollector.Wrap(j.Table.Name(), records)
	}
	return records, nil
}

// ApplyEvolution applies the pending schema evolution plan to the destination.
// The destination turns the abstract plan into DDL and executes it; this
// strategy layer only decides when the plan runs.
func (j *IngestionJob) ApplyEvolution(ctx context.Context) error {
	return applyEvolutionPlan(ctx, j.Destination, j.EvolutionPlan)
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
	CDCResumeLSNs  map[string]string                         // Per-table CDC resume LSNs: source table → max LSN already processed
	EvolutionPlans map[string]*schemaevolution.EvolutionPlan // Per-table schema evolution plans: source table → plan

	// WhitespaceTrimmer trims string values when --trim-whitespace is enabled.
	WhitespaceTrimmer *transformer.WhitespaceTrimmer

	// LoadTimestamp adds or replaces _ingestr_loaded_at with one timestamp for the job.
	LoadTimestamp *transformer.LoadTimestamp

	// StatsCollector records best-effort per-table row counts when --stats is enabled.
	StatsCollector *stats.Collector
}

// ApplyEvolutionFor applies the pending schema evolution plan for a source table.
func (j *MultiTableIngestionJob) ApplyEvolutionFor(ctx context.Context, sourceTable string) error {
	return applyEvolutionPlan(ctx, j.Destination, j.EvolutionPlans[sourceTable])
}

// applyEvolutionPlan asks the destination to apply an abstract evolution plan.
// Destinations that cannot evolve schemas (do not implement SchemaEvolver) are
// silently skipped, matching the previous no-dialect behavior.
func applyEvolutionPlan(ctx context.Context, dest destination.Destination, plan *schemaevolution.EvolutionPlan) error {
	if plan == nil || !plan.HasChanges() {
		return nil
	}
	evolver, ok := dest.(schemaevolution.SchemaEvolver)
	if !ok {
		return nil
	}
	ctx = annotation.WithStep(ctx, annotation.StepEvolve)
	warnings, err := evolver.ApplySchemaEvolution(ctx, plan.Table, plan.Comparison)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Printf("Warning: %s\n", w)
	}
	// Mark the plan applied so a repeat call is a no-op. This mirrors the prior
	// EvolutionPlan.Apply contract (which cleared its rendered statements) and
	// prevents re-issuing ADD COLUMN/ALTER on a double-apply.
	plan.Comparison = nil
	return nil
}

// GetDestTableName returns the destination table name for a source table.
func (j *MultiTableIngestionJob) GetDestTableName(sourceTable string) string {
	if mapping, ok := j.TableDestNames[sourceTable]; ok {
		return mapping
	}
	return sourceTable
}

func (j *MultiTableIngestionJob) ReadAll(ctx context.Context, opts source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	records, err := j.Source.ReadAll(ctx, opts)
	if err != nil {
		return nil, err
	}
	records = j.ApplyBatchTransformation(records)
	if j.StatsCollector != nil {
		records = j.StatsCollector.Wrap("", records)
	}
	return records, nil
}

func (j *MultiTableIngestionJob) ApplyBatchTransformation(records <-chan source.RecordBatchResult) <-chan source.RecordBatchResult {
	if j.WhitespaceTrimmer != nil {
		records = transformer.Wrap(records, j.WhitespaceTrimmer)
	}
	return j.ApplyLoadTimestamp(records)
}

func (j *MultiTableIngestionJob) ApplyLoadTimestamp(records <-chan source.RecordBatchResult) <-chan source.RecordBatchResult {
	if j.LoadTimestamp != nil {
		records = transformer.Wrap(records, j.LoadTimestamp)
	}
	return records
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

	if j.WhitespaceTrimmer != nil {
		records = transformer.Wrap(records, j.WhitespaceTrimmer)
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

	if j.LoadTimestamp != nil {
		records = transformer.Wrap(records, j.LoadTimestamp)
	}

	if j.SchemaAligner != nil {
		records = transformer.Wrap(records, j.SchemaAligner)
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
