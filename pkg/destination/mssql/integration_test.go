//go:build integration

package mssql_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	_ "github.com/bruin-data/ingestr/internal/testutil/quiettc"
	"github.com/bruin-data/ingestr/pkg/destination"
	mssqldest "github.com/bruin-data/ingestr/pkg/destination/mssql"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/mysql"
	_ "github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmssql "github.com/testcontainers/testcontainers-go/modules/mssql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mssqlImage       = "mcr.microsoft.com/mssql/server:2022-latest"
	mssqlPassword    = "TestPassword123!"
	mssqlPasswordEnc = "TestPassword123%21"
)

var (
	sharedMSSQLURI     string
	sharedMSSQLVersion string
	suffixCounter      atomic.Uint64
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	if !testutil.DockerProviderHealthy(ctx) {
		_, _ = os.Stderr.WriteString("skipping mssql destination integration tests: Docker provider is not available/healthy\n")
		os.Exit(0)
	}

	container, uri, err := startMSSQLContainer(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start MSSQL container (%s): %v\n", mssqlImage, err)
		os.Exit(1)
	}
	sharedMSSQLURI = uri

	db, err := sql.Open("sqlserver", mssqlConnString(uri))
	if err == nil {
		_ = db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&sharedMSSQLVersion)
		_ = db.Close()
		_, _ = fmt.Fprintf(os.Stderr, "MSSQL container ready (%s): %s\n", mssqlImage, strings.ReplaceAll(sharedMSSQLVersion, "\n", " "))
	}

	code := m.Run()
	_ = container.Terminate(context.Background())
	os.Exit(code)
}

func startMSSQLContainer(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := tcmssql.Run(
		ctx,
		mssqlImage,
		tcmssql.WithAcceptEULA(),
		tcmssql.WithPassword(mssqlPassword),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("1433/tcp"),
				wait.ForLog("Recovery is complete."),
				wait.ForSQL("1433/tcp", "sqlserver", func(host string, port network.Port) string {
					return fmt.Sprintf("server=%s;user id=sa;password=%s;port=%s;database=master;encrypt=disable", host, mssqlPassword, port.Port())
				}),
			).WithDeadline(3*time.Minute),
		),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "1433")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("mssql://sa:%s@%s:%s/master?encrypt=disable", mssqlPasswordEnc, host, port.Port())
	return container, uri, nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), suffixCounter.Add(1))
}

func mssqlConnString(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		database = "master"
	}
	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   u.Host,
		User:   url.UserPassword(user, pass),
	}
	q := u.Query()
	q.Set("database", database)
	connURL.RawQuery = q.Encode()
	return connURL.String()
}

func mssqlURIWithDB(t *testing.T, baseURI, database string) string {
	t.Helper()
	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	u.Path = "/" + database
	return u.String()
}

func openMSSQLDB(t *testing.T, uri string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlserver", mssqlConnString(uri))
	require.NoError(t, err)
	require.NoError(t, db.PingContext(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createTestDatabase(t *testing.T, ctx context.Context, adminDB *sql.DB, dbName string, compatLevel int, collation string) string {
	t.Helper()
	quoted := quoteIdent(dbName)
	createSQL := fmt.Sprintf("CREATE DATABASE %s", quoted)
	if collation != "" {
		createSQL += " COLLATE " + collation
	}
	_, err := adminDB.ExecContext(ctx, createSQL)
	require.NoError(t, err)
	if compatLevel > 0 {
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s SET COMPATIBILITY_LEVEL = %d", quoted, compatLevel))
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		literal := strings.ReplaceAll(dbName, "'", "''")
		_, _ = adminDB.ExecContext(cleanupCtx, fmt.Sprintf(`IF DB_ID(N'%s') IS NOT NULL
BEGIN
	ALTER DATABASE %s SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
	DROP DATABASE %s;
END`, literal, quoted, quoted))
	})
	return mssqlURIWithDB(t, sharedMSSQLURI, dbName)
}

func quoteIdent(ident string) string {
	return "[" + strings.ReplaceAll(ident, "]", "]]") + "]"
}

func quoteQualified(name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = quoteIdent(part)
	}
	return strings.Join(parts, ".")
}

func TestMSSQLIntegration_ManagedCDCRunLease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	adminDB := openMSSQLDB(t, sharedMSSQLURI)
	dbURI := createTestDatabase(t, ctx, adminDB, "cdc_lease_"+uniqueSuffix(), 0, "")

	dest1 := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest1.Connect(ctx, dbURI))
	t.Cleanup(func() { _ = dest1.Close(context.Background()) })

	dest2 := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest2.Connect(ctx, dbURI))
	t.Cleanup(func() { _ = dest2.Close(context.Background()) })

	_, err := dest1.AcquireManagedCDCRunLease(ctx, "")
	require.ErrorContains(t, err, "empty")

	// Connector ID longer than 255 characters exercises SHA-256 resource hashing for sp_getapplock.
	connectorA := "connector-a-" + strings.Repeat("x", 300) + "-" + uniqueSuffix()
	connectorB := "connector-b-" + uniqueSuffix()

	leaseA, err := dest1.AcquireManagedCDCRunLease(ctx, connectorA)
	require.NoError(t, err)

	_, err = dest2.AcquireManagedCDCRunLease(ctx, connectorA)
	require.ErrorContains(t, err, "already owns connector")

	leaseB, err := dest2.AcquireManagedCDCRunLease(ctx, connectorB)
	require.NoError(t, err)
	require.NoError(t, leaseB.Release())

	// Wait long enough for at least one 1s APPLOCK_MODE heartbeat tick.
	time.Sleep(1500 * time.Millisecond)
	select {
	case <-leaseA.Done():
		t.Fatalf("leaseA closed unexpectedly while held: %v", leaseA.Err())
	default:
	}
	require.NoError(t, leaseA.Err())

	require.NoError(t, leaseA.Release())
	require.NoError(t, leaseA.Release())
	select {
	case <-leaseA.Done():
	case <-time.After(time.Second):
		t.Fatal("leaseA.Done() was not closed after Release")
	}
	require.NoError(t, leaseA.Err())

	leaseA2, err := dest2.AcquireManagedCDCRunLease(ctx, connectorA)
	require.NoError(t, err)

	// Locate the SQL Server session holding the application lock and kill it to verify ErrConnectorLeaseLost.
	var lockSPID int
	err = adminDB.QueryRowContext(ctx, `
		SELECT TOP (1) request_session_id
		FROM sys.dm_tran_locks
		WHERE resource_type = 'APPLICATION'
		  AND resource_description LIKE '%ingestr_cdc_%'
		  AND request_session_id <> @@SPID
		ORDER BY request_session_id DESC`).Scan(&lockSPID)
	require.NoError(t, err)
	require.Greater(t, lockSPID, 0)

	_, err = adminDB.ExecContext(ctx, fmt.Sprintf("KILL %d", lockSPID))
	require.NoError(t, err)

	select {
	case <-leaseA2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for killed lease session to report ErrConnectorLeaseLost")
	}
	require.ErrorIs(t, leaseA2.Err(), source.ErrConnectorLeaseLost)
	_ = leaseA2.Release()
}

func TestMSSQLIntegration_TransactionalFencingAndIncarnations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	adminDB := openMSSQLDB(t, sharedMSSQLURI)
	dbURI := createTestDatabase(t, ctx, adminDB, "cdc_fence_"+uniqueSuffix(), 0, "")
	db := openMSSQLDB(t, dbURI)

	dest := mssqldest.NewMSSQLDestination()
	require.NoError(t, dest.Connect(ctx, dbURI))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	t.Run("managed_cdc_state_and_position_migration", func(t *testing.T) {
		stateTable := "_bruin_staging.cdc_state_test"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: stateTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "event_id", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
				{Name: "state_version", DataType: schema.TypeString, MaxLength: 16, Nullable: false},
				{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
				{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
				{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
				{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
				{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
				{Name: "state_status", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
				{Name: "_cdc_lsn", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
				{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
			}},
			PrimaryKeys: []string{"connector_id", "event_id"},
		}))

		var maxLen int
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT c.max_length FROM sys.columns AS c WHERE c.object_id = OBJECT_ID(@p1) AND c.name = '_cdc_lsn'`,
			stateTable).Scan(&maxLen))
		require.Equal(t, 256, maxLen)

		require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, stateTable))
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT c.max_length FROM sys.columns AS c WHERE c.object_id = OBJECT_ID(@p1) AND c.name = '_cdc_lsn'`,
			stateTable).Scan(&maxLen))
		require.Equal(t, -1, maxLen)

		_, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s
			([event_id], [state_version], [connector_id], [source_table], [destination_table], [state_kind], [state_generation], [state_status], [_cdc_lsn], [recorded_at])
			VALUES
			(N'run-1', N'2', N'conn-1', N'public.t1', N'dbo.t1', N'run', 1, N'complete', N'00000001/00000010', SYSUTCDATETIME()),
			(N'ckpt-1', N'2', N'conn-1', N'public.t1', N'dbo.t1', N'checkpoint', 1, N'complete', N'00000001/00000020', SYSUTCDATETIME()),
			(N'run-2a', N'2', N'conn-1', N'public.t1', N'dbo.t1', N'run', 2, N'in_progress', N'00000001/00000030', SYSUTCDATETIME()),
			(N'run-2b', N'2', N'conn-1', N'public.t1', N'dbo.t1', N'run', 2, N'in_progress', N'00000001/00000040', SYSUTCDATETIME()),
			(N'other', N'2', N'conn-2', N'public.t2', N'dbo.t2', N'run', 1, N'complete', N'00000001/00000050', SYSUTCDATETIME())`,
			quoteQualified(stateTable)))
		require.NoError(t, err)

		entries, err := dest.LoadCDCState(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Len(t, entries, 4)

		fence, err := dest.LoadCDCStateFence(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Equal(t, int64(2), fence.Generation)
		require.Equal(t, []string{"run-2a", "run-2b"}, fence.RunEventIDs)

		// Exercise >1000-row chunked DeleteCDCStateEvents alongside real rows.
		toDelete := make([]string, 0, 1005)
		toDelete = append(toDelete, "run-1", "ckpt-1")
		for i := 0; i < 1003; i++ {
			toDelete = append(toDelete, fmt.Sprintf("missing-%d", i))
		}
		require.NoError(t, dest.DeleteCDCStateEvents(ctx, stateTable, "conn-1", toDelete))

		entries, err = dest.LoadCDCState(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Len(t, entries, 2)
		otherEntries, err := dest.LoadCDCState(ctx, stateTable, "conn-2")
		require.NoError(t, err)
		require.Len(t, otherEntries, 1)
	})

	t.Run("claim_and_prepare_empty_target_and_concurrent_race", func(t *testing.T) {
		claimTable := "_bruin_staging.cdc_targets_test"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: claimTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "destination_table", DataType: schema.TypeString, MaxLength: 2048, Nullable: false},
				{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
				{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
			}},
			PrimaryKeys: []string{"destination_table"},
		}))

		targetSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, Nullable: false},
			{Name: "payload", DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ, Nullable: true},
		}}

		target := "dbo.claimed_target"
		inc, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: target,
			ConnectorID:      "conn-owner",
			SourceTable:      "public.orders",
		}, destination.PrepareOptions{
			Table:       target,
			Schema:      targetSchema,
			PrimaryKeys: []string{"id"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, inc)

		actualInc, exists, err := dest.CDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, inc, actualInc)

		// Idempotent ClaimCDCTarget for same owner, rejected for different owner.
		require.NoError(t, dest.ClaimCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: "DBO.CLAIMED_TARGET",
			ConnectorID:      "conn-owner",
			SourceTable:      "public.orders",
		}))
		err = dest.ClaimCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: target,
			ConnectorID:      "conn-other",
			SourceTable:      "public.orders",
		})
		require.ErrorContains(t, err, "already claimed")

		// Pre-existing unclaimed table must cause ClaimAndPrepareEmptyCDCTarget to roll back the claim.
		existingTarget := "dbo.preexisting_unclaimed"
		_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s ([id] BIGINT PRIMARY KEY, [val] NVARCHAR(50))", quoteQualified(existingTarget)))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s VALUES (1, N'keep')", quoteQualified(existingTarget)))
		require.NoError(t, err)

		_, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: existingTarget,
			ConnectorID:      "conn-late",
			SourceTable:      "public.preexisting",
		}, destination.PrepareOptions{
			Table:       existingTarget,
			Schema:      targetSchema,
			PrimaryKeys: []string{"id"},
		})
		require.ErrorContains(t, err, "exclusively create")

		canonicalExisting, err := dest.CanonicalCDCTarget(ctx, existingTarget)
		require.NoError(t, err)
		var claimCount int
		require.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [destination_table] = @p1", quoteQualified(claimTable)),
			canonicalExisting).Scan(&claimCount))
		require.Equal(t, 0, claimCount, "failed ClaimAndPrepareEmptyCDCTarget must roll back the claim row")

		// Concurrent race on an absent table: exactly one connector wins.
		raceTarget := "dbo.raced_target"
		destOther := mssqldest.NewMSSQLDestination()
		require.NoError(t, destOther.Connect(ctx, dbURI))
		t.Cleanup(func() { _ = destOther.Close(context.Background()) })

		start := make(chan struct{})
		type raceResult struct {
			conn string
			err  error
		}
		results := make(chan raceResult, 2)
		var wg sync.WaitGroup
		for _, tc := range []struct {
			d    *mssqldest.MSSQLDestination
			conn string
		}{
			{dest, "conn-racer-1"},
			{destOther, "conn-racer-2"},
		} {
			wg.Add(1)
			go func(d *mssqldest.MSSQLDestination, conn string) {
				defer wg.Done()
				<-start
				_, err := d.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
					DestinationTable: raceTarget,
					ConnectorID:      conn,
					SourceTable:      "public.raced",
				}, destination.PrepareOptions{
					Table:       raceTarget,
					Schema:      targetSchema,
					PrimaryKeys: []string{"id"},
				})
				results <- raceResult{conn: conn, err: err}
			}(tc.d, tc.conn)
		}
		close(start)
		wg.Wait()
		close(results)
		var successes, failures int
		for r := range results {
			if r.err == nil {
				successes++
			} else {
				failures++
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, failures)
	})

	t.Run("merge_atomic_truncate_swap_and_schema_evolution_fencing", func(t *testing.T) {
		createCDCTable := func(name string) {
			_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
				[id] BIGINT NOT NULL PRIMARY KEY,
				[payload] NVARCHAR(255) NULL,
				[_cdc_lsn] NVARCHAR(128) NULL,
				[_cdc_deleted] BIT NULL,
				[_cdc_synced_at] DATETIME2(6) NULL
			)`, quoteQualified(name)))
			require.NoError(t, err)
		}
		createStagingTable := func(name string) {
			_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
				[id] BIGINT NOT NULL,
				[payload] NVARCHAR(255) NULL,
				[_cdc_lsn] NVARCHAR(128) NULL,
				[_cdc_deleted] BIT NULL,
				[_cdc_synced_at] DATETIME2(6) NULL,
				[_cdc_unchanged_cols] NVARCHAR(MAX) NULL
			)`, quoteQualified(name)))
			require.NoError(t, err)
		}

		// Ensure _bruin_staging exists.
		_, err := db.ExecContext(ctx, "IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = '_bruin_staging') EXEC('CREATE SCHEMA [_bruin_staging]')")
		require.NoError(t, err)

		targetA := "dbo.fenced_a"
		stageA := "_bruin_staging.fenced_a_stg"
		targetB := "dbo.fenced_b"
		stageB := "_bruin_staging.fenced_b_stg"
		createCDCTable(targetA)
		createStagingTable(stageA)
		createCDCTable(targetB)
		createStagingTable(stageB)

		incA, exists, err := dest.CDCTargetIncarnation(ctx, targetA)
		require.NoError(t, err)
		require.True(t, exists)
		incB, exists, err := dest.CDCTargetIncarnation(ctx, targetB)
		require.NoError(t, err)
		require.True(t, exists)

		cols := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}

		// Conditional soft & hard merge succeeds with matching incarnation.
		_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (1, N'v1', N'000010', 0, '2026-01-01', N'[]')`, quoteQualified(stageA)))
		require.NoError(t, err)
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            targetA,
			StagingTable:           stageA,
			PrimaryKeys:            []string{"id"},
			Columns:                cols,
			CDCExpectedIncarnation: incA,
		}))

		// Conditional hard delete merge commits with matching incarnation.
		_, err = db.ExecContext(ctx, fmt.Sprintf(`TRUNCATE TABLE %s; INSERT INTO %s VALUES (2, N'v2', N'000020', 0, '2026-01-02', N'[]')`, quoteQualified(stageA), quoteQualified(stageA)))
		require.NoError(t, err)
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            targetA,
			StagingTable:           stageA,
			PrimaryKeys:            []string{"id"},
			Columns:                cols,
			CDCHardDelete:          true,
			CDCExpectedIncarnation: incA,
		}))

		// Drop and recreate targetA -> incarnation must change and fence old MergeTable and TruncateCDCTableIfIncarnation.
		_, err = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", quoteQualified(targetA)))
		require.NoError(t, err)
		createCDCTable(targetA)
		_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (99, N'recreated', N'000001', 0, '2026-01-01')`, quoteQualified(targetA)))
		require.NoError(t, err)

		incA2, exists, err := dest.CDCTargetIncarnation(ctx, targetA)
		require.NoError(t, err)
		require.True(t, exists)
		require.NotEqual(t, incA, incA2)

		for _, hardDelete := range []bool{false, true} {
			err = dest.MergeTable(ctx, destination.MergeOptions{
				TargetTable:            targetA,
				StagingTable:           stageA,
				PrimaryKeys:            []string{"id"},
				Columns:                cols,
				CDCHardDelete:          hardDelete,
				CDCExpectedIncarnation: incA,
			})
			require.ErrorContains(t, err, "physical incarnation changed")
		}

		err = dest.TruncateCDCTableIfIncarnation(ctx, targetA, incA)
		require.ErrorContains(t, err, "physical incarnation changed")
		var countA int
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteQualified(targetA))).Scan(&countA))
		require.Equal(t, 1, countA, "fenced truncate must not empty recreated table")

		// Atomic multi-table merge: if targetB fails incarnation check, targetA truncate must roll back!
		_, err = db.ExecContext(ctx, fmt.Sprintf(`TRUNCATE TABLE %s; INSERT INTO %s VALUES (10, N'new-a', N'000030', 0, '2026-01-03', N'[]')`, quoteQualified(stageA), quoteQualified(stageA)))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, fmt.Sprintf(`TRUNCATE TABLE %s; INSERT INTO %s VALUES (20, N'new-b', N'000030', 0, '2026-01-03', N'[]')`, quoteQualified(stageB), quoteQualified(stageB)))
		require.NoError(t, err)

		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            targetA,
					StagingTable:           stageA,
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: incA2,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            targetB,
					StagingTable:           stageB,
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: incB + "-stale",
				},
			},
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		var keptPayload string
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 99", quoteQualified(targetA))).Scan(&keptPayload))
		require.Equal(t, "recreated", keptPayload, "atomic multi-table rollback must preserve targetA")

		// With matching incarnations, MergeCDCTablesAtomically commits both atomically.
		require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            targetA,
					StagingTable:           stageA,
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: incA2,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            targetB,
					StagingTable:           stageB,
					PrimaryKeys:            []string{"id"},
					Columns:                cols,
					CDCExpectedIncarnation: incB,
				},
			},
		}))
		var valA, valB string
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 10", quoteQualified(targetA))).Scan(&valA))
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 20", quoteQualified(targetB))).Scan(&valB))
		require.Equal(t, "new-a", valA)
		require.Equal(t, "new-b", valB)

		// TruncateCDCTableIfIncarnation succeeds when incarnation matches.
		require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, targetB, incB))
		var countB int
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteQualified(targetB))).Scan(&countB))
		require.Equal(t, 0, countB)

		// Conditional schema evolution succeeds on match and aborts on mismatch.
		comp := &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "note",
				NewColumn:  schema.Column{Name: "note", DataType: schema.TypeString, Nullable: true},
			}},
		}
		_, _, err = dest.ApplySchemaEvolutionIfIncarnation(ctx, targetA, comp, incA)
		require.ErrorContains(t, err, "physical incarnation changed")
		_, evolvedInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, targetA, comp, incA2)
		require.NoError(t, err)
		require.Equal(t, incA2, evolvedInc)

		// Conditional SwapTable across schemas (_bruin_staging -> dbo).
		swapStage := "_bruin_staging.swap_stage"
		createCDCTable(swapStage)
		_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES (77, N'swapped', N'000077', 0, '2026-01-07')`, quoteQualified(swapStage)))
		require.NoError(t, err)

		stagingInc, resultInc, err := dest.CDCConditionalSwapIncarnations(ctx, targetB, swapStage)
		require.NoError(t, err)
		require.Equal(t, stagingInc, resultInc)

		_, _, err = dest.CDCConditionalSwapIncarnations(ctx, "otherdb.dbo.target", swapStage)
		require.ErrorContains(t, err, "across catalogs")

		// Stale target incarnation aborts SwapTable without touching swapStage or targetB.
		err = dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   targetB,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        incB + "-stale",
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  resultInc,
		})
		require.ErrorContains(t, err, "physical incarnation changed")

		// Matching incarnations succeed and targetB assumes resultInc.
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   targetB,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        incB,
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  resultInc,
		}))
		afterSwapInc, exists, err := dest.CDCTargetIncarnation(ctx, targetB)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, resultInc, afterSwapInc)

		var swappedPayload string
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 77", quoteQualified(targetB))).Scan(&swappedPayload))
		require.Equal(t, "swapped", swappedPayload)
	})

	t.Run("quoted_dotted_table_names", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = 'sales.v1') EXEC('CREATE SCHEMA [sales.v1]')")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = '_bruin_staging') EXEC('CREATE SCHEMA [_bruin_staging]')")
		require.NoError(t, err)

		claimTable := "_bruin_staging.[cdc.claims]"
		_, err = db.ExecContext(ctx, `CREATE TABLE [_bruin_staging].[cdc.claims] (
			[destination_table] NVARCHAR(450) NOT NULL PRIMARY KEY,
			[connector_id] NVARCHAR(450) NOT NULL,
			[claimed_at] DATETIME2(6) NOT NULL
		)`)
		require.NoError(t, err)

		dottedTarget := "[sales.v1].[order.events]"
		dottedStage := "_bruin_staging.[order.events_stg]"
		targetSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: false, IsPrimaryKey: true},
				{Name: "payload", DataType: schema.TypeString, Nullable: true},
				{Name: destination.CDCLSNColumn, DataType: schema.TypeString, Nullable: true},
				{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean, Nullable: true},
				{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestamp, Nullable: true},
			},
			PrimaryKeys: []string{"id"},
		}

		inc, err := dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: dottedTarget,
			ConnectorID:      "conn-dotted",
			SourceTable:      "public.order_events",
		}, destination.PrepareOptions{
			Table:       dottedTarget,
			Schema:      targetSchema,
			PrimaryKeys: []string{"id"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, inc)

		readInc, exists, err := dest.CDCTargetIncarnation(ctx, `"sales.v1"."order.events"`)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, inc, readInc)

		_, err = db.ExecContext(ctx, `CREATE TABLE [_bruin_staging].[order.events_stg] (
			[id] BIGINT NOT NULL,
			[payload] NVARCHAR(MAX) NULL,
			[_cdc_lsn] NVARCHAR(MAX) NULL,
			[_cdc_deleted] BIT NULL,
			[_cdc_synced_at] DATETIME2(6) NULL,
			[_cdc_unchanged_cols] NVARCHAR(MAX) NULL
		)`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO [_bruin_staging].[order.events_stg] VALUES (1, N'dotted-v1', N'000010', 0, '2026-01-01', N'[]')`)
		require.NoError(t, err)

		cols := []string{"id", "payload", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn}
		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            dottedTarget,
			StagingTable:           dottedStage,
			PrimaryKeys:            []string{"id"},
			Columns:                cols,
			CDCExpectedIncarnation: inc,
		}))

		require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{{
			Truncate: true,
			Options: destination.MergeOptions{
				TargetTable:            dottedTarget,
				StagingTable:           dottedStage,
				PrimaryKeys:            []string{"id"},
				Columns:                cols,
				CDCHardDelete:          true,
				CDCExpectedIncarnation: inc,
			},
		}}))

		require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, dottedTarget, inc))

		comp := &schemaevolution.SchemaComparison{
			HasChanges: true,
			Changes: []schemaevolution.SchemaChange{{
				Type:       schemaevolution.ChangeAddColumn,
				ColumnName: "note",
				NewColumn:  schema.Column{Name: "note", DataType: schema.TypeString, Nullable: true},
			}},
		}
		_, evolvedInc, err := dest.ApplySchemaEvolutionIfIncarnation(ctx, `"sales.v1"."order.events"`, comp, inc)
		require.NoError(t, err)
		require.Equal(t, inc, evolvedInc)

		swapStage := "_bruin_staging.[order.events_swap]"
		_, err = db.ExecContext(ctx, `CREATE TABLE [_bruin_staging].[order.events_swap] (
			[id] BIGINT NOT NULL,
			[payload] NVARCHAR(MAX) NULL,
			[_cdc_lsn] NVARCHAR(MAX) NULL,
			[_cdc_deleted] BIT NULL,
			[_cdc_synced_at] DATETIME2(6) NULL,
			[note] NVARCHAR(MAX) NULL
		)`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO [_bruin_staging].[order.events_swap] VALUES (7, N'swapped-dotted', N'000077', 0, '2026-01-07', N'ok')`)
		require.NoError(t, err)

		stagingInc, resultInc, err := dest.CDCConditionalSwapIncarnations(ctx, dottedTarget, swapStage)
		require.NoError(t, err)
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   dottedTarget,
			StagingTable:                  swapStage,
			PrimaryKeys:                   []string{"id"},
			CDCExpectedIncarnation:        evolvedInc,
			CDCExpectedStagingIncarnation: stagingInc,
			CDCExpectedResultIncarnation:  resultInc,
		}))

		afterSwapInc, exists, err := dest.CDCTargetIncarnation(ctx, dottedTarget)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, resultInc, afterSwapInc)

		var gotPayload string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT [payload] FROM [sales.v1].[order.events] WHERE [id] = 7`).Scan(&gotPayload))
		require.Equal(t, "swapped-dotted", gotPayload)
	})
}

func TestMSSQLIntegration_CompatibilityLevels_Below130AndAbove130(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	adminDB := openMSSQLDB(t, sharedMSSQLURI)

	for _, tc := range []struct {
		name        string
		compatLevel int
		collation   string
		expectJSON  bool
	}{
		{name: "compat_110_ci", compatLevel: 110, collation: "SQL_Latin1_General_CP1_CI_AS", expectJSON: false},
		{name: "compat_120_cs", compatLevel: 120, collation: "Latin1_General_100_CS_AS", expectJSON: false},
		{name: "compat_160_cs", compatLevel: 160, collation: "Latin1_General_100_CS_AS", expectJSON: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbName := "cdc_" + tc.name + "_" + uniqueSuffix()
			dbURI := createTestDatabase(t, ctx, adminDB, dbName, tc.compatLevel, tc.collation)
			db := openMSSQLDB(t, dbURI)

			dest := mssqldest.NewMSSQLDestination()
			require.NoError(t, dest.Connect(ctx, dbURI))
			t.Cleanup(func() { _ = dest.Close(context.Background()) })

			require.NoError(t, dest.ValidateManagedCDCState())
			require.NoError(t, dest.ValidateManagedCDCTarget(ctx, "dbo.items"))

			// Confirm OPENJSON fails natively on compat < 130 so our test genuinely exercises the CHARINDEX fallback.
			var openJSONErr error
			var cnt int
			openJSONErr = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM OPENJSON(N'["a"]')`).Scan(&cnt)
			if tc.expectJSON {
				require.NoError(t, openJSONErr)
			} else {
				require.Error(t, openJSONErr)
			}

			// Also verify cross-database ValidateManagedCDCTarget from master connection.
			masterDest := mssqldest.NewMSSQLDestination()
			require.NoError(t, masterDest.Connect(ctx, sharedMSSQLURI))
			t.Cleanup(func() { _ = masterDest.Close(context.Background()) })
			require.NoError(t, masterDest.ValidateManagedCDCTarget(ctx, dbName+".dbo.items"))

			// Verify managed CDC state table works under this compatibility level and collation.
			stateTable := "_bruin_staging.cdc_state"
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table: stateTable,
				Schema: &schema.TableSchema{Columns: []schema.Column{
					{Name: "event_id", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
					{Name: "state_version", DataType: schema.TypeString, MaxLength: 16, Nullable: false},
					{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
					{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
					{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
					{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
					{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
					{Name: "state_status", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
					{Name: "_cdc_lsn", DataType: schema.TypeString, Nullable: false},
					{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
				}},
				PrimaryKeys: []string{"connector_id", "event_id"},
			}))
			_, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
				(N'e1', N'2', N'conn-compat', N'public.t', N'dbo.items', N'run', 1, N'complete', N'000010', SYSUTCDATETIME())`,
				quoteQualified(stateTable)))
			require.NoError(t, err)
			entries, err := dest.LoadCDCState(ctx, stateTable, "conn-compat")
			require.NoError(t, err)
			require.Len(t, entries, 1)
			fence, err := dest.LoadCDCStateFence(ctx, stateTable, "conn-compat")
			require.NoError(t, err)
			require.Equal(t, []string{"e1"}, fence.RunEventIDs)

			// Test _cdc_unchanged_cols in both soft-delete and hard-delete merges,
			// including case-distinct columns (Foo vs foo on CS collations) and prefix columns (payload vs payload_extra).
			secondCaseCol := "foo"
			if !strings.Contains(tc.collation, "_CS_") {
				secondCaseCol = "bar"
			}
			for _, hardDelete := range []bool{false, true} {
				mode := "soft"
				if hardDelete {
					mode = "hard"
				}
				target := "dbo.unchanged_" + mode
				staging := "_bruin_staging.unchanged_" + mode + "_stg"
				_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
					[id] BIGINT NOT NULL PRIMARY KEY,
					[payload] NVARCHAR(255) NULL,
					[payload_extra] NVARCHAR(255) NULL,
					[Foo] NVARCHAR(255) NULL,
					[%s] NVARCHAR(255) NULL,
					[_cdc_lsn] NVARCHAR(128) NULL,
					[_cdc_deleted] BIT NULL,
					[_cdc_synced_at] DATETIME2(6) NULL
				)`, quoteQualified(target), secondCaseCol))
				require.NoError(t, err)
				_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
					[id] BIGINT NOT NULL,
					[payload] NVARCHAR(255) NULL,
					[payload_extra] NVARCHAR(255) NULL,
					[Foo] NVARCHAR(255) NULL,
					[%s] NVARCHAR(255) NULL,
					[_cdc_lsn] NVARCHAR(128) NULL,
					[_cdc_deleted] BIT NULL,
					[_cdc_synced_at] DATETIME2(6) NULL,
					[_cdc_unchanged_cols] NVARCHAR(MAX) NULL
				)`, quoteQualified(staging), secondCaseCol))
				require.NoError(t, err)

				_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
					(1, N'p1-orig', N'pe1-orig', N'F1-orig', N'f1-orig', N'000010', 0, '2026-01-01'),
					(2, N'p2-orig', N'pe2-orig', N'F2-orig', N'f2-orig', N'000010', 0, '2026-01-01'),
					(3, N'p3-orig', N'pe3-orig', N'F3-orig', N'f3-orig', N'000010', 0, '2026-01-01')`, quoteQualified(target)))
				require.NoError(t, err)

				_, err = db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s VALUES
					(1, N'p1-ignored', N'pe1-new', N'F1-ignored', N'f1-new', N'000020', 0, '2026-01-02', N'["payload","Foo"]'),
					(2, N'p2-new', N'pe2-ignored', N'F2-new', N'f2-new', N'000020', 0, '2026-01-02', N'["payload_extra"]'),
					(3, NULL, NULL, NULL, NULL, N'000020', 1, '2026-01-02', NULL),
					(4, N'p4-new', N'pe4-new', N'F4-new', N'f4-new', N'000020', 0, '2026-01-02', NULL)`, quoteQualified(staging)))
				require.NoError(t, err)

				inc, exists, err := dest.CDCTargetIncarnation(ctx, target)
				require.NoError(t, err)
				require.True(t, exists)

				require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
					TargetTable:            target,
					StagingTable:           staging,
					PrimaryKeys:            []string{"id"},
					Columns:                []string{"id", "payload", "payload_extra", "Foo", secondCaseCol, destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
					CDCHardDelete:          hardDelete,
					CDCExpectedIncarnation: inc,
				}))

				var p, pe, capF, lowF string
				require.NoError(t, db.QueryRowContext(ctx,
					fmt.Sprintf("SELECT [payload], [payload_extra], [Foo], [%s] FROM %s WHERE [id] = 1", secondCaseCol, quoteQualified(target))).
					Scan(&p, &pe, &capF, &lowF))
				require.Equal(t, "p1-orig", p, "payload was marked unchanged")
				require.Equal(t, "pe1-new", pe, "payload_extra must update")
				require.Equal(t, "F1-orig", capF, "Foo was marked unchanged")
				require.Equal(t, "f1-new", lowF, "second column must update")

				require.NoError(t, db.QueryRowContext(ctx,
					fmt.Sprintf("SELECT [payload], [payload_extra] FROM %s WHERE [id] = 2", quoteQualified(target))).
					Scan(&p, &pe))
				require.Equal(t, "p2-new", p, "payload must NOT match payload_extra")
				require.Equal(t, "pe2-orig", pe, "payload_extra was marked unchanged")

				var row3Count int
				require.NoError(t, db.QueryRowContext(ctx,
					fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 3", quoteQualified(target))).Scan(&row3Count))
				if hardDelete {
					require.Equal(t, 0, row3Count, "hard delete must remove id=3")
				} else {
					require.Equal(t, 1, row3Count)
					var del bool
					require.NoError(t, db.QueryRowContext(ctx,
						fmt.Sprintf("SELECT [_cdc_deleted], [payload] FROM %s WHERE [id] = 3", quoteQualified(target))).
						Scan(&del, &p))
					require.True(t, del)
					require.Equal(t, "p3-orig", p)
				}

				require.NoError(t, db.QueryRowContext(ctx,
					fmt.Sprintf("SELECT [payload] FROM %s WHERE [id] = 4", quoteQualified(target))).Scan(&p))
				require.Equal(t, "p4-new", p)
			}
		})
	}
}

func TestMSSQLIntegration_EndToEnd_PostgresCDC_SoftAndHardDeletes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	adminDB := openMSSQLDB(t, sharedMSSQLURI)

	pgContainer, pgConnStr, err := startPostgresCDCContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	pgPool, err := pgxpool.New(ctx, pgConnStr)
	require.NoError(t, err)
	t.Cleanup(pgPool.Close)

	_, err = pgPool.Exec(ctx, `
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	for _, hardDelete := range []bool{false, true} {
		mode := "soft_delete"
		behavior := config.CDCDeleteSoft
		if hardDelete {
			mode = "hard_delete"
			behavior = config.CDCDeleteHard
		}
		t.Run(mode, func(t *testing.T) {
			sourceTable := "public.orders_" + mode
			pubName := "orders_pub_" + mode
			_, err = pgPool.Exec(ctx, fmt.Sprintf(`
				CREATE TABLE %s (
					id INTEGER PRIMARY KEY,
					name VARCHAR(100) NOT NULL,
					value INTEGER NOT NULL,
					note TEXT NOT NULL
				);
				ALTER TABLE %s ALTER COLUMN note SET STORAGE EXTERNAL;
				INSERT INTO %s (id, name, value, note) VALUES
					(1, 'alpha', 100, repeat('a', 4000)),
					(2, 'beta', 200, repeat('b', 4000)),
					(3, 'gamma', 300, repeat('g', 4000));
				CREATE PUBLICATION %s FOR TABLE %s;
			`, sourceTable, sourceTable, sourceTable, pubName, sourceTable))
			require.NoError(t, err)

			dbURI := createTestDatabase(t, ctx, adminDB, "pg_cdc_"+mode+"_"+uniqueSuffix(), 0, "")
			db := openMSSQLDB(t, dbURI)
			slotName := "mssql_" + mode + "_" + uniqueSuffix()
			srcURI := "postgres+cdc://" + pgConnStr[len("postgres://"):] + "&publication=" + pubName + "&slot=" + slotName
			destTable := "dbo.orders_dest"

			run := func() {
				t.Helper()
				require.NoError(t, pipeline.New(&config.IngestConfig{
					SourceURI:           srcURI,
					DestURI:             dbURI,
					SourceTable:         sourceTable,
					DestTable:           destTable,
					IncrementalStrategy: config.StrategyMerge,
					CDCDeleteBehavior:   behavior,
				}).Run(ctx))
			}

			// Initial snapshot.
			run()
			var count int
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteQualified(destTable))).Scan(&count))
			require.Equal(t, 3, count)

			// Incremental update (touching value but NOT TOASTed note), insert, and delete.
			_, err = pgPool.Exec(ctx, fmt.Sprintf(`
				UPDATE %s SET value = 150 WHERE id = 1;
				DELETE FROM %s WHERE id = 2;
				INSERT INTO %s (id, name, value, note) VALUES (4, 'delta', 400, repeat('d', 4000));
			`, sourceTable, sourceTable, sourceTable))
			require.NoError(t, err)
			run()

			var val int
			var note string
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [value], [note] FROM %s WHERE [id] = 1", quoteQualified(destTable))).Scan(&val, &note))
			require.Equal(t, 150, val)
			require.Equal(t, strings.Repeat("a", 4000), note, "TOASTed unchanged note must be preserved")

			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [value] FROM %s WHERE [id] = 4", quoteQualified(destTable))).Scan(&val))
			require.Equal(t, 400, val)

			if hardDelete {
				var id2Count, tombstones int
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 2", quoteQualified(destTable))).Scan(&id2Count))
				require.Equal(t, 0, id2Count)
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [_cdc_deleted] = 1", quoteQualified(destTable))).Scan(&tombstones))
				require.Equal(t, 0, tombstones)
			} else {
				var del bool
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [_cdc_deleted] FROM %s WHERE [id] = 2", quoteQualified(destTable))).Scan(&del))
				require.True(t, del)
			}

			// Trailing delete of id=3 followed by no-op resume and re-insert.
			_, err = pgPool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = 3`, sourceTable))
			require.NoError(t, err)
			run()
			run()
			if hardDelete {
				var id3Count int
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 3", quoteQualified(destTable))).Scan(&id3Count))
				require.Equal(t, 0, id3Count)
			}

			_, err = pgPool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, name, value, note) VALUES (3, 'gamma-reborn', 333, 'short')`, sourceTable))
			require.NoError(t, err)
			run()
			var name string
			var del bool
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [name], [value], [_cdc_deleted] FROM %s WHERE [id] = 3", quoteQualified(destTable))).Scan(&name, &val, &del))
			require.Equal(t, "gamma-reborn", name)
			require.Equal(t, 333, val)
			require.False(t, del)
		})
	}
}

func TestMSSQLIntegration_EndToEnd_MySQLCDC_SoftAndHardDeletes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := t.Context()
	adminDB := openMSSQLDB(t, sharedMSSQLURI)

	mysqlContainer, mysqlURI, err := startMySQLCDCContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mysqlContainer.Terminate(context.Background()) })

	mysqlDB, err := sql.Open("mysql", mysqlDSN(mysqlURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mysqlDB.Close() })

	for _, hardDelete := range []bool{false, true} {
		mode := "soft_delete"
		behavior := config.CDCDeleteSoft
		if hardDelete {
			mode = "hard_delete"
			behavior = config.CDCDeleteHard
		}
		t.Run(mode, func(t *testing.T) {
			sourceTable := "items_" + mode
			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
				id INT NOT NULL PRIMARY KEY,
				name VARCHAR(100) NOT NULL,
				value INT NULL
			)`, sourceTable))
			require.NoError(t, err)
			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, name, value) VALUES (1, 'one', 10), (2, 'two', 20), (3, 'three', 30)`, sourceTable))
			require.NoError(t, err)

			dbURI := createTestDatabase(t, ctx, adminDB, "mysql_cdc_"+mode+"_"+uniqueSuffix(), 0, "")
			db := openMSSQLDB(t, dbURI)

			destTable := "dbo.items_dest"
			srcURI := mysqlCDCURI(mysqlURI, map[string]string{
				"mode": "batch",
			})
			run := func() {
				t.Helper()
				require.NoError(t, pipeline.New(&config.IngestConfig{
					SourceURI:           srcURI,
					DestURI:             dbURI,
					SourceTable:         sourceTable,
					DestTable:           destTable,
					IncrementalStrategy: config.StrategyMerge,
					CDCDeleteBehavior:   behavior,
				}).Run(ctx))
			}

			// Initial snapshot.
			run()
			var count int
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteQualified(destTable))).Scan(&count))
			require.Equal(t, 3, count)

			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET value = 11 WHERE id = 1`, sourceTable))
			require.NoError(t, err)
			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = 2`, sourceTable))
			require.NoError(t, err)
			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, name, value) VALUES (4, 'four', 40)`, sourceTable))
			require.NoError(t, err)
			run()

			var val int
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [value] FROM %s WHERE [id] = 1", quoteQualified(destTable))).Scan(&val))
			require.Equal(t, 11, val)
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [value] FROM %s WHERE [id] = 4", quoteQualified(destTable))).Scan(&val))
			require.Equal(t, 40, val)

			if hardDelete {
				var id2Count, tombstones int
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [id] = 2", quoteQualified(destTable))).Scan(&id2Count))
				require.Equal(t, 0, id2Count)
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE [_cdc_deleted] = 1", quoteQualified(destTable))).Scan(&tombstones))
				require.Equal(t, 0, tombstones)
			} else {
				var del bool
				require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [_cdc_deleted] FROM %s WHERE [id] = 2", quoteQualified(destTable))).Scan(&del))
				require.True(t, del)
			}

			// Trailing delete + idempotent re-run + re-insert.
			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = 3`, sourceTable))
			require.NoError(t, err)
			run()
			run()

			_, err = mysqlDB.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, name, value) VALUES (3, 'three-reborn', 33)`, sourceTable))
			require.NoError(t, err)
			run()
			var name string
			var del bool
			require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT [name], [value], [_cdc_deleted] FROM %s WHERE [id] = 3", quoteQualified(destTable))).Scan(&name, &val, &del))
			require.Equal(t, "three-reborn", name)
			require.Equal(t, 33, val)
			require.False(t, del)
		})
	}
}

func startPostgresCDCContainer(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER":     "testuser",
				"POSTGRES_PASSWORD": "testpass",
				"POSTGRES_DB":       "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			Cmd: []string{
				"postgres",
				"-c", "wal_level=logical",
				"-c", "max_replication_slots=6",
				"-c", "max_wal_senders=6",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", err
	}
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	return container, fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port()), nil
}

func startMySQLCDCContainer(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := tcmysql.Run(
		ctx,
		"mysql:8.0",
		tcmysql.WithDatabase("testdb"),
		tcmysql.WithUsername("root"),
		tcmysql.WithPassword("testpass"),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.Cmd = []string{
				"--server-id=18888",
				"--log-bin=mysql-bin",
				"--gtid-mode=ON",
				"--enforce-gtid-consistency=ON",
				"--binlog-format=ROW",
				"--binlog-row-image=FULL",
			}
			return nil
		}),
	)
	if err != nil {
		return nil, "", err
	}
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	return container, fmt.Sprintf("mysql://root:testpass@%s:%s/testdb", host, port.Port()), nil
}

func mysqlCDCURI(baseURI string, params map[string]string) string {
	u, err := url.Parse(baseURI)
	if err != nil {
		return baseURI
	}
	u.Scheme = "mysql+cdc"
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func mysqlDSN(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	db := strings.TrimPrefix(u.Path, "/")
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", user, pass, u.Host, db)
}
