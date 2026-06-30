// Package output centralizes all user-facing process output for the ingestr CLI.
//
// It exposes a single seam through which warnings, informational lines, lifecycle
// events, and debug logging flow. In text mode (the default) it reproduces the
// historical fmt/color behavior verbatim. In JSON mode it emits one structured
// JSON object per line to stdout via log/slog, so jq pipelines and orchestrators
// can consume a clean, machine-readable stream. Debug output goes to stderr in
// JSON mode so stdout stays pure JSON.
//
// This package is a leaf: it imports only the standard library so that lower-level
// packages (e.g. internal/config) can delegate to it without creating an import
// cycle. It deliberately defines its own Mode type rather than referencing
// config.ProgressMode.
package output

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// Mode selects how output is rendered.
type Mode int

const (
	// ModeText renders human-readable output (the historical behavior).
	ModeText Mode = iota
	// ModeJSON renders one JSON object per line to stdout.
	ModeJSON
)

// Event discriminator values written as the "event" attribute of every JSON record.
const (
	eventStart    = "start"
	eventProgress = "progress"
	eventEnd      = "end"
	eventLog      = "log"
)

var (
	stdoutW io.Writer = os.Stdout
	stderrW io.Writer = os.Stderr
	mode              = ModeText
	logger            = newJSONLogger(os.Stdout)

	// terminalEmitted guards the "exactly one terminal end event" invariant.
	terminalEmitted atomic.Bool
)

// newJSONLogger builds a slog logger that writes JSON lines to w, renaming the
// built-in "time" attribute to "ts". No buffering, so os.Exit cannot drop a line.
func newJSONLogger(w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			return a
		},
	})
	return slog.New(h)
}

// Init configures the output writers and mode. It must be called once, early,
// before any concurrent output is produced. Tests inject buffers via this entry
// point. If Init is never called (e.g. the server command), the package defaults
// to text mode on os.Stdout/os.Stderr.
func Init(stdout, stderr io.Writer, m Mode) {
	stdoutW = stdout
	stderrW = stderr
	mode = m
	logger = newJSONLogger(stdout)
	terminalEmitted.Store(false)
}

// IsJSON reports whether output is in JSON mode.
func IsJSON() bool { return mode == ModeJSON }

// Warnf reports a warning. Text mode: written verbatim to stdout. JSON mode: a
// {"event":"log","level":"WARN",...} record.
func Warnf(format string, args ...any) { emitLog(slog.LevelWarn, fmt.Sprintf(format, args...)) }

// Infof reports an informational line (e.g. a schema-change notice).
func Infof(format string, args ...any) { emitLog(slog.LevelInfo, fmt.Sprintf(format, args...)) }

// Errorf reports an error line.
func Errorf(format string, args ...any) { emitLog(slog.LevelError, fmt.Sprintf(format, args...)) }

// Statusf reports decorative status chatter (e.g. staging-table messages). Text
// mode: written verbatim to stdout. JSON mode: suppressed, since the structured
// lifecycle events already convey progress.
func Statusf(format string, args ...any) {
	if mode == ModeJSON {
		return
	}
	_, _ = fmt.Fprintf(stdoutW, format, args...)
}

func emitLog(level slog.Level, msg string) {
	if mode != ModeJSON {
		_, _ = fmt.Fprint(stdoutW, msg)
		return
	}
	logger.LogAttrs(context.Background(), level, strings.TrimRight(msg, "\n"),
		slog.String("event", eventLog))
}

// StartInfo carries the fields of the start event. Primitive types only, so this
// package stays a leaf.
type StartInfo struct {
	SourceType     string
	DestType       string
	SourceTable    string
	DestTable      string
	Strategy       string
	IncrementalKey string
	PrimaryKey     []string
	SchemaNaming   string
}

// EventStart emits the start event (JSON mode only).
func EventStart(i StartInfo) {
	if mode != ModeJSON {
		return
	}
	attrs := []slog.Attr{
		slog.String("event", eventStart),
		slog.String("source_type", i.SourceType),
		slog.String("dest_type", i.DestType),
		slog.String("source_table", i.SourceTable),
		slog.String("dest_table", i.DestTable),
		slog.String("strategy", i.Strategy),
	}
	if i.IncrementalKey != "" {
		attrs = append(attrs, slog.String("incremental_key", i.IncrementalKey))
	}
	if len(i.PrimaryKey) > 0 {
		attrs = append(attrs, slog.Any("primary_key", i.PrimaryKey))
	}
	if i.SchemaNaming != "" {
		attrs = append(attrs, slog.String("schema_naming", i.SchemaNaming))
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, "ingestion started", attrs...)
}

// EventProgress emits a periodic progress event (JSON mode only).
func EventProgress(rows, batches int64, rowsPerSec, elapsedSec, cpuPercent, memMB float64) {
	if mode != ModeJSON {
		return
	}
	logger.LogAttrs(
		context.Background(), slog.LevelInfo, "progress",
		slog.String("event", eventProgress),
		slog.Int64("rows", rows),
		slog.Int64("batches", batches),
		slog.Float64("rows_per_sec", rowsPerSec),
		slog.Float64("elapsed_sec", elapsedSec),
		slog.Float64("cpu_percent", cpuPercent),
		slog.Float64("mem_mb", memMB),
	)
}

// EventEnd emits the terminal end event (JSON mode only) and records that a
// terminal event has been emitted. A non-nil err makes the event ERROR-level
// with status "error"; otherwise it is INFO-level with status "success".
func EventEnd(status string, rows, batches int64, durationSec float64, err error) {
	if mode != ModeJSON {
		return
	}
	level := slog.LevelInfo
	msg := "ingestion completed"
	var errVal any
	if err != nil {
		level = slog.LevelError
		msg = "ingestion failed"
		errVal = err.Error()
	}
	logger.LogAttrs(
		context.Background(), level, msg,
		slog.String("event", eventEnd),
		slog.String("status", status),
		slog.Int64("rows", rows),
		slog.Int64("batches", batches),
		slog.Float64("duration_sec", durationSec),
		slog.Any("error", errVal),
	)
	terminalEmitted.Store(true)
}

// EnsureTerminal emits a terminal end event only if none has been emitted yet.
// It covers exit paths that occur before a progress tracker exists (config
// validation, source/destination connection, zero-table runs). It is idempotent
// and a no-op in text mode.
func EnsureTerminal(err error) {
	if mode != ModeJSON || terminalEmitted.Load() {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	EventEnd(status, 0, 0, 0, err)
}

// WriteDebug writes a preformatted debug line: to stderr in JSON mode (keeping
// stdout pure JSON), to stdout otherwise. Callers are responsible for gating on
// whether debug is enabled.
func WriteDebug(line string) {
	if mode == ModeJSON {
		_, _ = fmt.Fprint(stderrW, line)
		return
	}
	_, _ = fmt.Fprint(stdoutW, line)
}
