package cmd

import (
	"fmt"
	"testing"
	"time"

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
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgresql://user:pass@localhost:5432/db"
	cfg.DestURI = "bigquery://project/dataset"

	properties := ingestTelemetryProperties(cfg)

	require.Equal(t, "postgres", properties["source_platform"])
	require.Equal(t, "bigquery", properties["destination_platform"])
	require.Equal(t, "batch", properties["execution_mode"])
	require.Equal(t, "default", properties["strategy_selection"])
	require.Equal(t, "evolve", properties["schema_contract"])
}

func TestIngestTelemetryPropertiesUseAllowlistedValues(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "customer-secret://source"
	cfg.DestURI = "tenant-secret://destination"
	cfg.IncrementalStrategy = "strategy-secret"
	cfg.IncrementalStrategyExplicit = true
	cfg.SchemaContract = "contract-secret"

	properties := ingestTelemetryProperties(cfg)

	require.NotContains(t, properties, "source_platform")
	require.NotContains(t, properties, "destination_platform")
	require.NotContains(t, properties, "schema_contract")
	require.Equal(t, "invalid", properties["requested_strategy"])
	require.NotContains(t, fmt.Sprint(properties), "customer-secret")
	require.NotContains(t, fmt.Sprint(properties), "tenant-secret")
	require.NotContains(t, fmt.Sprint(properties), "strategy-secret")
	require.NotContains(t, fmt.Sprint(properties), "contract-secret")
}

func TestTelemetrySchemaContractMatchesParser(t *testing.T) {
	require.Equal(t, "evolve", telemetrySchemaContract("evolve"))
	require.Equal(t, "discard_row", telemetrySchemaContract("discard-row"))
	require.Equal(t, "discard_value", telemetrySchemaContract("discard-value"))
	require.Equal(t, "freeze", telemetrySchemaContract(" Freeze "))
	require.Equal(t, "", telemetrySchemaContract("contract-secret"))
}

func TestTelemetryExecutionMode(t *testing.T) {
	for _, tt := range []struct {
		name      string
		sourceURI string
		stream    bool
		want      string
	}{
		{name: "batch", sourceURI: "postgres://host/db", want: "batch"},
		{name: "stream", sourceURI: "kafka://host/topic", stream: true, want: "stream"},
		{name: "cdc batch", sourceURI: "postgres+cdc://host/db", want: "cdc_batch"},
		{name: "cdc stream", sourceURI: "postgres+cdc://host/db", stream: true, want: "cdc_stream"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI = tt.sourceURI
			cfg.Stream = tt.stream
			require.Equal(t, tt.want, telemetryExecutionMode(cfg))
		})
	}
}

func TestTelemetryDeploymentTypeUsesOnlyKnownSignals(t *testing.T) {
	getenv := func(key string) string {
		values := map[string]string{
			"KUBERNETES_SERVICE_HOST": "customer-cluster.internal",
			"WEBSITE_INSTANCE_ID":     "customer-instance-id",
		}
		return values[key]
	}

	require.Equal(t, "kubernetes", telemetryDeploymentType(getenv, func(string) bool { return false }))
	require.Equal(t, "cloud_run_job", telemetryDeploymentType(func(key string) string {
		values := map[string]string{
			"CLOUD_RUN_JOB":       "customer-job-name",
			"CLOUD_RUN_EXECUTION": "customer-job-execution",
		}
		return values[key]
	}, func(string) bool { return false }))
	require.Equal(t, "container", telemetryDeploymentType(func(string) string { return "" }, func(path string) bool {
		return path == "/.dockerenv"
	}))
	require.Equal(t, "host", telemetryDeploymentType(func(string) string { return "" }, func(string) bool { return false }))
}

func TestTelemetryInvocationStyle(t *testing.T) {
	require.Equal(t, "ci", telemetryInvocationStyle(func(key string) string {
		if key == "GITHUB_ACTIONS" {
			return "true"
		}
		return ""
	}, func(int) bool { return true }))
	require.Equal(t, "interactive", telemetryInvocationStyle(func(string) string { return "" }, func(int) bool { return true }))
	require.Equal(t, "non_interactive", telemetryInvocationStyle(func(string) string { return "" }, func(int) bool { return false }))
}

func TestTelemetryInvokerDoesNotForwardUnknownValues(t *testing.T) {
	require.Equal(t, "direct", telemetryInvoker(func(string) string { return "" }))
	require.Equal(t, "bruin_cli", telemetryInvoker(func(string) string { return "bruin_cli" }))
	require.Equal(t, "other", telemetryInvoker(func(string) string { return "customer-name" }))
}

func TestTelemetryBuckets(t *testing.T) {
	require.Equal(t, "1", telemetryCPUBucket(1))
	require.Equal(t, "4-7", telemetryCPUBucket(4))
	require.Equal(t, "32+", telemetryCPUBucket(64))

	require.Equal(t, "<1s", telemetryDurationBucket(500*time.Millisecond))
	require.Equal(t, "1m-10m", telemetryDurationBucket(5*time.Minute))
	require.Equal(t, "1d-7d", telemetryDurationBucket(48*time.Hour))
}

func TestNewAppUsesVersionFlagValue(t *testing.T) {
	originalVersion := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = originalVersion })

	require.Equal(t, versionFlagValue(), NewApp().Version)
}
