package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
)

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
