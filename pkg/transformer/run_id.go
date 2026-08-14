package transformer

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type RunID struct {
	column schema.Column
	value  string
}

func NewRunID(column schema.Column, value string) *RunID {
	column.DataType = schema.TypeString
	return &RunID{
		column: column,
		value:  value,
	}
}

func (t *RunID) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	runArray := t.runIDArray(batch.NumRows())
	batchSchema := batch.Schema()

	fields := make([]arrow.Field, 0, batchSchema.NumFields()+1)
	columns := make([]arrow.Array, 0, batch.NumCols()+1)
	replaced := false

	for i := 0; i < int(batch.NumCols()); i++ {
		field := batchSchema.Field(i)
		if strings.EqualFold(field.Name, t.column.Name) {
			if !replaced {
				fields = append(fields, t.field())
				columns = append(columns, runArray)
				replaced = true
			}
			continue
		}

		fields = append(fields, field)
		col := batch.Column(i)
		col.Retain()
		columns = append(columns, col)
	}

	if !replaced {
		fields = append(fields, t.field())
		columns = append(columns, runArray)
	}

	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, batch.NumRows())
	for _, col := range columns {
		col.Release()
	}
	return out, nil
}

func (t *RunID) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
	fields := make([]arrow.Field, 0, inputSchema.NumFields()+1)
	replaced := false

	for i := 0; i < inputSchema.NumFields(); i++ {
		field := inputSchema.Field(i)
		if strings.EqualFold(field.Name, t.column.Name) {
			if !replaced {
				fields = append(fields, t.field())
				replaced = true
			}
			continue
		}
		fields = append(fields, field)
	}

	if !replaced {
		fields = append(fields, t.field())
	}

	return arrow.NewSchema(fields, nil)
}

func (t *RunID) field() arrow.Field {
	return arrow.Field{
		Name:     t.column.Name,
		Type:     schema.DataTypeToArrowType(t.column),
		Nullable: t.column.Nullable,
	}
}

func (t *RunID) runIDArray(numRows int64) arrow.Array {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()

	builder.Reserve(int(numRows))
	for i := int64(0); i < numRows; i++ {
		builder.Append(t.value)
	}
	return builder.NewArray()
}
