package schemaevolution

import (
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// wideningOrder defines the type widening hierarchy.
// Higher index = wider type.
var wideningOrder = map[schema.DataType]int{
	schema.TypeBoolean:     0,
	schema.TypeInt8:        1,
	schema.TypeInt16:       2,
	schema.TypeInt32:       3,
	schema.TypeInt64:       4,
	schema.TypeFloat32:     5,
	schema.TypeFloat64:     6,
	schema.TypeDecimal:     7,
	schema.TypeDate:        8,
	schema.TypeTime:        9,
	schema.TypeTimestamp:   10,
	schema.TypeTimestampTZ: 11,
	schema.TypeBinary:      11,
	schema.TypeUUID:        11,
	schema.TypeInterval:    11,
	schema.TypeArray:       11,
	schema.TypeString:      12,
	schema.TypeJSON:        13,
}

// wideningPaths defines valid type widening paths.
// A type can be widened to any type in its path list.
var wideningPaths = map[schema.DataType][]schema.DataType{
	schema.TypeBoolean: {schema.TypeString, schema.TypeJSON},

	schema.TypeInt8:    {schema.TypeInt16, schema.TypeInt32, schema.TypeInt64, schema.TypeFloat64, schema.TypeDecimal, schema.TypeString, schema.TypeJSON},
	schema.TypeInt16:   {schema.TypeInt32, schema.TypeInt64, schema.TypeFloat64, schema.TypeDecimal, schema.TypeString, schema.TypeJSON},
	schema.TypeInt32:   {schema.TypeInt64, schema.TypeFloat64, schema.TypeDecimal, schema.TypeString, schema.TypeJSON},
	schema.TypeInt64:   {schema.TypeFloat64, schema.TypeDecimal, schema.TypeString, schema.TypeJSON},
	schema.TypeFloat32: {schema.TypeFloat64, schema.TypeString, schema.TypeJSON},
	schema.TypeFloat64: {schema.TypeDecimal, schema.TypeString, schema.TypeJSON},
	schema.TypeDecimal: {schema.TypeString, schema.TypeJSON},

	schema.TypeDate:        {schema.TypeTimestamp, schema.TypeTimestampTZ, schema.TypeString, schema.TypeJSON},
	schema.TypeTime:        {schema.TypeString, schema.TypeJSON},
	schema.TypeTimestamp:   {schema.TypeTimestampTZ, schema.TypeString, schema.TypeJSON},
	schema.TypeTimestampTZ: {schema.TypeString, schema.TypeJSON},

	schema.TypeString:   {schema.TypeJSON},
	schema.TypeBinary:   {schema.TypeString, schema.TypeJSON},
	schema.TypeUUID:     {schema.TypeString, schema.TypeJSON},
	schema.TypeInterval: {schema.TypeString, schema.TypeJSON},
	schema.TypeArray:    {schema.TypeJSON},
	schema.TypeJSON:     {},
}

// CanWiden returns true if the source type can be safely widened to the target type.
func CanWiden(from, to schema.DataType) bool {
	if from == to {
		return true
	}

	paths, ok := wideningPaths[from]
	if !ok {
		return false
	}

	for _, validTarget := range paths {
		if validTarget == to {
			return true
		}
	}
	return false
}

// GetWidenedType determines the best common type that can hold both src and dest types.
// Returns the widened type and a warning message if widening to string/JSON.
func GetWidenedType(src, dest schema.DataType) (schema.DataType, string) {
	if src == dest {
		return src, ""
	}

	srcOrder, srcOk := wideningOrder[src]
	destOrder, destOk := wideningOrder[dest]

	if !srcOk || !destOk {
		return schema.TypeJSON, "incompatible types widened to JSON"
	}

	if srcOrder > destOrder {
		if CanWiden(dest, src) {
			if src == schema.TypeString || src == schema.TypeJSON {
				return src, "type widened to " + src.String()
			}
			return src, ""
		}
	} else {
		if CanWiden(src, dest) {
			if dest == schema.TypeString || dest == schema.TypeJSON {
				return dest, "type widened to " + dest.String()
			}
			return dest, ""
		}
	}

	if CanWiden(src, schema.TypeString) && CanWiden(dest, schema.TypeString) {
		return schema.TypeString, "incompatible types widened to STRING"
	}

	return schema.TypeJSON, "incompatible types widened to JSON"
}

// MergeDecimalPrecision determines the precision and scale for merged decimal types.
// Preserves the maximum integer digits and scale, even when that exceeds the
// supported precision; callers that need an enforceable schema use the checked variant.
func MergeDecimalPrecision(src, dest schema.Column) (precision, scale int) {
	precision = src.Precision
	if dest.Precision > precision {
		precision = dest.Precision
	}
	scale = src.Scale
	if dest.Scale > scale {
		scale = dest.Scale
	}

	if precision == 0 {
		precision = 38
	}

	intDigits := precision - scale
	srcIntDigits := src.Precision - src.Scale
	destIntDigits := dest.Precision - dest.Scale
	if srcIntDigits > intDigits {
		intDigits = srcIntDigits
	}
	if destIntDigits > intDigits {
		intDigits = destIntDigits
	}

	precision = intDigits + scale
	return precision, scale
}

func MergeDecimalPrecisionChecked(src, dest schema.Column) (precision, scale int, err error) {
	precision, scale = MergeDecimalPrecision(src, dest)
	if precision > 38 {
		return 0, 0, fmt.Errorf(
			"decimal widening requires precision %d (integer digits %d + scale %d), maximum supported precision is 38",
			precision, precision-scale, scale,
		)
	}

	return precision, scale, nil
}
