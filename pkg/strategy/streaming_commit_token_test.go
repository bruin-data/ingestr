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

type durableTokenCall struct {
	table        string
	token        source.DurableID
	cdcResumeLSN string
}

type durableTokenDestination struct {
	*fakeDestination

	mu    sync.Mutex
	calls []durableTokenCall
	err   error
}

func (d *durableTokenDestination) CommitWriteToken(_ context.Context, table string, token source.DurableID, cdcResumeLSN string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, durableTokenCall{table: table, token: token, cdcResumeLSN: cdcResumeLSN})
	return d.err
}

func TestStreamingFlushPassesCommitMetadataToWriteAndMerge(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": st})

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil), CommitToken: stringerToken("00000000/00000020"),
		DurableCommitID: "payload:00000000/00000020", DurableCommitPosition: "00000000/00000020",
	}
	close(records)
	require.NoError(t, loop.run(context.Background(), records))

	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 1)
	require.Empty(t, dest.writeCalls[0].CommitToken, "staging writes must not claim the target payload identity")
	require.Empty(t, dest.writeCalls[0].CDCResumeLSN)
	require.True(t, dest.writeCalls[0].SkipCDCResume)
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, source.DurableID("payload:00000000/00000020"), dest.mergeCalls[0].CommitToken)
	require.Equal(t, "00000000/00000020", dest.mergeCalls[0].CDCResumeLSN)
	dest.mu.Unlock()
	require.Equal(t, []any{stringerToken("00000000/00000020")}, committer.committed())
}

func TestStreamingSnapshotChunksDoNotAdvanceCDCResumeBeforeFinalChunk(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": st})

	loop.buffer(source.RecordBatchResult{
		Batch:           int64RecordBatch(t, "id", []int64{1}, nil),
		CommitToken:     "snapshot-page-1",
		DurableCommitID: "snapshot-page-1",
	})
	require.NoError(t, loop.flush(context.Background()))

	loop.buffer(source.RecordBatchResult{
		Batch:                 int64RecordBatch(t, "id", []int64{2}, nil),
		CommitToken:           "snapshot-page-final",
		DurableCommitID:       "snapshot-page-final",
		DurableCommitPosition: "0/100",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.mu.Lock()
	require.Len(t, dest.writeCalls, 2)
	require.True(t, dest.writeCalls[0].SkipCDCResume)
	require.Empty(t, dest.writeCalls[0].CDCResumeLSN)
	require.True(t, dest.writeCalls[1].SkipCDCResume)
	require.Empty(t, dest.writeCalls[1].CDCResumeLSN)
	require.Len(t, dest.mergeCalls, 2)
	require.True(t, dest.mergeCalls[0].SkipCDCResume)
	require.Empty(t, dest.mergeCalls[0].CDCResumeLSN)
	require.False(t, dest.mergeCalls[1].SkipCDCResume)
	require.Equal(t, "0/100", dest.mergeCalls[1].CDCResumeLSN)
	dest.mu.Unlock()
	require.Equal(t, []any{"snapshot-page-1", "snapshot-page-final"}, committer.committed())
}

func TestStreamingFlushPersistsTokenOnlyCDCPositionBeforeAcknowledgingSource(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": st})

	loop.buffer(source.RecordBatchResult{
		CommitToken: "00000000/00000030", DurableCheckpointID: "00000000/00000030", DurableCheckpointPosition: "00000000/00000030",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.mu.Lock()
	require.Equal(t, []durableTokenCall{{
		table:        "lake.events",
		token:        "00000000/00000030",
		cdcResumeLSN: "00000000/00000030",
	}}, dest.calls)
	dest.mu.Unlock()
	require.Equal(t, []any{"00000000/00000030"}, committer.committed())
	require.False(t, loop.tokenDirty)
}

func TestStreamingFlushPersistsZeroRowBatchPositionBeforeAcknowledgingSource(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": st})

	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{}, nil), CommitToken: "00000000/00000031",
		DurableCommitID: "00000000/00000031", DurableCommitPosition: "00000000/00000031",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.mu.Lock()
	require.Equal(t, []durableTokenCall{{
		table:        "lake.events",
		token:        "00000000/00000031",
		cdcResumeLSN: "00000000/00000031",
	}}, dest.calls)
	dest.mu.Unlock()
	dest.fakeDestination.mu.Lock()
	require.Empty(t, dest.writeCalls)
	dest.fakeDestination.mu.Unlock()
	require.Equal(t, []any{"00000000/00000031"}, committer.committed())
}

func TestStreamingFlushPersistsGlobalPositionForIdleTables(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	active := mergeTableState("lake.active")
	active.isCDC = true
	idle := mergeTableState("lake.idle")
	idle.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{
		"public.active": active,
		"public.idle":   idle,
	})

	loop.buffer(source.RecordBatchResult{
		Batch: int64RecordBatch(t, "id", []int64{1}, nil), TableName: "public.active", CommitToken: "00000000/00000040",
		DurableCommitID: "00000000/00000040", DurableCommitPosition: "00000000/00000040",
		DurableCheckpointID: "00000000/00000040", DurableCheckpointPosition: "00000000/00000040",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.fakeDestination.mu.Lock()
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, "lake.active", dest.mergeCalls[0].TargetTable)
	require.Equal(t, "00000000/00000040", dest.mergeCalls[0].CDCResumeLSN)
	dest.fakeDestination.mu.Unlock()
	dest.mu.Lock()
	require.Equal(t, []durableTokenCall{{
		table:        "lake.idle",
		token:        "00000000/00000040",
		cdcResumeLSN: "00000000/00000040",
	}}, dest.calls)
	dest.mu.Unlock()
	require.Equal(t, []any{"00000000/00000040"}, committer.committed())
}

func TestStreamingSnapshotCheckpointDoesNotAdvanceOtherTables(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	committer := &fakeCommitter{}
	first := mergeTableState("lake.first")
	first.isCDC = true
	second := mergeTableState("lake.second")
	second.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{
		"public.first":  first,
		"public.second": second,
	})

	loop.buffer(source.RecordBatchResult{
		Batch:                     int64RecordBatch(t, "id", []int64{1}, nil),
		TableName:                 "public.first",
		CommitToken:               "0/100",
		DurableCommitID:           "snapshot:first:final",
		DurableCommitPosition:     "0/100",
		DurableCheckpointID:       "checkpoint:0/100",
		DurableCheckpointPosition: "0/100",
		DurableCheckpointTable:    "public.first",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.fakeDestination.mu.Lock()
	require.Len(t, dest.mergeCalls, 1)
	require.Equal(t, "lake.first", dest.mergeCalls[0].TargetTable)
	require.Equal(t, "0/100", dest.mergeCalls[0].CDCResumeLSN)
	dest.fakeDestination.mu.Unlock()
	dest.mu.Lock()
	require.Empty(t, dest.calls, "an unfinished table must not receive another table's snapshot cursor")
	dest.mu.Unlock()

	loop.buffer(source.RecordBatchResult{
		Batch:                     int64RecordBatch(t, "id", []int64{2}, nil),
		TableName:                 "public.second",
		CommitToken:               "0/100",
		DurableCommitID:           "snapshot:second:final",
		DurableCommitPosition:     "0/100",
		DurableCheckpointID:       "checkpoint:0/100",
		DurableCheckpointPosition: "0/100",
		DurableCheckpointTable:    "public.second",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.fakeDestination.mu.Lock()
	require.Len(t, dest.mergeCalls, 2)
	require.Equal(t, "lake.second", dest.mergeCalls[1].TargetTable)
	require.Equal(t, "0/100", dest.mergeCalls[1].CDCResumeLSN)
	dest.fakeDestination.mu.Unlock()
	dest.mu.Lock()
	require.Empty(t, dest.calls)
	dest.mu.Unlock()
}

func TestStreamingEmptySnapshotCheckpointOnlyAdvancesItsTable(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	first := mergeTableState("lake.first")
	first.isCDC = true
	second := mergeTableState("lake.second")
	second.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyMerge,
	}, map[string]*streamTableState{
		"public.first":  first,
		"public.second": second,
	})

	loop.buffer(source.RecordBatchResult{
		TableName:                 "public.first",
		DurableCheckpointID:       "checkpoint:0/100",
		DurableCheckpointPosition: "0/100",
		DurableCheckpointTable:    "public.first",
	})
	require.NoError(t, loop.flush(context.Background()))

	dest.mu.Lock()
	require.Equal(t, []durableTokenCall{{
		table:        "lake.first",
		token:        "checkpoint:0/100",
		cdcResumeLSN: "0/100",
	}}, dest.calls)
	dest.mu.Unlock()
}

func TestStreamingFinalFlushPersistsTokenlessTableCheckpoint(t *testing.T) {
	dest := &durableTokenDestination{fakeDestination: &fakeDestination{}}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour, FlushRecords: 100, Strategy: config.StrategyMerge,
	}, map[string]*streamTableState{"public.events": st})
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: "public.events", DurableCommitID: "empty-snapshot", DurableCommitPosition: "0/200",
	}
	close(records)

	require.NoError(t, loop.run(context.Background(), records))
	dest.mu.Lock()
	require.Equal(t, []durableTokenCall{{
		table: "lake.events", token: "empty-snapshot", cdcResumeLSN: "0/200",
	}}, dest.calls)
	dest.mu.Unlock()
	require.False(t, loop.hasPendingDurableCheckpoint())
}

func TestStreamingCheckpointFailureDoesNotAcknowledgeSource(t *testing.T) {
	checkpointErr := errors.New("checkpoint commit failed")
	dest := &durableTokenDestination{
		fakeDestination: &fakeDestination{},
		err:             checkpointErr,
	}
	committer := &fakeCommitter{}
	st := mergeTableState("lake.events")
	st.isCDC = true
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  1,
		Strategy:      config.StrategyMerge,
		Committer:     committer,
	}, map[string]*streamTableState{"": st})

	loop.buffer(source.RecordBatchResult{
		CommitToken: "00000000/00000050", DurableCheckpointID: "00000000/00000050", DurableCheckpointPosition: "00000000/00000050",
	})
	err := loop.flush(context.Background())
	require.ErrorIs(t, err, checkpointErr)
	require.Empty(t, committer.committed())
	require.True(t, loop.tokenDirty)
}

type stringerToken string

func (t stringerToken) String() string { return string(t) }

var _ destination.DurableCommitTokenWriter = (*durableTokenDestination)(nil)
