package strategy

import (
	"context"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRETLDestination is a fakeDestination that also marks itself as a
// reverse-ETL destination, so strategies route to the API (Write) path.
type fakeRETLDestination struct {
	*fakeDestination
}

func (d *fakeRETLDestination) IsReverseETL() {}

func TestUpdateStrategy_SQLDestinationNotSupported(t *testing.T) {
	job, _, _ := minimalJob()
	err := (&UpdateStrategy{}).Execute(context.Background(), job)
	require.ErrorContains(t, err, "not yet supported for fake")
}

func TestDeleteStrategy_SQLDestinationNotSupported(t *testing.T) {
	job, _, _ := minimalJob()
	err := (&DeleteStrategy{}).Execute(context.Background(), job)
	require.ErrorContains(t, err, "not yet supported for fake")
}

func TestReverseETL_RoutesToWriteWithStrategyAndDefaults(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	retl := &fakeRETLDestination{fakeDestination: base}
	job.Destination = retl

	err := (&UpdateStrategy{}).Execute(context.Background(), job)
	require.NoError(t, err)

	require.Len(t, base.prepareCalls, 1)
	assert.Equal(t, string(config.StrategyUpdate), base.prepareCalls[0].Strategy)
	require.Len(t, base.writeCalls, 1)
	assert.Equal(t, string(config.StrategyUpdate), base.writeCalls[0].Strategy)
	// reject-mode defaults to "fail" when the flag is unset.
	assert.Equal(t, string(config.RejectFail), base.writeCalls[0].RejectMode)
}

func TestReverseETL_PassesRejectModeAndWriteNulls(t *testing.T) {
	job, src, base := minimalJob()
	src.readCh = mustClosedRecords()
	job.Config.RejectMode = config.RejectSkip
	job.Config.WriteNulls = true
	job.Destination = &fakeRETLDestination{fakeDestination: base}

	err := (&DeleteStrategy{}).Execute(context.Background(), job)
	require.NoError(t, err)

	require.Len(t, base.writeCalls, 1)
	assert.Equal(t, string(config.RejectSkip), base.writeCalls[0].RejectMode)
	assert.True(t, base.writeCalls[0].WriteNulls)
	assert.Equal(t, string(config.StrategyDelete), base.writeCalls[0].Strategy)
}

func TestValidateReverseETLReject(t *testing.T) {
	for _, m := range []config.RejectMode{"", config.RejectFailFast, config.RejectFail, config.RejectSkip} {
		require.NoError(t, validateReverseETLReject(&config.IngestConfig{RejectMode: m}))
	}
	err := validateReverseETLReject(&config.IngestConfig{RejectMode: "bogus"})
	require.ErrorContains(t, err, "invalid --reject-mode")
}

// executor is the subset of WriteStrategy exercised by the Execute matrix.
type executor interface {
	Execute(context.Context, *IngestionJob) error
}

// TestReverseETL_FlagCrossMatrix runs every reverse-ETL strategy with each
// --reject-mode × --write-nulls combination and asserts both policies, plus the
// strategy name, reach the destination's WriteOptions.
func TestReverseETL_FlagCrossMatrix(t *testing.T) {
	strategies := map[config.IncrementalStrategy]executor{
		config.StrategyMerge:   &MergeStrategy{},
		config.StrategyUpdate:  &UpdateStrategy{},
		config.StrategyDelete:  &DeleteStrategy{},
		config.StrategyAppend:  &AppendStrategy{},
		config.StrategyReplace: &ReplaceStrategy{},
	}
	rejectModes := []config.RejectMode{config.RejectFail, config.RejectFailFast, config.RejectSkip}

	for stratName, strat := range strategies {
		for _, rm := range rejectModes {
			for _, wn := range []bool{false, true} {
				t.Run(string(stratName)+"/"+string(rm)+"/writeNulls="+boolStr(wn), func(t *testing.T) {
					job, src, base := minimalJob()
					src.readCh = mustClosedRecords()
					job.Config.RejectMode = rm
					job.Config.WriteNulls = wn
					job.Destination = &fakeRETLDestination{fakeDestination: base}

					require.NoError(t, strat.Execute(context.Background(), job))
					require.Len(t, base.writeCalls, 1)
					wc := base.writeCalls[0]
					assert.Equal(t, string(stratName), wc.Strategy)
					assert.Equal(t, string(rm), wc.RejectMode)
					assert.Equal(t, wn, wc.WriteNulls)
				})
			}
		}
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// validator is the subset of WriteStrategy exercised by the Validate matrix.
type validator interface {
	Validate(*config.IngestConfig) error
}

func retlStrategies() map[string]validator {
	return map[string]validator{
		"merge":   &MergeStrategy{},
		"update":  &UpdateStrategy{},
		"delete":  &DeleteStrategy{},
		"append":  &AppendStrategy{},
		"replace": &ReplaceStrategy{},
	}
}

// TestStrategyValidate_RejectModeAllStrategies: every reverse-ETL strategy
// accepts a valid --reject-mode and rejects a bogus one (regression for the gap
// where only update/delete validated it).
func TestStrategyValidate_RejectModeAllStrategies(t *testing.T) {
	for name, s := range retlStrategies() {
		for _, m := range []config.RejectMode{"", config.RejectFailFast, config.RejectFail, config.RejectSkip} {
			t.Run(name+"/valid/"+string(m), func(t *testing.T) {
				cfg := &config.IngestConfig{ReverseETLDestination: true, RejectMode: m, PrimaryKeys: []string{"email"}}
				require.NoError(t, s.Validate(cfg))
			})
		}
		t.Run(name+"/bogus", func(t *testing.T) {
			cfg := &config.IngestConfig{ReverseETLDestination: true, RejectMode: "bogus", PrimaryKeys: []string{"email"}}
			require.ErrorContains(t, s.Validate(cfg), "invalid --reject-mode")
		})
	}
}

// TestMergeValidate_PrimaryKeyRules: merge requires a PK for SQL destinations but
// not for reverse-ETL ones (which match via id_property).
func TestMergeValidate_PrimaryKeyRules(t *testing.T) {
	m := &MergeStrategy{}

	// SQL destination: PK required.
	require.ErrorContains(t, m.Validate(&config.IngestConfig{}), "requires at least one primary_key")
	require.NoError(t, m.Validate(&config.IngestConfig{PrimaryKeys: []string{"id"}}))

	// Reverse-ETL destination: no PK needed.
	require.NoError(t, m.Validate(&config.IngestConfig{ReverseETLDestination: true}))
	// ...but a bogus reject-mode is still caught.
	require.ErrorContains(t, m.Validate(&config.IngestConfig{ReverseETLDestination: true, RejectMode: "bogus"}), "invalid --reject-mode")
}
