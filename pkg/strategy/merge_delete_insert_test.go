package strategy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/require"
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

func TestMultiTableMergeReadFailureUsesDetachedStagingCleanup(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{name: "canceled", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}},
		{name: "deadline", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := source.SourceTableInfo{Name: "events", Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}
			src := &readAllSetupErrorSource{
				announcingMultiTableSource: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}},
				err:                        errors.New("read setup failed"),
			}
			base := &fakeDestination{}
			dest := &contextAwareDropDestination{fakeDestination: base}
			job := &MultiTableIngestionJob{
				Config: &config.IngestConfig{IncrementalStrategy: config.StrategyMerge}, Source: src, Destination: dest,
				Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "events"},
			}
			ctx, cancel := tc.ctx()
			defer cancel()

			if err := (&MergeStrategy{}).ExecuteMultiTable(ctx, job); err == nil {
				t.Fatal("expected read setup failure")
			}
			if len(dest.successfulDrops) != 1 {
				t.Fatalf("managed staging cleanup inherited %s context: %v", tc.name, dest.successfulDrops)
			}
			if !strings.Contains(dest.successfulDrops[0], "_merge_") {
				t.Fatalf("unexpected staging cleanup table: %v", dest.successfulDrops)
			}
		})
	}
}

func TestMultiTableMergeLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	table := source.SourceTableInfo{Name: "events", Schema: streamTestSchema(), PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult)
	close(records)
	base := &fakeDestination{}
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyMerge},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "events"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (&MergeStrategy{}).ExecuteMultiTable(ctx, job)
	if err == nil || !strings.Contains(err.Error(), "prepare response lost") {
		t.Fatalf("expected lost staging prepare response, got %v", err)
	}
	if len(dest.successfulDrops) != 1 {
		t.Fatalf("lost staging prepare was not detached-cleaned: %v", dest.successfulDrops)
	}
	if dest.successfulDrops[0] != base.prepareCalls[1].Table {
		t.Fatalf("cleaned %q, want prepared staging %q", dest.successfulDrops[0], base.prepareCalls[1].Table)
	}
}

func TestMergeLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	job, _, base := minimalJob()
	job.Config.PrimaryKeys = []string{"id"}
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&MergeStrategy{}).Execute(ctx, job), "prepare response lost")
	require.Len(t, base.prepareCalls, 2)
	require.Equal(t, []string{base.prepareCalls[1].Table}, dest.successfulDrops)
}

func TestDeleteInsertLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	job, _, base := minimalJob()
	job.Config.IncrementalKey = "id"
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&DeleteInsertStrategy{}).Execute(ctx, job), "prepare response lost")
	require.Len(t, base.prepareCalls, 2)
	require.Equal(t, []string{base.prepareCalls[1].Table}, dest.successfulDrops)
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
	if dest.mergeCalls[0].IncrementalKey != "id" {
		t.Fatalf("MergeOptions.IncrementalKey = %q, want id", dest.mergeCalls[0].IncrementalKey)
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

func TestMergeStrategy_LeaseLossAfterStagingWriteSkipsLaterMutations(t *testing.T) {
	job, src, base := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.PrimaryKeys = []string{"id"}
	src.readCh = mustClosedRecords()
	dest := &stageBlockingDestination{
		truncateCapableDestination: &truncateCapableDestination{fakeDestination: base},
		stage:                      "write",
		entered:                    make(chan struct{}),
		release:                    make(chan struct{}),
	}
	job.Destination = dest
	lease := &streamingTestLease{done: make(chan struct{}), err: errors.New("lease lost after batch staging write")}

	done := make(chan error, 1)
	go func() { done <- (&MergeStrategy{}).Execute(guardedStreamingContext(lease), job) }()
	<-dest.entered
	close(lease.done)
	close(dest.release)

	err := <-done
	if !errors.Is(err, source.ErrConnectorLeaseLost) {
		t.Fatalf("Execute error = %v, want connector lease loss", err)
	}
	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.mergeCalls) != 0 || len(base.truncateCalls) != 0 || len(base.dropCalls) != 0 {
		t.Fatalf("mutations after lease loss: merge=%d truncate=%d drop=%d", len(base.mergeCalls), len(base.truncateCalls), len(base.dropCalls))
	}
}

func TestMergeStrategy_EnablesSnapshotReplacementOnlyForCapableCDCDestination(t *testing.T) {
	job, src, _ := minimalJob()
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{
		Name:     "_cdc_deleted",
		DataType: schema.TypeBoolean,
		Nullable: false,
	})
	job.SourceSchema = job.Schema
	src.readCh = mustClosedRecords()

	if err := (&MergeStrategy{}).Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute without truncate capability returned error: %v", err)
	}
	if src.readOpts.CDCSnapshotReplace {
		t.Fatal("snapshot replacement enabled for destination without truncate capability")
	}

	job, src, dest := minimalJob()
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{
		Name:     "_cdc_deleted",
		DataType: schema.TypeBoolean,
		Nullable: false,
	})
	job.SourceSchema = job.Schema
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords()

	if err := (&MergeStrategy{}).Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute with truncate capability returned error: %v", err)
	}
	if !src.readOpts.CDCSnapshotReplace {
		t.Fatal("snapshot replacement not enabled for truncate-capable CDC destination")
	}
}

func TestMergeStrategy_Execute_SkipsOrderingKeyMissingFromStagingSchema(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyMerge
	job.Config.PrimaryKeys = []string{"id"}
	job.Config.IncrementalKey = "updated_at"
	src.readCh = mustClosedRecords()

	strat := &MergeStrategy{}
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

func TestMergeStrategy_Execute_GetRecordsFails_DropsManagedStaging(t *testing.T) {
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
	staging := dest.prepareCalls[1].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestMergeStrategy_Execute_WriteFails_DropsManagedStaging(t *testing.T) {
	job, src, dest := minimalJob()
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("write failed")

	err := (&MergeStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected write failure")
	}
	staging := dest.prepareCalls[1].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestMergeStrategy_WriterFailureBoundsNonclosingSource(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })
	job, src, base := minimalJob()
	records := make(chan source.RecordBatchResult)
	src.readCh = records
	job.Destination = &immediateFailStagedMergeDestination{fakeDestination: base}

	started := time.Now()
	err := (&MergeStrategy{}).Execute(context.Background(), job)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("merge did not return promptly after writer failure: %v", err)
	}
	close(records)
}

type immediateFailStagedMergeDestination struct {
	*fakeDestination
}

func (d *immediateFailStagedMergeDestination) WriteParallel(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
) error {
	return errors.New("writer failed")
}

func TestMergeStrategy_MultiTableWriterFailureCancelsAndDrainsSource(t *testing.T) {
	tableSchema := streamTestSchema()
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	done := make(chan struct{})
	src := &cancelAwareMultiTableSource{
		announcingMultiTableSource: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}},
		done:                       done,
	}
	base := &fakeDestination{}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    &immediateFailStagedMergeDestination{fakeDestination: base},
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "users"},
	}

	err := (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatal("expected writer failure")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("source producer remained blocked after staged writer failure")
	}
	if len(base.dropCalls) != 1 {
		t.Fatalf("expected failed staged merge cleanup, got %v", base.dropCalls)
	}
}

func TestMergeStrategy_PartialMultiTableSetupCleansCreatedStages(t *testing.T) {
	tableSchema := streamTestSchema()
	tables := []source.SourceTableInfo{
		{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}},
		{Name: "public.orders", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}
	base := &fakeDestination{}
	base.prepareHook = func(opts destination.PrepareOptions) error {
		if strings.Contains(opts.Table, "users_merge_") {
			return errors.New("prepare failed")
		}
		return nil
	}
	src := &announcingMultiTableSource{tables: tables, records: mustClosedRecords()}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{},
		Source:      src,
		Destination: base,
		Tables:      tables,
		TableDestNames: map[string]string{
			"public.users": "users", "public.orders": "orders",
		},
	}

	err := (&MergeStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatalf("expected partial setup failure, prepare calls: %+v", base.prepareCalls)
	}
	require.Len(t, base.dropCalls, 2)
	require.Condition(t, func() bool {
		return slices.ContainsFunc(base.dropCalls, func(table string) bool { return strings.Contains(table, "orders_merge_") })
	}, "expected orders staging cleanup, got %v", base.dropCalls)
	require.Condition(t, func() bool {
		return slices.ContainsFunc(base.dropCalls, func(table string) bool { return strings.Contains(table, "users_merge_") })
	}, "expected uncertain users staging cleanup, got %v", base.dropCalls)
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

func TestDeleteInsertStrategy_RejectsCDCBeforeDestinationOrSourceWork(t *testing.T) {
	job, src, dest := minimalJob()
	job.Schema = keylessCDCSchema()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert

	err := (&DeleteInsertStrategy{}).Execute(t.Context(), job)
	if err == nil || !strings.Contains(err.Error(), "not supported for CDC records") {
		t.Fatalf("Execute() error = %v, want CDC rejection", err)
	}
	if len(dest.prepareCalls) != 0 || len(dest.writeCalls) != 0 || src.readCalled {
		t.Fatalf("CDC rejection performed work: prepare=%d write=%d read=%v", len(dest.prepareCalls), len(dest.writeCalls), src.readCalled)
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

func TestDeleteInsertStrategy_FailureDropsManagedStaging(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("write failed")

	err := (&DeleteInsertStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected write failure")
	}
	staging := dest.prepareCalls[1].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestDeleteInsertStrategy_Execute_SkipsWhenNoIntervalDetected(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "id"
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: job.Config.DestTable,
		Comparison: &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
			Type: schemaevolution.ChangeAddColumn, ColumnName: "new_column", NewColumn: schema.Column{Name: "new_column", DataType: schema.TypeInt64},
		}}},
	}

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
	if len(dest.execCalls) != 1 {
		t.Fatalf("empty delete+insert must still apply schema evolution, got %d calls", len(dest.execCalls))
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
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{Name: "_cdc_unchanged_cols", DataType: schema.TypeString, Nullable: true})

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
	for _, column := range di.Columns {
		if column == "_cdc_unchanged_cols" {
			t.Fatalf("staging-only CDC column leaked into delete+insert target columns: %v", di.Columns)
		}
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
	job.SourceSchema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
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
	src.mu.Lock()
	readIncrementalKey := src.readOpts.IncrementalKey
	src.mu.Unlock()
	if readIncrementalKey != "id" {
		t.Fatalf("ReadOptions.IncrementalKey = %q, want id", readIncrementalKey)
	}
	if di.IntervalStart != int64(5) || di.IntervalEnd != int64(10) {
		t.Fatalf("DeleteInsertOptions interval = %v..%v, want 5..10", di.IntervalStart, di.IntervalEnd)
	}
}

func TestDeleteInsertStrategy_Execute_ReadsWithSourceIncrementalKeyAfterRename(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyDeleteInsert
	job.Config.IncrementalKey = "updated_at"
	job.Schema.Columns = []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "updated_at", DataType: schema.TypeInt64, Nullable: false},
	}
	job.Schema.IncrementalKey = "updated_at"
	job.SourceSchema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "updatedAt", DataType: schema.TypeInt64, Nullable: false},
		},
		IncrementalKey: "updatedAt",
	}
	job.ColumnRenamer = transformer.NewColumnRenamer(map[string]string{"updatedAt": "updated_at"})

	rec := int64RecordBatch(t, "updated_at", []int64{5, 10, 7}, nil)
	src.readCh = mustClosedRecords(source.RecordBatchResult{Batch: rec})

	strat := &DeleteInsertStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	src.mu.Lock()
	readIncrementalKey := src.readOpts.IncrementalKey
	src.mu.Unlock()
	if readIncrementalKey != "updatedAt" {
		t.Fatalf("ReadOptions.IncrementalKey = %q, want updatedAt", readIncrementalKey)
	}
	if len(dest.diCalls) != 1 {
		t.Fatalf("expected 1 DeleteInsertTable call, got %d", len(dest.diCalls))
	}
	if got := dest.diCalls[0].IncrementalKey; got != "updated_at" {
		t.Fatalf("DeleteInsertOptions.IncrementalKey = %q, want updated_at", got)
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
