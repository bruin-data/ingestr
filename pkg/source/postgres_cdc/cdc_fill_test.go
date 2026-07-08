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
	t.Run("insert then partial update fills omitted toast", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"big":true}`), status: "pending"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "done"},
		})
		defer out.Release()

		v, ok := configValueAt(t, out, 1)
		assert.True(t, ok, "config_data should be filled on the partial update row")
		assert.Equal(t, `{"big":true}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1), "filled column must be dropped from _cdc_unchanged_cols")
	})

	t.Run("changed then unchanged overwrites and drops unchanged flag", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "UPDATE", id: 1, config: strPtr(`{"v":"A"}`), status: "a"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		v, ok := configValueAt(t, out, 1)
		assert.True(t, ok)
		assert.Equal(t, `{"v":"A"}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1))
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

		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok, "filled value should be NULL")
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1), "column must be dropped from _cdc_unchanged_cols")
	})

	t.Run("no prior value keeps column unchanged", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "UPDATE", id: 1, configUnchanged: true, status: "a"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "b"},
		})
		defer out.Release()

		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok)
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 1))
	})

	t.Run("does not cross primary keys", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"id":1}`), status: "a"},
			{op: "UPDATE", id: 2, configUnchanged: true, status: "b"},
		})
		defer out.Release()

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

		_, ok := configValueAt(t, out, 2)
		assert.False(t, ok, "value before a delete must not carry past it")
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 2))
	})

	t.Run("preserves unrelated rows exactly", func(t *testing.T) {
		out := flushFillWindow(t, []fillChange{
			{op: "INSERT", id: 1, config: strPtr(`{"big":true}`), status: "pending"},
			{op: "UPDATE", id: 1, configUnchanged: true, status: "done"},
			{op: "INSERT", id: 2, config: strPtr(`{"other":1}`), status: "x"},
		})
		defer out.Release()

		v0, ok0 := configValueAt(t, out, 0)
		assert.True(t, ok0)
		assert.Equal(t, `{"big":true}`, v0)
		v2, ok2 := configValueAt(t, out, 2)
		assert.True(t, ok2)
		assert.Equal(t, `{"other":1}`, v2)

		idCol, ok := out.Column(0).(*array.Int64)
		require.True(t, ok)
		assert.Equal(t, int64(2), idCol.Value(2))
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

		v, ok := configValueAt(t, res.Batch, 2)
		assert.True(t, ok)
		assert.Equal(t, `{"b":2}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, res.Batch, 2))
	})
}
