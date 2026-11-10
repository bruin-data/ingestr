package postgres_cdc

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A budget is shared by all tables using an export. The planning connection
// becomes a worker, so no importer waits at a connection barrier.
type snapshotReadBudget struct {
	slots chan struct{}
}

func newSnapshotReadBudget(pool *pgxpool.Pool, parallelism int) *snapshotReadBudget {
	if parallelism <= 0 {
		parallelism = 4
	}
	return &snapshotReadBudget{slots: make(chan struct{}, min(parallelism, int(pool.Config().MaxConns)))}
}

func (b *snapshotReadBudget) acquire(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	select {
	case b.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		<-b.slots
		return nil, err
	}
	return conn, nil
}

func (b *snapshotReadBudget) release(conn *pgxpool.Conn) {
	conn.Release()
	<-b.slots
}

type boundedSnapshotOptions struct {
	normalSnapshot bool
	name           string
	boundary       pglogrepl.LSN
	after          []string
	upper          []string
	ranges         []snapshotRange
	read           source.ReadOptions
	budget         *snapshotReadBudget
	started        func() error
	progress       func(context.Context, []snapshotRange) error
}

type snapshotRangeResult struct {
	normalSnapshot bool
	index          int
	batch          arrow.RecordBatch
	lastKey        []string
	done           bool
}

func cloneSnapshotRanges(ranges []snapshotRange) []snapshotRange {
	copy := slices.Clone(ranges)
	for i := range copy {
		copy[i].After = slices.Clone(copy[i].After)
		copy[i].Upper = slices.Clone(copy[i].Upper)
		copy[i].LastKey = slices.Clone(copy[i].LastKey)
	}
	return copy
}

func importSnapshot(ctx context.Context, conn *pgxpool.Conn, name string) (pgx.Tx, error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+strings.ReplaceAll(name, "'", "''")+"'"); err != nil {
		rollbackSnapshot(tx)
		return nil, fmt.Errorf("import snapshot: %w", err)
	}
	return tx, nil
}

func rollbackSnapshot(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// readBoundedSnapshot consumes a caller-owned export, without creating slots,
// advancing WAL, or emitting table completion. Its coordinator alone advances
// progress, after forwarding each covered batch.
func (s *Snapshot) readBoundedSnapshot(ctx context.Context, opts boundedSnapshotOptions, results chan<- source.RecordBatchResult) error {
	if opts.ranges != nil && len(s.progressKeys) == 0 {
		return fmt.Errorf("saved snapshot ranges require non-null unique keys")
	}
	if len(s.progressKeys) > 0 || opts.after != nil || opts.upper != nil {
		if err := validateSnapshotChunkBounds(s.progressKeys, opts.after, opts.upper); err != nil {
			return err
		}
	}
	if opts.budget == nil {
		opts.budget = newSnapshotReadBudget(s.source.queryPool, opts.read.Parallelism)
	}
	planStart := time.Now()
	conn, err := opts.budget.acquire(ctx, s.source.queryPool)
	if err != nil {
		return fmt.Errorf("acquire snapshot planning connection: %w", err)
	}
	defer opts.budget.release(conn)
	tx, err := importSnapshot(ctx, conn, opts.name)
	if err != nil {
		return err
	}
	defer rollbackSnapshot(tx)
	if _, err := tx.Exec(ctx, "LOCK TABLE "+quoteTableName(s.tableName)+" IN ACCESS SHARE MODE"); err != nil {
		return fmt.Errorf("lock snapshot table: %w", err)
	}
	if err := s.validateProgressIdentity(ctx, tx); err != nil {
		return err
	}
	ranges, keyTypes, err := s.prepareSnapshotRanges(ctx, tx, opts)
	if err != nil {
		return err
	}
	columns := s.tableSchema.Columns[:sourceColumnCount(s.tableSchema)]
	projection := make([]string, len(columns))
	for i, col := range columns {
		projection[i] = quoteIdentifier(col.Name)
	}
	if _, err := tx.Conn().PgConn().Prepare(ctx, "", "SELECT "+strings.Join(projection, ", ")+" FROM "+quoteTableName(s.tableName), nil); err != nil {
		return fmt.Errorf("describe snapshot projection: %w", err)
	}
	planTime := time.Since(planStart)
	if opts.started != nil {
		if err := opts.started(); err != nil {
			return err
		}
	}
	if opts.progress != nil && len(s.progressKeys) > 0 {
		if err := opts.progress(ctx, cloneSnapshotRanges(ranges)); err != nil {
			return err
		}
	}

	var imports atomic.Int64
	readStart := time.Now()
	progress := opts.progress
	if len(s.progressKeys) == 0 {
		progress = nil
	}
	admitCtx, stopAdmission := context.WithCancel(ctx)
	defer stopAdmission()
	var readers atomic.Int64
	rows, blocked, err := coordinateSnapshotWorkers(ctx, ranges, cap(opts.budget.slots), results, progress,
		func(ctx context.Context, workerID int, jobs <-chan int, updates chan<- snapshotRangeResult) error {
			worker := *s
			workerTx := tx
			if workerID == 0 {
				defer stopAdmission()
			} else {
				// Claim a connection before a job. When other tables occupy the
				// budget, their planning workers can still drain their own jobs.
				select {
				case opts.budget.slots <- struct{}{}:
				default:
					return nil
				}
				acquireCtx, cancelAcquire := context.WithCancel(ctx)
				stop := context.AfterFunc(admitCtx, cancelAcquire)
				conn, err := s.source.queryPool.Acquire(acquireCtx)
				stop()
				cancelAcquire()
				if err != nil {
					<-opts.budget.slots
					if admitCtx.Err() != nil && ctx.Err() == nil {
						return nil
					}
					return fmt.Errorf("acquire snapshot worker connection: %w", err)
				}
				defer opts.budget.release(conn)
				if len(jobs) == 0 {
					return nil
				}
				importStart := time.Now()
				workerTx, err = importSnapshot(ctx, conn, opts.name)
				imports.Add(int64(time.Since(importStart)))
				if err != nil {
					return err
				}
				defer rollbackSnapshot(workerTx)
				// A queued exclusive DDL lock must not make a late reader wait
				// behind a writer that is itself waiting for the planning reader.
				if _, err := workerTx.Exec(ctx, "LOCK TABLE "+quoteTableName(s.tableName)+" IN ACCESS SHARE MODE NOWAIT"); err != nil {
					return fmt.Errorf("lock snapshot worker table: %w", err)
				}
			}
			readers.Add(1)
			for index := range jobs {
				if ctx.Err() != nil {
					return context.Cause(ctx)
				}
				if err := worker.readSnapshotRange(ctx, workerTx, opts, keyTypes, index, ranges[index], updates); err != nil {
					return err
				}
			}
			return nil
		})
	config.Debug("[CDC] Snapshot read table=%s readers=%d ranges=%d rows=%d planning=%s imports_sum=%s read=%s forward_wait=%s",
		s.tableName, readers.Load(), len(ranges), rows, planTime, time.Duration(imports.Load()), time.Since(readStart), blocked)
	return err
}

func coordinateSnapshotWorkers(ctx context.Context, plan []snapshotRange, concurrency int, results chan<- source.RecordBatchResult,
	progress func(context.Context, []snapshotRange) error,
	run func(context.Context, int, <-chan int, chan<- snapshotRangeResult) error,
) (int64, time.Duration, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	ranges := cloneSnapshotRanges(plan)
	updates := make(chan snapshotRangeResult)
	jobs := make(chan int, len(plan))
	for i, r := range plan {
		if !r.Complete {
			jobs <- i
		}
	}
	close(jobs)
	var wg sync.WaitGroup
	for workerID := range min(max(concurrency, 1), len(jobs)) {
		wg.Go(func() {
			if err := run(ctx, workerID, jobs, updates); err != nil {
				cancel(err)
			}
		})
	}
	go func() {
		wg.Wait()
		close(updates)
	}()
	var rows int64
	var blocked time.Duration
	var forwardErr error
	for update := range updates {
		if forwardErr != nil || ctx.Err() != nil {
			if update.batch != nil {
				update.batch.Release()
			}
			continue
		}
		if update.batch != nil {
			rows += update.batch.NumRows()
			start := time.Now()
			forwardErr = sendResult(ctx, results, source.RecordBatchResult{Batch: update.batch, CDCSnapshot: update.normalSnapshot})
			blocked += time.Since(start)
			if forwardErr != nil {
				cancel(forwardErr)
				continue
			}
		}
		if update.lastKey != nil {
			ranges[update.index].LastKey = slices.Clone(update.lastKey)
		}
		ranges[update.index].Complete = update.done
		if progress != nil {
			if err := progress(ctx, cloneSnapshotRanges(ranges)); err != nil {
				forwardErr = err
				cancel(err)
			}
		}
	}
	return rows, blocked, context.Cause(ctx)
}

func (s *Snapshot) prepareSnapshotRanges(ctx context.Context, tx pgx.Tx, opts boundedSnapshotOptions) ([]snapshotRange, []string, error) {
	if len(s.progressKeys) > 0 {
		supported, err := snapshotRangeKeysSupported(ctx, tx, s.tableName, s.progressKeys)
		if err != nil {
			return nil, nil, err
		}
		if !supported {
			if opts.ranges != nil || opts.after != nil || opts.upper != nil {
				return nil, nil, fmt.Errorf("snapshot bounds for %s require supported non-null unique keys", s.tableName)
			}
			config.Debug("[CDC] Snapshot of %s uses a serial scan: keys lack a supported unique ordering", s.tableName)
			s.progressKeys = nil
		}
	}
	if len(s.progressKeys) == 0 {
		return []snapshotRange{{ID: "0"}}, nil, nil
	}
	keyTypes, err := snapshotKeyTypes(ctx, tx, s.tableName, s.progressKeys)
	if err != nil {
		return nil, nil, err
	}
	if opts.ranges != nil {
		if err := validateSnapshotRanges(ctx, tx, s.tableName, s.progressKeys, keyTypes, opts.after, opts.upper, opts.ranges); err != nil {
			return nil, nil, err
		}
		return cloneSnapshotRanges(opts.ranges), keyTypes, nil
	}
	ranges, err := planSnapshotRanges(ctx, tx, s.tableName, s.progressKeys, keyTypes, opts.after, opts.upper, cap(opts.budget.slots))
	return ranges, keyTypes, err
}

func (s *Snapshot) readSnapshotRange(ctx context.Context, tx pgx.Tx, opts boundedSnapshotOptions, keyTypes []string, index int, r snapshotRange, updates chan<- snapshotRangeResult) (retErr error) {
	start := time.Now()
	var totalRows int64
	defer func() {
		config.Debug("[CDC] Snapshot range table=%s range=%s rows=%d elapsed=%s success=%t", s.tableName, r.ID, totalRows, time.Since(start), retErr == nil)
	}()
	columns := s.tableSchema.Columns[:sourceColumnCount(s.tableSchema)]
	names := make([]string, len(columns))
	for i, col := range columns {
		names[i] = quoteIdentifier(col.Name)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(names, ", "), quoteTableName(s.tableName))
	var args []any
	if len(s.progressKeys) > 0 {
		after := r.After
		if r.LastKey != nil {
			after = r.LastKey
		}
		query, args = boundedSnapshotQuery(names, quoteTableName(s.tableName), s.progressKeys, keyTypes, after, r.Upper)
	}
	description, err := tx.Conn().PgConn().Prepare(ctx, "", query, nil)
	if err != nil {
		return fmt.Errorf("describe snapshot range: %w", err)
	}
	s.typeMap = tx.Conn().TypeMap()
	formats := postgres.TextResultFormats(s.typeMap, description.Fields, columns, snapshotTextColumn)
	for i := len(columns); i < len(formats); i++ {
		formats[i] = pgx.TextFormatCode
	}
	rows, err := tx.Query(ctx, query, append([]any{pgx.QueryExecModeDescribeExec, formats}, args...)...)
	if err != nil {
		return fmt.Errorf("query snapshot range %s: %w", r.ID, err)
	}
	defer rows.Close()
	batchSize := opts.read.PageSize
	if batchSize <= 0 {
		batchSize = 100000
	}
	arrowSchema := buildArrowSchema(s.tableSchema.Columns)
	syncedAt := time.Now().UTC()
	for {
		batch, count, lastKey, err := s.rowsToBatch(rows, arrowSchema, columns, len(s.progressKeys), batchSize, opts.read.MaxBatchBytes, FormatLSN(opts.boundary), syncedAt)
		if err != nil {
			return fmt.Errorf("decode snapshot range %s: %w", r.ID, err)
		}
		totalRows += count
		update := snapshotRangeResult{index: index, batch: batch, lastKey: lastKey, done: count == 0, normalSnapshot: opts.normalSnapshot}
		select {
		case updates <- update:
		case <-ctx.Done():
			if batch != nil {
				batch.Release()
			}
			return context.Cause(ctx)
		}
		if update.done {
			return nil
		}
	}
}
