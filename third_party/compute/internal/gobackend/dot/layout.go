package dot

import (
	"github.com/gomlx/compute/internal/gobackend"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// Layout for inputs of DotGeneral.
type Layout int

//go:generate go tool enumer -type Layout -trimprefix=Layout -output=gen_layout_enumer.go layout.go

const (
	// LayoutNonTransposed, "MatMul", "Row-Major" or "Normal-Transposed" layout,
	// with lhs shaped [B, N, K]; rhs shaped [B, K, M] and output shaped [B, N, M].
	// B is the optional batch axis; N is the lhs "cross" axis; M is the rhs
	// "cross" axis; K is the contracting axis.
	LayoutNonTransposed Layout = iota

	// LayoutTransposed with lhs shaped [B, N, K]; rhs shaped [B, M, K] and
	// output shaped [B, N, M].
	// B is the optional batch axis; N is the lhs "cross" axis; M is the rhs
	// "cross" axis; K is the contracting axis.
	LayoutTransposed

	// LayoutIncompatible indicates that the layout is incompatible with the
	// underlying basic DotGeneral algorithms, and will require a Reshape or
	// Transpose operation before the DotGeneral operation.
	LayoutIncompatible
)

// LayoutForDotGeneral returns the layout for DotGeneral given the shapes of the
// inputs and the axes to contract and batch over.
func LayoutForDotGeneral(lhsShape shapes.Shape, lhsContractingAxes, lhsBatchAxes []int,
	rhsShape shapes.Shape, rhsContractingAxes, rhsBatchAxes []int) Layout {
	// Require exactly one contracting axis.
	if len(lhsContractingAxes) != 1 || len(rhsContractingAxes) != 1 {
		return LayoutIncompatible
	}

	// Batch axes must match in count, must be 0 or 1, and must be leading and sequential (0, 1, 2, ...).
	numBatchAxes := len(lhsBatchAxes)
	if numBatchAxes > 1 {
		return LayoutIncompatible
	}
	if len(rhsBatchAxes) != numBatchAxes {
		return LayoutIncompatible
	}
	for i := range numBatchAxes {
		if lhsBatchAxes[i] != i || rhsBatchAxes[i] != i {
			return LayoutIncompatible
		}
	}

	lhsRank := lhsShape.Rank()
	rhsRank := rhsShape.Rank()

	// LHS contracting axis must be the last axis: [B..., N, K]
	if lhsContractingAxes[0] != lhsRank-1 {
		return LayoutIncompatible
	}

	// For LayoutNonTransposed (MatMul): RHS contracting axis is first after batch axes [B..., K, M]
	if rhsContractingAxes[0] == numBatchAxes {
		return LayoutNonTransposed
	}

	// For LayoutTransposed (Normal-Transposed): RHS contracting axis is last [B..., M, K]
	if rhsContractingAxes[0] == rhsRank-1 {
		return LayoutTransposed
	}

	return LayoutIncompatible
}

// AxisType represents the type of an axis, based on its function in the DotGeneral.
type AxisType int

const (
	AxisTypeCross AxisType = iota
	AxisTypeBatch
	AxisTypeContracting
)

type axisInfo struct {
	typ AxisType
	idx int // index in batchAxes or contractingAxes
}

func getAxisInfo(axis int, batchAxes, contractingAxes []int) axisInfo {
	for i, a := range batchAxes {
		if a == axis {
			return axisInfo{AxisTypeBatch, i}
		}
	}
	for i, a := range contractingAxes {
		if a == axis {
			return axisInfo{AxisTypeContracting, i}
		}
	}
	return axisInfo{AxisTypeCross, -1}
}

// MergeAxes checks if adjacent axes in lhs and rhs are used for the same purpose
// (batch, contracting or cross), and if so, merges them by reshaping.
// This simplifies the shape of the tensors, making it more likely that the
// operation can be matched to a fast execution path (like SmallMatMul or Packgemm).
//
// It returns the reshaped lhs and rhs, the new contracting and batch axes.
func MergeAxes(f *gobackend.Function,
	lhs *gobackend.Node, lhsContractingAxes, lhsBatchAxes []int,
	rhs *gobackend.Node, rhsContractingAxes, rhsBatchAxes []int) (
	newLhs *gobackend.Node, newLhsContractingAxes, newLhsBatchAxes []int,
	newRhs *gobackend.Node, newRhsContractingAxes, newRhsBatchAxes []int,
	err error) {

	lhsRank := lhs.Shape.Rank()
	rhsRank := rhs.Shape.Rank()

	// 1. Group adjacent mergable axes in LHS
	var lhsGroups [][]int
	if lhsRank > 0 {
		currentGroup := []int{0}
		for i := 1; i < lhsRank; i++ {
			prev := currentGroup[len(currentGroup)-1]
			infoPrev := getAxisInfo(prev, lhsBatchAxes, lhsContractingAxes)
			infoCurr := getAxisInfo(i, lhsBatchAxes, lhsContractingAxes)

			canMerge := false
			if infoPrev.typ == infoCurr.typ {
				switch infoPrev.typ {
				case AxisTypeCross:
					canMerge = true
				case AxisTypeBatch:
					// Batch axes must be adjacent in the axes list and in correct order.
					if infoCurr.idx == infoPrev.idx+1 {
						rhsPrev := rhsBatchAxes[infoPrev.idx]
						rhsCurr := rhsBatchAxes[infoCurr.idx]
						// The corresponding RHS axes must be physically adjacent and in order.
						if rhsCurr == rhsPrev+1 {
							canMerge = true
						}
					}
				case AxisTypeContracting:
					// Contracting axes must map to adjacent physical axes in RHS.
					rhsPrev := rhsContractingAxes[infoPrev.idx]
					rhsCurr := rhsContractingAxes[infoCurr.idx]
					if rhsCurr == rhsPrev+1 {
						canMerge = true
					}
				}
			}

			if canMerge {
				currentGroup = append(currentGroup, i)
			} else {
				lhsGroups = append(lhsGroups, currentGroup)
				currentGroup = []int{i}
			}
		}
		if len(currentGroup) > 0 {
			lhsGroups = append(lhsGroups, currentGroup)
		}
	}

	// 2. Group adjacent mergable axes in RHS
	var rhsGroups [][]int
	if rhsRank > 0 {
		currentGroup := []int{0}
		for j := 1; j < rhsRank; j++ {
			prev := currentGroup[len(currentGroup)-1]
			infoPrev := getAxisInfo(prev, rhsBatchAxes, rhsContractingAxes)
			infoCurr := getAxisInfo(j, rhsBatchAxes, rhsContractingAxes)

			canMerge := false
			if infoPrev.typ == infoCurr.typ {
				switch infoPrev.typ {
				case AxisTypeCross:
					canMerge = true
				case AxisTypeBatch:
					// Check if LHS merged them
					if infoCurr.idx == infoPrev.idx+1 {
						lhsPrev := lhsBatchAxes[infoPrev.idx]
						lhsCurr := lhsBatchAxes[infoCurr.idx]
						if lhsCurr == lhsPrev+1 {
							canMerge = true
						}
					}
				case AxisTypeContracting:
					// Check if LHS merged them
					lhsPrev := lhsContractingAxes[infoPrev.idx]
					lhsCurr := lhsContractingAxes[infoCurr.idx]
					if lhsCurr == lhsPrev+1 {
						canMerge = true
					}
				}
			}

			if canMerge {
				currentGroup = append(currentGroup, j)
			} else {
				rhsGroups = append(rhsGroups, currentGroup)
				currentGroup = []int{j}
			}
		}
		if len(currentGroup) > 0 {
			rhsGroups = append(rhsGroups, currentGroup)
		}
	}

	// Calculate new shapes
	newLhsDims := make([]int, len(lhsGroups))
	lhsOldToNew := make([]int, lhsRank)
	for newAxis, group := range lhsGroups {
		size := 1
		for _, oldAxis := range group {
			size *= lhs.Shape.Dimensions[oldAxis]
			lhsOldToNew[oldAxis] = newAxis
		}
		newLhsDims[newAxis] = size
	}

	newRhsDims := make([]int, len(rhsGroups))
	rhsOldToNew := make([]int, rhsRank)
	for newAxis, group := range rhsGroups {
		size := 1
		for _, oldAxis := range group {
			size *= rhs.Shape.Dimensions[oldAxis]
			rhsOldToNew[oldAxis] = newAxis
		}
		newRhsDims[newAxis] = size
	}

	// Reshape nodes if needed
	newLhs = lhs
	if len(newLhsDims) != lhsRank {
		reshaped, err := f.Reshape(lhs, newLhsDims...)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		newLhs = reshaped.(*gobackend.Node)
	}

	newRhs = rhs
	if len(newRhsDims) != rhsRank {
		reshaped, err := f.Reshape(rhs, newRhsDims...)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		newRhs = reshaped.(*gobackend.Node)
	}

	// Generate new batch and contracting axes mapping
	newLhsBatchAxes = make([]int, 0)
	for _, oldAxis := range lhsBatchAxes {
		newAxis := lhsOldToNew[oldAxis]
		if len(newLhsBatchAxes) == 0 || newLhsBatchAxes[len(newLhsBatchAxes)-1] != newAxis {
			newLhsBatchAxes = append(newLhsBatchAxes, newAxis)
		}
	}

	newRhsBatchAxes = make([]int, 0)
	for _, oldAxis := range rhsBatchAxes {
		newAxis := rhsOldToNew[oldAxis]
		if len(newRhsBatchAxes) == 0 || newRhsBatchAxes[len(newRhsBatchAxes)-1] != newAxis {
			newRhsBatchAxes = append(newRhsBatchAxes, newAxis)
		}
	}

	newLhsContractingAxes = make([]int, 0)
	newRhsContractingAxes = make([]int, 0)
	seenLhsContracting := make(map[int]bool)
	for i := range lhsContractingAxes {
		newLhsAxis := lhsOldToNew[lhsContractingAxes[i]]
		newRhsAxis := rhsOldToNew[rhsContractingAxes[i]]
		if !seenLhsContracting[newLhsAxis] {
			seenLhsContracting[newLhsAxis] = true
			newLhsContractingAxes = append(newLhsContractingAxes, newLhsAxis)
			newRhsContractingAxes = append(newRhsContractingAxes, newRhsAxis)
		}
	}

	return newLhs, newLhsContractingAxes, newLhsBatchAxes,
		newRhs, newRhsContractingAxes, newRhsBatchAxes, nil
}

// transposeSide is a helper to transpose one side to a specific ordering of (batch, cross, contracting).
func transposeSide(f *gobackend.Function, node *gobackend.Node, contractingAxes, batchAxes []int, layout Layout) (*gobackend.Node, []int, []int, error) {
	rank := node.Shape.Rank()
	isContracting := make([]bool, rank)
	for _, axis := range contractingAxes {
		isContracting[axis] = true
	}
	isBatch := make([]bool, rank)
	for _, axis := range batchAxes {
		isBatch[axis] = true
	}

	crossAxes := make([]int, 0, rank)
	for axis := 0; axis < rank; axis++ {
		if !isContracting[axis] && !isBatch[axis] {
			crossAxes = append(crossAxes, axis)
		}
	}

	var perm []int
	switch layout {
	case LayoutTransposed:
		// Batch, Cross, Contracting
		perm = append(perm, batchAxes...)
		perm = append(perm, crossAxes...)
		perm = append(perm, contractingAxes...)
	case LayoutNonTransposed:
		// Batch, Contracting, Cross
		perm = append(perm, batchAxes...)
		perm = append(perm, contractingAxes...)
		perm = append(perm, crossAxes...)
	default:
		return nil, nil, nil, errors.Errorf("unsupported layout %v in transposeSide", layout)
	}

	// Check if already in order
	alreadyInOrder := true
	for i, p := range perm {
		if p != i {
			alreadyInOrder = false
			break
		}
	}

	newNode := node
	if !alreadyInOrder {
		transposed, err := f.Transpose(node, perm...)
		if err != nil {
			return nil, nil, nil, err
		}
		newNode = transposed.(*gobackend.Node)
	}

	// Calculate new axes
	newBatchAxes := make([]int, len(batchAxes))
	for i := range batchAxes {
		newBatchAxes[i] = i
	}

	newContractingAxes := make([]int, len(contractingAxes))
	if layout == LayoutTransposed {
		start := len(batchAxes) + len(crossAxes)
		for i := range contractingAxes {
			newContractingAxes[i] = start + i
		}
	} else {
		start := len(batchAxes)
		for i := range contractingAxes {
			newContractingAxes[i] = start + i
		}
	}

	return newNode, newContractingAxes, newBatchAxes, nil
}

// TransposeToLayout attempts to transpose the axes of lhs and rhs to match a the given layout: LayoutTransposed or LayoutNonTranposed.
//
// Consider running MergeAxes first, it will make the transposing faster.
func TransposeToLayout(f *gobackend.Function,
	lhs *gobackend.Node, lhsContractingAxes, lhsBatchAxes []int,
	rhs *gobackend.Node, rhsContractingAxes, rhsBatchAxes []int,
	layout Layout) (
	newLhs *gobackend.Node, newLhsContractingAxes, newLhsBatchAxes []int,
	newRhs *gobackend.Node, newRhsContractingAxes, newRhsBatchAxes []int,
	err error) {

	newLhs, newLhsContractingAxes, newLhsBatchAxes, err = transposeSide(f, lhs, lhsContractingAxes, lhsBatchAxes, LayoutTransposed)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	newRhs, newRhsContractingAxes, newRhsBatchAxes, err = transposeSide(f, rhs, rhsContractingAxes, rhsBatchAxes, layout)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	// Merge axes
	newLhs, newLhsContractingAxes, newLhsBatchAxes,
		newRhs, newRhsContractingAxes, newRhsBatchAxes, err = MergeAxes(
		f, newLhs, newLhsContractingAxes, newLhsBatchAxes,
		newRhs, newRhsContractingAxes, newRhsBatchAxes)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	return newLhs, newLhsContractingAxes, newLhsBatchAxes,
		newRhs, newRhsContractingAxes, newRhsBatchAxes,
		nil
}
