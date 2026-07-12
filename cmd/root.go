package cmd

import (
	"context"

	"github.com/urfave/cli/v3"
)

var Version = "dev"

func versionFlagValue() string {
	return Version
}

func NewApp() *cli.Command {
	return &cli.Command{
		Name:    "ingestr",
		Usage:   "A CLI tool for data ingestion between databases",
		Version: versionFlagValue(),
		Commands: []*cli.Command{
			IngestCommand(),
			CheckCommand(),
			ServerCommand(),
		},
	}
}

func Run(ctx context.Context, args []string) error {
	return NewApp().Run(ctx, args)
}
