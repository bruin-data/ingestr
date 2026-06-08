package postgres

import "testing"

func TestBuildCDCConflictUpdateSet(t *testing.T) {
	unchangedRef := `(SELECT la."_cdc_unchanged_cols" FROM latest_active la WHERE la."id" = EXCLUDED."id")`
	got := buildCDCConflictUpdateSet([]string{"config_data"}, `"public"."dest"`, unchangedRef)
	want := `"config_data" = CASE WHEN (SELECT la."_cdc_unchanged_cols" FROM latest_active la WHERE la."id" = EXCLUDED."id")::jsonb @> '["config_data"]'::jsonb THEN "public"."dest"."config_data" ELSE EXCLUDED."config_data" END`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCConflictUpdateSetSkipsMetaColumns(t *testing.T) {
	got := buildCDCConflictUpdateSet([]string{"_cdc_lsn"}, `"public"."dest"`, `unused`)
	want := `"_cdc_lsn" = EXCLUDED."_cdc_lsn"`
	if got != want {
		t.Fatalf("buildCDCConflictUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCUnchangedColsRef(t *testing.T) {
	got := buildCDCUnchangedColsRef([]string{"id", "tenant_id"})
	want := `(SELECT la."_cdc_unchanged_cols" FROM latest_active la WHERE la."id" = EXCLUDED."id" AND la."tenant_id" = EXCLUDED."tenant_id")`
	if got != want {
		t.Fatalf("buildCDCUnchangedColsRef() = %q, want %q", got, want)
	}
}
