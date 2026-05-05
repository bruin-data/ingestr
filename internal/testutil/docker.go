package testutil

import (
	"context"

	"github.com/testcontainers/testcontainers-go"
)

func DockerProviderHealthy(ctx context.Context) (healthy bool) {
	defer func() {
		if r := recover(); r != nil {
			healthy = false
		}
	}()
	provider, err := testcontainers.ProviderDocker.GetProvider()
	if err != nil {
		return false
	}
	return provider.Health(ctx) == nil
}
