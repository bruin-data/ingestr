package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
)

func singleBatchRecords(t *testing.T, rows ...int64) <-chan source.RecordBatchResult {
	t.Helper()
	batch := int64RecordBatch(t, "id", rows, nil)
	return mustClosedRecords(source.RecordBatchResult{Batch: batch})
}

func TestIngestionJob_GetRecords_UsesBuffered(t *testing.T) {
	buffered := mustClosedRecords(source.RecordBatchResult{})
	job, src, _ := minimalJob()
	job.BufferedRecords = buffered

	got, err := job.GetRecords(context.Background(), source.ReadOptions{})
	if err != nil {
		t.Fatalf("GetRecords returned error: %v", err)
	}
	if got != buffered {
		t.Fatalf("GetRecords did not return buffered records channel")
	}

	src.mu.Lock()
	defer src.mu.Unlock()
	if src.readCalled {
		t.Fatalf("expected Source.Read not to be called when BufferedRecords is set")
	}
}

func TestIngestionJob_GetRecords_AppliesTransformation(t *testing.T) {
	job, _, _ := minimalJob()
	job.BufferedRecords = mustClosedRecords(source.RecordBatchResult{
		Batch: intStringRecordBatch(t, "id", []int64{1}, "name", []string{"  alice  "}),
	})
	job.WhitespaceTrimmer = transformer.NewWhitespaceTrimmer()

	records, err := job.GetRecords(context.Background(), source.ReadOptions{})
	if err != nil {
		t.Fatalf("GetRecords returned error: %v", err)
	}

	result := <-records
	if result.Err != nil {
		t.Fatalf("transformed record returned error: %v", result.Err)
	}
	defer result.Batch.Release()

	names := result.Batch.Column(1).(*array.String)
	if got := names.Value(0); got != "alice" {
		t.Fatalf("trimmed name = %q, want alice", got)
	}
}

func TestAppendStrategy_Execute_HappyPath(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.LoaderFileSize = 321
	src.readCh = mustClosedRecords()

	strat := &AppendStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 1 {
		t.Fatalf("expected 1 PrepareTable call, got %d", len(dest.prepareCalls))
	}
	if dest.prepareCalls[0].Table != job.Config.DestTable {
		t.Fatalf("PrepareTable.Table = %q, want %q", dest.prepareCalls[0].Table, job.Config.DestTable)
	}
	if dest.prepareCalls[0].DropFirst {
		t.Fatalf("PrepareTable.DropFirst = true, want false")
	}

	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected 1 WriteParallel call, got %d", len(dest.writeCalls))
	}
	if dest.writeCalls[0].Table != job.Config.DestTable {
		t.Fatalf("WriteParallel.Table = %q, want %q", dest.writeCalls[0].Table, job.Config.DestTable)
	}
	if dest.writeCalls[0].Parallelism != job.Config.ExtractParallelism {
		t.Fatalf("WriteParallel.Parallelism = %d, want %d", dest.writeCalls[0].Parallelism, job.Config.ExtractParallelism)
	}
	if dest.writeCalls[0].LoaderFileSize != job.Config.LoaderFileSize {
		t.Fatalf("WriteParallel.LoaderFileSize = %d, want %d", dest.writeCalls[0].LoaderFileSize, job.Config.LoaderFileSize)
	}
	if dest.writeCalls[0].StagingTable {
		t.Fatalf("WriteParallel.StagingTable = true, want false")
	}

	src.mu.Lock()
	defer src.mu.Unlock()
	if !src.readCalled {
		t.Fatalf("expected Source.Read to be called")
	}
	if src.readOpts.Parallelism != job.Config.ExtractParallelism {
		t.Fatalf("ReadOptions.Parallelism = %d, want %d", src.readOpts.Parallelism, job.Config.ExtractParallelism)
	}
	if src.readOpts.Schema != job.SourceSchema {
		t.Fatalf("ReadOptions.Schema not set to job.SourceSchema")
	}
}

func TestAppendStrategy_Execute_DefaultParallelism(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.ExtractParallelism = 0
	src.readCh = mustClosedRecords()

	strat := &AppendStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if dest.writeCalls[0].Parallelism != 4 {
		t.Fatalf("WriteParallel.Parallelism = %d, want 4", dest.writeCalls[0].Parallelism)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.readOpts.Parallelism != 4 {
		t.Fatalf("ReadOptions.Parallelism = %d, want 4", src.readOpts.Parallelism)
	}
}

func TestReplaceStrategy_Execute_HappyPath(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyReplace
	job.Config.LoaderFileSize = 654
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Primary keys are set and the destination can merge, so replace
	// deduplicates: write to a PK-free staging table, merge-dedup into a table in
	// the target's schema, then atomically swap that table into the target.
	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected 2 PrepareTable calls (staging + dedup), got %d", len(dest.prepareCalls))
	}
	stagingTable := dest.prepareCalls[0].Table
	if !strings.HasPrefix(stagingTable, "_bruin_staging.ds__tbl_staging_") {
		t.Fatalf("staging table = %q, expected prefix %q", stagingTable, "_bruin_staging.ds__tbl_staging_")
	}
	if !dest.prepareCalls[0].DropFirst {
		t.Fatalf("PrepareTable.DropFirst = false, want true")
	}
	if len(dest.prepareCalls[0].PrimaryKeys) != 0 {
		t.Fatalf("staging table should be PK-free, got %v", dest.prepareCalls[0].PrimaryKeys)
	}
	normalisedTable := dest.prepareCalls[1].Table
	if !strings.HasPrefix(normalisedTable, "ds.ds__tbl_staging_normalised_") {
		t.Fatalf("normalised table = %q, expected prefix %q (target schema)", normalisedTable, "ds.ds__tbl_staging_normalised_")
	}

	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected 1 MergeTable call, got %d", len(dest.mergeCalls))
	}
	if dest.mergeCalls[0].StagingTable != stagingTable || dest.mergeCalls[0].TargetTable != normalisedTable {
		t.Fatalf("MergeTable = %q -> %q, want %q -> %q", dest.mergeCalls[0].StagingTable, dest.mergeCalls[0].TargetTable, stagingTable, normalisedTable)
	}
	if dest.mergeCalls[0].IncrementalKey != job.Config.IncrementalKey {
		t.Fatalf("MergeTable.IncrementalKey = %q, want %q", dest.mergeCalls[0].IncrementalKey, job.Config.IncrementalKey)
	}

	if len(dest.swapCalls) != 1 {
		t.Fatalf("expected 1 SwapTable call, got %d", len(dest.swapCalls))
	}
	if dest.swapCalls[0][0] != normalisedTable || dest.swapCalls[0][1] != job.Config.DestTable {
		t.Fatalf("SwapTable args = %v, want [%q %q]", dest.swapCalls[0], normalisedTable, job.Config.DestTable)
	}

	// The raw staging table is dropped after dedup.
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != stagingTable {
		t.Fatalf("expected DropTable(%q) after dedup, got %v", stagingTable, dest.dropCalls)
	}
	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected 1 WriteParallel call, got %d", len(dest.writeCalls))
	}
	if !dest.writeCalls[0].StagingTable {
		t.Fatalf("WriteParallel.StagingTable = false, want true")
	}
	if dest.writeCalls[0].LoaderFileSize != job.Config.LoaderFileSize {
		t.Fatalf("WriteParallel.LoaderFileSize = %d, want %d", dest.writeCalls[0].LoaderFileSize, job.Config.LoaderFileSize)
	}
	if len(dest.waitCalls) != 0 {
		t.Fatalf("expected no WaitForExactRowCount calls for empty write, got %v", dest.waitCalls)
	}
}

func TestReplaceStrategy_Execute_WriteFails_DropsStaging(t *testing.T) {
	job, src, dest := minimalJob()
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("write failed")

	strat := &ReplaceStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write data") {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dest.prepareCalls) != 1 {
		t.Fatalf("expected 1 PrepareTable call, got %d", len(dest.prepareCalls))
	}
	stagingTable := dest.prepareCalls[0].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != stagingTable {
		t.Fatalf("expected DropTable(%q), got %v", stagingTable, dest.dropCalls)
	}
	if len(dest.swapCalls) != 0 {
		t.Fatalf("expected SwapTable not to be called on write failure")
	}
}

func TestReplaceStrategy_Execute_SwapFails_DropsStaging(t *testing.T) {
	job, src, dest := minimalJob()
	src.readCh = mustClosedRecords()
	dest.swapErr = errors.New("swap failed")

	strat := &ReplaceStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to swap tables") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dedup path: raw staging is dropped after the merge, then the swap fails and
	// the normalised table is dropped too.
	stagingTable := dest.prepareCalls[0].Table
	normalisedTable := dest.prepareCalls[1].Table
	want := []string{stagingTable, normalisedTable}
	if len(dest.dropCalls) != 2 || dest.dropCalls[0] != want[0] || dest.dropCalls[1] != want[1] {
		t.Fatalf("expected DropTable %v, got %v", want, dest.dropCalls)
	}
}

func TestReplaceStrategy_Execute_VerifiesExactRowCount(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyReplace
	src.readCh = singleBatchRecords(t, 1, 2, 3)

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.waitCalls) != 1 {
		t.Fatalf("expected 1 WaitForExactRowCount call, got %d", len(dest.waitCalls))
	}
	stagingTable := dest.prepareCalls[0].Table
	if dest.waitCalls[0].Table != stagingTable {
		t.Fatalf("WaitForExactRowCount.Table = %q, want %q", dest.waitCalls[0].Table, stagingTable)
	}
	if dest.waitCalls[0].ExpectedRows != 3 {
		t.Fatalf("WaitForExactRowCount.ExpectedRows = %d, want 3", dest.waitCalls[0].ExpectedRows)
	}
}

func TestReplaceStrategy_Execute_VerifyRowCountFails_DropsStaging(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyReplace
	src.readCh = singleBatchRecords(t, 1, 2, 3)
	dest.waitErr = errors.New("row count mismatch")

	strat := &ReplaceStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to verify staging table row count") {
		t.Fatalf("unexpected error: %v", err)
	}

	stagingTable := dest.prepareCalls[0].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != stagingTable {
		t.Fatalf("expected DropTable(%q), got %v", stagingTable, dest.dropCalls)
	}
	if len(dest.swapCalls) != 0 {
		t.Fatalf("expected SwapTable not to be called on row count verification failure")
	}
}
