package iceberg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

// The Iceberg destination implements the incremental strategies natively.
// Staging tables are regular Iceberg tables. merge and scd2 default to
// merge-on-read row deltas (see row_delta.go) which stream with bounded
// memory; delete+insert streams into a copy-on-write overwrite whose filter
// is just the interval bounds. The copy-on-write join paths in this file
// remain for the cases that must read matched target rows (CDC merges,
// destination-only columns) or where equality deletes cannot apply — they
// materialize the staged rows and the target rows they affect in memory.

// mergeEntry tracks the winning staging row(s) per primary key.
type mergeEntry struct {
	// latest is the winning row overall: for CDC ordered by LSN (deletes win
	// ties), otherwise by incremental key (arrival order breaks ties).
	latest         []any
	latestLSN      string
	latestDeleted  bool
	incrementalVal any

	// latestNonDeleted is the newest non-delete change (CDC only). Updates to
	// existing rows use it so a trailing delete doesn't clobber column values.
	latestNonDeleted    []any
	latestNonDeletedLSN string
}

func (d *Destination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("iceberg: merge requires at least one primary key")
	}

	target, err := d.loadIcebergTable(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	if err := d.validateExpectedIncarnation(ctx, target, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	metadata := mergeCommitMetadata(opts)
	if tableHasCommitToken(target, metadata.token) {
		if opts.SkipCDCResume {
			return d.ensureManagedCDCResumeResetExpected(ctx, target, metadata.token, opts.CDCExpectedIncarnation)
		}
		return nil
	}
	if !opts.SkipCDCResume {
		if err := validateCDCResumeAdvance(target, metadata); err != nil {
			return err
		}
	}
	staging, err := d.loadIcebergTable(ctx, opts.StagingTable)
	if err != nil {
		return err
	}

	// The row-delta path streams with bounded memory but replaces matched
	// rows wholesale; fall back to the copy-on-write join when old row values
	// must be read (CDC delete handling, destination-only columns) or when
	// equality deletes cannot apply (v1 tables, partition keys outside the
	// primary key, explicit write.merge.mode=copy-on-write).
	isCDC := icebergSchemaHasFieldFold(staging.Schema(), destination.CDCDeletedColumn)
	if !isCDC && stagingCoversTarget(target.Schema(), staging.Schema()) {
		rowDelta, err := useRowDeltaMerge(ctx, target, opts.PrimaryKeys)
		if err != nil {
			return err
		}
		if rowDelta && target.Metadata().Version() < 3 {
			return d.mergeRowDelta(ctx, target, staging, opts)
		}
	}
	return d.mergeCopyOnWrite(ctx, target, staging, opts)
}

func (d *Destination) mergeCopyOnWrite(ctx context.Context, target, staging *icebergtable.Table, opts destination.MergeOptions) error {
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}
	stagingRows := tableRowsMetadata(stagingSchema)
	sorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer sorter.Close()
	var maxLSN string
	var staleEventErr error
	if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, func(row []any) error {
		lsn, _ := asString(stagingRows.ValueFold(row, destination.CDCLSNColumn))
		if !opts.SkipCDCResume {
			if err := validateCDCEventPosition(target, lsn); err != nil && staleEventErr == nil {
				staleEventErr = err
			}
		}
		if compareCDCResumeLSN(lsn, maxLSN) > 0 {
			maxLSN = lsn
		}
		return sorter.AddContext(ctx, row)
	}); err != nil {
		return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
	}
	metadata := mergeCommitMetadata(opts)
	if !opts.SkipCDCResume {
		metadata = metadata.withCDCResumeLSN(maxLSN)
	}
	if metadata.token == "" && sorter.Len() > 0 {
		contentID, err := copyOnWriteContentIdentity(ctx, sorter, stagingRows, opts)
		if err != nil {
			return err
		}
		metadata.token = commitTokenID("merge-content:" + contentID)
	}
	if err := d.validateExpectedIncarnation(ctx, target, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if tableHasCommitToken(target, metadata.token) {
		if opts.SkipCDCResume {
			return d.ensureManagedCDCResumeResetExpected(ctx, target, metadata.token, opts.CDCExpectedIncarnation)
		}
		return nil
	}
	if staleEventErr != nil {
		return staleEventErr
	}
	return d.mergeCopyOnWriteSorted(ctx, target, stagingRows, sorter, opts, metadata)
}

func (d *Destination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if opts.IncrementalKey == "" {
		return errors.New("iceberg: delete+insert requires an incremental key")
	}

	target, err := d.loadIcebergTable(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	staging, err := d.loadIcebergTable(ctx, opts.StagingTable)
	if err != nil {
		return err
	}
	current := target
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		writeErr = d.deleteInsertTableOnce(ctx, current, staging, opts)
		if writeErr == nil {
			return nil
		}
		if !errors.Is(writeErr, icebergtable.ErrCommitFailed) {
			return writeErr
		}
		current, err = d.loadIcebergTable(ctx, opts.TargetTable)
		if err != nil {
			return errors.Join(writeErr, err)
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: delete+insert failed on table %s after retries: %w", opts.TargetTable, writeErr)
}

func (d *Destination) deleteInsertTableOnce(
	ctx context.Context,
	target, staging *icebergtable.Table,
	opts destination.DeleteInsertOptions,
) error {
	field, ok := target.Schema().FindFieldByName(opts.IncrementalKey)
	if !ok {
		return fmt.Errorf("iceberg: incremental key column %q not found in table %s", opts.IncrementalKey, opts.TargetTable)
	}
	start, err := normalizeBoundValue(field, opts.IntervalStart)
	if err != nil {
		return err
	}
	end, err := normalizeBoundValue(field, opts.IntervalEnd)
	if err != nil {
		return err
	}
	lower, err := columnPredicate(opGreaterThanEqual, field, start)
	if err != nil {
		return err
	}
	upper, err := columnPredicate(opLessThanEqual, field, end)
	if err != nil {
		return err
	}
	filter := iceberggo.NewAnd(lower, upper)

	preserveLineage := target.Metadata().Version() >= 3
	writeSchema, err := copyOnWriteTargetSchema(target, preserveLineage)
	if err != nil {
		return err
	}
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}
	stagingCols := arrowSchemaColumnNames(stagingSchema)
	projection := newRowProjection(writeSchema, stagingCols)

	// The staging rows stream straight into the overwrite: iceberg-go rewrites
	// the files matching the interval filter one at a time, so memory stays
	// bounded regardless of increment size. With primary keys, rows pass
	// through a disk-backed sort first so duplicates collapse to one row each.
	var produce func(sink func(arrow.RecordBatch) error) error
	if len(opts.PrimaryKeys) > 0 {
		sorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
		if err != nil {
			return err
		}
		defer sorter.Close()
		if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, func(row []any) error { return sorter.AddContext(ctx, row) }); err != nil {
			return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
		}

		incrementalIdx, err := incrementalKeyIndex(stagingCols, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return err
		}

		produce = func(sink func(arrow.RecordBatch) error) error {
			it, err := sorter.IterContext(ctx)
			if err != nil {
				return err
			}
			defer it.Close()
			emitter := newBatchEmitter(projection, sink)
			defer emitter.release()
			for it.NextGroup() {
				if err := emitter.add(selectGroupWinner(it, incrementalIdx)); err != nil {
					return err
				}
			}
			if err := it.Err(); err != nil {
				return err
			}
			return emitter.flushBatch()
		}
	} else {
		produce = func(sink func(arrow.RecordBatch) error) error {
			emitter := newBatchEmitter(projection, sink)
			defer emitter.release()
			if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, emitter.add); err != nil {
				return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
			}
			return emitter.flushBatch()
		}
	}

	config.Debug("[ICEBERG DELETE+INSERT] Replacing interval [%v, %v] on %q from %s", opts.IntervalStart, opts.IntervalEnd, opts.IncrementalKey, opts.StagingTable)

	var reader array.RecordReader = streamingReader(writeSchema, produce)
	defer reader.Release()
	combined := streamingReaderContext(ctx, writeSchema, func(sink func(arrow.RecordBatch) error) error {
		emitter := newBatchEmitter(newRowProjection(writeSchema, arrowSchemaColumnNames(writeSchema)), sink)
		defer emitter.release()
		if err := forEachSCD2CurrentRow(ctx, target, iceberggo.NewNot(filter), preserveLineage, emitter.add); err != nil {
			return err
		}
		if err := emitter.flushBatch(); err != nil {
			return err
		}
		for reader.Next() {
			batch := reader.RecordBatch()
			batch.Retain()
			if err := sink(batch); err != nil {
				return err
			}
		}
		return reader.Err()
	})
	defer combined.Release()
	prepared := d.lookupPrepared(opts.TargetTable)
	clustered, cleanupCluster, err := clusterRecordReader(ctx, combined, prepared.clusterBy)
	if err != nil {
		return fmt.Errorf("iceberg: failed to cluster delete+insert rows: %w", err)
	}
	defer cleanupCluster()
	if err := d.replaceAllMergeRecords(ctx, target, clustered, prepared.clusterBy, destination.MergeOptions{TargetTable: opts.TargetTable, PrimaryKeys: opts.PrimaryKeys}, commitMetadata{}); err != nil {
		return fmt.Errorf("iceberg: delete+insert failed on table %s: %w", opts.TargetTable, err)
	}
	d.afterSuccessfulCommit(ctx, opts.TargetTable)
	return nil
}

func (d *Destination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("iceberg: scd2 requires at least one primary key")
	}

	target, err := d.loadIcebergTable(ctx, opts.TargetTable)
	if err != nil {
		return err
	}
	staging, err := d.loadIcebergTable(ctx, opts.StagingTable)
	if err != nil {
		return err
	}

	required := append([]string{}, opts.PrimaryKeys...)
	required = append(required, destination.SCD2ValidFromColumn)
	for _, col := range required {
		if _, ok := staging.Schema().FindFieldByName(col); !ok {
			return fmt.Errorf("iceberg: column %q not found in table %s", col, opts.StagingTable)
		}
	}
	for _, col := range []string{destination.SCD2IsCurrentColumn, destination.SCD2ValidToColumn} {
		if _, ok := target.Schema().FindFieldByName(col); !ok {
			return fmt.Errorf("iceberg: column %q not found in table %s", col, opts.TargetTable)
		}
	}

	// Closing a row only rewrites the current version, so the equality delete
	// key is (primary keys, _scd_is_current): historical rows keep their
	// older sequence numbers untouched.
	deleteKeyColumns := append(append([]string{}, opts.PrimaryKeys...), destination.SCD2IsCurrentColumn)
	rowDelta, err := useRowDeltaMerge(ctx, target, deleteKeyColumns)
	if err != nil {
		return err
	}
	if rowDelta && target.Metadata().Version() < 3 {
		return d.scd2RowDelta(ctx, target, staging, opts, deleteKeyColumns)
	}
	return d.scd2CopyOnWrite(ctx, target, staging, opts)
}

func (d *Destination) scd2CopyOnWrite(ctx context.Context, target, staging *icebergtable.Table, opts destination.SCD2Options) error {
	current := target
	var writeErr error
	for attempt := 0; attempt < 5; attempt++ {
		writeErr = d.scd2CopyOnWriteOnce(ctx, current, staging, opts)
		if writeErr == nil {
			return nil
		}
		if !errors.Is(writeErr, icebergtable.ErrCommitFailed) {
			return writeErr
		}
		next, err := d.loadIcebergTable(ctx, opts.TargetTable)
		if err != nil {
			return errors.Join(writeErr, err)
		}
		current = next
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: scd2 failed on table %s after retries: %w", opts.TargetTable, writeErr)
}

func (d *Destination) scd2CopyOnWriteOnce(ctx context.Context, target, staging *icebergtable.Table, opts destination.SCD2Options) error {
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}
	stagingCols := arrowSchemaColumnNames(stagingSchema)
	preserveLineage := target.Metadata().Version() >= 3
	writeSchema, err := copyOnWriteTargetSchema(target, preserveLineage)
	if err != nil {
		return err
	}
	targetCols := arrowSchemaColumnNames(writeSchema)
	stagingSorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer stagingSorter.Close()
	if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, func(row []any) error { return stagingSorter.AddContext(ctx, row) }); err != nil {
		return err
	}
	currentSorter, err := newSpillSorter(writeSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer currentSorter.Close()
	outputSorter, err := newSpillSorter(writeSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer outputSorter.Close()
	isCurrentIdx := -1
	for i, name := range targetCols {
		if name == destination.SCD2IsCurrentColumn {
			isCurrentIdx = i
		}
	}
	if isCurrentIdx < 0 {
		return fmt.Errorf("iceberg: column %q not found in table %s", destination.SCD2IsCurrentColumn, opts.TargetTable)
	}
	if err := forEachSCD2CurrentRow(ctx, target, iceberggo.AlwaysTrue{}, preserveLineage, func(row []any) error {
		if current, _ := row[isCurrentIdx].(bool); current {
			return currentSorter.AddContext(ctx, row)
		}
		return outputSorter.AddContext(ctx, row)
	}); err != nil {
		return err
	}
	join, err := newSCD2Join(opts, stagingCols, targetCols)
	if err != nil {
		return err
	}
	stagingProjection := newRowProjection(writeSchema, stagingCols)
	targetProjection := newRowProjection(writeSchema, targetCols)
	if err := join.run(ctx, stagingSorter, currentSorter, scd2Emitters{
		newRow:       func(row []any) error { return outputSorter.AddContext(ctx, stagingProjection.projectRow(row)) },
		closedRow:    func(row []any) error { return outputSorter.AddContext(ctx, targetProjection.projectRow(row)) },
		unchangedRow: func(row []any) error { return outputSorter.AddContext(ctx, targetProjection.projectRow(row)) },
	}); err != nil {
		return err
	}
	identity := newRowProjection(writeSchema, targetCols)
	reader := streamingReaderContext(ctx, writeSchema, func(sink func(arrow.RecordBatch) error) error {
		emitter := newBatchEmitter(identity, sink)
		defer emitter.release()
		it, err := outputSorter.IterContext(ctx)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.NextGroup() {
			for it.NextRow() {
				if err := emitter.add(it.Row()); err != nil {
					return err
				}
			}
		}
		if err := it.Err(); err != nil {
			return err
		}
		return emitter.flushBatch()
	})
	defer reader.Release()
	clusterBy, sortable := identitySortColumns(target)
	if !sortable {
		clusterBy = nil
	}
	clustered, cleanup, err := clusterRecordReader(ctx, reader, clusterBy)
	if err != nil {
		return err
	}
	defer cleanup()
	return d.replaceAllMergeRecords(ctx, target, clustered, clusterBy, destination.MergeOptions{TargetTable: opts.TargetTable, PrimaryKeys: opts.PrimaryKeys}, commitMetadata{})
}

// TruncateTable empties the table in place, keeping schema, partition spec and
// table history intact.
func (d *Destination) TruncateTable(ctx context.Context, table string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	tbl, err := d.loadIcebergTable(ctx, table)
	if err != nil {
		return err
	}
	txn := tbl.NewTransaction()
	if err := stageResetCommitTokenLedger(txn, ""); err != nil {
		return err
	}
	if err := stageCDCResumeState(txn, snapshotProps("truncate")); err != nil {
		return err
	}
	if tbl.CurrentSnapshot() != nil {
		if err := txn.Delete(ctx, iceberggo.AlwaysTrue{}, snapshotProps("truncate")); err != nil {
			return fmt.Errorf("iceberg: failed to truncate table %s: %w", table, err)
		}
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit truncate of table %s: %w", table, err)
	}
	d.afterSuccessfulCommit(ctx, table)
	return nil
}

func (d *Destination) loadIcebergTable(ctx context.Context, table string) (*icebergtable.Table, error) {
	ident, err := parseIdentifier(table)
	if err != nil {
		return nil, err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to load table %s: %w", table, err)
	}
	if err := validateIsolatedTableFilePaths(tbl.Properties()); err != nil {
		return nil, fmt.Errorf("iceberg: table %s: %w", table, err)
	}
	return tbl, nil
}

func tableWriteSchema(tbl *icebergtable.Table) (*arrow.Schema, error) {
	tableSchema, err := tableSchemaFromIceberg(strings.Join(tbl.Identifier(), "."), tbl.Schema())
	if err != nil {
		return nil, err
	}
	return icebergArrowSchema(tableSchema), nil
}

func requireColumns(rows *scannedTable, columns []string, table string) error {
	for _, col := range columns {
		if !rows.HasColumn(col) {
			return fmt.Errorf("iceberg: column %q not found in table %s", col, table)
		}
	}
	return nil
}

func incrementalKeyIndex(columns []string, incrementalKey, stagingTable string) (int, error) {
	if incrementalKey == "" {
		return -1, nil
	}
	for i, col := range columns {
		if col == incrementalKey {
			return i, nil
		}
	}
	return -1, fmt.Errorf("iceberg: incremental key column %q not found in staging table %s", incrementalKey, stagingTable)
}

// projectRow maps a staging row onto the target column layout. Columns missing
// from staging keep the existing row's value (or NULL for new rows), mirroring
// the SQL merge behavior where destination-only columns are left untouched.
func projectRow(targetCols []string, staging *scannedTable, stagingRow []any, existing []any) []any {
	out := make([]any, len(targetCols))
	for i, col := range targetCols {
		if idx, ok := staging.ColIdx[col]; ok {
			out[i] = stagingRow[idx]
		} else if existing != nil {
			out[i] = existing[i]
		}
	}
	return out
}

// composeCDCMergeRow mirrors the SQL CDC merge: existing rows are updated from
// the latest non-delete change (respecting _cdc_unchanged_cols), new keys
// materialize the latest change overall (including tombstones), and keys whose
// latest change is a delete keep their data with _cdc_deleted marked true.
func composeCDCMergeRow(targetCols []string, staging *scannedTable, entry *mergeEntry, existing []any) ([]any, error) {
	if existing != nil {
		var existingLSN string
		for i, col := range targetCols {
			if strings.EqualFold(col, destination.CDCLSNColumn) {
				existingLSN, _ = asString(existing[i])
				break
			}
		}
		if destination.CompareCDCPositions(entry.latestLSN, existingLSN) < 0 {
			return append([]any(nil), existing...), nil
		}
	}

	src := entry.latestNonDeleted
	if existing == nil && src == nil {
		src = entry.latest
	}

	unchanged, err := cdcUnchangedColumns(staging, src, existing != nil)
	if err != nil {
		return nil, err
	}

	out := make([]any, len(targetCols))
	for i, col := range targetCols {
		stagingIdx, inStaging := staging.ColIdx[col]
		if !inStaging && destination.IsCDCMetaColumn(col) {
			stagingIdx, inStaging = staging.foldedColumnIndex(col)
		}
		switch {
		case !inStaging:
			if existing != nil {
				out[i] = existing[i]
			}
		case src == nil:
			// Existing row whose only new changes are deletes: keep the data.
			out[i] = existing[i]
		case existing != nil && unchanged[strings.ToLower(col)] && !destination.IsCDCMetaColumn(col):
			out[i] = existing[i]
		default:
			out[i] = src[stagingIdx]
		}
	}

	if entry.latestDeleted {
		setColumn(targetCols, out, destination.CDCDeletedColumn, true)
		setColumn(targetCols, out, destination.CDCLSNColumn, staging.ValueFold(entry.latest, destination.CDCLSNColumn))
		setColumn(targetCols, out, destination.CDCSyncedAtColumn, staging.ValueFold(entry.latest, destination.CDCSyncedAtColumn))
	}
	return out, nil
}

func cdcUnchangedColumns(staging *scannedTable, src []any, hasExisting bool) (map[string]bool, error) {
	if src == nil || !hasExisting {
		return nil, nil
	}
	raw, ok := asString(staging.ValueFold(src, destination.CDCUnchangedColsColumn))
	if !ok || raw == "" {
		return nil, nil
	}
	var cols []string
	if err := json.Unmarshal([]byte(raw), &cols); err != nil {
		return nil, fmt.Errorf("iceberg: invalid %s value %q: %w", destination.CDCUnchangedColsColumn, raw, err)
	}
	out := make(map[string]bool, len(cols))
	for _, col := range cols {
		out[strings.ToLower(col)] = true
	}
	return out, nil
}

func setColumn(cols []string, row []any, name string, value any) {
	for i, col := range cols {
		if strings.EqualFold(col, name) {
			row[i] = value
			return
		}
	}
}
