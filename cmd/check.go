package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/internal/uri"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/urfave/cli/v3"
)

func CheckCommand() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Verify a destination connection end to end",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "dest-uri",
				Usage:    "The URI of the destination to verify",
				Required: true,
				Sources:  cli.EnvVars("DESTINATION_URI", "INGESTR_DESTINATION_URI"),
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "Maximum time allowed for the connection check",
				Value: 2 * time.Minute,
			},
		},
		Action: runCheck,
	}
}

func runCheck(ctx context.Context, c *cli.Command) (err error) {
	checkCtx, cancel := context.WithTimeout(ctx, c.Duration("timeout"))
	defer cancel()

	dest, err := uri.DefaultRegistry.GetDestination(c.String("dest-uri"))
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	if err := dest.Connect(checkCtx, c.String("dest-uri")); err != nil {
		return fmt.Errorf("failed to connect to destination: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer closeCancel()
		if closeErr := dest.Close(closeCtx); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close destination: %w", closeErr))
		}
	}()

	checker, ok := dest.(destination.ConnectionChecker)
	if !ok {
		return fmt.Errorf("destination %s does not support an end-to-end connection check", dest.GetScheme())
	}
	if err := checker.CheckConnection(checkCtx); err != nil {
		return fmt.Errorf("destination connection check failed: %w", err)
	}

	_, err = fmt.Fprintln(c.Root().Writer, "Destination connection check succeeded")
	return err
}
