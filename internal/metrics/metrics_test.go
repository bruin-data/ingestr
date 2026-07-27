package metrics

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

type fakeReporter struct {
	snap source.LagSnapshot
	ok   bool
}

func (f fakeReporter) ReplicationLag() (source.LagSnapshot, bool) { return f.snap, f.ok }

// replicationValue gathers the current value of a source-labeled replication
// metric. found is false when the series is absent, which is how the collector
// signals a dimension the engine cannot express.
func replicationValue(t *testing.T, name string) (value float64, found bool) {
	t.Helper()
	mfs, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		m := mf.GetMetric()
		if len(m) == 0 {
			return 0, false
		}
		return metricValue(m[0]), true
	}
	return 0, false
}

func metricValue(m *dto.Metric) float64 {
	switch {
	case m.Gauge != nil:
		return m.Gauge.GetValue()
	case m.Counter != nil:
		return m.Counter.GetValue()
	default:
		return 0
	}
}

// resetState clears the per-table series and the reporter so each test starts
// clean. The plain counters (rowsSynced, flushCycles) cannot be reset, so tests
// that assert their totals measure a before/after delta instead.
func resetState(t *testing.T) {
	t.Helper()
	SetLagReporter(nil)
	tableRowsSynced.Reset()
	tableLastFlushRows.Reset()
	tableLastSyncedTS.Reset()
	lastSyncedTS.Set(0)
	t.Cleanup(func() { SetLagReporter(nil) })
}

func TestReplicationWithoutReporter(t *testing.T) {
	resetState(t)

	if _, found := replicationValue(t, "ingestr_replication_streaming"); found {
		t.Fatal("expected no replication series without a reporter")
	}
}

func TestReplicationReporterNotReady(t *testing.T) {
	resetState(t)
	SetLagReporter(fakeReporter{ok: false})

	if _, found := replicationValue(t, "ingestr_replication_streaming"); found {
		t.Fatal("expected no replication series when the reporter is not ready")
	}
}

func TestReplicationBytesBehind(t *testing.T) {
	resetState(t)
	behind := uint64(4096)
	SetLagReporter(fakeReporter{
		ok: true,
		snap: source.LagSnapshot{
			Source:          "postgres_cdc",
			BytesBehind:     &behind,
			ServerPosition:  "0/1A2B3C0",
			DurablePosition: "0/1A2A3C0",
			UpdatedAt:       time.Unix(1700000000, 0),
		},
	})

	if v, found := replicationValue(t, "ingestr_replication_streaming"); !found || v != 1 {
		t.Fatalf("expected streaming=1, got %v (found=%v)", v, found)
	}
	if v, found := replicationValue(t, "ingestr_replication_bytes_behind"); !found || v != 4096 {
		t.Fatalf("expected bytes_behind=4096, got %v (found=%v)", v, found)
	}
	if v, found := replicationValue(t, "ingestr_replication_caught_up"); !found || v != 0 {
		t.Fatalf("expected caught_up=0, got %v (found=%v)", v, found)
	}
	if v, found := replicationValue(t, "ingestr_replication_updated_at_timestamp_seconds"); !found || v != 1700000000 {
		t.Fatalf("expected updated_at=1700000000, got %v (found=%v)", v, found)
	}
	// seconds_behind is not applicable to Postgres and must be omitted rather
	// than reported as a misleading zero.
	if _, found := replicationValue(t, "ingestr_replication_seconds_behind"); found {
		t.Fatal("expected seconds_behind to be omitted for postgres")
	}
}

func TestReplicationSecondsBehindOnly(t *testing.T) {
	resetState(t)
	secs := 12.0
	SetLagReporter(fakeReporter{
		ok:   true,
		snap: source.LagSnapshot{Source: "mongodb", SecondsBehind: &secs},
	})

	if v, found := replicationValue(t, "ingestr_replication_seconds_behind"); !found || v != 12 {
		t.Fatalf("expected seconds_behind=12, got %v (found=%v)", v, found)
	}
	if _, found := replicationValue(t, "ingestr_replication_bytes_behind"); found {
		t.Fatal("expected bytes_behind to be omitted for mongodb")
	}
}

func TestRecordSyncAccumulates(t *testing.T) {
	resetState(t)

	beforeRows := testutil.ToFloat64(rowsSynced)
	beforeCycles := testutil.ToFloat64(flushCycles)

	RecordSync(map[string]int64{"public.users": 100, "public.orders": 50}, time.Unix(1000, 0))
	RecordSync(map[string]int64{"public.users": 25, "public.items": 7}, time.Unix(2000, 0))

	if got := testutil.ToFloat64(rowsSynced) - beforeRows; got != 182 {
		t.Fatalf("expected 182 rows synced in total, got %v", got)
	}
	if got := testutil.ToFloat64(flushCycles) - beforeCycles; got != 2 {
		t.Fatalf("expected 2 flush cycles, got %v", got)
	}
	if got := testutil.ToFloat64(lastSyncedTS); got != 2000 {
		t.Fatalf("expected last synced 2000, got %v", got)
	}

	// rows_synced accumulates across cycles; last_flush_rows is replaced.
	if got := testutil.ToFloat64(tableRowsSynced.WithLabelValues("public.users")); got != 125 {
		t.Fatalf("expected users rows_synced=125, got %v", got)
	}
	if got := testutil.ToFloat64(tableLastFlushRows.WithLabelValues("public.users")); got != 25 {
		t.Fatalf("expected users last_flush_rows=25, got %v", got)
	}

	// A table absent from cycle 2 keeps its totals and its older timestamp.
	if got := testutil.ToFloat64(tableRowsSynced.WithLabelValues("public.orders")); got != 50 {
		t.Fatalf("expected orders rows_synced=50, got %v", got)
	}
	if got := testutil.ToFloat64(tableLastSyncedTS.WithLabelValues("public.orders")); got != 1000 {
		t.Fatalf("expected orders to keep last_synced=1000, got %v", got)
	}

	// A table first seen in cycle 2 appears.
	if got := testutil.ToFloat64(tableRowsSynced.WithLabelValues("public.items")); got != 7 {
		t.Fatalf("expected items rows_synced=7, got %v", got)
	}
}

// An idle commit cycle confirms the source position without writing rows; it
// must still advance last_synced so a staleness alarm does not misfire.
func TestRecordSyncIdleCycleAdvancesTimestamp(t *testing.T) {
	resetState(t)

	beforeRows := testutil.ToFloat64(rowsSynced)
	RecordSync(map[string]int64{}, time.Unix(500, 0))

	if got := testutil.ToFloat64(rowsSynced) - beforeRows; got != 0 {
		t.Fatalf("expected no rows synced, got %v", got)
	}
	if got := testutil.ToFloat64(lastSyncedTS); got != 500 {
		t.Fatalf("expected last synced 500, got %v", got)
	}
}

func TestRecordSyncConcurrentWithScrape(t *testing.T) {
	resetState(t)

	beforeRows := testutil.ToFloat64(rowsSynced)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			RecordSync(map[string]int64{fmt.Sprintf("t%d", i%3): 1}, time.Unix(int64(i), 0))
		})
		wg.Go(func() {
			_, _ = registry.Gather()
		})
	}
	wg.Wait()

	if got := testutil.ToFloat64(rowsSynced) - beforeRows; got != 8 {
		t.Fatalf("expected 8 rows synced, got %v", got)
	}
}

func TestServeExposesMetrics(t *testing.T) {
	resetState(t)
	RecordSync(map[string]int64{"public.users": 3}, time.Unix(1234, 0))

	addr, stop, err := Serve("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	t.Cleanup(stop)

	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("could not reach metrics server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	text := string(body)

	if !strings.Contains(text, "ingestr_stream_rows_synced_total") {
		t.Fatalf("expected ingestr_stream_rows_synced_total in /metrics, got:\n%s", text)
	}
	if !strings.Contains(text, `ingestr_stream_table_rows_synced_total{table="public.users"}`) {
		t.Fatalf("expected per-table series in /metrics, got:\n%s", text)
	}

	// The dedicated registry must not carry the default registry's Go runtime or
	// process collectors, so scrapes never trigger runtime.ReadMemStats.
	for _, builtin := range []string{"go_goroutines", "go_memstats_", "process_"} {
		if strings.Contains(text, builtin) {
			t.Fatalf("expected %q to be absent from /metrics", builtin)
		}
	}
}

func TestServeStopDoesNotHang(t *testing.T) {
	_, stop, err := Serve("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop() hung; the CLI would not exit")
	}
}

func TestServeRejectsBadAddress(t *testing.T) {
	if _, _, err := Serve("not-an-address"); err == nil {
		t.Fatal("expected Serve to fail fast on an unusable address")
	}
}
