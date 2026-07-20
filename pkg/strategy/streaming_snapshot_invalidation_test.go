package strategy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type orderedCDCStateDestination struct {
	*cdcStateDestination
	orderMu           sync.Mutex
	order             []string
	replaceAfterWrite bool
}

func (d *orderedCDCStateDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.orderMu.Lock()
	d.order = append(d.order, "state")
	d.orderMu.Unlock()
	return d.cdcStateDestination.WriteCDCState(ctx, records, opts)
}

func (d *orderedCDCStateDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := d.fakeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	if d.replaceAfterWrite {
		d.stateMu.Lock()
		d.incarnations[opts.Table] = "externally-replaced"
		d.stateMu.Unlock()
	}
	return nil
}

func (d *orderedCDCStateDestination) TruncateTable(_ context.Context, table string) error {
	d.orderMu.Lock()
	d.order = append(d.order, "truncate")
	d.orderMu.Unlock()
	d.mu.Lock()
	d.truncateCalls = append(d.truncateCalls, table)
	d.mu.Unlock()
	return nil
}

func (d *orderedCDCStateDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if err := d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *orderedCDCStateDestination) resetOrder() {
	d.orderMu.Lock()
	d.order = nil
	d.orderMu.Unlock()
}

func (d *orderedCDCStateDestination) recordedOrder() []string {
	d.orderMu.Lock()
	defer d.orderMu.Unlock()
	return append([]string(nil), d.order...)
}

func replacementSnapshotLoop(t *testing.T) (*flushLoop, *orderedCDCStateDestination, *CDCStateManager, *streamTableState) {
	t.Helper()
	ctx := t.Context()
	dest := &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTableIncarnation(ctx, "public.items", "raw.items", "100"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.items": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}); err != nil {
		t.Fatal(err)
	}

	st := mergeTableState("raw.items")
	st.incarnation = "100"
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		StateManager:  manager,
	}, map[string]*streamTableState{"public.items": st})
	loop.evolveTable = func(context.Context, string, *schema.TableSchema) error { return nil }
	dest.resetOrder()
	return loop, dest, manager, st
}

func TestStreamingSnapshotInvalidationIsDurableBeforeTruncate(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	info := newTableInfo("public.items")
	info.Incarnation = "101"
	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{TableName: "public.items", Incarnation: "101"}}
	records <- source.RecordBatchResult{TableName: "public.items", TableInfo: &info}
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	if got := dest.recordedOrder(); len(got) != 2 || got[0] != "state" || got[1] != "truncate" {
		t.Fatalf("replacement ordering = %v, want [state truncate]", got)
	}

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "101"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(t.Context(), "public.items"); err != nil || got != "" {
		t.Fatalf("resume after crash following truncate = %q, %v; want empty", got, err)
	}
}

func TestStreamingSnapshotInvalidationFailurePreventsTruncate(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	dest.failWrite = dest.cdcWrites + 1
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{TableName: "public.items", Incarnation: "101"}}
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true}
	close(records)

	if err := loop.run(t.Context(), records); err == nil {
		t.Fatal("snapshot invalidation write unexpectedly succeeded")
	}
	if got := dest.recordedOrder(); len(got) != 1 || got[0] != "state" {
		t.Fatalf("replacement ordering after failed state write = %v, want [state]", got)
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("truncate calls after failed invalidation = %v", dest.truncateCalls)
	}
}

func TestStreamingWALTruncateCompletesStableDestinationIncarnation(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	token := source.CDCStateCommitToken{Position: "00000000/00000030"}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName:      "public.items",
		Truncate:       true,
		CDCWALTruncate: true,
		CommitToken:    token,
	}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	if got := dest.recordedOrder(); len(got) != 3 || got[0] != "state" || got[1] != "truncate" || got[2] != "state" {
		t.Fatalf("WAL truncate ordering = %v, want [state truncate state]", got)
	}

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(t.Context(), "public.items"); err != nil || got != token.Position {
		t.Fatalf("resume after completed WAL truncate = %q, %v; want %s", got, err, token.Position)
	}
}

func TestStreamingFreshSnapshotRejectsDestinationReplacementAfterWrite(t *testing.T) {
	loop, dest, manager, st := replacementSnapshotLoop(t)
	st.stagingTable = ""
	st.incarnation = "101"
	loop.opts.Strategy = config.StrategyAppend
	require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.items", "raw.items", "101"))
	dest.replaceAfterWrite = true

	loop.buffer(source.RecordBatchResult{
		TableName: "public.items",
		Batch:     int64RecordBatch(t, "id", []int64{7}, nil),
		CommitToken: source.CDCStateCommitToken{
			Position:             "00000000/00000030",
			SnapshotPositions:    map[string]string{"public.items": "00000000/00000025"},
			SnapshotIncarnations: map[string]string{"public.items": "101"},
		},
	})

	err := loop.flush(t.Context())
	require.ErrorContains(t, err, "was replaced during its snapshot")

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "101"))
	position, err := restarted.ResumePosition(t.Context(), "public.items")
	require.NoError(t, err)
	require.Empty(t, position)
}

func TestStreamingKeylessAppendCrashBeforeStateForcesReplacement(t *testing.T) {
	loop, dest, _, st := replacementSnapshotLoop(t)
	st.stagingTable = ""
	st.primaryKeys = nil
	st.schema = keylessCDCSchema()
	st.isCDC = true
	loop.opts.Strategy = config.StrategyMerge

	// The first state write invalidates the completed marker before the append;
	// fail the following completion write to model a crash after data is durable.
	dest.failWrite = dest.cdcWrites + 2
	loop.buffer(source.RecordBatchResult{
		TableName: "public.items",
		Batch:     int64RecordBatch(t, "id", []int64{7}, nil),
		CommitToken: source.CDCStateCommitToken{
			Position: "00000000/00000030",
		},
	})
	require.Error(t, loop.flush(t.Context()))

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	position, err := restarted.ResumePosition(t.Context(), "public.items")
	require.NoError(t, err)
	require.Empty(t, position)
}

func TestStreamingSnapshotTokenFlushesFullRowsBeforePartialWAL(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName: "public.items",
		Batch:     int64RecordBatch(t, "id", []int64{1}, nil),
	}))
	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		CommitToken: source.CDCStateCommitToken{
			Position:             "00000000/00000020",
			SnapshotPositions:    map[string]string{"public.items": "00000000/00000020"},
			SnapshotIncarnations: map[string]string{"public.items": "100"},
		},
	}))
	require.Len(t, dest.writeCalls, 1)

	require.NoError(t, loop.processResult(t.Context(), source.RecordBatchResult{
		TableName:   "public.items",
		Batch:       int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken: source.CDCStateCommitToken{Position: "00000000/00000030"},
	}))
	require.NoError(t, loop.flush(t.Context()))
	require.Len(t, dest.writeCalls, 2)
}

func TestStreamingWALTruncateWaitsForFollowingRowsCommitToken(t *testing.T) {
	loop, dest, _, st := replacementSnapshotLoop(t)
	st.stagingTable = ""
	loop.opts.Strategy = config.StrategyAppend
	token := source.CDCStateCommitToken{Position: "00000000/00000040"}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true, CDCWALTruncate: true}
	records <- source.RecordBatchResult{
		TableName:   "public.items",
		Batch:       int64RecordBatch(t, "id", []int64{7}, nil),
		CommitToken: token,
	}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	if len(dest.writeCalls) != 1 || dest.writeCalls[0].Table != "raw.items" {
		t.Fatalf("post-truncate writes = %v, want one write to raw.items", dest.writeCalls)
	}

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(t.Context(), "public.items"); err != nil || got != token.Position {
		t.Fatalf("resume after WAL truncate and rows = %q, %v; want %s", got, err, token.Position)
	}
}

func TestStreamingWALTruncateInvalidationFailurePreventsPhysicalTruncate(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	dest.failWrite = dest.cdcWrites + 1
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName:      "public.items",
		Truncate:       true,
		CDCWALTruncate: true,
		CommitToken:    source.CDCStateCommitToken{Position: "00000000/00000030"},
	}
	close(records)

	if err := loop.run(t.Context(), records); err == nil {
		t.Fatal("WAL truncate state invalidation unexpectedly succeeded")
	}
	if got := dest.recordedOrder(); len(got) != 1 || got[0] != "state" {
		t.Fatalf("WAL truncate ordering after failed invalidation = %v, want [state]", got)
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("truncate calls after failed WAL invalidation = %v", dest.truncateCalls)
	}
}

func TestStreamingWALTruncateWithoutCommitTokenRemainsInvalidated(t *testing.T) {
	loop, dest, _, _ := replacementSnapshotLoop(t)
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true, CDCWALTruncate: true}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(t.Context(), "public.items"); err != nil || got != "" {
		t.Fatalf("resume after uncommitted WAL truncate = %q, %v; want empty", got, err)
	}
}

func TestStreamingSameShapeReincarnationCompletesReplacement(t *testing.T) {
	loop, dest, _, st := replacementSnapshotLoop(t)
	info := newTableInfo("public.items")
	info.Incarnation = "101"
	token := source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.items": "00000000/00000025"},
		SnapshotIncarnations: map[string]string{"public.items": "101"},
	}
	records := make(chan source.RecordBatchResult, 4)
	records <- source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{TableName: "public.items", Incarnation: "101"}}
	records <- source.RecordBatchResult{TableName: "public.items", TableInfo: &info}
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true}
	records <- source.RecordBatchResult{CommitToken: token}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	if st.incarnation != "101" {
		t.Fatalf("registered incarnation = %q, want 101", st.incarnation)
	}

	restarted, err := NewCDCStateManager(dest, "stream-replacement", "raw.items", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "101"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(t.Context(), "public.items"); err != nil || got != "00000000/00000030" {
		t.Fatalf("completed same-shape replacement resume = %q, %v", got, err)
	}
}

func TestStreamingSingleTableReplacementRoutesRealSourceName(t *testing.T) {
	loop, dest, _, st := replacementSnapshotLoop(t)
	delete(loop.tables, "public.items")
	loop.tables[""] = st
	st.stagingTable = ""
	loop.opts.Strategy = config.StrategyAppend
	info := newTableInfo("public.items")
	info.Incarnation = "101"
	token := source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.items": "00000000/00000025"},
		SnapshotIncarnations: map[string]string{"public.items": "101"},
	}
	records := make(chan source.RecordBatchResult, 5)
	records <- source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{TableName: "public.items", Incarnation: "101"}}
	records <- source.RecordBatchResult{TableName: "public.items", TableInfo: &info}
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true}
	records <- source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)}
	records <- source.RecordBatchResult{CommitToken: token}
	close(records)

	if err := loop.run(t.Context(), records); err != nil {
		t.Fatal(err)
	}
	if len(dest.truncateCalls) != 1 || dest.truncateCalls[0] != "raw.items" {
		t.Fatalf("single-table truncate calls = %v, want [raw.items]", dest.truncateCalls)
	}
	if len(dest.writeCalls) != 1 || dest.writeCalls[0].Table != "raw.items" {
		t.Fatalf("single-table snapshot writes = %v, want one write to raw.items", dest.writeCalls)
	}
}
