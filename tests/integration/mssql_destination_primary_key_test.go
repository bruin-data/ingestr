//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	mssqldest "github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestMSSQLDestinationCDCPrepareRequiresMatchingPrimaryKey(t *testing.T) {
	if mssqlDest.uri == "" {
		t.Skip("shared SQL Server destination container not available")
	}

	ctx := t.Context()
	suffix := uniqueSuffix()
	dest := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest.Connect(ctx, mssqlDest.uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db := openMSSQLTestDB(t, mssqlDest.uri)
	t.Cleanup(func() { _ = db.Close() })

	freshTable := "dbo.cdc_pk_fresh_" + suffix
	matchingTable := "dbo.cdc_pk_matching_" + suffix
	missingTable := "dbo.cdc_pk_missing_" + suffix
	mismatchedTable := "dbo.cdc_pk_mismatched_" + suffix
	uniqueTable := "dbo.cdc_pk_unique_" + suffix
	disabledTable := "dbo.cdc_pk_disabled_" + suffix
	concurrentTable := "dbo.cdc_pk_concurrent_" + suffix
	disabledConstraint := "PK_cdc_disabled_" + suffix
	caseSensitiveDB := "cdc_pk_cs_" + suffix
	accentInsensitiveDB := "cdc_pk_ai_" + suffix
	linkedServer := "cdc_pk_link_" + suffix
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = db.ExecContext(cleanupCtx, "EXEC master.dbo.sp_dropserver @server = @p1, @droplogins = 'droplogins'", linkedServer)
		for _, table := range []string{freshTable, matchingTable, missingTable, mismatchedTable, uniqueTable, disabledTable, concurrentTable} {
			_, _ = db.ExecContext(cleanupCtx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteMSSQLPrimaryKeyTestName(table)))
		}
		for _, database := range []string{caseSensitiveDB, accentInsensitiveDB} {
			dropMSSQLPrimaryKeyTestDatabase(cleanupCtx, db, database)
		}
	})

	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "part", DataType: schema.TypeString, MaxLength: 64},
		{Name: "payload", DataType: schema.TypeString, Nullable: true},
	}}
	prepare := func(table string, requestedSchema *schema.TableSchema, keys []string, requireMatch bool) error {
		return dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:                  table,
			Schema:                 requestedSchema,
			PrimaryKeys:            keys,
			CDCMode:                requireMatch,
			CDCKeys:                keys,
			RequirePrimaryKeyMatch: requireMatch,
		})
	}

	require.NoError(t, prepare(freshTable, tableSchema, []string{"id", "part"}, true))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		[ID] BIGINT NOT NULL,
		[part] NVARCHAR(64) NOT NULL,
		[payload] NVARCHAR(255) NULL,
		PRIMARY KEY ([part], [ID])
	)`, quoteMSSQLPrimaryKeyTestName(matchingTable))))
	require.NoError(t, prepare(matchingTable, tableSchema, []string{"id", "PART"}, true))

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		[id] BIGINT NOT NULL,
		[part] NVARCHAR(64) NULL,
		[payload] NVARCHAR(255) NULL
	)`, quoteMSSQLPrimaryKeyTestName(missingTable))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("INSERT INTO %s VALUES (1, N'a', N'keep')", quoteMSSQLPrimaryKeyTestName(missingTable))))
	err := prepare(missingTable, tableSchema, []string{"id"}, true)
	require.ErrorContains(t, err, "must have enabled primary key [id]; found []")
	var payload string
	require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 1", quoteMSSQLPrimaryKeyTestName(missingTable))).Scan(&payload))
	require.Equal(t, "keep", payload)
	require.NoError(t, prepare(missingTable, tableSchema, []string{"id"}, false), "unguarded preparation must preserve existing behavior")

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		[id] BIGINT NOT NULL,
		[part] NVARCHAR(64) NULL,
		[payload] NVARCHAR(255) NOT NULL PRIMARY KEY
	)`, quoteMSSQLPrimaryKeyTestName(mismatchedTable))))
	err = prepare(mismatchedTable, tableSchema, []string{"id"}, true)
	require.ErrorContains(t, err, "found [payload]")

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		[id] BIGINT NOT NULL UNIQUE,
		[part] NVARCHAR(64) NULL,
		[payload] NVARCHAR(255) NULL
	)`, quoteMSSQLPrimaryKeyTestName(uniqueTable))))
	err = prepare(uniqueTable, tableSchema, []string{"id"}, true)
	require.ErrorContains(t, err, "found []", "a unique constraint is not a physical primary key")

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s (
		[id] BIGINT NOT NULL,
		[part] NVARCHAR(64) NULL,
		[payload] NVARCHAR(255) NULL,
		CONSTRAINT %s PRIMARY KEY ([id])
	)`, quoteMSSQLPrimaryKeyTestName(disabledTable), quoteMSSQLPrimaryKeyTestName(disabledConstraint))))
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("ALTER INDEX %s ON %s DISABLE", quoteMSSQLPrimaryKeyTestName(disabledConstraint), quoteMSSQLPrimaryKeyTestName(disabledTable))))
	err = prepare(disabledTable, tableSchema, []string{"id"}, true)
	require.ErrorContains(t, err, "found []", "a disabled primary-key index is not enforced")

	type prepareResult struct {
		keys []string
		err  error
	}
	start := make(chan struct{})
	results := make(chan prepareResult, 2)
	for _, keys := range [][]string{{"id"}, {"part"}} {
		keys := keys
		go func() {
			<-start
			results <- prepareResult{keys: keys, err: prepare(concurrentTable, tableSchema, keys, true)}
		}()
	}
	close(start)
	first, second := <-results, <-results
	close(results)
	var winner, loser prepareResult
	if first.err == nil {
		winner, loser = first, second
	} else {
		winner, loser = second, first
	}
	require.NoError(t, winner.err)
	require.ErrorContains(t, loser.err, "must have enabled primary key", "the concurrent-create loser must validate the winner")
	require.NotEqual(t, winner.keys, loser.keys)

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s COLLATE Latin1_General_100_CS_AS", quoteMSSQLPrimaryKeyTestName(caseSensitiveDB))))
	caseTable := caseSensitiveDB + ".dbo.case_keys"
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE %s ([ID] BIGINT NOT NULL PRIMARY KEY)", quoteMSSQLPrimaryKeyTestName(caseTable))))
	lowerIDSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	err = prepare(caseTable, lowerIDSchema, []string{"id"}, true)
	require.ErrorContains(t, err, "found [ID]", "case-sensitive catalog identifiers must remain distinct")
	upperIDSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "ID", DataType: schema.TypeInt64}}}
	require.NoError(t, prepare(caseTable, upperIDSchema, []string{"ID"}, true))

	_, err = db.ExecContext(ctx, `
		EXEC master.dbo.sp_addlinkedserver
			@server = @p1,
			@srvproduct = N'',
			@provider = N'MSOLEDBSQL',
			@datasrc = N'localhost',
			@provstr = N'Encrypt=No';
		EXEC master.dbo.sp_addlinkedsrvlogin
			@rmtsrvname = @p1,
			@useself = N'False',
			@locallogin = NULL,
			@rmtuser = N'sa',
			@rmtpassword = @p2;
		EXEC master.dbo.sp_serveroption @server = @p1, @optname = N'rpc out', @optvalue = N'true';
		EXEC master.dbo.sp_serveroption @server = @p1, @optname = N'use remote collation', @optvalue = N'false';
	`, linkedServer, mssqlPassword)
	require.NoError(t, err)
	linkedCaseTable := linkedServer + "." + caseTable
	err = dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:                  linkedCaseTable,
		PrimaryKeys:            []string{"id"},
		CDCMode:                true,
		CDCKeys:                []string{"id"},
		RequirePrimaryKeyMatch: true,
	})
	require.ErrorContains(t, err, "found [ID]", "linked target comparisons must execute under the remote catalog collation")
	require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
		Table:                  linkedCaseTable,
		PrimaryKeys:            []string{"ID"},
		CDCMode:                true,
		CDCKeys:                []string{"ID"},
		RequirePrimaryKeyMatch: true,
	}))

	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s COLLATE Latin1_General_100_CI_AI", quoteMSSQLPrimaryKeyTestName(accentInsensitiveDB))))
	accentTable := accentInsensitiveDB + ".dbo.accent_keys"
	require.NoError(t, dest.Exec(ctx, fmt.Sprintf("CREATE TABLE %s ([Résumé] BIGINT NOT NULL PRIMARY KEY)", quoteMSSQLPrimaryKeyTestName(accentTable))))
	accentSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "resume", DataType: schema.TypeInt64}}}
	require.NoError(t, prepare(accentTable, accentSchema, []string{"resume"}, true), "catalog collation must control identifier matching")
}

func dropMSSQLPrimaryKeyTestDatabase(ctx context.Context, db *sql.DB, database string) {
	literal := strings.ReplaceAll(database, "'", "''")
	quoted := quoteMSSQLPrimaryKeyTestName(database)
	_, _ = db.ExecContext(ctx, fmt.Sprintf(`IF DB_ID(N'%s') IS NOT NULL
	BEGIN
		ALTER DATABASE %s SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
		DROP DATABASE %s;
	END`, literal, quoted, quoted))
}

func quoteMSSQLPrimaryKeyTestName(name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = "[" + strings.ReplaceAll(part, "]", "]]") + "]"
	}
	return strings.Join(parts, ".")
}
