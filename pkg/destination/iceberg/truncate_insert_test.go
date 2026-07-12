package iceberg

import (
	"context"
	"errors"
	"testing"

	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestAtomicTruncateInsertPreservesTargetOnFailureAndDeduplicatesCommit(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_insert"
	targetSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "version", DataType: schema.TypeInt64, Nullable: false},
		{Name: "legacy", DataType: schema.TypeString, Nullable: true},
	}, PrimaryKeys: []string{"id"}}
	inputSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "version", DataType: schema.TypeInt64, Nullable: false},
	}, PrimaryKeys: []string{"id"}}
	writeTableRows(t, dest, table, targetSchema, false, [][]any{
		{int64(1), int64(10), "one"},
		{int64(2), int64(20), "two"},
	})

	beforeSnapshots := icebergSnapshotCount(t, dest, table)
	failedBatch, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{{int64(9), int64(90)}})
	require.NoError(t, err)
	trailingBatch, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{{int64(10), int64(100)}})
	require.NoError(t, err)
	failedRecords := make(chan source.RecordBatchResult, 3)
	failedRecords <- source.RecordBatchResult{Batch: failedBatch[0]}
	failedRecords <- source.RecordBatchResult{Err: errors.New("source failed after data")}
	failedRecords <- source.RecordBatchResult{Batch: trailingBatch[0]}
	close(failedRecords)
	err = dest.TruncateInsertRecords(ctx, failedRecords, destination.WriteOptions{
		Table: table, Schema: inputSchema, PrimaryKeys: []string{"id"},
		DeduplicatePrimaryKeys: true, IncrementalKey: "version",
	})
	require.ErrorContains(t, err, "source failed after data")
	require.Empty(t, failedRecords, "a failed atomic reload must release all remaining source batches")
	require.Equal(t, beforeSnapshots, icebergSnapshotCount(t, dest, table))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))
	require.Contains(t, readTableRows(t, dest, table).Columns, "legacy")

	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{
		{int64(2), int64(21)},
		{int64(3), int64(30)},
		{int64(2), int64(22)},
	})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: inputSchema, PrimaryKeys: []string{"id"},
		DeduplicatePrimaryKeys: true, IncrementalKey: "version",
	}))

	require.Equal(t, beforeSnapshots+1, icebergSnapshotCount(t, dest, table))
	rows := singleRowByKey(t, readTableRows(t, dest, table), "id")
	require.Len(t, rows, 2)
	require.Equal(t, int64(22), rows[int64(2)][1])
	require.Nil(t, rows[int64(2)][2])
	require.Equal(t, int64(30), rows[int64(3)][1])
	require.Nil(t, rows[int64(3)][2])
	require.Equal(t, "truncate+insert", latestSnapshotSummary(t, dest, table)["ingestr.operation"])
}

func TestAtomicTruncateInsertRetriesCommitConflictAndCleansFirstAttempt(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_retry"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	before := allTableParquetFiles(t, dest, table)
	dest.catalog = &failNthCommitCatalog{Catalog: dest.catalog, failAt: 1, err: icebergtable.ErrCommitFailed}
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "truncate-retry",
	}))
	require.Equal(t, [][]any{{int64(2)}}, readTableRows(t, dest, table).Rows)
	require.Len(t, allTableParquetFiles(t, dest, table), len(before)+1)
}

func TestAtomicTruncateInsertPreservesExternalPartitionFieldMetadata(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_external_partition"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	iceSchema, err := icebergSchemaFromTableSchema(tableSchema)
	require.NoError(t, err)
	spec, err := iceberggo.NewPartitionSpecOpts(iceberggo.AddPartitionFieldByName(
		"id",
		"external_bucket_name",
		iceberggo.BucketTransform{NumBuckets: 8},
		iceSchema,
		nil,
	))
	require.NoError(t, err)
	idField, ok := iceSchema.FindFieldByName("id")
	require.True(t, ok)
	sortOrder, err := icebergtable.NewSortOrder(1, []icebergtable.SortField{{
		SourceIDs: []int{idField.ID},
		Transform: iceberggo.IdentityTransform{},
		Direction: icebergtable.SortDESC,
		NullOrder: icebergtable.NullsLast,
	}})
	require.NoError(t, err)
	ident, err := parseIdentifier(table)
	require.NoError(t, err)
	require.NoError(t, dest.ensureNamespace(ctx, icebergcatalog.NamespaceFromIdent(ident)))
	require.NoError(t, dest.ensureLocalTableDirs(ident))
	_, err = dest.catalog.CreateTable(
		ctx, ident, iceSchema,
		icebergcatalog.WithPartitionSpec(&spec),
		icebergcatalog.WithSortOrder(sortOrder),
	)
	require.NoError(t, err)

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: table, Schema: tableSchema, PreserveExistingLayout: true,
	}))
	oldBatches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(1), "old"}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(oldBatches...), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))
	before, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	beforeSpec := before.Metadata().PartitionSpec()
	beforeField := onlyPartitionField(t, beforeSpec)
	beforeSortOrder := before.SortOrder()
	dest.cfg.PartitionSpec = "id"
	dest.cfg.TableProperties["external.should-not-be-applied"] = "configured"
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:                  table,
		Schema:                 tableSchema,
		PartitionBy:            "id",
		ClusterBy:              []string{"id"},
		PreserveExistingLayout: true,
	}))
	prepared, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	preparedSpec := prepared.Metadata().PartitionSpec()
	preparedField := onlyPartitionField(t, preparedSpec)
	require.Equal(t, beforeSpec.ID(), preparedSpec.ID())
	require.Equal(t, beforeField.Name, preparedField.Name)
	require.Equal(t, beforeSortOrder.OrderID(), prepared.SortOrder().OrderID())
	require.True(t, sortFieldsEqual(beforeSortOrder, prepared.SortOrder()))
	require.NotContains(t, prepared.Properties(), "external.should-not-be-applied")

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(2), "new"}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: tableSchema,
	}))

	after, err := dest.loadIcebergTable(ctx, table)
	require.NoError(t, err)
	afterSpec := after.Metadata().PartitionSpec()
	afterField := onlyPartitionField(t, afterSpec)
	require.Equal(t, beforeSpec.ID(), afterSpec.ID())
	require.Equal(t, beforeField.FieldID, afterField.FieldID)
	require.Equal(t, beforeField.SourceIDs, afterField.SourceIDs)
	require.Equal(t, beforeField.Name, afterField.Name)
	require.True(t, beforeField.Transform.Equals(afterField.Transform))
	require.Equal(t, "external_bucket_name", afterField.Name)
	require.Equal(t, beforeSortOrder.OrderID(), after.SortOrder().OrderID())
	require.True(t, sortFieldsEqual(beforeSortOrder, after.SortOrder()))
	tasks, err := after.Scan().PlanFiles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tasks)
	for _, task := range tasks {
		require.NotNil(t, task.File.SortOrderID())
		require.Equal(t, icebergtable.UnsortedSortOrderID, *task.File.SortOrderID())
	}
	require.Equal(t, [][]any{{int64(2), "new"}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertAlreadyCommittedTokenDrainsRetry(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_retry"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
	}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(2)}})
	require.NoError(t, err)
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "stable-reload"}
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), opts))
	beforeSnapshots := icebergSnapshotCount(t, dest, table)

	retryBatches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(999)}})
	require.NoError(t, err)
	retry := recordBatches(retryBatches...)
	require.NoError(t, dest.TruncateInsertRecords(ctx, retry, opts))
	require.Empty(t, retry)
	require.Equal(t, beforeSnapshots, icebergSnapshotCount(t, dest, table))
	require.Equal(t, [][]any{{int64(2)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertExplicitTokenRetryPreservesInterleavedAppend(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_explicit_token_lineage"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})
	opts := destination.WriteOptions{Table: table, Schema: tableSchema, CommitToken: "explicit-reload"}
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(int64Batch(t, 2)), opts))
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(3)}})
	beforeSnapshots := icebergSnapshotCount(t, dest, table)

	retry := recordBatches(int64Batch(t, 999))
	require.NoError(t, dest.TruncateInsertRecords(ctx, retry, opts))
	require.Empty(t, retry)
	require.Equal(t, beforeSnapshots, icebergSnapshotCount(t, dest, table))
	require.ElementsMatch(t, [][]any{{int64(2)}, {int64(3)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertReconcilesUnknownOutcomeAcrossInterleavedAppend(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_unknown_interleaved"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})

	base := dest.catalog
	unknown := errors.New("simulated lost truncate commit response")
	var hookErr error
	dest.catalog = &commitOutcomeCatalog{
		Catalog:         base,
		afterCommitErrs: []error{unknown},
		afterCommitHook: func() {
			tbl, err := base.LoadTable(ctx, icebergtable.Identifier{"lake", "correctness", "atomic_truncate_unknown_interleaved"})
			if err != nil {
				hookErr = err
				return
			}
			batch := int64Batch(t, 3)
			reader := newRecordBatchReader(ctx, recordBatches(batch), batch.Schema())
			defer reader.Release()
			_, hookErr = tbl.Append(ctx, reader, snapshotProps("append", newCommitMetadata("interleaved-after-truncate", "")))
		},
	}
	t.Cleanup(func() { dest.catalog = base })

	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(int64Batch(t, 2)), destination.WriteOptions{
		Table: table, Schema: tableSchema, CommitToken: "truncate-unknown-token",
	}))
	require.NoError(t, hookErr)
	require.ElementsMatch(t, [][]any{{int64(2)}, {int64(3)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertUsesEvolvedSchemaInSameReload(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_schema_evolution"
	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
	}}
	evolvedSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "value", DataType: schema.TypeString, Nullable: true},
		{Name: "new_value", DataType: schema.TypeInt64, Nullable: true},
	}}
	writeTableRows(t, dest, table, initialSchema, false, [][]any{{int64(1), "old"}})

	batches, err := buildRecordBatches(icebergArrowSchema(evolvedSchema), [][]any{{int64(2), "new", int64(42)}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{
		Table: table, Schema: evolvedSchema,
	}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, evolvedSchema.Columns, loaded.Columns)
	require.Equal(t, [][]any{{int64(2), "new", int64(42)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertRollsBackSchemaEvolutionWithFailedInput(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_schema_rollback"
	initialSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
	}}
	evolvedSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "new_value", DataType: schema.TypeString, Nullable: true},
	}}
	writeTableRows(t, dest, table, initialSchema, false, [][]any{{int64(1)}})
	beforeSnapshots := icebergSnapshotCount(t, dest, table)

	batches, err := buildRecordBatches(icebergArrowSchema(evolvedSchema), [][]any{{int64(2), "uncommitted"}})
	require.NoError(t, err)
	records := make(chan source.RecordBatchResult, 2)
	records <- source.RecordBatchResult{Batch: batches[0]}
	records <- source.RecordBatchResult{Err: errors.New("source failed")}
	close(records)

	err = dest.TruncateInsertRecords(ctx, records, destination.WriteOptions{Table: table, Schema: evolvedSchema})
	require.ErrorContains(t, err, "source failed")
	require.Equal(t, beforeSnapshots, icebergSnapshotCount(t, dest, table))
	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, initialSchema.Columns, loaded.Columns)
	require.Equal(t, [][]any{{int64(1)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertContentRetryDoesNotIgnoreLaterDataCommits(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_content_lineage"
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, tableSchema, false, [][]any{{int64(1)}})

	reload := func() {
		batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(10)}})
		require.NoError(t, err)
		require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: tableSchema}))
	}
	reload()
	appended, err := buildRecordBatches(icebergArrowSchema(tableSchema), [][]any{{int64(99)}})
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(appended...), destination.WriteOptions{Table: table, Schema: tableSchema}))
	require.EqualValues(t, 2, icebergRowCount(ctx, t, dest, table))

	reload()
	require.Equal(t, [][]any{{int64(10)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertSoftRemovesRequiredColumn(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_soft_remove"
	initial := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "legacy", DataType: schema.TypeString, Nullable: false},
	}}
	desired := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1), "old"}})

	batches, err := buildRecordBatches(icebergArrowSchema(desired), [][]any{{int64(2)}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: desired}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Len(t, loaded.Columns, 2)
	require.True(t, loaded.Columns[1].Nullable)
	require.Equal(t, [][]any{{int64(2), nil}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertStripsStagingOnlyCDCColumns(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_cdc_projection"
	targetSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	inputSchema := &schema.TableSchema{Columns: []schema.Column{
		targetSchema.Columns[0],
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
	}}
	writeTableRows(t, dest, table, targetSchema, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(inputSchema), [][]any{{int64(2), `["payload"]`}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: inputSchema}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, targetSchema.Columns, loaded.Columns)
	require.Equal(t, [][]any{{int64(2)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertTypeEvolutionIsAtomic(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_type_evolution"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt32, Nullable: false}}}
	widened := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeInt64, Nullable: false}}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(widened), [][]any{{int64(1 << 40)}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: widened}))
	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, schema.TypeInt64, loaded.Columns[0].DataType)
	require.Equal(t, [][]any{{int64(1 << 40)}}, readTableRows(t, dest, table).Rows)

	invalid := &schema.TableSchema{Columns: []schema.Column{{Name: "value", DataType: schema.TypeBoolean, Nullable: false}}}
	invalidBatches, err := buildRecordBatches(icebergArrowSchema(invalid), [][]any{{true}})
	require.NoError(t, err)
	err = dest.TruncateInsertRecords(ctx, recordBatches(invalidBatches...), destination.WriteOptions{Table: table, Schema: invalid})
	require.ErrorContains(t, err, "not supported")
	require.Equal(t, [][]any{{int64(1 << 40)}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertAddsRequiredColumnDuringFullReplacement(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_required_add"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	desired := &schema.TableSchema{Columns: []schema.Column{
		initial.Columns[0],
		{Name: "required_value", DataType: schema.TypeString, Nullable: false},
	}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(desired), [][]any{{int64(2), "present"}})
	require.NoError(t, err)
	require.NoError(t, dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: desired}))

	loaded, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Len(t, loaded.Columns, 2)
	require.False(t, loaded.Columns[1].Nullable)
	require.Equal(t, [][]any{{int64(2), "present"}}, readTableRows(t, dest, table).Rows)
}

func TestAtomicTruncateInsertRejectsNullRequiredAdditionAndRollsBack(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.correctness.atomic_truncate_null_required_add"
	initial := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: false}}}
	desired := &schema.TableSchema{Columns: []schema.Column{
		initial.Columns[0],
		{Name: "required_value", DataType: schema.TypeString, Nullable: false},
	}}
	writeTableRows(t, dest, table, initial, false, [][]any{{int64(1)}})

	batches, err := buildRecordBatches(icebergArrowSchema(desired), [][]any{{int64(2), nil}})
	require.NoError(t, err)
	err = dest.TruncateInsertRecords(ctx, recordBatches(batches...), destination.WriteOptions{Table: table, Schema: desired})
	require.ErrorContains(t, err, "required nested value required_value[0] is null")
	loaded, loadErr := dest.GetTableSchema(ctx, table)
	require.NoError(t, loadErr)
	require.Equal(t, initial.Columns, loaded.Columns)
	require.Equal(t, [][]any{{int64(1)}}, readTableRows(t, dest, table).Rows)
}

func onlyPartitionField(t *testing.T, spec iceberggo.PartitionSpec) iceberggo.PartitionField {
	t.Helper()
	var fields []iceberggo.PartitionField
	for _, field := range spec.Fields() {
		fields = append(fields, field)
	}
	require.Len(t, fields, 1)
	return fields[0]
}
