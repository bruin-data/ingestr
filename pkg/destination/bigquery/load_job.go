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
	"sync/atomic"
	"time"

	gcbq "cloud.google.com/go/bigquery"
	gcsstorage "cloud.google.com/go/storage"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
)

var (
	queryJobJitter         = randomQueryJobJitter
	loadJobStartRetryDelay = func(attempt int) time.Duration { return time.Duration(attempt) * time.Second }
)

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

	// ignoreUnknownValues is set for pre-staged files, which may carry columns
	// that schema inference later dropped (all-null unknown columns).
	ignoreUnknownValues bool
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
	project string,
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
		tableRef := d.client.DatasetInProject(project, dataset).Table(table)
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

	// The pipeline's schema evolution plan decides the schema for both the
	// destination and the staging table; the load file must match that table.
	var targetSchema *arrow.Schema
	if opts.Schema != nil {
		targetSchema = opts.Schema.ToArrowSchema()
	}

	staged, err := d.stageLoadJobFiles(ctx, table, records, opts.StagingBucket, format, maxRowsPerFile, targetSchema)
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
	if _, err := d.runLoadJobs(ctx, project, dataset, table, staged, loadParallelism); err != nil {
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
	targetSchema *arrow.Schema,
) (*stagedLoadSet, error) {
	if stagingBucket != "" {
		return d.stageLoadJobFilesToGCS(ctx, table, records, stagingBucket, format, maxRowsPerFile, targetSchema)
	}

	tempDir, err := os.MkdirTemp("", "ingestr-bq-load-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for load job: %w", err)
	}

	staged := &stagedLoadSet{
		tempDir: tempDir,
		format:  format,
	}

	chunks, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, resolveLoadJobRowsPerFile(maxRowsPerFile), targetSchema, func(part int) (stagedLoadChunk, io.WriteCloser, error) {
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
	targetSchema *arrow.Schema,
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

	chunks, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, resolveLoadJobRowsPerFile(maxRowsPerFile), targetSchema, func(part int) (stagedLoadChunk, io.WriteCloser, error) {
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
	ignoreUnknownValues bool,
) (gcbq.LoadSource, func(), error) {
	if chunk.localPath != "" {
		file, err := os.Open(chunk.localPath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open staged %s file: %w", format, err)
		}

		src := gcbq.NewReaderSource(file)
		src.SourceFormat = format.bigQuerySourceFormat()
		src.IgnoreUnknownValues = ignoreUnknownValues
		if format == loadJobFormatParquet {
			src.ParquetOptions = &gcbq.ParquetOptions{EnableListInference: true}
		}
		return src, func() { _ = file.Close() }, nil
	}

	src := gcbq.NewGCSReference(chunk.gcsURI)
	src.SourceFormat = format.bigQuerySourceFormat()
	src.IgnoreUnknownValues = ignoreUnknownValues
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
	stagingProject string,
	stagingDataset string,
	stagingTable string,
	targetProject string,
	targetDataset string,
	targetTable string,
) error {
	stagingRef := d.client.DatasetInProject(stagingProject, stagingDataset).Table(stagingTable)
	targetRef := d.client.DatasetInProject(targetProject, targetDataset).Table(targetTable)

	copier := targetRef.CopierFrom(stagingRef)
	copier.CreateDisposition = gcbq.CreateIfNeeded
	copier.WriteDisposition = gcbq.WriteTruncate
	jobID := "ingestr_copy_" + strings.TrimPrefix(newBigQueryQueryJobID(), "ingestr_")

	job, err := d.startCopyJobWithRetry(ctx, copier, jobID, stagingRef, targetRef)
	if err != nil {
		return fmt.Errorf("failed to start copy job: %w", err)
	}

	status, err := d.waitForBigQueryJob(ctx, job)
	if err != nil {
		return fmt.Errorf("copy job failed (job %s): %w", jobRef(job), err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("copy job error (job %s): %w", jobRef(job), err)
	}

	return nil
}

func (d *BigQueryDestination) startCopyJobWithRetry(ctx context.Context, copier *gcbq.Copier, jobID string, sourceRef, targetRef *gcbq.Table) (*gcbq.Job, error) {
	if err := d.beginCDCJob(ctx, jobID); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		copier.JobID = jobID
		copier.ProjectID = d.projectID
		if d.location != "" {
			copier.Location = d.location
		}
		job, err := copier.Run(ctx)
		if err == nil {
			return job, nil
		}
		if ctx.Err() != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
		if isBigQueryDuplicateJobError(err) {
			job, recoverErr := d.recoverDuplicateCopyJob(ctx, jobID, sourceRef, targetRef)
			if recoverErr == nil {
				return job, nil
			}
			err = recoverErr
		}
		if !isRetryableLoadJobError(err) && !isNotFoundError(err) {
			_ = d.resolveCDCJob(context.Background(), jobID)
			return nil, err
		}
		config.Debug("[DEST] Retrying ambiguous copy job start with stable job ID %s: %v", jobID, err)
		if err := sleepWithContextForLoadJob(ctx, loadJobStartRetryDelay(min(attempt, loadJobMaxAttempts))); err != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
	}
}

func (d *BigQueryDestination) recoverDuplicateCopyJob(ctx context.Context, jobID string, sourceRef, targetRef *gcbq.Table) (*gcbq.Job, error) {
	job, err := d.client.JobFromProject(ctx, d.projectID, jobID, d.location)
	if err != nil {
		return nil, err
	}
	cfg, err := job.Config()
	if err != nil {
		return nil, err
	}
	copyCfg, ok := cfg.(*gcbq.CopyConfig)
	if !ok || copyCfg.Dst == nil || len(copyCfg.Srcs) != 1 {
		return nil, fmt.Errorf("existing job %s is %T, not the expected single-source copy job", jobID, cfg)
	}
	if !sameBigQueryTable(copyCfg.Dst, targetRef) || !sameBigQueryTable(copyCfg.Srcs[0], sourceRef) {
		return nil, fmt.Errorf("existing copy job %s does not match the requested source and destination", jobID)
	}
	return job, nil
}

func sameBigQueryTable(left, right *gcbq.Table) bool {
	return left != nil && right != nil && left.ProjectID == right.ProjectID && left.DatasetID == right.DatasetID && left.TableID == right.TableID
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

// runLoadJobs executes the load jobs for a staged set and returns the total
// number of rows BigQuery reported loading, or -1 when the count could not be
// determined from the job statistics.
func (d *BigQueryDestination) runLoadJobs(
	ctx context.Context,
	project string,
	dataset string,
	table string,
	staged *stagedLoadSet,
	parallelism int,
) (int64, error) {
	if staged == nil || len(staged.chunks) == 0 {
		return 0, nil
	}
	if staged.hasOnlyGCSObjects() {
		config.Debug("[DEST] Loading %d staged %s chunk(s) into %s.%s with a single multi-URI load job", len(staged.chunks), staged.format, dataset, table)
		return d.runCombinedGCSLoadJob(ctx, project, dataset, table, staged, staged.chunks)
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

	var outputRows atomic.Int64
	var outputRowsUnknown atomic.Bool

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range work {
				rows, err := d.runSingleLoadJob(loadCtx, project, dataset, table, staged, chunk)
				if err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
				if rows < 0 {
					outputRowsUnknown.Store(true)
				} else {
					outputRows.Add(rows)
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
		return -1, err
	default:
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		if outputRowsUnknown.Load() {
			return -1, nil
		}
		return outputRows.Load(), nil
	}
}

func (d *BigQueryDestination) runCombinedGCSLoadJob(
	ctx context.Context,
	project string,
	dataset string,
	table string,
	staged *stagedLoadSet,
	chunks []stagedLoadChunk,
) (int64, error) {
	loadSource, err := d.buildCombinedGCSLoadSource(staged.format, chunks, staged.ignoreUnknownValues)
	if err != nil {
		return -1, err
	}

	tableRef := d.client.DatasetInProject(project, dataset).Table(table)
	jobID := loadJobAttemptID(newBigQueryLoadJobID(), 1)

	config.Debug("[DEST] Starting combined load job for %d GCS chunk(s) into %s.%s", len(chunks), dataset, table)
	job, err := d.startLoadJobWithRetry(ctx, jobID, tableRef, func() (*gcbq.Loader, func(), error) {
		loader := tableRef.LoaderFrom(loadSource)
		loader.CreateDisposition = gcbq.CreateNever
		loader.WriteDisposition = gcbq.WriteAppend
		return loader, func() {}, nil
	})
	if err != nil {
		return -1, fmt.Errorf("failed to start combined load job: %w", err)
	}

	status, err := d.waitForBigQueryJob(ctx, job)
	if err != nil {
		return -1, fmt.Errorf("combined load job failed (job %s): %w", jobRef(job), err)
	}
	if err := status.Err(); err != nil {
		if details := loadJobErrorDetails(status); details != "" {
			return -1, fmt.Errorf("combined load job error (job %s): %w; details: %s", jobRef(job), err, details)
		}
		return -1, fmt.Errorf("combined load job error (job %s): %w", jobRef(job), err)
	}

	config.Debug("[DEST] Combined load job finished for %s.%s", dataset, table)
	return loadJobOutputRows(status), nil
}

// loadJobOutputRows extracts the loaded row count from a completed load job's
// statistics, returning -1 when unavailable.
func loadJobOutputRows(status *gcbq.JobStatus) int64 {
	if status == nil || status.Statistics == nil {
		return -1
	}
	details, ok := status.Statistics.Details.(*gcbq.LoadStatistics)
	if !ok || details == nil {
		return -1
	}
	return details.OutputRows
}

func (d *BigQueryDestination) runSingleLoadJob(
	ctx context.Context,
	project string,
	dataset string,
	table string,
	staged *stagedLoadSet,
	chunk stagedLoadChunk,
) (int64, error) {
	maxAttempts := loadJobMaxAttempts
	baseJobID := newBigQueryLoadJobID()

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
		tableRef := d.client.DatasetInProject(project, dataset).Table(table)
		jobID := loadJobAttemptID(baseJobID, attempt)
		job, err := d.startLoadJobWithRetry(ctx, jobID, tableRef, func() (*gcbq.Loader, func(), error) {
			loadSource, cleanup, err := d.buildLoadSource(staged.format, chunk, staged.ignoreUnknownValues)
			if err != nil {
				return nil, nil, err
			}
			loader := tableRef.LoaderFrom(loadSource)
			loader.CreateDisposition = gcbq.CreateNever
			loader.WriteDisposition = gcbq.WriteAppend
			return loader, cleanup, nil
		})
		if err != nil {
			return -1, fmt.Errorf("failed to start load job for chunk %d: %w", chunk.index+1, err)
		}

		status, err := d.waitForBigQueryJob(ctx, job)
		if err != nil {
			return -1, fmt.Errorf("load job failed for chunk %d (job %s): %w", chunk.index+1, jobRef(job), err)
		}
		if err := status.Err(); err != nil {
			if attempt < maxAttempts && isRetryableLoadJobError(err) {
				backoff := time.Duration(attempt) * time.Second
				config.Debug("[DEST] Retrying load job for chunk %d after job error: %v", chunk.index+1, err)
				if err := sleepWithContextForLoadJob(ctx, backoff); err != nil {
					return -1, err
				}
				continue
			}
			if details := loadJobErrorDetails(status); details != "" {
				return -1, fmt.Errorf("load job error for chunk %d (job %s): %w; details: %s", chunk.index+1, jobRef(job), err, details)
			}
			return -1, fmt.Errorf("load job error for chunk %d (job %s): %w", chunk.index+1, jobRef(job), err)
		}

		config.Debug("[DEST] Load job finished for chunk %d into %s.%s", chunk.index+1, dataset, table)
		return loadJobOutputRows(status), nil
	}

	return -1, fmt.Errorf("load job error for chunk %d: exhausted retries", chunk.index+1)
}

type loadJobFactory func() (*gcbq.Loader, func(), error)

func (d *BigQueryDestination) startLoadJobWithRetry(ctx context.Context, jobID string, tableRef *gcbq.Table, factory loadJobFactory) (*gcbq.Job, error) {
	if err := d.beginCDCJob(ctx, jobID); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		loader, cleanup, err := factory()
		if err != nil {
			_ = d.resolveCDCJob(context.Background(), jobID)
			return nil, err
		}
		loader.JobID = jobID
		loader.ProjectID = d.projectID
		if d.location != "" {
			loader.Location = d.location
		}
		job, err := loader.Run(ctx)
		cleanup()
		if err == nil {
			return job, nil
		}
		if ctx.Err() != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
		if isBigQueryDuplicateJobError(err) {
			job, recoverErr := d.recoverDuplicateLoadJob(ctx, jobID, tableRef)
			if recoverErr == nil {
				return job, nil
			}
			err = recoverErr
		}
		if !isRetryableLoadJobError(err) && !isNotFoundError(err) {
			_ = d.resolveCDCJob(context.Background(), jobID)
			return nil, err
		}
		config.Debug("[DEST] Retrying ambiguous load job start with stable job ID %s: %v", jobID, err)
		if err := sleepWithContextForLoadJob(ctx, loadJobStartRetryDelay(min(attempt, loadJobMaxAttempts))); err != nil {
			return d.reconcileAmbiguousBigQueryJob(ctx, jobID)
		}
	}
}

func (d *BigQueryDestination) recoverDuplicateLoadJob(ctx context.Context, jobID string, tableRef *gcbq.Table) (*gcbq.Job, error) {
	job, err := d.client.JobFromProject(ctx, d.projectID, jobID, d.location)
	if err != nil {
		return nil, err
	}
	cfg, err := job.Config()
	if err != nil {
		return nil, err
	}
	loadCfg, ok := cfg.(*gcbq.LoadConfig)
	if !ok || loadCfg.Dst == nil {
		return nil, fmt.Errorf("existing job %s is %T, not a load job", jobID, cfg)
	}
	if loadCfg.Dst.ProjectID != tableRef.ProjectID || loadCfg.Dst.DatasetID != tableRef.DatasetID || loadCfg.Dst.TableID != tableRef.TableID {
		return nil, fmt.Errorf("existing load job %s targets %s.%s.%s, expected %s.%s.%s", jobID,
			loadCfg.Dst.ProjectID, loadCfg.Dst.DatasetID, loadCfg.Dst.TableID,
			tableRef.ProjectID, tableRef.DatasetID, tableRef.TableID)
	}
	return job, nil
}

func newBigQueryLoadJobID() string {
	return "ingestr_load_" + strings.TrimPrefix(newBigQueryQueryJobID(), "ingestr_")
}

func loadJobAttemptID(base string, attempt int) string {
	return fmt.Sprintf("%s_%d", base, attempt)
}

// loadJobErrorDetails formats the per-row/per-field errors that BigQuery records
// in JobStatus.Errors. JobStatus.Err() only returns the top-level summary (e.g.
// "JSON table encountered too many errors"); the actual cause (the field or row
// that failed) lives in the Errors slice the summary tells you to inspect.
func loadJobErrorDetails(status *gcbq.JobStatus) string {
	if status == nil || len(status.Errors) == 0 {
		return ""
	}
	const maxDetails = 5
	parts := make([]string, 0, min(len(status.Errors), maxDetails))
	for _, e := range status.Errors {
		if e == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("[reason=%s location=%q] %s", e.Reason, e.Location, e.Message))
		if len(parts) >= maxDetails {
			if remaining := len(status.Errors) - maxDetails; remaining > 0 {
				parts = append(parts, fmt.Sprintf("... and %d more error(s)", remaining))
			}
			break
		}
	}
	return strings.Join(parts, "; ")
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
	ignoreUnknownValues bool,
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
	src.IgnoreUnknownValues = ignoreUnknownValues
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
	_, rowsWritten, err := d.writeLoadJobChunks(ctx, records, format, 0, nil, func(_ int) (stagedLoadChunk, io.WriteCloser, error) {
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

// chunkStager writes record batches into a sequence of load-file chunks,
// rotating files every maxRowsPerFile rows. It is push-based so that both the
// channel-consuming write path and the extract-time pre-staging path share
// the same chunking logic.
type chunkStager struct {
	format         loadJobFileFormat
	maxRowsPerFile int64
	targetSchema   *arrow.Schema
	rowOpts        jsonlRowOptions
	openWriter     func(part int) (stagedLoadChunk, io.WriteCloser, error)

	chunks      []stagedLoadChunk
	totalRows   int64
	currentRows int64
	part        int
	chunkMeta   stagedLoadChunk
	chunkWriter loadJobChunkWriter
}

func (s *chunkStager) closeChunk() error {
	if s.chunkWriter == nil {
		return nil
	}

	chunkWriter := s.chunkWriter
	s.chunks = append(s.chunks, s.chunkMeta)
	s.chunkWriter = nil
	s.currentRows = 0
	s.part++

	if err := chunkWriter.Close(); err != nil {
		return err
	}
	return nil
}

func (s *chunkStager) abort(cause error) {
	if s.chunkWriter == nil {
		return
	}
	s.chunks = append(s.chunks, s.chunkMeta)
	s.chunkWriter.Abort(cause)
	s.chunkWriter = nil
}

func (s *chunkStager) openChunk() error {
	meta, writer, err := s.openWriter(s.part)
	if err != nil {
		return err
	}
	s.chunkMeta = meta
	s.chunkMeta.index = s.part
	s.chunkWriter = newLoadJobChunkWriter(s.format, writer, s.targetSchema, s.rowOpts)
	s.currentRows = 0
	return nil
}

// writeRecord appends a record batch, rotating chunk files as needed.
// The caller retains ownership of the record.
func (s *chunkStager) writeRecord(ctx context.Context, record arrow.RecordBatch) error {
	for start := int64(0); start < record.NumRows(); {
		if err := ctx.Err(); err != nil {
			s.abort(err)
			return err
		}

		if s.chunkWriter == nil {
			if err := s.openChunk(); err != nil {
				return fmt.Errorf("failed to open %s staging writer: %w", s.format, err)
			}
		}

		end := record.NumRows()
		if s.maxRowsPerFile > 0 {
			remainingCapacity := s.maxRowsPerFile - s.currentRows
			if remainingCapacity <= 0 {
				if err := s.closeChunk(); err != nil {
					return fmt.Errorf("failed to finalize %s staging writer: %w", s.format, err)
				}
				continue
			}
			if span := start + remainingCapacity; span < end {
				end = span
			}
		}

		slice := record.NewSlice(start, end)
		err := s.chunkWriter.WriteRecord(slice)
		slice.Release()
		if err != nil {
			s.abort(err)
			return fmt.Errorf("failed to write %s staging batch: %w", s.format, err)
		}

		rowsWritten := end - start
		s.totalRows += rowsWritten
		s.currentRows += rowsWritten
		s.chunkMeta.rows += rowsWritten
		start = end

		if s.maxRowsPerFile > 0 && s.currentRows >= s.maxRowsPerFile {
			if err := s.closeChunk(); err != nil {
				return fmt.Errorf("failed to finalize %s staging writer: %w", s.format, err)
			}
		}
	}

	return nil
}

func (s *chunkStager) finish() ([]stagedLoadChunk, int64, error) {
	if s.chunkWriter == nil {
		return s.chunks, s.totalRows, nil
	}
	if err := s.closeChunk(); err != nil {
		return s.chunks, s.totalRows, fmt.Errorf("failed to finalize %s staging writer: %w", s.format, err)
	}
	return s.chunks, s.totalRows, nil
}

func (d *BigQueryDestination) writeLoadJobChunks(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	format loadJobFileFormat,
	maxRowsPerFile int64,
	targetSchema *arrow.Schema,
	openWriter func(part int) (stagedLoadChunk, io.WriteCloser, error),
) ([]stagedLoadChunk, int64, error) {
	stager := &chunkStager{
		format:         format,
		maxRowsPerFile: maxRowsPerFile,
		targetSchema:   targetSchema,
		openWriter:     openWriter,
	}

	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			stager.abort(result.Err)
			return stager.chunks, stager.totalRows, result.Err
		}

		record := result.Batch
		if record == nil {
			continue
		}

		err := stager.writeRecord(ctx, record)
		record.Release()
		if err != nil {
			return stager.chunks, stager.totalRows, err
		}
	}

	return stager.finish()
}

// jsonlRowOptions customizes JSONL row serialization for extract-time
// pre-staging: keys are renamed to the destination column names assumed at
// extract time and the load timestamp column is injected into every row.
type jsonlRowOptions struct {
	keyTransform        func(string) string
	loadTimestampColumn string
	loadTimestampValue  string
}

func newLoadJobChunkWriter(format loadJobFileFormat, writer io.WriteCloser, targetSchema *arrow.Schema, rowOpts jsonlRowOptions) loadJobChunkWriter {
	switch format {
	case loadJobFormatJSONL:
		return &jsonlChunkWriter{
			stageWriter: writer,
			bufferedW:   bufio.NewWriterSize(writer, stagedGCSBufferSize),
			rowOpts:     rowOpts,
		}
	default:
		return &parquetChunkWriter{
			stageWriter:  writer,
			targetSchema: targetSchema,
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
	rowOpts     jsonlRowOptions
}

func (w *jsonlChunkWriter) WriteRecord(record arrow.RecordBatch) error {
	_, err := writeRecordBatchAsJSONLWithOpts(w.bufferedW, record, w.rowOpts)
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
	stageWriter  io.WriteCloser
	parquetW     *pqarrow.FileWriter
	arrowSchema  *arrow.Schema
	targetSchema *arrow.Schema
	writerProps  *parquet.WriterProperties
	arrowProps   pqarrow.ArrowWriterProperties
}

func (w *parquetChunkWriter) WriteRecord(record arrow.RecordBatch) error {
	rec := record
	if w.targetSchema != nil && !record.Schema().Equal(w.targetSchema) {
		casted, err := databuffer.CastRecordToSchema(record, w.targetSchema, true)
		if err != nil {
			w.Abort(err)
			return fmt.Errorf("failed to cast batch to staging schema: %w", err)
		}
		rec = casted
		defer rec.Release()
	}

	if w.parquetW == nil {
		arrowSchema := stripSchemaMetadata(rec.Schema())
		parquetW, err := newParquetFileWriter(arrowSchema, w.stageWriter, w.writerProps, w.arrowProps)
		if err != nil {
			w.Abort(err)
			return err
		}
		w.arrowSchema = arrowSchema
		w.parquetW = parquetW
	}

	recordToWrite := rec
	shouldRelease := false
	if w.arrowSchema != nil && !rec.Schema().Equal(w.arrowSchema) && schemaEqualIgnoringMetadata(rec.Schema(), w.arrowSchema) {
		normalized, err := normalizeRecordToSchema(rec, w.arrowSchema)
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

func writeRecordBatchAsJSONLWithOpts(writer *bufio.Writer, record arrow.RecordBatch, opts jsonlRowOptions) (int64, error) {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)

	numRows := record.NumRows()
	numCols := int(record.NumCols())
	arrowSchema := record.Schema()

	keys := make([]string, numCols)
	for colIdx := 0; colIdx < numCols; colIdx++ {
		name := arrowSchema.Field(colIdx).Name
		if opts.keyTransform != nil {
			name = opts.keyTransform(name)
		}
		keys[colIdx] = name
	}

	for rowIdx := int64(0); rowIdx < numRows; rowIdx++ {
		row := make(map[string]any, numCols+1)
		for colIdx := 0; colIdx < numCols; colIdx++ {
			row[keys[colIdx]] = extractJSONLValue(record.Column(colIdx), int(rowIdx))
		}
		if opts.loadTimestampColumn != "" {
			row[opts.loadTimestampColumn] = opts.loadTimestampValue
		}

		if err := encoder.Encode(row); err != nil {
			return rowIdx, err
		}
	}

	return numRows, nil
}

func extractJSONLValue(arr arrow.Array, idx int) any {
	if arr.IsNull(idx) {
		// BigQuery REPEATED fields cannot be null: a JSON null is rejected with
		// "Repeated field must be imported as a JSON array". A null array column
		// must therefore serialize as an empty array, not null.
		switch arr.(type) {
		case *array.List, *array.LargeList, *array.FixedSizeList:
			return []any{}
		}
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
