package schemaevolution

import (
	"context"
	"fmt"

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
}

// NewDiscardValueTransformer creates a transformer for discard_value mode.
// It accepts srcSchema and dstSchema for context, but uses the comparison to determine violations.
func NewDiscardValueTransformer(comparison *SchemaComparison, _ *schema.TableSchema, destSchema *schema.TableSchema) *DiscardValueTransformer {
	transformer := &DiscardValueTransformer{
		violations: make(map[string]bool),
		destSchema: destSchema,
	}

	if comparison != nil {
		for _, change := range comparison.Changes {
			if change.Type == ChangeAddColumn {
				// New columns are allowed in discard_value mode, keep their values.
				continue
			}
			// Track type incompatibilities to NULL out values.
			transformer.violations[change.ColumnName] = true
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

	// For each column with violations, replace with NULL values
	for colIdx := 0; colIdx < int(batch.NumCols()); colIdx++ {
		colName := batchSchema.Field(colIdx).Name
		if t.violations[colName] {
			// Violation: discard value (set to NULL)
			// If the column exists in destination schema, use destination type (to satisfy strict typing)
			// Otherwise use source type (e.g. for rejected new columns)
			field := batchSchema.Field(colIdx)
			targetType := field.Type
			if t.destSchema != nil {
				for _, destCol := range t.destSchema.Columns {
					if destCol.Name == colName {
						targetType = schema.DataTypeToArrowType(destCol)
						break
					}
				}
			}

			builder := array.NewBuilder(memory.DefaultAllocator, targetType)
			defer builder.Release()

			for i := 0; i < int(batch.NumRows()); i++ {
				builder.AppendNull()
			}
			columns[colIdx] = builder.NewArray()
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
	return newBatch, nil
}

// DiscardRowTransformer filters out rows with incompatible values.
type DiscardRowTransformer struct {
	violations map[string]bool // column name -> has violation
	schema     *schema.TableSchema
	destSchema *schema.TableSchema
}

// NewDiscardRowTransformer creates a transformer for discard_row mode.
func NewDiscardRowTransformer(sourceSchema, destSchema *schema.TableSchema, comparison *SchemaComparison) *DiscardRowTransformer {
	transformer := &DiscardRowTransformer{
		violations: make(map[string]bool),
		schema:     sourceSchema,
		destSchema: destSchema,
	}

	if comparison != nil {
		for _, change := range comparison.Changes {
			// Track all violations (both new columns and type mismatches)
			transformer.violations[change.ColumnName] = true
		}
	}

	return transformer
}

// Transform filters out rows where violation columns would cause issues.
func (t *DiscardRowTransformer) Transform(ctx context.Context, batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(t.violations) == 0 {
		return batch, nil
	}

	// Find column indices that have violations
	violationCols := make(map[int]string)
	schema := batch.Schema()
	for i := 0; i < schema.NumFields(); i++ {
		colName := schema.Field(i).Name
		if t.violations[colName] {
			violationCols[i] = colName
		}
	}

	if len(violationCols) == 0 {
		return batch, nil
	}

	// Create a boolean filter array: true for rows to keep, false for rows to discard
	// Keep rows where all violation columns are NULL
	filterBuilder := array.NewBooleanBuilder(memory.DefaultAllocator)
	defer filterBuilder.Release()

	for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
		keepRow := true

		// Check if any violation column has a non-NULL value
		for colIdx := range violationCols {
			col := batch.Column(colIdx)
			if col.IsValid(rowIdx) {
				// This row has data in a violation column, discard it
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
	filtered, err := compute.FilterRecordBatch(ctx, batch, filterArray, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to filter rows: %w", err)
	}

	if filtered == nil || filtered.NumRows() == 0 {
		// Return empty batch if all rows filtered out
		return createEmptyBatch(batch), nil
	}

	return filtered, nil
}

// TransformBatchStream wraps a batch channel and applies transformation to each batch.
func TransformBatchStream(ctx context.Context, batches <-chan source.RecordBatchResult, transformer BatchTransformer) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)

	go func() {
		defer close(out)

		for result := range batches {
			// Pass through errors
			if result.Err != nil {
				out <- result
				continue
			}

			// Transform batch
			transformed, err := transformer.Transform(ctx, result.Batch)
			if err != nil {
				out <- source.RecordBatchResult{Err: err}
				continue
			}

			out <- source.RecordBatchResult{Batch: transformed}
		}
	}()

	return out
}

// RemovedColumnTransformer adds NULL columns for columns that exist in destination but not in source.
type RemovedColumnTransformer struct {
	removedColumns []schema.Column
}

// NewRemovedColumnTransformer creates a transformer that adds NULL columns for removed columns.
func NewRemovedColumnTransformer(comparison *SchemaComparison) *RemovedColumnTransformer {
	transformer := &RemovedColumnTransformer{
		removedColumns: make([]schema.Column, 0),
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

		builder := array.NewBuilder(memory.DefaultAllocator, arrowType)
		for j := 0; j < int(batch.NumRows()); j++ {
			builder.AppendNull()
		}
		columns[idx] = builder.NewArray()
		builder.Release()
	}

	newSchema := arrow.NewSchema(newFields, nil)
	return array.NewRecordBatch(newSchema, columns, batch.NumRows()), nil
}

// IngestrColumnFiller adds ingestr metadata columns with "-" to each batch.
type IngestrColumnFiller struct {
	columnNames []string
}

func NewIngestrColumnFiller(columnNames []string) *IngestrColumnFiller {
	return &IngestrColumnFiller{columnNames: columnNames}
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

		builder := array.NewStringBuilder(memory.DefaultAllocator)
		for j := 0; j < int(batch.NumRows()); j++ {
			builder.Append("-")
		}
		columns[idx] = builder.NewArray()
		builder.Release()
	}

	newSchema := arrow.NewSchema(newFields, nil)
	return array.NewRecordBatch(newSchema, columns, batch.NumRows()), nil
}

// Helper functions

func createEmptyBatch(template arrow.RecordBatch) arrow.RecordBatch {
	schema := template.Schema()
	emptyColumns := make([]arrow.Array, schema.NumFields())

	for i := 0; i < schema.NumFields(); i++ {
		builder := array.NewBuilder(memory.DefaultAllocator, schema.Field(i).Type)
		defer builder.Release()
		emptyColumns[i] = builder.NewArray()
	}

	return array.NewRecordBatch(schema, emptyColumns, 0)
}
