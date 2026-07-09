package postgres_cdc

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertTextValue(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		col      schema.Column
		expected interface{}
	}{
		{
			name:     "boolean true",
			text:     "t",
			col:      schema.Column{DataType: schema.TypeBoolean},
			expected: true,
		},
		{
			name:     "boolean false",
			text:     "f",
			col:      schema.Column{DataType: schema.TypeBoolean},
			expected: false,
		},
		{
			name:     "int16",
			text:     "123",
			col:      schema.Column{DataType: schema.TypeInt16},
			expected: int16(123),
		},
		{
			name:     "int32",
			text:     "12345",
			col:      schema.Column{DataType: schema.TypeInt32},
			expected: int32(12345),
		},
		{
			name:     "int64",
			text:     "123456789012",
			col:      schema.Column{DataType: schema.TypeInt64},
			expected: int64(123456789012),
		},
		{
			name:     "float32",
			text:     "3.14",
			col:      schema.Column{DataType: schema.TypeFloat32},
			expected: float32(3.14),
		},
		{
			name:     "float64",
			text:     "3.14159265359",
			col:      schema.Column{DataType: schema.TypeFloat64},
			expected: 3.14159265359,
		},
		{
			name:     "string",
			text:     "hello world",
			col:      schema.Column{DataType: schema.TypeString},
			expected: "hello world",
		},
		{
			name:     "timestamp",
			text:     "2024-01-15 10:30:45.123456",
			col:      schema.Column{DataType: schema.TypeTimestamp},
			expected: time.Date(2024, 1, 15, 10, 30, 45, 123456000, time.UTC),
		},
		{
			name:     "date",
			text:     "2024-01-15",
			col:      schema.Column{DataType: schema.TypeDate},
			expected: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTextValue(tt.text, tt.col)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReadString(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantStr   string
		wantBytes int
	}{
		{
			name:      "simple string",
			data:      []byte("hello\x00world"),
			wantStr:   "hello",
			wantBytes: 6, // includes null terminator
		},
		{
			name:      "empty string",
			data:      []byte("\x00more"),
			wantStr:   "",
			wantBytes: 1,
		},
		{
			name:      "no null terminator",
			data:      []byte("hello"),
			wantStr:   "hello",
			wantBytes: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, n := readString(tt.data)
			assert.Equal(t, tt.wantStr, str)
			assert.Equal(t, tt.wantBytes, n)
		})
	}
}

func TestDecoderHandleRelation(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "test_table",
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

	decoder := NewDecoder(tableSchema, "public", "test_table")

	// Build a relation message
	var data []byte

	// Relation ID (4 bytes)
	relID := make([]byte, 4)
	binary.BigEndian.PutUint32(relID, 12345)
	data = append(data, relID...)

	// Namespace (null-terminated)
	data = append(data, []byte("public\x00")...)

	// Name (null-terminated)
	data = append(data, []byte("test_table\x00")...)

	// Replica identity (1 byte)
	data = append(data, 'd') // default

	// Number of columns (2 bytes)
	numCols := make([]byte, 2)
	binary.BigEndian.PutUint16(numCols, 2)
	data = append(data, numCols...)

	// Column 1: id
	data = append(data, 0x01) // flags (part of key)
	data = append(data, []byte("id\x00")...)
	colType := make([]byte, 4)
	binary.BigEndian.PutUint32(colType, 23) // int4 OID
	data = append(data, colType...)
	typeMod := make([]byte, 4)
	binary.BigEndian.PutUint32(typeMod, 0xFFFFFFFF) // -1
	data = append(data, typeMod...)

	// Column 2: name
	data = append(data, 0x00) // flags
	data = append(data, []byte("name\x00")...)
	binary.BigEndian.PutUint32(colType, 25) // text OID
	data = append(data, colType...)
	binary.BigEndian.PutUint32(typeMod, 0xFFFFFFFF)
	data = append(data, typeMod...)

	err := decoder.handleRelation(data)
	require.NoError(t, err)

	// Verify relation was stored
	rel, ok := decoder.relations[12345]
	assert.True(t, ok)
	assert.Equal(t, "public", rel.Namespace)
	assert.Equal(t, "test_table", rel.Name)
	assert.Len(t, rel.Columns, 2)
	assert.Equal(t, "id", rel.Columns[0].Name)
	assert.Equal(t, "name", rel.Columns[1].Name)

	// Verify target relation was identified
	assert.Equal(t, uint32(12345), decoder.targetRelID)
}

func TestDecoderBeginAndCommit(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "test_table",
		Schema: "public",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: CDCLSNColumn, DataType: schema.TypeString},
			{Name: CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
	}

	decoder := NewDecoder(tableSchema, "public", "test_table")

	// Begin a transaction; the Begin payload carries the commit ("final") LSN.
	beginData := make([]byte, 8+8+4) // final LSN + timestamp + xid
	binary.BigEndian.PutUint64(beginData[:8], 100)
	err := decoder.handleBegin(beginData)
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(100), decoder.currentTxLSN)
	assert.Nil(t, decoder.pendingChanges)

	// Commit with no changes should return nil batch
	batch, err := decoder.handleCommit()
	require.NoError(t, err)
	assert.Nil(t, batch)
}

func TestDecoderEmitsTruncateAtCommit(t *testing.T) {
	tableSchema := &schema.TableSchema{Columns: cdcMetaColumns()}
	decoder := NewDecoder(tableSchema, "public", "items")
	decoder.targetRelID = 7

	_, err := decoder.Decode(pgoBeginMsg(88), 1)
	require.NoError(t, err)
	changes, err := decoder.Decode(pgoTruncateMsg(7, 9), 2)
	require.NoError(t, err)
	assert.Nil(t, changes)
	changes, err = decoder.Decode(pgoCommitMsg(88), 3)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "TRUNCATE", changes[0].Operation)
	assert.Equal(t, pglogrepl.LSN(88), changes[0].LSN)
}

func TestResolveColumnValue(t *testing.T) {
	t.Run("unchanged uses old tuple on update", func(t *testing.T) {
		change := Change{
			Operation: "UPDATE",
			Values:    []interface{}{int32(1), tupleUnchangedMarker, "done"},
			OldValues: []interface{}{int32(1), `{"testCases":[1,2,3]}`, "pending"},
		}
		assert.Equal(t, `{"testCases":[1,2,3]}`, resolveColumnValue(change, 1))
		assert.Equal(t, "done", resolveColumnValue(change, 2))
	})

	t.Run("unchanged without old tuple becomes nil", func(t *testing.T) {
		change := Change{
			Operation: "UPDATE",
			Values:    []interface{}{int32(1), tupleUnchangedMarker},
			OldValues: []interface{}{int32(1)},
		}
		assert.Nil(t, resolveColumnValue(change, 1))
	})

	t.Run("explicit null stays nil", func(t *testing.T) {
		change := Change{
			Operation: "UPDATE",
			Values:    []interface{}{int32(1), nil},
			OldValues: []interface{}{int32(1), `{"keep":true}`},
		}
		assert.Nil(t, resolveColumnValue(change, 1))
	})
}

func TestUnchangedColumnsJSON(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "config_data", DataType: schema.TypeString},
		{Name: "status", DataType: schema.TypeString},
	}
	change := Change{
		Operation: "UPDATE",
		Values:    []interface{}{int32(1), tupleUnchangedMarker, "done"},
		OldValues: []interface{}{int32(1), `{"big":true}`, "pending"},
	}
	assert.Equal(t, `["config_data"]`, unchangedColumnsJSON(change, cols, 3))
	assert.Equal(t, "[]", unchangedColumnsJSON(Change{Operation: "INSERT", Values: []interface{}{int32(1)}}, cols, 3))
}

func TestApplyIntraBatchFill(t *testing.T) {
	cols := []schema.Column{
		{Name: "id", DataType: schema.TypeInt32},
		{Name: "config_data", DataType: schema.TypeString},
		{Name: "status", DataType: schema.TypeString},
	}
	tableSchema := &schema.TableSchema{
		Columns:     append(cols, cdcMetaColumns()...),
		PrimaryKeys: []string{"id"},
	}

	t.Run("insert then partial update fills unchanged from batch", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "INSERT",
				Values:    []interface{}{int32(1), `{"big":true}`, "pending"},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "done"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"big":true}`, resolveColumnValue(changes[1], 1))
		assert.Equal(t, "done", resolveColumnValue(changes[1], 2))
	})

	t.Run("filled column is dropped from unchanged cols", func(t *testing.T) {
		// A changed value followed by a partial update that leaves the column
		// unchanged, in the same commit. The latest row wins at the destination;
		// because we now have an authoritative value in staging, the column must
		// NOT be reported as unchanged or the merge would keep a stale target.
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), `{"v":"new"}`, "a"},
				OldValues: []interface{}{int32(1)},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "b"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"v":"new"}`, resolveColumnValue(changes[1], 1))
		assert.Equal(t, `[]`, unchangedColumnsJSON(changes[1], tableSchema.Columns, 3))
	})

	t.Run("explicit null then unchanged keeps the null", func(t *testing.T) {
		// SET config_data = NULL, then a partial update that leaves it unchanged.
		// The known value is NULL, so it must be propagated (column dropped from
		// _cdc_unchanged_cols) rather than letting the destination resurrect the
		// stale target value.
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), nil, "a"},
				OldValues: []interface{}{int32(1)},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "b"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Nil(t, resolveColumnValue(changes[1], 1))
		assert.Equal(t, `[]`, unchangedColumnsJSON(changes[1], tableSchema.Columns, 3))
	})

	t.Run("unfilled unchanged column stays in unchanged cols", func(t *testing.T) {
		// No prior value available in the commit, so the column remains unchanged
		// and the destination must fall back to the existing target value.
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "b"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Nil(t, resolveColumnValue(changes[0], 1))
		assert.Equal(t, `["config_data"]`, unchangedColumnsJSON(changes[0], tableSchema.Columns, 3))
	})

	t.Run("pk update carries unchanged toast from old key within commit", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "INSERT",
				Values:    []interface{}{int32(1), `{"keep":true}`, "pending"},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(2), tupleUnchangedMarker, "done"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"keep":true}`, resolveColumnValue(changes[1], 1))
	})

	t.Run("chains multiple unchanged updates", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "INSERT",
				Values:    []interface{}{int32(1), `{"v":1}`, "a"},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "b"},
				OldValues: []interface{}{int32(1)},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "c"},
				OldValues: []interface{}{int32(1)},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"v":1}`, resolveColumnValue(changes[2], 1))
		assert.Equal(t, "c", resolveColumnValue(changes[2], 2))
	})

	t.Run("old tuple still preferred over batch state", func(t *testing.T) {
		changes := []Change{
			{
				Operation: "INSERT",
				Values:    []interface{}{int32(1), `{"from":"insert"}`, "pending"},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "done"},
				OldValues: []interface{}{int32(1), `{"from":"old"}`, "pending"},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"from":"old"}`, resolveColumnValue(changes[1], 1))
		// The old-tuple value is authoritative, so the column must not be
		// reported as unchanged (otherwise a matched merge would ignore it).
		assert.Equal(t, `[]`, unchangedColumnsJSON(changes[1], tableSchema.Columns, 3))
	})

	t.Run("old-tuple resolution drops unchanged flag after same-batch change", func(t *testing.T) {
		// An authoritative change to config, then a later unchanged update whose
		// REPLICA IDENTITY FULL old tuple still carries the value. Compaction
		// keeps only the latest row, so the column must be emitted (not flagged
		// unchanged) or the destination falls back to the stale target value.
		changes := []Change{
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), `{"v":"A"}`, "a"},
				OldValues: []interface{}{int32(1), `{"v":"OLD"}`, "a"},
			},
			{
				Operation: "UPDATE",
				Values:    []interface{}{int32(1), tupleUnchangedMarker, "b"},
				OldValues: []interface{}{int32(1), `{"v":"A"}`, "a"},
			},
		}
		applyIntraBatchFill(changes, tableSchema)
		assert.Equal(t, `{"v":"A"}`, resolveColumnValue(changes[1], 1))
		assert.Equal(t, `[]`, unchangedColumnsJSON(changes[1], tableSchema.Columns, 3))
	})

	t.Run("does not fill across separate commits", func(t *testing.T) {
		// Cross-commit coalescing is handled over the accumulator's flush
		// window (batchAccumulator.flushTable), not by the decoder. A partial
		// UPDATE arriving in its own commit has no prior state and stays
		// unchanged here.
		insert := []Change{{
			Operation: "INSERT",
			Values:    []interface{}{int32(1), `{"big":true}`, "pending"},
		}}
		applyIntraBatchFill(insert, tableSchema)

		update := []Change{{
			Operation: "UPDATE",
			Values:    []interface{}{int32(1), tupleUnchangedMarker, "done"},
			OldValues: []interface{}{int32(1)},
		}}
		applyIntraBatchFill(update, tableSchema)
		assert.Nil(t, resolveColumnValue(update[0], 1))
	})
}

func cdcMetaColumns() []schema.Column {
	return []schema.Column{
		{Name: CDCLSNColumn, DataType: schema.TypeString},
		{Name: CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		{Name: CDCUnchangedColsColumn, DataType: schema.TypeString},
	}
}

func TestNewDecoder(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Name:   "orders",
		Schema: "sales",
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		},
	}

	decoder := NewDecoder(tableSchema, "sales", "orders")

	assert.NotNil(t, decoder)
	assert.Equal(t, "sales", decoder.targetSchema)
	assert.Equal(t, "orders", decoder.targetTable)
	assert.NotNil(t, decoder.relations)
	assert.Empty(t, decoder.relations)
}
