//go:build integration

// End-to-end test for --progress json. CSV → DuckDB is fully in-process (no
// container), so it cheaply exercises the real pipeline and asserts the stdout
// stream is clean, machine-readable JSONL.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressJSON_CSVToDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "users.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvWithRows), 0o644))

	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)
	t.Cleanup(func() { output.Init(os.Stdout, os.Stderr, output.ModeText) })

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "users",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.users",
		IncrementalStrategy: config.StrategyReplace,
		Progress:            config.ProgressJSON,
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	var events []string
	var startRec, endRec map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(line), &rec), "non-JSON line on stdout: %s", line)
		ev, ok := rec["event"].(string)
		require.Truef(t, ok && ev != "", "record missing event discriminator: %s", line)
		events = append(events, ev)
		assert.NotContainsf(t, line, "Using staging table", "decorative chatter leaked into JSON: %s", line)
		switch ev {
		case "start":
			startRec = rec
		case "end":
			endRec = rec
		}
	}

	require.NotEmpty(t, events)
	assert.Equal(t, "start", events[0], "first event should be start")
	assert.Equal(t, "end", events[len(events)-1], "last event should be end")

	ends := 0
	for _, e := range events {
		if e == "end" {
			ends++
		}
	}
	assert.Equal(t, 1, ends, "exactly one terminal end event")

	require.NotNil(t, startRec)
	assert.Equal(t, "csv", startRec["source_type"])
	assert.Equal(t, "duckdb", startRec["dest_type"])
	assert.Equal(t, "users", startRec["source_table"])

	require.NotNil(t, endRec)
	assert.Equal(t, "success", endRec["status"])
	assert.EqualValues(t, 3, endRec["rows"], "all three CSV rows counted")
}
