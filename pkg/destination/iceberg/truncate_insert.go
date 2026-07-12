package iceberg

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/array"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *Destination) TruncateInsertRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	input := newRecordBatchInput(records)
	defer input.Close()

	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	tbl, err := d.loadIcebergTable(ctx, opts.Table)
	if err != nil {
		return err
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	var baseSnapshotID int64
	if snapshot := tbl.CurrentSnapshot(); snapshot != nil {
		baseSnapshotID = snapshot.SnapshotID
	}
	metadata := newCommitMetadata(opts.CommitToken, opts.CDCResumeLSN).withExpectedIncarnation(opts.CDCExpectedIncarnation)
	if opts.SkipCDCResume {
		metadata.cdcResumeLSN = ""
		metadata.resetCDCResume = true
	}
	callerToken := metadata.token != ""
	if metadata.token != "" {
		if tableHasCommitToken(tbl, metadata.token) {
			if err := input.Drain(ctx); err != nil {
				return fmt.Errorf("iceberg: failed to drain already-committed truncate+insert for table %s: %w", opts.Table, err)
			}
			if opts.SkipCDCResume {
				return d.ensureManagedCDCResumeResetExpected(ctx, tbl, metadata.token, opts.CDCExpectedIncarnation)
			}
			return nil
		}
		if err := validateCDCResumeAdvance(tbl, metadata); err != nil {
			return err
		}
	}
	if opts.Schema == nil {
		return errors.New("iceberg destination requires schema for truncate+insert")
	}
	if err := validateIcebergTableSchema(opts.Schema); err != nil {
		return err
	}
	desiredSchema := destination.DestinationTableSchema(opts.Schema)
	if err := validateIcebergTableSchema(desiredSchema); err != nil {
		return err
	}
	if opts.DeduplicatePrimaryKeys && len(opts.PrimaryKeys) == 0 {
		return errors.New("iceberg: truncate+insert primary-key deduplication requires at least one primary key")
	}

	clusterBy, physicallySortable := identitySortColumns(tbl)
	if !physicallySortable {
		clusterBy = nil
	}
	inputIcebergSchema, err := icebergSchemaFromTableSchema(opts.Schema)
	if err != nil {
		return err
	}
	arrowSchema := icebergWriteArrowSchema(opts.Schema, inputIcebergSchema)
	batchReader := input.RecordReader(ctx, arrowSchema)
	var reader array.RecordReader = batchReader
	var cleanupTransforms []func()
	defer func() {
		for i := len(cleanupTransforms) - 1; i >= 0; i-- {
			cleanupTransforms[i]()
		}
	}()
	if opts.DeduplicatePrimaryKeys && len(opts.PrimaryKeys) > 0 {
		reader, cleanupTransforms, err = addAtomicTruncateDedup(reader, cleanupTransforms, opts)
		if err != nil {
			return fmt.Errorf(
				"iceberg: failed to deduplicate truncate+insert for table %s: %w",
				opts.Table,
				err,
			)
		}
	}
	desiredIcebergSchema, err := icebergSchemaFromTableSchema(desiredSchema)
	if err != nil {
		return err
	}
	targetArrowSchema := icebergWriteArrowSchema(desiredSchema, desiredIcebergSchema)
	if !reader.Schema().Equal(targetArrowSchema) {
		projected, cleanup := projectRecordReader(reader, targetArrowSchema)
		reader = projected
		cleanupTransforms = append(cleanupTransforms, cleanup)
	}
	if len(clusterBy) > 0 {
		clustered, cleanup, clusterErr := clusterRecordReader(ctx, reader, clusterBy)
		if clusterErr != nil {
			return fmt.Errorf(
				"iceberg: failed to cluster truncate+insert for table %s: %w",
				opts.Table,
				clusterErr,
			)
		}
		reader = clustered
		cleanupTransforms = append(cleanupTransforms, cleanup)
	}

	if metadata.token == "" {
		spooled, cleanup, contentID, spoolErr := spoolAppendRecordReader(reader)
		if spoolErr != nil {
			return fmt.Errorf(
				"iceberg: failed to prepare idempotent truncate+insert for table %s: %w",
				opts.Table,
				spoolErr,
			)
		}
		reader = spooled
		cleanupTransforms = append(cleanupTransforms, cleanup)
		metadata = newCommitMetadata("truncate+insert-content:"+contentID, "")
	}
	if err := d.validateExpectedIncarnation(ctx, tbl, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if (callerToken && tableHasCommitToken(tbl, metadata.token)) ||
		(!callerToken && currentSnapshotHasCommitToken(tbl, metadata.token)) {
		if opts.SkipCDCResume {
			return d.ensureManagedCDCResumeResetExpected(ctx, tbl, metadata.token, opts.CDCExpectedIncarnation)
		}
		return nil
	}
	if err := validateCDCResumeAdvance(tbl, metadata); err != nil {
		return err
	}

	props := snapshotProps("truncate+insert", metadata)
	prepared := preparedTable{
		replace:          true,
		preserveMetadata: true,
		evolveSchema:     true,
		schema:           desiredSchema,
		clusterBy:        clusterBy,
	}
	openReplay, cleanupReplay, err := spoolReplayableRecordReader(reader)
	if err != nil {
		return err
	}
	defer cleanupReplay()
	current := tbl
	var committed *icebergtable.Table
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		replay, release, replayErr := openReplay()
		if replayErr != nil {
			return replayErr
		}
		committed, writeErr = d.overwritePrepared(ctx, current, replay, props, prepared, opts.Parallelism, nil, true, opts.CDCExpectedIncarnation)
		release()
		if writeErr == nil {
			break
		}
		if !errors.Is(writeErr, icebergtable.ErrCommitFailed) {
			if reconciled := d.reconcileCommitAfterSnapshot(ctx, opts.Table, metadata.token, baseSnapshotID, opts.CDCExpectedIncarnation, writeErr); reconciled != nil {
				return fmt.Errorf("iceberg: failed to atomically truncate and insert table %s: %w", opts.Table, reconciled)
			}
			writeErr = nil
			break
		}
		current, err = d.loadIcebergTable(ctx, opts.Table)
		if err != nil {
			return err
		}
		if err := d.validateExpectedIncarnation(ctx, current, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	if committed == nil && writeErr != nil {
		return fmt.Errorf("iceberg: failed to atomically truncate and insert table %s after retries: %w", opts.Table, writeErr)
	}
	d.afterSuccessfulCommitExpected(ctx, opts.Table, opts.CDCExpectedIncarnation)
	return nil
}

func addAtomicTruncateDedup(
	reader array.RecordReader,
	cleanups []func(),
	opts destination.WriteOptions,
) (array.RecordReader, []func(), error) {
	deduped, cleanup, err := deduplicateRecordReader(reader, opts.PrimaryKeys, opts.IncrementalKey)
	if err != nil {
		return reader, cleanups, err
	}
	return deduped, append(cleanups, cleanup), nil
}

var _ destination.AtomicTruncateInsertWriter = (*Destination)(nil)

func (d *Destination) EvolvesTruncateInsertSchemaAtomically() bool { return true }

var _ destination.AtomicTruncateInsertSchemaEvolver = (*Destination)(nil)
