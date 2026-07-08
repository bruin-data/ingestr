//go:build stress

// High-volume PostgreSQL CDC accuracy test. It is intentionally excluded from
// the regular integration suite (build tag `stress`, not `integration`) so it
// never slows down CI; run it with `make cdc-postgres-stress-test`.
//
// Scenario: a streaming CDC pipeline replicates from one Postgres container to
// another while parallel workers apply ~1000 inserts/updates per second for
// three minutes, and a new table (with pre-existing rows) is created on the
// source every minute mid-stream. Afterwards the destination must converge to
// the exact source state, verified first by per-table count/sum aggregates and
// then by a parallel row-by-row content comparison.
package integration

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	stressInitialTables   = 2
	stressSeedRows        = 5000
	stressLateSeedRows    = 2000
	stressCompareChunk    = 20000
	stressCompareParallel = 8
	stressConvergeTimeout = 4 * time.Minute
)

// Overridable for local iteration and heavier soak runs, e.g.
// STRESS_OPS_PER_SEC=5000 STRESS_LOAD_DURATION=5m make cdc-postgres-stress-test.
var (
	stressTargetOpsPerSec = envInt("STRESS_OPS_PER_SEC", 1000)
	stressLoadDuration    = envDuration("STRESS_LOAD_DURATION", 3*time.Minute)
	stressNewTableEvery   = envDuration("STRESS_NEW_TABLE_EVERY", 1*time.Minute)
	stressWorkers         = envInt("STRESS_WORKERS", 12)
)

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

type stressTable struct {
	name      string
	nextID    atomic.Int64
	insertSQL string
	updateSQL string
}

func newStressTable(name string, seeded int64) *stressTable {
	t := &stressTable{
		name:      name,
		insertSQL: fmt.Sprintf(`INSERT INTO public.%s (id, val, payload, updated_at) VALUES ($1, $2, $3, now())`, name),
		updateSQL: fmt.Sprintf(`UPDATE public.%s SET val = val + 1, payload = $2, updated_at = now() WHERE id = $1`, name),
	}
	t.nextID.Store(seeded)
	return t
}

type stressTableSet struct {
	mu     sync.RWMutex
	tables []*stressTable
}

func (s *stressTableSet) add(t *stressTable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables = append(s.tables, t)
}

func (s *stressTableSet) pick(rng *rand.Rand) *stressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tables[rng.Intn(len(s.tables))]
}

func (s *stressTableSet) snapshot() []*stressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*stressTable, len(s.tables))
	copy(out, s.tables)
	return out
}

func stressTableName(i int) string {
	return fmt.Sprintf("stress_%02d", i)
}

func stressCreateAndSeed(ctx context.Context, pool *pgxpool.Pool, name string, rows int) error {
	_, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%[1]s (
			id BIGINT PRIMARY KEY,
			val BIGINT NOT NULL,
			payload TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		INSERT INTO public.%[1]s
		SELECT g, g, 'seed-' || g, now() FROM generate_series(1, %[2]d) g;
	`, name, rows))
	return err
}

func stressSourceContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image: "postgres:16-alpine",
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		ExposedPorts: []string{"5432/tcp"},
		Cmd: []string{
			"postgres",
			"-c", "wal_level=logical",
			"-c", "max_replication_slots=10",
			"-c", "max_wal_senders=10",
			"-c", "max_connections=120",
			// Throwaway load-generator container: skip the per-commit WAL fsync
			// wait so single-row autocommit transactions aren't capped by disk
			// flush latency. Does not change logical decoding semantics.
			"-c", "synchronous_commit=off",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)
	return container, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
}

func stressDestContainer(t *testing.T, ctx context.Context) (string, *pgxpool.Pool) {
	t.Helper()
	container, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return connString, pool
}

func TestPostgresCDC_StressHighVolume(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}

	sourceContainer, sourceConnString := stressSourceContainer(t, ctx)
	destConnString, destPool := stressDestContainer(t, ctx)

	srcPool, err := pgxpool.New(ctx, fmt.Sprintf("%s&pool_max_conns=%d", sourceConnString, max(32, stressWorkers+8)))
	require.NoError(t, err)
	t.Cleanup(srcPool.Close)

	tables := &stressTableSet{}
	for i := 0; i < stressInitialTables; i++ {
		name := stressTableName(i)
		require.NoError(t, stressCreateAndSeed(ctx, srcPool, name, stressSeedRows))
		tables.add(newStressTable(name, stressSeedRows))
	}
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=5s",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       2 * time.Second,
		FlushRecords:        25000,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	t.Logf("load phase: %v at ~%d ops/sec across %d workers, new table every %v",
		stressLoadDuration, stressTargetOpsPerSec, stressWorkers, stressNewTableEvery)

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()

	var inserts, updates, opErrors atomic.Int64
	var firstOpErr atomic.Value
	recordOpErr := func(err error) {
		opErrors.Add(1)
		firstOpErr.CompareAndSwap(nil, err)
	}

	// Each worker paces itself at target/workers so the aggregate rate scales
	// past what a single shared ticker channel can hand out.
	workerInterval := time.Duration(stressWorkers) * time.Second / time.Duration(stressTargetOpsPerSec)

	var wg sync.WaitGroup
	for w := 0; w < stressWorkers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			ticker := time.NewTicker(workerInterval)
			defer ticker.Stop()
			for {
				select {
				case <-loadCtx.Done():
					return
				case <-ticker.C:
				}
				tbl := tables.pick(rng)
				if rng.Intn(2) == 0 {
					id := tbl.nextID.Add(1)
					// ctx, not loadCtx: every issued op runs to completion so
					// the post-load source state is settled when workers exit.
					if _, err := srcPool.Exec(ctx, tbl.insertSQL, id, id, fmt.Sprintf("ins-%d-%d", seed, id)); err != nil {
						recordOpErr(fmt.Errorf("insert %s id=%d: %w", tbl.name, id, err))
					}
					inserts.Add(1)
				} else {
					id := rng.Int63n(tbl.nextID.Load()) + 1
					if _, err := srcPool.Exec(ctx, tbl.updateSQL, id, fmt.Sprintf("upd-%d-%d", seed, id)); err != nil {
						recordOpErr(fmt.Errorf("update %s id=%d: %w", tbl.name, id, err))
					}
					updates.Add(1)
				}
			}
		}(int64(w + 1))
	}

	lateTables := max(int(stressLoadDuration/stressNewTableEvery)-1, 0)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < lateTables; i++ {
			select {
			case <-loadCtx.Done():
				return
			case <-time.After(stressNewTableEvery):
			}
			name := stressTableName(stressInitialTables + i)
			if err := stressCreateAndSeed(ctx, srcPool, name, stressLateSeedRows); err != nil {
				recordOpErr(fmt.Errorf("create late table %s: %w", name, err))
				return
			}
			tables.add(newStressTable(name, stressLateSeedRows))
			t.Logf("created new table %s mid-stream with %d pre-existing rows", name, stressLateSeedRows)
		}
	}()

	loadDone := make(chan struct{})
	go func() { wg.Wait(); close(loadDone) }()
	status := time.NewTicker(15 * time.Second)
	defer status.Stop()
	started := time.Now()
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case err := <-runErr:
			stopLoad()
			<-loadDone
			t.Fatalf("stream exited during load phase: %v", err)
		case <-status.C:
			t.Logf("t=%v: %d inserts, %d updates, %d op errors",
				time.Since(started).Round(time.Second), inserts.Load(), updates.Load(), opErrors.Load())
		}
	}

	if n := opErrors.Load(); n > 0 {
		t.Fatalf("%d load operations failed, first error: %v", n, firstOpErr.Load())
	}
	totalOps := inserts.Load() + updates.Load()
	achieved := float64(totalOps) / stressLoadDuration.Seconds()
	t.Logf("load complete: %d ops (%d inserts, %d updates), %.0f ops/sec achieved", totalOps, inserts.Load(), updates.Load(), achieved)
	require.GreaterOrEqual(t, achieved, float64(stressTargetOpsPerSec)/2,
		"load generator could not sustain enough pressure for the test to be meaningful")
	finalTables := tables.snapshot()
	require.Len(t, finalTables, stressInitialTables+lateTables, "all late tables should have been created")

	// The source is now quiescent; capture the ground truth.
	type truth struct{ count, sum int64 }
	truths := make(map[string]truth, len(finalTables))
	for _, tbl := range finalTables {
		var tr truth
		require.NoError(t, srcPool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*), COALESCE(sum(val), 0) FROM public.%s`, tbl.name)).Scan(&tr.count, &tr.sum))
		truths[tbl.name] = tr
		t.Logf("source truth %s: count=%d sum=%d", tbl.name, tr.count, tr.sum)
	}

	destAgg := func(table string) (truth, error) {
		var tr truth
		err := destPool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*), COALESCE(sum(val), 0) FROM %s WHERE _cdc_deleted = false`, table)).Scan(&tr.count, &tr.sum)
		return tr, err
	}

	dumpDiagnostics := func() {
		stressDumpReplicationState(t, ctx, srcPool)
		for _, tbl := range finalTables {
			got, err := destAgg(tbl.name)
			t.Logf("  %s: want %+v, got %+v (err=%v)", tbl.name, truths[tbl.name], got, err)
		}
		if err := stressCompareAll(ctx, srcPool, destPool, finalTables); err != nil {
			t.Logf("  first content mismatch: %v", err)
		}
		stressDumpContainerLogs(t, ctx, sourceContainer, 120)
	}

	deadline := time.Now().Add(stressConvergeTimeout)
	lastProgressLog := time.Now()
	for {
		select {
		case err := <-runErr:
			dumpDiagnostics()
			t.Fatalf("stream exited during convergence: %v", err)
		default:
		}
		pending := ""
		for _, tbl := range finalTables {
			got, err := destAgg(tbl.name)
			if err != nil || got != truths[tbl.name] {
				pending = fmt.Sprintf("%s: want %+v, got %+v (err=%v)", tbl.name, truths[tbl.name], got, err)
				break
			}
		}
		if pending == "" {
			break
		}
		if time.Since(lastProgressLog) > 20*time.Second {
			lastProgressLog = time.Now()
			t.Logf("convergence pending: %s", pending)
		}
		if time.Now().After(deadline) {
			dumpDiagnostics()
			t.Fatalf("destination did not converge within %v; still pending: %s", stressConvergeTimeout, pending)
		}
		time.Sleep(2 * time.Second)
	}
	t.Log("destination converged on count/sum aggregates for all tables")

	// Aggregates can match while a final merge is still landing payload
	// updates, so retry the deep comparison briefly before declaring failure.
	var compareErr error
	for attempt := 1; attempt <= 6; attempt++ {
		if compareErr = stressCompareAll(ctx, srcPool, destPool, finalTables); compareErr == nil {
			break
		}
		t.Logf("deep comparison attempt %d: %v", attempt, compareErr)
		time.Sleep(5 * time.Second)
	}
	require.NoError(t, compareErr, "row-by-row content comparison failed")
	t.Log("row-by-row content comparison passed for all tables")

	for _, tbl := range finalTables {
		var deleted int
		require.NoError(t, destPool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE _cdc_deleted = true`, tbl.name)).Scan(&deleted))
		require.Zero(t, deleted, "no deletes were issued, %s should have no soft-deleted rows", tbl.name)
	}

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("streaming pipeline did not exit within 60s of cancellation")
	}
}

func stressDumpReplicationState(t *testing.T, ctx context.Context, srcPool *pgxpool.Pool) {
	t.Helper()
	rows, err := srcPool.Query(ctx, `
		SELECT slot_name, active, COALESCE(active_pid, 0),
		       COALESCE(restart_lsn::text, '-'), COALESCE(confirmed_flush_lsn::text, '-'),
		       pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)
		FROM pg_replication_slots`)
	if err != nil {
		t.Logf("replication slot dump failed: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, restart, confirmed string
		var active bool
		var pid int
		var lag int64
		if err := rows.Scan(&name, &active, &pid, &restart, &confirmed, &lag); err == nil {
			t.Logf("  slot %s: active=%t pid=%d restart=%s confirmed=%s lag=%d bytes", name, active, pid, restart, confirmed, lag)
		}
	}

	var walLSN string
	if err := srcPool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&walLSN); err == nil {
		t.Logf("  pg_current_wal_lsn=%s", walLSN)
	}
}

func stressDumpContainerLogs(t *testing.T, ctx context.Context, container testcontainers.Container, lastN int) {
	t.Helper()
	reader, err := container.Logs(ctx)
	if err != nil {
		t.Logf("container log fetch failed: %v", err)
		return
	}
	defer func() { _ = reader.Close() }()
	var lines []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > lastN {
			lines = lines[1:]
		}
	}
	t.Logf("source container logs (last %d lines):", len(lines))
	for _, l := range lines {
		t.Logf("  %s", l)
	}
}

type stressRow struct {
	id        int64
	val       int64
	payload   string
	updatedAt time.Time
}

func stressFetchChunk(ctx context.Context, pool *pgxpool.Pool, table, extraWhere string, lo, hi int64) ([]stressRow, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT id, val, payload, updated_at FROM %s WHERE id >= $1 AND id < $2%s ORDER BY id`, table, extraWhere,
	), lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stressRow
	for rows.Next() {
		var r stressRow
		if err := rows.Scan(&r.id, &r.val, &r.payload, &r.updatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func stressCompareChunkRange(ctx context.Context, src, dst *pgxpool.Pool, table string, lo, hi int64) error {
	srcRows, err := stressFetchChunk(ctx, src, "public."+table, "", lo, hi)
	if err != nil {
		return fmt.Errorf("%s [%d,%d): source fetch: %w", table, lo, hi, err)
	}
	dstRows, err := stressFetchChunk(ctx, dst, table, " AND _cdc_deleted = false", lo, hi)
	if err != nil {
		return fmt.Errorf("%s [%d,%d): destination fetch: %w", table, lo, hi, err)
	}
	if len(srcRows) != len(dstRows) {
		return fmt.Errorf("%s [%d,%d): row count mismatch: source=%d destination=%d", table, lo, hi, len(srcRows), len(dstRows))
	}
	for i := range srcRows {
		s, d := srcRows[i], dstRows[i]
		if s.id != d.id || s.val != d.val || s.payload != d.payload || !s.updatedAt.Equal(d.updatedAt) {
			return fmt.Errorf("%s: content mismatch at id=%d: source={val:%d payload:%q updated_at:%v} destination={id:%d val:%d payload:%q updated_at:%v}",
				table, s.id, s.val, s.payload, s.updatedAt, d.id, d.val, d.payload, d.updatedAt)
		}
	}
	return nil
}

// stressCompareAll compares every table's full content in parallel, chunked by
// primary-key range so large tables are verified by concurrent scans.
func stressCompareAll(ctx context.Context, src, dst *pgxpool.Pool, tables []*stressTable) error {
	type chunk struct {
		table  string
		lo, hi int64
	}
	var chunks []chunk
	for _, tbl := range tables {
		maxID := tbl.nextID.Load()
		for lo := int64(1); lo <= maxID; lo += stressCompareChunk {
			chunks = append(chunks, chunk{table: tbl.name, lo: lo, hi: min(lo+stressCompareChunk, maxID+1)})
		}
	}

	sem := make(chan struct{}, stressCompareParallel)
	errCh := make(chan error, len(chunks))
	var wg sync.WaitGroup
	for _, c := range chunks {
		wg.Add(1)
		go func(c chunk) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := stressCompareChunkRange(ctx, src, dst, c.table, c.lo, c.hi); err != nil {
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}
