package schemainfer

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// typePriority defines the priority for type promotion.
// Higher priority types can absorb lower priority types.
var typePriority = map[arrow.Type]int{
	arrow.BOOL:         1,
	arrow.INT8:         2,
	arrow.INT16:        3,
	arrow.INT32:        4,
	arrow.INT64:        5,
	arrow.UINT8:        6,
	arrow.UINT16:       7,
	arrow.UINT32:       8,
	arrow.UINT64:       9,
	arrow.FLOAT16:      10,
	arrow.FLOAT32:      11,
	arrow.FLOAT64:      12,
	arrow.DECIMAL128:   13,
	arrow.DECIMAL256:   14,
	arrow.DATE32:       20,
	arrow.DATE64:       21,
	arrow.TIME32:       22,
	arrow.TIME64:       23,
	arrow.TIMESTAMP:    24,
	arrow.EXTENSION:    50,  // Extension types (like JSON) have their own priority
	arrow.STRING:       100, // String is the ultimate fallback
	arrow.LARGE_STRING: 101,
	arrow.BINARY:       102,
}

// MergeArrowTypes merges two Arrow types using promotion rules.
// When types conflict, the result is promoted to a type that can hold both.
func MergeArrowTypes(existing, new arrow.DataType) (arrow.DataType, error) {
	// Same type - no merge needed
	if arrow.TypeEqual(existing, new) {
		return existing, nil
	}

	// Unknown type is a placeholder and should yield to concrete types.
	if isUnknownType(existing) {
		return new, nil
	}
	if isUnknownType(new) {
		return existing, nil
	}

	// Handle JSON extension types - two JSON types merge to JSON
	existingIsJSON := isJSONType(existing)
	newIsJSON := isJSONType(new)
	if existingIsJSON && newIsJSON {
		return schema.JSONArrowType, nil
	}
	// JSON + any other type = JSON (JSON can hold any structure)
	if existingIsJSON || newIsJSON {
		return schema.JSONArrowType, nil
	}

	existingPriority, existingOk := typePriority[existing.ID()]
	newPriority, newOk := typePriority[new.ID()]

	// If either type is not in our priority map, fall back to string
	if !existingOk || !newOk {
		return arrow.BinaryTypes.String, nil
	}

	// If either is string, result is string
	if existing.ID() == arrow.STRING || new.ID() == arrow.STRING ||
		existing.ID() == arrow.LARGE_STRING || new.ID() == arrow.LARGE_STRING {
		return arrow.BinaryTypes.String, nil
	}

	// Handle numeric type promotions
	if isNumericType(existing) && isNumericType(new) {
		return promoteNumericTypes(existing, new)
	}

	// Handle temporal type promotions
	if isTemporalType(existing) && isTemporalType(new) {
		return promoteTemporalTypes(existing, new)
	}

	// For incompatible types (e.g., int and date), promote to string
	if existingPriority >= newPriority {
		if existingPriority >= 100 { // String or binary
			return existing, nil
		}
	} else {
		if newPriority >= 100 {
			return new, nil
		}
	}

	// Default: promote to string for safety
	return arrow.BinaryTypes.String, nil
}

// isNumericType returns true if the type is a numeric type.
func isNumericType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64,
		arrow.DECIMAL128, arrow.DECIMAL256:
		return true
	default:
		return false
	}
}

// isTemporalType returns true if the type is a temporal type.
func isTemporalType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.DATE32, arrow.DATE64, arrow.TIME32, arrow.TIME64, arrow.TIMESTAMP:
		return true
	default:
		return false
	}
}

// promoteNumericTypes promotes two numeric types to a common type.
func promoteNumericTypes(a, b arrow.DataType) (arrow.DataType, error) {
	// If either is floating point, result is floating point
	if isFloatingPoint(a) || isFloatingPoint(b) {
		// Use the wider floating point type
		if a.ID() == arrow.FLOAT64 || b.ID() == arrow.FLOAT64 {
			return arrow.PrimitiveTypes.Float64, nil
		}
		return arrow.PrimitiveTypes.Float32, nil
	}

	// If either is decimal, result is decimal
	if a.ID() == arrow.DECIMAL128 || b.ID() == arrow.DECIMAL128 ||
		a.ID() == arrow.DECIMAL256 || b.ID() == arrow.DECIMAL256 {
		// Use default precision/scale for merged decimals
		return &arrow.Decimal128Type{Precision: 38, Scale: 9}, nil
	}

	// Both are integers - use the wider type
	aBits := integerBits(a)
	bBits := integerBits(b)
	maxBits := aBits
	if bBits > maxBits {
		maxBits = bBits
	}

	switch maxBits {
	case 8:
		return arrow.PrimitiveTypes.Int16, nil // Promote to avoid overflow
	case 16:
		return arrow.PrimitiveTypes.Int32, nil
	case 32:
		return arrow.PrimitiveTypes.Int64, nil
	default:
		return arrow.PrimitiveTypes.Int64, nil
	}
}

// isFloatingPoint returns true if the type is a floating point type.
func isFloatingPoint(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		return true
	default:
		return false
	}
}

// integerBits returns the bit width of an integer type.
func integerBits(dt arrow.DataType) int {
	switch dt.ID() {
	case arrow.INT8, arrow.UINT8:
		return 8
	case arrow.INT16, arrow.UINT16:
		return 16
	case arrow.INT32, arrow.UINT32:
		return 32
	case arrow.INT64, arrow.UINT64:
		return 64
	default:
		return 64
	}
}

// promoteTemporalTypes promotes two temporal types to a common type.
func promoteTemporalTypes(a, b arrow.DataType) (arrow.DataType, error) {
	// Timestamp is the most general temporal type
	if a.ID() == arrow.TIMESTAMP || b.ID() == arrow.TIMESTAMP {
		// If either has timezone, result has timezone
		aTs, aIsTs := a.(*arrow.TimestampType)
		bTs, bIsTs := b.(*arrow.TimestampType)

		if aIsTs && bIsTs {
			tz := aTs.TimeZone
			if tz == "" {
				tz = bTs.TimeZone
			}
			return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: tz}, nil
		}
		if aIsTs {
			return aTs, nil
		}
		if bIsTs {
			return bTs, nil
		}
		return &arrow.TimestampType{Unit: arrow.Microsecond}, nil
	}

	// Date types
	if (a.ID() == arrow.DATE32 || a.ID() == arrow.DATE64) &&
		(b.ID() == arrow.DATE32 || b.ID() == arrow.DATE64) {
		return arrow.FixedWidthTypes.Date32, nil
	}

	// Time types
	if (a.ID() == arrow.TIME32 || a.ID() == arrow.TIME64) &&
		(b.ID() == arrow.TIME32 || b.ID() == arrow.TIME64) {
		return arrow.FixedWidthTypes.Time64us, nil
	}

	// Mixed date/time - promote to timestamp
	return &arrow.TimestampType{Unit: arrow.Microsecond}, nil
}

// ArrowFieldToColumn converts an Arrow field to an internal schema Column.
func ArrowFieldToColumn(name string, dt arrow.DataType, nullable bool) schema.Column {
	col := schema.Column{
		Name:     name,
		Nullable: nullable,
	}

	switch dt.ID() {
	case arrow.BOOL:
		col.DataType = schema.TypeBoolean
	case arrow.INT8, arrow.INT16:
		col.DataType = schema.TypeInt16
	case arrow.INT32, arrow.UINT8, arrow.UINT16:
		col.DataType = schema.TypeInt32
	case arrow.INT64, arrow.UINT32, arrow.UINT64:
		col.DataType = schema.TypeInt64
	case arrow.FLOAT16, arrow.FLOAT32:
		col.DataType = schema.TypeFloat32
	case arrow.FLOAT64:
		col.DataType = schema.TypeFloat64
	case arrow.DECIMAL128, arrow.DECIMAL256:
		col.DataType = schema.TypeDecimal
		if decType, ok := dt.(*arrow.Decimal128Type); ok {
			col.Precision = int(decType.Precision)
			col.Scale = int(decType.Scale)
		}
	case arrow.STRING, arrow.LARGE_STRING:
		col.DataType = schema.TypeString
	case arrow.BINARY, arrow.LARGE_BINARY:
		col.DataType = schema.TypeBinary
	case arrow.DATE32, arrow.DATE64:
		col.DataType = schema.TypeDate
	case arrow.TIME32, arrow.TIME64:
		col.DataType = schema.TypeTime
	case arrow.TIMESTAMP:
		if tsType, ok := dt.(*arrow.TimestampType); ok && tsType.TimeZone != "" {
			col.DataType = schema.TypeTimestampTZ
		} else {
			col.DataType = schema.TypeTimestamp
		}
	case arrow.LIST, arrow.LARGE_LIST:
		col.DataType = schema.TypeArray
		if listType, ok := dt.(*arrow.ListType); ok {
			elemCol := ArrowFieldToColumn("", listType.Elem(), true)
			col.ArrayType = elemCol.DataType
		}
	case arrow.EXTENSION:
		// Check if it's a JSON extension type
		if isJSONType(dt) {
			col.DataType = schema.TypeJSON
		} else if isUnknownType(dt) {
			col.DataType = schema.TypeUnknown
		} else {
			col.DataType = schema.TypeString
		}
	default:
		// Unknown type - default to string
		col.DataType = schema.TypeString
	}

	return col
}

// ArrowTypeToDataType converts an Arrow DataType to internal schema.DataType.
func ArrowTypeToDataType(dt arrow.DataType) (schema.DataType, int, int, schema.DataType) {
	col := ArrowFieldToColumn("", dt, true)
	return col.DataType, col.Precision, col.Scale, col.ArrayType
}

// ValidateSchema checks if the inferred schema is valid for use.
func ValidateSchema(s *schema.TableSchema) error {
	if s == nil {
		return fmt.Errorf("schema is nil")
	}
	if len(s.Columns) == 0 {
		return fmt.Errorf("schema has no columns")
	}
	for i, col := range s.Columns {
		if col.Name == "" {
			return fmt.Errorf("column %d has empty name", i)
		}
	}
	return nil
}

// isJSONType checks if the Arrow type is the JSON extension type.
func isJSONType(dt arrow.DataType) bool {
	if dt.ID() != arrow.EXTENSION {
		return false
	}
	ext, ok := dt.(arrow.ExtensionType)
	if !ok {
		return false
	}
	return ext.ExtensionName() == schema.JSONExtensionName
}

func isUnknownType(dt arrow.DataType) bool {
	if dt.ID() != arrow.EXTENSION {
		return false
	}
	ext, ok := dt.(arrow.ExtensionType)
	if !ok {
		return false
	}
	return ext.ExtensionName() == schema.UnknownExtensionName
}
