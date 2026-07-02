package bigquery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// NewPreStageWriter implements destination.PreStager. It stages JSONL load
// files while the source extract is still running so the write phase can skip
// the buffer-replay pipeline entirely and only run load jobs.
func (d *BigQueryDestination) NewPreStageWriter(ctx context.Context, opts destination.PreStageOptions) (destination.PreStageWriter, error) {
	if d.effectiveLoadMethod() != loadMethodLoadJob {
		return nil, destination.ErrPreStageUnsupported
	}
	// Pre-staged files are always JSONL: unlike Parquet, JSONL does not embed a
	// schema, so files written before inference finishes still load correctly.
	// An explicit parquet loader format is honored by not pre-staging.
	if strings.EqualFold(strings.TrimSpace(opts.LoaderFileFormat), string(loadJobFormatParquet)) {
		return nil, destination.ErrPreStageUnsupported
	}
	if _, err := resolveLoadJobFileFormat(opts.LoaderFileFormat); err != nil {
		return nil, destination.ErrPreStageUnsupported
	}

	rowOpts := jsonlRowOptions{keyTransform: opts.KeyTransform}
	if opts.LoadTimestampColumn != "" {
		rowOpts.loadTimestampColumn = opts.LoadTimestampColumn
		rowOpts.loadTimestampValue = opts.LoadTimestamp.UTC().Format(time.RFC3339Nano)
	}

	maxRowsPerFile := opts.LoaderFileSize
	if !opts.StagingTable && opts.StagingBucket == "" {
		maxRowsPerFile = 0
	}

	writer := &preStageWriter{dest: d}

	if opts.StagingBucket != "" {
		bucket, prefix, err := parseGCSBucketURI(opts.StagingBucket)
		if err != nil {
			return nil, err
		}
		if err := d.ensureGCSClient(ctx); err != nil {
			return nil, err
		}
		objectPrefix := buildGCSLoadObjectPrefix(prefix, opts.Table)
		writer.stager = &chunkStager{
			format:         loadJobFormatJSONL,
			maxRowsPerFile: resolveLoadJobRowsPerFile(maxRowsPerFile),
			rowOpts:        rowOpts,
			openWriter: func(part int) (stagedLoadChunk, io.WriteCloser, error) {
				objectName := buildGCSLoadObjectName(objectPrefix, loadJobFormatJSONL, part)
				w := d.gcsClient.Bucket(bucket).Object(objectName).NewWriter(ctx)
				w.ChunkSize = stagedGCSObjectChunkSize
				attrs := buildStagingGCSObjectAttrs(loadJobFormatJSONL)
				w.ContentType = attrs.ContentType
				w.CacheControl = attrs.CacheControl
				w.CustomTime = attrs.CustomTime
				w.Metadata = attrs.Metadata
				return stagedLoadChunk{
					index:     part,
					gcsBucket: bucket,
					gcsObject: objectName,
					gcsURI:    "gs://" + bucket + "/" + objectName,
				}, newBufferedWriteCloser(w), nil
			},
		}
		return writer, nil
	}

	tempDir, err := os.MkdirTemp("", "ingestr-bq-prestage-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for pre-staged load files: %w", err)
	}
	writer.tempDir = tempDir
	writer.stager = &chunkStager{
		format:         loadJobFormatJSONL,
		maxRowsPerFile: resolveLoadJobRowsPerFile(maxRowsPerFile),
		rowOpts:        rowOpts,
		openWriter: func(part int) (stagedLoadChunk, io.WriteCloser, error) {
			path := buildLocalLoadFilePath(tempDir, opts.Table, loadJobFormatJSONL, part)
			f, err := os.Create(path)
			if err != nil {
				return stagedLoadChunk{}, nil, err
			}
			return stagedLoadChunk{index: part, localPath: path}, f, nil
		},
	}
	return writer, nil
}

type preStageWriter struct {
	dest    *BigQueryDestination
	stager  *chunkStager
	tempDir string
}

func (w *preStageWriter) Append(ctx context.Context, batch arrow.RecordBatch) error {
	if batch == nil {
		return nil
	}
	return w.stager.writeRecord(ctx, batch)
}

func (w *preStageWriter) stagedSet() *stagedLoadSet {
	return &stagedLoadSet{
		tempDir:             w.tempDir,
		format:              loadJobFormatJSONL,
		chunks:              w.stager.chunks,
		ignoreUnknownValues: true,
	}
}

func (w *preStageWriter) Finish() (destination.PreStagedData, error) {
	chunks, rows, err := w.stager.finish()
	if err != nil {
		w.Discard()
		return nil, err
	}
	staged := w.stagedSet()
	staged.chunks = chunks
	if rows == 0 {
		staged.cleanupLocal()
		staged.cleanupRemote(context.Background(), w.dest.gcsClient)
		return nil, nil
	}
	return &preStagedLoadSet{dest: w.dest, staged: staged, rows: rows}, nil
}

func (w *preStageWriter) Discard() {
	w.stager.abort(errors.New("pre-staging discarded"))
	staged := w.stagedSet()
	staged.cleanupLocal()
	staged.cleanupRemote(context.Background(), w.dest.gcsClient)
}

// preStagedLoadSet implements destination.PreStagedData for BigQuery.
type preStagedLoadSet struct {
	dest   *BigQueryDestination
	staged *stagedLoadSet
	rows   int64
}

func (p *preStagedLoadSet) RowCount() int64 {
	return p.rows
}

func (p *preStagedLoadSet) Close() {
	p.staged.cleanupLocal()
	p.staged.cleanupRemote(context.Background(), p.dest.gcsClient)
}

// writePreStaged loads pre-staged JSONL files instead of consuming the record
// stream. The records channel is drained defensively: it must be empty, and
// receiving an actual batch indicates a wiring bug that would otherwise lose
// data silently.
func (d *BigQueryDestination) writePreStaged(
	ctx context.Context,
	project string,
	dataset string,
	table string,
	records <-chan source.RecordBatchResult,
	ps *preStagedLoadSet,
	opts destination.WriteOptions,
) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
			return errors.New("pre-staged write received unexpected record batches from the source")
		}
		if result.Err != nil {
			return result.Err
		}
	}

	staged := ps.staged
	config.Debug("[DEST] Loading %d pre-staged %s chunk(s) (%d rows) into %s.%s", len(staged.chunks), staged.format, ps.rows, dataset, table)
	logStagedLoadSet(dataset, table, staged)

	parallelism := d.resolveLoadJobParallelism(dataset, table, staged, opts)
	outputRows, err := d.runLoadJobs(ctx, project, dataset, table, staged, parallelism)
	if err != nil {
		return err
	}
	if outputRows >= 0 && outputRows != ps.rows {
		return fmt.Errorf("pre-staged load mismatch: BigQuery loaded %d rows but %d rows were staged", outputRows, ps.rows)
	}

	ps.Close()
	config.Debug("[DEST] Pre-staged load completed for %s.%s", dataset, table)
	return nil
}

var _ destination.PreStager = (*BigQueryDestination)(nil)
