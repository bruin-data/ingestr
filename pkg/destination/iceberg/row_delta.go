package iceberg

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

const (
	writeMergeModeProperty = "write.merge.mode"
	mergeModeCopyOnWrite   = "copy-on-write"
)

// useRowDeltaMerge decides whether an operation can run as a merge-on-read
// row delta (equality delete files + appended data files, constant memory)
// instead of the copy-on-write join path. Equality deletes are routed by
// partition value, so every partition source column must be part of the
// delete key — otherwise a row whose partition value changed would not be
// deleted from its old partition.
func useRowDeltaMerge(tbl *icebergtable.Table, deleteKeyColumns []string) bool {
	if strings.EqualFold(tbl.Metadata().Properties().Get(writeMergeModeProperty, ""), mergeModeCopyOnWrite) {
		return false
	}
	if tbl.Metadata().Version() < 2 {
		return false
	}
	allowed := make(map[string]bool, len(deleteKeyColumns))
	for _, col := range deleteKeyColumns {
		allowed[col] = true
	}
	spec := tbl.Metadata().PartitionSpec()
	for _, field := range spec.Fields() {
		name, ok := tbl.Schema().FindColumnName(field.SourceID())
		if !ok || !allowed[name] {
			return false
		}
	}
	return true
}

// stagingCoversTarget reports whether every target column also exists in
// staging. Row-delta merge replaces rows wholesale, so destination-only
// columns (which the copy-on-write path preserves) rule it out.
func stagingCoversTarget(target, staging *iceberggo.Schema) bool {
	stagingCols := make(map[string]bool, staging.NumFields())
	for _, field := range staging.Fields() {
		stagingCols[field.Name] = true
	}
	for _, field := range target.Fields() {
		if !stagingCols[field.Name] {
			return false
		}
	}
	return true
}

func fieldIDsByName(iceSchema *iceberggo.Schema, names []string) ([]int, error) {
	ids := make([]int, len(names))
	for i, name := range names {
		field, ok := iceSchema.FindFieldByName(name)
		if !ok {
			return nil, fmt.Errorf("iceberg: column %q not found in table schema", name)
		}
		ids[i] = field.ID
	}
	return ids, nil
}

func equalityDeleteArrowSchema(iceSchema *iceberggo.Schema, names []string) (*arrow.Schema, error) {
	selected, err := iceSchema.Select(true, names...)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to project delete schema: %w", err)
	}
	arrowSchema, err := icebergtable.SchemaToArrowSchema(selected, nil, true, false)
	if err != nil {
		return nil, fmt.Errorf("iceberg: failed to build delete schema: %w", err)
	}
	return arrowSchema, nil
}

func arrowSchemaColumnNames(schema *arrow.Schema) []string {
	names := make([]string, len(schema.Fields()))
	for i, field := range schema.Fields() {
		names[i] = field.Name
	}
	return names
}

// rowEmit receives composed output rows: row values laid out per the given
// projection's source column order.
type rowEmit func(proj *rowProjection, row []any) error

// batchStream converts a row-producing function into a record batch iterator
// with bounded buffering. When consumerReleases is false the batches are
// released after the consumer's yield returns (WriteEqualityDeletes borrows
// batches); WriteRecords releases consumed batches itself.
func batchStream(writeSchema *arrow.Schema, consumerReleases bool, produce func(emit rowEmit) error) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		builder := array.NewRecordBuilder(memory.DefaultAllocator, writeSchema)
		defer builder.Release()

		rows := 0
		stopped := false
		flush := func() error {
			if rows == 0 {
				return nil
			}
			batch := builder.NewRecordBatch()
			rows = 0
			ok := yield(batch, nil)
			if !consumerReleases {
				batch.Release()
			}
			if !ok {
				stopped = true
				return errStreamStopped
			}
			return nil
		}

		err := produce(func(proj *rowProjection, row []any) error {
			if err := proj.appendRow(builder, row); err != nil {
				return err
			}
			rows++
			if rows >= batchBuildRows {
				return flush()
			}
			return nil
		})
		if err == nil {
			err = flush()
		}
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

var errStreamStopped = fmt.Errorf("iceberg: batch stream consumer stopped")

// commitRowDelta writes the data and delete files produced by the two
// streaming passes and commits them as one atomic snapshot.
func commitRowDelta(
	ctx context.Context,
	tbl *icebergtable.Table,
	operation string,
	writeSchema *arrow.Schema,
	produceData func(emit rowEmit) error,
	deleteKeyColumns []string,
	produceDeletes func(emit rowEmit) error,
) error {
	txn := tbl.NewTransaction()

	var dataFiles []iceberggo.DataFile
	for df, err := range icebergtable.WriteRecords(ctx, tbl, writeSchema, batchStream(writeSchema, true, produceData)) {
		if err != nil {
			return fmt.Errorf("iceberg: %s failed to write data files: %w", operation, err)
		}
		dataFiles = append(dataFiles, df)
	}

	var deleteFiles []iceberggo.DataFile
	if produceDeletes != nil && tbl.CurrentSnapshot() != nil {
		deleteFieldIDs, err := fieldIDsByName(tbl.Schema(), deleteKeyColumns)
		if err != nil {
			return err
		}
		deleteSchema, err := equalityDeleteArrowSchema(tbl.Schema(), deleteKeyColumns)
		if err != nil {
			return err
		}
		deleteFiles, err = txn.WriteEqualityDeletes(ctx, deleteFieldIDs, batchStream(deleteSchema, false, produceDeletes))
		if err != nil {
			return fmt.Errorf("iceberg: %s failed to write delete files: %w", operation, err)
		}
	}

	if len(dataFiles) == 0 && len(deleteFiles) == 0 {
		return nil
	}

	rowDelta := txn.NewRowDelta(snapshotProps(operation)).AddRows(dataFiles...).AddDeletes(deleteFiles...)
	if err := rowDelta.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: %s failed to stage row delta: %w", operation, err)
	}
	if _, err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("iceberg: failed to commit %s on table %s: %w", operation, tbl.Identifier(), err)
	}
	return nil
}

// mergeRowDelta merges the staging table using merge-on-read: an equality
// delete file supersedes the affected keys and the winning staging rows are
// appended, in one snapshot. Memory is bounded by the spill run size.
func (d *Destination) mergeRowDelta(ctx context.Context, target, staging *icebergtable.Table, opts destination.MergeOptions) error {
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}
	stagingCols := arrowSchemaColumnNames(stagingSchema)

	sorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer sorter.Close()

	if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, sorter.Add); err != nil {
		return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
	}
	if sorter.Len() == 0 {
		return nil
	}

	incrementalIdx := -1
	if opts.IncrementalKey != "" {
		idx, err := incrementalKeyIndex(stagingCols, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return err
		}
		incrementalIdx = idx
	}

	writeSchema, err := tableWriteSchema(target)
	if err != nil {
		return err
	}
	dataProjection := newRowProjection(writeSchema, stagingCols)

	emitWinners := func(proj *rowProjection) func(emit rowEmit) error {
		return func(emit rowEmit) error {
			it, err := sorter.Iter()
			if err != nil {
				return err
			}
			defer it.Close()
			for it.NextGroup() {
				winner := selectGroupWinner(it, incrementalIdx)
				if err := emit(proj, winner); err != nil {
					return err
				}
			}
			return it.Err()
		}
	}

	deleteSchema, err := equalityDeleteArrowSchema(target.Schema(), opts.PrimaryKeys)
	if err != nil {
		return err
	}
	deleteProjection := newRowProjection(deleteSchema, stagingCols)

	config.Debug("[ICEBERG MERGE] Row-delta merge of %d staged row(s) into %s", sorter.Len(), opts.TargetTable)
	return commitRowDelta(
		ctx, target, "merge", writeSchema,
		emitWinners(dataProjection),
		opts.PrimaryKeys,
		emitWinners(deleteProjection),
	)
}

// scd2RowDelta maintains SCD2 history via merge-on-read: current versions of
// changed or vanished keys are superseded by an equality delete on
// (primary keys, _scd_is_current) and replaced by closed copies, while new
// versions and net-new keys are appended — all in one snapshot. Both sides of
// the change-detection join stream through disk-backed sorts, so memory stays
// bounded by the spill run size.
func (d *Destination) scd2RowDelta(ctx context.Context, target, staging *icebergtable.Table, opts destination.SCD2Options, deleteKeyColumns []string) error {
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}
	stagingCols := arrowSchemaColumnNames(stagingSchema)
	writeSchema, err := tableWriteSchema(target)
	if err != nil {
		return err
	}
	targetCols := arrowSchemaColumnNames(writeSchema)

	stagingSorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer stagingSorter.Close()
	if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, stagingSorter.Add); err != nil {
		return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
	}

	isCurrentField, _ := target.Schema().FindFieldByName(destination.SCD2IsCurrentColumn)
	currentFilter, err := equalityPredicate(isCurrentField, true)
	if err != nil {
		return err
	}
	currentSorter, err := newSpillSorter(writeSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer currentSorter.Close()
	if err := forEachScannedRow(ctx, target, currentFilter, currentSorter.Add); err != nil {
		return fmt.Errorf("iceberg: target table %s: %w", opts.TargetTable, err)
	}

	join, err := newSCD2Join(opts, stagingCols, targetCols)
	if err != nil {
		return err
	}

	deleteSchema, err := equalityDeleteArrowSchema(target.Schema(), deleteKeyColumns)
	if err != nil {
		return err
	}

	stagingProjection := newRowProjection(writeSchema, stagingCols)
	targetProjection := newRowProjection(writeSchema, targetCols)
	deleteProjection := newRowProjection(deleteSchema, deleteKeyColumns)

	config.Debug("[ICEBERG SCD2] Row-delta SCD2 of %d staged row(s) into %s", stagingSorter.Len(), opts.TargetTable)
	return commitRowDelta(
		ctx, target, "scd2", writeSchema,
		func(emit rowEmit) error {
			return join.run(stagingSorter, currentSorter, scd2Emitters{
				newRow:    func(row []any) error { return emit(stagingProjection, row) },
				closedRow: func(row []any) error { return emit(targetProjection, row) },
			})
		},
		deleteKeyColumns,
		func(emit rowEmit) error {
			return join.run(stagingSorter, currentSorter, scd2Emitters{
				deleteKey: func(keyVals []any) error { return emit(deleteProjection, append(keyVals, true)) },
			})
		},
	)
}

// scd2Emitters receives the join outputs; nil callbacks are skipped so the
// two streaming passes (data files, delete files) can share one join.
type scd2Emitters struct {
	newRow    func(row []any) error
	closedRow func(row []any) error
	deleteKey func(keyVals []any) error
}

func (e scd2Emitters) emitNew(row []any) error {
	if e.newRow == nil {
		return nil
	}
	return e.newRow(row)
}

func (e scd2Emitters) emitClosed(row []any) error {
	if e.closedRow == nil {
		return nil
	}
	return e.closedRow(row)
}

func (e scd2Emitters) emitDelete(keyVals []any) error {
	if e.deleteKey == nil {
		return nil
	}
	return e.deleteKey(keyVals)
}

type scd2Join struct {
	opts           destination.SCD2Options
	keyIdxTarget   []int
	validFromIdx   int
	validToIdx     int
	isCurrentIdx   int
	incrementalIdx int
	changePairs    [][2]int // staging column index, target column index
}

func newSCD2Join(opts destination.SCD2Options, stagingCols, targetCols []string) (*scd2Join, error) {
	stagingIdx := make(map[string]int, len(stagingCols))
	for i, col := range stagingCols {
		stagingIdx[col] = i
	}
	targetIdx := make(map[string]int, len(targetCols))
	for i, col := range targetCols {
		targetIdx[col] = i
	}

	j := &scd2Join{opts: opts, validFromIdx: -1, validToIdx: -1, isCurrentIdx: -1, incrementalIdx: -1}
	if idx, ok := stagingIdx[destination.SCD2ValidFromColumn]; ok {
		j.validFromIdx = idx
	} else {
		return nil, fmt.Errorf("iceberg: column %q not found in staging table", destination.SCD2ValidFromColumn)
	}
	if opts.IncrementalKey != "" {
		idx, err := incrementalKeyIndex(stagingCols, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return nil, err
		}
		j.incrementalIdx = idx
	}
	if idx, ok := targetIdx[destination.SCD2ValidToColumn]; ok {
		j.validToIdx = idx
	} else {
		return nil, fmt.Errorf("iceberg: column %q not found in target table", destination.SCD2ValidToColumn)
	}
	if idx, ok := targetIdx[destination.SCD2IsCurrentColumn]; ok {
		j.isCurrentIdx = idx
	} else {
		return nil, fmt.Errorf("iceberg: column %q not found in target table", destination.SCD2IsCurrentColumn)
	}

	for _, pk := range opts.PrimaryKeys {
		idx, ok := targetIdx[pk]
		if !ok {
			return nil, fmt.Errorf("iceberg: primary key column %q not found in target table", pk)
		}
		j.keyIdxTarget = append(j.keyIdxTarget, idx)
	}

	excluded := make(map[string]bool)
	for _, col := range destination.SCD2NonDataColumns(opts.PrimaryKeys) {
		excluded[strings.ToLower(col)] = true
	}
	for _, col := range opts.Columns {
		if excluded[strings.ToLower(col)] {
			continue
		}
		sIdx, sOK := stagingIdx[col]
		tIdx, tOK := targetIdx[col]
		if sOK && tOK {
			j.changePairs = append(j.changePairs, [2]int{sIdx, tIdx})
		}
	}
	return j, nil
}

// run merge-joins the key-sorted staging winners against the key-sorted
// current target rows and emits the SCD2 outcome per key.
func (j *scd2Join) run(stagingSorter, currentSorter *spillSorter, emitters scd2Emitters) error {
	stagingIt, err := stagingSorter.Iter()
	if err != nil {
		return err
	}
	defer stagingIt.Close()
	currentIt, err := currentSorter.Iter()
	if err != nil {
		return err
	}
	defer currentIt.Close()

	stagingOK := stagingIt.NextGroup()
	currentOK := currentIt.NextGroup()
	for stagingOK || currentOK {
		switch {
		case stagingOK && (!currentOK || stagingIt.Key() < currentIt.Key()):
			// Net-new key: insert the staged version as current.
			if err := emitters.emitNew(selectGroupWinner(stagingIt, j.incrementalIdx)); err != nil {
				return err
			}
			stagingOK = stagingIt.NextGroup()

		case currentOK && (!stagingOK || currentIt.Key() < stagingIt.Key()):
			// Key vanished from the source: soft-delete on full snapshots.
			if err := j.softDelete(currentIt, emitters); err != nil {
				return err
			}
			currentOK = currentIt.NextGroup()

		default:
			if err := j.matchKey(stagingIt, currentIt, emitters); err != nil {
				return err
			}
			stagingOK = stagingIt.NextGroup()
			currentOK = currentIt.NextGroup()
		}
	}
	if err := stagingIt.Err(); err != nil {
		return err
	}
	return currentIt.Err()
}

func (j *scd2Join) softDelete(currentIt *spillIter, emitters scd2Emitters) error {
	if j.opts.IncrementalKey != "" {
		return nil
	}
	softDeleteAt := j.opts.Timestamp.UTC().UnixMicro()
	first := true
	for currentIt.NextRow() {
		row := currentIt.Row()
		if first {
			if err := emitters.emitDelete(j.targetKeyValues(row)); err != nil {
				return err
			}
			first = false
		}
		if err := emitters.emitClosed(j.closeRow(row, softDeleteAt)); err != nil {
			return err
		}
	}
	return nil
}

func (j *scd2Join) matchKey(stagingIt, currentIt *spillIter, emitters scd2Emitters) error {
	stagingRow := selectGroupWinner(stagingIt, j.incrementalIdx)
	currentRows := collectGroupRows(currentIt)
	if len(currentRows) == 0 {
		return emitters.emitNew(stagingRow)
	}

	changed := false
	latestCurrent := currentRows[len(currentRows)-1]
	for _, pair := range j.changePairs {
		if !valuesEqual(stagingRow[pair[0]], latestCurrent[pair[1]]) {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}

	if err := emitters.emitDelete(j.targetKeyValues(latestCurrent)); err != nil {
		return err
	}
	validFrom := stagingRow[j.validFromIdx]
	for _, row := range currentRows {
		if err := emitters.emitClosed(j.closeRow(row, validFrom)); err != nil {
			return err
		}
	}
	return emitters.emitNew(stagingRow)
}

func (j *scd2Join) targetKeyValues(row []any) []any {
	keyVals := make([]any, 0, len(j.keyIdxTarget)+1)
	for _, idx := range j.keyIdxTarget {
		keyVals = append(keyVals, row[idx])
	}
	return keyVals
}

func (j *scd2Join) closeRow(row []any, validTo any) []any {
	closed := append([]any(nil), row...)
	closed[j.validToIdx] = validTo
	closed[j.isCurrentIdx] = false
	return closed
}

func collectGroupRows(it *spillIter) [][]any {
	var rows [][]any
	for it.NextRow() {
		rows = append(rows, it.Row())
	}
	return rows
}

// selectGroupWinner consumes the current group and returns the winning row:
// the highest incremental key value when one is tracked (arrival order breaks
// ties), otherwise the last row.
func selectGroupWinner(it *spillIter, incrementalIdx int) []any {
	var winner []any
	var winnerVal any
	for it.NextRow() {
		row := it.Row()
		if incrementalIdx < 0 || winner == nil {
			winner = row
			if incrementalIdx >= 0 {
				winnerVal = row[incrementalIdx]
			}
			continue
		}
		if cmp, comparable := compareOrdered(row[incrementalIdx], winnerVal); !comparable || cmp >= 0 {
			winner = row
			winnerVal = row[incrementalIdx]
		}
	}
	return winner
}
