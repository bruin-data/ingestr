package progress

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// LogDisplay provides a simple log-based progress display.
// Prints periodic progress updates to stdout without terminal manipulation.
type LogDisplay struct {
	interval time.Duration
	ticker   *time.Ticker
	done     chan struct{}
}

// NewLogDisplay creates a new log-based display with 3 second update interval.
func NewLogDisplay() Display {
	return &LogDisplay{
		interval: 3 * time.Second,
		done:     make(chan struct{}),
	}
}

// Start begins displaying progress updates every 3 seconds.
func (d *LogDisplay) Start(ctx context.Context, collector *MetricsCollector) {
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

	config.Debug("[PROGRESS] Log display started")
}

// Stop halts the display and shows the final summary.
func (d *LogDisplay) Stop(metrics Metrics) {
	if d.ticker != nil {
		d.ticker.Stop()
	}
	close(d.done)

	// Final progress log
	d.logProgress(metrics)

	// Print final summary
	d.printSummary(metrics)

	config.Debug("[PROGRESS] Log display stopped")
}

// logProgress prints a single progress line with current metrics.
func (d *LogDisplay) logProgress(m Metrics) {
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf(
		"[PROGRESS] %s | Rows: %s | Batches: %d | Rate: %.0f rows/s | CPU: %.1f%% | Mem: %.0f MB\n",
		timestamp,
		formatNumber(m.TotalRows),
		m.TotalBatches,
		m.CurrentRowsPerSec,
		m.CPUPercent,
		m.MemoryMB,
	)
}

// printSummary displays the final ingestion summary.
func (d *LogDisplay) printSummary(m Metrics) {
	fmt.Println() // Empty line before table

	// Create table with right-aligned values column
	cfg := tablewriter.NewConfigBuilder().
		Row().
		Alignment().
		WithPerColumn([]tw.Align{tw.AlignLeft, tw.AlignRight}).
		Build(). // Returns RowConfigBuilder
		Build()  // Returns Config

	table := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(cfg))
	table.Header("Metric", "Value")
	_ = table.Append("Total Rows", formatNumber(m.TotalRows))
	_ = table.Append("Total Batches", fmt.Sprintf("%d", m.TotalBatches))
	_ = table.Append("Duration", formatDuration(m.Duration()))
	_ = table.Append("Avg Throughput", fmt.Sprintf("%s rows/s", formatNumber(int64(m.AverageRowsPerSec()))))
	_ = table.Append("Peak Memory", fmt.Sprintf("%.0f MB", m.PeakMemoryMB))
	_ = table.Render()
}
