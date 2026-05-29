//go:build integration

package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	minioAccessKey = "minioadmin"
	minioSecretKey = "minioadmin"
)

type minioEnv struct {
	container testcontainers.Container
	endpoint  string
	uri       string
}

func startMinioContainerRaw(ctx context.Context) (testcontainers.Container, string, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "minio/minio:latest",
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     minioAccessKey,
			"MINIO_ROOT_PASSWORD": minioSecretKey,
		},
		Cmd: []string{"server", "/data"},
		WaitingFor: wait.ForHTTP("/minio/health/ready").
			WithPort("9000").
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}

	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}

	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())
	uri := fmt.Sprintf("s3://?endpoint_url=%s&access_key_id=%s&secret_access_key=%s",
		endpoint, minioAccessKey, minioSecretKey)

	return container, endpoint, uri, nil
}
