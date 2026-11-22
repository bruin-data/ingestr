package strategy

import (
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBatchStrategiesPreserveManagedCDCState(t *testing.T) {
	for _, strat := range []WriteStrategy{&MergeStrategy{}, &ReplaceStrategy{}} {
		for _, managed := range []bool{false, true} {
			name := string(strat.Name()) + "/legacy"
			if managed {
				name = string(strat.Name()) + "/managed"
			}
			t.Run(name, func(t *testing.T) {
				job, src, _ := minimalJob()
				job.Config.SourceTable = "dbo.items"
				job.Config.DestTable = "raw.items"
				job.Config.PrimaryKeys = []string{"id"}
				job.Config.FullRefresh = strat.Name() == config.StrategyReplace
				job.Schema = keylessCDCSchema()
				job.SourceSchema = job.Schema
				if managed {
					dest := newCDCStateDestination()
					job.Destination = dest
					manager, err := NewCDCStateManager(dest, "ct-batch", job.Config.DestTable, "")
					require.NoError(t, err)
					require.NoError(t, manager.RegisterTable(t.Context(), job.Config.SourceTable, job.Config.DestTable))
					require.NoError(t, manager.BeginRun(t.Context(), job.Config.FullRefresh))
					job.CDCStateManager = manager
				}
				src.readErr = errors.New("stopped after managed read options")
				require.ErrorContains(t, strat.Execute(t.Context(), job), "stopped after managed read options")
				require.Equal(t, managed, src.readOpts.CDCManagedState)
			})
		}
	}
}
