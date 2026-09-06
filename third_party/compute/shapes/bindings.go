// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package shapes

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gomlx/compute/support/xslices"
	"github.com/pkg/errors"
)

// AxisBindings maps named axis names to their concrete dimension values.
// Used at execution time to resolve dynamic shapes.
type AxisBindings map[string]int

// Key returns a deterministic string key suitable for use as a map key.
// Axis names are sorted alphabetically and formatted as "name=value,name=value".
func (b AxisBindings) Key() string {
	if len(b) == 0 {
		return ""
	}
	names := xslices.SortedKeys(b)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s=%d", name, b[name])
	}
	return strings.Join(parts, ",")
}

// Resolve returns a new Shape with all dynamic dimensions replaced by their bound values --
// except if the original shape was not dynamic, then it is returned as is (not a copy).
//
// The resolved shape retains its AxisNames for provenance/debugging.
//
// Returns an error a named dynamic axis has no corresponding binding, or if the binding is non-positive.
func (s Shape) Resolve(bindings AxisBindings) (Shape, error) {
	if !s.IsDynamic() {
		return s, nil
	}
	resolved := s.Clone()
	for i, dim := range resolved.Dimensions {
		if dim != DynamicDim {
			continue
		}
		name := resolved.AxisName(i)
		if name == "" {
			return Shape{}, errors.Errorf("Shape.Resolve: dynamic axis %d has no name and cannot be resolved: %s", i, s)
		}
		val, ok := resolveAxisName(name, bindings)
		if !ok {
			return Shape{}, errors.Errorf("Shape.Resolve: no binding for axis %q in shape %s", name, s)
		}
		if val <= 0 {
			return Shape{}, errors.Errorf("Shape.Resolve: binding for axis %q must be positive, got %d", name, val)
		}
		resolved.Dimensions[i] = val
	}
	return resolved, nil
}

// ResolvePartial resolves all dynamic dimensions for which a binding exists, leaving other dynamic dimensions untouched.
func (s Shape) ResolvePartial(bindings AxisBindings) (Shape, error) {
	if !s.IsDynamic() || len(bindings) == 0 {
		return s, nil
	}
	resolved := s.Clone()
	for i, dim := range resolved.Dimensions {
		if dim != DynamicDim {
			continue
		}
		name := resolved.AxisName(i)
		if name == "" {
			continue
		}
		val, ok := resolveAxisName(name, bindings)
		if !ok {
			continue
		}
		if val <= 0 {
			return Shape{}, errors.Errorf("Shape.ResolvePartial: binding for axis %q must be positive, got %d", name, val)
		}
		resolved.Dimensions[i] = val
	}
	return resolved, nil
}

// resolveAxisName attempts to resolve an axis name against bindings.
// It supports symbolic sum expressions starting with SymbolicAxisPrefix (e.g., "=10+a").
func resolveAxisName(name string, bindings AxisBindings) (int, bool) {
	if val, ok := bindings[name]; ok {
		return val, true
	}
	if strings.HasPrefix(name, SymbolicAxisPrefix) {
		expr := strings.TrimPrefix(name, SymbolicAxisPrefix)
		parts := strings.Split(expr, "+")
		total := 0
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return 0, false
			}
			if num, err := strconv.Atoi(part); err == nil {
				total += num
			} else if boundVal, ok := bindings[part]; ok {
				total += boundVal
			} else {
				return 0, false
			}
		}
		return total, true
	}
	return 0, false
}


// Extract axis bindings by comparing a template shape (with named dynamic axes)
// against a concrete shape with all dimensions known (presumably given during execution, when the concrete inputs
// are given).
//
// Returns an error if the shapes are incompatible: different ranks, different static dimensions,
// or inconsistent bindings where the same axis name maps to different concrete values.
func (b AxisBindings) Extract(template, concrete Shape) error {
	if template.Rank() != concrete.Rank() {
		return errors.Errorf("ExtractBindings: rank mismatch: template %s has rank %d, concrete %s has rank %d",
			template, template.Rank(), concrete, concrete.Rank())
	}
	for i := range template.Dimensions {
		templateDim := template.Dimensions[i]
		concreteDim := concrete.Dimensions[i]

		if concreteDim == DynamicDim {
			return errors.Errorf("ExtractBindings: concrete shape %s has a dynamic dimension at axis %d",
				concrete, i)
		}

		if templateDim == DynamicDim {
			name := template.AxisName(i)
			if name == "" {
				return errors.Errorf(
					"ExtractBindings: template %s has dynamic dimension at axis %d but no axis name",
					template, i)
			}
			if existing, ok := b[name]; ok && existing != concreteDim {
				return errors.Errorf("ExtractBindings: axis %q has conflicting values: %d vs %d",
					name, existing, concreteDim)
			}
			b[name] = concreteDim
		} else if templateDim != concreteDim {
			return errors.Errorf(
				"ExtractBindings: dimension %d mismatch: template has %d, concrete has %d",
				i, templateDim, concreteDim)
		}
	}
	return nil
}

// UnifyAxisName resolves the output axis name when combining two axes from different shapes.
//
// Rules:
//   - "" + "" = "" (both unnamed → unnamed)
//   - "name" + "" = "name" (one named → keep the name, provided name != AnonymousAxis)
//   - "" + "name" = "name" (one named → keep the name, provided name != AnonymousAxis)
//   - "name" + "name" = "name" (same name → keep it, provided name != AnonymousAxis)
//   - AnonymousAxis + any = error (AnonymousAxis never unifies/matches with anything)
//   - "a" + "b" = error (different names → conflict)
func UnifyAxisName(name1, name2 string) (string, error) {
	if name1 == AnonymousAxis || name2 == AnonymousAxis {
		return "", errors.Errorf("incompatible axis names: %q vs %q", name1, name2)
	}
	if name1 == "" {
		return name2, nil
	}
	if name2 == "" {
		return name1, nil
	}
	if name1 == name2 {
		return name1, nil
	}
	return "", errors.Errorf("incompatible axis names: %q vs %q", name1, name2)
}


// UnifyAxisNames unifies axis names from two shapes of the same rank.
// Returns the unified axis names, or error on name conflicts.
// Returns nil if neither shape has axis names.
func UnifyAxisNames(s1, s2 Shape) ([]string, error) {
	if s1.AxisNames == nil && s2.AxisNames == nil {
		return nil, nil
	}
	if s1.Rank() != s2.Rank() {
		return nil, errors.Errorf("UnifyAxisNames: rank mismatch: %d vs %d", s1.Rank(), s2.Rank())
	}
	if s1.AxisNames == nil {
		return s2.AxisNames, nil
	}
	if s2.AxisNames == nil {
		return s1.AxisNames, nil
	}
	result := make([]string, s1.Rank())
	for i := range result {
		unified, err := UnifyAxisName(s1.AxisNames[i], s2.AxisNames[i])
		if err != nil {
			return nil, errors.WithMessagef(err, "axis %d", i)
		}
		result[i] = unified
	}

	// If all names are empty, return nil for consistency.
	if slices.IndexFunc(result, func(s string) bool { return s != "" }) == -1 {
		return nil, nil
	}
	return result, nil
}
