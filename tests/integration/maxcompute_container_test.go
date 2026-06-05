//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startMaxComputeContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, string, error) {
	tmpFile, err := os.CreateTemp("", "maxcompute-emulator-*.db")
	if err != nil {
		return nil, "", "", err
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()

	req := testcontainers.ContainerRequest{
		Image:        "maxcompute/maxcompute-emulator:v0.0.7",
		ExposedPorts: []string{"8080/tcp"},
		Binds:        []string{fmt.Sprintf("%s:/tpch-tiny.db", dbPath)},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("8080/tcp"),
			wait.ForLog("Started MaxcomputeEmulatorApplication"),
		).WithDeadline(120 * time.Second),
		Name: name,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		_ = os.Remove(dbPath)
		return nil, "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.Remove(dbPath)
		return nil, "", "", err
	}
	port, err := container.MappedPort(ctx, "8080")
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.Remove(dbPath)
		return nil, "", "", err
	}

	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/init", bytes.NewBufferString(endpoint))
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.Remove(dbPath)
		return nil, "", "", err
	}
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.Remove(dbPath)
		return nil, "", "", err
	}
	_ = initResp.Body.Close()
	if initResp.StatusCode < 200 || initResp.StatusCode >= 300 {
		_ = container.Terminate(ctx)
		_ = os.Remove(dbPath)
		return nil, "", "", fmt.Errorf("maxcompute emulator init returned %s", initResp.Status)
	}

	values := url.Values{}
	values.Set("project", "project")
	values.Set("protocol", "http")
	values.Set("tunnel_endpoint", endpoint)
	values.Set("storage_api", "true")
	values.Set("emulator_db_path", dbPath)
	uri := fmt.Sprintf("maxcompute://ak:sk@%s:%s?%s", host, port.Port(), values.Encode())

	return container, uri, dbPath, nil
}

func startMaxComputeContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, string, error) {
	return startMaxComputeContainerRaw(ctx, name)
}
