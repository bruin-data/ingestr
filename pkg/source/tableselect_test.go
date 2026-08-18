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
	if !selection.Includes("anything") {
		t.Fatal("a nil selection includes every table")
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
	if !selection.Includes("dbo.users") {
		t.Fatal("expected a case-insensitive match")
	}
	if selection.Includes("dbo.orders") {
		t.Fatal("did not expect an unrequested table to match")
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
	if !selection.Includes("users") {
		t.Fatal("public.users should canonicalize to users")
	}
	if !selection.Includes("sales.orders") {
		t.Fatal("sales.orders should stay qualified")
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

func TestTableSelectionFilters(t *testing.T) {
	selection, err := NewTableSelection([]string{"users"}, TableSelectionOptions{Subject: "table", Scope: "tables"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := selection.FilterNames([]string{"users", "orders"})
	if len(names) != 1 || names[0] != "users" {
		t.Fatalf("FilterNames = %v, want [users]", names)
	}
	tables := selection.FilterTables([]SourceTableInfo{{Name: "orders"}, {Name: "users"}})
	if len(tables) != 1 || tables[0].Name != "users" {
		t.Fatalf("FilterTables = %v, want [users]", tables)
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
				selection.Includes("users")
				selection.FilterNames([]string{"users", "orders", "invoices"})
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
