package oracle

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oracleDottedBackupArgument struct{}

func (oracleDottedBackupArgument) Match(value driver.Value) bool {
	name, ok := value.(string)
	return ok && strings.HasPrefix(name, "order.events_OLD_") && name != "ORDER"
}

func TestBuildConnStrings(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		want    []string
		wantErr bool
	}{
		{
			name: "path as dbname tries SID then service name",
			uri:  "oracle://user:pass@localhost:1521/ORCL",
			want: []string{
				"oracle://user:pass@localhost:1521/?SID=ORCL",
				"oracle://user:pass@localhost:1521/ORCL",
			},
		},
		{
			name: "oracle+cx_oracle scheme",
			uri:  "oracle+cx_oracle://user:pass@host:1521/ORCL",
			want: []string{
				"oracle://user:pass@host:1521/?SID=ORCL",
				"oracle://user:pass@host:1521/ORCL",
			},
		},
		{
			name: "explicit service name",
			uri:  "oracle://user:pass@host:1521/?service_name=XEPDB1",
			want: []string{"oracle://user:pass@host:1521/XEPDB1"},
		},
		{
			name: "explicit SID",
			uri:  "oracle://user:pass@host:1521?SID=ORCL",
			want: []string{"oracle://user:pass@host:1521/?SID=ORCL"},
		},
		{
			name: "default port",
			uri:  "oracle://user:pass@host/ORCL",
			want: []string{
				"oracle://user:pass@host:1521/?SID=ORCL",
				"oracle://user:pass@host:1521/ORCL",
			},
		},
		{
			name:    "invalid scheme",
			uri:     "postgres://user:pass@host/db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildConnStrings(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQuoteTable(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "bare table",
			table: "users",
			want:  `"USERS"`,
		},
		{
			name:  "schema table",
			table: "hr.employees",
			want:  `"HR"."EMPLOYEES"`,
		},
		{
			name:  "explicit staging schema is honored",
			table: "_bruin_staging.hr__employees_merge_123",
			want:  `"_BRUIN_STAGING"."HR__EMPLOYEES_MERGE_123"`,
		},
		{
			name:  "custom staging schema is honored",
			table: "custom_staging.hr__employees_merge_123",
			want:  `"CUSTOM_STAGING"."HR__EMPLOYEES_MERGE_123"`,
		},
		{
			name:  "embedded quote",
			table: `hr.user"events`,
			want:  `"HR"."USER""EVENTS"`,
		},
		{
			name:  "quoted identifiers preserve case",
			table: `"sales.schema"."Orders"`,
			want:  `"sales.schema"."Orders"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quoteTable(tt.table))
		})
	}
}

func TestCanonicalCDCTargetUsesOracleIdentifierSemantics(t *testing.T) {
	dest := &OracleDestination{currentUser: "INGESTR"}

	unquoted := dest.canonicalCDCTarget("orders")
	require.Equal(t, destination.CDCTargetKey("INGESTR", "ORDERS"), unquoted)
	require.Equal(t, unquoted, dest.canonicalCDCTarget(`"ORDERS"`))
	require.NotEqual(t, unquoted, dest.canonicalCDCTarget(`"orders"`))
	require.NotEqual(
		t,
		dest.canonicalCDCTarget(`sales.orders`),
		dest.canonicalCDCTarget(`"sales".orders`),
	)
}

func TestCanonicalCDCTargetAliasesQuotedCurrentUser(t *testing.T) {
	dest := &OracleDestination{currentUser: "appUser"}

	unqualified, err := dest.CanonicalCDCTarget(t.Context(), "orders")
	require.NoError(t, err)
	quotedCurrent, err := dest.CanonicalCDCTarget(t.Context(), `"appUser".orders`)
	require.NoError(t, err)
	unquotedSchema, err := dest.CanonicalCDCTarget(t.Context(), `appUser.orders`)
	require.NoError(t, err)
	require.Equal(t, unqualified, quotedCurrent)
	require.NotEqual(t, unqualified, unquotedSchema)
}

func TestMixedCaseCurrentUserIsQuotedForManagedState(t *testing.T) {
	dest := &OracleDestination{currentUser: "appUser"}
	policy := dest.ManagedStagingPolicy()
	require.Equal(t, `"appUser"`, policy.DefaultTargetSchema)
	require.Equal(t, `"appUser"."CDC_STATE"`, quoteTable(policy.DefaultTargetSchema+".cdc_state"))
}

func TestOracleStagingNamesPreserveQuotedSchemaSemantics(t *testing.T) {
	dest := &OracleDestination{currentUser: "appUser"}
	policy := dest.ManagedStagingPolicy()

	for _, suffix := range []string{"staging", "merge", "stream"} {
		t.Run(suffix, func(t *testing.T) {
			quoted := strategy.GenerateReplaceStagingTableName(`"appUser".orders`, suffix, "", policy)
			require.True(t, strings.HasPrefix(quoteTable(quoted), `"appUser"."ORDERS_`+strings.ToUpper(suffix)+`_`), quoted)

			unquoted := strategy.GenerateReplaceStagingTableName(`appUser.orders`, suffix, "", policy)
			require.True(t, strings.HasPrefix(quoteTable(unquoted), `"APPUSER"."ORDERS_`+strings.ToUpper(suffix)+`_`), unquoted)

			defaultSchema := strategy.GenerateReplaceStagingTableName("orders", suffix, "", policy)
			require.True(t, strings.HasPrefix(quoteTable(defaultSchema), `"appUser"."ORDERS_`+strings.ToUpper(suffix)+`_`), defaultSchema)
		})
	}
}

func TestOracleStagingNamesEncodeQuotedTableDots(t *testing.T) {
	dest := &OracleDestination{currentUser: "appUser"}
	policy := dest.ManagedStagingPolicy()

	for _, suffix := range []string{"staging", "merge", "stream"} {
		t.Run(suffix, func(t *testing.T) {
			staging := strategy.GenerateReplaceStagingTableName(`"appUser"."order.events"`, suffix, "", policy)
			require.NoError(t, tablename.TwoLevel("oracle").CheckName(staging))
			parts := splitOracleIdentifiers(staging)
			require.Len(t, parts, 2)
			require.Equal(t, `"appUser"`, parts[0])
			require.NotContains(t, parts[1], ".")
			require.Contains(t, quoteTable(staging), `"appUser"."_INGESTR_HEX_`)
		})
	}
}

func TestMixedCaseCurrentUserIncarnationUsesExactOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &OracleDestination{db: db, currentUser: "appUser"}
	mock.ExpectQuery(`SELECT OBJECT_ID FROM ALL_OBJECTS`).
		WithArgs("appUser", "ORDERS").
		WillReturnRows(sqlmock.NewRows([]string{"object_id"}).AddRow(10))

	_, exists, err := dest.CDCTargetIncarnation(t.Context(), "orders")
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCDCTargetIncarnationChangesWithOracleObjectIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &OracleDestination{db: db, currentUser: "APP"}
	query := `SELECT OBJECT_ID FROM ALL_OBJECTS`
	mock.ExpectQuery(query).WithArgs("APP", "ORDERS").WillReturnRows(sqlmock.NewRows([]string{"object_id"}).AddRow(10))
	mock.ExpectQuery(query).WithArgs("APP", "ORDERS").WillReturnRows(sqlmock.NewRows([]string{"object_id"}).AddRow(11))

	first, exists, err := dest.CDCTargetIncarnation(t.Context(), "orders")
	require.NoError(t, err)
	require.True(t, exists)
	second, exists, err := dest.CDCTargetIncarnation(t.Context(), "orders")
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEqual(t, first, second)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetTableSchemaPreservesQuotedCaseDistinctPrimaryKeyIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &OracleDestination{db: db, currentUser: "APP"}

	mock.ExpectQuery(`FROM ALL_TAB_COLUMNS`).WithArgs("APP", "ITEMS").WillReturnRows(
		sqlmock.NewRows([]string{"column_name", "data_type", "nullable", "data_precision", "data_scale", "char_length"}).
			AddRow("Foo", "VARCHAR2", "N", nil, nil, 100).
			AddRow("foo", "CLOB", "Y", nil, nil, nil),
	)
	mock.ExpectQuery(`FROM ALL_CONSTRAINTS`).WithArgs("APP", "ITEMS").WillReturnRows(
		sqlmock.NewRows([]string{"column_name"}).AddRow("Foo"),
	)

	got, err := dest.GetTableSchema(t.Context(), "items")
	require.NoError(t, err)
	require.Equal(t, []string{`"Foo"`}, got.PrimaryKeys)
	require.Equal(t, `"Foo"`, got.Columns[0].Name)
	require.True(t, got.Columns[0].IsPrimaryKey)
	require.Equal(t, `"foo"`, got.Columns[1].Name)
	require.False(t, got.Columns[1].IsPrimaryKey)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetTableSchemaKeepsUppercaseMetadataComparableToSourceNames(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &OracleDestination{db: db, currentUser: "APP"}

	mock.ExpectQuery(`FROM ALL_TAB_COLUMNS`).WithArgs("APP", "ITEMS").WillReturnRows(
		sqlmock.NewRows([]string{"column_name", "data_type", "nullable", "data_precision", "data_scale", "char_length"}).
			AddRow("_INGESTR_LOADED_AT", "TIMESTAMP WITH TIME ZONE", "Y", nil, nil, nil),
	)
	mock.ExpectQuery(`FROM ALL_CONSTRAINTS`).WithArgs("APP", "ITEMS").WillReturnRows(
		sqlmock.NewRows([]string{"column_name"}),
	)

	got, err := dest.GetTableSchema(t.Context(), "items")
	require.NoError(t, err)
	require.Equal(t, "_INGESTR_LOADED_AT", got.Columns[0].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMapDataTypeToOracle(t *testing.T) {
	tests := []struct {
		name string
		col  schema.Column
		want string
	}{
		{name: "boolean", col: schema.Column{DataType: schema.TypeBoolean}, want: "NUMBER(1,0)"},
		{name: "int64", col: schema.Column{DataType: schema.TypeInt64}, want: "NUMBER(19,0)"},
		{name: "decimal bounded", col: schema.Column{DataType: schema.TypeDecimal, Precision: 12, Scale: 2}, want: "NUMBER(12,2)"},
		{name: "decimal capped", col: schema.Column{DataType: schema.TypeDecimal, Precision: 60, Scale: 50}, want: "NUMBER(38,38)"},
		{name: "short string", col: schema.Column{DataType: schema.TypeString, MaxLength: 255}, want: "VARCHAR2(255 CHAR)"},
		{name: "long string", col: schema.Column{DataType: schema.TypeString}, want: "CLOB"},
		{name: "cdc lsn string", col: schema.Column{Name: "_cdc_lsn", DataType: schema.TypeString}, want: "VARCHAR2(4000 CHAR)"},
		{name: "timestamp tz", col: schema.Column{DataType: schema.TypeTimestampTZ}, want: "TIMESTAMP(6) WITH TIME ZONE"},
		{name: "json", col: schema.Column{DataType: schema.TypeJSON}, want: "CLOB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MapDataTypeToOracle(tt.col))
		})
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 100},
			{Name: "created_at", DataType: schema.TypeTimestampTZ},
		},
	}

	got := buildCreateTableSQL("analytics.users", tableSchema, []string{"id"})

	assert.Equal(t, `CREATE TABLE "ANALYTICS"."USERS" (
  "ID" NUMBER(19,0),
  "NAME" VARCHAR2(100 CHAR),
  "CREATED_AT" TIMESTAMP(6) WITH TIME ZONE,
  PRIMARY KEY ("ID")
)`, got)
}

func TestBuildCreateTableSQL_StringPrimaryKeyUsesVarchar(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "name", DataType: schema.TypeString},
		},
	}

	got := buildCreateTableSQL("users", tableSchema, []string{"id"})

	assert.Equal(t, `CREATE TABLE "USERS" (
  "ID" VARCHAR2(4000 CHAR),
  "NAME" CLOB,
  PRIMARY KEY ("ID")
)`, got)
}

func TestBuildCreateTableSQL_StringIncrementalKeyUsesVarchar(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "cursor", DataType: schema.TypeString},
			{Name: "name", DataType: schema.TypeString},
		},
		IncrementalKey: "cursor",
	}

	got := buildCreateTableSQL("users", tableSchema, []string{"id"})

	assert.Equal(t, `CREATE TABLE "USERS" (
  "ID" NUMBER(19,0),
  "CURSOR" VARCHAR2(4000 CHAR),
  "NAME" CLOB,
  PRIMARY KEY ("ID")
)`, got)
}

func TestBuildCreateTableSQL_SchemaPrimaryKeyUsesVarcharWithoutConstraint(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "name", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}

	got := buildCreateTableSQL("users_staging", tableSchema, nil)

	assert.Equal(t, `CREATE TABLE "USERS_STAGING" (
  "ID" VARCHAR2(4000 CHAR),
  "NAME" CLOB
)`, got)
}

func TestBuildCreateTableSQLKeepsQuotedCaseDistinctPayloadAsCLOB(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: `"Foo"`, DataType: schema.TypeString},
		{Name: `"foo"`, DataType: schema.TypeString},
	}}

	got := buildCreateTableSQL("items", tableSchema, []string{`"Foo"`})

	assert.Contains(t, got, `"Foo" VARCHAR2(4000 CHAR)`)
	assert.Contains(t, got, `"foo" CLOB`)
	assert.Contains(t, got, `PRIMARY KEY ("Foo")`)
}

func TestBuildMergeSQL(t *testing.T) {
	source := oracleDedupSource(
		[]string{"id", "name", "updated_at"},
		[]string{"id"},
		quoteTable("_bruin_staging.users_merge_123"),
		quoteColumn("updated_at")+" DESC",
		"",
		"source",
	)

	got := buildMergeSQL(
		"users",
		source,
		[]string{"id", "name", "updated_at"},
		[]string{"id"},
		[]string{"name", "updated_at"},
	)

	assert.Equal(t, `MERGE INTO "USERS" target
USING (SELECT "ID", "NAME", "UPDATED_AT" FROM (SELECT "ID", "NAME", "UPDATED_AT", ROW_NUMBER() OVER (PARTITION BY "ID" ORDER BY "UPDATED_AT" DESC) bruin_dedup_rn FROM "_BRUIN_STAGING"."USERS_MERGE_123") bruin_numbered WHERE bruin_dedup_rn = 1) source
ON (target."ID" = source."ID")
WHEN MATCHED THEN UPDATE SET target."NAME" = source."NAME", target."UPDATED_AT" = source."UPDATED_AT"
WHEN NOT MATCHED THEN INSERT ("ID", "NAME", "UPDATED_AT") VALUES (source."ID", source."NAME", source."UPDATED_AT")`, got)
}

func TestBuildMergeSQLWithIncrementalPredicate(t *testing.T) {
	source := oracleDedupSource(
		[]string{"id", "event_date"},
		[]string{"id"},
		quoteTable("users_staging"),
		"1",
		"",
		"source",
	)
	got := buildMergeSQLWithPredicate(
		"users",
		source,
		[]string{"id", "event_date"},
		[]string{"id"},
		[]string{"event_date"},
		`target."EVENT_DATE" >= TRUNC(SYSDATE) - 7`,
	)

	assert.Contains(t, got, `MERGE INTO (SELECT * FROM "USERS" target WHERE target."EVENT_DATE" >= TRUNC(SYSDATE) - 7) target`)
	assert.Contains(t, got, `ON (target."ID" = source."ID")`)
}

func TestBuildCDCDeleteTombstoneInsertSQL(t *testing.T) {
	source := oracleDedupSource(
		[]string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		[]string{"id"},
		quoteTable("users_staging"),
		destination.CDCLatestOverallOrderBy(quoteColumn),
		"",
		"source",
	)

	got := buildCDCDeleteTombstoneInsertSQL(
		"users",
		source,
		[]string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		[]string{"id"},
	)

	assert.Equal(t, `INSERT INTO "USERS" ("ID", "NAME", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT") SELECT source."ID", source."NAME", source."_CDC_LSN", source."_CDC_DELETED", source."_CDC_SYNCED_AT" FROM (SELECT "ID", "NAME", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT" FROM (SELECT "ID", "NAME", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", ROW_NUMBER() OVER (PARTITION BY "ID" ORDER BY "_CDC_LSN" DESC, "_CDC_DELETED" DESC) bruin_dedup_rn FROM "USERS_STAGING") bruin_numbered WHERE bruin_dedup_rn = 1) source WHERE source."_CDC_DELETED" = 1 AND NOT EXISTS (SELECT 1 FROM "USERS" target WHERE target."ID" = source."ID")`, got)
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
	source := oracleDedupSource(columns, []string{"id"}, quoteTable("items_staging"), destination.CDCLatestOverallOrderBy(quoteColumn), "", "source")
	got := buildMergeSQL("items", source, columns, []string{"id"}, filterColumns(destination.DestinationColumns(columns), []string{"id"}))

	assert.Contains(t, got, `JSON_TABLE(COALESCE(source."_CDC_UNCHANGED_COLS", '[]'), '$[*]'`)
	assert.Contains(t, got, `NLSSORT(jt.value, 'NLS_SORT=BINARY') = NLSSORT('payload', 'NLS_SORT=BINARY')`)
	assert.Contains(t, got, `THEN target."PAYLOAD" ELSE source."PAYLOAD" END`)
	assert.Contains(t, got, `INSERT ("ID", "PAYLOAD", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT")`)
	assert.NotContains(t, got, `INSERT ("ID", "PAYLOAD", "_CDC_LSN", "_CDC_DELETED", "_CDC_SYNCED_AT", "_CDC_UNCHANGED_COLS")`)
	assert.True(t, NewOracleDestination().SupportsCDCUnchangedCols())
}

func TestBuildCDCMergeSQLWithoutUnchangedColsMarkerUsesPlainUpdateSet(t *testing.T) {
	columns := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}
	source := oracleDedupSource(columns, []string{"id"}, quoteTable("items_staging"), destination.CDCLatestOverallOrderBy(quoteColumn), "", "source")
	got := buildMergeSQL("items", source, columns, []string{"id"}, filterColumns(destination.DestinationColumns(columns), []string{"id"}))

	assert.NotContains(t, got, "_CDC_UNCHANGED_COLS")
	assert.NotContains(t, got, "JSON_TABLE")
	assert.Contains(t, got, `target."PAYLOAD" = source."PAYLOAD"`)
}

func TestBuildCDCMergeSQLMatchesUnchangedMarkersCaseSensitively(t *testing.T) {
	columns := []string{"id", `"Foo"`, `"foo"`, destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
	source := oracleDedupSource(columns, []string{"id"}, quoteTable("items_staging"), destination.CDCLatestOverallOrderBy(quoteColumn), "", "source")
	got := buildMergeSQL("items", source, columns, []string{"id"}, filterColumns(destination.DestinationColumns(columns), []string{"id"}))

	assert.Contains(t, got, `NLSSORT(jt.value, 'NLS_SORT=BINARY') = NLSSORT('"Foo"', 'NLS_SORT=BINARY')`)
	assert.Contains(t, got, `NLSSORT(jt.value, 'NLS_SORT=BINARY') = NLSSORT('"foo"', 'NLS_SORT=BINARY')`)
	assert.Contains(t, got, `THEN target."Foo" ELSE source."Foo" END`)
	assert.Contains(t, got, `THEN target."foo" ELSE source."foo" END`)
	assert.NotContains(t, got, "LOWER(")
}

func TestBuildCDCMergeSQLKeepsCaseDistinctPayloadSeparateFromPrimaryKey(t *testing.T) {
	columns := []string{`"Foo"`, `"foo"`, destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
	source := oracleDedupSource(columns, []string{`"Foo"`}, quoteTable("items_staging"), destination.CDCLatestOverallOrderBy(quoteColumn), "", "source")
	got := buildMergeSQL("items", source, columns, []string{`"Foo"`}, filterColumns(destination.DestinationColumns(columns), []string{`"Foo"`}))

	assert.Contains(t, got, `ON (target."Foo" = source."Foo")`)
	assert.Contains(t, got, `target."foo" = CASE WHEN`)
	assert.Contains(t, got, `THEN target."foo" ELSE source."foo" END`)
	assert.NotContains(t, got, `target."Foo" = CASE`)
}

func TestFilterColumnsRetainsOrdinaryCaseInsensitiveOracleMatching(t *testing.T) {
	assert.Equal(t, []string{"Name"}, filterColumns([]string{"ID", "Name"}, []string{"id"}))
	assert.Equal(t, []string{`"foo"`}, filterColumns([]string{`"Foo"`, `"foo"`}, []string{`"Foo"`}))
}

func TestBuildChangeConditionsKeepsCaseDistinctLOBTypes(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: `"Foo"`, DataType: schema.TypeString, MaxLength: 100},
		{Name: `"foo"`, DataType: schema.TypeJSON},
	}}

	got := buildChangeConditions([]string{`"Foo"`, `"foo"`}, "target", "source", tableSchema)

	assert.Contains(t, got, `target."Foo" <> source."Foo"`)
	assert.Contains(t, got, `DBMS_LOB.COMPARE(target."foo", source."foo")`)
	assert.NotContains(t, got, `DBMS_LOB.COMPARE(target."Foo", source."Foo")`)
}

func TestMergeTableRequiresPrimaryKey(t *testing.T) {
	dest := NewOracleDestination()

	err := dest.MergeTable(context.Background(), destination.MergeOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary key")
}

func TestSCD2TableRequiresPrimaryKey(t *testing.T) {
	dest := NewOracleDestination()

	err := dest.SCD2Table(context.Background(), destination.SCD2Options{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary key")
}

func TestBuildChangeConditions_UsesLOBCompareForLOBColumns(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "name", DataType: schema.TypeString},
			{Name: "tags", DataType: schema.TypeArray},
			{Name: "payload", DataType: schema.TypeJSON},
			{Name: "blob_data", DataType: schema.TypeBinary},
		},
	}

	got := buildChangeConditions([]string{"name", "tags", "payload", "blob_data"}, "target", "source", tableSchema)

	assert.Contains(t, got, `DBMS_LOB.COMPARE(target."NAME", source."NAME") != 0`)
	assert.Contains(t, got, `DBMS_LOB.COMPARE(target."TAGS", source."TAGS") != 0`)
	assert.Contains(t, got, `DBMS_LOB.COMPARE(target."PAYLOAD", source."PAYLOAD") != 0`)
	assert.Contains(t, got, `DBMS_LOB.COMPARE(target."BLOB_DATA", source."BLOB_DATA") != 0`)
	assert.NotContains(t, got, "TO_CHAR")
}

func TestBuildChangeConditions_UsesDirectCompareForNonLOBColumns(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 100},
			{Name: "data", DataType: schema.TypeBinary, MaxLength: 100},
		},
	}

	got := buildChangeConditions([]string{"id", "name", "data"}, "target", "source", tableSchema)

	assert.Contains(t, got, `target."ID" <> source."ID"`)
	assert.Contains(t, got, `target."NAME" <> source."NAME"`)
	assert.Contains(t, got, `target."DATA" <> source."DATA"`)
	assert.NotContains(t, got, "DBMS_LOB.COMPARE")
	assert.NotContains(t, got, "TO_CHAR")
}

func TestOracleReplaceUsesStagedPath(t *testing.T) {
	dest := &OracleDestination{currentUser: "INGESTR"}

	assert.True(t, dest.SupportsAtomicSwap())

	policy := dest.ReplaceStagingPolicy()
	assert.Equal(t, destination.ReplaceStagingTargetSchema, policy.DefaultPlacement)
	assert.Equal(t, "INGESTR", policy.DefaultTargetSchema)
	assert.Equal(t, defaultOracleStagingSchema, policy.DefaultManagedSchema)
}

func TestOracleManagedStagingUsesCurrentUserByDefault(t *testing.T) {
	dest := &OracleDestination{currentUser: "INGESTR"}

	policy := dest.ManagedStagingPolicy()

	assert.Equal(t, destination.ReplaceStagingTargetSchema, policy.DefaultPlacement)
	assert.Equal(t, "INGESTR", policy.DefaultTargetSchema)
	assert.Equal(t, defaultOracleStagingSchema, policy.DefaultManagedSchema)
}

func TestBackupTableName(t *testing.T) {
	got := backupTableName("hr", "employees")

	assert.True(t, strings.HasPrefix(got, "hr.EMPLOYEES_OLD_"), got)
	_, tableName := parseTableName(got)
	assert.LessOrEqual(t, len(tableName), destination.MaxIdentifierLength("oracle"))

	quoted := backupTableName(`"appUser"`, `"order.events"`)
	parts := splitOracleIdentifiers(quoted)
	require.Len(t, parts, 2)
	require.Equal(t, `"appUser"`, parts[0])
	require.Contains(t, parts[1], `"order.events_OLD_`)
	require.NotEqual(t, "ORDER", canonicalIdentifier(parts[1]))
	require.Equal(t, 2, len(splitOracleIdentifiers(quoteTable(quoted))))
}

func TestOracleSwapRejectsOverQualifiedNames(t *testing.T) {
	dest := &OracleDestination{}
	err := dest.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "app.staging", TargetTable: "catalog.app.orders"})
	require.ErrorContains(t, err, "oracle table name")
}

func TestOracleSwapQuotedDotTargetDoesNotAddressUnrelatedOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	dest := &OracleDestination{db: db, currentUser: "appUser"}
	countQuery := regexp.QuoteMeta("SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER = :1 AND TABLE_NAME = :2")
	mock.ExpectQuery(countQuery).WithArgs("appUser", "order.events").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(countQuery).WithArgs("appUser", oracleDottedBackupArgument{}).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`ALTER TABLE "appUser"\."order\.events" RENAME TO "order\.events_OLD_[0-9]+"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ALTER TABLE "appUser"\."staging_for_order_events" RENAME TO "order\.events"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(countQuery).WithArgs("appUser", oracleDottedBackupArgument{}).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(`DROP TABLE "appUser"\."order\.events_OLD_[0-9]+" PURGE`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	require.NoError(t, dest.SwapTable(t.Context(), destination.SwapOptions{
		StagingTable: `"appUser"."staging_for_order_events"`,
		TargetTable:  `"appUser"."order.events"`,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOracleDialectDoesNotSupportDirectTypeAlter(t *testing.T) {
	dialect := &Dialect{}

	assert.False(t, dialect.SupportsAlterType())
	assert.Empty(t, dialect.AlterColumnTypeSQL("users", "age", schema.Column{
		Name:     "age",
		DataType: schema.TypeString,
	}))
}

func TestOracleDialectTypeName_PrimaryKeyStringUsesVarchar(t *testing.T) {
	dialect := &Dialect{}

	assert.Equal(t, "VARCHAR2(4000 CHAR)", dialect.TypeName(schema.Column{
		Name:         "id",
		DataType:     schema.TypeString,
		IsPrimaryKey: true,
	}))
	assert.Equal(t, "CLOB", dialect.TypeName(schema.Column{
		Name:     "description",
		DataType: schema.TypeString,
	}))
}

func TestOracleDialectAddColumnSQL_StringIncrementalKeyMetadataUsesVarchar(t *testing.T) {
	dialect := &Dialect{}

	got := dialect.AddColumnSQL("users", schema.Column{
		Name:      "cursor",
		DataType:  schema.TypeString,
		MaxLength: oracleDefaultComparableStringLength,
		Nullable:  true,
	})

	assert.Equal(t, `ALTER TABLE "USERS" ADD "CURSOR" VARCHAR2(4000 CHAR)`, got)
}

func TestOracleMaxCDCLSNQueries(t *testing.T) {
	assert.Equal(
		t,
		`SELECT MAX("_CDC_LSN") FROM "USERS"`,
		oracleMaxCDCLSNQuery("users"),
	)
	assert.Equal(
		t,
		`SELECT MAX(DBMS_LOB.SUBSTR("_CDC_LSN", 4000, 1)) FROM "USERS"`,
		oracleMaxCDCLSNLobQuery("users"),
	)
}

func TestExtractValue_ListUsesRowValue(t *testing.T) {
	builder := array.NewListBuilder(memory.DefaultAllocator, arrow.PrimitiveTypes.Int64)
	values := builder.ValueBuilder().(*array.Int64Builder)
	builder.Append(true)
	values.AppendValues([]int64{1, 2}, nil)
	builder.Append(true)
	values.AppendValues([]int64{3}, nil)
	arr := builder.NewArray()
	defer arr.Release()
	builder.Release()

	assert.Equal(t, "[1,2]", extractValue(arr, 0))
	assert.Equal(t, "[3]", extractValue(arr, 1))
}
