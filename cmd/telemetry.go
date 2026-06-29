package cmd

import (
	"context"
	"errors"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/telemetry"
)

var telemetryWG sync.WaitGroup

func trackCommandTriggered(ctx context.Context, command string) {
	trackTelemetryAsync(ctx, "command_triggered", commandTelemetryProperties(command, nil))
}

func trackCommandRunning(ctx context.Context, command string, properties map[string]any) {
	trackTelemetryAsync(ctx, "command_running", commandTelemetryProperties(command, properties))
}

func trackCommandFinished(ctx context.Context, command string, err error) {
	properties := commandTelemetryProperties(command, map[string]any{
		"status": "success",
	})
	if err != nil {
		properties["status"] = "failed"
		properties["error"] = commandTelemetryError(err)
	}

	telemetryWG.Wait()
	telemetry.Track(context.WithoutCancel(ctx), "command_finished", properties, versionFlagValue())
}

func trackTelemetryAsync(ctx context.Context, event string, properties map[string]any) {
	if telemetry.Disabled() {
		return
	}

	ctx = context.WithoutCancel(ctx)
	version := versionFlagValue()
	telemetryWG.Add(1)
	go func() {
		defer telemetryWG.Done()
		telemetry.Track(ctx, event, properties, version)
	}()
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
