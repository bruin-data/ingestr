package strategy

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func streamSchemaChangeFixture(destSchema *schema.TableSchema) (*flushLoop, *fakeDestination, *streamTableState) {
	dest := &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"dest_items": destSchema},
	}
	cfg := &config.IngestConfig{NoLoadTimestamp: true}
	st := &streamTableState{
		destTable:    "dest_items",
		stagingTable: "stg_items",
		schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "status", DataType: schema.TypeString},
		}},
		primaryKeys: []string{"id"},
	}
	loop := newFlushLoop(dest, cfg, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.items": st})
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, dest, destTable, newSchema, cfg)
	}
	return loop, dest, st
}

// A re-announcement carrying a new column must evolve the destination table,
// update the tracked schema, and recreate the staging table in the new shape.
func TestFlushLoopRefreshEvolvesDestinationOnNewColumn(t *testing.T) {
	loop, dest, st := streamSchemaChangeFixture(&schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "status", DataType: schema.TypeString},
	}})

	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "status", DataType: schema.TypeString},
		{Name: "priority", DataType: schema.TypeInt64},
	}}
	err := loop.ensureTable(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: newSchema, PrimaryKeys: []string{"id"}})
	require.NoError(t, err)

	require.Len(t, dest.execCalls, 1)
	assert.Contains(t, dest.execCalls[0].sql, "ALTER TABLE dest_items ADD COLUMN priority")

	require.Len(t, dest.prepareCalls, 1)
	staging := dest.prepareCalls[0]
	assert.Equal(t, "stg_items", staging.Table)
	assert.True(t, staging.DropFirst)
	assert.Len(t, staging.Schema.Columns, 3)

	assert.Len(t, st.schema.Columns, 3)
	assert.Equal(t, "priority", st.schema.Columns[2].Name)
}

func TestStreamingRefreshRetainsContractFinalSchemaAndAlignsNewBatches(t *testing.T) {
	finalSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeInt64, Nullable: true},
	}}
	newSourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	comparison := &schemaevolution.SchemaComparison{HasChanges: true, Changes: []schemaevolution.SchemaChange{{
		Type: schemaevolution.ChangeWidenType, ColumnName: "value",
	}}}
	dest := &fakeDestination{}
	st := &streamTableState{destTable: "dest_items", stagingTable: "stg_items", schema: finalSchema, primaryKeys: []string{"id"}}
	loop := newFlushLoop(dest, &config.IngestConfig{NoLoadTimestamp: true, SchemaContract: "discard_value"}, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.items": st})
	loop.evolveTablePlan = func(context.Context, string, *schema.TableSchema) (*schemaevolution.EvolutionPlan, error) {
		return &schemaevolution.EvolutionPlan{TransformComparison: comparison, FinalSchema: finalSchema}, nil
	}

	require.NoError(t, loop.refreshTableSchema(context.Background(), source.SourceTableInfo{
		Name: "public.items", Schema: newSourceSchema, PrimaryKeys: []string{"id"},
	}, st))
	require.Equal(t, schema.TypeInt64, st.schema.Columns[1].DataType)
	require.NotNil(t, st.batchTransformer)
	require.NotNil(t, st.schemaAligner)
	require.Equal(t, schema.TypeInt64, dest.prepareCalls[0].Schema.Columns[1].DataType)

	ids := array.NewInt64Builder(memory.DefaultAllocator)
	values := array.NewStringBuilder(memory.DefaultAllocator)
	ids.Append(1)
	values.Append("invalid")
	idArray, valueArray := ids.NewArray(), values.NewArray()
	ids.Release()
	values.Release()
	batch := array.NewRecordBatch(newSourceSchema.ToArrowSchema(), []arrow.Array{idArray, valueArray}, 1)
	idArray.Release()
	valueArray.Release()
	defer batch.Release()
	contracted, err := st.batchTransformer.Transform(context.Background(), batch)
	require.NoError(t, err)
	defer contracted.Release()
	aligned, err := st.schemaAligner.Transform(contracted)
	require.NoError(t, err)
	defer aligned.Release()
	require.True(t, arrow.TypeEqual(arrow.PrimitiveTypes.Int64, aligned.Column(1).DataType()))
	require.True(t, aligned.Column(1).IsNull(0))
}

func TestDynamicStreamingTransformationsPreserveRefreshedBatchShape(t *testing.T) {
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	refreshed := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "segment", DataType: schema.TypeString},
	}}
	newBatch := func() arrow.RecordBatch {
		ids := array.NewInt64Builder(memory.DefaultAllocator)
		segments := array.NewStringBuilder(memory.DefaultAllocator)
		ids.Append(1)
		segments.Append("segment-1")
		idValues, segmentValues := ids.NewArray(), segments.NewArray()
		ids.Release()
		segments.Release()
		batch := array.NewRecordBatch(refreshed.ToArrowSchema(), []arrow.Array{idValues, segmentValues}, 1)
		idValues.Release()
		segmentValues.Release()
		return batch
	}

	t.Run("single table", func(t *testing.T) {
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{Batch: newBatch()}
		close(records)
		job := &IngestionJob{
			Config:        &config.IngestConfig{},
			SchemaAligner: transformer.NewSafeTypeCaster(initial.ToArrowSchema()),
		}
		out, err := job.applyBatchTransformation(t.Context(), records, true)
		require.NoError(t, err)
		result := <-out
		require.NoError(t, result.Err)
		require.Equal(t, int64(2), result.Batch.NumCols())
		require.Equal(t, "segment", result.Batch.ColumnName(1))
		result.Batch.Release()
	})

	t.Run("multi table", func(t *testing.T) {
		records := make(chan source.RecordBatchResult, 1)
		records <- source.RecordBatchResult{TableName: "public.items", Batch: newBatch()}
		close(records)
		job := &MultiTableIngestionJob{
			Config: &config.IngestConfig{},
			Tables: []source.SourceTableInfo{{Name: "public.items", Schema: initial}},
		}
		out, err := job.applyBatchTransformation(t.Context(), records, true)
		require.NoError(t, err)
		result := <-out
		require.NoError(t, result.Err)
		require.Equal(t, int64(2), result.Batch.NumCols())
		require.Equal(t, "segment", result.Batch.ColumnName(1))
		result.Batch.Release()
	})
}

func TestStreamingRefreshRejectsUnsupportedDecimalBeforeMutation(t *testing.T) {
	current := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 38, Scale: 2,
	}}}
	dest := &fakeDestination{}
	st := &streamTableState{destTable: "dest_items", stagingTable: "stg_items", schema: current}
	loop := newFlushLoop(dest, &config.IngestConfig{NoLoadTimestamp: true}, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.items": st})
	plannerCalled := false
	loop.evolveTablePlan = func(context.Context, string, *schema.TableSchema) (*schemaevolution.EvolutionPlan, error) {
		plannerCalled = true
		return nil, nil
	}
	invalid := &schema.TableSchema{Columns: []schema.Column{{
		Name: "amount", DataType: schema.TypeDecimal, Precision: 50, Scale: 2,
	}}}

	err := loop.refreshTableSchema(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: invalid}, st)
	require.ErrorContains(t, err, "maximum supported precision is 38")
	require.ErrorContains(t, err, "public.items")
	require.False(t, plannerCalled)
	require.Empty(t, dest.prepareCalls)
	require.Equal(t, 38, st.schema.Columns[0].Precision)
}

// A re-announcement with an unchanged schema (e.g. after a new-table rebuild)
// must stay a no-op.
func TestFlushLoopRefreshIgnoresUnchangedSchema(t *testing.T) {
	loop, dest, st := streamSchemaChangeFixture(&schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "status", DataType: schema.TypeString},
	}})

	sameSchema := &schema.TableSchema{Columns: append([]schema.Column{}, st.schema.Columns...)}
	err := loop.ensureTable(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: sameSchema, PrimaryKeys: []string{"id"}})
	require.NoError(t, err)

	assert.Empty(t, dest.execCalls)
	assert.Empty(t, dest.prepareCalls)
}

// A type change flows through the same path: the destination applies it (the
// fake supports type changes) and the staging table is rebuilt with the new
// type.
func TestFlushLoopRefreshHandlesTypeChange(t *testing.T) {
	loop, dest, st := streamSchemaChangeFixture(&schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "status", DataType: schema.TypeString},
	}})

	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "status", DataType: schema.TypeString},
	}}
	err := loop.ensureTable(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: newSchema, PrimaryKeys: []string{"id"}})
	require.NoError(t, err)

	require.Len(t, dest.execCalls, 1)
	assert.True(t, strings.Contains(dest.execCalls[0].sql, "id"))
	assert.Equal(t, schema.TypeInt64, st.schema.Columns[0].DataType)

	require.Len(t, dest.prepareCalls, 1)
	assert.Equal(t, schema.TypeInt64, dest.prepareCalls[0].Schema.Columns[0].DataType)
}

// The freeze contract must reject mid-stream schema changes instead of
// silently evolving the destination.
func TestFlushLoopRefreshHonorsFreezeContract(t *testing.T) {
	dest := &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"dest_items": {Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}}},
	}
	cfg := &config.IngestConfig{NoLoadTimestamp: true, SchemaContract: "freeze"}
	st := &streamTableState{
		destTable: "dest_items",
		schema:    &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}},
	}
	loop := newFlushLoop(dest, cfg, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.items": st})
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, dest, destTable, newSchema, cfg)
	}

	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "extra", DataType: schema.TypeString},
	}}
	err := loop.ensureTable(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: newSchema, PrimaryKeys: []string{"id"}})
	require.Error(t, err)
	assert.Empty(t, dest.execCalls)
}

func TestEvolveDestinationTableDoesNotRelaxPrimaryKeyNullability(t *testing.T) {
	dest := &fakeDestination{tableSchemas: map[string]*schema.TableSchema{"dest_items": {Columns: []schema.Column{{
		Name: "ID", DataType: schema.TypeInt64, Nullable: false,
	}}}}}
	sourceSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}},
		PrimaryKeys: []string{"id"},
	}

	err := evolveDestinationTable(context.Background(), dest, "dest_items", sourceSchema, &config.IngestConfig{
		PrimaryKeys: []string{"id"},
	})
	require.NoError(t, err)
	require.Empty(t, dest.execCalls)
}

type normalizingFakeDestination struct {
	*fakeDestination
}

func (d *normalizingFakeDestination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	if col.DataType == schema.TypeBoolean {
		col.DataType = schema.TypeInt64
	}
	return col
}

func TestEvolveDestinationTableUsesDestinationTypeNormalization(t *testing.T) {
	dest := &normalizingFakeDestination{fakeDestination: &fakeDestination{
		tableSchemas: map[string]*schema.TableSchema{"dest_items": {Columns: []schema.Column{{
			Name: "active", DataType: schema.TypeInt64,
		}}}},
	}}
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "active", DataType: schema.TypeBoolean}}}

	err := evolveDestinationTable(context.Background(), dest, "dest_items", sourceSchema, &config.IngestConfig{})
	require.NoError(t, err)
	require.Empty(t, dest.execCalls)
}

func TestFlushLoopRefreshDetectsNullabilityOnlyChange(t *testing.T) {
	loop, dest, st := streamSchemaChangeFixture(&schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32, Nullable: false},
		{Name: "status", DataType: schema.TypeString, Nullable: false},
	}})
	st.schema.Columns[1].Nullable = false
	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32, Nullable: false},
		{Name: "status", DataType: schema.TypeString, Nullable: true},
	}}

	err := loop.ensureTable(context.Background(), source.SourceTableInfo{
		Name: "public.items", Schema: newSchema, PrimaryKeys: []string{"id"},
	})
	require.NoError(t, err)
	require.Len(t, dest.execCalls, 1)
	require.True(t, st.schema.Columns[1].Nullable)
	require.Len(t, dest.prepareCalls, 1)
}

// Staging-only CDC columns are not persisted on the destination; a refresh
// must not try to ADD them there, and the CDC metadata columns must not be
// flagged as type changes.
func TestFlushLoopRefreshSkipsCDCStagingColumns(t *testing.T) {
	destSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
	}}
	dest := &fakeDestination{tableSchemas: map[string]*schema.TableSchema{"dest_items": destSchema}}
	cfg := &config.IngestConfig{NoLoadTimestamp: true}
	st := &streamTableState{
		destTable:    "dest_items",
		stagingTable: "stg_items",
		schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		}},
	}
	loop := newFlushLoop(dest, cfg, StreamingOptions{Strategy: config.StrategyMerge}, map[string]*streamTableState{"public.items": st})
	loop.evolveTable = func(ctx context.Context, destTable string, newSchema *schema.TableSchema) error {
		return evolveDestinationTable(ctx, dest, destTable, newSchema, cfg)
	}

	newSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "note", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
	}}
	err := loop.ensureTable(context.Background(), source.SourceTableInfo{Name: "public.items", Schema: newSchema})
	require.NoError(t, err)

	require.Len(t, dest.execCalls, 1)
	assert.Contains(t, dest.execCalls[0].sql, "ADD COLUMN note")
	// Staging keeps the staging-only column in its new shape.
	require.Len(t, dest.prepareCalls, 1)
	names := make([]string, 0)
	for _, c := range dest.prepareCalls[0].Schema.Columns {
		names = append(names, c.Name)
	}
	assert.Contains(t, names, destination.CDCUnchangedColsColumn)
	assert.Contains(t, names, "note")
}
