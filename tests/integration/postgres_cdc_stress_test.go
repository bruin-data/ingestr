//go:build stress

// High-volume PostgreSQL CDC accuracy test. It is intentionally excluded from
// the regular integration suite (build tag `stress`, not `integration`) so it
// never slows down CI; run it with `make cdc-postgres-stress-test`.
//
// Scenario: two streaming CDC pipelines replicate from one Postgres container
// into PostgreSQL and DuckDB while parallel workers apply
// ~1000 inserts/updates/deletes per second.
// During the load, tables with pre-existing rows are discovered and one table
// goes through column add/drop/rename, a numeric type change, large JSONB
// updates, primary-key updates, and TRUNCATE followed by inserts in the same
// transaction. Afterwards the destination must converge to the exact source
// rows and active schema, verified by aggregates and canonical row comparison.
package integration

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	stressEvolvingTable   = "stress_evolving"
)

// Overridable for local iteration and heavier soak runs, e.g.
// STRESS_OPS_PER_SEC=5000 STRESS_LOAD_DURATION=5m make cdc-postgres-stress-test.
var (
	stressTargetOpsPerSec = envInt("STRESS_OPS_PER_SEC", 1000)
	stressLoadDuration    = envDuration("STRESS_LOAD_DURATION", 3*time.Minute)
	stressNewTableEvery   = envDuration("STRESS_NEW_TABLE_EVERY", 1*time.Minute)
	stressSchemaEvery     = envDuration("STRESS_SCHEMA_CHANGE_EVERY", 20*time.Second)
	stressLateTables      = envInt("STRESS_LATE_TABLES", 2)
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
	deleteSQL string
}

type stressTruth struct {
	count int64
	sum   string
}

func newStressTable(name string, seeded int64) *stressTable {
	t := &stressTable{
		name:      name,
		insertSQL: fmt.Sprintf(`INSERT INTO public.%s (id, val, payload, updated_at) VALUES ($1, $2, $3, now())`, name),
		updateSQL: fmt.Sprintf(`UPDATE public.%s SET val = val + 1, payload = $2, updated_at = now() WHERE id = $1`, name),
		deleteSQL: fmt.Sprintf(`DELETE FROM public.%s WHERE id = $1`, name),
	}
	t.nextID.Store(seeded)
	return t
}

func stressCreateEvolvingAndSeed(ctx context.Context, pool *pgxpool.Pool, rows int) error {
	_, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%[1]s (
			id BIGINT PRIMARY KEY,
			val BIGINT NOT NULL,
			payload TEXT NOT NULL,
			legacy TEXT,
			updated_at TIMESTAMPTZ NOT NULL
		);
		INSERT INTO public.%[1]s
		SELECT g, g, 'seed-' || g, 'legacy-' || g, now() FROM generate_series(1, %[2]d) g;
	`, stressEvolvingTable, rows))
	return err
}

type stressSchemaPhase struct {
	name string
	sql  string
}

func stressSchemaPhases() []stressSchemaPhase {
	return []stressSchemaPhase{
		{
			name: "add populated segment column",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s ADD COLUMN segment TEXT NOT NULL DEFAULT 'segment-default';
				UPDATE public.%[1]s
				SET segment = 'segment-' || (id %% 7), updated_at = now()
				WHERE id %% 2 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "drop legacy column",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s DROP COLUMN legacy;
				UPDATE public.%[1]s
				SET payload = 'post-drop-' || id, updated_at = now()
				WHERE id %% 13 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "widen bigint to numeric",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s ALTER COLUMN val TYPE NUMERIC(30,0) USING val::numeric;
				UPDATE public.%[1]s
				SET val = val + 1000000000000, updated_at = now()
				WHERE id %% 17 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "widen numeric precision and scale",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s ALTER COLUMN val TYPE NUMERIC(35,4) USING val::numeric;
				UPDATE public.%[1]s
				SET val = val + 0.1250, updated_at = now()
				WHERE id %% 11 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "add and populate large jsonb",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
				UPDATE public.%[1]s
				SET metadata = jsonb_build_object('id', id, 'tags', jsonb_build_array('stress', segment)),
				    payload = repeat('x', 8192) || id,
				    updated_at = now()
				WHERE id %% 19 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "rename segment column",
			sql: fmt.Sprintf(`
				ALTER TABLE public.%[1]s RENAME COLUMN segment TO cohort;
				UPDATE public.%[1]s
				SET val = val + 5, updated_at = now()
				WHERE id %% 19 = 0;
			`, stressEvolvingTable),
		},
		{
			name: "truncate and repopulate transaction",
			sql: fmt.Sprintf(`
				BEGIN;
				TRUNCATE TABLE public.%[1]s;
				INSERT INTO public.%[1]s (id, val, payload, updated_at, cohort, metadata)
				SELECT g, g * 10, 'after-truncate-' || g, now(), 'cohort-' || (g %% 5),
				       jsonb_build_object('phase', 'truncate', 'id', g)
				FROM generate_series(1, 500) g;
				INSERT INTO public.%[1]s (id, val, payload, updated_at, cohort, metadata)
				VALUES (9000000000000, 42, 'pk-sentinel', now(), 'sentinel', '{"stable":true}'::jsonb);
				COMMIT;
			`, stressEvolvingTable),
		},
		{
			name: "primary key and unchanged toast update",
			sql: fmt.Sprintf(`
				UPDATE public.%[1]s
				SET id = 9000000000001, val = val + 1, updated_at = now()
				WHERE id = 9000000000000;
				UPDATE public.%[1]s
				SET val = val + 7, updated_at = now()
				WHERE id %% 23 = 0;
			`, stressEvolvingTable),
		},
	}
}

func stressEventDelay(configured, total time.Duration, events int) time.Duration {
	if events <= 0 {
		return configured
	}
	latestSafe := total / time.Duration(events+1)
	if latestSafe > 0 && configured > latestSafe {
		return latestSafe
	}
	return configured
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

func TestPostgresCDC_StressComplexWorkload(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}

	sourceContainer, sourceConnString := stressSourceContainer(t, ctx)
	destConnString, destPool := stressDestContainer(t, ctx)
	duckDBPath := filepath.Join(t.TempDir(), "postgres-cdc-stress.duckdb")

	srcPool, err := pgxpool.New(ctx, fmt.Sprintf("%s&pool_max_conns=%d", sourceConnString, max(32, stressWorkers+8)))
	require.NoError(t, err)
	t.Cleanup(srcPool.Close)

	tables := &stressTableSet{}
	for i := 0; i < stressInitialTables; i++ {
		name := stressTableName(i)
		require.NoError(t, stressCreateAndSeed(ctx, srcPool, name, stressSeedRows))
		tables.add(newStressTable(name, stressSeedRows))
	}
	require.NoError(t, stressCreateEvolvingAndSeed(ctx, srcPool, stressSeedRows))
	tables.add(newStressTable(stressEvolvingTable, stressSeedRows))
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	nativeConfig := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] +
			"&discover_interval=5s&state_id=postgres-stress-native",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       2 * time.Second,
		FlushRecords:        25000,
		Progress:            config.ProgressLog,
	}
	duckDBConfig := *nativeConfig
	duckDBConfig.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&discover_interval=5s&state_id=postgres-stress-duckdb"
	duckDBConfig.DestURI = "duckdb:///" + duckDBPath

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	type streamSink struct {
		name     string
		config   *config.IngestConfig
		restarts int
	}
	type streamExit struct {
		sink *streamSink
		err  error
	}
	streamSinks := []*streamSink{
		{name: "postgres", config: nativeConfig},
		{name: "duckdb", config: &duckDBConfig},
	}
	streamExits := make(chan streamExit, len(streamSinks)*2)
	startStream := func(sink *streamSink) {
		go func() { streamExits <- streamExit{sink: sink, err: pipeline.New(sink.config).Run(streamCtx)} }()
	}
	restartStreamIfRequired := func(exit streamExit) bool {
		if exit.err == nil || !strings.Contains(exit.err.Error(), "requires restarting the stream before it can be ingested") {
			return false
		}
		exit.sink.restarts++
		t.Logf("restarting %s stream after safe late-table discovery boundary: %v", exit.sink.name, exit.err)
		startStream(exit.sink)
		return true
	}
	for _, sink := range streamSinks {
		startStream(sink)
	}

	ddlPhases := stressSchemaPhases()
	ddlDelay := stressEventDelay(stressSchemaEvery, stressLoadDuration, len(ddlPhases))
	lateTableDelay := stressEventDelay(stressNewTableEvery, stressLoadDuration, stressLateTables)
	t.Logf("load phase: %v at ~%d ops/sec across %d workers, %d late tables every %v, %d schema phases every %v",
		stressLoadDuration, stressTargetOpsPerSec, stressWorkers, stressLateTables, lateTableDelay, len(ddlPhases), ddlDelay)

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()

	var inserts, updates, deletes, opErrors, completedDDL atomic.Int64
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
				switch roll := rng.Intn(100); {
				case roll < 45:
					id := tbl.nextID.Add(1)
					// ctx, not loadCtx: every issued op runs to completion so
					// the post-load source state is settled when workers exit.
					if _, err := srcPool.Exec(ctx, tbl.insertSQL, id, id, fmt.Sprintf("ins-%d-%d", seed, id)); err != nil {
						recordOpErr(fmt.Errorf("insert %s id=%d: %w", tbl.name, id, err))
					} else {
						inserts.Add(1)
					}
				case roll < 85:
					id := rng.Int63n(tbl.nextID.Load()) + 1
					result, err := srcPool.Exec(ctx, tbl.updateSQL, id, fmt.Sprintf("upd-%d-%d", seed, id))
					if err != nil {
						recordOpErr(fmt.Errorf("update %s id=%d: %w", tbl.name, id, err))
					} else {
						updates.Add(result.RowsAffected())
					}
				default:
					id := rng.Int63n(tbl.nextID.Load()) + 1
					result, err := srcPool.Exec(ctx, tbl.deleteSQL, id)
					if err != nil {
						recordOpErr(fmt.Errorf("delete %s id=%d: %w", tbl.name, id, err))
					} else {
						deletes.Add(result.RowsAffected())
					}
				}
			}
		}(int64(w + 1))
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < stressLateTables; i++ {
			select {
			case <-loadCtx.Done():
				return
			case <-time.After(lateTableDelay):
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, phase := range ddlPhases {
			select {
			case <-loadCtx.Done():
				return
			case <-time.After(ddlDelay):
			}
			if _, err := srcPool.Exec(ctx, phase.sql); err != nil {
				recordOpErr(fmt.Errorf("schema phase %q: %w", phase.name, err))
				return
			}
			completedDDL.Add(1)
			t.Logf("completed schema phase: %s", phase.name)
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
		case exit := <-streamExits:
			if restartStreamIfRequired(exit) {
				continue
			}
			stopLoad()
			<-loadDone
			t.Fatalf("%s stream exited during load phase: %v", exit.sink.name, exit.err)
		case <-status.C:
			t.Logf("t=%v: %d inserts, %d updates, %d deletes, %d/%d schema phases, %d op errors",
				time.Since(started).Round(time.Second), inserts.Load(), updates.Load(), deletes.Load(), completedDDL.Load(), len(ddlPhases), opErrors.Load())
		}
	}

	if n := opErrors.Load(); n > 0 {
		t.Fatalf("%d load operations failed, first error: %v", n, firstOpErr.Load())
	}
	require.Equal(t, int64(len(ddlPhases)), completedDDL.Load(), "all schema phases must complete")
	totalOps := inserts.Load() + updates.Load() + deletes.Load()
	achieved := float64(totalOps) / stressLoadDuration.Seconds()
	t.Logf("load complete: %d effective ops (%d inserts, %d updates, %d deletes), %.0f ops/sec achieved", totalOps, inserts.Load(), updates.Load(), deletes.Load(), achieved)
	require.GreaterOrEqual(t, achieved, float64(stressTargetOpsPerSec)/2,
		"load generator could not sustain enough pressure for the test to be meaningful")
	finalTables := tables.snapshot()
	require.Len(t, finalTables, stressInitialTables+1+stressLateTables, "all initial, evolving, and late tables should exist")

	// The source is now quiescent; capture the ground truth.
	truths := make(map[string]stressTruth, len(finalTables))
	for _, tbl := range finalTables {
		var tr stressTruth
		require.NoError(t, srcPool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*), COALESCE(sum(val), 0)::text FROM public.%s`, tbl.name)).Scan(&tr.count, &tr.sum))
		truths[tbl.name] = tr
		t.Logf("source truth %s: count=%d sum=%s", tbl.name, tr.count, tr.sum)
	}
	var duckDB *sql.DB
	withDuckDB := func(fn func(*sql.DB) error) error {
		if duckDB != nil {
			return fn(duckDB)
		}
		return withCDCStressDuckDB(duckDBPath, fn)
	}

	type stressDestination struct {
		name          string
		aggregate     func(string) (stressTruth, error)
		compareAll    func() error
		validate      func() error
		validateState func() error
		softDeleted   func(string) (int64, error)
	}
	destinations := []stressDestination{
		{
			name: "postgres",
			aggregate: func(table string) (stressTruth, error) {
				var tr stressTruth
				err := destPool.QueryRow(ctx,
					fmt.Sprintf(`SELECT count(*), COALESCE(sum(val), 0)::text FROM %s WHERE _cdc_deleted = false`, table)).Scan(&tr.count, &tr.sum)
				return tr, err
			},
			compareAll:    func() error { return stressCompareAll(ctx, srcPool, destPool, finalTables) },
			validate:      func() error { return stressValidateSchemas(ctx, srcPool, destPool, finalTables) },
			validateState: func() error { return stressValidateDestinationState(ctx, destPool, len(finalTables)) },
			softDeleted: func(table string) (int64, error) {
				var deleted int64
				err := destPool.QueryRow(ctx,
					fmt.Sprintf(`SELECT count(*) FROM %s WHERE _cdc_deleted = true`, quoteStressIdentifier(table))).Scan(&deleted)
				return deleted, err
			},
		},
		{
			name: "duckdb",
			aggregate: func(table string) (stressTruth, error) {
				var truth stressTruth
				err := withDuckDB(func(db *sql.DB) error {
					var err error
					truth, err = stressDuckDBTruth(ctx, db, table)
					return err
				})
				return truth, err
			},
			compareAll: func() error {
				return withDuckDB(func(db *sql.DB) error {
					return stressCompareAllDuckDB(ctx, srcPool, db, finalTables)
				})
			},
			validate: func() error {
				return withDuckDB(func(db *sql.DB) error {
					return stressValidateDuckDBSchemas(ctx, srcPool, db, finalTables)
				})
			},
			validateState: func() error {
				return withDuckDB(func(db *sql.DB) error {
					return stressValidateDuckDBState(ctx, db, len(finalTables))
				})
			},
			softDeleted: func(table string) (int64, error) {
				var deleted int64
				err := withDuckDB(func(db *sql.DB) error {
					return db.QueryRowContext(ctx,
						fmt.Sprintf(`SELECT count(*) FROM %s WHERE _cdc_deleted = true`, quoteStressIdentifier(table))).Scan(&deleted)
				})
				return deleted, err
			},
		},
	}

	dumpDiagnostics := func() {
		stressDumpReplicationState(t, ctx, srcPool)
		for _, destination := range destinations {
			for _, tbl := range finalTables {
				got, err := destination.aggregate(tbl.name)
				t.Logf("  %s/%s: want %+v, got %+v (err=%v)", destination.name, tbl.name, truths[tbl.name], got, err)
			}
			if err := destination.compareAll(); err != nil {
				t.Logf("  %s first content mismatch: %v", destination.name, err)
			}
		}
		stressDumpContainerLogs(t, ctx, sourceContainer, 120)
	}

	deadline := time.Now().Add(stressConvergeTimeout)
	lastProgressLog := time.Now()
	for {
		select {
		case exit := <-streamExits:
			if restartStreamIfRequired(exit) {
				continue
			}
			dumpDiagnostics()
			t.Fatalf("%s stream exited during convergence: %v", exit.sink.name, exit.err)
		default:
		}
		pending := ""
		for _, destination := range destinations {
			for _, tbl := range finalTables {
				got, err := destination.aggregate(tbl.name)
				if err != nil || got != truths[tbl.name] {
					pending = fmt.Sprintf("%s/%s: want %+v, got %+v (err=%v)", destination.name, tbl.name, truths[tbl.name], got, err)
					break
				}
			}
			if pending != "" {
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
	t.Log("PostgreSQL and DuckDB destinations converged on count/sum aggregates for all tables")
	for _, sink := range streamSinks {
		require.Positive(t, sink.restarts, "%s workload should exercise the safe restart boundary", sink.name)
	}

	cancelStream()
	stopped := make(map[string]struct{}, len(streamSinks))
	deadline = time.Now().Add(60 * time.Second)
	for len(stopped) < len(streamSinks) {
		select {
		case exit := <-streamExits:
			if exit.err != nil {
				require.ErrorIs(t, exit.err, context.Canceled, "%s stream shutdown", exit.sink.name)
			}
			stopped[exit.sink.name] = struct{}{}
		case <-time.After(time.Until(deadline)):
			t.Fatal("streaming pipelines did not exit within 60s of cancellation")
		}
	}
	duckDB = openCDCStressDuckDB(t, duckDBPath)

	// Aggregates can match while a final merge is still landing payload
	// updates, so retry the deep comparison briefly before declaring failure.
	for _, destination := range destinations {
		var compareErr error
		for attempt := 1; attempt <= 6; attempt++ {
			if compareErr = destination.compareAll(); compareErr == nil {
				break
			}
			t.Logf("%s deep comparison attempt %d: %v", destination.name, attempt, compareErr)
			time.Sleep(5 * time.Second)
		}
		require.NoError(t, compareErr, "%s row-by-row content comparison failed", destination.name)
		t.Logf("%s row-by-row content comparison passed for all tables", destination.name)
		require.NoError(t, destination.validate(), "%s destination schema validation failed", destination.name)
		require.NoError(t, destination.validateState(), "%s destination CDC state validation failed", destination.name)
	}

	for _, destination := range destinations {
		var softDeleted int64
		for _, tbl := range finalTables {
			deleted, err := destination.softDeleted(tbl.name)
			require.NoError(t, err)
			softDeleted += deleted
		}
		require.Positive(t, softDeleted, "%s destination should retain soft-deleted CDC rows outside truncated tables", destination.name)
	}
	require.Positive(t, deletes.Load(), "workload should execute real deletes")
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
	canonical string
}

func stressFetchChunk(ctx context.Context, pool *pgxpool.Pool, table, extraWhere string, columns []string, offset, limit int64) ([]stressRow, error) {
	pairs := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		pairs = append(pairs, quoteStressLiteral(column), quoteStressIdentifier(column))
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(
		`SELECT id, jsonb_build_object(%s)::text FROM %s WHERE true%s ORDER BY id LIMIT $1 OFFSET $2`,
		strings.Join(pairs, ", "), table, extraWhere,
	), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stressRow
	for rows.Next() {
		var r stressRow
		if err := rows.Scan(&r.id, &r.canonical); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func stressCompareChunkRange(ctx context.Context, src, dst *pgxpool.Pool, table string, columns []string, offset, limit int64) error {
	sourceTable := quoteStressIdentifier("public") + "." + quoteStressIdentifier(table)
	destTable := quoteStressIdentifier(table)
	srcRows, err := stressFetchChunk(ctx, src, sourceTable, "", columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: source fetch: %w", table, offset, err)
	}
	dstRows, err := stressFetchChunk(ctx, dst, destTable, " AND _cdc_deleted = false", columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: destination fetch: %w", table, offset, err)
	}
	if len(srcRows) != len(dstRows) {
		return fmt.Errorf("%s offset %d: row count mismatch: source=%d destination=%d", table, offset, len(srcRows), len(dstRows))
	}
	for i := range srcRows {
		s, d := srcRows[i], dstRows[i]
		if s.id != d.id || s.canonical != d.canonical {
			return fmt.Errorf("%s: content mismatch at id=%d: source=%s destination={id:%d row:%s}",
				table, s.id, s.canonical, d.id, d.canonical)
		}
	}
	return nil
}

// stressCompareAll compares every table's full content in parallel, chunked by
// primary-key range so large tables are verified by concurrent scans.
func stressCompareAll(ctx context.Context, src, dst *pgxpool.Pool, tables []*stressTable) error {
	type chunk struct {
		table   string
		columns []string
		offset  int64
	}
	var chunks []chunk
	for _, tbl := range tables {
		columns, err := stressSourceColumns(ctx, src, tbl.name)
		if err != nil {
			return err
		}
		var count int64
		if err := src.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM public.%s`, quoteStressIdentifier(tbl.name))).Scan(&count); err != nil {
			return fmt.Errorf("count source table %s: %w", tbl.name, err)
		}
		for offset := int64(0); offset < count; offset += stressCompareChunk {
			chunks = append(chunks, chunk{table: tbl.name, columns: columns, offset: offset})
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
			if err := stressCompareChunkRange(ctx, src, dst, c.table, c.columns, c.offset, stressCompareChunk); err != nil {
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

func stressDuckDBTruth(ctx context.Context, db *sql.DB, table string) (stressTruth, error) {
	var tr stressTruth
	err := db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT count(*), CAST(COALESCE(sum(val), 0) AS VARCHAR) FROM %s WHERE _cdc_deleted = false`,
		quoteStressIdentifier(table),
	)).Scan(&tr.count, &tr.sum)
	return tr, err
}

func stressFetchDuckDBChunk(
	ctx context.Context,
	db *sql.DB,
	table string,
	columns []string,
	offset, limit int64,
) ([]stressRow, error) {
	pairs := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		pairs = append(pairs, quoteStressLiteral(column), quoteStressIdentifier(column))
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, CAST(json_object(%s) AS VARCHAR)
		 FROM %s WHERE _cdc_deleted = false ORDER BY id LIMIT ? OFFSET ?`,
		strings.Join(pairs, ", "), quoteStressIdentifier(table),
	), limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []stressRow
	for rows.Next() {
		var row stressRow
		var raw string
		if err := rows.Scan(&row.id, &raw); err != nil {
			return nil, err
		}
		row.canonical, err = canonicalizeCDCStressJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("canonicalize DuckDB row %s id=%d: %w", table, row.id, err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func stressCompareChunkRangeDuckDB(
	ctx context.Context,
	src *pgxpool.Pool,
	dst *sql.DB,
	table string,
	columns []string,
	offset, limit int64,
) error {
	sourceTable := quoteStressIdentifier("public") + "." + quoteStressIdentifier(table)
	srcRows, err := stressFetchChunk(ctx, src, sourceTable, "", columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: source fetch: %w", table, offset, err)
	}
	dstRows, err := stressFetchDuckDBChunk(ctx, dst, table, columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: DuckDB fetch: %w", table, offset, err)
	}
	if len(srcRows) != len(dstRows) {
		return fmt.Errorf("%s offset %d: row count mismatch: source=%d DuckDB=%d", table, offset, len(srcRows), len(dstRows))
	}
	for i := range srcRows {
		sourceCanonical, err := canonicalizeCDCStressJSON(srcRows[i].canonical)
		if err != nil {
			return fmt.Errorf("canonicalize source row %s id=%d: %w", table, srcRows[i].id, err)
		}
		if srcRows[i].id != dstRows[i].id || sourceCanonical != dstRows[i].canonical {
			return fmt.Errorf("%s: content mismatch at id=%d: source=%s DuckDB={id:%d row:%s}",
				table, srcRows[i].id, sourceCanonical, dstRows[i].id, dstRows[i].canonical)
		}
	}
	return nil
}

func stressCompareAllDuckDB(ctx context.Context, src *pgxpool.Pool, dst *sql.DB, tables []*stressTable) error {
	type chunk struct {
		table   string
		columns []string
		offset  int64
	}
	var chunks []chunk
	for _, table := range tables {
		columns, err := stressSourceColumns(ctx, src, table.name)
		if err != nil {
			return err
		}
		var count int64
		if err := src.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM public.%s`, quoteStressIdentifier(table.name))).Scan(&count); err != nil {
			return fmt.Errorf("count source table %s: %w", table.name, err)
		}
		for offset := int64(0); offset < count; offset += stressCompareChunk {
			chunks = append(chunks, chunk{table: table.name, columns: columns, offset: offset})
		}
	}

	sem := make(chan struct{}, stressCompareParallel)
	errCh := make(chan error, len(chunks))
	var wg sync.WaitGroup
	for _, item := range chunks {
		wg.Add(1)
		go func(item chunk) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := stressCompareChunkRangeDuckDB(ctx, src, dst, item.table, item.columns, item.offset, stressCompareChunk); err != nil {
				errCh <- err
			}
		}(item)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

func stressDuckDBColumns(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteStressLiteral(table)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, dataType []byte
		var notNull, primaryKey bool
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[string(name)] = strings.ToUpper(string(dataType))
	}
	return columns, rows.Err()
}

func stressValidateDuckDBSchemas(ctx context.Context, src *pgxpool.Pool, dst *sql.DB, tables []*stressTable) error {
	for _, table := range tables {
		sourceColumns, err := stressColumns(ctx, src, table.name)
		if err != nil {
			return fmt.Errorf("source schema %s: %w", table.name, err)
		}
		destColumns, err := stressDuckDBColumns(ctx, dst, table.name)
		if err != nil {
			return fmt.Errorf("DuckDB schema %s: %w", table.name, err)
		}
		for name, sourceColumn := range sourceColumns {
			got, ok := destColumns[name]
			if !ok {
				return fmt.Errorf("DuckDB table %s is missing active source column %s", table.name, name)
			}
			switch sourceColumn.udt {
			case "numeric":
				want := fmt.Sprintf("DECIMAL(%d,%d)", sourceColumn.precision, sourceColumn.scale)
				if got != want {
					return fmt.Errorf("DuckDB table %s column %s type mismatch: got %s want %s", table.name, name, got, want)
				}
			case "jsonb":
				if got != "JSON" {
					return fmt.Errorf("DuckDB table %s column %s type mismatch: got %s want JSON", table.name, name, got)
				}
			}
		}
		if table.name == stressEvolvingTable {
			for _, removed := range []string{"legacy", "segment"} {
				if _, retained := destColumns[removed]; !retained {
					return fmt.Errorf("soft-removed DuckDB column %s is missing on %s", removed, table.name)
				}
			}
		}
	}
	return nil
}

func stressValidateDuckDBState(ctx context.Context, db *sql.DB, tableCount int) error {
	var stateTables int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = '_bruin_staging' AND table_name = 'cdc_state'`).Scan(&stateTables); err != nil {
		return err
	}
	if stateTables != 1 {
		return fmt.Errorf("unexpected DuckDB shared CDC state table count: got %d want 1", stateTables)
	}

	var checkpointRows, completedTables int
	if err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE state_kind = 'checkpoint' AND state_status = 'complete'),
			COUNT(DISTINCT source_table) FILTER (WHERE state_kind = 'snapshot' AND state_status = 'complete')
		FROM "_bruin_staging"."cdc_state"`).Scan(&checkpointRows, &completedTables); err != nil {
		return err
	}
	if checkpointRows == 0 || completedTables != tableCount {
		return fmt.Errorf("unexpected DuckDB shared CDC state: checkpoints=%d completed tables=%d want checkpoints>0 tables=%d", checkpointRows, completedTables, tableCount)
	}
	return nil
}

func stressSourceColumns(ctx context.Context, pool *pgxpool.Pool, table string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("list source columns for %s: %w", table, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("source table %s has no columns", table)
	}
	return columns, nil
}

type stressColumn struct {
	udt       string
	precision int
	scale     int
}

func stressColumns(ctx context.Context, pool *pgxpool.Pool, table string) (map[string]stressColumn, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name, udt_name, COALESCE(numeric_precision, -1), COALESCE(numeric_scale, -1)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]stressColumn)
	for rows.Next() {
		var name string
		var column stressColumn
		if err := rows.Scan(&name, &column.udt, &column.precision, &column.scale); err != nil {
			return nil, err
		}
		columns[name] = column
	}
	return columns, rows.Err()
}

func stressValidateSchemas(ctx context.Context, src, dst *pgxpool.Pool, tables []*stressTable) error {
	for _, table := range tables {
		sourceColumns, err := stressColumns(ctx, src, table.name)
		if err != nil {
			return fmt.Errorf("source schema %s: %w", table.name, err)
		}
		destColumns, err := stressColumns(ctx, dst, table.name)
		if err != nil {
			return fmt.Errorf("destination schema %s: %w", table.name, err)
		}
		for name, want := range sourceColumns {
			got, ok := destColumns[name]
			if !ok {
				return fmt.Errorf("destination table %s is missing active source column %s", table.name, name)
			}
			if got != want {
				return fmt.Errorf("destination table %s column %s type mismatch: source=%+v destination=%+v", table.name, name, want, got)
			}
		}
		if table.name == stressEvolvingTable {
			for _, removed := range []string{"legacy", "segment"} {
				if _, exists := sourceColumns[removed]; exists {
					return fmt.Errorf("removed source column %s still exists on %s", removed, table.name)
				}
				if _, retained := destColumns[removed]; !retained {
					return fmt.Errorf("soft-removed destination column %s is missing on %s", removed, table.name)
				}
			}
		}
	}
	return nil
}

func stressValidateDestinationState(ctx context.Context, dst *pgxpool.Pool, tableCount int) error {
	var stateTables int
	if err := dst.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = '_bruin_staging' AND table_name = 'cdc_state'`).Scan(&stateTables); err != nil {
		return err
	}
	if stateTables != 1 {
		return fmt.Errorf("unexpected shared CDC state table count: got %d want 1", stateTables)
	}

	stateTable := quoteStressIdentifier("_bruin_staging") + "." + quoteStressIdentifier("cdc_state")
	var checkpointRows, completedTables int
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE state_kind = 'checkpoint' AND state_status = 'complete'),
			COUNT(DISTINCT source_table) FILTER (WHERE state_kind = 'snapshot' AND state_status = 'complete')
		FROM %s`, stateTable)
	if err := dst.QueryRow(ctx, query).Scan(&checkpointRows, &completedTables); err != nil {
		return err
	}
	if checkpointRows == 0 || completedTables != tableCount {
		return fmt.Errorf("unexpected shared CDC state: checkpoints=%d completed tables=%d want checkpoints>0 tables=%d", checkpointRows, completedTables, tableCount)
	}
	return nil
}

func quoteStressIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteStressLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
