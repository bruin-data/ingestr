package postgres_cdc

import (
	"encoding/binary"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pgoCol struct {
	name string
	oid  uint32
}

func pgoRelationMsgWithCols(relID uint32, namespace, table string, cols ...pgoCol) []byte {
	data := []byte{'R'}
	data = binary.BigEndian.AppendUint32(data, relID)
	data = append(data, []byte(namespace+"\x00")...)
	data = append(data, []byte(table+"\x00")...)
	data = append(data, 'd')
	data = binary.BigEndian.AppendUint16(data, uint16(len(cols)))
	for _, c := range cols {
		data = append(data, 0x00)
		data = append(data, []byte(c.name+"\x00")...)
		data = binary.BigEndian.AppendUint32(data, c.oid)
		data = binary.BigEndian.AppendUint32(data, 0xFFFFFFFF)
	}
	return data
}

func pgoInsertMsgWithVals(relID uint32, vals ...string) []byte {
	data := []byte{'I'}
	data = binary.BigEndian.AppendUint32(data, relID)
	data = append(data, 'N')
	data = binary.BigEndian.AppendUint16(data, uint16(len(vals)))
	for _, v := range vals {
		data = append(data, 't')
		data = binary.BigEndian.AppendUint32(data, uint32(len(v)))
		data = append(data, []byte(v)...)
	}
	return data
}

func schemaChangeTestSchema(cols ...schema.Column) *schema.TableSchema {
	return &schema.TableSchema{
		Name:    "t",
		Schema:  "public",
		Columns: append(cols, cdcMetaColumns()...),
	}
}

// The replicated relation carries a column the connect-time schema does not
// have (WAL replayed from before a DROP COLUMN that happened while the pipeline
// was down). Values must be routed by column name, not ordinal position;
// positional decoding would write "ghost" into name.
func TestDecoderMapsTupleColumnsByName(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "name", DataType: schema.TypeString},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"legacy", 25}, pgoCol{"name", 25}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "ghost", "alice"), 20)
	require.NoError(t, err)
	batch, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.NotNil(t, batch)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.Equal(t, "alice", batch.Column(1).(*array.String).Value(0))
}

// A schema column missing from the replicated relation (WAL replayed from
// before an ADD COLUMN, or a mid-stream DROP COLUMN) decodes to NULL while the
// remaining columns stay aligned; positional decoding would write "active"
// into name.
func TestDecoderNullsSchemaColumnsMissingFromRelation(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "name", DataType: schema.TypeString},
		schema.Column{Name: "status", DataType: schema.TypeString},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"status", 25}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "active"), 20)
	require.NoError(t, err)
	batch, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.NotNil(t, batch)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.True(t, batch.Column(1).IsNull(0))
	assert.Equal(t, "active", batch.Column(2).(*array.String).Value(0))
}

func TestDecoderRejectsColumnAddedMidStream(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"extra", 25}), 20)
	require.ErrorContains(t, err, `column "extra" was added mid-stream`)

	// Streaming readers detect the typed error to rebuild instead of dying.
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
	assert.Equal(t, "public.t", schemaErr.Table)
	assert.Equal(t, "extra", schemaErr.Column)
}

// After a restart the refreshed schema contains the added column; the refreshed
// Relation message must map cleanly instead of erroring again.
func TestDecoderAcceptsAddedColumnPresentInSchema(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "extra", DataType: schema.TypeString},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"extra", 25}), 20)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 20)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "hello"), 30)
	require.NoError(t, err)
	batch, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.NotNil(t, batch)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.Equal(t, "hello", batch.Column(1).(*array.String).Value(0))
}

func TestDecoderRejectsColumnTypeChangedMidStream(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 25}), 20)
	require.ErrorContains(t, err, `column "id" changed type mid-stream`)

	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
	assert.Equal(t, "id", schemaErr.Column)
}

func TestDecoderAllowsUnknownCustomTypeOIDTransition(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "status", DataType: schema.TypeString},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"status", 900000}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"status", 900001}), 20)
	require.NoError(t, err)
	assert.Equal(t, []int{0}, d.relations[1].SchemaIndex)
}

// After a restart the refreshed schema carries the new type; replaying the WAL
// across the type change re-encounters the old-type Relation message first and
// then the new-type one. That transition matches the schema and must pass, or
// the pipeline would error on every restart.
func TestDecoderAcceptsTypeTransitionMatchingSchema(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "priority", DataType: schema.TypeInt64},
	)
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"priority", 23}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"priority", 20}), 20)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 20)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "9999999999"), 30)
	require.NoError(t, err)
	batch, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.NotNil(t, batch)
	defer batch.Release()

	assert.Equal(t, int64(9999999999), batch.Column(1).(*array.Int64).Value(0))
}

func TestMultiTableDecoderMapsTupleColumnsByName(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "name", DataType: schema.TypeString},
	)
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"legacy", 25}, pgoCol{"name", 25}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "ghost", "alice"), 20)
	require.NoError(t, err)
	batches, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.Len(t, batches, 1)
	defer batches[0].Batch.Release()

	assert.Equal(t, int32(7), batches[0].Batch.Column(0).(*array.Int32).Value(0))
	assert.Equal(t, "alice", batches[0].Batch.Column(1).(*array.String).Value(0))
}

func TestMultiTableDecoderRejectsColumnAddedMidStream(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
	)
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"extra", 25}), 20)
	require.ErrorContains(t, err, `column "extra" was added mid-stream`)
}

// A rebuild refreshes every table's schema, so every table whose shape changed
// must be re-announced — not just the one whose Relation message tripped the
// rebuild. A missed announcement would leave the consumer writing new-shape
// batches into old-shape staging.
func TestShapeChangedTables(t *testing.T) {
	prev := map[string]*schema.TableSchema{
		"a": {Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}},
		"b": {Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}},
		"c": {Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt32}}},
	}
	refreshed := []source.SourceTableInfo{
		{Name: "a", Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "extra", DataType: schema.TypeString},
		}}},
		{Name: "b", Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
		}}},
		{Name: "c", Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}}},
		{Name: "new_table", Schema: &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
		}}},
	}

	changed := shapeChangedTables(prev, refreshed)
	require.Equal(t, []int{0, 1}, changed) // a: column added, b: type changed; c unchanged, new_table excluded
}
