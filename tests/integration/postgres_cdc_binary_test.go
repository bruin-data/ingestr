//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// cdcTypesMatrixDDL covers every type the binary decode path supports; the
// wide text column is TOASTable so unchanged-TOAST handling is exercised too.
const cdcTypesMatrixDDL = `
CREATE TABLE public.types_matrix (
	id BIGINT PRIMARY KEY,
	b BOOLEAN,
	si SMALLINT,
	i INTEGER,
	bi BIGINT,
	r REAL,
	dp DOUBLE PRECISION,
	num NUMERIC(12,4),
	txt TEXT,
	vc VARCHAR(64),
	dt DATE,
	tm TIME,
	ts TIMESTAMP,
	tstz TIMESTAMPTZ,
	js JSON,
	jsb JSONB,
	uid UUID,
	byt BYTEA,
	ia INTEGER[],
	ta TEXT[],
	big TEXT
)`

// cdcTypesMatrixSeed is snapshotted by the first run: it both provides resume
// state for the second run and covers the snapshot path's type conversions
// (e.g. pgx's [16]byte UUIDs).
const cdcTypesMatrixSeed = `INSERT INTO public.types_matrix VALUES (
	0, false, 1, 2, 3, 0.5, 0.25, 42.0001,
	'seed', 'seed-vc', '2020-06-01', '01:02:03', '2020-06-01 01:02:03',
	'2020-06-01 01:02:03+00', '{"seed": true}', '{"seed": 1}',
	'00000000-0000-0000-0000-000000000001', '\x01',
	'{9}', '{"z"}', 'small')`

// cdcTypesMatrixDML is applied between two batch CDC runs, so every statement
// is decoded from WAL (not snapshotted). Values are literals so two identical
// scenario runs produce byte-identical destination content.
var cdcTypesMatrixDML = []string{
	`INSERT INTO public.types_matrix VALUES (
		1, true, 12, 3456, 789012345678, 1.5, 2.25, 1234.5678,
		'hello world', 'varchar-val', '2024-01-15', '10:30:45', '2024-01-15 10:30:45.123456',
		'2024-01-15 10:30:45.123456+02', '{"j": 1}', '{"k": [1,2,3]}',
		'550e8400-e29b-41d4-a716-446655440000', '\xdeadbeef',
		'{1,2,3}', '{"a","b c",NULL}', repeat('toast-', 2000))`,
	`INSERT INTO public.types_matrix (id, b, txt) VALUES (2, false, 'sparse row with nulls')`,
	`UPDATE public.types_matrix SET num = -0.0001, si = -3, jsb = '{"updated": true}' WHERE id = 1`,
	// Leaves the TOASTed "big" column unchanged: text mode marks it unchanged,
	// binary mode must do the same.
	`UPDATE public.types_matrix SET txt = 'updated-1' WHERE id = 1`,
	`INSERT INTO public.types_matrix (id, txt, byt, ia) VALUES (3, 'to be deleted', '\x00ff', '{}')`,
	`DELETE FROM public.types_matrix WHERE id = 3`,
	`UPDATE public.types_matrix SET dt = '1999-12-31', ts = '1999-12-31 23:59:59', tstz = '2000-01-01 00:00:00+00' WHERE id = 2`,
}

// runBinaryParityScenario spins up a fresh source+dest pair, runs
// snapshot -> DML -> CDC drain with the given source URI options, and returns
// the destination rows rendered as text (CDC position columns excluded, since
// LSNs differ between independent containers).
func runBinaryParityScenario(t *testing.T, ctx context.Context, uriExtra string) []string {
	t.Helper()

	_, sourceConn := setupPostgresCDCContainer(t, ctx)
	srcPool, err := pgxpool.New(ctx, sourceConn)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, cdcTypesMatrixDDL)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, cdcTypesMatrixSeed)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx, "postgres:16-alpine",
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
	destConn, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConn[len("postgres://"):] + uriExtra,
		DestURI:             destConn,
		IncrementalStrategy: config.StrategyMerge,
		Progress:            config.ProgressLog,
	}

	// Run 1: snapshot + slot creation.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// The DML below is decoded from WAL by run 2.
	for _, stmt := range cdcTypesMatrixDML {
		_, err := srcPool.Exec(ctx, stmt)
		require.NoError(t, err, "DML: %s", stmt)
	}

	// Run 2: batch CDC drain through the (text or binary) decode path.
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	destPool, err := pgxpool.New(ctx, destConn)
	require.NoError(t, err)
	defer destPool.Close()

	rows, err := destPool.Query(ctx, `
		SELECT id::text || '|' || COALESCE(b::text,'∅') || '|' || COALESCE(si::text,'∅') || '|' ||
			COALESCE(i::text,'∅') || '|' || COALESCE(bi::text,'∅') || '|' || COALESCE(r::text,'∅') || '|' ||
			COALESCE(dp::text,'∅') || '|' || COALESCE(num::text,'∅') || '|' || COALESCE(txt,'∅') || '|' ||
			COALESCE(vc,'∅') || '|' || COALESCE(dt::text,'∅') || '|' || COALESCE(tm::text,'∅') || '|' ||
			COALESCE(ts::text,'∅') || '|' || COALESCE(tstz AT TIME ZONE 'UTC','1970-01-01')::text || '|' ||
			COALESCE(js::text,'∅') || '|' || COALESCE(jsb::text,'∅') || '|' || COALESCE(uid::text,'∅') || '|' ||
			COALESCE(encode(byt,'hex'),'∅') || '|' || COALESCE(ia::text,'∅') || '|' || COALESCE(ta::text,'∅') || '|' ||
			COALESCE(md5(big),'∅') || '|' || _cdc_deleted::text
		FROM public.types_matrix ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestPostgresCDC_BinaryModeParity runs the identical scenario through the
// text decode path and the pgoutput binary decode path (binary=true) and
// requires byte-identical destination content across every supported type.
func TestPostgresCDC_BinaryModeParity(t *testing.T) {
	ctx := context.Background()

	textRows := runBinaryParityScenario(t, ctx, "&discover_interval=off")
	binaryRows := runBinaryParityScenario(t, ctx, "&discover_interval=off&binary=true")

	require.NotEmpty(t, textRows)
	assert.Equal(t, textRows, binaryRows, "binary decode must produce identical destination content to text decode")

	// Sanity: the scenario really covers the interesting rows.
	require.Len(t, textRows, 4)
	assert.Contains(t, textRows[0], "00000000-0000-0000-0000-000000000001", "snapshot must render UUIDs canonically")
	assert.Contains(t, textRows[1], "updated-1", "post-snapshot UPDATE must be applied")
	assert.Contains(t, textRows[1], "deadbeef", "bytea must round-trip as raw bytes")
	assert.Contains(t, textRows[1], "550e8400-e29b-41d4-a716-446655440000", "decoded UUIDs must render canonically")
	assert.Contains(t, textRows[3], "|true", "deleted row is soft-deleted")
}

// TestPostgresCDC_LargeTransactionStreaming forces protocol v2 streaming of
// in-progress transactions (tiny logical_decoding_work_mem) and verifies that
// a large committed transaction lands exactly and a rolled-back one leaves no
// trace, while the server reports that streaming actually happened.
func TestPostgresCDC_LargeTransactionStreaming(t *testing.T) {
	ctx := context.Background()

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
			"-c", "max_replication_slots=4",
			"-c", "max_wal_senders=4",
			// Force the walsender to stream large in-progress transactions
			// instead of buffering them server-side.
			"-c", "logical_decoding_work_mem=64kB",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60 * time.Second),
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
	sourceConn := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	srcPool, err := pgxpool.New(ctx, sourceConn)
	require.NoError(t, err)
	defer srcPool.Close()

	_, err = srcPool.Exec(ctx, `
		CREATE TABLE public.big_tx (
			id BIGINT PRIMARY KEY,
			payload TEXT NOT NULL
		);
		INSERT INTO public.big_tx SELECT g, 'seed-' || g FROM generate_series(1, 100) g;
	`)
	require.NoError(t, err)
	_, err = srcPool.Exec(ctx, `ALTER USER testuser REPLICATION`)
	require.NoError(t, err)

	destContainer, err := postgres.Run(
		ctx, "postgres:16-alpine",
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
	destConn, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConn)
	require.NoError(t, err)
	defer destPool.Close()

	cfg := &config.IngestConfig{
		SourceURI:           "postgres+cdc://" + sourceConn[len("postgres://"):] + "&discover_interval=off",
		DestURI:             destConn,
		IncrementalStrategy: config.StrategyMerge,
		Stream:              true,
		FlushInterval:       time.Second,
		FlushRecords:        50000,
		Progress:            config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	// Give the stream time to snapshot and enter streaming.
	waitForCount := func(want int64, what string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Minute)
		for {
			var got int64
			_ = destPool.QueryRow(ctx,
				`SELECT count(*) FROM public.big_tx WHERE _cdc_deleted = false`).Scan(&got)
			if got == want {
				return
			}
			select {
			case err := <-runErr:
				t.Fatalf("stream exited early while waiting for %s: %v", what, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: want %d rows, have %d", what, want, got)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	waitForCount(100, "initial snapshot")

	// A rolled-back big transaction must leave no trace (stream abort path).
	tx, err := srcPool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO public.big_tx SELECT g, repeat('rollback-', 20) || g FROM generate_series(100001, 120000) g`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	// A large committed transaction (well above logical_decoding_work_mem, so
	// the server streams it in-progress via protocol v2).
	tx, err = srcPool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO public.big_tx SELECT g, repeat('big-', 20) || g FROM generate_series(101, 20100) g`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE public.big_tx SET payload = 'rewritten-' || id WHERE id BETWEEN 101 AND 200`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	waitForCount(20100, "large streamed transaction")

	// Verify the server actually streamed the transaction (v2 in action) and
	// the update-inside-the-big-tx won.
	var streamCount int64
	require.NoError(t, srcPool.QueryRow(ctx,
		`SELECT COALESCE(sum(stream_count), 0) FROM pg_stat_replication_slots`).Scan(&streamCount))
	assert.Positive(t, streamCount, "walsender should have streamed the in-progress transaction (protocol v2)")

	var rewritten int64
	require.NoError(t, destPool.QueryRow(ctx,
		`SELECT count(*) FROM public.big_tx WHERE payload LIKE 'rewritten-%' AND _cdc_deleted = false`).Scan(&rewritten))
	assert.EqualValues(t, 100, rewritten)

	var rolledBack int64
	require.NoError(t, destPool.QueryRow(ctx,
		`SELECT count(*) FROM public.big_tx WHERE id > 100000`).Scan(&rolledBack))
	assert.Zero(t, rolledBack, "rolled-back streamed transaction must not reach the destination")

	// Source/destination content must match exactly.
	var srcSum, dstSum int64
	require.NoError(t, srcPool.QueryRow(ctx, `SELECT sum(length(payload)) FROM public.big_tx`).Scan(&srcSum))
	require.NoError(t, destPool.QueryRow(ctx, `SELECT sum(length(payload)) FROM public.big_tx WHERE _cdc_deleted = false`).Scan(&dstSum))
	assert.Equal(t, srcSum, dstSum)

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
