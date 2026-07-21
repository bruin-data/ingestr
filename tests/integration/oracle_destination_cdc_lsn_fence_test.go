//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	oracledest "github.com/bruin-data/ingestr/pkg/destination/oracle"
	_ "github.com/sijms/go-ora/v2"
	"github.com/stretchr/testify/require"
)

func TestOracleDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}

	ctx := t.Context()
	targetTable := "CDC_LSN_T_" + uniqueSuffix()
	stagingTable := "CDC_LSN_S_" + uniqueSuffix()
	auditTable := "CDC_LSN_A_" + uniqueSuffix()
	auditTrigger := "CDC_LSN_TR_" + uniqueSuffix()
	dest := oracledest.NewOracleDestination()
	require.NoError(t, dest.Connect(ctx, oracleDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER %s", auditTrigger))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", auditTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", stagingTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", targetTable))
	})

	setup := []string{
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(19) PRIMARY KEY,
			PAYLOAD VARCHAR2(255 CHAR),
			"_CDC_LSN" VARCHAR2(64 CHAR),
			"_CDC_DELETED" NUMBER(1),
			"_CDC_SYNCED_AT" TIMESTAMP
		)`, targetTable),
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(19),
			PAYLOAD VARCHAR2(255 CHAR),
			"_CDC_LSN" VARCHAR2(64 CHAR),
			"_CDC_DELETED" NUMBER(1),
			"_CDC_SYNCED_AT" TIMESTAMP,
			"_CDC_UNCHANGED_COLS" VARCHAR2(4000 CHAR)
		)`, stagingTable),
		fmt.Sprintf(`INSERT ALL
			INTO %s VALUES (1, 'newer-active', '00000000000000000030', 0, TIMESTAMP '2026-01-03 00:00:00')
			INTO %s VALUES (2, 'newer-deleted', '00000000000000000030', 1, TIMESTAMP '2026-01-03 00:00:00')
			INTO %s VALUES (3, 'legacy', NULL, 0, TIMESTAMP '2026-01-01 00:00:00')
			INTO %s VALUES (6, 'same-active', '00000000000000000010', 0, TIMESTAMP '2026-01-01 00:00:00')
			INTO %s VALUES (7, 'same-deleted', '00000000000000000010', 1, TIMESTAMP '2026-01-01 00:00:00')
			INTO %s VALUES (8, 'tie-delete', '00000000000000000010', 0, TIMESTAMP '2026-01-01 00:00:00')
			INTO %s VALUES (9, 'toast-newer', '00000000000000000030', 0, TIMESTAMP '2026-01-03 00:00:00')
			INTO %s VALUES (11, 'older-row-image', '00000000000000000010', 0, TIMESTAMP '2026-01-01 00:00:00')
		SELECT 1 FROM DUAL`, targetTable, targetTable, targetTable, targetTable, targetTable, targetTable, targetTable, targetTable),
		fmt.Sprintf(`INSERT ALL
			INTO %s VALUES (1, 'stale-active', '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (1, NULL, '00000000000000000025', 1, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (2, 'stale-resurrection', '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (3, 'first-cdc-update', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (4, 'first-insert', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (5, NULL, '00000000000000000010', 1, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (6, 'same-replay', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (7, 'same-resurrection', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (8, NULL, '00000000000000000010', 1, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (9, NULL, '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '["payload"]')
			INTO %s VALUES (10, 'insert-then-delete', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (10, NULL, '00000000000000000010', 1, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (11, 'latest-row-image', '00000000000000000010', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (11, NULL, '00000000000000000010', 1, TIMESTAMP '2026-01-02 00:00:00', '[]')
		SELECT 1 FROM DUAL`, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable, stagingTable),
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
		deleted int
		synced  string
	}{
		1:  {"newer-active", "00000000000000000030", 0, "2026-01-03"},
		2:  {"newer-deleted", "00000000000000000030", 1, "2026-01-03"},
		3:  {"first-cdc-update", "00000000000000000010", 0, "2026-01-02"},
		4:  {"first-insert", "00000000000000000010", 0, "2026-01-02"},
		5:  {"<null>", "00000000000000000010", 1, "2026-01-02"},
		6:  {"same-active", "00000000000000000010", 0, "2026-01-01"},
		7:  {"same-deleted", "00000000000000000010", 1, "2026-01-01"},
		8:  {"tie-delete", "00000000000000000010", 1, "2026-01-02"},
		9:  {"toast-newer", "00000000000000000030", 0, "2026-01-03"},
		10: {"insert-then-delete", "00000000000000000010", 1, "2026-01-02"},
		11: {"latest-row-image", "00000000000000000010", 1, "2026-01-02"},
	}
	for id, want := range expected {
		var payload, lsn, synced string
		var deleted int
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT NVL(PAYLOAD, '<null>'), NVL("_CDC_LSN", ''), "_CDC_DELETED", TO_CHAR("_CDC_SYNCED_AT", 'YYYY-MM-DD')
			FROM %s WHERE ID = :1
		`, targetTable), id).Scan(&payload, &lsn, &deleted, &synced))
		require.Equal(t, want.payload, payload, "id %d payload", id)
		require.Equal(t, want.lsn, lsn, "id %d LSN", id)
		require.Equal(t, want.deleted, deleted, "id %d deleted", id)
		require.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (TARGET_ID NUMBER(19))`, auditTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW BEGIN INSERT INTO %s VALUES (:NEW.ID); END;`, auditTrigger, targetTable, auditTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	var replayUpdates int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, auditTable)).Scan(&replayUpdates))
	require.Zero(t, replayUpdates)
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`DROP TRIGGER %s`, auditTrigger)))

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("DELETE FROM %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newest', '00000000000000000040', 0, TIMESTAMP '2026-01-04 00:00:00', '[]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertOracleCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", 0)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("DELETE FROM %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000040', 1, TIMESTAMP '2026-01-04 00:00:00', '[]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertOracleCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000040", 1)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("DELETE FROM %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000000000000050', 0, TIMESTAMP '2026-01-05 00:00:00', '["payload"]')`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertOracleCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", 0)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("DELETE FROM %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'stale-outside-predicate', '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')`, stagingTable)))
	opts.IncrementalPredicate = `target."ID" > 100`
	require.NoError(t, dest.MergeTable(ctx, opts))
	assertOracleCDCRow(t, ctx, db, targetTable, "newest", "00000000000000000050", 0)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE ID = 1", targetTable)).Scan(&count))
	require.Equal(t, 1, count)
}

func TestOracleDestinationCDCInternalAliasesDoNotCollide(t *testing.T) {
	if oracleDest.uri == "" {
		t.Skip("shared oracle destination container not available")
	}

	ctx := t.Context()
	targetTable := "CDC_ALIAS_T_" + uniqueSuffix()
	stagingTable := "CDC_ALIAS_S_" + uniqueSuffix()
	dest := oracledest.NewOracleDestination()
	require.NoError(t, dest.Connect(ctx, oracleDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("oracle", oracleSQLConnString(oracleDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", stagingTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE %s PURGE", targetTable))
	})

	const userColumnDefinitions = `
		BRUIN_ACTIVE_RN VARCHAR2(255 CHAR),
		BRUIN_ACTIVE_RN_2 VARCHAR2(255 CHAR),
		"__INGESTR_HAS_EQUAL_LSN_DELETE" VARCHAR2(255 CHAR),
		"__INGESTR_HAS_EQUAL_LSN_DELETE_2" VARCHAR2(255 CHAR)`
	for _, statement := range []string{
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(19) PRIMARY KEY,
			PAYLOAD VARCHAR2(255 CHAR),
			%s,
			"_CDC_LSN" VARCHAR2(64 CHAR),
			"_CDC_DELETED" NUMBER(1),
			"_CDC_SYNCED_AT" TIMESTAMP
		)`, targetTable, userColumnDefinitions),
		fmt.Sprintf(`CREATE TABLE %s (
			ID NUMBER(19),
			PAYLOAD VARCHAR2(255 CHAR),
			%s,
			"_CDC_LSN" VARCHAR2(64 CHAR),
			"_CDC_DELETED" NUMBER(1),
			"_CDC_SYNCED_AT" TIMESTAMP,
			"_CDC_UNCHANGED_COLS" VARCHAR2(4000 CHAR)
		)`, stagingTable, userColumnDefinitions),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'old', 'old-rn', 'old-rn-2', 'old-marker', 'old-marker-2', '00000000000000000020', 0, TIMESTAMP '2026-01-01 00:00:00')`, targetTable),
		fmt.Sprintf(`INSERT ALL
			INTO %s VALUES (1, 'active-existing', 'existing-rn', 'existing-rn-2', 'existing-marker', 'existing-marker-2', '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (1, NULL, NULL, NULL, NULL, NULL, '00000000000000000020', 1, TIMESTAMP '2026-01-03 00:00:00', '[]')
			INTO %s VALUES (2, 'active-new', 'new-rn', 'new-rn-2', 'new-marker', 'new-marker-2', '00000000000000000020', 0, TIMESTAMP '2026-01-02 00:00:00', '[]')
			INTO %s VALUES (2, NULL, NULL, NULL, NULL, NULL, '00000000000000000020', 1, TIMESTAMP '2026-01-03 00:00:00', '[]')
		SELECT 1 FROM DUAL`, stagingTable, stagingTable, stagingTable, stagingTable),
	} {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	columns := []string{
		"id",
		"payload",
		"BrUiN_Active_Rn",
		"bruin_active_rn_2",
		`"__INGESTR_HAS_EQUAL_LSN_DELETE"`,
		`"__INGESTR_HAS_EQUAL_LSN_DELETE_2"`,
		destination.CDCLSNColumn,
		destination.CDCDeletedColumn,
		destination.CDCSyncedAtColumn,
		destination.CDCUnchangedColsColumn,
	}
	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable:  targetTable,
		StagingTable: stagingTable,
		PrimaryKeys:  []string{"id"},
		Columns:      columns,
	}))

	for id, want := range map[int64][]string{
		1: {"active-existing", "existing-rn", "existing-rn-2", "existing-marker", "existing-marker-2"},
		2: {"active-new", "new-rn", "new-rn-2", "new-marker", "new-marker-2"},
	} {
		var payload, activeRN, activeRN2, marker, marker2, lsn, synced string
		var deleted int
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT PAYLOAD, BRUIN_ACTIVE_RN, BRUIN_ACTIVE_RN_2,
				"__INGESTR_HAS_EQUAL_LSN_DELETE", "__INGESTR_HAS_EQUAL_LSN_DELETE_2",
				"_CDC_LSN", "_CDC_DELETED", TO_CHAR("_CDC_SYNCED_AT", 'YYYY-MM-DD')
			FROM %s WHERE ID = :1
		`, targetTable), id).Scan(&payload, &activeRN, &activeRN2, &marker, &marker2, &lsn, &deleted, &synced))
		require.Equal(t, want, []string{payload, activeRN, activeRN2, marker, marker2}, "id %d active row image", id)
		require.Equal(t, "00000000000000000020", lsn, "id %d LSN", id)
		require.Equal(t, 1, deleted, "id %d deleted", id)
		require.Equal(t, "2026-01-03", synced, "id %d synced timestamp", id)
	}
}

func assertOracleCDCRow(t *testing.T, ctx context.Context, db *sql.DB, table, wantPayload, wantLSN string, wantDeleted int) {
	t.Helper()
	var payload, lsn string
	var deleted int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT PAYLOAD, "_CDC_LSN", "_CDC_DELETED" FROM %s WHERE ID = 1`, table)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, wantPayload, payload)
	require.Equal(t, wantLSN, lsn)
	require.Equal(t, wantDeleted, deleted)
}
