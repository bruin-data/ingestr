package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoColBatch(t *testing.T) arrow.RecordBatch {
	t.Helper()
	alloc := memory.DefaultAllocator
	sc := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	idB := array.NewInt32Builder(alloc)
	defer idB.Release()
	idB.AppendValues([]int32{1}, nil)
	idArr := idB.NewArray()
	defer idArr.Release()
	exB := array.NewStringBuilder(alloc)
	defer exB.Release()
	exB.Append("v")
	exArr := exB.NewArray()
	defer exArr.Release()
	return array.NewRecordBatch(sc, []arrow.Array{idArr, exArr}, 1)
}

// A schema announcement (mid-stream DDL rebuild) must retarget the aligner so
// batches carrying the new column keep it instead of being cast back to the
// job-start shape. Decoration columns only the old target had (e.g.
// _ingestr_loaded_at) are preserved.
func TestWrapRetargetsAlignerOnAnnouncement(t *testing.T) {
	oldTarget := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "_ingestr_loaded_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
	}, nil)
	aligner := NewSafeTypeCaster(oldTarget).EnableRetarget()

	announced := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32, Nullable: true},
		{Name: "extra", DataType: schema.TypeString, Nullable: true},
	}}

	input := make(chan source.RecordBatchResult, 2)
	input <- source.RecordBatchResult{TableInfo: &source.SourceTableInfo{Name: "t", Schema: announced}}
	input <- source.RecordBatchResult{Batch: twoColBatch(t)}
	close(input)

	output := Wrap(input, aligner)

	ann := <-output
	require.NotNil(t, ann.TableInfo)

	res := <-output
	require.NoError(t, res.Err)
	require.NotNil(t, res.Batch)
	defer res.Batch.Release()

	require.Equal(t, int64(3), res.Batch.NumCols())
	assert.Equal(t, "id", res.Batch.ColumnName(0))
	assert.Equal(t, "extra", res.Batch.ColumnName(1))
	assert.Equal(t, "_ingestr_loaded_at", res.Batch.ColumnName(2))
	assert.Equal(t, "v", res.Batch.Column(1).(*array.String).Value(0))
}

// Without EnableRetarget an announcement must not change the target (the
// job-start pin stays authoritative, e.g. for --columns override casters).
func TestWrapIgnoresAnnouncementWithoutRetarget(t *testing.T) {
	oldTarget := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
	}, nil)
	caster := NewSafeTypeCaster(oldTarget)

	announced := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt32, Nullable: true},
		{Name: "extra", DataType: schema.TypeString, Nullable: true},
	}}

	input := make(chan source.RecordBatchResult, 2)
	input <- source.RecordBatchResult{TableInfo: &source.SourceTableInfo{Name: "t", Schema: announced}}
	input <- source.RecordBatchResult{Batch: twoColBatch(t)}
	close(input)

	output := Wrap(input, caster)
	<-output
	res := <-output
	require.NoError(t, res.Err)
	defer res.Batch.Release()
	assert.Equal(t, int64(1), res.Batch.NumCols())
}
