package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sync/errgroup"
)

type snapshotSpoolReader struct {
	source    *PostgresCDCSource
	tables    []source.SourceTableInfo
	opts      source.ReadOptions
	options   *source.CDCSnapshotSpoolOptions
	state     *snapshotSpoolState
	results   chan<- source.RecordBatchResult
	multi     bool
	repl      *MultiTableReplicator
	accum     *batchAccumulator
	journalMu sync.Mutex
	journal   *chunkJournal
	memory    *snapshotMemoryStore
	wake      chan struct{}
}

func (s *PostgresCDCSource) readSnapshotSpool(ctx context.Context, tables []source.SourceTableInfo, slot string, resumes map[string]string, opts source.ReadOptions, multi bool) (<-chan source.RecordBatchResult, error) {
	o := opts.CDCSnapshotSpool
	if o == nil || o.Store == nil || !opts.CDCManagedState || !opts.CDCSnapshotReplace || !opts.Streaming {
		return nil, fmt.Errorf("normal snapshot spooling requires destination-managed CDC merge and commit acknowledgements")
	}
	if opts.CDCChunkedSnapshot != nil || o.WALBufferBytes <= 0 || (o.WALStaging != "inmemory" && o.WALStaging != "destination" && o.WALStaging != "disk") {
		return nil, fmt.Errorf("invalid normal snapshot spool options")
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("normal snapshot spooling requires selected tables")
	}
	if s.queryPool.Config().MaxConns < 2 {
		return nil, fmt.Errorf("normal snapshot spooling requires at least two query connections for concurrent WAL validation")
	}
	if err := s.requireSnapshotAllowed(); err != nil {
		return nil, err
	}
	if err := validateBatchBarrierSupport(s.serverVersion); err != nil {
		return nil, err
	}
	tables = chunkedTablesInOrder(tables)
	for i := range tables {
		table := &tables[i]
		var err error
		if table.Incarnation == "" {
			table.Incarnation, err = s.TableIncarnation(ctx, table.Name)
			if err != nil {
				return nil, err
			}
		}
		if table.SchemaFingerprint == "" {
			table.SchemaFingerprint, err = s.TableSchemaFingerprint(ctx, table.Name)
			if err != nil {
				return nil, err
			}
		}
		keys := resumableSnapshotKeys(*table)
		if len(keys) == 0 {
			return nil, fmt.Errorf("normal snapshot spooling requires non-null keys for %s", table.Name)
		}
		supported, err := snapshotRangeKeysSupported(ctx, s.queryPool, table.Name, keys)
		if err != nil {
			return nil, err
		}
		if !supported {
			return nil, fmt.Errorf("normal snapshot spooling requires resumable snapshot key ordering for %s", table.Name)
		}
	}
	results := make(chan source.RecordBatchResult, 8)
	r := &snapshotSpoolReader{source: s, tables: tables, opts: opts, options: o, results: results, multi: multi, wake: make(chan struct{}, 1)}
	go func() {
		defer close(results)
		if err := r.run(ctx, slot, resumes); err != nil {
			_ = sendResult(ctx, results, source.RecordBatchResult{Err: fmt.Errorf("normal PostgreSQL snapshot spool: %w", err)})
		}
	}()
	return results, nil
}

func (r *snapshotSpoolReader) emit(ctx context.Context, result source.RecordBatchResult) error {
	if !r.multi {
		result.TableName = ""
	}
	return sendResult(ctx, r.results, result)
}

func (r *snapshotSpoolReader) run(ctx context.Context, slot string, resumes map[string]string) (retErr error) {
	state, err := loadSnapshotSpoolState(ctx, r.options.Store)
	if err != nil {
		return err
	}
	directory := ""
	if r.options.WALStaging == "disk" {
		if r.options.Directory == "" {
			return fmt.Errorf("disk snapshot spooling requires a persistent directory")
		}
		directory, err = filepath.Abs(r.options.Directory)
		if err != nil {
			return err
		}
	}
	refresh := r.opts.FullRefresh && (r.options.RefreshID == "" || state == nil || state.RefreshID != r.options.RefreshID)
	if state != nil && state.Complete && !refresh {
		positions, sameIdentity, err := r.completedSpoolPositions(ctx, state, slot, resumes)
		if err != nil {
			return err
		}
		if sameIdentity {
			r.state = state
			if err := r.cleanupRetiredSpools(ctx); err != nil {
				return err
			}
		}
		// Ordinary CDC state can cover schema evolution after bootstrap. Keep
		// the completed journal only for retirement, not as a resume fallback.
		r.state = nil
		available, err := r.resumeAvailable(ctx, slot, positions)
		if err != nil {
			return err
		}
		if available {
			return r.continueRead(ctx, positions)
		}
		refresh = true
	}
	if state != nil && !refresh {
		if state.Slot != slot || state.Mode != r.options.WALStaging || state.Directory != directory || len(state.Tables) != len(r.tables) {
			return fmt.Errorf("normal snapshot spool configuration changed; restore its settings or use --full-refresh")
		}
		for _, table := range r.tables {
			p, ok := state.Tables[table.Name]
			if !ok || p.Incarnation != table.Incarnation || p.SchemaFingerprint != table.SchemaFingerprint {
				return fmt.Errorf("normal snapshot spool source table %s changed; use --full-refresh", table.Name)
			}
		}
		if !state.Reset {
			identity, err := r.options.Store.Identity(ctx)
			if err != nil {
				return err
			}
			if state.Identity != identity {
				return fmt.Errorf("normal snapshot spool source or destination identity changed; use --full-refresh")
			}
		}
	}
	// A connector with a complete baseline can use the ordinary reader until
	// it needs another snapshot. An unfinished journal always takes precedence.
	if state == nil && !refresh && allSnapshotSpoolTablesResumed(r.tables, resumes) {
		available, err := r.resumeAvailable(ctx, slot, resumes)
		if err != nil {
			return err
		}
		if available {
			return r.continueRead(ctx, resumes)
		}
	}
	fresh := state == nil || refresh || state.Reset
	if fresh {
		retired := retireSnapshotSpool(state)
		epoch, err := newChunkEpoch()
		if err != nil {
			return err
		}
		state = &snapshotSpoolState{Version: 1, Epoch: epoch, Slot: slot, Mode: r.options.WALStaging, Directory: directory, RefreshID: r.options.RefreshID, Reset: true, Tables: make(map[string]source.CDCSnapshotProgress)}
		state.Retired = retired
		for _, table := range r.tables {
			state.Tables[table.Name] = source.CDCSnapshotProgress{Incarnation: table.Incarnation, SchemaFingerprint: table.SchemaFingerprint, Attempt: epoch}
		}
		intent := &snapshotSpoolCommit{store: r.options.Store, state: state.clone()}
		if err := intent.persist(ctx); err != nil {
			return err
		}
	}
	r.state = state
	if !fresh {
		r.source.setWALRetentionSlot(state.Slot)
	}
	var export *snapshotSpoolExport
	if !state.CopyComplete {
		export, err = r.exportSnapshot(ctx, fresh)
		if err != nil {
			return err
		}
		defer export.close()
		if fresh {
			state.Applied, state.Reset = export.boundary, false
			for name, progress := range state.Tables {
				progress.Boundary = FormatLSN(export.boundary)
				state.Tables[name] = progress
			}
		}
		for _, table := range r.tables {
			if err := r.emit(ctx, source.RecordBatchResult{TableName: table.Name, Truncate: true, CDCSnapshotPosition: state.Tables[table.Name].Boundary, CDCSnapshotResumed: !fresh, CDCSnapshotSpoolIdentity: state.Identity}); err != nil {
				return err
			}
		}
		if err := r.checkpoint(ctx, nil); err != nil {
			return err
		}
	}
	if err := r.openJournal(ctx, fresh); err != nil {
		return err
	}
	start := max(state.Applied, r.journal.manifest.Captured)
	available, err := snapshotResumeWALAvailable(ctx, r.source.queryPool, slot, start)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("replication slot no longer covers the normal snapshot journal boundary %s; restore the journal or use --full-refresh", start)
	}
	cfg := r.source.cdcConfig
	cfg.SlotName = slot
	r.repl, err = NewMultiTableReplicator(r.source, r.tables, cfg, start, nil, true, batchBarrier{})
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, r.repl.Close(ctx)) }()
	r.repl.standbyBarrierProbes = true
	if err := r.repl.Start(ctx); err != nil {
		return err
	}
	schemas := make(map[string]*schema.TableSchema, len(r.tables))
	for _, table := range r.tables {
		schemas[table.Name] = table.Schema
	}
	batchSize := r.opts.PageSize
	if batchSize <= 0 {
		batchSize = 10000
	}
	r.accum = newBatchAccumulator(batchSize, schemas)
	r.accum.durable = func() pglogrepl.LSN { return r.state.Applied }
	defer func() { retErr = errors.Join(retErr, r.accum.toast.close()) }()
	runCtx, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	var captureErr error
	go func() {
		defer close(done)
		captureErr = r.capture(runCtx, stop)
		if captureErr != nil {
			cancel()
		}
	}()
	defer func() { cancel(); <-done; retErr = errors.Join(retErr, captureErr) }()
	if !state.CopyComplete {
		if err := r.copySnapshot(runCtx, export); err != nil {
			return err
		}
		export.close()
		state.CopyComplete = true
		positions := make(map[string]string, len(state.Tables))
		for name, progress := range state.Tables {
			positions[name] = progress.Boundary
		}
		if err := r.checkpoint(runCtx, positions); err != nil {
			return err
		}
	}
	if err := r.cleanupRetiredSpools(runCtx); err != nil {
		return err
	}
	if r.options.Continuous {
		for {
			if err := r.replay(runCtx); err != nil {
				return err
			}
			r.journalMu.Lock()
			caughtUp := r.state.Consumed == r.journal.manifest.Tail
			r.journalMu.Unlock()
			if caughtUp {
				break
			}
		}
	}
	close(stop)
	<-done
	if captureErr != nil {
		return captureErr
	}
	var barrier batchBarrier
	if !r.options.Continuous {
		// Freeze the finite run's endpoint before destination backpressure can
		// turn journal replay into an unbounded chase of new source changes.
		barrier, err = r.source.beginBatchBarrier(ctx)
		if err != nil {
			return err
		}
	}
	if err := r.replay(ctx); err != nil {
		return err
	}
	if err := r.catchUp(ctx, barrier); err != nil {
		return err
	}
	state.Complete = true
	if err := r.checkpoint(ctx, nil); err != nil {
		return err
	}
	if err := r.repl.Close(ctx); err != nil {
		return err
	}
	if !r.options.Continuous {
		r.source.recordCaughtUpLSN(state.Applied, slot, true)
		return r.source.FinalizeBatch(ctx)
	}
	if err := r.source.reconnectReplication(ctx); err != nil {
		return err
	}
	return r.continueRead(ctx, nil)
}

func (r *snapshotSpoolReader) completedSpoolPositions(ctx context.Context, state *snapshotSpoolState, slot string, resumes map[string]string) (map[string]string, bool, error) {
	identity, err := r.options.Store.Identity(ctx)
	if err != nil {
		return nil, false, err
	}
	sameIdentity := state.Identity != "" && state.Identity == identity && state.Slot == slot && len(state.Tables) == len(r.tables)
	positions := make(map[string]string, len(r.tables))
	for _, table := range r.tables {
		positions[table.Name] = resumes[table.Name]
		if !r.multi && resumeMetadataChanged(r.opts.CDCResumeIncarnation, r.opts.CDCResumeSchemaFingerprint, table.Incarnation, table.SchemaFingerprint) {
			positions[table.Name] = ""
			sameIdentity = false
		}
		progress, ok := state.Tables[table.Name]
		if !ok || progress.Incarnation != table.Incarnation || progress.SchemaFingerprint != table.SchemaFingerprint {
			sameIdentity = false
		}
	}
	if sameIdentity {
		for name, position := range positions {
			if position == "" {
				positions[name] = FormatLSN(state.Applied)
			}
		}
	}
	return positions, sameIdentity, nil
}

func allSnapshotSpoolTablesResumed(tables []source.SourceTableInfo, positions map[string]string) bool {
	for _, table := range tables {
		if positions[table.Name] == "" {
			return false
		}
	}
	return true
}

func (r *snapshotSpoolReader) resumeAvailable(ctx context.Context, slot string, resumes map[string]string) (bool, error) {
	var start pglogrepl.LSN
	for _, table := range r.tables {
		var position pglogrepl.LSN
		if raw := resumes[table.Name]; raw != "" {
			var err error
			position, err = parseStoredPostgresLSN(raw)
			if err != nil {
				return false, err
			}
		} else if r.state != nil {
			position = r.state.Applied
		}
		if position == 0 {
			return false, nil
		}
		if start == 0 || position < start {
			start = position
		}
	}
	waitReplicationSlotReleased(ctx, r.source.queryPool, slot)
	return snapshotResumeWALAvailable(ctx, r.source.queryPool, slot, start)
}

func (r *snapshotSpoolReader) continueRead(ctx context.Context, resumes map[string]string) error {
	opts := r.opts
	opts.CDCSnapshotSpool = nil
	opts.FullRefresh = false
	opts.Streaming = r.options.Continuous
	positions := make(map[string]string, len(r.tables))
	for _, table := range r.tables {
		positions[table.Name] = resumes[table.Name]
		if positions[table.Name] == "" && r.state != nil {
			positions[table.Name] = FormatLSN(r.state.Applied)
		}
	}
	cfg := r.source.cdcConfig
	if r.state != nil {
		cfg.SlotName = r.state.Slot
	}
	var results <-chan source.RecordBatchResult
	var err error
	if r.multi {
		reader := NewMultiTableCDCReader(r.source, r.tables, cfg, positions, r.opts.CDCSlotSuffix)
		results, err = reader.Read(ctx, source.MultiTableReadOptions{ReadOptions: opts})
	} else {
		table := r.tables[0]
		opts.CDCResumeLSN = positions[table.Name]
		opts.CDCResumeIncarnation = table.Incarnation
		opts.CDCResumeSchemaFingerprint = table.SchemaFingerprint
		results, err = NewCDCReader(r.source, table.Name, table.Schema, cfg).Read(ctx, opts)
	}
	if err != nil {
		return err
	}
	for result := range results {
		if result.Err != nil {
			drainRecordBatchResults(results)
			return result.Err
		}
		if err := sendResult(ctx, r.results, result); err != nil {
			drainRecordBatchResults(results)
			return err
		}
	}
	if !r.options.Continuous {
		token := r.source.CDCState()
		token.SourceCommitToken = snapshotSpoolBatchCommit{}
		return r.emit(ctx, source.RecordBatchResult{CDCFlush: true, CommitToken: token})
	}
	return nil
}

func (r *snapshotSpoolReader) checkpoint(ctx context.Context, snapshots map[string]string) error {
	commit := &snapshotSpoolCommit{store: r.options.Store, state: r.state.clone(), done: make(chan struct{})}
	token := source.CDCStateCommitToken{SourceCommitToken: commit, SnapshotPositions: snapshots}
	if r.state.CopyComplete {
		token.Position = FormatLSN(r.state.Applied)
	}
	if len(snapshots) > 0 {
		token.SnapshotIncarnations = tableIncarnations(r.tables)
		token.SnapshotSchemas = tableSchemaFingerprints(r.tables)
	}
	if err := r.emit(ctx, source.RecordBatchResult{CDCFlush: true, CommitToken: token}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-commit.done:
	}
	r.state.Identity = commit.state.Identity
	return nil
}

type snapshotSpoolExport struct {
	conn     *pgx.Conn
	tx       pgx.Tx
	name     string
	boundary pglogrepl.LSN
}

func (e *snapshotSpoolExport) close() {
	if e.tx != nil {
		rollbackSnapshot(e.tx)
		e.tx = nil
	}
	if e.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.conn.Close(ctx)
		e.conn = nil
	}
}

func (r *snapshotSpoolReader) exportSnapshot(ctx context.Context, fresh bool) (export *snapshotSpoolExport, retErr error) {
	conn := r.source.replConn
	if fresh {
		if _, exists, err := checkSlotExists(ctx, r.source.queryPool, r.state.Slot); err != nil {
			return nil, err
		} else if exists {
			if _, err := r.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", r.state.Slot); err != nil {
				return nil, err
			}
		}
	} else {
		var err error
		conn, err = r.source.openReplicationConn(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			retErr = errors.Join(retErr, conn.Close(closeCtx))
			if retErr != nil && export != nil {
				export.close()
			}
		}()
	}
	slot := r.state.Slot
	if !fresh {
		epoch, err := newChunkEpoch()
		if err != nil {
			return nil, err
		}
		slot = truncateSlotName(slot + "_export_" + epoch)
	}
	created, err := r.source.createLogicalSlot(ctx, conn, slot, !fresh)
	if err != nil {
		return nil, err
	}
	return r.handoffSnapshot(ctx, conn, created)
}

func (r *snapshotSpoolReader) handoffSnapshot(ctx context.Context, exporter *pgconn.PgConn, created pglogrepl.CreateReplicationSlotResult) (_ *snapshotSpoolExport, retErr error) {
	e := &snapshotSpoolExport{}
	defer func() {
		if retErr != nil {
			e.close()
		}
	}()
	var err error
	e.boundary, err = pglogrepl.ParseLSN(created.ConsistentPoint)
	if err != nil {
		return nil, err
	}
	e.conn, err = pgx.ConnectConfig(ctx, r.source.queryPool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	e.tx, err = e.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	if _, err = e.tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+strings.ReplaceAll(created.SnapshotName, "'", "''")+"'"); err != nil {
		return nil, fmt.Errorf("import normal snapshot for WAL capture: %w", err)
	}
	if err = e.tx.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&e.name); err != nil {
		return nil, fmt.Errorf("retain normal snapshot while decoding WAL: %w", err)
	}
	if _, err = pglogrepl.IdentifySystem(ctx, exporter); err != nil {
		return nil, fmt.Errorf("release slot snapshot export: %w", err)
	}
	return e, nil
}

func (r *snapshotSpoolReader) copySnapshot(ctx context.Context, export *snapshotSpoolExport) error {
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan source.RecordBatchResult, 8)
	done := make(chan error, 1)
	progress := r.state.clone().Tables
	go func() {
		defer close(results)
		g, gctx := errgroup.WithContext(copyCtx)
		budget := newSnapshotReadBudget(r.source.queryPool, r.opts.Parallelism)
		budget.slots = make(chan struct{}, min(cap(budget.slots), int(r.source.queryPool.Config().MaxConns)-1))
		g.SetLimit(cap(budget.slots))
		for _, table := range r.tables {
			g.Go(func() error {
				p := progress[table.Name]
				snapshot := &Snapshot{source: r.source, tableName: table.Name, tableSchema: table.Schema, cdcConfig: r.source.cdcConfig, readBudget: budget, progressKeys: resumableSnapshotKeys(table), progress: p, resuming: len(p.Ranges) > 0}
				opts := r.opts
				opts.CDCSnapshotReplace = false
				local := make(chan source.RecordBatchResult, 1)
				readDone := make(chan error, 1)
				go func() {
					readDone <- snapshot.readProgressSnapshot(gctx, export.name, export.boundary, local, opts)
					close(local)
				}()
				for result := range local {
					result.TableName = table.Name
					if err := sendResult(gctx, results, result); err != nil {
						drainRecordBatchResults(local)
						<-readDone
						return err
					}
				}
				return <-readDone
			})
		}
		done <- g.Wait()
	}()
	for result := range results {
		if token, ok := result.CommitToken.(source.CDCStateCommitToken); ok && len(token.SnapshotProgress) > 0 {
			for name, p := range token.SnapshotProgress {
				r.state.Tables[name] = p.Clone()
			}
			if err := r.checkpoint(ctx, nil); err != nil {
				cancel()
				drainRecordBatchResults(results)
				<-done
				return err
			}
			continue
		}
		if err := r.emit(ctx, result); err != nil {
			cancel()
			drainRecordBatchResults(results)
			<-done
			return err
		}
	}
	return <-done
}

func (r *snapshotSpoolReader) openJournal(ctx context.Context, fresh bool) error {
	store := r.options.Store
	var err error
	switch r.options.WALStaging {
	case "disk":
		store, err = newSnapshotDiskStore(r.state.Directory, r.state.Identity)
	case "inmemory":
		r.memory = &snapshotMemoryStore{values: make(map[string][]byte)}
		store = r.memory
	}
	if err != nil {
		return err
	}
	r.journal = &chunkJournal{store: store, epoch: "normal-" + r.state.Epoch}
	data, err := store.Load(ctx, r.journal.manifestKey())
	if err != nil {
		return err
	}
	if data == nil && !fresh && r.memory == nil {
		retained, err := snapshotResumeWALAvailable(ctx, r.source.queryPool, r.state.Slot, r.state.Applied)
		if err != nil {
			return err
		}
		if !retained {
			return fmt.Errorf("normal snapshot WAL manifest is missing; restore the spool or use --full-refresh")
		}
	}
	if err := r.journal.load(ctx, r.state.Consumed); err != nil {
		return err
	}
	if data == nil {
		return r.journal.append(ctx, nil, r.state.Applied)
	}
	return nil
}

// The memory backend retains source WAL; its contents are disposable on restart.
type snapshotMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
	bytes  int64
}

func (s *snapshotMemoryStore) Load(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.values[key]...), ctx.Err()
}

func (s *snapshotMemoryStore) Save(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasSuffix(key, "/manifest") {
		s.bytes += int64(len(value) - len(s.values[key]))
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *snapshotMemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasSuffix(key, "/manifest") {
		s.bytes -= int64(len(s.values[key]))
	}
	delete(s.values, key)
	return nil
}
func (*snapshotMemoryStore) Identity(context.Context) (string, error) { return "memory", nil }
func (s *snapshotMemoryStore) size() int64                            { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes }
