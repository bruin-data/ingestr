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
	keyValues []any

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

type dedupedStaging struct {
	keys    []string
	entries map[string]*mergeEntry
	isCDC   bool
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
	staging, err := d.loadIcebergTable(ctx, opts.StagingTable)
	if err != nil {
		return err
	}

	// The row-delta path streams with bounded memory but replaces matched
	// rows wholesale; fall back to the copy-on-write join when old row values
	// must be read (CDC delete handling, destination-only columns) or when
	// equality deletes cannot apply (v1 tables, partition keys outside the
	// primary key, explicit write.merge.mode=copy-on-write).
	_, isCDC := staging.Schema().FindFieldByName(destination.CDCDeletedColumn)
	if !isCDC && stagingCoversTarget(target.Schema(), staging.Schema()) && useRowDeltaMerge(target, opts.PrimaryKeys) {
		return d.mergeRowDelta(ctx, target, staging, opts)
	}
	return d.mergeCopyOnWrite(ctx, target, staging, opts)
}

func (d *Destination) mergeCopyOnWrite(ctx context.Context, target, staging *icebergtable.Table, opts destination.MergeOptions) error {
	stagingRows, err := scanTableRows(ctx, staging, iceberggo.AlwaysTrue{})
	if err != nil {
		return err
	}
	if err := requireColumns(stagingRows, opts.PrimaryKeys, opts.StagingTable); err != nil {
		return err
	}
	if len(stagingRows.Rows) == 0 {
		return nil
	}

	deduped, err := dedupeStagingRows(stagingRows, opts.PrimaryKeys, opts.IncrementalKey)
	if err != nil {
		return err
	}

	keyValues := make([][]any, 0, len(deduped.keys))
	for _, key := range deduped.keys {
		keyValues = append(keyValues, deduped.entries[key].keyValues)
	}
	filter, err := keyMatchFilter(target.Schema(), opts.PrimaryKeys, keyValues)
	if err != nil {
		return err
	}

	targetRows, err := scanTableRows(ctx, target, filter)
	if err != nil {
		return err
	}
	existingByKey, err := indexRowsByKey(targetRows, opts.PrimaryKeys)
	if err != nil {
		return fmt.Errorf("iceberg: target table %s: %w", opts.TargetTable, err)
	}

	outRows := make([][]any, 0, len(deduped.keys))
	for _, key := range deduped.keys {
		entry := deduped.entries[key]
		existing := lastRow(existingByKey[key])
		var row []any
		if deduped.isCDC {
			row, err = composeCDCMergeRow(targetRows.Columns, stagingRows, entry, existing)
			if err != nil {
				return err
			}
		} else {
			row = projectRow(targetRows.Columns, stagingRows, entry.latest, existing)
		}
		outRows = append(outRows, row)
	}

	config.Debug("[ICEBERG MERGE] Overwriting %d key(s) in %s", len(deduped.keys), opts.TargetTable)
	return d.overwriteRows(ctx, target, outRows, filter, "merge")
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

	writeSchema, err := tableWriteSchema(target)
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
		if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, sorter.Add); err != nil {
			return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
		}

		incrementalIdx := -1
		for i, col := range stagingCols {
			if col == opts.IncrementalKey {
				incrementalIdx = i
				break
			}
		}
		if incrementalIdx < 0 {
			return fmt.Errorf("iceberg: incremental key column %q not found in staging table %s", opts.IncrementalKey, opts.StagingTable)
		}

		produce = func(sink func(arrow.RecordBatch) error) error {
			it, err := sorter.Iter()
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

	reader := streamingReader(writeSchema, produce)
	defer reader.Release()

	txn := target.NewTransaction()
	if err := txn.Overwrite(ctx, reader, snapshotProps("delete+insert"), icebergtable.WithOverwriteFilter(filter)); err != nil {
		return fmt.Errorf("iceberg: delete+insert failed on table %s: %w", opts.TargetTable, err)
	}
	if err := reader.Err(); err != nil {
		return fmt.Errorf("iceberg: delete+insert failed on table %s: %w", opts.TargetTable, err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit delete+insert on table %s: %w", opts.TargetTable, err)
	}
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
	if useRowDeltaMerge(target, deleteKeyColumns) {
		return d.scd2RowDelta(ctx, target, staging, opts, deleteKeyColumns)
	}
	return d.scd2CopyOnWrite(ctx, target, staging, opts)
}

func (d *Destination) scd2CopyOnWrite(ctx context.Context, target, staging *icebergtable.Table, opts destination.SCD2Options) error {
	stagingRows, err := scanTableRows(ctx, staging, iceberggo.AlwaysTrue{})
	if err != nil {
		return err
	}
	required := append([]string{}, opts.PrimaryKeys...)
	required = append(required, destination.SCD2ValidFromColumn)
	if err := requireColumns(stagingRows, required, opts.StagingTable); err != nil {
		return err
	}

	isCurrentField, ok := target.Schema().FindFieldByName(destination.SCD2IsCurrentColumn)
	if !ok {
		return fmt.Errorf("iceberg: column %q not found in table %s", destination.SCD2IsCurrentColumn, opts.TargetTable)
	}
	currentFilter, err := equalityPredicate(isCurrentField, true)
	if err != nil {
		return err
	}
	targetRows, err := scanTableRows(ctx, target, currentFilter)
	if err != nil {
		return err
	}
	currentByKey, err := indexRowsByKey(targetRows, opts.PrimaryKeys)
	if err != nil {
		return fmt.Errorf("iceberg: target table %s: %w", opts.TargetTable, err)
	}

	deduped, err := dedupeStagingRows(stagingRows, opts.PrimaryKeys, "")
	if err != nil {
		return err
	}

	changeCols := scd2ChangeColumns(opts, stagingRows, targetRows)
	validToIdx, hasValidTo := targetRows.ColIdx[destination.SCD2ValidToColumn]
	isCurrentIdx := targetRows.ColIdx[destination.SCD2IsCurrentColumn]
	if !hasValidTo {
		return fmt.Errorf("iceberg: column %q not found in table %s", destination.SCD2ValidToColumn, opts.TargetTable)
	}

	var outRows [][]any
	var affectedKeys [][]any
	for _, key := range deduped.keys {
		entry := deduped.entries[key]
		current := currentByKey[key]
		if len(current) == 0 {
			outRows = append(outRows, projectRow(targetRows.Columns, stagingRows, entry.latest, nil))
			continue
		}

		if !scd2RowChanged(changeCols, stagingRows, entry.latest, targetRows, current[len(current)-1]) {
			continue
		}

		affectedKeys = append(affectedKeys, entry.keyValues)
		validFrom := stagingRows.Value(entry.latest, destination.SCD2ValidFromColumn)
		for _, cur := range current {
			closed := append([]any(nil), cur...)
			closed[validToIdx] = validFrom
			closed[isCurrentIdx] = false
			outRows = append(outRows, closed)
		}
		outRows = append(outRows, projectRow(targetRows.Columns, stagingRows, entry.latest, nil))
	}

	if opts.IncrementalKey == "" {
		softDeleteAt := opts.Timestamp.UTC().UnixMicro()
		for _, key := range targetRows.keyOrder(opts.PrimaryKeys) {
			if _, inStaging := deduped.entries[key]; inStaging {
				continue
			}
			current := currentByKey[key]
			if len(current) == 0 {
				continue
			}
			keyVals, err := rowKeyValues(targetRows, current[0], opts.PrimaryKeys)
			if err != nil {
				return fmt.Errorf("iceberg: target table %s: %w", opts.TargetTable, err)
			}
			affectedKeys = append(affectedKeys, keyVals)
			for _, cur := range current {
				closed := append([]any(nil), cur...)
				closed[validToIdx] = softDeleteAt
				closed[isCurrentIdx] = false
				outRows = append(outRows, closed)
			}
		}
	}

	if len(outRows) == 0 {
		return nil
	}

	config.Debug("[ICEBERG SCD2] Closing %d key(s), writing %d row(s) to %s", len(affectedKeys), len(outRows), opts.TargetTable)
	if len(affectedKeys) == 0 {
		return d.appendRows(ctx, target, outRows, "scd2")
	}

	keyFilter, err := keyMatchFilter(target.Schema(), opts.PrimaryKeys, affectedKeys)
	if err != nil {
		return err
	}
	filter := iceberggo.NewAnd(currentFilter, keyFilter)
	return d.overwriteRows(ctx, target, outRows, filter, "scd2")
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
	if tbl.CurrentSnapshot() == nil {
		return nil
	}
	txn := tbl.NewTransaction()
	if err := txn.Delete(ctx, iceberggo.AlwaysTrue{}, snapshotProps("truncate")); err != nil {
		return fmt.Errorf("iceberg: failed to truncate table %s: %w", table, err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit truncate of table %s: %w", table, err)
	}
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
	return tbl, nil
}

func snapshotProps(operation string) iceberggo.Properties {
	return iceberggo.Properties{
		"ingestr.destination": "iceberg",
		"ingestr.operation":   operation,
	}
}

// overwriteRows atomically deletes the rows matching filter and appends rows
// in a single snapshot commit.
func (d *Destination) overwriteRows(ctx context.Context, tbl *icebergtable.Table, rows [][]any, filter iceberggo.BooleanExpression, operation string) error {
	writeSchema, err := tableWriteSchema(tbl)
	if err != nil {
		return err
	}
	batches, err := buildRecordBatches(writeSchema, rows)
	if err != nil {
		return err
	}
	defer releaseBatches(batches)

	rdr, err := array.NewRecordReader(writeSchema, batches)
	if err != nil {
		return fmt.Errorf("iceberg: failed to create record reader: %w", err)
	}
	defer rdr.Release()

	txn := tbl.NewTransaction()
	if err := txn.Overwrite(ctx, rdr, snapshotProps(operation), icebergtable.WithOverwriteFilter(filter)); err != nil {
		return fmt.Errorf("iceberg: %s failed on table %s: %w", operation, tbl.Identifier(), err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit %s on table %s: %w", operation, tbl.Identifier(), err)
	}
	return nil
}

func (d *Destination) appendRows(ctx context.Context, tbl *icebergtable.Table, rows [][]any, operation string) error {
	writeSchema, err := tableWriteSchema(tbl)
	if err != nil {
		return err
	}
	batches, err := buildRecordBatches(writeSchema, rows)
	if err != nil {
		return err
	}
	defer releaseBatches(batches)

	rdr, err := array.NewRecordReader(writeSchema, batches)
	if err != nil {
		return fmt.Errorf("iceberg: failed to create record reader: %w", err)
	}
	defer rdr.Release()

	txn := tbl.NewTransaction()
	if err := txn.Append(ctx, rdr, snapshotProps(operation)); err != nil {
		return fmt.Errorf("iceberg: %s failed on table %s: %w", operation, tbl.Identifier(), err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit %s on table %s: %w", operation, tbl.Identifier(), err)
	}
	return nil
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

// dedupeStagingRows keeps a single winning row per primary key. For CDC data
// the latest change by LSN wins (with the latest non-delete tracked
// separately); otherwise the highest incremental key value wins, with arrival
// order breaking ties.
func dedupeStagingRows(staging *scannedTable, primaryKeys []string, incrementalKey string) (*dedupedStaging, error) {
	out := &dedupedStaging{
		entries: make(map[string]*mergeEntry),
		isCDC:   staging.HasColumn(destination.CDCDeletedColumn),
	}

	incrementalIdx := -1
	if !out.isCDC && incrementalKey != "" {
		if idx, ok := staging.ColIdx[incrementalKey]; ok {
			incrementalIdx = idx
		}
	}

	for _, row := range staging.Rows {
		keyValues, err := rowKeyValues(staging, row, primaryKeys)
		if err != nil {
			return nil, fmt.Errorf("iceberg: staging data: %w", err)
		}
		key, err := encodeRowKey(keyValues)
		if err != nil {
			return nil, fmt.Errorf("iceberg: staging data: %w", err)
		}

		entry, ok := out.entries[key]
		if !ok {
			entry = &mergeEntry{keyValues: keyValues}
			out.entries[key] = entry
			out.keys = append(out.keys, key)
		}

		switch {
		case out.isCDC:
			lsn, _ := asString(staging.Value(row, destination.CDCLSNColumn))
			deleted, _ := staging.Value(row, destination.CDCDeletedColumn).(bool)
			if entry.latest == nil || destination.CDCSupersedes(lsn, deleted, entry.latestLSN, entry.latestDeleted) {
				entry.latest = row
				entry.latestLSN = lsn
				entry.latestDeleted = deleted
			}
			if !deleted && (entry.latestNonDeleted == nil || lsn >= entry.latestNonDeletedLSN) {
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
	return out, nil
}

func rowKeyValues(rows *scannedTable, row []any, primaryKeys []string) ([]any, error) {
	values := make([]any, len(primaryKeys))
	for i, pk := range primaryKeys {
		idx, ok := rows.ColIdx[pk]
		if !ok {
			return nil, fmt.Errorf("primary key column %q not found", pk)
		}
		if row[idx] == nil {
			return nil, fmt.Errorf("primary key column %q contains NULL", pk)
		}
		values[i] = row[idx]
	}
	return values, nil
}

func indexRowsByKey(rows *scannedTable, primaryKeys []string) (map[string][][]any, error) {
	out := make(map[string][][]any, len(rows.Rows))
	for _, row := range rows.Rows {
		values, err := rowKeyValues(rows, row, primaryKeys)
		if err != nil {
			return nil, err
		}
		key, err := encodeRowKey(values)
		if err != nil {
			return nil, err
		}
		out[key] = append(out[key], row)
	}
	return out, nil
}

// keyOrder returns encoded keys in first-encounter row order.
func (s *scannedTable) keyOrder(primaryKeys []string) []string {
	seen := make(map[string]struct{}, len(s.Rows))
	keys := make([]string, 0, len(s.Rows))
	for _, row := range s.Rows {
		values, err := rowKeyValues(s, row, primaryKeys)
		if err != nil {
			continue
		}
		key, err := encodeRowKey(values)
		if err != nil {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func lastRow(rows [][]any) []any {
	if len(rows) == 0 {
		return nil
	}
	return rows[len(rows)-1]
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
		switch {
		case !inStaging:
			if existing != nil {
				out[i] = existing[i]
			}
		case src == nil:
			// Existing row whose only new changes are deletes: keep the data.
			out[i] = existing[i]
		case existing != nil && unchanged[col] && !destination.IsCDCMetaColumn(col):
			out[i] = existing[i]
		default:
			out[i] = src[stagingIdx]
		}
	}

	if entry.latestDeleted {
		setColumn(targetCols, out, destination.CDCDeletedColumn, true)
		setColumn(targetCols, out, destination.CDCLSNColumn, staging.Value(entry.latest, destination.CDCLSNColumn))
		setColumn(targetCols, out, destination.CDCSyncedAtColumn, staging.Value(entry.latest, destination.CDCSyncedAtColumn))
	}
	return out, nil
}

func cdcUnchangedColumns(staging *scannedTable, src []any, hasExisting bool) (map[string]bool, error) {
	if src == nil || !hasExisting || !staging.HasColumn(destination.CDCUnchangedColsColumn) {
		return nil, nil
	}
	raw, ok := asString(staging.Value(src, destination.CDCUnchangedColsColumn))
	if !ok || raw == "" {
		return nil, nil
	}
	var cols []string
	if err := json.Unmarshal([]byte(raw), &cols); err != nil {
		return nil, fmt.Errorf("iceberg: invalid %s value %q: %w", destination.CDCUnchangedColsColumn, raw, err)
	}
	out := make(map[string]bool, len(cols))
	for _, col := range cols {
		out[col] = true
	}
	return out, nil
}

func setColumn(cols []string, row []any, name string, value any) {
	for i, col := range cols {
		if col == name {
			row[i] = value
			return
		}
	}
}

func scd2ChangeColumns(opts destination.SCD2Options, staging, target *scannedTable) []string {
	excluded := make(map[string]bool)
	for _, col := range destination.SCD2NonDataColumns(opts.PrimaryKeys) {
		excluded[strings.ToLower(col)] = true
	}
	out := make([]string, 0, len(opts.Columns))
	for _, col := range opts.Columns {
		if excluded[strings.ToLower(col)] {
			continue
		}
		if !staging.HasColumn(col) || !target.HasColumn(col) {
			continue
		}
		out = append(out, col)
	}
	return out
}

func scd2RowChanged(changeCols []string, staging *scannedTable, stagingRow []any, target *scannedTable, targetRow []any) bool {
	for _, col := range changeCols {
		if !valuesEqual(staging.Value(stagingRow, col), target.Value(targetRow, col)) {
			return true
		}
	}
	return false
}
