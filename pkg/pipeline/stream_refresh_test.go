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

func TestStreamingRefreshScopeSurvivesBigQueryCredentialChanges(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mysql+cdc://source/db"
	cfg.SourceTables = []string{"orders", "users"}
	cfg.DestURI = "bigquery://project/dataset?location=EU&credentials_base64=b3JpZ2luYWw="
	p := &Pipeline{config: cfg}
	scope := p.streamingRefreshScope()
	connectorID := genericCDCConnectorID(cfg)
	for _, uri := range []string{
		"bigquery://project/dataset?location=EU&credentials_base64=cm90YXRlZA==",
		"bigquery://project/dataset?location=EU&credentials_path=/tmp/credentials-1.json",
		"bigquery://project/dataset?location=EU&credentials_path=/tmp/credentials-2.json",
		"bigquery://project/dataset?location=EU",
	} {
		t.Run(uri, func(t *testing.T) {
			cfg.DestURI = uri
			require.Equal(t, scope, p.streamingRefreshScope())
			require.NotEqual(t, connectorID, genericCDCConnectorID(cfg), "refresh scope normalization must not alter ordinary connector identity")
		})
	}
	for _, uri := range []string{
		"bigquery://other-project/dataset?location=EU",
		"bigquery://project/other-dataset?location=EU",
		"bigquery://project/dataset?location=US",
	} {
		t.Run(uri, func(t *testing.T) {
			cfg.DestURI = uri
			require.NotEqual(t, scope, p.streamingRefreshScope())
		})
	}
	cfg.DestURI = "bigquery://project/dataset?location=EU"
	cfg.SourceTables = []string{"orders"}
	require.NotEqual(t, scope, p.streamingRefreshScope())
}
