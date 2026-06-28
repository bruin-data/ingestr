//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type mqttEnv struct {
	container testcontainers.Container
	uri       string
}

func startMQTTContainerForMain(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "eclipse-mosquitto:2.0",
		ExposedPorts: []string{"1883/tcp"},
		Files: []testcontainers.ContainerFile{
			{
				Reader: strings.NewReader(strings.Join([]string{
					"listener 1883 0.0.0.0",
					"allow_anonymous true",
					"persistence false",
					"",
				}, "\n")),
				ContainerFilePath: "/mosquitto/config/mosquitto.conf",
				FileMode:          0o644,
			},
		},
		WaitingFor: wait.ForListeningPort("1883/tcp").WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "1883")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	return container, fmt.Sprintf("mqtt://%s:%s", host, port.Port()), nil
}
