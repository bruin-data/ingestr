package iceberg

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/stretchr/testify/require"
)

func TestMaintainTableCompactsFilesAndPreservesCommitLineage(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.compaction"
	tableSchema := lifecycleTestSchema()

	for value := int64(1); value <= 5; value++ {
		writeTableRows(t, dest, tableName, tableSchema, false, [][]any{{value}})
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(6)}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table:        tableName,
		Schema:       tableSchema,
		CommitToken:  "maintenance-token",
		CDCResumeLSN: "0/16B6C50",
	}))

	before, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	beforeTasks, err := before.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.Len(t, beforeTasks, 6)

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles:      true,
		TargetFileSizeBytes:   1024 * 1024,
		MinInputFiles:         2,
		DeleteFileThreshold:   1,
		EnableManifestMerge:   true,
		ManifestMinCount:      2,
		ManifestTargetSize:    1024 * 1024,
		PreviousMetadataFiles: 2,
	})
	require.NoError(t, err)
	require.Positive(t, result.CompactionGroups)
	require.Equal(t, 6, result.RemovedDataFiles)
	require.Less(t, result.AddedDataFiles, result.RemovedDataFiles)
	require.True(t, result.ManifestMergeEnabled)
	require.Equal(t, 2, result.PreviousMetadataVersionsKept)

	after, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	afterTasks, err := after.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.Less(t, len(afterTasks), len(beforeTasks))
	require.EqualValues(t, 6, icebergRowCount(ctx, t, dest, tableName))
	require.True(t, tableHasCommitToken(after, commitTokenID("maintenance-token")))
	require.Equal(t, "0/16B6C50", latestCDCResumeLSN(after))
	require.Equal(t, "maintenance", after.CurrentSnapshot().Summary.Properties["ingestr.operation"])
}

func TestMaintainTableCompactsFilesWithSQLiteCatalog(t *testing.T) {
	ctx := context.Background()
	dest := NewDestination()
	uri := "iceberg+sqlite://" + filepath.Join(t.TempDir(), "catalog.db") +
		"?warehouse_path=" + url.QueryEscape(t.TempDir())
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
	tableName := "maintenance.sqlite_compaction"
	tableSchema := lifecycleTestSchema()
	for value := int64(1); value <= 4; value++ {
		writeTableRows(t, dest, tableName, tableSchema, false, [][]any{{value}})
	}

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles:    true,
		TargetFileSizeBytes: 1024 * 1024,
		MinInputFiles:       2,
	})
	require.NoError(t, err)
	require.Positive(t, result.CompactionGroups)
	require.Equal(t, 4, result.RemovedDataFiles)
	require.EqualValues(t, 4, icebergRowCount(ctx, t, dest, tableName))
}

func TestMaintainTableCompactsEachPartitionIndependently(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.partitioned_compaction"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      tableSchema,
		PartitionBy: "id",
	}))
	for iteration := range 3 {
		for _, value := range []int64{1, 2} {
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
			require.NoError(t, dest.WriteParallel(ctx, recordBatches(int64Batch(t, value)), destination.WriteOptions{
				Table:       tableName,
				Schema:      tableSchema,
				CommitToken: fmt.Sprintf("partition-%d-%d", iteration, value),
			}))
		}
	}

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles:    true,
		TargetFileSizeBytes: 1024 * 1024,
		MinInputFiles:       2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.CompactionGroups)
	require.Equal(t, 6, result.RemovedDataFiles)
	require.EqualValues(t, 6, icebergRowCount(ctx, t, dest, tableName))
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
}

func TestMaintainTableSortsEachPartitionAndRecordsCurrentSortOrder(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.partitioned_sorted_compaction"
	tableSchema := sortAuditSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       tableName,
		Schema:      tableSchema,
		PartitionBy: "id",
		ClusterBy:   []string{"cluster_key"},
	}))
	for iteration, clusterKey := range []int64{30, 10, 20} {
		for _, id := range []int64{1, 2} {
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
			batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{id, clusterKey, int64(iteration)}})
			require.NoError(t, err)
			require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
				Table: tableName, Schema: tableSchema,
				CommitToken: fmt.Sprintf("partition-sort-%d-%d", iteration, id),
			}))
		}
	}

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles: true, TargetFileSizeBytes: 1024 * 1024, MinInputFiles: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.CompactionGroups)
	require.Equal(t, 6, result.RemovedDataFiles)

	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	tasks, err := tbl.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		require.NotNil(t, task.File.SortOrderID())
		require.Equal(t, tbl.SortOrder().OrderID(), *task.File.SortOrderID())
	}
	assertPhysicalSortOrder(t, dest, tableName)
}

func TestSafePositionDeletesRequireEveryReferencedDataFileToBeRewritten(t *testing.T) {
	dataA := testMaintenanceDataFile(t, iceberggo.EntryContentData, "file:///data/a.parquet")
	dataB := testMaintenanceDataFile(t, iceberggo.EntryContentData, "file:///data/b.parquet")
	sharedDelete := testMaintenanceDataFile(t, iceberggo.EntryContentPosDeletes, "file:///delete/shared.parquet")
	aOnlyDelete := testMaintenanceDataFile(t, iceberggo.EntryContentPosDeletes, "file:///delete/a-only.parquet")
	tasks := []icebergtable.FileScanTask{
		{File: dataA, DeleteFiles: []iceberggo.DataFile{sharedDelete, aOnlyDelete}},
		{File: dataB, DeleteFiles: []iceberggo.DataFile{sharedDelete}},
	}

	oneGroup := []icebergtable.CompactionTaskGroup{{Tasks: tasks[:1]}}
	require.Equal(t, []string{aOnlyDelete.FilePath()}, dataFilePaths(safePositionDeletesForRewrite(tasks, oneGroup)))

	allGroups := []icebergtable.CompactionTaskGroup{{Tasks: tasks[:1]}, {Tasks: tasks[1:]}}
	require.Equal(
		t,
		[]string{aOnlyDelete.FilePath(), sharedDelete.FilePath()},
		dataFilePaths(safePositionDeletesForRewrite(tasks, allGroups)),
	)
}

func TestSortedCompactionPreservesV3RowLineage(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table.format-version": []string{"3"},
	})
	tableName := "maintenance.v3_sorted_lineage"
	tableSchema := sortAuditSchema()
	for i, clusterKey := range []int64{30, 10, 20} {
		prepareSortAuditTable(t, dest, tableName, tableSchema, false, [][]any{{int64(i + 1), clusterKey, int64(i)}})
	}

	before, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, 3, before.Metadata().Version())
	beforeTasks, err := before.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.True(t, allTasksHaveRowLineage(beforeTasks))
	lineageBefore := readV3LineageByID(t, before)

	_, err = dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles: true, TargetFileSizeBytes: 1024 * 1024, MinInputFiles: 2,
	})
	require.NoError(t, err)
	after, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	afterTasks, err := after.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.True(t, allTasksHaveRowLineage(afterTasks))
	require.Equal(t, lineageBefore, readV3LineageByID(t, after))
}

func TestAllTasksHaveRowLineageRejectsMixedLegacyFiles(t *testing.T) {
	firstRowID := int64(10)
	lineage := icebergtable.FileScanTask{FirstRowID: &firstRowID}
	legacy := icebergtable.FileScanTask{}
	require.True(t, allTasksHaveRowLineage([]icebergtable.FileScanTask{lineage}))
	require.False(t, allTasksHaveRowLineage([]icebergtable.FileScanTask{lineage, legacy}))
	require.False(t, allTasksHaveRowLineage(nil))
}

func TestCanceledSortedCompactionDoesNotCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	tableName := "maintenance.canceled_sorted_compaction"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, tableName, tableSchema, false, [][]any{{int64(1), int64(20), int64(1)}})
	prepareSortAuditTable(t, dest, tableName, tableSchema, false, [][]any{{int64(2), int64(10), int64(2)}})
	before, err := dest.loadIcebergTable(context.Background(), tableName)
	require.NoError(t, err)
	beforeSnapshotID := before.CurrentSnapshot().SnapshotID

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles: true, TargetFileSizeBytes: 1024 * 1024, MinInputFiles: 2,
	})
	require.ErrorIs(t, err, context.Canceled)
	after, loadErr := dest.loadIcebergTable(context.Background(), tableName)
	require.NoError(t, loadErr)
	require.Equal(t, beforeSnapshotID, after.CurrentSnapshot().SnapshotID)
}

func TestSortedCompactionCommitFailureCleansReplacementFiles(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	tableName := "maintenance.sorted_cleanup"
	tableSchema := sortAuditSchema()
	prepareSortAuditTable(t, dest, tableName, tableSchema, false, [][]any{{int64(1), int64(20), int64(1)}})
	prepareSortAuditTable(t, dest, tableName, tableSchema, false, [][]any{{int64(2), int64(10), int64(2)}})
	before := allTableParquetFiles(t, dest, tableName)
	dest.catalog = &commitOutcomeCatalog{Catalog: dest.catalog, beforeCommitErrs: []error{icebergtable.ErrCommitFailed}}

	_, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		CompactDataFiles: true, TargetFileSizeBytes: 1024 * 1024, MinInputFiles: 2,
	})
	require.ErrorIs(t, err, icebergtable.ErrCommitFailed)
	require.Equal(t, before, allTableParquetFiles(t, dest, tableName))
}

func readV3LineageByID(t *testing.T, tbl *icebergtable.Table) map[int64][2]int64 {
	t.Helper()
	arrowSchema, records, err := tbl.Scan(icebergtable.WithRowLineage()).ToArrowRecords(context.Background())
	require.NoError(t, err)
	idColumns := make(map[string]int, 3)
	for i, field := range arrowSchema.Fields() {
		idColumns[field.Name] = i
	}
	require.Contains(t, idColumns, "id")
	require.Contains(t, idColumns, "_row_id")
	require.Contains(t, idColumns, "_last_updated_sequence_number")

	result := make(map[int64][2]int64)
	for batch, readErr := range records {
		require.NoError(t, readErr)
		for row := 0; row < int(batch.NumRows()); row++ {
			idValue, valueErr := rowValue(batch.Column(idColumns["id"]), row)
			require.NoError(t, valueErr)
			rowID, valueErr := rowValue(batch.Column(idColumns["_row_id"]), row)
			require.NoError(t, valueErr)
			sequence, valueErr := rowValue(batch.Column(idColumns["_last_updated_sequence_number"]), row)
			require.NoError(t, valueErr)
			require.NotNil(t, rowID)
			require.NotNil(t, sequence)
			result[idValue.(int64)] = [2]int64{rowID.(int64), sequence.(int64)}
		}
		batch.Release()
	}
	return result
}

func testMaintenanceDataFile(t *testing.T, content iceberggo.ManifestEntryContent, path string) iceberggo.DataFile {
	t.Helper()
	builder, err := iceberggo.NewDataFileBuilder(
		*iceberggo.UnpartitionedSpec, content, path, iceberggo.ParquetFile,
		nil, nil, nil, 1, 1,
	)
	require.NoError(t, err)
	return builder.Build()
}

func dataFilePaths(files []iceberggo.DataFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.FilePath()
	}
	return paths
}

func TestMaintainTableCompactsEqualityDeletesWithoutChangingRows(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	target := "maintenance.delete_compaction"
	staging := "maintenance.delete_compaction_staging"
	tableSchema := mergeTestSchema()
	tableSchema.PrimaryKeys = []string{"id"}
	base := micros(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	writeTableRows(t, dest, target, tableSchema, false, [][]any{
		{int64(1), "v0", 0.0, base},
		{int64(2), "unchanged", 2.0, base},
	})
	for version := 1; version <= 5; version++ {
		writeTableRows(t, dest, staging, tableSchema, true, [][]any{
			{int64(1), fmt.Sprintf("v%d", version), float64(version), base + int64(version)},
		})
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			StagingTable:   staging,
			TargetTable:    target,
			PrimaryKeys:    []string{"id"},
			Columns:        tableSchema.ColumnNames(),
			IncrementalKey: "updated_at",
		}))
	}

	before, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	beforeTasks, err := before.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	var equalityDeletesBefore int
	for _, task := range beforeTasks {
		equalityDeletesBefore += len(task.EqualityDeleteFiles)
	}
	require.Positive(t, equalityDeletesBefore)

	result, err := dest.MaintainTable(ctx, target, MaintenanceOptions{
		CompactDataFiles:    true,
		TargetFileSizeBytes: 1024 * 1024,
		MinInputFiles:       1,
		DeleteFileThreshold: 1,
	})
	require.NoError(t, err)
	require.Positive(t, result.RemovedEqualityDeleteFiles)

	after, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	afterTasks, err := after.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	for _, task := range afterTasks {
		require.Empty(t, task.EqualityDeleteFiles)
	}
	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Equal(t, "v5", rows[int64(1)][1])
	require.Equal(t, "unchanged", rows[int64(2)][1])
}

func TestMaintainTableExpiresSnapshotsThenDeletesOnlyGraceAgedOrphans(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.expiration"
	tableSchema := lifecycleTestSchema()

	for value := int64(1); value <= 5; value++ {
		writeTableRows(t, dest, tableName, tableSchema, true, [][]any{{value}})
		time.Sleep(2 * time.Millisecond)
	}
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	require.Len(t, tbl.Metadata().Snapshots(), 5)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	orphan := filepath.Join(location, "data", "abandoned-write.parquet")
	require.NoError(t, os.WriteFile(orphan, []byte("not parquet"), 0o600))
	ageFiles(t, location, 25*time.Hour)

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		ExpireSnapshots:    true,
		SnapshotMaxAge:     time.Millisecond,
		MinSnapshotsToKeep: 2,
		DeleteOrphanFiles:  true,
		OrphanFileAge:      24 * time.Hour,
	})
	require.NoError(t, err)
	require.Equal(t, 5, result.SnapshotsBefore)
	require.Equal(t, 2, result.SnapshotsAfter)
	require.Positive(t, result.DeletedOrphanFiles)
	require.NoFileExists(t, orphan)
	require.EqualValues(t, 1, icebergRowCount(ctx, t, dest, tableName))
	rows := readTableRows(t, dest, tableName)
	require.Equal(t, int64(5), rows.Rows[0][0])
}

func TestMaintainTableExpirationCarriesCommitTokenAndCDCCursorForward(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.lineage_expiration"
	tableSchema := lifecycleTestSchema()
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: tableName, Schema: tableSchema}))
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(1)}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table:        tableName,
		Schema:       tableSchema,
		CommitToken:  "cursor-snapshot-token",
		CDCResumeLSN: "0/ABCDEF",
	}))
	for value := int64(2); value <= 5; value++ {
		writeTableRows(t, dest, tableName, tableSchema, false, [][]any{{value}})
		time.Sleep(2 * time.Millisecond)
	}

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		ExpireSnapshots:    true,
		SnapshotMaxAge:     time.Millisecond,
		MinSnapshotsToKeep: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.SnapshotsAfter)
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	require.Equal(t, "0/ABCDEF", latestCDCResumeLSN(tbl))
	require.True(t, tableHasCommitToken(tbl, commitTokenID("cursor-snapshot-token")))
	require.Equal(t, "0/ABCDEF", tbl.CurrentSnapshot().Summary.Properties[snapshotCDCResumeLSNKey])
	require.Equal(t, commitTokenID("cursor-snapshot-token"), tbl.CurrentSnapshot().Summary.Properties[snapshotCommitTokenKey])
	require.EqualValues(t, 5, icebergRowCount(ctx, t, dest, tableName))
}

func TestMaintainTableTrimsAndDeletesOldMetadata(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.metadata_cleanup"
	tableSchema := lifecycleTestSchema()

	for value := int64(1); value <= 6; value++ {
		writeTableRows(t, dest, tableName, tableSchema, false, [][]any{{value}})
	}
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	metadataDir := filepath.Join(location, "metadata")
	metadataBefore := metadataJSONFiles(t, metadataDir)
	require.Greater(t, len(metadataBefore), 3)
	ageFiles(t, location, 25*time.Hour)

	result, err := dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		DeleteOrphanFiles:     true,
		OrphanFileAge:         24 * time.Hour,
		PreviousMetadataFiles: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.PreviousMetadataVersionsKept)
	require.Positive(t, result.DeletedOrphanFiles)
	metadataAfter := metadataJSONFiles(t, metadataDir)
	require.Less(t, len(metadataAfter), len(metadataBefore))
	// Current metadata plus at most two retained previous versions.
	require.LessOrEqual(t, len(metadataAfter), 3)
	require.EqualValues(t, 6, icebergRowCount(ctx, t, dest, tableName))
}

func TestMaintainTableRejectsUnsafeOrphanDeletionWindow(t *testing.T) {
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.unsafe_orphans", lifecycleTestSchema(), false, [][]any{{int64(1)}})

	_, err := dest.MaintainTable(context.Background(), "maintenance.unsafe_orphans", MaintenanceOptions{
		DeleteOrphanFiles: true,
		OrphanFileAge:     time.Hour,
	})
	require.ErrorContains(t, err, "minimum is 24h0m0s")
}

func TestConnectRejectsNamespaceCollidingLocationTemplate(t *testing.T) {
	ctx := context.Background()
	warehouse := t.TempDir()
	dest := NewDestination()
	uri := "iceberg+sqlite://" + filepath.Join(t.TempDir(), "catalog.db") + "?warehouse_path=" + url.QueryEscape(warehouse) +
		"&table_location=" + url.QueryEscape(filepath.Join(warehouse, "{table}"))
	err := dest.Connect(ctx, uri)
	require.ErrorContains(t, err, "both a namespace and {table}")
}

func TestOrphanCleanupTemplateRequiresNamespaceAndTable(t *testing.T) {
	for _, tt := range []struct {
		template string
		wantErr  bool
	}{
		{template: "s3://bucket/{table}", wantErr: true},
		{template: "s3://bucket/fixed", wantErr: true},
		{template: "s3://bucket/{namespace}", wantErr: true},
		{template: "s3://bucket/{namespace}/{table}"},
		{template: "s3://bucket/{namespace_dot}/{table}"},
		{template: "s3://bucket/{identifier}"},
		{template: "s3://bucket/{identifier_dot}"},
	} {
		err := validateOrphanCleanupTemplate(tt.template)
		if tt.wantErr {
			require.Error(t, err, tt.template)
		} else {
			require.NoError(t, err, tt.template)
		}
	}
}

func TestOrphanCleanupIsolationUsesVerifiedConfiguredTableLocation(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.target", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	target, err := dest.loadIcebergTable(ctx, "maintenance.target")
	require.NoError(t, err)
	warehouse := dest.cfg.Properties.Get("warehouse", "")
	dest.cfg.TableLocation = filepath.Join(warehouse, "{namespace}", "{table}")

	wrapped := &namespaceListingFailureCatalog{Catalog: dest.catalog, err: errors.New("nested namespace pagination failed")}
	dest.catalog = wrapped
	require.NoError(t, dest.validateOrphanCleanupLocation(ctx, target.Identifier(), target.Location()))
	require.Zero(t, wrapped.calls, "an exact injective table-location template must not require a catalog-wide scan")
}

func TestOrphanCleanupIsolationAllowsDescendantOfVerifiedConfiguredLocation(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.target", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	target, err := dest.loadIcebergTable(ctx, "maintenance.target")
	require.NoError(t, err)
	warehouse := dest.cfg.Properties.Get("warehouse", "")
	dest.cfg.TableLocation = filepath.Join(warehouse, "{namespace}", "{table}")
	descendant := filepath.Join(target.Location(), "_ingestr_connection_check_unique")
	require.NoError(t, os.MkdirAll(descendant, 0o755))
	descendantRoot, err := canonicalTableRoot(descendant)
	require.NoError(t, err)

	isolated, err := dest.configuredTableLocationProvesIsolation(target.Identifier(), descendant, descendantRoot)
	require.NoError(t, err)
	require.True(t, isolated)
}

func TestOrphanCleanupIsolationRejectsConfiguredLocationMismatchWithoutBypass(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.target", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	target, err := dest.loadIcebergTable(ctx, "maintenance.target")
	require.NoError(t, err)
	warehouse := dest.cfg.Properties.Get("warehouse", "")
	dest.cfg.TableLocation = filepath.Join(warehouse, "different", "{namespace}", "{table}")

	wrapped := &namespaceListingFailureCatalog{Catalog: dest.catalog, err: errors.New("must not be reached")}
	dest.catalog = wrapped
	err = dest.validateOrphanCleanupLocation(ctx, target.Identifier(), target.Location())
	require.ErrorContains(t, err, "is not within configured isolated location")
	require.Zero(t, wrapped.calls)
}

func TestOrphanCleanupIsolationStillScansCatalogAssignedLocations(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.target", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	target, err := dest.loadIcebergTable(ctx, "maintenance.target")
	require.NoError(t, err)

	scanErr := errors.New("catalog namespace scan failed")
	wrapped := &namespaceListingFailureCatalog{Catalog: dest.catalog, err: scanErr}
	dest.catalog = wrapped
	err = dest.validateOrphanCleanupLocation(ctx, target.Identifier(), target.Location())
	require.ErrorIs(t, err, scanErr)
	require.Equal(t, 1, wrapped.calls)
}

func TestOrphanCleanupIsolationSupportsFlatHiveNamespaces(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "maintenance.target", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	writeTableRows(t, dest, "maintenance.sibling", lifecycleTestSchema(), false, [][]any{{int64(2)}})
	target, err := dest.loadIcebergTable(ctx, "maintenance.target")
	require.NoError(t, err)

	dest.catalog = &flatNamespaceOnlyCatalog{Catalog: dest.catalog}
	dest.cfg.Properties["type"] = "hive"
	require.NoError(t, dest.validateOrphanCleanupLocation(ctx, target.Identifier(), target.Location()))
}

func TestMaintainTableUnknownCommitNeverDeletesFiles(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableName := "maintenance.unknown_commit"
	tableSchema := lifecycleTestSchema()
	for value := int64(1); value <= 3; value++ {
		writeTableRows(t, dest, tableName, tableSchema, true, [][]any{{value}})
		time.Sleep(2 * time.Millisecond)
	}
	tbl, err := dest.loadIcebergTable(ctx, tableName)
	require.NoError(t, err)
	location, ok := localFilesystemPath(tbl.Location())
	require.True(t, ok)
	orphan := filepath.Join(location, "data", "unknown-status-orphan.parquet")
	require.NoError(t, os.WriteFile(orphan, []byte("keep until reconciled"), 0o600))
	ageFiles(t, location, 25*time.Hour)

	base := dest.catalog
	dest.catalog = &unknownAfterCommitCatalog{Catalog: base}
	_, err = dest.MaintainTable(ctx, tableName, MaintenanceOptions{
		ExpireSnapshots:    true,
		SnapshotMaxAge:     time.Millisecond,
		MinSnapshotsToKeep: 1,
		DeleteOrphanFiles:  true,
		OrphanFileAge:      24 * time.Hour,
	})
	require.ErrorContains(t, err, "unknown commit status")
	require.FileExists(t, orphan)
	dest.catalog = base
}

func TestConfiguredMaintenanceOptionsAreOffByDefaultAndRateLimited(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	opts, due, err := configuredMaintenanceOptions(iceberggo.Properties{}, now)
	require.NoError(t, err)
	require.False(t, due)
	require.Equal(t, MaintenanceOptions{}, opts)

	props := iceberggo.Properties{
		maintenanceEnabledProperty:          "true",
		maintenanceIntervalMSProperty:       strconvDuration(time.Hour),
		maintenanceLastRunAtProperty:        now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
		maintenanceCompactDataProperty:      "false",
		maintenanceDeleteOrphansProperty:    "false",
		maintenanceExpireSnapshotsProperty:  "true",
		maintenanceMinSnapshotsProperty:     "7",
		maintenanceSnapshotMaxAgeMSProperty: strconvDuration(48 * time.Hour),
	}
	_, due, err = configuredMaintenanceOptions(props, now)
	require.NoError(t, err)
	require.False(t, due)

	props[maintenanceLastRunAtProperty] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	opts, due, err = configuredMaintenanceOptions(props, now)
	require.NoError(t, err)
	require.True(t, due)
	require.True(t, opts.ExpireSnapshots)
	require.Equal(t, 48*time.Hour, opts.SnapshotMaxAge)
	require.Equal(t, 7, opts.MinSnapshotsToKeep)
	require.False(t, opts.CompactDataFiles)
	require.False(t, opts.DeleteOrphanFiles)
	require.True(t, opts.EnableManifestMerge)

	props[maintenanceManifestMergeProperty] = "false"
	opts, due, err = configuredMaintenanceOptions(props, now)
	require.NoError(t, err)
	require.True(t, due)
	require.False(t, opts.EnableManifestMerge)
	require.True(t, opts.configureManifestMerge)
}

func TestConnectRejectsUnsafeOrInvalidMaintenanceConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "unsafe orphan window",
			query:   "&table." + maintenanceEnabledProperty + "=true&table." + maintenanceOrphanAgeMSProperty + "=3600000",
			wantErr: "orphan file age 1h0m0s is unsafe",
		},
		{
			name:    "invalid boolean",
			query:   "&table." + maintenanceEnabledProperty + "=true&table." + maintenanceCompactDataProperty + "=sometimes",
			wantErr: "invalid ingestr.maintenance.compact-data-files value",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dest := NewDestination()
			err := dest.Connect(context.Background(), "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())+tt.query)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRunConfiguredMaintenanceRecordsSuccessfulRun(t *testing.T) {
	ctx := context.Background()
	warehouse := t.TempDir()
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(warehouse) +
		"&table." + maintenanceEnabledProperty + "=true" +
		"&table." + maintenanceCompactDataProperty + "=false" +
		"&table." + maintenanceExpireSnapshotsProperty + "=false" +
		"&table." + maintenanceDeleteOrphansProperty + "=false"
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
	writeTableRows(t, dest, "maintenance.configured", lifecycleTestSchema(), false, [][]any{{int64(1)}})

	dest.runConfiguredMaintenance(ctx, "maintenance.configured")
	tbl, err := dest.loadIcebergTable(ctx, "maintenance.configured")
	require.NoError(t, err)
	_, err = time.Parse(time.RFC3339Nano, tbl.Properties()[maintenanceLastRunAtProperty])
	require.NoError(t, err)
	require.Equal(t, "true", tbl.Properties()[icebergtable.ManifestMergeEnabledKey])
}

func TestRunConfiguredMaintenanceSkipsRecreatedExpectedTarget(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	table := "maintenance.recreated_expected_target"
	tableSchema := lifecycleTestSchema()
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	original, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NoError(t, recreateTableWithCommitToken(ctx, dest.catalog, original.Identifier(), tableSchema, "recreated"))
	dest.cfg.TableProperties[maintenanceEnabledProperty] = "true"
	dest.cfg.TableProperties[maintenanceCompactDataProperty] = "false"
	dest.cfg.TableProperties[maintenanceExpireSnapshotsProperty] = "false"
	dest.cfg.TableProperties[maintenanceDeleteOrphansProperty] = "false"

	dest.runConfiguredMaintenanceExpected(ctx, table, original.Metadata().TableUUID().String())
	recreated, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	require.NotEqual(t, original.Metadata().TableUUID(), recreated.Metadata().TableUUID())
	require.Empty(t, recreated.Properties()[maintenanceLastRunAtProperty])
}

func TestConfiguredMaintenanceAppliesToAnExistingTable(t *testing.T) {
	ctx := context.Background()
	warehouse := t.TempDir()
	baseURI := "iceberg+hadoop://?warehouse=" + url.QueryEscape(warehouse)
	first := NewDestination()
	require.NoError(t, first.Connect(ctx, baseURI))
	writeTableRows(t, first, "maintenance.existing", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	require.NoError(t, first.Close(ctx))

	dest := NewDestination()
	require.NoError(t, dest.Connect(ctx, baseURI+
		"&table."+maintenanceEnabledProperty+"=true"+
		"&table."+maintenanceCompactDataProperty+"=false"+
		"&table."+maintenanceExpireSnapshotsProperty+"=false"+
		"&table."+maintenanceDeleteOrphansProperty+"=false"))
	t.Cleanup(func() { require.NoError(t, dest.Close(ctx)) })
	writeTableRows(t, dest, "maintenance.existing", lifecycleTestSchema(), false, [][]any{{int64(2)}})

	tbl, err := dest.loadIcebergTable(ctx, "maintenance.existing")
	require.NoError(t, err)
	_, err = time.Parse(time.RFC3339Nano, tbl.Properties()[maintenanceLastRunAtProperty])
	require.NoError(t, err)
	require.Equal(t, "true", tbl.Properties()[icebergtable.ManifestMergeEnabledKey])
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, "maintenance.existing"))
}

func TestPostCommitHookCleansExpiredManagedTablesHourlyAndExcludesActiveTable(t *testing.T) {
	ctx := context.Background()
	dest := newHadoopDestination(t)
	tableSchema := lifecycleTestSchema()
	writeLifecycleTable(t, dest, "maintenance.active_staging", tableSchema, time.Hour)
	writeLifecycleTable(t, dest, "maintenance.abandoned_staging", tableSchema, time.Hour)
	setManagedExpiration(t, dest, "maintenance.active_staging", time.Now().Add(-time.Hour))
	setManagedExpiration(t, dest, "maintenance.abandoned_staging", time.Now().Add(-time.Hour))
	// The setup writes legitimately claimed the namespace scan. Release it so
	// this call represents the next hourly opportunity.
	dest.mu.Lock()
	delete(dest.expirationScans, "maintenance")
	dest.mu.Unlock()

	dest.runConfiguredMaintenance(ctx, "maintenance.active_staging")
	for tableName, wantExists := range map[string]bool{
		"maintenance.active_staging":    true,
		"maintenance.abandoned_staging": false,
	} {
		exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier(tableName))
		require.NoError(t, err)
		require.Equal(t, wantExists, exists, tableName)
	}
	active, err := dest.loadIcebergTable(ctx, "maintenance.active_staging")
	require.NoError(t, err)
	activeExpiration, managed, err := managedTableExpiration(active.Properties())
	require.NoError(t, err)
	require.True(t, managed)
	require.True(t, activeExpiration.After(time.Now().Add(30*time.Minute)), "active lease must be refreshed")

	writeLifecycleTable(t, dest, "maintenance.second_abandoned", tableSchema, time.Hour)
	setManagedExpiration(t, dest, "maintenance.second_abandoned", time.Now().Add(-time.Hour))
	dest.runConfiguredMaintenance(ctx, "maintenance.active_staging")
	exists, err := dest.tableExists(ctx, icebergcatalog.ToIdentifier("maintenance.second_abandoned"))
	require.NoError(t, err)
	require.True(t, exists, "second scan must be throttled")

	dest.mu.Lock()
	dest.expirationScans["maintenance"] = time.Now().Add(-2 * time.Hour).UnixNano()
	dest.mu.Unlock()
	dest.runConfiguredMaintenance(ctx, "maintenance.active_staging")
	exists, err = dest.tableExists(ctx, icebergcatalog.ToIdentifier("maintenance.second_abandoned"))
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = dest.tableExists(ctx, icebergcatalog.ToIdentifier("maintenance.active_staging"))
	require.NoError(t, err)
	require.True(t, exists, "active table must always be excluded")
}

type flatNamespaceOnlyCatalog struct {
	icebergcatalog.Catalog
}

type namespaceListingFailureCatalog struct {
	icebergcatalog.Catalog
	err   error
	calls int
}

func (c *namespaceListingFailureCatalog) ListNamespaces(context.Context, icebergtable.Identifier) ([]icebergtable.Identifier, error) {
	c.calls++
	return nil, c.err
}

func (c *flatNamespaceOnlyCatalog) ListNamespaces(ctx context.Context, parent icebergtable.Identifier) ([]icebergtable.Identifier, error) {
	if len(parent) > 0 {
		return nil, errors.New("hierarchical namespace is not supported")
	}
	return c.Catalog.ListNamespaces(ctx, parent)
}

type unknownAfterCommitCatalog struct {
	icebergcatalog.Catalog
}

func (c *unknownAfterCommitCatalog) LoadTable(ctx context.Context, ident icebergtable.Identifier) (*icebergtable.Table, error) {
	tbl, err := c.Catalog.LoadTable(ctx, ident)
	if err != nil {
		return nil, err
	}
	return icebergtable.New(
		tbl.Identifier(),
		tbl.Metadata(),
		tbl.MetadataLocation(),
		func(ctx context.Context) (icebergio.IO, error) { return tbl.FS(ctx) },
		c,
	), nil
}

func (c *unknownAfterCommitCatalog) CommitTable(
	ctx context.Context,
	ident icebergtable.Identifier,
	requirements []icebergtable.Requirement,
	updates []icebergtable.Update,
) (icebergtable.Metadata, string, error) {
	metadata, location, err := c.Catalog.CommitTable(ctx, ident, requirements, updates)
	if err != nil {
		return metadata, location, err
	}
	return metadata, location, errors.New("unknown commit status")
}

func ageFiles(t *testing.T, root string, age time.Duration) {
	t.Helper()
	then := time.Now().Add(-age)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return os.Chtimes(path, then, then)
	}))
}

func metadataJSONFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".json" {
			files = append(files, path)
		}
		return nil
	}))
	return files
}

func strconvDuration(value time.Duration) string {
	return fmt.Sprintf("%d", value.Milliseconds())
}

func setManagedExpiration(t *testing.T, dest *Destination, tableName string, expiration time.Time) {
	t.Helper()
	tbl, err := dest.loadIcebergTable(context.Background(), tableName)
	require.NoError(t, err)
	txn := tbl.NewTransaction()
	require.NoError(t, txn.SetProperties(iceberggo.Properties{
		managedTableExpiresAt: expiration.UTC().Format(time.RFC3339Nano),
	}))
	_, err = txn.Commit(context.Background())
	require.NoError(t, err)
}

func TestOrphanCleanupRejectsTargetNestedInsideAnotherTable(t *testing.T) {
	dest := newHadoopDestination(t)
	writeTableRows(t, dest, "ns.b", lifecycleTestSchema(), false, [][]any{{int64(1)}})
	writeTableRows(t, dest, "ns.b.data", lifecycleTestSchema(), false, [][]any{{int64(2)}})

	_, err := dest.MaintainTable(context.Background(), "ns.b.data", MaintenanceOptions{
		DeleteOrphanFiles: true,
		OrphanFileAge:     24 * time.Hour,
	})
	require.ErrorContains(t, err, "overlapping location")
}
