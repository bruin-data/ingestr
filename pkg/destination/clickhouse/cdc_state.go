package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

const cdcStatePruneBatchSize = 500

func (d *ClickHouseDestination) ValidateManagedCDCState() error {
	return nil
}

func (d *ClickHouseDestination) CDCStatePruneBatchSize() int {
	return cdcStatePruneBatchSize
}

func (d *ClickHouseDestination) PrepareCDCStateTable(ctx context.Context, table string, tableSchema *schema.TableSchema) error {
	if err := tablename.TwoLevel("clickhouse").CheckName(table); err != nil {
		return err
	}
	if tableSchema == nil {
		return fmt.Errorf("schema is required")
	}

	db, name := d.parseTableName(table)
	if err := d.ensureDatabaseExists(ctx, db); err != nil {
		return fmt.Errorf("failed to ensure CDC state database exists: %w", err)
	}

	engineType := d.engineType
	if engineType == "" || strings.EqualFold(engineType, "replacing_merge_tree") {
		engineType = "merge_tree"
	}
	createSQL := buildCreateTableSQLWithOptions(
		db, name, tableSchema.Columns, []string{"connector_id", "event_id"},
		nil, false, false, false, engineType, d.engineSettings, d.cluster,
	)
	if err := d.conn.Exec(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return fmt.Errorf("failed to create ClickHouse CDC state table %s: %w", table, err)
	}
	return nil
}

func (d *ClickHouseDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	db, name := d.parseTableName(table)
	var colType string
	err := d.conn.QueryRow(
		ctx,
		"SELECT type FROM system.columns WHERE database = ? AND table = ? AND name = ?",
		db, name, destination.CDCLSNColumn,
	).Scan(&colType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect ClickHouse CDC state position column: %w", err)
	}
	if !strings.Contains(strings.ToLower(colType), "string") {
		return fmt.Errorf("ClickHouse CDC state position column must be a String, found %s", colType)
	}
	return nil
}

func (d *ClickHouseDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf(
		"SELECT DISTINCT ifNull(event_id, ''), ifNull(source_table, ''), ifNull(destination_table, ''), ifNull(state_kind, ''), state_generation, ifNull(state_status, ''), ifNull(%s, '') FROM %s WHERE connector_id = ?",
		quoteIdentifier(destination.CDCLSNColumn), d.tableRef(table),
	)
	rows, err := d.conn.Query(ctx, query, connectorID)
	if err != nil {
		if isMissingTableOrDB(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ClickHouse CDC state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var e destination.CDCStateEntry
		if err := rows.Scan(&e.EventID, &e.SourceTable, &e.DestinationTable, &e.StateKind, &e.Generation, &e.Status, &e.Position); err != nil {
			return nil, fmt.Errorf("scan ClickHouse CDC state entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (d *ClickHouseDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	ref := d.tableRef(table)
	var maxGen int64
	var count uint64
	if err := d.conn.QueryRow(
		ctx,
		"SELECT max(state_generation), count() FROM "+ref+" WHERE connector_id = ? AND state_kind = 'run'",
		connectorID,
	).Scan(&maxGen, &count); err != nil {
		if isMissingTableOrDB(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, fmt.Errorf("read ClickHouse CDC fence generation: %w", err)
	}
	if count == 0 {
		return destination.CDCStateFence{}, nil
	}

	rows, err := d.conn.Query(
		ctx,
		"SELECT DISTINCT event_id FROM "+ref+" WHERE connector_id = ? AND state_kind = 'run' AND state_generation = ? ORDER BY event_id",
		connectorID, maxGen,
	)
	if err != nil {
		if isMissingTableOrDB(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, fmt.Errorf("read ClickHouse CDC fence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	fence := destination.CDCStateFence{Generation: maxGen}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return destination.CDCStateFence{}, fmt.Errorf("scan ClickHouse CDC fence event: %w", err)
		}
		fence.RunEventIDs = append(fence.RunEventIDs, id)
	}
	return fence, rows.Err()
}

func (d *ClickHouseDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	db, name := d.parseTableName(table)
	ref := d.tableRef(table)

	replicated, err := d.isReplicatedTarget(ctx, db, name)
	if err != nil {
		if isMissingTableOrDB(err) {
			return nil
		}
		return err
	}
	mutationSettings := ""
	if replicated {
		mutationSettings = " SETTINGS mutations_sync = 2"
	}

	for start := 0; start < len(eventIDs); start += cdcStatePruneBatchSize {
		end := start + cdcStatePruneBatchSize
		if end > len(eventIDs) {
			end = len(eventIDs)
		}
		quotedIDs := make([]string, end-start)
		for i, id := range eventIDs[start:end] {
			quotedIDs[i] = sqlString(id)
		}
		deleteSQL := fmt.Sprintf(
			"ALTER TABLE %s%s DELETE WHERE connector_id = %s AND event_id IN (%s)%s",
			ref, onClusterClause(d.cluster), sqlString(connectorID), strings.Join(quotedIDs, ", "), mutationSettings,
		)
		if err := d.conn.Exec(ctx, deleteSQL); err != nil {
			if isMissingTableOrDB(err) {
				return nil
			}
			config.LogFailedQuery(deleteSQL, err)
			return fmt.Errorf("delete ClickHouse CDC state events: %w", err)
		}
		if err := d.waitForMutations(ctx, db, name); err != nil {
			return fmt.Errorf("wait for ClickHouse CDC state event deletion: %w", err)
		}
	}
	return nil
}

func (d *ClickHouseDestination) claimMarker(claimTable, target string) (string, string, string) {
	db, _ := d.parseTableName(claimTable)
	name := "cdc_claim_" + destination.CDCTargetKeyDigest(target)
	return db, name, fmt.Sprintf("%s.%s", quoteIdentifier(db), quoteIdentifier(name))
}

func (d *ClickHouseDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	owner, err := claim.OwnerID()
	if err != nil {
		return err
	}
	target, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	claimDB, _, marker := d.claimMarker(claimTable, target)
	if err := d.ensureDatabaseExists(ctx, claimDB); err != nil {
		return fmt.Errorf("ensure ClickHouse CDC claim database: %w", err)
	}

	createViewSQL := fmt.Sprintf("CREATE VIEW IF NOT EXISTS %s%s AS SELECT %s AS owner_id",
		marker, onClusterClause(d.cluster), sqlString(owner))
	if err := d.conn.Exec(ctx, createViewSQL); err != nil {
		config.LogFailedQuery(createViewSQL, err)
		return fmt.Errorf("claim ClickHouse CDC target: %w", err)
	}

	var actual string
	if err := d.conn.QueryRow(ctx, "SELECT owner_id FROM "+marker).Scan(&actual); err != nil {
		return fmt.Errorf("read ClickHouse CDC claim: %w", err)
	}
	if actual != owner {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", claim.DestinationTable, actual)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (destination_table, connector_id, claimed_at) VALUES (%s, %s, now64(6))",
		d.tableRef(claimTable), sqlString(destination.CDCTargetKeyDigest(target)), sqlString(owner),
	)
	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		if isMissingTableOrDB(err) {
			return nil
		}
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("record ClickHouse CDC target claim: %w", err)
	}
	return nil
}

func (d *ClickHouseDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if opts.Schema == nil {
		return "", errors.New("cannot create an empty managed CDC target without a schema")
	}
	target, err := d.CanonicalCDCTarget(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	claimed, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return "", err
	}
	if claimed != target {
		return "", fmt.Errorf("CDC target claim %q does not match prepared table %q", claim.DestinationTable, opts.Table)
	}
	if _, exists, err := d.CDCTargetIncarnation(ctx, opts.Table); err != nil {
		return "", err
	} else if exists {
		return "", fmt.Errorf("destination table %q already exists", opts.Table)
	}

	_, _, marker := d.claimMarker(claimTable, target)
	var existing string
	claimExisted := d.conn.QueryRow(ctx, "SELECT owner_id FROM "+marker).Scan(&existing) == nil

	if err := d.ClaimCDCTarget(ctx, claimTable, claim); err != nil {
		return "", err
	}

	opts.DropFirst = false
	if err := d.prepareTable(ctx, opts, true); err != nil {
		if !claimExisted {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			owner, _ := claim.OwnerID()
			dropViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s%s", marker, onClusterClause(d.cluster))
			dropErr := d.conn.Exec(cleanupCtx, dropViewSQL)
			deleteClaimSQL := fmt.Sprintf(
				"ALTER TABLE %s%s DELETE WHERE destination_table = %s AND connector_id = %s",
				d.tableRef(claimTable), onClusterClause(d.cluster),
				sqlString(destination.CDCTargetKeyDigest(target)), sqlString(owner),
			)
			deleteErr := d.conn.Exec(cleanupCtx, deleteClaimSQL)
			if isMissingTableOrDB(deleteErr) {
				deleteErr = nil
			} else if deleteErr == nil {
				claimDB, claimName := d.parseTableName(claimTable)
				deleteErr = d.waitForMutations(cleanupCtx, claimDB, claimName)
			}
			return "", errors.Join(err, dropErr, deleteErr)
		}
		return "", err
	}

	incarnation, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("claimed ClickHouse CDC target %q disappeared after creation", opts.Table)
	}
	return incarnation, nil
}
