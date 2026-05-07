package progress

import (
	"sync/atomic"
	"time"
)

// MetricsCollector provides thread-safe metrics collection using atomic operations.
// Designed to work with parallel workers without locks.
type MetricsCollector struct {
	// Atomic counters for concurrent access
	totalRows    atomic.Int64
	totalBatches atomic.Int64

	// Start time for duration calculations
	startTime time.Time

	// Resource monitoring
	resourceMon *ResourceMonitor

	// Peak memory tracking (in bytes, converted to MB for display)
	peakMemory atomic.Uint64
}

// NewMetricsCollector creates a new metrics collector with resource monitoring.
func NewMetricsCollector() (*MetricsCollector, error) {
	resourceMon, err := NewResourceMonitor()
	if err != nil {
		return nil, err
	}

	collector := &MetricsCollector{
		startTime:   time.Now(),
		resourceMon: resourceMon,
	}

	return collector, nil
}

// RecordBatch records a processed batch with the given number of rows.
// This method is lock-free and safe for concurrent calls from multiple workers.
func (m *MetricsCollector) RecordBatch(numRows int64) {
	m.totalRows.Add(numRows)
	m.totalBatches.Add(1)
}

// Snapshot captures the current metrics state.
// This includes updating resource metrics and calculating throughput.
func (m *MetricsCollector) Snapshot() Metrics {
	// Update resource metrics (CPU, memory)
	cpuPercent, memoryMB := m.updateResourceMetrics()

	// Get current counters
	totalRows := m.totalRows.Load()
	totalBatches := m.totalBatches.Load()
	peakMemoryBytes := m.peakMemory.Load()

	// Calculate run-level average throughput (total rows / total elapsed time)
	elapsed := time.Since(m.startTime).Seconds()
	var currentRowsPerSec float64
	if elapsed > 0 {
		currentRowsPerSec = float64(totalRows) / elapsed
	}

	return Metrics{
		TotalRows:         totalRows,
		TotalBatches:      totalBatches,
		StartTime:         m.startTime,
		CurrentRowsPerSec: currentRowsPerSec,
		CPUPercent:        cpuPercent,
		MemoryMB:          memoryMB,
		PeakMemoryMB:      float64(peakMemoryBytes) / (1024 * 1024),
	}
}

// updateResourceMetrics updates CPU and memory metrics and tracks peak memory.
// Returns current CPU percent and memory in MB.
func (m *MetricsCollector) updateResourceMetrics() (cpuPercent, memoryMB float64) {
	cpuPercent, memoryMB, err := m.resourceMon.Update()
	if err != nil {
		// If we can't get metrics, return zeros
		return 0, 0
	}

	// Track peak memory
	memoryBytes := uint64(memoryMB * 1024 * 1024)
	for {
		currentPeak := m.peakMemory.Load()
		if memoryBytes <= currentPeak {
			break
		}
		// Try to update peak (compare-and-swap)
		if m.peakMemory.CompareAndSwap(currentPeak, memoryBytes) {
			break
		}
		// If CAS failed, another goroutine updated it, retry
	}

	return cpuPercent, memoryMB
}
