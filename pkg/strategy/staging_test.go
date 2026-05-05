package strategy

import (
	"strings"
	"testing"
)

func TestGenerateStagingTableName(t *testing.T) {
	tests := []struct {
		name           string
		targetTable    string
		suffix         string
		stagingDataset string
		wantPrefix     string
	}{
		{"schema with no staging dataset", "analytics.users", "merge", "", "analytics.users_merge_"},
		{"schema with staging dataset", "analytics.users", "merge", "my_staging", "my_staging.users_merge_"},
		{"no schema no staging dataset", "users", "merge", "", "users_merge_"},
		{"no schema with staging dataset", "users", "merge", "my_staging", "my_staging.users_merge_"},

		{"staging suffix", "analytics.users", "staging", "", "analytics.users_staging_"},
		{"staging suffix with dataset", "analytics.users", "staging", "stg", "stg.users_staging_"},

		{"di suffix", "ds.tbl", "di", "", "ds.tbl_di_"},
		{"di suffix with dataset", "ds.tbl", "di", "staging_ds", "staging_ds.tbl_di_"},

		{"scd2 suffix", "ds.tbl", "scd2", "", "ds.tbl_scd2_"},
		{"scd2 suffix with dataset", "ds.tbl", "scd2", "staging_ds", "staging_ds.tbl_scd2_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateStagingTableName(tt.targetTable, tt.suffix, tt.stagingDataset)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Fatalf("GenerateStagingTableName(%q, %q, %q) = %q, want prefix %q",
					tt.targetTable, tt.suffix, tt.stagingDataset, got, tt.wantPrefix)
			}
			if strings.HasSuffix(got, "_") {
				t.Fatalf("unexpected trailing underscore: %q", got)
			}
		})
	}
}
