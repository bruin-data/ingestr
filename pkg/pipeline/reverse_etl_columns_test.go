package pipeline

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retlMockDestination is a mockDestination that also advertises reverse-ETL.
type retlMockDestination struct {
	mockDestination
}

func (retlMockDestination) IsReverseETL() {}

func (retlMockDestination) SupportsSCD2Strategy() bool         { return false }
func (retlMockDestination) SupportsDeleteInsertStrategy() bool { return false }
func (retlMockDestination) GetScheme() string                  { return "retl" }

// explicitStrategyRetlDestination is a reverse-ETL destination that also demands
// an explicit --incremental-strategy (like HubSpot, whose default is destructive).
type explicitStrategyRetlDestination struct {
	retlMockDestination
}

func (explicitStrategyRetlDestination) RequiresExplicitStrategy() {}

func TestValidateReverseETLColumnOverrides(t *testing.T) {
	sqlDest := &mockDestination{}
	retlDest := &retlMockDestination{}

	t.Run("no columns is fine", func(t *testing.T) {
		require.NoError(t, validateReverseETLColumnOverrides(retlDest, ""))
	})

	t.Run("rename-only allowed on reverse-ETL", func(t *testing.T) {
		require.NoError(t, validateReverseETLColumnOverrides(retlDest, "red_pencil::pencil"))
		require.NoError(t, validateReverseETLColumnOverrides(retlDest, "red_pencil::pencil,hs_lead_status::status"))
	})

	t.Run("typed override rejected on reverse-ETL", func(t *testing.T) {
		err := validateReverseETLColumnOverrides(retlDest, "lead_score:int:score")
		require.ErrorContains(t, err, "rename-only")
		require.ErrorContains(t, err, "score")
	})

	t.Run("type-only override rejected on reverse-ETL", func(t *testing.T) {
		err := validateReverseETLColumnOverrides(retlDest, "lead_score:bigint")
		require.ErrorContains(t, err, "rename-only")
	})

	t.Run("typed override allowed on SQL destination", func(t *testing.T) {
		require.NoError(t, validateReverseETLColumnOverrides(sqlDest, "lead_score:int:score"))
	})

	t.Run("multiple renames allowed", func(t *testing.T) {
		require.NoError(t, validateReverseETLColumnOverrides(retlDest, "a::x,b::y,c::z"))
	})

	t.Run("one typed among renames is rejected and named", func(t *testing.T) {
		err := validateReverseETLColumnOverrides(retlDest, "a::x,lead_score:int:score,b::y")
		require.ErrorContains(t, err, "rename-only")
		require.ErrorContains(t, err, "score")
	})

	t.Run("rename+type form is rejected", func(t *testing.T) {
		err := validateReverseETLColumnOverrides(retlDest, "dest:varchar:src")
		require.ErrorContains(t, err, "rename-only")
	})

	t.Run("empty spec is a no-op", func(t *testing.T) {
		require.NoError(t, validateReverseETLColumnOverrides(retlDest, ""))
	})
}

func TestValidateReverseETLOverrideColumns(t *testing.T) {
	src := &schema.TableSchema{Columns: []schema.Column{
		{Name: "employee_count"}, {Name: "annual_revenue"}, {Name: "company_id"},
	}}

	t.Run("all source columns present is fine", func(t *testing.T) {
		require.NoError(t, validateReverseETLOverrideColumns(
			"numberofemployees::employee_count,annualrevenue::annual_revenue", src, "", false))
	})

	t.Run("missing source column is rejected and named", func(t *testing.T) {
		// Reversed direction: source side "numberofemployees" is not in the source.
		err := validateReverseETLOverrideColumns(
			"employee_count::numberofemployees,annual_revenue::annualrevenue", src, "", false)
		require.ErrorContains(t, err, "numberofemployees")
		require.ErrorContains(t, err, "annualrevenue")
		require.ErrorContains(t, err, "not in the source")
	})

	t.Run("no columns and nil schema are no-ops", func(t *testing.T) {
		require.NoError(t, validateReverseETLOverrideColumns("", src, "", false))
		require.NoError(t, validateReverseETLOverrideColumns("a::b", nil, "", false))
	})

	t.Run("already-renamed schema (inferred source) passes on the RenameTo side", func(t *testing.T) {
		// Inferred-schema sources apply renames before this check, so the schema
		// carries the destination names and the original source names are gone.
		renamed := &schema.TableSchema{Columns: []schema.Column{
			{Name: "numberofemployees"}, {Name: "annualrevenue"},
		}}
		require.NoError(t, validateReverseETLOverrideColumns(
			"numberofemployees::employee_count,annualrevenue::annual_revenue", renamed, "", true))
	})

	t.Run("renamed schema still rejects an override matching neither side", func(t *testing.T) {
		renamed := &schema.TableSchema{Columns: []schema.Column{{Name: "numberofemployees"}}}
		err := validateReverseETLOverrideColumns("phone::mobile_phone", renamed, "", true)
		require.ErrorContains(t, err, "mobile_phone")
		require.ErrorContains(t, err, "not in the source")
	})
}

func TestResolveTablePrimaryKeys(t *testing.T) {
	t.Run("explicit --primary-key always wins", func(t *testing.T) {
		assert.Equal(t, []string{"email"},
			resolveTablePrimaryKeys([]string{"email"}, []string{"id"}, []string{"src_pk"}, true))
		assert.Equal(t, []string{"email"},
			resolveTablePrimaryKeys([]string{"email"}, nil, []string{"src_pk"}, false))
	})

	t.Run("reverse-ETL with no explicit key clears source-detected PKs", func(t *testing.T) {
		// A source-reported PK must not leak in as the match column.
		assert.Nil(t, resolveTablePrimaryKeys(nil, nil, []string{"src_pk"}, true))
		assert.Nil(t, resolveTablePrimaryKeys(nil, []string{"id"}, []string{"src_pk"}, true))
	})

	t.Run("SQL keeps table PK, else falls back to source-detected", func(t *testing.T) {
		assert.Equal(t, []string{"id"},
			resolveTablePrimaryKeys(nil, []string{"id"}, []string{"src_pk"}, false))
		assert.Equal(t, []string{"src_pk"},
			resolveTablePrimaryKeys(nil, nil, []string{"src_pk"}, false))
	})
}

// TestValidateReverseETLFlags covers the reverse-ETL-only run flags: rejected on
// SQL destinations (fail fast, not silently ignored), validated on RETL ones.
func TestValidateReverseETLFlags(t *testing.T) {
	sqlDest := &mockDestination{scheme: "postgres"}
	retlDest := &retlMockDestination{}

	t.Run("SQL rejects --reject-mode", func(t *testing.T) {
		err := validateReverseETLFlags(sqlDest, &config.IngestConfig{RejectMode: config.RejectSkip})
		require.ErrorContains(t, err, "only valid for reverse-ETL")
	})
	t.Run("SQL rejects an explicit --write-nulls", func(t *testing.T) {
		err := validateReverseETLFlags(sqlDest, &config.IngestConfig{WriteNulls: true, WriteNullsSet: true})
		require.ErrorContains(t, err, "only valid for reverse-ETL")
		// --write-nulls=false is still an explicit use, so it is rejected too.
		err = validateReverseETLFlags(sqlDest, &config.IngestConfig{WriteNulls: false, WriteNullsSet: true})
		require.ErrorContains(t, err, "only valid for reverse-ETL")
	})
	t.Run("SQL with neither is fine", func(t *testing.T) {
		require.NoError(t, validateReverseETLFlags(sqlDest, &config.IngestConfig{}))
	})
	t.Run("RETL accepts valid reject-modes", func(t *testing.T) {
		for _, m := range []config.RejectMode{"", config.RejectFailFast, config.RejectFail, config.RejectSkip} {
			require.NoError(t, validateReverseETLFlags(retlDest, &config.IngestConfig{RejectMode: m, WriteNulls: true}))
		}
	})
	t.Run("RETL rejects a bogus reject-mode", func(t *testing.T) {
		err := validateReverseETLFlags(retlDest, &config.IngestConfig{RejectMode: "bogus"})
		require.ErrorContains(t, err, "invalid --reject-mode")
	})
	t.Run("a safe-default RETL does not require an explicit strategy", func(t *testing.T) {
		// Most reverse-ETL destinations have a safe default, so an implicit
		// strategy is fine — the requirement must not apply to them.
		require.NoError(t, validateReverseETLFlags(retlDest, &config.IngestConfig{IncrementalStrategyExplicit: false}))
	})
	t.Run("a destructive-default RETL requires an explicit strategy", func(t *testing.T) {
		// HubSpot's default (replace) mirrors/archives, so it must not be inherited
		// silently — an implicit strategy is rejected, an explicit one accepted.
		explicitDest := &explicitStrategyRetlDestination{}
		err := validateReverseETLFlags(explicitDest, &config.IngestConfig{IncrementalStrategyExplicit: false})
		require.ErrorContains(t, err, "no default write strategy")
		require.NoError(t, validateReverseETLFlags(explicitDest, &config.IngestConfig{IncrementalStrategyExplicit: true}))
	})
	t.Run("RETL rejects strategies the destination does not support", func(t *testing.T) {
		for _, s := range []config.IncrementalStrategy{config.StrategySCD2, config.StrategyDeleteInsert} {
			err := validateReverseETLFlags(retlDest, &config.IngestConfig{IncrementalStrategy: s})
			require.ErrorContains(t, err, "does not support the "+string(s)+" strategy")
		}
	})
}
