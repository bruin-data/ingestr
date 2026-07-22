package strategy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

type atomicTruncateInsertDestination struct {
	*truncateCapableDestination
	finalizeCalls []destination.TruncateInsertFromStagingOptions
	finalizeErr   error
}

type stagingInsertDestination struct {
	*truncateCapableDestination
	insertCalls []destination.InsertFromStagingOptions
	insertErr   error
}

func (d *stagingInsertDestination) InsertFromStaging(_ context.Context, opts destination.InsertFromStagingOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "InsertFromStaging")
	d.insertCalls = append(d.insertCalls, opts)
	d.mu.Unlock()
	return d.insertErr
}

func (d *atomicTruncateInsertDestination) TruncateInsertFromStaging(_ context.Context, opts destination.TruncateInsertFromStagingOptions) error {
	d.finalizeCalls = append(d.finalizeCalls, opts)
	return d.finalizeErr
}

func (d *atomicTruncateInsertDestination) SupportsMergeStrategy() bool {
	return false
}

func TestTruncateInsertStrategy_Execute_PassesFullRefreshToRead(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &stagingInsertDestination{truncateCapableDestination: &truncateCapableDestination{fakeDestination: dest}}
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
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.NotEqual(t, job.Config.DestTable, dest.writeCalls[0].Table)
	require.Equal(t, []string{job.Config.DestTable}, dest.truncateCalls)
}

func TestTruncateInsertStrategy_Execute_KeylessFinalizesFromStaging(t *testing.T) {
	job, src, dest := minimalJob()
	truncateDest := &truncateCapableDestination{fakeDestination: dest}
	stagingDest := &stagingInsertDestination{truncateCapableDestination: truncateDest}
	job.Destination = stagingDest
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.Len(t, stagingDest.insertCalls, 1)
	require.Equal(t, dest.writeCalls[0].Table, stagingDest.insertCalls[0].StagingTable)
	require.Equal(t, job.Config.DestTable, stagingDest.insertCalls[0].TargetTable)
	require.Empty(t, dest.mergeCalls)
	require.Equal(t, []string{
		"PrepareTable",
		"PrepareTable",
		"WriteParallel",
		"TruncateTable",
		"InsertFromStaging",
		"DropTable",
	}, dest.calls)
}

func TestTruncateInsertStrategy_Execute_KeylessRequiresStagingInsert(t *testing.T) {
	job, _, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil

	err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "does not support keyless truncate+insert from staging")
	require.Empty(t, dest.prepareCalls)
	require.Empty(t, dest.writeCalls)
	require.Empty(t, dest.truncateCalls)
}

func TestTruncateInsertStrategy_Execute_KeylessWriteFailureLeavesTargetUntouched(t *testing.T) {
	job, src, dest := minimalJob()
	truncateDest := &truncateCapableDestination{fakeDestination: dest}
	stagingDest := &stagingInsertDestination{truncateCapableDestination: truncateDest}
	job.Destination = stagingDest
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	dest.writeErr = errors.New("write failed")
	src.readCh = mustClosedRecords()

	err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "failed to write to staging")
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.Empty(t, dest.truncateCalls)
	require.Empty(t, stagingDest.insertCalls)
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
}

func TestTruncateInsertStrategy_Execute_SkipsDedupForUniqueSourcePrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].StagingPrimaryKeysUnique)
}

func TestTruncateInsertStrategy_Execute_DedupsUncertainSourcePrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.mergeCalls, 1)
	require.False(t, dest.mergeCalls[0].StagingPrimaryKeysUnique)
}

func TestTruncateInsertStrategy_Execute_PrefersAtomicStagingFinalizer(t *testing.T) {
	for _, primaryKeysUnique := range []bool{false, true} {
		t.Run(fmt.Sprintf("unique=%t", primaryKeysUnique), func(t *testing.T) {
			job, src, dest := minimalJob()
			truncateDest := &truncateCapableDestination{fakeDestination: dest}
			atomicDest := &atomicTruncateInsertDestination{truncateCapableDestination: truncateDest}
			job.Destination = atomicDest
			job.Config.IncrementalStrategy = config.StrategyTruncateInsert
			src.primaryKeysUnique = primaryKeysUnique
			src.readCh = mustClosedRecords()

			require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
			require.Len(t, atomicDest.finalizeCalls, 1)
			require.Equal(t, primaryKeysUnique, atomicDest.finalizeCalls[0].StagingPrimaryKeysUnique)
			require.Empty(t, dest.truncateCalls)
			require.Empty(t, dest.mergeCalls)
		})
	}
}

func TestTruncateInsertStrategy_Execute_PartitionedExtractWithoutKeysUsesAtomicStaging(t *testing.T) {
	job, src, dest := minimalJob()
	truncateDest := &truncateCapableDestination{fakeDestination: dest}
	atomicDest := &atomicTruncateInsertDestination{truncateCapableDestination: truncateDest}
	job.Destination = atomicDest
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	job.SourceSchema.PrimaryKeys = nil
	job.Config.ExtractPartitionBy = "created_at"
	job.Config.ExtractPartitionInterval = 24 * time.Hour
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.prepareCalls, 2)
	require.Equal(t, job.Config.DestTable, dest.prepareCalls[0].Table)
	require.True(t, dest.prepareCalls[1].DropFirst)
	require.True(t, dest.writeCalls[0].StagingTable)
	require.Len(t, atomicDest.finalizeCalls, 1)
	require.Empty(t, atomicDest.finalizeCalls[0].PrimaryKeys)
	require.Empty(t, dest.truncateCalls)
	require.Equal(t, job.Config.ExtractPartitionBy, src.readOpts.ExtractPartitionBy)
	require.Equal(t, job.Config.ExtractPartitionInterval, src.readOpts.ExtractPartitionInterval)
}

func TestTruncateInsertStrategy_Execute_PartitionedExtractFinalizeFailureDoesNotTruncateDirectly(t *testing.T) {
	job, src, dest := minimalJob()
	truncateDest := &truncateCapableDestination{fakeDestination: dest}
	atomicDest := &atomicTruncateInsertDestination{
		truncateCapableDestination: truncateDest,
		finalizeErr:                errors.New("finalize failed"),
	}
	job.Destination = atomicDest
	job.Config.IncrementalStrategy = config.StrategyTruncateInsert
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	job.SourceSchema.PrimaryKeys = nil
	job.Config.ExtractPartitionBy = "created_at"
	job.Config.ExtractPartitionInterval = 24 * time.Hour
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()

	err := (&TruncateInsertStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "failed to atomically insert from staging")
	require.Empty(t, dest.truncateCalls)
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
}
