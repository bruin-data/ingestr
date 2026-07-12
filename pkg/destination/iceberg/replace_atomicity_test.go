package iceberg

import (
	"context"
	"errors"
	"sync"
	"testing"

	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnedPrepareAndCleanupRejectReplacementOwner(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.replace.owned_cleanup"
	tableSchema := mergeTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, OwnershipToken: "owner-a",
	}))
	require.ErrorContains(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, OwnershipToken: "owner-b",
	}), "another owner")
	require.ErrorContains(t, dest.DropTableIfOwned(ctx, table, "owner-b"), "ownership changed")
	_, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
}

func TestReplaceFailureLeavesRowsAndTableMetadataUnchanged(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_replace_metadata"
	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
	}}
	evolvedSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "cluster_key", DataType: schema.TypeInt64, Nullable: true},
	}}

	writeTableRows(t, dest, table, initialSchema, false, [][]any{{int64(1)}})
	dest.cfg.TableProperties["owner"] = "replacement"
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: evolvedSchema, DropFirst: true,
		PartitionBy: "bucket[8](id)", ClusterBy: []string{"cluster_key"},
	}))

	prepared, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	preparedSchema, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, preparedSchema.ColumnNames())
	require.Empty(t, prepared.Properties()["owner"])
	require.Empty(t, icebergPartitionFieldNames(ctx, t, dest, table))
	require.Zero(t, prepared.SortOrder().Len())

	failed := errors.New("simulated final replace rejection")
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{failed}}
	err = dest.WriteParallel(ctx, recordBatches(int64PairBatch(t, "id", []int64{2}, "cluster_key", []int64{20})), destination.WriteOptions{
		Table: table, Schema: evolvedSchema,
	})
	require.ErrorIs(t, err, failed)

	afterFailure, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	afterFailureSchema, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, afterFailureSchema.ColumnNames())
	require.Empty(t, afterFailure.Properties()["owner"])
	require.Empty(t, icebergPartitionFieldNames(ctx, t, dest, table))
	require.Zero(t, afterFailure.SortOrder().Len())
	require.Equal(t, [][]any{{int64(1)}}, readTableRows(t, dest, table).Rows)

	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64PairBatch(t, "id", []int64{2}, "cluster_key", []int64{20})), destination.WriteOptions{
		Table: table, Schema: evolvedSchema,
	}))
	committed, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	committedSchema, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "cluster_key"}, committedSchema.ColumnNames())
	require.Equal(t, "replacement", committed.Properties()["owner"])
	require.NotEmpty(t, icebergPartitionFieldNames(ctx, t, dest, table))
	require.NotZero(t, committed.SortOrder().Len())
	require.Equal(t, [][]any{{int64(2), int64(20)}}, readTableRows(t, dest, table).Rows)
}

func TestReplaceNormalizesNullableSourceIdentifierToRequiredTableField(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.replace_nullable_source_identifier"
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "value", DataType: schema.TypeString, Nullable: true},
		},
		PrimaryKeys: []string{"id"},
	}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1), "old"}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"}, DropFirst: true,
	}))
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: true},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	batches, err := buildRecordBatches(icebergArrowSchema(sourceSchema), [][]any{{int64(2), "new"}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: sourceSchema, PrimaryKeys: []string{"id"}, DeduplicatePrimaryKeys: true,
	}))
	require.Equal(t, [][]any{{int64(2), "new"}}, readTableRows(t, dest, table).Rows)
}

func TestReplaceReportsSortMetadataFailureAndRetryRepairsIt(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.replace_sort_retry"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "cluster_key", DataType: schema.TypeInt64, Nullable: false},
	}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1), int64(10)}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, DropFirst: true, ClusterBy: []string{"cluster_key"},
	}))

	injected := errors.New("simulated sort metadata rejection")
	base := dest.catalog
	dest.catalog = &failNthCommitCatalog{Catalog: base, failAt: 2, err: injected}
	t.Cleanup(func() { dest.catalog = base })
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "stable-replace"}

	err := dest.WriteParallel(ctx, recordBatches(int64PairBatch(t, "id", []int64{2}, "cluster_key", []int64{20})), opts)
	require.ErrorIs(t, err, injected)
	require.ErrorContains(t, err, "data replacement")
	require.Equal(t, [][]any{{int64(2), int64(20)}}, readTableRows(t, dest, table).Rows)
	committed, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.True(t, committed.SortOrder().IsUnsorted())

	// The data token is already durable, so the retry drains its input and
	// completes only the missing sort-order metadata update.
	retry := recordBatches(int64PairBatch(t, "id", []int64{999}, "cluster_key", []int64{999}))
	require.NoError(t, dest.WriteParallel(ctx, retry, opts))
	require.Empty(t, retry)
	require.Equal(t, [][]any{{int64(2), int64(20)}}, readTableRows(t, dest, table).Rows)
	repaired, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.False(t, repaired.SortOrder().IsUnsorted())
}

type failNthCommitCatalog struct {
	icebergcatalog.Catalog
	mu     sync.Mutex
	calls  int
	failAt int
	err    error
}

func (c *failNthCommitCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	fsFactory := func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) }
	return icebergtable.New(tbl.Identifier(), tbl.Metadata(), tbl.MetadataLocation(), fsFactory, c), nil
}

func (c *failNthCommitCatalog) CommitTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	c.mu.Lock()
	c.calls++
	fail := c.calls == c.failAt
	c.mu.Unlock()
	if fail {
		return nil, "", c.err
	}
	return c.Catalog.CommitTable(ctx, ident, requirements, updates)
}
