package postgres_cdc

import (
	"encoding/binary"
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

// pgoutput message builders (payloads include the leading type byte, as fed to
// Decode from XLogData).

func pgoRelationMsg(relID uint32, namespace, table string) []byte {
	data := []byte{'R'}
	data = binary.BigEndian.AppendUint32(data, relID)
	data = append(data, []byte(namespace+"\x00")...)
	data = append(data, []byte(table+"\x00")...)
	data = append(data, 'd')                      // replica identity
	data = binary.BigEndian.AppendUint16(data, 1) // one column
	data = append(data, 0x01)                     // flags: part of key
	data = append(data, []byte("id\x00")...)
	data = binary.BigEndian.AppendUint32(data, 23)         // int4 OID
	data = binary.BigEndian.AppendUint32(data, 0xFFFFFFFF) // typemod -1
	return data
}

func pgoBeginMsg(finalLSN uint64) []byte {
	data := []byte{'B'}
	data = binary.BigEndian.AppendUint64(data, finalLSN)
	data = binary.BigEndian.AppendUint64(data, 0) // commit timestamp
	data = binary.BigEndian.AppendUint32(data, 1) // xid
	return data
}

func pgoInsertMsg(relID uint32, idText string) []byte {
	data := []byte{'I'}
	data = binary.BigEndian.AppendUint32(data, relID)
	data = append(data, 'N')
	data = binary.BigEndian.AppendUint16(data, 1) // one column
	data = append(data, 't')
	data = binary.BigEndian.AppendUint32(data, uint32(len(idText)))
	data = append(data, []byte(idText)...)
	return data
}

func pgoCommitMsg(commitLSN uint64) []byte {
	data := []byte{'C', 0}
	data = binary.BigEndian.AppendUint64(data, commitLSN)
	data = binary.BigEndian.AppendUint64(data, commitLSN)
	data = binary.BigEndian.AppendUint64(data, 0) // commit timestamp
	return data
}

// Transactions are delivered in commit order, but their Begin records'
// positions interleave under concurrent writers: a transaction that began
// earlier can commit later. Batches must be stamped with the Begin payload's
// final (commit) LSN — stamping the Begin record's WAL position makes the
// delivered LSN sequence non-monotonic, and the per-table filter
// (ShouldFilterChange keeps only changeLSN >= lastProcessed) would silently
// drop the later transaction's data.
func TestMultiTableDecoderStampsCommitLSN(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "t",
		Schema: "public",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: CDCLSNColumn, DataType: schema.TypeString},
			{Name: CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})

	_, err := d.Decode(pgoRelationMsg(1, "public", "t"), 10)
	require.NoError(t, err)

	// Transaction B: began at WAL position 150, commits first at 300.
	_, err = d.Decode(pgoBeginMsg(300), 150)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsg(1, "7"), 160)
	require.NoError(t, err)
	batches, err := d.Decode(pgoCommitMsg(300), 300)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, pglogrepl.LSN(300), batches[0].LSN)
	batches[0].Batch.Release()

	// Transaction A: began EARLIER (WAL position 100) but commits later at 400.
	// With begin-position stamping its LSN would be 100 < 300 and the filter
	// would drop it as already processed.
	_, err = d.Decode(pgoBeginMsg(400), 100)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsg(1, "8"), 110)
	require.NoError(t, err)
	batches, err = d.Decode(pgoCommitMsg(400), 400)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	assert.Equal(t, pglogrepl.LSN(400), batches[0].LSN)
	batches[0].Batch.Release()
}
