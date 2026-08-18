package source

import (
	"fmt"
	"strings"
	"testing"
)

// stripPublicSchema stands in for a source canonicalizer: it folds an explicit
// "public." prefix away and rejects a malformed name.
func stripPublicSchema(name string) (string, error) {
	if strings.Contains(name, " ") {
		return "", fmt.Errorf("malformed table name %q", name)
	}
	return strings.TrimPrefix(name, "public."), nil
}

func TestTableSelectionEmptySelectsEverything(t *testing.T) {
	selection, err := NewTableSelection(nil, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selection != nil {
		t.Fatal("no requested names should yield a nil selection")
	}
	if !selection.Empty() {
		t.Fatal("a nil selection is empty")
	}
	resolved, err := selection.Resolve([]string{"anything", "else"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("a nil selection resolves to every table, got %v", resolved)
	}
	if err := selection.Validate(nil, nil); err != nil {
		t.Fatalf("a nil selection never fails validation: %v", err)
	}
}

func TestTableSelectionMatchesCaseInsensitively(t *testing.T) {
	selection, err := NewTableSelection([]string{"DBO.Users"}, TableSelectionOptions{
		Subject: "SQL Server CDC table",
		Scope:   "SQL Server CDC-enabled tables",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, err := selection.Resolve([]string{"dbo.users", "dbo.orders"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resolved["dbo.users"]; !ok {
		t.Fatal("expected a case-insensitive match")
	}
	if _, ok := resolved["dbo.orders"]; ok {
		t.Fatal("did not expect an unrequested table to match")
	}
}

// Case-insensitive matching must not pull in a second, distinctly-cased table.
func TestTableSelectionPrefersExactSpelling(t *testing.T) {
	selection, err := NewTableSelection([]string{"users"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, err := selection.Resolve([]string{"users", "Users"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %v, want only the exactly-named table", resolved)
	}
	if _, ok := resolved["users"]; !ok {
		t.Fatalf("resolved = %v, want users", resolved)
	}

	// The other spelling is selectable in its own right.
	upper, err := NewTableSelection([]string{"Users"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, err = upper.Resolve([]string{"users", "Users"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resolved["Users"]; !ok || len(resolved) != 1 {
		t.Fatalf("resolved = %v, want only Users", resolved)
	}
}

func TestTableSelectionRejectsUnresolvableCaseCollision(t *testing.T) {
	selection, err := NewTableSelection([]string{"USERS"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Neither discovered table is spelled the way the user asked, so picking
	// one would be a guess and picking both would ingest an unrequested table.
	_, err = selection.Resolve([]string{"users", "Users"})
	if err == nil {
		t.Fatal("expected an ambiguous request to be rejected")
	}
	if !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "Users") {
		t.Fatalf("error should name both candidates, got %v", err)
	}
}

func TestTableSelectionCanonicalizes(t *testing.T) {
	selection, err := NewTableSelection([]string{"public.users", "sales.orders"}, TableSelectionOptions{
		Subject:      "table",
		Scope:        "tables",
		Canonicalize: stripPublicSchema,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, err := selection.Resolve([]string{"users", "sales.orders", "invoices"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resolved["users"]; !ok {
		t.Fatal("public.users should canonicalize to users")
	}
	if _, ok := resolved["sales.orders"]; !ok {
		t.Fatal("sales.orders should stay qualified")
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %v, want exactly the two requested tables", resolved)
	}
}

func TestTableSelectionRejectsBadName(t *testing.T) {
	_, err := NewTableSelection([]string{"a b"}, TableSelectionOptions{
		Subject:      "table",
		Scope:        "tables",
		Canonicalize: stripPublicSchema,
	})
	if err == nil {
		t.Fatal("expected the canonicalizer's error to surface")
	}
}

func TestTableSelectionRejectsDuplicates(t *testing.T) {
	_, err := NewTableSelection([]string{"users", "users"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected a repeated-entry error, got %v", err)
	}

	// Two spellings collapse only after canonicalization; both must be named.
	_, err = NewTableSelection([]string{"public.users", "users"}, TableSelectionOptions{
		Subject:      "table",
		Scope:        "tables",
		Canonicalize: stripPublicSchema,
	})
	if err == nil {
		t.Fatal("expected post-canonicalization duplicates to be rejected")
	}
	if !strings.Contains(err.Error(), "public.users") || !strings.Contains(err.Error(), "users") {
		t.Fatalf("error should name both spellings, got %v", err)
	}
}

func TestTableSelectionValidateReportsMissing(t *testing.T) {
	selection, err := NewTableSelection([]string{"dbo.users", "dbo.missing", "dbo.absent"}, TableSelectionOptions{
		Subject: "SQL Server CDC table",
		Scope:   "SQL Server CDC-enabled tables",
		Hint:    "use schema-qualified names (e.g. dbo.users)",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = selection.Validate([]string{"dbo.users"}, nil)
	if err == nil {
		t.Fatal("expected an error for tables that matched nothing")
	}
	// Deterministic, sorted, and pointing at the format.
	want := "tables not found among SQL Server CDC-enabled tables: dbo.absent, dbo.missing; use schema-qualified names (e.g. dbo.users)"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestTableSelectionValidatePrefersIneligibleReason(t *testing.T) {
	selection, err := NewTableSelection([]string{"dbo.audit_log"}, TableSelectionOptions{
		Subject: "SQL Server CDC table",
		Scope:   "SQL Server CDC-enabled tables",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = selection.Validate(nil, map[string]string{"DBO.Audit_Log": "it has no primary key"})
	if err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("expected the exclusion reason, got %v", err)
	}
}

// The discovery timer and the stream-rebuild path consult one selection
// concurrently, so matching must not mutate it.
func TestTableSelectionIsSafeForConcurrentUse(t *testing.T) {
	selection, err := NewTableSelection([]string{"users", "orders"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				_, _ = selection.Resolve([]string{"users", "orders", "invoices"})
				_ = selection.Validate([]string{"users", "orders"}, nil)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if err := selection.Validate([]string{"users", "orders"}, nil); err != nil {
		t.Fatalf("selection should be unchanged after matching: %v", err)
	}
}
