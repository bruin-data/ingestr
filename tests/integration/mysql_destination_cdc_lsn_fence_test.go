//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	mysqldest "github.com/bruin-data/ingestr/pkg/destination/mysql"
	"github.com/bruin-data/ingestr/pkg/schema"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestMySQLDestinationCDCPrepareRequiresMatchingPrimaryKey(t *testing.T) {
	if mysqlDest.uri == "" {
		t.Skip("shared mysql destination container not available")
	}

	ctx := t.Context()
	suffix := uniqueSuffix()
	freshTable := "cdc_pk_fresh_" + suffix
	matchingTable := "cdc_pk_matching_" + suffix
	missingTable := "cdc_pk_missing_" + suffix
	mismatchedTable := "cdc_pk_mismatched_" + suffix
	dest := mysqldest.NewMySQLDestination()
	require.NoError(t, dest.Connect(ctx, mysqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		for _, table := range []string{freshTable, matchingTable, missingTable, mismatchedTable} {
			_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", table))
		}
	})

	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "part", DataType: schema.TypeString, MaxLength: 255},
		{Name: "payload", DataType: schema.TypeString, Nullable: true},
	}}
	prepare := func(table string) error {
		return dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:                  table,
			Schema:                 tableSchema,
			PrimaryKeys:            []string{"id", "part"},
			CDCMode:                true,
			CDCKeys:                []string{"id", "part"},
			RequirePrimaryKeyMatch: true,
		})
	}

	require.NoError(t, prepare(freshTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE `%s` (`ID` BIGINT, `part` VARCHAR(255), payload VARCHAR(255), PRIMARY KEY (`part`, `ID`))", matchingTable)))
	require.NoError(t, prepare(matchingTable))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE `%s` (id BIGINT, part VARCHAR(255), payload VARCHAR(255))", missingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("INSERT INTO `%s` VALUES (1, 'a', 'keep')", missingTable)))
	require.ErrorContains(t, prepare(missingTable), "found []")
	var payload string
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT payload FROM `%s` WHERE id = 1", missingTable)).Scan(&payload))
	require.Equal(t, "keep", payload)
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE `%s` (id BIGINT, part VARCHAR(255), payload VARCHAR(255), PRIMARY KEY (payload))", mismatchedTable)))
	require.ErrorContains(t, prepare(mismatchedTable), "found [payload]")
}

func TestMySQLDestinationCDCMergeDoesNotRegressTargetLSN(t *testing.T) {
	if mysqlDest.uri == "" {
		t.Skip("shared mysql destination container not available")
	}

	ctx := t.Context()
	targetTable := "cdc_lsn_target_" + uniqueSuffix()
	stagingTable := "cdc_lsn_staging_" + uniqueSuffix()
	auditTable := "cdc_lsn_audit_" + uniqueSuffix()
	auditTrigger := "cdc_lsn_trigger_" + uniqueSuffix()
	dest := mysqldest.NewMySQLDestination()
	require.NoError(t, dest.Connect(ctx, mysqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS `%s`", auditTrigger))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", auditTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", stagingTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", targetTable))
	})

	setup := []string{
		fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT PRIMARY KEY,
			payload VARCHAR(255),
			_cdc_lsn VARCHAR(64),
			_cdc_deleted BOOLEAN,
			_cdc_synced_at DATETIME(6)
		)`, targetTable),
		fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT,
			payload VARCHAR(255),
			_cdc_lsn VARCHAR(64),
			_cdc_deleted BOOLEAN,
			_cdc_synced_at DATETIME(6),
			_cdc_unchanged_cols JSON
		)`, stagingTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'newer-active', '00000000/00000030', 0, '2026-01-03'),
			(2, 'newer-deleted', '00000000/00000030', 1, '2026-01-03'),
			(3, 'legacy', NULL, 0, '2026-01-01'),
			(6, 'same-active', '00000000/00000010', 0, '2026-01-01'),
			(7, 'same-deleted', '00000000/00000010', 1, '2026-01-01'),
			(8, 'tie-delete', '00000000/00000010', 0, '2026-01-01'),
			(9, 'toast-newer', '00000000/00000030', 0, '2026-01-03'),
			(10, 'older-row-image', '00000000/00000010', 0, '2026-01-01')`, targetTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'stale-active', '00000000/00000020', 0, '2026-01-02', JSON_ARRAY()),
			(1, NULL, '00000000/00000025', 1, '2026-01-02', JSON_ARRAY()),
			(2, 'stale-resurrection', '00000000/00000020', 0, '2026-01-02', JSON_ARRAY()),
			(3, 'first-cdc-update', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(4, 'first-insert', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(5, 'insert-then-delete', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(5, NULL, '00000000/00000010', 1, '2026-01-02', JSON_ARRAY()),
			(6, 'same-replay', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(7, 'same-resurrection', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(8, NULL, '00000000/00000010', 1, '2026-01-02', JSON_ARRAY()),
			(9, NULL, '00000000/00000020', 0, '2026-01-02', JSON_ARRAY('payload')),
			(10, 'latest-row-image', '00000000/00000010', 0, '2026-01-02', JSON_ARRAY()),
			(10, NULL, '00000000/00000010', 1, '2026-01-02', JSON_ARRAY()),
			(11, NULL, '00000000/00000010', 1, '2026-01-02', JSON_ARRAY())`, stagingTable),
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
		1:  {"newer-active", "00000000/00000030", false, "2026-01-03"},
		2:  {"newer-deleted", "00000000/00000030", true, "2026-01-03"},
		3:  {"first-cdc-update", "00000000/00000010", false, "2026-01-02"},
		4:  {"first-insert", "00000000/00000010", false, "2026-01-02"},
		5:  {"insert-then-delete", "00000000/00000010", true, "2026-01-02"},
		6:  {"same-active", "00000000/00000010", false, "2026-01-01"},
		7:  {"same-deleted", "00000000/00000010", true, "2026-01-01"},
		8:  {"tie-delete", "00000000/00000010", true, "2026-01-02"},
		9:  {"toast-newer", "00000000/00000030", false, "2026-01-03"},
		10: {"latest-row-image", "00000000/00000010", true, "2026-01-02"},
		11: {"<null>", "00000000/00000010", true, "2026-01-02"},
	}
	for id, want := range expected {
		var payload, lsn, synced string
		var deleted bool
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COALESCE(payload, '<null>'), COALESCE(_cdc_lsn, ''), _cdc_deleted, DATE_FORMAT(_cdc_synced_at, '%%Y-%%m-%%d')
			FROM %s WHERE id = ?
		`, targetTable), id).Scan(&payload, &lsn, &deleted, &synced))
		require.Equal(t, want.payload, payload, "id %d payload", id)
		require.Equal(t, want.lsn, lsn, "id %d LSN", id)
		require.Equal(t, want.deleted, deleted, "id %d deleted", id)
		require.Equal(t, want.synced, synced, "id %d synced timestamp", id)
	}

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (target_id BIGINT)`, auditTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW INSERT INTO %s VALUES (NEW.id)`, auditTrigger, targetTable, auditTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	var replayUpdates int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, auditTable)).Scan(&replayUpdates))
	require.Zero(t, replayUpdates)
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`DROP TRIGGER %s`, auditTrigger)))

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'newest', '00000000/00000040', 0, '2026-01-04', JSON_ARRAY())`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	var payload, lsn string
	var deleted bool
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1`, targetTable)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, "newest", payload)
	require.Equal(t, "00000000/00000040", lsn)
	require.False(t, deleted)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, NULL, '00000000/00000040', 1, '2026-01-04', JSON_ARRAY())`, stagingTable)))
	require.NoError(t, dest.MergeTable(ctx, opts))
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1`, targetTable)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, "newest", payload)
	require.Equal(t, "00000000/00000040", lsn)
	require.True(t, deleted)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", stagingTable)))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, 'stale-outside-predicate', '00000000/00000020', 0, '2026-01-02', JSON_ARRAY())`, stagingTable)))
	opts.IncrementalPredicate = "target.`id` > 100"
	require.NoError(t, dest.MergeTable(ctx, opts))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = 1`, targetTable)).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload, _cdc_lsn, _cdc_deleted FROM %s WHERE id = 1`, targetTable)).Scan(&payload, &lsn, &deleted))
	require.Equal(t, "newest", payload)
	require.Equal(t, "00000000/00000040", lsn)
	require.True(t, deleted)
}

func TestMySQLDestinationCDCInternalAliasesDoNotCollide(t *testing.T) {
	if mysqlDest.uri == "" {
		t.Skip("shared mysql destination container not available")
	}

	ctx := t.Context()
	targetTable := "cdc_alias_target_" + uniqueSuffix()
	stagingTable := "cdc_alias_staging_" + uniqueSuffix()
	dest := mysqldest.NewMySQLDestination()
	require.NoError(t, dest.Connect(ctx, mysqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", stagingTable))
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS `%s`", targetTable))
	})

	userColumns := []string{
		"__BRUIN_DEDUP_RN",
		"__bruin_dedup_rn_2",
		"__INGESTR_HAS_EQUAL_LSN_DELETE",
		"__ingestr_has_equal_lsn_delete_2",
	}
	const userColumnDefinitions = "`__BRUIN_DEDUP_RN` VARCHAR(255), `__bruin_dedup_rn_2` VARCHAR(255), `__INGESTR_HAS_EQUAL_LSN_DELETE` VARCHAR(255), `__ingestr_has_equal_lsn_delete_2` VARCHAR(255)"
	const quotedUserColumns = "`__BRUIN_DEDUP_RN`, `__bruin_dedup_rn_2`, `__INGESTR_HAS_EQUAL_LSN_DELETE`, `__ingestr_has_equal_lsn_delete_2`"
	for _, statement := range []string{
		fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT PRIMARY KEY,
			payload VARCHAR(255),
			%s,
			_cdc_lsn VARCHAR(64),
			_cdc_deleted BOOLEAN,
			_cdc_synced_at DATETIME(6)
		)`, targetTable, userColumnDefinitions),
		fmt.Sprintf(`CREATE TABLE %s (
			id BIGINT,
			payload VARCHAR(255),
			%s,
			_cdc_lsn VARCHAR(64),
			_cdc_deleted BOOLEAN,
			_cdc_synced_at DATETIME(6),
			_cdc_unchanged_cols JSON
		)`, stagingTable, userColumnDefinitions),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'old', 'old-rn', 'old-rn-2', 'old-marker', 'old-marker-2', '00000000/00000020', 0, '2026-01-01')`, targetTable),
		fmt.Sprintf(`INSERT INTO %s VALUES
			(1, 'active-existing', 'existing-rn', 'existing-rn-2', 'existing-marker', 'existing-marker-2', '00000000/00000020', 0, '2026-01-02', JSON_ARRAY()),
			(1, NULL, NULL, NULL, NULL, NULL, '00000000/00000020', 1, '2026-01-03', JSON_ARRAY()),
			(2, 'active-new', 'new-rn', 'new-rn-2', 'new-marker', 'new-marker-2', '00000000/00000020', 0, '2026-01-02', JSON_ARRAY()),
			(2, NULL, NULL, NULL, NULL, NULL, '00000000/00000020', 1, '2026-01-03', JSON_ARRAY())`, stagingTable),
	} {
		require.NoError(t, dest.Exec(ctx, statement))
	}

	columns := append([]string{"id", "payload"}, userColumns...)
	columns = append(columns, destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn)
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
		var payload, rn, rn2, marker, marker2, lsn, synced string
		var deleted bool
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT payload, %s, _cdc_lsn, _cdc_deleted, DATE_FORMAT(_cdc_synced_at, '%%Y-%%m-%%d')
			FROM %s WHERE id = ?
		`, quotedUserColumns, targetTable), id).Scan(&payload, &rn, &rn2, &marker, &marker2, &lsn, &deleted, &synced))
		require.Equal(t, want, []string{payload, rn, rn2, marker, marker2}, "id %d active row image", id)
		require.Equal(t, "00000000/00000020", lsn, "id %d LSN", id)
		require.True(t, deleted, "id %d deleted", id)
		require.Equal(t, "2026-01-03", synced, "id %d synced timestamp", id)
	}
}
