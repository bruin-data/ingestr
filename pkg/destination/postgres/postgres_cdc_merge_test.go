package postgres

import "testing"

func TestBuildCDCConflictUpdateSet(t *testing.T) {
	got := buildCDCConflictUpdateSet([]string{"config_data"}, `"public"."dest"`)
	want := `"config_data" = CASE WHEN EXCLUDED."_cdc_unchanged_cols"::jsonb @> '["config_data"]'::jsonb THEN "public"."dest"."config_data" ELSE EXCLUDED."config_data" END`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCConflictUpdateSetSkipsMetaColumns(t *testing.T) {
	got := buildCDCConflictUpdateSet([]string{"_cdc_lsn"}, `"public"."dest"`)
	want := `"_cdc_lsn" = EXCLUDED."_cdc_lsn"`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}
