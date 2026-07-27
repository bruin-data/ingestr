package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/metrics"
	"github.com/bruin-data/ingestr/pkg/source"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatherValue reads the current value of a metric series from the metrics
// registry, matching every label in labels. It returns 0 when the series is
// absent, which is sufficient for the delta-based assertions below.
func gatherValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := metrics.Gatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m, labels) {
				return metricScalar(m)
			}
		}
	}
	return 0
}

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	for k, v := range want {
		found := false
		for _, lp := range m.GetLabel() {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func metricScalar(m *dto.Metric) float64 {
	switch {
	case m.Counter != nil:
		return m.Counter.GetValue()
	case m.Gauge != nil:
		return m.Gauge.GetValue()
	default:
		return 0
	}
}

func rowsSyncedValue(t *testing.T) float64 {
	t.Helper()
	return gatherValue(t, "ingestr_stream_rows_synced_total", nil)
}

func lastSyncedValue(t *testing.T) float64 {
	t.Helper()
	return gatherValue(t, "ingestr_stream_last_synced_timestamp_seconds", nil)
}

func tableRowsSynced(t *testing.T, table string) float64 {
	t.Helper()
	return gatherValue(t, "ingestr_stream_table_rows_synced_total", map[string]string{"table": table})
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

	assert.Equal(t, float64(4), rowsSyncedValue(t)-before)
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
