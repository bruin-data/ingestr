// Package metrics exposes streaming ingestion metrics over HTTP in the
// Prometheus text exposition format.
//
// It is opt-in: nothing is served unless the caller invokes Serve. Metrics are
// registered on a dedicated registry (never the default) so scrapes expose only
// ingestr's own metrics, never the Go runtime or process collectors the default
// registry ships with; those would add needless work (a stop-the-world
// runtime.ReadMemStats) and noise no ingestr consumer asked for.
package metrics

import (
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry backs every metric ingestr publishes. It is process-global because
// ingestr runs one stream per process; a hypothetical concurrent pipeline would
// share it.
var registry = prometheus.NewRegistry()

// reporter holds the active source's lag reporter. The replication collector
// reads it fresh on each scrape, so lag reflects the moment of the scrape.
var reporter atomic.Pointer[source.LagReporter]

var (
	rowsSynced = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ingestr_stream_rows_synced_total",
		Help: "Total rows durably synced to the destination and acknowledged to the source.",
	})
	flushCycles = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ingestr_stream_flush_cycles_total",
		Help: "Total committed flush cycles.",
	})
	lastSyncedTS = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ingestr_stream_last_synced_timestamp_seconds",
		Help: "Unix timestamp of the last committed flush cycle.",
	})
	tableRowsSynced = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ingestr_stream_table_rows_synced_total",
		Help: "Total rows durably synced, per table.",
	}, []string{"table"})
	tableLastFlushRows = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ingestr_stream_table_last_flush_rows",
		Help: "Rows written for a table in its most recent flush cycle.",
	}, []string{"table"})
	tableLastSyncedTS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ingestr_stream_table_last_synced_timestamp_seconds",
		Help: "Unix timestamp of the most recent flush cycle that included a table.",
	}, []string{"table"})
)

func init() {
	registry.MustRegister(
		rowsSynced,
		flushCycles,
		lastSyncedTS,
		tableRowsSynced,
		tableLastFlushRows,
		tableLastSyncedTS,
		replicationCollector{},
	)
}

var (
	descReplStreaming = prometheus.NewDesc(
		"ingestr_replication_streaming",
		"1 when the source is actively streaming replication.",
		[]string{"source"}, nil,
	)
	descReplCaughtUp = prometheus.NewDesc(
		"ingestr_replication_caught_up",
		"1 when the durable position has caught up to the server position, 0 otherwise.",
		[]string{"source"}, nil,
	)
	descReplBytesBehind = prometheus.NewDesc(
		"ingestr_replication_bytes_behind",
		"Bytes the durable position lags behind the server write position.",
		[]string{"source"}, nil,
	)
	descReplSecondsBehind = prometheus.NewDesc(
		"ingestr_replication_seconds_behind",
		"Seconds the durable position lags behind the server.",
		[]string{"source"}, nil,
	)
	descReplUpdatedAt = prometheus.NewDesc(
		"ingestr_replication_updated_at_timestamp_seconds",
		"Unix timestamp when the lag snapshot was taken.",
		[]string{"source"}, nil,
	)
)

// replicationCollector reads the active lag reporter on every scrape. Using a
// collector rather than gauges lets it omit dimensions the engine cannot
// express: Postgres has no per-LSN timestamp (no seconds_behind), and
// MongoDB/SQL Server logs have no byte offset (no bytes_behind). Sources that do
// not report lag (e.g. MySQL/Vitess CDC) emit nothing at all.
type replicationCollector struct{}

func (replicationCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descReplStreaming
	ch <- descReplCaughtUp
	ch <- descReplBytesBehind
	ch <- descReplSecondsBehind
	ch <- descReplUpdatedAt
}

func (replicationCollector) Collect(ch chan<- prometheus.Metric) {
	rp := reporter.Load()
	if rp == nil {
		return
	}
	s, ok := (*rp).ReplicationLag()
	if !ok {
		return
	}
	ch <- prometheus.MustNewConstMetric(descReplStreaming, prometheus.GaugeValue, 1, s.Source)
	ch <- prometheus.MustNewConstMetric(descReplCaughtUp, prometheus.GaugeValue, boolToFloat(s.CaughtUp), s.Source)
	if s.BytesBehind != nil {
		ch <- prometheus.MustNewConstMetric(descReplBytesBehind, prometheus.GaugeValue, float64(*s.BytesBehind), s.Source)
	}
	if s.SecondsBehind != nil {
		ch <- prometheus.MustNewConstMetric(descReplSecondsBehind, prometheus.GaugeValue, *s.SecondsBehind, s.Source)
	}
	if !s.UpdatedAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(descReplUpdatedAt, prometheus.GaugeValue, float64(s.UpdatedAt.Unix()), s.Source)
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Gatherer returns the registry backing the streaming metrics so embedders and
// tests can gather ingestr's own metrics without going through HTTP.
func Gatherer() prometheus.Gatherer { return registry }

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
	unix := float64(at.Unix())
	var total int64

	for name, rows := range perTable {
		tableRowsSynced.WithLabelValues(name).Add(float64(rows))
		tableLastFlushRows.WithLabelValues(name).Set(float64(rows))
		tableLastSyncedTS.WithLabelValues(name).Set(unix)
		total += rows
	}

	rowsSynced.Add(float64(total))
	flushCycles.Inc()
	lastSyncedTS.Set(unix)
}

// Serve starts an HTTP server exposing the metrics at /metrics. The listener is
// bound synchronously so an unusable address fails fast, and the resolved
// address is returned so a port of 0 can be used. The returned stop closes the
// server immediately rather than draining: this is a read-only endpoint, and an
// immediate close guarantees the CLI never hangs on exit.
func Serve(addr string) (boundAddr string, stop func(), err error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}

	// A dedicated mux, never http.DefaultServeMux: a transitively-imported expvar
	// would register /debug/vars there, and that must not leak into this server.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}
