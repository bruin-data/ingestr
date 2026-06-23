package main

import (
	"strings"
	"testing"
)

func TestMergeDependenciesAllowsDefaultLicenses(t *testing.T) {
	deps := mergeDependencies(nil, []scanEntry{
		{Module: "example.com/apache", Version: "v1.0.0", Licenses: []string{"Apache-2.0"}},
		{Module: "example.com/mixed", Version: "v1.0.0", Licenses: []string{"BSD-3-Clause", "MIT"}},
	}, "needs-review")

	for _, dep := range deps {
		if dep.Status != "allowed" {
			t.Fatalf("%s status = %q, want allowed", dep.Module, dep.Status)
		}
		if dep.Note != "" {
			t.Fatalf("%s note = %q, want empty", dep.Module, dep.Note)
		}
	}
}

func TestMergeDependenciesNeedsReviewForUnexpectedLicense(t *testing.T) {
	deps := mergeDependencies(nil, []scanEntry{
		{Module: "example.com/custom", Version: "v1.0.0", Licenses: []string{"Custom"}},
	}, "needs-review")

	if got, want := deps[0].Status, "needs-review"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if deps[0].Note == "" {
		t.Fatal("expected review note")
	}
}

func TestMergeDependenciesAllowsChangedDefaultLicense(t *testing.T) {
	deps := mergeDependencies([]dependencyReview{
		{Module: "example.com/lib", Version: "v1.0.0", Licenses: []string{"MIT"}, Status: "allowed"},
	}, []scanEntry{
		{Module: "example.com/lib", Version: "v1.1.0", Licenses: []string{"Apache-2.0"}},
	}, "needs-review")

	if got, want := deps[0].Status, "allowed"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if deps[0].Note != "" {
		t.Fatalf("note = %q, want empty", deps[0].Note)
	}
}

func TestMergeDependenciesRequiresReviewWhenManualReviewChanges(t *testing.T) {
	deps := mergeDependencies([]dependencyReview{
		{Module: "example.com/lib", Version: "v1.0.0", Licenses: []string{"Custom"}, Status: "manual-review"},
	}, []scanEntry{
		{Module: "example.com/lib", Version: "v1.1.0", Licenses: []string{"MIT"}},
	}, "needs-review")

	if got, want := deps[0].Status, "needs-review"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if deps[0].Note == "" {
		t.Fatal("expected review note")
	}
}

func TestMergeDependenciesUsesFallbackForChangedUnexpectedLicense(t *testing.T) {
	deps := mergeDependencies([]dependencyReview{
		{Module: "example.com/lib", Version: "v1.0.0", Licenses: []string{"MIT"}, Status: "allowed"},
	}, []scanEntry{
		{Module: "example.com/lib", Version: "v1.1.0", Licenses: []string{"Custom"}},
	}, "allowed")

	if got, want := deps[0].Status, "allowed"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if deps[0].Note != "" {
		t.Fatalf("note = %q, want empty", deps[0].Note)
	}
}

func TestValidateManualAuditRequiresLicenseName(t *testing.T) {
	err := validateManualAudit(manualAudit{
		Module:        "example.com/lib",
		Version:       "v1.0.0",
		LicenseFile:   "LICENSE",
		LicenseSHA256: "abc123",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckLockReportsStaleManualAudit(t *testing.T) {
	err := checkLock(&lockFile{
		ManualAudits: []manualAudit{{
			Module:        "example.com/not-selected",
			Version:       "v1.0.0",
			License:       "MIT",
			LicenseFile:   "LICENSE",
			LicenseSHA256: "abc123",
			Status:        "manual-review",
		}},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !containsString(got, "manual audit for example.com/not-selected is stale") {
		t.Fatalf("error = %q, want stale manual audit message", got)
	}
}

func TestModuleNotSelected(t *testing.T) {
	if !moduleNotSelected("go: module example.com/not-selected: not a known dependency") {
		t.Fatal("expected not selected error to be recognized")
	}
	if moduleNotSelected("go: proxy error") {
		t.Fatal("expected unrelated error not to be treated as not selected")
	}
}

func containsString(value, substr string) bool {
	return strings.Contains(value, substr)
}
