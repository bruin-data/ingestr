//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	mssqldest "github.com/bruin-data/ingestr/pkg/destination/mssql"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/require"
)

func TestMSSQLDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	if mssqlDest.uri == "" {
		t.Skip("shared SQL Server destination container not available")
	}

	ctx := t.Context()
	targetTable := "dbo.cdc_lsn_target_" + uniqueSuffix()
	stagingTable := "dbo.cdc_lsn_staging_" + uniqueSuffix()
	dest := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest.Connect(ctx, mssqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTableMSSQL(stagingTable)))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteTableMSSQL(targetTable)))
	})

	setup := []string{
		fmt.Sprintf(`CREATE TABLE %s (
			[id] BIGINT PRIMARY KEY,
			[payload] NVARCHAR(255),
			[_cdc_lsn] NVARCHAR(128),
			[_cdc_deleted] BIT,
			[_cdc_synced_at] DATETIME2(6)
		)`, quoteTableMSSQL(targetTable)),
		fmt.Sprintf(`CREATE TABLE %s (
			[id] BIGINT,
			[payload] NVARCHAR(255),
			[_cdc_lsn] NVARCHAR(128),
			[_cdc_deleted] BIT,
			[_cdc_synced_at] DATETIME2(6),
			[_cdc_unchanged_cols] NVARCHAR(MAX)
		)`, quoteTableMSSQL(stagingTable)),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, N'newer-active', N'00000000000000000030', 0, '2026-01-03'),
			(2, N'newer-deleted', N'00000000000000000030', 1, '2026-01-03'),
			(3, N'legacy', NULL, 0, '2026-01-01'),
			(6, N'same-active', N'00000000000000000010', 0, '2026-01-01'),
			(7, N'same-deleted', N'00000000000000000010', 1, '2026-01-01'),
			(8, N'tie-delete', N'00000000000000000010', 0, '2026-01-01'),
			(9, N'toast-newer', N'00000000000000000030', 0, '2026-01-03'),
			(11, N'known-lsn', N'00000000000000000030', 0, '2026-01-03')`, quoteTableMSSQL(targetTable)),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, N'stale-active', N'00000000000000000020', 0, '2026-01-02', N'[]'),
			(1, NULL, N'00000000000000000025', 1, '2026-01-02', N'[]'),
			(2, N'stale-resurrection', N'00000000000000000020', 0, '2026-01-02', N'[]'),
			(3, N'first-cdc-update', N'00000000000000000010', 0, '2026-01-02', N'[]'),
			(4, N'first-insert', N'00000000000000000010', 0, '2026-01-02', N'[]'),
			(5, NULL, N'00000000000000000010', 1, '2026-01-02', N'[]'),
			(6, N'same-replay', N'00000000000000000010', 0, '2026-01-02', N'[]'),
			(7, N'same-resurrection', N'00000000000000000010', 0, '2026-01-02', N'[]'),
			(8, NULL, N'00000000000000000010', 1, '2026-01-02', N'[]'),
			(9, NULL, N'00000000000000000020', 0, '2026-01-02', N'["payload"]'),
			(10, N'insert-then-delete', N'00000000000000000010', 0, '2026-01-02', N'[]'),
			(10, NULL, N'00000000000000000010', 1, '2026-01-02', N'[]'),
			(11, N'null-lsn-regression', NULL, 0, '2026-01-04', N'[]')`, quoteTableMSSQL(stagingTable)),
	}
	for _, statement := range setup {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	opts := destination.MergeOptions{
		TargetTable:  targetTable,
		StagingTable: stagingTable,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}
	require.NoError(t, dest.MergeTable(ctx, opts))

	expected := map[int64]struct {
		payload string
		lsn     string
		deleted bool
		synced  string
	}{
		1:  {"newer-active", "00000000000000000030", false, "2026-01-03"},
		2:  {"newer-deleted", "00000000000000000030", true, "2026-01-03"},
		3:  {"first-cdc-update", "00000000000000000010", false, "2026-01-02"},
		4:  {"first-insert", "00000000000000000010", false, "2026-01-02"},
		6:  {"same-active", "00000000000000000010", false, "2026-01-01"},
		7:  {"same-deleted", "00000000000000000010", true, "2026-01-01"},
		8:  {"tie-delete", "00000000000000000010", true, "2026-01-02"},
		9:  {"toast-newer", "00000000000000000030", false, "2026-01-03"},
		10: {"insert-then-delete", "00000000000000000010", true, "2026-01-02"},
		11: {"known-lsn", "00000000000000000030", false, "2026-01-03"},
	}
	for id, want := range expected {
		var payload, lsn, synced string
		var deleted bool
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT [payload], COALESCE([_cdc_lsn], N''), [_cdc_deleted], CONVERT(varchar(10), [_cdc_synced_at], 23)
			FROM %s WHERE [id] = @p1
		`, quoteTableMSSQL(targetTable)), id).Scan(&payload, &lsn, &deleted, &synced))
		require.Equal(t, want.payload, payload, "id %d payload", id)
		require.Equal(t, want.lsn, lsn, "id %d LSN", id)
		require.Equal(t, want.deleted, deleted, "id %d deleted", id)
		require.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 5", quoteTableMSSQL(targetTable))).Scan(&count))
	require.Zero(t, count, "a delete-only change must not create a payload-less row")

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, N'newest', N'00000000000000000040', 0, '2026-01-04', N'[]')`, quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertMSSQLCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", false)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, N'00000000000000000040', 1, '2026-01-04', N'[]')`, quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertMSSQLCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", true)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, N'00000000000000000050', 0, '2026-01-05', N'["payload"]')`, quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertMSSQLCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", false)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", quoteTableMSSQL(stagingTable))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, N'newer-outside-predicate', N'00000000000000000060', 0, '2026-01-06', N'[]')`, quoteTableMSSQL(stagingTable))))
	opts.IncrementalPredicate = "target.[id] > 100"
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertMSSQLCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", false)
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 1", quoteTableMSSQL(targetTable))).Scan(&count))
	require.Equal(t, 1, count)
}

func assertMSSQLCDCRow(t *testing.T, ctx context.Context, db *sql.DB, table, wantPayload, wantLSN string, wantDeleted bool) {
	t.Helper()
	var payload, lsn string
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT [payload], [_cdc_lsn], [_cdc_deleted] FROM %s WHERE [id] = 1`, quoteTableMSSQL(table))).Scan(&payload, &lsn, &deleted))
	require.Equal(t, wantPayload, payload)
	require.Equal(t, wantLSN, lsn)
	require.Equal(t, wantDeleted, deleted)
}
