package strategy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/require"
)

type contractCaptureReplaceDestination struct {
	*fakeDestination
	muCapture sync.Mutex
	types     []arrow.DataType
	nulls     []int
	rows      []int64
	schemas   []*schema.TableSchema
	fields    [][]string
}

type directContractCaptureReplaceDestination struct {
	*contractCaptureReplaceDestination
}

func (d *directContractCaptureReplaceDestination) SupportsAtomicSwap() bool { return false }

func (d *contractCaptureReplaceDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Batch != nil {
			d.muCapture.Lock()
			if result.Batch.NumCols() > 1 {
				d.types = append(d.types, result.Batch.Column(1).DataType())
				d.nulls = append(d.nulls, result.Batch.Column(1).NullN())
			}
			d.rows = append(d.rows, result.Batch.NumRows())
			d.schemas = append(d.schemas, opts.Schema)
			fields := make([]string, result.Batch.NumCols())
			for i, field := range result.Batch.Schema().Fields() {
				fields[i] = field.Name
			}
			d.fields = append(d.fields, fields)
			d.muCapture.Unlock()
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func singleBatchRecords(t *testing.T, rows ...int64) <-chan source.RecordBatchResult {
	t.Helper()
	batch := int64RecordBatch(t, "id", rows, nil)
	return mustClosedRecords(source.RecordBatchResult{Batch: batch})
}

func TestReplaceStrategyAppliesDiscardValueContractToRecreatedTable(t *testing.T) {
	job, src, base := minimalJob()
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
	}}}
	dest := &contractCaptureReplaceDestination{fakeDestination: base}
	job.Destination = dest
	job.Config.SchemaContract = "discard_value"
	job.Schema = finalSchema
	job.SourceSchema = sourceSchema
	job.SchemaComparison = comparison
	job.DestinationSchema = finalSchema
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: intStringRecordBatch(t, "id", []int64{1}, "value", []string{"invalid"}),
	})

	require.NoError(t, (&ReplaceStrategy{}).Execute(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Len(t, dest.types, 1)
	require.True(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, dest.types[0]))
	require.Equal(t, []int{1}, dest.nulls)
	require.Equal(t, schema.TypeInt64, dest.schemas[0].Columns[1].DataType)
}

func TestReplaceStrategyAppliesDiscardRowContractToRecreatedTable(t *testing.T) {
	job, src, base := minimalJob()
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
	}}}
	dest := &contractCaptureReplaceDestination{fakeDestination: base}
	job.Destination = dest
	job.Config.SchemaContract = "discard_row"
	job.Schema = finalSchema
	job.SourceSchema = sourceSchema
	job.SchemaComparison = comparison
	job.DestinationSchema = finalSchema
	job.SchemaAligner = transformer.NewSafeTypeCaster(finalSchema.ToArrowSchema())
	src.readCh = mustClosedRecords(source.RecordBatchResult{
		Batch: intStringRecordBatch(t, "id", []int64{1}, "value", []string{"invalid"}),
	})

	require.NoError(t, (&ReplaceStrategy{}).Execute(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Equal(t, []int64{0}, dest.rows)
	require.Equal(t, schema.TypeInt64, dest.schemas[0].Columns[1].DataType)
}

func TestMultiTableReplaceUsesContractFinalSchemaAndTransformation(t *testing.T) {
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
	}}}
	table := source.SourceTableInfo{Name: "public.events", Schema: sourceSchema}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: table.Name, Batch: intStringRecordBatch(t, "id", []int64{1}, "value", []string{"invalid"}),
	}
	close(records)
	dest := &contractCaptureReplaceDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyReplace, SchemaContract: "discard_value"},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		EvolutionPlans: map[string]*schemaevolution.EvolutionPlan{
			table.Name: {TransformComparison: comparison, FinalSchema: finalSchema},
		},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Len(t, dest.types, 1)
	require.True(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, dest.types[0]))
	require.Equal(t, []int{1}, dest.nulls)
	require.Equal(t, schema.TypeInt64, dest.schemas[0].Columns[1].DataType)
	require.Equal(t, schema.TypeInt64, dest.prepareCalls[0].Schema.Columns[1].DataType)
}

func TestMultiTableReplaceDiscardRowFiltersViolations(t *testing.T) {
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64}, {Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
	}}}
	table := source.SourceTableInfo{Name: "public.events", Schema: sourceSchema}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: table.Name, Batch: intStringRecordBatch(t, "id", []int64{1}, "value", []string{"invalid"}),
	}
	close(records)
	dest := &contractCaptureReplaceDestination{fakeDestination: &fakeDestination{}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyReplace, SchemaContract: "discard_row"},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "lake.events"},
		EvolutionPlans: map[string]*schemaevolution.EvolutionPlan{
			table.Name: {TransformComparison: comparison, FinalSchema: finalSchema},
		},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Equal(t, []int64{0}, dest.rows)
	require.Equal(t, schema.TypeInt64, dest.schemas[0].Columns[1].DataType)
}

func TestReplaceNeverPublishesStagingOnlyCDCColumns(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "staging"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			job, src, base := minimalJob()
			fullSchema := &schema.TableSchema{Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "_cdc_unchanged_cols", DataType: schema.TypeString, Nullable: true},
			}}
			capture := &contractCaptureReplaceDestination{fakeDestination: base}
			if direct {
				job.Destination = &directContractCaptureReplaceDestination{contractCaptureReplaceDestination: capture}
			} else {
				job.Destination = capture
			}
			job.Schema = fullSchema
			job.SourceSchema = fullSchema
			src.readCh = mustClosedRecords(source.RecordBatchResult{
				Batch: intStringRecordBatch(t, "id", []int64{1}, "_cdc_unchanged_cols", []string{"value"}),
			})

			require.NoError(t, (&ReplaceStrategy{}).Execute(context.Background(), job))
			capture.muCapture.Lock()
			defer capture.muCapture.Unlock()
			require.Equal(t, [][]string{{"id"}}, capture.fields)
			require.Equal(t, []string{"id"}, capture.schemas[0].ColumnNames())
			require.Equal(t, []string{"id"}, base.prepareCalls[0].Schema.ColumnNames())
			if !direct {
				require.Len(t, base.swapOptions, 1)
				require.Equal(t, []string{"id"}, base.swapOptions[0].Schema.ColumnNames())
			}
		})
	}
}

func TestMultiTableReplaceNeverPublishesStagingOnlyCDCColumns(t *testing.T) {
	fullSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "_cdc_unchanged_cols", DataType: schema.TypeString, Nullable: true},
	}}
	table := source.SourceTableInfo{Name: "events", Schema: fullSchema}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{
		TableName: table.Name, Batch: intStringRecordBatch(t, "id", []int64{1}, "_cdc_unchanged_cols", []string{"value"}),
	}
	close(records)
	base := &fakeDestination{}
	dest := &contractCaptureReplaceDestination{fakeDestination: base}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyReplace},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "events"},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job))
	dest.muCapture.Lock()
	defer dest.muCapture.Unlock()
	require.Equal(t, [][]string{{"id"}}, dest.fields)
	require.Equal(t, []string{"id"}, dest.schemas[0].ColumnNames())
	require.Equal(t, []string{"id"}, base.prepareCalls[0].Schema.ColumnNames())
	require.Equal(t, []string{"id"}, base.swapOptions[0].Schema.ColumnNames())
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

func TestIngestionJob_GetRecords_AddsSameLoadTimestampToEveryBatch(t *testing.T) {
	job, _, _ := minimalJob()
	job.BufferedRecords = mustClosedRecords(
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{1, 2}, nil)},
		source.RecordBatchResult{Batch: int64RecordBatch(t, "id", []int64{3}, nil)},
	)

	ts := time.Date(2026, 6, 19, 10, 11, 12, 345678901, time.UTC)
	job.LoadTimestamp = transformer.NewLoadTimestamp(schema.Column{
		Name:     "_ingestr_loaded_at",
		DataType: schema.TypeTimestampTZ,
		Nullable: false,
	}, ts)

	records, err := job.GetRecords(context.Background(), source.ReadOptions{})
	if err != nil {
		t.Fatalf("GetRecords returned error: %v", err)
	}

	for i := 0; i < 2; i++ {
		result := <-records
		if result.Err != nil {
			t.Fatalf("record %d returned error: %v", i, result.Err)
		}
		if result.Batch == nil {
			t.Fatalf("record %d batch is nil", i)
		}
		if got := result.Batch.ColumnName(1); got != "_ingestr_loaded_at" {
			t.Fatalf("record %d column 1 = %q, want _ingestr_loaded_at", i, got)
		}
		loadedAt := result.Batch.Column(1).(*array.Timestamp)
		for row := 0; row < int(result.Batch.NumRows()); row++ {
			if got := int64(loadedAt.Value(row)); got != ts.UnixMicro() {
				t.Fatalf("record %d row %d timestamp = %d, want %d", i, row, got, ts.UnixMicro())
			}
		}
		result.Batch.Release()
	}

	if _, ok := <-records; ok {
		t.Fatal("records channel still open after two batches")
	}
}

func TestAppendStrategy_Execute_HappyPath(t *testing.T) {
	job, src, dest := minimalJob()
	job.Config.LoaderFileSize = 321
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	job.Config.IntervalStart = &start
	job.Config.IntervalEnd = &end
	job.Config.ExtractPartitionBy = "created_at"
	job.Config.ExtractPartitionInterval = 7 * 24 * time.Hour
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
	if src.readOpts.ExtractPartitionBy != job.Config.ExtractPartitionBy {
		t.Fatalf("ReadOptions.ExtractPartitionBy = %q, want %q", src.readOpts.ExtractPartitionBy, job.Config.ExtractPartitionBy)
	}
	if src.readOpts.ExtractPartitionInterval != job.Config.ExtractPartitionInterval {
		t.Fatalf("ReadOptions.ExtractPartitionInterval = %v, want %v", src.readOpts.ExtractPartitionInterval, job.Config.ExtractPartitionInterval)
	}
}

func TestAppendStrategy_CDCForwardsResumeAndAppliesSnapshotBoundary(t *testing.T) {
	job, src, dest := minimalJob()
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{
		Name:     "_cdc_deleted",
		DataType: schema.TypeBoolean,
		Nullable: false,
	})
	job.SourceSchema = job.Schema
	job.Config.CDCResumeLSN = "00000000/0000002A"
	job.Config.CDCSlotSuffix = "abc123"
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords(source.RecordBatchResult{Truncate: true})

	if err := (&AppendStrategy{}).Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if src.readOpts.CDCResumeLSN != job.Config.CDCResumeLSN {
		t.Fatalf("CDCResumeLSN = %q, want %q", src.readOpts.CDCResumeLSN, job.Config.CDCResumeLSN)
	}
	if src.readOpts.CDCSlotSuffix != job.Config.CDCSlotSuffix {
		t.Fatalf("CDCSlotSuffix = %q, want %q", src.readOpts.CDCSlotSuffix, job.Config.CDCSlotSuffix)
	}
	if !src.readOpts.CDCSnapshotReplace {
		t.Fatal("snapshot replacement not enabled for truncate-capable CDC destination")
	}
	if len(dest.truncateCalls) != 1 || dest.truncateCalls[0] != job.Config.DestTable {
		t.Fatalf("truncate calls = %v, want [%s]", dest.truncateCalls, job.Config.DestTable)
	}
}

func TestAppendStrategy_CDCResumeStillReplacesOnSnapshotFallback(t *testing.T) {
	job, src, dest := minimalJob()
	job.Schema.Columns = append(job.Schema.Columns, schema.Column{
		Name:     "_cdc_deleted",
		DataType: schema.TypeBoolean,
		Nullable: false,
	})
	job.SourceSchema = job.Schema
	job.Config.CDCResumeLSN = "00000000/0000002A"
	job.Destination = &truncateCapableDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords(source.RecordBatchResult{Truncate: true})

	if err := (&AppendStrategy{}).Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !src.readOpts.CDCSnapshotReplace {
		t.Fatal("CDC resume must replace the target if the source falls back to a snapshot")
	}
	if len(dest.truncateCalls) != 1 {
		t.Fatalf("truncate calls = %v, want one", dest.truncateCalls)
	}
}

func TestAppendStrategyBindsFreshSnapshotDestinationBeforeWrite(t *testing.T) {
	job, src, _ := minimalJob()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "batch-destination-binding", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "100"))
	require.NoError(t, manager.BeginRun(t.Context(), false))
	job.Destination = dest
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords()

	require.NoError(t, (&AppendStrategy{}).Execute(t.Context(), job))
	dest.stateMu.Lock()
	dest.incarnations[job.Config.DestTable] = "externally-replaced"
	dest.stateMu.Unlock()

	err = manager.Persist(t.Context(), source.CDCStateCommitToken{
		Position:             "00000000/00000020",
		SnapshotPositions:    map[string]string{job.Config.SourceTable: "00000000/00000010"},
		SnapshotIncarnations: map[string]string{job.Config.SourceTable: "100"},
	})
	require.ErrorContains(t, err, "was replaced during its snapshot")
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

func TestReplaceStrategyManagedCDCDoesNotDelegateTargetCursor(t *testing.T) {
	job, src, _ := minimalJob()
	dest := &replaceCDCStateDestination{cdcStateDestination: newCDCStateDestination()}
	manager, err := NewCDCStateManager(dest, "managed-replace", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.Destination = dest
	job.Config.IncrementalStrategy = config.StrategyReplace
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords()
	require.NoError(t, (&ReplaceStrategy{}).Execute(t.Context(), job))
	require.Len(t, dest.writeCalls, 1)
	require.True(t, dest.writeCalls[0].SkipCDCResume)
	require.Len(t, dest.mergeCalls, 1)
	require.True(t, dest.mergeCalls[0].SkipCDCResume)
	dest.stateMu.Lock()
	dest.incarnations[job.Config.DestTable] = "externally-replaced"
	dest.stateMu.Unlock()
	err = manager.Persist(t.Context(), source.CDCStateCommitToken{
		Position: "0/20", SnapshotPositions: map[string]string{job.Config.SourceTable: "0/10"},
		SnapshotIncarnations: map[string]string{job.Config.SourceTable: "source-v1"},
	})
	require.ErrorContains(t, err, "was replaced during its snapshot")
}

func TestManagedReplaceFailsClosedWithoutConditionalSwap(t *testing.T) {
	job, src, _ := minimalJob()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "managed-replace-unsupported-swap", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.Destination = dest
	job.CDCStateManager = manager
	src.readCh = mustClosedRecords()
	prepareCalls := len(dest.prepareCalls)
	writeCalls := len(dest.writeCalls)

	err = (&ReplaceStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "cannot atomically fence managed CDC table replacement")
	require.Len(t, dest.prepareCalls, prepareCalls)
	require.Len(t, dest.writeCalls, writeCalls)
	require.Empty(t, dest.swapCalls)
}

func TestReplaceStrategy_Execute_SkipsDedupForUniqueSourcePrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 1 {
		t.Fatalf("expected 1 PrepareTable call, got %d", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 1 || got[0] != "id" {
		t.Fatalf("staging table should keep primary keys on the fast path, got %v", got)
	}
	if len(dest.mergeCalls) != 0 {
		t.Fatalf("expected no MergeTable calls on unique-PK fast path, got %d", len(dest.mergeCalls))
	}
	if len(dest.swapCalls) != 1 || dest.swapCalls[0][0] != dest.prepareCalls[0].Table {
		t.Fatalf("expected staging table to be swapped directly, got swaps=%v prepares=%v", dest.swapCalls, dest.prepareCalls)
	}
}

func TestReplaceStrategy_Execute_SkipsDedupWhenWhitespaceTrimmingCannotChangeNumericPrimaryKey(t *testing.T) {
	job, src, dest := minimalJob()
	job.WhitespaceTrimmer = transformer.NewWhitespaceTrimmer()
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 1 {
		t.Fatalf("expected unique numeric primary key to keep fast path, got %d PrepareTable calls", len(dest.prepareCalls))
	}
	if len(dest.mergeCalls) != 0 {
		t.Fatalf("expected no MergeTable call for numeric primary key, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_DedupsWhenWhitespaceTrimmingCanCollapseStringPrimaryKeys(t *testing.T) {
	job, src, dest := minimalJob()
	stringPrimaryKeySchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	job.Schema = stringPrimaryKeySchema
	job.SourceSchema = stringPrimaryKeySchema
	job.WhitespaceTrimmer = transformer.NewWhitespaceTrimmer()
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected trim-sensitive primary key to use dedup path, got %d PrepareTable calls", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 0 {
		t.Fatalf("raw staging table should be PK-free after whitespace trimming, got %v", got)
	}
	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected MergeTable call after whitespace trimming, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_RequestsDirectAtomicDeduplication(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &directReplaceDestination{fakeDestination: dest}
	job.Config.IncrementalKey = "id"
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 1 || dest.prepareCalls[0].Table != job.Config.DestTable {
		t.Fatalf("direct replace should prepare the target once, got %+v", dest.prepareCalls)
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 1 || got[0] != "id" {
		t.Fatalf("direct target should retain its primary key metadata, got %v", got)
	}
	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected one direct write, got %d", len(dest.writeCalls))
	}
	if !dest.writeCalls[0].DeduplicatePrimaryKeys {
		t.Fatal("direct replace did not request primary-key deduplication")
	}
	if got := dest.writeCalls[0].PrimaryKeys; len(got) != 1 || got[0] != "id" {
		t.Fatalf("WriteOptions.PrimaryKeys = %v, want [id]", got)
	}
	if dest.writeCalls[0].IncrementalKey != "id" {
		t.Fatalf("WriteOptions.IncrementalKey = %q, want id", dest.writeCalls[0].IncrementalKey)
	}
	if len(dest.mergeCalls) != 0 || len(dest.swapCalls) != 0 {
		t.Fatalf("direct replace should not stage/merge/swap, merge=%v swap=%v", dest.mergeCalls, dest.swapCalls)
	}
}

type directReplaceDestination struct {
	*fakeDestination
}

func (d *directReplaceDestination) SupportsAtomicSwap() bool { return false }
func (d *directReplaceDestination) SupportsDirectReplaceDeduplication() bool {
	return true
}

type replaceCDCStateDestination struct{ *cdcStateDestination }

func (*replaceCDCStateDestination) SupportsCDCConditionalSwap() bool { return true }

func (d *replaceCDCStateDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.fakeDestination.WriteParallel(ctx, records, opts)
}

func (d *replaceCDCStateDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	targetIncarnation, _, _ := d.CDCTargetIncarnation(ctx, opts.TargetTable)
	if opts.CDCExpectedIncarnation != targetIncarnation {
		return errors.New("destination target incarnation changed before staged replacement publish")
	}
	stagingIncarnation, _, _ := d.CDCTargetIncarnation(ctx, opts.StagingTable)
	if opts.CDCExpectedStagingIncarnation != stagingIncarnation {
		return errors.New("destination staging incarnation changed before staged replacement publish")
	}
	if err := d.fakeDestination.SwapTable(ctx, opts); err != nil {
		return err
	}
	d.stateMu.Lock()
	d.incarnations[opts.TargetTable] = stagingIncarnation
	d.stateMu.Unlock()
	return nil
}

type managedDirectReplaceDestination struct{ *replaceCDCStateDestination }

func (*managedDirectReplaceDestination) SupportsAtomicSwap() bool { return false }
func (*managedDirectReplaceDestination) SupportsDirectReplaceDeduplication() bool {
	return true
}

type recreatingDirectReplaceDestination struct {
	*managedDirectReplaceDestination
	recreateTable string
}

func (d *recreatingDirectReplaceDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.stateMu.Lock()
	d.incarnations[d.recreateTable] = "external-v2"
	d.stateMu.Unlock()
	current, _, _ := d.CDCTargetIncarnation(ctx, d.recreateTable)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	if opts.CDCExpectedIncarnation != current {
		return errors.New("destination incarnation changed before direct replacement write")
	}
	return nil
}

type recreatingStagedReplaceDestination struct {
	*replaceCDCStateDestination
	recreateTable string
}

type recreatingAfterStagedReplaceDestination struct {
	*replaceCDCStateDestination
}

type recreatingStagingAtSwapDestination struct {
	*replaceCDCStateDestination
}

func (d *recreatingStagingAtSwapDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	d.stateMu.Lock()
	d.incarnations[opts.StagingTable] = "unrelated-staging-recreation"
	d.stateMu.Unlock()
	return d.replaceCDCStateDestination.SwapTable(ctx, opts)
}

func (d *recreatingAfterStagedReplaceDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	if err := d.replaceCDCStateDestination.SwapTable(ctx, opts); err != nil {
		return err
	}
	d.stateMu.Lock()
	d.incarnations[opts.TargetTable] = "unrelated-recreation"
	d.stateMu.Unlock()
	return nil
}

func (d *recreatingStagedReplaceDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := d.fakeDestination.WriteParallel(ctx, records, opts); err != nil {
		return err
	}
	d.stateMu.Lock()
	d.incarnations[d.recreateTable] = "external-v2"
	d.stateMu.Unlock()
	return nil
}

func (d *recreatingStagedReplaceDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	current, _, _ := d.CDCTargetIncarnation(ctx, opts.TargetTable)
	if opts.CDCExpectedIncarnation != current {
		return errors.New("destination incarnation changed before staged replacement publish")
	}
	return d.replaceCDCStateDestination.SwapTable(ctx, opts)
}

func TestManagedReplaceRejectsTargetRecreationDuringExtraction(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "direct"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			job, src, _ := minimalJob()
			stateDest := newCDCStateDestination()
			stateDest.incarnations[job.Config.DestTable] = "target-v1"
			base := &replaceCDCStateDestination{cdcStateDestination: stateDest}
			if staged {
				job.Destination = &recreatingStagedReplaceDestination{replaceCDCStateDestination: base, recreateTable: job.Config.DestTable}
			} else {
				job.Destination = &recreatingDirectReplaceDestination{
					managedDirectReplaceDestination: &managedDirectReplaceDestination{replaceCDCStateDestination: base},
					recreateTable:                   job.Config.DestTable,
				}
			}
			manager, err := NewCDCStateManager(job.Destination, "replace-recreation-"+name, job.Config.DestTable, "")
			require.NoError(t, err)
			require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
			require.NoError(t, manager.BeginRun(t.Context(), true))
			job.CDCStateManager = manager
			src.readCh = singleBatchRecords(t, 1)

			err = (&ReplaceStrategy{}).Execute(t.Context(), job)
			require.ErrorContains(t, err, "incarnation changed")
		})
	}
}

func TestManagedReplaceRejectsRecreationAfterFencedPublication(t *testing.T) {
	job, src, _ := minimalJob()
	stateDest := newCDCStateDestination()
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	dest := &recreatingAfterStagedReplaceDestination{replaceCDCStateDestination: &replaceCDCStateDestination{cdcStateDestination: stateDest}}
	job.Destination = dest
	manager, err := NewCDCStateManager(dest, "replace-post-publication-recreation", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.CDCStateManager = manager
	src.readCh = singleBatchRecords(t, 1)

	err = (&ReplaceStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "changed after its fenced replacement")
}

func TestManagedReplaceRejectsStagingRecreationBeforeFencedPublication(t *testing.T) {
	job, src, _ := minimalJob()
	stateDest := newCDCStateDestination()
	stateDest.incarnations[job.Config.DestTable] = "target-v1"
	dest := &recreatingStagingAtSwapDestination{replaceCDCStateDestination: &replaceCDCStateDestination{cdcStateDestination: stateDest}}
	job.Destination = dest
	manager, err := NewCDCStateManager(dest, "replace-staging-recreation", job.Config.DestTable, "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), job.Config.SourceTable, job.Config.DestTable, "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job.CDCStateManager = manager
	src.readCh = singleBatchRecords(t, 1)

	err = (&ReplaceStrategy{}).Execute(t.Context(), job)
	require.ErrorContains(t, err, "staging incarnation changed")
	current, exists, incarnationErr := dest.CDCTargetIncarnation(t.Context(), job.Config.DestTable)
	require.NoError(t, incarnationErr)
	require.True(t, exists)
	require.Equal(t, "target-v1", current)
}

func TestManagedMultiTableReplaceRejectsTargetRecreationDuringExtraction(t *testing.T) {
	tableSchema := streamTestSchema()
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := mustClosedRecords(source.RecordBatchResult{
		TableName: table.Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil),
	})
	stateDest := newCDCStateDestination()
	stateDest.incarnations["users"] = "target-v1"
	dest := &recreatingDirectReplaceDestination{
		managedDirectReplaceDestination: &managedDirectReplaceDestination{
			replaceCDCStateDestination: &replaceCDCStateDestination{cdcStateDestination: stateDest},
		},
		recreateTable: "users",
	}
	manager, err := NewCDCStateManager(dest, "multi-replace-recreation", "users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "users", "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{}, Source: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "users"},
		CDCStateManager: manager,
	}

	err = (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "incarnation changed")
}

func TestManagedMultiTableReplaceBindFailureCleansOnlyUnpublishedStaging(t *testing.T) {
	tableSchema := streamTestSchema()
	tables := []source.SourceTableInfo{
		{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}},
		{Name: "public.orders", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}
	records := mustClosedRecords(
		source.RecordBatchResult{TableName: tables[0].Name, Batch: int64RecordBatch(t, "id", []int64{1}, nil)},
		source.RecordBatchResult{TableName: tables[1].Name, Batch: int64RecordBatch(t, "id", []int64{2}, nil)},
	)
	stateDest := newCDCStateDestination()
	stateDest.incarnations["users"] = "users-v1"
	stateDest.incarnations["orders"] = "orders-v1"
	dest := &recreatingAfterStagedReplaceDestination{replaceCDCStateDestination: &replaceCDCStateDestination{cdcStateDestination: stateDest}}
	manager, err := NewCDCStateManager(dest, "multi-replace-bind-failure", "users", "")
	require.NoError(t, err)
	for _, table := range tables {
		require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, strings.TrimPrefix(table.Name, "public."), "source-v1"))
	}
	require.NoError(t, manager.BeginRun(t.Context(), true))
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{}, Source: &announcingMultiTableSource{tables: tables, records: records},
		Destination: dest, Tables: tables,
		TableDestNames:  map[string]string{"public.users": "users", "public.orders": "orders"},
		CDCStateManager: manager,
	}

	err = (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job)
	require.ErrorContains(t, err, "changed after its fenced replacement")
	var stagingTables []string
	for _, call := range stateDest.prepareCalls {
		if call.ExpiresAfter == destination.ManagedStagingTTL {
			stagingTables = append(stagingTables, call.Table)
		}
	}
	require.GreaterOrEqual(t, len(stagingTables), 2)
	require.Len(t, stateDest.swapCalls, 1)
	publishedStaging := stateDest.swapCalls[0][0]
	var unpublishedStaging []string
	for _, stagingTable := range stagingTables {
		if stagingTable != publishedStaging {
			unpublishedStaging = append(unpublishedStaging, stagingTable)
		}
	}
	require.ElementsMatch(t, unpublishedStaging, stateDest.dropCalls)
	require.NotContains(t, stateDest.dropCalls, publishedStaging)
	require.NotContains(t, stateDest.dropCalls, "users")
}

type directReplaceWithoutDedupDestination struct {
	*fakeDestination
}

func (d *directReplaceWithoutDedupDestination) SupportsAtomicSwap() bool { return false }

func TestReplaceStrategy_Execute_DoesNotRequestDirectDeduplicationWithoutCapability(t *testing.T) {
	job, src, dest := minimalJob()
	job.Destination = &directReplaceWithoutDedupDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected one direct write, got %d", len(dest.writeCalls))
	}
	if dest.writeCalls[0].DeduplicatePrimaryKeys {
		t.Fatal("direct replace requested deduplication from a destination without the capability")
	}
	if got := dest.writeCalls[0].PrimaryKeys; len(got) != 1 || got[0] != "id" {
		t.Fatalf("WriteOptions.PrimaryKeys = %v, want [id]", got)
	}
}

func TestReplaceStrategy_DirectFailurePreservesUnownedTarget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*fakeSourceTable, *fakeDestination)
	}{
		{
			name: "read start failure",
			configure: func(src *fakeSourceTable, _ *fakeDestination) {
				src.readErr = errors.New("source unavailable")
			},
		},
		{
			name: "write failure",
			configure: func(src *fakeSourceTable, dest *fakeDestination) {
				src.readCh = mustClosedRecords()
				dest.writeErr = errors.New("catalog rejected write")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job, src, dest := minimalJob()
			job.Destination = &directReplaceDestination{fakeDestination: dest}
			tc.configure(src, dest)

			err := (&ReplaceStrategy{}).Execute(context.Background(), job)
			if err == nil {
				t.Fatal("expected direct replace to fail")
			}
			require.Empty(t, dest.dropCalls, "an unowned target may have been created by another writer")
		})
	}
}

func TestReplaceStrategy_DirectFailurePreservesPreexistingTarget(t *testing.T) {
	job, src, dest := minimalJob()
	dest.tableSchemas = map[string]*schema.TableSchema{job.Config.DestTable: job.Schema}
	job.Destination = &directReplaceDestination{fakeDestination: dest}
	src.readCh = mustClosedRecords()
	dest.writeErr = errors.New("catalog rejected write")

	err := (&ReplaceStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected direct replace to fail")
	}
	if len(dest.dropCalls) != 0 {
		t.Fatalf("preexisting target must survive failed direct replace, got drops %v", dest.dropCalls)
	}
}

func TestReplaceStrategy_ReadSetupFailureDropsManagedStaging(t *testing.T) {
	job, src, dest := minimalJob()
	src.readErr = errors.New("source unavailable")

	err := (&ReplaceStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected read setup failure")
	}
	staging := dest.prepareCalls[0].Table
	if len(dest.dropCalls) != 1 || dest.dropCalls[0] != staging {
		t.Fatalf("expected DropTable(%q), got %v", staging, dest.dropCalls)
	}
}

func TestReplaceStrategy_WriterFailureBoundsNonclosingSourceDrain(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })

	job, src, base := minimalJob()
	records := make(chan source.RecordBatchResult)
	src.readCh = records
	job.Destination = &immediateFailStagedMergeDestination{fakeDestination: base}

	started := time.Now()
	err := (&ReplaceStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected writer failure")
	}
	if time.Since(started) > time.Second {
		t.Fatal("replace waited indefinitely for a nonclosing source")
	}
	close(records)
	if len(base.dropCalls) != 1 {
		t.Fatalf("expected failed replace staging cleanup, got %v", base.dropCalls)
	}
}

func TestReplaceStrategy_ExecuteMultiTable_PropagatesDirectDeduplicationConfiguration(t *testing.T) {
	tableSchema := streamTestSchema()
	tableSchema.IncrementalKey = "id"
	table := source.SourceTableInfo{
		Name:        "public.users",
		Schema:      tableSchema,
		PrimaryKeys: []string{"id"},
	}
	records := make(chan source.RecordBatchResult)
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	stateDest := newCDCStateDestination()
	replaceDest := &replaceCDCStateDestination{cdcStateDestination: stateDest}
	managedDest := &managedDirectReplaceDestination{replaceCDCStateDestination: replaceDest}
	manager, err := NewCDCStateManager(managedDest, "managed-multi-replace", "users", "")
	require.NoError(t, err)
	require.NoError(t, manager.RegisterTableIncarnation(t.Context(), table.Name, "users", "source-v1"))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	dest := stateDest.fakeDestination
	job := &MultiTableIngestionJob{
		Config:          &config.IngestConfig{},
		Source:          src,
		Destination:     managedDest,
		Tables:          src.tables,
		TableDestNames:  map[string]string{"public.users": "users"},
		CDCStateManager: manager,
	}

	if err := (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job); err != nil {
		t.Fatalf("ExecuteMultiTable returned error: %v", err)
	}

	if len(dest.writeCalls) != 1 {
		t.Fatalf("expected one table write, got %d", len(dest.writeCalls))
	}
	write := dest.writeCalls[0]
	if !write.DeduplicatePrimaryKeys {
		t.Fatal("multi-table direct replace did not request primary-key deduplication")
	}
	if got := write.PrimaryKeys; len(got) != 1 || got[0] != "id" {
		t.Fatalf("WriteOptions.PrimaryKeys = %v, want [id]", got)
	}
	if write.IncrementalKey != "id" {
		t.Fatalf("WriteOptions.IncrementalKey = %q, want id", write.IncrementalKey)
	}
	if !write.SkipCDCResume {
		t.Fatal("managed multi-table direct replace delegated the CDC cursor to the destination")
	}
	stateDest.stateMu.Lock()
	stateDest.incarnations["users"] = "externally-replaced"
	stateDest.stateMu.Unlock()
	err = manager.Persist(t.Context(), source.CDCStateCommitToken{
		Position: "0/20", SnapshotPositions: map[string]string{table.Name: "0/10"},
		SnapshotIncarnations: map[string]string{table.Name: "source-v1"},
	})
	require.ErrorContains(t, err, "was replaced during its snapshot")
}

func TestReplaceStrategy_MultiTableDirectWriteFailurePreservesAttemptedTargets(t *testing.T) {
	tableSchema := streamTestSchema()
	tables := []source.SourceTableInfo{
		{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}},
		{Name: "public.orders", Schema: tableSchema, PrimaryKeys: []string{"id"}},
	}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Err: errors.New("source stream failed")}
	close(records)
	src := &announcingMultiTableSource{tables: tables, records: records}
	base := &fakeDestination{tableSchemas: map[string]*schema.TableSchema{
		"users": tableSchema,
	}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{},
		Source:      src,
		Destination: &directReplaceDestination{fakeDestination: base},
		Tables:      tables,
		TableDestNames: map[string]string{
			"public.users":  "users",
			"public.orders": "orders",
		},
	}

	err := (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatal("expected multi-table direct replace to fail")
	}
	require.Empty(t, base.dropCalls, "multi-table write errors may follow durable writes")
}

func TestReplaceStrategy_MultiTableSwapFailureCleansActiveStages(t *testing.T) {
	tableSchema := streamTestSchema()
	tableSchema.IncrementalKey = "id"
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema, PrimaryKeys: []string{"id"}}
	records := make(chan source.RecordBatchResult)
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	dest := &fakeDestination{swapErr: errors.New("swap failed")}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    dest,
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "users"},
	}

	err := (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatal("expected swap failure")
	}
	if len(dest.dropCalls) < 2 {
		t.Fatalf("expected raw and normalised stage cleanup, got %v", dest.dropCalls)
	}
	normalised := dest.prepareCalls[len(dest.prepareCalls)-1].Table
	if dest.dropCalls[len(dest.dropCalls)-1] != normalised {
		t.Fatalf("expected active normalised stage %q to be cleaned, got %v", normalised, dest.dropCalls)
	}
}

func TestAppendStrategy_WriterFailureBoundsNonclosingSource(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })
	job, src, base := minimalJob()
	records := make(chan source.RecordBatchResult)
	src.readCh = records
	job.Destination = &immediateFailStagedMergeDestination{fakeDestination: base}

	started := time.Now()
	err := (&AppendStrategy{}).Execute(context.Background(), job)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("append did not return promptly after writer failure: %v", err)
	}
	close(records)
}

type cancelAwareMultiTableSource struct {
	*announcingMultiTableSource
	done chan struct{}
}

func (s *cancelAwareMultiTableSource) ReadAll(ctx context.Context, _ source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	records := make(chan source.RecordBatchResult)
	go func() {
		defer close(s.done)
		defer close(records)
		for {
			select {
			case records <- source.RecordBatchResult{TableName: s.tables[0].Name}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return records, nil
}

type immediateFailMultiTableDestination struct {
	*directReplaceDestination
}

type ownedDirectReplaceDestination struct {
	*directReplaceDestination
	preparedToken string
	dropTokens    []string
	ownerChanged  bool
}

type uncertainPrepareOwnedDestination struct {
	*directReplaceDestination
	preparedToken string
	dropTokens    []string
}

func (d *uncertainPrepareOwnedDestination) PrepareTable(_ context.Context, opts destination.PrepareOptions) error {
	d.preparedToken = opts.OwnershipToken
	return errors.New("prepare response lost after target creation")
}

func (d *uncertainPrepareOwnedDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	return nil
}

type contextAwareDropDestination struct {
	*fakeDestination
	successfulDrops []string
}

type uncertainManagedStagingPrepareDestination struct {
	*contextAwareDropDestination
}

func (d *uncertainManagedStagingPrepareDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if err := d.contextAwareDropDestination.PrepareTable(ctx, opts); err != nil {
		return err
	}
	if opts.ExpiresAfter > 0 {
		return errors.New("managed staging prepare response lost after create")
	}
	return nil
}

func (d *contextAwareDropDestination) DropTable(ctx context.Context, table string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.successfulDrops = append(d.successfulDrops, table)
	return d.fakeDestination.DropTable(ctx, table)
}

func (d *ownedDirectReplaceDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.preparedToken = opts.OwnershipToken
	return d.directReplaceDestination.PrepareTable(ctx, opts)
}

func (d *ownedDirectReplaceDestination) WriteParallel(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
) error {
	d.ownerChanged = true
	return errors.New("write failed after replacement owner appeared")
}

func (d *ownedDirectReplaceDestination) DropTableIfOwned(_ context.Context, _ string, token string) error {
	d.dropTokens = append(d.dropTokens, token)
	if d.ownerChanged {
		return errors.New("ownership changed")
	}
	return nil
}

func TestDirectReplaceWriteFailurePreservesOwnedTarget(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	dest := &ownedDirectReplaceDestination{
		directReplaceDestination: &directReplaceDestination{fakeDestination: base},
	}
	job.Destination = dest

	err := (&ReplaceStrategy{}).Execute(context.Background(), job)
	if err == nil {
		t.Fatal("expected write failure")
	}
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "ownership does not prove a failed write was uncommitted")
	if len(base.dropCalls) != 0 {
		t.Fatalf("unconditional cleanup deleted a replacement owner: %v", base.dropCalls)
	}
}

func TestMultiTableDirectReplaceWriteFailurePreservesOwnedTarget(t *testing.T) {
	tableSchema := streamTestSchema()
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema}
	records := make(chan source.RecordBatchResult)
	close(records)
	base := &fakeDestination{}
	dest := &ownedDirectReplaceDestination{
		directReplaceDestination: &directReplaceDestination{fakeDestination: base},
	}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table},
		TableDestNames: map[string]string{table.Name: "users"},
	}

	require.Error(t, (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job))
	require.NotEmpty(t, dest.preparedToken)
	require.Empty(t, dest.dropTokens, "ownership does not prove a failed write was uncommitted")
	require.Empty(t, base.dropCalls, "unconditional cleanup must not delete the replacement owner")
}

func TestDirectReplaceUncertainPrepareFailureCleansRegisteredOwnedTarget(t *testing.T) {
	job, _, base := minimalJob()
	dest := &uncertainPrepareOwnedDestination{directReplaceDestination: &directReplaceDestination{fakeDestination: base}}
	job.Destination = dest

	require.ErrorContains(t, (&ReplaceStrategy{}).Execute(context.Background(), job), "prepare response lost")
	require.NotEmpty(t, dest.preparedToken)
	require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
	require.Empty(t, base.dropCalls)
}

func TestFailedOwnedDirectCleanupRequiresDestinationEnforcedOwnership(t *testing.T) {
	dest := &fakeDestination{}

	cleanupFailedOwnedDirectReplace(context.Background(), dest, "concurrent_target", true, "untrusted-token")

	require.Empty(t, dest.dropCalls)
}

func TestMultiTableDirectReplaceUncertainPrepareFailureCleansRegisteredOwnedTarget(t *testing.T) {
	table := source.SourceTableInfo{Name: "users", Schema: streamTestSchema()}
	records := make(chan source.RecordBatchResult)
	close(records)
	base := &fakeDestination{}
	dest := &uncertainPrepareOwnedDestination{directReplaceDestination: &directReplaceDestination{fakeDestination: base}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyReplace},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "users"},
	}

	require.ErrorContains(t, (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job), "prepare response lost")
	require.NotEmpty(t, dest.preparedToken)
	require.Equal(t, []string{dest.preparedToken}, dest.dropTokens)
	require.Empty(t, base.dropCalls)
}

func TestReplaceFailureUsesDetachedStagingCleanupContext(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	base.swapErr = errors.New("swap failed")
	dest := &contextAwareDropDestination{fakeDestination: base}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (&ReplaceStrategy{}).Execute(ctx, job)
	if err == nil {
		t.Fatal("expected replace failure")
	}
	if len(dest.successfulDrops) != 2 {
		t.Fatalf("staging cleanup inherited canceled caller context: %v", dest.successfulDrops)
	}
}

func TestMultiTableReplaceLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	table := source.SourceTableInfo{Name: "users", Schema: streamTestSchema()}
	records := make(chan source.RecordBatchResult)
	close(records)
	base := &fakeDestination{}
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	job := &MultiTableIngestionJob{
		Config:      &config.IngestConfig{IncrementalStrategy: config.StrategyReplace},
		Source:      &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records},
		Destination: dest, Tables: []source.SourceTableInfo{table}, TableDestNames: map[string]string{table.Name: "users"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&ReplaceStrategy{}).ExecuteMultiTable(ctx, job), "prepare response lost")
	require.Len(t, dest.successfulDrops, 1)
	require.Equal(t, base.prepareCalls[0].Table, dest.successfulDrops[0])
}

func TestReplaceLostStagingPrepareUsesDetachedCleanup(t *testing.T) {
	job, _, base := minimalJob()
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	job.Destination = dest
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorContains(t, (&ReplaceStrategy{}).Execute(ctx, job), "prepare response lost")
	require.Len(t, base.prepareCalls, 1)
	require.Equal(t, []string{base.prepareCalls[0].Table}, dest.successfulDrops)
}

func TestDeduplicateStagingLostNormalisedPrepareCleansBothStagesDetached(t *testing.T) {
	base := &fakeDestination{}
	dest := &uncertainManagedStagingPrepareDestination{contextAwareDropDestination: &contextAwareDropDestination{fakeDestination: base}}
	rawTable := "lake.events_raw"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := deduplicateStaging(ctx, dest, rawTable, "lake.events", "", "", streamTestSchema(), []string{"id"}, "", nil, false)
	require.ErrorContains(t, err, "prepare response lost")
	require.Len(t, base.prepareCalls, 1)
	require.ElementsMatch(t, []string{rawTable, base.prepareCalls[0].Table}, dest.successfulDrops)
}

func (d *immediateFailMultiTableDestination) WriteParallel(
	context.Context,
	<-chan source.RecordBatchResult,
	destination.WriteOptions,
) error {
	return errors.New("writer failed")
}

func TestReplaceStrategy_MultiTableWriterFailureCancelsSource(t *testing.T) {
	tableSchema := streamTestSchema()
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema}
	done := make(chan struct{})
	src := &cancelAwareMultiTableSource{
		announcingMultiTableSource: &announcingMultiTableSource{tables: []source.SourceTableInfo{table}},
		done:                       done,
	}
	base := &fakeDestination{}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    &immediateFailMultiTableDestination{directReplaceDestination: &directReplaceDestination{fakeDestination: base}},
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "users"},
	}

	err := (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatal("expected writer failure")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("source producer remained blocked after writer failure")
	}
}

func TestReplaceStrategy_MultiTableWriterFailureBoundsNonclosingSourceDrain(t *testing.T) {
	previousTimeout := canceledSourceDrainTimeout
	canceledSourceDrainTimeout = 20 * time.Millisecond
	t.Cleanup(func() { canceledSourceDrainTimeout = previousTimeout })

	tableSchema := streamTestSchema()
	table := source.SourceTableInfo{Name: "public.users", Schema: tableSchema}
	records := make(chan source.RecordBatchResult)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	base := &fakeDestination{}
	job := &MultiTableIngestionJob{
		Config:         &config.IngestConfig{},
		Source:         src,
		Destination:    &immediateFailMultiTableDestination{directReplaceDestination: &directReplaceDestination{fakeDestination: base}},
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "users"},
	}

	started := time.Now()
	err := (&ReplaceStrategy{}).ExecuteMultiTable(context.Background(), job)
	if err == nil {
		t.Fatal("expected writer failure")
	}
	if time.Since(started) > time.Second {
		t.Fatal("replace waited indefinitely for a nonclosing source")
	}
	close(records)
}

func TestReplaceStrategy_Execute_UsesDestinationReplaceStagingPolicy(t *testing.T) {
	job, src, dest := minimalJob()
	provider := &fakeReplaceStagingPolicyProvider{
		fakeDestination: dest,
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement: destination.ReplaceStagingTargetSchema,
		},
	}
	job.Destination = provider
	job.Config.PrimaryKeys = nil
	job.Schema.PrimaryKeys = nil
	job.SourceSchema.PrimaryKeys = nil
	src.primaryKeys = nil
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 1 {
		t.Fatalf("expected 1 PrepareTable call, got %d", len(dest.prepareCalls))
	}
	if stagingTable := dest.prepareCalls[0].Table; !strings.HasPrefix(stagingTable, "ds.tbl_staging_") {
		t.Fatalf("PrepareTable.Table = %q, want target-schema staging prefix %q", stagingTable, "ds.tbl_staging_")
	}
}

func TestReplaceStrategy_Execute_DedupsWhenUniqueSourcePrimaryKeysExcluded(t *testing.T) {
	job, src, dest := minimalJob()
	src.primaryKeys = []string{"tenant_id", "id"}
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()
	job.SourceSchema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected dedup path with 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 0 {
		t.Fatalf("raw staging table should be PK-free when part of the source PK was excluded, got %v", got)
	}
	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected MergeTable call for dedup path, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_DedupsWhenPrimaryKeyRenameCollapses(t *testing.T) {
	job, src, dest := minimalJob()
	src.primaryKeys = []string{"userId", "UserID"}
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()
	job.Config.PrimaryKeys = []string{"user_id"}
	job.SourceSchema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "userId", DataType: schema.TypeInt64, Nullable: false},
			{Name: "UserID", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"userId", "UserID"},
	}
	job.Schema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "user_id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"user_id"},
	}
	job.ColumnRenamer = transformer.NewColumnRenamer(map[string]string{
		"userId": "user_id",
		"UserID": "user_id",
	})

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected dedup path with 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 0 {
		t.Fatalf("raw staging table should be PK-free when PK renames collapse, got %v", got)
	}
	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected MergeTable call for dedup path, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_DedupsWhenNonPrimaryKeyRenamesToPrimaryKey(t *testing.T) {
	job, src, dest := minimalJob()
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()
	job.SourceSchema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "ID", DataType: schema.TypeInt64, Nullable: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	job.Schema = &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	job.ColumnRenamer = transformer.NewColumnRenamer(map[string]string{
		"ID": "id",
	})

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected dedup path with 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 0 {
		t.Fatalf("raw staging table should be PK-free when a non-PK renames to a PK, got %v", got)
	}
	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected MergeTable call for dedup path, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_DedupsWhenPrimaryKeyIsMasked(t *testing.T) {
	job, src, dest := minimalJob()
	src.primaryKeysUnique = true
	src.readCh = mustClosedRecords()

	masker, err := transformer.NewColumnMasker([]string{"id:redact"})
	if err != nil {
		t.Fatalf("NewColumnMasker returned error: %v", err)
	}
	job.ColumnMasker = masker

	strat := &ReplaceStrategy{}
	if err := strat.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(dest.prepareCalls) != 2 {
		t.Fatalf("expected dedup path with 2 PrepareTable calls, got %d", len(dest.prepareCalls))
	}
	if got := dest.prepareCalls[0].PrimaryKeys; len(got) != 0 {
		t.Fatalf("raw staging table should be PK-free when a PK is masked, got %v", got)
	}
	if len(dest.mergeCalls) != 1 {
		t.Fatalf("expected MergeTable call for dedup path, got %d", len(dest.mergeCalls))
	}
}

func TestReplaceStrategy_Execute_PassesFullRefreshToRead(t *testing.T) {
	job, src, _ := minimalJob()
	job.Config.IncrementalStrategy = config.StrategyReplace
	job.Config.FullRefresh = true
	job.Config.CDCSlotSuffix = "current-destination-slot"
	job.Config.CDCLegacySlotSuffix = "legacy-destination-slot"
	src.readCh = mustClosedRecords()

	strat := &ReplaceStrategy{}
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

func TestReplaceStrategy_ExecuteMultiTable_FullRefreshUsesLeasedSlotSuffix(t *testing.T) {
	table := newTableInfo("public.orders")
	records := make(chan source.RecordBatchResult)
	close(records)
	src := &announcingMultiTableSource{tables: []source.SourceTableInfo{table}, records: records}
	job := &MultiTableIngestionJob{
		Config: &config.IngestConfig{
			FullRefresh:         true,
			CDCSlotSuffix:       "current-destination-slot",
			CDCLegacySlotSuffix: "legacy-destination-slot",
		},
		Source:         src,
		Destination:    &fakeDestination{},
		Tables:         src.tables,
		TableDestNames: map[string]string{table.Name: "landing.orders"},
	}

	require.NoError(t, (&ReplaceStrategy{}).ExecuteMultiTable(t.Context(), job))
	require.True(t, src.readOpts.FullRefresh)
	require.Equal(t, job.Config.CDCSlotSuffix, src.readOpts.CDCSlotSuffix)
	require.Equal(t, job.Config.CDCLegacySlotSuffix, src.readOpts.CDCLegacySlotSuffix)
	require.Empty(t, src.readOpts.CDCResumeLSNs, "full refresh must not select a previous or legacy slot")
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

type releaseCountingRecordBatch struct {
	arrow.RecordBatch
	releases *atomic.Int64
}

func (b *releaseCountingRecordBatch) Release() {
	b.releases.Add(1)
	b.RecordBatch.Release()
}

type cancellationJoinSourceTable struct {
	*fakeSourceTable
	batches []arrow.RecordBatch
	done    chan struct{}
}

func (t *cancellationJoinSourceTable) Read(ctx context.Context, _ source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	records := make(chan source.RecordBatchResult, 2)
	go func() {
		defer close(records)
		defer close(t.done)
		for i, batch := range t.batches {
			select {
			case records <- source.RecordBatchResult{Batch: batch}:
			case <-ctx.Done():
				batch.Release()
				for _, remaining := range t.batches[i+1:] {
					remaining.Release()
				}
				return
			}
		}
	}()
	return records, nil
}

type nonDrainingFailureDestination struct {
	*fakeDestination
}

func (d *nonDrainingFailureDestination) WriteParallel(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error {
	return errors.New("write failed without draining")
}

func TestReplaceStrategy_Execute_WriteFailureCancelsDrainsAndJoinsProducer(t *testing.T) {
	job, baseSource, baseDestination := minimalJob()
	const batchCount = 32

	releaseCounts := make([]*atomic.Int64, 0, batchCount)
	batches := make([]arrow.RecordBatch, 0, batchCount)
	for i := 0; i < batchCount; i++ {
		count := &atomic.Int64{}
		releaseCounts = append(releaseCounts, count)
		batches = append(batches, &releaseCountingRecordBatch{
			RecordBatch: int64RecordBatch(t, "id", []int64{int64(i)}, nil),
			releases:    count,
		})
	}
	src := &cancellationJoinSourceTable{
		fakeSourceTable: baseSource,
		batches:         batches,
		done:            make(chan struct{}),
	}
	job.Table = src
	job.Destination = &nonDrainingFailureDestination{fakeDestination: baseDestination}

	result := make(chan error, 1)
	go func() {
		result <- (&ReplaceStrategy{}).Execute(context.Background(), job)
	}()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "failed to write data") {
			t.Fatalf("Execute() error = %v, want write failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() deadlocked after the destination stopped consuming")
	}

	select {
	case <-src.done:
	default:
		t.Fatal("Execute() returned before the source producer exited")
	}
	for i, count := range releaseCounts {
		if got := count.Load(); got != 1 {
			t.Fatalf("batch %d release count = %d, want exactly 1", i, got)
		}
	}
}

func TestReplaceStrategyWriteFailureReturnsWhenCanceledProducerNeverCloses(t *testing.T) {
	job, src, baseDestination := minimalJob()
	var releases atomic.Int64
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: &releaseCountingRecordBatch{
		RecordBatch: int64RecordBatch(t, "id", []int64{1}, nil),
		releases:    &releases,
	}}
	src.readCh = records
	job.Destination = &nonDrainingFailureDestination{fakeDestination: baseDestination}

	started := time.Now()
	err := (&ReplaceStrategy{}).Execute(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "failed to write data") {
		t.Fatalf("Execute() error = %v, want write failure", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Execute() waited %s for a canceled producer that never closed", elapsed)
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("batch release count = %d, want 1", got)
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
