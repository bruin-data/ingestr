//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/starrocks"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestStarRocksCDCDirectSnapshot(t *testing.T) {
	if testing.Short() || !containerWanted("starrocks") {
		t.Skip("Docker CDC integration")
	}
	t.Parallel()
	ctx := t.Context()
	dsn, uri := os.Getenv("INGESTR_TEST_STARROCKS_DSN"), os.Getenv("INGESTR_TEST_STARROCKS_URI")
	if dsn == "" || uri == "" {
		dsn, uri = sharedStarRocks(t)
	}
	waitForStarRocksBackend(t, dsn)
	mysqlConfig, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	mysqlConfig.InterpolateParams = true
	db, err := sql.Open("mysql", mysqlConfig.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	database := "cdc_snapshot_" + strings.ToLower(uniqueSuffix())
	execEventually(t, db, "CREATE DATABASE "+database)
	t.Cleanup(func() { _, _ = db.Exec("DROP DATABASE " + database) })
	execEventually(t, db, "CREATE TABLE "+database+".ready_probe (id INT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num' = '1')")
	execEventually(t, db, "DROP TABLE "+database+".ready_probe")
	dest := starrocks.NewStarRocksDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	const (
		lsn10 = "00000001/00000010"
		lsn20 = "00000001/00000020"
		lsn30 = "00000001/00000030"
		lsn40 = "00000001/00000040"
	)
	rowSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
	}}
	type row struct {
		ID      int64
		Payload string
		LSN     string
		Deleted bool
	}
	prepare := func(t *testing.T, name string) destination.WriteOptions {
		t.Helper()
		table := database + "." + name
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: table, Schema: rowSchema, PrimaryKeys: []string{"id"}, RequirePrimaryKeyMatch: true,
		}))
		targetSchema, err := dest.GetTableSchema(ctx, table)
		require.NoError(t, err)
		require.NotContains(t, targetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
		incarnation, exists, err := dest.CDCTargetIncarnation(ctx, table)
		require.NoError(t, err)
		require.True(t, exists)
		return destination.WriteOptions{
			Table: table, Schema: rowSchema, PrimaryKeys: []string{"id"}, Parallelism: 4,
			CDCSnapshot: true, CDCExpectedIncarnation: incarnation,
		}
	}
	write := func(t *testing.T, opts destination.WriteOptions, input []row, sourceErr error) error {
		t.Helper()
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		records := make(chan source.RecordBatchResult, len(input)+1)
		builder := array.NewRecordBuilder(mem, rowSchema.ToArrowSchema())
		for _, r := range input {
			builder.Field(0).(*array.Int64Builder).Append(r.ID)
			builder.Field(1).(*array.StringBuilder).Append(r.Payload)
			builder.Field(2).(*array.StringBuilder).Append(r.LSN)
			builder.Field(3).(*array.BooleanBuilder).Append(r.Deleted)
			builder.Field(4).(*array.TimestampBuilder).Append(arrow.Timestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro()))
			builder.Field(5).(*array.StringBuilder).Append("[]")
			records <- source.RecordBatchResult{Batch: builder.NewRecordBatch()}
		}
		builder.Release()
		if sourceErr != nil {
			records <- source.RecordBatchResult{Err: sourceErr}
		}
		close(records)
		err := dest.WriteParallel(ctx, records, opts)
		for result := range records {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		mem.AssertSize(t, 0)
		return err
	}
	read := func(t *testing.T, table string) []row {
		t.Helper()
		rows, err := db.QueryContext(ctx, "SELECT id, payload, _cdc_lsn, _cdc_deleted FROM "+table+" ORDER BY id")
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var result []row
		for rows.Next() {
			var r row
			require.NoError(t, rows.Scan(&r.ID, &r.Payload, &r.LSN, &r.Deleted))
			result = append(result, r)
		}
		require.NoError(t, rows.Err())
		return result
	}
	seedChange := func(t *testing.T, table string, r row) {
		t.Helper()
		require.NoError(t, dest.Exec(ctx, "INSERT INTO "+table+" (id, payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at) VALUES (?, ?, ?, ?, '2026-01-02')", r.ID, r.Payload, r.LSN, r.Deleted))
	}
	readOrder := func(t *testing.T, table string, id int64) string {
		t.Helper()
		var order string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT CAST(__ingestr_cdc_order AS VARCHAR) FROM "+table+" WHERE id = ?", id).Scan(&order))
		return order
	}

	t.Run("conditional generated numeric ordering", func(t *testing.T) {
		opts := prepare(t, "ordering")
		first := []row{{ID: 1, Payload: "first", LSN: lsn20}, {ID: 2, Payload: "snapshot", LSN: lsn20}}
		require.NoError(t, write(t, opts, first, nil))
		require.Equal(t, first, read(t, opts.Table))
		targetSchema, err := dest.GetTableSchema(ctx, opts.Table)
		require.NoError(t, err)
		require.NotContains(t, targetSchema.ColumnNames(), "__ingestr_cdc_order")
		require.NotContains(t, targetSchema.ColumnNames(), destination.CDCUnchangedColsColumn)
		var helpers int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'ordering' AND COLUMN_NAME = '__ingestr_cdc_order' AND GENERATION_EXPRESSION IS NOT NULL", database).Scan(&helpers))
		require.Equal(t, 1, helpers)

		require.NoError(t, write(t, opts, []row{{ID: 1, Payload: "older", LSN: lsn10}}, nil))
		require.Equal(t, first, read(t, opts.Table), "older conditional loads must succeed without replacing newer rows")
		first[0].Payload = "equal"
		require.NoError(t, write(t, opts, first[:1], nil))
		require.Equal(t, first, read(t, opts.Table), "an equal position must replace the row")
		first[0] = row{ID: 1, Payload: "newer", LSN: lsn30}
		require.NoError(t, write(t, opts, first[:1], nil))
		require.Equal(t, first, read(t, opts.Table))

		first[1] = row{ID: 2, Payload: "WAL at same base", LSN: lsn20 + "/0000000000000001"}
		seedChange(t, opts.Table, first[1])
		require.NoError(t, write(t, opts, []row{{ID: 2, Payload: "snapshot replay", LSN: lsn20}}, nil))
		require.Equal(t, first, read(t, opts.Table), "the base snapshot position must sort below a sequenced change at that base")
		first[1] = row{ID: 2, Payload: "later snapshot", LSN: lsn30}
		require.NoError(t, write(t, opts, first[1:], nil))
		require.Equal(t, first, read(t, opts.Table))
	})

	t.Run("full unsigned LSN and sequence range", func(t *testing.T) {
		opts := prepare(t, "numeric_boundaries")
		positions := []struct {
			lsn, order string
		}{
			{"00000000/00000000", "-170141183460469231731687303715884105728"},
			{"7FFFFFFF/FFFFFFFF", "-18446744073709551616"},
			{"80000000/00000000", "0"},
			{"FFFFFFFF/FFFFFFFF", "170141183460469231713240559642174554112"},
		}
		for _, position := range positions {
			current := row{ID: 1, Payload: position.lsn, LSN: position.lsn}
			require.NoError(t, write(t, opts, []row{current}, nil))
			require.Equal(t, []row{current}, read(t, opts.Table))
			require.Equal(t, position.order, readOrder(t, opts.Table, 1))
		}
		last := row{ID: 1, Payload: "maximum sequenced change", LSN: "FFFFFFFF/FFFFFFFF/FFFFFFFFFFFFFFFF"}
		seedChange(t, opts.Table, last)
		require.Equal(t, "170141183460469231731687303715884105727", readOrder(t, opts.Table, 1))
		for _, position := range positions {
			require.NoError(t, write(t, opts, []row{{ID: 1, Payload: "replayed", LSN: position.lsn}}, nil))
			require.Equal(t, []row{last}, read(t, opts.Table))
		}
	})

	for _, hardDelete := range []bool{false, true} {
		name := "soft"
		if hardDelete {
			name = "hard"
		}
		t.Run("resnapshot and empty replacement "+name, func(t *testing.T) {
			opts := prepare(t, "replace_"+name)
			initial := []row{{ID: 1, Payload: "keep", LSN: lsn10}, {ID: 2, Payload: "gone", LSN: lsn10}, {ID: 3, Payload: "old tombstone", LSN: lsn10}}
			require.NoError(t, write(t, opts, initial, nil))
			initial[2].Deleted = true
			seedChange(t, opts.Table, initial[2])
			replacement := row{ID: 1, Payload: "refreshed", LSN: lsn20}
			require.NoError(t, write(t, opts, []row{replacement}, nil))
			newer := row{ID: 4, Payload: "newer", LSN: lsn30}
			seedChange(t, opts.Table, newer)
			retire := destination.CDCRetireOptions{
				Table: opts.Table, PrimaryKeys: opts.PrimaryKeys, Boundary: lsn20,
				SyncedAt:            time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
				ExpectedIncarnation: opts.CDCExpectedIncarnation, HardDelete: hardDelete,
			}
			for range 2 {
				require.NoError(t, dest.RetireCDCRowsBefore(ctx, retire))
				if hardDelete {
					require.Equal(t, []row{replacement, newer}, read(t, opts.Table))
				} else {
					require.Equal(t, []row{replacement, {ID: 2, Payload: "gone", LSN: lsn20, Deleted: true}, initial[2], newer}, read(t, opts.Table))
					require.Equal(t, readOrder(t, opts.Table, 1), readOrder(t, opts.Table, 2), "retirement must recompute the generated position from its new LSN")
				}
			}

			require.NoError(t, write(t, opts, nil, nil))
			retire.Boundary = lsn40
			require.NoError(t, dest.RetireCDCRowsBefore(ctx, retire))
			if hardDelete {
				require.Empty(t, read(t, opts.Table))
			} else {
				require.Equal(t, []row{
					{ID: 1, Payload: "refreshed", LSN: lsn40, Deleted: true},
					{ID: 2, Payload: "gone", LSN: lsn20, Deleted: true},
					initial[2],
					{ID: 4, Payload: "newer", LSN: lsn40, Deleted: true},
				}, read(t, opts.Table))
			}
		})
	}

	t.Run("partial snapshot retry", func(t *testing.T) {
		opts := prepare(t, "retry")
		opts.Parallelism = 1
		interrupted := errors.New("snapshot interrupted after first load")
		first := row{ID: 1, Payload: "first chunk", LSN: lsn20}
		require.ErrorIs(t, write(t, opts, []row{first}, interrupted), interrupted)
		require.Equal(t, []row{first}, read(t, opts.Table))

		newer := row{ID: 1, Payload: "newer WAL", LSN: lsn20 + "/0000000000000001"}
		seedChange(t, opts.Table, newer)
		opts.Parallelism = 4
		retry := []row{first, {ID: 2, Payload: "second chunk", LSN: lsn20}, {ID: 3, Payload: "third chunk", LSN: lsn20}}
		for range 2 {
			require.NoError(t, write(t, opts, retry, nil))
			require.Equal(t, []row{newer, retry[1], retry[2]}, read(t, opts.Table), "replaying visible rows must not duplicate keys or overwrite newer WAL")
		}
	})

	t.Run("retained target columns use staged merge", func(t *testing.T) {
		target, stage := database+".retained", database+".retained_stage"
		withRetained := *rowSchema
		withRetained.Columns = append(append([]schema.Column(nil), rowSchema.Columns...), schema.Column{
			Name: "retained", DataType: schema.TypeString, Nullable: true,
		})
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: target, Schema: &withRetained, PrimaryKeys: []string{"id"},
		}))
		require.NoError(t, dest.Exec(ctx, "INSERT INTO "+target+" (id, payload, retained, _cdc_lsn, _cdc_deleted, _cdc_synced_at) VALUES (1, 'old', 'keep', ?, false, '2026-01-01')", lsn10))
		canWrite, err := destination.CanWriteDirectCDCSnapshot(ctx, dest, target, rowSchema, "")
		require.NoError(t, err)
		require.False(t, canWrite)
		opts := destination.WriteOptions{Table: target, Schema: rowSchema, PrimaryKeys: []string{"id"}, CDCSnapshot: true}
		input := []row{{ID: 1, Payload: "new", LSN: lsn20}}
		require.ErrorContains(t, write(t, opts, input, nil), "requires every target column")
		var retained string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT retained FROM "+target+" WHERE id = 1").Scan(&retained))
		require.Equal(t, "keep", retained)

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: stage, Schema: rowSchema}))
		opts.Table, opts.CDCSnapshot, opts.StagingTable = stage, false, true
		opts.PrimaryKeys = nil
		require.NoError(t, write(t, opts, input, nil))
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			StagingTable: stage, TargetTable: target, PrimaryKeys: []string{"id"},
			Columns: rowSchema.ColumnNames(), Schema: rowSchema, CDCSnapshot: true,
		}))
		require.Equal(t, input, read(t, target))
		require.NoError(t, db.QueryRowContext(ctx, "SELECT retained FROM "+target+" WHERE id = 1").Scan(&retained))
		require.Equal(t, "keep", retained)
	})
}
