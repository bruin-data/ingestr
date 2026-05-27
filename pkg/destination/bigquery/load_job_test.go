package bigquery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	gcbq "cloud.google.com/go/bigquery"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error { return nil }

type trackingWriteCloser struct {
	bytes.Buffer
	closeCount int
}

func (t *trackingWriteCloser) Close() error {
	t.closeCount++
	if t.closeCount > 1 {
		return errors.New("closed twice")
	}
	return nil
}

type shortWriteCloser struct{}

func (s shortWriteCloser) Write([]byte) (int, error) { return 0, nil }
func (s shortWriteCloser) Close() error              { return nil }

func TestWriteParquetStreamSkipsEmptyInput(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult)
	close(records)

	openCalls := 0
	rowsWritten, err := dest.writeParquetStream(context.Background(), records, func() (io.WriteCloser, error) {
		openCalls++
		return nopWriteCloser{Writer: io.Discard}, nil
	})
	if err != nil {
		t.Fatalf("writeParquetStream returned error: %v", err)
	}
	if rowsWritten != 0 {
		t.Fatalf("rowsWritten = %d, want 0", rowsWritten)
	}
	if openCalls != 0 {
		t.Fatalf("openWriter called %d times, want 0", openCalls)
	}
}

func TestBuildStagingGCSObjectAttrs(t *testing.T) {
	before := time.Now().UTC()
	attrs := buildStagingGCSObjectAttrs(loadJobFormatParquet)
	after := time.Now().UTC()

	if attrs.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q, want application/octet-stream", attrs.ContentType)
	}
	if attrs.CacheControl != "no-store" {
		t.Fatalf("cache control = %q, want no-store", attrs.CacheControl)
	}
	if attrs.CustomTime.Before(before) || attrs.CustomTime.After(after) {
		t.Fatalf("custom time %v outside expected window", attrs.CustomTime)
	}

	expiresAtRaw := attrs.Metadata["ingestr-expires-at"]
	if expiresAtRaw == "" {
		t.Fatal("ingestr-expires-at metadata missing")
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtRaw)
	if err != nil {
		t.Fatalf("failed to parse ingestr-expires-at: %v", err)
	}
	if expiresAt.Before(before.Add(destination.ManagedStagingTTL - time.Minute)) {
		t.Fatalf("expires-at %v too early", expiresAt)
	}
	if expiresAt.After(after.Add(destination.ManagedStagingTTL + time.Minute)) {
		t.Fatalf("expires-at %v too late", expiresAt)
	}

	jsonlAttrs := buildStagingGCSObjectAttrs(loadJobFormatJSONL)
	if jsonlAttrs.ContentType != "application/x-ndjson" {
		t.Fatalf("jsonl content type = %q, want application/x-ndjson", jsonlAttrs.ContentType)
	}
}

func TestBuildGCSLoadObjectNameUsesSanitizedTable(t *testing.T) {
	objectPrefix := buildGCSLoadObjectPrefix("prefix/path", "orders.daily/report")
	objectName := buildGCSLoadObjectName(objectPrefix, loadJobFormatParquet, 0)

	if !strings.HasPrefix(objectName, "prefix/path/ingestr-load/") {
		t.Fatalf("object name %q missing prefix", objectName)
	}
	if !strings.HasSuffix(objectName, "/orders_daily_report/part-000001.parquet") {
		t.Fatalf("object name %q missing sanitized table suffix", objectName)
	}

	jsonlName := buildGCSLoadObjectName(objectPrefix, loadJobFormatJSONL, 1)
	if !strings.HasSuffix(jsonlName, "/orders_daily_report/part-000002.jsonl") {
		t.Fatalf("object name %q missing jsonl suffix", jsonlName)
	}
}

func TestWriteParquetStreamClosesWriterOnce(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: makeTestRecordBatch(t, 1)}
	close(records)

	writer := &trackingWriteCloser{}
	rowsWritten, err := dest.writeParquetStream(context.Background(), records, func() (io.WriteCloser, error) {
		return writer, nil
	})
	if err != nil {
		t.Fatalf("writeParquetStream returned error: %v", err)
	}
	if rowsWritten != 1 {
		t.Fatalf("rowsWritten = %d, want 1", rowsWritten)
	}
	if writer.closeCount != 1 {
		t.Fatalf("writer closed %d times, want 1", writer.closeCount)
	}
}

func TestParquetChunkWriterCloseClosesStageWriter(t *testing.T) {
	writer := &trackingWriteCloser{}
	chunkWriter := &parquetChunkWriter{
		stageWriter: writer,
		writerProps: parquet.NewWriterProperties(),
		arrowProps:  pqarrow.NewArrowWriterProperties(),
	}

	record := makeTestRecordBatch(t, 1)
	defer record.Release()

	if err := chunkWriter.WriteRecord(record); err != nil {
		t.Fatalf("WriteRecord returned error: %v", err)
	}
	if err := chunkWriter.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if writer.closeCount != 1 {
		t.Fatalf("writer closed %d times, want 1", writer.closeCount)
	}
}

func TestBufferedWriteCloserFlushesOnClose(t *testing.T) {
	underlying := &trackingWriteCloser{}
	writer := newBufferedWriteCloser(underlying)

	n, err := writer.Write([]byte("parquet"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len("parquet") {
		t.Fatalf("Write wrote %d bytes, want %d", n, len("parquet"))
	}
	if underlying.Len() != 0 {
		t.Fatalf("underlying writer received %d bytes before Close, want 0", underlying.Len())
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if underlying.String() != "parquet" {
		t.Fatalf("underlying writer contents = %q, want parquet", underlying.String())
	}
}

func TestNewParquetFileWriterRecoversPanic(t *testing.T) {
	record := makeTestRecordBatch(t, 1)
	defer record.Release()

	schema := record.Schema()
	if _, err := newParquetFileWriter(schema, shortWriteCloser{}, parquet.NewWriterProperties(), pqarrow.NewArrowWriterProperties()); err == nil {
		t.Fatal("newParquetFileWriter returned nil error, want recovered panic")
	}
}

func TestResolveLoadJobFileFormat(t *testing.T) {
	tests := []struct {
		in   string
		want loadJobFileFormat
		ok   bool
	}{
		{in: "", want: loadJobFormatParquet, ok: true},
		{in: "parquet", want: loadJobFormatParquet, ok: true},
		{in: "json", want: loadJobFormatJSONL, ok: true},
		{in: "jsonl", want: loadJobFormatJSONL, ok: true},
		{in: "ndjson", want: loadJobFormatJSONL, ok: true},
		{in: "csv", ok: false},
	}

	for _, tt := range tests {
		got, err := resolveLoadJobFileFormat(tt.in)
		if tt.ok {
			if err != nil {
				t.Fatalf("resolveLoadJobFileFormat(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("resolveLoadJobFileFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("resolveLoadJobFileFormat(%q) returned nil error", tt.in)
		}
	}
}

func TestWriteJSONLStreamPreservesJSONFields(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult, 1)
	record := makeJSONRecordBatch(t)
	records <- source.RecordBatchResult{Batch: record}
	close(records)

	var output bytes.Buffer
	rowsWritten, err := dest.writeJSONLStream(context.Background(), records, func() (io.WriteCloser, error) {
		return nopWriteCloser{Writer: &output}, nil
	})
	if err != nil {
		t.Fatalf("writeJSONLStream returned error: %v", err)
	}
	if rowsWritten != 2 {
		t.Fatalf("rowsWritten = %d, want 2", rowsWritten)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d jsonl lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "\"payload\":{") || !strings.Contains(lines[0], "\"kind\":\"odd\"") || !strings.Contains(lines[0], "\"id\":1") {
		t.Fatalf("first line missing JSON object payload: %s", lines[0])
	}
	if !strings.Contains(lines[1], "\"numbers\":[2,3]") {
		t.Fatalf("second line missing numbers array: %s", lines[1])
	}
}

func TestWriteLoadJobChunksSplitsJSONLByLoaderFileSize(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: makeTestRecordBatch(t, 1, 2, 3, 4, 5)}
	close(records)

	var writers []*trackingWriteCloser
	chunks, rowsWritten, err := dest.writeLoadJobChunks(context.Background(), records, loadJobFormatJSONL, 2, func(part int) (stagedLoadChunk, io.WriteCloser, error) {
		writer := &trackingWriteCloser{}
		writers = append(writers, writer)
		return stagedLoadChunk{index: part}, writer, nil
	})
	if err != nil {
		t.Fatalf("writeLoadJobChunks returned error: %v", err)
	}
	if rowsWritten != 5 {
		t.Fatalf("rowsWritten = %d, want 5", rowsWritten)
	}
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}

	wantRows := []int64{2, 2, 1}
	for i, chunk := range chunks {
		if chunk.rows != wantRows[i] {
			t.Fatalf("chunk %d rows = %d, want %d", i, chunk.rows, wantRows[i])
		}
		if writers[i].closeCount != 1 {
			t.Fatalf("writer %d closeCount = %d, want 1", i, writers[i].closeCount)
		}

		lines := strings.Split(strings.TrimSpace(writers[i].String()), "\n")
		if len(lines) != int(wantRows[i]) {
			t.Fatalf("writer %d line count = %d, want %d", i, len(lines), wantRows[i])
		}
	}
}

func TestWriteLoadJobChunksSplitsParquetByLoaderFileSize(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: makeTestRecordBatch(t, 1, 2, 3, 4, 5)}
	close(records)

	var writers []*trackingWriteCloser
	chunks, rowsWritten, err := dest.writeLoadJobChunks(context.Background(), records, loadJobFormatParquet, 2, func(part int) (stagedLoadChunk, io.WriteCloser, error) {
		writer := &trackingWriteCloser{}
		writers = append(writers, writer)
		return stagedLoadChunk{index: part}, writer, nil
	})
	if err != nil {
		t.Fatalf("writeLoadJobChunks returned error: %v", err)
	}
	if rowsWritten != 5 {
		t.Fatalf("rowsWritten = %d, want 5", rowsWritten)
	}
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}

	wantRows := []int64{2, 2, 1}
	for i, chunk := range chunks {
		if chunk.rows != wantRows[i] {
			t.Fatalf("chunk %d rows = %d, want %d", i, chunk.rows, wantRows[i])
		}
		if writers[i].closeCount != 1 {
			t.Fatalf("writer %d closeCount = %d, want 1", i, writers[i].closeCount)
		}
		if writers[i].Len() == 0 {
			t.Fatalf("writer %d received no parquet bytes", i)
		}
	}
}

func TestWriteLoadJobChunksKeepsExactBoundaryChunks(t *testing.T) {
	dest := &BigQueryDestination{}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: makeTestRecordBatch(t, 1, 2, 3, 4)}
	close(records)

	var writers []*trackingWriteCloser
	chunks, rowsWritten, err := dest.writeLoadJobChunks(context.Background(), records, loadJobFormatParquet, 2, func(part int) (stagedLoadChunk, io.WriteCloser, error) {
		writer := &trackingWriteCloser{}
		writers = append(writers, writer)
		return stagedLoadChunk{index: part}, writer, nil
	})
	if err != nil {
		t.Fatalf("writeLoadJobChunks returned error: %v", err)
	}
	if rowsWritten != 4 {
		t.Fatalf("rowsWritten = %d, want 4", rowsWritten)
	}
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}

	for i, chunk := range chunks {
		if chunk.rows != 2 {
			t.Fatalf("chunk %d rows = %d, want 2", i, chunk.rows)
		}
		if writers[i].closeCount != 1 {
			t.Fatalf("writer %d closeCount = %d, want 1", i, writers[i].closeCount)
		}
		if writers[i].Len() == 0 {
			t.Fatalf("writer %d received no parquet bytes", i)
		}
	}
}

func TestBuildCombinedGCSLoadSourceUsesAllChunkURIs(t *testing.T) {
	dest := &BigQueryDestination{}
	chunks := []stagedLoadChunk{
		{index: 0, gcsURI: "gs://bucket/prefix/part-000001.parquet"},
		{index: 1, gcsURI: "gs://bucket/prefix/part-000002.parquet"},
	}

	src, err := dest.buildCombinedGCSLoadSource(loadJobFormatParquet, chunks)
	if err != nil {
		t.Fatalf("buildCombinedGCSLoadSource returned error: %v", err)
	}

	ref, ok := src.(*gcbq.GCSReference)
	if !ok {
		t.Fatalf("load source type = %T, want *bigquery.GCSReference", src)
	}
	wantURIs := []string{
		"gs://bucket/prefix/part-000001.parquet",
		"gs://bucket/prefix/part-000002.parquet",
	}
	if !reflect.DeepEqual(ref.URIs, wantURIs) {
		t.Fatalf("GCSReference.URIs = %v, want %v", ref.URIs, wantURIs)
	}
	if ref.SourceFormat != gcbq.Parquet {
		t.Fatalf("GCSReference.SourceFormat = %v, want Parquet", ref.SourceFormat)
	}
	if ref.ParquetOptions == nil || !ref.ParquetOptions.EnableListInference {
		t.Fatalf("ParquetOptions = %+v, want EnableListInference=true", ref.ParquetOptions)
	}
}

func TestResolveLoadJobParallelism(t *testing.T) {
	dest := &BigQueryDestination{}

	tests := []struct {
		name   string
		staged *stagedLoadSet
		opts   destination.WriteOptions
		want   int
	}{
		{
			name: "single chunk stays sequential",
			staged: &stagedLoadSet{
				chunks: []stagedLoadChunk{{index: 0, localPath: "/tmp/part-1.parquet"}},
			},
			opts: destination.WriteOptions{Parallelism: 8, StagingTable: true},
			want: 1,
		},
		{
			name: "multi chunk local staging uses capped parallelism",
			staged: &stagedLoadSet{
				chunks: []stagedLoadChunk{
					{index: 0, localPath: "/tmp/part-1.parquet"},
					{index: 1, localPath: "/tmp/part-2.parquet"},
				},
			},
			opts: destination.WriteOptions{Parallelism: 8, StagingTable: true},
			want: maxLocalLoadJobParallelism,
		},
		{
			name: "gcs chunks use combined single load job",
			staged: &stagedLoadSet{
				chunks: []stagedLoadChunk{
					{index: 0, gcsURI: "gs://bucket/part-1.parquet", gcsBucket: "bucket", gcsObject: "part-1.parquet"},
					{index: 1, gcsURI: "gs://bucket/part-2.parquet", gcsBucket: "bucket", gcsObject: "part-2.parquet"},
				},
			},
			opts: destination.WriteOptions{Parallelism: 8, StagingTable: true},
			want: 1,
		},
		{
			name: "non staging fallback stays sequential",
			staged: &stagedLoadSet{
				chunks: []stagedLoadChunk{
					{index: 0},
					{index: 1},
				},
			},
			opts: destination.WriteOptions{Parallelism: 8, StagingTable: false},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dest.resolveLoadJobParallelism("dataset", "table", tt.staged, tt.opts)
			if got != tt.want {
				t.Fatalf("resolveLoadJobParallelism() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsRetryableLoadJobError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "bigquery rate limit error",
			err:  gcbq.Error{Reason: "rateLimitExceeded", Message: "Exceeded rate limits"},
			want: true,
		},
		{
			name: "bigquery multi error with quota exceeded",
			err: gcbq.MultiError{
				gcbq.Error{Reason: "quotaExceeded", Message: "quota"},
			},
			want: true,
		},
		{
			name: "wrapped bigquery rate limit pointer error",
			err:  fmt.Errorf("wrapped: %w", &gcbq.Error{Reason: "rateLimitExceeded", Message: "Exceeded rate limits"}),
			want: true,
		},
		{
			name: "wrapped bigquery multi error pointer",
			err:  fmt.Errorf("wrapped: %w", &gcbq.MultiError{gcbq.Error{Reason: "quotaExceeded", Message: "quota"}}),
			want: true,
		},
		{
			name: "bigquery backend error reason",
			err:  gcbq.Error{Reason: "backendError", Message: "The job encountered an error during execution."},
			want: true,
		},
		{
			name: "bigquery job backend error reason variant",
			err:  gcbq.Error{Reason: "jobBackendError", Message: "boom"},
			want: true,
		},
		{
			name: "bigquery backend error message hint",
			err:  gcbq.Error{Reason: "", Message: "The job encountered an error during execution. Retrying the job may solve the problem."},
			want: true,
		},
		{
			name: "wrapped backend error pointer",
			err:  fmt.Errorf("wrapped: %w", &gcbq.Error{Reason: "backendError", Message: "boom"}),
			want: true,
		},
		{
			name: "non retryable invalid error",
			err:  gcbq.Error{Reason: "invalid", Message: "bad request"},
			want: false,
		},
		{
			name: "non retryable internal error (deterministic)",
			err:  gcbq.Error{Reason: "internalError", Message: "internal"},
			want: false,
		},
		{
			name: "dataset not found (eventual consistency after staging dataset create)",
			err:  gcbq.Error{Reason: "notFound", Message: "Not found: Dataset my-project:_bruin_staging"},
			want: true,
		},
		{
			name: "wrapped dataset not found",
			err:  fmt.Errorf("wrapped: %w", &gcbq.Error{Reason: "notFound", Message: "Not found: Dataset my-project:_bruin_staging, notFound"}),
			want: true,
		},
		{
			name: "table not found stays non-retryable (real bug, not propagation)",
			err:  gcbq.Error{Reason: "notFound", Message: "Not found: Table my-project:_bruin_staging.orders"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableLoadJobError(tt.err)
			if got != tt.want {
				t.Fatalf("isRetryableLoadJobError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func makeJSONRecordBatch(t *testing.T) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewGoAllocator()

	idBuilder := array.NewInt64Builder(pool)
	defer idBuilder.Release()
	idBuilder.AppendValues([]int64{1, 2}, nil)
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	jsonBuilder := schema.NewJSONBuilder(pool)
	defer jsonBuilder.Release()
	jsonBuilder.Append(`{"kind":"odd","id":1}`)
	jsonBuilder.Append(`{"kind":"even","id":2}`)
	payloadArr := jsonBuilder.NewArray()
	defer payloadArr.Release()

	listBuilder := array.NewListBuilder(pool, arrow.PrimitiveTypes.Int64)
	defer listBuilder.Release()
	valueBuilder := listBuilder.ValueBuilder().(*array.Int64Builder)
	listBuilder.Append(true)
	valueBuilder.AppendValues([]int64{1, 2}, nil)
	listBuilder.Append(true)
	valueBuilder.AppendValues([]int64{2, 3}, nil)
	numbersArr := listBuilder.NewArray()
	defer numbersArr.Release()

	recordSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "payload", Type: schema.JSONArrowType},
		{Name: "numbers", Type: numbersArr.DataType()},
	}, nil)

	return array.NewRecordBatch(recordSchema, []arrow.Array{idArr, payloadArr, numbersArr}, 2)
}
