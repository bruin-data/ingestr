package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
)

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
