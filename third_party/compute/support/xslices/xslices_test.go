// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package xslices

import (
	"flag"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

func TestMap(t *testing.T) {
	count := 17
	in := make([]int, count)
	for ii := range count {
		in[ii] = ii
	}
	out := Map(in, func(v int) int32 { return int32(v + 1) })
	for ii := range count {
		if out[ii] != int32(ii+1) {
			t.Errorf("element %d doesn't match: got %v, want %v", ii, out[ii], int32(ii+1))
		}
	}
}

func TestMapParallel(t *testing.T) {
	count := 17
	in := make([]int, count)
	for ii := range count {
		in[ii] = ii
	}
	out := MapParallel(in, func(v int) int32 { return int32(v + 1) })
	for ii := range count {
		if out[ii] != int32(ii+1) {
			t.Errorf("element %d doesn't match: got %v, want %v", ii, out[ii], int32(ii+1))
		}
	}
}

func TestAtAndLast(t *testing.T) {
	slice := []int{0, 1, 2, 3, 4, 5}
	if got, want := At(slice, -1), 5; got != want {
		t.Errorf("At(slice, -1): got %v, want %v", got, want)
	}
	if got, want := At(slice, -2), 4; got != want {
		t.Errorf("At(slice, -2): got %v, want %v", got, want)
	}
	if got, want := Last(slice), 5; got != want {
		t.Errorf("Last(slice): got %v, want %v", got, want)
	}
}

func TestPop(t *testing.T) {
	slice := []int{0, 1, 2, 3, 4, 5}
	var got int
	got, slice = Pop(slice)
	if got != 5 {
		t.Errorf("Pop got %v, want %v", got, 5)
	}
	if len(slice) != 5 {
		t.Errorf("Pop slice len got %d, want %d", len(slice), 5)
	}

	got, slice = Pop(slice)
	if got != 4 {
		t.Errorf("Pop got %v, want %v", got, 4)
	}
	if len(slice) != 4 {
		t.Errorf("Pop slice len got %d, want %d", len(slice), 4)
	}
}

type StringerFloat float64

func (f StringerFloat) String() string {
	return fmt.Sprintf("%.02f", float64(f))
}

func TestSliceFlag(t *testing.T) {
	f1Ptr := Flag("f1", []int{2, 3}, "f1 flag test", strconv.Atoi)
	if !slices.Equal(*f1Ptr, []int{2, 3}) {
		t.Errorf("Flag f1 initial value: got %v, want %v", *f1Ptr, []int{2, 3})
	}
	if err := flag.Set("f1", "3,4,5"); err != nil {
		t.Fatalf("flag.Set f1 failed: %+v", err)
	}
	if !slices.Equal(*f1Ptr, []int{3, 4, 5}) {
		t.Errorf("Flag f1 after Set: got %v, want %v", *f1Ptr, []int{3, 4, 5})
	}
	f1Flag := flag.Lookup("f1")
	if f1Flag == nil {
		t.Fatal("flag.Lookup f1 returned nil")
	}
	if f1Flag.DefValue != "2,3" {
		t.Errorf("Flag f1 DefValue: got %q, want %q", f1Flag.DefValue, "2,3")
	}

	f2Ptr := Flag("f2", []StringerFloat{2.0, 3.0}, "f2 flag test",
		func(v string) (StringerFloat, error) {
			f, err := strconv.ParseFloat(v, 64)
			return StringerFloat(f), err
		})
	if !slices.Equal(*f2Ptr, []StringerFloat{2, 3}) {
		t.Errorf("Flag f2 initial value: got %v, want %v", *f2Ptr, []StringerFloat{2, 3})
	}
	if err := flag.Set("f2", "3,4,5"); err != nil {
		t.Fatalf("flag.Set f2 failed: %+v", err)
	}
	if !slices.Equal(*f2Ptr, []StringerFloat{3, 4, 5}) {
		t.Errorf("Flag f2 after Set: got %v, want %v", *f2Ptr, []StringerFloat{3, 4, 5})
	}
	f2Flag := flag.Lookup("f2")
	if f2Flag == nil {
		t.Fatal("flag.Lookup f2 returned nil")
	}
	if f2Flag.DefValue != "2.00,3.00" {
		t.Errorf("Flag f2 DefValue: got %q, want %q", f2Flag.DefValue, "2.00,3.00")
	}
}

func TestOperations(t *testing.T) {
	s := []float32{1.0, -3.0, 2.0}
	if got, want := Max(s), float32(2); got != want {
		t.Errorf("Max: got %v, want %v", got, want)
	}
	if got, want := Min(s), float32(-3); got != want {
		t.Errorf("Min: got %v, want %v", got, want)
	}

	SetAt(s, 0, 10)
	if s[0] != 10 {
		t.Errorf("SetAt(0, 10): got %v, want %v", s[0], 10)
	}
	SetLast(s, 100)
	if s[2] != 100 {
		t.Errorf("SetLast(100): got %v, want %v", s[2], 100)
	}

	FillSlice(s, -7)
	if !slices.Equal(s, []float32{-7, -7, -7}) {
		t.Errorf("FillSlice: got %v, want %v", s, []float32{-7, -7, -7})
	}
	FillAnySlice(s, float32(11))
	if !slices.Equal(s, []float32{11, 11, 11}) {
		t.Errorf("FillAnySlice: got %v, want %v", s, []float32{11, 11, 11})
	}

	md := MultidimensionalSliceWithValue(int64(13), 2, 1, 1)
	wantMD := [][][]int64{{{13}}, {{13}}}
	if !reflect.DeepEqual(md, wantMD) {
		t.Errorf("MultidimensionalSliceWithValue: got %v, want %v", md, wantMD)
	}

	s2d := Slice2DWithValue(int8(67), 2, 1)
	want2d := [][]int8{{67}, {67}}
	if !reflect.DeepEqual(s2d, want2d) {
		t.Errorf("Slice2DWithValue: got %v, want %v", s2d, want2d)
	}

	s3d := Slice3DWithValue(uint8(23), 1, 1, 3)
	want3d := [][][]uint8{{{23, 23, 23}}}
	if !reflect.DeepEqual(s3d, want3d) {
		t.Errorf("Slice3DWithValue: got %v, want %v", s3d, want3d)
	}
}
