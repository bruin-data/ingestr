package strategy

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type softReplaceDestination struct {
	*orderedCDCStateDestination
	soft        bool
	maxLSN      string
	maxLSNErr   error
	retireErr   error
	retireCalls []destination.CDCRetireOptions
}

func (d *softReplaceDestination) SupportsCDCSnapshotSoftReplace() bool { return d.soft }

func (d *softReplaceDestination) MaxCDCRowLSN(context.Context, string) (string, error) {
	return d.maxLSN, d.maxLSNErr
}

func (d *softReplaceDestination) RetireCDCRowsBefore(_ context.Context, opts destination.CDCRetireOptions) error {
	d.orderMu.Lock()
	d.order = append(d.order, "retire:"+opts.Boundary)
	d.retireCalls = append(d.retireCalls, opts)
	d.orderMu.Unlock()
	return d.retireErr
}

func (d *softReplaceDestination) TruncateTable(_ context.Context, table string) error {
	d.orderMu.Lock()
	d.order = append(d.order, "truncate:"+table)
	d.orderMu.Unlock()
	return nil
}

func (d *softReplaceDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expected string) error {
	if err := d.cdcStateDestination.TruncateCDCTableIfIncarnation(ctx, table, expected); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *softReplaceDestination) targetTruncated() bool {
	return slices.Contains(d.recordedOrder(), "truncate:raw.items")
}

func (d *softReplaceDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	d.orderMu.Lock()
	d.order = append(d.order, "merge")
	d.orderMu.Unlock()
	return d.fakeDestination.MergeTable(ctx, opts)
}

const (
	softSnapshotBoundary = "00000000/00000025"
	softCheckpoint       = "00000000/00000030"
)

func softReplacementLoop(t *testing.T) (*flushLoop, *softReplaceDestination, *streamTableState) {
	t.Helper()
	ctx := t.Context()
	dest := &softReplaceDestination{
		orderedCDCStateDestination: &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()},
		soft:                       true,
		maxLSN:                     "00000000/00000020/0000000000000003",
	}
	manager, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.items", "raw.items", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.items": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}))

	st := mergeTableState("raw.items")
	st.isCDC = true
	st.incarnation = "100"
	cfg := config.DefaultConfig()
	cfg.NoLoadTimestamp = true
	cfg.NoRunID = true
	loop := newFlushLoop(dest, cfg, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		StateManager:  manager,
	}, map[string]*streamTableState{"public.items": st})
	dest.resetOrder()
	return loop, dest, st
}

func softReplacementRecords(t *testing.T, marker source.RecordBatchResult, completedAt string) chan source.RecordBatchResult {
	t.Helper()
	records := make(chan source.RecordBatchResult, 5)
	records <- source.RecordBatchResult{SnapshotInvalidation: &source.CDCSnapshotInvalidation{TableName: "public.items", Incarnation: "101"}}
	records <- marker
	records <- source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)}
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		Position:             softCheckpoint,
		SnapshotPositions:    map[string]string{"public.items": completedAt},
		SnapshotIncarnations: map[string]string{"public.items": "101"},
	}}
	close(records)
	return records
}

func softMarker() source.RecordBatchResult {
	return source.RecordBatchResult{TableName: "public.items", Truncate: true, CDCSnapshotPosition: softSnapshotBoundary}
}

func softResumePosition(t *testing.T, dest *softReplaceDestination) string {
	t.Helper()
	restarted, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "101"))
	position, err := restarted.ResumePosition(t.Context(), "public.items")
	require.NoError(t, err)
	keyed, err := restarted.ResumePositionForKeyedMerge(t.Context(), "public.items")
	require.NoError(t, err)
	require.Equal(t, position, keyed)
	return position
}

func TestStreamingSoftSnapshotReplaceRetiresBeforePersist(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)

	require.NoError(t, loop.run(t.Context(), softReplacementRecords(t, softMarker(), softSnapshotBoundary)))

	require.Equal(t, []string{"state", "merge", "truncate:raw.items_staging", "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
	require.Len(t, dest.retireCalls, 1)
	retire := dest.retireCalls[0]
	require.Equal(t, "raw.items", retire.Table)
	require.Equal(t, []string{"id"}, retire.PrimaryKeys)
	require.Equal(t, "incarnation:raw.items", retire.ExpectedIncarnation)
	require.False(t, retire.SyncedAt.IsZero())
	require.Nil(t, st.softReplace)
	require.Equal(t, softCheckpoint, softResumePosition(t, dest))
}

func TestStreamingSoftSnapshotReplaceRetireFailureSkipsPersist(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	dest.retireErr = errors.New("retire failed")

	err := loop.run(t.Context(), softReplacementRecords(t, softMarker(), softSnapshotBoundary))

	require.ErrorContains(t, err, "retire failed")
	require.Equal(t, []string{"state", "merge", "truncate:raw.items_staging", "retire:" + softSnapshotBoundary}, dest.recordedOrder())
	require.Empty(t, softResumePosition(t, dest))
}

func TestStreamingSoftSnapshotReplaceRejectsMismatchedCompletion(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)

	err := loop.run(t.Context(), softReplacementRecords(t, softMarker(), "00000000/00000026"))

	require.ErrorContains(t, err, "were stamped "+softSnapshotBoundary)
	require.Equal(t, []string{"state", "merge", "truncate:raw.items_staging"}, dest.recordedOrder())
	require.Empty(t, softResumePosition(t, dest))
}

func TestStreamingSoftSnapshotReplaceTargetPosition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		maxLSN string
		soft   bool
	}{
		{name: "empty target", maxLSN: "", soft: true},
		{name: "older sequenced row", maxLSN: "00000000/00000024/FFFFFFFFFFFFFFFF", soft: true},
		{name: "equal to boundary", maxLSN: softSnapshotBoundary, soft: false},
		{name: "sequenced row at boundary", maxLSN: softSnapshotBoundary + "/0000000000000001", soft: false},
		{name: "newer row", maxLSN: "00000001/00000000", soft: false},
		{name: "unpadded position", maxLSN: "0/24", soft: false},
		{name: "unparseable position", maxLSN: "not-an-lsn", soft: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loop, dest, _ := softReplacementLoop(t)
			dest.maxLSN = tc.maxLSN

			require.NoError(t, loop.run(t.Context(), softReplacementRecords(t, softMarker(), softSnapshotBoundary)))

			if tc.soft {
				require.False(t, dest.targetTruncated())
				require.Len(t, dest.retireCalls, 1)
			} else {
				require.True(t, dest.targetTruncated())
				require.Empty(t, dest.retireCalls)
			}
			require.Equal(t, softCheckpoint, softResumePosition(t, dest))
		})
	}
}

func TestStreamingSoftSnapshotReplaceMaxPositionErrorStopsReplacement(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	dest.maxLSNErr = errors.New("max unavailable")
	records := softReplacementRecords(t, softMarker(), softSnapshotBoundary)

	err := loop.run(t.Context(), records)
	drainAndRelease(records)

	require.ErrorContains(t, err, "max unavailable")
	require.False(t, dest.targetTruncated())
	require.Empty(t, dest.retireCalls)
}

func TestStreamingSoftSnapshotReplaceFallsBackToTruncate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker source.RecordBatchResult
		setup  func(*softReplaceDestination, *streamTableState)
	}{
		{
			name:   "destination without capability",
			marker: softMarker(),
			setup:  func(d *softReplaceDestination, _ *streamTableState) { d.soft = false },
		},
		{
			name:   "marker without snapshot position",
			marker: source.RecordBatchResult{TableName: "public.items", Truncate: true},
		},
		{
			name:   "keyless table",
			marker: softMarker(),
			setup: func(_ *softReplaceDestination, st *streamTableState) {
				st.stagingTable = ""
				st.primaryKeys = nil
			},
		},
		{
			name:   "non-CDC table",
			marker: softMarker(),
			setup:  func(_ *softReplaceDestination, st *streamTableState) { st.isCDC = false },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loop, dest, st := softReplacementLoop(t)
			if tc.setup != nil {
				tc.setup(dest, st)
			}

			require.NoError(t, loop.run(t.Context(), softReplacementRecords(t, tc.marker, softSnapshotBoundary)))

			require.True(t, dest.targetTruncated())
			require.Empty(t, dest.retireCalls)
			require.Nil(t, st.softReplace)
		})
	}
}

func TestStreamingSoftSnapshotReplaceKeepsSourceTruncateHard(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{
		TableName:           "public.items",
		Truncate:            true,
		CDCWALTruncate:      true,
		CDCSnapshotPosition: softSnapshotBoundary,
	}
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{Position: softCheckpoint}}
	close(records)

	require.NoError(t, loop.run(t.Context(), records))

	require.True(t, dest.targetTruncated())
	require.Empty(t, dest.retireCalls)
	require.Nil(t, st.softReplace)
}

func TestStreamingSoftSnapshotReplaceWithoutStateManagerTruncates(t *testing.T) {
	dest := &softReplaceDestination{
		orderedCDCStateDestination: &orderedCDCStateDestination{cdcStateDestination: newCDCStateDestination()},
		soft:                       true,
	}
	st := mergeTableState("raw.items")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge},
		map[string]*streamTableState{"public.items": st})
	records := make(chan source.RecordBatchResult, 1)
	records <- softMarker()
	close(records)

	require.NoError(t, loop.run(t.Context(), records))

	require.True(t, dest.targetTruncated())
	require.Empty(t, dest.retireCalls)
}

func TestStreamingSoftSnapshotReplaceHardDeletesRetiredRows(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	loop.cfg.CDCDeleteBehavior = config.CDCDeleteHard

	require.NoError(t, loop.run(t.Context(), softReplacementRecords(t, softMarker(), softSnapshotBoundary)))

	require.Len(t, dest.retireCalls, 1)
	require.True(t, dest.retireCalls[0].HardDelete)
}

func resumedSnapshotRecords(t *testing.T) chan source.RecordBatchResult {
	t.Helper()
	records := make(chan source.RecordBatchResult, 4)
	records <- source.RecordBatchResult{TableName: "public.items", Truncate: true, CDCSnapshotPosition: softSnapshotBoundary, CDCSnapshotResumed: true}
	records <- source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{7, 8}, nil)}
	records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
		Position:             softCheckpoint,
		SnapshotPositions:    map[string]string{"public.items": softSnapshotBoundary},
		SnapshotIncarnations: map[string]string{"public.items": "100"},
	}}
	close(records)
	return records
}

func TestStreamingResumedSnapshotKeepsRowsAndRetiresAtOriginalBoundary(t *testing.T) {
	loop, dest, st := softReplacementLoop(t)
	// Rows the interrupted run made durable carry positions at and beyond the
	// boundary; a fresh replacement would fall back to truncating them.
	dest.maxLSN = "00000000/00000099"

	require.NoError(t, loop.run(t.Context(), resumedSnapshotRecords(t)))

	require.False(t, dest.targetTruncated(), "a resumed snapshot must keep the rows already written")
	require.Equal(t, []string{"state", "merge", "truncate:raw.items_staging", "retire:" + softSnapshotBoundary, "state"}, dest.recordedOrder())
	require.Nil(t, st.softReplace)
	require.Equal(t, softCheckpoint, resumedSnapshotPosition(t, dest))
}

func resumedSnapshotPosition(t *testing.T, dest *softReplaceDestination) string {
	t.Helper()
	restarted, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "100"))
	position, err := restarted.ResumePosition(t.Context(), "public.items")
	require.NoError(t, err)
	return position
}

func TestStreamingResumedSnapshotWithoutSoftReplaceKeepsRows(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	dest.soft = false

	require.NoError(t, loop.run(t.Context(), resumedSnapshotRecords(t)))

	require.False(t, dest.targetTruncated())
	require.Empty(t, dest.retireCalls)
	require.Equal(t, softCheckpoint, resumedSnapshotPosition(t, dest))
}

func TestStreamingPersistsSnapshotProgress(t *testing.T) {
	loop, dest, _ := softReplacementLoop(t)
	records := make(chan source.RecordBatchResult, 4)
	records <- softMarker()
	records <- source.RecordBatchResult{TableName: "public.items", Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)}
	records <- source.RecordBatchResult{TableName: "public.items", CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{
		"public.items": {Boundary: softSnapshotBoundary, Key: `["2"]`, Incarnation: "101"},
	}}}
	close(records)

	require.NoError(t, loop.run(t.Context(), records))

	restarted, err := NewCDCStateManager(dest, "stream-soft-replacement", "raw.items", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(t.Context(), "public.items", "raw.items", "101"))
	resume, ok, err := restarted.SnapshotResume(t.Context(), "public.items")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, `["2"]`, resume.Key)
	require.Equal(t, softSnapshotBoundary, resume.Boundary)
}
