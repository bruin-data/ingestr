package postgres_cdc

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

// batchAccumulator collects small per-table Arrow batches and merges them
// into larger ones before sending downstream. This is critical for CDC
// workloads where each WAL transaction commit produces a 1-row batch.
type batchAccumulator struct {
	batches   map[string][]arrow.RecordBatch
	rowCounts map[string]int64
	// minLSN tracks the lowest (oldest) transaction LSN still buffered per
	// table. Batches are added in non-decreasing LSN order, so the first batch
	// buffered for a table after a flush carries that table's minimum. Used to
	// compute a safe commit position for streaming mode.
	minLSN    map[string]pglogrepl.LSN
	threshold int64

	// transform, when set, post-processes each merged batch just before it is
	// sent downstream. It receives the table name and the outgoing batch and
	// returns the batch to emit. Returning a batch different from the input
	// transfers ownership (the original is released). The accumulator itself
	// stays schema-agnostic; CDC-specific logic lives in the supplied function.
	transform func(tableName string, batch arrow.RecordBatch) arrow.RecordBatch
}

func newBatchAccumulator(threshold int) *batchAccumulator {
	return &batchAccumulator{
		batches:   make(map[string][]arrow.RecordBatch),
		rowCounts: make(map[string]int64),
		minLSN:    make(map[string]pglogrepl.LSN),
		threshold: int64(threshold),
	}
}

func (a *batchAccumulator) add(tableName string, batch arrow.RecordBatch, lsn pglogrepl.LSN) {
	if _, ok := a.minLSN[tableName]; !ok {
		a.minLSN[tableName] = lsn
	}
	a.batches[tableName] = append(a.batches[tableName], batch)
	a.rowCounts[tableName] += batch.NumRows()
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
// evaluated after the flushed table's batches have been removed from the
// accumulator, so it reflects only data that remains un-emitted.
type tokenFunc func() any

// flushReady sends merged batches for tables that have accumulated enough rows.
func (a *batchAccumulator) flushReady(results chan<- source.RecordBatchResult, token tokenFunc) {
	for tableName, count := range a.rowCounts {
		if count >= a.threshold {
			a.flushTable(tableName, results, token)
		}
	}
}

// flushAll sends merged batches for all tables, regardless of row count.
func (a *batchAccumulator) flushAll(results chan<- source.RecordBatchResult, token tokenFunc) {
	for tableName := range a.batches {
		a.flushTable(tableName, results, token)
	}
}

func (a *batchAccumulator) flushTable(tableName string, results chan<- source.RecordBatchResult, token tokenFunc) {
	batches := a.batches[tableName]
	if len(batches) == 0 {
		return
	}

	delete(a.batches, tableName)
	delete(a.rowCounts, tableName)
	delete(a.minLSN, tableName)

	var commitToken any
	if token != nil {
		commitToken = token()
	}

	var out arrow.RecordBatch
	if len(batches) == 1 {
		out = batches[0]
	} else {
		out = concatRecordBatches(batches)
		config.Debug("[CDC] Flushed %d micro-batches into %d rows for table %s", len(batches), out.NumRows(), tableName)

		// Release the originals now that the merged batch owns the data
		for _, b := range batches {
			b.Release()
		}
	}

	if a.transform != nil {
		if transformed := a.transform(tableName, out); transformed != out {
			out.Release()
			out = transformed
		}
	}

	results <- source.RecordBatchResult{
		Batch:       out,
		TableName:   tableName,
		CommitToken: commitToken,
	}
}

// concatRecordBatches merges multiple Arrow record batches (same schema)
// into a single batch by concatenating each column array.
func concatRecordBatches(batches []arrow.RecordBatch) arrow.RecordBatch {
	schema := batches[0].Schema()
	mem := memory.NewGoAllocator()

	numCols := int(schema.NumFields())
	cols := make([]arrow.Array, numCols)

	for i := 0; i < numCols; i++ {
		arrays := make([]arrow.Array, len(batches))
		for j, b := range batches {
			arrays[j] = b.Column(i)
		}
		concatenated, err := array.Concatenate(arrays, mem)
		if err != nil {
			// Same-schema arrays should always concatenate successfully.
			// Fall back to returning the first batch if this somehow fails.
			config.Debug("[CDC] Failed to concatenate column %d: %v, falling back to first batch", i, err)
			for k := 0; k < i; k++ {
				cols[k].Release()
			}
			batches[0].Retain()
			return batches[0]
		}
		cols[i] = concatenated
	}

	var totalRows int64
	for _, b := range batches {
		totalRows += b.NumRows()
	}

	result := array.NewRecordBatch(schema, cols, totalRows)

	// NewRecordBatch retains the columns, so release our references
	for _, col := range cols {
		col.Release()
	}

	return result
}
