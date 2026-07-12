package iceberg

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestReplaceDeduplicatesPrimaryKeysAcrossSpillRuns(t *testing.T) {
	withSpillRunRows(t, 2)

	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.replace_dedup.incremental"
	tableSchema := replaceDedupSchema()
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(99), "old target", int64(0)}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true,
		PrimaryKeys: []string{"id"}, ClusterBy: []string{"score"},
	}))

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "v1-old", int64(1)},
		{int64(2), "v2", int64(2)},
		{int64(1), "v1-latest", int64(10)},
		{int64(3), "v3-latest", int64(30)},
		{int64(3), "v3-old", int64(3)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, Parallelism: 4,
		DeduplicatePrimaryKeys: true, IncrementalKey: "score",
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 3)
	require.Equal(t, "v1-latest", rows[int64(1)][1])
	require.Equal(t, "v2", rows[int64(2)][1])
	require.Equal(t, "v3-latest", rows[int64(3)][1])
	assertPhysicalSortOrder(t, dest, table)
}

func TestReplaceDeduplicationFailureLeavesTargetUnchanged(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.replace_dedup.null_key"
	tableSchema := replaceDedupSchema()
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(7), "preserved", int64(7)}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true, PrimaryKeys: []string{"id"},
	}))

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{nil, "invalid", int64(8)}})
	require.NoError(t, err)
	records := recordBatches(batches...)
	err = dest.WriteParallel(ctx, records, destination.WriteOptions{
		Table: table, Schema: tableSchema, DeduplicatePrimaryKeys: true,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, `id`)
	require.ErrorContains(t, err, `null`)
	require.Zero(t, len(records), "failed deduplication must drain remaining source batches")

	rows := readTableRows(t, dest, table)
	require.Equal(t, [][]any{{int64(7), "preserved", int64(7)}}, rows.Rows)
}

func TestReplaceDeduplicationRejectsUnorderedIncrementalKey(t *testing.T) {
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "versions", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64)},
	}, nil)
	id := array.NewInt64Builder(memory.DefaultAllocator)
	id.Append(1)
	versions := array.NewListBuilder(memory.DefaultAllocator, arrow.PrimitiveTypes.Int64)
	versions.Append(true)
	versions.ValueBuilder().(*array.Int64Builder).Append(1)
	batch := array.NewRecordBatch(sc, []arrow.Array{id.NewArray(), versions.NewArray()}, 1)
	id.Release()
	versions.Release()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: batch}
	close(records)
	reader := newRecordBatchReader(context.Background(), records, sc)
	defer reader.Release()

	_, cleanup, err := deduplicateRecordReader(reader, []string{"id"}, "versions")
	if cleanup != nil {
		cleanup()
	}
	require.ErrorContains(t, err, `incremental key column "versions" is not orderable`)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
}

func replaceDedupSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeInt64, Nullable: true},
		},
		PrimaryKeys:    []string{"id"},
		IncrementalKey: "score",
	}
}
