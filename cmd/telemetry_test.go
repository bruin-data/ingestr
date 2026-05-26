package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandTelemetryPropertiesIncludeVersionFlagValue(t *testing.T) {
	originalVersion := Version
	Version = "v9.8.7"
	t.Cleanup(func() { Version = originalVersion })

	properties := commandTelemetryProperties("ingest", map[string]any{
		"source_type": "postgres",
		"version":     "stale",
	})

	require.Equal(t, "ingest", properties["command"])
	require.Equal(t, "postgres", properties["source_type"])
	require.Equal(t, "v9.8.7", properties["version"])
}

func TestNewAppUsesVersionFlagValue(t *testing.T) {
	originalVersion := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = originalVersion })

	require.Equal(t, versionFlagValue(), NewApp().Version)
}
