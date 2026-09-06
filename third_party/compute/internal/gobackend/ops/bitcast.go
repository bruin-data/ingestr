// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"reflect"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapeinference"
)

func init() {
	gobackend.SetNodeExecutor(compute.OpTypeBitcast, gobackend.PriorityGeneric, execBitcast)
	gobackend.RegisterBitcast.Register(Bitcast, gobackend.PriorityGeneric)
}

// Bitcast performs an elementwise bitcast operation from a dtype to another dtype.
//
// The Bitcast doesn't "convert", rather it just reinterprets the bits from operand.DType() to the targetDType.
//
// If the element sizes (in bytes/bits) differ, the last dimension is adjusted:
//   - Smaller target: a new trailing axis of size (srcBits / dstBits) is appended, so rank is increased by 1.
//   - Larger target: the last axis must be equal to (dstBits / srcBits), and the resultign rank is decreased by 1 ("squeezed").
//
// E.g:
//
//	Bitcast([1]uint32{0xdeadbeef}, dtypes.UInt16) -> [1][2]uint16{{0xbeef, 0xdead}} // Little-endian encoding.
//	Bitcast([1][2]uint16{{0xbeef, 0xdead}}, dtypes.UInt32) -> [1]uint32{0xdeadbeef}
func Bitcast(f *gobackend.Function, operandOp compute.Value, targetDType dtypes.DType) (compute.Value, error) {
	opType := compute.OpTypeBitcast
	inputs, err := f.VerifyAndCastValues(opType.String(), operandOp)
	if err != nil {
		return nil, err
	}
	operand := inputs[0]
	outputShape, err := shapeinference.Bitcast(operand.Shape, targetDType)
	if err != nil {
		return nil, err
	}
	node, _ := f.GetOrCreateNode(opType, outputShape, []*gobackend.Node{operand}, nil)
	return node, nil
}

// execBitcast implements Bitcast as a pure bit reinterpretation: the raw bytes are
// unchanged, only the shape and dtype are updated.
//
// For same-bit-width types (e.g. int8 ↔ uint8), the buffer is reused if owned
// and the Go storage type matches, otherwise a byte-level copy is performed.
//
// For different-bit-width types (e.g. uint8 → Int4), the raw bytes stay identical.
// A uint8[N] buffer bitcast to Int4[2*N] keeps its []byte flat data — the 2*N
// nibbles are stored packed (2 per byte). To unpack into one value per element,
// use ConvertDType (e.g. ConvertDType(Int4 → Int8)).
func execBitcast(backend *gobackend.Backend, node *gobackend.Node, inputs []*gobackend.Buffer, inputsOwned []bool) (*gobackend.Buffer, error) {
	src := inputs[0]
	targetDType := node.Shape.DType
	srcDType := src.RawShape.DType
	sameBitWidth := srcDType.Bits() == targetDType.Bits()

	// Reuse owned buffer only when the flat slice's Go element type is compatible
	// with downstream operations on the target dtype. For same-bit-width types
	// with matching Go types this is trivially true. For different-bit-width types
	// involving sub-byte types (e.g. Uint8 ↔ Int4), both use []uint8 storage so
	// reuse is safe. But Uint8 → Float16 must allocate because Float16 operations
	// expect []float16.Float16, not []uint8.
	canReuse := false
	if inputsOwned[0] {
		if sameBitWidth && srcDType.GoType() == targetDType.GoType() {
			canReuse = true
		} else if !sameBitWidth {
			// For different-bit-width bitcasts, reuse only when both source and
			// target use the same underlying Go storage type. Sub-byte types
			// (Int2, Uint2, Int4, Uint4) all store as []uint8.
			_, srcIsUint8 := src.Flat.([]uint8)
			tgtIsUint8 := targetDType.GoType().Kind() == reflect.Uint8
			canReuse = srcIsUint8 && tgtIsUint8
		}
	}
	if canReuse {
		src.RawShape = node.Shape
		inputs[0] = nil // signal to executor that input buffer was reused
		return src, nil
	}

	// Not owned or Go type mismatch: allocate via the standard pool and copy
	// raw bytes. For sub-byte types, getBufferForShape allocates packed storage
	// (e.g. Int4[2N] gets []byte of length N), matching the source byte count.
	output, err := backend.GetBuffer(node.Shape)
	if err != nil {
		return nil, err
	}
	outputBytes, err := output.MutableBytes()
	if err != nil {
		return nil, err
	}
	srcBytes, err := src.MutableBytes()
	if err != nil {
		return nil, err
	}
	copy(outputBytes, srcBytes)
	return output, nil
}
