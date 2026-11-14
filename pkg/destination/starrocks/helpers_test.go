package starrocks

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/float16"
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

func TestStreamLoadBatchEncoderExoticColumnsAndArrowTypes(t *testing.T) {
	pool := memory.NewGoAllocator()

	u64b := array.NewUint64Builder(pool)
	u64b.Append(18446744073709551615)
	u64Arr := u64b.NewUint64Array()
	defer u64Arr.Release()

	fsbb := array.NewFixedSizeBinaryBuilder(pool, &arrow.FixedSizeBinaryType{ByteWidth: 2})
	fsbb.Append([]byte{0xde, 0xad})
	fsbArr := fsbb.NewFixedSizeBinaryArray()
	defer fsbArr.Release()

	f16b := array.NewFloat16Builder(pool)
	f16b.Append(float16.New(1.5))
	f16Arr := f16b.NewFloat16Array()
	defer f16Arr.Release()

	t32b := array.NewTime32Builder(pool, &arrow.Time32Type{Unit: arrow.Second})
	t32b.Append(arrow.Time32(3723))
	t32Arr := t32b.NewTime32Array()
	defer t32Arr.Release()

	fslb := array.NewFixedSizeListBuilder(pool, 2, arrow.PrimitiveTypes.Uint16)
	fslvb := fslb.ValueBuilder().(*array.Uint16Builder)
	fslb.Append(true)
	fslvb.AppendValues([]uint16{7, 8}, nil)
	fslArr := fslb.NewListArray()
	defer fslArr.Release()

	sc := arrow.NewSchema([]arrow.Field{
		{Name: "$price", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "_c0", Type: &arrow.FixedSizeBinaryType{ByteWidth: 2}},
		{Name: "a.b[0]", Type: arrow.FixedWidthTypes.Float16},
		{Name: "t_sec", Type: &arrow.Time32Type{Unit: arrow.Second}},
		{Name: "pair", Type: arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Uint16)},
	}, nil)
	rec := array.NewRecordBatch(sc, []arrow.Array{u64Arr, fsbArr, f16Arr, t32Arr, fslArr}, 1)
	defer rec.Release()

	enc, opts := newStreamLoadBatchEncoder(rec, 4096)
	body, rows, err := enc.next(t.Context())
	if err != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
	if want := `[{"_c_0":18446744073709551615,"_c_1":"dead","_c_2":1.5,"_c_3":[7,8],"_c_4":"01:02:03.000000"}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", body, want)
	}
	if want := `["$._c_0","$._c_1","$._c_2","$._c_3","$._c_4"]`; opts.jsonpaths != want {
		t.Fatalf("jsonpaths: got %s want %s", opts.jsonpaths, want)
	}
	wantCols := "_c_0, _c_1, _c_2, _c_3, _c_4, `$price`=_c_0, `_c0`=to_binary(_c_1, 'hex'), `a.b[0]`=_c_2, `pair`=_c_3, `t_sec`=_c_4"
	if opts.columns != wantCols {
		t.Fatalf("columns: got %q want %q", opts.columns, wantCols)
	}
}
