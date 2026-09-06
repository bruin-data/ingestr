package onnxgomlx

import (
	"fmt"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/compute/shapes"
	. "github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/graph/graphtest"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/support/testutil"
	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestONNXWhere(t *testing.T) {
	graphtest.RunTestGraphFn(t, "Where(): Dense", func(g *Graph) (inputs, outputs []*Node) {
		cond := ConvertDType(Iota(g, shapes.Make(dtypes.Int32, 3, 2), -1), dtypes.Bool)
		onTrue := OnePlus(IotaFull(g, shapes.Make(dtypes.Float32, 3, 2)))
		onFalse := Neg(onTrue)
		inputs = []*Node{cond, onTrue, onFalse}
		// Use strict mode model since all dtypes match
		m := createTestModelWithDTypePromoConfig(false, false)
		outputs = []*Node{
			m.onnxWhere([]*Node{cond, onTrue, onFalse}),
			m.onnxWhere([]*Node{Const(g, true), onTrue, onFalse}),
			m.onnxWhere([]*Node{Const(g, false), onTrue, onFalse}),
			m.onnxWhere([]*Node{cond, Const(g, float32(100)), onFalse}),
			m.onnxWhere([]*Node{cond, onTrue, Const(g, []float32{100, 1000})}),
		}
		return
	}, []any{
		[][]float32{{-1, 2}, {-3, 4}, {-5, 6}},
		[][]float32{{1, 2}, {3, 4}, {5, 6}},
		[][]float32{{-1, -2}, {-3, -4}, {-5, -6}},
		[][]float32{{-1, 100}, {-3, 100}, {-5, 100}},
		[][]float32{{100, 2}, {100, 4}, {100, 6}},
	}, -1)
}

func TestONNXGather(t *testing.T) {
	graphtest.RunTestGraphFn(t, "onnxGather(axis=0)", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]float32{{1.0, 1.2}, {2.3, 3.4}, {4.5, 5.7}})
		indices := Const(g, [][]int32{{0, 1}, {1, 2}})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGather(data, indices, 0)}
		return
	}, []any{
		[][][]float32{
			{
				{1.0, 1.2},
				{2.3, 3.4},
			},
			{
				{2.3, 3.4},
				{4.5, 5.7},
			},
		},
	}, -1)

	graphtest.RunTestGraphFn(t, "onnxGather(axis=1)", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]float32{
			{1.0, 1.2, 1.9},
			{2.3, 3.4, 3.9},
			{4.5, 5.7, 5.9},
		})
		indices := Const(g, [][]int32{{0, 2}})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGather(data, indices, 1)}
		return
	}, []any{
		[][][]float32{
			{{1.0, 1.9}},
			{{2.3, 3.9}},
			{{4.5, 5.9}},
		},
	}, -1)

	// Test negative indices: -1 means last element, -2 means second-to-last, etc.
	graphtest.RunTestGraphFn(t, "onnxGather(axis=0, negative indices)", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]float32{{1.0, 1.2}, {2.3, 3.4}, {4.5, 5.7}})
		// -1 -> 2 (last), -2 -> 1, -3 -> 0
		indices := Const(g, []int32{-1, -2, -3})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGather(data, indices, 0)}
		return
	}, []any{
		// indices shape [3] -> output shape [3, 2]
		[][]float32{
			{4.5, 5.7}, // index -1 -> row 2
			{2.3, 3.4}, // index -2 -> row 1
			{1.0, 1.2}, // index -3 -> row 0
		},
	}, -1)

	graphtest.RunTestGraphFn(t, "onnxGather(axis=1, negative indices)", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]float32{
			{1.0, 1.2, 1.9},
			{2.3, 3.4, 3.9},
			{4.5, 5.7, 5.9},
		})
		// -1 -> 2 (last column)
		indices := Const(g, []int32{-1})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGather(data, indices, 1)}
		return
	}, []any{
		// data shape [3, 3], indices shape [1] -> output shape [3, 1]
		[][]float32{
			{1.9}, // row 0, col -1 (last)
			{3.9}, // row 1, col -1 (last)
			{5.9}, // row 2, col -1 (last)
		},
	}, -1)
}

func TestONNXGatherND(t *testing.T) {
	// Test case from ONNX specification example 1:
	// data = [[0,1],[2,3]]
	// indices = [[0,0],[1,1]]
	// output = [0,3]
	graphtest.RunTestGraphFn(t, "onnxGatherND: basic 2D", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]int32{{0, 1}, {2, 3}})
		indices := Const(g, [][]int32{{0, 0}, {1, 1}})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGatherND(data, indices, 0)}
		return
	}, []any{
		[]int32{0, 3},
	}, -1)

	// Test case from ONNX specification example 2:
	// data = [[0,1],[2,3]]
	// indices = [[1],[0]]
	// output = [[2,3],[0,1]]
	graphtest.RunTestGraphFn(t, "onnxGatherND: partial indexing", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]int32{{0, 1}, {2, 3}})
		indices := Const(g, [][]int32{{1}, {0}})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGatherND(data, indices, 0)}
		return
	}, []any{
		[][]int32{{2, 3}, {0, 1}},
	}, -1)

	// Test case from ONNX specification example 3:
	// data = [[[0,1],[2,3]],[[4,5],[6,7]]]
	// indices = [[0,1],[1,0]]
	// output = [[2,3],[4,5]]
	graphtest.RunTestGraphFn(t, "onnxGatherND: 3D data", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][][]int32{{{0, 1}, {2, 3}}, {{4, 5}, {6, 7}}})
		indices := Const(g, [][]int32{{0, 1}, {1, 0}})
		inputs = []*Node{data, indices}
		outputs = []*Node{onnxGatherND(data, indices, 0)}
		return
	}, []any{
		[][]int32{{2, 3}, {4, 5}},
	}, -1)
}

func TestTile(t *testing.T) {
	graphtest.RunTestGraphFn(t, "Tile 1D", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, []float32{1, 2})
		inputs = []*Node{operand}
		outputs = []*Node{onnxTile(operand, []int{2})}
		return
	}, []any{
		[]float32{1, 2, 1, 2},
	}, -1)

	graphtest.RunTestGraphFn(t, "Tile 2D", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{{1.0, 1.2}, {2.3, 3.4}, {4.5, 5.7}})
		inputs = []*Node{operand}
		outputs = []*Node{onnxTile(operand, []int{1, 2})}
		return
	}, []any{
		[][]float32{
			{1.0, 1.2, 1.0, 1.2},
			{2.3, 3.4, 2.3, 3.4},
			{4.5, 5.7, 4.5, 5.7},
		},
	}, -1)
}

func TestRangeCount(t *testing.T) {
	backend := testutil.BuildTestBackend()
	testFn := func(start, limit, delta any, want int) {
		startT := tensors.MustFromAnyValue(start)
		limitT := tensors.MustFromAnyValue(limit)
		deltaT := tensors.MustFromAnyValue(delta)
		got := rangeCount(backend, startT, limitT, deltaT)
		fmt.Printf("\trangeCount(start=%s, limit=%s, delta=%s) = %d (want %d)\n", startT, limitT, deltaT, got, want)
		assert.Equal(t, want, got)
	}

	testFn(uint8(3), uint8(9), uint8(3), 2)
	testFn(uint8(3), uint8(8), uint8(3), 2)
	testFn(uint8(3), uint8(7), uint8(3), 2)
	testFn(float32(3), float32(9.1), float32(3), 3)
	testFn(int32(10), int32(4), int32(-2), 3)
	testFn(int32(10), int32(5), int32(-2), 3)
	testFn(float64(10), float64(3.9), float64(-2), 4)
}

func TestOnnxGatherElements(t *testing.T) {
	graphtest.RunTestGraphFn(t, "GatherElements", func(g *Graph) (inputs, outputs []*Node) {
		data := Const(g, [][]float32{{1, 2}, {3, 4}})
		indices := Const(g, [][]int32{{0, 0}, {1, 0}})
		inputs = []*Node{data, indices}
		outputs = []*Node{
			onnxGatherElements(data, indices, 0),
			onnxGatherElements(data, indices, 1),
		}
		return
	}, []any{
		[][]float32{{1, 2}, {3, 2}},
		[][]float32{{1, 1}, {4, 3}},
	}, -1)

	graphtest.RunTestGraphFn(t, "GatherElements w/ incomplete indices", func(g *Graph) (inputs, outputs []*Node) {
		data := OnePlus(IotaFull(g, shapes.Make(dtypes.Float64, 3, 2)))
		indices0 := Const(g, [][]int8{{1, 2}})
		indices1 := Const(g, [][]int8{{0}, {0}, {1}})
		outputs = []*Node{
			onnxGatherElements(data, indices0, 0),
			onnxGatherElements(data, indices1, 1),
		}
		return
	}, []any{
		[][]float64{{3, 6}},
		[][]float64{{1}, {3}, {6}},
	}, -1)

	graphtest.RunTestGraphFn(t, "GatherElements: shape test with larger shapes", func(g *Graph) (inputs, outputs []*Node) {
		data := IotaFull(g, shapes.Make(dtypes.Float64, 3, 2, 512))
		indices := Iota(g, shapes.Make(dtypes.Int64, 3, 2, 7), 0)
		outputs = []*Node{
			Const(g, onnxGatherElements(data, indices, 2).Shape().Dimensions),
		}
		return
	}, []any{
		[]int64{3, 2, 7},
	}, -1)
}

func TestONNXCumSum(t *testing.T) {
	graphtest.RunTestGraphFn(t, "CumSum", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, []float32{1, 2, 3})
		inputs = []*Node{operand}
		outputs = []*Node{
			onnxCumSum(operand, 0, false, false),
			onnxCumSum(operand, 0, true, false),
			onnxCumSum(operand, 0, false, true),
			onnxCumSum(operand, 0, true, true),
		}
		return
	}, []any{
		[]float32{1, 3, 6},
		[]float32{0, 1, 3},
		[]float32{6, 5, 3},
		[]float32{5, 3, 0},
	}, -1)
}

func TestONNXFlatten(t *testing.T) {
	backend := testutil.BuildTestBackend()
	testIdx := 0
	flattenFn := func(shape shapes.Shape, splitAxis int) shapes.Shape {
		g := NewGraph(backend, fmt.Sprintf("Flatten #%d", testIdx))
		testIdx++
		operand := IotaFull(g, shape)
		newShape := onnxFlatten(operand, splitAxis).Shape()
		g.Finalize()
		return newShape
	}

	// Scalar becomes a 1x1 matrix.
	flattenFn(shapes.Make(dtypes.Float32), 0).Assert(dtypes.Float32, 1, 1)

	// Vector can be split in 2 different ways.
	flattenFn(shapes.Make(dtypes.Int32, 7), 0).Assert(dtypes.Int32, 1, 7)
	flattenFn(shapes.Make(dtypes.Int32, 7), 1).AssertDims(7, 1)

	// Higher-dimensional tensor.
	flattenFn(shapes.Make(dtypes.Float32, 7, 2, 3, 4), 2).AssertDims(14, 12)
}

func TestONNXDequantizeLinear(t *testing.T) {
	graphtest.RunTestGraphFn(t, "DequantizeLinear-scalar", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]int8{{-1, 0, 1}, {-2, 3, 4}})
		scale := Const(g, float32(3))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxDequantizeLinear(x, scale, nil, 1, scale.DType()),
		}
		return
	}, []any{
		[][]float32{{-3, 0, 3}, {-6, 9, 12}},
	}, -1)

	graphtest.RunTestGraphFn(t, "DequantizeLinear-outputDType", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]int8{{-1, 0, 1}, {-2, 3, 4}})
		scale := Const(g, float32(3))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxDequantizeLinear(x, scale, nil, 1, dtypes.Float64),
		}
		return
	}, []any{
		[][]float64{{-3, 0, 3}, {-6, 9, 12}},
	}, -1)

	graphtest.RunTestGraphFn(t, "DequantizeLinear-zero-point", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]int8{{-1, 0, 1}, {-2, 3, 4}})
		scale := Const(g, float32(3))
		zeroPoint := Const(g, int8(1))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxDequantizeLinear(x, scale, zeroPoint, 1, scale.DType()),
		}
		return
	}, []any{
		[][]float32{{-6, -3, 0}, {-9, 6, 9}},
	}, -1)

	graphtest.RunTestGraphFn(t, "DequantizeLinear-axis", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]int8{{-1, 0, 1}, {-2, 3, 4}})
		scale := Const(g, []float32{3, 30, 300})
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxDequantizeLinear(x, scale, nil, 1, scale.DType()),
		}
		return
	}, []any{
		[][]float32{{-3, 0, 300}, {-6, 90, 1200}},
	}, -1)
}

func TestONNXQuantizeLinear(t *testing.T) {
	graphtest.RunTestGraphFn(t, "QuantizeLinear-scalar", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{-3, 0, 3}, {-6, 9, 12}})
		scale := Const(g, float32(3))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, nil, 1, dtypes.Int8),
		}
		return
	}, []any{
		[][]int8{{-1, 0, 1}, {-2, 3, 4}},
	}, -1)

	graphtest.RunTestGraphFn(t, "QuantizeLinear-zero-point", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{-6, -3, 0}, {-9, 6, 9}})
		scale := Const(g, float32(3))
		zeroPoint := Const(g, int8(1))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, zeroPoint, 1, dtypes.Int8),
		}
		return
	}, []any{
		[][]int8{{-1, 0, 1}, {-2, 3, 4}},
	}, -1)

	graphtest.RunTestGraphFn(t, "QuantizeLinear-axis", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{-3, 0, 300}, {-6, 90, 1200}})
		scale := Const(g, []float32{3, 30, 300})
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, nil, 1, dtypes.Int8),
		}
		return
	}, []any{
		[][]int8{{-1, 0, 1}, {-2, 3, 4}},
	}, -1)

	graphtest.RunTestGraphFn(t, "QuantizeLinear-uint8", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{0, 127.5, 255}, {63.75, 191.25, 382.5}})
		scale := Const(g, float32(1.5))
		zeroPoint := Const(g, uint8(0))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, zeroPoint, 1, dtypes.Uint8),
		}
		return
	}, []any{
		// 63.75/1.5 = 42.5, rounds to 42 (round-half-to-even, 42 is even)
		[][]uint8{{0, 85, 170}, {42, 128, 255}},
	}, -1)

	// Test rounding behavior with .5 values
	// GoMLX Round uses round-half-to-even (banker's rounding)
	graphtest.RunTestGraphFn(t, "QuantizeLinear-rounding-half-values", func(g *Graph) (inputs, outputs []*Node) {
		// Using scale=2.0, these values will yield .5 after division:
		// Round-half-to-even: 0.5→0 (even), 1.5→2 (even), 2.5→2 (even), 3.5→4 (even), 4.5→4 (even), -0.5→0 (even), -1.5→-2 (even), -2.5→-2 (even)
		// 1.0/2.0=0.5 → 0, 3.0/2.0=1.5 → 2, 5.0/2.0=2.5 → 2,
		// 7.0/2.0=3.5 → 4, 9.0/2.0=4.5 → 4
		// -1.0/2.0=-0.5 → 0, -3.0/2.0=-1.5 → -2, -5.0/2.0=-2.5 → -2
		x := Const(g, [][]float32{{1.0, 3.0, 5.0, 7.0}, {9.0, -1.0, -3.0, -5.0}})
		scale := Const(g, float32(2.0))
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, nil, 1, dtypes.Int8),
		}
		return
	}, []any{
		[][]int8{{0, 2, 2, 4}, {4, 0, -2, -2}},
	}, -1)

	// Test negative axis support
	graphtest.RunTestGraphFn(t, "QuantizeLinear-negative-axis", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{-3, 0, 300}, {-6, 90, 1200}})
		scale := Const(g, []float32{3, 30, 300})
		inputs = []*Node{x, scale}
		outputs = []*Node{
			onnxQuantizeLinear(x, scale, nil, -1, dtypes.Int8), // -1 should be equivalent to axis=1 for rank-2 tensor
		}
		return
	}, []any{
		[][]int8{{-1, 0, 1}, {-2, 3, 4}},
	}, -1)
}

func TestONNX_DynamicQuantizeLinear(t *testing.T) {
	graphtest.RunTestGraphFn(t, "DequantizeLinear-scalar", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{-3, -0, 3}, {-6, 9, 12}})
		inputs = []*Node{x}
		y, yScale, yZeroPoint := onnxDynamicQuantizeLinear(x)
		outputs = []*Node{y, yScale, yZeroPoint}
		return
	}, []any{
		[][]uint8{{43, 85, 127}, {0, 212, 255}},
		float32(0.07058824),
		uint8(85),
	}, 1e-3)
}

func TestONNX_MatMulInteger(t *testing.T) {
	// Test basic MatMulInteger without zero points
	graphtest.RunTestGraphFn(t, "MatMulInteger-no-zero-points", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 3] int8 matrix
		a := Const(g, [][]int8{{1, 2, 3}, {4, 5, 6}})
		// B: [3, 2] int8 matrix
		b := Const(g, [][]int8{{1, 2}, {3, 4}, {5, 6}})
		inputs = []*Node{a, b}
		outputs = []*Node{onnxMatMulInteger(a, b, nil, nil)}
		return
	}, []any{
		// Expected: A @ B
		// [1*1+2*3+3*5, 1*2+2*4+3*6] = [22, 28]
		// [4*1+5*3+6*5, 4*2+5*4+6*6] = [49, 64]
		[][]int32{{22, 28}, {49, 64}},
	}, -1)

	// Test MatMulInteger with scalar zero points
	graphtest.RunTestGraphFn(t, "MatMulInteger-scalar-zero-points", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 3] int8 matrix
		a := Const(g, [][]int8{{1, 2, 3}, {4, 5, 6}})
		// B: [3, 2] int8 matrix
		b := Const(g, [][]int8{{1, 2}, {3, 4}, {5, 6}})
		// Zero points
		aZeroPoint := Const(g, int8(1))
		bZeroPoint := Const(g, int8(1))
		inputs = []*Node{a, b, aZeroPoint, bZeroPoint}
		outputs = []*Node{onnxMatMulInteger(a, b, aZeroPoint, bZeroPoint)}
		return
	}, []any{
		// Expected: (A - 1) @ (B - 1)
		// A-1 = [[0, 1, 2], [3, 4, 5]]
		// B-1 = [[0, 1], [2, 3], [4, 5]]
		// [0*0+1*2+2*4, 0*1+1*3+2*5] = [10, 13]
		// [3*0+4*2+5*4, 3*1+4*3+5*5] = [28, 40]
		[][]int32{{10, 13}, {28, 40}},
	}, -1)

	// Test MatMulInteger with uint8 inputs
	graphtest.RunTestGraphFn(t, "MatMulInteger-uint8", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 2] uint8 matrix
		a := Const(g, [][]uint8{{10, 20}, {30, 40}})
		// B: [2, 2] uint8 matrix
		b := Const(g, [][]uint8{{1, 2}, {3, 4}})
		// Zero points
		aZeroPoint := Const(g, uint8(5))
		bZeroPoint := Const(g, uint8(1))
		inputs = []*Node{a, b, aZeroPoint, bZeroPoint}
		outputs = []*Node{onnxMatMulInteger(a, b, aZeroPoint, bZeroPoint)}
		return
	}, []any{
		// Expected: (A - 5) @ (B - 1)
		// A-5 = [[5, 15], [25, 35]]
		// B-1 = [[0, 1], [2, 3]]
		// [5*0+15*2, 5*1+15*3] = [30, 50]
		// [25*0+35*2, 25*1+35*3] = [70, 130]
		[][]int32{{30, 50}, {70, 130}},
	}, -1)

	// Test MatMulInteger with only A zero point
	graphtest.RunTestGraphFn(t, "MatMulInteger-a-zero-point-only", func(g *Graph) (inputs, outputs []*Node) {
		a := Const(g, [][]int8{{2, 3}, {4, 5}})
		b := Const(g, [][]int8{{1, 2}, {3, 4}})
		aZeroPoint := Const(g, int8(1))
		inputs = []*Node{a, b, aZeroPoint}
		outputs = []*Node{onnxMatMulInteger(a, b, aZeroPoint, nil)}
		return
	}, []any{
		// Expected: (A - 1) @ B
		// A-1 = [[1, 2], [3, 4]]
		// [1*1+2*3, 1*2+2*4] = [7, 10]
		// [3*1+4*3, 3*2+4*4] = [15, 22]
		[][]int32{{7, 10}, {15, 22}},
	}, -1)

	// Test MatMulInteger with only B zero point
	graphtest.RunTestGraphFn(t, "MatMulInteger-b-zero-point-only", func(g *Graph) (inputs, outputs []*Node) {
		a := Const(g, [][]int8{{1, 2}, {3, 4}})
		b := Const(g, [][]int8{{2, 3}, {4, 5}})
		bZeroPoint := Const(g, int8(1))
		inputs = []*Node{a, b}
		outputs = []*Node{onnxMatMulInteger(a, b, nil, bZeroPoint)}
		return
	}, []any{
		// Expected: A @ (B - 1)
		// B-1 = [[1, 2], [3, 4]]
		// [1*1+2*3, 1*2+2*4] = [7, 10]
		// [3*1+4*3, 3*2+4*4] = [15, 22]
		[][]int32{{7, 10}, {15, 22}},
	}, -1)

	// Test 3D batch matrix multiplication
	graphtest.RunTestGraphFn(t, "MatMulInteger-batch", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 2, 3] - batch of 2 matrices
		a := Const(g, [][][]int8{
			{{1, 2, 3}, {4, 5, 6}},
			{{7, 8, 9}, {10, 11, 12}},
		})
		// B: [2, 3, 2] - batch of 2 matrices
		b := Const(g, [][][]int8{
			{{1, 2}, {3, 4}, {5, 6}},
			{{1, 0}, {0, 1}, {1, 0}},
		})
		inputs = []*Node{a, b}
		outputs = []*Node{onnxMatMulInteger(a, b, nil, nil)}
		return
	}, []any{
		// Batch 0: [[1,2,3],[4,5,6]] @ [[1,2],[3,4],[5,6]]
		//   = [[22, 28], [49, 64]]
		// Batch 1: [[7,8,9],[10,11,12]] @ [[1,0],[0,1],[1,0]]
		//   = [[7+0+9, 0+8+0], [10+0+12, 0+11+0]] = [[16, 8], [22, 11]]
		[][][]int32{
			{{22, 28}, {49, 64}},
			{{16, 8}, {22, 11}},
		},
	}, -1)

	// Test MatMulInteger with per-axis (1D) zero point for A
	graphtest.RunTestGraphFn(t, "MatMulInteger-per-axis-a-zero-point", func(g *Graph) (inputs, outputs []*Node) {
		// A: [3, 2] matrix
		a := Const(g, [][]int8{{10, 20}, {30, 40}, {50, 60}})
		// B: [2, 4] matrix
		b := Const(g, [][]int8{{1, 2, 3, 4}, {5, 6, 7, 8}})
		// Per-row zero point for A: [3] (one per row)
		aZeroPoint := Const(g, []int8{5, 10, 15})
		inputs = []*Node{a, b, aZeroPoint}
		outputs = []*Node{onnxMatMulInteger(a, b, aZeroPoint, nil)}
		return
	}, []any{
		// A-aZeroPoint = [[5, 15], [20, 30], [35, 45]]
		// (A-aZeroPoint) @ B:
		// Row 0: [5*1+15*5, 5*2+15*6, 5*3+15*7, 5*4+15*8] = [80, 100, 120, 140]
		// Row 1: [20*1+30*5, 20*2+30*6, 20*3+30*7, 20*4+30*8] = [170, 220, 270, 320]
		// Row 2: [35*1+45*5, 35*2+45*6, 35*3+45*7, 35*4+45*8] = [260, 340, 420, 500]
		[][]int32{{80, 100, 120, 140}, {170, 220, 270, 320}, {260, 340, 420, 500}},
	}, -1)

	// Test MatMulInteger with per-axis (1D) zero point for B
	graphtest.RunTestGraphFn(t, "MatMulInteger-per-axis-b-zero-point", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 3] matrix
		a := Const(g, [][]int8{{1, 2, 3}, {4, 5, 6}})
		// B: [3, 4] matrix
		b := Const(g, [][]int8{{10, 20, 30, 40}, {50, 60, 70, 80}, {90, 100, 110, 120}})
		// Per-column zero point for B: [4] (one per column)
		bZeroPoint := Const(g, []int8{5, 10, 15, 20})
		inputs = []*Node{a, b}
		outputs = []*Node{onnxMatMulInteger(a, b, nil, bZeroPoint)}
		return
	}, []any{
		// B-bZeroPoint = [[5, 10, 15, 20], [45, 50, 55, 60], [85, 90, 95, 100]]
		// A @ (B-bZeroPoint):
		// Row 0: [1*5+2*45+3*85, 1*10+2*50+3*90, 1*15+2*55+3*95, 1*20+2*60+3*100]
		//      = [350, 380, 410, 440]
		// Row 1: [4*5+5*45+6*85, 4*10+5*50+6*90, 4*15+5*55+6*95, 4*20+5*60+6*100]
		//      = [755, 830, 905, 980]
		[][]int32{{350, 380, 410, 440}, {755, 830, 905, 980}},
	}, -1)

	// Test MatMulInteger with both per-axis zero points
	graphtest.RunTestGraphFn(t, "MatMulInteger-per-axis-both-zero-points", func(g *Graph) (inputs, outputs []*Node) {
		// A: [2, 3] matrix
		a := Const(g, [][]int8{{11, 12, 13}, {21, 22, 23}})
		// B: [3, 2] matrix
		b := Const(g, [][]int8{{31, 32}, {41, 42}, {51, 52}})
		// Per-row zero point for A: [2]
		aZeroPoint := Const(g, []int8{10, 20})
		// Per-column zero point for B: [2]
		bZeroPoint := Const(g, []int8{30, 40})
		inputs = []*Node{a, b, aZeroPoint, bZeroPoint}
		outputs = []*Node{onnxMatMulInteger(a, b, aZeroPoint, bZeroPoint)}
		return
	}, []any{
		// A-aZeroPoint = [[1, 2, 3], [1, 2, 3]]
		// B-bZeroPoint = [[1, -8], [11, 2], [21, 12]]
		// (A-aZeroPoint) @ (B-bZeroPoint):
		// Row 0: [1*1+2*11+3*21, 1*(-8)+2*2+3*12] = [86, 32]
		// Row 1: [1*1+2*11+3*21, 1*(-8)+2*2+3*12] = [86, 32]
		[][]int32{{86, 32}, {86, 32}},
	}, -1)
}

////////////////////////////////////////////////////////////////////
//
// Tests for new operators added in this branch
//
////////////////////////////////////////////////////////////////////

func TestLayerNormalization(t *testing.T) {
	// Test basic layer normalization with default axis (-1)
	graphtest.RunTestGraphFn(t, "LayerNormalization-basic", func(g *Graph) (inputs, outputs []*Node) {
		// Input tensor [2, 3]: normalize over last axis (axis=-1, which is axis 1)
		x := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		scale := Const(g, []float32{1.0, 1.0, 1.0})
		bias := Const(g, []float32{0.0, 0.0, 0.0})

		// Create a mock node to pass attributes
		node := &protos.NodeProto{
			OpType: "LayerNormalization",
		}
		inputs = []*Node{x, scale, bias}
		outputs = []*Node{
			convertLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Expected: normalized values with mean=0, variance=1 for each row
		// For [1,2,3]: mean=2, std≈0.8165, normalized ≈ [-1.224, 0, 1.224]
		// For [4,5,6]: mean=5, std≈0.8165, normalized ≈ [-1.224, 0, 1.224]
		[][]float32{{-1.2247, 0.0, 1.2247}, {-1.2247, 0.0, 1.2247}},
	}, 1e-3)

	// Test with scale and bias
	graphtest.RunTestGraphFn(t, "LayerNormalization-scale-bias", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		scale := Const(g, []float32{2.0, 2.0, 2.0})
		bias := Const(g, []float32{1.0, 1.0, 1.0})

		node := &protos.NodeProto{
			OpType: "LayerNormalization",
		}
		inputs = []*Node{x, scale, bias}
		outputs = []*Node{
			convertLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Expected: (normalized * 2) + 1
		[][]float32{{-1.4494, 1.0, 3.4494}, {-1.4494, 1.0, 3.4494}},
	}, 1e-3)

	// Test with custom axis
	graphtest.RunTestGraphFn(t, "LayerNormalization-axis0", func(g *Graph) (inputs, outputs []*Node) {
		// Normalize over axis 0 and axis 1 (from axis 0 to end)
		x := Const(g, [][]float32{{1.0, 4.0}, {2.0, 5.0}, {3.0, 6.0}})
		scale := Const(g, [][]float32{{1.0, 1.0}, {1.0, 1.0}, {1.0, 1.0}})
		bias := Const(g, [][]float32{{0.0, 0.0}, {0.0, 0.0}, {0.0, 0.0}})

		node := &protos.NodeProto{
			OpType: "LayerNormalization",
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: 0},
			},
		}
		inputs = []*Node{x, scale, bias}
		outputs = []*Node{
			convertLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Normalize over all elements (axis 0 to end means entire tensor)
		// Mean = 3.5, values normalized around that
		[][]float32{{-1.4638, 0.2928}, {-0.8783, 0.8783}, {-0.2928, 1.4638}},
	}, 1e-3)

	// Test 3D tensor (common in transformers: batch, sequence, features)
	graphtest.RunTestGraphFn(t, "LayerNormalization-3D", func(g *Graph) (inputs, outputs []*Node) {
		// Shape [2, 2, 3]: batch=2, seq_len=2, features=3
		x := Const(g, [][][]float32{
			{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}},
			{{7.0, 8.0, 9.0}, {10.0, 11.0, 12.0}},
		})
		scale := Const(g, []float32{1.0, 1.0, 1.0})
		bias := Const(g, []float32{0.0, 0.0, 0.0})

		node := &protos.NodeProto{
			OpType: "LayerNormalization",
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: -1}, // Last axis
			},
		}
		inputs = []*Node{x, scale, bias}
		outputs = []*Node{
			convertLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Each feature vector should be normalized independently
		[][][]float32{
			{{-1.2247, 0.0, 1.2247}, {-1.2247, 0.0, 1.2247}},
			{{-1.2247, 0.0, 1.2247}, {-1.2247, 0.0, 1.2247}},
		},
	}, 1e-3)
}

func TestSimplifiedLayerNormalization(t *testing.T) {
	// Test basic SimplifiedLayerNormalization (RMSNorm) with default axis (-1)
	graphtest.RunTestGraphFn(t, "SimplifiedLayerNormalization-basic", func(g *Graph) (inputs, outputs []*Node) {
		// Input tensor [2, 3]: normalize over last axis (axis=-1, which is axis 1)
		x := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		scale := Const(g, []float32{1.0, 1.0, 1.0})

		// Create a mock node to pass attributes
		node := &protos.NodeProto{
			OpType: "SimplifiedLayerNormalization",
		}
		inputs = []*Node{x, scale}
		outputs = []*Node{
			convertSimplifiedLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Expected: X / sqrt(mean(X^2) + epsilon) * scale
		// For [1,2,3]: mean(x^2)=14/3≈4.667, rms=sqrt(4.667)≈2.16, normalized ≈ [0.463, 0.926, 1.389]
		// For [4,5,6]: mean(x^2)=77/3≈25.67, rms=sqrt(25.67)≈5.066, normalized ≈ [0.789, 0.987, 1.184]
		[][]float32{{0.4629, 0.9258, 1.3887}, {0.7895, 0.9869, 1.1843}},
	}, 1e-3)

	// Test with scale
	graphtest.RunTestGraphFn(t, "SimplifiedLayerNormalization-scale", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		scale := Const(g, []float32{2.0, 2.0, 2.0})

		node := &protos.NodeProto{
			OpType: "SimplifiedLayerNormalization",
		}
		inputs = []*Node{x, scale}
		outputs = []*Node{
			convertSimplifiedLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Expected: (X / RMS) * 2
		[][]float32{{0.9258, 1.8516, 2.7775}, {1.5790, 1.9737, 2.3685}},
	}, 1e-3)

	// Test 3D tensor (common in transformers: batch, sequence, features)
	graphtest.RunTestGraphFn(t, "SimplifiedLayerNormalization-3D", func(g *Graph) (inputs, outputs []*Node) {
		// Shape [2, 2, 3]: batch=2, seq_len=2, features=3
		x := Const(g, [][][]float32{
			{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}},
			{{7.0, 8.0, 9.0}, {10.0, 11.0, 12.0}},
		})
		scale := Const(g, []float32{1.0, 1.0, 1.0})

		node := &protos.NodeProto{
			OpType: "SimplifiedLayerNormalization",
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: -1}, // Last axis
			},
		}
		inputs = []*Node{x, scale}
		outputs = []*Node{
			convertSimplifiedLayerNormalization(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Each feature vector should be RMS-normalized independently
		[][][]float32{
			{{0.4629, 0.9258, 1.3887}, {0.7895, 0.9869, 1.1843}},
			{{0.8704, 0.9947, 1.1191}, {0.9071, 0.9978, 1.0885}},
		},
	}, 1e-3)
}

func TestReduceL2(t *testing.T) {
	// Test basic ReduceL2 over all elements
	graphtest.RunTestGraphFn(t, "ReduceL2-all", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{3.0, 4.0}})

		node := &protos.NodeProto{
			OpType: "ReduceL2",
			Attribute: []*protos.AttributeProto{
				{Name: "keepdims", Type: protos.AttributeProto_INT, I: 0},
			},
		}
		inputs = []*Node{x}
		outputs = []*Node{
			convertReduceL2(nil, nil, node, inputs),
		}
		return
	}, []any{
		// sqrt(3^2 + 4^2) = sqrt(9 + 16) = sqrt(25) = 5
		float32(5.0),
	}, 1e-5)

	// Test ReduceL2 over last axis with keepdims
	graphtest.RunTestGraphFn(t, "ReduceL2-axis-keepdims", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{3.0, 4.0}, {5.0, 12.0}})

		node := &protos.NodeProto{
			OpType: "ReduceL2",
			Attribute: []*protos.AttributeProto{
				{Name: "axes", Type: protos.AttributeProto_INTS, Ints: []int64{1}},
				{Name: "keepdims", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		inputs = []*Node{x}
		outputs = []*Node{
			convertReduceL2(nil, nil, node, inputs),
		}
		return
	}, []any{
		// sqrt(3^2 + 4^2) = 5, sqrt(5^2 + 12^2) = 13
		[][]float32{{5.0}, {13.0}},
	}, 1e-5)

	// Test ReduceL2 without keepdims
	graphtest.RunTestGraphFn(t, "ReduceL2-axis-no-keepdims", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{3.0, 4.0}, {5.0, 12.0}})

		node := &protos.NodeProto{
			OpType: "ReduceL2",
			Attribute: []*protos.AttributeProto{
				{Name: "axes", Type: protos.AttributeProto_INTS, Ints: []int64{1}},
				{Name: "keepdims", Type: protos.AttributeProto_INT, I: 0},
			},
		}
		inputs = []*Node{x}
		outputs = []*Node{
			convertReduceL2(nil, nil, node, inputs),
		}
		return
	}, []any{
		// sqrt(3^2 + 4^2) = 5, sqrt(5^2 + 12^2) = 13
		[]float32{5.0, 13.0},
	}, 1e-5)

	// Test ReduceL2 noop_with_empty_axes
	graphtest.RunTestGraphFn(t, "ReduceL2-noop", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{3.0, 4.0}, {5.0, 12.0}})

		node := &protos.NodeProto{
			OpType: "ReduceL2",
			Attribute: []*protos.AttributeProto{
				{Name: "noop_with_empty_axes", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		inputs = []*Node{x}
		outputs = []*Node{
			convertReduceL2(nil, nil, node, inputs),
		}
		return
	}, []any{
		// With noop_with_empty_axes=1 and no axes specified, return input unchanged
		[][]float32{{3.0, 4.0}, {5.0, 12.0}},
	}, 1e-5)
}

func TestRotaryEmbedding(t *testing.T) {
	// Test basic RotaryEmbedding with 4D input (batch, heads, seq, head_size)
	// Input order: [input, position_ids, cos_cache, sin_cache]
	graphtest.RunTestGraphFn(t, "RotaryEmbedding-4D-basic", func(g *Graph) (inputs, outputs []*Node) {
		// Input: (batch=1, heads=1, seq=2, head_size=4)
		// For simplicity, use values where rotation is easy to verify
		x := Const(g, [][][][]float32{{{{1.0, 0.0, 0.0, 0.0}, {0.0, 1.0, 0.0, 0.0}}}})

		// cos_cache and sin_cache: (max_pos=2, head_size/2=2)
		// Position 0: cos=[1, 1], sin=[0, 0] (no rotation)
		// Position 1: cos=[0, 1], sin=[1, 0] (90 degree rotation on first pair)
		cosCache := Const(g, [][]float32{{1.0, 1.0}, {0.0, 1.0}})
		sinCache := Const(g, [][]float32{{0.0, 0.0}, {1.0, 0.0}})

		// Sequential position_ids: [0, 1]
		positionIds := Const(g, [][]int64{{0, 1}})

		node := &protos.NodeProto{
			OpType: "RotaryEmbedding",
		}
		// Inputs: [input, position_ids, cos_cache, sin_cache]
		inputs = []*Node{x, positionIds, cosCache, sinCache}
		outputs = []*Node{
			convertRotaryEmbedding(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Position 0: x=[1,0,0,0], cos=[1,1], sin=[0,0]
		//   x1=[1,0], x2=[0,0]
		//   real = cos*x1 - sin*x2 = [1,1]*[1,0] - [0,0]*[0,0] = [1,0]
		//   imag = sin*x1 + cos*x2 = [0,0]*[1,0] + [1,1]*[0,0] = [0,0]
		//   output = [1,0,0,0]
		// Position 1: x=[0,1,0,0], cos=[0,1], sin=[1,0]
		//   x1=[0,1], x2=[0,0]
		//   real = cos*x1 - sin*x2 = [0,1]*[0,1] - [1,0]*[0,0] = [0,1]
		//   imag = sin*x1 + cos*x2 = [1,0]*[0,1] + [0,1]*[0,0] = [0,0]
		//   output = [0,1,0,0]
		[][][][]float32{{{{1.0, 0.0, 0.0, 0.0}, {0.0, 1.0, 0.0, 0.0}}}},
	}, 1e-5)

	// Test RotaryEmbedding with position_ids
	graphtest.RunTestGraphFn(t, "RotaryEmbedding-with-position-ids", func(g *Graph) (inputs, outputs []*Node) {
		// Input: (batch=1, heads=1, seq=2, head_size=4)
		x := Const(g, [][][][]float32{{{{1.0, 2.0, 3.0, 4.0}, {5.0, 6.0, 7.0, 8.0}}}})

		// cos_cache and sin_cache: (max_pos=3, head_size/2=2)
		cosCache := Const(g, [][]float32{{1.0, 1.0}, {0.5, 0.5}, {0.0, 0.0}})
		sinCache := Const(g, [][]float32{{0.0, 0.0}, {0.5, 0.5}, {1.0, 1.0}})

		// position_ids: use positions [0, 2] instead of [0, 1]
		positionIds := Const(g, [][]int64{{0, 2}})

		node := &protos.NodeProto{
			OpType: "RotaryEmbedding",
		}
		// Inputs: [input, position_ids, cos_cache, sin_cache]
		inputs = []*Node{x, positionIds, cosCache, sinCache}
		outputs = []*Node{
			convertRotaryEmbedding(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Position 0: cos=[1,1], sin=[0,0]
		//   x1=[1,2], x2=[3,4]
		//   real = [1,2]*[1,1] - [3,4]*[0,0] = [1,2]
		//   imag = [1,2]*[0,0] + [3,4]*[1,1] = [3,4]
		//   output = [1,2,3,4]
		// Position 2: cos=[0,0], sin=[1,1]
		//   x1=[5,6], x2=[7,8]
		//   real = [5,6]*[0,0] - [7,8]*[1,1] = [-7,-8]
		//   imag = [5,6]*[1,1] + [7,8]*[0,0] = [5,6]
		//   output = [-7,-8,5,6]
		[][][][]float32{{{{1.0, 2.0, 3.0, 4.0}, {-7.0, -8.0, 5.0, 6.0}}}},
	}, 1e-5)

	// Test RotaryEmbedding with interleaved mode
	graphtest.RunTestGraphFn(t, "RotaryEmbedding-interleaved", func(g *Graph) (inputs, outputs []*Node) {
		// Input: (batch=1, heads=1, seq=1, head_size=4)
		// With interleaved, x1=[x[0], x[2]] and x2=[x[1], x[3]]
		x := Const(g, [][][][]float32{{{{1.0, 2.0, 3.0, 4.0}}}})

		// cos_cache and sin_cache: (max_pos=1, head_size/2=2)
		cosCache := Const(g, [][]float32{{0.5, 0.5}})
		sinCache := Const(g, [][]float32{{0.5, 0.5}})

		// Sequential position_ids: [0]
		positionIds := Const(g, [][]int64{{0}})

		node := &protos.NodeProto{
			OpType: "RotaryEmbedding",
			Attribute: []*protos.AttributeProto{
				{Name: "interleaved", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		// Inputs: [input, position_ids, cos_cache, sin_cache]
		inputs = []*Node{x, positionIds, cosCache, sinCache}
		outputs = []*Node{
			convertRotaryEmbedding(nil, nil, node, inputs),
		}
		return
	}, []any{
		// x1=[1,3] (even indices), x2=[2,4] (odd indices)
		// cos=[0.5,0.5], sin=[0.5,0.5]
		// real = [1,3]*[0.5,0.5] - [2,4]*[0.5,0.5] = [0.5,1.5] - [1,2] = [-0.5,-0.5]
		// imag = [1,3]*[0.5,0.5] + [2,4]*[0.5,0.5] = [0.5,1.5] + [1,2] = [1.5,3.5]
		// interleaved output = [real[0],imag[0],real[1],imag[1]] = [-0.5,1.5,-0.5,3.5]
		[][][][]float32{{{{-0.5, 1.5, -0.5, 3.5}}}},
	}, 1e-5)

	// Test RotaryEmbedding with 3D input and num_heads derived from cos_cache shape.
	// This exercises the code path where num_heads=0 (default) and is inferred as
	// hidden_size / (cos_cache_last_dim * 2).
	graphtest.RunTestGraphFn(t, "RotaryEmbedding-3D-derive-num-heads", func(g *Graph) (inputs, outputs []*Node) {
		// Input: 3D (batch=1, seq=2, hidden_size=8)
		// cos_cache last dim = 2, so head_size = 2*2 = 4, num_heads = 8/4 = 2
		x := Const(g, [][][]float32{{{1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}, {0.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0}}})

		// cos_cache and sin_cache: (max_pos=2, rotary_dim/2=2)
		// Position 0: cos=[1, 1], sin=[0, 0] (no rotation)
		// Position 1: cos=[0, 1], sin=[1, 0]
		cosCache := Const(g, [][]float32{{1.0, 1.0}, {0.0, 1.0}})
		sinCache := Const(g, [][]float32{{0.0, 0.0}, {1.0, 0.0}})

		positionIds := Const(g, [][]int64{{0, 1}})

		node := &protos.NodeProto{
			OpType: "RotaryEmbedding",
			// num_heads is intentionally not set (defaults to 0)
		}
		inputs = []*Node{x, positionIds, cosCache, sinCache}
		outputs = []*Node{
			convertRotaryEmbedding(nil, nil, node, inputs),
		}
		return
	}, []any{
		// num_heads derived as 8 / (2*2) = 2, head_size = 4
		// Reshaped to (1, 2, 2, 4) then transposed to (1, 2, 2, 4) [batch, heads, seq, head_size]
		//
		// Head 0, Position 0: x=[1,0,0,0], cos=[1,1], sin=[0,0]
		//   x1=[1,0], x2=[0,0], real=[1,0], imag=[0,0] -> [1,0,0,0]
		// Head 0, Position 1: x=[0,0,0,0], cos=[0,1], sin=[1,0]
		//   x1=[0,0], x2=[0,0], real=[0,0], imag=[0,0] -> [0,0,0,0]
		// Head 1, Position 0: x=[0,0,0,0], cos=[1,1], sin=[0,0]
		//   x1=[0,0], x2=[0,0], real=[0,0], imag=[0,0] -> [0,0,0,0]
		// Head 1, Position 1: x=[1,0,0,0], cos=[0,1], sin=[1,0]
		//   x1=[1,0], x2=[0,0], real=[0,0], imag=[1,0] -> [0,0,1,0]
		//
		// Transposed back to (1, 2, 2, 4) [batch, seq, heads, head_size]
		// Then reshaped to (1, 2, 8):
		//   Seq 0: head0=[1,0,0,0] head1=[0,0,0,0] -> [1,0,0,0,0,0,0,0]
		//   Seq 1: head0=[0,0,0,0] head1=[0,0,1,0] -> [0,0,0,0,0,0,1,0]
		[][][]float32{{{1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}, {0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0, 0.0}}},
	}, 1e-5)
}

func TestMultiHeadAttention(t *testing.T) {
	// Test basic multi-head attention with 4D input (batch, heads, seq, head_size)
	graphtest.RunTestGraphFn(t, "MultiHeadAttention-4D-basic", func(g *Graph) (inputs, outputs []*Node) {
		// Simple case: batch=1, heads=1, seq=2, head_size=2
		// Q, K, V all same shape for self-attention
		q := Const(g, [][][][]float32{{{{1.0, 0.0}, {0.0, 1.0}}}})
		k := Const(g, [][][][]float32{{{{1.0, 0.0}, {0.0, 1.0}}}})
		v := Const(g, [][][][]float32{{{{1.0, 2.0}, {3.0, 4.0}}}})

		node := &protos.NodeProto{
			OpType: "MultiHeadAttention",
		}
		inputs = []*Node{q, k, v}
		outputs = []*Node{
			convertMultiHeadAttention(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Q @ K^T (scaled by 1/sqrt(2) ≈ 0.707):
		//   [[1,0],[0,1]] @ [[1,0],[0,1]]^T = [[1,0],[0,1]]
		//   scaled: [[0.707,0],[0,0.707]]
		// softmax over last dim:
		//   row 0: softmax([0.707, 0]) ≈ [0.66, 0.34]
		//   row 1: softmax([0, 0.707]) ≈ [0.34, 0.66]
		// output = attn @ V
		[][][][]float32{{{{1.6605, 2.6605}, {2.3395, 3.3395}}}},
	}, 1e-3)

	// Test with 3D input (batch, seq, hidden) and num_heads attribute
	graphtest.RunTestGraphFn(t, "MultiHeadAttention-3D", func(g *Graph) (inputs, outputs []*Node) {
		// batch=1, seq=2, hidden=4, num_heads=2, head_size=2
		q := Const(g, [][][]float32{{{1.0, 0.0, 0.0, 1.0}, {0.0, 1.0, 1.0, 0.0}}})
		k := Const(g, [][][]float32{{{1.0, 0.0, 0.0, 1.0}, {0.0, 1.0, 1.0, 0.0}}})
		v := Const(g, [][][]float32{{{1.0, 2.0, 3.0, 4.0}, {5.0, 6.0, 7.0, 8.0}}})

		node := &protos.NodeProto{
			OpType: "MultiHeadAttention",
			Attribute: []*protos.AttributeProto{
				{Name: "num_heads", Type: protos.AttributeProto_INT, I: 2},
			},
		}
		inputs = []*Node{q, k, v}
		outputs = []*Node{
			convertMultiHeadAttention(nil, nil, node, inputs),
		}
		return
	}, []any{
		// Output shape should be (batch=1, seq=2, hidden=4)
		// With 2 heads, each processes half of the hidden dimension
		[][][]float32{{{2.3210, 3.3210, 4.3210, 5.3210}, {3.6790, 4.6790, 5.6790, 6.6790}}},
	}, 1e-3)

	// Test with attention mask
	graphtest.RunTestGraphFn(t, "MultiHeadAttention-with-mask", func(g *Graph) (inputs, outputs []*Node) {
		// batch=1, heads=1, seq=2, head_size=2
		q := Const(g, [][][][]float32{{{{1.0, 0.0}, {0.0, 1.0}}}})
		k := Const(g, [][][][]float32{{{{1.0, 0.0}, {0.0, 1.0}}}})
		v := Const(g, [][][][]float32{{{{1.0, 2.0}, {3.0, 4.0}}}})

		// Attention mask: mask out second position for first query
		// Shape (1, 1, 2, 2): large negative masks out attention
		mask := Const(g, [][][][]float32{{{{0.0, -10000.0}, {0.0, 0.0}}}})

		node := &protos.NodeProto{
			OpType: "MultiHeadAttention",
		}
		inputs = []*Node{q, k, v, mask}
		outputs = []*Node{
			convertMultiHeadAttention(nil, nil, node, inputs),
		}
		return
	}, []any{
		// With mask, first query can only attend to first key
		// softmax([0.707, -inf]) ≈ [1.0, 0.0]
		// output row 0 = [1, 0] @ V = [1, 2]
		// Second query unchanged
		[][][][]float32{{{{1.0, 2.0}, {2.3395, 3.3395}}}},
	}, 1e-3)
}

func TestGroupQueryAttention(t *testing.T) {
	// Basic GQA: batch=1, seq=2, num_heads=2, kv_num_heads=1, head_size=2
	// Same Q/K/V for self-attention, no past cache.
	graphtest.RunTestGraphFn(t, "GQA-basic", func(g *Graph) (inputs, outputs []*Node) {
		// Q: (1, 2, 4) = batch=1, seq=2, num_heads=2 * head_size=2
		q := Const(g, [][][]float32{{{1, 0, 0, 1}, {0, 1, 1, 0}}})
		// K, V: (1, 2, 2) = batch=1, seq=2, kv_num_heads=1 * head_size=2
		k := Const(g, [][][]float32{{{1, 0}, {0, 1}}})
		v := Const(g, [][][]float32{{{1, 2}, {3, 4}}})

		node := &protos.NodeProto{
			OpType: "GroupQueryAttention",
			Output: []string{"output", "present_key", "present_value"},
			Attribute: []*protos.AttributeProto{
				{Name: "num_heads", Type: protos.AttributeProto_INT, I: 2},
				{Name: "kv_num_heads", Type: protos.AttributeProto_INT, I: 1},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{q, k, v}
		result := convertGroupQueryAttention(nil, convertedOutputs, node, inputs)

		outputs = []*Node{
			result,
			convertedOutputs["present_key"],
			convertedOutputs["present_value"],
		}
		return
	}, []any{
		// With causal mask: token 0 attends to [0], token 1 attends to [0,1].
		// KV head is replicated to both query heads.
		[][][]float32{{{1.0, 2.0, 1.0, 2.0}, {2.3395, 3.3395, 1.6605, 2.6605}}},
		// present_key: (1, 1, 2, 2) - same as input K reshaped
		[][][][]float32{{{{1, 0}, {0, 1}}}},
		// present_value: (1, 1, 2, 2)
		[][][][]float32{{{{1, 2}, {3, 4}}}},
	}, 1e-3)

	// GQA with sliding window: only attend within window of 1.
	graphtest.RunTestGraphFn(t, "GQA-sliding-window", func(g *Graph) (inputs, outputs []*Node) {
		// batch=1, seq=3, num_heads=1, kv_num_heads=1, head_size=2
		q := Const(g, [][][]float32{{{1, 0}, {0, 1}, {1, 1}}})
		k := Const(g, [][][]float32{{{1, 0}, {0, 1}, {1, 1}}})
		v := Const(g, [][][]float32{{{1, 0}, {0, 1}, {0.5, 0.5}}})

		node := &protos.NodeProto{
			OpType: "GroupQueryAttention",
			Output: []string{"output", "present_key", "present_value"},
			Attribute: []*protos.AttributeProto{
				{Name: "num_heads", Type: protos.AttributeProto_INT, I: 1},
				{Name: "kv_num_heads", Type: protos.AttributeProto_INT, I: 1},
				{Name: "local_window_size", Type: protos.AttributeProto_INT, I: 1},
				{Name: "scale", Type: protos.AttributeProto_FLOAT, F: 1.0},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{q, k, v}
		result := convertGroupQueryAttention(nil, convertedOutputs, node, inputs)

		outputs = []*Node{result}
		return
	}, []any{
		// With window=1, each token only attends to itself (causal + window).
		// Token 0: attends to [0] only -> v[0] = [1, 0]
		// Token 1: attends to [1] only -> v[1] = [0, 1]
		// Token 2: attends to [2] only -> v[2] = [0.5, 0.5]
		[][][]float32{{{1, 0}, {0, 1}, {0.5, 0.5}}},
	}, 1e-3)

	// GQA with KV cache: past_key/past_value prepended.
	graphtest.RunTestGraphFn(t, "GQA-kv-cache", func(g *Graph) (inputs, outputs []*Node) {
		// batch=1, current seq=1, num_heads=1, kv_num_heads=1, head_size=2
		q := Const(g, [][][]float32{{{0, 1}}})         // new query token
		k := Const(g, [][][]float32{{{1, 1}}})         // new key token
		v := Const(g, [][][]float32{{{0.5, 0.5}}})     // new value token
		pastK := Const(g, [][][][]float32{{{{1, 0}}}}) // 1 cached key
		pastV := Const(g, [][][][]float32{{{{1, 0}}}}) // 1 cached value

		node := &protos.NodeProto{
			OpType: "GroupQueryAttention",
			Output: []string{"output", "present_key", "present_value"},
			Attribute: []*protos.AttributeProto{
				{Name: "num_heads", Type: protos.AttributeProto_INT, I: 1},
				{Name: "kv_num_heads", Type: protos.AttributeProto_INT, I: 1},
				{Name: "scale", Type: protos.AttributeProto_FLOAT, F: 1.0},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{q, k, v, pastK, pastV}
		result := convertGroupQueryAttention(nil, convertedOutputs, node, inputs)

		outputs = []*Node{
			result,
			convertedOutputs["present_key"],
			convertedOutputs["present_value"],
		}
		return
	}, []any{
		// Query [0,1] attends to keys [[1,0],[1,1]] (past + current).
		// scores = [0*1+1*0, 0*1+1*1] = [0, 1], softmax = [0.2689, 0.7311]
		// output = 0.2689*[1,0] + 0.7311*[0.5,0.5] = [0.6345, 0.3655]
		[][][]float32{{{0.6345, 0.3655}}},
		// present_key: past [1,0] + current [1,1]
		[][][][]float32{{{{1, 0}, {1, 1}}}},
		// present_value: past [1,0] + current [0.5,0.5]
		[][][][]float32{{{{1, 0}, {0.5, 0.5}}}},
	}, 1e-3)
}

func TestSplit(t *testing.T) {
	// Test equal splits
	graphtest.RunTestGraphFn(t, "Split-equal", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{1.0, 2.0, 3.0, 4.0}, {5.0, 6.0, 7.0, 8.0}})

		node := &protos.NodeProto{
			OpType: "Split",
			Output: []string{"out1", "out2"},
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: 1},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{x}
		convertSplit(nil, convertedOutputs, node, inputs)

		outputs = []*Node{
			convertedOutputs["out1"],
			convertedOutputs["out2"],
		}
		return
	}, []any{
		[][]float32{{1.0, 2.0}, {5.0, 6.0}},
		[][]float32{{3.0, 4.0}, {7.0, 8.0}},
	}, -1)

	// Test Split on different axis (axis=1)
	graphtest.RunTestGraphFn(t, "Split-axis1", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}, {7.0, 8.0, 9.0, 10.0, 11.0, 12.0}})

		node := &protos.NodeProto{
			OpType: "Split",
			Output: []string{"out1", "out2", "out3"},
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: 1},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{x}
		convertSplit(nil, convertedOutputs, node, inputs)

		outputs = []*Node{
			convertedOutputs["out1"],
			convertedOutputs["out2"],
			convertedOutputs["out3"],
		}
		return
	}, []any{
		[][]float32{{1.0, 2.0}, {7.0, 8.0}},
		[][]float32{{3.0, 4.0}, {9.0, 10.0}},
		[][]float32{{5.0, 6.0}, {11.0, 12.0}},
	}, -1)

	// Test 3-way equal split on axis 0
	graphtest.RunTestGraphFn(t, "Split-3way", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{1.0, 2.0},
			{3.0, 4.0},
			{5.0, 6.0},
			{7.0, 8.0},
			{9.0, 10.0},
			{11.0, 12.0},
		})

		node := &protos.NodeProto{
			OpType: "Split",
			Output: []string{"out1", "out2", "out3"},
			Attribute: []*protos.AttributeProto{
				{Name: "axis", Type: protos.AttributeProto_INT, I: 0},
			},
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{x}
		convertSplit(nil, convertedOutputs, node, inputs)

		outputs = []*Node{
			convertedOutputs["out1"],
			convertedOutputs["out2"],
			convertedOutputs["out3"],
		}
		return
	}, []any{
		[][]float32{{1.0, 2.0}, {3.0, 4.0}},
		[][]float32{{5.0, 6.0}, {7.0, 8.0}},
		[][]float32{{9.0, 10.0}, {11.0, 12.0}},
	}, -1)
}

func TestIf(t *testing.T) {
	// Test basic If with scalar condition - true case
	graphtest.RunTestGraphFn(t, "If-true-branch", func(g *Graph) (inputs, outputs []*Node) {
		cond := Const(g, true)

		// Create simple then/else branches
		thenGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2, 2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{1.0, 2.0, 3.0, 4.0},
							},
						},
					},
				},
			},
		}

		elseGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2, 2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{10.0, 20.0, 30.0, 40.0},
							},
						},
					},
				},
			},
		}

		node := &protos.NodeProto{
			OpType: "If",
			Input:  []string{"cond"},
			Output: []string{"output"},
			Attribute: []*protos.AttributeProto{
				{Name: "then_branch", Type: protos.AttributeProto_GRAPH, G: thenGraph},
				{Name: "else_branch", Type: protos.AttributeProto_GRAPH, G: elseGraph},
			},
		}

		model := &Model{
			VariableNameToValue: make(map[string]*protos.TensorProto),
			NodeOutputToNode:    make(map[string]*protos.NodeProto),
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{cond}
		result := convertIf(nil, model, convertedOutputs, node, inputs)
		outputs = []*Node{result}
		return
	}, []any{
		[][]float32{{1.0, 2.0}, {3.0, 4.0}},
	}, -1)

	// Test basic If with scalar condition - false case
	graphtest.RunTestGraphFn(t, "If-false-branch", func(g *Graph) (inputs, outputs []*Node) {
		cond := Const(g, false)

		thenGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2, 2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{1.0, 2.0, 3.0, 4.0},
							},
						},
					},
				},
			},
		}

		elseGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2, 2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{10.0, 20.0, 30.0, 40.0},
							},
						},
					},
				},
			},
		}

		node := &protos.NodeProto{
			OpType: "If",
			Input:  []string{"cond"},
			Output: []string{"output"},
			Attribute: []*protos.AttributeProto{
				{Name: "then_branch", Type: protos.AttributeProto_GRAPH, G: thenGraph},
				{Name: "else_branch", Type: protos.AttributeProto_GRAPH, G: elseGraph},
			},
		}

		model := &Model{
			VariableNameToValue: make(map[string]*protos.TensorProto),
			NodeOutputToNode:    make(map[string]*protos.NodeProto),
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{cond}
		result := convertIf(nil, model, convertedOutputs, node, inputs)
		outputs = []*Node{result}
		return
	}, []any{
		[][]float32{{10.0, 20.0}, {30.0, 40.0}},
	}, -1)

	// Test If with multiple outputs
	graphtest.RunTestGraphFn(t, "If-multiple-outputs", func(g *Graph) (inputs, outputs []*Node) {
		cond := Const(g, true)

		thenGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result1"}, {Name: "result2"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result1"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{1.0, 2.0},
							},
						},
					},
				},
				{
					OpType: "Constant",
					Output: []string{"result2"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{3.0, 4.0},
							},
						},
					},
				},
			},
		}

		elseGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result1"}, {Name: "result2"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Constant",
					Output: []string{"result1"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{10.0, 20.0},
							},
						},
					},
				},
				{
					OpType: "Constant",
					Output: []string{"result2"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{30.0, 40.0},
							},
						},
					},
				},
			},
		}

		node := &protos.NodeProto{
			OpType: "If",
			Input:  []string{"cond"},
			Output: []string{"output1", "output2"},
			Attribute: []*protos.AttributeProto{
				{Name: "then_branch", Type: protos.AttributeProto_GRAPH, G: thenGraph},
				{Name: "else_branch", Type: protos.AttributeProto_GRAPH, G: elseGraph},
			},
		}

		model := &Model{
			VariableNameToValue: make(map[string]*protos.TensorProto),
			NodeOutputToNode:    make(map[string]*protos.NodeProto),
		}

		convertedOutputs := make(map[string]*Node)
		inputs = []*Node{cond}
		convertIf(nil, model, convertedOutputs, node, inputs)

		outputs = []*Node{
			convertedOutputs["output1"],
			convertedOutputs["output2"],
		}
		return
	}, []any{
		[]float32{1.0, 2.0},
		[]float32{3.0, 4.0},
	}, -1)

	// Test If with sub-graph that references parent outputs
	graphtest.RunTestGraphFn(t, "If-subgraph-parent-reference", func(g *Graph) (inputs, outputs []*Node) {
		cond := Const(g, true)
		parentValue := Const(g, []float32{100.0, 200.0})

		// Then branch: Add parent value
		thenGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Add",
					Input:  []string{"parent_val", "const_val"},
					Output: []string{"result"},
				},
				{
					OpType: "Constant",
					Output: []string{"const_val"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{1.0, 2.0},
							},
						},
					},
				},
			},
		}

		// Else branch: Subtract from parent value
		elseGraph := &protos.GraphProto{
			Output: []*protos.ValueInfoProto{{Name: "result"}},
			Node: []*protos.NodeProto{
				{
					OpType: "Sub",
					Input:  []string{"parent_val", "const_val"},
					Output: []string{"result"},
				},
				{
					OpType: "Constant",
					Output: []string{"const_val"},
					Attribute: []*protos.AttributeProto{
						{
							Name: "value",
							Type: protos.AttributeProto_TENSOR,
							T: &protos.TensorProto{
								Dims:      []int64{2},
								DataType:  int32(protos.TensorProto_FLOAT),
								FloatData: []float32{10.0, 20.0},
							},
						},
					},
				},
			},
		}

		node := &protos.NodeProto{
			OpType: "If",
			Input:  []string{"cond"},
			Output: []string{"output"},
			Attribute: []*protos.AttributeProto{
				{Name: "then_branch", Type: protos.AttributeProto_GRAPH, G: thenGraph},
				{Name: "else_branch", Type: protos.AttributeProto_GRAPH, G: elseGraph},
			},
		}

		model := &Model{
			VariableNameToValue: make(map[string]*protos.TensorProto),
			NodeOutputToNode:    make(map[string]*protos.NodeProto),
		}

		convertedOutputs := make(map[string]*Node)
		convertedOutputs["parent_val"] = parentValue
		inputs = []*Node{cond}
		result := convertIf(nil, model, convertedOutputs, node, inputs)
		outputs = []*Node{result}
		return
	}, []any{
		// Then branch should execute: 100+1=101, 200+2=202
		[]float32{101.0, 202.0},
	}, -1)
}

func TestTopK(t *testing.T) {
	// Test TopK with largest=true (default)
	graphtest.RunTestGraphFn(t, "TopK-largest", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{3, 1, 4, 1, 5, 9, 2, 6})
		values, indices := TopK(x, 3, 0) // Get top 3 largest
		inputs = []*Node{x}
		outputs = []*Node{values, ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[]float32{9, 6, 5}, // Top 3 values
		[]int32{5, 7, 4},   // Their indices
	}, -1)

	// Test TopK with 2D tensor
	graphtest.RunTestGraphFn(t, "TopK-2D", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{3, 1, 4},
			{1, 5, 9},
		})
		values, indices := TopK(x, 2, 1) // Top 2 along axis 1
		inputs = []*Node{x}
		outputs = []*Node{values, ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[][]float32{{4, 3}, {9, 5}}, // Top 2 values per row
		[][]int32{{2, 0}, {2, 1}},   // Their indices
	}, -1)

	// Test BottomK (smallest values)
	graphtest.RunTestGraphFn(t, "BottomK", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{3, 1, 4, 1, 5, 9, 2, 6})
		values, indices := BottomK(x, 3, 0) // Get bottom 3 smallest
		inputs = []*Node{x}
		outputs = []*Node{values, ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[]float32{1, 1, 2}, // Bottom 3 values
		[]int32{1, 3, 6},   // Their indices
	}, -1)
}

func TestArgMax(t *testing.T) {
	// Test ArgMax along axis 0
	graphtest.RunTestGraphFn(t, "ArgMax-axis0", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
			{7, 8, 9},
		})
		// ArgMax along axis 0, keepdims=true
		_, indices := TopK(x, 1, 0)
		inputs = []*Node{x}
		outputs = []*Node{ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[][]int32{{2, 2, 2}}, // Max values are in row 2
	}, -1)

	// Test ArgMax along axis 1
	graphtest.RunTestGraphFn(t, "ArgMax-axis1", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{3, 1, 4},
			{1, 5, 9},
			{2, 6, 5},
		})
		_, indices := TopK(x, 1, 1)
		inputs = []*Node{x}
		outputs = []*Node{ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[][]int32{{2}, {2}, {1}}, // Index of max in each row
	}, -1)

	// Test ArgMax with keepdims=false (squeeze)
	graphtest.RunTestGraphFn(t, "ArgMax-no-keepdims", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{3, 1, 4},
			{1, 5, 9},
		})
		_, indices := TopK(x, 1, 1)
		squeezed := Squeeze(indices, 1)
		inputs = []*Node{x}
		outputs = []*Node{ConvertDType(squeezed, dtypes.Int32)}
		return
	}, []any{
		[]int32{2, 2}, // Indices without keepdims
	}, -1)
}

func TestArgMin(t *testing.T) {
	// Test ArgMin along axis 1
	graphtest.RunTestGraphFn(t, "ArgMin-axis1", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{
			{3, 1, 4},
			{1, 5, 9},
			{2, 6, 5},
		})
		_, indices := BottomK(x, 1, 1)
		inputs = []*Node{x}
		outputs = []*Node{ConvertDType(indices, dtypes.Int32)}
		return
	}, []any{
		[][]int32{{1}, {0}, {0}}, // Index of min in each row
	}, -1)
}

func TestComputeNonZero(t *testing.T) {
	// Test 1D tensor
	t.Run("1D", func(t *testing.T) {
		input := tensors.FromValue([]float32{0, 1, 0, 2, 3})
		result := computeNonZero(input)
		expected := [][]int64{{1, 3, 4}}
		assert.Equal(t, expected, result.Value())
	})

	// Test 2D tensor
	t.Run("2D", func(t *testing.T) {
		input := tensors.FromValue([][]float32{
			{1, 0},
			{0, 1},
		})
		result := computeNonZero(input)
		// Non-zero elements at (0,0) and (1,1)
		expected := [][]int64{
			{0, 1}, // row indices
			{0, 1}, // col indices
		}
		assert.Equal(t, expected, result.Value())
	})

	// Test all zeros
	t.Run("AllZeros", func(t *testing.T) {
		input := tensors.FromValue([]float32{0, 0, 0})
		result := computeNonZero(input)
		assert.Equal(t, []int{1, 0}, result.Shape().Dimensions)
	})

	// Test boolean tensor
	t.Run("Bool", func(t *testing.T) {
		input := tensors.FromValue([]bool{false, true, true, false})
		result := computeNonZero(input)
		expected := [][]int64{{1, 2}}
		assert.Equal(t, expected, result.Value())
	})

	// Test int tensor
	t.Run("Int", func(t *testing.T) {
		input := tensors.FromValue([]int32{0, -1, 2, 0, 3})
		result := computeNonZero(input)
		expected := [][]int64{{1, 2, 4}}
		assert.Equal(t, expected, result.Value())
	})
}

func TestReduceOperations(t *testing.T) {
	// Test ReduceMax
	graphtest.RunTestGraphFn(t, "ReduceMax-axis0", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceMax(operand, 0)}
		return
	}, []any{
		[]float32{4, 5, 6},
	}, -1)

	graphtest.RunTestGraphFn(t, "ReduceMax-axis1", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceMax(operand, 1)}
		return
	}, []any{
		[]float32{3, 6},
	}, -1)

	graphtest.RunTestGraphFn(t, "ReduceMax-keepdims", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceAndKeep(operand, ReduceMax, 0)}
		return
	}, []any{
		[][]float32{{4, 5, 6}},
	}, -1)

	// Test ReduceMin
	graphtest.RunTestGraphFn(t, "ReduceMin-axis0", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceMin(operand, 0)}
		return
	}, []any{
		[]float32{1, 2, 3},
	}, -1)

	// Test ReduceSum
	graphtest.RunTestGraphFn(t, "ReduceSum-axis0", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceSum(operand, 0)}
		return
	}, []any{
		[]float32{5, 7, 9},
	}, -1)

	graphtest.RunTestGraphFn(t, "ReduceSum-all", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceAllSum(operand)}
		return
	}, []any{
		float32(21),
	}, -1)

	// Test ReduceMultiply (ReduceProd)
	graphtest.RunTestGraphFn(t, "ReduceProd-axis0", func(g *Graph) (inputs, outputs []*Node) {
		operand := Const(g, [][]float32{
			{1, 2, 3},
			{4, 5, 6},
		})
		inputs = []*Node{operand}
		outputs = []*Node{ReduceMultiply(operand, 0)}
		return
	}, []any{
		[]float32{4, 10, 18},
	}, -1)
}

////////////////////////////////////////////////////////////////////
//
// Tests for dtype promotion
//
////////////////////////////////////////////////////////////////////

// createTestModelWithDTypePromoConfig creates a minimal Model for testing with the specified dtype promotion config.
func createTestModelWithDTypePromoConfig(allowPromotion, prioritizeFloat16 bool) *Model {
	m := &Model{}
	if allowPromotion {
		m.allowDTypePromotion = true
	}
	if prioritizeFloat16 {
		m.prioritizeFloat16 = true
	}
	return m
}

func TestPromoteToCommonDTypeStrictMode(t *testing.T) {
	// Test that strict mode (default) panics on dtype mismatch
	backend, err := gobackend.New("")
	require.NoError(t, err)
	g := NewGraph(backend, "StrictModePanic")

	lhs := Const(g, []float32{1.0, 2.0, 3.0})
	lhs = ConvertDType(lhs, dtypes.Float16)
	rhs := Const(g, []float32{4.0, 5.0, 6.0})

	// Strict mode config (default)
	m := createTestModelWithDTypePromoConfig(false, false)
	require.Panics(t, func() {
		_, _ = m.checkOrPromoteDTypes(lhs, rhs)
	}, "expected panic when AllowPromotion=false and dtypes mismatch")
}

func TestPromoteToCommonDTypeSameDType(t *testing.T) {
	// Test that same dtype works in strict mode (no promotion needed)
	graphtest.RunTestGraphFn(t, "PromoteDType-SameDType", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{1.0, 2.0, 3.0})
		rhs := Const(g, []float32{4.0, 5.0, 6.0})
		inputs = []*Node{lhs, rhs}

		// Strict mode - should work because dtypes match
		m := createTestModelWithDTypePromoConfig(false, false)
		result := m.convertBinaryOp(Add, lhs, rhs)
		outputs = []*Node{result}
		return
	}, []any{
		[]float32{5.0, 7.0, 9.0},
	}, -1)
}

func TestPromoteToCommonDTypeWithPromotion(t *testing.T) {
	// Test Float16 + Float32 -> Float16 with PrioritizeFloat16
	graphtest.RunTestGraphFn(t, "PromoteDType-Float16-Float32-PrioritizeFP16", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{1.0, 2.0, 3.0})
		lhs = ConvertDType(lhs, dtypes.Float16)
		rhs := Const(g, []float32{4.0, 5.0, 6.0})
		inputs = []*Node{lhs, rhs}

		// Use model with PrioritizeFloat16
		m := createTestModelWithDTypePromoConfig(true, true)
		result := m.convertBinaryOp(Add, lhs, rhs)
		// Verify the result dtype is Float16
		outputs = []*Node{
			ConvertDType(result, dtypes.Float32),
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[]float32{5.0, 7.0, 9.0},
		int64(dtypes.Float16),
	}, 1e-2)

	// Test Float16 + Float32 -> Float32 without PrioritizeFloat16
	graphtest.RunTestGraphFn(t, "PromoteDType-Float16-Float32-StandardPromotion", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{1.0, 2.0, 3.0})
		lhs = ConvertDType(lhs, dtypes.Float16)
		rhs := Const(g, []float32{4.0, 5.0, 6.0})
		inputs = []*Node{lhs, rhs}

		// Use model without PrioritizeFloat16 - should promote to Float32
		m := createTestModelWithDTypePromoConfig(true, false)
		result := m.convertBinaryOp(Add, lhs, rhs)
		outputs = []*Node{
			result,
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[]float32{5.0, 7.0, 9.0},
		int64(dtypes.Float32),
	}, -1)

	// Test Float32 + Float64 -> Float64 (standard promotion)
	graphtest.RunTestGraphFn(t, "PromoteDType-Float32-Float64", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{1.0, 2.0, 3.0})
		rhs := Const(g, []float64{4.0, 5.0, 6.0})
		inputs = []*Node{lhs, rhs}

		m := createTestModelWithDTypePromoConfig(true, false)
		result := m.convertBinaryOp(Add, lhs, rhs)
		outputs = []*Node{
			result,
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[]float64{5.0, 7.0, 9.0},
		int64(dtypes.Float64),
	}, -1)

	// Test Int32 + Float32 -> Float32 (int to float promotion)
	graphtest.RunTestGraphFn(t, "PromoteDType-Int32-Float32", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []int32{1, 2, 3})
		rhs := Const(g, []float32{4.5, 5.5, 6.5})
		inputs = []*Node{lhs, rhs}

		m := createTestModelWithDTypePromoConfig(true, false)
		result := m.convertBinaryOp(Add, lhs, rhs)
		outputs = []*Node{
			result,
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[]float32{5.5, 7.5, 9.5},
		int64(dtypes.Float32),
	}, -1)

	// Test Int32 + Int64 -> Int64
	graphtest.RunTestGraphFn(t, "PromoteDType-Int32-Int64", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []int32{1, 2, 3})
		rhs := Const(g, []int64{4, 5, 6})
		inputs = []*Node{lhs, rhs}

		m := createTestModelWithDTypePromoConfig(true, false)
		result := m.convertBinaryOp(Add, lhs, rhs)
		outputs = []*Node{
			result,
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[]int64{5, 7, 9},
		int64(dtypes.Int64),
	}, -1)
}

func TestConvertMatMulMixedDTypes(t *testing.T) {
	// Test MatMul with mixed Float16 and Float32 inputs with PrioritizeFloat16
	graphtest.RunTestGraphFn(t, "MatMul-Mixed-Float16-Float32-PrioritizeFP16", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		lhs = ConvertDType(lhs, dtypes.Float16)
		rhs := Const(g, [][]float32{{1.0, 2.0}, {3.0, 4.0}, {5.0, 6.0}})
		inputs = []*Node{lhs, rhs}

		m := createTestModelWithDTypePromoConfig(true, true)
		result := m.convertMatMul(lhs, rhs)
		outputs = []*Node{
			ConvertDType(result, dtypes.Float32),
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[][]float32{{22.0, 28.0}, {49.0, 64.0}},
		int64(dtypes.Float16),
	}, 1e-1)

	// Test MatMul with mixed Float16 and Float32 inputs without PrioritizeFloat16
	graphtest.RunTestGraphFn(t, "MatMul-Mixed-Float16-Float32-StandardPromotion", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, [][]float32{{1.0, 2.0, 3.0}, {4.0, 5.0, 6.0}})
		lhs = ConvertDType(lhs, dtypes.Float16)
		rhs := Const(g, [][]float32{{1.0, 2.0}, {3.0, 4.0}, {5.0, 6.0}})
		inputs = []*Node{lhs, rhs}

		m := createTestModelWithDTypePromoConfig(true, false)
		result := m.convertMatMul(lhs, rhs)
		outputs = []*Node{
			result,
			Const(g, int64(result.DType())),
		}
		return
	}, []any{
		[][]float32{{22.0, 28.0}, {49.0, 64.0}},
		int64(dtypes.Float32),
	}, -1)
}

func TestOnnxWhereMixedDTypes(t *testing.T) {
	// Test Where with mixed dtypes for onTrue and onFalse
	graphtest.RunTestGraphFn(t, "Where-Mixed-DTypes", func(g *Graph) (inputs, outputs []*Node) {
		cond := Const(g, []bool{true, false, true})
		onTrue := Const(g, []int32{1, 2, 3})
		onFalse := Const(g, []float32{10.0, 20.0, 30.0})
		inputs = []*Node{cond, onTrue, onFalse}

		m := createTestModelWithDTypePromoConfig(true, false)
		outputs = []*Node{
			m.onnxWhere([]*Node{cond, onTrue, onFalse}),
		}
		return
	}, []any{
		// Result should be promoted to Float32
		[]float32{1.0, 20.0, 3.0},
	}, -1)
}

func TestConvertResize(t *testing.T) {
	// Helper to build a Resize node and call convertResize.
	// sizes and scales are placed in convertedOutputs as Const nodes so that
	// materializeConstantExpression can resolve them.
	runResize := func(t *testing.T, name string, buildFn func(g *Graph) (x *Node, node *protos.NodeProto, convertedOutputs map[string]*Node), want any) {
		t.Helper()
		graphtest.RunTestGraphFn(t, name, func(g *Graph) (inputs, outputs []*Node) {
			x, node, convertedOutputs := buildFn(g)
			model := &Model{
				VariableNameToValue: make(map[string]*protos.TensorProto),
				NodeOutputToNode:    make(map[string]*protos.NodeProto),
			}
			inputs = []*Node{x}
			outputs = []*Node{convertResize(model, convertedOutputs, node, []*Node{x})}
			return
		}, []any{want}, -1)
	}

	// Nearest upsample 2x with sizes input, shape [1,1,2,2] -> [1,1,4,4].
	runResize(t, "Resize-nearest-sizes", func(g *Graph) (*Node, *protos.NodeProto, map[string]*Node) {
		x := Const(g, [][][][]float32{{{{1, 2}, {3, 4}}}})
		convertedOutputs := map[string]*Node{
			"sizes": Const(g, []int64{1, 1, 4, 4}),
		}
		node := &protos.NodeProto{
			OpType: "Resize",
			Input:  []string{"X", "", "", "sizes"},
			Output: []string{"Y"},
			Attribute: []*protos.AttributeProto{
				{Name: "mode", Type: protos.AttributeProto_STRING, S: []byte("nearest")},
				{Name: "coordinate_transformation_mode", Type: protos.AttributeProto_STRING, S: []byte("asymmetric")},
			},
		}
		return x, node, convertedOutputs
	}, [][][][]float32{{{{1, 1, 2, 2}, {1, 1, 2, 2}, {3, 3, 4, 4}, {3, 3, 4, 4}}}})

	// Linear (bilinear) downsample with scales input, shape [1,1,4,4] -> [1,1,2,2].
	runResize(t, "Resize-linear-scales", func(g *Graph) (*Node, *protos.NodeProto, map[string]*Node) {
		x := Const(g, [][][][]float32{{{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}, {13, 14, 15, 16}}}})
		convertedOutputs := map[string]*Node{
			"scales": Const(g, []float32{1.0, 1.0, 0.5, 0.5}),
		}
		node := &protos.NodeProto{
			OpType: "Resize",
			Input:  []string{"X", "", "scales"},
			Output: []string{"Y"},
			Attribute: []*protos.AttributeProto{
				{Name: "mode", Type: protos.AttributeProto_STRING, S: []byte("linear")},
			},
		}
		return x, node, convertedOutputs
	}, [][][][]float32{{{{3.5, 5.5}, {11.5, 13.5}}}})

	// No-op resize: sizes match input dimensions.
	runResize(t, "Resize-noop", func(g *Graph) (*Node, *protos.NodeProto, map[string]*Node) {
		x := Const(g, [][][][]float32{{{{1, 2}, {3, 4}}}})
		convertedOutputs := map[string]*Node{
			"sizes": Const(g, []int64{1, 1, 2, 2}),
		}
		node := &protos.NodeProto{
			OpType: "Resize",
			Input:  []string{"X", "", "", "sizes"},
			Output: []string{"Y"},
		}
		return x, node, convertedOutputs
	}, [][][][]float32{{{{1, 2}, {3, 4}}}})
}

func TestMod(t *testing.T) {
	// fmod=1 (C-style): result sign follows the dividend.
	graphtest.RunTestGraphFn(t, "Mod(fmod=1, int)", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []int32{7, -7, 7, -7})
		rhs := Const(g, []int32{3, 3, -3, -3})
		node := &protos.NodeProto{
			OpType: "Mod",
			Attribute: []*protos.AttributeProto{
				{Name: "fmod", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		m := createTestModelWithDTypePromoConfig(false, false)
		inputs = []*Node{lhs, rhs}
		outputs = []*Node{m.convertMod(node, inputs)}
		return
	}, []any{
		[]int32{1, -1, 1, -1},
	}, -1)

	graphtest.RunTestGraphFn(t, "Mod(fmod=1, float)", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{7, -7, 7, -7})
		rhs := Const(g, []float32{3, 3, -3, -3})
		node := &protos.NodeProto{
			OpType: "Mod",
			Attribute: []*protos.AttributeProto{
				{Name: "fmod", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		m := createTestModelWithDTypePromoConfig(false, false)
		inputs = []*Node{lhs, rhs}
		outputs = []*Node{m.convertMod(node, inputs)}
		return
	}, []any{
		[]float32{1, -1, 1, -1},
	}, -1)

	// fmod=0 (default, Python-style): result sign follows the divisor.
	graphtest.RunTestGraphFn(t, "Mod(fmod=0, int)", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []int32{7, -7, 7, -7})
		rhs := Const(g, []int32{3, 3, -3, -3})
		node := &protos.NodeProto{OpType: "Mod"}
		m := createTestModelWithDTypePromoConfig(false, false)
		inputs = []*Node{lhs, rhs}
		outputs = []*Node{m.convertMod(node, inputs)}
		return
	}, []any{
		[]int32{1, 2, -2, -1},
	}, -1)

	graphtest.RunTestGraphFn(t, "Mod(fmod=0, float)", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []float32{7, -7, 7, -7})
		rhs := Const(g, []float32{3, 3, -3, -3})
		node := &protos.NodeProto{OpType: "Mod"}
		m := createTestModelWithDTypePromoConfig(false, false)
		inputs = []*Node{lhs, rhs}
		outputs = []*Node{m.convertMod(node, inputs)}
		return
	}, []any{
		[]float32{1, 2, -2, -1},
	}, -1)

	// Broadcasting: scalar divisor
	graphtest.RunTestGraphFn(t, "Mod(broadcast)", func(g *Graph) (inputs, outputs []*Node) {
		lhs := Const(g, []int32{10, 11, 12})
		rhs := Const(g, int32(3))
		node := &protos.NodeProto{
			OpType: "Mod",
			Attribute: []*protos.AttributeProto{
				{Name: "fmod", Type: protos.AttributeProto_INT, I: 1},
			},
		}
		m := createTestModelWithDTypePromoConfig(false, false)
		inputs = []*Node{lhs, rhs}
		outputs = []*Node{m.convertMod(node, inputs)}
		return
	}, []any{
		[]int32{1, 2, 0},
	}, -1)
}

func TestConvertSize(t *testing.T) {
	graphtest.RunTestGraphFn(t, "Size(scalar)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, float32(42))
		inputs = []*Node{x}
		outputs = []*Node{convertSize(inputs)}
		return
	}, []any{
		int64(1),
	}, -1)

	graphtest.RunTestGraphFn(t, "Size(1D)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{1, 2, 3, 4, 5})
		inputs = []*Node{x}
		outputs = []*Node{convertSize(inputs)}
		return
	}, []any{
		int64(5),
	}, -1)

	graphtest.RunTestGraphFn(t, "Size(2D)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, [][]float32{{1, 2, 3}, {4, 5, 6}})
		inputs = []*Node{x}
		outputs = []*Node{convertSize(inputs)}
		return
	}, []any{
		int64(6),
	}, -1)
}

func TestConvertPadReflect(t *testing.T) {
	// 1D: left padding only
	graphtest.RunTestGraphFn(t, "PadReflect(1D, left=2)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{1, 2, 3, 4})
		inputs = []*Node{x}
		outputs = []*Node{convertPadReflect(x, []int{2, 0}, 1)}
		return
	}, []any{
		[]float32{3, 2, 1, 2, 3, 4},
	}, -1)

	// 1D: right padding only
	graphtest.RunTestGraphFn(t, "PadReflect(1D, right=1)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{1, 2, 3, 4})
		inputs = []*Node{x}
		outputs = []*Node{convertPadReflect(x, []int{0, 1}, 1)}
		return
	}, []any{
		[]float32{1, 2, 3, 4, 3},
	}, -1)

	// 1D: both left and right padding
	graphtest.RunTestGraphFn(t, "PadReflect(1D, left=2, right=1)", func(g *Graph) (inputs, outputs []*Node) {
		x := Const(g, []float32{1, 2, 3, 4})
		inputs = []*Node{x}
		outputs = []*Node{convertPadReflect(x, []int{2, 1}, 1)}
		return
	}, []any{
		// [c, b, a, b, c, d, c] = [3, 2, 1, 2, 3, 4, 3]
		[]float32{3, 2, 1, 2, 3, 4, 3},
	}, -1)

	// 2D: padding on both axes
	graphtest.RunTestGraphFn(t, "PadReflect(2D)", func(g *Graph) (inputs, outputs []*Node) {
		// Input:
		//  [[1, 2, 3],
		//   [4, 5, 6]]
		x := Const(g, [][]float32{{1, 2, 3}, {4, 5, 6}})
		inputs = []*Node{x}
		// pads = [padStart_axis0, padStart_axis1, padEnd_axis0, padEnd_axis1]
		//      = [1,              1,              0,             1]
		outputs = []*Node{convertPadReflect(x, []int{1, 1, 0, 1}, 2)}
		return
	}, []any{
		// After axis 0 (pad start=1, end=0): reflect row 1 above
		//  [[4, 5, 6],
		//   [1, 2, 3],
		//   [4, 5, 6]]
		// After axis 1 (pad start=1, end=1): reflect col 1 left, col -2 right
		//  [[5, 4, 5, 6, 5],
		//   [2, 1, 2, 3, 2],
		//   [5, 4, 5, 6, 5]]
		[][]float32{
			{5, 4, 5, 6, 5},
			{2, 1, 2, 3, 2},
			{5, 4, 5, 6, 5},
		},
	}, -1)
}
