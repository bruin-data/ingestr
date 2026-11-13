package fabric

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFabricCDCCapabilitiesAndStrategyValidators(t *testing.T) {
	dest := NewFabricDestination()

	assert.True(t, dest.SupportsCDCMerge())
	assert.True(t, dest.SupportsCDCHardDelete())
	assert.True(t, dest.SupportsCDCUnchangedCols())
	assert.True(t, dest.SupportsCDCConditionalMerge())
	assert.True(t, dest.SupportsCDCConditionalSwap())
	assert.True(t, dest.SupportsAtomicSwap())
	assert.True(t, dest.RequiresSerializedCDCRuns())

	policy := dest.ReplaceStagingPolicy()
	assert.Equal(t, destination.ReplaceStagingTargetSchema, policy.DefaultPlacement)
	assert.Equal(t, "dbo", policy.DefaultTargetSchema)
	assert.Equal(t, cdcLeaseSchema, policy.DefaultManagedSchema)

	require.NoError(t, strategy.ValidateCDCChunkedSnapshotDestination(dest))
	require.NoError(t, strategy.ValidateCDCSnapshotSpoolDestination(dest))

	manager, err := strategy.NewCDCStateManager(dest, "connector-1", "", "")
	require.NoError(t, err)
	require.NotNil(t, manager)
}

func TestBuildCreateTableSQLBoundsPrimaryKeyStringAndBinary(t *testing.T) {
	sqlText := buildCreateTableSQL("dbo.mongo_events", []schema.Column{
		{Name: "_id", DataType: schema.TypeString},
		{Name: "bounded_id", DataType: schema.TypeString, MaxLength: 1500},
		{Name: "oversized_id", DataType: schema.TypeString, MaxLength: 10000},
		{Name: "payload", DataType: schema.TypeString},
		{Name: "raw_key", DataType: schema.TypeBinary},
		{Name: "bounded_raw", DataType: schema.TypeBinary, MaxLength: 2048},
		{Name: "oversized_raw", DataType: schema.TypeBinary, MaxLength: 12000},
		{Name: "blob", DataType: schema.TypeBinary},
	}, []string{"_id", "bounded_id", "oversized_id", "raw_key", "bounded_raw", "oversized_raw"})

	assert.Contains(t, sqlText, "[_id] VARCHAR(8000) NOT NULL")
	assert.Contains(t, sqlText, "[bounded_id] VARCHAR(1500) NOT NULL")
	assert.Contains(t, sqlText, "[oversized_id] VARCHAR(8000) NOT NULL")
	assert.Contains(t, sqlText, "[raw_key] VARBINARY(8000) NOT NULL")
	assert.Contains(t, sqlText, "[bounded_raw] VARBINARY(2048) NOT NULL")
	assert.Contains(t, sqlText, "[oversized_raw] VARBINARY(8000) NOT NULL")
	assert.Contains(t, sqlText, "[payload] VARCHAR(MAX)")
	assert.Contains(t, sqlText, "[blob] VARBINARY(MAX)")
	assert.NotContains(t, sqlText, "VARCHAR(900)")
	assert.NotContains(t, sqlText, "VARBINARY(900)")
	assert.Contains(t, sqlText, "PRIMARY KEY NONCLUSTERED ([_id], [bounded_id], [oversized_id], [raw_key], [bounded_raw], [oversized_raw]) NOT ENFORCED")
}

func TestBuildCreateTableSQLWithDeferredPrimaryKeys(t *testing.T) {
	sqlText := buildCreateTableSQLWithOptions("dbo.events", []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "slug", DataType: schema.TypeString},
		{Name: "raw_key", DataType: schema.TypeBinary},
		{Name: "name", DataType: schema.TypeString},
	}, nil, []string{"id", "slug", "raw_key"}, false)

	assert.Contains(t, sqlText, "[id] BIGINT NOT NULL")
	assert.Contains(t, sqlText, "[slug] VARCHAR(8000) NOT NULL")
	assert.Contains(t, sqlText, "[raw_key] VARBINARY(8000) NOT NULL")
	assert.Contains(t, sqlText, "[name] VARCHAR(MAX)")
	assert.NotContains(t, sqlText, "ADD CONSTRAINT")
}

func TestCDCLongStringAndBinaryPrimaryKeys(t *testing.T) {
	longStringKey := strings.Repeat("k", 1200)
	longBinaryKey := []byte(strings.Repeat("b", 1200))
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "doc_id", DataType: schema.TypeString},
		{Name: "bin_id", DataType: schema.TypeBinary},
		{Name: "payload", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
	}}

	t.Run("CDC ClaimAndPrepareEmptyCDCTarget and fenced WriteParallel accept >900-byte keys", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		canonical := destination.CDCTargetKeyDigest("", "dbo", "long_pk_events")
		claim := destination.CDCTargetClaim{
			ConnectorID:      "pg-cdc-long-pk",
			DestinationTable: "dbo.long_pk_events",
			SourceTable:      "public.long_pk_events",
		}
		ownerID, err := claim.OwnerID()
		require.NoError(t, err)

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`CREATE VIEW \[_bruin_staging\]\.\[cdc_claim_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT \[owner_id\] FROM \[_bruin_staging\]\.\[cdc_claim_`).WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(ownerID))
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`INSERT INTO \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical, ownerID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectedCreateSQL := buildCreateTableSQLWithOptions("dbo.long_pk_events", tableSchema.Columns, []string{"doc_id", "bin_id"}, nil, true)
		require.Contains(t, expectedCreateSQL, "[doc_id] VARCHAR(8000) NOT NULL")
		require.Contains(t, expectedCreateSQL, "[bin_id] VARBINARY(8000) NOT NULL")
		require.NotContains(t, expectedCreateSQL, "900")
		mock.ExpectExec(regexp.QuoteMeta(expectedCreateSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(301), "2026-01-01T00:00:00"))

		dest := &FabricDestination{db: db, writeMode: writeModeInsert}
		inc, err := dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
			Table:       "dbo.long_pk_events",
			Schema:      tableSchema,
			PrimaryKeys: []string{"doc_id", "bin_id"},
			CDCMode:     true,
			CDCKeys:     []string{"doc_id", "bin_id"},
		})
		require.NoError(t, err)

		// Merge staging table prepared without PrimaryKeys keeps VARCHAR(MAX)/VARBINARY(MAX).
		stagingCreateSQL := buildCreateTableSQL("dbo.long_pk_events_staging", tableSchema.Columns, nil)
		require.Contains(t, stagingCreateSQL, "[doc_id] VARCHAR(MAX)")
		require.Contains(t, stagingCreateSQL, "[bin_id] VARBINARY(MAX)")
		mock.ExpectExec(regexp.QuoteMeta(stagingCreateSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		require.NoError(t, dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table:   "dbo.long_pk_events_staging",
			Schema:  tableSchema,
			CDCMode: true,
			CDCKeys: []string{"doc_id", "bin_id"},
		}))

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(301), "2026-01-01T00:00:00"))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(301), "2026-01-01T00:00:00"))
		mock.ExpectExec(`INSERT INTO \[dbo\]\.\[long_pk_events\]`).
			WithArgs(longStringKey, longBinaryKey, "hello", "0/16B3748", 0).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(301), "2026-01-01T00:00:00"))
		mock.ExpectCommit()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "long_pk_events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(301), "2026-01-01T00:00:00"))

		mem := memory.NewGoAllocator()
		recSchema := arrow.NewSchema([]arrow.Field{
			{Name: "doc_id", Type: arrow.BinaryTypes.String},
			{Name: "bin_id", Type: arrow.BinaryTypes.Binary},
			{Name: "payload", Type: arrow.BinaryTypes.String},
			{Name: destination.CDCLSNColumn, Type: arrow.BinaryTypes.String},
			{Name: destination.CDCDeletedColumn, Type: arrow.FixedWidthTypes.Boolean},
		}, nil)
		b := array.NewRecordBuilder(mem, recSchema)
		b.Field(0).(*array.StringBuilder).Append(longStringKey)
		b.Field(1).(*array.BinaryBuilder).Append(longBinaryKey)
		b.Field(2).(*array.StringBuilder).Append("hello")
		b.Field(3).(*array.StringBuilder).Append("0/16B3748")
		b.Field(4).(*array.BooleanBuilder).Append(false)
		batch := b.NewRecordBatch()
		b.Release()

		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: batch}
		close(records)

		require.NoError(t, dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.long_pk_events",
			Schema:                 tableSchema,
			CDCExpectedIncarnation: inc,
		}))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPrepareTableRequirePrimaryKeyMatch(t *testing.T) {
	t.Run("matching primary key passes", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		tableSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "val", DataType: schema.TypeString},
		}}
		createSQL := buildCreateTableSQL("dbo.events", tableSchema.Columns, []string{"id"})
		mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.TABLE_CONSTRAINTS`).
			WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("id"))

		dest := &FabricDestination{db: db, database: "wh"}
		err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table:                  "dbo.events",
			Schema:                 tableSchema,
			PrimaryKeys:            []string{"id"},
			CDCMode:                true,
			RequirePrimaryKeyMatch: true,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("mismatched primary key fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.TABLE_CONSTRAINTS`).
			WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("other_id"))

		dest := &FabricDestination{db: db, database: "wh"}
		err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table:                  "dbo.events",
			PrimaryKeys:            []string{"id"},
			RequirePrimaryKeyMatch: true,
		})
		require.ErrorContains(t, err, "must have primary key [id]; found [other_id]")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("already exists error ignored when PK match not required", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
		mock.ExpectExec("CREATE TABLE").WillReturnError(mssqldb.Error{Number: 2714, Message: "There is already an object named 'events'"})

		dest := &FabricDestination{db: db}
		err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
			Table:       "dbo.events",
			Schema:      tableSchema,
			PrimaryKeys: []string{"id"},
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetTableSchemaIncludesPrimaryKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "NUMERIC_PRECISION", "NUMERIC_SCALE", "CHARACTER_MAXIMUM_LENGTH", "IS_PRIMARY_KEY"}).
		AddRow("id", "bigint", "NO", 19, 0, nil, true).
		AddRow("name", "varchar", "YES", nil, nil, -1, false)
	mock.ExpectQuery(`FROM INFORMATION_SCHEMA\.COLUMNS`).WithArgs("dbo", "events").WillReturnRows(rows)

	dest := &FabricDestination{db: db}
	got, err := dest.GetTableSchema(t.Context(), "dbo.events")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []string{"id"}, got.PrimaryKeys)
	assert.True(t, got.Columns[0].IsPrimaryKey)
	assert.False(t, got.Columns[1].IsPrimaryKey)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildFabricCDCMergeSQLSoftDeleteAndUnchangedCols(t *testing.T) {
	sqlText := buildFabricMergeSQL(destination.MergeOptions{
		TargetTable:  "dbo.users",
		StagingTable: "_bruin_staging.users_tmp",
		PrimaryKeys:  []string{"id"},
		Columns: []string{
			"id",
			"name",
			"bio",
			destination.CDCLSNColumn,
			destination.CDCDeletedColumn,
			destination.CDCSyncedAtColumn,
			destination.CDCUnchangedColsColumn,
		},
		IncrementalPredicate: "target.[_cdc_lsn] >= '00000001'",
	})

	assert.Contains(t, sqlText, "ORDER BY [_cdc_lsn] DESC, [_cdc_deleted] DESC")
	assert.Contains(t, sqlText, "WHERE [_cdc_deleted] = 0")
	assert.Contains(t, sqlText, "[__ingestr_has_active]")
	assert.Contains(t, sqlText, "[__ingestr_active_lsn]")
	assert.Contains(t, sqlText, "OPENJSON(COALESCE(source.[_cdc_unchanged_cols], '[]'))")
	assert.Contains(t, sqlText, "COLLATE Latin1_General_100_BIN2_UTF8 = 'name' COLLATE Latin1_General_100_BIN2_UTF8")
	assert.Contains(t, sqlText, "(target.[_cdc_lsn] >= '00000001') AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] > target.[_cdc_lsn]")
	assert.NotContains(t, sqlText, "target.[_cdc_unchanged_cols] =")
	assert.NotContains(t, sqlText, "INSERT ([id], [name], [bio], [_cdc_lsn], [_cdc_deleted], [_cdc_synced_at], [_cdc_unchanged_cols])")
	assert.Contains(t, sqlText, "INSERT ([id], [name], [bio], [_cdc_lsn], [_cdc_deleted], [_cdc_synced_at])")
}

func TestBuildFabricCDCMergeSQLHardDelete(t *testing.T) {
	sqlText := buildFabricMergeSQL(destination.MergeOptions{
		TargetTable:   "dbo.users",
		StagingTable:  "_bruin_staging.users_tmp",
		PrimaryKeys:   []string{"id"},
		CDCHardDelete: true,
		Columns: []string{
			"id",
			"name",
			destination.CDCLSNColumn,
			destination.CDCDeletedColumn,
			destination.CDCSyncedAtColumn,
			destination.CDCUnchangedColsColumn,
		},
		IncrementalPredicate: "target.[tenant_id] = 42",
	})

	assert.Contains(t, sqlText, "ORDER BY [_cdc_lsn] DESC, [_cdc_deleted] DESC")
	assert.Contains(t, sqlText, "WHEN MATCHED AND (target.[tenant_id] = 42) AND source.[_cdc_deleted] = 1 AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] >= target.[_cdc_lsn]) THEN DELETE")
	assert.Contains(t, sqlText, "WHEN MATCHED AND (target.[tenant_id] = 42) AND source.[_cdc_deleted] = 0 AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] > target.[_cdc_lsn]) THEN UPDATE SET")
	assert.Contains(t, sqlText, "OPENJSON(COALESCE(source.[_cdc_unchanged_cols], '[]'))")
	assert.Contains(t, sqlText, "WHEN NOT MATCHED AND source.[_cdc_deleted] = 0 THEN INSERT ([id], [name], [_cdc_lsn], [_cdc_deleted], [_cdc_synced_at])")
	assert.NotContains(t, sqlText, "target.[_cdc_unchanged_cols] =")
}

func TestMergeTableConditionalIncarnation(t *testing.T) {
	expected := destination.CDCTargetKey("wh", "101", "2026-01-01T00:00:00")

	t.Run("commits when incarnation matches before and after", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(101), "2026-01-01T00:00:00"))
		mock.ExpectExec("MERGE").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(101), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		dest := &FabricDestination{db: db, database: "wh"}
		err = dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "dbo.events",
			StagingTable:           "_bruin_staging.events_tmp",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "val", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCExpectedIncarnation: expected,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rolls back when incarnation changed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(202), "2026-01-01T00:00:00"))
		mock.ExpectRollback()

		dest := &FabricDestination{db: db, database: "wh"}
		err = dest.MergeTable(t.Context(), destination.MergeOptions{
			TargetTable:            "dbo.events",
			StagingTable:           "_bruin_staging.events_tmp",
			PrimaryKeys:            []string{"id"},
			Columns:                []string{"id", "val", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
			CDCExpectedIncarnation: expected,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetMaxCDCLSN(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		want    string
		wantErr bool
	}{
		{
			name: "returns max lsn",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT MAX\(\[_cdc_lsn\]\) FROM \[dbo\]\.\[events\]`).
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow("00000001/00000042"))
			},
			want: "00000001/00000042",
		},
		{
			name: "empty table returns empty string",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT MAX\(\[_cdc_lsn\]\)`).
					WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(nil))
			},
		},
		{
			name: "missing table returns empty string",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT MAX\(\[_cdc_lsn\]\)`).
					WillReturnError(errors.New("Invalid object name 'dbo.events'"))
			},
		},
		{
			name: "missing column returns empty string",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT MAX\(\[_cdc_lsn\]\)`).
					WillReturnError(mssqldb.Error{Number: 207, Message: "Invalid column name '_cdc_lsn'"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			tc.setup(mock)
			dest := &FabricDestination{db: db}
			got, err := dest.GetMaxCDCLSN(t.Context(), "dbo.events")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestManagedCDCStateMethods(t *testing.T) {
	t.Run("LoadCDCState and missing table", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM \[_bruin_staging\]\.\[cdc_state\] WHERE \[connector_id\] = @p1`).
			WithArgs("conn-1").
			WillReturnRows(sqlmock.NewRows([]string{"event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn"}).
				AddRow("ev-1", "public.orders", "dbo.orders", "checkpoint", int64(2), "completed", "00000001/00000010"))

		dest := &FabricDestination{db: db}
		entries, err := dest.LoadCDCState(t.Context(), "_bruin_staging.cdc_state", "conn-1")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "ev-1", entries[0].EventID)
		assert.Equal(t, int64(2), entries[0].Generation)

		mock.ExpectQuery(`FROM \[_bruin_staging\]\.\[cdc_state\]`).
			WithArgs("conn-1").
			WillReturnError(errors.New("Invalid object name '_bruin_staging.cdc_state'"))
		entries, err = dest.LoadCDCState(t.Context(), "_bruin_staging.cdc_state", "conn-1")
		require.NoError(t, err)
		assert.Nil(t, entries)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("LoadCDCStateFence", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT DISTINCT \[event_id\], \[state_generation\]`).
			WithArgs("conn-1").
			WillReturnRows(sqlmock.NewRows([]string{"event_id", "state_generation"}).
				AddRow("run-a", int64(3)).
				AddRow("run-b", int64(3)))

		dest := &FabricDestination{db: db}
		fence, err := dest.LoadCDCStateFence(t.Context(), "_bruin_staging.cdc_state", "conn-1")
		require.NoError(t, err)
		assert.Equal(t, int64(3), fence.Generation)
		assert.Equal(t, []string{"run-a", "run-b"}, fence.RunEventIDs)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteCDCStateEvents", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectExec(`DELETE FROM \[_bruin_staging\]\.\[cdc_state\] WHERE \[connector_id\] = @p1 AND \[event_id\] IN \(@p2, @p3\)`).
			WithArgs("conn-1", "ev-1", "ev-2").
			WillReturnResult(sqlmock.NewResult(0, 2))

		dest := &FabricDestination{db: db}
		require.NoError(t, dest.DeleteCDCStateEvents(t.Context(), "_bruin_staging.cdc_state", "conn-1", nil))
		require.NoError(t, dest.DeleteCDCStateEvents(t.Context(), "_bruin_staging.cdc_state", "conn-1", []string{"ev-1", "ev-2"}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("EnsureCDCStatePositionColumn widens bounded column", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT CHARACTER_MAXIMUM_LENGTH FROM INFORMATION_SCHEMA\.COLUMNS`).
			WithArgs("_bruin_staging", "cdc_state").
			WillReturnRows(sqlmock.NewRows([]string{"CHARACTER_MAXIMUM_LENGTH"}).AddRow(64))
		mock.ExpectExec(`ALTER TABLE \[_bruin_staging\]\.\[cdc_state\] ALTER COLUMN \[_cdc_lsn\] VARCHAR\(MAX\) NOT NULL`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		dest := &FabricDestination{db: db}
		require.NoError(t, dest.EnsureCDCStatePositionColumn(t.Context(), "_bruin_staging.cdc_state"))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCanonicalCDCTargetRespectsFabricCollation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\]`).
		WithArgs("wh").
		WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("WH", "Latin1_General_100_BIN2_UTF8"))
	mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\]`).
		WithArgs("wh").
		WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("WH", "Latin1_General_100_CI_AS_KS_WS_SC_UTF8"))

	dest := &FabricDestination{db: db, database: "wh"}
	cs, err := dest.CanonicalCDCTarget(t.Context(), "Sales.Orders")
	require.NoError(t, err)
	assert.Equal(t, destination.CDCTargetKeyDigest("WH", "Sales", "Orders"), cs)

	ci, err := dest.CanonicalCDCTarget(t.Context(), "Sales.Orders")
	require.NoError(t, err)
	assert.Equal(t, destination.CDCTargetKeyDigest("WH", "sales", "orders"), ci)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimCDCTarget(t *testing.T) {
	claim := destination.CDCTargetClaim{
		DestinationTable: "dbo.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "public.orders",
	}
	ownerID, err := claim.OwnerID()
	require.NoError(t, err)
	canonical := destination.CDCTargetKeyDigest("", "dbo", "orders")

	t.Run("first claim creates view marker and inserts target row", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).
			WithArgs(canonical).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`CREATE VIEW \[_bruin_staging\]\.\[cdc_claim_`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT \[owner_id\] FROM \[_bruin_staging\]\.\[cdc_claim_`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(ownerID))
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).
			WithArgs(canonical).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`INSERT INTO \[_bruin_staging\]\.\[cdc_targets\]`).
			WithArgs(canonical, ownerID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		dest := &FabricDestination{db: db}
		require.NoError(t, dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rejects foreign view lock owner on concurrent claim", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).
			WithArgs(canonical).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`CREATE VIEW \[_bruin_staging\]\.\[cdc_claim_`).
			WillReturnError(mssqldb.Error{Number: 2714, Message: "There is already an object named 'cdc_claim_x'"})
		mock.ExpectQuery(`SELECT \[owner_id\] FROM \[_bruin_staging\]\.\[cdc_claim_`).
			WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow("connector-b"))

		dest := &FabricDestination{db: db}
		err = dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim)
		require.ErrorContains(t, err, `already claimed by CDC connector "connector-b"`)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClaimAndPrepareEmptyCDCTarget(t *testing.T) {
	canonical := destination.CDCTargetKeyDigest("", "dbo", "orders")
	claim := destination.CDCTargetClaim{
		DestinationTable: "dbo.orders",
		ConnectorID:      "connector-a",
		SourceTable:      "public.orders",
	}
	ownerID, err := claim.OwnerID()
	require.NoError(t, err)
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}

	t.Run("creates target strictly and returns incarnation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`CREATE VIEW \[_bruin_staging\]\.\[cdc_claim_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT \[owner_id\] FROM \[_bruin_staging\]\.\[cdc_claim_`).WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(ownerID))
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`INSERT INTO \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical, ownerID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		createSQL := buildCreateTableSQLWithOptions("dbo.orders", tableSchema.Columns, []string{"id"}, nil, true)
		mock.ExpectExec(regexp.QuoteMeta(createSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:00:00"))

		dest := &FabricDestination{db: db}
		inc, err := dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
			Table:       "dbo.orders",
			Schema:      tableSchema,
			PrimaryKeys: []string{"id"},
		})
		require.NoError(t, err)
		assert.Equal(t, destination.CDCTargetKey("", "99", "2026-01-01T00:00:00"), inc)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cleans up newly created claim if table creation fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`CREATE VIEW \[_bruin_staging\]\.\[cdc_claim_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT \[owner_id\] FROM \[_bruin_staging\]\.\[cdc_claim_`).WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(ownerID))
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \[connector_id\] FROM \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical).WillReturnError(sql.ErrNoRows)
		mock.ExpectExec(`INSERT INTO \[_bruin_staging\]\.\[cdc_targets\]`).WithArgs(canonical, ownerID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		mock.ExpectExec("CREATE TABLE").WillReturnError(mssqldb.Error{Number: 2714, Message: "already exists"})
		mock.ExpectExec(`DROP VIEW IF EXISTS \[_bruin_staging\]\.\[cdc_claim_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DELETE FROM \[_bruin_staging\]\.\[cdc_targets\] WHERE \[destination_table\] = @p1 AND \[connector_id\] = @p2`).
			WithArgs(canonical, ownerID).WillReturnResult(sqlmock.NewResult(0, 1))

		dest := &FabricDestination{db: db}
		_, err = dest.ClaimAndPrepareEmptyCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim, destination.PrepareOptions{
			Table:       "dbo.orders",
			Schema:      tableSchema,
			PrimaryKeys: []string{"id"},
		})
		require.Error(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestTruncateCDCTableIfIncarnation(t *testing.T) {
	expected := destination.CDCTargetKey("", "55", "2026-01-01T00:00:00")

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(55), "2026-01-01T00:00:00"))
	mock.ExpectExec(`TRUNCATE TABLE \[dbo\]\.\[orders\]`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(55), "2026-01-01T00:00:00"))
	mock.ExpectCommit()

	dest := &FabricDestination{db: db}
	require.NoError(t, dest.TruncateCDCTableIfIncarnation(t.Context(), "dbo.orders", expected))
	require.ErrorContains(t, dest.TruncateCDCTableIfIncarnation(t.Context(), "dbo.orders", ""), "requires an expected incarnation")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMergeCDCTablesAtomically(t *testing.T) {
	incA := destination.CDCTargetKey("", "11", "2026-01-01T00:00:00")
	incB := destination.CDCTargetKey("", "12", "2026-01-01T00:00:00")

	t.Run("truncates and merges multiple tables in one transaction", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(11), "2026-01-01T00:00:00"))
		mock.ExpectExec(`TRUNCATE TABLE \[dbo\]\.\[orders\]`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("MERGE").WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(11), "2026-01-01T00:00:00"))

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "items").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectExec("MERGE").WillReturnResult(sqlmock.NewResult(0, 3))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "items").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		dest := &FabricDestination{db: db}
		err = dest.MergeCDCTablesAtomically(t.Context(), []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            "dbo.orders",
					StagingTable:           "_bruin_staging.orders_tmp",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
					CDCExpectedIncarnation: incA,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            "dbo.items",
					StagingTable:           "_bruin_staging.items_tmp",
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"},
					CDCExpectedIncarnation: incB,
				},
			},
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("aborts on lease loss", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		mock.ExpectRollback()

		lease := &stubLostLease{done: make(chan struct{}), err: source.ErrConnectorLeaseLost}
		close(lease.done)
		ctx := source.WithConnectorLeaseGuard(t.Context(), source.NewConnectorLeaseGuard(lease))

		dest := &FabricDestination{db: db}
		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{{
			Options: destination.MergeOptions{TargetTable: "dbo.orders", StagingTable: "dbo.s", PrimaryKeys: []string{"id"}, Columns: []string{"id", "_cdc_lsn", "_cdc_deleted", "_cdc_synced_at"}},
		}})
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSwapTableSameSchemaAndCrossSchemaConditional(t *testing.T) {
	t.Run("same schema conditional swap renames and drops backup atomically", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		targetInc := destination.CDCTargetKey("", "10", "2026-01-01T00:00:00")
		stagingInc := destination.CDCTargetKey("", "20", "2026-01-01T00:01:00")

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders_staging_1").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(20), "2026-01-01T00:01:00"))

		dest := &FabricDestination{db: db}
		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(t.Context(), "dbo.orders", "dbo.orders_staging_1")
		require.NoError(t, err)
		assert.Equal(t, stagingInc, gotStaging)
		assert.Equal(t, stagingInc, gotResult)

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders_staging_1").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(20), "2026-01-01T00:01:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectExec(`EXEC sys\.sp_rename '\[dbo\]\.\[orders\]', 'orders_old_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`EXEC sys\.sp_rename '\[dbo\]\.\[orders_staging_1\]', 'orders', 'OBJECT'`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE \[dbo\]\.\[orders_old_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(20), "2026-01-01T00:01:00"))
		mock.ExpectCommit()

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable:                  "dbo.orders_staging_1",
			TargetTable:                   "dbo.orders",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: gotStaging,
			CDCExpectedResultIncarnation:  gotResult,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cross schema swap clones into target schema and cleans up staging", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectExec(`CREATE TABLE \[dbo\]\.\[orders_swap_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(30), "2026-01-01T00:02:00"))

		dest := &FabricDestination{db: db}
		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(t.Context(), "dbo.orders", "_bruin_staging.orders_staging_1")
		require.NoError(t, err)
		assert.Equal(t, destination.CDCTargetKey("", "30", "2026-01-01T00:02:00"), gotStaging)
		assert.Equal(t, gotStaging, gotResult)

		targetInc := destination.CDCTargetKey("", "10", "2026-01-01T00:00:00")
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(30), "2026-01-01T00:02:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(10), "2026-01-01T00:00:00"))
		mock.ExpectExec(`EXEC sys\.sp_rename '\[dbo\]\.\[orders\]', 'orders_old_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`EXEC sys\.sp_rename '\[dbo\]\.\[orders_swap_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`DROP TABLE \[dbo\]\.\[orders_old_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(30), "2026-01-01T00:02:00"))
		mock.ExpectCommit()
		mock.ExpectExec(`DROP TABLE \[_bruin_staging\]\.\[orders_staging_1\]`).WillReturnResult(sqlmock.NewResult(0, 0))

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable:                  "_bruin_staging.orders_staging_1",
			TargetTable:                   "dbo.orders",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: gotStaging,
			CDCExpectedResultIncarnation:  gotResult,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAcquireManagedCDCRunLeaseLifecycleAndTakeover(t *testing.T) {
	origTTL, origHB, origCheck, origRetry, origGrace := cdcLeaseTTL, cdcLeaseHeartbeat, cdcLeaseCheck, cdcLeaseRetryDelay, cdcLeaseAcquireGrace
	cdcLeaseTTL = 200 * time.Millisecond
	cdcLeaseHeartbeat = 20 * time.Millisecond
	cdcLeaseCheck = 10 * time.Millisecond
	cdcLeaseRetryDelay = 10 * time.Millisecond
	cdcLeaseAcquireGrace = 200 * time.Millisecond
	defer func() {
		cdcLeaseTTL, cdcLeaseHeartbeat, cdcLeaseCheck, cdcLeaseRetryDelay, cdcLeaseAcquireGrace = origTTL, origHB, origCheck, origRetry, origGrace
	}()

	t.Run("acquires heartbeats and releases", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.MatchExpectationsInOrder(false)
		mock.ExpectExec(`CREATE SCHEMA \[_bruin_staging\]`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectExec(`CREATE TABLE \[_bruin_staging\]\.\[cdc_lease_`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`INSERT INTO \[_bruin_staging\]\.\[cdc_lease_`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		mock.ExpectQuery(`SELECT TOP 1 \[token\] FROM \[_bruin_staging\]\.\[cdc_lease_`).
			WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("fixed"))

		dest := &FabricDestination{db: db}
		// Direct lease object test with deterministic token for sqlmock verification.
		lease := &fabricCDCRunLease{
			d:       dest,
			schema:  cdcLeaseSchema,
			name:    "cdc_lease_test",
			token:   "fixed",
			done:    make(chan struct{}),
			stop:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		require.NoError(t, dest.ensureSchemaExists(t.Context(), lease.schema))
		acquired, err := lease.tryAcquire(t.Context())
		require.NoError(t, err)
		require.True(t, acquired)

		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT TOP 1 \[token\] FROM \[_bruin_staging\]\.\[cdc_lease_test\]`).
			WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("fixed"))
		mock.ExpectExec(`EXEC sys\.sp_rename '\[_bruin_staging\]\.\[cdc_lease_test\]', 'cdc_lease_test_released_fixed', 'OBJECT'`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT TOP 1 \[token\] FROM \[_bruin_staging\]\.\[cdc_lease_test_released_fixed\]`).
			WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("fixed"))
		mock.ExpectExec(`DROP TABLE \[_bruin_staging\]\.\[cdc_lease_test_released_fixed\]`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		lease.lastHeartbeat.Store(time.Now().UnixNano())
		go lease.monitor()
		// Stop before releasing so monitor doesn't race sqlmock queries during Release.
		lease.stopping.Store(true)
		close(lease.stop)
		<-lease.stopped
		// Reset releaseOnce after stopping monitor so Release executes full tx cleanly.
		lease = &fabricCDCRunLease{
			d:       dest,
			schema:  cdcLeaseSchema,
			name:    "cdc_lease_test",
			token:   "fixed",
			done:    make(chan struct{}),
			stop:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		close(lease.stopped)
		require.NoError(t, lease.Release())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("takes over expired stale lease", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		mock.ExpectExec(`CREATE TABLE \[_bruin_staging\]\.\[cdc_lease_stale\]`).
			WillReturnError(mssqldb.Error{Number: 2714, Message: "already exists"})
		mock.ExpectRollback()
		mock.ExpectQuery(`SELECT TOP 1 \[token\], DATEDIFF`).
			WillReturnRows(sqlmock.NewRows([]string{"token", "age"}).AddRow("dead-token", int64(60)))
		mock.ExpectBegin()
		mock.ExpectExec(`EXEC sys\.sp_rename '\[_bruin_staging\]\.\[cdc_lease_stale\]', 'cdc_lease_stale_expired_new-token', 'OBJECT'`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT TOP 1 \[token\] FROM \[_bruin_staging\]\.\[cdc_lease_stale_expired_new-token\]`).
			WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("dead-token"))
		mock.ExpectExec(`DROP TABLE \[_bruin_staging\]\.\[cdc_lease_stale_expired_new-token\]`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		lease := &fabricCDCRunLease{
			d:       &FabricDestination{db: db},
			schema:  cdcLeaseSchema,
			name:    "cdc_lease_stale",
			token:   "new-token",
			done:    make(chan struct{}),
			stop:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		acquired, err := lease.tryAcquire(t.Context())
		require.NoError(t, err)
		assert.False(t, acquired)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rejects empty connector ID and marks overdue lease lost", func(t *testing.T) {
		dest := &FabricDestination{}
		_, err := dest.AcquireManagedCDCRunLease(t.Context(), "")
		require.ErrorContains(t, err, "empty")

		lease := &fabricCDCRunLease{
			done: make(chan struct{}),
		}
		lease.lastHeartbeat.Store(time.Now().Add(-time.Minute).UnixNano())
		<-lease.Done()
		require.ErrorIs(t, lease.Err(), source.ErrConnectorLeaseLost)
	})
}

func TestWriteParallelHandlesNilBatchAndIncarnationFencing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectedInc := destination.CDCTargetKey("", "77", "2026-01-01T00:00:00")
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

	mem := memory.NewGoAllocator()
	recSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewRecordBuilder(mem, recSchema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(1)
	batch := b.NewRecordBatch()

	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
	mock.ExpectExec(`INSERT INTO \[dbo\]\.\[events\]`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
	mock.ExpectCommit()
	mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
		WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{CDCSnapshotRangeEnd: true}
	records <- source.RecordBatchResult{Batch: batch}
	close(records)

	dest := &FabricDestination{db: db, writeMode: writeModeInsert}
	err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
		Table:                  "dbo.events",
		CDCExpectedIncarnation: expectedInc,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newSingleInt64Batch(val int64) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	recSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewRecordBuilder(mem, recSchema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(val)
	return b.NewRecordBatch()
}

func newEmptyInt64Batch() arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	recSchema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewRecordBuilder(mem, recSchema)
	defer b.Release()
	return b.NewRecordBatch()
}

func TestWriteParallelIncarnationFencingAcrossBatchesAndModes(t *testing.T) {
	expectedInc := destination.CDCTargetKey("", "77", "2026-01-01T00:00:00")

	t.Run("insert mode aborts second batch before INSERT when target replaced mid-stream", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		mock.ExpectExec(`INSERT INTO \[dbo\]\.\[events\]`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(88), "2026-01-01T00:05:00"))
		mock.ExpectRollback()

		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(1)}
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(2)}
		close(records)

		dest := &FabricDestination{db: db, writeMode: writeModeInsert}
		err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.events",
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("insert mode rolls back uncommitted batch when incarnation changes before commit", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		mock.ExpectExec(`INSERT INTO \[dbo\]\.\[events\]`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(88), "2026-01-01T00:05:00"))
		mock.ExpectRollback()

		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(1)}
		close(records)

		dest := &FabricDestination{db: db, writeMode: writeModeInsert}
		err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.events",
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("copy mode checks incarnation inside each batch transaction and commits on match", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		copySQL := mssqldb.CopyIn("[dbo].[events]", mssqldb.BulkOptions{RowsPerBatch: 1, Tablock: true}, "id")

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		prep := mock.ExpectPrepare(regexp.QuoteMeta(copySQL))
		prep.ExpectExec().WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
		prep.ExpectExec().WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(1)}
		close(records)

		dest := &FabricDestination{db: db, writeMode: writeModeCopy}
		err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.events",
			CDCExpectedIncarnation: expectedInc,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("copy mode aborts second batch before CopyIn when target replaced mid-stream", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		copySQL := mssqldb.CopyIn("[dbo].[events]", mssqldb.BulkOptions{RowsPerBatch: 1, Tablock: true}, "id")

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		prep := mock.ExpectPrepare(regexp.QuoteMeta(copySQL))
		prep.ExpectExec().WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
		prep.ExpectExec().WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:06:00"))
		mock.ExpectRollback()

		records := make(chan source.RecordBatchResult, 2)
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(1)}
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(2)}
		close(records)

		dest := &FabricDestination{db: db, writeMode: writeModeCopy}
		err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.events",
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("copy mode rolls back uncommitted bulk copy when incarnation changes before commit", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		copySQL := mssqldb.CopyIn("[dbo].[events]", mssqldb.BulkOptions{RowsPerBatch: 1, Tablock: true}, "id")

		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))

		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
		prep := mock.ExpectPrepare(regexp.QuoteMeta(copySQL))
		prep.ExpectExec().WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
		prep.ExpectExec().WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:06:00"))
		mock.ExpectRollback()

		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: newSingleInt64Batch(1)}
		close(records)

		dest := &FabricDestination{db: db, writeMode: writeModeCopy}
		err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
			Table:                  "dbo.events",
			CDCExpectedIncarnation: expectedInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("zero-row batch still checks expected incarnation", func(t *testing.T) {
		for _, mode := range []string{writeModeInsert, writeModeCopy} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)

			mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
				WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(77), "2026-01-01T00:00:00"))
			mock.ExpectQuery(`FROM sys\.tables`).WithArgs("dbo", "events").
				WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(99), "2026-01-01T00:06:00"))

			records := make(chan source.RecordBatchResult, 1)
			records <- source.RecordBatchResult{Batch: newEmptyInt64Batch()}
			close(records)

			dest := &FabricDestination{db: db, writeMode: mode}
			err = dest.WriteParallel(t.Context(), records, destination.WriteOptions{
				Table:                  "dbo.events",
				CDCExpectedIncarnation: expectedInc,
			})
			require.ErrorContains(t, err, "physical incarnation changed")
			require.NoError(t, mock.ExpectationsWereMet())
			_ = db.Close()
		}
	})
}

func TestSwapTablePreservesDottedSchemaAndTableQuoting(t *testing.T) {
	t.Run("conditional cross-schema swap with dotted target schema and table", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales\.Detail'\) EXEC\('CREATE SCHEMA \[Sales\.Detail\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^CREATE TABLE \[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\] AS CLONE OF \[_bruin_staging\]\.\[order\.events_staging_1\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(31), "2026-01-01T00:02:00"))

		dest := &FabricDestination{db: db, database: "wh"}
		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(
			t.Context(),
			"[Sales.Detail].[order.events]",
			"[_bruin_staging].[order.events_staging_1]",
		)
		require.NoError(t, err)
		assert.Equal(t, destination.CDCTargetKey("wh", "31", "2026-01-01T00:02:00"), gotStaging)
		assert.Equal(t, gotStaging, gotResult)

		targetInc := destination.CDCTargetKey("wh", "11", "2026-01-01T00:00:00")
		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales\.Detail'\) EXEC\('CREATE SCHEMA \[Sales\.Detail\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", "order.events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(11), "2026-01-01T00:00:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(31), "2026-01-01T00:02:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", "order.events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(11), "2026-01-01T00:00:00"))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\.Detail\]\.\[order\.events\]', 'order\.events_old_[0-9]+', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\]', 'order\.events', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^DROP TABLE \[Sales\.Detail\]\.\[order\.events_old_[0-9]+\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", "order.events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(31), "2026-01-01T00:02:00"))
		mock.ExpectCommit()
		mock.ExpectExec(`^IF OBJECT_ID\('\[_bruin_staging\]\.\[order\.events_staging_1\]', 'U'\) IS NOT NULL DROP TABLE \[_bruin_staging\]\.\[order\.events_staging_1\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable:                  "[_bruin_staging].[order.events_staging_1]",
			TargetTable:                   "[Sales.Detail].[order.events]",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: gotStaging,
			CDCExpectedResultIncarnation:  gotResult,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unconditional swap rollback drops dotted clone table with intact brackets", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectExec(`CREATE SCHEMA \[Sales\.Detail\]`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^CREATE TABLE \[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\] AS CLONE OF \[_bruin_staging\]\.\[orders_stg\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales.Detail", "order.events").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\]', 'order\.events', 'OBJECT'$`).
			WillReturnError(errors.New("rename failed"))
		mock.ExpectRollback()
		mock.ExpectExec(`^IF OBJECT_ID\('\[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\]', 'U'\) IS NOT NULL DROP TABLE \[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		dest := &FabricDestination{db: db, database: "wh"}
		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable: "_bruin_staging.orders_stg",
			TargetTable:  "[Sales.Detail].[order.events]",
		})
		require.ErrorContains(t, err, "rename failed")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cross-database dotted catalog schema and table quoting", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectExec(`CREATE SCHEMA \[Sales\.Detail\]`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^CREATE TABLE \[wh\.two\]\.\[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\] AS CLONE OF \[_bruin_staging\]\.\[orders_stg\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		dest := &FabricDestination{db: db, database: "wh1"}
		targetID, err := dest.resolveTargetIdentity("[wh.two].[Sales.Detail].[order.events]")
		require.NoError(t, err)

		cloned, err := dest.cloneStagingIntoTargetSchema(t.Context(), targetID, "[_bruin_staging].[orders_stg]")
		require.NoError(t, err)
		assert.Regexp(t, `^\[wh\.two\]\.\[Sales\.Detail\]\.\[order\.events_swap_[0-9]+\]$`, cloned)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSwapTableRespectsWarehouseCollationForSchemaCase(t *testing.T) {
	t.Run("conditional swap on case-sensitive BIN2 warehouse clones across Sales and sales schemas", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\] WHERE \[name\] = @p1`).
			WithArgs("wh").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("wh", "Latin1_General_100_BIN2_UTF8"))
		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales'\) EXEC\('CREATE SCHEMA \[Sales\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^CREATE TABLE \[Sales\]\.\[orders_swap_[0-9]+\] AS CLONE OF \[sales\]\.\[orders_tmp\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(42), "2026-01-01T00:02:00"))

		dest := &FabricDestination{db: db, database: "wh"}
		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(t.Context(), "Sales.orders", "sales.orders_tmp")
		require.NoError(t, err)
		assert.Equal(t, destination.CDCTargetKey("wh", "42", "2026-01-01T00:02:00"), gotStaging)
		assert.Equal(t, gotStaging, gotResult)

		targetInc := destination.CDCTargetKey("wh", "12", "2026-01-01T00:00:00")
		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales'\) EXEC\('CREATE SCHEMA \[Sales\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(42), "2026-01-01T00:02:00"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\]\.\[orders\]', 'orders_old_[0-9]+', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\]\.\[orders_swap_[0-9]+\]', 'orders', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^DROP TABLE \[Sales\]\.\[orders_old_[0-9]+\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(42), "2026-01-01T00:02:00"))
		mock.ExpectCommit()
		mock.ExpectExec(`^IF OBJECT_ID\('sales\.orders_tmp', 'U'\) IS NOT NULL DROP TABLE \[sales\]\.\[orders_tmp\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable:                  "sales.orders_tmp",
			TargetTable:                   "Sales.orders",
			CDCExpectedIncarnation:        targetInc,
			CDCExpectedStagingIncarnation: gotStaging,
			CDCExpectedResultIncarnation:  gotResult,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unconditional swap on case-sensitive CS warehouse with default DB_NAME clones into Sales", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\] WHERE \[name\] = DB_NAME\(\)`).
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("wh", "Latin1_General_100_CS_AS_KS_WS_SC_UTF8"))
		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales'\) EXEC\('CREATE SCHEMA \[Sales\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^CREATE TABLE \[Sales\]\.\[orders_swap_[0-9]+\] AS CLONE OF \[sales\]\.\[orders_tmp\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\]\.\[orders\]', 'orders_old_[0-9]+', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\]\.\[orders_swap_[0-9]+\]', 'orders', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^DROP TABLE \[Sales\]\.\[orders_old_[0-9]+\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		mock.ExpectExec(`^IF OBJECT_ID\('sales\.orders_tmp', 'U'\) IS NOT NULL DROP TABLE \[sales\]\.\[orders_tmp\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))

		dest := &FabricDestination{db: db}
		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable: "sales.orders_tmp",
			TargetTable:  "Sales.orders",
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("case-insensitive CI warehouse treats Sales and sales as same schema without cloning", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\] WHERE \[name\] = @p1`).
			WithArgs("wh").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("wh", "Latin1_General_100_CI_AS_KS_WS_SC_UTF8"))
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("sales", "orders_tmp").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(50), "2026-01-01T00:03:00"))

		dest := &FabricDestination{db: db, database: "wh"}
		gotStaging, gotResult, err := dest.CDCConditionalSwapIncarnations(t.Context(), "Sales.orders", "sales.orders_tmp")
		require.NoError(t, err)
		assert.Equal(t, destination.CDCTargetKey("wh", "50", "2026-01-01T00:03:00"), gotStaging)
		assert.Equal(t, gotStaging, gotResult)

		mock.ExpectQuery(`SELECT \[name\], \[collation_name\] FROM \[sys\]\.\[databases\] WHERE \[name\] = @p1`).
			WithArgs("wh").
			WillReturnRows(sqlmock.NewRows([]string{"name", "collation_name"}).AddRow("wh", "Latin1_General_100_CI_AS_KS_WS_SC_UTF8"))
		mock.ExpectExec(`IF NOT EXISTS \(SELECT \* FROM sys\.schemas WHERE name = 'Sales'\) EXEC\('CREATE SCHEMA \[Sales\]'\)`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectBegin()
		mock.ExpectQuery(`FROM sys\.tables`).WithArgs("Sales", "orders").
			WillReturnRows(sqlmock.NewRows([]string{"object_id", "create_date"}).AddRow(int64(12), "2026-01-01T00:00:00"))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[Sales\]\.\[orders\]', 'orders_old_[0-9]+', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^EXEC sys\.sp_rename '\[sales\]\.\[orders_tmp\]', 'orders', 'OBJECT'$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`^DROP TABLE \[Sales\]\.\[orders_old_[0-9]+\]$`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err = dest.SwapTable(t.Context(), destination.SwapOptions{
			StagingTable: "sales.orders_tmp",
			TargetTable:  "Sales.orders",
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

type stubLostLease struct {
	done chan struct{}
	err  error
}

func (l *stubLostLease) Done() <-chan struct{} { return l.done }
func (l *stubLostLease) Err() error            { return l.err }
func (l *stubLostLease) Release() error        { return nil }

var _ source.ConnectorLease = (*stubLostLease)(nil)

func TestBuildFabricCDCMergeSQLWithAllCDCSourcesColumns(t *testing.T) {
	// Ensure CDC merge handles all column layouts including unchanged cols and composite PKs.
	sqlText := buildFabricCDCMergeSQL("dbo.composite", "dbo.composite_stg", []string{"tenant_id", "id"}, []string{
		"tenant_id",
		"id",
		"payload",
		destination.CDCLSNColumn,
		destination.CDCDeletedColumn,
		destination.CDCSyncedAtColumn,
	}, "")

	assert.Contains(t, sqlText, "ON target.[tenant_id] = source.[tenant_id] AND target.[id] = source.[id]")
	assert.Contains(t, sqlText, "PARTITION BY [tenant_id], [id]")
	assert.Contains(t, sqlText, "la.[tenant_id] = act.[tenant_id] AND la.[id] = act.[id]")
	assert.False(t, strings.Contains(sqlText, "OPENJSON"))
}
