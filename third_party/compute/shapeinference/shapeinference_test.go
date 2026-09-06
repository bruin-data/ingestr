// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapeinference

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/sets"
	"github.com/gomlx/compute/support/testutil"
)

// Aliases
var (
	Bool = dtypes.Bool
	I8   = dtypes.Int8
	I32  = dtypes.Int32
	F32  = dtypes.Float32
	U64  = dtypes.Uint64

	S  = shapes.Make
	SD = shapes.MakeDynamic
)

func must1[T any](value T, err error) T {
	return testutil.Must1(value, err)
}

func TestBinaryOp(t *testing.T) {
	// Invalid data types check.
	var err error
	_, err = BinaryOp(OpTypeLogicalAnd, S(I8), S(I8))
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	_, err = BinaryOp(OpTypeMul, S(Bool, 1), S(Bool, 1))
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	_, err = BinaryOp(OpTypeMul, S(Bool, 1), S(Bool, 1))
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	_, err = BinaryOp(OpTypeBitwiseXor, S(F32, 1), S(F32, 1))
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Invalid operation type (not binary op).
	_, err = BinaryOp(OpTypeExp, S(F32), S(F32))
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// The same shape should be ok.
	var output shapes.Shape
	intMatrixShape := S(I8, 3, 3)
	output, err = BinaryOp(OpTypeBitwiseOr, intMatrixShape, intMatrixShape)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(intMatrixShape.Equal(output)) {
		t.Fatalf("Condition expected to be true")
	}

	// Scalar with matrix.
	scalarShape := S(F32)
	matrixShape := S(F32, 2, 3)
	expectedShape := S(F32, 2, 3)
	output, err = BinaryOp(OpTypeAdd, scalarShape, scalarShape)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(scalarShape.Equal(output)) {
		t.Fatalf("Condition expected to be true")
	}
	output, err = BinaryOp(OpTypeAdd, scalarShape, matrixShape)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expectedShape.Equal(output)) {
		t.Fatalf("Condition expected to be true")
	}

	// Broadcasting on both sides.
	shape1 := S(F32, 2, 1, 3)
	shape2 := S(F32, 1, 4, 3)
	expectedBroadcastShape := S(F32, 2, 4, 3)
	if !(expectedBroadcastShape.Equal(must1(BinaryOp(OpTypeMul, shape1, shape2)))) {
		t.Fatalf("Condition expected to be true")
	}

	// Matrix with scalar.
	if !(expectedShape.Equal(must1(BinaryOp(OpTypeAdd, matrixShape, scalarShape)))) {
		t.Fatalf("Condition expected to be true")
	}

	// Invalid broadcasting shapes.
	invalidShape1 := S(F32, 2, 3)
	invalidShape2 := S(F32, 3, 2)
	_, err = BinaryOp(OpTypeAdd, invalidShape1, invalidShape2)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
}

func TestUnaryOp(t *testing.T) {
	// Invalid data types check.
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeLogicalNot, S(F32))) }); !panicked {
		t.Fatalf("Expected panic")
	}
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeLogicalNot, S(I8))) }); !panicked {
		t.Fatalf("Expected panic")
	}
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeBitwiseNot, S(F32))) }); !panicked {
		t.Fatalf("Expected panic")
	}
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeNeg, S(Bool))) }); !panicked {
		t.Fatalf("Expected panic")
	}

	// Invalid operation type (not unary op).
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeAdd, S(F32))) }); !panicked {
		t.Fatalf("Expected panic")
	}
	if panicked, _ := testutil.Try(func() { must1(UnaryOp(OpTypeNeg, S(U64))) }); !panicked {
		t.Fatalf("Expected panic")
	}

	// Valid operations
	boolShape := S(Bool, 2, 3)
	if !(boolShape.Equal(must1(UnaryOp(OpTypeLogicalNot, boolShape)))) {
		t.Fatalf("Condition expected to be true")
	}

	intShape := S(I8, 3, 3)
	if !(intShape.Equal(must1(UnaryOp(OpTypeBitwiseNot, intShape)))) {
		t.Fatalf("Condition expected to be true")
	}

	floatShape := S(F32, 2, 3)
	if !(floatShape.Equal(must1(UnaryOp(OpTypeExp, floatShape)))) {
		t.Fatalf("Condition expected to be true")
	}
	if !(floatShape.Equal(must1(UnaryOp(OpTypeNeg, floatShape)))) {
		t.Fatalf("Condition expected to be true")
	}
}

func TestGatherOp(t *testing.T) {
	// Test 1:
	operand := S(F32, 4, 3, 2, 2)
	startIndices := S(I8, 3, 3, 2)
	indexVectorAxis := 1
	offsetOutputAxes := []int{0, 3}
	collapsedSliceAxes := []int{0, 2}
	startIndexMap := []int{0, 2, 3}
	sliceSizes := []int{1, 3, 1, 1}
	output, err := Gather(operand, startIndices, indexVectorAxis,
		offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	fmt.Printf("\tTest 1: outputShape=%s\n", output)
	if output.Check(F32, 3, 3, 2, 1) != nil {
		t.Fatalf("Expected no error, got %v", output.Check(F32, 3, 3, 2, 1))
	}

	// Test 2:
	operand = S(F32, 3, 4, 5, 6)
	startIndices = S(U64, 7, 3, 8)
	indexVectorAxis = 1
	offsetOutputAxes = []int{1, 2}
	collapsedSliceAxes = []int{1, 2}
	startIndexMap = []int{1, 2, 3}
	sliceSizes = []int{3, 1, 1, 1}
	output, err = Gather(operand, startIndices, indexVectorAxis, offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	fmt.Printf("\tTest 2: outputShape=%s\n", output)
	if output.Check(F32, 7, 3, 1, 8) != nil {
		t.Fatalf("Expected no error, got %v", output.Check(F32, 7, 3, 1, 8))
	}

	// Test 3:
	operand = S(F32, 8, 16)
	startIndices = S(U64, 8, 1)
	indexVectorAxis = 1
	offsetOutputAxes = []int{1}
	collapsedSliceAxes = []int{0}
	startIndexMap = []int{0}
	sliceSizes = []int{1, 16}
	output, err = Gather(operand, startIndices, indexVectorAxis, offsetOutputAxes, collapsedSliceAxes, startIndexMap, sliceSizes, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	fmt.Printf("\tTest 3: outputShape=%s\n", output)
	if output.Check(F32, 8, 16) != nil {
		t.Fatalf("Expected no error, got %v", output.Check(F32, 8, 16))
	}
}

func TestScatterOp(t *testing.T) {
	// --- Valid Cases ---

	// Case 1: Typical scatter (like TF ScatterNd)
	// Scatter 2 updates of shape [5] into operand [4, 5]
	// Indices shape [2, 1] indicates 2 indices, each pointing to 1 dimension (axis 0) of operand.
	operand1 := S(F32, 4, 5)
	indices1 := S(I8, 2, 1)  // Batch shape [2]
	updates1 := S(F32, 2, 5) // Batch shape [2]
	indexVectorAxis1 := 1
	updateWindowAxes1 := []int{1}
	insertedWindowAxes1 := []int{0}
	scatterAxesToOperandAxes1 := []int{0} // Index coordinate vector element 0 maps to operand axis 0
	expected1 := operand1
	output1, err := Scatter(operand1, indices1, updates1, indexVectorAxis1, updateWindowAxes1, insertedWindowAxes1, scatterAxesToOperandAxes1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected1.Equal(output1)) {
		t.Fatalf("Valid Case 1 Failed: Expected %s, got %s", expected1, output1)
	}

	// Case 2: Scattering into a higher-rank tensor
	// Scatter updates of shape [4] into operand[i, j, :], where [i, j] comes from indices.
	// Operand: [10, 9, 8] (Rank 3)
	// Indices: [2, 3, 2] (Rank 3) -> 2x3 batch, each index is a pointer to the first 2 axes of the operand
	// Updates: [2, 3, 8] (Rank 3) -> 2x3 batch, update window shape [8]
	operand2 := S(F32, 10, 9, 8)
	indices2 := S(I32, 2, 3, 2)              // 6 indices, each is a 2D coordinate
	updates2 := S(F32, 2, 3, 8)              // 6 updates, window shape [8] matching operand's last dim
	indexVectorAxis2 := 2                    // Axis 2 of indices holds the coordinate vector [coord0, coord1]
	updateWindowAxes2 := []int{2}            // Axis 2 of updates corresponds to the window shape [8]
	insertedWindowAxes2 := []int{0, 1}       // Axis 0, 1 of operand are the dimensions determined by the indices[i,j,:]
	scatterAxesToOperandAxes2 := []int{0, 1} // index coord 0 -> operand axis 0, index coord 1 -> operand axis 1
	expected2 := operand2
	output2, err := Scatter(operand2, indices2, updates2, indexVectorAxis2, updateWindowAxes2, insertedWindowAxes2, scatterAxesToOperandAxes2)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected2.Equal(output2)) {
		t.Fatalf("Valid Case 2 Failed: Expected %s, got %s", expected2, output2)
	}

	// Case 3: Different indexVectorAxis
	// Same as case 2, but indices are [2, 2, 3] -> indexVectorAxis is 1 and different order of axes in the operand.
	operand3 := S(F32, 10, 9, 8)
	indices3 := S(I32, 2, 2, 3) // 2x3 batch, index vector size 2, indexVecAxis=1
	updates3 := S(F32, 8, 2, 3) // Update axis [8] is "out-of-order", which should be fine.
	indexVectorAxis3 := 1       // Index vector is now axis 1
	updateWindowAxes3 := []int{0}
	insertedWindowAxes3 := []int{1, 2}
	scatterAxesToOperandAxes3 := []int{1, 2} // indices are used for different axes in the operand this time.
	expected3 := operand2                    // Still expect operand shape
	output3, err := Scatter(operand3, indices3, updates3, indexVectorAxis3, updateWindowAxes3, insertedWindowAxes3, scatterAxesToOperandAxes3)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected3.Equal(output3)) {
		t.Fatalf("Valid Case 3 Failed (IndexVecAxis=1): Expected %s, got %s", expected3, output3)
	}

	// Case 4: No insertedWindowAxes (scattering full slices)
	// Scatter updates of shape [9] into operand [10, 9]
	operand4 := S(F32, 10, 9)
	indices4 := S(I32, 6)                 // 6 indices, coord size 1
	updates4 := S(F32, 6, 9)              // 6 updates, window shape [] (scalar)
	indexVectorAxis4 := 1                 // == indices4.Rank() -> trigger extra virtual axes to indices4.
	updateWindowAxes4 := []int{1}         // No window axes in updates (updates are scalars matching batch dims)
	insertedWindowAxes4 := []int{0}       // No window axes in operand (index selects full slice - which is scalar here)
	scatterAxesToOperandAxes4 := []int{0} // Index coord 0 -> operand axis 0
	expected4 := operand4
	output4, err := Scatter(operand4, indices4, updates4, indexVectorAxis4, updateWindowAxes4, insertedWindowAxes4, scatterAxesToOperandAxes4)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected4.Equal(output4)) {
		t.Fatalf("Valid Case 4 Failed (No Window): Expected %s, got %s", expected4, output4)
	}

	// Case 5: rearranging the output axes:
	operand5 := S(F32, 2, 5, 2)
	indices5 := S(I32, 2, 2)
	updates5 := S(F32, 5, 2)
	indexVectorAxis5 := 1
	updateWindowAxes5 := []int{0}
	insertedWindowAxes5 := []int{0, 2}
	scatterAxesToOperandAxes5 := []int{0, 2}
	output5, err := Scatter(operand5, indices5, updates5, indexVectorAxis5, updateWindowAxes5, insertedWindowAxes5, scatterAxesToOperandAxes5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(operand5.Equal(output5)) {
		t.Fatalf("Valid Case 5 Failed (No Window): Expected %s, got %s", operand5, output5)
	}

	// --- Error Cases ---

	// Error Case 1: Mismatched DType (Operand vs Updates) - unchanged
	_, err = Scatter(S(F32, 4, 5), S(I8, 2, 1), S(I8, 2, 5), 1, []int{1}, []int{1}, []int{0})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 2: Invalid DType for indices - unchanged
	_, err = Scatter(S(F32, 4, 5), S(F32, 2, 1), S(F32, 2, 5), 1, []int{1}, []int{1}, []int{0})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 3: indexVectorAxis out of bounds
	_, err = Scatter(operand1, indices1, updates1, 2, updateWindowAxes1, insertedWindowAxes1, scatterAxesToOperandAxes1) // indices1 rank 2, axis 2 invalid
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	_, err = Scatter(operand1, indices1, updates1, -1, updateWindowAxes1, insertedWindowAxes1, scatterAxesToOperandAxes1) // Negative axis
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 4: len(updateWindowAxes) != len(insertedWindowAxes)
	_, err = Scatter(operand1, indices1, updates1, indexVectorAxis1, []int{1}, []int{1, 0}, scatterAxesToOperandAxes1) // inserted has len 2, update has len 1
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 5: len(scatterAxesToOperandAxes) != size of index vector dimension
	_, err = Scatter(operand1, indices1, updates1, indexVectorAxis1, updateWindowAxes1, insertedWindowAxes1, []int{0, 1}) // scatterAxes has len 2, expected 1
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
	_, err = Scatter(operand2, indices2, updates2, indexVectorAxis2, updateWindowAxes2, insertedWindowAxes2, []int{0}) // scatterAxes has len 1, expected 2
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 6: Invalid axis index in updateWindowAxes
	_, err = Scatter(operand1, indices1, updates1, indexVectorAxis1, []int{2}, insertedWindowAxes1, scatterAxesToOperandAxes1) // axis 2 invalid for rank 2 updates
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 7: Invalid axis index in insertedWindowAxes
	_, err = Scatter(operand1, indices1, updates1, indexVectorAxis1, updateWindowAxes1, []int{2}, scatterAxesToOperandAxes1) // axis 2 invalid for rank 2 operand
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 8: Invalid axis index in scatterAxesToOperandAxes
	_, err = Scatter(operand1, indices1, updates1, indexVectorAxis1, updateWindowAxes1, insertedWindowAxes1, []int{2}) // axis 2 invalid for rank 2 operand
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error Case 9: Update dimension is larger than the corresponding dimension in the operand:
	_, err = Scatter(operand5, indices5, updates5, indexVectorAxis5, updateWindowAxes5, []int{0, 1}, scatterAxesToOperandAxes5) // axis 2 invalid for rank 2 operand
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
}

func TestSliceOp(t *testing.T) {
	opName := "SliceOp"

	// --- Valid Cases ---
	// Case 1: Simple 1D slice
	operand1 := S(F32, 10)
	starts1 := []int{2}
	limits1 := []int{8}
	strides1 := []int{1}
	expected1 := S(F32, 6)
	output1, err := Slice(operand1, starts1, limits1, strides1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected1.Equal(output1)) {
		t.Fatalf("%s Valid Case 1 Failed: Expected %s, got %s", opName, expected1, output1)
	}

	// Case 2: 2D slice with stride 1
	operand2 := S(I32, 5, 6)
	starts2 := []int{1, 2}
	limits2 := []int{4, 5}
	strides2 := []int{1, 1}
	expected2 := S(I32, 3, 3)
	output2, err := Slice(operand2, starts2, limits2, strides2)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected2.Equal(output2)) {
		t.Fatalf("%s Valid Case 2 Failed: Expected %s, got %s", opName, expected2, output2)
	}

	// Case 3: 3D slice with different strides
	operand3 := S(Bool, 10, 8, 6)
	starts3 := []int{1, 0, 1}
	limits3 := []int{10, 8, 6} // End index exclusive
	strides3 := []int{2, 3, 1}
	// Dim 0: (10-1)/2 = 4.5 -> 5 elements (indices 1, 3, 5, 7, 9)
	// Dim 1: (8-0)/3 = 2.66 -> 3 elements (indices 0, 3, 6)
	// Dim 2: (6-1)/1 = 5 -> 5 elements (indices 1, 2, 3, 4, 5)
	expected3 := S(Bool, 5, 3, 5)
	output3, err := Slice(operand3, starts3, limits3, strides3)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected3.Equal(output3)) {
		t.Fatalf("%s Valid Case 3 Failed: Expected %s, got %s", opName, expected3, output3)
	}

	// Case 4: Slice resulting in size 1 dimension
	operand4 := S(F32, 10)
	starts4 := []int{5}
	limits4 := []int{6}
	strides4 := []int{1}
	expected4 := S(F32, 1)
	output4, err := Slice(operand4, starts4, limits4, strides4)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected4.Equal(output4)) {
		t.Fatalf("%s Valid Case 4 Failed: Expected %s, got %s", opName, expected4, output4)
	}

	// Case 5: Slice taking full dimension with stride > 1
	operand5 := S(I8, 7)
	starts5 := []int{0}
	limits5 := []int{7}
	strides5 := []int{2}
	// Dim 0: (7-0)/2 = 3.5 -> 4 elements (indices 0, 2, 4, 6)
	expected5 := S(I8, 4)
	output5, err := Slice(operand5, starts5, limits5, strides5)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !(expected5.Equal(output5)) {
		t.Fatalf("%s Valid Case 5 Failed: Expected %s, got %s", opName, expected5, output5)
	}

	// --- Error Cases ---
	operand := S(F32, 10, 5) // Rank 2
	validStarts := []int{1, 1}
	validLimits := []int{8, 4}
	validStrides := []int{1, 1}

	// Error 1: Invalid operand DType
	_, err = Slice(shapes.Shape{DType: dtypes.InvalidDType, Dimensions: []int{10}}, []int{0}, []int{5}, []int{1})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 2: Incorrect length for starts
	_, err = Slice(operand, []int{1}, validLimits, validStrides)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 3: Incorrect length for limits
	_, err = Slice(operand, validStarts, []int{8}, validStrides)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 4: Incorrect length for strides
	_, err = Slice(operand, validStarts, validLimits, []int{1})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 5: Zero stride
	_, err = Slice(operand, validStarts, validLimits, []int{1, 0})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 6: Negative stride
	_, err = Slice(operand, validStarts, validLimits, []int{-1, 1})
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 7: Start index < 0
	_, err = Slice(operand, []int{-1, 1}, validLimits, validStrides)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 8: Start index >= dimSize
	_, err = Slice(operand, []int{10, 1}, validLimits, validStrides)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 9: Limit index < start index
	_, err = Slice(operand, validStarts, []int{0, 4}, validStrides) // limit[0]=0 < start[0]=1
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// Error 10: Limit index > dimSize
	_, err = Slice(operand, validStarts, []int{8, 6}, validStrides) // limit[1]=6 > dimSize[1]=5
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}
}

func TestArgMinMaxOp(t *testing.T) {
	// --- Valid Cases ---

	// Case 1: 1D tensor
	operand1 := S(F32, 10)
	expected1 := S(I32)
	output1 := must1(ArgMinMax(operand1, 0, I32))
	if !(expected1.Equal(output1)) {
		t.Fatalf("Valid Case 1 Failed: Expected %s, got %s", expected1, output1)
	}

	// Case 2: 2D tensor, single axis
	operand2 := S(F32, 5, 6)
	expected2 := S(I8, 5)
	output2 := must1(ArgMinMax(operand2, 1, expected2.DType))
	if !(expected2.Equal(output2)) {
		t.Fatalf("Valid Case 2 Failed: Expected %s, got %s", expected2, output2)
	}

	// Case 3: 3D tensor, multiple axes
	operand3 := S(F32, 4, 5, 6)
	expected3 := S(U64, 5, 6)
	output3 := must1(ArgMinMax(operand3, 0, expected3.DType))
	if !(expected3.Equal(output3)) {
		t.Fatalf("Valid Case 3 Failed: Expected %s, got %s", expected3, output3)
	}

	// --- Error Cases ---

	// Error 1: Invalid operand DType
	if panicked, _ := testutil.Try(func() {
		must1(ArgMinMax(shapes.Make(dtypes.InvalidDType, 10), 0, I32))
	}); !panicked {
		t.Fatalf("Expected panic")
	}

	// Error 2: Invalid axis (out of bounds)
	if panicked, _ := testutil.Try(func() {
		must1(ArgMinMax(operand1, 1, I32)) // operand1 is rank 1, axis 1 invalid
	}); !panicked {
		t.Fatalf("Expected panic")
	}

	// Error 3: Negative axis
	if panicked, _ := testutil.Try(func() {
		must1(ArgMinMax(operand2, -1, I32))
	}); !panicked {
		t.Fatalf("Expected panic")
	}
}

func TestReduceWindowOp(t *testing.T) {
	type testCase struct {
		name                 string
		operandShape         shapes.Shape
		windowDimensions     []int
		strides              []int
		baseDilations        []int
		windowDilations      []int
		paddings             [][2]int
		expectedShape        shapes.Shape
		expectError          bool
		errorMessageContains string // Optional: for more specific error checking
	}

	testCases := []testCase{
		{
			name:             "ScalarInput_AllNilParams_Defaults",
			operandShape:     shapes.Make(dtypes.Float32), // Rank 0
			windowDimensions: nil,                         // Should be handled as empty for rank 0
			strides:          nil,                         // Should be handled as empty for rank 0
			baseDilations:    nil,
			windowDilations:  nil,
			paddings:         nil, // Should be handled as empty for rank 0
			expectedShape:    shapes.Make(dtypes.Float32),
			expectError:      false,
		},
		{
			name:             "1D_AllNilParams_Defaults",
			operandShape:     shapes.Make(dtypes.Float32, 10),
			windowDimensions: nil, // Defaults to {1}
			strides:          nil, // Defaults to windowDimensions, in this case {1}
			baseDilations:    nil, // Defaults to {1}
			windowDilations:  nil, // Defaults to {1}
			paddings:         nil, // Defaults to {{0,0}}
			// Calculation: EffIn=10, EffWin=1. PaddedEffIn=10. Num=10-1=9. Out=(9/1)+1=10.
			expectedShape: shapes.Make(dtypes.Float32, 10),
			expectError:   false,
		},
		{
			name:             "1D_NonDefault_WindowDimensions_NilOthers",
			operandShape:     shapes.Make(dtypes.Float32, 10),
			windowDimensions: []int{3}, // EffWin=3
			strides:          nil,      // Default to windowDimensions, in this case {3}
			baseDilations:    nil,      // Default {1}
			windowDilations:  nil,      // Default {1}
			paddings:         nil,      // Default {{0,0}}
			// Calculation: EffIn=10, EffWin=3. PaddedEffIn=10. Num=10-3=7. Out=(7/3)+1=3.
			expectedShape: shapes.Make(dtypes.Float32, 3),
			expectError:   false,
		},
		{
			name:             "1D_NonDefault_Strides_NilOthers",
			operandShape:     shapes.Make(dtypes.Float32, 10),
			windowDimensions: nil, // Default {1} => EffWin=1
			strides:          []int{2},
			baseDilations:    nil,
			windowDilations:  nil,
			paddings:         nil,
			// Calculation: EffIn=10, EffWin=1. PaddedEffIn=10. Num=10-1=9. Out=(9/2)+1=4+1=5.
			expectedShape: shapes.Make(dtypes.Float32, 5),
			expectError:   false,
		},
		{
			name:             "1D_NonDefault_Paddings_NilOthers",
			operandShape:     shapes.Make(dtypes.Float32, 10),
			windowDimensions: nil, // Default {1} => EffWin=1
			strides:          nil, // Default {1}
			baseDilations:    nil,
			windowDilations:  nil,
			paddings:         [][2]int{{1, 1}},
			// Calculation: EffIn=10, EffWin=1. PaddedEffIn=10+1+1=12. Num=12-1=11. Out=(11/1)+1=12.
			expectedShape: shapes.Make(dtypes.Float32, 12),
			expectError:   false,
		},
		{
			name:             "1D_NonDefault_BaseDilations",
			operandShape:     shapes.Make(dtypes.Float32, 5),
			windowDimensions: []int{3}, // EffWin=3
			strides:          []int{1},
			baseDilations:    []int{2}, // EffIn=(5-1)*2+1 = 9
			windowDilations:  nil,
			paddings:         nil,
			// Calculation: PaddedEffIn=9. Num=9-3=6. Out=(6/1)+1=7.
			expectedShape: shapes.Make(dtypes.Float32, 7),
			expectError:   false,
		},
		{
			name:             "1D_NonDefault_WindowDilations",
			operandShape:     shapes.Make(dtypes.Float32, 10),
			windowDimensions: []int{3},
			strides:          []int{1},
			baseDilations:    nil,
			windowDilations:  []int{2}, // EffWin=(3-1)*2+1=5
			paddings:         nil,
			// Calculation: EffIn=10. PaddedEffIn=10. Num=10-5=5. Out=(5/1)+1=6.
			expectedShape: shapes.Make(dtypes.Float32, 6),
			expectError:   false,
		},
		{
			name:             "2D_Comprehensive_AllNonDefault",
			operandShape:     shapes.Make(dtypes.Int32, 10, 12),
			windowDimensions: []int{3, 4},
			strides:          []int{2, 3},
			baseDilations:    []int{2, 1},
			windowDilations:  []int{1, 2},
			paddings:         [][2]int{{1, 1}, {0, 2}},
			// Dim0: In=10,Win=3,Str=2,Pad=[1,1],BD=2,WD=1. EffIn=(10-1)*2+1=19. EffWin=(3-1)*1+1=3. PaddedEffIn=19+1+1=21. Num=21-3=18. Out=18/2+1=10.
			// Dim1: In=12,Win=4,Str=3,Pad=[0,2],BD=1,WD=2. EffIn=(12-1)*1+1=12. EffWin=(4-1)*2+1=7. PaddedEffIn=12+0+2=14. Num=14-7=7. Out=7/3+1=2+1=3.
			expectedShape: shapes.Make(dtypes.Int32, 10, 3),
			expectError:   false,
		},
		{
			name:             "Rank4_Image_NHWC_Style_VariedParams",
			operandShape:     shapes.Make(dtypes.Float32, 1, 20, 22, 3), // N, H, W, C
			windowDimensions: []int{1, 3, 3, 1},                         // Window on H, W
			strides:          []int{1, 2, 2, 1},                         // Stride on H, W
			baseDilations:    nil,                                       // Default {1,1,1,1}
			windowDilations:  nil,                                       // Default {1,1,1,1}
			paddings:         [][2]int{{0, 0}, {1, 0}, {0, 1}, {0, 0}},  // Padding H (low), W (high)
			// Dim0(N): In=1,Win=1,Str=1,Pad0,BD1,WD1. EffIn=1,EffWin=1.Padded=1.Num=0.Out=1.
			// Dim1(H): In=20,Win=3,Str=2,PadL=1,PadH=0,BD1,WD1. EffIn=20,EffWin=3.Padded=20+1+0=21.Num=21-3=18.Out=18/2+1=10.
			// Dim2(W): In=22,Win=3,Str=2,PadL=0,PadH=1,BD1,WD1. EffIn=22,EffWin=3.Padded=22+0+1=23.Num=23-3=20.Out=20/2+1=11.
			// Dim3(C): In=3,Win=1,Str=1,Pad0,BD1,WD1. EffIn=3,EffWin=1.Padded=3.Num=2.Out=3.
			expectedShape: shapes.Make(dtypes.Float32, 1, 10, 11, 3),
			expectError:   false,
		},
		{
			name:                 "Error_WindowTooLarge_NoPadding",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{6}, // EffWin=6
			strides:              []int{1},
			paddings:             nil, // PaddedEffIn=5
			expectError:          true,
			errorMessageContains: "effective window dimension 6 for axis 0 is larger than padded effective input dimension 5",
		},
		{
			name:                 "Error_InvalidStrideZero",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{2},
			strides:              []int{0},
			expectError:          true,
			errorMessageContains: "strides[0]=0 must be >= 1",
		},
		{
			name:                 "Error_InvalidWindowDimZero_FromNonNil",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{0},
			strides:              []int{1},
			expectError:          true,
			errorMessageContains: "windowDimensions[0]=0 must be >= 1",
		},
		{
			name:                 "Error_NegativePadding_FromNonNil",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{2},
			strides:              []int{1},
			paddings:             [][2]int{{-1, 0}},
			expectError:          true,
			errorMessageContains: "paddings[0]=[-1, 0] must be non-negative",
		},
		{
			name:                 "Error_InvalidBaseDilationZero",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{2},
			strides:              []int{1},
			baseDilations:        []int{0},
			expectError:          true,
			errorMessageContains: "baseDilations[0]=0 must be >= 1",
		},
		{
			name:                 "Error_InvalidWindowDilationZero",
			operandShape:         shapes.Make(dtypes.Float32, 5),
			windowDimensions:     []int{2},
			strides:              []int{1},
			windowDilations:      []int{0},
			expectError:          true,
			errorMessageContains: "windowDilations[0]=0 must be >= 1",
		},
		{
			name:                 "Error_StridesNotNil_WrongLengthForRank",
			operandShape:         shapes.Make(dtypes.Float32, 5, 5), // Rank 2
			windowDimensions:     nil,                               // Defaults to {1,1}
			strides:              []int{1},                          // Error: len 1, rank 2
			expectError:          true,
			errorMessageContains: "len(strides)=1, but operand rank is 2",
		},
		{
			name:                 "Error_WindowDimensionsNotNil_WrongLengthForRank",
			operandShape:         shapes.Make(dtypes.Float32, 5, 5), // Rank 2
			windowDimensions:     []int{1},                          // Error: len 1, rank 2
			strides:              nil,
			expectError:          true,
			errorMessageContains: "len(windowDimensions)=1, but operand rank is 2",
		},
		{
			name:                 "Error_PaddingsNotNil_WrongLengthForRank",
			operandShape:         shapes.Make(dtypes.Float32, 5, 5), // Rank 2
			windowDimensions:     nil,
			strides:              nil,
			paddings:             [][2]int{{0, 0}}, // Error: len 1, rank 2
			expectError:          true,
			errorMessageContains: "len(paddings)=1, but operand rank is 2",
		},
		{
			name:                 "Error_BaseDilationsNotNil_WrongLength",
			operandShape:         shapes.Make(dtypes.Float32, 5, 5), // Rank 2
			baseDilations:        []int{1},                          // Error: len 1, rank 2
			expectError:          true,
			errorMessageContains: "baseDilations is not nil and len(baseDilations)=1, but operand rank is 2",
		},
		{
			name:                 "Error_WindowDilationsNotNil_WrongLength",
			operandShape:         shapes.Make(dtypes.Float32, 5, 5), // Rank 2
			windowDilations:      []int{1},                          // Error: len 1, rank 2
			expectError:          true,
			errorMessageContains: "windowDilations is not nil and len(windowDilations)=1, but operand rank is 2",
		},
		{
			// This case would lead to outputDim 0 if the formula `(Num/Stride)+1` was used naively with Num < 0.
			// The pre-check `effectiveWindowDim > paddedEffectiveInputDim` should catch this.
			name:                 "NearZeroOutputDim_HandledByPreCheck",
			operandShape:         shapes.Make(dtypes.Float32, 2), // InputDim=2
			windowDimensions:     []int{3},                       // EffWin=3
			strides:              []int{1},
			paddings:             nil, // PaddedEffIn=2
			expectError:          true,
			errorMessageContains: "effective window dimension 3 for axis 0 is larger than padded effective input dimension 2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outputShape, err := ReduceWindow(
				tc.operandShape,
				tc.windowDimensions,
				tc.strides,
				tc.baseDilations,
				tc.windowDilations,
				tc.paddings,
			)

			if tc.expectError {
				if err == nil {
					t.Fatalf("Expected an error for test case: %s", tc.name)
				}
				if tc.errorMessageContains != "" {
					if !strings.Contains(err.Error(), tc.errorMessageContains) {
						t.Errorf("Error message mismatch for: %s", tc.name)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("Did not expect an error for test case: %s (error was: %v)", tc.name, err)
				}
				if !(tc.expectedShape.Equal(outputShape)) {
					t.Errorf("Mismatch in output shape for test case: %s. Expected %s, Got %s", tc.name, tc.expectedShape, outputShape)
				}
			}
		})
	}
}

func TestSelectAndScatterOp(t *testing.T) {
	operand := shapes.Make(dtypes.Float32, 10, 20)
	source := shapes.Make(dtypes.Float32, 5, 10)
	windowDimensions := []int{2, 2}
	strides := []int{2, 2}

	outputShape, err := SelectAndScatter(operand, source, windowDimensions, strides, nil)
	if err != nil {
		t.Fatalf("Unexpected error in SelectAndScatter: %v", err)
	}
	if !outputShape.Equal(operand) {
		t.Errorf("SelectAndScatter output shape %s, expected %s", outputShape, operand)
	}

	// Mismatched source shape
	badSource := shapes.Make(dtypes.Float32, 5, 5)
	_, err = SelectAndScatter(operand, badSource, windowDimensions, strides, nil)
	if err == nil {
		t.Errorf("Expected error for mismatched source shape")
	}

	// Mismatched DType
	badDTypeSource := shapes.Make(dtypes.Int32, 5, 10)
	_, err = SelectAndScatter(operand, badDTypeSource, windowDimensions, strides, nil)
	if err == nil {
		t.Errorf("Expected error for mismatched DType")
	}
}

func TestGather(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		// operand: F32[3, 4], startIndices: I32[2, 1]
		// Gather 2 rows from a 3x4 matrix.
		output, err := Gather(
			S(F32, 3, 4), // operand
			S(I32, 2, 1), // startIndices
			1,            // indexVectorAxis
			[]int{1},     // offsetOutputAxes
			[]int{0},     // collapsedSliceAxes
			[]int{0},     // startIndexMap
			[]int{1, 4},  // sliceSizes
			false,        // indicesAreSorted
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !(S(F32, 2, 4).Equal(output)) {
			t.Errorf("expected F32[2, 4], got %s", output)
		}
	})

	t.Run("HighIndexVectorAxisLowRankOperand", func(t *testing.T) {
		// Regression test: indexVectorAxis refers to startIndices, not operand.
		// A high indexVectorAxis is valid when startIndices has high rank,
		// even if the operand has low rank (e.g. rank 2).
		// This is the pattern used by CLIP text models (aten.index.Tensor).
		//
		// operand: Bool[1, 12], startIndices: I32[1, 1, 12, 1, 1]
		// indexVectorAxis=4 is valid because startIndices.Rank()=5.
		output, err := Gather(
			S(Bool, 1, 12),         // operand
			S(I32, 1, 1, 12, 1, 1), // startIndices
			4,                      // indexVectorAxis (valid: <= startIndices.Rank())
			[]int{},                // offsetOutputAxes
			[]int{0, 1},            // collapsedSliceAxes
			[]int{0},               // startIndexMap
			[]int{1, 1},            // sliceSizes
			false,                  // indicesAreSorted
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !(S(Bool, 1, 1, 12, 1).Equal(output)) {
			t.Errorf("expected Bool[1, 1, 12, 1], got %s", output)
		}
	})

	t.Run("IndexVectorAxisOutOfRange", func(t *testing.T) {
		// indexVectorAxis=3 is out of range for startIndices of rank 2.
		_, err := Gather(
			S(F32, 3, 4), // operand
			S(I32, 2, 1), // startIndices (rank 2)
			3,            // indexVectorAxis (invalid: > startIndices.Rank())
			[]int{1},     // offsetOutputAxes
			[]int{0},     // collapsedSliceAxes
			[]int{0},     // startIndexMap
			[]int{1, 4},  // sliceSizes
			false,        // indicesAreSorted
		)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "indexVectorAxis=3 is out of range") {
			t.Errorf("Expected %v to contain %v", err.Error(), "indexVectorAxis=3 is out of range")
		}
	})
}

func TestPad(t *testing.T) {
	// 1. Prefixing padding; Infixing padding; Suffix padding.
	t.Run("PrefixInfixSuffix", func(t *testing.T) {
		output, err := Pad(
			S(F32, 2, 3),                           // operand
			PadAxis{Start: 1, End: 0, Interior: 0}, // prefix axis 0
			PadAxis{Start: 0, End: 2, Interior: 1}, // infix / suffix axis 1
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Axis 0: dim=2, start=1, end=0, interior=0 => 2 + 1 + 0 + (2-1)*0 = 3
		// Axis 1: dim=3, start=0, end=2, interior=1 => 3 + 0 + 2 + (3-1)*1 = 7
		if !(S(F32, 3, 7).Equal(output)) {
			t.Errorf("expected F32[3, 7], got %s", output)
		}
	})

	// 2. Check that padding works when it is on the last axis, and when the last axis is untouched.
	// 3. That untouched axes remain untouched.
	t.Run("UntouchedAxes", func(t *testing.T) {
		output, err := Pad(
			S(F32, 4, 5, 2),                        // operand
			PadAxis{Start: 0, End: 0, Interior: 0}, // padding on first axis is zero
			PadAxis{Start: 1, End: 1, Interior: 0}, // padding on middle axis
			// last axis untouched (omitted)
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Axis 0: 4
		// Axis 1: 5 + 1 + 1 = 7
		// Axis 2: 2
		if !(S(F32, 4, 7, 2).Equal(output)) {
			t.Errorf("expected F32[4, 7, 2], got %s", output)
		}
	})

	t.Run("LastAxisPadding", func(t *testing.T) {
		output, err := Pad(
			S(F32, 2, 2, 2),                        // operand
			PadAxis{Start: 0, End: 0, Interior: 0}, // untouched
			PadAxis{Start: 0, End: 0, Interior: 0}, // untouched
			PadAxis{Start: 0, End: 0, Interior: 2}, // padding on the last axis
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Axis 0: 2
		// Axis 1: 2
		// Axis 2: 2 + 0 + 0 + (2-1)*2 = 4
		if !(S(F32, 2, 2, 4).Equal(output)) {
			t.Errorf("expected F32[2, 2, 4], got %s", output)
		}
	})

	t.Run("MissingAxes", func(t *testing.T) {
		output, err := Pad(
			S(F32, 5, 3), // operand
			PadAxis{Start: 2, End: 2, Interior: 0},
			// missing axis
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Axis 0: 5 + 4 = 9
		// Axis 1: 3
		if !(S(F32, 9, 3).Equal(output)) {
			t.Errorf("expected F32[9, 3], got %s", output)
		}
	})
}

// --- Axis Name Propagation Tests ---

func TestBinaryOp_AxisNames(t *testing.T) {
	t.Run("MatchingNames", func(t *testing.T) {
		lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		rhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := BinaryOp(OpTypeAdd, lhs, rhs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
		// Dynamic dim propagates.
		if ok, diff := testutil.IsEqual([]int{-1, 512}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, 512}, output.Dimensions)
		}
	})

	t.Run("OneNamed", func(t *testing.T) {
		// lhs has names, rhs does not → adopt lhs names.
		lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		rhs := S(F32, 1, 512)
		output, err := BinaryOp(OpTypeAdd, lhs, rhs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})

	t.Run("BothUnnamed", func(t *testing.T) {
		lhs := S(F32, 2, 3)
		rhs := S(F32, 2, 3)
		output, err := BinaryOp(OpTypeAdd, lhs, rhs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})

	t.Run("ConflictingNames", func(t *testing.T) {
		lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		rhs := SD(F32, []int{-1, 512}, []string{"time", ""})
		_, err := BinaryOp(OpTypeAdd, lhs, rhs)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "different axis names") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "different axis names")
		}
	})

	t.Run("DynamicBroadcast", func(t *testing.T) {
		// lhs: [batch=-1, 1], rhs: [1, 512] → output: [batch=-1, 512]
		lhs := SD(F32, []int{-1, 1}, []string{"batch", ""})
		rhs := S(F32, 1, 512)
		output, err := BinaryOp(OpTypeMul, lhs, rhs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, 512}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, 512}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})

	t.Run("BothDynamic", func(t *testing.T) {
		// Both sides dynamic non-broadcast → output is dynamic.
		lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		rhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := BinaryOp(OpTypeAdd, lhs, rhs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual(shapes.DynamicDim, output.Dimensions[0]); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, shapes.DynamicDim, output.Dimensions[0])
		}
	})

	t.Run("ScalarWithNamed", func(t *testing.T) {
		// Scalar + named tensor → named tensor (scalar path returns rhs directly).
		scalar := S(F32)
		named := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := BinaryOp(OpTypeAdd, scalar, named)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})
}

func TestComparisonOp_AxisNames(t *testing.T) {
	lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
	rhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
	output, err := ComparisonOp(OpTypeEqual, lhs, rhs)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if ok, diff := testutil.IsEqual(dtypes.Bool, output.DType); !ok {
		t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, dtypes.Bool, output.DType)
	}
	if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
		t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
	}
}

func TestUnaryOp_AxisNames(t *testing.T) {
	named := SD(F32, []int{-1, 512}, []string{"batch", ""})
	output, err := UnaryOp(OpTypeExp, named)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	// UnaryOp returns operand directly, so names are preserved.
	if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
		t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
	}
}

func TestTransposeOp_AxisNames(t *testing.T) {
	t.Run("PermutesNames", func(t *testing.T) {
		s := SD(F32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
		output, err := Transpose(s, []int{1, 0, 2})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, -1, 768}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, -1, 768}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"seq_len", "batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"seq_len", "batch", ""}, output.AxisNames)
		}
	})

	t.Run("NoNames", func(t *testing.T) {
		s := S(F32, 2, 3)
		output, err := Transpose(s, []int{1, 0})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})
}

func TestBroadcastInDim(t *testing.T) {
	t.Run("DynamicDimMatch", func(t *testing.T) {
		operand := SD(F32, []int{-1, 512}, []string{"batch", ""})
		outputShape := SD(F32, []int{-1, 512}, []string{"batch", ""})
		err := BroadcastInDim(operand, outputShape, []int{0, 1}, nil)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})

	t.Run("DynamicDimMismatch", func(t *testing.T) {
		// A dynamic dimension cannot be broadcast to a static dimension.
		operand := SD(F32, []int{-1, 512}, []string{"batch", ""})
		outputShape := S(F32, 32, 512)
		err := BroadcastInDim(operand, outputShape, []int{0, 1}, nil)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "must be preserved as dynamic in output shape") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "must be preserved as dynamic in output shape")
		}
	})

	t.Run("BroadcastToUnknownDynamicAxis", func(t *testing.T) {
		// Broadcasting dimension 1 to a dynamic dimension is allowed, if its name is known.
		operand := S(F32, 1, 512)
		outputShape := SD(F32, []int{-1, 512}, []string{"batch", ""})
		err := BroadcastInDim(operand, outputShape, []int{0, 1}, nil)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "cannot introduce an unknown dynamic axis name -- the dynamic axis must be known from the input parameters") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "cannot introduce an unknown dynamic axis name -- the dynamic axis must be known from the input parameters")
		}
	})

	t.Run("BroadcastToKnownDynamicAxis", func(t *testing.T) {
		// Broadcasting dimension 1 to a dynamic dimension is allowed, if its name is known.
		operand := SD(F32, []int{-1, 512}, []string{"seqLen", ""})
		outputShape := SD(F32, []int{-1, -1, 512}, []string{"batch", "seqLen", ""})
		err := BroadcastInDim(operand, outputShape, []int{1, 2}, sets.MakeWith("batch"))
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})
}

func TestReduceOp_AxisNames(t *testing.T) {
	t.Run("RemovesReducedAxis", func(t *testing.T) {
		s := SD(F32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
		// Reduce last axis → names for remaining axes preserved.
		output, err := Reduce(s, []int{2})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, -1}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, -1}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "seq_len"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "seq_len"}, output.AxisNames)
		}
	})

	t.Run("ReduceToScalar", func(t *testing.T) {
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := Reduce(s, []int{0, 1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !(output.IsScalar()) {
			t.Fatalf("Condition expected to be true")
		}
	})

	t.Run("NoNames", func(t *testing.T) {
		s := S(F32, 4, 5, 6)
		output, err := Reduce(s, []int{1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})
}

func TestConcatenateOp_AxisNames(t *testing.T) {
	t.Run("UnifiesNames", func(t *testing.T) {
		s1 := SD(F32, []int{-1, 512}, []string{"batch", ""})
		s2 := SD(F32, []int{-1, 256}, []string{"batch", ""})
		output, err := Concatenate([]shapes.Shape{s1, s2}, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})

	t.Run("ConcatAxisNameSymbolicDynamic", func(t *testing.T) {
		s1 := SD(F32, []int{-1, 10}, []string{"batch", "a"})
		s2 := SD(F32, []int{-1, 10}, []string{"batch", "a"})
		output, err := Concatenate([]shapes.Shape{s1, s2}, 0) // concat on axis 0 (dynamic)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"=batch+batch", "a"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"=batch+batch", "a"}, output.AxisNames)
		}
	})

	t.Run("NonConcatAxisNameConflictErrors", func(t *testing.T) {
		s1 := SD(F32, []int{-1, 512}, []string{"batch", ""})
		s2 := SD(F32, []int{-1, 512}, []string{"time", ""})
		_, err := Concatenate([]shapes.Shape{s1, s2}, 1) // concat on axis 1, conflict on axis 0
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "axis name conflict") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "axis name conflict")
		}
	})

	t.Run("DynamicConcatAxisSymbolic", func(t *testing.T) {
		s1 := SD(F32, []int{-1, -1}, []string{"batch", "seq"})
		s2 := SD(F32, []int{-1, 128}, []string{"batch", ""})
		output, err := Concatenate([]shapes.Shape{s1, s2}, 1) // Concatenating on dynamic axis 1
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "=128+seq"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "=128+seq"}, output.AxisNames)
		}
	})
}

func TestSliceOp_AxisNames(t *testing.T) {
	t.Run("PreservesNames", func(t *testing.T) {
		s := S(F32, 10, 20).WithAxisNames("height", "width")
		output, err := Slice(s, []int{2, 5}, []int{8, 15}, []int{1, 1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"height", "width"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"height", "width"}, output.AxisNames)
		}
		if ok, diff := testutil.IsEqual([]int{6, 10}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{6, 10}, output.Dimensions)
		}
	})

	t.Run("DynamicAxis", func(t *testing.T) {
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		// Use -1 for limit to mean "full axis" (results in -1 dimension).
		output, err := Slice(s, []int{0, 0}, []int{-1, 256}, []int{1, 1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual(shapes.DynamicDim, output.Dimensions[0]); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, shapes.DynamicDim, output.Dimensions[0])
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})

	t.Run("NoNames", func(t *testing.T) {
		s := S(F32, 10, 20)
		output, err := Slice(s, []int{0, 0}, []int{5, 10}, []int{1, 1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})
}

func TestArgMinMaxOp_AxisNames(t *testing.T) {
	t.Run("RemovesAxisName", func(t *testing.T) {
		s := SD(F32, []int{-1, -1, 768}, []string{"batch", "seq_len", ""})
		output, err := ArgMinMax(s, 2, I32)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, -1}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, -1}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "seq_len"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "seq_len"}, output.AxisNames)
		}
	})

	t.Run("RemovesFirstAxis", func(t *testing.T) {
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := ArgMinMax(s, 0, I32)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{512}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{512}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{""}, output.AxisNames)
		}
	})

	t.Run("NoNames", func(t *testing.T) {
		s := S(F32, 5, 6)
		output, err := ArgMinMax(s, 1, I32)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})
}

func TestReduceWindowOp_AxisNames(t *testing.T) {
	t.Run("PreservesNames", func(t *testing.T) {
		s := S(F32, 1, 20, 22, 3).WithAxisNames("batch", "height", "width", "channels")
		output, err := ReduceWindow(s, []int{1, 3, 3, 1}, []int{1, 2, 2, 1}, nil, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "height", "width", "channels"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "height", "width", "channels"}, output.AxisNames)
		}
	})

	t.Run("DynamicDim", func(t *testing.T) {
		s := SD(F32, []int{-1, 10}, []string{"batch", "seq"})
		output, err := ReduceWindow(s, []int{1, 3}, []int{1, 1}, nil, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual(shapes.DynamicDim, output.Dimensions[0]); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, shapes.DynamicDim, output.Dimensions[0])
		}
		if ok, diff := testutil.IsEqual(8, output.Dimensions[1]); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, 8, output.Dimensions[1])
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "seq"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "seq"}, output.AxisNames)
		}
	})

	t.Run("NoNames", func(t *testing.T) {
		s := S(F32, 10)
		output, err := ReduceWindow(s, []int{3}, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if output.AxisNames != nil {
			t.Fatalf("Expected nil, got %v", output.AxisNames)
		}
	})
}

func TestDotGeneral_DynamicAndNames(t *testing.T) {
	t.Run("DynamicBatchDim", func(t *testing.T) {
		lhs := SD(F32, []int{-1, 512}, []string{"batch", ""})
		rhs := SD(F32, []int{-1, 512, 256}, []string{"batch", "", ""})
		output, err := DotGeneral(lhs, []int{1}, []int{0}, rhs, []int{1}, []int{0}, DotGeneralConfig{})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, 256}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, 256}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})

	t.Run("AxisNameConflict", func(t *testing.T) {
		lhs := SD(F32, []int{-1, 512}, []string{"batch1", ""})
		rhs := SD(F32, []int{-1, 512}, []string{"batch2", ""})
		_, err := DotGeneral(lhs, []int{1}, []int{0}, rhs, []int{1}, []int{0}, DotGeneralConfig{})
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "axis name conflict") {
			t.Fatalf("Expected %v to contain %v", err.Error(), "axis name conflict")
		}
	})

	t.Run("AnonymousAxisConflict", func(t *testing.T) {
		lhs := SD(F32, []int{-1, 512}, []string{shapes.AnonymousAxis, ""})
		rhs := SD(F32, []int{-1, 512}, []string{shapes.AnonymousAxis, ""})
		_, err := DotGeneral(lhs, []int{1}, []int{0}, rhs, []int{1}, []int{0}, DotGeneralConfig{})
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
	})
}


func TestConcatenateOp_Dynamic(t *testing.T) {
	t.Run("ConcatOnDynamicAxis", func(t *testing.T) {
		s1 := SD(F32, []int{-1, 512}, []string{"batch", ""})
		s2 := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := Concatenate([]shapes.Shape{s1, s2}, 0)
		if err != nil {
			t.Fatalf("Expected no error when concatenating on dynamic axis, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"=batch+batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"=batch+batch", ""}, output.AxisNames)
		}
	})

	t.Run("ConcatPreservesDynamicNonConcatAxes", func(t *testing.T) {
		s1 := SD(F32, []int{-1, 10}, []string{"batch", ""})
		s2 := SD(F32, []int{-1, 20}, []string{"batch", ""})
		output, err := Concatenate([]shapes.Shape{s1, s2}, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, 30}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, 30}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})
}

func TestSliceOp_DynamicCorrect(t *testing.T) {
	t.Run("StaticSliceOfDynamicAxis", func(t *testing.T) {
		// Slicing a dynamic axis with static bounds should result in a static output size.
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := Slice(s, []int{0, 0}, []int{10, 256}, []int{1, 1})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// The first axis should now be static size 10.
		if ok, diff := testutil.IsEqual([]int{10, 256}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{10, 256}, output.Dimensions)
		}
	})
}

func TestPadOp_Dynamic(t *testing.T) {
	t.Run("PadOnDynamicAxis", func(t *testing.T) {
		// Padding on a dynamic axis should be an error if padding is non-zero.
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		_, err := Pad(s, PadAxis{Start: 1, End: 1})
		if err == nil {
			t.Fatalf("Expected error when padding on dynamic axis, but got nil")
		}
		if !strings.Contains(err.Error(), "padding on a dynamic axis") {
			t.Errorf("Expected error message to mention padding on dynamic axis, got: %v", err)
		}
	})

	t.Run("ZeroPadOnDynamicAxis", func(t *testing.T) {
		// Zero padding on a dynamic axis should be allowed.
		s := SD(F32, []int{-1, 512}, []string{"batch", ""})
		output, err := Pad(s, PadAxis{Start: 0, End: 0})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, 512}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, 512}, output.Dimensions)
		}
	})
}

func TestGatherOp_Dynamic(t *testing.T) {
	t.Run("PropagatesBatchNames", func(t *testing.T) {
		operand := S(F32, 10, 20)
		startIndices := SD(I32, []int{-1, 1}, []string{"batch", ""})
		output, err := Gather(
			operand, startIndices, 1,
			[]int{1}, []int{0}, []int{0}, []int{1, 20}, false,
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", ""}, output.AxisNames); !ok {
			t.Fatalf("Expected axis names %v, got %v"+"\nDiff:\n"+diff, []string{"batch", ""}, output.AxisNames)
		}
	})
}

func TestReduceOp_Dynamic(t *testing.T) {
	t.Run("PreservesOtherDynamicAxes", func(t *testing.T) {
		s := SD(F32, []int{-1, 10, -1}, []string{"batch", "", "seq"})
		output, err := Reduce(s, []int{1}) // Reduce middle axis
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, -1}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, -1}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "seq"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "seq"}, output.AxisNames)
		}
	})
}

func TestArgMinMaxOp_Dynamic(t *testing.T) {
	t.Run("PreservesOtherDynamicAxes", func(t *testing.T) {
		s := SD(F32, []int{-1, 10, -1}, []string{"batch", "", "seq"})
		output, err := ArgMinMax(s, 1, dtypes.Int32)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if ok, diff := testutil.IsEqual([]int{-1, -1}, output.Dimensions); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []int{-1, -1}, output.Dimensions)
		}
		if ok, diff := testutil.IsEqual([]string{"batch", "seq"}, output.AxisNames); !ok {
			t.Fatalf("Expected %v, got %v"+"\nDiff:\n"+diff, []string{"batch", "seq"}, output.AxisNames)
		}
	})
}

func TestScatterOp_Dynamic(t *testing.T) {
	t.Run("AllowsDynamicOperandAndUpdates", func(t *testing.T) {
		operand := SD(F32, []int{-1, 10}, []string{"batch", ""})
		indices := S(I32, 5, 1)
		updates := SD(F32, []int{5, 10}, []string{"", ""})
		// Scatter 5 updates to the dynamic batch axis
		output, err := Scatter(
			operand, indices, updates, 1,
			[]int{1}, []int{0}, []int{0},
		)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if !output.Equal(operand) {
			t.Fatalf("Expected output to be equal to operand")
		}
	})
}
