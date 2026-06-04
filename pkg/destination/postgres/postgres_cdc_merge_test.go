package postgres

import "testing"

func TestBuildCDCConflictUpdateSet(t *testing.T) {
	got := buildCDCConflictUpdateSet([]string{"config_data"}, `"public"."dest"`)
	want := `"config_data" = COALESCE(EXCLUDED."config_data", "public"."dest"."config_data")`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
