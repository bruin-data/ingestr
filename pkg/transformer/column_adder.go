package transformer

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// ColumnSpec defines a column to add with its value generator.
type ColumnSpec struct {
	Column    schema.Column
	Generator func(rowIndex int, numRows int64) interface{}
}

// ColumnAdder adds new columns to record batches.
type ColumnAdder struct {
	columns []ColumnSpec
}

// NewColumnAdder creates a new ColumnAdder with the specified columns.
func NewColumnAdder(columns ...ColumnSpec) *ColumnAdder {
	return &ColumnAdder{
		columns: columns,
	}
}

// Transform adds the configured columns to the batch.
func (c *ColumnAdder) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	numRows := batch.NumRows()

	// Collect existing columns
	existingCols := make([]arrow.Array, batch.NumCols())
	for i := 0; i < int(batch.NumCols()); i++ {
		existingCols[i] = batch.Column(i)
		existingCols[i].Retain()
	}

	// Build new columns
	newCols := make([]arrow.Array, len(c.columns))
	for i, spec := range c.columns {
		arr, err := buildColumn(spec, numRows)
		if err != nil {
			for j := 0; j < i; j++ {
				newCols[j].Release()
			}
			for _, col := range existingCols {
				col.Release()
			}
			return nil, fmt.Errorf("failed to build column %s: %w", spec.Column.Name, err)
		}
		newCols[i] = arr
	}

	// Combine all columns
	allCols := append(existingCols, newCols...)

	// Build new schema
	outputSchema := c.OutputSchema(batch.Schema())

	// Create new record batch
	newBatch := array.NewRecordBatch(outputSchema, allCols, numRows)

	// Release our references to the columns (NewRecord retains them)
	for _, col := range allCols {
		col.Release()
	}

	return newBatch, nil
}

// OutputSchema returns the schema with added columns.
func (c *ColumnAdder) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	fields := make([]arrow.Field, len(inputSchema.Fields())+len(c.columns))
	copy(fields, inputSchema.Fields())

	for i, spec := range c.columns {
		fields[len(inputSchema.Fields())+i] = arrow.Field{
			Name:     spec.Column.Name,
			Type:     schema.DataTypeToArrowType(spec.Column),
			Nullable: spec.Column.Nullable,
		}
	}

	return arrow.NewSchema(fields, nil)
}

func buildColumn(spec ColumnSpec, numRows int64) (arrow.Array, error) {
	allocator := memory.DefaultAllocator
	arrowType := schema.DataTypeToArrowType(spec.Column)

	switch arrowType.(type) {
	case *arrow.BooleanType:
		builder := array.NewBooleanBuilder(allocator)
		defer builder.Release()
		for i := int64(0); i < numRows; i++ {
			val := spec.Generator(int(i), numRows)
			if val == nil {
				builder.AppendNull()
			} else {
				builder.Append(val.(bool))
			}
		}
		return builder.NewArray(), nil

	case *arrow.TimestampType:
		tsType := arrowType.(*arrow.TimestampType)
		builder := array.NewTimestampBuilder(allocator, tsType)
		defer builder.Release()
		for i := int64(0); i < numRows; i++ {
			val := spec.Generator(int(i), numRows)
			if val == nil {
				builder.AppendNull()
			} else {
				switch v := val.(type) {
				case time.Time:
					builder.Append(arrow.Timestamp(v.UnixMicro()))
				case arrow.Timestamp:
					builder.Append(v)
				default:
					return nil, fmt.Errorf("unsupported timestamp value type: %T", val)
				}
			}
		}
		return builder.NewArray(), nil

	case *arrow.StringType:
		builder := array.NewStringBuilder(allocator)
		defer builder.Release()
		for i := int64(0); i < numRows; i++ {
			val := spec.Generator(int(i), numRows)
			if val == nil {
				builder.AppendNull()
			} else {
				builder.Append(val.(string))
			}
		}
		return builder.NewArray(), nil

	case *arrow.Int64Type:
		builder := array.NewInt64Builder(allocator)
		defer builder.Release()
		for i := int64(0); i < numRows; i++ {
			val := spec.Generator(int(i), numRows)
			if val == nil {
				builder.AppendNull()
			} else {
				builder.Append(val.(int64))
			}
		}
		return builder.NewArray(), nil

	case *arrow.Float64Type:
		builder := array.NewFloat64Builder(allocator)
		defer builder.Release()
		for i := int64(0); i < numRows; i++ {
			val := spec.Generator(int(i), numRows)
			if val == nil {
				builder.AppendNull()
			} else {
				builder.Append(val.(float64))
			}
		}
		return builder.NewArray(), nil

	default:
		return nil, fmt.Errorf("unsupported column type: %v", arrowType)
	}
}
