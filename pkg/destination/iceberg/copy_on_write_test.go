package iceberg

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestDirectCopyOnWriteMergeSpillsAndPreservesClustering(t *testing.T) {
	forceCopyOnWriteSpills(t)
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })

	ctx := context.Background()
	target := "lake.cow_spill.direct"
	tableSchema := sortAuditSchema()
	initial := make([][]any, 0, 24)
	for id := int64(0); id < 24; id++ {
		initial = append(initial, []any{id, 100 - id, id})
	}
	prepareClusteredTable(t, dest, target, tableSchema, []string{"cluster_key"}, false, initial)

	updates := make([][]any, 0, 30)
	for id := int64(12); id < 36; id++ {
		updates = append(updates, []any{id, 200 - id, id})
		if id%3 == 0 {
			updates = append(updates, []any{id, 300 - id, id + 100})
		}
	}
	merge := func() {
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), updates)
		require.NoError(t, err)
		require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: tableSchema, Parallelism: 4,
		}, destination.MergeOptions{
			TargetTable: target, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at",
			Columns: tableSchema.ColumnNames(), CommitToken: "forced-cow-spill",
		}))
	}

	merge()
	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 36)
	require.Equal(t, int64(288), rows[int64(12)][1])
	require.Equal(t, int64(112), rows[int64(12)][2])
	firstSnapshots := icebergSnapshotCount(t, dest, target)
	merge()
	require.Equal(t, firstSnapshots, icebergSnapshotCount(t, dest, target))
	assertPhysicalSortOrder(t, dest, target)
	assertNoCopyOnWriteSpills(t)
}

func TestCopyOnWriteRetryRejectsRecreatedTargetWithMatchingToken(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table.write.merge.mode": []string{"copy-on-write"},
	})
	ctx := context.Background()
	target := "lake.cow_retry_recreated.target"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{{int64(1), "before", 1.0, int64(1)}})
	original, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	const token = "cow-recreated-target-token"
	dest.catalog = &recreateWithCommitTokenOnConflictCatalog{
		Catalog: dest.catalog,
		schema:  tableSchema,
		token:   token,
	}
	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "after", 2.0, int64(2)},
	})
	require.NoError(t, err)

	err = dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: tableSchema,
		CDCExpectedIncarnation: original.Metadata().TableUUID().String(),
	}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at",
		Columns: tableSchema.ColumnNames(), CommitToken: token,
	})
	require.ErrorContains(t, err, "incarnation changed")
	recreated, loadErr := dest.loadIcebergTable(ctx, target)
	require.NoError(t, loadErr)
	require.NotEqual(t, original.Metadata().TableUUID(), recreated.Metadata().TableUUID())
}

func TestCopyOnWriteDerivedTokenFastPathRejectsRecreatedTarget(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table.write.merge.mode": []string{"copy-on-write"},
	})
	ctx := context.Background()
	target := "lake.cow_derived_token_recreated.target"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{{int64(1), "before", 1.0, int64(1)}})
	merge := func(expectedIncarnation string) error {
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
			{int64(1), "after", 2.0, int64(2)},
		})
		require.NoError(t, err)
		return dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: tableSchema, CDCExpectedIncarnation: expectedIncarnation,
		}, destination.MergeOptions{
			TargetTable: target, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at",
			Columns: tableSchema.ColumnNames(), CDCExpectedIncarnation: expectedIncarnation,
		})
	}
	require.NoError(t, merge(""))
	original, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	dest.catalog = &recreateOnNthLoadCatalog{
		Catalog: dest.catalog, schema: tableSchema, recreateAt: 3,
	}

	err = merge(original.Metadata().TableUUID().String())
	require.ErrorContains(t, err, "incarnation changed")
	recreated, loadErr := dest.loadIcebergTable(ctx, target)
	require.NoError(t, loadErr)
	require.NotEqual(t, original.Metadata().TableUUID(), recreated.Metadata().TableUUID())
}

func TestCopyOnWriteMergePreservesV3Lineage(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{
		"table.format-version":   []string{"3"},
		"table.write.merge.mode": []string{"copy-on-write"},
	})
	ctx := context.Background()
	target := "lake.cow_lineage.target"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{
		{int64(1), "before", 1.0, int64(1)},
		{int64(2), "untouched", 2.0, int64(1)},
	})
	before, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	lineageBefore := readV3LineageByID(t, before)

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{
		{int64(1), "after", 10.0, int64(2)},
		{int64(3), "inserted", 3.0, int64(2)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: target, Schema: tableSchema}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, IncrementalKey: "updated_at", Columns: tableSchema.ColumnNames(),
	}))
	after, err := dest.loadIcebergTable(ctx, target)
	require.NoError(t, err)
	lineageAfter := readV3LineageByID(t, after)
	require.Equal(t, lineageBefore[int64(2)], lineageAfter[int64(2)], "untouched rows preserve complete lineage")
	require.Equal(t, lineageBefore[int64(1)][0], lineageAfter[int64(1)][0], "updated rows preserve row identity")
	require.Greater(t, lineageAfter[int64(1)][1], lineageBefore[int64(1)][1], "updated rows receive a new sequence")
	require.NotEqual(t, lineageAfter[int64(1)][0], lineageAfter[int64(3)][0], "inserted rows receive a new identity")
}

func TestMergeRetriesConcurrentNonOverlappingCommitConflict(t *testing.T) {
	for _, copyOnWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("copy_on_write_%t", copyOnWrite), func(t *testing.T) {
			uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir())
			if copyOnWrite {
				uri += "&table.write.merge.mode=copy-on-write"
			}
			dest := NewDestination()
			require.NoError(t, dest.Connect(context.Background(), uri))
			t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
			ctx := context.Background()
			target := fmt.Sprintf("lake.concurrent_retry.mode_%t", copyOnWrite)
			tableSchema := mergeTestSchema()
			writeTableRows(t, dest, target, tableSchema, false, [][]any{{int64(1), "initial", 1.0, int64(1)}})
			dest.catalog = &conflictFirstOfTwoCatalog{
				Catalog: dest.catalog, firstReady: make(chan struct{}), secondCommitted: make(chan struct{}),
			}
			errs := make(chan error, 2)
			for _, id := range []int64{2, 3} {
				id := id
				go func() {
					batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{id, fmt.Sprintf("row-%d", id), float64(id), id}})
					if err == nil {
						err = dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: target, Schema: tableSchema}, destination.MergeOptions{
							TargetTable: target, PrimaryKeys: []string{"id"}, Columns: tableSchema.ColumnNames(), CommitToken: fmt.Sprintf("retry-%d", id),
						})
					}
					errs <- err
				}()
			}
			require.NoError(t, <-errs)
			require.NoError(t, <-errs)
			require.Len(t, readTableRows(t, dest, target).Rows, 3)
		})
	}
}

func TestCopyOnWriteMergeRejectsDuplicateTargetKeysBeforeCommit(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	ctx := context.Background()
	target, staging := "lake.cow_duplicates.target", "lake.cow_duplicates.staging"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{
		{int64(1), "first", 1.0, int64(1)},
		{int64(1), "duplicate", 2.0, int64(2)},
	})
	writeTableRows(t, dest, staging, tableSchema, true, [][]any{{int64(1), "update", 3.0, int64(3)}})
	before := icebergSnapshotCount(t, dest, target)

	err := dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", Columns: tableSchema.ColumnNames(),
	})
	require.ErrorContains(t, err, "contains duplicate primary key")
	require.Equal(t, before, icebergSnapshotCount(t, dest, target))
	require.Len(t, readTableRows(t, dest, target).Rows, 2)
}

func TestDirectCDCEqualCursorUsesContentIdentity(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow_content.equal_cursor"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))

	merge := func(id int64, name string) {
		t.Helper()
		batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
			{id, name, "payload", "0/100", false, int64(100), nil},
		})
		require.NoError(t, err)
		require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: inputSchema,
		}, destination.MergeOptions{
			TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
		}))
	}

	merge(1, "first")
	merge(2, "second")
	require.Len(t, readTableRows(t, dest, target).Rows, 2)
	afterDistinct := icebergSnapshotCount(t, dest, target)
	merge(2, "second")
	require.Equal(t, afterDistinct, icebergSnapshotCount(t, dest, target))
}

func TestDirectCDCContentIdentityIgnoresIndependentKeyOrder(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow_content.key_order"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))
	merge := func(rows [][]any) {
		batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), rows)
		require.NoError(t, err)
		require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
			Table: target, Schema: inputSchema,
		}, destination.MergeOptions{
			TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
		}))
	}
	first := []any{int64(1), "first", "one", "0/100", false, int64(100), nil}
	second := []any{int64(2), "second", "two", "0/100", false, int64(100), nil}
	merge([][]any{first, second})
	beforeRetry := icebergSnapshotCount(t, dest, target)
	merge([][]any{second, first})
	require.Equal(t, beforeRetry, icebergSnapshotCount(t, dest, target))
}

func TestCDCDeleteWinsEqualLSNTieInBothOrdersWithSpill(t *testing.T) {
	forceCopyOnWriteSpills(t)
	for _, deleteFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("delete_first_%t", deleteFirst), func(t *testing.T) {
			dest := newHadoopDestination(t)
			ctx := context.Background()
			target := fmt.Sprintf("lake.cow_tie.order_%t", deleteFirst)
			inputSchema := cdcSchema(true)
			targetSchema := destination.DestinationTableSchema(inputSchema)
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
			}))
			update := []any{int64(1), "updated", "payload", "0/100", false, int64(100), nil}
			deleted := []any{int64(1), nil, nil, "0/100", true, int64(101), nil}
			rows := [][]any{update, deleted}
			if deleteFirst {
				rows = [][]any{deleted, update}
			}
			batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), rows)
			require.NoError(t, err)
			require.NoError(t, dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
				Table: target, Schema: inputSchema,
			}, destination.MergeOptions{
				TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
			}))
			got := readTableRows(t, dest, target)
			require.Len(t, got.Rows, 1)
			require.Equal(t, true, got.Value(got.Rows[0], destination.CDCDeletedColumn))
			require.Equal(t, "updated", got.Value(got.Rows[0], "name"))
		})
	}
}

func TestDirectCDCRejectsAnyStaleEventBeforeCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow_content.stale_event"
	inputSchema := cdcSchema(true)
	targetSchema := destination.DestinationTableSchema(inputSchema)
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))
	require.NoError(t, dest.CommitWriteToken(ctx, target, "checkpoint", "0/100"))
	before := icebergSnapshotCount(t, dest, target)

	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
		{int64(1), "stale", "payload", "0/090", false, int64(90), nil},
		{int64(2), "newer", "payload", "0/110", false, int64(110), nil},
	})
	require.NoError(t, err)
	err = dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: inputSchema,
	}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
	})
	require.ErrorContains(t, err, "stale CDC resume position in event")
	require.Equal(t, before, icebergSnapshotCount(t, dest, target))
	require.Empty(t, readTableRows(t, dest, target).Rows)
}

func TestCDCUnchangedColumnsMatchCaseInsensitively(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target, staging := "lake.cow_case.target", "lake.cow_case.staging"
	targetSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "Payload", DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
	}}
	stagingSchema := &schema.TableSchema{Columns: append([]schema.Column(nil), targetSchema.Columns...)}
	for i := range stagingSchema.Columns {
		if destination.IsCDCMetaColumn(stagingSchema.Columns[i].Name) {
			stagingSchema.Columns[i].Name = strings.ToUpper(stagingSchema.Columns[i].Name)
		}
	}
	stagingSchema.Columns = append(stagingSchema.Columns, schema.Column{
		Name: strings.ToUpper(destination.CDCUnchangedColsColumn), DataType: schema.TypeString, Nullable: true,
	})
	writeTableRows(t, dest, target, targetSchema, false, [][]any{
		{int64(1), "keep", "0/100", false, int64(100)},
	})
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), nil, "0/110", false, int64(110), `["payload"]`},
	})
	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"}, Columns: stagingSchema.ColumnNames(),
	}))
	rows := readTableRows(t, dest, target)
	require.Equal(t, "keep", rows.Value(rows.Rows[0], "Payload"))
}

func TestDirectCDCMixedCaseMetadataUsesCDCValidation(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow_case.direct_metadata"
	targetSchema := cdcSchema(false)
	inputSchema := cdcSchema(true)
	for i := range inputSchema.Columns {
		if destination.IsCDCMetaColumn(inputSchema.Columns[i].Name) || destination.IsCDCStagingOnlyColumn(inputSchema.Columns[i].Name) {
			inputSchema.Columns[i].Name = strings.ToUpper(inputSchema.Columns[i].Name)
		}
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: target, Schema: targetSchema, PrimaryKeys: []string{"id"},
	}))
	require.NoError(t, dest.CommitWriteToken(ctx, target, "mixed-case-checkpoint", "0/100"))
	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
		{int64(1), "stale", "payload", "0/090", false, int64(90), nil},
		{int64(2), "newer", "payload", "0/110", false, int64(110), nil},
	})
	require.NoError(t, err)
	err = dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: inputSchema,
	}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
	})
	require.ErrorContains(t, err, "stale CDC resume position in event")
	require.Empty(t, readTableRows(t, dest, target).Rows)
}

func TestCopyOnWriteRetriesDefiniteCommitFailureAndRemovesFirstAttemptFiles(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	ctx := context.Background()
	target, staging := "lake.cow_cleanup.target", "lake.cow_cleanup.staging"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{{int64(1), "before", 1.0, int64(1)}})
	writeTableRows(t, dest, staging, tableSchema, true, [][]any{{int64(1), "after", 2.0, int64(2)}})
	before := allTableParquetFiles(t, dest, target)
	base := dest.catalog
	dest.catalog = &failNthCommitCatalog{Catalog: base, failAt: 1, err: icebergtable.ErrCommitFailed}

	err := dest.MergeTable(ctx, destination.MergeOptions{
		TargetTable: target, StagingTable: staging, PrimaryKeys: []string{"id"},
		IncrementalKey: "updated_at", Columns: tableSchema.ColumnNames(),
	})
	require.NoError(t, err)
	require.NotEqual(t, before, allTableParquetFiles(t, dest, target))
	require.Len(t, allTableParquetFiles(t, dest, target), len(before)+1)
}

func TestCopyOnWriteCancellationRemovesGeneratedFiles(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.target-file-size-bytes=1"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	target := "lake.cow_cleanup.canceled"
	tableSchema := mergeTestSchema()
	writeTableRows(t, dest, target, tableSchema, false, [][]any{{int64(1), "before", 1.0, int64(1)}})
	before := allTableParquetFiles(t, dest, target)
	tbl, err := dest.loadIcebergTable(context.Background(), target)
	require.NoError(t, err)
	first, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(1), "after", 2.0, int64(2)}})
	require.NoError(t, err)
	second, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(2), "new", 3.0, int64(3)}})
	require.NoError(t, err)
	batches := append(first, second...)
	inner, err := array.NewRecordReader(icebergArrowSchema(tableSchema), batches)
	require.NoError(t, err)
	defer inner.Release()
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterRecordReader{RecordReader: inner, cancel: cancel, cancelAt: 2}
	err = dest.replaceAllMergeRecords(ctx, tbl, reader, nil, destination.MergeOptions{TargetTable: target}, newCommitMetadata("cancel-cleanup", ""))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, before, allTableParquetFiles(t, dest, target))
}

func allTableParquetFiles(t *testing.T, dest *Destination, table string) []string {
	t.Helper()
	tbl, err := dest.loadIcebergTable(context.Background(), table)
	require.NoError(t, err)
	root := strings.TrimPrefix(tbl.Location(), "file://")
	var files []string
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".parquet") {
			files = append(files, path)
		}
		return err
	}))
	slices.Sort(files)
	return files
}

func TestClusterRecordReaderCancellationCleansSpills(t *testing.T) {
	forceCopyOnWriteSpills(t)
	sc := icebergArrowSchema(sortAuditSchema())
	rows := make([][]any, 0, 20)
	for i := int64(0); i < 20; i++ {
		rows = append(rows, []any{i, 20 - i, i})
	}
	batches, err := buildRecordBatches(sc, rows)
	require.NoError(t, err)
	inner, err := array.NewRecordReader(sc, batches)
	require.NoError(t, err)
	defer inner.Release()
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterRecordReader{RecordReader: inner, cancel: cancel}

	_, _, err = clusterRecordReader(ctx, reader, []string{"cluster_key"})
	require.ErrorIs(t, err, context.Canceled)
	assertNoCopyOnWriteSpills(t)
}

type cancelAfterRecordReader struct {
	array.RecordReader
	cancel   context.CancelFunc
	cancelAt int
	seen     int
}

func (r *cancelAfterRecordReader) Next() bool {
	next := r.RecordReader.Next()
	if next {
		r.seen++
	}
	if next && (r.cancelAt == 0 || r.seen == r.cancelAt) {
		r.cancel()
	}
	return next
}

func TestSpillIterationStopsAfterCancellation(t *testing.T) {
	forceCopyOnWriteSpills(t)
	sorter, err := newSpillSorter(icebergArrowSchema(sortAuditSchema()), []string{"id"})
	require.NoError(t, err)
	defer sorter.Close()
	for i := int64(0); i < 20; i++ {
		require.NoError(t, sorter.Add([]any{i, i, i}))
	}
	ctx, cancel := context.WithCancel(context.Background())
	it, err := sorter.IterContext(ctx)
	require.NoError(t, err)
	cancel()
	require.False(t, it.NextGroup())
	require.ErrorIs(t, it.Err(), context.Canceled)
	it.Close()
	sorter.Close()
	require.Eventually(t, func() bool {
		spills, _ := filepath.Glob(filepath.Join(os.TempDir(), "ingestr-iceberg-spill-*"))
		return len(spills) == 0
	}, time.Second, 10*time.Millisecond)
	require.True(t, errors.Is(ctx.Err(), context.Canceled))
}

func TestDirectCopyOnWriteMergeFailureCleansSpillsAndDoesNotCommit(t *testing.T) {
	forceCopyOnWriteSpills(t)
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow_spill.failure"
	inputSchema := cdcSchema(true)
	targetSchema := cdcSchema(false)
	writeTableRows(t, dest, target, targetSchema, false, [][]any{
		{int64(1), "one", "payload-1", "0/1", false, int64(1)},
		{int64(2), "two", "payload-2", "0/1", false, int64(1)},
		{int64(3), "three", "payload-3", "0/1", false, int64(1)},
	})
	beforeSnapshots := icebergSnapshotCount(t, dest, target)

	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
		{int64(1), "updated", nil, "0/10", false, int64(10), `not-json`},
		{int64(2), "updated", nil, "0/11", false, int64(11), `not-json`},
		{int64(4), "new", "payload-4", "0/12", false, int64(12), nil},
	})
	require.NoError(t, err)
	err = dest.MergeRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: target, Schema: inputSchema,
	}, destination.MergeOptions{
		TargetTable: target, PrimaryKeys: []string{"id"}, Columns: inputSchema.ColumnNames(),
		CommitToken: "forced-cow-failure",
	})
	require.ErrorContains(t, err, "invalid character")
	require.Equal(t, beforeSnapshots, icebergSnapshotCount(t, dest, target))
	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 3)
	require.Equal(t, "one", rows[int64(1)][1])
	assertNoCopyOnWriteSpills(t)
}

func forceCopyOnWriteSpills(t *testing.T) {
	t.Helper()
	oldRows, oldFanIn := spillRunRows, spillMergeFanIn
	spillRunRows, spillMergeFanIn = 2, 2
	t.Cleanup(func() {
		spillRunRows, spillMergeFanIn = oldRows, oldFanIn
	})
	t.Setenv("TMPDIR", t.TempDir())
}

func assertNoCopyOnWriteSpills(t *testing.T) {
	t.Helper()
	spills, err := filepath.Glob(filepath.Join(os.TempDir(), "ingestr-iceberg-spill-*"))
	require.NoError(t, err)
	require.Empty(t, spills)
}
