//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	internalregistry "github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	icebergdest "github.com/bruin-data/ingestr/pkg/destination/iceberg"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresCDCToIcebergRESTMinioStreamingSnapshotCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
	setupCDCSource(t, ctx, sourceConnString)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)
	_, err = sourcePool.Exec(ctx, `
		INSERT INTO public.test_cdc (name, value)
		SELECT 'snapshot-' || g, g FROM generate_series(4, 300) g
	`)
	require.NoError(t, err)
	var removedBeforeRecoveryID int64
	require.NoError(t, sourcePool.QueryRow(ctx, "SELECT min(id) FROM public.test_cdc").Scan(&removedBeforeRecoveryID))

	env := setupIcebergRESTMinioCatalog(t, ctx)
	target := "it_" + uniqueSuffix() + ".postgres_cdc_streaming_recovery"
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] + "&publication=test_pub"
	cfg := config.DefaultConfig()
	cfg.SourceURI = cdcSourceURI
	cfg.SourceTable = "public.test_cdc"
	cfg.DestURI = env.destURI
	cfg.DestTable = target
	cfg.PrimaryKeys = []string{"id"}
	cfg.PageSize = 1
	cfg.Stream = true
	cfg.FlushInterval = time.Hour
	cfg.FlushRecords = 1
	cfg.Progress = config.ProgressLog
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.IncrementalStrategyExplicit = true

	firstCtx, cancelFirst := context.WithCancel(ctx)
	firstRun := make(chan error, 1)
	go func() { firstRun <- pipeline.New(cfg).Run(firstCtx) }()

	require.Eventually(t, func() bool {
		var slots int
		err := sourcePool.QueryRow(ctx, `
			SELECT count(*) FROM pg_replication_slots
			WHERE slot_name LIKE 'ingestr_%'
		`).Scan(&slots)
		return err == nil && slots > 0
	}, 30*time.Second, 50*time.Millisecond, "streaming snapshot should create its durable replication slot")
	time.Sleep(time.Second)
	cancelFirst()
	waitForCanceledPipeline(t, firstRun)

	partialRows := readIcebergRows(t, ctx, env.destURI, target)
	require.Empty(t, partialRows, "an interrupted snapshot must not become visible")
	connectorID, partialState := managedIcebergCDCStateForTarget(t, ctx, env.destURI, target)
	require.False(t, hasCompleteManagedIcebergState(partialState, "snapshot", target),
		"a partial snapshot must not publish resumable destination state")

	var replacementID int64
	_, err = sourcePool.Exec(ctx, "DELETE FROM public.test_cdc WHERE id = $1", removedBeforeRecoveryID)
	require.NoError(t, err)
	err = sourcePool.QueryRow(
		ctx,
		"INSERT INTO public.test_cdc (name, value) VALUES ('after-crash', 9999) RETURNING id",
	).Scan(&replacementID)
	require.NoError(t, err)

	restartCfg := *cfg
	restartCfg.PageSize = 25
	restartCfg.FlushRecords = 25
	restartCfg.CDCResumeLSN = ""
	secondCtx, cancelSecond := context.WithCancel(ctx)
	secondRun := make(chan error, 1)
	go func() { secondRun <- pipeline.New(&restartCfg).Run(secondCtx) }()

	stateDest := icebergdest.NewDestination()
	require.NoError(t, stateDest.Connect(ctx, env.destURI))
	t.Cleanup(func() { _ = stateDest.Close(ctx) })
	require.Eventually(t, func() bool {
		entries, stateErr := stateDest.LoadCDCState(ctx, managedIcebergCDCStateTable, connectorID)
		return stateErr == nil && hasCompleteManagedIcebergState(entries, "snapshot", target)
	}, 120*time.Second, 250*time.Millisecond,
		"the restarted stream should reset the partial attempt and complete one fresh managed snapshot")
	cancelSecond()
	waitForCanceledPipeline(t, secondRun)

	rows := readIcebergRows(t, ctx, env.destURI, target)
	require.Len(t, rows, 300)
	byID := icebergRowsByID(rows)
	require.Empty(t, byID[removedBeforeRecoveryID], "a row deleted before the recovery snapshot must not survive from the interrupted attempt")
	require.Len(t, byID[replacementID], 1)
	for id, group := range byID {
		require.Lenf(t, group, 1, "id=%d must occur exactly once after recovery", id)
		require.Equal(t, false, group[0][destination.CDCDeletedColumn])
	}
}

func waitForCanceledPipeline(t *testing.T, run <-chan error) {
	t.Helper()
	select {
	case err := <-run:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30 seconds of cancellation")
	}
}

func TestPostgresCDCToIcebergRESTMinioResumesDurably(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
	setupCDCSource(t, ctx, sourceConnString)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)

	env := setupIcebergRESTMinioCatalog(t, ctx)
	target := "it_" + uniqueSuffix() + ".postgres_cdc_resume"
	cdcSourceURI := "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&publication=test_pub&mode=batch"
	cfg := config.DefaultConfig()
	cfg.SourceURI = cdcSourceURI
	cfg.SourceTable = "public.test_cdc"
	cfg.DestURI = env.destURI
	cfg.DestTable = target
	cfg.PrimaryKeys = []string{"id"}
	cfg.PageSize = 1
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.IncrementalStrategyExplicit = true

	// The first process snapshots the source and records its resume authority in
	// the shared destination-managed state table.
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	initialRows := readIcebergRows(t, ctx, env.destURI, target)
	require.Len(t, initialRows, 3)
	initialByID := icebergRowsByID(initialRows)
	for id := int64(1); id <= 3; id++ {
		require.Lenf(t, initialByID[id], 1, "snapshot row id=%d", id)
		require.Equal(t, false, initialByID[id][0][destination.CDCDeletedColumn])
	}
	connectorID, firstState := managedIcebergCDCStateForTarget(t, ctx, env.destURI, target)
	firstCheckpoint := latestCompleteManagedIcebergCheckpoint(t, firstState)
	require.NotEmpty(t, firstCheckpoint)
	firstIncarnation := managedIcebergTargetIncarnation(t, ctx, env.destURI, target)
	firstConfirmed, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.NotEmpty(t, firstConfirmed)

	// These transactions land while no pipeline exists. id=4 changes twice so
	// the resumed batch must collapse multiple WAL versions without duplicating
	// the key; id=2 exercises a durable tombstone.
	_, err = sourcePool.Exec(ctx, `
		BEGIN;
		UPDATE public.test_cdc SET name = 'item1-updated', value = 111 WHERE id = 1;
		DELETE FROM public.test_cdc WHERE id = 2;
		INSERT INTO public.test_cdc (name, value) VALUES ('item4', 400);
		COMMIT;
		UPDATE public.test_cdc SET name = 'item4-updated', value = 444 WHERE name = 'item4';
	`)
	require.NoError(t, err)

	// A new Pipeline is the restart boundary. It must discover the checkpoint
	// from managed state and consume only the WAL gap after the snapshot.
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	secondState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	secondCheckpoint := latestCompleteManagedIcebergCheckpoint(t, secondState)
	require.Truef(t, lsnGreater(t, ctx, sourcePool, secondCheckpoint, firstCheckpoint),
		"managed checkpoint must advance after resume (%s -> %s)", firstCheckpoint, secondCheckpoint)
	secondConfirmed, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.Truef(t, lsnGreater(t, ctx, sourcePool, secondConfirmed, firstConfirmed),
		"PostgreSQL confirmed_flush_lsn must advance after the resumed data is durable (%s -> %s)", firstConfirmed, secondConfirmed)

	assertPostgresCDCToIcebergState(t, readIcebergRows(t, ctx, env.destURI, target))

	// A second clean restart with no source changes must not replay rows or move
	// the durable checkpoint backwards.
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	thirdState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	thirdCheckpoint := latestCompleteManagedIcebergCheckpoint(t, thirdState)
	require.GreaterOrEqual(t, destination.CompareCDCPositions(thirdCheckpoint, secondCheckpoint), 0)
	assertPostgresCDCToIcebergState(t, readIcebergRows(t, ctx, env.destURI, target))
	require.Equal(t, firstIncarnation, managedIcebergTargetIncarnation(t, ctx, env.destURI, target))

	// WAL TRUNCATE is a destination mutation and a state transition. The target
	// is cleared in place, the following row is applied once, and only then may
	// the managed checkpoint advance.
	_, err = sourcePool.Exec(ctx, `
		BEGIN;
		TRUNCATE TABLE public.test_cdc;
		INSERT INTO public.test_cdc (name, value) VALUES ('after-truncate', 900);
		COMMIT;
	`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	truncateRows := readIcebergRows(t, ctx, env.destURI, target)
	require.Len(t, truncateRows, 1)
	require.Equal(t, "after-truncate", truncateRows[0]["name"])
	require.Equal(t, false, truncateRows[0][destination.CDCDeletedColumn])
	truncateState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	truncateCheckpoint := latestCompleteManagedIcebergCheckpoint(t, truncateState)
	require.Truef(t, lsnGreater(t, ctx, sourcePool, truncateCheckpoint, thirdCheckpoint),
		"managed checkpoint must advance after WAL TRUNCATE (%s -> %s)", thirdCheckpoint, truncateCheckpoint)
	require.True(t, hasCompleteManagedIcebergState(truncateState, "snapshot", target))
	require.True(t, hasCompleteManagedIcebergState(truncateState, "destination", target))
	require.Equal(t, firstIncarnation, managedIcebergTargetIncarnation(t, ctx, env.destURI, target),
		"WAL TRUNCATE must preserve the physical Iceberg table incarnation")
}

func TestPostgresCDCMultiTableToIcebergRESTMinioSnapshotsAndWALTruncate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := t.Context()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
	setupCDCSource(t, ctx, sourceConnString)

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)
	_, err = sourcePool.Exec(ctx, `
		CREATE TABLE public.test_cdc_aux (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL
		);
		INSERT INTO public.test_cdc_aux (name) VALUES ('aux-1'), ('aux-2');
		ALTER PUBLICATION test_pub ADD TABLE public.test_cdc_aux;
	`)
	require.NoError(t, err)

	env := setupIcebergRESTMinioCatalog(t, ctx)
	namespace := "it_" + uniqueSuffix()
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&publication=test_pub&mode=batch&dest_schema=" + namespace
	cfg.SourceTable = ""
	cfg.DestURI = env.destURI
	cfg.DestTable = namespace + ".anchor"
	cfg.NoLoadTimestamp = true
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.IncrementalStrategyExplicit = true

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	firstTarget := namespace + ".public_test_cdc"
	secondTarget := namespace + ".public_test_cdc_aux"
	require.Len(t, readIcebergRows(t, ctx, env.destURI, firstTarget), 3)
	require.Len(t, readIcebergRows(t, ctx, env.destURI, secondTarget), 2)

	_, err = sourcePool.Exec(ctx, `
		BEGIN;
		TRUNCATE TABLE public.test_cdc;
		INSERT INTO public.test_cdc (name, value) VALUES ('after-multi-truncate', 901);
		INSERT INTO public.test_cdc_aux (name) VALUES ('aux-3');
		COMMIT;
	`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	firstRows := readIcebergRows(t, ctx, env.destURI, firstTarget)
	require.Len(t, firstRows, 1)
	require.Equal(t, "after-multi-truncate", firstRows[0]["name"])
	secondRows := readIcebergRows(t, ctx, env.destURI, secondTarget)
	require.Len(t, secondRows, 3)
}

func TestPostgresCDCKeylessToIcebergRESTMinioIsIdempotentByTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })

	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)
	_, err = sourcePool.Exec(ctx, `
		CREATE TABLE public.keyless_events (event_id BIGINT, payload TEXT);
		ALTER TABLE public.keyless_events REPLICA IDENTITY FULL;
		INSERT INTO public.keyless_events VALUES (1, 'one'), (2, 'two');
		CREATE PUBLICATION keyless_pub FOR TABLE public.keyless_events;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	env := setupIcebergRESTMinioCatalog(t, ctx)
	namespace := "it_" + uniqueSuffix()
	target := namespace + ".public_keyless_events"
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&publication=keyless_pub&mode=batch&dest_schema=" + namespace
	cfg.DestURI = env.destURI
	cfg.PageSize = 1
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.IncrementalStrategyExplicit = true

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.Len(t, readIcebergRows(t, ctx, env.destURI, target), 2)
	connectorID, initialState := managedIcebergCDCStateForTarget(t, ctx, env.destURI, target)
	initialCheckpoint := latestCompleteManagedIcebergCheckpoint(t, initialState)
	initialIncarnation := managedIcebergTargetIncarnation(t, ctx, env.destURI, target)

	_, err = sourcePool.Exec(ctx, `INSERT INTO public.keyless_events VALUES (3, 'three')`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.Len(t, readIcebergRows(t, ctx, env.destURI, target), 3)
	insertState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	insertCheckpoint := latestCompleteManagedIcebergCheckpoint(t, insertState)
	require.True(t, lsnGreater(t, ctx, sourcePool, insertCheckpoint, initialCheckpoint))

	// UPDATE on a REPLICA IDENTITY FULL table is a retract pair: one deleted old
	// image plus one live new image, committed under the transaction's stable LSN.
	_, err = sourcePool.Exec(ctx, `UPDATE public.keyless_events SET payload = 'three-updated' WHERE event_id = 3`)
	require.NoError(t, err)
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	rows := readIcebergRows(t, ctx, env.destURI, target)
	require.Len(t, rows, 5)

	var oldDeletes, updatedInserts int
	for _, row := range rows {
		if row["event_id"] != int64(3) {
			continue
		}
		if row["payload"] == "three" && row[destination.CDCDeletedColumn] == true {
			oldDeletes++
		}
		if row["payload"] == "three-updated" && row[destination.CDCDeletedColumn] == false {
			updatedInserts++
		}
	}
	require.Equal(t, 1, oldDeletes)
	require.Equal(t, 1, updatedInserts)
	updateState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	updateCheckpoint := latestCompleteManagedIcebergCheckpoint(t, updateState)
	require.True(t, lsnGreater(t, ctx, sourcePool, updateCheckpoint, insertCheckpoint))

	// A clean finite rerun must not append the strict-resume boundary transaction
	// again. Managed state is the resume authority; the target commit token makes
	// a strict-boundary replay idempotent.
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	require.Len(t, readIcebergRows(t, ctx, env.destURI, target), 5)
	restartState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	require.GreaterOrEqual(t,
		destination.CompareCDCPositions(latestCompleteManagedIcebergCheckpoint(t, restartState), updateCheckpoint), 0)
	require.True(t, hasCompleteManagedIcebergState(restartState, "snapshot", target))
	require.True(t, hasCompleteManagedIcebergState(restartState, "destination", target))
	require.Equal(t, initialIncarnation, managedIcebergTargetIncarnation(t, ctx, env.destURI, target))
}

func TestPostgresCDCToIcebergRESTMinioReplaysAfterDataBeforeStateFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceContainer, sourceConnString := setupPostgresCDCContainer(t, ctx)
	t.Cleanup(func() { _ = sourceContainer.Terminate(ctx) })
	setupCDCSource(t, ctx, sourceConnString)
	sourcePool, err := pgxpool.New(ctx, sourceConnString)
	require.NoError(t, err)
	t.Cleanup(sourcePool.Close)

	env := setupIcebergRESTMinioCatalog(t, ctx)
	target := "it_" + uniqueSuffix() + ".postgres_cdc_state_failure"
	controller := &managedIcebergStateFailureController{}
	scheme := "iceberg-cdc-state-failure-" + uniqueSuffix()
	internalregistry.Default.RegisterDestination([]string{scheme}, func() interface{} {
		return &managedIcebergStateFailureDestination{
			Destination: icebergdest.NewDestination(),
			actualURI:   env.destURI,
			controller:  controller,
		}
	})

	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://" + sourceConnString[len("postgres://"):] +
		"&publication=test_pub&mode=batch&state_id=iceberg-data-before-state"
	cfg.SourceTable = "public.test_cdc"
	cfg.DestURI = scheme + "://catalog"
	cfg.DestTable = target
	cfg.PrimaryKeys = []string{"id"}
	cfg.PageSize = 1
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.IncrementalStrategyExplicit = true

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	connectorID, initialState := managedIcebergCDCStateForTarget(t, ctx, env.destURI, target)
	initialCheckpoint := latestCompleteManagedIcebergCheckpoint(t, initialState)
	initialConfirmed, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.NotEmpty(t, initialConfirmed)

	_, err = sourcePool.Exec(ctx, `
		BEGIN;
		UPDATE public.test_cdc SET name = 'item1-after-failure', value = 1111 WHERE id = 1;
		INSERT INTO public.test_cdc (name, value) VALUES ('fault-replay', 4444);
		COMMIT;
	`)
	require.NoError(t, err)
	controller.arm()
	err = pipeline.New(cfg).Run(ctx)
	require.ErrorIs(t, err, errInjectedManagedIcebergStateFailure)

	rowsAfterFailure := icebergRowsByID(readIcebergRows(t, ctx, env.destURI, target))
	require.Len(t, rowsAfterFailure[1], 1)
	require.Equal(t, "item1-after-failure", rowsAfterFailure[1][0]["name"],
		"target data must commit before the injected managed-state failure")
	require.Len(t, rowsAfterFailure[4], 1)
	failedState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	require.Equal(t, initialCheckpoint, latestCompleteManagedIcebergCheckpoint(t, failedState),
		"failed state persistence must not advance resume authority")
	failedConfirmed, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.Equal(t, initialConfirmed, failedConfirmed,
		"PostgreSQL confirmed_flush_lsn must not advance when managed state persistence fails")

	require.NoError(t, pipeline.New(cfg).Run(ctx))
	replayedRows := icebergRowsByID(readIcebergRows(t, ctx, env.destURI, target))
	require.Len(t, replayedRows, 4)
	for id, rows := range replayedRows {
		require.Lenf(t, rows, 1, "keyed WAL replay must remain idempotent for id=%d", id)
	}
	require.Equal(t, "item1-after-failure", replayedRows[1][0]["name"])
	require.Equal(t, "fault-replay", replayedRows[4][0]["name"])
	replayedState := loadManagedIcebergCDCState(t, ctx, env.destURI, connectorID)
	replayedCheckpoint := latestCompleteManagedIcebergCheckpoint(t, replayedState)
	require.Truef(t, lsnGreater(t, ctx, sourcePool, replayedCheckpoint, initialCheckpoint),
		"managed checkpoint must advance only after replay succeeds (%s -> %s)", initialCheckpoint, replayedCheckpoint)
	replayedConfirmed, _ := cdcReplicationSlotState(t, ctx, sourcePool)
	require.Truef(t, lsnGreater(t, ctx, sourcePool, replayedConfirmed, failedConfirmed),
		"PostgreSQL confirmed_flush_lsn must advance after durable replay (%s -> %s)", failedConfirmed, replayedConfirmed)
}

const managedIcebergCDCStateTable = "_bruin_staging.cdc_state"

var errInjectedManagedIcebergStateFailure = errors.New("injected Iceberg managed CDC state failure")

type managedIcebergStateFailureController struct {
	mu     sync.Mutex
	armed  bool
	writes int
}

func (c *managedIcebergStateFailureController) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.armed = true
	c.writes = 0
}

func (c *managedIcebergStateFailureController) shouldFail() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.armed {
		return false
	}
	c.writes++
	// BeginRun durably writes its run fence and in-progress snapshot first. The
	// third write is Persist, after the target data commit has succeeded.
	if c.writes != 3 {
		return false
	}
	c.armed = false
	return true
}

type managedIcebergStateFailureDestination struct {
	*icebergdest.Destination
	actualURI  string
	controller *managedIcebergStateFailureController
}

func (d *managedIcebergStateFailureDestination) Connect(ctx context.Context, _ string) error {
	return d.Destination.Connect(ctx, d.actualURI)
}

func (d *managedIcebergStateFailureDestination) WriteCDCState(
	ctx context.Context,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
) error {
	if !d.controller.shouldFail() {
		return d.Destination.WriteCDCState(ctx, records, opts)
	}
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	return errInjectedManagedIcebergStateFailure
}

func managedIcebergCDCStateForTarget(
	t *testing.T,
	ctx context.Context,
	destURI, target string,
) (string, []destination.CDCStateEntry) {
	t.Helper()
	rows := readIcebergRows(t, ctx, destURI, managedIcebergCDCStateTable)
	connectors := make(map[string]struct{})
	for _, row := range rows {
		if row["destination_table"] == target {
			connectorID, ok := row["connector_id"].(string)
			require.True(t, ok)
			connectors[connectorID] = struct{}{}
		}
	}
	require.Len(t, connectors, 1, "managed state should have one connector for target %s", target)
	var connectorID string
	for connectorID = range connectors {
	}
	return connectorID, loadManagedIcebergCDCState(t, ctx, destURI, connectorID)
}

func loadManagedIcebergCDCState(
	t *testing.T,
	ctx context.Context,
	destURI, connectorID string,
) []destination.CDCStateEntry {
	t.Helper()
	dest := icebergdest.NewDestination()
	require.NoError(t, dest.Connect(ctx, destURI))
	entries, err := dest.LoadCDCState(ctx, managedIcebergCDCStateTable, connectorID)
	require.NoError(t, err)
	require.NoError(t, dest.Close(ctx))
	return entries
}

func latestCompleteManagedIcebergCheckpoint(t *testing.T, entries []destination.CDCStateEntry) string {
	t.Helper()
	latest := ""
	for _, entry := range entries {
		if entry.StateKind != "checkpoint" || entry.Status != destination.CDCStateStatusComplete {
			continue
		}
		if latest == "" || destination.CompareCDCPositions(entry.Position, latest) > 0 {
			latest = entry.Position
		}
	}
	require.NotEmpty(t, latest, "managed CDC state has no complete checkpoint")
	return latest
}

func hasCompleteManagedIcebergState(entries []destination.CDCStateEntry, kind, target string) bool {
	for _, entry := range entries {
		if entry.StateKind == kind && entry.Status == destination.CDCStateStatusComplete && entry.DestinationTable == target {
			return true
		}
	}
	return false
}

func managedIcebergTargetIncarnation(t *testing.T, ctx context.Context, destURI, table string) string {
	t.Helper()
	dest := icebergdest.NewDestination()
	require.NoError(t, dest.Connect(ctx, destURI))
	incarnation, exists, err := dest.CDCTargetIncarnation(ctx, table)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotEmpty(t, incarnation)
	require.NoError(t, dest.Close(ctx))
	return incarnation
}

func assertPostgresCDCToIcebergState(t *testing.T, rows []map[string]any) {
	t.Helper()

	require.Len(t, rows, 4, "the delete tombstone remains as one logical Iceberg row")
	byID := icebergRowsByID(rows)
	for id := int64(1); id <= 4; id++ {
		require.Lenf(t, byID[id], 1, "id=%d must have exactly one logical row", id)
	}
	require.Equal(t, "item1-updated", byID[1][0]["name"])
	require.Equal(t, int64(111), byID[1][0]["value"])
	require.Equal(t, false, byID[1][0][destination.CDCDeletedColumn])
	require.Equal(t, true, byID[2][0][destination.CDCDeletedColumn])
	require.Equal(t, "item3", byID[3][0]["name"])
	require.Equal(t, false, byID[3][0][destination.CDCDeletedColumn])
	require.Equal(t, "item4-updated", byID[4][0]["name"])
	require.Equal(t, int64(444), byID[4][0]["value"])
	require.Equal(t, false, byID[4][0][destination.CDCDeletedColumn])

	var live int
	for _, row := range rows {
		if row[destination.CDCDeletedColumn] == false {
			live++
		}
	}
	require.Equal(t, 3, live)
}
