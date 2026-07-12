package iceberg

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// MergeRecords merges incoming batches directly into the target snapshot,
// avoiding a remote Iceberg staging-table write and scan.
func (d *Destination) MergeRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	writeOpts destination.WriteOptions,
	mergeOpts destination.MergeOptions,
) error {
	input := newRecordBatchInput(records)
	defer input.Close()

	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if writeOpts.Schema == nil {
		return errors.New("iceberg: direct merge requires schema")
	}
	if len(mergeOpts.PrimaryKeys) == 0 {
		return errors.New("iceberg: direct merge requires at least one primary key")
	}
	target, err := d.loadIcebergTable(ctx, mergeOpts.TargetTable)
	if err != nil {
		return err
	}
	if mergeOpts.CDCExpectedIncarnation == "" {
		mergeOpts.CDCExpectedIncarnation = writeOpts.CDCExpectedIncarnation
	} else if writeOpts.CDCExpectedIncarnation != "" && mergeOpts.CDCExpectedIncarnation != writeOpts.CDCExpectedIncarnation {
		return errors.New("iceberg: direct merge received conflicting expected target incarnations")
	}
	if err := d.validateExpectedIncarnation(ctx, target, mergeOpts.CDCExpectedIncarnation); err != nil {
		return err
	}
	metadata := mergeCommitMetadata(mergeOpts)
	if mergeOpts.Parallelism <= 0 {
		mergeOpts.Parallelism = writeOpts.Parallelism
	}
	if tableHasCommitToken(target, metadata.token) {
		if err := input.Drain(ctx); err != nil {
			return err
		}
		if mergeOpts.SkipCDCResume {
			return d.ensureManagedCDCResumeResetExpected(ctx, target, metadata.token, mergeOpts.CDCExpectedIncarnation)
		}
		return nil
	}
	if !mergeOpts.SkipCDCResume {
		if err := validateCDCResumeAdvance(target, metadata); err != nil {
			return err
		}
	}

	stagingSchema := icebergArrowSchema(writeOpts.Schema)
	stagingIceSchema, err := icebergSchemaFromTableSchema(writeOpts.Schema)
	if err != nil {
		return err
	}
	isCDC := icebergSchemaHasFieldFold(stagingIceSchema, destination.CDCDeletedColumn)
	rowDelta := false
	if !isCDC && stagingCoversTarget(target.Schema(), stagingIceSchema) {
		rowDelta, err = useRowDeltaMerge(ctx, target, mergeOpts.PrimaryKeys)
		if err != nil {
			return err
		}
	}
	if target.Metadata().Version() >= 3 {
		rowDelta = false
	}
	if rowDelta {
		sorter, err := newSpillSorter(stagingSchema, mergeOpts.PrimaryKeys)
		if err != nil {
			return err
		}
		defer sorter.Close()
		if _, err := consumeMergeRecords(ctx, input, stagingSchema, func(row []any) error { return sorter.AddContext(ctx, row) }); err != nil {
			return err
		}
		return d.mergeRowDeltaFromSorter(ctx, target, stagingSchema, sorter, mergeOpts, metadata)
	}

	rows := tableRowsMetadata(stagingSchema)
	sorter, err := newSpillSorter(stagingSchema, mergeOpts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer sorter.Close()
	var staleEventErr error
	maxLSN, err := consumeMergeRecords(ctx, input, stagingSchema, func(row []any) error {
		lsn, _ := asString(rows.ValueFold(row, destination.CDCLSNColumn))
		if !mergeOpts.SkipCDCResume {
			if err := validateCDCEventPosition(target, lsn); err != nil && staleEventErr == nil {
				staleEventErr = err
			}
		}
		return sorter.AddContext(ctx, row)
	})
	if err != nil {
		return err
	}
	if !mergeOpts.SkipCDCResume {
		metadata = metadata.withCDCResumeLSN(maxLSN)
	}
	if metadata.token == "" && sorter.Len() > 0 {
		contentID, err := copyOnWriteContentIdentity(ctx, sorter, rows, mergeOpts)
		if err != nil {
			return err
		}
		metadata.token = commitTokenID("merge-content:" + contentID)
	}
	if err := d.validateExpectedIncarnation(ctx, target, mergeOpts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if tableHasCommitToken(target, metadata.token) {
		if mergeOpts.SkipCDCResume {
			return d.ensureManagedCDCResumeResetExpected(ctx, target, metadata.token, mergeOpts.CDCExpectedIncarnation)
		}
		return nil
	}
	if staleEventErr != nil {
		return staleEventErr
	}
	// The resume position can be derived from the records rather than supplied
	// by the caller, so validate it again after consumption. Equal positions are
	// valid at a snapshot/WAL boundary; true regressions still fail.
	if !mergeOpts.SkipCDCResume {
		if err := validateCDCResumeAdvance(target, metadata); err != nil {
			return err
		}
	}
	return d.mergeCopyOnWriteSorted(ctx, target, rows, sorter, mergeOpts, metadata)
}

func consumeMergeRecords(
	ctx context.Context,
	input *recordBatchInput,
	sc *arrow.Schema,
	consume func([]any) error,
) (string, error) {
	reader := input.RecordReader(ctx, sc)
	defer input.ReleaseReader()

	var maxLSN string
	for reader.Next() {
		batch := reader.RecordBatch()
		if lsn := maxCDCResumeLSNInBatch(batch); compareCDCResumeLSN(lsn, maxLSN) > 0 {
			maxLSN = lsn
		}
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			row := make([]any, int(batch.NumCols()))
			for columnIdx := range row {
				value, err := rowValue(batch.Column(columnIdx), rowIdx)
				if err != nil {
					return "", fmt.Errorf("iceberg: direct merge column %q: %w", sc.Field(columnIdx).Name, err)
				}
				row[columnIdx] = value
			}
			if err := consume(row); err != nil {
				return "", err
			}
		}
	}
	if err := reader.Err(); err != nil {
		return "", fmt.Errorf("iceberg: failed to read direct merge records: %w", err)
	}
	if compareCDCResumeLSN(reader.DurableCommitPosition(), maxLSN) > 0 {
		maxLSN = reader.DurableCommitPosition()
	}
	return maxLSN, nil
}

var _ destination.DirectMergeWriter = (*Destination)(nil)
