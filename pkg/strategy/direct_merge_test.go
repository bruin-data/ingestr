package strategy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type directMergeCall struct {
	write destination.WriteOptions
	merge destination.MergeOptions
}

type directMergeDestination struct {
	*fakeDestination
	directMu    sync.Mutex
	directCalls []directMergeCall
}

type durableDirectMergeDestination struct {
	*directMergeDestination
	tokenMu         sync.Mutex
	tokenCalls      []durableTokenCall
	committedTokens map[source.DurableID]struct{}
}

func (*durableDirectMergeDestination) SupportsIdempotentCommitTokenWrites() bool { return true }

type idempotentDirectMergeDestination struct {
	*directMergeDestination
	tokenMu         sync.Mutex
	committedTokens map[source.DurableID]struct{}
}

func (*idempotentDirectMergeDestination) SupportsIdempotentCommitTokenWrites() bool { return true }

func (d *durableDirectMergeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	if d.committedTokens == nil {
		d.committedTokens = make(map[source.DurableID]struct{})
	}
	if _, duplicate := d.committedTokens[opts.CommitToken]; opts.CommitToken != "" && duplicate {
		drainAndRelease(records)
		return nil
	}
	if err := d.directMergeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if opts.CommitToken != "" {
		d.committedTokens[opts.CommitToken] = struct{}{}
	}
	return nil
}

func (d *idempotentDirectMergeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	if d.committedTokens == nil {
		d.committedTokens = make(map[source.DurableID]struct{})
	}
	_, duplicate := d.committedTokens[opts.CommitToken]
	if opts.CommitToken != "" && duplicate {
		drainAndRelease(records)
		return nil
	}
	if err := d.directMergeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if opts.CommitToken != "" {
		d.committedTokens[opts.CommitToken] = struct{}{}
	}
	return nil
}

type failAfterOneDirectMergeDestination struct {
	*fakeDestination
	err error
}

func (d *failAfterOneDirectMergeDestination) MergeRecords(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions, _ destination.MergeOptions) error {
	result, ok := <-records
	if ok && result.Batch != nil {
		result.Batch.Release()
	}
	return d.err
}

func (d *durableDirectMergeDestination) CommitWriteToken(_ context.Context, table string, token source.DurableID, cdcResumeLSN string) error {
	d.tokenMu.Lock()
	defer d.tokenMu.Unlock()
	d.tokenCalls = append(d.tokenCalls, durableTokenCall{table: table, token: token, cdcResumeLSN: cdcResumeLSN})
	return nil
}

func (d *directMergeDestination) MergeRecords(ctx context.Context, records <-chan source.RecordBatchResult, write destination.WriteOptions, merge destination.MergeOptions) error {
	d.directMu.Lock()
	d.directCalls = append(d.directCalls, directMergeCall{write: write, merge: merge})
	d.directMu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-records:
			if !ok {
				return nil
			}
			if result.Batch != nil {
				result.Batch.Release()
			}
			if result.Err != nil {
				return result.Err
			}
		}
	}
}

func (d *directMergeDestination) callsSnapshot() []directMergeCall {
	d.directMu.Lock()
	defer d.directMu.Unlock()
	return append([]directMergeCall(nil), d.directCalls...)
}

func TestMergeStrategyUsesDirectMergeWithoutStaging(t *testing.T) {
	job, src, base := minimalJob()
	dest := &directMergeDestination{fakeDestination: base}
	job.Destination = dest
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.IncrementalPredicate = "t.updated_at >= DATE '2026-07-01'"
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)})

	require.NoError(t, (&MergeStrategy{}).Execute(context.Background(), job))
	require.Len(t, base.prepareCalls, 1)
	require.Equal(t, job.Config.DestTable, base.prepareCalls[0].Table)
	require.Empty(t, base.writeCalls)
	require.Empty(t, base.mergeCalls)
	require.Empty(t, base.dropCalls)

	calls := dest.callsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, job.Config.DestTable, calls[0].merge.TargetTable)
	require.Equal(t, []string{"id"}, calls[0].merge.PrimaryKeys)
	require.Equal(t, job.Config.ExtractParallelism, calls[0].write.Parallelism)
	require.Equal(t, job.Config.IncrementalPredicate, calls[0].merge.IncrementalPredicate)
}

func TestStreamingDirectMergeFlushesTargetAndTokenWithoutStaging(t *testing.T) {
	base := &fakeDestination{}
	dest := &directMergeDestination{fakeDestination: base}
	executor := NewStreamingExecutor(StreamingOptions{Strategy: config.StrategyMerge})
	state := &streamTableState{
		destTable:            "ds.events",
		schema:               streamTestSchema(),
		primaryKeys:          []string{"id"},
		incrementalKey:       "id",
		incrementalPredicate: "t.id >= 100",
	}
	cfg := &config.IngestConfig{ExtractParallelism: 3}
	require.NoError(t, executor.prepareTable(context.Background(), dest, cfg, state))
	require.True(t, state.directMerge)
	require.Empty(t, state.stagingTable)
	require.Len(t, base.prepareCalls, 1)

	committer := &fakeCommitter{}
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		Strategy: config.StrategyMerge, Committer: committer, FlushInterval: time.Hour,
	}, map[string]*streamTableState{"": state})
	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{7}, nil), CommitToken: "0/70",
		DurableCommitID: "0/70", DurableCommitPosition: "0/70",
	})
	require.NoError(t, loop.flush(context.Background()))

	calls := dest.callsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "ds.events", calls[0].merge.TargetTable)
	require.Equal(t, source.DurableID("0/70"), calls[0].merge.CommitToken)
	require.Equal(t, state.incrementalPredicate, calls[0].merge.IncrementalPredicate)
	require.Empty(t, base.writeCalls)
	require.Empty(t, base.mergeCalls)
	require.Equal(t, []any{"0/70"}, committer.committed())
}

func TestStreamingDirectMergeDoesNotMislabelCombinedFlushWithLastPayloadID(t *testing.T) {
	dest := &directMergeDestination{fakeDestination: &fakeDestination{}}
	state := &streamTableState{
		destTable: "ds.events", schema: streamTestSchema(), primaryKeys: []string{"id"}, directMerge: true,
	}
	loop := newFlushLoop(dest, &config.IngestConfig{ExtractParallelism: 2}, StreamingOptions{
		Strategy: config.StrategyMerge, FlushInterval: time.Hour,
	}, map[string]*streamTableState{"": state})
	loop.buffer(source.RecordBatchResult{
		Batch:           int64RecordBatch(t, "id", []int64{1}, nil),
		DurableCommitID: "payload-a", DurableCommitPosition: "0/10",
	})
	loop.buffer(source.RecordBatchResult{
		Batch:           int64RecordBatch(t, "id", []int64{2}, nil),
		DurableCommitID: "payload-b", DurableCommitPosition: "0/20",
	})

	require.NoError(t, loop.flush(t.Context()))
	calls := dest.callsSnapshot()
	require.Len(t, calls, 1)
	require.Empty(t, calls[0].merge.CommitToken, "a combined commit cannot use only its final payload identity")
	require.Equal(t, "0/20", calls[0].merge.CDCResumeLSN)
}

func TestWriteDirectMultiTableMergeRoutesEachTable(t *testing.T) {
	base := &fakeDestination{}
	dest := &directMergeDestination{fakeDestination: base}
	tableSchema := streamTestSchema()
	tables := []source.SourceTableInfo{
		{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}},
		{Name: "public.orders", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}
	configs := map[string]destination.TableWriteConfig{
		"public.users": {
			DestTable: "lake.users", Schema: tableSchema, PrimaryKeys: []string{"id"}, IncrementalKey: "id",
			IncrementalPredicate: "t.id >= 100", SkipCDCResume: true, CDCExpectedIncarnation: "users-v1",
		},
		"public.orders": {DestTable: "lake.orders", Schema: tableSchema, PrimaryKeys: []string{"id"}, IncrementalKey: "id"},
	}
	records := mustClosedRecords(
		source.RecordBatchResult{TableName: "public.users", Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
		source.RecordBatchResult{TableName: "public.orders", Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
	)
	require.NoError(t, writeDirectMultiTableMerge(context.Background(), func() {}, dest, dest, records, tables, configs, 2))

	calls := dest.callsSnapshot()
	require.Len(t, calls, 2)
	seen := map[string]bool{}
	for _, call := range calls {
		seen[call.merge.TargetTable] = true
		if call.merge.TargetTable == "lake.users" {
			require.True(t, call.write.SkipCDCResume)
			require.True(t, call.merge.SkipCDCResume)
			require.Equal(t, "users-v1", call.write.CDCExpectedIncarnation)
			require.Equal(t, "id", call.merge.IncrementalKey)
			require.Equal(t, "t.id >= 100", call.merge.IncrementalPredicate)
		}
	}
	require.Equal(t, map[string]bool{"lake.users": true, "lake.orders": true}, seen)
}

func TestWriteDirectMultiTableMergeCommitsKeylessCDCByDurableTransaction(t *testing.T) {
	base := &fakeDestination{}
	dest := &idempotentDirectMergeDestination{directMergeDestination: &directMergeDestination{fakeDestination: base}}
	tableSchema := keylessCDCSchema()
	tables := []source.SourceTableInfo{{Name: "public.events", Schema: tableSchema}}
	configs := map[string]destination.TableWriteConfig{
		"public.events": {DestTable: "lake.events", Schema: tableSchema},
	}
	records := mustClosedRecords(
		source.RecordBatchResult{
			TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "wal:public.events:0/10", DurableCommitPosition: "0/10",
		},
		source.RecordBatchResult{
			TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
			DurableCommitID: "wal:public.events:0/20", DurableCommitPosition: "0/20",
		},
	)

	require.NoError(t, writeDirectMultiTableMerge(context.Background(), func() {}, dest, dest, records, tables, configs, 2))
	replay := mustClosedRecords(
		source.RecordBatchResult{
			TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "wal:public.events:0/10", DurableCommitPosition: "0/10",
		},
		source.RecordBatchResult{
			TableName: "public.events", Batch: int64RecordBatch(t, "id", []int64{2}, nil),
			DurableCommitID: "wal:public.events:0/20", DurableCommitPosition: "0/20",
		},
	)
	require.NoError(t, writeDirectMultiTableMerge(context.Background(), func() {}, dest, dest, replay, tables, configs, 2))
	require.Empty(t, dest.callsSnapshot(), "keyless CDC must append rather than merge")

	base.mu.Lock()
	defer base.mu.Unlock()
	require.Len(t, base.writeCalls, 2, "replayed WAL transactions must not be appended twice")
	require.Equal(t, source.DurableID("wal:public.events:0/10"), base.writeCalls[0].CommitToken)
	require.Equal(t, "0/10", base.writeCalls[0].CDCResumeLSN)
	require.False(t, base.writeCalls[0].SkipCDCResume)
	require.Equal(t, source.DurableID("wal:public.events:0/20"), base.writeCalls[1].CommitToken)
	require.Equal(t, "0/20", base.writeCalls[1].CDCResumeLSN)
}

func TestWriteDirectMultiTableMergeDrainsBlockingProducerAfterCancellation(t *testing.T) {
	mergeErr := errors.New("merge failed")
	dest := &failAfterOneDirectMergeDestination{fakeDestination: &fakeDestination{}, err: mergeErr}
	tableSchema := streamTestSchema()
	tables := []source.SourceTableInfo{{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}}}
	configs := map[string]destination.TableWriteConfig{
		"public.users": {DestTable: "lake.users", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}

	records := make(chan source.RecordBatchResult, 1)
	trigger := source.RecordBatchResult{
		TableName: "public.users",
		Batch:     int64RecordBatch(t, "id", []int64{0}, nil),
	}
	pending := []source.RecordBatchResult{
		{TableName: "public.users", Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
		{TableName: "public.users", Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
		{TableName: "public.users", Batch: int64RecordBatch(t, "id", []int64{3}, nil)},
		{TableName: "public.users", Batch: int64RecordBatch(t, "id", []int64{4}, nil)},
	}
	canceled := make(chan struct{})
	firstPendingSent := make(chan struct{})
	resumeProducer := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		records <- trigger
		<-canceled
		records <- pending[0]
		close(firstPendingSent)
		<-resumeProducer
		for _, result := range pending[1:] {
			records <- result
		}
		close(records)
	}()
	go func() {
		<-firstPendingSent
		time.Sleep(25 * time.Millisecond)
		close(resumeProducer)
	}()

	var cancelOnce sync.Once
	err := writeDirectMultiTableMerge(context.Background(), func() {
		cancelOnce.Do(func() { close(canceled) })
	}, dest, dest, records, tables, configs, 1)
	require.ErrorIs(t, err, mergeErr)

	select {
	case <-producerDone:
	case <-time.After(time.Second):
		go drainAndRelease(records)
		<-producerDone
		t.Fatal("source producer remained blocked after cancellation")
	}
}

func TestStartBoundedRecordDrainStopsForNonclosingSource(t *testing.T) {
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1}, nil)}

	done := startBoundedRecordDrain(records, 20*time.Millisecond)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bounded source drain did not stop at its deadline")
	}
	close(records)
}

func TestWriteDurableKeylessCDCRecordsPersistsEmptySnapshotCheckpoint(t *testing.T) {
	base := &fakeDestination{}
	dest := &durableDirectMergeDestination{
		directMergeDestination: &directMergeDestination{fakeDestination: base},
	}
	records := mustClosedRecords(source.RecordBatchResult{
		DurableCommitID: "snapshot:0/30:empty", DurableCommitPosition: "0/30",
	})

	require.NoError(t, writeDurableKeylessCDCRecords(context.Background(), dest, records, destination.WriteOptions{
		Table: "lake.empty_events", Schema: keylessCDCSchema(),
	}))
	require.Empty(t, base.writeCalls)
	dest.tokenMu.Lock()
	defer dest.tokenMu.Unlock()
	require.Equal(t, []durableTokenCall{{
		table: "lake.empty_events", token: "snapshot:0/30:empty", cdcResumeLSN: "0/30",
	}}, dest.tokenCalls)
}

func TestWriteDurableKeylessCDCRecordsManagedWriteSkipsTargetCursor(t *testing.T) {
	base := &fakeDestination{}
	dest := &durableDirectMergeDestination{directMergeDestination: &directMergeDestination{fakeDestination: base}}
	records := mustClosedRecords(
		source.RecordBatchResult{
			Batch:           int64RecordBatch(t, "id", []int64{1}, nil),
			DurableCommitID: "wal:0/20", DurableCommitPosition: "0/20",
		},
		source.RecordBatchResult{DurableCommitID: "wal:0/21", DurableCommitPosition: "0/21"},
	)

	require.NoError(t, writeDurableKeylessCDCRecords(t.Context(), dest, records, destination.WriteOptions{
		Table: "lake.events", Schema: keylessCDCSchema(), SkipCDCResume: true,
	}))
	require.Len(t, base.writeCalls, 1)
	require.True(t, base.writeCalls[0].SkipCDCResume)
	require.Empty(t, base.writeCalls[0].CDCResumeLSN)
	dest.tokenMu.Lock()
	require.Empty(t, dest.tokenCalls)
	dest.tokenMu.Unlock()
}

func TestWriteDurableKeylessCDCRecordsUsesManagedBatchIdentity(t *testing.T) {
	base := &fakeDestination{}
	dest := &durableDirectMergeDestination{directMergeDestination: &directMergeDestination{fakeDestination: base}}
	records := mustClosedRecords(source.RecordBatchResult{
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
		TableName: "public.events",
		CommitToken: source.CDCStateCommitToken{
			Position: "0/20", DataBatchID: "public.events:0/20/1:0/20/1",
		},
	})

	require.NoError(t, writeDurableKeylessCDCRecords(t.Context(), dest, records, destination.WriteOptions{
		Table: "lake.events", Schema: keylessCDCSchema(), SkipCDCResume: true,
	}))
	require.Len(t, base.writeCalls, 1)
	require.Equal(t, managedCDCDataWriteID("public.events", "public.events:0/20/1:0/20/1"), base.writeCalls[0].CommitToken)
	require.True(t, base.writeCalls[0].SkipCDCResume)
	require.Empty(t, base.writeCalls[0].CDCResumeLSN)
}

var (
	_ destination.DirectMergeWriter        = (*directMergeDestination)(nil)
	_ destination.DirectMergeWriter        = (*failAfterOneDirectMergeDestination)(nil)
	_ destination.DurableCommitTokenWriter = (*durableDirectMergeDestination)(nil)
)
