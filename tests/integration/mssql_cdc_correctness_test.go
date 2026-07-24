//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMSSQLCDCPostgresRun(t *testing.T, ctx context.Context, prefix string) (*sql.DB, *config.IngestConfig, string) {
	t.Helper()

	dbName, db := setupMSSQLCDCDatabase(t, ctx)
	createMSSQLCDCItemsTable(t, ctx, db)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 3)

	destSchema := uniqueSchemaName(t, prefix)
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"mode": "batch"}),
		SourceTable: "dbo.items",
		DestURI:     pgDest.uri,
		DestTable:   destSchema + ".items_dest",
	}
	return db, cfg, destSchema
}

func openPostgresDest(t *testing.T) *sql.DB {
	t.Helper()
	pg, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })
	return pg
}

func queryPostgresCount(t *testing.T, ctx context.Context, pg *sql.DB, query string) int {
	t.Helper()
	var n int
	require.NoError(t, pg.QueryRowContext(ctx, query).Scan(&n))
	return n
}

// TestMSSQLCDC_PrimaryKeyUpdate_Postgres covers a primary-key move: the
// before-image must be replayed as a delete of the old key, otherwise the
// destination keeps the orphaned row forever.
func TestMSSQLCDC_PrimaryKeyUpdate_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	db, cfg, destSchema := setupMSSQLCDCPostgresRun(t, ctx, "mssql_cdc_pkmove")

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Move the primary key of one row and plain-update another in the same window.
	_, err := db.ExecContext(ctx, `UPDATE dbo.items SET id = 100 WHERE id = 1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE dbo.items SET value = 250 WHERE id = 2`)
	require.NoError(t, err)
	// 3 + pk update (2) + update (2) = 7
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 7)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg := openPostgresDest(t)
	count := func(query string, args ...any) int {
		return queryPostgresCount(t, ctx, pg, fmt.Sprintf(query, args...))
	}

	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 1 AND NOT "_cdc_deleted"`, destSchema),
		"old key must be deleted by a primary-key update")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 1 AND "_cdc_deleted"`, destSchema),
		"old key lands as a soft delete, matching plain-delete semantics")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 100 AND name = 'item1' AND value = 100 AND NOT "_cdc_deleted"`, destSchema),
		"row must land under the new key")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 2 AND value = 250 AND NOT "_cdc_deleted"`, destSchema),
		"plain update in the same window must still apply")
	assert.Equal(t, 4, count(`SELECT COUNT(*) FROM %q.items_dest`, destSchema))
}

// TestMSSQLCDC_ResumeMidTransaction_Postgres simulates a crash that leaves the
// destination's max _cdc_lsn mid-transaction: the next run must re-read the
// whole transaction instead of skipping its tail.
func TestMSSQLCDC_ResumeMidTransaction_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	db, cfg, destSchema := setupMSSQLCDCPostgresRun(t, ctx, "mssql_cdc_midtx")

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// One transaction inserting two rows: both share a start_lsn, so a resume
	// cursor on the first must not skip the second.
	_, err := db.ExecContext(ctx, `
		BEGIN TRAN
		INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)
		INSERT INTO dbo.items (id, name, value) VALUES (5, N'item5', 500)
		COMMIT
	`)
	require.NoError(t, err)
	// 3 + 2 = 5
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 5)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg := openPostgresDest(t)
	count := func(query string, args ...any) int {
		return queryPostgresCount(t, ctx, pg, fmt.Sprintf(query, args...))
	}
	assert.Equal(t, 5, count(`SELECT COUNT(*) FROM %q.items_dest`, destSchema), "both transaction rows delivered")

	// Simulate a crash where the second run's write was only partially
	// durable and its state commit never happened: remove the row carrying
	// the newest change AND every state event of the second run's generation,
	// reverting the connector to the first run's completed-snapshot state.
	// Resume must then re-deliver the transaction and restore the lost row.
	// (Deleting destination rows while keeping the second run's state would
	// be out-of-band tampering: managed state persists only after the write,
	// so state never runs ahead of data in a real crash.)
	var droppedID int64
	require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(
		`DELETE FROM %q.items_dest WHERE "_cdc_lsn" = (SELECT MAX("_cdc_lsn") FROM %q.items_dest) RETURNING id`,
		destSchema, destSchema,
	)).Scan(&droppedID))
	var stateSchema string
	require.NoError(t, pg.QueryRowContext(ctx, `
		SELECT table_schema FROM information_schema.tables
		WHERE table_name = 'cdc_state'
		ORDER BY table_schema LIMIT 1`).Scan(&stateSchema))
	res, err := pg.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %q.cdc_state AS cs
		USING (
			SELECT connector_id, MAX(state_generation) AS max_gen
			FROM %q.cdc_state
			WHERE destination_table = $1
			GROUP BY connector_id
		) AS mine
		WHERE cs.connector_id = mine.connector_id AND cs.state_generation = mine.max_gen`,
		stateSchema, stateSchema), cfg.DestTable)
	require.NoError(t, err)
	deleted, err := res.RowsAffected()
	require.NoError(t, err)
	require.Positive(t, deleted, "the second run's state generation must exist to be rolled back")
	assert.Contains(t, []int64{4, 5}, droppedID)
	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = %d`, destSchema, droppedID))

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.Equal(t, 5, count(`SELECT COUNT(*) FROM %q.items_dest`, destSchema),
		"resume must re-read the transaction and restore the lost tail row")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = %d AND NOT "_cdc_deleted"`, destSchema, droppedID))
}

// TestMSSQLCDC_ResnapshotCleansDestination_Postgres forces a re-snapshot by
// purging the change table past the resume cursor. The replacement snapshot
// must empty the destination first, or rows deleted while the cursor was
// invalid linger as stale data.
func TestMSSQLCDC_ResnapshotCleansDestination_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	db, cfg, destSchema := setupMSSQLCDCPostgresRun(t, ctx, "mssql_cdc_resnap")

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	_, err := db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 2`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	// 3 + delete (1) + insert (1) = 5
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 5)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	// Advance the log past the delivered watermark, then purge the change
	// table up to it: the resume cursor now points below the low watermark.
	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (5, N'item5', 500)`)
	require.NoError(t, err)
	// 5 + insert (1) = 6
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 6)

	_, err = db.ExecContext(ctx, `
		DECLARE @lsn binary(10) = sys.fn_cdc_get_max_lsn();
		EXEC sys.sp_cdc_cleanup_change_table @capture_instance = N'dbo_items', @low_water_mark = @lsn;
	`)
	require.NoError(t, err)

	// A delete issued while the cursor is invalid: the next run re-snapshots,
	// so the row vanishes from the snapshot. Depending on when the capture job
	// harvests it, the post-snapshot catch-up read may also deliver it as a
	// soft delete — either way no live row for id 3 may remain.
	_, err = db.ExecContext(ctx, `DELETE FROM dbo.items WHERE id = 3`)
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg := openPostgresDest(t)
	count := func(query string, args ...any) int {
		return queryPostgresCount(t, ctx, pg, fmt.Sprintf(query, args...))
	}

	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 2`, destSchema),
		"row soft-deleted before the purge must not linger after re-snapshot")
	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 3 AND NOT "_cdc_deleted"`, destSchema),
		"row deleted while the cursor was invalid must not linger as live after re-snapshot")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 1 AND NOT "_cdc_deleted"`, destSchema))
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 4 AND NOT "_cdc_deleted"`, destSchema))
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM %q.items_dest WHERE id = 5 AND NOT "_cdc_deleted"`, destSchema))
	assert.Equal(t, 3, count(`SELECT COUNT(*) FROM %q.items_dest WHERE NOT "_cdc_deleted"`, destSchema))
}

// TestMSSQLCDC_SnapshotConcurrentWriter_Postgres writes into the source while
// the initial snapshot runs under snapshot isolation. Every commit must land
// in the destination exactly once, whether the scan or the change stream sees
// it first.
func TestMSSQLCDC_SnapshotConcurrentWriter_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	dbName, db := setupMSSQLCDCDatabase(t, ctx)

	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE [%s] SET ALLOW_SNAPSHOT_ISOLATION ON", dbName))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `CREATE TABLE dbo.bulk_rows (id INT NOT NULL PRIMARY KEY, v INT NOT NULL)`)
	require.NoError(t, err)
	enableMSSQLCDCOnTable(t, ctx, db, "bulk_rows")

	// Seed enough rows that the snapshot scan takes a moment.
	_, err = db.ExecContext(ctx, `
		INSERT INTO dbo.bulk_rows (id, v)
		SELECT TOP 5000 ROW_NUMBER() OVER (ORDER BY (SELECT NULL)), 0
		FROM sys.objects a CROSS JOIN sys.objects b
	`)
	require.NoError(t, err)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_bulk_rows", 5000)

	destSchema := uniqueSchemaName(t, "mssql_cdc_bulk")
	ensurePostgresSchema(t, ctx, pgDest.uri, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, pgDest.uri, destSchema) })

	cfg := &config.IngestConfig{
		SourceURI:   mssqlURIForDatabase(t, mssqlDest.uri, "mssql+cdc", dbName, map[string]string{"mode": "batch"}),
		SourceTable: "dbo.bulk_rows",
		DestURI:     pgDest.uri,
		DestTable:   destSchema + ".bulk_rows_dest",
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		id := 100000
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := db.ExecContext(ctx, `INSERT INTO dbo.bulk_rows (id, v) VALUES (@p1, @p1)`, id); err == nil {
					id++
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	close(stop)
	wg.Wait()

	// Wait for the capture job to harvest every concurrent insert, then drain
	// with a second run.
	var sourceCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dbo.bulk_rows`).Scan(&sourceCount))
	waitForMSSQLCDCRows(t, ctx, db, "dbo_bulk_rows", sourceCount)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg := openPostgresDest(t)
	count := func(query string, args ...any) int {
		return queryPostgresCount(t, ctx, pg, fmt.Sprintf(query, args...))
	}

	destCount := count(`SELECT COUNT(*) FROM %q.bulk_rows_dest`, destSchema)
	assert.Equal(t, sourceCount, destCount, "destination must hold every source row")
	assert.Equal(t, destCount, count(`SELECT COUNT(DISTINCT id) FROM %q.bulk_rows_dest`, destSchema),
		"no row may be duplicated by snapshot/stream overlap or inclusive resume")

	srcIDs := map[int]bool{}
	srcRows, err := db.QueryContext(ctx, `SELECT id FROM dbo.bulk_rows`)
	require.NoError(t, err)
	for srcRows.Next() {
		var id int
		require.NoError(t, srcRows.Scan(&id))
		srcIDs[id] = true
	}
	require.NoError(t, srcRows.Err())
	_ = srcRows.Close()

	dstRows, err := pg.QueryContext(ctx, fmt.Sprintf(`SELECT id FROM %q.bulk_rows_dest`, destSchema))
	require.NoError(t, err)
	for dstRows.Next() {
		var id int
		require.NoError(t, dstRows.Scan(&id))
		assert.Contains(t, srcIDs, id, "destination row missing from source")
		delete(srcIDs, id)
	}
	require.NoError(t, dstRows.Err())
	_ = dstRows.Close()
	assert.Empty(t, srcIDs, "source rows missing from destination")
}

// TestMSSQLCDC_ManagedState_Postgres proves SQL Server CDC runs on
// destination-managed state when the destination supports it: the run must
// record a completed snapshot event, and a follow-up run must resume from
// that state rather than re-snapshot (original rows keep their exact
// _cdc_lsn stamps).
func TestMSSQLCDC_ManagedState_Postgres(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cfg, destSchema := setupMSSQLCDCPostgresRun(t, ctx, "mssqlstate")
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	pg := openPostgresDest(t)

	var stateSchema string
	err := pg.QueryRowContext(ctx, `
		SELECT table_schema FROM information_schema.tables
		WHERE table_name = 'cdc_state'
		ORDER BY table_schema LIMIT 1`).Scan(&stateSchema)
	require.NoError(t, err, "managed CDC state table must exist after an mssql+cdc run")

	completeSnapshots := queryPostgresCount(t, ctx, pg, fmt.Sprintf(
		`SELECT COUNT(*) FROM %q.cdc_state WHERE state_kind = 'snapshot' AND state_status = 'complete' AND source_table = 'dbo.items'`,
		stateSchema,
	))
	require.Positive(t, completeSnapshots, "a completed snapshot event must be recorded for dbo.items")

	stampsBefore := map[int]string{}
	rows, err := pg.QueryContext(ctx, fmt.Sprintf(`SELECT id, "_cdc_lsn" FROM %q.items_dest ORDER BY id`, destSchema))
	require.NoError(t, err)
	for rows.Next() {
		var id int
		var lsn string
		require.NoError(t, rows.Scan(&id, &lsn))
		stampsBefore[id] = lsn
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, stampsBefore, 3)

	_, err = db.ExecContext(ctx, `INSERT INTO dbo.items (id, name, value) VALUES (4, N'item4', 400)`)
	require.NoError(t, err)
	waitForMSSQLCDCRows(t, ctx, db, "dbo_items", 4)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.Equal(t, 4, queryPostgresCount(t, ctx, pg, fmt.Sprintf(`SELECT COUNT(*) FROM %q.items_dest`, destSchema)))
	for id, before := range stampsBefore {
		var after string
		require.NoError(t, pg.QueryRowContext(ctx, fmt.Sprintf(`SELECT "_cdc_lsn" FROM %q.items_dest WHERE id = $1`, destSchema), id).Scan(&after))
		assert.Equal(t, before, after, "row %d must keep its stamp: a changed stamp means the second run re-snapshotted instead of resuming from managed state", id)
	}
}
