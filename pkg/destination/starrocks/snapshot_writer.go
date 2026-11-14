package starrocks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/sync/errgroup"
)

// Direct snapshot loads stream into commit windows. Each worker encodes pages
// into JSON requests and loads them into its own transaction in the open window
// (StarRocks refuses concurrent loads into one transaction), while a committer
// prepares, commits and checkpoints earlier windows in order, so neither
// commits nor range boundaries stop loading. A window keeps its request bodies
// until it is committed, so a transaction refused for version pressure can be
// replayed.
type snapshotWriteLimits struct {
	requestBytes  int
	requestTarget int
	// transactionBytes seals a window once its retained buffers reach it; each
	// worker's last request can still overshoot it by up to requestBytes.
	transactionBytes int
	commitInterval   time.Duration
	idleInterval     time.Duration
	retryInitial     time.Duration
	retryMaximum     time.Duration
	retryTimeout     time.Duration
}

const snapshotMemoryBudget = 4 << 30

func defaultSnapshotWriteLimits(workers int) snapshotWriteLimits {
	request := maxStreamLoadJSONBytes
	// The open window and the one committing each hold up to transactionBytes
	// plus a request per worker. Each encoder also holds a request and a row.
	window := max(256<<20, snapshotMemoryBudget/2-2*workers*request)
	return snapshotWriteLimits{
		requestBytes: request, requestTarget: min(64<<20, request),
		transactionBytes: window,
		commitInterval:   10 * time.Second, idleInterval: time.Second,
		retryInitial: 5 * time.Second, retryMaximum: time.Minute, retryTimeout: 5 * time.Minute,
	}
}

type snapshotWriteStats struct {
	recvWait, dispatchWait, sealWait, commit, persist         atomic.Int64
	txPrepare, txCommit, txPublish, visibleWait               atomic.Int64
	idle, prepare, encode, load, loadWait, permitWait, begin  atomic.Int64
	windows, loads, loadBytes, transactions, versionPressures atomic.Int64
	validatedJSONColumns                                      atomic.Int64
}

func since(counter *atomic.Int64, start time.Time) { counter.Add(int64(time.Since(start))) }

func (s *snapshotWriteStats) report(table string, elapsed time.Duration) {
	d := func(c *atomic.Int64) time.Duration { return time.Duration(c.Load()).Round(time.Millisecond) }
	config.Debug("[STARROCKS] Snapshot writer stats table=%s elapsed=%s coordinator: recv_wait=%s dispatch_wait=%s seal_wait=%s windows=%d validated_json_columns=%d; committer: commit=%s (prepare=%s commit=%s) transactions=%d version_pressure=%d; checkpointer: visible_wait=%s (publish=%s) persist=%s; workers(sum): idle=%s prepare=%s encode=%s load_wait=%s; loaders(sum): load=%s (%d, %d MB) permit_wait=%s begin=%s",
		table, elapsed.Round(time.Millisecond), d(&s.recvWait), d(&s.dispatchWait), d(&s.sealWait), s.windows.Load(), s.validatedJSONColumns.Load(),
		d(&s.commit), d(&s.txPrepare), d(&s.txCommit), s.transactions.Load(), s.versionPressures.Load(),
		d(&s.visibleWait), d(&s.txPublish), d(&s.persist),
		d(&s.idle), d(&s.prepare), d(&s.encode), d(&s.loadWait), d(&s.load), s.loads.Load(), s.loadBytes.Load()>>20, d(&s.permitWait), d(&s.begin))
}

// A command without seal asks a worker to flush its buffered request.
type snapshotWriteCommand struct {
	seal     bool
	next     *snapshotWindow
	sequence int64
}

type snapshotWindow struct {
	id         int64
	parts      []*snapshotWindowPart
	bytes      atomic.Int64
	rows       atomic.Int64
	arrowBytes atomic.Uint64
	// open counts workers that may still load into the window.
	open  sync.WaitGroup
	token *source.CDCStateCommitToken
}

type snapshotWindowPart struct {
	mu          sync.Mutex
	tx          *streamLoadTransaction
	options     streamLoadOptions
	replay      [][]byte
	failed      error
	terminalErr error
}

type snapshotWriter struct {
	dest     *StarRocksDestination
	database string
	table    string
	opts     destination.WriteOptions
	limits   snapshotWriteLimits
	limiter  *snapshotLoadLimiter
	stats    *snapshotWriteStats
	workers  int

	mu      sync.Mutex
	windows []*snapshotWindow
}

func (s *snapshotWriter) newWindow(id int64) *snapshotWindow {
	window := &snapshotWindow{id: id, parts: make([]*snapshotWindowPart, s.workers)}
	for i := range window.parts {
		window.parts[i] = &snapshotWindowPart{}
	}
	window.open.Add(s.workers)
	s.mu.Lock()
	s.windows = append(s.windows, window)
	s.mu.Unlock()
	return window
}

func (s *snapshotWriter) begin(ctx context.Context, opts streamLoadOptions) (*streamLoadTransaction, error) {
	start := time.Now()
	defer since(&s.stats.begin, start)
	opts.mergeCondition = cdcOrderColumn
	return s.dest.loader.beginTransaction(ctx, s.database, s.table, s.dest.nextLabel(s.table), opts)
}

func (d *StarRocksDestination) writeSnapshot(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	parallelism = min(parallelism, 8)
	limits := defaultSnapshotWriteLimits(parallelism)
	if d.snapshotLimits != nil {
		limits = *d.snapshotLimits
	}
	db, table := d.tableParts(opts.Table)
	s := &snapshotWriter{
		dest: d, database: db, table: table, opts: opts, limits: limits, workers: parallelism,
		limiter: &snapshotLoadLimiter{limit: parallelism, changed: make(chan struct{})},
		stats:   &snapshotWriteStats{},
	}
	writeStart := time.Now()
	defer func() { s.stats.report(table, time.Since(writeStart)) }()
	err := s.run(ctx, records)
	if err != nil {
		s.rollbackOpen(ctx)
		return err
	}
	return d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation)
}

func (s *snapshotWriter) run(ctx context.Context, records <-chan source.RecordBatchResult) error {
	group, writeCtx := errgroup.WithContext(ctx)
	jobs := make(chan snapshotWriteJob)
	completed := make(chan int64, s.workers)
	commands := make([]chan snapshotWriteCommand, s.workers)
	sealRequests := make(chan struct{}, 1)
	sealed := make(chan *snapshotWindow, 1)
	// One window may be committing while the next one fills; committed windows
	// wait for visibility before their progress is recorded.
	commitSlot := make(chan struct{}, 1)
	checkpoints := make(chan *snapshotWindow, 4)
	checkpointerDone := make(chan struct{})
	current := s.newWindow(0)
	for i := range commands {
		commands[i] = make(chan snapshotWriteCommand, 1)
		worker := &snapshotLoadWorker{id: i, writer: s, window: current, sealRequests: sealRequests}
		group.Go(func() error { return worker.run(writeCtx, jobs, commands[i], completed) })
	}
	group.Go(func() error {
		defer close(checkpoints)
		for {
			select {
			case <-writeCtx.Done():
				return writeCtx.Err()
			case window, ok := <-sealed:
				if !ok {
					return nil
				}
				if err := s.commitWindow(writeCtx, window); err != nil {
					return err
				}
				select {
				case checkpoints <- window:
				case <-writeCtx.Done():
					return writeCtx.Err()
				}
				<-commitSlot
			}
		}
	})
	group.Go(func() error {
		defer close(checkpointerDone)
		for {
			select {
			case <-writeCtx.Done():
				return writeCtx.Err()
			case window, ok := <-checkpoints:
				if !ok {
					return nil
				}
				if err := s.checkpointWindow(writeCtx, window); err != nil {
					return err
				}
			}
		}
	})
	group.Go(func() error {
		defer close(jobs)
		ticker := time.NewTicker(s.limits.commitInterval)
		defer ticker.Stop()
		var progress *source.CDCStateCommitToken
		var pendingProgress []snapshotWriteProgress
		var dispatched, finished int64
		// Count completions until received so workers can always publish them
		// without blocking on the coordinator while it seals a window.
		activePages := 0
		finishedPages := make(map[int64]bool)
		var pending *snapshotWriteJob
		input := records
		promoteProgress := func() {
			for len(pendingProgress) > 0 && pendingProgress[0].sequence <= finished {
				progress = pendingProgress[0].token
				pendingProgress[0] = snapshotWriteProgress{}
				pendingProgress = pendingProgress[1:]
			}
		}
		recordProgress := func(result source.RecordBatchResult) error {
			token, hasToken := result.CommitToken.(source.CDCStateCommitToken)
			if ptr, ok := result.CommitToken.(*source.CDCStateCommitToken); ok && ptr != nil {
				token, hasToken = *ptr, true
			}
			if hasToken && len(token.SnapshotProgress) != 0 && len(token.SnapshotPositions) == 0 {
				pendingProgress = append(pendingProgress, snapshotWriteProgress{
					sequence: dispatched, token: &source.CDCStateCommitToken{SnapshotProgress: token.SnapshotProgress},
				})
				promoteProgress()
			}
			if result.CDCSnapshotRangeEnd {
				for _, command := range commands {
					select {
					case command <- snapshotWriteCommand{sequence: dispatched}:
					case <-writeCtx.Done():
						return writeCtx.Err()
					}
				}
			}
			return nil
		}
		defer func() {
			if pending != nil {
				pending.result.Batch.Release()
			}
		}()
		seal := func(final bool) error {
			start := time.Now()
			select {
			case commitSlot <- struct{}{}:
			case <-writeCtx.Done():
				return writeCtx.Err()
			}
			since(&s.stats.sealWait, start)
			s.stats.windows.Add(1)
			window := current
			window.token, progress = progress, nil
			var next *snapshotWindow
			if !final {
				next = s.newWindow(window.id + 1)
			}
			current = next
			for _, command := range commands {
				select {
				case command <- snapshotWriteCommand{seal: true, next: next}:
				case <-writeCtx.Done():
					return writeCtx.Err()
				}
			}
			select {
			case sealed <- window:
			case <-writeCtx.Done():
				return writeCtx.Err()
			}
			ticker.Reset(s.limits.commitInterval)
			return nil
		}
		sealFull := func() error {
			if current.bytes.Load() >= int64(s.limits.transactionBytes) {
				return seal(false)
			}
			return nil
		}
		for {
			if input == nil && pending == nil && finished == dispatched {
				if err := seal(true); err != nil {
					return err
				}
				close(sealed)
				select {
				case <-checkpointerDone:
					return nil
				case <-writeCtx.Done():
					return writeCtx.Err()
				}
			}
			var dispatch chan<- snapshotWriteJob
			var job snapshotWriteJob
			receive := input
			if pending != nil {
				job, receive = *pending, nil
				if activePages < s.workers {
					dispatch = jobs
				}
			}
			recvStart := time.Now()
			select {
			case <-writeCtx.Done():
				return writeCtx.Err()
			case <-ticker.C:
				if finished < dispatched || current.bytes.Load() > 0 || progress != nil {
					if err := seal(false); err != nil {
						return err
					}
				}
			case <-sealRequests:
				if err := sealFull(); err != nil {
					return err
				}
			case sequence := <-completed:
				activePages--
				finishedPages[sequence] = true
				for finishedPages[finished+1] {
					finished++
					delete(finishedPages, finished)
				}
				promoteProgress()
			case dispatch <- job:
				since(&s.stats.dispatchWait, recvStart)
				dispatched = job.sequence
				activePages++
				pending = nil
				if err := recordProgress(job.result); err != nil {
					return err
				}
			case result, ok := <-receive:
				since(&s.stats.recvWait, recvStart)
				if !ok {
					input = nil
					continue
				}
				if result.Err != nil || result.Truncate {
					if result.Batch != nil {
						result.Batch.Release()
					}
					if result.Err != nil {
						return result.Err
					}
					return fmt.Errorf("StarRocks CDC snapshot cannot contain TRUNCATE events")
				}
				if result.Batch != nil {
					pending = &snapshotWriteJob{result: result, sequence: dispatched + 1}
				} else if err := recordProgress(result); err != nil {
					return err
				}
			}
		}
	})
	return group.Wait()
}

// commitWindow commits a sealed window once every worker has flushed into it.
// Its request bodies are released once StarRocks accepts the commit.
func (s *snapshotWriter) commitWindow(ctx context.Context, window *snapshotWindow) error {
	released := make(chan struct{})
	go func() {
		window.open.Wait()
		close(released)
	}()
	select {
	case <-released:
	case <-ctx.Done():
		return ctx.Err()
	}
	start := time.Now()
	group, commitCtx := errgroup.WithContext(ctx)
	for _, part := range window.parts {
		group.Go(func() error { return s.commitPart(commitCtx, window, part) })
	}
	if err := group.Wait(); err != nil {
		return err
	}
	since(&s.stats.commit, start)
	for _, part := range window.parts {
		part.replay = nil
		if part.tx != nil {
			s.stats.txPrepare.Add(int64(part.tx.prepareTime))
			s.stats.txCommit.Add(int64(part.tx.commitTime))
		}
	}
	return nil
}

// checkpointWindow waits until a committed window is visible, then records the
// progress that its rows cover. Windows arrive in commit order.
func (s *snapshotWriter) checkpointWindow(ctx context.Context, window *snapshotWindow) error {
	start := time.Now()
	group, visibleCtx := errgroup.WithContext(ctx)
	for _, part := range window.parts {
		if part.tx != nil && part.tx.committed && !part.tx.closed {
			group.Go(func() error {
				if err := part.tx.awaitVisible(visibleCtx, nil); err != nil {
					return err
				}
				s.stats.txPublish.Add(int64(part.tx.publishTime))
				return nil
			})
		}
	}
	if err := group.Wait(); err != nil {
		return err
	}
	since(&s.stats.visibleWait, start)
	persistProgress := window.token != nil && s.opts.CDCSnapshotProgress != nil
	rows := window.rows.Load()
	reportCommitted := rows > 0 && s.opts.CDCSnapshotCommitted != nil
	if persistProgress || reportCommitted {
		if err := s.dest.checkCDCIncarnation(ctx, s.opts.Table, s.opts.CDCExpectedIncarnation); err != nil {
			return err
		}
	}
	if persistProgress {
		start := time.Now()
		if err := s.opts.CDCSnapshotProgress(ctx, *window.token); err != nil {
			return fmt.Errorf("persist committed StarRocks snapshot progress: %w", err)
		}
		since(&s.stats.persist, start)
	}
	if reportCommitted {
		if err := s.opts.CDCSnapshotCommitted(ctx, rows, window.arrowBytes.Load()); err != nil {
			return fmt.Errorf("report committed StarRocks snapshot metrics: %w", err)
		}
	}
	window.token = nil
	s.mu.Lock()
	for i, tracked := range s.windows {
		if tracked == window {
			s.windows = slices.Delete(s.windows, i, i+1)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *snapshotWriter) commitPart(ctx context.Context, window *snapshotWindow, part *snapshotWindowPart) error {
	if part.terminalErr != nil {
		return part.terminalErr
	}
	if part.tx == nil && len(part.replay) == 0 {
		return nil
	}
	cause := part.failed
	if cause == nil {
		// Commits don't hold a load permit, which would stall the next
		// window's loads.
		var uncertain error
		uncertain, cause = part.tx.submit(ctx)
		if cause == nil && uncertain != nil {
			// Only visibility tells whether a commit with a lost response took
			// effect; the replay must stay until then.
			cause = part.tx.awaitVisible(ctx, uncertain)
		}
		if cause == nil {
			s.stats.transactions.Add(1)
			config.Debug("[STARROCKS] Committed snapshot window %d transaction %d (label %s) for %s.%s: %d requests, %d bytes",
				window.id, part.tx.id, part.tx.label, s.database, s.table, len(part.replay), window.bytes.Load())
			return nil
		}
	}
	return s.retry(ctx, part, cause)
}

// retry replays a window part refused for version pressure or confirmed aborted.
func (s *snapshotWriter) retry(ctx context.Context, part *snapshotWindowPart, cause error) (retErr error) {
	retryCtx, cancel := context.WithTimeout(ctx, s.limits.retryTimeout)
	defer cancel()
	reason := "transaction retries"
	if isStreamLoadVersionPressure(cause) {
		reason = "version pressure"
	}
	defer func() {
		if retryCtx.Err() != nil {
			if ctx.Err() != nil {
				retErr = ctx.Err()
			} else {
				retErr = fmt.Errorf("StarRocks snapshot %s exceeded %s: %w", reason, s.limits.retryTimeout, errors.Join(cause, retryCtx.Err()))
			}
		}
	}()
	deadline, _ := retryCtx.Deadline()
	delay := s.limits.retryInitial
	for {
		if isStreamLoadVersionPressure(cause) {
			s.stats.versionPressures.Add(1)
			s.limiter.reduce()
		} else {
			aborted, err := part.tx.confirmAborted(retryCtx)
			if err != nil || !aborted {
				return errors.Join(cause, err)
			}
		}
		if part.tx != nil && !part.tx.closed {
			if err := part.tx.rollback(retryCtx); err != nil {
				return fmt.Errorf("rollback rejected StarRocks snapshot transaction: %w (load error: %v)", err, cause)
			}
		}
		if time.Until(deadline) < delay {
			return fmt.Errorf("StarRocks snapshot %s exceeded %s: %w", reason, s.limits.retryTimeout, cause)
		}
		config.Debug("[STARROCKS] Snapshot transaction for %s failed (%v); retrying in %s", s.table, cause, delay)
		timer := time.NewTimer(delay)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return retryCtx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, s.limits.retryMaximum)
		cause = s.withPermit(retryCtx, func() error {
			tx, err := s.begin(retryCtx, part.options)
			part.tx = tx
			if err != nil {
				return err
			}
			for _, body := range part.replay {
				if err := tx.load(retryCtx, body); err != nil {
					return err
				}
			}
			return tx.commit(retryCtx)
		})
		if cause == nil {
			s.stats.transactions.Add(1)
			return nil
		}
	}
}

func (s *snapshotWriter) rollbackOpen(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for _, window := range s.windows {
		for _, part := range window.parts {
			if part.tx != nil && !part.tx.committed && !part.tx.closed {
				if err := part.tx.rollback(cleanupCtx); err != nil {
					config.Debug("[STARROCKS] Roll back snapshot transaction %d: %v", part.tx.id, err)
				}
			}
		}
	}
}

func (s *snapshotWriter) withPermit(ctx context.Context, operation func() error) error {
	start := time.Now()
	if err := s.limiter.acquire(ctx); err != nil {
		return err
	}
	since(&s.stats.permitWait, start)
	defer s.limiter.release()
	return operation()
}

// Progress can cover a page only after all its requests have been assigned to
// windows. A page may span several windows when it exceeds the replay budget.
type snapshotWriteJob struct {
	result   source.RecordBatchResult
	sequence int64
}

type snapshotWriteProgress struct {
	sequence int64
	token    *source.CDCStateCommitToken
}

// A worker encodes pages into requests while its loader sends the previous
// request; loads into one transaction must not overlap.
type snapshotLoadWorker struct {
	id           int
	writer       *snapshotWriter
	window       *snapshotWindow
	sealRequests chan<- struct{}
	loads        chan snapshotLoad
	body         boundedJSONBuffer
	options      streamLoadOptions
	hasOptions   bool
	lastRow      time.Time
}

// snapshotLoad is a request for the worker's loader, or with release, the
// worker's last request into window.
type snapshotLoad struct {
	window  *snapshotWindow
	part    *snapshotWindowPart
	body    []byte
	options streamLoadOptions
	release bool
}

func (w *snapshotLoadWorker) run(ctx context.Context, jobs <-chan snapshotWriteJob, commands <-chan snapshotWriteCommand, completed chan<- int64) error {
	w.body.limit = w.writer.limits.requestBytes
	w.loads = make(chan snapshotLoad)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return w.loadRequests(groupCtx) })
	group.Go(func() error {
		defer close(w.loads)
		return w.encode(groupCtx, jobs, commands, completed)
	})
	err := group.Wait()
	if w.window != nil {
		if err == nil {
			err = fmt.Errorf("StarRocks snapshot worker stopped before sealing its window")
		}
		// Releasing a failed worker must not certify its unflushed rows.
		w.window.parts[w.id].terminalErr = err
		w.window.open.Done()
		w.window = nil
	}
	return err
}

func (w *snapshotLoadWorker) loadRequests(ctx context.Context) error {
	for request := range w.loads {
		if request.release {
			request.window.open.Done()
			continue
		}
		if err := w.load(ctx, request); err != nil {
			return err
		}
	}
	return nil
}

func (w *snapshotLoadWorker) encode(ctx context.Context, jobs <-chan snapshotWriteJob, commands <-chan snapshotWriteCommand, completed chan<- int64) error {
	s := w.writer
	ticker := time.NewTicker(s.limits.idleInterval)
	defer ticker.Stop()
	for {
		// A seal or flush sent before the next page must apply first.
		select {
		case command := <-commands:
			if err := w.handle(ctx, command); err != nil {
				return err
			}
			continue
		default:
		}
		idleStart := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case command := <-commands:
			since(&s.stats.idle, idleStart)
			if err := w.handle(ctx, command); err != nil {
				return err
			}
		case <-ticker.C:
			if !w.lastRow.IsZero() && time.Since(w.lastRow) >= s.limits.idleInterval {
				if err := w.flush(ctx); err != nil {
					return err
				}
			}
		case job, ok := <-jobs:
			if !ok {
				return nil
			}
			since(&s.stats.idle, idleStart)
			rows, bytes := job.result.Batch.NumRows(), snapshotRecordPayloadBytes(job.result.Batch)
			prepStart := time.Now()
			batch, err := prepareCDCSnapshotRecord(job.result.Batch, s.opts)
			since(&s.stats.prepare, prepStart)
			job.result.Batch.Release()
			if err != nil {
				return err
			}
			err = w.append(ctx, batch, job.sequence, commands)
			batch.Release()
			if err != nil {
				return err
			}
			// A page spanning windows is fully durable only with its final window.
			w.window.rows.Add(rows)
			w.window.arrowBytes.Add(bytes)
			select {
			case completed <- job.sequence:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func snapshotRecordPayloadBytes(batch arrow.RecordBatch) uint64 {
	var total uint64
	for i := 0; i < int(batch.NumCols()); i++ {
		column := batch.Column(i)
		if column != nil && column.Data() != nil {
			total += column.Data().SizeInBytes()
		}
	}
	return total
}

func (w *snapshotLoadWorker) handle(ctx context.Context, command snapshotWriteCommand) error {
	if err := w.flush(ctx); err != nil {
		return err
	}
	if command.seal {
		if err := w.queue(ctx, snapshotLoad{window: w.window, release: true}); err != nil {
			return err
		}
		w.window = command.next
	}
	return nil
}

func (w *snapshotLoadWorker) queue(ctx context.Context, request snapshotLoad) error {
	start := time.Now()
	defer since(&w.writer.stats.loadWait, start)
	select {
	case w.loads <- request:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *snapshotLoadWorker) append(ctx context.Context, batch arrow.RecordBatch, sequence int64, commands <-chan snapshotWriteCommand) error {
	s := w.writer
	encoder, opts := newStreamLoadBatchEncoder(batch, s.limits.requestBytes)
	if encoder.initErr != nil {
		return fmt.Errorf("encode StarRocks snapshot batch: %w", encoder.initErr)
	}
	if batch.NumRows() > 0 {
		validated := 0
		for _, v := range encoder.validated {
			if v {
				validated++
			}
		}
		s.stats.validatedJSONColumns.Store(int64(validated))
		if w.hasOptions && w.options != opts {
			return fmt.Errorf("StarRocks snapshot columns changed while loading %s", s.table)
		}
		w.options = opts
		w.hasOptions = true
	}
	flushPage := false
	for encoder.nextRow < batch.NumRows() {
		if err := w.beforeRequest(ctx, commands, sequence, &flushPage); err != nil {
			return err
		}
		if len(w.body.data) == 0 {
			w.body.writeByte('[')
		}
		encodeStart := time.Now()
		if _, err := encoder.appendTo(ctx, &w.body); err != nil {
			return fmt.Errorf("encode StarRocks snapshot batch: %w", err)
		}
		since(&s.stats.encode, encodeStart)
		w.lastRow = time.Now()
		if encoder.nextRow < batch.NumRows() || len(w.body.data) >= s.limits.requestTarget {
			if err := w.flush(ctx); err != nil {
				return err
			}
		}
	}
	if flushPage {
		return w.flush(ctx)
	}
	return nil
}

// beforeRequest stops encoding into a full window even while the coordinator
// waits for the previous commit. Sealing can flush at most one partial request
// per worker beyond the budget.
func (w *snapshotLoadWorker) beforeRequest(ctx context.Context, commands <-chan snapshotWriteCommand, sequence int64, flushPage *bool) error {
	handle := func(command snapshotWriteCommand) error {
		if !command.seal && command.sequence >= sequence {
			// A range boundary must flush the page it follows, even if the
			// command arrives before this page has encoded any rows.
			*flushPage = true
			return nil
		}
		return w.handle(ctx, command)
	}
	for {
		select {
		case command := <-commands:
			if err := handle(command); err != nil {
				return err
			}
			continue
		default:
		}
		if w.window == nil {
			return fmt.Errorf("StarRocks snapshot rows for %s arrived after the final commit", w.writer.table)
		}
		if w.window.bytes.Load() < int64(w.writer.limits.transactionBytes) {
			return nil
		}
		select {
		case command := <-commands:
			if err := handle(command); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// flush queues the buffered request for the worker's window.
func (w *snapshotLoadWorker) flush(ctx context.Context) error {
	if len(w.body.data) <= 1 {
		return nil
	}
	s := w.writer
	w.body.writeByte(']')
	if w.body.err != nil {
		return w.body.err
	}
	if w.window == nil {
		return fmt.Errorf("StarRocks snapshot rows for %s arrived after the final commit", s.table)
	}
	body := w.body.data
	w.body = boundedJSONBuffer{limit: s.limits.requestBytes}
	w.lastRow = time.Time{}
	window := w.window
	part := window.parts[w.id]
	part.mu.Lock()
	part.replay = append(part.replay, body)
	failed := part.failed
	part.mu.Unlock()
	if window.bytes.Add(int64(cap(body))) >= int64(s.limits.transactionBytes) {
		select {
		case w.sealRequests <- struct{}{}:
		default:
		}
	}
	if failed != nil {
		return nil
	}
	return w.queue(ctx, snapshotLoad{window: window, part: part, body: body, options: w.options})
}

// load sends a request into its window part's transaction. A request refused
// for version pressure marks the part for replay at commit.
func (w *snapshotLoadWorker) load(ctx context.Context, request snapshotLoad) error {
	s, window, part, body := w.writer, request.window, request.part, request.body
	part.mu.Lock()
	failed := part.failed
	part.mu.Unlock()
	if failed != nil {
		return nil
	}
	err := s.withPermit(ctx, func() error {
		tx, err := w.transaction(ctx, part, request.options)
		if err != nil {
			return err
		}
		start := time.Now()
		defer since(&s.stats.load, start)
		s.stats.loads.Add(1)
		s.stats.loadBytes.Add(int64(len(body)))
		return tx.load(ctx, body)
	})
	if err == nil {
		return nil
	}
	if !isStreamLoadVersionPressure(err) {
		aborted, abortErr := part.tx.confirmAborted(ctx)
		if abortErr != nil || !aborted {
			return errors.Join(err, abortErr)
		}
	}
	part.mu.Lock()
	if part.failed == nil {
		part.failed = err
	}
	part.mu.Unlock()
	if isStreamLoadVersionPressure(err) {
		s.limiter.reduce()
	}
	config.Debug("[STARROCKS] Snapshot load for %s failed (%v); window %d will be replayed at commit", s.table, err, window.id)
	return nil
}

func (w *snapshotLoadWorker) transaction(ctx context.Context, part *snapshotWindowPart, opts streamLoadOptions) (*streamLoadTransaction, error) {
	part.mu.Lock()
	defer part.mu.Unlock()
	if part.tx == nil {
		part.options = opts
		tx, err := w.writer.begin(ctx, opts)
		part.tx = tx
		if err != nil {
			return nil, err
		}
	}
	return part.tx, nil
}

type snapshotLoadLimiter struct {
	mu      sync.Mutex
	limit   int
	active  int
	changed chan struct{}
}

func (l *snapshotLoadLimiter) acquire(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (l *snapshotLoadLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *snapshotLoadLimiter) reduce() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = max(1, l.limit/2)
}
