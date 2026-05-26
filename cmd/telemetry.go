package cmd

import (
	"context"
	"errors"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/telemetry"
)

func trackCommandTriggered(ctx context.Context, command string) {
	telemetry.Track(ctx, "command_triggered", commandTelemetryProperties(command, nil), versionFlagValue())
}

func trackCommandRunning(ctx context.Context, command string, properties map[string]any) {
	telemetry.Track(ctx, "command_running", commandTelemetryProperties(command, properties), versionFlagValue())
}

func trackCommandFinished(ctx context.Context, command string, err error) {
	properties := commandTelemetryProperties(command, map[string]any{
		"status": "success",
	})
	if err != nil {
		properties["status"] = "failed"
		properties["error"] = commandTelemetryError(err)
	}

	telemetry.Track(context.WithoutCancel(ctx), "command_finished", properties, versionFlagValue())
}

func commandTelemetryProperties(command string, properties map[string]any) map[string]any {
	eventProperties := map[string]any{
		"command": command,
		"version": versionFlagValue(),
	}
	for key, value := range properties {
		eventProperties[key] = value
	}
	eventProperties["version"] = versionFlagValue()
	return eventProperties
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
