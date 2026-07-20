//go:build stress

// High-volume SQL Server CDC accuracy test. Like the PostgreSQL and MySQL
// variants it is excluded from the regular integration suite (build tag
// `stress`); run it with `make cdc-mssql-stress-test`.
//
// Scenario: a streaming multi-table CDC pipeline replicates from SQL Server
// into Postgres while parallel workers apply ~1000 inserts/updates/deletes/
// PK-updates per second. SQL Server CDC freezes captured columns per capture
// instance and discovers tables only at stream start, so schema changes follow
// the documented operational path — recreate the capture instance and restart
// the stream, which must recover via resume-from-destination-LSN or the
// re-snapshot+truncate boundary. During the load, tables with pre-existing
// rows appear mid-stream, and one table goes through column add/drop/rename, a
// bigint→decimal widening, a delete-all+repopulate transaction, and a
// primary-key move. Afterwards the destination must converge to the exact
// source rows and schema, verified by aggregates and canonical cross-engine
// row comparison.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmssql "github.com/testcontainers/testcontainers-go/modules/mssql"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	msqStressPassword    = "StressPassword123!"
	msqStressPasswordEnc = "StressPassword123%21"
	msqStressDB          = "stressdb"
	msqStressDestSchema  = "stress"
	msqStressLateSeed    = 2000
	msqStressEvolving    = "stress_evolving"
	msqStressTypes       = "stress_types"
	msqStressPKOffset    = int64(1_000_000_000)
	msqStressSentinelOld = int64(9_000_000_000_000)
	msqStressSentinelNew = int64(9_000_000_000_001)
	msqStressGiB         = uint64(1 << 30)
)

var (
	msqStressSeedRows       = envInt("STRESS_SEED_ROWS", 5000)
	msqStressWorkers        = envInt("STRESS_WORKERS", 32)
	msqStressDataInitialMB  = envInt("MSSQL_STRESS_DATA_INITIAL_MB", 4096)
	msqStressDataMaxMB      = envInt("MSSQL_STRESS_DATA_MAX_MB", 8192)
	msqStressLogInitialMB   = envInt("MSSQL_STRESS_LOG_INITIAL_MB", 4096)
	msqStressLogMaxMB       = envInt("MSSQL_STRESS_LOG_MAX_MB", 8192)
	msqStressMinFreeDiskGiB = envInt("STRESS_MIN_FREE_DISK_GIB", 32)
)

func msqStressAvailableDiskBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

type msqStressTable struct {
	name   string
	types  bool
	nextID atomic.Int64
}

type msqStressTableSet struct {
	mu     sync.RWMutex
	tables []*msqStressTable
}

func (s *msqStressTableSet) add(t *msqStressTable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables = append(s.tables, t)
}

func (s *msqStressTableSet) pick(rng *rand.Rand) *msqStressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tables[rng.Intn(len(s.tables))]
}

func (s *msqStressTableSet) snapshot() []*msqStressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*msqStressTable, len(s.tables))
	copy(out, s.tables)
	return out
}

func msqStressSourceContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string, string) {
	t.Helper()
	container, err := tcmssql.Run(
		ctx,
		"mcr.microsoft.com/mssql/server:2022-latest",
		tcmssql.WithAcceptEULA(),
		tcmssql.WithPassword(msqStressPassword),
		// SQL Server Agent runs the CDC capture/cleanup jobs.
		testcontainers.WithEnv(map[string]string{"MSSQL_AGENT_ENABLED": "true"}),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("1433/tcp"),
				wait.ForLog("Recovery is complete."),
				wait.ForSQL("1433/tcp", "sqlserver", func(host string, port string) string {
					portNum, _, _ := strings.Cut(port, "/")
					return fmt.Sprintf("server=%s;user id=sa;password=%s;port=%s;database=master;encrypt=disable", host, msqStressPassword, portNum)
				}),
			).WithDeadline(4*time.Minute),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "1433")
	require.NoError(t, err)
	return container, host, port.Port()
}

func msqStressOpenDB(t *testing.T, host, port, database string, maxConns int) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("sqlserver://sa:%s@%s:%s?database=%s&encrypt=disable", msqStressPasswordEnc, host, port, database)
	db, err := sql.Open("sqlserver", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxLifetime(5 * time.Minute)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func msqStressCaptureJobActive(ctx context.Context, db *sql.DB) (bool, error) {
	var active bool
	err := db.QueryRowContext(ctx, `
		SELECT CONVERT(bit, CASE WHEN EXISTS (
			SELECT 1
			FROM msdb.dbo.sysjobactivity AS activity
			JOIN msdb.dbo.sysjobs AS job ON job.job_id = activity.job_id
			WHERE job.name = @p1
				AND activity.session_id = (SELECT MAX(session_id) FROM msdb.dbo.syssessions)
				AND activity.run_requested_date IS NOT NULL
				AND activity.stop_execution_date IS NULL
		) THEN 1 ELSE 0 END)`, "cdc."+msqStressDB+"_capture").Scan(&active)
	return active, err
}

func msqStressAgentJobBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "already running") ||
		strings.Contains(message, "pending request") ||
		strings.Contains(message, "not currently running")
}

// msqStressExec retries transient victim errors (deadlocks, snapshot write
// conflicts) that are expected when DDL phases and a delete-all transaction
// run against tables the workers are hammering.
func msqStressExec(ctx context.Context, db *sql.DB, query string, args ...any) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		result, err := db.ExecContext(ctx, query, args...)
		if err == nil {
			affected, _ := result.RowsAffected()
			return affected, nil
		}
		lastErr = err
		msg := err.Error()
		if !strings.Contains(msg, "deadlock") && !strings.Contains(msg, "1205") {
			return 0, err
		}
		time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
	}
	return 0, lastErr
}

func msqStressEnableCDC(ctx context.Context, db *sql.DB, table, instance string) error {
	_, err := msqStressExec(ctx, db, fmt.Sprintf(`
		EXEC sys.sp_cdc_enable_table
			@source_schema = N'dbo',
			@source_name = N'%s',
			@role_name = NULL,
			@capture_instance = N'%s',
			@supports_net_changes = 0
	`, table, instance))
	return err
}

func msqStressDisableCDC(ctx context.Context, db *sql.DB, table, instance string) error {
	_, err := msqStressExec(ctx, db, fmt.Sprintf(`
		EXEC sys.sp_cdc_disable_table
			@source_schema = N'dbo',
			@source_name = N'%s',
			@capture_instance = N'%s'
	`, table, instance))
	return err
}

// msqStressRecreateCapture is SQL Server's documented schema-change path: a
// capture instance freezes its column list, so after DDL a new instance is
// created and the old one dropped. The stream restart then finds the resume
// cursor below the new instance's start_lsn and re-snapshots the table.
func msqStressRecreateCapture(ctx context.Context, db *sql.DB, table, oldInstance, newInstance string) error {
	if err := msqStressEnableCDC(ctx, db, table, newInstance); err != nil {
		return fmt.Errorf("enable capture instance %s: %w", newInstance, err)
	}
	if err := msqStressDisableCDC(ctx, db, table, oldInstance); err != nil {
		return fmt.Errorf("disable capture instance %s: %w", oldInstance, err)
	}
	return nil
}

func msqStressCreatePlain(ctx context.Context, db *sql.DB, name string, seedRows int, legacy bool) error {
	legacyCol := ""
	legacySelect := ""
	legacyName := ""
	if legacy {
		legacyCol = "legacy NVARCHAR(60) NULL,"
		legacyName = ", legacy"
		legacySelect = ", CONCAT(N'legacy-', value)"
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE dbo.%[1]s (
			id BIGINT NOT NULL PRIMARY KEY,
			val BIGINT NOT NULL,
			payload NVARCHAR(400) NOT NULL,
			%[2]s
			updated_at DATETIME2(6) NOT NULL
		);
		INSERT INTO dbo.%[1]s (id, val, payload%[3]s, updated_at)
		SELECT value, value, CONCAT(N'seed-', value)%[4]s, SYSUTCDATETIME()
		FROM GENERATE_SERIES(CONVERT(bigint, 1), CONVERT(bigint, %[5]d));
	`, name, legacyCol, legacyName, legacySelect, seedRows))
	return err
}

func msqStressCreateTypes(ctx context.Context, db *sql.DB, seedRows int) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE dbo.%[1]s (
			id BIGINT NOT NULL PRIMARY KEY,
			val BIGINT NOT NULL,
			ival INT NOT NULL,
			dec_amount DECIMAL(18,4) NOT NULL,
			name NVARCHAR(120) NOT NULL,
			body NVARCHAR(MAX) NOT NULL,
			ts6 DATETIME2(6) NOT NULL,
			tzo DATETIMEOFFSET(6) NOT NULL,
			d DATE NOT NULL,
			flag BIT NOT NULL,
			bin VARBINARY(64) NOT NULL,
			fval FLOAT NOT NULL
		);
		INSERT INTO dbo.%[1]s (id, val, ival, dec_amount, name, body, ts6, tzo, d, flag, bin, fval)
		SELECT value, value, value %% 100000,
			CONVERT(decimal(18,4), value) / 7,
			CONCAT(N'name-', value),
			CONCAT(N'body-', value, REPLICATE(N'x', value %% 200)),
			SYSUTCDATETIME(), SYSDATETIMEOFFSET(), CONVERT(date, SYSUTCDATETIME()),
			value %% 2, CONVERT(varbinary(64), CONCAT('bin-', value)), CONVERT(float, value) / 3.0
		FROM GENERATE_SERIES(CONVERT(bigint, 1), CONVERT(bigint, %[2]d));
	`, msqStressTypes, seedRows))
	return err
}

// msqStreamCtrl runs the streaming pipeline and supports test-driven restarts:
// SQL Server CDC discovers tables and capture instances only at stream start,
// so topology changes are absorbed by restarting the run, which must resume
// every unchanged table from the destination's max _cdc_lsn.
type msqStreamCtrl struct {
	cfg       *config.IngestConfig
	mu        sync.Mutex
	restartMu sync.Mutex
	cancel    context.CancelFunc
	done      chan error
	restarts  int
	// aborted unblocks every pending restart once the stream has failed for
	// good; without it a stolen exit value leaves restarts waiting forever on
	// an empty channel and deadlocks the whole test.
	aborted   chan struct{}
	abortOnce sync.Once
	fatalErr  error
}

func (c *msqStreamCtrl) start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	c.mu.Lock()
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()
	go func() { done <- pipeline.New(c.cfg).Run(runCtx) }()
}

func msqStressIsCancel(err error) bool {
	return err == nil || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled")
}

func (c *msqStreamCtrl) abort(err error) {
	c.abortOnce.Do(func() {
		c.mu.Lock()
		c.fatalErr = err
		c.mu.Unlock()
		close(c.aborted)
	})
}

func (c *msqStreamCtrl) fatal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fatalErr
}

func (c *msqStreamCtrl) restart(ctx context.Context, t *testing.T, reason string) error {
	return c.withStreamStopped(ctx, t, reason, nil)
}

// withStreamStopped cancels the running stream, waits for it to wind down,
// runs fn (nil-safe) while no consumer is querying the change tables, and
// starts a fresh stream. Capture-instance swaps must run inside fn: dropping
// an instance while the stream reads its change table fails with error 22837.
func (c *msqStreamCtrl) withStreamStopped(ctx context.Context, t *testing.T, reason string, fn func() error) error {
	c.restartMu.Lock()
	defer c.restartMu.Unlock()
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	waitStart := time.Now()
	cancel()
	select {
	case err := <-done:
		if !msqStressIsCancel(err) {
			c.abort(err)
			return fmt.Errorf("stream exited with unexpected error during restart (%s): %w", reason, err)
		}
	case <-c.aborted:
		return fmt.Errorf("restart (%s) aborted: stream already failed: %w", reason, c.fatal())
	case <-time.After(3 * time.Minute):
		err := fmt.Errorf("stream did not exit within 3m of cancellation (%s)", reason)
		c.abort(err)
		return err
	}
	windDown := time.Since(waitStart).Round(time.Millisecond)
	var fnErr error
	if fn != nil {
		fnErr = fn()
	}
	c.mu.Lock()
	c.restarts++
	n := c.restarts
	c.mu.Unlock()
	c.start(ctx)
	if fnErr != nil {
		return fmt.Errorf("stopped-stream work failed (%s): %w", reason, fnErr)
	}
	t.Logf("stream restart %d after %v wind-down: %s", n, windDown, reason)
	return nil
}

// exitedUnexpectedly probes for a stream exit outside a controlled restart.
// A detected exit aborts the controller so pending restarts fail fast instead
// of waiting on the exit value this probe consumed.
func (c *msqStreamCtrl) exitedUnexpectedly() (error, bool) {
	if !c.restartMu.TryLock() {
		return nil, false
	}
	defer c.restartMu.Unlock()
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			err = errors.New("streaming pipeline returned nil while the load was still running")
		}
		c.abort(err)
		return err, true
	default:
		return nil, false
	}
}

func (c *msqStreamCtrl) stop(t *testing.T) {
	c.restartMu.Lock()
	defer c.restartMu.Unlock()
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	cancel()
	select {
	case err := <-done:
		if !msqStressIsCancel(err) {
			t.Fatalf("streaming pipeline exited with unexpected error on shutdown: %v", err)
		}
	case <-c.aborted:
		t.Fatalf("streaming pipeline already failed: %v", c.fatal())
	case <-time.After(60 * time.Second):
		t.Fatal("streaming pipeline did not exit within 60s of cancellation")
	}
}

func (c *msqStreamCtrl) restartCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.restarts
}

// Canonical cross-engine row comparison: every column is rendered to a value
// both engines agree on exactly — temporals as microseconds since epoch,
// binary as hex, numerics as normalized decimal text, floats re-formatted
// from the parsed float64.
type msqColumnKind int

const (
	msqKindInt msqColumnKind = iota
	msqKindNum
	msqKindText
	msqKindTimestamp
	msqKindTimestampTZ
	msqKindDate
	msqKindBool
	msqKindHex
	msqKindFloat
)

type msqStressColumn struct {
	name string
	kind msqColumnKind
}

func msqStressSourceColumns(ctx context.Context, db *sql.DB, table string) ([]msqStressColumn, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = 'dbo' AND TABLE_NAME = @p1
		ORDER BY ORDINAL_POSITION`, table)
	if err != nil {
		return nil, fmt.Errorf("list source columns for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var columns []msqStressColumn
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		var kind msqColumnKind
		switch strings.ToLower(dataType) {
		case "bigint", "int", "smallint", "tinyint":
			kind = msqKindInt
		case "decimal", "numeric", "money", "smallmoney":
			kind = msqKindNum
		case "datetime2", "datetime", "smalldatetime":
			kind = msqKindTimestamp
		case "datetimeoffset":
			kind = msqKindTimestampTZ
		case "date":
			kind = msqKindDate
		case "bit":
			kind = msqKindBool
		case "binary", "varbinary":
			kind = msqKindHex
		case "float", "real":
			kind = msqKindFloat
		default:
			kind = msqKindText
		}
		columns = append(columns, msqStressColumn{name: name, kind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("source table %s has no columns", table)
	}
	return columns, nil
}

func msqStressSourceExpr(c msqStressColumn) string {
	quoted := "t.[" + c.name + "]"
	switch c.kind {
	case msqKindNum:
		return "CONVERT(varchar(80), " + quoted + ")"
	case msqKindTimestamp:
		return "DATEDIFF_BIG(MICROSECOND, CONVERT(datetime2(6), '1970-01-01'), " + quoted + ")"
	case msqKindTimestampTZ:
		return "DATEDIFF_BIG(MICROSECOND, CONVERT(datetime2(6), '1970-01-01'), CONVERT(datetime2(6), SWITCHOFFSET(" + quoted + ", '+00:00')))"
	case msqKindDate:
		return "DATEDIFF(DAY, '1970-01-01', " + quoted + ")"
	case msqKindBool:
		return "CONVERT(int, " + quoted + ")"
	case msqKindHex:
		return "UPPER(CONVERT(varchar(300), " + quoted + ", 2))"
	case msqKindFloat:
		return "CONVERT(varchar(60), " + quoted + ", 3)"
	default:
		return quoted
	}
}

func msqStressDestExpr(c msqStressColumn) string {
	quoted := "t." + quoteStressIdentifier(strings.ToLower(c.name))
	switch c.kind {
	case msqKindNum, msqKindFloat:
		return quoted + "::text"
	case msqKindTimestamp, msqKindTimestampTZ:
		return "(EXTRACT(EPOCH FROM " + quoted + ") * 1000000)::bigint"
	case msqKindDate:
		return "(" + quoted + " - DATE '1970-01-01')"
	case msqKindBool:
		return quoted + "::int"
	case msqKindHex:
		return "UPPER(encode(" + quoted + ", 'hex'))"
	default:
		return quoted
	}
}

func msqStressNormalizeNumeric(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	switch s {
	case "", "-":
		return "0"
	}
	return s
}

func msqStressCanonicalValue(kind msqColumnKind, v any) (string, error) {
	if v == nil {
		return "<null>", nil
	}
	asString := func() string {
		switch value := v.(type) {
		case string:
			return value
		case []byte:
			return string(value)
		default:
			return fmt.Sprintf("%v", value)
		}
	}
	switch kind {
	case msqKindNum:
		return msqStressNormalizeNumeric(asString()), nil
	case msqKindFloat:
		f, err := strconv.ParseFloat(asString(), 64)
		if err != nil {
			return "", fmt.Errorf("parse float %q: %w", asString(), err)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	case msqKindBool:
		if b, ok := v.(bool); ok {
			if b {
				return "1", nil
			}
			return "0", nil
		}
		return asString(), nil
	default:
		return asString(), nil
	}
}

type msqStressRow struct {
	id        int64
	canonical string
}

func msqStressFetchSourceChunk(ctx context.Context, db *sql.DB, table string, columns []msqStressColumn, offset, limit int64) ([]msqStressRow, error) {
	exprs := make([]string, len(columns))
	for i, c := range columns {
		exprs[i] = msqStressSourceExpr(c)
	}
	query := fmt.Sprintf(
		"SELECT t.[id] AS row_pk, %s FROM dbo.[%s] AS t ORDER BY t.[id] OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY",
		strings.Join(exprs, ", "), table,
	)
	rows, err := db.QueryContext(ctx, query, offset, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return msqStressScanRows(rows.Next, rows.Scan, rows.Err, columns)
}

func msqStressFetchDestChunk(ctx context.Context, pool *pgxpool.Pool, destTable string, columns []msqStressColumn, offset, limit int64) ([]msqStressRow, error) {
	exprs := make([]string, len(columns))
	for i, c := range columns {
		exprs[i] = msqStressDestExpr(c)
	}
	query := fmt.Sprintf(
		`SELECT t.id AS row_pk, %s FROM %s.%s AS t WHERE NOT t."_cdc_deleted" ORDER BY t.id LIMIT $1 OFFSET $2`,
		strings.Join(exprs, ", "), quoteStressIdentifier(msqStressDestSchema), quoteStressIdentifier(destTable),
	)
	rows, err := pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return msqStressScanRows(rows.Next, rows.Scan, rows.Err, columns)
}

func msqStressScanRows(next func() bool, scan func(...any) error, rowsErr func() error, columns []msqStressColumn) ([]msqStressRow, error) {
	var out []msqStressRow
	for next() {
		dest := make([]any, len(columns)+1)
		for i := range dest {
			dest[i] = new(any)
		}
		if err := scan(dest...); err != nil {
			return nil, err
		}
		id, err := msqStressToInt64(*dest[0].(*any))
		if err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		parts := make([]string, len(columns))
		for i, c := range columns {
			canonical, err := msqStressCanonicalValue(c.kind, *dest[i+1].(*any))
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", c.name, err)
			}
			parts[i] = c.name + "=" + canonical
		}
		out = append(out, msqStressRow{id: id, canonical: strings.Join(parts, "|")})
	}
	return out, rowsErr()
}

func msqStressToInt64(v any) (int64, error) {
	switch value := v.(type) {
	case int64:
		return value, nil
	case int32:
		return int64(value), nil
	case int:
		return int64(value), nil
	case []byte:
		return strconv.ParseInt(string(value), 10, 64)
	case string:
		return strconv.ParseInt(value, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected integer type %T", v)
	}
}

func msqStressCompareChunkRange(ctx context.Context, src *sql.DB, dst *pgxpool.Pool, table string, columns []msqStressColumn, offset, limit int64) error {
	srcRows, err := msqStressFetchSourceChunk(ctx, src, table, columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: source fetch: %w", table, offset, err)
	}
	dstRows, err := msqStressFetchDestChunk(ctx, dst, "dbo_"+table, columns, offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: destination fetch: %w", table, offset, err)
	}
	if len(srcRows) != len(dstRows) {
		return fmt.Errorf("%s offset %d: row count mismatch: source=%d destination=%d", table, offset, len(srcRows), len(dstRows))
	}
	for i := range srcRows {
		s, d := srcRows[i], dstRows[i]
		if s.id != d.id || s.canonical != d.canonical {
			return fmt.Errorf("%s: content mismatch at id=%d:\n  source:      %s\n  destination: {id:%d row:%s}",
				table, s.id, s.canonical, d.id, d.canonical)
		}
	}
	return nil
}

func msqStressCompareAll(ctx context.Context, src *sql.DB, dst *pgxpool.Pool, tables []*msqStressTable) error {
	type chunk struct {
		table   string
		columns []msqStressColumn
		offset  int64
	}
	var chunks []chunk
	for _, tbl := range tables {
		columns, err := msqStressSourceColumns(ctx, src, tbl.name)
		if err != nil {
			return err
		}
		var count int64
		if err := src.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT_BIG(*) FROM dbo.[%s]", tbl.name)).Scan(&count); err != nil {
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
			if err := msqStressCompareChunkRange(ctx, src, dst, c.table, c.columns, c.offset, stressCompareChunk); err != nil {
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

type msqStressTruth struct {
	count int64
	sum   string
}

func msqStressSourceTruth(ctx context.Context, db *sql.DB, table string) (msqStressTruth, error) {
	var tr msqStressTruth
	err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT_BIG(*), CONVERT(varchar(80), COALESCE(SUM(val), 0)) FROM dbo.[%s]", table)).Scan(&tr.count, &tr.sum)
	tr.sum = msqStressNormalizeNumeric(tr.sum)
	return tr, err
}

func msqStressDestTruth(ctx context.Context, pool *pgxpool.Pool, table string) (msqStressTruth, error) {
	var tr msqStressTruth
	err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*), COALESCE(SUM(val), 0)::text FROM %s.%s WHERE NOT "_cdc_deleted"`,
		quoteStressIdentifier(msqStressDestSchema), quoteStressIdentifier("dbo_"+table),
	)).Scan(&tr.count, &tr.sum)
	tr.sum = msqStressNormalizeNumeric(tr.sum)
	return tr, err
}

// msqStressDiffIDs reports which live source ids are missing from the
// destination and whether they are absent entirely (delivery loss) or present
// only as tombstones (spurious delete), plus live destination ids the source
// no longer has (missed delete).
func msqStressDiffIDs(t *testing.T, ctx context.Context, src *sql.DB, dst *pgxpool.Pool, table string) {
	t.Helper()
	srcIDs := map[int64]bool{}
	rows, err := src.QueryContext(ctx, fmt.Sprintf("SELECT id FROM dbo.[%s]", table))
	if err != nil {
		t.Logf("  %s: source id fetch failed: %v", table, err)
		return
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			srcIDs[id] = true
		}
	}
	_ = rows.Close()

	destLive := map[int64]bool{}
	destDeleted := map[int64]bool{}
	destTable := quoteStressIdentifier(msqStressDestSchema) + "." + quoteStressIdentifier("dbo_"+table)
	pgRows, err := dst.Query(ctx, fmt.Sprintf(`SELECT id, "_cdc_deleted" FROM %s`, destTable))
	if err != nil {
		t.Logf("  %s: destination id fetch failed: %v", table, err)
		return
	}
	for pgRows.Next() {
		var id int64
		var deleted bool
		if err := pgRows.Scan(&id, &deleted); err == nil {
			if deleted {
				destDeleted[id] = true
			} else {
				destLive[id] = true
			}
		}
	}
	pgRows.Close()

	var absent, tombstoned, extraLive []int64
	for id := range srcIDs {
		switch {
		case destLive[id]:
		case destDeleted[id]:
			tombstoned = append(tombstoned, id)
		default:
			absent = append(absent, id)
		}
	}
	for id := range destLive {
		if !srcIDs[id] {
			extraLive = append(extraLive, id)
		}
	}
	cap := func(ids []int64) []int64 {
		if len(ids) > 40 {
			return ids[:40]
		}
		return ids
	}
	if len(absent)+len(tombstoned)+len(extraLive) == 0 {
		t.Logf("  %s: id sets match (%d live rows)", table, len(srcIDs))
		return
	}
	t.Logf("  %s: %d source ids absent from destination %v; %d spuriously tombstoned %v; %d extra live in destination %v",
		table, len(absent), cap(absent), len(tombstoned), cap(tombstoned), len(extraLive), cap(extraLive))
}

func msqStressDumpCDCState(t *testing.T, ctx context.Context, db *sql.DB, tables []*msqStressTable) {
	t.Helper()
	var maxLSN sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT CONVERT(varchar(20), sys.fn_cdc_get_max_lsn(), 2)").Scan(&maxLSN); err == nil {
		t.Logf("  fn_cdc_get_max_lsn=%s", maxLSN.String)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT capture_instance, CONVERT(varchar(20), start_lsn, 2)
		FROM cdc.change_tables WHERE end_lsn IS NULL`)
	if err == nil {
		for rows.Next() {
			var instance, startLSN string
			if err := rows.Scan(&instance, &startLSN); err == nil {
				t.Logf("  capture instance %s: start_lsn=%s", instance, startLSN)
			}
		}
		_ = rows.Close()
	}
	errRows, err := db.QueryContext(ctx, `
		SELECT TOP 3 entry_time, error_message FROM sys.dm_cdc_errors ORDER BY entry_time DESC`)
	if err == nil {
		for errRows.Next() {
			var entryTime time.Time
			var message sql.NullString
			if err := errRows.Scan(&entryTime, &message); err == nil {
				t.Logf("  cdc error at %v: %s", entryTime, message.String)
			}
		}
		_ = errRows.Close()
	}
	_ = tables
}

func msqStressDestColumns(ctx context.Context, pool *pgxpool.Pool, destTable string) (map[string]stressColumn, error) {
	rows, err := pool.Query(ctx, `
		SELECT column_name, udt_name, COALESCE(numeric_precision, -1), COALESCE(numeric_scale, -1)
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2`, msqStressDestSchema, destTable)
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

func msqStressValidateSchemas(ctx context.Context, src *sql.DB, dst *pgxpool.Pool, tables []*msqStressTable) error {
	for _, tbl := range tables {
		sourceColumns, err := msqStressSourceColumns(ctx, src, tbl.name)
		if err != nil {
			return fmt.Errorf("source schema %s: %w", tbl.name, err)
		}
		destColumns, err := msqStressDestColumns(ctx, dst, "dbo_"+tbl.name)
		if err != nil {
			return fmt.Errorf("destination schema %s: %w", tbl.name, err)
		}
		for _, col := range sourceColumns {
			if _, ok := destColumns[strings.ToLower(col.name)]; !ok {
				return fmt.Errorf("destination table %s is missing active source column %s", tbl.name, col.name)
			}
		}
		for _, meta := range []string{"_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"} {
			if _, ok := destColumns[meta]; !ok {
				return fmt.Errorf("destination table %s is missing CDC metadata column %s", tbl.name, meta)
			}
		}
		if tbl.name == msqStressEvolving {
			val, ok := destColumns["val"]
			if !ok || val.udt != "numeric" || val.precision != 28 || val.scale != 4 {
				return fmt.Errorf("evolving table val column must be numeric(28,4) after widening, got %+v (present=%t)", val, ok)
			}
			for _, removed := range []string{"legacy", "segment"} {
				if _, retained := destColumns[removed]; !retained {
					return fmt.Errorf("soft-removed destination column %s is missing on %s", removed, tbl.name)
				}
			}
			sourceNames := make(map[string]bool, len(sourceColumns))
			for _, col := range sourceColumns {
				sourceNames[strings.ToLower(col.name)] = true
			}
			if sourceNames["legacy"] || sourceNames["segment"] {
				return fmt.Errorf("removed source columns still exist on %s: %v", tbl.name, sourceNames)
			}
			if !sourceNames["cohort"] {
				return fmt.Errorf("renamed source column cohort is missing on %s", tbl.name)
			}
		}
	}
	return nil
}

func TestMSSQLCDC_StressComplexWorkload(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}
	if msqStressDataMaxMB < msqStressDataInitialMB || msqStressLogMaxMB < msqStressLogInitialMB {
		t.Fatal("SQL Server stress file maximums must be greater than or equal to their initial sizes")
	}
	availableDisk, err := msqStressAvailableDiskBytes(".")
	require.NoError(t, err, "check available disk before starting stress containers")
	minimumFreeDisk := uint64(msqStressMinFreeDiskGiB) * msqStressGiB
	if availableDisk < minimumFreeDisk {
		t.Fatalf("stress test requires at least %d GiB free, only %.1f GiB is available",
			msqStressMinFreeDiskGiB, float64(availableDisk)/float64(msqStressGiB))
	}
	t.Logf("disk preflight: %.1f GiB free; requiring %d GiB; SQL Server files capped at %.1f GiB combined",
		float64(availableDisk)/float64(msqStressGiB), msqStressMinFreeDiskGiB,
		float64(msqStressDataMaxMB+msqStressLogMaxMB)/1024)

	sourceContainer, host, port := msqStressSourceContainer(t, ctx)
	destConnString, destPool := stressDestContainer(t, ctx)
	_, err = destPool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoteStressIdentifier(msqStressDestSchema))
	require.NoError(t, err)

	masterDB := msqStressOpenDB(t, host, port, "master", 4)
	// Snapshot isolation lets the pipeline snapshot without blocking the
	// writers. SIMPLE recovery permits inactive log reuse without log backups.
	// Pre-sizing removes file-growth pauses; MAXSIZE bounds protect the host.
	// Delayed durability cannot be combined with CDC.
	_, err = masterDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE DATABASE [%[1]s];
		ALTER DATABASE [%[1]s] SET RECOVERY SIMPLE;
		ALTER DATABASE [%[1]s] MODIFY FILE (
			NAME = N'%[1]s', SIZE = %[2]dMB, MAXSIZE = %[3]dMB, FILEGROWTH = 512MB
		);
		ALTER DATABASE [%[1]s] MODIFY FILE (
			NAME = N'%[1]s_log', SIZE = %[4]dMB, MAXSIZE = %[5]dMB, FILEGROWTH = 64MB
		);
		ALTER DATABASE [%[1]s] SET ALLOW_SNAPSHOT_ISOLATION ON;
	`, msqStressDB, msqStressDataInitialMB, msqStressDataMaxMB, msqStressLogInitialMB, msqStressLogMaxMB))
	require.NoError(t, err)

	loadDB := msqStressOpenDB(t, host, port, msqStressDB, max(32, msqStressWorkers+8))
	var recoveryModel string
	require.NoError(t, loadDB.QueryRowContext(
		ctx,
		"SELECT recovery_model_desc FROM sys.databases WHERE name = DB_NAME()",
	).Scan(&recoveryModel))
	t.Logf("source database: recovery=%s, data=%d/%d MiB, log=%d/%d MiB",
		recoveryModel, msqStressDataInitialMB, msqStressDataMaxMB, msqStressLogInitialMB, msqStressLogMaxMB)
	_, err = loadDB.ExecContext(ctx, "EXEC sys.sp_cdc_enable_db")
	require.NoError(t, err)

	tables := &msqStressTableSet{}
	seedStart := time.Now()
	for i := 0; i < stressInitialTables; i++ {
		name := stressTableName(i)
		require.NoError(t, msqStressCreatePlain(ctx, loadDB, name, msqStressSeedRows, false))
		require.NoError(t, msqStressEnableCDC(ctx, loadDB, name, "dbo_"+name))
		tbl := &msqStressTable{name: name}
		tbl.nextID.Store(int64(msqStressSeedRows))
		tables.add(tbl)
	}
	require.NoError(t, msqStressCreatePlain(ctx, loadDB, msqStressEvolving, msqStressSeedRows, true))
	require.NoError(t, msqStressEnableCDC(ctx, loadDB, msqStressEvolving, "dbo_"+msqStressEvolving))
	evolvingTbl := &msqStressTable{name: msqStressEvolving}
	evolvingTbl.nextID.Store(int64(msqStressSeedRows))
	tables.add(evolvingTbl)
	require.NoError(t, msqStressCreateTypes(ctx, loadDB, msqStressSeedRows))
	require.NoError(t, msqStressEnableCDC(ctx, loadDB, msqStressTypes, "dbo_"+msqStressTypes))
	typesTbl := &msqStressTable{name: msqStressTypes, types: true}
	typesTbl.nextID.Store(int64(msqStressSeedRows))
	tables.add(typesTbl)
	seededRows := (stressInitialTables + 2) * msqStressSeedRows
	t.Logf("seeded %d rows across %d CDC-enabled tables in %v", seededRows, stressInitialTables+2, time.Since(seedStart).Round(time.Millisecond))

	// The default capture job (500 transactions per scan, 5s polling) cannot
	// keep up with ~1000 autocommit transactions per second; tune it so the
	// harvest, not the test, is the bottleneck being measured.
	_, err = loadDB.ExecContext(ctx, `
		EXEC sys.sp_cdc_change_job @job_type = N'capture', @pollinginterval = 1, @maxtrans = 5000, @maxscans = 200, @continuous = 1;
	`)
	require.NoError(t, err)
	// SQL Server Agent can have an automatic start request pending while CDC is
	// enabled. Keep requesting the stop until it is accepted, then observe the
	// job activity reach idle before starting it with the new parameters.
	restartDeadline := time.Now().Add(60 * time.Second)
	for {
		_, stopErr := loadDB.ExecContext(ctx, "EXEC sys.sp_cdc_stop_job @job_type = N'capture'")
		active, statusErr := msqStressCaptureJobActive(ctx, loadDB)
		require.NoError(t, statusErr, "inspect CDC capture job while stopping")
		if !active {
			break
		}
		if stopErr != nil && !msqStressAgentJobBusy(stopErr) {
			require.NoError(t, stopErr, "stop CDC capture job before applying tuned parameters")
		}
		if time.Now().After(restartDeadline) {
			require.FailNow(t, "CDC capture job did not stop within 60s")
		}
		time.Sleep(time.Second)
	}

	_, err = loadDB.ExecContext(ctx, "EXEC sys.sp_cdc_start_job @job_type = N'capture'")
	if err != nil && msqStressAgentJobBusy(err) {
		active, statusErr := msqStressCaptureJobActive(ctx, loadDB)
		require.NoError(t, statusErr, "inspect CDC capture job after starting")
		if active {
			err = nil
		}
	}
	require.NoError(t, err, "restart CDC capture job with tuned parameters")

	cfg := &config.IngestConfig{
		SourceURI: fmt.Sprintf("mssql+cdc://sa:%s@%s:%s/%s?encrypt=disable&dest_schema=%s&poll_interval=500ms",
			msqStressPasswordEnc, host, port, msqStressDB, msqStressDestSchema),
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       2 * time.Second,
		FlushRecords:        25000,
		Progress:            config.ProgressLog,
	}
	ctrl := &msqStreamCtrl{cfg: cfg, aborted: make(chan struct{})}
	ctrl.start(ctx)

	ddlDelay := stressEventDelay(stressSchemaEvery, stressLoadDuration, 6)
	lateTableDelay := stressEventDelay(stressNewTableEvery, stressLoadDuration, stressLateTables)
	t.Logf("load phase: %v at %d scheduled ops/sec across %d workers, %d late tables every %v, 6 schema phases every %v",
		stressLoadDuration, stressTargetOpsPerSec, msqStressWorkers, stressLateTables, lateTableDelay, ddlDelay)

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()

	var inserts, updates, deletes, pkUpdates, opErrors, completedDDL atomic.Int64
	var scheduled, enqueued, dropped, attempted, statementErrors, zeroAffected atomic.Int64
	var inFlight, maxQueueDepth, totalLatencyNanos, maxLatencyNanos atomic.Int64
	var latencyBuckets [7]atomic.Int64
	// Boxed so atomic.Value always stores one concrete type; storing raw error
	// values panics as soon as two distinct error types are recorded.
	type opErrBox struct{ err error }
	var firstOpErr atomic.Value
	recordOpErr := func(err error) {
		opErrors.Add(1)
		firstOpErr.CompareAndSwap(nil, opErrBox{err: err})
		t.Logf("load op error: %v", err)
	}

	recordLatency := func(elapsed time.Duration) {
		nanos := elapsed.Nanoseconds()
		totalLatencyNanos.Add(nanos)
		for {
			current := maxLatencyNanos.Load()
			if nanos <= current || maxLatencyNanos.CompareAndSwap(current, nanos) {
				break
			}
		}
		bucket := 6
		switch {
		case elapsed < time.Millisecond:
			bucket = 0
		case elapsed < 5*time.Millisecond:
			bucket = 1
		case elapsed < 10*time.Millisecond:
			bucket = 2
		case elapsed < 50*time.Millisecond:
			bucket = 3
		case elapsed < 100*time.Millisecond:
			bucket = 4
		case elapsed < 500*time.Millisecond:
			bucket = 5
		}
		latencyBuckets[bucket].Add(1)
	}

	queueCapacity := max(msqStressWorkers*8, stressTargetOpsPerSec/4)
	operationQueue := make(chan struct{}, queueCapacity)
	loadStarted := time.Now()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(operationQueue)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var emitted int64
		emitDue := func(elapsed time.Duration) {
			if elapsed > stressLoadDuration {
				elapsed = stressLoadDuration
			}
			due := int64(elapsed) * int64(stressTargetOpsPerSec) / int64(time.Second)
			for emitted < due {
				emitted++
				scheduled.Add(1)
				select {
				case operationQueue <- struct{}{}:
					enqueued.Add(1)
					depth := int64(len(operationQueue))
					for {
						current := maxQueueDepth.Load()
						if depth <= current || maxQueueDepth.CompareAndSwap(current, depth) {
							break
						}
					}
				default:
					dropped.Add(1)
				}
			}
		}
		for {
			select {
			case <-loadCtx.Done():
				emitDue(stressLoadDuration)
				return
			case <-ticker.C:
				emitDue(time.Since(loadStarted))
			}
		}
	}()

	for w := 0; w < msqStressWorkers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for range operationQueue {
				attempted.Add(1)
				inFlight.Add(1)
				operationStarted := time.Now()
				tbl := tables.pick(rng)
				roll := rng.Intn(100)
				var affected int64
				var err error
				var id int64
				operation := "insert"
				switch {
				case roll < 40:
					id = tbl.nextID.Add(1)
					if tbl.types {
						affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf(`
							INSERT INTO dbo.%s (id, val, ival, dec_amount, name, body, ts6, tzo, d, flag, bin, fval)
							VALUES (@p1, @p1, @p1 %% 100000, CONVERT(decimal(18,4), @p1) / 7, CONCAT(N'name-', @p1),
								CONCAT(N'body-', @p1, REPLICATE(N'x', @p1 %% 200)), SYSUTCDATETIME(), SYSDATETIMEOFFSET(),
								CONVERT(date, SYSUTCDATETIME()), @p1 %% 2, CONVERT(varbinary(64), CONCAT('bin-', @p1)),
								CONVERT(float, @p1) / 3.0)`, tbl.name), id)
					} else {
						affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf(
							"INSERT INTO dbo.%s (id, val, payload, updated_at) VALUES (@p1, @p2, @p3, SYSUTCDATETIME())", tbl.name,
						),
							id, id, fmt.Sprintf("ins-%d-%d", seed, id))
					}
				case roll < 75:
					operation = "update"
					id = rng.Int63n(tbl.nextID.Load()) + 1
					if tbl.types {
						affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf(`
							UPDATE dbo.%s SET val = val + 1, dec_amount = dec_amount + CONVERT(decimal(18,4), 0.0001),
								body = CONCAT(N'upd-', id, N'-', @p2), ts6 = SYSUTCDATETIME() WHERE id = @p1`, tbl.name),
							id, seed)
					} else {
						affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf(
							"UPDATE dbo.%s SET val = val + 1, payload = @p2, updated_at = SYSUTCDATETIME() WHERE id = @p1", tbl.name,
						),
							id, fmt.Sprintf("upd-%d-%d", seed, id))
					}
				case roll < 90 || tbl.types:
					operation = "delete"
					id = rng.Int63n(tbl.nextID.Load()) + 1
					affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf("DELETE FROM dbo.%s WHERE id = @p1", tbl.name), id)
				default:
					// Primary-key move: exercises the update pairer's replay of the
					// before-image as a delete of the old key.
					operation = "pk-update"
					id = rng.Int63n(tbl.nextID.Load()) + 1
					affected, err = msqStressExec(ctx, loadDB, fmt.Sprintf(
						"UPDATE dbo.%s SET id = id + @p1, updated_at = SYSUTCDATETIME() WHERE id = @p2 AND id < @p1", tbl.name,
					),
						msqStressPKOffset, id)
				}
				if err != nil {
					statementErrors.Add(1)
					recordOpErr(fmt.Errorf("%s %s id=%d: %w", operation, tbl.name, id, err))
				} else {
					switch operation {
					case "insert":
						inserts.Add(affected)
					case "update":
						updates.Add(affected)
					case "delete":
						deletes.Add(affected)
					case "pk-update":
						pkUpdates.Add(affected)
					}
					if affected == 0 {
						zeroAffected.Add(1)
					}
				}
				recordLatency(time.Since(operationStarted))
				inFlight.Add(-1)
			}
		}(int64(w + 1))
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < stressLateTables; i++ {
			select {
			case <-loadCtx.Done():
			case <-time.After(lateTableDelay):
			}
			name := stressTableName(stressInitialTables + i)
			if err := msqStressCreatePlain(ctx, loadDB, name, msqStressLateSeed, false); err != nil {
				recordOpErr(fmt.Errorf("create late table %s: %w", name, err))
				return
			}
			if err := msqStressEnableCDC(ctx, loadDB, name, "dbo_"+name); err != nil {
				recordOpErr(fmt.Errorf("enable CDC on late table %s: %w", name, err))
				return
			}
			tbl := &msqStressTable{name: name}
			tbl.nextID.Store(int64(msqStressLateSeed))
			tables.add(tbl)
			if err := ctrl.restart(ctx, t, "late table "+name); err != nil {
				recordOpErr(err)
				return
			}
			t.Logf("created new table %s mid-stream with %d pre-existing rows", name, msqStressLateSeed)
		}
	}()

	// Schema phases run to completion even if they outlast the load window;
	// the workers stop on schedule and the phases finish shortly after.
	evolving := "dbo." + msqStressEvolving
	ddlPhases := []struct {
		name string
		run  func() error
	}{
		{"add populated segment column (capture instance v2)", func() error {
			if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"ALTER TABLE %s ADD segment NVARCHAR(40) NOT NULL CONSTRAINT df_stress_segment DEFAULT N'segment-default'", evolving,
			)); err != nil {
				return err
			}
			if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET segment = CONCAT(N'segment-', id %% 7), updated_at = SYSUTCDATETIME() WHERE id %% 2 = 0 AND id < @p1", evolving,
			), int64(msqStressSeedRows)); err != nil {
				return err
			}
			return ctrl.withStreamStopped(ctx, t, "capture instance v2 after column add", func() error {
				return msqStressRecreateCapture(ctx, loadDB, msqStressEvolving, "dbo_"+msqStressEvolving, "dbo_"+msqStressEvolving+"_v2")
			})
		}},
		{"drop legacy column mid-stream", func() error {
			if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf("ALTER TABLE %s DROP COLUMN legacy", evolving)); err != nil {
				return err
			}
			_, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET payload = CONCAT(N'post-drop-', id), updated_at = SYSUTCDATETIME() WHERE id %% 13 = 0 AND id < @p1", evolving,
			), int64(msqStressSeedRows))
			return err
		}},
		{"widen val to decimal(28,4) (capture instance v3)", func() error {
			if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN val DECIMAL(28,4) NOT NULL", evolving)); err != nil {
				return err
			}
			if err := ctrl.withStreamStopped(ctx, t, "capture instance v3 after type widening", func() error {
				return msqStressRecreateCapture(ctx, loadDB, msqStressEvolving, "dbo_"+msqStressEvolving+"_v2", "dbo_"+msqStressEvolving+"_v3")
			}); err != nil {
				return err
			}
			_, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET val = val + CONVERT(decimal(28,4), 0.1250), updated_at = SYSUTCDATETIME() WHERE id %% 11 = 0 AND id < @p1", evolving,
			), int64(msqStressSeedRows))
			return err
		}},
		{"rename segment to cohort (capture instance v4)", func() error {
			// A captured column cannot be renamed: sp_rename fails with
			// "Cannot alter column ... because it is 'REPLICATED'". The
			// documented order is disable capture, rename, re-enable; the
			// uncaptured window is healed by the restart's re-snapshot.
			if err := ctrl.withStreamStopped(ctx, t, "capture instance v4 after column rename", func() error {
				if err := msqStressDisableCDC(ctx, loadDB, msqStressEvolving, "dbo_"+msqStressEvolving+"_v3"); err != nil {
					return err
				}
				if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf("EXEC sp_rename '%s.segment', 'cohort', 'COLUMN'", evolving)); err != nil {
					return err
				}
				return msqStressEnableCDC(ctx, loadDB, msqStressEvolving, "dbo_"+msqStressEvolving+"_v4")
			}); err != nil {
				return err
			}
			_, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET val = val + 5, updated_at = SYSUTCDATETIME() WHERE id %% 19 = 0 AND id < @p1", evolving,
			), int64(msqStressSeedRows))
			return err
		}},
		{"delete-all and repopulate in one transaction", func() error {
			// TRUNCATE is rejected on CDC-enabled tables, so the wipe is a
			// transactional delete-all: every prior row must become a tombstone
			// and the repopulated rows must land exactly once.
			_, err := msqStressExec(ctx, loadDB, fmt.Sprintf(`
				BEGIN TRAN;
				DELETE FROM %[1]s;
				INSERT INTO %[1]s (id, val, payload, updated_at, cohort)
				SELECT value, CONVERT(decimal(28,4), value) * 10, CONCAT(N'after-wipe-', value), SYSUTCDATETIME(), CONCAT(N'cohort-', value %% 5)
				FROM GENERATE_SERIES(CONVERT(bigint, 1), CONVERT(bigint, 500));
				INSERT INTO %[1]s (id, val, payload, updated_at, cohort)
				VALUES (%[2]d, 42, N'pk-sentinel', SYSUTCDATETIME(), N'sentinel');
				COMMIT;
			`, evolving, msqStressSentinelOld))
			return err
		}},
		{"primary-key move on the sentinel row", func() error {
			if _, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET id = @p1, val = val + 1, updated_at = SYSUTCDATETIME() WHERE id = @p2", evolving,
			),
				msqStressSentinelNew, msqStressSentinelOld); err != nil {
				return err
			}
			_, err := msqStressExec(ctx, loadDB, fmt.Sprintf(
				"UPDATE %s SET val = val + 7, updated_at = SYSUTCDATETIME() WHERE id %% 23 = 0 AND id < @p1", evolving,
			), int64(msqStressSeedRows))
			return err
		}},
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, phase := range ddlPhases {
			select {
			case <-ctx.Done():
				return
			case <-time.After(ddlDelay):
			}
			if err := phase.run(); err != nil {
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
	for running := true; running; {
		select {
		case <-loadDone:
			running = false
		case <-status.C:
			if err, exited := ctrl.exitedUnexpectedly(); exited {
				t.Logf("stream exited during load phase: %v", err)
				stopLoad()
				<-loadDone
				t.Fatalf("stream exited during load phase: %v", err)
			}
			elapsed := time.Since(loadStarted)
			elapsedSeconds := elapsed.Seconds()
			effective := inserts.Load() + updates.Load() + deletes.Load() + pkUpdates.Load()
			completed := attempted.Load() - inFlight.Load()
			averageLatency := time.Duration(0)
			if completed > 0 {
				averageLatency = time.Duration(totalLatencyNanos.Load() / completed)
			}
			t.Logf("t=%v: scheduled=%d attempted=%d (%.0f/sec), effective=%d (%.0f/sec), dropped=%d, queue=%d/%d (max=%d), latency avg=%v max=%v; mutations=%d/%d/%d/%d, DDL=%d/%d, restarts=%d, errors=%d",
				elapsed.Round(time.Second), scheduled.Load(), attempted.Load(), float64(attempted.Load())/elapsedSeconds,
				effective, float64(effective)/elapsedSeconds, dropped.Load(), len(operationQueue), queueCapacity, maxQueueDepth.Load(),
				averageLatency.Round(time.Microsecond), time.Duration(maxLatencyNanos.Load()).Round(time.Microsecond),
				inserts.Load(), updates.Load(), deletes.Load(), pkUpdates.Load(), completedDDL.Load(), len(ddlPhases),
				ctrl.restartCount(), opErrors.Load())
		}
	}

	if n := opErrors.Load(); n > 0 {
		first, _ := firstOpErr.Load().(opErrBox)
		t.Fatalf("%d load operations failed, first error: %v", n, first.err)
	}
	require.Equal(t, int64(len(ddlPhases)), completedDDL.Load(), "all schema phases must complete")
	require.Equal(t, scheduled.Load(), enqueued.Load()+dropped.Load(), "every scheduled operation must be enqueued or explicitly dropped")
	require.Equal(t, enqueued.Load(), attempted.Load(), "workers must drain every enqueued operation")
	totalOps := inserts.Load() + updates.Load() + deletes.Load() + pkUpdates.Load()
	achieved := float64(totalOps) / stressLoadDuration.Seconds()
	attemptedRate := float64(attempted.Load()) / stressLoadDuration.Seconds()
	dropPercent := float64(0)
	if scheduled.Load() > 0 {
		dropPercent = 100 * float64(dropped.Load()) / float64(scheduled.Load())
	}
	averageLatency := time.Duration(0)
	if attempted.Load() > 0 {
		averageLatency = time.Duration(totalLatencyNanos.Load() / attempted.Load())
	}
	t.Logf("load complete: scheduled=%d, enqueued/attempted=%d (%.0f/sec), dropped=%d (%.2f%%), no-op=%d, statement errors=%d, max queue=%d/%d",
		scheduled.Load(), attempted.Load(), attemptedRate, dropped.Load(), dropPercent, zeroAffected.Load(), statementErrors.Load(),
		maxQueueDepth.Load(), queueCapacity)
	t.Logf("effective mutations: %d (%d inserts, %d updates, %d deletes, %d pk-updates), %.0f/sec; latency avg=%v max=%v; stream restarts=%d",
		totalOps, inserts.Load(), updates.Load(), deletes.Load(), pkUpdates.Load(), achieved,
		averageLatency.Round(time.Microsecond), time.Duration(maxLatencyNanos.Load()).Round(time.Microsecond), ctrl.restartCount())
	t.Logf("statement latency buckets: <1ms=%d, 1-5ms=%d, 5-10ms=%d, 10-50ms=%d, 50-100ms=%d, 100-500ms=%d, >=500ms=%d",
		latencyBuckets[0].Load(), latencyBuckets[1].Load(), latencyBuckets[2].Load(), latencyBuckets[3].Load(),
		latencyBuckets[4].Load(), latencyBuckets[5].Load(), latencyBuckets[6].Load())
	require.GreaterOrEqual(t, achieved, float64(stressTargetOpsPerSec)/2,
		"load generator could not sustain enough pressure for the test to be meaningful")
	require.Positive(t, pkUpdates.Load(), "workload should execute real primary-key updates")
	require.Positive(t, deletes.Load(), "workload should execute real deletes")
	require.Positive(t, ctrl.restartCount(), "late tables and capture-instance recreation must exercise stream restarts")
	finalTables := tables.snapshot()
	require.Len(t, finalTables, stressInitialTables+2+stressLateTables, "all initial, evolving, types, and late tables should exist")

	// The source is quiescent; capture ground truth.
	truths := make(map[string]msqStressTruth, len(finalTables))
	for _, tbl := range finalTables {
		tr, err := msqStressSourceTruth(ctx, loadDB, tbl.name)
		require.NoError(t, err)
		truths[tbl.name] = tr
		t.Logf("source truth %s: count=%d sum=%s", tbl.name, tr.count, tr.sum)
	}

	dumpDiagnostics := func() {
		msqStressDumpCDCState(t, ctx, loadDB, finalTables)
		for _, tbl := range finalTables {
			got, err := msqStressDestTruth(ctx, destPool, tbl.name)
			t.Logf("  %s: want %+v, got %+v (err=%v)", tbl.name, truths[tbl.name], got, err)
			msqStressDiffIDs(t, ctx, loadDB, destPool, tbl.name)
		}
		if err := msqStressCompareAll(ctx, loadDB, destPool, finalTables); err != nil {
			t.Logf("  first content mismatch: %v", err)
		}
		stressDumpContainerLogs(t, ctx, sourceContainer, 120)
	}

	// Convergence: the stream keeps polling; the capture job drains its backlog
	// and every table's aggregates must match the quiescent source exactly.
	deadline := time.Now().Add(stressConvergeTimeout)
	lastProgressLog := time.Now()
	for {
		if err, exited := ctrl.exitedUnexpectedly(); exited && !msqStressIsCancel(err) {
			dumpDiagnostics()
			t.Fatalf("stream exited during convergence: %v", err)
		}
		pending := ""
		for _, tbl := range finalTables {
			got, err := msqStressDestTruth(ctx, destPool, tbl.name)
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
		if compareErr = msqStressCompareAll(ctx, loadDB, destPool, finalTables); compareErr == nil {
			break
		}
		t.Logf("deep comparison attempt %d: %v", attempt, compareErr)
		time.Sleep(5 * time.Second)
	}
	require.NoError(t, compareErr, "row-by-row content comparison failed")
	t.Log("row-by-row content comparison passed for all tables")

	require.NoError(t, msqStressValidateSchemas(ctx, loadDB, destPool, finalTables))

	var softDeleted int64
	for _, tbl := range finalTables {
		var deleted int64
		require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.%s WHERE "_cdc_deleted"`,
			quoteStressIdentifier(msqStressDestSchema), quoteStressIdentifier("dbo_"+tbl.name),
		)).Scan(&deleted))
		softDeleted += deleted
	}
	require.Positive(t, softDeleted, "destination should retain soft-deleted CDC rows")

	var movedLive int64
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.%s WHERE id >= $1 AND NOT "_cdc_deleted"`,
		quoteStressIdentifier(msqStressDestSchema), quoteStressIdentifier("dbo_"+stressTableName(0)),
	),
		msqStressPKOffset).Scan(&movedLive))
	require.Positive(t, movedLive, "primary-key moves must land live rows under the new keys")

	var sentinelLive, sentinelOldLive int64
	evolvingDest := quoteStressIdentifier(msqStressDestSchema) + "." + quoteStressIdentifier("dbo_"+msqStressEvolving)
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE id = $1 AND cohort = 'sentinel' AND NOT "_cdc_deleted"`, evolvingDest,
	),
		msqStressSentinelNew).Scan(&sentinelLive))
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE id = $1 AND NOT "_cdc_deleted"`, evolvingDest,
	),
		msqStressSentinelOld).Scan(&sentinelOldLive))
	require.Equal(t, int64(1), sentinelLive, "sentinel row must live under its moved primary key")
	require.Zero(t, sentinelOldLive, "sentinel row must not remain live under its old primary key")

	ctrl.stop(t)
	t.Logf("PERF SUMMARY: %.0f attempted ops/sec, %.0f effective mutations/sec over %v across %d tables; %.2f%% scheduler drops; %d stream restarts; %d soft-deleted rows retained",
		attemptedRate, achieved, stressLoadDuration, len(finalTables), dropPercent, ctrl.restartCount(), softDeleted)
}
