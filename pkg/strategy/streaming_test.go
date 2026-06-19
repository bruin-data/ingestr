package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCommitter struct {
	mu     sync.Mutex
	tokens []any
	err    error
}

func (c *fakeCommitter) CommitStream(_ context.Context, token any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens = append(c.tokens, token)
	return c.err
}

func (c *fakeCommitter) committed() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.tokens...)
}

// ctxRecordingDest records the context error observed at each WriteParallel
// call so tests can assert the final flush runs on a non-cancelled context.
type ctxRecordingDest struct {
	*fakeDestination
	mu           sync.Mutex
	writeCtxErrs []error
}

func (d *ctxRecordingDest) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.mu.Lock()
	d.writeCtxErrs = append(d.writeCtxErrs, ctx.Err())
	d.mu.Unlock()
	return d.fakeDestination.WriteParallel(ctx, records, opts)
}

type truncatingDest struct {
	*fakeDestination
	mu            sync.Mutex
	truncateCalls []string
}

func (d *truncatingDest) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.truncateCalls = append(d.truncateCalls, table)
	return nil
}

func streamTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys: []string{"id"},
	}
}

func mergeTableState(name string) *streamTableState {
	return &streamTableState{
		destTable:    name,
		stagingTable: name + "_staging",
		schema:       streamTestSchema(),
		primaryKeys:  []string{"id"},
	}
}

func newTestLoop(dest destination.Destination, opts StreamingOptions, tables map[string]*streamTableState) *flushLoop {
	return newFlushLoop(dest, &config.IngestConfig{ExtractParallelism: 2}, opts, tables)
}

func writeCallCount(d *fakeDestination) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.writeCalls)
}

func TestStreaming_CountTriggerFlushes(t *testing.T) {
	baseDest := &fakeDestination{}
	dest := &truncateCapableDestination{fakeDestination: baseDest}
	committer := &fakeCommitter{}
	loop := newTestLoop(&truncateCapableDestination{fakeDestination: dest}, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	for i := range 3 {
		records <- source.RecordBatchResult{
			Batch:       int64RecordBatch(t, "id", []int64{1, 2, 3, 4}, nil),
			CommitToken: i,
		}
	}
	// 3 batches x 4 rows = 12 rows, threshold 100 not reached yet: no flush.
	assert.Equal(t, 0, writeCallCount(baseDest))

	big := make([]int64, 100)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", big, nil), CommitToken: 3}

	require.Eventually(t, func() bool { return writeCallCount(baseDest) == 1 }, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return len(committer.committed()) == 1 }, 5*time.Second, time.Millisecond)

	// Order: write to staging, merge into dest, reset staging, then commit.
	baseDest.mu.Lock()
	assert.Equal(t, []string{"WriteParallel", "MergeTable", "TruncateTable"}, baseDest.calls)
	assert.Equal(t, "ds.tbl_staging", baseDest.writeCalls[0].Table)
	assert.True(t, baseDest.writeCalls[0].StagingTable)
	assert.Equal(t, "ds.tbl_staging", baseDest.mergeCalls[0].StagingTable)
	assert.Equal(t, "ds.tbl", baseDest.mergeCalls[0].TargetTable)
	baseDest.mu.Unlock()
	// Cumulative token semantics: only the newest token is committed.
	assert.Equal(t, []any{3}, committer.committed())

	cancel()
	close(records)
	require.NoError(t, <-done)
}

func TestStreaming_IntervalTriggerFlushes(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: 20 * time.Millisecond,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil), CommitToken: "t1"}

	require.Eventually(t, func() bool { return writeCallCount(dest) == 1 }, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return len(committer.committed()) == 1 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, []any{"t1"}, committer.committed())

	cancel()
	close(records)
	require.NoError(t, <-done)
	// Nothing was buffered after the flush, so shutdown adds no extra writes.
	assert.Equal(t, 1, writeCallCount(dest))
}

func TestStreaming_EmptyCyclesSkipped(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: 5 * time.Millisecond,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, writeCallCount(dest))
	assert.Empty(t, committer.committed())

	// A token-only result (CDC keepalive advanced the position with no rows)
	// must still be committed so the source can release retention.
	records <- source.RecordBatchResult{CommitToken: "keepalive"}
	require.Eventually(t, func() bool { return len(committer.committed()) == 1 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, 0, writeCallCount(dest))
	assert.Equal(t, []any{"keepalive"}, committer.committed())

	// Token already committed: subsequent ticks stay idle.
	time.Sleep(30 * time.Millisecond)
	assert.Len(t, committer.committed(), 1)

	cancel()
	close(records)
	require.NoError(t, <-done)
}

func TestStreaming_MergeFailureAbortsWithoutCommit(t *testing.T) {
	dest := &fakeDestination{mergeErr: errors.New("merge exploded")}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  2,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	ctx := context.Background()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2, 3}, nil), CommitToken: "t1"}

	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	err := <-done
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge exploded")
	assert.Empty(t, committer.committed())
}

func TestStreaming_SourceErrorAborts(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	records <- source.RecordBatchResult{Err: errors.New("replication slot vanished")}

	err := loop.run(context.Background(), records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replication slot vanished")
}

func TestStreaming_GracefulShutdownFlushesTail(t *testing.T) {
	dest := &ctxRecordingDest{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	ctx, cancel := context.WithCancel(context.Background())
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2, 3}, nil), CommitToken: "tail"}
	cancel()
	// Source reacts to cancellation by flushing its accumulator and closing.
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{4}, nil), CommitToken: "tail2"}
	close(records)

	require.NoError(t, <-done)

	assert.Equal(t, 1, writeCallCount(dest.fakeDestination))
	assert.Equal(t, []any{"tail2"}, committer.committed())

	// The final flush must run on a fresh, non-cancelled context.
	dest.mu.Lock()
	require.Len(t, dest.writeCtxErrs, 1)
	assert.NoError(t, dest.writeCtxErrs[0])
	dest.mu.Unlock()
}

func TestStreaming_ChannelCloseTriggersFinalFlush(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyAppend,
	}, map[string]*streamTableState{"": {destTable: "ds.tbl", schema: streamTestSchema()}})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 1)
	assert.Equal(t, "ds.tbl", dest.writeCalls[0].Table)
	assert.False(t, dest.writeCalls[0].StagingTable)
	assert.Empty(t, dest.mergeCalls)
	dest.mu.Unlock()
}

func TestStreaming_MultiTableRouting(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  4,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{
		"public.users":  mergeTableState("ds.users"),
		"public.orders": mergeTableState("ds.orders"),
	})

	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil), TableName: "public.users"}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{3, 4}, nil), TableName: "public.orders"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.writeCalls, 2)
	writtenTables := []string{dest.writeCalls[0].Table, dest.writeCalls[1].Table}
	assert.ElementsMatch(t, []string{"ds.users_staging", "ds.orders_staging"}, writtenTables)
	require.Len(t, dest.mergeCalls, 2)
	mergedTables := []string{dest.mergeCalls[0].TargetTable, dest.mergeCalls[1].TargetTable}
	assert.ElementsMatch(t, []string{"ds.users", "ds.orders"}, mergedTables)
}

func TestStreaming_UnknownTableBatchDropped(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"public.users": mergeTableState("ds.users")})

	records := make(chan source.RecordBatchResult, 1)
	// The CheckedAllocator cleanup in int64RecordBatch verifies this gets released.
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.unknown"}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))
	assert.Equal(t, 0, writeCallCount(dest))
}

func TestStreaming_TruncateCapableResetsStagingInPlace(t *testing.T) {
	dest := &truncatingDest{fakeDestination: &fakeDestination{}}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"": mergeTableState("ds.tbl")})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	assert.Equal(t, []string{"ds.tbl_staging"}, dest.truncateCalls)
	dest.mu.Unlock()
	dest.fakeDestination.mu.Lock()
	assert.Empty(t, dest.prepareCalls, "truncate-capable destinations must not drop+recreate staging")
	dest.fakeDestination.mu.Unlock()
}

func TestStreaming_CommitFailureAborts(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{err: errors.New("ack failed")}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyAppend,
		Committer:     committer,
	}, map[string]*streamTableState{"": {destTable: "ds.tbl", schema: streamTestSchema()}})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), CommitToken: "t1"}
	close(records)

	err := loop.run(context.Background(), records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ack failed")
}

func TestStreaming_AppendDoesNotEnforcePrimaryKey(t *testing.T) {
	// At-least-once streaming can redeliver the same key, so the append path
	// must create the table without an enforced primary-key constraint.
	dest := &fakeDestination{}
	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyAppend})
	st := &streamTableState{
		destTable: "ds.evt",
		schema: &schema.TableSchema{
			Columns:     []schema.Column{{Name: "msg_id", DataType: schema.TypeString}, {Name: "data", DataType: schema.TypeJSON}},
			PrimaryKeys: []string{"msg_id"},
		},
		primaryKeys: []string{"msg_id"},
	}

	require.NoError(t, exec.prepareTable(context.Background(), dest, &config.IngestConfig{}, st))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.prepareCalls, 1)
	assert.Equal(t, "ds.evt", dest.prepareCalls[0].Table)
	assert.Empty(t, dest.prepareCalls[0].PrimaryKeys, "append must not enforce a primary-key constraint")
	assert.Empty(t, st.stagingTable, "append uses no staging table")
}

func TestStreaming_MergeEnforcesPrimaryKey(t *testing.T) {
	dest := &fakeDestination{}
	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge})
	st := &streamTableState{
		destTable:   "ds.evt",
		schema:      streamTestSchema(),
		primaryKeys: []string{"id"},
	}

	require.NoError(t, exec.prepareTable(context.Background(), dest, &config.IngestConfig{}, st))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	// merge prepares the dest table (with PK) and a staging table.
	require.GreaterOrEqual(t, len(dest.prepareCalls), 2)
	assert.Equal(t, []string{"id"}, dest.prepareCalls[0].PrimaryKeys, "merge must keep the primary key for upserts")
	assert.NotEmpty(t, st.stagingTable)
}

func TestStreaming_MergePassesIncrementalKeyForOrdering(t *testing.T) {
	// Broker streams set an incremental key (e.g. _ingestr_order) so the per-PK
	// dedup keeps the latest record within a flush cycle rather than arbitrary.
	dest := &fakeDestination{}
	st := mergeTableState("ds.tbl")
	st.incrementalKey = "_ingestr_order"
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{"": st})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}
	close(records)
	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	defer dest.mu.Unlock()
	require.Len(t, dest.mergeCalls, 1)
	assert.Equal(t, "_ingestr_order", dest.mergeCalls[0].IncrementalKey, "merge must order dedup by the incremental key")
}

func TestStreaming_CleanupDropsStagingTables(t *testing.T) {
	dest := &fakeDestination{}
	loop := newTestLoop(dest, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{
		"a": mergeTableState("ds.a"),
		"b": {destTable: "ds.b", schema: streamTestSchema()}, // append-style: no staging
	})

	loop.cleanup(context.Background())

	dest.mu.Lock()
	defer dest.mu.Unlock()
	assert.Equal(t, []string{"ds.a_staging"}, dest.dropCalls)
}
