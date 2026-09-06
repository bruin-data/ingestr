package gliner

import (
	"reflect"
	"testing"

	"github.com/gomlx/compute-onnx/support/protos"
	"github.com/gomlx/compute/gobackend"
	"github.com/gomlx/gomlx/core/graph"
	"github.com/gomlx/gomlx/core/tensors"
	"github.com/gomlx/gomlx/ml/model"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPoCCompatibility(t *testing.T) {
	tensor := func(name string, dims, values []int64) *protos.TensorProto {
		return &protos.TensorProto{Name: name, DataType: int32(protos.TensorProto_INT64), Dims: dims, Int64Data: values}
	}
	cases := []struct {
		name string
		node *protos.NodeProto
		data []*protos.TensorProto
		dims []int
		want []int64
	}{
		{"FlattenAxisRank", &protos.NodeProto{OpType: "Flatten", Input: []string{"data"}, Output: []string{"out"}, Attribute: []*protos.AttributeProto{{Name: "axis", Type: protos.AttributeProto_INT, I: 2}}}, []*protos.TensorProto{tensor("data", []int64{2, 3}, []int64{1, 2, 3, 4, 5, 6})}, []int{6, 1}, []int64{1, 2, 3, 4, 5, 6}},
		{"ScatterNegativeIndices", &protos.NodeProto{OpType: "ScatterElements", Input: []string{"data", "indices", "updates"}, Output: []string{"out"}}, []*protos.TensorProto{tensor("data", []int64{3}, []int64{10, 20, 30}), tensor("indices", []int64{2}, []int64{-1, 0}), tensor("updates", []int64{2}, []int64{9, 8})}, []int{3}, []int64{8, 20, 9}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shape := &protos.TensorShapeProto{}
			for _, d := range c.dims {
				shape.Dim = append(shape.Dim, &protos.TensorShapeProto_Dimension{Value: &protos.TensorShapeProto_Dimension_DimValue{DimValue: int64(d)}})
			}
			p := &protos.ModelProto{IrVersion: 8, OpsetImport: []*protos.OperatorSetIdProto{{Version: 13}}, Graph: &protos.GraphProto{Node: []*protos.NodeProto{c.node}, Initializer: c.data, Output: []*protos.ValueInfoProto{{Name: "out", Type: &protos.TypeProto{Value: &protos.TypeProto_TensorType{TensorType: &protos.TypeProto_Tensor{ElemType: int32(protos.TensorProto_INT64), Shape: shape}}}}}}}
			data, err := proto.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			om, err := parser.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = om.Close() }()
			store := model.NewStore()
			defer store.Finalize()
			if err := om.VariablesToScope(store.RootScope()); err != nil {
				t.Fatal(err)
			}
			b, err := gobackend.New("")
			if err != nil {
				t.Fatal(err)
			}
			defer b.Finalize()
			exec := model.MustNewExec(b, store, func(s *model.Scope, g *graph.Graph) []*graph.Node { return om.CallGraph(s, g, nil) })
			defer exec.Finalize()
			out, err := exec.Call()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { require.NoError(t, out[0].FinalizeAll()) }()
			if !reflect.DeepEqual(out[0].Shape().Dimensions, c.dims) {
				t.Fatal(out[0].Shape())
			}
			if got := tensors.MustCopyFlatData[int64](out[0]); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestSmallMatmulTypedSlices(t *testing.T) {
	for _, transposed := range []bool{false, true} {
		name := "non_transposed"
		if transposed {
			name = "transposed"
		}
		t.Run(name, func(t *testing.T) {
			lhs := make([][][]float32, 3)
			rhs := make([][][]float32, 3)
			var want []float32
			for b := range lhs {
				lhs[b] = make([][]float32, 4)
				for i := range lhs[b] {
					lhs[b][i] = []float32{1, 2, float32(b + i), 4, 5}
				}
				rows, cols := 5, 6
				if transposed {
					rows, cols = cols, rows
				}
				rhs[b] = make([][]float32, rows)
				for i := range rhs[b] {
					rhs[b][i] = make([]float32, cols)
					for j := range rhs[b][i] {
						rhs[b][i][j] = float32(b + i + j)
					}
				}
				for i := range lhs[b] {
					for j := range 6 {
						var sum float32
						for k := range 5 {
							sum += lhs[b][i][k] * float32(b+k+j)
						}
						want = append(want, sum)
					}
				}
			}
			backend, err := gobackend.New("")
			require.NoError(t, err)
			defer backend.Finalize()
			store := model.NewStore()
			defer store.Finalize()
			rhsAxis := 1
			if transposed {
				rhsAxis = 2
			}
			exec := model.MustNewExec(backend, store, func(_ *model.Scope, left, right *graph.Node) *graph.Node {
				return graph.DotGeneral(left, []int{2}, []int{0}, right, []int{rhsAxis}, []int{0})
			})
			defer exec.Finalize()
			out, err := exec.Call(lhs, rhs)
			require.NoError(t, err)
			defer func() { require.NoError(t, out[0].FinalizeAll()) }()
			require.Equal(t, want, tensors.MustCopyFlatData[float32](out[0]))
		})
	}
}
