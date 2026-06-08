package postgres

import "testing"

func TestBuildCDCUpdateSet(t *testing.T) {
	got := buildCDCUpdateSet([]string{"config_data"}, "target", "latest_active")
	want := `"config_data" = CASE WHEN latest_active."_cdc_unchanged_cols"::jsonb @> '["config_data"]'::jsonb THEN target."config_data" ELSE latest_active."config_data" END`
	if got != want {
		t.Fatalf("buildCDCUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCUpdateSetSkipsMetaColumns(t *testing.T) {
	got := buildCDCUpdateSet([]string{"_cdc_lsn"}, "target", "latest_active")
	want := `"_cdc_lsn" = latest_active."_cdc_lsn"`
	if got != want {
		t.Fatalf("buildCDCUpdateSet() = %q, want %q", got, want)
	}
}

func TestBuildCDCUpdateSetSkipsStagingOnlyColumns(t *testing.T) {
	got := buildCDCUpdateSet([]string{"_cdc_unchanged_cols"}, "target", "latest_active")
	if got != "" {
		t.Fatalf("buildCDCUpdateSet() = %q, want empty (staging-only column must be skipped)", got)
	}
}
