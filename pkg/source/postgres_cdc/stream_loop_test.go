package postgres_cdc

import (
	"context"
	"testing"
	"time"

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

// TestStreamLoopEmitsIdleCommitToken is a regression test for the streaming
// lag stall: once the stream catches up and goes idle, the loop must emit a
// bare CommitToken (no batch) carrying the caught-up LSN so the pipeline can
// advance the replication slot's confirmed_flush_lsn. Without it, an idle or
// low-traffic stream never advances the slot and replica lag grows unbounded.
func TestStreamLoopEmitsIdleCommitToken(t *testing.T) {
	steps, targetLSN := buildSingleRowTxnStream(3)
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, 64)
	accum := newBatchAccumulator(10000)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// ModeStream with targetLSN 0 streams forever until ctx is cancelled.
		done <- streamLoop(ctx, repl, ModeStream, 0, 10000, accum, results, true)
	}()

	var idleToken pglogrepl.LSN
	sawIdleToken := false
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case res := <-results:
			require.NoError(t, res.Err)
			// A bare commit token has no batch but carries the caught-up LSN.
			if res.Batch == nil && res.CommitToken != nil {
				lsn, ok := res.CommitToken.(pglogrepl.LSN)
				require.True(t, ok, "expected an LSN commit token")
				idleToken = lsn
				sawIdleToken = true
				break loop
			}
			if res.Batch != nil {
				res.Batch.Release()
			}
		case <-deadline:
			break loop
		}
	}
	cancel()
	<-done

	require.True(t, sawIdleToken, "streaming idle must emit a bare commit token")
	assert.Equal(t, targetLSN, idleToken, "idle token must equal the caught-up LSN")
}

// TestStreamModeIdleSlotAdvances_Repro is an end-to-end reproduction of the
// streaming replica-lag stall. It models the real-world trigger: a single
// change to a published table, then a long idle period during which the rest of
// the database keeps writing WAL (keepalives advance the received position) but
// our tables produce nothing further.
//
// It drives the real streamLoop and threads every emitted CommitToken through
// the real PostgresCDCSource.CommitStream / streamPosition, then checks the
// position the client would report as WALFlushPosition (which is what advances
// the slot's confirmed_flush_lsn and therefore what makes lag drop).
//
// With the idle-token fix the reported flush position catches up to the
// received position (lag -> 0). WITHOUT it (delete the emitIdleCommitToken
// calls in reader.go / multitable_reader.go), the flush position stays pinned
// at the last data LSN and this test times out with lag still ~588.
func TestStreamModeIdleSlotAdvances_Repro(t *testing.T) {
	const (
		dataTxLSN     = pglogrepl.LSN(12)  // the tx that actually touched our table
		receivedFinal = pglogrepl.LSN(600) // where unrelated WAL/keepalives carry us
	)

	steps := []replStep{
		{batch: nil, hadActivity: true, lsn: 10},                   // BEGIN
		{batch: nil, hadActivity: true, lsn: 11},                   // INSERT
		{batch: makeRowBatch(1), hadActivity: true, lsn: dataTxLSN}, // COMMIT -> 1 row at LSN 12
		{batch: nil, hadActivity: false, lsn: 0},                   // idle: flush the row, confirm LSN 12
		{batch: nil, hadActivity: true, lsn: 500},                  // keepalive: other tables' WAL
		{batch: nil, hadActivity: false, lsn: 0},                   // idle: nothing for us at LSN 500
		{batch: nil, hadActivity: true, lsn: receivedFinal},        // keepalive: more unrelated WAL
		{batch: nil, hadActivity: false, lsn: 0},                   // idle: nothing for us at LSN 600
	}

	src := NewPostgresCDCSource()
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, 64)
	accum := newBatchAccumulator(10000) // high threshold: only the idle path flushes

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = streamLoop(ctx, repl, ModeStream, 0, 10000, accum, results, true)
		close(results)
	}()

	// Pipeline stand-in: confirm every CommitToken via the real CommitStream,
	// which advances the real streamPosition the standby update reads from.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for res := range results {
			if res.Batch != nil {
				res.Batch.Release()
			}
			if res.CommitToken != nil {
				if err := src.CommitStream(ctx, res.CommitToken); err != nil {
					t.Errorf("CommitStream: %v", err)
				}
			}
		}
	}()

	// Poll the position we would report as WALFlushPosition until it catches up
	// to the received position, or give up.
	caughtUp := false
	deadline := time.After(5 * time.Second)
pollLoop:
	for {
		flush := standbyUpdate(true, receivedFinal, src.pos.Committed(), dataTxLSN).WALFlushPosition
		if flush >= receivedFinal {
			caughtUp = true
			break
		}
		select {
		case <-deadline:
			t.Logf("TIMEOUT: flush position stuck at %s, received %s, lag %d",
				flush, receivedFinal, uint64(receivedFinal-flush))
			break pollLoop
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	<-loopDone
	<-consumerDone

	committed := src.pos.Committed()
	t.Logf("final flush(committed) LSN = %s, received = %s, lag = %d",
		committed, receivedFinal, uint64(receivedFinal-committed))
	require.True(t, caughtUp,
		"streaming flush position must advance to the received position during idle; "+
			"if this fails the idle CommitToken is not emitted and replica lag grows unbounded")
	assert.Equal(t, receivedFinal, committed, "flush position should equal the received position (lag 0)")
}
