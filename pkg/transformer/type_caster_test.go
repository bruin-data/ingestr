package transformer

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestTypeCasterKeepsSourceFieldMetadataWhenNothingIsCast(t *testing.T) {
	metadata := arrow.NewMetadata([]string{schema.JSONValidatedMetadataKey}, []string{"true"})
	source := arrow.NewSchema([]arrow.Field{{Name: "doc", Type: schema.JSONArrowType, Nullable: true, Metadata: metadata}}, nil)
	target := arrow.NewSchema([]arrow.Field{{Name: "doc", Type: schema.JSONArrowType, Nullable: true}}, nil)
	builder := schema.NewJSONBuilder(memory.DefaultAllocator)
	defer builder.Release()
	builder.Append(`{"a": 1}`)
	arr := builder.NewArray()
	defer arr.Release()
	batch := array.NewRecordBatch(source, []arrow.Array{arr}, 1)
	defer batch.Release()
	out, err := NewTypeCaster(target).EnableJSONText().Transform(batch)
	require.NoError(t, err)
	defer out.Release()
	require.Same(t, batch, out)
	value, ok := out.Schema().Field(0).Metadata.GetValue(schema.JSONValidatedMetadataKey)
	require.True(t, ok)
	require.Equal(t, "true", value)
}
