package jsonl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type JSONLDestination struct {
	filePath string
	file     *os.File
	mu       sync.Mutex
	schema   *schema.TableSchema
}

func NewJSONLDestination() *JSONLDestination {
	return &JSONLDestination{}
}

func (d *JSONLDestination) Schemes() []string {
	return []string{"jsonl", "ndjson"}
}

func (d *JSONLDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseJSONLPath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse JSONL URI: %w", err)
	}

	d.filePath = path
	config.Debug("[JSONL] Destination file: %s", d.filePath)
	return nil
}

func (d *JSONLDestination) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file != nil {
		return d.file.Close()
	}
	return nil
}

func (d *JSONLDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.schema = opts.Schema

	dir := filepath.Dir(d.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if opts.DropFirst {
		if d.file != nil {
			_ = d.file.Close()
		}

		file, err := os.Create(d.filePath)
		if err != nil {
			return fmt.Errorf("failed to create JSONL file: %w", err)
		}
		d.file = file
	} else {
		file, err := os.OpenFile(d.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open JSONL file: %w", err)
		}
		d.file = file
	}

	return nil
}

func (d *JSONLDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *JSONLDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[JSONL] Starting write to %s", d.filePath)

	for result := range records {
		if result.Err != nil {
			return result.Err
		}

		batchNum++
		startBatch := time.Now()

		rows, err := d.writeRecordBatch(result.Batch)
		if err != nil {
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		totalRows += rows
		config.Debug("[JSONL] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)

		result.Batch.Release()
	}

	d.mu.Lock()
	if d.file != nil {
		_ = d.file.Sync()
	}
	d.mu.Unlock()

	config.Debug("[JSONL] Total: %d rows written in %v", totalRows, time.Since(startTime))
	return nil
}

func (d *JSONLDestination) writeRecordBatch(record arrow.RecordBatch) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file == nil {
		return 0, fmt.Errorf("JSONL file not initialized")
	}

	numRows := record.NumRows()
	numCols := int(record.NumCols())
	arrowSchema := record.Schema()

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		row := make(map[string]interface{}, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			col := record.Column(colIdx)
			fieldName := arrowSchema.Field(colIdx).Name
			row[fieldName] = extractValue(col, int(rowIdx))
		}

		jsonBytes, err := json.Marshal(row)
		if err != nil {
			return rowIdx, fmt.Errorf("failed to marshal row: %w", err)
		}

		if _, err := d.file.Write(append(jsonBytes, '\n')); err != nil {
			return rowIdx, fmt.Errorf("failed to write row: %w", err)
		}
	}

	return numRows, nil
}

func (d *JSONLDestination) SwapTable(ctx context.Context, stagingTable, targetTable string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file != nil {
		_ = d.file.Sync()
		_ = d.file.Close()
		d.file = nil
	}

	config.Debug("[JSONL] SwapTable called (no-op for JSONL)")
	return nil
}

func (d *JSONLDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (d *JSONLDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return &jsonlTransaction{}, nil
}

type jsonlTransaction struct{}

func (t *jsonlTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *jsonlTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *jsonlTransaction) Rollback(ctx context.Context) error {
	return nil
}

func (d *JSONLDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return fmt.Errorf("merge strategy is not supported for JSONL destination")
}

func (d *JSONLDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("delete+insert strategy is not supported for JSONL destination")
}

func (d *JSONLDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("scd2 strategy is not supported for JSONL destination")
}

func (d *JSONLDestination) DropTable(ctx context.Context, table string) error {
	return nil
}

func (d *JSONLDestination) SupportsReplaceStrategy() bool { return true }

func (d *JSONLDestination) SupportsAppendStrategy() bool { return true }

func (d *JSONLDestination) SupportsMergeStrategy() bool { return false }

func (d *JSONLDestination) SupportsDeleteInsertStrategy() bool { return false }

func (d *JSONLDestination) SupportsSCD2Strategy() bool { return false }

func (d *JSONLDestination) SupportsAtomicSwap() bool { return false }

func (d *JSONLDestination) GetScheme() string { return "jsonl" }

func (d *JSONLDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

func parseJSONLPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	path := u.Host + u.Path

	if path == "" {
		return "", fmt.Errorf("empty file path in URI")
	}

	return path, nil
}

func extractValue(arr arrow.Array, idx int) interface{} {
	if arr.IsNull(idx) {
		return nil
	}

	switch a := arr.(type) {
	case *array.Boolean:
		return a.Value(idx)
	case *array.Int8:
		return a.Value(idx)
	case *array.Int16:
		return a.Value(idx)
	case *array.Int32:
		return a.Value(idx)
	case *array.Int64:
		return a.Value(idx)
	case *array.Uint8:
		return a.Value(idx)
	case *array.Uint16:
		return a.Value(idx)
	case *array.Uint32:
		return a.Value(idx)
	case *array.Uint64:
		return a.Value(idx)
	case *array.Float32:
		return a.Value(idx)
	case *array.Float64:
		return a.Value(idx)
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return a.Value(idx)
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		val := int64(a.Value(idx))
		unit := a.DataType().(*arrow.Time64Type).Unit
		var d time.Duration
		switch unit {
		case arrow.Nanosecond:
			d = time.Duration(val) * time.Nanosecond
		default:
			d = time.Duration(val) * time.Microsecond
		}
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	case *array.Timestamp:
		ts := a.Value(idx)
		t := ts.ToTime(a.DataType().(*arrow.TimestampType).Unit)
		return t.Format(time.RFC3339Nano)
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		storage := a.Storage()
		if sb, ok := storage.(*array.String); ok {
			val := sb.Value(idx)
			var parsed interface{}
			if err := json.Unmarshal([]byte(val), &parsed); err == nil {
				return parsed
			}
			return val
		}
		return extractValue(storage, idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}

var _ destination.Destination = (*JSONLDestination)(nil)
