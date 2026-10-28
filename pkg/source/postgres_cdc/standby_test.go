package postgres_cdc

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandbyResumePosition(t *testing.T) {
	position, ok := standbyResumePosition(0x500, false)
	require.True(t, ok)
	assert.Equal(t, pglogrepl.LSN(0x4FF), position, "a transaction committing at the keepalive position has not been sent")

	_, ok = standbyResumePosition(0x500, true)
	assert.False(t, ok, "a keepalive inside a transaction is not a resume point")

	_, ok = standbyResumePosition(0, false)
	assert.False(t, ok)
}

func TestSingleReplicatorStandbyKeepaliveCompletesBatchPastTarget(t *testing.T) {
	repl := &Replicator{
		decoder:       NewDecoder(testStreamSchema(), "public", "t"),
		clientXLogPos: 100,
		barrierTarget: 500,
		standby:       true,
	}

	repl.observeStandbyKeepalive(400)
	assert.False(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(399), repl.CurrentLSN())

	repl.observeStandbyKeepalive(600)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(599), repl.BarrierLSN())
	assert.Equal(t, pglogrepl.LSN(599), repl.CurrentLSN())
}

func TestPrimaryKeepaliveDoesNotMoveSingleReplicator(t *testing.T) {
	repl := &Replicator{
		decoder:       NewDecoder(testStreamSchema(), "public", "t"),
		clientXLogPos: 100,
		barrierTarget: 500,
	}

	repl.observeStandbyKeepalive(600)
	assert.False(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(100), repl.CurrentLSN())
}

func TestPipelinedStandbyBatchIgnoresKeepaliveInsideTransaction(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Schema:      "public",
		Name:        "t",
		Columns:     append([]schema.Column{{Name: "id", DataType: schema.TypeInt32}}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
	conn := &scriptedConn{script: [][]byte{
		xlogCopyData(10, pgoRelationMsg(1, "public", "t")),
		xlogCopyData(11, pgoBeginMsg(300)),
		xlogCopyData(12, pgoInsertMsg(1, "7")),
		keepaliveCopyData(600, false),
		xlogCopyData(300, pgoCommitMsg(300)),
		keepaliveCopyData(700, false),
	}}
	recv := startTestReceiver(t, conn, false, 5)
	repl := &MultiTableReplicator{
		decoder:       NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}}),
		recv:          recv,
		startLSN:      5,
		started:       true,
		clientXLogPos: 5,
		barrierTarget: 500,
		standby:       true,
	}

	var groups []DecodedChanges
	for range 5 {
		decoded, active, err := repl.NextChanges(context.Background())
		require.NoError(t, err)
		require.True(t, active)
		groups = append(groups, decoded...)
		assert.False(t, repl.BarrierReached(), "a keepalive inside a transaction must not complete the batch")
	}
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Changes, 1)

	_, active, err := repl.NextChanges(context.Background())
	require.NoError(t, err)
	require.True(t, active)
	assert.True(t, repl.BarrierReached())
	assert.Equal(t, pglogrepl.LSN(699), repl.BarrierLSN())
}

func TestWALReceiverReplyProbeRequestsKeepalives(t *testing.T) {
	conn := &scriptedConn{}
	recv := startWALReceiverWithProbe(context.Background(), conn.receive, conn.sendStatus, false, 5, newStreamPosition(), newLagState(), newByteBudget(defaultWALBufferBytes), 10*time.Millisecond)
	t.Cleanup(recv.stop)

	require.Eventually(t, func() bool { return len(conn.statusUpdates()) >= 2 }, 5*time.Second, 5*time.Millisecond)
	for _, status := range conn.statusUpdates() {
		assert.True(t, status.ReplyRequested)
	}
}

type keepaliveCheckpointingReplicator struct {
	*fakeReplicator
	standby bool
}

func (r *keepaliveCheckpointingReplicator) CheckpointsOnKeepalive() bool { return r.standby }

func TestBusyStandbyStreamEmitsCommitTokenOnTimer(t *testing.T) {
	repl := &keepaliveCheckpointingReplicator{fakeReplicator: &fakeReplicator{lsn: 0x4FF}, standby: true}
	results := make(chan source.RecordBatchResult, 1)
	accum := testAccumulator(100)

	lastAttempt := time.Now()
	last, err := maybeEmitBusyCommitToken(context.Background(), repl, accum, results, 0, &lastAttempt)
	require.NoError(t, err)
	assert.Zero(t, last, "no token before the interval elapses")
	assert.Empty(t, results)

	lastAttempt = time.Now().Add(-streamHeartbeatInterval)
	last, err = maybeEmitBusyCommitToken(context.Background(), repl, accum, results, 0, &lastAttempt)
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(0x4FF), last)
	require.Len(t, results, 1)
	token, ok := (<-results).CommitToken.(source.CDCStateCommitToken)
	require.True(t, ok)
	assert.Equal(t, pglogrepl.LSN(0x4FF), token.SourceCommitToken)
}

func TestBusyPrimaryStreamDoesNotEmitTimedCommitToken(t *testing.T) {
	repl := &keepaliveCheckpointingReplicator{fakeReplicator: &fakeReplicator{lsn: 0x4FF}}
	results := make(chan source.RecordBatchResult, 1)

	lastAttempt := time.Now().Add(-streamHeartbeatInterval)
	last, err := maybeEmitBusyCommitToken(context.Background(), repl, testAccumulator(100), results, 0, &lastAttempt)
	require.NoError(t, err)
	assert.Zero(t, last)
	assert.Empty(t, results)
}

func TestBatchBarrierActive(t *testing.T) {
	assert.False(t, batchBarrier{}.active())
	assert.True(t, batchBarrier{nonce: "n"}.active())
	assert.True(t, batchBarrier{target: 1}.active())
}
