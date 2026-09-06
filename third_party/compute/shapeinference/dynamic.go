// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapeinference

import (
	"slices"

	"github.com/gomlx/compute"
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/shapes"
	"github.com/gomlx/compute/support/sets"
	"github.com/pkg/errors"
)

// DynamicReshape calculates the output shape resulting from a DynamicReshape operation.
//
// DynamicDimensionSpec must be one of:
//   - Static: `Static >= 0`, `Name == ""`, `Value == nil`.
//   - Dynamic: `Name != ""`, `Value != nil`, `Static == 0`.
//   - Inferred: `Name != ""`, `Value == nil`, `Static == 0` (at most one inferred dimension).
//
// Static dimensions cannot be negative (e.g. -1 is invalid for Static).
func DynamicReshape(operand shapes.Shape, specs []compute.DynamicDimensionSpec) (output shapes.Shape, err error) {
	if len(specs) == 0 {
		return shapes.Invalid(), errors.Errorf("DynamicReshape() requires at least one dimension spec")
	}

	dims := make([]int, len(specs))
	axisNames := make([]string, len(specs))
	inferredIdx := -1

	for i, spec := range specs {
		if spec.Name != "" {
			if spec.Static != 0 {
				return shapes.Invalid(), errors.Errorf("DynamicReshape() spec at index %d has Name %q but Static is %d (must be 0 when Name is set)", i, spec.Name, spec.Static)
			}
			axisNames[i] = spec.Name
			dims[i] = shapes.DynamicDim

			if spec.Value == nil {
				// Dynamic inferred dimension with Name.
				if inferredIdx != -1 {
					return shapes.Invalid(), errors.Errorf("DynamicReshape() allows at most one inferred dimension, but found multiple at indices %d and %d", inferredIdx, i)
				}
				inferredIdx = i
			}
		} else {
			// Name == "" -> Static dimension
			if spec.Value != nil {
				return shapes.Invalid(), errors.Errorf("DynamicReshape() spec at index %d has a non-nil Value but Name is empty", i)
			}
			if spec.Static < 0 {
				return shapes.Invalid(), errors.Errorf("DynamicReshape() static dimension at index %d cannot be negative (%d)", i, spec.Static)
			}
			dims[i] = spec.Static
		}
	}

	// Case 1: Operand is static.
	if !operand.IsDynamic() {
		// Calculate product of all static dimensions in target.
		knownProduct := 1
		hasDynamicValue := false
		for _, spec := range specs {
			if spec.Name != "" {
				if spec.Value != nil {
					hasDynamicValue = true
				}
			} else {
				knownProduct *= spec.Static
			}
		}

		// If there are no dynamic Value nodes, and at most 1 inferred dim, we can resolve statically!
		if !hasDynamicValue {
			if inferredIdx != -1 {
				if knownProduct == 0 {
					return shapes.Invalid(), errors.Errorf("DynamicReshape() cannot infer dimension size when static dimensions multiply to 0")
				}
				if operand.Size()%knownProduct != 0 {
					return shapes.Invalid(), errors.Errorf("DynamicReshape() cannot reshape %s to specs %v: total size %d is not divisible by static product %d", operand, specs, operand.Size(), knownProduct)
				}
				dims[inferredIdx] = operand.Size() / knownProduct
				inferredIdx = -1 // resolved!
			}

			// If inferredIdx resolved (or was never present) and no dynamic values exist, return a static shape.
			if inferredIdx == -1 {
				output = shapes.Make(operand.DType, dims...)
				if operand.Size() != output.Size() {
					return shapes.Invalid(), errors.Errorf("DynamicReshape() shape mismatch: input %s (size %d) != output %s (size %d)", operand, operand.Size(), output, output.Size())
				}
				// If any axis names were provided, preserve them.
				if slices.ContainsFunc(axisNames, func(name string) bool { return name != "" }) {
					output = output.WithAxisNames(axisNames...)
				}
				return output, nil
			}
		}
	}

	// Case 2: Dynamic output shape.
	// Build dynamic shape with axis names.
	output = shapes.MakeDynamic(operand.DType, dims, axisNames)
	return output, nil
}

// DynamicBroadcastInDim calculates the output shape resulting from a DynamicBroadcastInDim operation.
//
// DynamicDimensionSpec must be one of:
//   - Static: `Static >= 0`, `Name == ""`, `Value == nil`.
//   - Dynamic with Value: `Name != ""`, `Value != nil`, `Static == 0`.
//   - Dynamic known axis: `Name != ""`, `Value == nil`, `Static == 0` (must match an operand dynamic axis name or known dynamic axis name).
func DynamicBroadcastInDim(operand shapes.Shape, broadcastAxes []int, specs []compute.DynamicDimensionSpec, knownDynamicAxisNames sets.Set[string]) (output shapes.Shape, err error) {
	if len(broadcastAxes) != operand.Rank() {
		return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: there must be exactly one broadcastAxes (%v) per axis in the operand (%s)",
			broadcastAxes, operand)
	}
	outputRank := len(specs)
	if outputRank < operand.Rank() {
		return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: output rank (%d) cannot be less than operand rank (%d)",
			outputRank, operand.Rank())
	}

	dims := make([]int, outputRank)
	axisNames := make([]string, outputRank)

	for i, spec := range specs {
		if spec.Name != "" {
			if spec.Static != 0 {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim() spec at index %d has Name %q but Static is %d (must be 0 when Name is set)", i, spec.Name, spec.Static)
			}
			axisNames[i] = spec.Name
			dims[i] = shapes.DynamicDim
		} else {
			if spec.Value != nil {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim() spec at index %d has a non-nil Value but Name is empty", i)
			}
			if spec.Static < 0 {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim() static dimension at index %d cannot be negative (%d)", i, spec.Static)
			}
			dims[i] = spec.Static
		}
	}

	// Verify broadcastAxes and check compatibility.
	lastAxis := -1
	preservedSet := sets.Make[int](len(broadcastAxes))
	for axisInOperand, axisInOutput := range broadcastAxes {
		if axisInOutput < 0 || axisInOutput >= outputRank {
			return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: broadcastAxes (%v) defines out-of-range index (%d-th value -> %d), must be between 0 and %d",
				broadcastAxes, axisInOperand, axisInOutput, outputRank-1)
		}
		if axisInOutput <= lastAxis {
			return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: broadcastAxes (%v) must be strictly increasing, but broadcastAxes[%d]=%d <= %d",
				broadcastAxes, axisInOperand, axisInOutput, lastAxis)
		}
		lastAxis = axisInOutput
		preservedSet.Insert(axisInOutput)

		inDim := operand.Dimensions[axisInOperand]
		outDim := dims[axisInOutput]
		spec := specs[axisInOutput]

		if inDim == shapes.DynamicDim {
			if outDim != shapes.DynamicDim {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: dynamic axis %d in operand shape %s mapped to broadcastAxes[%d]=%d must be preserved as dynamic in output",
					axisInOperand, operand, axisInOperand, axisInOutput)
			}
			nameOperand := operand.AxisName(axisInOperand)
			nameOutput := axisNames[axisInOutput]
			if nameOperand != nameOutput {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: dynamic axis %d in operand shape %s mapped to broadcastAxes[%d]=%d must preserve its axis name %q, got %q",
					axisInOperand, operand, axisInOperand, axisInOutput, nameOperand, nameOutput)
			}
		} else if inDim != 1 {
			if outDim == shapes.DynamicDim {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: cannot broadcast static dimension %d (> 1) at operand axis %d to dynamic output dimension",
					inDim, axisInOperand)
			}
			if inDim != outDim {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: broadcast dimension mismatch: operand axis %d has size %d, but output axis %d has size %d",
					axisInOperand, inDim, axisInOutput, outDim)
			}
		}

		if outDim == shapes.DynamicDim && inDim != shapes.DynamicDim {
			// Newly introduced dynamic axis by broadcasting dimension 1.
			if spec.Value == nil && (knownDynamicAxisNames == nil || !knownDynamicAxisNames.Has(spec.Name)) {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: cannot introduce unknown dynamic axis name %q without a dynamic Value or known axis name", spec.Name)
			}
		}
	}

	// Verify newly introduced dimensions (not in broadcastAxes).
	for axisInOutput, outDim := range dims {
		if !preservedSet.Has(axisInOutput) && outDim == shapes.DynamicDim {
			spec := specs[axisInOutput]
			if spec.Value == nil && (knownDynamicAxisNames == nil || !knownDynamicAxisNames.Has(spec.Name)) {
				return shapes.Invalid(), errors.Errorf("DynamicBroadcastInDim: cannot introduce unknown dynamic axis name %q without a dynamic Value or known axis name", spec.Name)
			}
		}
	}

	// If operand is static and all specs are static with no dynamic names or values:
	hasDynamic := operand.IsDynamic() || slices.Contains(dims, shapes.DynamicDim)
	if !hasDynamic {
		output = shapes.Make(operand.DType, dims...)
		if slices.ContainsFunc(axisNames, func(name string) bool { return name != "" }) {
			output = output.WithAxisNames(axisNames...)
		}
		return output, nil
	}

	output = shapes.MakeDynamic(operand.DType, dims, axisNames)
	return output, nil
}

// DynamicIota calculates the output shape resulting from a DynamicIota operation.
//
// DynamicDimensionSpec must be one of:
//   - Static: `Static >= 0`, `Name == ""`, `Value == nil`.
//   - Dynamic: `Name != ""`, `Value != nil`, `Static == 0`.
//   - Dynamic known axis: `Name != ""`, `Value == nil`, `Static == 0` (known dynamic axis name).
func DynamicIota(dtype dtypes.DType, iotaAxis int, specs []compute.DynamicDimensionSpec, knownDynamicAxisNames sets.Set[string]) (output shapes.Shape, err error) {
	if len(specs) == 0 {
		return shapes.Invalid(), errors.Errorf("DynamicIota: shape must have at least one dimension")
	}
	if iotaAxis < 0 || iotaAxis >= len(specs) {
		return shapes.Invalid(), errors.Errorf("DynamicIota: iotaAxis (%d) must be in the range [0,%d)", iotaAxis, len(specs)-1)
	}

	dims := make([]int, len(specs))
	axisNames := make([]string, len(specs))

	for i, spec := range specs {
		if spec.Name != "" {
			if spec.Static != 0 {
				return shapes.Invalid(), errors.Errorf("DynamicIota() spec at index %d has Name %q but Static is %d (must be 0 when Name is set)", i, spec.Name, spec.Static)
			}
			axisNames[i] = spec.Name
			dims[i] = shapes.DynamicDim
			if spec.Value == nil && (knownDynamicAxisNames == nil || !knownDynamicAxisNames.Has(spec.Name)) {
				return shapes.Invalid(), errors.Errorf("DynamicIota: cannot introduce unknown dynamic axis name %q without a dynamic Value or known axis name", spec.Name)
			}
		} else {
			if spec.Value != nil {
				return shapes.Invalid(), errors.Errorf("DynamicIota() spec at index %d has a non-nil Value but Name is empty", i)
			}
			if spec.Static < 0 {
				return shapes.Invalid(), errors.Errorf("DynamicIota() static dimension at index %d cannot be negative (%d)", i, spec.Static)
			}
			dims[i] = spec.Static
		}
	}

	hasDynamic := slices.Contains(dims, shapes.DynamicDim)
	if !hasDynamic {
		output = shapes.Make(dtype, dims...)
		if slices.ContainsFunc(axisNames, func(name string) bool { return name != "" }) {
			output = output.WithAxisNames(axisNames...)
		}
		return output, nil
	}

	output = shapes.MakeDynamic(dtype, dims, axisNames)
	return output, nil
}

// DynamicPad calculates the output shape resulting from a DynamicPad operation.
func DynamicPad(operand shapes.Shape, axesConfig ...compute.DynamicPadAxis) (output shapes.Shape, err error) {
	if !operand.Ok() {
		return shapes.Invalid(), errors.Errorf("DynamicPad: invalid operand shape %s", operand)
	}
	rank := operand.Rank()
	if len(axesConfig) > rank {
		return shapes.Invalid(), errors.Errorf("DynamicPad: too many DynamicPadAxis given (%d) for operand rank %d", len(axesConfig), rank)
	}

	output = operand.Clone()
	if output.AxisNames == nil && rank > 0 {
		output.AxisNames = make([]string, rank)
	}

	for axis, config := range axesConfig {
		isDynamicPad := config.StartValue != nil || config.EndValue != nil || config.InteriorValue != nil
		targetAxisName := config.TargetAxisName

		if config.InteriorValue == nil && config.Interior < 0 {
			return shapes.Invalid(), errors.Errorf("DynamicPad: interior padding must be non-negative, got %d for axis %d", config.Interior, axis)
		}

		dim := operand.Dimensions[axis]
		if isDynamicPad || dim == shapes.DynamicDim {
			output.Dimensions[axis] = shapes.DynamicDim
			if targetAxisName != "" {
				output.AxisNames[axis] = targetAxisName
			} else if dim != shapes.DynamicDim {
				// Static dimension became dynamic without targetAxisName -> anonymous.
				output.AxisNames[axis] = shapes.AnonymousAxis
			}
			continue
		}

		interiorPadding := 0
		if dim > 0 {
			interiorPadding = (dim - 1) * config.Interior
		}
		outDim := dim + config.Start + config.End + interiorPadding
		if outDim < 0 {
			return shapes.Invalid(), errors.Errorf("DynamicPad: resulting dimension for axis %d is negative (%d)", axis, outDim)
		}
		output.Dimensions[axis] = outDim
		if targetAxisName != "" {
			output.AxisNames[axis] = targetAxisName
		}
	}

	if !output.IsDynamic() && output.AxisNames != nil && !slices.ContainsFunc(output.AxisNames, func(name string) bool { return name != "" }) {
		output.AxisNames = nil
	}
	return output, nil
}

