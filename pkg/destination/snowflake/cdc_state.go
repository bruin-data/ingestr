package snowflake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

func (d *SnowflakeDestination) ManagedCDCStateCatalog() string {
	return strings.ToUpper(d.database)
}

func (d *SnowflakeDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	quoted := quoteFQN(sfTable(table))
	query := fmt.Sprintf(
		`SELECT %s, %s, %s, %s, %s, %s, %s FROM %s WHERE %s = ?`,
		quoteIdentifier("event_id"),
		quoteIdentifier("source_table"),
		quoteIdentifier("destination_table"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_status"),
		quoteIdentifier(destination.CDCLSNColumn),
		quoted,
		quoteIdentifier("connector_id"),
	)
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isSnowflakeMissingObject(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var entry destination.CDCStateEntry
		if err := rows.Scan(
			&entry.EventID,
			&entry.SourceTable,
			&entry.DestinationTable,
			&entry.StateKind,
			&entry.Generation,
			&entry.Status,
			&entry.Position,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *SnowflakeDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	quoted := quoteFQN(sfTable(table))
	query := fmt.Sprintf(
		`SELECT DISTINCT %s, %s FROM %s
		WHERE %s = ? AND %s = 'run'
		  AND %s = (
			SELECT MAX(%s) FROM %s
			WHERE %s = ? AND %s = 'run'
		  )
		ORDER BY %s`,
		quoteIdentifier("event_id"),
		quoteIdentifier("state_generation"),
		quoted,
		quoteIdentifier("connector_id"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_generation"),
		quoted,
		quoteIdentifier("connector_id"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("event_id"),
	)
	rows, err := d.db.QueryContext(ctx, query, connectorID, connectorID)
	if err != nil {
		if isSnowflakeMissingObject(err) {
			return destination.CDCStateFence{}, nil
		}
		config.LogFailedQuery(query, err)
		return destination.CDCStateFence{}, err
	}
	defer func() { _ = rows.Close() }()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, err
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *SnowflakeDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = "?"
		args = append(args, eventID)
	}
	query := fmt.Sprintf(
		`DELETE FROM %s WHERE %s = ? AND %s IN (%s)`,
		quoteFQN(sfTable(table)),
		quoteIdentifier("connector_id"),
		quoteIdentifier("event_id"),
		strings.Join(placeholders, ", "),
	)
	if _, err := d.db.ExecContext(ctx, query, args...); err != nil {
		if isSnowflakeMissingObject(err) {
			return nil
		}
		config.LogFailedQuery(query, err)
		return err
	}
	return nil
}

func (d *SnowflakeDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return err
	}
	canonicalTarget, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	quotedClaim := quoteFQN(sfTable(claimTable))
	mergeSQL := fmt.Sprintf(
		`MERGE INTO %s AS t
		USING (SELECT ? AS destination_table, ? AS connector_id) AS s
		ON t.%s = s.destination_table
		WHEN NOT MATCHED THEN INSERT (%s, %s, %s)
		VALUES (s.destination_table, s.connector_id, CURRENT_TIMESTAMP())`,
		quotedClaim,
		quoteIdentifier("destination_table"),
		quoteIdentifier("destination_table"),
		quoteIdentifier("connector_id"),
		quoteIdentifier("claimed_at"),
	)
	if _, err := tx.ExecContext(ctx, mergeSQL, canonicalTarget, ownerID); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return err
	}

	var owner string
	selectSQL := fmt.Sprintf(
		`SELECT %s FROM %s WHERE %s = ?`,
		quoteIdentifier("connector_id"),
		quotedClaim,
		quoteIdentifier("destination_table"),
	)
	if err := tx.QueryRowContext(ctx, selectSQL, canonicalTarget).Scan(&owner); err != nil {
		config.LogFailedQuery(selectSQL, err)
		return err
	}
	if owner != ownerID {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}
	return tx.Commit()
}

func (d *SnowflakeDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	tn := sfTable(table)
	return d.canonicalCDCTarget(tn), nil
}

func (d *SnowflakeDestination) canonicalCDCTarget(tn tablename.TableName) string {
	catalog := tn.Catalog
	if catalog == "" {
		catalog = strings.ToUpper(d.database)
	}
	return destination.CDCTargetKey(catalog, tn.Schema, tn.Table)
}

func (d *SnowflakeDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	tn := sfTable(table)
	catalog := tn.Catalog
	if catalog == "" {
		catalog = strings.ToUpper(d.database)
	}
	infoTables := "INFORMATION_SCHEMA.TABLES"
	if catalog != "" {
		infoTables = quoteIdentifier(catalog) + ".INFORMATION_SCHEMA.TABLES"
	}
	query := fmt.Sprintf(
		`SELECT CREATED FROM %s WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_TYPE = 'BASE TABLE'`,
		infoTables,
	)
	var created time.Time
	err := d.db.QueryRowContext(ctx, query, tn.Schema, tn.Table).Scan(&created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		if isSnowflakeMissingObject(err) {
			return "", false, nil
		}
		config.LogFailedQuery(query, err)
		return "", false, fmt.Errorf("failed to read Snowflake CDC target incarnation for %s: %w", table, err)
	}
	if created.IsZero() {
		return "", false, fmt.Errorf("Snowflake table %s returned an empty creation time", table)
	}
	return destination.CDCTargetKey(catalog, tn.Schema, tn.Table, strconv.FormatInt(created.UnixNano(), 10)), true, nil
}

func isSnowflakeMissingObject(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "object does not exist") ||
		strings.Contains(msg, "002003") // SQL compilation error: object does not exist
}
