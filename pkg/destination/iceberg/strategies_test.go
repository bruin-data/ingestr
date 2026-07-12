package iceberg

import (
	"context"
	"net/url"
	"testing"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func newHadoopDestination(t *testing.T) *Destination {
	t.Helper()

	dest := NewDestination()
	require.NoError(t, dest.Connect(context.Background(), "iceberg+hadoop://?warehouse="+url.QueryEscape(t.TempDir())))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })
	return dest
}

func writeTableRows(t *testing.T, dest *Destination, table string, tableSchema *schema.TableSchema, dropFirst bool, rows [][]any) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     table,
		Schema:    tableSchema,
		DropFirst: dropFirst,
	}))

	batches, err := buildRecordBatches(icebergArrowSchema(tableSchema), rows)
	require.NoError(t, err)
	require.NoError(t, dest.WriteParallel(ctx, recordBatches(batches...), destination.WriteOptions{
		Table:  table,
		Schema: tableSchema,
	}))
}

func readTableRows(t *testing.T, dest *Destination, table string) *scannedTable {
	t.Helper()

	tbl, err := dest.loadIcebergTable(context.Background(), table)
	require.NoError(t, err)
	rows, err := scanTableRows(context.Background(), tbl, iceberggo.AlwaysTrue{})
	require.NoError(t, err)
	return rows
}

func rowsByKey(t *testing.T, rows *scannedTable, keyColumn string) map[any][][]any {
	t.Helper()

	out := make(map[any][][]any)
	idx, ok := rows.ColIdx[keyColumn]
	require.True(t, ok, "column %q missing", keyColumn)
	for _, row := range rows.Rows {
		out[row[idx]] = append(out[row[idx]], row)
	}
	return out
}

func singleRowByKey(t *testing.T, rows *scannedTable, keyColumn string) map[any][]any {
	t.Helper()

	grouped := rowsByKey(t, rows, keyColumn)
	out := make(map[any][]any, len(grouped))
	for key, group := range grouped {
		require.Len(t, group, 1, "expected one row for key %v", key)
		out[key] = group[0]
	}
	return out
}

func mergeTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
			{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: true},
		},
	}
}

func micros(t time.Time) int64 { return t.UTC().UnixMicro() }

func TestMergeTableUpsert(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.upsert_target"
	staging := "lake.merge.upsert_staging"
	ts := micros(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))

	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "alpha", 1.5, ts},
		{int64(2), "bravo", 2.5, ts},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(2), "bravo-updated", 22.5, ts + 1},
		{int64(3), "charlie", 3.5, ts + 1},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 3)
	require.Equal(t, "alpha", rows[int64(1)][1])
	require.Equal(t, 1.5, rows[int64(1)][2])
	require.Equal(t, "bravo-updated", rows[int64(2)][1])
	require.Equal(t, 22.5, rows[int64(2)][2])
	require.Equal(t, ts+1, rows[int64(2)][3])
	require.Equal(t, "charlie", rows[int64(3)][1])
}

func TestMergeTableDedupesByIncrementalKey(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.dedup_target"
	staging := "lake.merge.dedup_staging"
	base := micros(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "old", 0.0, base - 10},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(1), "middle", 1.0, base + 5},
		{int64(1), "latest", 2.0, base + 9},
		{int64(1), "earliest", 3.0, base + 1},
		{int64(2), "new-early", 4.0, base + 1},
		{int64(2), "new-late", 5.0, base + 2},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "name", "score", "updated_at"},
		IncrementalKey: "updated_at",
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
	require.Equal(t, "latest", rows[int64(1)][1])
	require.Equal(t, "new-late", rows[int64(2)][1])
}

func TestMergeTableRequiresStagingIncrementalKeyWhenDedupingCopyOnWrite(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.missing_key_cow_target"
	staging := "lake.merge.missing_key_cow_staging"
	stagingSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
		},
	}

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), "first", 1.0},
		{int64(1), "second", 2.0},
	})

	err := dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "name", "score"},
		IncrementalKey: "updated_at",
	})
	require.ErrorContains(t, err, `iceberg: incremental key column "updated_at" not found in staging table lake.merge.missing_key_cow_staging`)
}

func TestMergeTableDedupesWithoutIncrementalKey(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.dedup_noinc_target"
	staging := "lake.merge.dedup_noinc_staging"

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(1), "first", 1.0, nil},
		{int64(1), "last", 2.0, nil},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 1)
	require.Equal(t, "last", rows[int64(1)][1])
}

func TestMergeTableCompositeKeys(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.composite_target"
	staging := "lake.merge.composite_staging"
	compositeSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "tenant", DataType: schema.TypeString, Nullable: false},
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	writeTableRows(t, dest, target, compositeSchema, false, [][]any{
		{"acme", int64(1), "acme-1"},
		{"acme", int64(2), "acme-2"},
		{"globex", int64(1), "globex-1"},
	})
	writeTableRows(t, dest, staging, compositeSchema, true, [][]any{
		{"acme", int64(2), "acme-2-updated"},
		{"globex", int64(2), "globex-2-new"},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"tenant", "id"},
		Columns:      []string{"tenant", "id", "name"},
	}))

	rows := readTableRows(t, dest, target)
	require.Len(t, rows.Rows, 4)
	names := make(map[string]string)
	for _, row := range rows.Rows {
		names[row[0].(string)+"/"+string(rune('0'+row[1].(int64)))] = row[2].(string)
	}
	require.Equal(t, map[string]string{
		"acme/1":   "acme-1",
		"acme/2":   "acme-2-updated",
		"globex/1": "globex-1",
		"globex/2": "globex-2-new",
	}, names)
}

func TestMergeTablePreservesDestinationOnlyColumns(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.destonly_target"
	staging := "lake.merge.destonly_staging"

	targetSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "legacy", DataType: schema.TypeString, Nullable: true},
		},
	}
	stagingSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	writeTableRows(t, dest, target, targetSchema, false, [][]any{
		{int64(1), "alpha", "keep-me"},
	})
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), "alpha-updated"},
		{int64(2), "bravo-new"},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
	require.Equal(t, "alpha-updated", rows[int64(1)][1])
	require.Equal(t, "keep-me", rows[int64(1)][2], "destination-only column must keep its value for existing rows")
	require.Equal(t, "bravo-new", rows[int64(2)][1])
	require.Nil(t, rows[int64(2)][2], "destination-only column must be NULL for new rows")
}

func cdcSchema(withUnchangedCols bool) *schema.TableSchema {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "name", DataType: schema.TypeString, Nullable: true},
		{Name: "payload", DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
	}
	if withUnchangedCols {
		cols = append(cols, schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true})
	}
	return &schema.TableSchema{Columns: cols}
}

func TestMergeTableCDC(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.cdc_target"
	staging := "lake.merge.cdc_staging"
	syncedAt := micros(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

	writeTableRows(t, dest, target, cdcSchema(false), false, [][]any{
		{int64(1), "alpha", "payload-1", "0/100", false, syncedAt - 10},
		{int64(2), "bravo", "payload-2", "0/100", false, syncedAt - 10},
		{int64(3), "charlie", "payload-3", "0/100", false, syncedAt - 10},
	})
	writeTableRows(t, dest, staging, cdcSchema(true), true, [][]any{
		// id=1: update then delete -> data preserved from update, tombstone marked.
		{int64(1), "alpha-updated", "payload-1b", "0/200", false, syncedAt, nil},
		{int64(1), nil, nil, "0/300", true, syncedAt + 1, nil},
		// id=2: update with TOAST-style unchanged payload column.
		{int64(2), "bravo-updated", nil, "0/210", false, syncedAt, `["payload"]`},
		// id=4: net-new insert.
		{int64(4), "delta", "payload-4", "0/220", false, syncedAt, nil},
		// id=5: delete-only key materializes a tombstone for resume.
		{int64(5), nil, nil, "0/230", true, syncedAt, nil},
	})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
	}))

	got := readTableRows(t, dest, target)
	require.NotContains(t, got.ColIdx, destination.CDCUnchangedColsColumn)
	rows := singleRowByKey(t, got, "id")
	require.Len(t, rows, 5)

	value := func(id int64, col string) any { return got.Value(rows[id], col) }

	// id=1: latest non-delete change applied, tombstone marked with the delete's LSN.
	require.Equal(t, "alpha-updated", value(1, "name"))
	require.Equal(t, "payload-1b", value(1, "payload"))
	require.Equal(t, true, value(1, destination.CDCDeletedColumn))
	require.Equal(t, "0/300", value(1, destination.CDCLSNColumn))
	require.Equal(t, syncedAt+1, value(1, destination.CDCSyncedAtColumn))

	// id=2: unchanged TOAST column keeps the target value.
	require.Equal(t, "bravo-updated", value(2, "name"))
	require.Equal(t, "payload-2", value(2, "payload"))
	require.Equal(t, false, value(2, destination.CDCDeletedColumn))
	require.Equal(t, "0/210", value(2, destination.CDCLSNColumn))

	// id=3: untouched.
	require.Equal(t, "charlie", value(3, "name"))
	require.Equal(t, "0/100", value(3, destination.CDCLSNColumn))

	// id=4: inserted.
	require.Equal(t, "delta", value(4, "name"))

	// id=5: tombstone row inserted so CDC resume sees the delete LSN.
	require.Equal(t, true, value(5, destination.CDCDeletedColumn))
	require.Equal(t, "0/230", value(5, destination.CDCLSNColumn))
	require.Nil(t, value(5, "name"))
}

func TestDeleteInsertTable(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.di.int_target"
	staging := "lake.di.int_staging"

	seed := make([][]any, 0, 10)
	for i := int64(1); i <= 10; i++ {
		seed = append(seed, []any{i, "v1", 0.0, nil})
	}
	writeTableRows(t, dest, target, mergeTestSchema(), false, seed)
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(3), "v2", 0.0, nil},
		{int64(4), "v2", 0.0, nil},
		{int64(7), "v2", 0.0, nil},
	})

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   staging,
		TargetTable:    target,
		IncrementalKey: "id",
		IntervalStart:  int64(3),
		IntervalEnd:    int64(7),
		Columns:        []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 8, "ids 5 and 6 must be deleted")
	require.Equal(t, "v1", rows[int64(2)][1])
	require.Equal(t, "v1", rows[int64(10)][1])
	require.Equal(t, "v2", rows[int64(3)][1])
	require.Equal(t, "v2", rows[int64(4)][1])
	require.Equal(t, "v2", rows[int64(7)][1])
	require.NotContains(t, rows, int64(5))
	require.NotContains(t, rows, int64(6))
}

func TestV3DeleteInsertRetriesCommitConflict(t *testing.T) {
	dest := newHadoopDestinationWithTableProperties(t, url.Values{"table.format-version": []string{"3"}})
	ctx := context.Background()
	target, staging := "lake.di.v3_retry_target", "lake.di.v3_retry_staging"
	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "old", 1.0, nil}, {int64(2), "keep", 2.0, nil},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{{int64(1), "new", 3.0, nil}})
	dest.catalog = &failNthCommitCatalog{Catalog: dest.catalog, failAt: 1, err: icebergtable.ErrCommitFailed}

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable: staging, TargetTable: target, IncrementalKey: "id",
		IntervalStart: int64(1), IntervalEnd: int64(1), PrimaryKeys: []string{"id"},
	}))
	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Equal(t, "new", rows[int64(1)][1])
	require.Equal(t, "keep", rows[int64(2)][1])
}

func TestDeleteInsertTableTimestampInterval(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.di.ts_target"
	staging := "lake.di.ts_staging"
	day := func(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }

	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "v1", 0.0, micros(day(1))},
		{int64(2), "v1", 0.0, micros(day(5))},
		{int64(3), "v1", 0.0, micros(day(9))},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(4), "v2", 0.0, micros(day(6))},
	})

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   staging,
		TargetTable:    target,
		IncrementalKey: "updated_at",
		IntervalStart:  day(4),
		IntervalEnd:    day(7),
		Columns:        []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 3)
	require.Contains(t, rows, int64(1))
	require.NotContains(t, rows, int64(2), "row inside the timestamp interval must be deleted")
	require.Contains(t, rows, int64(3))
	require.Contains(t, rows, int64(4))
}

func TestDeleteInsertTableDedupesStagingByPK(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.di.dedup_target"
	staging := "lake.di.dedup_staging"

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(1), "first", 0.0, nil},
		{int64(1), "second", 0.0, nil},
		{int64(2), "only", 0.0, nil},
	})

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   staging,
		TargetTable:    target,
		IncrementalKey: "id",
		IntervalStart:  int64(1),
		IntervalEnd:    int64(2),
		Columns:        []string{"id", "name", "score", "updated_at"},
		PrimaryKeys:    []string{"id"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 2)
	require.Equal(t, "second", rows[int64(1)][1], "duplicate PKs must collapse, highest incremental key wins (arrival order on ties)")
}

func TestDeleteInsertTableRequiresStagingIncrementalKeyWhenDeduping(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.di.missing_key_target"
	staging := "lake.di.missing_key_staging"
	stagingSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
		},
	}

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{
		{int64(1), "first", 0.0},
		{int64(1), "second", 0.0},
	})

	err := dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   staging,
		TargetTable:    target,
		IncrementalKey: "updated_at",
		IntervalStart:  int64(1),
		IntervalEnd:    int64(2),
		Columns:        []string{"id", "name", "score"},
		PrimaryKeys:    []string{"id"},
	})
	require.ErrorContains(t, err, `iceberg: incremental key column "updated_at" not found in staging table lake.di.missing_key_staging`)
}

func scd2TestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "status", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
			{Name: destination.SCD2ValidFromColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
			{Name: destination.SCD2ValidToColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
			{Name: destination.SCD2IsCurrentColumn, DataType: schema.TypeBoolean, Nullable: false},
		},
	}
}

func scd2Row(id int64, status string, score float64, validFrom int64) []any {
	return []any{id, status, score, validFrom, nil, true}
}

func scd2IncrementalTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "status", DataType: schema.TypeString, Nullable: true},
			{Name: "score", DataType: schema.TypeFloat64, Nullable: true},
			{Name: "updated_at", DataType: schema.TypeTimestampTZ, Nullable: false},
			{Name: destination.SCD2ValidFromColumn, DataType: schema.TypeTimestampTZ, Nullable: false},
			{Name: destination.SCD2ValidToColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
			{Name: destination.SCD2IsCurrentColumn, DataType: schema.TypeBoolean, Nullable: false},
		},
	}
}

func scd2IncrementalRow(id int64, status string, score float64, updatedAt, validFrom int64) []any {
	return []any{id, status, score, updatedAt, validFrom, nil, true}
}

func currentSCD2Row(t *testing.T, rows *scannedTable, id int64) []any {
	t.Helper()
	var current []any
	for _, row := range rowsByKey(t, rows, "id")[id] {
		if rows.Value(row, destination.SCD2IsCurrentColumn) == true {
			require.Nil(t, current, "expected one current row for id=%d", id)
			current = row
		}
	}
	require.NotNil(t, current, "expected current row for id=%d", id)
	return current
}

func runSCD2(t *testing.T, dest *Destination, target, staging string, ts time.Time, incrementalKey string) {
	t.Helper()
	require.NoError(t, dest.SCD2Table(context.Background(), destination.SCD2Options{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "status", "score"},
		IncrementalKey: incrementalKey,
		Timestamp:      ts,
	}))
}

func TestSCD2Table(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.scd2.target"
	staging := "lake.scd2.staging"
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)

	// Initial load: empty target, all staging rows are net-new (append path).
	writeTableRows(t, dest, target, scd2TestSchema(), false, nil)
	writeTableRows(t, dest, staging, scd2TestSchema(), true, [][]any{
		scd2Row(1, "active", 1.0, micros(t1)),
		scd2Row(2, "active", 2.0, micros(t1)),
		scd2Row(3, "active", 3.0, micros(t1)),
	})
	runSCD2(t, dest, target, staging, t1, "")

	rows := readTableRows(t, dest, target)
	require.Len(t, rows.Rows, 3)
	for _, row := range rows.Rows {
		require.Equal(t, true, rows.Value(row, destination.SCD2IsCurrentColumn))
		require.Nil(t, rows.Value(row, destination.SCD2ValidToColumn))
	}

	// Update load: id=1 changed, id=2 unchanged, id=3 missing (soft delete), id=4 new.
	writeTableRows(t, dest, staging, scd2TestSchema(), true, [][]any{
		scd2Row(1, "inactive", 1.0, micros(t2)),
		scd2Row(2, "active", 2.0, micros(t2)),
		scd2Row(4, "active", 4.0, micros(t2)),
	})
	runSCD2(t, dest, target, staging, t2, "")

	got := readTableRows(t, dest, target)
	require.Len(t, got.Rows, 5, "3 current + 1 closed version + 1 soft-deleted")
	byID := rowsByKey(t, got, "id")

	require.Len(t, byID[int64(1)], 2, "changed key keeps history")
	var closed, current []any
	for _, row := range byID[int64(1)] {
		if got.Value(row, destination.SCD2IsCurrentColumn) == true {
			current = row
		} else {
			closed = row
		}
	}
	require.NotNil(t, current)
	require.NotNil(t, closed)
	require.Equal(t, "inactive", got.Value(current, "status"))
	require.Equal(t, micros(t2), got.Value(current, destination.SCD2ValidFromColumn))
	require.Equal(t, "active", got.Value(closed, "status"))
	require.Equal(t, micros(t1), got.Value(closed, destination.SCD2ValidFromColumn))
	require.Equal(t, micros(t2), got.Value(closed, destination.SCD2ValidToColumn), "closed version must end where the new one starts")

	require.Len(t, byID[int64(2)], 1, "unchanged key must not produce a new version")
	require.Equal(t, true, got.Value(byID[int64(2)][0], destination.SCD2IsCurrentColumn))
	require.Nil(t, got.Value(byID[int64(2)][0], destination.SCD2ValidToColumn))

	require.Len(t, byID[int64(3)], 1)
	require.Equal(t, false, got.Value(byID[int64(3)][0], destination.SCD2IsCurrentColumn), "missing key must be soft-deleted")
	require.Equal(t, micros(t2), got.Value(byID[int64(3)][0], destination.SCD2ValidToColumn))

	require.Len(t, byID[int64(4)], 1)
	require.Equal(t, true, got.Value(byID[int64(4)][0], destination.SCD2IsCurrentColumn))
}

func TestSCD2TableIncrementalKeySkipsSoftDelete(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.scd2.inc_target"
	staging := "lake.scd2.inc_staging"
	schema := scd2IncrementalTestSchema()
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)

	writeTableRows(t, dest, target, schema, false, nil)
	writeTableRows(t, dest, staging, schema, true, [][]any{
		scd2IncrementalRow(1, "active", 1.0, micros(t1), micros(t1)),
		scd2IncrementalRow(2, "active", 2.0, micros(t1), micros(t1)),
	})
	runSCD2(t, dest, target, staging, t1, "updated_at")

	// Incremental update containing only id=1; id=2 must stay current.
	writeTableRows(t, dest, staging, schema, true, [][]any{
		scd2IncrementalRow(1, "inactive", 1.0, micros(t2), micros(t2)),
	})
	runSCD2(t, dest, target, staging, t2, "updated_at")

	got := readTableRows(t, dest, target)
	byID := rowsByKey(t, got, "id")
	require.Len(t, byID[int64(1)], 2)
	require.Len(t, byID[int64(2)], 1)
	require.Equal(t, true, got.Value(byID[int64(2)][0], destination.SCD2IsCurrentColumn), "incremental SCD2 must not soft-delete missing keys")
	require.Nil(t, got.Value(byID[int64(2)][0], destination.SCD2ValidToColumn))
}

func TestSCD2TableCopyOnWriteDedupesByIncrementalKey(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })

	target := "lake.scd2.inc_cow_target"
	staging := "lake.scd2.inc_cow_staging"
	schema := scd2IncrementalTestSchema()
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)

	writeTableRows(t, dest, target, schema, false, nil)
	writeTableRows(t, dest, staging, schema, true, [][]any{
		scd2IncrementalRow(1, "active", 1.0, micros(t1), micros(t1)),
	})
	require.NoError(t, dest.SCD2Table(context.Background(), destination.SCD2Options{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "status", "score", "updated_at"},
		IncrementalKey: "updated_at",
		Timestamp:      t1,
	}))

	writeTableRows(t, dest, staging, schema, true, [][]any{
		scd2IncrementalRow(1, "newer", 3.0, micros(t3), micros(t3)),
		scd2IncrementalRow(1, "older", 2.0, micros(t2), micros(t2)),
	})
	require.NoError(t, dest.SCD2Table(context.Background(), destination.SCD2Options{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "status", "score", "updated_at"},
		IncrementalKey: "updated_at",
		Timestamp:      t3,
	}))

	got := readTableRows(t, dest, target)
	current := currentSCD2Row(t, got, 1)
	require.Equal(t, "newer", got.Value(current, "status"))
	require.Equal(t, micros(t3), got.Value(current, "updated_at"))
	requireNoEqualityDeletes(t, dest, target)
}

func TestSCD2TableCopyOnWriteRequiresStagingIncrementalKey(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })

	target := "lake.scd2.missing_key_cow_target"
	staging := "lake.scd2.missing_key_cow_staging"
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	writeTableRows(t, dest, target, scd2TestSchema(), false, nil)
	writeTableRows(t, dest, staging, scd2TestSchema(), true, [][]any{
		scd2Row(1, "active", 1.0, micros(t1)),
	})

	err := dest.SCD2Table(context.Background(), destination.SCD2Options{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "status", "score"},
		IncrementalKey: "updated_at",
		Timestamp:      t1,
	})
	require.ErrorContains(t, err, `iceberg: incremental key column "updated_at" not found in staging table lake.scd2.missing_key_cow_staging`)
}

func TestTruncateTable(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	table := "lake.truncate.events"

	writeTableRows(t, dest, table, mergeTestSchema(), false, [][]any{
		{int64(1), "alpha", 1.0, nil},
		{int64(2), "bravo", 2.0, nil},
	})
	require.NoError(t, dest.TruncateTable(ctx, table))

	rows := readTableRows(t, dest, table)
	require.Empty(t, rows.Rows)

	gotSchema, err := dest.GetTableSchema(ctx, table)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "name", "score", "updated_at"}, gotSchema.ColumnNames(), "truncate must preserve the schema")

	// Truncating an empty (or never-written) table is a no-op.
	require.NoError(t, dest.TruncateTable(ctx, table))
}

func TestMergeTableRejectsNullPrimaryKeys(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.merge.nullpk_target"
	staging := "lake.merge.nullpk_staging"
	nullableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: true},
			{Name: "name", DataType: schema.TypeString, Nullable: true},
		},
	}

	writeTableRows(t, dest, target, nullableSchema, false, nil)
	writeTableRows(t, dest, staging, nullableSchema, true, [][]any{
		{nil, "broken"},
	})

	err := dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
	})
	require.ErrorContains(t, err, "contains NULL")
}
