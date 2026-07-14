package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime/pprof"
	"sync"
	"syscall"

	"github.com/bruin-data/ingestr/cmd"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

func main() {
	os.Exit(run())
}

func run() int {
	stopProfile, profiling := startCPUProfile()
	defer stopProfile()
	if profiling {
		originalExiter := cli.OsExiter
		cli.OsExiter = func(code int) {
			stopProfile()
			originalExiter(code)
		}
		defer func() { cli.OsExiter = originalExiter }()
	}

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
			return 1
		}
		if !output.IsJSON() {
			color.Red("Error: %v", err)
		}
		config.PrintFailedQuery()
		return 1
	}
	return 0
}

func startCPUProfile() (func(), bool) {
	path := os.Getenv("INGESTR_CPUPROFILE")
	if path == "" {
		return func() {}, false
	}

	f, err := os.Create(path)
	if err != nil {
		return func() {}, false
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return func() {}, false
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		})
	}, true
}
