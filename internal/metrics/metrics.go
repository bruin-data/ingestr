// Package metrics exposes streaming ingestion metrics over HTTP via expvar.
//
// It is opt-in: nothing is served unless the caller invokes Serve. The exported
// vars are published once at init because expvar panics on duplicate publish.
package metrics

import (
	"expvar"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

// varPrefix namespaces every variable ingestr publishes. The handler filters on
// it so the endpoint exposes only ingestr's own vars, never expvar's built-in
// cmdline/memstats, which the standard library injects into the shared registry
// at package-import time.
const varPrefix = "ingestr_"

// reporter holds the active source's lag reporter. It is a package global
// because expvar's registry is process-global and ingestr runs one stream per
// process; a hypothetical concurrent pipeline would overwrite it.
var reporter atomic.Pointer[source.LagReporter]

var (
	rowsSynced   = expvar.NewInt("ingestr_stream_rows_synced")
	flushCycles  = expvar.NewInt("ingestr_stream_flush_cycles")
	lastSyncedTS = expvar.NewInt("ingestr_stream_last_synced_unix")
)

var (
	tablesMu sync.Mutex
	tables   = map[string]*tableStat{}
)

type tableStat struct {
	RowsSynced     int64 `json:"rows_synced"`
	LastFlushRows  int64 `json:"last_flush_rows"`
	LastSyncedUnix int64 `json:"last_synced_unix"`
}

type lagVars struct {
	Streaming       bool     `json:"streaming"`
	Source          string   `json:"source,omitempty"`
	BytesBehind     *uint64  `json:"bytes_behind,omitempty"`
	SecondsBehind   *float64 `json:"seconds_behind,omitempty"`
	ServerPosition  string   `json:"server_position,omitempty"`
	DurablePosition string   `json:"durable_position,omitempty"`
	CaughtUp        bool     `json:"caught_up"`
	UpdatedAtUnix   int64    `json:"updated_at_unix,omitempty"`
}

func init() {
	expvar.Publish("ingestr_replication", expvar.Func(replicationVars))
	expvar.Publish("ingestr_stream_tables", expvar.Func(tableVars))
}

func replicationVars() any {
	rp := reporter.Load()
	if rp == nil {
		return lagVars{}
	}
	s, ok := (*rp).ReplicationLag()
	if !ok {
		return lagVars{}
	}
	v := lagVars{
		Streaming:       true,
		Source:          s.Source,
		BytesBehind:     s.BytesBehind,
		SecondsBehind:   s.SecondsBehind,
		ServerPosition:  s.ServerPosition,
		DurablePosition: s.DurablePosition,
		CaughtUp:        s.CaughtUp,
	}
	if !s.UpdatedAt.IsZero() {
		v.UpdatedAtUnix = s.UpdatedAt.Unix()
	}
	return v
}

// tableVars returns a copy: expvar encodes the returned value after this
// function has released the lock.
func tableVars() any {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	out := make(map[string]tableStat, len(tables))
	for name, st := range tables {
		out[name] = *st
	}
	return out
}

// SetLagReporter installs the source whose lag is published. Pass nil to clear.
func SetLagReporter(r source.LagReporter) {
	if r == nil {
		reporter.Store(nil)
		return
	}
	reporter.Store(&r)
}

// RecordSync accounts one successfully committed flush cycle. Callers must
// invoke it only after the source position is committed, so the counters mean
// "durable in the destination" rather than merely "written".
func RecordSync(perTable map[string]int64, at time.Time) {
	unix := at.Unix()
	var total int64

	tablesMu.Lock()
	for name, rows := range perTable {
		st, ok := tables[name]
		if !ok {
			st = &tableStat{}
			tables[name] = st
		}
		st.RowsSynced += rows
		st.LastFlushRows = rows
		st.LastSyncedUnix = unix
		total += rows
	}
	tablesMu.Unlock()

	rowsSynced.Add(total)
	flushCycles.Add(1)
	lastSyncedTS.Set(unix)
}

// Serve starts an HTTP server exposing expvar at /debug/vars. The listener is
// bound synchronously so an unusable address fails fast, and the resolved
// address is returned so a port of 0 can be used. The returned stop closes the
// server immediately rather than draining: this is a read-only endpoint, and an
// immediate close guarantees the CLI never hangs on exit.
func Serve(addr string) (boundAddr string, stop func(), err error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}

	// A dedicated mux, never http.DefaultServeMux: importing expvar registers
	// /debug/vars there, and that must not leak into the web UI server.
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/vars", handler)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}

// handler serves ingestr's own vars as a JSON object, mirroring the format of
// expvar.Handler but skipping every var outside the ingestr_ namespace. This
// keeps expvar's cmdline/memstats built-ins out of the response; memstats in
// particular calls runtime.ReadMemStats on each scrape, which is needless work
// and a stop-the-world pause for data no ingestr consumer asked for.
func handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, "{\n")
	first := true
	expvar.Do(func(kv expvar.KeyValue) {
		if !strings.HasPrefix(kv.Key, varPrefix) {
			return
		}
		if !first {
			fmt.Fprintf(w, ",\n")
		}
		first = false
		fmt.Fprintf(w, "%q: %s", kv.Key, kv.Value)
	})
	fmt.Fprintf(w, "\n}\n")
}
