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
	dynamoDBAccessKey = "fakeAccessKey"
	dynamoDBSecretKey = "fakeSecretKey"
	dynamoDBRegion    = "us-east-1"
)

type dynamoDBEnv struct {
	container testcontainers.Container
	uri       string
}

func startDynamoDBContainerRaw(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "amazon/dynamodb-local:latest",
		ExposedPorts: []string{"8000/tcp"},
		WaitingFor: wait.ForListeningPort("8000/tcp").
			WithStartupTimeout(60 * time.Second),
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

	port, err := container.MappedPort(ctx, "8000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("dynamodb://%s:%s?region=%s&access_key_id=%s&secret_access_key=%s",
		host, port.Port(), dynamoDBRegion, dynamoDBAccessKey, dynamoDBSecretKey)

	return container, uri, nil
}

func startDynamoDBContainerForMain(ctx context.Context) (testcontainers.Container, string, error) {
	return startDynamoDBContainerRaw(ctx)
}
