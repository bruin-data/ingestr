package postgres_cdc

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DropReplicationSlot connects to the Postgres server described by uri (a
// postgres+cdc://, postgresql+cdc://, postgres:// or postgresql:// URI) and
// drops the named logical replication slot.
//
// It returns existed=false with a nil error when the slot is already gone, so
// the operation is idempotent. Real failures (unreachable server, or a slot
// that is still active for a running walsender) are returned as errors.
func DropReplicationSlot(ctx context.Context, uri, slotName string) (existed bool, err error) {
	if slotName == "" {
		return false, fmt.Errorf("slot name is required")
	}

	_, normalizedURI, err := parseURIConfig(uri)
	if err != nil {
		return false, fmt.Errorf("failed to parse CDC config: %w", err)
	}

	pgConfig, err := pgxpool.ParseConfig(normalizedURI)
	if err != nil {
		return false, fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return false, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slotName); err != nil {
		if isMissingReplicationSlotError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to drop replication slot %q: %w", slotName, err)
	}

	return true, nil
}
