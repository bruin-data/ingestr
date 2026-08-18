package cmd

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
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
