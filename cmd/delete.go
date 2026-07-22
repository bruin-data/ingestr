package cmd

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

func DeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a resource from the source (e.g. a CDC replication slot)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "uri",
				Usage:    "The URI of the source",
				Required: true,
				Sources:  cli.EnvVars("SOURCE_URI", "INGESTR_SOURCE_URI"),
			},
			&cli.StringFlag{
				Name:    "replication-slot",
				Usage:   "Name of the PostgreSQL CDC replication slot to drop",
				Sources: cli.EnvVars("INGESTR_CDC_SLOT"),
			},
		},
		Action: runDelete,
	}
}

func runDelete(ctx context.Context, c *cli.Command) (err error) {
	trackCommandTriggered(ctx, "delete")
	defer func() {
		trackCommandFinished(ctx, "delete", err, nil)
	}()

	sourceURI := c.String("uri")

	switch {
	case c.IsSet("replication-slot"):
		return dropReplicationSlot(ctx, sourceURI, c.String("replication-slot"))
	default:
		return fmt.Errorf("no delete target specified; provide one of: --replication-slot")
	}
}

func dropReplicationSlot(ctx context.Context, sourceURI, slotName string) error {
	existed, err := postgres_cdc.DropReplicationSlot(ctx, sourceURI, slotName)
	if err != nil {
		return err
	}

	if !output.IsJSON() {
		if existed {
			color.Green("Dropped replication slot %q", slotName)
		} else {
			color.Yellow("Replication slot %q does not exist; nothing to do", slotName)
		}
	}

	return nil
}
