package postgres_cdc

import (
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
)

// deadConnectionTimeout bounds how long the replicator tolerates total silence
// from the server before declaring the replication connection dead. Postgres
// sends keepalives roughly every wal_sender_timeout/2 (default 30s), so a
// prolonged gap with no data and no keepalives indicates a broken or half-open
// connection (e.g. the server terminated the wal sender). Surfacing it as an
// error lets the stream exit so a supervisor can restart it, rather than
// spinning forever on a 100ms read timeout that masks the dead socket.
const deadConnectionTimeout = 2 * time.Minute

// receiveTimeout bounds a single ReceiveMessage call. It governs how quickly the
// loop reacts to context cancellation and flushes idle batches. It must not be
// too small: churning the replication connection's read deadline every few
// milliseconds races with pgconn's background reader and can wedge the
// connection so the server stops delivering WAL. One second keeps the loop
// responsive while leaving the deadline stable enough to avoid that race.
const receiveTimeout = 1 * time.Second

// streamPosition holds the LSN the pipeline has confirmed durable (flushed and
// merged into the destination). The pipeline goroutine advances it via
// CommitStream; the replicator goroutine reads it when sending standby status
// updates, so that the slot's confirmed_flush_lsn only moves once data is
// durable. A monotonic CAS-max keeps it safe across goroutines and tolerates
// out-of-order or stale commits.
type streamPosition struct {
	committed atomic.Uint64
}

func newStreamPosition() *streamPosition {
	return &streamPosition{}
}

func (p *streamPosition) Commit(lsn pglogrepl.LSN) {
	for {
		cur := p.committed.Load()
		if uint64(lsn) <= cur {
			return
		}
		if p.committed.CompareAndSwap(cur, uint64(lsn)) {
			return
		}
	}
}

func (p *streamPosition) Committed() pglogrepl.LSN {
	return pglogrepl.LSN(p.committed.Load())
}

// lowWaterReporter exposes the in-flight position state needed to compute a
// safe commit LSN. Both the single- and multi-table replicators implement it.
type lowWaterReporter interface {
	// PendingLowWater returns the lowest LSN of any change the replicator has
	// received but not yet handed to the accumulator (buffered batches from a
	// multi-row transaction, plus an in-flight transaction whose COMMIT has not
	// arrived). The bool is false when nothing is pending inside the replicator.
	PendingLowWater() (pglogrepl.LSN, bool)
	CurrentLSN() pglogrepl.LSN
}

// safeCommitLSN computes the highest LSN below which every change has already
// been emitted into the results channel, so confirming it to the slot cannot
// discard WAL the destination has not seen. Un-emitted data may sit in the
// replicator (PendingLowWater) or the accumulator (minPendingLSN); the safe
// position is one below the oldest of those. When nothing is pending anywhere,
// every received change has been emitted and the current received position is
// safe.
func safeCommitLSN(repl lowWaterReporter, accum *batchAccumulator) pglogrepl.LSN {
	low, pending := repl.PendingLowWater()
	if l, ok := accum.minPendingLSN(); ok {
		if !pending || l < low {
			low = l
		}
		pending = true
	}
	if !pending {
		return repl.CurrentLSN()
	}
	if low == 0 {
		return 0
	}
	return low - 1
}

// standbyUpdate computes the standby status positions to report to Postgres.
// In streaming mode the flushed/applied positions track the confirmed durable
// LSN (committed) rather than the received position, so the slot only advances
// once data is durable. committed==0 (nothing confirmed yet) falls back to the
// replication start LSN so pglogrepl does not default the flush position to the
// received position.
func standbyUpdate(streaming bool, received, committed, start pglogrepl.LSN) pglogrepl.StandbyStatusUpdate {
	ssu := pglogrepl.StandbyStatusUpdate{WALWritePosition: received}
	if !streaming {
		return ssu
	}
	flush := committed
	if flush == 0 {
		flush = start
	}
	ssu.WALFlushPosition = flush
	ssu.WALApplyPosition = flush
	return ssu
}
