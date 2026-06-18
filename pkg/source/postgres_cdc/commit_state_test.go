package postgres_cdc

import (
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPosition_MonotonicMax(t *testing.T) {
	p := newStreamPosition()
	assert.Equal(t, pglogrepl.LSN(0), p.Committed())

	p.Commit(100)
	assert.Equal(t, pglogrepl.LSN(100), p.Committed())

	// A lower commit must not move it backwards.
	p.Commit(50)
	assert.Equal(t, pglogrepl.LSN(100), p.Committed())

	p.Commit(200)
	assert.Equal(t, pglogrepl.LSN(200), p.Committed())
}

func TestStreamPosition_ConcurrentMax(t *testing.T) {
	p := newStreamPosition()
	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(lsn pglogrepl.LSN) {
			defer wg.Done()
			p.Commit(lsn)
		}(pglogrepl.LSN(i))
	}
	wg.Wait()
	assert.Equal(t, pglogrepl.LSN(100), p.Committed())
}

func TestStandbyUpdate(t *testing.T) {
	tests := []struct {
		name        string
		streaming   bool
		received    pglogrepl.LSN
		committed   pglogrepl.LSN
		start       pglogrepl.LSN
		wantWrite   pglogrepl.LSN
		wantFlush   pglogrepl.LSN
		wantApply   pglogrepl.LSN
		flushIsZero bool
	}{
		{
			name:        "batch mode reports only write position",
			streaming:   false,
			received:    500,
			committed:   300,
			start:       100,
			wantWrite:   500,
			flushIsZero: true, // pglogrepl defaults flush:=write when 0
		},
		{
			name:      "streaming reports committed as flush/apply",
			streaming: true,
			received:  500,
			committed: 300,
			start:     100,
			wantWrite: 500,
			wantFlush: 300,
			wantApply: 300,
		},
		{
			name:      "streaming with no commit yet falls back to start",
			streaming: true,
			received:  500,
			committed: 0,
			start:     100,
			wantWrite: 500,
			wantFlush: 100,
			wantApply: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssu := standbyUpdate(tt.streaming, tt.received, tt.committed, tt.start)
			assert.Equal(t, tt.wantWrite, ssu.WALWritePosition)
			if tt.flushIsZero {
				assert.Equal(t, pglogrepl.LSN(0), ssu.WALFlushPosition, "flush must stay 0 in batch mode so behavior is unchanged")
				return
			}
			assert.Equal(t, tt.wantFlush, ssu.WALFlushPosition)
			assert.Equal(t, tt.wantApply, ssu.WALApplyPosition)
		})
	}
}

func TestSafeCommitLSN(t *testing.T) {
	tests := []struct {
		name         string
		replLowWater func() (pglogrepl.LSN, bool)
		accumBatches []struct{ lsn pglogrepl.LSN } // batches added to a single table
		currentLSN   pglogrepl.LSN
		flushAccum   bool // flush the accumulator before computing (drains it)
		want         pglogrepl.LSN
	}{
		{
			name:         "fully drained returns current received LSN",
			replLowWater: func() (pglogrepl.LSN, bool) { return 0, false },
			currentLSN:   500,
			want:         500,
		},
		{
			name:         "replicator pending caps below its low water",
			replLowWater: func() (pglogrepl.LSN, bool) { return 200, true },
			currentLSN:   500,
			want:         199,
		},
		{
			name:         "accumulator pending caps below its min LSN",
			replLowWater: func() (pglogrepl.LSN, bool) { return 0, false },
			accumBatches: []struct{ lsn pglogrepl.LSN }{{100}, {200}},
			currentLSN:   500,
			want:         99,
		},
		{
			name:         "min of replicator and accumulator pending wins",
			replLowWater: func() (pglogrepl.LSN, bool) { return 300, true },
			accumBatches: []struct{ lsn pglogrepl.LSN }{{150}},
			currentLSN:   500,
			want:         149,
		},
		{
			name:         "replicator pending lower than accumulator wins",
			replLowWater: func() (pglogrepl.LSN, bool) { return 80, true },
			accumBatches: []struct{ lsn pglogrepl.LSN }{{150}},
			currentLSN:   500,
			want:         79,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repl := &fakeReplicator{lsn: tt.currentLSN, pendingLowWater: tt.replLowWater}
			accum := newBatchAccumulator(10000)
			for _, b := range tt.accumBatches {
				accum.add("t", makeRowBatch(1), b.lsn)
			}
			defer accum.flushAll(make(chan source.RecordBatchResult, 16), nil)

			got := safeCommitLSN(repl, accum)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestStreamLoopCumulativeTokens verifies that when a small transaction is
// parked in the accumulator (below the flush threshold) while a larger one
// flushes, the emitted token never claims the parked transaction is durable.
func TestStreamLoopCumulativeTokens(t *testing.T) {
	// Threshold 3 rows: table "small" gets a 1-row txn at LSN 100 (stays
	// buffered); table "big" gets a 3-row txn at LSN 200 (flushes immediately).
	accum := newBatchAccumulator(3)
	results := make(chan source.RecordBatchResult, 16)

	repl := &fakeReplicator{lsn: 200}
	token := func() any { return safeCommitLSN(repl, accum) }

	accum.add("small", makeRowBatch(1), 100)
	accum.add("big", makeNRowBatch(3), 200)

	accum.flushReady(results, token)
	close(results)

	var bigToken pglogrepl.LSN
	sawBig := false
	for res := range results {
		require.NoError(t, res.Err)
		if res.TableName == "big" {
			sawBig = true
			lsn, ok := res.CommitToken.(pglogrepl.LSN)
			require.True(t, ok, "expected an LSN commit token")
			bigToken = lsn
		}
		res.Batch.Release()
	}

	require.True(t, sawBig, "big table should have flushed")
	// The parked "small" txn is at LSN 100, so the token must be below 100.
	assert.Less(t, bigToken, pglogrepl.LSN(100), "token must not claim the parked LSN-100 txn is durable")
	assert.Equal(t, pglogrepl.LSN(99), bigToken)
}
