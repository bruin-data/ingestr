package dot_test

import (
	"reflect"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/internal/gobackend/dot"
	"github.com/gomlx/compute/shapes"
)

func TestTransposeToLayout(t *testing.T) {
	if _, ok := backend.(*gobackend.Backend); !ok {
		t.Skip("Skipping test because backend is not the Go backend")
	}

	testCases := []struct {
		name                  string
		lhsShape              shapes.Shape
		lhsContract, lhsBatch []int
		rhsShape              shapes.Shape
		rhsContract, rhsBatch []int
		layout                dot.Layout
		wantLhsDims           []int
		wantLhsContract       []int
		wantLhsBatch          []int
		wantRhsDims           []int
		wantRhsContract       []int
		wantRhsBatch          []int
	}{
		{
			name:            "LayoutTransposed",
			lhsShape:        shapes.Make(dtypes.Float32, 2, 3, 4, 5), // Batch=2, Cross=(3,4), Contracting=5
			lhsContract:     []int{3},
			lhsBatch:        []int{0},
			rhsShape:        shapes.Make(dtypes.Float32, 2, 6, 5), // Batch=2, Cross=6, Contracting=5
			rhsContract:     []int{2},
			rhsBatch:        []int{0},
			layout:          dot.LayoutTransposed,
			wantLhsDims:     []int{2, 12, 5}, // Batch=2, Cross=3*4=12, Contracting=5
			wantLhsContract: []int{2},
			wantLhsBatch:    []int{0},
			wantRhsDims:     []int{2, 6, 5},
			wantRhsContract: []int{2},
			wantRhsBatch:    []int{0},
		},
		{
			name:            "LayoutNonTransposed",
			lhsShape:        shapes.Make(dtypes.Float32, 2, 3, 4, 5), // Batch=2, Cross=(3,4), Contracting=5
			lhsContract:     []int{3},
			lhsBatch:        []int{0},
			rhsShape:        shapes.Make(dtypes.Float32, 2, 6, 5), // Batch=2, Cross=6, Contracting=5
			rhsContract:     []int{2},
			rhsBatch:        []int{0},
			layout:          dot.LayoutNonTransposed,
			wantLhsDims:     []int{2, 12, 5}, // Batch=2, Cross=3*4=12, Contracting=5
			wantLhsContract: []int{2},
			wantLhsBatch:    []int{0},
			wantRhsDims:     []int{2, 5, 6}, // LayoutNonTransposed for RHS -> [Batch, Contracting, Cross] -> [2, 5, 6]
			wantRhsContract: []int{1},
			wantRhsBatch:    []int{0},
		},
		{
			name:            "Non-sequential original axes",
			lhsShape:        shapes.Make(dtypes.Float32, 4, 2, 5, 3), // e.g. Cross1=4, Batch=2, Contract=5, Cross2=3
			lhsContract:     []int{2},
			lhsBatch:        []int{1},
			rhsShape:        shapes.Make(dtypes.Float32, 5, 2, 6), // Contract=5, Batch=2, Cross=6
			rhsContract:     []int{0},
			rhsBatch:        []int{1},
			layout:          dot.LayoutNonTransposed,
			wantLhsDims:     []int{2, 12, 5}, // Batch=2, Cross=4*3=12, Contracting=5
			wantLhsContract: []int{2},
			wantLhsBatch:    []int{0},
			wantRhsDims:     []int{2, 5, 6}, // Batch=2, Contracting=5, Cross=6
			wantRhsContract: []int{1},
			wantRhsBatch:    []int{0},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := backend.Builder("test_TransposeToLayout")
			compFunc, err := builder.NewFunction("test")
			if err != nil {
				t.Fatalf("failed to create function: %v", err)
			}
			f := compFunc.(*gobackend.Function)

			// Create parameters with the given shapes
			lhsVal, err := f.Parameter("lhs", tc.lhsShape, nil)
			if err != nil {
				t.Fatalf("failed to create parameter: %v", err)
			}
			rhsVal, err := f.Parameter("rhs", tc.rhsShape, nil)
			if err != nil {
				t.Fatalf("failed to create parameter: %v", err)
			}
			lhsNode := lhsVal.(*gobackend.Node)
			rhsNode := rhsVal.(*gobackend.Node)

			newLhs, newLhsContractingAxes, newLhsBatchAxes,
				newRhs, newRhsContractingAxes, newRhsBatchAxes,
				err := dot.TransposeToLayout(
				f, lhsNode, tc.lhsContract, tc.lhsBatch,
				rhsNode, tc.rhsContract, tc.rhsBatch, tc.layout)

			if err != nil {
				t.Fatalf("TransposeToLayout failed: %+v", err)
			}

			if !reflect.DeepEqual(newLhs.Shape.Dimensions, tc.wantLhsDims) {
				t.Errorf("newLhs.Shape.Dimensions = %v, want %v", newLhs.Shape.Dimensions, tc.wantLhsDims)
			}
			if !reflect.DeepEqual(newLhsContractingAxes, tc.wantLhsContract) {
				t.Errorf("newLhsContractingAxes = %v, want %v", newLhsContractingAxes, tc.wantLhsContract)
			}
			if !reflect.DeepEqual(newLhsBatchAxes, tc.wantLhsBatch) {
				t.Errorf("newLhsBatchAxes = %v, want %v", newLhsBatchAxes, tc.wantLhsBatch)
			}

			if !reflect.DeepEqual(newRhs.Shape.Dimensions, tc.wantRhsDims) {
				t.Errorf("newRhs.Shape.Dimensions = %v, want %v", newRhs.Shape.Dimensions, tc.wantRhsDims)
			}
			if !reflect.DeepEqual(newRhsContractingAxes, tc.wantRhsContract) {
				t.Errorf("newRhsContractingAxes = %v, want %v", newRhsContractingAxes, tc.wantRhsContract)
			}
			if !reflect.DeepEqual(newRhsBatchAxes, tc.wantRhsBatch) {
				t.Errorf("newRhsBatchAxes = %v, want %v", newRhsBatchAxes, tc.wantRhsBatch)
			}
		})
	}
}
