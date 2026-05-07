package postgres_cdc

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/source"
)

// batchAccumulator collects small per-table Arrow batches and merges them
// into larger ones before sending downstream. This is critical for CDC
// workloads where each WAL transaction commit produces a 1-row batch.
type batchAccumulator struct {
	batches   map[string][]arrow.RecordBatch
	rowCounts map[string]int64
	threshold int64
}

func newBatchAccumulator(threshold int) *batchAccumulator {
	return &batchAccumulator{
		batches:   make(map[string][]arrow.RecordBatch),
		rowCounts: make(map[string]int64),
		threshold: int64(threshold),
	}
}

func (a *batchAccumulator) add(tableName string, batch arrow.RecordBatch) {
	a.batches[tableName] = append(a.batches[tableName], batch)
	a.rowCounts[tableName] += batch.NumRows()
}

// flushReady sends merged batches for tables that have accumulated enough rows.
func (a *batchAccumulator) flushReady(results chan<- source.RecordBatchResult) {
	for tableName, count := range a.rowCounts {
		if count >= a.threshold {
			a.flushTable(tableName, results)
		}
	}
}

// flushAll sends merged batches for all tables, regardless of row count.
func (a *batchAccumulator) flushAll(results chan<- source.RecordBatchResult) {
	for tableName := range a.batches {
		a.flushTable(tableName, results)
	}
}

func (a *batchAccumulator) flushTable(tableName string, results chan<- source.RecordBatchResult) {
	batches := a.batches[tableName]
	if len(batches) == 0 {
		return
	}

	delete(a.batches, tableName)
	delete(a.rowCounts, tableName)

	if len(batches) == 1 {
		results <- source.RecordBatchResult{
			Batch:     batches[0],
			TableName: tableName,
		}
		return
	}

	merged := concatRecordBatches(batches)
	config.Debug("[CDC] Flushed %d micro-batches into %d rows for table %s", len(batches), merged.NumRows(), tableName)

	// Release the originals now that the merged batch owns the data
	for _, b := range batches {
		b.Release()
	}

	results <- source.RecordBatchResult{
		Batch:     merged,
		TableName: tableName,
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
