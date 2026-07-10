package strategy

import (
	"context"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

type storedCDCState struct {
	connectorID string
	entry       destination.CDCStateEntry
}

type cdcStateDestination struct {
	*fakeDestination
	stateMu sync.Mutex
	states  map[string][]storedCDCState
}

func newCDCStateDestination() *cdcStateDestination {
	return &cdcStateDestination{fakeDestination: &fakeDestination{}, states: make(map[string][]storedCDCState)}
}

func (d *cdcStateDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		batch := result.Batch
		connectors := batch.Column(2).(*array.String)
		sourceTables := batch.Column(3).(*array.String)
		kinds := batch.Column(5).(*array.String)
		generations := batch.Column(6).(*array.Int64)
		statuses := batch.Column(7).(*array.String)
		positions := batch.Column(8).(*array.String)

		d.stateMu.Lock()
		for i := 0; i < int(batch.NumRows()); i++ {
			d.states[opts.Table] = append(d.states[opts.Table], storedCDCState{
				connectorID: connectors.Value(i),
				entry: destination.CDCStateEntry{
					SourceTable: sourceTables.Value(i),
					StateKind:   kinds.Value(i),
					Generation:  generations.Value(i),
					Status:      statuses.Value(i),
					Position:    positions.Value(i),
				},
			})
		}
		d.stateMu.Unlock()
		batch.Release()
	}
	return nil
}

func (d *cdcStateDestination) LoadCDCState(_ context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	var entries []destination.CDCStateEntry
	for _, state := range d.states[table] {
		if state.connectorID == connectorID {
			entries = append(entries, state.entry)
		}
	}
	return entries, nil
}

func TestCDCStateRequiresLatestGenerationCompletedSnapshot(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 2 || got[0] != "connector_id" || got[1] != "event_id" {
		t.Fatalf("shared state primary keys = %v, want [connector_id event_id]", got)
	}

	position, err := manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("new connector became resumable at %s", position)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020"}); err != nil {
		t.Fatal(err)
	}
	position, err = manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("checkpoint without a snapshot marker became resumable at %s", position)
	}

	if err := manager.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}
	position, err = manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if want := "00000000/00000020"; position != want {
		t.Fatalf("resume position = %s, want latest checkpoint %s", position, want)
	}

	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	position, err = manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("in-progress generation remained resumable at %s", position)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000030"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000040"}); err != nil {
		t.Fatal(err)
	}
	position, err = manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if want := "00000000/00000040"; position != want {
		t.Fatalf("restored resume position = %s, want %s", position, want)
	}
	completedSnapshots := 0
	for _, state := range dest.states[manager.stateTable] {
		if state.entry.StateKind == cdcStateKindSnapshot && state.entry.Generation == 2 && state.entry.Status == destination.CDCStateStatusComplete {
			completedSnapshots++
		}
	}
	if completedSnapshots != 1 {
		t.Fatalf("completed snapshot events in generation 2 = %d, want 1", completedSnapshots)
	}
	if manager.stateTable != "_bruin_staging.cdc_state" {
		t.Fatalf("unexpected shared state table: %s", manager.stateTable)
	}
}

func TestCDCStateConnectorsShareTableWithoutSharingRows(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	a, err := NewCDCStateManager(dest, "aaaaaaaaaaaaaaaa", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewCDCStateManager(dest, "bbbbbbbbbbbbbbbb", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, manager := range []*CDCStateManager{a, b} {
		if err := manager.RegisterTable(ctx, "db1.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "db2.orders", "raw.orders_archive"); err != nil {
			t.Fatal(err)
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	if a.stateTable != b.stateTable {
		t.Fatalf("connectors use different state tables: %s != %s", a.stateTable, b.stateTable)
	}
	if err := a.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"db1.orders": "00000000/00000010",
		"db2.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}
	if position, err := b.ResumePosition(ctx, "db1.orders"); err != nil || position != "" {
		t.Fatalf("connector B observed connector A state: position=%s err=%v", position, err)
	}
}

func TestCDCStateFullRefreshDoesNotRestorePreviousMarkers(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResumePosition(ctx, "public.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020"}); err != nil {
		t.Fatal(err)
	}
	if position, err := manager.ResumePosition(ctx, "public.orders"); err != nil || position != "" {
		t.Fatalf("full refresh restored an old marker: position=%s err=%v", position, err)
	}
}
