package postgres_cdc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
)

type snapshotResumeSlotQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Saved snapshot keys are safe only while WAL can repair changes behind them.
// A logical slot starts at least at confirmed_flush_lsn even when the caller
// requests an earlier position, so both slot positions must cover the boundary.
func snapshotResumeWALAvailable(ctx context.Context, query snapshotResumeSlotQuerier, slot string, start pglogrepl.LSN) (bool, error) {
	if start == 0 {
		return false, nil
	}
	var usable bool
	var confirmed, restart string
	err := query.QueryRow(ctx, `
		SELECT slot_type = 'logical' AND plugin = 'pgoutput' AND database = current_database()
		       AND NOT active AND NOT temporary
		       AND COALESCE(to_jsonb(s)->>'wal_status', '') != 'lost'
		       AND COALESCE(to_jsonb(s)->>'invalidation_reason', '') = '',
		       COALESCE(confirmed_flush_lsn::text, ''), COALESCE(restart_lsn::text, '')
		FROM pg_replication_slots s WHERE slot_name = $1`, slot).Scan(&usable, &confirmed, &restart)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check retained WAL for snapshot resume on slot %s: %w", slot, err)
	}
	if !usable {
		return false, nil
	}
	confirmedLSN, confirmedErr := pglogrepl.ParseLSN(confirmed)
	restartLSN, restartErr := pglogrepl.ParseLSN(restart)
	return confirmedErr == nil && restartErr == nil && confirmedLSN <= start && restartLSN <= start, nil
}
