#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 RESULT_DIR" >&2
  exit 2
fi

result_dir="$1"
runs="$(jq '.results[0].times | length' "$result_dir/hyperfine.json")"
last_timed_line="$((runs + 1))"
rss="$(sed -n "2,${last_timed_line}p" "$result_dir/rss-bytes.txt" | jq -Rsc 'split("\n") | map(select(length > 0) | tonumber / 1048576)')"
cli_peak="$(sed -n "2,${last_timed_line}p" "$result_dir/cli-peak-mib.txt" | jq -Rsc 'split("\n") | map(select(length > 0) | tonumber)')"

jq -n \
  --slurpfile benchmark "$result_dir/hyperfine.json" \
  --argjson rss "$rss" \
  --argjson cli_peak "$cli_peak" '
    def stats:
      sort as $values |
      {
        min: $values[0],
        median: $values[(length / 2 | floor)],
        mean: (add / length),
        max: $values[-1]
      };
    ($benchmark[0].results[0]) as $result |
    {
      duration_seconds: {
        min: $result.min,
        median: $result.median,
        mean: $result.mean,
        max: $result.max,
        stddev: $result.stddev,
        user: $result.user,
        system: $result.system,
        times: $result.times
      },
      rss_mib: ($rss | stats),
      cli_peak_mib: ($cli_peak | stats)
    }
  ' > "$result_dir/summary.json"
