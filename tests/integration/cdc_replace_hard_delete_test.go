//go:build integration

package integration

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	duckdbdest "github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

func TestDuckDBCDCReplaceHardDelete_CatchUp(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "cdc_replace_hard_delete.duckdb")
	dest := duckdbdest.NewDuckDBDestination()
	require.NoError(t, dest.Connect(ctx, "duckdb:///"+path))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	tableSchema := &schema.TableSchema{
		Name: "target",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true},
			{Name: "payload", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestamp},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}
	buildBatch := func(changes []cdcHardDeleteChange) arrow.RecordBatch {
		builder := array.NewRecordBuilder(memory.DefaultAllocator, tableSchema.ToArrowSchema())
		defer builder.Release()
		for _, change := range changes {
			builder.Field(0).(*array.Int64Builder).Append(change.id)
			payload := builder.Field(1).(*array.StringBuilder)
			if change.deleted {
				payload.AppendNull()
			} else {
				payload.Append(change.payload)
			}
			builder.Field(2).(*array.StringBuilder).Append(change.lsn)
			builder.Field(3).(*array.BooleanBuilder).Append(change.deleted)
			builder.Field(4).(*array.TimestampBuilder).Append(arrow.Timestamp(0))
			builder.Field(5).(*array.StringBuilder).Append("[]")
		}
		return builder.NewRecordBatch()
	}
	changes := make(chan source.RecordBatchResult, 2)
	changes <- source.RecordBatchResult{Batch: buildBatch([]cdcHardDeleteChange{
		{id: 1, payload: "kept", lsn: "00000000/00000010"},
		{id: 2, payload: "deleted", lsn: "00000000/00000010"},
		{id: 3, payload: "original", lsn: "00000000/00000010"},
	})}
	changes <- source.RecordBatchResult{Batch: buildBatch([]cdcHardDeleteChange{
		{id: 2, lsn: "00000000/00000020", deleted: true},
		{id: 3, lsn: "00000000/00000020", deleted: true},
		{id: 3, payload: "reinserted", lsn: "00000000/00000030"},
		{id: 4, lsn: "00000000/00000030", deleted: true},
	})}
	close(changes)

	require.NoError(t, (&strategy.ReplaceStrategy{}).Execute(ctx, &strategy.IngestionJob{
		Config: &config.IngestConfig{
			DestTable:           "target",
			IncrementalStrategy: config.StrategyReplace,
			PrimaryKeys:         []string{"id"},
			FullRefresh:         true,
			CDCDeleteBehavior:   config.CDCDeleteHard,
		},
		Destination:     dest,
		Schema:          tableSchema,
		SourceSchema:    tableSchema,
		BufferedRecords: changes,
	}))

	require.NoError(t, dest.Exec(ctx, "CHECKPOINT"))
	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", path))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.Equal(t, map[int64]cdcHardDeleteRow{
		1: {payload: "kept", lsn: "00000000/00000010"},
		3: {payload: "reinserted", lsn: "00000000/00000030"},
	}, readCDCHardDeleteRows(t, db, `SELECT id, payload, "_cdc_lsn", "_cdc_deleted" FROM target`))
	var unchangedColumns int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'target' AND column_name = '_cdc_unchanged_cols'`).Scan(&unchangedColumns))
	require.Zero(t, unchangedColumns)
}
