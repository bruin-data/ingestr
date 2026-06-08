package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/display"
	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/progress"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"golang.org/x/term"
)

type Pipeline struct {
	config                   *config.IngestConfig
	src                      source.Source
	dest                     destination.Destination
	schemaComparison         *schemaevolution.SchemaComparison // Original schema comparison (all violations)
	filteredSchemaComparison *schemaevolution.SchemaComparison // After contract filtering (for migration)
	destinationSchema        *schema.TableSchema
	columnRenamer            *transformer.ColumnRenamer
	namingMapping            map[string]string // original → normalized column names from naming convention
	ingestrColumnFiller      *schemaevolution.IngestrColumnFiller
	droppedColumns           map[string]bool // columns dropped during schema inference (all-null nullable)
	logWriter                io.Writer
}

func New(cfg *config.IngestConfig) *Pipeline {
	return &Pipeline{
		config: cfg,
	}
}

func (p *Pipeline) SetLogWriter(w io.Writer) {
	p.logWriter = w
}

func (p *Pipeline) Run(ctx context.Context) error {
	// Parse query annotations once and carry the base payload on the context.
	// Destinations read it (plus a per-operation step) to annotate queries for
	// cost attribution. Absent caller annotations just means ingestr's own keys
	// (type, ingestr_step) are emitted without any caller-supplied keys.
	annotations, err := annotation.Parse(p.config.QueryAnnotations)
	if err != nil {
		return err
	}
	ctx = annotation.WithPayload(ctx, annotations)

	src, err := uri.DefaultRegistry.GetSource(p.config.SourceURI)
	if err != nil {
		return fmt.Errorf("failed to get source: %w", err)
	}
	p.src = src

	if err := src.Connect(ctx, p.config.SourceURI); err != nil {
		return fmt.Errorf("failed to connect to source: %w", err)
	}
	defer func() { _ = src.Close(ctx) }()

	dest, err := uri.DefaultRegistry.GetDestination(p.config.DestURI)
	if err != nil {
		return fmt.Errorf("failed to get destination: %w", err)
	}
	p.dest = dest

	if err := dest.Connect(ctx, p.config.DestURI); err != nil {
		return fmt.Errorf("failed to connect to destination: %w", err)
	}
	defer func() { _ = dest.Close(ctx) }()

	// For CDC sources, compute a destination-aware slot suffix
	if isCDCSource(p.config.SourceURI) {
		p.config.CDCSlotSuffix = cdcSlotSuffix(p.config.DestURI)
		config.Debug("[PIPELINE] CDC slot suffix: %s", p.config.CDCSlotSuffix)
	}

	// Check if source is multi-table (only if no specific source table is requested)
	if p.config.SourceTable == "" {
		if mtSource, ok := src.(source.MultiTableSource); ok && mtSource.IsMultiTable() {
			return p.runMultiTable(ctx, mtSource)
		}
	}

	// For CDC sources, check if we can resume from existing data
	if isCDCSource(p.config.SourceURI) && !p.config.FullRefresh {
		if resumeProvider, ok := dest.(destination.CDCResumeProvider); ok {
			maxLSN, err := resumeProvider.GetMaxCDCLSN(ctx, p.config.DestTable)
			if err != nil {
				config.Debug("[PIPELINE] Failed to get max CDC LSN from destination: %v", err)
			} else if maxLSN != "" {
				config.Debug("[PIPELINE] Found existing CDC data, will resume from LSN: %s", maxLSN)
				p.config.CDCResumeLSN = maxLSN
			} else {
				config.Debug("[PIPELINE] No existing CDC data found, will perform full snapshot")
			}
		}
	}

	// Get the source table with user configuration
	// Resolution of PKs, strategy, and incremental key happens inside GetTable
	table, err := src.GetTable(ctx, source.TableRequest{
		Name:           p.config.SourceTable,
		IncrementalKey: p.config.IncrementalKey,
		PrimaryKeys:    p.config.PrimaryKeys,
		Strategy:       p.config.IncrementalStrategy,
	})
	if err != nil {
		return fmt.Errorf("failed to get table: %w", err)
	}

	// Sources that manage incrementality internally resolve their own key in
	// GetTable. Only warn if the user's --incremental-key was actually dropped;
	// a source that adopts it (resolved key matches) needs no warning.
	if src.HandlesIncrementality() && p.config.IncrementalKey != "" && table.IncrementalKey() != p.config.IncrementalKey {
		fmt.Printf("Warning: source handles incrementality internally, ignoring --incremental-key=%s\n", p.config.IncrementalKey)
	}

	if table.Name() == source.CustomQueryTableName {
		if p.config.DestTable == p.config.SourceTable {
			p.config.DestTable = source.CustomQueryTableName
		}
		p.config.SourceTable = source.CustomQueryTableName
	}

	preFetchStrategy := resolveStrategy(p.config, src, table)
	preFetchConfig := *p.config
	preFetchConfig.IncrementalStrategy = preFetchStrategy
	preFetchConfig.IncrementalKey = resolveIncrementalKey(p.config, src, table)
	preFetchConfig.PrimaryKeys = resolvePrimaryKeys(p.config, table)
	preFetchConfig.PartitionBy = resolvePartitionBy(p.config, table)
	display.PrintSummary(&preFetchConfig)

	if shouldWarnCDCStrategy(p.config, preFetchStrategy) {
		fmt.Printf("Warning: CDC source is using %q strategy instead of %q; CDC delete and update operations may not be properly reflected in the destination\n", preFetchStrategy, config.StrategyMerge)
	}

	tracker, err := p.createTracker(ctx)
	if err != nil {
		return err
	}
	if tracker != nil {
		defer tracker.Stop()
	}

	// Check if source has known schema or needs inference
	var tableSchema *schema.TableSchema
	var bufferedRecords <-chan source.RecordBatchResult
	var inferBuffer *databuffer.FileBuffer

	if table.HasKnownSchema() {
		tableSchema, err = table.GetSchema(ctx)
		if err != nil {
			return fmt.Errorf("failed to get schema: %w", err)
		}
	} else if p.config.NoInference {
		tableSchema, err = p.schemaFromColumnOverrides(table)
		if err != nil {
			return err
		}
		config.Debug("[PIPELINE] Schema inference disabled; using %d columns from --columns", len(tableSchema.Columns))
	} else {
		// Schema inference path: read all data first. Buffer is opened later
		config.Debug("[PIPELINE] Source has unknown schema, inferring from data...")
		tableSchema, inferBuffer, err = p.inferSchemaFromData(ctx, table, tracker)
		if err != nil {
			return fmt.Errorf("failed to infer schema: %w", err)
		}
		defer func() {
			if inferBuffer != nil {
				_ = inferBuffer.Close()
			}
		}()
		if tableSchema != nil {
			config.Debug("[PIPELINE] Inferred schema with %d columns", len(tableSchema.Columns))
		} else {
			config.Debug("[PIPELINE] Inferred schema is nil")
			if fallback, err := table.GetSchema(ctx); err == nil && fallback != nil && len(fallback.Columns) > 0 {
				tableSchema = fallback
				config.Debug("[PIPELINE] Using source-provided schema (%d columns) for empty extract", len(fallback.Columns))
			} else if synthetic, err := schemainfer.TableSchemaFromColumnOverrides(p.config.Columns, table.Name(), p.config.SchemaNaming); err != nil {
				return fmt.Errorf("failed to build schema from column overrides: %w", err)
			} else if synthetic != nil {
				pks := p.config.PrimaryKeys
				if len(pks) == 0 {
					pks = table.PrimaryKeys()
				}
				ik := p.config.IncrementalKey
				if ik == "" {
					ik = table.IncrementalKey()
				}
				partitionCol := resolvePartitionBy(p.config, table)
				if err := schemainfer.AddKeyColumnsIfMissing(synthetic, pks, ik, partitionCol, p.config.SchemaNaming); err != nil {
					return fmt.Errorf("failed to add key columns to synthetic schema: %w", err)
				}

				tableSchema = synthetic
				bufferedRecords = emptyRecordChannel()
				fmt.Printf("Warning: table %q produced no rows; creating destination table from --columns\n", table.Name())
				config.Debug("[PIPELINE] Built synthetic schema with %d columns from --columns", len(synthetic.Columns))
			} else {
				fmt.Printf("Warning: table %q has no inferred columns; skipping ingestion\n", table.Name())
				return nil
			}
		}
	}

	// Ensure table-level keys are on the schema before naming convention runs
	// Resolve PKs: user always wins, then table, then schema
	if len(p.config.PrimaryKeys) > 0 {
		tableSchema.PrimaryKeys = p.config.PrimaryKeys
	} else if len(tableSchema.PrimaryKeys) == 0 {
		tableSchema.PrimaryKeys = table.PrimaryKeys()
	}

	tableSchema.PrimaryKeys = p.filterDroppedPKs(tableSchema.PrimaryKeys)

	resolvedStrategy := preFetchStrategy
	if src.HandlesIncrementality() {
		tableSchema.IncrementalKey = table.IncrementalKey()
		config.Debug("[PIPELINE] Source Handles Incrementality: source-defined strategy=%s, incrementalKey=%s, PKs=%v", resolvedStrategy, tableSchema.IncrementalKey, tableSchema.PrimaryKeys)
	} else {
		if p.config.IncrementalKey != "" {
			tableSchema.IncrementalKey = p.config.IncrementalKey
		} else if tableSchema.IncrementalKey == "" {
			tableSchema.IncrementalKey = table.IncrementalKey()
		}
		config.Debug("[PIPELINE] Framework Handles Incrementality: framework-resolved strategy=%s, incrementalKey=%s, PKs=%v", resolvedStrategy, tableSchema.IncrementalKey, tableSchema.PrimaryKeys)
	}

	// Set partition key on schema: CLI flag wins, then source hint.
	if p.config.PartitionBy != "" {
		tableSchema.PartitionBy = p.config.PartitionBy
	} else if pt, ok := table.(source.PartitionedTable); ok && pt.PartitionBy() != "" {
		tableSchema.PartitionBy = pt.PartitionBy()
	}

	// Resolve the naming convention up front so excluded columns can be matched
	// against either the source name or the destination name they map to.
	namingConv, err := p.resolveNamingConvention(ctx, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to resolve naming convention: %w", err)
	}

	// Excluded columns should be removed from the effective schema before destination
	// preparation, even for known-schema sources where read-time filtering alone is not enough.
	tableSchema = p.applyExcludedColumnsToSchema(tableSchema, namingConv)

	// Preserve the original source column names before naming convention renames them.
	// The source needs original names for its SELECT queries; the ColumnRenamer
	// transforms Arrow batch column names from original → destination after reading.
	originalSourceSchema := &schema.TableSchema{
		Name:           tableSchema.Name,
		Schema:         tableSchema.Schema,
		Columns:        make([]schema.Column, len(tableSchema.Columns)),
		PrimaryKeys:    make([]string, len(tableSchema.PrimaryKeys)),
		IncrementalKey: tableSchema.IncrementalKey,
	}
	copy(originalSourceSchema.Columns, tableSchema.Columns)
	copy(originalSourceSchema.PrimaryKeys, tableSchema.PrimaryKeys)

	// Setup naming convention and column renamer using the convention resolved above.
	if err := p.applyNamingConvention(tableSchema, namingConv); err != nil {
		return fmt.Errorf("failed to setup naming convention: %w", err)
	}

	// Shorten column names that exceed the destination's identifier length limit.
	// Must run before schema evolution so ALTER TABLE uses shortened names.
	p.shortenLongIdentifiers(tableSchema)

	// setupIngestrColumns copies the schema and adds ingestr columns for the destination.
	// The original tableSchema stays clean (used by sources for SELECT queries).
	destSchema := tableSchema
	if ds, err := p.setupIngestrColumns(ctx, tableSchema); err != nil {
		config.Debug("[INGESTR] Failed to setup ingestr columns: %v", err)
	} else if ds != nil {
		destSchema = ds
	}

	// Apply column type overrides to the destination schema so PrepareTable
	// creates columns with the user-specified types.
	// For inferred schemas this was already done during inference (for data casting),
	// but destSchema may be a fresh copy from setupIngestrColumns that lacks overrides.
	if p.config.Columns != "" {
		if destSchema == tableSchema {
			copied := *tableSchema
			copied.Columns = make([]schema.Column, len(tableSchema.Columns))
			copy(copied.Columns, tableSchema.Columns)
			destSchema = &copied
		}
		if err := p.applyColumnOverrides(destSchema); err != nil {
			return fmt.Errorf("failed to apply column overrides: %w", err)
		}
	}

	// Schema contract handling: evolve destination schema if needed (skip for replace strategy)
	// Build the evolution plan but do NOT apply it here. Strategies decide when to apply.
	var evolutionPlan *schemaevolution.EvolutionPlan
	if resolvedStrategy != config.StrategyReplace {
		evolutionPlan, err = p.evolveSchemaIfNeeded(ctx, destSchema)
		if err != nil {
			return fmt.Errorf("schema evolution failed: %w", err)
		}
		if evolutionPlan != nil && evolutionPlan.FinalSchema != nil {
			destSchema = evolutionPlan.FinalSchema
		}
	}

	if inferBuffer != nil {
		bufferTarget := p.buildBufferReaderTarget(originalSourceSchema, destSchema)
		bufferedRecords, err = inferBuffer.Reader(ctx, bufferTarget)
		if err != nil {
			return fmt.Errorf("failed to open buffer reader: %w", err)
		}
		inferBuffer = nil
	}

	strat, err := strategy.Get(resolvedStrategy)
	if err != nil {
		return fmt.Errorf("failed to get strategy: %w", err)
	}

	// Create a config copy with resolved values from the table for strategy validation
	resolvedConfig := *p.config
	resolvedConfig.PrimaryKeys = tableSchema.PrimaryKeys
	resolvedConfig.IncrementalKey = tableSchema.IncrementalKey
	resolvedConfig.IncrementalStrategy = resolvedStrategy

	if resolvedConfig.PartitionBy == "" && tableSchema.PartitionBy != "" {
		resolvedConfig.PartitionBy = tableSchema.PartitionBy
	}

	// Primary key columns must be NOT NULL
	pkSet := make(map[string]bool, len(destSchema.PrimaryKeys))
	for _, pk := range destSchema.PrimaryKeys {
		pkSet[pk] = true
	}
	for i := range destSchema.Columns {
		if pkSet[destSchema.Columns[i].Name] {
			destSchema.Columns[i].Nullable = false
		}
	}

	// Validate strategy requirements
	if err := strat.Validate(&resolvedConfig); err != nil {
		return err
	}

	fmt.Println("\nStarting data ingestion...")

	// Don't pass tracker to the job when bufferedRecords is set,
	// because the tracker already counted rows during schema inference.
	var jobTracker progress.Tracker
	if bufferedRecords == nil {
		jobTracker = tracker
	}

	var columnMasker *transformer.ColumnMasker
	if len(p.config.Mask) > 0 {
		m, err := transformer.NewColumnMasker(p.config.Mask)
		if err != nil {
			return fmt.Errorf("invalid mask configuration: %w", err)
		}
		if m.HasMasks() {
			if err := m.ValidateColumns(destSchema); err != nil {
				return fmt.Errorf("invalid mask configuration: %w", err)
			}
			m.ApplyToSchema(destSchema)
			columnMasker = m
		}
	}

	job := &strategy.IngestionJob{
		Config:              &resolvedConfig,
		Table:               table,
		Destination:         dest,
		Schema:              destSchema,
		SourceSchema:        originalSourceSchema,
		Tracker:             jobTracker,
		BufferedRecords:     bufferedRecords,
		SchemaComparison:    p.schemaComparison,
		DestinationSchema:   p.destinationSchema,
		ColumnRenamer:       p.columnRenamer,
		IngestrColumnFiller: p.ingestrColumnFiller,
		ColumnMasker:        columnMasker,
		EvolutionPlan:       evolutionPlan,
	}

	// For --no-inference, enforce the user-provided source schema even when
	// a schema-less source does not apply ReadOptions.Schema itself.
	if p.config.NoInference && bufferedRecords == nil {
		job.TypeCaster = p.buildSourceSchemaCaster(originalSourceSchema)
	} else if p.config.Columns != "" && bufferedRecords == nil {
		// For known-schema sources with column type overrides, add a type caster
		// that converts Arrow batches from source types to the overridden types.
		job.TypeCaster = p.buildTypeCaster(tableSchema, destSchema)
	}

	if err := strat.Execute(ctx, job); err != nil {
		return fmt.Errorf("ingestion failed: %w", err)
	}

	return nil
}

func (p *Pipeline) schemaFromColumnOverrides(table source.SourceTable) (*schema.TableSchema, error) {
	tableSchema, err := schemainfer.SourceTableSchemaFromColumnOverrides(p.config.Columns, table.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to build schema from column overrides: %w", err)
	}
	if tableSchema == nil || len(tableSchema.Columns) == 0 {
		return nil, fmt.Errorf("--no-inference requires at least one column in --columns")
	}

	pks := p.config.PrimaryKeys
	if len(pks) == 0 {
		pks = table.PrimaryKeys()
	}
	ik := p.config.IncrementalKey
	if ik == "" {
		ik = table.IncrementalKey()
	}
	partitionCol := resolvePartitionBy(p.config, table)
	if err := schemainfer.AddKeyColumnsIfMissing(tableSchema, pks, ik, partitionCol, p.config.SchemaNaming); err != nil {
		return nil, fmt.Errorf("failed to add key columns to --columns schema: %w", err)
	}

	return tableSchema, nil
}

func (p *Pipeline) buildSourceSchemaCaster(sourceSchema *schema.TableSchema) *transformer.TypeCaster {
	if sourceSchema == nil {
		return nil
	}

	fields := make([]arrow.Field, len(sourceSchema.Columns))
	for i, col := range sourceSchema.Columns {
		fields[i] = arrowField(col.Name, col, col.Nullable)
	}

	return transformer.NewTypeCaster(arrow.NewSchema(fields, nil))
}

// buildTypeCaster creates a TypeCaster when column type overrides differ from the source types.
// It builds a target Arrow schema that matches the source columns but with overridden types from destSchema.
func (p *Pipeline) buildTypeCaster(sourceSchema, destSchema *schema.TableSchema) *transformer.TypeCaster {
	destTypes := make(map[string]schema.Column, len(destSchema.Columns))
	for _, col := range destSchema.Columns {
		destTypes[col.Name] = col
	}

	hasOverride := false
	fields := make([]arrow.Field, len(sourceSchema.Columns))
	for i, col := range sourceSchema.Columns {
		if destCol, ok := destTypes[col.Name]; ok && destCol.DataType != col.DataType {
			fields[i] = arrow.Field{
				Name:     col.Name,
				Type:     schema.DataTypeToArrowType(destCol),
				Nullable: col.Nullable,
			}
			hasOverride = true
		} else {
			fields[i] = arrow.Field{
				Name:     col.Name,
				Type:     schema.DataTypeToArrowType(col),
				Nullable: col.Nullable,
			}
		}
	}

	if !hasOverride {
		return nil
	}

	return transformer.NewTypeCaster(arrow.NewSchema(fields, nil))
}

func (p *Pipeline) createTracker(ctx context.Context) (progress.Tracker, error) {
	progressMode := p.config.Progress

	if progressMode == "" {
		progressMode = config.ProgressInteractive
	}

	// Interactive mode requires a TTY; fall back to log mode otherwise
	if progressMode == config.ProgressInteractive && !term.IsTerminal(int(os.Stdout.Fd())) {
		progressMode = config.ProgressLog
		config.Debug("[PIPELINE] No TTY detected, falling back to log progress mode")
	}

	var display progress.Display
	if p.logWriter != nil {
		display = progress.NewWriterLogDisplay(p.logWriter)
	} else {
		switch progressMode {
		case config.ProgressInteractive:
			display = progress.NewInteractiveDisplay()
		case config.ProgressLog:
			display = progress.NewLogDisplay()
		default:
			return nil, fmt.Errorf("unknown progress mode: %s", progressMode)
		}
	}

	tracker, err := progress.NewTracker(display)
	if err != nil {
		return nil, fmt.Errorf("failed to create progress tracker: %w", err)
	}

	tracker.Start(ctx)
	return tracker, nil
}

func (p *Pipeline) inferSchemaFromData(ctx context.Context, table source.SourceTable, tracker progress.Tracker) (*schema.TableSchema, *databuffer.FileBuffer, error) {
	// Create schema inferrer and file-backed data buffer
	inferrer := schemainfer.NewSchemaInferrer()
	buffer, err := databuffer.NewFileBuffer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create buffer: %w", err)
	}

	// Read all data from source
	parallelism := p.config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readOpts := source.ReadOptions{
		IncrementalKey: table.IncrementalKey(),
		IntervalStart:  p.config.IntervalStart,
		IntervalEnd:    p.config.IntervalEnd,
		PageSize:       p.config.PageSize,
		Limit:          p.config.SQLLimit,
		ExcludeColumns: p.config.SQLExcludeColumns,
		Parallelism:    parallelism,
		FullRefresh:    p.config.FullRefresh,
		Columns:        p.config.Columns,
	}

	records, err := table.Read(ctx, readOpts)
	if err != nil {
		_ = buffer.Close()
		return nil, nil, fmt.Errorf("failed to read from source: %w", err)
	}

	// Wrap records with progress tracker for Extract logging
	if tracker != nil {
		records = tracker.Wrap(records)
	}

	// Feed all records to inferrer and buffer in parallel
	for result := range records {
		if result.Err != nil {
			_ = buffer.Close()
			return nil, nil, fmt.Errorf("error reading batch: %w", result.Err)
		}

		var wg sync.WaitGroup
		var inferErr, bufErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			inferErr = inferrer.AddBatch(result.Batch)
		}()
		go func() {
			defer wg.Done()
			bufErr = buffer.Append(ctx, result.Batch)
		}()
		wg.Wait()

		result.Batch.Release()

		if inferErr != nil {
			_ = buffer.Close()
			return nil, nil, fmt.Errorf("failed to infer schema from batch: %w", inferErr)
		}
		if bufErr != nil {
			_ = buffer.Close()
			return nil, nil, fmt.Errorf("failed to buffer batch: %w", bufErr)
		}
	}

	// Protect columns specified via --columns from being dropped as all-null
	if err := inferrer.ProtectColumnOverrides(p.config.Columns); err != nil {
		_ = buffer.Close()
		return nil, nil, err
	}

	if partitionCol := resolvePartitionBy(p.config, table); partitionCol != "" {
		inferrer.ProtectColumns([]string{partitionCol})
	}

	// Infer schema
	tableSchema, err := inferrer.ToTableSchema(table.Name())
	if err != nil {
		_ = buffer.Close()
		return nil, nil, fmt.Errorf("failed to build schema: %w", err)
	}

	if tableSchema == nil {
		_ = buffer.Close()
		return nil, nil, nil
	}

	p.droppedColumns = inferrer.DroppedColumns()

	stats := inferrer.Stats()
	config.Debug("[PIPELINE] Schema inferred from %d batches, %d rows", stats.BatchCount, stats.RowCount)

	// Apply column type overrides before creating the buffer reader,
	// so the reader casts data to match the overridden types.
	if err := p.applyColumnOverrides(tableSchema); err != nil {
		_ = buffer.Close()
		return nil, nil, fmt.Errorf("failed to apply column overrides: %w", err)
	}

	// For schema-less sources, also append override columns that were never seen
	// in the data — the buffer reader will fill them with nulls.
	if err := schemainfer.AppendMissingOverrideColumns(tableSchema, p.config.Columns, p.config.SchemaNaming); err != nil {
		_ = buffer.Close()
		return nil, nil, fmt.Errorf("failed to append missing override columns: %w", err)
	}

	return tableSchema, buffer, nil
}

// buildBufferReaderTarget builds the Arrow schema for buffer.Reader:
// dest order, source names (for buffer file match), dest types (for staging
// match). Ingestr/SCD2 metadata cols are skipped and dest-only cols get null-fill.
func (p *Pipeline) buildBufferReaderTarget(sourceSchema, destSchema *schema.TableSchema) *arrow.Schema {
	var renameMap map[string]string
	if p.columnRenamer != nil && p.columnRenamer.HasRenames() {
		renameMap = p.columnRenamer.Mapping()
	}

	srcByDestName := make(map[string][]schema.Column, len(sourceSchema.Columns))
	for _, c := range sourceSchema.Columns {
		key := c.Name
		if r, ok := renameMap[key]; ok {
			key = r
		}
		key = strings.ToLower(key)
		srcByDestName[key] = append(srcByDestName[key], c)
	}

	fields := make([]arrow.Field, 0, len(destSchema.Columns))
	for _, dc := range destSchema.Columns {
		if naming.IsIngestrColumn(dc.Name) || isSCD2MetadataColumn(dc.Name) {
			continue
		}
		if sourceCols, ok := srcByDestName[strings.ToLower(dc.Name)]; ok {
			for _, sc := range sourceCols {
				m := sc
				m.DataType, m.Precision, m.Scale, m.ArrayType = dc.DataType, dc.Precision, dc.Scale, dc.ArrayType
				fields = append(fields, arrowField(sc.Name, m, m.Nullable || dc.Nullable)) // add columns using dest types but source names
			}
			continue
		}
		fields = append(fields, arrowField(dc.Name, dc, true)) // add soft deleted columns using dest names
	}

	return arrow.NewSchema(fields, nil)
}

func arrowField(name string, col schema.Column, nullable bool) arrow.Field {
	return arrow.Field{Name: name, Type: schema.DataTypeToArrowType(col), Nullable: nullable}
}

func isSCD2MetadataColumn(name string) bool {
	for _, scd := range destination.SCD2MetadataColumns() {
		if scd == name {
			return true
		}
	}
	return false
}

func (p *Pipeline) filterDroppedPKs(pks []string) []string {
	if len(p.droppedColumns) == 0 || len(pks) == 0 {
		return pks
	}
	var filtered []string
	for _, pk := range pks {
		if p.droppedColumns[pk] {
			config.Debug("[PIPELINE] Removing primary key %q: column was dropped during schema inference (all-null)", pk)
			continue
		}
		filtered = append(filtered, pk)
	}
	return filtered
}

func (p *Pipeline) applyExcludedColumnsToSchema(tableSchema *schema.TableSchema, namingConv naming.NamingConvention) *schema.TableSchema {
	if tableSchema == nil || len(p.config.SQLExcludeColumns) == 0 {
		return tableSchema
	}

	excluded := make(map[string]bool, len(p.config.SQLExcludeColumns))
	for _, col := range p.config.SQLExcludeColumns {
		excluded[strings.ToLower(col)] = true
	}

	// A column matches if the user named it either by its source name or by the
	// destination name it gets after the naming convention is applied
	isExcluded := func(name string) bool {
		if name == "" {
			return false
		}
		if excluded[strings.ToLower(name)] {
			return true
		}
		return namingConv != nil && excluded[strings.ToLower(namingConv.Normalize(name))]
	}

	filteredCols := make([]schema.Column, 0, len(tableSchema.Columns))
	for _, col := range tableSchema.Columns {
		if isExcluded(col.Name) {
			config.Debug("[PIPELINE] Excluding column from effective schema: %s", col.Name)
			continue
		}
		filteredCols = append(filteredCols, col)
	}

	filteredPKs := make([]string, 0, len(tableSchema.PrimaryKeys))
	for _, pk := range tableSchema.PrimaryKeys {
		if isExcluded(pk) {
			config.Debug("[PIPELINE] Excluding primary key column from effective schema: %s", pk)
			continue
		}
		filteredPKs = append(filteredPKs, pk)
	}

	incrementalKey := tableSchema.IncrementalKey
	if isExcluded(incrementalKey) {
		config.Debug("[PIPELINE] Excluding incremental key column from effective schema: %s", incrementalKey)
		incrementalKey = ""
	}

	filtered := *tableSchema
	filtered.Columns = filteredCols
	filtered.PrimaryKeys = filteredPKs
	filtered.IncrementalKey = incrementalKey

	return &filtered
}

func (p *Pipeline) runMultiTable(ctx context.Context, src source.MultiTableSource) error {
	tables, err := src.GetTables(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tables from multi-table source: %w", err)
	}

	if len(tables) == 0 {
		config.Debug("[PIPELINE] Multi-table source returned no tables")
		return nil
	}

	config.Debug("[PIPELINE] Multi-table mode: %d tables", len(tables))

	resolvedStrategy := p.config.IncrementalStrategy
	if p.config.FullRefresh {
		resolvedStrategy = config.StrategyReplace
	}

	if shouldWarnCDCStrategy(p.config, resolvedStrategy) {
		fmt.Printf("Warning: CDC source is using %q strategy instead of %q; CDC delete and update operations may not be properly reflected in the destination\n", resolvedStrategy, config.StrategyMerge)
	}

	strat, err := strategy.Get(resolvedStrategy)
	if err != nil {
		return fmt.Errorf("failed to get strategy: %w", err)
	}

	mtStrat, ok := strat.(strategy.MultiTableStrategy)
	if !ok {
		return fmt.Errorf("strategy %s does not support multi-table mode", resolvedStrategy)
	}

	resolvedConfig := *p.config
	resolvedConfig.IncrementalStrategy = resolvedStrategy

	display.PrintSummary(&resolvedConfig)

	// For CDC multi-table mode, skip validation since each table has its own PKs
	// The PKs come from the table schemas, not from config
	if !isCDCSource(p.config.SourceURI) {
		if err := strat.Validate(&resolvedConfig); err != nil {
			return err
		}
	}

	tracker, err := p.createTracker(ctx)
	if err != nil {
		return err
	}
	if tracker != nil {
		defer tracker.Stop()
	}

	tableDestNames := make(map[string]string)
	for _, table := range tables {
		destName := table.Name
		if table.DestSchema != "" {
			// TODO(turtledev): When a publication in a non-public
			// schema is created, the tables names may (?) have a schema.
			//
			// We need to verify this and if it's affirmitive, we need to
			// combine it with dest schema.
			// Possible formats:
			//  - {dest_schema}.{src_schema}_{name}
			//  - {dest_schema}_{src_schema}.{name}
			destName = table.DestSchema + "." + table.Name
		}
		tableDestNames[table.Name] = destName
	}

	// For CDC sources, query per-table max LSNs for resume
	var cdcResumeLSNs map[string]string
	if isCDCSource(p.config.SourceURI) && !p.config.FullRefresh {
		if resumeProvider, ok := p.dest.(destination.CDCResumeProvider); ok {
			cdcResumeLSNs = make(map[string]string)
			for _, table := range tables {
				destTable := tableDestNames[table.Name]
				maxLSN, err := resumeProvider.GetMaxCDCLSN(ctx, destTable)
				if err != nil {
					config.Debug("[PIPELINE] Failed to get max CDC LSN for table %s: %v", destTable, err)
					continue
				}
				if maxLSN != "" {
					cdcResumeLSNs[table.Name] = maxLSN
					config.Debug("[PIPELINE] Found existing CDC data for %s, max LSN: %s", table.Name, maxLSN)
				} else {
					config.Debug("[PIPELINE] No existing CDC data for %s, will perform snapshot", table.Name)
				}
			}
		}
	}

	job := &strategy.MultiTableIngestionJob{
		Config:         &resolvedConfig,
		Source:         src,
		Destination:    p.dest,
		Tables:         tables,
		TableDestNames: tableDestNames,
		Tracker:        tracker,
		CDCResumeLSNs:  cdcResumeLSNs,
	}

	if err := mtStrat.ExecuteMultiTable(ctx, job); err != nil {
		return fmt.Errorf("multi-table ingestion failed: %w", err)
	}

	return nil
}

// evolveSchemaIfNeeded inspects the destination's current schema and builds an
// EvolutionPlan describing how it should change to accommodate sourceSchema.
func (p *Pipeline) evolveSchemaIfNeeded(ctx context.Context, sourceSchema *schema.TableSchema) (*schemaevolution.EvolutionPlan, error) {
	// Get destination table schema (nil if table doesn't exist)
	destSchema, err := p.dest.GetTableSchema(ctx, p.config.DestTable)
	if err != nil {
		return nil, fmt.Errorf("failed to get destination schema: %w", err)
	}
	if destSchema == nil {
		config.Debug("[SCHEMA EVOLUTION] Destination table doesn't exist yet, skipping evolution")
		return nil, nil
	}

	// Store destination schema for use by strategies
	p.destinationSchema = destSchema

	// Parse schema contract mode
	contractMode, err := schemaevolution.ParseContractMode(p.config.SchemaContract)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema contract: %w", err)
	}
	contract := schemaevolution.SchemaContract{Mode: contractMode}
	config.Debug("[SCHEMA EVOLUTION] Using schema contract mode: %s", contractMode)

	// Parse column overrides from config
	overrides, err := schemaevolution.ParseColumnOverrides(p.config.Columns)
	if err != nil {
		return nil, fmt.Errorf("failed to parse column overrides: %w", err)
	}
	if len(overrides) > 0 {
		config.Debug("[SCHEMA EVOLUTION] Applying %d column type overrides", len(overrides))
	}

	// Compare schemas with overrides. Staging-only CDC columns are not persisted on the destination.
	opts := &schemaevolution.CompareOptions{Overrides: overrides}
	comparison, err := schemaevolution.Compare(destination.DestinationTableSchema(sourceSchema), destSchema, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to compare schemas: %w", err)
	}
	if !comparison.HasChanges {
		config.Debug("[SCHEMA EVOLUTION] No schema changes detected")
		return &schemaevolution.EvolutionPlan{
			FinalSchema: schemaevolution.BuildFinalSchema(destSchema, nil),
			Migration:   nil,
		}, nil
	}

	config.Debug("[SCHEMA EVOLUTION] Detected %d schema changes", len(comparison.Changes))

	// Log warnings for removed columns (Fivetran-style soft removal)
	for _, change := range comparison.Changes {
		if change.Type == schemaevolution.ChangeRemoveColumn {
			fmt.Printf("Warning: Column %q no longer exists in source, future values will be NULL\n", change.ColumnName)
		}
	}

	// Apply schema contract to determine which changes are allowed
	contractResult := schemaevolution.ApplyContract(contract, comparison)

	// Store ORIGINAL schema comparison for use by strategies (for runtime batch transformation)
	// This includes all violations, even those that will be handled by the contract
	p.schemaComparison = comparison

	// Handle contract violations based on mode
	switch contract.Mode {
	case schemaevolution.ContractFreeze:
		if contractResult.HasViolations() {
			return nil, contractResult.ViolationError()
		}
		p.filteredSchemaComparison = comparison
		return &schemaevolution.EvolutionPlan{
			FinalSchema: schemaevolution.BuildFinalSchema(destSchema, p.filteredSchemaComparison),
			Migration:   nil,
		}, nil

	case schemaevolution.ContractDiscardRow:
		if contractResult.HasViolations() {
			for _, v := range contractResult.Violations {
				fmt.Printf("Warning: %s - rows with schema violations will be discarded\n", v.Description)
			}
			config.Debug("[SCHEMA EVOLUTION] Discard row mode will filter out incompatible rows during ingestion")
		}
		// For discard_row, apply all schema changes but filter rows at write time
		p.filteredSchemaComparison = comparison

	case schemaevolution.ContractDiscardValue:
		if contractResult.HasViolations() {
			for _, v := range contractResult.Violations {
				fmt.Printf("Warning: %s - non-conforming values will be set to NULL\n", v.Description)
			}
			config.Debug("[SCHEMA EVOLUTION] Discard value mode will NULL out incompatible values during ingestion")

			// Patch source schema to match destination types for violations
			// This ensures Staging table is created with Destination types (e.g. INT) instead of Source types (e.g. STRING)
			// enabling correct INSERT INTO ... SELECT behavior on strict DBs like Postgres
			for _, change := range comparison.Changes {
				if change.Type == schemaevolution.ChangeWidenType && change.OldColumn != nil {
					// Find column in sourceSchema and revert it to OldColumn (Dest Type)
					for i, col := range sourceSchema.Columns {
						if col.Name == change.ColumnName {
							sourceSchema.Columns[i] = *change.OldColumn
							// Ensure nullable is true as we might be setting NULLs
							sourceSchema.Columns[i].Nullable = true
							break
						}
					}
				}
			}
		}
		if len(contractResult.Allowed) == 0 {
			// No new columns to migrate, but violations still need to be NULLed during ingestion
			p.filteredSchemaComparison = &schemaevolution.SchemaComparison{
				Changes:    []schemaevolution.SchemaChange{},
				HasChanges: false,
			}
			return &schemaevolution.EvolutionPlan{
				FinalSchema: schemaevolution.BuildFinalSchema(destSchema, p.filteredSchemaComparison),
				Migration:   nil,
			}, nil
		}
		// For migration, only apply allowed changes (new columns) for discard_value mode
		// Type changes are NOT applied, but they ARE captured for runtime transformation
		p.filteredSchemaComparison = &schemaevolution.SchemaComparison{
			Changes:    contractResult.Allowed,
			HasChanges: len(contractResult.Allowed) > 0,
		}

	case schemaevolution.ContractEvolve:
		p.filteredSchemaComparison = comparison
	}

	// Get dialect for the destination
	dialect := schemaevolution.GetDialect(p.dest.GetScheme())
	if dialect == nil {
		config.Debug("[SCHEMA EVOLUTION] No dialect registered for scheme %s, skipping", p.dest.GetScheme())
		return &schemaevolution.EvolutionPlan{
			FinalSchema: schemaevolution.BuildFinalSchema(destSchema, p.filteredSchemaComparison),
			Migration:   nil,
		}, nil
	}

	// Generate migration using the FILTERED schema comparison (after contract filtering)
	// This ensures we only apply allowed changes (e.g., new columns in discard_value mode)
	migration, err := schemaevolution.GenerateMigration(p.filteredSchemaComparison, dialect, p.config.DestTable)
	if err != nil {
		return nil, fmt.Errorf("failed to generate migration: %w", err)
	}

	// Log warnings
	for _, w := range migration.Warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	// Log SQL statements in debug mode
	for _, sql := range migration.Statements {
		config.Debug("[SCHEMA EVOLUTION] %s", sql)
	}

	// Build the plan; migration is NOT applied here.
	plan := &schemaevolution.EvolutionPlan{
		FinalSchema: schemaevolution.BuildFinalSchema(destSchema, p.filteredSchemaComparison),
		Migration:   migration,
	}
	config.Debug("[SCHEMA EVOLUTION] Built plan with %d statements (deferred apply)", len(migration.Statements))
	return plan, nil
}

func (p *Pipeline) setupIngestrColumns(ctx context.Context, sourceSchema *schema.TableSchema) (*schema.TableSchema, error) {
	destSchema, err := p.dest.GetTableSchema(ctx, p.config.DestTable)
	if err != nil || destSchema == nil {
		return nil, nil
	}

	if !naming.HasIngestrColumns(destSchema) {
		return nil, nil
	}

	ingestrCols := naming.GetIngestrColumns(destSchema)
	p.ingestrColumnFiller = schemaevolution.NewIngestrColumnFiller(ingestrCols)
	config.Debug("[INGESTR] Will fill %d ingestr columns with '-': %v", len(ingestrCols), ingestrCols)

	// Copy the source schema so the original stays clean for source SELECT queries.
	copied := *sourceSchema
	copied.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(copied.Columns, sourceSchema.Columns)

	for _, colName := range ingestrCols {
		exists := false
		for _, col := range copied.Columns {
			if col.Name == colName {
				exists = true
				break
			}
		}
		if !exists {
			copied.Columns = append(copied.Columns, schema.Column{
				Name:     colName,
				DataType: schema.TypeString,
				Nullable: true,
			})
		}
	}

	return &copied, nil
}

func (p *Pipeline) setupNamingConvention(ctx context.Context, sourceSchema *schema.TableSchema) error {
	namingConv, err := p.resolveNamingConvention(ctx, sourceSchema)
	if err != nil {
		return err
	}
	return p.applyNamingConvention(sourceSchema, namingConv)
}

// resolveNamingConvention determines which naming convention applies, resolving
// the "auto" setting by inspecting the destination table. It never returns Auto.
func (p *Pipeline) resolveNamingConvention(ctx context.Context, sourceSchema *schema.TableSchema) (naming.NamingConvention, error) {
	convention, err := naming.ParseConvention(p.config.SchemaNaming)
	if err != nil {
		return nil, err
	}

	// For auto detection, check if destination exists and has snake_case naming
	if convention == naming.Auto {
		destSchema, err := p.dest.GetTableSchema(ctx, p.config.DestTable)
		if err != nil {
			config.Debug("[NAMING] Failed to get destination schema for auto-detection: %v", err)
			convention = naming.SnakeCase
		} else if destSchema != nil {
			detected := naming.DetectConvention(sourceSchema, destSchema)
			convention = detected
			config.Debug("[NAMING] Auto-detected naming convention: %s", detected)
		} else {
			config.Debug("[NAMING] Destination table doesn't exist, using snake_case naming")
			convention = naming.SnakeCase
		}
	}

	return naming.Get(convention), nil
}

func (p *Pipeline) applyNamingConvention(sourceSchema *schema.TableSchema, namingConv naming.NamingConvention) error {
	// If using direct naming (no transformation), skip setup
	if namingConv.Name() == string(naming.Direct) {
		config.Debug("[NAMING] Using direct naming (no column transformation)")
		return nil
	}

	config.Debug("[NAMING] Using %s naming convention", namingConv.Name())

	// Build column mapping
	columnMapping := naming.BuildColumnMapping(sourceSchema, namingConv)
	if len(columnMapping) == 0 {
		config.Debug("[NAMING] No column renames needed")
		return nil
	}

	// Log the column mappings
	for src, dest := range columnMapping {
		config.Debug("[NAMING] Column rename: %s -> %s", src, dest)
	}
	fmt.Printf("Naming convention: %s (%d columns will be renamed)\n", namingConv.Name(), len(columnMapping))

	p.namingMapping = columnMapping
	p.applyColumnMapping(sourceSchema, columnMapping)
	return nil
}

func (p *Pipeline) shortenLongIdentifiers(sourceSchema *schema.TableSchema) {
	maxLen := destination.MaxIdentifierLength(p.dest.GetScheme())
	mapping := destination.ShortenColumnNames(sourceSchema.Columns, maxLen, p.namingMapping)
	if len(mapping) == 0 {
		return
	}
	fmt.Printf("Identifier shortening: %d column(s) shortened to fit %d-byte limit\n", len(mapping), maxLen)
	p.applyColumnMapping(sourceSchema, mapping)
}

// applyColumnMapping renames schema columns/PKs/incremental key and updates the column renamer.
func (p *Pipeline) applyColumnMapping(s *schema.TableSchema, mapping map[string]string) {
	for i := range s.Columns {
		if newName, ok := mapping[s.Columns[i].Name]; ok {
			s.Columns[i].Name = newName
		}
	}
	s.Columns = dedupeMappedColumns(s.Columns)

	for i, pk := range s.PrimaryKeys {
		if newName, ok := mapping[pk]; ok {
			s.PrimaryKeys[i] = newName
		}
	}
	s.PrimaryKeys = dedupeStringsPreserveOrder(s.PrimaryKeys)

	if newName, ok := mapping[s.IncrementalKey]; ok {
		s.IncrementalKey = newName
	}
	if newName, ok := mapping[s.PartitionBy]; ok {
		s.PartitionBy = newName
	}

	// Compose with existing renamer: if A→B already exists and B→C is new, result is A→C.
	if p.columnRenamer != nil && p.columnRenamer.HasRenames() {
		for src, dst := range p.columnRenamer.Mapping() {
			if newDst, ok := mapping[dst]; ok {
				mapping[src] = newDst
			} else {
				mapping[src] = dst
			}
		}
	}
	p.columnRenamer = transformer.NewColumnRenamer(mapping)
}

func dedupeMappedColumns(columns []schema.Column) []schema.Column {
	if len(columns) < 2 {
		return columns
	}

	merged := make([]schema.Column, 0, len(columns))
	indexByName := make(map[string]int, len(columns))
	for _, col := range columns {
		if idx, ok := indexByName[col.Name]; ok {
			merged[idx] = mergeSchemaColumns(merged[idx], col)
			continue
		}
		indexByName[col.Name] = len(merged)
		merged = append(merged, col)
	}

	return merged
}

func mergeSchemaColumns(existing, next schema.Column) schema.Column {
	merged := existing
	merged.Nullable = existing.Nullable || next.Nullable
	merged.IsPrimaryKey = existing.IsPrimaryKey || next.IsPrimaryKey
	if next.MaxLength > merged.MaxLength {
		merged.MaxLength = next.MaxLength
	}

	switch {
	case existing.DataType == schema.TypeUnknown:
		merged.DataType = next.DataType
	case next.DataType == schema.TypeUnknown:
		merged.DataType = existing.DataType
	default:
		merged.DataType, _ = schemaevolution.GetWidenedType(existing.DataType, next.DataType)
	}

	if merged.DataType == schema.TypeDecimal {
		merged.Precision, merged.Scale = schemaevolution.MergeDecimalPrecision(existing, next)
	} else {
		merged.Precision = 0
		merged.Scale = 0
	}

	if merged.DataType == schema.TypeArray {
		if existing.ArrayType == next.ArrayType {
			merged.ArrayType = existing.ArrayType
		} else {
			merged.ArrayType, _ = schemaevolution.GetWidenedType(existing.ArrayType, next.ArrayType)
		}
	} else {
		merged.ArrayType = schema.TypeUnknown
	}

	return merged
}

func dedupeStringsPreserveOrder(values []string) []string {
	if len(values) < 2 {
		return values
	}

	seen := make(map[string]bool, len(values))
	deduped := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
}

func (p *Pipeline) applyColumnOverrides(sourceSchema *schema.TableSchema) error {
	overrides, err := schemaevolution.ParseColumnOverrides(p.config.Columns)
	if err != nil {
		return fmt.Errorf("failed to parse column overrides: %w", err)
	}
	if len(overrides) == 0 {
		return nil
	}

	applied := 0
	renameMap := make(map[string]string)
	for i, col := range sourceSchema.Columns {
		override, ok := overrides.GetForColumn(col.Name, p.config.SchemaNaming)
		if !ok {
			continue
		}

		if override.DataType != schema.TypeUnknown {
			newCol := override.ApplyToColumn(col)
			if col.DataType != newCol.DataType || col.Precision != newCol.Precision || col.Scale != newCol.Scale {
				fmt.Printf("Column override: %q type changed from %v(p=%v,s=%v) to %v(p=%v,s=%v)\n",
					col.Name, col.DataType, col.Precision, col.Scale, newCol.DataType, newCol.Precision, newCol.Scale)
			}
			sourceSchema.Columns[i] = newCol
			config.Debug("[PIPELINE] Column override applied: %s -> %v", col.Name, override.DataType)
			applied++
		}

		if override.RenameTo != "" && override.RenameTo != sourceSchema.Columns[i].Name {
			renameMap[sourceSchema.Columns[i].Name] = override.RenameTo
			fmt.Printf("Column rename: %q -> %q (from --columns)\n", sourceSchema.Columns[i].Name, override.RenameTo)
		}
	}

	if applied > 0 {
		config.Debug("[PIPELINE] Applied %d column type overrides", applied)
	}

	if len(renameMap) > 0 {
		p.applyColumnMapping(sourceSchema, renameMap)
	}

	return nil
}

// shouldWarnCDCStrategy returns true if the user should be warned about using
// a non-merge strategy with a CDC source.
func shouldWarnCDCStrategy(cfg *config.IngestConfig, resolvedStrategy config.IncrementalStrategy) bool {
	return isCDCSource(cfg.SourceURI) && !cfg.FullRefresh && resolvedStrategy != config.StrategyMerge
}

func resolveStrategy(cfg *config.IngestConfig, src source.Source, table source.SourceTable) config.IncrementalStrategy {
	var s config.IncrementalStrategy
	if src.HandlesIncrementality() {
		s = table.Strategy()
	} else {
		s = cfg.IncrementalStrategy
	}
	if cfg.FullRefresh {
		s = config.StrategyReplace
	}
	return rewriteReplaceForPostgres(s, cfg.DestURI)
}

func resolveIncrementalKey(cfg *config.IngestConfig, src source.Source, table source.SourceTable) string {
	if src.HandlesIncrementality() {
		return table.IncrementalKey()
	}
	if cfg.IncrementalKey != "" {
		return cfg.IncrementalKey
	}
	return table.IncrementalKey()
}

func resolvePrimaryKeys(cfg *config.IngestConfig, table source.SourceTable) []string {
	if len(cfg.PrimaryKeys) > 0 {
		return cfg.PrimaryKeys
	}
	return table.PrimaryKeys()
}

func resolvePartitionBy(cfg *config.IngestConfig, table source.SourceTable) string {
	if cfg.PartitionBy != "" {
		return cfg.PartitionBy
	}
	if pt, ok := table.(source.PartitionedTable); ok {
		return pt.PartitionBy()
	}
	return ""
}

// rewriteReplaceForPostgres swaps the replace strategy for truncate+insert when
// the destination is Postgres. Replace drops and recreates the table, which
// breaks dependent views, grants, and foreign keys; truncate+insert preserves
// the table definition.
func rewriteReplaceForPostgres(strat config.IncrementalStrategy, destURI string) config.IncrementalStrategy {
	if strat != config.StrategyReplace {
		return strat
	}
	scheme, err := uri.ExtractScheme(destURI)
	if err != nil {
		return strat
	}
	if uri.NormalizeScheme(scheme) != "postgres" {
		return strat
	}
	return config.StrategyTruncateInsert
}

// isCDCSource returns true if the source URI indicates a CDC source
func isCDCSource(uri string) bool {
	return strings.HasPrefix(uri, "postgres+cdc://") || strings.HasPrefix(uri, "postgresql+cdc://")
}

// cdcSlotSuffix returns a 6-hex-char hash of the destination URI for use as a
// replication slot name suffix, making auto-generated slot names unique per destination.
func cdcSlotSuffix(destURI string) string {
	h := sha256.Sum256([]byte(destURI))
	return fmt.Sprintf("%x", h[:3])
}

func emptyRecordChannel() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}
