package strategy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/annotation"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSourceTable struct {
	mu sync.Mutex

	name              string
	primaryKeys       []string
	primaryKeysUnique bool
	incrementalKey    string
	strategy          config.IncrementalStrategy
	hasKnownSchema    bool
	tableSchema       *schema.TableSchema

	readCalled bool
	readOpts   source.ReadOptions

	readCh  <-chan source.RecordBatchResult
	readErr error
}

func TestMultiTableColumnRenamingRewritesCDCMarkers(t *testing.T) {
	ctx := context.Background()
	job := &MultiTableIngestionJob{
		ColumnRenamers: map[string]*transformer.ColumnRenamer{
			"public.items": transformer.NewColumnRenamer(map[string]string{"configData": "config_data"}),
		},
	}
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{TableName: "public.items", Batch: cdcRenameRecordBatch(t, "configData", nil, `["configData"]`)}
	close(records)

	result := <-job.ApplyBatchTransformation(ctx, records)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Batch)
	defer result.Batch.Release()
	require.Equal(t, "config_data", result.Batch.Schema().Field(0).Name)
	require.Equal(t, `["config_data"]`, result.Batch.Column(1).(*array.String).Value(0))
}

func TestMultiTableDynamicAnnouncementInstallsColumnRenamer(t *testing.T) {
	ctx := context.Background()
	job := &MultiTableIngestionJob{
		Destination: &fakeDestination{},
		NormalizeTableInfo: func(_ context.Context, table source.SourceTableInfo, _ string) (source.SourceTableInfo, *transformer.ColumnRenamer, error) {
			table.Schema = &schema.TableSchema{Columns: []schema.Column{
				{Name: "config_data", DataType: schema.TypeString},
				{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
			}}
			return table, transformer.NewColumnRenamer(map[string]string{"configData": "config_data"}), nil
		},
	}
	rawInfo := source.SourceTableInfo{Name: "public.items", Schema: &schema.TableSchema{Columns: []schema.Column{
		{Name: "configData", DataType: schema.TypeString},
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
	}}}
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{TableName: rawInfo.Name, TableInfo: &rawInfo}
	records <- source.RecordBatchResult{TableName: rawInfo.Name, Batch: cdcRenameRecordBatch(t, "configData", nil, `["configData"]`)}
	close(records)

	out := job.ApplyBatchTransformation(ctx, records)
	announcement := <-out
	require.NoError(t, announcement.Err)
	require.Equal(t, "config_data", announcement.TableInfo.Schema.Columns[0].Name)
	result := <-out
	require.NoError(t, result.Err)
	require.Equal(t, "config_data", result.Batch.Schema().Field(0).Name)
	require.Equal(t, `["config_data"]`, result.Batch.Column(1).(*array.String).Value(0))
	result.Batch.Release()
}

func cdcRenameRecordBatch(t *testing.T, dataColumn string, value *string, marker string) arrow.RecordBatch {
	t.Helper()
	dataBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	if value == nil {
		dataBuilder.AppendNull()
	} else {
		dataBuilder.Append(*value)
	}
	markerBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	markerBuilder.Append(marker)
	data := dataBuilder.NewArray()
	markers := markerBuilder.NewArray()
	dataBuilder.Release()
	markerBuilder.Release()
	record := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: dataColumn, Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: destination.CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String},
	}, nil), []arrow.Array{data, markers}, 1)
	data.Release()
	markers.Release()
	return record
}

func (t *fakeSourceTable) Name() string {
	return t.name
}

func (t *fakeSourceTable) PrimaryKeys() []string {
	return t.primaryKeys
}

func (t *fakeSourceTable) PrimaryKeysUnique() bool {
	return t.primaryKeysUnique
}

func (t *fakeSourceTable) IncrementalKey() string {
	return t.incrementalKey
}

func (t *fakeSourceTable) Strategy() config.IncrementalStrategy {
	return t.strategy
}

func (t *fakeSourceTable) HasKnownSchema() bool {
	return t.hasKnownSchema
}

func (t *fakeSourceTable) GetSchema(ctx context.Context) (*schema.TableSchema, error) {
	return t.tableSchema, nil
}

func (t *fakeSourceTable) Read(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.readCalled = true
	t.readOpts = opts
	return t.readCh, t.readErr
}

type execCall struct {
	ctx context.Context
	sql string
}

type fakeDestination struct {
	mu sync.Mutex

	calls []string

	prepareCalls  []destination.PrepareOptions
	writeCalls    []destination.WriteOptions
	swapCalls     [][2]string
	mergeCalls    []destination.MergeOptions
	diCalls       []destination.DeleteInsertOptions
	dropCalls     []string
	execCalls     []execCall
	truncateCalls []string
	waitCalls     []struct {
		Table        string
		ExpectedRows int64
	}

	prepareErrByTable map[string]error
	writeErr          error
	swapErr           error
	mergeErr          error
	deleteInsertErr   error
	truncateErr       error
	waitErr           error
	dropErrByTable    map[string]error
	noDeleteInsert    bool
	evolutionWarnings []string
	evolutionErr      error

	tableSchemas map[string]*schema.TableSchema
}

func (d *fakeDestination) Schemes() []string                             { return nil }
func (d *fakeDestination) Connect(ctx context.Context, uri string) error { return nil }
func (d *fakeDestination) Close(ctx context.Context) error               { return nil }
func (d *fakeDestination) Exec(ctx context.Context, sql string, args ...interface{}) error {
	d.mu.Lock()
	d.execCalls = append(d.execCalls, execCall{ctx: ctx, sql: sql})
	d.mu.Unlock()
	return nil
}

func (d *fakeDestination) BeginTransaction(ctx context.Context) (destination.Transaction, error) {
	return nil, errors.New("not implemented")
}

// ApplySchemaEvolution turns the abstract plan into DDL and runs it via Exec,
// mirroring how real destinations implement schemaevolution.SchemaEvolver.
func (d *fakeDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if comparison == nil {
		return nil, nil
	}
	if d.evolutionWarnings != nil || d.evolutionErr != nil {
		return d.evolutionWarnings, d.evolutionErr
	}
	for _, change := range comparison.Changes {
		if err := d.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT", table, change.ColumnName)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (d *fakeDestination) SupportsColumnTypeChanges() bool    { return true }
func (d *fakeDestination) SupportsReplaceStrategy() bool      { return true }
func (d *fakeDestination) SupportsAppendStrategy() bool       { return true }
func (d *fakeDestination) SupportsMergeStrategy() bool        { return true }
func (d *fakeDestination) SupportsDeleteInsertStrategy() bool { return !d.noDeleteInsert }
func (d *fakeDestination) SupportsSCD2Strategy() bool         { return true }
func (d *fakeDestination) SupportsAtomicSwap() bool           { return true }
func (d *fakeDestination) GetScheme() string                  { return "fake" }

func (d *fakeDestination) GetTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if d.tableSchemas != nil {
		return d.tableSchemas[table], nil
	}
	return nil, nil
}

func (d *fakeDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *fakeDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "PrepareTable")
	d.prepareCalls = append(d.prepareCalls, opts)
	err := error(nil)
	if d.prepareErrByTable != nil {
		err = d.prepareErrByTable[opts.Table]
	}
	d.mu.Unlock()
	return err
}

func (d *fakeDestination) WriteParallel(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "WriteParallel")
	d.writeCalls = append(d.writeCalls, opts)
	writeErr := d.writeErr
	d.mu.Unlock()

	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return writeErr
}

func (d *fakeDestination) SwapTable(ctx context.Context, opts destination.SwapOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "SwapTable")
	d.swapCalls = append(d.swapCalls, [2]string{opts.StagingTable, opts.TargetTable})
	swapErr := d.swapErr
	d.mu.Unlock()
	return swapErr
}

func (d *fakeDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "MergeTable")
	d.mergeCalls = append(d.mergeCalls, opts)
	mergeErr := d.mergeErr
	d.mu.Unlock()
	return mergeErr
}

func (d *fakeDestination) DeleteInsertTable(ctx context.Context, opts destination.DeleteInsertOptions) error {
	d.mu.Lock()
	d.calls = append(d.calls, "DeleteInsertTable")
	d.diCalls = append(d.diCalls, opts)
	diErr := d.deleteInsertErr
	d.mu.Unlock()
	return diErr
}

func (d *fakeDestination) SCD2Table(ctx context.Context, opts destination.SCD2Options) error {
	d.mu.Lock()
	d.calls = append(d.calls, "SCD2Table")
	d.mu.Unlock()
	return nil
}

func (d *fakeDestination) DropTable(ctx context.Context, table string) error {
	d.mu.Lock()
	d.calls = append(d.calls, "DropTable")
	d.dropCalls = append(d.dropCalls, table)
	err := error(nil)
	if d.dropErrByTable != nil {
		err = d.dropErrByTable[table]
	}
	d.mu.Unlock()
	return err
}

type truncateCapableDestination struct {
	*fakeDestination
}

func (d *truncateCapableDestination) TruncateTable(ctx context.Context, table string) error {
	d.mu.Lock()
	d.calls = append(d.calls, "TruncateTable")
	d.truncateCalls = append(d.truncateCalls, table)
	truncateErr := d.truncateErr
	d.mu.Unlock()
	return truncateErr
}

func (d *fakeDestination) WaitForExactRowCount(ctx context.Context, table string, expectedRows int64) error {
	d.mu.Lock()
	d.waitCalls = append(d.waitCalls, struct {
		Table        string
		ExpectedRows int64
	}{Table: table, ExpectedRows: expectedRows})
	waitErr := d.waitErr
	d.mu.Unlock()
	return waitErr
}

type fakeReplaceStagingPolicyProvider struct {
	*fakeDestination

	policy destination.ReplaceStagingPolicy
}

func (d *fakeReplaceStagingPolicyProvider) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return d.policy
}

func int64RecordBatch(t *testing.T, colName string, values []int64, nullAt map[int]bool) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	fields := []arrow.Field{{Name: colName, Type: arrow.PrimitiveTypes.Int64, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewInt64Builder(pool)
	defer b.Release()

	for i, v := range values {
		if nullAt != nil && nullAt[i] {
			b.AppendNull()
			continue
		}
		b.Append(v)
	}
	arr := b.NewArray()
	defer arr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
	return rec
}

func timestampRecordBatch(t *testing.T, colName string, values []time.Time) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	dt := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	fields := []arrow.Field{{Name: colName, Type: dt, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewTimestampBuilder(pool, dt)
	defer b.Release()

	for _, v := range values {
		b.Append(arrow.Timestamp(v.UnixMicro()))
	}
	arr := b.NewArray()
	defer arr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
	return rec
}

func stringRecordBatch(t *testing.T, colName string, values []string) arrow.RecordBatch {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	fields := []arrow.Field{{Name: colName, Type: arrow.BinaryTypes.String, Nullable: true}}
	schema := arrow.NewSchema(fields, nil)

	b := array.NewStringBuilder(pool)
	defer b.Release()

	for _, v := range values {
		b.Append(v)
	}
	arr := b.NewArray()
	defer arr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(values)))
	return rec
}

func intStringRecordBatch(t *testing.T, idName string, ids []int64, nameName string, names []string) arrow.RecordBatch {
	t.Helper()

	require.Equal(t, len(ids), len(names))

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { pool.AssertSize(t, 0) })

	fields := []arrow.Field{
		{Name: idName, Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: nameName, Type: arrow.BinaryTypes.String, Nullable: true},
	}
	schema := arrow.NewSchema(fields, nil)

	idBuilder := array.NewInt64Builder(pool)
	defer idBuilder.Release()
	for _, v := range ids {
		idBuilder.Append(v)
	}
	idArr := idBuilder.NewArray()
	defer idArr.Release()

	nameBuilder := array.NewStringBuilder(pool)
	defer nameBuilder.Release()
	for _, v := range names {
		nameBuilder.Append(v)
	}
	nameArr := nameBuilder.NewArray()
	defer nameArr.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{idArr, nameArr}, int64(len(ids)))
	return rec
}

func mustClosedRecords(ch ...source.RecordBatchResult) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult, len(ch))
	for _, v := range ch {
		out <- v
	}
	close(out)
	return out
}

func minimalJob() (*IngestionJob, *fakeSourceTable, *fakeDestination) {
	cfg := &config.IngestConfig{
		SourceTable:        "src_table",
		DestTable:          "ds.tbl",
		ExtractParallelism: 2,
		PageSize:           1000,
		SQLLimit:           0,
		SQLExcludeColumns:  nil,
		IncrementalKey:     "id",
		PrimaryKeys:        []string{"id"},
	}
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	src := &fakeSourceTable{
		name:           "src_table",
		primaryKeys:    []string{"id"},
		incrementalKey: "id",
		strategy:       config.StrategyReplace,
		hasKnownSchema: true,
		tableSchema:    tableSchema,
	}
	dest := &fakeDestination{}
	job := &IngestionJob{
		Config:       cfg,
		Table:        src,
		Destination:  dest,
		Schema:       tableSchema,
		SourceSchema: tableSchema,
	}
	return job, src, dest
}

func TestApplyEvolution_AnnotatesWithEvolveStep(t *testing.T) {
	job, _, dest := minimalJob()
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: "ds.tbl",
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col"},
			},
		},
	}

	err := job.ApplyEvolution(context.Background())
	require.NoError(t, err)
	require.Len(t, dest.execCalls, 1)

	got := annotation.Prepend(dest.execCalls[0].ctx, "X")
	assert.True(t, strings.Contains(got, `"ingestr_step":"evolve"`), "missing ingestr_step=evolve: %s", got)
	assert.True(t, strings.Contains(got, `"type":"ingestr_load"`), "missing type=ingestr_load: %s", got)
}

func TestApplyEvolution_IsIdempotent(t *testing.T) {
	job, _, dest := minimalJob()
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: "ds.tbl",
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col"},
			},
		},
	}

	require.NoError(t, job.ApplyEvolution(context.Background()))
	require.Len(t, dest.execCalls, 1, "first apply should execute the change")

	// A second apply of the same plan must not re-issue the DDL.
	require.NoError(t, job.ApplyEvolution(context.Background()))
	require.Len(t, dest.execCalls, 1, "second apply should be a no-op")
}

func TestApplyEvolution_DoesNotPrintWarningsOnError(t *testing.T) {
	job, _, dest := minimalJob()
	dest.evolutionWarnings = []string{"column \"age\" type change skipped"}
	dest.evolutionErr = errors.New("apply failed")
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{
		Table: "ds.tbl",
		Comparison: &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{
				{Type: schemaevolution.ChangeAddColumn, ColumnName: "new_col"},
			},
		},
	}

	output := captureStdout(t, func() {
		require.ErrorIs(t, job.ApplyEvolution(context.Background()), dest.evolutionErr)
	})

	assert.NotContains(t, output, "Warning:")
	assert.NotNil(t, job.EvolutionPlan.Comparison, "failed plans should remain retryable")
}

func TestApplyEvolution_NoMigrationDoesNothing(t *testing.T) {
	job, _, dest := minimalJob()
	// Nil plan
	require.NoError(t, job.ApplyEvolution(context.Background()))
	assert.Empty(t, dest.execCalls)

	// Empty plan
	job.EvolutionPlan = &schemaevolution.EvolutionPlan{Comparison: &schemaevolution.SchemaComparison{}}
	require.NoError(t, job.ApplyEvolution(context.Background()))
	assert.Empty(t, dest.execCalls)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = r.Close()
		_ = w.Close()
	}()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestDeleteInsertStrategy_UsesIncrementalKeyFromLaterBatch(t *testing.T) {
	job, src, dest := minimalJob()
	strategy := &DeleteInsertStrategy{}

	batchWithoutKey := stringRecordBatch(t, "name", []string{"alpha", "beta"})
	batchWithKey := intStringRecordBatch(t, "id", []int64{10, 5}, "name", []string{"gamma", "delta"})

	src.readCh = mustClosedRecords(
		source.RecordBatchResult{Batch: batchWithoutKey},
		source.RecordBatchResult{Batch: batchWithKey},
	)

	err := strategy.Execute(context.Background(), job)
	require.NoError(t, err)
	require.Len(t, dest.diCalls, 1)
	assert.Equal(t, int64(5), dest.diCalls[0].IntervalStart)
	assert.Equal(t, int64(10), dest.diCalls[0].IntervalEnd)
}
