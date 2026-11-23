package pipeline

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSpoolFiniteMultiTableUsesStreamingExecutor(t *testing.T) {
	for _, fullRefresh := range []bool{false, true} {
		name := "initial"
		if fullRefresh {
			name = "full_refresh"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI, cfg.DestURI = "postgres+cdc://source/db", "postgres://destination/db"
			cfg.CDCSnapshotSpool, cfg.FullRefresh = true, fullRefresh
			cfg.NoLoadTimestamp, cfg.NoRunID = true, true
			cfg.SchemaNaming = "snake_case"
			cfg.Progress = config.ProgressLog
			cfg.FlushInterval = time.Hour
			dest := &chunkedRoutingDestination{}
			src := &chunkedRoutingSource{staticMultiTableSource: staticMultiTableSource{tables: []source.SourceTableInfo{chunkedRoutingTable()}}}
			pipeline := &Pipeline{config: cfg, dest: dest, cdcConnectorID: "connector"}
			require.NoError(t, pipeline.runMultiTable(t.Context(), src))
			require.True(t, src.readOptions.Streaming)
			require.NotNil(t, src.readOptions.CDCSnapshotSpool)
			require.False(t, src.readOptions.CDCSnapshotSpool.Continuous)
			require.Equal(t, fullRefresh, src.readOptions.FullRefresh)
			require.NotNil(t, src.readOptions.CDCSnapshotSpool.Store)
			require.False(t, cfg.Stream)
			require.Empty(t, src.validated)
			require.Equal(t, "OrderID", src.tables[0].Schema.Columns[0].Name)
			require.Positive(t, dest.claims)
		})
	}
}

func TestSpoolStrategyResolution(t *testing.T) {
	for _, fullRefresh := range []bool{false, true} {
		cfg := config.DefaultConfig()
		cfg.SourceURI, cfg.DestURI = "postgres+cdc://source/db", "postgres://destination/db"
		cfg.CDCSnapshotSpool, cfg.FullRefresh = true, fullRefresh
		require.NoError(t, validateManagedChangeConfig(cfg))
		require.True(t, usesStreamingExecutor(cfg))
		require.Equal(t, config.StrategyMerge, resolveStrategy(cfg, &incrementalitySource{}, &mockSourceTable{pks: []string{"id"}}))
		require.Equal(t, config.StrategyMerge, resolveMultiTableStrategy(cfg, &staticMultiTableSource{}))
	}
}
