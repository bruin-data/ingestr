//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresCDCToastAcrossBatches(t *testing.T) {
	for _, multi := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("multi=%v/streaming=%v", multi, streaming), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				container, uri := setupPostgresCDCContainer(t, ctx)
				defer func() { _ = container.Terminate(context.Background()) }()
				pool, err := pgxpool.New(ctx, uri)
				require.NoError(t, err)
				defer pool.Close()
				_, err = pool.Exec(ctx, `CREATE TABLE items (id bigint PRIMARY KEY, payload text, n int);
      ALTER TABLE items ALTER COLUMN payload SET STORAGE EXTERNAL;
      CREATE PUBLICATION items_pub FOR TABLE items; CREATE SCHEMA warehouse`)
				require.NoError(t, err)
				cfg := &config.IngestConfig{SourceURI: strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=items_pub", SourceTable: "public.items", DestURI: uri, DestTable: "warehouse.items", IncrementalStrategy: config.StrategyMerge, PageSize: 1}
				if multi {
					cfg.SourceTable = ""
					cfg.DestTable = ""
					cfg.SourceURI += "&dest_schema=warehouse"
				}
				require.NoError(t, pipeline.New(cfg).Run(ctx))
				payload := strings.Repeat("abcdefghij", 5000)
				// Separate commits and PageSize=1 force separate source Arrow batches,
				// while the destination stages both before one merge.
				_, err = pool.Exec(ctx, `INSERT INTO items VALUES (1, $1, 0)`, payload)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `UPDATE items SET n=1 WHERE id=1`)
				require.NoError(t, err)
				if streaming {
					cfg.Stream = true
					cfg.FlushInterval = time.Second
					cfg.FlushRecords = 10000
					streamCtx, stop := context.WithCancel(ctx)
					defer stop()
					done := make(chan error, 1)
					go func() { done <- pipeline.New(cfg).Run(streamCtx) }()
					require.Eventually(t, func() bool {
						var n int
						return pool.QueryRow(ctx, `SELECT n FROM warehouse.items WHERE id=1`).Scan(&n) == nil && n == 1
					}, 30*time.Second, 100*time.Millisecond)
					stop()
					if err := <-done; err != nil {
						require.ErrorIs(t, err, context.Canceled)
					}
				} else {
					require.NoError(t, pipeline.New(cfg).Run(ctx))
				}
				var actual *string
				require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM warehouse.items WHERE id=1 AND NOT _cdc_deleted`).Scan(&actual))
				require.NotNil(t, actual)
				require.Equal(t, payload, *actual)
			})
		}
	}
}

func TestPostgresCDCToastKeyMove(t *testing.T) {
	for _, fullIdentity := range []bool{false, true} {
		t.Run(fmt.Sprintf("full_identity=%v", fullIdentity), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			container, uri := setupPostgresCDCContainer(t, ctx)
			defer func() { _ = container.Terminate(context.Background()) }()
			pool, err := pgxpool.New(ctx, uri)
			require.NoError(t, err)
			defer pool.Close()
			_, err = pool.Exec(ctx, `CREATE TABLE items (id bigint PRIMARY KEY, payload text);
     ALTER TABLE items ALTER COLUMN payload SET STORAGE EXTERNAL;
     CREATE PUBLICATION items_pub FOR TABLE items; CREATE SCHEMA warehouse`)
			require.NoError(t, err)
			if fullIdentity {
				_, err = pool.Exec(ctx, `ALTER TABLE items REPLICA IDENTITY FULL`)
				require.NoError(t, err)
			}
			payload := strings.Repeat("abcdefghij", 5000)
			_, err = pool.Exec(ctx, `INSERT INTO items VALUES (1, $1)`, payload)
			require.NoError(t, err)
			cfg := &config.IngestConfig{SourceURI: strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=items_pub", SourceTable: "public.items", DestURI: uri, DestTable: "warehouse.items", IncrementalStrategy: config.StrategyMerge, PageSize: 1}
			require.NoError(t, pipeline.New(cfg).Run(ctx))
			_, err = pool.Exec(ctx, `UPDATE items SET id=2 WHERE id=1`)
			require.NoError(t, err)
			err = pipeline.New(cfg).Run(ctx)
			if !fullIdentity {
				require.ErrorContains(t, err, "REPLICA IDENTITY FULL")
				var oldPayload string
				require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM warehouse.items WHERE id=1 AND NOT _cdc_deleted`).Scan(&oldPayload))
				require.Equal(t, payload, oldPayload, "an incomplete key move must leave the destination intact")
				_, err = pool.Exec(ctx, `ALTER TABLE items REPLICA IDENTITY FULL`)
				require.NoError(t, err)
				cfg.FullRefresh = true
				require.NoError(t, pipeline.New(cfg).Run(ctx))
			} else {
				require.NoError(t, err)
			}
			var actual string
			require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM warehouse.items WHERE id=2 AND NOT _cdc_deleted`).Scan(&actual))
			require.Equal(t, payload, actual)
			var active int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM warehouse.items WHERE NOT _cdc_deleted`).Scan(&active))
			require.Equal(t, 1, active)
		})
	}
}

func TestPostgresCDCReplicaIdentityKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, uri := setupPostgresCDCContainer(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()
	pool, err := pgxpool.New(ctx, uri)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE items (id bigint PRIMARY KEY, code text NOT NULL, payload text NOT NULL);
  CREATE UNIQUE INDEX items_identity ON items(code) INCLUDE (payload);
  ALTER TABLE items REPLICA IDENTITY USING INDEX items_identity;
  INSERT INTO items VALUES (1, 'a', 'payload');
  CREATE PUBLICATION items_pub FOR TABLE items; CREATE SCHEMA warehouse`)
	require.NoError(t, err)
	cdcURI := strings.Replace(uri, "postgres://", "postgres+cdc://", 1) + "&publication=items_pub"
	src := postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI))
	defer func() { _ = src.Close(ctx) }()
	table, err := src.GetTable(ctx, source.TableRequest{Name: "public.items"})
	require.NoError(t, err)
	require.Equal(t, []string{"code"}, table.PrimaryKeys(), "replica identity overrides PK and excludes INCLUDE columns")
	tables, err := src.GetTables(ctx)
	require.NoError(t, err)
	require.Len(t, tables, 1)
	require.Equal(t, []string{"code"}, tables[0].PrimaryKeys)
	for _, keys := range [][]string{{"id"}, {"code", "payload"}} {
		_, err = src.GetTable(ctx, source.TableRequest{Name: "public.items", PrimaryKeys: keys})
		require.ErrorContains(t, err, "do not match its replica identity")
	}
	require.NoError(t, src.Close(ctx))
	cfg := &config.IngestConfig{SourceURI: cdcURI, SourceTable: "public.items", DestURI: uri, DestTable: "warehouse.items", IncrementalStrategy: config.StrategyMerge}
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	_, err = pool.Exec(ctx, `UPDATE items SET id=2 WHERE code='a'`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	var id, count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), min(id) FROM warehouse.items WHERE NOT _cdc_deleted`).Scan(&count, &id))
	require.Equal(t, 1, count)
	require.Equal(t, 2, id)
	_, err = pool.Exec(ctx, `DELETE FROM items WHERE code='a'`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM warehouse.items WHERE NOT _cdc_deleted`).Scan(&count))
	require.Zero(t, count)
	_, err = pool.Exec(ctx, `ALTER TABLE items REPLICA IDENTITY FULL`)
	require.NoError(t, err)
	src = postgres_cdc.NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI))
	defer func() { _ = src.Close(ctx) }()
	table, err = src.GetTable(ctx, source.TableRequest{Name: "public.items", PrimaryKeys: []string{"code"}})
	require.NoError(t, err, "FULL identity supports an explicitly selected unique key")
	require.Equal(t, []string{"code"}, table.PrimaryKeys())
}
