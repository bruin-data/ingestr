package postgres_cdc

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
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

// silenceProbeAfter is how long the server may stay silent before the next
// standby status update requests an explicit reply. The server only sends
// keepalives on its own when the standby has been quiet for
// wal_sender_timeout/2, and we send status every 10s — so a healthy connection
// to a quiet database receives nothing at all, indefinitely. Probing
// distinguishes quiet from dead: a live server answers immediately (resetting
// the silence clock), so deadConnectionTimeout only fires on connections that
// stay silent even when asked to reply.
const silenceProbeAfter = 30 * time.Second

// receiveTimeout bounds a single ReceiveMessage call. It governs how quickly the
// loop reacts to context cancellation and flushes idle batches. It must not be
// too small: churning the replication connection's read deadline every few
// milliseconds races with pgconn's background reader and can wedge the
// connection so the server stops delivering WAL. One second keeps the loop
// responsive while leaving the deadline stable enough to avoid that race.
const receiveTimeout = 1 * time.Second

// streamHeartbeatInterval bounds slot lag while published tables are idle. The
// heartbeat is decoded through pgoutput, so its LSN is an ordered, safe commit
// point rather than the database-wide WAL head reported by a keepalive.
const streamHeartbeatInterval = 10 * time.Second

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

// lagState tracks the server's WAL head so replication lag can be reported as
// serverHead - pos.Committed(), which is what the slot's
// pg_current_wal_lsn() - confirmed_flush_lsn shows. It lives on the source, not
// the replicator: replicators are rebuilt on reconnect and on new-table
// discovery, which would otherwise reset the head. Written by the replication
// goroutine and read by the metrics scraper, so every field is atomic.
type lagState struct {
	serverHead atomic.Uint64
	streaming  atomic.Bool
}

func newLagState() *lagState {
	return &lagState{}
}

func (l *lagState) observe(serverWALEnd pglogrepl.LSN) {
	storeMax(&l.serverHead, uint64(serverWALEnd))
}

func storeMax(dst *atomic.Uint64, v uint64) {
	for {
		cur := dst.Load()
		if v <= cur || dst.CompareAndSwap(cur, v) {
			return
		}
	}
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

// emitIdleCommitToken sends a bare CommitToken (no batch) on results when the
// stream has caught up past lastEmitted. This lets the pipeline confirm the
// replication slot over WAL that produced no rows for this slot. Logical
// heartbeats advance the decoded position across unpublished-table WAL. Without it,
// an idle or low-traffic stream never advances the slot's confirmed_flush_lsn,
// so retained WAL and replica lag grow without bound even though the
// destination is fully caught up. The safe position already excludes any change
// still pending in the replicator or accumulator, and the pipeline writes
// buffered batches durably before committing, so confirming it cannot discard
// un-emitted WAL. Returns the LSN to treat as the new lastEmitted; the caller
// must only call this when streaming. The send respects ctx so a cancelled run
// shutting down cannot wedge the reader on a channel the pipeline stopped draining.
func emitIdleCommitToken(ctx context.Context, repl lowWaterReporter, accum *batchAccumulator, results chan<- source.RecordBatchResult, lastEmitted pglogrepl.LSN) pglogrepl.LSN {
	safe := safeCommitLSN(repl, accum)
	if safe <= lastEmitted {
		return lastEmitted
	}
	if err := sendResult(ctx, results, source.RecordBatchResult{CommitToken: checkpointCommitToken(safe)}); err == nil {
		return safe
	}
	return lastEmitted
}

type streamHeartbeatEmitter interface {
	EmitStreamHeartbeat(ctx context.Context) error
}

func maybeEmitStreamHeartbeat(ctx context.Context, repl any, last time.Time) time.Time {
	now := time.Now()
	if now.Sub(last) < streamHeartbeatInterval {
		return last
	}
	emitter, ok := repl.(streamHeartbeatEmitter)
	if !ok {
		return now
	}
	_ = emitter.EmitStreamHeartbeat(ctx)
	return now
}

func checkpointCommitToken(lsn pglogrepl.LSN) source.CDCStateCommitToken {
	position := ""
	if lsn > 0 {
		position = FormatLSN(lsn)
	}
	return source.CDCStateCommitToken{SourceCommitToken: lsn, Position: position}
}

func snapshotCommitToken(lsn pglogrepl.LSN, snapshots map[string]string) source.CDCStateCommitToken {
	return source.CDCStateCommitToken{
		SourceCommitToken: lsn,
		Position:          FormatLSN(lsn),
		SnapshotPositions: snapshots,
	}
}

// standbyUpdate computes the standby status positions to report to Postgres.
// The flushed/applied positions track the confirmed durable LSN rather than
// the received position, so the slot only advances once data is durable.
// committed==0 falls back to the replication start LSN. Positions must always
// be explicit because pglogrepl replaces zero flush/apply with write.
func standbyUpdate(streaming bool, received, committed, start pglogrepl.LSN) pglogrepl.StandbyStatusUpdate {
	flush := pglogrepl.LSN(0)
	if streaming {
		flush = committed
	}
	if flush == 0 {
		flush = start
	}
	ssu := pglogrepl.StandbyStatusUpdate{WALWritePosition: max(received, flush)}
	ssu.WALFlushPosition = flush
	ssu.WALApplyPosition = flush
	return ssu
}
