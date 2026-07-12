package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/metrics"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	// streamDrainTimeout bounds how long shutdown waits for the source to flush
	// its internal buffers into the channel after cancellation.
	streamDrainTimeout = 15 * time.Second

	// streamFinalFlushTimeout bounds the final flush performed on a detached
	// context after the run context has been cancelled.
	streamFinalFlushTimeout = 60 * time.Second

	streamAbortDrainTimeout = time.Second
)

// StreamingOptions configures the streaming flush loop.
type StreamingOptions struct {
	FlushInterval   time.Duration
	FlushRecords    int64
	Strategy        config.IncrementalStrategy // merge or append
	Committer       source.StreamCommitter     // nil = source needs no durability feedback
	StateManager    *CDCStateManager           // nil = source does not use destination-managed CDC state
	LegacyFinalizer source.CDCLegacySlotFinalizer
}

// StreamingExecutor runs continuous ingestion: it buffers record batches from
// the source and flushes them to the destination whenever FlushInterval
// elapses or FlushRecords rows are buffered, whichever comes first. Merge mode
// reuses one staging table per destination table across cycles; append mode
// writes directly. After each successful flush the source's CommitStream is
// invoked so it can confirm the durable position (LSN, delivery tag, offsets).
type StreamingExecutor struct {
	opts StreamingOptions
}

func NewStreamingExecutor(opts StreamingOptions) *StreamingExecutor {
	return &StreamingExecutor{opts: opts}
}

func (e *StreamingExecutor) Execute(ctx context.Context, job *IngestionJob) error {
	_, atomicSnapshots := job.Destination.(destination.AtomicSnapshotPublisher)
	if !atomicSnapshots {
		if err := job.ApplyEvolution(ctx); err != nil {
			return fmt.Errorf("failed to apply schema evolution: %w", err)
		}
	}

	st := &streamTableState{
		destTable:      job.Config.DestTable,
		schema:         job.Schema,
		primaryKeys:    job.Config.PrimaryKeys,
		incrementalKey: job.Schema.IncrementalKey,
		isCDC:          hasCDCColumns(job.Schema),
		partitionBy:    job.Config.PartitionBy,
		clusterBy:      job.Config.ClusterBy,
	}
	if atomicSnapshots {
		st.deferredEvolution = job.ApplyEvolution
		if job.EvolutionPlan != nil && job.EvolutionPlan.FinalSchema != nil {
			st.atomicTargetSchema = job.EvolutionPlan.FinalSchema
		} else if job.DestinationSchema != nil {
			st.atomicTargetSchema = job.DestinationSchema
		}
		if mode, _ := schemaevolution.ParseContractMode(job.Config.SchemaContract); mode == schemaevolution.ContractDiscardValue && st.atomicTargetSchema != nil {
			comparison := job.SchemaComparison
			if job.EvolutionPlan != nil && job.EvolutionPlan.TransformComparison != nil {
				comparison = job.EvolutionPlan.TransformComparison
			}
			st.atomicWriteSchema = st.atomicTargetSchema
			st.atomicBatchTransformer = schemaevolution.NewDiscardValueTransformer(comparison, st.schema, st.atomicWriteSchema)
		}
	}
	loop := newFlushLoop(job.Destination, job.Config, e.opts, map[string]*streamTableState{"": st})
	defer loop.cleanup(ctx)
	if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
		return err
	}
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	records, err := job.GetRecords(readCtx, source.ReadOptions{
		IncrementalKey:             job.Config.IncrementalKey,
		IntervalStart:              job.Config.IntervalStart,
		PageSize:                   job.Config.PageSize,
		ExcludeColumns:             job.Config.SQLExcludeColumns,
		Parallelism:                parallelism,
		Schema:                     job.SourceSchema,
		CDCResumeLSN:               job.Config.CDCResumeLSN,
		CDCResumeIncarnation:       job.Config.CDCResumeIncarnation,
		CDCResumeSchemaFingerprint: job.Config.CDCResumeSchemaFingerprint,
		CDCSlotSuffix:              job.Config.CDCSlotSuffix,
		CDCLegacySlotSuffix:        job.Config.CDCLegacySlotSuffix,
		CDCSnapshotReplace:         st.isCDC && supportsCDCSnapshotReplace(job.Destination),
		CDCStableDataBatches:       st.isCDC && e.opts.StateManager != nil && e.opts.Strategy == config.StrategyAppend,
		Streaming:                  true,
		FlushInterval:              job.Config.FlushInterval,
		FlushRecords:               job.Config.FlushRecords,
	})
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	// CDC sources re-announce their table (with its refreshed schema) after
	// rebuilding their stream around mid-stream DDL; evolve the destination
	// before the new-shape batches arrive. Sources that never announce
	// (message brokers) leave this callback unused.
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	loop.evolveTablePlan = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) (*schemaevolution.EvolutionPlan, error) {
		return evolveDestinationTablePlan(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	err = loop.run(ctx, records)
	if err != nil {
		cancelRead()
		drainAndReleaseUntil(records, streamAbortDrainTimeout)
	}
	return err
}

// ExecuteMultiTable runs the streaming loop over a multi-table source (CDC).
func (e *StreamingExecutor) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	anyTableHasCDC := false
	for _, ti := range job.Tables {
		if hasCDCColumns(ti.Schema) {
			anyTableHasCDC = true
			break
		}
	}
	if anyTableHasCDC && e.opts.Strategy == config.StrategyMerge {
		warnIfCDCMergeUnsupported(job.Destination)
	}

	tables := make(map[string]*streamTableState, len(job.Tables))
	loop := newFlushLoop(job.Destination, job.Config, e.opts, tables)
	defer loop.cleanup(ctx)
	for _, ti := range job.Tables {
		sourceSchema := ti.Schema
		st := &streamTableState{
			destTable:         job.GetDestTableName(ti.Name),
			schema:            ti.Schema,
			primaryKeys:       ti.PrimaryKeys,
			incrementalKey:    ti.Schema.IncrementalKey,
			isCDC:             hasCDCColumns(ti.Schema),
			incarnation:       ti.Incarnation,
			schemaFingerprint: ti.SchemaFingerprint,
		}
		configureStreamingContractState(job.Config, sourceSchema, job.EvolutionPlans[ti.Name], st)
		if _, atomicSnapshots := job.Destination.(destination.AtomicSnapshotPublisher); atomicSnapshots {
			sourceTable := ti.Name
			st.deferredEvolution = func(ctx context.Context) error { return job.ApplyEvolutionFor(ctx, sourceTable) }
			configureAtomicStreamingContractState(job.Config, sourceSchema, job.EvolutionPlans[sourceTable], st)
		} else {
			if err := job.ApplyEvolutionFor(ctx, ti.Name); err != nil {
				return fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
			}
		}
		tables[ti.Name] = st
		if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
			return err
		}
	}
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	knownTables := make([]string, 0, len(job.Tables))
	for _, table := range job.Tables {
		knownTables = append(knownTables, table.Name)
	}
	resumeIncarnations, resumeSchemas := cdcResumeMetadata(job.Tables)
	records, err := job.ReadAll(readCtx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:          parallelism,
			PageSize:             job.Config.PageSize,
			CDCSlotSuffix:        job.Config.CDCSlotSuffix,
			CDCLegacySlotSuffix:  job.Config.CDCLegacySlotSuffix,
			CDCSnapshotReplace:   (anyTableHasCDC || e.opts.StateManager != nil) && supportsCDCSnapshotReplace(job.Destination),
			CDCStableDataBatches: anyTableHasCDC && e.opts.StateManager != nil && e.opts.Strategy == config.StrategyAppend,
			Streaming:            true,
			FlushInterval:        job.Config.FlushInterval,
			FlushRecords:         job.Config.FlushRecords,
		},
		KnownTables:                 knownTables,
		CDCResumeLSNs:               job.CDCResumeLSNs,
		CDCResumeIncarnations:       resumeIncarnations,
		CDCResumeSchemaFingerprints: resumeSchemas,
	})
	if err != nil {
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}
	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	// CDC sources announce tables created mid-stream before emitting their
	// batches. Normal runs stop at that boundary so a restart can include the
	// table in the startup set; explicit full refresh retains dynamic handling.
	loop.prepareNewTable = func(ctx context.Context, ti source.SourceTableInfo) (*streamTableState, error) {
		if !job.Config.FullRefresh {
			return nil, fmt.Errorf("newly discovered CDC table %q requires restarting the stream before it can be ingested", ti.Name)
		}
		if !job.Config.NoLoadTimestamp {
			ti.Schema = withLoadTimestampColumn(ti.Schema)
		}
		if ti.Schema == nil {
			return nil, fmt.Errorf("newly discovered table %s has no schema", ti.Name)
		}
		st := &streamTableState{
			destTable:         multiTableDestName(job.Destination, ti),
			schema:            ti.Schema,
			primaryKeys:       ti.PrimaryKeys,
			incrementalKey:    ti.Schema.IncrementalKey,
			isCDC:             hasCDCColumns(ti.Schema),
			incarnation:       ti.Incarnation,
			schemaFingerprint: ti.SchemaFingerprint,
		}
		plan, err := planDestinationSchemaEvolution(ctx, job.Destination, st.destTable, ti.Schema, job.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to validate schema contract for newly discovered table %s: %w", ti.Name, err)
		}
		configureStreamingContractState(job.Config, ti.Schema, plan, st)
		if _, atomicSnapshots := job.Destination.(destination.AtomicSnapshotPublisher); atomicSnapshots {
			st.deferredEvolution = func(ctx context.Context) error { return applyEvolutionPlan(ctx, job.Destination, plan) }
			configureAtomicStreamingContractState(job.Config, ti.Schema, plan, st)
		} else if err := applyEvolutionPlan(ctx, job.Destination, plan); err != nil {
			return nil, fmt.Errorf("failed to evolve newly discovered table %s: %w", ti.Name, err)
		}
		for sourceTable, destTable := range job.TableDestNames {
			if sourceTable != ti.Name && destTable == st.destTable {
				return nil, fmt.Errorf("multi-table destination collision: source tables %q and %q both map to destination table %q", sourceTable, ti.Name, st.destTable)
			}
		}
		if e.opts.StateManager != nil {
			if err := e.opts.StateManager.ClaimLateDiscoveredTarget(
				ctx,
				ti.Name,
				st.destTable,
				ti.Incarnation,
				ti.SchemaFingerprint,
				job.Config.FullRefresh,
				e.lateTargetPrepareOptions(st),
			); err != nil {
				return nil, err
			}
		}
		pendingSafeBoundary := e.opts.StateManager != nil && e.opts.StateManager.HasPendingLateSnapshotBoundary(ti.Name)
		if pendingSafeBoundary {
			if err := e.prepareLateStagingTable(ctx, job.Destination, job.Config, st); err != nil {
				cleanupUnregisteredStreamTable(ctx, job.Destination, st)
				return nil, err
			}
		} else if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
			cleanupUnregisteredStreamTable(ctx, job.Destination, st)
			return nil, err
		}
		if job.TableDestNames == nil {
			job.TableDestNames = make(map[string]string)
		}
		job.TableDestNames[ti.Name] = st.destTable
		if e.opts.StateManager != nil {
			if err := e.opts.StateManager.RegisterTableState(ctx, ti.Name, st.destTable, ti.Incarnation, ti.SchemaFingerprint); err != nil {
				cleanupUnregisteredStreamTable(ctx, job.Destination, st)
				return nil, err
			}
		}
		if job.EvolutionPlans == nil {
			job.EvolutionPlans = make(map[string]*schemaevolution.EvolutionPlan)
		}
		job.EvolutionPlans[ti.Name] = plan
		return st, nil
	}
	// CDC sources also re-announce a table (with its refreshed schema) after
	// rebuilding their stream around mid-stream DDL — a column added or a type
	// changed on the source while streaming. The destination is evolved before
	// the new-shape batches arrive.
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	loop.evolveTablePlan = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) (*schemaevolution.EvolutionPlan, error) {
		return evolveDestinationTablePlan(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	err = loop.run(ctx, records)
	if err != nil {
		cancelRead()
		drainAndReleaseUntil(records, streamAbortDrainTimeout)
	}
	return err
}

func cleanupUnregisteredStreamTable(ctx context.Context, dest destination.Destination, st *streamTableState) {
	if st == nil || connectorLeaseLoss(ctx) != nil {
		return
	}
	if st.targetCreatedByRun && !st.atomicPublishAttempted {
		cleanupFailedOwnedDirectReplace(ctx, dest, st.destTable, true, st.targetOwnershipToken)
		st.targetCreatedByRun = false
	}
	if st.stagingTable == "" {
		return
	}
	cleanupCtx, cancel := detachedLeaseContext(ctx, streamFinalFlushTimeout)
	defer cancel()
	if connectorLeaseLoss(cleanupCtx) != nil {
		return
	}
	if err := dest.DropTable(cleanupCtx, st.stagingTable); err != nil {
		config.Debug("[STREAM] Warning: failed to drop unregistered staging table %s: %v", st.stagingTable, err)
	}
}

func (e *StreamingExecutor) lateTargetPrepareOptions(st *streamTableState) destination.PrepareOptions {
	opts := destination.PrepareOptions{
		Table:       st.destTable,
		Schema:      destination.DestinationTableSchema(st.schema),
		PrimaryKeys: st.primaryKeys,
		PartitionBy: st.partitionBy,
		ClusterBy:   st.clusterBy,
	}
	if e.opts.Strategy == config.StrategyAppend || (st.isCDC && len(st.primaryKeys) == 0) {
		opts.PrimaryKeys = nil
		if st.isCDC {
			opts.Schema = st.schema
			opts.CDCMode = true
		}
	}
	return opts
}

func (e *StreamingExecutor) prepareLateStagingTable(ctx context.Context, dest destination.Destination, cfg *config.IngestConfig, st *streamTableState) error {
	if e.opts.Strategy != config.StrategyMerge || (st.isCDC && len(st.primaryKeys) == 0) {
		return nil
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	st.stagingTable = managedStagingTableName(dest, st.destTable, "stream", cfg.StagingDataset)
	output.Statusf("[STREAM] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), st.stagingTable)
	if err := dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        st.stagingTable,
		Schema:       st.schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		CDCMode:      st.isCDC,
		PartitionBy:  st.partitionBy,
		ClusterBy:    st.clusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare staging table %s: %w", st.stagingTable, err)
	}
	return connectorLeaseLoss(ctx)
}

func configureStreamingContractState(cfg *config.IngestConfig, sourceSchema *schema.TableSchema, plan *schemaevolution.EvolutionPlan, st *streamTableState) {
	if plan == nil || plan.FinalSchema == nil {
		st.schema = sourceSchema
		st.batchTransformer = nil
		st.schemaAligner = nil
		return
	}
	st.schema = destination.StagingIngestSchema(sourceSchema, plan.FinalSchema)
	st.schema = destination.PreserveSourceCDCColumnTypes(st.schema, sourceSchema)
	comparison := plan.TransformComparison
	if comparison == nil {
		comparison = plan.Comparison
	}
	mode, _ := schemaevolution.ParseContractMode(cfg.SchemaContract)
	switch mode {
	case schemaevolution.ContractDiscardValue:
		st.batchTransformer = schemaevolution.NewDiscardValueTransformer(comparison, sourceSchema, plan.FinalSchema)
	case schemaevolution.ContractDiscardRow:
		st.batchTransformer = schemaevolution.NewDiscardRowTransformer(sourceSchema, plan.FinalSchema, comparison)
	default:
		st.batchTransformer = nil
	}
	st.schemaAligner = transformer.NewSafeTypeCaster(st.schema.ToArrowSchema())
}

func configureAtomicStreamingContractState(cfg *config.IngestConfig, sourceSchema *schema.TableSchema, plan *schemaevolution.EvolutionPlan, st *streamTableState) {
	if plan == nil || plan.FinalSchema == nil {
		return
	}
	st.atomicTargetSchema = plan.FinalSchema
	mode, _ := schemaevolution.ParseContractMode(cfg.SchemaContract)
	if mode != schemaevolution.ContractDiscardValue && mode != schemaevolution.ContractDiscardRow {
		return
	}
	comparison := plan.TransformComparison
	if comparison == nil {
		comparison = plan.Comparison
	}
	st.atomicWriteSchema = st.schema
	if mode == schemaevolution.ContractDiscardValue {
		st.atomicBatchTransformer = schemaevolution.NewDiscardValueTransformer(comparison, sourceSchema, plan.FinalSchema)
	} else {
		st.atomicBatchTransformer = schemaevolution.NewDiscardRowTransformer(sourceSchema, plan.FinalSchema, comparison)
	}
}

// multiTableDestName computes the destination table name for a source table
// discovered mid-stream, matching the pipeline's upfront naming.
func multiTableDestName(dest destination.Destination, ti source.SourceTableInfo) string {
	namer, _ := dest.(destination.MultiTableNamer)
	return destination.ResolveMultiTableName(dest.GetScheme(), namer, ti.DestSchema, ti.Name)
}

// withLoadTimestampColumn mirrors the pipeline's schema decoration for tables
// discovered mid-stream: startup tables get _ingestr_loaded_at added by the
// pipeline before the executor sees them, so late arrivals must match or their
// destination tables would be created without the column the flush fills.
func withLoadTimestampColumn(s *schema.TableSchema) *schema.TableSchema {
	if s == nil {
		return nil
	}
	for _, col := range s.Columns {
		if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
			return s
		}
	}
	out := *s
	out.Columns = append(append([]schema.Column{}, s.Columns...), schema.Column{
		Name:     naming.IngestrLoadedAtColumn,
		DataType: schema.TypeTimestampTZ,
		Nullable: true,
	})
	return &out
}

// prepareTable sets up the destination (and staging, for merge) tables for one
// stream table and records the staging name on the state.
func (e *StreamingExecutor) prepareTable(ctx context.Context, dest destination.Destination, cfg *config.IngestConfig, st *streamTableState) error {
	switch e.opts.Strategy {
	case config.StrategyMerge:
		if st.isCDC && len(st.primaryKeys) == 0 {
			idempotent, ok := dest.(destination.IdempotentCommitTokenWriter)
			if !ok || !idempotent.SupportsIdempotentCommitTokenWrites() {
				return fmt.Errorf(
					"streaming merge for keyless CDC table %s requires destination support for idempotent commit-token writes",
					st.destTable,
				)
			}
			// Keyless CDC table: no key to merge on, so its change log lands
			// directly in the destination table and flush skips the merge
			// (stagingTable stays empty). See isAppendOnlyCDCTable in merge.go.
			return prepareAppendOnlyCDCTable(ctx, dest, st.destTable, st.schema)
		}
		if _, ok := dest.(destination.DirectMergeWriter); ok {
			st.directMerge = true
			return prepareMergeTarget(ctx, dest, mergeTableParams{
				DestTable:   st.destTable,
				Schema:      st.schema,
				PrimaryKeys: st.primaryKeys,
				PartitionBy: st.partitionBy,
				ClusterBy:   st.clusterBy,
				IsCDC:       st.isCDC,
			})
		}
		st.stagingTable = managedStagingTableName(dest, st.destTable, "stream", cfg.StagingDataset)
		output.Statusf("[STREAM] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), st.stagingTable)
		return prepareMergeTables(ctx, dest, mergeTableParams{
			DestTable:    st.destTable,
			StagingTable: st.stagingTable,
			Schema:       st.schema,
			PrimaryKeys:  st.primaryKeys,
			PartitionBy:  st.partitionBy,
			ClusterBy:    st.clusterBy,
			IsCDC:        st.isCDC,
		})
	case config.StrategyAppend:
		// Append must not enforce the primary key as a unique constraint:
		// streaming is at-least-once, so the same key (e.g. a broker msg_id)
		// can be redelivered, and an enforced PK would turn that into a
		// duplicate-key error that aborts the stream. The key stays a regular
		// column (available for downstream dedup or a later merge).
		//
		// CDC change batches carry the otherwise staging-only
		// _cdc_unchanged_cols column; append lands batches directly, so the
		// destination table must keep it.
		prepSchema := destination.DestinationTableSchema(st.schema)
		if st.isCDC {
			prepSchema = st.schema
		}
		if _, atomicSnapshots := dest.(destination.AtomicSnapshotPublisher); atomicSnapshots && st.isCDC {
			existingSchema, err := dest.GetTableSchema(ctx, st.destTable)
			if err != nil {
				return fmt.Errorf("failed to inspect destination table %s before atomic append streaming: %w", st.destTable, err)
			}
			if existingSchema != nil {
				return nil
			}
			st.targetOwnershipToken = newTargetOwnershipToken(dest, false)
			if st.targetOwnershipToken == "" {
				return fmt.Errorf("destination %s cannot safely clean up a failed atomic append target", dest.GetScheme())
			}
			st.targetCreatedByRun = true
			prepSchema = st.schema
			if st.atomicTargetSchema != nil {
				prepSchema = st.atomicTargetSchema
			}
		}
		if err := dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:          st.destTable,
			Schema:         prepSchema,
			DropFirst:      false,
			PrimaryKeys:    nil,
			PartitionBy:    st.partitionBy,
			ClusterBy:      st.clusterBy,
			CDCMode:        st.isCDC,
			OwnershipToken: st.targetOwnershipToken,
		}); err != nil {
			return fmt.Errorf("failed to prepare destination table %s: %w", st.destTable, err)
		}
		return nil
	default:
		return fmt.Errorf("streaming supports only merge and append strategies, got %q", e.opts.Strategy)
	}
}

// streamTableState tracks one destination table's buffered batches across
// flush cycles. stagingTable is empty in append mode.
type streamTableState struct {
	sourceTable            string
	destTable              string
	stagingTable           string
	schema                 *schema.TableSchema
	primaryKeys            []string
	incrementalKey         string
	isCDC                  bool
	partitionBy            string
	clusterBy              []string
	incarnation            string
	schemaFingerprint      string
	directMerge            bool
	atomicSnapshot         bool
	atomicTargetSchema     *schema.TableSchema
	atomicWriteSchema      *schema.TableSchema
	atomicBatchTransformer schemaevolution.BatchTransformer
	batchTransformer       schemaevolution.BatchTransformer
	schemaAligner          *transformer.TypeCaster
	snapshotAttemptID      string
	atomicPublishAttempted bool
	targetCreatedByRun     bool
	targetOwnershipToken   string
	snapshotStarted        bool
	deferredEvolution      func(context.Context) error
	evolutionApplied       bool

	pending          []arrow.RecordBatch
	pendingPositions []managedCDCDataWriteBoundary
	pendingRows      int64

	durableCommitID       any
	durableCommitPosition string
	checkpointID          any
	checkpointPosition    string
}

type flushLoop struct {
	dest   destination.Destination
	cfg    *config.IngestConfig
	opts   StreamingOptions
	tables map[string]*streamTableState // key = RecordBatchResult.TableName ("" for single-table)

	// prepareNewTable provisions state for a table announced by the source
	// after the stream started (RecordBatchResult.TableInfo). Nil means dynamic
	// tables are unsupported and announcements are ignored.
	prepareNewTable func(ctx context.Context, ti source.SourceTableInfo) (*streamTableState, error)

	// evolveTable applies destination schema evolution for a known table whose
	// source schema changed mid-stream (the source re-announces it with the
	// refreshed schema after rebuilding its stream). Nil means mid-stream
	// schema changes are unsupported and re-announcements are ignored.
	evolveTable     func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error
	evolveTablePlan func(ctx context.Context, destTable string, newSchema *schema.TableSchema) (*schemaevolution.EvolutionPlan, error)

	buffered int64 // rows buffered across all tables

	// lastToken is the newest CommitToken seen; tokenDirty marks that it has
	// not been committed yet. Tokens may be uncomparable types (maps), so a
	// dirty flag is used instead of comparing tokens.
	lastToken    any
	tokenDirty   bool
	pendingState source.CDCStateCommitToken

	lastDurableCommitID       any
	lastDurableCommitPosition string
	// pendingTruncates contains source tables whose previous snapshot marker was
	// invalidated before applying a replicated WAL TRUNCATE. The next durable
	// source position completes the new empty-table snapshot boundary.
	pendingTruncates map[string]string
	legacyFinalized  bool

	cycles int64
}

type managedCDCDataWriteToken struct {
	SourceTable string
	DataBatchID string
}

type managedCDCDataWriteBoundary struct {
	Token    managedCDCDataWriteToken
	Position string
}

func newFlushLoop(dest destination.Destination, cfg *config.IngestConfig, opts StreamingOptions, tables map[string]*streamTableState) *flushLoop {
	return &flushLoop{
		dest:             dest,
		cfg:              cfg,
		opts:             opts,
		tables:           tables,
		pendingTruncates: make(map[string]string),
	}
}

func (l *flushLoop) run(ctx context.Context, records <-chan source.RecordBatchResult) error {
	defer l.releasePending()

	ticker := time.NewTicker(l.opts.FlushInterval)
	defer ticker.Stop()

	for {
		if err := connectorLeaseLoss(ctx); err != nil {
			return l.abortLeaseLoss(records, err)
		}
		select {
		case res, ok := <-records:
			if !ok {
				// Source ended on its own (e.g. its context was cancelled and
				// it closed the channel before we observed ctx.Done).
				if ctx.Err() != nil && l.hasActiveAtomicSnapshot() {
					return ctx.Err()
				}
				return l.finalFlush(ctx)
			}
			if res.Err != nil {
				if res.Batch != nil {
					res.Batch.Release()
				}
				if ctx.Err() != nil {
					return l.shutdown(ctx, records)
				}
				return fmt.Errorf("source error: %w", res.Err)
			}
			if err := connectorLeaseLoss(ctx); err != nil {
				if res.Batch != nil {
					res.Batch.Release()
				}
				return l.abortLeaseLoss(records, err)
			}
			if err := l.processResult(ctx, res); err != nil {
				if loss := connectorLeaseLoss(ctx); loss != nil {
					return l.abortLeaseLoss(records, loss)
				}
				return err
			}
			if l.buffered >= l.opts.FlushRecords {
				if err := connectorLeaseLoss(ctx); err != nil {
					return l.abortLeaseLoss(records, err)
				}
				if err := l.flush(ctx); err != nil {
					if loss := connectorLeaseLoss(ctx); loss != nil {
						return l.abortLeaseLoss(records, loss)
					}
					return err
				}
				ticker.Reset(l.opts.FlushInterval)
			}

		case <-ticker.C:
			if err := connectorLeaseLoss(ctx); err != nil {
				return l.abortLeaseLoss(records, err)
			}
			if l.buffered == 0 && !l.tokenDirty {
				continue
			}
			if err := l.flush(ctx); err != nil {
				if loss := connectorLeaseLoss(ctx); loss != nil {
					return l.abortLeaseLoss(records, loss)
				}
				return err
			}

		case <-ctx.Done():
			return l.shutdown(ctx, records)
		}
	}
}

func (l *flushLoop) processResult(ctx context.Context, res source.RecordBatchResult) error {
	if res.SnapshotInvalidation != nil {
		if err := l.invalidateSnapshot(ctx, *res.SnapshotInvalidation); err != nil {
			if res.Batch != nil {
				res.Batch.Release()
			}
			return err
		}
	}
	if res.TableInfo != nil {
		if err := l.ensureTable(ctx, *res.TableInfo); err != nil {
			if res.Batch != nil {
				res.Batch.Release()
			}
			return err
		}
	}
	if err := l.applyDeferredEvolution(ctx, res); err != nil {
		if res.Batch != nil {
			res.Batch.Release()
		}
		return err
	}
	if err := l.handleResult(ctx, res); err != nil {
		return err
	}
	if token, ok := res.CommitToken.(source.CDCStateCommitToken); ok && len(token.SnapshotPositions) > 0 && (l.buffered > 0 || l.tokenDirty) {
		return l.flush(ctx)
	}
	return nil
}

func (l *flushLoop) invalidateSnapshot(ctx context.Context, invalidation source.CDCSnapshotInvalidation) error {
	st, ok := l.tableState(invalidation.TableName)
	if !ok {
		return fmt.Errorf("source requested snapshot invalidation for unknown table %q", invalidation.TableName)
	}
	if l.opts.StateManager == nil {
		return fmt.Errorf("source requested snapshot invalidation for %q without destination-managed CDC state", invalidation.TableName)
	}
	if l.buffered > 0 || l.tokenDirty {
		if err := l.flush(ctx); err != nil {
			return fmt.Errorf("failed to flush before snapshot invalidation for table %s: %w", invalidation.TableName, err)
		}
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if st.atomicSnapshot {
		if st.atomicPublishAttempted {
			return fmt.Errorf("cannot invalidate snapshot for table %s while atomic publication outcome is unknown", st.destTable)
		}
		if err := l.abortUnpublishedAtomicSnapshot(ctx, st); err != nil {
			return fmt.Errorf("failed to abort stale atomic snapshot for table %s: %w", st.destTable, err)
		}
	}
	if err := l.opts.StateManager.InvalidateSnapshotStatePreservingDestination(
		ctx, invalidation.TableName, st.destTable, invalidation.Incarnation, invalidation.SchemaFingerprint,
	); err != nil {
		return err
	}
	st.incarnation = invalidation.Incarnation
	return connectorLeaseLoss(ctx)
}

func (l *flushLoop) tableState(sourceTable string) (*streamTableState, bool) {
	if st, ok := l.tables[sourceTable]; ok {
		return st, true
	}
	if st, ok := l.tables[""]; ok && len(l.tables) == 1 {
		return st, true
	}
	return nil, false
}

func (l *flushLoop) handleResult(ctx context.Context, res source.RecordBatchResult) error {
	if !res.Truncate {
		return l.bufferResult(res)
	}

	if l.buffered > 0 || l.tokenDirty {
		if err := l.flush(ctx); err != nil {
			return fmt.Errorf("failed to flush before source truncate: %w", err)
		}
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	st, ok := l.tableState(res.TableName)
	if !ok {
		return fmt.Errorf("source requested truncate for unknown table %q", res.TableName)
	}
	stateTable := ""
	if l.opts.StateManager != nil {
		stateTable = l.stateSourceTable(res.TableName)
		if stateTable == "" {
			return fmt.Errorf("cannot resolve source table for CDC truncate")
		}
		if res.CDCWALTruncate {
			if err := l.opts.StateManager.InvalidateSnapshotPreservingDestination(ctx, stateTable, st.destTable, st.incarnation); err != nil {
				return fmt.Errorf("failed to invalidate CDC state before source truncate for %s: %w", stateTable, err)
			}
		}
		if !res.CDCWALTruncate {
			handled, err := l.opts.StateManager.ApplyLateSnapshotBoundary(ctx, stateTable, st.destTable)
			if err != nil {
				return err
			}
			if handled {
				return l.bufferResult(res)
			}
		}
		if !res.CDCWALTruncate {
			if err := l.opts.StateManager.BindDestinationIncarnation(ctx, stateTable, st.destTable); err != nil {
				return fmt.Errorf("failed to bind CDC destination before source truncate for %s: %w", stateTable, err)
			}
		}
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if !res.CDCWALTruncate {
		if publisher, ok := l.dest.(destination.AtomicSnapshotPublisher); ok {
			if st.snapshotStarted && st.snapshotAttemptID != "" {
				if st.atomicPublishAttempted {
					return fmt.Errorf("cannot restart atomic snapshot for table %s while publication outcome is unknown", st.destTable)
				}
				if err := l.abortUnpublishedAtomicSnapshot(ctx, st); err != nil {
					return fmt.Errorf("failed to abort previous atomic snapshot for table %s: %w", st.destTable, err)
				}
			}
			st.snapshotStarted = true
			st.snapshotAttemptID = uuid.NewString()
			st.atomicPublishAttempted = false
			expectedIncarnation := ""
			if l.opts.StateManager != nil && l.opts.StateManager.dest != nil {
				expectedIncarnation = l.opts.StateManager.BoundDestinationIncarnation(stateTable)
				if expectedIncarnation == "" {
					return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", st.destTable)
				}
			}
			if err := publisher.BeginAtomicSnapshot(ctx, destination.AtomicSnapshotOptions{
				Table: st.destTable, Schema: l.atomicSnapshotWriteSchema(st), TargetSchema: l.atomicSnapshotTargetSchema(st), PrimaryKeys: st.primaryKeys,
				PartitionBy: st.partitionBy, ClusterBy: st.clusterBy, Parallelism: l.parallelism(), AttemptID: st.snapshotAttemptID,
				CDCExpectedIncarnation: expectedIncarnation,
			}); err != nil {
				return fmt.Errorf("failed to begin atomic snapshot for table %s: %w", st.destTable, err)
			}
			st.atomicSnapshot = true
			return l.bufferResult(res)
		}
	}
	var truncateErr error
	if res.CDCWALTruncate {
		if l.opts.StateManager != nil {
			truncateErr = destination.ApplyCDCTruncateIfIncarnation(
				ctx,
				l.dest,
				st.destTable,
				l.opts.StateManager.BoundDestinationIncarnation(stateTable),
			)
		} else {
			truncateErr = destination.ApplyCDCTruncate(ctx, l.dest, st.destTable)
		}
	} else {
		truncateErr = destination.ApplyTruncate(ctx, l.dest, st.destTable)
	}
	if truncateErr != nil {
		return fmt.Errorf("failed to apply source truncate to %s: %w", st.destTable, truncateErr)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if stateTable != "" && res.CDCWALTruncate {
		l.pendingTruncates[stateTable] = st.incarnation
	}
	return l.bufferResult(res)
}

func (l *flushLoop) stateSourceTable(recordTable string) string {
	if recordTable != "" {
		return recordTable
	}
	if l.cfg != nil && l.cfg.SourceTable != "" {
		return l.cfg.SourceTable
	}
	if len(l.tables) == 1 {
		for table, st := range l.tables {
			if table == "" && st.sourceTable != "" {
				return st.sourceTable
			}
			return table
		}
	}
	return ""
}

func (l *flushLoop) abortUnpublishedAtomicSnapshot(ctx context.Context, st *streamTableState) error {
	aborter, ok := l.dest.(destination.AtomicSnapshotAborter)
	if !ok {
		return fmt.Errorf("destination %s cannot abort owned atomic snapshot attempts", l.dest.GetScheme())
	}
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamFinalFlushTimeout)
	defer cancel()
	if err := aborter.AbortAtomicSnapshot(abortCtx, destination.AtomicSnapshotOptions{
		Table: st.destTable, AttemptID: st.snapshotAttemptID,
	}); err != nil {
		return err
	}
	st.atomicSnapshot = false
	st.snapshotStarted = false
	st.snapshotAttemptID = ""
	return nil
}

func (l *flushLoop) applyDeferredEvolution(ctx context.Context, res source.RecordBatchResult) error {
	if res.Batch == nil {
		return nil
	}
	st, ok := l.tableState(res.TableName)
	if !ok || st.deferredEvolution == nil || st.evolutionApplied || st.snapshotStarted {
		return nil
	}
	if err := st.deferredEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply deferred schema evolution for table %s: %w", st.destTable, err)
	}
	st.evolutionApplied = true
	return nil
}

// ensureTable provisions per-table state for a table announced mid-stream.
// An announcement for a table already known is a schema refresh: the source
// rebuilt its stream around new DDL and re-announces the table so the
// destination can evolve before the new-shape batches arrive. Announcements
// that carry no schema change are ignored.
func (l *flushLoop) ensureTable(ctx context.Context, ti source.SourceTableInfo) error {
	if st, ok := l.tableState(ti.Name); ok {
		return l.refreshTableSchema(ctx, ti, st)
	}
	if l.prepareNewTable == nil {
		config.Debug("[STREAM] Ignoring new-table announcement for %q (dynamic tables unsupported here)", ti.Name)
		return nil
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	st, err := l.prepareNewTable(ctx, ti)
	if err != nil {
		return fmt.Errorf("failed to prepare newly discovered table %s: %w", ti.Name, err)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	l.tables[ti.Name] = st
	output.Statusf("[STREAM] %s | New table %s added to stream (dest: %s)\n", time.Now().Format("15:04:05"), ti.Name, st.destTable)
	return nil
}

// refreshTableSchema handles a re-announcement of a known table after a
// mid-stream schema change: batches buffered in the old shape are flushed
// first, then the destination table is evolved and the staging table
// recreated so the new-shape batches following the announcement land cleanly.
func (l *flushLoop) refreshTableSchema(ctx context.Context, ti source.SourceTableInfo, st *streamTableState) error {
	if l.opts.StateManager != nil {
		if err := l.opts.StateManager.RegisterTableState(ctx, ti.Name, st.destTable, ti.Incarnation, ti.SchemaFingerprint); err != nil {
			return fmt.Errorf("failed to register CDC state for table %s: %w", ti.Name, err)
		}
	}
	st.incarnation = ti.Incarnation
	st.schemaFingerprint = ti.SchemaFingerprint
	if ti.Schema == nil {
		return nil
	}
	if err := schemainfer.ValidateSchema(ti.Schema); err != nil {
		return fmt.Errorf("invalid refreshed schema for source table %s: %w", ti.Name, err)
	}
	if l.evolveTable == nil && l.evolveTablePlan == nil {
		return nil
	}
	newSchema := ti.Schema
	if !l.cfg.NoLoadTimestamp {
		newSchema = withLoadTimestampColumn(newSchema)
	}
	keysChanged := !equalFoldedStrings(st.primaryKeys, ti.PrimaryKeys)
	if keysChanged {
		return fmt.Errorf(
			"streaming primary-key change for table %s from %v to %v requires a new snapshot; stop the stream and run a full refresh",
			st.destTable, st.primaryKeys, ti.PrimaryKeys,
		)
	}
	if st.schema.SameColumnShape(newSchema) && !keysChanged {
		return nil
	}

	if err := l.flush(ctx); err != nil {
		return fmt.Errorf("failed to flush before schema change for table %s: %w", ti.Name, err)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if publisher, ok := l.dest.(destination.AtomicSnapshotPublisher); ok && st.atomicSnapshot {
		contractMode, err := schemaevolution.ParseContractMode(l.cfg.SchemaContract)
		if err != nil {
			return fmt.Errorf("failed to parse schema contract for atomic snapshot refresh: %w", err)
		}
		if contractMode == schemaevolution.ContractDiscardRow {
			return fmt.Errorf("atomic snapshot schema refresh for table %s is unsupported with discard_row because refreshed rows cannot be published without contract filtering", st.destTable)
		}
		plan, err := planDestinationSchemaEvolution(ctx, l.dest, st.destTable, newSchema, l.cfg)
		if err != nil {
			return fmt.Errorf("failed to validate atomic snapshot schema change for table %s: %w", st.destTable, err)
		}
		configureStreamingContractState(l.cfg, newSchema, plan, st)
		st.atomicTargetSchema = plan.FinalSchema
		st.atomicWriteSchema = nil
		st.atomicBatchTransformer = nil
		if contractMode == schemaevolution.ContractDiscardValue {
			st.atomicWriteSchema = st.schema
			st.atomicBatchTransformer = schemaevolution.NewDiscardValueTransformer(plan.TransformComparison, newSchema, plan.FinalSchema)
		}
		st.schemaAligner = transformer.NewSafeTypeCaster(l.atomicSnapshotWriteSchema(st).ToArrowSchema())
		st.isCDC = hasCDCColumns(newSchema)
		st.primaryKeys = append([]string(nil), ti.PrimaryKeys...)
		_, supportsDirectMerge := l.dest.(destination.DirectMergeWriter)
		st.directMerge = len(st.primaryKeys) > 0 && supportsDirectMerge
		if err := publisher.EvolveAtomicSnapshot(ctx, destination.AtomicSnapshotOptions{
			Table: st.destTable, Schema: l.atomicSnapshotWriteSchema(st), TargetSchema: l.atomicSnapshotTargetSchema(st), PrimaryKeys: st.primaryKeys,
			PartitionBy: st.partitionBy, ClusterBy: st.clusterBy, Parallelism: l.parallelism(), AttemptID: st.snapshotAttemptID,
		}); err != nil {
			return fmt.Errorf("failed to evolve atomic snapshot staging for table %s after schema change: %w", st.destTable, err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		output.Statusf("[STREAM] %s | Snapshot schema change staged for %s (dest: %s)\n", time.Now().Format("15:04:05"), ti.Name, st.destTable)
		return nil
	}
	var plan *schemaevolution.EvolutionPlan
	if l.evolveTablePlan != nil {
		var err error
		plan, err = l.evolveTablePlan(ctx, st.destTable, newSchema)
		if err != nil {
			return fmt.Errorf("failed to evolve destination table %s: %w", st.destTable, err)
		}
	} else if err := l.evolveTable(ctx, st.destTable, newSchema); err != nil {
		return fmt.Errorf("failed to evolve destination table %s: %w", st.destTable, err)
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}

	configureStreamingContractState(l.cfg, newSchema, plan, st)
	st.isCDC = hasCDCColumns(newSchema)
	st.primaryKeys = append([]string(nil), ti.PrimaryKeys...)
	if st.stagingTable != "" {
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := l.dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:        st.stagingTable,
			Schema:       st.schema,
			DropFirst:    true,
			PrimaryKeys:  nil,
			CDCMode:      st.isCDC,
			PartitionBy:  st.partitionBy,
			ClusterBy:    st.clusterBy,
			ExpiresAfter: destination.ManagedStagingTTL,
		}); err != nil {
			return fmt.Errorf("failed to recreate staging table %s after schema change: %w", st.stagingTable, err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	output.Statusf("[STREAM] %s | Schema change applied for %s (dest: %s)\n", time.Now().Format("15:04:05"), ti.Name, st.destTable)
	return nil
}

func equalFoldedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !strings.EqualFold(left[i], right[i]) {
			return false
		}
	}
	return true
}

func (l *flushLoop) buffer(res source.RecordBatchResult) {
	if err := l.bufferResult(res); err != nil {
		panic(err)
	}
}

func (l *flushLoop) bufferResult(res source.RecordBatchResult) error {
	var st *streamTableState
	if res.Batch != nil && res.Batch.NumRows() > 0 {
		var ok bool
		st, ok = l.tableState(res.TableName)
		if !ok {
			rows := res.Batch.NumRows()
			res.Batch.Release()
			return fmt.Errorf("streaming received %d row(s) for unknown table %q", rows, res.TableName)
		}
		if res.TableName != "" {
			st.sourceTable = res.TableName
		}
	}
	if res.CommitToken != nil {
		l.lastToken = res.CommitToken
		l.tokenDirty = true
		if token, ok := res.CommitToken.(source.CDCStateCommitToken); ok {
			if token.Position != "" {
				l.pendingState.Position = token.Position
			}
			if len(token.SnapshotPositions) > 0 {
				if l.pendingState.SnapshotPositions == nil {
					l.pendingState.SnapshotPositions = make(map[string]string)
				}
				if len(token.SnapshotIncarnations) > 0 {
					if l.pendingState.SnapshotIncarnations == nil {
						l.pendingState.SnapshotIncarnations = make(map[string]string)
					}
					for table, incarnation := range token.SnapshotIncarnations {
						l.pendingState.SnapshotIncarnations[table] = incarnation
					}
				}
				if len(token.SnapshotSchemas) > 0 {
					if l.pendingState.SnapshotSchemas == nil {
						l.pendingState.SnapshotSchemas = make(map[string]string)
					}
					for table, fingerprint := range token.SnapshotSchemas {
						l.pendingState.SnapshotSchemas[table] = fingerprint
					}
				}
				for table, position := range token.SnapshotPositions {
					l.pendingState.SnapshotPositions[table] = position
				}
			}
			if token.Position != "" && len(l.pendingTruncates) > 0 {
				if l.pendingState.SnapshotPositions == nil {
					l.pendingState.SnapshotPositions = make(map[string]string)
				}
				if l.pendingState.SnapshotIncarnations == nil {
					l.pendingState.SnapshotIncarnations = make(map[string]string)
				}
				for table, incarnation := range l.pendingTruncates {
					l.pendingState.SnapshotPositions[table] = token.Position
					l.pendingState.SnapshotIncarnations[table] = incarnation
					delete(l.pendingTruncates, table)
				}
			}
		}
	}
	if l.opts.StateManager == nil && res.DurableCheckpointID != nil {
		if res.DurableCheckpointTable == "" {
			l.lastDurableCommitID = res.DurableCheckpointID
			l.lastDurableCommitPosition = res.DurableCheckpointPosition
		} else if checkpointState, ok := l.tableState(res.DurableCheckpointTable); ok {
			checkpointState.checkpointID = res.DurableCheckpointID
			checkpointState.checkpointPosition = res.DurableCheckpointPosition
		} else {
			config.Debug("[STREAM] Ignoring checkpoint for unknown table %q", res.DurableCheckpointTable)
		}
	}
	if res.Batch != nil && res.Batch.NumRows() == 0 {
		res.Batch.Release()
		res.Batch = nil
	}
	if res.Batch == nil {
		if l.opts.StateManager == nil && res.DurableCheckpointID == nil && res.DurableCommitID != nil {
			if checkpointState, ok := l.tableState(res.TableName); ok && res.TableName != "" {
				checkpointState.checkpointID = res.DurableCommitID
				checkpointState.checkpointPosition = res.DurableCommitPosition
			} else {
				l.lastDurableCommitID = res.DurableCommitID
				l.lastDurableCommitPosition = res.DurableCommitPosition
			}
		}
		return nil
	}
	if l.opts.StateManager == nil && res.DurableCommitID != nil {
		st.durableCommitID = res.DurableCommitID
		st.durableCommitPosition = res.DurableCommitPosition
	}
	if l.managedCDCNeedsDataWriteIdentity(st) {
		token, ok := res.CommitToken.(source.CDCStateCommitToken)
		if !ok || token.Position == "" || token.DataBatchID == "" {
			res.Batch.Release()
			return fmt.Errorf("managed CDC data batch for table %s has no typed source position and stable data batch identity", res.TableName)
		}
		st.pendingPositions = append(st.pendingPositions, managedCDCDataWriteBoundary{
			Token: managedCDCDataWriteToken{
				SourceTable: l.stateSourceTable(res.TableName),
				DataBatchID: token.DataBatchID,
			},
			Position: token.Position,
		})
	}
	st.pending = append(st.pending, res.Batch)
	st.pendingRows += res.Batch.NumRows()
	l.buffered += res.Batch.NumRows()
	return nil
}

// flush writes all buffered batches to the destination (one write+merge cycle
// per table) and then confirms the durable position with the source. The
// ordering write → merge → reset staging → commit is what makes delivery
// at-least-once: a crash before commit re-delivers from the last committed
// position and the merge is idempotent by primary key.
//
// Tables flush concurrently when the destination declares its cross-table
// cycles independent (destination.ConcurrentFlusher); the position is
// confirmed only after every table's cycle succeeded, so a partial failure
// re-delivers all tables from the last committed position.
func (l *flushLoop) flush(ctx context.Context) error {
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	start := time.Now()
	loadTimestamp := start.UTC().Truncate(time.Microsecond)
	if l.opts.StateManager != nil {
		for sourceTable := range l.pendingState.SnapshotPositions {
			st, ok := l.tableState(sourceTable)
			if !ok {
				return fmt.Errorf("CDC snapshot state references unknown table %q", sourceTable)
			}
			if st.atomicSnapshot {
				continue
			}
			if err := l.opts.StateManager.BindDestinationIncarnation(ctx, sourceTable, st.destTable); err != nil {
				return fmt.Errorf("streaming flush: failed to bind CDC destination for %s: %w", sourceTable, err)
			}
		}
	}
	type flushWork struct {
		name string
		st   *streamTableState

		batches   []arrow.RecordBatch
		positions []managedCDCDataWriteBoundary
		rows      int64
	}

	var work []flushWork
	for name, st := range l.tables {
		if st.pendingRows == 0 {
			continue
		}
		work = append(work, flushWork{name: name, st: st, batches: st.pending, positions: st.pendingPositions, rows: st.pendingRows})
		l.buffered -= st.pendingRows
		st.pending = nil
		st.pendingPositions = nil
		st.pendingRows = 0
	}
	releaseWork := func() {
		for _, pending := range work {
			for _, batch := range pending.batches {
				batch.Release()
			}
		}
	}
	keylessSnapshots := make(map[string]*streamTableState)
	if l.opts.StateManager != nil && l.pendingState.Position != "" {
		for _, w := range work {
			if !w.st.isCDC || len(w.st.primaryKeys) > 0 || w.st.stagingTable != "" {
				continue
			}
			sourceTable := l.stateSourceTable(w.name)
			if _, freshSnapshot := l.pendingState.SnapshotPositions[sourceTable]; freshSnapshot {
				continue
			}
			if err := l.opts.StateManager.InvalidateSnapshotPreservingDestination(ctx, sourceTable, w.st.destTable, w.st.incarnation); err != nil {
				releaseWork()
				return fmt.Errorf("streaming flush: failed to invalidate keyless CDC state for %s: %w", sourceTable, err)
			}
			keylessSnapshots[sourceTable] = w.st
		}
	}

	flushOne := func(ctx context.Context, w flushWork) error {
		st := w.st
		displayName := w.name
		if displayName == "" {
			displayName = st.destTable
		}
		if l.managedCDCNeedsDataWriteIdentity(st) {
			if len(w.positions) != len(w.batches) {
				for _, batch := range w.batches {
					batch.Release()
				}
				return fmt.Errorf("streaming flush: managed CDC table %s lost source write boundaries", displayName)
			}
			sourceTable := l.stateSourceTable(w.name)
			if sourceTable == "" {
				for _, batch := range w.batches {
					batch.Release()
				}
				return fmt.Errorf("streaming flush: managed CDC table %s has no source table identity", displayName)
			}
			expectedIncarnation := l.opts.StateManager.BoundDestinationIncarnation(sourceTable)
			if expectedIncarnation == "" {
				for _, batch := range w.batches {
					batch.Release()
				}
				return fmt.Errorf("streaming flush: managed CDC table %s has no bound destination incarnation", displayName)
			}
			for i, batch := range w.batches {
				boundary := w.positions[i]
				writeToken := boundary.Token
				if writeToken.SourceTable != sourceTable {
					for _, unstarted := range w.batches[i:] {
						unstarted.Release()
					}
					return fmt.Errorf("streaming flush: managed CDC table %s has inconsistent source write identity", displayName)
				}
				writeOpts := destination.WriteOptions{
					Table: st.destTable, Schema: st.schema, Parallelism: l.parallelism(), CDCExpectedIncarnation: expectedIncarnation,
					StagingBucket: l.cfg.StagingBucket, LoaderFileSize: l.cfg.LoaderFileSize, LoaderFileFormat: l.cfg.LoaderFileFormat,
					CommitToken: writeToken, SkipCDCResume: true,
				}
				records := (<-chan source.RecordBatchResult)(prefilledBatchChannel([]arrow.RecordBatch{batch}))
				if col, ok := loadTimestampColumn(st.schema); ok {
					records = transformer.Wrap(records, transformer.NewLoadTimestamp(col, loadTimestamp))
				}
				if st.batchTransformer != nil {
					records = schemaevolution.TransformBatchStream(ctx, records, st.batchTransformer)
				}
				if st.schemaAligner != nil {
					records = transformer.Wrap(records, st.schemaAligner)
				}
				if err := l.dest.WriteParallel(ctx, records, writeOpts); err != nil {
					drainAndRelease(records)
					for _, unstarted := range w.batches[i+1:] {
						unstarted.Release()
					}
					return fmt.Errorf("streaming flush: failed to write managed CDC boundary for table %s at %s: %w", displayName, boundary.Position, err)
				}
				if err := connectorLeaseLoss(ctx); err != nil {
					for _, unstarted := range w.batches[i+1:] {
						unstarted.Release()
					}
					return err
				}
			}
			return nil
		}

		managedCDC := l.opts.StateManager != nil
		commitToken := st.durableCommitID
		cdcResumeLSN := st.durableCommitPosition
		skipCDCResume := st.durableCommitID != nil && st.durableCommitPosition == ""
		if managedCDC {
			cdcResumeLSN = ""
			skipCDCResume = true
		}

		writeOpts := destination.WriteOptions{
			Table:            st.destTable,
			Schema:           st.schema,
			Parallelism:      l.parallelism(),
			StagingBucket:    l.cfg.StagingBucket,
			LoaderFileSize:   l.cfg.LoaderFileSize,
			LoaderFileFormat: l.cfg.LoaderFileFormat,
			CommitToken:      commitToken,
			CDCResumeLSN:     cdcResumeLSN,
			SkipCDCResume:    skipCDCResume,
		}
		expectedIncarnation := ""
		if managedCDC && l.opts.StateManager.dest != nil {
			expectedIncarnation = l.opts.StateManager.BoundDestinationIncarnation(l.stateSourceTable(w.name))
			if expectedIncarnation == "" {
				for _, batch := range w.batches {
					batch.Release()
				}
				return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", st.destTable)
			}
		}
		if st.atomicSnapshot {
			writeOpts.Schema = l.atomicSnapshotWriteSchema(st)
		}
		if st.stagingTable != "" && !st.atomicSnapshot {
			writeOpts.Table = st.stagingTable
			writeOpts.StagingTable = true
		} else if !st.atomicSnapshot {
			writeOpts.CDCExpectedIncarnation = expectedIncarnation
		}

		if err := connectorLeaseLoss(ctx); err != nil {
			for _, batch := range w.batches {
				batch.Release()
			}
			return err
		}
		records := (<-chan source.RecordBatchResult)(prefilledBatchChannel(w.batches))
		if col, ok := loadTimestampColumn(st.schema); ok {
			records = transformer.Wrap(records, transformer.NewLoadTimestamp(col, loadTimestamp))
		}
		if !st.atomicSnapshot && st.batchTransformer != nil {
			records = schemaevolution.TransformBatchStream(ctx, records, st.batchTransformer)
		}
		if !st.atomicSnapshot && st.schemaAligner != nil {
			records = transformer.Wrap(records, st.schemaAligner)
		}
		if st.atomicSnapshot && st.atomicBatchTransformer != nil {
			records = schemaevolution.TransformBatchStream(ctx, records, st.atomicBatchTransformer)
		}
		if st.atomicSnapshot && st.schemaAligner != nil {
			records = transformer.Wrap(records, st.schemaAligner)
		}
		if st.atomicSnapshot {
			publisher := l.dest.(destination.AtomicSnapshotPublisher)
			writeOpts.AtomicSnapshotAttemptID = st.snapshotAttemptID
			if err := publisher.WriteAtomicSnapshot(ctx, records, writeOpts); err != nil {
				drainAndRelease(records)
				return fmt.Errorf("streaming flush: failed to stage %d snapshot rows for table %s: %w", w.rows, displayName, err)
			}
		} else if st.directMerge {
			direct := l.dest.(destination.DirectMergeWriter)
			if err := direct.MergeRecords(ctx, records, writeOpts, destination.MergeOptions{
				TargetTable:            st.destTable,
				PrimaryKeys:            st.primaryKeys,
				Columns:                destination.MergeColumnsFor(l.dest, st.schema.ColumnNames()),
				IncrementalKey:         mergeIncrementalKeyForSchema(st.schema, st.incrementalKey),
				Schema:                 st.schema,
				Parallelism:            writeOpts.Parallelism,
				CommitToken:            commitToken,
				CDCResumeLSN:           cdcResumeLSN,
				SkipCDCResume:          skipCDCResume,
				CDCExpectedIncarnation: expectedIncarnation,
			}); err != nil {
				drainAndRelease(records)
				return fmt.Errorf("streaming flush: failed to merge %d rows directly for table %s: %w", w.rows, displayName, err)
			}
		} else if err := l.dest.WriteParallel(ctx, records, writeOpts); err != nil {
			drainAndRelease(records)
			return fmt.Errorf("streaming flush: failed to write %d rows for table %s: %w", w.rows, displayName, err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}

		if st.stagingTable != "" && !st.atomicSnapshot {
			if err := connectorLeaseLoss(ctx); err != nil {
				return err
			}
			if err := mergeStagingIntoWithCommit(ctx, l.dest, st.stagingTable, st.destTable, st.primaryKeys, st.schema, st.incrementalKey,
				commitToken, cdcResumeLSN, skipCDCResume, expectedIncarnation); err != nil {
				return fmt.Errorf("streaming flush: failed to merge table %s: %w", displayName, err)
			}
			if err := connectorLeaseLoss(ctx); err != nil {
				return err
			}
			if err := l.resetStaging(ctx, st); err != nil {
				return fmt.Errorf("streaming flush: failed to reset staging table %s: %w", st.stagingTable, err)
			}
			if err := connectorLeaseLoss(ctx); err != nil {
				return err
			}
		}
		return nil
	}

	var flushErr error
	if limit := l.flushConcurrency(); limit > 1 && len(work) > 1 {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(limit)
		for _, w := range work {
			g.Go(func() error { return flushOne(gctx, w) })
		}
		flushErr = g.Wait()
	} else {
		for i, w := range work {
			if err := flushOne(ctx, w); err != nil {
				flushErr = err
				for _, pending := range work[i+1:] {
					for _, batch := range pending.batches {
						batch.Release()
					}
				}
				break
			}
		}
	}
	if flushErr != nil {
		return flushErr
	}
	for sourceTable, st := range keylessSnapshots {
		if l.pendingState.SnapshotPositions == nil {
			l.pendingState.SnapshotPositions = make(map[string]string)
		}
		if l.pendingState.SnapshotIncarnations == nil {
			l.pendingState.SnapshotIncarnations = make(map[string]string)
		}
		if l.pendingState.SnapshotSchemas == nil {
			l.pendingState.SnapshotSchemas = make(map[string]string)
		}
		l.pendingState.SnapshotPositions[sourceTable] = l.pendingState.Position
		l.pendingState.SnapshotIncarnations[sourceTable] = st.incarnation
		l.pendingState.SnapshotSchemas[sourceTable] = st.schemaFingerprint
	}
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}

	for tableName, st := range l.tables {
		if !st.atomicSnapshot {
			continue
		}
		sourceTable := l.stateSourceTable(tableName)
		position, complete := l.pendingState.SnapshotPositions[sourceTable]
		if !complete {
			continue
		}
		publisher := l.dest.(destination.AtomicSnapshotPublisher)
		st.atomicPublishAttempted = true
		expectedIncarnation := ""
		if l.opts.StateManager != nil && l.opts.StateManager.dest != nil {
			expectedIncarnation = l.opts.StateManager.BoundDestinationIncarnation(sourceTable)
			if expectedIncarnation == "" {
				return fmt.Errorf("managed CDC destination %s has no bound physical incarnation", st.destTable)
			}
		}
		if err := publisher.PublishAtomicSnapshot(ctx, destination.AtomicSnapshotOptions{
			Table: st.destTable, Schema: st.schema, TargetSchema: l.atomicSnapshotTargetSchema(st), PrimaryKeys: st.primaryKeys,
			PartitionBy: st.partitionBy, ClusterBy: st.clusterBy, Parallelism: l.parallelism(),
			CommitToken: position, CDCResumeLSN: position, SkipCDCResume: l.opts.StateManager != nil, AttemptID: st.snapshotAttemptID,
			CDCExpectedIncarnation: expectedIncarnation,
		}); err != nil {
			return fmt.Errorf("streaming flush: failed to publish snapshot for table %s: %w", st.destTable, err)
		}
		st.atomicSnapshot = false
		st.snapshotStarted = false
		st.snapshotAttemptID = ""
		st.atomicPublishAttempted = false
		st.targetCreatedByRun = false
		if l.opts.StateManager != nil {
			if err := l.opts.StateManager.BindDestinationIncarnation(ctx, sourceTable, st.destTable); err != nil {
				return fmt.Errorf("streaming flush: failed to bind published CDC destination for %s: %w", sourceTable, err)
			}
		}
	}

	ackBlocked := l.hasActiveAtomicSnapshot()
	if l.opts.StateManager == nil && !ackBlocked {
		if tokenWriter, ok := l.dest.(destination.DurableCommitTokenWriter); ok {
			workedTables := make(map[*streamTableState]struct{}, len(work))
			for _, w := range work {
				workedTables[w.st] = struct{}{}
			}
			for _, st := range l.tables {
				if st.atomicSnapshot {
					continue
				}
				if _, wroteRows := workedTables[st]; wroteRows {
					continue
				}
				checkpointID := st.checkpointID
				checkpointPosition := st.checkpointPosition
				if checkpointID == nil {
					checkpointID = l.lastDurableCommitID
					checkpointPosition = l.lastDurableCommitPosition
				}
				if checkpointID == nil {
					continue
				}
				cdcResumeLSN := ""
				if st.isCDC {
					cdcResumeLSN = checkpointPosition
				}
				if err := tokenWriter.CommitWriteToken(ctx, st.destTable, checkpointID, cdcResumeLSN); err != nil {
					return fmt.Errorf("streaming flush: failed to persist source position for table %s: %w", st.destTable, err)
				}
			}
		}
	}

	var flushedRows int64
	for _, w := range work {
		flushedRows += w.rows
	}

	if l.opts.StateManager != nil && l.tokenDirty && !ackBlocked {
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := l.opts.StateManager.Persist(ctx, l.pendingState); err != nil {
			return fmt.Errorf("streaming flush: failed to persist destination CDC state: %w", err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	if l.opts.Committer != nil && l.tokenDirty && !ackBlocked {
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := l.opts.Committer.CommitStream(ctx, l.lastToken); err != nil {
			return fmt.Errorf("streaming flush: failed to commit source position: %w", err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	if l.opts.StateManager != nil && l.opts.LegacyFinalizer != nil && l.tokenDirty && !ackBlocked && !l.legacyFinalized {
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := l.opts.LegacyFinalizer.FinalizeLegacySlot(ctx); err != nil {
			return fmt.Errorf("streaming flush: failed to finalize legacy PostgreSQL CDC slot: %w", err)
		}
		if err := connectorLeaseLoss(ctx); err != nil {
			return err
		}
		l.legacyFinalized = true
	}
	if !ackBlocked {
		l.tokenDirty = false
		l.pendingState = source.CDCStateCommitToken{}
		l.lastDurableCommitID = nil
		l.lastDurableCommitPosition = ""
		for _, st := range l.tables {
			st.durableCommitID = nil
			st.durableCommitPosition = ""
			st.checkpointID = nil
			st.checkpointPosition = ""
		}
	}

	// Only after the commit: the counters mean "durable in the destination and
	// acknowledged to the source", not merely "written".
	perTable := make(map[string]int64, len(work))
	for _, w := range work {
		name := w.name
		if name == "" {
			name = w.st.destTable // single-table streams key l.tables on ""
		}
		perTable[name] = w.rows
	}
	metrics.RecordSync(perTable, time.Now())

	l.cycles++
	if flushedRows > 0 {
		output.Statusf("[STREAM] %s | cycle %d: flushed %d rows in %s\n", time.Now().Format("15:04:05"), l.cycles, flushedRows, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func (l *flushLoop) managedCDCNeedsDataWriteIdentity(st *streamTableState) bool {
	if l.opts.StateManager == nil || st == nil || !st.isCDC || st.atomicSnapshot {
		return false
	}
	return l.opts.Strategy == config.StrategyAppend || len(st.primaryKeys) == 0
}

func (l *flushLoop) atomicSnapshotTargetSchema(st *streamTableState) *schema.TableSchema {
	if st.atomicTargetSchema != nil {
		return st.atomicTargetSchema
	}
	if l.opts.Strategy == config.StrategyAppend && st.isCDC {
		return st.schema
	}
	return destination.DestinationTableSchema(st.schema)
}

func (l *flushLoop) atomicSnapshotWriteSchema(st *streamTableState) *schema.TableSchema {
	if st.atomicWriteSchema != nil {
		return st.atomicWriteSchema
	}
	return st.schema
}

func (l *flushLoop) hasActiveAtomicSnapshot() bool {
	for _, st := range l.tables {
		if st.atomicSnapshot {
			return true
		}
	}
	return false
}

// flushConcurrency returns how many tables may flush at once: destinations
// opt in via destination.ConcurrentFlusher; everything else stays sequential.
func (l *flushLoop) flushConcurrency() int {
	cf, ok := l.dest.(destination.ConcurrentFlusher)
	if !ok {
		return 1
	}
	if n := cf.MaxConcurrentFlushes(); n > 1 {
		return n
	}
	return 1
}

// resetStaging empties the staging table for the next cycle, preferring an
// in-place truncate over drop+recreate.
func (l *flushLoop) resetStaging(ctx context.Context, st *streamTableState) error {
	if tc, ok := l.dest.(destination.TruncateCapable); ok {
		return tc.TruncateTable(ctx, st.stagingTable)
	}
	return l.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:        st.stagingTable,
		Schema:       st.schema,
		DropFirst:    true,
		PrimaryKeys:  nil,
		CDCMode:      st.isCDC,
		PartitionBy:  st.partitionBy,
		ClusterBy:    st.clusterBy,
		ExpiresAfter: destination.ManagedStagingTTL,
	})
}

// shutdown handles context cancellation: it drains whatever the source still
// flushes into the channel, then performs a final flush on a detached context
// so the buffered tail reaches the destination.
func (l *flushLoop) shutdown(ctx context.Context, records <-chan source.RecordBatchResult) error {
	if err := connectorLeaseLoss(ctx); err != nil {
		return l.abortLeaseLoss(records, err)
	}
	// A partially staged atomic snapshot must never be flushed during shutdown.
	// Its hidden attempt is safe to reclaim on restart; returning promptly also
	// prevents a canceled source snapshot from being drained into durable pages.
	if l.hasActiveAtomicSnapshot() {
		startBoundedRecordDrain(records, streamDrainTimeout)
		return ctx.Err()
	}
	deadline := time.NewTimer(streamDrainTimeout)
	defer deadline.Stop()
	drainCtx, cancel := detachedLeaseContext(ctx, streamFinalFlushTimeout)
	defer cancel()

drain:
	for {
		select {
		case res, ok := <-records:
			if !ok {
				break drain
			}
			if res.Err != nil {
				if loss := connectorLeaseLoss(drainCtx); loss != nil {
					if res.Batch != nil {
						res.Batch.Release()
					}
					return l.abortLeaseLoss(records, loss)
				}
				if res.Batch != nil {
					res.Batch.Release()
				}
				// Cancellation errors from the source are expected here.
				config.Debug("[STREAM] Ignoring source error during shutdown: %v", res.Err)
				continue
			}
			if err := l.processResult(drainCtx, res); err != nil {
				if loss := connectorLeaseLoss(drainCtx); loss != nil {
					return l.abortLeaseLoss(records, loss)
				}
				return err
			}
		case <-drainCtx.Done():
			if loss := connectorLeaseLoss(drainCtx); loss != nil {
				return l.abortLeaseLoss(records, loss)
			}
			break drain
		case <-deadline.C:
			config.Debug("[STREAM] Drain deadline reached, proceeding to final flush")
			break drain
		}
	}
	return l.finalFlush(ctx)
}

// finalFlush flushes any remaining buffered data. It detaches from the
// (possibly cancelled) run context while preserving its values, so destination
// writes still carry query annotations.
func (l *flushLoop) finalFlush(ctx context.Context) error {
	if err := connectorLeaseLoss(ctx); err != nil {
		l.discardPending()
		return err
	}
	flushCtx, cancel := detachedLeaseContext(ctx, streamFinalFlushTimeout)
	defer cancel()
	for _, st := range l.tables {
		if st.deferredEvolution == nil || st.evolutionApplied || st.snapshotStarted {
			continue
		}
		if err := st.deferredEvolution(flushCtx); err != nil {
			return fmt.Errorf("failed to apply deferred schema evolution for table %s: %w", st.destTable, err)
		}
		st.evolutionApplied = true
	}

	if l.buffered == 0 && !l.tokenDirty && !l.hasPendingDurableCheckpoint() {
		return nil
	}
	config.Debug("[STREAM] Final flush: %d buffered rows", l.buffered)
	return l.flush(flushCtx)
}

func (l *flushLoop) hasPendingDurableCheckpoint() bool {
	if l.opts.StateManager != nil {
		return false
	}
	if l.lastDurableCommitID != nil {
		return true
	}
	for _, st := range l.tables {
		if st.checkpointID != nil {
			return true
		}
	}
	return false
}

// cleanup drops staging tables (best-effort) regardless of how the loop ended.
func (l *flushLoop) cleanup(ctx context.Context) {
	if connectorLeaseLoss(ctx) != nil {
		return
	}
	if l.cfg.KeepStaging {
		return
	}
	dropCtx, cancel := detachedLeaseContext(ctx, streamFinalFlushTimeout)
	defer cancel()
	for _, st := range l.tables {
		if connectorLeaseLoss(dropCtx) != nil {
			return
		}
		if st.snapshotStarted && !st.atomicPublishAttempted && st.snapshotAttemptID != "" {
			if aborter, ok := l.dest.(destination.AtomicSnapshotAborter); ok {
				if err := aborter.AbortAtomicSnapshot(dropCtx, destination.AtomicSnapshotOptions{
					Table: st.destTable, AttemptID: st.snapshotAttemptID,
				}); err != nil {
					config.Debug("[STREAM] Warning: failed to abort atomic snapshot stage for %s: %v", st.destTable, err)
				}
			}
		}
		if st.targetCreatedByRun && !st.atomicPublishAttempted {
			cleanupFailedOwnedDirectReplace(dropCtx, l.dest, st.destTable, true, st.targetOwnershipToken)
			st.targetCreatedByRun = false
		}
		if st.stagingTable == "" {
			continue
		}
		if err := l.dest.DropTable(dropCtx, st.stagingTable); err != nil {
			config.Debug("[STREAM] Warning: failed to drop staging table %s: %v", st.stagingTable, err)
		}
		if connectorLeaseLoss(dropCtx) != nil {
			return
		}
	}
}

func (l *flushLoop) releasePending() {
	for _, st := range l.tables {
		for _, b := range st.pending {
			b.Release()
		}
		l.buffered -= st.pendingRows
		st.pending = nil
		st.pendingPositions = nil
		st.pendingRows = 0
	}
}

func (l *flushLoop) discardPending() {
	l.releasePending()
	l.lastToken = nil
	l.tokenDirty = false
	l.pendingState = source.CDCStateCommitToken{}
}

func connectorLeaseLoss(ctx context.Context) error {
	return source.ConnectorLeaseLoss(ctx)
}

func detachedLeaseContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	detached, stopGuard := source.WithoutCancelWithConnectorLease(ctx)
	timed, cancel := context.WithTimeout(detached, timeout)
	return timed, func() {
		cancel()
		stopGuard()
	}
}

func (l *flushLoop) abortLeaseLoss(records <-chan source.RecordBatchResult, loss error) error {
	l.discardPending()
	timer := time.NewTimer(streamDrainTimeout)
	defer timer.Stop()
	for {
		select {
		case res, ok := <-records:
			if !ok {
				return loss
			}
			if res.Batch != nil {
				res.Batch.Release()
			}
		case <-timer.C:
			return loss
		}
	}
}

func (l *flushLoop) parallelism() int {
	if l.cfg.ExtractParallelism > 0 {
		return l.cfg.ExtractParallelism
	}
	return 4
}

func loadTimestampColumn(s *schema.TableSchema) (schema.Column, bool) {
	if s == nil {
		return schema.Column{}, false
	}
	for _, col := range s.Columns {
		if strings.EqualFold(col.Name, naming.IngestrLoadedAtColumn) {
			return col, true
		}
	}
	return schema.Column{}, false
}

// prefilledBatchChannel wraps already-buffered batches in a closed channel so
// WriteParallel (which consumes until close) performs exactly one bounded
// write cycle. Ownership of the batches passes to the writer, which releases
// them after writing.
func prefilledBatchChannel(batches []arrow.RecordBatch) chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult, len(batches))
	for _, b := range batches {
		ch <- source.RecordBatchResult{Batch: b}
	}
	close(ch)
	return ch
}

// drainAndRelease releases batches a failed writer left unconsumed.
func drainAndRelease(ch <-chan source.RecordBatchResult) {
	for res := range ch {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}
}

func drainAndReleaseUntil(ch <-chan source.RecordBatchResult, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case res, ok := <-ch:
			if !ok {
				return true
			}
			if res.Batch != nil {
				res.Batch.Release()
			}
		case <-timer.C:
			return false
		}
	}
}
