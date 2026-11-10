# Parallel Postgres CDC snapshot benchmark

Normal snapshots use `--extract-parallelism` as the total snapshot query
connection budget, including concurrent tables. `--extract-parallelism=1`
keeps a serial reader. The budget is also capped by the PostgreSQL query pool;
the replication exporter uses a separate connection. The planning connection
counts toward that budget and becomes the first range worker. Additional
workers take an available budget permit before taking a range; if the shared
budget is occupied, the planning worker can drain the remaining ranges.
Workers do not wait at an import barrier, and a one-connection query pool
can complete the read.

Eligible tables have non-null unique keys with supported native PostgreSQL
ordering. The planner sorts a bounded sample, and workers scan disjoint
`(after, upper]` ranges. PostgreSQL compares typed keys, including composite
keys and their collations; serialized text is only for saving boundaries.
Tables without suitable keys, and unsupported index/table shapes, retain a
serial scan. Sampling can produce fewer ranges than requested readers and
does not guarantee balanced work for skewed data.

Every planning or worker transaction imports the same export before querying.
The caller keeps its exporting connection open and idle until the readers
finish. The planning transaction holds an `ACCESS SHARE` table lock from
planning until every range finishes, preserving the table during the read
without an extra keeper connection. Additional workers import the snapshot
and request `ACCESS SHARE NOWAIT` before scanning. A conflicting queued DDL
lock therefore fails the read and cancels its workers. This prevents a cycle
where a reader waits behind a writer that is waiting for the planning
transaction.
A replacement/resume marker precedes data, range progress follows
the rows it covers, and table completion follows every range and forwarded
result. Destination writes and replacement cleanup must succeed before
completion becomes durable. See the [shared progress contract](../../docs/development/postgres-cdc-parallel-contract.md).

Managed streaming backfills persist a complete range-progress vector after
the covered destination writes succeed. The stable plan, attempt identity,
and each range's durable key/completion survive restarts. Changing reader
count changes scheduling without changing saved coverage. A completed high
range cannot skip unfinished low ranges, and all ranges being complete does
not replace the separate durable table-completion event. Existing single-key
progress resumes as one full-table range with its saved key.
The original replacement boundary remains distinct from the newer export/read
LSN used on resume; retained WAL must repair changes behind saved keys and
completed ranges. An exported snapshot name itself cannot survive a process
restart. Initial snapshots without a usable retained resume position can
restart from scratch. Schema, source or destination identity mismatches and
missing required WAL use the existing invalidation/replacement path.

Normal snapshot data batches identify full images through `CDCSnapshot` for
WI-1's loader. The reusable bounded reader leaves that marker unset; chunk
adoption still requires its own orchestration and ordering work.

`Snapshot.ReadWithSnapshot(ctx, name, lsn, results, opts, after, upper)` reads
an existing export and optional outer bounds without owning the slot, WAL
catch-up, table completion, or chunk orchestration. Its caller must provide
the CDC table schema and keep the export alive. Bounded reads cannot set
`CDCSnapshotReplace`, because a subrange is not a full replacement image.
This is the bounded reader entry point for later chunk work; it does not
enable WI-6.

## Verify export/import on the actual standby

The opt-in test creates a unique disposable table and publication on the
primary and temporary logical slots on the standby. Four independent
read-only, repeatable-read transactions import one export; the fourth starts
after source changes have reached the replica. Updates continue while the
actual reader runs with one/four readers and a bounded range. The test also
closes an exporter before a later import, checking that the import fails and
the previously imported transaction remains usable, and verifies that an
empty full-table snapshot still emits its replacement boundary.

Provide ordinary `postgres://` URIs through the environment, using the
benchmark's PostgreSQL 17 primary and standby. The primary account needs
disposable-table/publication DDL and `pg_log_standby_snapshot()` permission;
the replica account needs logical replication and table reads. The test
asserts the reader endpoint is a standby running PostgreSQL 17.

```sh
# Set these through your normal secret environment; do not put passwords in logs.
export INGESTR_TEST_SNAPSHOT_PRIMARY_URI
export INGESTR_TEST_SNAPSHOT_STANDBY_URI
INTEGRATION_BACKENDS=none go test -tags integration ./tests/integration \
  -run '^TestPostgresParallelSnapshotStandby$' -count=1 -v -timeout 10m
```

The container counterpart explicitly runs PostgreSQL 17 on a primary:

```sh
INTEGRATION_BACKENDS=none go test -tags integration ./tests/integration \
  -run '^TestPostgresParallelSnapshotImports$' -count=1 -v -timeout 5m
```

Neither a primary container result nor a skipped opt-in test establishes the
RDS standby acceptance criterion.

The PostgreSQL pipeline restart regression is
`TestPostgresCDCParallelSnapshotDurableResume`. It injects a failure immediately
after a real progress event becomes durable, restores a legal checkpoint from
that event's exported image and stable range plan, then resumes with a changed
reader count. Its cases cover a completed high range with unfinished low ranges,
and an all-complete vector without table completion. Inserts, updates, deletes
and key moves behind saved cursors must match every source key and payload after
WAL catch-up. `TestPostgresCDC_StreamingSnapshotResumesAfterRestart` retains
single-reader coverage against the PostgreSQL and StarRocks staged paths.
Run the tagged integration and race suites in CI; the commands above describe
the focused container and reserved-standby checks.

## Isolated reader: one versus four readers

Use the same seeded `bench.wide_25k` table, replica instance, storage and
concurrent-write workload in both runs. Analyze the seeded table before both
runs so sampled range planning uses representative row-density statistics;
record the analysis time separately from the timed snapshots. The URI must name an existing
publication containing the table. The command creates its own uniquely
named slot, drains Arrow batches into a counting sink, and removes the slot
on exit. It never writes a destination or acknowledges WAL. The table is
read-only; the run still holds snapshot transactions and retains WAL.
Use the same primary standby-snapshot logging schedule as `compare_all.sh`;
waiting for the standby to build the slot's export is included in snapshot
time.

Build the benchmark once, preferably in the existing build environment,
outside the timed command. Preserve the commit SHA alongside results.

```sh
go build -o /tmp/ingestr-cdc-snapshot-bench ./benchmarks/postgres_cdc_snapshot
mkdir -p benchmarks/results/wi2
export INGESTR_BENCH_SOURCE_URI
git rev-parse HEAD > benchmarks/results/wi2/revision.txt
for readers in 1 4; do
  /usr/bin/time -v -o "benchmarks/results/wi2/reader-${readers}.resources.txt" \
    /tmp/ingestr-cdc-snapshot-bench \
      -readers "$readers" -table bench.wide_25k \
      -page-size 5000 -batch-bytes 16777216 -debug \
      > "benchmarks/results/wi2/reader-${readers}.json" \
      2> "benchmarks/results/wi2/reader-${readers}.log"
done
```

Repeat in reverse order to expose cache effects. The command uses an output
channel of one batch, releases each record immediately, and caps its query
pool at the requested reader budget. Its monitoring connection is separate;
the sampled source connection count excludes that monitor. Record the
instance CPU and memory from the benchmark infrastructure as well as the
local process resources.

Measurement definitions:

| Metric | Definition |
| --- | --- |
| `rows` | Sum of rows in successfully received snapshot Arrow batches. |
| `source_arrow_buffer_bytes` | Sum of Arrow array buffer lengths for source columns, including offsets/null bitmaps/nested buffers and excluding `_cdc_` metadata. This is decoded in-memory size, not compressed PostgreSQL heap size or network bytes. Unsliced batches are counted once. |
| `connection_schema_setup_seconds` | Command setup through source connection and table schema acquisition, before `Snapshot.Execute`. |
| `snapshot_seconds` | From `Snapshot.Execute` start through export creation, planning/imports, all reads, forwarding and counting-sink consumption. |
| `total_seconds` | Setup plus snapshot time, ending when the reader and sink finish; excludes cleanup and process startup. |
| `first_batch_seconds` | Time from snapshot start to the first received data batch. Includes slot export, planning/imports and initial decoding; it is not planning time alone. |
| `first_batch_to_end_seconds` | Remaining drain time after first data arrival. |
| `count_release_seconds` | Time the sink spends counting buffers/rows and releasing records. |
| `source_arrow_mb_per_second_*` | Decoded source Arrow bytes divided by the named elapsed time and 1,000,000. |
| `peak_*_sampled` | Source sessions, active query sessions and slot-retained WAL sampled every 200 ms. These can miss short peaks; require zero monitoring failures. |
| Reader debug timing | `planning` includes the first worker's connection acquisition and snapshot import, table lock, key validation, sampling and projection validation. `imports_sum` sums imports by additional workers only; the planning worker reuses its transaction. `read` is the worker/coordinator wall time; `forward_wait` is time spent sending data downstream. These overlapping metrics must not be added together. |
| GNU time report | User/system CPU time and maximum resident set size of the compiled Go process; GNU time's peak RSS is in KiB. |

Report both total and snapshot throughput. For the 150 MB/s isolated-reader
criterion, state explicitly that the numerator is decoded source Arrow bytes
and use `snapshot_seconds` as the inclusive snapshot denominator. Include
`forward_wait` and counting-sink time to show whether the sink limited the
result. Do not subtract summed per-worker durations from wall time. Preserve
the full debug log and record each range's rows/duration to diagnose skew.

## Combined WI-1 run

The shared benchmark stack is owned in the separate
`instrumental-pg2sr-cdc/infra/` checkout. Coordinate ownership before bringing
up its approximately $1.7/hour stack (25–30 minutes startup). Its documented
provisioning command, run by the stack owner from that directory, is:

```sh
terraform apply -var-file=bench.tfvars -var seed_events_rows=0
```

The harness is `infra/bench/cdc/compare_run.sh`, with `compare_all.sh` for
comparisons; the dataset comes from `infra/bench/wide25.sql`. Baselines are
under `infra/bench/results/c25_*` dated 2026-11-06. The inspected harness
interface is `cdc/compare_run.sh <label> ingestr normal`, run on the benchmark
runner. Its `start_ingestr` command does not accept concurrency arguments.
A wrapper can append both settings while using the same harness:

```sh
# Run from the prepared infra/bench directory on the reserved benchmark runner.
export WI2_INGESTR_BINARY=/absolute/path/to/combined/ingestr
export WI2_DESTINATION_PARALLELISM=4
wi2_wrapper_dir=$(mktemp -d)
cat > "$wi2_wrapper_dir/ingestr" <<'SH'
#!/bin/sh
exec "$WI2_INGESTR_BINARY" "$@" \
  --extract-parallelism "$WI2_EXTRACT_PARALLELISM" \
  --destination-parallelism "$WI2_DESTINATION_PARALLELISM"
SH
chmod +x "$wi2_wrapper_dir/ingestr"
for readers in 1 4; do
  WI2_EXTRACT_PARALLELISM="$readers" PATH="$wi2_wrapper_dir:$PATH" \
    SEEDED=2000000 LIMIT=7200 \
    cdc/compare_run.sh "wi2_reader_${readers}_dest_4" ingestr normal
done
rm -rf "$wi2_wrapper_dir"
```

The owner must prepare the same 1M changes/hour workload and standby snapshot
logging as `compare_all.sh` and arrange clean destinations/slots between runs.
The existing `reset.sh` drops **every inactive logical slot** on the replica,
and the `bench` and `_bruin_staging` databases. Run it only while the stack is
reserved for this benchmark; the wrapper above does not invoke it.

The harness records `total_s` from launch until every seeded ID is visible,
polling every 15 seconds. Its `avg_rows_per_s` excludes the initial wait for
visible rows and is not the end-to-end acceptance metric. Use
`2000000 / total_s` for a complete run, record the polling uncertainty, and
retain the `complete=1` indicator. The harness stops ingestion at that point;
it does not itself establish replacement cleanup, durable table completion,
or correctness after WAL catch-up. Those checks require a separate continued
run and source/target comparison before accepting the combined change.

With WI-1 and WI-2 combined, run the 2M-row normal snapshot at one/four
extraction readers while holding destination concurrency constant explicitly.
Record the exact harness invocation and ingestr flags, resource configuration,
dataset row/byte counts, source-write rate, and wall time from snapshot setup
through all rows becoming visible in StarRocks. Measure 3,000 rows/s against
that complete interval (3,500 rows/s stretch). Preserve range skew, CPU/RSS,
retained WAL, source-forward blocking, destination load time and retirement
cleanup observations separately. After WAL catch-up, compare all live rows
and values, including inserts, deletes, updates and key changes behind saved
range keys; row count alone is insufficient.

## Acceptance evidence

No throughput measurements or RDS standby results are included with this
harness. The shared benchmark stack and WI-1 integration are required to
establish those results. Fill the following from retained run artifacts:

| Evidence | Status |
| --- | --- |
| RDS PostgreSQL 17 shared logical-slot export, delayed fourth importer | Awaiting the opt-in run on the actual standby. |
| One-reader source throughput and CPU/peak memory | Awaiting the shared benchmark dataset. |
| Four-reader source throughput ≥150 MB/s | Awaiting the shared benchmark dataset. |
| WI-1 + WI-2 ≥3,000 rows/s through destination visibility | Awaiting combined branch and shared benchmark stack. |
| Source/target equality after WAL catch-up and restart | Awaiting combined run; automated regression tests are separate evidence. |
