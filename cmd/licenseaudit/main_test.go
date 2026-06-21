package main

import "testing"

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
