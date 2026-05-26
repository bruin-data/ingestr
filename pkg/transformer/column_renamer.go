package transformer

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// ColumnRenamer renames columns in record batches, optionally dropping a
// specified set of columns. 
type ColumnRenamer struct {
	mapping map[string]string // source name -> destination name
	drops   map[string]bool   // source names to remove from each batch
}

// NewColumnRenamer creates a ColumnRenamer that only renames columns.
func NewColumnRenamer(mapping map[string]string) *ColumnRenamer {
	return &ColumnRenamer{mapping: mapping}
}

// NewColumnRenamerWithDrops creates a ColumnRenamer that also drops columns
// whose source name is in drops.
func NewColumnRenamerWithDrops(mapping map[string]string, drops map[string]bool) *ColumnRenamer {
	return &ColumnRenamer{mapping: mapping, drops: drops}
}

// Transform renames columns in the batch according to the mapping and removes
// any column whose name is in the drop set.
func (r *ColumnRenamer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(r.mapping) == 0 && len(r.drops) == 0 {
		batch.Retain()
		return batch, nil
	}

	inputSchema := batch.Schema()
	fields := make([]arrow.Field, 0, inputSchema.NumFields())
	cols := make([]arrow.Array, 0, inputSchema.NumFields())
	for i, field := range inputSchema.Fields() {
		if r.drops[field.Name] {
			continue
		}
		if renamed, ok := r.mapping[field.Name]; ok {
			field = arrow.Field{
				Name:     renamed,
				Type:     field.Type,
				Nullable: field.Nullable,
				Metadata: field.Metadata,
			}
		}
		fields = append(fields, field)
		col := batch.Column(i)
		col.Retain()
		cols = append(cols, col)
	}

	newBatch := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, batch.NumRows())
	// Release our references to the columns
	for _, col := range cols {
		col.Release()
	}

	return newBatch, nil
}

// OutputSchema returns the schema with renamed columns; dropped columns are removed.
func (r *ColumnRenamer) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	if len(r.mapping) == 0 && len(r.drops) == 0 {
		return inputSchema
	}

	fields := make([]arrow.Field, 0, len(inputSchema.Fields()))
	for _, field := range inputSchema.Fields() {
		if r.drops[field.Name] {
			continue
		}
		if renamed, ok := r.mapping[field.Name]; ok {
			fields = append(fields, arrow.Field{
				Name:     renamed,
				Type:     field.Type,
				Nullable: field.Nullable,
				Metadata: field.Metadata,
			})
			continue
		}
		fields = append(fields, field)
	}

	return arrow.NewSchema(fields, nil)
}

// HasRenames returns true if the renamer will modify any batch (rename or drop).
func (r *ColumnRenamer) HasRenames() bool {
	return len(r.mapping) > 0 || len(r.drops) > 0
}

// Mapping returns the rename map.
func (r *ColumnRenamer) Mapping() map[string]string {
	return r.mapping
}

// Drops returns the drop set.
func (r *ColumnRenamer) Drops() map[string]bool {
	return r.drops
}
