package postgres_cdc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresCDCSource) ConfigureNoSnapshot(ctx context.Context, positions map[string]string) error {
	s.neverSnapshot = true
	if len(positions) == 0 {
		return fmt.Errorf("CDC snapshot mode never requires a resume boundary for every selected table")
	}
	var minimum pglogrepl.LSN
	for table, position := range positions {
		lsn, err := pglogrepl.ParseLSN(position)
		if err != nil || lsn == 0 {
			return fmt.Errorf("CDC snapshot mode never: no valid resume boundary for %s", table)
		}
		if minimum == 0 || lsn < minimum {
			minimum = lsn
		}
	}
	return s.validateNoSnapshotSlot(ctx, minimum)
}

func (s *PostgresCDCSource) validateNoSnapshotSlot(ctx context.Context, start pglogrepl.LSN) error {
	if !s.neverSnapshot {
		return nil
	}
	if s.cdcConfig.SlotName == "" {
		return fmt.Errorf("CDC snapshot mode never requires an explicit existing slot= replication slot")
	}
	var usable bool
	var confirmed, restart, current string
	currentLSNExpr := "pg_current_wal_lsn()"
	if s.standby {
		currentLSNExpr = "pg_last_wal_replay_lsn()"
	}
	err := s.queryPool.QueryRow(ctx, `
		SELECT slot_type = 'logical' AND plugin = 'pgoutput' AND database = current_database()
		       AND NOT active AND NOT temporary AND COALESCE(to_jsonb(s)->>'wal_status', '') != 'lost',
		       COALESCE(confirmed_flush_lsn::text, ''), COALESCE(restart_lsn::text, ''), `+currentLSNExpr+`::text
		FROM pg_replication_slots s WHERE slot_name = $1`, s.cdcConfig.SlotName).Scan(&usable, &confirmed, &restart, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("CDC snapshot mode never: replication slot %q is missing; refusing to snapshot", s.cdcConfig.SlotName)
	}
	if err != nil {
		return fmt.Errorf("failed to validate CDC replication slot: %w", err)
	}
	confirmedLSN, confirmedErr := pglogrepl.ParseLSN(confirmed)
	restartLSN, restartErr := pglogrepl.ParseLSN(restart)
	currentLSN, currentErr := pglogrepl.ParseLSN(current)
	if !usable || confirmedErr != nil || restartErr != nil || currentErr != nil {
		return fmt.Errorf("CDC snapshot mode never: slot %q must be an inactive persistent pgoutput slot in this database with retained WAL", s.cdcConfig.SlotName)
	}
	if start < confirmedLSN || start < restartLSN || start > currentLSN {
		return fmt.Errorf("CDC snapshot mode never: slot %q cannot honor LSN %s (confirmed_flush_lsn=%s, restart_lsn=%s, current_lsn=%s); refusing to skip changes or snapshot", s.cdcConfig.SlotName, start, confirmed, restart, current)
	}
	return nil
}

func (s *PostgresCDCSource) requireSnapshotAllowed() error {
	if s.neverSnapshot {
		return fmt.Errorf("CDC snapshot mode never prohibits snapshot or backfill; restore a valid checkpoint and slot, or establish a new external baseline")
	}
	return nil
}
