//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	snowflakedest "github.com/bruin-data/ingestr/pkg/destination/snowflake"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := t.Context()
	targetTable := "PUBLIC.CDC_LSN_TARGET_" + uniqueSuffix()
	stagingTable := "PUBLIC.CDC_LSN_STAGING_" + uniqueSuffix()
	dest := snowflakedest.NewSnowflakeDestination()
	require.NoError(t, dest.Connect(ctx, snowflakeTestURI))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+stagingTable)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+targetTable)
	})

	setup := []string{
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(38,0),
			PAYLOAD VARCHAR,
			_CDC_LSN VARCHAR,
			_CDC_DELETED BOOLEAN,
			_CDC_SYNCED_AT TIMESTAMP_NTZ
		)`, targetTable),
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(38,0),
			PAYLOAD VARCHAR,
			_CDC_LSN VARCHAR,
			_CDC_DELETED BOOLEAN,
			_CDC_SYNCED_AT TIMESTAMP_NTZ,
			_CDC_UNCHANGED_COLS VARCHAR
		)`, stagingTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'newer-active', '00000000000000000030', false, '2026-01-03'),
			(2, 'newer-deleted', '00000000000000000030', true, '2026-01-03'),
			(3, 'legacy', NULL, false, '2026-01-01'),
			(6, 'same-active', '00000000000000000010', false, '2026-01-01'),
			(7, 'same-deleted', '00000000000000000010', true, '2026-01-01'),
			(8, 'tie-delete', '00000000000000000010', false, '2026-01-01'),
			(9, 'toast-newer', '00000000000000000030', false, '2026-01-03'),
			(11, 'known-lsn', '00000000000000000030', false, '2026-01-03')`, targetTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'stale-active', '00000000000000000020', false, '2026-01-02', '[]'),
			(1, NULL, '00000000000000000025', true, '2026-01-02', '[]'),
			(2, 'stale-resurrection', '00000000000000000020', false, '2026-01-02', '[]'),
			(3, 'first-cdc-update', '00000000000000000010', false, '2026-01-02', '[]'),
			(4, 'first-insert', '00000000000000000010', false, '2026-01-02', '[]'),
			(5, NULL, '00000000000000000010', true, '2026-01-02', '[]'),
			(6, 'same-replay', '00000000000000000010', false, '2026-01-02', '[]'),
			(7, 'same-resurrection', '00000000000000000010', false, '2026-01-02', '[]'),
			(8, NULL, '00000000000000000010', true, '2026-01-02', '[]'),
			(9, NULL, '00000000000000000020', false, '2026-01-02', '["payload"]'),
			(10, 'insert-then-delete', '00000000000000000010', false, '2026-01-02', '[]'),
			(10, NULL, '00000000000000000010', true, '2026-01-02', '[]'),
			(11, 'null-lsn-regression', NULL, false, '2026-01-04', '[]')`, stagingTable),
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
		5:  {"<null>", "00000000000000000010", true, "2026-01-02"},
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
			SELECT COALESCE(PAYLOAD, '<null>'), COALESCE(_CDC_LSN, ''), _CDC_DELETED, TO_VARCHAR(_CDC_SYNCED_AT, 'YYYY-MM-DD')
			FROM %s WHERE ID = %d
		`, targetTable, id)).Scan(&payload, &lsn, &deleted, &synced))
		require.Equal(t, want.payload, payload, "id %d payload", id)
		require.Equal(t, want.lsn, lsn, "id %d LSN", id)
		require.Equal(t, want.deleted, deleted, "id %d deleted", id)
		require.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+stagingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (5, 'stale-resurrection', '00000000000000000005', false, '2026-01-01', '[]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	var tombstonePayload, tombstoneLSN, tombstoneSynced string
	var tombstoneDeleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(PAYLOAD, '<null>'), _CDC_LSN, _CDC_DELETED, TO_VARCHAR(_CDC_SYNCED_AT, 'YYYY-MM-DD')
		FROM %s WHERE ID = 5
	`, targetTable)).Scan(&tombstonePayload, &tombstoneLSN, &tombstoneDeleted, &tombstoneSynced))
	require.Equal(t, "<null>", tombstonePayload)
	require.Equal(t, "00000000000000000010", tombstoneLSN)
	require.True(t, tombstoneDeleted)
	require.Equal(t, "2026-01-02", tombstoneSynced)

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+stagingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newest', '00000000000000000040', false, '2026-01-04', '[]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertSnowflakeCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", false)

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+stagingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000040', true, '2026-01-04', '[]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertSnowflakeCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", true)

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+stagingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000050', false, '2026-01-05', '["payload"]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertSnowflakeCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", false)

	require.NoError(t, dest.Exec(ctx, "TRUNCATE TABLE "+stagingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newer-outside-predicate', '00000000000000000060', false, '2026-01-06', '[]')`, stagingTable)))
	opts.IncrementalPredicate = `target."ID" > 100`
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertSnowflakeCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", false)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE ID = 1", targetTable)).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSnowflakeDestinationCDCMergeAvoidsInternalAliasCollisions(t *testing.T) {
	snowflakeTestURI := os.Getenv("GONG_TEST_SNOWFLAKE_URI")
	if snowflakeTestURI == "" {
		t.Skip("GONG_TEST_SNOWFLAKE_URI not set")
	}

	ctx := t.Context()
	targetTable := "PUBLIC.CDC_ALIAS_TARGET_" + uniqueSuffix()
	stagingTable := "PUBLIC.CDC_ALIAS_STAGING_" + uniqueSuffix()
	dest := snowflakedest.NewSnowflakeDestination()
	require.NoError(t, dest.Connect(ctx, snowflakeTestURI))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := snowflakeOpenDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+stagingTable)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+targetTable)
	})

	const userColumnDefinitions = `
		__BRUIN_DEDUP_RN VARCHAR,
		__BRUIN_DEDUP_RN_2 VARCHAR,
		"__ingestr_has_active" VARCHAR,
		"__ingestr_has_active_2" VARCHAR`
	for _, statement := range []string{
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(38,0),
			PAYLOAD VARCHAR,
			%s,
			_CDC_LSN VARCHAR,
			_CDC_DELETED BOOLEAN,
			_CDC_SYNCED_AT TIMESTAMP_NTZ
		)`, targetTable, userColumnDefinitions),
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(38,0),
			PAYLOAD VARCHAR,
			%s,
			_CDC_LSN VARCHAR,
			_CDC_DELETED BOOLEAN,
			_CDC_SYNCED_AT TIMESTAMP_NTZ,
			_CDC_UNCHANGED_COLS VARCHAR
		)`, stagingTable, userColumnDefinitions),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'old', 'old-rn', 'old-rn-2', 'old-active', 'old-active-2', '00000000000000000020', false, '2026-01-01')`, targetTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'active', 'user-rn', 'user-rn-2', 'user-active', 'user-active-2', '00000000000000000020', false, '2026-01-02', '[]'),
			(1, NULL, NULL, NULL, NULL, NULL, '00000000000000000020', true, '2026-01-03', '[]')`, stagingTable),
	} {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable:  targetTable,
		StagingTable: stagingTable,
		PrimaryKeys:  []string{"id"},
		Columns: []string{
			"id", "payload", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2",
			`"__ingestr_has_active"`, `"__ingestr_has_active_2"`,
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn,
		},
	}))

	var payload, rn, rn2, active, active2, lsn, synced string
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT PAYLOAD, __BRUIN_DEDUP_RN, __BRUIN_DEDUP_RN_2,
			"__ingestr_has_active", "__ingestr_has_active_2",
			_CDC_LSN, _CDC_DELETED, TO_VARCHAR(_CDC_SYNCED_AT, 'YYYY-MM-DD')
		FROM %s WHERE ID = 1
	`, targetTable)).Scan(&payload, &rn, &rn2, &active, &active2, &lsn, &deleted, &synced))
	require.Equal(t, []string{"active", "user-rn", "user-rn-2", "user-active", "user-active-2"}, []string{payload, rn, rn2, active, active2})
	require.Equal(t, "00000000000000000020", lsn)
	require.True(t, deleted)
	require.Equal(t, "2026-01-03", synced)
}

func assertSnowflakeCDCRow(t *testing.T, ctx context.Context, db *sql.DB, table, wantPayload, wantLSN string, wantDeleted bool) {
	t.Helper()
	var payload, lsn string
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT PAYLOAD, _CDC_LSN, _CDC_DELETED FROM %s WHERE ID = 1`, table)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, wantPayload, payload)
	require.Equal(t, wantLSN, lsn)
	require.Equal(t, wantDeleted, deleted)
}
