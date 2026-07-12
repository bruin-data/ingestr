package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The accumulator buffers plain changes across transaction boundaries and runs
// the unchanged-TOAST fill over the whole flush window when it materializes
// the Arrow batch. These tests feed changes from *separate commits* (separate
// add calls with increasing LSNs) and inspect the flushed batch — the
// cross-commit coalescing that used to happen at the Arrow staging-batch level.

// fillTestSchema: id (pk), config_data (TOASTable), status + CDC meta columns.
func fillTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Name:   "t",
		Schema: "public",
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "config_data", DataType: schema.TypeString, Nullable: true},
			{Name: "status", DataType: schema.TypeString},
		}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
}

// fillChange builds one change row for the fill tests. config == nil means an
// explicit NULL; configUnchanged means the column was omitted as unchanged
// TOAST (marker).
type fillChange struct {
	op              string
	id              int64
	config          *string
	configUnchanged bool
	status          string
}

func (c fillChange) toChange(lsn pglogrepl.LSN) Change {
	var configVal interface{}
	switch {
	case c.configUnchanged:
		configVal = tupleUnchangedMarker
	case c.config != nil:
		configVal = *c.config
	}
	return Change{
		Operation: c.op,
		LSN:       lsn,
		Values:    []interface{}{c.id, configVal, c.status},
		OldValues: []interface{}{c.id},
	}
}

func strPtr(s string) *string { return &s }

// flushFillWindow adds each change as its own commit (increasing LSNs) and
// flushes, returning the materialized batch.
func flushFillWindow(t *testing.T, rows []fillChange) arrow.RecordBatch {
	t.Helper()
	accum := newBatchAccumulator(10000, map[string]*schema.TableSchema{"t": fillTestSchema()})
	for i, r := range rows {
		lsn := pglogrepl.LSN(i + 1)
		accum.add("t", []Change{r.toChange(lsn)}, lsn)
	}
	results := make(chan source.RecordBatchResult, 4)
	require.NoError(t, accum.flushAll(results, nil))
	close(results)
	res := <-results
	require.NoError(t, res.Err)
	require.NotNil(t, res.Batch)
	return res.Batch
}

func configValueAt(t *testing.T, batch arrow.RecordBatch, row int) (string, bool) {
	t.Helper()
	col, ok := batch.Column(1).(*array.String)
	require.True(t, ok)
	if col.IsNull(row) {
		return "", false
	}
	return col.Value(row), true
}

func unchangedValueAt(t *testing.T, batch arrow.RecordBatch, row int) string {
	t.Helper()
	col, ok := batch.Column(6).(*array.String)
	require.True(t, ok)
	return col.Value(row)
}

func TestFlushWindowFillsUnchangedAcrossCommits(t *testing.T) {
	// Compaction keeps only the latest change per PK, so most windows below
	// materialize as a single (filled) row.

	t.Run("insert then partial update fills omitted toast", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"big":true}`), status: "pending"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "done"},
		})
		defer out.Release()

		require.EqualValues(t, 1, out.NumRows(), "superseded insert must be compacted away")
		v, ok := configValueAt(t, out, 0)
		assert.True(t, ok, "config_data should be filled on the partial update row")
		assert.Equal(t, `{"big":true}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 0), "filled column must be dropped from _cdc_unchanged_cols")
		assert.Equal(t, "done", statusValueAt(t, out, 0))
	})

	t.Run("changed then unchanged overwrites and drops unchanged flag", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "UPDATE", id: 1, config: strPtr(`{"v":"A"}`), status: "a"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		require.EqualValues(t, 1, out.NumRows())
		v, ok := configValueAt(t, out, 0)
		assert.True(t, ok)
		assert.Equal(t, `{"v":"A"}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 0))
	})

	t.Run("explicit null carries across commits and drops unchanged flag", func(t *testing.T) {
		// An authoritative NULL (SET config_data = NULL) in one transaction
		// followed by an unchanged TOAST update in another. The NULL must be
		// filled forward and the column dropped from _cdc_unchanged_cols, so the
		// destination does not fall back to the stale (non-null) target value.
		out := flushFillWindow(t, []fillChange{
			{op: "UPDATE", id: 1, config: nil, status: "a"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		require.EqualValues(t, 1, out.NumRows())
		_, ok := configValueAt(t, out, 0)
		assert.False(t, ok, "filled value should be NULL")
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 0), "column must be dropped from _cdc_unchanged_cols")
	})

	t.Run("no prior value keeps column unchanged", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "UPDATE", id: 1, configUnchanged: true, status: "a"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		require.EqualValues(t, 1, out.NumRows())
		_, ok := configValueAt(t, out, 0)
		assert.False(t, ok)
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 0))
	})

	t.Run("does not cross primary keys", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"id":1}`), status: "a"},
			{op: "UPDATE", id: 2, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		require.EqualValues(t, 2, out.NumRows(), "distinct PKs must both survive")
		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok)
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 1))
	})

	t.Run("delete resets prior state", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"v":1}`), status: "a"},
			{op: "DELETE", id: 1},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "c"},
		})
		defer out.Release()

		// Compaction keeps the latest deleted and the latest non-deleted row.
		require.EqualValues(t, 2, out.NumRows())
		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok, "value before a delete must not carry past it")
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 1))
	})

	t.Run("preserves unrelated rows exactly", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"big":true}`), status: "pending"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "done"},
			{op: "INSERT", id: 2, config: strPtr(`{"other":1}`), status: "x"},
		})
		defer out.Release()

		require.EqualValues(t, 2, out.NumRows())
		v0, ok0 := configValueAt(t, out, 0)
		assert.True(t, ok0, "id=1's filled update survives compaction")
		assert.Equal(t, `{"big":true}`, v0)
		v1, ok1 := configValueAt(t, out, 1)
		assert.True(t, ok1)
		assert.Equal(t, `{"other":1}`, v1)

		idCol, ok := out.Column(0).(*array.Int64)
		require.True(t, ok)
		assert.Equal(t, int64(1), idCol.Value(0))
		assert.Equal(t, int64(2), idCol.Value(1))
	})

	t.Run("fill crosses accumulator adds from multi-row commits", func(t *testing.T) {
		// One commit inserts two rows, a later commit partially updates one of
		// them; the fill must resolve within the merged window.
		accum := newBatchAccumulator(10000, map[string]*schema.TableSchema{"t": fillTestSchema()})
		accum.add("t", []Change{
			fillChange{op: "INSERT", id: 1, config: strPtr(`{"a":1}`), status: "a"}.toChange(1),
			fillChange{op: "INSERT", id: 2, config: strPtr(`{"b":2}`), status: "b"}.toChange(1),
		}, 1)
		accum.add("t", []Change{
			fillChange{op: "UPDATE", id: 2, configUnchanged: true, status: "b2"}.toChange(2),
		}, 2)

		results := make(chan source.RecordBatchResult, 4)
		require.NoError(t, accum.flushAll(results, nil))
		close(results)
		res := <-results
		require.NotNil(t, res.Batch)
		defer res.Batch.Release()

		require.EqualValues(t, 2, res.Batch.NumRows())
		v, ok := configValueAt(t, res.Batch, 1)
		assert.True(t, ok)
		assert.Equal(t, `{"b":2}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, res.Batch, 1))
	})
}

func statusValueAt(t *testing.T, batch arrow.RecordBatch, row int) string {
	t.Helper()
	col, ok := batch.Column(2).(*array.String)
	require.True(t, ok)
	return col.Value(row)
}

func lsnValueAt(t *testing.T, batch arrow.RecordBatch, row int) string {
	t.Helper()
	col, ok := batch.Column(3).(*array.String)
	require.True(t, ok)
	return col.Value(row)
}

func deletedValueAt(t *testing.T, batch arrow.RecordBatch, row int) bool {
	t.Helper()
	col, ok := batch.Column(4).(*array.Boolean)
	require.True(t, ok)
	return col.Value(row)
}

// Last-write-wins compaction across the flush window: only the latest
// non-deleted and latest deleted change per PK survive to the staging batch,
// mirroring exactly what the destination merge would read anyway.
func TestFlushWindowCompaction(t *testing.T) {
	t.Run("update chain keeps only the last version", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"v":0}`), status: "s0"},
			{op: "UPDATE", id: 1, config: strPtr(`{"v":1}`), status: "s1"},
			{op: "UPDATE", id: 1, config: strPtr(`{"v":2}`), status: "s2"},
			{op: "UPDATE", id: 1, config: strPtr(`{"v":3}`), status: "s3"},
		})
		defer out.Release()

		require.EqualValues(t, 1, out.NumRows())
		v, _ := configValueAt(t, out, 0)
		assert.Equal(t, `{"v":3}`, v)
		assert.Equal(t, "s3", statusValueAt(t, out, 0))
		// The surviving row keeps its own commit LSN (commit 4 of the window).
		assert.Equal(t, FormatLSN(4), lsnValueAt(t, out, 0))
	})

	t.Run("insert then delete keeps both latest versions", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"v":0}`), status: "s0"},
			{op: "UPDATE", id: 1, config: strPtr(`{"v":1}`), status: "s1"},
			{op: "DELETE", id: 1},
		})
		defer out.Release()

		// Latest non-deleted (the update) and latest deleted both survive so
		// the merge can decide: latest overall is the delete, row ends deleted.
		require.EqualValues(t, 2, out.NumRows())
		assert.False(t, deletedValueAt(t, out, 0))
		assert.Equal(t, FormatLSN(2), lsnValueAt(t, out, 0))
		assert.True(t, deletedValueAt(t, out, 1))
		assert.Equal(t, FormatLSN(3), lsnValueAt(t, out, 1))
	})

	t.Run("delete then reinsert keeps both, insert is latest", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "DELETE", id: 1},
			{op: "INSERT", id: 1, config: strPtr(`{"v":9}`), status: "back"},
		})
		defer out.Release()

		require.EqualValues(t, 2, out.NumRows())
		assert.True(t, deletedValueAt(t, out, 0))
		assert.False(t, deletedValueAt(t, out, 1))
		assert.Greater(t, lsnValueAt(t, out, 1), lsnValueAt(t, out, 0),
			"reinsert must carry the higher LSN so the merge resurrects the row")
	})

	t.Run("distinct keys are untouched", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"v":1}`), status: "a"},
			{op: "INSERT", id: 2, config: strPtr(`{"v":2}`), status: "b"},
			{op: "INSERT", id: 3, config: strPtr(`{"v":3}`), status: "c"},
		})
		defer out.Release()

		require.EqualValues(t, 3, out.NumRows())
	})

	t.Run("no primary key expands updates and disables compaction", func(t *testing.T) {
		sc := fillTestSchema()
		sc.PrimaryKeys = nil
		accum := newBatchAccumulator(10000, map[string]*schema.TableSchema{"t": sc})
		accum.add("t", []Change{
			fillChange{op: "INSERT", id: 1, config: strPtr(`{"v":1}`), status: "a"}.toChange(1),
			fillChange{op: "UPDATE", id: 1, config: strPtr(`{"v":2}`), status: "b"}.toChange(2),
		}, 1)

		results := make(chan source.RecordBatchResult, 4)
		require.NoError(t, accum.flushAll(results, nil))
		close(results)
		var batches []arrow.RecordBatch
		for res := range results {
			require.NotNil(t, res.Batch)
			batches = append(batches, res.Batch)
		}
		require.Len(t, batches, 2, "keyless CDC emits one durable write unit per source transaction")
		defer batches[0].Release()
		defer batches[1].Release()
		assert.EqualValues(t, 1, batches[0].NumRows())
		assert.False(t, deletedValueAt(t, batches[0], 0))
		assert.EqualValues(t, 2, batches[1].NumRows())
		assert.True(t, deletedValueAt(t, batches[1], 0))
		assert.False(t, deletedValueAt(t, batches[1], 1))
	})

	t.Run("pk-changing update keeps old-key history and new-key row", func(t *testing.T) {
		// UPDATE id 1 -> 2: the change is keyed by its new PK, so the insert
		// of id=1 survives as that key's latest version (matching the merge,
		// which never deletes the old-key row on a PK change).
		changes := []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"v":1}`), status: "a"},
		}
		out := flushFillWindow(t, append(changes, fillChange{op: "UPDATE", id: 2, config: strPtr(`{"v":2}`), status: "b"}))
		defer out.Release()

		require.EqualValues(t, 2, out.NumRows())
		idCol := out.Column(0).(*array.Int64)
		assert.Equal(t, int64(1), idCol.Value(0))
		assert.Equal(t, int64(2), idCol.Value(1))
	})
}
