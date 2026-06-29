package oracle

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quoteTable(tt.table))
		})
	}
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
