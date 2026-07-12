//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupNewTableDest(t *testing.T, ctx context.Context) (string, *pgxpool.Pool) {
	t.Helper()
	destContainer, err := postgres.Run(
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
	t.Cleanup(func() { _ = destContainer.Terminate(ctx) })

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	t.Cleanup(destPool.Close)
	return destConnString, destPool
}

// TestPostgresCDC_NewTableBetweenBatchRuns verifies that a table created on the
// source between two batch runs is picked up by the next run with a full
// backfill of its pre-existing rows, while the original table keeps resuming
// incrementally.
func TestPostgresCDC_NewTableBetweenBatchRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.orders (id BIGINT PRIMARY KEY, amount BIGINT);
		INSERT INTO public.orders SELECT g, g * 10 FROM generate_series(1, 50) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)

	// No publication= -> ingestr manages the publication and reconciles it on
	// every run.
	cfg := &config.IngestConfig{
		SourceURI: "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&mode=batch",
		DestURI:   destConnString,
	}

	// Run 1: snapshot of orders.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var count int
	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE _cdc_deleted = false`).Scan(&count))
	require.Equal(t, 50, count)

	// A new table appears, with pre-existing rows that only a backfill can
	// deliver; the original table also gets an incremental change.
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.customers (id BIGINT PRIMARY KEY, name TEXT);
		INSERT INTO public.customers SELECT g, 'customer-' || g FROM generate_series(1, 30) g;
		INSERT INTO public.orders VALUES (51, 510);
	`)
	require.NoError(t, err)

	// Run 2: resumes orders, backfills customers.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 30, count, "new table should be fully backfilled")
	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 51, count, "original table should keep resuming")

	// Run 3: subsequent changes to the new table flow incrementally (no
	// re-snapshot: the destination now has resume state for customers).
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.customers VALUES (31, 'customer-31');
		UPDATE public.customers SET name = 'renamed' WHERE id = 1;
		DELETE FROM public.customers WHERE id = 2;
	`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	require.NoError(t, destPool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE _cdc_deleted = false`).Scan(&count))
	assert.Equal(t, 30, count, "31 rows minus 1 delete")

	var name string
	require.NoError(t, destPool.QueryRow(ctx, `SELECT name FROM customers WHERE id = 1`).Scan(&name))
	assert.Equal(t, "renamed", name)

	var deleted bool
	require.NoError(t, destPool.QueryRow(ctx, `SELECT _cdc_deleted FROM customers WHERE id = 2`).Scan(&deleted))
	assert.True(t, deleted, "delete should be reflected via merge")
}

func TestPostgresCDC_StreamingRestartRecoversOfflineDDLAndTableRecreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.offline_stable (id BIGINT PRIMARY KEY, value TEXT);
		CREATE TABLE public.offline_ddl (id BIGINT PRIMARY KEY, amount BIGINT);
		CREATE TABLE public.offline_default (id BIGINT PRIMARY KEY, value TEXT);
		CREATE TABLE public.offline_recreated (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.offline_stable VALUES (1, 'stable-1');
		INSERT INTO public.offline_ddl VALUES (1, 10);
		INSERT INTO public.offline_default VALUES (1, 'default-1');
		INSERT INTO public.offline_recreated VALUES (1, 'old-1'), (2, 'old-2');
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=1h",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       500 * time.Millisecond,
		FlushRecords:        10,
		Progress:            config.ProgressLog,
	}

	start := func() (context.CancelFunc, <-chan error) {
		streamCtx, cancel := context.WithCancel(ctx)
		runErr := make(chan error, 1)
		go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()
		return cancel, runErr
	}
	stop := func(cancel context.CancelFunc, runErr <-chan error) {
		cancel()
		select {
		case runErr := <-runErr:
			if runErr != nil {
				require.ErrorIs(t, runErr, context.Canceled)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("streaming pipeline did not exit within 30s of cancellation")
		}
	}
	rowCount := func(table string) int {
		var count int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE _cdc_deleted = false`).Scan(&count); err != nil {
			return -1
		}
		return count
	}

	cancel, runErr := start()
	require.Eventually(t, func() bool {
		return rowCount("offline_stable") == 1 && rowCount("offline_ddl") == 1 && rowCount("offline_default") == 1 && rowCount("offline_recreated") == 2
	}, 60*time.Second, 500*time.Millisecond)
	stop(cancel, runErr)

	// All changes below occur after the durable checkpoint and before restart.
	// The DDL table keeps its OID, while the recreated table gets a new OID and
	// shares the persistent slot with the independently resumable stable table.
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.offline_ddl VALUES (2, 20);
		ALTER TABLE public.offline_ddl ADD COLUMN status TEXT NOT NULL DEFAULT 'ready';
		ALTER TABLE public.offline_ddl ALTER COLUMN amount TYPE NUMERIC(30,4) USING amount::numeric;
		UPDATE public.offline_ddl SET amount = amount + 0.1250;
		ALTER TABLE public.offline_default ADD COLUMN status TEXT NOT NULL DEFAULT 'ready';
		INSERT INTO public.offline_default (id, value) VALUES (2, 'default-2');
		DROP TABLE public.offline_recreated;
		CREATE TABLE public.offline_recreated (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.offline_recreated VALUES (100, 'new-100'), (101, 'new-101');
		INSERT INTO public.offline_stable VALUES (2, 'stable-2');
	`)
	require.NoError(t, err)

	cancel, runErr = start()
	require.Eventually(t, func() bool {
		if rowCount("offline_stable") != 2 || rowCount("offline_ddl") != 2 || rowCount("offline_default") != 2 || rowCount("offline_recreated") != 2 {
			return false
		}
		var amount string
		if err := destPool.QueryRow(ctx, `SELECT amount::text FROM offline_ddl WHERE id = 2 AND _cdc_deleted = false`).Scan(&amount); err != nil || amount != "20.1250" {
			return false
		}
		var statuses []string
		statusRows, err := destPool.Query(ctx, `SELECT status FROM offline_ddl WHERE _cdc_deleted = false ORDER BY id`)
		if err != nil {
			return false
		}
		for statusRows.Next() {
			var status string
			if err := statusRows.Scan(&status); err != nil {
				statusRows.Close()
				return false
			}
			statuses = append(statuses, status)
		}
		statusRows.Close()
		if !assert.ObjectsAreEqual([]string{"ready", "ready"}, statuses) {
			return false
		}
		var defaultStatuses []string
		defaultRows, err := destPool.Query(ctx, `SELECT status FROM offline_default WHERE _cdc_deleted = false ORDER BY id`)
		if err != nil {
			return false
		}
		for defaultRows.Next() {
			var status string
			if err := defaultRows.Scan(&status); err != nil {
				defaultRows.Close()
				return false
			}
			defaultStatuses = append(defaultStatuses, status)
		}
		defaultRows.Close()
		if !assert.ObjectsAreEqual([]string{"ready", "ready"}, defaultStatuses) {
			return false
		}
		var ids []int64
		rows, err := destPool.Query(ctx, `SELECT id FROM offline_recreated WHERE _cdc_deleted = false ORDER BY id`)
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return false
			}
			ids = append(ids, id)
		}
		return assert.ObjectsAreEqual([]int64{100, 101}, ids)
	}, 90*time.Second, 500*time.Millisecond, "restart must replace both affected snapshots and continue shared-slot replay")

	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.offline_ddl (id, amount) VALUES (3, 30.5000);
		INSERT INTO public.offline_recreated VALUES (102, 'new-102');
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return rowCount("offline_ddl") == 3 && rowCount("offline_recreated") == 3
	}, 60*time.Second, 500*time.Millisecond, "live replication must continue after offline recovery")
	stop(cancel, runErr)
}

// TestPostgresCDC_StreamingNewTableDetection verifies that mid-stream table
// discovery is a side-effect-free restart boundary. The restarted stream sees
// the table in its startup set, backfills it, and then replicates live changes.
func TestPostgresCDC_StreamingNewTableDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()

	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.evt (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.evt SELECT g, 0 FROM generate_series(1, 100) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)

	// Managed publication + fast discovery so the test observes a mid-stream
	// rebuild quickly.
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=1s",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       1 * time.Second,
		FlushRecords:        50,
		Progress:            config.ProgressLog,
	}

	start := func() (context.CancelFunc, <-chan error) {
		streamCtx, cancelStream := context.WithCancel(ctx)
		runErr := make(chan error, 1)
		go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()
		return cancelStream, runErr
	}
	cancelStream, runErr := start()

	tableCount := func(table string) int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE _cdc_deleted = false`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// 1. Snapshot of the original table lands.
	require.Eventually(t, func() bool { return tableCount("evt") == 100 }, 60*time.Second, 500*time.Millisecond,
		"snapshot of evt should appear in destination")

	// 2. A new table appears mid-stream with pre-existing rows.
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.late (id BIGINT PRIMARY KEY, val BIGINT);
		INSERT INTO public.late SELECT g, g FROM generate_series(1, 40) g;
	`)
	require.NoError(t, err)

	// 3. Discovery stops the first run before creating, claiming, truncating, or
	// writing the late destination. The already-running target stays intact.
	select {
	case err := <-runErr:
		require.ErrorContains(t, err, "requires restarting the stream")
	case <-time.After(60 * time.Second):
		cancelStream()
		t.Fatal("stream did not stop after discovering the new table")
	}
	assert.Equal(t, 100, tableCount("evt"), "late discovery changed an existing destination")
	var lateTargetExists bool
	require.NoError(t, destPool.QueryRow(ctx, `SELECT to_regclass('public.late') IS NOT NULL`).Scan(&lateTargetExists))
	assert.False(t, lateTargetExists, "first run created the late destination before restart")
	var lateClaims, lateStateEvents int
	require.NoError(t, destPool.QueryRow(ctx, `
		SELECT count(*) FROM _bruin_staging.cdc_targets WHERE destination_table = $1
	`, destination.CDCTargetKey("public", "late")).Scan(&lateClaims))
	require.NoError(t, destPool.QueryRow(ctx, `
		SELECT count(*) FROM _bruin_staging.cdc_state WHERE source_table = 'late'
	`).Scan(&lateStateEvents))
	assert.Zero(t, lateClaims, "first run claimed the late destination before restart")
	assert.Zero(t, lateStateEvents, "first run registered late-table CDC state before restart")

	// 4. On restart the table belongs to the startup KnownTables set and follows
	// the normal snapshot path.
	cancelStream, runErr = start()
	require.Eventually(t, func() bool { return tableCount("late") == 40 }, 60*time.Second, 500*time.Millisecond,
		"restarted stream should backfill the new table, got %d rows", tableCount("late"))

	// 5. Live changes on both the new and the original table keep flowing.
	_, err = srcPool.Exec(ctx, `
		INSERT INTO public.late SELECT g, g FROM generate_series(41, 60) g;
		UPDATE public.late SET val = -1 WHERE id = 1;
		DELETE FROM public.late WHERE id = 2;
		INSERT INTO public.evt SELECT g, g FROM generate_series(101, 120) g;
	`)
	require.NoError(t, err)

	// 40 backfilled + 20 inserted - 1 deleted = 59 live rows.
	require.Eventually(t, func() bool {
		return tableCount("late") == 59 && tableCount("evt") == 120
	}, 60*time.Second, 500*time.Millisecond,
		"live changes should replicate after the rebuild (late=%d, evt=%d)", tableCount("late"), tableCount("evt"))

	var val int64
	require.NoError(t, destPool.QueryRow(ctx, `SELECT val FROM late WHERE id = 1`).Scan(&val))
	assert.Equal(t, int64(-1), val)

	// 6. Graceful shutdown.
	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func TestPostgresCDC_StreamingColumnRenameAcrossHistoricalRelations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.rename_history (id BIGINT PRIMARY KEY, legacy TEXT);
		INSERT INTO public.rename_history SELECT g, 'legacy-' || g FROM generate_series(1, 100) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):],
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       500 * time.Millisecond,
		FlushRecords:        50,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	liveCount := func() int {
		var count int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM rename_history WHERE _cdc_deleted = false`).Scan(&count); err != nil {
			return -1
		}
		return count
	}
	require.Eventually(t, func() bool { return liveCount() == 100 }, 60*time.Second, 500*time.Millisecond)

	_, err = srcPool.Exec(ctx, `
		ALTER TABLE public.rename_history ADD COLUMN segment TEXT NOT NULL DEFAULT 'initial';
		UPDATE public.rename_history SET segment = 'segment-' || id;
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var value string
		return destPool.QueryRow(ctx, `SELECT segment FROM rename_history WHERE id = 1 AND _cdc_deleted = false`).Scan(&value) == nil && value == "segment-1"
	}, 60*time.Second, 500*time.Millisecond)

	_, err = srcPool.Exec(ctx, `
		ALTER TABLE public.rename_history DROP COLUMN legacy;
		UPDATE public.rename_history SET segment = 'pre-rename-' || id;
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var value string
		return destPool.QueryRow(ctx, `SELECT segment FROM rename_history WHERE id = 1 AND _cdc_deleted = false`).Scan(&value) == nil && value == "pre-rename-1"
	}, 60*time.Second, 500*time.Millisecond)

	_, err = srcPool.Exec(ctx, `
		ALTER TABLE public.rename_history RENAME COLUMN segment TO cohort;
		UPDATE public.rename_history SET cohort = 'cohort-' || id;
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var value string
		return destPool.QueryRow(ctx, `SELECT cohort FROM rename_history WHERE id = 1 AND _cdc_deleted = false`).Scan(&value) == nil && value == "cohort-1"
	}, 60*time.Second, 500*time.Millisecond, "stream must progress from repeated historical segment relations to the live cohort relation")

	select {
	case err := <-runErr:
		t.Fatalf("stream exited after column rename: %v", err)
	default:
	}
	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func TestPostgresCDC_StreamingNumericTypeChangesRefreshCopyEncoding(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.numeric_history (id BIGINT PRIMARY KEY, val BIGINT NOT NULL);
		INSERT INTO public.numeric_history SELECT g, g * 10 FROM generate_series(1, 100) g;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):],
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       500 * time.Millisecond,
		FlushRecords:        50,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	waitForValue := func(want string) {
		t.Helper()
		require.Eventually(t, func() bool {
			select {
			case err := <-runErr:
				t.Fatalf("stream exited during numeric type replay: %v", err)
			default:
			}
			var value string
			return destPool.QueryRow(ctx, `SELECT val::text FROM numeric_history WHERE id = 1 AND _cdc_deleted = false`).Scan(&value) == nil && value == want
		}, 60*time.Second, 500*time.Millisecond)
	}
	waitForValue("10")

	_, err = srcPool.Exec(ctx, `
		ALTER TABLE public.numeric_history ALTER COLUMN val TYPE NUMERIC(30,0) USING val::numeric;
		UPDATE public.numeric_history SET val = val + 1000000000000;
	`)
	require.NoError(t, err)
	waitForValue("1000000000010")

	_, err = srcPool.Exec(ctx, `
		ALTER TABLE public.numeric_history ALTER COLUMN val TYPE NUMERIC(35,4) USING val::numeric;
		UPDATE public.numeric_history SET val = val + 0.1250;
	`)
	require.NoError(t, err)
	waitForValue("1000000000010.1250")

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func TestPostgresCDC_SingleTableStreamingReplacesRecreatedTable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.recreated (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.recreated VALUES (1, 'old-1'), (2, 'old-2'), (3, 'old-3');
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&discover_interval=1s",
		DestURI:             destConnString,
		SourceTable:         "public.recreated",
		DestTable:           "recreated",
		PrimaryKeys:         []string{"id"},
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       500 * time.Millisecond,
		FlushRecords:        10,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	liveIDs := func() []int64 {
		rows, err := destPool.Query(ctx, `SELECT id FROM recreated WHERE _cdc_deleted = false ORDER BY id`)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil
			}
			ids = append(ids, id)
		}
		return ids
	}
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]int64{1, 2, 3}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond)

	_, err = srcPool.Exec(ctx, `
		DROP TABLE public.recreated;
		CREATE TABLE public.recreated (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.recreated VALUES (100, 'new-100'), (101, 'new-101');
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]int64{100, 101}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond, "replacement snapshot must remove rows from the old table incarnation")

	_, err = srcPool.Exec(ctx, `INSERT INTO public.recreated VALUES (102, 'new-102')`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]int64{100, 101, 102}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond, "live replication must continue after table recreation")

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func TestPostgresCDC_MultiTableStreamingRejectsReplacementRelationBeforeResnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = sourceContainer.Terminate(ctx) }()
	srcPool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	defer srcPool.Close()
	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.recreated_multi (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.recreated_multi VALUES (1, 'old-1'), (2, 'old-2'), (3, 'old-3');
		CREATE TABLE public.recreated_stable (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.recreated_stable VALUES (1, 'stable');
		CREATE PUBLICATION ingestr_reincarnation_all FOR ALL TABLES;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	destConnString, destPool := setupNewTableDest(t, ctx)
	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=ingestr_reincarnation_all&discover_interval=1h",
		DestURI:             destConnString,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       500 * time.Millisecond,
		FlushRecords:        10,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	liveIDs := func() []int64 {
		rows, err := destPool.Query(ctx, `SELECT id FROM recreated_multi WHERE _cdc_deleted = false ORDER BY id`)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil
			}
			ids = append(ids, id)
		}
		return ids
	}
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]int64{1, 2, 3}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond)

	_, err = srcPool.Exec(ctx, `
		DROP TABLE public.recreated_multi;
		CREATE TABLE public.recreated_multi (id BIGINT PRIMARY KEY, value TEXT);
		INSERT INTO public.recreated_multi VALUES (100, 'new-100'), (101, 'new-101');
	`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("stream exited after seeing the replacement relation: %v", err)
		default:
		}
		return assert.ObjectsAreEqual([]int64{100, 101}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond, "replacement relation must trigger an immediate resnapshot even with periodic discovery disabled")

	_, err = srcPool.Exec(ctx, `INSERT INTO public.recreated_multi VALUES (102, 'new-102')`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]int64{100, 101, 102}, liveIDs())
	}, 60*time.Second, 500*time.Millisecond)

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}
