package ops_test

import (
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend/ops"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestTransposeIterator(t *testing.T) {
	operand := shapes.Make(dtypes.Int32, 2, 3, 4)
	permutations := []int{2, 0, 1}
	it := ops.NewTransposeIterator(operand, permutations)
	transposedFlatIndices := make([]int, 0, operand.Size())
	for range operand.Size() {
		transposedFlatIndices = append(transposedFlatIndices, it.Next())
	}
	// fmt.Printf("\ttransposedFlatIndices=%#v\n", transposedFlatIndices)
	want := []int{
		// Operand axis 2 (the first being iterated) becomes output axis 0, in row-major order,
		// this is the largest one, with strides of 6:
		0, 6, 12, 18,
		1, 7, 13, 19,
		2, 8, 14, 20,

		3, 9, 15, 21,
		4, 10, 16, 22,
		5, 11, 17, 23}
	if ok, diff := testutil.IsEqual(want, transposedFlatIndices); !ok {
		t.Fatalf("transposeIterator mismatch:\n%s", diff)
	}
}
