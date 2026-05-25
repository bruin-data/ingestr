package cmd

import (
	"context"
	"errors"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/telemetry"
)

func trackCommandTriggered(ctx context.Context, command string) {
	telemetry.Track(ctx, "command_triggered", map[string]any{
		"command": command,
	}, Version)
}

func trackCommandRunning(ctx context.Context, command string, properties map[string]any) {
	eventProperties := map[string]any{
		"command": command,
	}
	for key, value := range properties {
		eventProperties[key] = value
	}
	telemetry.Track(ctx, "command_running", eventProperties, Version)
}

func trackCommandFinished(ctx context.Context, command string, err error) {
	properties := map[string]any{
		"command": command,
		"status":  "success",
	}
	if err != nil {
		properties["status"] = "failed"
		properties["error"] = commandTelemetryError(err)
	}

	telemetry.Track(context.WithoutCancel(ctx), "command_finished", properties, Version)
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
