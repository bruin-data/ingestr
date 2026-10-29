package strategy

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// cdcSnapshotProgressState is the position of a snapshot_progress event. An
// in-progress event resumes a snapshot; a complete event (no boundary) marks
// that no snapshot of the table is resumable. Destination is the digest of
// the physical target the progress was made durable in. Events order by
// generation, then Sequence.
type cdcSnapshotProgressState struct {
	Version           int    `json:"v"`
	Boundary          string `json:"b,omitempty"`
	Key               string `json:"k,omitempty"`
	Incarnation       string `json:"i,omitempty"`
	SchemaFingerprint string `json:"s,omitempty"`
	Destination       string `json:"d,omitempty"`
	Sequence          int64  `json:"q"`
}

func decodeCDCSnapshotProgress(entry destination.CDCStateEntry) (cdcSnapshotProgressState, bool) {
	var state cdcSnapshotProgressState
	if entry.StateKind != cdcStateKindSnapshotProgress || json.Unmarshal([]byte(entry.Position), &state) != nil || state.Version != 1 {
		return state, false
	}
	return state, true
}

func (m *CDCStateManager) nextSnapshotProgressSequence() int64 {
	m.progressSequence = max(m.progressSequence+1, time.Now().UnixNano())
	return m.progressSequence
}

func (m *CDCStateManager) snapshotProgressEvent(sourceTable, destTable, status string, state cdcSnapshotProgressState) cdcStateWriteEvent {
	state.Version = 1
	state.Sequence = m.nextSnapshotProgressSequence()
	position, _ := json.Marshal(state)
	return m.newStateWriteEvent(sourceTable, destTable, cdcStateKindSnapshotProgress, m.generation, status, string(position))
}

// latestSnapshotProgress returns the newest snapshot_progress event for a
// table across all loaded generations.
func (m *CDCStateManager) latestSnapshotProgress(sourceTable string) (destination.CDCStateEntry, cdcSnapshotProgressState, bool) {
	var latest destination.CDCStateEntry
	var latestState cdcSnapshotProgressState
	found := false
	for _, entry := range m.entries {
		if entry.SourceTable != sourceTable {
			continue
		}
		state, ok := decodeCDCSnapshotProgress(entry)
		if !ok {
			continue
		}
		if !found || entry.Generation > latest.Generation ||
			(entry.Generation == latest.Generation && state.Sequence > latestState.Sequence) {
			latest, latestState, found = entry, state, true
		}
	}
	return latest, latestState, found
}

// snapshotProgressReset returns an event ending the table's resumable
// snapshot, or false when none is in progress.
func (m *CDCStateManager) snapshotProgressReset(sourceTable, destTable string) (cdcStateWriteEvent, bool) {
	entry, _, ok := m.latestSnapshotProgress(sourceTable)
	if !ok || entry.Status != cdcStateStatusInProgress {
		return cdcStateWriteEvent{}, false
	}
	return m.snapshotProgressEvent(sourceTable, destTable, destination.CDCStateStatusComplete, cdcSnapshotProgressState{}), true
}

// snapshotProgressEvents records the progress a token carries for snapshots
// it does not complete. Progress is bound to the physical target the
// snapshot writes into; without a bound target it cannot be verified later
// and is not recorded.
func (m *CDCStateManager) snapshotProgressEvents(token source.CDCStateCommitToken) []cdcStateWriteEvent {
	var events []cdcStateWriteEvent
	for sourceTable, progress := range token.SnapshotProgress {
		if _, completed := token.SnapshotPositions[sourceTable]; completed {
			continue
		}
		destTable, registered := m.destTables[sourceTable]
		boundDestination := m.boundDestinations[sourceTable]
		if !registered || boundDestination == "" || progress.Key == "" || !cdcStatePositionValid(progress.Boundary) {
			continue
		}
		events = append(events, m.snapshotProgressEvent(sourceTable, destTable, cdcStateStatusInProgress, cdcSnapshotProgressState{
			Boundary:          progress.Boundary,
			Key:               progress.Key,
			Incarnation:       progress.Incarnation,
			SchemaFingerprint: progress.SchemaFingerprint,
			Destination:       boundDestination,
		}))
	}
	return events
}

// SnapshotResume returns the interrupted snapshot a table can resume: the
// newest progress, provided no later invalidation or completion superseded it
// and the destination table is still the physical table it was made durable
// in. The source checks the table incarnation and schema.
func (m *CDCStateManager) SnapshotResume(ctx context.Context, sourceTable string) (source.CDCSnapshotProgress, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.refresh != nil {
		return source.CDCSnapshotProgress{}, false, nil
	}
	if err := m.load(ctx); err != nil {
		return source.CDCSnapshotProgress{}, false, err
	}
	entry, state, ok := m.latestSnapshotProgress(sourceTable)
	if !ok || entry.Status != cdcStateStatusInProgress || entry.DestinationTable != m.destTables[sourceTable] {
		return source.CDCSnapshotProgress{}, false, nil
	}
	current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
	if err != nil {
		return source.CDCSnapshotProgress{}, false, err
	}
	if !exists || current != state.Destination {
		return source.CDCSnapshotProgress{}, false, nil
	}
	return source.CDCSnapshotProgress{
		Boundary:          state.Boundary,
		Key:               state.Key,
		Incarnation:       state.Incarnation,
		SchemaFingerprint: state.SchemaFingerprint,
	}, true, nil
}

// supersededSnapshotProgress lists every snapshot_progress event except the
// newest per table, which alone decides whether a snapshot resumes.
func (m *CDCStateManager) supersededSnapshotProgress() []string {
	latest := make(map[string]string)
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindSnapshotProgress {
			continue
		}
		if _, seen := latest[entry.SourceTable]; seen {
			continue
		}
		newest, _, ok := m.latestSnapshotProgress(entry.SourceTable)
		if ok {
			latest[entry.SourceTable] = newest.EventID
		}
	}
	var stale []string
	for _, entry := range m.entries {
		if entry.StateKind == cdcStateKindSnapshotProgress && entry.EventID != "" && entry.EventID != latest[entry.SourceTable] {
			stale = append(stale, entry.EventID)
		}
	}
	return stale
}
