package iceberg

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/source"
)

type requiredColumn struct {
	name  string
	index int
}

type recordBatchReader struct {
	ctx             context.Context
	records         <-chan source.RecordBatchResult
	schema          *arrow.Schema
	requiredColumns []requiredColumn

	current  arrow.RecordBatch
	err      error
	refCount atomic.Int64
}

func newRecordBatchReader(ctx context.Context, records <-chan source.RecordBatchResult, schema *arrow.Schema) *recordBatchReader {
	r := &recordBatchReader{
		ctx:     ctx,
		records: records,
		schema:  schema,
	}
	r.refCount.Store(1)
	return r
}

func newTableRecordBatchReader(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	writeSchema *arrow.Schema,
	tableSchema *iceberggo.Schema,
) (*recordBatchReader, error) {
	writeSchema = applyTableRequirements(writeSchema, tableSchema)
	if err := validateArrowSchemaCompatibility(tableSchema, writeSchema); err != nil {
		return nil, err
	}

	r := newRecordBatchReader(ctx, records, writeSchema)
	identifierIDs := make(map[int]struct{}, len(tableSchema.IdentifierFieldIDs))
	for _, id := range tableSchema.IdentifierFieldIDs {
		identifierIDs[id] = struct{}{}
	}
	for i, field := range writeSchema.Fields() {
		tableField, ok := tableSchema.FindFieldByName(field.Name)
		if !ok {
			continue
		}
		_, identifier := identifierIDs[tableField.ID]
		if tableField.Required || identifier {
			r.requiredColumns = append(r.requiredColumns, requiredColumn{name: field.Name, index: i})
		}
	}
	return r, nil
}

func (r *recordBatchReader) Retain() {
	r.refCount.Add(1)
}

func (r *recordBatchReader) Release() {
	if r.refCount.Add(-1) != 0 {
		return
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
}

func (r *recordBatchReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *recordBatchReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}

	for {
		select {
		case <-r.ctx.Done():
			r.err = r.ctx.Err()
			return false
		case result, ok := <-r.records:
			if !ok {
				return false
			}
			if result.Err != nil {
				r.err = result.Err
				return false
			}
			if result.Batch == nil {
				continue
			}

			batch, err := normalizeRecordBatch(result.Batch, r.schema)
			if err != nil {
				result.Batch.Release()
				r.err = err
				return false
			}
			if err := validateRequiredColumns(batch, r.requiredColumns); err != nil {
				batch.Release()
				r.err = err
				return false
			}
			r.current = batch
			return true
		}
	}
}

func applyTableRequirements(writeSchema *arrow.Schema, tableSchema *iceberggo.Schema) *arrow.Schema {
	fields := writeSchema.Fields()
	identifierIDs := make(map[int]struct{}, len(tableSchema.IdentifierFieldIDs))
	for _, id := range tableSchema.IdentifierFieldIDs {
		identifierIDs[id] = struct{}{}
	}
	for i := range fields {
		tableField, ok := tableSchema.FindFieldByName(fields[i].Name)
		if !ok {
			continue
		}
		_, identifier := identifierIDs[tableField.ID]
		fields[i].Nullable = !tableField.Required && !identifier
	}
	metadata := writeSchema.Metadata()
	return arrow.NewSchema(fields, &metadata)
}

func validateArrowSchemaCompatibility(tableSchema *iceberggo.Schema, writeSchema *arrow.Schema) error {
	if err := validateArrowSchemaCompatibilityWithTimestampUnit(tableSchema, writeSchema, false); err == nil {
		return nil
	}
	return validateArrowSchemaCompatibilityWithTimestampUnit(tableSchema, writeSchema, true)
}

func validateArrowSchemaCompatibilityWithTimestampUnit(
	tableSchema *iceberggo.Schema,
	writeSchema *arrow.Schema,
	downcastNanoToMicro bool,
) error {
	provided, err := icebergtable.ArrowSchemaToIceberg(writeSchema, downcastNanoToMicro, tableSchema.NameMapping())
	if err != nil {
		return err
	}
	for _, field := range tableSchema.Fields() {
		if err := validateIcebergFieldCompatibility(field, provided, field.Name); err != nil {
			return err
		}
	}
	return nil
}

func validateIcebergFieldCompatibility(target iceberggo.NestedField, provided *iceberggo.Schema, path string) error {
	actual, ok := provided.FindFieldByID(target.ID)
	if !ok {
		if target.Required && target.WriteDefault == nil && target.InitialDefault == nil {
			return fmt.Errorf("required field %q is missing", path)
		}
		return nil
	}
	if target.Required && !actual.Required {
		return fmt.Errorf("required field %q is nullable in the write schema", path)
	}

	switch targetType := target.Type.(type) {
	case *iceberggo.StructType:
		if _, ok := actual.Type.(*iceberggo.StructType); !ok {
			return incompatibleIcebergTypeError(path, target.Type, actual.Type)
		}
		for _, field := range targetType.Fields() {
			if err := validateIcebergFieldCompatibility(field, provided, path+"."+field.Name); err != nil {
				return err
			}
		}
		return nil
	case *iceberggo.ListType:
		if _, ok := actual.Type.(*iceberggo.ListType); !ok {
			return incompatibleIcebergTypeError(path, target.Type, actual.Type)
		}
		return validateIcebergFieldCompatibility(targetType.ElementField(), provided, path+".element")
	case *iceberggo.MapType:
		if _, ok := actual.Type.(*iceberggo.MapType); !ok {
			return incompatibleIcebergTypeError(path, target.Type, actual.Type)
		}
		if err := validateIcebergFieldCompatibility(targetType.KeyField(), provided, path+".key"); err != nil {
			return err
		}
		return validateIcebergFieldCompatibility(targetType.ValueField(), provided, path+".value")
	default:
		if target.Type.Equals(actual.Type) {
			return nil
		}
		if _, err := iceberggo.PromoteType(actual.Type, target.Type); err != nil {
			return incompatibleIcebergTypeError(path, target.Type, actual.Type)
		}
		return nil
	}
}

func incompatibleIcebergTypeError(path string, target, actual iceberggo.Type) error {
	return fmt.Errorf("field %q has incompatible type %s, expected %s", path, actual, target)
}

func validateRequiredColumns(batch arrow.RecordBatch, required []requiredColumn) error {
	for _, field := range required {
		if nulls := batch.Column(field.index).NullN(); nulls > 0 {
			return fmt.Errorf("required field %q contains %d NULL value(s)", field.name, nulls)
		}
	}
	return nil
}

func (r *recordBatchReader) RecordBatch() arrow.RecordBatch {
	return r.current
}

func (r *recordBatchReader) Record() arrow.RecordBatch {
	return r.current
}

func (r *recordBatchReader) Err() error {
	return r.err
}

func normalizeRecordBatch(batch arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if batch.Schema().Equal(target) {
		return batch, nil
	}
	if int(batch.NumCols()) != len(target.Fields()) {
		return nil, fmt.Errorf("record batch column count mismatch: got %d, want %d", batch.NumCols(), len(target.Fields()))
	}

	cols := make([]arrow.Array, int(batch.NumCols()))
	var converted []arrow.Array
	defer func() {
		for _, col := range converted {
			col.Release()
		}
	}()
	for i := 0; i < int(batch.NumCols()); i++ {
		sourceField := batch.Schema().Field(i)
		targetField := target.Field(i)
		if sourceField.Name != targetField.Name {
			return nil, fmt.Errorf("record batch column %d name mismatch: got %q, want %q", i, sourceField.Name, targetField.Name)
		}

		col := batch.Column(i)
		if !arrow.TypeEqual(col.DataType(), targetField.Type) {
			normalized, release, err := normalizeColumn(col, targetField.Type)
			if err != nil {
				return nil, fmt.Errorf("record batch column %q type mismatch: %w", sourceField.Name, err)
			}
			if release {
				converted = append(converted, normalized)
			}
			col = normalized
		}
		cols[i] = col
	}

	normalized := array.NewRecordBatch(target, cols, batch.NumRows())
	batch.Release()
	return normalized, nil
}

func normalizeColumn(col arrow.Array, target arrow.DataType) (arrow.Array, bool, error) {
	if ext, ok := col.(array.ExtensionArray); ok && arrow.TypeEqual(ext.Storage().DataType(), target) {
		return ext.Storage(), false, nil
	}
	if _, ok := target.(*extensions.UUIDType); ok {
		converted, err := uuidStringArray(col)
		return converted, true, err
	}
	return nil, false, fmt.Errorf("got %s, want %s", col.DataType(), target)
}

func uuidStringArray(col arrow.Array) (arrow.Array, error) {
	strings, ok := col.(array.StringLike)
	if !ok {
		return nil, fmt.Errorf("got %s, want %s", col.DataType(), extensions.NewUUIDType())
	}
	builder := extensions.NewUUIDBuilder(memory.DefaultAllocator)
	defer builder.Release()

	for i := 0; i < strings.Len(); i++ {
		if strings.IsNull(i) {
			builder.AppendNull()
			continue
		}
		if err := builder.AppendValueFromString(strings.Value(i)); err != nil {
			return nil, err
		}
	}
	return builder.NewArray(), nil
}
