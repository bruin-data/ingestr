package postgres_cdc

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

func snapshotSpoolTestState(identity string) *snapshotSpoolState {
	return &snapshotSpoolState{
		Version: 1, Epoch: "epoch", Identity: identity, Slot: "slot", Mode: "destination", Applied: 20,
		Tables: map[string]source.CDCSnapshotProgress{
			"public.items": {
				Boundary: FormatLSN(10), Incarnation: "42", SchemaFingerprint: "schema", Attempt: "attempt",
				Ranges: []source.CDCSnapshotRangeProgress{
					{ID: "0", Upper: []string{"5"}, Key: []string{"2"}},
					{ID: "1", After: []string{"5"}},
				},
			},
		},
	}
}

func TestSnapshotSpoolStateCloneKeepsCheckpointIndependent(t *testing.T) {
	state := snapshotSpoolTestState("identity")
	cloned := state.clone()
	state.Applied = 100
	progress := state.Tables["public.items"]
	progress.Ranges[0].Upper[0] = "10"
	progress.Ranges[0].Key[0] = "4"
	progress.Ranges[1].After[0] = "10"
	delete(state.Tables, "public.items")
	require.Equal(t, snapshotSpoolTestState("identity"), cloned)
}

func TestSnapshotSpoolStateLoadsInitialProgressAndCompletedCopy(t *testing.T) {
	store := newChunkTestStore()
	absent, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Nil(t, absent)
	state := snapshotSpoolTestState(store.identity)
	progress := state.Tables["public.items"]
	progress.Ranges = nil
	state.Tables["public.items"] = progress
	commit := &snapshotSpoolCommit{store: store, state: state}
	require.NoError(t, commit.persist(t.Context()))
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, state, loaded)

	state.CopyComplete, state.Complete = true, true
	state.Mode, state.Directory = "disk", t.TempDir()
	commit = &snapshotSpoolCommit{store: store, state: state}
	require.NoError(t, commit.persist(t.Context()))
	loaded, err = loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, state, loaded)
	var encoded map[string]any
	require.NoError(t, json.Unmarshal(store.values[snapshotSpoolStateKey], &encoded))
	require.Equal(t, true, encoded["complete"])
}

func TestSnapshotSpoolStateRejectsMalformedCheckpoint(t *testing.T) {
	for name, mutate := range map[string]func(*snapshotSpoolState){
		"version":        func(s *snapshotSpoolState) { s.Version = 2 },
		"epoch":          func(s *snapshotSpoolState) { s.Epoch = "" },
		"identity":       func(s *snapshotSpoolState) { s.Identity = "" },
		"slot":           func(s *snapshotSpoolState) { s.Slot = "" },
		"applied":        func(s *snapshotSpoolState) { s.Applied = 0 },
		"mode":           func(s *snapshotSpoolState) { s.Mode = "unknown" },
		"directory":      func(s *snapshotSpoolState) { s.Mode = "disk" },
		"tables":         func(s *snapshotSpoolState) { s.Tables = nil },
		"empty table":    func(s *snapshotSpoolState) { s.Tables[""] = s.Tables["public.items"] },
		"empty progress": func(s *snapshotSpoolState) { s.Tables["public.items"] = source.CDCSnapshotProgress{} },
		"completion":     func(s *snapshotSpoolState) { s.Complete = true },
		"zero boundary": func(s *snapshotSpoolState) {
			progress := s.Tables["public.items"]
			progress.Boundary = "0/0"
			s.Tables["public.items"] = progress
		},
		"invalid boundary": func(s *snapshotSpoolState) {
			progress := s.Tables["public.items"]
			progress.Boundary = "invalid"
			s.Tables["public.items"] = progress
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := newChunkTestStore()
			state := snapshotSpoolTestState(store.identity)
			mutate(state)
			data, err := json.Marshal(state)
			require.NoError(t, err)
			store.values[snapshotSpoolStateKey] = data
			_, err = loadSnapshotSpoolState(t.Context(), store)
			require.Error(t, err)
		})
	}
	store := newChunkTestStore()
	store.values[snapshotSpoolStateKey] = []byte("not json")
	_, err := loadSnapshotSpoolState(t.Context(), store)
	require.ErrorContains(t, err, "invalid normal snapshot spool checkpoint")
}

func TestSnapshotSpoolStatePersistsResetBeforeSlotCreation(t *testing.T) {
	store := newChunkTestStore()
	store.identity = ""
	state := snapshotSpoolTestState("")
	state.Reset, state.Applied, state.Mode = true, 0, "inmemory"
	progress := state.Tables["public.items"]
	progress.Boundary, progress.Ranges = "", nil
	state.Tables["public.items"] = progress
	commit := &snapshotSpoolCommit{store: store, state: state}
	require.NoError(t, commit.persist(t.Context()))
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, state, loaded)

	for _, mutate := range []func(*snapshotSpoolState){
		func(s *snapshotSpoolState) { s.CopyComplete = true },
		func(s *snapshotSpoolState) { s.Consumed = 1 },
	} {
		invalid := state.clone()
		mutate(invalid)
		data, err := json.Marshal(invalid)
		require.NoError(t, err)
		store.values[snapshotSpoolStateKey] = data
		_, err = loadSnapshotSpoolState(t.Context(), store)
		require.Error(t, err)
	}
}

func TestSnapshotSpoolCommitPersistsBeforeAcknowledgement(t *testing.T) {
	store := newChunkTestStore()
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(10)
	commit := &snapshotSpoolCommit{store: store, state: snapshotSpoolTestState(store.identity), done: make(chan struct{})}
	store.beforeSave = func(key string) {
		require.Equal(t, snapshotSpoolStateKey, key)
		require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
		select {
		case <-commit.done:
			t.Fatal("checkpoint acknowledged before persistence")
		default:
		}
	}
	store.failSaveKey = snapshotSpoolStateKey
	require.ErrorContains(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{SourceCommitToken: commit}), "injected")
	require.Equal(t, pglogrepl.LSN(10), src.pos.Committed())
	select {
	case <-commit.done:
		t.Fatal("failed checkpoint acknowledged the spool")
	default:
	}
	store.failSaveKey = ""
	require.NoError(t, src.CommitStream(t.Context(), source.CDCStateCommitToken{SourceCommitToken: commit}))
	require.Equal(t, commit.state.Applied, src.pos.Committed())
	select {
	case <-commit.done:
	default:
		t.Fatal("durable checkpoint did not acknowledge the spool")
	}
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, commit.state, loaded)

	store.beforeSave = func(string) { t.Fatal("acknowledged checkpoint must not be republished") }
	require.NoError(t, src.CommitStream(t.Context(), commit))
}

func TestSnapshotSpoolCommitBindsIdentityOnce(t *testing.T) {
	store := newChunkTestStore()
	commit := &snapshotSpoolCommit{store: store, state: snapshotSpoolTestState("")}
	store.failSaveKey = snapshotSpoolStateKey
	require.ErrorContains(t, commit.persist(t.Context()), "injected")
	require.Equal(t, store.identity, commit.state.Identity)
	store.failSaveKey = ""
	store.identity = "replacement-destination"
	require.ErrorContains(t, commit.persist(t.Context()), "identity changed")
	require.Empty(t, store.values)
}

func TestSnapshotSpoolCommitRejectsChangedOrMissingIdentity(t *testing.T) {
	for _, identity := range []string{"replacement-destination", ""} {
		store := newChunkTestStore()
		commit := &snapshotSpoolCommit{store: store, state: snapshotSpoolTestState(store.identity)}
		store.identity = identity
		require.ErrorContains(t, commit.persist(t.Context()), "identity changed")
		require.Empty(t, store.values)
	}
}

func TestSnapshotSpoolCommitKeepsAppliedBehindCapturedWAL(t *testing.T) {
	store := newChunkTestStore()
	src := &PostgresCDCSource{pos: newStreamPosition()}
	src.pos.Commit(100)
	commit := &snapshotSpoolCommit{store: store, state: snapshotSpoolTestState(store.identity), done: make(chan struct{})}
	require.NoError(t, src.CommitStream(t.Context(), commit))
	require.Equal(t, pglogrepl.LSN(100), src.pos.Committed())
	loaded, err := loadSnapshotSpoolState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, pglogrepl.LSN(20), loaded.Applied)
}
