package postgres_cdc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeSpoolUsesByteBudgetForLargeRows(t *testing.T) {
	budget := newByteBudget(1024)
	spool := newChangeSpoolWithBudget[Change](1024, budget, nil)
	t.Cleanup(func() { require.NoError(t, spool.Close()) })

	require.NoError(t, spool.Append(Change{Operation: "INSERT", Values: []interface{}{strings.Repeat("x", 4096)}}))
	assert.Zero(t, spool.InMemoryBytes())
	require.NotNil(t, spool.file)
	assert.LessOrEqual(t, budget.Used(), int64(1024))
}

func TestChangeSpoolCloseReportsCleanupFailure(t *testing.T) {
	spool := newChangeSpool[Change](1)
	require.NoError(t, spool.Append(Change{Operation: "INSERT", Values: []interface{}{strings.Repeat("x", 128)}}))
	require.NotNil(t, spool.file)
	require.NoError(t, os.Remove(spool.file.Name()))
	err := spool.Close()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestChangeSpoolsShareAggregateByteBudget(t *testing.T) {
	value := Change{Operation: "INSERT", Values: []interface{}{strings.Repeat("x", 700)}}
	size, err := encodedChangeSize(value)
	require.NoError(t, err)
	budget := newByteBudget(size)
	first := newChangeSpoolWithBudget[Change](size*2, budget, nil)
	second := newChangeSpoolWithBudget[Change](size*2, budget, nil)
	t.Cleanup(func() {
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())
	})

	require.NoError(t, first.Append(value))
	require.NoError(t, second.Append(value))
	assert.Equal(t, size, first.InMemoryBytes())
	assert.Zero(t, second.InMemoryBytes())
	require.NotNil(t, second.file)
}

func TestChangeSpoolBoundsMemoryAndDrainsInOrder(t *testing.T) {
	spool := newChangeSpool[Change](2)
	t.Cleanup(func() { require.NoError(t, spool.Close()) })
	for i := 0; i < 10; i++ {
		require.NoError(t, spool.Append(Change{
			Operation: "UPDATE",
			Values:    []interface{}{int32(i), tupleUnchangedMarker, fmt.Sprintf("value-%d", i)},
		}))
		assert.LessOrEqual(t, spool.InMemoryLen(), 2)
	}
	require.NotNil(t, spool.file)
	path := spool.file.Name()
	require.NoError(t, spool.Seal())

	var got []Change
	for spool.Len() > 0 {
		chunk, err := spool.Drain(3)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(chunk), 3)
		got = append(got, chunk...)
	}
	require.Len(t, got, 10)
	for i, change := range got {
		assert.Equal(t, int32(i), change.Values[0])
		assert.Equal(t, tupleUnchangedMarker, change.Values[1])
	}
	require.NoError(t, spool.Close())
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMultiTableStreamAbortFiltersSpilledSubtransaction(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}}
	decoder := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})
	decoder.streamed[10] = newChangeSpoolWithBudget[streamedChange](1, decoder.memoryBudget, streamedChangeXID)
	t.Cleanup(func() { require.NoError(t, decoder.Close()) })
	for i, xid := range []uint32{10, 11, 10, 11, 10} {
		require.NoError(t, decoder.streamed[10].Append(streamedChange{XID: xid, TableChange: TableChange{
			TableName: "t",
			Change:    Change{Operation: "INSERT", Values: []interface{}{int32(i)}},
		}}))
		assert.LessOrEqual(t, decoder.streamed[10].InMemoryLen(), 1)
	}
	abort := make([]byte, 8)
	putUint32(abort[:4], 10)
	putUint32(abort[4:], 11)
	require.NoError(t, decoder.handleStreamAbort(abort))
	require.Equal(t, 1, decoder.streamed[10].Len())
	path := decoder.streamed[10].file.Name()
	before, err := os.Stat(path)
	require.NoError(t, err)
	for i := 0; i < 1000; i++ {
		require.NoError(t, decoder.handleStreamAbort(abort))
	}
	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, before.Size(), after.Size(), "aborts must not rewrite the spill file")
	require.NoError(t, decoder.streamed[10].Seal())
	kept, err := decoder.streamed[10].Drain(10)
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, uint32(10), kept[0].XID)
}

func TestWALReceiverSharesByteBudgetWithBufferedPayloads(t *testing.T) {
	budget := newByteBudget(1024)
	conn := &scriptedConn{script: [][]byte{
		xlogCopyData(10, []byte(strings.Repeat("a", 800))),
		xlogCopyData(20, []byte(strings.Repeat("b", 800))),
	}}
	recv := startWALReceiverFuncsWithBudget(context.Background(), conn.receive, conn.sendStatus, false, 5, newStreamPosition(), newLagState(), budget)
	t.Cleanup(recv.stop)

	require.Eventually(t, func() bool { return len(recv.msgs) == 1 }, time.Second, time.Millisecond)
	assert.LessOrEqual(t, budget.Used(), int64(1024))
	first := <-recv.msgs
	first.release()
	require.Eventually(t, func() bool { return len(recv.msgs) == 1 }, time.Second, time.Millisecond)
	second := <-recv.msgs
	second.release()
	assert.Equal(t, int64(0), budget.Used())
}

func TestMultiTableStreamCommitDrainsSpilledTransactionInChunks(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}}
	decoder := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})
	decoder.streamed[10] = newChangeSpool[streamedChange](2)
	t.Cleanup(func() { require.NoError(t, decoder.Close()) })
	for i := 0; i < 2050; i++ {
		require.NoError(t, decoder.streamed[10].Append(streamedChange{XID: 10, TableChange: TableChange{
			TableName: "t", Change: Change{Operation: "INSERT", Values: []interface{}{int32(i)}},
		}}))
		assert.LessOrEqual(t, decoder.streamed[10].InMemoryLen(), 2)
	}
	commit := make([]byte, 13)
	putUint32(commit[:4], 10)
	commit[4] = 0
	commitLSN := pglogrepl.LSN(900)
	for i := 0; i < 8; i++ {
		commit[5+i] = byte(uint64(commitLSN) >> (56 - 8*i))
	}
	groups, err := decoder.handleStreamCommit(commit)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Changes, defaultCommittedDrainChanges)
	assert.Equal(t, commitLSN, groups[0].LSN)
	assert.True(t, decoder.HasCommitted())
	groups, err = decoder.DrainCommitted(defaultCommittedDrainChanges)
	require.NoError(t, err)
	require.Len(t, groups[0].Changes, defaultCommittedDrainChanges)
	groups, err = decoder.DrainCommitted(defaultCommittedDrainChanges)
	require.NoError(t, err)
	require.Len(t, groups[0].Changes, 2)
	assert.False(t, decoder.HasCommitted())
}

func TestDecoderCommitDrainsSpilledTransactionInChunks(t *testing.T) {
	decoder := NewDecoder(&schema.TableSchema{}, "public", "t")
	require.NoError(t, decoder.pendingChanges.Close())
	decoder.pendingChanges = newChangeSpool[Change](2)
	t.Cleanup(func() { require.NoError(t, decoder.Close()) })
	decoder.currentTxLSN = pglogrepl.LSN(42)
	for i := 0; i < 2050; i++ {
		require.NoError(t, decoder.pendingChanges.Append(Change{Operation: "INSERT", Values: []interface{}{int32(i)}}))
		assert.LessOrEqual(t, decoder.pendingChanges.InMemoryLen(), 2)
	}
	first, err := decoder.handleCommit()
	require.NoError(t, err)
	require.Len(t, first, defaultCommittedDrainChanges)
	assert.True(t, decoder.HasCommitted())
	chunk, err := decoder.DrainCommitted(defaultCommittedDrainChanges)
	require.NoError(t, err)
	assert.Len(t, chunk, defaultCommittedDrainChanges)
	assert.True(t, decoder.HasCommitted())
	chunk, err = decoder.DrainCommitted(defaultCommittedDrainChanges)
	require.NoError(t, err)
	assert.Len(t, chunk, 2)
	assert.False(t, decoder.HasCommitted())
}

func putUint32(dst []byte, value uint32) {
	dst[0] = byte(value >> 24)
	dst[1] = byte(value >> 16)
	dst[2] = byte(value >> 8)
	dst[3] = byte(value)
}
