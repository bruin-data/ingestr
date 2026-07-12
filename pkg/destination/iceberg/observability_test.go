package iceberg

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestGetCommitInfoReportsSnapshotMetrics(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.observability.commits"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: tableSchema}))

	token := "observable-flush"
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, 1, 2)), destination.WriteOptions{
		Table:       table,
		Schema:      tableSchema,
		CommitToken: token,
		Parallelism: 2,
	}))

	info, err := dest.GetCommitInfo(ctx, table)
	require.NoError(t, err)
	require.NotZero(t, info.SnapshotID)
	require.NotZero(t, info.CommittedAt)
	require.Equal(t, "append", info.Operation)
	require.EqualValues(t, 2, info.AddedRows)
	require.EqualValues(t, 2, info.TotalRows)
	require.Positive(t, info.AddedDataFiles)
	require.Equal(t, commitTokenID(token), info.CommitToken)
	require.Contains(t, info.String(), "added_rows=2")
}
