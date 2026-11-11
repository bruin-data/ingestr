package metrics

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/prometheus/client_golang/prometheus"
)

var walReporter atomic.Pointer[source.WALRetentionReporter]

var (
	descSlotRetainedWAL = prometheus.NewDesc(
		"ingestr_replication_slot_retained_wal_bytes",
		"WAL bytes between the server WAL head and the slot restart position.",
		[]string{"source", "slot"}, nil,
	)
	descSlotSafeWAL = prometheus.NewDesc(
		"ingestr_replication_slot_safe_wal_bytes",
		"Additional WAL bytes that can be written before the slot is at risk of losing required WAL; absent when undefined.",
		[]string{"source", "slot"}, nil,
	)
	descSlotMaxWALKeep = prometheus.NewDesc(
		"ingestr_replication_slot_max_wal_keep_bytes",
		"Configured max_slot_wal_keep_size in bytes; -1 means unlimited.",
		[]string{"source", "slot"}, nil,
	)
	descSlotWALStatus = prometheus.NewDesc(
		"ingestr_replication_slot_wal_status",
		"1 for the slot's current PostgreSQL WAL status.",
		[]string{"source", "slot", "status"}, nil,
	)
	descSlotCollectionSuccess = prometheus.NewDesc(
		"ingestr_replication_slot_collection_success",
		"1 when the slot's WAL retention could be read on this scrape, 0 otherwise.",
		[]string{"source", "slot"}, nil,
	)
)

type walRetentionCollector struct{}

func (walRetentionCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descSlotRetainedWAL
	ch <- descSlotSafeWAL
	ch <- descSlotMaxWALKeep
	ch <- descSlotWALStatus
	ch <- descSlotCollectionSuccess
}

func (walRetentionCollector) Collect(ch chan<- prometheus.Metric) {
	rp := walReporter.Load()
	if rp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := (*rp).WALRetention(ctx)
	if s.Slot == "" {
		return
	}
	ch <- prometheus.MustNewConstMetric(descSlotCollectionSuccess, prometheus.GaugeValue, boolToFloat(err == nil), s.Source, s.Slot)
	if err != nil {
		return
	}
	if s.RetainedBytes != nil {
		ch <- prometheus.MustNewConstMetric(descSlotRetainedWAL, prometheus.GaugeValue, *s.RetainedBytes, s.Source, s.Slot)
	}
	if s.SafeBytes != nil {
		ch <- prometheus.MustNewConstMetric(descSlotSafeWAL, prometheus.GaugeValue, float64(*s.SafeBytes), s.Source, s.Slot)
	}
	if s.MaxKeepBytes != nil {
		ch <- prometheus.MustNewConstMetric(descSlotMaxWALKeep, prometheus.GaugeValue, float64(*s.MaxKeepBytes), s.Source, s.Slot)
	}
	if s.Status != "" {
		ch <- prometheus.MustNewConstMetric(descSlotWALStatus, prometheus.GaugeValue, 1, s.Source, s.Slot, s.Status)
	}
}

// SetWALRetentionReporter installs the source queried on each metrics scrape.
// Pass nil to clear it. Queries have a three-second deadline.
func SetWALRetentionReporter(r source.WALRetentionReporter) {
	if r == nil {
		walReporter.Store(nil)
		return
	}
	walReporter.Store(&r)
}
