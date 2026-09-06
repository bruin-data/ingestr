// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"fmt"

	"github.com/gomlx/compute/dtypes"
	"github.com/pkg/errors"
)

// UncheckedAxis can be used in CheckDims or AssertDims functions for an axis
// whose dimension doesn't matter.
const UncheckedAxis = int(-1)

// HasShape is an interface for objects that have an associated Shape.
// `tensor.Tensor` (concrete tensor) and `graph.Node` (tensor representations in a
// computation graph), `context.Variable` and Shape itself implement the interface.
type HasShape interface {
	Shape() Shape
}

// CheckDims checks that the shape has the given dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It returns an error if the rank is different or if any of the dimensions don't match.
func (s Shape) CheckDims(dimensions ...int) error {
	if s.Rank() != len(dimensions) {
		return errors.Errorf("shape (%s) has incompatible rank %d (wanted %d)", s, s.Rank(), len(dimensions))
	}
	for ii, wantDim := range dimensions {
		if wantDim != -1 && s.Dimensions[ii] != wantDim {
			return errors.Errorf("shape (%s) axis %d has dimension %d, wanted %d (shape wanted=%v)", s, ii, s.Dimensions[ii], wantDim, dimensions)
		}
	}
	return nil
}

// Check that the shape has the given dtype, dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It returns an error if the dtype or rank is different or if any of the dimensions don't match.
func (s Shape) Check(dtype dtypes.DType, dimensions ...int) error {
	if dtype != s.DType {
		return errors.Errorf("shape (%s) has incompatible dtype %s (wanted %s)", s, s.DType, dtype)
	}
	return s.CheckDims(dimensions...)
}

// AssertDims checks that the shape has the given dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func (s Shape) AssertDims(dimensions ...int) {
	err := s.CheckDims(dimensions...)
	if err != nil {
		panic(fmt.Sprintf("shapes.AssertDims(%v): %+v", dimensions, err))
	}
}

// Assert checks that the shape has the given dtype, dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It panics if it doesn't match.
func (s Shape) Assert(dtype dtypes.DType, dimensions ...int) {
	err := s.Check(dtype, dimensions...)
	if err != nil {
		panic(fmt.Sprintf("shapes.Assert(%s, %v): %+v", dtype, dimensions, err))
	}
}

// CheckDims checks that the shape has the given dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It returns an error if the rank is different or any of the dimensions.
func CheckDims(shaped HasShape, dimensions ...int) error {
	return shaped.Shape().CheckDims(dimensions...)
}

// AssertDims checks that the shape has the given dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func AssertDims(shaped HasShape, dimensions ...int) {
	shaped.Shape().AssertDims(dimensions...)
}

// Assert checks that the shape has the given dtype, dimensions and rank. A value of -1 in
// dimensions means it can take any value and is not checked.
//
// It panics if it doesn't match.
func Assert(shaped HasShape, dtype dtypes.DType, dimensions ...int) {
	shaped.Shape().Assert(dtype, dimensions...)
}

// CheckRank checks that the shape has the given rank.
//
// It returns an error if the rank is different.
func (s Shape) CheckRank(rank int) error {
	if s.Rank() != rank {
		return errors.Errorf("shape (%s) has incompatible rank %d -- wanted %d", s, s.Rank(), rank)
	}
	return nil
}

// AssertRank checks that the shape has the given rank.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func (s Shape) AssertRank(rank int) {
	err := s.CheckRank(rank)
	if err != nil {
		panic(fmt.Sprintf("assertRank(%d): %+v", rank, err))
	}
}

// CheckRank checks that the shape has the given rank.
//
// It returns an error if the rank is different.
func CheckRank(shaped HasShape, rank int) error {
	return shaped.Shape().CheckRank(rank)
}

// AssertRank checks that the shape has the given rank.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func AssertRank(shaped HasShape, rank int) {
	shaped.Shape().AssertRank(rank)
}

// CheckScalar checks that the shape is a scalar.
//
// It returns an error if shape is not a scalar.
func (s Shape) CheckScalar() error {
	if !s.IsScalar() {
		return errors.Errorf("shape (%s) is not a scalar", s)
	}
	return nil
}

// AssertScalar checks that the shape is a scalar.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func (s Shape) AssertScalar() {
	err := s.CheckScalar()
	if err != nil {
		panic(fmt.Sprintf("AssertScalar(): %+v", err))
	}
}

// CheckScalar checks that the shape is a scalar.
//
// It returns an error if shape is not a scalar.
func CheckScalar(shaped HasShape) error {
	return shaped.Shape().CheckScalar()
}

// AssertScalar checks that the shape is a scalar.
//
// It panics if it doesn't match.
//
// See usage example in package shapes documentation.
func AssertScalar(shaped HasShape) {
	shaped.Shape().AssertScalar()
}
