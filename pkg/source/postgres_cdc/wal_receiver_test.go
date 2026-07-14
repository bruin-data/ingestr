package postgres_cdc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xlogCopyData wraps a pgoutput payload in an XLogData CopyData message.
func xlogCopyData(walStart uint64, payload []byte) []byte {
	data := []byte{pglogrepl.XLogDataByteID}
	data = binary.BigEndian.AppendUint64(data, walStart)
	data = binary.BigEndian.AppendUint64(data, walStart)
	data = binary.BigEndian.AppendUint64(data, 0)
	return append(data, payload...)
}

func keepaliveCopyData(serverWALEnd uint64, replyRequested bool) []byte {
	data := []byte{pglogrepl.PrimaryKeepaliveMessageByteID}
	data = binary.BigEndian.AppendUint64(data, serverWALEnd)
	data = binary.BigEndian.AppendUint64(data, 0)
	if replyRequested {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	return data
}

// scriptedConn replays raw CopyData payloads through a shared, reused buffer —
// mimicking pgconn, whose returned message is only valid until the next
// receive. After the script is exhausted it blocks until the context ends,
// or returns errAfter if set.
type scriptedConn struct {
	script   [][]byte
	errAfter error
	idx      int
	buf      []byte

	mu     sync.Mutex
	status []pglogrepl.StandbyStatusUpdate
}

func (c *scriptedConn) receive(ctx context.Context) (pgproto3.BackendMessage, error) {
	if c.idx >= len(c.script) {
		if c.errAfter != nil {
			return nil, c.errAfter
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	msg := c.script[c.idx]
	c.idx++
	// Reuse one buffer across calls so any aliasing in the receiver shows up
	// as corrupted earlier messages.
	c.buf = append(c.buf[:0], msg...)
	return &pgproto3.CopyData{Data: c.buf}, nil
}

func (c *scriptedConn) sendStatus(_ context.Context, ssu pglogrepl.StandbyStatusUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = append(c.status, ssu)
	return nil
}

func (c *scriptedConn) statusUpdates() []pglogrepl.StandbyStatusUpdate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]pglogrepl.StandbyStatusUpdate(nil), c.status...)
}

func startTestReceiver(t *testing.T, conn *scriptedConn, streaming bool, startLSN pglogrepl.LSN) *walReceiver {
	t.Helper()
	recv := startWALReceiverFuncs(context.Background(), conn.receive, conn.sendStatus, streaming, startLSN, newStreamPosition(), newLagState())
	t.Cleanup(recv.stop)
	return recv
}

func collectMessages(t *testing.T, recv *walReceiver, n int) []walMessage {
	t.Helper()
	out := make([]walMessage, 0, n)
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case m := <-recv.msgs:
			out = append(out, m)
		case <-deadline:
			t.Fatalf("timed out waiting for %d messages, got %d", n, len(out))
		}
	}
	return out
}

// The receiver must deliver messages in arrival order and must copy XLogData
// payloads out of the connection's reused read buffer — otherwise buffered
// messages get corrupted while the socket keeps draining.
func TestWALReceiverOrderingAndBufferSafety(t *testing.T) {
	conn := &scriptedConn{script: [][]byte{
		xlogCopyData(10, []byte("payload-aaaaaaaa")),
		keepaliveCopyData(15, false),
		xlogCopyData(20, []byte("payload-bbbbbbbb")),
		xlogCopyData(30, []byte("payload-cccccccc")),
	}}
	recv := startTestReceiver(t, conn, false, 5)

	msgs := collectMessages(t, recv, 4)

	require.NotNil(t, msgs[0].data)
	assert.Equal(t, pglogrepl.LSN(10), msgs[0].walStart)
	assert.Equal(t, "payload-aaaaaaaa", string(msgs[0].data))

	assert.Nil(t, msgs[1].data, "keepalive travels through the same channel")
	assert.Equal(t, pglogrepl.LSN(15), msgs[1].serverWALEnd)

	assert.Equal(t, "payload-bbbbbbbb", string(msgs[2].data))
	assert.Equal(t, "payload-cccccccc", string(msgs[3].data))

	assert.Equal(t, pglogrepl.LSN(30), recv.receivedLSN(), "keepalive server head is not received XLogData")
}

// A keepalive with reply-requested must be answered immediately by the
// receiver itself; the decoder may be busy for longer than the server's
// timeout.
func TestWALReceiverAnswersReplyRequested(t *testing.T) {
	conn := &scriptedConn{script: [][]byte{
		keepaliveCopyData(100, true),
	}}
	recv := startTestReceiver(t, conn, false, 5)
	collectMessages(t, recv, 1)

	require.Eventually(t, func() bool { return len(conn.statusUpdates()) >= 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, pglogrepl.LSN(5), conn.statusUpdates()[0].WALWritePosition,
		"keepalive server head must not become the received position")
	assert.Equal(t, pglogrepl.LSN(5), conn.statusUpdates()[0].WALFlushPosition,
		"batch status must not flush WAL received during the current run")
	assert.Equal(t, pglogrepl.LSN(5), conn.statusUpdates()[0].WALApplyPosition,
		"batch status must not apply WAL received during the current run")
}

// Buffered messages must be drained before a receiver failure is surfaced, so
// no received WAL is dropped ahead of the error.
func TestNextMessageDrainsBufferedBeforeError(t *testing.T) {
	hardErr := errors.New("connection reset")
	conn := &scriptedConn{
		script: [][]byte{
			xlogCopyData(10, []byte("m1")),
			xlogCopyData(20, []byte("m2")),
		},
		errAfter: hardErr,
	}
	recv := startTestReceiver(t, conn, false, 5)
	<-recv.done // receiver has failed; both messages are buffered

	r := &MultiTableReplicator{recv: recv, started: true}

	m, ok, err := r.nextMessage(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "m1", string(m.data))

	m, ok, err = r.nextMessage(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "m2", string(m.data))

	_, ok, err = r.nextMessage(context.Background())
	assert.False(t, ok)
	require.ErrorIs(t, err, hardErr)

	// The error must persist across calls.
	_, _, err = r.nextMessage(context.Background())
	require.ErrorIs(t, err, hardErr)
}

// End-to-end through the pipelined replicator: pgoutput messages received on
// the connection come out of NextChanges as decoded per-table change groups,
// and the decode-side position (CurrentLSN) advances only as messages are
// consumed — never ahead to WAL that is received but not yet decoded.
func TestPipelinedReplicatorDecodesAndTracksPosition(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "t",
		Schema: "public",
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}

	conn := &scriptedConn{script: [][]byte{
		xlogCopyData(10, pgoRelationMsg(1, "public", "t")),
		xlogCopyData(11, pgoBeginMsg(300)),
		xlogCopyData(12, pgoInsertMsg(1, "7")),
		xlogCopyData(300, pgoCommitMsg(300)),
		keepaliveCopyData(500, false),
	}}
	recv := startTestReceiver(t, conn, false, 5)

	r := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}}),
		recv:          recv,
		started:       true,
		clientXLogPos: 5,
	}

	// Give the receiver time to buffer everything: the position must still be
	// at the start because nothing has been consumed yet.
	require.Eventually(t, func() bool { return recv.receivedLSN() == 300 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, pglogrepl.LSN(5), r.CurrentLSN(), "processed position must not advance on receive")

	var groups []DecodedChanges
	for i := 0; i < 4; i++ {
		g, hadActivity, err := r.NextChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hadActivity)
		groups = append(groups, g...)
	}

	require.Len(t, groups, 1)
	assert.Equal(t, "t", groups[0].TableName)
	assert.Equal(t, pglogrepl.LSN(300), groups[0].LSN)
	require.Len(t, groups[0].Changes, 1)
	assert.Equal(t, int32(7), groups[0].Changes[0].Values[0])
	assert.Equal(t, pglogrepl.LSN(300), r.CurrentLSN(), "position advances with consumed commits")

	// Consuming the keepalive only updates lag in the receiver.
	_, hadActivity, err := r.NextChanges(context.Background())
	require.NoError(t, err)
	require.True(t, hadActivity)
	assert.Equal(t, pglogrepl.LSN(300), r.CurrentLSN())
	assert.False(t, r.BarrierReached())
}

func TestPipelinedReplicatorCheckpointsLogicalCommitLSN(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name: "events",
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}, cdcMetaColumns()...),
	}
	const commitLSN = 5000
	reader := NewMultiTableCDCReader(nil, []source.SourceTableInfo{{Name: "events", Schema: tableSchema}}, CDCConfig{}, nil, "")
	recv := startTestReceiver(t, &scriptedConn{script: [][]byte{
		xlogCopyData(10, pgoRelationMsg(1, "public", "events")),
		xlogCopyData(11, pgoBeginMsg(commitLSN)),
		xlogCopyData(12, pgoInsertMsg(1, "7")),
		xlogCopyData(13, pgoCommitMsg(commitLSN)),
	}}, true, 5)
	repl := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder([]source.SourceTableInfo{{Name: "events", Schema: tableSchema}}),
		lsnFilter:     reader,
		recv:          recv,
		started:       true,
		clientXLogPos: 5,
	}

	var groups []DecodedChanges
	for range 4 {
		decoded, active, err := repl.NextChanges(context.Background())
		require.NoError(t, err)
		require.True(t, active)
		groups = append(groups, decoded...)
	}
	require.Len(t, groups, 1)
	assert.Equal(t, pglogrepl.LSN(commitLSN), groups[0].LSN)
	assert.Equal(t, pglogrepl.LSN(commitLSN), repl.CurrentLSN())

	resumed := NewMultiTableCDCReader(nil, []source.SourceTableInfo{{Name: "events", Schema: tableSchema}}, CDCConfig{}, map[string]string{"events": FormatLSN(commitLSN)}, "")
	needsSnapshot, _ := resumed.determineResumeStrategy()
	require.False(t, needsSnapshot)
	assert.True(t, resumed.ShouldFilterChange("events", commitLSN), "keyless resume must filter the last durable transaction at equality")
}

func TestPipelinedReplicatorKeepsAllKeylessTransactionChunks(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "events",
		Schema: "public",
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}, cdcMetaColumns()...),
	}
	table := source.SourceTableInfo{Name: "events", Schema: tableSchema}
	reader := NewMultiTableCDCReader(nil, []source.SourceTableInfo{table}, CDCConfig{}, nil, "")

	const rows = 2050
	const commitLSN = 5000
	script := make([][]byte, 0, rows+3)
	script = append(
		script,
		xlogCopyData(10, pgoRelationMsg(1, "public", "events")),
		xlogCopyData(11, pgoBeginMsg(commitLSN)),
	)
	for i := 0; i < rows; i++ {
		script = append(script, xlogCopyData(uint64(12+i), pgoInsertMsg(1, fmt.Sprint(i))))
	}
	script = append(script, xlogCopyData(commitLSN, pgoCommitMsg(commitLSN)))

	recv := startTestReceiver(t, &scriptedConn{script: script}, true, 5)
	repl := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder([]source.SourceTableInfo{table}),
		lsnFilter:     reader,
		recv:          recv,
		started:       true,
		clientXLogPos: 5,
	}

	var got []int32
	for range len(script) + 2 {
		groups, hadActivity, err := repl.NextChanges(context.Background())
		require.NoError(t, err)
		require.True(t, hadActivity)
		for _, group := range groups {
			for _, change := range group.Changes {
				got = append(got, change.Values[0].(int32))
			}
		}
		if len(got) == defaultCommittedDrainChanges {
			assert.Zero(t, reader.GetProcessedLSN("events"), "a partial transaction must not advance the processed LSN")
		}
	}

	require.Len(t, got, rows)
	for i, value := range got {
		assert.Equal(t, int32(i), value)
	}
	assert.Equal(t, pglogrepl.LSN(commitLSN), reader.GetProcessedLSN("events"))
}

func TestMultiTableReplicatorFiltersEveryChunkOfResumedKeylessTransaction(t *testing.T) {
	table := source.SourceTableInfo{Name: "events", Schema: &schema.TableSchema{}}
	reader := NewMultiTableCDCReader(nil, []source.SourceTableInfo{table}, CDCConfig{}, map[string]string{"events": "0/1388"}, "")
	needsSnapshot, _ := reader.determineResumeStrategy()
	require.False(t, needsSnapshot)
	repl := &MultiTableReplicator{lsnFilter: reader}
	group := []DecodedChanges{{TableName: "events", LSN: 5000, Changes: []Change{{Operation: "INSERT"}}}}

	assert.Empty(t, repl.filterGroups(group, false))
	assert.Empty(t, repl.filterGroups(group, false))
	assert.Empty(t, repl.filterGroups(group, true))
	assert.Equal(t, pglogrepl.LSN(5000), reader.GetProcessedLSN("events"))
}

func TestMultiTableReplicatorDoesNotAdvanceProcessedLSNOnFinalDrainError(t *testing.T) {
	table := source.SourceTableInfo{Name: "events", Schema: &schema.TableSchema{}}
	reader := NewMultiTableCDCReader(nil, []source.SourceTableInfo{table}, CDCConfig{}, nil, "")
	decoder := NewMultiTableDecoder([]source.SourceTableInfo{table})
	require.NoError(t, decoder.pendingChanges.Close())
	decoder.pendingChanges = newChangeSpoolWithBudget[streamedChange](defaultTransactionMemoryBytes, decoder.memoryBudget, streamedChangeXID)
	decoder.committed = newChangeSpool[streamedChange](1)
	require.NoError(t, decoder.committed.Append(streamedChange{TableChange: TableChange{
		TableName: "events",
		Change:    Change{Operation: "INSERT"},
	}}))
	require.NoError(t, decoder.committed.Seal())
	decoder.committedLSN = 5000
	require.NoError(t, os.Remove(decoder.committed.file.Name()))
	t.Cleanup(func() { _ = decoder.Close() })

	repl := &MultiTableReplicator{decoder: decoder, lsnFilter: reader, started: true}
	groups, hadActivity, err := repl.NextChanges(context.Background())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.True(t, hadActivity)
	assert.Empty(t, groups)
	assert.Zero(t, reader.GetProcessedLSN("events"))
}

func TestMultiTableReplicatorSeparatesWALAndDecoderBudgets(t *testing.T) {
	src := &PostgresCDCSource{serverVersion: 160000, lag: newLagState()}
	repl, err := NewMultiTableReplicator(src, nil, CDCConfig{}, 5, nil, true, "")
	require.NoError(t, err)
	require.NotSame(t, repl.decoderBudget, repl.walBudget)
	require.True(t, repl.decoderBudget.tryReserve(defaultDecoderMemoryBytes))
	t.Cleanup(func() { repl.decoderBudget.release(defaultDecoderMemoryBytes) })

	conn := &scriptedConn{script: [][]byte{xlogCopyData(10, []byte(strings.Repeat("x", 1024)))}}
	recv := startWALReceiverFuncsWithBudget(context.Background(), conn.receive, conn.sendStatus, true, 5, newStreamPosition(), newLagState(), repl.walBudget)
	t.Cleanup(recv.stop)

	select {
	case message := <-recv.msgs:
		defer message.release()
		assert.Len(t, message.data, 1024)
	case <-time.After(time.Second):
		t.Fatal("WAL admission blocked behind the saturated decoder budget")
	}
}

func TestByteBudgetKeepsHeartbeatFlowingWhileAdmissionIsBlocked(t *testing.T) {
	budget := newByteBudget(10)
	require.True(t, budget.tryReserve(10))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	heartbeats := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- budget.reserveWithHeartbeat(ctx, 1, 5*time.Millisecond, func() error {
			select {
			case heartbeats <- struct{}{}:
			default:
			}
			return nil
		})
	}()

	select {
	case <-heartbeats:
	case <-time.After(time.Second):
		t.Fatal("blocked WAL admission stopped standby heartbeat")
	}
	budget.release(10)
	require.NoError(t, <-done)
	budget.release(1)
}

func TestPipelinedReplicatorWaitsForBarrierAfterKeepaliveAndDelayedChanges(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Schema:      "public",
		Name:        "t",
		Columns:     append([]schema.Column{{Name: "id", DataType: schema.TypeInt32}}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
	const nonce = "delayed-barrier"
	markerLSN := pglogrepl.LSN(400)
	conn := &scriptedConn{script: [][]byte{
		keepaliveCopyData(500, false),
		xlogCopyData(10, pgoRelationMsg(1, "public", "t")),
		xlogCopyData(11, pgoBeginMsg(300)),
		xlogCopyData(12, pgoInsertMsg(1, "7")),
		xlogCopyData(300, pgoCommitMsg(300)),
		xlogCopyData(301, logicalMessageData(false, markerLSN, batchBarrierPrefix, nonce, nil)),
	}}
	recv := startTestReceiver(t, conn, false, 5)
	repl := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}}),
		recv:          recv,
		startLSN:      5,
		started:       true,
		clientXLogPos: 5,
		barrierNonce:  nonce,
		protocolV2:    true,
	}

	_, hadActivity, err := repl.NextChanges(context.Background())
	require.NoError(t, err)
	require.True(t, hadActivity)
	assert.False(t, repl.BarrierReached(), "server-head keepalive must not complete a batch")

	var groups []DecodedChanges
	for range 4 {
		decoded, active, err := repl.NextChanges(context.Background())
		require.NoError(t, err)
		require.True(t, active)
		groups = append(groups, decoded...)
		assert.False(t, repl.BarrierReached(), "data before the barrier must be decoded before completion")
	}
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Changes, 1)
	assert.Equal(t, int32(7), groups[0].Changes[0].Values[0])

	_, hadActivity, err = repl.NextChanges(context.Background())
	require.NoError(t, err)
	require.True(t, hadActivity)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, markerLSN, repl.CurrentLSN())
}
