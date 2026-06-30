package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateTrackerJSONMode verifies that ProgressJSON selects the JSON display:
// stopping the tracker emits a terminal end event as JSON (the log/interactive
// displays would not).
func TestCreateTrackerJSONMode(t *testing.T) {
	var out, errb bytes.Buffer
	output.Init(&out, &errb, output.ModeJSON)
	t.Cleanup(func() { output.Init(os.Stdout, os.Stderr, output.ModeText) })

	p := New(&config.IngestConfig{Progress: config.ProgressJSON})
	tr, err := p.createTracker(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tr)
	tr.Stop(nil)

	line := strings.TrimSpace(out.String())
	require.NotEmpty(t, line)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &rec))
	assert.Equal(t, "end", rec["event"])
	assert.Equal(t, "success", rec["status"])
}

func TestCreateTrackerUnknownModeErrors(t *testing.T) {
	p := New(&config.IngestConfig{Progress: config.ProgressMode("bogus")})
	_, err := p.createTracker(context.Background())
	require.Error(t, err)
}
