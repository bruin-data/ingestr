package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeCasterStringToJSON(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	sourceSchema := arrow.NewSchema([]arrow.Field{{Name: "payload", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
	builder := array.NewRecordBuilder(mem, sourceSchema)
	builder.Field(0).(*array.StringBuilder).Append(`{"k":"v"}`)
	builder.Field(0).(*array.StringBuilder).Append(`not json`)
	builder.Field(0).(*array.StringBuilder).AppendNull()
	batch := builder.NewRecordBatch()
	builder.Release()
	defer batch.Release()

	target := arrow.NewSchema([]arrow.Field{{Name: "payload", Type: schema.JSONArrowType, Nullable: true}}, nil)
	cast := func(caster *TypeCaster) *array.String {
		casted, err := caster.Transform(batch)
		require.NoError(t, err)
		t.Cleanup(casted.Release)
		return casted.Column(0).(array.ExtensionArray).Storage().(*array.String)
	}

	text := cast(NewSafeTypeCaster(target).EnableJSONText())
	assert.Equal(t, `{"k":"v"}`, text.Value(0))
	assert.Equal(t, `"not json"`, text.Value(1))
	assert.True(t, text.IsNull(2))

	quoted := cast(NewSafeTypeCaster(target))
	assert.Equal(t, `"{\"k\":\"v\"}"`, quoted.Value(0), "inference flows keep quoting strings")
	assert.Equal(t, `"not json"`, quoted.Value(1))
	assert.True(t, quoted.IsNull(2))
}
