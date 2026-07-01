package starrocks

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Binary columns are base64-encoded in the body (so json.Marshal can't mangle
// non-UTF-8 bytes) and reported so the loader can add a from_base64 transform.
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
	if want := `[{"data":"//4A"}]`; string(body) != want {
		t.Fatalf("body: got %s want %s", string(body), want)
	}
	if len(binaryCols) != 1 || binaryCols[0] != "data" {
		t.Fatalf("binaryCols: got %v", binaryCols)
	}
	if got, want := columnsHeader(sc, binaryCols), "data, data=from_base64(data)"; got != want {
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
