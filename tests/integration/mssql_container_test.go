//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mssql"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mssqlPassword    = "TestPassword123!"
	mssqlPasswordEnc = "TestPassword123%21" // URL-encoded version of password
	mssqlDB          = "master"
)

func startMSSQLContainerRaw(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, err := mssql.Run(
		ctx,
		"mcr.microsoft.com/mssql/server:2022-latest",
		mssql.WithAcceptEULA(),
		mssql.WithPassword(mssqlPassword),
		// SQL Server Agent is required by the CDC capture/cleanup jobs.
		testcontainers.WithEnv(map[string]string{"MSSQL_AGENT_ENABLED": "true"}),
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForListeningPort("1433/tcp"),
				wait.ForLog("Recovery is complete."),
				// The "ready for client connections" log line precedes login
				// availability by several seconds on slower hosts; probe with a
				// real authenticated query so the first test cannot hit it.
				wait.ForSQL("1433/tcp", "sqlserver", func(host string, port string) string {
					portNum, _, _ := strings.Cut(port, "/")
					return fmt.Sprintf("server=%s;user id=sa;password=%s;port=%s;database=master;encrypt=disable", host, mssqlPassword, portNum)
				}),
			).WithDeadline(3*time.Minute),
		),
	)
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "1433")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	uri := fmt.Sprintf("mssql://sa:%s@%s:%s/%s?encrypt=disable",
		mssqlPasswordEnc, host, port.Port(), mssqlDB)

	_ = name
	return container, uri, nil
}

func startMSSQLContainerForMain(ctx context.Context, name string) (testcontainers.Container, string, error) {
	container, uri, err := startMSSQLContainerRaw(ctx, name)
	if err != nil {
		return nil, "", err
	}
	return container, uri, nil
}
