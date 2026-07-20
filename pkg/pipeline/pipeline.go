package pipeline

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/display"
	"github.com/bruin-data/ingestr/internal/metrics"
	"github.com/bruin-data/ingestr/internal/output"
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

const (
	oracleComparableStringLen = 4000
	resourceCloseTimeout      = 30 * time.Second
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
	cdcConnectorID           string
}

func New(cfg *config.IngestConfig) *Pipeline {
	return &Pipeline{
		config: cfg,
	}
}

func (p *Pipeline) SetLogWriter(w io.Writer) {
	p.logWriter = w
}

func (p *Pipeline) Run(ctx context.Context) (retErr error) {
	// Parse query annotations once and carry the base payload on the context.
	// Destinations read it (plus a per-operation step) to annotate queries for
	// cost attribution. Absent caller annotations just means ingestr's own keys
	// (type, ingestr_step) are emitted without any caller-supplied keys.
	annotations, err := annotation.Parse(p.config.QueryAnnotations)
	if err != nil {
		return err
	}
	ctx = annotation.WithPayload(ctx, annotations)
	cleanupBaseCtx := context.WithoutCancel(ctx)

	if err := validateManagedChangeConfig(p.config); err != nil {
		return err
	}

	src, err := uri.DefaultRegistry.GetSource(p.config.SourceURI)
	if err != nil {
		return fmt.Errorf("failed to get source: %w", err)
	}
	p.src = src

	if err := src.Connect(ctx, p.config.SourceURI); err != nil {
		return fmt.Errorf("failed to connect to source: %w", err)
	}
	defer func() {
		closeCtx, cancel := resourceCloseContext(cleanupBaseCtx)
		defer cancel()
		if err := src.Close(closeCtx); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to close source: %w", err))
		}
	}()

	var postgresCDCIdentity source.ConnectorIdentity
	if isPostgresCDCSource(p.config.SourceURI) {
		identityProvider, ok := src.(source.ConnectorIdentityProvider)
		if !ok {
			return fmt.Errorf("postgres CDC source does not expose its resolved connector identity")
		}
		identity, err := identityProvider.ConnectorIdentity(ctx)
		if err != nil {
			return fmt.Errorf("failed to resolve PostgreSQL CDC connector identity: %w", err)
		}
		postgresCDCIdentity = identity
	}
	if validator, ok := src.(source.ConnectorPreflightValidator); ok {
		if err := validator.ValidateConnectorPreflight(ctx, source.ConnectorPreflightOptions{Streaming: p.config.Stream}); err != nil {
			return fmt.Errorf("source connector preflight failed: %w", err)
		}
	}

	if p.config.Stream {
		ss, ok := src.(source.StreamingSource)
		if !ok || !ss.SupportsStreaming() {
			return fmt.Errorf("--stream is not supported by this source; streaming requires a CDC source (postgres+cdc, mssql+cdc) or a message broker source (kafka, amqp, mqtt, sqs, pubsub, kinesis)")
		}
		if lr, ok := src.(source.LagReporter); ok {
			metrics.SetLagReporter(lr)
			defer metrics.SetLagReporter(nil)
		}
	}

	dest, err := uri.DefaultRegistry.GetDestination(p.config.DestURI)
	if err != nil {
		return fmt.Errorf("failed to get destination: %w", err)
	}
	p.dest = dest

	if err := dest.Connect(ctx, p.config.DestURI); err != nil {
		return fmt.Errorf("failed to connect to destination: %w", err)
	}
	defer func() {
		closeCtx, cancel := resourceCloseContext(cleanupBaseCtx)
		defer cancel()
		if err := dest.Close(closeCtx); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to close destination: %w", err))
		}
	}()

	destinationTarget := ""
	if isPostgresCDCSource(p.config.SourceURI) {
		destinationTarget = managedCDCDestinationTarget(p.config, dest)
		if err := validateMultiTableNamespace(p.config, dest, destinationTarget); err != nil {
			return err
		}
		destinationIdentity, err := managedCDCDestinationIdentity(ctx, dest, destinationTarget)
		if err != nil {
			return fmt.Errorf("failed to resolve PostgreSQL CDC destination identity: %w", err)
		}
		p.cdcConnectorID = resolvedCDCStateConnectorID(p.config, postgresCDCIdentity, destinationIdentity)
	} else if isCDCSource(p.config.SourceURI) {
		p.cdcConnectorID = genericCDCConnectorID(p.config)
	}

	var cdcStateManager *strategy.CDCStateManager

	// For CDC sources, compute a destination-aware slot suffix
	if isCDCSource(p.config.SourceURI) {
		p.config.CDCSlotSuffix = cdcSlotSuffix(canonicalCDCStateURI(p.config.DestURI) + "\x00" + p.cdcConnectorID)
		p.config.CDCLegacySlotSuffix = legacyCDCSlotSuffix(p.config.DestURI)
		config.Debug("[PIPELINE] CDC slot suffix: %s", p.config.CDCSlotSuffix)
	}

	managedPostgresCDC := isPostgresCDCSource(p.config.SourceURI)
	if managedPostgresCDC {
		if err := validateDestinationManagedCDCState(dest); err != nil {
			return err
		}
		if validator, ok := dest.(destination.ManagedCDCTargetValidator); ok {
			if err := validator.ValidateManagedCDCTarget(ctx, destinationTarget); err != nil {
				return fmt.Errorf("destination scheme %q cannot safely use managed CDC target %q: %w", dest.GetScheme(), destinationTarget, err)
			}
		}
		leaser, ok := src.(source.ConnectorLeaser)
		if !ok {
			return fmt.Errorf("postgres CDC source does not support connector leases")
		}
		if preparer, ok := src.(source.ConnectorPreparer); ok {
			if err := preparer.PrepareConnector(ctx); err != nil {
				return err
			}
		}
		lease, err := leaser.AcquireConnectorLease(ctx, source.ConnectorLeaseOptions{
			ConnectorID:      p.cdcConnectorID,
			SlotSuffix:       p.config.CDCSlotSuffix,
			LegacySlotSuffix: p.config.CDCLegacySlotSuffix,
			SourceTable:      p.config.SourceTable,
		})
		if err != nil {
			return fmt.Errorf("failed to acquire PostgreSQL CDC connector lease: %w", err)
		}
		leaseCtx, stopLeaseWatch := connectorLeaseContext(ctx, lease)
		ctx = leaseCtx
		defer func() {
			stopLeaseWatch()
			if leaseErr := lease.Err(); leaseErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("PostgreSQL CDC connector lease lost: %w", leaseErr))
			}
		}()
	}

	// Check if source is multi-table (only if no specific source table is requested)
	if p.config.SourceTable == "" {
		if mtSource, ok := src.(source.MultiTableSource); ok && mtSource.IsMultiTable() {
			return p.runMultiTable(ctx, mtSource)
		}
	}
	sourceIncarnation := ""
	sourceSchemaFingerprint := ""
	if managedPostgresCDC {
		cdcStateManager, err = strategy.NewCDCStateManager(
			dest,
			p.cdcConnectorID,
			p.config.DestTable,
			p.config.StagingDataset,
		)
		if err != nil {
			return err
		}
		if checker, ok := src.(source.TableExistenceChecker); ok {
			exists, err := checker.TableExists(ctx, p.config.SourceTable)
			if err != nil {
				return fmt.Errorf("failed to verify source table %q: %w", p.config.SourceTable, err)
			}
			if !exists {
				cdcStateManager.RegisterTableForRead(p.config.SourceTable, p.config.DestTable)
				if err := cdcStateManager.BeginRun(ctx, false); err != nil {
					return err
				}
				return fmt.Errorf("source table %q does not exist", p.config.SourceTable)
			}
		}
		if provider, ok := src.(source.TableIncarnationProvider); ok {
			sourceIncarnation, err = provider.TableIncarnation(ctx, p.config.SourceTable)
			if err != nil {
				return err
			}
		}
		if provider, ok := src.(source.TableSchemaFingerprintProvider); ok {
			sourceSchemaFingerprint, err = provider.TableSchemaFingerprint(ctx, p.config.SourceTable)
			if err != nil {
				return err
			}
		}
		if err := cdcStateManager.RegisterTableState(ctx, p.config.SourceTable, p.config.DestTable, sourceIncarnation, sourceSchemaFingerprint); err != nil {
			return err
		}
	}

	if isChangeTrackingSource(p.config.SourceURI) {
		if err := validateChangeTrackingDestination(dest); err != nil {
			return err
		}
	}
	// For managed change sources, check if we can resume from existing data
	if cdcStateManager != nil && !p.config.FullRefresh {
		resumeLSN, err := cdcStateManager.ResumePosition(ctx, p.config.SourceTable)
		if err != nil {
			return err
		}
		if resumeLSN != "" {
			p.config.CDCResumeLSN = resumeLSN
			p.config.CDCResumeIncarnation = sourceIncarnation
			p.config.CDCResumeSchemaFingerprint = sourceSchemaFingerprint
			config.Debug("[PIPELINE] Found destination-managed CDC state, resuming from: %s", resumeLSN)
		} else {
			// Once any state exists, replacement snapshots are authorized: this
			// connector already owns the target. With no state at all, a target
			// that contains CDC data belongs to an unmanaged (pre-state) run or
			// to a lost state table, so fail closed.
			stateEmpty, err := cdcStateManager.StateEmpty(ctx)
			if err != nil {
				return err
			}
			if stateEmpty {
				if err := rejectUnprovenLegacyCDCTarget(ctx, dest, p.config.SourceTable, p.config.DestTable); err != nil {
					return err
				}
			}
			config.Debug("[PIPELINE] No completed CDC snapshot state found, will perform full snapshot")
		}
	} else if isManagedChangeSource(p.config.SourceURI) && !p.config.FullRefresh {
		resumeProvider, ok := dest.(destination.CDCResumeProvider)
		if !ok {
			if isChangeTrackingSource(p.config.SourceURI) {
				return fmt.Errorf("destination scheme %q does not support resume cursors required by SQL Server Change Tracking", dest.GetScheme())
			}
		} else {
			maxLSN, err := resumeProvider.GetMaxCDCLSN(ctx, p.config.DestTable)
			if err != nil {
				if isChangeTrackingSource(p.config.SourceURI) {
					return fmt.Errorf("failed to get SQL Server Change Tracking cursor from destination: %w", err)
				}
				config.Debug("[PIPELINE] Failed to get max change cursor from destination: %v", err)
			} else if maxLSN != "" {
				config.Debug("[PIPELINE] Found existing change data, will resume from cursor: %s", maxLSN)
				p.config.CDCResumeLSN = maxLSN
			} else {
				config.Debug("[PIPELINE] No existing change data found, will perform full snapshot")
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
		Streaming:      p.config.Stream,
		StrategySet:    p.config.IncrementalStrategyExplicit,
		FullRefresh:    p.config.FullRefresh,
	})
	if err != nil {
		return fmt.Errorf("failed to get table: %w", err)
	}

	// Sources that manage incrementality internally resolve their own key in
	// GetTable. Only warn if the user's --incremental-key was actually dropped;
	// a source that adopts it (resolved key matches) needs no warning.
	if src.HandlesIncrementality() && p.config.IncrementalKey != "" && table.IncrementalKey() != p.config.IncrementalKey {
		output.Warnf("Warning: source handles incrementality internally, ignoring --incremental-key=%s\n", p.config.IncrementalKey)
	}

	if table.Name() == source.CustomQueryTableName {
		if p.config.DestTable == p.config.SourceTable {
			p.config.DestTable = source.CustomQueryTableName
		}
		p.config.SourceTable = source.CustomQueryTableName
	}
	if err := validateExtractPartitionSupport(p.config, table); err != nil {
		return err
	}

	preFetchStrategy := resolveStrategy(p.config, src, table)
	preFetchConfig := *p.config
	preFetchConfig.IncrementalStrategy = preFetchStrategy
	preFetchConfig.IncrementalKey = resolveIncrementalKey(p.config, src, table)
	preFetchConfig.PrimaryKeys = resolvePrimaryKeys(p.config, table)
	preFetchConfig.PartitionBy = resolvePartitionBy(p.config, table)
	if err := validateExtractPartitionStrategy(&preFetchConfig, preFetchStrategy); err != nil {
		return err
	}
	if err := validateIncrementalPredicate(p.config, dest, preFetchStrategy); err != nil {
		return err
	}
	display.PrintSummary(&preFetchConfig)

	if shouldWarnCDCStrategy(p.config, preFetchStrategy) {
		output.Warnf("Warning: change data source is using %q strategy instead of %q; delete and update operations may not be properly reflected in the destination\n", preFetchStrategy, config.StrategyMerge)
	}

	tracker, err := p.createTracker(ctx)
	if err != nil {
		return err
	}
	if tracker != nil {
		defer func() { tracker.Stop(retErr) }()
	}

	// The load timestamp is chosen before extract so that pre-staged load
	// files (written during extract) and the replay path stamp the same value.
	var loadTimestamp time.Time
	if !p.config.NoLoadTimestamp && !p.config.Stream {
		loadTimestamp = time.Now().UTC().Truncate(time.Microsecond)
	}

	// Check if source has known schema or needs inference
	var tableSchema *schema.TableSchema
	var bufferedRecords <-chan source.RecordBatchResult
	var inferBuffer *databuffer.FileBuffer
	var preStagedData destination.PreStagedData
	var preStageRpt *preStageReport
	var preStageKeyTransform func(string) string
	var preStagedForJob destination.PreStagedData

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
		var preStage destination.PreStageWriter
		preStage, preStageKeyTransform = p.maybeStartPreStage(ctx, preFetchStrategy, preFetchConfig.PrimaryKeys, loadTimestamp)
		tableSchema, inferBuffer, preStagedData, preStageRpt, err = p.inferSchemaFromData(ctx, table, tracker, preStage)
		defer func() {
			if preStagedData != nil {
				preStagedData.Close()
			}
		}()
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
				output.Warnf("Warning: table %q produced no rows; creating destination table from --columns\n", table.Name())
				config.Debug("[PIPELINE] Built synthetic schema with %d columns from --columns", len(synthetic.Columns))
			} else {
				output.Warnf("Warning: table %q has no inferred columns; skipping ingestion\n", table.Name())
				return nil
			}
		}
	}

	if p.config.ExtractPartitionBy != "" {
		partitionColumn, err := source.ValidateExtractPartitionColumn(tableSchema, p.config.ExtractPartitionBy)
		if err != nil {
			return err
		}
		p.config.ExtractPartitionBy = partitionColumn
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
	markPrimaryKeyColumns(tableSchema)

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
	p.applyDestinationSchemaConstraints(destSchema)

	if !p.config.NoLoadTimestamp {
		destSchema = addLoadTimestampColumn(destSchema)
	}

	// Capture the schema before evolution: on incremental runs against an
	// existing table, evolution replaces destSchema with FinalSchema, which is
	// built from the destination's columns and therefore drops staging-only
	// CDC columns (e.g. _cdc_unchanged_cols). StagingIngestSchema below
	// re-appends them from this snapshot so staging keeps carrying them.
	fullSchema := destSchema

	// Schema contract handling: evolve destination schema if needed (skip for replace strategy)
	// Build the evolution plan but do NOT apply it here. Strategies decide when to apply.
	var evolutionPlan *schemaevolution.EvolutionPlan
	if resolvedStrategy != config.StrategyReplace {
		evolutionPlan, err = p.evolveSchemaIfNeeded(ctx, p.config.DestTable, destSchema, resolvedStrategy)
		if err != nil {
			return fmt.Errorf("schema evolution failed: %w", err)
		}
		if evolutionPlan != nil && evolutionPlan.FinalSchema != nil {
			destSchema = evolutionPlan.FinalSchema
		}
	}
	if p.config.NoLoadTimestamp {
		destSchema = removeLoadTimestampColumn(destSchema)
		fullSchema = removeLoadTimestampColumn(fullSchema)
	} else {
		destSchema = addLoadTimestampColumn(destSchema)
		fullSchema = addLoadTimestampColumn(fullSchema)
	}

	// Staging mirrors the destination schema, with staging-only CDC columns retained.
	ingestSchema := destination.StagingIngestSchema(fullSchema, destSchema)
	ingestSchema = destination.PreserveSourceCDCColumnTypes(ingestSchema, fullSchema)
	if strategyUsesLogicalPrimaryKeys(resolvedStrategy) {
		ingestSchema = preserveLogicalPrimaryKeys(ingestSchema, fullSchema)
	}

	if inferBuffer != nil {
		if preStagedData != nil && p.preStagedUsable(preStageRpt, preStageKeyTransform, originalSourceSchema, ingestSchema) {
			config.Debug("[PIPELINE] Using %d rows of pre-staged load files; skipping buffer replay", preStagedData.RowCount())
			bufferedRecords = emptyRecordChannel()
			preStagedForJob = preStagedData
			_ = inferBuffer.Close()
			inferBuffer = nil
		} else {
			if preStagedData != nil {
				config.Debug("[PIPELINE] Discarding pre-staged load files; replaying from buffer")
				preStagedData.Close()
				preStagedData = nil
			}
			bufferTarget := p.buildBufferReaderTarget(originalSourceSchema, destSchema)
			bufferedRecords, err = inferBuffer.Reader(ctx, bufferTarget)
			if err != nil {
				return fmt.Errorf("failed to open buffer reader: %w", err)
			}
			inferBuffer = nil
		}
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

	applyPartitionNaming(&resolvedConfig, tableSchema, namingConv)

	// Primary key columns must be NOT NULL
	pkSet := make(map[string]bool, len(ingestSchema.PrimaryKeys))
	for _, pk := range ingestSchema.PrimaryKeys {
		pkSet[pk] = true
	}
	for i := range ingestSchema.Columns {
		if pkSet[ingestSchema.Columns[i].Name] {
			ingestSchema.Columns[i].Nullable = false
		}
	}

	// Validate strategy requirements
	if err := strat.Validate(&resolvedConfig); err != nil {
		return err
	}

	output.Statusf("\nStarting data ingestion...\n")

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
			if err := m.ValidateColumns(ingestSchema); err != nil {
				return fmt.Errorf("invalid mask configuration: %w", err)
			}
			m.ApplyToSchema(ingestSchema)
			columnMasker = m
		}
	}

	var whitespaceTrimmer *transformer.WhitespaceTrimmer
	if resolvedConfig.TrimWhitespace {
		whitespaceTrimmer = transformer.NewWhitespaceTrimmer()
	}

	var loadTimestampTransformer *transformer.LoadTimestamp
	if !p.config.NoLoadTimestamp && !p.config.Stream {
		loadTimestampTransformer = transformer.NewLoadTimestamp(loadTimestampColumnForSchema(ingestSchema), loadTimestamp)
	}

	job := &strategy.IngestionJob{
		Config:              &resolvedConfig,
		Table:               table,
		Destination:         dest,
		Schema:              ingestSchema,
		SourceSchema:        originalSourceSchema,
		Tracker:             jobTracker,
		BufferedRecords:     bufferedRecords,
		PreStaged:           preStagedForJob,
		SchemaComparison:    p.schemaComparison,
		DestinationSchema:   p.destinationSchema,
		ColumnRenamer:       p.columnRenamer,
		IngestrColumnFiller: p.ingestrColumnFiller,
		ColumnMasker:        columnMasker,
		WhitespaceTrimmer:   whitespaceTrimmer,
		LoadTimestamp:       loadTimestampTransformer,
		SchemaAligner:       transformer.NewSafeTypeCaster(ingestSchema.ToArrowSchema()).EnableRetarget(),
		EvolutionPlan:       evolutionPlan,
		CDCStateManager:     cdcStateManager,
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
	if cdcStateManager != nil {
		if err := cdcStateManager.ClaimTarget(ctx, p.config.SourceTable, p.config.DestTable); err != nil {
			return err
		}
		if err := cdcStateManager.BeginRun(ctx, p.config.FullRefresh); err != nil {
			return err
		}
	}

	if p.config.Stream {
		committer, _ := src.(source.StreamCommitter)
		legacyFinalizer, _ := src.(source.CDCLegacySlotFinalizer)
		exec := strategy.NewStreamingExecutor(strategy.StreamingOptions{
			FlushInterval:   p.config.FlushInterval,
			FlushRecords:    int64(p.config.FlushRecords),
			Strategy:        resolvedStrategy,
			Committer:       committer,
			StateManager:    cdcStateManager,
			LegacyFinalizer: legacyFinalizer,
		})
		if err := exec.Execute(ctx, job); err != nil {
			return fmt.Errorf("streaming ingestion failed: %w", err)
		}
		return nil
	}

	if err := strat.Execute(ctx, job); err != nil {
		return fmt.Errorf("ingestion failed: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if cdcStateManager != nil {
		stateProvider, ok := src.(source.CDCStateProvider)
		if !ok {
			return fmt.Errorf("postgres CDC source does not expose completed batch state")
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := cdcStateManager.Persist(ctx, stateProvider.CDCState()); err != nil {
			return fmt.Errorf("failed to persist destination CDC state: %w", err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	// After a successful, durable write, let CDC sources confirm the position
	// they caught up to (e.g. advance the replication slot's flush LSN). Safe
	// here because the write above committed. Best-effort: a failure to confirm
	// must not fail an otherwise-successful run.
	batchFinalized := false
	if finalizer, ok := src.(source.CDCBatchFinalizer); ok {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := finalizer.FinalizeBatch(ctx); err != nil {
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				return loss
			}
			config.Debug("[PIPELINE] CDC batch finalize failed: %v", err)
			return nil
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		batchFinalized = true
	}
	if cdcStateManager != nil && batchFinalized {
		if finalizer, ok := src.(source.CDCLegacySlotFinalizer); ok {
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				return err
			}
			if err := finalizer.FinalizeLegacySlot(ctx); err != nil {
				return fmt.Errorf("failed to finalize legacy PostgreSQL CDC slot: %w", err)
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

func connectorLeaseContext(parent context.Context, lease source.ConnectorLease) (context.Context, context.CancelFunc) {
	guard := source.NewConnectorLeaseGuard(lease)
	ctx, cancel := context.WithCancelCause(source.WithConnectorLeaseGuard(parent, guard))
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-guard.Done():
			cancel(guard.Err())
		case <-stop:
		}
	}()
	var stopOnce sync.Once
	return ctx, func() {
		stopOnce.Do(func() {
			close(stop)
			<-done
		})
	}
}

func resourceCloseContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), resourceCloseTimeout)
}

func rejectUnprovenLegacyCDCTarget(ctx context.Context, dest destination.Destination, sourceTable, destTable string) error {
	resumeProvider, ok := dest.(destination.CDCResumeProvider)
	if !ok {
		return nil
	}
	position, err := resumeProvider.GetMaxCDCLSN(ctx, destTable)
	if err != nil {
		return fmt.Errorf("failed to inspect legacy CDC cursor for %s: %w", sourceTable, err)
	}
	if position == "" {
		return nil
	}
	return fmt.Errorf("destination table %q contains PostgreSQL CDC data at LSN %s, but no matching completed CDC state exists for source table %q; rerun with --full-refresh or restore the completed v1 CDC state before upgrading", destTable, position, sourceTable)
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
		case config.ProgressJSON:
			display = progress.NewJSONDisplay()
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

func (p *Pipeline) inferSchemaFromData(
	ctx context.Context,
	table source.SourceTable,
	tracker progress.Tracker,
	preStage destination.PreStageWriter,
) (*schema.TableSchema, *databuffer.FileBuffer, destination.PreStagedData, *preStageReport, error) {
	// Create schema inferrer and file-backed data buffer
	inferrer := schemainfer.NewSchemaInferrer()
	buffer, err := databuffer.NewFileBuffer()
	if err != nil {
		if preStage != nil {
			preStage.Discard()
		}
		return nil, nil, nil, nil, fmt.Errorf("failed to create buffer: %w", err)
	}

	// Read all data from source
	parallelism := p.config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readOpts := source.ReadOptions{
		IncrementalKey:                  table.IncrementalKey(),
		IntervalStart:                   p.config.IntervalStart,
		IntervalEnd:                     p.config.IntervalEnd,
		ExtractPartitionBy:              p.config.ExtractPartitionBy,
		ExtractPartitionInterval:        p.config.ExtractPartitionInterval,
		ExtractPartitionNumericInterval: p.config.ExtractPartitionNumericInterval,
		ExtractPartitionAuto:            p.config.ExtractPartitionAuto,
		PageSize:                        p.config.PageSize,
		Limit:                           p.config.SQLLimit,
		ExcludeColumns:                  p.config.SQLExcludeColumns,
		Parallelism:                     parallelism,
		FullRefresh:                     p.config.FullRefresh,
		Columns:                         p.config.Columns,
	}

	records, err := table.Read(ctx, readOpts)
	if err != nil {
		_ = buffer.Close()
		if preStage != nil {
			preStage.Discard()
		}
		return nil, nil, nil, nil, fmt.Errorf("failed to read from source: %w", err)
	}

	// Wrap records with progress tracker for Extract logging
	if tracker != nil {
		records = tracker.Wrap(records)
	}

	// Feed all records to the inferrer, the replay buffer, and (when active)
	// the destination's pre-stage writer in parallel.
	for result := range records {
		if result.Err != nil {
			_ = buffer.Close()
			if preStage != nil {
				preStage.Discard()
			}
			return nil, nil, nil, nil, fmt.Errorf("error reading batch: %w", result.Err)
		}

		var wg sync.WaitGroup
		var inferErr, bufErr, preErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			inferErr = inferrer.AddBatch(result.Batch)
		}()
		go func() {
			defer wg.Done()
			bufErr = buffer.Append(ctx, result.Batch)
		}()
		if preStage != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				preErr = preStage.Append(ctx, result.Batch)
			}()
		}
		wg.Wait()

		result.Batch.Release()

		if inferErr != nil {
			_ = buffer.Close()
			if preStage != nil {
				preStage.Discard()
			}
			return nil, nil, nil, nil, fmt.Errorf("failed to infer schema from batch: %w", inferErr)
		}
		if bufErr != nil {
			_ = buffer.Close()
			if preStage != nil {
				preStage.Discard()
			}
			return nil, nil, nil, nil, fmt.Errorf("failed to buffer batch: %w", bufErr)
		}
		if preErr != nil {
			// Pre-staging is an optimization: a write failure (e.g. a value JSON
			// cannot encode) only cancels it, the extract continues.
			config.Debug("[PIPELINE] Pre-staging disabled after write error: %v", preErr)
			preStage.Discard()
			preStage = nil
		}
	}

	// Finalize pre-staged files and capture the inference facts needed to
	// judge them once the final schema and column names are resolved.
	var preStaged destination.PreStagedData
	var report *preStageReport
	if preStage != nil {
		data, finishErr := preStage.Finish()
		if finishErr != nil {
			config.Debug("[PIPELINE] Pre-staging finalize failed: %v", finishErr)
		} else if data != nil {
			preStaged = data
			report = &preStageReport{
				typeUnstableColumns:   inferrer.TypeUnstableColumns(),
				unknownStorageColumns: inferrer.UnknownStorageColumns(),
			}
		}
	}

	cleanup := func() {
		_ = buffer.Close()
		if preStaged != nil {
			preStaged.Close()
		}
	}

	// Protect columns specified via --columns from being dropped as all-null
	if err := inferrer.ProtectColumnOverrides(p.config.Columns); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}

	if partitionCol := resolvePartitionBy(p.config, table); partitionCol != "" {
		inferrer.ProtectColumns([]string{partitionCol})
	}

	// Infer schema
	tableSchema, err := inferrer.ToTableSchema(table.Name())
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("failed to build schema: %w", err)
	}

	if tableSchema == nil {
		cleanup()
		return nil, nil, nil, nil, nil
	}

	p.droppedColumns = inferrer.DroppedColumns()

	stats := inferrer.Stats()
	config.Debug("[PIPELINE] Schema inferred from %d batches, %d rows", stats.BatchCount, stats.RowCount)

	// Apply column type overrides before creating the buffer reader,
	// so the reader casts data to match the overridden types.
	if err := p.applyColumnOverrides(tableSchema); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("failed to apply column overrides: %w", err)
	}

	// For schema-less sources, also append override columns that were never seen
	// in the data — the buffer reader will fill them with nulls.
	if err := schemainfer.AppendMissingOverrideColumns(tableSchema, p.config.Columns, p.config.SchemaNaming); err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("failed to append missing override columns: %w", err)
	}

	return tableSchema, buffer, preStaged, report, nil
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
		if strings.EqualFold(scd, name) {
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

func (p *Pipeline) runMultiTable(ctx context.Context, src source.MultiTableSource) (retErr error) {
	tables, err := src.GetTables(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tables from multi-table source: %w", err)
	}

	if len(tables) == 0 {
		config.Debug("[PIPELINE] Multi-table source returned no tables")
		if isPostgresCDCSource(p.config.SourceURI) && !p.config.Stream {
			manager, err := strategy.NewCDCStateManager(
				p.dest,
				p.cdcConnectorID,
				"",
				p.config.StagingDataset,
			)
			if err != nil {
				return err
			}
			if err := manager.BeginRun(ctx, false); err != nil {
				return err
			}
			return nil
		}
		if !isPostgresCDCSource(p.config.SourceURI) {
			return nil
		}
	}

	config.Debug("[PIPELINE] Multi-table mode: %d tables", len(tables))

	namer, _ := p.dest.(destination.MultiTableNamer)
	tableDestNames, err := multiTableDestinationNames(tables, p.dest.GetScheme(), namer)
	if err != nil {
		return err
	}

	columnRenamers := make(map[string]*transformer.ColumnRenamer)
	for i := range tables {
		normalized, renamer, err := p.normalizeMultiTableInfo(ctx, tables[i], tableDestNames[tables[i].Name])
		if err != nil {
			return fmt.Errorf("failed to normalize columns for table %s: %w", tables[i].Name, err)
		}
		tables[i] = normalized
		if renamer != nil && renamer.HasRenames() {
			columnRenamers[normalized.Name] = renamer
		}
	}

	var loadTimestamp time.Time
	if !p.config.NoLoadTimestamp {
		if !p.config.Stream {
			loadTimestamp = time.Now().UTC().Truncate(time.Microsecond)
		}
		for i := range tables {
			tables[i].Schema = addLoadTimestampColumn(tables[i].Schema)
		}
	}

	// A single predicate cannot apply across heterogeneous table schemas.
	if strings.TrimSpace(p.config.IncrementalPredicate) != "" {
		return fmt.Errorf("incremental-predicate is not supported in multi-table mode")
	}

	resolvedStrategy := p.config.IncrementalStrategy
	if p.config.Stream && resolvedStrategy == "" {
		if ss, ok := src.(source.StreamingSource); ok {
			resolvedStrategy = ss.DefaultStreamingStrategy()
		}
	}
	if isCDCSource(p.config.SourceURI) && !p.config.FullRefresh && (resolvedStrategy == "" || resolvedStrategy == config.StrategyReplace) {
		resolvedStrategy = config.StrategyMerge
	}
	if p.config.FullRefresh {
		resolvedStrategy = config.StrategyReplace
	}

	if shouldWarnCDCStrategy(p.config, resolvedStrategy) {
		output.Warnf("Warning: change data source is using %q strategy instead of %q; delete and update operations may not be properly reflected in the destination\n", resolvedStrategy, config.StrategyMerge)
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
		defer func() { tracker.Stop(retErr) }()
	}

	var cdcStateManager *strategy.CDCStateManager
	anchorTable := ""
	if isPostgresCDCSource(p.config.SourceURI) {
		for _, destTable := range tableDestNames {
			if anchorTable == "" || destTable < anchorTable {
				anchorTable = destTable
			}
		}
		cdcStateManager, err = strategy.NewCDCStateManager(
			p.dest,
			p.cdcConnectorID,
			anchorTable,
			p.config.StagingDataset,
		)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if err := cdcStateManager.RegisterTableState(ctx, table.Name, tableDestNames[table.Name], table.Incarnation, table.SchemaFingerprint); err != nil {
				return err
			}
		}
	}

	// For CDC sources, query per-table max LSNs for resume
	var cdcResumeLSNs map[string]string
	if cdcStateManager != nil && !p.config.FullRefresh {
		cdcResumeLSNs = make(map[string]string)
		stateEmpty, err := cdcStateManager.StateEmpty(ctx)
		if err != nil {
			return err
		}
		for _, table := range tables {
			resumeLSN, err := cdcStateManager.ResumePosition(ctx, table.Name)
			if err != nil {
				return err
			}
			if resumeLSN != "" {
				cdcResumeLSNs[table.Name] = resumeLSN
				config.Debug("[PIPELINE] Found destination-managed CDC state for %s: %s", table.Name, resumeLSN)
			} else {
				if stateEmpty {
					if err := rejectUnprovenLegacyCDCTarget(ctx, p.dest, table.Name, tableDestNames[table.Name]); err != nil {
						return err
					}
				}
				config.Debug("[PIPELINE] No completed CDC snapshot state for %s, will snapshot", table.Name)
			}
		}
	} else if isCDCSource(p.config.SourceURI) && !p.config.FullRefresh {
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
				}
			}
		}
	}

	if cdcStateManager != nil {
		for _, table := range tables {
			if err := cdcStateManager.ClaimTarget(ctx, table.Name, tableDestNames[table.Name]); err != nil {
				return err
			}
		}
	}

	// Schema contract handling: build a per-table evolution plan so destination
	// tables gain columns added at the source (skip for replace, which drops and
	// recreates). Plans are built sequentially because evolveSchemaIfNeeded
	// keeps comparison state on the pipeline; strategies apply them per table.
	var evolutionPlans map[string]*schemaevolution.EvolutionPlan
	if resolvedStrategy != config.StrategyReplace {
		evolutionPlans = make(map[string]*schemaevolution.EvolutionPlan)
		for _, table := range tables {
			plan, err := p.evolveSchemaIfNeeded(ctx, tableDestNames[table.Name], table.Schema, resolvedStrategy)
			if err != nil {
				return fmt.Errorf("schema evolution failed for table %s: %w", table.Name, err)
			}
			if plan != nil {
				evolutionPlans[table.Name] = plan
			}
		}
	}
	var whitespaceTrimmer *transformer.WhitespaceTrimmer
	if resolvedConfig.TrimWhitespace {
		whitespaceTrimmer = transformer.NewWhitespaceTrimmer()
	}

	var loadTimestampTransformer *transformer.LoadTimestamp
	if !p.config.NoLoadTimestamp && !p.config.Stream {
		loadTimestampTransformer = transformer.NewLoadTimestamp(loadTimestampColumnForSchema(nil), loadTimestamp)
	}

	job := &strategy.MultiTableIngestionJob{
		Config:            &resolvedConfig,
		Source:            src,
		Destination:       p.dest,
		Tables:            tables,
		TableDestNames:    tableDestNames,
		Tracker:           tracker,
		CDCResumeLSNs:     cdcResumeLSNs,
		EvolutionPlans:    evolutionPlans,
		CDCStateManager:   cdcStateManager,
		WhitespaceTrimmer: whitespaceTrimmer,
		LoadTimestamp:     loadTimestampTransformer,
		ColumnRenamers:    columnRenamers,
		NormalizeTableInfo: func(ctx context.Context, table source.SourceTableInfo, destTable string) (source.SourceTableInfo, *transformer.ColumnRenamer, error) {
			return p.normalizeMultiTableInfo(ctx, table, destTable)
		},
	}
	if cdcStateManager != nil {
		if err := cdcStateManager.BeginRun(ctx, p.config.FullRefresh); err != nil {
			return err
		}
	}

	if p.config.Stream {
		committer, _ := src.(source.StreamCommitter)
		legacyFinalizer, _ := src.(source.CDCLegacySlotFinalizer)
		exec := strategy.NewStreamingExecutor(strategy.StreamingOptions{
			FlushInterval:   p.config.FlushInterval,
			FlushRecords:    int64(p.config.FlushRecords),
			Strategy:        resolvedStrategy,
			Committer:       committer,
			StateManager:    cdcStateManager,
			LegacyFinalizer: legacyFinalizer,
		})
		if err := exec.ExecuteMultiTable(ctx, job); err != nil {
			return fmt.Errorf("streaming ingestion failed: %w", err)
		}
		return nil
	}

	if err := mtStrat.ExecuteMultiTable(ctx, job); err != nil {
		return fmt.Errorf("multi-table ingestion failed: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if cdcStateManager != nil {
		stateProvider, ok := src.(source.CDCStateProvider)
		if !ok {
			return fmt.Errorf("postgres CDC source does not expose completed batch state")
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := cdcStateManager.Persist(ctx, stateProvider.CDCState()); err != nil {
			return fmt.Errorf("failed to persist destination CDC state: %w", err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}

	// After a successful, durable write, let CDC sources confirm the position
	// they caught up to (e.g. advance the replication slot's flush LSN). This is
	// safe here because the write above has committed. Best-effort: a failure to
	// confirm must not fail an otherwise-successful run.
	batchFinalized := false
	if finalizer, ok := src.(source.CDCBatchFinalizer); ok {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := finalizer.FinalizeBatch(ctx); err != nil {
			if loss := source.ConnectorLeaseLoss(ctx); loss != nil {
				return loss
			}
			config.Debug("[PIPELINE] CDC batch finalize failed: %v", err)
			return nil
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		batchFinalized = true
	}
	if cdcStateManager != nil && batchFinalized {
		if finalizer, ok := src.(source.CDCLegacySlotFinalizer); ok {
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				return err
			}
			if err := finalizer.FinalizeLegacySlot(ctx); err != nil {
				return fmt.Errorf("failed to finalize legacy PostgreSQL CDC slot: %w", err)
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

func multiTableDestinationNames(tables []source.SourceTableInfo, scheme string, namer destination.MultiTableNamer) (map[string]string, error) {
	destinations := make(map[string]string, len(tables))
	sourcesByDestination := make(map[string]string, len(tables))
	for _, table := range tables {
		// When funneling into a dest schema, the source-schema qualifier is
		// flattened into the table name ("dbo.orders" -> "<dest>.dbo_orders") so
		// the result is an unambiguous two-part name rather than something that
		// looks like a catalog.schema.table reference. Destinations with their own
		// naming rules (e.g. BigQuery) override via MultiTableNamer.
		destName := destination.ResolveMultiTableName(scheme, namer, table.DestSchema, table.Name)
		if existingSource, exists := sourcesByDestination[destName]; exists && existingSource != table.Name {
			return nil, fmt.Errorf("multi-table destination collision: source tables %q and %q both map to destination table %q", existingSource, table.Name, destName)
		}
		destinations[table.Name] = destName
		sourcesByDestination[destName] = table.Name
	}
	return destinations, nil
}

// evolveSchemaIfNeeded inspects the destination's current schema and builds an
// EvolutionPlan describing how it should change to accommodate sourceSchema.
func (p *Pipeline) evolveSchemaIfNeeded(ctx context.Context, destTable string, sourceSchema *schema.TableSchema, strategy config.IncrementalStrategy) (*schemaevolution.EvolutionPlan, error) {
	// Get destination table schema (nil if table doesn't exist)
	destSchema, err := p.dest.GetTableSchema(ctx, destTable)
	if err != nil {
		return nil, fmt.Errorf("failed to get destination schema: %w", err)
	}
	if destSchema == nil {
		config.Debug("[SCHEMA EVOLUTION] Destination table doesn't exist yet, skipping evolution")
		return nil, nil
	}

	comparisonDestSchema := destSchema
	if strategy == config.StrategySCD2 {
		comparisonDestSchema = removeSCD2MetadataColumns(destSchema)
	}
	comparisonDestSchema = destination.PreserveSourceCDCColumnTypes(comparisonDestSchema, sourceSchema)

	// Store destination schema for use by strategies.
	p.destinationSchema = comparisonDestSchema

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
	opts := &schemaevolution.CompareOptions{Overrides: overrides, PrimaryKeys: sourceSchema.PrimaryKeys}
	if normalizer, ok := p.dest.(destination.SchemaEvolutionColumnNormalizer); ok {
		opts.NormalizeColumn = normalizer.NormalizeSchemaEvolutionColumn
	}
	comparison, err := schemaevolution.Compare(destination.DestinationTableSchema(sourceSchema), comparisonDestSchema, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to compare schemas: %w", err)
	}
	if !comparison.HasChanges {
		config.Debug("[SCHEMA EVOLUTION] No schema changes detected")
		return &schemaevolution.EvolutionPlan{
			Table:       destTable,
			FinalSchema: schemaevolution.BuildFinalSchema(comparisonDestSchema, nil),
		}, nil
	}

	config.Debug("[SCHEMA EVOLUTION] Detected %d schema changes", len(comparison.Changes))

	// Log warnings for removed columns (Fivetran-style soft removal)
	for _, change := range comparison.Changes {
		if change.Type == schemaevolution.ChangeRemoveColumn {
			output.Warnf("Warning: Column %q no longer exists in source, future values will be NULL\n", change.ColumnName)
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
			Table:       destTable,
			FinalSchema: schemaevolution.BuildFinalSchema(comparisonDestSchema, p.filteredSchemaComparison),
		}, nil

	case schemaevolution.ContractDiscardRow:
		if contractResult.HasViolations() {
			for _, v := range contractResult.Violations {
				output.Warnf("Warning: %s - rows with schema violations will be discarded\n", v.Description)
			}
			config.Debug("[SCHEMA EVOLUTION] Discard row mode will filter out incompatible rows during ingestion")
		}
		// For discard_row, apply all schema changes but filter rows at write time
		p.filteredSchemaComparison = comparison

	case schemaevolution.ContractDiscardValue:
		if contractResult.HasViolations() {
			for _, v := range contractResult.Violations {
				output.Warnf("Warning: %s - non-conforming values will be set to NULL\n", v.Description)
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
				Table:       destTable,
				FinalSchema: schemaevolution.BuildFinalSchema(comparisonDestSchema, p.filteredSchemaComparison),
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

	// The destination is responsible for turning the abstract plan into DDL and
	// applying it. Destinations that cannot evolve schemas (e.g. schema-less
	// file or document stores) do not implement SchemaEvolver, so evolution is
	// skipped and the table is left unchanged.
	evolver, ok := p.dest.(schemaevolution.SchemaEvolver)
	if !ok {
		config.Debug("[SCHEMA EVOLUTION] Destination %s does not support schema evolution, skipping", p.dest.GetScheme())
		return &schemaevolution.EvolutionPlan{
			Table:       destTable,
			FinalSchema: schemaevolution.BuildFinalSchema(comparisonDestSchema, nil),
		}, nil
	}

	// Compute the post-evolution schema from the changes that will actually take
	// effect. Type changes only apply when the destination supports them; the
	// abstract plan still carries them so the destination can warn at apply time.
	applicable := schemaevolution.ApplicableComparison(p.filteredSchemaComparison, evolver.SupportsColumnTypeChanges())

	plan := &schemaevolution.EvolutionPlan{
		Table:       destTable,
		Comparison:  p.filteredSchemaComparison,
		FinalSchema: schemaevolution.BuildFinalSchema(comparisonDestSchema, applicable),
	}
	config.Debug("[SCHEMA EVOLUTION] Built plan with %d changes (deferred apply)", len(p.filteredSchemaComparison.Changes))
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
	legacyCols := make([]string, 0, len(ingestrCols))
	for _, colName := range ingestrCols {
		if strings.EqualFold(colName, naming.IngestrLoadedAtColumn) {
			continue
		}
		legacyCols = append(legacyCols, colName)
	}
	if len(legacyCols) == 0 {
		return nil, nil
	}

	p.ingestrColumnFiller = schemaevolution.NewIngestrColumnFiller(legacyCols)
	config.Debug("[INGESTR] Will fill %d ingestr columns with '-': %v", len(legacyCols), legacyCols)

	// Copy the source schema so the original stays clean for source SELECT queries.
	copied := *sourceSchema
	copied.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(copied.Columns, sourceSchema.Columns)

	for _, colName := range legacyCols {
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

func addLoadTimestampColumn(s *schema.TableSchema) *schema.TableSchema {
	if s == nil {
		return nil
	}

	result := *s
	result.Columns = append([]schema.Column{}, s.Columns...)

	for i, col := range result.Columns {
		if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
			result.Columns[i] = loadTimestampColumnWithName(col.Name, true)
			return &result
		}
	}

	result.Columns = append(result.Columns, loadTimestampColumnWithName(naming.IngestrLoadedAtColumn, true))
	return &result
}

func removeLoadTimestampColumn(s *schema.TableSchema) *schema.TableSchema {
	if s == nil {
		return nil
	}

	result := *s
	result.Columns = make([]schema.Column, 0, len(s.Columns))
	for _, col := range s.Columns {
		if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
			continue
		}
		result.Columns = append(result.Columns, col)
	}
	return &result
}

func removeSCD2MetadataColumns(s *schema.TableSchema) *schema.TableSchema {
	if s == nil {
		return nil
	}

	result := *s
	result.Columns = make([]schema.Column, 0, len(s.Columns))
	for _, col := range s.Columns {
		if isSCD2MetadataColumn(col.Name) {
			continue
		}
		result.Columns = append(result.Columns, col)
	}
	return &result
}

func preserveLogicalPrimaryKeys(ingestSchema, sourceSchema *schema.TableSchema) *schema.TableSchema {
	if ingestSchema == nil || sourceSchema == nil || len(sourceSchema.PrimaryKeys) == 0 {
		return ingestSchema
	}

	result := *ingestSchema
	result.PrimaryKeys = append([]string{}, sourceSchema.PrimaryKeys...)
	return &result
}

func markPrimaryKeyColumns(tableSchema *schema.TableSchema) {
	if tableSchema == nil || len(tableSchema.PrimaryKeys) == 0 {
		return
	}

	primaryKeys := make(map[string]bool, len(tableSchema.PrimaryKeys))
	for _, key := range tableSchema.PrimaryKeys {
		primaryKeys[strings.ToLower(key)] = true
	}
	for i := range tableSchema.Columns {
		tableSchema.Columns[i].IsPrimaryKey = primaryKeys[strings.ToLower(tableSchema.Columns[i].Name)]
	}
}

func (p *Pipeline) applyDestinationSchemaConstraints(tableSchema *schema.TableSchema) {
	if tableSchema == nil || tableSchema.IncrementalKey == "" || p.dest == nil {
		return
	}
	if p.dest.GetScheme() != "oracle" && p.dest.GetScheme() != "oracle+cx_oracle" {
		return
	}

	for i := range tableSchema.Columns {
		col := &tableSchema.Columns[i]
		if col.DataType != schema.TypeString || !strings.EqualFold(col.Name, tableSchema.IncrementalKey) {
			continue
		}
		if col.MaxLength <= 0 || col.MaxLength > oracleComparableStringLen {
			col.MaxLength = oracleComparableStringLen
		}
	}
}

func strategyUsesLogicalPrimaryKeys(strategy config.IncrementalStrategy) bool {
	switch strategy {
	case config.StrategyMerge, config.StrategyDeleteInsert, config.StrategySCD2, config.StrategyReplace:
		return true
	default:
		return false
	}
}

func loadTimestampColumnForSchema(s *schema.TableSchema) schema.Column {
	if s != nil {
		for _, col := range s.Columns {
			if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
				return loadTimestampColumnWithName(col.Name, true)
			}
		}
	}
	return loadTimestampColumnWithName(naming.IngestrLoadedAtColumn, true)
}

func loadTimestampColumnWithName(name string, nullable bool) schema.Column {
	return schema.Column{
		Name:     name,
		DataType: schema.TypeTimestampTZ,
		Nullable: nullable,
	}
}

func (p *Pipeline) setupNamingConvention(ctx context.Context, sourceSchema *schema.TableSchema) error {
	namingConv, err := p.resolveNamingConvention(ctx, sourceSchema)
	if err != nil {
		return err
	}
	return p.applyNamingConvention(sourceSchema, namingConv)
}

// applyPartitionNaming normalizes partition_by/cluster_by to the destination column
// naming (no-op for direct); partition_by falls back to the source partition column.
func applyPartitionNaming(cfg *config.IngestConfig, tableSchema *schema.TableSchema, namingConv naming.NamingConvention) {
	switch {
	case cfg.PartitionBy != "":
		cfg.PartitionBy = namingConv.Normalize(cfg.PartitionBy)
	case tableSchema.PartitionBy != "":
		cfg.PartitionBy = namingConv.Normalize(tableSchema.PartitionBy)
	}
	if len(cfg.ClusterBy) > 0 {
		clusterBy := make([]string, len(cfg.ClusterBy))
		for i, col := range cfg.ClusterBy {
			clusterBy[i] = namingConv.Normalize(col)
		}
		cfg.ClusterBy = clusterBy
	}
}

// resolveNamingConvention determines which naming convention applies, resolving
// the "auto" setting by inspecting the destination table. It never returns Auto.
func (p *Pipeline) resolveNamingConvention(ctx context.Context, sourceSchema *schema.TableSchema) (naming.NamingConvention, error) {
	return p.resolveNamingConventionForTable(ctx, sourceSchema, p.config.DestTable)
}

func (p *Pipeline) resolveNamingConventionForTable(ctx context.Context, sourceSchema *schema.TableSchema, destTable string) (naming.NamingConvention, error) {
	convention, err := naming.ParseConvention(p.config.SchemaNaming)
	if err != nil {
		return nil, err
	}

	// For auto detection, check if destination exists and has snake_case naming
	if convention == naming.Auto {
		destSchema, err := p.dest.GetTableSchema(ctx, destTable)
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

func (p *Pipeline) normalizeMultiTableInfo(ctx context.Context, table source.SourceTableInfo, destTable string) (source.SourceTableInfo, *transformer.ColumnRenamer, error) {
	if table.Schema == nil {
		return source.SourceTableInfo{}, nil, fmt.Errorf("table has no schema")
	}

	normalized := table
	normalized.Schema = cloneTableSchema(table.Schema)
	normalized.PrimaryKeys = append([]string(nil), table.PrimaryKeys...)

	convention, err := p.resolveNamingConventionForTable(ctx, table.Schema, destTable)
	if err != nil {
		return source.SourceTableInfo{}, nil, err
	}

	namingMapping := make(map[string]string)
	if convention.Name() != string(naming.Direct) {
		for sourceName, destinationName := range naming.BuildColumnMapping(table.Schema, convention) {
			if destination.IsCDCMetaColumn(sourceName) {
				continue
			}
			if destination.IsCDCMetaColumn(destinationName) {
				return source.SourceTableInfo{}, nil, fmt.Errorf("source column %q normalizes to reserved CDC metadata column %q", sourceName, destinationName)
			}
			namingMapping[sourceName] = destinationName
		}
		applyColumnMappingToSchema(normalized.Schema, namingMapping)
	}

	maxLen := destination.MaxIdentifierLength(p.dest.GetScheme())
	shorteningMapping := destination.ShortenColumnNames(normalized.Schema.Columns, maxLen, namingMapping)
	for sourceName := range shorteningMapping {
		if destination.IsCDCMetaColumn(sourceName) {
			delete(shorteningMapping, sourceName)
		}
	}
	applyColumnMappingToSchema(normalized.Schema, shorteningMapping)

	combined := composeColumnMappings(namingMapping, shorteningMapping)
	normalized.PrimaryKeys = mapColumnNames(normalized.PrimaryKeys, combined)
	if len(normalized.PrimaryKeys) == 0 && len(normalized.Schema.PrimaryKeys) > 0 {
		normalized.PrimaryKeys = append([]string(nil), normalized.Schema.PrimaryKeys...)
	}
	normalized.Schema.PrimaryKeys = append([]string(nil), normalized.PrimaryKeys...)
	markPrimaryKeyColumns(normalized.Schema)

	if len(combined) == 0 {
		return normalized, nil, nil
	}
	return normalized, transformer.NewColumnRenamer(combined), nil
}

func cloneTableSchema(input *schema.TableSchema) *schema.TableSchema {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.Columns = append([]schema.Column(nil), input.Columns...)
	cloned.PrimaryKeys = append([]string(nil), input.PrimaryKeys...)
	return &cloned
}

func composeColumnMappings(first, second map[string]string) map[string]string {
	combined := make(map[string]string, len(first)+len(second))
	for sourceName, intermediateName := range first {
		if finalName, ok := second[intermediateName]; ok {
			combined[sourceName] = finalName
		} else {
			combined[sourceName] = intermediateName
		}
	}
	for sourceName, destinationName := range second {
		combined[sourceName] = destinationName
	}
	return combined
}

func mapColumnNames(columns []string, mapping map[string]string) []string {
	mapped := append([]string(nil), columns...)
	for i, name := range mapped {
		if destinationName, ok := mapping[name]; ok {
			mapped[i] = destinationName
		}
	}
	return dedupeStringsPreserveOrder(mapped)
}

func applyColumnMappingToSchema(s *schema.TableSchema, mapping map[string]string) {
	if s == nil || len(mapping) == 0 {
		return
	}
	for i := range s.Columns {
		if newName, ok := mapping[s.Columns[i].Name]; ok {
			s.Columns[i].Name = newName
		}
	}
	s.Columns = dedupeMappedColumns(s.Columns)
	s.PrimaryKeys = mapColumnNames(s.PrimaryKeys, mapping)
	if newName, ok := mapping[s.IncrementalKey]; ok {
		s.IncrementalKey = newName
	}
	if newName, ok := mapping[s.PartitionBy]; ok {
		s.PartitionBy = newName
	}
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
	output.Infof("Naming convention: %s (%d columns will be renamed)\n", namingConv.Name(), len(columnMapping))

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
	output.Infof("Identifier shortening: %d column(s) shortened to fit %d-byte limit\n", len(mapping), maxLen)
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
	// MaxLength of 0 means unbounded (the widest), so it wins over any bounded
	// length rather than being treated as the smallest.
	merged.MaxLength = schemaevolution.WidenedStringLength(existing.MaxLength, next.MaxLength)

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
			if col.DataType != newCol.DataType || col.Precision != newCol.Precision || col.Scale != newCol.Scale || col.MaxLength != newCol.MaxLength {
				output.Infof("Column override: %q type changed from %s(p=%v,s=%v,len=%v) to %s(p=%v,s=%v,len=%v)\n",
					col.Name, col.DataType, col.Precision, col.Scale, col.MaxLength, newCol.DataType, newCol.Precision, newCol.Scale, newCol.MaxLength)
			}
			sourceSchema.Columns[i] = newCol
			config.Debug("[PIPELINE] Column override applied: %s -> %v", col.Name, override.DataType)
			applied++
		}

		if override.RenameTo != "" && override.RenameTo != sourceSchema.Columns[i].Name {
			renameMap[sourceSchema.Columns[i].Name] = override.RenameTo
			output.Infof("Column rename: %q -> %q (from --columns)\n", sourceSchema.Columns[i].Name, override.RenameTo)
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
	return isManagedChangeSource(cfg.SourceURI) && !cfg.FullRefresh && resolvedStrategy != config.StrategyMerge
}

func resolveStrategy(cfg *config.IngestConfig, src source.Source, table source.SourceTable) config.IncrementalStrategy {
	var s config.IncrementalStrategy
	if src.HandlesIncrementality() {
		s = table.Strategy()
	} else {
		s = cfg.IncrementalStrategy
	}
	if cfg.Stream && s == "" {
		if ss, ok := src.(source.StreamingSource); ok {
			s = ss.DefaultStreamingStrategy()
		}
	}
	if isManagedChangeSource(cfg.SourceURI) && !cfg.FullRefresh && (s == "" || s == config.StrategyReplace) {
		s = config.StrategyMerge
		if len(resolvePrimaryKeys(cfg, table)) == 0 {
			// Keyless CDC table (no primary key, no replica identity index):
			// there is nothing to merge on, so land the change log append-only.
			s = config.StrategyAppend
		}
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

func validateExtractPartitionSupport(cfg *config.IngestConfig, table source.SourceTable) error {
	if cfg.ExtractPartitionBy == "" {
		return nil
	}
	if table.Name() == source.CustomQueryTableName {
		return fmt.Errorf("custom queries do not support extract partitioning")
	}
	provider, ok := table.(source.ExtractPartitioningProvider)
	if !ok || !provider.SupportsExtractPartitioning() {
		return fmt.Errorf("source table %q does not support extract partitioning; v1 supports normal SQL table scans for postgres, mysql, mssql, sqlite, and ADBC-backed sources", table.Name())
	}
	return nil
}

func validateExtractPartitionStrategy(cfg *config.IngestConfig, resolvedStrategy config.IncrementalStrategy) error {
	if cfg.ExtractPartitionBy == "" {
		return nil
	}
	switch resolvedStrategy {
	case config.StrategyReplace, config.StrategyTruncateInsert:
		return &config.ValidationError{Field: "incremental-strategy", Message: fmt.Sprintf("%q cannot be combined with extract partitioning because it rewrites the whole destination table from a bounded source read", resolvedStrategy)}
	default:
		return nil
	}
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
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(uri[:schemeEnd]), "+cdc")
}

func isPostgresCDCSource(rawURI string) bool {
	schemeEnd := strings.Index(rawURI, "://")
	if schemeEnd == -1 {
		return false
	}
	scheme := strings.ToLower(rawURI[:schemeEnd])
	return scheme == "postgres+cdc" || scheme == "postgresql+cdc"
}

func resolvedCDCStateConnectorID(cfg *config.IngestConfig, identity source.ConnectorIdentity, destinationIdentity string) string {
	if parsed, err := url.Parse(cfg.SourceURI); err == nil {
		if stateID := parsed.Query().Get("state_id"); stateID != "" {
			seed := "explicit:" + stateID + "\x00" + identity.Database
			if destinationIdentity != "" {
				seed += "\x00" + destinationIdentity
			}
			sum := sha256.Sum256([]byte(seed))
			return fmt.Sprintf("%x", sum[:8])
		}
	}

	destinationTarget := cfg.DestTable
	if destinationIdentity != "" {
		destinationTarget = destinationIdentity
	}
	identityParts := []string{
		identity.Connector,
		canonicalCDCStateURI(cfg.DestURI),
		cfg.SourceTable,
		destinationTarget,
	}
	connectorIdentity := strings.Join(identityParts, "\x00")
	sum := sha256.Sum256([]byte(connectorIdentity))
	return fmt.Sprintf("%x", sum[:8])
}

func managedCDCDestinationIdentity(ctx context.Context, dest destination.Destination, target string) (string, error) {
	provider, ok := dest.(destination.CDCTargetIdentityProvider)
	if !ok {
		return target, nil
	}
	return provider.CanonicalCDCTarget(ctx, target)
}

// validateMultiTableNamespace rejects a multi-table CDC run whose destination
// requires a namespace that neither dest_schema nor the destination URI
// supplies. Left to the destination, the failure names an internal synthesized
// table instead of the setting the user has to change.
func validateMultiTableNamespace(cfg *config.IngestConfig, dest destination.Destination, target string) error {
	if cfg.SourceTable != "" {
		return nil
	}
	if cfg.DestTable != "" {
		output.Warnf("Warning: --dest-table=%s is ignored in multi-table CDC mode; use ?dest_schema= on the source URI\n", cfg.DestTable)
	}
	capability, ok := destination.RequiresNamespace(dest.GetScheme())
	if !ok || capability.CheckName(target) == nil {
		return nil
	}
	namespace := capability.Labels[1]
	return fmt.Errorf(
		"multi-table CDC to %s requires a %s: add ?dest_schema=<%s> to the source URI, or a default %s to the destination URI",
		dest.GetScheme(), namespace, namespace, namespace)
}

func managedCDCDestinationTarget(cfg *config.IngestConfig, dest destination.Destination) string {
	if cfg.SourceTable != "" {
		return cfg.DestTable
	}

	const namespaceTable = "__ingestr_cdc_namespace__"
	destSchema := ""
	if parsed, err := url.Parse(cfg.SourceURI); err == nil {
		destSchema = parsed.Query().Get("dest_schema")
	}
	if namer, ok := dest.(destination.MultiTableNamer); ok {
		return namer.DestTableName(destSchema, namespaceTable)
	}
	return destination.DefaultMultiTableName(destSchema, namespaceTable)
}

func genericCDCConnectorID(cfg *config.IngestConfig) string {
	identity := strings.Join([]string{
		canonicalCDCStateURI(cfg.SourceURI),
		canonicalCDCStateURI(cfg.DestURI),
		cfg.SourceTable,
		cfg.DestTable,
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%x", sum[:8])
}

func canonicalCDCStateURI(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}
	normalizePostgresURI(parsed)
	parsed.User = nil
	query := parsed.Query()
	for _, key := range []string{
		"state_id", "mode", "binary", "discover_interval",
		"database", "dbname",
		"password", "pass", "token", "secret", "api_key", "private_key",
	} {
		query.Del(key)
	}
	for key := range query {
		if strings.HasPrefix(key, "pool_") {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func normalizePostgresURI(parsed *url.URL) {
	originalScheme := strings.ToLower(parsed.Scheme)
	isPostgres := originalScheme == "postgres+cdc" || originalScheme == "postgresql+cdc" ||
		originalScheme == "postgres" || originalScheme == "postgresql" || originalScheme == "postgresql+psycopg2"
	switch originalScheme {
	case "postgresql+cdc":
		parsed.Scheme = "postgres+cdc"
	case "postgresql", "postgresql+psycopg2":
		parsed.Scheme = "postgres"
	default:
		parsed.Scheme = strings.ToLower(parsed.Scheme)
	}
	if isPostgres {
		database := parsed.Query().Get("database")
		if database == "" {
			database = parsed.Query().Get("dbname")
		}
		if database == "" {
			database = strings.TrimLeft(parsed.Path, "/")
		}
		if database == "" && parsed.User != nil {
			database = parsed.User.Username()
		}
		if database != "" {
			parsed.Path = "/" + database
			parsed.RawPath = "/" + url.PathEscape(database)
		}
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "5432" {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
}

func isManagedChangeSource(uri string) bool {
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return false
	}
	scheme := strings.ToLower(uri[:schemeEnd])
	return strings.Contains(scheme, "+cdc") || strings.Contains(scheme, "+ct")
}

func validateManagedChangeConfig(cfg *config.IngestConfig) error {
	if cfg.ExtractPartitionBy != "" || cfg.ExtractPartitionInterval != 0 || cfg.ExtractPartitionNumericInterval != 0 || cfg.ExtractPartitionAuto {
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	if isPostgresCDCSource(cfg.SourceURI) && !cfg.FullRefresh {
		switch cfg.IncrementalStrategy {
		case config.StrategyDeleteInsert, config.StrategySCD2, config.StrategyTruncateInsert:
			return &config.ValidationError{
				Field:   "incremental-strategy",
				Message: fmt.Sprintf("%q is not supported for PostgreSQL CDC; use merge or replace", cfg.IncrementalStrategy),
			}
		}
	}
	if isChangeTrackingSource(cfg.SourceURI) && cfg.SQLLimit > 0 {
		return &config.ValidationError{Field: "sql-limit", Message: "is not supported for SQL Server Change Tracking sources because partial snapshots cannot safely advance the resume cursor"}
	}
	return nil
}

func validateIncrementalPredicate(cfg *config.IngestConfig, dest destination.Destination, resolvedStrategy config.IncrementalStrategy) error {
	if strings.TrimSpace(cfg.IncrementalPredicate) == "" {
		return nil
	}
	if resolvedStrategy != config.StrategyMerge {
		return fmt.Errorf("incremental-predicate can only be used with the merge incremental strategy, but the resolved strategy is %q", resolvedStrategy)
	}
	if sup, ok := dest.(destination.IncrementalPredicateSupport); !ok || !sup.SupportsIncrementalPredicate() {
		return fmt.Errorf("destination scheme %q does not support --incremental-predicate", dest.GetScheme())
	}
	return nil
}

func validateChangeTrackingDestination(dest destination.Destination) error {
	if _, ok := dest.(destination.CDCResumeProvider); !ok {
		return fmt.Errorf("destination scheme %q does not support resume cursors required by SQL Server Change Tracking", dest.GetScheme())
	}
	return nil
}

func supportsDestinationManagedCDCState(dest destination.Destination) bool {
	if _, ok := dest.(destination.CDCStateReader); !ok {
		return false
	}
	if _, ok := dest.(destination.CDCStateFenceReader); !ok {
		return false
	}
	if _, ok := dest.(destination.CDCStatePruner); !ok {
		return false
	}
	_, ok := dest.(destination.TruncateCapable)
	return ok
}

func validateDestinationManagedCDCState(dest destination.Destination) error {
	if !supportsDestinationManagedCDCState(dest) {
		return fmt.Errorf("destination scheme %q cannot safely run PostgreSQL CDC: destination-managed state with fencing, pruning, and truncation is not supported", dest.GetScheme())
	}
	if _, ok := dest.(destination.CDCTargetClaimer); !ok {
		return fmt.Errorf("destination scheme %q cannot safely run PostgreSQL CDC: atomic destination-table claims are not supported", dest.GetScheme())
	}
	if _, ok := dest.(destination.CDCTargetIncarnationProvider); !ok {
		return fmt.Errorf("destination scheme %q cannot safely run PostgreSQL CDC: destination-table incarnation checks are not supported", dest.GetScheme())
	}
	cdcMerge, ok := dest.(destination.CDCMergeAware)
	if !ok || !cdcMerge.SupportsCDCMerge() {
		return fmt.Errorf("destination scheme %q cannot safely run PostgreSQL CDC: CDC-aware merge is not supported", dest.GetScheme())
	}
	unchangedCols, ok := dest.(destination.CDCUnchangedColsAware)
	if !ok || !unchangedCols.SupportsCDCUnchangedCols() {
		return fmt.Errorf("destination scheme %q cannot safely run PostgreSQL CDC: preserving unchanged TOAST columns is not supported", dest.GetScheme())
	}
	validator, ok := dest.(destination.ManagedCDCStateValidator)
	if !ok {
		return nil
	}
	if err := validator.ValidateManagedCDCState(); err != nil {
		return fmt.Errorf("destination scheme %q cannot safely use managed CDC state: %w", dest.GetScheme(), err)
	}
	return nil
}

func isChangeTrackingSource(uri string) bool {
	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return false
	}
	return strings.Contains(strings.ToLower(uri[:schemeEnd]), "+ct")
}

// cdcSlotSuffix returns a 20-hex-char hash of the connector destination identity for use as a
// replication slot name suffix, making auto-generated slot names unique per destination.
func cdcSlotSuffix(destURI string) string {
	h := sha256.Sum256([]byte(destURI))
	return fmt.Sprintf("%x", h[:10])
}

func legacyCDCSlotSuffix(rawDestURI string) string {
	h := sha256.Sum256([]byte(rawDestURI))
	return fmt.Sprintf("%x", h[:3])
}

func emptyRecordChannel() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}
