package cmd

import (
	"context"

	"github.com/urfave/cli/v3"
)

var Version = "dev"

func NewApp() *cli.Command {
	return &cli.Command{
		Name:    "gong",
		Usage:   "A CLI tool for data ingestion between databases",
		Version: Version,
		Commands: []*cli.Command{
			IngestCommand(),
			ServerCommand(),
		},
	}
}

func Run(ctx context.Context, args []string) error {
	return NewApp().Run(ctx, args)
}
