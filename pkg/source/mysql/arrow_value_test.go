package mysql

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type testDriverRows struct {
	columns []string
	values  [][]driver.Value
	next    int
	closed  bool
}

func (r *testDriverRows) Columns() []string { return r.columns }

func (r *testDriverRows) Close() error {
	r.closed = true
	return nil
}

func (r *testDriverRows) Next(dest []driver.Value) error {
	if r.next == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.next])
	r.next++
	return nil
}

func TestDriverRowsToArrowRecordBatch(t *testing.T) {
	columns := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "name", DataType: schema.TypeString},
		{Name: "amount", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
		{Name: "enabled", DataType: schema.TypeBoolean},
		{Name: "created_date", DataType: schema.TypeDate},
		{Name: "created_at", DataType: schema.TypeTimestamp},
		{Name: "payload", DataType: schema.TypeJSON},
	}
	now := time.Date(2026, 7, 15, 12, 34, 56, 123456000, time.UTC)
	rows := &testDriverRows{
		columns: []string{"id", "name", "amount", "enabled", "created_date", "created_at", "payload"},
		values: [][]driver.Value{
			{int64(1), []byte("first"), []byte("123.4567"), int64(1), now, now, []byte(`{"id":1}`)},
			{int64(2), []byte{}, []byte("-0.0001"), int64(0), now.AddDate(0, 0, 1), now.Add(time.Second), nil},
		},
	}
	dataCapacities := make([]int, len(columns))
	record, count, err := driverRowsToArrowRecordBatch(rows, buildArrowSchema(columns), columns, 25_000, dataCapacities)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Release()
	if count != 2 || record.NumRows() != 2 {
		t.Fatalf("row count = %d/%d, want 2", count, record.NumRows())
	}
	if got := record.Column(0).(*array.Int64).Value(1); got != 2 {
		t.Fatalf("id = %d, want 2", got)
	}
	names := record.Column(1).(*array.String)
	if names.Value(0) != "first" || names.IsNull(1) || names.Value(1) != "" {
		t.Fatalf("names = %v, want first and non-null empty", names)
	}
	amounts := record.Column(2).(*array.Decimal128)
	if got := amounts.Value(0).ToString(4); got != "123.4567" {
		t.Fatalf("amount = %s, want 123.4567", got)
	}
	enabled := record.Column(3).(*array.Boolean)
	if !enabled.Value(0) || enabled.Value(1) {
		t.Fatalf("enabled = %v/%v, want true/false", enabled.Value(0), enabled.Value(1))
	}
	if got := record.Column(4).(*array.Date32).Value(0); got != arrow.Date32FromTime(now) {
		t.Fatalf("date = %d, want %d", got, arrow.Date32FromTime(now))
	}
	if got := record.Column(5).(*array.Timestamp).Value(0); got != arrow.Timestamp(now.UnixMicro()) {
		t.Fatalf("timestamp = %d, want %d", got, now.UnixMicro())
	}
	jsonStorage := record.Column(6).(array.ExtensionArray).Storage().(*array.String)
	if jsonStorage.Value(0) != `{"id":1}` || !jsonStorage.IsNull(1) {
		t.Fatalf("JSON values = %v, want value and null", jsonStorage)
	}
	if dataCapacities[1] == 0 || dataCapacities[6] == 0 {
		t.Fatalf("variable-width capacities were not recorded: %v", dataCapacities)
	}
}

func TestMySQLValueAppenderMatchesGenericConversion(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })
	now := time.Date(2026, 7, 15, 12, 34, 56, 123456000, time.UTC)

	tests := []struct {
		name     string
		dataType arrow.DataType
		values   []driver.Value
	}{
		{name: "boolean", dataType: arrow.FixedWidthTypes.Boolean, values: []driver.Value{nil, int64(0), int64(1), true}},
		{name: "int16", dataType: arrow.PrimitiveTypes.Int16, values: []driver.Value{nil, int64(-32768), int64(32767), int64(32768)}},
		{name: "int32", dataType: arrow.PrimitiveTypes.Int32, values: []driver.Value{nil, int64(-42), int64(42)}},
		{name: "int64", dataType: arrow.PrimitiveTypes.Int64, values: []driver.Value{nil, int64(-42), int64(42)}},
		{name: "float64", dataType: arrow.PrimitiveTypes.Float64, values: []driver.Value{nil, float64(-1.25), int64(2)}},
		{name: "string", dataType: arrow.BinaryTypes.String, values: []driver.Value{nil, []byte("bytes"), "string"}},
		{name: "binary", dataType: arrow.BinaryTypes.Binary, values: []driver.Value{nil, []byte{0, 1, 2}, "string"}},
		{name: "date", dataType: arrow.FixedWidthTypes.Date32, values: []driver.Value{nil, now, "2026-07-15"}},
		{name: "timestamp", dataType: &arrow.TimestampType{Unit: arrow.Microsecond}, values: []driver.Value{nil, now, "2026-07-15T12:34:56Z"}},
		{name: "time", dataType: &arrow.Time64Type{Unit: arrow.Microsecond}, values: []driver.Value{nil, []byte("12:34:56.123456")}},
		{name: "decimal", dataType: &arrow.Decimal128Type{Precision: 18, Scale: 4}, values: []driver.Value{nil, []byte("123.4567"), []byte("  -0.0001  "), []byte("1e2"), []byte("invalid")}},
		{name: "json", dataType: schema.JSONArrowType, values: []driver.Value{nil, []byte(`{"key":"value"}`), `{"other":1}`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			generic := array.NewBuilder(mem, test.dataType)
			typed := array.NewBuilder(mem, test.dataType)
			typed.Reserve(len(test.values))
			appendTyped := mysqlValueAppender(typed, true)
			for _, value := range test.values {
				appendMySQLValue(generic, value)
				appendTyped(value)
			}
			genericArray := generic.NewArray()
			typedArray := typed.NewArray()
			generic.Release()
			typed.Release()
			defer genericArray.Release()
			defer typedArray.Release()
			if !array.Equal(genericArray, typedArray) {
				t.Fatalf("typed appender result differs: generic=%v typed=%v", genericArray, typedArray)
			}
		})
	}
}

func TestAppendMySQLValueCopiesDriverBytesIntoArrow(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	stringBuilder := array.NewStringBuilder(mem)
	input := []byte("mysql text")
	appendMySQLValue(stringBuilder, input)
	input[0] = 'X'
	stringArray := stringBuilder.NewStringArray()
	if got := stringArray.Value(0); got != "mysql text" {
		t.Fatalf("string value = %q, want %q", got, "mysql text")
	}
	stringArray.Release()
	stringBuilder.Release()

	binaryBuilder := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	input = []byte{1, 2, 3}
	appendMySQLValue(binaryBuilder, input)
	input[0] = 9
	binaryArray := binaryBuilder.NewBinaryArray()
	if got := binaryArray.Value(0); got[0] != 1 {
		t.Fatalf("binary value = %v, want [1 2 3]", got)
	}
	binaryArray.Release()
	binaryBuilder.Release()

	jsonBuilder := array.NewExtensionBuilder(mem, schema.JSONArrowType)
	input = []byte(`{"key":"value"}`)
	appendMySQLValue(jsonBuilder, input)
	input[2] = 'X'
	storage := jsonBuilder.StorageBuilder().(*array.StringBuilder)
	if got := storage.Value(0); got != `{"key":"value"}` {
		t.Fatalf("JSON value = %q, want %q", got, `{"key":"value"}`)
	}
	jsonBuilder.Release()
}

func TestMySQLRawBytesScanPreservesNullAndEmpty(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })

	builder := array.NewStringBuilder(mem)
	nullValue := sql.RawBytes(nil)
	appendMySQLScannedValue(builder, &nullValue)
	emptyValue := sql.RawBytes([]byte{})
	appendMySQLScannedValue(builder, &emptyValue)
	values := builder.NewStringArray()
	defer values.Release()
	builder.Release()

	if !values.IsNull(0) {
		t.Fatal("nil RawBytes should append a null")
	}
	if values.IsNull(1) || values.Value(1) != "" {
		t.Fatal("non-nil empty RawBytes should append an empty value")
	}
}

func TestMySQLScanDestinationUsesRawBytesOnlyForByteBackedTypes(t *testing.T) {
	for _, dataType := range []schema.DataType{
		schema.TypeDecimal,
		schema.TypeString,
		schema.TypeBinary,
		schema.TypeTime,
		schema.TypeJSON,
		schema.TypeUUID,
	} {
		if _, ok := mysqlScanDestination(dataType).(*sql.RawBytes); !ok {
			t.Fatalf("%s scan destination is not *sql.RawBytes", dataType)
		}
	}

	for _, dataType := range []schema.DataType{
		schema.TypeBoolean,
		schema.TypeInt64,
		schema.TypeFloat64,
		schema.TypeDate,
		schema.TypeTimestamp,
	} {
		if _, ok := mysqlScanDestination(dataType).(*interface{}); !ok {
			t.Fatalf("%s scan destination is not *interface{}", dataType)
		}
	}
}
