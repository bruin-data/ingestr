package postgres_cdc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const chunkStateKey = "bootstrap"

type chunkTableState struct {
	After       []string `json:"after,omitempty"`
	Upper       []string `json:"upper,omitempty"`
	Boundary    string   `json:"boundary"`
	ReplayFrom  string   `json:"replay_from,omitempty"`
	Initialized bool     `json:"initialized"`
	Done        bool     `json:"done"`
}

type chunkBootstrapState struct {
	Version          int                         `json:"version"`
	Epoch            string                      `json:"epoch"`
	Identity         string                      `json:"identity"`
	Slot             string                      `json:"slot"`
	Mode             string                      `json:"mode"`
	Execution        string                      `json:"execution,omitempty"`
	RefreshID        string                      `json:"refresh_id,omitempty"`
	SnapshotComplete bool                        `json:"snapshot_complete,omitempty"`
	Applied          pglogrepl.LSN               `json:"applied"`
	Consumed         uint64                      `json:"consumed"`
	Tables           map[string]*chunkTableState `json:"tables"`
	RetiredEpochs    []chunkRetiredEpoch         `json:"retired_epochs,omitempty"`
}

func (s *chunkBootstrapState) complete() bool {
	for _, table := range s.Tables {
		if table == nil || !table.Done {
			return false
		}
	}
	return len(s.Tables) > 0
}

func (s *chunkBootstrapState) bootstrapComplete() bool {
	return s.complete() && (s.Execution != "serial" || s.SnapshotComplete)
}

func (s *chunkBootstrapState) replayPositions() map[string]string {
	positions := make(map[string]string)
	for name, table := range s.Tables {
		if table.ReplayFrom != "" {
			positions[name] = table.ReplayFrom
		}
	}
	return positions
}

func loadChunkState(ctx context.Context, store source.CDCChunkStore) (*chunkBootstrapState, error) {
	data, err := store.Load(ctx, chunkStateKey)
	if err != nil || data == nil {
		return nil, err
	}
	var state chunkBootstrapState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid chunked snapshot checkpoint: %w", err)
	}
	if state.Version != 1 || state.Epoch == "" || state.Slot == "" || state.Applied == 0 || len(state.Tables) == 0 {
		return nil, fmt.Errorf("invalid chunked snapshot checkpoint")
	}
	if state.Execution != "" && state.Execution != "parallel" && state.Execution != "serial" {
		return nil, fmt.Errorf("invalid chunked snapshot execution mode")
	}
	if state.Execution == "serial" && (state.Mode != "inmemory" || state.SnapshotComplete && !state.complete()) {
		return nil, fmt.Errorf("invalid serial snapshot checkpoint")
	}
	for name, table := range state.Tables {
		if name == "" || table == nil {
			return nil, fmt.Errorf("invalid chunked snapshot table checkpoint")
		}
		if boundary, err := pglogrepl.ParseLSN(table.Boundary); err != nil || boundary == 0 {
			return nil, fmt.Errorf("invalid chunked snapshot boundary for %s", name)
		}
		if table.ReplayFrom != "" {
			if position, err := parseStoredPostgresLSN(table.ReplayFrom); err != nil || position == 0 || !table.Done || table.Initialized {
				return nil, fmt.Errorf("invalid snapshot replay boundary for %s", name)
			}
		}
	}
	return &state, nil
}

// chunkedCommit is acknowledged only after the consumer has merged the data
// and persisted its ordinary CDC state. The bootstrap record must then become
// durable before any source WAL is acknowledged.
type chunkedCommit struct {
	store     source.CDCChunkStore
	state     *chunkBootstrapState
	done      chan struct{}
	mu        sync.Mutex
	persisted bool
	ack       sync.Once
}

func (c *chunkedCommit) persist(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.persisted {
		return nil
	}
	identity, err := c.store.Identity(ctx)
	if err != nil {
		return err
	}
	if c.state.Identity != "" && identity != c.state.Identity {
		return fmt.Errorf("chunked snapshot source or destination identity changed before checkpoint persistence")
	}
	c.state.Identity = identity
	data, err := json.Marshal(c.state)
	if err != nil {
		return fmt.Errorf("failed to encode chunked snapshot checkpoint: %w", err)
	}
	if err := c.store.Save(ctx, chunkStateKey, data); err != nil {
		return err
	}
	c.persisted = true
	return nil
}

func (s *chunkBootstrapState) clone() *chunkBootstrapState {
	cloned := *s
	cloned.RetiredEpochs = append([]chunkRetiredEpoch(nil), s.RetiredEpochs...)
	cloned.Tables = make(map[string]*chunkTableState, len(s.Tables))
	for name, state := range s.Tables {
		table := *state
		table.After = append([]string(nil), state.After...)
		table.Upper = append([]string(nil), state.Upper...)
		cloned.Tables[name] = &table
	}
	return &cloned
}

type chunkJournalManifest struct {
	Version  int           `json:"version"`
	Head     uint64        `json:"head"`
	Tail     uint64        `json:"tail"`
	Captured pglogrepl.LSN `json:"captured"`
}

// chunkJournal separates captured WAL from applied progress. A manifest is
// published only after every page it references is durable. Pages are deleted
// only after a bootstrap checkpoint certifies their output.
type chunkJournal struct {
	store    source.CDCChunkStore
	epoch    string
	manifest chunkJournalManifest
}

func newChunkEpoch() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("failed to create snapshot epoch: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (j *chunkJournal) manifestKey() string { return "wal/" + j.epoch + "/manifest" }

func (j *chunkJournal) pageKey(sequence uint64) string {
	return fmt.Sprintf("wal/%s/%020d", j.epoch, sequence)
}

func (j *chunkJournal) load(ctx context.Context, consumed uint64) error {
	data, err := j.store.Load(ctx, j.manifestKey())
	if err != nil {
		return err
	}
	if data == nil {
		j.manifest = chunkJournalManifest{Version: 1, Head: consumed, Tail: consumed}
		return nil
	}
	if err := json.Unmarshal(data, &j.manifest); err != nil {
		return fmt.Errorf("invalid snapshot WAL manifest: %w", err)
	}
	if j.manifest.Version != 1 || j.manifest.Head > consumed || j.manifest.Tail < consumed {
		return fmt.Errorf("snapshot WAL manifest does not cover the durable checkpoint")
	}
	return nil
}

func (j *chunkJournal) append(ctx context.Context, groups []DecodedChanges, captured pglogrepl.LSN) error {
	manifest := j.manifest
	if len(groups) > 0 {
		var encoded bytes.Buffer
		if err := gob.NewEncoder(&encoded).Encode(groups); err != nil {
			return fmt.Errorf("failed to encode snapshot WAL: %w", err)
		}
		if err := j.store.Save(ctx, j.pageKey(manifest.Tail), encoded.Bytes()); err != nil {
			return err
		}
		manifest.Tail++
	}
	manifest.Captured = max(manifest.Captured, captured)
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := j.store.Save(ctx, j.manifestKey(), data); err != nil {
		return err
	}
	j.manifest = manifest
	return nil
}

func (j *chunkJournal) replay(ctx context.Context, from uint64, consume func([]DecodedChanges) error) error {
	for sequence := from; sequence < j.manifest.Tail; sequence++ {
		data, err := j.store.Load(ctx, j.pageKey(sequence))
		if err != nil {
			return err
		}
		if data == nil {
			return fmt.Errorf("snapshot WAL page %d is missing; cannot resume safely", sequence)
		}
		var groups []DecodedChanges
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&groups); err != nil {
			return fmt.Errorf("failed to decode snapshot WAL page %d: %w", sequence, err)
		}
		if err := consume(groups); err != nil {
			return err
		}
	}
	return nil
}

func (j *chunkJournal) prune(ctx context.Context, from, to uint64) error {
	if from > to || to < j.manifest.Head || to > j.manifest.Tail {
		return fmt.Errorf("invalid snapshot WAL cleanup boundary")
	}
	if to == j.manifest.Head {
		return nil
	}
	for sequence := j.manifest.Head; sequence < to; sequence++ {
		if err := j.store.Delete(ctx, j.pageKey(sequence)); err != nil {
			return err
		}
	}
	manifest := j.manifest
	manifest.Head = to
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := j.store.Save(ctx, j.manifestKey(), data); err != nil {
		return err
	}
	j.manifest = manifest
	return nil
}
