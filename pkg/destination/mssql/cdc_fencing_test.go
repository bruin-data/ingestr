package mssql

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
)

var (
	_ destination.Destination                           = (*MSSQLDestination)(nil)
	_ destination.CDCResumeProvider                     = (*MSSQLDestination)(nil)
	_ destination.CDCStateReader                        = (*MSSQLDestination)(nil)
	_ destination.CDCStateFenceReader                   = (*MSSQLDestination)(nil)
	_ destination.CDCStatePruner                        = (*MSSQLDestination)(nil)
	_ destination.CDCStatePositionMigrator              = (*MSSQLDestination)(nil)
	_ destination.CDCTargetClaimer                      = (*MSSQLDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer            = (*MSSQLDestination)(nil)
	_ destination.CDCTargetIdentityProvider             = (*MSSQLDestination)(nil)
	_ destination.CDCTargetIncarnationProvider          = (*MSSQLDestination)(nil)
	_ destination.ManagedCDCStateValidator              = (*MSSQLDestination)(nil)
	_ destination.ManagedCDCTargetValidator             = (*MSSQLDestination)(nil)
	_ destination.ManagedCDCRunLeaser                   = (*MSSQLDestination)(nil)
	_ destination.CDCMergeAware                         = (*MSSQLDestination)(nil)
	_ destination.CDCHardDeleteCapable                  = (*MSSQLDestination)(nil)
	_ destination.CDCUnchangedColsAware                 = (*MSSQLDestination)(nil)
	_ destination.CDCConditionalMergeCapable            = (*MSSQLDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger             = (*MSSQLDestination)(nil)
	_ destination.CDCConditionalTruncater               = (*MSSQLDestination)(nil)
	_ destination.CDCTruncateCapable                    = (*MSSQLDestination)(nil)
	_ destination.TruncateCapable                       = (*MSSQLDestination)(nil)
	_ destination.CDCConditionalSwapCapable             = (*MSSQLDestination)(nil)
	_ destination.CDCConditionalSwapPlanner             = (*MSSQLDestination)(nil)
	_ destination.CDCConditionalSchemaEvolver           = (*MSSQLDestination)(nil)
	_ destination.SchemaEvolutionSourceColumnNormalizer = (*MSSQLDestination)(nil)
	_ destination.DecimalPrecisionLimiter               = (*MSSQLDestination)(nil)
)

func mssqlTestIncarnation(server, database string, objectID int64, createdAt string) string {
	return destination.CDCTargetKey(server, database, fmt.Sprintf("%d", objectID), createdAt)
}

func TestAcquireManagedCDCRunLeaseAcquireAndRelease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
	if _, err := dest.AcquireManagedCDCRunLease(t.Context(), ""); err == nil {
		t.Fatal("expected empty connector ID error")
	}

	resource := mssqlCDCRunLeaseResource("conn-1")
	mock.ExpectQuery(regexp.QuoteMeta(mssqlAcquireAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(mssqlReleaseAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(0))

	lease, err := dest.AcquireManagedCDCRunLease(t.Context(), "conn-1")
	if err != nil {
		t.Fatalf("AcquireManagedCDCRunLease failed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("second Release failed: %v", err)
	}
	select {
	case <-lease.Done():
	case <-time.After(time.Second):
		t.Fatal("lease Done channel not closed after Release")
	}
	if lease.Err() != nil {
		t.Fatalf("expected nil lease Err after clean Release, got %v", lease.Err())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireManagedCDCRunLeaseRejectsContentionAndErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
	resource := mssqlCDCRunLeaseResource("conn-busy")
	mock.ExpectQuery(regexp.QuoteMeta(mssqlAcquireAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(-1))
	mock.ExpectQuery(regexp.QuoteMeta(mssqlAcquireAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(-999))

	_, err = dest.AcquireManagedCDCRunLease(t.Context(), "conn-busy")
	if err == nil || !strings.Contains(err.Error(), "already owns connector") {
		t.Fatalf("expected contention error, got %v", err)
	}
	_, err = dest.AcquireManagedCDCRunLease(t.Context(), "conn-busy")
	if err == nil || !strings.Contains(err.Error(), "-999") {
		t.Fatalf("expected return code error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedCDCRunLeaseDetectsLostLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
	resource := mssqlCDCRunLeaseResource("conn-lost")
	mock.ExpectQuery(regexp.QuoteMeta(mssqlAcquireAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(mssqlCheckAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"mode"}).AddRow("NoLock"))
	mock.ExpectQuery(regexp.QuoteMeta(mssqlReleaseAppLockSQL)).
		WithArgs(resource).
		WillReturnRows(sqlmock.NewRows([]string{"rc"}).AddRow(0))

	lease, err := dest.AcquireManagedCDCRunLease(t.Context(), "conn-lost")
	if err != nil {
		t.Fatalf("AcquireManagedCDCRunLease failed: %v", err)
	}
	defer func() { _ = lease.Release() }()

	select {
	case <-lease.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lease loss detection")
	}
	if !errors.Is(lease.Err(), source.ErrConnectorLeaseLost) {
		t.Fatalf("expected ErrConnectorLeaseLost, got %v", lease.Err())
	}
}

func TestMergeTableCompatibilityBelow130UsesCharindexSoftAndHardDelete(t *testing.T) {
	columns := []string{"id", "Foo", "foo", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}

	for _, hardDelete := range []bool{false, true} {
		name := "soft_delete"
		if hardDelete {
			name = "hard_delete"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()

			dest := &MSSQLDestination{db: db, server: "srv", database: "warehouse", compatibilityLevel: 120}
			mock.ExpectExec("MERGE \\[dbo\\]\\.\\[items\\] AS target").
				WillReturnResult(sqlmock.NewResult(0, 1))

			err = dest.MergeTable(t.Context(), destination.MergeOptions{
				TargetTable:   "dbo.items",
				StagingTable:  "stage.items",
				PrimaryKeys:   []string{"id"},
				Columns:       columns,
				CDCHardDelete: hardDelete,
			})
			if err != nil {
				t.Fatalf("MergeTable failed: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}

	softSQL := buildMergeSQLWithOptions("dbo.items", "stage.items", []string{"id"}, columns, "", "", false)
	if strings.Contains(softSQL, "OPENJSON") {
		t.Fatalf("compat < 130 soft merge SQL must not use OPENJSON:\n%s", softSQL)
	}
	assertContains(t, softSQL, `CHARINDEX(N'["Foo"' COLLATE Latin1_General_100_BIN2`)
	assertContains(t, softSQL, `CHARINDEX(N',"Foo"' COLLATE Latin1_General_100_BIN2`)
	assertContains(t, softSQL, `CHARINDEX(N'["foo"' COLLATE Latin1_General_100_BIN2`)
	assertContains(t, softSQL, `CHARINDEX(N',"foo"' COLLATE Latin1_General_100_BIN2`)
	assertContains(t, softSQL, `COALESCE(source.[_cdc_unchanged_cols], N'[]') COLLATE Latin1_General_100_BIN2`)

	hardSQL := buildCDCHardDeleteMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "", false)
	if strings.Contains(hardSQL, "OPENJSON") {
		t.Fatalf("compat < 130 hard-delete merge SQL must not use OPENJSON:\n%s", hardSQL)
	}
	assertContains(t, hardSQL, "THEN DELETE")
	assertContains(t, hardSQL, `CHARINDEX(N'["Foo"' COLLATE Latin1_General_100_BIN2`)
	assertContains(t, hardSQL, `CHARINDEX(N'["foo"' COLLATE Latin1_General_100_BIN2`)

	hardOpenJSONSQL := buildCDCHardDeleteMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "", true)
	assertContains(t, hardOpenJSONSQL, "THEN DELETE")
	assertContains(t, hardOpenJSONSQL, "OPENJSON(COALESCE(source.[_cdc_unchanged_cols], N'[]'))")
}

func TestConditionalMergeTableEnforcesIncarnationForSoftAndHardDeletes(t *testing.T) {
	columns := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}
	expected := mssqlTestIncarnation("srv", "app", 101, "2026-01-01T00:00:00")
	replaced := mssqlTestIncarnation("srv", "app", 202, "2026-01-01T00:01:00")

	if !NewMSSQLDestination().SupportsCDCConditionalMerge() {
		t.Fatal("MSSQLDestination must support conditional merge")
	}

	t.Run("commits_when_incarnation_matches", func(t *testing.T) {
		for _, hardDelete := range []bool{false, true} {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			dest := &MSSQLDestination{db: db, server: "srv", database: "app", compatibilityLevel: 150}

			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
				WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
				WithArgs("dbo", "events").
				WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(101), "2026-01-01T00:00:00"))
			if hardDelete {
				mock.ExpectExec("THEN DELETE").WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				mock.ExpectExec("MERGE \\[dbo\\]\\.\\[events\\] AS target").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()

			err = dest.MergeTable(t.Context(), destination.MergeOptions{
				TargetTable:            "dbo.events",
				StagingTable:           "stage.events",
				PrimaryKeys:            []string{"id"},
				Columns:                columns,
				CDCExpectedIncarnation: expected,
				CDCHardDelete:          hardDelete,
			})
			_ = db.Close()
			if err != nil {
				t.Fatalf("conditional MergeTable(hardDelete=%v) failed: %v", hardDelete, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("aborts_before_merge_when_incarnation_changed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app", compatibilityLevel: 150}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(202), "2026-01-01T00:01:00"))
		mock.ExpectRollback()

		err = dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "dbo.events",
			StagingTable:           "stage.events",
			PrimaryKeys:            []string{"id"},
			Columns:                columns,
			CDCExpectedIncarnation: expected,
		})
		if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
			t.Fatalf("expected incarnation mismatch error (replaced=%s), got %v", replaced, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMergeCDCTablesAtomically(t *testing.T) {
	columns := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}
	incA := mssqlTestIncarnation("srv", "app", 10, "2026-01-01T00:00:00")
	incB := mssqlTestIncarnation("srv", "app", 20, "2026-01-01T00:00:00")

	t.Run("commits_multiple_tables_and_truncates_in_one_tx", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app", compatibilityLevel: 150}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[a] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "a").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectExec("MERGE \\[dbo\\]\\.\\[a\\] AS target").WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[b] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "b").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(20), "2026-01-01T00:00:00"))
		mock.ExpectExec(regexp.QuoteMeta("TRUNCATE TABLE [app].[dbo].[b]")).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("MERGE \\[dbo\\]\\.\\[b\\] AS target").WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectCommit()

		err = dest.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{Options: destination.MergeOptions{
				TargetTable:            "dbo.a",
				StagingTable:           "stage.a",
				PrimaryKeys:            []string{"id"},
				Columns:                columns,
				CDCExpectedIncarnation: incA,
			}},
			{Truncate: true, Options: destination.MergeOptions{
				TargetTable:            "dbo.b",
				StagingTable:           "stage.b",
				PrimaryKeys:            []string{"id"},
				Columns:                columns,
				CDCExpectedIncarnation: incB,
				CDCHardDelete:          true,
			}},
		})
		if err != nil {
			t.Fatalf("MergeCDCTablesAtomically failed: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rolls_back_all_tables_when_second_incarnation_mismatches", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app", compatibilityLevel: 150}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[a] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "a").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectExec("MERGE \\[dbo\\]\\.\\[a\\] AS target").WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[b] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "b").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:05:00"))
		mock.ExpectRollback()

		err = dest.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{Options: destination.MergeOptions{
				TargetTable:            "dbo.a",
				StagingTable:           "stage.a",
				PrimaryKeys:            []string{"id"},
				Columns:                columns,
				CDCExpectedIncarnation: incA,
			}},
			{Options: destination.MergeOptions{
				TargetTable:            "dbo.b",
				StagingTable:           "stage.b",
				PrimaryKeys:            []string{"id"},
				Columns:                columns,
				CDCExpectedIncarnation: incB,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
			t.Fatalf("expected incarnation mismatch error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTruncateCDCTableAndIfIncarnation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
	expected := mssqlTestIncarnation("srv", "app", 55, "2026-01-01T00:00:00")

	mock.ExpectExec(regexp.QuoteMeta("TRUNCATE TABLE [dbo].[events]")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := dest.TruncateCDCTable(t.Context(), "dbo.events"); err != nil {
		t.Fatalf("TruncateCDCTable failed: %v", err)
	}

	if err := dest.TruncateCDCTableIfIncarnation(t.Context(), "dbo.events", ""); err == nil {
		t.Fatal("expected error for empty expected incarnation")
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
		WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(55), "2026-01-01T00:00:00"))
	mock.ExpectExec(regexp.QuoteMeta("TRUNCATE TABLE [app].[dbo].[events]")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := dest.TruncateCDCTableIfIncarnation(t.Context(), "dbo.events", expected); err != nil {
		t.Fatalf("TruncateCDCTableIfIncarnation failed: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
		WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(56), "2026-01-01T00:00:00"))
	mock.ExpectRollback()

	err = dest.TruncateCDCTableIfIncarnation(t.Context(), "dbo.events", expected)
	if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
		t.Fatalf("expected incarnation mismatch error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimAndPrepareEmptyCDCTarget(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false, IsPrimaryKey: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	claim := destination.CDCTargetClaim{
		ConnectorID:      "mysql-conn",
		SourceTable:      "orders",
		DestinationTable: "dbo.orders",
	}
	opts := destination.PrepareOptions{
		Table:       "dbo.orders",
		Schema:      tableSchema,
		PrimaryKeys: []string{"id"},
	}

	t.Run("atomically_claims_creates_and_returns_incarnation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
		canonical := destination.CDCTargetKeyDigest("srv", "app", "dbo", "orders")
		ownerID, err := claim.OwnerID()
		if err != nil {
			t.Fatal(err)
		}

		mock.ExpectQuery(regexp.QuoteMeta("SELECT [name], [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1")).
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("app", "Latin1_General_100_CI_AS"))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT [connector_id] FROM [_bruin_staging].[cdc_targets] WITH (UPDLOCK, HOLDLOCK) WHERE [destination_table] = @p1")).
			WithArgs(canonical).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO [_bruin_staging].[cdc_targets] ([destination_table], [connector_id], [claimed_at]) VALUES (@p1, @p2, SYSUTCDATETIME())")).
			WithArgs(canonical, ownerID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`^CREATE TABLE \[app\]\.\[dbo\]\.\[orders\]`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(777), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		inc, err := dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, opts)
		if err != nil {
			t.Fatalf("ClaimAndPrepareEmptyCDCTarget failed: %v", err)
		}
		want := mssqlTestIncarnation("srv", "app", 777, "2026-01-01T00:00:00")
		if inc != want {
			t.Fatalf("incarnation = %q, want %q", inc, want)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rolls_back_claim_if_target_already_exists", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
		canonical := destination.CDCTargetKeyDigest("srv", "app", "dbo", "orders")
		ownerID, _ := claim.OwnerID()

		mock.ExpectQuery(regexp.QuoteMeta("SELECT [name], [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1")).
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("app", "Latin1_General_100_CI_AS"))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT [connector_id] FROM [_bruin_staging].[cdc_targets] WITH (UPDLOCK, HOLDLOCK) WHERE [destination_table] = @p1")).
			WithArgs(canonical).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO [_bruin_staging].[cdc_targets] ([destination_table], [connector_id], [claimed_at]) VALUES (@p1, @p2, SYSUTCDATETIME())")).
			WithArgs(canonical, ownerID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`^CREATE TABLE \[app\]\.\[dbo\]\.\[orders\]`).
			WillReturnError(mssqldb.Error{Number: 2714, Message: "There is already an object named 'orders' in the database."})
		mock.ExpectRollback()

		_, err = dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, opts)
		if err == nil || !strings.Contains(err.Error(), "exclusively create") {
			t.Fatalf("expected exclusive create error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects_conflicting_claim_without_creating_target", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}
		canonical := destination.CDCTargetKeyDigest("srv", "app", "dbo", "orders")

		mock.ExpectQuery(regexp.QuoteMeta("SELECT [name], [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1")).
			WithArgs("app").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("app", "Latin1_General_100_CI_AS"))
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT [connector_id] FROM [_bruin_staging].[cdc_targets] WITH (UPDLOCK, HOLDLOCK) WHERE [destination_table] = @p1")).
			WithArgs(canonical).
			WillReturnRows(sqlmock.NewRows([]string{"connector_id"}).AddRow("other-owner"))
		mock.ExpectRollback()

		_, err = dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, opts)
		if err == nil || !strings.Contains(err.Error(), "already claimed") {
			t.Fatalf("expected already claimed error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestConditionalSwapPlannerAndSwapTable(t *testing.T) {
	if !NewMSSQLDestination().SupportsCDCConditionalSwap() {
		t.Fatal("MSSQLDestination must support conditional swap")
	}

	targetInc := mssqlTestIncarnation("srv", "app", 11, "2026-01-01T00:00:00")
	stagingInc := mssqlTestIncarnation("srv", "app", 22, "2026-01-01T00:01:00")

	t.Run("planner_returns_staging_incarnation_for_result", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("_bruin_staging", "events_stage").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(22), "2026-01-01T00:01:00"))

		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(t.Context(), "dbo.events", "_bruin_staging.events_stage")
		if err != nil {
			t.Fatalf("CDCConditionalSwapIncarnations failed: %v", err)
		}
		if gotStaging != stagingInc || gotResult != stagingInc {
			t.Fatalf("got (%q, %q), want (%q, %q)", gotStaging, gotResult, stagingInc, stagingInc)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("swap_commits_when_target_and_staging_incarnations_match", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(11), "2026-01-01T00:00:00"))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events_stage] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "events_stage").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(22), "2026-01-01T00:01:00"))

		mock.ExpectQuery(regexp.QuoteMeta("SELECT CASE WHEN OBJECT_ID('[app].[dbo].[events]', 'U') IS NOT NULL THEN 1 ELSE 0 END")).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
		mock.ExpectExec("EXEC sys\\.sp_rename 'dbo\\.events'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("EXEC sys\\.sp_rename 'dbo\\.events_stage', 'events'").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("DROP TABLE \\[app\\]\\.\\[dbo\\]\\.\\[events_old_").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "dbo.events",
			StagingTable:                  "dbo.events_stage",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  stagingInc,
		})
		if err != nil {
			t.Fatalf("conditional SwapTable failed: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("swap_aborts_when_target_incarnation_changed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[events] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:00:00"))
		mock.ExpectRollback()

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "dbo.events",
			StagingTable:                  "dbo.events_stage",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  stagingInc,
		})
		if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
			t.Fatalf("expected incarnation mismatch error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("swap_rejects_partial_or_mismatched_incarnations", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectBegin()
		mock.ExpectRollback()
		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:            "dbo.events",
			StagingTable:           "dbo.events_stage",
			CDCExpectedIncarnation: targetInc,
		})
		if err == nil || !strings.Contains(err.Error(), "requires target, staging, and result incarnations") {
			t.Fatalf("expected partial incarnation error, got %v", err)
		}

		mock.ExpectBegin()
		mock.ExpectRollback()
		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "dbo.events",
			StagingTable:                  "dbo.events_stage",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  targetInc,
		})
		if err == nil || !strings.Contains(err.Error(), "cannot match the staging table") {
			t.Fatalf("expected mismatched result incarnation error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestApplySchemaEvolutionIfIncarnation(t *testing.T) {
	expected := mssqlTestIncarnation("srv", "app", 42, "2026-01-01T00:00:00")
	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{
			{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "note",
				NewColumn:  schema.Column{Name: "note", DataType: schema.TypeString, Nullable: true},
			},
		},
	}

	t.Run("executes_alter_and_returns_incarnation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[items] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "items").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(42), "2026-01-01T00:00:00"))
		mock.ExpectExec("ALTER TABLE dbo\\.items ADD \\[note\\] NVARCHAR\\(MAX\\) NULL").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "items").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(42), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		_, result, err := dest.ApplySchemaEvolutionIfIncarnation(t.Context(), "dbo.items", comparison, expected)
		if err != nil {
			t.Fatalf("ApplySchemaEvolutionIfIncarnation failed: %v", err)
		}
		if result != expected {
			t.Fatalf("result incarnation = %q, want %q", result, expected)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("aborts_on_incarnation_mismatch", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT TOP (1) 1 FROM [app].[dbo].[items] WITH (TABLOCKX, HOLDLOCK)")).
			WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)")).
			WithArgs("dbo", "items").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:00:00"))
		mock.ExpectRollback()

		_, _, err = dest.ApplySchemaEvolutionIfIncarnation(t.Context(), "dbo.items", comparison, expected)
		if err == nil || !strings.Contains(err.Error(), "physical incarnation changed") {
			t.Fatalf("expected incarnation mismatch error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDeleteCDCStateEventsBatchesLargeEventLists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "srv", database: "app"}

	eventIDs := make([]string, 1005)
	for i := range eventIDs {
		eventIDs[i] = fmt.Sprintf("evt-%d", i)
	}
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM \\[_bruin_staging\\]\\.\\[cdc_state\\] WHERE \\[connector_id\\] = @p1 AND \\[event_id\\] IN \\(").
		WillReturnResult(sqlmock.NewResult(0, 1000))
	mock.ExpectExec("DELETE FROM \\[_bruin_staging\\]\\.\\[cdc_state\\] WHERE \\[connector_id\\] = @p1 AND \\[event_id\\] IN \\(").
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	if err := dest.DeleteCDCStateEvents(t.Context(), "_bruin_staging.cdc_state", "conn-1", eventIDs); err != nil {
		t.Fatalf("DeleteCDCStateEvents failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
