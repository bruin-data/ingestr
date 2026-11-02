package postgres_cdc

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func TestSerialSnapshotCertificationWaitsForCatchUp(t *testing.T) {
	state := chunkTestState("identity")
	state.Mode, state.Execution = "inmemory", "serial"
	state.Tables["public.items"].Done = true
	require.True(t, state.complete())
	require.False(t, state.bootstrapComplete())
	r := &chunkReader{state: state}
	truncate := []DecodedChanges{{TableName: "public.items", Changes: []Change{{Operation: "TRUNCATE"}}}}
	incomplete := []DecodedChanges{{TableName: "public.items", Changes: []Change{{Operation: "UPDATE", Values: []any{int64(1), tupleUnchangedMarker}}}}}
	require.ErrorContains(t, r.checkChanges(truncate), "truncated during bootstrap")
	require.ErrorContains(t, r.checkChanges(incomplete), "incomplete row image during bootstrap")
	state.SnapshotComplete = true
	require.True(t, state.bootstrapComplete())
	require.NoError(t, r.checkChanges(truncate))
	require.NoError(t, r.checkChanges(incomplete))
	state.Execution, state.SnapshotComplete = "", false
	require.True(t, state.bootstrapComplete(), "legacy parallel checkpoints retain their completion semantics")
}

func TestSerialChunkCheckpointRetainsReplayBoundary(t *testing.T) {
	store := newChunkTestStore()
	state := chunkTestState(store.identity)
	state.Mode, state.Execution = "inmemory", "serial"
	r, results := chunkCleanupReader(t, store, state)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.checkpoint(ctx, state.Applied, nil) }()
	var result source.RecordBatchResult
	select {
	case result = <-results:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	token := result.CommitToken.(source.CDCStateCommitToken)
	require.Equal(t, FormatLSN(20), token.Position)
	require.Empty(t, token.SnapshotPositions)
	require.Empty(t, store.values)
	require.NoError(t, r.source.CommitStream(ctx, token))
	require.NoError(t, <-done)
	loaded, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, []string{"5"}, loaded.Tables["public.items"].After)
	require.Equal(t, pglogrepl.LSN(20), loaded.Applied)
	require.Equal(t, loaded.Applied, r.source.pos.Committed())
	require.False(t, loaded.SnapshotComplete)
}

func TestSerialSnapshotRetainsAdoptedTableReplayFloors(t *testing.T) {
	store := newChunkTestStore()
	state := chunkTestState(store.identity)
	state.Mode, state.Execution = "inmemory", "serial"
	state.Tables["public.adopted"] = &chunkTableState{Boundary: FormatLSN(20), ReplayFrom: FormatLSN(100), Done: true}
	require.NoError(t, (&chunkedCommit{store: store, state: state.clone()}).persist(t.Context()))
	loaded, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	filter := &chunkResumeFilter{positions: loaded.replayPositions()}
	require.True(t, filter.ShouldFilterChange("public.adopted", 90), "historical truncates must not erase an adopted baseline")
	require.False(t, filter.ShouldFilterChange("public.adopted", 100))
	require.False(t, filter.ShouldFilterChange("public.items", 90), "new snapshots still replay all changes from the slot boundary")
}

func TestSerialSnapshotRejectsInconsistentState(t *testing.T) {
	for _, modify := range []func(*chunkBootstrapState){
		func(s *chunkBootstrapState) { s.SnapshotComplete = true },
		func(s *chunkBootstrapState) { s.Mode = "destination" },
		func(s *chunkBootstrapState) { s.Execution = "unknown" },
		func(s *chunkBootstrapState) { s.Tables["public.items"].ReplayFrom = "bad" },
		func(s *chunkBootstrapState) { s.Tables["public.items"] = nil; s.SnapshotComplete = true },
	} {
		store := newChunkTestStore()
		state := chunkTestState(store.identity)
		state.Mode, state.Execution = "inmemory", "serial"
		modify(state)
		require.NoError(t, (&chunkedCommit{store: store, state: state}).persist(t.Context()))
		_, err := loadChunkState(t.Context(), store)
		require.Error(t, err)
	}
}
