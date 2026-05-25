package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/denisbrodbeck/machineid"
)

const (
	defaultDataPlaneURL = "https://getbruinbumlky.dataplane.rudderstack.com"
	defaultTimeout      = 2 * time.Second
)

const (
	writeKeyPart1 = "2cUr13DDQcX2x2kAf"
	writeKeyPart2 = "MEfdrKvrQa"
)

var defaultClient = NewClient()

type Client struct {
	WriteKey     string
	DataPlaneURL string
	HTTPClient   *http.Client
	Timeout      time.Duration
	MachineID    func() (string, error)
	Now          func() time.Time
}

func NewClient() *Client {
	return &Client{
		WriteKey:     writeKeyPart1 + writeKeyPart2,
		DataPlaneURL: defaultDataPlaneURL,
		HTTPClient:   http.DefaultClient,
		Timeout:      defaultTimeout,
		MachineID:    protectedMachineID,
		Now:          time.Now,
	}
}

func Track(ctx context.Context, event string, properties map[string]any, version string) {
	_ = defaultClient.Track(ctx, event, properties, version)
}

func (c *Client) Track(ctx context.Context, event string, properties map[string]any, version string) error {
	if c == nil || Disabled() || strings.TrimSpace(event) == "" {
		return nil
	}

	userID, err := c.machineID()
	if err != nil {
		userID = fallbackMachineID()
	}

	payload := map[string]any{
		"userId":     userID,
		"event":      event,
		"properties": c.enrichProperties(properties, version),
		"context": map[string]any{
			"app": map[string]any{
				"name":    "ingestr",
				"version": version,
			},
			"os": map[string]any{
				"name": runtime.GOOS,
			},
			"library": map[string]any{
				"name":    "ingestr-go",
				"version": version,
			},
		},
		"timestamp": c.now().UTC().Format(time.RFC3339Nano),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry event: %w", err)
	}

	reqCtx := ctx
	cancel := func() {}
	if timeout := c.timeout(); timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimRight(c.DataPlaneURL, "/")+"/v1/track", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telemetry request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.WriteKey, "")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("send telemetry event: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry request failed with status %s", resp.Status)
	}
	return nil
}

func Disabled() bool {
	return envDisablesTelemetry(os.Getenv("DISABLE_TELEMETRY")) ||
		envDisablesTelemetry(os.Getenv("INGESTR_DISABLE_TELEMETRY"))
}

func envDisablesTelemetry(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (c *Client) enrichProperties(properties map[string]any, version string) map[string]any {
	enriched := make(map[string]any, len(properties)+5)
	for key, value := range properties {
		enriched[key] = value
	}
	enriched["version"] = version
	enriched["os"] = runtime.GOOS
	enriched["platform"] = runtime.GOOS + "/" + runtime.GOARCH
	enriched["architecture"] = runtime.GOARCH
	enriched["go_version"] = runtime.Version()
	return enriched
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) machineID() (string, error) {
	if c.MachineID != nil {
		return c.MachineID()
	}
	return protectedMachineID()
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) timeout() time.Duration {
	if c.Timeout != 0 {
		return c.Timeout
	}
	return defaultTimeout
}

func protectedMachineID() (string, error) {
	return machineid.ProtectedID("ingestr")
}

func fallbackMachineID() string {
	hostname, _ := os.Hostname()
	sum := sha256.Sum256([]byte(runtime.GOOS + ":" + hostname))
	return hex.EncodeToString(sum[:])
}
