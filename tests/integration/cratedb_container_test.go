//go:build integration

package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cratedbUser = "crate"

func startCrateDBContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "crate:latest",
			ExposedPorts: []string{"5432/tcp"},
			Cmd:          []string{"-Cdiscovery.type=single-node"},
			Env: map[string]string{
				"CRATE_HEAP_SIZE": "256m",
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("5432/tcp"),
				wait.ForLog("started"),
			).WithDeadline(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("cratedb://%s@%s:%s/?sslmode=disable",
		cratedbUser, host, port.Port())

	_ = name
	return container, uri, nil
}

func startCrateDBContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startCrateDBContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
