package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	cdcChunkTableName      = "cdc_snapshot_wal"
	cdcChunkPartKind       = "snapshot_blob_part"
	cdcChunkCommitKind     = "snapshot_blob_commit"
	cdcChunkFragmentBytes  = 384 << 10
	cdcChunkWriteBatchSize = 8
)

type cdcChunkStore struct {
	manager  *CDCStateManager
	table    string
	prepared bool
}

type cdcChunkRecord struct {
	Version  int    `json:"v"`
	Sequence uint64 `json:"q"`
	ID       string `json:"id"`
	Part     int    `json:"p,omitempty"`
	Parts    int    `json:"n,omitempty"`
	Bytes    int    `json:"b,omitempty"`
	Digest   string `json:"h,omitempty"`
	Data     string `json:"d,omitempty"`
	Deleted  bool   `json:"x,omitempty"`
}

func (m *CDCStateManager) ChunkStore() source.CDCChunkStore {
	return &cdcChunkStore{manager: m, table: m.chunkTableName()}
}

func (m *CDCStateManager) chunkTableName() string {
	prefix := m.stateTable[:strings.LastIndexByte(m.stateTable, '.')+1]
	return prefix + cdcChunkTableName
}

func (m *CDCStateManager) validateChunkTableTarget(ctx context.Context, target string) error {
	journal := m.chunkTableName()
	if target == journal {
		return fmt.Errorf("CDC destination table %q is reserved for managed snapshot state", target)
	}
	provider, ok := m.dest.(destination.CDCTargetIdentityProvider)
	if !ok {
		return nil
	}
	if m.chunkTableIdentity == "" {
		identity, err := provider.CanonicalCDCTarget(ctx, journal)
		if err != nil {
			return fmt.Errorf("failed to resolve managed snapshot table identity: %w", err)
		}
		m.chunkTableIdentity = identity
	}
	identity, err := provider.CanonicalCDCTarget(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to resolve CDC destination table identity: %w", err)
	}
	if identity == m.chunkTableIdentity {
		return fmt.Errorf("CDC destination table %q is reserved for managed snapshot state", target)
	}
	return nil
}

func (s *cdcChunkStore) keyID(key string) string {
	encoded, _ := json.Marshal([]string{s.manager.connectorID, key})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (s *cdcChunkStore) Load(ctx context.Context, key string) ([]byte, error) {
	s.manager.mu.Lock()
	defer s.manager.mu.Unlock()
	entries, err := s.entries(ctx, key)
	if err != nil {
		return nil, err
	}
	return decodeCDCChunk(entries)
}

func (s *cdcChunkStore) entries(ctx context.Context, key string) ([]destination.CDCStateEntry, error) {
	entries, err := s.manager.reader.LoadCDCState(ctx, s.table, s.keyID(key))
	if err != nil {
		return nil, fmt.Errorf("failed to load CDC snapshot blob: %w", err)
	}
	if s.manager.started {
		for _, entry := range entries {
			if entry.Generation > s.manager.generation {
				return nil, fmt.Errorf("CDC snapshot journal generation %d exceeds active managed state generation %d; managed CDC state was lost or replaced", entry.Generation, s.manager.generation)
			}
		}
	}
	return entries, nil
}

func decodeCDCChunk(entries []destination.CDCStateEntry) ([]byte, error) {
	var latest destination.CDCStateEntry
	var manifest cdcChunkRecord
	found := false
	for _, entry := range entries {
		if entry.StateKind != cdcChunkCommitKind {
			continue
		}
		var record cdcChunkRecord
		if err := json.Unmarshal([]byte(entry.Position), &record); err != nil {
			return nil, fmt.Errorf("invalid CDC snapshot blob manifest: %w", err)
		}
		if record.Version != 1 || record.ID == "" || record.Sequence == 0 || entry.Status != destination.CDCStateStatusComplete {
			return nil, fmt.Errorf("invalid CDC snapshot blob manifest")
		}
		if found && entry.Generation == latest.Generation && record.Sequence == manifest.Sequence && entry.Position != latest.Position {
			return nil, fmt.Errorf("conflicting CDC snapshot blob manifests")
		}
		if !found || entry.Generation > latest.Generation || entry.Generation == latest.Generation && record.Sequence > manifest.Sequence {
			latest, manifest, found = entry, record, true
		}
	}
	if !found || manifest.Deleted {
		return nil, nil
	}
	if manifest.Bytes < 0 || manifest.Parts < 0 || manifest.Parts > len(entries) || manifest.Parts != (manifest.Bytes+cdcChunkFragmentBytes-1)/cdcChunkFragmentBytes {
		return nil, fmt.Errorf("invalid CDC snapshot blob size")
	}
	parts := make(map[int]string, manifest.Parts)
	for _, entry := range entries {
		if entry.StateKind != cdcChunkPartKind || entry.Generation != latest.Generation {
			continue
		}
		var record cdcChunkRecord
		if err := json.Unmarshal([]byte(entry.Position), &record); err != nil {
			return nil, fmt.Errorf("invalid CDC snapshot blob fragment: %w", err)
		}
		if record.ID != manifest.ID || record.Sequence != manifest.Sequence {
			continue
		}
		if record.Version != 1 || record.Part < 0 || record.Part >= manifest.Parts {
			return nil, fmt.Errorf("invalid CDC snapshot blob fragment index")
		}
		if previous, ok := parts[record.Part]; ok && previous != record.Data {
			return nil, fmt.Errorf("conflicting CDC snapshot blob fragments")
		}
		parts[record.Part] = record.Data
	}
	if len(parts) != manifest.Parts {
		return nil, fmt.Errorf("CDC snapshot blob is missing committed fragments")
	}
	data := make([]byte, 0, manifest.Bytes)
	for index := 0; index < manifest.Parts; index++ {
		part, err := base64.StdEncoding.DecodeString(parts[index])
		if err != nil {
			return nil, fmt.Errorf("invalid CDC snapshot blob encoding: %w", err)
		}
		if len(part) != min(cdcChunkFragmentBytes, manifest.Bytes-index*cdcChunkFragmentBytes) {
			return nil, fmt.Errorf("invalid CDC snapshot blob fragment size")
		}
		data = append(data, part...)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != manifest.Digest {
		return nil, fmt.Errorf("CDC snapshot blob checksum mismatch")
	}
	return data, nil
}

func (s *cdcChunkStore) Save(ctx context.Context, key string, data []byte) error {
	return s.save(ctx, key, data, false)
}

func (s *cdcChunkStore) Delete(ctx context.Context, key string) error {
	return s.save(ctx, key, nil, true)
}

func (s *cdcChunkStore) checkOwner(ctx context.Context) error {
	m := s.manager
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC snapshot blob requires an active CDC state run")
	}
	fence, err := m.fenceReader.LoadCDCStateFence(ctx, m.stateTable, m.connectorID)
	if err != nil {
		return fmt.Errorf("failed to load CDC snapshot ownership fence: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	ids := uniqueCDCStateEventIDs(fence.RunEventIDs)
	if fence.Generation != m.generation || len(ids) != 1 || ids[0] != m.runEventID {
		return fmt.Errorf("CDC snapshot state run %s lost exclusive ownership of generation %d", m.runID, m.generation)
	}
	return nil
}

func (s *cdcChunkStore) save(ctx context.Context, key string, data []byte, deleted bool) error {
	m := s.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := s.checkOwner(ctx); err != nil {
		return err
	}
	entries, err := s.entries(ctx, key)
	if err != nil {
		return err
	}
	writer := &CDCStateManager{dest: m.dest, stateTable: s.table, connectorID: s.keyID(key)}
	if !s.prepared {
		if m.targetClaimer == nil {
			return fmt.Errorf("CDC snapshot blobs require managed target ownership")
		}
		if err := m.prepareTargetTable(ctx); err != nil {
			return err
		}
		if err := s.checkOwner(ctx); err != nil {
			return err
		}
		if err := writer.prepareTable(ctx); err != nil {
			return err
		}
		s.prepared = true
	}
	var sequence uint64
	for _, entry := range entries {
		var record cdcChunkRecord
		if err := json.Unmarshal([]byte(entry.Position), &record); err != nil {
			return fmt.Errorf("invalid CDC snapshot blob record: %w", err)
		}
		if entry.Generation == m.generation {
			sequence = max(sequence, record.Sequence)
		}
	}
	sequence++
	if sequence == 0 {
		return fmt.Errorf("CDC snapshot blob sequence overflow")
	}
	id, err := newCDCStateRunID()
	if err != nil {
		return err
	}
	manifest := cdcChunkRecord{Version: 1, Sequence: sequence, ID: id, Deleted: deleted}
	manifest.Bytes = len(data)
	manifest.Parts = (len(data) + cdcChunkFragmentBytes - 1) / cdcChunkFragmentBytes
	sum := sha256.Sum256(data)
	manifest.Digest = hex.EncodeToString(sum[:])
	event := func(kind string, record cdcChunkRecord) cdcStateWriteEvent {
		position, _ := json.Marshal(record)
		return cdcStateWriteEvent{entry: destination.CDCStateEntry{
			EventID:   fmt.Sprintf("%s-%s-%d", id, kind, record.Part),
			StateKind: kind, Generation: m.generation, Status: destination.CDCStateStatusComplete, Position: string(position),
		}, recordedAt: time.Now().UTC()}
	}
	write := func(events []cdcStateWriteEvent) error {
		if err := s.checkOwner(ctx); err != nil {
			return err
		}
		return writer.writeStateBatch(ctx, events)
	}
	batch := make([]cdcStateWriteEvent, 0, cdcChunkWriteBatchSize)
	for index, start := 0, 0; start < len(data); index, start = index+1, start+cdcChunkFragmentBytes {
		part := cdcChunkRecord{
			Version: 1, Sequence: sequence, ID: id, Part: index,
			Data: base64.StdEncoding.EncodeToString(data[start:min(start+cdcChunkFragmentBytes, len(data))]),
		}
		batch = append(batch, event(cdcChunkPartKind, part))
		if len(batch) == cdcChunkWriteBatchSize || index == manifest.Parts-1 {
			if err := write(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	// Publishing the manifest only after every fragment is durable keeps an
	// interrupted replacement from hiding the previous complete value.
	commit := event(cdcChunkCommitKind, manifest)
	if err := write([]cdcStateWriteEvent{commit}); err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.EventID)
	}
	ids = uniqueCDCStateEventIDs(ids)
	for start := 0; start < len(ids); start += m.pruneBatchSize {
		if err := s.checkOwner(ctx); err != nil {
			return err
		}
		if err := m.pruner.DeleteCDCStateEvents(ctx, s.table, writer.connectorID, ids[start:min(start+m.pruneBatchSize, len(ids))]); err != nil {
			config.Debug("[CDC] Failed to prune superseded snapshot blobs: %v", err)
			return source.ConnectorLeaseLoss(ctx)
		}
	}
	if deleted {
		remaining, err := s.entries(ctx, key)
		if err != nil {
			config.Debug("[CDC] Failed to verify snapshot blob cleanup: %v", err)
			return source.ConnectorLeaseLoss(ctx)
		}
		// Keep the tombstone until no older value can become visible again.
		for _, entry := range remaining {
			if entry.EventID != commit.entry.EventID || entry.Generation != commit.entry.Generation || entry.Position != commit.entry.Position {
				return source.ConnectorLeaseLoss(ctx)
			}
		}
		if err := s.checkOwner(ctx); err != nil {
			return err
		}
		if err := m.pruner.DeleteCDCStateEvents(ctx, s.table, writer.connectorID, []string{commit.entry.EventID}); err != nil {
			config.Debug("[CDC] Failed to prune snapshot blob tombstone: %v", err)
		}
	}
	return source.ConnectorLeaseLoss(ctx)
}

func (s *cdcChunkStore) Identity(ctx context.Context) (string, error) {
	m := s.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.destTables) == 0 {
		return "", fmt.Errorf("CDC snapshot identity requires registered tables")
	}
	tables := make([]string, 0, len(m.destTables))
	for table := range m.destTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	identity := [][]string{{"1", m.connectorID}}
	for _, table := range tables {
		physical, exists, err := m.currentDestinationIncarnation(ctx, table)
		if err != nil {
			return "", err
		}
		if !exists || physical == "" {
			return "", fmt.Errorf("CDC snapshot destination %q has no physical identity", m.destTables[table])
		}
		identity = append(identity, []string{table, m.currentIncarnations[table], m.currentSchemas[table], m.destTables[table], physical})
	}
	encoded, _ := json.Marshal(identity)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
