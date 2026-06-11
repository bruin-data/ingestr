package postgres

import "testing"

func TestBuildCDCConflictUpdateSet(t *testing.T) {
	unchangedRef := `la."_cdc_unchanged_cols"`
	got := buildCDCConflictUpdateSet([]string{"config_data"}, "target", "la", unchangedRef)
	want := `"config_data" = CASE WHEN la."_cdc_unchanged_cols"::jsonb @> '["config_data"]'::jsonb THEN target."config_data" ELSE la."config_data" END`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCConflictUpdateSetSkipsMetaColumns(t *testing.T) {
	got := buildCDCConflictUpdateSet([]string{"_cdc_lsn"}, "target", "la", `unused`)
	want := `"_cdc_lsn" = la."_cdc_lsn"`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}
