package strategy

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
)

func TestGenerateStagingTableName(t *testing.T) {
	tests := []struct {
		name           string
		targetTable    string
		suffix         string
		stagingDataset string
		wantPrefix     string
	}{
		{"schema with no staging dataset", "analytics.users", "merge", "", "_bruin_staging.analytics__users_merge_"},
		{"schema with staging dataset", "analytics.users", "merge", "my_staging", "my_staging.analytics__users_merge_"},
		{"no schema no staging dataset", "users", "merge", "", "_bruin_staging.users_merge_"},
		{"no schema with staging dataset", "users", "merge", "my_staging", "my_staging.users_merge_"},

		{"staging suffix", "analytics.users", "staging", "", "_bruin_staging.analytics__users_staging_"},
		{"staging suffix with dataset", "analytics.users", "staging", "stg", "stg.analytics__users_staging_"},

		{"di suffix", "ds.tbl", "di", "", "_bruin_staging.ds__tbl_di_"},
		{"di suffix with dataset", "ds.tbl", "di", "staging_ds", "staging_ds.ds__tbl_di_"},

		{"scd2 suffix", "ds.tbl", "scd2", "", "_bruin_staging.ds__tbl_scd2_"},
		{"scd2 suffix with dataset", "ds.tbl", "scd2", "staging_ds", "staging_ds.ds__tbl_scd2_"},
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

func TestGenerateReplaceStagingTableName(t *testing.T) {
	tests := []struct {
		name           string
		targetTable    string
		stagingDataset string
		policy         destination.ReplaceStagingPolicy
		wantPrefix     string
	}{
		{
			name:        "default managed schema",
			targetTable: "analytics.users",
			wantPrefix:  "_bruin_staging.analytics__users_staging_",
		},
		{
			name:        "target schema placement",
			targetTable: "analytics.users",
			policy: destination.ReplaceStagingPolicy{
				DefaultPlacement: destination.ReplaceStagingTargetSchema,
			},
			wantPrefix: "analytics.users_staging_",
		},
		{
			name:        "target schema placement with unqualified target",
			targetTable: "users",
			policy: destination.ReplaceStagingPolicy{
				DefaultPlacement:    destination.ReplaceStagingTargetSchema,
				DefaultTargetSchema: "main",
			},
			wantPrefix: "main.users_staging_",
		},
		{
			name:           "explicit staging dataset with target policy",
			targetTable:    "analytics.users",
			stagingDataset: "scratch",
			policy: destination.ReplaceStagingPolicy{
				DefaultPlacement: destination.ReplaceStagingTargetSchema,
			},
			wantPrefix: "scratch.analytics__users_staging_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateReplaceStagingTableName(tt.targetTable, "staging", tt.stagingDataset, tt.policy)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Fatalf("GenerateReplaceStagingTableName(%q, %q) = %q, want prefix %q",
					tt.targetTable, tt.stagingDataset, got, tt.wantPrefix)
			}
		})
	}
}
