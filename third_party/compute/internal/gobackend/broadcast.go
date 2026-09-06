package gobackend

import (
	"iter"
	"slices"

	"github.com/gomlx/compute/internal/exceptions"
	"github.com/gomlx/compute/shapes"
)

// BroadcastIterator allows iteration over the flat indices of the target shape of a broadcast (where some axis
// dimensions grow)
//
// It is used by implicit broadcasting in binary ops and by the BroadcastInDim.
type BroadcastIterator struct {
	tgtSize     int
	srcStrides  []int
	isBroadcast []bool
	tgtDims     []int
}

// NewBroadcastIterator returns an iterator contructor over the flat indices of the target shape of a broadcast (where
// some axis dimensions grow).
//
// Pre-requisite: srcShape.Rank() == tgtShape.Rank().
//
// It is used by implicit broadcasting in binary ops and by the BroadcastInDim.
func NewBroadcastIterator(srcShape, tgtShape shapes.Shape) *BroadcastIterator {
	rank := srcShape.Rank()
	if rank != tgtShape.Rank() {
		exceptions.Panicf("broadcastIterator: rank mismatch srcShape=%s, tgtShape=%s", srcShape, tgtShape)
	}
	bi := &BroadcastIterator{
		tgtSize:     tgtShape.Size(),
		tgtDims:     slices.Clone(tgtShape.Dimensions),
		isBroadcast: make([]bool, rank),
		srcStrides:  srcShape.Strides(),
	}
	for axis := range rank {
		bi.isBroadcast[axis] = srcShape.Dimensions[axis] != tgtShape.Dimensions[axis]
	}
	return bi
}

// IterFlatIndices iterate over the source and target flat indices for broadcast.
// Notice since we are broadcasting, the target index will always be incremented by 1,
// while the source index may repeat (when it is being broadcast).
func (bi *BroadcastIterator) IterFlatIndices() iter.Seq2[int, int] {
	return func(yield func(srcIdx, tgtIdx int) bool) {
		rank := len(bi.tgtDims)
		perAxesIdx := make([]int, rank)
		srcFlatIdx := 0
		for dstFlatIdx := range bi.tgtSize {
			// Yield first.
			if !yield(srcFlatIdx, dstFlatIdx) {
				return
			}

			// Bump to next srcFlatIdx.
			srcFlatIdx++
			for axis := rank - 1; axis >= 0; axis-- {
				perAxesIdx[axis]++
				if perAxesIdx[axis] < bi.tgtDims[axis] {
					if bi.isBroadcast[axis] {
						// If we are broadcasting on this axis, we need to go back and repeat the same slice of the tensor.
						srcFlatIdx -= bi.srcStrides[axis]
					}
					break
				}
				perAxesIdx[axis] = 0
			}
		}
	}
}

// ZippedIndices of two BroadcastIterators.
type ZippedIndices struct {
	LHSFlatIdx, RHSFlatIdx int
	TgtFlatIdx             int
}

// ZipIterator allows iteration over the flat indices of the target shape of a broadcast (where some axis
// dimensions grow) for two input shapes (LHS and RHS).
//
// It implements the implicit broadcasting for binary ops like Add, Sub, Mul, Div, etc.
type ZipIterator struct {
	tgtSize        int
	tgtDims        []int
	lhsStrides     []int
	rhsStrides     []int
	lhsIsBroadcast []bool
	rhsIsBroadcast []bool
}

// NewZippedBroadcastIterator returns an iterator constructor over the flat indices of the target shape of a broadcast (where
// some axis dimensions grow) for two input shapes (LHS and RHS).
//
// Pre-requisite: lhsShape.Rank() == tgtShape.Rank() && rhsShape.Rank() == tgtShape.Rank().
//
// It implements the implicit broadcasting for binary ops like Add, Sub, Mul, Div, etc.
func NewZippedBroadcastIterator(lhsShape, rhsShape, tgtShape shapes.Shape) *ZipIterator {
	rank := tgtShape.Rank()
	if lhsShape.Rank() != rank || rhsShape.Rank() != rank {
		exceptions.Panicf("zippedBroadcastIterator: rank mismatch lhsShape=%s, rhsShape=%s, tgtShape=%s", lhsShape, rhsShape, tgtShape)
	}

	zi := &ZipIterator{
		tgtSize:        tgtShape.Size(),
		tgtDims:        slices.Clone(tgtShape.Dimensions),
		lhsStrides:     lhsShape.Strides(),
		rhsStrides:     rhsShape.Strides(),
		lhsIsBroadcast: make([]bool, rank),
		rhsIsBroadcast: make([]bool, rank),
	}
	for axis := range rank {
		zi.lhsIsBroadcast[axis] = lhsShape.Dimensions[axis] != tgtShape.Dimensions[axis]
		zi.rhsIsBroadcast[axis] = rhsShape.Dimensions[axis] != tgtShape.Dimensions[axis]
	}
	return zi
}

// EqualNodeData implements NodeDataComparable, so this can be used as NodeData -- the comparison
// is used for deduping repeated nodes during model building.
func (z *ZipIterator) EqualNodeData(other NodeDataComparable) bool {
	if z == nil && other == nil {
		return true
	}
	if z == nil || other == nil {
		return false
	}
	otherZ, ok := other.(*ZipIterator)
	if !ok {
		return false
	}
	return z.tgtSize == otherZ.tgtSize &&
		slices.Equal(z.tgtDims, otherZ.tgtDims) &&
		slices.Equal(z.lhsStrides, otherZ.lhsStrides) &&
		slices.Equal(z.rhsStrides, otherZ.rhsStrides) &&
		slices.Equal(z.lhsIsBroadcast, otherZ.lhsIsBroadcast) &&
		slices.Equal(z.rhsIsBroadcast, otherZ.rhsIsBroadcast)
}

// IterFlatIndices iterate over the lhs, rhs and target flat indices for broadcast.
// Notice since we are broadcasting, the target index will always be incremented by 1,
// while the source indices may repeat (when it is being broadcast).
func (zi *ZipIterator) IterFlatIndices() iter.Seq[ZippedIndices] {
	return func(yield func(ZippedIndices) bool) {
		rank := len(zi.tgtDims)
		perAxesIdx := make([]int, rank)
		lhsFlatIdx := 0
		rhsFlatIdx := 0

		for dstFlatIdx := range zi.tgtSize {
			// Yield first.
			if !yield(ZippedIndices{LHSFlatIdx: lhsFlatIdx, RHSFlatIdx: rhsFlatIdx, TgtFlatIdx: dstFlatIdx}) {
				return
			}

			// Bump to next
			lhsFlatIdx++
			rhsFlatIdx++
			for axis := rank - 1; axis >= 0; axis-- {
				perAxesIdx[axis]++
				if perAxesIdx[axis] < zi.tgtDims[axis] {
					if zi.lhsIsBroadcast[axis] {
						lhsFlatIdx -= zi.lhsStrides[axis]
					}
					if zi.rhsIsBroadcast[axis] {
						rhsFlatIdx -= zi.rhsStrides[axis]
					}
					break
				}
				perAxesIdx[axis] = 0
			}
		}
	}
}
