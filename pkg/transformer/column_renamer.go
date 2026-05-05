package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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

	// Build new schema with renamed fields
	newSchema := r.OutputSchema(batch.Schema())

	// Collect columns (retain references)
	cols := make([]arrow.Array, batch.NumCols())
	for i := 0; i < int(batch.NumCols()); i++ {
		cols[i] = batch.Column(i)
		cols[i].Retain()
	}

	// Create new record batch with renamed schema
	newBatch := array.NewRecordBatch(newSchema, cols, batch.NumRows())

	// Release our references to the columns
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

	fields := make([]arrow.Field, len(inputSchema.Fields()))
	for i, field := range inputSchema.Fields() {
		newName := field.Name
		if renamed, ok := r.mapping[field.Name]; ok {
			newName = renamed
		}
		fields[i] = arrow.Field{
			Name:     newName,
			Type:     field.Type,
			Nullable: field.Nullable,
			Metadata: field.Metadata,
		}
	}

	return arrow.NewSchema(fields, nil)
}

// HasRenames returns true if any columns will be renamed.
func (r *ColumnRenamer) HasRenames() bool {
	return len(r.mapping) > 0
}

// Mapping returns the column name mapping.
func (r *ColumnRenamer) Mapping() map[string]string {
	return r.mapping
}
