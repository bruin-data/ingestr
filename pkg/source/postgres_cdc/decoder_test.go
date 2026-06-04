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
		},
	}

	decoder := NewDecoder(tableSchema, "public", "test_table")

	// Begin a transaction
	beginData := make([]byte, 8+8+4) // LSN + timestamp + xid
	err := decoder.handleBegin(beginData, pglogrepl.LSN(100))
	require.NoError(t, err)
	assert.Equal(t, pglogrepl.LSN(100), decoder.currentTxLSN)
	assert.Nil(t, decoder.pendingChanges)

	// Commit with no changes should return nil batch
	batch, err := decoder.handleCommit()
	require.NoError(t, err)
	assert.Nil(t, batch)
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
