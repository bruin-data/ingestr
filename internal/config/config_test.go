package config

import (
	"strings"
	"testing"
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
