package shapeinference

import (
	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/pkg/errors"
)

// DotGeneral returns the output shape of a DotGeneral operation given the shapes of the
// inputs and the axes to contract and batch over.
//
// The resulting shape is [batchIndices..., <lhs cross indices...>, <rhs cross indices...>], the
// indices come in the order they were provided.
// The output dtype is by default the same as the input dtype unless config.OutputDType is set.
func DotGeneral(
	lhs shapes.Shape, lhsContractingAxes, lhsBatchAxes []int,
	rhs shapes.Shape, rhsContractingAxes, rhsBatchAxes []int,
	config compute.DotGeneralConfig) (shapes.Shape, error) {
	var output shapes.Shape

	if lhs.DType != rhs.DType {
		return output, errors.Errorf("DotGeneral requires inputs with the same DType, got lhs=%s and rhs=%s", lhs.DType, rhs.DType)
	}

	if len(lhsContractingAxes) != len(rhsContractingAxes) {
		return output, errors.Errorf("DotGeneral number of contracting axes for lhs (%d) doesn't match rhs (%d)",
			len(lhsContractingAxes), len(rhsContractingAxes))
	}
	if len(lhsBatchAxes) != len(rhsBatchAxes) {
		return output, errors.Errorf("DotGeneral number of batch axes for lhs (%d) doesn't match rhs (%d)",
			len(lhsBatchAxes), len(rhsBatchAxes))
	}

	isLHSUsed := make([]bool, lhs.Rank())
	for i, axis := range lhsContractingAxes {
		if axis < 0 || axis >= lhs.Rank() {
			return output, errors.Errorf("DotGeneral lhs contracting axis %d out of bounds for rank %d", axis, lhs.Rank())
		}
		if isLHSUsed[axis] {
			return output, errors.Errorf("DotGeneral lhs axis %d used more than once in contracting/batch", axis)
		}
		isLHSUsed[axis] = true

		rAxis := rhsContractingAxes[i]
		if rAxis < 0 || rAxis >= rhs.Rank() {
			return output, errors.Errorf("DotGeneral rhs contracting axis %d out of bounds for rank %d", rAxis, rhs.Rank())
		}
		lDim := lhs.Dimensions[axis]
		rDim := rhs.Dimensions[rAxis]
		if lDim == shapes.DynamicDim && rDim == shapes.DynamicDim {
			if !shapes.AxisNameEqual(lhs.AxisName(axis), rhs.AxisName(rAxis)) {
				return output, errors.Errorf("DotGeneral contracting axis #%d has different axis names (axis name conflict) for lhs (%q) and rhs (%q)", i, lhs.AxisName(axis), rhs.AxisName(rAxis))
			}
		} else if lDim != shapes.DynamicDim && rDim != shapes.DynamicDim && lDim != rDim {
			return output, errors.Errorf("DotGeneral contracting dimensions do not match: lhs[%d]=%d, rhs[%d]=%d", axis, lDim, rAxis, rDim)
		}
	}

	for i, axis := range lhsBatchAxes {
		if axis < 0 || axis >= lhs.Rank() {
			return output, errors.Errorf("DotGeneral lhs batch axis %d out of bounds for rank %d", axis, lhs.Rank())
		}
		if isLHSUsed[axis] {
			return output, errors.Errorf("DotGeneral lhs axis %d used more than once in contracting/batch", axis)
		}
		isLHSUsed[axis] = true

		rAxis := rhsBatchAxes[i]
		if rAxis < 0 || rAxis >= rhs.Rank() {
			return output, errors.Errorf("DotGeneral rhs batch axis %d out of bounds for rank %d", rAxis, rhs.Rank())
		}
		lDim := lhs.Dimensions[axis]
		rDim := rhs.Dimensions[rAxis]
		if lDim == shapes.DynamicDim && rDim == shapes.DynamicDim {
			if !shapes.AxisNameEqual(lhs.AxisName(axis), rhs.AxisName(rAxis)) {
				return output, errors.Errorf("DotGeneral batch axis #%d has different axis names (axis name conflict) for lhs (%q) and rhs (%q)", i, lhs.AxisName(axis), rhs.AxisName(rAxis))
			}
		} else if lDim != shapes.DynamicDim && rDim != shapes.DynamicDim && lDim != rDim {
			return output, errors.Errorf("DotGeneral batch dimensions do not match: lhs[%d]=%d, rhs[%d]=%d", axis, lDim, rAxis, rDim)
		}
	}


	isRHSUsed := make([]bool, rhs.Rank())
	for _, axis := range rhsContractingAxes {
		if isRHSUsed[axis] {
			return output, errors.Errorf("DotGeneral rhs axis %d used more than once in contracting/batch", axis)
		}
		isRHSUsed[axis] = true
	}
	for _, axis := range rhsBatchAxes {
		if isRHSUsed[axis] {
			return output, errors.Errorf("DotGeneral rhs axis %d used more than once in contracting/batch", axis)
		}
		isRHSUsed[axis] = true
	}

	outDType := lhs.DType
	if config.OutputDType != dtypes.InvalidDType {
		outDType = config.OutputDType
	}

	outputRank := len(lhsBatchAxes) + (lhs.Rank() - len(lhsBatchAxes) - len(lhsContractingAxes)) + (rhs.Rank() - len(rhsBatchAxes) - len(rhsContractingAxes))
	dims := make([]int, 0, outputRank)
	var axisNames []string
	if lhs.AxisNames != nil || rhs.AxisNames != nil {
		axisNames = make([]string, 0, outputRank)
	}

	for i, axis := range lhsBatchAxes {
		lDim := lhs.Dimensions[axis]
		rDim := rhs.Dimensions[rhsBatchAxes[i]]
		if lDim == shapes.DynamicDim || rDim == shapes.DynamicDim {
			dims = append(dims, shapes.DynamicDim)
		} else {
			dims = append(dims, lDim)
		}
		if axisNames != nil {
			lName := lhs.AxisName(axis)
			rName := rhs.AxisName(rhsBatchAxes[i])
			unified, err := shapes.UnifyAxisName(lName, rName)
			if err != nil {
				return output, errors.Wrapf(err, "axis name conflict in DotGeneral batch axis %d", i)
			}
			axisNames = append(axisNames, unified)
		}
	}
	for axis := 0; axis < lhs.Rank(); axis++ {
		if !isLHSUsed[axis] {
			dims = append(dims, lhs.Dimensions[axis])
			if axisNames != nil {
				axisNames = append(axisNames, lhs.AxisName(axis))
			}
		}
	}
	for axis := 0; axis < rhs.Rank(); axis++ {
		if !isRHSUsed[axis] {
			dims = append(dims, rhs.Dimensions[axis])
			if axisNames != nil {
				axisNames = append(axisNames, rhs.AxisName(axis))
			}
		}
	}

	output = shapes.Shape{
		DType:      outDType,
		Dimensions: dims,
		AxisNames:  axisNames,
	}
	return output, nil
}
