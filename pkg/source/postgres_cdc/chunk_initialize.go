package postgres_cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/jackc/pglogrepl"
)

func (r *chunkReader) initialize(ctx context.Context, slot string, resumes map[string]string) (pglogrepl.LSN, bool, error) {
	execution := chunkExecution(r.options.Execution)
	if execution != "parallel" && execution != "serial" {
		return 0, false, fmt.Errorf("invalid chunked snapshot execution mode")
	}
	if execution == "serial" && r.options.WALStaging != "inmemory" {
		return 0, false, fmt.Errorf("serial chunked snapshots require inmemory WAL staging")
	}
	identity, err := r.options.Store.Identity(ctx)
	if err != nil {
		return 0, false, err
	}
	state, err := loadChunkState(ctx, r.options.Store)
	if err != nil {
		return 0, false, err
	}
	state, refresh, err := r.prepareChunkRefresh(ctx, state, slot)
	if err != nil {
		return 0, false, err
	}
	if refresh != nil {
		clear(resumes)
	}
	if state != nil && state.Identity != identity {
		return 0, false, fmt.Errorf("source or destination changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline")
	}
	fresh := state == nil
	if fresh {
		var epoch string
		var retired []chunkRetiredEpoch
		if refresh != nil {
			epoch, retired = refresh.Epoch, refresh.RetiredEpochs
		} else {
			epoch, err = newChunkEpoch()
			if err != nil {
				return 0, false, err
			}
		}
		state = &chunkBootstrapState{
			Version: 1, Epoch: epoch, Identity: identity, Slot: slot, Mode: r.options.WALStaging,
			Execution: execution, RefreshID: r.options.RefreshID,
			Tables: make(map[string]*chunkTableState), RetiredEpochs: retired,
		}
		for _, table := range r.tables {
			if position := resumes[table.Name]; position != "" {
				lsn, err := parseStoredPostgresLSN(position)
				if err != nil {
					return 0, false, err
				}
				if state.Applied == 0 || lsn < state.Applied {
					state.Applied = lsn
				}
			}
		}
		if state.Applied == 0 {
			if _, exists, err := checkSlotExists(ctx, r.source.queryPool, slot); err != nil {
				return 0, false, err
			} else if exists {
				if _, err := r.source.queryPool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slot); err != nil {
					return 0, false, fmt.Errorf("failed to reset chunked snapshot slot: %w", err)
				}
			}
			created, err := r.source.createLogicalSlot(ctx, r.source.replConn, slot, false)
			if err != nil {
				return 0, false, err
			}
			state.Applied, err = pglogrepl.ParseLSN(created.ConsistentPoint)
			if err != nil {
				return 0, false, err
			}
			// A new replication command releases the slot's exported snapshot.
			if _, err := pglogrepl.IdentifySystem(ctx, r.source.replConn); err != nil {
				return 0, false, fmt.Errorf("failed to release initial snapshot export: %w", err)
			}
		}
		for _, table := range r.tables {
			state.Tables[table.Name] = &chunkTableState{
				Boundary: FormatLSN(state.Applied), Done: resumes[table.Name] != "", ReplayFrom: resumes[table.Name],
			}
		}
		state.SnapshotComplete = execution == "serial" && state.complete()
	} else {
		if state.Slot != slot || state.Mode != r.options.WALStaging || checkpointExecution(state.Execution) != execution {
			return 0, false, fmt.Errorf("chunked snapshot slot, execution, or WAL staging mode changed; keep the original configuration or use --full-refresh")
		}
		if len(state.Tables) != len(r.tables) {
			return 0, false, fmt.Errorf("chunked snapshot table selection changed")
		}
		for _, table := range r.tables {
			if state.Tables[table.Name] == nil {
				return 0, false, fmt.Errorf("chunked snapshot has no checkpoint for %s", table.Name)
			}
		}
	}
	r.state = state
	start := state.Applied
	if r.options.WALStaging == "destination" {
		r.journal = &chunkJournal{store: r.options.Store, epoch: state.Epoch}
		if err := r.journal.load(ctx, state.Consumed); err != nil {
			return 0, false, err
		}
		start = max(start, r.journal.manifest.Captured)
	}
	if err := r.validateSlot(ctx, start); err != nil {
		return 0, false, err
	}
	r.source.setWALRetentionSlot(state.Slot)
	return start, fresh, nil
}

func chunkExecution(execution string) string {
	if execution == "" {
		return config.DefaultCDCSnapshotExecution
	}
	return execution
}

// Checkpoints without an execution field encode parallel snapshots.
func checkpointExecution(execution string) string {
	if execution == "" {
		return "parallel"
	}
	return execution
}

const chunkRefreshIntentKey = "refresh-intent"

type chunkRefreshTable struct {
	Name        string `json:"name"`
	Incarnation string `json:"incarnation"`
	Schema      string `json:"schema"`
}

type chunkRefreshIntent struct {
	Version       int                 `json:"version"`
	Epoch         string              `json:"epoch"`
	RefreshID     string              `json:"refresh_id,omitempty"`
	Slot          string              `json:"slot"`
	Mode          string              `json:"mode"`
	Execution     string              `json:"execution"`
	Tables        []chunkRefreshTable `json:"tables"`
	RetiredEpochs []chunkRetiredEpoch `json:"retired_epochs,omitempty"`
}

// A refresh intent is durable before the retained slot is replaced. Until its
// epoch has a bootstrap checkpoint, recovery repeats the reset and initial copy.
func (r *chunkReader) prepareChunkRefresh(ctx context.Context, state *chunkBootstrapState, slot string) (*chunkBootstrapState, *chunkRefreshIntent, error) {
	data, err := r.options.Store.Load(ctx, chunkRefreshIntentKey)
	if err != nil {
		return nil, nil, err
	}
	var intent *chunkRefreshIntent
	if data != nil {
		intent = &chunkRefreshIntent{}
		if err := json.Unmarshal(data, intent); err != nil {
			return nil, nil, fmt.Errorf("invalid chunked snapshot refresh intent: %w", err)
		}
		if intent.Version != 1 || intent.Epoch == "" || intent.Slot == "" || len(intent.Tables) == 0 ||
			(intent.Execution != "parallel" && intent.Execution != "serial") ||
			(intent.Mode != "inmemory" && intent.Mode != "destination") ||
			(intent.Execution == "serial" && intent.Mode != "inmemory") {
			return nil, nil, fmt.Errorf("invalid chunked snapshot refresh intent")
		}
		if state != nil && state.Epoch == intent.Epoch {
			intent = nil
		}
	}
	requestID := r.options.RefreshID
	requested := r.opts.FullRefresh && requestID == "" || requestID != "" && (state == nil || state.RefreshID != requestID)
	if intent != nil && intent.RefreshID == requestID {
		if intent.Slot != slot || intent.Mode != r.options.WALStaging || intent.Execution != chunkExecution(r.options.Execution) ||
			!slices.Equal(intent.Tables, r.chunkRefreshTables()) {
			return nil, nil, fmt.Errorf("unfinished chunked snapshot refresh configuration changed; restore its settings or request a new refresh ID")
		}
		return nil, intent, nil
	}
	if !requested {
		if intent != nil {
			return nil, nil, fmt.Errorf("unfinished chunked snapshot refresh requires its original --refresh-id or a new --full-refresh")
		}
		return state, nil, nil
	}
	epoch, err := newChunkEpoch()
	if err != nil {
		return nil, nil, err
	}
	intent = &chunkRefreshIntent{
		Version: 1, Epoch: epoch, RefreshID: requestID, Slot: slot, Mode: r.options.WALStaging,
		Execution: chunkExecution(r.options.Execution), Tables: r.chunkRefreshTables(), RetiredEpochs: chunkEpochsToRetire(state),
	}
	data, err = json.Marshal(intent)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode chunked snapshot refresh intent: %w", err)
	}
	if err := r.options.Store.Save(ctx, chunkRefreshIntentKey, data); err != nil {
		return nil, nil, fmt.Errorf("failed to persist chunked snapshot refresh intent: %w", err)
	}
	return nil, intent, nil
}

func (r *chunkReader) chunkRefreshTables() []chunkRefreshTable {
	tables := make([]chunkRefreshTable, len(r.tables))
	for i, table := range r.tables {
		tables[i] = chunkRefreshTable{Name: table.Name, Incarnation: table.Incarnation, Schema: table.SchemaFingerprint}
	}
	return tables
}

func (r *chunkReader) validateSlot(ctx context.Context, start pglogrepl.LSN) error {
	var restart, confirmed *string
	var plugin string
	var sameDatabase, temporary bool
	if err := r.source.queryPool.QueryRow(ctx, `SELECT restart_lsn::text, confirmed_flush_lsn::text, plugin, database = current_database(), temporary FROM pg_replication_slots WHERE slot_name = $1`, r.state.Slot).Scan(&restart, &confirmed, &plugin, &sameDatabase, &temporary); err != nil {
		return fmt.Errorf("chunked snapshot replication slot is unavailable; retained WAL is required: %w", err)
	}
	if plugin != "pgoutput" || restart == nil || confirmed == nil || !sameDatabase || temporary {
		return fmt.Errorf("chunked snapshot requires a valid persistent pgoutput slot")
	}
	for _, raw := range []*string{restart, confirmed} {
		position, err := pglogrepl.ParseLSN(*raw)
		if err != nil || position > start {
			return fmt.Errorf("replication slot no longer retains the chunked snapshot resume boundary %s", start)
		}
	}
	return nil
}
