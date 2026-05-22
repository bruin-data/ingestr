package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	mysqldest "github.com/bruin-data/ingestr/pkg/destination/mysql"
	pgdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
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
	defer func() {
		_ = dest.Close(ctx)
	}()

	err = dest.SwapTable(ctx, destination.SwapOptions{StagingTable: stagingTable, TargetTable: targetTable})
	require.Error(t, err, "SwapTable should fail when the target table has dependent objects")
	require.Contains(t, err.Error(), "failed to drop old table",
		"Error should clearly indicate the drop failure, not a misleading commit/rollback message")
}

// TestMySQLSwapTable_LongTargetName is a regression test for a bug where swap
// implementations built their rename-target name as `<targetName>_old_<unixNano>`
// without length-bounding. When the result exceeded MySQL's 64-char identifier
// limit, RENAME TABLE failed with "Identifier name '...' is too long" and the
// swap aborted — failing the entire ingestion strategy.
//
// The 19-digit unix-nano suffix plus `_old_` adds 24 chars on top of the target
// table name, so any target ≥ 41 chars triggers the bug on MySQL.
func TestMySQLSwapTable_LongTargetName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("shared mysql dest container not available")
	}

	ctx := context.Background()
	db, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	suffix := uniqueSuffix()
	// 45-char unqualified target table name: <name>_old_<19-digit nano> = 69 chars,
	// past MySQL's 64-char identifier limit.
	targetTable := fmt.Sprintf("%s_%s", strings.Repeat("a", 35), suffix[len(suffix)-9:])
	require.Len(t, targetTable, 45, "test scenario requires a 45-char target table name")
	stagingTable := fmt.Sprintf("%s_tmp", targetTable[:35])
	qualifiedTarget := fmt.Sprintf("%s.%s", mysqlDB, targetTable)
	qualifiedStaging := fmt.Sprintf("%s.%s", mysqlDB, stagingTable)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", mysqlDB, targetTable))
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", mysqlDB, stagingTable))
		// ShortenIdentifier preserves only the first ~29 chars of the candidate as
		// a literal prefix on MySQL (64-char limit), so the LIKE prefix must stay
		// below that to also match a shortened orphan.
		rows, err := db.QueryContext(ctx,
			"SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_name LIKE ?",
			mysqlDB, targetTable[:20]+"%_old_%")
		if err == nil && rows != nil {
			defer func() {
				_ = rows.Close()
			}()
			for rows.Next() {
				var name string
				_ = rows.Scan(&name)
				_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", mysqlDB, name))
			}
		}
	})

	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE `%s`.`%s` (id INT)", mysqlDB, targetTable))
	require.NoError(t, err, "precondition: 45-char target table fits within mysql's 64-char limit")
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE `%s`.`%s` (id INT)", mysqlDB, stagingTable))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO `%s`.`%s` VALUES (1)", mysqlDB, stagingTable))
	require.NoError(t, err)

	dest := mysqldest.NewMySQLDestination()
	require.NoError(t, dest.Connect(ctx, mysqlDest.uri))
	defer func() { _ = dest.Close(ctx) }()

	// Pre-fix this call returned "Error 1059: Identifier name '<69-char>' is too long".
	err = dest.SwapTable(ctx, destination.SwapOptions{
		StagingTable: qualifiedStaging,
		TargetTable:  qualifiedTarget,
	})
	require.NoError(t, err, "swap of long-named target must not fail on mysql identifier limit")

	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", mysqlDB, targetTable)).Scan(&count))
	require.Equal(t, 1, count, "swapped target must contain the staging row")
}
