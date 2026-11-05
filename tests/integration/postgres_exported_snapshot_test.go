//go:build integration

package integration

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func exportPostgresTestSnapshot(t *testing.T, ctx context.Context, uri, slot string) (*pgconn.PgConn, pglogrepl.CreateReplicationSlotResult) {
	t.Helper()
	conn, err := pgconn.Connect(ctx, uri+"&replication=database")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	created, err := pglogrepl.CreateReplicationSlot(ctx, conn, slot, "pgoutput", pglogrepl.CreateReplicationSlotOptions{
		Mode:           pglogrepl.LogicalReplication,
		SnapshotAction: "EXPORT_SNAPSHOT",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.SnapshotName)
	require.NotEmpty(t, created.ConsistentPoint)
	return conn, created
}

func requirePostgresSnapshotRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, expected map[int32]string) {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT id, name FROM "+pqTable("landing", table))
	require.NoError(t, err)
	defer rows.Close()
	actual := make(map[int32]string)
	for rows.Next() {
		var id int32
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		require.NotContains(t, actual, id, "snapshot must not duplicate a row")
		actual[id] = name
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, actual)
}

func TestPostgresExportedSnapshotCDC(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, uri := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE SCHEMA landing;
		CREATE TABLE public.orders (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO public.orders VALUES (0, 'deleted'), (1, 'untouched'), (2, 'before');
		CREATE TABLE public.products (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO public.products VALUES (10, 'product');
		CREATE PUBLICATION snapshot_pub FOR TABLE public.orders, public.products;
	`)
	require.NoError(t, err)
	exporter, snapshot := exportPostgresTestSnapshot(t, ctx, uri, "snapshot_slot")
	_, err = pool.Exec(ctx, `
		UPDATE public.orders SET name = 'after' WHERE id = 2;
		DELETE FROM public.orders WHERE id = 0;
		INSERT INTO public.orders VALUES (4, 'inserted');
		UPDATE public.products SET name = 'changed product' WHERE id = 10;
	`)
	require.NoError(t, err)

	for _, tc := range []struct {
		name        string
		sourceTable string
		partitioned bool
	}{
		{name: "orders", sourceTable: "public.orders"},
		{name: "partitioned_orders", sourceTable: "public.orders", partitioned: true},
		{name: "query_orders", sourceTable: "query:SELECT id, name FROM public.orders"},
		{name: "partitioned_query_orders", sourceTable: "query:SELECT id, name FROM public.orders", partitioned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.IngestConfig{
				SourceURI: uri + "&snapshot=" + url.QueryEscape(snapshot.SnapshotName), SourceTable: tc.sourceTable,
				DestURI: uri, DestTable: "landing." + tc.name,
				IncrementalStrategy: config.StrategyReplace, SchemaNaming: "direct",
				PageSize: 1, ExtractParallelism: 3, NoLoadTimestamp: true, NoRunID: true, Yes: true,
			}
			if tc.partitioned {
				cfg.ExtractPartitionBy = "id"
				cfg.ExtractPartitionNumericInterval = 1
			}
			require.NoError(t, pipeline.New(&cfg).Run(ctx))
			requirePostgresSnapshotRows(t, ctx, pool, tc.name, map[int32]string{0: "deleted", 1: "untouched", 2: "before"})
		})
	}
	productLoad := config.IngestConfig{
		SourceURI: uri + "&snapshot=" + url.QueryEscape(snapshot.SnapshotName), SourceTable: "public.products",
		DestURI: uri, DestTable: "landing.products",
		IncrementalStrategy: config.StrategyReplace, SchemaNaming: "direct",
		NoLoadTimestamp: true, NoRunID: true, Yes: true,
	}
	require.NoError(t, pipeline.New(&productLoad).Run(ctx))
	requirePostgresSnapshotRows(t, ctx, pool, "products", map[int32]string{10: "product"})
	require.NoError(t, exporter.Close(ctx))

	cdc := config.IngestConfig{
		SourceURI:    strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=snapshot_pub&slot=snapshot_slot&dest_schema=landing",
		SourceTables: []string{"public.orders", "public.products"}, DestURI: uri,
		IncrementalStrategy: config.StrategyMerge, SchemaContract: "evolve", SchemaNaming: "direct",
		CDCSnapshotMode: "never", CDCStartLSN: snapshot.ConsistentPoint, CDCTableNaming: config.TableNamingTable,
		NoLoadTimestamp: true, NoRunID: true, Yes: true,
	}
	run := cdc
	require.NoError(t, pipeline.New(&run).Run(ctx))
	requirePostgresSnapshotRows(t, ctx, pool, "orders", map[int32]string{0: "deleted", 1: "untouched", 2: "after", 4: "inserted"})
	requirePostgresSnapshotRows(t, ctx, pool, "products", map[int32]string{10: "changed product"})
	var deleted, untouched, advanced bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT _cdc_deleted FROM landing.orders WHERE id = 0`).Scan(&deleted))
	require.True(t, deleted)
	require.NoError(t, pool.QueryRow(ctx, `SELECT _cdc_lsn IS NULL FROM landing.orders WHERE id = 1`).Scan(&untouched))
	require.True(t, untouched, "adopting the baseline must preserve untouched rows")
	require.NoError(t, pool.QueryRow(ctx, `SELECT confirmed_flush_lsn > $1::pg_lsn FROM pg_replication_slots WHERE slot_name = 'snapshot_slot'`, snapshot.ConsistentPoint).Scan(&advanced))
	require.True(t, advanced)

	_, err = pool.Exec(ctx, `UPDATE public.orders SET name = 'resumed' WHERE id = 4`)
	require.NoError(t, err)
	run = cdc
	require.NoError(t, pipeline.New(&run).Run(ctx))
	requirePostgresSnapshotRows(t, ctx, pool, "orders", map[int32]string{0: "deleted", 1: "untouched", 2: "after", 4: "resumed"})
	require.NoError(t, pool.QueryRow(ctx, `SELECT _cdc_lsn IS NULL FROM landing.orders WHERE id = 1`).Scan(&untouched))
	require.True(t, untouched, "retrying with the bootstrap LSN must use managed checkpoints")
}

func TestPostgresExportedSnapshotReadGuards(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	container, uri := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE SCHEMA landing;
		CREATE TABLE public.orders (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO public.orders VALUES (1, 'visible'), (2, 'hidden');
		ALTER TABLE public.orders ENABLE ROW LEVEL SECURITY;
		CREATE POLICY only_first ON public.orders USING (id = 1);
		CREATE ROLE snapshot_reader LOGIN PASSWORD 'snapshot_reader';
		GRANT USAGE ON SCHEMA public TO snapshot_reader;
		GRANT SELECT ON public.orders TO snapshot_reader;
		CREATE TABLE landing.orders (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO landing.orders VALUES (99, 'keep destination');
	`)
	require.NoError(t, err)
	exporter, snapshot := exportPostgresTestSnapshot(t, ctx, uri, "guard_slot")
	readerURI, err := url.Parse(uri)
	require.NoError(t, err)
	readerURI.User = url.UserPassword("snapshot_reader", "snapshot_reader")
	params := readerURI.Query()
	params.Set("snapshot", snapshot.SnapshotName)
	readerURI.RawQuery = params.Encode()
	cfg := config.IngestConfig{
		SourceURI: readerURI.String(), SourceTable: "public.orders", DestURI: uri, DestTable: "landing.orders",
		IncrementalStrategy: config.StrategyReplace, SchemaNaming: "direct",
		NoLoadTimestamp: true, NoRunID: true, Yes: true,
	}
	require.ErrorContains(t, pipeline.New(&cfg).Run(ctx), "row-level security")
	requirePostgresSnapshotRows(t, ctx, pool, "orders", map[int32]string{99: "keep destination"})

	require.NoError(t, exporter.Close(ctx))
	cfg.SourceURI = uri + "&snapshot=" + url.QueryEscape(snapshot.SnapshotName)
	require.ErrorContains(t, pipeline.New(&cfg).Run(ctx), "invalid snapshot identifier")
	requirePostgresSnapshotRows(t, ctx, pool, "orders", map[int32]string{99: "keep destination"})
}
