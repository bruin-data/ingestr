package progress

import (
	"fmt"
	"os"
	"sync"

	"github.com/shirou/gopsutil/v4/process"
)

// ResourceMonitor tracks CPU and memory usage of the current process.
// Uses gopsutil for cross-platform resource monitoring.
type ResourceMonitor struct {
	proc *process.Process
	mu   sync.Mutex
}

// NewResourceMonitor creates a new resource monitor for the current process.
func NewResourceMonitor() (*ResourceMonitor, error) {
	pid := os.Getpid()
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("failed to create process monitor: %w", err)
	}

	return &ResourceMonitor{
		proc: proc,
	}, nil
}

// Update retrieves current CPU and memory metrics.
// Returns CPU percentage and memory in MB.
func (r *ResourceMonitor) Update() (cpuPercent, memoryMB float64, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cpuPercent, err = r.proc.CPUPercent()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	// Get memory info (RSS - Resident Set Size)
	memInfo, err := r.proc.MemoryInfo()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get memory usage: %w", err)
	}

	// Convert bytes to MB
	memoryMB = float64(memInfo.RSS) / (1024 * 1024)

	return cpuPercent, memoryMB, nil
}
