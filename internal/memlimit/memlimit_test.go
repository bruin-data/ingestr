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

	files["/sys/fs/cgroup/memory/memory.limit_in_bytes"] = "2147483648\n"
	require.EqualValues(t, 2147483648, available(read))

	delete(files, "/sys/fs/cgroup/memory.max")
	files["/sys/fs/cgroup/memory/memory.limit_in_bytes"] = "9223372036854771712\n"
	require.EqualValues(t, int64(16384000)<<10, available(read), "cgroup v1's unlimited value is larger than physical memory")

	require.Zero(t, available(func(string) []byte { return nil }))
}

func TestAvailableResolvesCgroupHierarchy(t *testing.T) {
	const physical = int64(16 << 30)
	tests := []struct {
		name      string
		cgroup    string
		mountinfo string
		files     map[string]string
		want      int64
	}{
		{
			name:      "v2 nested process limit",
			cgroup:    "0::/system.slice/ingestr.service\n",
			mountinfo: "29 23 0:26 / /sys/fs/cgroup rw,nosuid shared:4 - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/sys/fs/cgroup/memory.max":                              "max\n",
				"/sys/fs/cgroup/system.slice/memory.max":                 "8589934592\n",
				"/sys/fs/cgroup/system.slice/ingestr.service/memory.max": "4294967296\n",
			},
			want: 4 << 30,
		},
		{
			name:      "v2 tighter parent limit",
			cgroup:    "0::/jobs/ingestr\n",
			mountinfo: "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/sys/fs/cgroup/jobs/memory.max":         "2147483648\n",
				"/sys/fs/cgroup/jobs/ingestr/memory.max": "4294967296\n",
			},
			want: 2 << 30,
		},
		{
			name:      "v2 unlimited child inherits parent limit",
			cgroup:    "0::/jobs/ingestr\n",
			mountinfo: "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/sys/fs/cgroup/jobs/memory.max":         "2147483648\n",
				"/sys/fs/cgroup/jobs/ingestr/memory.max": "max\n",
			},
			want: 2 << 30,
		},
		{
			name:      "v1 memory controller in a combined mount",
			cgroup:    "7:cpu,cpuacct:/other\n8:memory,hugetlb:/jobs/ingestr\n",
			mountinfo: "30 23 0:27 / /sys/fs/cgroup/cpu rw - cgroup cgroup rw,cpu,cpuacct\n31 23 0:28 / /custom/memory rw - cgroup cgroup rw,memory,hugetlb\n",
			files: map[string]string{
				"/sys/fs/cgroup/cpu/other/memory.limit_in_bytes":    "1\n",
				"/custom/memory/memory.limit_in_bytes":              "9223372036854771712\n",
				"/custom/memory/jobs/memory.limit_in_bytes":         "4294967296\n",
				"/custom/memory/jobs/ingestr/memory.limit_in_bytes": "2147483648\n",
			},
			want: 2 << 30,
		},
		{
			name:      "v1 tighter parent limit",
			cgroup:    "8:memory:/jobs/ingestr\n",
			mountinfo: "31 23 0:28 / /sys/fs/cgroup/memory rw - cgroup cgroup rw,memory\n",
			files: map[string]string{
				"/sys/fs/cgroup/memory/jobs/memory.limit_in_bytes":         "2147483648\n",
				"/sys/fs/cgroup/memory/jobs/ingestr/memory.limit_in_bytes": "4294967296\n",
			},
			want: 2 << 30,
		},
		{
			name:      "bind-mounted subtree stops at mount root",
			cgroup:    "0::/containers/job/worker\n",
			mountinfo: "29 23 0:26 /containers/job /custom/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/custom/cgroup/worker/memory.max": "4294967296\n",
				"/custom/cgroup/memory.max":        "2147483648\n",
				"/custom/memory.max":               "1\n",
				"/sys/fs/cgroup/memory.max":        "1\n",
			},
			want: 2 << 30,
		},
		{
			name:      "cgroup namespace root",
			cgroup:    "0::/\n",
			mountinfo: "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/sys/fs/cgroup/memory.max": "2147483648\n",
			},
			want: 2 << 30,
		},
		{
			name:      "escaped mount paths and colon in group name",
			cgroup:    "0::/job group/worker:1\n",
			mountinfo: "29 23 0:26 /job\\040group /custom\\040cgroup\\134mount rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/custom cgroup\\mount/worker:1/memory.max": "2147483648\n",
			},
			want: 2 << 30,
		},
		{
			name:      "ignore mounts outside the process hierarchy",
			cgroup:    "0::/jobs/worker\n",
			mountinfo: "29 23 0:26 /job /other rw - cgroup2 cgroup rw\n30 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/other/memory.max":                     "1\n",
				"/sys/fs/cgroup/jobs/memory.max":        "2147483648\n",
				"/sys/fs/cgroup/memory.max":             "max\n",
				"/other/s/worker/memory.max":            "1\n",
				"/sys/fs/cgroup/jobs/worker/memory.max": "max\n",
			},
			want: 2 << 30,
		},
		{
			name:      "missing and invalid limits fall back to physical memory",
			cgroup:    "0::/jobs/worker\n",
			mountinfo: "29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/sys/fs/cgroup/jobs/worker/memory.max": "invalid\n",
				"/sys/fs/cgroup/jobs/memory.max":        "-1\n",
				"/sys/fs/cgroup/memory.max":             "max\n",
			},
			want: physical,
		},
		{
			name:      "malformed metadata",
			cgroup:    "invalid\n8:cpu:/jobs\n0::relative\n",
			mountinfo: "invalid\n29 - cgroup2 cgroup rw\n29 23 0:26 / /custom rw - cgroup2 cgroup rw\n",
			files: map[string]string{
				"/custom/relative/memory.max": "1\n",
			},
			want: physical,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := func(path string) []byte {
				switch path {
				case "/proc/meminfo":
					return []byte("MemTotal: 16777216 kB\n")
				case "/proc/self/cgroup":
					return []byte(tt.cgroup)
				case "/proc/self/mountinfo":
					return []byte(tt.mountinfo)
				default:
					return []byte(tt.files[path])
				}
			}
			require.Equal(t, tt.want, available(read))
		})
	}
}

func TestSetDefaultRespectsGOMEMLIMIT(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	require.Zero(t, SetDefault(0.75))
}
