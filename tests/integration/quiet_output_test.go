//go:build integration || stress

package integration

import (
	"io"
	"os"

	"github.com/bruin-data/ingestr/internal/output"
	tclog "github.com/testcontainers/testcontainers-go/log"
)

// quietTCLogger silences testcontainers' Docker lifecycle logs, which it enables
// whenever the test binary is run with -v.
type quietTCLogger struct{}

func (quietTCLogger) Printf(string, ...any) {}

// init keeps `go test -v` output readable across both the integration and stress
// binaries: when INGESTR_QUIET_PROGRESS is set (the Makefile test targets),
// discard ingestr's own status output and testcontainers' Docker logs so the
// per-test PASS/FAIL and timing lines stay visible.
func init() {
	if os.Getenv("INGESTR_QUIET_PROGRESS") != "" {
		output.Init(io.Discard, io.Discard, output.ModeText)
		tclog.SetDefault(quietTCLogger{})
	}
}
