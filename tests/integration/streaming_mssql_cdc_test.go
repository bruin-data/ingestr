//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMSSQLCDC_Streaming verifies continuous (--stream) ingestion from SQL
// Server CDC: an initial snapshot followed by live insert/update/delete changes
// merged into the destination via the polling stream, plus a clean shutdown on
// cancellation. SQL Server's capture job harvests changes every few seconds, so
// the assertions use generous timeouts.
func TestMSSQLCDC_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)
	createMSSQLCDCItemsTable(t, ctx, db)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 3)

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
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	cfg := &config.IngestConfig{
		SourceURI:     mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"poll_interval": "1s"}),
		SourceTable:   "dbo.items",
		DestURI:       destConnString,
		DestTable:     "public.items",
		Stream:        true,
		FlushInterval: 1 * time.Second,
		FlushRecords:  50,
		Progress:      config.ProgressLog,
	}

	count := func(where string) int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.items WHERE `+where).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	// Snapshot of 3 rows lands.
	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return count("_cdc_deleted = false") == 3
	}, 90*time.Second, time.Second, "snapshot of 3 rows should land")

	// Live changes.
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 150 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 2`)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return count("_cdc_deleted = false") == 3 && // 3 + insert(4) - delete(2)
			count("id = 1 AND value = 150 AND _cdc_deleted = false") == 1 &&
			count("id = 2 AND _cdc_deleted = true") == 1 &&
			count("id = 4 AND value = 400 AND _cdc_deleted = false") == 1
	}, 120*time.Second, time.Second, "live insert/update/delete should be merged")

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}

	assert.Equal(t, 3, count("_cdc_deleted = false"))
}
