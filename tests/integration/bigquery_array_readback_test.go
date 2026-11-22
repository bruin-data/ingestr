//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/stretchr/testify/require"
)

func TestBigQuery_GetTableSchema_PreservesArrayElementTypes(t *testing.T) {
	dest, _, _, dataset := bqDedupSetup(t)
	ctx := context.Background()
	defer func() { _ = dest.Close(ctx) }()

	table := fmt.Sprintf("array_reload_%d", time.Now().UnixNano())
	qualified := dataset + "." + table
	sourceSchema := &schema.TableSchema{
		Name:        table,
		Schema:      dataset,
		PrimaryKeys: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "amounts", DataType: schema.TypeArray, ArrayType: schema.TypeDecimal, Precision: 10, Scale: 2, Nullable: true},
			{Name: "counts", DataType: schema.TypeArray, ArrayType: schema.TypeInt64, Nullable: true},
			{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString, MaxLength: 20, Nullable: true},
		},
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: qualified, Schema: sourceSchema, PrimaryKeys: sourceSchema.PrimaryKeys,
	}))

	readBack, err := dest.GetTableSchema(ctx, qualified)
	require.NoError(t, err)
	byName := make(map[string]schema.Column, len(readBack.Columns))
	for _, c := range readBack.Columns {
		byName[c.Name] = c
	}
	require.Equal(t, schema.TypeDecimal, byName["amounts"].ArrayType)
	require.Equal(t, schema.TypeInt64, byName["counts"].ArrayType)
	require.Equal(t, schema.TypeString, byName["tags"].ArrayType)

	comparison, err := schemaevolution.Compare(sourceSchema, readBack, nil)
	require.NoError(t, err)
	require.False(t, comparison.HasChanges, "unchanged arrays must not evolve: %+v", comparison.Changes)
}
