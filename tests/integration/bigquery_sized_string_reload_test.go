//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

// TestBigQuery_GetTableSchema_PreservesStringMaxLength guards a schema-evolution
// regression: re-reading an existing BigQuery table dropped the bounded
// STRING(n) length, so the destination looked unbounded. On the second load of
// an otherwise unchanged schema, the prepared schema (derived from the
// read-back) was unbounded while the real column was bounded, and PrepareTable
// rejected it with "existing max length n is bounded, want unbounded".
func TestBigQuery_GetTableSchema_PreservesStringMaxLength(t *testing.T) {
	dest, _, _, dataset := bqDedupSetup(t)
	ctx := context.Background()

	table := fmt.Sprintf("sized_reload_%d", time.Now().UnixNano())
	qualified := dataset + "." + table
	defer func() { _ = dest.Close(ctx) }()

	primaryKeys := []string{"id"}
	sourceSchema := &schema.TableSchema{
		Name:        table,
		Schema:      dataset,
		PrimaryKeys: primaryKeys,
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString, MaxLength: 50},
			{Name: "bio", DataType: schema.TypeString}, // unbounded (TEXT-like)
		},
	}

	// First load: create the table with a bounded name column (STRING(50)).
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       qualified,
		Schema:      sourceSchema,
		PrimaryKeys: primaryKeys,
	}))

	// Reading the table back must preserve the bound. Before the fix this
	// returned MaxLength 0 for every string column.
	readBack, err := dest.GetTableSchema(ctx, qualified)
	require.NoError(t, err)
	byName := make(map[string]schema.Column, len(readBack.Columns))
	for _, c := range readBack.Columns {
		byName[c.Name] = c
	}
	require.Equal(t, 50, byName["name"].MaxLength, "bounded STRING(50) length must survive read-back")
	require.Equal(t, 0, byName["bio"].MaxLength, "unbounded STRING must stay unbounded")

	// Second load: on an incremental run the pipeline builds the prepared schema
	// from the read-back. Preparing again against the existing bounded table must
	// not error.
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:       qualified,
		Schema:      readBack,
		PrimaryKeys: primaryKeys,
	}))
}
