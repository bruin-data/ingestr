package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/jackc/pglogrepl"
)

// Baseline manifests use JSON positions so checkpoint decoding and pruning
// leave the two bootstrap markers intact across later generations.
const cdcStateKindBaseline = "external_baseline"

type cdcBaselineTable struct {
	Position               string `json:"position"`
	Destination            string `json:"destination"`
	SourceIncarnation      string `json:"source_incarnation"`
	SourceSchema           string `json:"source_schema"`
	DestinationIncarnation string `json:"destination_incarnation"`
}

// ResolveBaseline distinguishes an unfinished adoption from a completed one.
// A matching completed boundary is only a resume hint: callers must use their
// validated managed checkpoints, never the original baseline positions.
func (m *CDCStateManager) ResolveBaseline(ctx context.Context, requestedLSN string) (pending map[string]string, completed bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending, err = m.pendingBaseline(ctx, requestedLSN)
	if err != nil || len(pending) > 0 || requestedLSN == "" {
		return pending, false, err
	}
	var manifest string
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindBaseline {
			continue
		}
		if entry.Status == destination.CDCStateStatusComplete {
			if completed && manifest != entry.Position {
				return nil, false, fmt.Errorf("conflicting external CDC baseline manifests")
			}
			manifest, completed = entry.Position, true
		}
	}
	if !completed {
		return nil, false, nil
	}
	for _, entry := range m.entries {
		if entry.StateKind == cdcStateKindBaseline && (entry.Position != manifest ||
			entry.Status != cdcStateStatusInProgress && entry.Status != destination.CDCStateStatusComplete) {
			return nil, false, fmt.Errorf("conflicting external CDC baseline manifests")
		}
	}
	var tables map[string]cdcBaselineTable
	if err := json.Unmarshal([]byte(manifest), &tables); err != nil {
		return nil, false, fmt.Errorf("invalid external CDC baseline manifest: %w", err)
	}
	if len(tables) == 0 {
		return nil, false, fmt.Errorf("invalid external CDC baseline manifest: no tables")
	}
	requested, err := pglogrepl.ParseLSN(requestedLSN)
	if err != nil || requested == 0 {
		return nil, false, fmt.Errorf("--cdc-start-lsn must be a nonzero PostgreSQL LSN")
	}
	for table, baseline := range tables {
		lsn, err := pglogrepl.ParseLSN(baseline.Position)
		if table == "" || err != nil || lsn == 0 {
			return nil, false, fmt.Errorf("invalid external CDC baseline position for %s", table)
		}
		if requested != lsn {
			return nil, false, fmt.Errorf("--cdc-start-lsn cannot change the boundary of a completed external CDC bootstrap; omit it to resume")
		}
	}
	return nil, true, nil
}

// PendingBaseline returns the original boundaries only for an unfinished
// bootstrap whose source tables and preloaded targets still match its manifest.
func (m *CDCStateManager) PendingBaseline(ctx context.Context, requestedLSN string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingBaseline(ctx, requestedLSN)
}

func (m *CDCStateManager) pendingBaseline(ctx context.Context, requestedLSN string) (map[string]string, error) {
	if err := m.load(ctx); err != nil {
		return nil, err
	}
	var manifest string
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindBaseline {
			continue
		}
		if entry.Status == destination.CDCStateStatusComplete {
			return nil, nil
		}
		if entry.Status != cdcStateStatusInProgress || manifest != "" && manifest != entry.Position {
			return nil, fmt.Errorf("conflicting external CDC baseline manifests")
		}
		manifest = entry.Position
	}
	if manifest == "" {
		return nil, nil
	}
	for _, entry := range m.entries {
		_, _, epoch, _ := decodeCDCStateEntry(entry)
		if entry.StateKind == cdcStateKindCheckpoint || epoch != 0 {
			return nil, fmt.Errorf("cannot retry external CDC bootstrap after checkpoint or snapshot invalidation")
		}
	}
	var tables map[string]cdcBaselineTable
	if err := json.Unmarshal([]byte(manifest), &tables); err != nil {
		return nil, fmt.Errorf("invalid external CDC baseline manifest: %w", err)
	}
	if len(tables) == 0 || len(tables) != len(m.destTables) {
		return nil, fmt.Errorf("cannot change the table set of an unfinished external CDC bootstrap")
	}
	positions := make(map[string]string, len(tables))
	for table, baseline := range tables {
		lsn, err := pglogrepl.ParseLSN(baseline.Position)
		if err != nil || lsn == 0 {
			return nil, fmt.Errorf("invalid external CDC baseline position for %s", table)
		}
		if requestedLSN != "" {
			requested, err := pglogrepl.ParseLSN(requestedLSN)
			if err != nil || requested != lsn {
				return nil, fmt.Errorf("--cdc-start-lsn cannot change the boundary of an unfinished external CDC bootstrap")
			}
		}
		if target, ok := m.destTables[table]; !ok || target != baseline.Destination ||
			m.currentIncarnations[table] != baseline.SourceIncarnation ||
			compactSchemaFingerprint(m.currentSchemas[table]) != baseline.SourceSchema {
			return nil, fmt.Errorf("source table or destination mapping changed during external CDC bootstrap for %s", table)
		}
		current, exists, err := m.currentDestinationIncarnation(ctx, table)
		if err != nil {
			return nil, err
		}
		if !exists || current != baseline.DestinationIncarnation {
			return nil, fmt.Errorf("destination table %s changed during external CDC bootstrap", baseline.Destination)
		}
		positions[table] = baseline.Position
	}
	return positions, nil
}

// BeginBaseline records the bootstrap intent before any run state, then
// certifies every target before sealing the bootstrap and allowing WAL reads.
func (m *CDCStateManager) BeginBaseline(ctx context.Context, positions map[string]string) error {
	manifest, err := m.recordBaseline(ctx, positions)
	if err != nil {
		return err
	}
	if err := m.BeginRun(ctx, false); err != nil {
		return err
	}
	if err := m.AdoptBaseline(ctx, positions); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeState(ctx, "", "", cdcStateKindBaseline, m.generation, destination.CDCStateStatusComplete, manifest)
}

func (m *CDCStateManager) recordBaseline(ctx context.Context, positions map[string]string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.neverSnapshot {
		return "", fmt.Errorf("external CDC bootstrap requires snapshot mode never")
	}
	pending, err := m.pendingBaseline(ctx, "")
	if err != nil {
		return "", err
	}
	if len(pending) > 0 {
		if !maps.Equal(pending, positions) {
			return "", fmt.Errorf("cannot change the boundaries of an unfinished external CDC bootstrap")
		}
		for _, entry := range m.entries {
			if entry.StateKind == cdcStateKindBaseline {
				return entry.Position, nil
			}
		}
	}
	if len(m.entries) > 0 {
		return "", fmt.Errorf("--cdc-start-lsn cannot reset existing CDC state; omit it to resume")
	}
	if len(positions) == 0 || len(positions) != len(m.destTables) {
		return "", fmt.Errorf("external CDC bootstrap requires a boundary for every registered table")
	}
	tables := make(map[string]cdcBaselineTable, len(positions))
	for table, position := range positions {
		lsn, err := pglogrepl.ParseLSN(position)
		if err != nil || lsn == 0 {
			return "", fmt.Errorf("invalid external CDC baseline position for %s", table)
		}
		target, ok := m.destTables[table]
		if !ok {
			return "", fmt.Errorf("CDC state is not registered for source table %q", table)
		}
		current, exists, err := m.currentDestinationIncarnation(ctx, table)
		if err != nil {
			return "", err
		}
		if !exists || current == "" || current != m.boundDestinations[table] {
			return "", fmt.Errorf("destination table %s changed before external CDC bootstrap", target)
		}
		tables[table] = cdcBaselineTable{
			Position: position, Destination: target,
			SourceIncarnation: m.currentIncarnations[table], SourceSchema: compactSchemaFingerprint(m.currentSchemas[table]),
			DestinationIncarnation: current,
		}
	}
	encoded, err := json.Marshal(tables)
	if err != nil {
		return "", fmt.Errorf("failed to encode external CDC baseline manifest: %w", err)
	}
	if err := m.prepareTable(ctx); err != nil {
		return "", err
	}
	runID, err := newCDCStateRunID()
	if err != nil {
		return "", err
	}
	manifest := string(encoded)
	event := m.newStateWriteEventForRun(runID, "", "", cdcStateKindBaseline, m.generation, cdcStateStatusInProgress, manifest)
	if err := m.writeStateEvents(ctx, []cdcStateWriteEvent{event}); err != nil {
		return "", err
	}
	return manifest, nil
}
