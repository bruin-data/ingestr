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
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"golang.org/x/sync/errgroup"
)

const (
	// streamDrainTimeout bounds how long shutdown waits for the source to flush
	// its internal buffers into the channel after cancellation.
	streamDrainTimeout = 15 * time.Second

	// streamFinalFlushTimeout bounds the final flush performed on a detached
	// context after the run context has been cancelled.
	streamFinalFlushTimeout = 60 * time.Second
)

// StreamingOptions configures the streaming flush loop.
type StreamingOptions struct {
	FlushInterval time.Duration
	FlushRecords  int64
	Strategy      config.IncrementalStrategy // merge or append
	Committer     source.StreamCommitter     // nil = source needs no durability feedback
	StateManager  *CDCStateManager           // nil = source does not use destination-managed CDC state
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
	if err := job.ApplyEvolution(ctx); err != nil {
		return fmt.Errorf("failed to apply schema evolution: %w", err)
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
	if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
		return err
	}
	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	records, err := job.GetRecords(ctx, source.ReadOptions{
		IncrementalKey:     job.Config.IncrementalKey,
		IntervalStart:      job.Config.IntervalStart,
		PageSize:           job.Config.PageSize,
		ExcludeColumns:     job.Config.SQLExcludeColumns,
		Parallelism:        parallelism,
		Schema:             job.SourceSchema,
		CDCResumeLSN:       job.Config.CDCResumeLSN,
		CDCSlotSuffix:      job.Config.CDCSlotSuffix,
		CDCSnapshotReplace: st.isCDC && supportsCDCSnapshotReplace(job.Destination),
		Streaming:          true,
		FlushInterval:      job.Config.FlushInterval,
		FlushRecords:       job.Config.FlushRecords,
	})
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	loop := newFlushLoop(job.Destination, job.Config, e.opts, map[string]*streamTableState{"": st})
	// CDC sources re-announce their table (with its refreshed schema) after
	// rebuilding their stream around mid-stream DDL; evolve the destination
	// before the new-shape batches arrive. Sources that never announce
	// (message brokers) leave this callback unused.
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	defer loop.cleanup(ctx)
	return loop.run(ctx, records)
}

// ExecuteMultiTable runs the streaming loop over a multi-table source (CDC).
func (e *StreamingExecutor) ExecuteMultiTable(ctx context.Context, job *MultiTableIngestionJob) error {
	if len(job.Tables) == 0 {
		return nil
	}

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
	for _, ti := range job.Tables {
		st := &streamTableState{
			destTable:      job.GetDestTableName(ti.Name),
			schema:         ti.Schema,
			primaryKeys:    ti.PrimaryKeys,
			incrementalKey: ti.Schema.IncrementalKey,
			isCDC:          hasCDCColumns(ti.Schema),
		}

		if err := job.ApplyEvolutionFor(ctx, ti.Name); err != nil {
			return fmt.Errorf("failed to evolve destination table %s: %w", ti.Name, err)
		}
		if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
			return err
		}
		tables[ti.Name] = st
	}

	parallelism := job.Config.ExtractParallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	records, err := job.Source.ReadAll(ctx, source.MultiTableReadOptions{
		ReadOptions: source.ReadOptions{
			Parallelism:        parallelism,
			PageSize:           job.Config.PageSize,
			CDCSlotSuffix:      job.Config.CDCSlotSuffix,
			CDCSnapshotReplace: anyTableHasCDC && supportsCDCSnapshotReplace(job.Destination),
			Streaming:          true,
			FlushInterval:      job.Config.FlushInterval,
			FlushRecords:       job.Config.FlushRecords,
		},
		CDCResumeLSNs: job.CDCResumeLSNs,
	})
	if err != nil {
		return fmt.Errorf("failed to read from multi-table source: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	loop := newFlushLoop(job.Destination, job.Config, e.opts, tables)
	// CDC sources can discover tables created on the source mid-stream; they
	// announce each one (with schema) before emitting its batches so the
	// destination can be provisioned on the fly.
	loop.prepareNewTable = func(ctx context.Context, ti source.SourceTableInfo) (*streamTableState, error) {
		if !job.Config.NoLoadTimestamp {
			ti.Schema = withLoadTimestampColumn(ti.Schema)
		}
		if ti.Schema == nil {
			return nil, fmt.Errorf("newly discovered table %s has no schema", ti.Name)
		}
		st := &streamTableState{
			destTable:      multiTableDestName(job.Destination, ti),
			schema:         ti.Schema,
			primaryKeys:    ti.PrimaryKeys,
			incrementalKey: ti.Schema.IncrementalKey,
			isCDC:          hasCDCColumns(ti.Schema),
		}
		if err := e.prepareTable(ctx, job.Destination, job.Config, st); err != nil {
			return nil, err
		}
		if job.TableDestNames == nil {
			job.TableDestNames = make(map[string]string)
		}
		job.TableDestNames[ti.Name] = st.destTable
		if e.opts.StateManager != nil {
			if err := e.opts.StateManager.RegisterTable(ctx, ti.Name, st.destTable); err != nil {
				return nil, err
			}
		}
		return st, nil
	}
	// CDC sources also re-announce a table (with its refreshed schema) after
	// rebuilding their stream around mid-stream DDL — a column added or a type
	// changed on the source while streaming. The destination is evolved before
	// the new-shape batches arrive.
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, job.Destination, destTable, newSchema, job.Config)
	}
	defer loop.cleanup(ctx)
	return loop.run(ctx, records)
}

// multiTableDestName computes the destination table name for a source table
// discovered mid-stream, matching the pipeline's upfront naming.
func multiTableDestName(dest destination.Destination, ti source.SourceTableInfo) string {
	if namer, ok := dest.(destination.MultiTableNamer); ok {
		return namer.DestTableName(ti.DestSchema, ti.Name)
	}
	return destination.DefaultMultiTableName(ti.DestSchema, ti.Name)
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
			// Keyless CDC table: no key to merge on, so its change log lands
			// directly in the destination table and flush skips the merge
			// (stagingTable stays empty). See isAppendOnlyCDCTable in merge.go.
			return prepareAppendOnlyCDCTable(ctx, dest, st.destTable, st.schema)
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
		if err := dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:       st.destTable,
			Schema:      prepSchema,
			DropFirst:   false,
			PrimaryKeys: nil,
			PartitionBy: st.partitionBy,
			ClusterBy:   st.clusterBy,
			CDCMode:     st.isCDC,
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
	destTable      string
	stagingTable   string
	schema         *schema.TableSchema
	primaryKeys    []string
	incrementalKey string
	isCDC          bool
	partitionBy    string
	clusterBy      []string

	pending     []arrow.RecordBatch
	pendingRows int64
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
	evolveTable func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error

	buffered int64 // rows buffered across all tables

	// lastToken is the newest CommitToken seen; tokenDirty marks that it has
	// not been committed yet. Tokens may be uncomparable types (maps), so a
	// dirty flag is used instead of comparing tokens.
	lastToken    any
	tokenDirty   bool
	pendingState source.CDCStateCommitToken

	cycles int64
}

func newFlushLoop(dest destination.Destination, cfg *config.IngestConfig, opts StreamingOptions, tables map[string]*streamTableState) *flushLoop {
	return &flushLoop{
		dest:   dest,
		cfg:    cfg,
		opts:   opts,
		tables: tables,
	}
}

func (l *flushLoop) run(ctx context.Context, records <-chan source.RecordBatchResult) error {
	defer l.releasePending()

	ticker := time.NewTicker(l.opts.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-records:
			if !ok {
				// Source ended on its own (e.g. its context was cancelled and
				// it closed the channel before we observed ctx.Done).
				return l.finalFlush(ctx)
			}
			if res.Err != nil {
				if ctx.Err() != nil {
					return l.shutdown(ctx, records)
				}
				return fmt.Errorf("source error: %w", res.Err)
			}
			if res.TableInfo != nil {
				if err := l.ensureTable(ctx, *res.TableInfo); err != nil {
					return err
				}
			}
			if err := l.handleResult(ctx, res); err != nil {
				return err
			}
			if l.buffered >= l.opts.FlushRecords {
				if err := l.flush(ctx); err != nil {
					return err
				}
				ticker.Reset(l.opts.FlushInterval)
			}

		case <-ticker.C:
			if l.buffered == 0 && !l.tokenDirty {
				continue
			}
			if err := l.flush(ctx); err != nil {
				return err
			}

		case <-ctx.Done():
			return l.shutdown(ctx, records)
		}
	}
}

func (l *flushLoop) handleResult(ctx context.Context, res source.RecordBatchResult) error {
	if !res.Truncate {
		l.buffer(res)
		return nil
	}

	if l.buffered > 0 || l.tokenDirty {
		if err := l.flush(ctx); err != nil {
			return fmt.Errorf("failed to flush before source truncate: %w", err)
		}
	}
	st, ok := l.tables[res.TableName]
	if !ok {
		return fmt.Errorf("source requested truncate for unknown table %q", res.TableName)
	}
	truncater, ok := l.dest.(destination.TruncateCapable)
	if !ok {
		return fmt.Errorf("destination scheme %q cannot apply source TRUNCATE events", l.dest.GetScheme())
	}
	if err := truncater.TruncateTable(ctx, st.destTable); err != nil {
		return fmt.Errorf("failed to apply source truncate to %s: %w", st.destTable, err)
	}
	l.buffer(res)
	return nil
}

// ensureTable provisions per-table state for a table announced mid-stream.
// An announcement for a table already known is a schema refresh: the source
// rebuilt its stream around new DDL and re-announces the table so the
// destination can evolve before the new-shape batches arrive. Announcements
// that carry no schema change are ignored.
func (l *flushLoop) ensureTable(ctx context.Context, ti source.SourceTableInfo) error {
	if st, ok := l.tables[ti.Name]; ok {
		return l.refreshTableSchema(ctx, ti, st)
	}
	// Single-table streams key their state under "" but announce with the real
	// source table name; route the refresh to that state.
	if st, ok := l.tables[""]; ok && len(l.tables) == 1 {
		return l.refreshTableSchema(ctx, ti, st)
	}
	if l.prepareNewTable == nil {
		config.Debug("[STREAM] Ignoring new-table announcement for %q (dynamic tables unsupported here)", ti.Name)
		return nil
	}
	st, err := l.prepareNewTable(ctx, ti)
	if err != nil {
		return fmt.Errorf("failed to prepare newly discovered table %s: %w", ti.Name, err)
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
	if l.evolveTable == nil || ti.Schema == nil {
		return nil
	}
	newSchema := ti.Schema
	if !l.cfg.NoLoadTimestamp {
		newSchema = withLoadTimestampColumn(newSchema)
	}
	if st.schema.SameColumnShape(newSchema) {
		return nil
	}

	if err := l.flush(ctx); err != nil {
		return fmt.Errorf("failed to flush before schema change for table %s: %w", ti.Name, err)
	}
	if err := l.evolveTable(ctx, st.destTable, newSchema); err != nil {
		return fmt.Errorf("failed to evolve destination table %s: %w", st.destTable, err)
	}

	st.schema = newSchema
	st.isCDC = hasCDCColumns(newSchema)
	if len(ti.PrimaryKeys) > 0 {
		st.primaryKeys = ti.PrimaryKeys
	}
	if st.stagingTable != "" {
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
	}
	output.Statusf("[STREAM] %s | Schema change applied for %s (dest: %s)\n", time.Now().Format("15:04:05"), ti.Name, st.destTable)
	return nil
}

func (l *flushLoop) buffer(res source.RecordBatchResult) {
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
				for table, position := range token.SnapshotPositions {
					l.pendingState.SnapshotPositions[table] = position
				}
			}
		}
	}
	if res.Batch == nil {
		return
	}
	if res.Batch.NumRows() == 0 {
		res.Batch.Release()
		return
	}
	st, ok := l.tables[res.TableName]
	if !ok {
		config.Debug("[STREAM] Dropping batch for unknown table %q (%d rows)", res.TableName, res.Batch.NumRows())
		res.Batch.Release()
		return
	}
	st.pending = append(st.pending, res.Batch)
	st.pendingRows += res.Batch.NumRows()
	l.buffered += res.Batch.NumRows()
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
	start := time.Now()
	loadTimestamp := start.UTC().Truncate(time.Microsecond)

	type flushWork struct {
		name string
		st   *streamTableState

		batches []arrow.RecordBatch
		rows    int64
	}

	var work []flushWork
	for name, st := range l.tables {
		if st.pendingRows == 0 {
			continue
		}
		work = append(work, flushWork{name: name, st: st, batches: st.pending, rows: st.pendingRows})
		l.buffered -= st.pendingRows
		st.pending = nil
		st.pendingRows = 0
	}

	flushOne := func(ctx context.Context, w flushWork) error {
		st := w.st
		displayName := w.name
		if displayName == "" {
			displayName = st.destTable
		}

		writeOpts := destination.WriteOptions{
			Table:            st.destTable,
			Schema:           st.schema,
			Parallelism:      l.parallelism(),
			StagingBucket:    l.cfg.StagingBucket,
			LoaderFileSize:   l.cfg.LoaderFileSize,
			LoaderFileFormat: l.cfg.LoaderFileFormat,
		}
		if st.stagingTable != "" {
			writeOpts.Table = st.stagingTable
			writeOpts.StagingTable = true
		}

		records := (<-chan source.RecordBatchResult)(prefilledBatchChannel(w.batches))
		if col, ok := loadTimestampColumn(st.schema); ok {
			records = transformer.Wrap(records, transformer.NewLoadTimestamp(col, loadTimestamp))
		}
		if err := l.dest.WriteParallel(ctx, records, writeOpts); err != nil {
			drainAndRelease(records)
			return fmt.Errorf("streaming flush: failed to write %d rows for table %s: %w", w.rows, displayName, err)
		}

		if st.stagingTable != "" {
			if err := mergeStagingInto(ctx, l.dest, st.stagingTable, st.destTable, st.primaryKeys, st.schema, st.incrementalKey); err != nil {
				return fmt.Errorf("streaming flush: failed to merge table %s: %w", displayName, err)
			}
			if err := l.resetStaging(ctx, st); err != nil {
				return fmt.Errorf("streaming flush: failed to reset staging table %s: %w", st.stagingTable, err)
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
		for _, w := range work {
			if err := flushOne(ctx, w); err != nil {
				flushErr = err
				break
			}
		}
	}
	if flushErr != nil {
		return flushErr
	}

	var flushedRows int64
	for _, w := range work {
		flushedRows += w.rows
	}

	if l.opts.StateManager != nil && l.tokenDirty {
		if err := l.opts.StateManager.Persist(ctx, l.pendingState); err != nil {
			return fmt.Errorf("streaming flush: failed to persist destination CDC state: %w", err)
		}
	}
	if l.opts.Committer != nil && l.tokenDirty {
		if err := l.opts.Committer.CommitStream(ctx, l.lastToken); err != nil {
			return fmt.Errorf("streaming flush: failed to commit source position: %w", err)
		}
	}
	l.tokenDirty = false
	l.pendingState = source.CDCStateCommitToken{}

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
	deadline := time.NewTimer(streamDrainTimeout)
	defer deadline.Stop()
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamFinalFlushTimeout)
	defer cancel()

drain:
	for {
		select {
		case res, ok := <-records:
			if !ok {
				break drain
			}
			if res.Err != nil {
				// Cancellation errors from the source are expected here.
				config.Debug("[STREAM] Ignoring source error during shutdown: %v", res.Err)
				continue
			}
			if err := l.handleResult(drainCtx, res); err != nil {
				return err
			}
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
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamFinalFlushTimeout)
	defer cancel()

	if l.buffered == 0 && !l.tokenDirty {
		return nil
	}
	config.Debug("[STREAM] Final flush: %d buffered rows", l.buffered)
	return l.flush(flushCtx)
}

// cleanup drops staging tables (best-effort) regardless of how the loop ended.
func (l *flushLoop) cleanup(ctx context.Context) {
	if l.cfg.KeepStaging {
		return
	}
	dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), streamFinalFlushTimeout)
	defer cancel()
	for _, st := range l.tables {
		if st.stagingTable == "" {
			continue
		}
		if err := l.dest.DropTable(dropCtx, st.stagingTable); err != nil {
			config.Debug("[STREAM] Warning: failed to drop staging table %s: %v", st.stagingTable, err)
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
		st.pendingRows = 0
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
