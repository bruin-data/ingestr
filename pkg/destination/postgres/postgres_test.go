package postgres

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type postgresStatementDescriberStub struct {
	name      string
	sql       string
	paramOIDs []uint32
}

func (s *postgresStatementDescriberStub) Prepare(_ context.Context, name, sql string, paramOIDs []uint32) (*pgconn.StatementDescription, error) {
	s.name = name
	s.sql = sql
	s.paramOIDs = paramOIDs
	return &pgconn.StatementDescription{}, nil
}

type postgresResolverStub struct {
	query  string
	schema string
	table  string
}

func (s *postgresResolverStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	s.query = query
	return postgresResolverRow{schema: s.schema, table: s.table}
}

type postgresResolverRow struct {
	schema string
	table  string
}

func (r postgresResolverRow) Scan(dest ...any) error {
	*dest[0].(*string) = r.schema
	*dest[1].(*string) = r.table
	return nil
}

func TestResolveSchemaTableUsesSearchPathForUnqualifiedTarget(t *testing.T) {
	dest := NewPostgresDestination()
	resolver := &postgresResolverStub{schema: "tenant_a", table: "orders"}

	schemaName, tableName, err := dest.resolveSchemaTable(t.Context(), resolver, "orders")
	if err != nil {
		t.Fatal(err)
	}
	if schemaName != "tenant_a" || tableName != "orders" {
		t.Fatalf("resolveSchemaTable() = %q.%q", schemaName, tableName)
	}
	if !strings.Contains(resolver.query, "to_regclass($1)") || !strings.Contains(resolver.query, "current_schema()") {
		t.Fatalf("resolver query does not honor existing table resolution and search_path: %s", resolver.query)
	}
}

func TestBuildPredicateMergeSQLUsesPredicateForUpdateAndInsertMatch(t *testing.T) {
	predicate := `target."event_date" >= CURRENT_DATE - INTERVAL '7 days'`
	sql := buildPredicateMergeSQL(
		`"public"."events"`,
		`"staging"."events"`,
		[]string{"id"},
		quoteColumns([]string{"id", "event_date", "value"}),
		[]string{"event_date", "value"},
		`"id"`,
		predicate,
	)

	matchCondition := `target."id" = source."id" AND (` + predicate + `)`
	if count := strings.Count(sql, matchCondition); count != 2 {
		t.Fatalf("predicate match condition appears %d times, want 2:\n%s", count, sql)
	}
	if !strings.Contains(sql, `UPDATE "public"."events" AS target`) {
		t.Fatalf("expected target update in predicate merge SQL:\n%s", sql)
	}
	if !strings.Contains(sql, `WHERE NOT EXISTS (SELECT 1 FROM "public"."events" AS target WHERE `+matchCondition+`)`) {
		t.Fatalf("expected guarded insert in predicate merge SQL:\n%s", sql)
	}
	if strings.Contains(sql, "ON CONFLICT") {
		t.Fatalf("predicate merge SQL must not use ON CONFLICT:\n%s", sql)
	}
}

func TestDescribePostgresStatementUsesAnonymousPreparedStatement(t *testing.T) {
	describer := &postgresStatementDescriberStub{}
	const sql = `select "id" from "public"."events"`

	if _, err := describePostgresStatement(t.Context(), describer, sql); err != nil {
		t.Fatal(err)
	}
	if describer.name != "" {
		t.Fatalf("prepared statement name = %q, want anonymous statement", describer.name)
	}
	if describer.sql != sql {
		t.Fatalf("prepared statement SQL = %q, want %q", describer.sql, sql)
	}
	if describer.paramOIDs != nil {
		t.Fatalf("prepared statement parameter OIDs = %v, want nil", describer.paramOIDs)
	}
}

func TestClaimCDCTargetRejectsInvalidNameBeforeTransaction(t *testing.T) {
	dest := NewPostgresDestination()
	claim := func(table string) destination.CDCTargetClaim {
		return destination.CDCTargetClaim{DestinationTable: table, ConnectorID: "connector-a", SourceTable: "public.orders"}
	}

	for _, table := range []string{
		"db.public.orders",
		strings.Repeat("s", 64) + ".orders",
		"public." + strings.Repeat("t", 64),
	} {
		t.Run(table, func(t *testing.T) {
			err := dest.ClaimCDCTarget(t.Context(), "_bruin_staging.cdc_targets", claim(table))
			if err == nil {
				t.Fatal("ClaimCDCTarget() returned nil, want validation error before transaction")
			}
		})
	}
}

func TestValidatePostgresClaimTargetAcceptsIdentifierLimit(t *testing.T) {
	wantSchema := strings.Repeat("s", 63)
	wantTable := strings.Repeat("t", 63)
	parts, err := validatePostgresClaimTarget(wantSchema + "." + wantTable)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parts, []string{wantSchema, wantTable}) {
		t.Fatalf("validatePostgresClaimTarget() = %v", parts)
	}
}

func TestMapPostgresColumnTypePreservesArrayElementType(t *testing.T) {
	tests := []struct {
		udt  string
		want schema.DataType
	}{
		{"_bool", schema.TypeBoolean},
		{"_int2", schema.TypeInt16},
		{"_int4", schema.TypeInt32},
		{"_int8", schema.TypeInt64},
		{"_float4", schema.TypeFloat32},
		{"_float8", schema.TypeFloat64},
		{"_numeric", schema.TypeDecimal},
		{"_text", schema.TypeString},
		{"_varchar", schema.TypeString},
		{"_bpchar", schema.TypeString},
		{"_bytea", schema.TypeBinary},
		{"_date", schema.TypeDate},
		{"_time", schema.TypeTime},
		{"_timestamp", schema.TypeTimestamp},
		{"_timestamptz", schema.TypeTimestampTZ},
		{"_interval", schema.TypeInterval},
		{"_json", schema.TypeJSON},
		{"_jsonb", schema.TypeJSON},
		{"_uuid", schema.TypeUUID},
		{"_custom_enum", schema.TypeString},
	}

	for _, tt := range tests {
		t.Run(tt.udt, func(t *testing.T) {
			dataType, arrayType := mapPostgresColumnType("ARRAY", tt.udt)
			if dataType != schema.TypeArray || arrayType != tt.want {
				t.Fatalf("mapPostgresColumnType(ARRAY, %q) = (%v, %v), want (%v, %v)", tt.udt, dataType, arrayType, schema.TypeArray, tt.want)
			}
		})
	}
}

func TestMapPostgresColumnTypeScalarHasNoArrayElement(t *testing.T) {
	dataType, arrayType := mapPostgresColumnType("integer", "int4")
	if dataType != schema.TypeInt32 || arrayType != schema.TypeUnknown {
		t.Fatalf("mapPostgresColumnType(integer, int4) = (%v, %v), want (%v, %v)", dataType, arrayType, schema.TypeInt32, schema.TypeUnknown)
	}
}

func TestResolvedMultiTableNameFitsPostgresClaimLimit(t *testing.T) {
	sourceTable := strings.Repeat("s", 40) + "." + strings.Repeat("t", 40)
	resolved := destination.ResolveMultiTableName("postgres", nil, "landing", sourceTable)
	parts, err := validatePostgresClaimTarget(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0] != "landing" || len(parts[1]) > destination.MaxIdentifierLength("postgres") {
		t.Fatalf("resolved PostgreSQL multi-table target = %q (%v)", resolved, parts)
	}
}

func TestBuildDeleteInsertLockSQL(t *testing.T) {
	got := buildDeleteInsertLockSQL(`"public"."orders"`)
	want := `LOCK TABLE "public"."orders" IN EXCLUSIVE MODE`
	if got != want {
		t.Fatalf("buildDeleteInsertLockSQL() = %q, want %q", got, want)
	}
}

func TestParseSchemaTableCanonicalizesQuotedComponents(t *testing.T) {
	schemaName, tableName := parseSchemaTable(`"public"."orders"`)
	if schemaName != "public" || tableName != "orders" {
		t.Fatalf("parseSchemaTable() = %q.%q", schemaName, tableName)
	}
}

func TestQuotePostgresTablePreservesDottedIdentifierBoundary(t *testing.T) {
	got := quotePostgresTable("tenant", "order.events_old_123")
	if got != `"tenant"."order.events_old_123"` {
		t.Fatalf("quotePostgresTable() = %q", got)
	}
}

func TestPostgresSwapRejectsOverQualifiedNames(t *testing.T) {
	dest := NewPostgresDestination()
	err := dest.SwapTable(t.Context(), destination.SwapOptions{StagingTable: "public.staging", TargetTable: "database.public.orders"})
	if err == nil || !strings.Contains(err.Error(), "postgres table name") {
		t.Fatalf("SwapTable() error = %v", err)
	}
}

func TestPostgresValueGetterMatchesArrowutilValue(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	arrays := []arrow.Array{
		buildBoolArray(mem),
		buildInt16Array(mem),
		buildInt32Array(mem),
		buildInt64Array(mem),
		buildFloat32Array(mem),
		buildFloat64Array(mem),
		buildStringArray(mem),
		buildLargeStringArray(mem),
		buildBinaryArray(mem),
		buildDecimal128Array(mem),
		buildDate32Array(mem),
		buildDate64Array(mem),
		buildTime64Array(mem),
		buildTimestampArray(mem),
		buildJSONExtensionArray(mem),
		buildUint8FallbackArray(mem),
	}
	for _, arr := range arrays {
		defer arr.Release()
	}

	for _, arr := range arrays {
		get := postgresValueGetter(arr)
		for i := 0; i < arr.Len(); i++ {
			got := get(i)
			want := arrowutil.Value(arr, i)
			if !postgresTestValuesEqual(got, want) {
				t.Fatalf("%s[%d]: postgresValueGetter = %#v (%T), arrowutil.Value = %#v (%T)", arr.DataType(), i, got, got, want, want)
			}
		}
	}
}

func TestDialectRelaxColumnNullabilitySQL(t *testing.T) {
	got := (&Dialect{}).RelaxColumnNullabilitySQL("public.events", "Payload")
	want := `ALTER TABLE "public"."events" ALTER COLUMN "Payload" DROP NOT NULL`
	if got != want {
		t.Fatalf("RelaxColumnNullabilitySQL() = %q, want %q", got, want)
	}
}

func TestPostgresValueGettersConvertsUUIDColumns(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.Append("0f364130-da0b-4909-b824-5413d795aa93")
	b.AppendNull()
	uuidArray := b.NewArray()
	defer uuidArray.Release()

	recordSchema := arrow.NewSchema([]arrow.Field{{
		Name:     "uuid_col",
		Type:     arrow.BinaryTypes.String,
		Nullable: true,
	}}, nil)
	record := array.NewRecordBatch(recordSchema, []arrow.Array{uuidArray}, 2)
	defer record.Release()

	getters := postgresValueGetters(record, &schema.TableSchema{
		Columns: []schema.Column{{Name: "uuid_col", DataType: schema.TypeUUID, Nullable: true}},
	})

	got := getters[0](0)
	uuid, ok := got.(pgtype.UUID)
	if !ok {
		t.Fatalf("UUID getter returned %T, want pgtype.UUID", got)
	}
	if uuid.String() != "0f364130-da0b-4909-b824-5413d795aa93" {
		t.Fatalf("UUID getter returned %q", uuid.String())
	}
	if got := getters[0](1); got != nil {
		t.Fatalf("UUID getter null = %#v, want nil", got)
	}
}

func TestPostgresValueGetterPreservesDecimalPrecision(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	dt := &arrow.Decimal128Type{Precision: 38, Scale: 4}
	b := array.NewDecimal128Builder(mem, dt)
	defer b.Release()
	want, err := decimal128.FromString("123456789012345678901234567890.1234", dt.Precision, dt.Scale)
	if err != nil {
		t.Fatal(err)
	}
	b.Append(want)
	b.AppendNull()
	arr := b.NewArray()
	defer arr.Release()

	get := postgresValueGetter(arr)
	got, ok := get(0).(pgtype.Numeric)
	if !ok {
		t.Fatalf("decimal getter returned %T, want pgtype.Numeric", get(0))
	}
	if !got.Valid || got.Exp != -dt.Scale || got.Int.Cmp(want.BigInt()) != 0 {
		t.Fatalf("decimal getter returned %#v, want unscaled %s with exponent %d", got, want.BigInt(), -dt.Scale)
	}
	if got := get(1); got != nil {
		t.Fatalf("decimal getter null = %#v, want nil", got)
	}
}

func postgresTestValuesEqual(left, right any) bool {
	switch l := left.(type) {
	case []byte:
		r, ok := right.([]byte)
		return ok && bytes.Equal(l, r)
	case time.Time:
		r, ok := right.(time.Time)
		return ok && l.Equal(r)
	case pgtype.Numeric:
		r, ok := right.(float64)
		if !ok {
			return false
		}
		converted, err := l.Float64Value()
		return err == nil && converted.Valid && converted.Float64 == r
	default:
		return reflect.DeepEqual(left, right)
	}
}

func buildBoolArray(mem memory.Allocator) arrow.Array {
	b := array.NewBooleanBuilder(mem)
	defer b.Release()
	b.AppendValues([]bool{true, false}, []bool{true, false})
	return b.NewArray()
}

func buildInt16Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt16Builder(mem)
	defer b.Release()
	b.AppendValues([]int16{12, 0}, []bool{true, false})
	return b.NewArray()
}

func buildInt32Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt32Builder(mem)
	defer b.Release()
	b.AppendValues([]int32{34, 0}, []bool{true, false})
	return b.NewArray()
}

func buildInt64Array(mem memory.Allocator) arrow.Array {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues([]int64{56, 0}, []bool{true, false})
	return b.NewArray()
}

func buildFloat32Array(mem memory.Allocator) arrow.Array {
	b := array.NewFloat32Builder(mem)
	defer b.Release()
	b.AppendValues([]float32{1.25, 0}, []bool{true, false})
	return b.NewArray()
}

func buildFloat64Array(mem memory.Allocator) arrow.Array {
	b := array.NewFloat64Builder(mem)
	defer b.Release()
	b.AppendValues([]float64{2.5, 0}, []bool{true, false})
	return b.NewArray()
}

func buildStringArray(mem memory.Allocator) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.AppendValues([]string{"hello", ""}, []bool{true, false})
	return b.NewArray()
}

func buildLargeStringArray(mem memory.Allocator) arrow.Array {
	b := array.NewLargeStringBuilder(mem)
	defer b.Release()
	b.AppendValues([]string{"large", ""}, []bool{true, false})
	return b.NewArray()
}

func buildBinaryArray(mem memory.Allocator) arrow.Array {
	b := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	defer b.Release()
	b.Append([]byte{1, 2, 3})
	b.AppendNull()
	return b.NewArray()
}

func buildDecimal128Array(mem memory.Allocator) arrow.Array {
	dt := &arrow.Decimal128Type{Precision: 10, Scale: 2}
	b := array.NewDecimal128Builder(mem, dt)
	defer b.Release()
	b.Append(decimal128.FromI64(12345))
	b.AppendNull()
	return b.NewArray()
}

func buildDate32Array(mem memory.Allocator) arrow.Array {
	b := array.NewDate32Builder(mem)
	defer b.Release()
	b.Append(arrow.Date32FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
	b.AppendNull()
	return b.NewArray()
}

func buildDate64Array(mem memory.Allocator) arrow.Array {
	b := array.NewDate64Builder(mem)
	defer b.Release()
	b.Append(arrow.Date64FromTime(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)))
	b.AppendNull()
	return b.NewArray()
}

func buildTime64Array(mem memory.Allocator) arrow.Array {
	b := array.NewTime64Builder(mem, arrow.FixedWidthTypes.Time64us.(*arrow.Time64Type))
	defer b.Release()
	b.Append(arrow.Time64((1*3600+2*60+3)*1_000_000 + 4))
	b.AppendNull()
	return b.NewArray()
}

func buildTimestampArray(mem memory.Allocator) arrow.Array {
	dt := arrow.FixedWidthTypes.Timestamp_us.(*arrow.TimestampType)
	b := array.NewTimestampBuilder(mem, dt)
	defer b.Release()
	ts, err := arrow.TimestampFromTime(time.Date(2021, 3, 4, 5, 6, 7, 123000, time.UTC), arrow.Microsecond)
	if err != nil {
		panic(err)
	}
	b.Append(ts)
	b.AppendNull()
	return b.NewArray()
}

func buildJSONExtensionArray(mem memory.Allocator) arrow.Array {
	b := schema.NewJSONBuilder(mem)
	defer b.Release()
	b.Append(`{"a":1}`)
	b.AppendNull()
	return b.NewArray()
}

func buildUint8FallbackArray(mem memory.Allocator) arrow.Array {
	b := array.NewUint8Builder(mem)
	defer b.Release()
	b.AppendValues([]uint8{7, 0}, []bool{true, false})
	return b.NewArray()
}
