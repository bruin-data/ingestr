package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bruin-data/ingestr/pkg/destination"
)

const cdcRefreshStateKind = "refresh"

type cdcRefresh struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	ConnectorID string   `json:"connector_id"`
	Generation  int64    `json:"generation"`
	Tables      []string `json:"tables"`
	complete    bool
	writer      *CDCStateManager
}

// PrepareRefresh records the refresh before any checkpoint is invalidated or
// destination data is removed. The operation is looked up by refresh ID alone;
// its generation links it to the separately identified CDC checkpoint history.
// A retry can only resume snapshots from this generation or a later one.
func (m *CDCStateManager) PrepareRefresh(ctx context.Context, id, scope string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		return false, fmt.Errorf("refresh ID is empty")
	}
	if err := m.prepareTable(ctx); err != nil {
		return false, err
	}
	if err := m.load(ctx); err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte("ingestr:refresh:" + id))
	key := fmt.Sprintf("%x", sum[:])
	writer, err := newCDCStateManager(m.dest, key, m.stateTable)
	if err != nil {
		return false, err
	}
	writer.runID, err = newCDCStateRunID()
	if err != nil {
		return false, err
	}
	refresh, err := loadRefresh(ctx, m.reader, m.stateTable, key, id, scope)
	if err != nil {
		return false, err
	}
	if refresh != nil && refresh.ConnectorID != m.connectorID {
		return false, fmt.Errorf("refresh ID %q belongs to CDC checkpoint identity %q; the current identity is %q", id, refresh.ConnectorID, m.connectorID)
	}
	if refresh != nil {
		if m.generation+1 < refresh.Generation || (refresh.complete && m.generation < refresh.Generation) {
			return false, fmt.Errorf("refresh ID %q is missing its associated CDC checkpoint history", id)
		}
		if !refresh.complete && m.generation >= refresh.Generation {
			associated := false
			for _, entry := range m.entries {
				if entry.Generation == m.generation && entry.StateKind == cdcStateKindRun {
					if entry.DestinationTable != "refresh:"+key {
						return false, fmt.Errorf("unfinished refresh ID %q was interrupted by a different CDC operation", id)
					}
					associated = true
				}
			}
			if !associated {
				return false, fmt.Errorf("unfinished refresh ID %q has no associated CDC run", id)
			}
		}
		refresh.writer = writer
		m.refresh = refresh
		return m.generation < refresh.Generation, nil
	}
	tables := make([]string, 0, len(m.destTables))
	for table := range m.destTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	refresh = &cdcRefresh{ID: id, Scope: scope, ConnectorID: m.connectorID, Generation: m.generation + 1, Tables: tables, writer: writer}
	if err := refresh.write(ctx, cdcStateStatusInProgress); err != nil {
		return false, err
	}
	m.refresh = refresh
	return true, nil
}

func (r *cdcRefresh) write(ctx context.Context, status string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	event := r.writer.newStateWriteEvent("", "", cdcRefreshStateKind, r.Generation, status, string(data))
	event.entry.EventID = fmt.Sprintf("%s-%x", r.writer.runID, sha256.Sum256([]byte(string(data)+status)))
	return r.writer.writeStateEvents(ctx, []cdcStateWriteEvent{event})
}

func (m *CDCStateManager) completeRefresh(ctx context.Context) error {
	if m.refresh == nil || m.refresh.complete || m.generation < m.refresh.Generation {
		return nil
	}
	for _, table := range m.refresh.Tables {
		if _, selected := m.destTables[table]; !selected {
			continue
		}
		snapshot := m.states[cdcStateKey{runID: m.runID, sourceTable: table, kind: cdcStateKindSnapshot}]
		if !snapshot.complete {
			return nil
		}
	}
	if err := m.refresh.write(ctx, destination.CDCStateStatusComplete); err != nil {
		return err
	}
	m.refresh.complete = true
	return nil
}

// RefreshConnectorID restores the operation's checkpoint association before
// leases and source slots are acquired. Recomputed connector identities are
// not part of the refresh lookup key.
func RefreshConnectorID(ctx context.Context, dest destination.Destination, id, scope, stagingDataset string) (string, error) {
	reader, ok := dest.(destination.CDCStateReader)
	if !ok {
		return "", fmt.Errorf("destination does not support durable refresh metadata")
	}
	sum := sha256.Sum256([]byte("ingestr:refresh:" + id))
	key := fmt.Sprintf("%x", sum[:])
	table := managedCDCStateTableName(dest, cdcStateTableName, stagingDataset)
	record, err := loadRefresh(ctx, reader, table, key, id, scope)
	if err != nil || record == nil {
		return "", err
	}
	return record.ConnectorID, nil
}

func loadRefresh(ctx context.Context, reader destination.CDCStateReader, table, key, id, scope string) (*cdcRefresh, error) {
	entries, err := reader.LoadCDCState(ctx, table, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load refresh request: %w", err)
	}
	var refresh *cdcRefresh
	for _, entry := range entries {
		if entry.StateKind != cdcRefreshStateKind {
			return nil, fmt.Errorf("unexpected metadata under refresh ID %q", id)
		}
		var record cdcRefresh
		if err := json.Unmarshal([]byte(entry.Position), &record); err != nil {
			return nil, fmt.Errorf("invalid refresh request metadata: %w", err)
		}
		if record.ID != id || record.Scope != scope {
			return nil, fmt.Errorf("refresh ID %q is already assigned to a different source, destination, or table selection", id)
		}
		if record.Generation <= 0 || record.ConnectorID == "" || (refresh != nil && (record.Generation != refresh.Generation || record.ConnectorID != refresh.ConnectorID)) {
			return nil, fmt.Errorf("refresh ID %q has conflicting CDC generations or checkpoint identities", id)
		}
		completed := entry.Status == destination.CDCStateStatusComplete
		if refresh != nil {
			completed = completed || refresh.complete
		}
		refresh = &record
		refresh.complete = completed
	}
	return refresh, nil
}
