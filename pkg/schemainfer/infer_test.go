package schemainfer

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
)

func TestNewSchemaInferrer(t *testing.T) {
	inferrer := NewSchemaInferrer()
	if inferrer == nil {
		t.Fatal("expected non-nil inferrer")
	}
	if inferrer.seenFields == nil {
		t.Error("seenFields should be initialized")
	}
	if inferrer.fieldOrder == nil {
		t.Error("fieldOrder should be initialized")
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 0 {
		t.Errorf("expected 0 batches, got %d", stats.BatchCount)
	}
	if stats.RowCount != 0 {
		t.Errorf("expected 0 rows, got %d", stats.RowCount)
	}
	if stats.FieldCount != 0 {
		t.Errorf("expected 0 fields, got %d", stats.FieldCount)
	}
}

func TestSchemaInferrer_AddBatch_Nil(t *testing.T) {
	inferrer := NewSchemaInferrer()
	err := inferrer.AddBatch(nil)
	if err != nil {
		t.Errorf("expected no error for nil batch, got %v", err)
	}
}

func TestSchemaInferrer_AddBatch_SingleBatch(t *testing.T) {
	inferrer := NewSchemaInferrer()

	// Create a simple record
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2, 3}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	nameBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer nameBuilder.Release()
	nameBuilder.AppendValues([]string{"a", "b", "c"}, nil)
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr, nameArr}, 3)
	defer record.Release()

	err := inferrer.AddBatch(record)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 1 {
		t.Errorf("expected 1 batch, got %d", stats.BatchCount)
	}
	if stats.RowCount != 3 {
		t.Errorf("expected 3 rows, got %d", stats.RowCount)
	}
	if stats.FieldCount != 2 {
		t.Errorf("expected 2 fields, got %d", stats.FieldCount)
	}
}

func TestSchemaInferrer_AddBatch_MultipleBatches(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	// Add 3 batches
	for i := 0; i < 3; i++ {
		idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
		idBuilder.AppendValues([]int64{int64(i * 10), int64(i*10 + 1)}, nil)
		idArr := idBuilder.NewArray()
		record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr}, 2)

		err := inferrer.AddBatch(record)
		if err != nil {
			t.Errorf("batch %d: unexpected error: %v", i, err)
		}

		record.Release()
		idArr.Release()
		idBuilder.Release()
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 3 {
		t.Errorf("expected 3 batches, got %d", stats.BatchCount)
	}
	if stats.RowCount != 6 {
		t.Errorf("expected 6 rows, got %d", stats.RowCount)
	}
}

func TestSchemaInferrer_AddBatch_WithNulls(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	valBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer valBuilder.Release()
	valBuilder.AppendValues([]int64{1, 0, 3}, []bool{true, false, true}) // Middle value is null
	valArr := valBuilder.NewArray()
	defer valArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{valArr}, 3)
	defer record.Release()

	err := inferrer.AddBatch(record)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check that null was detected
	info := inferrer.seenFields["value"]
	if info == nil {
		t.Fatal("field 'value' not found")
	}
	if !info.SeenNull {
		t.Error("expected SeenNull to be true")
	}
	if !info.Nullable {
		t.Error("expected Nullable to be true")
	}
}

func TestSchemaInferrer_AddBatch_TypePromotion(t *testing.T) {
	inferrer := NewSchemaInferrer()

	// First batch with int32
	schema1 := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
	}, nil)

	valBuilder1 := array.NewInt32Builder(memory.DefaultAllocator)
	defer valBuilder1.Release()
	valBuilder1.AppendValues([]int32{1, 2, 3}, nil)
	valArr1 := valBuilder1.NewArray()
	defer valArr1.Release()

	record1 := array.NewRecordBatch(schema1, []arrow.Array{valArr1}, 3)
	defer record1.Release()

	err := inferrer.AddBatch(record1)
	if err != nil {
		t.Errorf("batch 1: unexpected error: %v", err)
	}

	// Check type after first batch
	info := inferrer.seenFields["value"]
	if info.Type.ID() != arrow.INT32 {
		t.Errorf("expected INT32 after first batch, got %v", info.Type)
	}

	// Second batch with int64 - should promote
	schema2 := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	valBuilder2 := array.NewInt64Builder(memory.DefaultAllocator)
	defer valBuilder2.Release()
	valBuilder2.AppendValues([]int64{4, 5, 6}, nil)
	valArr2 := valBuilder2.NewArray()
	defer valArr2.Release()

	record2 := array.NewRecordBatch(schema2, []arrow.Array{valArr2}, 3)
	defer record2.Release()

	err = inferrer.AddBatch(record2)
	if err != nil {
		t.Errorf("batch 2: unexpected error: %v", err)
	}

	// Check that type was promoted
	info = inferrer.seenFields["value"]
	if info.Type.ID() != arrow.INT64 {
		t.Errorf("expected INT64 after promotion, got %v", info.Type)
	}
	if len(info.SeenTypes) != 2 {
		t.Errorf("expected 2 seen types, got %d", len(info.SeenTypes))
	}
}

func TestSchemaInferrer_AddBatch_NewFieldInLaterBatch(t *testing.T) {
	inferrer := NewSchemaInferrer()

	// First batch with just 'id'
	schema1 := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	idBuilder1 := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder1.Release()
	idBuilder1.AppendValues([]int64{1, 2}, nil)
	idArr1 := idBuilder1.NewArray()
	defer idArr1.Release()

	record1 := array.NewRecordBatch(schema1, []arrow.Array{idArr1}, 2)
	defer record1.Release()

	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	// Second batch with 'id' and new field 'name'
	schema2 := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder2 := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder2.Release()
	idBuilder2.AppendValues([]int64{3, 4}, nil)
	idArr2 := idBuilder2.NewArray()
	defer idArr2.Release()

	nameBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer nameBuilder.Release()
	nameBuilder.AppendValues([]string{"a", "b"}, nil)
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()

	record2 := array.NewRecordBatch(schema2, []arrow.Array{idArr2, nameArr}, 2)
	defer record2.Release()

	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	stats := inferrer.Stats()
	if stats.FieldCount != 2 {
		t.Errorf("expected 2 fields, got %d", stats.FieldCount)
	}

	// Verify field order preserved
	if inferrer.fieldOrder[0] != "id" || inferrer.fieldOrder[1] != "name" {
		t.Errorf("unexpected field order: %v", inferrer.fieldOrder)
	}
}

func TestSchemaInferrer_AddBatch_TypeConflictToString(t *testing.T) {
	inferrer := NewSchemaInferrer()

	// First batch with int64
	schema1 := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	valBuilder1 := array.NewInt64Builder(memory.DefaultAllocator)
	defer valBuilder1.Release()
	valBuilder1.AppendValues([]int64{1, 2}, nil)
	valArr1 := valBuilder1.NewArray()
	defer valArr1.Release()

	record1 := array.NewRecordBatch(schema1, []arrow.Array{valArr1}, 2)
	defer record1.Release()

	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	// Second batch with date32 - incompatible types
	schema2 := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.FixedWidthTypes.Date32, Nullable: false},
	}, nil)

	dateBuilder := array.NewDate32Builder(memory.DefaultAllocator)
	defer dateBuilder.Release()
	dateBuilder.AppendValues([]arrow.Date32{19000, 19001}, nil)
	dateArr := dateBuilder.NewArray()
	defer dateArr.Release()

	record2 := array.NewRecordBatch(schema2, []arrow.Array{dateArr}, 2)
	defer record2.Release()

	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	// Should be promoted to string
	info := inferrer.seenFields["value"]
	if info.Type.ID() != arrow.STRING {
		t.Errorf("expected STRING after conflict, got %v", info.Type)
	}
}

func TestSchemaInferrer_InferSchema(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	nameBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer nameBuilder.Release()
	nameBuilder.AppendValues([]string{"a", "b"}, nil)
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()

	scoreBuilder := array.NewFloat64Builder(memory.DefaultAllocator)
	defer scoreBuilder.Release()
	scoreBuilder.AppendValues([]float64{1.5, 2.5}, nil)
	scoreArr := scoreBuilder.NewArray()
	defer scoreArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr, nameArr, scoreArr}, 2)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Infer schema
	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if inferred.NumFields() != 3 {
		t.Errorf("expected 3 fields, got %d", inferred.NumFields())
	}

	// Check field properties
	idField := inferred.Field(0)
	if idField.Name != "id" || idField.Type.ID() != arrow.INT64 {
		t.Errorf("unexpected id field: %v", idField)
	}

	nameField := inferred.Field(1)
	if nameField.Name != "name" || nameField.Type.ID() != arrow.STRING {
		t.Errorf("unexpected name field: %v", nameField)
	}

	scoreField := inferred.Field(2)
	if scoreField.Name != "score" || scoreField.Type.ID() != arrow.FLOAT64 {
		t.Errorf("unexpected score field: %v", scoreField)
	}
}

func TestSchemaInferrer_InferSchema_Empty(t *testing.T) {
	inferrer := NewSchemaInferrer()

	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("expected no error for empty inferrer, got: %v", err)
	}
	if inferred != nil {
		t.Errorf("expected nil schema for empty inferrer, got %v", inferred)
	}
}

func TestSchemaInferrer_ToTableSchema(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	nameBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer nameBuilder.Release()
	nameBuilder.AppendValues([]string{"a", "b"}, nil)
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr, nameArr}, 2)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Convert to TableSchema
	tableSchema, err := inferrer.ToTableSchema("my_schema.my_table")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if tableSchema.Name != "my_table" {
		t.Errorf("expected table name 'my_table', got %s", tableSchema.Name)
	}
	if tableSchema.Schema != "my_schema" {
		t.Errorf("expected schema 'my_schema', got %s", tableSchema.Schema)
	}
	if len(tableSchema.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(tableSchema.Columns))
	}

	// Check column types
	idCol := tableSchema.Columns[0]
	if idCol.Name != "id" || idCol.DataType != schema.TypeInt64 {
		t.Errorf("unexpected id column: %+v", idCol)
	}

	nameCol := tableSchema.Columns[1]
	if nameCol.Name != "name" || nameCol.DataType != schema.TypeString {
		t.Errorf("unexpected name column: %+v", nameCol)
	}
}

func TestSchemaInferrer_ToTableSchema_SimpleTableName(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr}, 1)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simple table name without schema
	tableSchema, err := inferrer.ToTableSchema("my_table")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if tableSchema.Name != "my_table" {
		t.Errorf("expected table name 'my_table', got %s", tableSchema.Name)
	}
	if tableSchema.Schema != "" {
		t.Errorf("expected empty schema, got %s", tableSchema.Schema)
	}
}

func TestSchemaInferrer_ToTableSchema_Empty(t *testing.T) {
	inferrer := NewSchemaInferrer()

	tableSchema, err := inferrer.ToTableSchema("my_schema.test")
	if err != nil {
		t.Fatalf("expected no error for empty inferrer, got: %v", err)
	}
	if tableSchema != nil {
		t.Errorf("expected nil table schema for empty inferrer, got %+v", tableSchema)
	}
}

func TestSchemaInferrer_FieldOrder(t *testing.T) {
	inferrer := NewSchemaInferrer()

	// Add batches with different field orders but same fields
	// Should preserve the order from first appearance

	schema1 := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "b", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	aBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer aBuilder.Release()
	aBuilder.AppendValues([]int64{1}, nil)
	aArr := aBuilder.NewArray()
	defer aArr.Release()

	bBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer bBuilder.Release()
	bBuilder.AppendValues([]int64{2}, nil)
	bArr := bBuilder.NewArray()
	defer bArr.Release()

	record1 := array.NewRecordBatch(schema1, []arrow.Array{aArr, bArr}, 1)
	defer record1.Release()

	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	// Add a new field 'c'
	schema2 := arrow.NewSchema([]arrow.Field{
		{Name: "c", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	cBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer cBuilder.Release()
	cBuilder.AppendValues([]int64{3}, nil)
	cArr := cBuilder.NewArray()
	defer cArr.Release()

	aBuilder2 := array.NewInt64Builder(memory.DefaultAllocator)
	defer aBuilder2.Release()
	aBuilder2.AppendValues([]int64{1}, nil)
	aArr2 := aBuilder2.NewArray()
	defer aArr2.Release()

	record2 := array.NewRecordBatch(schema2, []arrow.Array{cArr, aArr2}, 1)
	defer record2.Release()

	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	// Field order should be: a, b, c (order of first appearance)
	expected := []string{"a", "b", "c"}
	if len(inferrer.fieldOrder) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(inferrer.fieldOrder))
	}
	for i, name := range expected {
		if inferrer.fieldOrder[i] != name {
			t.Errorf("field %d: expected %s, got %s", i, name, inferrer.fieldOrder[i])
		}
	}
}

func TestSchemaInferrer_Stats(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	for i := 0; i < 5; i++ {
		aBuilder := array.NewInt64Builder(memory.DefaultAllocator)
		aBuilder.AppendValues([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, nil) // 10 rows each
		aArr := aBuilder.NewArray()

		bBuilder := array.NewStringBuilder(memory.DefaultAllocator)
		bBuilder.AppendValues([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, nil)
		bArr := bBuilder.NewArray()

		record := array.NewRecordBatch(arrowSchema, []arrow.Array{aArr, bArr}, 10)
		if err := inferrer.AddBatch(record); err != nil {
			t.Fatalf("batch %d: unexpected error: %v", i+1, err)
		}

		record.Release()
		aArr.Release()
		bArr.Release()
		aBuilder.Release()
		bBuilder.Release()
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 5 {
		t.Errorf("expected 5 batches, got %d", stats.BatchCount)
	}
	if stats.RowCount != 50 {
		t.Errorf("expected 50 rows, got %d", stats.RowCount)
	}
	if stats.FieldCount != 2 {
		t.Errorf("expected 2 fields, got %d", stats.FieldCount)
	}
}

func TestInferValueType_JSONNumber(t *testing.T) {
	if !arrow.TypeEqual(inferValueType(json.Number("42")), arrow.PrimitiveTypes.Int64) {
		t.Error("expected json.Number(42) to infer as int64")
	}
	if !arrow.TypeEqual(inferValueType(json.Number("42.5")), arrow.PrimitiveTypes.Float64) {
		t.Error("expected json.Number(42.5) to infer as float64")
	}
	if !arrow.TypeEqual(inferValueType(json.Number("1e6")), arrow.PrimitiveTypes.Float64) {
		t.Error("expected json.Number(1e6) to infer as float64")
	}
}

func TestInferValueType_UnsignedOverflow(t *testing.T) {
	if !arrow.TypeEqual(inferValueType(uint64(math.MaxInt64)), arrow.PrimitiveTypes.Int64) {
		t.Error("expected uint64(maxInt64) to infer as int64")
	}
	if !arrow.TypeEqual(inferValueType(uint64(math.MaxInt64)+1), arrow.PrimitiveTypes.Float64) {
		t.Error("expected uint64(maxInt64+1) to infer as float64")
	}
}

func TestInferValueType_FloatIntegralAndRange(t *testing.T) {
	if !arrow.TypeEqual(inferValueType(float32(42)), arrow.PrimitiveTypes.Int64) {
		t.Error("expected float32(42) to infer as int64")
	}
	if !arrow.TypeEqual(inferValueType(float32(42.5)), arrow.PrimitiveTypes.Float64) {
		t.Error("expected float32(42.5) to infer as float64")
	}
	if !arrow.TypeEqual(inferValueType(float64(math.MaxFloat64)), arrow.PrimitiveTypes.Float64) {
		t.Error("expected out-of-range float64 to infer as float64")
	}
	if !arrow.TypeEqual(inferValueType(float64(math.Inf(1))), arrow.PrimitiveTypes.Float64) {
		t.Error("expected +Inf to infer as float64")
	}
}

func TestInferValueType_TemporalStrings(t *testing.T) {
	if !arrow.TypeEqual(inferValueType("2026-01-15"), arrow.FixedWidthTypes.Date32) {
		t.Error("expected YYYY-MM-DD to infer as date32")
	}
	if !arrow.TypeEqual(inferValueType("2026-01-15T14:30:10Z"), arrow.FixedWidthTypes.Timestamp_us) {
		t.Error("expected datetime string to infer as timestamp")
	}
	if !arrow.TypeEqual(inferValueType("plain-text"), arrow.BinaryTypes.String) {
		t.Error("expected plain text to infer as string")
	}
}

func TestLooksLikeTemporal(t *testing.T) {
	if !looksLikeTemporal("2026-01-15T14:30:10Z") {
		t.Error("expected RFC3339 string to look temporal")
	}
	if !looksLikeTemporal("2026/01/15 14:30:10") {
		t.Error("expected slash date-time string to look temporal")
	}
	if !looksLikeTemporal("14:30:10") {
		t.Error("expected time string to look temporal")
	}

	if looksLikeTemporal("a-b") {
		t.Error("did not expect a-b to look temporal")
	}
	if looksLikeTemporal("x/y") {
		t.Error("did not expect x/y to look temporal")
	}
	if looksLikeTemporal("t") {
		t.Error("did not expect t to look temporal")
	}
	if looksLikeTemporal("Z") {
		t.Error("did not expect Z to look temporal")
	}
}

func TestInferUnknownColumnType_UsesInferValueType(t *testing.T) {
	builder := array.NewBuilder(memory.DefaultAllocator, schema.UnknownArrowType).(*array.ExtensionBuilder)
	defer builder.Release()

	storage := builder.StorageBuilder().(*array.StringBuilder)
	storage.Append("\"2026-01-15T14:30:10Z\"")
	storage.Append("123")

	arr := builder.NewArray()
	defer arr.Release()

	inferred, ok := inferUnknownColumnType(arr)
	if !ok {
		t.Fatal("expected unknown column inference to return a type")
	}

	// timestamp + int should promote to string according to merge rules.
	if !arrow.TypeEqual(inferred, arrow.BinaryTypes.String) {
		t.Errorf("expected merged type string, got %s", inferred)
	}
}

func TestSchemaInferrer_Integration_UnknownSourceLikeBatches(t *testing.T) {
	inferrer := NewSchemaInferrer()

	schemaBatch1 := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: schema.UnknownArrowType, Nullable: false},
		{Name: "created_at", Type: schema.UnknownArrowType, Nullable: false},
		{Name: "metadata", Type: schema.UnknownArrowType, Nullable: false},
	}, nil)

	idBatch1, idBatch1Builder := buildUnknownArray([]string{"1", "2", "3"}, []bool{true, true, true})
	defer idBatch1.Release()
	defer idBatch1Builder.Release()

	createdAtBatch1, createdAtBatch1Builder := buildUnknownArray(
		[]string{`"2026-01-15"`, `"2026-01-16"`, `"2026-01-17"`},
		[]bool{true, true, true},
	)
	defer createdAtBatch1.Release()
	defer createdAtBatch1Builder.Release()

	metadataBatch1, metadataBatch1Builder := buildUnknownArray(
		[]string{`{"plan":"free"}`, `{"plan":"pro"}`, `{"plan":"team"}`},
		[]bool{true, true, true},
	)
	defer metadataBatch1.Release()
	defer metadataBatch1Builder.Release()

	recordBatch1 := array.NewRecordBatch(schemaBatch1, []arrow.Array{idBatch1, createdAtBatch1, metadataBatch1}, 3)
	defer recordBatch1.Release()
	if err := inferrer.AddBatch(recordBatch1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	schemaBatch2 := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: schema.UnknownArrowType, Nullable: false},
		{Name: "created_at", Type: schema.UnknownArrowType, Nullable: false},
		{Name: "metadata", Type: schema.UnknownArrowType, Nullable: false},
		{Name: "score", Type: schema.UnknownArrowType, Nullable: false},
	}, nil)

	idBatch2, idBatch2Builder := buildUnknownArray([]string{"4", "5.5", "6"}, []bool{true, true, true})
	defer idBatch2.Release()
	defer idBatch2Builder.Release()

	createdAtBatch2, createdAtBatch2Builder := buildUnknownArray(
		[]string{`"2026-01-18T01:02:03Z"`, `"2026-01-19T01:02:03Z"`, ""},
		[]bool{true, true, false},
	)
	defer createdAtBatch2.Release()
	defer createdAtBatch2Builder.Release()

	metadataBatch2, metadataBatch2Builder := buildUnknownArray(
		[]string{`{"plan":"enterprise","flags":["a"]}`, `{"plan":"pro","active":true}`, `{"plan":"free"}`},
		[]bool{true, true, true},
	)
	defer metadataBatch2.Release()
	defer metadataBatch2Builder.Release()

	scoreBatch2, scoreBatch2Builder := buildUnknownArray([]string{"10", "15.25", "20"}, []bool{true, true, true})
	defer scoreBatch2.Release()
	defer scoreBatch2Builder.Release()

	recordBatch2 := array.NewRecordBatch(schemaBatch2, []arrow.Array{idBatch2, createdAtBatch2, metadataBatch2, scoreBatch2}, 3)
	defer recordBatch2.Release()
	if err := inferrer.AddBatch(recordBatch2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("analytics.events")
	if err != nil {
		t.Fatalf("to table schema: unexpected error: %v", err)
	}
	if len(tableSchema.Columns) != 4 {
		t.Fatalf("expected 4 table columns, got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[0].DataType != schema.TypeFloat64 {
		t.Errorf("expected id column type float64, got %v", tableSchema.Columns[0].DataType)
	}
	if tableSchema.Columns[1].DataType != schema.TypeTimestamp && tableSchema.Columns[1].DataType != schema.TypeTimestampTZ {
		t.Errorf("expected created_at column type timestamp/timestamptz, got %v", tableSchema.Columns[1].DataType)
	}
	if tableSchema.Columns[2].DataType != schema.TypeJSON {
		t.Errorf("expected metadata column type json, got %v", tableSchema.Columns[2].DataType)
	}
	if tableSchema.Columns[3].DataType != schema.TypeFloat64 {
		t.Errorf("expected score column type float64, got %v", tableSchema.Columns[3].DataType)
	}
	if !tableSchema.Columns[1].Nullable {
		t.Error("expected created_at column to be nullable after observing nulls")
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 2 {
		t.Errorf("expected 2 batches, got %d", stats.BatchCount)
	}
	if stats.RowCount != 6 {
		t.Errorf("expected 6 rows, got %d", stats.RowCount)
	}
	if stats.FieldCount != 4 {
		t.Errorf("expected 4 fields, got %d", stats.FieldCount)
	}
}

func buildUnknownArray(values []string, valid []bool) (arrow.Array, *array.ExtensionBuilder) {
	builder := array.NewBuilder(memory.DefaultAllocator, schema.UnknownArrowType).(*array.ExtensionBuilder)
	storage := builder.StorageBuilder().(*array.StringBuilder)
	storage.AppendValues(values, valid)
	return builder.NewArray(), builder
}

func TestSchemaInferrer_Integration_EndToEndConvertWithSchemaToInference(t *testing.T) {
	inferrer := NewSchemaInferrer()

	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
	}

	itemsBatch1 := []map[string]interface{}{
		{"id": int64(1), "name": "a", "dynamic": 10},
		{"id": int64(2), "name": "b", "dynamic": 20},
	}
	recordBatch1, err := arrowconv.ItemsToArrowRecordWithSchema(itemsBatch1, cols, nil)
	if err != nil {
		t.Fatalf("build batch 1: unexpected error: %v", err)
	}
	defer recordBatch1.Release()

	dynamicIdx := -1
	for i := 0; i < int(recordBatch1.NumCols()); i++ {
		if recordBatch1.ColumnName(i) == "dynamic" {
			dynamicIdx = i
			break
		}
	}
	if dynamicIdx == -1 {
		t.Fatal("expected dynamic field to exist")
	}
	if !arrow.TypeEqual(recordBatch1.Schema().Field(dynamicIdx).Type, schema.UnknownArrowType) {
		t.Fatalf("expected dynamic field to start as unknown type, got %s", recordBatch1.Schema().Field(dynamicIdx).Type)
	}

	if err := inferrer.AddBatch(recordBatch1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	itemsBatch2 := []map[string]interface{}{
		{"id": int64(3), "name": "c", "dynamic": 30.5},
		{"id": int64(4), "name": "d", "dynamic": nil},
	}
	recordBatch2, err := arrowconv.ItemsToArrowRecordWithSchema(itemsBatch2, cols, nil)
	if err != nil {
		t.Fatalf("build batch 2: unexpected error: %v", err)
	}
	defer recordBatch2.Release()

	if err := inferrer.AddBatch(recordBatch2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("analytics.events")
	if err != nil {
		t.Fatalf("to table schema: unexpected error: %v", err)
	}
	if len(tableSchema.Columns) != 3 {
		t.Fatalf("expected 3 table columns, got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[0].Name != "id" || tableSchema.Columns[0].DataType != schema.TypeInt64 {
		t.Errorf("unexpected id column: %+v", tableSchema.Columns[0])
	}
	if tableSchema.Columns[1].Name != "name" || tableSchema.Columns[1].DataType != schema.TypeString {
		t.Errorf("unexpected name column: %+v", tableSchema.Columns[1])
	}
	if tableSchema.Columns[2].Name != "dynamic" {
		t.Errorf("expected third column to be dynamic, got %s", tableSchema.Columns[2].Name)
	}
	if tableSchema.Columns[2].DataType != schema.TypeFloat64 {
		t.Errorf("expected dynamic column type float64, got %v", tableSchema.Columns[2].DataType)
	}
	if !tableSchema.Columns[2].Nullable {
		t.Error("expected dynamic column to be nullable")
	}

	stats := inferrer.Stats()
	if stats.BatchCount != 2 {
		t.Errorf("expected 2 batches, got %d", stats.BatchCount)
	}
	if stats.RowCount != 4 {
		t.Errorf("expected 4 rows, got %d", stats.RowCount)
	}
	if stats.FieldCount != 3 {
		t.Errorf("expected 3 fields, got %d", stats.FieldCount)
	}
}

func TestSchemaInferrer_Integration_EndToEndConvertWithSchema_AllNullColumnDropped(t *testing.T) {
	inferrer := NewSchemaInferrer()

	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
	}

	items := []map[string]interface{}{
		{"id": int64(1), "dynamic": nil},
		{"id": int64(2), "dynamic": nil},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, nil)
	if err != nil {
		t.Fatalf("build batch: unexpected error: %v", err)
	}
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("add batch: unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("analytics.events")
	if err != nil {
		t.Fatalf("to table schema: unexpected error: %v", err)
	}

	if len(tableSchema.Columns) != 1 {
		t.Fatalf("expected 1 column (all-null column should be dropped), got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[0].Name != "id" {
		t.Fatalf("expected first column to be id, got %s", tableSchema.Columns[0].Name)
	}
}

func TestSchemaInferrer_AllNullKnownType_KeptInInferSchema(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "empty", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	emptyBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer emptyBuilder.Release()
	emptyBuilder.AppendValues([]string{"", ""}, []bool{false, false}) // all null
	emptyArr := emptyBuilder.NewArray()
	defer emptyArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr, emptyArr}, 2)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred.NumFields() != 2 {
		t.Fatalf("expected 2 fields (all-null with known type should be kept), got %d", inferred.NumFields())
	}
	if inferred.Field(0).Name != "id" {
		t.Errorf("expected field 'id', got %s", inferred.Field(0).Name)
	}
	if inferred.Field(1).Name != "empty" {
		t.Errorf("expected field 'empty', got %s", inferred.Field(1).Name)
	}

	tableSchema, err := inferrer.ToTableSchema("test_table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tableSchema == nil {
		t.Fatal("expected non-nil table schema when known-type all-null column is present")
	}
	if len(tableSchema.Columns) != 2 {
		t.Fatalf("expected 2 columns (all-null with known type should be kept), got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[1].Name != "empty" {
		t.Errorf("expected second column 'empty', got %s", tableSchema.Columns[1].Name)
	}
}

func TestSchemaInferrer_AllNullUnknownType_DroppedFromInferSchema(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "empty", Type: schema.UnknownArrowType, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	emptyBuilder := array.NewExtensionBuilder(memory.DefaultAllocator, schema.UnknownArrowType)
	defer emptyBuilder.Release()
	sb := emptyBuilder.StorageBuilder().(*array.StringBuilder)
	sb.AppendValues([]string{"", ""}, []bool{false, false}) // all null
	emptyArr := emptyBuilder.NewArray()
	defer emptyArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr, emptyArr}, 2)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred.NumFields() != 1 {
		t.Fatalf("expected 1 field (all-null unknown type dropped), got %d", inferred.NumFields())
	}
	if inferred.Field(0).Name != "id" {
		t.Errorf("expected field 'id', got %s", inferred.Field(0).Name)
	}
}

func TestSchemaInferrer_AllNullColumn_DataInLaterBatch(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	// Batch 1: value is all null
	idBuilder1 := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder1.Release()
	idBuilder1.AppendValues([]int64{1, 2}, nil)
	idArr1 := idBuilder1.NewArray()
	defer idArr1.Release()

	valBuilder1 := array.NewStringBuilder(memory.DefaultAllocator)
	defer valBuilder1.Release()
	valBuilder1.AppendValues([]string{"", ""}, []bool{false, false})
	valArr1 := valBuilder1.NewArray()
	defer valArr1.Release()

	record1 := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr1, valArr1}, 2)
	defer record1.Release()

	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	// Batch 2: value has data
	idBuilder2 := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder2.Release()
	idBuilder2.AppendValues([]int64{3, 4}, nil)
	idArr2 := idBuilder2.NewArray()
	defer idArr2.Release()

	valBuilder2 := array.NewStringBuilder(memory.DefaultAllocator)
	defer valBuilder2.Release()
	valBuilder2.AppendValues([]string{"hello", "world"}, nil)
	valArr2 := valBuilder2.NewArray()
	defer valArr2.Release()

	record2 := array.NewRecordBatch(arrowSchema, []arrow.Array{idArr2, valArr2}, 2)
	defer record2.Release()

	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	// Both columns should be present since value got data in batch 2
	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred.NumFields() != 2 {
		t.Fatalf("expected 2 fields, got %d", inferred.NumFields())
	}

	tableSchema, err := inferrer.ToTableSchema("test_table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tableSchema.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tableSchema.Columns))
	}
}

func TestSchemaInferrer_AllColumnsNull_KnownTypes_Kept(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "b", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	aBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer aBuilder.Release()
	aBuilder.AppendValues([]string{"", ""}, []bool{false, false})
	aArr := aBuilder.NewArray()
	defer aArr.Release()

	bBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer bBuilder.Release()
	bBuilder.AppendValues([]int64{0, 0}, []bool{false, false})
	bArr := bBuilder.NewArray()
	defer bArr.Release()

	record := array.NewRecordBatch(arrowSchema, []arrow.Array{aArr, bArr}, 2)
	defer record.Release()

	if err := inferrer.AddBatch(record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inferred == nil {
		t.Fatal("expected non-nil schema when all columns have known types")
	}
	if inferred.NumFields() != 2 {
		t.Fatalf("expected 2 fields, got %d", inferred.NumFields())
	}

	tableSchema, err := inferrer.ToTableSchema("test_table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tableSchema == nil {
		t.Fatal("expected non-nil table schema when all columns have known types")
	}
	if len(tableSchema.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tableSchema.Columns))
	}
}

func TestSchemaInferrer_Integration_UnknownScalarThenJSONPromotesToJSON(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "payload", Type: schema.UnknownArrowType, Nullable: false},
	}, nil)

	payload1, payload1Builder := buildUnknownArray([]string{"1", "2"}, []bool{true, true})
	defer payload1.Release()
	defer payload1Builder.Release()
	record1 := array.NewRecordBatch(arrowSchema, []arrow.Array{payload1}, 2)
	defer record1.Release()
	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	payload2, payload2Builder := buildUnknownArray([]string{`{"a":1}`, `{"b":"x"}`}, []bool{true, true})
	defer payload2.Release()
	defer payload2Builder.Release()
	record2 := array.NewRecordBatch(arrowSchema, []arrow.Array{payload2}, 2)
	defer record2.Release()
	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("analytics.events")
	if err != nil {
		t.Fatalf("to table schema: unexpected error: %v", err)
	}
	if len(tableSchema.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[0].DataType != schema.TypeJSON {
		t.Errorf("expected payload type json, got %v", tableSchema.Columns[0].DataType)
	}
}

func TestSchemaInferrer_Integration_UnknownInvalidJSONPromotesToString(t *testing.T) {
	inferrer := NewSchemaInferrer()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: schema.UnknownArrowType, Nullable: false},
	}, nil)

	value1, value1Builder := buildUnknownArray([]string{"plain-text"}, []bool{true})
	defer value1.Release()
	defer value1Builder.Release()
	record1 := array.NewRecordBatch(arrowSchema, []arrow.Array{value1}, 1)
	defer record1.Release()
	if err := inferrer.AddBatch(record1); err != nil {
		t.Fatalf("batch 1: unexpected error: %v", err)
	}

	value2, value2Builder := buildUnknownArray([]string{"123"}, []bool{true})
	defer value2.Release()
	defer value2Builder.Release()
	record2 := array.NewRecordBatch(arrowSchema, []arrow.Array{value2}, 1)
	defer record2.Release()
	if err := inferrer.AddBatch(record2); err != nil {
		t.Fatalf("batch 2: unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("analytics.events")
	if err != nil {
		t.Fatalf("to table schema: unexpected error: %v", err)
	}
	if len(tableSchema.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(tableSchema.Columns))
	}
	if tableSchema.Columns[0].DataType != schema.TypeString {
		t.Errorf("expected value type string, got %v", tableSchema.Columns[0].DataType)
	}
}

func TestSchemaInferrer_AllNullUnknownTypeColumnDropped(t *testing.T) {
	inferrer := NewSchemaInferrer()

	s := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "adsquad_id", Type: schema.UnknownArrowType, Nullable: true},
	}, nil)

	idBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.Append("abc")

	adsquadArr, adsquadBuilder := buildUnknownArray([]string{""}, []bool{false})
	defer adsquadArr.Release()
	defer adsquadBuilder.Release()

	batch := array.NewRecordBatch(s, []arrow.Array{idBuilder.NewArray(), adsquadArr}, 1)
	defer batch.Release()

	if err := inferrer.AddBatch(batch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableSchema, err := inferrer.ToTableSchema("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tableSchema.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d: %v", len(tableSchema.Columns), tableSchema.ColumnNames())
	}
	if tableSchema.Columns[0].Name != "id" {
		t.Errorf("expected column 'id', got %q", tableSchema.Columns[0].Name)
	}

	dropped := inferrer.DroppedColumns()
	if !dropped["adsquad_id"] {
		t.Errorf("expected adsquad_id in dropped columns, got %v", dropped)
	}
}

func TestSchemaInferrer_ProtectedColumns_NotDropped(t *testing.T) {
	inferrer := NewSchemaInferrer()

	s := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "plan_id", Type: schema.UnknownArrowType, Nullable: true},
	}, nil)

	idBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.Append("abc")

	planArr, planBuilder := buildUnknownArray([]string{""}, []bool{false})
	defer planArr.Release()
	defer planBuilder.Release()

	batch := array.NewRecordBatch(s, []arrow.Array{idBuilder.NewArray(), planArr}, 1)
	defer batch.Release()

	if err := inferrer.AddBatch(batch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inferrer.ProtectColumns([]string{"plan_id"})

	tableSchema, err := inferrer.ToTableSchema("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tableSchema.Columns) != 2 {
		t.Fatalf("expected 2 columns (protected column should be kept), got %d: %v", len(tableSchema.Columns), tableSchema.ColumnNames())
	}
	if tableSchema.Columns[1].Name != "plan_id" {
		t.Errorf("expected second column 'plan_id', got %q", tableSchema.Columns[1].Name)
	}

	dropped := inferrer.DroppedColumns()
	if dropped["plan_id"] {
		t.Errorf("protected column 'plan_id' should not be in dropped columns")
	}
}

func TestSchemaInferrer_ProtectedColumns_InferSchema(t *testing.T) {
	inferrer := NewSchemaInferrer()

	s := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "plan_id", Type: schema.UnknownArrowType, Nullable: true},
		{Name: "other", Type: schema.UnknownArrowType, Nullable: true},
	}, nil)

	idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
	defer idBuilder.Release()
	idBuilder.Append(1)

	planArr, planBuilder := buildUnknownArray([]string{""}, []bool{false})
	defer planArr.Release()
	defer planBuilder.Release()

	otherArr, otherBuilder := buildUnknownArray([]string{""}, []bool{false})
	defer otherArr.Release()
	defer otherBuilder.Release()

	batch := array.NewRecordBatch(s, []arrow.Array{idBuilder.NewArray(), planArr, otherArr}, 1)
	defer batch.Release()

	if err := inferrer.AddBatch(batch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inferrer.ProtectColumns([]string{"plan_id"})

	inferred, err := inferrer.InferSchema()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inferred.NumFields() != 2 {
		t.Fatalf("expected 2 fields (id + protected plan_id, other dropped), got %d", inferred.NumFields())
	}
	if inferred.Field(0).Name != "id" {
		t.Errorf("expected field 'id', got %s", inferred.Field(0).Name)
	}
	if inferred.Field(1).Name != "plan_id" {
		t.Errorf("expected field 'plan_id', got %s", inferred.Field(1).Name)
	}

	dropped := inferrer.DroppedColumns()
	if dropped["plan_id"] {
		t.Errorf("protected column 'plan_id' should not be dropped")
	}
	if !dropped["other"] {
		t.Errorf("unprotected column 'other' should be dropped")
	}
}
