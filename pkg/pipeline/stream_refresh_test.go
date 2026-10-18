package pipeline

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
)

func TestStreamingRefreshScopeUsesEffectiveTableSelection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db?dest_schema=raw"
	cfg.DestURI = "postgres://destination/db"
	cfg.SourceTables = []string{"public.orders", "public.users"}
	cfg.DestTable = "cdc.stream_name"
	p := &Pipeline{config: cfg}
	scope := p.streamingRefreshScope()
	cfg.DestTable = "cdc.renamed_stream"
	cfg.SourceTables = []string{"public.users", "public.orders"}
	require.Equal(t, scope, p.streamingRefreshScope(), "an ignored asset name or table order cannot change refresh identity")
	cfg.SourceTables = []string{"public.orders"}
	require.NotEqual(t, scope, p.streamingRefreshScope())
	cfg.SourceTable = "public.orders"
	scope = p.streamingRefreshScope()
	cfg.DestTable = "raw.orders"
	require.NotEqual(t, scope, p.streamingRefreshScope(), "single-table destination changes must invalidate the scope")
}

func TestStreamingRefreshRequiresSnapshotReplacement(t *testing.T) {
	p := &Pipeline{config: &config.IngestConfig{Stream: true, FullRefresh: true, RefreshID: "request"}}
	require.ErrorContains(t, p.prepareStreamingRefresh(t.Context(), nil), "snapshot truncation")
}
