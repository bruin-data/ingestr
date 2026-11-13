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

	dest, _ := startTrinoIcebergForCDC(t, ctx, "cdc_it")

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
		staged, err := dest.ListStagingTables(ctx, "_bruin_staging", "orders_stream_")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"orders_stream_a", "orders_stream_b"}, staged)
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

	t.Run("TruncateCDCTableIfIncarnation, conditional SwapTable, and MergeCDCTablesAtomically", func(t *testing.T) {
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

		// Conditional SwapTable across schemas (_bruin_staging -> cdc_it).
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
		require.NotEmpty(t, resInc)
		require.NotEqual(t, oldTargetInc, resInc)

		// Mismatched target incarnation must fail before swapping.
		require.ErrorContains(t, dest.SwapTable(ctx, destination.SwapOptions{
			TargetTable:                   swapTarget,
			StagingTable:                  swapStage,
			CDCExpectedIncarnation:        oldTargetInc + "-bad",
			CDCExpectedStagingIncarnation: stgInc,
			CDCExpectedResultIncarnation:  resInc,
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

		// MergeCDCTablesAtomically with Truncate: true.
		atomicStage := "_bruin_staging.atomic_stage"
		require.NoError(t, dest.PrepareTable(ctx, destination.PrepareOptions{Table: atomicStage, Schema: &schema.TableSchema{Columns: cols}, DropFirst: true}))
		require.NoError(t, dest.Exec(ctx, `INSERT INTO "iceberg"."_bruin_staging"."atomic_stage" VALUES (99, 'atomic', '020', false, CURRENT_TIMESTAMP)`))

		require.NoError(t, dest.MergeCDCTablesAtomically(ctx, []destination.CDCAtomicTableMerge{{
			Truncate: true,
			Options: destination.MergeOptions{
				TargetTable:            swapTarget,
				StagingTable:           atomicStage,
				PrimaryKeys:            []string{"id"},
				Columns:                []string{"id", "val", destination.CDCLSNColumn, destination.CDCDeletedColumn, destination.CDCSyncedAtColumn},
				CDCExpectedIncarnation: resInc,
			},
		}}))
		require.NoError(t, dest.db.QueryRowContext(ctx, `SELECT id, val FROM "iceberg"."cdc_it"."swap_target"`).Scan(&swappedID, &swappedVal))
		require.Equal(t, int64(99), swappedID)
		require.Equal(t, "atomic", swappedVal)
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
