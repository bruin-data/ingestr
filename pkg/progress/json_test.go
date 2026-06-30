package progress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseJSONLines(t *testing.T, out *bytes.Buffer) []map[string]any {
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

// TestJSONDisplayEmit exercises the deterministic per-tick seam directly with a
// hand-built Metrics value, avoiding both the ticker and a real collector.
func TestJSONDisplayEmit(t *testing.T) {
	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)

	d := &JSONDisplay{interval: time.Hour, done: make(chan struct{})}
	d.emit(Metrics{TotalRows: 100, TotalBatches: 2, CurrentRowsPerSec: 50, StartTime: time.Now()})

	recs := parseJSONLines(t, &out)
	require.Len(t, recs, 1)
	assert.Equal(t, "progress", recs[0]["event"])
	assert.EqualValues(t, 100, recs[0]["rows"])
	assert.EqualValues(t, 2, recs[0]["batches"])
}

// TestJSONDisplaySequence verifies the display emits progress events then a
// terminal end event, and notably does NOT emit a start event (the pipeline
// banner owns start).
func TestJSONDisplaySequence(t *testing.T) {
	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)

	d := NewJSONDisplay().(*JSONDisplay)
	d.emit(Metrics{TotalRows: 10, StartTime: time.Now()})
	d.emit(Metrics{TotalRows: 20, StartTime: time.Now()})
	d.Stop(Metrics{TotalRows: 20, TotalBatches: 2, StartTime: time.Now().Add(-time.Second)}, nil)

	recs := parseJSONLines(t, &out)
	require.Len(t, recs, 3)
	assert.Equal(t, "progress", recs[0]["event"])
	assert.Equal(t, "progress", recs[1]["event"])
	assert.Equal(t, "end", recs[2]["event"])
	assert.Equal(t, "success", recs[2]["status"])
}

func TestJSONDisplayStopError(t *testing.T) {
	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)

	d := NewJSONDisplay().(*JSONDisplay)
	d.Stop(Metrics{TotalRows: 5, StartTime: time.Now()}, errors.New("boom"))

	recs := parseJSONLines(t, &out)
	require.Len(t, recs, 1)
	assert.Equal(t, "end", recs[0]["event"])
	assert.Equal(t, "error", recs[0]["status"])
	assert.Equal(t, "boom", recs[0]["error"])
}

// TestJSONDisplayNoSpuriousProgress drives the full Start/Stop lifecycle with a
// long ticker interval so no tick fires, proving the lifecycle yields exactly
// one terminal event and no stray progress lines.
func TestJSONDisplayNoSpuriousProgress(t *testing.T) {
	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)

	collector, err := NewMetricsCollector()
	require.NoError(t, err)

	d := &JSONDisplay{interval: time.Hour, done: make(chan struct{})}
	d.Start(context.Background(), collector)
	d.Stop(Metrics{StartTime: time.Now()}, nil)

	recs := parseJSONLines(t, &out)
	require.Len(t, recs, 1)
	assert.Equal(t, "end", recs[0]["event"])
}
