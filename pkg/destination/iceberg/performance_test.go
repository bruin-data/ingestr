package iceberg

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestPrepareTableCreatesPartitionTransformsAndSortOrder(t *testing.T) {
	dest := newHadoopDestination(t)
	dest.cfg.PartitionSpec = "day(created_at),bucket[16](id),truncate[4](category)"
	ctx := context.Background()
	table := "lake.performance.partitioned"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "created_at", DataType: schema.TypeTimestamp, Nullable: false},
		{Name: "category", DataType: schema.TypeString, Nullable: true},
	}}

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, ClusterBy: []string{"category", "id"},
	}))
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)

	var partitionFields []string
	spec := tbl.Metadata().PartitionSpec()
	for _, field := range spec.Fields() {
		partitionFields = append(partitionFields, field.Name+":"+field.Transform.String())
	}
	require.Equal(t, []string{
		"created_at_day:day",
		"id_bucket_16:bucket[16]",
		"category_truncate_4:truncate[4]",
	}, partitionFields)

	var sortSources []string
	for _, field := range tbl.SortOrder().Fields() {
		name, ok := tbl.Schema().FindColumnName(field.SourceID())
		require.True(t, ok)
		sortSources = append(sortSources, name)
	}
	require.Equal(t, []string{"category", "id"}, sortSources)
}

func TestWriteParallelPhysicallyClustersUnpartitionedRows(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.performance.clustered"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, ClusterBy: []string{"id"},
	}))

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 10, 2, -1)), destination.WriteOptions{
		Table: table, Schema: tableSchema, Parallelism: 4,
	}))
	rows := readTableRows(t, dest, table)
	require.Len(t, rows.Rows, 3)
	require.Equal(t, []any{int64(-1), int64(2), int64(10)}, []any{rows.Rows[0][0], rows.Rows[1][0], rows.Rows[2][0]})
}

func TestPrepareTableUpdatesExistingSortOrder(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.performance.sort_evolution"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "score", DataType: schema.TypeInt64},
	}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, ClusterBy: []string{"id"},
	}))
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, ClusterBy: []string{"score", "id"},
	}))

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	var columns []string
	for _, field := range tbl.SortOrder().Fields() {
		name, ok := tbl.Schema().FindColumnName(field.SourceID())
		require.True(t, ok)
		columns = append(columns, name)
	}
	require.Equal(t, []string{"score", "id"}, columns)
	require.GreaterOrEqual(t, len(tbl.Metadata().SortOrders()), 2)
}
