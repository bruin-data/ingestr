package schemaevolution

import (
	"github.com/bruin-data/ingestr/pkg/schema"
)

// EvolutionPlan is an abstract, database-agnostic description of how a
// destination table should change to accommodate a new source schema. It
// carries the set of column-level actions (the Comparison) rather than any
// pre-rendered SQL. Destinations turn the plan into DDL and apply it
// themselves via the SchemaEvolver interface.
type EvolutionPlan struct {
	// Table is the fully-qualified destination table the plan applies to.
	Table string
	// Comparison is the set of schema changes the destination should apply.
	// It is nil or empty when no migration is required.
	Comparison *SchemaComparison
	// TransformComparison retains contract-rejected changes needed by runtime
	// discard transformations; Comparison contains only destination mutations.
	TransformComparison *SchemaComparison
	// FinalSchema is what the destination table will look like once the
	// applicable changes have been applied.
	FinalSchema *schema.TableSchema
}

// HasChanges reports whether the plan contains any column changes that a
// destination needs to apply.
func (p *EvolutionPlan) HasChanges() bool {
	return p != nil && p.Comparison != nil && len(p.Comparison.Changes) > 0
}

// BuildFinalSchema computes what the destination table will look like AFTER a
// given comparison's changes have been applied via ALTER.
func BuildFinalSchema(destSchema *schema.TableSchema, comparison *SchemaComparison) *schema.TableSchema {
	if destSchema == nil {
		return nil
	}

	result := *destSchema
	result.Columns = cloneColumns(destSchema.Columns)
	result.PrimaryKeys = append([]string{}, destSchema.PrimaryKeys...)

	if comparison == nil || !comparison.HasChanges {
		return &result
	}

	for _, change := range comparison.Changes {
		switch change.Type {
		case ChangeAddColumn:
			if len(change.ColumnPath) <= 1 {
				result.Columns = append(result.Columns, change.NewColumn)
			} else {
				applyNestedColumnChange(result.Columns, change.ColumnPath[:len(change.ColumnPath)-1], func(parent *schema.Column) {
					if parent.StructFields != nil {
						parent.StructFields.Columns = append(parent.StructFields.Columns, change.NewColumn)
					}
				})
			}

		case ChangeWidenType, ChangeOverrideType:
			path := change.ColumnPath
			if len(path) == 0 {
				path = []string{change.ColumnName}
			}
			applyNestedColumnChange(result.Columns, path, func(col *schema.Column) {
				name := col.Name
				*col = change.NewColumn
				if col.Name == "" {
					col.Name = name
				}
			})

		case ChangeRemoveColumn:
			if len(change.ColumnPath) > 1 {
				applyNestedColumnChange(result.Columns, change.ColumnPath, func(col *schema.Column) { col.Nullable = true })
			}
			// Soft remove: destination keeps the column; future values are NULL.
			path := change.ColumnPath
			if len(path) == 0 {
				path = []string{change.ColumnName}
			}
			applyNestedColumnChange(result.Columns, path, func(col *schema.Column) { col.Nullable = true })
		case ChangeRelaxNullability:
			path := change.ColumnPath
			if len(path) == 0 {
				path = []string{change.ColumnName}
			}
			applyNestedColumnChange(result.Columns, path, func(col *schema.Column) { col.Nullable = true })
		}
	}

	return &result
}

func cloneColumns(columns []schema.Column) []schema.Column {
	out := make([]schema.Column, len(columns))
	for i := range columns {
		out[i] = columns[i]
		if columns[i].Element != nil {
			element := cloneColumns([]schema.Column{*columns[i].Element})[0]
			out[i].Element = &element
		}
		if columns[i].StructFields != nil {
			fields := *columns[i].StructFields
			fields.Columns = cloneColumns(columns[i].StructFields.Columns)
			out[i].StructFields = &fields
		}
		if columns[i].MapKey != nil {
			key := cloneColumns([]schema.Column{*columns[i].MapKey})[0]
			out[i].MapKey = &key
		}
		if columns[i].MapValue != nil {
			value := cloneColumns([]schema.Column{*columns[i].MapValue})[0]
			out[i].MapValue = &value
		}
	}
	return out
}

func applyNestedColumnChange(columns []schema.Column, path []string, apply func(*schema.Column)) bool {
	if len(path) == 0 {
		return false
	}
	for i := range columns {
		if columns[i].Name != path[0] {
			continue
		}
		return applyNestedColumn(&columns[i], path[1:], apply)
	}
	return false
}

func applyNestedColumn(col *schema.Column, path []string, apply func(*schema.Column)) bool {
	if len(path) == 0 {
		apply(col)
		return true
	}
	switch col.DataType {
	case schema.TypeStruct:
		if col.StructFields != nil {
			return applyNestedColumnChange(col.StructFields.Columns, path, apply)
		}
	case schema.TypeArray:
		if path[0] == "element" {
			if col.Element == nil {
				element := normalizedArrayElement(*col)
				col.Element = &element
			}
			changed := applyNestedColumn(col.Element, path[1:], apply)
			if changed {
				col.ArrayType = col.Element.DataType
				col.Precision = col.Element.Precision
				col.Scale = col.Element.Scale
				col.MaxLength = col.Element.MaxLength
				col.FixedLength = col.Element.FixedLength
			}
			return changed
		}
	case schema.TypeMap:
		if path[0] == "value" && col.MapValue != nil {
			return applyNestedColumn(col.MapValue, path[1:], apply)
		}
	}
	return false
}
