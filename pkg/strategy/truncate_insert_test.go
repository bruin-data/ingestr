package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type atomicTruncateInsertDestination struct {
	*fakeDestination
	calls []destination.WriteOptions
	err   error
}

type atomicSchemaEvolvingTruncateDestination struct {
	*atomicTruncateInsertDestination
}

type immediateFailAtomicTruncateDestination struct {
	*fakeDestination
}

type ownedAtomicTruncateDestination struct {
	*atomicTruncateInsertDestination
	preparedToken string
	dropTokens    []string
}

type recreatingAtomicTruncateDestination struct {
	*cdcStateDestination
	calls []destination.WriteOptions
}

func (d *recreatingAtomicTruncateDestination) TruncateInsertRecords(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.calls = append(d.calls, opts)
	d.stateMu.Lock()
	d.incarnations[opts.Table] = "external-v2"
	d.stateMu.Unlock()
	current, _, _ := d.CDCTargetIncarnation(ctx, opts.Table)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	if opts.CDCExpectedIncarnation != current {
		return errors.New("destination incarnation changed before atomic truncate+insert")
	}
	return nil
}

func TestManagedAtomicTruncateInsertRejectsTargetRecreationDuringExtraction(t *testing.T) {
	job, src, _ := minimalJob()
	stateDest := newCDCStateDestination()
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	dest := &recreatingAtomicTruncateDestination{cdcStateDestination: stateDest}
	job.Destination = dest
	manager, err := NewCDCStateManager(dest, "truncate-insert-recreation", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.CDCStateManager = manager
	src.readCh = singleBatchRecords(t, 1)

	err = (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed")
	require.Len(t, dest.calls, 1)
	require.Equal(t, "target-v1", dest.calls[0].CDCExpectedIncarnation)
	require.True(t, dest.calls[0].SkipCDCResume)
}

func (d *ownedAtomicTruncateDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Table != "" && opts.OwnershipToken != "" {
		d.preparedToken = opts.OwnershipToken
	}
	return d.atomicTruncateInsertDestination.PrepareTable(ctx, opts)
}

func (d *ownedAtomicTruncateDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	return errors.New("ownership changed")
}

type ownedTruncateDestination struct {
	*truncateCapableDestination
	preparedToken string
	dropTokens    []string
}

type uncertainStagingTruncateDestination struct {
	*uncertainManagedStagingPrepareDestination
}

func (d *uncertainStagingTruncateDestination) TruncateTable(context.Context, string) error {
	return nil
}

func (d *ownedTruncateDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.OwnershipToken != "" {
		d.preparedToken = opts.OwnershipToken
	}
	return d.truncateCapableDestination.PrepareTable(ctx, opts)
}

func TestTruncateInsertLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	job, _, base := minimalJob()
	job.Config.PrimaryKeys = []string{"id"}
	dest := &uncertainStagingTruncateDestination{uncertainManagedStagingPrepareDestination: &uncertainManagedStagingPrepareDestination{
		contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base},
	}}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(ctx, job), "prepare response lost")
	require.Len(t, base.prepareCalls, 2)
	require.ElementsMatch(t, []string{base.prepareCalls[0].Table, base.prepareCalls[1].Table}, dest.successfulDrops)
}

func (d *ownedTruncateDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	return errors.New("ownership changed")
}

func (d *immediateFailAtomicTruncateDestination) TruncateInsertRecords(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
) error {
	return errors.New("atomic writer failed")
}

func (d *atomicSchemaEvolvingTruncateDestination) EvolvesTruncateInsertSchemaAtomically() bool {
	return true
}

func (d *atomicTruncateInsertDestination) TruncateInsertRecords(
	_ context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	d.calls = append(d.calls, opts)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return d.err
}

func TestTruncateInsertStrategy_PrefersAtomicWriter(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.readCh = singleBatchRecords(t, 1, 1, 2)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	if len(atomic.calls) != 1 {
		t.Fatalf("expected one atomic write, got %d", len(atomic.calls))
	}
	if !atomic.calls[0].DeduplicatePrimaryKeys {
		t.Fatal("atomic truncate+insert did not request primary-key deduplication")
	}
	require.Len(t, base.prepareCalls, 1)
	require.True(t, base.prepareCalls[0].PreserveExistingLayout)
	if len(base.truncateCalls) != 0 || len(base.mergeCalls) != 0 {
		t.Fatalf("atomic path must not truncate or merge separately: truncate=%v merge=%v", base.truncateCalls, base.mergeCalls)
	}
}

func TestTruncateInsertStrategy_AtomicWriterForwardsCDCSlotSuffixes(t *testing.T) {
	job, src, base := minimalJob()
	job.Destination = &atomicTruncateInsertDestination{fakeDestination: base}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Equal(t, job.Config.CDCSlotSuffix, src.readOpts.CDCSlotSuffix)
	require.Equal(t, job.Config.CDCLegacySlotSuffix, src.readOpts.CDCLegacySlotSuffix)
}

// Atomic writers without AtomicTruncateInsertSchemaEvolver still rely on the
// strategy to apply the pending evolution plan before writing.
func TestTruncateInsertStrategy_AtomicWriterAppliesEvolution(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: job.Schema}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable,
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "new_column",
			}},
		},
	}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, base.execCalls, 1)
	require.Contains(t, base.execCalls[0].sql, "ADD COLUMN new_column")
	require.Nil(t, job.EvolutionPlan.Comparison, "the evolution plan must be applied before the atomic write")
}

func TestTruncateInsertStrategy_AtomicSchemaEvolverOwnsEvolution(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicSchemaEvolvingTruncateDestination{
		atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base},
	}
	job.Destination = atomic
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: job.Schema}
	comparison := &schemaevolution.SchemaComparison{
		HasChanges: true,
		Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeAddColumn, ColumnName: "new_column",
		}},
	}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{Table: job.Config.DestTable, Comparison: comparison}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Empty(t, base.execCalls)
	require.Same(t, comparison, job.EvolutionPlan.Comparison)
	require.Len(t, atomic.calls, 1)
}

func TestTruncateInsertStrategy_AtomicSchemaEvolverSoftensRemovedRequiredColumn(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicSchemaEvolvingTruncateDestination{
		atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base},
	}
	job.Destination = atomic
	destinationSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "legacy", DataType: schema.TypeString, Nullable: false},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeRemoveColumn, ColumnName: "legacy",
		OldColumn: &destinationSchema.Columns[1],
	}}}
	job.Schema = schemaevolution.BuildFinalSchema(destinationSchema, comparison)
	job.SourceSchema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable, Comparison: comparison, FinalSchema: job.Schema,
	}
	base.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: destinationSchema}
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, atomic.calls, 1)
	require.Len(t, atomic.calls[0].Schema.Columns, 2)
	require.Equal(t, "legacy", atomic.calls[0].Schema.Columns[1].Name)
	require.True(t, atomic.calls[0].Schema.Columns[1].Nullable)
}

func TestTruncateInsertStrategy_AtomicWriterSkipsMissingOrderingKey(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{fakeDestination: base}
	job.Destination = atomic
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.IncrementalKey = "missing_ordering_column"
	src.readCh = singleBatchRecords(t, 1, 1, 2)

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.Len(t, atomic.calls, 1)
	require.Empty(t, atomic.calls[0].IncrementalKey)
}

func TestTruncateInsertStrategy_AtomicFailurePreservesNewTargetForOutcomeReconciliation(t *testing.T) {
	job, src, base := minimalJob()
	atomic := &atomicTruncateInsertDestination{
		fakeDestination: base,
		err:             errors.New("atomic commit failed"),
	}
	job.Destination = atomic
	src.readCh = mustClosedRecords()

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected atomic truncate+insert to fail")
	}
	require.Empty(t, base.dropCalls, "atomic write errors may be lost success responses and must preserve the target")
}

func TestTruncateInsertStrategy_AtomicFailureDoesNotAttemptCleanupAfterPossibleCommit(t *testing.T) {
	job, src, base := minimalJob()
	dest := &ownedAtomicTruncateDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{
		fakeDestination: base,
		err:             errors.New("atomic commit failed after replacement"),
	}}
	job.Destination = dest
	src.readCh = mustClosedRecords()

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "conditional ownership is insufficient after a possibly successful commit")
	require.Empty(t, base.dropCalls, "unconditional cleanup must not delete a replacement owner")
}

func TestTruncateInsertStrategy_LostPrepareResponseCleansOwnedTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		make   func(*fakeDestination) destination.Destination
		assert func(*testing.T, destination.Destination)
	}{
		{
			name: "atomic",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedAtomicTruncateDestination{atomicTruncateInsertDestination: &atomicTruncateInsertDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedAtomicTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
		{
			name: "direct",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
		{
			name: "staged",
			make: func(base *fakeDestination) destination.Destination {
				return &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
			},
			assert: func(t *testing.T, raw destination.Destination) {
				dest := raw.(*ownedTruncateDestination)
				require.NotEmpty(t, dest.preparedToken)
				require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job, src, base := minimalJob()
			base.prepareHook = func(destination.PrepareOptions) error { return errors.New("prepare response lost after create") }
			if tc.name == "direct" {
				job.Config.PrimaryKeys = nil
				job.Schema.PrimaryKeys = nil
				src.primaryKeys = nil
			}
			raw := tc.make(base)
			job.Destination = raw

			require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job), "prepare response lost")
			tc.assert(t, raw)
			require.Empty(t, base.dropCalls)
		})
	}
}

func TestTruncateInsertStrategy_AtomicWriterFailureBoundsNonclosingSource(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })
	job, src, base := minimalJob()
	records := make(chan source.RecordBatchResult)
	src.readCh = records
	job.Destination = &immediateFailAtomicTruncateDestination{fakeDestination: base}

	started := time.Now()
	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("truncate+insert did not return promptly after writer failure: %v", err)
	}
	close(records)
}

func TestTruncateInsertStrategy_Execute_PassesFullRefreshToRead(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.FullRefresh = true
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.readCh = mustClosedRecords()

	strat := &TruncateInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	src.mu.Lock()
	defer src.mu.Unlock()
	if !src.readOpts.FullRefresh {
		t.Fatalf("ReadOptions.FullRefresh = false, want true")
	}
	if src.readOpts.CDCSlotSuffix != job.Config.CDCSlotSuffix {
		t.Fatalf("ReadOptions.CDCSlotSuffix = %q, want leased slot suffix %q", src.readOpts.CDCSlotSuffix, job.Config.CDCSlotSuffix)
	}
	if src.readOpts.CDCLegacySlotSuffix != job.Config.CDCLegacySlotSuffix {
		t.Fatalf("full-refresh legacy slot suffix = %q, want %q",
			src.readOpts.CDCLegacySlotSuffix, job.Config.CDCLegacySlotSuffix)
	}
	if src.readOpts.CDCResumeLSN != "" {
		t.Fatalf("full refresh unexpectedly supplied resume LSN %q, which could select a previous or legacy slot", src.readOpts.CDCResumeLSN)
	}
}

func TestTruncateInsertStrategy_ExecuteWithStaging_FullRefreshUsesLeasedSlotSuffix(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.FullRefresh = true
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.True(t, src.readOpts.FullRefresh)
	require.Equal(t, job.Config.CDCSlotSuffix, src.readOpts.CDCSlotSuffix)
	require.Equal(t, job.Config.CDCLegacySlotSuffix, src.readOpts.CDCLegacySlotSuffix)
	require.Empty(t, src.readOpts.CDCResumeLSN, "full refresh must not select a previous or legacy slot")
}

func TestTruncateInsertStrategy_Execute_SkipsOrderingKeyMissingFromStagingSchema(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.IncrementalKey = "updated_at"
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{Name: "_cdc_unchanged_cols", DataType: schema.TypeString, Nullable: true})
	src.readCh = mustClosedRecords()

	strat := &TruncateInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected 1 MergeTable call, got %d", len(dest.mergeCalls))
	}
	if dest.mergeCalls[0].IncrementalKey != "" {
		t.Fatalf("MergeOptions.IncrementalKey = %q, want empty for missing staging column", dest.mergeCalls[0].IncrementalKey)
	}
	require.NotContains(t, dest.mergeCalls[0].Columns, "_cdc_unchanged_cols")
}

func TestTruncateInsertStrategy_Execute_ReadFailsBeforeTruncateWithPrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.primaryKeysUnique = true
	src.readErr = errors.New("read failed")

	strat := &TruncateInsertStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get records") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("expected no truncate before source read succeeds, got %v", dest.truncateCalls)
	}
	if len(dest.writeCalls) != 0 {
		t.Fatalf("expected no write when source read setup fails, got %d", len(dest.writeCalls))
	}
	require.ElementsMatch(t, []string{job.Config.DestTable, dest.prepareCalls[1].Table}, dest.dropCalls)
}

func TestTruncateInsertStrategy_DirectReadSetupFailsBeforeTruncate(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readErr = errors.New("read setup failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected read setup failure")
	}
	if len(dest.truncateCalls) != 0 {
		t.Fatalf("target was truncated before source setup succeeded: %v", dest.truncateCalls)
	}
	require.Equal(t, []string{job.Config.DestTable}, dest.dropCalls)
}

func TestTruncateInsertStrategy_StagingFailureDropsManagedTable(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("write failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected write failure")
	}
	staging := dest.prepareCalls[1].Table
	require.ElementsMatch(t, []string{job.Config.DestTable, staging}, dest.dropCalls)
}

func TestTruncateInsertStrategy_DirectTruncateFailurePreservesNewTarget(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()
	dest.truncateErr = errors.New("truncate failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	require.Empty(t, dest.dropCalls, "truncate errors may be lost responses after a durable truncate")
}

func TestTruncateInsertStrategy_DirectFailurePreservesReplacementOwner(t *testing.T) {
	job, src, base := minimalJob()
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()
	base.writeErr = errors.New("write failed after replacement")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "write errors may follow durable truncate/insert work")
	require.Empty(t, base.dropCalls, "unconditional cleanup must not delete a replacement owner")
}

func TestTruncateInsertStrategy_StagedMergeFailurePreservesTargetAndDropsStaging(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords()
	dest.mergeErr = errors.New("merge failed")

	err := (&TruncateInsertStrategy{}).Execute(context.Background(), job)
	require.Error(t, err)
	require.NotContains(t, dest.dropCalls, job.Config.DestTable)
	require.Equal(t, []string{dest.prepareCalls[1].Table}, dest.dropCalls)
}

func TestTruncateInsertStrategy_StagedTruncateLostResponsePreservesTargetAndDropsStaging(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	base.truncateErr = errors.New("truncate response lost after commit")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.ErrorContains(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job), "truncate response lost")
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens)
	require.NotContains(t, base.dropCalls, job.Config.DestTable)
	require.Equal(t, []string{base.prepareCalls[1].Table}, base.dropCalls)
}

func TestTruncateInsertStrategy_StagedFailurePreservesReplacementOwner(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	base.mergeErr = errors.New("merge failed after replacement")
	dest := &ownedTruncateDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: base}}
	job.Destination = dest

	require.Error(t, (&TruncateInsertStrategy{}).Execute(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "merge errors may follow a durable target mutation")
	require.NotContains(t, base.dropCalls, job.Config.DestTable)
	require.Contains(t, base.dropCalls, base.prepareCalls[1].Table, "owned staging table should still be reclaimed")
}
