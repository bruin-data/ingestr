//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	oracleUser     = "ingestr"
	oraclePassword = "TestPassword123"
	oracleService  = "FREEPDB1"
)

func startOracleContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	image := os.Getenv("GONG_TEST_ORACLE_IMAGE")
	if image == "" {
		image = "gvenzl/oracle-free:23-slim"
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{"1521/tcp"},
			Env: map[string]string{
				"ORACLE_PASSWORD":      oraclePassword,
				"APP_USER":             oracleUser,
				"APP_USER_PASSWORD":    oraclePassword,
				"ENABLE_ARCHIVELOG":    "false",
				"ENABLE_FORCE_LOGGING": "false",
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("1521/tcp"),
				wait.ForLog("DATABASE IS READY TO USE!"),
			).WithDeadline(10 * time.Minute),
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
	port, err := container.MappedPort(ctx, "1521")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("oracle://%s:%s@%s:%s/?service_name=%s",
		oracleUser, oraclePassword, host, port.Port(), oracleService)

	_ = name
	return container, uri, nil
}

func startOracleContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startOracleContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
