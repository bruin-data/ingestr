package starrocks

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

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
		[]arrow.Array{arr}, 2)
	defer rec.Release()

	body, n, err := recordBatchToJSON(rec)
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
