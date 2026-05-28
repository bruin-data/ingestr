# /// script
# requires-python = ">=3.9"
# dependencies = ["pyyaml", "rich"]
# ///
"""Benchmark runner for gong vs other data ingestion tools.

Reads scenario definitions from scenarios.yaml and orchestrates hyperfine benchmarks.

Usage:
    uv run benchmarks/scripts/runner.py                          # Run all scenarios
    uv run benchmarks/scripts/runner.py --rows 1000 --runs 3     # Quick test
    uv run benchmarks/scripts/runner.py --tools gong sling       # Specific tools
    uv run benchmarks/scripts/runner.py --scenarios '*bigquery*'  # Filter scenarios
    uv run benchmarks/scripts/runner.py --validate               # Validation mode
    uv run benchmarks/scripts/runner.py --report                 # Report from latest results
"""

import argparse
import fnmatch
import glob
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path

import yaml
from rich import box
from rich.console import Console
from rich.table import Table

BENCH_DIR = Path(__file__).resolve().parent.parent
PROJECT_ROOT = BENCH_DIR.parent
RESULTS_DIR = BENCH_DIR / "results"
DUCKDB_DIR = BENCH_DIR / "duckdb_files"

console = Console()

EXPECTED_COLS = [
    "id", "small_str", "medium_str", "large_str", "tiny_int",
    "regular_int", "big_int", "float_val", "decimal_val", "bool_val",
    "date_val", "ts_val", "ts_tz_val", "json_val", "extra_text",
]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def size_suffix(rows: int) -> str:
    mapping = {
        1000: "1k", 100000: "100k", 1000000: "1m",
        10000000: "10m", 100000000: "100m", 1000000000: "1b",
    }
    return mapping.get(rows, str(rows))


def resolve_uri(config: dict) -> str | None:
    if "uri" in config:
        uri = config["uri"]
        uri = uri.replace("{bench_dir}", str(BENCH_DIR))
        return uri
    if "uri_from_env" in config:
        return os.environ.get(config["uri_from_env"])
    return None


def resolve_table(pattern: str, size: str) -> str:
    return pattern.replace("{size}", size)


def scenario_name(src_name: str, dst_name: str) -> str:
    src = src_name.removeprefix("local_")
    dst = dst_name.removeprefix("local_")
    return f"{src}_to_{dst}"


def scenario_display(name: str) -> str:
    return name.replace("_to_", " -> ")


def parse_table_parts(qualified: str) -> tuple[str, str]:
    """Split 'schema.table' into (schema, table). Returns ('', table) if no schema."""
    if "." in qualified:
        schema, table = qualified.split(".", 1)
        return schema, table
    return "", qualified


def qualify_bq_table(uri: str, table: str) -> str:
    """For BigQuery, ensure table is qualified as dataset.table."""
    _, bare = parse_table_parts(table)
    _, dataset = bq_parts_from_uri(uri)
    return f"{dataset}.{bare}"


def duckdb_path_from_uri(uri: str) -> str:
    return uri.split("duckdb:///", 1)[1]


def bq_parts_from_uri(uri: str) -> tuple[str, str]:
    """Parse bigquery://project/dataset into (project, dataset)."""
    stripped = uri.replace("bigquery://", "")
    parts = stripped.split("/", 1)
    project = parts[0]
    dataset = parts[1] if len(parts) > 1 else ""
    return project, dataset


# ---------------------------------------------------------------------------
# Config loading
# ---------------------------------------------------------------------------

def load_config(config_path: Path | None = None) -> dict:
    if config_path is None:
        config_path = BENCH_DIR / "scenarios.yaml"
    with open(config_path) as f:
        return yaml.safe_load(f)


def resolve_sources(config: dict, size: str) -> dict:
    """Resolve all sources: expand URIs and table patterns."""
    resolved = {}
    for name, src in config.get("sources", {}).items():
        uri = resolve_uri(src)
        if uri is None:
            continue
        resolved[name] = {
            **src,
            "uri": uri,
            "table": resolve_table(src["table"], size),
        }
    return resolved


def resolve_destinations(config: dict) -> dict:
    """Resolve all destinations: expand URIs."""
    resolved = {}
    for name, dst in config.get("destinations", {}).items():
        uri = resolve_uri(dst)
        if uri is None:
            console.print(f"  [yellow]Destination '{name}' skipped "
                          f"(env var {dst.get('uri_from_env', '?')} not set)[/yellow]")
            continue
        creds_env = dst.get("credentials_env")
        if creds_env and not os.environ.get(creds_env):
            console.print(f"  [yellow]Destination '{name}' skipped "
                          f"(env var {creds_env} not set)[/yellow]")
            continue
        resolved[name] = {**dst, "uri": uri}
    return resolved


# ---------------------------------------------------------------------------
# Tool availability & skip logic
# ---------------------------------------------------------------------------

def check_tool_available(name: str, tool_cfg: dict) -> bool:
    if name == "gong":
        binary = tool_cfg.get("binary", "bin/gong")
        return (PROJECT_ROOT / binary).exists()
    requires = tool_cfg.get("requires")
    if requires:
        return shutil.which(requires) is not None
    return True


def should_skip_tool(tool_cfg: dict, src_type: str, dst_type: str) -> bool:
    for rule in tool_cfg.get("skip", []):
        if "source_type" in rule and rule["source_type"] == src_type:
            return True
        if "destination_type" in rule and rule["destination_type"] == dst_type:
            return True
    return False


# ---------------------------------------------------------------------------
# Tool command builders
# ---------------------------------------------------------------------------

def translate_uri(uri: str, tool_cfg: dict) -> str:
    for original, replacement in tool_cfg.get("uri_scheme_overrides", {}).items():
        if uri.startswith(f"{original}://"):
            uri = uri.replace(f"{original}://", f"{replacement}://", 1)
    return uri


def sling_env_name(role: str, name: str) -> str:
    return f"SLING_{role}_{name.upper()}"


def sling_connection_value(uri: str, db_type: str) -> str:
    if db_type == "bigquery":
        project, dataset = bq_parts_from_uri(uri)
        cfg = {"type": "bigquery", "project": project, "dataset": dataset}
        creds = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
        if creds:
            cfg["gc_key_file"] = creds
        return json.dumps(cfg)
    return uri


def build_tool_command(
    tool_name: str, tool_cfg: dict,
    src_uri: str, src_table: str,
    dst_uri: str, dst_table: str,
    src_type: str, dst_type: str,
    src_cfg_name: str, dst_cfg_name: str,
) -> str:
    if tool_name == "gong":
        binary = PROJECT_ROOT / tool_cfg.get("binary", "bin/gong")
        return (
            f"INGESTR_DISABLE_TELEMETRY=1 DISABLE_TELEMETRY=1 '{binary}' ingest"
            f" --source-uri '{src_uri}'"
            f" --source-table '{src_table}'"
            f" --dest-uri '{dst_uri}'"
            f" --dest-table '{dst_table}'"
            f" --incremental-strategy replace --progress log --yes"
        )

    if tool_name == "ingestr":
        prefix = tool_cfg.get(
            "command_prefix",
            "uv tool run --python 3.11 ingestr@0.14.141 ingest",
        )
        prefix = f"INGESTR_DISABLE_TELEMETRY=1 DISABLE_TELEMETRY=1 {prefix}"
        tsrc = translate_uri(src_uri, tool_cfg)
        tdst = translate_uri(dst_uri, tool_cfg)
        extra = tool_cfg.get("extra_args_by_source", {}).get(src_type, "")
        parts = [
            prefix,
            f"--source-uri '{tsrc}'",
            f"--source-table '{src_table}'",
            f"--dest-uri '{tdst}'",
            f"--dest-table '{dst_table}'",
            "--yes --full-refresh",
        ]
        if extra:
            parts.append(extra)
        return " ".join(parts)

    if tool_name == "sling":
        src_env = sling_env_name("SRC", src_cfg_name)
        dst_env = sling_env_name("DST", dst_cfg_name)
        env = ["SLING_DISABLE_TELEMETRY=true"]
        if src_type == "mongodb":
            env.append("SLING_SAMPLE_SIZE=3000")
        prefix = " ".join(env) + " "
        parts = [
            f"{prefix}sling run"
            f" --src-conn {src_env}",
            f" --src-stream '{src_table}'",
        ]
        if src_type == "mongodb":
            parts.append(" --src-options '{flatten: true}'")
        parts += [
            f" --tgt-conn {dst_env}",
            f" --tgt-object '{dst_table}'",
            f" --mode full-refresh",
        ]
        return "".join(parts)

    if tool_name == "dlt":
        script = BENCH_DIR / tool_cfg.get("script", "scripts/bench_dlt.py")
        return (
            f"RUNTIME__DLTHUB_TELEMETRY=false uv run '{script}'"
            f" --source-uri '{src_uri}'"
            f" --source-table '{src_table}'"
            f" --dest-uri '{dst_uri}'"
            f" --dest-table '{dst_table}'"
        )

    if tool_name == "airbyte":
        script = BENCH_DIR / tool_cfg.get("script", "scripts/bench_airbyte.py")
        return (
            f"AIRBYTE_ANALYTICS_DISABLED=1 DO_NOT_TRACK=1 uv run '{script}'"
            f" --source-uri '{src_uri}'"
            f" --source-table '{src_table}'"
            f" --dest-uri '{dst_uri}'"
            f" --dest-table '{dst_table}'"
        )

    raise ValueError(f"Unknown tool: {tool_name}")


# ---------------------------------------------------------------------------
# Prepare / cleanup commands
# ---------------------------------------------------------------------------

def build_prepare_command(
    dst_type: str, dst_uri: str, dst_table: str,
    src_config: dict | None = None,
) -> str:
    src_db = (src_config or {}).get("database", "")

    if dst_type == "postgres":
        schema, table = parse_table_parts(dst_table)
        schema = schema or "public"
        drops = [
            f"DROP TABLE IF EXISTS \"{schema}\".{table}",
            f"DROP TABLE IF EXISTS \"{schema}\"._dlt_loads",
            f"DROP TABLE IF EXISTS \"{schema}\"._dlt_version",
            f"DROP TABLE IF EXISTS \"{schema}\"._dlt_pipeline_state",
            "DROP SCHEMA IF EXISTS airbyte_internal CASCADE",
        ]
        if src_db:
            drops.append(f"DROP SCHEMA IF EXISTS {src_db} CASCADE")
        sql = "; ".join(drops)
        return f"psql '{dst_uri}' -c '{sql}' -q"

    if dst_type == "duckdb":
        path = duckdb_path_from_uri(dst_uri)
        return f"rm -f '{path}' '{path}.wal'"

    if dst_type == "bigquery":
        project, dataset = bq_parts_from_uri(dst_uri)
        _, table = parse_table_parts(dst_table)
        tables = [table, "_dlt_loads", "_dlt_version", "_dlt_pipeline_state"]
        cmds = [
            f"bq rm -f --table '{project}:{dataset}.{t}' 2>/dev/null"
            for t in tables
        ]
        return "; ".join(cmds) + "; true"

    raise ValueError(f"Unknown destination type: {dst_type}")


# ---------------------------------------------------------------------------
# Setup & seed (delegate to existing bash scripts)
# ---------------------------------------------------------------------------

def run_setup():
    console.print("[bold]==> Running setup...[/bold]")
    subprocess.run(
        ["bash", str(BENCH_DIR / "scripts" / "setup.sh")],
        check=True,
    )


def run_seed(rows: int):
    console.print(f"[bold]==> Seeding {rows:,} rows...[/bold]")
    env = {**os.environ, "BENCH_ROWS": str(rows), "BENCH_SEED_SIZES": str(rows)}
    subprocess.run(
        ["bash", str(BENCH_DIR / "scripts" / "seed.sh")],
        env=env,
        check=True,
    )


# ---------------------------------------------------------------------------
# Sling env var export
# ---------------------------------------------------------------------------

def export_sling_env(sources: dict, destinations: dict):
    for name, src in sources.items():
        env_name = sling_env_name("SRC", name)
        os.environ[env_name] = sling_connection_value(src["uri"], src["type"])
    for name, dst in destinations.items():
        env_name = sling_env_name("DST", name)
        os.environ[env_name] = sling_connection_value(dst["uri"], dst["type"])


# ---------------------------------------------------------------------------
# Benchmark execution
# ---------------------------------------------------------------------------

def run_benchmarks(
    config: dict,
    scenarios: list[dict],
    sources: dict,
    destinations: dict,
    tools: list[str],
    tool_configs: dict,
    rows: int,
    runs: int,
    warmup: int,
    size: str,
    show_output: bool = False,
):
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    results_prefix = str(RESULTS_DIR / timestamp)

    meta = {"rows": rows, "runs": runs, "warmup": warmup, "tools": " ".join(tools)}
    with open(f"{results_prefix}_meta.json", "w") as f:
        json.dump(meta, f)

    export_sling_env(sources, destinations)

    for scenario_cfg in scenarios:
        src_name = scenario_cfg["source"]
        dst_name = scenario_cfg["destination"]
        src = sources[src_name]
        dst = destinations[dst_name]
        name = scenario_name(src_name, dst_name)

        dst_table = dst["table"]
        if dst["type"] == "bigquery":
            dst_table = qualify_bq_table(dst["uri"], dst_table)

        console.print(f"\n[bold magenta]===== {scenario_display(name)} =====[/bold magenta]")

        prepare_cmd = build_prepare_command(
            dst["type"], dst["uri"], dst_table, src_config=src,
        )

        names = []
        cmds = []

        for tool_name in tools:
            tool_cfg = tool_configs[tool_name]

            if should_skip_tool(tool_cfg, src["type"], dst["type"]):
                console.print(f"  [dim]{tool_name}: skipped[/dim]")
                continue

            cmd = build_tool_command(
                tool_name, tool_cfg,
                src["uri"], src["table"],
                dst["uri"], dst_table,
                src["type"], dst["type"],
                src_name, dst_name,
            )
            names.append(tool_name)
            cmds.append(cmd)

        if not names:
            console.print("  [yellow]No eligible tools for this scenario[/yellow]")
            continue

        hf_args = ["hyperfine"]
        hf_args += ["--runs", str(runs)]
        hf_args += ["--warmup", str(warmup)]
        hf_args += ["--prepare", prepare_cmd]
        hf_args += ["--export-json", f"{results_prefix}_{name}.json"]
        hf_args += ["--export-markdown", f"{results_prefix}_{name}.md"]


        if show_output:
            hf_args.append("--show-output")

        for i, tool_name in enumerate(names):
            hf_args += ["--command-name", tool_name]
            hf_args.append(cmds[i])

        subprocess.run(hf_args, check=True)

    console.print("\n[bold]==> All benchmarks complete.[/bold]")
    console.print(f"    Results in: {RESULTS_DIR}/")

    generate_report(results_prefix, rows, runs, warmup)


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

def query_destination(dst_type: str, dst_uri: str, table: str, schema: str, query_type: str):
    """Query a destination database. query_type: 'count', 'sum_id', 'columns'."""
    try:
        if dst_type == "postgres":
            schema = schema or "public"
            if query_type == "count":
                sql = f'SELECT count(*) FROM "{schema}".{table}'
                result = subprocess.run(
                    ["psql", dst_uri, "-t", "-A", "-c", sql],
                    capture_output=True, text=True, timeout=30,
                )
                if result.returncode != 0:
                    return None
                return result.stdout.strip()
            elif query_type == "sum_id":
                queries = [
                    f'SELECT COALESCE(SUM(id), 0) FROM "{schema}".{table}',
                    f"""SELECT COALESCE(SUM((data->>'id')::bigint), 0) FROM "{schema}".{table}""",
                ]
                for sql in queries:
                    result = subprocess.run(
                        ["psql", dst_uri, "-t", "-A", "-c", sql],
                        capture_output=True, text=True, timeout=30,
                    )
                    if result.returncode == 0:
                        return result.stdout.strip()
                return None
            elif query_type == "columns":
                sql = (f"SELECT column_name FROM information_schema.columns "
                       f"WHERE table_schema='{schema}' AND table_name='{table}' "
                       f"ORDER BY ordinal_position")
                result = subprocess.run(
                    ["psql", dst_uri, "-t", "-A", "-c", sql],
                    capture_output=True, text=True, timeout=30,
                )
                if result.returncode != 0:
                    return None
                columns = [line.strip() for line in result.stdout.strip().split("\n") if line.strip()]
                if "data" in columns:
                    json_sql = (
                        f"""SELECT DISTINCT key FROM "{schema}".{table}, """
                        f"""LATERAL jsonb_object_keys(data) AS key ORDER BY key"""
                    )
                    json_result = subprocess.run(
                        ["psql", dst_uri, "-t", "-A", "-c", json_sql],
                        capture_output=True, text=True, timeout=30,
                    )
                    if json_result.returncode == 0:
                        json_columns = [
                            line.strip()
                            for line in json_result.stdout.strip().split("\n")
                            if line.strip()
                        ]
                        columns = list(dict.fromkeys(columns + json_columns))
                return columns
            else:
                return None

        elif dst_type == "duckdb":
            path = duckdb_path_from_uri(dst_uri)
            schema = schema or "main"
            if query_type == "count":
                sql = f'SELECT count(*) FROM "{schema}".{table}'
                result = subprocess.run(
                    ["duckdb", path, "-noheader", "-csv", "-c", sql],
                    capture_output=True, text=True, timeout=30,
                )
                if result.returncode != 0:
                    return None
                return result.stdout.strip()
            elif query_type == "sum_id":
                queries = [
                    f'SELECT COALESCE(SUM(id), 0) FROM "{schema}".{table}',
                    f"""SELECT COALESCE(SUM(CAST(json_extract_string(data, '$.id') AS BIGINT)), 0) FROM "{schema}".{table}""",
                ]
                for sql in queries:
                    result = subprocess.run(
                        ["duckdb", path, "-noheader", "-csv", "-c", sql],
                        capture_output=True, text=True, timeout=30,
                    )
                    if result.returncode == 0:
                        return result.stdout.strip()
                return None
            elif query_type == "columns":
                sql = (f"SELECT column_name FROM information_schema.columns "
                       f"WHERE table_schema='{schema}' AND table_name='{table}' "
                       f"ORDER BY ordinal_position")
                result = subprocess.run(
                    ["duckdb", path, "-noheader", "-csv", "-c", sql],
                    capture_output=True, text=True, timeout=30,
                )
                if result.returncode != 0:
                    return None
                columns = [line.strip() for line in result.stdout.strip().split("\n") if line.strip()]
                if "data" in columns:
                    json_sql = f"""SELECT DISTINCT unnest(json_keys(data)) AS key FROM "{schema}".{table} ORDER BY key"""
                    json_result = subprocess.run(
                        ["duckdb", path, "-noheader", "-csv", "-c", json_sql],
                        capture_output=True, text=True, timeout=30,
                    )
                    if json_result.returncode == 0:
                        json_columns = [
                            line.strip()
                            for line in json_result.stdout.strip().split("\n")
                            if line.strip()
                        ]
                        columns = list(dict.fromkeys(columns + json_columns))
                return columns
            else:
                return None

        elif dst_type == "bigquery":
            project, ds = bq_parts_from_uri(dst_uri)
            fqt = f"`{project}.{ds}.{table}`"
            if query_type == "count":
                sql = f"SELECT count(*) as v FROM {fqt}"
            elif query_type == "sum_id":
                sql = f"SELECT COALESCE(SUM(id), 0) as v FROM {fqt}"
            elif query_type == "columns":
                sql = (f"SELECT column_name FROM "
                       f"`{project}.{ds}.INFORMATION_SCHEMA.COLUMNS` "
                       f"WHERE table_name='{table}' ORDER BY ordinal_position")
            else:
                return None
            result = subprocess.run(
                ["bq", "query", "--nouse_legacy_sql", "--format=csv", "--quiet",
                 f"--project_id={project}", sql],
                capture_output=True, text=True, timeout=60,
            )
            if result.returncode != 0:
                return None
            lines = [l.strip() for l in result.stdout.strip().split("\n") if l.strip()]
            if query_type == "columns":
                return lines[1:] if len(lines) > 1 else []
            return lines[-1] if lines else None

    except Exception:
        return None
    return None


def check_result(
    label: str, dst_type: str, dst_uri: str,
    check_table: str, check_schema: str,
    expected_rows: int, expected_id_sum: int,
) -> bool:
    row_count = query_destination(dst_type, dst_uri, check_table, check_schema, "count")
    id_sum = query_destination(dst_type, dst_uri, check_table, check_schema, "sum_id")
    columns = query_destination(dst_type, dst_uri, check_table, check_schema, "columns") or []

    ok = True

    if str(row_count) != str(expected_rows):
        console.print(f"  [red]FAIL: row count = {row_count}, expected {expected_rows}[/red]")
        ok = False

    if str(id_sum) != str(expected_id_sum):
        console.print(f"  [red]FAIL: SUM(id) = {id_sum}, expected {expected_id_sum}[/red]")
        ok = False

    for col in EXPECTED_COLS:
        if col not in columns:
            console.print(f"  [red]FAIL: missing column '{col}'[/red]")
            ok = False

    if ok:
        console.print(f"  [green]PASS ({row_count} rows, SUM(id)={id_sum}, all 15 columns)[/green]")

    return ok


def run_tool_once(
    tool_name: str, tool_cfg: dict,
    src_uri: str, src_table: str,
    dst_uri: str, dst_table: str,
    src_type: str, dst_type: str,
    src_cfg_name: str, dst_cfg_name: str,
) -> int:
    cmd = build_tool_command(
        tool_name, tool_cfg,
        src_uri, src_table,
        dst_uri, dst_table,
        src_type, dst_type,
        src_cfg_name, dst_cfg_name,
    )
    result = subprocess.run(
        ["bash", "-c", cmd],
        capture_output=True, text=True, timeout=300,
    )
    return result.returncode


def run_validation(
    config: dict,
    scenarios: list[dict],
    sources: dict,
    destinations: dict,
    tools: list[str],
    tool_configs: dict,
    rows: int,
    size: str,
):
    expected_rows = rows
    expected_id_sum = rows * (rows + 1) // 2

    export_sling_env(sources, destinations)

    passed = 0
    failed = 0
    skipped = 0
    failures = []

    console.print(f"[bold]==> Validating all tools with {expected_rows:,} rows[/bold]")
    console.print(f"    Tools: {', '.join(tools)}")

    for scenario_cfg in scenarios:
        src_name = scenario_cfg["source"]
        dst_name = scenario_cfg["destination"]
        src = sources[src_name]
        dst = destinations[dst_name]
        name = scenario_name(src_name, dst_name)

        dst_table = dst["table"]
        if dst["type"] == "bigquery":
            dst_table = qualify_bq_table(dst["uri"], dst_table)

        console.print(f"\n[bold magenta]===== {scenario_display(name)} =====[/bold magenta]")

        for tool_name in tools:
            tool_cfg = tool_configs[tool_name]

            if should_skip_tool(tool_cfg, src["type"], dst["type"]):
                console.print(f"  [{tool_name}] [dim]SKIP (tool skip rule)[/dim]")
                skipped += 1
                continue

            label = f"{tool_name} / {name}"
            console.print(f"  [{tool_name}] ", end="")

            # Clean destination
            prepare_cmd = build_prepare_command(
                dst["type"], dst["uri"], dst_table, src_config=src,
            )
            subprocess.run(["bash", "-c", prepare_cmd], capture_output=True, timeout=60)

            # Run tool
            rc = run_tool_once(
                tool_name, tool_cfg,
                src["uri"], src["table"],
                dst["uri"], dst_table,
                src["type"], dst["type"],
                src_name, dst_name,
            )

            if rc != 0:
                console.print(f"[red]FAIL: command exited with code {rc}[/red]")
                failed += 1
                failures.append(f"{label} (exit code {rc})")
                continue

            # Determine which table/schema to check
            _, src_table_bare = parse_table_parts(src["table"])
            dst_schema, dst_table_name = parse_table_parts(dst_table)

            if tool_name == "airbyte":
                check_table = src_table_bare
                check_schema = src.get("database", "") if src["type"] in ("mysql", "mongodb") else dst_schema
            else:
                check_table = dst_table_name
                check_schema = dst_schema

            ok = check_result(
                label, dst["type"], dst["uri"],
                check_table, check_schema,
                expected_rows, expected_id_sum,
            )

            if ok:
                passed += 1
            else:
                failed += 1
                failures.append(label)

    console.print()
    console.rule("[bold]Validation Summary[/bold]")
    console.print(f"  [green]PASSED:  {passed}[/green]")
    console.print(f"  [red]FAILED:  {failed}[/red]")
    console.print(f"  [dim]SKIPPED: {skipped}[/dim]")

    if failures:
        console.print("\n[red]Failures:[/red]")
        for f in failures:
            console.print(f"  - {f}")
        sys.exit(1)


# ---------------------------------------------------------------------------
# Report generation
# ---------------------------------------------------------------------------

def generate_report(results_prefix: str, rows: int, runs: int, warmup: int):
    console.print()
    console.rule("[bold]Benchmark Results[/bold]")
    console.print(
        f"  Rows: [cyan]{rows:,}[/cyan]  |  "
        f"Runs: [cyan]{runs}[/cyan]  |  "
        f"Warmup: [cyan]{warmup}[/cyan]"
    )
    console.print()

    json_files = sorted(glob.glob(f"{results_prefix}_*.json"))
    json_files = [f for f in json_files if not f.endswith("_meta.json")]

    if not json_files:
        console.print("[red]No result files found.[/red]")
        return

    md_lines = [
        "# Benchmark Results\n",
        f"- **Rows**: {rows:,}",
        f"- **Runs**: {runs}",
        f"- **Warmup**: {warmup}\n",
    ]

    for json_file in json_files:
        name = Path(json_file).stem
        parts = name.split("_", 2)
        if len(parts) >= 3:
            name = parts[2]

        with open(json_file) as f:
            data = json.load(f)

        results = data.get("results", [])
        if not results:
            continue

        results.sort(key=lambda r: r["mean"])
        fastest = results[0]["mean"]

        table = Table(
            title=scenario_display(name),
            box=box.ROUNDED,
            title_style="bold magenta",
            header_style="bold",
        )
        table.add_column("Tool", style="cyan", min_width=8)
        table.add_column("Mean (s)", justify="right", min_width=10)
        table.add_column("± Stddev", justify="right", min_width=10)
        table.add_column("Min (s)", justify="right", min_width=10)
        table.add_column("Max (s)", justify="right", min_width=10)
        table.add_column("vs fastest", justify="right", min_width=10)

        md_lines.append(f"## {name}\n")
        md_lines.append("| Tool | Mean (s) | ± Stddev | Min (s) | Max (s) | vs fastest |")
        md_lines.append("|------|----------|----------|---------|---------|------------|")

        for r in results:
            tool = r["command"]
            mean = r["mean"]
            stddev = r.get("stddev", 0) or 0
            mn = r.get("min", mean)
            mx = r.get("max", mean)
            ratio = mean / fastest if fastest > 0 else 1.0

            if ratio <= 1.01:
                ratio_rich = "[bold green]fastest[/bold green]"
                ratio_md = "fastest"
            else:
                ratio_rich = f"[yellow]{ratio:.1f}x[/yellow]"
                ratio_md = f"{ratio:.1f}x"

            table.add_row(
                tool, f"{mean:.2f}", f"{stddev:.2f}",
                f"{mn:.2f}", f"{mx:.2f}", ratio_rich,
            )
            md_lines.append(
                f"| {tool} | {mean:.2f} | {stddev:.2f} | "
                f"{mn:.2f} | {mx:.2f} | {ratio_md} |"
            )

        md_lines.append("")
        console.print(table)
        console.print()

    summary_file = f"{results_prefix}_summary.md"
    with open(summary_file, "w") as f:
        f.write("\n".join(md_lines))
    console.print(f"Summary saved to: [dim]{summary_file}[/dim]")


def report_from_existing(prefix: str | None):
    if prefix:
        results_prefix = prefix
    else:
        files = sorted(glob.glob(str(RESULTS_DIR / "*.json")))
        if not files:
            console.print("[red]No results found.[/red]")
            sys.exit(1)
        import re
        m = re.match(r"(.*?/\d{8}_\d{6})_", files[-1])
        if not m:
            console.print("[red]Could not determine results prefix.[/red]")
            sys.exit(1)
        results_prefix = m.group(1)

    meta_file = f"{results_prefix}_meta.json"
    if os.path.exists(meta_file):
        with open(meta_file) as f:
            meta = json.load(f)
        rows = meta["rows"]
        runs = meta["runs"]
        warmup = meta["warmup"]
    else:
        rows = runs = warmup = "?"

    generate_report(results_prefix, rows, runs, warmup)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args():
    parser = argparse.ArgumentParser(
        description="Benchmark runner for gong vs other data ingestion tools.",
    )
    parser.add_argument(
        "--config", type=Path, default=None,
        help="Path to scenarios YAML (default: benchmarks/scenarios.yaml)",
    )
    parser.add_argument(
        "--rows", type=int, default=None,
        help="Number of rows (default from YAML/BENCH_ROWS env)",
    )
    parser.add_argument(
        "--runs", type=int, default=None,
        help="Number of benchmark runs (default from YAML/BENCH_RUNS env)",
    )
    parser.add_argument(
        "--warmup", type=int, default=None,
        help="Warmup runs (default from YAML/BENCH_WARMUP env)",
    )
    parser.add_argument(
        "--tools", nargs="+", default=None,
        help="Only run these tools (e.g. --tools gong sling)",
    )
    parser.add_argument(
        "--scenarios", nargs="+", default=None,
        help="Only run scenarios matching these patterns (e.g. --scenarios '*bigquery*')",
    )
    parser.add_argument(
        "--show-output", action="store_true",
        help="Pass --show-output to hyperfine (shows tool stdout/stderr)",
    )
    parser.add_argument(
        "--validate", action="store_true",
        help="Run validation (1k rows, check correctness) instead of benchmarks",
    )
    parser.add_argument(
        "--report", nargs="?", const="__latest__", default=None,
        help="Generate report from existing results (optionally pass a prefix path)",
    )
    parser.add_argument(
        "--skip-setup", action="store_true",
        help="Skip setup and seeding (assume infrastructure is ready)",
    )
    return parser.parse_args()


def main():
    args = parse_args()

    # Report mode: just generate report and exit
    if args.report is not None:
        prefix = None if args.report == "__latest__" else args.report
        report_from_existing(prefix)
        return

    config = load_config(args.config)
    defaults = config.get("defaults", {})

    # Resolve tunables: CLI > env var > YAML defaults
    rows = (
        args.rows
        or int(os.environ.get("BENCH_ROWS", 0)) or None
        or defaults.get("rows", 10_000_000)
    )
    runs = (
        args.runs
        or int(os.environ.get("BENCH_RUNS", 0)) or None
        or defaults.get("runs", 5)
    )
    warmup = args.warmup
    if warmup is None:
        env_warmup = os.environ.get("BENCH_WARMUP")
        if env_warmup is not None:
            warmup = int(env_warmup)
        else:
            warmup = defaults.get("warmup", 1)

    if args.validate:
        rows = 1000

    size = size_suffix(rows)

    console.print(f"[bold]==> Configuration[/bold]")
    console.print(f"    Rows: {rows:,}  |  Runs: {runs}  |  Warmup: {warmup}")

    # Resolve sources and destinations
    sources = resolve_sources(config, size)
    destinations = resolve_destinations(config)

    # Determine available tools
    tool_configs = config.get("tools", {})
    tool_filter = args.tools or os.environ.get("BENCH_TOOLS", "").split() or None

    available_tools = []
    for tool_name, tool_cfg in tool_configs.items():
        if tool_filter and tool_name not in tool_filter:
            continue
        if check_tool_available(tool_name, tool_cfg):
            available_tools.append(tool_name)
        else:
            req = tool_cfg.get("requires", tool_cfg.get("binary", "?"))
            console.print(f"  [dim]Tool '{tool_name}' skipped ({req} not found)[/dim]")

    if not available_tools:
        console.print("[red]No tools available![/red]")
        sys.exit(1)

    console.print(f"    Tools: {', '.join(available_tools)}")

    # Filter scenarios
    active_scenarios = []
    for scenario_cfg in config.get("scenarios", []):
        src_name = scenario_cfg["source"]
        dst_name = scenario_cfg["destination"]

        if src_name not in sources or dst_name not in destinations:
            continue

        name = scenario_name(src_name, dst_name)
        if args.scenarios:
            if not any(fnmatch.fnmatch(name, pat) for pat in args.scenarios):
                continue

        active_scenarios.append(scenario_cfg)

    if not active_scenarios:
        console.print("[red]No scenarios to run (check env vars for remote destinations).[/red]")
        sys.exit(1)

    names = [scenario_name(s["source"], s["destination"]) for s in active_scenarios]
    console.print(f"    Scenarios: {', '.join(names)}")

    # Setup and seed
    if not args.skip_setup:
        run_setup()
        run_seed(rows)

    # Run
    if args.validate:
        run_validation(
            config, active_scenarios, sources, destinations,
            available_tools, tool_configs, rows, size,
        )
    else:
        run_benchmarks(
            config, active_scenarios, sources, destinations,
            available_tools, tool_configs, rows, runs, warmup, size,
            show_output=args.show_output,
        )


if __name__ == "__main__":
    main()
