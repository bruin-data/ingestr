package cmd

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCommandTelemetryPropertiesIncludeVersionFlagValue(t *testing.T) {
	originalVersion := Version
	Version = "v9.8.7"
	t.Cleanup(func() { Version = originalVersion })

	properties := commandTelemetryProperties("ingest", map[string]any{
		"source_platform": "postgres",
		"version":         "stale",
	})

	require.Equal(t, "ingest", properties["command"])
	require.Equal(t, "postgres", properties["source_platform"])
	require.Equal(t, "v9.8.7", properties["version"])
}

func TestIngestTelemetryPropertiesIncludeConnectorSchemes(t *testing.T) {
	properties := ingestTelemetryProperties(&config.IngestConfig{
		SourceURI: "postgres://user:pass@localhost:5432/db",
		DestURI:   "bigquery://project/dataset",
	})

	require.Equal(t, "postgres", properties["source_platform"])
	require.Equal(t, "bigquery", properties["destination_platform"])
}

func TestNewAppUsesVersionFlagValue(t *testing.T) {
	originalVersion := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = originalVersion })

	require.Equal(t, versionFlagValue(), NewApp().Version)
}
