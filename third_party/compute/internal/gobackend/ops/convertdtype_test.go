// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package ops_test

import (
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend/ops"
	"github.com/gomlx/compute/shapes"
)

func TestConvertPackedInt4ToInt8(t *testing.T) {
	// Packed Int4 → Int8: unpacks nibbles with sign extension.
	// Byte 0xF0 = low nibble 0x0 (0), high nibble 0xF (-1).
	// Byte 0x87 = low nibble 0x7 (7), high nibble 0x8 (-8).
	srcData := []byte{0xF0, 0x87}
	srcShape := shapes.Make(dtypes.Int4, 4) // 4 Int4 elements packed in 2 bytes
	srcBuf := makeBuffer(t, srcShape, srcData)

	dstShape := shapes.Make(dtypes.Int8, 4)
	dstBuf, err := backend.GetBuffer(dstShape)
	if err != nil {
		t.Fatalf("Failed to get buffer: %+v", err)
	}

	tmpAny, tmpErr := ops.ConvertDTypePairMap.Get(dtypes.Int4, dtypes.Int8)
	if tmpErr != nil {
		t.Fatalf("Failed to get convertFn: %+v", tmpErr)
	}
	convertFn := tmpAny.(ops.ConvertFnType)
	convertFn(srcBuf, dstBuf)

	result := dstBuf.Flat.([]int8)
	if result[0] != int8(0) {
		t.Errorf("Expected result[0] to be 0, got %d", result[0])
	}
	if result[1] != int8(-1) {
		t.Errorf("Expected result[1] to be -1, got %d", result[1])
	}
	if result[2] != int8(7) {
		t.Errorf("Expected result[2] to be 7, got %d", result[2])
	}
	if result[3] != int8(-8) {
		t.Errorf("Expected result[3] to be -8, got %d", result[3])
	}
}

func TestConvertPackedUint4ToUint8(t *testing.T) {
	// Packed Uint4 → Uint8: unpacks nibbles (no sign extension).
	// Byte 0xF0 = low nibble 0, high nibble 15.
	// Byte 0x87 = low nibble 7, high nibble 8.
	srcData := []byte{0xF0, 0x87}
	srcShape := shapes.Make(dtypes.Uint4, 4) // 4 Uint4 elements packed in 2 bytes
	srcBuf := makeBuffer(t, srcShape, srcData)

	dstShape := shapes.Make(dtypes.Uint8, 4)
	dstBuf := makeBuffer(t, dstShape, make([]uint8, 4))

	tmpAny, tmpErr := ops.ConvertDTypePairMap.Get(dtypes.Uint4, dtypes.Uint8)
	if tmpErr != nil {
		t.Fatalf("Failed to get convertFn: %+v", tmpErr)
	}
	convertFn := tmpAny.(ops.ConvertFnType)
	convertFn(srcBuf, dstBuf)

	result := dstBuf.Flat.([]uint8)
	if result[0] != uint8(0) {
		t.Errorf("Expected result[0] to be 0, got %d", result[0])
	}
	if result[1] != uint8(15) {
		t.Errorf("Expected result[1] to be 15, got %d", result[1])
	}
	if result[2] != uint8(7) {
		t.Errorf("Expected result[2] to be 7, got %d", result[2])
	}
	if result[3] != uint8(8) {
		t.Errorf("Expected result[3] to be 8, got %d", result[3])
	}
}

func TestConvertPackedInt4ToFloat32(t *testing.T) {
	// Packed Int4 → Float32: unpacks and converts.
	srcData := []byte{0xF0, 0x87}
	srcShape := shapes.Make(dtypes.Int4, 4)
	srcBuf := makeBuffer(t, srcShape, srcData)

	dstShape := shapes.Make(dtypes.Float32, 4)
	dstBuf := makeBuffer(t, dstShape, make([]float32, 4))

	tmpAny, tmpErr := ops.ConvertDTypePairMap.Get(dtypes.Int4, dtypes.Float32)
	if tmpErr != nil {
		t.Fatalf("Failed to get convertFn: %+v", tmpErr)
	}
	convertFn := tmpAny.(ops.ConvertFnType)
	convertFn(srcBuf, dstBuf)

	result := dstBuf.Flat.([]float32)
	if result[0] != float32(0) {
		t.Errorf("Expected result[0] to be 0, got %f", result[0])
	}
	if result[1] != float32(-1) {
		t.Errorf("Expected result[1] to be -1, got %f", result[1])
	}
	if result[2] != float32(7) {
		t.Errorf("Expected result[2] to be 7, got %f", result[2])
	}
	if result[3] != float32(-8) {
		t.Errorf("Expected result[3] to be -8, got %f", result[3])
	}
}

func TestConvertPackedInt2ToInt8(t *testing.T) {
	// Packed Int2 → Int8: unpacks 2-bit values with sign extension.
	// Byte 0b11_10_01_00 = 0xE4: values 0, 1, -2, -1.
	srcData := []byte{0xE4}
	srcShape := shapes.Make(dtypes.Int2, 4) // 4 Int2 elements packed in 1 byte
	srcBuf := makeBuffer(t, srcShape, srcData)

	dstShape := shapes.Make(dtypes.Int8, 4)
	dstBuf := makeBuffer(t, dstShape, make([]int8, 4))

	tmpAny, tmpErr := ops.ConvertDTypePairMap.Get(dtypes.Int2, dtypes.Int8)
	if tmpErr != nil {
		t.Fatalf("Failed to get convertFn: %+v", tmpErr)
	}
	convertFn := tmpAny.(ops.ConvertFnType)
	convertFn(srcBuf, dstBuf)

	result := dstBuf.Flat.([]int8)
	if result[0] != int8(0) {
		t.Errorf("Expected result[0] to be 0, got %d", result[0])
	}
	if result[1] != int8(1) {
		t.Errorf("Expected result[1] to be 1, got %d", result[1])
	}
	if result[2] != int8(-2) {
		t.Errorf("Expected result[2] to be -2, got %d", result[2])
	}
	if result[3] != int8(-1) {
		t.Errorf("Expected result[3] to be -1, got %d", result[3])
	}
}

func TestConvertPackedUint2ToUint8(t *testing.T) {
	// Packed Uint2 → Uint8: unpacks 2-bit values (no sign extension).
	// Byte 0b11_10_01_00 = 0xE4: values 0, 1, 2, 3.
	srcData := []byte{0xE4}
	srcShape := shapes.Make(dtypes.Uint2, 4) // 4 Uint2 elements packed in 1 byte
	srcBuf := makeBuffer(t, srcShape, srcData)

	dstShape := shapes.Make(dtypes.Uint8, 4)
	dstBuf := makeBuffer(t, dstShape, make([]uint8, 4))

	tmpAny, tmpErr := ops.ConvertDTypePairMap.Get(dtypes.Uint2, dtypes.Uint8)
	if tmpErr != nil {
		t.Fatalf("Failed to get convertFn: %+v", tmpErr)
	}
	convertFn := tmpAny.(ops.ConvertFnType)
	convertFn(srcBuf, dstBuf)

	result := dstBuf.Flat.([]uint8)
	if result[0] != uint8(0) {
		t.Errorf("Expected result[0] to be 0, got %d", result[0])
	}
	if result[1] != uint8(1) {
		t.Errorf("Expected result[1] to be 1, got %d", result[1])
	}
	if result[2] != uint8(2) {
		t.Errorf("Expected result[2] to be 2, got %d", result[2])
	}
	if result[3] != uint8(3) {
		t.Errorf("Expected result[3] to be 3, got %d", result[3])
	}
}
