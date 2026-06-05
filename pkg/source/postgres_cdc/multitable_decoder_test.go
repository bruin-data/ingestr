package postgres_cdc

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
