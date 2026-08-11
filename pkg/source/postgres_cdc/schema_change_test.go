package postgres_cdc

import (
	"encoding/binary"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pgoCol struct {
	name string
	oid  uint32
}

type pgoModCol struct {
	name    string
	oid     uint32
	typemod int32
}

func pgoRelationMsgWithCols(relID uint32, namespace, table string, cols ...pgoCol) []byte {
	modCols := make([]pgoModCol, len(cols))
	for i, col := range cols {
		modCols[i] = pgoModCol{name: col.name, oid: col.oid, typemod: -1}
	}
	return pgoRelationMsgWithTypeMods(relID, namespace, table, modCols...)
}

func pgoRelationMsgWithTypeMods(relID uint32, namespace, table string, cols ...pgoModCol) []byte {
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
		data = binary.BigEndian.AppendUint32(data, uint32(c.typemod))
	}
	return data
}

func numericTypeMod(precision, scale int) int32 {
	return int32(((precision << 16) | (scale & 0x7ff)) + 4)
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

func pgoInsertMsgWithBinaryVals(relID uint32, vals ...[]byte) []byte {
	data := []byte{'I'}
	data = binary.BigEndian.AppendUint32(data, relID)
	data = append(data, 'N')
	data = binary.BigEndian.AppendUint16(data, uint16(len(vals)))
	for _, v := range vals {
		data = append(data, 'b')
		data = binary.BigEndian.AppendUint32(data, uint32(len(v)))
		data = append(data, v...)
	}
	return data
}

func binaryNumeric(t *testing.T, value string) []byte {
	t.Helper()
	var numeric pgtype.Numeric
	require.NoError(t, numeric.Scan(value))
	encoded, err := pgtype.NewMap().Encode(pgtype.NumericOID, pgtype.BinaryFormatCode, numeric, nil)
	require.NoError(t, err)
	return encoded
}

func pgoUpdateMsgWithVals(relID uint32, vals ...string) []byte {
	data := []byte{'U'}
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

func decodeCommitBatch(t *testing.T, d *Decoder, tableSchema *schema.TableSchema) arrow.RecordBatch {
	t.Helper()
	changes, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.NotNil(t, changes)
	batch, err := changesToBatch(changes, tableSchema)
	require.NoError(t, err)
	require.NotNil(t, batch)
	return batch
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
	d.AllowUnknownRelationColumns(map[string]struct{}{"legacy": {}})

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t",
		pgoCol{"id", 23}, pgoCol{"legacy", 25}, pgoCol{"name", 25}), 10)
	require.NoError(t, err)

	_, err = d.Decode(pgoBeginMsg(100), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "ghost", "alice"), 20)
	require.NoError(t, err)
	batch := decodeCommitBatch(t, d, tableSchema)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.Equal(t, "alice", batch.Column(1).(*array.String).Value(0))
}

func TestDecoderRejectsUnknownColumnInFirstRelationUntilCatalogRefresh(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "id", DataType: schema.TypeInt32})
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}, pgoCol{"status", 25}), 10)
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
	assert.Equal(t, "status", schemaErr.Column)
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
	batch := decodeCommitBatch(t, d, tableSchema)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.True(t, batch.Column(1).IsNull(0))
	assert.Equal(t, "active", batch.Column(2).(*array.String).Value(0))
}

func TestUpdateTreatsColumnsMissingFromPublicationTupleAsUnchanged(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "secret", DataType: schema.TypeString},
	)
	d := NewDecoder(tableSchema, "public", "t")
	_, err := d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoBeginMsg(100), 11)
	require.NoError(t, err)
	_, err = d.Decode(pgoUpdateMsgWithVals(1, "7"), 12)
	require.NoError(t, err)
	changes, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, tupleUnchangedMarker, changes[0].Values[1])
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
	batch := decodeCommitBatch(t, d, tableSchema)
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

func TestDecoderRejectsNumericTypeModifierChangedMidStream(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 10, Scale: 2})
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(10, 2)}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(30, 6)}), 20)
	require.ErrorContains(t, err, `column "amount" changed type modifier mid-stream`)
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
}

func TestDecoderRejectsVarcharLengthChangedMidStream(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "name", DataType: schema.TypeString, MaxLength: 20})
	d := NewDecoder(tableSchema, "public", "t")

	_, err := d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"name", 1043, 24}), 10)
	require.NoError(t, err)
	_, err = d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"name", 1043, 44}), 20)
	require.ErrorContains(t, err, `column "name" changed type modifier mid-stream`)
}

func TestDecoderAcceptsTypeModifierTransitionMatchingRefreshedSchema(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 30, Scale: 6})
	d := NewDecoder(tableSchema, "public", "t")
	d.AllowUnknownRelationColumns(map[string]struct{}{"amount": {}})

	_, err := d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(10, 2)}), 10)
	require.NoError(t, err)
	require.True(t, d.relations[1].Stale)
	_, err = d.Decode(pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(30, 6)}), 20)
	require.NoError(t, err)
	require.False(t, d.relations[1].Stale)
}

func TestDecoderSkipsHistoricalBinaryNumericUntilRefreshedTypeModifier(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 35, Scale: 4})
	allowed := map[string]struct{}{"amount": {}}
	d := NewDecoder(tableSchema, "public", "t")
	d.AllowUnknownRelationColumns(allowed)

	oldRelation := pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(30, 0)})
	_, err := d.Decode(oldRelation, 10)
	require.NoError(t, err)
	require.True(t, d.relations[1].Stale)
	_, err = d.Decode(pgoBeginMsg(100), 11)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithBinaryVals(1, binaryNumeric(t, "1000000000000")), 12)
	require.NoError(t, err)

	currentRelation := pgoRelationMsgWithTypeMods(1, "public", "t", pgoModCol{"amount", 1700, numericTypeMod(35, 4)})
	_, err = d.Decode(currentRelation, 13)
	require.NoError(t, err)
	require.False(t, d.relations[1].Stale)
	require.Empty(t, allowed)
	_, err = d.Decode(pgoInsertMsgWithBinaryVals(1, binaryNumeric(t, "1000000000000.1250")), 14)
	require.NoError(t, err)

	changes, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	batch, err := changesToBatch(changes, tableSchema)
	require.NoError(t, err)
	defer batch.Release()
	amounts := batch.Column(0).(*array.Decimal128)
	assert.Equal(t, "1000000000000.1250", decimal128.Num(amounts.Value(0)).ToString(4))
}

func TestDecoderRejectsUnexpectedTargetRelationOID(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "id", DataType: schema.TypeInt32})
	d := NewDecoder(tableSchema, "public", "t")
	d.ExpectRelationID(41)

	_, err := d.Decode(pgoRelationMsgWithCols(42, "public", "t", pgoCol{"id", 23}), 10)
	var reincarnated *TableReincarnatedError
	require.ErrorAs(t, err, &reincarnated)
	assert.Equal(t, "41", reincarnated.Previous)
	assert.Equal(t, "42", reincarnated.Current)
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
	d.AllowUnknownRelationColumns(map[string]struct{}{"priority": {}})

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
	batch := decodeCommitBatch(t, d, tableSchema)
	defer batch.Release()

	assert.Equal(t, int64(9999999999), batch.Column(1).(*array.Int64).Value(0))
}

func TestDecoderResnapshotAllowsOneHistoricalRelationUntilLiveTypeAppears(t *testing.T) {
	tableSchema := schemaChangeTestSchema(schema.Column{Name: "id", DataType: schema.TypeInt32})
	d := NewDecoder(tableSchema, "public", "t")

	oldRelation := pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 25})
	_, err := d.Decode(oldRelation, 10)
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)

	d.AllowUnknownRelationColumns(map[string]struct{}{"id": {}})
	_, err = d.Decode(oldRelation, 10)
	require.NoError(t, err)
	require.True(t, d.relations[1].Stale)
	_, err = d.Decode(pgoBeginMsg(100), 11)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "not-an-int"), 12)
	require.NoError(t, err)

	_, err = d.Decode(pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}), 13)
	require.NoError(t, err)
	require.False(t, d.relations[1].Stale)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7"), 14)
	require.NoError(t, err)
	changes, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, int32(7), changes[0].Values[0])
}

func TestMultiTableDecoderMapsTupleColumnsByName(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "name", DataType: schema.TypeString},
	)
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})
	d.AllowUnknownRelationColumns(map[string]map[string]struct{}{"t": {"legacy": {}}})

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
	batch, err := changesToBatch(batches[0].Changes, tableSchema)
	require.NoError(t, err)
	defer batch.Release()

	assert.Equal(t, int32(7), batch.Column(0).(*array.Int32).Value(0))
	assert.Equal(t, "alice", batch.Column(1).(*array.String).Value(0))
}

func TestMultiTableDecoderReplaysRepeatedHistoricalRenameRelationsUntilCurrentShape(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt32},
		schema.Column{Name: "cohort", DataType: schema.TypeString},
	)
	allowed := map[string]map[string]struct{}{
		"t": {"cohort": {}, "segment": {}},
	}
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})
	d.AllowUnknownRelationColumns(allowed)

	historical := pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}, pgoCol{"segment", 25})
	for range 3 {
		_, err := d.Decode(historical, 10)
		require.NoError(t, err)
		require.True(t, d.relations[1].Stale)
		require.Contains(t, allowed["t"], "segment")
		require.Contains(t, allowed["t"], "cohort")
	}

	current := pgoRelationMsgWithCols(1, "public", "t", pgoCol{"id", 23}, pgoCol{"cohort", 25})
	_, err := d.Decode(current, 20)
	require.NoError(t, err)
	require.False(t, d.relations[1].Stale)
	require.Empty(t, allowed["t"])

	_, err = d.Decode(pgoBeginMsg(100), 21)
	require.NoError(t, err)
	_, err = d.Decode(pgoInsertMsgWithVals(1, "7", "new"), 22)
	require.NoError(t, err)
	groups, err := d.Decode(pgoCommitMsg(100), 100)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Changes, 1)
	assert.Equal(t, "new", groups[0].Changes[0].Values[1])
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

func TestMultiTableDecoderReportsEveryHistoricalSchemaMismatch(t *testing.T) {
	tableSchema := schemaChangeTestSchema(
		schema.Column{Name: "id", DataType: schema.TypeInt64},
		schema.Column{Name: "amount", DataType: schema.TypeDecimal, Precision: 30, Scale: 4},
	)
	d := NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})

	_, err := d.Decode(pgoRelationMsgWithCols(
		1, "public", "t",
		pgoCol{"id", 23},
		pgoCol{"amount", 23},
		pgoCol{"legacy_a", 25},
		pgoCol{"legacy_b", 25},
	), 10)
	var schemaErr *SchemaChangedError
	require.ErrorAs(t, err, &schemaErr)
	assert.ElementsMatch(t, []string{"id", "amount", "legacy_a", "legacy_b"}, schemaErr.Columns())
	assert.Len(t, schemaErr.Mismatches, 4)

	allowed := make(map[string]struct{})
	for _, column := range schemaErr.Columns() {
		allowed[column] = struct{}{}
	}
	d = NewMultiTableDecoder([]source.SourceTableInfo{{Name: "t", Schema: tableSchema}})
	d.AllowUnknownRelationColumns(map[string]map[string]struct{}{"t": allowed})
	_, err = d.Decode(pgoRelationMsgWithCols(
		1, "public", "t",
		pgoCol{"id", 23},
		pgoCol{"amount", 23},
		pgoCol{"legacy_a", 25},
		pgoCol{"legacy_b", 25},
	), 11)
	require.NoError(t, err, "one rebuild must authorize all mismatches from the relation")
	require.True(t, d.relations[1].Stale)
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
