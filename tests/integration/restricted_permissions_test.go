//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests reproduce the most realistic production failure mode for the
// `_bruin_staging` schema design: a gong user that has table-level grants on its
// target schema but lacks `CREATE DATABASE`/`CREATE SCHEMA` privileges. The
// expected behavior is:
//
//   1. Without a pre-created `_bruin_staging`, gong fails with a clear permission
//      error pointing at the missing privilege.
//   2. After a DBA pre-creates `_bruin_staging` and grants table-level access on
//      it, gong succeeds without needing global CREATE rights.
//
// Each engine has its own subtest because user/role creation syntax differs.

// TestPostgresDestination_RestrictedPermissions verifies the permission contract on
// Postgres. Restricted user gets USAGE+CREATE on `public` only — no rights on the
// `_bruin_staging` schema until the DBA grants them.
func TestPostgresDestination_RestrictedPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if pgDest.uri == "" {
		t.Skip("shared postgres dest container not available")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	adminDB, err := sql.Open("pgx", pgDest.uri)
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()

	suffix := uniqueSuffix()
	user := fmt.Sprintf("gong_restricted_%s", suffix)
	password := "test_pass_123"
	targetTable := fmt.Sprintf("public.restricted_target_%s", suffix)

	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, pqTable("public", strings.TrimPrefix(targetTable, "public."))))
		_, _ = adminDB.ExecContext(ctx, `DROP SCHEMA IF EXISTS _bruin_staging CASCADE`)
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`REVOKE ALL PRIVILEGES ON SCHEMA public FROM %s`, pqIdent(user)))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP USER IF EXISTS %s`, pqIdent(user)))
	})

	// Restricted user: can create/read/write tables in public, but cannot create
	// new schemas (no CREATE on the database itself).
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE USER %s WITH PASSWORD '%s'`, pqIdent(user), password))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO %s`, pqIdent(user)))
	require.NoError(t, err)

	restrictedURI := postgresURIWithUser(t, pgDest.uri, user, password)

	t.Run("fails_without_staging_schema_grant", func(t *testing.T) {
		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test",
			DestURI:             restrictedURI,
			DestTable:           targetTable,
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge, // exercises staging-schema creation
		}
		err := runPipeline(t, ctx, pipeline.New(cfg))
		require.Error(t, err, "ingest should fail when restricted user can't create _bruin_staging")
		errMsg := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(errMsg, "permission denied") || strings.Contains(errMsg, "must be owner") ||
				strings.Contains(errMsg, "not authorized"),
			"error should clearly indicate a permission issue, got: %s", err.Error())
	})

	t.Run("succeeds_when_admin_pre_creates_staging", func(t *testing.T) {
		// DBA workaround: pre-create _bruin_staging and grant the restricted user
		// table-level access. After this, gong's CREATE SCHEMA IF NOT EXISTS is a
		// no-op and the restricted user can create staging tables inside the schema.
		_, err := adminDB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS _bruin_staging`)
		require.NoError(t, err)
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf(
			`GRANT USAGE, CREATE ON SCHEMA _bruin_staging TO %s`, pqIdent(user),
		))
		require.NoError(t, err)

		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test_2",
			DestURI:             restrictedURI,
			DestTable:           targetTable,
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge,
		}
		require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)),
			"ingest should succeed once _bruin_staging is pre-created and granted")

		var count int
		require.NoError(t, adminDB.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, pqTable("public", strings.TrimPrefix(targetTable, "public.")))).Scan(&count))
		assert.Equal(t, replaceFixtureRows, count)
	})
}

// TestMySQLDestination_RestrictedPermissions verifies the permission contract on
// MySQL. Restricted user gets full grants on the URI's database but NO global CREATE,
// so they cannot create the `_bruin_staging` database.
func TestMySQLDestination_RestrictedPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mysqlDest.uri == "" {
		t.Skip("shared mysql dest container not available")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	adminDB, err := sql.Open("mysql", mysqlDSN(mysqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()

	parsedURI, err := url.Parse(mysqlDest.uri)
	require.NoError(t, err)
	defaultDB := strings.TrimPrefix(parsedURI.Path, "/")
	require.NotEmpty(t, defaultDB, "MySQL URI must include a database")

	suffix := uniqueSuffix()
	user := fmt.Sprintf("gong_r_%s", suffix)
	password := "test_pass_123"
	targetTable := fmt.Sprintf("restricted_target_%s", suffix)

	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", defaultDB, targetTable))
		_, _ = adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS `_bruin_staging`")
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user))
	})

	// Create user with full rights on the URI's DB only — no global CREATE.
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", user, password))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%%'", defaultDB, user))
	require.NoError(t, err)

	restrictedURI := mysqlURIWithUser(t, mysqlDest.uri, user, password)

	t.Run("fails_without_staging_database_grant", func(t *testing.T) {
		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test",
			DestURI:             restrictedURI,
			DestTable:           fmt.Sprintf("%s.%s", defaultDB, targetTable),
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge,
		}
		err := runPipeline(t, ctx, pipeline.New(cfg))
		require.Error(t, err, "ingest should fail when restricted user can't create _bruin_staging")
		errMsg := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(errMsg, "access denied") || strings.Contains(errMsg, "denied") ||
				strings.Contains(errMsg, "command denied"),
			"error should clearly indicate a permission issue, got: %s", err.Error())
	})

	t.Run("succeeds_when_admin_pre_creates_staging", func(t *testing.T) {
		_, err := adminDB.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `_bruin_staging`")
		require.NoError(t, err)
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON `_bruin_staging`.* TO '%s'@'%%'", user))
		require.NoError(t, err)
		_, err = adminDB.ExecContext(ctx, "FLUSH PRIVILEGES")
		require.NoError(t, err)

		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test_2",
			DestURI:             restrictedURI,
			DestTable:           fmt.Sprintf("%s.%s", defaultDB, targetTable),
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge,
		}
		require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)),
			"ingest should succeed once _bruin_staging is pre-created and granted")

		var count int
		require.NoError(t, adminDB.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", defaultDB, targetTable)).Scan(&count))
		assert.Equal(t, replaceFixtureRows, count)
	})
}

// TestMSSQLDestination_RestrictedPermissions verifies the permission contract on
// MSSQL. Restricted user gets db_datareader/db_datawriter/db_ddladmin on dbo only,
// no ALTER ANY SCHEMA — so they cannot create the `_bruin_staging` schema.
func TestMSSQLDestination_RestrictedPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if mssqlDest.uri == "" {
		t.Skip("shared mssql dest container not available")
	}

	ctx := context.Background()
	sourceURI := jsonlURI(t, "testdata/conformance.jsonl")

	adminDB, err := sql.Open("sqlserver", mssqlConnString(mssqlDest.uri))
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()

	suffix := uniqueSuffix()
	user := fmt.Sprintf("gong_r_%s", suffix)
	password := "TestPass123!"
	targetTable := fmt.Sprintf("dbo.restricted_target_%s", suffix)

	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("IF OBJECT_ID('%s', 'U') IS NOT NULL DROP TABLE %s",
			strings.ReplaceAll(targetTable, "'", "''"), quoteTableMSSQL(targetTable)))
		_, _ = adminDB.ExecContext(ctx,
			"IF EXISTS (SELECT * FROM sys.schemas WHERE name = '_bruin_staging') EXEC('DROP SCHEMA [_bruin_staging]')")
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("IF EXISTS (SELECT * FROM sys.database_principals WHERE name = '%s') DROP USER [%s]", user, user))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("IF EXISTS (SELECT * FROM sys.server_principals WHERE name = '%s') DROP LOGIN [%s]", user, user))
	})

	// Create restricted login + user on master db. Grant grants only inside dbo.
	// Crucially: NO permission to CREATE SCHEMA at the database level.
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE LOGIN [%s] WITH PASSWORD = '%s'", user, password))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE USER [%s] FOR LOGIN [%s]", user, user))
	require.NoError(t, err)
	for _, role := range []string{"db_datareader", "db_datawriter"} {
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER ROLE %s ADD MEMBER [%s]", role, user))
		require.NoError(t, err)
	}
	// Grant table creation in dbo only (not at the database level).
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("GRANT CREATE TABLE TO [%s]", user))
	require.NoError(t, err)
	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("GRANT ALTER ON SCHEMA::dbo TO [%s]", user))
	require.NoError(t, err)

	restrictedURI := mssqlURIWithUser(t, mssqlDest.uri, user, password)

	t.Run("fails_without_staging_schema_grant", func(t *testing.T) {
		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test",
			DestURI:             restrictedURI,
			DestTable:           targetTable,
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge,
		}
		err := runPipeline(t, ctx, pipeline.New(cfg))
		require.Error(t, err, "ingest should fail when restricted user can't create _bruin_staging")
		errMsg := strings.ToLower(err.Error())
		assert.True(t,
			strings.Contains(errMsg, "permission") || strings.Contains(errMsg, "denied") ||
				strings.Contains(errMsg, "not authorized") || strings.Contains(errMsg, "cannot"),
			"error should clearly indicate a permission issue, got: %s", err.Error())
	})

	t.Run("succeeds_when_admin_pre_creates_staging", func(t *testing.T) {
		_, err := adminDB.ExecContext(ctx,
			"IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '_bruin_staging') EXEC('CREATE SCHEMA [_bruin_staging]')")
		require.NoError(t, err)
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("GRANT CONTROL ON SCHEMA::[_bruin_staging] TO [%s]", user))
		require.NoError(t, err)

		cfg := &config.IngestConfig{
			SourceURI:           sourceURI,
			SourceTable:         "restricted_test_2",
			DestURI:             restrictedURI,
			DestTable:           targetTable,
			PrimaryKeys:         []string{"id"},
			IncrementalStrategy: config.StrategyMerge,
		}
		require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)),
			"ingest should succeed once _bruin_staging is pre-created and granted")

		var count int
		require.NoError(t, adminDB.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteTableMSSQL(targetTable))).Scan(&count))
		assert.Equal(t, replaceFixtureRows, count)
	})
}

// postgresURIWithUser returns a copy of the given Postgres URI with credentials
// swapped to the supplied user/password.
func postgresURIWithUser(t *testing.T, baseURI, user, password string) string {
	t.Helper()
	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.User = url.UserPassword(user, password)
	return u.String()
}

// mysqlURIWithUser returns a copy of the given MySQL URI with credentials swapped.
func mysqlURIWithUser(t *testing.T, baseURI, user, password string) string {
	t.Helper()
	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.User = url.UserPassword(user, password)
	return u.String()
}

// mssqlURIWithUser returns a copy of the given MSSQL URI with credentials swapped.
func mssqlURIWithUser(t *testing.T, baseURI, user, password string) string {
	t.Helper()
	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.User = url.UserPassword(user, password)
	return u.String()
}
