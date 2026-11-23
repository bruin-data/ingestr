package postgres_cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const snapshotSpoolStateKey = "normal-spool/bootstrap"

type snapshotSpoolState struct {
	Version      int                                   `json:"version"`
	Epoch        string                                `json:"epoch"`
	Identity     string                                `json:"identity"`
	Slot         string                                `json:"slot"`
	Mode         string                                `json:"mode"`
	Directory    string                                `json:"directory,omitempty"`
	RefreshID    string                                `json:"refresh_id,omitempty"`
	Applied      pglogrepl.LSN                         `json:"applied"`
	Consumed     uint64                                `json:"consumed"`
	Reset        bool                                  `json:"reset"`
	CopyComplete bool                                  `json:"copy_complete"`
	Complete     bool                                  `json:"complete"`
	Tables       map[string]source.CDCSnapshotProgress `json:"tables"`
	Retired      []snapshotSpoolRetired                `json:"retired,omitempty"`
}

func (s *snapshotSpoolState) clone() *snapshotSpoolState {
	cloned := *s
	cloned.Retired = append([]snapshotSpoolRetired(nil), s.Retired...)
	cloned.Tables = make(map[string]source.CDCSnapshotProgress, len(s.Tables))
	for name, progress := range s.Tables {
		cloned.Tables[name] = progress.Clone()
	}
	return &cloned
}

func loadSnapshotSpoolState(ctx context.Context, store source.CDCChunkStore) (*snapshotSpoolState, error) {
	data, err := store.Load(ctx, snapshotSpoolStateKey)
	if err != nil || data == nil {
		return nil, err
	}
	var state snapshotSpoolState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid normal snapshot spool checkpoint: %w", err)
	}
	if state.Version != 1 || state.Epoch == "" || state.Slot == "" || len(state.Tables) == 0 ||
		!state.Reset && (state.Identity == "" || state.Applied == 0) {
		return nil, fmt.Errorf("invalid normal snapshot spool checkpoint")
	}
	if state.Mode != "inmemory" && state.Mode != "disk" && state.Mode != "destination" || state.Mode == "disk" && state.Directory == "" {
		return nil, fmt.Errorf("invalid normal snapshot spool storage mode or directory")
	}
	if state.Complete && !state.CopyComplete {
		return nil, fmt.Errorf("normal snapshot spool cannot complete before its snapshot")
	}
	if state.Reset && (state.CopyComplete || state.Complete || state.Consumed != 0) {
		return nil, fmt.Errorf("normal snapshot spool reset cannot contain completed progress")
	}
	for name, progress := range state.Tables {
		if name == "" || progress.Incarnation == "" || progress.SchemaFingerprint == "" || progress.Attempt == "" {
			return nil, fmt.Errorf("invalid normal snapshot spool table checkpoint for %q", name)
		}
		if state.Reset && progress.Boundary == "" {
			continue
		}
		if boundary, err := pglogrepl.ParseLSN(progress.Boundary); err != nil || boundary == 0 {
			return nil, fmt.Errorf("invalid normal snapshot spool boundary for %s", name)
		}
	}
	return &state, nil
}

type snapshotSpoolBatchCommit struct{}

type snapshotSpoolCommit struct {
	store     source.CDCChunkStore
	state     *snapshotSpoolState
	done      chan struct{}
	mu        sync.Mutex
	persisted bool
	ack       sync.Once
}

func (c *snapshotSpoolCommit) persist(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.persisted {
		return nil
	}
	if !c.state.Reset || c.state.Identity != "" {
		identity, err := c.store.Identity(ctx)
		if err != nil {
			return err
		}
		if identity == "" || c.state.Identity != "" && identity != c.state.Identity {
			return fmt.Errorf("normal snapshot spool source or destination identity changed before checkpoint persistence")
		}
		c.state.Identity = identity
	}
	data, err := json.Marshal(c.state)
	if err != nil {
		return fmt.Errorf("failed to encode normal snapshot spool checkpoint: %w", err)
	}
	if err := c.store.Save(ctx, snapshotSpoolStateKey, data); err != nil {
		return err
	}
	c.persisted = true
	return nil
}
