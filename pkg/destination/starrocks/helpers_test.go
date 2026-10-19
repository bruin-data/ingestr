package starrocks

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// Binary columns are hex-encoded in the body (so json.Marshal can't mangle
// non-UTF-8 bytes) and reported so the loader can add a to_binary transform.
func TestRecordBatchToJSONBinary(t *testing.T) {
	pool := memory.NewGoAllocator()
	bb := array.NewBinaryBuilder(pool, arrow.BinaryTypes.Binary)
	bb.Append([]byte{0xFF, 0xFE, 0x00})
	arr := bb.NewBinaryArray()
	defer arr.Release()

	sc := arrow.NewSchema([]arrow.Field{{Name: "data", Type: arrow.BinaryTypes.Binary}}, nil)
	rec := array.NewRecordBatch(sc, []arrow.Array{arr}, 1)
	defer rec.Release()

	body, n, binaryCols, err := recordBatchToJSON(rec)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if want := `[{"data":"fffe00"}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", string(body), want)
	}
	if len(binaryCols) != 1 || binaryCols[0] != "data" {
		t.Fatalf("binaryCols: got %v", binaryCols)
	}
	if got, want := columnsHeader(sc, binaryCols), "`data`, `data`=to_binary(`data`, 'hex')"; got != want {
		t.Fatalf("columnsHeader: got %q want %q", got, want)
	}
	if columnsHeader(sc, nil) != "" {
		t.Fatalf("columnsHeader should be empty without binary columns")
	}
}

// A list column maps to a StarRocks JSON column, so it must serialize as a JSON
// array (not a quoted string) and null elements must survive.
func TestRecordBatchToJSONList(t *testing.T) {
	pool := memory.NewGoAllocator()
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Int32)
	vb := lb.ValueBuilder().(*array.Int32Builder)
	lb.Append(true)
	vb.AppendValues([]int32{1, 2, 3}, nil)
	lb.Append(true)
	vb.Append(9)
	vb.AppendNull()
	arr := lb.NewListArray()
	defer arr.Release()

	rec := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{{Name: "c", Type: arr.DataType()}}, nil),
		[]arrow.Array{arr}, 2,
	)
	defer rec.Release()

	body, n, _, err := recordBatchToJSON(rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows: got %d want 2", n)
	}
	want := `[{"c":[1,2,3]},{"c":[9,null]}]`
	if string(body) != want {
		t.Fatalf("body: got %s want %s", string(body), want)
	}
}

func TestMapStarRocksArrayType(t *testing.T) {
	tests := []struct {
		columnType       string
		elem             schema.DataType
		precision, scale int
	}{
		{"array<DECIMAL64(5,2)>", schema.TypeDecimal, 5, 2},
		{"array<DECIMAL128(20,12)>", schema.TypeDecimal, 20, 12},
		{"array<int(11)>", schema.TypeInt32, 0, 0},
		{"array<varchar(65533)>", schema.TypeString, 0, 0},
		{"array<array<int(11)>>", schema.TypeArray, 0, 0},
	}
	for _, tt := range tests {
		elem, precision, scale := mapStarRocksArrayType(tt.columnType)
		if elem != tt.elem || precision != tt.precision || scale != tt.scale {
			t.Errorf("%s: got (%s, %d, %d), want (%s, %d, %d)", tt.columnType, elem, precision, scale, tt.elem, tt.precision, tt.scale)
		}
	}
}

func TestRecordBatchToJSONNonFiniteFloats(t *testing.T) {
	pool := memory.NewGoAllocator()
	fb := array.NewFloat64Builder(pool)
	fb.AppendValues([]float64{math.NaN(), math.Inf(1), 1.5}, nil)
	arr := fb.NewFloat64Array()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "f", Type: arrow.PrimitiveTypes.Float64}}, nil), []arrow.Array{arr}, 3)
	defer rec.Release()
	body, _, _, err := recordBatchToJSON(rec)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"f":null},{"f":null},{"f":1.5}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", body, want)
	}
}

func TestRecordBatchToJSONFloat32KeepsShortestForm(t *testing.T) {
	fb := array.NewFloat32Builder(memory.NewGoAllocator())
	fb.AppendValues([]float32{0.1, float32(math.NaN())}, nil)
	arr := fb.NewFloat32Array()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "f", Type: arrow.PrimitiveTypes.Float32}}, nil), []arrow.Array{arr}, 2)
	defer rec.Release()
	body, _, _, err := recordBatchToJSON(rec)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"f":0.1},{"f":null}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", body, want)
	}
}

func TestRecordBatchToJSONBinaryListUsesBase64(t *testing.T) {
	pool := memory.NewGoAllocator()
	lb := array.NewListBuilder(pool, arrow.BinaryTypes.Binary)
	vb := lb.ValueBuilder().(*array.BinaryBuilder)
	lb.Append(true)
	vb.Append([]byte{0x00, 0xff})
	vb.Append([]byte{})
	vb.AppendNull()
	arr := lb.NewListArray()
	defer arr.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "c", Type: arr.DataType()}}, nil), []arrow.Array{arr}, 1)
	defer rec.Release()
	body, _, _, err := recordBatchToJSON(rec)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"c":["AP8=","",null]}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", body, want)
	}
}
