package databuffer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
)

// FileBuffer is a file-backed implementation of DataBuffer.
// It writes each record batch to a separate Arrow IPC file.
// When replaying via Reader(), batches are cast to the provided target schema.
type FileBuffer struct {
	baseDir    string
	batchFiles []string
	mu         sync.Mutex
	closed     bool
	rowCount   int64
	batchCount int64
	bytesUsed  int64
}

// NewFileBuffer creates a new file-backed buffer using a temporary directory.
func NewFileBuffer() (*FileBuffer, error) {
	tmpDir, err := os.MkdirTemp("", "ingestr-buffer-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &FileBuffer{
		baseDir:    tmpDir,
		batchFiles: make([]string, 0),
	}, nil
}

// NewFileBufferWithPath creates a file buffer at a specific directory path.
func NewFileBufferWithPath(path string) (*FileBuffer, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create buffer directory: %w", err)
	}

	return &FileBuffer{
		baseDir:    path,
		batchFiles: make([]string, 0),
	}, nil
}

// Append adds a record batch to the buffer by writing it to a separate Arrow IPC file.
func (b *FileBuffer) Append(ctx context.Context, batch arrow.RecordBatch) error {
	if batch == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrBufferClosed
	}

	batchPath := filepath.Join(b.baseDir, fmt.Sprintf("batch_%06d.arrow", b.batchCount))
	if err := b.writeBatchToFile(batch, batchPath); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}
	b.batchFiles = append(b.batchFiles, batchPath)

	b.rowCount += batch.NumRows()
	b.batchCount++

	for i := 0; i < int(batch.NumCols()); i++ {
		col := batch.Column(i)
		for _, buf := range col.Data().Buffers() {
			if buf != nil {
				b.bytesUsed += int64(buf.Len())
			}
		}
	}

	return nil
}

func (b *FileBuffer) writeBatchToFile(batch arrow.RecordBatch, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create batch file: %w", err)
	}
	defer func() { _ = f.Close() }()

	writer, err := ipc.NewFileWriter(
		f,
		ipc.WithSchema(batch.Schema()),
		ipc.WithAllocator(memory.DefaultAllocator),
	)
	if err != nil {
		return fmt.Errorf("failed to create arrow ipc writer: %w", err)
	}

	if err := writer.Write(batch); err != nil {
		_ = writer.Close()
		return fmt.Errorf("failed to write batch: %w", err)
	}

	return writer.Close()
}

// Reader returns a channel that replays all buffered batches, cast to the target schema.
func (b *FileBuffer) Reader(ctx context.Context, targetSchema *arrow.Schema) (<-chan source.RecordBatchResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrBufferClosed
	}

	if b.batchCount == 0 {
		out := make(chan source.RecordBatchResult)
		close(out)
		return out, nil
	}

	out := make(chan source.RecordBatchResult, 10)
	batchFiles := make([]string, len(b.batchFiles))
	copy(batchFiles, b.batchFiles)

	go func() {
		defer close(out)

		for _, batchPath := range batchFiles {
			record, err := b.readAndCastBatch(batchPath, targetSchema)
			if err != nil {
				out <- source.RecordBatchResult{Err: fmt.Errorf("failed to read batch %s: %w", batchPath, err)}
				return
			}

			select {
			case out <- source.RecordBatchResult{Batch: record}:
			case <-ctx.Done():
				record.Release()
				return
			}
		}
	}()

	return out, nil
}

func (b *FileBuffer) readAndCastBatch(path string, targetSchema *arrow.Schema) (arrow.RecordBatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open batch file: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader, err := ipc.NewFileReader(file, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return nil, fmt.Errorf("failed to create arrow reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	if reader.NumRecords() == 0 {
		return nil, fmt.Errorf("empty batch file")
	}

	record, err := reader.RecordBatch(0)
	if err != nil {
		return nil, fmt.Errorf("failed to read record: %w", err)
	}

	if record.Schema().Equal(targetSchema) {
		record.Retain()
		return record, nil
	}

	return CastRecordToSchema(record, targetSchema, true)
}

// CastRecordToSchema creates a new record with the target schema, casting columns as needed.
// When safe is false, lossy conversions (e.g. decimal → int64) are allowed to match
// the behavior of user-specified column type overrides.
func CastRecordToSchema(record arrow.RecordBatch, targetSchema *arrow.Schema, safe bool) (arrow.RecordBatch, error) {
	mem := memory.DefaultAllocator
	numRows := record.NumRows()

	existingCols := make(map[string]arrow.Array)
	for i := 0; i < int(record.NumCols()); i++ {
		existingCols[strings.ToLower(record.Schema().Field(i).Name)] = record.Column(i)
	}

	cols := make([]arrow.Array, targetSchema.NumFields())
	for i := 0; i < targetSchema.NumFields(); i++ {
		field := targetSchema.Field(i)
		existingCol, ok := existingCols[strings.ToLower(field.Name)]
		if !ok {
			nullArray, err := makeNullArray(mem, field.Type, int(numRows))
			if err != nil {
				for j := 0; j < i; j++ {
					cols[j].Release()
				}
				return nil, fmt.Errorf("failed to create null array for field %s: %w", field.Name, err)
			}
			cols[i] = nullArray
			continue
		}

		if arrow.TypeEqual(existingCol.DataType(), field.Type) {
			existingCol.Retain()
			cols[i] = existingCol
			continue
		}

		casted, err := castArrayToType(context.Background(), existingCol, field.Type, safe)
		if err != nil {
			for j := 0; j < i; j++ {
				cols[j].Release()
			}
			return nil, fmt.Errorf("failed to cast field %s from %s to %s: %w", field.Name, existingCol.DataType(), field.Type, err)
		}
		cols[i] = casted
	}

	result := array.NewRecordBatch(targetSchema, cols, numRows)

	for _, col := range cols {
		col.Release()
	}

	return result, nil
}

func makeNullArray(mem memory.Allocator, dt arrow.DataType, length int) (arrow.Array, error) {
	builder := array.NewBuilder(mem, dt)
	defer builder.Release()

	builder.AppendNulls(length)
	return builder.NewArray(), nil
}

func castArrayToType(ctx context.Context, arr arrow.Array, target arrow.DataType, safe bool) (arrow.Array, error) {
	if arrow.TypeEqual(arr.DataType(), target) {
		arr.Retain()
		return arr, nil
	}

	arr, releaseArr, err := normalizeArrayOffset(arr)
	if err != nil {
		return nil, err
	}
	if releaseArr {
		defer arr.Release()
	}

	if isUnknownType(arr.DataType()) {
		return castUnknownArray(arr, target)
	}
	if target.ID() == arrow.LIST {
		if values, ok := arr.(array.VarLenListLike); ok {
			return castVariableListToList(ctx, values, target.(*arrow.ListType), safe)
		}
	}

	if isJSONType(target) {
		return castArrayToJSON(arr, target)
	}

	if isJSONType(arr.DataType()) && target.ID() == arrow.STRING {
		if ext, ok := arr.(array.ExtensionArray); ok {
			storage := ext.Storage()
			storage.Retain()
			return storage, nil
		}
	}

	// For numeric → timestamp, cast to string first so the string→timestamp
	// path can detect the unit via dateparse (seconds vs milliseconds vs micros).
	if target.ID() == arrow.TIMESTAMP && arrowconv.IsNumeric(arr.DataType()) {
		strArr, err := compute.CastArray(ctx, arr, compute.SafeCastOptions(arrow.BinaryTypes.String))
		if err == nil {
			result, err := castStringArrayViaAppendValue(strArr, target)
			strArr.Release()
			return result, err
		}
	}

	var casted arrow.Array
	if safe {
		casted, err = compute.CastArray(ctx, arr, compute.SafeCastOptions(target))
	} else if (arr.DataType().ID() == arrow.DECIMAL128 || arr.DataType().ID() == arrow.DECIMAL256) && isIntegerType(target) {
		// Cast decimal via float64 to truncate toward zero (matching Python's int()).
		// Arrow's direct decimal → int path rounds instead of truncating.
		floatArr, floatErr := compute.CastArray(ctx, arr, compute.UnsafeCastOptions(arrow.PrimitiveTypes.Float64))
		if floatErr == nil {
			casted, err = compute.CastArray(ctx, floatArr, compute.UnsafeCastOptions(target))
			floatArr.Release()
		} else {
			err = floatErr
		}
	} else {
		casted, err = compute.CastArray(ctx, arr, compute.UnsafeCastOptions(target))
	}
	if err == nil {
		return casted, nil
	}

	if target.ID() == arrow.STRING {
		return castArrayToString(ctx, arr)
	}

	// Fallback: if the source is a string-like type, parse each value via AppendValue
	// which handles conversions like string → timestamp (via dateparse).
	if arr.DataType().ID() == arrow.STRING || arr.DataType().ID() == arrow.LARGE_STRING {
		return castStringArrayViaAppendValue(arr, target)
	}

	return nil, err
}

func castVariableListToList(ctx context.Context, values array.VarLenListLike, target *arrow.ListType, safe bool) (arrow.Array, error) {
	validity := make([]byte, bitutil.BytesForBits(int64(values.Len())))
	offsets := make([]int32, values.Len()+1)
	slices := make([]arrow.Array, 0, values.Len())
	defer func() {
		for _, value := range slices {
			value.Release()
		}
	}()

	var total int64
	nullCount := 0
	items := values.ListValues()
	for i := 0; i < values.Len(); i++ {
		if values.IsNull(i) {
			nullCount++
			offsets[i+1] = int32(total)
			continue
		}
		bitutil.SetBit(validity, i)
		start, end := values.ValueOffsets(i)
		if start < 0 || end < start || end > int64(items.Len()) {
			return nil, fmt.Errorf("invalid list offsets [%d:%d] for %d values", start, end, items.Len())
		}
		total += end - start
		if total > math.MaxInt32 {
			return nil, fmt.Errorf("list contains %d values, exceeding the int32 offset limit", total)
		}
		offsets[i+1] = int32(total)
		if end > start {
			slices = append(slices, array.NewSlice(items, start, end))
		}
	}

	var flattened arrow.Array
	var err error
	if len(slices) == 0 {
		flattened = array.NewSlice(items, 0, 0)
	} else {
		flattened, err = array.Concatenate(slices, memory.DefaultAllocator)
		if err != nil {
			return nil, fmt.Errorf("failed to concatenate list values: %w", err)
		}
	}
	defer flattened.Release()
	castedValues, err := castArrayToType(ctx, flattened, target.Elem(), safe)
	if err != nil {
		return nil, fmt.Errorf("failed to cast list values from %s to %s: %w", flattened.DataType(), target.Elem(), err)
	}
	defer castedValues.Release()

	validityBuffer := memory.NewBufferBytes(validity)
	defer validityBuffer.Release()
	offsetBuffer := memory.NewBufferBytes(arrow.Int32Traits.CastToBytes(offsets))
	defer offsetBuffer.Release()
	data := array.NewData(target, values.Len(), []*memory.Buffer{validityBuffer, offsetBuffer}, []arrow.ArrayData{castedValues.Data()}, nullCount, 0)
	defer data.Release()
	return array.NewListData(data), nil
}

func normalizeArrayOffset(arr arrow.Array) (arrow.Array, bool, error) {
	if arr == nil || arr.Data().Offset() == 0 {
		return arr, false, nil
	}

	normalized, err := array.Concatenate([]arrow.Array{arr}, memory.DefaultAllocator)
	if err != nil {
		return nil, false, fmt.Errorf("failed to normalize sliced array offset: %w", err)
	}
	return normalized, true, nil
}

func castStringArrayViaAppendValue(arr arrow.Array, target arrow.DataType) (arrow.Array, error) {
	builder := array.NewBuilder(memory.DefaultAllocator, target)
	defer builder.Release()

	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			builder.AppendNull()
			continue
		}
		raw, ok := schemainfer.StringValueAt(arr, i)
		if !ok {
			builder.AppendNull()
			continue
		}
		arrowconv.AppendValue(builder, raw)
	}

	return builder.NewArray(), nil
}

func castUnknownArray(arr arrow.Array, target arrow.DataType) (arrow.Array, error) {
	ext, ok := arr.(array.ExtensionArray)
	if !ok {
		return nil, fmt.Errorf("unknown type is not an extension array")
	}

	if isJSONType(target) {
		return castUnknownArrayToJSON(ext, target)
	}

	builder := array.NewBuilder(memory.DefaultAllocator, target)
	defer builder.Release()

	storage := ext.Storage()
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			builder.AppendNull()
			continue
		}

		raw, ok := schemainfer.StringValueAt(storage, i)
		if !ok {
			builder.AppendNull()
			continue
		}

		decoded, err := schemainfer.DecodeUnknownValue(raw)
		if err != nil {
			decoded = raw
		}
		arrowconv.AppendValue(builder, decoded)
	}

	return builder.NewArray(), nil
}

func castUnknownArrayToJSON(ext array.ExtensionArray, target arrow.DataType) (arrow.Array, error) {
	extType, ok := target.(arrow.ExtensionType)
	if !ok {
		return nil, fmt.Errorf("target type is not an extension type")
	}

	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()

	storage := ext.Storage()
	for i := 0; i < ext.Len(); i++ {
		if ext.IsNull(i) {
			builder.AppendNull()
			continue
		}

		raw, ok := schemainfer.StringValueAt(storage, i)
		if !ok {
			builder.AppendNull()
			continue
		}

		// Unknown storage is already JSON text; pass it through unless it came
		// from a non-JSON fallback, in which case quote it as a JSON string.
		if json.Valid([]byte(raw)) {
			builder.Append(raw)
			continue
		}

		jsonBytes, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to encode unknown value as JSON: %w", err)
		}
		builder.Append(string(jsonBytes))
	}

	storageArr := builder.NewArray()
	defer storageArr.Release()

	return array.NewExtensionArrayWithStorage(extType, storageArr), nil
}

func castArrayToJSON(arr arrow.Array, target arrow.DataType) (arrow.Array, error) {
	extType, ok := target.(arrow.ExtensionType)
	if !ok {
		return nil, fmt.Errorf("target type is not an extension type")
	}

	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			builder.AppendNull()
			continue
		}
		value, err := marshalArrowJSONValue(arr, i)
		if err != nil {
			return nil, fmt.Errorf("failed to encode row %d as JSON: %w", i, err)
		}
		builder.Append(string(value))
	}

	storage := builder.NewArray()
	defer storage.Release()
	return array.NewExtensionArrayWithStorage(extType, storage), nil
}

func marshalArrowJSONValue(arr arrow.Array, i int) ([]byte, error) {
	if i < 0 || i >= arr.Len() {
		return nil, fmt.Errorf("array index %d is out of range", i)
	}
	if arr.IsNull(i) {
		return []byte("null"), nil
	}

	switch values := arr.(type) {
	case *array.Struct:
		return marshalArrowStructJSON(values, i)
	case *array.Map:
		return marshalArrowMapJSON(values, i)
	case array.ListLike:
		return marshalArrowListJSON(values, i)
	case array.ExtensionArray:
		if isJSONType(values.DataType()) {
			raw, ok := schemainfer.StringValueAt(values.Storage(), i)
			if !ok || !json.Valid([]byte(raw)) {
				return nil, fmt.Errorf("JSON extension contains invalid JSON")
			}
			return []byte(raw), nil
		}
		return marshalArrowJSONValue(values.Storage(), i)
	case *array.Dictionary:
		return marshalArrowJSONValue(values.Dictionary(), values.GetValueIndex(i))
	case *array.RunEndEncoded:
		return marshalArrowJSONValue(values.Values(), values.GetPhysicalIndex(i))
	case interface{ Value(int) []byte }:
		return json.Marshal(values.Value(i))
	}

	raw := arr.ValueStr(i)
	switch arr.DataType().ID() {
	case arrow.STRING, arrow.LARGE_STRING, arrow.STRING_VIEW,
		arrow.DATE32, arrow.DATE64, arrow.TIME32, arrow.TIME64, arrow.TIMESTAMP,
		arrow.DURATION, arrow.INTERVAL_MONTHS, arrow.INTERVAL_DAY_TIME, arrow.INTERVAL_MONTH_DAY_NANO:
		return json.Marshal(raw)
	default:
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("arrow %s value %q has no lossless JSON representation", arr.DataType(), raw)
		}
		return []byte(raw), nil
	}
}

func marshalArrowStructJSON(values *array.Struct, i int) ([]byte, error) {
	fields := values.DataType().(*arrow.StructType).Fields()
	seen := make(map[string]struct{}, len(fields))
	var result bytes.Buffer
	result.WriteByte('{')
	for fieldIndex, field := range fields {
		if _, ok := seen[field.Name]; ok {
			return nil, fmt.Errorf("struct contains duplicate field %q", field.Name)
		}
		seen[field.Name] = struct{}{}
		if fieldIndex > 0 {
			result.WriteByte(',')
		}
		name, _ := json.Marshal(field.Name)
		result.Write(name)
		result.WriteByte(':')
		value, err := marshalArrowJSONValue(values.Field(fieldIndex), i)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field.Name, err)
		}
		result.Write(value)
	}
	result.WriteByte('}')
	return result.Bytes(), nil
}

func marshalArrowListJSON(values array.ListLike, i int) ([]byte, error) {
	start, end := values.ValueOffsets(i)
	items := values.ListValues()
	if start < 0 || end < start || end > int64(items.Len()) {
		return nil, fmt.Errorf("invalid list offsets [%d:%d] for %d values", start, end, items.Len())
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for itemIndex := start; itemIndex < end; itemIndex++ {
		if itemIndex > start {
			result.WriteByte(',')
		}
		value, err := marshalArrowJSONValue(items, int(itemIndex))
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", itemIndex-start, err)
		}
		result.Write(value)
	}
	result.WriteByte(']')
	return result.Bytes(), nil
}

func marshalArrowMapJSON(values *array.Map, i int) ([]byte, error) {
	start, end := values.ValueOffsets(i)
	keys, items := values.Keys(), values.Items()
	if start < 0 || end < start || end > int64(keys.Len()) || end > int64(items.Len()) {
		return nil, fmt.Errorf("invalid map offsets [%d:%d] for %d keys and %d values", start, end, keys.Len(), items.Len())
	}
	if !arrowMapKeysUseJSONObject(keys.DataType()) {
		return marshalArrowMapEntriesJSON(keys, items, start, end)
	}
	seen := make(map[string]struct{}, end-start)
	var result bytes.Buffer
	result.WriteByte('{')
	for itemIndex := start; itemIndex < end; itemIndex++ {
		keyJSON, err := marshalArrowJSONValue(keys, int(itemIndex))
		if err != nil {
			return nil, fmt.Errorf("map key %d: %w", itemIndex-start, err)
		}
		key := string(keyJSON)
		if len(keyJSON) > 0 && keyJSON[0] == '"' {
			if err := json.Unmarshal(keyJSON, &key); err != nil {
				return nil, fmt.Errorf("map key %d: %w", itemIndex-start, err)
			}
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("map contains duplicate JSON key %q", key)
		}
		seen[key] = struct{}{}
		if itemIndex > start {
			result.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		result.Write(encodedKey)
		result.WriteByte(':')
		value, err := marshalArrowJSONValue(items, int(itemIndex))
		if err != nil {
			return nil, fmt.Errorf("map value for key %q: %w", key, err)
		}
		result.Write(value)
	}
	result.WriteByte('}')
	return result.Bytes(), nil
}

func marshalArrowMapEntriesJSON(keys, items arrow.Array, start, end int64) ([]byte, error) {
	var result bytes.Buffer
	result.WriteByte('[')
	for itemIndex := start; itemIndex < end; itemIndex++ {
		if itemIndex > start {
			result.WriteByte(',')
		}
		key, err := marshalArrowJSONValue(keys, int(itemIndex))
		if err != nil {
			return nil, fmt.Errorf("map key %d: %w", itemIndex-start, err)
		}
		value, err := marshalArrowJSONValue(items, int(itemIndex))
		if err != nil {
			return nil, fmt.Errorf("map value %d: %w", itemIndex-start, err)
		}
		result.WriteString(`{"key":`)
		result.Write(key)
		result.WriteString(`,"value":`)
		result.Write(value)
		result.WriteByte('}')
	}
	result.WriteByte(']')
	return result.Bytes(), nil
}

func arrowMapKeysUseJSONObject(dt arrow.DataType) bool {
	switch typed := dt.(type) {
	case *arrow.DictionaryType:
		return arrowMapKeysUseJSONObject(typed.ValueType)
	default:
		return dt.ID() == arrow.STRING || dt.ID() == arrow.LARGE_STRING || dt.ID() == arrow.STRING_VIEW
	}
}

func castArrayToString(ctx context.Context, arr arrow.Array) (arrow.Array, error) {
	if arr.DataType().ID() == arrow.STRING {
		arr.Retain()
		return arr, nil
	}

	if isJSONType(arr.DataType()) {
		if ext, ok := arr.(array.ExtensionArray); ok {
			storage := ext.Storage()
			storage.Retain()
			return storage, nil
		}
	}

	casted, err := compute.CastArray(ctx, arr, compute.SafeCastOptions(arrow.BinaryTypes.String))
	if err == nil {
		return casted, nil
	}

	builder := array.NewStringBuilder(memory.DefaultAllocator)
	defer builder.Release()

	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			builder.AppendNull()
			continue
		}
		builder.Append(arr.ValueStr(i))
	}

	return builder.NewArray(), nil
}

func isIntegerType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return true
	}
	return false
}

func isJSONType(dt arrow.DataType) bool {
	if dt.ID() != arrow.EXTENSION {
		return false
	}
	ext, ok := dt.(arrow.ExtensionType)
	if !ok {
		return false
	}
	return ext.ExtensionName() == schema.JSONExtensionName
}

func isUnknownType(dt arrow.DataType) bool {
	if dt.ID() != arrow.EXTENSION {
		return false
	}
	ext, ok := dt.(arrow.ExtensionType)
	if !ok {
		return false
	}
	return ext.ExtensionName() == schema.UnknownExtensionName
}

// Close releases buffer resources and removes the temporary directory.
func (b *FileBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	if b.baseDir != "" {
		_ = os.RemoveAll(b.baseDir)
	}

	b.closed = true
	return nil
}

// Stats returns buffer statistics.
func (b *FileBuffer) Stats() BufferStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	return BufferStats{
		BatchCount: b.batchCount,
		RowCount:   b.rowCount,
		BytesUsed:  b.bytesUsed,
	}
}

var _ DataBuffer = (*FileBuffer)(nil)
