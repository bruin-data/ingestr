// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"reflect"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
)

// ToFlatAndShape converts a nested slice of any into a flat slice and a shape that can be fed into testBackend (or a BufferFromFlatData).
func ToFlatAndShape(v any) (any, shapes.Shape) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		dtype := dtypes.FromGoType(rv.Type())
		slice := reflect.MakeSlice(reflect.SliceOf(rv.Type()), 1, 1)
		slice.Index(0).Set(rv)
		return slice.Interface(), shapes.Make(dtype)
	}

	var dims []int
	curr := rv
	for curr.Kind() == reflect.Slice {
		dims = append(dims, curr.Len())
		if curr.Len() > 0 {
			curr = curr.Index(0)
		} else {
			break
		}
	}
	dtype := dtypes.FromGoType(curr.Type())

	flat := reflect.MakeSlice(reflect.SliceOf(curr.Type()), 0, 0)
	var flatten func(reflect.Value)
	flatten = func(val reflect.Value) {
		if val.Kind() == reflect.Slice {
			for i := 0; i < val.Len(); i++ {
				flatten(val.Index(i))
			}
		} else {
			flat = reflect.Append(flat, val)
		}
	}
	flatten(rv)
	return flat.Interface(), shapes.Make(dtype, dims...)
}

// FlattenSlice is a helper to convert a nested slice of any into a flat slice.
func FlattenSlice(v any) any {
	flat, _ := ToFlatAndShape(v)
	return flat
}

// ToBuffer converts a nested slice of any into a buffer for the given backend.
func ToBuffer(backend compute.Backend, v any) (compute.Buffer, error) {
	flat, shape := ToFlatAndShape(v)
	return backend.BufferFromFlatData(0, flat, shape)
}

// FromBuffer converts a [compute.Buffer] into a nested slice of the corresponding Go type.
func FromBuffer(backend compute.Backend, buf compute.Buffer) (any, error) {
	shape, err := buf.Shape()
	if err != nil {
		return nil, err
	}
	flat := dtypes.MakeAnySlice(shape.DType, shape.Size())
	if err := buf.ToFlatData(flat); err != nil {
		return nil, err
	}

	if shape.Rank() == 0 {
		return reflect.ValueOf(flat).Index(0).Interface(), nil
	}
	sliceTypes := make([]reflect.Type, shape.Rank())
	flatVal := reflect.ValueOf(flat)
	currType := flatVal.Type().Elem()
	for i := shape.Rank() - 1; i >= 0; i-- {
		currType = reflect.SliceOf(currType)
		sliceTypes[i] = currType
	}

	flatIdx := 0
	var build func(dimIdx int) reflect.Value
	build = func(dimIdx int) reflect.Value {
		if dimIdx == shape.Rank()-1 {
			dimSize := shape.Dimensions[dimIdx]
			slice := flatVal.Slice(flatIdx, flatIdx+dimSize)
			flatIdx += dimSize
			return slice
		}

		dimSize := shape.Dimensions[dimIdx]
		slice := reflect.MakeSlice(sliceTypes[dimIdx], dimSize, dimSize)
		for i := range dimSize {
			slice.Index(i).Set(build(dimIdx + 1))
		}
		return slice
	}

	return build(0).Interface(), nil
}
