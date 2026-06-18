package postgres_cdc

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replStep is one NextBatch result in a scripted WAL stream.
type replStep struct {
	batch       arrow.RecordBatch
	hadActivity bool
	lsn         pglogrepl.LSN
}

// fakeReplicator replays a scripted sequence of NextBatch results, mimicking
// the WAL message stream a real Replicator produces (Begin/Insert/Relation
// messages return no batch but are still activity; only Commit yields a batch).
type fakeReplicator struct {
	steps []replStep
	idx   int
	lsn   pglogrepl.LSN

	// pendingLowWater, when set, scripts PendingLowWater's return value.
	pendingLowWater func() (pglogrepl.LSN, bool)
}

func (f *fakeReplicator) NextBatch(_ context.Context, _ int) (arrow.RecordBatch, pglogrepl.LSN, bool, error) {
	if f.idx >= len(f.steps) {
		// Stream exhausted: report idle with no further LSN progress.
		return nil, 0, false, nil
	}
	s := f.steps[f.idx]
	f.idx++
	if s.lsn > f.lsn {
		f.lsn = s.lsn
	}
	return s.batch, s.lsn, s.hadActivity, nil
}

func (f *fakeReplicator) CurrentLSN() pglogrepl.LSN { return f.lsn }

func (f *fakeReplicator) PendingLowWater() (pglogrepl.LSN, bool) {
	if f.pendingLowWater != nil {
		return f.pendingLowWater()
	}
	return 0, false
}

func makeRowBatch(id int64) arrow.RecordBatch {
	return makeNRowBatch(1)
}

func makeNRowBatch(n int) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	b := array.NewInt64Builder(mem)
	defer b.Release()
	for i := range n {
		b.Append(int64(i))
	}
	col := b.NewArray()
	defer col.Release()

	return array.NewRecordBatch(arrowSchema, []arrow.Array{col}, int64(n))
}

// buildSingleRowTxnStream models n single-row transactions and returns the
// scripted steps plus the target LSN (the LSN of the final commit). Each
// transaction in the pgoutput protocol is Begin -> Insert -> Commit; Begin and
// Insert produce no batch but ARE activity, and only Commit emits the 1-row
// batch. LSNs increase monotonically across every step so batch mode only
// catches up after the very last commit is read.
func buildSingleRowTxnStream(n int) (steps []replStep, targetLSN pglogrepl.LSN) {
	var lsn pglogrepl.LSN
	next := func(batch arrow.RecordBatch) replStep {
		lsn++
		return replStep{batch: batch, hadActivity: true, lsn: lsn}
	}
	for i := range n {
		steps = append(
			steps,
			next(nil),                    // Begin
			next(nil),                    // Insert
			next(makeRowBatch(int64(i))), // Commit
		)
	}
	return steps, lsn
}

func drainStreamLoop(t *testing.T, steps []replStep, targetLSN pglogrepl.LSN) (batchCount int, totalRows int64) {
	t.Helper()

	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, len(steps)+1)
	accum := newBatchAccumulator(10000)

	err := streamLoop(context.Background(), repl, ModeBatch, targetLSN, 10000, accum, results, false)
	require.NoError(t, err)
	close(results)

	for res := range results {
		require.NoError(t, res.Err)
		batchCount++
		totalRows += res.Batch.NumRows()
	}
	return batchCount, totalRows
}

// TestStreamLoopAccumulatesSingleRowTransactions is a regression test for the
// bug where each single-row WAL transaction was emitted as its own batch
// (batches == rows). With activity-aware idle detection, the per-transaction
// 1-row batches accumulate and flush as a single merged batch.
func TestStreamLoopAccumulatesSingleRowTransactions(t *testing.T) {
	const numTxns = 50
	steps, targetLSN := buildSingleRowTxnStream(numTxns)

	batchCount, totalRows := drainStreamLoop(t, steps, targetLSN)

	assert.Equal(t, int64(numTxns), totalRows, "all rows should be emitted")
	assert.Equal(t, 1, batchCount, "single-row transactions should merge into one batch, not one batch per row")
	assert.Less(t, batchCount, numTxns, "batch count must not equal row count")
}
