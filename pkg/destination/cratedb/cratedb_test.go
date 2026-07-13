package cratedb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
)

func TestSchemes(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	schemes := d.Schemes()
	assert.Equal(t, []string{"cratedb"}, schemes)
}

func TestGetScheme(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	assert.Equal(t, "cratedb", d.GetScheme())
}

func TestManagedCDCStateFailsWithoutDurableIncarnation(t *testing.T) {
	d := NewCrateDBDestination()
	assert.ErrorContains(t, d.ValidateManagedCDCState(), "does not expose a durable physical table incarnation")
}

func TestStrategySupport(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	assert.True(t, d.SupportsReplaceStrategy())
	assert.True(t, d.SupportsAppendStrategy())
	assert.True(t, d.SupportsMergeStrategy())
	assert.False(t, d.SupportsDeleteInsertStrategy())
	assert.True(t, d.SupportsSCD2Strategy())
	assert.False(t, d.SupportsAtomicSwap())
}

func TestDeleteInsertUnsupported(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	err := d.DeleteInsertTable(t.Context(), destination.DeleteInsertOptions{})
	if err == nil || !strings.Contains(err.Error(), "delete+insert strategy is not supported") {
		t.Fatalf("DeleteInsertTable error = %v, want unsupported error", err)
	}
}

func TestWriteCDCStateRequiresSuccessfulRefresh(t *testing.T) {
	wantErr := errors.New("refresh unavailable")
	d := NewCrateDBDestination()
	d.cdcStateRefreshOverride = func(_ context.Context, table string) error {
		if table != "_bruin_staging.cdc_state" {
			t.Fatalf("refresh table = %q", table)
		}
		return wantErr
	}
	records := make(chan source.RecordBatchResult)
	close(records)

	err := d.WriteCDCState(t.Context(), records, destination.WriteOptions{Table: "_bruin_staging.cdc_state"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteCDCState() error = %v, want refresh error", err)
	}
}

func TestWriteCDCStateAcceptsSuccessfulRefresh(t *testing.T) {
	d := NewCrateDBDestination()
	refreshed := false
	d.cdcStateRefreshOverride = func(_ context.Context, _ string) error {
		refreshed = true
		return nil
	}
	records := make(chan source.RecordBatchResult)
	close(records)

	if err := d.WriteCDCState(t.Context(), records, destination.WriteOptions{Table: "_bruin_staging.cdc_state"}); err != nil {
		t.Fatalf("WriteCDCState() error = %v", err)
	}
	if !refreshed {
		t.Fatal("WriteCDCState() did not require a refresh")
	}
}

func TestBeginTransactionUnsupported(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	tx, err := d.BeginTransaction(t.Context())
	if err == nil || !strings.Contains(err.Error(), "transactions are not supported") {
		t.Fatalf("BeginTransaction error = %v, want unsupported error", err)
	}
	if tx != nil {
		t.Fatalf("BeginTransaction tx = %T, want nil", tx)
	}
}

func TestParseSchemaTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input          string
		expectedSchema string
		expectedTable  string
	}{
		{"my_table", "doc", "my_table"},
		{"my_schema.my_table", "my_schema", "my_table"},
		{"doc.users", "doc", "users"},
		{`"doc"."users"`, "doc", "users"},
	}

	for _, tt := range tests {
		s, tbl := parseSchemaTable(tt.input)
		assert.Equal(t, tt.expectedSchema, s)
		assert.Equal(t, tt.expectedTable, tbl)
	}
}

func TestBuildCreateTableSQL(t *testing.T) {
	t.Parallel()
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "active", DataType: schema.TypeBoolean},
	}

	sql := buildCreateTableSQL(`"doc"."users"`, columns, []string{"id"})
	assert.Contains(t, sql, `CREATE TABLE IF NOT EXISTS "doc"."users"`)
	assert.Contains(t, sql, `"id" BIGINT`)
	assert.Contains(t, sql, `"name" TEXT`)
	assert.Contains(t, sql, `"active" BOOLEAN`)
	assert.Contains(t, sql, `PRIMARY KEY ("id")`)
}

func TestBuildCreateTableSQLNoPrimaryKey(t *testing.T) {
	t.Parallel()
	columns := []schema.Column{
		{Name: "value", DataType: schema.TypeFloat64},
	}

	sql := buildCreateTableSQL(`"doc"."metrics"`, columns, nil)
	assert.Contains(t, sql, `CREATE TABLE IF NOT EXISTS "doc"."metrics"`)
	assert.NotContains(t, sql, "PRIMARY KEY")
}

func TestMapDataTypeToCrateDB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		col      schema.Column
		expected string
	}{
		{schema.Column{DataType: schema.TypeBoolean}, "BOOLEAN"},
		{schema.Column{DataType: schema.TypeInt16}, "BIGINT"},
		{schema.Column{DataType: schema.TypeInt32}, "BIGINT"},
		{schema.Column{DataType: schema.TypeInt64}, "BIGINT"},
		{schema.Column{DataType: schema.TypeFloat32}, "DOUBLE PRECISION"},
		{schema.Column{DataType: schema.TypeFloat64}, "DOUBLE PRECISION"},
		{schema.Column{DataType: schema.TypeDecimal, Precision: 10, Scale: 2}, "NUMERIC(10,2)"},
		{schema.Column{DataType: schema.TypeDecimal}, "NUMERIC"},
		{schema.Column{DataType: schema.TypeString}, "TEXT"},
		{schema.Column{DataType: schema.TypeBinary}, "TEXT"},
		{schema.Column{DataType: schema.TypeDate}, "TIMESTAMP WITHOUT TIME ZONE"},
		{schema.Column{DataType: schema.TypeTime}, "TEXT"},
		{schema.Column{DataType: schema.TypeTimestamp}, "TIMESTAMP WITHOUT TIME ZONE"},
		{schema.Column{DataType: schema.TypeTimestampTZ}, "TIMESTAMP WITH TIME ZONE"},
		{schema.Column{DataType: schema.TypeInterval}, "TEXT"},
		{schema.Column{DataType: schema.TypeJSON}, "TEXT"},
		{schema.Column{DataType: schema.TypeUUID}, "TEXT"},
		{schema.Column{DataType: schema.TypeArray, ArrayType: schema.TypeInt64}, "ARRAY(BIGINT)"},
	}

	for _, tt := range tests {
		result := mapDataTypeToCrateDB(tt.col)
		assert.Equal(t, tt.expected, result, "for type %v", tt.col.DataType)
	}
}

func TestMapCrateDBTypeToSchema(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected schema.DataType
	}{
		{"boolean", schema.TypeBoolean},
		{"smallint", schema.TypeInt16},
		{"short", schema.TypeInt16},
		{"integer", schema.TypeInt32},
		{"int", schema.TypeInt32},
		{"bigint", schema.TypeInt64},
		{"long", schema.TypeInt64},
		{"real", schema.TypeFloat32},
		{"float", schema.TypeFloat32},
		{"double precision", schema.TypeFloat64},
		{"double", schema.TypeFloat64},
		{"numeric", schema.TypeDecimal},
		{"text", schema.TypeString},
		{"varchar(255)", schema.TypeString},
		{"character varying", schema.TypeString},
		{"timestamp without time zone", schema.TypeTimestamp},
		{"timestamp with time zone", schema.TypeTimestampTZ},
		{"object", schema.TypeJSON},
		{"object(dynamic)", schema.TypeJSON},
		{"array(bigint)", schema.TypeArray},
	}

	for _, tt := range tests {
		result := mapCrateDBTypeToSchema(tt.input)
		assert.Equal(t, tt.expected, result, "for input %q", tt.input)
	}
}

func TestQuoteColumns(t *testing.T) {
	t.Parallel()
	result := quoteColumns([]string{"id", "name", "created_at"})
	assert.Equal(t, []string{`"id"`, `"name"`, `"created_at"`}, result)
}

func TestConnectRejectsRemoteWithoutPassword(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	err := d.Connect(t.Context(), "cratedb://crate@some-cloud-host.cratedb.net:5432/?sslmode=require")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "password is required for non-local CrateDB connections")
}

func TestConnectAllowsLocalhostWithoutPassword(t *testing.T) {
	t.Parallel()
	d := NewCrateDBDestination()
	err := d.Connect(t.Context(), "cratedb://crate@localhost:5432/?sslmode=disable")
	if err != nil {
		assert.NotContains(t, err.Error(), "password is required")
	}
}

func TestFilterColumns(t *testing.T) {
	t.Parallel()
	result := filterColumns([]string{"id", "name", "email"}, []string{"id"})
	assert.Equal(t, []string{"name", "email"}, result)
}

func TestBuildConflictUpdateSet(t *testing.T) {
	t.Parallel()
	result := buildConflictUpdateSet([]string{"name", "email"})
	assert.Equal(t, `"name" = EXCLUDED."name", "email" = EXCLUDED."email"`, result)
}

func TestBuildConcatExpr(t *testing.T) {
	t.Parallel()

	t.Run("single key no prefix", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, `"id"`, buildConcatExpr([]string{"id"}, ""))
	})

	t.Run("single key with prefix", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, `s."id"`, buildConcatExpr([]string{"id"}, "s"))
	})

	t.Run("composite key no prefix", func(t *testing.T) {
		t.Parallel()
		result := buildConcatExpr([]string{"org_id", "user_id"}, "")
		assert.Equal(t, `CAST("org_id" AS TEXT) || '~' || CAST("user_id" AS TEXT)`, result)
	})

	t.Run("composite key with prefix", func(t *testing.T) {
		t.Parallel()
		result := buildConcatExpr([]string{"org_id", "user_id"}, "s")
		assert.Equal(t, `CAST(s."org_id" AS TEXT) || '~' || CAST(s."user_id" AS TEXT)`, result)
	})
}

func TestArrowFieldToCrateDBArrayCast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		field    arrow.Field
		expected string
	}{
		{"bool", arrow.Field{Type: arrow.FixedWidthTypes.Boolean}, "boolean[]"},
		{"int16", arrow.Field{Type: arrow.PrimitiveTypes.Int16}, "bigint[]"},
		{"int32", arrow.Field{Type: arrow.PrimitiveTypes.Int32}, "bigint[]"},
		{"int64", arrow.Field{Type: arrow.PrimitiveTypes.Int64}, "bigint[]"},
		{"float32", arrow.Field{Type: arrow.PrimitiveTypes.Float32}, "double precision[]"},
		{"float64", arrow.Field{Type: arrow.PrimitiveTypes.Float64}, "double precision[]"},
		{"string", arrow.Field{Type: arrow.BinaryTypes.String}, "text[]"},
		{"date32", arrow.Field{Type: arrow.FixedWidthTypes.Date32}, "timestamp without time zone[]"},
		{"timestamp_no_tz", arrow.Field{Type: &arrow.TimestampType{Unit: arrow.Microsecond}}, "timestamp without time zone[]"},
		{"timestamp_with_tz", arrow.Field{Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}}, "timestamp with time zone[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, arrowFieldToCrateDBArrayCast(tt.field))
		})
	}
}
