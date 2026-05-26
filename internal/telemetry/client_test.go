package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTrackSendsRudderStackPayload(t *testing.T) {
	clearTelemetryEnv(t)

	var got map[string]any
	var authUser, authPassword string
	var hasAuth bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/track", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		authUser, authPassword, hasAuth = r.BasicAuth()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		WriteKey:     "test-key",
		DataPlaneURL: server.URL,
		HTTPClient:   server.Client(),
		Timeout:      time.Second,
		MachineID: func() (string, error) {
			return "machine-123", nil
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 25, 17, 8, 0, 0, time.UTC)
		},
	}

	err := client.Track(context.Background(), "command_triggered", map[string]any{"command": "ingest"}, "v1.2.3")
	require.NoError(t, err)

	require.True(t, hasAuth)
	require.Equal(t, "test-key", authUser)
	require.Empty(t, authPassword)

	require.Equal(t, "machine-123", got["userId"])
	require.Equal(t, "command_triggered", got["event"])
	require.Equal(t, "2026-05-25T17:08:00Z", got["timestamp"])

	properties := got["properties"].(map[string]any)
	require.Equal(t, "ingest", properties["command"])
	require.Equal(t, "v1.2.3", properties["version"])
	require.Equal(t, runtime.GOOS, properties["os"])
	require.Equal(t, runtime.GOARCH, properties["architecture"])
	require.Equal(t, runtime.Version(), properties["go_version"])
}

func TestTrackHonorsDisableTelemetryEnv(t *testing.T) {
	t.Setenv("DISABLE_TELEMETRY", "")
	t.Setenv("INGESTR_DISABLE_TELEMETRY", "true")

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		WriteKey:     "test-key",
		DataPlaneURL: server.URL,
		HTTPClient:   server.Client(),
		MachineID: func() (string, error) {
			return "machine-123", nil
		},
	}

	err := client.Track(context.Background(), "command_triggered", nil, "dev")
	require.NoError(t, err)
	require.False(t, called)
}

func TestEnvDisablesTelemetry(t *testing.T) {
	for _, value := range []string{"", "0", "false", "False", "no", "off"} {
		require.False(t, envDisablesTelemetry(value), value)
	}
	for _, value := range []string{"1", "true", "yes", "on", "anything"} {
		require.True(t, envDisablesTelemetry(value), value)
	}
}

func TestTrackFallsBackWhenMachineIDFails(t *testing.T) {
	clearTelemetryEnv(t)

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		WriteKey:     "test-key",
		DataPlaneURL: server.URL,
		HTTPClient:   server.Client(),
		MachineID: func() (string, error) {
			return "", errors.New("unavailable")
		},
	}

	err := client.Track(context.Background(), "command_triggered", nil, "dev")
	require.NoError(t, err)
	require.NotEmpty(t, got["userId"])
}

func clearTelemetryEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DISABLE_TELEMETRY", "")
	t.Setenv("INGESTR_DISABLE_TELEMETRY", "")
}
