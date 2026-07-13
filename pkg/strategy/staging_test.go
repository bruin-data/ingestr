package strategy

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/tablename"
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

func TestSyntheticStagingIdentifierEncodesQualificationAndAmbiguity(t *testing.T) {
	if got := syntheticStagingIdentifier("analytics", "users"); got != "analytics__users" {
		t.Fatalf("ordinary staging identifier changed: %q", got)
	}

	tests := [][]string{
		{"sales.schema", "order.events"},
		{"analytics__users"},
		{"_ingestr_hex_616263"},
		{"Case Schema", "orders"},
	}
	seen := map[string]bool{"analytics__users": true}
	for _, parts := range tests {
		got := syntheticStagingIdentifier(parts...)
		if !strings.HasPrefix(got, encodedStagingIdentifierPrefix) {
			t.Fatalf("syntheticStagingIdentifier(%q) = %q, want encoded prefix", parts, got)
		}
		if strings.Contains(got, ".") {
			t.Fatalf("encoded staging identifier contains a qualifier: %q", got)
		}
		if seen[got] {
			t.Fatalf("synthetic staging identifier collision for %q: %q", parts, got)
		}
		seen[got] = true
	}
}

func TestQuotedDotsRemainPlacementOnlyAcrossStagingPaths(t *testing.T) {
	target := `"appUser"."order.events"`
	targetPolicy := destination.ReplaceStagingPolicy{
		DefaultPlacement:    destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema: `"appUser"`,
	}
	managedPolicy := destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingManagedSchema,
		DefaultManagedSchema: DefaultStagingSchema,
	}
	targetDest := &fakeManagedStagingPolicyProvider{fakeDestination: &fakeDestination{}, policy: targetPolicy}

	tests := []struct {
		name       string
		got        string
		wantSchema string
		wantSuffix string
	}{
		{name: "regular managed schema", got: GenerateStagingTableName(target, "merge", ""), wantSchema: DefaultStagingSchema, wantSuffix: "_merge_"},
		{name: "replace target schema", got: GenerateReplaceStagingTableName(target, "staging", "", targetPolicy), wantSchema: `"appUser"`, wantSuffix: "_staging_"},
		{name: "replace managed schema", got: GenerateReplaceStagingTableName(target, "staging", "", managedPolicy), wantSchema: DefaultStagingSchema, wantSuffix: "_staging_"},
		{name: "normalised target schema", got: GenerateNormalisedStagingTableName(target, ""), wantSchema: `"appUser"`, wantSuffix: "_staging_normalised_"},
		{name: "merge target schema", got: managedStagingTableName(targetDest, target, "merge", ""), wantSchema: `"appUser"`, wantSuffix: "_merge_"},
		{name: "streaming CDC target schema", got: managedStagingTableName(targetDest, target, "stream", ""), wantSchema: `"appUser"`, wantSuffix: "_stream_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := tablename.SplitRaw(tt.got)
			if len(parts) != 2 {
				t.Fatalf("staging table %q has %d components, want schema.table", tt.got, len(parts))
			}
			if parts[0] != tt.wantSchema {
				t.Fatalf("staging table %q schema = %q, want %q", tt.got, parts[0], tt.wantSchema)
			}
			if !strings.HasPrefix(parts[1], encodedStagingIdentifierPrefix) || !strings.Contains(parts[1], tt.wantSuffix) {
				t.Fatalf("staging table component %q is not a safely encoded %q table", parts[1], tt.wantSuffix)
			}
			if strings.Contains(parts[1], ".") {
				t.Fatalf("generated table component still contains a qualifier: %q", parts[1])
			}
		})
	}

	legacy := legacyManagedCDCStateTableName(targetDest, target, "cdc_state", "")
	if got := tablename.SplitRaw(legacy); len(got) != 2 || got[0] != `"appUser"` || got[1] != "cdc_state" {
		t.Fatalf("legacy staging state placement changed: %q", legacy)
	}
}

func TestQuotedCatalogAndStagingSchemaRemainPlacementComponents(t *testing.T) {
	got := GenerateStagingTableName(`"catalog.name"."app.schema"."order.events"`, "merge", `"stage.schema"`)
	parts := tablename.SplitRaw(got)
	if len(parts) != 3 {
		t.Fatalf("staging table %q has %d components, want catalog.schema.table", got, len(parts))
	}
	if parts[0] != `"catalog.name"` || parts[1] != `"stage.schema"` {
		t.Fatalf("staging placement changed: %q", got)
	}
	if !strings.HasPrefix(parts[2], encodedStagingIdentifierPrefix) || strings.Contains(parts[2], ".") {
		t.Fatalf("generated staging component is not safely encoded: %q", parts[2])
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
			name:        "quoted target schema placement preserves reference",
			targetTable: `"appUser".orders`,
			policy: destination.ReplaceStagingPolicy{
				DefaultPlacement: destination.ReplaceStagingTargetSchema,
			},
			wantPrefix: `"appUser".orders_staging_`,
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
		{
			name:           "explicit staging dataset matching target schema",
			targetTable:    "analytics.users",
			stagingDataset: "analytics",
			policy: destination.ReplaceStagingPolicy{
				DefaultPlacement: destination.ReplaceStagingTargetSchema,
			},
			wantPrefix: "analytics.users_staging_",
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

type fakeManagedStagingPolicyProvider struct {
	*fakeDestination

	policy destination.ReplaceStagingPolicy
}

type fakeManagedCDCStateCatalogProvider struct {
	*fakeManagedStagingPolicyProvider
	catalog string
}

func (d *fakeManagedCDCStateCatalogProvider) ManagedCDCStateCatalog() string {
	return d.catalog
}

func (d *fakeManagedStagingPolicyProvider) ManagedStagingPolicy() destination.ReplaceStagingPolicy {
	return d.policy
}

func (d *fakeManagedStagingPolicyProvider) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return d.policy
}

func TestManagedStagingTableName_UsesDestinationPolicy(t *testing.T) {
	dest := &fakeManagedStagingPolicyProvider{
		fakeDestination: &fakeDestination{},
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: "app",
		},
	}

	got := managedStagingTableName(dest, "users", "merge", "")
	if !strings.HasPrefix(got, "app.users_merge_") {
		t.Fatalf("managedStagingTableName() = %q, want prefix %q", got, "app.users_merge_")
	}
}

func TestManagedStagingTableName_ExplicitDatasetOverridesDestinationPolicy(t *testing.T) {
	dest := &fakeManagedStagingPolicyProvider{
		fakeDestination: &fakeDestination{},
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: "app",
		},
	}

	got := managedStagingTableName(dest, "analytics.users", "merge", "scratch")
	if !strings.HasPrefix(got, "scratch.analytics__users_merge_") {
		t.Fatalf("managedStagingTableName() = %q, want prefix %q", got, "scratch.analytics__users_merge_")
	}
}

func TestOracleStyleQuotedSchemaSurvivesEveryManagedStagingPath(t *testing.T) {
	dest := &fakeManagedStagingPolicyProvider{
		fakeDestination: &fakeDestination{},
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: `"appUser"`,
		},
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "replace", got: replaceStagingTableName(dest, `"appUser".orders`, ""), want: `"appUser".orders_staging_`},
		{name: "merge", got: managedStagingTableName(dest, `"appUser".orders`, "merge", ""), want: `"appUser".orders_merge_`},
		{name: "managed CDC stream", got: managedStagingTableName(dest, `"appUser".orders`, "stream", ""), want: `"appUser".orders_stream_`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.HasPrefix(tt.got, tt.want) {
				t.Fatalf("staging table = %q, want prefix %q", tt.got, tt.want)
			}
		})
	}

	if got := managedCDCStateTableName(dest, "cdc_state", ""); got != `"appUser".cdc_state` {
		t.Fatalf("managedCDCStateTableName() = %q, want quoted current schema", got)
	}
}

func TestManagedCDCStateTableName_IsStableAcrossTargetSchemaPolicies(t *testing.T) {
	tests := []struct {
		name          string
		defaultSchema string
	}{
		{name: "mysql", defaultSchema: "app"},
		{name: "duckdb", defaultSchema: "main"},
		{name: "oracle", defaultSchema: "INGESTR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := &fakeManagedStagingPolicyProvider{
				fakeDestination: &fakeDestination{},
				policy: destination.ReplaceStagingPolicy{
					DefaultPlacement:    destination.ReplaceStagingTargetSchema,
					DefaultTargetSchema: tt.defaultSchema,
				},
			}
			if got := managedCDCStateTableName(dest, "cdc_state", ""); got != tt.defaultSchema+".cdc_state" {
				t.Fatalf("managedCDCStateTableName() = %q, want %q", got, tt.defaultSchema+".cdc_state")
			}
		})
	}
}

func TestLegacyManagedCDCStateTableName_PreservesTargetSchema(t *testing.T) {
	dest := &fakeManagedStagingPolicyProvider{
		fakeDestination: &fakeDestination{},
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: "app",
		},
	}

	if got := legacyManagedCDCStateTableName(dest, "analytics.orders", "cdc_state", ""); got != "analytics.cdc_state" {
		t.Fatalf("legacyManagedCDCStateTableName() = %q, want analytics.cdc_state", got)
	}
	if got := legacyManagedCDCStateTableName(dest, `"appUser".orders`, "cdc_state", ""); got != `"appUser".cdc_state` {
		t.Fatalf("legacyManagedCDCStateTableName() = %q, want quoted target schema", got)
	}
}

func TestManagedCDCStateTableName_IgnoresTransientStagingDataset(t *testing.T) {
	dest := &fakeManagedStagingPolicyProvider{
		fakeDestination: &fakeDestination{},
		policy: destination.ReplaceStagingPolicy{
			DefaultPlacement:    destination.ReplaceStagingTargetSchema,
			DefaultTargetSchema: "stable",
		},
	}

	for _, stateTable := range []string{"cdc_state", "cdc_targets"} {
		withoutOverride := managedCDCStateTableName(dest, stateTable, "")
		withOverride := managedCDCStateTableName(dest, stateTable, "scratch")
		if withOverride != withoutOverride {
			t.Fatalf("managedCDCStateTableName(%q) changed from %q to %q with a transient staging dataset", stateTable, withoutOverride, withOverride)
		}
		if withOverride != "stable."+stateTable {
			t.Fatalf("managedCDCStateTableName(%q) = %q, want %q", stateTable, withOverride, "stable."+stateTable)
		}
	}
}

func TestManagedCDCStateTableName_UsesConfiguredCatalog(t *testing.T) {
	dest := &fakeManagedCDCStateCatalogProvider{
		fakeManagedStagingPolicyProvider: &fakeManagedStagingPolicyProvider{fakeDestination: &fakeDestination{}},
		catalog:                          "projectA",
	}

	if got := managedCDCStateTableName(dest, "cdc_state", ""); got != "projectA._bruin_staging.cdc_state" {
		t.Fatalf("managedCDCStateTableName() = %q, want projectA._bruin_staging.cdc_state", got)
	}
}

func TestLegacyManagedCDCStateTableName_PreservesExplicitTargetCatalog(t *testing.T) {
	dest := &fakeManagedCDCStateCatalogProvider{
		fakeManagedStagingPolicyProvider: &fakeManagedStagingPolicyProvider{fakeDestination: &fakeDestination{}},
		catalog:                          "projectA",
	}

	if got := legacyManagedCDCStateTableName(dest, "projectB.dataset.orders", "cdc_state", ""); got != "projectB._bruin_staging.cdc_state" {
		t.Fatalf("legacyManagedCDCStateTableName() = %q, want projectB._bruin_staging.cdc_state", got)
	}
}
