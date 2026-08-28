package postgres_cdc

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/bruin-data/ingestr/internal/output"
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
		assert.Zero(t, repl.BarrierLSN())
		assert.Equal(t, pglogrepl.LSN(10), repl.CurrentLSN())
	}

	handled, err := repl.handleLogicalMessage(logicalMessageData(false, 30, batchBarrierPrefix, nonce, nil))
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(30), repl.BarrierLSN())
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
	assert.Equal(t, pglogrepl.LSN(30), repl.BarrierLSN())
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
	assert.Equal(t, pglogrepl.LSN(30), repl.BarrierLSN())
	assert.Equal(t, pglogrepl.LSN(30), repl.CurrentLSN())
}

func TestStreamHeartbeatAdvancesDecodedPositionWithoutCompletingBatch(t *testing.T) {
	data := logicalMessageData(false, 30, streamHeartbeatPrefix, "heartbeat", nil)

	single := &Replicator{barrierNonce: "batch", clientXLogPos: 10}
	handled, err := single.handleLogicalMessage(data)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.False(t, single.BarrierReached())
	assert.Zero(t, single.BarrierLSN())
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
	assert.Zero(t, multi.BarrierLSN())
	assert.Equal(t, pglogrepl.LSN(30), multi.CurrentLSN())
}

func TestReplicatorsKeepExactBarrierLSNWhenCurrentLSNIsAhead(t *testing.T) {
	const nonce = "expected-nonce"
	data := logicalMessageData(false, 30, batchBarrierPrefix, nonce, nil)

	single := &Replicator{barrierNonce: nonce, clientXLogPos: 40}
	handled, err := single.handleLogicalMessage(data)
	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, pglogrepl.LSN(30), single.BarrierLSN())
	assert.Equal(t, pglogrepl.LSN(40), single.CurrentLSN())

	multi := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder(nil),
		barrierNonce:  nonce,
		clientXLogPos: 40,
	}
	handled, err = multi.handleLogicalMessage(data)
	require.NoError(t, err)
	require.True(t, handled)
	assert.Equal(t, pglogrepl.LSN(30), multi.BarrierLSN())
	assert.Equal(t, pglogrepl.LSN(40), multi.CurrentLSN())
}

func TestBatchBarrierVersionRequirement(t *testing.T) {
	require.ErrorContains(t, validateBatchBarrierSupport(130000), "requires PostgreSQL 14 or newer")
	require.NoError(t, validateBatchBarrierSupport(140000))
}

func TestBatchCheckpointUsesDecodedBarrierWhenSQLReturnsAuroraVolumeLSN(t *testing.T) {
	sqlResultLSN := parseTestLSN(t, "8368/74C209CD")
	decodedBarrierLSN := parseTestLSN(t, "6A5/ACE93530")
	startLSN := parseTestLSN(t, "6A5/A5E6B1F0")
	const nonce = "expected-nonce"

	repl := &Replicator{barrierNonce: nonce, clientXLogPos: startLSN}
	handled, err := repl.handleLogicalMessage(logicalMessageData(false, decodedBarrierLSN, batchBarrierPrefix, nonce, nil))
	require.NoError(t, err)
	require.True(t, handled)
	require.True(t, repl.BarrierReached())
	require.Equal(t, decodedBarrierLSN, repl.BarrierLSN())

	src := NewPostgresCDCSource()
	src.checkpointBatchBarrier(context.Background(), repl.BarrierLSN(), startLSN, "slot")

	assert.Equal(t, decodedBarrierLSN, src.caughtUp.Committed())
	assert.Equal(t, FormatLSN(decodedBarrierLSN), src.CDCState().Position)
	assert.NotEqual(t, sqlResultLSN, src.caughtUp.Committed())

	require.NoError(t, src.FinalizeBatch(context.Background()))
	assert.Equal(t, FormatLSN(decodedBarrierLSN), src.CDCState().Position)
}

func TestBatchCheckpointUsesDecodedBarrierOnPostgres(t *testing.T) {
	sqlResultLSN := parseTestLSN(t, "0/1000")
	decodedBarrierLSN := parseTestLSN(t, "0/1040")

	src := NewPostgresCDCSource()
	src.checkpointBatchBarrier(context.Background(), decodedBarrierLSN, pglogrepl.LSN(1), "slot")

	assert.Equal(t, FormatLSN(decodedBarrierLSN), src.CDCState().Position)
	assert.NotEqual(t, FormatLSN(sqlResultLSN), src.CDCState().Position)
}

func TestBatchCheckpointStaysAtDecodedBarrierWhenCurrentLSNMovesPastIt(t *testing.T) {
	const (
		decodedBarrierLSN = pglogrepl.LSN(30)
		currentLSN        = pglogrepl.LSN(40)
		startLSN          = pglogrepl.LSN(20)
	)

	repl := &fakeReplicator{steps: []replStep{
		{hadActivity: true, lsn: 25, changes: makeInsertChanges(1, 1, 25)},
		{hadActivity: true, lsn: currentLSN, barrier: true, barrierLSN: decodedBarrierLSN},
	}}
	results := make(chan source.RecordBatchResult, 4)
	require.NoError(t, streamLoop(context.Background(), repl, 100, testAccumulator(100), results, false))
	close(results)
	for res := range results {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}
	require.Equal(t, currentLSN, repl.CurrentLSN())
	require.Equal(t, decodedBarrierLSN, repl.BarrierLSN())

	src := NewPostgresCDCSource()
	src.checkpointBatchBarrier(context.Background(), repl.BarrierLSN(), startLSN, "slot")

	assert.Equal(t, decodedBarrierLSN, src.caughtUp.Committed())
	assert.Equal(t, FormatLSN(decodedBarrierLSN), src.CDCState().Position)
	assert.NotEqual(t, FormatLSN(currentLSN), src.CDCState().Position)
}

func TestLargeBarrierLSNDivergenceWarns(t *testing.T) {
	previousStdout, previousStderr, previousMode := output.Current()
	t.Cleanup(func() { output.Init(previousStdout, previousStderr, previousMode) })

	var stdout bytes.Buffer
	output.Init(&stdout, &bytes.Buffer{}, output.ModeText)
	sqlResultLSN := parseTestLSN(t, "8368/74C209CD")
	decodedBarrierLSN := parseTestLSN(t, "6A5/ACE93530")

	warnIfBarrierLSNsDiverge(sqlResultLSN, decodedBarrierLSN)

	assert.Contains(t, stdout.String(), sqlResultLSN.String())
	assert.Contains(t, stdout.String(), decodedBarrierLSN.String())
	assert.Contains(t, stdout.String(), "using the decoded LSN")
}

func TestNearbyBarrierLSNsDoNotWarn(t *testing.T) {
	previousStdout, previousStderr, previousMode := output.Current()
	t.Cleanup(func() { output.Init(previousStdout, previousStderr, previousMode) })

	var stdout bytes.Buffer
	output.Init(&stdout, &bytes.Buffer{}, output.ModeText)

	warnIfBarrierLSNsDiverge(parseTestLSN(t, "0/1000"), parseTestLSN(t, "0/1040"))

	assert.Empty(t, stdout.String())
}

func parseTestLSN(t *testing.T, raw string) pglogrepl.LSN {
	t.Helper()
	lsn, err := pglogrepl.ParseLSN(raw)
	require.NoError(t, err)
	return lsn
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
