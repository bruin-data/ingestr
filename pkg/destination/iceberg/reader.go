package iceberg

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	writeInputDrainTimeout   = 30 * time.Second
	immediateWriteDrainLimit = 64
)

// recordBatchInput owns the source channel and the reader consuming it. Close
// releases the reader's current batch before taking over the channel, then
// drains any trailing batches without making a failed write wait on a slow or
// broken producer forever.
type recordBatchInput struct {
	records      <-chan source.RecordBatchResult
	drainTimeout time.Duration
	done         chan struct{}

	mu            sync.Mutex
	reader        *recordBatchReader
	readerCreated bool
	closed        bool
	closeOnce     sync.Once
}

func newRecordBatchInput(records <-chan source.RecordBatchResult) *recordBatchInput {
	return newRecordBatchInputWithDrainTimeout(records, writeInputDrainTimeout)
}

func newRecordBatchInputWithDrainTimeout(
	records <-chan source.RecordBatchResult,
	drainTimeout time.Duration,
) *recordBatchInput {
	return &recordBatchInput{
		records:      records,
		drainTimeout: drainTimeout,
		done:         make(chan struct{}),
	}
}

func (i *recordBatchInput) RecordReader(ctx context.Context, schema *arrow.Schema) *recordBatchReader {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.readerCreated {
		panic("iceberg: record batch input reader requested more than once")
	}
	i.readerCreated = true
	i.reader = newRecordBatchReader(ctx, i.records, schema)
	return i.reader
}

func (i *recordBatchInput) ReleaseReader() {
	i.mu.Lock()
	reader := i.reader
	i.reader = nil
	i.mu.Unlock()
	if reader != nil {
		reader.Release()
	}
}

func (i *recordBatchInput) Drain(ctx context.Context) error {
	if i.records == nil {
		return nil
	}
	timer := time.NewTimer(i.drainTimeout)
	defer timer.Stop()
	var sourceErr error
	for {
		select {
		case result, ok := <-i.records:
			if !ok {
				return sourceErr
			}
			releaseWriteResult(result)
			if sourceErr == nil && result.Err != nil {
				sourceErr = result.Err
			}
		case <-ctx.Done():
			return errors.Join(sourceErr, ctx.Err())
		case <-timer.C:
			return errors.Join(sourceErr, fmt.Errorf("iceberg: timed out draining write input after %s", i.drainTimeout))
		}
	}
}

func (i *recordBatchInput) Close() {
	i.closeOnce.Do(func() {
		i.mu.Lock()
		i.closed = true
		i.mu.Unlock()
		i.ReleaseReader()

		if i.records == nil {
			close(i.done)
			return
		}
		if drainAvailableWriteRecords(i.records, immediateWriteDrainLimit) {
			close(i.done)
			return
		}
		go func() {
			defer close(i.done)
			drainTrailingWriteRecords(i.records, i.drainTimeout)
		}()
	})
}

func drainAvailableWriteRecords(records <-chan source.RecordBatchResult, limit int) bool {
	for range limit {
		select {
		case result, ok := <-records:
			if !ok {
				return true
			}
			releaseWriteResult(result)
		default:
			return false
		}
	}
	return false
}

func drainTrailingWriteRecords(records <-chan source.RecordBatchResult, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case result, ok := <-records:
			if !ok {
				return
			}
			releaseWriteResult(result)
		case <-timer.C:
			return
		}
	}
}

func releaseWriteResult(result source.RecordBatchResult) {
	if result.Batch != nil {
		result.Batch.Release()
	}
}

type recordBatchReader struct {
	ctx     context.Context
	records <-chan source.RecordBatchResult
	schema  *arrow.Schema

	current               arrow.RecordBatch
	err                   error
	observe               func(arrow.RecordBatch)
	durableCommitPosition string
	refCount              atomic.Int64
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
		// A canceled context must win over a ready batch: the select alone
		// picks randomly, letting a canceled write occasionally commit.
		if err := r.ctx.Err(); err != nil {
			r.err = err
			return false
		}
		select {
		case <-r.ctx.Done():
			r.err = r.ctx.Err()
			return false
		case result, ok := <-r.records:
			if !ok {
				return false
			}
			if result.Err != nil {
				releaseWriteResult(result)
				r.err = result.Err
				return false
			}
			if compareCDCResumeLSN(result.DurableCommitPosition, r.durableCommitPosition) > 0 {
				r.durableCommitPosition = result.DurableCommitPosition
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
			r.current = batch
			if r.observe != nil {
				r.observe(batch)
			}
			return true
		}
	}
}

func (r *recordBatchReader) DurableCommitPosition() string { return r.durableCommitPosition }

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
		if err := validateRequiredNestedBatch(batch, target); err != nil {
			return nil, err
		}
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
	if err := validateRequiredNestedBatch(normalized, target); err != nil {
		normalized.Release()
		return nil, err
	}
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
	if fixed, ok := target.(*arrow.FixedSizeBinaryType); ok {
		converted, err := fixedSizeBinaryArray(col, fixed)
		return converted, true, err
	}
	if listType, ok := target.(*arrow.ListType); ok {
		converted, err := normalizedListArray(col, listType)
		return converted, true, err
	}
	if _, ok := target.(*arrow.StructType); ok {
		converted, err := normalizedNestedArray(col, target)
		return converted, true, err
	}
	if _, ok := target.(*arrow.MapType); ok {
		converted, err := normalizedNestedArray(col, target)
		return converted, true, err
	}
	return nil, false, fmt.Errorf("got %s, want %s", col.DataType(), target)
}

func normalizedNestedArray(col arrow.Array, target arrow.DataType) (arrow.Array, error) {
	builder := array.NewBuilder(memory.DefaultAllocator, target)
	defer builder.Release()
	for i := 0; i < col.Len(); i++ {
		value, err := rowValue(col, i)
		if err != nil {
			return nil, err
		}
		if err := appendValueAtPath(builder, value, fmt.Sprintf("value[%d]", i), true); err != nil {
			return nil, err
		}
	}
	return builder.NewArray(), nil
}

func validateRequiredNestedBatch(batch arrow.RecordBatch, target *arrow.Schema) error {
	for col := 0; col < int(batch.NumCols()); col++ {
		field := target.Field(col)
		if err := validateRequiredArray(batch.Column(col), field, field.Name, nil); err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredArray(values arrow.Array, field arrow.Field, valuePath string, active []bool) error {
	if !field.Nullable {
		for i := 0; i < values.Len(); i++ {
			if (active == nil || active[i]) && values.IsNull(i) {
				return fmt.Errorf("required nested value %s[%d] is null", valuePath, i)
			}
		}
	}
	switch typed := values.(type) {
	case *array.Struct:
		structType := field.Type.(*arrow.StructType)
		childActive := make([]bool, typed.Len())
		for row := range childActive {
			childActive[row] = (active == nil || active[row]) && !typed.IsNull(row)
		}
		for i := 0; i < typed.NumField(); i++ {
			child := structType.Field(i)
			if err := validateRequiredArray(typed.Field(i), child, valuePath+"."+child.Name, childActive); err != nil {
				return err
			}
		}
	case *array.Map:
		mapType := field.Type.(*arrow.MapType)
		entryActive := listChildActivity(typed, active)
		if err := validateRequiredArray(typed.Keys(), mapType.KeyField(), valuePath+".key", entryActive); err != nil {
			return err
		}
		if err := validateRequiredArray(typed.Items(), mapType.ItemField(), valuePath+".value", entryActive); err != nil {
			return err
		}
	case array.ListLike:
		var elem arrow.Field
		switch listType := field.Type.(type) {
		case *arrow.ListType:
			elem = listType.ElemField()
		case *arrow.LargeListType:
			elem = listType.ElemField()
		default:
			return nil
		}
		if err := validateRequiredArray(typed.ListValues(), elem, valuePath+".element", listChildActivity(typed, active)); err != nil {
			return err
		}
	}
	return nil
}

func listChildActivity(list array.ListLike, active []bool) []bool {
	child := make([]bool, list.ListValues().Len())
	for row := 0; row < list.Len(); row++ {
		if (active != nil && !active[row]) || list.IsNull(row) {
			continue
		}
		start, end := list.ValueOffsets(row)
		for index := start; index < end; index++ {
			child[index] = true
		}
	}
	return child
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

func fixedSizeBinaryArray(col arrow.Array, target *arrow.FixedSizeBinaryType) (arrow.Array, error) {
	col = extensionStorage(col)
	builder := array.NewFixedSizeBinaryBuilder(memory.DefaultAllocator, target)
	defer builder.Release()

	for i := 0; i < col.Len(); i++ {
		if col.IsNull(i) {
			builder.AppendNull()
			continue
		}
		value, ok := binaryValue(col, i)
		if !ok {
			return nil, fmt.Errorf("got %s, want %s", col.DataType(), target)
		}
		if len(value) != target.ByteWidth {
			return nil, fmt.Errorf("fixed binary value %d has length %d, want %d", i, len(value), target.ByteWidth)
		}
		builder.Append(value)
	}
	return builder.NewArray(), nil
}

func normalizedListArray(col arrow.Array, target *arrow.ListType) (arrow.Array, error) {
	list, ok := col.(array.ListLike)
	if !ok {
		return nil, fmt.Errorf("got %s, want %s", col.DataType(), target)
	}
	builder := array.NewListBuilderWithField(memory.DefaultAllocator, target.ElemField())
	defer builder.Release()
	values := list.ListValues()

	for row := 0; row < list.Len(); row++ {
		if list.IsNull(row) {
			builder.AppendNull()
			continue
		}
		builder.Append(true)
		start, end := list.ValueOffsets(row)
		for idx := start; idx < end; idx++ {
			if err := appendNormalizedListValue(builder.ValueBuilder(), values, int(idx), target.Elem()); err != nil {
				return nil, fmt.Errorf("list row %d element %d: %w", row, idx-start, err)
			}
		}
	}
	return builder.NewArray(), nil
}

func appendNormalizedListValue(builder array.Builder, values arrow.Array, idx int, target arrow.DataType) error {
	if values.IsNull(idx) {
		builder.AppendNull()
		return nil
	}

	switch targetType := target.(type) {
	case *arrow.StringType:
		stringBuilder, ok := builder.(*array.StringBuilder)
		if !ok {
			return fmt.Errorf("unexpected string builder %T", builder)
		}
		storage := extensionStorage(values)
		if strings, ok := storage.(array.StringLike); ok {
			stringBuilder.Append(strings.Value(idx))
			return nil
		}
		return fmt.Errorf("got %s, want %s", values.DataType(), targetType)

	case *extensions.UUIDType:
		var uuidBuilder *extensions.UUIDBuilder
		switch typedBuilder := builder.(type) {
		case *extensions.UUIDBuilder:
			uuidBuilder = typedBuilder
		case *array.ExtensionBuilder:
			uuidBuilder = &extensions.UUIDBuilder{ExtensionBuilder: typedBuilder}
		default:
			return fmt.Errorf("unexpected UUID builder %T", builder)
		}
		if strings, ok := values.(array.StringLike); ok {
			return uuidBuilder.AppendValueFromString(strings.Value(idx))
		}
		value, ok := binaryValue(extensionStorage(values), idx)
		if !ok || len(value) != 16 {
			return fmt.Errorf("got %s, want %s", values.DataType(), targetType)
		}
		var uuidBytes [16]byte
		copy(uuidBytes[:], value)
		uuidBuilder.AppendBytes(uuidBytes)
		return nil

	case *arrow.FixedSizeBinaryType:
		fixedBuilder, ok := builder.(*array.FixedSizeBinaryBuilder)
		if !ok {
			return fmt.Errorf("unexpected fixed binary builder %T", builder)
		}
		value, ok := binaryValue(extensionStorage(values), idx)
		if !ok {
			return fmt.Errorf("got %s, want %s", values.DataType(), targetType)
		}
		if len(value) != targetType.ByteWidth {
			return fmt.Errorf("fixed binary value has length %d, want %d", len(value), targetType.ByteWidth)
		}
		fixedBuilder.Append(value)
		return nil

	default:
		value, err := rowValue(values, idx)
		if err != nil {
			return err
		}
		return appendValueAtPath(builder, value, "element", true)
	}
}

func extensionStorage(col arrow.Array) arrow.Array {
	if ext, ok := col.(array.ExtensionArray); ok {
		return ext.Storage()
	}
	return col
}

func binaryValue(col arrow.Array, idx int) ([]byte, bool) {
	if values, ok := col.(interface{ Value(int) []byte }); ok {
		return values.Value(idx), true
	}
	if values, ok := col.(array.StringLike); ok {
		return []byte(values.Value(idx)), true
	}
	return nil, false
}
