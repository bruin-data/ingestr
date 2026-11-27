package postgres_cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
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
	r.targetIdentity = identity
	var tableIdentities map[string]source.CDCChunkTableIdentity
	if provider, ok := r.options.Store.(source.CDCChunkTableIdentityProvider); ok {
		tableIdentities, err = provider.TableIdentities(ctx)
		if err != nil {
			return 0, false, err
		}
	}
	r.tableIdentities = tableIdentities
	r.resnapshotTables = nil
	r.resnapshotOldRelIDs = nil
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
			entry := &chunkTableState{
				Boundary: FormatLSN(state.Applied), Done: resumes[table.Name] != "", ReplayFrom: resumes[table.Name],
			}
			ti, hasTI := tableIdentities[table.Name]
			populateChunkTableMetadata(entry, table, ti, hasTI)
			state.Tables[table.Name] = entry
		}
		state.SnapshotComplete = execution == "serial" && state.complete()
	} else {
		if state.Slot != slot || state.Mode != r.options.WALStaging || checkpointExecution(state.Execution) != execution {
			return 0, false, fmt.Errorf("chunked snapshot slot, execution, or WAL staging mode changed; keep the original configuration or use --full-refresh")
		}
		reconciled, err := reconcileChunkState(state, r.tables, identity, execution, resumes, tableIdentities)
		if err != nil {
			return 0, false, err
		}
		r.resnapshotTables = reconciled.resnapshot
		r.resnapshotOldRelIDs = reconciled.oldRelIDs
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

type chunkReconcileResult struct {
	resnapshot []source.SourceTableInfo
	oldRelIDs  map[string]uint32
}

// adoptChunkTableSelection keeps finished checkpoints when the publication
// gains or loses tables, and leaves new tables to be copied in chunks.
func adoptChunkTableSelection(state *chunkBootstrapState, tables []source.SourceTableInfo, identity, execution string, resumes map[string]string) error {
	_, err := reconcileChunkState(state, tables, identity, execution, resumes, nil)
	return err
}

// reconcileChunkState validates retained tables against the saved checkpoint,
// routes tables whose source OID or schema fingerprint changed (or whose
// destination was recreated by startup schema evolution) into per-table
// resnapshot, and fails closed when primary keys or physical destination
// identities changed unexpectedly.
func reconcileChunkState(
	state *chunkBootstrapState,
	tables []source.SourceTableInfo,
	identity, execution string,
	resumes map[string]string,
	tableIdentities map[string]source.CDCChunkTableIdentity,
) (chunkReconcileResult, error) {
	if state.Tables == nil {
		state.Tables = make(map[string]*chunkTableState)
	}
	selected := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		selected[table.Name] = struct{}{}
	}
	sameSelection := len(state.Tables) == len(selected)
	if sameSelection {
		for name := range state.Tables {
			if _, ok := selected[name]; !ok {
				sameSelection = false
				break
			}
		}
	}

	identityChanged := state.Identity != "" && state.Identity != identity

	type metadataUpdate struct {
		name      string
		table     source.SourceTableInfo
		ti        source.CDCChunkTableIdentity
		hasTI     bool
		unstarted bool
	}
	var result chunkReconcileResult
	var updates []metadataUpdate

	for _, table := range tables {
		saved, retained := state.Tables[table.Name]
		if !retained {
			continue
		}
		if saved == nil {
			return chunkReconcileResult{}, fmt.Errorf("chunked snapshot has no checkpoint for %s", table.Name)
		}
		ti, hasTI := tableIdentities[table.Name]

		currentPKs := tablePrimaryKeys(table)
		if len(saved.PrimaryKeys) > 0 && !slices.Equal(saved.PrimaryKeys, currentPKs) {
			return chunkReconcileResult{}, fmt.Errorf("primary keys for %s changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline", table.Name)
		}

		savedDest := saved.DestTable
		if savedDest == "" && hasTI {
			savedDest = ti.LastCertifiedDest
		}
		savedDestID := saved.DestIdentity
		if savedDestID == "" && hasTI {
			savedDestID = ti.LastCertifiedPhysical
		}
		prevIncarnation := saved.Incarnation
		prevSchema := saved.SchemaFingerprint
		if prevIncarnation == "" && prevSchema == "" && hasTI {
			prevIncarnation = ti.LastCertifiedSource
			prevSchema = ti.LastCertifiedSchema
		}

		// When the aggregate identity changed, every retained table that has
		// already copied rows (Done or Initialized) must have verifiable source
		// and physical destination identity proof so an unfinished legacy
		// table cannot resume its cursor against an unchecked replacement.
		if identityChanged && (saved.Done || saved.Initialized) {
			if tableIdentities != nil {
				if !hasTI || savedDestID == "" || ti.PhysicalIdentity == "" || (prevIncarnation == "" && prevSchema == "") {
					return chunkReconcileResult{}, fmt.Errorf("chunked snapshot checkpoint lacks per-table identity proof for %s; use --full-refresh to establish a new baseline", table.Name)
				}
			} else if sameSelection {
				return chunkReconcileResult{}, fmt.Errorf("source or destination changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline")
			}
		}

		destRecreatedByEvolution := false
		if hasTI {
			if savedDest != "" && ti.DestTable != "" && savedDest != ti.DestTable {
				return chunkReconcileResult{}, fmt.Errorf("destination table for %s changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline", table.Name)
			}
			if savedDestID != "" && ti.PhysicalIdentity != "" && savedDestID != ti.PhysicalIdentity {
				if ti.PreEvolutionIdentity != "" && savedDestID == ti.PreEvolutionIdentity {
					destRecreatedByEvolution = true
				} else {
					return chunkReconcileResult{}, fmt.Errorf("destination table for %s was replaced since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline", table.Name)
				}
			}
		}

		oidChanged := prevIncarnation != "" && table.Incarnation != "" && prevIncarnation != table.Incarnation
		schemaChanged := prevSchema != "" && table.SchemaFingerprint != "" && !schemaFingerprintMatches(prevSchema, table.SchemaFingerprint)

		if oidChanged || schemaChanged || destRecreatedByEvolution {
			if saved.Done || saved.Initialized {
				result.resnapshot = append(result.resnapshot, table)
				if oidChanged {
					if oldID, ok := parseIncarnationOID(prevIncarnation); ok {
						if result.oldRelIDs == nil {
							result.oldRelIDs = make(map[string]uint32)
						}
						result.oldRelIDs[table.Name] = oldID
					}
				}
			} else {
				updates = append(updates, metadataUpdate{name: table.Name, table: table, ti: ti, hasTI: hasTI, unstarted: true})
			}
			continue
		}

		if saved.Incarnation == "" || saved.SchemaFingerprint == "" || len(saved.PrimaryKeys) == 0 || (hasTI && (saved.DestTable == "" || saved.DestIdentity == "")) {
			updates = append(updates, metadataUpdate{name: table.Name, table: table, ti: ti, hasTI: hasTI})
		}
	}

	unstartedChanged := false
	for _, upd := range updates {
		if upd.unstarted {
			unstartedChanged = true
			break
		}
	}
	if sameSelection && identityChanged && len(result.resnapshot) == 0 && !unstartedChanged {
		return chunkReconcileResult{}, fmt.Errorf("source or destination changed since the chunked snapshot checkpoint; use --full-refresh to establish a new baseline")
	}

	for _, table := range tables {
		if _, ok := state.Tables[table.Name]; ok {
			continue
		}
		if position := resumes[table.Name]; position != "" {
			if _, err := parseStoredPostgresLSN(position); err != nil {
				return chunkReconcileResult{}, err
			}
		}
	}

	for _, upd := range updates {
		populateChunkTableMetadata(state.Tables[upd.name], upd.table, upd.ti, upd.hasTI)
	}

	if !sameSelection {
		for name := range state.Tables {
			if _, ok := selected[name]; !ok {
				delete(state.Tables, name)
			}
		}
		for _, table := range tables {
			if _, ok := state.Tables[table.Name]; ok {
				continue
			}
			position := resumes[table.Name]
			entry := &chunkTableState{
				Boundary: FormatLSN(state.Applied), Done: position != "", ReplayFrom: position,
			}
			ti, hasTI := tableIdentities[table.Name]
			populateChunkTableMetadata(entry, table, ti, hasTI)
			state.Tables[table.Name] = entry
		}
		if len(result.resnapshot) == 0 {
			state.Identity = identity
		}
		if execution == "serial" && state.complete() {
			state.SnapshotComplete = true
		}
	} else if len(result.resnapshot) == 0 && state.Identity != identity {
		state.Identity = identity
	}

	return result, nil
}

func populateChunkTableMetadata(entry *chunkTableState, table source.SourceTableInfo, ti source.CDCChunkTableIdentity, hasTI bool) {
	if table.Incarnation != "" {
		entry.Incarnation = table.Incarnation
	}
	if table.SchemaFingerprint != "" {
		entry.SchemaFingerprint = table.SchemaFingerprint
	}
	if pks := tablePrimaryKeys(table); len(pks) > 0 {
		entry.PrimaryKeys = append([]string(nil), pks...)
	}
	if hasTI {
		if ti.DestTable != "" {
			entry.DestTable = ti.DestTable
		}
		if ti.PhysicalIdentity != "" {
			entry.DestIdentity = ti.PhysicalIdentity
		}
	}
}

func tablePrimaryKeys(table source.SourceTableInfo) []string {
	if len(table.PrimaryKeys) > 0 {
		return table.PrimaryKeys
	}
	if table.Schema != nil && len(table.Schema.PrimaryKeys) > 0 {
		return table.Schema.PrimaryKeys
	}
	return nil
}

func parseIncarnationOID(incarnation string) (uint32, bool) {
	if incarnation == "" {
		return 0, false
	}
	oid, err := strconv.ParseUint(incarnation, 10, 32)
	if err != nil || oid == 0 {
		return 0, false
	}
	return uint32(oid), true
}

func schemaFingerprintMatches(stored, current string) bool {
	if stored == current {
		return true
	}
	// Legacy cdc_state entries store 18-character compact SHA256 prefixes.
	return len(stored) == 18 && len(current) > 18 && current[:18] == stored
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
