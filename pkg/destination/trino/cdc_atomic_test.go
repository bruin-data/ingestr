package trino

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestClassifyTrinoSwapType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target string
		stage  string
		want   trinoSwapTypeRelation
	}{
		{"bigint", "bigint", trinoSwapTypeEqual},
		{"integer", "int", trinoSwapTypeEqual},
		{"decimal(18, 4)", "decimal(18,4)", trinoSwapTypeEqual},
		{"varchar", "varchar", trinoSwapTypeEqual},
		{"char", "char(1)", trinoSwapTypeEqual},
		{"array(bigint)", "array(bigint)", trinoSwapTypeEqual},

		{"bigint", "integer", trinoSwapTypeTargetSubsumesStage},
		{"bigint", "smallint", trinoSwapTypeTargetSubsumesStage},
		{"bigint", "tinyint", trinoSwapTypeTargetSubsumesStage},
		{"integer", "smallint", trinoSwapTypeTargetSubsumesStage},
		{"smallint", "tinyint", trinoSwapTypeTargetSubsumesStage},
		{"double", "real", trinoSwapTypeTargetSubsumesStage},
		{"decimal(38,9)", "decimal(18,4)", trinoSwapTypeTargetSubsumesStage},
		{"decimal(18,4)", "decimal(10,2)", trinoSwapTypeTargetSubsumesStage},
		{"varchar", "varchar(32)", trinoSwapTypeTargetSubsumesStage},
		{"varchar", "char(8)", trinoSwapTypeTargetSubsumesStage},
		{"varchar(64)", "varchar(32)", trinoSwapTypeTargetSubsumesStage},
		{"char(10)", "char(5)", trinoSwapTypeTargetSubsumesStage},
		{"array(bigint)", "array(integer)", trinoSwapTypeTargetSubsumesStage},
		{"array(decimal(38, 9))", "array(decimal(18, 4))", trinoSwapTypeTargetSubsumesStage},

		{"integer", "bigint", trinoSwapTypeStageWidensTarget},
		{"real", "double", trinoSwapTypeStageWidensTarget},
		{"decimal(18,4)", "decimal(38,9)", trinoSwapTypeStageWidensTarget},
		{"varchar(32)", "varchar", trinoSwapTypeStageWidensTarget},
		{"varchar(32)", "varchar(64)", trinoSwapTypeStageWidensTarget},
		{"array(integer)", "array(bigint)", trinoSwapTypeStageWidensTarget},

		{"bigint", "varchar", trinoSwapTypeIncompatible},
		{"decimal(10,4)", "decimal(10,2)", trinoSwapTypeIncompatible},
		{"char(10)", "varchar(5)", trinoSwapTypeIncompatible},
		{"timestamp(6)", "date", trinoSwapTypeIncompatible},
	}

	for _, tc := range cases {
		got := classifyTrinoSwapType(tc.target, tc.stage)
		require.Equalf(t, tc.want, got, "classifyTrinoSwapType(%q, %q)", tc.target, tc.stage)
	}
}

func TestSwapTableConditionally_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("absent target with stale staging aborts CREATE TABLE without dropping target", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-stale-stg"),
		}
		stgMarker := "77777777777777777777777777777777"
		stgInc := trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		resInc := trinoTableIncarnation("iceberg", "analytics", "orders", stgMarker)
		stgGuard := trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-stale-stg")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."analytics"`).WillReturnResult(sqlmock.NewResult(0, 0))

		expectedCreateSQL := buildCreateTableFromStagingSQL(
			`"iceberg"."analytics"."orders"`,
			`"iceberg"."_bruin_staging"."orders_stg"`,
			`"id"`,
			jsonTypeVarchar,
			trinoIncarnationCommentPrefix+stgMarker,
			stgGuard,
		)
		mock.ExpectExec(regexp.QuoteMeta(expectedCreateSQL)).
			WillReturnError(errors.New("INVALID_CAST_ARGUMENT: Cannot cast 'INGESTR_CDC_INCARNATION_CHANGED:1234' to BOOLEAN"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-stale-stg")

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  resInc,
			Schema:                        &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wider existing target types cast staging columns without ALTER COLUMN", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-wider"),
		}
		marker := "88888888888888888888888888888888"
		stgMarker := "99999999999999999999999999999999"
		targetInc := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
		stgInc := trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		combinedGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker) +
			" AND " + trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-wider")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").
				AddRow("amt", "decimal(38, 9)", "YES").
				AddRow("score", "double", "YES").
				AddRow("label", "varchar", "YES"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "integer", "YES").
				AddRow("amt", "decimal(18, 4)", "YES").
				AddRow("score", "real", "YES").
				AddRow("label", "varchar(32)", "YES"))

		expectedMergeSQL := buildConditionalSwapMergeSQL(
			`"iceberg"."analytics"."orders"`,
			`"iceberg"."_bruin_staging"."orders_stg"`,
			[]trinoSwapMergeCol{
				{name: "id", castToType: "bigint"},
				{name: "amt", castToType: "decimal(38, 9)"},
				{name: "score", castToType: "double"},
				{name: "label", castToType: "varchar"},
			},
			[]string{"id", "amt", "score", "label"},
			combinedGuard,
		)
		require.Contains(t, expectedMergeSQL, `CAST("id" AS bigint) AS "id"`)
		require.Contains(t, expectedMergeSQL, `CAST("amt" AS decimal(38, 9)) AS "amt"`)
		mock.ExpectExec(regexp.QuoteMeta(expectedMergeSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."orders_stg"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-wider")

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  targetInc,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt32},
				{Name: "amt", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
				{Name: "score", DataType: schema.TypeFloat32},
				{Name: "label", DataType: schema.TypeString, MaxLength: 32},
			}},
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing staging column in Schema fails explicitly before DDL or DML", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-miss-1", "swaptok-miss-2"),
		}
		marker := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		stgMarker := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		targetInc := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
		stgInc := trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-miss-1")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-miss-1")

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  targetInc,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "missing_col", DataType: schema.TypeString},
			}},
		})
		require.ErrorContains(t, err, "is missing expected column")

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-miss-2")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-miss-2")

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedStagingIncarnation: stgInc,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "missing_col", DataType: schema.TypeString},
			}},
		})
		require.ErrorContains(t, err, "is missing expected column")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMergeCDCTablesAtomically_Unit(t *testing.T) {
	t.Parallel()

	t.Run("single non-truncate merge delegates directly to MergeTable", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("single-tok"),
		}
		marker := "12121212121212121212121212121212"
		inc := trinoTableIncarnation("iceberg", "analytics", "t1", marker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "t1", "single-tok")
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 1))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+marker)
		expectTargetLockRelease(mock, "iceberg", "analytics", "t1", "single-tok")

		require.NoError(t, d.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{{
			Options: destination.MergeOptions{
				TargetTable:            "analytics.t1",
				StagingTable:           "_bruin_staging.t1_stg",
				PrimaryKeys:            []string{"id"},
				Columns:                []string{"id", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
				CDCHardDelete:          true,
				CDCExpectedIncarnation: inc,
			},
		}}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("phase-1 shadow merge error aborts before any live target or journal mutation", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("p1-tok-a", "p1-tok-b"),
		}
		m1 := "11111111111111111111111111111111"
		m2 := "22222222222222222222222222222222"
		inc1 := trinoTableIncarnation("iceberg", "analytics", "t1", m1)
		inc2 := trinoTableIncarnation("iceberg", "analytics", "t2", m2)

		// Canonical target keys sort lexicographically: t1 then t2.
		k1 := destination.CDCTargetKey("iceberg", "analytics", "t1")
		k2 := destination.CDCTargetKey("iceberg", "analytics", "t2")
		firstTable, secondTable := "t1", "t2"
		if strings.Compare(k1, k2) > 0 {
			firstTable, secondTable = "t2", "t1"
		}

		expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "p1-tok-a")
		expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "p1-tok-b")
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))

		// Target 0 (t1): incarnation + columns + $refs + backup + shadow + reverify.
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(101)))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 2))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(101)))

		// Target 1 (t2): incarnation + columns + $refs + backup + shadow + reverify.
		expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 2))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))

		// Shadow step 0 succeeds on shadow_0; shadow step 1 fails on shadow_1 before any live table write!
		mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).
			WillReturnError(errors.New("staging table t2_stg has invalid row"))

		// Cleanup drops both shadows and backups; releases locks in reverse order.
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "p1-tok-b")
		expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "p1-tok-a")

		err = d.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "analytics.t1",
					StagingTable:           "_bruin_staging.t1_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", destination.CDCLSNColumn},
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "analytics.t2",
					StagingTable:           "_bruin_staging.t2_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", destination.CDCLSNColumn},
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		require.ErrorContains(t, err, "staging table t2_stg has invalid row")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("mid-publish failure rolls back t1 via inline-guarded MERGE and skips externally replaced target", func(t *testing.T) {
		t.Parallel()

		for _, replacedExternally := range []bool{false, true} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)

			m1 := "33333333333333333333333333333333"
			m2 := "44444444444444444444444444444444"
			inc1 := trinoTableIncarnation("iceberg", "analytics", "t1", m1)
			inc2 := trinoTableIncarnation("iceberg", "analytics", "t2", m2)

			k1 := destination.CDCTargetKey("iceberg", "analytics", "t1")
			k2 := destination.CDCTargetKey("iceberg", "analytics", "t2")
			firstTable, secondTable := "t1", "t2"
			if strings.Compare(k1, k2) > 0 {
				firstTable, secondTable = "t2", "t1"
			}

			d := &TrinoDestination{
				db:            db,
				catalog:       "iceberg",
				schema:        "analytics",
				newLeaseToken: deterministicTokenGen("p3-tok-a", "p3-tok-b"),
				beforeAtomicPublishStep: func(step int, _ string) error {
					if step == 1 {
						return errors.New("injected mid-publish failure before t2")
					}
					return nil
				},
			}

			expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "p3-tok-a")
			expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "p3-tok-b")
			mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))

			for _, tbl := range []struct {
				name   string
				marker string
				snapID int64
			}{
				{"t1", m1, 101},
				{"t2", m2, 202},
			} {
				expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
						AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
				mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
					WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
				mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
				expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
				mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
					WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
			}

			// Phase 1 shadow merges.
			mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))

			// Phase 2 journal table creation + Phase 3 pre-publish verification across t1 & t2.
			mock.ExpectExec(`CREATE TABLE IF NOT EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 0))
			expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(101)))
			expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))

			// Step 0 records t1's journal row right before publishing t1 via inline-guarded MERGE INTO, advancing snapshot 101 -> 105.
			mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
			expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)

			// Step 1 fails via beforeAtomicPublishStep -> rollback t1!
			if replacedExternally {
				// External replacement changed t1's table comment -> rollback skips MERGE INTO!
				expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, "outside-owner-comment")
			} else {
				expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
				mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
					WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(105)))
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
						AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
						AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
				mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
			}

			mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 2))
			mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
			expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "p3-tok-b")
			expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "p3-tok-a")

			err = d.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
				{
					Options: destination.MergeOptions{
						TargetTable:            "analytics.t1",
						StagingTable:           "_bruin_staging.t1_stg",
						PrimaryKeys:            []string{"id"},
						Columns:                []string{"id", destination.CDCLSNColumn},
						CDCExpectedIncarnation: inc1,
					},
				},
				{
					Options: destination.MergeOptions{
						TargetTable:            "analytics.t2",
						StagingTable:           "_bruin_staging.t2_stg",
						PrimaryKeys:            []string{"id"},
						Columns:                []string{"id", destination.CDCLSNColumn},
						CDCExpectedIncarnation: inc2,
					},
				},
			})
			require.ErrorContains(t, err, "injected mid-publish failure before t2")
			require.NoError(t, mock.ExpectationsWereMet())
			_ = db.Close()
		}
	})

	t.Run("expired journal is recovered on entrypoint while active journal is left untouched", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		m1 := "55555555555555555555555555555555"
		m2 := "66666666666666666666666666666666"
		bakMarker := "77777777777777777777777777777777"
		inc1 := trinoTableIncarnation("iceberg", "analytics", "t1", m1)
		inc2 := trinoTableIncarnation("iceberg", "analytics", "t2", m2)

		k1 := destination.CDCTargetKey("iceberg", "analytics", "t1")
		k2 := destination.CDCTargetKey("iceberg", "analytics", "t2")
		firstTable, secondTable := "t1", "t2"
		if strings.Compare(k1, k2) > 0 {
			firstTable, secondTable = "t2", "t1"
		}

		d := &TrinoDestination{
			db:                    db,
			catalog:               "iceberg",
			schema:                "analytics",
			atomicRecoveryEnabled: true,
			newLeaseToken:         deterministicTokenGen("rec-tok-1", "rec-tok-2", "rec-tok-3", "rec-tok-4"),
		}

		journalCols := []string{
			"batch_id", "step_ord", "owner_token",
			"target_table", "target_catalog", "target_schema", "target_name",
			"expected_incarnation", "target_marker",
			"has_refs", "has_pre_snapshot", "pre_snapshot_id",
			"backup_name", "backup_marker", "shadow_name",
		}

		// 1) Active batch renewed right before lock check -> stillExpired == false -> skipped!
		mock.ExpectQuery(`SELECT DISTINCT "batch_id" FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows([]string{"batch_id"}).AddRow("active-b1"))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows(journalCols).
				AddRow("active-b1", int64(1), "own1", "analytics.t2", "iceberg", "analytics", "t2", inc2, m2, true, true, int64(202), "__ingestr_cdc_bak_b1_1", bakMarker, "__ingestr_cdc_shd_b1_1").
				AddRow("active-b1", int64(0), "own1", "analytics.t1", "iceberg", "analytics", "t1", inc1, m1, true, true, int64(101), "__ingestr_cdc_bak_b1_0", bakMarker, "__ingestr_cdc_shd_b1_0"))
		expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "rec-tok-1")
		expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "rec-tok-2")
		mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(MAX\("expires_at_ms"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt", "expired"}).AddRow(int64(2), false))
		expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "rec-tok-2")
		expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "rec-tok-1")
		mock.ExpectQuery(`SELECT table_name FROM "iceberg".information_schema.tables`).
			WillReturnRows(sqlmock.NewRows([]string{"table_name"}))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}))

		gotSchema, err := d.GetTableSchema(t.Context(), "analytics.t1")
		require.NoError(t, err)
		require.Nil(t, gotSchema)

		// 2) Expired crashed batch -> t2 is still at pre_snapshot_id (202 -> skipped), t1 advanced to 109 (restored via MERGE INTO)!
		mock.ExpectQuery(`SELECT DISTINCT "batch_id" FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows([]string{"batch_id"}).AddRow("crashed-b2"))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows(journalCols).
				AddRow("crashed-b2", int64(1), "own2", "analytics.t2", "iceberg", "analytics", "t2", inc2, m2, true, true, int64(202), "__ingestr_cdc_bak_b2_1", bakMarker, "__ingestr_cdc_shd_b2_1").
				AddRow("crashed-b2", int64(0), "own2", "analytics.t1", "iceberg", "analytics", "t1", inc1, m1, true, true, int64(101), "__ingestr_cdc_bak_b2_0", bakMarker, "__ingestr_cdc_shd_b2_0"))
		expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "rec-tok-3")
		expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "rec-tok-4")
		mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(MAX\("expires_at_ms"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt", "expired"}).AddRow(int64(2), true))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows(journalCols).
				AddRow("crashed-b2", int64(1), "own2", "analytics.t2", "iceberg", "analytics", "t2", inc2, m2, true, true, int64(202), "__ingestr_cdc_bak_b2_1", bakMarker, "__ingestr_cdc_shd_b2_1").
				AddRow("crashed-b2", int64(0), "own2", "analytics.t1", "iceberg", "analytics", "t1", inc1, m1, true, true, int64(101), "__ingestr_cdc_bak_b2_0", bakMarker, "__ingestr_cdc_shd_b2_0"))

		// step_ord 1 (t2): still at pre_snapshot_id 202 -> skipped!
		expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))

		// step_ord 0 (t1): at post-crash snapshot 109 -> restored from __ingestr_cdc_bak_b2_0 via MERGE INTO!
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(109)))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
		mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))

		// Journal deleted + backup/shadow tables dropped + locks released + orphan sweep + original CDCTargetIncarnation proceeds!
		mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_b2_1"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_b2_1"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_b2_0"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_b2_0"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "rec-tok-4")
		expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "rec-tok-3")
		mock.ExpectQuery(`SELECT table_name FROM "iceberg".information_schema.tables`).
			WillReturnRows(sqlmock.NewRows([]string{"table_name"}))

		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		gotInc, exists, err := d.CDCTargetIncarnation(t.Context(), "analytics.t1")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, inc1, gotInc)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit-point journal delete failure rolls back published targets and retains backups when delete stays broken", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		m1 := "81818181818181818181818181818181"
		m2 := "82828282828282828282828282828282"
		inc1 := trinoTableIncarnation("iceberg", "analytics", "t1", m1)
		inc2 := trinoTableIncarnation("iceberg", "analytics", "t2", m2)

		k1 := destination.CDCTargetKey("iceberg", "analytics", "t1")
		k2 := destination.CDCTargetKey("iceberg", "analytics", "t2")
		firstTable, secondTable := "t1", "t2"
		if strings.Compare(k1, k2) > 0 {
			firstTable, secondTable = "t2", "t1"
		}

		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("cp-tok-a", "cp-tok-b"),
		}

		expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "cp-tok-a")
		expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "cp-tok-b")
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))

		for _, tbl := range []struct {
			name   string
			marker string
			snapID int64
		}{
			{"t1", m1, 101},
			{"t2", m2, 202},
		} {
			expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
			mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
				WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
					AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
			mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
			expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
		}

		mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`CREATE TABLE IF NOT EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 0))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(101)))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
		mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))

		// Both steps record their journal row and publish cleanly.
		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)

		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t2" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
		expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)

		// Commit-point DELETE FROM journal fails -> MUST NOT return nil; enters rollback across t2 then t1!
		mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnError(errors.New("ACCESS_DENIED: cannot delete journal at commit"))

		for _, rb := range []struct {
			name   string
			marker string
			snapID int64
		}{
			{"t2", m2, 209},
			{"t1", m1, 109},
		} {
			expectIncarnationLookup(mock, "iceberg", "analytics", rb.name, true, trinoIncarnationCommentPrefix+rb.marker)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + rb.name + `\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(rb.snapID))
			mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
				WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
					AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
			mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
				WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
					AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
			mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."` + rb.name + `" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
		}

		// Post-rollback journal delete ALSO fails -> backups are retained (no DROP bak_*) and journal is expired!
		mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnError(errors.New("ACCESS_DENIED: still cannot delete journal"))
		mock.ExpectExec(`UPDATE "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal" SET "expires_at_ms" = BIGINT '0'`).
			WillReturnResult(sqlmock.NewResult(0, 2))

		// Only shadow tables are dropped; backup tables are retained for subsequent journal recovery!
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "cp-tok-b")
		expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "cp-tok-a")

		err = d.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{
				Options: destination.MergeOptions{
					TargetTable:            "analytics.t1",
					StagingTable:           "_bruin_staging.t1_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", destination.CDCLSNColumn},
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "analytics.t2",
					StagingTable:           "_bruin_staging.t2_stg",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", destination.CDCLSNColumn},
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		require.ErrorContains(t, err, "cannot delete journal at commit")
		require.ErrorContains(t, err, "still cannot delete journal")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("external INSERT snapshot change on unfenced t2 aborts publish without rolling back t2 and cleans unmutated step row if t1 rollback fails", func(t *testing.T) {
		t.Parallel()

		for _, rollbackSucceeds := range []bool{true, false} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)

			m1 := "91919191919191919191919191919191"
			m2 := "92929292929292929292929292929292"

			k1 := destination.CDCTargetKey("iceberg", "analytics", "t1")
			k2 := destination.CDCTargetKey("iceberg", "analytics", "t2")
			firstTable, secondTable := "t1", "t2"
			if strings.Compare(k1, k2) > 0 {
				firstTable, secondTable = "t2", "t1"
			}

			d := &TrinoDestination{
				db:            db,
				catalog:       "iceberg",
				schema:        "analytics",
				newLeaseToken: deterministicTokenGen("ext-tok-a", "ext-tok-b"),
			}

			expectTargetLockAcquire(mock, "iceberg", "analytics", firstTable, "ext-tok-a")
			expectTargetLockAcquire(mock, "iceberg", "analytics", secondTable, "ext-tok-b")
			mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))

			// Unfenced call (CDCExpectedIncarnation == "") still reads trinoTargetMetadata and records targetMarker!
			for _, tbl := range []struct {
				name   string
				marker string
				snapID int64
			}{
				{"t1", m1, 101},
				{"t2", m2, 202},
			} {
				expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
						AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
				mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
					WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
				mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
				expectIncarnationLookup(mock, "iceberg", "analytics", tbl.name, true, trinoIncarnationCommentPrefix+tbl.marker)
				mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."` + tbl.name + `\$refs"`).
					WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(tbl.snapID))
			}

			mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`MERGE INTO "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 1))

			mock.ExpectExec(`CREATE TABLE IF NOT EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 0))
			expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(101)))
			expectIncarnationLookup(mock, "iceberg", "analytics", "t2", true, trinoIncarnationCommentPrefix+m2)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t2\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(202)))

			// Step 0 publishes t1.
			mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))

			// Step 1 inserts t2's journal row, then publishSQL fails with guard sentinel because external INSERT advanced t2's snapshot_id!
			mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t2" AS t`).
				WillReturnError(errors.New("INVALID_CAST_ARGUMENT: Cannot cast 'INGESTR_CDC_INCARNATION_CHANGED:5555' to BOOLEAN"))

			// Rollback MUST ONLY run on t1 (never t2!) so the external INSERT on t2 is preserved!
			expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
			mock.ExpectQuery(`SELECT snapshot_id FROM "iceberg"."analytics"."t1\$refs"`).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(109)))
			mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
				WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
					AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))
			mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
				WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
					AddRow("id", "bigint", "YES").AddRow("_cdc_lsn", "varchar", "YES"))

			if rollbackSucceeds {
				mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 2))
				mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 2))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_bak_`).WillReturnResult(sqlmock.NewResult(0, 0))
			} else {
				mock.ExpectExec(`MERGE INTO "iceberg"."analytics"."t1" AS t`).
					WillReturnError(errors.New("simulated t1 rollback failure"))
				expectIncarnationLookup(mock, "iceberg", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
				// Unmutated step_ord 1 (t2) is deleted from journal so crash recovery will NEVER roll back t2's external INSERT!
				mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal" WHERE "batch_id" = .* AND "step_ord" = BIGINT '1'`).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`UPDATE "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal" SET "expires_at_ms" = BIGINT '0'`).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."__ingestr_cdc_shd_`).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			expectTargetLockRelease(mock, "iceberg", "analytics", secondTable, "ext-tok-b")
			expectTargetLockRelease(mock, "iceberg", "analytics", firstTable, "ext-tok-a")

			err = d.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
				{
					Options: destination.MergeOptions{
						TargetTable:  "analytics.t1",
						StagingTable: "_bruin_staging.t1_stg",
						PrimaryKeys:  []string{"id"},
						Columns:      []string{"id", destination.CDCLSNColumn},
					},
				},
				{
					Options: destination.MergeOptions{
						TargetTable:  "analytics.t2",
						StagingTable: "_bruin_staging.t2_stg",
						PrimaryKeys:  []string{"id"},
						Columns:      []string{"id", destination.CDCLSNColumn},
					},
				},
			})
			require.ErrorContains(t, err, "physical incarnation changed")
			if !rollbackSucceeds {
				require.ErrorContains(t, err, "simulated t1 rollback failure")
			}
			require.NoError(t, mock.ExpectationsWereMet())
			_ = db.Close()
		}
	})

	t.Run("non-default batchCatalog hint recovers expired batch in hinted catalog", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		m1 := "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
		bakMarker := "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1"
		inc1 := trinoTableIncarnation("lakehouse", "analytics", "t1", m1)

		d := &TrinoDestination{
			db:                    db,
			catalog:               "iceberg",
			schema:                "analytics",
			atomicRecoveryEnabled: true,
			newLeaseToken:         deterministicTokenGen("cat-tok-1"),
		}

		journalCols := []string{
			"batch_id", "step_ord", "owner_token",
			"target_table", "target_catalog", "target_schema", "target_name",
			"expected_incarnation", "target_marker",
			"has_refs", "has_pre_snapshot", "pre_snapshot_id",
			"backup_name", "backup_marker", "shadow_name",
		}

		// 1) Default catalog ("iceberg"): no journal table, no staging tables.
		mock.ExpectQuery(`SELECT DISTINCT "batch_id" FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnError(errors.New("TABLE_NOT_FOUND: Table 'iceberg._bruin_staging.__ingestr_cdc_batch_journal' does not exist"))
		mock.ExpectQuery(`SELECT table_name FROM "iceberg".information_schema.tables`).
			WillReturnRows(sqlmock.NewRows([]string{"table_name"}))

		// 2) Hinted catalog ("lakehouse"): recovers expired batch using "lakehouse" throughout!
		mock.ExpectQuery(`SELECT DISTINCT "batch_id" FROM "lakehouse"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows([]string{"batch_id"}).AddRow("othercat-b1"))
		mock.ExpectQuery(`FROM "lakehouse"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows(journalCols).
				AddRow("othercat-b1", int64(0), "own-cat", "lakehouse.analytics.t1", "lakehouse", "analytics", "t1", inc1, m1, true, true, int64(301), "__ingestr_cdc_bak_other_0", bakMarker, "__ingestr_cdc_shd_other_0"))
		expectTargetLockAcquire(mock, "lakehouse", "analytics", "t1", "cat-tok-1")
		mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(MAX\("expires_at_ms"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt", "expired"}).AddRow(int64(1), true))
		mock.ExpectQuery(`FROM "lakehouse"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows(journalCols).
				AddRow("othercat-b1", int64(0), "own-cat", "lakehouse.analytics.t1", "lakehouse", "analytics", "t1", inc1, m1, true, true, int64(301), "__ingestr_cdc_bak_other_0", bakMarker, "__ingestr_cdc_shd_other_0"))

		expectIncarnationLookup(mock, "lakehouse", "analytics", "t1", true, trinoIncarnationCommentPrefix+m1)
		mock.ExpectQuery(`SELECT snapshot_id FROM "lakehouse"."analytics"."t1\$refs"`).
			WillReturnRows(sqlmock.NewRows([]string{"snapshot_id"}).AddRow(int64(309)))
		mock.ExpectQuery(`FROM "lakehouse".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		mock.ExpectQuery(`FROM "lakehouse".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		mock.ExpectExec(`MERGE INTO "lakehouse"."analytics"."t1" AS t`).WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`DELETE FROM "lakehouse"."_bruin_staging"."__ingestr_cdc_batch_journal"`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "lakehouse"."_bruin_staging"."__ingestr_cdc_bak_other_0"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "lakehouse"."_bruin_staging"."__ingestr_cdc_shd_other_0"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "lakehouse", "analytics", "t1", "cat-tok-1")
		mock.ExpectQuery(`SELECT table_name FROM "lakehouse".information_schema.tables`).
			WillReturnRows(sqlmock.NewRows([]string{"table_name"}))

		mock.ExpectQuery(`FROM "lakehouse".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}))

		gotSchema, err := d.GetTableSchema(t.Context(), "lakehouse.analytics.t1")
		require.NoError(t, err)
		require.Nil(t, gotSchema)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("stale Phase-1 orphan bak and shd tables are swept while fresh and journal-referenced tables are kept", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		d := &TrinoDestination{
			db:                    db,
			catalog:               "iceberg",
			schema:                "analytics",
			atomicRecoveryEnabled: true,
			atomicOrphanMaxAge:    2 * time.Minute,
		}

		oldMS := time.Now().Add(-10 * time.Minute).UnixMilli()
		freshMS := time.Now().UnixMilli()
		orphanBak := formatTrinoAtomicStagingTableName("bak", oldMS, "orphanb", 0)
		orphanShd := formatTrinoAtomicStagingTableName("shd", oldMS, "orphanb", 0)
		referencedBak := formatTrinoAtomicStagingTableName("bak", oldMS, "activeb", 0)
		freshBak := formatTrinoAtomicStagingTableName("bak", freshMS, "freshb", 0)

		mock.ExpectQuery(`SELECT DISTINCT "batch_id" FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows([]string{"batch_id"}))
		mock.ExpectQuery(`SELECT table_name FROM "iceberg".information_schema.tables`).
			WillReturnRows(sqlmock.NewRows([]string{"table_name"}).
				AddRow(orphanBak).
				AddRow(orphanShd).
				AddRow(referencedBak).
				AddRow(freshBak).
				AddRow("__ingestr_cdc_bak_legacy_no_ts"))
		mock.ExpectQuery(`SELECT DISTINCT lower\("batch_id"\) FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).
			WillReturnRows(sqlmock.NewRows([]string{"batch_id"}).AddRow("activeb"))

		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging".` + quoteIdentifier(orphanBak))).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging".` + quoteIdentifier(orphanShd))).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}))

		gotSchema, err := d.GetTableSchema(t.Context(), "analytics.t1")
		require.NoError(t, err)
		require.Nil(t, gotSchema)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestParseTrinoAtomicStagingTableName(t *testing.T) {
	t.Parallel()

	bak := formatTrinoAtomicStagingTableName("bak", 1700000000123, "AbCdEf01", 2)
	ms, batchID, ok := parseTrinoAtomicStagingTableName(bak)
	require.True(t, ok)
	require.Equal(t, int64(1700000000123), ms)
	require.Equal(t, "abcdef01", batchID)

	shd := formatTrinoAtomicStagingTableName("shd", 1700000000999, "11223344", 0)
	ms, batchID, ok = parseTrinoAtomicStagingTableName(shd)
	require.True(t, ok)
	require.Equal(t, int64(1700000000999), ms)
	require.Equal(t, "11223344", batchID)

	for _, invalid := range []string{
		"",
		"__ingestr_cdc_batch_journal",
		"__ingestr_cdc_bak_legacy_0",
		"__ingestr_cdc_bak_notanumber_abc_0",
		"__ingestr_cdc_bak_0_abc_0",
		"__ingestr_cdc_bak_1700000000123__0",
		"__ingestr_cdc_bak_1700000000123_abc_notidx",
	} {
		_, _, ok := parseTrinoAtomicStagingTableName(invalid)
		require.Falsef(t, ok, "expected %q to be rejected", invalid)
	}
}
