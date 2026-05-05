package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	pgdest "github.com/bruin-data/gong/pkg/destination/postgres"
	"github.com/stretchr/testify/require"
)

// TestSwapTable_FailedRenameAbortsTransaction reproduces a bug where SwapTable
// silently discards the error from DROP TABLE of the old table (line: _, _ = tx.Exec).
// When the old table has dependent objects (e.g., a view), the DROP fails, which
// aborts the PostgreSQL transaction. The subsequent Commit() then returns the
// misleading "commit unexpectedly resulted in rollback" instead of the real cause.
//
// The scenario: target table has a dependent view. The rename to _old_ succeeds
// (PostgreSQL views reference by OID), but DROP TABLE _old_ fails because of
// the view dependency. The discarded error aborts the transaction.
func TestSwapTable_FailedRenameAbortsTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	uri := sharedPostgresURI(t, "dest")
	schema := uniqueSchemaName(t, "swap")
	ensurePostgresSchema(t, ctx, uri, schema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, uri, schema) })

	db, err := sql.Open("pgx", uri)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	targetTable := schema + ".users"
	stagingTable := schema + ".users__gong_tmp"

	// Create the target table (simulating a previous ingestion run).
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id INT, name TEXT)`, pqTable(schema, "users"),
	))
	require.NoError(t, err)

	// Create a view that depends on the target table.
	// This will cause ALTER TABLE ... RENAME TO to fail because of the dependency.
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`CREATE VIEW %s AS SELECT * FROM %s`,
		pqTable(schema, "users_view"), pqTable(schema, "users"),
	))
	require.NoError(t, err)

	// Create the staging table with new data (simulating what the pipeline does
	// before calling SwapTable).
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (id INT, name TEXT)`, pqTable(schema, "users__gong_tmp"),
	))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s VALUES (1, 'alice'), (2, 'bob')`, pqTable(schema, "users__gong_tmp"),
	))
	require.NoError(t, err)

	// Connect the destination and call SwapTable directly.
	dest := pgdest.NewPostgresDestination()
	err = dest.Connect(ctx, uri)
	require.NoError(t, err)
	defer dest.Close(ctx)

	err = dest.SwapTable(ctx, stagingTable, targetTable)
	require.Error(t, err, "SwapTable should fail when the target table has dependent objects")
	require.Contains(t, err.Error(), "failed to drop old table",
		"Error should clearly indicate the drop failure, not a misleading commit/rollback message")
}
