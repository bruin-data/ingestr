package progress

import (
	"context"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
)

// JSONDisplay emits progress and lifecycle events as structured JSON records via
// the output package. The start event is emitted separately by the pipeline
// banner (display.PrintSummary); this display owns the periodic progress events
// and the terminal end event.
type JSONDisplay struct {
	interval time.Duration
	ticker   *time.Ticker
	done     chan struct{}
}

// NewJSONDisplay creates a JSON progress display that emits a progress event
// every second. Output is routed through the output package, which must be in
// JSON mode for records to be written.
func NewJSONDisplay() Display {
	return &JSONDisplay{
		interval: 1 * time.Second,
		done:     make(chan struct{}),
	}
}

// Start begins emitting periodic progress events.
func (d *JSONDisplay) Start(ctx context.Context, collector *MetricsCollector) {
	d.ticker = time.NewTicker(d.interval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.done:
				return
			case <-d.ticker.C:
				d.emit(collector.Snapshot())
			}
		}
	}()

	config.Debug("[PROGRESS] JSON display started")
}

// Stop halts progress emission and writes the terminal end event. A non-nil err
// makes the end event report status "error".
func (d *JSONDisplay) Stop(metrics Metrics, err error) {
	if d.ticker != nil {
		d.ticker.Stop()
	}
	close(d.done)

	status := "success"
	if err != nil {
		status = "error"
	}
	output.EventEnd(status, metrics.TotalRows, metrics.TotalBatches, metrics.Duration().Seconds(), err)

	config.Debug("[PROGRESS] JSON display stopped")
}

// emit writes a single progress event. It is the deterministic seam exercised by
// tests directly, without the ticker.
func (d *JSONDisplay) emit(m Metrics) {
	output.EventProgress(m.TotalRows, m.TotalBatches, m.CurrentRowsPerSec, m.Duration().Seconds(), m.CPUPercent, m.MemoryMB)
}
