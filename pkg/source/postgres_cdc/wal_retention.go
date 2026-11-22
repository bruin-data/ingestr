package postgres_cdc

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
)

const walRetentionTimeout = 5 * time.Second

type walRetentionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *PostgresCDCSource) setWALRetentionSlot(slot string) {
	s.walRetentionMu.Lock()
	s.walRetentionSlot = slot
	s.walRetentionMu.Unlock()
}

func (s *PostgresCDCSource) resetWALRetention() {
	s.walRetentionMu.Lock()
	s.walRetentionSlot = ""
	s.walRetentionConfig = nil
	s.walRetentionMu.Unlock()
}

func (s *PostgresCDCSource) WALRetention(ctx context.Context) (source.WALRetentionSnapshot, error) {
	s.walRetentionMu.RLock()
	snapshot := source.WALRetentionSnapshot{Source: "postgres_cdc", Slot: s.walRetentionSlot}
	var connConfig *pgx.ConnConfig
	if s.walRetentionConfig != nil {
		connConfig = s.walRetentionConfig.Copy()
	}
	s.walRetentionMu.RUnlock()
	if snapshot.Slot == "" {
		return snapshot, nil
	}
	if connConfig == nil {
		return snapshot, fmt.Errorf("PostgreSQL WAL retention source is not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, walRetentionTimeout)
	defer cancel()
	// Snapshot readers can occupy every query-pool connection for the whole
	// copy, so observation needs an independent SQL session.
	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return snapshot, fmt.Errorf("connect for PostgreSQL WAL retention: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}()
	return readWALRetention(ctx, conn, snapshot.Slot)
}

func readWALRetention(ctx context.Context, query walRetentionQuerier, slot string) (source.WALRetentionSnapshot, error) {
	snapshot := source.WALRetentionSnapshot{Source: "postgres_cdc", Slot: slot}
	var status *string
	err := query.QueryRow(ctx, `
		SELECT pg_wal_lsn_diff(
		           CASE WHEN pg_is_in_recovery() THEN pg_last_wal_receive_lsn()
		                ELSE pg_current_wal_lsn() END,
		           s.restart_lsn)::double precision,
		       (to_jsonb(s)->>'safe_wal_size')::bigint,
		       pg_size_bytes(current_setting('max_slot_wal_keep_size', true)),
		       to_jsonb(s)->>'wal_status'
		FROM pg_replication_slots s
		WHERE s.slot_name = $1 AND s.database = current_database()
	`, slot).Scan(&snapshot.RetainedBytes, &snapshot.SafeBytes, &snapshot.MaxKeepBytes, &status)
	if err != nil {
		return source.WALRetentionSnapshot{Source: snapshot.Source, Slot: slot}, fmt.Errorf("read PostgreSQL WAL retention for slot %q: %w", slot, err)
	}
	if status != nil {
		snapshot.Status = *status
	}
	return snapshot, nil
}
