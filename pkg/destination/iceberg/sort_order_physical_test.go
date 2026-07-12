package iceberg

import (
	"context"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestPhysicalSortOrderClusteredAppend(t *testing.T) {
	dest := newHadoopDestination(t)
	table := "lake.sort_audit.append"
	tableSchema := sortAuditSchema()

	prepareSortAuditTable(t, dest, table, tableSchema, false, [][]any{
		{int64(1), int64(30), int64(3)},
		{int64(2), int64(10), int64(1)},
		{int64(3), int64(20), int64(2)},
	})

	assertPhysicalSortOrder(t, dest, table)
}

func TestPhysicalSortOrderReplace(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.sort_audit.replace"
	tableSchema := sortAuditSchema()

	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(9), int64(90), int64(9)}})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"},
		ClusterBy: []string{"cluster_key"}, DropFirst: true,
	}))
	writeSortAuditBatches(t, dest, table, tableSchema, [][]any{
		{int64(1), int64(30), int64(3)},
		{int64(2), int64(10), int64(1)},
		{int64(3), int64(20), int64(2)},
	})

	assertPhysicalSortOrder(t, dest, table)
}

func TestReplaceSortOrderTransitionUsesConservativeFileMetadata(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.sort_audit.replace_transition"
	tableSchema := sortAuditSchema()

	prepareSortAuditTable(t, dest, table, tableSchema, false, [][]any{
		{int64(1), int64(10), int64(30)},
		{int64(2), int64(20), int64(10)},
	})
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"},
		ClusterBy: []string{"updated_at"}, DropFirst: true,
	}))
	writeSortAuditBatches(t, dest, table, tableSchema, [][]any{
		{int64(3), int64(40), int64(30)},
		{int64(4), int64(30), int64(10)},
		{int64(5), int64(50), int64(20)},
	})

	assertPhysicalSortOrder(t, dest, table)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tasks)
	for _, task := range tasks {
		require.NotNil(t, task.File.SortOrderID())
		require.Equal(t, icebergtable.UnsortedSortOrderID, *task.File.SortOrderID())
	}
}

func TestPhysicalSortOrderDirectMerge(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.sort_audit.direct_merge"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, target, tableSchema, false, [][]any{
		{int64(1), int64(10), int64(1)},
		{int64(2), int64(20), int64(2)},
	})

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(3), int64(40), int64(3)},
		{int64(4), int64(30), int64(4)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: tableSchema, Parallelism: 4,
	}, destination.MergeOptions{
		TargetTable: target, StagingTable: "direct-records", PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", Parallelism: 4,
	}))

	assertPhysicalSortOrder(t, dest, target)
}

func TestPhysicalSortOrderDeleteInsert(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.sort_audit.delete_insert_target"
	staging := "lake.sort_audit.delete_insert_staging"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, target, tableSchema, false, [][]any{
		{int64(1), int64(10), int64(10)},
		{int64(2), int64(20), int64(20)},
	})
	prepareSortAuditTable(t, dest, staging, tableSchema, true, [][]any{
		{int64(3), int64(40), int64(30)},
		{int64(4), int64(30), int64(40)},
	})

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", IntervalStart: int64(10), IntervalEnd: int64(40),
		Columns: []string{"id", "cluster_key", "updated_at"},
	}))

	assertPhysicalSortOrder(t, dest, target)
}

func TestPhysicalSortOrderSCD2(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.sort_audit.scd2_target"
	staging := "lake.sort_audit.scd2_staging"
	tableSchema := scd2TestSchema()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)

	prepareClusteredTable(t, dest, target, tableSchema, []string{"score"}, false, nil)
	prepareClusteredTable(t, dest, staging, tableSchema, []string{"score"}, true, [][]any{
		scd2Row(3, "first", 40, micros(t1)),
		scd2Row(4, "stable", 30, micros(t1)),
	})
	runSCD2(t, dest, target, staging, t1, "")

	prepareClusteredTable(t, dest, staging, tableSchema, []string{"score"}, true, [][]any{
		scd2Row(3, "changed", 5, micros(t2)),
		scd2Row(4, "stable", 30, micros(t2)),
		scd2Row(5, "new", 25, micros(t2)),
	})
	runSCD2(t, dest, target, staging, t2, "")

	assertPhysicalSortOrder(t, dest, target)
}

func TestPhysicalSortOrderMergeOnReadRowDelta(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.sort_audit.mor_target"
	staging := "lake.sort_audit.mor_staging"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, target, tableSchema, false, [][]any{
		{int64(1), int64(10), int64(1)},
		{int64(2), int64(20), int64(2)},
	})
	prepareSortAuditTable(t, dest, staging, tableSchema, true, [][]any{
		{int64(3), int64(40), int64(3)},
		{int64(4), int64(30), int64(4)},
	})

	require.NoError(t, dest.MergeTable(context.Background(), destination.MergeOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", Columns: []string{"id", "cluster_key", "updated_at"},
		Parallelism: 4,
	}))

	assertPhysicalSortOrder(t, dest, target)
}

func TestPhysicalSortOrderCopyOnWriteFallback(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table." + writeMergeModeProperty: []string{mergeModeCopyOnWrite},
	})
	target := "lake.sort_audit.cow_target"
	staging := "lake.sort_audit.cow_staging"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, target, tableSchema, false, [][]any{
		{int64(1), int64(10), int64(1)},
		{int64(2), int64(20), int64(2)},
	})
	prepareSortAuditTable(t, dest, staging, tableSchema, true, [][]any{
		{int64(3), int64(40), int64(3)},
		{int64(4), int64(30), int64(4)},
	})

	require.NoError(t, dest.MergeTable(context.Background(), destination.MergeOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", Columns: []string{"id", "cluster_key", "updated_at"},
	}))

	assertPhysicalSortOrder(t, dest, target)
}

func TestPhysicalSortOrderCopyOnWriteSCD2NetNewRows(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table." + writeMergeModeProperty: []string{mergeModeCopyOnWrite},
	})
	target := "lake.sort_audit.cow_scd2_target"
	staging := "lake.sort_audit.cow_scd2_staging"
	tableSchema := scd2TestSchema()
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)

	prepareClusteredTable(t, dest, target, tableSchema, []string{"score"}, false, nil)
	prepareClusteredTable(t, dest, staging, tableSchema, []string{"score"}, true, [][]any{
		scd2Row(1, "highest", 50, micros(now)),
		scd2Row(2, "lowest", 10, micros(now)),
		scd2Row(3, "middle", 30, micros(now)),
	})
	runSCD2(t, dest, target, staging, now, "")

	assertPhysicalSortOrder(t, dest, target)
}

func TestSortedTableCompactionPreservesPhysicalOrder(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.sort_audit.compaction_skip"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, table, tableSchema, false, [][]any{
		{int64(1), int64(20), int64(1)},
		{int64(2), int64(10), int64(2)},
	})
	writeSortAuditBatches(t, dest, table, tableSchema, [][]any{
		{int64(3), int64(40), int64(3)},
		{int64(4), int64(30), int64(4)},
	})

	before := sortAuditDataFilePaths(t, dest, table)
	require.Len(t, before, 2)
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	beforeSnapshotID := tbl.CurrentSnapshot().SnapshotID

	result, err := dest.MaintainTable(ctx, table, MaintenanceOptions{
		CompactDataFiles: true, MinInputFiles: 2, TargetFileSizeBytes: 1 << 30,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.CompactionGroups)
	require.Equal(t, 1, result.AddedDataFiles)
	require.Equal(t, 2, result.RemovedDataFiles)
	require.NotEqual(t, before, sortAuditDataFilePaths(t, dest, table))
	tbl, err = dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NotEqual(t, beforeSnapshotID, tbl.CurrentSnapshot().SnapshotID)
	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tasks)
	for _, task := range tasks {
		require.NotNil(t, task.File.SortOrderID())
		require.Equal(t, tbl.SortOrder().OrderID(), *task.File.SortOrderID())
	}

	assertPhysicalSortOrder(t, dest, table)
	rows := readTableRows(t, dest, table).Rows
	require.Len(t, rows, 4)
	for i := 1; i < len(rows); i++ {
		require.LessOrEqual(t, rows[i-1][1].(int64), rows[i][1].(int64))
	}
}

func sortAuditSchema() *schema.TableSchema {
	return &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "cluster_key", DataType: schema.TypeInt64, Nullable: true},
		{Name: "updated_at", DataType: schema.TypeInt64, Nullable: false},
	}}
}

func newHadoopDestinationWithTableProperties(t *testing.T, properties url.Values) *Destination {
	t.Helper()
	properties.Set("warehouse", t.TempDir())
	dest := NewDestination()
	require.NoError(t, dest.Connect(context.Background(), "iceberg+hadoop://?"+properties.Encode()))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	return dest
}

func prepareSortAuditTable(t *testing.T, dest *Destination, table string, tableSchema *schema.TableSchema, dropFirst bool, rows [][]any) {
	t.Helper()
	prepareClusteredTable(t, dest, table, tableSchema, []string{"cluster_key"}, dropFirst, rows)
}

func prepareClusteredTable(t *testing.T, dest *Destination, table string, tableSchema *schema.TableSchema, clusterBy []string, dropFirst bool, rows [][]any) {
	t.Helper()
	require.NoError(t, dest.PrepareTable(context.Background(), destination.PrepareOptions{
		Table: table, Schema: tableSchema, PrimaryKeys: []string{"id"},
		ClusterBy: clusterBy, DropFirst: dropFirst,
	}))
	writeSortAuditBatches(t, dest, table, tableSchema, rows)
}

func writeSortAuditBatches(t *testing.T, dest *Destination, table string, tableSchema *schema.TableSchema, rows [][]any) {
	t.Helper()
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), rows)
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(context.Background(), recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema, Parallelism: 4,
	}))
}

func assertPhysicalSortOrder(t *testing.T, dest *Destination, table string) {
	t.Helper()
	ctx := context.Background()
	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	order := tbl.SortOrder()
	require.NotZero(t, order.Len(), "table %s has no declared sort order", table)

	var sortColumns []string
	for _, sortField := range order.Fields() {
		require.Equal(t, icebergtable.SortASC, sortField.Direction)
		require.Equal(t, icebergtable.NullsFirst, sortField.NullOrder)
		require.Equal(t, "identity", sortField.Transform.String())
		name, ok := tbl.Schema().FindColumnName(sortField.SourceID())
		require.True(t, ok, "sort source field %d is missing", sortField.SourceID())
		sortColumns = append(sortColumns, name)
	}

	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tasks, "table %s has no live data files", table)
	fs, err := tbl.FS(ctx)
	require.NoError(t, err)
	for _, task := range tasks {
		dataFile := task.File
		require.Equal(t, iceberggo.EntryContentData, dataFile.ContentType())
		require.NotNil(t, dataFile.SortOrderID(), "data file %s has no sort-order-id", dataFile.FilePath())
		require.Contains(t, []int{icebergtable.UnsortedSortOrderID, order.OrderID()}, *dataFile.SortOrderID(),
			"data file %s claims an unexpected sort-order-id", dataFile.FilePath())
		require.Equal(t, iceberggo.ParquetFile, dataFile.FileFormat())

		input, err := fs.Open(dataFile.FilePath())
		require.NoError(t, err)
		parquetReader, err := file.NewParquetReader(input)
		if err != nil {
			_ = input.Close()
		}
		require.NoError(t, err)
		arrowReader, err := pqarrow.NewFileReader(parquetReader, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
		require.NoError(t, err)
		recordReader, err := arrowReader.GetRecordReader(ctx, nil, nil)
		require.NoError(t, err)
		sorter, err := newClusterSorter(recordReader.Schema(), sortColumns)
		require.NoError(t, err)

		var previous string
		var previousValues []any
		hasPrevious := false
		var rowsRead int64
		for recordReader.Next() {
			batch := recordReader.RecordBatch()
			for row := 0; row < int(batch.NumRows()); row++ {
				rowsRead++
				values := make([]any, len(sorter.keyIdx))
				for i, column := range sorter.keyIdx {
					values[i], err = rowValue(batch.Column(column), row)
					require.NoError(t, err)
				}
				key, err := sorter.encode(values)
				require.NoError(t, err)
				if hasPrevious {
					require.LessOrEqual(t, previous, key,
						"data file %s is not sorted by %v: %v precedes %v",
						dataFile.FilePath(), sortColumns, previousValues, values)
				}
				previous = key
				previousValues = slices.Clone(values)
				hasPrevious = true
			}
		}
		require.NoError(t, recordReader.Err())
		require.Equal(t, dataFile.Count(), rowsRead, "data file %s row-count audit", dataFile.FilePath())
		sorter.Close()
		recordReader.Release()
		require.NoError(t, parquetReader.Close())
	}
}

func sortAuditDataFilePaths(t *testing.T, dest *Destination, table string) []string {
	t.Helper()
	tbl, err := dest.loadIcebergTable(context.Background(), table)
	require.NoError(t, err)
	tasks, err := tbl.Scan().PlanFiles(context.Background())
	require.NoError(t, err)
	paths := make([]string, 0, len(tasks))
	for _, task := range tasks {
		paths = append(paths, task.File.FilePath())
	}
	slices.Sort(paths)
	return paths
}

func TestEnsureSortOrderClearsOrderOnTableCreatedSorted(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "lake.sort_audit.created_sorted"
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    lifecycleTestSchema(),
		ClusterBy: []string{"id"},
	}))

	tbl, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NotEqual(t, icebergtable.UnsortedSortOrder.OrderID(), tbl.SortOrder().OrderID())

	cleared, err := dest.ensureSortOrder(ctx, tbl, nil)
	require.NoError(t, err)
	require.Equal(t, icebergtable.UnsortedSortOrder.OrderID(), cleared.SortOrder().OrderID())
}
