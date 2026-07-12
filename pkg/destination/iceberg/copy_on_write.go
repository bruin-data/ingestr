package iceberg

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/google/uuid"
)

func tableRowsMetadata(sc *arrow.Schema) *scannedTable {
	rows := &scannedTable{ColIdx: make(map[string]int, len(sc.Fields()))}
	for i, field := range sc.Fields() {
		rows.Columns = append(rows.Columns, field.Name)
		rows.ColIdx[field.Name] = i
	}
	return rows
}

func (d *Destination) mergeCopyOnWriteSorted(
	ctx context.Context,
	target *icebergtable.Table,
	stagingRows *scannedTable,
	stagingSorter *spillSorter,
	opts destination.MergeOptions,
	metadata commitMetadata,
) error {
	const maxAttempts = 5
	current := target
	var commitErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		commitErr = d.mergeCopyOnWriteSortedAttempt(ctx, current, stagingRows, stagingSorter, opts, metadata)
		if !errors.Is(commitErr, icebergtable.ErrCommitFailed) {
			return commitErr
		}
		var err error
		current, err = d.catalog.LoadTable(ctx, target.Identifier())
		if err != nil {
			return errors.Join(commitErr, err)
		}
		if err := d.validateExpectedIncarnation(ctx, current, metadata.expectedIncarnation); err != nil {
			return err
		}
		if tableHasCommitToken(current, metadata.token) {
			return nil
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return commitErr
}

func (d *Destination) mergeCopyOnWriteSortedAttempt(
	ctx context.Context,
	target *icebergtable.Table,
	stagingRows *scannedTable,
	stagingSorter *spillSorter,
	opts destination.MergeOptions,
	metadata commitMetadata,
) error {
	if err := d.validateExpectedIncarnation(ctx, target, metadata.expectedIncarnation); err != nil {
		return err
	}
	if tableHasCommitToken(target, metadata.token) {
		return nil
	}
	if err := validateCDCResumeAdvance(target, metadata); err != nil {
		return err
	}
	if err := requireColumns(stagingRows, opts.PrimaryKeys, opts.StagingTable); err != nil {
		return err
	}
	if stagingSorter.Len() == 0 {
		return d.commitMetadataOnly(ctx, target, "merge", metadata)
	}

	incrementalIdx := -1
	isCDC := stagingRows.HasColumnFold(destination.CDCDeletedColumn)
	if !isCDC && opts.IncrementalKey != "" {
		idx, err := incrementalKeyIndex(stagingRows.Columns, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return err
		}
		incrementalIdx = idx
	}

	preserveLineage := target.Metadata().Version() >= 3
	targetSchema, err := copyOnWriteTargetSchema(target, preserveLineage)
	if err != nil {
		return err
	}
	targetRows := tableRowsMetadata(targetSchema)
	targetSorter, err := newSpillSorter(targetSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer targetSorter.Close()
	if err := forEachCopyOnWriteTargetRow(ctx, target, preserveLineage, func(row []any) error {
		return targetSorter.AddContext(ctx, row)
	}); err != nil {
		return fmt.Errorf("iceberg: target table %s: %w", opts.TargetTable, err)
	}
	if err := validateUniqueTargetKeys(ctx, targetSorter, opts.TargetTable); err != nil {
		return err
	}

	produce := func(sink func(arrow.RecordBatch) error) error {
		emitter := newBatchEmitter(newRowProjection(targetSchema, targetRows.Columns), sink)
		defer emitter.release()
		if err := mergeSortedCopyOnWriteRows(
			ctx,
			stagingSorter,
			targetSorter,
			stagingRows,
			targetRows,
			incrementalIdx,
			isCDC,
			preserveLineage,
			emitter.add,
		); err != nil {
			return err
		}
		return emitter.flushBatch()
	}

	reader := streamingReaderContext(ctx, targetSchema, produce)
	defer reader.Release()
	clusterBy, sortable := identitySortColumns(target)
	if !sortable {
		clusterBy = nil
	}
	clustered, cleanup, err := clusterRecordReader(ctx, reader, clusterBy)
	if err != nil {
		return fmt.Errorf("iceberg: failed to cluster merge rows for table %s: %w", opts.TargetTable, err)
	}
	defer cleanup()

	config.Debug(
		"[ICEBERG MERGE] Copy-on-write sort/merge of %d staged and %d target row(s) into %s",
		stagingSorter.Len(), targetSorter.Len(), opts.TargetTable,
	)
	return d.replaceAllMergeRecords(ctx, target, clustered, clusterBy, opts, metadata)
}

func mergeSortedCopyOnWriteRows(
	ctx context.Context,
	stagingSorter, targetSorter *spillSorter,
	stagingRows, targetRows *scannedTable,
	incrementalIdx int,
	isCDC bool,
	preserveLineage bool,
	emit func([]any) error,
) error {
	staging, err := stagingSorter.IterContext(ctx)
	if err != nil {
		return err
	}
	defer staging.Close()
	target, err := targetSorter.IterContext(ctx)
	if err != nil {
		return err
	}
	defer target.Close()

	hasStaging := staging.NextGroup()
	hasTarget := target.NextGroup()
	for hasStaging || hasTarget {
		switch {
		case !hasStaging || hasTarget && strings.Compare(target.Key(), staging.Key()) < 0:
			for target.NextRow() {
				if err := emit(target.Row()); err != nil {
					return err
				}
			}
			hasTarget = target.NextGroup()
		case !hasTarget || strings.Compare(staging.Key(), target.Key()) < 0:
			entry, err := mergeEntryFromGroup(staging, stagingRows, incrementalIdx, isCDC)
			if err != nil {
				return err
			}
			row, err := composeCopyOnWriteRow(targetRows.Columns, stagingRows, entry, nil, isCDC, preserveLineage)
			if err != nil {
				return err
			}
			if err := emit(row); err != nil {
				return err
			}
			hasStaging = staging.NextGroup()
		default:
			var existing []any
			for target.NextRow() {
				existing = target.Row()
			}
			entry, err := mergeEntryFromGroup(staging, stagingRows, incrementalIdx, isCDC)
			if err != nil {
				return err
			}
			row, err := composeCopyOnWriteRow(targetRows.Columns, stagingRows, entry, existing, isCDC, preserveLineage)
			if err != nil {
				return err
			}
			if err := emit(row); err != nil {
				return err
			}
			hasStaging = staging.NextGroup()
			hasTarget = target.NextGroup()
		}
	}
	if err := staging.Err(); err != nil {
		return err
	}
	return target.Err()
}

func validateUniqueTargetKeys(ctx context.Context, sorter *spillSorter, table string) error {
	it, err := sorter.IterContext(ctx)
	if err != nil {
		return err
	}
	defer it.Close()
	for it.NextGroup() {
		count := 0
		for it.NextRow() {
			count++
			if count > 1 {
				return fmt.Errorf("iceberg: target table %s contains duplicate primary key %x; copy-on-write merge requires unique target keys", table, it.Key())
			}
		}
	}
	return it.Err()
}

func validateCDCEventPosition(tbl *icebergtable.Table, position string) error {
	position = strings.TrimSpace(position)
	current := latestCDCResumeLSN(tbl)
	if position == "" || current == "" || compareCDCResumeLSN(position, current) >= 0 {
		return nil
	}
	return fmt.Errorf("iceberg: stale CDC resume position in event %q is older than durable position %q", position, current)
}

func copyOnWriteContentIdentity(
	ctx context.Context,
	sorter *spillSorter,
	rows *scannedTable,
	opts destination.MergeOptions,
) (string, error) {
	incrementalIdx := -1
	isCDC := rows.HasColumnFold(destination.CDCDeletedColumn)
	if !isCDC && opts.IncrementalKey != "" {
		idx, err := incrementalKeyIndex(rows.Columns, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return "", err
		}
		incrementalIdx = idx
	}
	it, err := sorter.IterContext(ctx)
	if err != nil {
		return "", err
	}
	defer it.Close()
	hasher := newOrderedRowContentHasher(sorter.schema)
	for it.NextGroup() {
		entry, err := mergeEntryFromGroup(it, rows, incrementalIdx, isCDC)
		if err != nil {
			return "", err
		}
		if isCDC && entry.latestNonDeleted != nil {
			hasher.Add(entry.latestNonDeleted)
		}
		hasher.Add(entry.latest)
	}
	if err := it.Err(); err != nil {
		return "", err
	}
	return hasher.Identity(), nil
}

func mergeEntryFromGroup(it *spillIter, rows *scannedTable, incrementalIdx int, isCDC bool) (*mergeEntry, error) {
	entry := &mergeEntry{}
	for it.NextRow() {
		row := it.Row()
		switch {
		case isCDC:
			lsn, _ := asString(rows.ValueFold(row, destination.CDCLSNColumn))
			deleted, _ := rows.ValueFold(row, destination.CDCDeletedColumn).(bool)
			if entry.latest == nil || destination.CDCSupersedes(lsn, deleted, entry.latestLSN, entry.latestDeleted) {
				entry.latest = row
				entry.latestLSN = lsn
				entry.latestDeleted = deleted
			}
			if !deleted && (entry.latestNonDeleted == nil || destination.CompareCDCPositions(lsn, entry.latestNonDeletedLSN) >= 0) {
				entry.latestNonDeleted = row
				entry.latestNonDeletedLSN = lsn
			}
		case incrementalIdx >= 0:
			if entry.latest == nil {
				entry.latest = row
				entry.incrementalVal = row[incrementalIdx]
				continue
			}
			cmp, comparable := compareOrdered(row[incrementalIdx], entry.incrementalVal)
			if !comparable || cmp >= 0 {
				entry.latest = row
				entry.incrementalVal = row[incrementalIdx]
			}
		default:
			entry.latest = row
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	if entry.latest == nil {
		return nil, fmt.Errorf("iceberg: encountered an empty staging key group")
	}
	return entry, nil
}

func composeCopyOnWriteRow(targetColumns []string, stagingRows *scannedTable, entry *mergeEntry, existing []any, isCDC, preserveLineage bool) ([]any, error) {
	var row []any
	var err error
	if isCDC {
		row, err = composeCDCMergeRow(targetColumns, stagingRows, entry, existing)
	} else {
		row = projectRow(targetColumns, stagingRows, entry.latest, existing)
	}
	if err != nil || row == nil || !preserveLineage {
		return row, err
	}
	setColumn(targetColumns, row, iceberggo.LastUpdatedSequenceNumberColumnName, nil)
	if existing == nil {
		setColumn(targetColumns, row, iceberggo.RowIDColumnName, nil)
	}
	return row, nil
}

func copyOnWriteTargetSchema(tbl *icebergtable.Table, preserveLineage bool) (*arrow.Schema, error) {
	if !preserveLineage {
		return tableWriteSchema(tbl)
	}
	sc, err := icebergtable.SchemaToArrowSchema(iceberggo.SchemaWithRowLineage(tbl.Schema()), nil, true, false)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to build row-lineage merge schema: %w", err)
	}
	return sc, nil
}

func forEachCopyOnWriteTargetRow(ctx context.Context, tbl *icebergtable.Table, preserveLineage bool, fn func([]any) error) error {
	if !preserveLineage {
		return forEachScannedRow(ctx, tbl, iceberggo.AlwaysTrue{}, fn)
	}
	sc, records, err := tbl.Scan(icebergtable.WithRowLineage()).ToArrowRecords(ctx)
	if err != nil {
		return fmt.Errorf("iceberg: failed to scan target row lineage: %w", err)
	}
	for batch, readErr := range records {
		if readErr != nil {
			return readErr
		}
		for rowIdx := 0; rowIdx < int(batch.NumRows()); rowIdx++ {
			row := make([]any, len(sc.Fields()))
			for colIdx := range sc.Fields() {
				row[colIdx], err = rowValue(batch.Column(colIdx), rowIdx)
				if err != nil {
					batch.Release()
					return err
				}
			}
			if err := fn(row); err != nil {
				batch.Release()
				return err
			}
		}
		batch.Release()
	}
	return nil
}

func (d *Destination) replaceAllMergeRecords(
	ctx context.Context,
	tbl *icebergtable.Table,
	reader array.RecordReader,
	clusterBy []string,
	opts destination.MergeOptions,
	metadata commitMetadata,
) (retErr error) {
	dataFilesToDelete, deleteFilesToRemove, err := liveFilesForReplace(ctx, tbl)
	if err != nil {
		return err
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		return fmt.Errorf("iceberg: merge failed to load table file IO: %w", err)
	}
	generatedPaths := make([]string, 0)
	writeID := uuid.New()
	committed := false
	cleanupSafe := true
	defer func() {
		if committed || !cleanupSafe {
			return
		}
		if err := removeGeneratedMergeFiles(tableFS, tbl.Location(), writeID.String(), generatedPaths); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	writeOptions := make([]icebergtable.WriteRecordOption, 0, 2)
	writeOptions = append(writeOptions, icebergtable.WithMaxWriteWorkers(1))
	writeOptions = append(writeOptions, icebergtable.WithWriteUUID(writeID))
	if tbl.Metadata().Version() >= 3 {
		writeOptions = append(writeOptions, icebergtable.WithPreserveRowLineage(iceberggo.SchemaWithRowLineage(tbl.Schema())))
	}
	sortOrderID := icebergtable.UnsortedSortOrderID
	if len(clusterBy) > 0 {
		sortOrderID = tbl.SortOrder().OrderID()
	}
	dataFiles := make([]iceberggo.DataFile, 0)
	// WriteRecords can return before canceled worker goroutines finish. Keep its
	// internal context alive so it joins every writer before cleanup; caller
	// cancellation still reaches the record reader and is returned below.
	writeCtx, cancelWrite := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWrite()
	for dataFile, writeErr := range icebergtable.WriteRecords(writeCtx, tbl, reader.Schema(), retainedRecordIterator(reader), writeOptions...) {
		if writeErr != nil {
			return fmt.Errorf("iceberg: merge failed to write replacement data: %w", writeErr)
		}
		generatedPaths = append(generatedPaths, dataFile.FilePath())
		dataFile, err = withDataFileSortOrderID(dataFile, tbl, sortOrderID)
		if err != nil {
			return err
		}
		dataFiles = append(dataFiles, dataFile)
	}
	if err := reader.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	props := snapshotProps("merge", metadata)
	txn := tbl.NewTransaction()
	if err := stageCommitTokenLedger(txn, tbl, metadata.token); err != nil {
		return err
	}
	if err := stageCDCResumeState(txn, props); err != nil {
		return err
	}
	if len(dataFilesToDelete) == 0 {
		if tbl.Metadata().Version() >= 3 {
			nextRowID := tbl.Metadata().NextRowID()
			for i, file := range dataFiles {
				dataFiles[i], err = withDataFileFirstRowID(file, tbl, nextRowID)
				if err != nil {
					return err
				}
				nextRowID += file.Count()
			}
		}
		if err := txn.AddDataFiles(ctx, dataFiles, props); err != nil {
			return fmt.Errorf("iceberg: merge failed to stage initial files: %w", err)
		}
	} else {
		rewrite := txn.NewRewrite(props)
		rewrite.Apply(dataFilesToDelete, dataFiles, deleteFilesToRemove)
		if err := rewrite.Commit(ctx); err != nil {
			return fmt.Errorf("iceberg: merge failed to stage replacement: %w", err)
		}
	}
	table := strings.Join(tbl.Identifier(), ".")
	if err := d.validateExpectedIncarnation(ctx, tbl, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if _, err := txn.Commit(ctx); err != nil {
		if reconciled := d.reconcileCommit(ctx, table, metadata.token, opts.CDCExpectedIncarnation, err); reconciled != nil {
			cleanupSafe = errors.Is(err, icebergtable.ErrCommitFailed)
			return fmt.Errorf("iceberg: failed to commit merge on table %s: %w", tbl.Identifier(), reconciled)
		}
	}
	committed = true
	d.afterSuccessfulCommitExpected(ctx, table, opts.CDCExpectedIncarnation)
	return nil
}

func removeGeneratedMergeFiles(tableFS icebergio.IO, location, writeID string, yielded []string) error {
	const cleanupTimeout = 2 * time.Second
	deadline := time.Now().Add(cleanupTimeout)
	emptyScans := 0
	firstScan := true
	for {
		paths := make([]string, 0, len(yielded))
		if firstScan {
			paths = append(paths, yielded...)
			firstScan = false
		}
		var walkErr error
		if writeID != "" {
			walkErr = walkGeneratedMergeFiles(tableFS, location, writeID, func(path string) {
				paths = append(paths, path)
			})
		}
		if walkErr != nil {
			return fmt.Errorf("iceberg: failed to discover uncommitted merge files: %w", walkErr)
		}
		seen := make(map[string]struct{}, len(paths))
		var cleanupErr error
		for _, path := range paths {
			if _, exists := seen[path]; exists {
				continue
			}
			seen[path] = struct{}{}
			if err := tableFS.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("iceberg: failed to remove uncommitted merge file %s: %w", path, err))
			}
		}
		if cleanupErr != nil {
			return cleanupErr
		}
		if len(seen) == 0 {
			emptyScans++
			if emptyScans >= 10 {
				return nil
			}
		} else {
			emptyScans = 0
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("iceberg: timed out cleaning uncommitted merge files for write %s", writeID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func walkGeneratedMergeFiles(tableFS icebergio.IO, location, writeID string, add func(string)) error {
	visit := func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.Contains(entry.Name(), writeID) {
			add(path)
		}
		return nil
	}
	if listable, ok := tableFS.(icebergio.ListableIO); ok {
		return listable.WalkDir(location, visit)
	}
	local := strings.TrimPrefix(location, "file://")
	if strings.Contains(local, "://") {
		return fmt.Errorf("table file IO does not support listing %s", location)
	}
	return filepath.WalkDir(local, visit)
}
