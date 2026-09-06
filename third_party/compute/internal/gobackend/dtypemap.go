// Copyright 2023-2026 The GoMLX Authors. SPDX-License-Identifier: Apache-2.0

package gobackend

import (
	"github.com/gomlx/compute/dtypes"
	"github.com/gomlx/compute/dtypes/bfloat16"
	"github.com/gomlx/compute/dtypes/float16"
	"github.com/gomlx/compute/internal/exceptions"
	"github.com/pkg/errors"
)

const MaxDTypes = 32

// DTypeMap --------------------------------------------------------------------------------------------------

// DTypeMap manages registering of an arbitrary value per dtype.
// See NewDTypeMap for more information, including how to auto-generation registration for various
// classes of dtypes (ints, uints, floats, half).
type DTypeMap struct {
	Name     string
	Map      [MaxDTypes]any
	Priority [MaxDTypes]RegisterPriority
}

// NewDTypeMap creates a new DTypeMap.
//
// To automatically create code to register Go-generic functions to handle those dtypes, you can use the
// generator in internal/cmd/gobackend_dtypemap and add annotations (comments) before the
// DTypeMap variable declaration, like the example:
//
//	//gobackend:dtypemap MyGeneric ints,uints,floats,half
//	var myDTypeMap = gobackend.NewDTypeMap("myDTypeMap")
//
// This will automatically trigger the generation of registration calls in `gen_dtypemaps_registration.go` with
// content like:
//
//	myDTypeMap.Register(dtypes.Int8, gobackend.PriorityGeneric, MyGeneric[int8])
//	myDTypeMap.Register(dtypes.Int16, gobackend.PriorityGeneric, MyGeneric[int16])
//	myDTypeMap.Register(dtypes.Int32, gobackend.PriorityGeneric, MyGeneric[int32])
//	myDTypeMap.Register(dtypes.Int64, gobackend.PriorityGeneric, MyGeneric[int64])
//	myDTypeMap.Register(dtypes.Uint8, gobackend.PriorityGeneric, MyGeneric[uint8])
//	myDTypeMap.Register(dtypes.Uint16, gobackend.PriorityGeneric, MyGeneric[uint16])
//	myDTypeMap.Register(dtypes.Uint32, gobackend.PriorityGeneric, MyGeneric[uint32])
//	myDTypeMap.Register(dtypes.Uint64, gobackend.PriorityGeneric, MyGeneric[uint64])
//	myDTypeMap.Register(dtypes.Float32, gobackend.PriorityGeneric, MyGeneric[float32])
//	myDTypeMap.Register(dtypes.Float64, gobackend.PriorityGeneric, MyGeneric[float64])
//	myDTypeMap.Register(dtypes.BFloat16, gobackend.PriorityGeneric, MyGeneric[bfloat16.BFloat16])
//	myDTypeMap.Register(dtypes.Float16, gobackend.PriorityGeneric, MyGeneric[float16.Float16])
func NewDTypeMap(name string) *DTypeMap {
	return &DTypeMap{
		Name: name,
	}
}

// Get retrieves the value for the given dtype, or return an error if none was registered.
func (d *DTypeMap) Get(dtype dtypes.DType) (any, error) {
	if dtype >= MaxDTypes {
		return nil, errors.Errorf("dtype %s not supported by %s", dtype, d.Name)
	}
	value := d.Map[dtype]
	if value == nil {
		return nil, errors.Errorf("dtype %s not supported by %s -- "+
			"if you need it, consider creating an issue to add support in github.com/gomlx/gomlx",
			dtype, d.Name)
	}
	return value, nil
}

// Register a value for a dtype with the specified priority.
// If the priority is lower than the current priority for the dtype, the value is ignored.
func (d *DTypeMap) Register(dtype dtypes.DType, priority RegisterPriority, value any) {
	if dtype >= MaxDTypes {
		exceptions.Panicf("dtype %s not supported by %s", dtype, d.Name)
	}
	if priority < d.Priority[dtype] {
		// We have something registered with higher priority, ignore.
		return
	}
	d.Priority[dtype] = priority
	d.Map[dtype] = value
}

// DTypePairMap --------------------------------------------------------------------------------------------------

// DTypePairMap manages registering of an arbitrary value per dtype pair.
type DTypePairMap struct {
	Name     string
	Map      [MaxDTypes][MaxDTypes]any
	Priority [MaxDTypes][MaxDTypes]RegisterPriority
}

// NewDTypePairMap creates a new DTypePairMap.
//
// Similar to NewDTypeMap, you can add annotations to generate code to register generic functions. Example:
//
//	//gobackend:dtypemap_pair callImplementationGeneric ints same
//	//gobackend:dtypemap_pair callImplementationGeneric ints int32,int64
//	//gobackend:dtypemap_pair callImplementationGeneric uints same
//	//gobackend:dtypemap_pair callImplementationGeneric uints uint32,uint64
//	//gobackend:dtypemap_pair callImplementationGeneric floats floats
//	//gobackend:dtypemap_pair callImplementationGeneric half float32
//	callImplementationDTypePairMap = gobackend.NewDTypePairMap("callImplementationGeneric")
func NewDTypePairMap(name string) *DTypePairMap {
	return &DTypePairMap{
		Name: name,
	}
}

// Get retrieves the value for the given dtype pair, or return an error if none was registered.
func (d *DTypePairMap) Get(dtype1, dtype2 dtypes.DType) (any, error) {
	if dtype1 >= MaxDTypes || dtype2 >= MaxDTypes {
		return nil, errors.Errorf("dtypes %s or %s not supported by %s", dtype1, dtype2, d.Name)
	}
	value := d.Map[dtype1][dtype2]
	if value == nil {
		return nil, errors.Errorf("dtype pair (%s, %s) not supported by %s -- "+
			"if you need it, consider creating an issue to add support in github.com/gomlx/gomlx",
			dtype1, dtype2, d.Name)
	}
	return value, nil
}

// Register a value for a dtype pair with the specified priority.
// If the priority is lower than the current priority for the dtype pair, the value is ignored.
func (d *DTypePairMap) Register(dtype1, dtype2 dtypes.DType, priority RegisterPriority, value any) {
	if dtype1 >= MaxDTypes || dtype2 >= MaxDTypes {
		exceptions.Panicf("dtypes %s or %s not supported by %s", dtype1, dtype2, d.Name)
	}
	if priority < d.Priority[dtype1][dtype2] {
		// We have something registered with higher priority, ignore.
		return
	}
	d.Priority[dtype1][dtype2] = priority
	d.Map[dtype1][dtype2] = value
}

// Constraints --------------------------------------------------------------------------------------------------------

// SupportedTypesConstraints enumerates the types supported by the Go backend.
type SupportedTypesConstraints interface {
	bool | int8 | int16 | int32 | int64 | uint8 | uint16 | uint32 | uint64 | float32 | float64 |
		bfloat16.BFloat16 | float16.Float16
}

// PODNumericConstraints are used for generics for the Golang pod (plain-old-data) types.
// BFloat16 is not included because it is a specialized type, not natively supported by Go.
type PODNumericConstraints interface {
	int8 | int16 | int32 | int64 | uint8 | uint16 | uint32 | uint64 | float32 | float64
}

// PODSignedNumericConstraints are used for generics for the Golang pod (plain-old-data) types.
// BFloat16 and Float16 are not included because they are specialized types, not natively supported by Go.
type PODSignedNumericConstraints interface {
	int8 | int16 | int32 | int64 | float32 | float64
}

// PODIntegerConstraints are used for generics for the Golang pod (plain-old-data) types.
type PODIntegerConstraints interface {
	int8 | int16 | int32 | int64 | uint8 | uint16 | uint32 | uint64
}

// PODUnsignedConstraints are used for generics for the Golang pod (plain-old-data) types.
type PODUnsignedConstraints interface {
	uint8 | uint16 | uint32 | uint64
}

// PODFloatConstraints are used for generics for the Golang pod (plain-old-data) types.
// BFloat16 and Float16 are not included because they are specialized types, not natively supported by Go.
type PODFloatConstraints interface {
	float32 | float64
}

// PODBooleanConstraints is a simple placeholder for the gen_exec_binary.go generated code.
type PODBooleanConstraints interface {
	bool
}
