package postgres_cdc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readerTestLease struct {
	done chan struct{}
	err  error
}

func (l *readerTestLease) Done() <-chan struct{} { return l.done }
func (l *readerTestLease) Err() error            { return l.err }
func (l *readerTestLease) Release() error        { return nil }

type leaseLossReplicator struct {
	lease *readerTestLease
	calls int
}

func (r *leaseLossReplicator) NextChanges(context.Context) ([]Change, pglogrepl.LSN, bool, error) {
	r.calls++
	if r.calls == 1 {
		return makeInsertChanges(1, 1, 10), 10, true, nil
	}
	close(r.lease.done)
	return nil, 0, false, errors.New("replication stopped")
}

func (r *leaseLossReplicator) CurrentLSN() pglogrepl.LSN { return 10 }
func (r *leaseLossReplicator) BarrierReached() bool      { return false }
func (r *leaseLossReplicator) PendingLowWater() (pglogrepl.LSN, bool) {
	return 0, false
}

// replStep is one NextChanges result in a scripted WAL stream.
type replStep struct {
	changes     []Change
	hadActivity bool
	lsn         pglogrepl.LSN
	barrier     bool
}

// fakeReplicator replays a scripted sequence of NextChanges results, mimicking
// the WAL message stream a real Replicator produces (Begin/Insert/Relation
// messages return no changes but are still activity; only Commit yields the
// transaction's changes).
type fakeReplicator struct {
	steps       []replStep
	idx         int
	lsn         pglogrepl.LSN
	barrierSeen bool

	// pendingLowWater, when set, scripts PendingLowWater's return value.
	pendingLowWater func() (pglogrepl.LSN, bool)
}

func (f *fakeReplicator) NextChanges(_ context.Context) ([]Change, pglogrepl.LSN, bool, error) {
	if f.idx >= len(f.steps) {
		// Stream exhausted: report idle with no further LSN progress.
		return nil, 0, false, nil
	}
	s := f.steps[f.idx]
	f.idx++
	if s.lsn > f.lsn {
		f.lsn = s.lsn
	}
	if s.barrier {
		f.barrierSeen = true
	}
	return s.changes, s.lsn, s.hadActivity, nil
}

func (f *fakeReplicator) CurrentLSN() pglogrepl.LSN { return f.lsn }

func (f *fakeReplicator) BarrierReached() bool { return f.barrierSeen }

func (f *fakeReplicator) PendingLowWater() (pglogrepl.LSN, bool) {
	if f.pendingLowWater != nil {
		return f.pendingLowWater()
	}
	return 0, false
}

// testStreamSchema is the minimal CDC table schema used by the accumulator
// tests: one source column (id) plus the four CDC meta columns.
func testStreamSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Name:        "t",
		Schema:      "public",
		Columns:     append([]schema.Column{{Name: "id", DataType: schema.TypeInt64}}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
}

func testAccumulator(threshold int, tables ...string) *batchAccumulator {
	schemas := map[string]*schema.TableSchema{"": testStreamSchema()}
	for _, tbl := range tables {
		schemas[tbl] = testStreamSchema()
	}
	return newBatchAccumulator(threshold, schemas)
}

// makeInsertChanges builds n INSERT changes with distinct ids starting at base.
func makeInsertChanges(n int, base int64, lsn pglogrepl.LSN) []Change {
	changes := make([]Change, n)
	for i := range n {
		changes[i] = Change{
			Operation: "INSERT",
			LSN:       lsn,
			Values:    []interface{}{base + int64(i)},
		}
	}
	return changes
}

// buildSingleRowTxnStream models n single-row transactions and returns the
// scripted steps plus the target LSN (the LSN of the final commit). Each
// transaction in the pgoutput protocol is Begin -> Insert -> Commit; Begin and
// Insert produce no changes but ARE activity, and only Commit emits the 1-row
// change set. LSNs increase monotonically across every step so batch mode only
// catches up after the very last commit is read.
func buildSingleRowTxnStream(n int) (steps []replStep, finalLSN pglogrepl.LSN) {
	var lsn pglogrepl.LSN
	next := func(changes []Change) replStep {
		lsn++
		return replStep{changes: changes, hadActivity: true, lsn: lsn}
	}
	for i := range n {
		steps = append(
			steps,
			next(nil), // Begin
			next(nil), // Insert
			next(makeInsertChanges(1, int64(i), lsn+1)), // Commit
		)
	}
	lsn++
	steps = append(steps, replStep{hadActivity: true, lsn: lsn, barrier: true})
	return steps, lsn
}

func drainStreamLoop(t *testing.T, steps []replStep) (batchCount int, totalRows int64) {
	t.Helper()

	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, len(steps)+1)
	accum := testAccumulator(10000)

	err := streamLoop(context.Background(), repl, 10000, accum, results, false)
	require.NoError(t, err)
	close(results)

	for res := range results {
		require.NoError(t, res.Err)
		batchCount++
		totalRows += res.Batch.NumRows()
		res.Batch.Release()
	}
	return batchCount, totalRows
}

// TestStreamLoopAccumulatesSingleRowTransactions is a regression test for the
// bug where each single-row WAL transaction was emitted as its own batch
// (batches == rows). With activity-aware idle detection, the per-transaction
// 1-row change sets accumulate and flush as a single merged batch.
func TestStreamLoopAccumulatesSingleRowTransactions(t *testing.T) {
	const numTxns = 50
	steps, _ := buildSingleRowTxnStream(numTxns)

	batchCount, totalRows := drainStreamLoop(t, steps)

	assert.Equal(t, int64(numTxns), totalRows, "all rows should be emitted")
	assert.Equal(t, 1, batchCount, "single-row transactions should merge into one batch, not one batch per row")
	assert.Less(t, batchCount, numTxns, "batch count must not equal row count")
}

func TestBatchStreamLoopEmitsStableKeylessBatchIdentity(t *testing.T) {
	steps, _ := buildSingleRowTxnStream(1)
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, len(steps)+1)
	tableSchema := testStreamSchema()
	tableSchema.PrimaryKeys = nil
	accum := newBatchAccumulator(10000, map[string]*schema.TableSchema{"": tableSchema})
	accum.stableAll = true

	require.NoError(t, streamLoop(t.Context(), repl, 10000, accum, results, false))
	close(results)
	result := <-results
	require.NotNil(t, result.Batch)
	defer result.Batch.Release()
	token, ok := result.CommitToken.(source.CDCStateCommitToken)
	require.True(t, ok)
	require.NotEmpty(t, token.Position)
	require.NotEmpty(t, token.DataBatchID)
}

func TestBatchStreamLoopEmitsStableKeyedBatchIdentity(t *testing.T) {
	steps, _ := buildSingleRowTxnStream(2)
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, len(steps)+1)
	accum := newBatchAccumulator(10000, map[string]*schema.TableSchema{"": testStreamSchema()})
	accum.stableAll = true

	require.NoError(t, streamLoop(t.Context(), repl, 10000, accum, results, false))
	close(results)
	var ids []source.DurableID
	for result := range results {
		require.NotNil(t, result.Batch)
		token, ok := result.CommitToken.(source.CDCStateCommitToken)
		require.True(t, ok)
		require.NotEmpty(t, token.Position)
		require.NotEmpty(t, token.DataBatchID)
		ids = append(ids, token.DataBatchID)
		result.Batch.Release()
	}
	require.Len(t, ids, 2)
	require.NotEqual(t, ids[0], ids[1])
}

func TestStreamLoopLeaseLossDiscardsAccumulatorWithoutMaterializing(t *testing.T) {
	lease := &readerTestLease{done: make(chan struct{}), err: errors.New("lease backend terminated")}
	ctx := source.WithConnectorLeaseGuard(context.Background(), source.NewConnectorLeaseGuard(lease))
	accum := testAccumulator(10000)
	results := make(chan source.RecordBatchResult, 1)

	err := streamLoop(ctx, &leaseLossReplicator{lease: lease}, 10000, accum, results, true)
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	assert.Empty(t, accum.changes)
	assert.Empty(t, accum.minLSN)
	assert.Empty(t, results, "lease loss must not materialize buffered changes into Arrow")
}

func TestAccumulatorLeaseLossWhileSendingReleasesMaterializedBatch(t *testing.T) {
	lease := &readerTestLease{done: make(chan struct{}), err: errors.New("lease backend terminated")}
	ctx := source.WithConnectorLeaseGuard(context.Background(), source.NewConnectorLeaseGuard(lease))
	accum := testAccumulator(10000)
	accum.add("", makeInsertChanges(1, 1, 10), 10)
	results := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- accum.flushAllContext(ctx, results, nil) }()

	time.Sleep(20 * time.Millisecond)
	close(lease.done)
	require.ErrorIs(t, <-done, source.ErrConnectorLeaseLost)
	assert.Empty(t, accum.changes)
	assert.Empty(t, accum.minLSN)
}

func TestCommitStreamRejectsLeaseLoss(t *testing.T) {
	lease := &readerTestLease{done: make(chan struct{}), err: errors.New("lease backend terminated")}
	ctx := source.WithConnectorLeaseGuard(context.Background(), source.NewConnectorLeaseGuard(lease))
	src := &PostgresCDCSource{pos: newStreamPosition()}
	close(lease.done)

	err := src.CommitStream(ctx, pglogrepl.LSN(42))
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	assert.Zero(t, src.pos.Committed())
}

// TestStreamLoopEmitsIdleCommitToken is a regression test for the streaming
// lag stall: once the stream catches up and goes idle, the loop must emit a
// bare CommitToken (no batch) carrying the caught-up LSN so the pipeline can
// advance the replication slot's confirmed_flush_lsn. Without it, an idle or
// low-traffic stream never advances the slot and replica lag grows unbounded.
func TestStreamLoopEmitsIdleCommitToken(t *testing.T) {
	steps, finalLSN := buildSingleRowTxnStream(3)
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, 64)
	accum := testAccumulator(10000)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// Streaming ignores the scripted barrier and runs until cancellation.
		done <- streamLoop(ctx, repl, 10000, accum, results, true)
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
				stateToken, ok := res.CommitToken.(source.CDCStateCommitToken)
				require.True(t, ok, "expected a CDC state commit token")
				lsn, ok := stateToken.SourceCommitToken.(pglogrepl.LSN)
				require.True(t, ok, "expected an LSN source commit token")
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
	assert.Equal(t, finalLSN, idleToken, "idle token must equal the decoded LSN")
}

func TestStreamModeKeepaliveHeadNeverBecomesCommitToken(t *testing.T) {
	const (
		dataTxLSN = pglogrepl.LSN(12) // the tx that actually touched our table
	)

	steps := []replStep{
		{changes: nil, hadActivity: true, lsn: 10},                                       // BEGIN
		{changes: nil, hadActivity: true, lsn: 11},                                       // INSERT
		{changes: makeInsertChanges(1, 1, dataTxLSN), hadActivity: true, lsn: dataTxLSN}, // COMMIT -> 1 row at LSN 12
		{changes: nil, hadActivity: false, lsn: 0},                                       // idle: flush the row, confirm LSN 12
		{changes: nil, hadActivity: true},                                                // keepalive: server WAL head only
		{changes: nil, hadActivity: false, lsn: 0},                                       // idle: nothing for us at LSN 500
		{changes: nil, hadActivity: true},                                                // keepalive: more server WAL
		{changes: nil, hadActivity: false, lsn: 0},                                       // idle: nothing for us at LSN 600
	}

	src := NewPostgresCDCSource()
	repl := &fakeReplicator{steps: steps}
	results := make(chan source.RecordBatchResult, 64)
	accum := testAccumulator(10000) // high threshold: only the idle path flushes

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = streamLoop(ctx, repl, 10000, accum, results, true)
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

	require.Eventually(t, func() bool { return src.pos.Committed() == dataTxLSN }, 5*time.Second, 20*time.Millisecond)
	// The scripted keepalive/idle pairs each exercise the loop's idle delay.
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-loopDone
	<-consumerDone

	assert.Equal(t, dataTxLSN, repl.CurrentLSN())
	assert.Equal(t, dataTxLSN, src.pos.Committed(), "server WAL head must never become durable decoded progress")
}

func TestBatchWaitsForBarrierAfterKeepaliveAndDelayedChanges(t *testing.T) {
	repl := &fakeReplicator{steps: []replStep{
		{hadActivity: true}, // server-head keepalive must not terminate the batch
		{hadActivity: false},
		{hadActivity: true, lsn: 25, changes: makeInsertChanges(1, 7, 25)},
		{hadActivity: true, lsn: 30, barrier: true},
	}}
	results := make(chan source.RecordBatchResult, 1)
	err := streamLoop(context.Background(), repl, 100, testAccumulator(100), results, false)
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(30), repl.CurrentLSN())
	res := <-results
	require.NotNil(t, res.Batch)
	assert.Equal(t, int64(1), res.Batch.NumRows())
	res.Batch.Release()
}
