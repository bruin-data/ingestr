// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"iter"
	"slices"

	"github.com/pkg/errors"
)

// Strides returns the strides for each axis of the shape, assuming a "row-major" layout
// in memory, the one used everywhere in GoMLX.
//
// Notice the strides are **not in bytes**, but in indices.
//
// It panics if any dimension is dynamic (DynamicDim), see Shape.IsDynamic.
func (s Shape) Strides() (strides []int) {
	rank := s.Rank()
	if rank == 0 {
		return
	}
	strides = make([]int, rank)
	if s.IsZeroSize() {
		// Some axis is zero-dimension.
		return
	}
	currentStride := 1
	for axis := rank - 1; axis >= 0; axis-- {
		strides[axis] = currentStride
		dim := s.Dimensions[axis]
		if dim <= 0 {
			switch dim {
			case DynamicDim:
				panic(errors.Errorf("Shape.Strides() called on shape with dynamic dimensions: %s", s))
			case 0:
				for i := range strides {
					strides[i] = 0
				}
				return strides
			default:
				panic(errors.Errorf("Shape.Strides() called on shape with invalid negative dimension %d at axis %d: %s",
					dim, axis, s))
			}
		}
		currentStride *= dim
	}
	return
}

// Iter iterates sequentially over all possible indices of the given shape.
//
// It yields the flat index (counter) and a slice of indices for each axis.
//
// Panics if any dimension is dynamic (DynamicDim).
//
// To avoid allocating the slice of indices, the yielded indices is owned by the Iter() method:
// don't change it inside the loop.
func (s Shape) Iter() iter.Seq2[int, []int] {
	if s.IsDynamic() {
		panic(errors.Errorf("Shape.Iter() called on shape with dynamic dimensions: %s", s))
	}
	indices := make([]int, s.Rank())
	return s.IterOn(indices)
}

// IterOn iterates over all possible indices of the given shape.
//
// It yields the flat index (counter) and a slice of indices for each axis.
//
// Panics if any dimension is dynamic (DynamicDim).
//
// The iteration updates the indices on the given indices slice.
// During the iteration the caller shouldn't modify the slice of indices, otherwise it will lead to undefined behavior.
//
// It expects len(indices) == s.Rank(). It will panic otherwise.
func (s Shape) IterOn(indices []int) iter.Seq2[int, []int] {
	if s.IsDynamic() {
		panic(errors.Errorf("Shape.IterOn() called on shape with dynamic dimensions: %s", s))
	}
	if len(indices) != s.Rank() {
		panic(errors.Errorf("Shape.IterOn given len(indices) == %d, want it to be equal to the rank %d", len(indices), s.Rank()))
	}
	return func(yield func(int, []int) bool) {
		if !s.Ok() || s.IsTuple() {
			return // Iteration completed (vacuously true as no items were yielded)
		}

		rank := s.Rank()
		if rank == 0 {
			// Valid scalar: yield one empty index slice.
			_ = yield(0, indices)
			return
		}

		// Defensive check: if any dimension is non-positive, treat as an empty iteration.
		// shapes.Make should prevent this for validly constructed shapes.
		//
		// Also count the number of "non-trivial" axes: axes whose dimensions > 1.
		numNonTrivialAxes := 0
		for _, dimSize := range s.Dimensions {
			if dimSize <= 0 {
				return
			}
			if dimSize > 1 {
				numNonTrivialAxes++
			}
		}

		// Version 1: there are only trivial axes, there is only one value to iterate over.
		for i := range indices {
			indices[i] = 0
		}
		if numNonTrivialAxes == 0 {
			yield(0, indices)
			return
		}

		// Version 2: most axes are non-trivial, simply iterate over all of them:
		if rank > numNonTrivialAxes+2 {
			// Loop until all indices are generated.
			// This structure simulates an N-dimensional counter for the indices.
			flatIdx := 0
		v2Yielder:
			for {
				if !yield(flatIdx, indices) {
					return // Consumer requested to stop iteration.
				}
				flatIdx++

				// Increment indices to the next set of coordinates
				// (row-major order: the last index changes fastest).
				for axis := rank - 1; axis >= 0; axis-- {
					if s.Dimensions[axis] == 1 {
						// Nothing to iterate at this axis.
						continue
					}
					indices[axis]++
					if indices[axis] < s.Dimensions[axis] {
						// Successfully incremented this dimension; no carry-over needed.
						continue v2Yielder
					}
					// The current axis overflowed; reset it to 0 and
					// continue to increment the next higher-order dimension (carry-over).
					indices[axis] = 0
				}

				// If the axis is less than 0, all dimensions have been iterated through
				// (i.e., the first dimension also overflowed). Iteration is complete.
				break
			}
			return
		}

		// Version 3: many "trivial" axes (dimension == 1), create an indirection and only
		// iterate over the non-trivial axes:
		flatIdx := 0
		spatialAxes := make([]int, 0, numNonTrivialAxes)
		for axis, dim := range s.Dimensions {
			if dim > 1 {
				spatialAxes = append(spatialAxes, axis)
			}
		}
		slices.Reverse(spatialAxes) // We want to iterate over the last axis first.
	v3Yielder:
		for {
			if !yield(flatIdx, indices) {
				return // Consumer requested to stop iteration.
			}
			flatIdx++

			// Increment indices to the next set of coordinates
			// (row-major order: the last index changes fastest).
			for _, axis := range spatialAxes {
				indices[axis]++
				if indices[axis] < s.Dimensions[axis] {
					// Successfully incremented this dimension; no carry-over needed.
					continue v3Yielder
				}
				// The current axis overflowed; reset it to 0 and
				// continue to increment the next higher-order dimension (carry-over).
				indices[axis] = 0
			}

			// That was the last index.
			break
		}
	}
}

// IterOnAxes iterates over all possible indices of the given shape's axesToIterate.
//
// It yields the flat index and the update indices for all axes of the shape (not only the one in axes).
// The indices not pointed by axesToIterate are not touched.
//
// Panics if any dimension is dynamic (DynamicDim).
//
// Args:
//   - axesToIterate: axes of the shape to iterate over. They must be 0 <= axis < rank.
//     Axes not included here are not touched in the indices.
//   - strides: for the shape, as returned by Shape.Strides(). If nil, it will use the value returned by Shape.Strides.
//     If you are iterating over a shape many times, pre-calculating the strides saves some time.
//     If provided, it expects len(strides) == s.Rank(). It will panic otherwise.
//   - indices: slice that will be yielded during the iteration, it must have length equal to the shape's rank.
//     If it is nil, one will be allocated for the iteration.
//     The indices not in axesToIterate are left untouched, but they are used to calculate the flatIdx that is also yielded.
//     If provided, it expects len(indices) == s.Rank(). It will panic otherwise.
//
// During the iteration the caller shouldn't modify the slice of indices, otherwise it will lead to undefined behavior.
//
// Example:
//
//	// Create a shape with dimensions [2, 3, 4]
//	shape := Make(dtypes.F32, 2, 3, 4)
//
//	// Iterate over the first and last axes (0 and 2)
//	axesToIterate := []int{0, 2}
//	indices := make([]int, shape.Rank())
//	indices[1] = 1  // Fix middle axis to 1
//
//	// Each iteration will update indices[0] and indices[2], keeping indices[1]=1
//	for flatIdx, indices := range shape.IterOnAxes(axesToIterate, nil, indices) {
//	    fmt.Printf("flatIdx=%d, indices=%v\n", flatIdx, indices)
//	}
func (s Shape) IterOnAxes(axesToIterate, strides, indices []int) iter.Seq2[int, []int] {
	if s.IsDynamic() {
		panic(errors.Errorf("Shape.IterOnAxes() called on shape with dynamic dimensions: %s", s))
	}
	rank := s.Rank()

	// Validate and initialize strides
	if strides == nil {
		strides = s.Strides()
	} else if len(strides) != rank {
		panic(errors.Errorf("Shape.IterOnAxes given len(strides) == %d, want it to be equal to the rank %d", len(strides), rank))
	}

	// Validate and initialize indices
	if indices == nil {
		indices = make([]int, rank)
	} else if len(indices) != rank {
		panic(errors.Errorf("Shape.IterOnAxes given len(indices) == %d, but rank of shape is %d, shape is %s", len(indices), rank, s))
	}

	return func(yield func(int, []int) bool) {
		if !s.Ok() || s.IsTuple() {
			return // Iteration completed (vacuously true as no items were yielded)
		}

		if rank == 0 {
			// Valid scalar: yield one empty index slice.
			_ = yield(0, indices)
			return
		}

		// Defensive check: if any dimension to iterate is non-positive, treat as an empty iteration.
		for _, axis := range axesToIterate {
			if axis < 0 || axis >= rank {
				panic(errors.Errorf("Shape.IterOnAxes: invalid axis %d, must be 0 <= axis < rank (%d)", axis, rank))
			}
			if s.Dimensions[axis] <= 0 {
				return
			}
			// Initialize indices for the axesToIterate to 0.
			indices[axis] = 0
		}

		// Calculate initial flatIdx based on the current indices
		flatIdx := 0
		for axis := range rank {
			flatIdx += indices[axis] * strides[axis]
		}

	yielder:
		for {
			if !yield(flatIdx, indices) {
				return // Consumer requested to stop iteration.
			}

			// Increment indices to the next set of coordinates
			// (row-major order: the last index changes fastest).
			for axisIdx := len(axesToIterate) - 1; axisIdx >= 0; axisIdx-- {
				axis := axesToIterate[axisIdx]
				indices[axis]++
				flatIdx += strides[axis]
				if indices[axis] < s.Dimensions[axis] {
					// Successfully incremented this dimension; no carry-over needed.
					continue yielder
				}
				// The current axis overflowed; reset it to 0 and
				// continue to increment the next higher-order dimension (carry-over).
				flatIdx -= indices[axis] * strides[axis]
				indices[axis] = 0
			}

			// That was the last index.
			break
		}
	}
}
