//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const elasticsearchImage = "docker.elastic.co/elasticsearch/elasticsearch:9.4.4"

func startElasticsearchContainerRaw(ctx context.Context) (testcontainers.Container, string, string, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        elasticsearchImage,
			ExposedPorts: []string{"9200/tcp"},
			Env: map[string]string{
				"discovery.type":                    "single-node",
				"xpack.security.enabled":            "false",
				"xpack.security.enrollment.enabled": "false",
				"xpack.ml.enabled":                  "false",
				"ES_JAVA_OPTS":                      "-Xms512m -Xmx512m",
			},
			WaitingFor: wait.ForHTTP("/").
				WithPort("9200/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == 200 }).
				WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return nil, "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}
	port, err := container.MappedPort(ctx, "9200/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", "", err
	}

	address := net.JoinHostPort(host, port.Port())
	return container,
		fmt.Sprintf("elasticsearch://%s?secure=false", address),
		"http://" + address,
		nil
}

func startElasticsearchContainerForMain(ctx context.Context) (testcontainers.Container, string, string, error) {
	return startElasticsearchContainerRaw(ctx)
}
