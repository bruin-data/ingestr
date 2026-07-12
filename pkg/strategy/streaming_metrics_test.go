package strategy

import (
	"context"
	"encoding/json"
	"expvar"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rowsSyncedValue(t *testing.T) int64 {
	t.Helper()
	v, ok := expvar.Get("ingestr_stream_rows_synced").(*expvar.Int)
	require.True(t, ok, "ingestr_stream_rows_synced is not an expvar.Int")
	return v.Value()
}

func lastSyncedValue(t *testing.T) int64 {
	t.Helper()
	v, ok := expvar.Get("ingestr_stream_last_synced_unix").(*expvar.Int)
	require.True(t, ok, "ingestr_stream_last_synced_unix is not an expvar.Int")
	return v.Value()
}

func tableRowsSynced(t *testing.T, table string) float64 {
	t.Helper()
	var out map[string]map[string]float64
	require.NoError(t, json.Unmarshal([]byte(expvar.Get("ingestr_stream_tables").String()), &out))
	return out[table]["rows_synced"]
}

func TestStreaming_FlushRecordsRowsSynced(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyAppend,
		Committer:     committer,
	}, map[string]*streamTableState{"": {destTable: "ds.tbl", schema: streamTestSchema()}})

	before := rowsSyncedValue(t)

	loop.buffer(source.RecordBatchResult{
		Batch:       int64RecordBatch(t, "id", []int64{1, 2, 3, 4}, nil),
		CommitToken: 7,
	})
	require.NoError(t, loop.flush(context.Background()))

	assert.Equal(t, int64(4), rowsSyncedValue(t)-before)
	// Single-table loops key l.tables on "", so the metric must fall back to
	// the destination table name rather than publishing an empty key.
	assert.Positive(t, tableRowsSynced(t, "ds.tbl"))
	assert.NotZero(t, lastSyncedValue(t))
}

// A write that lands but whose source position fails to commit is not durable:
// re-delivery will replay those rows, so counting them would double-count.
func TestStreaming_FailedCommitDoesNotRecordRowsSynced(t *testing.T) {
	dest := &fakeDestination{}
	committer := &fakeCommitter{err: assert.AnError}
	loop := newTestLoop(dest, StreamingOptions{
		FlushInterval: time.Hour,
		FlushRecords:  100,
		Strategy:      config.StrategyAppend,
		Committer:     committer,
	}, map[string]*streamTableState{"": {destTable: "ds.tbl", schema: streamTestSchema()}})

	before := rowsSyncedValue(t)
	beforeTS := lastSyncedValue(t)

	loop.buffer(source.RecordBatchResult{
		Batch:       int64RecordBatch(t, "id", []int64{1, 2, 3, 4}, nil),
		CommitToken: 7,
	})
	require.Error(t, loop.flush(context.Background()))

	assert.Equal(t, before, rowsSyncedValue(t), "rows must not be counted when the commit fails")
	assert.Equal(t, beforeTS, lastSyncedValue(t), "last synced must not advance when the commit fails")
}
