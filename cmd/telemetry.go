package cmd

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/telemetry"
	"golang.org/x/term"
)

func trackCommandFinished(ctx context.Context, command string, startedAt time.Time, err error, additionalProperties map[string]any) {
	finishedProperties := map[string]any{}
	for key, value := range additionalProperties {
		finishedProperties[key] = value
	}
	finishedProperties["duration_bucket"] = telemetryDurationBucket(time.Since(startedAt))
	finishedProperties["status"] = "success"
	if err != nil {
		finishedProperties["status"] = "failed"
		finishedProperties["error"] = commandTelemetryError(err)
	}

	properties := commandTelemetryProperties(command, finishedProperties)
	telemetry.Track(context.WithoutCancel(ctx), "command_finished", properties, versionFlagValue())
}

func commandTelemetryProperties(command string, properties map[string]any) map[string]any {
	eventProperties := make(map[string]any, len(properties)+6)
	for key, value := range properties {
		eventProperties[key] = value
	}
	eventProperties["command"] = command
	eventProperties["version"] = versionFlagValue()
	eventProperties["deployment_type"] = telemetryDeploymentType(os.Getenv, telemetryFileExists)
	eventProperties["invocation_style"] = telemetryInvocationStyle(os.Getenv, telemetryTerminal)
	eventProperties["invoker"] = telemetryInvoker(os.Getenv)
	eventProperties["cpu_bucket"] = telemetryCPUBucket(runtime.GOMAXPROCS(0))
	return eventProperties
}

func telemetryDeploymentType(getenv func(string) string, fileExists func(string) bool) string {
	switch {
	case getenv("KUBERNETES_SERVICE_HOST") != "":
		return "kubernetes"
	case getenv("ECS_CONTAINER_METADATA_URI_V4") != "" || getenv("ECS_CONTAINER_METADATA_URI") != "":
		return "ecs"
	case getenv("K_SERVICE") != "" && getenv("K_REVISION") != "" && getenv("K_CONFIGURATION") != "":
		return "cloud_run"
	case getenv("CLOUD_RUN_JOB") != "" && getenv("CLOUD_RUN_EXECUTION") != "":
		return "cloud_run_job"
	case getenv("AWS_LAMBDA_FUNCTION_NAME") != "":
		return "lambda"
	case getenv("WEBSITE_INSTANCE_ID") != "":
		return "azure_app_service"
	case fileExists("/.dockerenv") || fileExists("/run/.containerenv"):
		return "container"
	default:
		return "host"
	}
}

func telemetryInvocationStyle(getenv func(string) string, isTerminal func(int) bool) string {
	if telemetryCIEnvironment(getenv) {
		return "ci"
	}
	if isTerminal(int(os.Stdin.Fd())) || isTerminal(int(os.Stdout.Fd())) {
		return "interactive"
	}
	return "non_interactive"
}

func telemetryCIEnvironment(getenv func(string) string) bool {
	for _, key := range []string{
		"CI",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"BUILDKITE",
		"JENKINS_URL",
		"TF_BUILD",
		"CIRCLECI",
	} {
		if telemetryEnvEnabled(getenv(key)) {
			return true
		}
	}
	return false
}

func telemetryEnvEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func telemetryInvoker(getenv func(string) string) string {
	value := strings.ToLower(strings.TrimSpace(getenv("INGESTR_TELEMETRY_INVOKER")))
	switch value {
	case "":
		return "direct"
	case "bruin_cli", "bruin_cloud", "ingestr_server":
		return value
	default:
		return "other"
	}
}

func telemetryCPUBucket(cpus int) string {
	switch {
	case cpus <= 1:
		return "1"
	case cpus <= 3:
		return "2-3"
	case cpus <= 7:
		return "4-7"
	case cpus <= 15:
		return "8-15"
	case cpus <= 31:
		return "16-31"
	default:
		return "32+"
	}
}

func telemetryDurationBucket(duration time.Duration) string {
	switch {
	case duration < time.Second:
		return "<1s"
	case duration < 10*time.Second:
		return "1s-10s"
	case duration < time.Minute:
		return "10s-1m"
	case duration < 10*time.Minute:
		return "1m-10m"
	case duration < time.Hour:
		return "10m-1h"
	case duration < 24*time.Hour:
		return "1h-1d"
	case duration < 7*24*time.Hour:
		return "1d-7d"
	default:
		return "7d+"
	}
}

func telemetryFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func telemetryTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

func commandTelemetryError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	var validationErr *config.ValidationError
	if errors.As(err, &validationErr) {
		return "validation_error"
	}
	return "error"
}
