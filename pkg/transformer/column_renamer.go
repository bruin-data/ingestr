package transformer

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ColumnRenamer renames columns in record batches, and optionally coalesces
// multiple source columns into a single target column when their normalized
// names collide.
type ColumnRenamer struct {
	mapping map[string]string   // source name → destination name (1-to-1)
	merges  map[string][]string // target name → ordered source columns to coalesce
}

// NewColumnRenamer creates a renamer that only does 1-to-1 renames.
func NewColumnRenamer(mapping map[string]string) *ColumnRenamer {
	return &ColumnRenamer{mapping: mapping}
}

// NewColumnRenamerWithMerges creates a renamer that also coalesces colliding
// source columns into a single target column.
func NewColumnRenamerWithMerges(mapping map[string]string, merges map[string][]string) *ColumnRenamer {
	return &ColumnRenamer{mapping: mapping, merges: merges}
}

// HasRenames returns true if any rename or merge will modify a batch.
func (r *ColumnRenamer) HasRenames() bool {
	return len(r.mapping) > 0 || len(r.merges) > 0
}

// Mapping returns the 1-to-1 rename map.
func (r *ColumnRenamer) Mapping() map[string]string { return r.mapping }

// Merges returns the merge groups (target → ordered source columns).
func (r *ColumnRenamer) Merges() map[string][]string { return r.merges }

type mergeBinding struct {
	target   string
	isWinner bool
	sources  []string
}

func (r *ColumnRenamer) bindings() map[string]mergeBinding {
	b := make(map[string]mergeBinding)
	for target, sources := range r.merges {
		if len(sources) == 0 {
			continue
		}
		winner := sources[len(sources)-1]
		for _, s := range sources {
			b[s] = mergeBinding{target: target, isWinner: s == winner, sources: sources}
		}
	}
	return b
}

// Transform produces a new record batch with columns renamed per the mapping
// and merge groups coalesced into single columns.
func (r *ColumnRenamer) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if !r.HasRenames() {
		batch.Retain()
		return batch, nil
	}

	bindings := r.bindings()
	inputSchema := batch.Schema()
	fields := make([]arrow.Field, 0, inputSchema.NumFields())
	cols := make([]arrow.Array, 0, inputSchema.NumFields())

	for i, field := range inputSchema.Fields() {
		if b, ok := bindings[field.Name]; ok {
			if !b.isWinner {
				continue
			}
			merged, err := coalesceColumns(batch, b.sources)
			if err != nil {
				for _, c := range cols {
					c.Release()
				}
				return nil, fmt.Errorf("coalesce columns for %q: %w", b.target, err)
			}
			fields = append(fields, arrow.Field{
				Name:     b.target,
				Type:     merged.DataType(),
				Nullable: true,
			})
			cols = append(cols, merged)
			continue
		}
		name := field.Name
		if renamed, ok := r.mapping[name]; ok {
			name = renamed
		}
		fields = append(fields, arrow.Field{
			Name:     name,
			Type:     field.Type,
			Nullable: field.Nullable,
			Metadata: field.Metadata,
		})
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

// OutputSchema returns the schema after rename and merge.
func (r *ColumnRenamer) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	if !r.HasRenames() {
		return inputSchema
	}

	bindings := r.bindings()
	fields := make([]arrow.Field, 0, len(inputSchema.Fields()))
	for _, field := range inputSchema.Fields() {
		if b, ok := bindings[field.Name]; ok {
			if !b.isWinner {
				continue
			}
			fields = append(fields, arrow.Field{
				Name:     b.target,
				Type:     field.Type,
				Nullable: true,
				Metadata: field.Metadata,
			})
			continue
		}
		name := field.Name
		if renamed, ok := r.mapping[name]; ok {
			name = renamed
		}
		fields = append(fields, arrow.Field{
			Name:     name,
			Type:     field.Type,
			Nullable: field.Nullable,
			Metadata: field.Metadata,
		})
	}
	return arrow.NewSchema(fields, nil)
}

// coalesceColumns picks values column-wise: per row, the LAST non-null value
// across `sources` (in their original order) is taken. Result type follows the
// last source's type — caller is responsible for ensuring all variants share a
// compatible type.
func coalesceColumns(batch arrow.RecordBatch, sources []string) (arrow.Array, error) {
	inputSchema := batch.Schema()
	columns := make([]arrow.Array, 0, len(sources))
	for _, name := range sources {
		idxs := inputSchema.FieldIndices(name)
		if len(idxs) == 0 {
			continue
		}
		columns = append(columns, batch.Column(idxs[0]))
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("no source columns present in batch")
	}
	if len(columns) == 1 {
		columns[0].Retain()
		return columns[0], nil
	}

	baseType := columns[len(columns)-1].DataType()
	for i, col := range columns {
		if !arrow.TypeEqual(col.DataType(), baseType) {
			return nil, fmt.Errorf("type mismatch on variant %d: %s vs winner type %s", i, col.DataType(), baseType)
		}
	}

	pool := memory.NewGoAllocator()
	numRows := int(batch.NumRows())

	if numRows == 0 {
		return array.NewSlice(columns[len(columns)-1], 0, 0), nil
	}

	winners := make([]int, numRows)
	for row := 0; row < numRows; row++ {
		winners[row] = -1
		for i := len(columns) - 1; i >= 0; i-- {
			c := columns[i]
			if row < c.Len() && !c.IsNull(row) {
				winners[row] = i
				break
			}
		}
	}

	buildNullRun := func(n int) arrow.Array {
		b := array.NewBuilder(pool, baseType)
		defer b.Release()
		for j := 0; j < n; j++ {
			b.AppendNull()
		}
		return b.NewArray()
	}

	var slices []arrow.Array
	releaseSlices := func() {
		for _, s := range slices {
			s.Release()
		}
	}

	for i := 0; i < numRows; {
		w := winners[i]
		j := i + 1
		for j < numRows && winners[j] == w {
			j++
		}
		var s arrow.Array
		if w == -1 {
			s = buildNullRun(j - i)
		} else {
			s = array.NewSlice(columns[w], int64(i), int64(j))
		}
		slices = append(slices, s)
		i = j
	}

	out, err := array.Concatenate(slices, pool)
	releaseSlices()
	if err != nil {
		return nil, fmt.Errorf("concatenate coalesce slices: %w", err)
	}
	return out, nil
}
