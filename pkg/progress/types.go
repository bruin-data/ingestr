package progress

import (
	"context"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

// Tracker manages progress tracking for data ingestion.
// It wraps channels to intercept batches and collects metrics without modifying source/destination code.
type Tracker interface {
	// Wrap creates a transparent wrapper around a RecordBatchResult channel.
	// The wrapper intercepts batches, records metrics, and forwards batches unchanged.
	Wrap(ch <-chan source.RecordBatchResult) <-chan source.RecordBatchResult

	// Start begins progress tracking and display updates.
	Start(ctx context.Context)

	// Stop halts progress tracking and displays the final summary.
	Stop()

	// GetMetrics returns the current metrics snapshot.
	GetMetrics() Metrics
}

// Display handles the visual representation of progress.
// Implementations include interactive (spinner) and log-based displays.
type Display interface {
	// Start begins displaying progress updates from the collector.
	// Updates are throttled based on the display implementation (500ms interactive, 1s log).
	Start(ctx context.Context, collector *MetricsCollector)

	// Stop halts display updates and shows the final summary.
	Stop(metrics Metrics)
}

// Metrics contains all progress tracking data collected during ingestion.
type Metrics struct {
	// TotalRows is the total number of rows processed across all batches
	TotalRows int64

	// TotalBatches is the total number of batches processed
	TotalBatches int64

	// StartTime marks when ingestion started
	StartTime time.Time

	// CurrentRowsPerSec is the current throughput in rows per second
	CurrentRowsPerSec float64

	// CPUPercent is the current CPU usage percentage of the process
	CPUPercent float64

	// MemoryMB is the current memory usage in megabytes
	MemoryMB float64

	// PeakMemoryMB is the peak memory usage in megabytes (for final summary)
	PeakMemoryMB float64
}

// Duration returns the elapsed time since ingestion started.
func (m Metrics) Duration() time.Duration {
	return time.Since(m.StartTime)
}

// AverageRowsPerSec calculates the average throughput over the entire ingestion.
func (m Metrics) AverageRowsPerSec() float64 {
	duration := m.Duration().Seconds()
	if duration == 0 {
		return 0
	}
	return float64(m.TotalRows) / duration
}
