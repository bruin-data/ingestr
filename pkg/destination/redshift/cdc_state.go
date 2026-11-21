package redshift

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
)

const (
	defaultRedshiftLeaseTTL           = 45 * time.Second
	defaultRedshiftLeaseRenewInterval = 7500 * time.Millisecond
	cdcRunLeasesTableName             = "cdc_run_leases"
	cdcStateDeleteBatchSize           = 500
)

var (
	_ destination.ManagedCDCStateValidator     = (*RedshiftDestination)(nil)
	_ destination.CDCStateTablePreparer        = (*RedshiftDestination)(nil)
	_ destination.CDCStateReader               = (*RedshiftDestination)(nil)
	_ destination.CDCStateFenceReader          = (*RedshiftDestination)(nil)
	_ destination.CDCStateWriter               = (*RedshiftDestination)(nil)
	_ destination.CDCStatePositionMigrator     = (*RedshiftDestination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*RedshiftDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*RedshiftDestination)(nil)
	_ destination.CDCTargetClaimer             = (*RedshiftDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer   = (*RedshiftDestination)(nil)
	_ destination.CDCConditionalTruncater      = (*RedshiftDestination)(nil)
	_ destination.CDCTruncateCapable           = (*RedshiftDestination)(nil)
	_ destination.CDCConditionalMergeCapable   = (*RedshiftDestination)(nil)
	_ destination.CDCConditionalSwapCapable    = (*RedshiftDestination)(nil)
	_ destination.CDCConditionalSwapPlanner    = (*RedshiftDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger    = (*RedshiftDestination)(nil)
	_ destination.ManagedCDCRunLeaser          = (*RedshiftDestination)(nil)
	_ destination.SerializedCDCRunsRequired    = (*RedshiftDestination)(nil)
	_ destination.CDCStatePruner               = (*RedshiftDestination)(nil)
	_ destination.CDCHardDeleteCapable         = (*RedshiftDestination)(nil)
	_ destination.CDCMergeAware                = (*RedshiftDestination)(nil)
	_ destination.CDCUnchangedColsAware        = (*RedshiftDestination)(nil)
	_ destination.CDCConditionalSchemaEvolver  = (*RedshiftDestination)(nil)
	_ destination.ReplaceStagingPolicyProvider = (*RedshiftDestination)(nil)
	_ destination.ManagedStagingPolicyProvider = (*RedshiftDestination)(nil)
	_ destination.CDCResumeProvider            = (*RedshiftDestination)(nil)
)

func (d *RedshiftDestination) ValidateManagedCDCState() error { return nil }

func (d *RedshiftDestination) RequiresSerializedCDCRuns() bool { return true }

func (d *RedshiftDestination) SupportsCDCConditionalMerge() bool { return true }

func (d *RedshiftDestination) SupportsCDCConditionalSwap() bool { return true }

func (d *RedshiftDestination) SupportsCDCHardDelete() bool { return true }

func (d *RedshiftDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	db, err := d.requireDB()
	if err != nil {
		return "", err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return "", err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return "", err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	query := fmt.Sprintf(`SELECT MAX(%s) FROM %s`, destination.QuoteIdentifier(destination.CDCLSNColumn), quoteTable(schemaName, tableName))
	var maxLSN *string
	if err := db.QueryRow(ctx, query).Scan(&maxLSN); err != nil {
		return "", fmt.Errorf("failed to query max CDC LSN from %s: %w", table, err)
	}
	if maxLSN == nil {
		return "", nil
	}
	return *maxLSN, nil
}

func (d *RedshiftDestination) PrepareCDCStateTable(ctx context.Context, table string, tableSchema *schema.TableSchema) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	if err := ensureSchemaExists(ctx, db, schemaName); err != nil {
		return err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return err
	}
	if exists {
		return d.EnsureCDCStatePositionColumn(ctx, table)
	}
	if tableSchema == nil || len(tableSchema.Columns) == 0 {
		return fmt.Errorf("cannot prepare CDC state table %s without schema columns", table)
	}
	createSQL := buildRedshiftCreateTableSQL(schemaName, tableName, destination.PrepareOptions{
		Table:  table,
		Schema: tableSchema,
	})
	if _, err := db.Exec(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create Redshift CDC state table %s: %w", table, err)
	}
	return nil
}

func (d *RedshiftDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	var dataType string
	var maxLen *int
	err = db.QueryRow(ctx, `
		SELECT data_type, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		  AND LOWER(column_name) = $3
		LIMIT 1
	`, schemaName, tableName, strings.ToLower(destination.CDCLSNColumn)).Scan(&dataType, &maxLen)
	tableRef := quoteTable(schemaName, tableName)
	lsnCol := destination.QuoteIdentifier(destination.CDCLSNColumn)
	if errors.Is(err, pgx.ErrNoRows) {
		alterSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s VARCHAR(65535)`, tableRef, lsnCol)
		if _, execErr := db.Exec(ctx, alterSQL); execErr != nil {
			return fmt.Errorf("failed to add %s column to %s: %w", destination.CDCLSNColumn, table, execErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to inspect %s column on %s: %w", destination.CDCLSNColumn, table, err)
	}

	if maxLen != nil && *maxLen < 65535 {
		// Redshift disallows ALTER COLUMN TYPE inside explicit transaction blocks.
		alterSQL := fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE VARCHAR(65535)`, tableRef, lsnCol)
		if _, execErr := db.Exec(ctx, alterSQL); execErr != nil {
			return fmt.Errorf("failed to widen %s column on %s: %w", destination.CDCLSNColumn, table, execErr)
		}
	}
	return nil
}

func (d *RedshiftDestination) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		for result := range records {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		return err
	}

	writeErr := d.withTx(ctx, func(tx redshiftTx) error {
		for result := range records {
			if result.Err != nil {
				if result.Batch != nil {
					result.Batch.Release()
				}
				return result.Err
			}
			record := result.Batch
			if record == nil {
				continue
			}
			numRows := int(record.NumRows())
			numCols := int(record.NumCols())
			if numRows == 0 {
				record.Release()
				continue
			}
			if err := source.ConnectorLeaseLoss(ctx); err != nil {
				record.Release()
				return err
			}

			columns := make([]string, numCols)
			for i := 0; i < numCols; i++ {
				columns[i] = record.ColumnName(i)
			}
			const maxParams = 65535
			rowsPerInsert := min(1000, max(1, maxParams/max(1, numCols)))
			for offset := 0; offset < numRows; offset += rowsPerInsert {
				end := min(offset+rowsPerInsert, numRows)
				sql, args := buildInsert(opts.Table, columns, record, offset, end)
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					record.Release()
					return fmt.Errorf("failed to write Redshift CDC state batch: %w", err)
				}
			}
			record.Release()
		}
		return source.ConnectorLeaseLoss(ctx)
	})

	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
	}
	return writeErr
}

func (d *RedshiftDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	db, err := d.requireDB()
	if err != nil {
		return nil, err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return nil, err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return nil, err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	hasPosition, err := columnExists(ctx, db, schemaName, tableName, destination.CDCLSNColumn)
	if err != nil {
		return nil, err
	}
	positionExpr := `''`
	if hasPosition {
		positionExpr = fmt.Sprintf(`COALESCE(%s, '')`, destination.QuoteIdentifier(destination.CDCLSNColumn))
	}

	query := fmt.Sprintf(
		`SELECT "event_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", %s, "recorded_at"
		 FROM %s
		 WHERE "connector_id" = $1
		 ORDER BY "recorded_at" ASC, "event_id" ASC`,
		positionExpr,
		quoteTable(schemaName, tableName),
	)
	rows, err := db.Query(ctx, query, connectorID)
	if err != nil {
		return nil, fmt.Errorf("failed to load Redshift CDC state from %s: %w", table, err)
	}
	defer rows.Close()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var e destination.CDCStateEntry
		if scanErr := rows.Scan(
			&e.EventID,
			&e.SourceTable,
			&e.DestinationTable,
			&e.StateKind,
			&e.Generation,
			&e.Status,
			&e.Position,
			&e.RecordedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan Redshift CDC state row from %s: %w", table, scanErr)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate Redshift CDC state from %s: %w", table, err)
	}
	return entries, nil
}

func (d *RedshiftDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	db, err := d.requireDB()
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return destination.CDCStateFence{}, err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	if !exists {
		return destination.CDCStateFence{}, nil
	}

	tableRef := quoteTable(schemaName, tableName)
	var maxGen *int64
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT MAX("state_generation") FROM %s WHERE "connector_id" = $1 AND "state_kind" = 'run'`,
		tableRef,
	), connectorID).Scan(&maxGen)
	if err != nil {
		return destination.CDCStateFence{}, fmt.Errorf("failed to query Redshift CDC fence generation from %s: %w", table, err)
	}
	if maxGen == nil {
		return destination.CDCStateFence{}, nil
	}

	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT "event_id" FROM %s WHERE "connector_id" = $1 AND "state_kind" = 'run' AND "state_generation" = $2 ORDER BY "event_id" ASC`,
		tableRef,
	), connectorID, *maxGen)
	if err != nil {
		return destination.CDCStateFence{}, fmt.Errorf("failed to load Redshift CDC fence events from %s: %w", table, err)
	}
	defer rows.Close()

	var eventIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return destination.CDCStateFence{}, fmt.Errorf("failed to scan Redshift CDC fence event from %s: %w", table, scanErr)
		}
		eventIDs = append(eventIDs, id)
	}
	if err := rows.Err(); err != nil {
		return destination.CDCStateFence{}, fmt.Errorf("failed to iterate Redshift CDC fence events from %s: %w", table, err)
	}

	return destination.CDCStateFence{
		Generation:  *maxGen,
		RunEventIDs: eventIDs,
	}, nil
}

func (d *RedshiftDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	exists, err := tableExists(ctx, db, schemaName, tableName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	tableRef := quoteTable(schemaName, tableName)
	return d.withTx(ctx, func(tx redshiftTx) error {
		for start := 0; start < len(eventIDs); start += cdcStateDeleteBatchSize {
			end := min(start+cdcStateDeleteBatchSize, len(eventIDs))
			chunk := eventIDs[start:end]
			placeholders := make([]string, len(chunk))
			args := make([]any, 0, len(chunk)+1)
			args = append(args, connectorID)
			for i, id := range chunk {
				placeholders[i] = fmt.Sprintf("$%d", i+2)
				args = append(args, id)
			}
			delSQL := fmt.Sprintf(
				`DELETE FROM %s WHERE "connector_id" = $1 AND "event_id" IN (%s)`,
				tableRef, strings.Join(placeholders, ", "),
			)
			if _, execErr := tx.Exec(ctx, delSQL, args...); execErr != nil {
				return fmt.Errorf("failed to delete Redshift CDC state events from %s: %w", table, execErr)
			}
		}
		return nil
	})
}

func (d *RedshiftDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	db, err := d.requireDB()
	if err != nil {
		return "", err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return "", err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return "", err
	}
	return schemaName + "." + tableName, nil
}

func (d *RedshiftDestination) resolveDBOID(ctx context.Context, queryer redshiftQueryer) (int64, error) {
	d.dbOIDMu.Lock()
	defer d.dbOIDMu.Unlock()
	if d.dbOID > 0 {
		return d.dbOID, nil
	}
	var dbOID int64
	err := queryer.QueryRow(ctx, `SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = current_database()`).Scan(&dbOID)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve Redshift database OID: %w", err)
	}
	d.dbOID = dbOID
	return dbOID, nil
}

func (d *RedshiftDestination) targetIncarnationInTx(ctx context.Context, queryer redshiftQueryer, schemaName, tableName string) (string, bool, error) {
	var relOID int64
	err := queryer.QueryRow(ctx, `
		SELECT c.oid::bigint
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p')
		  AND n.nspname = $1
		  AND c.relname = $2
		LIMIT 1
	`, schemaName, tableName).Scan(&relOID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read Redshift CDC target incarnation for %s.%s: %w", schemaName, tableName, err)
	}
	dbOID, err := d.resolveDBOID(ctx, queryer)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%d:%d", dbOID, relOID), true, nil
}

func (d *RedshiftDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	db, err := d.requireDB()
	if err != nil {
		return "", false, err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return "", false, err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return "", false, err
	}
	return d.targetIncarnationInTx(ctx, db, schemaName, tableName)
}

func (d *RedshiftDestination) ensureCDCTargetClaimTable(ctx context.Context, queryer redshiftQueryer, claimTable string) (string, error) {
	claimSchema, claimName, err := d.resolveSchemaTable(ctx, queryer, claimTable)
	if err != nil {
		return "", err
	}
	if err := ensureSchemaExists(ctx, queryer, claimSchema); err != nil {
		return "", err
	}
	claimRef := quoteTable(claimSchema, claimName)
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			"destination_table" VARCHAR(2048) NOT NULL,
			"connector_id" VARCHAR(64) NOT NULL,
			"claimed_at" TIMESTAMP WITH TIME ZONE NOT NULL
		)
	`, claimRef)
	if _, err := queryer.Exec(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to prepare Redshift CDC claim table %s: %w", claimTable, err)
	}
	return claimRef, nil
}

func (d *RedshiftDestination) claimCDCTargetInTx(ctx context.Context, tx redshiftTx, claimRef, canonicalTarget, owner string) error {
	if _, err := tx.Exec(ctx, "LOCK "+claimRef); err != nil {
		return fmt.Errorf("failed to lock CDC claim table %s: %w", claimRef, err)
	}
	var existingOwner string
	err := tx.QueryRow(ctx, fmt.Sprintf(
		`SELECT "connector_id" FROM %s WHERE "destination_table" = $1 LIMIT 1`,
		claimRef,
	), canonicalTarget).Scan(&existingOwner)
	if err == nil {
		if existingOwner != owner {
			return fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, existingOwner)
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to read CDC claim for %q: %w", canonicalTarget, err)
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s ("destination_table", "connector_id", "claimed_at") VALUES ($1, $2, CURRENT_TIMESTAMP)`,
		claimRef,
	)
	if _, err := tx.Exec(ctx, insertSQL, canonicalTarget, owner); err != nil {
		return fmt.Errorf("failed to record CDC target claim for %q: %w", canonicalTarget, err)
	}
	return nil
}

func (d *RedshiftDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	owner, err := claim.OwnerID()
	if err != nil {
		return err
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	canonicalTarget, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	claimRef, err := d.ensureCDCTargetClaimTable(ctx, db, claimTable)
	if err != nil {
		return err
	}
	return d.withTx(ctx, func(tx redshiftTx) error {
		return d.claimCDCTargetInTx(ctx, tx, claimRef, canonicalTarget, owner)
	})
}

func (d *RedshiftDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return "", err
	}
	owner, err := claim.OwnerID()
	if err != nil {
		return "", err
	}
	db, err := d.requireDB()
	if err != nil {
		return "", err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return "", err
	}
	canonicalClaimTarget, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return "", err
	}
	targetSchema, targetTable, err := d.resolveSchemaTable(ctx, db, opts.Table)
	if err != nil {
		return "", err
	}
	if targetSchema+"."+targetTable != canonicalClaimTarget {
		return "", fmt.Errorf("CDC claim target %q does not match prepare target %q", canonicalClaimTarget, targetSchema+"."+targetTable)
	}
	if opts.Schema == nil || len(opts.Schema.Columns) == 0 {
		return "", fmt.Errorf("cannot prepare empty CDC target %s without schema columns", opts.Table)
	}

	claimRef, err := d.ensureCDCTargetClaimTable(ctx, db, claimTable)
	if err != nil {
		return "", err
	}
	if err := ensureSchemaExists(ctx, db, targetSchema); err != nil {
		return "", err
	}

	var incarnation string
	err = d.withTx(ctx, func(tx redshiftTx) error {
		resolvedSchema, resolvedTable, resolveErr := d.resolveSchemaTable(ctx, tx, opts.Table)
		if resolveErr != nil {
			return resolveErr
		}
		_, exists, incErr := d.targetIncarnationInTx(ctx, tx, resolvedSchema, resolvedTable)
		if incErr != nil {
			return incErr
		}
		if exists {
			return fmt.Errorf("destination table %q already exists before late CDC target claim", canonicalClaimTarget)
		}

		if err := d.claimCDCTargetInTx(ctx, tx, claimRef, canonicalClaimTarget, owner); err != nil {
			return err
		}

		createOpts := opts
		createOpts.DropFirst = false
		createSQL := buildRedshiftCreateTableSQLWithMode(targetSchema, targetTable, createOpts, false)
		if _, execErr := tx.Exec(ctx, createSQL); execErr != nil {
			return fmt.Errorf("failed to exclusively create Redshift CDC target %s: %w", opts.Table, execErr)
		}

		inc, created, incErr := d.targetIncarnationInTx(ctx, tx, targetSchema, targetTable)
		if incErr != nil {
			return incErr
		}
		if !created || inc == "" {
			return fmt.Errorf("created Redshift CDC target %s has no physical incarnation", opts.Table)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		incarnation = inc
		return nil
	})
	if err != nil {
		return "", err
	}
	return incarnation, nil
}

func (d *RedshiftDestination) TruncateCDCTable(ctx context.Context, table string) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	return d.TruncateTable(ctx, table)
}

func (d *RedshiftDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if expectedIncarnation == "" {
		return errors.New("redshift conditional CDC truncate requires an expected incarnation")
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return err
	}
	targetRef := quoteTable(schemaName, tableName)

	return d.withTx(ctx, func(tx redshiftTx) error {
		inc, exists, err := d.targetIncarnationInTx(ctx, tx, schemaName, tableName)
		if err != nil {
			return err
		}
		if !exists || inc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed", table)
		}
		if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
			return fmt.Errorf("failed to lock CDC target %s: %w", table, err)
		}
		inc, exists, err = d.targetIncarnationInTx(ctx, tx, schemaName, tableName)
		if err != nil {
			return err
		}
		if !exists || inc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed", table)
		}
		// DELETE FROM keeps the wipe inside the locked transaction without TRUNCATE's implicit commit.
		if _, err := tx.Exec(ctx, "DELETE FROM "+targetRef); err != nil {
			return fmt.Errorf("failed to clear CDC target %s: %w", table, err)
		}
		return source.ConnectorLeaseLoss(ctx)
	})
}

func (d *RedshiftDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	db, err := d.requireDB()
	if err != nil {
		return "", "", err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return "", "", err
	}
	targetSchema, _, err := d.resolveSchemaTable(ctx, db, targetTable)
	if err != nil {
		return "", "", err
	}
	stagingSchema, stagingName, err := d.resolveSchemaTable(ctx, db, stagingTable)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(targetSchema, stagingSchema) {
		return "", "", fmt.Errorf("redshift CDC conditional swap requires staging table %q to share schema %q with target %q", stagingTable, targetSchema, targetTable)
	}
	stagingIncarnation, exists, err := d.targetIncarnationInTx(ctx, db, stagingSchema, stagingName)
	if err != nil {
		return "", "", err
	}
	if !exists || stagingIncarnation == "" {
		return "", "", fmt.Errorf("redshift CDC staging table %q has no durable physical incarnation", stagingTable)
	}
	return stagingIncarnation, stagingIncarnation, nil
}

func (d *RedshiftDestination) lockAndValidateCDCIncarnation(ctx context.Context, tx redshiftTx, table, expectedIncarnation string) (string, string, string, error) {
	schemaName, tableName, err := d.resolveSchemaTable(ctx, tx, table)
	if err != nil {
		return "", "", "", err
	}
	targetRef := quoteTable(schemaName, tableName)
	if expectedIncarnation != "" {
		inc, exists, incErr := d.targetIncarnationInTx(ctx, tx, schemaName, tableName)
		if incErr != nil {
			return "", "", "", incErr
		}
		if !exists || inc != expectedIncarnation {
			return "", "", "", fmt.Errorf("redshift CDC target %q physical incarnation changed", table)
		}
	}
	if _, err := tx.Exec(ctx, "LOCK "+targetRef); err != nil {
		return "", "", "", fmt.Errorf("failed to lock CDC target %s: %w", table, err)
	}
	if expectedIncarnation != "" {
		inc, exists, incErr := d.targetIncarnationInTx(ctx, tx, schemaName, tableName)
		if incErr != nil {
			return "", "", "", incErr
		}
		if !exists || inc != expectedIncarnation {
			return "", "", "", fmt.Errorf("redshift CDC target %q physical incarnation changed", table)
		}
	}
	return schemaName, tableName, targetRef, nil
}

type redshiftManagedCDCRunLease struct {
	d            *RedshiftDestination
	leaseRef     string
	connectorID  string
	token        string
	ttlSeconds   int
	ttl          time.Duration
	overdueAfter time.Duration
	renewEvery   time.Duration

	done        chan struct{}
	stop        chan struct{}
	wg          sync.WaitGroup
	mu          sync.Mutex
	lastRenewal time.Time
	releasing   bool
	err         error
	closed      bool

	releaseOnce sync.Once
	releaseErr  error
}

func leaseElapsed(now, since time.Time) time.Duration {
	mono := now.Sub(since)
	wall := time.Duration(now.UnixNano() - since.UnixNano())
	return max(mono, wall)
}

func (d *RedshiftDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	db, err := d.requireDB()
	if err != nil {
		return nil, err
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate Redshift CDC lease token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	ttl := d.leaseTTL
	if ttl <= 0 {
		ttl = defaultRedshiftLeaseTTL
	}
	overdueAfter := max(time.Millisecond, ttl/2)
	renewEvery := d.leaseRenewInterval
	if renewEvery <= 0 || renewEvery >= overdueAfter {
		renewEvery = max(time.Millisecond, ttl/6)
	}
	ttlSeconds := max(1, int(math.Ceil(ttl.Seconds())))

	if err := ensureSchemaExists(ctx, db, defaultManagedStagingSchema); err != nil {
		return nil, err
	}
	leaseRef := quoteTable(defaultManagedStagingSchema, cdcRunLeasesTableName)
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			"connector_id" VARCHAR(65535) NOT NULL,
			"lease_token" VARCHAR(128) NOT NULL,
			"acquired_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL,
			"expires_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL
		)
	`, leaseRef)
	if _, err := db.Exec(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("failed to prepare Redshift CDC run lease table: %w", err)
	}

	var acquiredAt time.Time
	err = d.withTx(ctx, func(tx redshiftTx) error {
		if _, execErr := tx.Exec(ctx, "LOCK "+leaseRef); execErr != nil {
			return fmt.Errorf("failed to lock Redshift CDC run lease table: %w", execErr)
		}
		var existingToken string
		var active bool
		scanErr := tx.QueryRow(ctx, fmt.Sprintf(
			`SELECT "lease_token", ("expires_at" > CURRENT_TIMESTAMP) FROM %s WHERE "connector_id" = $1 LIMIT 1`,
			leaseRef,
		), connectorID).Scan(&existingToken, &active)
		if scanErr == nil && active {
			return fmt.Errorf("redshift managed CDC connector %q is already locked by another active run", connectorID)
		}
		if scanErr != nil && !errors.Is(scanErr, pgx.ErrNoRows) {
			return fmt.Errorf("failed to inspect Redshift CDC run lease for %q: %w", connectorID, scanErr)
		}

		if _, execErr := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE "connector_id" = $1`, leaseRef), connectorID); execErr != nil {
			return fmt.Errorf("failed to clear expired Redshift CDC run lease for %q: %w", connectorID, execErr)
		}
		acquiredAt = d.now()
		insertSQL := fmt.Sprintf(
			`INSERT INTO %s ("connector_id", "lease_token", "acquired_at", "expires_at") VALUES ($1, $2, CURRENT_TIMESTAMP, DATEADD(second, %d, CURRENT_TIMESTAMP))`,
			leaseRef, ttlSeconds,
		)
		if _, execErr := tx.Exec(ctx, insertSQL, connectorID, token); execErr != nil {
			return fmt.Errorf("failed to acquire Redshift CDC run lease for %q: %w", connectorID, execErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	lease := &redshiftManagedCDCRunLease{
		d:            d,
		leaseRef:     leaseRef,
		connectorID:  connectorID,
		token:        token,
		ttlSeconds:   ttlSeconds,
		ttl:          ttl,
		overdueAfter: overdueAfter,
		renewEvery:   renewEvery,
		lastRenewal:  acquiredAt,
		done:         make(chan struct{}),
		stop:         make(chan struct{}),
	}
	lease.wg.Add(1)
	go lease.heartbeatLoop()
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		_ = lease.Release()
		return nil, err
	}
	return lease, nil
}

func (l *redshiftManagedCDCRunLease) heartbeatLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.renewEvery)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			if l.checkOverdue() {
				return
			}
			renewCtx, cancel := context.WithTimeout(context.Background(), l.renewEvery)
			owned, renewedAt, err := l.renew(renewCtx)
			cancel()
			if err == nil && owned {
				if !l.recordRenewal(renewedAt) {
					return
				}
				continue
			}
			if err == nil && !owned {
				l.fail(errors.Join(source.ErrConnectorLeaseLost, errors.New("redshift CDC run lease was taken over or expired")))
				return
			}
			if l.failIfOverdue(err) {
				return
			}
		}
	}
}

func (l *redshiftManagedCDCRunLease) renew(ctx context.Context) (bool, time.Time, error) {
	var rowsAffected int64
	var renewedAt time.Time
	err := l.d.withTx(ctx, func(tx redshiftTx) error {
		if _, err := tx.Exec(ctx, "LOCK "+l.leaseRef); err != nil {
			return err
		}
		renewedAt = l.d.now()
		updateSQL := fmt.Sprintf(
			`UPDATE %s SET "expires_at" = DATEADD(second, %d, CURRENT_TIMESTAMP) WHERE "connector_id" = $1 AND "lease_token" = $2 AND "expires_at" > CURRENT_TIMESTAMP`,
			l.leaseRef, l.ttlSeconds,
		)
		tag, err := tx.Exec(ctx, updateSQL, l.connectorID, l.token)
		if err != nil {
			return err
		}
		rowsAffected = tag.RowsAffected()
		return nil
	})
	if err != nil {
		return false, time.Time{}, err
	}
	return rowsAffected > 0, renewedAt, nil
}

func (l *redshiftManagedCDCRunLease) failLocked(cause error) {
	if l.closed {
		return
	}
	l.err = cause
	l.closed = true
	close(l.done)
}

func (l *redshiftManagedCDCRunLease) fail(cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failLocked(cause)
}

func (l *redshiftManagedCDCRunLease) checkOverdue() bool {
	now := l.d.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return true
	}
	if l.releasing {
		return false
	}
	if leaseElapsed(now, l.lastRenewal) >= l.overdueAfter {
		l.failLocked(errors.Join(source.ErrConnectorLeaseLost, errors.New("redshift CDC run lease renewal is overdue")))
		return true
	}
	return false
}

func (l *redshiftManagedCDCRunLease) recordRenewal(renewedAt time.Time) bool {
	now := l.d.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.releasing {
		return false
	}
	l.lastRenewal = renewedAt
	if leaseElapsed(now, l.lastRenewal) >= l.overdueAfter {
		l.failLocked(errors.Join(source.ErrConnectorLeaseLost, errors.New("redshift CDC run lease renewal is overdue")))
		return false
	}
	return true
}

func (l *redshiftManagedCDCRunLease) failIfOverdue(cause error) bool {
	now := l.d.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.releasing {
		return true
	}
	if leaseElapsed(now, l.lastRenewal) >= l.overdueAfter {
		l.failLocked(errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("redshift CDC run lease renewal failed: %w", cause)))
		return true
	}
	return false
}

// Done checks whether lease renewal is overdue before returning the channel so
// ConnectorLeaseLoss rejects a process resuming from a pause before its
// background heartbeat goroutine gets scheduled.
func (l *redshiftManagedCDCRunLease) Done() <-chan struct{} {
	l.checkOverdue()
	return l.done
}

func (l *redshiftManagedCDCRunLease) Err() error {
	l.checkOverdue()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *redshiftManagedCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.mu.Lock()
		l.releasing = true
		l.mu.Unlock()
		close(l.stop)
		l.wg.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var rowsAffected int64
		err := l.d.withTx(ctx, func(tx redshiftTx) error {
			if _, err := tx.Exec(ctx, "LOCK "+l.leaseRef); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, fmt.Sprintf(
				`DELETE FROM %s WHERE "connector_id" = $1 AND "lease_token" = $2 AND "expires_at" > CURRENT_TIMESTAMP`,
				l.leaseRef,
			), l.connectorID, l.token)
			if err != nil {
				return err
			}
			rowsAffected = tag.RowsAffected()
			return nil
		})
		if err != nil {
			l.releaseErr = fmt.Errorf("failed to release Redshift CDC run lease: %w", err)
			return
		}
		if rowsAffected == 0 {
			l.releaseErr = errors.New("redshift CDC run lease was not owned during release")
		}
	})
	return l.releaseErr
}
