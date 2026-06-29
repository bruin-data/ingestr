package strategy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestMergeStrategy_Validate(t *testing.T) {
	strat := &MergeStrategy{}

	cfg := &config.IngestConfig{PrimaryKeys: nil}
	if err := strat.Validate(cfg); err == nil {
		t.Fatalf("expected error when primary keys are missing")
	}

	cfg.PrimaryKeys = []string{"id"}
	if err := strat.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeStrategy_Execute_HappyPath(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.PrimaryKeys = []string{"id"}
	job.Config.LoaderFileSize = 222
	src.readCh = mustClosedRecords()

	strat := &MergeStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if dest.prepareCalls[0].Table != job.Config.DestTable || dest.prepareCalls[0].DropFirst {
		t.Fatalf("first PrepareTable call should prepare destination without DropFirst: %+v", dest.prepareCalls[0])
	}

	staging := dest.prepareCalls[1].Table
	if !strings.HasPrefix(staging, "_bruin_staging.ds__tbl_merge_") {
		t.Fatalf("staging table = %q, expected prefix %q", staging, "_bruin_staging.ds__tbl_merge_")
	}
	if !dest.prepareCalls[1].DropFirst {
		t.Fatalf("staging table PrepareTable should set DropFirst=true")
	}

	if len(dest.writeCalls) != 1 || dest.writeCalls[0].Table != staging {
		t.Fatalf("expected WriteParallel to staging table %q, got %+v", staging, dest.writeCalls)
	}
	if !dest.writeCalls[0].StagingTable {
		t.Fatalf("WriteParallel.StagingTable = false, want true")
	}
	if dest.writeCalls[0].LoaderFileSize != job.Config.LoaderFileSize {
		t.Fatalf("WriteParallel.LoaderFileSize = %d, want %d", dest.writeCalls[0].LoaderFileSize, job.Config.LoaderFileSize)
	}

	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected 1 MergeTable call, got %d", len(dest.mergeCalls))
	}
	if dest.mergeCalls[0].StagingTable != staging || dest.mergeCalls[0].TargetTable != job.Config.DestTable {
		t.Fatalf("MergeOptions tables = %+v", dest.mergeCalls[0])
	}
	if len(dest.mergeCalls[0].PrimaryKeys) != 1 || dest.mergeCalls[0].PrimaryKeys[0] != "id" {
		t.Fatalf("MergeOptions.PrimaryKeys = %v", dest.mergeCalls[0].PrimaryKeys)
	}
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}

	src.mu.Lock()
	defer src.mu.Unlock()
	if !src.readCalled {
		t.Fatalf("expected Source.Read to be called")
	}
}

func TestMergeStrategy_Execute_GetRecordsFails_LeavesStagingForDebugging(t *testing.T) {
	job, src, dest := minimalJob()
	src.readErr = errors.New("read failed")

	strat := &MergeStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get records") {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if len(dest.dropCalls) != 0 {
		t.Fatalf("expected staging table to be left for debugging, got %d DropTable calls", len(dest.dropCalls))
	}
}

func TestDeleteInsertStrategy_Validate(t *testing.T) {
	strat := &DeleteInsertStrategy{}
	cfg := &config.IngestConfig{IncrementalKey: ""}
	if err := strat.Validate(cfg); err == nil {
		t.Fatalf("expected error when incremental_key is missing")
	}
	cfg.IncrementalKey = "updated_at"
	if err := strat.Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteInsertStrategy_Execute_RejectsUnsupportedDestinationBeforeStaging(t *testing.T) {
	job, _, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"
	dest.noDeleteInsert = true

	strat := &DeleteInsertStrategy{}
	err := strat.Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected unsupported destination error")
	}
	if !strings.Contains(err.Error(), "does not support delete+insert strategy") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dest.prepareCalls) != 0 || len(dest.writeCalls) != 0 || len(dest.diCalls) != 0 {
		t.Fatalf("expected no staging work, got prepare=%d write=%d di=%d", len(dest.prepareCalls), len(dest.writeCalls), len(dest.diCalls))
	}
}

func TestDeleteInsertStrategy_Execute_SkipsWhenNoIntervalDetected(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"

	rec := int64RecordBatch(t, "other", []int64{1, 2, 3}, nil)
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: rec})

	strat := &DeleteInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	staging := dest.prepareCalls[1].Table
	if !strings.Contains(staging, "_di_") {
		t.Fatalf("expected delete+insert staging table name, got %q", staging)
	}

	if len(dest.diCalls) != 0 {
		t.Fatalf("expected DeleteInsertTable not to be called, got %+v", dest.diCalls)
	}

	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestDeleteInsertStrategy_Execute_UsesAutoDetectedInterval(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"
	job.Config.LoaderFileSize = 111

	rec := int64RecordBatch(t, "id", []int64{5, 10, 7}, map[int]bool{1: false})
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: rec})

	strat := &DeleteInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	staging := dest.prepareCalls[1].Table
	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected 1 WriteParallel call, got %d", len(dest.writeCalls))
	}
	if !dest.writeCalls[0].StagingTable {
		t.Fatalf("WriteParallel.StagingTable = false, want true")
	}
	if dest.writeCalls[0].LoaderFileSize != job.Config.LoaderFileSize {
		t.Fatalf("WriteParallel.LoaderFileSize = %d, want %d", dest.writeCalls[0].LoaderFileSize, job.Config.LoaderFileSize)
	}
	if len(dest.diCalls) != 1 {
		t.Fatalf("expected 1 DeleteInsertTable call, got %d", len(dest.diCalls))
	}
	di := dest.diCalls[0]
	if di.StagingTable != staging || di.TargetTable != job.Config.DestTable {
		t.Fatalf("DeleteInsertOptions tables = %+v", di)
	}
	if di.IncrementalKey != "id" {
		t.Fatalf("DeleteInsertOptions.IncrementalKey = %q", di.IncrementalKey)
	}
	if di.IntervalStart != int64(5) || di.IntervalEnd != int64(10) {
		t.Fatalf("DeleteInsertOptions interval = %v..%v, want 5..10", di.IntervalStart, di.IntervalEnd)
	}
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestDeleteInsertStrategy_Execute_TracksIncrementalKeyCaseInsensitive(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"
	job.Schema.Columns[0].Name = "ID"
	job.Schema.PrimaryKeys = []string{"ID"}

	rec := int64RecordBatch(t, "ID", []int64{5, 10, 7}, map[int]bool{1: false})
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: rec})

	strat := &DeleteInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.diCalls) != 1 {
		t.Fatalf("expected 1 DeleteInsertTable call, got %d", len(dest.diCalls))
	}
	di := dest.diCalls[0]
	if di.IncrementalKey != "ID" {
		t.Fatalf("DeleteInsertOptions.IncrementalKey = %q, want ID", di.IncrementalKey)
	}
	if di.IntervalStart != int64(5) || di.IntervalEnd != int64(10) {
		t.Fatalf("DeleteInsertOptions interval = %v..%v, want 5..10", di.IntervalStart, di.IntervalEnd)
	}
}

func TestDeleteInsertStrategy_Execute_UserIntervalOverridesAuto(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "updated_at"

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	job.Config.IntervalStart = &start
	job.Config.IntervalEnd = &end

	job.Schema.Columns = append(job.Schema.Columns, schema.Column{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true})

	rec := timestampRecordBatch(t, "updated_at", []time.Time{
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC),
	})
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: rec})

	strat := &DeleteInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.diCalls) != 1 {
		t.Fatalf("expected 1 DeleteInsertTable call, got %d", len(dest.diCalls))
	}
	di := dest.diCalls[0]

	gotStart, okStart := di.IntervalStart.(*time.Time)
	gotEnd, okEnd := di.IntervalEnd.(*time.Time)
	if !okStart || !okEnd || gotStart == nil || gotEnd == nil {
		t.Fatalf("expected *time.Time bounds, got %T/%T", di.IntervalStart, di.IntervalEnd)
	}
	if !gotStart.Equal(start) || !gotEnd.Equal(end) {
		t.Fatalf("DeleteInsertOptions interval = %v..%v, want %v..%v", gotStart, gotEnd, start, end)
	}
}

func TestIntervalTracker_Int64_MinMax(t *testing.T) {
	tk := NewIntervalTracker("id")

	rec1 := int64RecordBatch(t, "id", []int64{3, 1, 2}, nil)
	rec2 := int64RecordBatch(t, "id", []int64{9, 7}, map[int]bool{0: true}) // null then 7

	in := mustClosedRecords(
		source.RecordBatchResult{Batch: rec1},
		source.RecordBatchResult{Batch: rec2},
	)

	out := tk.Wrap(in)
	for res := range out {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}

	if tk.Min != int64(1) || tk.Max != int64(7) {
		t.Fatalf("Min/Max = %v/%v, want 1/7", tk.Min, tk.Max)
	}
}

func TestIntervalTracker_Timestamp_MinMax(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	fields := []arrow.Field{{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Microsecond}, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewTimestampBuilder(pool, &arrow.TimestampType{Unit: arrow.Microsecond})
	defer b.Release()

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	b.Append(arrow.Timestamp(t2.UnixMicro()))
	b.AppendNull()
	b.Append(arrow.Timestamp(t1.UnixMicro()))
	arr := b.NewArray()
	defer arr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, 3)

	tk := NewIntervalTracker("ts")
	in := mustClosedRecords(source.RecordBatchResult{Batch: rec})
	out := tk.Wrap(in)
	for res := range out {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}

	min, okMin := tk.Min.(time.Time)
	max, okMax := tk.Max.(time.Time)
	if !okMin || !okMax {
		t.Fatalf("expected time.Time bounds, got %T/%T", tk.Min, tk.Max)
	}
	if !min.Equal(t1) || !max.Equal(t2) {
		t.Fatalf("Min/Max = %v/%v, want %v/%v", min, max, t1, t2)
	}
}

func TestResolveIntervalBound_UsesAutoWhenUserIsTypedNil(t *testing.T) {
	var user *time.Time = nil
	got := resolveIntervalBound(user, int64(5))
	if got != int64(5) {
		t.Fatalf("resolveIntervalBound(typed-nil, 5) = %v, want 5", got)
	}
}

func TestIsNilInterface(t *testing.T) {
	var p *int = nil
	if !isNilInterface(p) {
		t.Fatalf("expected typed nil pointer to be nil")
	}
	var s []string = nil
	if !isNilInterface(s) {
		t.Fatalf("expected typed nil slice to be nil")
	}
	if isNilInterface(0) {
		t.Fatalf("expected int(0) not to be nil")
	}
}

func TestIntervalTracker_Decimal128_MinMax(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	dt := &arrow.Decimal128Type{Precision: 38, Scale: 0}
	fields := []arrow.Field{{Name: "id", Type: dt, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewDecimal128Builder(pool, dt)
	defer b.Release()

	b.Append(decimal128.FromI64(3))
	b.Append(decimal128.FromI64(1))
	b.AppendNull()
	b.Append(decimal128.FromI64(7))
	b.Append(decimal128.FromI64(5))
	arr := b.NewArray()
	defer arr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, 5)

	tk := NewIntervalTracker("id")
	in := mustClosedRecords(source.RecordBatchResult{Batch: rec})
	out := tk.Wrap(in)
	for res := range out {
		if res.Batch != nil {
			res.Batch.Release()
		}
	}

	if tk.Min == nil || tk.Max == nil {
		t.Fatalf("Min/Max should be set for Decimal128 column, got %v/%v", tk.Min, tk.Max)
	}
	minF, okMin := tk.Min.(float64)
	maxF, okMax := tk.Max.(float64)
	if !okMin || !okMax {
		t.Fatalf("expected float64 bounds for Decimal128, got %T/%T", tk.Min, tk.Max)
	}
	if minF != 1 || maxF != 7 {
		t.Fatalf("Min/Max = %v/%v, want 1/7", minF, maxF)
	}
}
