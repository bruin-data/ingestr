package schemaevolution

import (
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// CompareOptions contains optional parameters for schema comparison.
type CompareOptions struct {
	Overrides   ColumnOverrides
	PrimaryKeys []string
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
	primaryKeys := make(map[string]struct{})
	if opts != nil {
		overrides = opts.Overrides
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

	for _, srcCol := range source.Columns {
		lowerName := strings.ToLower(srcCol.Name)
		destCol, exists := destColumnMap[lowerName]

		// Check for user override first
		if override, hasOverride := overrides.Get(srcCol.Name); hasOverride {
			newCol := override.ApplyToColumn(srcCol)
			if exists {
				newCol.Name = destCol.Name
				_, isPrimaryKey := primaryKeys[lowerName]
				relaxNullability := srcCol.Nullable && !destCol.Nullable && !isPrimaryKey
				newCol.Nullable = destCol.Nullable || relaxNullability
				if relaxNullability {
					old := destCol
					relaxed := destCol
					relaxed.Nullable = true
					changes = append(changes, SchemaChange{
						Type: ChangeRelaxNullability, ColumnName: destCol.Name, ColumnPath: []string{destCol.Name},
						OldColumn: &old, NewColumn: relaxed,
					})
				}
			} else {
				newCol.Nullable = true
			}

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
				ColumnName: newCol.Name,
				ColumnPath: []string{newCol.Name},
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
		if srcCol.DataType != destCol.DataType && (nestedColumnType(srcCol.DataType) || nestedColumnType(destCol.DataType)) {
			return nil, fmt.Errorf("unsupported nested schema change at %s: %s to %s", srcCol.Name, destCol.DataType, srcCol.DataType)
		}
		if nestedColumnType(srcCol.DataType) && srcCol.DataType == destCol.DataType {
			_, isPrimaryKey := primaryKeys[lowerName]
			nestedChanges, err := compareColumnRecursive([]string{destCol.Name}, srcCol, destCol, isPrimaryKey)
			if err != nil {
				return nil, err
			}
			changes = append(changes, nestedChanges...)
			continue
		}
		_, isPrimaryKey := primaryKeys[lowerName]
		if srcCol.Nullable && !destCol.Nullable && !isPrimaryKey {
			old := destCol
			newCol := srcCol
			newCol.Name = destCol.Name
			changes = append(changes, SchemaChange{
				Type: ChangeRelaxNullability, ColumnName: destCol.Name, ColumnPath: []string{destCol.Name},
				OldColumn: &old, NewColumn: newCol,
			})
		}

		if needsWidening(srcCol, destCol) {
			widenedType, _ := GetWidenedType(srcCol.DataType, destCol.DataType)
			newCol := srcCol
			newCol.Name = destCol.Name
			newCol.DataType = widenedType
			newCol.Nullable = destCol.Nullable || (srcCol.Nullable && !isPrimaryKey)

			// For decimal precision/scale widening, keep TypeDecimal but merge precision
			isDecimalPrecisionWidening := srcCol.DataType == schema.TypeDecimal && destCol.DataType == schema.TypeDecimal
			if isDecimalPrecisionWidening {
				var err error
				newCol.Precision, newCol.Scale, err = MergeDecimalPrecisionChecked(srcCol, destCol)
				if err != nil {
					return nil, fmt.Errorf("column %s: %w", destCol.Name, err)
				}
			}

			// For string length widening, keep TypeString but grow the length.
			isStringLengthWidening := widenedType == schema.TypeString &&
				destCol.DataType == schema.TypeString &&
				stringNeedsWidening(srcCol.MaxLength, destCol.MaxLength)
			if isStringLengthWidening {
				newCol.MaxLength = WidenedStringLength(srcCol.MaxLength, destCol.MaxLength)
			}

			// Only add change if type is different OR precision/length needs widening
			isNestedShapeChange := srcCol.DataType == destCol.DataType && nestedColumnType(srcCol.DataType)
			if widenedType != destCol.DataType || isDecimalPrecisionWidening || isStringLengthWidening || isNestedShapeChange {
				changes = append(changes, SchemaChange{
					Type:       ChangeWidenType,
					ColumnName: destCol.Name,
					ColumnPath: []string{destCol.Name},
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

func compareColumnRecursive(path []string, src, dest schema.Column, ignoreNullability bool) ([]SchemaChange, error) {
	var changes []SchemaChange
	name := strings.Join(path, ".")
	if src.Nullable && !dest.Nullable && !ignoreNullability {
		old := dest
		newColumn := src
		newColumn.Name = dest.Name
		changes = append(changes, SchemaChange{
			Type: ChangeRelaxNullability, ColumnName: name, ColumnPath: append([]string(nil), path...),
			OldColumn: &old, NewColumn: newColumn,
		})
	}
	if src.DataType != dest.DataType {
		if nestedColumnType(src.DataType) || nestedColumnType(dest.DataType) {
			return nil, fmt.Errorf("unsupported nested schema change at %s: %s to %s", name, dest.DataType, src.DataType)
		}
		old := dest
		newColumn, err := widenedColumn(src, dest)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", name, err)
		}
		return append(changes, SchemaChange{
			Type: ChangeWidenType, ColumnName: name, ColumnPath: append([]string(nil), path...),
			OldColumn: &old, NewColumn: newColumn,
		}), nil
	}
	switch src.DataType {
	case schema.TypeStruct:
		if src.StructFields == nil || dest.StructFields == nil {
			if src.StructFields == nil && dest.StructFields == nil {
				return changes, nil
			}
			return nil, fmt.Errorf("unsupported incomplete struct schema at %s", name)
		}
		destFields := make(map[string]schema.Column, len(dest.StructFields.Columns))
		sourceFields := make(map[string]struct{}, len(src.StructFields.Columns))
		for _, field := range dest.StructFields.Columns {
			destFields[strings.ToLower(field.Name)] = field
		}
		for _, field := range src.StructFields.Columns {
			sourceFields[strings.ToLower(field.Name)] = struct{}{}
			old, ok := destFields[strings.ToLower(field.Name)]
			childName := field.Name
			if ok {
				childName = old.Name
			}
			childPath := append(append([]string(nil), path...), childName)
			if !ok {
				added := makeNullable(field)
				changes = append(changes, SchemaChange{
					Type: ChangeAddColumn, ColumnName: strings.Join(childPath, "."), ColumnPath: childPath,
					NewColumn: added,
				})
				continue
			}
			childChanges, err := compareColumnRecursive(childPath, field, old, false)
			if err != nil {
				return nil, err
			}
			changes = append(changes, childChanges...)
		}
		for _, old := range dest.StructFields.Columns {
			if _, ok := sourceFields[strings.ToLower(old.Name)]; ok || old.Nullable {
				continue
			}
			childPath := append(append([]string(nil), path...), old.Name)
			oldCopy := old
			changes = append(changes, SchemaChange{
				Type: ChangeRemoveColumn, ColumnName: strings.Join(childPath, "."), ColumnPath: childPath,
				OldColumn: &oldCopy,
			})
		}
	case schema.TypeArray:
		if src.ArrayLength != dest.ArrayLength {
			return nil, fmt.Errorf("unsupported list cardinality change at %s: %d to %d", name, dest.ArrayLength, src.ArrayLength)
		}
		srcElem := normalizedArrayElement(src)
		destElem := normalizedArrayElement(dest)
		child, err := compareColumnRecursive(append(append([]string(nil), path...), "element"), srcElem, destElem, false)
		if err != nil {
			return nil, err
		}
		changes = append(changes, child...)
	case schema.TypeMap:
		if src.MapKey == nil || dest.MapKey == nil || src.MapValue == nil || dest.MapValue == nil {
			return nil, fmt.Errorf("unsupported incomplete map schema at %s", name)
		}
		if schema.DataTypeToArrowType(*src.MapKey).Fingerprint() != schema.DataTypeToArrowType(*dest.MapKey).Fingerprint() {
			return nil, fmt.Errorf("unsupported map key change at %s.key", name)
		}
		child, err := compareColumnRecursive(append(append([]string(nil), path...), "value"), *src.MapValue, *dest.MapValue, false)
		if err != nil {
			return nil, err
		}
		changes = append(changes, child...)
	default:
		if src.DataType == schema.TypeFixedBinary && dest.DataType == schema.TypeFixedBinary && src.FixedLength != dest.FixedLength {
			return nil, fmt.Errorf("unsupported fixed-binary width change at %s: %d to %d", name, dest.FixedLength, src.FixedLength)
		}
		if needsWidening(src, dest) {
			old := dest
			newColumn, err := widenedColumn(src, dest)
			if err != nil {
				return nil, fmt.Errorf("column %s: %w", name, err)
			}
			changes = append(changes, SchemaChange{
				Type: ChangeWidenType, ColumnName: name, ColumnPath: append([]string(nil), path...),
				OldColumn: &old, NewColumn: newColumn,
			})
		}
	}
	return changes, nil
}

func widenedColumn(src, dest schema.Column) (schema.Column, error) {
	result := src
	result.Name = dest.Name
	result.DataType, _ = GetWidenedType(src.DataType, dest.DataType)
	result.Nullable = src.Nullable || dest.Nullable
	if result.DataType == schema.TypeDecimal && src.DataType == schema.TypeDecimal && dest.DataType == schema.TypeDecimal {
		var err error
		result.Precision, result.Scale, err = MergeDecimalPrecisionChecked(src, dest)
		if err != nil {
			return schema.Column{}, err
		}
	}
	if result.DataType == schema.TypeString && src.DataType == schema.TypeString && dest.DataType == schema.TypeString {
		result.MaxLength = WidenedStringLength(src.MaxLength, dest.MaxLength)
	}
	return result, nil
}

func normalizedArrayElement(col schema.Column) schema.Column {
	if col.Element != nil {
		return *col.Element
	}
	return schema.Column{
		Name: "element", DataType: col.ArrayType, Nullable: true,
		Precision: col.Precision, Scale: col.Scale, MaxLength: col.MaxLength, FixedLength: col.FixedLength,
	}
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
		case schema.TypeArray, schema.TypeStruct, schema.TypeMap, schema.TypeFixedBinary:
			return schema.DataTypeToArrowType(src).Fingerprint() != schema.DataTypeToArrowType(dest).Fingerprint()
		default:
			return false
		}
	}
	return true
}

func nestedColumnType(dataType schema.DataType) bool {
	switch dataType {
	case schema.TypeArray, schema.TypeStruct, schema.TypeMap, schema.TypeFixedBinary:
		return true
	default:
		return false
	}
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
