package schemaevolution

import (
	"strings"

	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// CompareOptions contains optional parameters for schema comparison.
type CompareOptions struct {
	Overrides ColumnOverrides
	// DestinationScheme is the destination's URI scheme (e.g. "bigquery"), used
	// to limit type-equivalence handling to destinations that need it.
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
			// On destinations that store every integer width as one physical type
			// (BigQuery's INT64), a narrower int override against a stored int64 is a
			// no-op; the ALTER it would emit is rejected on key/partition columns.
			if exists && collapsesIntegerWidths(destScheme) && isInt(newCol.DataType) && isInt(destCol.DataType) && CanWiden(newCol.DataType, destCol.DataType) {
				continue
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

// collapsesIntegerWidths reports whether a destination stores every integer
// width as a single physical type, so int8/16/32/64 are interchangeable and an
// int32 override against a stored int64 needs no ALTER: BigQuery (INT64),
// Snowflake (NUMBER(38,0)), and Trino (reads all ints back as int64).
func collapsesIntegerWidths(scheme string) bool {
	switch scheme {
	case "bigquery", "snowflake", "trino":
		return true
	default:
		return false
	}
}

func isInt(t schema.DataType) bool {
	return t == schema.TypeInt8 || t == schema.TypeInt16 || t == schema.TypeInt32 || t == schema.TypeInt64
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
