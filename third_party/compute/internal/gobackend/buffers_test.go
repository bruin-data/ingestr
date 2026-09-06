// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend_test

import (
	"runtime"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestBuffers_Bytes(t *testing.T) {
	buf, err := backend.(*gobackend.Backend).GetBuffer(shapes.Make(dtypes.Int32, 3))
	if err != nil {
		t.Fatalf("Failed to get buffer: %+v", err)
	}
	buf.Zeros()
	if len(buf.Flat.([]int32)) != 3 {
		t.Fatalf("Expected length 3, got %d", len(buf.Flat.([]int32)))
	}
	flatBytes, err := buf.MutableBytes()
	if err != nil {
		t.Fatalf("Failed to get mutable bytes: %+v", err)
	}
	if len(flatBytes) != 3*int(dtypes.Int32.Size()) {
		t.Fatalf("Expected length %d, got %d", 3*int(dtypes.Int32.Size()), len(flatBytes))
	}
	flatBytes[0] = 1
	flatBytes[4] = 7
	flatBytes[8] = 3
	if ok, diff := testutil.IsEqual([]int32{1, 7, 3}, buf.Flat.([]int32)); !ok {
		t.Fatalf("Unexpected result (-want +got):\n%s", diff)
	}
	runtime.KeepAlive(buf)
}

func TestBuffers_Fill(t *testing.T) {
	buf, err := backend.(*gobackend.Backend).GetBuffer(shapes.Make(dtypes.Int32, 3))
	if err != nil {
		t.Fatalf("Failed to get buffer: %+v", err)
	}
	if len(buf.Flat.([]int32)) != 3 {
		t.Fatalf("Expected length 3, got %d", len(buf.Flat.([]int32)))
	}
	if err := buf.Fill(int32(3)); err != nil {
		t.Fatalf("Failed to fill buffer: %+v", err)
	}
	if ok, diff := testutil.IsEqual([]int32{3, 3, 3}, buf.Flat.([]int32)); !ok {
		t.Fatalf("Unexpected result (-want +got):\n%s", diff)
	}

	buf.Zeros()
	if ok, diff := testutil.IsEqual([]int32{0, 0, 0}, buf.Flat.([]int32)); !ok {
		t.Fatalf("Unexpected result (-want +got):\n%s", diff)
	}
}
