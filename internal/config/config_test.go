package config

import (
	"strings"
	"testing"
)

func TestSetDebugMaskValues_DropsShortValues(t *testing.T) {
	defer SetDebugMaskValues(nil)
	SetDebugMaskValues([]string{"short", "longenoughsecret"})
	got := maskDebugMessage("short and longenoughsecret here")
	if !strings.Contains(got, "short") {
		t.Errorf("expected short value to be left alone, got %q", got)
	}
	if strings.Contains(got, "longenoughsecret") {
		t.Errorf("expected long secret to be masked, got %q", got)
	}
}

func TestSetDebugMaskValues_LongestFirst(t *testing.T) {
	defer SetDebugMaskValues(nil)
	// "supersecret" is a prefix of "supersecretvalue"; longest-first
	// ordering ensures the longer match wins.
	SetDebugMaskValues([]string{"supersecret", "supersecretvalue"})
	got := maskDebugMessage("here is supersecretvalue")
	if got != "here is ***" {
		t.Errorf("expected longest match to be replaced fully, got %q", got)
	}
}

func TestMaskDebugMessage_NoMasksNoOp(t *testing.T) {
	defer SetDebugMaskValues(nil)
	SetDebugMaskValues(nil)
	in := "some debug line with no secrets"
	if got := maskDebugMessage(in); got != in {
		t.Errorf("expected no-op, got %q", got)
	}
}

func TestMaskDebugMessage_MultipleOccurrences(t *testing.T) {
	defer SetDebugMaskValues(nil)
	SetDebugMaskValues([]string{"hunter2hunter2"})
	got := maskDebugMessage("connect with hunter2hunter2 and again hunter2hunter2")
	want := "connect with *** and again ***"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

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
