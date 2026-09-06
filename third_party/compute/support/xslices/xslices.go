// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package xslices provide missing functionality to the slices package.
//
// It was created before the standard slices package, so some functionality may be duplicate.
package xslices

import (
	"cmp"
	"flag"
	"fmt"
	"math"
	"math/cmplx"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
)

// At takes an element at the given `index`, where `index` can be negative, in which case it takes from the end
// of the slice.
func At[T any](slice []T, index int) T {
	if index < 0 {
		index = len(slice) + index
	}
	return slice[index]
}

// SetAt sets an element at the given `index`, where `index` can be negative, in which case it takes from the end
// of the slice.
func SetAt[T any](slice []T, index int, value T) {
	if index < 0 {
		index = len(slice) + index
	}
	slice[index] = value
}

// Last returns the last element of a slice.
func Last[T any](slice []T) T {
	return At(slice, -1)
}

// SetLast sets the last element of a slice.
func SetLast[T any](slice []T, value T) {
	SetAt(slice, -1, value)
}

// Copy creates a new (shallow) copy of T. A shortcut to a call to `make` and then `copy`.
func Copy[T any](slice []T) []T {
	if len(slice) == 0 {
		return nil
	}
	slice2 := make([]T, len(slice))
	copy(slice2, slice)
	return slice2
}

// SlicesInDelta checks whether multidimensional slices s0 and s1 have the same shape and types,
// and that each of their values is within the given delta. Works with any numeric
// types.
//
// If delta <= 0, it checks for equality.
func SlicesInDelta(s0, s1 any, delta float64) bool {
	cmpFn := func(e0, e1 any) bool {
		// First, they must have both the same type.
		if reflect.TypeOf(e0).Kind() != reflect.TypeOf(e0).Kind() {
			return false
		}
		// If they are equal, return true.
		if reflect.DeepEqual(e0, e1) {
			return true
		}
		if delta <= 0 {
			return false
		}

		e0v := reflect.ValueOf(e0)
		e1v := reflect.ValueOf(e1)

		if reflect.TypeOf(e0).Kind() == reflect.Complex64 || reflect.TypeOf(e0).Kind() == reflect.Complex128 {
			// Complex numbers
			e0c, e1c := e0v.Complex(), e1v.Complex()
			ok := cmplx.Abs(e0c-e1c) <= delta
			if !ok {
				fmt.Printf("e0c=%v, e1c=%v, abs diff=%f\n", e0c, e1c, cmplx.Abs(e0c-e1c))
			}
			return ok
		}

		// Other numbers:
		deltaType := reflect.TypeFor[float64]()
		if !e0v.CanConvert(deltaType) {
			// Not numeric, cannot check for delta.
			return false
		}
		e0Float := e0v.Convert(deltaType).Float()
		e1Float := e1v.Convert(deltaType).Float()
		return math.Abs(e0Float-e1Float) <= delta
	}
	return DeepSliceCmp(s0, s1, cmpFn)
}

// Close is a comparison function that can be fed to DeepSliceCmp.
func Close[T interface{ float32 | float64 }](e0, e1 any) bool {
	e0v, ok := e0.(T)
	if !ok {
		fmt.Printf("*** Close[T] given (e0) incompatible type value %v for expected type %T\n", e0, e0v)
		return false
	}
	e1v, ok := e1.(T)
	if !ok {
		fmt.Printf("*** Close[T] given (e1) incompatible type value %v for expected type %T\n", e1, e1v)
		return false
	}
	if math.IsNaN(float64(e0v)) && math.IsNaN(float64(e1v)) {
		return true
	}
	diff := e0v - e1v
	if !(diff < Epsilon && diff > -Epsilon) {
		fmt.Printf("\t***Unmatching: %v, %v, diff=%v, epsilon=%v\n", e0, e1, diff, Epsilon)
	}
	return diff < Epsilon && diff > -Epsilon
}

// EqualAny is a comparison function that tests for exact equality and can be fed to DeepSliceCmp.
func EqualAny[T comparable](e0, e1 any) bool {
	e0v, ok := e0.(T)
	if !ok {
		return false
	}
	e1v, ok := e1.(T)
	if !ok {
		return false
	}
	return e0v == e1v
}

// FillSlice with fill the slice with the given value.
func FillSlice[T any](slice []T, value T) {
	// Apparently, the fastest way is by using copy.
	if len(slice) == 0 {
		return
	}
	slice[0] = value
	filled := 1
	for ; filled < len(slice); filled *= 2 {
		copy(slice[filled:], slice[:filled])
	}
}

// FillAnySlice fills a slice with the given value. Both are given as interface{} values,
// so it works for arbitrary underlying type values. Silently returns if the slice is not a
// slice or if the value is not the base type of the slice.
func FillAnySlice(slice any, value any) {
	// Check types.
	sliceT := reflect.TypeOf(slice)
	valueT := reflect.TypeOf(value)
	if sliceT.Kind() != reflect.Slice {
		return
	}
	if sliceT.Elem() != valueT {
		return
	}

	// Set the first value.
	sliceV := reflect.ValueOf(slice)
	valueV := reflect.ValueOf(value)
	items := sliceV.Len()
	if items == 0 {
		return
	}
	sliceV.Index(0).Set(valueV)

	// Recursively copy over value.
	for filled := 1; filled < items; filled *= 2 {
		from := sliceV.Slice(0, filled)
		to := sliceV.Slice(filled, items)
		reflect.Copy(to, from)
	}
}

// SliceToGoStr converts the slice to text, in a Go-syntax style that can be copy&pasted back to Go code. Similar
// to the "%#v" formatting option, but up to date for not repeating the inner-dimension slice types.
func SliceToGoStr(slice any) string {
	return fmt.Sprintf("%T%v", slice, recursiveSliceToGoStr(slice))
}

func recursiveSliceToGoStr(slice any) string {
	sliceT := reflect.TypeOf(slice)
	if sliceT.Kind() != reflect.Slice {
		return fmt.Sprintf("%v", slice)
	}
	sliceV := reflect.ValueOf(slice)
	parts := make([]string, 0, sliceV.Len())
	for ii := 0; ii < sliceV.Len(); ii++ {
		parts = append(parts, recursiveSliceToGoStr(sliceV.Index(ii).Interface()))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

// SliceWithValue creates a slice of given size filled with given value.
func SliceWithValue[T any](size int, value T) []T {
	s := make([]T, size)
	for ii := range s {
		s[ii] = value
	}
	return s
}

// Keys returns the keys of a map in the form of a slice.
func Keys[K comparable, V any](m map[K]V) []K {
	s := make([]K, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

// SortedKeys returns the sorted keys of a map in the form of a slice.
func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	s := Keys(m)
	slices.Sort(s)
	return s
}

// MultidimensionalSliceWithValue creates and initializes a multidimensional slice with the given value repeated everywhere.
// It can be cast to the appropriate type. Example:
//
// MultidimensionalSliceWithValue(0, 3, 3) -> [][]int{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}}
//
// All the data is allocated in one giant slice, and then partitioned accordingly.
func MultidimensionalSliceWithValue[T any](value T, dims ...int) any {
	size := 1
	for _, dim := range dims {
		size *= dim
	}
	if size == 0 {
		return nil
	}
	data := make([]T, size)
	dataPos := 0
	for ii := range data {
		data[ii] = value
	}
	result, _ := recursiveMDSlice(dims, data, dataPos)
	return result.Interface()
}

func recursiveMDSlice[T any](dims []int, data []T, dataPos int) (reflect.Value, int) {
	if len(dims) == 1 {
		// Slice into data
		slice := data[dataPos : dataPos+dims[0]]
		dataPos += dims[0]
		return reflect.ValueOf(slice), dataPos
	}

	// Create the first sub-slice and use its type to create the higher order slice.
	var subSlice reflect.Value
	subSlice, dataPos = recursiveMDSlice[T](dims[1:], data, dataPos)
	slice := reflect.MakeSlice(reflect.SliceOf(subSlice.Type()), dims[0], dims[0])
	slice.Index(0).Set(subSlice)

	// Now create the other sub-slices:
	for ii := 1; ii < dims[0]; ii++ {
		subSlice, dataPos = recursiveMDSlice[T](dims[1:], data, dataPos)
		slice.Index(ii).Set(subSlice)
	}
	return slice, dataPos
}

// Slice2DWithValue creates a 2D-slice of given dimensions filled with the given value.
func Slice2DWithValue[T any](value T, dim0, dim1 int) [][]T {
	return MultidimensionalSliceWithValue(value, dim0, dim1).([][]T)
}

// Slice3DWithValue creates a 3D-slice of given dimensions filled with the given value.
func Slice3DWithValue[T any](value T, dim0, dim1, dim2 int) [][][]T {
	return MultidimensionalSliceWithValue(value, dim0, dim1, dim2).([][][]T)
}

// Iota returns a slice of incremental int values, starting with start and of length len.
// Eg: Iota(3.0, 2) -> []float64{3.0, 4.0}
func Iota[T interface {
	constraints.Integer | constraints.Float
}](start T, len int) (slice []T) {
	slice = make([]T, len)
	for ii := range slice {
		slice[ii] = start + T(ii)
	}
	return
}

const Epsilon = 1e-4

// Map executes the given function sequentially for every element on in and returns a mapped slice.
func Map[In, Out any](in []In, fn func(e In) Out) (out []Out) {
	out = make([]Out, len(in))
	for ii, e := range in {
		out[ii] = fn(e)
	}
	return
}

// MapParallel executes the given function for every element of `in` with at most `runtime.NumCPU` goroutines. The
// execution order is not guaranteed, but in the end `out[ii] = fn(in[ii])` for every element.
func MapParallel[In, Out any](in []In, fn func(e In) Out) (out []Out) {
	if len(in) <= 1 {
		return Map(in, fn)
	}
	out = make([]Out, len(in))
	goroutines := min(runtime.NumCPU(), len(in))
	indices := make(chan int, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for ii := range indices {
				out[ii] = fn(in[ii])
			}
		})
	}
	for ii := range in {
		indices <- ii
	}
	close(indices)
	wg.Wait()
	return
}

// Max scans the slice and returns the maximum value.
func Max[T cmp.Ordered](slice []T) (max T) {
	if len(slice) == 0 {
		return
	}
	max = slice[0]
	for _, v := range slice {
		if max < v {
			max = v
		}
	}
	return
}

// Min scans the slice and returns the smallest value.
func Min[T cmp.Ordered](slice []T) (min T) {
	if len(slice) == 0 {
		return
	}
	min = slice[0]
	for _, v := range slice {
		if v < min {
			min = v
		}
	}
	return
}

// Pop the last element of the slice and returns slice with one less element.
// If the slice is empty, it returns the zero value for `T` and returns the slice unchanged.
func Pop[T any](slice []T) (T, []T) {
	var value T
	if len(slice) > 0 {
		value = slice[len(slice)-1]
		slice = slice[:len(slice)-1]
	}
	return value, slice
}

// DeepSliceCmp returns false if the slices given are of different shapes, or if the given cmpFn on each element
// returns false.
func DeepSliceCmp(s0, s1 any, cmpFn func(e0, e1 any) bool) bool {
	return recursiveDeepSliceCmp(reflect.ValueOf(s0), reflect.ValueOf(s1), cmpFn)
}

func recursiveDeepSliceCmp(s0, s1 reflect.Value, cmpFn func(e0, e1 any) bool) bool {
	if !s0.IsValid() || !s1.IsValid() {
		return false
	}
	if s0.Type().Kind() != s1.Type().Kind() {
		fmt.Printf("*** Kinds are different: %s, %s\n", s0.Type().Kind(), s1.Type().Kind())
		return false
	}
	if s0.Type().Kind() != reflect.Slice {
		return cmpFn(s0.Interface(), s1.Interface())
	}
	if s0.Len() != s1.Len() {
		return false
	}
	for ii := 0; ii < s0.Len(); ii++ {
		if !recursiveDeepSliceCmp(s0.Index(ii), s1.Index(ii), cmpFn) {
			return false
		}
	}
	return true
}

// Flag creates a flag for []T with the given name, description and default value.
// It takes as input a parser for an individual T value.
func Flag[T any](name string, defaultValue []T, usage string,
	parserFn func(valueStr string) (T, error)) *[]T {
	f := &genericSliceFlagImpl[T]{
		parsedSlice: defaultValue,
		parserFn:    parserFn,
	}
	flag.Var(f, name, usage)
	return &f.parsedSlice
}

// genericSliceFlagImpl implements flag.Value for a generic type.
type genericSliceFlagImpl[T any] struct {
	parsedSlice []T
	parserFn    func(valueStr string) (T, error)
}

func (f *genericSliceFlagImpl[T]) String() string {
	if len(f.parsedSlice) == 0 {
		return ""
	}
	parts := make([]string, len(f.parsedSlice))
	for ii, elem := range f.parsedSlice {
		v := reflect.ValueOf(elem)
		stringerType := reflect.TypeFor[fmt.Stringer]()
		if v.CanConvert(stringerType) {
			parts[ii] = v.Convert(stringerType).Interface().(fmt.Stringer).String()
		} else {
			parts[ii] = fmt.Sprintf("%v", elem)
		}
	}
	return strings.Join(parts, ",")
}

func (f *genericSliceFlagImpl[T]) Set(listStr string) error {
	if listStr == "" {
		f.parsedSlice = make([]T, 0)
		return nil
	}
	parts := strings.Split(listStr, ",")
	f.parsedSlice = make([]T, len(parts))
	var err error
	for ii, part := range parts {
		f.parsedSlice[ii], err = f.parserFn(part)
		if err != nil {
			return err
		}
	}
	return nil
}

// MustSlicesInRelData checks whether multidimensional slices s0 and s1 have the same shape and types,
// and that each of their values is within the given delta.
// It works with any float32 and float64 types.
//
// A value pair from s0 and s1 is considered to be within the delta if
// abs(v0 - v1) / max(abs(v0), abs(v1)) <= relDelta.
//
// An error is returned if values don't match, indicating the index of the error.
func MustSlicesInRelData[T float32 | float64](s0, s1 []T, relDelta float64) error {
	if len(s0) != len(s1) {
		return errors.Errorf("len(s0)==%d != len(s1)==%d", len(s0), len(s1))
	}
	for i, v0T := range s0 {
		v0 := float64(v0T)
		v1 := float64(s1[i])
		if math.Abs(v0-v1)/math.Abs(max(v0, v1)) > relDelta {
			return errors.Errorf("s0[%d]==%g != s1[%d]==%g (relDelta=%g > %g)",
				i, v0, i, v1, math.Abs(v0-v1)/math.Abs(max(v0, v1)), relDelta)
		}
	}
	return nil
}

// Gather returns a slice of T values by gathering the values indexed by indices.
// Eg: Gather([]int{1,3}, []float32{10, 20, 30, 40, 50}) -> []float32{20, 40}
func Gather[T any](values []T, indices []int) []T {
	slice := make([]T, len(indices))
	for ii := range slice {
		slice[ii] = values[indices[ii]]
	}
	return slice
}
