package integration

import (
	"context"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

type rabbitmqEnv struct {
	container testcontainers.Container
	uri       string
}

func startRabbitMQContainerForMain(ctx context.Context) (testcontainers.Container, string, error) {
	container, err := rabbitmq.Run(
		ctx,
		"rabbitmq:3-management-alpine",
	)
	if err != nil {
		return nil, "", err
	}

	uri, err := container.AmqpURL(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	return container, uri, nil
}
