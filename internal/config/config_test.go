package config

import (
	"strings"
	"testing"
	"time"
)

func TestIngestConfigValidate_NoInferenceRequiresColumns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mongodb://localhost:27017/db"
	cfg.SourceTable = "db.users"
	cfg.DestURI = "duckdb://out.duckdb"
	cfg.NoInference = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "columns") {
		t.Fatalf("expected columns validation error, got %v", err)
	}
}

func TestIngestConfigValidate_NoInferenceWithColumns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mongodb://localhost:27017/db"
	cfg.SourceTable = "db.users"
	cfg.DestURI = "duckdb://out.duckdb"
	cfg.Columns = "_id:string,name:string"
	cfg.NoInference = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestIngestConfigValidate_Stream(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		mutate  func(c *IngestConfig)
		wantErr string
	}{
		{
			name:   "valid defaults",
			mutate: func(c *IngestConfig) {},
		},
		{
			name:    "full refresh rejected",
			mutate:  func(c *IngestConfig) { c.FullRefresh = true },
			wantErr: "full-refresh",
		},
		{
			name:    "interval end rejected",
			mutate:  func(c *IngestConfig) { c.IntervalEnd = &now },
			wantErr: "interval-end",
		},
		{
			name:    "sql limit rejected",
			mutate:  func(c *IngestConfig) { c.SQLLimit = 100 },
			wantErr: "sql-limit",
		},
		{
			name:    "non-positive flush interval rejected",
			mutate:  func(c *IngestConfig) { c.FlushInterval = 0 },
			wantErr: "flush-interval",
		},
		{
			name:    "non-positive flush records rejected",
			mutate:  func(c *IngestConfig) { c.FlushRecords = -1 },
			wantErr: "flush-records",
		},
		{
			name:    "replace strategy rejected",
			mutate:  func(c *IngestConfig) { c.IncrementalStrategy = StrategyReplace },
			wantErr: "incremental-strategy",
		},
		{
			name:    "scd2 strategy rejected",
			mutate:  func(c *IngestConfig) { c.IncrementalStrategy = StrategySCD2 },
			wantErr: "incremental-strategy",
		},
		{
			name:   "merge strategy allowed",
			mutate: func(c *IngestConfig) { c.IncrementalStrategy = StrategyMerge },
		},
		{
			name:   "append strategy allowed",
			mutate: func(c *IngestConfig) { c.IncrementalStrategy = StrategyAppend },
		},
		{
			name:   "empty strategy allowed",
			mutate: func(c *IngestConfig) { c.IncrementalStrategy = "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SourceURI = "postgres+cdc://localhost:5432/db"
			cfg.DestURI = "duckdb://out.duckdb"
			cfg.Stream = true
			cfg.IncrementalStrategy = ""
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestIngestConfigValidate_ChangeTrackingRejectsSQLLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.users"
	cfg.DestURI = "duckdb:///tmp/out.duckdb"
	cfg.SQLLimit = 10

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "sql-limit") {
		t.Fatalf("expected sql-limit validation error, got %v", err)
	}
}

func TestIngestConfigValidate_ChangeTrackingRejectsExplicitReplace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.users"
	cfg.DestURI = "duckdb:///tmp/out.duckdb"
	cfg.IncrementalStrategy = StrategyReplace
	cfg.IncrementalStrategyExplicit = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "incremental-strategy") {
		t.Fatalf("expected incremental-strategy validation error, got %v", err)
	}
}

func TestIngestConfigValidate_ChangeTrackingAllowsDefaultReplace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.users"
	cfg.DestURI = "duckdb:///tmp/out.duckdb"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestIngestConfigValidate_ChangeTrackingAllowsExplicitReplaceWithFullRefresh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.users"
	cfg.DestURI = "duckdb:///tmp/out.duckdb"
	cfg.IncrementalStrategy = StrategyReplace
	cfg.IncrementalStrategyExplicit = true
	cfg.FullRefresh = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestIngestConfigValidate_DestTableDefault(t *testing.T) {
	tests := []struct {
		name      string
		sourceURI string
		srcTable  string
		destTable string // pre-set destination; the server path sets this = source table
		wantDest  string
	}{
		{"trello filter, empty dest", "trello://?api_key=k&token=t", "cards:abc,def", "", "cards"},
		{"trello filter, server-defaulted dest", "trello://?api_key=k&token=t", "cards:abc,def", "cards:abc,def", "cards"},
		{"trello no filter", "trello://?api_key=k&token=t", "cards", "", "cards"},
		{"non-trello colon preserved", "monday://?api_token=t", "items:123:linked", "", "items:123:linked"},
		{"explicit different dest untouched", "trello://?api_key=k&token=t", "cards:abc", "main.custom", "main.custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SourceURI = tt.sourceURI
			cfg.SourceTable = tt.srcTable
			cfg.DestTable = tt.destTable
			cfg.DestURI = "duckdb://out.duckdb"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if cfg.DestTable != tt.wantDest {
				t.Errorf("DestTable = %q, want %q", cfg.DestTable, tt.wantDest)
			}
		})
	}
}
