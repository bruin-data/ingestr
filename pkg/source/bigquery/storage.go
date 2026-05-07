package bigquery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	storage "cloud.google.com/go/bigquery/storage/apiv1"
	"cloud.google.com/go/bigquery/storage/apiv1/storagepb"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/sourcegraph/conc/pool"
	"google.golang.org/api/option"
)

// createStorageClient creates a BigQuery Storage Read API client with appropriate credentials.
func (d *Dialect) createStorageClient(ctx context.Context) (*storage.BigQueryReadClient, error) {
	var opts []option.ClientOption

	// Configure credentials based on what's available
	if d.credPath != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, d.credPath))
	} else if d.credJSON != "" {
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(d.credJSON)))
	}

	client, err := storage.NewBigQueryReadClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery Storage Read client: %w", err)
	}

	return client, nil
}

// createReadSession sets up a BigQuery Storage Read API session with filters and column selection.
func createReadSession(ctx context.Context, client *storage.BigQueryReadClient,
	projectID, dataset, table string, opts source.ReadOptions, tableSchema *schema.TableSchema, parallelism int,
) (*storagepb.ReadSession, error) {
	// Build the table reference
	tableRef := fmt.Sprintf("projects/%s/datasets/%s/tables/%s", projectID, dataset, table)

	// Determine selected fields (all columns minus excluded ones)
	selectedFields := make([]string, 0, len(tableSchema.Columns))
	excludeMap := make(map[string]bool)
	for _, col := range opts.ExcludeColumns {
		excludeMap[col] = true
	}

	for _, col := range tableSchema.Columns {
		if !excludeMap[col.Name] {
			selectedFields = append(selectedFields, col.Name)
		}
	}

	// Build row filter from ReadOptions
	rowFilter := buildRowFilter(opts, tableSchema)

	// Configure read session with Arrow format and LZ4 compression
	session := &storagepb.ReadSession{
		Table:      tableRef,
		DataFormat: storagepb.DataFormat_ARROW,
		ReadOptions: &storagepb.ReadSession_TableReadOptions{
			SelectedFields: selectedFields,
		},
	}

	// Set Arrow serialization options with LZ4 compression
	session.ReadOptions.OutputFormatSerializationOptions = &storagepb.ReadSession_TableReadOptions_ArrowSerializationOptions{
		ArrowSerializationOptions: &storagepb.ArrowSerializationOptions{
			BufferCompression: storagepb.ArrowSerializationOptions_LZ4_FRAME,
		},
	}

	// Add row filter if specified
	if rowFilter != "" {
		session.ReadOptions.RowRestriction = rowFilter
		config.Debug("[BIGQUERY] Row filter: %s", rowFilter)
	}

	config.Debug("[BIGQUERY] Selected fields: %v", selectedFields)

	// Create read session
	// Use single stream for optimal performance, matching SQLAlchemy's approach
	// Multiple streams add coordination overhead without benefit for most queries
	req := &storagepb.CreateReadSessionRequest{
		Parent:                  fmt.Sprintf("projects/%s", projectID),
		ReadSession:             session,
		MaxStreamCount:          int32(parallelism),
		PreferredMinStreamCount: int32(parallelism),
	}

	createdSession, err := client.CreateReadSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create read session: %w", err)
	}

	config.Debug("[BIGQUERY] Read session created with %d streams", len(createdSession.Streams))

	return createdSession, nil
}

// buildRowFilter translates ReadOptions into BigQuery Storage API row filter syntax.
func buildRowFilter(opts source.ReadOptions, tableSchema *schema.TableSchema) string {
	if opts.IncrementalKey == "" {
		return ""
	}

	// Find the column type for proper value formatting
	var columnType schema.DataType
	for _, col := range tableSchema.Columns {
		if col.Name == opts.IncrementalKey {
			columnType = col.DataType
			break
		}
	}

	var filters []string

	// Add start filter
	if opts.IntervalStart != nil {
		startValue := formatFilterValue(opts.IntervalStart, columnType)
		filters = append(filters, fmt.Sprintf("%s >= %s", opts.IncrementalKey, startValue))
	}

	// Add end filter
	if opts.IntervalEnd != nil {
		endValue := formatFilterValue(opts.IntervalEnd, columnType)
		filters = append(filters, fmt.Sprintf("%s <= %s", opts.IncrementalKey, endValue))
	}

	if len(filters) == 0 {
		return ""
	}

	return strings.Join(filters, " AND ")
}

// formatFilterValue formats a value for use in BigQuery Storage API row filters.
func formatFilterValue(value interface{}, dataType schema.DataType) string {
	switch dataType {
	case schema.TypeTimestamp, schema.TypeTimestampTZ:
		// Handle timestamp values
		switch v := value.(type) {
		case time.Time:
			return fmt.Sprintf("\"%s\"", v.Format(time.RFC3339))
		case string:
			// Try to parse as time, otherwise use as-is
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return fmt.Sprintf("\"%s\"", t.Format(time.RFC3339))
			}
			return fmt.Sprintf("\"%s\"", v)
		case int64:
			// Unix timestamp
			return fmt.Sprintf("%d", v)
		}

	case schema.TypeDate:
		// Handle date values
		switch v := value.(type) {
		case time.Time:
			return fmt.Sprintf("\"%s\"", v.Format("2006-01-02"))
		case string:
			return fmt.Sprintf("\"%s\"", v)
		}

	case schema.TypeInt64, schema.TypeInt32, schema.TypeInt16, schema.TypeFloat64, schema.TypeFloat32:
		// Numeric values don't need quotes
		return fmt.Sprintf("%v", value)

	case schema.TypeString:
		// String values need quotes and escaping
		return fmt.Sprintf("\"%s\"", escapeFilterString(fmt.Sprintf("%v", value)))
	}

	// Default: treat as string
	return fmt.Sprintf("\"%s\"", escapeFilterString(fmt.Sprintf("%v", value)))
}

// escapeFilterString escapes special characters in filter strings.
func escapeFilterString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// readStream reads Arrow data from a single BigQuery Storage API stream.
func readStream(ctx context.Context, client *storage.BigQueryReadClient,
	streamName string, arrowSchema *arrow.Schema, schemaBytes []byte, results chan<- source.RecordBatchResult, rowLimit int, rowCounter *atomic.Int64,
) {
	config.Debug("[BIGQUERY] Reading stream %s", streamName)

	// Create read rows request
	req := &storagepb.ReadRowsRequest{
		ReadStream: streamName,
	}

	streamReader, err := client.ReadRows(ctx, req)
	if err != nil {
		results <- source.RecordBatchResult{
			Err: fmt.Errorf("failed to read stream %s: %w", streamName, err),
		}
		return
	}

	batchCount := 0

	for {
		// Check if we've reached the limit
		if rowLimit > 0 && rowCounter.Load() >= int64(rowLimit) {
			config.Debug("[BIGQUERY] Stream %s: row limit reached, stopping", streamName)
			break
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			config.Debug("[BIGQUERY] Stream %s: context cancelled", streamName)
			return
		default:
		}

		// Read next response
		resp, err := streamReader.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			results <- source.RecordBatchResult{
				Err: fmt.Errorf("stream %s read error: %w", streamName, err),
			}
			return
		}

		// Deserialize Arrow record batch
		if resp.GetArrowRecordBatch() != nil {
			arrowBatch := resp.GetArrowRecordBatch()

			// Deserialize using pre-parsed schema to avoid re-parsing
			record, err := deserializeArrowRecordBatch(arrowSchema, schemaBytes, arrowBatch.SerializedRecordBatch)
			if err != nil {
				results <- source.RecordBatchResult{
					Err: fmt.Errorf("failed to deserialize Arrow record: %w", err),
				}
				return
			}

			numRows := record.NumRows()

			// Check row limit and potentially trim the record
			if rowLimit > 0 {
				currentCount := rowCounter.Load()
				if currentCount >= int64(rowLimit) {
					record.Release()
					return
				}
				if currentCount+numRows > int64(rowLimit) {
					// Trim the record to fit within the limit
					remainingRows := int64(rowLimit) - currentCount
					record = sliceArrowRecord(record, 0, remainingRows)
					numRows = record.NumRows()
				}
				rowCounter.Add(numRows)
			}

			batchCount++
			config.Debug("[BIGQUERY] Stream %s: batch %d with %d rows", streamName, batchCount, numRows)

			// Send the batch as-is from BigQuery
			results <- source.RecordBatchResult{Batch: record}
		}
	}

	config.Debug("[BIGQUERY] Stream %s completed: %d batches", streamName, batchCount)
}

// deserializeArrowRecordBatch converts BigQuery Storage API Arrow IPC bytes to arrow.RecordBatch.
// Following BigQuery's sample code, we concatenate schema + record batch bytes and use pre-parsed schema.
func deserializeArrowRecordBatch(arrowSchema *arrow.Schema, schemaBytes []byte, serializedBatch []byte) (arrow.RecordBatch, error) {
	// Create a buffer with schema bytes + record batch bytes
	buf := bytes.NewBuffer(schemaBytes)
	buf.Write(serializedBatch)

	// Create reader with pre-parsed schema to avoid re-parsing on every batch
	// This is the key optimization from BigQuery's Go sample code
	reader, err := ipc.NewReader(buf,
		ipc.WithAllocator(memory.DefaultAllocator),
		ipc.WithSchema(arrowSchema))
	if err != nil {
		return nil, fmt.Errorf("failed to create IPC reader: %w", err)
	}
	defer reader.Release()

	// Read the record
	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return nil, fmt.Errorf("failed to read record: %w", err)
		}
		return nil, fmt.Errorf("no record in batch")
	}

	rec := reader.RecordBatch()
	rec.Retain()
	return rec, nil
}

// sliceArrowRecord creates a new record with rows from offset to offset+length.
func sliceArrowRecord(record arrow.RecordBatch, offset int64, length int64) arrow.RecordBatch {
	if offset < 0 || length <= 0 || offset >= record.NumRows() {
		return nil
	}

	// Ensure we don't exceed record bounds
	if offset+length > record.NumRows() {
		length = record.NumRows() - offset
	}

	return record.NewSlice(offset, offset+length)
}

// ReadWithStorageAPI implements the StorageReader interface.
// Uses BigQuery Storage Read API for fast Arrow-native data reading.
func (d *Dialect) ReadWithStorageAPI(ctx context.Context, tableName string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	startTime := time.Now()

	// 1. Create storage client
	client, err := d.createStorageClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	// 2. Parse table name
	dataset, table := d.ParseTableName(tableName)
	if dataset == "" || table == "" {
		_ = client.Close()
		return nil, fmt.Errorf("invalid table name format, expected dataset.table")
	}

	config.Debug("[BIGQUERY] Creating read session for %s.%s", dataset, table)

	// 3. Get or use provided schema
	var tableSchema *schema.TableSchema
	if opts.Schema != nil {
		tableSchema = opts.Schema
	} else {
		tableSchema, err = d.GetSchema(ctx, tableName)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("failed to get schema: %w", err)
		}
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 15 // Default from --extract-parallelism flag
	}

	// 4. Create read session with filters
	session, err := createReadSession(ctx, client, d.projectID, dataset, table, opts, tableSchema, parallelism)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to create read session: %w", err)
	}

	// 5. Parse the Arrow schema once
	// We'll reuse this parsed schema for all stream readers to avoid re-parsing
	var arrowSchema *arrow.Schema
	var schemaBytes []byte
	if session.GetArrowSchema() != nil {
		schemaBytes = session.GetArrowSchema().SerializedSchema
		config.Debug("[BIGQUERY] Arrow schema received: %d bytes", len(schemaBytes))

		// Parse schema once here
		schemaReader, err := ipc.NewReader(bytes.NewBuffer(schemaBytes), ipc.WithAllocator(memory.DefaultAllocator))
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("failed to parse Arrow schema: %w", err)
		}
		arrowSchema = schemaReader.Schema()
		schemaReader.Release()
		config.Debug("[BIGQUERY] Arrow schema parsed successfully")
	} else {
		_ = client.Close()
		return nil, fmt.Errorf("no Arrow schema in read session")
	}

	results := make(chan source.RecordBatchResult, parallelism*3)

	streamCount := len(session.Streams)
	if streamCount == 0 {
		_ = client.Close()
		close(results)
		config.Debug("[BIGQUERY] No streams available, query returned 0 rows in %v", time.Since(startTime))
		return results, nil
	}

	config.Debug("[BIGQUERY] Reading %d streams with parallelism %d", streamCount, parallelism)

	// Shared row counter for limit tracking
	var rowCounter atomic.Int64

	// Read streams in parallel using conc pool
	go func() {
		defer func() { _ = client.Close() }()
		defer close(results)

		p := pool.New().WithMaxGoroutines(parallelism)
		for _, stream := range session.Streams {
			stream := stream // Capture loop variable
			p.Go(func() {
				readStream(ctx, client, stream.Name, arrowSchema, schemaBytes, results, opts.Limit, &rowCounter)
			})
		}
		p.Wait()

		totalRows := rowCounter.Load()
		config.Debug("[BIGQUERY] Storage API read completed: %d rows in %v", totalRows, time.Since(startTime))
	}()

	return results, nil
}
