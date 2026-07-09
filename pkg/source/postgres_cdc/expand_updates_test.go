package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keyedTestSchema() *schema.TableSchema {
	return &schema.TableSchema{
		Columns: append([]schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "payload", DataType: schema.TypeString},
		}, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}
}

func keylessTestSchema() *schema.TableSchema {
	s := keyedTestSchema()
	s.PrimaryKeys = nil
	return s
}

func TestExpandUpdatesKeyChange(t *testing.T) {
	t.Run("pk-changing update emits delete for the old key", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				LSN:       7,
				Values:    []interface{}{int32(2), "moved"},
				OldValues: []interface{}{int32(1), nil},
			},
		}
		out := expandUpdates(changes, keyedTestSchema())
		require.Len(t, out, 2)

		assert.Equal(t, "DELETE", out[0].Operation)
		assert.Equal(t, []interface{}{int32(1), nil}, out[0].Values)
		assert.Equal(t, changes[0].LSN, out[0].LSN)

		assert.Equal(t, "UPDATE", out[1].Operation)
		assert.Equal(t, []interface{}{int32(2), "moved"}, out[1].Values)
	})

	t.Run("update with unchanged pk is not expanded", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), "new"},
				OldValues: []interface{}{int32(1), "old"},
			},
		}
		out := expandUpdates(changes, keyedTestSchema())
		require.Len(t, out, 1)
		assert.Equal(t, "UPDATE", out[0].Operation)
	})

	t.Run("update without old tuple is not expanded", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), "new"},
			},
		}
		out := expandUpdates(changes, keyedTestSchema())
		require.Len(t, out, 1)
		assert.Equal(t, "UPDATE", out[0].Operation)
	})

	t.Run("inserts and deletes pass through untouched", func(t *testing.T) {
		changes := []Change{
			{Operation: "INSERT", Values: []interface{}{int32(1), "a"}},
			{Operation: "DELETE", Values: []interface{}{int32(1), nil}},
		}
		out := expandUpdates(changes, keyedTestSchema())
		assert.Equal(t, changes, out)
	})
}

func TestExpandUpdatesKeyless(t *testing.T) {
	t.Run("update becomes delete of old image plus insert of new image", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				LSN:       9,
				Values:    []interface{}{int32(1), "new"},
				OldValues: []interface{}{int32(1), "old"},
			},
		}
		out := expandUpdates(changes, keylessTestSchema())
		require.Len(t, out, 2)

		assert.Equal(t, "DELETE", out[0].Operation)
		assert.Equal(t, []interface{}{int32(1), "old"}, out[0].Values)

		assert.Equal(t, "INSERT", out[1].Operation)
		assert.Equal(t, []interface{}{int32(1), "new"}, out[1].Values)
	})

	t.Run("unchanged toast marker resolves from the full old image", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker},
				OldValues: []interface{}{int32(1), "toasted"},
			},
		}
		out := expandUpdates(changes, keylessTestSchema())
		require.Len(t, out, 2)
		assert.Equal(t, []interface{}{int32(1), "toasted"}, out[1].Values)
	})

	t.Run("update without old image stays an update", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), "new"},
			},
		}
		out := expandUpdates(changes, keylessTestSchema())
		require.Len(t, out, 1)
		assert.Equal(t, "UPDATE", out[0].Operation)
	})
}

// TestDecoderCommitEmitsKeyChangeDelete drives a pk-changing UPDATE through
// handleCommit and asserts the emitted batch carries both the old-key delete
// row and the new-key row, so the destination merge removes the old row.
func TestDecoderCommitEmitsKeyChangeDelete(t *testing.T) {
	tableSchema := keyedTestSchema()
	decoder := NewDecoder(tableSchema, "public", "test_table")
	decoder.currentTxLSN = 42
	decoder.pendingChanges = []Change{
		{
			Operation: "UPDATE",
			LSN:       42,
			Values:    []interface{}{int32(2), "moved"},
			OldValues: []interface{}{int32(1), nil},
		},
	}

	changes, err := decoder.handleCommit()
	require.NoError(t, err)
	applyIntraBatchFill(changes, tableSchema)
	changes = expandUpdates(changes, tableSchema)
	changes = compactChanges(changes, tableSchema)

	batch, err := changesToBatch(changes, tableSchema)
	require.NoError(t, err)
	require.NotNil(t, batch)
	defer batch.Release()

	require.EqualValues(t, 2, batch.NumRows())

	ids := batch.Column(0).(*array.Int32)
	deleted := batch.Column(3).(*array.Boolean)

	assert.Equal(t, int32(1), ids.Value(0))
	assert.True(t, deleted.Value(0))

	assert.Equal(t, int32(2), ids.Value(1))
	assert.False(t, deleted.Value(1))
}
