package fabric

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	mssqldb "github.com/microsoft/go-mssqldb"
)

const (
	cdcLeaseSchema     = "_bruin_staging"
	emptyLeaseSentinel = "__ingestr_empty_lease__"
)

var (
	cdcLeaseTTL          = 30 * time.Second
	cdcLeaseHeartbeat    = 5 * time.Second
	cdcLeaseCheck        = time.Second
	cdcLeaseRetryDelay   = 2 * time.Second
	cdcLeaseAcquireGrace = 15 * time.Second
)

var (
	_ destination.CDCMergeAware                = (*FabricDestination)(nil)
	_ destination.CDCHardDeleteCapable         = (*FabricDestination)(nil)
	_ destination.CDCUnchangedColsAware        = (*FabricDestination)(nil)
	_ destination.CDCResumeProvider            = (*FabricDestination)(nil)
	_ destination.CDCStateReader               = (*FabricDestination)(nil)
	_ destination.CDCStateFenceReader          = (*FabricDestination)(nil)
	_ destination.CDCStatePruner               = (*FabricDestination)(nil)
	_ destination.CDCStatePositionMigrator     = (*FabricDestination)(nil)
	_ destination.CDCTargetClaimer             = (*FabricDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer   = (*FabricDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*FabricDestination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*FabricDestination)(nil)
	_ destination.TruncateCapable              = (*FabricDestination)(nil)
	_ destination.CDCTruncateCapable           = (*FabricDestination)(nil)
	_ destination.CDCConditionalTruncater      = (*FabricDestination)(nil)
	_ destination.CDCConditionalMergeCapable   = (*FabricDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger    = (*FabricDestination)(nil)
	_ destination.ManagedCDCRunLeaser          = (*FabricDestination)(nil)
	_ destination.CDCConditionalSwapCapable    = (*FabricDestination)(nil)
	_ destination.CDCConditionalSwapPlanner    = (*FabricDestination)(nil)
	_ destination.ReplaceStagingPolicyProvider = (*FabricDestination)(nil)
	_ destination.SerializedCDCRunsRequired    = (*FabricDestination)(nil)
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type fabricTargetIdentity struct {
	database string
	schema   string
	table    string
}

func (i fabricTargetIdentity) catalogPrefix(defaultDatabase string) string {
	if i.database != "" && !strings.EqualFold(i.database, defaultDatabase) {
		return quoteColumn(i.database) + "."
	}
	return ""
}

func (i fabricTargetIdentity) qualifiedInSchema(defaultDatabase, tableName string) string {
	return i.catalogPrefix(defaultDatabase) + quoteColumn(i.schema) + "." + quoteColumn(tableName)
}

func (d *FabricDestination) SupportsCDCMerge() bool            { return true }
func (d *FabricDestination) SupportsCDCHardDelete() bool       { return true }
func (d *FabricDestination) SupportsCDCUnchangedCols() bool    { return true }
func (d *FabricDestination) SupportsCDCConditionalMerge() bool { return true }
func (d *FabricDestination) SupportsCDCConditionalSwap() bool  { return true }
func (d *FabricDestination) RequiresSerializedCDCRuns() bool   { return true }

func (d *FabricDestination) ReplaceStagingPolicy() destination.ReplaceStagingPolicy {
	return destination.ReplaceStagingPolicy{
		DefaultPlacement:     destination.ReplaceStagingTargetSchema,
		DefaultTargetSchema:  "dbo",
		DefaultManagedSchema: cdcLeaseSchema,
	}
}

func (d *FabricDestination) resolveTargetIdentity(target string) (fabricTargetIdentity, error) {
	parts := tablename.Split(target)
	if len(parts) == 0 || len(parts) > 3 {
		return fabricTargetIdentity{}, fmt.Errorf("fabric table name must contain between one and three components, %q given", target)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return fabricTargetIdentity{}, fmt.Errorf("fabric table name contains an empty component, %q given", target)
		}
	}
	identity := fabricTargetIdentity{
		database: d.database,
		schema:   "dbo",
	}
	switch len(parts) {
	case 1:
		identity.table = parts[0]
	case 2:
		identity.schema, identity.table = parts[0], parts[1]
	case 3:
		identity.database, identity.schema, identity.table = parts[0], parts[1], parts[2]
	}
	return identity, nil
}

func (d *FabricDestination) validateTargetPrimaryKey(ctx context.Context, table string, expected []string) error {
	if len(expected) == 0 {
		return fmt.Errorf("CDC merge target %s requires at least one primary key", table)
	}
	identity, err := d.resolveTargetIdentity(table)
	if err != nil {
		return err
	}
	prefix := identity.catalogPrefix(d.database)

	query := fmt.Sprintf(`SELECT kcu.COLUMN_NAME
		FROM %sINFORMATION_SCHEMA.TABLE_CONSTRAINTS AS tc
		JOIN %sINFORMATION_SCHEMA.KEY_COLUMN_USAGE AS kcu
		  ON tc.CONSTRAINT_CATALOG = kcu.CONSTRAINT_CATALOG
		 AND tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
		 AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
		WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		  AND kcu.TABLE_SCHEMA = @p1
		  AND kcu.TABLE_NAME = @p2
		ORDER BY kcu.ORDINAL_POSITION`, prefix, prefix)

	rows, err := d.db.QueryContext(ctx, query, identity.schema, identity.table)
	if err != nil {
		config.LogFailedQuery(query, err)
		return fmt.Errorf("failed to inspect CDC merge target %s primary key: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var actual []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return fmt.Errorf("failed to inspect CDC merge target %s primary key: %w", table, err)
		}
		actual = append(actual, col)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to inspect CDC merge target %s primary key: %w", table, err)
	}

	matched := matchedFabricIdentifiers(actual, expected)
	if len(actual) != len(expected) || len(matched) != len(expected) {
		return fmt.Errorf("CDC merge target %s must have primary key %v; found %v", table, expected, actual)
	}
	return nil
}

func (d *FabricDestination) mergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}
	if opts.CDCExpectedIncarnation == "" {
		return d.mergeTableWithExecutor(ctx, d.db, opts)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.checkCDCIncarnationTx(ctx, tx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if err := d.mergeTableWithExecutor(ctx, tx, opts); err != nil {
		return err
	}
	if err := d.checkCDCIncarnationTx(ctx, tx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit merge transaction: %w", err)
	}
	return nil
}

func (d *FabricDestination) mergeTableWithExecutor(ctx context.Context, exec sqlExecutor, opts destination.MergeOptions) error {
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}

	mergeSQL := buildFabricMergeSQL(opts)
	config.Debug("[Fabric MERGE] Executing MERGE: %s", mergeSQL)

	if _, err := exec.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}
	return nil
}

func buildFabricMergeSQL(opts destination.MergeOptions) string {
	if destination.HasCDCDeletedColumn(opts.Columns) {
		if opts.CDCHardDelete {
			return buildFabricCDCHardDeleteMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalPredicate)
		}
		return buildFabricCDCMergeSQL(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, opts.Columns, opts.IncrementalPredicate)
	}

	quotedColumns := quoteColumns(opts.Columns)
	nonPKColumns := filterColumns(opts.Columns, opts.PrimaryKeys)
	return buildMergeSQLWithPredicate(opts.TargetTable, opts.StagingTable, opts.PrimaryKeys, quotedColumns, nonPKColumns, opts.IncrementalPredicate)
}

func buildFabricCDCMergeSQL(targetTable, stagingTable string, primaryKeys, columns []string, incrementalPredicate string) string {
	targetColumns := destination.DestinationColumns(columns)
	quotedTargetColumns := quoteColumns(targetColumns)
	nonPKColumns := excludeFabricIdentifiers(targetColumns, primaryKeys)

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(pk), quoteColumn(pk))
	}
	onClause := strings.Join(onConditions, " AND ")

	stagingCols := strings.Join(quoteColumns(columns), ", ")
	insertCols := strings.Join(quotedTargetColumns, ", ")
	sourceCols := make([]string, len(quotedTargetColumns))
	for i, col := range quotedTargetColumns {
		sourceCols[i] = "source." + col
	}

	pkPartition := strings.Join(quoteColumns(primaryKeys), ", ")
	uniqueName := newFabricInternalNameAllocator(columns)
	dedupRowNumber := quoteColumn(uniqueName("__bruin_dedup_rn"))
	hasActiveColumn := quoteColumn(uniqueName("__ingestr_has_active"))
	activeLSNColumn := quoteColumn(uniqueName("__ingestr_active_lsn"))

	pkMap := matchedFabricIdentifiers(columns, primaryKeys)
	laActJoin := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		laActJoin[i] = fmt.Sprintf("la.%s = act.%s", quoteColumn(pk), quoteColumn(pk))
	}

	selectCols := make([]string, 0, len(columns)+2)
	for _, col := range columns {
		alias := "act"
		if pkMap[col] || destination.IsCDCColumn(col) {
			alias = "la"
		}
		selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteColumn(col)))
	}
	selectCols = append(selectCols, fmt.Sprintf("CASE WHEN act.[_cdc_lsn] IS NOT NULL THEN 1 ELSE 0 END AS %s", hasActiveColumn))
	selectCols = append(selectCols, fmt.Sprintf("act.[_cdc_lsn] AS %s", activeLSNColumn))

	dedup := func(where, orderBy string) string {
		return fmt.Sprintf(
			`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s%s) AS _numbered WHERE %s = 1)`,
			stagingCols, stagingCols, pkPartition, orderBy, dedupRowNumber, quoteTable(stagingTable), where, dedupRowNumber,
		)
	}
	composedSource := fmt.Sprintf(
		"(SELECT %s FROM %s AS la LEFT JOIN %s AS act ON %s)",
		strings.Join(selectCols, ", "),
		dedup("", destination.CDCLatestOverallOrderBy(quoteColumn)),
		dedup(" WHERE [_cdc_deleted] = 0", "[_cdc_lsn] DESC"),
		strings.Join(laActJoin, " AND "),
	)

	hasFreshRowData := fmt.Sprintf(
		"(source.[_cdc_deleted] = 0 OR (source.%s = 1 AND (target.[_cdc_lsn] IS NULL OR source.%s >= target.[_cdc_lsn])))",
		hasActiveColumn,
		activeLSNColumn,
	)
	newerChange := "(target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] > target.[_cdc_lsn] OR (source.[_cdc_lsn] = target.[_cdc_lsn] AND source.[_cdc_deleted] = 1 AND COALESCE(target.[_cdc_deleted], 0) = 0))"
	matchedConditions := []string{newerChange}
	if strings.TrimSpace(incrementalPredicate) != "" {
		matchedConditions = append([]string{"(" + incrementalPredicate + ")"}, matchedConditions...)
	}

	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(columns)
	updates := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		quoted := quoteColumn(col)
		if destination.IsCDCColumn(col) {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoted, quoted)
			continue
		}
		condition := hasFreshRowData
		if hasUnchangedCols {
			unchanged := fabricUnchangedColExists(col)
			condition = fmt.Sprintf("%s AND NOT %s", hasFreshRowData, unchanged)
		}
		updates[i] = fmt.Sprintf("target.%s = CASE WHEN %s THEN source.%s ELSE target.%s END", quoted, condition, quoted, quoted)
	}

	updateClause := ""
	if len(updates) > 0 {
		updateClause = fmt.Sprintf("WHEN MATCHED AND %s THEN UPDATE SET %s\n", strings.Join(matchedConditions, " AND "), strings.Join(updates, ", "))
	}

	return fmt.Sprintf(
		`MERGE %s AS target
USING %s AS source
ON %s
%sWHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);`,
		quoteTable(targetTable),
		composedSource,
		onClause,
		updateClause,
		insertCols,
		strings.Join(sourceCols, ", "),
	)
}

func buildFabricCDCHardDeleteMergeSQL(targetTable, stagingTable string, primaryKeys, columns []string, incrementalPredicate string) string {
	targetColumns := destination.DestinationColumns(columns)
	quotedTargetColumns := quoteColumns(targetColumns)
	nonPKColumns := excludeFabricIdentifiers(targetColumns, primaryKeys)
	stagingCols := strings.Join(quoteColumns(columns), ", ")
	dedupRowNumber := quoteColumn(newFabricInternalNameAllocator(columns)("__bruin_dedup_rn"))

	onConditions := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", quoteColumn(pk), quoteColumn(pk))
	}
	latest := fmt.Sprintf(
		`(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS _numbered WHERE %s = 1)`,
		stagingCols, stagingCols, strings.Join(quoteColumns(primaryKeys), ", "), destination.CDCLatestOverallOrderBy(quoteColumn), dedupRowNumber, quoteTable(stagingTable), dedupRowNumber,
	)

	predicate := ""
	if strings.TrimSpace(incrementalPredicate) != "" {
		predicate = "(" + incrementalPredicate + ") AND "
	}
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(columns)
	updates := make([]string, len(nonPKColumns))
	for i, col := range nonPKColumns {
		quoted := quoteColumn(col)
		if !hasUnchangedCols || destination.IsCDCColumn(col) {
			updates[i] = fmt.Sprintf("target.%s = source.%s", quoted, quoted)
			continue
		}
		unchanged := fabricUnchangedColExists(col)
		updates[i] = fmt.Sprintf("target.%s = CASE WHEN NOT %s THEN source.%s ELSE target.%s END", quoted, unchanged, quoted, quoted)
	}
	updateClause := ""
	if len(updates) > 0 {
		updateClause = fmt.Sprintf(
			"WHEN MATCHED AND %ssource.[_cdc_deleted] = 0 AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] > target.[_cdc_lsn]) THEN UPDATE SET %s\n",
			predicate,
			strings.Join(updates, ", "),
		)
	}
	sourceCols := make([]string, len(quotedTargetColumns))
	for i, col := range quotedTargetColumns {
		sourceCols[i] = "source." + col
	}

	return fmt.Sprintf(
		`MERGE %s AS target
USING %s AS source
ON %s
WHEN MATCHED AND %ssource.[_cdc_deleted] = 1 AND (target.[_cdc_lsn] IS NULL OR source.[_cdc_lsn] >= target.[_cdc_lsn]) THEN DELETE
%sWHEN NOT MATCHED AND source.[_cdc_deleted] = 0 THEN INSERT (%s) VALUES (%s);`,
		quoteTable(targetTable),
		latest,
		strings.Join(onConditions, " AND "),
		predicate,
		updateClause,
		strings.Join(quotedTargetColumns, ", "),
		strings.Join(sourceCols, ", "),
	)
}

func fabricUnchangedColExists(col string) string {
	return fmt.Sprintf(
		"EXISTS (SELECT 1 FROM OPENJSON(COALESCE(source.%s, '[]')) WHERE [value] COLLATE Latin1_General_100_BIN2_UTF8 = '%s' COLLATE Latin1_General_100_BIN2_UTF8)",
		quoteColumn(destination.CDCUnchangedColsColumn),
		strings.ReplaceAll(col, "'", "''"),
	)
}

func newFabricInternalNameAllocator(columns []string) func(string) string {
	used := make(map[string]struct{}, len(columns)+3)
	for _, col := range columns {
		used[strings.ToLower(col)] = struct{}{}
	}
	return func(base string) string {
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := used[strings.ToLower(candidate)]; !exists {
				used[strings.ToLower(candidate)] = struct{}{}
				return candidate
			}
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
	}
}

// excludeFabricIdentifiers removes the selected identifiers from columns using
// matchedFabricIdentifiers, so a key `id` does not also drop a distinct `ID`.
func excludeFabricIdentifiers(columns, excluded []string) []string {
	matched := matchedFabricIdentifiers(columns, excluded)
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		if !matched[col] {
			result = append(result, col)
		}
	}
	return result
}

func matchedFabricIdentifiers(columns, selected []string) map[string]bool {
	foldedCounts := make(map[string]int, len(columns))
	for _, col := range columns {
		foldedCounts[strings.ToLower(col)]++
	}
	exact := make(map[string]bool, len(selected))
	folded := make(map[string]bool, len(selected))
	for _, col := range selected {
		exact[col] = true
		folded[strings.ToLower(col)] = true
	}

	matched := make(map[string]bool, len(selected))
	for _, col := range columns {
		if exact[col] || (foldedCounts[strings.ToLower(col)] == 1 && folded[strings.ToLower(col)]) {
			matched[col] = true
		}
	}
	return matched
}

func (d *FabricDestination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	var maxLSN sql.NullString
	query := fmt.Sprintf("SELECT MAX([_cdc_lsn]) FROM %s", quoteTable(table))
	err := d.db.QueryRowContext(ctx, query).Scan(&maxLSN)
	if err != nil {
		if isMissingObjectError(err) || isInvalidColumnError(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read Fabric max CDC LSN for %s: %w", table, err)
	}
	if !maxLSN.Valid {
		return "", nil
	}
	return maxLSN.String, nil
}

func (d *FabricDestination) LoadCDCState(ctx context.Context, table, connectorID string) ([]destination.CDCStateEntry, error) {
	query := fmt.Sprintf("SELECT [event_id], [source_table], [destination_table], [state_kind], [state_generation], [state_status], [_cdc_lsn] FROM %s WHERE [connector_id] = @p1", quoteTable(table))
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isMissingObjectError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load Fabric CDC state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []destination.CDCStateEntry
	for rows.Next() {
		var entry destination.CDCStateEntry
		if err := rows.Scan(&entry.EventID, &entry.SourceTable, &entry.DestinationTable, &entry.StateKind, &entry.Generation, &entry.Status, &entry.Position); err != nil {
			return nil, fmt.Errorf("failed to scan Fabric CDC state: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (d *FabricDestination) LoadCDCStateFence(ctx context.Context, table, connectorID string) (destination.CDCStateFence, error) {
	quotedTable := quoteTable(table)
	query := fmt.Sprintf(
		"SELECT DISTINCT [event_id], [state_generation] FROM %s WHERE [connector_id] = @p1 AND [state_kind] = 'run' AND [state_generation] = (SELECT MAX([state_generation]) FROM %s WHERE [connector_id] = @p1 AND [state_kind] = 'run') ORDER BY [event_id]",
		quotedTable,
		quotedTable,
	)
	rows, err := d.db.QueryContext(ctx, query, connectorID)
	if err != nil {
		if isMissingObjectError(err) {
			return destination.CDCStateFence{}, nil
		}
		return destination.CDCStateFence{}, fmt.Errorf("failed to load Fabric CDC fence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var fence destination.CDCStateFence
	for rows.Next() {
		var eventID string
		var generation int64
		if err := rows.Scan(&eventID, &generation); err != nil {
			return destination.CDCStateFence{}, fmt.Errorf("failed to scan Fabric CDC fence: %w", err)
		}
		fence.Generation = generation
		fence.RunEventIDs = append(fence.RunEventIDs, eventID)
	}
	return fence, rows.Err()
}

func (d *FabricDestination) DeleteCDCStateEvents(ctx context.Context, table, connectorID string, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, connectorID)
	placeholders := make([]string, len(eventIDs))
	for i, eventID := range eventIDs {
		placeholders[i] = fmt.Sprintf("@p%d", i+2)
		args = append(args, eventID)
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE [connector_id] = @p1 AND [event_id] IN (%s)", quoteTable(table), strings.Join(placeholders, ", "))
	if _, err := d.db.ExecContext(ctx, query, args...); err != nil {
		if isMissingObjectError(err) {
			return nil
		}
		return fmt.Errorf("failed to prune Fabric CDC state events: %w", err)
	}
	return nil
}

func (d *FabricDestination) EnsureCDCStatePositionColumn(ctx context.Context, table string) error {
	identity, err := d.resolveTargetIdentity(table)
	if err != nil {
		return err
	}
	prefix := identity.catalogPrefix(d.database)
	query := fmt.Sprintf(
		`SELECT CHARACTER_MAXIMUM_LENGTH FROM %sINFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2 AND COLUMN_NAME = '_cdc_lsn'`,
		prefix,
	)
	var maxLength sql.NullInt64
	err = d.db.QueryRowContext(ctx, query, identity.schema, identity.table).Scan(&maxLength)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to inspect Fabric CDC state position column: %w", err)
	}
	if maxLength.Valid && maxLength.Int64 == -1 {
		return nil
	}
	alterSQL := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN [_cdc_lsn] VARCHAR(MAX) NOT NULL", quoteTable(table))
	if _, err := d.db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("failed to widen Fabric CDC state position column: %w", err)
	}
	return nil
}

func (d *FabricDestination) CanonicalCDCTarget(ctx context.Context, table string) (string, error) {
	identity, err := d.resolveTargetIdentity(table)
	if err != nil {
		return "", err
	}
	actualDatabase := identity.database
	caseSensitive := true
	if d.db != nil && identity.database != "" {
		dbName, collation, err := d.databaseCollation(ctx, identity.database)
		if err != nil {
			return "", err
		}
		actualDatabase = dbName
		caseSensitive = fabricCollationCaseSensitive(collation)
	}
	if !caseSensitive {
		identity.schema = strings.ToLower(identity.schema)
		identity.table = strings.ToLower(identity.table)
	}
	return destination.CDCTargetKeyDigest(actualDatabase, identity.schema, identity.table), nil
}

func (d *FabricDestination) databaseCollation(ctx context.Context, database string) (string, string, error) {
	var dbName, collation string
	var err error
	if database != "" {
		err = d.db.QueryRowContext(ctx, "SELECT [name], [collation_name] FROM [sys].[databases] WHERE [name] = @p1", database).Scan(&dbName, &collation)
	} else {
		err = d.db.QueryRowContext(ctx, "SELECT [name], [collation_name] FROM [sys].[databases] WHERE [name] = DB_NAME()").Scan(&dbName, &collation)
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve Fabric database collation for %q: %w", database, err)
	}
	return dbName, collation, nil
}

func (d *FabricDestination) sameSwapSchema(ctx context.Context, targetID, stagingID fabricTargetIdentity) (bool, error) {
	if !strings.EqualFold(targetID.database, stagingID.database) {
		return false, nil
	}
	var collation string
	hasCollation := false
	if targetID.database != stagingID.database {
		if d.db == nil {
			return false, nil
		}
		targetDB, targetCollation, err := d.databaseCollation(ctx, targetID.database)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		stagingDB, _, err := d.databaseCollation(ctx, stagingID.database)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if targetDB != stagingDB {
			return false, nil
		}
		collation = targetCollation
		hasCollation = true
	}
	if targetID.schema == stagingID.schema {
		return true, nil
	}
	if !strings.EqualFold(targetID.schema, stagingID.schema) {
		return false, nil
	}
	if !hasCollation {
		if d.db == nil {
			return false, nil
		}
		var err error
		_, collation, err = d.databaseCollation(ctx, targetID.database)
		if err != nil {
			return false, err
		}
	}
	return !fabricCollationCaseSensitive(collation), nil
}

func fabricCollationCaseSensitive(collation string) bool {
	upper := strings.ToUpper(collation)
	return strings.Contains(upper, "_CS_") || strings.Contains(upper, "_BIN")
}

func (d *FabricDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	return d.cdcTargetIncarnation(ctx, d.db, table)
}

func (d *FabricDestination) cdcTargetIncarnation(ctx context.Context, exec sqlExecutor, table string) (string, bool, error) {
	identity, err := d.resolveTargetIdentity(table)
	if err != nil {
		return "", false, err
	}
	prefix := identity.catalogPrefix(d.database)
	query := fmt.Sprintf(`SELECT CAST(t.object_id AS BIGINT), CONVERT(VARCHAR(33), t.create_date, 126)
		FROM %ssys.tables AS t
		JOIN %ssys.schemas AS s ON s.schema_id = t.schema_id
		WHERE s.name = @p1 AND t.name = @p2`, prefix, prefix)
	var objectID int64
	var createdAt string
	err = exec.QueryRowContext(ctx, query, identity.schema, identity.table).Scan(&objectID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read Fabric CDC target incarnation for %s: %w", table, err)
	}
	return destination.CDCTargetKey(identity.database, strconv.FormatInt(objectID, 10), createdAt), true, nil
}

func (d *FabricDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	return d.checkCDCIncarnationTx(ctx, d.db, table, expected)
}

func (d *FabricDestination) checkCDCIncarnationTx(ctx context.Context, exec sqlExecutor, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.cdcTargetIncarnation(ctx, exec, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("fabric CDC target %q physical incarnation changed", table)
	}
	return nil
}

func (d *FabricDestination) claimMarker(claimTable, canonicalTarget string) string {
	schemaName, _ := parseTableName(claimTable)
	if schemaName == "" {
		schemaName = "dbo"
	}
	return quoteColumn(schemaName) + "." + quoteColumn("cdc_claim_"+destination.CDCTargetKeyDigest(canonicalTarget))
}

func (d *FabricDestination) ClaimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) error {
	_, err := d.claimCDCTarget(ctx, claimTable, claim)
	return err
}

func (d *FabricDestination) claimCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim) (bool, error) {
	ownerID, err := claim.OwnerID()
	if err != nil {
		return false, err
	}
	canonicalTarget, err := d.CanonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return false, err
	}

	var existingOwner string
	selectSQL := fmt.Sprintf("SELECT [connector_id] FROM %s WHERE [destination_table] = @p1", quoteTable(claimTable))
	err = d.db.QueryRowContext(ctx, selectSQL, canonicalTarget).Scan(&existingOwner)
	if err == nil {
		if existingOwner != ownerID {
			return false, fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, existingOwner)
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to inspect Fabric CDC target claim: %w", err)
	}

	// Fabric primary keys are NOT ENFORCED, so CREATE VIEW provides catalog-level
	// mutual exclusion across concurrent claims for the same target.
	marker := d.claimMarker(claimTable, canonicalTarget)
	createdView := false
	createViewSQL := fmt.Sprintf("CREATE VIEW %s AS SELECT '%s' AS [owner_id]", marker, strings.ReplaceAll(ownerID, "'", "''"))
	if _, err := d.db.ExecContext(ctx, createViewSQL); err != nil {
		if !isObjectAlreadyExistsError(err) {
			return false, fmt.Errorf("failed to create Fabric CDC target claim lock: %w", err)
		}
	} else {
		createdView = true
	}

	var markerOwner string
	if err := d.db.QueryRowContext(ctx, "SELECT [owner_id] FROM "+marker).Scan(&markerOwner); err != nil {
		return false, fmt.Errorf("failed to read Fabric CDC target claim lock: %w", err)
	}
	if markerOwner != ownerID {
		return false, fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, markerOwner)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		if createdView {
			d.dropClaimMarkerQuietly(ctx, marker)
		}
		return false, fmt.Errorf("failed to begin Fabric CDC target claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, selectSQL, canonicalTarget).Scan(&existingOwner)
	if errors.Is(err, sql.ErrNoRows) {
		insertSQL := fmt.Sprintf("INSERT INTO %s ([destination_table], [connector_id], [claimed_at]) VALUES (@p1, @p2, SYSUTCDATETIME())", quoteTable(claimTable))
		if _, err := tx.ExecContext(ctx, insertSQL, canonicalTarget, ownerID); err != nil {
			if createdView {
				d.dropClaimMarkerQuietly(ctx, marker)
			}
			return false, fmt.Errorf("failed to record Fabric CDC target claim: %w", err)
		}
		if err := tx.Commit(); err != nil {
			if createdView {
				d.dropClaimMarkerQuietly(ctx, marker)
			}
			return false, fmt.Errorf("failed to commit Fabric CDC target claim: %w", err)
		}
		return true, nil
	}
	if err != nil {
		if createdView {
			d.dropClaimMarkerQuietly(ctx, marker)
		}
		return false, fmt.Errorf("failed to verify Fabric CDC target claim: %w", err)
	}
	if existingOwner != ownerID {
		if createdView {
			d.dropClaimMarkerQuietly(ctx, marker)
		}
		return false, fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, existingOwner)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit Fabric CDC target claim: %w", err)
	}
	return createdView, nil
}

func (d *FabricDestination) dropClaimMarkerQuietly(ctx context.Context, marker string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, _ = d.db.ExecContext(cleanupCtx, "DROP VIEW IF EXISTS "+marker)
}

func (d *FabricDestination) ClaimAndPrepareEmptyCDCTarget(ctx context.Context, claimTable string, claim destination.CDCTargetClaim, opts destination.PrepareOptions) (string, error) {
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

	newlyClaimed, err := d.claimCDCTarget(ctx, claimTable, claim)
	if err != nil {
		return "", err
	}

	opts.DropFirst = false
	if err := d.prepareTable(ctx, opts, true); err != nil {
		if newlyClaimed {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			ownerID, _ := claim.OwnerID()
			marker := d.claimMarker(claimTable, target)
			_, dropErr := d.db.ExecContext(cleanupCtx, "DROP VIEW IF EXISTS "+marker)
			_, deleteErr := d.db.ExecContext(cleanupCtx, fmt.Sprintf("DELETE FROM %s WHERE [destination_table] = @p1 AND [connector_id] = @p2", quoteTable(claimTable)), target, ownerID)
			return "", errors.Join(err, dropErr, deleteErr)
		}
		return "", err
	}

	incarnation, exists, err := d.CDCTargetIncarnation(ctx, opts.Table)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("claimed Fabric CDC target %q disappeared after creation", opts.Table)
	}
	return incarnation, nil
}

func (d *FabricDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *FabricDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("fabric conditional CDC truncate requires an expected incarnation")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin Fabric conditional truncate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := d.checkCDCIncarnationTx(ctx, tx, table, expectedIncarnation); err != nil {
		return err
	}
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(table))
	if _, err := tx.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate Fabric CDC target %s: %w", table, err)
	}
	if err := d.checkCDCIncarnationTx(ctx, tx, table, expectedIncarnation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit Fabric conditional truncate: %w", err)
	}
	return nil
}

func (d *FabricDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if len(merges) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin multi-table Fabric CDC transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if merge.Options.CDCExpectedIncarnation != "" {
			if err := d.checkCDCIncarnationTx(ctx, tx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
				return err
			}
		}
		if merge.Truncate {
			truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(merge.Options.TargetTable))
			if _, err := tx.ExecContext(ctx, truncateSQL); err != nil {
				config.LogFailedQuery(truncateSQL, err)
				return fmt.Errorf("failed to truncate Fabric CDC target %s: %w", merge.Options.TargetTable, err)
			}
		}
		if err := d.mergeTableWithExecutor(ctx, tx, merge.Options); err != nil {
			return fmt.Errorf("failed to merge Fabric CDC target %s: %w", merge.Options.TargetTable, err)
		}
		if merge.Options.CDCExpectedIncarnation != "" {
			if err := d.checkCDCIncarnationTx(ctx, tx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit multi-table Fabric CDC transaction: %w", err)
	}
	return nil
}

func (d *FabricDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetID, stagingID, err := d.resolveSwapIdentities(targetTable, stagingTable)
	if err != nil {
		return "", "", err
	}

	sameSchema, err := d.sameSwapSchema(ctx, targetID, stagingID)
	if err != nil {
		return "", "", err
	}

	physical := stagingTable
	if !sameSchema {
		cloned, err := d.cloneStagingIntoTargetSchema(ctx, targetID, stagingTable)
		if err != nil {
			return "", "", err
		}
		d.swapMu.Lock()
		if d.preparedSwaps == nil {
			d.preparedSwaps = make(map[string]string)
		}
		previous, hadPrevious := d.preparedSwaps[stagingTable]
		d.preparedSwaps[stagingTable] = cloned
		d.swapMu.Unlock()
		if hadPrevious {
			_ = d.DropTable(ctx, previous)
		}
		physical = cloned
	}

	incarnation, exists, err := d.CDCTargetIncarnation(ctx, physical)
	if err != nil {
		if physical != stagingTable {
			if cloned, ok := d.takePreparedSwap(stagingTable); ok {
				_ = d.DropTable(ctx, cloned)
			}
		}
		return "", "", err
	}
	if !exists {
		if physical != stagingTable {
			if cloned, ok := d.takePreparedSwap(stagingTable); ok {
				_ = d.DropTable(ctx, cloned)
			}
		}
		return "", "", fmt.Errorf("fabric CDC staging table %q does not exist", physical)
	}
	return incarnation, incarnation, nil
}

// resolveSwapIdentities resolves both swap tables and rejects tables in another
// warehouse: sys.sp_rename only renames objects in the connected warehouse.
func (d *FabricDestination) resolveSwapIdentities(targetTable, stagingTable string) (fabricTargetIdentity, fabricTargetIdentity, error) {
	targetID, err := d.resolveTargetIdentity(targetTable)
	if err != nil {
		return fabricTargetIdentity{}, fabricTargetIdentity{}, err
	}
	stagingID, err := d.resolveTargetIdentity(stagingTable)
	if err != nil {
		return fabricTargetIdentity{}, fabricTargetIdentity{}, err
	}
	for _, id := range []struct {
		role  string
		table string
		id    fabricTargetIdentity
	}{{"target", targetTable, targetID}, {"staging", stagingTable, stagingID}} {
		if id.id.catalogPrefix(d.database) != "" {
			return fabricTargetIdentity{}, fabricTargetIdentity{}, fmt.Errorf("fabric table swap %s %q is in warehouse %q; swaps are only supported within the connected warehouse %q", id.role, id.table, id.id.database, d.database)
		}
	}
	return targetID, stagingID, nil
}

func (d *FabricDestination) takePreparedSwap(stagingTable string) (string, bool) {
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	cloned, ok := d.preparedSwaps[stagingTable]
	delete(d.preparedSwaps, stagingTable)
	return cloned, ok
}

func (d *FabricDestination) cloneStagingIntoTargetSchema(ctx context.Context, targetID fabricTargetIdentity, stagingTable string) (string, error) {
	if err := d.ensureSchemaExists(ctx, targetID.schema); err != nil {
		return "", fmt.Errorf("failed to ensure target schema exists: %w", err)
	}
	cloneCandidate := fmt.Sprintf("%s_swap_%d", targetID.table, time.Now().UnixNano())
	cloneName := destination.ShortenIdentifier(cloneCandidate, cloneCandidate, destination.MaxIdentifierLength("fabric"))
	cloneTable := targetID.qualifiedInSchema(d.database, cloneName)
	cloneSQL := fmt.Sprintf("CREATE TABLE %s AS CLONE OF %s", cloneTable, quoteTable(stagingTable))
	if _, err := d.db.ExecContext(ctx, cloneSQL); err != nil {
		config.LogFailedQuery(cloneSQL, err)
		return "", fmt.Errorf("failed to clone Fabric staging table %s into target schema: %w", stagingTable, err)
	}
	return cloneTable, nil
}

func (d *FabricDestination) swapTable(ctx context.Context, opts destination.SwapOptions) error {
	conditional := opts.CDCExpectedIncarnation != "" || opts.CDCExpectedStagingIncarnation != "" || opts.CDCExpectedResultIncarnation != ""
	if conditional {
		if opts.CDCExpectedIncarnation == "" || opts.CDCExpectedStagingIncarnation == "" || opts.CDCExpectedResultIncarnation == "" {
			return errors.New("fabric CDC conditional swap requires target, staging, and result incarnations")
		}
	}

	targetID, stagingID, err := d.resolveSwapIdentities(opts.TargetTable, opts.StagingTable)
	if err != nil {
		return err
	}

	swapSource := opts.StagingTable
	dropSourceAfterCommit := false
	if prepared, ok := d.takePreparedSwap(opts.StagingTable); ok {
		if err := d.ensureSchemaExists(ctx, targetID.schema); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
		swapSource = prepared
		dropSourceAfterCommit = true
	} else {
		sameSchema, err := d.sameSwapSchema(ctx, targetID, stagingID)
		if err != nil {
			return err
		}
		if !sameSchema {
			cloned, err := d.cloneStagingIntoTargetSchema(ctx, targetID, opts.StagingTable)
			if err != nil {
				return err
			}
			swapSource = cloned
			dropSourceAfterCommit = true
		} else if err := d.ensureSchemaExists(ctx, targetID.schema); err != nil {
			return fmt.Errorf("failed to ensure target schema exists: %w", err)
		}
	}

	swapSourceID, err := d.resolveTargetIdentity(swapSource)
	if err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		if dropSourceAfterCommit {
			_ = d.DropTable(ctx, swapSource)
		}
		return fmt.Errorf("failed to begin Fabric swap transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
			if dropSourceAfterCommit {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				_ = d.DropTable(cleanupCtx, swapSource)
			}
		}
	}()

	if conditional {
		if err := d.checkCDCIncarnationTx(ctx, tx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
			return err
		}
		if err := d.checkCDCIncarnationTx(ctx, tx, swapSource, opts.CDCExpectedStagingIncarnation); err != nil {
			return err
		}
	}

	_, targetExists, err := d.cdcTargetIncarnation(ctx, tx, opts.TargetTable)
	if err != nil {
		return fmt.Errorf("failed to inspect Fabric swap target %s: %w", opts.TargetTable, err)
	}

	if targetExists {
		backupCandidate := fmt.Sprintf("%s_old_%d", targetID.table, time.Now().UnixNano())
		backupName := destination.ShortenIdentifier(backupCandidate, backupCandidate, destination.MaxIdentifierLength("fabric"))
		backupTable := targetID.qualifiedInSchema(d.database, backupName)

		renameOldSQL := buildFabricSPRenameSQL(quoteColumn(targetID.schema)+"."+quoteColumn(targetID.table), backupName)
		if _, err := tx.ExecContext(ctx, renameOldSQL); err != nil {
			config.LogFailedQuery(renameOldSQL, err)
			return fmt.Errorf("failed to rename existing Fabric target table: %w", err)
		}

		renameNewSQL := buildFabricSPRenameSQL(quoteColumn(swapSourceID.schema)+"."+quoteColumn(swapSourceID.table), targetID.table)
		if _, err := tx.ExecContext(ctx, renameNewSQL); err != nil {
			config.LogFailedQuery(renameNewSQL, err)
			return fmt.Errorf("failed to rename Fabric staging table onto target: %w", err)
		}

		dropOldSQL := fmt.Sprintf("DROP TABLE %s", backupTable)
		if _, err := tx.ExecContext(ctx, dropOldSQL); err != nil {
			config.LogFailedQuery(dropOldSQL, err)
			return fmt.Errorf("failed to drop replaced Fabric target table: %w", err)
		}
	} else {
		renameNewSQL := buildFabricSPRenameSQL(quoteColumn(swapSourceID.schema)+"."+quoteColumn(swapSourceID.table), targetID.table)
		if _, err := tx.ExecContext(ctx, renameNewSQL); err != nil {
			config.LogFailedQuery(renameNewSQL, err)
			return fmt.Errorf("failed to rename Fabric staging table onto target: %w", err)
		}
	}

	if conditional {
		if err := d.checkCDCIncarnationTx(ctx, tx, opts.TargetTable, opts.CDCExpectedResultIncarnation); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit Fabric table swap: %w", err)
	}
	committed = true

	if dropSourceAfterCommit {
		_ = d.DropTable(ctx, opts.StagingTable)
	}
	return nil
}

func buildFabricSPRenameSQL(oldSchemaAndTable, newTableNameOnly string) string {
	return fmt.Sprintf(
		"EXEC sys.sp_rename '%s', '%s', 'OBJECT'",
		strings.ReplaceAll(oldSchemaAndTable, "'", "''"),
		strings.ReplaceAll(newTableNameOnly, "'", "''"),
	)
}

type fabricCDCRunLease struct {
	d      *FabricDestination
	schema string
	name   string
	token  string

	lastHeartbeat atomic.Int64
	stopping      atomic.Bool

	done        chan struct{}
	stop        chan struct{}
	stopped     chan struct{}
	doneOnce    sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	err         error
	releaseErr  error
}

func (d *FabricDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	sum := sha256.Sum256([]byte(connectorID))
	tokenBytes := make([]byte, 12)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	lease := &fabricCDCRunLease{
		d:       d,
		schema:  cdcLeaseSchema,
		name:    "cdc_lease_" + hex.EncodeToString(sum[:10]),
		token:   token,
		done:    make(chan struct{}),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	if err := d.ensureSchemaExists(ctx, lease.schema); err != nil {
		return nil, fmt.Errorf("ensure Fabric CDC lease schema: %w", err)
	}

	deadline := time.Now().Add(cdcLeaseTTL + cdcLeaseAcquireGrace)
	for {
		acquired, err := lease.tryAcquire(ctx)
		if err != nil {
			return nil, err
		}
		if acquired {
			lease.lastHeartbeat.Store(time.Now().UnixNano())
			go lease.monitor()
			return lease, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another CDC run already owns connector %s (Fabric lease %s.%s)", connectorID, lease.schema, lease.name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(cdcLeaseRetryDelay):
		}
	}
}

func (l *fabricCDCRunLease) tableName() string {
	return l.schema + "." + l.name
}

func (l *fabricCDCRunLease) ref() string {
	return quoteTable(l.tableName())
}

func (l *fabricCDCRunLease) tryAcquire(ctx context.Context) (bool, error) {
	tx, err := l.d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin Fabric CDC run lease transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	createSQL := fmt.Sprintf(
		"CREATE TABLE %s ([token] VARCHAR(64) NOT NULL, [heartbeat] DATETIME2(6) NOT NULL)",
		l.ref(),
	)
	if _, err := tx.ExecContext(ctx, createSQL); err == nil {
		insertSQL := fmt.Sprintf("INSERT INTO %s ([token], [heartbeat]) VALUES (@p1, SYSUTCDATETIME())", l.ref())
		if _, err := tx.ExecContext(ctx, insertSQL, l.token); err != nil {
			return false, fmt.Errorf("record Fabric CDC lease heartbeat: %w", err)
		}
		if err := tx.Commit(); err != nil {
			if isObjectAlreadyExistsError(err) {
				return false, nil
			}
			return false, fmt.Errorf("commit Fabric CDC run lease: %w", err)
		}
		committed = true
		owner, err := l.owner(ctx, l.tableName())
		if err != nil {
			return false, err
		}
		return owner == l.token, nil
	} else if !isObjectAlreadyExistsError(err) {
		return false, fmt.Errorf("acquire Fabric CDC run lease: %w", err)
	}
	_ = tx.Rollback()

	staleToken, err := l.staleHolder(ctx)
	if err != nil || staleToken == "" {
		return false, err
	}

	takeoverTx, err := l.d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin Fabric CDC stale lease takeover: %w", err)
	}
	takeoverCommitted := false
	defer func() {
		if !takeoverCommitted {
			_ = takeoverTx.Rollback()
		}
	}()

	dead := l.name + "_expired_" + l.token
	if _, err := takeoverTx.ExecContext(ctx, buildFabricSPRenameSQL(l.ref(), dead)); err != nil {
		return false, nil
	}
	var movedToken string
	scanErr := takeoverTx.QueryRowContext(ctx, "SELECT TOP 1 [token] FROM "+quoteTable(l.schema+"."+dead)).Scan(&movedToken)
	if staleToken == emptyLeaseSentinel {
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return false, nil
		}
	} else if scanErr != nil || movedToken != staleToken {
		return false, nil
	}
	if _, err := takeoverTx.ExecContext(ctx, "DROP TABLE "+quoteTable(l.schema+"."+dead)); err != nil {
		return false, nil
	}
	if err := takeoverTx.Commit(); err != nil {
		return false, nil
	}
	takeoverCommitted = true
	config.Debug("[Fabric] Took over expired CDC run lease %s", l.tableName())
	return false, nil
}

func (l *fabricCDCRunLease) staleHolder(ctx context.Context) (string, error) {
	var token string
	var ageSeconds sql.NullInt64
	err := l.d.db.QueryRowContext(
		ctx,
		"SELECT TOP 1 [token], DATEDIFF(SECOND, [heartbeat], SYSUTCDATETIME()) FROM "+l.ref(),
	).Scan(&token, &ageSeconds)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = l.d.db.QueryRowContext(
				ctx,
				`SELECT DATEDIFF(SECOND, t.create_date, SYSUTCDATETIME())
				 FROM sys.tables AS t
				 JOIN sys.schemas AS s ON s.schema_id = t.schema_id
				 WHERE s.name = @p1 AND t.name = @p2`,
				l.schema,
				l.name,
			).Scan(&ageSeconds)
			if errors.Is(err, sql.ErrNoRows) {
				return "", nil
			}
			if err != nil {
				return "", fmt.Errorf("read Fabric CDC lease creation time: %w", err)
			}
			if ageSeconds.Valid && time.Duration(ageSeconds.Int64)*time.Second > cdcLeaseTTL {
				return emptyLeaseSentinel, nil
			}
			return "", nil
		}
		if isMissingObjectError(err) {
			return "", nil
		}
		return "", fmt.Errorf("read Fabric CDC lease heartbeat: %w", err)
	}
	if ageSeconds.Valid && time.Duration(ageSeconds.Int64)*time.Second > cdcLeaseTTL {
		return token, nil
	}
	return "", nil
}

func (l *fabricCDCRunLease) owner(ctx context.Context, table string) (string, error) {
	var token string
	err := l.d.db.QueryRowContext(ctx, "SELECT TOP 1 [token] FROM "+quoteTable(table)).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isMissingObjectError(err) {
			return "", sql.ErrNoRows
		}
		return "", fmt.Errorf("read Fabric CDC lease owner: %w", err)
	}
	return token, nil
}

func (l *fabricCDCRunLease) heartbeat(ctx context.Context) (bool, error) {
	res, err := l.d.db.ExecContext(ctx, "UPDATE "+l.ref()+" SET [heartbeat] = SYSUTCDATETIME() WHERE [token] = @p1", l.token)
	if err != nil {
		if isMissingObjectError(err) {
			return false, sql.ErrNoRows
		}
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return true, nil
	}
	return affected > 0, nil
}

func (l *fabricCDCRunLease) monitor() {
	defer close(l.stopped)
	ticker := time.NewTicker(cdcLeaseCheck)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
		}
		if l.renewalOverdue() {
			l.markLost(errors.New("the lease could not be renewed in time"))
			return
		}
		var lost error
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		owner, err := l.owner(ctx, l.tableName())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			lost = errors.New("the lease table no longer exists")
		case err != nil:
			config.Debug("[Fabric] CDC lease check failed: %v", err)
		case owner != l.token:
			lost = errors.New("the lease was taken over by another run")
		case time.Since(time.Unix(0, l.lastHeartbeat.Load())) >= cdcLeaseHeartbeat:
			ok, hbErr := l.heartbeat(ctx)
			if errors.Is(hbErr, sql.ErrNoRows) || (hbErr == nil && !ok) {
				lost = errors.New("the lease was taken over by another run")
			} else if hbErr != nil {
				config.Debug("[Fabric] CDC lease heartbeat failed: %v", hbErr)
			} else {
				l.lastHeartbeat.Store(time.Now().UnixNano())
			}
		}
		cancel()
		if lost == nil && l.renewalOverdue() {
			lost = errors.New("the lease could not be renewed in time")
		}
		if lost == nil {
			continue
		}
		l.markLost(lost)
		return
	}
}

func (l *fabricCDCRunLease) markLost(cause error) {
	l.mu.Lock()
	l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("fabric CDC run lease lost: %w", cause))
	l.mu.Unlock()
	l.doneOnce.Do(func() { close(l.done) })
}

func (l *fabricCDCRunLease) renewalOverdue() bool {
	return time.Since(time.Unix(0, l.lastHeartbeat.Load())) > cdcLeaseTTL/2
}

func (l *fabricCDCRunLease) Done() <-chan struct{} {
	if !l.stopping.Load() && l.renewalOverdue() {
		l.markLost(errors.New("the lease could not be renewed in time"))
	}
	return l.done
}

func (l *fabricCDCRunLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *fabricCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		l.stopping.Store(true)
		close(l.stop)
		<-l.stopped
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tx, err := l.d.db.BeginTx(ctx, nil)
		if err != nil {
			l.releaseErr = fmt.Errorf("begin Fabric CDC lease release: %w", err)
			return
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		var owner string
		err = tx.QueryRowContext(ctx, "SELECT TOP 1 [token] FROM "+l.ref()).Scan(&owner)
		if err != nil || owner != l.token {
			if err == nil || errors.Is(err, sql.ErrNoRows) || isMissingObjectError(err) {
				err = errors.New("lease was not owned during release")
			}
			l.releaseErr = fmt.Errorf("release Fabric CDC run lease: %w", err)
			return
		}
		released := l.name + "_released_" + l.token
		if _, err := tx.ExecContext(ctx, buildFabricSPRenameSQL(l.ref(), released)); err != nil {
			l.releaseErr = fmt.Errorf("release Fabric CDC run lease: %w", err)
			return
		}
		var movedToken string
		if err := tx.QueryRowContext(ctx, "SELECT TOP 1 [token] FROM "+quoteTable(l.schema+"."+released)).Scan(&movedToken); err != nil || movedToken != l.token {
			l.releaseErr = errors.New("release Fabric CDC run lease: the lease was taken over before release")
			return
		}
		if _, err := tx.ExecContext(ctx, "DROP TABLE "+quoteTable(l.schema+"."+released)); err != nil {
			l.releaseErr = fmt.Errorf("drop released Fabric CDC run lease: %w", err)
			return
		}
		if err := tx.Commit(); err != nil {
			l.releaseErr = fmt.Errorf("commit Fabric CDC run lease release: %w", err)
			return
		}
		committed = true
	})
	return l.releaseErr
}

func hasSQLErrorNumber(err error, target int32) bool {
	var sqlErr interface{ SQLErrorNumber() int32 }
	if errors.As(err, &sqlErr) && sqlErr.SQLErrorNumber() == target {
		return true
	}
	var detailed mssqldb.Error
	if !errors.As(err, &detailed) {
		return false
	}
	for _, item := range detailed.All {
		if item.Number == target {
			return true
		}
	}
	return false
}

func isObjectAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	if hasSQLErrorNumber(err, 2714) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already an object named") || strings.Contains(msg, "already exists")
}

func isMissingObjectError(err error) bool {
	if err == nil {
		return false
	}
	if hasSQLErrorNumber(err, 208) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid object name") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "cannot find the object")
}

func isInvalidColumnError(err error) bool {
	if err == nil {
		return false
	}
	if hasSQLErrorNumber(err, 207) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "invalid column name")
}
