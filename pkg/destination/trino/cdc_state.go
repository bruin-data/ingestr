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
	_ destination.Destination                           = (*TrinoDestination)(nil)
	_ destination.CDCMergeAware                         = (*TrinoDestination)(nil)
	_ destination.CDCHardDeleteCapable                  = (*TrinoDestination)(nil)
	_ destination.CDCUnchangedColsAware                 = (*TrinoDestination)(nil)
	_ destination.CDCResumeProvider                     = (*TrinoDestination)(nil)
	_ destination.CDCStateTablePreparer                 = (*TrinoDestination)(nil)
	_ destination.CDCStatePositionMigrator              = (*TrinoDestination)(nil)
	_ destination.CDCStateReader                        = (*TrinoDestination)(nil)
	_ destination.CDCStateFenceReader                   = (*TrinoDestination)(nil)
	_ destination.CDCStatePruner                        = (*TrinoDestination)(nil)
	_ destination.CDCStatePruneBatchSizer               = (*TrinoDestination)(nil)
	_ destination.CDCTargetClaimer                      = (*TrinoDestination)(nil)
	_ destination.CDCTargetIdentityProvider             = (*TrinoDestination)(nil)
	_ destination.CDCTargetIncarnationProvider          = (*TrinoDestination)(nil)
	_ destination.CDCTargetIncarnationInitializer       = (*TrinoDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer            = (*TrinoDestination)(nil)
	_ destination.CDCConditionalTruncater               = (*TrinoDestination)(nil)
	_ destination.CDCConditionalMergeCapable            = (*TrinoDestination)(nil)
	_ destination.CDCConditionalSwapCapable             = (*TrinoDestination)(nil)
	_ destination.CDCConditionalSwapPlanner             = (*TrinoDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger             = (*TrinoDestination)(nil)
	_ destination.ManagedCDCRunLeaser                   = (*TrinoDestination)(nil)
	_ destination.ManagedCDCStateValidator              = (*TrinoDestination)(nil)
	_ destination.ManagedCDCStateCatalogProvider        = (*TrinoDestination)(nil)
	_ destination.SerializedCDCRunsRequired             = (*TrinoDestination)(nil)
	_ destination.TruncateCapable                       = (*TrinoDestination)(nil)
	_ destination.CDCTruncateCapable                    = (*TrinoDestination)(nil)
	_ destination.StagingTableLister                    = (*TrinoDestination)(nil)
	_ destination.SchemaEvolutionSourceColumnNormalizer = (*TrinoDestination)(nil)
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
	tableComment string
	lsnComment   string
	hasLSNColumn bool
}

func (m trinoTargetMeta) effectiveMarker() string {
	if marker := trinoIncarnationMarker(m.tableComment); marker != "" {
		return marker
	}
	if m.hasLSNColumn {
		return trinoIncarnationMarker(m.lsnComment)
	}
	return ""
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
		`SELECT t.table_catalog, t.table_schema, t.table_name, tc.comment
		FROM %s.information_schema.tables AS t
		LEFT JOIN system.metadata.table_comments AS tc
		  ON tc.catalog_name = t.table_catalog AND tc.schema_name = t.table_schema AND tc.table_name = t.table_name
		WHERE lower(t.table_schema) = lower(%s) AND lower(t.table_name) = lower(%s) AND t.table_type = 'BASE TABLE'`,
		quoteIdentifier(catalog),
		escapeString(schemaName),
		escapeString(tableName),
	)
	var actualCatalog, actualSchema, actualTable string
	var tableComment sql.NullString
	err = d.db.QueryRowContext(ctx, existsSQL).Scan(&actualCatalog, &actualSchema, &actualTable, &tableComment)
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
		catalog:      actualCatalog,
		schemaName:   actualSchema,
		tableName:    actualTable,
		fqn:          fqn,
		tableComment: tableComment.String,
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
	marker := meta.effectiveMarker()
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
	marker := meta.effectiveMarker()
	needTableComment := trinoIncarnationMarker(meta.tableComment) == ""
	needColumnComment := false
	if marker == "" {
		marker, err = generateTrinoIncarnationMarker()
		if err != nil {
			return "", false, err
		}
		needTableComment = true
		needColumnComment = true
	}
	commentLiteral := escapeString(trinoIncarnationCommentPrefix + marker)
	if needTableComment {
		tableQuery := fmt.Sprintf("COMMENT ON TABLE %s IS %s", meta.fqn, commentLiteral)
		if _, err := d.db.ExecContext(ctx, tableQuery); err != nil {
			config.LogFailedQuery(tableQuery, err)
			return "", false, fmt.Errorf("establish Trino CDC target incarnation for %s: %w", table, err)
		}
	}
	if needColumnComment {
		colQuery := fmt.Sprintf(
			"COMMENT ON COLUMN %s.%s IS %s",
			meta.fqn,
			quoteIdentifier(destination.CDCLSNColumn),
			commentLiteral,
		)
		if _, err := d.db.ExecContext(ctx, colQuery); err != nil {
			config.LogFailedQuery(colQuery, err)
			return "", false, fmt.Errorf("establish Trino CDC target incarnation for %s: %w", table, err)
		}
	}
	return trinoTableIncarnation(meta.catalog, meta.schemaName, meta.tableName, marker), true, nil
}

func (d *TrinoDestination) ensureCheckedCDCIncarnation(ctx context.Context, table, expected string) (trinoTargetMeta, string, error) {
	meta, exists, err := d.trinoTargetMetadata(ctx, table)
	if err != nil {
		return trinoTargetMeta{}, "", err
	}
	marker := meta.effectiveMarker()
	if !exists || !meta.hasLSNColumn || marker == "" || trinoTableIncarnation(meta.catalog, meta.schemaName, meta.tableName, marker) != expected {
		return trinoTargetMeta{}, "", fmt.Errorf("trino CDC target %q physical incarnation changed", table)
	}
	if trinoIncarnationMarker(meta.tableComment) == "" {
		tableQuery := fmt.Sprintf("COMMENT ON TABLE %s IS %s", meta.fqn, escapeString(trinoIncarnationCommentPrefix+marker))
		if _, err := d.db.ExecContext(ctx, tableQuery); err != nil {
			config.LogFailedQuery(tableQuery, err)
			return trinoTargetMeta{}, "", fmt.Errorf("persist Trino CDC target incarnation comment for %s: %w", table, err)
		}
		meta.tableComment = trinoIncarnationCommentPrefix + marker
		if err := d.checkCDCIncarnation(ctx, table, expected); err != nil {
			return trinoTargetMeta{}, "", err
		}
	}
	return meta, marker, nil
}

func trinoIncarnationExistsPredicate(catalog, schemaName, tableName, marker string) string {
	return fmt.Sprintf(
		`EXISTS (SELECT 1 FROM system.metadata.table_comments WHERE catalog_name = %s AND schema_name = %s AND table_name = %s AND comment = %s)`,
		escapeString(catalog),
		escapeString(schemaName),
		escapeString(tableName),
		escapeString(trinoIncarnationCommentPrefix+marker),
	)
}

func markerFromTableIncarnation(catalog, schemaName, tableName, incarnation string) (string, bool) {
	prefix := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName)) + "64:"
	encodedMarker, ok := strings.CutPrefix(incarnation, prefix)
	if !ok || len(encodedMarker) != 64 {
		return "", false
	}
	raw, err := hex.DecodeString(encodedMarker)
	if err != nil {
		return "", false
	}
	marker := string(raw)
	if trinoIncarnationMarker(trinoIncarnationCommentPrefix+marker) == "" {
		return "", false
	}
	return marker, trinoTableIncarnation(catalog, schemaName, tableName, marker) == incarnation
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
	targetSchema := destination.DestinationTableSchema(opts.Schema)
	createSQL := buildCreateTableSQLWithOptions(
		catalog,
		schemaName,
		tableName,
		targetSchema.Columns,
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

type trinoExecFunc func(ctx context.Context, sqlStr string, args ...any) error

func (d *TrinoDestination) defaultExec(ctx context.Context, sqlStr string, args ...any) error {
	_, err := d.db.ExecContext(ctx, sqlStr, args...)
	return err
}

func (d *TrinoDestination) TruncateTable(ctx context.Context, table string) error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	return d.truncateTableWithExec(ctx, table, d.defaultExec, false)
}

func (d *TrinoDestination) truncateTableWithExec(ctx context.Context, table string, execFn trinoExecFunc, useDelete bool) error {
	catalog, schemaName, tableName, err := d.resolveTableName(table)
	if err != nil {
		return err
	}
	verb := "TRUNCATE TABLE "
	if useDelete {
		verb = "DELETE FROM "
	}
	query := verb + qualifyTrinoTable(catalog, schemaName, tableName)
	if err := execFn(ctx, query); err != nil {
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
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	releaseLocks, verifyLocks, err := d.acquireCDCTargetLocks(ctx, table)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	meta, marker, err := d.ensureCheckedCDCIncarnation(ctx, table, expectedIncarnation)
	if err != nil {
		return err
	}
	incarnationPred := trinoIncarnationExistsPredicate(meta.catalog, meta.schemaName, meta.tableName, marker)
	if err := d.truncateCDCTableIfIncarnationLocked(ctx, table, meta.fqn, expectedIncarnation, incarnationPred, d.defaultExec, verifyLocks); err != nil {
		return err
	}
	return d.checkCDCIncarnation(ctx, table, expectedIncarnation)
}

func (d *TrinoDestination) truncateCDCTableIfIncarnationLocked(
	ctx context.Context,
	table, targetFQN, expectedIncarnation, incarnationPred string,
	execFn trinoExecFunc,
	verifyLocks func() error,
) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if verifyLocks != nil {
		if err := verifyLocks(); err != nil {
			return err
		}
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", targetFQN, incarnationPred)
	if err := execFn(ctx, query); err != nil {
		config.LogFailedQuery(query, err)
		return fmt.Errorf("truncate Trino table %s: %w", table, err)
	}
	if verifyLocks != nil {
		if err := verifyLocks(); err != nil {
			return err
		}
	}
	return source.ConnectorLeaseLoss(ctx)
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
	marker := meta.effectiveMarker()
	if marker == "" {
		return "", "", fmt.Errorf("trino CDC staging table %q is missing an incarnation marker", stagingTable)
	}
	return stagingIncarnation, trinoTableIncarnation(targetCatalog, targetSchema, targetName, marker), nil
}

type preparedAtomicMerge struct {
	merge           destination.CDCAtomicTableMerge
	canonicalTarget string
	targetFQN       string
	incarnationPred string
}

type trinoTargetSnapshot struct {
	canonicalTarget     string
	table               string
	catalog             string
	schemaName          string
	tableName           string
	fqn                 string
	expectedIncarnation string
	incarnationPred     string
	hasSnapshot         bool
	snapshotID          int64
}

func (d *TrinoDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if len(merges) == 0 {
		return nil
	}
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	targetTables := make([]string, 0, len(merges))
	for _, merge := range merges {
		if len(merge.Options.PrimaryKeys) == 0 {
			return fmt.Errorf("trino atomic CDC merge for %q requires primary keys", merge.Options.TargetTable)
		}
		targetTables = append(targetTables, merge.Options.TargetTable)
	}

	releaseLocks, verifyLocks, err := d.acquireCDCTargetLocks(ctx, targetTables...)
	if err != nil {
		return err
	}
	defer func() { _ = releaseLocks() }()

	prepared := make([]preparedAtomicMerge, 0, len(merges))
	snapshotsByKey := make(map[string]*trinoTargetSnapshot, len(merges))
	orderedSnapshots := make([]*trinoTargetSnapshot, 0, len(merges))

	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := verifyLocks(); err != nil {
			return err
		}
		catalog, schemaName, tableName, err := d.resolveTableName(merge.Options.TargetTable)
		if err != nil {
			return err
		}
		var guardPred string
		if merge.Options.CDCExpectedIncarnation != "" {
			meta, marker, err := d.ensureCheckedCDCIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation)
			if err != nil {
				return err
			}
			catalog, schemaName, tableName = meta.catalog, meta.schemaName, meta.tableName
			guardPred = trinoIncarnationExistsPredicate(catalog, schemaName, tableName, marker)
		}
		canonicalKey := destination.CDCTargetKey(strings.ToLower(catalog), strings.ToLower(schemaName), strings.ToLower(tableName))
		fqn := qualifyTrinoTable(catalog, schemaName, tableName)
		prepared = append(prepared, preparedAtomicMerge{
			merge:           merge,
			canonicalTarget: canonicalKey,
			targetFQN:       fqn,
			incarnationPred: guardPred,
		})
		if existing, ok := snapshotsByKey[canonicalKey]; ok {
			if existing.expectedIncarnation == "" && merge.Options.CDCExpectedIncarnation != "" {
				existing.expectedIncarnation = merge.Options.CDCExpectedIncarnation
				existing.incarnationPred = guardPred
			}
			continue
		}
		snap := &trinoTargetSnapshot{
			canonicalTarget:     canonicalKey,
			table:               merge.Options.TargetTable,
			catalog:             catalog,
			schemaName:          schemaName,
			tableName:           tableName,
			fqn:                 fqn,
			expectedIncarnation: merge.Options.CDCExpectedIncarnation,
			incarnationPred:     guardPred,
		}
		snapshotsByKey[canonicalKey] = snap
		orderedSnapshots = append(orderedSnapshots, snap)
	}

	needsRollbackCapability := len(prepared) > 1 || prepared[0].merge.Truncate
	hasIcebergSnapshots, err := d.captureTrinoTargetSnapshots(ctx, orderedSnapshots)
	if err != nil {
		return err
	}
	if !hasIcebergSnapshots {
		if !needsRollbackCapability {
			return d.executePreparedAtomicMergeStep(ctx, prepared[0], d.defaultExec, verifyLocks, true)
		}
		return d.executeAtomicMergesInTransaction(ctx, prepared, verifyLocks)
	}

	mutatedOrder := make([]*trinoTargetSnapshot, 0, len(orderedSnapshots))
	mutatedSeen := make(map[string]struct{}, len(orderedSnapshots))
	recordMutated := func(key string) {
		if _, ok := mutatedSeen[key]; ok {
			return
		}
		mutatedSeen[key] = struct{}{}
		mutatedOrder = append(mutatedOrder, snapshotsByKey[key])
	}

	var batchErr error
	for _, item := range prepared {
		err := d.executePreparedAtomicMergeStepWithMutationHook(
			ctx,
			item,
			d.defaultExec,
			verifyLocks,
			true,
			func() { recordMutated(item.canonicalTarget) },
		)
		if err != nil {
			batchErr = err
			break
		}
	}
	if batchErr == nil {
		if err := verifyLocks(); err != nil {
			batchErr = err
		} else if err := source.ConnectorLeaseLoss(ctx); err != nil {
			batchErr = err
		}
	}
	if batchErr != nil {
		rollbackErr := d.rollbackTrinoTargetSnapshots(ctx, mutatedOrder)
		return errors.Join(batchErr, rollbackErr)
	}
	return nil
}

func (d *TrinoDestination) captureTrinoTargetSnapshots(ctx context.Context, targets []*trinoTargetSnapshot) (bool, error) {
	for i, target := range targets {
		refsFQN := qualifyTrinoTable(target.catalog, target.schemaName, target.tableName+"$refs")
		query := fmt.Sprintf("SELECT snapshot_id FROM %s WHERE name = 'main'", refsFQN)
		var snapshotID sql.NullInt64
		err := d.db.QueryRowContext(ctx, query).Scan(&snapshotID)
		if errors.Is(err, sql.ErrNoRows) {
			target.hasSnapshot = false
			continue
		}
		if err != nil {
			if i == 0 && isTrinoUnsupportedSnapshots(err) {
				return false, nil
			}
			config.LogFailedQuery(query, err)
			return false, fmt.Errorf("read Trino snapshot ref for %s: %w", target.table, err)
		}
		if snapshotID.Valid {
			target.hasSnapshot = true
			target.snapshotID = snapshotID.Int64
		}
	}
	return true, nil
}

func isTrinoUnsupportedSnapshots(err error) bool {
	if isTrinoMissingObject(err) {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not supported") || strings.Contains(msg, "unsupported")
}

func (d *TrinoDestination) rollbackTrinoTargetSnapshots(ctx context.Context, mutated []*trinoTargetSnapshot) error {
	if len(mutated) == 0 {
		return nil
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()

	var errs []error
	for i := len(mutated) - 1; i >= 0; i-- {
		if err := d.rollbackSingleTrinoTargetSnapshot(rollbackCtx, mutated[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (d *TrinoDestination) rollbackSingleTrinoTargetSnapshot(ctx context.Context, snap *trinoTargetSnapshot) error {
	if snap.expectedIncarnation != "" {
		if err := d.checkCDCIncarnation(ctx, snap.table, snap.expectedIncarnation); err != nil {
			return nil
		}
	}
	if !snap.hasSnapshot {
		query := "DELETE FROM " + snap.fqn
		if snap.incarnationPred != "" {
			query += " WHERE " + snap.incarnationPred
		}
		if _, err := d.db.ExecContext(ctx, query); err != nil {
			config.LogFailedQuery(query, err)
			return fmt.Errorf("rollback empty Trino CDC target %s: %w", snap.table, err)
		}
		return nil
	}

	rollbackSQL := fmt.Sprintf("ALTER TABLE %s EXECUTE rollback_to_snapshot(%d)", snap.fqn, snap.snapshotID)
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		_, err := d.db.ExecContext(ctx, rollbackSQL)
		if err == nil {
			return nil
		}
		if currentID, ok := d.currentTrinoSnapshotID(ctx, snap); ok && currentID == snap.snapshotID {
			return nil
		}
		if !isTrinoCommitConflict(err) || attempt == maxAttempts-1 {
			config.LogFailedQuery(rollbackSQL, err)
			return fmt.Errorf("rollback Trino CDC target %s to snapshot %d: %w", snap.table, snap.snapshotID, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return nil
}

func (d *TrinoDestination) currentTrinoSnapshotID(ctx context.Context, snap *trinoTargetSnapshot) (int64, bool) {
	refsFQN := qualifyTrinoTable(snap.catalog, snap.schemaName, snap.tableName+"$refs")
	var id sql.NullInt64
	if err := d.db.QueryRowContext(ctx, fmt.Sprintf("SELECT snapshot_id FROM %s WHERE name = 'main'", refsFQN)).Scan(&id); err != nil || !id.Valid {
		return 0, false
	}
	return id.Int64, true
}

func (d *TrinoDestination) executeAtomicMergesInTransaction(
	ctx context.Context,
	prepared []preparedAtomicMerge,
	verifyLocks func() error,
) error {
	tx, err := d.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("begin Trino atomic CDC merge transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	for _, item := range prepared {
		if err := d.executePreparedAtomicMergeStep(ctx, item, tx.Exec, verifyLocks, false); err != nil {
			return err
		}
	}
	for _, item := range prepared {
		if item.merge.Options.CDCExpectedIncarnation != "" {
			if err := d.checkCDCIncarnation(ctx, item.merge.Options.TargetTable, item.merge.Options.CDCExpectedIncarnation); err != nil {
				return err
			}
		}
	}
	if err := verifyLocks(); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Trino atomic CDC merge transaction: %w", err)
	}
	committed = true
	return nil
}

func (d *TrinoDestination) executePreparedAtomicMergeStep(
	ctx context.Context,
	item preparedAtomicMerge,
	execFn trinoExecFunc,
	verifyLocks func() error,
	verifyIncarnationAfter bool,
) error {
	return d.executePreparedAtomicMergeStepWithMutationHook(ctx, item, execFn, verifyLocks, verifyIncarnationAfter, nil)
}

func (d *TrinoDestination) executePreparedAtomicMergeStepWithMutationHook(
	ctx context.Context,
	item preparedAtomicMerge,
	execFn trinoExecFunc,
	verifyLocks func() error,
	verifyIncarnationAfter bool,
	onMutated func(),
) error {
	hookedExec := func(execCtx context.Context, sqlStr string, args ...any) error {
		err := execFn(execCtx, sqlStr, args...)
		if (err == nil || execCtx.Err() != nil) && onMutated != nil {
			onMutated()
		}
		return err
	}

	merge := item.merge
	if merge.Truncate {
		if merge.Options.CDCExpectedIncarnation != "" {
			if err := d.truncateCDCTableIfIncarnationLocked(
				ctx,
				merge.Options.TargetTable,
				item.targetFQN,
				merge.Options.CDCExpectedIncarnation,
				item.incarnationPred,
				hookedExec,
				verifyLocks,
			); err != nil {
				return err
			}
		} else if err := d.truncateTableWithExec(ctx, merge.Options.TargetTable, hookedExec, !verifyIncarnationAfter); err != nil {
			return err
		}
	}

	if destination.HasCDCDeletedColumn(merge.Options.Columns) {
		if err := d.mergeCDCLocked(ctx, merge.Options, item.incarnationPred, hookedExec, verifyLocks); err != nil {
			return err
		}
	} else {
		if err := d.mergeTableLocked(ctx, merge.Options, item.incarnationPred, hookedExec, verifyLocks); err != nil {
			return err
		}
	}

	if verifyIncarnationAfter && merge.Options.CDCExpectedIncarnation != "" {
		if err := d.checkCDCIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
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
