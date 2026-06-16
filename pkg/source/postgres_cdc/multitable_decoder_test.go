package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// User-provided merge keys (SourceTableInfo.PrimaryKeys) must be reconciled into
// the schema so the decoder, compaction, and unchanged-TOAST fill all key off
// them — otherwise tables without a detected database primary key silently run
// the fill pass with no keys and lose unchanged TOAST values.
func TestNewMultiTableCDCReaderReconcilesPrimaryKeys(t *testing.T) {
	t.Run("user keys override empty schema keys", func(t *testing.T) {
		withKeys := &schema.TableSchema{Name: "events", PrimaryKeys: nil}
		NewMultiTableCDCReader(nil, []source.SourceTableInfo{
			{Name: "events", Schema: withKeys, PrimaryKeys: []string{"event_id"}},
		}, CDCConfig{}, nil, "")
		assert.Equal(t, []string{"event_id"}, withKeys.PrimaryKeys)
	})

	t.Run("detected schema keys are preserved when no override", func(t *testing.T) {
		detected := &schema.TableSchema{Name: "accounts", PrimaryKeys: []string{"id"}}
		NewMultiTableCDCReader(nil, []source.SourceTableInfo{
			{Name: "accounts", Schema: detected, PrimaryKeys: nil},
		}, CDCConfig{}, nil, "")
		assert.Equal(t, []string{"id"}, detected.PrimaryKeys)
	})
}

func TestMultiTableDecoderChangesToBatchSequencesSyncedAt(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "accounts",
		Schema: "public",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "name", DataType: schema.TypeString},
			{Name: CDCLSNColumn, DataType: schema.TypeString},
			{Name: CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}

	decoder := NewMultiTableDecoder(nil)
	changes := []Change{
		{
			Operation: "UPDATE",
			LSN:       pglogrepl.LSN(42),
			Values:    []interface{}{int32(1), "alpha-updated"},
		},
		{
			Operation: "DELETE",
			LSN:       pglogrepl.LSN(42),
			Values:    []interface{}{int32(1), "alpha-updated"},
		},
	}

	batch, err := decoder.changesToBatch(changes, tableSchema)
	require.NoError(t, err)
	defer batch.Release()

	syncedAt := batch.Column(4).(*array.Timestamp)
	assert.Greater(t, syncedAt.Value(1), syncedAt.Value(0))
	assert.Equal(t, "00000000/0000002A", batch.Column(2).(*array.String).Value(0))
	assert.Equal(t, "00000000/0000002A", batch.Column(2).(*array.String).Value(1))
	assert.False(t, batch.Column(3).(*array.Boolean).Value(0))
	assert.True(t, batch.Column(3).(*array.Boolean).Value(1))
}
