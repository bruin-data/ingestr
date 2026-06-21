package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
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
		IncrementalKey: job.Config.IncrementalKey,
		IntervalStart:  job.Config.IntervalStart,
		PageSize:       job.Config.PageSize,
		ExcludeColumns: job.Config.SQLExcludeColumns,
		Parallelism:    parallelism,
		Schema:         job.SourceSchema,
		CDCResumeLSN:   job.Config.CDCResumeLSN,
		CDCSlotSuffix:  job.Config.CDCSlotSuffix,
		Streaming:      true,
	})
	if err != nil {
		return fmt.Errorf("failed to get records: %w", err)
	}

	if job.Tracker != nil {
		records = job.Tracker.Wrap(records)
	}

	loop := newFlushLoop(job.Destination, job.Config, e.opts, map[string]*streamTableState{"": st})
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
			Parallelism:   parallelism,
			PageSize:      job.Config.PageSize,
			CDCSlotSuffix: job.Config.CDCSlotSuffix,
			Streaming:     true,
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
	defer loop.cleanup(ctx)
	return loop.run(ctx, records)
}

// prepareTable sets up the destination (and staging, for merge) tables for one
// stream table and records the staging name on the state.
func (e *StreamingExecutor) prepareTable(ctx context.Context, dest destination.Destination, cfg *config.IngestConfig, st *streamTableState) error {
	switch e.opts.Strategy {
	case config.StrategyMerge:
		st.stagingTable = GenerateStagingTableName(st.destTable, "stream", cfg.StagingDataset)
		fmt.Printf("[STREAM] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), st.stagingTable)
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
		if err := dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:       st.destTable,
			Schema:      destination.DestinationTableSchema(st.schema),
			DropFirst:   false,
			PrimaryKeys: nil,
			PartitionBy: st.partitionBy,
			ClusterBy:   st.clusterBy,
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

	buffered int64 // rows buffered across all tables

	// lastToken is the newest CommitToken seen; tokenDirty marks that it has
	// not been committed yet. Tokens may be uncomparable types (maps), so a
	// dirty flag is used instead of comparing tokens.
	lastToken  any
	tokenDirty bool

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
			l.buffer(res)
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

func (l *flushLoop) buffer(res source.RecordBatchResult) {
	if res.CommitToken != nil {
		l.lastToken = res.CommitToken
		l.tokenDirty = true
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
func (l *flushLoop) flush(ctx context.Context) error {
	start := time.Now()
	var flushedRows int64

	for name, st := range l.tables {
		if st.pendingRows == 0 {
			continue
		}
		batches := st.pending
		rows := st.pendingRows
		st.pending = nil
		st.pendingRows = 0
		l.buffered -= rows

		displayName := name
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

		ch := prefilledBatchChannel(batches)
		if err := l.dest.WriteParallel(ctx, ch, writeOpts); err != nil {
			drainAndRelease(ch)
			return fmt.Errorf("streaming flush: failed to write %d rows for table %s: %w", rows, displayName, err)
		}

		if st.stagingTable != "" {
			if err := mergeStagingInto(ctx, l.dest, st.stagingTable, st.destTable, st.primaryKeys, st.schema, st.incrementalKey); err != nil {
				return fmt.Errorf("streaming flush: failed to merge table %s: %w", displayName, err)
			}
			if err := l.resetStaging(ctx, st); err != nil {
				return fmt.Errorf("streaming flush: failed to reset staging table %s: %w", st.stagingTable, err)
			}
		}

		flushedRows += rows
	}

	if l.opts.Committer != nil && l.tokenDirty {
		if err := l.opts.Committer.CommitStream(ctx, l.lastToken); err != nil {
			return fmt.Errorf("streaming flush: failed to commit source position: %w", err)
		}
	}
	l.tokenDirty = false

	l.cycles++
	if flushedRows > 0 {
		fmt.Printf("[STREAM] %s | cycle %d: flushed %d rows in %s\n", time.Now().Format("15:04:05"), l.cycles, flushedRows, time.Since(start).Round(time.Millisecond))
	}
	return nil
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
			l.buffer(res)
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
