package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

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
	source  *PostgresCDCSource
	tables  []source.SourceTableInfo
	opts    source.ReadOptions
	options *source.CDCChunkedSnapshotOptions
	state   *chunkBootstrapState
	journal *chunkJournal
	repl    *MultiTableReplicator
	accum   *batchAccumulator
	results chan<- source.RecordBatchResult
	multi   bool
	pending []DecodedChanges
	bytes   int64
}

func (s *PostgresCDCSource) readChunked(ctx context.Context, tables []source.SourceTableInfo, slot string, resumes map[string]string, opts source.ReadOptions, multi bool) (<-chan source.RecordBatchResult, error) {
	options := opts.CDCChunkedSnapshot
	if options == nil || options.Store == nil || !opts.CDCManagedState || !opts.CDCSnapshotReplace || !opts.Streaming {
		return nil, fmt.Errorf("chunked snapshots require destination-managed CDC merge and chunk acknowledgements")
	}
	if options.ChunkSize <= 0 || options.ChunkTimeout <= 0 || options.WALBufferBytes <= 0 ||
		(options.WALStaging != "inmemory" && options.WALStaging != "destination") {
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
	r.repl, err = NewMultiTableReplicator(r.source, r.tables, cfg, start, &chunkResumeFilter{positions: resumes}, true, batchBarrier{})
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, r.repl.Close(ctx)) }()
	if err := r.repl.Start(ctx); err != nil {
		return err
	}

	for _, table := range r.tables {
		state := r.state.Tables[table.Name]
		if state.Done {
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
			if err := r.copyChunk(ctx, table); err != nil {
				return err
			}
		}
	}
	if err := r.catchUp(ctx); err != nil {
		return err
	}
	if err := r.checkpoint(ctx, safeCommitLSN(r.repl, r.accum), nil); err != nil {
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

func (r *chunkReader) initialize(ctx context.Context, slot string, resumes map[string]string) (pglogrepl.LSN, bool, error) {
	identity, err := r.options.Store.Identity(ctx)
	if err != nil {
		return 0, false, err
	}
	state, err := loadChunkState(ctx, r.options.Store)
	if err != nil {
		return 0, false, err
	}
	var retired []chunkRetiredEpoch
	if r.opts.FullRefresh {
		retired = chunkEpochsToRetire(state)
		state = nil
		clear(resumes)
	}
	if state != nil && state.Identity != identity {
		return 0, false, fmt.Errorf("source or destination changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline")
	}
	fresh := state == nil
	if fresh {
		epoch, err := newChunkEpoch()
		if err != nil {
			return 0, false, err
		}
		state = &chunkBootstrapState{Version: 1, Epoch: epoch, Identity: identity, Slot: slot, Mode: r.options.WALStaging, Tables: make(map[string]*chunkTableState), RetiredEpochs: retired}
		for _, table := range r.tables {
			if position := resumes[table.Name]; position != "" {
				lsn, err := parseStoredPostgresLSN(position)
				if err != nil {
					return 0, false, err
				}
				if state.Applied == 0 || lsn < state.Applied {
					state.Applied = lsn
				}
			}
		}
		if state.Applied == 0 {
			if _, exists, err := checkSlotExists(ctx, r.source.queryPool, slot); err != nil {
				return 0, false, err
			} else if exists {
				if _, err := r.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slot); err != nil {
					return 0, false, fmt.Errorf("failed to reset chunked snapshot slot: %w", err)
				}
			}
			created, err := r.source.createLogicalSlot(ctx, r.source.replConn, slot, false)
			if err != nil {
				return 0, false, err
			}
			state.Applied, err = pglogrepl.ParseLSN(created.ConsistentPoint)
			if err != nil {
				return 0, false, err
			}
		}
		for _, table := range r.tables {
			state.Tables[table.Name] = &chunkTableState{Boundary: FormatLSN(state.Applied), Done: resumes[table.Name] != ""}
		}
	} else {
		if state.Slot != slot || state.Mode != r.options.WALStaging {
			return 0, false, fmt.Errorf("chunked snapshot slot or WAL staging mode changed; keep the original configuration or use --full-refresh")
		}
		if len(state.Tables) != len(r.tables) {
			return 0, false, fmt.Errorf("chunked snapshot table selection changed")
		}
		for _, table := range r.tables {
			if state.Tables[table.Name] == nil {
				return 0, false, fmt.Errorf("chunked snapshot has no checkpoint for %s", table.Name)
			}
		}
	}
	r.state = state
	start := state.Applied
	if r.options.WALStaging == "destination" {
		r.journal = &chunkJournal{store: r.options.Store, epoch: state.Epoch}
		if err := r.journal.load(ctx, state.Consumed); err != nil {
			return 0, false, err
		}
		start = max(start, r.journal.manifest.Captured)
	}
	if err := r.validateSlot(ctx, start); err != nil {
		return 0, false, err
	}
	return start, fresh, nil
}

func (r *chunkReader) validateSlot(ctx context.Context, start pglogrepl.LSN) error {
	var restart, confirmed *string
	var plugin string
	var sameDatabase, temporary bool
	if err := r.source.queryPool.QueryRow(ctx, `SELECT restart_lsn::text, confirmed_flush_lsn::text, plugin, database = current_database(), temporary FROM pg_replication_slots WHERE slot_name = $1`, r.state.Slot).Scan(&restart, &confirmed, &plugin, &sameDatabase, &temporary); err != nil {
		return fmt.Errorf("chunked snapshot replication slot is unavailable; retained WAL is required: %w", err)
	}
	if plugin != "pgoutput" || restart == nil || confirmed == nil || !sameDatabase || temporary {
		return fmt.Errorf("chunked snapshot requires a valid persistent pgoutput slot")
	}
	for _, raw := range []*string{restart, confirmed} {
		position, err := pglogrepl.ParseLSN(*raw)
		if err != nil || position > start {
			return fmt.Errorf("replication slot no longer retains the chunked snapshot resume boundary %s", start)
		}
	}
	return nil
}

func (r *chunkReader) emit(ctx context.Context, result source.RecordBatchResult) error {
	if !r.multi {
		result.TableName = ""
	}
	return sendResult(ctx, r.results, result)
}

type chunkReadResult struct {
	chunk snapshotChunk
	err   error
}

func (r *chunkReader) copyChunk(ctx context.Context, table source.SourceTableInfo) error {
	state := r.state.Tables[table.Name]
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	completed := make(chan chunkReadResult, 1)
	maxBytes := r.opts.MaxBatchBytes
	if maxBytes <= 0 {
		maxBytes = r.options.WALBufferBytes
	}
	go func() {
		chunk, err := r.source.readSnapshotChunk(readCtx, table, r.state.Slot, state.After, state.Upper, r.options.ChunkSize, maxBytes, r.options.ChunkTimeout)
		completed <- chunkReadResult{chunk: chunk, err: err}
	}()
	var read chunkReadResult
	received := false
	defer func() {
		cancel()
		if !received {
			read = <-completed
		}
		if read.chunk.Batch != nil {
			read.chunk.Batch.Release()
		}
	}()
	for !received {
		select {
		case read = <-completed:
			received = true
		default:
		}
		if received {
			break
		}
		if r.journal == nil && r.bytes >= r.options.WALBufferBytes {
			select {
			case read = <-completed:
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
	if read.chunk.Batch != nil {
		batch := read.chunk.Batch
		read.chunk.Batch = nil
		if err := r.emit(ctx, source.RecordBatchResult{TableName: table.Name, Batch: batch}); err != nil {
			return err
		}
	}
	if err := r.drainCaptured(ctx); err != nil {
		return err
	}
	if err := r.catchUp(ctx); err != nil {
		return err
	}
	state.Upper = read.chunk.UpperKey
	if read.chunk.LastKey != nil {
		state.After = read.chunk.LastKey
	}
	state.Done = read.chunk.Done
	var completedTables map[string]string
	if state.Done {
		completedTables = map[string]string{table.Name: state.Boundary}
	}
	return r.checkpoint(ctx, safeCommitLSN(r.repl, r.accum), completedTables)
}

func (r *chunkReader) checkChanges(groups []DecodedChanges) error {
	for _, group := range groups {
		for _, change := range group.Changes {
			if change.Operation == "TRUNCATE" && !r.state.complete() {
				return fmt.Errorf("table %s was truncated during bootstrap; restart with --full-refresh", group.TableName)
			}
			if change.Operation == "UPDATE" {
				for i := range change.Values {
					if columnIsUnchanged(change, i) && resolveColumnValueBase(change, i) == nil && !r.state.complete() {
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
	for {
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		if err := r.apply(ctx, groups); err != nil {
			return err
		}
		if !r.repl.BarrierReached() {
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

func (r *chunkReader) checkpoint(ctx context.Context, position pglogrepl.LSN, snapshots map[string]string) error {
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
	if err := r.emit(ctx, source.RecordBatchResult{CDCFlush: true, CommitToken: token}); err != nil {
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
