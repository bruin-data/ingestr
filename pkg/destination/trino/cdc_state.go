package trino

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

const (
	trinoIncarnationCommentPrefix = "__ingestr_cdc_incarnation="
	trinoCDCStatePruneBatchSize   = 1000
	trinoCDCDeleteChunkSize       = 500
	trinoMinWideVarcharLen        = 1_000_000
)

var (
	_ destination.Destination                     = (*TrinoDestination)(nil)
	_ destination.CDCMergeAware                   = (*TrinoDestination)(nil)
	_ destination.CDCHardDeleteCapable            = (*TrinoDestination)(nil)
	_ destination.CDCUnchangedColsAware           = (*TrinoDestination)(nil)
	_ destination.CDCResumeProvider               = (*TrinoDestination)(nil)
	_ destination.CDCStateTablePreparer           = (*TrinoDestination)(nil)
	_ destination.CDCStatePositionMigrator        = (*TrinoDestination)(nil)
	_ destination.CDCStateReader                  = (*TrinoDestination)(nil)
	_ destination.CDCStateFenceReader             = (*TrinoDestination)(nil)
	_ destination.CDCStatePruner                  = (*TrinoDestination)(nil)
	_ destination.CDCStatePruneBatchSizer         = (*TrinoDestination)(nil)
	_ destination.CDCTargetClaimer                = (*TrinoDestination)(nil)
	_ destination.CDCTargetIdentityProvider       = (*TrinoDestination)(nil)
	_ destination.CDCTargetIncarnationProvider    = (*TrinoDestination)(nil)
	_ destination.CDCTargetIncarnationInitializer = (*TrinoDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer      = (*TrinoDestination)(nil)
	_ destination.CDCConditionalTruncater         = (*TrinoDestination)(nil)
	_ destination.CDCConditionalMergeCapable      = (*TrinoDestination)(nil)
	_ destination.CDCConditionalSwapCapable       = (*TrinoDestination)(nil)
	_ destination.CDCConditionalSwapPlanner       = (*TrinoDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger       = (*TrinoDestination)(nil)
	_ destination.ManagedCDCRunLeaser             = (*TrinoDestination)(nil)
	_ destination.ManagedCDCStateValidator        = (*TrinoDestination)(nil)
	_ destination.ManagedCDCStateCatalogProvider  = (*TrinoDestination)(nil)
	_ destination.SerializedCDCRunsRequired       = (*TrinoDestination)(nil)
	_ destination.TruncateCapable                 = (*TrinoDestination)(nil)
	_ destination.CDCTruncateCapable              = (*TrinoDestination)(nil)
	_ destination.StagingTableLister              = (*TrinoDestination)(nil)
)

func (d *TrinoDestination) ManagedCDCStateCatalog() string {
	return d.catalog
}

func (d *TrinoDestination) ValidateManagedCDCState() error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if strings.TrimSpace(d.catalog) == "" {
		return errors.New("trino managed CDC requires a catalog")
	}
	return nil
}

func (d *TrinoDestination) RequiresSerializedCDCRuns() bool { return true }

func (d *TrinoDestination) CDCStatePruneBatchSize() int { return trinoCDCStatePruneBatchSize }

func (d *TrinoDestination) PrepareCDCStateTable(ctx context.Context, table string, tableSchema *schema.TableSchema) error {
	if err := tablename.Trino.CheckName(table); err != nil {
		return err
	}
	return d.PrepareTable(ctx, destination.PrepareOptions{
		Table:  table,
		Schema: tableSchema,
	})
}

func (d *TrinoDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(
		`SELECT data_type FROM %s.information_schema.columns
		WHERE lower(table_schema) = lower(%s) AND lower(table_name) = lower(%s) AND lower(column_name) = lower(%s)`,
		quoteIdentifier(catalog),
		escapeString(schemaName),
		escapeString(tableName),
		escapeString(destination.CDCLSNColumn),
	)
	var dataType string
	err = d.db.QueryRowContext(ctx, query).Scan(&dataType)
	if errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err) {
		return nil
	}
	if err != nil {
		config.LogFailedQuery(query, err)
		return fmt.Errorf("inspect Trino CDC state position column: %w", err)
	}
	norm := strings.ToLower(strings.TrimSpace(dataType))
	if norm == "varchar" {
		return nil
	}
	if base, params := splitTrinoTypeParams(norm); base == "varchar" && len(params) == 1 {
		if n, err := strconv.Atoi(params[0]); err == nil && n >= trinoMinWideVarcharLen {
			return nil
		}
		return fmt.Errorf("trino CDC state position column %s has bounded type %s; expected unbounded VARCHAR", table, dataType)
	}
	return fmt.Errorf("trino CDC state position column must be VARCHAR, found %s", dataType)
}

func (d *TrinoDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	if d.db == nil {
		return nil, errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		`SELECT DISTINCT %s, %s, %s, %s, %s, %s, %s FROM %s WHERE %s = ?`,
		quoteIdentifier("event_id"),
		quoteIdentifier("source_table"),
		quoteIdentifier("destination_table"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_status"),
		quoteIdentifier(destination.CDCLSNColumn),
		qualifyTrinoTable(catalog, schemaName, tableName),
		quoteIdentifier("connector_id"),
	)
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isTrinoMissingObject(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("read Trino CDC state: %w", err)
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
			return nil, fmt.Errorf("scan Trino CDC state row: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *TrinoDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	if d.db == nil {
		return destination.CDCStateFence{}, errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return destination.CDCStateFence{}, err
	}
	fqn := qualifyTrinoTable(catalog, schemaName, tableName)
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
		fqn,
		quoteIdentifier("connector_id"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("state_generation"),
		quoteIdentifier("state_generation"),
		fqn,
		quoteIdentifier("connector_id"),
		quoteIdentifier("state_kind"),
		quoteIdentifier("event_id"),
	)
	rows, err := d.db.QueryContext(ctx, query, connectorID, connectorID)
	if err != nil {
		if isTrinoMissingObject(err) {
			return destination.CDCStateFence{}, nil
		}
		config.LogFailedQuery(query, err)
		return destination.CDCStateFence{}, fmt.Errorf("read Trino CDC fence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, fmt.Errorf("scan Trino CDC fence row: %w", err)
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *TrinoDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return err
	}
	fqn := qualifyTrinoTable(catalog, schemaName, tableName)

	for start := 0; start < len(eventIDs); start += trinoCDCDeleteChunkSize {
		end := start + trinoCDCDeleteChunkSize
		if end > len(eventIDs) {
			end = len(eventIDs)
		}
		chunk := eventIDs[start:end]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, connectorID)
		placeholders := make([]string, len(chunk))
		for i, id := range chunk {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query := fmt.Sprintf(
			`DELETE FROM %s WHERE %s = ? AND %s IN (%s)`,
			fqn,
			quoteIdentifier("connector_id"),
			quoteIdentifier("event_id"),
			strings.Join(placeholders, ", "),
		)
		if _, err := d.db.ExecContext(ctx, query, args...); err != nil {
			if isTrinoMissingObject(err) {
				return nil
			}
			config.LogFailedQuery(query, err)
			return fmt.Errorf("delete Trino CDC state events: %w", err)
		}
	}
	return nil
}

func (d *TrinoDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	if d.db == nil {
		return "", errors.New("trino destination is not connected")
	}
	meta, exists, err := d.trinoTargetMetadata(ctx, table)
	if err != nil || !exists || !meta.hasLSNColumn {
		return "", err
	}
	query := fmt.Sprintf("SELECT MAX(%s) FROM %s", quoteIdentifier(destination.CDCLSNColumn), meta.fqn)
	var position sql.NullString
	if err := d.db.QueryRowContext(ctx, query).Scan(&position); err != nil {
		if isTrinoMissingObject(err) {
			return "", nil
		}
		config.LogFailedQuery(query, err)
		return "", fmt.Errorf("read Trino CDC cursor for %s: %w", table, err)
	}
	return position.String, nil
}

func (d *TrinoDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return "", err
	}
	return destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName)), nil
}

type trinoTargetMeta struct {
	catalog      string
	schemaName   string
	tableName    string
	fqn          string
	lsnComment   string
	hasLSNColumn bool
}

func (d *TrinoDestination) trinoTargetMetadata(ctx context.Context, table string) (trinoTargetMeta, bool, error) {
	if d.db == nil {
		return trinoTargetMeta{}, false, errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return trinoTargetMeta{}, false, err
	}
	existsSQL := fmt.Sprintf(
		`SELECT table_catalog, table_schema, table_name
		FROM %s.information_schema.tables
		WHERE lower(table_schema) = lower(%s) AND lower(table_name) = lower(%s) AND table_type = 'BASE TABLE'`,
		quoteIdentifier(catalog),
		escapeString(schemaName),
		escapeString(tableName),
	)
	var actualCatalog, actualSchema, actualTable string
	err = d.db.QueryRowContext(ctx, existsSQL).Scan(&actualCatalog, &actualSchema, &actualTable)
	if errors.Is(err, sql.ErrNoRows) || isTrinoMissingObject(err) {
		return trinoTargetMeta{}, false, nil
	}
	if err != nil {
		config.LogFailedQuery(existsSQL, err)
		return trinoTargetMeta{}, false, fmt.Errorf("read Trino CDC target metadata for %s: %w", table, err)
	}

	fqn := qualifyTrinoTable(actualCatalog, actualSchema, actualTable)
	rows, err := d.db.QueryContext(ctx, "SHOW COLUMNS FROM "+fqn)
	if err != nil {
		if isTrinoMissingObject(err) {
			return trinoTargetMeta{}, false, nil
		}
		return trinoTargetMeta{}, false, fmt.Errorf("inspect Trino CDC target columns for %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	meta := trinoTargetMeta{
		catalog:    actualCatalog,
		schemaName: actualSchema,
		tableName:  actualTable,
		fqn:        fqn,
	}
	for rows.Next() {
		var colName, colType, extra string
		var comment sql.NullString
		if err := rows.Scan(&colName, &colType, &extra, &comment); err != nil {
			return trinoTargetMeta{}, false, fmt.Errorf("scan Trino column metadata for %s: %w", table, err)
		}
		if strings.EqualFold(colName, destination.CDCLSNColumn) {
			meta.hasLSNColumn = true
			meta.lsnComment = comment.String
			break
		}
	}
	if err := rows.Err(); err != nil {
		return trinoTargetMeta{}, false, fmt.Errorf("read Trino column metadata for %s: %w", table, err)
	}
	return meta, true, nil
}

func (d *TrinoDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	meta, exists, err := d.trinoTargetMetadata(ctx, table)
	if err != nil || !exists {
		return "", exists, err
	}
	marker := trinoIncarnationMarker(meta.lsnComment)
	if !meta.hasLSNColumn || marker == "" {
		return "", true, nil
	}
	return trinoTableIncarnation(meta.catalog, meta.schemaName, meta.tableName, marker), true, nil
}

func (d *TrinoDestination) EnsureCDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	meta, exists, err := d.trinoTargetMetadata(ctx, table)
	if err != nil || !exists {
		return "", exists, err
	}
	if !meta.hasLSNColumn {
		return "", true, fmt.Errorf("trino CDC target %q is missing managed column %q", table, destination.CDCLSNColumn)
	}
	marker := trinoIncarnationMarker(meta.lsnComment)
	if marker == "" {
		marker, err = generateTrinoIncarnationMarker()
		if err != nil {
			return "", false, err
		}
		query := fmt.Sprintf(
			"COMMENT ON COLUMN %s.%s IS %s",
			meta.fqn,
			quoteIdentifier(destination.CDCLSNColumn),
			escapeString(trinoIncarnationCommentPrefix+marker),
		)
		if _, err := d.db.ExecContext(ctx, query); err != nil {
			config.LogFailedQuery(query, err)
			return "", false, fmt.Errorf("establish Trino CDC target incarnation for %s: %w", table, err)
		}
	}
	return trinoTableIncarnation(meta.catalog, meta.schemaName, meta.tableName, marker), true, nil
}

func (d *TrinoDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("trino CDC target %q physical incarnation changed", table)
	}
	return nil
}

func (d *TrinoDestination) claimMarkerTable(claimTable, target string) (catalog, schemaName, tableName string, err error) {
	catalog, schemaName, _, err = d.resolveTableName(claimTable)
	if err != nil {
		return "", "", "", err
	}
	return catalog, schemaName, "cdc_claim_" + destination.CDCTargetKeyDigest(target), nil
}

func (d *TrinoDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	owner, err := claim.OwnerID()
	if err != nil {
		return err
	}
	target, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return err
	}
	claimCatalog, claimSchema, claimName, err := d.resolveTableName(claimTable)
	if err != nil {
		return err
	}
	if err := d.ensureSchemaExists(ctx, claimCatalog, claimSchema); err != nil {
		return fmt.Errorf("ensure Trino CDC claim schema: %w", err)
	}
	markerCatalog, markerSchema, markerName, err := d.claimMarkerTable(claimTable, target)
	if err != nil {
		return err
	}
	markerFQN := qualifyTrinoTable(markerCatalog, markerSchema, markerName)

	createClaimSQL := fmt.Sprintf(
		"CREATE TABLE %s AS SELECT CAST(%s AS VARCHAR) AS owner_id",
		markerFQN,
		escapeString(owner),
	)
	const maxClaimAttempts = 5
	for attempt := 0; attempt < maxClaimAttempts; attempt++ {
		_, err := d.db.ExecContext(ctx, createClaimSQL)
		if err == nil || isTrinoAlreadyExists(err) {
			break
		}
		if !isTrinoCommitConflict(err) {
			config.LogFailedQuery(createClaimSQL, err)
			return fmt.Errorf("claim Trino CDC target %q: %w", claim.DestinationTable, err)
		}
		var racedOwner sql.NullString
		if readErr := d.db.QueryRowContext(ctx, "SELECT MAX(owner_id) FROM "+markerFQN).Scan(&racedOwner); readErr == nil && racedOwner.Valid && racedOwner.String != "" {
			break
		}
		if attempt == maxClaimAttempts-1 {
			config.LogFailedQuery(createClaimSQL, err)
			return fmt.Errorf("claim Trino CDC target %q: %w", claim.DestinationTable, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}

	var actual string
	readOwnerSQL := "SELECT MAX(owner_id) FROM " + markerFQN
	var ownerVal sql.NullString
	if err := d.db.QueryRowContext(ctx, readOwnerSQL).Scan(&ownerVal); err != nil {
		config.LogFailedQuery(readOwnerSQL, err)
		return fmt.Errorf("read Trino CDC target claim for %q: %w", claim.DestinationTable, err)
	}
	actual = ownerVal.String
	if actual == "" {
		return fmt.Errorf("trino CDC target claim %s has no owner", markerFQN)
	}
	if actual != owner {
		return fmt.Errorf("destination table %q is already claimed by CDC connector %q", claim.DestinationTable, actual)
	}

	claimFQN := qualifyTrinoTable(claimCatalog, claimSchema, claimName)
	digest := destination.CDCTargetKeyDigest(target)
	var existingCount int64
	checkSQL := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE %s = ? AND %s = ?",
		claimFQN,
		quoteIdentifier("destination_table"),
		quoteIdentifier("connector_id"),
	)
	if err := d.db.QueryRowContext(ctx, checkSQL, digest, owner).Scan(&existingCount); err != nil {
		if !isTrinoMissingObject(err) {
			config.LogFailedQuery(checkSQL, err)
			return fmt.Errorf("check Trino CDC claim log: %w", err)
		}
		return nil
	}
	if existingCount == 0 {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES (?, ?, CURRENT_TIMESTAMP)",
			claimFQN,
			quoteIdentifier("destination_table"),
			quoteIdentifier("connector_id"),
			quoteIdentifier("claimed_at"),
		)
		for attempt := 0; attempt < maxClaimAttempts; attempt++ {
			_, err := d.db.ExecContext(ctx, insertSQL, digest, owner)
			if err == nil {
				break
			}
			if !isTrinoCommitConflict(err) || attempt == maxClaimAttempts-1 {
				config.LogFailedQuery(insertSQL, err)
				return fmt.Errorf("record Trino CDC target claim: %w", err)
			}
			var recheckedCount int64
			if checkErr := d.db.QueryRowContext(ctx, checkSQL, digest, owner).Scan(&recheckedCount); checkErr == nil && recheckedCount > 0 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
			}
		}
	}
	return nil
}

func (d *TrinoDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
	if d.db == nil {
		return "", errors.New("trino destination is not connected")
	}
	if opts.Schema == nil {
		return "", errors.New("cannot create an empty managed CDC target without a schema")
	}
	if !slices.ContainsFunc(opts.Schema.Columns, func(col schema.Column) bool {
		return strings.EqualFold(col.Name, destination.CDCLSNColumn)
	}) {
		return "", fmt.Errorf("cannot create Trino CDC target %q without managed column %q", opts.Table, destination.CDCLSNColumn)
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

	markerCatalog, markerSchema, markerName, err := d.claimMarkerTable(claimTable, target)
	if err != nil {
		return "", err
	}
	markerFQN := qualifyTrinoTable(markerCatalog, markerSchema, markerName)
	var existingOwner sql.NullString
	claimExisted := d.db.QueryRowContext(ctx, "SELECT MAX(owner_id) FROM "+markerFQN).Scan(&existingOwner) == nil && existingOwner.Valid

	if err := d.ClaimCDCTarget(ctx, claimTable, claim); err != nil {
		return "", err
	}

	catalog, schemaName, tableName, err := d.resolveTableName(opts.Table)
	if err != nil {
		return "", err
	}
	if err := d.ensureSchemaExists(ctx, catalog, schemaName); err != nil {
		return "", fmt.Errorf("failed to ensure schema exists: %w", err)
	}
	marker, err := generateTrinoIncarnationMarker()
	if err != nil {
		return "", err
	}
	createSQL := buildCreateTableSQLWithOptions(
		catalog,
		schemaName,
		tableName,
		opts.Schema.Columns,
		d.jsonType,
		false,
		trinoIncarnationCommentPrefix+marker,
	)
	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		if !claimExisted {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			owner, _ := claim.OwnerID()
			claimCatalog, claimSchema, claimName, resolveErr := d.resolveTableName(claimTable)
			_, dropErr := d.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+markerFQN)
			var deleteErr error
			if resolveErr == nil {
				deleteSQL := fmt.Sprintf(
					"DELETE FROM %s WHERE %s = ? AND %s = ?",
					qualifyTrinoTable(claimCatalog, claimSchema, claimName),
					quoteIdentifier("destination_table"),
					quoteIdentifier("connector_id"),
				)
				_, deleteErr = d.db.ExecContext(cleanupCtx, deleteSQL, destination.CDCTargetKeyDigest(target), owner)
			}
			return "", errors.Join(fmt.Errorf("failed to create claimed Trino CDC target: %w", err), dropErr, deleteErr)
		}
		return "", fmt.Errorf("failed to create claimed Trino CDC target: %w", err)
	}

	incarnation, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists || incarnation == "" {
		return "", fmt.Errorf("claimed Trino CDC target %q has no provable physical incarnation", opts.Table)
	}
	return incarnation, nil
}

func (d *TrinoDestination) TruncateTable(ctx context.Context, table string) error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return err
	}
	query := "TRUNCATE TABLE " + qualifyTrinoTable(catalog, schemaName, tableName)
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		config.LogFailedQuery(query, err)
		return fmt.Errorf("truncate Trino table %s: %w", table, err)
	}
	return nil
}

func (d *TrinoDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *TrinoDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("trino conditional CDC truncate requires an expected incarnation")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return err
	}
	if err := d.TruncateCDCTable(ctx, table); err != nil {
		return err
	}
	return d.checkCDCIncarnation(ctx, table, expectedIncarnation)
}

func (d *TrinoDestination) SupportsCDCConditionalSwap() bool { return true }

func (d *TrinoDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetCatalog, targetSchema, targetName, err := d.resolveTableName(targetTable)
	if err != nil {
		return "", "", err
	}
	stagingCatalog, _, _, err := d.resolveTableName(stagingTable)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(stagingCatalog, targetCatalog) {
		return "", "", fmt.Errorf("trino CDC conditional swaps require staging (%s) and target (%s) in the same catalog", stagingCatalog, targetCatalog)
	}
	stagingIncarnation, exists, err := d.EnsureCDCTargetIncarnation(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	if !exists || stagingIncarnation == "" {
		return "", "", fmt.Errorf("trino CDC staging table %q has no durable physical incarnation", stagingTable)
	}
	meta, _, err := d.trinoTargetMetadata(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	marker := trinoIncarnationMarker(meta.lsnComment)
	if marker == "" {
		return "", "", fmt.Errorf("trino CDC staging table %q is missing an incarnation marker", stagingTable)
	}
	return stagingIncarnation, trinoTableIncarnation(targetCatalog, targetSchema, targetName, marker), nil
}

func (d *TrinoDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if merge.Truncate {
			if merge.Options.CDCExpectedIncarnation != "" {
				if err := d.TruncateCDCTableIfIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
					return err
				}
			} else if err := d.TruncateCDCTable(ctx, merge.Options.TargetTable); err != nil {
				return err
			}
		}
		if err := d.MergeTable(ctx, merge.Options); err != nil {
			return err
		}
	}
	return nil
}

func (d *TrinoDestination) ListStagingTables(ctx context.Context, schemaName, prefix string) ([]string, error) {
	if d.db == nil {
		return nil, errors.New("trino destination is not connected")
	}
	catalog, parsedSchema, _, err := d.resolveTableName(schemaName + ".x")
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		`SELECT table_name FROM %s.information_schema.tables
		WHERE lower(table_schema) = lower(%s) AND table_type = 'BASE TABLE'`,
		quoteIdentifier(catalog),
		escapeString(parsedSchema),
	)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		if isTrinoMissingObject(err) {
			return nil, nil
		}
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("list Trino staging tables in %s.%s: %w", catalog, parsedSchema, err)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan Trino staging table name: %w", err)
		}
		if strings.HasPrefix(name, prefix) {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}

func generateTrinoIncarnationMarker() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate Trino CDC incarnation marker: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func trinoIncarnationMarker(comment string) string {
	marker, found := strings.CutPrefix(strings.TrimSpace(comment), trinoIncarnationCommentPrefix)
	if !found || len(marker) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(marker); err != nil {
		return ""
	}
	return marker
}

func trinoTableIncarnation(catalog, schemaName, tableName, marker string) string {
	return destination.CDCTargetKey(
		strings.ToLower(catalog),
		strings.ToLower(schemaName),
		strings.ToLower(tableName),
		marker,
	)
}

func isTrinoMissingObject(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "table not found") ||
		strings.Contains(msg, "schema not found") ||
		strings.Contains(msg, "not found") && strings.Contains(msg, "table")
}

func isTrinoAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		(strings.Contains(msg, "new value to update existing key") && strings.Contains(msg, "has no content id"))
}

func isTrinoCommitConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ref hash is out of date") ||
		strings.Contains(msg, "cannot commit") ||
		strings.Contains(msg, "commitfailedexception") ||
		strings.Contains(msg, "iceberg_commit_error")
}
