package postgres_cdc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/require"
)

type chunkIdentityTestStore struct {
	*chunkTestStore
	identities map[string]source.CDCChunkTableIdentity
}

var _ source.CDCChunkTableIdentityProvider = (*chunkIdentityTestStore)(nil)

func (s *chunkIdentityTestStore) TableIdentities(context.Context) (map[string]source.CDCChunkTableIdentity, error) {
	return s.identities, nil
}

func makeChunkTestTable(name, incarnation, fingerprint string, cols ...schema.Column) source.SourceTableInfo {
	if len(cols) == 0 {
		cols = []schema.Column{{Name: "id", DataType: schema.TypeInt64}}
	}
	return source.SourceTableInfo{
		Name:              name,
		Incarnation:       incarnation,
		SchemaFingerprint: fingerprint,
		PrimaryKeys:       []string{"id"},
		Schema: addCDCColumns(&schema.TableSchema{
			Name:        name,
			PrimaryKeys: []string{"id"},
			Columns:     cols,
		}),
	}
}

func completedMultiTableChunkState(identity string) *chunkBootstrapState {
	return &chunkBootstrapState{
		Version:          1,
		Epoch:            "epoch-1",
		Identity:         identity,
		Slot:             "slot",
		Mode:             "inmemory",
		Execution:        "serial",
		SnapshotComplete: true,
		Applied:          100,
		Tables: map[string]*chunkTableState{
			"public.anchor": {
				Boundary:          FormatLSN(100),
				Done:              true,
				Incarnation:       "1001",
				SchemaFingerprint: "fp-anchor-v1",
				PrimaryKeys:       []string{"id"},
				DestTable:         "raw.anchor",
				DestIdentity:      "dest-anchor-v1",
			},
			"public.items": {
				Boundary:          FormatLSN(100),
				Done:              true,
				Incarnation:       "1002",
				SchemaFingerprint: "fp-items-v1",
				PrimaryKeys:       []string{"id"},
				DestTable:         "raw.items",
				DestIdentity:      "dest-items-v1",
			},
		},
	}
}

func defaultTableIdentities() map[string]source.CDCChunkTableIdentity {
	return map[string]source.CDCChunkTableIdentity{
		"public.anchor": {
			DestTable:            "raw.anchor",
			PhysicalIdentity:     "dest-anchor-v1",
			PreEvolutionIdentity: "dest-anchor-v1",
		},
		"public.items": {
			DestTable:            "raw.items",
			PhysicalIdentity:     "dest-items-v1",
			PreEvolutionIdentity: "dest-items-v1",
		},
	}
}

func TestReconcileChunkStateSchemaChangeRoutesOnlyChangedTable(t *testing.T) {
	state := completedMultiTableChunkState("old-identity")
	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "1002", "fp-items-v2",
			schema.Column{Name: "id", DataType: schema.TypeInt64},
			schema.Column{Name: "note", DataType: schema.TypeString},
		),
	}

	reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, defaultTableIdentities())
	require.NoError(t, err)
	require.Len(t, reconciled.resnapshot, 1)
	require.Equal(t, "public.items", reconciled.resnapshot[0].Name)
	require.Empty(t, reconciled.oldRelIDs)
	require.True(t, state.Tables["public.anchor"].Done)
	require.Equal(t, "old-identity", state.Identity, "identity must not update until resnapshot commits")
}

func TestReconcileChunkStateOIDChangeRecordsHistoricalRelID(t *testing.T) {
	state := completedMultiTableChunkState("old-identity")
	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "2002", "fp-items-v1"),
	}

	reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, defaultTableIdentities())
	require.NoError(t, err)
	require.Len(t, reconciled.resnapshot, 1)
	require.Equal(t, "public.items", reconciled.resnapshot[0].Name)
	require.Equal(t, uint32(1002), reconciled.oldRelIDs["public.items"])
}

func TestReconcileChunkStateFailsClosedOnDestinationReplacement(t *testing.T) {
	for _, tc := range []struct {
		name          string
		itemsSchemaFP string
		modifyTI      func(map[string]source.CDCChunkTableIdentity)
		wantErr       string
	}{
		{
			name:          "external replacement without source change",
			itemsSchemaFP: "fp-items-v1",
			modifyTI: func(m map[string]source.CDCChunkTableIdentity) {
				ti := m["public.items"]
				ti.PhysicalIdentity = "dest-items-replaced"
				ti.PreEvolutionIdentity = "dest-items-replaced"
				m["public.items"] = ti
			},
			wantErr: "was replaced",
		},
		{
			name:          "external replacement even when source schema also changed",
			itemsSchemaFP: "fp-items-v2",
			modifyTI: func(m map[string]source.CDCChunkTableIdentity) {
				ti := m["public.items"]
				ti.PhysicalIdentity = "dest-items-after-evolve"
				ti.PreEvolutionIdentity = "dest-items-replaced-externally"
				m["public.items"] = ti
			},
			wantErr: "was replaced",
		},
		{
			name:          "destination table name changed",
			itemsSchemaFP: "fp-items-v2",
			modifyTI: func(m map[string]source.CDCChunkTableIdentity) {
				ti := m["public.items"]
				ti.DestTable = "raw.items_renamed"
				m["public.items"] = ti
			},
			wantErr: "destination table for public.items changed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := completedMultiTableChunkState("old-identity")
			tables := []source.SourceTableInfo{
				makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
				makeChunkTestTable("public.items", "1002", tc.itemsSchemaFP),
			}
			identities := defaultTableIdentities()
			tc.modifyTI(identities)

			_, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
			require.ErrorContains(t, err, tc.wantErr)
			require.ErrorContains(t, err, "--full-refresh")
		})
	}
}

func TestReconcileChunkStateAllowsStartupEvolutionRecreation(t *testing.T) {
	state := completedMultiTableChunkState("old-identity")
	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "1002", "fp-items-v2"),
	}
	identities := defaultTableIdentities()
	identities["public.items"] = source.CDCChunkTableIdentity{
		DestTable:            "raw.items",
		PhysicalIdentity:     "dest-items-v2-evolved",
		PreEvolutionIdentity: "dest-items-v1",
	}

	reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
	require.NoError(t, err)
	require.Len(t, reconciled.resnapshot, 1)
	require.Equal(t, "public.items", reconciled.resnapshot[0].Name)
}

func TestReconcileChunkStateFailsClosedOnPrimaryKeyChange(t *testing.T) {
	state := completedMultiTableChunkState("old-identity")
	changed := makeChunkTestTable("public.items", "1002", "fp-items-v2")
	changed.PrimaryKeys = []string{"tenant_id", "id"}
	changed.Schema.PrimaryKeys = []string{"tenant_id", "id"}
	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		changed,
	}

	_, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, defaultTableIdentities())
	require.ErrorContains(t, err, "primary keys for public.items changed")
	require.ErrorContains(t, err, "--full-refresh")
}

func TestReconcileChunkStateSimultaneousTableSelectionAndSchemaChange(t *testing.T) {
	state := completedMultiTableChunkState("old-identity")
	state.Tables["public.gone"] = &chunkTableState{
		Boundary: FormatLSN(100), Done: true,
		Incarnation: "1003", SchemaFingerprint: "fp-gone-v1",
		PrimaryKeys: []string{"id"}, DestTable: "raw.gone", DestIdentity: "dest-gone-v1",
	}

	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "1002", "fp-items-v2"),
		makeChunkTestTable("public.orders", "1004", "fp-orders-v1"),
	}
	identities := defaultTableIdentities()
	identities["public.orders"] = source.CDCChunkTableIdentity{
		DestTable:            "raw.orders",
		PhysicalIdentity:     "dest-orders-v1",
		PreEvolutionIdentity: "dest-orders-v1",
	}

	reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
	require.NoError(t, err)
	require.NotContains(t, state.Tables, "public.gone")
	require.True(t, state.Tables["public.anchor"].Done)
	require.False(t, state.Tables["public.orders"].Done)
	require.Equal(t, "1004", state.Tables["public.orders"].Incarnation)
	require.Equal(t, "dest-orders-v1", state.Tables["public.orders"].DestIdentity)
	require.Len(t, reconciled.resnapshot, 1)
	require.Equal(t, "public.items", reconciled.resnapshot[0].Name)

	// Simultaneous table addition + external destination replacement on a
	// retained table must still fail closed.
	state2 := completedMultiTableChunkState("old-identity")
	badIdentities := defaultTableIdentities()
	badIdentities["public.orders"] = identities["public.orders"]
	bad := badIdentities["public.anchor"]
	bad.PhysicalIdentity = "dest-anchor-replaced"
	bad.PreEvolutionIdentity = "dest-anchor-replaced"
	badIdentities["public.anchor"] = bad
	_, err = reconcileChunkState(state2, tables, "new-identity", "serial", nil, badIdentities)
	require.ErrorContains(t, err, "destination table for public.anchor was replaced")
	require.ErrorContains(t, err, "--full-refresh")
}

func TestReconcileChunkStateLegacyCheckpointResnapshotAndFailClosed(t *testing.T) {
	legacyState := func() *chunkBootstrapState {
		return &chunkBootstrapState{
			Version: 1, Epoch: "epoch-legacy", Identity: "old-identity",
			Slot: "slot", Mode: "inmemory", Execution: "serial",
			SnapshotComplete: true, Applied: 100,
			Tables: map[string]*chunkTableState{
				"public.anchor": {Boundary: FormatLSN(100), Done: true},
				"public.items":  {Boundary: FormatLSN(100), Done: true},
			},
		}
	}

	// Legacy schema fingerprints in cdc_state are 18-char prefixes.
	anchorFP := "111111111111111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	itemsOldFP := "222222222222222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	itemsNewFP := "333333333333333333cccccccccccccccccccccccccccccccccccccccccccccc"
	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", anchorFP),
		makeChunkTestTable("public.items", "2002", itemsNewFP),
	}

	t.Run("resnapshots changed table using last certified state", func(t *testing.T) {
		state := legacyState()
		identities := map[string]source.CDCChunkTableIdentity{
			"public.anchor": {
				DestTable:             "raw.anchor",
				PhysicalIdentity:      "dest-anchor-v1",
				PreEvolutionIdentity:  "dest-anchor-v1",
				DestPrimaryKeys:       []string{"id"},
				LastCertifiedDest:     "raw.anchor",
				LastCertifiedPhysical: "dest-anchor-v1",
				LastCertifiedSource:   "1001",
				LastCertifiedSchema:   anchorFP[:18],
			},
			"public.items": {
				DestTable:             "raw.items",
				PhysicalIdentity:      "dest-items-v1",
				PreEvolutionIdentity:  "dest-items-v1",
				DestPrimaryKeys:       []string{"id"},
				LastCertifiedDest:     "raw.items",
				LastCertifiedPhysical: "dest-items-v1",
				LastCertifiedSource:   "1002",
				LastCertifiedSchema:   itemsOldFP[:18],
			},
		}

		reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
		require.NoError(t, err)
		require.Len(t, reconciled.resnapshot, 1)
		require.Equal(t, "public.items", reconciled.resnapshot[0].Name)
		require.Equal(t, uint32(1002), reconciled.oldRelIDs["public.items"])
		// Unchanged legacy table metadata is backfilled.
		require.Equal(t, "1001", state.Tables["public.anchor"].Incarnation)
		require.Equal(t, anchorFP, state.Tables["public.anchor"].SchemaFingerprint)
		require.Equal(t, "dest-anchor-v1", state.Tables["public.anchor"].DestIdentity)
		require.Equal(t, []string{"id"}, state.Tables["public.anchor"].PrimaryKeys)
	})

	t.Run("fails closed when legacy destination was replaced", func(t *testing.T) {
		state := legacyState()
		identities := map[string]source.CDCChunkTableIdentity{
			"public.anchor": {
				DestTable:             "raw.anchor",
				PhysicalIdentity:      "dest-anchor-v1",
				PreEvolutionIdentity:  "dest-anchor-v1",
				DestPrimaryKeys:       []string{"id"},
				LastCertifiedDest:     "raw.anchor",
				LastCertifiedPhysical: "dest-anchor-v1",
				LastCertifiedSource:   "1001",
				LastCertifiedSchema:   anchorFP[:18],
			},
			"public.items": {
				DestTable:             "raw.items",
				PhysicalIdentity:      "dest-items-replaced",
				PreEvolutionIdentity:  "dest-items-replaced",
				DestPrimaryKeys:       []string{"id"},
				LastCertifiedDest:     "raw.items",
				LastCertifiedPhysical: "dest-items-v1",
				LastCertifiedSource:   "1002",
				LastCertifiedSchema:   itemsOldFP[:18],
			},
		}

		_, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
		require.ErrorContains(t, err, "was replaced")
		require.ErrorContains(t, err, "--full-refresh")
	})

	t.Run("fails closed when unfinished legacy table lacks identity proof alongside changed table", func(t *testing.T) {
		state := &chunkBootstrapState{
			Version: 1, Epoch: "epoch-legacy-unfinished", Identity: "old-identity",
			Slot: "slot", Mode: "inmemory", Execution: "parallel",
			Applied: 100,
			Tables: map[string]*chunkTableState{
				"public.anchor": {Boundary: FormatLSN(100), Done: true},
				"public.items":  {Boundary: FormatLSN(100), Done: false, Initialized: true, After: []string{"500"}},
			},
		}
		// Table A (public.anchor) changed schema and has a completed certificate,
		// while unfinished legacy table B (public.items) has no DestIdentity and no
		// completed certificate (its destination was externally replaced).
		identities := map[string]source.CDCChunkTableIdentity{
			"public.anchor": {
				DestTable:             "raw.anchor",
				PhysicalIdentity:      "dest-anchor-v1",
				PreEvolutionIdentity:  "dest-anchor-v1",
				DestPrimaryKeys:       []string{"id"},
				LastCertifiedDest:     "raw.anchor",
				LastCertifiedPhysical: "dest-anchor-v1",
				LastCertifiedSource:   "1001",
				LastCertifiedSchema:   anchorFP[:18],
			},
			"public.items": {
				DestTable:            "raw.items",
				PhysicalIdentity:     "dest-items-empty-replacement",
				PreEvolutionIdentity: "dest-items-empty-replacement",
			},
		}
		changedTables := []source.SourceTableInfo{
			makeChunkTestTable("public.anchor", "1001", itemsNewFP),
			makeChunkTestTable("public.items", "1002", itemsOldFP),
		}

		_, err := reconcileChunkState(state, changedTables, "new-identity", "parallel", nil, identities)
		require.ErrorContains(t, err, "lacks per-table identity proof for public.items")
		require.ErrorContains(t, err, "--full-refresh")
	})

	t.Run("fails closed when legacy proof omits source OID, schema, or primary keys", func(t *testing.T) {
		cases := []struct {
			name    string
			mutate  func(ti *source.CDCChunkTableIdentity)
			wantErr string
		}{
			{
				name:    "missing LastCertifiedSource",
				mutate:  func(ti *source.CDCChunkTableIdentity) { ti.LastCertifiedSource = "" },
				wantErr: "lacks per-table identity proof for public.anchor",
			},
			{
				name:    "missing LastCertifiedSchema",
				mutate:  func(ti *source.CDCChunkTableIdentity) { ti.LastCertifiedSchema = "" },
				wantErr: "lacks per-table identity proof for public.anchor",
			},
			{
				name:    "missing DestPrimaryKeys",
				mutate:  func(ti *source.CDCChunkTableIdentity) { ti.DestPrimaryKeys = nil },
				wantErr: "lacks per-table identity proof for public.anchor",
			},
			{
				name:    "mismatched DestPrimaryKeys",
				mutate:  func(ti *source.CDCChunkTableIdentity) { ti.DestPrimaryKeys = []string{"other_id"} },
				wantErr: "primary keys for public.anchor changed",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				state := legacyState()
				anchorTI := source.CDCChunkTableIdentity{
					DestTable:             "raw.anchor",
					PhysicalIdentity:      "dest-anchor-v1",
					PreEvolutionIdentity:  "dest-anchor-v1",
					DestPrimaryKeys:       []string{"id"},
					LastCertifiedDest:     "raw.anchor",
					LastCertifiedPhysical: "dest-anchor-v1",
					LastCertifiedSource:   "1001",
					LastCertifiedSchema:   anchorFP[:18],
				}
				tc.mutate(&anchorTI)
				identities := map[string]source.CDCChunkTableIdentity{
					"public.anchor": anchorTI,
					"public.items": {
						DestTable:             "raw.items",
						PhysicalIdentity:      "dest-items-v1",
						PreEvolutionIdentity:  "dest-items-v1",
						DestPrimaryKeys:       []string{"id"},
						LastCertifiedDest:     "raw.items",
						LastCertifiedPhysical: "dest-items-v1",
						LastCertifiedSource:   "1002",
						LastCertifiedSchema:   itemsOldFP[:18],
					},
				}
				_, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
				require.ErrorContains(t, err, tc.wantErr)
				require.ErrorContains(t, err, "--full-refresh")
			})
		}
	})

	t.Run("fails closed when checkpoint Identity is empty", func(t *testing.T) {
		state := completedMultiTableChunkState("")
		tablesUnchanged := []source.SourceTableInfo{
			makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
			makeChunkTestTable("public.items", "1002", "fp-items-v1"),
		}
		_, err := reconcileChunkState(state, tablesUnchanged, "new-identity", "serial", nil, defaultTableIdentities())
		require.ErrorContains(t, err, "source or destination changed")
	})

	t.Run("fails closed when legacy checkpoint has no identity provider", func(t *testing.T) {
		state := legacyState()
		_, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, nil)
		require.ErrorContains(t, err, "source or destination changed")
		require.ErrorContains(t, err, "--full-refresh")
	})
}

func TestReconcileChunkStateAllowsStaleEvolutionIntentAfterCheckpointUpdated(t *testing.T) {
	// If a crash happened right after checkpointResnapshot saved DestIdentity=v2
	// and before the durable evolution-intent key was deleted, TableIdentities
	// may still report PreEvolutionIdentity=v1 alongside PhysicalIdentity=v2.
	// The updated checkpoint already matches PhysicalIdentity=v2 and must resume
	// cleanly without resnapshotting or failing closed.
	state := completedMultiTableChunkState("new-identity")
	state.Tables["public.items"].DestIdentity = "dest-items-v2-evolved"
	state.Tables["public.items"].SchemaFingerprint = "fp-items-v2"

	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "1002", "fp-items-v2"),
	}
	identities := defaultTableIdentities()
	identities["public.items"] = source.CDCChunkTableIdentity{
		DestTable:            "raw.items",
		PhysicalIdentity:     "dest-items-v2-evolved",
		PreEvolutionIdentity: "dest-items-v1",
	}

	reconciled, err := reconcileChunkState(state, tables, "new-identity", "serial", nil, identities)
	require.NoError(t, err)
	require.Empty(t, reconciled.resnapshot)
}

func TestChunkResnapshotEndToEndUpdatesCheckpointAndSkipsStaleWAL(t *testing.T) {
	ctx := t.Context()
	store := &chunkIdentityTestStore{
		chunkTestStore: newChunkTestStore(),
		identities:     defaultTableIdentities(),
	}
	store.identity = "new-identity"

	initial := completedMultiTableChunkState("old-identity")
	initial.Mode = "destination"
	initial.Execution = "parallel"
	initial.SnapshotComplete = false
	initialBytes, err := json.Marshal(initial)
	require.NoError(t, err)
	store.values[chunkStateKey] = initialBytes

	// Stage a pre-DDL WAL journal page that has not been replayed yet;
	// checkpointResnapshot must NOT prune or advance Consumed past it.
	journal := &chunkJournal{store: store, epoch: initial.Epoch}
	require.NoError(t, journal.load(ctx, 0))
	require.NoError(t, journal.append(ctx, []DecodedChanges{
		{
			TableName: "public.items",
			LSN:       120,
			Changes:   []Change{{Operation: "TRUNCATE", LSN: 120}},
		},
		{
			TableName: "public.anchor",
			LSN:       120,
			Changes:   []Change{{Operation: "INSERT", LSN: 120, Values: []any{int64(1)}}},
		},
	}, 120))

	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "2002", "fp-items-v2",
			schema.Column{Name: "id", DataType: schema.TypeInt64},
			schema.Column{Name: "note", DataType: schema.TypeString},
		),
	}

	reconciled, err := reconcileChunkState(initial, tables, store.identity, "parallel", nil, store.identities)
	require.NoError(t, err)
	require.Len(t, reconciled.resnapshot, 1)
	require.Equal(t, "public.items", reconciled.resnapshot[0].Name)
	require.Equal(t, uint32(1002), reconciled.oldRelIDs["public.items"])

	results := make(chan source.RecordBatchResult, 16)
	schemas := map[string]*schema.TableSchema{
		"public.anchor": tables[0].Schema,
		"public.items":  tables[1].Schema,
	}
	reader := &chunkReader{
		tables:              tables,
		options:             &source.CDCChunkedSnapshotOptions{Store: store, WALStaging: "destination", Execution: "parallel", ChunkSize: 100, WALBufferBytes: 1 << 20},
		state:               initial,
		journal:             journal,
		accum:               newBatchAccumulator(100, schemas),
		results:             results,
		multi:               true,
		targetIdentity:      store.identity,
		tableIdentities:     store.identities,
		resnapshotTables:    reconciled.resnapshot,
		resnapshotOldRelIDs: reconciled.oldRelIDs,
	}
	reader.accum.durable = func() pglogrepl.LSN { return reader.state.Applied }
	defer func() { _ = reader.accum.toast.close() }()

	var resnapshotted []string
	reader.resnapshotFn = func(_ context.Context, _ string, targets []source.SourceTableInfo) (map[string]pglogrepl.LSN, error) {
		out := make(map[string]pglogrepl.LSN, len(targets))
		for _, target := range targets {
			resnapshotted = append(resnapshotted, target.Name)
			out[target.Name] = 140
		}
		return out, nil
	}

	// Consumer goroutine acknowledges commit tokens and records emitted batches.
	type emittedBatch struct {
		table string
		rows  int64
	}
	var flushedTokens []source.CDCStateCommitToken
	var batches []emittedBatch
	consumerDone := make(chan error, 1)
	go func() {
		for res := range results {
			if res.Batch != nil {
				batches = append(batches, emittedBatch{table: res.TableName, rows: res.Batch.NumRows()})
				res.Batch.Release()
			}
			if res.CDCFlush {
				tok, ok := res.CommitToken.(source.CDCStateCommitToken)
				if !ok {
					consumerDone <- nil
					return
				}
				flushedTokens = append(flushedTokens, tok)
				commit := tok.SourceCommitToken.(*chunkedCommit)
				if err := commit.persist(ctx); err != nil {
					consumerDone <- err
					return
				}
				commit.ack.Do(func() { close(commit.done) })
			}
		}
		consumerDone <- nil
	}()

	require.NoError(t, reader.resnapshotChangedTables(ctx, "slot"))
	require.Equal(t, []string{"public.items"}, resnapshotted, "only the changed table should be resnapshotted")
	require.Len(t, flushedTokens, 1)
	require.Equal(t, FormatLSN(100), flushedTokens[0].Position, "shared Applied LSN must not advance before WAL replay")
	require.Equal(t, map[string]string{"public.items": FormatLSN(140)}, flushedTokens[0].SnapshotPositions)
	require.Equal(t, map[string]string{"public.items": "2002"}, flushedTokens[0].SnapshotIncarnations)
	require.Equal(t, map[string]string{"public.items": "fp-items-v2"}, flushedTokens[0].SnapshotSchemas)

	// Verify durable checkpoint right after resnapshot (simulates crash before WAL catch-up).
	midState, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, "new-identity", midState.Identity)
	require.Equal(t, pglogrepl.LSN(100), midState.Applied)
	require.Equal(t, uint64(0), midState.Consumed, "unplayed journal pages must not be pruned during resnapshot checkpoint")
	require.Equal(t, FormatLSN(140), midState.Tables["public.items"].ReplayFrom)
	require.Equal(t, []uint32{1002}, midState.Tables["public.items"].HistoricalRelIDs)
	require.True(t, midState.Tables["public.items"].Done)
	require.False(t, midState.Tables["public.items"].Initialized)

	// A crash-recovery restart must NOT resnapshot public.items again, while
	// still preserving ReplayFrom and HistoricalRelIDs.
	recoveryReconciled, err := reconcileChunkState(midState.clone(), tables, store.identity, "parallel", nil, store.identities)
	require.NoError(t, err)
	require.Empty(t, recoveryReconciled.resnapshot)
	require.Equal(t, map[string]string{"public.items": FormatLSN(140)}, midState.replayPositions())
	require.Equal(t, map[string]map[uint32]struct{}{"public.items": {1002: {}}}, midState.historicalRelationIDs())
	require.Equal(t, map[string]pglogrepl.LSN{"public.items": 140}, midState.staleRelationsBefore())

	// Replay the staged journal: pre-DDL TRUNCATE on public.items at LSN 120 (< 140)
	// must be skipped, while public.anchor INSERT at LSN 120 must be applied.
	require.NoError(t, journal.replay(ctx, reader.state.Consumed, func(groups []DecodedChanges) error {
		return reader.apply(ctx, groups)
	}))
	// Apply post-snapshot WAL on public.items at LSN 150 (>= 140).
	require.NoError(t, reader.apply(ctx, []DecodedChanges{
		{
			TableName: "public.items",
			LSN:       150,
			Changes:   []Change{{Operation: "INSERT", LSN: 150, Values: []any{int64(2), "after-ddl"}}},
		},
	}))

	// Final catch-up checkpoint at LSN 160 (>= 140) clears ReplayFrom and HistoricalRelIDs.
	require.NoError(t, reader.checkpoint(ctx, 160, nil))
	close(results)
	require.NoError(t, <-consumerDone)

	require.ElementsMatch(t, []emittedBatch{
		{table: "public.anchor", rows: 1},
		{table: "public.items", rows: 1},
	}, batches)

	finalState, err := loadChunkState(ctx, store)
	require.NoError(t, err)
	require.Equal(t, pglogrepl.LSN(160), finalState.Applied)
	require.Equal(t, uint64(1), finalState.Consumed)
	require.Empty(t, finalState.Tables["public.items"].ReplayFrom)
	require.Empty(t, finalState.Tables["public.items"].HistoricalRelIDs)

	// Next run with identical tables and schemas must resume cleanly without resnapshotting.
	nextRun, err := reconcileChunkState(finalState.clone(), tables, store.identity, "parallel", nil, store.identities)
	require.NoError(t, err)
	require.Empty(t, nextRun.resnapshot)
}

func TestChunkInitializeFailsClosedOnSlotOrModeChangeEvenWithSchemaChange(t *testing.T) {
	store := &chunkIdentityTestStore{
		chunkTestStore: newChunkTestStore(),
		identities:     defaultTableIdentities(),
	}
	store.identity = "new-identity"
	saved := completedMultiTableChunkState("old-identity")
	data, err := json.Marshal(saved)
	require.NoError(t, err)
	store.values[chunkStateKey] = data

	reader := &chunkReader{
		tables: []source.SourceTableInfo{
			makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
			makeChunkTestTable("public.items", "1002", "fp-items-v2"),
		},
		options: &source.CDCChunkedSnapshotOptions{
			Store:      store,
			WALStaging: "inmemory",
			Execution:  "serial",
		},
	}

	_, _, err = reader.initialize(t.Context(), "other-slot", nil)
	require.ErrorContains(t, err, "slot, execution, or WAL staging mode changed")
	require.ErrorContains(t, err, "--full-refresh")
}

func TestChunkCheckpointClearsOrphanedEvolutionIntent(t *testing.T) {
	ctx := t.Context()
	identities := defaultTableIdentities()
	identities["public.items"] = source.CDCChunkTableIdentity{
		DestTable:            "raw.items",
		PhysicalIdentity:     "dest-items-v2",
		PreEvolutionIdentity: "dest-items-v1",
	}
	store := &chunkIdentityTestStore{
		chunkTestStore: newChunkTestStore(),
		identities:     identities,
	}
	store.identity = "new-identity"
	store.values[source.CDCChunkEvolutionIntentKey] = []byte(`{"version":1,"tables":{"public.items":{"dest_table":"raw.items","pre_identity":"dest-items-v1","post_identity":"dest-items-v2"}}}`)

	tables := []source.SourceTableInfo{
		makeChunkTestTable("public.anchor", "1001", "fp-anchor-v1"),
		makeChunkTestTable("public.items", "1002", "fp-items-v2"),
	}
	state := completedMultiTableChunkState("new-identity")
	state.Tables["public.items"].DestIdentity = "dest-items-v2"
	state.Tables["public.items"].SchemaFingerprint = "fp-items-v2"

	results := make(chan source.RecordBatchResult, 4)
	schemas := map[string]*schema.TableSchema{
		"public.anchor": tables[0].Schema,
		"public.items":  tables[1].Schema,
	}
	reader := &chunkReader{
		tables:          tables,
		options:         &source.CDCChunkedSnapshotOptions{Store: store, WALStaging: "inmemory", Execution: "serial", ChunkSize: 100, WALBufferBytes: 1 << 20},
		state:           state,
		accum:           newBatchAccumulator(100, schemas),
		results:         results,
		multi:           true,
		targetIdentity:  store.identity,
		tableIdentities: identities,
	}
	reader.accum.durable = func() pglogrepl.LSN { return reader.state.Applied }
	defer func() { _ = reader.accum.toast.close() }()

	done := make(chan error, 1)
	go func() {
		for res := range results {
			if res.CDCFlush {
				tok := res.CommitToken.(source.CDCStateCommitToken)
				commit := tok.SourceCommitToken.(*chunkedCommit)
				if err := commit.persist(ctx); err != nil {
					done <- err
					return
				}
				commit.ack.Do(func() { close(commit.done) })
			}
		}
		done <- nil
	}()

	require.NoError(t, reader.checkpoint(ctx, 120, nil))
	close(results)
	require.NoError(t, <-done)

	raw, err := store.Load(ctx, source.CDCChunkEvolutionIntentKey)
	require.NoError(t, err)
	require.Nil(t, raw, "ordinary checkpoint must clear orphaned evolution intent when no resnapshot is pending")
	require.True(t, reader.evolutionIntentCleared)
}
