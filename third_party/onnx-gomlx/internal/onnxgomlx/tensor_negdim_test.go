package onnxgomlx

import (
	"testing"

	"github.com/gomlx/compute-onnx/support/protos"
)

func TestNegativeDimRejected(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Shape/Size panicked on negative dim instead of erroring: %v", r)
		}
	}()
	proto := &protos.TensorProto{Dims: []int64{-1}, DataType: int32(protos.TensorProto_FLOAT)}
	shape, err := Shape(proto)
	if err == nil {
		t.Fatalf("expected error for negative dim, got shape %v", shape)
		_ = shape.Size() // would panic pre-fix
	}
	t.Logf("negative dim correctly rejected: %v", err)
}

func TestValidDimsStillWork(t *testing.T) {
	proto := &protos.TensorProto{Dims: []int64{3, 4}, DataType: int32(protos.TensorProto_INT32)}
	shape, err := Shape(proto)
	if err != nil {
		t.Fatalf("valid dims errored: %v", err)
	}
	if shape.Size() != 12 {
		t.Fatalf("Size()=%d want 12", shape.Size())
	}
}
