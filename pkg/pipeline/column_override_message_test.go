package pipeline

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/schema"
)

// An override on a column with no known source type is reported as "set to",
// not "changed from unknown to ...", which would imply a type mutated when
// nothing was known before.
func TestColumnOverrideMessage_UnknownSourceType(t *testing.T) {
	var out bytes.Buffer
	output.Init(&out, &out, output.ModeText)
	t.Cleanup(func() { output.Init(os.Stdout, os.Stderr, output.ModeText) })

	src := schema.TableSchema{Columns: []schema.Column{{Name: "payload", DataType: schema.TypeUnknown}}}
	p := &Pipeline{config: &config.IngestConfig{Columns: "payload:json"}}
	if err := p.applyColumnOverrides(&src); err != nil {
		t.Fatalf("applyColumnOverrides() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `type set to json`) || strings.Contains(got, "changed from") {
		t.Errorf("unexpected override message: %q", got)
	}
}
