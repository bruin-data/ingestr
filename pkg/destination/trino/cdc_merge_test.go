package trino

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestTrinoCDCCapabilities(t *testing.T) {
	t.Parallel()

	d := NewTrinoDestination()
	require.True(t, d.SupportsAtomicSwap())
	require.True(t, d.SupportsCDCMerge())
	require.True(t, d.SupportsCDCHardDelete())
	require.True(t, d.SupportsCDCUnchangedCols())
	require.True(t, d.SupportsCDCConditionalMerge())
	require.True(t, d.SupportsCDCConditionalSwap())
	require.True(t, d.RequiresSerializedCDCRuns())
}

func TestBuildCDCMergeSQL_SoftDelete(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "iceberg", schema: "analytics"}
	got := d.buildCDCMergeSQL(destination.MergeOptions{
		TargetTable:  "orders",
		StagingTable: "_bruin_staging.orders_stage",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
	})

	require.NotContains(t, got, "QUALIFY")
	require.NotContains(t, got, "_cdc_unchanged_cols")
	require.Contains(t, got, `MERGE INTO "iceberg"."analytics"."orders" AS t`)
	require.Contains(t, got, `ORDER BY "_cdc_lsn" DESC, "_cdc_deleted" DESC`)
	require.Contains(t, got, `WHERE "_cdc_deleted" = false`)
	require.Contains(t, got, `ON la."id" IS NOT DISTINCT FROM act."id"`)
	require.Contains(t, got, `act."_cdc_lsn" IS NOT NULL AS "__ingestr_has_active"`)
	require.Contains(t, got, `act."_cdc_lsn" AS "__ingestr_active_lsn"`)
	require.Contains(t, got, `ON t."id" = s."id"`)
	require.Contains(t, got, `WHEN MATCHED AND (t."_cdc_lsn" IS NULL OR s."_cdc_lsn" > t."_cdc_lsn" OR (s."_cdc_lsn" = t."_cdc_lsn" AND s."_cdc_deleted" = true AND COALESCE(t."_cdc_deleted", false) = false)) AND (s."_cdc_deleted" = false OR (s."__ingestr_has_active" AND (t."_cdc_lsn" IS NULL OR s."__ingestr_active_lsn" >= t."_cdc_lsn"))) THEN`)
	require.Contains(t, got, `UPDATE SET "name" = s."name", "_cdc_lsn" = s."_cdc_lsn", "_cdc_deleted" = s."_cdc_deleted", "_cdc_synced_at" = s."_cdc_synced_at"`)
	require.Contains(t, got, "WHEN MATCHED AND (t.\"_cdc_lsn\" IS NULL OR s.\"_cdc_lsn\" > t.\"_cdc_lsn\" OR (s.\"_cdc_lsn\" = t.\"_cdc_lsn\" AND s.\"_cdc_deleted\" = true AND COALESCE(t.\"_cdc_deleted\", false) = false)) AND s.\"_cdc_deleted\" = true THEN\n  UPDATE SET \"_cdc_deleted\" = true, \"_cdc_lsn\" = s.\"_cdc_lsn\", \"_cdc_synced_at\" = s.\"_cdc_synced_at\"")
	require.Contains(t, got, `WHEN NOT MATCHED THEN`+"\n"+`  INSERT ("id", "name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at") VALUES (s."id", s."name", s."_cdc_lsn", s."_cdc_deleted", s."_cdc_synced_at")`)
}

func TestBuildCDCMergeSQL_SoftDeleteWithUnchangedColsAndCaseSensitivity(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "iceberg", schema: "analytics"}
	got := d.buildCDCMergeSQL(destination.MergeOptions{
		TargetTable:          "orders",
		StagingTable:         "_bruin_staging.orders_stage",
		PrimaryKeys:          []string{"id"},
		Columns:              []string{"id", "CONFIG_DATA", "o'name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"},
		IncrementalPredicate: `t."id" > 0`,
	})

	require.Contains(t, got, `WITH __ingestr_changes AS`)
	require.Contains(t, got, `tgt."_cdc_lsn" IS NULL OR stg."_cdc_lsn" > tgt."_cdc_lsn"`)
	require.Contains(t, got, `JOIN __ingestr_v_1`)
	require.Contains(t, got, `TRY(CAST(json_parse(lower(COALESCE(NULLIF(stg."_cdc_unchanged_cols", ''), '[]'))) AS ARRAY(VARCHAR))) AS "__ingestr_unchanged_arr"`)
	require.Contains(t, got, `NOT COALESCE(contains("__ingestr_unchanged_arr", 'config_data'), false)`)
	require.Contains(t, got, `NOT COALESCE(contains("__ingestr_unchanged_arr", 'o''name'), false)`)
	require.Contains(t, got, `"CONFIG_DATA" = CASE WHEN s."__ingestr_mat_1" THEN s."CONFIG_DATA" ELSE t."CONFIG_DATA" END`)
	require.Contains(t, got, `"o'name" = CASE WHEN s."__ingestr_mat_2" THEN s."o'name" ELSE t."o'name" END`)
	require.Contains(t, got, `WHEN MATCHED AND (t."id" > 0) AND (t."_cdc_lsn" IS NULL`)
	require.NotContains(t, got, `INSERT ("id", "CONFIG_DATA", "o'name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols")`)
	require.Contains(t, got, `INSERT ("id", "CONFIG_DATA", "o'name", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at")`)
}

func TestBuildCDCMergeSQL_HardDelete(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "iceberg", schema: "analytics"}
	got := d.buildCDCMergeSQL(destination.MergeOptions{
		TargetTable:          "orders",
		StagingTable:         "_bruin_staging.orders_stage",
		PrimaryKeys:          []string{"tenant", "id"},
		Columns:              []string{"tenant", "id", "payload", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"},
		IncrementalPredicate: `t."tenant" = 'acme'`,
		CDCHardDelete:        true,
	})

	require.NotContains(t, got, "QUALIFY")
	require.NotContains(t, got, "__ingestr_has_active")
	require.Contains(t, got, `PARTITION BY "tenant", "id" ORDER BY "_cdc_lsn" DESC, "_cdc_deleted" DESC`)
	require.Contains(t, got, `ON t."tenant" = s."tenant" AND t."id" = s."id"`)
	require.Contains(t, got, "WHEN MATCHED AND (t.\"tenant\" = 'acme') AND s.\"_cdc_deleted\" = true AND (t.\"_cdc_lsn\" IS NULL OR s.\"_cdc_lsn\" >= t.\"_cdc_lsn\") THEN\n  DELETE")
	require.Contains(t, got, `NOT COALESCE(contains("__ingestr_unchanged_arr", 'payload'), false)`)
	require.Contains(t, got, "WHEN MATCHED AND (t.\"tenant\" = 'acme') AND s.\"_cdc_deleted\" = false AND (t.\"_cdc_lsn\" IS NULL OR s.\"_cdc_lsn\" > t.\"_cdc_lsn\") THEN\n  UPDATE SET \"payload\" = CASE WHEN s.\"__ingestr_mat_2\" THEN s.\"payload\" ELSE t.\"payload\" END, \"_cdc_lsn\" = s.\"_cdc_lsn\", \"_cdc_deleted\" = s.\"_cdc_deleted\", \"_cdc_synced_at\" = s.\"_cdc_synced_at\"")
	require.Contains(t, got, "WHEN NOT MATCHED AND s.\"_cdc_deleted\" = false THEN\n  INSERT (\"tenant\", \"id\", \"payload\", \"_cdc_lsn\", \"_cdc_deleted\", \"_cdc_synced_at\") VALUES (s.\"tenant\", s.\"id\", s.\"payload\", s.\"_cdc_lsn\", s.\"_cdc_deleted\", s.\"_cdc_synced_at\")")
}

func TestBuildCDCMergeSQL_AvoidsSyntheticColumnCollision(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "iceberg", schema: "analytics"}
	got := d.buildCDCMergeSQL(destination.MergeOptions{
		TargetTable:  "orders",
		StagingTable: "_bruin_staging.orders_stage",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "__ingestr_cdc_rn", "__ingestr_has_active", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
	})

	require.Contains(t, got, `AS "__ingestr_cdc_rn_"`)
	require.Contains(t, got, `AS "__ingestr_has_active_"`)
}

func TestMergeCDC_ExecutesWithIncarnationFencing(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &TrinoDestination{
		db:            db,
		catalog:       "iceberg",
		schema:        "analytics",
		newLeaseToken: deterministicTokenGen("locktok01"),
	}
	marker := "0123456789abcdef0123456789abcdef"
	expectedIncarnation := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
	expectedGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker)

	expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "locktok01")
	expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
	mock.ExpectExec("MERGE INTO .* " + regexp.QuoteMeta(expectedGuard)).WillReturnResult(sqlmock.NewResult(0, 3))
	expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
	expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "locktok01")

	err = d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:            "orders",
		StagingTable:           "_bruin_staging.orders_stage",
		PrimaryKeys:            []string{"id"},
		Columns:                []string{"id", "val", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		CDCExpectedIncarnation: expectedIncarnation,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildCDCMergeSQLWithGuard_AllBranches(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "iceberg", schema: "analytics"}
	marker := "0123456789abcdef0123456789abcdef"
	guard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker)

	softSQL := d.buildCDCMergeSQLWithGuard(destination.MergeOptions{
		TargetTable:  "orders",
		StagingTable: "_bruin_staging.orders_stage",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "val", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
	}, guard)
	require.Contains(t, softSQL, "WHERE "+guard+") AS s")

	hardSQL := d.buildCDCMergeSQLWithGuard(destination.MergeOptions{
		TargetTable:   "orders",
		StagingTable:  "_bruin_staging.orders_stage",
		PrimaryKeys:   []string{"id"},
		Columns:       []string{"id", "val", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		CDCHardDelete: true,
	}, guard)
	require.Contains(t, hardSQL, `WHERE "__ingestr_cdc_rn" = 1 AND `+guard+`) AS s`)

	for _, hardDelete := range []bool{false, true} {
		toastSQL := d.buildCDCMergeSQLWithGuard(destination.MergeOptions{
			TargetTable:   "orders",
			StagingTable:  "_bruin_staging.orders_stage",
			PrimaryKeys:   []string{"id"},
			Columns:       []string{"id", "payload", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at", "_cdc_unchanged_cols"},
			CDCHardDelete: hardDelete,
		}, guard)
		require.Contains(t, toastSQL, "WHERE "+guard+") AS s")
	}
}

func TestMergeTable_FencedNonCDC(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	d := &TrinoDestination{
		db:            db,
		catalog:       "iceberg",
		schema:        "analytics",
		newLeaseToken: deterministicTokenGen("locktok-noncdc"),
	}
	marker := "fedcba9876543210fedcba9876543210"
	expectedIncarnation := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
	expectedGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker)

	expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "locktok-noncdc")
	expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
	mock.ExpectExec("MERGE INTO .* " + regexp.QuoteMeta("WHERE __bruin_dedup_rn = 1 AND "+expectedGuard)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
	expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "locktok-noncdc")

	err = d.MergeTable(t.Context(), destination.MergeOptions{
		TargetTable:            "orders",
		StagingTable:           "_bruin_staging.orders_stage",
		PrimaryKeys:            []string{"id"},
		Columns:                []string{"id", "val", "_cdc_lsn"},
		CDCExpectedIncarnation: expectedIncarnation,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMergeCDC_Errors(t *testing.T) {
	t.Parallel()

	t.Run("missing primary keys", func(t *testing.T) {
		t.Parallel()
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		err = d.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:  "orders",
			StagingTable: "stage",
			Columns:      []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		})
		require.ErrorContains(t, err, "primary keys")
	})

	t.Run("lease lost", func(t *testing.T) {
		t.Parallel()
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		ctx := source.WithConnectorLeaseGuard(t.Context(), source.NewConnectorLeaseGuard(&lostTestLease{}))
		err = d.MergeTable(ctx, destination.MergeOptions{
			TargetTable:  "orders",
			StagingTable: "stage",
			PrimaryKeys:  []string{"id"},
			Columns:      []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
		})
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	})

	t.Run("incarnation changed before merge", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("locktok-pre"),
		}

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "locktok-pre")
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "locktok-pre")

		err = d.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "orders",
			StagingTable:           "stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCExpectedIncarnation: trinoTableIncarnation("iceberg", "analytics", "orders", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("incarnation changed after merge", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("locktok-post"),
		}

		marker := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "locktok-post")
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec("MERGE INTO").WillReturnResult(sqlmock.NewResult(0, 1))
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+"cccccccccccccccccccccccccccccccc")
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "locktok-post")

		err = d.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "orders",
			StagingTable:           "stage",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCExpectedIncarnation: trinoTableIncarnation("iceberg", "analytics", "orders", marker),
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func deterministicTokenGen(tokens ...string) func() (string, error) {
	idx := 0
	return func() (string, error) {
		if idx >= len(tokens) {
			return "", errors.New("unexpected lease token request")
		}
		tok := tokens[idx]
		idx++
		return tok, nil
	}
}

func targetLockTableName(catalog, schemaName, tableName string) string {
	key := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName))
	return "cdc_tlock_" + destination.CDCTargetKeyDigest(key)
}

func expectTargetLockAcquire(mock sqlmock.Sqlmock, catalog, schemaName, tableName, token string) {
	lockName := targetLockTableName(catalog, schemaName, tableName)
	lockFQN := qualifyTrinoTable(catalog, trinoCDCLeaseSchema, lockName)
	mock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "` + catalog + `"."` + trinoCDCLeaseSchema + `"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE ` + lockFQN + ` AS SELECT CAST('` + token + `' AS VARCHAR) AS "owner", CURRENT_TIMESTAMP AS "heartbeat"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM ` + lockFQN)).
		WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow(token))
}

func expectTargetLockRelease(mock sqlmock.Sqlmock, catalog, schemaName, tableName, token string) {
	lockName := targetLockTableName(catalog, schemaName, tableName)
	lockFQN := qualifyTrinoTable(catalog, trinoCDCLeaseSchema, lockName)
	releasedFQN := qualifyTrinoTable(catalog, trinoCDCLeaseSchema, lockName+"_released_"+token)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM ` + lockFQN)).
		WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow(token))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE ` + lockFQN + ` RENAME TO ` + releasedFQN)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT MAX("owner") FROM ` + releasedFQN)).
		WillReturnRows(sqlmock.NewRows([]string{"owner"}).AddRow(token))
	mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS ` + releasedFQN)).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectIncarnationLookup(mock sqlmock.Sqlmock, catalog, schemaName, tableName string, hasLSN bool, comment string) {
	expectIncarnationLookupWithComments(mock, catalog, schemaName, tableName, hasLSN, comment, comment)
}

func expectIncarnationLookupWithComments(mock sqlmock.Sqlmock, catalog, schemaName, tableName string, hasLSN bool, tableComment, lsnComment string) {
	var tableCommentVal any
	if tableComment != "" {
		tableCommentVal = tableComment
	}
	tableRows := sqlmock.NewRows([]string{"table_catalog", "table_schema", "table_name", "comment"}).
		AddRow(catalog, schemaName, tableName, tableCommentVal)
	mock.ExpectQuery(`FROM "` + catalog + `".information_schema.tables`).WillReturnRows(tableRows)

	colRows := sqlmock.NewRows([]string{"Column", "Type", "Extra", "Comment"}).
		AddRow("id", "bigint", "", nil)
	if hasLSN {
		var commentVal any
		if lsnComment != "" {
			commentVal = lsnComment
		}
		colRows.AddRow(destination.CDCLSNColumn, "varchar", "", commentVal)
	}
	mock.ExpectQuery(`SHOW COLUMNS FROM "` + catalog + `"."` + schemaName + `"."` + tableName + `"`).WillReturnRows(colRows)
}

type lostTestLease struct{}

func (l *lostTestLease) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (l *lostTestLease) Err() error {
	return errors.Join(source.ErrConnectorLeaseLost, errors.New("lost"))
}

func (l *lostTestLease) Release() error { return nil }
