// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

// Package shapes define Shape and DType and associated tools.
//
// Shape represents the shape (rank, dimensions for each axis, and DType) of either a Tensor or the expected
// shape of a node in a computation graph. DType indicates the data type for a Tensor's unit element.
//
// Optionally, the shape can also carry axes names.
//
// It also supports "dynamic shapes", where one or more axis has an undefined (DynamicDim == -1) dimension.
// Any dynamic dimension must be named (it must have a non-empty AxisName), so their values can be
// inferred when needed (see AxisBindings).
//
// ## Immutable Semantics
//
// The Shape object should be immutable semantic after creation: function that need to mutate shapes
// should clone first (see Shape.Clone), mutate, and return the updated (and henceforward immutable) shape.
// It's ok to simply copy shapes (shallow copy) if they are not meant to be mutated.
//
// ## Glossary
//
//   - Rank: number of axes (dimensions) of a Tensor.
//   - Axis: is the index of a dimension on a multidimensional array (tensor). Sometimes used
//     interchangeably with Dimension, but here we try to refer to a dimension index as "axis"
//     (plural axes), and its size as its dimension. An axis may also have an associated name.
//   - Dimension: the size of a multi-dimension Tensor in one of its axes. See the example below.
//   - DynamicDim (-1): special dimension value that indicates the axis is dynamic (unknown at graph building
//     and compilation time). Axes with dynamic dimensions must be named.
//   - DType: the data type of the unit element in a tensor. Enumeration defined in github.com/gomlx/compute/dtypes
//   - Scalar: is a shape where there are no axes (or dimensions), only a single value
//     of the associated DType.
//
// Example: The multi-dimensional array `[][]int32{{0, 1, 2}, {3, 4, 5}}` if converted to a Tensor
// would have shape `(int32)[2, 3]`. We say it has rank 2 (so 2 axes), axis 0 has
// dimension 2, and axis 1 has dimension 3. This shape could be created with
// `shapes.Make(int32, 2, 3)`.
//
// ## Creating a new shape:
//
//   - Make(dtype, dimensions...int): create a concrete (no dynamic axes) un-named shape.
//   - MakeDynamic(dtype, dimensions []int, axesNames []string): create a shape with (optional) dynamic axes
//     and axes names. Dynamic axes must have an associated name, while concrete axes are usually unnamed ("").
//   - MakeTuple(shapes...): internal use, create a shape that represents a tuple of shapes. Internal use, and subject
//     to change.
//
// # Iterators and Strides
//
// Shapes support several iteration facilities.
// See [Shape.Iter], [Shape.IterOn], [Shape.IterOnAxes] and [Shape.Strides].
package shapes

import (
	"encoding/gob"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/gotype"
	"github.com/pkg/errors"
)

// Shape represents the shape of either a Tensor or the expected shape
// of the value from a computation node.
//
// Use Make to create a new shape. See examples in the package documentation.
type Shape struct {
	// DType is the data type of the unit element in a tensor.
	DType dtypes.DType

	// Dimensions is the size of each axis. Its length determines the rank.
	// A value of DynamicDim (-1) indicates a dynamic axis whose size is unknown at graph build time.
	Dimensions []int

	// AxisNames holds optional names for each axis. nil means no axis names (the default).
	// When non-nil, len(AxisNames) must equal len(Dimensions).
	// An empty string "" means the axis is unnamed. A non-empty string names the axis.
	AxisNames []string `json:"axis_names,omitempty"`

	// TupleShapes is used if this Shape represents a tuple of elements.
	// Internal use only.
	TupleShapes []Shape `json:"tuple,omitempty"` // Shapes of the tuple, if this is a tuple.
}

// Make returns a Shape structure filled with the values given.
//
// See MakeDynamic for shapes with dynamic and/or named axes.
// See MakeTuple for tuple shapes.
func Make(dtype dtypes.DType, dimensions ...int) Shape {
	s := Shape{Dimensions: slices.Clone(dimensions), DType: dtype}
	for _, dim := range dimensions {
		if dim < 0 {
			panic(errors.Errorf(
				"shapes.Make(%s): cannot create a shape with an axis with dimension < 0 "+
					"-- see MakeDynamic to create dynamic shapes", s))
		}
	}
	return s
}

// Scalar returns a scalar Shape for the given type.
func Scalar[T gotype.Numeric]() Shape {
	return Shape{DType: dtypes.FromGenericsType[T]()}
}

// Invalid returns an invalid shape.
//
// Invalid().IsOk() == false.
func Invalid() Shape {
	return Shape{DType: dtypes.InvalidDType}
}

// Ok returns whether this is a valid Shape. A "zero" shape, that is just instantiating it with Shape{} will be invalid.
func (s Shape) Ok() bool { return s.DType != dtypes.InvalidDType || len(s.TupleShapes) > 0 }

// Rank of the shape, that is, the number of dimensions.
func (s Shape) Rank() int { return len(s.Dimensions) }

// IsScalar returns whether the shape represents a scalar, that is there are no dimensions (rank==0).
func (s Shape) IsScalar() bool { return s.Ok() && s.Rank() == 0 }

// Dim returns the dimension of the given axis. axis can take negative numbers, in which
// case it counts as starting from the end -- so axis=-1 refers to the last axis.
// Like with a slice indexing, it panics for an out-of-bound axis.
func (s Shape) Dim(axis int) int {
	adjustedAxis := axis
	if adjustedAxis < 0 {
		adjustedAxis += s.Rank()
	}
	if adjustedAxis < 0 || adjustedAxis > s.Rank() {
		panic(errors.Errorf("Shape.Dim(%d) out-of-bounds for rank %d (shape=%s)", axis, s.Rank(), s))
	}
	return s.Dimensions[adjustedAxis]
}

// Shape returns a shallow copy of itself. It implements the HasShape interface.
func (s Shape) Shape() Shape { return s }

// String implements stringer, pretty-prints the shape.
func (s Shape) String() string {
	if s.TupleSize() > 0 {
		parts := make([]string, 0, s.TupleSize())
		for _, tuple := range s.TupleShapes {
			parts = append(parts, tuple.String())
		}
		return fmt.Sprintf("Tuple<%s>", strings.Join(parts, ", "))
	}
	if s.Rank() == 0 {
		return fmt.Sprintf("(%s)", s.DType)
	}
	parts := make([]string, s.Rank())
	for i, dim := range s.Dimensions {
		if dim == DynamicDim {
			parts[i] = "?"
		} else {
			parts[i] = fmt.Sprintf("%d", dim)
		}
		if len(s.AxisNames) > i {
			name := s.AxisNames[i]
			if name != "" {
				parts[i] = fmt.Sprintf("%s=%s", name, parts[i])
			}
		}
	}
	return fmt.Sprintf("(%s)[%s]", s.DType, strings.Join(parts, ", "))
}

// Size returns the number of elements (not bytes) for this shape. It's the product of all dimensions.
//
// It panics if s.IsDynamic().
//
// For the number of bytes used to store this shape, see Shape.ByteSize.
func (s Shape) Size() (size int) {
	size = 1
	for _, d := range s.Dimensions {
		if d == DynamicDim {
			panic(errors.Errorf("Shape.Size() called on shape with dynamic dimensions: %s", s))
		}
		size *= d
	}
	return
}

// IsZeroSize returns whether any of the dimensions is zero, in which case
// it's an empty shape, with no data attached to it.
//
// Notice scalars are not zero in size -- they have size one, but rank zero.
func (s Shape) IsZeroSize() bool {
	return slices.Contains(s.Dimensions, 0)
}

// ByteSize returns the number of bytes used to store an array of the given shape.
func (s Shape) ByteSize() int64 {
	return int64(s.DType.SizeForDimensions(s.Dimensions...))
}

// Memory is an old alias to ByteSize, kept for backward compatibility.
//
// Deprecated: use ByteSize() instead.
func (s Shape) Memory() uintptr {
	return uintptr(s.ByteSize())
}

// MakeTuple returns a shape representing a tuple of elements with the given shapes.
func MakeTuple(elements []Shape) Shape {
	return Shape{DType: dtypes.InvalidDType, Dimensions: nil, TupleShapes: elements}
}

// IsTuple returns whether the shape represents a tuple.
func (s Shape) IsTuple() bool {
	return s.DType == dtypes.InvalidDType
}

// TupleSize returns the number of elements in the tuple, if it is a tuple.
func (s Shape) TupleSize() int {
	return len(s.TupleShapes)
}

// Equal compares two shapes for equality: dtype and dimensions are compared.
func (s Shape) Equal(s2 Shape) bool {
	if s.DType != s2.DType {
		return false
	}
	if s.IsTuple() {
		if s.TupleSize() != s2.TupleSize() {
			return false
		}
		for ii, element := range s.TupleShapes {
			if !element.Equal(s2.TupleShapes[ii]) {
				return false
			}
		}
		return true
	}
	if s.Rank() != s2.Rank() {
		return false
	}
	if s.IsScalar() {
		return true
	}
	// Compare dimensions.
	if !slices.Equal(s.Dimensions, s2.Dimensions) {
		return false
	}
	// Compare axis names.
	return axisNamesEqual(s.AxisNames, s2.AxisNames)
}

// EqualDimensions compares two shapes for equality of dimensions.
//
// DType and axis names are ignored.
func (s Shape) EqualDimensions(s2 Shape) bool {
	if s.IsTuple() {
		if !s2.IsTuple() {
			return false
		}
		if s.TupleSize() != s2.TupleSize() {
			return false
		}
		for ii, element := range s.TupleShapes {
			if !element.EqualDimensions(s2.TupleShapes[ii]) {
				return false
			}
		}
		return true
	}
	if s.Rank() != s2.Rank() {
		return false
	}
	if s.IsScalar() {
		return true
	}
	// For normal shapes just compare dimensions.
	return slices.Equal(s.Dimensions, s2.Dimensions)
}

// Clone returns a new deep copy of the shape.
func (s Shape) Clone() (s2 Shape) {
	s2.DType = s.DType
	s2.Dimensions = slices.Clone(s.Dimensions)
	s2.AxisNames = slices.Clone(s.AxisNames)
	if s.TupleSize() > 0 {
		s2.TupleShapes = make([]Shape, 0, len(s.TupleShapes))
		for _, subShape := range s.TupleShapes {
			s2.TupleShapes = append(s2.TupleShapes, subShape.Clone())
		}
	}
	return
}

// gobFormatV1 is a marker value used to distinguish the new gob format (with AxisNames)
// from the old format (without).
//
// In the old format, the corresponding gob-encoded position held numTuples (>= 0).
const gobFormatV1 = -1

// GobSerialize shape in binary format.
//
// Format v1 (current): DType, Dimensions, -1 (version marker), hasAxisNames (bool),
// [AxisNames if hasAxisNames], numTuples, [sub-shapes...].
//
// Old format: DType, Dimensions, numTuples, [sub-shapes...].
// GobDeserialize handles both formats for backward compatibility (old data → new code).
// Note: new-format data cannot be read by old code (the -1 marker would be interpreted
// as numTuples, causing a panic). This is forward-compatible only.
func (s Shape) GobSerialize(encoder *gob.Encoder) (err error) {
	enc := func(e any) {
		if err != nil {
			return
		}
		err = encoder.Encode(e)
		if err != nil {
			err = errors.Wrapf(err, "failed to serialize Shape %s", s)
		}
	}
	enc(s.DType)
	enc(s.Dimensions)

	// Encode the format version: currently gobFormatV1.
	enc(gobFormatV1)

	// Encode the axis names.
	hasAxisNames := s.AxisNames != nil
	enc(hasAxisNames)
	if hasAxisNames {
		enc(s.AxisNames)
	}

	// Encode the tuple shapes.
	enc(len(s.TupleShapes))
	if err != nil {
		return
	}
	for _, subShape := range s.TupleShapes {
		err = subShape.GobSerialize(encoder)
		if err != nil {
			return
		}
	}
	return
}

// GobDeserialize a Shape. Returns new Shape or an error.
// Handles both the old format (without a format version) and the v1 format (with AxisNames).
func GobDeserialize(decoder *gob.Decoder) (s Shape, err error) {
	dec := func(data any) {
		if err != nil {
			return
		}
		err = decoder.Decode(data)
		if err != nil {
			err = errors.Wrapf(err, "failed to deserialize Shape")
		}
	}
	dec(&s.DType)
	dec(&s.Dimensions)

	// Read version version or numTuples (for backward compat).
	// Old format: this int is numTuples (>= 0).
	// New format (v1): this int is -1, followed by hasAxisNames, [axisNames], numTuples.
	var version int
	dec(&version)

	var numTuples int

	switch {
	case version == gobFormatV1:
		// New format: read axis names, then numTuples.
		var hasAxisNames bool
		dec(&hasAxisNames)
		if hasAxisNames {
			dec(&s.AxisNames)
		}
		dec(&numTuples)

	case version >= 0:
		// Old vormat, where instead of version we have numTuples.
		numTuples = version

	default:
		err = errors.Errorf("unknown Shape format version %d !? (maybe input came from a newer GoMLX version?)", version)
	}

	if err != nil {
		return
	}
	s.TupleShapes = make([]Shape, numTuples)
	for ii := range s.TupleShapes {
		s.TupleShapes[ii], err = GobDeserialize(decoder)
		if err != nil {
			return
		}
	}
	return
}

// ConcatenateDimensions of two shapes. The resulting rank is the sum of both ranks. They must
// have the same dtype. If any of them is a scalar, the resulting shape will be a copy of the other.
// It doesn't work for Tuples.
func ConcatenateDimensions(s1, s2 Shape) (shape Shape) {
	if s1.IsTuple() || s2.IsTuple() {
		return
	}
	if s1.DType == dtypes.InvalidDType || s2.DType == dtypes.InvalidDType {
		return
	}
	if s1.DType != s2.DType {
		return
	}
	if s1.IsScalar() {
		return s2.Clone()
	} else if s2.IsScalar() {
		return s1.Clone()
	}
	shape.DType = s1.DType
	shape.Dimensions = make([]int, s1.Rank()+s2.Rank())
	copy(shape.Dimensions, s1.Dimensions)
	copy(shape.Dimensions[s1.Rank():], s2.Dimensions)
	if s1.AxisNames != nil || s2.AxisNames != nil {
		shape.AxisNames = make([]string, s1.Rank()+s2.Rank())
		if s1.AxisNames != nil {
			copy(shape.AxisNames, s1.AxisNames)
		}
		if s2.AxisNames != nil {
			copy(shape.AxisNames[s1.Rank():], s2.AxisNames)
		}
	}
	return
}

// FromAnyValue attempts to convert a Go "any" value to its expected shape.
// Accepted values are plain-old-data (POD) types (ints, floats, complex), slices (or multiple level of slices) of POD.
//
// It returns the expected shape.
//
// Example:
//
//	shape := shapes.FromAnyValue([][]float64{{0, 0}}) // Returns shape (Float64)[1 2]
func FromAnyValue(v any) (shape Shape, err error) {
	err = shapeForAnyValueRecursive(&shape, reflect.ValueOf(v), reflect.TypeOf(v))
	return
}

func shapeForAnyValueRecursive(shape *Shape, v reflect.Value, t reflect.Type) error {
	if t.Kind() != reflect.Slice {
		// If it's not a slice, it must be one of the supported scalar types.
		shape.DType = dtypes.FromGoType(t)
		if shape.DType == dtypes.InvalidDType {
			return errors.Errorf("cannot convert type %q to a valid GoMLX shape (maybe type not supported yet?)", t)
		}
		return nil
	}

	// Slice: recurse into its element type (again slices or a supported POD).
	t = t.Elem()
	shape.Dimensions = append(shape.Dimensions, v.Len())
	shapePrefix := shape.Clone()

	// The first element is the reference
	if v.Len() == 0 {
		return errors.Errorf("value with empty slice not valid for shape conversion: %T: %v -- it wouldn't be possible to figure out the inner dimensions", v.Interface(), v)
	}
	v0 := v.Index(0)
	err := shapeForAnyValueRecursive(shape, v0, t)
	if err != nil {
		return err
	}

	// Test that other elements have the same shape as the first one.
	for ii := 1; ii < v.Len(); ii++ {
		shapeTest := shapePrefix.Clone()
		err = shapeForAnyValueRecursive(&shapeTest, v.Index(ii), t)
		if err != nil {
			return err
		}
		if !shape.Equal(shapeTest) {
			return fmt.Errorf("sub-slices have irregular shapes, found shapes %q, and %q", shape, shapeTest)
		}
	}
	return nil
}
