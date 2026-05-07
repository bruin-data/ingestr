package databuffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/pkg/arrowconv"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/schemainfer"
	"github.com/bruin-data/gong/pkg/source"
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
	tmpDir, err := os.MkdirTemp("", "gong-buffer-*")
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
		existingCols[record.Schema().Field(i).Name] = record.Column(i)
	}

	cols := make([]arrow.Array, targetSchema.NumFields())
	for i := 0; i < targetSchema.NumFields(); i++ {
		field := targetSchema.Field(i)
		existingCol, ok := existingCols[field.Name]
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

	if isUnknownType(arr.DataType()) {
		return castUnknownArray(arr, target)
	}

	if isJSONType(target) {
		return castArrayToJSON(ctx, arr, target)
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
	var err error
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

func castArrayToJSON(ctx context.Context, arr arrow.Array, target arrow.DataType) (arrow.Array, error) {
	extType, ok := target.(arrow.ExtensionType)
	if !ok {
		return nil, fmt.Errorf("target type is not an extension type")
	}

	strArr, err := castArrayToString(ctx, arr)
	if err != nil {
		return nil, err
	}

	extArr := array.NewExtensionArrayWithStorage(extType, strArr)
	strArr.Release()
	return extArr, nil
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
