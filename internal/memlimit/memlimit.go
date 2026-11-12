// Package memlimit gives the Go runtime a soft memory limit sized to the memory
// available to the process, so large in-flight batches make the garbage
// collector work harder instead of letting the heap double past the machine.
package memlimit

import (
	"bufio"
	"bytes"
	"os"
	"path"
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
	total := memTotal(read("/proc/meminfo"))
	paths := cgroupLimitPaths(read("/proc/self/cgroup"), read("/proc/self/mountinfo"))
	if len(paths) == 0 {
		paths = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	}
	for _, path := range paths {
		limit, err := strconv.ParseInt(strings.TrimSpace(string(read(path))), 10, 64)
		if err == nil && limit > 0 && (total == 0 || limit < total) {
			total = limit
		}
	}
	return total
}

func cgroupLimitPaths(cgroup, mountinfo []byte) []string {
	var unified, memory string
	scanner := bufio.NewScanner(bytes.NewReader(cgroup))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), ":", 3)
		if len(fields) != 3 {
			continue
		}
		if fields[0] == "0" && fields[1] == "" {
			unified = fields[2]
		}
		if hasOption(fields[1], "memory") {
			memory = fields[2]
		}
	}

	var paths []string
	scanner = bufio.NewScanner(bytes.NewReader(mountinfo))
	for scanner.Scan() {
		mount, filesystem, ok := strings.Cut(scanner.Text(), " - ")
		if !ok {
			continue
		}
		fields, fs := strings.Fields(mount), strings.Fields(filesystem)
		if len(fields) < 6 || len(fs) < 3 {
			continue
		}
		var group, filename string
		switch {
		case fs[0] == "cgroup2":
			group, filename = unified, "memory.max"
		case fs[0] == "cgroup" && hasOption(fs[2], "memory"):
			group, filename = memory, "memory.limit_in_bytes"
		default:
			continue
		}
		root, mountpoint := unescapeMountPath(fields[3]), unescapeMountPath(fields[4])
		if !cleanAbsolutePath(group) || !cleanAbsolutePath(root) || !cleanAbsolutePath(mountpoint) {
			continue
		}
		if root != "/" && group != root && !strings.HasPrefix(group, root+"/") {
			continue
		}
		dir := path.Join(mountpoint, strings.TrimPrefix(group, root))
		// Ancestor limits apply to the entire subtree, including this process.
		for {
			paths = append(paths, path.Join(dir, filename))
			if dir == mountpoint {
				break
			}
			dir = path.Dir(dir)
		}
	}
	return paths
}

func hasOption(options, option string) bool {
	return strings.Contains(","+options+",", ","+option+",")
}

func cleanAbsolutePath(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value
}

func unescapeMountPath(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
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
