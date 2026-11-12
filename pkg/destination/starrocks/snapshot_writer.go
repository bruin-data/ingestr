package starrocks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"golang.org/x/sync/errgroup"
)

type snapshotWriteLimits struct {
	requestBytes     int
	requestTarget    int
	transactionBytes int
	replayMemory     int
	commitInterval   time.Duration
	idleInterval     time.Duration
	retryInitial     time.Duration
	retryMaximum     time.Duration
	retryTimeout     time.Duration
}

func defaultSnapshotWriteLimits(workers int) snapshotWriteLimits {
	return snapshotWriteLimits{
		requestBytes: maxStreamLoadJSONBytes, requestTarget: min(64<<20, maxStreamLoadJSONBytes),
		transactionBytes: 512 << 20,
		// Include the request being encoded in the aggregate four-GiB budget.
		replayMemory:   min(512<<20, (4<<30)/workers-maxStreamLoadJSONBytes),
		commitInterval: 10 * time.Second, idleInterval: time.Second,
		retryInitial: 5 * time.Second, retryMaximum: time.Minute, retryTimeout: 5 * time.Minute,
	}
}

type snapshotWriteStats struct {
	recvWait, dispatchWait, commitBarrier, rangeBarrier, persist atomic.Int64
	idle, prepare, encode, load, permitWait, commit, begin       atomic.Int64
	commits, rangeBarriers, loads, loadBytes                     atomic.Int64
}

func since(counter *atomic.Int64, start time.Time) { counter.Add(int64(time.Since(start))) }

func (s *snapshotWriteStats) report(table string, elapsed time.Duration) {
	d := func(c *atomic.Int64) time.Duration { return time.Duration(c.Load()).Round(time.Millisecond) }
	config.Debug("[STARROCKS] Snapshot writer stats table=%s elapsed=%s coordinator: recv_wait=%s dispatch_wait=%s commit_barrier=%s (%d) range_barrier=%s (%d) persist=%s; workers(sum): idle=%s prepare=%s encode=%s load=%s (%d, %d MB) permit_wait=%s begin=%s commit=%s",
		table, elapsed.Round(time.Millisecond), d(&s.recvWait), d(&s.dispatchWait), d(&s.commitBarrier), s.commits.Load(), d(&s.rangeBarrier), s.rangeBarriers.Load(), d(&s.persist),
		d(&s.idle), d(&s.prepare), d(&s.encode), d(&s.load), s.loads.Load(), s.loadBytes.Load()>>20, d(&s.permitWait), d(&s.begin), d(&s.commit))
}

type snapshotWriteCommand struct {
	commit bool
	done   chan<- error
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
	limiter := &snapshotLoadLimiter{limit: parallelism, changed: make(chan struct{})}
	stats := &snapshotWriteStats{}
	writeStart := time.Now()
	defer func() { stats.report(table, time.Since(writeStart)) }()
	group, writeCtx := errgroup.WithContext(ctx)
	jobs := make(chan source.RecordBatchResult)
	commands := make([]chan snapshotWriteCommand, parallelism)
	for i := range commands {
		commands[i] = make(chan snapshotWriteCommand, 1)
		worker := &snapshotLoadWorker{dest: d, database: db, table: table, opts: opts, limits: limits, limiter: limiter, stats: stats}
		group.Go(func() error { return worker.run(writeCtx, jobs, commands[i]) })
	}
	group.Go(func() error {
		defer close(jobs)
		ticker := time.NewTicker(limits.commitInterval)
		defer ticker.Stop()
		var progress *source.CDCStateCommitToken
		barrier := func(commit bool) error {
			done := make(chan error, parallelism)
			for _, command := range commands {
				select {
				case command <- snapshotWriteCommand{commit: commit, done: done}:
				case <-writeCtx.Done():
					return writeCtx.Err()
				}
			}
			for range commands {
				select {
				case err := <-done:
					if err != nil {
						return err
					}
				case <-writeCtx.Done():
					return writeCtx.Err()
				}
			}
			if commit && progress != nil && opts.CDCSnapshotProgress != nil {
				defer since(&stats.persist, time.Now())
				if err := d.checkCDCIncarnation(writeCtx, opts.Table, opts.CDCExpectedIncarnation); err != nil {
					return err
				}
				if err := opts.CDCSnapshotProgress(writeCtx, *progress); err != nil {
					return fmt.Errorf("persist committed StarRocks snapshot progress: %w", err)
				}
				progress = nil
			}
			return nil
		}
		for {
			recvStart := time.Now()
			select {
			case <-writeCtx.Done():
				return writeCtx.Err()
			case <-ticker.C:
				start := time.Now()
				stats.commits.Add(1)
				if err := barrier(true); err != nil {
					return err
				}
				since(&stats.commitBarrier, start)
			case result, ok := <-records:
				since(&stats.recvWait, recvStart)
				if !ok {
					return barrier(true)
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
					dispatchStart := time.Now()
					select {
					case jobs <- result:
						since(&stats.dispatchWait, dispatchStart)
					case <-writeCtx.Done():
						result.Batch.Release()
						return writeCtx.Err()
					}
				}
				token, hasToken := result.CommitToken.(source.CDCStateCommitToken)
				if ptr, ok := result.CommitToken.(*source.CDCStateCommitToken); ok && ptr != nil {
					token, hasToken = *ptr, true
				}
				if hasToken && len(token.SnapshotProgress) != 0 && len(token.SnapshotPositions) == 0 {
					progress = &source.CDCStateCommitToken{SnapshotProgress: token.SnapshotProgress}
				}
				if result.CDCSnapshotRangeEnd {
					start := time.Now()
					stats.rangeBarriers.Add(1)
					if err := barrier(false); err != nil {
						return err
					}
					since(&stats.rangeBarrier, start)
				}
			}
		}
	})
	if err := group.Wait(); err != nil {
		return err
	}
	return d.checkCDCIncarnation(ctx, opts.Table, opts.CDCExpectedIncarnation)
}

type snapshotLoadWorker struct {
	dest              *StarRocksDestination
	database          string
	table             string
	opts              destination.WriteOptions
	limits            snapshotWriteLimits
	limiter           *snapshotLoadLimiter
	stats             *snapshotWriteStats
	body              boundedJSONBuffer
	columns           string
	lastRow           time.Time
	tx                *streamLoadTransaction
	replay            [][]byte
	transactionBytes  int
	transactionMemory int
}

func (w *snapshotLoadWorker) run(ctx context.Context, jobs <-chan source.RecordBatchResult, commands <-chan snapshotWriteCommand) (retErr error) {
	w.body.limit = w.limits.requestBytes
	defer func() {
		if w.tx != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if err := w.tx.rollback(cleanupCtx); err != nil && retErr == nil {
				retErr = fmt.Errorf("rollback StarRocks snapshot transaction: %w", err)
			}
		}
	}()
	ticker := time.NewTicker(w.limits.idleInterval)
	defer ticker.Stop()
	for {
		idleStart := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case command := <-commands:
			since(&w.st().idle, idleStart)
			err := w.flush(ctx)
			if err == nil && command.commit {
				start := time.Now()
				err = w.commit(ctx)
				since(&w.st().commit, start)
			}
			command.done <- err
			if err != nil {
				return err
			}
		case <-ticker.C:
			if !w.lastRow.IsZero() && time.Since(w.lastRow) >= w.limits.idleInterval {
				if err := w.flush(ctx); err != nil {
					return err
				}
			}
		case result, ok := <-jobs:
			if !ok {
				return nil
			}
			since(&w.st().idle, idleStart)
			prepStart := time.Now()
			batch, err := prepareCDCSnapshotRecord(result.Batch, w.opts)
			since(&w.st().prepare, prepStart)
			result.Batch.Release()
			if err != nil {
				return err
			}
			err = w.append(ctx, batch)
			batch.Release()
			if err != nil {
				return err
			}
		}
	}
}

func (w *snapshotLoadWorker) append(ctx context.Context, batch arrow.RecordBatch) error {
	encoder := newJSONBatchEncoder(batch, w.limits.requestBytes)
	columns := columnsHeader(batch.Schema(), encoder.binaryColumns())
	if w.columns != "" && w.columns != columns {
		return fmt.Errorf("StarRocks snapshot columns changed while loading %s", w.table)
	}
	w.columns = columns
	for encoder.nextRow < batch.NumRows() {
		if len(w.body.data) == 0 {
			w.body.writeByte('[')
		}
		encodeStart := time.Now()
		if _, err := encoder.appendTo(ctx, &w.body); err != nil {
			return fmt.Errorf("encode StarRocks snapshot batch: %w", err)
		}
		since(&w.st().encode, encodeStart)
		w.lastRow = time.Now()
		if encoder.nextRow < batch.NumRows() || len(w.body.data) >= w.limits.requestTarget {
			if err := w.flush(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *snapshotLoadWorker) flush(ctx context.Context) error {
	if len(w.body.data) <= 1 {
		return nil
	}
	w.body.writeByte(']')
	if w.body.err != nil {
		return w.body.err
	}
	if len(w.replay) != 0 && (w.transactionBytes+len(w.body.data) > w.limits.transactionBytes || w.transactionMemory+cap(w.body.data) > w.limits.replayMemory) {
		if err := w.commit(ctx); err != nil {
			return err
		}
	}
	body := w.body.data
	w.body = boundedJSONBuffer{limit: w.limits.requestBytes}
	w.lastRow = time.Time{}
	w.replay = append(w.replay, body)
	w.transactionBytes += len(body)
	w.transactionMemory += cap(body)
	err := w.withPermit(ctx, func() error {
		if w.tx == nil {
			start := time.Now()
			if err := w.begin(ctx); err != nil {
				return err
			}
			since(&w.st().begin, start)
		}
		start := time.Now()
		defer since(&w.st().load, start)
		w.st().loads.Add(1)
		w.st().loadBytes.Add(int64(len(body)))
		return w.tx.load(ctx, body)
	})
	if err != nil {
		return w.retry(ctx, err, false)
	}
	return nil
}

func (w *snapshotLoadWorker) begin(ctx context.Context) error {
	var err error
	w.tx, err = w.dest.loader.beginTransaction(ctx, w.database, w.table, w.dest.nextLabel(w.table), streamLoadOptions{
		columns: w.columns, mergeCondition: cdcOrderColumn,
	})
	return err
}

func (w *snapshotLoadWorker) commit(ctx context.Context) error {
	if w.tx == nil {
		return nil
	}
	err := w.withPermit(ctx, func() error { return w.tx.commit(ctx) })
	if err != nil {
		if err := w.retry(ctx, err, true); err != nil {
			return err
		}
	}
	config.Debug("[STARROCKS] Committed snapshot transaction %d (label %s) for %s.%s: %d requests, %d bytes",
		w.tx.id, w.tx.label, w.database, w.table, len(w.replay), w.transactionBytes)
	w.tx = nil
	clear(w.replay)
	w.replay = w.replay[:0]
	w.transactionBytes, w.transactionMemory = 0, 0
	return nil
}

func (w *snapshotLoadWorker) retry(ctx context.Context, cause error, commit bool) (retErr error) {
	if !isStreamLoadVersionPressure(cause) {
		return cause
	}
	retryCtx, cancel := context.WithTimeout(ctx, w.limits.retryTimeout)
	defer cancel()
	defer func() {
		if retryCtx.Err() != nil {
			if ctx.Err() != nil {
				retErr = ctx.Err()
			} else {
				retErr = fmt.Errorf("StarRocks snapshot version pressure exceeded %s: %w", w.limits.retryTimeout, errors.Join(cause, retryCtx.Err()))
			}
		}
	}()
	deadline, _ := retryCtx.Deadline()
	delay := w.limits.retryInitial
	for isStreamLoadVersionPressure(cause) {
		w.limiter.reduce()
		if w.tx != nil {
			if err := w.tx.rollback(retryCtx); err != nil {
				return fmt.Errorf("rollback rejected StarRocks snapshot transaction: %w (load error: %v)", err, cause)
			}
			w.tx = nil
		}
		if time.Until(deadline) < delay {
			return fmt.Errorf("StarRocks snapshot version pressure exceeded %s: %w", w.limits.retryTimeout, cause)
		}
		config.Debug("[STARROCKS] Snapshot load for %s hit version pressure; retrying transaction in %s", w.table, delay)
		timer := time.NewTimer(delay)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return retryCtx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, w.limits.retryMaximum)
		cause = w.withPermit(retryCtx, func() error {
			if err := w.begin(retryCtx); err != nil {
				return err
			}
			for _, body := range w.replay {
				if err := w.tx.load(retryCtx, body); err != nil {
					return err
				}
			}
			if commit {
				return w.tx.commit(retryCtx)
			}
			return nil
		})
		if cause == nil {
			return nil
		}
	}
	return cause
}

func (w *snapshotLoadWorker) withPermit(ctx context.Context, operation func() error) error {
	start := time.Now()
	if err := w.limiter.acquire(ctx); err != nil {
		return err
	}
	since(&w.st().permitWait, start)
	defer w.limiter.release()
	return operation()
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

func (w *snapshotLoadWorker) st() *snapshotWriteStats {
	if w.stats == nil {
		w.stats = &snapshotWriteStats{}
	}
	return w.stats
}
