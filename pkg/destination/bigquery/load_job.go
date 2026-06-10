package bigquery

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gcbq "cloud.google.com/go/bigquery"
	gcsstorage "cloud.google.com/go/storage"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
)

var queryJobJitter = randomQueryJobJitter

const (
	stagedGCSObjectChunkSize   = 32 * 1024 * 1024
	stagedGCSBufferSize        = 1 * 1024 * 1024
	maxLocalLoadJobParallelism = 4
	loadJobMaxAttempts         = 4
)

type loadJobFileFormat string

const (
	loadJobFormatParquet loadJobFileFormat = "parquet"
	loadJobFormatJSONL   loadJobFileFormat = "jsonl"
)

type stagedLoadChunk struct {
	index     int
	rows      int64
	localPath string
	gcsURI    string
	gcsBucket string
	gcsObject string
}

type stagedLoadSet struct {
	tempDir string
	format  loadJobFileFormat
	chunks  []stagedLoadChunk
}

type loadJobChunkWriter interface {
	WriteRecord(record arrow.RecordBatch) error
	Close() error
	Abort(cause error)
}

type bufferedWriteCloser struct {
	writer io.WriteCloser
	buffer *bufio.Writer
}

func newBufferedWriteCloser(writer io.WriteCloser) *bufferedWriteCloser {
	return &bufferedWriteCloser{
		writer: writer,
		buffer: bufio.NewWriterSize(writer, stagedGCSBufferSize),
	}
}

func (b *bufferedWriteCloser) Write(p []byte) (int, error) {
	return b.buffer.Write(p)
}

func (b *bufferedWriteCloser) Close() error {
	if err := b.buffer.Flush(); err != nil {
		if closeWithErr, ok := b.writer.(interface{ CloseWithError(error) error }); ok {
			_ = closeWithErr.CloseWithError(err)
		} else {
			_ = b.writer.Close()
		}
		return err
	}
	return b.writer.Close()
}

func (b *bufferedWriteCloser) CloseWithError(err error) error {
	if closeWithErr, ok := b.writer.(interface{ CloseWithError(error) error }); ok {
		return closeWithErr.CloseWithError(err)
	}
	return b.writer.Close()
}

func (d *BigQueryDestination) writeWithLoadJob(
	ctx context.Context,
	dataset string,
	table string,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	format, err := resolveLoadJobFileFormat(opts.LoaderFileFormat)
	if err != nil {
		return err
	}

	// Parquet has no native JSON type — JSON columns become STRING in Parquet
	// files, causing BigQuery to reject the load. Switch to JSONL which
	// natively supports JSON.
	if format == loadJobFormatParquet {
		tableRef := d.client.Dataset(dataset).Table(table)
		if meta, err := tableRef.Metadata(ctx); err == nil {
			for _, f := range meta.Schema {
				if f.Type == gcbq.JSONFieldType {
					config.Debug("[DEST] Table %s.%s has JSON columns; switching from Parquet to JSONL for load job", dataset, table)
					format = loadJobFormatJSONL
					break
				}
			}
		}
	}

	maxRowsPerFile := opts.LoaderFileSize
	if !opts.StagingTable && opts.StagingBucket == "" {
		maxRowsPerFile = 0
	}

	staged, err := d.stageLoadJobFiles(ctx, table, records, opts.StagingBucket, format, maxRowsPerFile)
	if err != nil {
		return err
	}
	if staged == nil {
		config.Debug("[DEST] No rows produced for %s.%s; skipping load job", dataset, table)
		return nil
	}
	defer staged.cleanupLocal()
	defer staged.cleanupRemote(context.Background(), d.gcsClient)

	logStagedLoadSet(dataset, table, staged)

	loadParallelism := d.resolveLoadJobParallelism(dataset, table, staged, opts)
	if opts.StagingTable && staged.hasLocalFiles() && len(staged.chunks) > 1 && opts.Parallelism > loadParallelism {
		config.Debug(
			"[DEST] Capping local load job parallelism from %d to %d for %s.%s to avoid BigQuery table update rate limits",
			opts.Parallelism,
			loadParallelism,
			dataset,
			table,
		)
	}
	if err := d.runLoadJobs(ctx, dataset, table, staged, loadParallelism); err != nil {
		return err
	}

	config.Debug("[DEST] Load job completed for %s.%s", dataset, table)
	return nil
}

func logStagedLoadSet(dataset string, table string, staged *stagedLoadSet) {
	if staged == nil {
		return
	}
	if staged.hasLocalFiles() {
		var totalBytes int64
		for _, chunk := range staged.chunks {
			info, err := os.Stat(chunk.localPath)
			if err != nil {
				config.Debug("[DEST] Failed to stat staged %s file for %s.%s at %s: %v", staged.format, dataset, table, chunk.localPath, err)
				continue
			}
			totalBytes += info.Size()
		}
		config.Debug("[DEST] Staged %d local %s file(s) totaling %d bytes for %s.%s", len(staged.chunks), staged.format, totalBytes, dataset, table)
		return
	}

	if len(staged.chunks) > 0 {
		config.Debug("[DEST] Staged %d %s object(s) for %s.%s starting at %s", len(staged.chunks), staged.format, dataset, table, staged.chunks[0].gcsURI)
	}
}

func (d *BigQueryDestination) stageLoadJobFiles(
	ctx context.Context,
	table string,
	records <-chan source.RecordBatchResult,
	stagingBucket string,
	format loadJobFileFormat,
	maxRowsPerFile int,
) (*stagedLoadSet, error) {
	if stagingBucket != "" {
		return d.stageLoadJobFilesToGCS(ctx, table, records, stagingBucket, format, maxRowsPerFile)
	}

	tempDir, err := os.MkdirTemp("", "ingestr-bq-load-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for load job: %w", err)
	}

	staged := &stagedLoadSet{
		tempDir: tempDir,
		format:  format,
	}

	chunks, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, resolveLoadJobRowsPerFile(maxRowsPerFile), func(part int) (stagedLoadChunk, io.WriteCloser, error) {
		path := buildLocalLoadFilePath(tempDir, table, format, part)
		writer, err := os.Create(path)
		if err != nil {
			return stagedLoadChunk{}, nil, err
		}
		return stagedLoadChunk{index: part, localPath: path}, writer, nil
	})
	if err != nil {
		staged.cleanupLocal()
		return nil, err
	}
	if rowsWritten == 0 {
		staged.cleanupLocal()
		return nil, nil
	}

	staged.chunks = chunks
	return staged, nil
}

func (d *BigQueryDestination) stageLoadJobFilesToGCS(
	ctx context.Context,
	table string,
	records <-chan source.RecordBatchResult,
	stagingBucket string,
	format loadJobFileFormat,
	maxRowsPerFile int,
) (*stagedLoadSet, error) {
	bucket, prefix, err := parseGCSBucketURI(stagingBucket)
	if err != nil {
		return nil, err
	}
	if err := d.ensureGCSClient(ctx); err != nil {
		return nil, err
	}

	objectPrefix := buildGCSLoadObjectPrefix(prefix, table)
	staged := &stagedLoadSet{
		format: format,
	}

	chunks, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, resolveLoadJobRowsPerFile(maxRowsPerFile), func(part int) (stagedLoadChunk, io.WriteCloser, error) {
		objectName := buildGCSLoadObjectName(objectPrefix, format, part)
		writer := d.gcsClient.Bucket(bucket).Object(objectName).NewWriter(ctx)
		writer.ChunkSize = stagedGCSObjectChunkSize
		attrs := buildStagingGCSObjectAttrs(format)
		writer.ContentType = attrs.ContentType
		writer.CacheControl = attrs.CacheControl
		writer.CustomTime = attrs.CustomTime
		writer.Metadata = attrs.Metadata
		return stagedLoadChunk{
			index:     part,
			gcsBucket: bucket,
			gcsObject: objectName,
			gcsURI:    "gs://" + bucket + "/" + objectName,
		}, newBufferedWriteCloser(writer), nil
	})
	staged.chunks = chunks
	if err != nil {
		staged.cleanupRemote(ctx, d.gcsClient)
		return nil, err
	}
	if rowsWritten == 0 {
		staged.cleanupRemote(ctx, d.gcsClient)
		return nil, nil
	}
	return staged, nil
}

func (d *BigQueryDestination) buildLoadSource(
	format loadJobFileFormat,
	chunk stagedLoadChunk,
) (gcbq.LoadSource, func(), error) {
	if chunk.localPath != "" {
		file, err := os.Open(chunk.localPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open staged %s file: %w", format, err)
		}

		src := gcbq.NewReaderSource(file)
		src.SourceFormat = format.bigQuerySourceFormat()
		if format == loadJobFormatParquet {
			src.ParquetOptions = &gcbq.ParquetOptions{EnableListInference: true}
		}
		return src, func() { _ = file.Close() }, nil
	}

	src := gcbq.NewGCSReference(chunk.gcsURI)
	src.SourceFormat = format.bigQuerySourceFormat()
	if format == loadJobFormatParquet {
		src.ParquetOptions = &gcbq.ParquetOptions{EnableListInference: true}
	}

	return src, func() {}, nil
}

func (d *BigQueryDestination) resolveLoadJobParallelism(
	_ string,
	_ string,
	staged *stagedLoadSet,
	opts destination.WriteOptions,
) int {
	if staged == nil || len(staged.chunks) <= 1 {
		return 1
	}
	if staged.hasOnlyGCSObjects() {
		return 1
	}
	if staged.hasLocalFiles() {
		if opts.Parallelism <= 0 {
			return 1
		}
		if opts.Parallelism > maxLocalLoadJobParallelism {
			return maxLocalLoadJobParallelism
		}
		return opts.Parallelism
	}
	if !opts.StagingTable || opts.Parallelism <= 0 {
		return 1
	}
	return opts.Parallelism
}

func (d *BigQueryDestination) ensureGCSClient(ctx context.Context) error {
	d.gcsClientMu.Lock()
	defer d.gcsClientMu.Unlock()

	if d.gcsClient != nil {
		return nil
	}

	var opts []option.ClientOption
	if d.credPath != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, d.credPath))
	} else if d.credJSON != "" {
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(d.credJSON)))
	}

	client, err := gcsstorage.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create GCS client for load-job staging: %w", err)
	}

	d.gcsClient = client
	return nil
}

func (d *BigQueryDestination) swapTableWithCopyJob(
	ctx context.Context,
	stagingDataset string,
	stagingTable string,
	targetDataset string,
	targetTable string,
) error {
	stagingRef := d.client.Dataset(stagingDataset).Table(stagingTable)
	targetRef := d.client.Dataset(targetDataset).Table(targetTable)

	copier := targetRef.CopierFrom(stagingRef)
	copier.CreateDisposition = gcbq.CreateIfNeeded
	copier.WriteDisposition = gcbq.WriteTruncate

	job, err := copier.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start copy job: %w", err)
	}

	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("copy job failed (job %s): %w", jobRef(job), err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("copy job error (job %s): %w", jobRef(job), err)
	}

	return nil
}

func parseGCSBucketURI(uri string) (string, string, error) {
	if uri == "" {
		return "", "", fmt.Errorf("empty staging bucket URI")
	}
	if !strings.HasPrefix(uri, "gs://") && !strings.HasPrefix(uri, "gcs://") {
		return "", "", fmt.Errorf("BigQuery load-job staging bucket must use gs:// or gcs://, got %q", uri)
	}

	trimmed := strings.TrimPrefix(strings.TrimPrefix(uri, "gs://"), "gcs://")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "", "", fmt.Errorf("invalid staging bucket URI %q", uri)
	}

	prefix := ""
	if len(parts) == 2 {
		prefix = strings.Trim(parts[1], "/")
	}
	return parts[0], prefix, nil
}

func resolveLoadJobFileFormat(requested string) (loadJobFileFormat, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", string(loadJobFormatParquet):
		return loadJobFormatParquet, nil
	case "json", "jsonl", "ndjson":
		return loadJobFormatJSONL, nil
	default:
		return "", fmt.Errorf("unsupported BigQuery load-job loader file format %q", requested)
	}
}

func (f loadJobFileFormat) fileExtension() string {
	switch f {
	case loadJobFormatJSONL:
		return "jsonl"
	default:
		return "parquet"
	}
}

func (f loadJobFileFormat) bigQuerySourceFormat() gcbq.DataFormat {
	switch f {
	case loadJobFormatJSONL:
		return gcbq.JSON
	default:
		return gcbq.Parquet
	}
}

func resolveLoadJobRowsPerFile(requested int) int64 {
	if requested <= 0 {
		return 0
	}
	return int64(requested)
}

func buildGCSLoadObjectPrefix(prefix string, table string) string {
	objectName := fmt.Sprintf("ingestr-load/%d/%s", time.Now().UnixNano(), sanitizeLoadObjectName(table))
	if prefix == "" {
		return objectName
	}
	return strings.TrimSuffix(prefix, "/") + "/" + objectName
}

func buildGCSLoadObjectName(prefix string, format loadJobFileFormat, part int) string {
	return fmt.Sprintf("%s/part-%06d.%s", prefix, part+1, format.fileExtension())
}

func buildLocalLoadFilePath(tempDir string, table string, format loadJobFileFormat, part int) string {
	return filepath.Join(tempDir, fmt.Sprintf("%s-part-%06d.%s", sanitizeLoadObjectName(table), part+1, format.fileExtension()))
}

func sanitizeLoadObjectName(value string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		".", "_",
		":", "_",
		" ", "_",
	)
	sanitized := replacer.Replace(value)
	if sanitized == "" {
		return "data"
	}
	return sanitized
}

func (s *stagedLoadSet) cleanupLocal() {
	if s.tempDir == "" {
		return
	}
	_ = os.RemoveAll(s.tempDir)
}

func (s *stagedLoadSet) hasLocalFiles() bool {
	if s == nil {
		return false
	}
	for _, chunk := range s.chunks {
		if chunk.localPath != "" {
			return true
		}
	}
	return false
}

func (s *stagedLoadSet) hasOnlyGCSObjects() bool {
	if s == nil || len(s.chunks) == 0 {
		return false
	}
	for _, chunk := range s.chunks {
		if chunk.gcsURI == "" || chunk.localPath != "" {
			return false
		}
	}
	return true
}

func (s *stagedLoadSet) cleanupRemote(ctx context.Context, client *gcsstorage.Client) {
	if client == nil || s == nil {
		return
	}

	for _, chunk := range s.chunks {
		if chunk.gcsBucket == "" || chunk.gcsObject == "" {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := client.Bucket(chunk.gcsBucket).Object(chunk.gcsObject).Delete(cleanupCtx)
		cancel()
		if err != nil && !errors.Is(err, gcsstorage.ErrObjectNotExist) {
			config.Debug("[DEST] Failed to delete staged GCS object %s: %v", chunk.gcsURI, err)
		}
	}
}

func buildStagingGCSObjectAttrs(format loadJobFileFormat) gcsstorage.ObjectAttrs {
	now := time.Now().UTC()
	contentType := "application/octet-stream"
	if format == loadJobFormatJSONL {
		contentType = "application/x-ndjson"
	}

	return gcsstorage.ObjectAttrs{
		ContentType:  contentType,
		CacheControl: "no-store",
		CustomTime:   now,
		Metadata: map[string]string{
			"ingestr-staging-object": "true",
			"ingestr-expires-at":     now.Add(destination.ManagedStagingTTL).Format(time.RFC3339),
		},
	}
}

func (d *BigQueryDestination) runLoadJobs(
	ctx context.Context,
	dataset string,
	table string,
	staged *stagedLoadSet,
	parallelism int,
) error {
	if staged == nil || len(staged.chunks) == 0 {
		return nil
	}
	if staged.hasOnlyGCSObjects() {
		config.Debug("[DEST] Loading %d staged %s chunk(s) into %s.%s with a single multi-URI load job", len(staged.chunks), staged.format, dataset, table)
		return d.runCombinedGCSLoadJob(ctx, dataset, table, staged.format, staged.chunks)
	}
	if parallelism <= 0 {
		parallelism = 1
	}
	if parallelism > len(staged.chunks) {
		parallelism = len(staged.chunks)
	}
	config.Debug("[DEST] Loading %d staged %s chunk(s) into %s.%s with parallelism=%d", len(staged.chunks), staged.format, dataset, table, parallelism)

	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := make(chan stagedLoadChunk)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range work {
				if err := d.runSingleLoadJob(loadCtx, dataset, table, staged.format, chunk); err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}

dispatch:
	for _, chunk := range staged.chunks {
		select {
		case <-loadCtx.Done():
			break dispatch
		case work <- chunk:
		}
	}
	close(work)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
}

func (d *BigQueryDestination) runCombinedGCSLoadJob(
	ctx context.Context,
	dataset string,
	table string,
	format loadJobFileFormat,
	chunks []stagedLoadChunk,
) error {
	loadSource, err := d.buildCombinedGCSLoadSource(format, chunks)
	if err != nil {
		return err
	}

	tableRef := d.client.Dataset(dataset).Table(table)
	loader := tableRef.LoaderFrom(loadSource)
	loader.CreateDisposition = gcbq.CreateNever
	loader.WriteDisposition = gcbq.WriteAppend

	config.Debug("[DEST] Starting combined load job for %d GCS chunk(s) into %s.%s", len(chunks), dataset, table)
	job, err := loader.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start combined load job: %w", err)
	}

	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("combined load job failed (job %s): %w", jobRef(job), err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("combined load job error (job %s): %w", jobRef(job), err)
	}

	config.Debug("[DEST] Combined load job finished for %s.%s", dataset, table)
	return nil
}

func (d *BigQueryDestination) runSingleLoadJob(
	ctx context.Context,
	dataset string,
	table string,
	format loadJobFileFormat,
	chunk stagedLoadChunk,
) error {
	maxAttempts := loadJobMaxAttempts

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		config.Debug(
			"[DEST] Starting load job for chunk %d (%d rows) into %s.%s (attempt %d/%d)",
			chunk.index+1,
			chunk.rows,
			dataset,
			table,
			attempt,
			maxAttempts,
		)
		loadSource, cleanup, err := d.buildLoadSource(format, chunk)
		if err != nil {
			return err
		}

		tableRef := d.client.Dataset(dataset).Table(table)
		loader := tableRef.LoaderFrom(loadSource)
		loader.CreateDisposition = gcbq.CreateNever
		loader.WriteDisposition = gcbq.WriteAppend

		job, err := loader.Run(ctx)
		cleanup()
		if err != nil {
			if attempt < maxAttempts && isRetryableLoadJobError(err) {
				backoff := time.Duration(attempt) * time.Second
				config.Debug("[DEST] Retrying load job for chunk %d after start error: %v", chunk.index+1, err)
				if err := sleepWithContextForLoadJob(ctx, backoff); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("failed to start load job for chunk %d: %w", chunk.index+1, err)
		}

		status, err := job.Wait(ctx)
		if err != nil {
			if attempt < maxAttempts && isRetryableLoadJobError(err) {
				backoff := time.Duration(attempt) * time.Second
				config.Debug("[DEST] Retrying load job for chunk %d after wait error: %v", chunk.index+1, err)
				if err := sleepWithContextForLoadJob(ctx, backoff); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("load job failed for chunk %d (job %s): %w", chunk.index+1, jobRef(job), err)
		}
		if err := status.Err(); err != nil {
			if attempt < maxAttempts && isRetryableLoadJobError(err) {
				backoff := time.Duration(attempt) * time.Second
				config.Debug("[DEST] Retrying load job for chunk %d after job error: %v", chunk.index+1, err)
				if err := sleepWithContextForLoadJob(ctx, backoff); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("load job error for chunk %d (job %s): %w", chunk.index+1, jobRef(job), err)
		}

		config.Debug("[DEST] Load job finished for chunk %d into %s.%s", chunk.index+1, dataset, table)
		return nil
	}

	return fmt.Errorf("load job error for chunk %d: exhausted retries", chunk.index+1)
}

func isRetryableLoadJobError(err error) bool {
	if err == nil {
		return false
	}

	var bqErrPtr *gcbq.Error
	if errors.As(err, &bqErrPtr) && bqErrPtr != nil {
		return isRetryableLoadJobReason(bqErrPtr.Reason, bqErrPtr.Message)
	}

	var bqErr gcbq.Error
	if errors.As(err, &bqErr) {
		return isRetryableLoadJobReason(bqErr.Reason, bqErr.Message)
	}

	var multiErrPtr *gcbq.MultiError
	if errors.As(err, &multiErrPtr) && multiErrPtr != nil {
		for _, item := range *multiErrPtr {
			if isRetryableLoadJobError(item) {
				return true
			}
		}
	}

	var multiErr gcbq.MultiError
	if errors.As(err, &multiErr) {
		for _, item := range multiErr {
			if isRetryableLoadJobError(item) {
				return true
			}
		}
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ratelimitexceeded") ||
		strings.Contains(msg, "quotaexceeded") ||
		strings.Contains(msg, "exceeded rate limits") ||
		strings.Contains(msg, "jobbackenderror") ||
		strings.Contains(msg, "backenderror") ||
		strings.Contains(msg, "not found: dataset") ||
		isBigQueryConcurrentUpdateError(err)
}

func isRetryableLoadJobReason(reason string, message string) bool {
	switch strings.ToLower(reason) {
	case "ratelimitexceeded", "quotaexceeded", "backenderror", "jobbackenderror", "aborted":
		return true
	}

	msg := strings.ToLower(message)
	if strings.Contains(msg, "not found: dataset") {
		return true
	}
	return strings.Contains(msg, "exceeded rate limits") ||
		strings.Contains(msg, "retrying the job may solve the problem") ||
		strings.Contains(msg, "could not serialize access") ||
		strings.Contains(msg, "concurrent update") ||
		strings.Contains(msg, "transaction is aborted")
}

func retryDelayForQueryJob(attempt int, err error) time.Duration {
	delay := time.Duration(attempt) * time.Second
	if !isBigQueryConcurrentUpdateError(err) {
		return delay
	}

	return delay + queryJobJitter(time.Duration(attempt+1)*time.Second)
}

func randomQueryJobJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return time.Duration(time.Now().UnixNano() % int64(max))
	}
	return time.Duration(n.Int64())
}

func isBigQueryConcurrentUpdateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "could not serialize access") ||
		strings.Contains(msg, "concurrent update") ||
		strings.Contains(msg, "transaction is aborted")
}

func sleepWithContextForLoadJob(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *BigQueryDestination) buildCombinedGCSLoadSource(
	format loadJobFileFormat,
	chunks []stagedLoadChunk,
) (gcbq.LoadSource, error) {
	uris := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.gcsURI == "" {
			return nil, fmt.Errorf("chunk %d is missing a GCS URI", chunk.index+1)
		}
		uris = append(uris, chunk.gcsURI)
	}

	src := gcbq.NewGCSReference(uris...)
	src.SourceFormat = format.bigQuerySourceFormat()
	if format == loadJobFormatParquet {
		src.ParquetOptions = &gcbq.ParquetOptions{EnableListInference: true}
	}
	return src, nil
}

func (d *BigQueryDestination) writeLoadJobStream(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	format loadJobFileFormat,
	openWriter func() (io.WriteCloser, error),
) (int64, error) {
	_, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, 0, func(_ int) (stagedLoadChunk, io.WriteCloser, error) {
		writer, err := openWriter()
		return stagedLoadChunk{}, writer, err
	})
	return rowsWritten, err
}

func (d *BigQueryDestination) writeJSONLStream(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	openWriter func() (io.WriteCloser, error),
) (int64, error) {
	return d.writeLoadJobStream(ctx, records, loadJobFormatJSONL, openWriter)
}

func (d *BigQueryDestination) writeParquetStream(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	openWriter func() (io.WriteCloser, error),
) (int64, error) {
	return d.writeLoadJobStream(ctx, records, loadJobFormatParquet, openWriter)
}

func (d *BigQueryDestination) writeLoadJobChunks(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	format loadJobFileFormat,
	maxRowsPerFile int64,
	openWriter func(part int) (stagedLoadChunk, io.WriteCloser, error),
) ([]stagedLoadChunk, int64, error) {
	var (
		chunks      []stagedLoadChunk
		totalRows   int64
		currentRows int64
		part        int
		chunkMeta   stagedLoadChunk
		chunkWriter loadJobChunkWriter
	)

	closeChunk := func() error {
		if chunkWriter == nil {
			return nil
		}
		if err := chunkWriter.Close(); err != nil {
			return err
		}
		chunks = append(chunks, chunkMeta)
		chunkWriter = nil
		currentRows = 0
		part++
		return nil
	}

	abortChunk := func(cause error) {
		if chunkWriter == nil {
			return
		}
		chunkWriter.Abort(cause)
		chunkWriter = nil
	}

	openChunk := func() error {
		meta, writer, err := openWriter(part)
		if err != nil {
			return err
		}
		chunkMeta = meta
		chunkMeta.index = part
		chunkWriter = newLoadJobChunkWriter(format, writer)
		currentRows = 0
		return nil
	}

	for result := range records {
		if result.Err != nil {
			abortChunk(result.Err)
			return nil, totalRows, result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		for start := int64(0); start < record.NumRows(); {
			if err := ctx.Err(); err != nil {
				record.Release()
				abortChunk(err)
				return nil, totalRows, err
			}

			if chunkWriter == nil {
				if err := openChunk(); err != nil {
					record.Release()
					return nil, totalRows, fmt.Errorf("failed to open %s staging writer: %w", format, err)
				}
			}

			end := record.NumRows()
			if maxRowsPerFile > 0 {
				remainingCapacity := maxRowsPerFile - currentRows
				if remainingCapacity <= 0 {
					if err := closeChunk(); err != nil {
						record.Release()
						return nil, totalRows, fmt.Errorf("failed to finalize %s staging writer: %w", format, err)
					}
					continue
				}
				if span := start + remainingCapacity; span < end {
					end = span
				}
			}

			slice := record.NewSlice(start, end)
			err := chunkWriter.WriteRecord(slice)
			slice.Release()
			if err != nil {
				record.Release()
				abortChunk(err)
				return nil, totalRows, fmt.Errorf("failed to write %s staging batch: %w", format, err)
			}

			rowsWritten := end - start
			totalRows += rowsWritten
			currentRows += rowsWritten
			chunkMeta.rows += rowsWritten
			start = end

			if maxRowsPerFile > 0 && currentRows >= maxRowsPerFile {
				if err := closeChunk(); err != nil {
					record.Release()
					return nil, totalRows, fmt.Errorf("failed to finalize %s staging writer: %w", format, err)
				}
			}
		}

		record.Release()
	}

	if chunkWriter == nil {
		return chunks, totalRows, nil
	}
	if err := closeChunk(); err != nil {
		return nil, totalRows, fmt.Errorf("failed to finalize %s staging writer: %w", format, err)
	}

	return chunks, totalRows, nil
}

func newLoadJobChunkWriter(format loadJobFileFormat, writer io.WriteCloser) loadJobChunkWriter {
	switch format {
	case loadJobFormatJSONL:
		return &jsonlChunkWriter{
			stageWriter: writer,
			bufferedW:   bufio.NewWriterSize(writer, stagedGCSBufferSize),
		}
	default:
		return &parquetChunkWriter{
			stageWriter: writer,
			writerProps: parquet.NewWriterProperties(
				parquet.WithCompression(compress.Codecs.Snappy),
				parquet.WithDictionaryDefault(true),
				parquet.WithDataPageSize(1024*1024),
			),
			arrowProps: pqarrow.NewArrowWriterProperties(
				pqarrow.WithStoreSchema(),
			),
		}
	}
}

type jsonlChunkWriter struct {
	stageWriter io.WriteCloser
	bufferedW   *bufio.Writer
}

func (w *jsonlChunkWriter) WriteRecord(record arrow.RecordBatch) error {
	_, err := writeRecordBatchAsJSONL(w.bufferedW, record)
	return err
}

func (w *jsonlChunkWriter) Close() error {
	if err := w.bufferedW.Flush(); err != nil {
		w.Abort(err)
		return err
	}
	return w.stageWriter.Close()
}

func (w *jsonlChunkWriter) Abort(cause error) {
	if closeWithErr, ok := w.stageWriter.(interface{ CloseWithError(error) error }); ok {
		_ = closeWithErr.CloseWithError(cause)
		return
	}
	_ = w.stageWriter.Close()
}

type parquetChunkWriter struct {
	stageWriter io.WriteCloser
	parquetW    *pqarrow.FileWriter
	arrowSchema *arrow.Schema
	writerProps *parquet.WriterProperties
	arrowProps  pqarrow.ArrowWriterProperties
}

func (w *parquetChunkWriter) WriteRecord(record arrow.RecordBatch) error {
	if w.parquetW == nil {
		arrowSchema := stripSchemaMetadata(record.Schema())
		parquetW, err := newParquetFileWriter(arrowSchema, w.stageWriter, w.writerProps, w.arrowProps)
		if err != nil {
			w.Abort(err)
			return err
		}
		w.arrowSchema = arrowSchema
		w.parquetW = parquetW
	}

	recordToWrite := record
	shouldRelease := false
	if w.arrowSchema != nil && !record.Schema().Equal(w.arrowSchema) && schemaEqualIgnoringMetadata(record.Schema(), w.arrowSchema) {
		normalized, err := normalizeRecordToSchema(record, w.arrowSchema)
		if err != nil {
			return err
		}
		recordToWrite = normalized
		shouldRelease = true
	}

	err := writeParquetRecord(w.parquetW, recordToWrite)
	if shouldRelease {
		recordToWrite.Release()
	}
	return err
}

func (w *parquetChunkWriter) Close() error {
	if w.parquetW == nil {
		return w.stageWriter.Close()
	}
	// pqarrow.FileWriter.Close() flushes the footer and closes the underlying writer.
	if err := closeParquetFileWriter(w.parquetW); err != nil {
		w.Abort(err)
		return err
	}
	return nil
}

func (w *parquetChunkWriter) Abort(cause error) {
	if closeWithErr, ok := w.stageWriter.(interface{ CloseWithError(error) error }); ok {
		_ = closeWithErr.CloseWithError(cause)
		return
	}
	_ = w.stageWriter.Close()
}

func newParquetFileWriter(
	schema *arrow.Schema,
	writer io.Writer,
	writerProps *parquet.WriterProperties,
	arrowProps pqarrow.ArrowWriterProperties,
) (_ *pqarrow.FileWriter, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parquet writer initialization panic: %v", recovered)
		}
	}()

	return pqarrow.NewFileWriter(schema, writer, writerProps, arrowProps)
}

func writeParquetRecord(writer *pqarrow.FileWriter, record arrow.RecordBatch) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parquet write panic: %v", recovered)
		}
	}()

	return writer.WriteBuffered(record)
}

func closeParquetFileWriter(writer *pqarrow.FileWriter) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parquet writer close panic: %v", recovered)
		}
	}()

	return writer.Close()
}

func writeRecordBatchAsJSONL(writer *bufio.Writer, record arrow.RecordBatch) (int64, error) {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)

	numRows := record.NumRows()
	numCols := int(record.NumCols())
	arrowSchema := record.Schema()

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		row := make(map[string]any, numCols)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			row[arrowSchema.Field(colIdx).Name] = extractJSONLValue(record.Column(colIdx), int(rowIdx))
		}

		if err := encoder.Encode(row); err != nil {
			return rowIdx, err
		}
	}

	return numRows, nil
}

func extractJSONLValue(arr arrow.Array, idx int) any {
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
	case *array.List:
		start, end := a.ValueOffsets(idx)
		return extractJSONLListValue(a.ListValues(), start, end)
	case *array.LargeList:
		start, end := a.ValueOffsets(idx)
		return extractJSONLListValue(a.ListValues(), start, end)
	case *array.FixedSizeList:
		start, end := a.ValueOffsets(idx)
		return extractJSONLListValue(a.ListValues(), start, end)
	case array.ExtensionArray:
		storage := a.Storage()
		if sb, ok := storage.(*array.String); ok {
			val := sb.Value(idx)
			var parsed any
			if err := json.Unmarshal([]byte(val), &parsed); err == nil {
				return parsed
			}
			return val
		}
		return extractJSONLValue(storage, idx)
	default:
		return arr.ValueStr(idx)
	}
}

func extractJSONLListValue(values arrow.Array, start, end int64) []any {
	out := make([]any, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, extractJSONLValue(values, int(i)))
	}
	return out
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
		field := a.Field(i)
		field.Metadata = arrow.Metadata{}
		af[i] = field
	}
	for i := 0; i < b.NumFields(); i++ {
		field := b.Field(i)
		field.Metadata = arrow.Metadata{}
		bf[i] = field
	}

	return arrow.NewSchema(af, nil).Equal(arrow.NewSchema(bf, nil))
}

func stripSchemaMetadata(s *arrow.Schema) *arrow.Schema {
	if s == nil {
		return nil
	}

	fields := make([]arrow.Field, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		field.Metadata = arrow.Metadata{}
		fields[i] = field
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
	for _, col := range cols {
		col.Release()
	}

	return out, nil
}
