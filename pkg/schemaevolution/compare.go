package schemaevolution

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// CompareOptions contains optional parameters for schema comparison.
type CompareOptions struct {
	Overrides         ColumnOverrides
	DestinationScheme string
}

// Compare compares source and destination schemas and returns the differences.
// It identifies:
// - New columns in source that don't exist in destination
// - Type mismatches that require widening
// - User-specified type overrides
func Compare(source, dest *schema.TableSchema, opts *CompareOptions) (*SchemaComparison, error) {
	if source == nil || dest == nil {
		return &SchemaComparison{}, nil
	}

	var overrides ColumnOverrides
	var destScheme string
	if opts != nil {
		overrides = opts.Overrides
		destScheme = opts.DestinationScheme
	}

	destColumnMap := make(map[string]schema.Column)
	for _, col := range dest.Columns {
		destColumnMap[strings.ToLower(col.Name)] = col
	}

	srcColumnMap := make(map[string]bool)
	for _, col := range source.Columns {
		srcColumnMap[strings.ToLower(col.Name)] = true
	}

	var changes []SchemaChange

	for _, srcCol := range source.Columns {
		lowerName := strings.ToLower(srcCol.Name)
		destCol, exists := destColumnMap[lowerName]

		// Check for user override first
		if override, hasOverride := overrides.Get(srcCol.Name); hasOverride {
			newCol := override.ApplyToColumn(srcCol)
			newCol.Nullable = true

			// If destination exists and matches override, no change needed
			if exists && destCol.DataType == newCol.DataType {
				if newCol.DataType != schema.TypeDecimal {
					continue
				}
				// For decimal, check integer digits and scale separately
				destIntDigits := destCol.Precision - destCol.Scale
				newIntDigits := newCol.Precision - newCol.Scale
				if destIntDigits >= newIntDigits && destCol.Scale >= newCol.Scale {
					continue
				}
			}
			// Where the destination stores all int (and float) widths as one type,
			// an override within the same numeric family is a no-op, so skip it.
			if exists && collapsesNumericWidths(destScheme) {
				if c := numericWidthClass(newCol.DataType); c != schema.TypeUnknown && c == numericWidthClass(destCol.DataType) {
					continue
				}
			}

			changeType := ChangeOverrideType
			if !exists {
				changeType = ChangeAddColumn
			}

			var oldCol *schema.Column
			if exists {
				oldCol = &destCol
			}

			changes = append(changes, SchemaChange{
				Type:       changeType,
				ColumnName: srcCol.Name,
				OldColumn:  oldCol,
				NewColumn:  newCol,
			})
			continue
		}

		if !exists {
			changes = append(changes, SchemaChange{
				Type:       ChangeAddColumn,
				ColumnName: srcCol.Name,
				OldColumn:  nil,
				NewColumn:  makeNullable(srcCol),
			})
			continue
		}

		if needsWidening(srcCol, destCol) {
			widenedType, _ := GetWidenedType(srcCol.DataType, destCol.DataType)
			newCol := srcCol
			newCol.DataType = widenedType
			newCol.Nullable = true

			// For decimal precision/scale widening, keep TypeDecimal but merge precision
			isDecimalPrecisionWidening := srcCol.DataType == schema.TypeDecimal && destCol.DataType == schema.TypeDecimal
			if isDecimalPrecisionWidening {
				newCol.Precision, newCol.Scale = MergeDecimalPrecision(srcCol, destCol)
			}

			// Only add change if type is different OR decimal precision needs widening
			if widenedType != destCol.DataType || isDecimalPrecisionWidening {
				changes = append(changes, SchemaChange{
					Type:       ChangeWidenType,
					ColumnName: srcCol.Name,
					OldColumn:  &destCol,
					NewColumn:  newCol,
				})
			}
		}
	}

	// Detect columns in destination that are missing from source (column removals)
	// Skip ingestr metadata columns as they are internal columns
	for _, destCol := range dest.Columns {
		// Skip ingestr metadata columns
		if naming.IsIngestrColumn(destCol.Name) {
			continue
		}
		lowerName := strings.ToLower(destCol.Name)
		if !srcColumnMap[lowerName] {
			destColCopy := destCol
			changes = append(changes, SchemaChange{
				Type:       ChangeRemoveColumn,
				ColumnName: destCol.Name,
				OldColumn:  &destColCopy,
				NewColumn:  schema.Column{},
			})
		}
	}

	return &SchemaComparison{
		Changes:    changes,
		HasChanges: len(changes) > 0,
	}, nil
}

func makeNullable(col schema.Column) schema.Column {
	col.Nullable = true
	return col
}

// collapsesNumericWidths reports whether a destination stores every integer width
// as one type and every float width as one type (BigQuery, Snowflake, Trino).
func collapsesNumericWidths(scheme string) bool {
	switch scheme {
	case "bigquery", "snowflake", "trino":
		return true
	default:
		return false
	}
}

// numericWidthClass maps a type to the single width such destinations store it as
// (all ints -> int64, all floats -> float64); TypeUnknown for non-numeric types.
func numericWidthClass(t schema.DataType) schema.DataType {
	switch t {
	case schema.TypeInt8, schema.TypeInt16, schema.TypeInt32, schema.TypeInt64:
		return schema.TypeInt64
	case schema.TypeFloat32, schema.TypeFloat64:
		return schema.TypeFloat64
	default:
		return schema.TypeUnknown
	}
}

func needsWidening(src, dest schema.Column) bool {
	if src.DataType == dest.DataType {
		if src.DataType == schema.TypeDecimal {
			return src.Precision > dest.Precision || src.Scale > dest.Scale
		}
		return false
	}
	return true
}
