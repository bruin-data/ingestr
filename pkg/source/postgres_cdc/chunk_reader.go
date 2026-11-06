package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

func (s *PostgresCDCSource) ValidateCDCChunkedSnapshot(ctx context.Context, tables []source.SourceTableInfo) error {
	if err := validateBatchBarrierSupport(s.serverVersion); err != nil {
		return err
	}
	for _, table := range tables {
		var err error
		nativeSchema, err := s.getTableSchema(ctx, table.Name)
		if err != nil {
			return err
		}
		table.Schema = addCDCColumns(nativeSchema)
		table.Schema.PrimaryKeys = table.PrimaryKeys
		if table.Incarnation == "" {
			table.Incarnation, err = s.TableIncarnation(ctx, table.Name)
			if err != nil {
				return err
			}
		}
		if table.SchemaFingerprint == "" {
			table.SchemaFingerprint, err = s.TableSchemaFingerprint(ctx, table.Name)
			if err != nil {
				return err
			}
		}
		if err := s.validateChunkedTable(ctx, table); err != nil {
			return err
		}
	}
	return nil
}

type chunkReader struct {
	source   *PostgresCDCSource
	tables   []source.SourceTableInfo
	opts     source.ReadOptions
	options  *source.CDCChunkedSnapshotOptions
	state    *chunkBootstrapState
	journal  *chunkJournal
	repl     *MultiTableReplicator
	accum    *batchAccumulator
	results  chan<- source.RecordBatchResult
	multi    bool
	pending  []DecodedChanges
	bytes    int64
	prefetch *chunkPrefetch
}

func (s *PostgresCDCSource) readChunked(ctx context.Context, tables []source.SourceTableInfo, slot string, resumes map[string]string, opts source.ReadOptions, multi bool) (<-chan source.RecordBatchResult, error) {
	options := opts.CDCChunkedSnapshot
	if options == nil || options.Store == nil || !opts.CDCManagedState || !opts.CDCSnapshotReplace || !opts.Streaming {
		return nil, fmt.Errorf("chunked snapshots require destination-managed CDC merge and chunk acknowledgements")
	}
	if options.ChunkSize <= 0 || options.ChunkTimeout <= 0 || options.WALBufferBytes <= 0 ||
		(options.WALStaging != "inmemory" && options.WALStaging != "destination") ||
		(options.Execution != "" && options.Execution != "parallel" && options.Execution != "serial") ||
		(chunkExecution(options.Execution) == "serial" && options.WALStaging == "destination") {
		return nil, fmt.Errorf("invalid chunked snapshot options")
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("chunked snapshots require at least one selected table")
	}
	tables = chunkedTablesInOrder(tables)
	for i := range tables {
		var err error
		if tables[i].Incarnation == "" {
			tables[i].Incarnation, err = s.TableIncarnation(ctx, tables[i].Name)
			if err != nil {
				return nil, err
			}
		}
		if tables[i].SchemaFingerprint == "" {
			tables[i].SchemaFingerprint, err = s.TableSchemaFingerprint(ctx, tables[i].Name)
			if err != nil {
				return nil, err
			}
		}
	}
	if !multi && !opts.FullRefresh && resumes[tables[0].Name] != "" &&
		resumeMetadataChanged(opts.CDCResumeIncarnation, opts.CDCResumeSchemaFingerprint, tables[0].Incarnation, tables[0].SchemaFingerprint) {
		return nil, fmt.Errorf("source table metadata changed before chunked snapshot startup; restart with --full-refresh")
	}
	if err := s.ValidateCDCChunkedSnapshot(ctx, tables); err != nil {
		return nil, err
	}
	if s.standby {
		output.Warnf("[CDC] Chunked snapshots on a standby create a temporary slot per chunk. Run SELECT pg_log_standby_snapshot(); regularly on the primary (for example, every second with pg_cron) to reduce slot creation waits.\n")
	}
	results := make(chan source.RecordBatchResult, 8)
	r := &chunkReader{source: s, tables: tables, opts: opts, options: options, multi: multi, results: results}
	go func() {
		defer close(results)
		if err := r.run(ctx, slot, resumes); err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("chunked PostgreSQL snapshot: %w", err)})
		}
	}()
	return results, nil
}

func (r *chunkReader) run(ctx context.Context, slot string, resumes map[string]string) (retErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if r.prefetch != nil {
			retErr = errors.Join(retErr, r.prefetch.close())
		}
	}()
	start, fresh, err := r.initialize(ctx, slot, resumes)
	if err != nil {
		return err
	}
	schemas := make(map[string]*schema.TableSchema, len(r.tables))
	for _, table := range r.tables {
		schemas[table.Name] = table.Schema
	}
	batchSize := r.opts.PageSize
	if batchSize <= 0 {
		batchSize = r.options.ChunkSize
	}
	r.accum = newBatchAccumulator(batchSize, schemas)
	r.accum.byteLimit = r.options.WALBufferBytes
	r.accum.durable = func() pglogrepl.LSN { return r.state.Applied }
	defer func() { retErr = errors.Join(retErr, r.accum.toast.close()) }()
	cfg := r.source.cdcConfig
	cfg.SlotName = slot
	if !fresh {
		// Ordinary CDC state can be ahead after a crash between its commit
		// and the independent bootstrap checkpoint. Replay from the latter.
		resumes = nil
	}
	serial := chunkExecution(r.options.Execution) == "serial"
	if serial {
		resumes = r.state.replayPositions()
	}
	r.repl, err = NewMultiTableReplicator(r.source, r.tables, cfg, start, &chunkResumeFilter{positions: resumes}, true, batchBarrier{})
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, r.repl.Close(ctx)) }()
	if !serial {
		if err := r.repl.Start(ctx); err != nil {
			return err
		}
	}

	for _, table := range r.tables {
		state := r.state.Tables[table.Name]
		resumeSerial := serial && !r.state.SnapshotComplete && state.Initialized
		if state.Done && !resumeSerial {
			continue
		}
		marker := source.RecordBatchResult{TableName: table.Name, Truncate: true}
		if state.Initialized {
			marker.CDCSnapshotResumed = true
			marker.CDCSnapshotPosition = state.Boundary
		}
		if err := r.emit(ctx, marker); err != nil {
			return err
		}
		state.Initialized = true
	}
	if fresh {
		// No WAL beyond the initial boundary is released until the reset and
		// independent bootstrap checkpoint have both become durable.
		r.state.Identity = ""
		if err := r.checkpoint(ctx, r.state.Applied, nil); err != nil {
			return err
		}
	}
	if r.journal != nil && r.state.complete() {
		if err := r.journal.replay(ctx, r.state.Consumed, func(groups []DecodedChanges) error { return r.apply(ctx, groups) }); err != nil {
			return err
		}
	}
	for _, table := range r.tables {
		for !r.state.Tables[table.Name].Done {
			var err error
			if serial {
				err = r.copySerialChunk(ctx, table)
			} else {
				err = r.copyChunk(ctx, table)
			}
			if err != nil {
				return err
			}
		}
	}
	if serial {
		if err := r.source.ValidateCDCChunkedSnapshot(ctx, r.tables); err != nil {
			return err
		}
		if err := r.validateSlot(ctx, start); err != nil {
			return err
		}
		if err := r.repl.Start(ctx); err != nil {
			return err
		}
	}
	if err := r.catchUp(ctx); err != nil {
		return err
	}
	var snapshots map[string]string
	if serial && !r.state.SnapshotComplete {
		snapshots = make(map[string]string)
		for name, table := range r.state.Tables {
			if table.Initialized {
				snapshots[name] = table.Boundary
			}
		}
		r.state.SnapshotComplete = true
	}
	if err := r.checkpoint(ctx, safeCommitLSN(r.repl, r.accum), snapshots); err != nil {
		return err
	}
	if !r.options.Continuous {
		if err := r.repl.Close(ctx); err != nil {
			return err
		}
		r.source.recordCaughtUpLSN(r.state.Applied, slot, true)
		return r.source.FinalizeBatch(ctx)
	}
	return r.stream(ctx)
}

func (r *chunkReader) emit(ctx context.Context, result source.RecordBatchResult) error {
	if !r.multi {
		result.TableName = ""
	}
	return sendResult(ctx, r.results, result)
}

func (r *chunkReader) copyChunk(ctx context.Context, table source.SourceTableInfo) (retErr error) {
	state := r.state.Tables[table.Name]
	prefetch := r.prefetch
	if prefetch == nil {
		prefetch = r.startChunkRead(ctx, table, r.state.Applied)
	}
	r.prefetch = nil
	var read chunkReadResult
	received := false
	defer func() {
		prefetch.cancel()
		if !received {
			read = <-prefetch.completed
		}
		retErr = errors.Join(retErr, closeChunkSpool(read.spool))
	}()
	for !received {
		select {
		case read = <-prefetch.completed:
			received = true
		default:
		}
		if received {
			break
		}
		if r.journal == nil && r.bytes >= r.options.WALBufferBytes {
			select {
			case read = <-prefetch.completed:
				received = true
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		if err := r.capture(ctx, groups); err != nil {
			return err
		}
	}
	if read.err != nil {
		return read.err
	}
	timing := &source.CDCChunkTiming{}
	if err := r.emitChunk(ctx, table, prefetch, read, timing); err != nil {
		return err
	}
	catchUpStart := time.Now()
	if err := r.drainCaptured(ctx); err != nil {
		return err
	}
	if err := r.catchUp(ctx); err != nil {
		return err
	}
	catchUpTime := time.Since(catchUpStart)
	state.Upper = read.chunk.UpperKey
	if read.chunk.LastKey != nil {
		state.After = read.chunk.LastKey
	}
	state.Done = read.chunk.Done
	var completedTables map[string]string
	if state.Done {
		completedTables = map[string]string{table.Name: state.Boundary}
	}
	position := safeCommitLSN(r.repl, r.accum)
	if !state.Done {
		// The next snapshot must include WAL applied by this chunk, including
		// hard deletes whose destination tombstones may already be removed.
		r.prefetch = r.startChunkRead(ctx, table, position)
	}
	checkpointStart := time.Now()
	if err := r.checkpoint(ctx, position, completedTables, timing); err != nil {
		return err
	}
	logChunkTiming(table.Name, prefetch, read.chunk, timing, catchUpTime, time.Since(checkpointStart))
	return nil
}

func logChunkTiming(table string, prefetch *chunkPrefetch, chunk snapshotChunk, timing *source.CDCChunkTiming, catchUp, checkpoint time.Duration) {
	output.Infof("[CDC] Snapshot chunk table=%s rows=%d slot_ms=%d read_ms=%d load_ms=%d merge_ms=%d catchup_ms=%d checkpoint_ms=%d total_ms=%d\n",
		table, chunk.Rows, chunk.SlotTime.Milliseconds(), chunk.ReadTime.Milliseconds(),
		time.Duration(timing.LoadNanos.Load()).Milliseconds(), time.Duration(timing.MergeNanos.Load()).Milliseconds(),
		catchUp.Milliseconds(), checkpoint.Milliseconds(), time.Since(prefetch.started).Milliseconds())
}

func (r *chunkReader) checkChanges(groups []DecodedChanges) error {
	for _, group := range groups {
		for _, change := range group.Changes {
			if change.Operation == "TRUNCATE" && !r.state.bootstrapComplete() {
				return fmt.Errorf("table %s was truncated during bootstrap; restart with --full-refresh", group.TableName)
			}
			if change.Operation == "UPDATE" {
				for i := range change.Values {
					if columnIsUnchanged(change, i) && resolveColumnValueBase(change, i) == nil && !r.state.bootstrapComplete() {
						return fmt.Errorf("update of %s has an incomplete row image during bootstrap; configure REPLICA IDENTITY FULL and restart with --full-refresh", group.TableName)
					}
				}
			}
		}
	}
	return nil
}

func (r *chunkReader) capture(ctx context.Context, groups []DecodedChanges) error {
	if err := r.checkChanges(groups); err != nil {
		return err
	}
	for _, group := range groups {
		r.pending = append(r.pending, group)
		for _, change := range group.Changes {
			r.bytes += changeStructBytes + estimateChangeBytes(change)
		}
	}
	if r.journal != nil && r.bytes >= min(r.options.WALBufferBytes, int64(4<<20)) {
		return r.stagePending(ctx)
	}
	return nil
}

func (r *chunkReader) stagePending(ctx context.Context) error {
	if len(r.pending) == 0 {
		return nil
	}
	captured := r.repl.CurrentLSN()
	if low, pending := r.repl.PendingLowWater(); pending {
		captured = 0
		if low > 0 {
			captured = low - 1
		}
	}
	if err := r.journal.append(ctx, r.pending, captured); err != nil {
		return err
	}
	r.pending, r.bytes = nil, 0
	r.source.pos.Commit(captured)
	return nil
}

func (r *chunkReader) drainCaptured(ctx context.Context) error {
	if r.journal != nil {
		if err := r.stagePending(ctx); err != nil {
			return err
		}
		return r.journal.replay(ctx, r.state.Consumed, func(groups []DecodedChanges) error { return r.apply(ctx, groups) })
	}
	groups := r.pending
	r.pending, r.bytes = nil, 0
	return r.apply(ctx, groups)
}

func (r *chunkReader) apply(ctx context.Context, groups []DecodedChanges) error {
	if err := r.checkChanges(groups); err != nil {
		return err
	}
	for _, group := range groups {
		r.accum.add(group.TableName, group.Changes, group.LSN)
	}
	return r.flushReady(ctx)
}

func (r *chunkReader) flushReady(ctx context.Context) error {
	return r.withAccumulatorResults(ctx, func(results chan<- source.RecordBatchResult) error {
		return r.accum.flushReadyContext(ctx, results, nil)
	})
}

func (r *chunkReader) flushAll(ctx context.Context) error {
	return r.withAccumulatorResults(ctx, func(results chan<- source.RecordBatchResult) error {
		return r.accum.flushAllContext(ctx, results, nil)
	})
}

func (r *chunkReader) withAccumulatorResults(ctx context.Context, flush func(chan<- source.RecordBatchResult) error) error {
	if r.multi {
		return flush(r.results)
	}
	// Accumulators key by native table identity; single-table consumers use
	// the empty table name. Forward synchronously with bounded buffering.
	results := make(chan source.RecordBatchResult, 1)
	done := make(chan error, 1)
	go func() { done <- flush(results); close(results) }()
	var forwardErr error
	for result := range results {
		if forwardErr != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			continue
		}
		forwardErr = r.emit(ctx, result)
	}
	return errors.Join(forwardErr, <-done)
}

func (r *chunkReader) catchUp(ctx context.Context) error {
	barrier, err := r.source.beginBatchBarrier(ctx)
	if err != nil {
		return err
	}
	r.repl.barrierNonce, r.repl.barrierTarget = barrier.nonce, barrier.target
	r.repl.barrierSeen, r.repl.barrierLSN = false, 0
	lastCheckpoint, pendingRows := time.Now(), 0
	for {
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		if err := r.apply(ctx, groups); err != nil {
			return err
		}
		if !r.repl.BarrierReached() {
			if chunkExecution(r.options.Execution) == "serial" {
				for _, group := range groups {
					pendingRows += len(group.Changes)
				}
				if pendingRows >= r.options.ChunkSize || time.Since(lastCheckpoint) >= streamingMaxAge(r.opts) {
					if err := r.flushAll(ctx); err != nil {
						return err
					}
					if position := safeCommitLSN(r.repl, r.accum); position > r.state.Applied {
						if err := r.checkpoint(ctx, position, nil); err != nil {
							return err
						}
					}
					lastCheckpoint, pendingRows = time.Now(), 0
				}
			}
			continue
		}
		if err := r.flushAll(ctx); err != nil {
			return err
		}
		if safeCommitLSN(r.repl, r.accum) >= r.repl.BarrierLSN() {
			return nil
		}
	}
}

func (r *chunkReader) checkpoint(ctx context.Context, position pglogrepl.LSN, snapshots map[string]string, timings ...*source.CDCChunkTiming) error {
	if err := r.flushAll(ctx); err != nil {
		return err
	}
	previous := r.state.Consumed
	r.state.Applied = max(r.state.Applied, position)
	if r.journal != nil {
		r.state.Consumed = r.journal.manifest.Tail
	}
	commit := &chunkedCommit{store: r.options.Store, state: r.state.clone(), done: make(chan struct{})}
	token := source.CDCStateCommitToken{SourceCommitToken: commit, Position: FormatLSN(r.state.Applied), SnapshotPositions: snapshots}
	if len(snapshots) > 0 {
		token.SnapshotIncarnations = tableIncarnations(r.tables)
		token.SnapshotSchemas = tableSchemaFingerprints(r.tables)
		for table := range token.SnapshotIncarnations {
			if _, ok := snapshots[table]; !ok {
				delete(token.SnapshotIncarnations, table)
				delete(token.SnapshotSchemas, table)
			}
		}
	}
	var timing *source.CDCChunkTiming
	if len(timings) > 0 {
		timing = timings[0]
	}
	if err := r.emit(ctx, source.RecordBatchResult{CDCFlush: true, CommitToken: token, CDCChunkTiming: timing}); err != nil {
		return err
	}
	select {
	case <-commit.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.state.Identity = commit.state.Identity
	if err := r.cleanupRetiredEpochs(ctx); err != nil {
		return fmt.Errorf("failed to retire replaced snapshot WAL: %w", err)
	}
	if r.journal != nil {
		if err := r.journal.prune(ctx, previous, r.state.Consumed); err != nil {
			return fmt.Errorf("failed to retire applied snapshot WAL: %w", err)
		}
	}
	if len(snapshots) > 0 {
		// Snapshot certification retains the replacement boundary. Follow it
		// with the applied checkpoint so ordinary CDC can resume after bootstrap.
		return r.checkpoint(ctx, position, nil)
	}
	return nil
}

func (r *chunkReader) stream(ctx context.Context) error {
	lastFlush, lastCheck, lastHeartbeat := time.Now(), time.Now(), time.Now()
	for {
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		if err := r.apply(ctx, groups); err != nil {
			return err
		}
		lastHeartbeat = maybeEmitStreamHeartbeat(ctx, r.repl, lastHeartbeat)
		if time.Since(lastFlush) >= streamingMaxAge(r.opts) {
			if err := r.flushAll(ctx); err != nil {
				return err
			}
			if position := safeCommitLSN(r.repl, r.accum); position > r.state.Applied {
				if err := r.checkpoint(ctx, position, nil); err != nil {
					return err
				}
			}
			lastFlush = time.Now()
		}
		if interval := r.source.cdcConfig.DiscoverInterval; interval > 0 && time.Since(lastCheck) >= interval {
			if err := r.source.ValidateCDCChunkedSnapshot(ctx, r.tables); err != nil {
				return err
			}
			if r.multi {
				current, err := r.source.GetTables(ctx)
				if err != nil {
					return err
				}
				if len(current) != len(r.tables) {
					return fmt.Errorf("CDC table selection changed; restart the chunked stream to discover tables")
				}
			}
			lastCheck = time.Now()
		}
	}
}

type chunkResumeFilter struct{ positions map[string]string }

func (f *chunkResumeFilter) GetProcessedLSN(table string) pglogrepl.LSN {
	position, _ := parseStoredPostgresLSN(f.positions[table])
	return position
}

func (f *chunkResumeFilter) ShouldFilterChange(table string, lsn pglogrepl.LSN) bool {
	return lsn < f.GetProcessedLSN(table)
}

func (f *chunkResumeFilter) updateProcessedLSN(string, pglogrepl.LSN) {}

func chunkedTablesInOrder(tables []source.SourceTableInfo) []source.SourceTableInfo {
	tables = append([]source.SourceTableInfo(nil), tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables
}
