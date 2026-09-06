// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"fmt"
	"testing"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/testutil"
)

func TestBroadcastIterator(t *testing.T) {
	S := func(dims ...int) shapes.Shape {
		return shapes.Make(dtypes.Float32, dims...)
	}

	t.Run("[2,1] -> [2, 3]", func(t *testing.T) {
		targetShape := S(2, 3)
		bi := NewBroadcastIterator(S(2, 1), targetShape)
		srcIndices := make([]int, 0, targetShape.Size())
		for srcIdx := range bi.IterFlatIndices() {
			srcIndices = append(srcIndices, srcIdx)
		}
		if ok, diff := testutil.IsEqual([]int{0, 0, 0, 1, 1, 1}, srcIndices); !ok {
			t.Errorf("srcIndices mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("[1,3] -> [2, 3]", func(t *testing.T) {
		targetShape := S(2, 3)
		bi := NewBroadcastIterator(S(1, 3), targetShape)
		srcIndices := make([]int, 0, targetShape.Size())
		for srcIdx := range bi.IterFlatIndices() {
			srcIndices = append(srcIndices, srcIdx)
		}
		if ok, diff := testutil.IsEqual([]int{0, 1, 2, 0, 1, 2}, srcIndices); !ok {
			t.Errorf("srcIndices mismatch (-want +got):\n%s", diff)
		}
	})

	// Alternating broadcast axes.
	t.Run("[3,1,4,1] -> [3,2,4,2]", func(t *testing.T) {
		targetShape := S(3, 2, 4, 2)
		bi := NewBroadcastIterator(S(3, 1, 4, 1), targetShape)
		srcIndices := make([]int, 0, targetShape.Size())
		for srcIdx := range bi.IterFlatIndices() {
			srcIndices = append(srcIndices, srcIdx)
		}
		want := []int{
			0, 0, 1, 1, 2, 2, 3, 3,
			0, 0, 1, 1, 2, 2, 3, 3,
			4, 4, 5, 5, 6, 6, 7, 7,
			4, 4, 5, 5, 6, 6, 7, 7,
			8, 8, 9, 9, 10, 10, 11, 11,
			8, 8, 9, 9, 10, 10, 11, 11}
		if ok, diff := testutil.IsEqual(want, srcIndices); !ok {
			fmt.Printf("\t- want: %#v\n", want)
			fmt.Printf("\t- got:  %#v\n", srcIndices)
			t.Errorf("srcIndices mismatch (-want +got):\n%s", diff)
		}
	})
}
