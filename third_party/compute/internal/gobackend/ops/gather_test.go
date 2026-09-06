package ops

import (
	"slices"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapeinference"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestExecSpecialOps_gatherIterator(t *testing.T) {
	operandShape := shapes.Make(dtypes.F32, 4, 3, 2, 2)
	startIndicesShape := shapes.Make(dtypes.Int8, 3, 3, 2)
	startVectorAxis := 1
	offsetOutputAxes := []int{1, 3}
	collapsedSliceAxes := []int{0, 2}
	startIndexMap := []int{0, 2, 3}
	sliceSizes := []int{1, 3, 1, 1}
	outputShape, err := shapeinference.Gather(operandShape, startIndicesShape, startVectorAxis,
		offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes, false)
	if err != nil {
		t.Fatalf("shapeinference.Gather failed: %+v", err)
	}
	// fmt.Printf("\toutputShape=%s\n", outputShape)
	if err := outputShape.Check(dtypes.F32, 3, 3, 2, 1); err != nil {
		t.Fatalf("outputShape check failed: %+v", err)
	}
	it := newGatherIterator(startIndicesShape, startVectorAxis, outputShape, offsetOutputAxes)
	var gotStartIndices [][]int
	var gotOutputIndices []int
	indices := make([]int, 3)
	var outputBytesIdx int
	for it.Next(indices, &outputBytesIdx) {
		gotStartIndices = append(gotStartIndices, slices.Clone(indices))
		gotOutputIndices = append(gotOutputIndices, outputBytesIdx)
	}
	// fmt.Printf("\tgatherStartIndicesIterator got startIndices=%#v\n", gotStartIndices)
	// fmt.Printf("\tgatherStartIndicesIterator got outputBytesIndices=%#v\n", gotOutputIndices)
	wantStartIndirectIndices := [][]int{{0, 2, 4}, {1, 3, 5}, {6, 8, 10}, {7, 9, 11}, {12, 14, 16}, {13, 15, 17}}
	if ok, diff := testutil.IsEqual(wantStartIndirectIndices, gotStartIndices); !ok {
		t.Errorf("gotStartIndices mismatch:\n%s", diff)
	}
	dataSize := operandShape.DType.Size() // == 4 for Float32
	wantOutputFlatIndices := []int{0, 1, 6, 7, 12, 13}
	for ii := range wantOutputFlatIndices {
		wantOutputFlatIndices[ii] *= dataSize
	}
	if ok, diff := testutil.IsEqual(wantOutputFlatIndices, gotOutputIndices); !ok {
		t.Errorf("gotOutputIndices mismatch:\n%s", diff)
	}
}
