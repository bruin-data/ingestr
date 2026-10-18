//go:build stress

// PostgreSQL CDC → StarRocks soak test. The shared queue-based load generator
// drives inserts/updates/deletes/PK moves, late tables and schema phases (the
// same workload as TestPostgresCDC_StressComplexWorkload) while the real
// ingestr binary streams into StarRocks and is repeatedly killed with SIGKILL
// or SIGTERM and restarted. After the load, the slot must acknowledge the
// final source LSN and StarRocks must hold exactly the source rows, compared
// value by value.
//
// Run with e.g.:
//
//	make build && STRESS_LOAD_DURATION=30m go test -tags stress -count=1 -v -timeout 90m \
//	  -run '^TestPostgresCDC_StressStarRocks$' ./tests/integration/
package integration

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	srStressKillEvery     = envDuration("STRESS_KILL_EVERY", 150*time.Second)
	srStressConverge      = envDuration("STRESS_CONVERGE_TIMEOUT", 20*time.Minute)
	srStressFlushInterval = envDuration("STRESS_FLUSH_INTERVAL", 2*time.Second)
)

const srStressDatabase = "stress"

func srStressContainer(t *testing.T, ctx context.Context) (dsn, uri string) {
	t.Helper()
	if dsn, uri := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI"); dsn != "" && uri != "" {
		return dsn, uri
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "starrocks/allin1-ubuntu:3.3-latest",
			ExposedPorts: []string{"9030/tcp", "8040/tcp"},
			Cmd: []string{"/bin/sh", "-c", "echo 'storage_flood_stage_usage_percent=99' >> /data/deploy/starrocks/be/conf/be.conf && " +
				"echo 'storage_flood_stage_left_capacity_bytes=104857600' >> /data/deploy/starrocks/be/conf/be.conf && " +
				"cd /data/deploy && ./entrypoint.sh"},
			WaitingFor: wait.ForListeningPort("9030/tcp").WithStartupTimeout(300 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "9030")
	require.NoError(t, err)
	httpPort, err := container.MappedPort(ctx, "8040")
	require.NoError(t, err)
	return fmt.Sprintf("root@tcp(%s:%s)/", host, port.Port()),
		fmt.Sprintf("starrocks://root@%s:%s/?http_port=%s&replication_num=1", host, port.Port(), httpPort.Port())
}

// srStressOpenDB connects to StarRocks and drops the databases a previous run
// left behind. StarRocks drops DATETIME fractional seconds in the binary
// protocol that parameterized queries use; interpolated queries use the text
// protocol.
func srStressOpenDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	sr, err := sql.Open("mysql", dsn+"?interpolateParams=true")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sr.Close() })
	sr.SetMaxOpenConns(8)
	srStressWaitReady(t, sr)
	for _, db := range []string{srStressDatabase, "_bruin_staging"} {
		_, err = sr.Exec("DROP DATABASE IF EXISTS " + db + " FORCE")
		require.NoError(t, err)
	}
	return sr
}

// srStressWaitReady waits until StarRocks can create a table: the backend is
// alive and has reported disk capacity.
func srStressWaitReady(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		_, lastErr = db.Exec("CREATE DATABASE IF NOT EXISTS ready_probe")
		if lastErr == nil {
			_, lastErr = db.Exec("CREATE TABLE IF NOT EXISTS ready_probe.t (k INT) DUPLICATE KEY(k) DISTRIBUTED BY HASH(k) BUCKETS 1 PROPERTIES ('replication_num' = '1')")
		}
		if lastErr == nil {
			_, _ = db.Exec("DROP DATABASE ready_probe FORCE")
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("StarRocks did not become ready: %v", lastErr)
}

// srStressSink supervises the ingestr binary: it restarts it whenever it
// exits and, while chaos is enabled, kills it at random intervals.
type srStressSink struct {
	t      *testing.T
	bin    string
	args   []string
	logDir string
	chaos  atomic.Bool
	// expectedExit, when set, reports that an exit was caused by an injected
	// destination fault and should be restarted rather than failing the test.
	expectedExit func() bool
	// dockerNetwork runs each ingestr process as a container on this network
	// (bin must then be a Linux build); signals go through docker kill.
	dockerNetwork string

	mu                sync.Mutex
	logs              []string
	runs              int
	sigkills, sigterm int
	restartRequests   int
	transientExits    int
	fatal             error
}

func (s *srStressSink) record(update func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update()
}

func (s *srStressSink) fatalErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatal
}

func (s *srStressSink) summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("runs=%d sigkill=%d sigterm=%d restart-required=%d transient-exits=%d", s.runs, s.sigkills, s.sigterm, s.restartRequests, s.transientExits)
}

func srStressTail(path string, lines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// srStressTransient lists exits a production supervisor would simply restart:
// the previous process's replication connection has not been released yet.
func srStressTransient(tail string) bool {
	for _, marker := range []string{"is active for PID", "replication slot", "connection refused", "broken pipe"} {
		if strings.Contains(tail, marker) {
			return true
		}
	}
	return false
}

func (s *srStressSink) run(ctx context.Context) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		s.mu.Lock()
		s.runs++
		logPath := filepath.Join(s.logDir, fmt.Sprintf("ingestr-%03d.log", s.runs))
		s.logs = append(s.logs, logPath)
		s.mu.Unlock()
		logFile, err := os.Create(logPath)
		if err != nil {
			s.record(func() { s.fatal = err })
			return
		}
		cmd := exec.Command(s.bin, s.args...)
		signal := func(sig syscall.Signal) { _ = cmd.Process.Signal(sig) }
		if s.dockerNetwork != "" {
			name := fmt.Sprintf("ingestr-stress-%d-%d", os.Getpid(), s.runs)
			dockerArgs := append([]string{
				"run", "--rm", "--name", name, "--network", s.dockerNetwork,
				"--add-host", "host.docker.internal:host-gateway",
				"-v", filepath.Dir(s.bin) + ":/app:ro", "golang:1.26-bookworm", "/app/" + filepath.Base(s.bin),
			}, s.args...)
			cmd = exec.Command("docker", dockerArgs...)
			signal = func(sig syscall.Signal) {
				name := name
				flag := "TERM"
				if sig == syscall.SIGKILL {
					flag = "KILL"
				}
				_ = exec.Command("docker", "kill", "--signal", flag, name).Run()
			}
		}
		cmd.Stdout, cmd.Stderr = logFile, logFile
		if err := cmd.Start(); err != nil {
			_ = logFile.Close()
			s.record(func() { s.fatal = fmt.Errorf("start ingestr: %w", err) })
			return
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		killAfter := time.Duration(float64(srStressKillEvery) * (0.5 + rng.Float64()))
		killTimer := time.NewTimer(killAfter)
		expected, stopping := false, false
		var exitErr error
	wait:
		for {
			select {
			case exitErr = <-done:
				break wait
			case <-killTimer.C:
				if !s.chaos.Load() {
					killTimer.Reset(5 * time.Second)
					continue
				}
				expected = true
				if rng.Intn(2) == 0 {
					s.record(func() { s.sigkills++ })
					s.t.Logf("chaos: SIGKILL ingestr run %d", s.runs)
					signal(syscall.SIGKILL)
				} else {
					s.record(func() { s.sigterm++ })
					s.t.Logf("chaos: SIGTERM ingestr run %d", s.runs)
					signal(syscall.SIGTERM)
					go func() {
						time.Sleep(90 * time.Second)
						signal(syscall.SIGKILL)
					}()
				}
			case <-ctx.Done():
				expected, stopping = true, true
				signal(syscall.SIGTERM)
				select {
				case exitErr = <-done:
				case <-time.After(2 * time.Minute):
					signal(syscall.SIGKILL)
					exitErr = <-done
					s.record(func() { s.fatal = errors.New("ingestr did not exit within 2m of SIGTERM") })
				}
				break wait
			}
		}
		killTimer.Stop()
		_ = logFile.Close()
		if stopping {
			return
		}
		if !expected {
			tail := srStressTail(logPath, 40)
			switch {
			case strings.Contains(tail, "requires restarting the stream before it can be ingested"),
				strings.Contains(tail, "retry so it can be prepared safely"):
				s.record(func() { s.restartRequests++ })
				s.t.Logf("ingestr run %d requested a restart", s.runs)
			case srStressTransient(tail), s.expectedExit != nil && s.expectedExit():
				s.record(func() { s.transientExits++ })
				s.t.Logf("ingestr run %d exited transiently (%v):\n%s", s.runs, exitErr, srStressTail(logPath, 8))
			default:
				s.record(func() {
					s.fatal = fmt.Errorf("ingestr run %d exited unexpectedly (%v); log %s:\n%s", s.runs, exitErr, logPath, tail)
				})
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// srStressScanLogs fails on panics and Go runtime crashes in any run.
func (s *srStressSink) scanLogs() error {
	s.mu.Lock()
	logs := append([]string(nil), s.logs...)
	s.mu.Unlock()
	for _, path := range logs {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for line := 1; scanner.Scan(); line++ {
			text := scanner.Text()
			if strings.HasPrefix(text, "panic:") || strings.HasPrefix(text, "fatal error:") || strings.Contains(text, "goroutine ") && strings.Contains(text, "[running]") {
				_ = f.Close()
				return fmt.Errorf("%s:%d: %s\n%s", path, line, text, srStressTail(path, 60))
			}
		}
		_ = f.Close()
	}
	return nil
}

func TestPostgresCDC_StressStarRocks(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}
	bin := os.Getenv("STRESS_INGESTR_BIN")
	if bin == "" {
		bin = "../../bin/ingestr"
	}
	bin, err := filepath.Abs(bin)
	require.NoError(t, err)
	_, err = os.Stat(bin)
	require.NoError(t, err, "build the binary first with `make build` or set STRESS_INGESTR_BIN")

	sourceContainer, sourceConnString := stressSourceContainer(t, ctx)
	srDSN, srURI := srStressContainer(t, ctx)
	sr := srStressOpenDB(t, srDSN)

	srcPool, err := pgxpool.New(ctx, fmt.Sprintf("%s&pool_max_conns=%d", sourceConnString, max(32, stressWorkers+8)))
	require.NoError(t, err)
	t.Cleanup(srcPool.Close)
	verifyPool, err := pgxpool.New(ctx, sourceConnString+"&timezone=UTC&pool_max_conns=8")
	require.NoError(t, err)
	t.Cleanup(verifyPool.Close)

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

	destURI := strings.Replace(srURI, "/?", "/"+srStressDatabase+"?", 1)
	sink := &srStressSink{
		t:      t,
		bin:    bin,
		logDir: t.TempDir(),
		args: []string{
			"ingest",
			"--source-uri", "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=5s&state_id=postgres-starrocks-stress&dest_schema=" + srStressDatabase,
			"--dest-uri", destURI,
			"--cdc-table-naming", "table",
			"--incremental-strategy", "merge",
			"--stream",
			"--flush-interval", srStressFlushInterval.String(),
			"--flush-records", "25000",
			"--progress", "log",
			"--yes",
		},
	}
	if keep := os.Getenv("STRESS_LOG_DIR"); keep != "" {
		require.NoError(t, os.MkdirAll(keep, 0o755))
		sink.logDir = keep
	}
	t.Logf("ingestr logs: %s", sink.logDir)
	sink.chaos.Store(true)
	sinkCtx, stopSink := context.WithCancel(ctx)
	sinkDone := make(chan struct{})
	go func() { defer close(sinkDone); sink.run(sinkCtx) }()
	t.Cleanup(func() { stopSink(); <-sinkDone })

	ddlPhases := srStressSchemaPhases()
	ddlDelay := stressEventDelay(stressSchemaEvery, stressLoadDuration, len(ddlPhases))
	lateTableDelay := stressEventDelay(stressNewTableEvery, stressLoadDuration, stressLateTables)
	t.Logf("load phase: %v at ~%d ops/sec across %d workers, %d late tables every %v, %d schema phases every %v, chaos kill every ~%v",
		stressLoadDuration, stressTargetOpsPerSec, stressWorkers, stressLateTables, lateTableDelay, len(ddlPhases), ddlDelay, srStressKillEvery)

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()
	var completedDDL atomic.Int64
	gen := newCDCStressLoadGen(t, stressWorkers, stressTargetOpsPerSec, stressLoadDuration)
	recordOpErr := gen.recordOpErr
	var wg sync.WaitGroup
	gen.start(loadCtx, &wg, func(rng *rand.Rand) (string, int64, error) {
		tbl := tables.pick(rng)
		switch roll := rng.Intn(100); {
		case roll < 40:
			id := tbl.nextID.Add(1)
			if _, err := srcPool.Exec(ctx, tbl.insertSQL, id, id, fmt.Sprintf("ins-%d", id)); err != nil {
				return "insert", 0, fmt.Errorf("insert %s id=%d: %w", tbl.name, id, err)
			}
			return "insert", 1, nil
		case roll < 75:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			result, err := srcPool.Exec(ctx, tbl.updateSQL, id, fmt.Sprintf("upd-%d", id))
			if err != nil {
				return "update", 0, fmt.Errorf("update %s id=%d: %w", tbl.name, id, err)
			}
			return "update", result.RowsAffected(), nil
		case roll < 90:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			result, err := srcPool.Exec(ctx, tbl.deleteSQL, id)
			if err != nil {
				return "delete", 0, fmt.Errorf("delete %s id=%d: %w", tbl.name, id, err)
			}
			return "delete", result.RowsAffected(), nil
		default:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			result, err := srcPool.Exec(ctx, tbl.pkUpdateSQL, stressPKMoveOffset, id)
			if err != nil {
				return "pk-update", 0, fmt.Errorf("pk-update %s id=%d: %w", tbl.name, id, err)
			}
			return "pk-update", result.RowsAffected(), nil
		}
	})
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
	status := time.NewTicker(30 * time.Second)
	defer status.Stop()
	started := time.Now()
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case <-sinkDone:
			stopLoad()
			<-loadDone
			t.Fatalf("ingestr supervisor stopped during load: %v", sink.fatalErr())
		case <-status.C:
			t.Logf("t=%v: %s, DDL=%d/%d, sink %s", time.Since(started).Round(time.Second), gen.status(), completedDDL.Load(), len(ddlPhases), sink.summary())
		}
	}
	require.Equal(t, int64(len(ddlPhases)), completedDDL.Load(), "all schema phases must complete")
	gen.verify()
	finalTables := tables.snapshot()

	sink.chaos.Store(false)
	truths := make(map[string]stressTruth, len(finalTables))
	for _, tbl := range finalTables {
		var tr stressTruth
		require.NoError(t, srcPool.QueryRow(ctx,
			fmt.Sprintf(`SELECT count(*), COALESCE(sum(val), 0)::text FROM public.%s`, tbl.name)).Scan(&tr.count, &tr.sum))
		truths[tbl.name] = tr
		t.Logf("source truth %s: count=%d sum=%s", tbl.name, tr.count, tr.sum)
	}
	var finalLSN string
	require.NoError(t, srcPool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&finalLSN))
	deadline := time.Now().Add(srStressConverge)
	lastLog := time.Now()
	for {
		select {
		case <-sinkDone:
			t.Fatalf("ingestr supervisor stopped during convergence: %v", sink.fatalErr())
		default:
		}
		var caughtUp int
		require.NoError(t, srcPool.QueryRow(ctx, `
			SELECT count(*) FROM pg_replication_slots
			WHERE NOT temporary AND confirmed_flush_lsn >= $1::pg_lsn`, finalLSN).Scan(&caughtUp))
		if caughtUp >= 1 {
			break
		}
		if time.Since(lastLog) > 30*time.Second {
			lastLog = time.Now()
			t.Logf("waiting for durable CDC acknowledgement through %s (%s)", finalLSN, sink.summary())
			stressDumpReplicationState(t, ctx, srcPool)
		}
		if time.Now().After(deadline) {
			stressDumpReplicationState(t, ctx, srcPool)
			stressDumpContainerLogs(t, ctx, sourceContainer, 60)
			t.Fatalf("slot did not acknowledge %s within %v", finalLSN, srStressConverge)
		}
		time.Sleep(2 * time.Second)
	}
	stopSink()
	<-sinkDone
	require.NoError(t, sink.fatalErr())
	t.Logf("ingestr supervisor: %s", sink.summary())
	require.NoError(t, sink.scanLogs(), "ingestr logs contain a panic or runtime crash")

	var mismatches []string
	for _, tbl := range finalTables {
		dest := srStressDatabase + "." + quoteStarRocksStressIdent(tbl.name)
		var count int64
		var sum string
		if err := sr.QueryRow("SELECT count(*), CAST(COALESCE(sum(val), 0) AS STRING) FROM "+dest+" WHERE _cdc_deleted = false").Scan(&count, &sum); err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s: aggregate query failed: %v", tbl.name, err))
			continue
		}
		want := truths[tbl.name]
		if count != want.count || !srStressNumericEqual(sum, want.sum) {
			mismatches = append(mismatches, fmt.Sprintf("%s: aggregate mismatch: source count=%d sum=%s, StarRocks count=%d sum=%s", tbl.name, want.count, want.sum, count, sum))
		}
		if err := srStressCompareRows(ctx, verifyPool, sr, tbl.name, dest); err != nil {
			mismatches = append(mismatches, err.Error())
		} else {
			t.Logf("%s: %d rows match value by value", tbl.name, want.count)
		}
		var nullDeleted int64
		require.NoError(t, sr.QueryRow("SELECT count(*) FROM "+dest+" WHERE _cdc_deleted IS NULL OR _cdc_lsn IS NULL").Scan(&nullDeleted))
		if nullDeleted > 0 {
			mismatches = append(mismatches, fmt.Sprintf("%s: %d rows have NULL _cdc_deleted or _cdc_lsn", tbl.name, nullDeleted))
		}
	}
	require.Empty(t, mismatches, "StarRocks diverged from the source:\n%s", strings.Join(mismatches, "\n"))

	destColumns := srStressColumns(t, sr, stressEvolvingTable)
	for _, column := range []string{"id", "val", "payload", "updated_at", "cohort", "metadata", "legacy", "segment"} {
		require.Contains(t, destColumns, column, "StarRocks %s should keep column %s", stressEvolvingTable, column)
	}
	var softDeleted int64
	for _, tbl := range finalTables {
		var deleted int64
		require.NoError(t, sr.QueryRow("SELECT count(*) FROM "+srStressDatabase+"."+quoteStarRocksStressIdent(tbl.name)+" WHERE _cdc_deleted = true").Scan(&deleted))
		softDeleted += deleted
	}
	require.Positive(t, softDeleted, "StarRocks should retain soft-deleted CDC rows")
	var checkpoints, completed int
	require.NoError(t, sr.QueryRow(`SELECT
		SUM(CASE WHEN state_kind = 'checkpoint' AND state_status = 'complete' THEN 1 ELSE 0 END),
		COUNT(DISTINCT CASE WHEN state_kind = 'snapshot' AND state_status = 'complete' THEN source_table END)
		FROM _bruin_staging.cdc_state`).Scan(&checkpoints, &completed))
	t.Logf("StarRocks CDC state: checkpoints=%d completed snapshot tables=%d", checkpoints, completed)
	require.Positive(t, checkpoints)
	require.Equal(t, len(finalTables), completed)
}

// srStressSchemaPhases swaps the BIGINT→NUMERIC phases, which StarRocks
// cannot apply in place (the stream stops with a --full-refresh error), for
// widenings it supports.
func srStressSchemaPhases() []stressSchemaPhase {
	var phases []stressSchemaPhase
	for _, phase := range stressSchemaPhases() {
		switch phase.name {
		case "widen bigint to numeric":
			phases = append(phases, stressSchemaPhase{
				name: "add int counter column",
				sql: fmt.Sprintf(`
					ALTER TABLE public.%[1]s ADD COLUMN counter INT NOT NULL DEFAULT 1;
					UPDATE public.%[1]s SET counter = (id %% 1000)::int, updated_at = now() WHERE id %% 3 = 0;
				`, stressEvolvingTable),
			})
		case "widen numeric precision and scale":
			phases = append(phases, stressSchemaPhase{
				name: "widen int to bigint",
				sql: fmt.Sprintf(`
					ALTER TABLE public.%[1]s ALTER COLUMN counter TYPE BIGINT;
					UPDATE public.%[1]s SET counter = counter + 3000000000, updated_at = now() WHERE id %% 29 = 0;
				`, stressEvolvingTable),
			})
		default:
			phases = append(phases, phase)
		}
	}
	return phases
}

func quoteStarRocksStressIdent(name string) string {
	return "`" + name + "`"
}

func srStressColumns(t *testing.T, sr *sql.DB, table string) map[string]string {
	t.Helper()
	rows, err := sr.Query("SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", srStressDatabase, table)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	columns := map[string]string{}
	for rows.Next() {
		var name, typ string
		require.NoError(t, rows.Scan(&name, &typ))
		columns[name] = typ
	}
	require.NoError(t, rows.Err())
	return columns
}

func srStressNumericEqual(a, b string) bool {
	x, okA := new(big.Rat).SetString(strings.TrimSpace(a))
	y, okB := new(big.Rat).SetString(strings.TrimSpace(b))
	return okA && okB && x.Cmp(y) == 0
}

var srStressTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999Z07",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999Z07:00",
}

// srStressNormalize renders a source or destination value in a canonical form:
// JSON is canonicalized, timestamps become UTC instants (StarRocks DATETIME
// stores the UTC wall clock), and numbers compare by value.
func srStressNormalize(v sql.NullString) string {
	if !v.Valid {
		return "<NULL>"
	}
	s := v.String
	if trimmed := strings.TrimSpace(s); strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if canonical, err := canonicalizeCDCStressJSON(trimmed); err == nil {
			return "json:" + canonical
		}
	}
	for _, layout := range srStressTimeLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return "ts:" + ts.UTC().Format(time.RFC3339Nano)
		}
	}
	if r, ok := new(big.Rat).SetString(s); ok && !strings.ContainsAny(s, "/") {
		return "num:" + r.RatString()
	}
	return "str:" + s
}

// srStressCompareRows walks both tables in primary-key order and compares
// every active source column.
func srStressCompareRows(ctx context.Context, src *pgxpool.Pool, sr *sql.DB, table, dest string) error {
	columns, err := stressSourceColumns(ctx, src, table)
	if err != nil {
		return err
	}
	pgCols := make([]string, len(columns))
	srCols := make([]string, len(columns))
	for i, column := range columns {
		pgCols[i] = fmt.Sprintf("%s::text AS c%d", quoteStressIdentifier(column), i)
		srCols[i] = quoteStarRocksStressIdent(column)
	}
	fetch := func(last int64, limit int) ([]srStressRow, error) {
		rows, err := src.Query(ctx, fmt.Sprintf(`SELECT id, %s FROM public.%s WHERE id > $1 ORDER BY id LIMIT %d`,
			strings.Join(pgCols, ", "), quoteStressIdentifier(table), limit), last)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []srStressRow
		for rows.Next() {
			row, err := srStressScanRow(rows.Scan, len(columns))
			if err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}
	return srStressCompareTable(ctx, sr, table, dest, columns, srCols, fetch)
}

type srStressRow struct {
	id     int64
	values []string
}

func srStressScanRow(scan func(...any) error, columns int) (srStressRow, error) {
	raw := make([]sql.NullString, columns)
	ptrs := make([]any, columns+1)
	var row srStressRow
	ptrs[0] = &row.id
	for i := range raw {
		ptrs[i+1] = &raw[i]
	}
	if err := scan(ptrs...); err != nil {
		return row, err
	}
	row.values = make([]string, columns)
	for i := range raw {
		row.values[i] = srStressNormalize(raw[i])
	}
	return row, nil
}

// srStressCompareTable pages through the source with fetch and compares each
// chunk with the live StarRocks rows in the same id range.
func srStressCompareTable(ctx context.Context, sr *sql.DB, table, dest string, columns, srExprs []string, fetch func(last int64, limit int) ([]srStressRow, error)) error {
	const chunk = 20000
	last := int64(-1 << 62)
	for {
		srcRows, err := fetch(last, chunk)
		if err != nil {
			return fmt.Errorf("%s: source fetch: %w", table, err)
		}
		upper := ""
		if len(srcRows) == chunk {
			upper = fmt.Sprintf(" AND id <= %d", srcRows[len(srcRows)-1].id)
		}
		dstRows, err := sr.QueryContext(ctx, fmt.Sprintf("SELECT id, %s FROM %s WHERE _cdc_deleted = false AND id > ?%s ORDER BY id",
			strings.Join(srExprs, ", "), dest, upper), last)
		if err != nil {
			return fmt.Errorf("%s: StarRocks fetch: %w", table, err)
		}
		i := 0
		for dstRows.Next() {
			got, err := srStressScanRow(dstRows.Scan, len(columns))
			if err != nil {
				_ = dstRows.Close()
				return fmt.Errorf("%s: StarRocks scan: %w", table, err)
			}
			if i >= len(srcRows) || got.id < srcRows[i].id {
				_ = dstRows.Close()
				return fmt.Errorf("%s: StarRocks has extra live row id=%d not in source", table, got.id)
			}
			want := srcRows[i]
			if got.id != want.id {
				_ = dstRows.Close()
				return fmt.Errorf("%s: StarRocks is missing source row id=%d", table, want.id)
			}
			for j := range got.values {
				if got.values[j] != want.values[j] {
					_ = dstRows.Close()
					return fmt.Errorf("%s: id=%d column %s: source=%.200s StarRocks=%.200s", table, got.id, columns[j], want.values[j], got.values[j])
				}
			}
			i++
		}
		_ = dstRows.Close()
		if err := dstRows.Err(); err != nil {
			return fmt.Errorf("%s: StarRocks rows: %w", table, err)
		}
		if i < len(srcRows) {
			return fmt.Errorf("%s: StarRocks is missing source row id=%d", table, srcRows[i].id)
		}
		if len(srcRows) < chunk {
			return nil
		}
		last = srcRows[len(srcRows)-1].id
	}
}
