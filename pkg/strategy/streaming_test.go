package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCommitter struct {
	mu     sync.Mutex
	tokens []any
	err    error
}

type fakeLegacySlotFinalizer struct {
	committer *fakeCommitter
	calls     int
	err       error
}

func (f *fakeLegacySlotFinalizer) FinalizeLegacySlot(context.Context) error {
	if len(f.committer.committed()) == 0 {
		return errors.New("legacy slot finalized before source position commit")
	}
	f.calls++
	return f.err
}

type cancelAwareSourceTable struct {
	*fakeSourceTable
	batches []arrow.RecordBatch
	done    chan struct{}
}

func (t *cancelAwareSourceTable) Read(ctx context.Context, _ source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 1)
	go func() {
		defer close(t.done)
		defer close(results)
		for i, batch := range t.batches {
			select {
			case results <- source.RecordBatchResult{Batch: batch}:
			case <-ctx.Done():
				batch.Release()
				for _, remaining := range t.batches[i+1:] {
					remaining.Release()
				}
				return
			}
		}
	}()
	return results, nil
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

type streamingTestLease struct {
	done chan struct{}
	err  error
}

func (l *streamingTestLease) Done() <-chan struct{} { return l.done }
func (l *streamingTestLease) Err() error            { return l.err }
func (l *streamingTestLease) Release() error        { return nil }

func guardedStreamingContext(lease source.ConnectorLease) context.Context {
	return source.WithConnectorLeaseGuard(context.Background(), source.NewConnectorLeaseGuard(lease))
}

type stageBlockingDestination struct {
	*truncateCapableDestination
	stage   string
	entered chan struct{}
	release chan struct{}
}

func (d *stageBlockingDestination) block(stage string) {
	if d.stage != stage {
		return
	}
	close(d.entered)
	<-d.release
}

func (d *stageBlockingDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	err := d.fakeDestination.WriteParallel(ctx, records, opts)
	d.block("write")
	return err
}

func (d *stageBlockingDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	err := d.fakeDestination.MergeTable(ctx, opts)
	d.block("merge")
	return err
}

func (d *stageBlockingDestination) TruncateTable(ctx context.Context, table string) error {
	err := d.truncateCapableDestination.TruncateTable(ctx, table)
	d.block("reset")
	return err
}

type blockingStateDestination struct {
	*cdcStateDestination
	entered chan struct{}
	release chan struct{}
	block   bool
}

func (d *blockingStateDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	err := d.cdcStateDestination.WriteCDCState(ctx, records, opts)
	if d.block {
		close(d.entered)
		<-d.release
	}
	return err
}

func (d *truncatingDest) TruncateTable(_ context.Context, table string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.truncateCalls = append(d.truncateCalls, table)
	return nil
}

func TestStreamingAppliesTruncateBeforeAcknowledgingLaterRows(t *testing.T) {
	base := &fakeDestination{}
	dest := &truncatingDest{fakeDestination: base}
	committer := &fakeCommitter{}
	cfg := config.DefaultConfig()
	cfg.DestTable = "raw.items"
	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), CommitToken: "before"}
	records <- source.RecordBatchResult{Truncate: true, CommitToken: "truncate"}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil), CommitToken: "after"}
	close(records)

	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyAppend,
		Committer:     committer,
	}, map[string]*streamTableState{"": {
		destTable: "raw.items",
		schema:    &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	}})
	if err := loop.run(context.Background(), records); err != nil {
		t.Fatal(err)
	}

	if got := dest.truncateCalls; len(got) != 1 || got[0] != "raw.items" {
		t.Fatalf("truncate calls = %v, want [raw.items]", got)
	}
	if got := committer.committed(); len(got) != 2 || got[0] != "before" || got[1] != "after" {
		t.Fatalf("commit tokens = %v, want [before after]", got)
	}
	if len(base.writeCalls) != 2 {
		t.Fatalf("write calls = %d, want pre- and post-truncate segments", len(base.writeCalls))
	}
}

type capturingDestination struct {
	*fakeDestination
	valuesMu sync.Mutex
	values   []string
}

func (d *capturingDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	fd := d.fakeDestination
	fd.mu.Lock()
	fd.calls = append(fd.calls, "WriteParallel")
	fd.writeCalls = append(fd.writeCalls, opts)
	writeErr := fd.writeErr
	fd.mu.Unlock()

	for result := range records {
		if result.Batch != nil {
			names := result.Batch.Column(1).(*array.String)
			d.valuesMu.Lock()
			d.values = append(d.values, names.Value(0))
			d.valuesMu.Unlock()
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return writeErr
}

type timestampCapturingDest struct {
	*fakeDestination
	mu              sync.Mutex
	writeTimestamps [][]int64
}

func (d *timestampCapturingDest) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.fakeDestination.mu.Lock()
	d.calls = append(d.calls, "WriteParallel")
	d.writeCalls = append(d.writeCalls, opts)
	writeErr := d.writeErr
	d.fakeDestination.mu.Unlock()

	var timestamps []int64
	for result := range records {
		if result.Batch != nil {
			values, err := loadTimestampValues(result.Batch)
			if err != nil {
				result.Batch.Release()
				return err
			}
			timestamps = append(timestamps, values...)
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}

	d.mu.Lock()
	d.writeTimestamps = append(d.writeTimestamps, timestamps)
	d.mu.Unlock()
	return writeErr
}

func (d *timestampCapturingDest) capturedTimestamps() [][]int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([][]int64, len(d.writeTimestamps))
	for i, values := range d.writeTimestamps {
		out[i] = append([]int64(nil), values...)
	}
	return out
}

func streamTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys: []string{"id"},
	}
}

func streamTestSchemaWithLoadTimestamp() *schema.TableSchema {
	s := streamTestSchema()
	result := *s
	result.Columns = append([]schema.Column{}, s.Columns...)
	result.Columns = append(result.Columns, schema.Column{
		Name:     naming.IngestrLoadedAtColumn,
		DataType: schema.TypeTimestampTZ,
		Nullable: true,
	})
	return &result
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

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func loadTimestampValues(batch arrow.RecordBatch) ([]int64, error) {
	idx := -1
	for i := 0; i < int(batch.NumCols()); i++ {
		if strings.EqualFold(batch.ColumnName(i), naming.IngestrLoadedAtColumn) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("missing %s column", naming.IngestrLoadedAtColumn)
	}

	col, ok := batch.Column(idx).(*array.Timestamp)
	if !ok {
		return nil, fmt.Errorf("%s column is %T, want *array.Timestamp", naming.IngestrLoadedAtColumn, batch.Column(idx))
	}

	values := make([]int64, int(batch.NumRows()))
	for row := 0; row < int(batch.NumRows()); row++ {
		values[row] = int64(col.Value(row))
	}
	return values, nil
}

func TestStreaming_CountTriggerFlushes(t *testing.T) {
	baseDest := &fakeDestination{}
	dest := &truncateCapableDestination{fakeDestination: baseDest}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
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

func TestStreaming_LoadTimestampIsSetPerFlushCycle(t *testing.T) {
	dest := &timestampCapturingDest{fakeDestination: &fakeDestination{}}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  4,
		Strategy:      config.StrategyAppend,
	}, map[string]*streamTableState{"": {destTable: "ds.tbl", schema: streamTestSchemaWithLoadTimestamp()}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()

	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)}
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{3, 4}, nil)}

	require.Eventually(t, func() bool {
		return len(dest.capturedTimestamps()) == 1
	}, 5*time.Second, time.Millisecond)

	first := dest.capturedTimestamps()[0]
	require.Len(t, first, 4)
	for _, value := range first[1:] {
		assert.Equal(t, first[0], value, "all rows in one flush should share a timestamp")
	}

	time.Sleep(2 * time.Millisecond)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{5, 6, 7, 8}, nil)}

	require.Eventually(t, func() bool {
		return len(dest.capturedTimestamps()) == 2
	}, 5*time.Second, time.Millisecond)

	second := dest.capturedTimestamps()[1]
	require.Len(t, second, 4)
	for _, value := range second[1:] {
		assert.Equal(t, second[0], value, "all rows in one flush should share a timestamp")
	}
	assert.NotEqual(t, first[0], second[0], "later flushes should get a fresh load timestamp")

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

func TestStreamingExecutor_PassesFlushOptionsToSource(t *testing.T) {
	job, src, _ := minimalJob()
	job.Config.FlushInterval = 123 * time.Millisecond
	job.Config.FlushRecords = 7
	src.readCh = mustClosedRecords()

	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: job.Config.FlushInterval,
		FlushRecords:  int64(job.Config.FlushRecords),
		Strategy:      config.StrategyAppend,
	})
	require.NoError(t, exec.Execute(context.Background(), job))

	src.mu.Lock()
	defer src.mu.Unlock()
	require.True(t, src.readCalled)
	assert.True(t, src.readOpts.Streaming)
	assert.Equal(t, 123*time.Millisecond, src.readOpts.FlushInterval)
	assert.Equal(t, 7, src.readOpts.FlushRecords)
}

func TestStreaming_ExecuteAppliesBatchTransformationsOnce(t *testing.T) {
	job, src, _ := minimalJob()
	dest := &capturingDestination{fakeDestination: &fakeDestination{}}
	job.Destination = dest
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: intStringRecordBatch(t, "id", []int64{1}, "name", []string{"  secret  "}),
	})
	job.WhitespaceTrimmer = transformer.NewWhitespaceTrimmer()
	masker, err := transformer.NewColumnMasker([]string{"name:hash"})
	require.NoError(t, err)
	job.ColumnMasker = masker

	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyAppend,
	})

	require.NoError(t, exec.Execute(context.Background(), job))

	dest.valuesMu.Lock()
	defer dest.valuesMu.Unlock()
	require.Equal(t, []string{sha256Hex("secret")}, dest.values)
	assert.NotEqual(t, sha256Hex(sha256Hex("secret")), dest.values[0], "masking must not run twice")
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

func TestStreamingExecutorMergeFailureCancelsAndJoinsSource(t *testing.T) {
	job, baseSource, dest := minimalJob()
	job.Config.NoLoadTimestamp = true
	job.Config.FlushRecords = 1
	baseSource.strategy = config.StrategyMerge
	src := &cancelAwareSourceTable{fakeSourceTable: baseSource, done: make(chan struct{})}
	for i := 0; i < 32; i++ {
		src.batches = append(src.batches, int64RecordBatch(t, "id", []int64{int64(i)}, nil))
	}
	job.Table = src
	dest.mergeErr = errors.New("merge failed")

	exec := NewStreamingExecutor(StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
	})
	err := exec.Execute(context.Background(), job)
	require.ErrorContains(t, err, "merge failed")
	select {
	case <-src.done:
	case <-time.After(time.Second):
		t.Fatal("source producer remained blocked after merge failure")
	}
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

func TestStreaming_LeaseLossDiscardsTailWithoutDestinationOrStateMutation(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	stateDest := newCDCStateDestination()
	manager, err := NewCDCStateManager(stateDest, "0123456789abcdef", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(context.Background(), "public.items", "raw.items"))
	require.NoError(t, manager.BeginRun(context.Background(), false))
	stateWritesBeforeLoss := stateDest.cdcWrites

	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
		StateManager:  manager,
	}, map[string]*streamTableState{"": mergeTableState("raw.items")})
	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1, 2, 3}, nil),
		CommitToken: source.CDCStateCommitToken{
			SourceCommitToken: "token",
			Position:          "00000000/00000010",
		},
	})

	ctx, cancel := context.WithCancelCause(context.Background())
	loss := errors.New("lease backend terminated")
	cancel(errors.Join(source.ErrConnectorLeaseLost, loss))
	records := make(chan source.RecordBatchResult)
	close(records)

	err = loop.run(ctx, records)
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	require.ErrorIs(t, err, loss)
	assert.Zero(t, writeCallCount(dest))
	assert.Empty(t, dest.mergeCalls)
	assert.Empty(t, dest.truncateCalls)
	assert.Empty(t, committer.committed())
	assert.Equal(t, stateWritesBeforeLoss, stateDest.cdcWrites)
	assert.Zero(t, loop.buffered)
	assert.False(t, loop.tokenDirty)

	loop.cleanup(ctx)
	assert.Empty(t, dest.dropCalls, "lease-loss cleanup must not mutate staging tables")
}

func TestStreaming_LeaseLossRefencesBetweenFlushMutations(t *testing.T) {
	for _, stage := range []string{"write", "merge", "reset"} {
		t.Run(stage, func(t *testing.T) {
			lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during " + stage)}
			base := &fakeDestination{}
			dest := &stageBlockingDestination{
				truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
				stage:                      stage,
				entered:                    make(chan struct{}),
				release:                    make(chan struct{}),
			}
			committer := &fakeCommitter{}
			loop := newTestLoop(dest, StreamingOptions{
				FlushInterval: time.Hour,
				FlushRecords:  1 << 30,
				Strategy:      config.StrategyMerge,
				Committer:     committer,
			}, map[string]*streamTableState{"": mergeTableState("raw.items")})
			loop.buffer(source.RecordBatchResult{
				Batch:       int64RecordBatch(t, "id", []int64{1, 2, 3}, nil),
				CommitToken: "token",
			})

			done := make(chan error, 1)
			go func() { done <- loop.flush(guardedStreamingContext(lease)) }()
			<-dest.entered
			close(lease.done)
			close(dest.release)

			err := <-done
			require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
			assert.Empty(t, committer.committed())

			base.mu.Lock()
			calls := append([]string(nil), base.calls...)
			base.mu.Unlock()
			switch stage {
			case "write":
				assert.Equal(t, []string{"WriteParallel"}, calls)
			case "merge":
				assert.Equal(t, []string{"WriteParallel", "MergeTable"}, calls)
			case "reset":
				assert.Equal(t, []string{"WriteParallel", "MergeTable", "TruncateTable"}, calls)
			}
		})
	}
}

func TestStreaming_LeaseLossDuringStatePersistSkipsSourceCommit(t *testing.T) {
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during state persist")}
	stateDest := &blockingStateDestination{
		cdcStateDestination: newCDCStateDestination(),
		entered:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	manager, err := NewCDCStateManager(stateDest, "0123456789abcdef", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(context.Background(), "public.items", "raw.items"))
	require.NoError(t, manager.BeginRun(context.Background(), false))
	stateDest.block = true

	committer := &fakeCommitter{}
	loop := newTestLoop(&truncateCapableDestination{fakeDestination: stateDest.fakeDestination}, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
		StateManager:  manager,
	}, map[string]*streamTableState{"": mergeTableState("raw.items")})
	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{
			SourceCommitToken: "token",
			Position:          "00000000/00000010",
		},
	})

	done := make(chan error, 1)
	go func() { done <- loop.flush(guardedStreamingContext(lease)) }()
	<-stateDest.entered
	close(lease.done)
	close(stateDest.release)

	err = <-done
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	assert.Empty(t, committer.committed())
}

func TestStreamingFinalizesLegacySlotAfterDurableStateAndSourceCommit(t *testing.T) {
	stateDest := newCDCStateDestination()
	manager, err := NewCDCStateManager(stateDest, "streaming-legacy-cutover", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	require.NoError(t, manager.BeginRun(t.Context(), false))

	committer := &fakeCommitter{}
	finalizer := &fakeLegacySlotFinalizer{committer: committer}
	loop := newTestLoop(&truncateCapableDestination{fakeDestination: stateDest.fakeDestination}, StreamingOptions{
		FlushInterval:   time.Hour,
		FlushRecords:    1 << 30,
		Strategy:        config.StrategyMerge,
		Committer:       committer,
		StateManager:    manager,
		LegacyFinalizer: finalizer,
	}, map[string]*streamTableState{"": mergeTableState("raw.items")})
	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{
			SourceCommitToken:    "source-token",
			Position:             "00000000/00000010",
			SnapshotPositions:    map[string]string{"public.items": "00000000/00000010"},
			SnapshotIncarnations: map[string]string{"public.items": "100"},
		},
	})

	require.NoError(t, loop.flush(t.Context()))
	require.Equal(t, 1, finalizer.calls)
	require.Len(t, committer.committed(), 1)
	require.False(t, loop.tokenDirty)

	loop.buffer(source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		SourceCommitToken: "later-token",
		Position:          "00000000/00000020",
	}})
	require.NoError(t, loop.flush(t.Context()))
	require.Equal(t, 1, finalizer.calls)
	require.Len(t, committer.committed(), 2)
}

func TestDrainAndReleaseUntilReturnsWhenProducerStalls(t *testing.T) {
	var releases atomic.Int64
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: &releaseCountingRecordBatch{
		RecordBatch: int64RecordBatch(t, "id", []int64{1}, nil),
		releases:    &releases,
	}}

	closed := drainAndReleaseUntil(records, 20*time.Millisecond)
	require.False(t, closed)
	require.Equal(t, int64(1), releases.Load())
}

func TestStreaming_GracefulCancelThenLeaseLossCancelsDetachedFlush(t *testing.T) {
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during final flush")}
	base := &fakeDestination{}
	dest := &stageBlockingDestination{
		truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
		stage:                      "write",
		entered:                    make(chan struct{}),
		release:                    make(chan struct{}),
	}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": mergeTableState("raw.items")})
	loop.buffer(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), CommitToken: "token"})

	guarded := guardedStreamingContext(lease)
	ctx, cancel := context.WithCancel(guarded)
	records := make(chan source.RecordBatchResult)
	done := make(chan error, 1)
	go func() { done <- loop.run(ctx, records) }()
	cancel()
	close(records)
	<-dest.entered
	close(lease.done)
	close(dest.release)

	err := <-done
	require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
	assert.Empty(t, committer.committed())
	base.mu.Lock()
	assert.Empty(t, base.mergeCalls)
	assert.Empty(t, base.truncateCalls)
	base.mu.Unlock()

	loop.cleanup(ctx)
	base.mu.Lock()
	assert.Empty(t, base.dropCalls)
	base.mu.Unlock()
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

func TestStreaming_CDCMergePreservesStagingKeysWithoutConstraint(t *testing.T) {
	dest := &fakeDestination{}
	exec := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge})
	st := &streamTableState{
		destTable: "ds.evt",
		schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		}},
		primaryKeys: []string{"id"},
		isCDC:       true,
	}

	require.NoError(t, exec.prepareTable(t.Context(), dest, &config.IngestConfig{}, st))
	require.Len(t, dest.prepareCalls, 2)
	assert.Equal(t, []string{"id"}, dest.prepareCalls[0].CDCKeys)
	assert.Equal(t, []string{"id"}, dest.prepareCalls[1].CDCKeys)
	assert.Empty(t, dest.prepareCalls[1].PrimaryKeys, "staging must not declare a PK constraint")

	require.NoError(t, (&flushLoop{dest: dest}).resetStaging(t.Context(), st))
	require.Len(t, dest.prepareCalls, 3)
	assert.Equal(t, []string{"id"}, dest.prepareCalls[2].CDCKeys)
	assert.Empty(t, dest.prepareCalls[2].PrimaryKeys, "reset staging must not declare a PK constraint")
}

func TestStreaming_MergePassesIncrementalKeyForOrdering(t *testing.T) {
	// Broker streams set an incremental key (e.g. _ingestr_order) so the per-PK
	// dedup keeps the latest record within a flush cycle rather than arbitrary.
	dest := &fakeDestination{}
	st := mergeTableState("ds.tbl")
	st.incrementalKey = "_ingestr_order"
	st.schema.Columns = append(st.schema.Columns, schema.Column{Name: "_ingestr_order", DataType: schema.TypeInt64, Nullable: false})
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

// rendezvousDest blocks each WriteParallel until `expected` calls are in
// flight simultaneously, proving the flush cycles overlap.
type rendezvousDest struct {
	*fakeDestination
	limit    int
	expected int

	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func (d *rendezvousDest) MaxConcurrentFlushes() int { return d.limit }

func (d *rendezvousDest) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.mu.Lock()
	d.arrived++
	if d.arrived == d.expected {
		close(d.release)
	}
	d.mu.Unlock()

	select {
	case <-d.release:
	case <-time.After(5 * time.Second):
		drainAndRelease(records)
		return fmt.Errorf("flushes did not overlap: table %s waited alone", opts.Table)
	}
	return d.fakeDestination.WriteParallel(ctx, records, opts)
}

func TestStreaming_ParallelFlushMergesTablesConcurrently(t *testing.T) {
	dest := &rendezvousDest{
		fakeDestination: &fakeDestination{},
		limit:           4,
		expected:        2,
		release:         make(chan struct{}),
	}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{
		"public.users":  mergeTableState("ds.users"),
		"public.orders": mergeTableState("ds.orders"),
	})

	loop.buffer(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil), TableName: "public.users", CommitToken: "t1"})
	loop.buffer(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{3}, nil), TableName: "public.orders", CommitToken: "t2"})

	require.NoError(t, loop.flush(context.Background()))

	dest.fakeDestination.mu.Lock()
	assert.Len(t, dest.writeCalls, 2)
	assert.Len(t, dest.mergeCalls, 2)
	dest.fakeDestination.mu.Unlock()
	assert.Equal(t, []any{"t2"}, committer.committed(), "token committed once after all tables flushed")
	assert.Zero(t, loop.buffered)
}

// A failure in any table's cycle must abort the flush without committing the
// source position, so all tables re-deliver from the last durable point.
func TestStreaming_ParallelFlushFailureSkipsCommit(t *testing.T) {
	base := &fakeDestination{mergeErr: errors.New("merge exploded")}
	dest := &rendezvousDest{
		fakeDestination: base,
		limit:           4,
		expected:        2,
		release:         make(chan struct{}),
	}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1 << 30,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{
		"public.users":  mergeTableState("ds.users"),
		"public.orders": mergeTableState("ds.orders"),
	})

	loop.buffer(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.users", CommitToken: "t1"})
	loop.buffer(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{2}, nil), TableName: "public.orders", CommitToken: "t2"})

	err := loop.flush(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge exploded")
	assert.Empty(t, committer.committed(), "failed flush must not confirm the source position")
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
