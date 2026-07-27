package schemaevolution

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// CompareOptions contains optional parameters for schema comparison.
type CompareOptions struct {
	Overrides       ColumnOverrides
	PrimaryKeys     []string
	NormalizeColumn func(schema.Column) schema.Column
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
	normalizeColumn := func(col schema.Column) schema.Column { return col }
	primaryKeys := make(map[string]struct{})
	if opts != nil {
		overrides = opts.Overrides
		if opts.NormalizeColumn != nil {
			normalizeColumn = opts.NormalizeColumn
		}
		for _, key := range opts.PrimaryKeys {
			primaryKeys[strings.ToLower(key)] = struct{}{}
		}
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

	for _, sourceColumn := range source.Columns {
		srcCol := normalizeColumn(sourceColumn)
		lowerName := strings.ToLower(sourceColumn.Name)
		destColumn, exists := destColumnMap[lowerName]
		destCol := normalizeColumn(destColumn)

		// Check for user override first
		if override, hasOverride := overrides.Get(sourceColumn.Name); hasOverride {
			newCol := override.ApplyToColumn(sourceColumn)
			comparisonCol := normalizeColumn(newCol)
			if exists {
				newCol.Name = destColumn.Name
				comparisonCol.Name = destCol.Name
				_, isPrimaryKey := primaryKeys[lowerName]
				relaxNullability := sourceColumn.Nullable && !destColumn.Nullable && !isPrimaryKey && !naming.IsIngestrColumn(sourceColumn.Name)
				newCol.Nullable = destColumn.Nullable || relaxNullability
				if relaxNullability {
					old := destColumn
					relaxed := destColumn
					relaxed.Nullable = true
					changes = append(changes, SchemaChange{
						Type: ChangeRelaxNullability, ColumnName: destColumn.Name,
						OldColumn: &old, NewColumn: relaxed,
					})
				}
			} else {
				newCol.Nullable = true
			}

			// If destination exists and matches override, no change needed
			if exists && destCol.DataType == comparisonCol.DataType {
				switch comparisonCol.DataType {
				case schema.TypeDecimal:
					precision, scale, err := MergeDecimalPrecisionChecked(
						normalizedDecimal(comparisonCol), normalizedDecimal(destCol),
					)
					if err != nil {
						return nil, fmt.Errorf("column %s: %w", destColumn.Name, err)
					}
					if precision == normalizedDecimal(destCol).Precision && scale == destCol.Scale {
						continue
					}
					newCol.Precision = precision
					newCol.Scale = scale
				case schema.TypeString:
					// Only evolve when the override widens the length; never
					// narrow an existing column (that would truncate data).
					if !stringNeedsWidening(comparisonCol.MaxLength, destCol.MaxLength) {
						continue
					}
					newCol.MaxLength = WidenedStringLength(comparisonCol.MaxLength, destCol.MaxLength)
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
				oldCol = &destColumn
			}

			changes = append(changes, SchemaChange{
				Type:       changeType,
				ColumnName: newCol.Name,
				OldColumn:  oldCol,
				NewColumn:  newCol,
			})
			continue
		}

		if !exists {
			changes = append(changes, SchemaChange{
				Type:       ChangeAddColumn,
				ColumnName: sourceColumn.Name,
				OldColumn:  nil,
				NewColumn:  makeNullable(sourceColumn),
			})
			continue
		}
		_, isPrimaryKey := primaryKeys[lowerName]
		if sourceColumn.Nullable && !destColumn.Nullable && !isPrimaryKey && !naming.IsIngestrColumn(sourceColumn.Name) {
			old := destColumn
			newCol := sourceColumn
			newCol.Name = destColumn.Name
			changes = append(changes, SchemaChange{
				Type: ChangeRelaxNullability, ColumnName: destColumn.Name,
				OldColumn: &old, NewColumn: newCol,
			})
		}

		if needsWidening(srcCol, destCol) {
			newCol, err := widenedColumn(srcCol, destCol)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", destColumn.Name, err)
			}
			newCol.Nullable = destColumn.Nullable || (sourceColumn.Nullable && !isPrimaryKey)

			if !sameColumnTypeShape(newCol, destCol) {
				changes = append(changes, SchemaChange{
					Type:       ChangeWidenType,
					ColumnName: destColumn.Name,
					OldColumn:  &destColumn,
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

func widenedColumn(src, dest schema.Column) (schema.Column, error) {
	result := src
	result.Name = dest.Name
	result.DataType, _ = GetWidenedType(src.DataType, dest.DataType)
	result.Nullable = src.Nullable || dest.Nullable
	if result.DataType == schema.TypeArray && src.DataType == schema.TypeArray && dest.DataType == schema.TypeArray {
		result.ArrayType, _ = GetWidenedType(src.ArrayType, dest.ArrayType)
		if result.ArrayType == schema.TypeDecimal && (isFloatingType(src.ArrayType) || isFloatingType(dest.ArrayType)) {
			result.ArrayType = schema.TypeString
			result.Precision = 0
			result.Scale = 0
			result.MaxLength = 0
			return result, nil
		}
		if result.ArrayType == dest.ArrayType {
			result.Precision = dest.Precision
			result.Scale = dest.Scale
			result.MaxLength = dest.MaxLength
		}
		if result.ArrayType == schema.TypeDecimal {
			var err error
			result.Precision, result.Scale, err = mergeDecimalTypesChecked(
				arrayElementColumn(src), arrayElementColumn(dest),
			)
			if err != nil {
				return schema.Column{}, err
			}
		}
		if result.ArrayType == schema.TypeString {
			if src.ArrayType == schema.TypeString && dest.ArrayType == schema.TypeString {
				result.MaxLength = WidenedStringLength(src.MaxLength, dest.MaxLength)
			} else {
				result.MaxLength = 0
			}
		}
		return result, nil
	}
	if result.DataType == schema.TypeDecimal && (isFloatingType(src.DataType) || isFloatingType(dest.DataType)) {
		result.DataType = schema.TypeString
		result.Precision = 0
		result.Scale = 0
		result.MaxLength = 0
		return result, nil
	}
	if result.DataType == schema.TypeDecimal {
		var err error
		result.Precision, result.Scale, err = mergeDecimalTypesChecked(src, dest)
		if err != nil {
			return schema.Column{}, err
		}
	}
	if result.DataType == schema.TypeString {
		if src.DataType == schema.TypeString && dest.DataType == schema.TypeString {
			result.MaxLength = WidenedStringLength(src.MaxLength, dest.MaxLength)
		} else {
			result.MaxLength = 0
		}
	}
	return result, nil
}

func isFloatingType(dataType schema.DataType) bool {
	return dataType == schema.TypeFloat32 || dataType == schema.TypeFloat64
}

func makeNullable(col schema.Column) schema.Column {
	col.Nullable = true
	return col
}

func needsWidening(src, dest schema.Column) bool {
	if src.DataType == dest.DataType {
		switch src.DataType {
		case schema.TypeDecimal:
			return decimalNeedsWidening(src, dest)
		case schema.TypeString:
			return stringNeedsWidening(src.MaxLength, dest.MaxLength)
		case schema.TypeArray:
			if src.ArrayType != dest.ArrayType {
				return true
			}
			switch src.ArrayType {
			case schema.TypeDecimal:
				return decimalNeedsWidening(src, dest)
			case schema.TypeString:
				return stringNeedsWidening(src.MaxLength, dest.MaxLength)
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}

func decimalNeedsWidening(src, dest schema.Column) bool {
	src = normalizedDecimal(src)
	dest = normalizedDecimal(dest)
	return src.Precision-src.Scale > dest.Precision-dest.Scale || src.Scale > dest.Scale
}

func arrayElementColumn(col schema.Column) schema.Column {
	return schema.Column{
		DataType:  col.ArrayType,
		Precision: col.Precision,
		Scale:     col.Scale,
		MaxLength: col.MaxLength,
	}
}

func mergeDecimalTypesChecked(src, dest schema.Column) (int, int, error) {
	srcDecimal, srcOK := decimalRepresentation(src)
	destDecimal, destOK := decimalRepresentation(dest)
	if !srcOK || !destOK {
		if src.DataType == schema.TypeDecimal {
			return src.Precision, src.Scale, nil
		}
		return dest.Precision, dest.Scale, nil
	}
	return MergeDecimalPrecisionChecked(srcDecimal, destDecimal)
}

func decimalRepresentation(col schema.Column) (schema.Column, bool) {
	if col.DataType == schema.TypeDecimal {
		return normalizedDecimal(col), true
	}
	var digits int
	switch col.DataType {
	case schema.TypeInt8:
		digits = 3
	case schema.TypeInt16:
		digits = 5
	case schema.TypeInt32:
		digits = 10
	case schema.TypeInt64:
		digits = 19
	default:
		return schema.Column{}, false
	}
	return schema.Column{DataType: schema.TypeDecimal, Precision: digits}, true
}

func normalizedDecimal(col schema.Column) schema.Column {
	if col.Precision == 0 {
		col.Precision = 38
	}
	return col
}

func sameColumnTypeShape(a, b schema.Column) bool {
	if a.DataType != b.DataType {
		return false
	}
	switch a.DataType {
	case schema.TypeDecimal:
		return a.Precision == b.Precision && a.Scale == b.Scale
	case schema.TypeString:
		return a.MaxLength == b.MaxLength
	case schema.TypeArray:
		if a.ArrayType != b.ArrayType {
			return false
		}
		switch a.ArrayType {
		case schema.TypeDecimal:
			return a.Precision == b.Precision && a.Scale == b.Scale
		case schema.TypeString:
			return a.MaxLength == b.MaxLength
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
