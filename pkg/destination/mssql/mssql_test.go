package mssql

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	mssqldb "github.com/microsoft/go-mssqldb"
)

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
