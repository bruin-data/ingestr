package postgres_cdc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// standbySlotHintAfter is how long slot creation may block on a standby before
// ingestr explains why. A standby builds the slot's initial snapshot from a
// running-transactions record replayed from the primary, which the primary
// only logs every 15s and only while it has write activity.
const standbySlotHintAfter = 20 * time.Second

// standbyBarrierProbeInterval is how often a standby batch run asks the
// walsender for a keepalive while waiting to decode past its target.
const standbyBarrierProbeInterval = time.Second

// createLogicalSlot creates a pgoutput slot that exports its snapshot.
func (s *PostgresCDCSource) createLogicalSlot(ctx context.Context, conn *pgconn.PgConn, name string, temporary bool) (pglogrepl.CreateReplicationSlotResult, error) {
	if s.standby {
		hint := time.AfterFunc(standbySlotHintAfter, func() {
			output.Warnf("Warning: creating replication slot %s on a PostgreSQL standby is waiting for the primary to log a running-transactions record; this needs write activity on the primary, or run SELECT pg_log_standby_snapshot(); there\n", name)
		})
		defer hint.Stop()
	}
	return pglogrepl.CreateReplicationSlot(ctx, conn, name, "pgoutput", pglogrepl.CreateReplicationSlotOptions{
		Temporary:      temporary,
		SnapshotAction: "EXPORT_SNAPSHOT",
		Mode:           pglogrepl.LogicalReplication,
	})
}

// explainSlotFailure names the reason Postgres invalidated a slot when starting
// replication from it fails, and how to recover. Postgres only reports that
// the slot can no longer be read.
func (s *PostgresCDCSource) explainSlotFailure(ctx context.Context, slotName string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgErrObjectNotInPrerequisiteState || s.queryPool == nil {
		return err
	}
	var reasonExpr string
	switch {
	case s.serverVersion >= 170000:
		reasonExpr = "invalidation_reason"
	case s.serverVersion >= 160000:
		reasonExpr = "CASE WHEN conflicting THEN 'conflict with recovery' WHEN wal_status = 'lost' THEN 'wal_removed' END"
	default:
		reasonExpr = "CASE WHEN wal_status = 'lost' THEN 'wal_removed' END"
	}
	var reason *string
	if qerr := s.queryPool.QueryRow(ctx, "SELECT "+reasonExpr+" FROM pg_replication_slots WHERE slot_name = $1", slotName).Scan(&reason); qerr != nil || reason == nil {
		return err
	}
	const feedbackHint = "enable hot_standby_feedback on the standby and have it stream from the primary through a physical replication slot (primary_slot_name)"
	const walLevelHint = "keep wal_level=logical on the primary"
	hint := ""
	if s.standby {
		switch *reason {
		case "rows_removed":
			hint = "; to keep standby slots valid, " + feedbackHint
		case "wal_level_insufficient":
			hint = "; to keep standby slots valid, " + walLevelHint
		case "conflict with recovery":
			hint = "; to keep standby slots valid, " + feedbackHint + ", and " + walLevelHint
		}
	}
	return fmt.Errorf("replication slot %q was invalidated by PostgreSQL (%s) and can no longer stream changes; rerun with --full-refresh to rebuild the destination from a new snapshot%s: %w", slotName, *reason, hint, err)
}

// checkManagedPublicationOnStandby stands in for reconciling the managed
// publication when connected to a standby, which cannot run DDL. The
// publication must be created on the primary and reach the standby through
// replication; eligible tables it does not cover are reported, not added.
func (s *PostgresCDCSource) checkManagedPublicationOnStandby(ctx context.Context) error {
	included, _, err := selectPublishableTables(ctx, s.queryPool)
	if err != nil {
		return err
	}
	pubIdent := pgx.Identifier{s.cdcConfig.Publication}.Sanitize()

	var pubAllTables bool
	err = s.queryPool.QueryRow(ctx, "SELECT puballtables FROM pg_publication WHERE pubname = $1", s.cdcConfig.Publication).Scan(&pubAllTables)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		create := createPublicationSQL(pubIdent, quotePublicationTables(included))
		if s.serverVersion >= 180000 {
			create += " WITH (publish_generated_columns = stored)"
		}
		return fmt.Errorf("publication %q does not exist, and a PostgreSQL standby cannot create it; run this on the primary, or pass publication=<name> for an existing one: %s", s.cdcConfig.Publication, create)
	case err != nil:
		return fmt.Errorf("failed to check publication existence: %w", err)
	}

	if !pubAllTables {
		published, err := s.queryPool.Query(ctx, "SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1", s.cdcConfig.Publication)
		if err != nil {
			return fmt.Errorf("failed to list publication tables: %w", err)
		}
		covered := make(map[pgTableRef]struct{})
		for published.Next() {
			var t pgTableRef
			if err := published.Scan(&t.schema, &t.name); err != nil {
				published.Close()
				return fmt.Errorf("failed to scan publication table: %w", err)
			}
			covered[t] = struct{}{}
		}
		published.Close()
		if err := published.Err(); err != nil {
			return fmt.Errorf("failed to list publication tables: %w", err)
		}
		var missing []pgTableRef
		for _, t := range included {
			if _, ok := covered[t]; !ok {
				missing = append(missing, t)
			}
		}
		if gap := quotePublicationTables(missing); gap != s.standbyGapWarned {
			s.standbyGapWarned = gap
			if gap != "" {
				output.Warnf("Warning: publication %q does not include %s, so changes to those tables are not replicated; to include them, run this on the primary: ALTER PUBLICATION %s ADD TABLE %s\n", s.cdcConfig.Publication, gap, pubIdent, gap)
			}
		}
	}
	return s.validatePublicationShape(ctx)
}

// standbyResumePosition turns a keepalive from a logical walsender into a
// resume position. A standby cannot emit logical messages, so keepalives are
// its only progress signal across WAL that produced no rows. A logical
// walsender's keepalive carries its decoding position, and every transaction
// that committed before that position has already been sent. A transaction
// committing exactly at it has not, and resume filtering treats a change at
// the resume LSN as applied, so the position is one byte earlier. ok is false
// when a transaction has started but not committed: its changes before the
// keepalive must not be skipped.
func standbyResumePosition(walEnd pglogrepl.LSN, inFlight bool) (pglogrepl.LSN, bool) {
	if walEnd == 0 || inFlight {
		return 0, false
	}
	return walEnd - 1, true
}
