package iceberg

import (
	"context"
	"errors"
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
	"github.com/google/uuid"
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
func useRowDeltaMerge(ctx context.Context, tbl *icebergtable.Table, deleteKeyColumns []string) (bool, error) {
	if strings.EqualFold(tbl.Metadata().Properties().Get(writeMergeModeProperty, ""), mergeModeCopyOnWrite) {
		return false, nil
	}
	if tbl.Metadata().Version() < 2 {
		return false, nil
	}
	allowed := make(map[string]bool, len(deleteKeyColumns))
	for _, col := range deleteKeyColumns {
		allowed[col] = true
	}
	spec := tbl.Metadata().PartitionSpec()
	for _, field := range spec.Fields() {
		name, ok := tbl.Schema().FindColumnName(field.SourceID())
		if !ok || !allowed[name] {
			return false, nil
		}
	}
	if tbl.CurrentSnapshot() != nil {
		tasks, err := tbl.Scan().PlanFiles(ctx)
		if err != nil {
			return false, fmt.Errorf("iceberg: failed to inspect live partition specs before merge: %w", err)
		}
		currentSpecID := int32(spec.ID())
		for _, task := range tasks {
			if task.File.SpecID() != currentSpecID {
				return false, nil
			}
		}
	}
	return true, nil
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

func icebergSchemaHasFieldFold(sc *iceberggo.Schema, name string) bool {
	for _, field := range sc.Fields() {
		if strings.EqualFold(field.Name, name) {
			return true
		}
	}
	return false
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
func batchStream(ctx context.Context, writeSchema *arrow.Schema, consumerReleases bool, produce func(emit rowEmit) error) iter.Seq2[arrow.RecordBatch, error] {
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
			if err := ctx.Err(); err != nil {
				return err
			}
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
func (d *Destination) commitRowDelta(
	ctx context.Context,
	tbl *icebergtable.Table,
	operation string,
	metadata commitMetadata,
	writeSchema *arrow.Schema,
	produceData func(emit rowEmit) error,
	deleteKeyColumns []string,
	produceDeletes func(emit rowEmit) error,
	parallelism int,
) (retErr error) {
	if err := d.validateExpectedIncarnation(ctx, tbl, metadata.expectedIncarnation); err != nil {
		return err
	}
	if tableHasCommitToken(tbl, metadata.token) {
		return nil
	}
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
	writeCtx, cancelWrite := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWrite()

	var writeOptions []icebergtable.WriteRecordOption
	writeOptions = append(writeOptions, icebergtable.WithWriteUUID(writeID))
	if parallelism > 0 {
		writeOptions = append(writeOptions, icebergtable.WithMaxWriteWorkers(parallelism))
	}
	preserveLineage := tbl.Metadata().Version() >= 3 && len(writeSchema.FieldIndices(iceberggo.RowIDColumnName)) > 0
	if preserveLineage {
		writeOptions = append(writeOptions, icebergtable.WithPreserveRowLineage(iceberggo.SchemaWithRowLineage(tbl.Schema())))
	}
	var dataFiles []iceberggo.DataFile
	for df, err := range icebergtable.WriteRecords(writeCtx, tbl, writeSchema, batchStream(ctx, writeSchema, true, produceData), writeOptions...) {
		if err != nil {
			return fmt.Errorf("iceberg: %s failed to write data files: %w", operation, err)
		}
		dataFiles = append(dataFiles, df)
		generatedPaths = append(generatedPaths, df.FilePath())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if preserveLineage {
		nextRowID := tbl.Metadata().NextRowID()
		for i, file := range dataFiles {
			dataFiles[i], err = withDataFileFirstRowID(file, tbl, nextRowID)
			if err != nil {
				return err
			}
			nextRowID += file.Count()
		}
	}

	var deleteFiles []iceberggo.DataFile
	if produceDeletes != nil && tbl.CurrentSnapshot() != nil {
		writeTxn := tbl.NewTransaction()
		deleteFieldIDs, err := fieldIDsByName(tbl.Schema(), deleteKeyColumns)
		if err != nil {
			return err
		}
		deleteSchema, err := equalityDeleteArrowSchema(tbl.Schema(), deleteKeyColumns)
		if err != nil {
			return err
		}
		deleteFiles, err = writeTxn.WriteEqualityDeletes(writeCtx, deleteFieldIDs, batchStream(ctx, deleteSchema, false, produceDeletes))
		if err != nil {
			return fmt.Errorf("iceberg: %s failed to write delete files: %w", operation, err)
		}
		for _, file := range deleteFiles {
			generatedPaths = append(generatedPaths, file.FilePath())
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(dataFiles) == 0 && len(deleteFiles) == 0 {
		err := d.commitMetadataOnly(ctx, tbl, operation, metadata)
		committed = err == nil
		return err
	}

	table := strings.Join(tbl.Identifier(), ".")
	const maxAttempts = 5
	current := tbl
	var commitErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := d.validateExpectedIncarnation(ctx, current, metadata.expectedIncarnation); err != nil {
			return err
		}
		if tableHasCommitToken(current, metadata.token) {
			committed = true
			return nil
		}
		txn := current.NewTransaction()
		if err := stageCommitTokenLedger(txn, current, metadata.token); err != nil {
			return err
		}
		props := snapshotProps(operation, metadata)
		if err := stageCDCResumeState(txn, props); err != nil {
			return err
		}
		rowDelta := txn.NewRowDelta(props).AddRows(dataFiles...).AddDeletes(deleteFiles...)
		if err := rowDelta.Commit(ctx); err != nil {
			return fmt.Errorf("iceberg: %s failed to stage row delta: %w", operation, err)
		}
		if err := d.validateExpectedIncarnation(ctx, current, metadata.expectedIncarnation); err != nil {
			return err
		}
		if _, commitErr = txn.Commit(ctx); commitErr == nil {
			committed = true
			d.afterSuccessfulCommitExpected(ctx, table, metadata.expectedIncarnation)
			return nil
		}
		if !errors.Is(commitErr, icebergtable.ErrCommitFailed) {
			if reconciled := d.reconcileCommit(ctx, table, metadata.token, metadata.expectedIncarnation, commitErr); reconciled == nil {
				committed = true
				return nil
			} else {
				cleanupSafe = false
				return fmt.Errorf("iceberg: failed to commit %s on table %s: %w", operation, tbl.Identifier(), reconciled)
			}
		}
		current, err = d.catalog.LoadTable(ctx, tbl.Identifier())
		if err != nil {
			return errors.Join(commitErr, err)
		}
		if err := waitForCommitRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("iceberg: failed to commit %s on table %s after %d attempts: %w", operation, tbl.Identifier(), maxAttempts, commitErr)
}

// mergeRowDelta merges the staging table using merge-on-read: an equality
// delete file supersedes the affected keys and the winning staging rows are
// appended, in one snapshot. Memory is bounded by the spill run size.
func (d *Destination) mergeRowDelta(ctx context.Context, target, staging *icebergtable.Table, opts destination.MergeOptions) error {
	stagingSchema, err := tableWriteSchema(staging)
	if err != nil {
		return err
	}

	sorter, err := newSpillSorter(stagingSchema, opts.PrimaryKeys)
	if err != nil {
		return err
	}
	defer sorter.Close()

	if err := forEachScannedRow(ctx, staging, iceberggo.AlwaysTrue{}, func(row []any) error { return sorter.AddContext(ctx, row) }); err != nil {
		return fmt.Errorf("iceberg: staging table %s: %w", opts.StagingTable, err)
	}
	return d.mergeRowDeltaFromSorter(ctx, target, stagingSchema, sorter, opts, mergeCommitMetadata(opts))
}

func (d *Destination) mergeRowDeltaFromSorter(
	ctx context.Context,
	target *icebergtable.Table,
	stagingSchema *arrow.Schema,
	sorter *spillSorter,
	opts destination.MergeOptions,
	metadata commitMetadata,
) error {
	stagingCols := arrowSchemaColumnNames(stagingSchema)
	if sorter.Len() == 0 {
		return d.commitMetadataOnly(ctx, target, "merge", metadata)
	}

	incrementalIdx := -1
	if opts.IncrementalKey != "" {
		idx, err := incrementalKeyIndex(stagingCols, opts.IncrementalKey, opts.StagingTable)
		if err != nil {
			return err
		}
		incrementalIdx = idx
	}

	preserveLineage := target.Metadata().Version() >= 3
	writeSchema, err := copyOnWriteTargetSchema(target, preserveLineage)
	if err != nil {
		return err
	}
	dataProjection := newRowProjection(writeSchema, stagingCols)

	emitWinners := func(proj *rowProjection) func(emit rowEmit) error {
		return func(emit rowEmit) error {
			it, err := sorter.IterContext(ctx)
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
	dataProducer := emitWinners(dataProjection)
	prepared := d.lookupPrepared(opts.TargetTable)
	var clusterSorter *spillSorter
	if len(prepared.clusterBy) > 0 {
		clusterSorter, err = newClusterSorter(writeSchema, prepared.clusterBy)
		if err != nil {
			return err
		}
		defer clusterSorter.Close()
		winners, err := sorter.IterContext(ctx)
		if err != nil {
			return err
		}
		for winners.NextGroup() {
			if err := clusterSorter.AddContext(ctx, dataProjection.projectRow(selectGroupWinner(winners, incrementalIdx))); err != nil {
				winners.Close()
				return err
			}
		}
		if err := winners.Err(); err != nil {
			winners.Close()
			return err
		}
		winners.Close()
		identity := newRowProjection(writeSchema, arrowSchemaColumnNames(writeSchema))
		dataProducer = func(emit rowEmit) error {
			it, err := clusterSorter.IterContext(ctx)
			if err != nil {
				return err
			}
			defer it.Close()
			for it.NextGroup() {
				for it.NextRow() {
					if err := emit(identity, it.Row()); err != nil {
						return err
					}
				}
			}
			return it.Err()
		}
		opts.Parallelism = 1
	}

	config.Debug("[ICEBERG MERGE] Row-delta merge of %d staged row(s) into %s", sorter.Len(), opts.TargetTable)
	return d.commitRowDelta(
		ctx, target, "merge", metadata, writeSchema,
		dataProducer,
		opts.PrimaryKeys,
		emitWinners(deleteProjection),
		opts.Parallelism,
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
	if err := forEachSCD2CurrentRow(ctx, target, currentFilter, preserveLineage, func(row []any) error { return currentSorter.AddContext(ctx, row) }); err != nil {
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
	dataProducer := func(emit rowEmit) error {
		return join.run(ctx, stagingSorter, currentSorter, scd2Emitters{
			newRow:    func(row []any) error { return emit(stagingProjection, row) },
			closedRow: func(row []any) error { return emit(targetProjection, row) },
		})
	}
	prepared := d.lookupPrepared(opts.TargetTable)
	var clusterSorter *spillSorter
	parallelism := 0
	if len(prepared.clusterBy) > 0 {
		parallelism = 1
		clusterSorter, err = newClusterSorter(writeSchema, prepared.clusterBy)
		if err != nil {
			return err
		}
		defer clusterSorter.Close()
		if err := join.run(ctx, stagingSorter, currentSorter, scd2Emitters{
			newRow:    func(row []any) error { return clusterSorter.AddContext(ctx, stagingProjection.projectRow(row)) },
			closedRow: func(row []any) error { return clusterSorter.AddContext(ctx, targetProjection.projectRow(row)) },
		}); err != nil {
			return err
		}
		identity := newRowProjection(writeSchema, arrowSchemaColumnNames(writeSchema))
		dataProducer = func(emit rowEmit) error {
			it, err := clusterSorter.IterContext(ctx)
			if err != nil {
				return err
			}
			defer it.Close()
			for it.NextGroup() {
				for it.NextRow() {
					if err := emit(identity, it.Row()); err != nil {
						return err
					}
				}
			}
			return it.Err()
		}
	}

	config.Debug("[ICEBERG SCD2] Row-delta SCD2 of %d staged row(s) into %s", stagingSorter.Len(), opts.TargetTable)
	return d.commitRowDelta(
		ctx, target, "scd2", commitMetadata{}, writeSchema,
		dataProducer,
		deleteKeyColumns,
		func(emit rowEmit) error {
			return join.run(ctx, stagingSorter, currentSorter, scd2Emitters{
				deleteKey: func(keyVals []any) error { return emit(deleteProjection, append(keyVals, true)) },
			})
		},
		parallelism,
	)
}

// scd2Emitters receives the join outputs; nil callbacks are skipped so the
// two streaming passes (data files, delete files) can share one join.
type scd2Emitters struct {
	newRow       func(row []any) error
	closedRow    func(row []any) error
	unchangedRow func(row []any) error
	deleteKey    func(keyVals []any) error
}

func (e scd2Emitters) emitUnchanged(row []any) error {
	if e.unchangedRow == nil {
		return nil
	}
	return e.unchangedRow(row)
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
	lastUpdatedIdx int
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

	j := &scd2Join{opts: opts, validFromIdx: -1, validToIdx: -1, isCurrentIdx: -1, lastUpdatedIdx: -1, incrementalIdx: -1}
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
	if idx, ok := targetIdx[iceberggo.LastUpdatedSequenceNumberColumnName]; ok {
		j.lastUpdatedIdx = idx
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
func (j *scd2Join) run(ctx context.Context, stagingSorter, currentSorter *spillSorter, emitters scd2Emitters) error {
	stagingIt, err := stagingSorter.IterContext(ctx)
	if err != nil {
		return err
	}
	defer stagingIt.Close()
	currentIt, err := currentSorter.IterContext(ctx)
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
		row, err := onlyCurrentRow(currentIt)
		if err != nil || row == nil {
			return err
		}
		return emitters.emitUnchanged(row)
	}
	softDeleteAt := j.opts.Timestamp.UTC().UnixMicro()
	row, err := onlyCurrentRow(currentIt)
	if err != nil || row == nil {
		return err
	}
	if err := emitters.emitDelete(j.targetKeyValues(row)); err != nil {
		return err
	}
	return emitters.emitClosed(j.closeRow(row, softDeleteAt))
}

func (j *scd2Join) matchKey(stagingIt, currentIt *spillIter, emitters scd2Emitters) error {
	stagingRow := selectGroupWinner(stagingIt, j.incrementalIdx)
	currentRow, err := onlyCurrentRow(currentIt)
	if err != nil {
		return err
	}
	if currentRow == nil {
		return emitters.emitNew(stagingRow)
	}

	changed := false
	for _, pair := range j.changePairs {
		if !valuesEqual(stagingRow[pair[0]], currentRow[pair[1]]) {
			changed = true
			break
		}
	}
	if !changed {
		return emitters.emitUnchanged(currentRow)
	}

	if err := emitters.emitDelete(j.targetKeyValues(currentRow)); err != nil {
		return err
	}
	validFrom := stagingRow[j.validFromIdx]
	if err := emitters.emitClosed(j.closeRow(currentRow, validFrom)); err != nil {
		return err
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
	if j.lastUpdatedIdx >= 0 {
		closed[j.lastUpdatedIdx] = nil
	}
	return closed
}

func forEachSCD2CurrentRow(ctx context.Context, tbl *icebergtable.Table, filter iceberggo.BooleanExpression, lineage bool, fn func([]any) error) error {
	if !lineage {
		return forEachScannedRow(ctx, tbl, filter, fn)
	}
	sc, records, err := tbl.Scan(icebergtable.WithRowLineage(), icebergtable.WithRowFilter(filter)).ToArrowRecords(ctx)
	if err != nil {
		return err
	}
	for batch, readErr := range records {
		if readErr != nil {
			return readErr
		}
		for r := 0; r < int(batch.NumRows()); r++ {
			row := make([]any, len(sc.Fields()))
			for c := range sc.Fields() {
				row[c], err = rowValue(batch.Column(c), r)
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

func onlyCurrentRow(it *spillIter) ([]any, error) {
	var row []any
	for it.NextRow() {
		if row != nil {
			return nil, fmt.Errorf("iceberg: SCD2 target contains duplicate current rows for primary key %x", it.Key())
		}
		row = it.Row()
	}
	return row, it.Err()
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
