package hana

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteTableUsesHanaIdentifierSemantics(t *testing.T) {
	assert.Equal(t, `"ORDERS"`, quoteTable("orders"))
	assert.Equal(t, `"SALES"."ORDERS"`, quoteTable("sales.orders"))
	assert.Equal(t, `"sales.schema"."Orders"`, quoteTable(`"sales.schema"."Orders"`))
	assert.Equal(t, `"SALES"."USER""EVENTS"`, quoteTable(`sales.user"events`))
}

func TestMapDataTypeToHana(t *testing.T) {
	tests := []struct {
		column schema.Column
		want   string
	}{
		{schema.Column{DataType: schema.TypeBoolean}, "BOOLEAN"},
		{schema.Column{DataType: schema.TypeInt8}, "SMALLINT"},
		{schema.Column{DataType: schema.TypeInt32}, "INTEGER"},
		{schema.Column{DataType: schema.TypeInt64}, "BIGINT"},
		{schema.Column{DataType: schema.TypeFloat32}, "REAL"},
		{schema.Column{DataType: schema.TypeFloat64}, "DOUBLE"},
		{schema.Column{DataType: schema.TypeDecimal, Precision: 18, Scale: 4}, "DECIMAL(18,4)"},
		{schema.Column{DataType: schema.TypeDecimal, Precision: 50, Scale: 45}, "DECIMAL(38,38)"},
		{schema.Column{DataType: schema.TypeString, MaxLength: 128}, "NVARCHAR(128)"},
		{schema.Column{DataType: schema.TypeString}, "NCLOB"},
		{schema.Column{DataType: schema.TypeBinary, MaxLength: 64}, "VARBINARY(64)"},
		{schema.Column{DataType: schema.TypeBinary}, "BLOB"},
		{schema.Column{DataType: schema.TypeTimestampTZ}, "TIMESTAMP"},
		{schema.Column{DataType: schema.TypeJSON}, "NCLOB"},
		{schema.Column{DataType: schema.TypeUUID}, "NVARCHAR(36)"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, MapDataTypeToHana(tt.column))
	}
	assert.Equal(t, "NVARCHAR(5000)", mapDataTypeToHana(schema.Column{DataType: schema.TypeString}, true))
}

func TestNormalizeSchemaEvolutionColumn(t *testing.T) {
	dest := NewHanaDestination()

	assert.Equal(t, schema.TypeInt16, dest.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeInt8}).DataType)
	assert.Equal(t, schema.TypeTimestamp, dest.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeTimestampTZ}).DataType)
	assert.Equal(t, schema.Column{DataType: schema.TypeString, MaxLength: 36}, dest.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeUUID}))
	assert.Equal(t, schema.Column{DataType: schema.TypeString}, dest.NormalizeSchemaEvolutionColumn(schema.Column{DataType: schema.TypeJSON}))
}

func TestApplySchemaEvolutionRewritesUnsupportedStringWidening(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	temporaryName, backupName := rewriteArtifactColumnNames("app.events", "AGE")

	mock.ExpectExec(`ALTER TABLE "APP"\."EVENTS" ALTER \("AGE" NCLOB\)`).
		WillReturnError(assert.AnError)
	mock.ExpectQuery(`FROM SYS\.TABLE_COLUMNS`).
		WithArgs("APP", "EVENTS", "AGE", temporaryName, backupName).
		WillReturnRows(sqlmock.NewRows([]string{"column"}).AddRow("AGE"))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`ALTER TABLE "APP"."EVENTS" ADD ("%s" NCLOB)`, temporaryName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`UPDATE "APP"."EVENTS" SET "%s" = TO_NCLOB("AGE")`, temporaryName))).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`ALTER TABLE "APP"."EVENTS" ALTER ("%s" NOT NULL)`, temporaryName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`RENAME COLUMN "APP"."EVENTS"."AGE" TO "%s"`, backupName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`RENAME COLUMN "APP"."EVENTS"."%s" TO "AGE"`, temporaryName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`ALTER TABLE "APP"."EVENTS" DROP ("%s")`, backupName))).
		WillReturnResult(sqlmock.NewResult(0, 0))

	oldColumn := schema.Column{Name: "AGE", DataType: schema.TypeInt64, Nullable: false}
	warnings, err := dest.ApplySchemaEvolution(t.Context(), "app.events", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeWidenType,
			ColumnName: "AGE",
			OldColumn:  &oldColumn,
			NewColumn:  schema.Column{Name: "AGE", DataType: schema.TypeString, Nullable: false},
		}},
	})
	require.NoError(t, err)
	assert.Contains(t, warnings, `column "AGE": type widened to string`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplySchemaEvolutionRecoversInterruptedColumnRewrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	temporaryName, backupName := rewriteArtifactColumnNames("app.events", "AGE")

	mock.ExpectQuery(`FROM SYS\.TABLE_COLUMNS`).
		WithArgs("APP", "EVENTS", "AGE", temporaryName, backupName).
		WillReturnRows(sqlmock.NewRows([]string{"column"}).AddRow(temporaryName).AddRow(backupName))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`RENAME COLUMN "APP"."EVENTS"."%s" TO "AGE"`, backupName))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`ALTER TABLE "APP"."EVENTS" DROP ("%s")`, temporaryName))).
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err = dest.ApplySchemaEvolution(t.Context(), "app.events", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeAddColumn,
			ColumnName: "AGE",
			NewColumn:  schema.Column{Name: "AGE", DataType: schema.TypeString, Nullable: true},
		}},
	})
	require.ErrorContains(t, err, "recovered interrupted HANA rewrite")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplySchemaEvolutionCleansRewriteArtifactsBeforeApplyingPlan(t *testing.T) {
	temporaryName, backupName := rewriteArtifactColumnNames("app.events", "AGE")
	for _, artifactName := range []string{temporaryName, backupName} {
		t.Run(artifactName, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			dest := &HanaDestination{db: db, defaultSchema: "APP"}

			mock.ExpectExec(regexp.QuoteMeta(fmt.Sprintf(`ALTER TABLE "APP"."EVENTS" DROP ("%s")`, artifactName))).
				WillReturnResult(sqlmock.NewResult(0, 0))

			warnings, err := dest.ApplySchemaEvolution(t.Context(), "app.events", &schemaevolution.SchemaComparison{
				HasChanges: true,
				Changes: []schemaevolution.SchemaChange{{
					Type:       schemaevolution.ChangeRemoveColumn,
					ColumnName: artifactName,
					OldColumn:  &schema.Column{Name: artifactName, DataType: schema.TypeString},
				}},
			})
			require.NoError(t, err)
			assert.Empty(t, warnings)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPrepareTableCreatesMissingSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	mock.ExpectQuery(`FROM SYS\.TABLES`).WithArgs("LANDING", "EVENTS").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`FROM SYS\.SCHEMAS`).WithArgs("LANDING").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA "LANDING"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE COLUMN TABLE "LANDING"\."EVENTS"`).WillReturnResult(sqlmock.NewResult(0, 0))

	err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table:  "landing.events",
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareTableSkipsSchemaCreationForDefaultSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	mock.ExpectQuery(`FROM SYS\.TABLES`).WithArgs("APP", "EVENTS").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`CREATE COLUMN TABLE "APP"\."EVENTS"`).WillReturnResult(sqlmock.NewResult(0, 0))

	err = dest.PrepareTable(t.Context(), destination.PrepareOptions{
		Table:  "app.events",
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplySchemaEvolutionSkipsLOBWideningOnPrimaryKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	// The target column was created as NVARCHAR(5000) because it is a primary key; the source
	// reports it as an unbounded string. No ALTER may be issued, as HANA rejects LOB keys.
	oldColumn := schema.Column{Name: "ID", DataType: schema.TypeString, MaxLength: 5000, IsPrimaryKey: true}
	warnings, err := dest.ApplySchemaEvolution(t.Context(), "app.events", &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type:       schemaevolution.ChangeWidenType,
			ColumnName: "ID",
			OldColumn:  &oldColumn,
			NewColumn:  schema.Column{Name: "ID", DataType: schema.TypeString},
		}},
	})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildCreateTableSQL(t *testing.T) {
	tableSchema := &schema.TableSchema{
		IncrementalKey: "updated_at",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString, Nullable: false},
			{Name: "payload", DataType: schema.TypeJSON, Nullable: false},
			{Name: "updated_at", DataType: schema.TypeTimestamp, Nullable: false},
		},
	}

	got := buildCreateTableSQL("landing.events", destination.PrepareOptions{
		Schema: tableSchema, PrimaryKeys: []string{"id"}, CDCMode: true, CDCKeys: []string{"id"},
	})

	assert.Equal(t, `CREATE COLUMN TABLE "LANDING"."EVENTS" (
  "ID" NVARCHAR(5000) NOT NULL,
  "PAYLOAD" NCLOB,
  "UPDATED_AT" TIMESTAMP,
  PRIMARY KEY ("ID")
)`, got)
}

func TestBuildMergeSQLDeduplicatesStaging(t *testing.T) {
	got := buildMergeSQL("landing.events_merge", "landing.events", []string{"id"}, []string{"id", "name", "updated_at"}, "updated_at")

	assert.Contains(t, got, `MERGE INTO "LANDING"."EVENTS" AS target`)
	assert.Contains(t, got, `ROW_NUMBER() OVER (PARTITION BY "ID" ORDER BY "UPDATED_AT" DESC)`)
	assert.Contains(t, got, `ON target."ID" = source."ID"`)
	assert.Contains(t, got, `WHEN MATCHED THEN UPDATE SET "NAME" = source."NAME", "UPDATED_AT" = source."UPDATED_AT"`)
	assert.Contains(t, got, `WHEN NOT MATCHED THEN INSERT ("ID", "NAME", "UPDATED_AT")`)
}

func TestGetTableSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	mock.ExpectQuery(`FROM SYS\.TABLE_COLUMNS`).
		WithArgs("APP", "EVENTS").
		WillReturnRows(sqlmock.NewRows([]string{"column", "type", "nullable", "length", "scale"}).
			AddRow("ID", "BIGINT", "FALSE", 19, 0).
			AddRow("Name", "NVARCHAR", "TRUE", 255, nil).
			AddRow("AMOUNT", "DECIMAL", "TRUE", 18, 4))
	mock.ExpectQuery(`FROM SYS\.CONSTRAINTS`).
		WithArgs("APP", "EVENTS").
		WillReturnRows(sqlmock.NewRows([]string{"column"}).AddRow("ID"))

	got, err := dest.GetTableSchema(t.Context(), "events")
	require.NoError(t, err)
	require.Len(t, got.Columns, 3)
	assert.Equal(t, schema.Column{Name: "ID", DataType: schema.TypeInt64, Nullable: false, IsPrimaryKey: true}, got.Columns[0])
	assert.Equal(t, schema.Column{Name: `"Name"`, DataType: schema.TypeString, Nullable: true, MaxLength: 255}, got.Columns[1])
	assert.Equal(t, schema.Column{Name: "AMOUNT", DataType: schema.TypeDecimal, Nullable: true, Precision: 18, Scale: 4}, got.Columns[2])
	assert.Equal(t, []string{"ID"}, got.PrimaryKeys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRecordBatchUsesHanaBulkInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	idBuilder := array.NewInt64Builder(allocator)
	idBuilder.AppendValues([]int64{1, 2}, nil)
	ids := idBuilder.NewArray()
	idBuilder.Release()
	nameBuilder := array.NewStringBuilder(allocator)
	nameBuilder.AppendValues([]string{"one", "two"}, nil)
	names := nameBuilder.NewArray()
	nameBuilder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "name", Type: arrow.BinaryTypes.String}}, nil),
		[]arrow.Array{ids, names}, 2,
	)
	ids.Release()
	names.Release()
	t.Cleanup(record.Release)

	query := regexp.QuoteMeta(`INSERT INTO "EVENTS" ("ID", "NAME") VALUES (?, ?)`)
	mock.ExpectBegin()
	prepared := mock.ExpectPrepare(query)
	prepared.ExpectExec().WithArgs(int64(1), "one", int64(2), "two").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	rows, err := dest.writeRecordBatch(t.Context(), record, "events")
	require.NoError(t, err)
	assert.EqualValues(t, 2, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWriteRecordBatchChunksLargeBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}

	allocator := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { allocator.AssertSize(t, 0) })
	numRows := hanaInsertRowsPerStatement + 5
	idBuilder := array.NewInt64Builder(allocator)
	for i := 0; i < numRows; i++ {
		idBuilder.Append(int64(i))
	}
	ids := idBuilder.NewArray()
	idBuilder.Release()
	record := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil),
		[]arrow.Array{ids}, int64(numRows),
	)
	ids.Release()
	t.Cleanup(record.Release)

	query := regexp.QuoteMeta(`INSERT INTO "EVENTS" ("ID") VALUES (?)`)
	mock.ExpectBegin()
	prepared := mock.ExpectPrepare(query)
	prepared.ExpectExec().WillReturnResult(sqlmock.NewResult(0, hanaInsertRowsPerStatement))
	prepared.ExpectExec().WithArgs(
		int64(hanaInsertRowsPerStatement), int64(hanaInsertRowsPerStatement+1), int64(hanaInsertRowsPerStatement+2),
		int64(hanaInsertRowsPerStatement+3), int64(hanaInsertRowsPerStatement+4),
	).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()

	rows, err := dest.writeRecordBatch(t.Context(), record, "events")
	require.NoError(t, err)
	assert.EqualValues(t, numRows, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExtractValue(t *testing.T) {
	allocator := memory.NewGoAllocator()

	cases := []struct {
		name  string
		build func() arrow.Array
		want  interface{}
	}{
		{
			name: "null",
			build: func() arrow.Array {
				b := array.NewInt64Builder(allocator)
				defer b.Release()
				b.AppendNull()
				return b.NewArray()
			},
			want: nil,
		},
		{
			name: "uint64",
			build: func() arrow.Array {
				b := array.NewUint64Builder(allocator)
				defer b.Release()
				b.Append(42)
				return b.NewArray()
			},
			want: uint64(42),
		},
		{
			name: "date32",
			build: func() arrow.Array {
				b := array.NewDate32Builder(allocator)
				defer b.Release()
				b.Append(arrow.Date32FromTime(time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)))
				return b.NewArray()
			},
			want: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "time32_seconds",
			build: func() arrow.Array {
				b := array.NewTime32Builder(allocator, &arrow.Time32Type{Unit: arrow.Second})
				defer b.Release()
				b.Append(arrow.Time32(13*3600 + 45*60 + 30))
				return b.NewArray()
			},
			want: time.Date(1, time.January, 1, 13, 45, 30, 0, time.UTC),
		},
		{
			name: "time64_microseconds",
			build: func() arrow.Array {
				b := array.NewTime64Builder(allocator, &arrow.Time64Type{Unit: arrow.Microsecond})
				defer b.Release()
				b.Append(arrow.Time64((13*3600+45*60+30)*1e6 + 123456))
				return b.NewArray()
			},
			want: time.Date(1, time.January, 1, 13, 45, 30, 123456000, time.UTC),
		},
		{
			name: "timestamp_microseconds",
			build: func() arrow.Array {
				b := array.NewTimestampBuilder(allocator, &arrow.TimestampType{Unit: arrow.Microsecond})
				defer b.Release()
				b.Append(arrow.Timestamp(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC).UnixMicro()))
				return b.NewArray()
			},
			want: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		},
		{
			name: "decimal128_negative",
			build: func() arrow.Array {
				b := array.NewDecimal128Builder(allocator, &arrow.Decimal128Type{Precision: 10, Scale: 3})
				defer b.Release()
				b.Append(decimal128.FromI64(-1234))
				return b.NewArray()
			},
			want: "-1.234",
		},
		{
			name: "decimal256",
			build: func() arrow.Array {
				b := array.NewDecimal256Builder(allocator, &arrow.Decimal256Type{Precision: 40, Scale: 5})
				defer b.Release()
				b.Append(decimal256.FromI64(1234567))
				return b.NewArray()
			},
			want: "12.34567",
		},
		{
			name: "list_renders_json",
			build: func() arrow.Array {
				b := array.NewListBuilder(allocator, arrow.PrimitiveTypes.Int64)
				defer b.Release()
				b.Append(true)
				b.ValueBuilder().(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
				return b.NewArray()
			},
			want: "[1,2]",
		},
		{
			name: "struct_renders_json",
			build: func() arrow.Array {
				structType := arrow.StructOf(arrow.Field{Name: "a", Type: arrow.PrimitiveTypes.Int64})
				b := array.NewStructBuilder(allocator, structType)
				defer b.Release()
				b.Append(true)
				b.FieldBuilder(0).(*array.Int64Builder).Append(7)
				return b.NewArray()
			},
			want: `{"a":7}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			arr := tt.build()
			defer arr.Release()
			assert.Equal(t, tt.want, extractValue(arr, 0))
		})
	}
}

func TestDeleteInsertTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	intervalStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	intervalEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "APP"."EVENTS" WHERE "UPDATED_AT" >= ? AND "UPDATED_AT" <= ?`)).
		WithArgs(intervalStart, intervalEnd).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`(?s)^INSERT INTO "APP"\."EVENTS".*ROW_NUMBER\(\) OVER \(PARTITION BY "ID" ORDER BY "UPDATED_AT" DESC\)`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err = dest.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{
		StagingTable:   "app.events_staging",
		TargetTable:    "app.events",
		IncrementalKey: "updated_at",
		IntervalStart:  intervalStart,
		IntervalEnd:    intervalEnd,
		Columns:        []string{"id", "updated_at"},
		PrimaryKeys:    []string{"id"},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectHanaTableSchema(mock sqlmock.Sqlmock, schemaName, tableName string, rows *sqlmock.Rows) {
	mock.ExpectQuery(`FROM SYS\.TABLE_COLUMNS`).
		WithArgs(schemaName, tableName).
		WillReturnRows(rows)
	mock.ExpectQuery(`FROM SYS\.CONSTRAINTS`).
		WithArgs(schemaName, tableName).
		WillReturnRows(sqlmock.NewRows([]string{"column"}))
}

func TestSCD2Table(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	timestamp := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeString},
		{Name: "payload", DataType: schema.TypeJSON},
	}}

	physicalColumns := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"column", "type", "nullable", "length", "scale"}).
			AddRow("ID", "NVARCHAR", "FALSE", 5000, nil).
			AddRow("PAYLOAD", "NCLOB", "TRUE", nil, nil)
	}
	expectHanaTableSchema(mock, "APP", "DIM_EVENTS", physicalColumns())
	expectHanaTableSchema(mock, "APP", "DIM_EVENTS_STAGING", physicalColumns())
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "APP"\."DIM_EVENTS" WHERE "_SCD_IS_CURRENT" = TRUE AND \(LENGTH\("PAYLOAD"\) > 2000\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "APP"\."DIM_EVENTS_STAGING" WHERE \(LENGTH\("PAYLOAD"\) > 2000\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)^MERGE INTO "APP"\."DIM_EVENTS" AS target.*HASH_SHA256\(TO_BINARY\(target\."PAYLOAD"\)\).*WHEN MATCHED THEN UPDATE`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)^UPDATE "APP"\."DIM_EVENTS" AS target.*NOT EXISTS`).
		WithArgs(timestamp).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)^INSERT INTO "APP"\."DIM_EVENTS".*NOT EXISTS`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err = dest.SCD2Table(t.Context(), destination.SCD2Options{
		StagingTable: "app.dim_events_staging",
		TargetTable:  "app.dim_events",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload"},
		Timestamp:    timestamp,
		Schema:       tableSchema,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSCD2TableRejectsOversizedLOBValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeString},
	}}

	physicalColumns := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"column", "type", "nullable", "length", "scale"}).
			AddRow("ID", "BIGINT", "FALSE", 19, nil).
			AddRow("PAYLOAD", "NCLOB", "TRUE", nil, nil)
	}
	expectHanaTableSchema(mock, "APP", "DIM_EVENTS", physicalColumns())
	expectHanaTableSchema(mock, "APP", "DIM_EVENTS_STAGING", physicalColumns())
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "APP"\."DIM_EVENTS" WHERE "_SCD_IS_CURRENT" = TRUE AND \(LENGTH\("PAYLOAD"\) > 2000\)`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err = dest.SCD2Table(t.Context(), destination.SCD2Options{
		StagingTable: "app.dim_events_staging",
		TargetTable:  "app.dim_events",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload"},
		Schema:       tableSchema,
	})
	require.ErrorContains(t, err, "exceeds 2000 bytes")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSCD2ByteLengthExpressionUsesByteLengthForBoundedUnicode(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "bounded_text", DataType: schema.TypeString, MaxLength: 5000},
		{Name: "lob_text", DataType: schema.TypeString},
		{Name: "binary_data", DataType: schema.TypeBinary, MaxLength: 5000},
	}}

	assert.Equal(t, `LENGTH(TO_BLOB(TO_NCLOB("BOUNDED_TEXT")))`, scd2ByteLengthExpression("bounded_text", tableSchema))
	assert.Equal(t, `LENGTH("LOB_TEXT")`, scd2ByteLengthExpression("lob_text", tableSchema))
	assert.Equal(t, `LENGTH("BINARY_DATA")`, scd2ByteLengthExpression("binary_data", tableSchema))
}

func TestSCD2TableRejectsLOBLogicalPrimaryKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	dest := &HanaDestination{db: db, defaultSchema: "APP"}
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeString, MaxLength: 100},
		{Name: "payload", DataType: schema.TypeInt64},
	}}

	expectHanaTableSchema(mock, "APP", "DIM_EVENTS", sqlmock.NewRows([]string{"column", "type", "nullable", "length", "scale"}).
		AddRow("ID", "NCLOB", "FALSE", nil, nil).
		AddRow("PAYLOAD", "BIGINT", "TRUE", 19, nil))
	expectHanaTableSchema(mock, "APP", "DIM_EVENTS_STAGING", sqlmock.NewRows([]string{"column", "type", "nullable", "length", "scale"}).
		AddRow("ID", "NVARCHAR", "FALSE", 100, nil).
		AddRow("PAYLOAD", "BIGINT", "TRUE", 19, nil))
	err = dest.SCD2Table(t.Context(), destination.SCD2Options{
		StagingTable: "app.dim_events_staging",
		TargetTable:  "app.dim_events",
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "payload"},
		Schema:       tableSchema,
	})
	require.ErrorContains(t, err, "logical primary key")
	require.ErrorContains(t, err, "stored as a LOB")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReplaceStagingPolicyUsesCurrentSchema(t *testing.T) {
	dest := &HanaDestination{defaultSchema: "appUser"}
	policy := dest.ReplaceStagingPolicy()
	assert.Equal(t, destination.ReplaceStagingTargetSchema, policy.DefaultPlacement)
	assert.Equal(t, `"appUser"`, policy.DefaultTargetSchema)
}
