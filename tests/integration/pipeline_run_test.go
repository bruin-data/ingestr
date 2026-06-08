//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/pkg/pipeline"
)

// stdoutCaptureMu serializes os.Stdout redirection so concurrent (t.Parallel)
// pipeline runs don't race on the process-global stream.
var stdoutCaptureMu sync.Mutex

// runPipeline runs p while capturing everything it (and the packages it calls)
// prints to stdout. The captured output is attached to the test log, which
// `go test` only surfaces when the test fails or when run with -v — keeping
// passing-test logs free of pipeline progress noise.
func runPipeline(t *testing.T, ctx context.Context, p *pipeline.Pipeline) error {
	t.Helper()
	out, err := captureStdout(func() error { return p.Run(ctx) })
	if s := strings.TrimSpace(out); s != "" {
		t.Logf("pipeline output:\n%s", s)
	}
	return err
}

func captureStdout(fn func() error) (out string, err error) {
	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		return "", fn()
	}

	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	// Restore stdout and drain the pipe even if fn panics.
	defer func() {
		os.Stdout = orig
		_ = w.Close()
		<-done
		_ = r.Close()
		out = buf.String()
	}()

	return "", fn()
}
