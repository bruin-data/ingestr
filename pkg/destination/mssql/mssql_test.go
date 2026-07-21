package mssql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	mssqldb "github.com/microsoft/go-mssqldb"
)

type releaseCountingBatch struct {
	arrow.RecordBatch
	releases int
}

func TestResolveMSSQLTargetIdentityIncludesServerAndDatabase(t *testing.T) {
	tests := map[string]mssqlTargetIdentity{
		"orders":                        {server: "srv", database: "db", schema: "sales", table: "orders"},
		`"sales"."orders"`:              {server: "srv", database: "db", schema: "sales", table: "orders"},
		"warehouse.sales.orders":        {server: "srv", database: "warehouse", schema: "sales", table: "orders"},
		"linked.warehouse.sales.orders": {server: "linked", database: "warehouse", schema: "sales", table: "orders"},
	}
	for input, want := range tests {
		if got := resolveMSSQLTargetIdentity("srv", "db", "sales", input); got != want {
			t.Fatalf("resolveMSSQLTargetIdentity(%q) = %#v, want %#v", input, got, want)
		}
	}
	if !mssqlCollationCaseSensitive("Latin1_General_100_CS_AS") || mssqlCollationCaseSensitive("Latin1_General_100_CI_AS") {
		t.Fatal("collation case-sensitivity detection is incorrect")
	}
}

func TestValidateManagedCDCStateRequiresOpenJSONCompatibility(t *testing.T) {
	err := (&MSSQLDestination{database: "warehouse", compatibilityLevel: 120}).ValidateManagedCDCState()
	if err == nil || !strings.Contains(err.Error(), "requires level 130") {
		t.Fatalf("compatibility level 120 error = %v", err)
	}
	if err := (&MSSQLDestination{database: "warehouse", compatibilityLevel: 130}).ValidateManagedCDCState(); err != nil {
		t.Fatalf("compatibility level 130 rejected: %v", err)
	}
}

func TestValidateManagedCDCTargetChecksCrossDatabaseCompatibility(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "local", database: "default", compatibilityLevel: 160}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT [compatibility_level] FROM [master].sys.databases WHERE [name] = @p1")).
		WithArgs("legacy").WillReturnRows(sqlmock.NewRows([]string{"compatibility_level"}).AddRow(120))

	err = dest.ValidateManagedCDCTarget(t.Context(), "legacy.dbo.items")
	if err == nil || !strings.Contains(err.Error(), "requires level 130") {
		t.Fatalf("cross-database compatibility error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalCDCTargetUsesConnectedDefaultSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SCHEMA_NAME(), 'dbo')")).WillReturnRows(sqlmock.NewRows([]string{"schema"}).AddRow("tenant_a"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT [name], [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1")).
		WithArgs("AppDB").
		WillReturnRows(sqlmock.NewRows([]string{"name", "collation"}).AddRow("AppDB", "Latin1_General_100_CI_AS"))

	got, err := dest.CanonicalCDCTarget(t.Context(), "orders")
	if err != nil {
		t.Fatal(err)
	}
	want := destination.CDCTargetKeyDigest("server-a", "AppDB", "tenant_a", "orders")
	if got != want {
		t.Fatalf("CanonicalCDCTarget() = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetTableSchemaUsesConnectedDefaultSchemaAndDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SCHEMA_NAME(), 'dbo')")).
		WillReturnRows(sqlmock.NewRows([]string{"schema"}).AddRow("tenant_a"))
	mock.ExpectQuery(`FROM \[AppDB\]\.INFORMATION_SCHEMA\.COLUMNS c`).
		WithArgs("tenant_a", "orders").
		WillReturnRows(sqlmock.NewRows([]string{
			"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH",
		}).AddRow("id", "bigint", "NO", nil, nil, nil))

	got, err := dest.GetTableSchema(t.Context(), "orders")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Schema != "tenant_a" || got.Name != "orders" {
		t.Fatalf("GetTableSchema() = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetTableSchemaUsesThreePartDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectQuery(`FROM \[Warehouse\]\.INFORMATION_SCHEMA\.COLUMNS c`).
		WithArgs("sales", "orders").
		WillReturnRows(sqlmock.NewRows([]string{
			"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH",
		}).AddRow("id", "bigint", "NO", nil, nil, nil))

	got, err := dest.GetTableSchema(t.Context(), "Warehouse.sales.orders")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Schema != "sales" || got.Name != "orders" {
		t.Fatalf("GetTableSchema() = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareTableUsesConnectedDefaultSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SCHEMA_NAME(), 'dbo')")).
		WillReturnRows(sqlmock.NewRows([]string{"schema"}).AddRow("tenant_a"))
	mock.ExpectExec(`IF NOT EXISTS .*CREATE SCHEMA \[tenant_a\]`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE \[AppDB\]\.\[tenant_a\]\.\[orders\]`).WillReturnResult(sqlmock.NewResult(0, 0))

	err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table:  "orders",
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSwapTableUsesResolvedDefaultSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(SCHEMA_NAME(), 'dbo')")).
		WillReturnRows(sqlmock.NewRows([]string{"schema"}).AddRow("tenant_a"))
	mock.ExpectExec(`IF NOT EXISTS .*CREATE SCHEMA \[tenant_a\]`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("ALTER SCHEMA [tenant_a] TRANSFER [stage].[orders_staging]")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`OBJECT_ID\('\[AppDB\]\.\[tenant_a\]\.\[orders\]', 'U'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("EXEC sys.sp_rename 'tenant_a.orders_staging', 'orders'")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = dest.SwapTable(t.Context(), destination.SwapOptions{
		TargetTable:  "orders",
		StagingTable: "stage.orders_staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSwapTableTransfersCaseDistinctSchemasUnderCaseSensitiveCollation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	mock.ExpectExec(`IF NOT EXISTS .*CREATE SCHEMA \[Sales\]`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT [collation_name] FROM [master].[sys].[databases] WHERE [name] = @p1")).
		WithArgs("AppDB").
		WillReturnRows(sqlmock.NewRows([]string{"collation_name"}).AddRow("Latin1_General_100_CS_AS"))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("ALTER SCHEMA [Sales] TRANSFER [sales].[orders_staging]")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`OBJECT_ID\('\[AppDB\]\.\[Sales\]\.\[orders\]', 'U'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("EXEC sys.sp_rename 'Sales.orders_staging', 'orders'")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err = dest.SwapTable(t.Context(), destination.SwapOptions{
		TargetTable:  "Sales.orders",
		StagingTable: "sales.orders_staging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCDCTargetIncarnationChangesWithObjectIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	dest := &MSSQLDestination{db: db, server: "server-a", database: "AppDB"}
	query := regexp.QuoteMeta("SELECT CAST(t.object_id AS BIGINT), CONVERT(NVARCHAR(33), t.create_date, 126)\n\t\tFROM [AppDB].sys.tables AS t\n\t\tJOIN [AppDB].sys.schemas AS s ON s.schema_id = t.schema_id\n\t\tWHERE s.name = @p1 AND t.name = @p2")
	mock.ExpectQuery(query).WithArgs("dbo", "orders").WillReturnRows(sqlmock.NewRows([]string{"object_id", "created"}).AddRow(10, "2026-01-01T00:00:00"))
	mock.ExpectQuery(query).WithArgs("dbo", "orders").WillReturnRows(sqlmock.NewRows([]string{"object_id", "created"}).AddRow(11, "2026-01-01T00:00:01"))

	first, exists, err := dest.CDCTargetIncarnation(t.Context(), "dbo.orders")
	if err != nil || !exists {
		t.Fatalf("first incarnation=%q exists=%v err=%v", first, exists, err)
	}
	second, exists, err := dest.CDCTargetIncarnation(t.Context(), "dbo.orders")
	if err != nil || !exists || second == first {
		t.Fatalf("second incarnation=%q first=%q exists=%v err=%v", second, first, exists, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func (b *releaseCountingBatch) Release() {
	b.releases++
	b.RecordBatch.Release()
}

func newReleaseCountingBatch(t *testing.T) *releaseCountingBatch {
	t.Helper()
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(1)
	values := builder.NewArray()
	builder.Release()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{values}, 1)
	values.Release()
	return &releaseCountingBatch{RecordBatch: record}
}

func TestWriteSerialBatchesReleasesBatchOnWriteError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
	dest := &MSSQLDestination{db: db}
	batch := newReleaseCountingBatch(t)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)

	err = dest.Write(t.Context(), records, destination.WriteOptions{Table: "dbo.events"})
	if err == nil || !strings.Contains(err.Error(), "begin failed") {
		t.Fatalf("Write() error = %v, want begin failure", err)
	}
	if batch.releases != 1 {
		t.Fatalf("batch releases = %d, want exactly 1", batch.releases)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareTableAcceptsConcurrentCreateWinner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	schema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	createSQL := buildCreateTableSQL("dbo.events", schema.Columns, nil)
	mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnError(mssqldb.Error{Number: 2714, Message: "There is already an object named 'events' in the database."})

	dest := &MSSQLDestination{db: db}
	if err := dest.PrepareTable(context.Background(), destination.PrepareOptions{Table: "dbo.events", Schema: schema}); err != nil {
		t.Fatalf("PrepareTable() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSchemaExistsAcceptsConcurrentCreateWinner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	createSQL := "IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '_bruin_staging') EXEC('CREATE SCHEMA [_bruin_staging]')"
	mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnError(mssqldb.Error{
		Number:  2759,
		Message: "CREATE SCHEMA failed due to previous errors.",
		All: []mssqldb.Error{
			{Number: 2714, Message: "There is already an object named '_bruin_staging' in the database."},
			{Number: 2759, Message: "CREATE SCHEMA failed due to previous errors."},
		},
	})

	dest := &MSSQLDestination{db: db}
	if err := dest.ensureSchemaExists(context.Background(), "_bruin_staging"); err != nil {
		t.Fatalf("ensureSchemaExists() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSchemaExistsDoesNotHideOtherErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	createSQL := "IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '_bruin_staging') EXEC('CREATE SCHEMA [_bruin_staging]')"
	mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnError(mssqldb.Error{Number: 262, Message: "CREATE SCHEMA permission denied"})

	dest := &MSSQLDestination{db: db}
	err = dest.ensureSchemaExists(context.Background(), "_bruin_staging")
	if err == nil || !strings.Contains(err.Error(), "failed to create schema") {
		t.Fatalf("ensureSchemaExists() error = %v, want schema creation failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCDCStateEventsScopesConnectorAndEventIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	query := "DELETE FROM [dbo].[cdc_state] WHERE [connector_id] = @p1 AND [event_id] IN (@p2, @p3)"
	mock.ExpectExec(regexp.QuoteMeta(query)).WithArgs("connector-a", "event-1", "event-2").WillReturnResult(sqlmock.NewResult(0, 2))
	dest := &MSSQLDestination{db: db}
	if err := dest.DeleteCDCStateEvents(context.Background(), "dbo.cdc_state", "connector-a", []string{"event-1", "event-2"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCDCStateFenceUsesConnectorScopedLatestRunQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	query := "SELECT DISTINCT [event_id], [state_generation] FROM [dbo].[cdc_state] WHERE [connector_id] = @p1 AND [state_kind] = 'run' AND [state_generation] = (SELECT MAX([state_generation]) FROM [dbo].[cdc_state] WHERE [connector_id] = @p1 AND [state_kind] = 'run') ORDER BY [event_id]"
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("connector-a").WillReturnRows(
		sqlmock.NewRows([]string{"event_id", "state_generation"}).AddRow("run-a", int64(7)).AddRow("run-b", int64(7)),
	)

	dest := &MSSQLDestination{db: db}
	fence, err := dest.LoadCDCStateFence(context.Background(), "dbo.cdc_state", "connector-a")
	if err != nil {
		t.Fatal(err)
	}
	if fence.Generation != 7 || len(fence.RunEventIDs) != 2 || fence.RunEventIDs[0] != "run-a" || fence.RunEventIDs[1] != "run-b" {
		t.Fatalf("LoadCDCStateFence() = %#v", fence)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareTableDoesNotHideOtherCreateErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	schema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	createSQL := buildCreateTableSQL("dbo.events", schema.Columns, nil)
	mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnError(mssqldb.Error{Number: 262, Message: "CREATE TABLE permission denied"})

	dest := &MSSQLDestination{db: db}
	err = dest.PrepareTable(context.Background(), destination.PrepareOptions{Table: "dbo.events", Schema: schema})
	if err == nil || !strings.Contains(err.Error(), "failed to create table") {
		t.Fatalf("PrepareTable() error = %v, want create failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCreateTableSQL_StringPrimaryKeyUsesIndexableType(t *testing.T) {
	sql := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "_id", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeJSON},
	}, []string{"_id"})

	assertContains(t, sql, "[_id] NVARCHAR(450)")
	assertContains(t, sql, "[payload] NVARCHAR(MAX)")
	assertContains(t, sql, "PRIMARY KEY ([_id])")
}

func TestBuildCreateTableSQL_CapsLongStringPrimaryKeyLength(t *testing.T) {
	sql := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "id", DataType: schema.TypeString, MaxLength: 1000},
		{Name: "name", DataType: schema.TypeString, MaxLength: 1000},
	}, []string{"id"})

	assertContains(t, sql, "[id] NVARCHAR(450)")
	assertContains(t, sql, "[name] NVARCHAR(1000)")
}

func TestBuildCreateTableSQLKeepsCaseDistinctPayloadUnbounded(t *testing.T) {
	sql := buildCreateTableSQL("dbo.events", []schema.Column{
		{Name: "Foo", DataType: schema.TypeString},
		{Name: "foo", DataType: schema.TypeString},
	}, []string{"Foo"})

	assertContains(t, sql, "[Foo] NVARCHAR(450)")
	assertContains(t, sql, "[foo] NVARCHAR(MAX)")
	assertContains(t, sql, "PRIMARY KEY ([Foo])")
}

func TestBuildCDCMergeSQLPreservesMarkedColumnsAndOmitsMarkerFromTarget(t *testing.T) {
	columns := []string{
		"id",
		"payload",
		destination.CDCLSNColumn,
		destination.CDCDeletedColumn,
		destination.CDCSyncedAtColumn,
		destination.CDCUnchangedColsColumn,
	}
	got := buildMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "")

	assertContains(t, got, "OPENJSON(COALESCE(source.[_cdc_unchanged_cols], N'[]'))")
	assertContains(t, got, "[value] COLLATE Latin1_General_100_BIN2 = N'payload' COLLATE Latin1_General_100_BIN2")
	assertContains(t, got, "THEN source.[payload] ELSE target.[payload] END")
	assertContains(t, got, "WHEN MATCHED AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] > target.[_cdc_lsn] OR (source.[_cdc_lsn] = target.[_cdc_lsn] AND source.[_cdc_deleted] = 1 AND COALESCE(target.[_cdc_deleted], 0) = 0)) THEN UPDATE")
	assertContains(t, got, "WHEN NOT MATCHED THEN INSERT")
	assertContains(t, got, "INSERT ([id], [payload], [_cdc_lsn], [_cdc_deleted], [_cdc_synced_at])")
	if strings.Contains(got, "INSERT ([id], [payload], [_cdc_lsn], [_cdc_deleted], [_cdc_synced_at], [_cdc_unchanged_cols])") {
		t.Fatalf("CDC marker leaked into target INSERT:\n%s", got)
	}
	if !NewMSSQLDestination().SupportsCDCUnchangedCols() {
		t.Fatal("MSSQL destination must advertise unchanged-column support")
	}
}

func TestBuildMergeSQLAvoidsInternalAliasCollisions(t *testing.T) {
	t.Run("non_cdc_dedup", func(t *testing.T) {
		got := buildMergeSQL(
			"dbo.items",
			"stage.items",
			[]string{"id"},
			[]string{"id", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2"},
			"",
		)
		assertContains(t, got, "AS [__bruin_dedup_rn_3]")
		assertContains(t, got, "WHERE [__bruin_dedup_rn_3] = 1")
	})

	t.Run("cdc", func(t *testing.T) {
		columns := []string{
			"id", "payload",
			"__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2",
			"__INGESTR_HAS_ACTIVE", "__ingestr_has_active_2",
			destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn,
		}
		got := buildMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "")

		assertContains(t, got, "AS [__bruin_dedup_rn_3]")
		assertContains(t, got, "WHERE [__bruin_dedup_rn_3] = 1")
		assertContains(t, got, "AS [__ingestr_has_active_3]")
		assertContains(t, got, "source.[__ingestr_has_active_3] = 1")
		assertContains(t, got, "target.[__INGESTR_HAS_ACTIVE] = CASE WHEN")
		assertContains(t, got, "target.[__ingestr_has_active_2] = CASE WHEN")
	})
}

func TestBuildMergeSQLWithIncrementalPredicate(t *testing.T) {
	got := buildMergeSQLWithPredicate(
		"dbo.items",
		"stage.items",
		[]string{"id"},
		[]string{"id", "event_date"},
		"",
		"target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date))",
	)

	assertContains(t, got, "ON target.[id] = source.[id] AND (target.[event_date] >= DATEADD(day, -7, CAST(GETDATE() AS date)))")
}

func TestBuildCDCMergeSQLKeepsIncrementalPredicateOutOfPrimaryKeyMatch(t *testing.T) {
	predicate := "target.[id] >= 10"
	got := buildMergeSQLWithPredicate(
		"dbo.items",
		"stage.items",
		[]string{"id"},
		[]string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		"",
		predicate,
	)

	assertContains(t, got, "ON target.[id] = source.[id]\nWHEN MATCHED AND (target.[id] >= 10) AND (target.[_cdc_lsn] IS NULL")
	if strings.Contains(got, "ON target.[id] = source.[id] AND ("+predicate+")") {
		t.Fatalf("incremental predicate in CDC primary-key match can turn an existing row into an insert:\n%s", got)
	}
}

func TestBuildCDCMergeSQLMatchesUnchangedMarkersCaseSensitively(t *testing.T) {
	columns := []string{"id", "Foo", "foo", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
	got := buildMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "")

	assertContains(t, got, "[value] COLLATE Latin1_General_100_BIN2 = N'Foo' COLLATE Latin1_General_100_BIN2")
	assertContains(t, got, "[value] COLLATE Latin1_General_100_BIN2 = N'foo' COLLATE Latin1_General_100_BIN2")
	assertContains(t, got, "THEN source.[Foo] ELSE target.[Foo] END")
	assertContains(t, got, "THEN source.[foo] ELSE target.[foo] END")
	if strings.Contains(got, "LOWER(") {
		t.Fatalf("case-folded marker membership can conflate [Foo] and [foo]:\n%s", got)
	}
}

func TestBuildCDCMergeSQLKeepsCaseDistinctPayloadSeparateFromPrimaryKey(t *testing.T) {
	columns := []string{"Foo", "foo", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
	got := buildMergeSQL("dbo.items", "stage.items", []string{"Foo"}, columns, "")

	assertContains(t, got, "ON target.[Foo] = source.[Foo]")
	assertContains(t, got, "SELECT la.[Foo], act.[foo]")
	assertContains(t, got, "target.[foo] = CASE WHEN (source.[_cdc_deleted] = 0 OR source.[__ingestr_has_active] = 1) AND NOT EXISTS")
	assertContains(t, got, "THEN source.[foo] ELSE target.[foo] END")
	if strings.Contains(got, "target.[Foo] = CASE") {
		t.Fatalf("case-distinct primary key leaked into update set:\n%s", got)
	}
}

func TestBuildCDCMergeSQLWithoutUnchangedColsMarkerSkipsMarkerPredicate(t *testing.T) {
	columns := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}
	got := buildMergeSQL("dbo.items", "stage.items", []string{"id"}, columns, "")

	if strings.Contains(got, destination.CDCUnchangedColsColumn) {
		t.Fatalf("merge SQL references %s although staging has no such column:\n%s", destination.CDCUnchangedColsColumn, got)
	}
	if strings.Contains(got, "OPENJSON") {
		t.Fatalf("merge SQL uses OPENJSON marker predicate without the marker column:\n%s", got)
	}
	assertContains(t, got, "target.[payload] = CASE WHEN (source.[_cdc_deleted] = 0 OR source.[__ingestr_has_active] = 1) THEN source.[payload] ELSE target.[payload] END")
}

func TestFilterColumnsRetainsOrdinaryCaseInsensitiveMSSQLMatching(t *testing.T) {
	if got := strings.Join(filterColumns([]string{"ID", "Name"}, []string{"id"}), ","); got != "Name" {
		t.Fatalf("ordinary case-insensitive filter = %q, want Name", got)
	}
	if got := strings.Join(filterColumns([]string{"Foo", "foo"}, []string{"Foo"}), ","); got != "foo" {
		t.Fatalf("case-distinct filter = %q, want foo", got)
	}
}

func TestBuildDeleteInsertDeleteSQLUsesTableLock(t *testing.T) {
	sql := buildDeleteInsertDeleteSQL("dbo.events", "updated_at")

	assertContains(t, sql, "DELETE FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)")
	assertContains(t, sql, "[updated_at] >= @p1")
	assertContains(t, sql, "[updated_at] <= @p2")
}

func TestRowsPerSecondAllowsZeroDuration(t *testing.T) {
	if got := rowsPerSecond(10, 0); got != 0 {
		t.Fatalf("rowsPerSecond with zero duration = %v, want 0", got)
	}
	if got := rowsPerSecond(10, time.Second); got != 10 {
		t.Fatalf("rowsPerSecond = %v, want 10", got)
	}
}

func TestBuildTableIsEmptyForUpdateSQLLocksTarget(t *testing.T) {
	sql := buildTableIsEmptyForUpdateSQL("dbo.events")

	assertContains(t, sql, "FROM [dbo].[events] WITH (TABLOCKX, HOLDLOCK)")
}

func TestBuildInsertDedupSQLUsesTableLockAndDedupsPrimaryKey(t *testing.T) {
	sql := buildInsertDedupSQL(
		"dbo.events",
		"_bruin_staging.events_raw",
		[]string{"id"},
		[]string{"id", "name", "updated_at"},
		"updated_at",
	)

	assertContains(t, sql, "INSERT INTO [dbo].[events] WITH (TABLOCK) ([id], [name], [updated_at])")
	assertContains(t, sql, "ROW_NUMBER() OVER (PARTITION BY [id] ORDER BY [updated_at] DESC)")
	assertContains(t, sql, "FROM [_bruin_staging].[events_raw]")
}

func TestBuildInsertDedupSQLAllowsNoIncrementalKey(t *testing.T) {
	sql := buildInsertDedupSQL(
		"dbo.events",
		"_bruin_staging.events_raw",
		[]string{"id"},
		[]string{"id", "name"},
		"",
	)

	assertContains(t, sql, "ROW_NUMBER() OVER (PARTITION BY [id] ORDER BY (SELECT NULL))")
}

func TestBuildInsertDedupSQLAvoidsInternalAliasCollisions(t *testing.T) {
	sql := buildInsertDedupSQL(
		"dbo.events",
		"_bruin_staging.events_raw",
		[]string{"id"},
		[]string{"id", "__BRUIN_DEDUP_RN", "__bruin_dedup_rn_2"},
		"",
	)

	assertContains(t, sql, "AS [__bruin_dedup_rn_3]")
	assertContains(t, sql, "WHERE [__bruin_dedup_rn_3] = 1")
}

func TestBuildInsertDirectSQL(t *testing.T) {
	sql := buildInsertDirectSQL(
		"dbo.events",
		"_bruin_staging.events_raw",
		[]string{"id", "name"},
	)

	assertContains(t, sql, "INSERT INTO [dbo].[events] WITH (TABLOCK) ([id], [name])")
	assertContains(t, sql, "SELECT [id], [name] FROM [_bruin_staging].[events_raw]")
}

func TestInsertIntoEmptyTargetDirectSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db}
	target := "dbo.events_staging_normalised_123"
	staging := "_bruin_staging.events_raw"
	directSQL := buildInsertDirectSQL(target, staging, []string{"id", "name"})
	dedupSQL := buildInsertDedupSQL(target, staging, []string{"id"}, []string{"id", "name"}, "")

	mock.ExpectBegin()
	expectEmptyTableCheck(mock, target)
	mock.ExpectExec(regexp.QuoteMeta("SAVE TRANSACTION bruin_direct_insert")).WillReturnResult(sqlmock.NewResult(0, 0))
	expectDropPrimaryKey(mock, target, "PK_events")
	mock.ExpectExec(regexp.QuoteMeta(directSQL)).WillReturnResult(sqlmock.NewResult(0, 10))
	expectAddPrimaryKey(mock, target, "PK_events")
	mock.ExpectCommit()

	inserted, err := dest.insertIntoEmptyTarget(context.Background(), target, []string{"id"}, directSQL, dedupSQL)
	if err != nil {
		t.Fatalf("insertIntoEmptyTarget() error = %v", err)
	}
	if !inserted {
		t.Fatal("insertIntoEmptyTarget() inserted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertIntoEmptyTargetFallsBackWhenDirectPrimaryKeyFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db}
	target := "dbo.events_staging_normalised_123"
	staging := "_bruin_staging.events_raw"
	directSQL := buildInsertDirectSQL(target, staging, []string{"id", "name"})
	dedupSQL := buildInsertDedupSQL(target, staging, []string{"id"}, []string{"id", "name"}, "")

	mock.ExpectBegin()
	expectEmptyTableCheck(mock, target)
	mock.ExpectExec(regexp.QuoteMeta("SAVE TRANSACTION bruin_direct_insert")).WillReturnResult(sqlmock.NewResult(0, 0))
	expectDropPrimaryKey(mock, target, "PK_events")
	mock.ExpectExec(regexp.QuoteMeta(directSQL)).WillReturnResult(sqlmock.NewResult(0, 10))
	expectAddPrimaryKeyError(mock, target, "PK_events", errors.New("duplicate key"))
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TRANSACTION bruin_direct_insert")).WillReturnResult(sqlmock.NewResult(0, 0))
	expectDropPrimaryKey(mock, target, "PK_events")
	mock.ExpectExec(regexp.QuoteMeta(dedupSQL)).WillReturnResult(sqlmock.NewResult(0, 10))
	expectAddPrimaryKey(mock, target, "PK_events")
	mock.ExpectCommit()

	inserted, err := dest.insertIntoEmptyTarget(context.Background(), target, []string{"id"}, directSQL, dedupSQL)
	if err != nil {
		t.Fatalf("insertIntoEmptyTarget() error = %v", err)
	}
	if !inserted {
		t.Fatal("insertIntoEmptyTarget() inserted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInsertIntoEmptyTargetRetriesDedupWhenDirectRollbackFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	dest := &MSSQLDestination{db: db}
	target := "dbo.events_staging_normalised_123"
	staging := "_bruin_staging.events_raw"
	directSQL := buildInsertDirectSQL(target, staging, []string{"id", "name"})
	dedupSQL := buildInsertDedupSQL(target, staging, []string{"id"}, []string{"id", "name"}, "")

	mock.ExpectBegin()
	expectEmptyTableCheck(mock, target)
	mock.ExpectExec(regexp.QuoteMeta("SAVE TRANSACTION bruin_direct_insert")).WillReturnResult(sqlmock.NewResult(0, 0))
	expectDropPrimaryKey(mock, target, "PK_events")
	mock.ExpectExec(regexp.QuoteMeta(directSQL)).WillReturnResult(sqlmock.NewResult(0, 10))
	expectAddPrimaryKeyError(mock, target, "PK_events", errors.New("duplicate key"))
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TRANSACTION bruin_direct_insert")).WillReturnError(errors.New("transaction aborted"))
	mock.ExpectRollback()

	mock.ExpectBegin()
	expectEmptyTableCheck(mock, target)
	expectDropPrimaryKey(mock, target, "PK_events")
	mock.ExpectExec(regexp.QuoteMeta(dedupSQL)).WillReturnResult(sqlmock.NewResult(0, 3))
	expectAddPrimaryKey(mock, target, "PK_events")
	mock.ExpectCommit()

	inserted, err := dest.insertIntoEmptyTarget(context.Background(), target, []string{"id"}, directSQL, dedupSQL)
	if err != nil {
		t.Fatalf("insertIntoEmptyTarget() error = %v", err)
	}
	if !inserted {
		t.Fatal("insertIntoEmptyTarget() inserted = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectEmptyTableCheck(mock sqlmock.Sqlmock, table string) {
	mock.ExpectQuery(regexp.QuoteMeta(buildTableIsEmptyForUpdateSQL(table))).
		WillReturnError(sql.ErrNoRows)
}

func expectDropPrimaryKey(mock sqlmock.Sqlmock, table, constraintName string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT kc.name
FROM sys.key_constraints AS kc
WHERE kc.parent_object_id = OBJECT_ID(@p1) AND kc.[type] = 'PK'`)).
		WithArgs(table).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow(constraintName))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", quoteTable(table), quoteColumn(constraintName)))).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectAddPrimaryKey(mock sqlmock.Sqlmock, table, constraintName string) {
	mock.ExpectExec(regexp.QuoteMeta(buildAddPrimaryKeySQLForTest(table, constraintName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectAddPrimaryKeyError(mock sqlmock.Sqlmock, table, constraintName string, err error) {
	mock.ExpectExec(regexp.QuoteMeta(buildAddPrimaryKeySQLForTest(table, constraintName))).
		WillReturnError(err)
}

func buildAddPrimaryKeySQLForTest(table, constraintName string) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY ([id]) WITH (SORT_IN_TEMPDB = ON)", quoteTable(table), quoteColumn(constraintName))
}

func TestDialectTypeNameUsesDestinationTypeMapping(t *testing.T) {
	dialect := &Dialect{}
	columns := []schema.Column{
		{Name: "amount", DataType: schema.TypeDecimal},
		{Name: "ratio", DataType: schema.TypeDecimal, Precision: 50, Scale: 60},
		{Name: "payload", DataType: schema.TypeBinary, MaxLength: 64},
		{Name: "event_time", DataType: schema.TypeTime},
		{Name: "event_at", DataType: schema.TypeTimestamp},
	}

	for _, col := range columns {
		if got, want := dialect.TypeName(col), MapDataTypeToMSSQL(col); got != want {
			t.Fatalf("TypeName(%s) = %s, want %s", col.Name, got, want)
		}
	}
}

func TestExtractValueReturnsNativeTypesForBulkCopy(t *testing.T) {
	pool := memory.DefaultAllocator

	boolBuilder := array.NewBooleanBuilder(memory.DefaultAllocator)
	boolBuilder.Append(true)
	boolArray := boolBuilder.NewArray()
	defer boolArray.Release()
	boolBuilder.Release()

	if got := mustExtractValue(t, boolArray, nil); got != true {
		t.Fatalf("expected bool true, got %T %v", got, got)
	}

	int8Builder := array.NewInt8Builder(pool)
	int8Builder.Append(-12)
	int8Array := int8Builder.NewArray()
	defer int8Array.Release()
	int8Builder.Release()

	if got := mustExtractValue(t, int8Array, nil); got != int32(-12) {
		t.Fatalf("expected int32 -12, got %T %v", got, got)
	}

	int16Builder := array.NewInt16Builder(pool)
	int16Builder.Append(42)
	int16Array := int16Builder.NewArray()
	defer int16Array.Release()
	int16Builder.Release()

	if got := mustExtractValue(t, int16Array, nil); got != int32(42) {
		t.Fatalf("expected int32 42, got %T %v", got, got)
	}

	uint8Builder := array.NewUint8Builder(pool)
	uint8Builder.Append(255)
	uint8Array := uint8Builder.NewArray()
	defer uint8Array.Release()
	uint8Builder.Release()

	if got := mustExtractValue(t, uint8Array, nil); got != int32(255) {
		t.Fatalf("expected int32 255, got %T %v", got, got)
	}

	uint16Builder := array.NewUint16Builder(pool)
	uint16Builder.Append(65535)
	uint16Array := uint16Builder.NewArray()
	defer uint16Array.Release()
	uint16Builder.Release()

	if got := mustExtractValue(t, uint16Array, nil); got != int32(65535) {
		t.Fatalf("expected int32 65535, got %T %v", got, got)
	}

	uint32Builder := array.NewUint32Builder(pool)
	uint32Builder.Append(4294967295)
	uint32Array := uint32Builder.NewArray()
	defer uint32Array.Release()
	uint32Builder.Release()

	if got := mustExtractValue(t, uint32Array, nil); got != int64(4294967295) {
		t.Fatalf("expected int64 4294967295, got %T %v", got, got)
	}

	uint64Builder := array.NewUint64Builder(pool)
	uint64Builder.Append(uint64(1<<63 - 1))
	uint64Array := uint64Builder.NewArray()
	defer uint64Array.Release()
	uint64Builder.Release()

	if got := mustExtractValue(t, uint64Array, nil); got != int64(1<<63-1) {
		t.Fatalf("expected max int64, got %T %v", got, got)
	}

	tsType := &arrow.TimestampType{Unit: arrow.Microsecond}
	tsBuilder := array.NewTimestampBuilder(pool, tsType)
	want := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	tsBuilder.Append(arrow.Timestamp(want.UnixMicro()))
	tsArray := tsBuilder.NewArray()
	defer tsArray.Release()
	tsBuilder.Release()

	if got := mustExtractValue(t, tsArray, nil); !got.(time.Time).Equal(want) {
		t.Fatalf("expected timestamp %v, got %T %v", want, got, got)
	}

	time32Type := &arrow.Time32Type{Unit: arrow.Second}
	time32Builder := array.NewTime32Builder(pool, time32Type)
	time32Builder.Append(arrow.Time32(3723))
	time32Array := time32Builder.NewArray()
	defer time32Array.Release()
	time32Builder.Release()

	wantTime := time.Date(1, 1, 1, 1, 2, 3, 0, time.UTC)
	if got := mustExtractValue(t, time32Array, nil); !got.(time.Time).Equal(wantTime) {
		t.Fatalf("expected time32 %v, got %T %v", wantTime, got, got)
	}

	time64Type := &arrow.Time64Type{Unit: arrow.Microsecond}
	time64Builder := array.NewTime64Builder(pool, time64Type)
	time64Builder.Append(arrow.Time64(3723456789))
	time64Array := time64Builder.NewArray()
	defer time64Array.Release()
	time64Builder.Release()

	wantTime64 := time.Date(1, 1, 1, 1, 2, 3, 456789000, time.UTC)
	if got := mustExtractValue(t, time64Array, nil); !got.(time.Time).Equal(wantTime64) {
		t.Fatalf("expected time64 %v, got %T %v", wantTime64, got, got)
	}

	largeBinaryBuilder := array.NewBinaryBuilder(pool, arrow.BinaryTypes.LargeBinary)
	largeBinaryBuilder.Append([]byte{1, 2, 3})
	largeBinaryArray := largeBinaryBuilder.NewArray()
	defer largeBinaryArray.Release()
	largeBinaryBuilder.Release()

	if got := mustExtractValue(t, largeBinaryArray, nil); !bytes.Equal(got.([]byte), []byte{1, 2, 3}) {
		t.Fatalf("expected large binary bytes, got %T %v", got, got)
	}

	fixedBinaryBuilder := array.NewFixedSizeBinaryBuilder(pool, &arrow.FixedSizeBinaryType{ByteWidth: 3})
	fixedBinaryBuilder.Append([]byte{4, 5, 6})
	fixedBinaryArray := fixedBinaryBuilder.NewArray()
	defer fixedBinaryArray.Release()
	fixedBinaryBuilder.Release()

	if got := mustExtractValue(t, fixedBinaryArray, nil); !bytes.Equal(got.([]byte), []byte{4, 5, 6}) {
		t.Fatalf("expected fixed binary bytes, got %T %v", got, got)
	}

	decimalType := &arrow.Decimal256Type{Precision: 38, Scale: 4}
	decimalBuilder := array.NewDecimal256Builder(pool, decimalType)
	decimalValue, err := decimal.Decimal256FromString("12345678901234567890.1234", 38, 4)
	if err != nil {
		t.Fatalf("failed to build decimal: %v", err)
	}
	decimalBuilder.Append(decimalValue)
	decimalArray := decimalBuilder.NewArray()
	defer decimalArray.Release()
	decimalBuilder.Release()

	if got := mustExtractValue(t, decimalArray, nil); got != "12345678901234567890.1234" {
		t.Fatalf("expected decimal string, got %T %v", got, got)
	}

	listBuilder := array.NewListBuilder(pool, arrow.PrimitiveTypes.Int64)
	listValues := listBuilder.ValueBuilder().(*array.Int64Builder)
	listBuilder.Append(true)
	listValues.AppendValues([]int64{1, 2}, nil)
	listArray := listBuilder.NewArray()
	defer listArray.Release()
	listBuilder.Release()

	if got := mustExtractValue(t, listArray, nil); got != "[1,2]" {
		t.Fatalf("expected JSON list, got %T %v", got, got)
	}

	structType := arrow.StructOf(
		arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "name", Type: arrow.BinaryTypes.String},
	)
	structBuilder := array.NewStructBuilder(pool, structType)
	structBuilder.Append(true)
	structBuilder.FieldBuilder(0).(*array.Int64Builder).Append(7)
	structBuilder.FieldBuilder(1).(*array.StringBuilder).Append("ada")
	structArray := structBuilder.NewArray()
	defer structArray.Release()
	structBuilder.Release()

	if got := mustExtractValue(t, structArray, nil); got != `{"id":7,"name":"ada"}` {
		t.Fatalf("expected JSON struct, got %T %v", got, got)
	}
}

func TestExtractValueConvertsUUIDStringsForBulkCopy(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Append("01234567-89ab-cdef-0123-456789abcdef")
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	got := mustExtractValue(t, arr, &schema.Column{DataType: schema.TypeUUID})
	uuid, ok := got.(mssqldb.UniqueIdentifier)
	if !ok {
		t.Fatalf("expected UniqueIdentifier, got %T %v", got, got)
	}
	if uuid.String() != "01234567-89AB-CDEF-0123-456789ABCDEF" {
		t.Fatalf("unexpected UUID value: %s", uuid.String())
	}
}

func TestExtractValueRejectsInvalidUUIDStrings(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Append("not-a-uuid")
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	if _, err := extractValue(arr, 0, &schema.Column{DataType: schema.TypeUUID}); err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestColumnsForRecordKeepsCaseDistinctLogicalTypes(t *testing.T) {
	fooBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	fooBuilder.Append("not-a-uuid")
	foo := fooBuilder.NewArray()
	defer foo.Release()
	fooBuilder.Release()

	lowerFooBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	lowerFooBuilder.Append("01234567-89ab-cdef-0123-456789abcdef")
	lowerFoo := lowerFooBuilder.NewArray()
	defer lowerFoo.Release()
	lowerFooBuilder.Release()

	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{
			{Name: "Foo", Type: arrow.BinaryTypes.String},
			{Name: "foo", Type: arrow.BinaryTypes.String},
		}, nil),
		[]arrow.Array{foo, lowerFoo},
		1,
	)
	defer record.Release()
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "Foo", DataType: schema.TypeString},
		{Name: "foo", DataType: schema.TypeUUID},
	}}

	columnTypes := columnsForRecord(record, tableSchema)
	if got := mustExtractValue(t, record.Column(0), columnTypes[0]); got != "not-a-uuid" {
		t.Fatalf("Foo extraction = %T %v, want original string", got, got)
	}
	if got := mustExtractValue(t, record.Column(1), columnTypes[1]); got.(mssqldb.UniqueIdentifier).String() != "01234567-89AB-CDEF-0123-456789ABCDEF" {
		t.Fatalf("foo extraction = %T %v, want UUID", got, got)
	}
}

func TestExtractValueRejectsOverflowingUint64(t *testing.T) {
	builder := array.NewUint64Builder(memory.DefaultAllocator)
	builder.Append(uint64(1 << 63))
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	if _, err := extractValue(arr, 0, nil); err == nil {
		t.Fatal("expected uint64 overflow error")
	}
}

func TestExtractValueReturnsNilForNulls(t *testing.T) {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.AppendNull()
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	got := mustExtractValue(t, arr, nil)
	if got != nil {
		t.Fatalf("expected nil, got %T %v", got, got)
	}
}

func mustExtractValue(t *testing.T, arr arrow.Array, col *schema.Column) interface{} {
	t.Helper()
	value, err := extractValue(arr, 0, col)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("SQL does not contain %q:\n%s", want, got)
	}
}
