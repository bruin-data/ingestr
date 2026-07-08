package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseLines splits buffered output into parsed JSON records, asserting that
// every non-empty line is valid JSON (the contract jq pipelines depend on).
func parseLines(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(line), &m), "line should be valid JSON: %s", line)
		records = append(records, m)
	}
	return records
}

func initJSON(t *testing.T) (out, errb *bytes.Buffer) {
	t.Helper()
	out = &bytes.Buffer{}
	errb = &bytes.Buffer{}
	Init(out, errb, ModeJSON)
	return out, errb
}

func TestWarnfJSON(t *testing.T) {
	out, _ := initJSON(t)
	Warnf("Warning: table %q produced no rows", "users")

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	assert.Equal(t, "log", recs[0]["event"])
	assert.Equal(t, "WARN", recs[0]["level"])
	assert.Equal(t, `Warning: table "users" produced no rows`, recs[0]["msg"])
}

func TestInfofJSON(t *testing.T) {
	out, _ := initJSON(t)
	Infof("Naming convention: %s", "snake_case")

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	assert.Equal(t, "log", recs[0]["event"])
	assert.Equal(t, "INFO", recs[0]["level"])
	assert.Equal(t, "Naming convention: snake_case", recs[0]["msg"])
}

func TestStatusfSuppressedInJSON(t *testing.T) {
	out, _ := initJSON(t)
	Statusf("[MERGE] %s | Using staging table: %s\n", "14:00:00", "stg_users")
	assert.Empty(t, out.String(), "decorative status chatter must not appear in JSON output")
}

func TestTextModeVerbatim(t *testing.T) {
	var out, errb bytes.Buffer
	Init(&out, &errb, ModeText)

	Warnf("Warning: x\n")
	Infof("Naming: y\n")
	Statusf("[MERGE] %s | z\n", "14:00:00")

	assert.Equal(t, "Warning: x\nNaming: y\n[MERGE] 14:00:00 | z\n", out.String())
	assert.Empty(t, errb.String())
}

func TestWriteDebugRouting(t *testing.T) {
	// JSON mode: debug goes to stderr so stdout stays pure JSON.
	out, errb := initJSON(t)
	WriteDebug("2026-06-30T00:00:00.000Z\tDEBUG\thello\n")
	assert.Empty(t, out.String())
	assert.Contains(t, errb.String(), "DEBUG\thello")

	// Text mode: debug goes to stdout (historical behavior).
	var o2, e2 bytes.Buffer
	Init(&o2, &e2, ModeText)
	WriteDebug("dbg\n")
	assert.Equal(t, "dbg\n", o2.String())
	assert.Empty(t, e2.String())
}

func TestTimestampRenamedToTS(t *testing.T) {
	out, _ := initJSON(t)
	Infof("hello")

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	assert.Contains(t, recs[0], "ts")
	assert.NotContains(t, recs[0], "time")
}

func TestEventStart(t *testing.T) {
	out, _ := initJSON(t)
	EventStart(StartInfo{
		SourceType:     "postgres",
		DestType:       "bigquery",
		SourceTable:    "public.users",
		DestTable:      "users",
		Strategy:       "merge",
		IncrementalKey: "updated_at",
		PrimaryKey:     []string{"id"},
		SchemaNaming:   "default",
	})

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "start", r["event"])
	assert.Equal(t, "postgres", r["source_type"])
	assert.Equal(t, "bigquery", r["dest_type"])
	assert.Equal(t, "public.users", r["source_table"])
	assert.Equal(t, "users", r["dest_table"])
	assert.Equal(t, "merge", r["strategy"])
	assert.Equal(t, "updated_at", r["incremental_key"])
	assert.Equal(t, []any{"id"}, r["primary_key"])
}

func TestEventStartOmitsEmptyOptionals(t *testing.T) {
	out, _ := initJSON(t)
	EventStart(StartInfo{SourceType: "csv", DestType: "duckdb", Strategy: "replace"})

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	assert.NotContains(t, recs[0], "incremental_key")
	assert.NotContains(t, recs[0], "primary_key")
	assert.NotContains(t, recs[0], "schema_naming")
}

func TestEventProgress(t *testing.T) {
	out, _ := initJSON(t)
	EventProgress(12345, 12, 4200, 2.9, 35.2, 512)

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "progress", r["event"])
	assert.EqualValues(t, 12345, r["rows"])
	assert.EqualValues(t, 12, r["batches"])
	assert.EqualValues(t, 4200, r["rows_per_sec"])
	assert.EqualValues(t, 2.9, r["elapsed_sec"])
	assert.NotContains(t, r, "error")
}

func TestEventEndSuccess(t *testing.T) {
	out, _ := initJSON(t)
	EventEnd("success", 50000, 50, 11.8, nil)

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "end", r["event"])
	assert.Equal(t, "success", r["status"])
	assert.Equal(t, "INFO", r["level"])
	assert.EqualValues(t, 50000, r["rows"])
	assert.Nil(t, r["error"])
}

func TestEventEndError(t *testing.T) {
	out, _ := initJSON(t)
	EventEnd("error", 123, 1, 3.1, errors.New("boom"))

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "end", r["event"])
	assert.Equal(t, "error", r["status"])
	assert.Equal(t, "ERROR", r["level"])
	assert.Equal(t, "boom", r["error"])
}

func TestEventStatsJSON(t *testing.T) {
	out, _ := initJSON(t)
	rowsSkipped := int64(0)
	tables := []struct {
		Name            string  `json:"name"`
		RowsLoaded      *int64  `json:"rows_loaded"`
		RowsSkipped     *int64  `json:"rows_skipped"`
		DurationSeconds float64 `json:"duration_seconds"`
		Mode            string  `json:"mode"`
	}{
		{Name: "public.users", RowsLoaded: int64Ptr(3), RowsSkipped: &rowsSkipped, DurationSeconds: 1.25, Mode: "replace"},
	}

	EventStats("run-1", "postgres://user:xxxxx@localhost/db", "duckdb:///tmp/out.duckdb", 1.5, tables)

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "stats", r["event"])
	assert.Equal(t, "run-1", r["run_id"])
	assert.Equal(t, "postgres://user:xxxxx@localhost/db", r["source"])
	assert.EqualValues(t, 1.5, r["duration_seconds"])
	require.Len(t, r["tables"], 1)
	table := r["tables"].([]any)[0].(map[string]any)
	assert.Equal(t, "public.users", table["name"])
	assert.EqualValues(t, 3, table["rows_loaded"])
	assert.EqualValues(t, 0, table["rows_skipped"])
	assert.Equal(t, "replace", table["mode"])
}

func TestEventStatsTextWritesStderr(t *testing.T) {
	var out, errb bytes.Buffer
	Init(&out, &errb, ModeText)

	EventStats("run-1", "csv:///tmp/users.csv", "duckdb:///tmp/out.duckdb", 1.5, []map[string]any{
		{"name": "users", "rows_loaded": 3, "rows_skipped": nil, "duration_seconds": 1.25, "mode": "replace"},
	})

	assert.Empty(t, out.String())
	line := strings.TrimSpace(errb.String())
	assert.Contains(t, line, `ingestr_stats run_id="run-1"`)
	assert.Contains(t, line, `source="csv:///tmp/users.csv"`)
	assert.Contains(t, line, `duration_seconds=1.500`)
	assert.Contains(t, line, `"rows_loaded":3`)
}

func TestEnsureTerminalIdempotent(t *testing.T) {
	out, _ := initJSON(t)
	EventEnd("success", 10, 1, 1.0, nil)
	EnsureTerminal(nil)

	recs := parseLines(t, out)
	ends := 0
	for _, r := range recs {
		if r["event"] == "end" {
			ends++
		}
	}
	assert.Equal(t, 1, ends, "EnsureTerminal must not emit a second end event")
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestEnsureTerminalEmitsWhenNone(t *testing.T) {
	out, _ := initJSON(t)
	EnsureTerminal(errors.New("validation failed"))

	recs := parseLines(t, out)
	require.Len(t, recs, 1)
	assert.Equal(t, "end", recs[0]["event"])
	assert.Equal(t, "error", recs[0]["status"])
	assert.Equal(t, "validation failed", recs[0]["error"])
}

func TestEnsureTerminalNoopInTextMode(t *testing.T) {
	var out, errb bytes.Buffer
	Init(&out, &errb, ModeText)
	EnsureTerminal(errors.New("boom"))
	assert.Empty(t, out.String())
	assert.Empty(t, errb.String())
}

// TestEveryLineIsJSON is the contract guard: a realistic mixed sequence must
// produce a stdout stream where every line parses as JSON and carries an event
// discriminator, and decorative Statusf output is absent.
func TestEveryLineIsJSON(t *testing.T) {
	out, _ := initJSON(t)
	EventStart(StartInfo{SourceType: "pg", DestType: "bq", Strategy: "merge"})
	Warnf("Warning: heads up\n")
	EventProgress(10, 1, 5, 1, 0, 0)
	EventProgress(20, 2, 5, 2, 0, 0)
	Statusf("decorative chatter\n")
	EventEnd("success", 20, 2, 2.0, nil)

	recs := parseLines(t, out)
	require.Len(t, recs, 5) // start, log, progress, progress, end — Statusf produced nothing
	for _, r := range recs {
		assert.Contains(t, r, "event")
	}
	assert.Equal(t, "start", recs[0]["event"])
	assert.Equal(t, "log", recs[1]["event"])
	assert.Equal(t, "end", recs[4]["event"])
}
