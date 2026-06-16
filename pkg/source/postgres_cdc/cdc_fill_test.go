package postgres_cdc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fillRow describes one staging row for the forward-fill tests.
type fillRow struct {
	id        int64
	config    *string // nil => null in the config_data column
	status    string
	lsn       string
	deleted   bool
	unchanged []string
}

// buildFillBatch assembles a CDC staging batch with two source columns
// (id, config_data) plus status and the four CDC meta columns.
func buildFillBatch(rows []fillRow) arrow.RecordBatch {
	mem := memory.NewGoAllocator()
	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "config_data", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "status", Type: arrow.BinaryTypes.String},
		{Name: CDCLSNColumn, Type: arrow.BinaryTypes.String},
		{Name: CDCDeletedColumn, Type: arrow.FixedWidthTypes.Boolean},
		{Name: CDCSyncedAtColumn, Type: &arrow.TimestampType{Unit: arrow.Microsecond}},
		{Name: CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String},
	}
	sc := arrow.NewSchema(fields, nil)

	idB := array.NewInt64Builder(mem)
	configB := array.NewStringBuilder(mem)
	statusB := array.NewStringBuilder(mem)
	lsnB := array.NewStringBuilder(mem)
	delB := array.NewBooleanBuilder(mem)
	syncedB := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Microsecond})
	unchangedB := array.NewStringBuilder(mem)
	defer func() {
		idB.Release()
		configB.Release()
		statusB.Release()
		lsnB.Release()
		delB.Release()
		syncedB.Release()
		unchangedB.Release()
	}()

	now := time.Now().UTC()
	for i, r := range rows {
		idB.Append(r.id)
		if r.config == nil {
			configB.AppendNull()
		} else {
			configB.Append(*r.config)
		}
		statusB.Append(r.status)
		lsnB.Append(r.lsn)
		delB.Append(r.deleted)
		syncedB.Append(arrow.Timestamp(now.Add(time.Duration(i) * time.Microsecond).UnixMicro()))
		b, _ := json.Marshal(r.unchanged)
		unchangedB.Append(string(b))
	}

	cols := []arrow.Array{
		idB.NewArray(), configB.NewArray(), statusB.NewArray(),
		lsnB.NewArray(), delB.NewArray(), syncedB.NewArray(), unchangedB.NewArray(),
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	return array.NewRecordBatch(sc, cols, int64(len(rows)))
}

func strPtr(s string) *string { return &s }

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

func TestForwardFillUnchanged(t *testing.T) {
	pk := []string{"id"}

	t.Run("insert then partial update fills omitted toast", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: strPtr(`{"big":true}`), status: "pending", lsn: "0/1"},
			{id: 1, config: nil, status: "done", lsn: "0/2", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()
		require.NotSame(t, in, out, "fill should produce a new batch")

		v, ok := configValueAt(t, out, 1)
		assert.True(t, ok, "config_data should be filled on the partial update row")
		assert.Equal(t, `{"big":true}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1), "filled column must be dropped from _cdc_unchanged_cols")
	})

	t.Run("changed then unchanged overwrites and drops unchanged flag", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: strPtr(`{"v":"A"}`), status: "a", lsn: "0/1"},
			{id: 1, config: nil, status: "b", lsn: "0/2", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()

		v, ok := configValueAt(t, out, 1)
		assert.True(t, ok)
		assert.Equal(t, `{"v":"A"}`, v)
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1))
	})

	t.Run("explicit null carries across rows and drops unchanged flag", func(t *testing.T) {
		// An authoritative NULL (SET config_data = NULL) in one transaction
		// followed by an unchanged TOAST update in another. The NULL must be
		// filled forward and the column dropped from _cdc_unchanged_cols, so the
		// destination does not fall back to the stale (non-null) target value.
		in := buildFillBatch([]fillRow{
			{id: 1, config: nil, status: "a", lsn: "0/1"},
			{id: 1, config: nil, status: "b", lsn: "0/2", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()
		require.NotSame(t, in, out, "the unchanged row should be rewritten")

		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok, "filled value should be NULL")
		assert.Equal(t, `[]`, unchangedValueAt(t, out, 1), "column must be dropped from _cdc_unchanged_cols")
	})

	t.Run("no prior value keeps column unchanged", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: nil, status: "a", lsn: "0/1", unchanged: []string{"config_data"}},
			{id: 1, config: nil, status: "b", lsn: "0/2", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()
		assert.Same(t, in, out, "nothing to fill should return the same batch")

		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok)
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 1))
	})

	t.Run("does not cross primary keys", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: strPtr(`{"id":1}`), status: "a", lsn: "0/1"},
			{id: 2, config: nil, status: "b", lsn: "0/2", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()
		assert.Same(t, in, out, "different pk must not be filled")

		_, ok := configValueAt(t, out, 1)
		assert.False(t, ok)
	})

	t.Run("delete resets prior state", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: strPtr(`{"v":1}`), status: "a", lsn: "0/1"},
			{id: 1, config: nil, status: "", lsn: "0/2", deleted: true},
			{id: 1, config: nil, status: "c", lsn: "0/3", unchanged: []string{"config_data"}},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
		defer out.Release()

		_, ok := configValueAt(t, out, 2)
		assert.False(t, ok, "value before a delete must not carry past it")
		assert.Equal(t, `["config_data"]`, unchangedValueAt(t, out, 2))
	})

	t.Run("preserves unrelated rows exactly", func(t *testing.T) {
		in := buildFillBatch([]fillRow{
			{id: 1, config: strPtr(`{"big":true}`), status: "pending", lsn: "0/1"},
			{id: 1, config: nil, status: "done", lsn: "0/2", unchanged: []string{"config_data"}},
			{id: 2, config: strPtr(`{"other":1}`), status: "x", lsn: "0/3"},
		})
		defer in.Release()

		out := forwardFillUnchanged(in, pk)
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
}
