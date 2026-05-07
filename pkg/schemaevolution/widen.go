package schemaevolution

import (
	"github.com/bruin-data/ingestr/pkg/schema"
)

// wideningOrder defines the type widening hierarchy.
// Higher index = wider type.
var wideningOrder = map[schema.DataType]int{
	schema.TypeBoolean:     0,
	schema.TypeInt16:       1,
	schema.TypeInt32:       2,
	schema.TypeInt64:       3,
	schema.TypeFloat32:     4,
	schema.TypeFloat64:     5,
	schema.TypeDecimal:     6,
	schema.TypeDate:        7,
	schema.TypeTime:        8,
	schema.TypeTimestamp:   9,
	schema.TypeTimestampTZ: 10,
	schema.TypeBinary:      10,
	schema.TypeUUID:        10,
	schema.TypeInterval:    10,
	schema.TypeArray:       10,
	schema.TypeString:      11,
	schema.TypeJSON:        12,
}

// wideningPaths defines valid type widening paths.
// A type can be widened to any type in its path list.
var wideningPaths = map[schema.DataType][]schema.DataType{
	schema.TypeBoolean: {schema.TypeString, schema.TypeJSON},

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
				return src, "type widened to " + dataTypeName(src)
			}
			return src, ""
		}
	} else {
		if CanWiden(src, dest) {
			if dest == schema.TypeString || dest == schema.TypeJSON {
				return dest, "type widened to " + dataTypeName(dest)
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
// Uses the larger precision and scale from both columns.
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
	if precision > 38 {
		precision = 38
	}

	return precision, scale
}

func dataTypeName(dt schema.DataType) string {
	switch dt {
	case schema.TypeBoolean:
		return "BOOLEAN"
	case schema.TypeInt16:
		return "INT16"
	case schema.TypeInt32:
		return "INT32"
	case schema.TypeInt64:
		return "INT64"
	case schema.TypeFloat32:
		return "FLOAT32"
	case schema.TypeFloat64:
		return "FLOAT64"
	case schema.TypeDecimal:
		return "DECIMAL"
	case schema.TypeString:
		return "STRING"
	case schema.TypeBinary:
		return "BINARY"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeTime:
		return "TIME"
	case schema.TypeTimestamp:
		return "TIMESTAMP"
	case schema.TypeTimestampTZ:
		return "TIMESTAMPTZ"
	case schema.TypeInterval:
		return "INTERVAL"
	case schema.TypeJSON:
		return "JSON"
	case schema.TypeUUID:
		return "UUID"
	case schema.TypeArray:
		return "ARRAY"
	default:
		return "UNKNOWN"
	}
}
