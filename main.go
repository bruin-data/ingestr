package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/bruin-data/ingestr/cmd"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/fatih/color"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	if err := cmd.Run(ctx, os.Args); err != nil {
		if errors.Is(err, context.Canceled) {
			if !output.IsJSON() {
				color.Red("\nPipeline cancelled.")
			}
			os.Exit(1)
		}
		if !output.IsJSON() {
			color.Red("Error: %v", err)
		}
		config.PrintFailedQuery()
		os.Exit(1)
	}
}
