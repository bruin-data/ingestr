package parquet

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type ParquetDestination struct {
	filePath    string
	tempPath    string
	file        *os.File
	writer      *pqarrow.FileWriter
	arrowSchema *arrow.Schema
	mu          sync.Mutex
	schema      *schema.TableSchema
	appendMode  bool
}

func NewParquetDestination() *ParquetDestination {
	return &ParquetDestination{}
}

func (d *ParquetDestination) Schemes() []string {
	return []string{"parquet"}
}

func (d *ParquetDestination) Connect(ctx context.Context, uri string) error {
	path, err := parseParquetPath(uri)
	if err != nil {
		return fmt.Errorf("failed to parse Parquet URI: %w", err)
	}

	d.filePath = path
	config.Debug("[PARQUET] Destination file: %s", d.filePath)
	return nil
}

func (d *ParquetDestination) Close(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.writer != nil {
		if err := d.writer.Close(); err != nil {
			return err
		}
		d.writer = nil
	}
	if d.file != nil {
		if err := d.file.Close(); err != nil {
			return err
		}
		d.file = nil
	}
	return nil
}

func (d *ParquetDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.schema = opts.Schema
	d.appendMode = !opts.DropFirst

	// Ensure directory exists
	dir := filepath.Dir(d.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Close existing writer if any
	if d.writer != nil {
		_ = d.writer.Close()
		d.writer = nil
	}
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
	}
	d.tempPath = ""

	// For DropFirst or new file, create/truncate
	if opts.DropFirst {
		if opts.Schema != nil {
			d.arrowSchema = opts.Schema.ToArrowSchema()
		}
		// Don't create file yet - wait for first write to get schema
	}

	return nil
}

func (d *ParquetDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *ParquetDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	startTime := time.Now()
	var totalRows int64
	var batchNum int

	config.Debug("[PARQUET] Starting write to %s", d.filePath)

	for result := range records {
		if result.Err != nil {
			d.cleanupTempOnError()
			return result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		batchNum++
		startBatch := time.Now()

		// Initialize writer on first batch using the record's schema
		if d.writer == nil {
			if err := d.initWriter(ctx, record.Schema()); err != nil {
				d.cleanupTempOnError()
				return fmt.Errorf("failed to initialize parquet writer: %w", err)
			}
		}

		recordToWrite := record
		shouldRelease := false
		if d.arrowSchema != nil && !record.Schema().Equal(d.arrowSchema) && schemaEqualIgnoringMetadata(record.Schema(), d.arrowSchema) {
			normalized, err := normalizeRecordToSchema(record, d.arrowSchema)
			if err != nil {
				d.cleanupTempOnError()
				record.Release()
				return fmt.Errorf("failed to normalize record schema: %w", err)
			}
			recordToWrite = normalized
			shouldRelease = true
		}

		if err := d.writer.WriteBuffered(recordToWrite); err != nil {
			if shouldRelease {
				recordToWrite.Release()
			}
			record.Release()
			d.cleanupTempOnError()
			return fmt.Errorf("failed to write batch %d: %w", batchNum, err)
		}

		rows := recordToWrite.NumRows()
		totalRows += rows
		config.Debug("[PARQUET] Batch %d: %d rows in %v (total: %d)", batchNum, rows, time.Since(startBatch), totalRows)

		if shouldRelease {
			recordToWrite.Release()
		}
		record.Release()
	}

	// Flush and get final stats
	d.mu.Lock()
	finalPath := d.filePath
	tempPath := d.tempPath
	shouldSwap := d.appendMode && tempPath != ""
	if d.writer != nil {
		if err := d.writer.Close(); err != nil {
			d.mu.Unlock()
			d.cleanupTempOnError()
			return fmt.Errorf("failed to close parquet writer: %w", err)
		}
		d.writer = nil
	}
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
	}
	d.tempPath = ""
	d.appendMode = false
	d.mu.Unlock()

	if shouldSwap {
		if err := os.Rename(tempPath, finalPath); err != nil {
			_ = os.Remove(tempPath)
			return fmt.Errorf("failed to finalize parquet append (rename): %w", err)
		}
	}

	config.Debug("[PARQUET] Total: %d rows written in %v", totalRows, time.Since(startTime))
	return nil
}

func (d *ParquetDestination) initWriter(ctx context.Context, arrowSchema *arrow.Schema) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.writer != nil {
		return nil
	}

	normalizedSchema := stripSchemaMetadata(arrowSchema)

	targetPath := d.filePath
	if d.appendMode {
		// If appending to an existing parquet file, write to a temp file first and swap atomically.
		if info, err := os.Stat(d.filePath); err == nil && info.Size() > 0 {
			d.tempPath = fmt.Sprintf("%s.tmp-%d", d.filePath, time.Now().UnixNano())
			targetPath = d.tempPath
		}
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create parquet file: %w", err)
	}
	d.file = file
	d.arrowSchema = normalizedSchema

	// Configure parquet writer properties
	writerProps := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Snappy),
		parquet.WithDictionaryDefault(true),
		parquet.WithDataPageSize(1024*1024), // 1MB page size
	)

	arrowProps := pqarrow.NewArrowWriterProperties(
		pqarrow.WithStoreSchema(),
	)

	writer, err := pqarrow.NewFileWriter(normalizedSchema, file, writerProps, arrowProps)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}

	d.writer = writer

	// If we are appending and we have a temp file, copy existing rows first.
	if d.appendMode && d.tempPath != "" {
		if err := d.copyExistingParquetIntoWriter(ctx, normalizedSchema); err != nil {
			_ = d.writer.Close()
			d.writer = nil
			_ = d.file.Close()
			d.file = nil
			_ = os.Remove(d.tempPath)
			d.tempPath = ""
			return err
		}
	}
	return nil
}

func (d *ParquetDestination) copyExistingParquetIntoWriter(ctx context.Context, expectedSchema *arrow.Schema) error {
	// Read the original file (not temp) and write its rows into the current writer.
	orig, err := os.Open(d.filePath)
	if err != nil {
		return fmt.Errorf("failed to open existing parquet file for append: %w", err)
	}
	defer func() { _ = orig.Close() }()

	pr, err := file.NewParquetReader(orig)
	if err != nil {
		return fmt.Errorf("failed to open existing parquet reader: %w", err)
	}

	fr, err := pqarrow.NewFileReader(pr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return fmt.Errorf("failed to create parquet arrow reader: %w", err)
	}

	tbl, err := fr.ReadTable(ctx)
	if err != nil {
		return fmt.Errorf("failed to read existing parquet table: %w", err)
	}
	defer tbl.Release()

	if !schemaEqualIgnoringMetadata(tbl.Schema(), expectedSchema) {
		return fmt.Errorf("append schema mismatch: existing=%v new=%v", tbl.Schema(), expectedSchema)
	}

	tr := array.NewTableReader(tbl, 1024*64)
	defer tr.Release()

	for tr.Next() {
		rec := tr.RecordBatch()
		toWrite := rec
		shouldRelease := false
		if !rec.Schema().Equal(expectedSchema) && schemaEqualIgnoringMetadata(rec.Schema(), expectedSchema) {
			normalized, err := normalizeRecordToSchema(rec, expectedSchema)
			if err != nil {
				return fmt.Errorf("failed to normalize existing parquet record schema: %w", err)
			}
			toWrite = normalized
			shouldRelease = true
		}

		if err := d.writer.WriteBuffered(toWrite); err != nil {
			if shouldRelease {
				toWrite.Release()
			}
			return fmt.Errorf("failed to copy existing parquet data: %w", err)
		}
		if shouldRelease {
			toWrite.Release()
		}
	}
	if err := tr.Err(); err != nil {
		return fmt.Errorf("failed to iterate existing parquet table: %w", err)
	}

	return nil
}

func (d *ParquetDestination) cleanupTempOnError() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tempPath != "" {
		_ = os.Remove(d.tempPath)
		d.tempPath = ""
	}
	d.appendMode = false
}

func schemaEqualIgnoringMetadata(a, b *arrow.Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.NumFields() != b.NumFields() {
		return false
	}

	af := make([]arrow.Field, a.NumFields())
	bf := make([]arrow.Field, b.NumFields())
	for i := 0; i < a.NumFields(); i++ {
		f := a.Field(i)
		f.Metadata = arrow.Metadata{}
		af[i] = f
	}
	for i := 0; i < b.NumFields(); i++ {
		f := b.Field(i)
		f.Metadata = arrow.Metadata{}
		bf[i] = f
	}

	na := arrow.NewSchema(af, nil)
	nb := arrow.NewSchema(bf, nil)
	return na.Equal(nb)
}

func stripSchemaMetadata(s *arrow.Schema) *arrow.Schema {
	if s == nil {
		return nil
	}
	fields := make([]arrow.Field, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		f.Metadata = arrow.Metadata{}
		fields[i] = f
	}
	return arrow.NewSchema(fields, nil)
}

func normalizeRecordToSchema(rec arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if rec == nil {
		return nil, nil
	}
	if target == nil {
		return nil, fmt.Errorf("target schema is nil")
	}
	if rec.NumCols() != int64(target.NumFields()) {
		return nil, fmt.Errorf("column count mismatch: record=%d schema=%d", rec.NumCols(), target.NumFields())
	}

	cols := make([]arrow.Array, rec.NumCols())
	for i := 0; i < int(rec.NumCols()); i++ {
		col := rec.Column(i)
		col.Retain()
		cols[i] = col
	}

	out := array.NewRecordBatch(target, cols, rec.NumRows())
	for _, c := range cols {
		c.Release()
	}
	return out, nil
}

func (d *ParquetDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	config.Debug("[PARQUET] SwapTable called (no-op for Parquet)")
	return nil
}

func (d *ParquetDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	// No-op for Parquet
	return nil
}

func (d *ParquetDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	// Parquet doesn't support transactions, return a no-op transaction
	return &parquetTransaction{}, nil
}

// parquetTransaction is a no-op transaction for Parquet
type parquetTransaction struct{}

func (t *parquetTransaction) Exec(ctx context.Context, sql string, args ...interface{}) error {
	return nil
}

func (t *parquetTransaction) Commit(ctx context.Context) error {
	return nil
}

func (t *parquetTransaction) Rollback(ctx context.Context) error {
	return nil
}

// MergeTable is not supported for Parquet destinations.
func (d *ParquetDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	return fmt.Errorf("merge strategy is not supported for Parquet destination")
}

// DeleteInsertTable is not supported for Parquet destinations.
func (d *ParquetDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	return fmt.Errorf("delete+insert strategy is not supported for Parquet destination")
}

// SCD2Table is not supported for Parquet destinations.
func (d *ParquetDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	return fmt.Errorf("scd2 strategy is not supported for Parquet destination")
}

// DropTable is a no-op for Parquet destinations.
func (d *ParquetDestination) DropTable(ctx context.Context, table string) error {
	// No-op for Parquet - files are managed by the file system
	return nil
}

// SupportsReplaceStrategy returns true as Parquet supports the replace strategy (overwrite file).
func (d *ParquetDestination) SupportsReplaceStrategy() bool { return true }

// SupportsAppendStrategy returns true as Parquet supports the append strategy.
func (d *ParquetDestination) SupportsAppendStrategy() bool { return true }

// SupportsMergeStrategy returns false as Parquet does not support the merge strategy.
func (d *ParquetDestination) SupportsMergeStrategy() bool { return false }

// SupportsDeleteInsertStrategy returns false as Parquet does not support the delete+insert strategy.
func (d *ParquetDestination) SupportsDeleteInsertStrategy() bool { return false }

// SupportsSCD2Strategy returns false as Parquet does not support the SCD2 strategy.
func (d *ParquetDestination) SupportsSCD2Strategy() bool { return false }

// SupportsAtomicSwap returns false as Parquet writes directly to the target file.
func (d *ParquetDestination) SupportsAtomicSwap() bool { return false }

func (d *ParquetDestination) GetScheme() string { return "parquet" }

func (d *ParquetDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	return nil, nil
}

// parseParquetPath extracts the file path from a parquet:// URI
func parseParquetPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	// parquet:///path/to/file.parquet -> /path/to/file.parquet
	path := u.Host + u.Path

	if path == "" {
		return "", fmt.Errorf("empty file path in URI")
	}

	return path, nil
}
