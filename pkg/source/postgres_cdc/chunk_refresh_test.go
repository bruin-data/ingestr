package postgres_cdc

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func chunkRefreshTestReader(store source.CDCChunkStore, refreshID string, full bool) *chunkReader {
	return &chunkReader{
		tables: []source.SourceTableInfo{{Name: "public.items", Incarnation: "42", SchemaFingerprint: "schema"}},
		opts:   source.ReadOptions{FullRefresh: full},
		options: &source.CDCChunkedSnapshotOptions{
			Store: store, RefreshID: refreshID, WALStaging: "destination", Execution: "parallel",
		},
	}
}

func TestChunkRefreshIntentSurvivesSlotResetBeforeFirstCheckpoint(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	previous.RefreshID = "old-operation"
	require.NoError(t, (&chunkedCommit{store: store, state: previous}).persist(t.Context()))

	reader := chunkRefreshTestReader(store, "new-operation", true)
	state, intent, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	require.Nil(t, state)
	require.NotNil(t, intent)
	require.NotEqual(t, previous.Epoch, intent.Epoch)
	require.Equal(t, []chunkRetiredEpoch{{Epoch: previous.Epoch, Consumed: previous.Consumed}}, intent.RetiredEpochs)
	stored, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, previous, stored, "the new intent must survive independently of the old checkpoint")

	// PrepareRefresh has already opened the operation's CDC generation on retry.
	restarted := chunkRefreshTestReader(store, "new-operation", false)
	state, recovered, err := restarted.prepareChunkRefresh(t.Context(), stored, "slot")
	require.NoError(t, err)
	require.Nil(t, state, "the replaced slot must not be validated against the old bootstrap")
	require.Equal(t, intent, recovered)
}

func TestChunkRefreshRetriesAfterOrdinaryGenerationBeforeSourceIntent(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	reader := chunkRefreshTestReader(store, "pending-operation", false)
	state, intent, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	require.Nil(t, state)
	require.Equal(t, "pending-operation", intent.RefreshID)
	require.NotEmpty(t, store.values[chunkRefreshIntentKey])
}

func TestChunkRefreshFirstCheckpointMakesIntentIdempotent(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	reader := chunkRefreshTestReader(store, "operation", true)
	_, intent, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	current := chunkTestState(store.identity)
	current.Epoch, current.RefreshID = intent.Epoch, "operation"
	current.Tables["public.items"].After = []string{"25"}
	store.beforeSave = func(string) { t.Fatal("a completed refresh intent must not be reissued") }
	for _, full := range []bool{false, true} {
		reader.opts.FullRefresh = full
		state, pending, err := reader.prepareChunkRefresh(t.Context(), current, "slot")
		require.NoError(t, err)
		require.Same(t, current, state)
		require.Nil(t, pending)
	}
	reader.options.RefreshID, reader.opts.FullRefresh = "", false
	state, pending, err := reader.prepareChunkRefresh(t.Context(), current, "slot")
	require.NoError(t, err)
	require.Same(t, current, state, "completed operations need no active refresh ID")
	require.Nil(t, pending)
}

func TestChunkRefreshIntentFailureLeavesPreviousCheckpoint(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	require.NoError(t, (&chunkedCommit{store: store, state: previous}).persist(t.Context()))
	store.failSaveKey = chunkRefreshIntentKey
	reader := chunkRefreshTestReader(store, "operation", true)
	_, _, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.ErrorContains(t, err, "failed to persist")
	stored, err := loadChunkState(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, previous, stored)
	require.NotContains(t, store.values, chunkRefreshIntentKey)
}

func TestChunkRefreshRejectsChangedPendingConfiguration(t *testing.T) {
	for _, change := range []string{"slot", "mode", "execution", "table", "incarnation", "schema", "missing-refresh-id"} {
		t.Run(change, func(t *testing.T) {
			store := newChunkTestStore()
			previous := chunkTestState(store.identity)
			reader := chunkRefreshTestReader(store, "operation", true)
			_, _, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
			require.NoError(t, err)
			reader.opts.FullRefresh = false
			slot := "slot"
			switch change {
			case "slot":
				slot = "other-slot"
			case "mode":
				reader.options.WALStaging = "inmemory"
			case "execution":
				reader.options.Execution = "serial"
			case "table":
				reader.tables[0].Name = "public.other"
			case "incarnation":
				reader.tables[0].Incarnation = "43"
			case "schema":
				reader.tables[0].SchemaFingerprint = "new-schema"
			case "missing-refresh-id":
				reader.options.RefreshID = ""
			}
			store.beforeSave = func(string) { t.Fatal("invalid retry must not replace the pending intent") }
			_, _, err = reader.prepareChunkRefresh(t.Context(), previous, slot)
			require.ErrorContains(t, err, "unfinished chunked snapshot refresh")
		})
	}
}

func TestChunkRefreshNewOperationSupersedesPendingIntent(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	reader := chunkRefreshTestReader(store, "operation-one", true)
	_, first, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	reader.options.RefreshID = "operation-two"
	reader.opts.FullRefresh = false
	state, second, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	require.Nil(t, state)
	require.NotEqual(t, first.Epoch, second.Epoch)
	require.Equal(t, "operation-two", second.RefreshID)
	require.Equal(t, first.RetiredEpochs, second.RetiredEpochs)
}

func TestChunkRefreshUnnamedOperationRetriesUntilCheckpoint(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	reader := chunkRefreshTestReader(store, "", true)
	_, first, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	reader.opts.FullRefresh = false
	state, recovered, err := reader.prepareChunkRefresh(t.Context(), previous, "slot")
	require.NoError(t, err)
	require.Nil(t, state)
	require.Equal(t, first, recovered)
	current := previous.clone()
	current.Epoch = first.Epoch
	reader.opts.FullRefresh = true
	_, second, err := reader.prepareChunkRefresh(t.Context(), current, "slot")
	require.NoError(t, err)
	require.NotEqual(t, first.Epoch, second.Epoch, "an unnamed full refresh after a durable checkpoint starts a new operation")
}

func TestChunkRefreshRejectsCorruptIntent(t *testing.T) {
	store := newChunkTestStore()
	store.values[chunkRefreshIntentKey] = []byte(`{"version":1,"epoch":"pending"}`)
	reader := chunkRefreshTestReader(store, "operation", false)
	_, _, err := reader.prepareChunkRefresh(t.Context(), chunkTestState(store.identity), "slot")
	require.ErrorContains(t, err, "invalid chunked snapshot refresh intent")
}

func TestChunkInitializeRejectsChangedExecutionBeforeSlotAccess(t *testing.T) {
	store := newChunkTestStore()
	previous := chunkTestState(store.identity)
	previous.Mode = "inmemory"
	data, err := json.Marshal(previous)
	require.NoError(t, err)
	store.values[chunkStateKey] = data
	reader := chunkRefreshTestReader(store, "", false)
	reader.options.Execution, reader.options.WALStaging = "", "inmemory"
	_, _, err = reader.initialize(t.Context(), "slot", nil)
	require.ErrorContains(t, err, "execution")
	require.Equal(t, "serial", chunkExecution(reader.options.Execution))
	require.Equal(t, "parallel", checkpointExecution(previous.Execution))
}

func TestChunkInitializeRejectsSerialDestinationStaging(t *testing.T) {
	reader := chunkRefreshTestReader(nil, "", false)
	reader.options.Execution = "serial"
	_, _, err := reader.initialize(t.Context(), "slot", nil)
	require.ErrorContains(t, err, "serial chunked snapshots require inmemory")
}
