package iceberg

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

// latestSnapshotSummary returns the summary properties of the table's current
// snapshot, used to detect whether an operation committed equality delete
// files (merge-on-read) or rewrote data files (copy-on-write).
func latestSnapshotSummary(t *testing.T, dest *Destination, table string) map[string]string {
	t.Helper()

	tbl, err := dest.loadIcebergTable(context.Background(), table)
	require.NoError(t, err)
	snap := tbl.CurrentSnapshot()
	require.NotNil(t, snap)
	require.NotNil(t, snap.Summary)
	return snap.Summary.Properties
}

func requireEqualityDeletes(t *testing.T, dest *Destination, table string) {
	t.Helper()
	summary := latestSnapshotSummary(t, dest, table)
	require.NotEmpty(t, summary["added-equality-delete-files"], "expected a merge-on-read snapshot with equality delete files, summary: %v", summary)
}

func requireNoEqualityDeletes(t *testing.T, dest *Destination, table string) {
	t.Helper()
	summary := latestSnapshotSummary(t, dest, table)
	require.Empty(t, summary["added-equality-delete-files"], "expected a copy-on-write snapshot without equality delete files, summary: %v", summary)
}

func runBasicMerge(t *testing.T, dest *Destination, target, staging string) {
	t.Helper()
	ctx := context.Background()
	ts := micros(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

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
	require.Equal(t, "bravo-updated", rows[int64(2)][1])
	require.Equal(t, "charlie", rows[int64(3)][1])
}

func TestMergeTableUsesRowDeltaByDefault(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.mor.merge_target"
	runBasicMerge(t, dest, target, "lake.mor.merge_staging")
	requireEqualityDeletes(t, dest, target)
}

func TestMergeTableHonorsCopyOnWriteProperty(t *testing.T) {
	dest := NewDestination()
	uri := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir()) + "&table.write.merge.mode=copy-on-write"
	require.NoError(t, dest.Connect(context.Background(), uri))
	t.Cleanup(func() { require.NoError(t, dest.Close(context.Background())) })

	target := "lake.cow.merge_target"
	runBasicMerge(t, dest, target, "lake.cow.merge_staging")
	requireNoEqualityDeletes(t, dest, target)
}

func TestMergeTablePartitionedByPrimaryKeyUsesRowDelta(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.mor.part_pk_target"
	staging := "lake.mor.part_pk_staging"

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       target,
		Schema:      mergeTestSchema(),
		PartitionBy: "id",
	}))
	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "alpha", 1.5, nil},
		{int64(2), "bravo", 2.5, nil},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(2), "bravo-updated", 22.5, nil},
		{int64(3), "charlie", 3.5, nil},
	})
	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 3)
	require.Equal(t, "bravo-updated", rows[int64(2)][1])
	requireEqualityDeletes(t, dest, target)
}

func TestMergeTablePartitionedOutsidePrimaryKeyFallsBack(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.cow.part_other_target"
	staging := "lake.cow.part_other_staging"

	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       target,
		Schema:      mergeTestSchema(),
		PartitionBy: "name",
	}))
	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(1), "alpha", 1.5, nil},
	})
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		// The row moves partitions ("alpha" -> "alpha-moved"); equality deletes
		// would be routed to the new partition and miss the old row, so this
		// must run copy-on-write.
		{int64(1), "alpha-moved", 11.5, nil},
	})
	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name", "score", "updated_at"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 1)
	require.Equal(t, "alpha-moved", rows[int64(1)][1], "partition-crossing update must not leave the old row behind")
	requireNoEqualityDeletes(t, dest, target)
}

func TestMergeTableSpilledRuns(t *testing.T) {
	withSpillRunRows(t, 8)

	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.mor.spill_target"
	staging := "lake.mor.spill_staging"
	base := micros(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	writeTableRows(t, dest, target, mergeTestSchema(), false, [][]any{
		{int64(0), "seed-0", 0.0, base - 1},
		{int64(1), "seed-1", 0.0, base - 1},
	})

	// 60 staged rows over 20 keys, three versions each, interleaved so
	// duplicates land in different spill runs. The highest updated_at wins.
	stagingRows := make([][]any, 0, 60)
	for version := range 3 {
		for key := range 20 {
			stagingRows = append(stagingRows, []any{
				int64(key),
				fmt.Sprintf("k%02d-v%d", key, version),
				float64(version),
				base + int64(version),
			})
		}
	}
	writeTableRows(t, dest, staging, mergeTestSchema(), true, stagingRows)

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "name", "score", "updated_at"},
		IncrementalKey: "updated_at",
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 20)
	for key := range 20 {
		require.Equal(t, fmt.Sprintf("k%02d-v2", key), rows[int64(key)][1], "latest version by incremental key must win for key %d", key)
	}
	requireEqualityDeletes(t, dest, target)
}

func TestMergeTableRequiresStagingIncrementalKeyWhenDedupingRowDelta(t *testing.T) {
	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.mor.missing_key_target"
	staging := "lake.mor.missing_key_staging"

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	writeTableRows(t, dest, staging, mergeTestSchema(), true, [][]any{
		{int64(1), "first", 1.0, nil},
		{int64(1), "second", 2.0, nil},
	})

	err := dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable:   staging,
		TargetTable:    target,
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "name", "score", "updated_at"},
		IncrementalKey: "missing_key",
	})
	require.ErrorContains(t, err, `iceberg: incremental key column "missing_key" not found in staging table lake.mor.missing_key_staging`)
}

func TestSCD2TableUsesRowDeltaByDefault(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.mor.scd2_target"
	staging := "lake.mor.scd2_staging"
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	writeTableRows(t, dest, target, scd2TestSchema(), false, nil)
	writeTableRows(t, dest, staging, scd2TestSchema(), true, [][]any{
		scd2Row(1, "active", 1.0, micros(t1)),
		scd2Row(2, "active", 2.0, micros(t1)),
	})
	runSCD2(t, dest, target, staging, t1, "")

	writeTableRows(t, dest, staging, scd2TestSchema(), true, [][]any{
		scd2Row(1, "inactive", 1.0, micros(t2)),
		scd2Row(2, "active", 2.0, micros(t2)),
	})
	runSCD2(t, dest, target, staging, t2, "")

	got := readTableRows(t, dest, target)
	require.Len(t, got.Rows, 3, "changed key gains a closed version, unchanged key stays single")
	requireEqualityDeletes(t, dest, target)
}

func TestSCD2TableRowDeltaDedupesByIncrementalKey(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.mor.scd2_inc_target"
	staging := "lake.mor.scd2_inc_staging"
	schema := scd2IncrementalTestSchema()
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)

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
	requireEqualityDeletes(t, dest, target)
}

func TestDeleteInsertSpilledRuns(t *testing.T) {
	withSpillRunRows(t, 4)

	dest := newHadoopDestination(t)
	ctx := context.Background()
	target := "lake.di.spill_target"
	staging := "lake.di.spill_staging"

	writeTableRows(t, dest, target, mergeTestSchema(), false, nil)
	stagingRows := make([][]any, 0, 30)
	for version := range 3 {
		for key := range 10 {
			stagingRows = append(stagingRows, []any{int64(key), fmt.Sprintf("k%02d-v%d", key, version), 0.0, nil})
		}
	}
	writeTableRows(t, dest, staging, mergeTestSchema(), true, stagingRows)

	require.NoError(t, dest.DeleteInsertTable(ctx, destination.DeleteInsertOptions{
		StagingTable:   staging,
		TargetTable:    target,
		IncrementalKey: "id",
		IntervalStart:  int64(0),
		IntervalEnd:    int64(9),
		Columns:        []string{"id", "name", "score", "updated_at"},
		PrimaryKeys:    []string{"id"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Len(t, rows, 10)
	for key := range 10 {
		require.Equal(t, fmt.Sprintf("k%02d-v2", key), rows[int64(key)][1], "arrival order must break dedup ties for key %d", key)
	}
}

// TestSchemaMergeCompatibility ensures a merge after schema evolution (target
// widened, staging matching) still routes correctly.
func TestMergeTableWithDestinationOnlyColumnStillFallsBack(t *testing.T) {
	dest := newHadoopDestination(t)
	target := "lake.cow.destonly2_target"
	staging := "lake.cow.destonly2_staging"
	ctx := context.Background()

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
	writeTableRows(t, dest, target, targetSchema, false, [][]any{{int64(1), "alpha", "keep"}})
	writeTableRows(t, dest, staging, stagingSchema, true, [][]any{{int64(1), "alpha-updated"}})

	require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
		StagingTable: staging,
		TargetTable:  target,
		PrimaryKeys:  []string{"id"},
		Columns:      []string{"id", "name"},
	}))

	rows := singleRowByKey(t, readTableRows(t, dest, target), "id")
	require.Equal(t, "keep", rows[int64(1)][2], "destination-only column must survive, forcing the copy-on-write path")
	requireNoEqualityDeletes(t, dest, target)
}
