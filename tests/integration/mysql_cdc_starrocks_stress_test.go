//go:build stress

// MySQL CDC → StarRocks soak test. MySQL CDC runs as batch catch-up, so the
// real ingestr binary runs back to back while the shared load generator
// writes to the source. Runs are randomly killed with SIGKILL or SIGTERM and
// sometimes overlap a second run, which the destination run lease must
// serialize. After the load, StarRocks must hold exactly the source rows,
// compared value by value. Schema churn then forces --full-refresh swaps and
// incremental schema evolution, verified the same way.
//
// Run with e.g.:
//
//	make build && STRESS_LOAD_DURATION=30m go test -tags stress -count=1 -v -timeout 120m \
//	  -run '^TestMySQLCDC_StressStarRocks$' ./tests/integration/
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/stretchr/testify/require"
)

var (
	srMySQLChaosRate = envInt("STRESS_CHAOS_PERCENT", 35)
	// STRESS_MYSQL_STREAM=1 replaces the back-to-back batch runs with one
	// supervised --stream process that is killed and restarted during load.
	srMySQLStream = os.Getenv("STRESS_MYSQL_STREAM") == "1"
)

type srMySQLRunner struct {
	t      *testing.T
	bin    string
	logDir string
	base   []string

	mu                        sync.Mutex
	runs                      int
	ok, sigkill, sigterm      int
	leaseBusy, retryable      int
	overlaps, overlapFailures int
	logs                      []string
}

func (r *srMySQLRunner) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("runs=%d ok=%d sigkill=%d sigterm=%d lease-busy=%d retryable=%d overlapping=%d", r.runs, r.ok, r.sigkill, r.sigterm, r.leaseBusy, r.retryable, r.overlaps)
}

// run executes one ingestr run. killAfter > 0 sends signal after that delay.
// It returns the log tail and an error only for unexpected failures.
func (r *srMySQLRunner) run(ctx context.Context, extra []string, killAfter time.Duration, signal syscall.Signal) (string, error) {
	r.mu.Lock()
	r.runs++
	logPath := filepath.Join(r.logDir, fmt.Sprintf("ingestr-%04d.log", r.runs))
	r.logs = append(r.logs, logPath)
	r.mu.Unlock()
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.Command(r.bin, append(append([]string{}, r.base...), extra...)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timer <-chan time.Time
	if killAfter > 0 {
		timer = time.After(killAfter)
	}
	killed := false
	var exitErr error
	select {
	case exitErr = <-done:
	case <-timer:
		killed = true
		_ = cmd.Process.Signal(signal)
		select {
		case exitErr = <-done:
		case <-time.After(90 * time.Second):
			_ = cmd.Process.Signal(syscall.SIGKILL)
			exitErr = <-done
			return srStressTail(logPath, 40), fmt.Errorf("ingestr did not exit within 90s of %v", signal)
		}
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGKILL)
		<-done
		return "", ctx.Err()
	}
	tail := srStressTail(logPath, 40)
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case exitErr == nil:
		r.ok++
		return tail, nil
	case killed && signal == syscall.SIGKILL:
		r.sigkill++
		return tail, nil
	case killed:
		r.sigterm++
		return tail, nil
	case strings.Contains(tail, "already owns connector"):
		r.leaseBusy++
		return tail, errLeaseBusy
	case strings.Contains(tail, "retry so it can be prepared safely"):
		r.retryable++
		return tail, nil
	}
	return tail, fmt.Errorf("ingestr run %d failed (%v); log %s:\n%s", r.runs, exitErr, logPath, tail)
}

var errLeaseBusy = fmt.Errorf("lease busy")

// resnapshots counts runs after the first that read at least minRows rows,
// i.e. that re-snapshotted instead of resuming.
func (r *srMySQLRunner) resnapshots(minRows int) int {
	r.mu.Lock()
	logs := append([]string(nil), r.logs...)
	r.mu.Unlock()
	count := 0
	for i, path := range logs {
		data, err := os.ReadFile(path)
		if i == 0 || err != nil {
			continue
		}
		idx := strings.LastIndex(string(data), "Total Rows")
		if idx < 0 {
			continue
		}
		var rows int
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, strings.SplitN(string(data[idx:]), "\n", 2)[0])
		_, _ = fmt.Sscan(digits, &rows)
		if rows >= minRows {
			count++
		}
	}
	return count
}

func (r *srMySQLRunner) scanLogs() error {
	s := &srStressSink{logs: r.logs}
	return s.scanLogs()
}

func srMySQLCompareRows(ctx context.Context, src, sr *sql.DB, table, dest string) error {
	columns, err := mysqlStressSourceColumns(ctx, src, table)
	if err != nil {
		return err
	}
	names := make([]string, len(columns))
	srcExprs := make([]string, len(columns))
	srExprs := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.name
		quoted := "`" + column.name + "`"
		switch strings.ToLower(column.dataType) {
		case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
			srcExprs[i], srExprs[i] = "LOWER(HEX("+quoted+"))", "LOWER(HEX("+quoted+"))"
		default:
			srcExprs[i], srExprs[i] = quoted, quoted
		}
		srcExprs[i] += fmt.Sprintf(" AS c%d", i)
	}
	fetch := func(last int64, limit int) ([]srStressRow, error) {
		rows, err := src.QueryContext(ctx, fmt.Sprintf("SELECT id, %s FROM `%s` WHERE id > ? ORDER BY id LIMIT %d",
			strings.Join(srcExprs, ", "), table, limit), last)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
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
	return srStressCompareTable(ctx, sr, table, dest, names, srExprs, fetch)
}

// srMySQLAwaitStream waits until the streaming sink has applied every source
// change: aggregates match and stay matched across two flush intervals.
func srMySQLAwaitStream(t *testing.T, ctx context.Context, sink *srStressSink, src, sr *sql.DB, tables *mysqlStressTableSet) {
	t.Helper()
	deadline := time.Now().Add(srStressConverge)
	stable := 0
	for {
		require.NoError(t, sink.fatalErr())
		pending := ""
		for _, tbl := range tables.snapshot() {
			want, err := mysqlStressSourceTruth(ctx, src, tbl.name)
			require.NoError(t, err)
			var got mysqlStressTruth
			err = sr.QueryRow("SELECT count(*), CAST(COALESCE(sum(val), 0) AS STRING) FROM "+srStressDatabase+".`"+tbl.name+"` WHERE _cdc_deleted = false").Scan(&got.count, &got.sum)
			if err != nil || got != want {
				pending = fmt.Sprintf("%s: source %+v StarRocks %+v (err=%v)", tbl.name, want, got, err)
				break
			}
		}
		if pending == "" {
			stable++
			if stable >= 2 {
				return
			}
		} else {
			stable = 0
			require.False(t, time.Now().After(deadline), "stream did not converge within %v: %s", srStressConverge, pending)
		}
		time.Sleep(2*srStressFlushInterval + time.Second)
	}
}

func TestMySQLCDC_StressStarRocks(t *testing.T) {
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

	_, sourceURI, sourceDSN := mysqlStressContainer(t, ctx, []string{
		"--server-id=21777",
		"--log-bin=mysql-bin",
		"--gtid-mode=ON",
		"--enforce-gtid-consistency=ON",
		"--binlog-format=ROW",
		"--binlog-row-image=FULL",
		"--default-time-zone=+03:00",
		"--max-connections=200",
		"--innodb-flush-log-at-trx-commit=0",
		"--sync-binlog=0",
	})
	srDSN, srURI := srStressContainer(t, ctx)
	// StarRocks drops DATETIME fractional seconds in the binary protocol that
	// parameterized queries use; interpolated queries use the text protocol.
	sr, err := sql.Open("mysql", srDSN+"?interpolateParams=true")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sr.Close() })
	sr.SetMaxOpenConns(8)
	srStressWaitReady(t, sr)
	_, err = sr.Exec("DROP DATABASE IF EXISTS " + srStressDatabase + " FORCE")
	require.NoError(t, err)
	_, err = sr.Exec("DROP DATABASE IF EXISTS _bruin_staging FORCE")
	require.NoError(t, err)

	loadDB := mysqlStressOpenDB(t, sourceDSN, stressWorkers+8)
	verifyDSN := strings.Replace(sourceDSN, "?parseTime=true", "?time_zone=%27%2B00%3A00%27", 1)
	srcVerify := mysqlStressOpenDB(t, verifyDSN, 4)

	tables := &mysqlStressTableSet{}
	for i := 0; i < mysqlStressInitialTables; i++ {
		tbl := &mysqlStressTable{name: mysqlStressTableName(i)}
		require.NoError(t, mysqlStressCreatePlain(ctx, loadDB, tbl.name))
		require.NoError(t, mysqlStressSeedTable(ctx, loadDB, tbl, mysqlStressSeedRows))
		tables.add(tbl)
	}
	typesTbl := &mysqlStressTable{name: mysqlStressTypesTable, types: true}
	require.NoError(t, mysqlStressCreateTypes(ctx, loadDB))
	require.NoError(t, mysqlStressSeedTable(ctx, loadDB, typesTbl, mysqlStressSeedRows))
	tables.add(typesTbl)

	runner := &srMySQLRunner{
		t:      t,
		bin:    bin,
		logDir: t.TempDir(),
		base: []string{
			"ingest",
			"--source-uri", sourceURI[:len("mysql")] + "+cdc" + sourceURI[len("mysql"):] + "?server_id=23001",
			"--dest-uri", strings.Replace(srURI, "/?", "/"+srStressDatabase+"?", 1),
			"--incremental-strategy", "merge",
			"--progress", "log",
			"--yes",
		},
	}
	if keep := os.Getenv("STRESS_LOG_DIR"); keep != "" {
		require.NoError(t, os.MkdirAll(keep, 0o755))
		runner.logDir = keep
	}
	t.Logf("ingestr logs: %s", runner.logDir)

	start := time.Now()
	_, err = runner.run(ctx, nil, 0, 0)
	require.NoError(t, err, "initial snapshot run")
	t.Logf("initial snapshot of %d rows took %v", (mysqlStressInitialTables+1)*mysqlStressSeedRows, time.Since(start).Round(time.Millisecond))

	lateTableDelay := stressEventDelay(stressNewTableEvery, stressLoadDuration, stressLateTables)
	t.Logf("load phase: %v at ~%d ops/sec across %d workers, %d late tables every %v, chaos on %d%% of runs",
		stressLoadDuration, stressTargetOpsPerSec, stressWorkers, stressLateTables, lateTableDelay, srMySQLChaosRate)
	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()
	gen := newCDCStressLoadGen(t, stressWorkers, stressTargetOpsPerSec, stressLoadDuration)
	recordOpErr := gen.recordOpErr
	var wg sync.WaitGroup
	gen.start(loadCtx, &wg, func(rng *rand.Rand) (string, int64, error) {
		tbl := tables.pick(rng)
		roll := rng.Intn(100)
		switch {
		case roll < 40:
			id := tbl.nextID.Add(1)
			var err error
			if tbl.types {
				err = mysqlStressInsertTypes(ctx, loadDB, id)
			} else {
				_, err = loadDB.ExecContext(ctx,
					fmt.Sprintf("INSERT INTO %s (id, val, payload, updated_at) VALUES (?, ?, ?, NOW(6))", tbl.name),
					id, id, fmt.Sprintf("ins-%d", id))
			}
			if err != nil {
				return "insert", 0, fmt.Errorf("insert %s id=%d: %w", tbl.name, id, err)
			}
			return "insert", 1, nil
		case roll < 75:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			var affected int64
			var err error
			if tbl.types {
				affected, err = mysqlStressUpdateTypes(ctx, loadDB, id, rng.Int63n(1_000_000))
			} else {
				var result sql.Result
				result, err = loadDB.ExecContext(ctx,
					fmt.Sprintf("UPDATE %s SET val = val + 1, payload = ?, updated_at = NOW(6) WHERE id = ?", tbl.name),
					fmt.Sprintf("upd-%d", id), id)
				if err == nil {
					affected, _ = result.RowsAffected()
				}
			}
			if err != nil {
				return "update", 0, fmt.Errorf("update %s id=%d: %w", tbl.name, id, err)
			}
			return "update", affected, nil
		case roll < 90 || tbl.types:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			result, err := loadDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", tbl.name), id)
			if err != nil {
				return "delete", 0, fmt.Errorf("delete %s id=%d: %w", tbl.name, id, err)
			}
			affected, _ := result.RowsAffected()
			return "delete", affected, nil
		default:
			id := rng.Int63n(tbl.nextID.Load()) + 1
			result, err := loadDB.ExecContext(ctx,
				fmt.Sprintf("UPDATE %s SET id = id + ?, updated_at = NOW(6) WHERE id = ? AND id < ?", tbl.name),
				stressPKMoveOffset, id, stressPKMoveOffset)
			if err != nil {
				return "pk-update", 0, fmt.Errorf("pk-update %s id=%d: %w", tbl.name, id, err)
			}
			affected, _ := result.RowsAffected()
			return "pk-update", affected, nil
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
			tbl := &mysqlStressTable{name: mysqlStressTableName(mysqlStressInitialTables + i)}
			if err := mysqlStressCreatePlain(ctx, loadDB, tbl.name); err != nil {
				recordOpErr(fmt.Errorf("create late table %s: %w", tbl.name, err))
				return
			}
			if err := mysqlStressSeedTable(ctx, loadDB, tbl, mysqlStressLateSeedRows); err != nil {
				recordOpErr(fmt.Errorf("seed late table %s: %w", tbl.name, err))
				return
			}
			tables.add(tbl)
			t.Logf("created new table %s mid-load with %d pre-existing rows", tbl.name, mysqlStressLateSeedRows)
		}
	}()

	runnerErr := make(chan error, 2)
	runnerDone := make(chan struct{})
	var overlapWG sync.WaitGroup
	var sink *srStressSink
	stopSink := func() {}
	if srMySQLStream {
		sink = &srStressSink{
			t:      t,
			bin:    bin,
			logDir: filepath.Join(runner.logDir, "stream"),
			args: append(append([]string{}, runner.base...),
				"--stream", "--flush-interval", srStressFlushInterval.String(), "--flush-records", "25000"),
		}
		require.NoError(t, os.MkdirAll(sink.logDir, 0o755))
		sink.chaos.Store(true)
		sinkCtx, cancel := context.WithCancel(ctx)
		go func() {
			defer close(runnerDone)
			sink.run(sinkCtx)
			if err := sink.fatalErr(); err != nil {
				runnerErr <- err
			}
		}()
		stopSink = func() { cancel(); <-runnerDone }
		t.Cleanup(stopSink)
	} else {
		go func() {
			defer close(runnerDone)
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for loadCtx.Err() == nil {
				var killAfter time.Duration
				signal := syscall.SIGKILL
				if rng.Intn(100) < srMySQLChaosRate {
					killAfter = time.Duration(500+rng.Intn(15000)) * time.Millisecond
					if rng.Intn(2) == 0 {
						signal = syscall.SIGTERM
					}
				}
				if rng.Intn(100) < 10 {
					runner.mu.Lock()
					runner.overlaps++
					runner.mu.Unlock()
					overlapWG.Add(1)
					go func() {
						defer overlapWG.Done()
						time.Sleep(time.Duration(rng.Intn(3000)) * time.Millisecond)
						if _, err := runner.run(ctx, nil, 0, 0); err != nil && err != errLeaseBusy {
							runnerErr <- fmt.Errorf("overlapping run: %w", err)
						}
					}()
				}
				if _, err := runner.run(ctx, nil, killAfter, signal); err != nil && err != errLeaseBusy {
					runnerErr <- err
					return
				}
				select {
				case <-loadCtx.Done():
				case <-time.After(time.Second):
				}
			}
		}()
	}

	loadDone := make(chan struct{})
	go func() { wg.Wait(); close(loadDone) }()
	status := time.NewTicker(30 * time.Second)
	defer status.Stop()
	started := time.Now()
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case err := <-runnerErr:
			stopLoad()
			<-loadDone
			t.Fatalf("ingestr run failed during load: %v", err)
		case <-status.C:
			if sink != nil {
				t.Logf("t=%v: %s, ingestr stream %s", time.Since(started).Round(time.Second), gen.status(), sink.summary())
			} else {
				t.Logf("t=%v: %s, ingestr %s", time.Since(started).Round(time.Second), gen.status(), runner.summary())
			}
		}
	}
	if sink != nil {
		sink.chaos.Store(false)
		srMySQLAwaitStream(t, ctx, sink, srcVerify, sr, tables)
		stopSink()
		require.NoError(t, sink.fatalErr())
		t.Logf("ingestr stream supervisor: %s", sink.summary())
		require.NoError(t, sink.scanLogs(), "ingestr stream logs contain a panic or runtime crash")
	}
	<-runnerDone
	overlapWG.Wait()
	select {
	case err := <-runnerErr:
		t.Fatalf("ingestr run failed: %v", err)
	default:
	}
	gen.verify()
	finalTables := tables.snapshot()
	t.Logf("load finished: %s", runner.summary())

	dest := func(table string) string { return srStressDatabase + ".`" + table + "`" }
	aggregate := func(table string) (mysqlStressTruth, error) {
		var tr mysqlStressTruth
		err := sr.QueryRow("SELECT count(*), CAST(COALESCE(sum(val), 0) AS STRING) FROM "+dest(table)+" WHERE _cdc_deleted = false").Scan(&tr.count, &tr.sum)
		return tr, err
	}
	converge := func(phase string, extra []string) {
		deadline := time.Now().Add(srStressConverge)
		for attempt := 1; ; attempt++ {
			tail, err := runner.run(ctx, extra, 0, 0)
			if err == errLeaseBusy {
				time.Sleep(5 * time.Second)
				continue
			}
			require.NoError(t, err, "phase %q: convergence run %d", phase, attempt)
			if strings.Contains(tail, "retry so it can be prepared safely") {
				continue
			}
			extra = nil
			pending := ""
			for _, tbl := range tables.snapshot() {
				want, err := mysqlStressSourceTruth(ctx, srcVerify, tbl.name)
				require.NoError(t, err)
				got, err := aggregate(tbl.name)
				if err != nil || got != want {
					pending = fmt.Sprintf("%s: source %+v StarRocks %+v (err=%v)", tbl.name, want, got, err)
					break
				}
			}
			if pending == "" {
				break
			}
			require.False(t, time.Now().After(deadline), "phase %q: StarRocks did not converge: %s", phase, pending)
			t.Logf("phase %q: pending after run %d: %s", phase, attempt, pending)
		}
		var mismatches []string
		for _, tbl := range tables.snapshot() {
			if err := srMySQLCompareRows(ctx, srcVerify, sr, tbl.name, dest(tbl.name)); err != nil {
				mismatches = append(mismatches, err.Error())
			}
		}
		require.Empty(t, mismatches, "phase %q: StarRocks diverged from the source:\n%s", phase, strings.Join(mismatches, "\n"))
		t.Logf("phase %q: %d tables match value by value", phase, len(tables.snapshot()))
	}
	converge("after load", nil)

	var softDeleted, moved int64
	for _, tbl := range finalTables {
		var deleted int64
		require.NoError(t, sr.QueryRow("SELECT count(*) FROM "+dest(tbl.name)+" WHERE _cdc_deleted = true").Scan(&deleted))
		softDeleted += deleted
	}
	require.NoError(t, sr.QueryRow("SELECT count(*) FROM "+dest(mysqlStressTableName(0))+" WHERE id >= ? AND _cdc_deleted = false", stressPKMoveOffset).Scan(&moved))
	// A run killed after merging but before certifying its state forces the
	// next run to re-snapshot, which rebuilds tables without tombstones.
	t.Logf("soft-deleted rows: %d, live moved rows in %s: %d, re-snapshot runs: %d", softDeleted, mysqlStressTableName(0), moved, runner.resnapshots(30000))

	driftTable := mysqlStressTableName(0)
	appendTable := mysqlStressTableName(1)
	churnBase := finalTables[0].nextID.Add(1000)
	appendBase := finalTables[1].nextID.Add(1000)
	execSQL := func(query string, args ...any) {
		t.Helper()
		_, err := loadDB.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	churn := func(phase string, apply func()) {
		apply()
		tail, err := runner.run(ctx, nil, 0, 0)
		if err != nil {
			require.Contains(t, tail, "--full-refresh", "phase %q: failure should point at --full-refresh: %v", phase, err)
			t.Logf("phase %q: incremental run refused the change, running --full-refresh", phase)
			converge(phase, []string{"--full-refresh"})
			return
		}
		t.Logf("phase %q: ingested incrementally", phase)
		converge(phase, nil)
	}
	churn("mid-table column add with unconsumed rows", func() {
		execSQL(fmt.Sprintf("INSERT INTO %s (id, val, payload, updated_at) VALUES (?, ?, 'pre-drift', NOW(6))", driftTable), churnBase, churnBase)
		execSQL(fmt.Sprintf("ALTER TABLE %s ADD COLUMN drift_note VARCHAR(32) NULL AFTER val", driftTable))
		execSQL(fmt.Sprintf("INSERT INTO %s (id, val, drift_note, payload, updated_at) VALUES (?, ?, 'post-drift', 'post-drift', NOW(6))", driftTable), churnBase+1, churnBase+1)
	})
	churn("append column while caught up", func() {
		execSQL(fmt.Sprintf("ALTER TABLE %s ADD COLUMN note2 VARCHAR(40) NULL", appendTable))
		execSQL(fmt.Sprintf("INSERT INTO %s (id, val, payload, note2, updated_at) VALUES (?, ?, 'note2-insert', 'fresh', NOW(6))", appendTable), appendBase, appendBase)
		execSQL(fmt.Sprintf("UPDATE %s SET note2 = CONCAT('n2-', id), updated_at = NOW(6) WHERE id %% 97 = 0 AND id < ?", appendTable), int64(mysqlStressSeedRows))
	})
	churn("unsigned and varchar widening with unconsumed rows", func() {
		execSQL(fmt.Sprintf("UPDATE %s SET u16 = 65535, ts = NOW(6) WHERE id %% 41 = 0 AND id < ?", mysqlStressTypesTable), int64(mysqlStressSeedRows))
		execSQL(fmt.Sprintf("ALTER TABLE %s MODIFY u16 INT UNSIGNED NOT NULL, MODIFY name VARCHAR(300) NOT NULL", mysqlStressTypesTable))
		execSQL(fmt.Sprintf("UPDATE %s SET u16 = 4294967295, name = CONCAT('widened-', id, '-', REPEAT('w', 150)), ts = NOW(6) WHERE id %% 43 = 0 AND id < ?", mysqlStressTypesTable), int64(mysqlStressSeedRows))
	})
	churn("column drop with unconsumed rows", func() {
		execSQL(fmt.Sprintf("UPDATE %s SET val = val + 3, updated_at = NOW(6) WHERE id %% 89 = 0 AND id < ?", driftTable), int64(mysqlStressSeedRows))
		execSQL(fmt.Sprintf("ALTER TABLE %s DROP COLUMN drift_note", driftTable))
		execSQL(fmt.Sprintf("UPDATE %s SET val = val + 5, updated_at = NOW(6) WHERE id %% 83 = 0 AND id < ?", driftTable), int64(mysqlStressSeedRows))
	})
	churn("column rename with full backfill", func() {
		execSQL(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN note2 TO note3", appendTable))
		execSQL(fmt.Sprintf("UPDATE %s SET val = val + 1, updated_at = NOW(6)", appendTable))
	})
	churn("new table appears after load", func() {
		tbl := &mysqlStressTable{name: "mstress_post_load"}
		require.NoError(t, mysqlStressCreatePlain(ctx, loadDB, tbl.name))
		require.NoError(t, mysqlStressSeedTable(ctx, loadDB, tbl, mysqlStressLateSeedRows))
		tables.add(tbl)
		execSQL(fmt.Sprintf("UPDATE %s SET val = val + 7, updated_at = NOW(6) WHERE id %% 5 = 0", tbl.name))
	})

	require.NoError(t, runner.scanLogs(), "ingestr logs contain a panic or runtime crash")
	var leases int
	require.NoError(t, sr.QueryRow("SELECT count(*) FROM information_schema.tables WHERE TABLE_SCHEMA = '_bruin_staging' AND TABLE_NAME LIKE 'cdc\\_lease\\_%'").Scan(&leases))
	t.Logf("final: %s; leftover lease tables: %d", runner.summary(), leases)
}
