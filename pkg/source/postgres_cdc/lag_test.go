package postgres_cdc

import (
	"testing"

	"github.com/jackc/pglogrepl"
)

func TestReplicationLagNotStreaming(t *testing.T) {
	s := NewPostgresCDCSource()
	s.lag.observe(pglogrepl.LSN(5000), pglogrepl.LSN(5000))

	// Batch runs never call CommitStream, so pos stays 0 and reporting
	// serverHead-0 would claim the whole WAL as lag.
	if _, ok := s.ReplicationLag(); ok {
		t.Fatal("expected no lag snapshot outside streaming mode")
	}
}

func TestReplicationLagNoServerPositionYet(t *testing.T) {
	s := NewPostgresCDCSource()
	s.lag.streaming.Store(true)

	if _, ok := s.ReplicationLag(); ok {
		t.Fatal("expected no lag snapshot before any server position is observed")
	}
}

func TestReplicationLagBytesBehind(t *testing.T) {
	s := NewPostgresCDCSource()
	s.lag.streaming.Store(true)
	s.lag.observe(pglogrepl.LSN(9000), pglogrepl.LSN(9000))
	s.pos.Commit(pglogrepl.LSN(8000))

	snap, ok := s.ReplicationLag()
	if !ok {
		t.Fatal("expected a lag snapshot")
	}
	if snap.BytesBehind == nil || *snap.BytesBehind != 1000 {
		t.Fatalf("expected 1000 bytes behind, got %v", snap.BytesBehind)
	}
	if snap.CaughtUp {
		t.Fatal("expected caught_up=false while behind")
	}
	if snap.SecondsBehind != nil {
		t.Fatal("postgres cannot express seconds behind; it must be omitted")
	}
	if snap.ServerPosition != pglogrepl.LSN(9000).String() {
		t.Fatalf("unexpected server position %q", snap.ServerPosition)
	}
}

func TestReplicationLagCaughtUp(t *testing.T) {
	s := NewPostgresCDCSource()
	s.lag.streaming.Store(true)
	s.lag.observe(pglogrepl.LSN(9000), pglogrepl.LSN(9000))
	s.pos.Commit(pglogrepl.LSN(9000))

	snap, ok := s.ReplicationLag()
	if !ok {
		t.Fatal("expected a lag snapshot")
	}
	if *snap.BytesBehind != 0 || !snap.CaughtUp {
		t.Fatalf("expected caught up with 0 bytes behind, got %d / %v", *snap.BytesBehind, snap.CaughtUp)
	}
}

// LSN is a uint64: a committed position ahead of the observed head must clamp
// to zero rather than wrapping to ~1.8e19.
func TestReplicationLagCommittedAheadOfHeadSaturates(t *testing.T) {
	s := NewPostgresCDCSource()
	s.lag.streaming.Store(true)
	s.lag.observe(pglogrepl.LSN(100), pglogrepl.LSN(100))
	s.pos.Commit(pglogrepl.LSN(500))

	snap, ok := s.ReplicationLag()
	if !ok {
		t.Fatal("expected a lag snapshot")
	}
	if *snap.BytesBehind != 0 {
		t.Fatalf("expected saturating subtraction to yield 0, got %d", *snap.BytesBehind)
	}
	if !snap.CaughtUp {
		t.Fatal("expected caught_up=true when committed is ahead of the head")
	}
}

// A replicator rebuilt on reconnect restarts at an older position; the head
// must not follow it backwards.
func TestLagStateObserveIsMonotonic(t *testing.T) {
	l := newLagState()
	l.observe(pglogrepl.LSN(9000), pglogrepl.LSN(8000))
	l.observe(pglogrepl.LSN(500), pglogrepl.LSN(400))

	if got := l.serverHead.Load(); got != 9000 {
		t.Fatalf("expected stale server head to be ignored, got %d", got)
	}
	if got := l.received.Load(); got != 8000 {
		t.Fatalf("expected stale received position to be ignored, got %d", got)
	}
}
