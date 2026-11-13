package databricks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

var (
	_ destination.CDCMergeAware                   = (*DatabricksDestination)(nil)
	_ destination.CDCHardDeleteCapable            = (*DatabricksDestination)(nil)
	_ destination.CDCUnchangedColsAware           = (*DatabricksDestination)(nil)
	_ destination.SerializedCDCRunsRequired       = (*DatabricksDestination)(nil)
	_ destination.CDCStateReader                  = (*DatabricksDestination)(nil)
	_ destination.CDCStateFenceReader             = (*DatabricksDestination)(nil)
	_ destination.CDCStatePruner                  = (*DatabricksDestination)(nil)
	_ destination.CDCTargetClaimer                = (*DatabricksDestination)(nil)
	_ destination.CDCTargetIdentityProvider       = (*DatabricksDestination)(nil)
	_ destination.CDCTargetIncarnationInitializer = (*DatabricksDestination)(nil)
	_ destination.CDCTargetIncarnationProvider    = (*DatabricksDestination)(nil)
	_ destination.CDCTruncateCapable              = (*DatabricksDestination)(nil)
	_ destination.CDCResumeProvider               = (*DatabricksDestination)(nil)
	_ destination.ManagedCDCStateCatalogProvider  = (*DatabricksDestination)(nil)
	_ destination.StagingTableLister              = (*DatabricksDestination)(nil)
	_ destination.ReplaceStagingPolicyProvider    = (*DatabricksDestination)(nil)
	_ destination.ManagedStagingPolicyProvider    = (*DatabricksDestination)(nil)
)

const cdcStateDeleteBatchSize = 200

func canonicalTargetKey(catalog, schemaName, tableName string) string {
	return destination.CDCTargetKey(
		strings.ToLower(catalog),
		strings.ToLower(schemaName),
		strings.ToLower(tableName),
	)
}

func canonicalIncarnationKey(catalog, schemaName, tableName, id string) string {
	return destination.CDCTargetKey(
		strings.ToLower(catalog),
		strings.ToLower(schemaName),
		strings.ToLower(tableName),
		id,
	)
}

func (d *DatabricksDestination) ManagedCDCStateCatalog() string {
	return strings.ToLower(d.effectiveCatalog())
}

func (d *DatabricksDestination) TruncateCDCTable(ctx context.Context, table string) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *DatabricksDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	if err := tablename.Databricks.CheckName(table); err != nil {
		return "", err
	}
	catalog, schemaName, tableName := d.parseTableParts(table)
	if schemaName == "" {
		return "", fmt.Errorf("databricks CDC requires a schema for target %q", table)
	}
	return canonicalTargetKey(catalog, schemaName, tableName), nil
}

func (d *DatabricksDestination) tableIdentity(ctx context.Context, table string) (catalog, schemaName, tableName, id string, exists bool, err error) {
	catalog, schemaName, tableName = d.parseTableParts(table)
	fullTable := quoteTableParts(catalog, schemaName, tableName)
	query := fmt.Sprintf("DESCRIBE DETAIL %s", fullTable)
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return catalog, schemaName, tableName, "", false, nil
		}
		config.LogFailedQuery(query, err)
		return "", "", "", "", false, fmt.Errorf("failed to read Databricks table identity for %s: %w", table, err)
	}
	if len(res.rows) == 0 {
		return catalog, schemaName, tableName, "", false, fmt.Errorf("databricks table %s returned no detail row", table)
	}
	row := res.rows[0]
	if idIdx := res.columnIndex("id"); idIdx >= 0 && idIdx < len(row) {
		id = strings.TrimSpace(row[idIdx])
	}
	if id == "" {
		if createdIdx := res.columnIndex("createdAt"); createdIdx >= 0 && createdIdx < len(row) {
			id = strings.TrimSpace(row[createdIdx])
		}
	}
	if id == "" {
		return catalog, schemaName, tableName, "", false, fmt.Errorf("databricks table %s returned an empty physical identity", table)
	}
	return catalog, schemaName, tableName, id, true, nil
}

func (d *DatabricksDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	catalog, schemaName, tableName, id, exists, err := d.tableIdentityWithRecovery(ctx, table)
	if err != nil || !exists {
		return "", false, err
	}
	return canonicalIncarnationKey(catalog, schemaName, tableName, id), true, nil
}

func (d *DatabricksDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("databricks CDC target %q physical incarnation changed", table)
	}
	return nil
}

func (d *DatabricksDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	if _, exists, err := d.CDCTargetIncarnation(ctx, table); err != nil || !exists {
		return "", err
	}

	tblSchema, err := d.GetTableSchema(ctx, table)
	if err != nil || tblSchema == nil {
		return "", err
	}

	lsnCol := ""
	for _, col := range tblSchema.Columns {
		if strings.EqualFold(col.Name, destination.CDCLSNColumn) {
			lsnCol = col.Name
			break
		}
	}
	if lsnCol == "" {
		return "", nil
	}

	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteIdentifier(lsnCol), d.quoteTableRef(table))
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return "", nil
		}
		config.LogFailedQuery(query, err)
		return "", fmt.Errorf("read Databricks CDC cursor: %w", err)
	}
	if len(res.rows) == 0 || len(res.rows[0]) == 0 {
		return "", nil
	}
	return res.rows[0][0], nil
}

func (d *DatabricksDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	ref := d.quoteTableRef(table)
	query := fmt.Sprintf(
		"SELECT DISTINCT %s, %s, %s, %s, %s, %s, COALESCE(%s, '') FROM %s WHERE %s = %s",
		quoteIdentifier("event_id"),
		quoteIdentifier("source_table"),
		quoteIdentifier("destination_table"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_status"),
		quoteIdentifier(destination.CDCLSNColumn),
		ref,
		quoteIdentifier("connector_id"),
		sqlString(connectorID),
	)
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("read Databricks CDC state: %w", err)
	}

	entries := make([]destination.CDCStateEntry, 0, len(res.rows))
	for _, row := range res.rows {
		if len(row) < 7 {
			return nil, fmt.Errorf("unexpected Databricks CDC state row width %d", len(row))
		}
		var gen int64
		if row[4] != "" {
			gen, err = strconv.ParseInt(row[4], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse Databricks CDC state generation %q: %w", row[4], err)
			}
		}
		entries = append(entries, destination.CDCStateEntry{
			EventID:          row[0],
			SourceTable:      row[1],
			DestinationTable: row[2],
			StateKind:        row[3],
			Generation:       gen,
			Status:           row[5],
			Position:         row[6],
		})
	}
	return entries, nil
}

func (d *DatabricksDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	ref := d.quoteTableRef(table)
	connLit := sqlString(connectorID)
	query := fmt.Sprintf(
		"SELECT DISTINCT %s, %s FROM %s WHERE %s = %s AND %s = 'run' AND %s = (SELECT MAX(%s) FROM %s WHERE %s = %s AND %s = 'run') ORDER BY %s",
		quoteIdentifier("event_id"),
		quoteIdentifier("state_generation"),
		ref,
		quoteIdentifier("connector_id"), connLit,
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_generation"),
		ref,
		quoteIdentifier("connector_id"), connLit,
		quoteIdentifier("state_kind"),
		quoteIdentifier("event_id"),
	)
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return destination.CDCStateFence{}, nil
		}
		config.LogFailedQuery(query, err)
		return destination.CDCStateFence{}, fmt.Errorf("read Databricks CDC fence: %w", err)
	}

	var fence destination.CDCStateFence
	for _, row := range res.rows {
		if len(row) < 2 {
			return destination.CDCStateFence{}, fmt.Errorf("unexpected Databricks CDC fence row width %d", len(row))
		}
		if row[1] != "" {
			gen, err := strconv.ParseInt(row[1], 10, 64)
			if err != nil {
				return destination.CDCStateFence{}, fmt.Errorf("parse Databricks CDC fence generation %q: %w", row[1], err)
			}
			fence.Generation = gen
		}
		fence.RunEventIDs = append(fence.RunEventIDs, row[0])
	}
	return fence, nil
}

func (d *DatabricksDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	ref := d.quoteTableRef(table)
	connLit := sqlString(connectorID)

	for start := 0; start < len(eventIDs); start += cdcStateDeleteBatchSize {
		end := start + cdcStateDeleteBatchSize
		if end > len(eventIDs) {
			end = len(eventIDs)
		}
		quotedIDs := make([]string, end-start)
		for i, id := range eventIDs[start:end] {
			quotedIDs[i] = sqlString(id)
		}
		query := fmt.Sprintf(
			"DELETE FROM %s WHERE %s = %s AND %s IN (%s)",
			ref,
			quoteIdentifier("connector_id"), connLit,
			quoteIdentifier("event_id"), strings.Join(quotedIDs, ", "),
		)
		if err := d.executeStatement(ctx, query); err != nil {
			if isDatabricksMissingError(err) {
				return nil
			}
			config.LogFailedQuery(query, err)
			return fmt.Errorf("delete Databricks CDC state events: %w", err)
		}
	}
	return nil
}

func (d *DatabricksDestination) claimMarker(claimTable, target string) string {
	cat, sch, _ := d.parseTableParts(claimTable)
	return quoteTableParts(cat, sch, "cdc_claim_"+destination.CDCTargetKeyDigest(target))
}

func (d *DatabricksDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	owner, err := claim.OwnerID()
	if err != nil {
		return err
	}
	target, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}

	cat, sch, _ := d.parseTableParts(claimTable)
	if err := d.ensureSchemaForParts(ctx, cat, sch); err != nil {
		return err
	}

	marker := d.claimMarker(claimTable, target)
	createMarkerSQL := fmt.Sprintf("CREATE VIEW IF NOT EXISTS %s AS SELECT %s AS owner_id", marker, sqlString(owner))
	if err := d.executeStatement(ctx, createMarkerSQL); err != nil {
		config.LogFailedQuery(createMarkerSQL, err)
		return fmt.Errorf("claim Databricks CDC target: %w", err)
	}

	res, err := d.queryStatement(ctx, "SELECT owner_id FROM "+marker)
	if err != nil {
		return fmt.Errorf("read Databricks CDC claim: %w", err)
	}
	if len(res.rows) == 0 || len(res.rows[0]) == 0 {
		return fmt.Errorf("read Databricks CDC claim: marker %s returned no owner", marker)
	}
	if actual := res.rows[0][0]; actual != owner {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", claim.DestinationTable, actual)
	}

	mergeClaimSQL := fmt.Sprintf(
		"MERGE INTO %s AS t USING (SELECT %s AS destination_table, %s AS connector_id) AS s ON t.%s = s.destination_table WHEN NOT MATCHED THEN INSERT (%s, %s, %s) VALUES (s.destination_table, s.connector_id, CURRENT_TIMESTAMP())",
		d.quoteTableRef(claimTable),
		sqlString(destination.CDCTargetKeyDigest(target)),
		sqlString(owner),
		quoteIdentifier("destination_table"),
		quoteIdentifier("destination_table"),
		quoteIdentifier("connector_id"),
		quoteIdentifier("claimed_at"),
	)
	if err := d.executeStatement(ctx, mergeClaimSQL); err != nil {
		config.LogFailedQuery(mergeClaimSQL, err)
		return fmt.Errorf("record Databricks CDC target claim: %w", err)
	}
	return nil
}
