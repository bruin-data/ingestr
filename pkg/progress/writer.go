package progress

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bruin-data/gong/internal/config"
)

type WriterLogDisplay struct {
	writer   io.Writer
	interval time.Duration
	ticker   *time.Ticker
	done     chan struct{}
}

func NewWriterLogDisplay(w io.Writer) Display {
	return &WriterLogDisplay{
		writer:   w,
		interval: 1 * time.Second,
		done:     make(chan struct{}),
	}
}

func (d *WriterLogDisplay) Start(ctx context.Context, collector *MetricsCollector) {
	d.ticker = time.NewTicker(d.interval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.done:
				return
			case <-d.ticker.C:
				metrics := collector.Snapshot()
				d.logProgress(metrics)
			}
		}
	}()

	config.Debug("[PROGRESS] Writer display started")
}

func (d *WriterLogDisplay) Stop(metrics Metrics) {
	if d.ticker != nil {
		d.ticker.Stop()
	}
	close(d.done)

	d.logProgress(metrics)
	d.printSummary(metrics)

	config.Debug("[PROGRESS] Writer display stopped")
}

func (d *WriterLogDisplay) logProgress(m Metrics) {
	timestamp := time.Now().Format("15:04:05")
	_, _ = fmt.Fprintf(
		d.writer, "[PROGRESS] %s | Rows: %s | Batches: %d | Rate: %.0f rows/s\n",
		timestamp,
		formatNumber(m.TotalRows),
		m.TotalBatches,
		m.CurrentRowsPerSec,
	)
}

func (d *WriterLogDisplay) printSummary(m Metrics) {
	_, _ = fmt.Fprintf(d.writer, "\n=== Ingestion Summary ===\n")
	_, _ = fmt.Fprintf(d.writer, "Total Rows: %s\n", formatNumber(m.TotalRows))
	_, _ = fmt.Fprintf(d.writer, "Total Batches: %d\n", m.TotalBatches)
	_, _ = fmt.Fprintf(d.writer, "Duration: %s\n", formatDuration(m.Duration()))
	_, _ = fmt.Fprintf(d.writer, "Avg Throughput: %s rows/s\n", formatNumber(int64(m.AverageRowsPerSec())))
}
