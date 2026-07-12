package postgres_cdc

import (
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

// batchAccumulator buffers decoded changes per table as plain Go values and
// materializes a single Arrow batch per table at flush time. Building Arrow
// once per flush window instead of once per WAL transaction is what keeps
// autocommit workloads (single-row transactions) cheap: no per-transaction
// builders, schema construction, or batch concatenation.
type batchAccumulator struct {
	schemas map[string]*schema.TableSchema
	changes map[string][]Change
	// minLSN tracks the lowest (oldest) transaction LSN still buffered per
	// table. Changes are added in non-decreasing LSN order, so the first group
	// buffered for a table after a flush carries that table's minimum. Used to
	// compute a safe commit position for streaming mode.
	minLSN    map[string]pglogrepl.LSN
	threshold int
}

func newBatchAccumulator(threshold int, schemas map[string]*schema.TableSchema) *batchAccumulator {
	return &batchAccumulator{
		schemas:   schemas,
		changes:   make(map[string][]Change),
		minLSN:    make(map[string]pglogrepl.LSN),
		threshold: threshold,
	}
}

func (a *batchAccumulator) add(tableName string, changes []Change, lsn pglogrepl.LSN) {
	if len(changes) == 0 {
		return
	}
	if _, ok := a.minLSN[tableName]; !ok {
		a.minLSN[tableName] = lsn
	}
	a.changes[tableName] = append(a.changes[tableName], changes...)
}

// minPendingLSN returns the lowest transaction LSN still buffered across all
// tables, or (0, false) when the accumulator is empty.
func (a *batchAccumulator) minPendingLSN() (pglogrepl.LSN, bool) {
	var min pglogrepl.LSN
	found := false
	for _, lsn := range a.minLSN {
		if !found || lsn < min {
			min = lsn
			found = true
		}
	}
	return min, found
}

// tokenFunc computes the CommitToken to attach to a flushed batch. It is
// evaluated after the flushed table's changes have been removed from the
// accumulator, so it reflects only data that remains un-emitted.
type tokenFunc func() any

// flushReady sends merged batches for tables that have accumulated enough rows.
func (a *batchAccumulator) flushReady(results chan<- source.RecordBatchResult, token tokenFunc) error {
	for tableName, changes := range a.changes {
		if len(changes) >= a.threshold {
			if err := a.flushTable(tableName, results, token); err != nil {
				return err
			}
		}
	}
	return nil
}

// flushAll sends merged batches for all tables, regardless of row count.
func (a *batchAccumulator) flushAll(results chan<- source.RecordBatchResult, token tokenFunc) error {
	for tableName := range a.changes {
		if err := a.flushTable(tableName, results, token); err != nil {
			return err
		}
	}
	return nil
}

func (a *batchAccumulator) flushTable(tableName string, results chan<- source.RecordBatchResult, token tokenFunc) error {
	changes := a.changes[tableName]
	if len(changes) == 0 {
		return nil
	}

	delete(a.changes, tableName)
	delete(a.minLSN, tableName)

	tableSchema := a.schemas[tableName]
	if tableSchema == nil {
		return fmt.Errorf("no schema registered for table %q", tableName)
	}

	// Resolve unchanged-TOAST markers across the whole flush window: a row
	// carrying a column's full value fills a later partial UPDATE that omitted
	// it, including across transaction boundaries. The destination merge only
	// looks at the latest change per primary key, so without this fill an
	// INSERT + partial UPDATE landing in the same window would lose the
	// omitted column (the merge falls back to a target row that doesn't exist
	// yet).
	applyIntraBatchFill(changes, tableSchema)
	changes = expandUpdates(changes, tableSchema)

	// Last-write-wins compaction: only the latest change per primary key can
	// affect the merge outcome, so superseded row versions are dropped before
	// materialization.
	buffered := len(changes)
	changes = compactChanges(changes, tableSchema)

	var commitToken any
	if token != nil {
		commitToken = token()
	}

	batch, err := changesToBatch(changes, tableSchema)
	if err != nil {
		return fmt.Errorf("failed to materialize batch for table %s: %w", tableName, err)
	}

	config.Debug("[CDC] Flushed %d buffered changes (%d rows after compaction) for table %s", buffered, len(changes), tableName)

	results <- source.RecordBatchResult{
		Batch:       batch,
		TableName:   tableName,
		CommitToken: commitToken,
	}
	return nil
}
