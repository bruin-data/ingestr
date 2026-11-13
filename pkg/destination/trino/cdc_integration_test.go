//go:build integration

package trino

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/bruin-data/ingestr/internal/testutil/quiettc"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startTrinoIcebergForCDC(t *testing.T, ctx context.Context, defaultSchema string) (*TrinoDestination, string) {
	t.Helper()

	testNetwork, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testNetwork.Remove(context.Background()) })

	nessie, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          "ghcr.io/projectnessie/nessie:0.104.5",
			ExposedPorts:   []string{"19120/tcp"},
			Networks:       []string{testNetwork.Name},
			NetworkAliases: map[string][]string{testNetwork.Name: {"nessie"}},
			Env:            map[string]string{"NESSIE_VERSION_STORE_TYPE": "IN_MEMORY"},
			WaitingFor:     wait.ForHTTP("/api/v2/config").WithPort("19120/tcp").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = nessie.Terminate(context.Background()) })

	trinoContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "trinodb/trino:483",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{testNetwork.Name},
			Files: []testcontainers.ContainerFile{{
				Reader: strings.NewReader(`connector.name=iceberg
iceberg.catalog.type=nessie
iceberg.nessie-catalog.uri=http://nessie:19120/api/v2
iceberg.nessie-catalog.default-warehouse-dir=local:///warehouse
fs.local.enabled=true
local.location=/tmp
`),
				ContainerFilePath: "/etc/trino/catalog/iceberg.properties",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForLog("======== SERVER STARTED ========").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = trinoContainer.Terminate(context.Background()) })

	host, err := trinoContainer.Host(ctx)
	require.NoError(t, err)
	port, err := trinoContainer.MappedPort(ctx, "8080/tcp")
	require.NoError(t, err)

	uri := fmt.Sprintf("trino://test@%s:%s/iceberg/%s", host, port.Port(), defaultSchema)
	dest := NewTrinoDestination()
	require.NoError(t, dest.Connect(ctx, uri))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })

	require.NoError(t, dest.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS iceberg."+quoteIdentifier(defaultSchema)))
	return dest, uri
}

func TestTrinoCDCIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Trino CDC integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	dest, uri := startTrinoIcebergForCDC(t, ctx, "cdc_it")

	t.Run("managed CDC state", func(t *testing.T) {
		require.NoError(t, dest.ValidateManagedCDCState())
		require.Equal(t, "iceberg", dest.ManagedCDCStateCatalog())
		require.True(t, dest.RequiresSerializedCDCRuns())
		require.Equal(t, 1000, dest.CDCStatePruneBatchSize())

		stateTable := "_bruin_staging._ingestr_cdc_state"
		stateSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "event_id", DataType: schema.TypeString, MaxLength: 128},
			{Name: "state_version", DataType: schema.TypeString, MaxLength: 16},
			{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64},
			{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000},
			{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000},
			{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32},
			{Name: "state_generation", DataType: schema.TypeInt64},
			{Name: "state_status", DataType: schema.TypeString, MaxLength: 32},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: "recorded_at", DataType: schema.TypeTimestampTZ},
		}}
		require.NoError(t, dest.PrepareCDCStateTable(ctx, stateTable, stateSchema))
		require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, stateTable))

		// Empty state & fence before any inserts.
		entries, err := dest.LoadCDCState(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Empty(t, entries)
		fence, err := dest.LoadCDCStateFence(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Zero(t, fence.Generation)
		require.Empty(t, fence.RunEventIDs)

		// Iceberg maps bounded VARCHAR(255) to unbounded varchar, so EnsureCDCStatePositionColumn accepts it,
		// while non-VARCHAR types (e.g. BIGINT) must be rejected.
		narrowTable := "_bruin_staging.narrow_pos"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: narrowTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: destination.CDCLSNColumn, DataType: schema.TypeString, MaxLength: 255},
			}},
		}))
		require.NoError(t, dest.EnsureCDCStatePositionColumn(ctx, narrowTable))

		intTable := "_bruin_staging.int_pos"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: intTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: destination.CDCLSNColumn, DataType: schema.TypeInt64},
			}},
		}))
		require.ErrorContains(t, dest.EnsureCDCStatePositionColumn(ctx, intTable), "must be VARCHAR")

		// Insert duplicate events, multiple generations, and a second connector.
		insertStateSQL := `INSERT INTO "iceberg"."_bruin_staging"."_ingestr_cdc_state"
			("event_id", "state_version", "connector_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn", "recorded_at")
			VALUES
			('ev-r1', '2', 'conn-1', 'public.orders', 'cdc_it.orders', 'run', 1, 'started', '00000001/00000010', CURRENT_TIMESTAMP),
			('ev-r2b', '2', 'conn-1', 'public.orders', 'cdc_it.orders', 'run', 2, 'started', '00000001/00000020', CURRENT_TIMESTAMP),
			('ev-r2a', '2', 'conn-1', 'public.orders', 'cdc_it.orders', 'run', 2, 'started', '00000001/00000020', CURRENT_TIMESTAMP),
			('ev-r2a', '2', 'conn-1', 'public.orders', 'cdc_it.orders', 'run', 2, 'started', '00000001/00000020', CURRENT_TIMESTAMP),
			('ev-cp', '2', 'conn-1', 'public.orders', 'cdc_it.orders', 'checkpoint', 2, 'completed', '00000001/00000050', CURRENT_TIMESTAMP),
			('ev-other', '2', 'conn-2', 'public.users', 'cdc_it.users', 'run', 9, 'started', '00000009/00000099', CURRENT_TIMESTAMP)`
		require.NoError(t, dest.Exec(ctx, insertStateSQL))

		entries, err = dest.LoadCDCState(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Len(t, entries, 4)

		fence, err = dest.LoadCDCStateFence(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Equal(t, int64(2), fence.Generation)
		require.Equal(t, []string{"ev-r2a", "ev-r2b"}, fence.RunEventIDs)

		// Delete events for conn-1 without affecting conn-2 or remaining conn-1 rows.
		require.NoError(t, dest.DeleteCDCStateEvents(ctx, stateTable, "conn-1", []string{"ev-r1", "ev-r2a"}))
		entries, err = dest.LoadCDCState(ctx, stateTable, "conn-1")
		require.NoError(t, err)
		require.Len(t, entries, 2)
		otherEntries, err := dest.LoadCDCState(ctx, stateTable, "conn-2")
		require.NoError(t, err)
		require.Len(t, otherEntries, 1)
	})

	t.Run("target claiming, incarnation comments, GetMaxCDCLSN, and staging listing", func(t *testing.T) {
		claimTable := "_bruin_staging._ingestr_cdc_targets"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table: claimTable,
			Schema: &schema.TableSchema{Columns: []schema.Column{
				{Name: "destination_table", DataType: schema.TypeString, MaxLength: 2048},
				{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64},
				{Name: "claimed_at", DataType: schema.TypeTimestampTZ},
			}},
		}))

		// Missing target checks.
		inc, exists, err := dest.CDCTargetIncarnation(ctx, "cdc_it.missing_target")
		require.NoError(t, err)
		require.False(t, exists)
		require.Empty(t, inc)

		maxLSN, err := dest.GetMaxCDCLSN(ctx, "cdc_it.missing_target")
		require.NoError(t, err)
		require.Empty(t, maxLSN)

		// Non-CDC table checks.
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "cdc_it.non_cdc_table",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		}))
		inc, exists, err = dest.CDCTargetIncarnation(ctx, "cdc_it.non_cdc_table")
		require.NoError(t, err)
		require.True(t, exists)
		require.Empty(t, inc)
		_, exists, err = dest.EnsureCDCTargetIncarnation(ctx, "cdc_it.non_cdc_table")
		require.True(t, exists)
		require.ErrorContains(t, err, "missing managed column")
		maxLSN, err = dest.GetMaxCDCLSN(ctx, "cdc_it.non_cdc_table")
		require.NoError(t, err)
		require.Empty(t, maxLSN)

		// Concurrent ClaimCDCTarget: exactly 1 connector wins, idempotent for winner, rejected for others.
		results := make([]error, 6)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = dest.ClaimCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
					DestinationTable: "cdc_it.raced_target",
					ConnectorID:      fmt.Sprintf("conn-%d", i),
					SourceTable:      "public.orders",
				})
			}()
		}
		wg.Wait()
		winners := 0
		for i, claimErr := range results {
			if claimErr == nil {
				winners++
				require.NoError(t, dest.ClaimCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
					DestinationTable: "cdc_it.raced_target",
					ConnectorID:      fmt.Sprintf("conn-%d", i),
					SourceTable:      "public.orders",
				}))
			} else {
				require.ErrorContains(t, claimErr, "already claimed")
			}
		}
		require.Equal(t, 1, winners)

		// ClaimAndPrepareEmptyCDCTarget creates target with incarnation comment on _cdc_lsn.
		cdcCols := []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "name", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}
		claim := destination.CDCTargetClaim{
			DestinationTable: "cdc_it.claimed_orders",
			ConnectorID:      "conn-alpha",
			SourceTable:      "public.claimed_orders",
		}
		inc, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, claim, destination.PrepareOptions{
			Table:  "cdc_it.claimed_orders",
			Schema: &schema.TableSchema{Columns: cdcCols},
		})
		require.NoError(t, err)
		require.NotEmpty(t, inc)

		readInc, exists, err := dest.CDCTargetIncarnation(ctx, "cdc_it.claimed_orders")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, inc, readInc)

		// Existing target cannot be re-claimed by another connector or recreated.
		_, err = dest.ClaimAndPrepareEmptyCDCTarget(ctx, claimTable, claim, destination.PrepareOptions{
			Table:  "cdc_it.claimed_orders",
			Schema: &schema.TableSchema{Columns: cdcCols},
		})
		require.ErrorContains(t, err, "already exists")
		require.ErrorContains(t, dest.ClaimCDCTarget(ctx, claimTable, destination.CDCTargetClaim{
			DestinationTable: "cdc_it.claimed_orders",
			ConnectorID:      "conn-beta",
			SourceTable:      "public.claimed_orders",
		}), "already claimed")

		// EnsureCDCTargetIncarnation stamps COMMENT ON COLUMN on an existing CDC table.
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "cdc_it.manual_cdc_target",
			Schema: &schema.TableSchema{Columns: cdcCols},
		}))
		inc0, exists, err := dest.CDCTargetIncarnation(ctx, "cdc_it.manual_cdc_target")
		require.NoError(t, err)
		require.True(t, exists)
		require.Empty(t, inc0)

		stampedInc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "cdc_it.manual_cdc_target")
		require.NoError(t, err)
		require.True(t, exists)
		require.NotEmpty(t, stampedInc)

		repeatInc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, "cdc_it.manual_cdc_target")
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, stampedInc, repeatInc)

		// GetMaxCDCLSN on empty then populated table.
		maxLSN, err = dest.GetMaxCDCLSN(ctx, "cdc_it.manual_cdc_target")
		require.NoError(t, err)
		require.Empty(t, maxLSN)

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."manual_cdc_target" VALUES
			(1, 'a', '00000001/00000100', false, CURRENT_TIMESTAMP),
			(2, 'b', '00000002/00000250', true, CURRENT_TIMESTAMP)`))
		maxLSN, err = dest.GetMaxCDCLSN(ctx, "cdc_it.manual_cdc_target")
		require.NoError(t, err)
		require.Equal(t, "00000002/00000250", maxLSN)

		// ListStagingTables prefix filter.
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "_bruin_staging.orders_stream_a",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "_bruin_staging.orders_stream_b",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "_bruin_staging.unrelated_tbl",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:  "_bruin_staging.__ingestr_cdc_bak_hidden",
			Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
		}))
		staged, err := dest.ListStagingTables(ctx, "_bruin_staging", "orders_stream_")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"orders_stream_a", "orders_stream_b"}, staged)
		allStaged, err := dest.ListStagingTables(ctx, "_bruin_staging", "")
		require.NoError(t, err)
		require.NotContains(t, allStaged, "__ingestr_cdc_bak_hidden")
		_ = dest.DropTable(ctx, "_bruin_staging.__ingestr_cdc_bak_hidden")
	})

	t.Run("run lease CTAS, heartbeat, contention, stale takeover, loss fencing, and release", func(t *testing.T) {
		// Default timings acquire, contention, and clean release.
		lease, err := dest.AcquireManagedCDCRunLease(ctx, "trino-default-lease-conn")
		require.NoError(t, err)

		contendCtx, contendCancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = dest.AcquireManagedCDCRunLease(contendCtx, "trino-default-lease-conn")
		contendCancel()
		require.Error(t, err)

		require.NoError(t, lease.Release())

		// Short timings: heartbeat keeps lease alive past TTL.
		shortLease, err := dest.acquireManagedCDCRunLeaseWithTimings(
			ctx,
			"trino-short-lease-conn",
			2*time.Second,
			400*time.Millisecond,
			200*time.Millisecond,
			200*time.Millisecond,
		)
		require.NoError(t, err)
		select {
		case <-shortLease.Done():
			t.Fatalf("lease lost unexpectedly during heartbeat window: %v", shortLease.Err())
		case <-time.After(3 * time.Second):
		}
		require.NoError(t, shortLease.Err())
		require.NoError(t, shortLease.Release())

		// Stale lease takeover: seed an expired lease table and verify takeover succeeds.
		expiredLease, err := dest.acquireManagedCDCRunLeaseWithTimings(
			ctx,
			"trino-stale-lease-conn",
			2*time.Second,
			400*time.Millisecond,
			200*time.Millisecond,
			200*time.Millisecond,
		)
		require.NoError(t, err)
		// Forcefully leak it by replacing its table with a stale timestamp > TTL (2s).
		typedLease := expiredLease.(*trinoCDCRunLease)
		require.NoError(t, expiredLease.Release())
		require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE %s AS SELECT CAST('dead-owner' AS VARCHAR) AS "owner", CURRENT_TIMESTAMP - INTERVAL '10' SECOND AS "heartbeat"`,
			typedLease.fqn(),
		)))

		takenOver, err := dest.acquireManagedCDCRunLeaseWithTimings(
			ctx,
			"trino-stale-lease-conn",
			2*time.Second,
			400*time.Millisecond,
			200*time.Millisecond,
			200*time.Millisecond,
		)
		require.NoError(t, err)
		require.NoError(t, takenOver.Release())

		// External takeover triggers ErrConnectorLeaseLost and blocks fenced destination operations.
		watched, err := dest.acquireManagedCDCRunLeaseWithTimings(
			ctx,
			"trino-loss-lease-conn",
			10*time.Second,
			3*time.Second,
			200*time.Millisecond,
			200*time.Millisecond,
		)
		require.NoError(t, err)
		watchedFQN := watched.(*trinoCDCRunLease).fqn()
		require.NoError(t, dest.Exec(ctx, "DROP TABLE "+watchedFQN))

		select {
		case <-watched.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for lease loss notification after external drop")
		}
		require.ErrorIs(t, watched.Err(), source.ErrConnectorLeaseLost)

		guard := source.NewConnectorLeaseGuard(watched)
		guardCtx := source.WithConnectorLeaseGuard(ctx, guard)
		require.ErrorIs(t, source.ConnectorLeaseLoss(guardCtx), source.ErrConnectorLeaseLost)
		err = dest.MergeTable(guardCtx, destination.MergeOptions{
			TargetTable:  "cdc_it.claimed_orders",
			StagingTable: "_bruin_staging.orders_stream_a",
			PrimaryKeys:  []string{"id"},
			Columns:      []string{"id", "name", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
		})
		require.ErrorIs(t, err, source.ErrConnectorLeaseLost)
		_ = watched.Release()
	})

	t.Run("CDC MergeTable soft delete, hard delete, _cdc_unchanged_cols, and predicates", func(t *testing.T) {
		cols := []schema.Column{
			{Name: "tenant", DataType: schema.TypeInt64},
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString},
			{Name: "RankCol", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}
		stageCols := append(slicesClone(cols), schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString})

		target := "cdc_it.merge_soft_target"
		stage := "_bruin_staging.merge_soft_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: target, Schema: &schema.TableSchema{Columns: cols}}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: stage, Schema: &schema.TableSchema{Columns: stageCols}}))
		inc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, target)
		require.NoError(t, err)
		require.True(t, exists)

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."merge_soft_target" VALUES
			(1, 1, 'current', 'r0', '030', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 2, 'deleted', 'r0', '030', true, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 3, 'legacy', 'r0', NULL, false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 4, 'keep', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 5, 'old', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 6, 'clear', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 7, 'old', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 8, 'before', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 9, 'original', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC')`))

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."merge_soft_stage" VALUES
			(1, 1, 'stale', 'rx', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 1, NULL, NULL, '025', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 2, 'resurrect', 'r1', '040', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 3, 'first', 'r1', '010', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 4, NULL, NULL, '010', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 5, 'new', 'r2', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 5, NULL, 'r3', '030', false, TIMESTAMP '2026-01-03 00:00:00.000000 UTC', '["PAYLOAD"]'),
			(1, 6, NULL, 'r2', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 6, NULL, 'r3', '030', false, TIMESTAMP '2026-01-03 00:00:00.000000 UTC', '["payload"]'),
			(1, 7, 'updated-then-deleted', 'r2', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 7, NULL, NULL, '030', true, TIMESTAMP '2026-01-03 00:00:00.000000 UTC', '[]'),
			(1, 8, 'same-lsn-update', 'r2', '010', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 8, NULL, NULL, '010', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 9, 'same-lsn-replay', 'r2', '010', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(2, 1, 'new-row', 'r1', '010', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(2, 2, NULL, NULL, '010', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]')`))

		mergeOpts := destination.MergeOptions{
			TargetTable:            target,
			StagingTable:           stage,
			PrimaryKeys:            []string{"tenant", "id"},
			Columns:                []string{"tenant", "id", "payload", "RankCol", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
			CDCExpectedIncarnation: inc,
		}
		require.NoError(t, dest.MergeTable(ctx, mergeOpts))

		got := readMergeRows(t, ctx, dest.db, `"iceberg"."cdc_it"."merge_soft_target"`)
		require.Equal(t, []string{
			"1/1:current:r0:030:false",
			"1/2:resurrect:r1:040:false",
			"1/3:first:r1:010:false",
			"1/4:keep:r0:010:true",
			"1/5:new:r3:030:false",
			"1/6:<null>:r3:030:false",
			"1/7:updated-then-deleted:r2:030:true",
			"1/8:same-lsn-update:r2:010:true",
			"1/9:original:r0:010:false",
			"2/1:new-row:r1:010:false",
			"2/2:<null>:<null>:010:true",
		}, got)

		// Verify incarnation mismatch rejects merge.
		badOpts := mergeOpts
		badOpts.CDCExpectedIncarnation = inc + "-mismatch"
		require.ErrorContains(t, dest.MergeTable(ctx, badOpts), "physical incarnation changed")

		// Hard delete mode with _cdc_unchanged_cols and IncrementalPredicate.
		hardTarget := "cdc_it.merge_hard_target"
		hardStage := "_bruin_staging.merge_hard_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: hardTarget, Schema: &schema.TableSchema{Columns: cols}}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: hardStage, Schema: &schema.TableSchema{Columns: stageCols}}))
		hardInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, hardTarget)
		require.NoError(t, err)

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."merge_hard_target" VALUES
			(1, 1, 'keep-me', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 2, 'delete-me', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(1, 3, 'stale-delete-survivor', 'r0', '050', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC'),
			(2, 1, 'outside-pred', 'r0', '010', false, TIMESTAMP '2026-01-01 00:00:00.000000 UTC')`))

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."merge_hard_stage" VALUES
			(1, 1, NULL, 'r2', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '["payload"]'),
			(1, 2, NULL, NULL, '010', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 3, NULL, NULL, '040', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 4, 'inserted', 'r1', '020', false, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(1, 5, 'never-inserted', 'r1', '020', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]'),
			(2, 1, NULL, NULL, '090', true, TIMESTAMP '2026-01-02 00:00:00.000000 UTC', '[]')`))

		require.NoError(t, dest.MergeTable(ctx, destination.MergeOptions{
			TargetTable:            hardTarget,
			StagingTable:           hardStage,
			PrimaryKeys:            []string{"tenant", "id"},
			Columns:                []string{"tenant", "id", "payload", "RankCol", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn, destination.CDCUnchangedColsColumn},
			IncrementalPredicate:   `t."tenant" = 1`,
			CDCHardDelete:          true,
			CDCExpectedIncarnation: hardInc,
		}))

		gotHard := readMergeRows(t, ctx, dest.db, `"iceberg"."cdc_it"."merge_hard_target"`)
		require.Equal(t, []string{
			"1/1:keep-me:r2:020:false",
			"1/3:stale-delete-survivor:r0:050:false",
			"1/4:inserted:r1:020:false",
			"2/1:outside-pred:r0:010:false",
		}, gotHard)
	})

	t.Run("TruncateCDCTableIfIncarnation and conditional SwapTable", func(t *testing.T) {
		_, hasAtomicMulti := any(dest).(destination.CDCMultiTableAtomicMerger)
		require.True(t, hasAtomicMulti)

		cols := []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "val", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}

		// Conditional truncate preserves incarnation comment.
		truncTarget := "cdc_it.trunc_target"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: truncTarget, Schema: &schema.TableSchema{Columns: cols}}))
		truncInc, exists, err := dest.EnsureCDCTargetIncarnation(ctx, truncTarget)
		require.NoError(t, err)
		require.True(t, exists)
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."trunc_target" VALUES (1, 'v1', '010', false, CURRENT_TIMESTAMP)`))

		require.ErrorContains(t, dest.TruncateCDCTableIfIncarnation(ctx, truncTarget, truncInc+"-bad"), "physical incarnation changed")
		require.NoError(t, dest.TruncateCDCTableIfIncarnation(ctx, truncTarget, truncInc))

		var count int
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_it"."trunc_target"`).Scan(&count))
		require.Zero(t, count)
		afterTruncInc, _, err := dest.CDCTargetIncarnation(ctx, truncTarget)
		require.NoError(t, err)
		require.Equal(t, truncInc, afterTruncInc)

		// Conditional SwapTable across schemas (_bruin_staging -> cdc_it) preserves target incarnation in place.
		swapTarget := "cdc_it.swap_target"
		swapStage := "_bruin_staging.swap_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: swapTarget, Schema: &schema.TableSchema{Columns: cols}}))
		oldTargetInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, swapTarget)
		require.NoError(t, err)
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."swap_target" VALUES (1, 'stale', '001', false, CURRENT_TIMESTAMP)`))

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: swapStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."swap_stage" VALUES (2, 'fresh', '010', false, CURRENT_TIMESTAMP)`))

		stgInc, resInc, err := dest.CDCConditionalSwapIncarnations(ctx, swapTarget, swapStage)
		require.NoError(t, err)
		require.NotEmpty(t, stgInc)
		require.Equal(t, oldTargetInc, resInc)

		// Mismatched target incarnation must fail before swapping.
		require.ErrorContains(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   swapTarget,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        oldTargetInc + "-bad",
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  oldTargetInc + "-bad",
		}), "physical incarnation changed")

		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   swapTarget,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        oldTargetInc,
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  resInc,
		}))

		var swappedID int64
		var swappedVal string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT id, val FROM "iceberg"."cdc_it"."swap_target"`).Scan(&swappedID, &swappedVal))
		require.Equal(t, int64(2), swappedID)
		require.Equal(t, "fresh", swappedVal)
		actualResInc, _, err := dest.CDCTargetIncarnation(ctx, swapTarget)
		require.NoError(t, err)
		require.Equal(t, resInc, actualResInc)

		// Conditional SwapTable with an added column in staging evolves target additively and swaps atomically.
		evolvedCols := append(slicesClone(cols), schema.Column{Name: "tier", DataType: schema.TypeString})
		evolvedStage := "_bruin_staging.swap_stage_evolved"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: evolvedStage, Schema: &schema.TableSchema{Columns: evolvedCols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."swap_stage_evolved" VALUES (99, 'evolved', '020', false, CURRENT_TIMESTAMP, 'gold')`))

		evolvedStgInc, evolvedResInc, err := dest.CDCConditionalSwapIncarnations(ctx, swapTarget, evolvedStage)
		require.NoError(t, err)
		require.Equal(t, oldTargetInc, evolvedResInc)
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   swapTarget,
			StagingTable:                  evolvedStage,
			CDCExpectedIncarnation:        oldTargetInc,
			CDCExpectedStagingIncarnation: evolvedStgInc,
			CDCExpectedResultIncarnation:  evolvedResInc,
			Schema:                        &schema.TableSchema{Columns: evolvedCols},
		}))
		var swappedTier string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT id, val, tier FROM "iceberg"."cdc_it"."swap_target"`).Scan(&swappedID, &swappedVal, &swappedTier))
		require.Equal(t, int64(99), swappedID)
		require.Equal(t, "evolved", swappedVal)
		require.Equal(t, "gold", swappedTier)

		// Wider existing target columns cast narrower staging types without ALTER COLUMN narrowing failure.
		widerTarget := "cdc_it.swap_wider_target"
		narrowStage := "_bruin_staging.swap_narrow_stage"
		require.NoError(t, dest.Exec(ctx, `CREATE TABLE "iceberg"."cdc_it"."swap_wider_target" (
			id BIGINT,
			amt DECIMAL(38, 9),
			score DOUBLE,
			label VARCHAR,
			"_cdc_lsn" VARCHAR
		)`))
		widerInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, widerTarget)
		require.NoError(t, err)
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."swap_wider_target" VALUES (1, DECIMAL '1.500000000', 1.25, 'old', '001')`))

		narrowSchema := &schema.TableSchema{Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt32},
			{Name: "amt", DataType: schema.TypeDecimal, Precision: 18, Scale: 4},
			{Name: "score", DataType: schema.TypeFloat32},
			{Name: "label", DataType: schema.TypeString, MaxLength: 16},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		}}
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: narrowStage, Schema: narrowSchema, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."swap_narrow_stage" VALUES (42, DECIMAL '123.4500', REAL '2.5', 'narrow', '010')`))

		narrowStgInc, narrowResInc, err := dest.CDCConditionalSwapIncarnations(ctx, widerTarget, narrowStage)
		require.NoError(t, err)
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   widerTarget,
			StagingTable:                  narrowStage,
			CDCExpectedIncarnation:        widerInc,
			CDCExpectedStagingIncarnation: narrowStgInc,
			CDCExpectedResultIncarnation:  narrowResInc,
			Schema:                        narrowSchema,
		}))
		var widerGotID int64
		var widerGotLabel string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT id, label FROM "iceberg"."cdc_it"."swap_wider_target"`).Scan(&widerGotID, &widerGotLabel))
		require.Equal(t, int64(42), widerGotID)
		require.Equal(t, "narrow", widerGotLabel)
		widerSchemaAfter, err := dest.GetTableSchema(ctx, widerTarget)
		require.NoError(t, err)
		require.Equal(t, schema.TypeInt64, widerSchemaAfter.Columns[0].DataType)
		require.Equal(t, 38, widerSchemaAfter.Columns[1].Precision)

		// Missing column in Schema returns explicit error without mutating target.
		missingColStage := "_bruin_staging.swap_missing_col_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: missingColStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		missStgInc, missResInc, err := dest.CDCConditionalSwapIncarnations(ctx, swapTarget, missingColStage)
		require.NoError(t, err)
		err = dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   swapTarget,
			StagingTable:                  missingColStage,
			CDCExpectedIncarnation:        oldTargetInc,
			CDCExpectedStagingIncarnation: missStgInc,
			CDCExpectedResultIncarnation:  missResInc,
			Schema: &schema.TableSchema{Columns: append(slicesClone(cols), schema.Column{
				Name:     "nonexistent_col",
				DataType: schema.TypeString,
			})},
		})
		require.ErrorContains(t, err, "is missing expected column")

		// Absent target with 0-row staging creates an empty target without bogus NULL rows;
		// absent target with stale staging aborts inside Trino without creating target at all.
		emptyAbsentTarget := "cdc_it.absent_empty_ok_target"
		emptyAbsentStage := "_bruin_staging.absent_empty_ok_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: emptyAbsentStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		emptyStgInc, emptyResInc, err := dest.CDCConditionalSwapIncarnations(ctx, emptyAbsentTarget, emptyAbsentStage)
		require.NoError(t, err)
		require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   emptyAbsentTarget,
			StagingTable:                  emptyAbsentStage,
			CDCExpectedStagingIncarnation: emptyStgInc,
			CDCExpectedResultIncarnation:  emptyResInc,
			Schema:                        &schema.TableSchema{Columns: cols},
		}))
		var emptyTargetCount int
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_it"."absent_empty_ok_target"`).Scan(&emptyTargetCount))
		require.Zero(t, emptyTargetCount)

		staleAbsentTarget := "cdc_it.absent_stale_stg_target"
		staleAbsentStage := "_bruin_staging.absent_stale_stg_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: staleAbsentStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."absent_stale_stg_stage" VALUES (7, 'stale-stg', '010', false, CURRENT_TIMESTAMP)`))
		staleStgInc, staleResInc, err := dest.CDCConditionalSwapIncarnations(ctx, staleAbsentTarget, staleAbsentStage)
		require.NoError(t, err)
		dest.afterSwapPrecheck = func() {
			_ = dest.Exec(ctx, `COMMENT ON TABLE "iceberg"."_bruin_staging"."absent_stale_stg_stage" IS 'tampered-staging'`)
		}
		err = dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   staleAbsentTarget,
			StagingTable:                  staleAbsentStage,
			CDCExpectedStagingIncarnation: staleStgInc,
			CDCExpectedResultIncarnation:  staleResInc,
			Schema:                        &schema.TableSchema{Columns: cols},
		})
		dest.afterSwapPrecheck = nil
		require.ErrorContains(t, err, "physical incarnation changed")
		_, staleTargetExists, err := dest.CDCTargetIncarnation(ctx, staleAbsentTarget)
		require.NoError(t, err)
		require.False(t, staleTargetExists)
	})

	t.Run("outside replacement protection for swap/merge/truncate, continuous swap visibility, and JSON freeze resume", func(t *testing.T) {
		cols := []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "val", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}
		colNames := []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}

		// 1. Conditional SwapTable leaves an outside replacement table 100% untouched when replaced after precheck.
		raceTarget := "cdc_it.race_swap_target"
		raceStage := "_bruin_staging.race_swap_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: raceTarget, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: raceStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		oldRaceInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, raceTarget)
		require.NoError(t, err)
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."race_swap_target" VALUES (1, 'old-row', '001', false, CURRENT_TIMESTAMP)`))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."race_swap_stage" VALUES (2, 'refresh-row', '010', false, CURRENT_TIMESTAMP)`))

		raceStgInc, raceResInc, err := dest.CDCConditionalSwapIncarnations(ctx, raceTarget, raceStage)
		require.NoError(t, err)

		dest.afterSwapPrecheck = func() {
			// External process (not acquiring _bruin_staging target lock) replaces race_swap_target right before SwapTable writes.
			_ = dest.Exec(ctx, `CREATE OR REPLACE TABLE "iceberg"."cdc_it"."race_swap_target" COMMENT 'external-owner' AS SELECT CAST(777 AS BIGINT) AS id, CAST('outside-replacement' AS VARCHAR) AS val, CAST('050' AS VARCHAR) AS "_cdc_lsn", false AS "_cdc_deleted", CURRENT_TIMESTAMP AS "_cdc_synced_at"`)
		}
		err = dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   raceTarget,
			StagingTable:                  raceStage,
			CDCExpectedIncarnation:        oldRaceInc,
			CDCExpectedStagingIncarnation: raceStgInc,
			CDCExpectedResultIncarnation:  raceResInc,
			Schema:                        &schema.TableSchema{Columns: cols},
		})
		dest.afterSwapPrecheck = nil
		require.ErrorContains(t, err, "physical incarnation changed")

		var raceCount int
		var raceID int64
		var raceVal string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(id), MAX(val) FROM "iceberg"."cdc_it"."race_swap_target"`).Scan(&raceCount, &raceID, &raceVal))
		require.Equal(t, 1, raceCount)
		require.Equal(t, int64(777), raceID)
		require.Equal(t, "outside-replacement", raceVal)

		var raceComment string
		require.NoError(t, dest.db.QueryRowContext(
			ctx,
			`SELECT comment FROM system.metadata.table_comments WHERE catalog_name = 'iceberg' AND schema_name = 'cdc_it' AND table_name = 'race_swap_target'`,
		).Scan(&raceComment))
		require.Equal(t, "external-owner", raceComment)

		// Absent-target conditional SwapTable also refuses to overwrite a table created after precheck.
		absentTarget := "cdc_it.absent_race_target"
		absentStage := "_bruin_staging.absent_race_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: absentStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."absent_race_stage" VALUES (3, 'staged-new', '010', false, CURRENT_TIMESTAMP)`))
		absentStgInc, absentResInc, err := dest.CDCConditionalSwapIncarnations(ctx, absentTarget, absentStage)
		require.NoError(t, err)

		dest.afterSwapPrecheck = func() {
			_ = dest.Exec(ctx, `CREATE TABLE "iceberg"."cdc_it"."absent_race_target" (id BIGINT, val VARCHAR) COMMENT 'external-absent-owner'`)
			_ = dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."absent_race_target" VALUES (888, 'keep-external')`)
		}
		err = dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   absentTarget,
			StagingTable:                  absentStage,
			CDCExpectedStagingIncarnation: absentStgInc,
			CDCExpectedResultIncarnation:  absentResInc,
			Schema:                        &schema.TableSchema{Columns: cols},
		})
		dest.afterSwapPrecheck = nil
		require.ErrorContains(t, err, "physical incarnation changed")
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(id), MAX(val) FROM "iceberg"."cdc_it"."absent_race_target"`).Scan(&raceCount, &raceID, &raceVal))
		require.Equal(t, 1, raceCount)
		require.Equal(t, int64(888), raceID)
		require.Equal(t, "keep-external", raceVal)

		// 2. Inline SQL incarnation predicate leaves an externally replaced table completely untouched on truncate & merge.
		guardTarget := "cdc_it.guard_target"
		guardStage := "_bruin_staging.guard_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: guardTarget, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: guardStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		oldGuardInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, guardTarget)
		require.NoError(t, err)
		metaBefore, oldMarker, err := dest.ensureCheckedCDCIncarnation(ctx, guardTarget, oldGuardInc)
		require.NoError(t, err)
		stalePred := trinoIncarnationExistsPredicate(metaBefore.catalog, metaBefore.schemaName, metaBefore.tableName, oldMarker)

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."guard_stage" VALUES (1, 'intruder-update', '999', false, CURRENT_TIMESTAMP), (2, 'intruder-insert', '999', false, CURRENT_TIMESTAMP)`))
		// External process replaces guard_target with a brand-new table carrying row (1, 'replacement-safe').
		require.NoError(t, dest.Exec(ctx, `CREATE OR REPLACE TABLE "iceberg"."cdc_it"."guard_target" AS SELECT CAST(1 AS BIGINT) AS id, CAST('replacement-safe' AS VARCHAR) AS val, CAST('050' AS VARCHAR) AS "_cdc_lsn", false AS "_cdc_deleted", CURRENT_TIMESTAMP AS "_cdc_synced_at"`))

		require.NoError(t, dest.truncateCDCTableIfIncarnationLocked(ctx, guardTarget, metaBefore.fqn, stalePred, nil))
		require.NoError(t, dest.mergeCDCLocked(ctx, destination.MergeOptions{
			TargetTable:            guardTarget,
			StagingTable:           guardStage,
			PrimaryKeys:            []string{"id"},
			Columns:                colNames,
			CDCExpectedIncarnation: oldGuardInc,
		}, stalePred, nil))
		require.ErrorContains(t, dest.checkCDCIncarnation(ctx, guardTarget, oldGuardInc), "physical incarnation changed")

		var replacementCount int
		var replacementVal string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(val) FROM "iceberg"."cdc_it"."guard_target"`).Scan(&replacementCount, &replacementVal))
		require.Equal(t, 1, replacementCount)
		require.Equal(t, "replacement-safe", replacementVal)

		// 3. Both unconditional and conditional SwapTable keep target continuously queryable and strip _cdc_unchanged_cols.
		visTarget := "cdc_it.vis_swap_target"
		stageWithToastCols := append(slicesClone(cols), schema.Column{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString})
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: visTarget, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		visInc, _, err := dest.EnsureCDCTargetIncarnation(ctx, visTarget)
		require.NoError(t, err)
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."vis_swap_target" VALUES (1, 'v0', '001', false, CURRENT_TIMESTAMP)`))

		stopReader := make(chan struct{})
		readerErrCh := make(chan error, 1)
		go func() {
			defer close(readerErrCh)
			for {
				select {
				case <-stopReader:
					return
				default:
				}
				var c int
				if qErr := dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_it"."vis_swap_target"`).Scan(&c); qErr != nil {
					readerErrCh <- qErr
					return
				}
				if c < 1 {
					readerErrCh <- fmt.Errorf("expected at least 1 visible row during swap, got %d", c)
					return
				}
			}
		}()

		for round := 1; round <= 3; round++ {
			stg := fmt.Sprintf("_bruin_staging.vis_swap_stage_%d", round)
			require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
				Table:     stg,
				Schema:    &schema.TableSchema{Columns: stageWithToastCols},
				DropFirst: true,
			}))
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(
				`INSERT INTO "iceberg"."_bruin_staging"."vis_swap_stage_%d" VALUES (%d, 'v%d', '01%d', false, CURRENT_TIMESTAMP, '[]')`,
				round, round, round, round,
			)))
			stgInc, resInc, err := dest.CDCConditionalSwapIncarnations(ctx, visTarget, stg)
			require.NoError(t, err)
			require.NoError(t, dest.SwapTable(ctx, destination.SwapOptions{
				TargetTable:                   visTarget,
				StagingTable:                  stg,
				CDCExpectedIncarnation:        visInc,
				CDCExpectedStagingIncarnation: stgInc,
				CDCExpectedResultIncarnation:  resInc,
				Schema:                        &schema.TableSchema{Columns: stageWithToastCols},
			}))
		}
		close(stopReader)
		require.NoError(t, <-readerErrCh)

		visSchema, err := dest.GetTableSchema(ctx, visTarget)
		require.NoError(t, err)
		require.False(t, destination.HasCDCUnchangedColsColumn(visSchema.ColumnNames()))

		// 4. JSON, JSON array, interval, and unconstrained decimal survive GetTableSchema + ContractFreeze resume.
		jsonResumeTable := "cdc_it.json_resume_target"
		sourceResumeSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64, Nullable: true},
				{Name: "payload", DataType: schema.TypeJSON, Nullable: true},
				{Name: "payloads", DataType: schema.TypeArray, ArrayType: schema.TypeJSON, Nullable: true},
				{Name: "span", DataType: schema.TypeInterval, Nullable: true},
				{Name: "amt", DataType: schema.TypeDecimal, Nullable: true},
				{Name: "label", DataType: schema.TypeString, Nullable: true},
			},
		}
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{
			Table:     jsonResumeTable,
			Schema:    sourceResumeSchema,
			DropFirst: true,
		}))
		inspectedSchema, err := dest.GetTableSchema(ctx, jsonResumeTable)
		require.NoError(t, err)
		require.NotNil(t, inspectedSchema)

		cmp, err := schemaevolution.Compare(sourceResumeSchema, inspectedSchema, &schemaevolution.CompareOptions{
			NormalizeSourceColumn: dest.NormalizeSchemaEvolutionSourceColumn,
		})
		require.NoError(t, err)
		require.False(t, cmp.HasChanges)
		contractRes := schemaevolution.ApplyContract(schemaevolution.SchemaContract{Mode: schemaevolution.ContractFreeze}, cmp)
		require.False(t, contractRes.HasViolations())
	})

	t.Run("MergeCDCTablesAtomically shadow compute, inline-guarded rollback, active heartbeat, and crash recovery", func(t *testing.T) {
		cols := []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "val", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		}
		colNames := []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn}

		t1 := "cdc_it.atomic_t1"
		t2 := "cdc_it.atomic_t2"
		s1 := "_bruin_staging.atomic_s1"
		s2 := "_bruin_staging.atomic_s2"

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: t1, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: t2, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		inc1, _, err := dest.EnsureCDCTargetIncarnation(ctx, t1)
		require.NoError(t, err)
		inc2, _, err := dest.EnsureCDCTargetIncarnation(ctx, t2)
		require.NoError(t, err)

		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."atomic_t1" VALUES (1, 'old1', '010', false, CURRENT_TIMESTAMP), (2, 'old2', '010', false, CURRENT_TIMESTAMP)`))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."atomic_t2" VALUES (10, 'base10', '010', false, CURRENT_TIMESTAMP)`))

		// 1) Multi-table success (Truncate: true on t1 + incremental merge on t2).
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s1, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s2, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s1" VALUES (3, 'snap3', '020', false, CURRENT_TIMESTAMP)`))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s2" VALUES (10, 'updated10', '020', false, CURRENT_TIMESTAMP), (11, 'new11', '020', false, CURRENT_TIMESTAMP)`))

		require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            t1,
					StagingTable:           s1,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            t2,
					StagingTable:           s2,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc2,
				},
			},
		}))

		var cnt1, cnt2 int
		var val1, val2 string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1, &val1))
		require.Equal(t, 1, cnt1)
		require.Equal(t, "snap3", val1)
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(val) FROM "iceberg"."cdc_it"."atomic_t2"`).Scan(&cnt2, &val2))
		require.Equal(t, 2, cnt2)
		require.Equal(t, "new11", val2)

		var journalRemaining int
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).Scan(&journalRemaining))
		require.Zero(t, journalRemaining)

		// 2) Phase-1 shadow compute error aborts BEFORE any live table snapshot changes!
		_, hasSnapBefore, snap1Before, err := dest.readTrinoMainSnapshotRef(ctx, "iceberg", "cdc_it", "atomic_t1")
		require.NoError(t, err)
		require.True(t, hasSnapBefore)

		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s1, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s1" VALUES (99, 'must-not-commit', '030', false, CURRENT_TIMESTAMP)`))

		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            t1,
					StagingTable:           s1,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            t2,
					StagingTable:           "_bruin_staging.nonexistent_atomic_stage",
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		require.Error(t, err)

		_, _, snap1AfterP1, err := dest.readTrinoMainSnapshotRef(ctx, "iceberg", "cdc_it", "atomic_t1")
		require.NoError(t, err)
		require.Equal(t, snap1Before, snap1AfterP1, "Phase-1 shadow failure must not touch live t1 snapshot")

		// 3) Phase-3 mid-publish failure after t1 commits rolls t1 back to exact pre-batch rows via inline-guarded MERGE INTO.
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s2, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s2" VALUES (12, 'must-not-commit-t2', '030', false, CURRENT_TIMESTAMP)`))

		dest.beforeAtomicPublishStep = func(step int, _ string) error {
			if step == 1 {
				// Confirm t1 actually published its new row before step 1 fails!
				var publishedVal string
				if qErr := dest.db.QueryRowContext(ctx, `SELECT MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&publishedVal); qErr != nil {
					return qErr
				}
				if publishedVal != "must-not-commit" {
					return fmt.Errorf("expected t1 to have published 'must-not-commit' at step 1, got %q", publishedVal)
				}
				return fmt.Errorf("simulated mid-publish failure at step 1")
			}
			return nil
		}
		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            t1,
					StagingTable:           s1,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            t2,
					StagingTable:           s2,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		dest.beforeAtomicPublishStep = nil
		require.ErrorContains(t, err, "simulated mid-publish failure at step 1")

		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1, &val1))
		require.Equal(t, 1, cnt1)
		require.Equal(t, "snap3", val1)
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).Scan(&journalRemaining))
		require.Zero(t, journalRemaining)

		// 4) Active journal lease is NOT stolen by a second destination, while an expired crashed journal IS automatically recovered!
		_, hasPreSnap, preSnapID, err := dest.readTrinoMainSnapshotRef(ctx, "iceberg", "cdc_it", "atomic_t1")
		require.NoError(t, err)
		metaT1, markerT1, err := dest.ensureCheckedCDCIncarnation(ctx, t1, inc1)
		require.NoError(t, err)
		_, hasPreSnap2, preSnapID2, err := dest.readTrinoMainSnapshotRef(ctx, "iceberg", "cdc_it", "atomic_t2")
		require.NoError(t, err)
		_, markerT2, err := dest.ensureCheckedCDCIncarnation(ctx, t2, inc2)
		require.NoError(t, err)

		bakMarker, err := generateTrinoIncarnationMarker()
		require.NoError(t, err)
		crashMS := time.Now().UnixMilli()
		bakName0 := formatTrinoAtomicStagingTableName("bak", crashMS, "crashtest", 0)
		bakFQN0 := qualifyTrinoTable("iceberg", trinoCDCLeaseSchema, bakName0)
		bakName1 := formatTrinoAtomicStagingTableName("bak", crashMS, "crashtest", 1)
		bakFQN1 := qualifyTrinoTable("iceberg", trinoCDCLeaseSchema, bakName1)
		shdName0 := formatTrinoAtomicStagingTableName("shd", crashMS, "crashtest", 0)
		shdName1 := formatTrinoAtomicStagingTableName("shd", crashMS, "crashtest", 1)

		require.NoError(t, dest.Exec(ctx, buildCreateTableFromStagingSQL(
			bakFQN0,
			metaT1.fqn,
			strings.Join(quoteColumns(colNames), ", "),
			dest.jsonType,
			trinoIncarnationCommentPrefix+bakMarker,
			"",
		)))
		require.NoError(t, dest.Exec(ctx, buildCreateTableFromStagingSQL(
			bakFQN1,
			`"iceberg"."cdc_it"."atomic_t2"`,
			strings.Join(quoteColumns(colNames), ", "),
			dest.jsonType,
			trinoIncarnationCommentPrefix+bakMarker,
			"",
		)))

		// Simulate partial publish where t1 committed dirty row (888, 'partial-crashed') before process died, while t2 stayed at preSnapID2.
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."atomic_t1" VALUES (888, 'partial-crashed', '099', false, CURRENT_TIMESTAMP)`))

		journalFQN := qualifyTrinoTable("iceberg", trinoCDCLeaseSchema, trinoCDCBatchJournalTable)
		crashPlans := []*trinoAtomicTargetPlan{
			{
				index: 0, table: t1, catalog: "iceberg", schemaName: "cdc_it", tableName: "atomic_t1",
				expectedIncarnation: inc1, targetMarker: markerT1,
				hasRefs: true, hasPreSnapshot: hasPreSnap, preSnapshotID: preSnapID,
				backupName: bakName0, backupMarker: bakMarker, shadowName: shdName0,
			},
			{
				index: 1, table: t2, catalog: "iceberg", schemaName: "cdc_it", tableName: "atomic_t2",
				expectedIncarnation: inc2, targetMarker: markerT2,
				hasRefs: true, hasPreSnapshot: hasPreSnap2, preSnapshotID: preSnapID2,
				backupName: bakName1, backupMarker: bakMarker, shadowName: shdName1,
			},
		}
		// Unexpired TTL (60s): second destination must NOT roll back active batch!
		require.NoError(t, dest.insertAtomicBatchJournalRows(ctx, journalFQN, "crashtest", "crash-owner", 60_000, crashPlans))

		dest2 := NewTrinoDestination()
		require.NoError(t, dest2.Connect(ctx, uri))
		defer func() { _ = dest2.Close(ctx) }()

		_, _, err = dest2.CDCTargetIncarnation(ctx, t1)
		require.NoError(t, err)
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1))
		require.Equal(t, 2, cnt1, "active unexpired batch must not be rolled back by another process")

		// Now expire the journal lease to simulate crashed owner timeout -> dest2 recovers t1 on entrypoint!
		require.NoError(t, dest.expireAtomicBatchJournalRows(ctx, journalFQN, "crashtest", "crash-owner"))
		recoveredInc, exists, err := dest2.CDCTargetIncarnation(ctx, t1)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, inc1, recoveredInc)

		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1, &val1))
		require.Equal(t, 1, cnt1)
		require.Equal(t, "snap3", val1)
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).Scan(&journalRemaining))
		require.Zero(t, journalRemaining)

		// 4b) External INSERT into t2 right before t2's publishSQL advances t2$refs snapshot_id ->
		// t2's inline snapshot guard aborts publish, t1 rolls back cleanly, and t2's external INSERT row is NEVER wiped!
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s1, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s1" VALUES (66, 'must-rollback-on-ext-insert', '045', false, CURRENT_TIMESTAMP)`))

		dest.beforeAtomicPublishStep = func(step int, _ string) error {
			if step == 1 {
				var publishedVal string
				if qErr := dest.db.QueryRowContext(ctx, `SELECT MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&publishedVal); qErr != nil {
					return qErr
				}
				if publishedVal != "must-rollback-on-ext-insert" {
					return fmt.Errorf("expected t1 to have published 'must-rollback-on-ext-insert' before step 1, got %q", publishedVal)
				}
				return dest.Exec(ctx, `INSERT INTO "iceberg"."cdc_it"."atomic_t2" VALUES (777, 'external-concurrent-insert', '095', false, CURRENT_TIMESTAMP)`)
			}
			return nil
		}
		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            t1,
					StagingTable:           s1,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            t2,
					StagingTable:           s2,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		dest.beforeAtomicPublishStep = nil
		require.ErrorContains(t, err, "physical incarnation changed")

		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1, &val1))
		require.Equal(t, 1, cnt1)
		require.Equal(t, "snap3", val1)

		var extCount, uncommittedCount int
		require.NoError(t, dest.db.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FILTER (WHERE id = 777 AND val = 'external-concurrent-insert'), COUNT(*) FILTER (WHERE id = 12) FROM "iceberg"."cdc_it"."atomic_t2"`,
		).Scan(&extCount, &uncommittedCount))
		require.Equal(t, 1, extCount, "external concurrent INSERT into t2 must not be wiped by atomic rollback")
		require.Zero(t, uncommittedCount, "t2's staged CDC row must not have published after snapshot conflict")
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "iceberg"."_bruin_staging"."__ingestr_cdc_batch_journal"`).Scan(&journalRemaining))
		require.Zero(t, journalRemaining)

		// 4c) Stale unreferenced Phase-1 orphan bak_/shd_ tables are swept on entrypoint while fresh tables are preserved!
		oldOrphanMS := time.Now().Add(-10 * time.Minute).UnixMilli()
		freshOrphanMS := time.Now().UnixMilli()
		staleOrphanBak := formatTrinoAtomicStagingTableName("bak", oldOrphanMS, "orphanp1", 0)
		staleOrphanShd := formatTrinoAtomicStagingTableName("shd", oldOrphanMS, "orphanp1", 0)
		freshOrphanBak := formatTrinoAtomicStagingTableName("bak", freshOrphanMS, "freshp1", 0)

		for _, orphanTbl := range []string{staleOrphanBak, staleOrphanShd, freshOrphanBak} {
			orphanFQN := qualifyTrinoTable("iceberg", trinoCDCLeaseSchema, orphanTbl)
			require.NoError(t, dest.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s AS SELECT CAST(1 AS BIGINT) AS id`, orphanFQN)))
		}
		_, err = dest2.GetTableSchema(ctx, t1)
		require.NoError(t, err)

		var staleRemaining, freshRemaining int
		require.NoError(t, dest.db.QueryRowContext(
			ctx,
			fmt.Sprintf(
				`SELECT
					COUNT(*) FILTER (WHERE lower(table_name) IN (lower(%s), lower(%s))),
					COUNT(*) FILTER (WHERE lower(table_name) = lower(%s))
				FROM "iceberg".information_schema.tables
				WHERE lower(table_schema) = lower('_bruin_staging')`,
				escapeString(staleOrphanBak),
				escapeString(staleOrphanShd),
				escapeString(freshOrphanBak),
			),
		).Scan(&staleRemaining, &freshRemaining))
		require.Zero(t, staleRemaining, "stale Phase-1 orphan bak_/shd_ tables must be swept")
		require.Equal(t, 1, freshRemaining, "fresh bak_ table must not be swept prematurely")
		require.NoError(t, dest.Exec(ctx, "DROP TABLE IF EXISTS "+qualifyTrinoTable("iceberg", trinoCDCLeaseSchema, freshOrphanBak)))

		// 5) Outside replacement of t1 during mid-publish rollback is NEVER overwritten!
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: s1, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_s1" VALUES (55, 'transient-publish', '040', false, CURRENT_TIMESTAMP)`))

		dest.beforeAtomicPublishStep = func(step int, _ string) error {
			if step == 1 {
				_ = dest.Exec(ctx, `CREATE OR REPLACE TABLE "iceberg"."cdc_it"."atomic_t1" COMMENT 'outside-atomic-owner' AS SELECT CAST(999 AS BIGINT) AS id, CAST('outside-atomic' AS VARCHAR) AS val, CAST('080' AS VARCHAR) AS "_cdc_lsn", false AS "_cdc_deleted", CURRENT_TIMESTAMP AS "_cdc_synced_at"`)
				return fmt.Errorf("abort after external replacement")
			}
			return nil
		}
		err = dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{
			{
				Truncate: true,
				Options: destination.MergeOptions{
					TargetTable:            t1,
					StagingTable:           s1,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc1,
				},
			},
			{
				Options: destination.MergeOptions{
					TargetTable:            t2,
					StagingTable:           s2,
					PrimaryKeys:            []string{"id"},
					Columns:                colNames,
					CDCExpectedIncarnation: inc2,
				},
			},
		})
		dest.beforeAtomicPublishStep = nil
		require.ErrorContains(t, err, "abort after external replacement")

		var outsideID int64
		var outsideVal, outsideComment string
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(id), MAX(val) FROM "iceberg"."cdc_it"."atomic_t1"`).Scan(&cnt1, &outsideID, &outsideVal))
		require.Equal(t, 1, cnt1)
		require.Equal(t, int64(999), outsideID)
		require.Equal(t, "outside-atomic", outsideVal)
		require.NoError(t, dest.db.QueryRowContext(
			ctx,
			`SELECT comment FROM system.metadata.table_comments WHERE catalog_name = 'iceberg' AND schema_name = 'cdc_it' AND table_name = 'atomic_t1'`,
		).Scan(&outsideComment))
		require.Equal(t, "outside-atomic-owner", outsideComment)
	})
}

func readMergeRows(t *testing.T, ctx context.Context, db *sql.DB, fqn string) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		`SELECT tenant, id, COALESCE(payload, '<null>'), COALESCE("RankCol", '<null>'), COALESCE(_cdc_lsn, '<null>'), _cdc_deleted FROM %s ORDER BY tenant, id`,
		fqn,
	))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var tenant, id int64
		var payload, rank, lsn string
		var deleted bool
		require.NoError(t, rows.Scan(&tenant, &id, &payload, &rank, &lsn, &deleted))
		out = append(out, fmt.Sprintf("%d/%d:%s:%s:%s:%t", tenant, id, payload, rank, lsn, deleted))
	}
	require.NoError(t, rows.Err())
	return out
}

func slicesClone[T any](in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	return out
}
