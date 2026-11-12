// Package memlimit gives the Go runtime a soft memory limit sized to the memory
// available to the process, so large in-flight batches make the garbage
// collector work harder instead of letting the heap double past the machine.
package memlimit

import (
	"bufio"
	"bytes"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// SetDefault applies fraction of the process's available memory (its cgroup
// limit, or physical memory) as the soft limit unless GOMEMLIMIT is set. It
// returns the limit applied, or 0 when it left the runtime unchanged.
func SetDefault(fraction float64) int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	total := available(readFile)
	if total <= 0 {
		return 0
	}
	limit := int64(float64(total) * fraction)
	debug.SetMemoryLimit(limit)
	return limit
}

func readFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func available(read func(string) []byte) int64 {
	physical := memTotal(read("/proc/meminfo"))
	for _, path := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		limit, err := strconv.ParseInt(strings.TrimSpace(string(read(path))), 10, 64)
		if err == nil && limit > 0 && (physical == 0 || limit < physical) {
			return limit
		}
	}
	return physical
}

func memTotal(meminfo []byte) int64 {
	scanner := bufio.NewScanner(bytes.NewReader(meminfo))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kb << 10
			}
		}
	}
	return 0
}
