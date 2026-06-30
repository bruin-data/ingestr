package cmd

import (
	"context"

	"github.com/bruin-data/ingestr/internal/server"
	"github.com/urfave/cli/v3"
)

func ServerCommand() *cli.Command {
	return &cli.Command{
		Name:  "server",
		Usage: "Start the web UI server",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "port",
				Usage:   "Port to listen on",
				Value:   8080,
				Sources: cli.EnvVars("INGESTR_PORT"),
			},
			&cli.StringFlag{
				Name:    "creds-file",
				Usage:   "Path to credentials file",
				Value:   "creds.json",
				Sources: cli.EnvVars("INGESTR_CREDS_FILE"),
			},
			&cli.StringFlag{
				Name:    "logs-dir",
				Usage:   "Directory to store job logs",
				Value:   "logs",
				Sources: cli.EnvVars("INGESTR_LOGS_DIR"),
			},
			&cli.StringFlag{
				Name:    "db",
				Usage:   "Path to SQLite database for storing runs",
				Value:   "ingestr.db",
				Sources: cli.EnvVars("INGESTR_DB"),
			},
			&cli.StringFlag{
				Name:    "ingestr-binary",
				Usage:   "Path to the ingestr binary used by the web UI to run ingestion jobs",
				Sources: cli.EnvVars("INGESTR_BINARY"),
			},
		},
		Action: runServer,
	}
}

func runServer(ctx context.Context, c *cli.Command) (err error) {
	trackCommandTriggered(ctx, "server")
	defer func() {
		trackCommandFinished(ctx, "server", err, nil)
	}()

	port := int(c.Int("port"))
	credsFile := c.String("creds-file")
	logsDir := c.String("logs-dir")
	dbPath := c.String("db")
	binaryPath := c.String("ingestr-binary")

	s, err := server.NewWithBinary(port, credsFile, logsDir, dbPath, binaryPath)
	if err != nil {
		return err
	}
	return s.Run(ctx)
}
