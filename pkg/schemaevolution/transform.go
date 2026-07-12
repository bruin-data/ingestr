package schemaevolution

import (
	"context"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

// BatchTransformer handles runtime transformation of Arrow batches based on schema contract violations.
type BatchTransformer interface {
	Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error)
}

// DiscardValueTransformer sets non-conforming values to NULL for type mismatches.
type DiscardValueTransformer struct {
	violations map[string]bool // column name -> has violation
	destSchema *schema.TableSchema
	allocator  memory.Allocator
}

// NewDiscardValueTransformer creates a transformer for discard_value mode.
// It accepts srcSchema and dstSchema for context, but uses the comparison to determine violations.
func NewDiscardValueTransformer(comparison *SchemaComparison, _ *schema.TableSchema, destSchema *schema.TableSchema) *DiscardValueTransformer {
	transformer := &DiscardValueTransformer{
		violations: make(map[string]bool),
		destSchema: destSchema,
		allocator:  memory.DefaultAllocator,
	}

	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type == ChangeAddColumn || change.Type == ChangeRemoveColumn || change.Type == ChangeRelaxNullability {
				// New columns are allowed in discard_value mode, keep their values.
				// Removed source fields are filled during schema alignment; they do not
				// invalidate conforming siblings in the same nested parent.
				// Nullability relaxation is applied as schema evolution because NULL
				// cannot be transformed into a value accepted by a required column.
				continue
			}
			// Track type incompatibilities to NULL out values.
			path := changePath(change)
			transformer.violations[strings.ToLower(path[0])] = true
		}
	}

	return transformer
}

// Transform sets values in violation columns to NULL.
func (t *DiscardValueTransformer) Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(t.violations) == 0 {
		return batch, nil
	}

	// Create a copy of the schema to modify
	batchSchema := batch.Schema()
	columns := make([]arrow.Array, batch.NumCols())
	allocator := allocatorOrDefault(t.allocator)

	// For each column with violations, replace with NULL values
	for colIdx := 0; colIdx < int(batch.NumCols()); colIdx++ {
		colName := batchSchema.Field(colIdx).Name
		if t.violations[strings.ToLower(colName)] {
			// Violation: discard value (set to NULL)
			// If the column exists in destination schema, use destination type (to satisfy strict typing)
			// Otherwise use source type (e.g. for rejected new columns)
			field := batchSchema.Field(colIdx)
			targetType := field.Type
			if t.destSchema != nil {
				for _, destCol := range t.destSchema.Columns {
					if strings.EqualFold(destCol.Name, colName) {
						targetType = schema.DataTypeToArrowType(destCol)
						break
					}
				}
			}

			builder := array.NewBuilder(allocator, targetType)

			for i := 0; i < int(batch.NumRows()); i++ {
				builder.AppendNull()
			}
			columns[colIdx] = builder.NewArray()
			builder.Release()
		} else {
			// Keep original column
			columns[colIdx] = batch.Column(colIdx)
		}
	}

	// Create new batch with modified columns
	// Note: We need to reconstruct the schema because column types might have changed
	newFields := make([]arrow.Field, len(columns))
	for i, col := range columns {
		oldField := batchSchema.Field(i)
		newFields[i] = arrow.Field{
			Name:     oldField.Name,
			Type:     col.DataType(),
			Nullable: true, // Force nullable as we might have introduced NULLs
			Metadata: oldField.Metadata,
		}
	}
	newSchema := arrow.NewSchema(newFields, nil)
	newBatch := array.NewRecordBatch(newSchema, columns, batch.NumRows())
	for i, col := range columns {
		if t.violations[strings.ToLower(batchSchema.Field(i).Name)] {
			col.Release()
		}
	}
	return newBatch, nil
}

// DiscardRowTransformer filters out rows with incompatible values.
type DiscardRowTransformer struct {
	violations []SchemaChange
	schema     *schema.TableSchema
	destSchema *schema.TableSchema
	allocator  memory.Allocator
}

// NewDiscardRowTransformer creates a transformer for discard_row mode.
func NewDiscardRowTransformer(sourceSchema, destSchema *schema.TableSchema, comparison *SchemaComparison) *DiscardRowTransformer {
	transformer := &DiscardRowTransformer{
		violations: make([]SchemaChange, 0),
		schema:     sourceSchema,
		destSchema: destSchema,
		allocator:  memory.DefaultAllocator,
	}

	if comparison != nil {
		for _, change := range comparison.Changes {
			if isDiscardRowAllowedChange(change) {
				continue
			}
			// Track all violations (both new columns and type mismatches)
			transformer.violations = append(transformer.violations, change)
		}
	}

	return transformer
}

// Transform filters out rows where violation columns would cause issues.
func (t *DiscardRowTransformer) Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(t.violations) == 0 {
		return batch, nil
	}

	allocator := allocatorOrDefault(t.allocator)

	// Create a boolean filter array: true for rows to keep, false for rows to discard
	// Keep rows where all violation columns are NULL
	filterBuilder := array.NewBooleanBuilder(allocator)
	defer filterBuilder.Release()

	for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
		keepRow := true

		for _, violation := range t.violations {
			path := changePath(violation)
			colIdx := fieldIndexFold(batch.Schema(), path[0])
			if colIdx < 0 {
				continue
			}
			if rowViolatesChange(batch.Column(colIdx), path[1:], rowIdx) {
				keepRow = false
				break
			}
		}

		filterBuilder.Append(keepRow)
	}
	filterArray := filterBuilder.NewArray()
	defer filterArray.Release()

	// Apply filter using compute function
	opts := compute.DefaultFilterOptions()
	filtered, err := compute.FilterRecordBatch(compute.WithAllocator(ctx, allocator), batch, filterArray, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to filter rows: %w", err)
	}

	if filtered == nil || filtered.NumRows() == 0 {
		if filtered != nil {
			filtered.Release()
		}
		// Return empty batch if all rows filtered out
		return createEmptyBatchWithAllocator(batch, allocator), nil
	}

	return filtered, nil
}

func changePath(change SchemaChange) []string {
	if len(change.ColumnPath) > 0 {
		return change.ColumnPath
	}
	return strings.Split(change.ColumnName, ".")
}

func fieldIndexFold(sc *arrow.Schema, name string) int {
	for i, field := range sc.Fields() {
		if strings.EqualFold(field.Name, name) {
			return i
		}
	}
	return -1
}

func rowViolatesChange(values arrow.Array, path []string, row int) bool {
	if len(path) == 0 {
		return values.IsValid(row)
	}
	if values.IsNull(row) {
		return false
	}

	switch nested := values.(type) {
	case *array.Struct:
		structType := nested.DataType().(*arrow.StructType)
		for i, field := range structType.Fields() {
			if strings.EqualFold(field.Name, path[0]) {
				return rowViolatesChange(nested.Field(i), path[1:], row)
			}
		}
	case *array.List:
		if path[0] != "element" {
			return false
		}
		start, end := nested.ValueOffsets(row)
		for i := start; i < end; i++ {
			if rowViolatesChange(nested.ListValues(), path[1:], int(i)) {
				return true
			}
		}
	case *array.LargeList:
		if path[0] != "element" {
			return false
		}
		start, end := nested.ValueOffsets(row)
		for i := start; i < end; i++ {
			if rowViolatesChange(nested.ListValues(), path[1:], int(i)) {
				return true
			}
		}
	case *array.FixedSizeList:
		if path[0] != "element" {
			return false
		}
		start, end := nested.ValueOffsets(row)
		for i := start; i < end; i++ {
			if rowViolatesChange(nested.ListValues(), path[1:], int(i)) {
				return true
			}
		}
	case *array.Map:
		start, end := nested.ValueOffsets(row)
		var child arrow.Array
		switch path[0] {
		case "key":
			child = nested.Keys()
		case "value":
			child = nested.Items()
		default:
			return false
		}
		for i := start; i < end; i++ {
			if rowViolatesChange(child, path[1:], int(i)) {
				return true
			}
		}
	}
	return false
}

// TransformBatchStream wraps a batch channel and applies transformation to each batch.
func TransformBatchStream(ctx context.Context, batches <-chan source.RecordBatchResult, transformer BatchTransformer) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)

	go func() {
		defer close(out)

		for result := range batches {
			if result.Err != nil || result.Batch == nil {
				out <- result
				continue
			}

			transformed, err := transformer.Transform(ctx, result.Batch)
			if err != nil {
				result.Batch.Release()
				out <- source.RecordBatchResult{Err: err, TableName: result.TableName}
				continue
			}

			if transformed != result.Batch {
				result.Batch.Release()
			}
			result.Batch = transformed
			out <- result
		}
	}()

	return out
}

// RemovedColumnTransformer adds NULL columns for columns that exist in destination but not in source.
type RemovedColumnTransformer struct {
	removedColumns []schema.Column
	allocator      memory.Allocator
}

// NewRemovedColumnTransformer creates a transformer that adds NULL columns for removed columns.
func NewRemovedColumnTransformer(comparison *SchemaComparison) *RemovedColumnTransformer {
	transformer := &RemovedColumnTransformer{
		removedColumns: make([]schema.Column, 0),
		allocator:      memory.DefaultAllocator,
	}

	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type == ChangeRemoveColumn && change.OldColumn != nil {
				transformer.removedColumns = append(transformer.removedColumns, *change.OldColumn)
			}
		}
	}

	return transformer
}

// HasRemovedColumns returns true if there are columns to add.
func (t *RemovedColumnTransformer) HasRemovedColumns() bool {
	return len(t.removedColumns) > 0
}

// RemovedColumns returns the list of removed columns.
func (t *RemovedColumnTransformer) RemovedColumns() []schema.Column {
	return t.removedColumns
}

// Transform adds NULL columns for removed columns.
func (t *RemovedColumnTransformer) Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(t.removedColumns) == 0 {
		return batch, nil
	}

	batchSchema := batch.Schema()
	newNumCols := int(batch.NumCols()) + len(t.removedColumns)
	newFields := make([]arrow.Field, newNumCols)
	columns := make([]arrow.Array, newNumCols)
	allocator := allocatorOrDefault(t.allocator)

	// Copy existing columns
	for i := 0; i < int(batch.NumCols()); i++ {
		newFields[i] = batchSchema.Field(i)
		columns[i] = batch.Column(i)
	}

	// Add NULL columns for removed columns
	for i, col := range t.removedColumns {
		idx := int(batch.NumCols()) + i
		arrowType := schema.DataTypeToArrowType(col)
		newFields[idx] = arrow.Field{
			Name:     col.Name,
			Type:     arrowType,
			Nullable: true,
		}

		builder := array.NewBuilder(allocator, arrowType)
		for j := 0; j < int(batch.NumRows()); j++ {
			builder.AppendNull()
		}
		columns[idx] = builder.NewArray()
		builder.Release()
	}

	newSchema := arrow.NewSchema(newFields, nil)
	newBatch := array.NewRecordBatch(newSchema, columns, batch.NumRows())
	for i := int(batch.NumCols()); i < len(columns); i++ {
		columns[i].Release()
	}
	return newBatch, nil
}

// IngestrColumnFiller adds ingestr metadata columns with "-" to each batch.
type IngestrColumnFiller struct {
	columnNames []string
	allocator   memory.Allocator
}

func NewIngestrColumnFiller(columnNames []string) *IngestrColumnFiller {
	return &IngestrColumnFiller{columnNames: columnNames, allocator: memory.DefaultAllocator}
}

func (t *IngestrColumnFiller) HasColumns() bool {
	return len(t.columnNames) > 0
}

func (t *IngestrColumnFiller) Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(t.columnNames) == 0 {
		return batch, nil
	}

	batchSchema := batch.Schema()

	// Filter out columns that already exist in the batch
	existingCols := make(map[string]bool, batchSchema.NumFields())
	for i := 0; i < batchSchema.NumFields(); i++ {
		existingCols[batchSchema.Field(i).Name] = true
	}
	var colsToAdd []string
	for _, colName := range t.columnNames {
		if !existingCols[colName] {
			colsToAdd = append(colsToAdd, colName)
		}
	}
	if len(colsToAdd) == 0 {
		return batch, nil
	}

	newNumCols := int(batch.NumCols()) + len(colsToAdd)
	newFields := make([]arrow.Field, newNumCols)
	columns := make([]arrow.Array, newNumCols)
	allocator := allocatorOrDefault(t.allocator)

	for i := 0; i < int(batch.NumCols()); i++ {
		newFields[i] = batchSchema.Field(i)
		columns[i] = batch.Column(i)
	}

	for i, colName := range colsToAdd {
		idx := int(batch.NumCols()) + i
		newFields[idx] = arrow.Field{
			Name:     colName,
			Type:     arrow.BinaryTypes.String,
			Nullable: false,
		}

		builder := array.NewStringBuilder(allocator)
		for j := 0; j < int(batch.NumRows()); j++ {
			builder.Append("-")
		}
		columns[idx] = builder.NewArray()
		builder.Release()
	}

	newSchema := arrow.NewSchema(newFields, nil)
	newBatch := array.NewRecordBatch(newSchema, columns, batch.NumRows())
	for i := int(batch.NumCols()); i < len(columns); i++ {
		columns[i].Release()
	}
	return newBatch, nil
}

// Helper functions

func createEmptyBatchWithAllocator(template arrow.RecordBatch, allocator memory.Allocator) arrow.RecordBatch {
	schema := template.Schema()
	emptyColumns := make([]arrow.Array, schema.NumFields())
	allocator = allocatorOrDefault(allocator)

	for i := 0; i < schema.NumFields(); i++ {
		builder := array.NewBuilder(allocator, schema.Field(i).Type)
		emptyColumns[i] = builder.NewArray()
		builder.Release()
	}

	emptyBatch := array.NewRecordBatch(schema, emptyColumns, 0)
	for _, col := range emptyColumns {
		col.Release()
	}
	return emptyBatch
}

func allocatorOrDefault(allocator memory.Allocator) memory.Allocator {
	if allocator != nil {
		return allocator
	}
	return memory.DefaultAllocator
}
