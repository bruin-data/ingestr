// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops

import (
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestBitcast_Uint8ToUint4_PureReinterpret(t *testing.T) {
	// Bitcast uint8[2] → Uint4[4]: raw bytes stay the same.
	// Byte 0xF0 = low nibble 0, high nibble 15.
	// Byte 0x87 = low nibble 7, high nibble 8.
	backend, err := gobackend.New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %+v", err)
	}
	defer backend.Finalize()

	srcData := []uint8{0xF0, 0x87}
	srcShape := shapes.Make(dtypes.Uint8, 2)
	srcComputeBuf, err := backend.BufferFromFlatData(0, srcData, srcShape)
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}
	srcBuf := srcComputeBuf.(*gobackend.Buffer)

	dstShape := shapes.Make(dtypes.Uint4, 4)
	node := &gobackend.Node{Shape: dstShape}

	// Not owned: should copy bytes without unpacking.
	result, err := execBitcast(backend.(*gobackend.Backend), node, []*gobackend.Buffer{srcBuf}, []bool{false})
	if err != nil {
		t.Fatalf("execBitcast failed: %+v", err)
	}
	if !result.RawShape.Equal(dstShape) {
		t.Errorf("Expected shape %s, got %s", dstShape, result.RawShape)
	}

	// Raw bytes should be identical to source.
	resultData := result.Flat.([]byte)
	if ok, diff := testutil.IsEqual([]byte(srcData), resultData); !ok {
		t.Errorf("Result data mismatch (-want +got):\n%s", diff)
	}
}

func TestBitcast_Uint8ToInt4_PureReinterpret(t *testing.T) {
	// Bitcast uint8[2] → Int4[4]: raw bytes stay the same.
	backend, err := gobackend.New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %+v", err)
	}
	defer backend.Finalize()

	srcData := []uint8{0xF0, 0x87}
	srcShape := shapes.Make(dtypes.Uint8, 2)
	srcComputeBuf, err := backend.BufferFromFlatData(0, srcData, srcShape)
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}
	srcBuf := srcComputeBuf.(*gobackend.Buffer)

	dstShape := shapes.Make(dtypes.Int4, 4)
	node := &gobackend.Node{Shape: dstShape}

	result, err := execBitcast(backend.(*gobackend.Backend), node, []*gobackend.Buffer{srcBuf}, []bool{false})
	if err != nil {
		t.Fatalf("execBitcast failed: %+v", err)
	}
	if !result.RawShape.Equal(dstShape) {
		t.Errorf("Expected shape %s, got %s", dstShape, result.RawShape)
	}

	// Raw bytes should be identical — Bitcast doesn't unpack.
	resultData := result.Flat.([]byte)
	if ok, diff := testutil.IsEqual([]byte(srcData), resultData); !ok {
		t.Errorf("Result data mismatch (-want +got):\n%s", diff)
	}
}

func TestBitcast_Uint8ToInt4_OwnedReuse(t *testing.T) {
	backend, err := gobackend.New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %+v", err)
	}
	defer backend.Finalize()

	// When owned, Bitcast should reuse the buffer.
	srcData := []uint8{0xAB, 0xCD}
	srcShape := shapes.Make(dtypes.Uint8, 2)
	srcComputeBuf, err := backend.BufferFromFlatData(0, srcData, srcShape)
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}
	srcBuf := srcComputeBuf.(*gobackend.Buffer)

	dstShape := shapes.Make(dtypes.Int4, 4)
	node := &gobackend.Node{Shape: dstShape}

	result, err := execBitcast(backend.(*gobackend.Backend), node, []*gobackend.Buffer{srcBuf}, []bool{true})
	if err != nil {
		t.Fatalf("execBitcast failed: %+v", err)
	}
	if !result.RawShape.Equal(dstShape) {
		t.Errorf("Expected shape %s, got %s", dstShape, result.RawShape)
	}
	// Should be the exact same buffer (reused).
	if result != srcBuf {
		t.Errorf("Expected buffer reuse, but got different buffer")
	}
	if ok, diff := testutil.IsEqual([]byte(srcData), result.Flat.([]byte)); !ok {
		t.Errorf("Result data mismatch (-want +got):\n%s", diff)
	}
}

func TestBitcast_SameSize_Uint8ToInt8(t *testing.T) {
	// Same bit-width, different Go type: should copy bytes.
	backend, err := gobackend.New("")
	if err != nil {
		t.Fatalf("Failed to create backend: %+v", err)
	}
	defer backend.Finalize()

	srcData := []uint8{0xFF, 0x80, 0x01}
	srcShape := shapes.Make(dtypes.Uint8, 3)
	srcComputeBuf, err := backend.BufferFromFlatData(0, srcData, srcShape)
	if err != nil {
		t.Fatalf("BufferFromFlatData failed: %+v", err)
	}
	srcBuf := srcComputeBuf.(*gobackend.Buffer)

	dstShape := shapes.Make(dtypes.Int8, 3)
	node := &gobackend.Node{Shape: dstShape}

	result, err := execBitcast(backend.(*gobackend.Backend), node, []*gobackend.Buffer{srcBuf}, []bool{false})
	if err != nil {
		t.Fatalf("execBitcast failed: %+v", err)
	}
	if !result.RawShape.Equal(dstShape) {
		t.Errorf("Expected shape %s, got %s", dstShape, result.RawShape)
	}

	// Verify byte-level identity.
	resultData := result.Flat.([]int8)
	if resultData[0] != int8(-1) {
		t.Errorf("Expected resultData[0] to be -1, got %d", resultData[0])
	}
	if resultData[1] != int8(-128) {
		t.Errorf("Expected resultData[1] to be -128, got %d", resultData[1])
	}
	if resultData[2] != int8(1) {
		t.Errorf("Expected resultData[2] to be 1, got %d", resultData[2])
	}
}
