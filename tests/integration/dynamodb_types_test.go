package integration

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bruin-data/gong/pkg/destination"
	dynDest "github.com/bruin-data/gong/pkg/destination/dynamodb"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sendRecord(rec arrow.RecordBatch) <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult, 1)
	rec.Retain()
	ch <- source.RecordBatchResult{Batch: rec}
	close(ch)
	return ch
}

func connectDynamoDBDest(t *testing.T, ctx context.Context, table string, pks []string) *dynDest.DynamoDBDestination {
	t.Helper()
	dest := dynDest.NewDynamoDBDestination()
	require.NoError(t, dest.Connect(ctx, dynamoDBDest.uri))

	tableSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: pks[0], DataType: schema.TypeInt64}},
		PrimaryKeys: pks,
	}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       table,
		Schema:      tableSchema,
		PrimaryKeys: pks,
		DropFirst:   true,
	}))
	return dest
}

// TestDynamoDB_AllTypes verifies that every Arrow type handled by arrowToDynamoDB
// is correctly written to and readable from DynamoDB.
func TestDynamoDB_AllTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("alltypes")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "col_bool", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
		{Name: "col_int8", Type: arrow.PrimitiveTypes.Int8, Nullable: true},
		{Name: "col_int16", Type: arrow.PrimitiveTypes.Int16, Nullable: true},
		{Name: "col_int32", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "col_int64", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "col_uint8", Type: arrow.PrimitiveTypes.Uint8, Nullable: true},
		{Name: "col_uint16", Type: arrow.PrimitiveTypes.Uint16, Nullable: true},
		{Name: "col_uint32", Type: arrow.PrimitiveTypes.Uint32, Nullable: true},
		{Name: "col_uint64", Type: arrow.PrimitiveTypes.Uint64, Nullable: true},
		{Name: "col_float32", Type: arrow.PrimitiveTypes.Float32, Nullable: true},
		{Name: "col_float64", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "col_string", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "col_large_string", Type: arrow.BinaryTypes.LargeString, Nullable: true},
		{Name: "col_binary", Type: arrow.BinaryTypes.Binary, Nullable: true},
		{Name: "col_date32", Type: arrow.FixedWidthTypes.Date32, Nullable: true},
		{Name: "col_date64", Type: arrow.FixedWidthTypes.Date64, Nullable: true},
		{Name: "col_time64_us", Type: arrow.FixedWidthTypes.Time64us, Nullable: true},
		{Name: "col_time64_ns", Type: arrow.FixedWidthTypes.Time64ns, Nullable: true},
		{Name: "col_timestamp", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
		{Name: "col_decimal128", Type: &arrow.Decimal128Type{Precision: 18, Scale: 4}, Nullable: true},
	}, nil)

	alloc := memory.NewGoAllocator()
	bldr := array.NewRecordBuilder(alloc, arrowSchema)
	defer bldr.Release()

	// Row 1: all types populated with representative values
	bldr.Field(0).(*array.Int64Builder).Append(1)
	bldr.Field(1).(*array.BooleanBuilder).Append(true)
	bldr.Field(2).(*array.Int8Builder).Append(-128)
	bldr.Field(3).(*array.Int16Builder).Append(-32768)
	bldr.Field(4).(*array.Int32Builder).Append(-2147483648)
	bldr.Field(5).(*array.Int64Builder).Append(-9223372036854775808)
	bldr.Field(6).(*array.Uint8Builder).Append(255)
	bldr.Field(7).(*array.Uint16Builder).Append(65535)
	bldr.Field(8).(*array.Uint32Builder).Append(4294967295)
	bldr.Field(9).(*array.Uint64Builder).Append(18446744073709551615)
	bldr.Field(10).(*array.Float32Builder).Append(3.14)
	bldr.Field(11).(*array.Float64Builder).Append(2.718281828459045)
	bldr.Field(12).(*array.StringBuilder).Append("hello world")
	bldr.Field(13).(*array.LargeStringBuilder).Append("large string value")
	bldr.Field(14).(*array.BinaryBuilder).Append([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	bldr.Field(15).(*array.Date32Builder).Append(arrow.Date32FromTime(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)))
	bldr.Field(16).(*array.Date64Builder).Append(arrow.Date64FromTime(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)))
	bldr.Field(17).(*array.Time64Builder).Append(arrow.Time64(13*3600000000 + 45*60000000 + 30*1000000 + 123456))        // microseconds
	bldr.Field(18).(*array.Time64Builder).Append(arrow.Time64((13*3600000000+45*60000000+30*1000000+123456)*1000 + 789)) // nanoseconds: same time + 789ns
	ts := time.Date(2024, 6, 15, 13, 45, 30, 0, time.UTC)
	bldr.Field(19).(*array.TimestampBuilder).Append(arrow.Timestamp(ts.UnixMicro()))
	bldr.Field(20).(*array.Decimal128Builder).Append(decimal128.FromI64(123456789)) // 12345.6789 with scale=4

	// Row 2: all nulls except id
	bldr.Field(0).(*array.Int64Builder).Append(2)
	for i := 1; i < arrowSchema.NumFields(); i++ {
		bldr.Field(i).AppendNull()
	}

	// Row 3: edge cases - zero values, empty strings, NaN, Inf
	bldr.Field(0).(*array.Int64Builder).Append(3)
	bldr.Field(1).(*array.BooleanBuilder).Append(false)
	bldr.Field(2).(*array.Int8Builder).Append(0)
	bldr.Field(3).(*array.Int16Builder).Append(0)
	bldr.Field(4).(*array.Int32Builder).Append(0)
	bldr.Field(5).(*array.Int64Builder).Append(0)
	bldr.Field(6).(*array.Uint8Builder).Append(0)
	bldr.Field(7).(*array.Uint16Builder).Append(0)
	bldr.Field(8).(*array.Uint32Builder).Append(0)
	bldr.Field(9).(*array.Uint64Builder).Append(0)
	bldr.Field(10).(*array.Float32Builder).Append(float32(math.NaN()))
	bldr.Field(11).(*array.Float64Builder).Append(math.Inf(1))
	bldr.Field(12).(*array.StringBuilder).Append("")
	bldr.Field(13).(*array.LargeStringBuilder).Append("")
	bldr.Field(14).(*array.BinaryBuilder).Append([]byte{})
	bldr.Field(15).(*array.Date32Builder).Append(arrow.Date32FromTime(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)))
	bldr.Field(16).(*array.Date64Builder).Append(arrow.Date64FromTime(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)))
	bldr.Field(17).(*array.Time64Builder).Append(0)
	bldr.Field(18).(*array.Time64Builder).Append(0)
	bldr.Field(19).(*array.TimestampBuilder).Append(0)
	bldr.Field(20).(*array.Decimal128Builder).Append(decimal128.FromI64(0))

	record := bldr.NewRecordBatch()
	defer record.Release()

	dest := connectDynamoDBDest(t, ctx, table, []string{"id"})
	require.NoError(t, dest.Write(ctx, sendRecord(record), destination.WriteOptions{Table: table}))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, 3, count, "should have 3 rows")

	// === Row 1: verify all populated values ===
	item1 := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item1, "row 1 should exist")

	assertBool(t, item1, "col_bool", true)
	assertNumber(t, item1, "col_int8", "-128")
	assertNumber(t, item1, "col_int16", "-32768")
	assertNumber(t, item1, "col_int32", "-2147483648")
	assertNumber(t, item1, "col_int64", "-9223372036854775808")
	assertNumber(t, item1, "col_uint8", "255")
	assertNumber(t, item1, "col_uint16", "65535")
	assertNumber(t, item1, "col_uint32", "4294967295")
	assertNumber(t, item1, "col_uint64", "18446744073709551615")
	assertNumberPrefix(t, item1, "col_float32", "3.14")
	assertNumberPrefix(t, item1, "col_float64", "2.718281828459045")
	assertString(t, item1, "col_string", "hello world")
	assertString(t, item1, "col_large_string", "large string value")
	assertBinary(t, item1, "col_binary", []byte{0xDE, 0xAD, 0xBE, 0xEF})
	assertString(t, item1, "col_date32", "2024-06-15")
	assertString(t, item1, "col_date64", "2024-06-15")
	assertString(t, item1, "col_time64_us", "13:45:30.123456")
	assertString(t, item1, "col_time64_ns", "13:45:30.123456") // nanoseconds converted to microseconds
	assertString(t, item1, "col_timestamp", "2024-06-15T13:45:30Z")
	assertNumber(t, item1, "col_decimal128", "12345.6789")

	// === Row 2: verify all nulls ===
	item2 := getItemByID(t, ctx, client, table, 2)
	require.NotNil(t, item2, "row 2 should exist")

	nullCols := []string{
		"col_bool", "col_int8", "col_int16", "col_int32", "col_int64",
		"col_uint8", "col_uint16", "col_uint32", "col_uint64",
		"col_float32", "col_float64", "col_string", "col_large_string",
		"col_binary", "col_date32", "col_date64",
		"col_time64_us", "col_time64_ns", "col_timestamp", "col_decimal128",
	}
	for _, col := range nullCols {
		assertNull(t, item2, col)
	}

	// === Row 3: edge cases ===
	item3 := getItemByID(t, ctx, client, table, 3)
	require.NotNil(t, item3, "row 3 should exist")

	assertBool(t, item3, "col_bool", false)
	assertNumber(t, item3, "col_int8", "0")
	assertNumber(t, item3, "col_int64", "0")
	assertIsString(t, item3, "col_float32") // NaN → string
	assertIsString(t, item3, "col_float64") // +Inf → string
	assertString(t, item3, "col_string", "")
	assertString(t, item3, "col_large_string", "")
	assertNull(t, item3, "col_binary") // empty binary → NULL (DynamoDB rejects empty B values)
	assertString(t, item3, "col_date32", "1970-01-01")
	assertString(t, item3, "col_time64_us", "00:00:00.000000")
	assertString(t, item3, "col_time64_ns", "00:00:00.000000")
}

// TestDynamoDB_StructAndListTypes tests nested struct and list Arrow types.
func TestDynamoDB_StructAndListTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("nested")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	structType := arrow.StructOf(
		arrow.Field{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		arrow.Field{Name: "age", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
	)
	listType := arrow.ListOf(arrow.PrimitiveTypes.Int32)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "col_struct", Type: structType, Nullable: true},
		{Name: "col_list", Type: listType, Nullable: true},
	}, nil)

	alloc := memory.NewGoAllocator()
	bldr := array.NewRecordBuilder(alloc, arrowSchema)
	defer bldr.Release()

	// Row 1: populated struct and list
	bldr.Field(0).(*array.Int64Builder).Append(1)
	structBldr := bldr.Field(1).(*array.StructBuilder)
	structBldr.Append(true)
	structBldr.FieldBuilder(0).(*array.StringBuilder).Append("Alice")
	structBldr.FieldBuilder(1).(*array.Int32Builder).Append(30)
	listBldr := bldr.Field(2).(*array.ListBuilder)
	listBldr.Append(true)
	listBldr.ValueBuilder().(*array.Int32Builder).Append(10)
	listBldr.ValueBuilder().(*array.Int32Builder).Append(20)
	listBldr.ValueBuilder().(*array.Int32Builder).Append(30)

	// Row 2: null struct and empty list
	bldr.Field(0).(*array.Int64Builder).Append(2)
	structBldr.AppendNull()
	listBldr.Append(true) // empty list

	// Row 3: struct with null fields, list with single element
	bldr.Field(0).(*array.Int64Builder).Append(3)
	structBldr.Append(true)
	structBldr.FieldBuilder(0).(*array.StringBuilder).AppendNull()
	structBldr.FieldBuilder(1).(*array.Int32Builder).AppendNull()
	listBldr.Append(true)
	listBldr.ValueBuilder().(*array.Int32Builder).Append(99)

	record := bldr.NewRecordBatch()
	defer record.Release()

	dest := connectDynamoDBDest(t, ctx, table, []string{"id"})
	require.NoError(t, dest.Write(ctx, sendRecord(record), destination.WriteOptions{Table: table}))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, 3, count)

	// Row 1: struct → Map (M), list → List (L)
	item1 := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item1)

	structVal, ok := item1["col_struct"].(*types.AttributeValueMemberM)
	require.True(t, ok, "col_struct should be Map type, got %T", item1["col_struct"])
	nameVal, ok := structVal.Value["name"].(*types.AttributeValueMemberS)
	require.True(t, ok)
	assert.Equal(t, "Alice", nameVal.Value)
	ageVal, ok := structVal.Value["age"].(*types.AttributeValueMemberN)
	require.True(t, ok)
	assert.Equal(t, "30", ageVal.Value)

	listVal, ok := item1["col_list"].(*types.AttributeValueMemberL)
	require.True(t, ok, "col_list should be List type, got %T", item1["col_list"])
	require.Len(t, listVal.Value, 3)
	for i, expected := range []string{"10", "20", "30"} {
		nVal, ok := listVal.Value[i].(*types.AttributeValueMemberN)
		require.True(t, ok, "list[%d] should be N type", i)
		assert.Equal(t, expected, nVal.Value)
	}

	// Row 2: null struct → NULL, empty list → L with 0 elements
	item2 := getItemByID(t, ctx, client, table, 2)
	require.NotNil(t, item2)
	assertNull(t, item2, "col_struct")
	listVal2, ok := item2["col_list"].(*types.AttributeValueMemberL)
	require.True(t, ok, "empty col_list should still be L type, got %T", item2["col_list"])
	assert.Empty(t, listVal2.Value)

	// Row 3: struct with null fields → Map with NULL members
	item3 := getItemByID(t, ctx, client, table, 3)
	require.NotNil(t, item3)
	structVal3, ok := item3["col_struct"].(*types.AttributeValueMemberM)
	require.True(t, ok)
	_, isNull := structVal3.Value["name"].(*types.AttributeValueMemberNULL)
	assert.True(t, isNull, "struct.name should be NULL")
	_, isNull = structVal3.Value["age"].(*types.AttributeValueMemberNULL)
	assert.True(t, isNull, "struct.age should be NULL")

	listVal3, ok := item3["col_list"].(*types.AttributeValueMemberL)
	require.True(t, ok)
	require.Len(t, listVal3.Value, 1)
	nVal, ok := listVal3.Value[0].(*types.AttributeValueMemberN)
	require.True(t, ok)
	assert.Equal(t, "99", nVal.Value)
}

// TestDynamoDB_WriteParallelTypes verifies WriteParallel handles various types correctly.
func TestDynamoDB_WriteParallelTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("parallel_types")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "col_string", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "col_int64", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "col_float64", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "col_bool", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
		{Name: "col_timestamp", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
	}, nil)

	alloc := memory.NewGoAllocator()

	const batchCount = 4
	const rowsPerBatch = 10
	ch := make(chan source.RecordBatchResult, batchCount)

	for b := range batchCount {
		bldr := array.NewRecordBuilder(alloc, arrowSchema)
		for r := range rowsPerBatch {
			rowID := int64(b*rowsPerBatch + r + 1)
			bldr.Field(0).(*array.Int64Builder).Append(rowID)
			bldr.Field(1).(*array.StringBuilder).Append(fmt.Sprintf("row_%d", rowID))
			bldr.Field(2).(*array.Int64Builder).Append(rowID * 100)
			bldr.Field(3).(*array.Float64Builder).Append(float64(rowID) * 1.5)
			bldr.Field(4).(*array.BooleanBuilder).Append(rowID%2 == 0)
			ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rowID) * time.Hour)
			bldr.Field(5).(*array.TimestampBuilder).Append(arrow.Timestamp(ts.UnixMicro()))
		}
		rec := bldr.NewRecordBatch()
		ch <- source.RecordBatchResult{Batch: rec}
		bldr.Release()
	}
	close(ch)

	dest := connectDynamoDBDest(t, ctx, table, []string{"id"})
	require.NoError(t, dest.WriteParallel(ctx, ch, destination.WriteOptions{
		Table:       table,
		Parallelism: 4,
	}))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, batchCount*rowsPerBatch, count, "parallel write should insert all rows")

	item1 := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item1)
	assertString(t, item1, "col_string", "row_1")
	assertNumber(t, item1, "col_int64", "100")

	item40 := getItemByID(t, ctx, client, table, 40)
	require.NotNil(t, item40)
	assertString(t, item40, "col_string", "row_40")
	assertBool(t, item40, "col_bool", true)
}

// TestDynamoDB_LargeBatch verifies batches exceeding the 25-item DynamoDB limit are chunked correctly.
func TestDynamoDB_LargeBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("largebatch")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	alloc := memory.NewGoAllocator()
	bldr := array.NewRecordBuilder(alloc, arrowSchema)
	defer bldr.Release()

	const totalRows = 75 // 3 full chunks of 25
	for i := 1; i <= totalRows; i++ {
		bldr.Field(0).(*array.Int64Builder).Append(int64(i))
		bldr.Field(1).(*array.StringBuilder).Append(fmt.Sprintf("item_%d", i))
	}

	record := bldr.NewRecordBatch()
	defer record.Release()

	dest := connectDynamoDBDest(t, ctx, table, []string{"id"})
	require.NoError(t, dest.Write(ctx, sendRecord(record), destination.WriteOptions{Table: table}))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, totalRows, count, "all 75 rows should be written across multiple chunks")

	item1 := getItemByID(t, ctx, client, table, 1)
	require.NotNil(t, item1)
	assertString(t, item1, "value", "item_1")

	item75 := getItemByID(t, ctx, client, table, 75)
	require.NotNil(t, item75)
	assertString(t, item75, "value", "item_75")
}

// TestDynamoDB_CompositeKey tests tables with hash + range key (2 primary keys).
func TestDynamoDB_CompositeKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	client := dynamoDBClient(t, ctx)

	table := dynamoDBTableName("compositekey")
	defer cleanupDynamoDBTable(t, ctx, client, table)

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "pk", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "sk", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "data", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	alloc := memory.NewGoAllocator()
	bldr := array.NewRecordBuilder(alloc, arrowSchema)
	defer bldr.Release()

	bldr.Field(0).(*array.StringBuilder).Append("user_1")
	bldr.Field(1).(*array.Int64Builder).Append(1)
	bldr.Field(2).(*array.StringBuilder).Append("first")

	bldr.Field(0).(*array.StringBuilder).Append("user_1")
	bldr.Field(1).(*array.Int64Builder).Append(2)
	bldr.Field(2).(*array.StringBuilder).Append("second")

	bldr.Field(0).(*array.StringBuilder).Append("user_2")
	bldr.Field(1).(*array.Int64Builder).Append(1)
	bldr.Field(2).(*array.StringBuilder).Append("other")

	record := bldr.NewRecordBatch()
	defer record.Release()

	dest := dynDest.NewDynamoDBDestination()
	require.NoError(t, dest.Connect(ctx, dynamoDBDest.uri))

	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "pk", DataType: schema.TypeString},
			{Name: "sk", DataType: schema.TypeInt64},
		},
		PrimaryKeys: []string{"pk", "sk"},
	}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       table,
		Schema:      tableSchema,
		PrimaryKeys: []string{"pk", "sk"},
		DropFirst:   true,
	}))

	require.NoError(t, dest.Write(ctx, sendRecord(record), destination.WriteOptions{Table: table}))

	count := scanCount(t, ctx, client, table)
	assert.Equal(t, 3, count, "should have 3 rows with composite key")

	out, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "user_1"},
			"sk": &types.AttributeValueMemberN{Value: "2"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, out.Item)
	sv, ok := out.Item["data"].(*types.AttributeValueMemberS)
	require.True(t, ok)
	assert.Equal(t, "second", sv.Value)
}

// === Assertion helpers ===

func assertBool(t *testing.T, item map[string]types.AttributeValue, key string, expected bool) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	bv, ok := av.(*types.AttributeValueMemberBOOL)
	require.True(t, ok, "key %q should be BOOL type, got %T", key, av)
	assert.Equal(t, expected, bv.Value, "key %q", key)
}

func assertNumber(t *testing.T, item map[string]types.AttributeValue, key string, expected string) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	nv, ok := av.(*types.AttributeValueMemberN)
	require.True(t, ok, "key %q should be N type, got %T", key, av)
	assert.Equal(t, expected, nv.Value, "key %q", key)
}

func assertNumberPrefix(t *testing.T, item map[string]types.AttributeValue, key string, prefix string) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	nv, ok := av.(*types.AttributeValueMemberN)
	require.True(t, ok, "key %q should be N type, got %T", key, av)
	assert.Contains(t, nv.Value, prefix[:4], "key %q value %q should contain %q", key, nv.Value, prefix[:4])
}

func assertString(t *testing.T, item map[string]types.AttributeValue, key string, expected string) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	sv, ok := av.(*types.AttributeValueMemberS)
	require.True(t, ok, "key %q should be S type, got %T", key, av)
	assert.Equal(t, expected, sv.Value, "key %q", key)
}

func assertIsString(t *testing.T, item map[string]types.AttributeValue, key string) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	_, ok = av.(*types.AttributeValueMemberS)
	assert.True(t, ok, "key %q should be S type (NaN/Inf stored as string), got %T", key, av)
}

func assertBinary(t *testing.T, item map[string]types.AttributeValue, key string, expected []byte) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist", key)
	bv, ok := av.(*types.AttributeValueMemberB)
	require.True(t, ok, "key %q should be B type, got %T", key, av)
	assert.Equal(t, expected, bv.Value, "key %q", key)
}

func assertNull(t *testing.T, item map[string]types.AttributeValue, key string) {
	t.Helper()
	av, ok := item[key]
	require.True(t, ok, "key %q should exist in item", key)
	_, ok = av.(*types.AttributeValueMemberNULL)
	assert.True(t, ok, "key %q should be NULL type, got %T", key, av)
}
