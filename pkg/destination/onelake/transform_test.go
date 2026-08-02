package onelake

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/arrowutil"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var tsTZType = &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}

func makeBatch(t *testing.T, fields []arrow.Field, rows [][]any) arrow.RecordBatch {
	t.Helper()
	s := arrow.NewSchema(fields, nil)
	builders := make([]array.Builder, len(fields))
	for i, f := range fields {
		builders[i] = array.NewBuilder(memory.DefaultAllocator, f.Type)
	}
	for _, row := range rows {
		for i, v := range row {
			appendTestCell(t, builders[i], v)
		}
	}
	cols := make([]arrow.Array, len(builders))
	for i, b := range builders {
		cols[i] = b.NewArray()
		b.Release()
	}
	rec := array.NewRecordBatch(s, cols, int64(len(rows)))
	for _, c := range cols {
		c.Release()
	}
	return rec
}

func appendTestCell(t *testing.T, b array.Builder, v any) {
	t.Helper()
	if v == nil {
		b.AppendNull()
		return
	}
	switch bb := b.(type) {
	case *array.Int64Builder:
		bb.Append(v.(int64))
	case *array.StringBuilder:
		bb.Append(v.(string))
	case *array.BooleanBuilder:
		bb.Append(v.(bool))
	case *array.TimestampBuilder:
		bb.Append(arrow.Timestamp(v.(time.Time).UnixMicro()))
	case *array.Time64Builder:
		bb.Append(arrow.Time64(v.(int64)))
	default:
		t.Fatalf("unsupported builder type %T", b)
	}
}

// collectRows reads all batches into a slice of maps keyed by column name.
func collectRows(batches []arrow.RecordBatch) []map[string]any {
	var rows []map[string]any
	for _, b := range batches {
		s := b.Schema()
		for r := 0; r < int(b.NumRows()); r++ {
			m := make(map[string]any)
			for c := 0; c < int(b.NumCols()); c++ {
				m[s.Field(c).Name] = arrowutil.Value(b.Column(c), r)
			}
			rows = append(rows, m)
		}
	}
	return rows
}

var idValFields = []arrow.Field{
	{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
}

func TestMergeBatches(t *testing.T) {
	t.Parallel()
	target := makeBatch(t, idValFields, [][]any{
		{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"},
	})
	defer target.Release()
	staging := makeBatch(t, idValFields, [][]any{
		{int64(2), "B"}, {int64(4), "d"},
	})
	defer staging.Release()

	out, err := mergeBatches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id"})
	require.NoError(t, err)
	defer releaseBatches(out)

	got := map[int64]string{}
	for _, row := range collectRows(out) {
		got[row["id"].(int64)] = row["val"].(string)
	}
	assert.Equal(t, map[int64]string{1: "a", 2: "B", 3: "c", 4: "d"}, got)
}

func TestMergeBatchesEmptyTarget(t *testing.T) {
	t.Parallel()
	staging := makeBatch(t, idValFields, [][]any{{int64(1), "a"}})
	defer staging.Release()

	out, err := mergeBatches(t.Context(), nil, []arrow.RecordBatch{staging}, []string{"id"})
	require.NoError(t, err)
	defer releaseBatches(out)

	rows := collectRows(out)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), rows[0]["id"])
}

func TestAlignDeltaBatchesFillsEvolvedColumnsForOldFiles(t *testing.T) {
	t.Parallel()

	oldBatch := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(1), "old"}})
	defer oldBatch.Release()
	newBatch := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "email", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(2), "new", "new@example.com"}})
	defer newBatch.Release()
	tableSchema := &deltaStruct{Type: "struct", Fields: []deltaField{
		{Name: "id", Type: "long", Metadata: map[string]any{}},
		{Name: "name", Type: "string", Nullable: true, Metadata: map[string]any{}},
		{Name: "email", Type: "string", Nullable: true, Metadata: map[string]any{}},
	}}

	aligned, err := alignDeltaBatches([]arrow.RecordBatch{oldBatch, newBatch}, tableSchema)
	require.NoError(t, err)
	defer releaseBatches(aligned)
	require.Len(t, aligned, 2)
	for _, batch := range aligned {
		assert.Equal(t, "id", batch.Schema().Field(0).Name)
		assert.Equal(t, "name", batch.Schema().Field(1).Name)
		assert.Equal(t, "email", batch.Schema().Field(2).Name)
	}
	assert.True(t, aligned[0].Column(2).IsNull(0))
	assert.Equal(t, "new@example.com", aligned[1].Column(2).(*array.String).Value(0))

	merged, err := mergeBatches(t.Context(), []arrow.RecordBatch{aligned[0]}, []arrow.RecordBatch{aligned[1]}, []string{"id"})
	require.NoError(t, err)
	defer releaseBatches(merged)
	_, _, err = writeBatchesToParquet(merged)
	require.NoError(t, err, "copy-on-write strategies must accept files from before and after schema evolution")
}

func TestDeleteInsertRewriteAfterAdditiveSchemaEvolution(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	oldTarget := makeBatch(t, []arrow.Field{
		{Name: "ORDER_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "LINE_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "UPDATED_AT", Type: tsTZType},
	}, [][]any{{int64(1), int64(10), updatedAt.Add(-time.Hour)}})
	defer oldTarget.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "ORDER_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "LINE_ID", Type: arrow.PrimitiveTypes.Int64},
		{Name: "UPDATED_AT", Type: tsTZType},
		{Name: "EXTERNAL_REF_ID", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(2), int64(20), updatedAt, "external-2"}})
	defer staging.Release()
	evolvedSchema := &deltaStruct{Type: "struct", Fields: []deltaField{
		{Name: "ORDER_ID", Type: "long", Metadata: map[string]any{}},
		{Name: "LINE_ID", Type: "long", Metadata: map[string]any{}},
		{Name: "UPDATED_AT", Type: "timestamp", Metadata: map[string]any{}},
		{Name: "EXTERNAL_REF_ID", Type: "string", Nullable: true, Metadata: map[string]any{}},
	}}

	targetBatches, err := alignDeltaBatches([]arrow.RecordBatch{oldTarget}, evolvedSchema)
	require.NoError(t, err)
	defer releaseBatches(targetBatches)
	stagingBatches, err := alignDeltaBatches([]arrow.RecordBatch{staging}, evolvedSchema)
	require.NoError(t, err)
	defer releaseBatches(stagingBatches)
	assert.True(t, targetBatches[0].Column(3).IsNull(0))

	result, err := deleteInsertBatches(t.Context(), targetBatches, stagingBatches, destination.DeleteInsertOptions{
		IncrementalKey:     "UPDATED_AT",
		IncrementalKeyType: schema.TypeTimestampTZ,
		IntervalStart:      updatedAt,
		IntervalEnd:        updatedAt,
	})
	require.NoError(t, err)
	defer releaseBatches(result)
	_, _, err = writeBatchesToParquet(result)
	require.NoError(t, err, "delete+insert rewrite must accept the evolved target and staging schemas")
}

func TestUnifyRewriteBatchesReconcilesNullabilityAndOrder(t *testing.T) {
	t.Parallel()

	// Evolution forces the target's added column nullable and appends it last;
	// staging keeps the source's NOT NULL flag and its own column order.
	target := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "tail", Type: arrow.BinaryTypes.String},
		{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(1), "kept", nil}})
	defer target.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "extra", Type: arrow.BinaryTypes.String},
		{Name: "tail", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(2), "new-extra", "new-tail"}})
	defer staging.Release()

	unifiedTarget, unifiedStaging, release, err := unifyRewriteBatches(
		[]arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id", "tail", "extra"},
	)
	require.NoError(t, err)
	defer release()
	assert.True(t, unifiedTarget[0].Schema().Equal(unifiedStaging[0].Schema()))
	assert.Equal(t, []string{"id", "tail", "extra"}, arrowFieldNames(unifiedStaging[0].Schema()))
	assert.Equal(t, "new-tail", unifiedStaging[0].Column(1).(*array.String).Value(0), "values must follow names, not positions")
	assert.Equal(t, "new-extra", unifiedStaging[0].Column(2).(*array.String).Value(0))

	merged := append(retainAll(unifiedTarget), retainAll(unifiedStaging)...)
	defer releaseBatches(merged)
	_, _, err = writeBatchesToParquet(merged)
	require.NoError(t, err)
}

// TestUnifyRewriteBatchesFillsSoftRemovedColumn covers the other direction of
// evolution: the source dropped a column, so the table keeps it relaxed to
// nullable and the incoming rows have to be NULL-filled for it.
func TestUnifyRewriteBatchesFillsSoftRemovedColumn(t *testing.T) {
	t.Parallel()

	target := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "gone", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(1), "kept"}})
	defer target.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2)}})
	defer staging.Release()

	unifiedTarget, unifiedStaging, release, err := unifyRewriteBatches(
		[]arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id", "gone"},
	)
	require.NoError(t, err)
	defer release()
	assert.Equal(t, []string{"id", "gone"}, arrowFieldNames(unifiedStaging[0].Schema()))
	assert.True(t, unifiedStaging[0].Column(1).IsNull(0))
	assert.Equal(t, "kept", unifiedTarget[0].Column(1).(*array.String).Value(0))

	merged := append(retainAll(unifiedTarget), retainAll(unifiedStaging)...)
	defer releaseBatches(merged)
	_, _, err = writeBatchesToParquet(merged)
	require.NoError(t, err)
}

// TestUnifyRewriteBatchesKeepsRequiredColumnMissingFromStaging guards the same
// case when the column is still declared NOT NULL, which is what a table looks
// like when evolution was skipped: filling it would contradict the declaration,
// so the rewrite has to keep failing.
func TestUnifyRewriteBatchesKeepsRequiredColumnMissingFromStaging(t *testing.T) {
	t.Parallel()

	target := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "gone", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "kept"}})
	defer target.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2)}})
	defer staging.Release()

	_, _, release, err := unifyRewriteBatches(
		[]arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id", "gone"},
	)
	defer release()
	require.ErrorContains(t, err, `column "gone" is required but the incoming rows do not have it`)
}

// TestUnifyRewriteBatchesKeepsUndeclaredStagingColumn guards against silently
// dropping a staging column the target does not declare, which the NULL-filling
// projection would otherwise do.
func TestUnifyRewriteBatchesKeepsUndeclaredStagingColumn(t *testing.T) {
	t.Parallel()

	target := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "kept", Type: arrow.BinaryTypes.String, Nullable: true},
	}, [][]any{{int64(1), "old"}})
	defer target.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "undeclared", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(2), "new"}})
	defer staging.Release()

	_, _, release, err := unifyRewriteBatches(
		[]arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id", "kept"},
	)
	defer release()
	require.ErrorContains(t, err, `column "undeclared" is not declared in its Delta schema`)
}

func TestUnifyRewriteBatchesKeepsRealTypeConflicts(t *testing.T) {
	t.Parallel()

	target := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "note", Type: arrow.BinaryTypes.String},
	}, [][]any{{int64(1), "kept"}})
	defer target.Release()
	staging := makeBatch(t, []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "note", Type: arrow.PrimitiveTypes.Int64},
	}, [][]any{{int64(2), int64(3)}})
	defer staging.Release()

	_, _, release, err := unifyRewriteBatches(
		[]arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, []string{"id", "note"},
	)
	defer release()
	require.ErrorContains(t, err, `column "note" is utf8 in the table and int64 in the incoming rows`)
}

func TestDeleteInsertBatches(t *testing.T) {
	t.Parallel()
	tsFields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "ts", Type: tsTZType},
	}
	day := func(d int) time.Time { return time.Date(2024, 1, d, 0, 0, 0, 0, time.UTC) }

	target := makeBatch(t, tsFields, [][]any{
		{int64(1), day(1)}, {int64(2), day(5)}, {int64(3), day(10)},
	})
	defer target.Release()
	staging := makeBatch(t, tsFields, [][]any{
		{int64(20), day(6)}, {int64(21), day(7)},
	})
	defer staging.Release()

	opts := destination.DeleteInsertOptions{
		IncrementalKey:     "ts",
		IncrementalKeyType: schema.TypeTimestampTZ,
		IntervalStart:      day(4),
		IntervalEnd:        day(8),
	}
	out, err := deleteInsertBatches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, opts)
	require.NoError(t, err)
	defer releaseBatches(out)

	ids := map[int64]bool{}
	for _, row := range collectRows(out) {
		ids[row["id"].(int64)] = true
	}
	// id=2 (day 5) is inside [4,8] and removed; id=1,3 kept; staging 20,21 inserted.
	assert.Equal(t, map[int64]bool{1: true, 3: true, 20: true, 21: true}, ids)
}

var scd2Fields = []arrow.Field{
	{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
	{Name: destination.SCD2ValidFromColumn, Type: tsTZType},
	{Name: destination.SCD2ValidToColumn, Type: tsTZType, Nullable: true},
	{Name: destination.SCD2IsCurrentColumn, Type: arrow.FixedWidthTypes.Boolean},
}

func scd2Opts(incrementalKey string, ts time.Time) destination.SCD2Options {
	return destination.SCD2Options{
		PrimaryKeys:    []string{"id"},
		Columns:        []string{"id", "val"},
		IncrementalKey: incrementalKey,
		Timestamp:      ts,
	}
}

func TestSCD2Unchanged(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	target := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "a", t0, nil, true},
	})
	defer target.Release()
	staging := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "a", ts, nil, true},
	})
	defer staging.Release()

	out, err := scd2Batches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, scd2Opts("", ts))
	require.NoError(t, err)
	defer releaseBatches(out)

	rows := collectRows(out)
	require.Len(t, rows, 1)
	assert.Equal(t, true, rows[0][destination.SCD2IsCurrentColumn])
	assert.Nil(t, rows[0][destination.SCD2ValidToColumn])
}

func TestSCD2Changed(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	target := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "a", t0, nil, true},
	})
	defer target.Release()
	staging := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "b", ts, nil, true},
	})
	defer staging.Release()

	out, err := scd2Batches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, scd2Opts("", ts))
	require.NoError(t, err)
	defer releaseBatches(out)

	rows := collectRows(out)
	require.Len(t, rows, 2)

	var closed, current map[string]any
	for _, r := range rows {
		if r[destination.SCD2IsCurrentColumn].(bool) {
			current = r
		} else {
			closed = r
		}
	}
	require.NotNil(t, closed)
	require.NotNil(t, current)
	assert.Equal(t, "a", closed["val"]) // old version closed
	require.NotNil(t, closed[destination.SCD2ValidToColumn])
	assert.Equal(t, "b", current["val"]) // new version current
}

func TestSCD2NetNewAndSoftDelete(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Target has id=1 (will be missing from staging) and id=2 (unchanged).
	target := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "x", t0, nil, true},
		{int64(2), "y", t0, nil, true},
	})
	defer target.Release()
	// Staging has id=2 (unchanged) and id=3 (net-new).
	staging := makeBatch(t, scd2Fields, [][]any{
		{int64(2), "y", ts, nil, true},
		{int64(3), "z", ts, nil, true},
	})
	defer staging.Release()

	// No incremental key -> missing rows get soft-deleted.
	out, err := scd2Batches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, scd2Opts("", ts))
	require.NoError(t, err)
	defer releaseBatches(out)

	byID := map[int64]map[string]any{}
	for _, r := range collectRows(out) {
		byID[r["id"].(int64)] = r
	}
	require.Len(t, byID, 3)
	assert.Equal(t, false, byID[1][destination.SCD2IsCurrentColumn]) // soft-deleted
	assert.NotNil(t, byID[1][destination.SCD2ValidToColumn])
	assert.Equal(t, true, byID[2][destination.SCD2IsCurrentColumn]) // unchanged stays current
	assert.Equal(t, true, byID[3][destination.SCD2IsCurrentColumn]) // net-new current
}

func TestSCD2TargetColumnOrderIndependent(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Target lists _scd_is_current before _scd_valid_to — the opposite order
	// from staging. The close logic must resolve indices from each batch's own
	// schema, not from staging's.
	targetFields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "val", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: destination.SCD2ValidFromColumn, Type: tsTZType},
		{Name: destination.SCD2IsCurrentColumn, Type: arrow.FixedWidthTypes.Boolean},
		{Name: destination.SCD2ValidToColumn, Type: tsTZType, Nullable: true},
	}
	target := makeBatch(t, targetFields, [][]any{
		{int64(1), "a", t0, true, nil},
	})
	defer target.Release()
	staging := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "b", ts, nil, true},
	})
	defer staging.Release()

	out, err := scd2Batches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, scd2Opts("", ts))
	require.NoError(t, err)
	defer releaseBatches(out)

	var closed map[string]any
	for _, r := range collectRows(out) {
		if !r[destination.SCD2IsCurrentColumn].(bool) {
			closed = r
		}
	}
	require.NotNil(t, closed, "changed row should be closed")
	assert.Equal(t, "a", closed["val"])
	assert.NotNil(t, closed[destination.SCD2ValidToColumn], "valid_to must be stamped, not is_current")
}

func TestSCD2IncrementalKeySkipsSoftDelete(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	target := makeBatch(t, scd2Fields, [][]any{
		{int64(1), "x", t0, nil, true},
	})
	defer target.Release()
	staging := makeBatch(t, scd2Fields, [][]any{
		{int64(2), "z", ts, nil, true},
	})
	defer staging.Release()

	// Incremental key set -> id=1 missing from staging must NOT be soft-deleted.
	out, err := scd2Batches(t.Context(), []arrow.RecordBatch{target}, []arrow.RecordBatch{staging}, scd2Opts("ts", ts))
	require.NoError(t, err)
	defer releaseBatches(out)

	byID := map[int64]map[string]any{}
	for _, r := range collectRows(out) {
		byID[r["id"].(int64)] = r
	}
	require.Len(t, byID, 2)
	assert.Equal(t, true, byID[1][destination.SCD2IsCurrentColumn]) // untouched
	assert.Equal(t, true, byID[2][destination.SCD2IsCurrentColumn]) // net-new
}
