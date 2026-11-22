package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
	Version           int                               `json:"v"`
	Boundary          string                            `json:"b,omitempty"`
	Key               string                            `json:"k,omitempty"`
	Incarnation       string                            `json:"i,omitempty"`
	SchemaFingerprint string                            `json:"s,omitempty"`
	Destination       string                            `json:"d,omitempty"`
	Sequence          int64                             `json:"q"`
	Attempt           string                            `json:"a,omitempty"`
	Ranges            []source.CDCSnapshotRangeProgress `json:"r,omitempty"`
}

func decodeCDCSnapshotProgress(entry destination.CDCStateEntry) (cdcSnapshotProgressState, bool) {
	var state cdcSnapshotProgressState
	if entry.StateKind != cdcStateKindSnapshotProgress || json.Unmarshal([]byte(entry.Position), &state) != nil ||
		(state.Version != 1 && state.Version != 2) || state.Sequence <= 0 {
		return state, false
	}
	if entry.Status == destination.CDCStateStatusComplete {
		return state, state.Boundary == "" && state.Key == "" && state.Attempt == "" && len(state.Ranges) == 0
	}
	if entry.Status != cdcStateStatusInProgress || state.Destination == "" ||
		(state.Version == 1 && (state.Attempt != "" || len(state.Ranges) != 0)) ||
		(state.Version == 2 && len(state.Ranges) == 0) {
		return state, false
	}
	return state, validateSnapshotProgressShape(state.progress()) == nil
}

func (state cdcSnapshotProgressState) progress() source.CDCSnapshotProgress {
	return source.CDCSnapshotProgress{
		Boundary: state.Boundary, Key: state.Key, Incarnation: state.Incarnation,
		SchemaFingerprint: state.SchemaFingerprint, Attempt: state.Attempt, Ranges: state.Ranges,
	}
}

func validateSnapshotProgressShape(progress source.CDCSnapshotProgress) error {
	if !cdcStatePositionValid(progress.Boundary) || progress.Boundary == zeroCDCPosition {
		return fmt.Errorf("invalid snapshot progress boundary %q", progress.Boundary)
	}
	if len(progress.Ranges) == 0 {
		_, err := progress.NormalizeLegacy()
		return err
	}
	if progress.Attempt == "" || progress.Incarnation == "" || progress.SchemaFingerprint == "" {
		return fmt.Errorf("snapshot range progress requires attempt, source incarnation, and schema identity")
	}
	if progress.Ranges[0].After != nil || progress.Ranges[len(progress.Ranges)-1].Upper != nil {
		return fmt.Errorf("snapshot range progress must cover the whole table")
	}
	ids := make(map[string]struct{}, len(progress.Ranges))
	arity := 0
	for i, r := range progress.Ranges {
		if _, exists := ids[r.ID]; exists || r.ID == "" {
			return fmt.Errorf("snapshot range progress has missing or duplicate identity %q", r.ID)
		}
		ids[r.ID] = struct{}{}
		if i > 0 && (r.After == nil || !slices.Equal(progress.Ranges[i-1].Upper, r.After)) {
			return fmt.Errorf("snapshot range progress has nonadjacent boundaries")
		}
		for _, key := range [][]string{r.After, r.Upper, r.Key} {
			if key == nil {
				continue
			}
			if len(key) == 0 || (arity != 0 && len(key) != arity) {
				return fmt.Errorf("snapshot range progress has inconsistent key arity")
			}
			arity = len(key)
		}
	}
	if progress.Key != "" {
		legacy := progress
		legacy.Attempt, legacy.Ranges = "", nil
		normalized, err := legacy.NormalizeLegacy()
		if err != nil || len(progress.Ranges) != 1 || !slices.Equal(normalized.Ranges[0].Key, progress.Ranges[0].Key) {
			return fmt.Errorf("legacy snapshot cursor conflicts with range progress")
		}
	}
	return nil
}

func snapshotProgressSequence(position string) (int64, bool) {
	var envelope struct {
		Sequence int64 `json:"q"`
	}
	err := json.Unmarshal([]byte(position), &envelope)
	return envelope.Sequence, err == nil && envelope.Sequence > 0
}

func (m *CDCStateManager) nextSnapshotProgressSequence() (int64, error) {
	if m.progressGeneration != m.generation {
		m.progressSequence, m.progressGeneration = 0, m.generation
	}
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindSnapshotProgress || entry.Generation != m.generation {
			continue
		}
		if sequence, ordered := snapshotProgressSequence(entry.Position); ordered {
			m.progressSequence = max(m.progressSequence, sequence)
		}
	}
	if m.progressSequence == math.MaxInt64 {
		return 0, fmt.Errorf("snapshot progress sequence exhausted; restart with a full refresh")
	}
	m.progressSequence = max(m.progressSequence+1, time.Now().UnixNano())
	return m.progressSequence, nil
}

func (m *CDCStateManager) snapshotProgressEvent(sourceTable, destTable, status string, state cdcSnapshotProgressState) (cdcStateWriteEvent, error) {
	state.Version = 1
	if len(state.Ranges) > 0 {
		state.Version = 2
	}
	sequence, err := m.nextSnapshotProgressSequence()
	if err != nil {
		return cdcStateWriteEvent{}, err
	}
	state.Sequence = sequence
	position, _ := json.Marshal(state)
	event := m.newStateWriteEvent(sourceTable, destTable, cdcStateKindSnapshotProgress, m.generation, status, string(position))
	event.entry.RecordedAt = event.recordedAt
	return event, nil
}

// latestSnapshotProgress returns the newest snapshot_progress event for a
// table across all loaded generations.
func (m *CDCStateManager) latestSnapshotProgress(sourceTable string) (destination.CDCStateEntry, cdcSnapshotProgressState, bool) {
	var latest destination.CDCStateEntry
	var latestState cdcSnapshotProgressState
	found := false
	latestOrdered := false
	for _, entry := range m.entries {
		if entry.SourceTable != sourceTable || entry.StateKind != cdcStateKindSnapshotProgress {
			continue
		}
		var state cdcSnapshotProgressState
		_ = json.Unmarshal([]byte(entry.Position), &state)
		sequence, ordered := snapshotProgressSequence(entry.Position)
		state.Sequence = sequence
		if !found || snapshotProgressNewer(entry, state, ordered, latest, latestState, latestOrdered) {
			latest, latestState, found = entry, state, true
			latestOrdered = ordered
		}
	}
	return latest, latestState, found
}

func snapshotProgressNewer(entry destination.CDCStateEntry, state cdcSnapshotProgressState, ordered bool,
	latest destination.CDCStateEntry, latestState cdcSnapshotProgressState, latestOrdered bool,
) bool {
	if entry.Generation != latest.Generation {
		return entry.Generation > latest.Generation
	}
	// A missing sequence cannot be ordered safely against valid progress,
	// including after wall-clock rollback. It fences the entire generation.
	if ordered != latestOrdered {
		return !ordered
	}
	if ordered && state.Sequence != latestState.Sequence {
		return state.Sequence > latestState.Sequence
	}
	_, valid := decodeCDCSnapshotProgress(entry)
	_, latestValid := decodeCDCSnapshotProgress(latest)
	if valid != latestValid {
		return !valid
	}
	if !entry.RecordedAt.Equal(latest.RecordedAt) {
		return entry.RecordedAt.After(latest.RecordedAt)
	}
	return entry.EventID > latest.EventID
}

// snapshotProgressReset returns an event ending the table's resumable
// snapshot, or false when none is in progress.
func (m *CDCStateManager) snapshotProgressReset(sourceTable, destTable string) (cdcStateWriteEvent, bool, error) {
	entry, _, ok := m.latestSnapshotProgress(sourceTable)
	_, valid := decodeCDCSnapshotProgress(entry)
	if !ok || (valid && entry.Status == destination.CDCStateStatusComplete) {
		return cdcStateWriteEvent{}, false, nil
	}
	event, err := m.snapshotProgressEvent(sourceTable, destTable, destination.CDCStateStatusComplete, cdcSnapshotProgressState{})
	return event, err == nil, err
}

// snapshotProgressEvents records the progress a token carries for snapshots
// it does not complete. Progress is bound to the physical target the
// snapshot writes into; without a bound target it cannot be verified later
// and is not recorded.
func (m *CDCStateManager) snapshotProgressEvents(token source.CDCStateCommitToken) ([]cdcStateWriteEvent, error) {
	var events []cdcStateWriteEvent
	for sourceTable, progress := range token.SnapshotProgress {
		if _, completed := token.SnapshotPositions[sourceTable]; completed {
			continue
		}
		if err := validateSnapshotProgressShape(progress); err != nil {
			return nil, fmt.Errorf("invalid snapshot progress for %s: %w", sourceTable, err)
		}
		destTable, registered := m.destTables[sourceTable]
		boundDestination := m.boundDestinations[sourceTable]
		if !registered || boundDestination == "" {
			continue
		}
		if entry, previous, found := m.latestSnapshotProgress(sourceTable); found {
			if _, valid := decodeCDCSnapshotProgress(entry); !valid {
				return nil, fmt.Errorf("invalid saved snapshot progress for %s; invalidate the snapshot before replacing its progress", sourceTable)
			}
			if entry.Status != destination.CDCStateStatusComplete {
				if previous.Destination != boundDestination || entry.DestinationTable != destTable {
					return nil, fmt.Errorf("snapshot progress destination changed for %s", sourceTable)
				}
				if err := validateSnapshotProgressContinuation(previous.progress(), progress); err != nil {
					return nil, fmt.Errorf("snapshot progress changed for %s: %w", sourceTable, err)
				}
			}
		}
		progress = progress.Clone()
		event, err := m.snapshotProgressEvent(sourceTable, destTable, cdcStateStatusInProgress, cdcSnapshotProgressState{
			Boundary:          progress.Boundary,
			Key:               progress.Key,
			Incarnation:       progress.Incarnation,
			SchemaFingerprint: progress.SchemaFingerprint,
			Destination:       boundDestination,
			Attempt:           progress.Attempt,
			Ranges:            progress.Ranges,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func validateSnapshotProgressContinuation(previous, next source.CDCSnapshotProgress) error {
	previous, err := previous.NormalizeLegacy()
	if err != nil {
		return err
	}
	next, err = next.NormalizeLegacy()
	if err != nil {
		return err
	}
	if previous.Attempt != next.Attempt || previous.Boundary != next.Boundary ||
		previous.Incarnation != next.Incarnation || previous.SchemaFingerprint != next.SchemaFingerprint || len(previous.Ranges) != len(next.Ranges) {
		return fmt.Errorf("attempt, boundary, source identity, or range count changed without invalidation")
	}
	for i, before := range previous.Ranges {
		after := next.Ranges[i]
		if before.ID != after.ID || !slices.Equal(before.After, after.After) || !slices.Equal(before.Upper, after.Upper) {
			return fmt.Errorf("range %s changed its stable boundaries", before.ID)
		}
		if (before.Key != nil && after.Key == nil) || (before.Complete && (!after.Complete || !slices.Equal(before.Key, after.Key))) {
			return fmt.Errorf("range %s discarded durable progress", before.ID)
		}
	}
	return nil
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
	if !ok {
		return source.CDCSnapshotProgress{}, false, nil
	}
	if _, valid := decodeCDCSnapshotProgress(entry); !valid {
		return source.CDCSnapshotProgress{}, false, fmt.Errorf("invalid or unsupported saved snapshot progress for %s; restart with a full refresh", sourceTable)
	}
	if entry.Status != cdcStateStatusInProgress || entry.DestinationTable != m.destTables[sourceTable] {
		return source.CDCSnapshotProgress{}, false, nil
	}
	current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
	if err != nil {
		return source.CDCSnapshotProgress{}, false, err
	}
	if !exists || current != state.Destination {
		return source.CDCSnapshotProgress{}, false, nil
	}
	progress, err := state.progress().NormalizeLegacy()
	return progress, err == nil, err
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
