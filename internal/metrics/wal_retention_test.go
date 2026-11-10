package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	dto "github.com/prometheus/client_model/go"
)

type walRetentionFunc func(context.Context) (source.WALRetentionSnapshot, error)

func (f walRetentionFunc) WALRetention(ctx context.Context) (source.WALRetentionSnapshot, error) {
	return f(ctx)
}

func gatherWALMetrics(t *testing.T) map[string]*dto.Metric {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]*dto.Metric)
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "ingestr_replication_slot_") {
			continue
		}
		if len(family.Metric) != 1 {
			t.Fatalf("expected one slot for %s, got %d", family.GetName(), len(family.Metric))
		}
		result[strings.TrimPrefix(family.GetName(), "ingestr_replication_slot_")] = family.Metric[0]
	}
	return result
}

func TestWALRetentionMetricsDuringSnapshot(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { SetWALRetentionReporter(nil) })
	retained, safe, limit := float64(2048), int64(6144), int64(8192)
	SetWALRetentionReporter(walRetentionFunc(func(ctx context.Context) (source.WALRetentionSnapshot, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("WAL collection must have a bounded deadline")
		}
		return source.WALRetentionSnapshot{
			Source: "postgres_cdc", Slot: "snapshot_slot", RetainedBytes: &retained,
			SafeBytes: &safe, MaxKeepBytes: &limit, Status: "reserved",
		}, nil
	}))

	got := gatherWALMetrics(t)
	want := map[string]float64{
		"retained_wal_bytes": 2048, "safe_wal_bytes": 6144,
		"max_wal_keep_bytes": 8192, "wal_status": 1, "collection_success": 1,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d WAL metrics, want %d", len(got), len(want))
	}
	for name, value := range want {
		metric := got[name]
		if metric == nil || metric.GetGauge().GetValue() != value {
			t.Fatalf("%s = %v, want %v", name, metric, value)
		}
		labels := make(map[string]string)
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		if labels["source"] != "postgres_cdc" || labels["slot"] != "snapshot_slot" {
			t.Fatalf("%s labels = %v", name, labels)
		}
		if name == "wal_status" && labels["status"] != "reserved" {
			t.Fatalf("WAL status labels = %v", labels)
		}
	}
	if _, found := replicationValue(t, "ingestr_replication_streaming"); found {
		t.Fatal("slot metrics must not mark a snapshot as streaming")
	}
}

func TestWALRetentionMetricsOmitUnavailableReadings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		limit  *int64
		err    error
	}{
		{name: "unlimited", status: "extended", limit: new(int64(-1))},
		{name: "lost", status: "lost", limit: new(int64(8192))},
		{name: "older server"},
		{name: "query failed", err: errors.New("query failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { SetWALRetentionReporter(nil) })
			SetWALRetentionReporter(walRetentionFunc(func(context.Context) (source.WALRetentionSnapshot, error) {
				return source.WALRetentionSnapshot{
					Source: "postgres_cdc", Slot: "test_slot", MaxKeepBytes: tc.limit, Status: tc.status,
				}, tc.err
			}))
			got := gatherWALMetrics(t)
			if got["safe_wal_bytes"] != nil || got["retained_wal_bytes"] != nil {
				t.Fatal("undefined WAL readings must be absent, not zero")
			}
			if metric := got["collection_success"]; metric == nil || metric.GetGauge().GetValue() != boolToFloat(tc.err == nil) {
				t.Fatalf("collection success = %v", metric)
			}
			if tc.err != nil {
				if len(got) != 1 {
					t.Fatalf("failed query emitted slot readings: %v", got)
				}
				return
			}
			if tc.limit == nil {
				if got["max_wal_keep_bytes"] != nil {
					t.Fatal("unavailable retention limit must be absent")
				}
			} else if metric := got["max_wal_keep_bytes"]; metric == nil || metric.GetGauge().GetValue() != float64(*tc.limit) {
				t.Fatalf("retention limit = %v, want %d", metric, *tc.limit)
			}
			if (got["wal_status"] != nil) != (tc.status != "") {
				t.Fatalf("WAL status metric = %v for status %q", got["wal_status"], tc.status)
			}
		})
	}
}

func TestWALRetentionMetricsDoNotKeepStaleSamples(t *testing.T) {
	t.Cleanup(func() { SetWALRetentionReporter(nil) })
	safe := int64(4096)
	snapshot := source.WALRetentionSnapshot{Source: "postgres_cdc", Slot: "test_slot", SafeBytes: &safe, Status: "reserved"}
	var queryErr error
	SetWALRetentionReporter(walRetentionFunc(func(context.Context) (source.WALRetentionSnapshot, error) {
		return snapshot, queryErr
	}))
	if got := gatherWALMetrics(t); got["safe_wal_bytes"] == nil {
		t.Fatal("expected the initial safe WAL reading")
	}
	queryErr = context.DeadlineExceeded
	if got := gatherWALMetrics(t); len(got) != 1 || got["collection_success"].GetGauge().GetValue() != 0 {
		t.Fatalf("failed scrape must omit previous readings: %v", got)
	}
	queryErr = nil
	snapshot.SafeBytes = nil
	snapshot.Status = "lost"
	if got := gatherWALMetrics(t); got["safe_wal_bytes"] != nil || got["wal_status"] == nil {
		t.Fatalf("lost slot must expose status without stale safe WAL: %v", got)
	}
	SetWALRetentionReporter(nil)
	if got := gatherWALMetrics(t); len(got) != 0 {
		t.Fatalf("cleared reporter left metrics behind: %v", got)
	}
}

func TestWALRetentionMetricsBeforeSlotSelection(t *testing.T) {
	t.Cleanup(func() { SetWALRetentionReporter(nil) })
	SetWALRetentionReporter(walRetentionFunc(func(context.Context) (source.WALRetentionSnapshot, error) {
		return source.WALRetentionSnapshot{Source: "postgres_cdc"}, nil
	}))
	if got := gatherWALMetrics(t); len(got) != 0 {
		t.Fatalf("slot metrics appeared before a slot was selected: %v", got)
	}
}
