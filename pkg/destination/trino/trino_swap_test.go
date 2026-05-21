package trino

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "github.com/trinodb/trino-go-client/trino"
)

// TestSwapTable_CrossSchema reproduces and locks in the fix for the bug where
// gong's SwapTable used a bare new-name in ALTER TABLE RENAME TO, which Trino
// interprets as an in-place rename within the staging schema. With the
// _bruin_staging design every swap is cross-schema, so the staging table
// silently never reached the target schema.
//
// Requires a Trino instance reachable at TRINO_URI (e.g.
// `trino://user@localhost:18080/memory/default`). Skipped otherwise.
func TestSwapTable_CrossSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Trino integration test in short mode")
	}
	uri := os.Getenv("TRINO_URI")
	if uri == "" {
		t.Skip("TRINO_URI not set")
	}

	ctx := context.Background()
	dest := NewTrinoDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(ctx) })

	suffix := time.Now().UnixNano()
	stagingSchema := fmt.Sprintf("_bruin_staging_%d", suffix)
	targetSchema := fmt.Sprintf("public_%d", suffix)
	stagingTable := fmt.Sprintf("%s.%s.t", dest.catalog, stagingSchema)
	targetTable := fmt.Sprintf("%s.%s.t", dest.catalog, targetSchema)

	t.Cleanup(func() {
		_, _ = dest.db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"."t"`, dest.catalog, stagingSchema))
		_, _ = dest.db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."%s"."t"`, dest.catalog, targetSchema))
		_, _ = dest.db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s"."%s"`, dest.catalog, stagingSchema))
		_, _ = dest.db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s"."%s"`, dest.catalog, targetSchema))
	})

	// Prepare ONLY the staging side, mirroring what the replace strategy does.
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
		},
	}
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:     stagingTable,
		Schema:    tableSchema,
		DropFirst: true,
	}))
	_, err := dest.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO "%s"."%s"."t" VALUES (1, 'a'), (2, 'b')`,
			dest.catalog, stagingSchema))
	require.NoError(t, err)

	// Sanity-check: target schema must not exist yet — proves the swap is the
	// thing that has to create it.
	var preCount int
	require.NoError(t, dest.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM "%s".information_schema.schemata WHERE schema_name = '%s'`,
		dest.catalog, targetSchema,
	)).Scan(&preCount))
	require.Equal(t, 0, preCount, "precondition: target schema must not exist yet")

	require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: stagingTable,
		TargetTable:  targetTable,
	}))

	// Target schema must now exist and hold the rows; staging must be empty.
	var rows int64
	require.NoError(t, dest.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM "%s"."%s"."t"`, dest.catalog, targetSchema)).Scan(&rows))
	assert.Equal(t, int64(2), rows, "target should hold the rows after swap")

	var stagingRows int
	err = dest.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM "%s".information_schema.tables WHERE table_schema = '%s'`,
			dest.catalog, stagingSchema)).Scan(&stagingRows)
	require.NoError(t, err)
	assert.Equal(t, 0, stagingRows, "staging schema should have no tables after swap")
}
