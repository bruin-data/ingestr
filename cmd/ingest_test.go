package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/urfave/cli/v3"
)

func TestColumnsHelpDocumentsDecimalPrecisionOnlyScale(t *testing.T) {
	command := IngestCommand()
	for _, flag := range command.Flags {
		if flag.Names()[0] != "columns" {
			continue
		}
		help := flag.(*cli.StringFlag).Usage
		if !strings.Contains(help, "decimal(p)") || !strings.Contains(help, "decimal(p,0)") {
			t.Fatalf("columns help does not document decimal(p) scale semantics: %q", help)
		}
		return
	}
	t.Fatal("columns flag not found")
}

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
