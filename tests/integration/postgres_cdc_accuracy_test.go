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
