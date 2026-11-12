package memlimit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailablePrefersTheSmallerCgroupLimit(t *testing.T) {
	files := map[string]string{"/proc/meminfo": "MemTotal:       16384000 kB\nMemFree: 1 kB\n"}
	read := func(path string) []byte { return []byte(files[path]) }
	require.EqualValues(t, int64(16384000)<<10, available(read))

	files["/sys/fs/cgroup/memory.max"] = "max\n"
	require.EqualValues(t, int64(16384000)<<10, available(read), "an unlimited cgroup falls back to physical memory")

	files["/sys/fs/cgroup/memory.max"] = "4294967296\n"
	require.EqualValues(t, 4294967296, available(read))

	delete(files, "/sys/fs/cgroup/memory.max")
	files["/sys/fs/cgroup/memory/memory.limit_in_bytes"] = "9223372036854771712\n"
	require.EqualValues(t, int64(16384000)<<10, available(read), "cgroup v1's unlimited value is larger than physical memory")

	require.Zero(t, available(func(string) []byte { return nil }))
}

func TestSetDefaultRespectsGOMEMLIMIT(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	require.Zero(t, SetDefault(0.75))
}
