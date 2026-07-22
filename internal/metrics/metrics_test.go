package metrics

import (
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

type fakeReporter struct {
	snap source.LagSnapshot
	ok   bool
}

func (f fakeReporter) ReplicationLag() (source.LagSnapshot, bool) { return f.snap, f.ok }

func scrapeReplication(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(expvar.Get("ingestr_replication").String()), &out); err != nil {
		t.Fatalf("failed to decode ingestr_replication: %v", err)
	}
	return out
}

func scrapeTables(t *testing.T) map[string]map[string]any {
	t.Helper()
	var out map[string]map[string]any
	if err := json.Unmarshal([]byte(expvar.Get("ingestr_stream_tables").String()), &out); err != nil {
		t.Fatalf("failed to decode ingestr_stream_tables: %v", err)
	}
	return out
}

// resetState clears the package globals so each test starts clean. expvar has
// no deregister, so the vars themselves are reused across tests.
func resetState(t *testing.T) {
	t.Helper()
	SetLagReporter(nil)
	tablesMu.Lock()
	tables = map[string]*tableStat{}
	tablesMu.Unlock()
	rowsSynced.Set(0)
	flushCycles.Set(0)
	lastSyncedTS.Set(0)
	t.Cleanup(func() { SetLagReporter(nil) })
}

func TestReplicationVarsWithoutReporter(t *testing.T) {
	resetState(t)

	got := scrapeReplication(t)
	if got["streaming"] != false {
		t.Fatalf("expected streaming=false with no reporter, got %v", got)
	}
	if _, ok := got["bytes_behind"]; ok {
		t.Fatalf("expected no bytes_behind with no reporter, got %v", got)
	}
}

func TestReplicationVarsReporterNotReady(t *testing.T) {
	resetState(t)
	SetLagReporter(fakeReporter{ok: false})

	if got := scrapeReplication(t); got["streaming"] != false {
		t.Fatalf("expected streaming=false when reporter is not ready, got %v", got)
	}
}

func TestReplicationVarsBytesBehind(t *testing.T) {
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

	got := scrapeReplication(t)
	if got["streaming"] != true {
		t.Fatalf("expected streaming=true, got %v", got)
	}
	if got["bytes_behind"] != float64(4096) {
		t.Fatalf("expected bytes_behind=4096, got %v", got["bytes_behind"])
	}
	if got["caught_up"] != false {
		t.Fatalf("expected caught_up=false, got %v", got["caught_up"])
	}
	if got["server_position"] != "0/1A2B3C0" {
		t.Fatalf("unexpected server_position: %v", got["server_position"])
	}
	// seconds_behind is not applicable to Postgres and must be omitted rather
	// than reported as a misleading zero.
	if _, ok := got["seconds_behind"]; ok {
		t.Fatalf("expected seconds_behind to be omitted, got %v", got)
	}
}

func TestReplicationVarsSecondsBehindOnly(t *testing.T) {
	resetState(t)
	secs := 12.0
	SetLagReporter(fakeReporter{
		ok:   true,
		snap: source.LagSnapshot{Source: "mongodb", SecondsBehind: &secs},
	})

	got := scrapeReplication(t)
	if got["seconds_behind"] != float64(12) {
		t.Fatalf("expected seconds_behind=12, got %v", got["seconds_behind"])
	}
	if _, ok := got["bytes_behind"]; ok {
		t.Fatalf("expected bytes_behind to be omitted for mongodb, got %v", got)
	}
}

func TestRecordSyncAccumulates(t *testing.T) {
	resetState(t)

	RecordSync(map[string]int64{"public.users": 100, "public.orders": 50}, time.Unix(1000, 0))
	RecordSync(map[string]int64{"public.users": 25, "public.items": 7}, time.Unix(2000, 0))

	if got := rowsSynced.Value(); got != 182 {
		t.Fatalf("expected 182 rows synced in total, got %d", got)
	}
	if got := flushCycles.Value(); got != 2 {
		t.Fatalf("expected 2 flush cycles, got %d", got)
	}
	if got := lastSyncedTS.Value(); got != 2000 {
		t.Fatalf("expected last synced 2000, got %d", got)
	}

	tbl := scrapeTables(t)

	// rows_synced accumulates across cycles; last_flush_rows is replaced.
	if tbl["public.users"]["rows_synced"] != float64(125) {
		t.Fatalf("expected users rows_synced=125, got %v", tbl["public.users"]["rows_synced"])
	}
	if tbl["public.users"]["last_flush_rows"] != float64(25) {
		t.Fatalf("expected users last_flush_rows=25, got %v", tbl["public.users"]["last_flush_rows"])
	}

	// A table absent from cycle 2 keeps its totals and its older timestamp.
	if tbl["public.orders"]["rows_synced"] != float64(50) {
		t.Fatalf("expected orders rows_synced=50, got %v", tbl["public.orders"]["rows_synced"])
	}
	if tbl["public.orders"]["last_synced_unix"] != float64(1000) {
		t.Fatalf("expected orders to keep last_synced_unix=1000, got %v", tbl["public.orders"]["last_synced_unix"])
	}

	// A table first seen in cycle 2 appears.
	if tbl["public.items"]["rows_synced"] != float64(7) {
		t.Fatalf("expected items rows_synced=7, got %v", tbl["public.items"]["rows_synced"])
	}
}

// An idle commit cycle confirms the source position without writing rows; it
// must still advance last_synced so a staleness alarm does not misfire.
func TestRecordSyncIdleCycleAdvancesTimestamp(t *testing.T) {
	resetState(t)

	RecordSync(map[string]int64{}, time.Unix(500, 0))

	if got := rowsSynced.Value(); got != 0 {
		t.Fatalf("expected no rows synced, got %d", got)
	}
	if got := lastSyncedTS.Value(); got != 500 {
		t.Fatalf("expected last synced 500, got %d", got)
	}
}

func TestRecordSyncConcurrentWithScrape(t *testing.T) {
	resetState(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			RecordSync(map[string]int64{fmt.Sprintf("t%d", i%3): 1}, time.Unix(int64(i), 0))
		})
		wg.Go(func() {
			_ = expvar.Get("ingestr_stream_tables").String()
		})
	}
	wg.Wait()

	if got := rowsSynced.Value(); got != 8 {
		t.Fatalf("expected 8 rows synced, got %d", got)
	}
}

func TestServeExposesDebugVars(t *testing.T) {
	resetState(t)
	RecordSync(map[string]int64{"public.users": 3}, time.Unix(1234, 0))

	addr, stop, err := Serve("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Serve failed: %v", err)
	}
	t.Cleanup(stop)

	resp, err := http.Get("http://" + addr + "/debug/vars")
	if err != nil {
		t.Fatalf("could not reach metrics server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("expvar output is not valid JSON: %v", err)
	}
	if _, ok := out["ingestr_stream_rows_synced"]; !ok {
		t.Fatalf("expected ingestr_stream_rows_synced in /debug/vars, got keys %v", out)
	}

	// expvar injects cmdline and memstats into the shared registry; the handler
	// must filter them out so scrapes never trigger runtime.ReadMemStats or leak
	// the process arguments.
	for _, builtin := range []string{"memstats", "cmdline"} {
		if _, ok := out[builtin]; ok {
			t.Fatalf("expected %q to be excluded from /debug/vars, got keys %v", builtin, out)
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
