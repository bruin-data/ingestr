package iceberg

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestParsePartitionExpression(t *testing.T) {
	terms, err := parsePartitionExpression("day(created_at), bucket[16](id), truncate[4](category), region")
	require.NoError(t, err)
	require.Len(t, terms, 4)
	require.Equal(t, "created_at", terms[0].source)
	require.Equal(t, "created_at_day", terms[0].name)
	require.Equal(t, "day", terms[0].transform.String())
	require.Equal(t, "id_bucket_16", terms[1].name)
	require.Equal(t, "bucket[16]", terms[1].transform.String())
	require.Equal(t, "category_truncate_4", terms[2].name)
	require.Equal(t, "truncate[4]", terms[2].transform.String())
	require.Equal(t, "region", terms[3].name)
	require.Equal(t, "identity", terms[3].transform.String())
}

func TestParsePartitionExpressionRejectsInvalidFields(t *testing.T) {
	for _, expression := range []string{
		"day()",
		"unknown(created_at)",
		"bucket[0](id)",
		"id,,region",
		"day(created_at",
		"day(created_at),created_at_day",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := parsePartitionExpression(expression)
			require.Error(t, err)
		})
	}
}

func TestBuildPartitionSpecWithMultipleTransforms(t *testing.T) {
	iceSchema, err := icebergSchemaFromTableSchema(&schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64, Nullable: false},
		{Name: "created_at", DataType: schema.TypeTimestamp, Nullable: false},
		{Name: "category", DataType: schema.TypeString, Nullable: true},
		{Name: "region", DataType: schema.TypeString, Nullable: true},
	}})
	require.NoError(t, err)

	spec, err := buildPartitionSpec(iceSchema, "day(created_at),bucket[16](id),truncate[4](category),region")
	require.NoError(t, err)
	fields := make([]struct {
		name      string
		transform string
	}, 0, 4)
	for _, field := range spec.Fields() {
		fields = append(fields, struct {
			name      string
			transform string
		}{name: field.Name, transform: field.Transform.String()})
	}
	require.Len(t, fields, 4)
	require.Equal(t, "created_at_day", fields[0].name)
	require.Equal(t, "day", fields[0].transform)
	require.Equal(t, "id_bucket_16", fields[1].name)
	require.Equal(t, "bucket[16]", fields[1].transform)
	require.Equal(t, "category_truncate_4", fields[2].name)
	require.Equal(t, "region", fields[3].name)
}
