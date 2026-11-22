package strategy

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

const progressConnector = "snapshot-progress"

func progressManager(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	manager, err := NewCDCStateManager(dest, progressConnector, "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), "public.orders", "raw.orders", "100"))
	return manager
}

// startProgress begins a run whose replacement snapshot of public.orders is
// bound to its target and has made rows up to key 41 durable.
func startProgress(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	ctx := t.Context()
	manager := progressManager(t, dest)
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.InvalidateSnapshot(ctx, "public.orders", "raw.orders", "100"))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.orders", "raw.orders"))
	require.NoError(t, manager.Persist(ctx, progressToken(`["41"]`)))
	return manager
}

func progressToken(key string) source.CDCStateCommitToken {
	return source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{
		"public.orders": {Boundary: "00000000/00000020", Key: key, Incarnation: "100", SchemaFingerprint: "fp"},
	}}
}

func resumeAfterRestart(t *testing.T, dest *cdcStateDestination) (source.CDCSnapshotProgress, bool) {
	t.Helper()
	resume, ok, err := progressManager(t, dest).SnapshotResume(context.Background(), "public.orders")
	require.NoError(t, err)
	return resume, ok
}

func TestSnapshotProgressResumesAfterRestart(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.Persist(t.Context(), progressToken(`["73"]`)))

	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok)
	expected, err := progressToken(`["73"]`).SnapshotProgress["public.orders"].NormalizeLegacy()
	require.NoError(t, err)
	require.Equal(t, expected, resume)
}

func TestSnapshotProgressSurvivesResumedRunsUntilCompletion(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	startProgress(t, dest)

	resumed := progressManager(t, dest)
	require.NoError(t, resumed.BeginRun(ctx, false))
	require.NoError(t, resumed.ResumeSnapshot(ctx, "public.orders", "raw.orders", "100"))
	require.NoError(t, resumed.BindDestinationIncarnation(ctx, "public.orders", "raw.orders"))
	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok, "resuming must not discard the progress it continues")
	require.Equal(t, `["41"]`, resume.Key)

	require.NoError(t, resumed.Persist(ctx, progressToken(`["99"]`)))
	resume, ok = resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, `["99"]`, resume.Key)

	require.NoError(t, resumed.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000020"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	}))
	_, ok = resumeAfterRestart(t, dest)
	require.False(t, ok, "a completed snapshot must not resume")
}

func TestSnapshotProgressResetByInvalidation(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok, "a new replacement can empty the target, so older progress must not resume")
}

func TestSnapshotProgressResetByBatchInvalidation(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	require.NoError(t, manager.InvalidateBatchSnapshot(t.Context(), "public.orders", "raw.orders"))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotProgressRequiresSamePhysicalTarget(t *testing.T) {
	dest := newCDCStateDestination()
	startProgress(t, dest)
	dest.incarnations["raw.orders"] = "replaced-target"

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotProgressNeedsBoundTarget(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	manager := progressManager(t, dest)
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.Persist(ctx, progressToken(`["41"]`)))

	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok, "progress that cannot be tied to a physical target is not recorded")
}

func TestSnapshotProgressPrunesSupersededEvents(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	for _, key := range []string{`["50"]`, `["60"]`, `["70"]`} {
		require.NoError(t, manager.Persist(t.Context(), progressToken(key)))
	}

	stale := manager.supersededSnapshotProgress()
	require.Len(t, stale, 3)
	entry, state, ok := manager.latestSnapshotProgress("public.orders")
	require.True(t, ok)
	require.NotContains(t, stale, entry.EventID)
	require.Equal(t, `["70"]`, state.Key)
}

func durableRangeProgress() source.CDCSnapshotProgress {
	return source.CDCSnapshotProgress{
		Boundary: "00000000/00000020", Incarnation: "100", SchemaFingerprint: "fp", Attempt: "snapshot-attempt",
		Ranges: []source.CDCSnapshotRangeProgress{
			{ID: "low", Upper: []string{"10"}},
			{ID: "middle", After: []string{"10"}, Upper: []string{"20"}},
			{ID: "high", After: []string{"20"}},
		},
	}
}

func snapshotRangeToken(progress source.CDCSnapshotProgress) source.CDCStateCommitToken {
	return source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"public.orders": progress}}
}

func startRangeProgress(t *testing.T, dest *cdcStateDestination) *CDCStateManager {
	t.Helper()
	manager := progressManager(t, dest)
	require.NoError(t, manager.BeginRun(t.Context(), false))
	require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"))
	require.NoError(t, manager.BindDestinationIncarnation(t.Context(), "public.orders", "raw.orders"))
	return manager
}

func TestSnapshotRangeProgressPersistsInitialAndOutOfOrderCompletion(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startRangeProgress(t, dest)
	progress := durableRangeProgress()
	require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok, "the initial range plan must survive before any rows are loaded")
	require.Equal(t, progress, resume)

	progress.Ranges[2].Key = []string{"500"}
	progress.Ranges[2].Complete = true
	progress.Ranges[1].Complete = true
	require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
	resume, ok = resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, progress, resume)
	require.False(t, resume.Ranges[0].Complete, "a fast high range cannot complete the low range")
	require.Nil(t, resume.Ranges[1].Key, "an empty completed range needs no cursor")

	progress.Ranges[0].Key = []string{"10"}
	progress.Ranges[0].Complete = true
	require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
	resume, ok = resumeAfterRestart(t, dest)
	require.True(t, ok, "all ranges exhausted is not a durable table completion")
	require.Equal(t, progress, resume)
	entry, encoded, found := manager.latestSnapshotProgress("public.orders")
	require.True(t, found)
	require.Equal(t, 2, encoded.Version)
	require.Equal(t, cdcStateStatusInProgress, entry.Status)
	require.Empty(t, encoded.Key)

	require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions:    map[string]string{"public.orders": progress.Boundary},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
		SnapshotSchemas:      map[string]string{"public.orders": "fp"},
	}))
	_, ok = resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotRangeProgressEmptyTableCompletesExplicitly(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startRangeProgress(t, dest)
	progress := durableRangeProgress()
	progress.Ranges = []source.CDCSnapshotRangeProgress{{ID: "empty", Complete: true}}
	require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, progress, resume)
}

func TestSnapshotRangeProgressRejectsChangedPlanAndLostProgress(t *testing.T) {
	for name, mutate := range map[string]func(*source.CDCSnapshotProgress){
		"attempt":            func(p *source.CDCSnapshotProgress) { p.Attempt = "different-attempt" },
		"boundary":           func(p *source.CDCSnapshotProgress) { p.Boundary = "00000000/00000021" },
		"source incarnation": func(p *source.CDCSnapshotProgress) { p.Incarnation = "101" },
		"schema":             func(p *source.CDCSnapshotProgress) { p.SchemaFingerprint = "new-shape" },
		"range identity":     func(p *source.CDCSnapshotProgress) { p.Ranges[0].ID = "new-range" },
		"range split": func(p *source.CDCSnapshotProgress) {
			p.Ranges[0].Upper[0], p.Ranges[1].After[0] = "11", "11"
		},
		"range count": func(p *source.CDCSnapshotProgress) {
			p.Ranges = []source.CDCSnapshotRangeProgress{{ID: "all", Key: []string{"500"}, Complete: true}}
		},
		"lost cursor":              func(p *source.CDCSnapshotProgress) { p.Ranges[0].Key = nil },
		"lost completion":          func(p *source.CDCSnapshotProgress) { p.Ranges[2].Complete = false },
		"completed cursor changed": func(p *source.CDCSnapshotProgress) { p.Ranges[2].Key[0] = "501" },
	} {
		t.Run(name, func(t *testing.T) {
			dest := newCDCStateDestination()
			manager := startRangeProgress(t, dest)
			progress := durableRangeProgress()
			progress.Ranges[0].Key = []string{"2"}
			progress.Ranges[2].Key, progress.Ranges[2].Complete = []string{"500"}, true
			require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
			changed := progress.Clone()
			mutate(&changed)
			require.Error(t, manager.Persist(t.Context(), snapshotRangeToken(changed)))
			resume, ok := resumeAfterRestart(t, dest)
			require.True(t, ok)
			require.Equal(t, progress, resume, "failed persistence must retain the last durable vector")
		})
	}
}

func TestSnapshotProgressLegacyUpgradeKeepsStableAttemptAndBoundary(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	resume, ok := resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Len(t, resume.Ranges, 1)
	require.Nil(t, resume.Ranges[0].After)
	require.Nil(t, resume.Ranges[0].Upper)
	require.Equal(t, []string{"41"}, resume.Ranges[0].Key)
	attempt := resume.Attempt

	resume.Key = ""
	resume.Ranges[0].Key = []string{"73"}
	require.NoError(t, manager.Persist(t.Context(), snapshotRangeToken(resume)))
	upgraded, ok := resumeAfterRestart(t, dest)
	require.True(t, ok)
	require.Equal(t, attempt, upgraded.Attempt)
	require.Equal(t, "00000000/00000020", upgraded.Boundary)
	require.Equal(t, []string{"73"}, upgraded.Ranges[0].Key)
	_, state, _ := manager.latestSnapshotProgress("public.orders")
	require.Equal(t, 2, state.Version)

	legacy, err := progressToken(`["999"]`).SnapshotProgress["public.orders"].NormalizeLegacy()
	require.NoError(t, err)
	require.Equal(t, attempt, legacy.Attempt, "the saved cursor must not change the attempt identity")
}

func TestSnapshotProgressRejectsMalformedKeysAndRangeVectors(t *testing.T) {
	for _, key := range []string{"", "null", "[]", "[null]", `[1]`, `["a",null]`, `{}`} {
		t.Run("legacy_"+key, func(t *testing.T) {
			_, err := progressToken(key).SnapshotProgress["public.orders"].NormalizeLegacy()
			require.Error(t, err)
		})
	}
	for name, mutate := range map[string]func(*source.CDCSnapshotProgress){
		"missing attempt":           func(p *source.CDCSnapshotProgress) { p.Attempt = "" },
		"missing schema":            func(p *source.CDCSnapshotProgress) { p.SchemaFingerprint = "" },
		"missing incarnation":       func(p *source.CDCSnapshotProgress) { p.Incarnation = "" },
		"missing range identity":    func(p *source.CDCSnapshotProgress) { p.Ranges[0].ID = "" },
		"duplicate range identity":  func(p *source.CDCSnapshotProgress) { p.Ranges[0].ID = p.Ranges[1].ID },
		"bounded outer":             func(p *source.CDCSnapshotProgress) { p.Ranges[0].After = []string{"0"} },
		"nonadjacent boundaries":    func(p *source.CDCSnapshotProgress) { p.Ranges[1].After[0] = "11" },
		"empty cursor":              func(p *source.CDCSnapshotProgress) { p.Ranges[0].Key = []string{} },
		"wrong arity":               func(p *source.CDCSnapshotProgress) { p.Ranges[0].Key = []string{"2", "a"} },
		"conflicting legacy cursor": func(p *source.CDCSnapshotProgress) { p.Key = `["41"]` },
	} {
		t.Run(name, func(t *testing.T) {
			dest := newCDCStateDestination()
			manager := startRangeProgress(t, dest)
			progress := durableRangeProgress()
			mutate(&progress)
			require.Error(t, manager.Persist(t.Context(), snapshotRangeToken(progress)))
			_, ok := resumeAfterRestart(t, dest)
			require.False(t, ok)
		})
	}
	for _, raw := range []string{`{"id":"0","k":[null]}`, `{"id":"0","a":[1]}`, `{"id":"0","u":[true]}`} {
		var r source.CDCSnapshotRangeProgress
		require.Error(t, json.Unmarshal([]byte(raw), &r))
	}
}

func TestSnapshotProgressInvalidNewestBlocksResumeAndSurvivesPruning(t *testing.T) {
	for name, corrupt := range map[string]func(cdcSnapshotProgressState) (string, string){
		"malformed json": func(_ cdcSnapshotProgressState) (string, string) { return `{`, cdcStateStatusInProgress },
		"unknown version": func(s cdcSnapshotProgressState) (string, string) {
			s.Version = 99
			data, _ := json.Marshal(s)
			return string(data), cdcStateStatusInProgress
		},
		"incomplete v2": func(s cdcSnapshotProgressState) (string, string) {
			s.Version = 2
			data, _ := json.Marshal(s)
			return string(data), cdcStateStatusInProgress
		},
		"invalid reset": func(s cdcSnapshotProgressState) (string, string) {
			data, _ := json.Marshal(s)
			return string(data), destination.CDCStateStatusComplete
		},
	} {
		t.Run(name, func(t *testing.T) {
			dest := newCDCStateDestination()
			manager := startProgress(t, dest)
			older, state, _ := manager.latestSnapshotProgress("public.orders")
			state.Sequence++
			position, status := corrupt(state)
			event := manager.newStateWriteEvent("public.orders", "raw.orders", cdcStateKindSnapshotProgress, manager.generation, status, position)
			event.entry.RecordedAt = event.recordedAt
			require.NoError(t, manager.writeStateEvents(t.Context(), []cdcStateWriteEvent{event}))
			reversed := slices.Clone(manager.entries)
			slices.Reverse(reversed)
			for _, entries := range [][]destination.CDCStateEntry{manager.entries, reversed} {
				manager.entries = entries
				latest, _, found := manager.latestSnapshotProgress("public.orders")
				require.True(t, found)
				require.Equal(t, event.entry.EventID, latest.EventID)
			}
			_, ok, err := progressManager(t, dest).SnapshotResume(t.Context(), "public.orders")
			require.ErrorContains(t, err, "invalid or unsupported")
			require.False(t, ok)
			stale := manager.supersededSnapshotProgress()
			require.Contains(t, stale, older.EventID)
			require.NotContains(t, stale, event.entry.EventID)
			require.Error(t, manager.Persist(t.Context(), progressToken(`["73"]`)))
			require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"))
			_, ok, err = progressManager(t, dest).SnapshotResume(t.Context(), "public.orders")
			require.False(t, ok, "a reset must never resurrect older progress")
			if name == "malformed json" {
				require.Error(t, err, "without a saved timestamp or sequence, full refresh is required to order the corrupt event")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSnapshotProgressMalformedNewGenerationSupersedesValidOlder(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	old, _, _ := manager.latestSnapshotProgress("public.orders")
	broken := old
	broken.EventID, broken.Generation, broken.Position = "malformed-new-generation", old.Generation+1, "{"
	broken.RecordedAt = time.Time{}
	manager.entries = append(manager.entries, broken)
	newest, _, found := manager.latestSnapshotProgress("public.orders")
	require.True(t, found)
	require.Equal(t, broken.EventID, newest.EventID)
	require.Contains(t, manager.supersededSnapshotProgress(), old.EventID)
}

func TestSnapshotProgressCloneOwnsEveryRangeKey(t *testing.T) {
	original := durableRangeProgress()
	original.Ranges[1].Key = []string{"12"}
	cloned := original.Clone()
	cloned.Ranges[0].Upper[0] = "11"
	cloned.Ranges[1].After[0] = "11"
	cloned.Ranges[1].Key[0] = "13"
	cloned.Ranges[2].Complete = true
	require.Equal(t, "10", original.Ranges[0].Upper[0])
	require.Equal(t, "10", original.Ranges[1].After[0])
	require.Equal(t, "12", original.Ranges[1].Key[0])
	require.False(t, original.Ranges[2].Complete)
}

func TestSnapshotProgressSequenceOrdersMalformedVectorsAcrossClockRollback(t *testing.T) {
	for _, corrupt := range []string{
		`{"v":99,"q":3}`,
		`{"v":2,"q":3,"r":[{"id":"0","k":[null]}]}`,
		`{`,
	} {
		entries := make([]destination.CDCStateEntry, 3)
		for i := range 2 {
			encoded, err := json.Marshal(cdcSnapshotProgressState{
				Version: 1, Boundary: "00000000/00000020", Key: `["41"]`,
				Incarnation: "100", SchemaFingerprint: "fp", Destination: "target", Sequence: int64(i + 1),
			})
			require.NoError(t, err)
			entries[i] = destination.CDCStateEntry{
				EventID: string(rune('a' + i)), SourceTable: "public.orders", StateKind: cdcStateKindSnapshotProgress,
				Generation: 1, Status: cdcStateStatusInProgress, Position: string(encoded),
			}
		}
		entries[0].RecordedAt = time.Unix(100, 0)
		entries[1].RecordedAt = time.Unix(50, 0)
		entries[2] = destination.CDCStateEntry{
			EventID: "corrupt", SourceTable: "public.orders", StateKind: cdcStateKindSnapshotProgress,
			Generation: 1, Status: cdcStateStatusInProgress, Position: corrupt, RecordedAt: time.Unix(75, 0),
		}
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			manager := &CDCStateManager{entries: []destination.CDCStateEntry{entries[order[0]], entries[order[1]], entries[order[2]]}}
			latest, _, found := manager.latestSnapshotProgress("public.orders")
			require.True(t, found)
			require.Equal(t, "corrupt", latest.EventID, "wall time and entry order cannot hide a newer invalid sequence")
			require.NotContains(t, manager.supersededSnapshotProgress(), "corrupt")
		}
	}
}

func TestSnapshotProgressResetAdvancesUnknownVersionSequence(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	_, state, _ := manager.latestSnapshotProgress("public.orders")
	state.Version, state.Sequence = 99, time.Now().Add(time.Hour).UnixNano()
	data, err := json.Marshal(state)
	require.NoError(t, err)
	event := manager.newStateWriteEvent("public.orders", "raw.orders", cdcStateKindSnapshotProgress, manager.generation, cdcStateStatusInProgress, string(data))
	require.NoError(t, manager.writeStateEvents(t.Context(), []cdcStateWriteEvent{event}))
	require.NoError(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"))
	_, reset, found := manager.latestSnapshotProgress("public.orders")
	require.True(t, found)
	require.Greater(t, reset.Sequence, state.Sequence)
	_, ok := resumeAfterRestart(t, dest)
	require.False(t, ok)
}

func TestSnapshotProgressSequenceOverflowFailsBeforeWritingAndRecoversInNewGeneration(t *testing.T) {
	dest := newCDCStateDestination()
	manager := startProgress(t, dest)
	data, err := json.Marshal(cdcSnapshotProgressState{Version: 99, Sequence: math.MaxInt64})
	require.NoError(t, err)
	event := manager.newStateWriteEvent("public.orders", "raw.orders", cdcStateKindSnapshotProgress, manager.generation, cdcStateStatusInProgress, string(data))
	require.NoError(t, manager.writeStateEvents(t.Context(), []cdcStateWriteEvent{event}))
	writes := dest.cdcWrites
	require.ErrorContains(t, manager.InvalidateSnapshot(t.Context(), "public.orders", "raw.orders", "100"), "sequence exhausted")
	require.ErrorContains(t, manager.Persist(t.Context(), source.CDCStateCommitToken{
		SnapshotPositions: map[string]string{"public.orders": "00000000/00000020"},
	}), "sequence exhausted")
	require.Equal(t, writes, dest.cdcWrites)
	manager.generation++
	sequence, err := manager.nextSnapshotProgressSequence()
	require.NoError(t, err)
	require.Positive(t, sequence)
	require.Less(t, sequence, int64(math.MaxInt64))
}
