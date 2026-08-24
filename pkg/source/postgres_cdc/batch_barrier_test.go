package postgres_cdc

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logicalMessageData(transactional bool, lsn pglogrepl.LSN, prefix, content string, xid *uint32) []byte {
	data := []byte{byte(pglogrepl.MessageTypeMessage)}
	if xid != nil {
		data = binary.BigEndian.AppendUint32(data, *xid)
	}
	if transactional {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	data = binary.BigEndian.AppendUint64(data, uint64(lsn))
	data = append(data, prefix...)
	data = append(data, 0)
	data = binary.BigEndian.AppendUint32(data, uint32(len(content)))
	return append(data, content...)
}

func TestSingleReplicatorRecognizesOnlyExactBatchBarrier(t *testing.T) {
	const nonce = "expected-nonce"
	repl := &Replicator{barrierNonce: nonce, clientXLogPos: 10}

	for _, data := range [][]byte{
		logicalMessageData(false, 20, "other-prefix", nonce, nil),
		logicalMessageData(false, 21, batchBarrierPrefix, "other-nonce", nil),
		logicalMessageData(true, 22, batchBarrierPrefix, nonce, nil),
	} {
		handled, err := repl.handleLogicalMessage(data)
		require.NoError(t, err)
		assert.True(t, handled)
		assert.False(t, repl.BarrierReached())
		assert.Equal(t, pglogrepl.LSN(10), repl.CurrentLSN())
	}

	handled, err := repl.handleLogicalMessage(logicalMessageData(false, 30, batchBarrierPrefix, nonce, nil))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), repl.CurrentLSN())
}

func TestMultiTableReplicatorRecognizesV2BatchBarrier(t *testing.T) {
	const nonce = "expected-nonce"
	repl := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder(nil),
		barrierNonce:  nonce,
		clientXLogPos: 10,
		protocolV2:    true,
	}

	handled, err := repl.handleLogicalMessage(logicalMessageData(false, 20, batchBarrierPrefix, "other-nonce", nil))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.False(t, repl.BarrierReached())

	handled, err = repl.handleLogicalMessage(logicalMessageData(false, 30, batchBarrierPrefix, nonce, nil))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), repl.CurrentLSN())
}

func TestMultiTableReplicatorParsesV2MessageInsideStream(t *testing.T) {
	const nonce = "expected-nonce"
	decoder := NewMultiTableDecoder(nil)
	decoder.inStream = true
	repl := &MultiTableReplicator{
		decoder:       decoder,
		barrierNonce:  nonce,
		clientXLogPos: 10,
		protocolV2:    true,
	}
	xid := uint32(42)

	handled, err := repl.handleLogicalMessage(logicalMessageData(false, 30, batchBarrierPrefix, nonce, &xid))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), repl.CurrentLSN())
}

func TestStreamHeartbeatAdvancesDecodedPositionWithoutCompletingBatch(t *testing.T) {
	data := logicalMessageData(false, 30, streamHeartbeatPrefix, "heartbeat", nil)

	single := &Replicator{barrierNonce: "batch", clientXLogPos: 10}
	handled, err := single.handleLogicalMessage(data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.False(t, single.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), single.CurrentLSN())

	multi := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder(nil),
		barrierNonce:  "batch",
		clientXLogPos: 10,
	}
	handled, err = multi.handleLogicalMessage(data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.False(t, multi.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), multi.CurrentLSN())
}

func TestBatchBarrierVersionRequirement(t *testing.T) {
	require.ErrorContains(t, validateBatchBarrierSupport(130000), "requires PostgreSQL 14 or newer")
	require.NoError(t, validateBatchBarrierSupport(140000))
}

// TestBatchCheckpointStaysAtEmittedBarrier is a regression test for a run that
// persisted checkpoint 00008367/1181E504 while its slot sat around 661/...,
// leaving the next run unable to resume and forcing a replacement snapshot.
// The checkpoint used to be max(decoded, emitted), so a decoded position past
// the barrier — the protocol position, which also advances on keepalives and on
// WAL that was never handed to the destination — became the resume point. Only
// the SQL-emitted barrier LSN is known to have everything before it decoded and
// flushed, so it is what a completed batch persists and confirms.
func TestBatchCheckpointStaysAtEmittedBarrier(t *testing.T) {
	const (
		emitted  = pglogrepl.LSN(30)
		startLSN = pglogrepl.LSN(20)
	)

	// The barrier marker is decoded at protocol position 40, past the LSN that
	// pg_logical_emit_message reported for it.
	repl := &fakeReplicator{steps: []replStep{
		{hadActivity: true, lsn: 25, changes: makeInsertChanges(1, 1, 25)},
		{hadActivity: true, lsn: 40, barrier: true},
	}}
	results := make(chan source.RecordBatchResult, 4)
	require.NoError(t, streamLoop(context.Background(), repl, 100, testAccumulator(100), results, false))
	close(results)
	for res := range results {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}
	require.Greater(t, repl.CurrentLSN(), emitted, "the decoded position must run past the barrier for this test to mean anything")

	src := NewPostgresCDCSource()
	src.checkpointBatchBarrier(context.Background(), emitted, startLSN, "slot")

	assert.Equal(t, emitted, src.caughtUp.Committed())
	assert.Equal(t, FormatLSN(emitted), src.CDCState().Position, "the persisted checkpoint must be the emitted barrier, not the decoded position")

	require.NoError(t, src.FinalizeBatch(context.Background()))
	assert.Equal(t, FormatLSN(emitted), src.CDCState().Position)
}

func TestReadersRejectPre14BatchBeforeSnapshot(t *testing.T) {
	src := &PostgresCDCSource{serverVersion: 130000}
	cfg := CDCConfig{}

	single := NewCDCReader(src, "public.t", testStreamSchema(), cfg)
	records, err := single.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, (<-records).Err, "requires PostgreSQL 14 or newer")

	multi := NewMultiTableCDCReader(src, nil, cfg, nil, "")
	multiRecords, err := multi.Read(context.Background(), source.MultiTableReadOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, (<-multiRecords).Err, "requires PostgreSQL 14 or newer")
}
