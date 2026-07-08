package schemaevolution

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// CompareOptions contains optional parameters for schema comparison.
type CompareOptions struct {
	Overrides ColumnOverrides
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
	if opts != nil {
		overrides = opts.Overrides
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
				switch newCol.DataType {
				case schema.TypeDecimal:
					// For decimal, check integer digits and scale separately
					destIntDigits := destCol.Precision - destCol.Scale
					newIntDigits := newCol.Precision - newCol.Scale
					if destIntDigits >= newIntDigits && destCol.Scale >= newCol.Scale {
						continue
					}
				case schema.TypeString:
					// Only evolve when the override widens the length; never
					// narrow an existing column (that would truncate data).
					if !stringNeedsWidening(newCol.MaxLength, destCol.MaxLength) {
						continue
					}
					newCol.MaxLength = WidenedStringLength(newCol.MaxLength, destCol.MaxLength)
				default:
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

			// For string length widening, keep TypeString but grow the length.
			isStringLengthWidening := widenedType == schema.TypeString && destCol.DataType == schema.TypeString
			if isStringLengthWidening {
				newCol.MaxLength = WidenedStringLength(srcCol.MaxLength, destCol.MaxLength)
			}

			// Only add change if type is different OR precision/length needs widening
			if widenedType != destCol.DataType || isDecimalPrecisionWidening || isStringLengthWidening {
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

func needsWidening(src, dest schema.Column) bool {
	if src.DataType == dest.DataType {
		switch src.DataType {
		case schema.TypeDecimal:
			return src.Precision > dest.Precision || src.Scale > dest.Scale
		case schema.TypeString:
			return stringNeedsWidening(src.MaxLength, dest.MaxLength)
		default:
			return false
		}
	}
	return true
}

// stringNeedsWidening reports whether a bounded string column must grow to hold
// srcLen. MaxLength 0 means unbounded (widest); widening only ever grows.
func stringNeedsWidening(srcLen, destLen int) bool {
	if destLen <= 0 {
		return false // destination is already unbounded
	}
	return srcLen <= 0 || srcLen > destLen // source is unbounded or longer
}

// WidenedStringLength returns the wider of two string lengths, treating 0 as
// unbounded (which wins over any bounded length).
func WidenedStringLength(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > b {
		return a
	}
	return b
}
