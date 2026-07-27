package strategy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type storedCDCState struct {
	connectorID string
	entry       destination.CDCStateEntry
}

type cdcStateDestination struct {
	*fakeDestination
	catalog        string
	policy         destination.ReplaceStagingPolicy
	stateMu        sync.Mutex
	states         map[string][]storedCDCState
	pruneErr       error
	prunes         int
	pruneSizes     []int
	missing        map[string]bool
	incarnations   map[string]string
	cdcWrites      int
	stateLoads     int
	fenceLoads     int
	maxFence       int
	maxWrite       int
	failWrite      int
	pruneBatchSize int
	targets        map[string]string
}

type caseCanonicalCDCStateDestination struct {
	*cdcStateDestination
}

func (d *caseCanonicalCDCStateDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	claim.DestinationTable = strings.ToLower(claim.DestinationTable)
	return d.cdcStateDestination.ClaimCDCTarget(ctx, claimTable, claim)
}

func (d *cdcStateDestination) ManagedCDCStateCatalog() string {
	return d.catalog
}

func (d *cdcStateDestination) CDCStatePruneBatchSize() int {
	return d.pruneBatchSize
}

func (d *cdcStateDestination) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.policy
}

func (d *cdcStateDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.stateMu.Lock()
	d.cdcWrites++
	writeNumber := d.cdcWrites
	fail := d.failWrite == writeNumber
	d.stateMu.Unlock()
	if fail {
		for result := range records {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		return errors.New("injected CDC state write failure")
	}
	return d.WriteParallel(ctx, records, opts)
}

func (d *cdcStateDestination) GetTableSchema(_ context.Context, table string) (*schema.TableSchema, error) {
	if d.missing[table] {
		return nil, nil
	}
	return &schema.TableSchema{}, nil
}

func (d *cdcStateDestination) CDCTargetIncarnation(_ context.Context, table string) (string, bool, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.missing[table] {
		return "", false, nil
	}
	incarnation := d.incarnations[table]
	if incarnation == "" {
		incarnation = "incarnation:" + table
	}
	return incarnation, true, nil
}

func newCDCStateDestination() *cdcStateDestination {
	return &cdcStateDestination{
		fakeDestination: &fakeDestination{},
		states:          make(map[string][]storedCDCState),
		targets:         make(map[string]string),
		missing:         make(map[string]bool),
		incarnations:    make(map[string]string),
	}
}

func (d *cdcStateDestination) ClaimCDCTarget(_ context.Context, _ string, claim destination.CDCTargetClaim) error {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	ownerID := destination.CDCTargetOwnerID(claim.ConnectorID, claim.SourceTable)
	if owner := d.targets[claim.DestinationTable]; owner != "" && owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC owner %q", claim.DestinationTable, owner)
	}
	d.targets[claim.DestinationTable] = ownerID
	return nil
}

func (d *cdcStateDestination) ClaimAndPrepareEmptyCDCTarget(_ context.Context, _ string, claim destination.CDCTargetClaim, _ destination.PrepareOptions) (string, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if !d.missing[claim.DestinationTable] {
		return "", fmt.Errorf("destination table %q already exists", claim.DestinationTable)
	}
	ownerID := destination.CDCTargetOwnerID(claim.ConnectorID, claim.SourceTable)
	if owner := d.targets[claim.DestinationTable]; owner != "" && owner != ownerID {
		return "", fmt.Errorf("destination table %q is already claimed by CDC owner %q", claim.DestinationTable, owner)
	}
	d.targets[claim.DestinationTable] = ownerID
	delete(d.missing, claim.DestinationTable)
	incarnation := d.incarnations[claim.DestinationTable]
	if incarnation == "" {
		incarnation = "incarnation:" + claim.DestinationTable
		d.incarnations[claim.DestinationTable] = incarnation
	}
	return incarnation, nil
}

func (d *cdcStateDestination) TruncateCDCTableIfIncarnation(_ context.Context, table, expected string) error {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.missing[table] {
		return fmt.Errorf("destination table %q is missing", table)
	}
	current := d.incarnations[table]
	if current == "" {
		current = "incarnation:" + table
	}
	if current != expected {
		return fmt.Errorf("destination table %q physical incarnation changed", table)
	}
	return nil
}

func TestStateReadRegistrationDoesNotPrepareTable(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "read-only-connector", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	manager.RegisterTableForRead("public.orders", "raw.orders")

	position, err := manager.ResumePosition(t.Context(), "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("ResumePosition() = %q, want empty", position)
	}
	if got := len(dest.prepareCalls); got != 0 {
		t.Fatalf("read-only registration prepared %d tables, want zero", got)
	}
}

func TestCDCStateRequiresMatchingSourceTableIncarnation(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	first, err := NewCDCStateManager(dest, "incarnation-connector", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"); err != nil {
		t.Fatal(err)
	}
	if err := first.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := first.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	}); err != nil {
		t.Fatal(err)
	}

	same, err := NewCDCStateManager(dest, "incarnation-connector", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := same.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"); err != nil {
		t.Fatal(err)
	}
	if got, err := same.ResumePosition(ctx, "public.orders"); err != nil || got != "00000000/00000020" {
		t.Fatalf("same incarnation resume = %q, %v", got, err)
	}

	recreated, err := NewCDCStateManager(dest, "incarnation-connector", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "101"); err != nil {
		t.Fatal(err)
	}
	if got, err := recreated.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
		t.Fatalf("recreated table resume = %q, %v; want forced snapshot", got, err)
	}
}

func TestCDCStateRequiresMatchingSourceSchemaFingerprint(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	first, err := NewCDCStateManager(dest, "schema-fingerprint-connector", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableState(ctx, "public.orders", "raw.orders", "100", "schema-a"))
	require.NoError(t, first.BeginRun(ctx, false))
	require.NoError(t, first.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
		SnapshotSchemas:      map[string]string{"public.orders": "schema-a"},
	}))

	same, err := NewCDCStateManager(dest, "schema-fingerprint-connector", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, same.RegisterTableState(ctx, "public.orders", "raw.orders", "100", "schema-a"))
	position, err := same.ResumePosition(ctx, "public.orders")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", position)

	changed, err := NewCDCStateManager(dest, "schema-fingerprint-connector", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, changed.RegisterTableState(ctx, "public.orders", "raw.orders", "100", "schema-b"))
	position, err = changed.ResumePosition(ctx, "public.orders")
	require.NoError(t, err)
	require.Empty(t, position, "same-OID offline DDL must force a replacement snapshot")
}

func TestCDCStateSchemaEncodingFitsExistingPositionColumn(t *testing.T) {
	encoded := encodeCDCStatePositionWithSchema(
		"FFFFFFFF/FFFFFFFF",
		"4294967295",
		strings.Repeat("f", 64),
		^uint64(0),
	)
	require.LessOrEqual(t, len(encoded), 64)
	position, incarnation, fingerprint, epoch := decodeCDCStatePositionWithSchema(encoded)
	require.Equal(t, "FFFFFFFF/FFFFFFFF", position)
	require.Equal(t, "4294967295", incarnation)
	require.Equal(t, strings.Repeat("f", 18), fingerprint)
	require.Equal(t, ^uint64(0), epoch)
}

func TestCDCStateWALTruncatePreservesRegisteredSourceIncarnation(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "truncate-incarnation", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	}))

	require.NoError(t, manager.InvalidateSnapshot(ctx, "public.orders", "raw.orders", ""))
	require.NoError(t, manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000030"},
		SnapshotIncarnations: map[string]string{"public.orders": ""},
	}))

	restarted, err := NewCDCStateManager(dest, "truncate-incarnation", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"))
	position, err := restarted.ResumePosition(ctx, "public.orders")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000030", position)
}

func TestCDCStateRequiresMatchingDestinationTableIncarnation(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "destination-100"
	manager, err := NewCDCStateManager(dest, "destination-incarnation", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "source-100"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "source-100"},
	}); err != nil {
		t.Fatal(err)
	}

	dest.incarnations["raw.orders"] = "destination-101"
	restarted, err := NewCDCStateManager(dest, "destination-incarnation", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "source-100"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
		t.Fatalf("recreated destination resume = %q, %v; want forced snapshot", got, err)
	}
}

func TestCDCStateExposesBoundRawDestinationIncarnation(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "physical-target-42"
	manager, err := NewCDCStateManager(dest, "bound-incarnation", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTable(ctx, "public.orders", "raw.orders"))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.NoError(t, manager.BindDestinationIncarnation(ctx, "public.orders", "raw.orders"))

	incarnation, err := manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "physical-target-42", incarnation)
}

func TestCDCStateRejectsTargetReplacementAfterResumeBeforeBind(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "destination-100"
	first, err := NewCDCStateManager(dest, "bind-race", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, first.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "source-100"))
	require.NoError(t, first.BeginRun(ctx, false))
	require.NoError(t, first.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "source-100"},
	}))

	restarted, err := NewCDCStateManager(dest, "bind-race", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, restarted.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "source-100"))
	position, err := restarted.ResumePosition(ctx, "public.orders")
	require.NoError(t, err)
	require.Equal(t, "00000000/00000020", position)
	require.NoError(t, restarted.BeginRun(ctx, false))

	dest.incarnations["raw.orders"] = "destination-101"
	err = restarted.BindDestinationIncarnation(ctx, "public.orders", "raw.orders")
	require.ErrorContains(t, err, "replaced after its completed snapshot")
}

func TestCDCStateRejectsDestinationReplacementBeforeCheckpoint(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "destination-100"
	manager, err := NewCDCStateManager(dest, "destination-checkpoint", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "source-100"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "source-100"},
	}); err != nil {
		t.Fatal(err)
	}
	dest.incarnations["raw.orders"] = "destination-101"
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000030"}); err == nil || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("checkpoint after destination replacement error = %v", err)
	}
}

func TestCDCStateReplacementSnapshotCrashBoundariesFailClosed(t *testing.T) {
	tests := []struct {
		name              string
		afterInvalidation func(t *testing.T, manager *CDCStateManager)
	}{
		{name: "after invalidation"},
		{name: "after destination truncate"},
		{
			name: "after partial snapshot",
			afterInvalidation: func(t *testing.T, manager *CDCStateManager) {
				t.Helper()
				if err := manager.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000030"}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			dest := newCDCStateDestination()
			manager, err := NewCDCStateManager(dest, "replacement-connector", "raw.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"); err != nil {
				t.Fatal(err)
			}
			if err := manager.BeginRun(ctx, false); err != nil {
				t.Fatal(err)
			}
			if err := manager.Persist(ctx, source.CDCStateCommitToken{
				Position:             "00000000/00000020",
				SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
				SnapshotIncarnations: map[string]string{"public.orders": "100"},
			}); err != nil {
				t.Fatal(err)
			}
			if got, err := manager.ResumePosition(ctx, "public.orders"); err != nil || got != "00000000/00000020" {
				t.Fatalf("pre-invalidation resume = %q, %v", got, err)
			}
			if err := manager.InvalidateSnapshot(ctx, "public.orders", "raw.orders", "101"); err != nil {
				t.Fatal(err)
			}
			if tt.afterInvalidation != nil {
				tt.afterInvalidation(t, manager)
			}

			restarted, err := NewCDCStateManager(dest, "replacement-connector", "raw.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "101"); err != nil {
				t.Fatal(err)
			}
			if got, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
				t.Fatalf("resume after replacement crash boundary = %q, %v; want empty", got, err)
			}
		})
	}
}

func TestCDCStateReplacementSnapshotCompletesCurrentEpoch(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "replacement-complete", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "100"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000010"},
		SnapshotIncarnations: map[string]string{"public.orders": "100"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.InvalidateSnapshot(ctx, "public.orders", "raw.orders", "101"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{
		Position:             "00000000/00000030",
		SnapshotPositions:    map[string]string{"public.orders": "00000000/00000025"},
		SnapshotIncarnations: map[string]string{"public.orders": "101"},
	}); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewCDCStateManager(dest, "replacement-complete", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTableIncarnation(ctx, "public.orders", "raw.orders", "101"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || got != "00000000/00000030" {
		t.Fatalf("completed replacement resume = %q, %v", got, err)
	}
}

func TestCDCTargetClaimsRejectDifferentConnector(t *testing.T) {
	dest := newCDCStateDestination()
	first, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterTable(t.Context(), "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := first.ClaimTarget(t.Context(), "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}

	second, err := NewCDCStateManager(dest, "connector-b", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	err = second.ClaimTarget(t.Context(), "other.orders", "raw.orders")
	if err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("conflicting target claim error = %v", err)
	}
	if len(second.destTables) != 0 {
		t.Fatalf("conflicting connector mutated table mappings: %v", second.destTables)
	}
}

func TestCDCTargetClaimsAreSourceScopedIdempotentAndIndependent(t *testing.T) {
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ClaimTarget(t.Context(), "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ClaimTarget(t.Context(), "public.orders", "raw.orders"); err != nil {
		t.Fatalf("same logical owner could not reclaim target: %v", err)
	}
	if err := manager.ClaimTarget(t.Context(), "archive.orders", "raw.orders"); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("different source table under same connector reclaimed target: %v", err)
	}
	if err := manager.ClaimTarget(t.Context(), "public.customers", "raw.customers"); err != nil {
		t.Fatalf("same connector could not claim another target: %v", err)
	}

	other, err := NewCDCStateManager(dest, "connector-b", "raw.invoices", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.ClaimTarget(t.Context(), "public.invoices", "raw.invoices"); err != nil {
		t.Fatalf("different connector could not claim independent target: %v", err)
	}
}

func TestCDCTargetClaimsAreAtomic(t *testing.T) {
	dest := newCDCStateDestination()
	const target = "raw.orders"
	results := make(chan error, 2)
	for _, connectorID := range []string{"connector-a", "connector-b"} {
		connectorID := connectorID
		go func() {
			manager, err := NewCDCStateManager(dest, connectorID, target, "")
			if err == nil {
				err = manager.ClaimTarget(context.Background(), connectorID+".orders", target)
			}
			results <- err
		}()
	}
	var successes int
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent claims had %d winners, want exactly one", successes)
	}
}

func TestCDCStateClaimsCreatesAndBindsMissingTargetBeforeRun(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.missing["raw.orders"] = true
	manager, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	require.NoError(t, err)

	err = manager.ClaimAndPrepareTarget(ctx, "public.orders", "raw.orders", destination.PrepareOptions{
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	})
	require.NoError(t, err)
	require.False(t, dest.missing["raw.orders"])
	require.Equal(t, destination.CDCTargetOwnerID("connector-a", "public.orders"), dest.targets["raw.orders"])

	incarnation, err := manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "incarnation:raw.orders", incarnation)
	require.NoError(t, manager.BeginRun(ctx, false))
	incarnation, err = manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "incarnation:raw.orders", incarnation)
}

func TestCDCStatePreservesTargetFenceAcrossFullRefreshAndRebindsConditionalSwap(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "old-incarnation"
	manager, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.ClaimAndPrepareTarget(ctx, "public.orders", "raw.orders", destination.PrepareOptions{
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	}))
	require.NoError(t, manager.BeginRun(ctx, true))
	incarnation, err := manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "old-incarnation", incarnation)

	dest.incarnations["raw.orders"] = "swap-result-incarnation"
	require.NoError(t, manager.CompleteConditionalSwap(ctx, "public.orders", "raw.orders", "old-incarnation", "swap-result-incarnation"))
	incarnation, err = manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "swap-result-incarnation", incarnation)
}

func TestCDCStateRebindsDestinationAfterSchemaEvolution(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	dest.incarnations["raw.orders"] = "old-incarnation"
	manager, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	require.NoError(t, err)
	require.NoError(t, manager.ClaimAndPrepareTarget(ctx, "public.orders", "raw.orders", destination.PrepareOptions{
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
	}))
	require.NoError(t, manager.BeginRun(ctx, true))

	dest.incarnations["raw.orders"] = "evolved-incarnation"
	require.NoError(t, manager.CompleteSchemaEvolution(ctx, "public.orders", "raw.orders", "old-incarnation", "evolved-incarnation"))
	incarnation, err := manager.BoundDestinationIncarnation("public.orders")
	require.NoError(t, err)
	require.Equal(t, "evolved-incarnation", incarnation)
}

func TestCDCStateValidationFailureDoesNotClaimTarget(t *testing.T) {
	ctx := t.Context()
	dest := newCDCStateDestination()
	first, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := first.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := first.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{"public.orders": "00000000/00000010"}}); err != nil {
		t.Fatal(err)
	}
	prepareCallsBeforeValidation := len(dest.prepareCalls)
	targetsBeforeValidation := make(map[string]string, len(dest.targets))
	for table, owner := range dest.targets {
		targetsBeforeValidation[table] = owner
	}

	restarted, err := NewCDCStateManager(dest, "connector-a", "other.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "other.orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ResumePosition(ctx, "public.orders"); err == nil || !strings.Contains(err.Error(), "not \"other.orders\"") {
		t.Fatalf("destination ownership validation error = %v", err)
	}
	if !maps.Equal(dest.targets, targetsBeforeValidation) {
		t.Fatalf("read-only validation wrote permanent target claims: %v", dest.targets)
	}
	if len(dest.prepareCalls) != prepareCallsBeforeValidation {
		t.Fatalf("read-only validation prepared %d tables", len(dest.prepareCalls)-prepareCallsBeforeValidation)
	}
}

func TestCDCTargetClaimsRejectCanonicalManagementTableAliases(t *testing.T) {
	for _, target := range []string{"_BRUIN_STAGING.CDC_STATE", "_BRUIN_STAGING.CDC_TARGETS"} {
		t.Run(target, func(t *testing.T) {
			dest := &caseCanonicalCDCStateDestination{cdcStateDestination: newCDCStateDestination()}
			manager, err := NewCDCStateManager(dest, "connector-a", "raw.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			err = manager.ClaimTarget(t.Context(), "public.orders", target)
			if err == nil || !strings.Contains(err.Error(), "already claimed") {
				t.Fatalf("reserved management target %q claim error = %v", target, err)
			}
		})
	}
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
		eventIDs := batch.Column(0).(*array.String)
		connectors := batch.Column(2).(*array.String)
		sourceTables := batch.Column(3).(*array.String)
		destinationTables := batch.Column(4).(*array.String)
		kinds := batch.Column(5).(*array.String)
		generations := batch.Column(6).(*array.Int64)
		statuses := batch.Column(7).(*array.String)
		positions := batch.Column(8).(*array.String)

		d.stateMu.Lock()
		d.maxWrite = max(d.maxWrite, int(batch.NumRows()))
		for i := 0; i < int(batch.NumRows()); i++ {
			d.states[opts.Table] = append(d.states[opts.Table], storedCDCState{
				connectorID: connectors.Value(i),
				entry: destination.CDCStateEntry{
					EventID:          eventIDs.Value(i),
					SourceTable:      sourceTables.Value(i),
					DestinationTable: destinationTables.Value(i),
					StateKind:        kinds.Value(i),
					Generation:       generations.Value(i),
					Status:           statuses.Value(i),
					Position:         positions.Value(i),
				},
			})
		}
		d.stateMu.Unlock()
		batch.Release()
	}
	return nil
}

func (d *cdcStateDestination) DeleteCDCStateEvents(_ context.Context, table, connectorID string, eventIDs []string) error {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.prunes++
	d.pruneSizes = append(d.pruneSizes, len(eventIDs))
	if d.pruneErr != nil {
		return d.pruneErr
	}
	deleted := make(map[string]struct{}, len(eventIDs))
	for _, eventID := range eventIDs {
		deleted[eventID] = struct{}{}
	}
	kept := d.states[table][:0]
	for _, state := range d.states[table] {
		if state.connectorID == connectorID {
			if _, ok := deleted[state.entry.EventID]; ok {
				continue
			}
		}
		kept = append(kept, state)
	}
	d.states[table] = kept
	return nil
}

func TestCDCStateRejectsDestinationRemapping(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	first, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := first.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := first.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}

	remapped, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders_v2", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := remapped.RegisterTable(ctx, "public.orders", "raw.orders_v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := remapped.ResumePosition(ctx, "public.orders"); err == nil {
		t.Fatal("destination remapping reused state from the old target")
	}
}

func TestCDCStateMissingDestinationDoesNotResume(t *testing.T) {
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
	if dest.cdcWrites == 0 {
		t.Fatal("destination-specific CDC state writer was not used")
	}
	dest.missing = map[string]bool{"raw.orders": true}

	restarted, err := NewCDCStateManager(dest, manager.connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := restarted.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("missing destination resumed at %s", position)
	}
}

func TestCDCStateIncompleteForeignGenerationBlocksResumeAndRawBootstrap(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"
	dest.states["_bruin_staging.cdc_state"] = []storedCDCState{{
		connectorID: connectorID,
		entry: destination.CDCStateEntry{
			EventID:          connectorID + "-legacy-event",
			SourceTable:      "public.orders",
			DestinationTable: "raw.orders",
			StateKind:        cdcStateKindSnapshot,
			Generation:       7,
			Status:           cdcStateStatusInProgress,
			Position:         zeroCDCPosition,
		},
	}}

	manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("incomplete legacy generation resumed at %s", position)
	}
	stateEmpty, err := manager.StateEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateEmpty {
		t.Fatal("incomplete generation classified the connector state as empty")
	}
}

func TestCDCStateDroppedTableCannotReuseOlderGeneration(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"

	first, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	for sourceTable, destTable := range map[string]string{"public.orders": "raw.orders", "public.customers": "raw.customers"} {
		if err := first.RegisterTable(ctx, sourceTable, destTable); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := first.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010", "public.customers": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}

	withoutOrders, err := NewCDCStateManager(dest, connectorID, "raw.customers", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := withoutOrders.RegisterTable(ctx, "public.customers", "raw.customers"); err != nil {
		t.Fatal(err)
	}
	if _, err := withoutOrders.ResumePosition(ctx, "public.customers"); err != nil {
		t.Fatal(err)
	}
	if err := withoutOrders.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := withoutOrders.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020"}); err != nil {
		t.Fatal(err)
	}

	recreated, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := recreated.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("recreated table resumed from stale state at %s", position)
	}
}

func TestCDCStateLeaseLossDuringPersistSkipsPruneAndReturnsFenceError(t *testing.T) {
	base := newCDCStateDestination()
	dest := &blockingStateDestination{
		cdcStateDestination: base,
		entered:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(context.Background(), "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	dest.block = true
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost during batch state persist")}
	ctx := guardedStreamingContext(lease)
	prunesBefore := base.prunes
	done := make(chan error, 1)
	go func() {
		done <- manager.Persist(ctx, source.CDCStateCommitToken{
			Position: "00000000/00000010",
			SnapshotPositions: map[string]string{
				"public.orders": "00000000/00000010",
			},
		})
	}()
	<-dest.entered
	close(lease.done)
	close(dest.release)

	err = <-done
	if !errors.Is(err, source.ErrConnectorLeaseLost) {
		t.Fatalf("Persist error = %v, want connector lease loss", err)
	}
	if base.prunes != prunesBefore {
		t.Fatalf("Persist pruned state after lease loss: before=%d after=%d", prunesBefore, base.prunes)
	}
}

func TestCDCStateEmptySourceSetInvalidatesPreviousGeneration(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	dest.catalog = "analytics"
	const connectorID = "0123456789abcdef"

	first, err := NewCDCStateManager(dest, connectorID, "analytics.raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RegisterTable(ctx, "public.orders", "analytics.raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := first.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := first.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}

	empty, err := NewCDCStateManager(dest, connectorID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if empty.stateTable != first.stateTable {
		t.Fatalf("empty source state table = %q, want %q", empty.stateTable, first.stateTable)
	}

	recreated, err := NewCDCStateManager(dest, connectorID, "analytics.raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.RegisterTable(ctx, "public.orders", "analytics.raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := recreated.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("recreated table resumed after an empty source generation at %s", position)
	}
}

func TestCDCStateLocationSurvivesMultiSchemaAddRemoveEmptyAndReappear(t *testing.T) {
	tests := []struct {
		name          string
		defaultSchema string
	}{
		{name: "mysql", defaultSchema: "app"},
		{name: "duckdb", defaultSchema: "main"},
		{name: "oracle", defaultSchema: "INGESTR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dest := newCDCStateDestination()
			dest.policy = destination.ReplaceStagingPolicy{
				DefaultPlacement:    destination.ReplaceStagingTargetSchema,
				DefaultTargetSchema: tt.defaultSchema,
			}
			const connectorID = "0123456789abcdef"
			wantStateTable := tt.defaultSchema + ".cdc_state"

			first, err := NewCDCStateManager(dest, connectorID, "archive.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := first.RegisterTable(ctx, "public.orders", "archive.orders"); err != nil {
				t.Fatal(err)
			}
			if err := first.BeginRun(ctx, false); err != nil {
				t.Fatal(err)
			}
			if err := first.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{
				"public.orders": "00000000/00000010",
			}}); err != nil {
				t.Fatal(err)
			}

			withCustomers, err := NewCDCStateManager(dest, connectorID, "archive.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := withCustomers.RegisterTable(ctx, "public.orders", "archive.orders"); err != nil {
				t.Fatal(err)
			}
			if err := withCustomers.RegisterTable(ctx, "sales.customers", "reporting.customers"); err != nil {
				t.Fatal(err)
			}
			if err := withCustomers.BeginRun(ctx, false); err != nil {
				t.Fatal(err)
			}
			if err := withCustomers.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{
				"public.orders": "00000000/00000020", "sales.customers": "00000000/00000020",
			}}); err != nil {
				t.Fatal(err)
			}

			withoutOrders, err := NewCDCStateManager(dest, connectorID, "reporting.customers", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := withoutOrders.RegisterTable(ctx, "sales.customers", "reporting.customers"); err != nil {
				t.Fatal(err)
			}
			if got, err := withoutOrders.ResumePosition(ctx, "sales.customers"); err != nil || got == "" {
				t.Fatalf("remaining table resume = %q, err = %v", got, err)
			}
			if err := withoutOrders.BeginRun(ctx, false); err != nil {
				t.Fatal(err)
			}

			empty, err := NewCDCStateManager(dest, connectorID, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := empty.BeginRun(ctx, false); err != nil {
				t.Fatal(err)
			}

			reappeared, err := NewCDCStateManager(dest, connectorID, "new_schema.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			for _, manager := range []*CDCStateManager{first, withCustomers, withoutOrders, empty, reappeared} {
				if manager.stateTable != wantStateTable {
					t.Fatalf("state table = %q, want %q", manager.stateTable, wantStateTable)
				}
			}
			if err := reappeared.RegisterTable(ctx, "public.orders", "new_schema.orders"); err != nil {
				t.Fatal(err)
			}
			if got, err := reappeared.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
				t.Fatalf("reappeared table resumed at %q, err = %v", got, err)
			}
		})
	}
}

func TestCDCStateIncompleteRunMarksStateNonEmpty(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	stateEmpty, err := manager.StateEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !stateEmpty {
		t.Fatal("fresh connector state must be empty")
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewCDCStateManager(dest, manager.connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	stateEmpty, err = restarted.StateEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateEmpty {
		t.Fatal("incomplete run classified the connector state as empty")
	}
}

func TestCDCStateConcurrentRunsCannotCertifySibling(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	managers := make([]*CDCStateManager, 2)
	for i := range managers {
		manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		// Force both managers to observe the same previous generation before
		// either claims its new run.
		if _, err := manager.ResumePosition(ctx, "public.orders"); err != nil {
			t.Fatal(err)
		}
		managers[i] = manager
	}
	for _, manager := range managers {
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	if managers[0].runID == managers[1].runID {
		t.Fatal("concurrent managers generated the same run ID")
	}
	loadsBeforePersist := dest.stateLoads
	if err := managers[0].Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err == nil {
		t.Fatal("one concurrent run certified a generation containing an in-progress sibling")
	}
	if dest.stateLoads != loadsBeforePersist || dest.fenceLoads != 2 {
		t.Fatalf("supersession check full loads=%d fence loads=%d", dest.stateLoads-loadsBeforePersist, dest.fenceLoads)
	}

	restarted, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := restarted.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("conflicting generation became resumable at %s", position)
	}
}

func TestCDCStateLateFenceLoserInvalidatesNewerCompletedRun(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"

	late, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := late.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := late.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}

	newer, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := newer.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := newer.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000020",
	}}); err != nil {
		t.Fatal(err)
	}
	prunesBeforeLoss := dest.prunes

	err = late.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}})
	if err == nil || !strings.Contains(err.Error(), "invalidated destination CDC state at generation 3") {
		t.Fatalf("late Persist error = %v", err)
	}
	if dest.prunes != prunesBeforeLoss {
		t.Fatalf("fence-loss invalidation pruned evidence: before=%d after=%d", prunesBeforeLoss, dest.prunes)
	}

	restarted, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
		t.Fatalf("newer completed generation survived late target mutation: position=%q err=%v", got, err)
	}
}

func TestCDCStateConcurrentFenceLosersInvalidateMonotonically(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"

	losers := make([]*CDCStateManager, 2)
	for i := range losers {
		manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.ResumePosition(ctx, "public.orders"); err != nil {
			t.Fatal(err)
		}
		losers[i] = manager
	}
	for _, manager := range losers {
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
	}

	winner, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := winner.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := winner.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := winner.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000020",
	}}); err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, len(losers))
	var wg sync.WaitGroup
	for _, manager := range losers {
		wg.Add(1)
		go func(manager *CDCStateManager) {
			defer wg.Done()
			errs <- manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010"})
		}(manager)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil || !strings.Contains(err.Error(), "invalidated destination CDC state") {
			t.Fatalf("concurrent loser error = %v", err)
		}
	}
	fence, err := dest.LoadCDCStateFence(ctx, "_bruin_staging.cdc_state", connectorID)
	if err != nil {
		t.Fatal(err)
	}
	if fence.Generation < 3 {
		t.Fatalf("latest invalidation generation = %d, want at least 3", fence.Generation)
	}

	restarted, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || got != "" {
		t.Fatalf("concurrent invalidators left resumable state: position=%q err=%v", got, err)
	}
}

func TestCDCStateFenceTreatsDuplicateEventIDAsOneSentinel(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"
	manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}

	dest.stateMu.Lock()
	for _, state := range dest.states[manager.stateTable] {
		if state.connectorID == connectorID && state.entry.EventID == manager.runEventID {
			dest.states[manager.stateTable] = append(dest.states[manager.stateTable], state)
			break
		}
	}
	dest.stateMu.Unlock()
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatalf("duplicate copy of the same run sentinel caused false conflict: %v", err)
	}
}

func TestCDCStateFenceLossReportsInvalidationFailure(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"
	late, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := late.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := late.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	newer, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := newer.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	dest.failWrite = dest.cdcWrites + 1
	err = late.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010"})
	if err == nil || !strings.Contains(err.Error(), "lost exclusive ownership") || !strings.Contains(err.Error(), "failed to persist CDC recovery invalidation") {
		t.Fatalf("combined invalidation error = %v", err)
	}
}

func (d *cdcStateDestination) LoadCDCState(_ context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.stateLoads++
	var entries []destination.CDCStateEntry
	for _, state := range d.states[table] {
		if state.connectorID == connectorID {
			entries = append(entries, state.entry)
		}
	}
	return entries, nil
}

func (d *cdcStateDestination) LoadCDCStateFence(_ context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.fenceLoads++
	var fence destination.CDCStateFence
	for _, state := range d.states[table] {
		if state.connectorID != connectorID || state.entry.StateKind != cdcStateKindRun {
			continue
		}
		if state.entry.Generation > fence.Generation {
			fence.Generation = state.entry.Generation
			fence.RunEventIDs = fence.RunEventIDs[:0]
		}
		if state.entry.Generation == fence.Generation {
			fence.RunEventIDs = append(fence.RunEventIDs, state.entry.EventID)
		}
	}
	sort.Strings(fence.RunEventIDs)
	d.maxFence = max(d.maxFence, len(fence.RunEventIDs))
	return fence, nil
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
	if err := manager.ClaimTarget(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 1 || got[0] != "destination_table" {
		t.Fatalf("shared target primary keys = %v, want [destination_table]", got)
	}
	if got := dest.prepareCalls[0].Schema.Columns[0].MaxLength; got != 2048 {
		t.Fatalf("shared target key width = %d, want 2048", got)
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
	if got := dest.prepareCalls[1].PrimaryKeys; len(got) != 2 || got[0] != "connector_id" || got[1] != "event_id" {
		t.Fatalf("shared state primary keys = %v, want [connector_id event_id]", got)
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
	b, err := NewCDCStateManager(dest, "bbbbbbbbbbbbbbbb", "raw.orders_b", "")
	if err != nil {
		t.Fatal(err)
	}
	for i, manager := range []*CDCStateManager{a, b} {
		suffix := ""
		if i == 1 {
			suffix = "_b"
		}
		if err := manager.RegisterTable(ctx, "db1.orders", "raw.orders"+suffix); err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "db2.orders", "raw.orders_archive"+suffix); err != nil {
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

func TestCDCStatePrunesLongRunningCheckpointHistory(t *testing.T) {
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
	for i := 1; i <= 1000; i++ {
		token := source.CDCStateCommitToken{Position: fmt.Sprintf("00000000/%08X", i)}
		if i == 1 {
			token.SnapshotPositions = map[string]string{"public.orders": token.Position}
		}
		if err := manager.Persist(ctx, token); err != nil {
			t.Fatal(err)
		}
	}

	if got := connectorStateCount(dest, manager.stateTable, manager.connectorID); got > cdcStatePruneThreshold+2 {
		t.Fatalf("connector retained %d state events after 1000 checkpoints", got)
	}
	for _, size := range dest.pruneSizes {
		if size > cdcStatePruneBatchSize {
			t.Fatalf("prune batch size = %d, want at most %d", size, cdcStatePruneBatchSize)
		}
	}
	restarted, err := NewCDCStateManager(dest, manager.connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	position, err := restarted.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if want := "00000000/000003E8"; position != want {
		t.Fatalf("resume position = %s, want %s", position, want)
	}
}

func TestCDCStatePruneBatchSizeUsesBoundedDestinationCapability(t *testing.T) {
	tests := []struct {
		name       string
		advertised int
		want       int
	}{
		{name: "default", want: cdcStatePruneBatchSize},
		{name: "destination override", advertised: 10_000, want: 10_000},
		{name: "negative ignored", advertised: -1, want: cdcStatePruneBatchSize},
		{name: "override capped", advertised: cdcStateMaxPruneBatchSize + 1, want: cdcStateMaxPruneBatchSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := newCDCStateDestination()
			dest.pruneBatchSize = tt.advertised
			manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
			if err != nil {
				t.Fatal(err)
			}
			if manager.pruneBatchSize != tt.want {
				t.Fatalf("prune batch size = %d, want %d", manager.pruneBatchSize, tt.want)
			}
		})
	}
}

func TestCDCStateRepeatedManagersDoNotPruneBelowThreshold(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"

	for i := 1; i <= 19; i++ {
		manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		position, err := manager.ResumePosition(ctx, "public.orders")
		if err != nil {
			t.Fatal(err)
		}
		if i > 1 && position == "" {
			t.Fatalf("run %d could not resume prior state", i)
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
		position = fmt.Sprintf("00000000/%08X", i)
		token := source.CDCStateCommitToken{Position: position}
		if i == 1 {
			token.SnapshotPositions = map[string]string{"public.orders": position}
		}
		if err := manager.Persist(ctx, token); err != nil {
			t.Fatal(err)
		}
	}

	if dest.prunes != 0 {
		t.Fatalf("short restarted runs triggered %d cleanup mutations below threshold", dest.prunes)
	}
	if got := connectorStateCount(dest, "_bruin_staging.cdc_state", connectorID); got >= cdcStatePruneThreshold {
		t.Fatalf("test accumulated %d events, expected fewer than threshold", got)
	}
}

func TestCDCStateRepeatedAbandonedRunsStayBounded(t *testing.T) {
	tests := []struct {
		name          string
		registerTable bool
		currentEvents int
	}{
		{name: "empty source", currentEvents: 1},
		{name: "registered table", registerTable: true, currentEvents: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dest := newCDCStateDestination()
			const connectorID = "0123456789abcdef"

			for i := 0; i < cdcStatePruneThreshold*10; i++ {
				manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
				if err != nil {
					t.Fatal(err)
				}
				if tt.registerTable {
					if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
						t.Fatal(err)
					}
				}
				if err := manager.BeginRun(ctx, false); err != nil {
					t.Fatal(err)
				}
			}

			if got := connectorStateCount(dest, "_bruin_staging.cdc_state", connectorID); got > cdcStatePruneThreshold+tt.currentEvents {
				t.Fatalf("abandoned runs retained %d state events, want at most %d", got, cdcStatePruneThreshold+tt.currentEvents)
			}
			for _, size := range dest.pruneSizes {
				if size > cdcStatePruneBatchSize {
					t.Fatalf("prune batch size = %d, want at most %d", size, cdcStatePruneBatchSize)
				}
			}
		})
	}
}

func TestCDCStateLargeMultiTableRunsStayBounded(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const (
		connectorID = "0123456789abcdef"
		tableCount  = cdcStatePruneThreshold * 10
		runCount    = 4
	)
	positions := make(map[string]string, tableCount)
	for i := 0; i < tableCount; i++ {
		positions[fmt.Sprintf("public.table_%04d", i)] = "00000000/00000001"
	}

	for run := 1; run <= runCount; run++ {
		manager, err := NewCDCStateManager(dest, connectorID, "raw.table_0000", "")
		if err != nil {
			t.Fatal(err)
		}
		for sourceTable := range positions {
			destTable := "raw." + sourceTable[len("public."):]
			if err := manager.RegisterTable(ctx, sourceTable, destTable); err != nil {
				t.Fatal(err)
			}
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
		position := fmt.Sprintf("00000000/%08X", run)
		for table := range positions {
			positions[table] = position
		}
		if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: position, SnapshotPositions: positions}); err != nil {
			t.Fatal(err)
		}
		if got := connectorStateCount(dest, manager.stateTable, connectorID); got > tableCount*2+2 {
			t.Fatalf("run %d retained %d state events, want at most %d", run, got, tableCount*2+2)
		}
	}

	assertCDCStatePruneBatchesBounded(t, dest)
}

func TestCDCStateTenThousandTablesUseBatchedWritesAndCompactFences(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const (
		connectorID = "0123456789abcdef"
		tableCount  = 10_000
	)
	manager, err := NewCDCStateManager(dest, connectorID, "raw.table_00000", "")
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]string, tableCount)
	for i := 0; i < tableCount; i++ {
		sourceTable := fmt.Sprintf("public.table_%05d", i)
		if err := manager.RegisterTable(ctx, sourceTable, fmt.Sprintf("raw.table_%05d", i)); err != nil {
			t.Fatal(err)
		}
		positions[sourceTable] = "00000000/00000010"
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	beginWrites := 1 + (tableCount+cdcStateWriteBatchSize-1)/cdcStateWriteBatchSize
	if dest.cdcWrites != beginWrites {
		t.Fatalf("BeginRun writes = %d, want %d", dest.cdcWrites, beginWrites)
	}
	loadsAfterBegin := dest.stateLoads
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: positions}); err != nil {
		t.Fatal(err)
	}
	persistWrites := (tableCount*2 + 1 + cdcStateWriteBatchSize - 1) / cdcStateWriteBatchSize
	if dest.cdcWrites != beginWrites+persistWrites {
		t.Fatalf("writes after Persist = %d, want %d", dest.cdcWrites, beginWrites+persistWrites)
	}
	if dest.maxWrite != cdcStateWriteBatchSize {
		t.Fatalf("largest state write batch = %d, want %d", dest.maxWrite, cdcStateWriteBatchSize)
	}
	if dest.stateLoads != loadsAfterBegin {
		t.Fatalf("Persist performed %d full state reloads", dest.stateLoads-loadsAfterBegin)
	}
	if dest.fenceLoads != 1 || dest.maxFence != 1 {
		t.Fatalf("compact fence calls=%d max rows=%d, want calls=1 rows=1", dest.fenceLoads, dest.maxFence)
	}
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020"}); err != nil {
		t.Fatal(err)
	}
	if dest.prunes > 48 {
		t.Fatalf("10,000-table run used %d prune calls, want at most 48", dest.prunes)
	}
	assertCDCStatePruneBatchesBounded(t, dest)
	if dest.stateLoads != loadsAfterBegin || dest.fenceLoads != 2 {
		t.Fatalf("second Persist full loads=%d fence loads=%d", dest.stateLoads-loadsAfterBegin, dest.fenceLoads)
	}
}

func TestCDCStatePartialBatchedPersistOnlyAppliesConfirmedBatches(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const (
		connectorID = "0123456789abcdef"
		tableCount  = 10_000
	)
	manager, err := NewCDCStateManager(dest, connectorID, "raw.table_00000", "")
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]string, tableCount)
	for i := 0; i < tableCount; i++ {
		sourceTable := fmt.Sprintf("public.table_%05d", i)
		if err := manager.RegisterTable(ctx, sourceTable, fmt.Sprintf("raw.table_%05d", i)); err != nil {
			t.Fatal(err)
		}
		positions[sourceTable] = "00000000/00000010"
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	entriesAfterBegin := len(manager.entries)
	dest.failWrite = dest.cdcWrites + 2
	err = manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000010", SnapshotPositions: positions})
	if err == nil || !strings.Contains(err.Error(), "injected CDC state write failure") {
		t.Fatalf("Persist error = %v, want injected batch failure", err)
	}
	if got := len(manager.entries) - entriesAfterBegin; got != cdcStateWriteBatchSize {
		t.Fatalf("in-memory confirmed events = %d, want %d", got, cdcStateWriteBatchSize)
	}
	if got := connectorStateCount(dest, manager.stateTable, connectorID); got != entriesAfterBegin+cdcStateWriteBatchSize {
		t.Fatalf("durable event count = %d, want %d", got, entriesAfterBegin+cdcStateWriteBatchSize)
	}

	restarted, err := NewCDCStateManager(dest, connectorID, "raw.table_00000", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 999} {
		sourceTable := fmt.Sprintf("public.table_%05d", index)
		if err := restarted.RegisterTable(ctx, sourceTable, fmt.Sprintf("raw.table_%05d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := restarted.ResumePosition(ctx, "public.table_00000"); err != nil || got != "00000000/00000010" {
		t.Fatalf("confirmed batch resume = %q, err = %v", got, err)
	}
	if got, err := restarted.ResumePosition(ctx, "public.table_00999"); err != nil || got != "" {
		t.Fatalf("failed batch resume = %q, err = %v", got, err)
	}
}

func TestCDCStateLargeMultiTableAbandonedRunsStayBounded(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const (
		connectorID = "0123456789abcdef"
		tableCount  = cdcStatePruneThreshold * 10
		runCount    = 4
	)

	for run := 1; run <= runCount; run++ {
		manager, err := NewCDCStateManager(dest, connectorID, "raw.table_0000", "")
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < tableCount; i++ {
			if err := manager.RegisterTable(ctx, fmt.Sprintf("public.table_%04d", i), fmt.Sprintf("raw.table_%04d", i)); err != nil {
				t.Fatal(err)
			}
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
		if got := connectorStateCount(dest, manager.stateTable, connectorID); got > tableCount+1 {
			t.Fatalf("abandoned run %d retained %d state events, want at most %d", run, got, tableCount+1)
		}
	}

	assertCDCStatePruneBatchesBounded(t, dest)
}

func TestCDCStateAbandonedRunPruneFailureRetriesAfterRestart(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	dest.pruneErr = errors.New("temporary cleanup failure")
	const connectorID = "0123456789abcdef"

	for i := 0; i < cdcStatePruneThreshold/2+2; i++ {
		manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatalf("durable run markers failed because cleanup failed: %v", err)
		}
	}
	failedPrunes := dest.prunes
	if failedPrunes == 0 {
		t.Fatal("cleanup failure was not exercised")
	}

	dest.pruneErr = nil
	restarted, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if dest.prunes <= failedPrunes {
		t.Fatal("restart did not retry pruning")
	}
	for _, size := range dest.pruneSizes[failedPrunes:] {
		if size > cdcStatePruneBatchSize {
			t.Fatalf("restart prune batch = %d, want at most %d", size, cdcStatePruneBatchSize)
		}
	}
	if got := connectorStateCount(dest, restarted.stateTable, connectorID); got > cdcStatePruneThreshold+2 {
		t.Fatalf("restart retained %d state events after cleanup retry", got)
	}
	if got := connectorRunStateCount(dest, restarted.stateTable, connectorID, restarted.runID); got != 2 {
		t.Fatalf("cleanup retained %d current run markers, want 2", got)
	}
}

func TestCDCStatePruneBacklogRetriesAfterRestart(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	const connectorID = "0123456789abcdef"
	manager, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	dest.pruneErr = errors.New("temporary cleanup failure")
	for i := 1; i <= cdcStatePruneThreshold*2+5; i++ {
		position := fmt.Sprintf("00000000/%08X", i)
		token := source.CDCStateCommitToken{Position: position}
		if i == 1 {
			token.SnapshotPositions = map[string]string{"public.orders": position}
		}
		if err := manager.Persist(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	failedPrunes := dest.prunes
	if failedPrunes == 0 {
		t.Fatal("cleanup failure was not exercised")
	}

	dest.pruneErr = nil
	restarted, err := NewCDCStateManager(dest, connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ResumePosition(ctx, "public.orders"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.BeginRun(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/0000FFFF"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4 && connectorStateCount(dest, restarted.stateTable, connectorID) > cdcStatePruneThreshold+2; i++ {
		if err := restarted.Persist(ctx, source.CDCStateCommitToken{Position: fmt.Sprintf("00000000/%08X", 0x10000+i)}); err != nil {
			t.Fatal(err)
		}
	}
	if dest.prunes < failedPrunes+1 {
		t.Fatalf("restart performed %d successful prune batches, want at least 1", dest.prunes-failedPrunes)
	}
	for _, size := range dest.pruneSizes[failedPrunes:] {
		if size > cdcStatePruneBatchSize {
			t.Fatalf("restart prune batch = %d, want at most %d", size, cdcStatePruneBatchSize)
		}
	}
	if got := connectorStateCount(dest, restarted.stateTable, connectorID); got > cdcStatePruneThreshold+2 {
		t.Fatalf("restart left %d state events after bounded backlog cleanup", got)
	}
}

func TestCDCStatePruneFailurePreservesStateAndRetries(t *testing.T) {
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
	dest.pruneErr = errors.New("temporary cleanup failure")
	for i := 1; i <= cdcStatePruneThreshold+5; i++ {
		token := source.CDCStateCommitToken{Position: fmt.Sprintf("00000000/%08X", i)}
		if i == 1 {
			token.SnapshotPositions = map[string]string{"public.orders": token.Position}
		}
		if err := manager.Persist(ctx, token); err != nil {
			t.Fatalf("durable state failed because cleanup failed: %v", err)
		}
	}
	if dest.prunes == 0 {
		t.Fatal("cleanup failure was not exercised")
	}
	before := connectorStateCount(dest, manager.stateTable, manager.connectorID)
	dest.pruneErr = nil
	latest := "00000000/0000FFFF"
	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: latest}); err != nil {
		t.Fatal(err)
	}
	after := connectorStateCount(dest, manager.stateTable, manager.connectorID)
	if after >= before {
		t.Fatalf("retry did not compact state: before=%d after=%d", before, after)
	}

	restarted, err := NewCDCStateManager(dest, manager.connectorID, "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}
	if position, err := restarted.ResumePosition(ctx, "public.orders"); err != nil || position != latest {
		t.Fatalf("latest durable state was lost: position=%s err=%v", position, err)
	}
}

func TestCDCStatePruningIsConnectorScoped(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	managers := make([]*CDCStateManager, 0, 2)
	for i, connectorID := range []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"} {
		target := "raw.orders"
		if i == 1 {
			target = "raw.orders_b"
		}
		manager, err := NewCDCStateManager(dest, connectorID, target, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "public.orders", target); err != nil {
			t.Fatal(err)
		}
		if err := manager.BeginRun(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000001", SnapshotPositions: map[string]string{"public.orders": "00000000/00000001"}}); err != nil {
			t.Fatal(err)
		}
		managers = append(managers, manager)
	}
	beforeB := connectorStateCount(dest, managers[1].stateTable, managers[1].connectorID)
	for i := 2; i <= cdcStatePruneThreshold+10; i++ {
		if err := managers[0].Persist(ctx, source.CDCStateCommitToken{Position: fmt.Sprintf("00000000/%08X", i)}); err != nil {
			t.Fatal(err)
		}
	}
	afterB := connectorStateCount(dest, managers[1].stateTable, managers[1].connectorID)
	if afterB != beforeB {
		t.Fatalf("connector B state changed during connector A cleanup: before=%d after=%d", beforeB, afterB)
	}
}

func TestCDCStatePositionComparisonIsNumeric(t *testing.T) {
	if got := compareCDCPositions("0/F", "0/10"); got >= 0 {
		t.Fatalf("compareCDCPositions(0/F, 0/10) = %d, want negative", got)
	}
	candidate := destination.CDCStateEntry{Position: "0/F", EventID: "z"}
	current := destination.CDCStateEntry{Position: "0/10", EventID: "a"}
	if preferCDCStateEntry(candidate, current) {
		t.Fatal("lexically larger but numerically older position was preferred")
	}
}

func TestCDCStatePositionSupportsMSSQLFormat(t *testing.T) {
	const (
		snapshotIncomplete = "0000002F0000010D0002:00000000000000000000:00"
		snapshotComplete   = "0000002F0000010D0002:00000000000000000000:01"
		changeRow          = "0000002F0000010D0002:0000002F0000010D0003:04"
		laterChange        = "0000003A0000010D0001:0000003A0000010D0002:02"
	)

	for _, position := range []string{snapshotIncomplete, snapshotComplete, changeRow, laterChange} {
		if !cdcStatePositionValid(position) {
			t.Fatalf("cdcStatePositionValid(%q) = false, want true", position)
		}
	}
	if cdcStatePositionValid("0000002f0000010d0002") {
		t.Fatal("bare LSN without seqval/op must not validate as an mssql position")
	}

	if got := compareCDCPositions(snapshotIncomplete, snapshotComplete); got >= 0 {
		t.Fatalf("incomplete stamp must order below complete stamp, got %d", got)
	}
	if got := compareCDCPositions(snapshotComplete, changeRow); got >= 0 {
		t.Fatalf("snapshot stamp must order below a change row at the same LSN, got %d", got)
	}
	if got := compareCDCPositions(changeRow, laterChange); got >= 0 {
		t.Fatalf("earlier change must order below a later change, got %d", got)
	}
	if got := compareCDCPositions(changeRow, "0/10"); got != 0 {
		t.Fatalf("cross-format comparison must be incomparable (0), got %d", got)
	}
}

func TestCDCStatePruningPreservesUnparseablePositions(t *testing.T) {
	const (
		connectorID = "0123456789abcdef"
		runID       = "0123456789abcdef0123456789abcdef"
	)
	manager := &CDCStateManager{
		connectorID: connectorID,
		generation:  2,
		runID:       runID,
		runs:        map[string]struct{}{runID: {}},
		entries: []destination.CDCStateEntry{
			{EventID: connectorID + "-" + runID + "-current", StateKind: cdcStateKindRun, Generation: 2, Position: zeroCDCPosition},
			{EventID: connectorID + "-legacy-malformed", StateKind: cdcStateKindCheckpoint, Generation: 1, Position: "not-an-lsn"},
		},
	}
	for _, eventID := range manager.supersededEventIDs() {
		if eventID == connectorID+"-legacy-malformed" {
			t.Fatal("unparseable CDC state position was selected for pruning")
		}
	}
}

func TestCDCDestinationStateAcceptsMySQLPosition(t *testing.T) {
	position := "00000000000000000012:mysql-bin.000012:00000000000000000345:00000000000000000000"
	encoded := encodeCDCDestinationState(position, "0123456789abcdefabcd", 7)

	decoded, incarnation, epoch, valid := decodeCDCDestinationState(encoded)
	if !valid {
		t.Fatal("MySQL destination state was rejected")
	}
	if decoded != position || incarnation != "0123456789abcdefabcd" || epoch != 7 {
		t.Fatalf("decoded destination state = (%q, %q, %d), want (%q, %q, 7)", decoded, incarnation, epoch, position, "0123456789abcdefabcd")
	}
}

func TestCDCStatePositionColumnIsUnbounded(t *testing.T) {
	for _, column := range cdcStateSchema.Columns {
		if column.Name == "_cdc_lsn" {
			if column.MaxLength != 0 {
				t.Fatalf("CDC state position MaxLength = %d, want unbounded", column.MaxLength)
			}
			return
		}
	}
	t.Fatal("CDC state schema is missing _cdc_lsn")
}

func connectorStateCount(dest *cdcStateDestination, table, connectorID string) int {
	dest.stateMu.Lock()
	defer dest.stateMu.Unlock()
	count := 0
	for _, state := range dest.states[table] {
		if state.connectorID == connectorID {
			count++
		}
	}
	return count
}

func connectorRunStateCount(dest *cdcStateDestination, table, connectorID, runID string) int {
	dest.stateMu.Lock()
	defer dest.stateMu.Unlock()
	count := 0
	for _, state := range dest.states[table] {
		stateRunID, ok := cdcStateRunID(state.entry.EventID, connectorID)
		if state.connectorID == connectorID && ok && stateRunID == runID {
			count++
		}
	}
	return count
}

func assertCDCStatePruneBatchesBounded(t *testing.T, dest *cdcStateDestination) {
	t.Helper()
	if dest.prunes <= 1 {
		t.Fatalf("large connector triggered %d prune batches, want multiple bounded batches", dest.prunes)
	}
	for _, size := range dest.pruneSizes {
		if size > cdcStatePruneBatchSize {
			t.Fatalf("prune batch size = %d, want at most %d", size, cdcStatePruneBatchSize)
		}
	}
}

type migratingCDCStateDestination struct {
	*cdcStateDestination
	migrations int
}

func (d *migratingCDCStateDestination) EnsureCDCStatePositionColumn(_ context.Context, table string) error {
	d.migrations++
	delete(d.prepareErrByTable, table)
	return nil
}

func TestCDCStatePrepareRetriesThroughPositionMigration(t *testing.T) {
	ctx := context.Background()
	dest := &migratingCDCStateDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "widen-connector", "raw.orders", "")
	require.NoError(t, err)

	dest.prepareErrByTable = map[string]error{
		manager.stateTable: errors.New(`bigquery table has incompatible column "_cdc_lsn": existing max length 64 is bounded, want unbounded`),
	}
	require.NoError(t, manager.RegisterTableState(ctx, "public.orders", "raw.orders", "", ""))
	require.NoError(t, manager.BeginRun(ctx, false))
	require.Equal(t, 1, dest.migrations, "prepare failure must be retried through the position migration exactly once")
}
