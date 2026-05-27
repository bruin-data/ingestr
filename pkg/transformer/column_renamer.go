package transformer

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
)

// ColumnRenamer renames columns in record batches based on a mapping.
type ColumnRenamer struct {
	mapping map[string]string // source name -> destination name
}

// NewColumnRenamer creates a new ColumnRenamer with the specified column name mapping.
// The mapping is from source column names to destination column names.
func NewColumnRenamer(mapping map[string]string) *ColumnRenamer {
	return &ColumnRenamer{
		mapping: mapping,
	}
}

// Transform renames columns in the batch according to the mapping.
func (r *ColumnRenamer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(r.mapping) == 0 {
		batch.Retain()
		return batch, nil
	}

	groups, hasDuplicates := r.outputGroups(batch.Schema())
	newSchema := schemaFromGroups(groups)

	cols := make([]arrow.Array, len(groups))
	for i, group := range groups {
		if !hasDuplicates || len(group.indices) == 1 {
			cols[i] = batch.Column(group.indices[0])
			cols[i].Retain()
			continue
		}

		castedCols, err := castGroupColumns(batch, group)
		if err != nil {
			for _, col := range cols[:i] {
				if col != nil {
					col.Release()
				}
			}
			return nil, err
		}

		builder := array.NewBuilder(memory.DefaultAllocator, group.field.Type)
		for row := 0; row < int(batch.NumRows()); row++ {
			var selected arrow.Array
			for _, col := range castedCols {
				if !col.IsNull(row) {
					selected = col
				}
			}
			if selected == nil {
				builder.AppendNull()
			} else {
				appendArrayValue(builder, selected, row)
			}
		}
		cols[i] = builder.NewArray()
		builder.Release()
		for _, col := range castedCols {
			col.Release()
		}
	}

	newBatch := array.NewRecordBatch(newSchema, cols, batch.NumRows())

	for _, col := range cols {
		col.Release()
	}

	return newBatch, nil
}

// OutputSchema returns the schema with renamed columns.
func (r *ColumnRenamer) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	if len(r.mapping) == 0 {
		return inputSchema
	}

	groups, _ := r.outputGroups(inputSchema)
	return schemaFromGroups(groups)
}

// HasRenames returns true if any columns will be renamed.
func (r *ColumnRenamer) HasRenames() bool {
	return len(r.mapping) > 0
}

// Mapping returns the column name mapping.
func (r *ColumnRenamer) Mapping() map[string]string {
	return r.mapping
}

type columnRenameGroup struct {
	field   arrow.Field
	indices []int
}

func (r *ColumnRenamer) outputGroups(inputSchema *arrow.Schema) ([]columnRenameGroup, bool) {
	groups := make([]columnRenameGroup, 0, inputSchema.NumFields())
	groupByName := make(map[string]int, inputSchema.NumFields())
	hasDuplicates := false

	for i, field := range inputSchema.Fields() {
		field.Name = r.outputName(field.Name)
		if groupIdx, ok := groupByName[field.Name]; ok {
			hasDuplicates = true
			group := &groups[groupIdx]
			group.field = mergeArrowFields(group.field, field)
			group.indices = append(group.indices, i)
			continue
		}

		groupByName[field.Name] = len(groups)
		groups = append(groups, columnRenameGroup{
			field:   field,
			indices: []int{i},
		})
	}

	return groups, hasDuplicates
}

func (r *ColumnRenamer) outputName(name string) string {
	if renamed, ok := r.mapping[name]; ok {
		return renamed
	}
	return name
}

func mergeArrowFields(existing, next arrow.Field) arrow.Field {
	mergedType, err := schemainfer.MergeArrowTypes(existing.Type, next.Type)
	if err != nil {
		mergedType = arrow.BinaryTypes.String
	}

	return arrow.Field{
		Name:     existing.Name,
		Type:     mergedType,
		Nullable: existing.Nullable || next.Nullable,
		Metadata: existing.Metadata,
	}
}

func appendArrayValue(builder array.Builder, col arrow.Array, row int) {
	if err := builder.AppendValueFromString(col.ValueStr(row)); err == nil {
		return
	}
	arrowconv.AppendValue(builder, arrowutil.Value(col, row))
}

func castGroupColumns(batch arrow.RecordBatch, group columnRenameGroup) ([]arrow.Array, error) {
	castedCols := make([]arrow.Array, len(group.indices))
	for i, colIdx := range group.indices {
		col := batch.Column(colIdx)
		if arrow.TypeEqual(col.DataType(), group.field.Type) {
			col.Retain()
			castedCols[i] = col
			continue
		}

		casted, err := castColumnToField(batch, colIdx, group.field)
		if err != nil {
			for _, c := range castedCols {
				if c != nil {
					c.Release()
				}
			}
			return nil, err
		}
		castedCols[i] = casted
	}

	return castedCols, nil
}

func castColumnToField(batch arrow.RecordBatch, colIdx int, field arrow.Field) (arrow.Array, error) {
	sourceField := field
	sourceField.Name = batch.Schema().Field(colIdx).Name
	sourceField.Type = batch.Column(colIdx).DataType()
	sourceSchema := arrow.NewSchema([]arrow.Field{sourceField}, nil)
	sourceBatch := array.NewRecordBatch(sourceSchema, []arrow.Array{batch.Column(colIdx)}, batch.NumRows())
	defer sourceBatch.Release()

	targetField := field
	targetField.Name = sourceField.Name
	targetSchema := arrow.NewSchema([]arrow.Field{targetField}, nil)
	castedBatch, err := databuffer.CastRecordToSchema(sourceBatch, targetSchema, false)
	if err != nil {
		return nil, fmt.Errorf("failed to cast duplicate column %s to %s: %w", sourceField.Name, field.Type, err)
	}
	defer castedBatch.Release()

	casted := castedBatch.Column(0)
	casted.Retain()
	return casted, nil
}

func schemaFromGroups(groups []columnRenameGroup) *arrow.Schema {
	fields := make([]arrow.Field, len(groups))
	for i, group := range groups {
		fields[i] = group.field
	}
	return arrow.NewSchema(fields, nil)
}
