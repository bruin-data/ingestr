package transformer

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
)

type LoadTimestamp struct {
	column    schema.Column
	timestamp time.Time
}

func NewLoadTimestamp(column schema.Column, timestamp time.Time) *LoadTimestamp {
	if column.DataType != schema.TypeTimestamp && column.DataType != schema.TypeTimestampTZ {
		column.DataType = schema.TypeTimestampTZ
	}
	return &LoadTimestamp{
		column:    column,
		timestamp: timestamp.UTC().Truncate(time.Microsecond),
	}
}

func (t *LoadTimestamp) Transform(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	loadArray := t.timestampArray(batch.NumRows())
	batchSchema := batch.Schema()

	fields := make([]arrow.Field, 0, batchSchema.NumFields()+1)
	columns := make([]arrow.Array, 0, batch.NumCols()+1)
	replaced := false

	for i := 0; i < int(batch.NumCols()); i++ {
		field := batchSchema.Field(i)
		if strings.EqualFold(field.Name, t.column.Name) {
			if !replaced {
				fields = append(fields, t.field())
				columns = append(columns, loadArray)
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
		columns = append(columns, loadArray)
	}

	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), columns, batch.NumRows())
	for _, col := range columns {
		col.Release()
	}
	return out, nil
}

func (t *LoadTimestamp) OutputSchema(inputSchema *arrow.Schema) *arrow.Schema {
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

func (t *LoadTimestamp) field() arrow.Field {
	return arrow.Field{
		Name:     t.column.Name,
		Type:     schema.DataTypeToArrowType(t.column),
		Nullable: t.column.Nullable,
	}
}

func (t *LoadTimestamp) timestampArray(numRows int64) arrow.Array {
	arrowType, ok := schema.DataTypeToArrowType(t.column).(*arrow.TimestampType)
	if !ok {
		arrowType = &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	}

	builder := array.NewTimestampBuilder(memory.DefaultAllocator, arrowType)
	defer builder.Release()

	value := arrow.Timestamp(t.timestamp.UnixMicro())
	builder.Reserve(int(numRows))
	for i := int64(0); i < numRows; i++ {
		builder.UnsafeAppend(value)
	}
	return builder.NewArray()
}
