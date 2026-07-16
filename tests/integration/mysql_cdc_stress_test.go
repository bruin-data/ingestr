//go:build stress

// High-volume MySQL CDC accuracy and performance test. Like the PostgreSQL
// variant it is excluded from the regular integration suite (build tag
// `stress`); run it with `make cdc-mysql-stress-test`.
//
// MySQL CDC is batch catch-up (snapshot, then replay the binlog to the
// position captured at stream start, then exit), so instead of one streaming
// pipeline this test drives repeated pipeline runs while parallel workers
// apply ~1000 inserts/updates/deletes/PK-updates per second — every mid-load
// run exercises the resume-from-LSN path under active writes. The source
// server runs in a non-UTC time zone and a wide-types table covers unsigned
// integer extremes, DECIMAL, JSON, DATE/DATETIME/TIMESTAMP, and binary
// columns. Afterwards the destination must converge to the exact source rows,
// verified by aggregates and canonical row comparison. A final phase alters a
// table mid-binlog and asserts the pipeline fails loudly and recovers via
// --full-refresh.
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

const (
	mysqlStressLateSeedRows = 2000
	mysqlStressTypesTable   = "mstress_types"
	mysqlStressPKOffset     = int64(1_000_000_000)
	// Lenient floor: even constrained CI machines snapshot far faster; a miss
	// indicates a real regression in the snapshot path.
	mysqlStressMinSnapshotRowsPerSec = 1000.0
	mysqlStressDB                    = "stressdb"
	mysqlStressUser                  = "root"
	mysqlStressPassword              = "stresspass"
)

var (
	mysqlStressSeedRows      = envInt("STRESS_SEED_ROWS", 10000)
	mysqlStressInitialTables = envInt("STRESS_INITIAL_TABLES", 2)
)

type mysqlStressTable struct {
	name   string
	types  bool
	nextID atomic.Int64
}

type mysqlStressTableSet struct {
	mu     sync.RWMutex
	tables []*mysqlStressTable
}

func (s *mysqlStressTableSet) add(t *mysqlStressTable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables = append(s.tables, t)
}

func (s *mysqlStressTableSet) pick(rng *rand.Rand) *mysqlStressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tables[rng.Intn(len(s.tables))]
}

func (s *mysqlStressTableSet) snapshot() []*mysqlStressTable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*mysqlStressTable, len(s.tables))
	copy(out, s.tables)
	return out
}

func mysqlStressTableName(i int) string {
	return fmt.Sprintf("mstress_%02d", i)
}

func mysqlStressContainer(t *testing.T, ctx context.Context, cmd []string) (testcontainers.Container, string, string) {
	t.Helper()
	container, err := tcmysql.Run(
		ctx,
		"mysql:8.0",
		tcmysql.WithDatabase(mysqlStressDB),
		tcmysql.WithUsername(mysqlStressUser),
		tcmysql.WithPassword(mysqlStressPassword),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.Cmd = cmd
			return nil
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)
	uri := fmt.Sprintf("mysql://%s:%s@%s:%s/%s", mysqlStressUser, mysqlStressPassword, host, port.Port(), mysqlStressDB)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", mysqlStressUser, mysqlStressPassword, host, port.Port(), mysqlStressDB)
	return container, uri, dsn
}

func mysqlStressOpenDB(t *testing.T, dsn string, maxConns int) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxLifetime(5 * time.Minute)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mysqlStressCreatePlain(ctx context.Context, db *sql.DB, name string) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGINT NOT NULL PRIMARY KEY,
			val BIGINT NOT NULL,
			payload TEXT NOT NULL,
			updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
		)`, name))
	return err
}

func mysqlStressCreateTypes(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGINT NOT NULL PRIMARY KEY,
			val BIGINT NOT NULL,
			u8 TINYINT UNSIGNED NOT NULL,
			u16 SMALLINT UNSIGNED NOT NULL,
			u32 INT UNSIGNED NOT NULL,
			u64 BIGINT UNSIGNED NOT NULL,
			dec_amount DECIMAL(18,4) NOT NULL,
			name VARCHAR(120) NOT NULL,
			body TEXT NOT NULL,
			meta JSON NOT NULL,
			d DATE NOT NULL,
			dt DATETIME(6) NOT NULL,
			ts TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			bin VARBINARY(64) NOT NULL
		)`, mysqlStressTypesTable))
	return err
}

type mysqlStressTypesValues struct {
	u8   uint64
	u16  uint64
	u32  uint64
	u64  uint64
	dec  string
	meta string
	body string
	bin  []byte
}

func mysqlStressTypesFor(id int64) mysqlStressTypesValues {
	v := mysqlStressTypesValues{
		u8:   uint64(id % 251),
		u16:  uint64(id % 65521),
		u32:  uint64(uint32(id * 2654435761)),
		u64:  uint64(id) * 11400714819323198485,
		dec:  fmt.Sprintf("%d.%04d", id%1_000_000_000, id%10000),
		meta: fmt.Sprintf(`{"id": %d, "tags": ["stress", "t%d"], "nested": {"flag": %t}}`, id, id%7, id%2 == 0),
		body: fmt.Sprintf("body-%d-%s", id, strings.Repeat("x", int(id%512))),
		bin:  []byte(fmt.Sprintf("bin-%016d", id%1_000_000)),
	}
	// Pin the documented extremes on a deterministic subset so unsigned
	// wraparound regressions always have witnesses.
	if id%7 == 0 {
		v.u8, v.u16, v.u32 = 255, 65535, 4294967295
	}
	if id%5 == 0 {
		v.u64 = 18446744073709551615
	}
	return v
}

func mysqlStressInsertTypes(ctx context.Context, db *sql.DB, id int64) error {
	v := mysqlStressTypesFor(id)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, val, u8, u16, u32, u64, dec_amount, name, body, meta, d, dt, ts, bin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURDATE(), NOW(6), NOW(6), ?)`, mysqlStressTypesTable),
		id, id, v.u8, v.u16, v.u32, v.u64, v.dec, fmt.Sprintf("name-%d", id), v.body, v.meta, v.bin)
	return err
}

func mysqlStressUpdateTypes(ctx context.Context, db *sql.DB, id int64, salt int64) (int64, error) {
	v := mysqlStressTypesFor(id + salt)
	result, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET val = val + 1, u32 = ?, u64 = ?, dec_amount = ?, body = ?, meta = ?, ts = NOW(6)
		WHERE id = ?`, mysqlStressTypesTable),
		v.u32, v.u64, v.dec, v.body, v.meta, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func mysqlStressSeedTable(ctx context.Context, db *sql.DB, tbl *mysqlStressTable, rows int) error {
	const batch = 500
	for start := int64(1); start <= int64(rows); start += batch {
		end := start + batch - 1
		if end > int64(rows) {
			end = int64(rows)
		}
		var query strings.Builder
		var args []interface{}
		if tbl.types {
			query.WriteString(fmt.Sprintf("INSERT INTO %s (id, val, u8, u16, u32, u64, dec_amount, name, body, meta, d, dt, ts, bin) VALUES ", tbl.name))
			for id := start; id <= end; id++ {
				if id > start {
					query.WriteString(",")
				}
				v := mysqlStressTypesFor(id)
				query.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURDATE(), NOW(6), NOW(6), ?)")
				args = append(args, id, id, v.u8, v.u16, v.u32, v.u64, v.dec, fmt.Sprintf("name-%d", id), v.body, v.meta, v.bin)
			}
		} else {
			query.WriteString(fmt.Sprintf("INSERT INTO %s (id, val, payload, updated_at) VALUES ", tbl.name))
			for id := start; id <= end; id++ {
				if id > start {
					query.WriteString(",")
				}
				query.WriteString("(?, ?, ?, NOW(6))")
				args = append(args, id, id, fmt.Sprintf("seed-%d", id))
			}
		}
		if _, err := db.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}
	tbl.nextID.Store(int64(rows))
	return nil
}

type mysqlStressColumn struct {
	name     string
	dataType string
}

func mysqlStressSourceColumns(ctx context.Context, db *sql.DB, table string) ([]mysqlStressColumn, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, mysqlStressDB, table)
	if err != nil {
		return nil, fmt.Errorf("list source columns for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var columns []mysqlStressColumn
	for rows.Next() {
		var c mysqlStressColumn
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			return nil, err
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("source table %s has no columns", table)
	}
	return columns, nil
}

// mysqlStressCanonicalExpr renders a row as one canonical JSON array string so
// source and destination rows can be compared as text. Binary columns go
// through HEX (JSON cannot hold binary charset strings); everything else uses
// MySQL's deterministic JSON rendering, which normalizes JSON documents,
// keeps DECIMAL scale, and formats temporal values identically on both sides
// as long as the sessions share a time zone.
func mysqlStressCanonicalExpr(columns []mysqlStressColumn) string {
	parts := make([]string, len(columns))
	for i, c := range columns {
		quoted := "`" + c.name + "`"
		switch strings.ToLower(c.dataType) {
		case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob", "bit":
			parts[i] = "HEX(" + quoted + ")"
		default:
			parts[i] = quoted
		}
	}
	return "CAST(JSON_ARRAY(" + strings.Join(parts, ", ") + ") AS CHAR)"
}

type mysqlStressRow struct {
	id        int64
	canonical string
}

func mysqlStressFetchChunk(ctx context.Context, db *sql.DB, table, canonical, extraWhere string, offset, limit int64) ([]mysqlStressRow, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		"SELECT id, %s FROM %s WHERE TRUE%s ORDER BY id LIMIT ? OFFSET ?", canonical, table, extraWhere,
	), limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []mysqlStressRow
	for rows.Next() {
		var r mysqlStressRow
		if err := rows.Scan(&r.id, &r.canonical); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func mysqlStressCompareChunkRange(ctx context.Context, src, dst *sql.DB, table, canonical string, offset, limit int64) error {
	srcRows, err := mysqlStressFetchChunk(ctx, src, table, canonical, "", offset, limit)
	if err != nil {
		return fmt.Errorf("%s offset %d: source fetch: %w", table, offset, err)
	}
	dstRows, err := mysqlStressFetchChunk(ctx, dst, table, canonical, " AND `_cdc_deleted` = FALSE", offset, limit)
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

func mysqlStressCompareAll(ctx context.Context, src, dst *sql.DB, tables []*mysqlStressTable) error {
	type chunk struct {
		table     string
		canonical string
		offset    int64
	}
	var chunks []chunk
	for _, tbl := range tables {
		columns, err := mysqlStressSourceColumns(ctx, src, tbl.name)
		if err != nil {
			return err
		}
		canonical := mysqlStressCanonicalExpr(columns)
		var count int64
		if err := src.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl.name)).Scan(&count); err != nil {
			return fmt.Errorf("count source table %s: %w", tbl.name, err)
		}
		for offset := int64(0); offset < count; offset += stressCompareChunk {
			chunks = append(chunks, chunk{table: tbl.name, canonical: canonical, offset: offset})
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
			if err := mysqlStressCompareChunkRange(ctx, src, dst, c.table, c.canonical, c.offset, stressCompareChunk); err != nil {
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

type mysqlStressTruth struct {
	count int64
	sum   string
}

func mysqlStressSourceTruth(ctx context.Context, db *sql.DB, table string) (mysqlStressTruth, error) {
	var tr mysqlStressTruth
	err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*), CAST(COALESCE(SUM(val), 0) AS CHAR) FROM %s", table)).Scan(&tr.count, &tr.sum)
	return tr, err
}

func mysqlStressDestTruth(ctx context.Context, db *sql.DB, table string) (mysqlStressTruth, error) {
	var tr mysqlStressTruth
	err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*), CAST(COALESCE(SUM(val), 0) AS CHAR) FROM %s WHERE `_cdc_deleted` = FALSE", table)).Scan(&tr.count, &tr.sum)
	return tr, err
}

func TestMySQLCDC_StressComplexWorkload(t *testing.T) {
	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		t.Skip("skipping stress test: Docker provider is not available/healthy")
	}

	sourceContainer, sourceURI, sourceDSN := mysqlStressContainer(t, ctx, []string{
		"--server-id=21777",
		"--log-bin=mysql-bin",
		"--binlog-format=ROW",
		"--binlog-row-image=FULL",
		// Non-UTC source: snapshot and binlog paths must agree on instants.
		"--default-time-zone=+03:00",
		"--max-connections=200",
		// Throwaway load-generator container: skip per-commit redo/binlog fsyncs
		// so single-row autocommit ops aren't capped by disk flush latency. Does
		// not change binlog content or CDC semantics.
		"--innodb-flush-log-at-trx-commit=0",
		"--sync-binlog=0",
	})
	_, destURI, destDSN := mysqlStressContainer(t, ctx, []string{
		"--max-connections=200",
	})

	loadDB := mysqlStressOpenDB(t, sourceDSN, stressWorkers+8)
	// Verification sessions pin time_zone so TIMESTAMP columns render
	// identically on both servers regardless of their server time zones.
	srcVerify := mysqlStressOpenDB(t, sourceDSN+"&time_zone=%27%2B00%3A00%27", stressCompareParallel+2)
	dstVerify := mysqlStressOpenDB(t, destDSN+"&time_zone=%27%2B00%3A00%27", stressCompareParallel+2)

	tables := &mysqlStressTableSet{}
	seedStart := time.Now()
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
	seededRows := (mysqlStressInitialTables + 1) * mysqlStressSeedRows
	t.Logf("seeded %d rows across %d tables in %v", seededRows, mysqlStressInitialTables+1, time.Since(seedStart).Round(time.Millisecond))

	cfg := &config.IngestConfig{
		SourceURI:           sourceURI[:len("mysql")] + "+cdc" + sourceURI[len("mysql"):] + "?server_id=21999",
		DestURI:             destURI,
		IncrementalStrategy: config.StrategyMerge,
	}

	// Initial snapshot: the headline snapshot-throughput measurement.
	snapshotStart := time.Now()
	require.NoError(t, pipeline.New(cfg).Run(ctx), "initial snapshot run failed")
	snapshotDuration := time.Since(snapshotStart)
	snapshotRate := float64(seededRows) / snapshotDuration.Seconds()
	t.Logf("initial snapshot: %d rows in %v (%.0f rows/sec)", seededRows, snapshotDuration.Round(time.Millisecond), snapshotRate)
	require.GreaterOrEqual(t, snapshotRate, mysqlStressMinSnapshotRowsPerSec,
		"snapshot throughput regressed below the acceptance floor")

	lateTableDelay := stressEventDelay(stressNewTableEvery, stressLoadDuration, stressLateTables)
	t.Logf("load phase: %v at ~%d ops/sec across %d workers, %d late tables every %v, catch-up runs back-to-back",
		stressLoadDuration, stressTargetOpsPerSec, stressWorkers, stressLateTables, lateTableDelay)

	loadCtx, stopLoad := context.WithTimeout(ctx, stressLoadDuration)
	defer stopLoad()

	var inserts, updates, deletes, pkUpdates, opErrors atomic.Int64
	var firstOpErr atomic.Value
	recordOpErr := func(err error) {
		opErrors.Add(1)
		firstOpErr.CompareAndSwap(nil, err)
	}

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
							id, id, fmt.Sprintf("ins-%d-%d", seed, id))
					}
					if err != nil {
						recordOpErr(fmt.Errorf("insert %s id=%d: %w", tbl.name, id, err))
					} else {
						inserts.Add(1)
					}
				case roll < 75:
					id := rng.Int63n(tbl.nextID.Load()) + 1
					var affected int64
					var err error
					if tbl.types {
						affected, err = mysqlStressUpdateTypes(ctx, loadDB, id, seed)
					} else {
						var result sql.Result
						result, err = loadDB.ExecContext(ctx,
							fmt.Sprintf("UPDATE %s SET val = val + 1, payload = ?, updated_at = NOW(6) WHERE id = ?", tbl.name),
							fmt.Sprintf("upd-%d-%d", seed, id), id)
						if err == nil {
							affected, _ = result.RowsAffected()
						}
					}
					if err != nil {
						recordOpErr(fmt.Errorf("update %s id=%d: %w", tbl.name, id, err))
					} else {
						updates.Add(affected)
					}
				case roll < 90 || tbl.types:
					id := rng.Int63n(tbl.nextID.Load()) + 1
					result, err := loadDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", tbl.name), id)
					if err != nil {
						recordOpErr(fmt.Errorf("delete %s id=%d: %w", tbl.name, id, err))
					} else {
						affected, _ := result.RowsAffected()
						deletes.Add(affected)
					}
				default:
					// Primary-key update: exercises the delete+insert change pair.
					id := rng.Int63n(tbl.nextID.Load()) + 1
					result, err := loadDB.ExecContext(ctx,
						fmt.Sprintf("UPDATE %s SET id = id + ?, updated_at = NOW(6) WHERE id = ? AND id < ?", tbl.name),
						mysqlStressPKOffset, id, mysqlStressPKOffset)
					if err != nil {
						recordOpErr(fmt.Errorf("pk-update %s id=%d: %w", tbl.name, id, err))
					} else {
						affected, _ := result.RowsAffected()
						pkUpdates.Add(affected)
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

	// Catch-up runner: back-to-back batch pipeline runs while the load is hot.
	// Every run after the first resumes from the destination's MAX(_cdc_lsn).
	var catchupRuns atomic.Int64
	var catchupTotal atomic.Int64 // nanoseconds
	runnerErr := make(chan error, 1)
	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		for loadCtx.Err() == nil {
			start := time.Now()
			if err := pipeline.New(cfg).Run(ctx); err != nil {
				runnerErr <- err
				return
			}
			catchupRuns.Add(1)
			catchupTotal.Add(int64(time.Since(start)))
			select {
			case <-loadCtx.Done():
			case <-time.After(2 * time.Second):
			}
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
		case err := <-runnerErr:
			stopLoad()
			<-loadDone
			t.Fatalf("catch-up run failed during load phase: %v", err)
		case <-status.C:
			t.Logf("t=%v: %d inserts, %d updates, %d deletes, %d pk-updates, %d catch-up runs, %d op errors",
				time.Since(started).Round(time.Second), inserts.Load(), updates.Load(), deletes.Load(),
				pkUpdates.Load(), catchupRuns.Load(), opErrors.Load())
		}
	}
	select {
	case err := <-runnerErr:
		t.Fatalf("catch-up run failed: %v", err)
	case <-runnerDone:
	}

	if n := opErrors.Load(); n > 0 {
		t.Fatalf("%d load operations failed, first error: %v", n, firstOpErr.Load())
	}
	totalOps := inserts.Load() + updates.Load() + deletes.Load() + pkUpdates.Load()
	achieved := float64(totalOps) / stressLoadDuration.Seconds()
	t.Logf("load complete: %d effective ops (%d inserts, %d updates, %d deletes, %d pk-updates), %.0f ops/sec achieved, %d mid-load catch-up runs (avg %v)",
		totalOps, inserts.Load(), updates.Load(), deletes.Load(), pkUpdates.Load(), achieved,
		catchupRuns.Load(), (time.Duration(catchupTotal.Load()) / time.Duration(max(1, catchupRuns.Load()))).Round(time.Millisecond))
	require.GreaterOrEqual(t, achieved, float64(stressTargetOpsPerSec)/2,
		"load generator could not sustain enough pressure for the test to be meaningful")
	require.Positive(t, catchupRuns.Load(), "at least one mid-load catch-up run must complete to exercise resume under load")
	require.Positive(t, pkUpdates.Load(), "workload should execute real primary-key updates")
	require.Positive(t, deletes.Load(), "workload should execute real deletes")

	finalTables := tables.snapshot()
	require.Len(t, finalTables, mysqlStressInitialTables+1+stressLateTables, "all initial, types, and late tables should exist")

	// The source is quiescent; capture ground truth.
	truths := make(map[string]mysqlStressTruth, len(finalTables))
	for _, tbl := range finalTables {
		tr, err := mysqlStressSourceTruth(ctx, srcVerify, tbl.name)
		require.NoError(t, err)
		truths[tbl.name] = tr
		t.Logf("source truth %s: count=%d sum=%s", tbl.name, tr.count, tr.sum)
	}

	dumpDiagnostics := func() {
		for _, tbl := range finalTables {
			got, err := mysqlStressDestTruth(ctx, dstVerify, tbl.name)
			t.Logf("  %s: want %+v, got %+v (err=%v)", tbl.name, truths[tbl.name], got, err)
		}
		if err := mysqlStressCompareAll(ctx, srcVerify, dstVerify, finalTables); err != nil {
			t.Logf("  first content mismatch: %v", err)
		}
		stressDumpContainerLogs(t, ctx, sourceContainer, 120)
	}

	// Convergence: run the batch pipeline until every aggregate matches.
	convergeStart := time.Now()
	deadline := time.Now().Add(stressConvergeTimeout)
	for attempt := 1; ; attempt++ {
		runStart := time.Now()
		if err := pipeline.New(cfg).Run(ctx); err != nil {
			dumpDiagnostics()
			t.Fatalf("convergence run %d failed: %v", attempt, err)
		}
		t.Logf("convergence run %d finished in %v", attempt, time.Since(runStart).Round(time.Millisecond))
		pending := ""
		for _, tbl := range finalTables {
			got, err := mysqlStressDestTruth(ctx, dstVerify, tbl.name)
			if err != nil || got != truths[tbl.name] {
				pending = fmt.Sprintf("%s: want %+v, got %+v (err=%v)", tbl.name, truths[tbl.name], got, err)
				break
			}
		}
		if pending == "" {
			break
		}
		if time.Now().After(deadline) {
			dumpDiagnostics()
			t.Fatalf("destination did not converge within %v; still pending: %s", stressConvergeTimeout, pending)
		}
		t.Logf("convergence pending after run %d: %s", attempt, pending)
		time.Sleep(2 * time.Second)
	}
	convergeDuration := time.Since(convergeStart)
	t.Logf("destination converged on count/sum aggregates for all tables in %v after load stop", convergeDuration.Round(time.Millisecond))

	compareStart := time.Now()
	require.NoError(t, mysqlStressCompareAll(ctx, srcVerify, dstVerify, finalTables), "row-by-row content comparison failed")
	t.Logf("row-by-row content comparison passed for all %d tables in %v", len(finalTables), time.Since(compareStart).Round(time.Millisecond))

	// Deletes and PK updates must leave tombstones behind.
	var softDeleted int64
	for _, tbl := range finalTables {
		var deleted int64
		require.NoError(t, dstVerify.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE `_cdc_deleted` = TRUE", tbl.name)).Scan(&deleted))
		softDeleted += deleted
	}
	require.Positive(t, softDeleted, "destination should retain soft-deleted CDC rows")
	var movedLive, movedTombstones int64
	require.NoError(t, dstVerify.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE id >= ? AND `_cdc_deleted` = FALSE", mysqlStressTableName(0),
	), mysqlStressPKOffset).Scan(&movedLive))
	require.NoError(t, dstVerify.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE id < ? AND `_cdc_deleted` = TRUE", mysqlStressTableName(0),
	), mysqlStressPKOffset).Scan(&movedTombstones))
	t.Logf("pk-update evidence in %s: %d live moved rows, %d tombstones below the move offset", mysqlStressTableName(0), movedLive, movedTombstones)

	// Schema-churn phases. MySQL CDC intentionally rejects schema changes that
	// sit inside the unconsumed binlog range (positional decode cannot be
	// trusted across them) and recovers via --full-refresh; changes made while
	// the destination is caught up flow through schema evolution. Each phase
	// applies DML+DDL, runs the pipeline (recovering with a full refresh where
	// that is the documented path), and then requires exact aggregates plus
	// full row-by-row parity for every table.
	driftTable := mysqlStressTableName(0)
	appendTable := mysqlStressTableName(1)
	churnBase := tables.snapshot()[0].nextID.Add(1000)
	appendBase := tables.snapshot()[1].nextID.Add(1000)
	fullRefreshes := 0

	verifyAllAccurate := func(phase string) {
		verifyDeadline := time.Now().Add(stressConvergeTimeout)
		for {
			pending := ""
			for _, tbl := range finalTables {
				want, err := mysqlStressSourceTruth(ctx, srcVerify, tbl.name)
				require.NoError(t, err, "phase %q: source truth for %s", phase, tbl.name)
				got, err := mysqlStressDestTruth(ctx, dstVerify, tbl.name)
				if err != nil || got != want {
					pending = fmt.Sprintf("%s: want %+v, got %+v (err=%v)", tbl.name, want, got, err)
					break
				}
			}
			if pending == "" {
				break
			}
			if time.Now().After(verifyDeadline) {
				dumpDiagnostics()
				t.Fatalf("phase %q: aggregates did not converge: %s", phase, pending)
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx), "phase %q: convergence run failed", phase)
		}
		require.NoError(t, mysqlStressCompareAll(ctx, srcVerify, dstVerify, finalTables), "phase %q: row-by-row comparison failed", phase)
		t.Logf("phase %q: aggregates and row-by-row content verified for %d tables", phase, len(finalTables))
	}

	execSQL := func(query string, args ...interface{}) error {
		_, err := loadDB.ExecContext(ctx, query, args...)
		return err
	}

	runChurnPhase := func(phase string, expectDrift bool, apply func() error) {
		require.NoError(t, apply(), "phase %q: workload failed", phase)
		err := pipeline.New(cfg).Run(ctx)
		needRefresh := expectDrift
		switch {
		case expectDrift:
			require.Error(t, err, "phase %q must be rejected instead of corrupting rows", phase)
			require.Contains(t, err.Error(), "--full-refresh", "phase %q error should point at the recovery path", phase)
			t.Logf("phase %q correctly rejected: %v", phase, err)
		case err == nil:
			t.Logf("phase %q ingested incrementally without a full refresh", phase)
		default:
			// Accuracy is the gate; the recovery path taken is reported. A full
			// refresh must still land the exact source state.
			t.Logf("phase %q fell back to full refresh after: %v", phase, err)
			needRefresh = true
		}
		if needRefresh {
			cfg.FullRefresh = true
			start := time.Now()
			require.NoError(t, pipeline.New(cfg).Run(ctx), "phase %q: full refresh failed", phase)
			cfg.FullRefresh = false
			fullRefreshes++
			t.Logf("phase %q: full refresh completed in %v", phase, time.Since(start).Round(time.Millisecond))
		}
		verifyAllAccurate(phase)
	}

	runChurnPhase("mid-table column add with unconsumed rows", true, func() error {
		if err := execSQL(fmt.Sprintf("INSERT INTO %s (id, val, payload, updated_at) VALUES (?, ?, 'pre-drift', NOW(6))", driftTable), churnBase, churnBase); err != nil {
			return err
		}
		if err := execSQL(fmt.Sprintf("ALTER TABLE %s ADD COLUMN drift_note VARCHAR(32) NULL AFTER val", driftTable)); err != nil {
			return err
		}
		return execSQL(fmt.Sprintf("INSERT INTO %s (id, val, drift_note, payload, updated_at) VALUES (?, ?, 'post-drift', 'post-drift', NOW(6))", driftTable), churnBase+1, churnBase+1)
	})
	var driftNote string
	require.NoError(t, dstVerify.QueryRowContext(ctx,
		fmt.Sprintf("SELECT drift_note FROM %s WHERE id = ?", driftTable), churnBase+1).Scan(&driftNote))
	require.Equal(t, "post-drift", driftNote, "the post-ALTER column must land after recovery")

	runChurnPhase("append column while caught up", false, func() error {
		if err := execSQL(fmt.Sprintf("ALTER TABLE %s ADD COLUMN note2 VARCHAR(40) NULL", appendTable)); err != nil {
			return err
		}
		if err := execSQL(fmt.Sprintf("INSERT INTO %s (id, val, payload, note2, updated_at) VALUES (?, ?, 'note2-insert', 'fresh', NOW(6))", appendTable), appendBase, appendBase); err != nil {
			return err
		}
		return execSQL(fmt.Sprintf("UPDATE %s SET note2 = CONCAT('n2-', id), updated_at = NOW(6) WHERE id %% 97 = 0 AND id < ?", appendTable), int64(mysqlStressSeedRows))
	})

	runChurnPhase("unsigned and varchar widening with unconsumed rows", false, func() error {
		if err := execSQL(fmt.Sprintf("UPDATE %s SET u16 = 65535, ts = NOW(6) WHERE id %% 41 = 0 AND id < ?", mysqlStressTypesTable), int64(mysqlStressSeedRows)); err != nil {
			return err
		}
		if err := execSQL(fmt.Sprintf("ALTER TABLE %s MODIFY u16 INT UNSIGNED NOT NULL, MODIFY name VARCHAR(300) NOT NULL", mysqlStressTypesTable)); err != nil {
			return err
		}
		return execSQL(fmt.Sprintf("UPDATE %s SET u16 = 4294967295, name = CONCAT('widened-', id, '-', REPEAT('w', 150)), ts = NOW(6) WHERE id %% 43 = 0 AND id < ?", mysqlStressTypesTable), int64(mysqlStressSeedRows))
	})

	runChurnPhase("column drop with unconsumed rows", true, func() error {
		if err := execSQL(fmt.Sprintf("UPDATE %s SET val = val + 3, updated_at = NOW(6) WHERE id %% 89 = 0 AND id < ?", driftTable), int64(mysqlStressSeedRows)); err != nil {
			return err
		}
		if err := execSQL(fmt.Sprintf("ALTER TABLE %s DROP COLUMN drift_note", driftTable)); err != nil {
			return err
		}
		return execSQL(fmt.Sprintf("UPDATE %s SET val = val + 5, updated_at = NOW(6) WHERE id %% 83 = 0 AND id < ?", driftTable), int64(mysqlStressSeedRows))
	})

	runChurnPhase("column rename with full backfill", false, func() error {
		if err := execSQL(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN note2 TO note3", appendTable)); err != nil {
			return err
		}
		// Re-emit every live row so the renamed column is populated in the
		// destination without a rebuild.
		return execSQL(fmt.Sprintf("UPDATE %s SET val = val + 1, updated_at = NOW(6)", appendTable))
	})

	runChurnPhase("new table appears after load", false, func() error {
		tbl := &mysqlStressTable{name: "mstress_post_load"}
		if err := mysqlStressCreatePlain(ctx, loadDB, tbl.name); err != nil {
			return err
		}
		if err := mysqlStressSeedTable(ctx, loadDB, tbl, mysqlStressLateSeedRows); err != nil {
			return err
		}
		tables.add(tbl)
		finalTables = append(finalTables, tbl)
		return execSQL(fmt.Sprintf("UPDATE %s SET val = val + 7, updated_at = NOW(6) WHERE id %% 5 = 0", tbl.name))
	})

	t.Logf("PERF SUMMARY: snapshot %.0f rows/sec (%d rows in %v); load %.0f ops/sec sustained; %d mid-load catch-up runs; convergence %v after load stop; %d full refreshes across 6 schema-churn phases",
		snapshotRate, seededRows, snapshotDuration.Round(time.Millisecond), achieved, catchupRuns.Load(), convergeDuration.Round(time.Millisecond), fullRefreshes)
}
