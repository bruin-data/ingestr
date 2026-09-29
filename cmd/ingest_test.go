package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/urfave/cli/v3"
)

func TestParseExtractPartitionInterval(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{input: "1h", want: time.Hour},
		{input: "24h", want: 24 * time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "1w", want: 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, numeric, auto, err := parseExtractPartitionInterval(tt.input)
			if err != nil {
				t.Fatalf("parseExtractPartitionInterval() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("duration = %v, want %v", got, tt.want)
			}
			if numeric != 0 {
				t.Fatalf("numeric interval = %d, want 0", numeric)
			}
			if auto {
				t.Fatal("auto = true, want false")
			}
		})
	}
}

func TestParseExtractPartitionIntervalNumeric(t *testing.T) {
	duration, numeric, auto, err := parseExtractPartitionInterval("10000")
	if err != nil {
		t.Fatalf("parseExtractPartitionInterval() error = %v", err)
	}
	if duration != 0 {
		t.Fatalf("duration = %v, want 0", duration)
	}
	if numeric != 10000 {
		t.Fatalf("numeric interval = %d, want 10000", numeric)
	}
	if auto {
		t.Fatal("auto = true, want false")
	}
}

func TestParseExtractPartitionIntervalAuto(t *testing.T) {
	duration, numeric, auto, err := parseExtractPartitionInterval("auto")
	if err != nil {
		t.Fatalf("parseExtractPartitionInterval() error = %v", err)
	}
	if duration != 0 {
		t.Fatalf("duration = %v, want 0", duration)
	}
	if numeric != 0 {
		t.Fatalf("numeric interval = %d, want 0", numeric)
	}
	if !auto {
		t.Fatal("auto = false, want true")
	}
}

func TestParseExtractPartitionIntervalRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"", "0", "-1", "0h", "-1h", "month", "100000000000d", "100000000000w"} {
		t.Run(input, func(t *testing.T) {
			if _, _, _, err := parseExtractPartitionInterval(input); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestApplyExtractPartitionIntervalDefaultsToAutoWhenPartitionBySet(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ExtractPartitionBy = "created_at"

	if err := applyExtractPartitionInterval(cfg, ""); err != nil {
		t.Fatalf("applyExtractPartitionInterval() error = %v", err)
	}
	if !cfg.ExtractPartitionAuto {
		t.Fatal("ExtractPartitionAuto = false, want true")
	}
	if cfg.ExtractPartitionInterval != 0 {
		t.Fatalf("ExtractPartitionInterval = %v, want 0", cfg.ExtractPartitionInterval)
	}
	if cfg.ExtractPartitionNumericInterval != 0 {
		t.Fatalf("ExtractPartitionNumericInterval = %d, want 0", cfg.ExtractPartitionNumericInterval)
	}
}

func TestApplyExtractPartitionIntervalDoesNotDefaultWithoutPartitionBy(t *testing.T) {
	cfg := config.DefaultConfig()

	if err := applyExtractPartitionInterval(cfg, ""); err != nil {
		t.Fatalf("applyExtractPartitionInterval() error = %v", err)
	}
	if cfg.ExtractPartitionAuto {
		t.Fatal("ExtractPartitionAuto = true, want false")
	}
}

func TestApplySourceTableKeepsCommasForNonCDCSources(t *testing.T) {
	// --source-table carries commas legitimately outside CDC: custom queries
	// and structured SaaS table specs. Splitting those would break them.
	tests := []struct {
		name      string
		sourceURI string
		raw       string
	}{
		{"custom query", "postgres://user:pass@localhost:5432/db", "query:SELECT a, b FROM t"},
		{"facebook ads account list", "facebookads://?access_token=x", "campaigns:1234567890,9876543210"},
		{"fluxx field list", "fluxx://instance?client_id=x", "grant_request:id,name,amount"},
		{"plain table", "postgres://user:pass@localhost:5432/db", "public.users"},
		{"cdc single table", "postgres+cdc://user:pass@localhost:5432/db", "public.users"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI = tt.sourceURI
			if err := applySourceTable(cfg, tt.raw); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.SourceTable != tt.raw {
				t.Fatalf("SourceTable = %q, want %q", cfg.SourceTable, tt.raw)
			}
			if cfg.SourceTables != nil {
				t.Fatalf("SourceTables = %v, want nil", cfg.SourceTables)
			}
		})
	}
}

func TestApplySourceTableSplitsCDCLists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://user:pass@localhost:5432/db"
	if err := applySourceTable(cfg, "public.users, sales.orders"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SourceTable != "" {
		t.Fatalf("SourceTable = %q, want empty so the multi-table path runs", cfg.SourceTable)
	}
	if len(cfg.SourceTables) != 2 || cfg.SourceTables[0] != "public.users" || cfg.SourceTables[1] != "sales.orders" {
		t.Fatalf("SourceTables = %v, want [public.users sales.orders]", cfg.SourceTables)
	}
}

func TestApplySourceTableSingleEntryListStaysSingleTable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mysql+cdc://user:pass@localhost:3306/app"
	if err := applySourceTable(cfg, "users,"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SourceTable != "users" || cfg.SourceTables != nil {
		t.Fatalf("got SourceTable=%q SourceTables=%v, want users and nil", cfg.SourceTable, cfg.SourceTables)
	}
}

func TestApplySourceTableRejectsEmptyList(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://user:pass@localhost:5432/db"
	// Reading this as "every table" would silently widen an intended subset.
	if err := applySourceTable(cfg, " , "); err == nil {
		t.Fatal("expected a list with no names to be rejected")
	}
}

func TestTelemetryTableSelection(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.IngestConfig
		want string
	}{
		{"all", &config.IngestConfig{}, "all"},
		{"single", &config.IngestConfig{SourceTable: "users"}, "single"},
		{"subset", &config.IngestConfig{SourceTables: []string{"users", "orders"}}, "subset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telemetryTableSelection(tt.cfg); got != tt.want {
				t.Fatalf("telemetryTableSelection = %q, want %q", got, tt.want)
			}
		})
	}
}

func unsetReverseETLEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"WRITE_NULLS", "INGESTR_WRITE_NULLS", "REJECT_MODE", "INGESTR_REJECT_MODE"} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
}

func TestReverseETLFlagDefaults(t *testing.T) {
	unsetReverseETLEnv(t)
	tests := []struct {
		args          []string
		wantNulls     bool
		wantNullsSet  bool
		wantRejectSet string
	}{
		{args: nil, wantNulls: true},
		{args: []string{"--write-nulls=false"}, wantNulls: false, wantNullsSet: true},
		{args: []string{"--write-nulls", "--reject-mode", "skip"}, wantNulls: true, wantNullsSet: true, wantRejectSet: "skip"},
	}
	for _, tt := range tests {
		cmd := IngestCommand()
		cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
		cmd.Action = func(_ context.Context, c *cli.Command) error {
			if got := c.Bool("write-nulls"); got != tt.wantNulls {
				t.Errorf("%v: write-nulls = %v, want %v", tt.args, got, tt.wantNulls)
			}
			if got := c.IsSet("write-nulls"); got != tt.wantNullsSet {
				t.Errorf("%v: write-nulls set = %v, want %v", tt.args, got, tt.wantNullsSet)
			}
			if got := c.String("reject-mode"); got != tt.wantRejectSet {
				t.Errorf("%v: reject-mode = %q, want %q", tt.args, got, tt.wantRejectSet)
			}
			return nil
		}
		if err := cmd.Run(context.Background(), append([]string{"ingest", "--source-uri", "csv://a.csv", "--dest-uri", "duckdb:///a.db"}, tt.args...)); err != nil {
			t.Fatalf("%v: Run returned error: %v", tt.args, err)
		}
	}
}

func TestReverseETLFlagHelpShowsDefaults(t *testing.T) {
	unsetReverseETLEnv(t)
	var out bytes.Buffer
	cmd := IngestCommand()
	cmd.Writer = &out
	if err := cmd.Run(context.Background(), []string{"ingest", "--help"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for flag, want := range map[string]string{"--write-nulls": "(default: true)", "--reject-mode value": "(default: fail)"} {
		line := ""
		for _, l := range strings.Split(out.String(), "\n") {
			if strings.Contains(l, flag) {
				line = l
			}
		}
		if !strings.Contains(line, want) {
			t.Errorf("%s help = %q, want %s", flag, line, want)
		}
	}
}

func parseIngestConfig(t *testing.T, args ...string) (*config.IngestConfig, error) {
	t.Helper()
	cfg := config.DefaultConfig()
	cmd := IngestCommand()
	cmd.Writer, cmd.ErrWriter = io.Discard, io.Discard
	cmd.Action = func(_ context.Context, c *cli.Command) error {
		return configureIngest(c, cfg)
	}
	err := cmd.Run(context.Background(), append([]string{
		"ingest", "--source-uri", "csv://input.csv", "--dest-uri", "postgres://localhost/db",
		"--source-table", "input", "--dest-table", "output",
	}, args...))
	return cfg, err
}

func clearIngestMappingEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"EXTRACT_PARALLELISM", "BATCH_SIZE", "INCREMENTAL_STRATEGY", "INTERVAL_START", "INTERVAL_END"} {
		for _, prefix := range []string{"", "INGESTR_"} {
			name := prefix + key
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"INGESTR_STREAM", "INGESTR_FLUSH_INTERVAL", "INGESTR_FLUSH_RECORDS", "INGESTR_METRICS_ADDR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIngestConfigParallelism(t *testing.T) {
	clearIngestMappingEnv(t)
	for _, tt := range []struct {
		name                            string
		args                            []string
		env                             string
		extract, destination, effective int
	}{
		{name: "implicit default", extract: 5, effective: 8},
		{name: "explicit default", args: []string{"--extract-parallelism=5"}, extract: 5, destination: 5, effective: 5},
		{name: "override", args: []string{"--extract-parallelism=3"}, extract: 3, destination: 3, effective: 3},
		{name: "environment default", env: "5", extract: 5, destination: 5, effective: 5},
		{name: "flag beats environment", env: "9", args: []string{"--extract-parallelism=2"}, extract: 2, destination: 2, effective: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("INGESTR_EXTRACT_PARALLELISM", tt.env)
			}
			cfg, err := parseIngestConfig(t, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ExtractParallelism != tt.extract || cfg.DestinationParallelism != tt.destination || cfg.EffectiveDestinationParallelism() != tt.effective {
				t.Fatalf("parallelism = (%d, %d, %d), want (%d, %d, %d)", cfg.ExtractParallelism, cfg.DestinationParallelism, cfg.EffectiveDestinationParallelism(), tt.extract, tt.destination, tt.effective)
			}
		})
	}
}

func TestIngestConfigBatchSize(t *testing.T) {
	clearIngestMappingEnv(t)
	for _, tt := range []struct {
		value string
		bytes int64
		err   string
	}{
		{bytes: 536870912},
		{value: "1", bytes: 1048576},
		{value: "3", bytes: 3145728},
		{value: "8796093022207", bytes: 9223372036853727232},
		{value: "8796093022208", err: "--batch-size is too large"},
		{value: "0", err: "--batch-size must be a positive number"},
		{value: "-1", err: "--batch-size must be a positive number"},
	} {
		t.Run("MiB="+tt.value, func(t *testing.T) {
			var args []string
			if tt.value != "" {
				args = []string{"--batch-size=" + tt.value}
			}
			cfg, err := parseIngestConfig(t, args...)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxBatchBytes != tt.bytes {
				t.Fatalf("bytes = %d, want %d", cfg.MaxBatchBytes, tt.bytes)
			}
		})
	}
}

func TestIngestConfigStream(t *testing.T) {
	clearIngestMappingEnv(t)
	for _, tt := range []struct {
		name             string
		args             []string
		envStrategy      string
		stream, explicit bool
		strategy         config.IncrementalStrategy
		interval         time.Duration
		records          int
		err              string
	}{
		{name: "batch", strategy: config.StrategyReplace, interval: 30 * time.Second, records: 50000},
		{name: "source chooses strategy", args: []string{"--stream"}, stream: true, interval: 30 * time.Second, records: 50000},
		{name: "explicit append", args: []string{"--stream", "--incremental-strategy=append", "--flush-interval=1250ms", "--flush-records=17"}, stream: true, explicit: true, strategy: config.StrategyAppend, interval: 1250 * time.Millisecond, records: 17},
		{name: "environment merge", args: []string{"--stream"}, envStrategy: "merge", stream: true, explicit: true, strategy: config.StrategyMerge, interval: 30 * time.Second, records: 50000},
		{name: "explicit replace is not default", args: []string{"--stream", "--incremental-strategy=replace"}, err: "not supported with --stream"},
		{name: "explicit default interval requires stream", args: []string{"--flush-interval=30s"}, err: "only valid together with --stream"},
		{name: "explicit default records requires stream", args: []string{"--flush-records=50000"}, err: "only valid together with --stream"},
		{name: "metrics requires stream", args: []string{"--metrics-addr=:9090"}, err: "--metrics-addr is only valid"},
		{name: "zero interval", args: []string{"--stream", "--flush-interval=0s"}, err: "flush-interval"},
		{name: "zero records", args: []string{"--stream", "--flush-records=0"}, err: "flush-records"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envStrategy != "" {
				t.Setenv("INGESTR_INCREMENTAL_STRATEGY", tt.envStrategy)
			}
			cfg, err := parseIngestConfig(t, tt.args...)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Stream != tt.stream || cfg.IncrementalStrategy != tt.strategy || cfg.IncrementalStrategyExplicit != tt.explicit || cfg.FlushInterval != tt.interval || cfg.FlushRecords != tt.records {
				t.Fatalf("stream config = (%v, %q, %v, %v, %d), want (%v, %q, %v, %v, %d)", cfg.Stream, cfg.IncrementalStrategy, cfg.IncrementalStrategyExplicit, cfg.FlushInterval, cfg.FlushRecords, tt.stream, tt.strategy, tt.explicit, tt.interval, tt.records)
			}
		})
	}
}

func TestParseDateTime(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  time.Time
	}{
		{"2024-02-29", time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"2024-02-29T12:34:56", time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC)},
		{"2024-02-29 12:34:56", time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC)},
		{"2024-02-29T12:34:56Z", time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC)},
		{"2024-02-29T01:02:03+03:30", time.Date(2024, 2, 28, 21, 32, 3, 0, time.UTC)},
		{"2024-02-29T23:34:56.123-0430", time.Date(2024, 3, 1, 4, 4, 56, 123000000, time.UTC)},
		{"2024-02-29T12:34:56.123456", time.Date(2024, 2, 29, 12, 34, 56, 123456000, time.UTC)},
		{"2024-02-29T12:34:56.123456+02:00", time.Date(2024, 2, 29, 10, 34, 56, 123456000, time.UTC)},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDateTime(tt.input)
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("parseDateTime = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	for _, input := range []string{"", "2023-02-29", "2024-13-01", "2024-02-29T24:00:00", "2024-02-29junk"} {
		t.Run(input, func(t *testing.T) {
			got, err := parseDateTime(input)
			if err == nil || !got.IsZero() {
				t.Fatalf("parseDateTime = %v, %v; want zero and error", got, err)
			}
		})
	}
}

func TestIngestConfigIntervals(t *testing.T) {
	clearIngestMappingEnv(t)
	cfg, err := parseIngestConfig(t, "--interval-start=2024-02-29T01:02:03.123456+03:30", "--interval-end=2024-03-02")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 2, 28, 21, 32, 3, 123456000, time.UTC)
	end := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)
	if cfg.IntervalStart == nil || !cfg.IntervalStart.Equal(start) || cfg.IntervalEnd == nil || !cfg.IntervalEnd.Equal(end) {
		t.Fatalf("interval = %v to %v", cfg.IntervalStart, cfg.IntervalEnd)
	}
	for _, flag := range []string{"interval-start", "interval-end"} {
		_, err := parseIngestConfig(t, "--"+flag+"=2023-02-29")
		if err == nil || !strings.Contains(err.Error(), "invalid "+flag+": could not parse date") {
			t.Fatalf("%s error = %v", flag, err)
		}
	}
}
