package iceberg

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/google/uuid"
)

func (d *Destination) appendRecordBatches(
	ctx context.Context,
	tbl *icebergtable.Table,
	reader array.RecordReader,
	props iceberggo.Properties,
	parallelism int,
	expectedIncarnation string,
) (retErr error) {
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return err
	}
	writeID := uuid.New()
	generatedPaths := make([]string, 0)
	committed, cleanupSafe := false, true
	defer func() {
		if committed || !cleanupSafe {
			return
		}
		if err := removeGeneratedMergeFiles(tableFS, tbl.Location(), writeID.String(), generatedPaths); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	writeOpts := make([]icebergtable.WriteRecordOption, 0, 2)
	writeOpts = append(writeOpts, icebergtable.WithWriteUUID(writeID))
	if parallelism > 0 {
		writeOpts = append(writeOpts, icebergtable.WithMaxWriteWorkers(parallelism))
	}

	files := make([]iceberggo.DataFile, 0)
	for dataFile, err := range icebergtable.WriteRecords(ctx, tbl, reader.Schema(), retainedRecordIterator(reader), writeOpts...) {
		if err != nil {
			return err
		}
		files = append(files, dataFile)
		generatedPaths = append(generatedPaths, dataFile.FilePath())
	}
	if err := reader.Err(); err != nil {
		return err
	}
	if len(files) == 0 {
		empty, err := array.NewRecordReader(reader.Schema(), nil)
		if err != nil {
			return err
		}
		defer empty.Release()
		if props[snapshotCommitTokenKey] == "" {
			if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
				return err
			}
			_, err = tbl.Append(ctx, empty, props)
			return err
		}
		_, err = d.commitTokenizedAppend(ctx, tbl, props, expectedIncarnation, func(txn *icebergtable.Transaction) error {
			return txn.Append(ctx, empty, props)
		})
		return err
	}

	cleanupSafe, err = d.commitAppendDataFiles(ctx, tbl, files, props, expectedIncarnation)
	committed = err == nil && !cleanupSafe
	return err
}

func (d *Destination) commitAppendDataFiles(
	ctx context.Context,
	tbl *icebergtable.Table,
	files []iceberggo.DataFile,
	props iceberggo.Properties,
	expectedIncarnation string,
) (cleanupSafe bool, retErr error) {
	const maxAttempts = 5
	token := props[snapshotCommitTokenKey]
	current := tbl
	var commitErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if token != "" && tableHasCommitToken(current, token) {
			if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
				return true, err
			}
			return true, nil
		}
		if err := validateCDCResumeAdvance(current, commitMetadata{
			token: token, cdcResumeLSN: props[snapshotCDCResumeLSNKey],
		}); err != nil {
			return true, err
		}

		attemptFiles := files
		if current.Metadata().Version() >= 3 {
			attemptFiles = make([]iceberggo.DataFile, len(files))
			nextRowID := current.Metadata().NextRowID()
			for i, file := range files {
				var err error
				attemptFiles[i], err = withDataFileFirstRowID(file, current, nextRowID)
				if err != nil {
					return true, err
				}
				nextRowID += file.Count()
			}
		}

		txn := current.NewTransaction()
		if token != "" {
			if err := stageCommitTokenLedger(txn, current, token); err != nil {
				return true, err
			}
		}
		if err := stageCDCResumeState(txn, props); err != nil {
			return true, err
		}
		if err := txn.AddDataFiles(ctx, attemptFiles, props, icebergtable.WithoutDuplicateCheck()); err != nil {
			return true, err
		}
		commit, err := txn.TableCommit()
		if err != nil {
			return true, err
		}
		if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
			return true, err
		}
		_, _, commitErr = d.catalog.CommitTable(ctx, commit.Identifier, commit.Requirements, commit.Updates)
		if commitErr == nil {
			txn.MarkCommitted()
			return false, nil
		}
		if !errors.Is(commitErr, icebergtable.ErrCommitFailed) {
			return false, commitErr
		}

		current, err = d.catalog.LoadTable(ctx, tbl.Identifier())
		if err != nil {
			return false, errors.Join(commitErr, err)
		}
		if token != "" && tableHasCommitToken(current, token) {
			if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
				return true, err
			}
			return true, nil
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return true, err
		}
	}
	return true, commitErr
}

func (d *Destination) commitTokenizedAppend(
	ctx context.Context,
	tbl *icebergtable.Table,
	props iceberggo.Properties,
	expectedIncarnation string,
	stage func(*icebergtable.Transaction) error,
) (cleanupSafe bool, retErr error) {
	const maxAttempts = 5
	token := props[snapshotCommitTokenKey]
	current := tbl
	var commitErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if tableHasCommitToken(current, token) {
			if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
				return true, err
			}
			return true, nil
		}
		if err := validateCDCResumeAdvance(current, commitMetadata{
			token: token, cdcResumeLSN: props[snapshotCDCResumeLSNKey],
		}); err != nil {
			return true, err
		}
		txn := current.NewTransaction()
		if err := stageCommitTokenLedger(txn, current, token); err != nil {
			return true, err
		}
		if err := stageCDCResumeState(txn, props); err != nil {
			return true, err
		}
		if err := stage(txn); err != nil {
			return true, err
		}
		commit, err := txn.TableCommit()
		if err != nil {
			return true, err
		}
		if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
			return true, err
		}
		_, _, commitErr = d.catalog.CommitTable(ctx, commit.Identifier, commit.Requirements, commit.Updates)
		if commitErr == nil {
			txn.MarkCommitted()
			return false, nil
		}
		if !errors.Is(commitErr, icebergtable.ErrCommitFailed) {
			return false, commitErr
		}

		current, err = d.catalog.LoadTable(ctx, tbl.Identifier())
		if err != nil {
			return false, errors.Join(commitErr, err)
		}
		if tableHasCommitToken(current, token) {
			if err := d.validateExpectedIncarnation(ctx, current, expectedIncarnation); err != nil {
				return true, err
			}
			return true, nil
		}
		wait := min(10*time.Millisecond<<attempt, 250*time.Millisecond)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return true, ctx.Err()
		case <-timer.C:
		}
	}
	return true, commitErr
}

func retainedRecordIterator(reader array.RecordReader) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for reader.Next() {
			batch := reader.RecordBatch()
			batch.Retain()
			if !yield(batch, nil) {
				return
			}
		}
		if err := reader.Err(); err != nil {
			yield(nil, err)
		}
	}
}

func waitForCommitRetry(ctx context.Context, attempt int) error {
	wait := min(10*time.Millisecond<<attempt, 250*time.Millisecond)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
