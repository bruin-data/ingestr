package trino

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

func TestGetTableSchema(t *testing.T) {
	t.Parallel()

	t.Run("missing table returns nil", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}))

		got, err := d.GetTableSchema(t.Context(), "missing_table")
		require.NoError(t, err)
		require.Nil(t, got)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("maps all Trino column types", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		rows := sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
			AddRow("flag", "boolean", "NO").
			AddRow("tiny", "tinyint", "YES").
			AddRow("small", "smallint", "YES").
			AddRow("num", "integer", "YES").
			AddRow("big", "bigint", "NO").
			AddRow("flt", "real", "YES").
			AddRow("dbl", "double", "YES").
			AddRow("amt", "decimal(18, 4)", "YES").
			AddRow("code", "varchar(64)", "YES").
			AddRow("lsn", "varchar", "NO").
			AddRow("raw", "varbinary", "YES").
			AddRow("dt", "date", "YES").
			AddRow("tm", "time(6)", "YES").
			AddRow("ts", "timestamp(6)", "YES").
			AddRow("tstz", "timestamp(6) with time zone", "NO").
			AddRow("doc", "variant", "YES").
			AddRow("uid", "uuid", "YES").
			AddRow("tags", "array(varchar)", "YES").
			AddRow("nums", "array(decimal(10, 2))", "YES")

		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).WillReturnRows(rows)

		got, err := d.GetTableSchema(t.Context(), "events")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, "analytics", got.Schema)
		require.Equal(t, "events", got.Name)
		require.Len(t, got.Columns, 19)

		require.Equal(t, schema.Column{Name: "flag", DataType: schema.TypeBoolean, Nullable: false}, got.Columns[0])
		require.Equal(t, schema.Column{Name: "tiny", DataType: schema.TypeInt8, Nullable: true}, got.Columns[1])
		require.Equal(t, schema.Column{Name: "small", DataType: schema.TypeInt16, Nullable: true}, got.Columns[2])
		require.Equal(t, schema.Column{Name: "num", DataType: schema.TypeInt32, Nullable: true}, got.Columns[3])
		require.Equal(t, schema.Column{Name: "big", DataType: schema.TypeInt64, Nullable: false}, got.Columns[4])
		require.Equal(t, schema.Column{Name: "flt", DataType: schema.TypeFloat32, Nullable: true}, got.Columns[5])
		require.Equal(t, schema.Column{Name: "dbl", DataType: schema.TypeFloat64, Nullable: true}, got.Columns[6])
		require.Equal(t, schema.Column{Name: "amt", DataType: schema.TypeDecimal, Precision: 18, Scale: 4, Nullable: true}, got.Columns[7])
		require.Equal(t, schema.Column{Name: "code", DataType: schema.TypeString, MaxLength: 64, Nullable: true}, got.Columns[8])
		require.Equal(t, schema.Column{Name: "lsn", DataType: schema.TypeString, Nullable: false}, got.Columns[9])
		require.Equal(t, schema.Column{Name: "raw", DataType: schema.TypeBinary, Nullable: true}, got.Columns[10])
		require.Equal(t, schema.Column{Name: "dt", DataType: schema.TypeDate, Nullable: true}, got.Columns[11])
		require.Equal(t, schema.Column{Name: "tm", DataType: schema.TypeTime, Nullable: true}, got.Columns[12])
		require.Equal(t, schema.Column{Name: "ts", DataType: schema.TypeTimestamp, Nullable: true}, got.Columns[13])
		require.Equal(t, schema.Column{Name: "tstz", DataType: schema.TypeTimestampTZ, Nullable: false}, got.Columns[14])
		require.Equal(t, schema.Column{Name: "doc", DataType: schema.TypeJSON, Nullable: true}, got.Columns[15])
		require.Equal(t, schema.Column{Name: "uid", DataType: schema.TypeUUID, Nullable: true}, got.Columns[16])
		require.Equal(t, schema.Column{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, Nullable: true}, got.Columns[17])
		require.Equal(t, schema.Column{Name: "nums", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true}, got.Columns[18])
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCanonicalCDCTargetAndCatalog(t *testing.T) {
	t.Parallel()

	d := &TrinoDestination{catalog: "Iceberg_Prod", schema: "Raw"}
	require.Equal(t, "Iceberg_Prod", d.ManagedCDCStateCatalog())

	got, err := d.CanonicalCDCTarget(t.Context(), "Orders")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKey("iceberg_prod", "raw", "orders"), got)

	got2, err := d.CanonicalCDCTarget(t.Context(), "Analytics.Orders")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKey("iceberg_prod", "analytics", "orders"), got2)

	got3, err := d.CanonicalCDCTarget(t.Context(), "OtherCat.Analytics.Orders")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKey("othercat", "analytics", "orders"), got3)
}

func TestValidateManagedCDCState(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, NewTrinoDestination().ValidateManagedCDCState(), "not connected")

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.ErrorContains(t, (&TrinoDestination{db: db}).ValidateManagedCDCState(), "catalog")
	require.NoError(t, (&TrinoDestination{db: db, catalog: "iceberg"}).ValidateManagedCDCState())
}

func TestEnsureCDCStatePositionColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dataType  string
		noRows    bool
		wantError string
	}{
		{name: "missing table", noRows: true},
		{name: "unbounded varchar", dataType: "varchar"},
		{name: "wide varchar", dataType: "varchar(2000000)"},
		{name: "narrow varchar", dataType: "varchar(255)", wantError: "bounded type varchar(255)"},
		{name: "non string", dataType: "bigint", wantError: "must be VARCHAR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			d := &TrinoDestination{db: db, catalog: "iceberg", schema: "_bruin_staging"}

			if tt.noRows {
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).WillReturnError(sql.ErrNoRows)
			} else {
				mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
					WillReturnRows(sqlmock.NewRows([]string{"data_type"}).AddRow(tt.dataType))
			}

			err = d.EnsureCDCStatePositionColumn(t.Context(), "_bruin_staging._ingestr_cdc_state")
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCDCStateReaderFenceAndPruner(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &TrinoDestination{db: db, catalog: "iceberg", schema: "_bruin_staging"}

	// Missing table returns nil/empty
	mock.ExpectQuery(`SELECT DISTINCT "event_id"`).
		WithArgs("conn-1").
		WillReturnError(errors.New("Table 'iceberg._bruin_staging._ingestr_cdc_state' does not exist"))
	entries, err := d.LoadCDCState(t.Context(), "_bruin_staging._ingestr_cdc_state", "conn-1")
	require.NoError(t, err)
	require.Nil(t, entries)

	// Populated state entries
	stateRows := sqlmock.NewRows([]string{"event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn"}).
		AddRow("ev-1", "public.orders", "orders", "run", int64(2), "completed", "0001")
	mock.ExpectQuery(`SELECT DISTINCT "event_id"`).WithArgs("conn-1").WillReturnRows(stateRows)
	entries, err = d.LoadCDCState(t.Context(), "_bruin_staging._ingestr_cdc_state", "conn-1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, destination.CDCStateEntry{
		EventID: "ev-1", SourceTable: "public.orders", DestinationTable: "orders",
		StateKind: "run", Generation: 2, Status: "completed", Position: "0001",
	}, entries[0])

	// Fence loading
	fenceRows := sqlmock.NewRows([]string{"event_id", "state_generation"}).
		AddRow("run-a", int64(5)).
		AddRow("run-b", int64(5))
	mock.ExpectQuery(`SELECT DISTINCT "event_id", "state_generation"`).
		WithArgs("conn-1", "conn-1").
		WillReturnRows(fenceRows)
	fence, err := d.LoadCDCStateFence(t.Context(), "_bruin_staging._ingestr_cdc_state", "conn-1")
	require.NoError(t, err)
	require.Equal(t, destination.CDCStateFence{Generation: 5, RunEventIDs: []string{"run-a", "run-b"}}, fence)

	// Chunked delete (>500 IDs triggers 2 DELETE statements)
	require.Equal(t, 1000, d.CDCStatePruneBatchSize())
	ids := make([]string, 502)
	for i := range ids {
		ids[i] = fmt.Sprintf("ev-%d", i)
	}
	mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."_ingestr_cdc_state" WHERE "connector_id" = \? AND "event_id" IN`).
		WillReturnResult(sqlmock.NewResult(0, 500))
	mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."_ingestr_cdc_state" WHERE "connector_id" = \? AND "event_id" IN`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	require.NoError(t, d.DeleteCDCStateEvents(t.Context(), "_bruin_staging._ingestr_cdc_state", "conn-1", ids))

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetMaxCDCLSN(t *testing.T) {
	t.Parallel()

	t.Run("missing table returns empty", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		got, err := d.GetMaxCDCLSN(t.Context(), "orders")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("non-CDC table without _cdc_lsn returns empty", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", false, "")
		got, err := d.GetMaxCDCLSN(t.Context(), "orders")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("CDC table returns max LSN", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, "")
		mock.ExpectQuery(`SELECT MAX\("_cdc_lsn"\) FROM "iceberg"."analytics"."orders"`).
			WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow("00000012/00000400"))

		got, err := d.GetMaxCDCLSN(t.Context(), "orders")
		require.NoError(t, err)
		require.Equal(t, "00000012/00000400", got)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCDCTargetIncarnationAndEnsure(t *testing.T) {
	t.Parallel()

	t.Run("non-CDC table rejects EnsureCDCTargetIncarnation", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", false, "")
		inc, exists, err := d.CDCTargetIncarnation(t.Context(), "orders")
		require.NoError(t, err)
		require.True(t, exists)
		require.Empty(t, inc)

		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", false, "")
		_, exists, err = d.EnsureCDCTargetIncarnation(t.Context(), "orders")
		require.ErrorContains(t, err, `missing managed column "_cdc_lsn"`)
		require.True(t, exists)
	})

	t.Run("EnsureCDCTargetIncarnation stamps missing marker and CDCTargetIncarnation reads it", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, "spoofed comment")
		mock.ExpectExec(`COMMENT ON TABLE "iceberg"."analytics"."orders" IS '__ingestr_cdc_incarnation=`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`COMMENT ON COLUMN "iceberg"."analytics"."orders"."_cdc_lsn" IS '__ingestr_cdc_incarnation=`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		inc, exists, err := d.EnsureCDCTargetIncarnation(t.Context(), "orders")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotEmpty(t, inc)

		marker := "abcdef0123456789abcdef0123456789"
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		got, exists, err := d.CDCTargetIncarnation(t.Context(), "orders")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, trinoTableIncarnation("iceberg", "analytics", "orders", marker), got)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("legacy column-only marker is migrated to table comment without changing incarnation", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		marker := "abcdef0123456789abcdef0123456789"
		expectIncarnationLookupWithComments(mock, "iceberg", "analytics", "orders", true, "", trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`COMMENT ON TABLE "iceberg"."analytics"."orders" IS '` + trinoIncarnationCommentPrefix + marker + `'`)).
			WillReturnResult(sqlmock.NewResult(0, 0))

		got, exists, err := d.EnsureCDCTargetIncarnation(t.Context(), "orders")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, trinoTableIncarnation("iceberg", "analytics", "orders", marker), got)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClaimCDCTargetAndLatePrepare(t *testing.T) {
	t.Parallel()

	t.Run("ClaimCDCTarget detects conflict from concurrent owner", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		target, err := d.CanonicalCDCTarget(t.Context(), "analytics.orders")
		require.NoError(t, err)
		markerName := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
		claim2 := destination.CDCTargetClaim{
			DestinationTable: "analytics.orders",
			ConnectorID:      "conn-2",
			SourceTable:      "public.orders",
		}
		owner2, err := claim2.OwnerID()
		require.NoError(t, err)
		owner1, err := (destination.CDCTargetClaim{ConnectorID: "conn-1", SourceTable: "public.orders"}).OwnerID()
		require.NoError(t, err)

		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `" AS SELECT CAST\('` + owner2 + `' AS VARCHAR\) AS owner_id`).
			WillReturnError(errors.New("Table already exists"))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(owner1))

		err = d.ClaimCDCTarget(t.Context(), "_bruin_staging._ingestr_cdc_targets", claim2)
		require.ErrorContains(t, err, "already claimed by CDC connector")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ClaimCDCTarget treats Nessie content-ID CTAS race as already exists", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		target, err := d.CanonicalCDCTarget(t.Context(), "analytics.orders")
		require.NoError(t, err)
		markerName := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
		claim2 := destination.CDCTargetClaim{
			DestinationTable: "analytics.orders",
			ConnectorID:      "conn-2",
			SourceTable:      "public.orders",
		}
		owner2, err := claim2.OwnerID()
		require.NoError(t, err)
		owner1, err := (destination.CDCTargetClaim{ConnectorID: "conn-1", SourceTable: "public.orders"}).OwnerID()
		require.NoError(t, err)

		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `" AS SELECT CAST\('` + owner2 + `' AS VARCHAR\) AS owner_id`).
			WillReturnError(errors.New("INTERNAL_ERROR: Bad Request (HTTP/400): New value to update existing key '_bruin_staging." + markerName + "' has no content ID"))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(owner1))

		err = d.ClaimCDCTarget(t.Context(), "_bruin_staging._ingestr_cdc_targets", claim2)
		require.ErrorContains(t, err, "already claimed by CDC connector")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ClaimCDCTarget retries on Nessie commit conflict and succeeds", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		target, err := d.CanonicalCDCTarget(t.Context(), "analytics.orders")
		require.NoError(t, err)
		markerName := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
		claim := destination.CDCTargetClaim{
			DestinationTable: "analytics.orders",
			ConnectorID:      "conn-1",
			SourceTable:      "public.orders",
		}
		owner, err := claim.OwnerID()
		require.NoError(t, err)

		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnError(errors.New("EXTERNAL: Failed to commit the transaction during insert: Cannot commit: ref hash is out of date"))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnError(errors.New("Table does not exist"))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(owner))
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt"}).AddRow(int64(0)))
		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnError(errors.New("Cannot commit: ref hash is out of date"))
		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, d.ClaimCDCTarget(t.Context(), "_bruin_staging._ingestr_cdc_targets", claim))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ClaimAndPrepareEmptyCDCTarget creates target atomically and cleans up on failure", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		target, err := d.CanonicalCDCTarget(t.Context(), "analytics.orders")
		require.NoError(t, err)
		markerName := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
		claim := destination.CDCTargetClaim{
			DestinationTable: "analytics.orders",
			ConnectorID:      "conn-1",
			SourceTable:      "public.orders",
		}
		owner, err := claim.OwnerID()
		require.NoError(t, err)

		// Target does not exist yet
		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		// Claim didn't exist before
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnError(errors.New("Table does not exist"))
		// ClaimCDCTarget succeeds
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `"`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(owner))
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt"}).AddRow(int64(0)))
		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		// Target creation fails -> triggers rollback cleanup
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."analytics"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."analytics"."orders"`).WillReturnError(errors.New("create failed"))
		mock.ExpectExec(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."` + markerName + `"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DELETE FROM "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).WillReturnResult(sqlmock.NewResult(0, 1))

		cdcSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}}
		_, err = d.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging._ingestr_cdc_targets", claim, destination.PrepareOptions{
			Table:  "analytics.orders",
			Schema: cdcSchema,
		})
		require.ErrorContains(t, err, "create failed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ClaimAndPrepareEmptyCDCTarget succeeds", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		target, err := d.CanonicalCDCTarget(t.Context(), "analytics.orders")
		require.NoError(t, err)
		markerName := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
		claim := destination.CDCTargetClaim{
			DestinationTable: "analytics.orders",
			ConnectorID:      "conn-1",
			SourceTable:      "public.orders",
		}
		owner, err := claim.OwnerID()
		require.NoError(t, err)

		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnError(errors.New("Table does not exist"))
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + markerName + `"`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT MAX\(owner_id\) FROM "iceberg"."_bruin_staging"."` + markerName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(owner))
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnRows(sqlmock.NewRows([]string{"cnt"}).AddRow(int64(0)))
		mock.ExpectExec(`INSERT INTO "iceberg"."_bruin_staging"."_ingestr_cdc_targets"`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."analytics"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."analytics"."orders"`).WillReturnResult(sqlmock.NewResult(0, 0))
		marker := "abcdef0123456789abcdef0123456789"
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)

		cdcSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}}
		inc, err := d.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging._ingestr_cdc_targets", claim, destination.PrepareOptions{
			Table:  "analytics.orders",
			Schema: cdcSchema,
		})
		require.NoError(t, err)
		require.Equal(t, trinoTableIncarnation("iceberg", "analytics", "orders", marker), inc)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestConditionalSwapAndTruncate(t *testing.T) {
	t.Parallel()

	t.Run("CDCConditionalSwapIncarnations and fenced SwapTable use inline-guarded MERGE INTO without CREATE OR REPLACE", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok1"),
		}

		oldMarker := "11111111111111111111111111111111"
		stgMarker := "22222222222222222222222222222222"

		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+oldMarker)

		stgInc, resInc, err := d.CDCConditionalSwapIncarnations(t.Context(), "analytics.orders", "_bruin_staging.orders_stg")
		require.NoError(t, err)
		targetInc := trinoTableIncarnation("iceberg", "analytics", "orders", oldMarker)
		require.Equal(t, trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker), stgInc)
		require.Equal(t, targetInc, resInc)

		targetGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", oldMarker)
		stgGuard := trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		combinedGuard := targetGuard + " AND " + stgGuard

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok1")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+oldMarker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").
				AddRow("_cdc_lsn", "varchar", "YES").
				AddRow("_cdc_deleted", "boolean", "YES").
				AddRow("_cdc_synced_at", "timestamp(6) with time zone", "YES"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").
				AddRow("_cdc_lsn", "varchar", "YES").
				AddRow("_cdc_deleted", "boolean", "YES").
				AddRow("_cdc_synced_at", "timestamp(6) with time zone", "YES").
				AddRow("_cdc_unchanged_cols", "varchar", "YES"))

		expectedMergeSQL := buildConditionalSwapMergeSQL(
			`"iceberg"."analytics"."orders"`,
			`"iceberg"."_bruin_staging"."orders_stg"`,
			[]trinoSwapMergeCol{{name: "id"}, {name: "_cdc_lsn"}, {name: "_cdc_deleted"}, {name: "_cdc_synced_at"}},
			[]string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			combinedGuard,
		)
		mock.ExpectExec(regexp.QuoteMeta(expectedMergeSQL)).WillReturnResult(sqlmock.NewResult(0, 2))
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+oldMarker)
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."orders_stg"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok1")

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  resInc,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
				{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
				{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
				{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
			}},
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("fenced SwapTable detects outside replacement after precheck without overwriting comment or dropping staging", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		precheckHookCalled := false
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-race"),
			afterSwapPrecheck: func() {
				precheckHookCalled = true
			},
		}

		oldMarker := "11111111111111111111111111111111"
		stgMarker := "22222222222222222222222222222222"
		targetInc := trinoTableIncarnation("iceberg", "analytics", "orders", oldMarker)
		stgInc := trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		combinedGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", oldMarker) +
			" AND " + trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-race")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+oldMarker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES"))

		expectedMergeSQL := buildConditionalSwapMergeSQL(
			`"iceberg"."analytics"."orders"`,
			`"iceberg"."_bruin_staging"."orders_stg"`,
			[]trinoSwapMergeCol{{name: "id"}},
			[]string{"id"},
			combinedGuard,
		)
		mock.ExpectExec(regexp.QuoteMeta(expectedMergeSQL)).
			WillReturnError(errors.New("INVALID_CAST_ARGUMENT: Cannot cast 'INGESTR_CDC_INCARNATION_CHANGED:deadbeef' to BOOLEAN"))
		// Outside replacement has its own comment (not oldMarker), so error check detects replacement without dropping staging.
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, "external-replacement")
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-race")

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  targetInc,
			Schema:                        &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.True(t, precheckHookCalled)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("fenced SwapTable on absent target uses CREATE TABLE without OR REPLACE and rejects outside creation", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-absent", "swaptok-preexist"),
		}

		stgMarker := "33333333333333333333333333333333"
		// Existing unmarked target is rejected by CDCConditionalSwapIncarnations.
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", false, "")
		_, _, err = d.CDCConditionalSwapIncarnations(t.Context(), "analytics.orders", "_bruin_staging.orders_stg")
		require.ErrorContains(t, err, "has no durable physical incarnation")

		// Absent target plans result incarnation from staging marker.
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).WillReturnError(sql.ErrNoRows)

		stgInc, resInc, err := d.CDCConditionalSwapIncarnations(t.Context(), "analytics.orders", "_bruin_staging.orders_stg")
		require.NoError(t, err)
		require.Equal(t, trinoTableIncarnation("iceberg", "analytics", "orders", stgMarker), resInc)

		// Case 1: target created externally after precheck -> CREATE TABLE (no OR REPLACE) fails with already exists.
		stgGuard := trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-absent")
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
		mock.ExpectExec(regexp.QuoteMeta(expectedCreateSQL)).WillReturnError(errors.New("Table 'iceberg.analytics.orders' already exists"))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-absent")

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  resInc,
			Schema:                        &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		// Case 2: target already exists at precheck -> rejected before CREATE TABLE.
		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-preexist")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", false, "")
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-preexist")

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

	t.Run("fenced SwapTable aligns additive target schema differences before guarded MERGE", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("swaptok-evolve"),
		}

		marker := "55555555555555555555555555555555"
		stgMarker := "66666666666666666666666666666666"
		targetInc := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
		stgInc := trinoTableIncarnation("iceberg", "_bruin_staging", "orders_stg", stgMarker)
		combinedGuard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker) +
			" AND " + trinoIncarnationExistsPredicate("iceberg", "_bruin_staging", "orders_stg", stgMarker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "swaptok-evolve")
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "integer", "YES").
				AddRow("legacy_req", "varchar", "NO"))
		mock.ExpectQuery(`FROM "iceberg".information_schema.columns`).
			WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable"}).
				AddRow("id", "bigint", "YES").
				AddRow("tier", "varchar", "YES"))

		// 1. Widen id integer -> bigint
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "iceberg"."analytics"."orders" ALTER COLUMN "id" SET DATA TYPE bigint`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)

		// 2. Add new column tier varchar
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "iceberg"."analytics"."orders" ADD COLUMN "tier" varchar`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)

		// 3. Relax NOT NULL on removed column legacy_req
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE "iceberg"."analytics"."orders" ALTER COLUMN "legacy_req" DROP NOT NULL`)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)

		expectedMergeSQL := buildConditionalSwapMergeSQL(
			`"iceberg"."analytics"."orders"`,
			`"iceberg"."_bruin_staging"."orders_stg"`,
			[]trinoSwapMergeCol{{name: "id"}, {name: "tier"}},
			[]string{"id", "legacy_req", "tier"},
			combinedGuard,
		)
		mock.ExpectExec(regexp.QuoteMeta(expectedMergeSQL)).WillReturnResult(sqlmock.NewResult(0, 1))
		expectIncarnationLookup(mock, "iceberg", "_bruin_staging", "orders_stg", true, trinoIncarnationCommentPrefix+stgMarker)
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`DROP TABLE IF EXISTS "iceberg"."_bruin_staging"."orders_stg"`)).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "swaptok-evolve")

		require.NoError(t, d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:                   "analytics.orders",
			StagingTable:                  "_bruin_staging.orders_stg",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  targetInc,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "tier", DataType: schema.TypeString},
			}},
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SwapTable failure never renames existing target away", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."analytics"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`CREATE OR REPLACE TABLE "iceberg"."analytics"."orders"` + "\n" + `AS SELECT * FROM "iceberg"."_bruin_staging"."orders_stg"`)).
			WillReturnError(errors.New("catalog commit failed"))

		err = d.SwapTable(t.Context(), destination.SwapOptions{
			TargetTable:  "analytics.orders",
			StagingTable: "_bruin_staging.orders_stg",
		})
		require.ErrorContains(t, err, "catalog commit failed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("TruncateCDCTableIfIncarnation uses inline incarnation guard under target lock", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("trunctok1"),
		}

		marker := "44444444444444444444444444444444"
		inc := trinoTableIncarnation("iceberg", "analytics", "orders", marker)
		guard := trinoIncarnationExistsPredicate("iceberg", "analytics", "orders", marker)

		expectTargetLockAcquire(mock, "iceberg", "analytics", "orders", "trunctok1")
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "iceberg"."analytics"."orders" WHERE ` + guard)).
			WillReturnResult(sqlmock.NewResult(0, 5))
		expectIncarnationLookup(mock, "iceberg", "analytics", "orders", true, trinoIncarnationCommentPrefix+marker)
		expectTargetLockRelease(mock, "iceberg", "analytics", "orders", "trunctok1")

		require.NoError(t, d.TruncateCDCTableIfIncarnation(t.Context(), "analytics.orders", inc))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("TruncateCDCTableIfIncarnation blocks on contended target lock", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		d := &TrinoDestination{
			db:            db,
			catalog:       "iceberg",
			schema:        "analytics",
			newLeaseToken: deterministicTokenGen("contendedtok"),
		}

		lockName := targetLockTableName("iceberg", "analytics", "orders")
		mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "iceberg"."_bruin_staging"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`CREATE TABLE "iceberg"."_bruin_staging"."` + lockName + `"`).
			WillReturnError(errors.New("Table already exists"))
		mock.ExpectQuery(`FROM "iceberg"."_bruin_staging"."` + lockName + `"`).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "age"}).AddRow("other-holder", int64(1)))

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		err = d.TruncateCDCTableIfIncarnation(ctx, "analytics.orders", trinoTableIncarnation("iceberg", "analytics", "orders", "44444444444444444444444444444444"))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestListStagingTables(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

	mock.ExpectQuery(`FROM "iceberg".information_schema.tables`).
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}).
			AddRow("orders_stream_1").
			AddRow("orders_stream_2").
			AddRow("__ingestr_cdc_batch_journal").
			AddRow("__ingestr_cdc_bak_abc_0").
			AddRow("__ingestr_cdc_shd_abc_0").
			AddRow("other_table"))

	tables, err := d.ListStagingTables(t.Context(), "iceberg._bruin_staging", "")
	require.NoError(t, err)
	require.Equal(t, []string{"orders_stream_1", "orders_stream_2", "other_table"}, tables)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStrategyAndPipelineCDCInterfaceCompliance(t *testing.T) {
	t.Parallel()

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	d := &TrinoDestination{db: db, catalog: "iceberg", schema: "analytics"}

	// Managed CDC state & Postgres/MySQL/MariaDB/MSSQL/MongoDB/Vitess/PlanetScale readiness
	var dest destination.Destination = d
	_, ok := dest.(destination.CDCStateReader)
	require.True(t, ok)
	_, ok = dest.(destination.CDCStateFenceReader)
	require.True(t, ok)
	_, ok = dest.(destination.CDCStatePruner)
	require.True(t, ok)
	_, ok = dest.(destination.TruncateCapable)
	require.True(t, ok)
	_, ok = dest.(destination.CDCTruncateCapable)
	require.True(t, ok)
	_, ok = dest.(destination.CDCTargetClaimer)
	require.True(t, ok)
	_, ok = dest.(destination.CDCTargetIncarnationProvider)
	require.True(t, ok)
	_, ok = dest.(destination.CDCTargetIncarnationInitializer)
	require.True(t, ok)
	_, ok = dest.(destination.CDCTargetIdentityProvider)
	require.True(t, ok)
	_, ok = dest.(destination.CDCResumeProvider)
	require.True(t, ok)
	_, ok = dest.(destination.CDCMergeAware)
	require.True(t, ok)
	_, ok = dest.(destination.CDCHardDeleteCapable)
	require.True(t, ok)
	_, ok = dest.(destination.CDCUnchangedColsAware)
	require.True(t, ok)
	_, ok = dest.(destination.CDCConditionalMergeCapable)
	require.True(t, ok)
	_, ok = dest.(destination.CDCConditionalTruncater)
	require.True(t, ok)
	_, ok = dest.(destination.ManagedCDCRunLeaser)
	require.True(t, ok)
	_, ok = dest.(destination.CDCMultiTableAtomicMerger)
	require.True(t, ok)
	_, ok = dest.(destination.CDCLateTargetClaimPreparer)
	require.True(t, ok)
	_, ok = dest.(destination.CDCConditionalSwapCapable)
	require.True(t, ok)
	_, ok = dest.(destination.CDCConditionalSwapPlanner)
	require.True(t, ok)
	_, ok = dest.(destination.ManagedCDCStateValidator)
	require.True(t, ok)
	_, ok = dest.(destination.SerializedCDCRunsRequired)
	require.True(t, ok)

	stateMgr, err := strategy.NewCDCStateManager(dest, "conn-1", "", "")
	require.NoError(t, err)
	require.NotNil(t, stateMgr)

	_ = source.ErrConnectorLeaseLost
}
