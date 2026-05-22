package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type CSVDestination struct {
	filePath string
	file     *os.File
	writer   *csv.Writer
	mu       sync.Mutex
	schema   *schema.TableSchema
}

func NewCSVDestination() *CSVDestination {
	return &CSVDestination{}
}

func (d *CSVDestination) Schemes() []string {
	return []string{"csv"}
}

func (d *CSVDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseCSVPath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse CSV URI: %w", err)
	}

	d.filePath = path
	config.Debug("[CSV] Destination file: %s", d.filePath)
	return nil
}

func (d *CSVDestination) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.writer != nil {
		d.writer.Flush()
	}
	if d.file != nil {
		return d.file.Close()
	}
	return nil
}

func (d *CSVDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.schema = opts.Schema

	// Ensure directory exists
	dir := filepath.Dir(d.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// For DropFirst, we truncate/create a new file
	if opts.DropFirst {
		if d.file != nil {
			d.writer.Flush()
			_ = d.file.Close()
		}

		file, err := os.Create(d.filePath)
		if err != nil {
			return fmt.Errorf("failed to create CSV file: %w", err)
		}
		d.file = file
		d.writer = csv.NewWriter(file)

		// Write header row
		if opts.Schema != nil {
			headers := opts.Schema.ColumnNames()
			if err := d.writer.Write(headers); err != nil {
				return fmt.Errorf("failed to write CSV header: %w", err)
			}
			d.writer.Flush()
		}
	} else {
		// Append mode - open for append, create if not exists
		file, err := os.OpenFile(d.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open CSV file: %w", err)
		}

		// Check if file is empty, if so write header
		info, _ := file.Stat()
		d.file = file
		d.writer = csv.NewWriter(file)

		if info.Size() == 0 && opts.Schema != nil {
			headers := opts.Schema.ColumnNames()
			if err := d.writer.Write(headers); err != nil {
				return fmt.Errorf("failed to write CSV header: %w", err)
			}
			d.writer.Flush()
		}
	}

	return nil
}

func (d *CSVDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *CSVDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[CSV] Starting write to %s", d.filePath)

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
		config.Debug("[CSV] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)

		result.Batch.Release()
	}

	d.mu.Lock()
	if d.writer != nil {
		d.writer.Flush()
	}
	d.mu.Unlock()

	config.Debug("[CSV] Total: %d rows written in %v", totalRows, time.Since(startTime))
	return nil
}

func (d *CSVDestination) writeRecordBatch(record arrow.RecordBatch) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.writer == nil {
		return 0, fmt.Errorf("CSV writer not initialized")
	}

	numRows := record.NumRows()
	numCols := int(record.NumCols())

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		row := make([]string, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			col := record.Column(colIdx)
			row[colIdx] = formatValue(col, int(rowIdx))
		}
		if err := d.writer.Write(row); err != nil {
			return rowIdx, fmt.Errorf("failed to write row: %w", err)
		}
	}

	return numRows, nil
}

func (d *CSVDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	// For CSV, swap means rename the staging file to target file
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.writer != nil {
		d.writer.Flush()
	}
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
		d.writer = nil
	}

	// In CSV context, stagingTable and targetTable are file paths
	// But since we write directly to the target, this is a no-op
	config.Debug("[CSV] SwapTable called (no-op for CSV)")
	return nil
}

func (d *CSVDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	// No-op for CSV
	return nil
}

func (d *CSVDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	// CSV doesn't support transactions, return a no-op transaction
	return &csvTransaction{}, nil
}

// csvTransaction is a no-op transaction for CSV
type csvTransaction struct{}

func (t *csvTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *csvTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *csvTransaction) Rollback(ctx context.Context) error {
	return nil
}

// MergeTable is not supported for CSV destinations.
func (d *CSVDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return fmt.Errorf("merge strategy is not supported for CSV destination")
}

// DeleteInsertTable is not supported for CSV destinations.
func (d *CSVDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("delete+insert strategy is not supported for CSV destination")
}

// SCD2Table is not supported for CSV destinations.
func (d *CSVDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("scd2 strategy is not supported for CSV destination")
}

// DropTable is a no-op for CSV destinations.
func (d *CSVDestination) DropTable(ctx context.Context, table string) error {
	// No-op for CSV - files are managed by the file system
	return nil
}

// SupportsReplaceStrategy returns true as CSV supports the replace strategy (overwrite file).
func (d *CSVDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as CSV supports the append strategy.
func (d *CSVDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns false as CSV does not support the merge strategy.
func (d *CSVDestination) SupportsMergeStrategy() bool { return false }

// SupportsDeleteInsertStrategy returns false as CSV does not support the delete+insert strategy.
func (d *CSVDestination) SupportsDeleteInsertStrategy() bool { return false }

// SupportsSCD2Strategy returns false as CSV does not support the SCD2 strategy.
func (d *CSVDestination) SupportsSCD2Strategy() bool { return false }

// SupportsAtomicSwap returns false as CSV writes directly to the target file.
func (d *CSVDestination) SupportsAtomicSwap() bool { return false }

func (d *CSVDestination) GetScheme() string { return "csv" }

func (d *CSVDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

// parseCSVPath extracts the file path from a csv:// URI
func parseCSVPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "csv:") {
		return "", fmt.Errorf("invalid csv URI: %s", uri)
	}
	rest := strings.TrimPrefix(uri, "csv:")
	rest = strings.TrimPrefix(rest, "//")

	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}

	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}

	if rest == "" {
		return "", fmt.Errorf("empty file path in URI")
	}

	return rest, nil
}

// formatValue converts an Arrow array value at a given index to a string
func formatValue(arr arrow.Array, idx int) string {
	if arr.IsNull(idx) {
		return ""
	}

	switch a := arr.(type) {
	case *array.Boolean:
		if a.Value(idx) {
			return "true"
		}
		return "false"
	case *array.Int16:
		return fmt.Sprintf("%d", a.Value(idx))
	case *array.Int32:
		return fmt.Sprintf("%d", a.Value(idx))
	case *array.Int64:
		return fmt.Sprintf("%d", a.Value(idx))
	case *array.Float32:
		return fmt.Sprintf("%g", a.Value(idx))
	case *array.Float64:
		return fmt.Sprintf("%g", a.Value(idx))
	case *array.String:
		return a.Value(idx)
	case *array.LargeString:
		return a.Value(idx)
	case *array.Binary:
		return string(a.Value(idx))
	case *array.Date32:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Date64:
		return a.Value(idx).ToTime().Format("2006-01-02")
	case *array.Time64:
		// Time64 in microseconds
		micros := int64(a.Value(idx))
		t := time.Duration(micros) * time.Microsecond
		hours := int(t.Hours())
		mins := int(t.Minutes()) % 60
		secs := int(t.Seconds()) % 60
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	case *array.Timestamp:
		ts := a.Value(idx)
		// Timestamp is in the unit specified by the type
		t := ts.ToTime(a.DataType().(*arrow.TimestampType).Unit)
		return t.Format("2006-01-02T15:04:05.000000")
	case *array.Decimal128:
		return a.Value(idx).ToString(int32(a.DataType().(*arrow.Decimal128Type).Scale))
	case array.ExtensionArray:
		return formatValue(a.Storage(), idx)
	default:
		return fmt.Sprintf("%v", arr)
	}
}
