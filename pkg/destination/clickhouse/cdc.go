package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

var (
	_ destination.Destination                  = (*ClickHouseDestination)(nil)
	_ destination.TruncateCapable              = (*ClickHouseDestination)(nil)
	_ destination.CDCTruncateCapable           = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSchemaEvolver  = (*ClickHouseDestination)(nil)
	_ destination.CDCMergeAware                = (*ClickHouseDestination)(nil)
	_ destination.CDCUnchangedColsAware        = (*ClickHouseDestination)(nil)
	_ destination.CDCHardDeleteCapable         = (*ClickHouseDestination)(nil)
	_ destination.CDCResumeProvider            = (*ClickHouseDestination)(nil)
	_ destination.SerializedCDCRunsRequired    = (*ClickHouseDestination)(nil)
	_ destination.StagingTableLister           = (*ClickHouseDestination)(nil)
	_ destination.CDCStateTablePreparer        = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePositionMigrator     = (*ClickHouseDestination)(nil)
	_ destination.CDCStateReader               = (*ClickHouseDestination)(nil)
	_ destination.CDCStateFenceReader          = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePruner               = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePruneBatchSizer      = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetClaimer             = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*ClickHouseDestination)(nil)
	_ destination.ManagedCDCStateValidator     = (*ClickHouseDestination)(nil)
	_ destination.ManagedCDCRunLeaser          = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalMergeCapable   = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalTruncater      = (*ClickHouseDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger    = (*ClickHouseDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer   = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSwapCapable    = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSwapPlanner    = (*ClickHouseDestination)(nil)
)

func (d *ClickHouseDestination) SupportsCDCMerge() bool            { return true }
func (d *ClickHouseDestination) SupportsCDCUnchangedCols() bool    { return true }
func (d *ClickHouseDestination) SupportsCDCHardDelete() bool       { return true }
func (d *ClickHouseDestination) SupportsCDCConditionalMerge() bool { return true }
func (d *ClickHouseDestination) SupportsCDCConditionalSwap() bool  { return true }
func (d *ClickHouseDestination) RequiresSerializedCDCRuns() bool   { return true }

func (d *ClickHouseDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	if err := tablename.TwoLevel("clickhouse").CheckName(table); err != nil {
		return "", err
	}
	db, name := d.parseTableName(table)
	if db == "" {
		return "", fmt.Errorf("ClickHouse CDC requires a database for target %q", table)
	}
	return destination.CDCTargetKey(db, name), nil
}

func (d *ClickHouseDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	db, name := d.parseTableName(table)
	uuid, modTime, exists, err := d.tableIdentity(ctx, db, name)
	if err != nil || !exists {
		return "", false, err
	}
	return destination.CDCTargetKey(db, name, formatTableIdentity(uuid, modTime)), true, nil
}

func (d *ClickHouseDestination) tableIdentity(ctx context.Context, db, name string) (string, string, bool, error) {
	var uuid, modTime string
	err := d.conn.QueryRow(
		ctx,
		"SELECT toString(uuid), toString(metadata_modification_time) FROM system.tables WHERE database = ? AND name = ?",
		db, name,
	).Scan(&uuid, &modTime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read ClickHouse table identity for %s.%s: %w", db, name, err)
	}
	return uuid, modTime, true, nil
}

func formatTableIdentity(uuid, modTime string) string {
	if uuid != "" && uuid != "00000000-0000-0000-0000-000000000000" {
		return uuid
	}
	return "legacy:" + modTime
}

func extractIncarnationUUID(incarnation string) string {
	idx := strings.LastIndex(incarnation, ":")
	if idx == -1 || idx+1 >= len(incarnation) {
		return ""
	}
	raw, err := destinationDecodeHex(incarnation[idx+1:])
	if err != nil {
		return ""
	}
	return raw
}

func destinationDecodeHex(s string) (string, error) {
	if len(s)%2 != 0 {
		return "", fmt.Errorf("invalid hex length %d", len(s))
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", err
		}
		out[i] = byte(v)
	}
	return string(out), nil
}

func (d *ClickHouseDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", table)
	}
	return nil
}

func (d *ClickHouseDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *ClickHouseDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("ClickHouse conditional CDC truncate requires an expected incarnation")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return err
	}
	if err := d.TruncateTable(ctx, table); err != nil {
		return fmt.Errorf("truncate ClickHouse CDC target: %w", err)
	}
	return d.checkCDCIncarnation(ctx, table, expectedIncarnation)
}

func (d *ClickHouseDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if merge.Truncate {
			if merge.Options.CDCExpectedIncarnation != "" {
				if err := d.TruncateCDCTableIfIncarnation(ctx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation); err != nil {
					return err
				}
			} else if err := d.TruncateTable(ctx, merge.Options.TargetTable); err != nil {
				return fmt.Errorf("truncate ClickHouse CDC target: %w", err)
			}
		}
		if err := d.MergeTable(ctx, merge.Options); err != nil {
			return err
		}
	}
	return nil
}

func (d *ClickHouseDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetDB, targetName := d.parseTableName(targetTable)
	stageDB, stageName := d.parseTableName(stagingTable)
	uuid, modTime, exists, err := d.tableIdentity(ctx, stageDB, stageName)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", fmt.Errorf("ClickHouse swap staging table %q does not exist", stagingTable)
	}
	id := formatTableIdentity(uuid, modTime)
	return destination.CDCTargetKey(stageDB, stageName, id), destination.CDCTargetKey(targetDB, targetName, id), nil
}

func (d *ClickHouseDestination) mergeCDC(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()
	if opts.IncrementalPredicate != "" {
		return fmt.Errorf("ClickHouse CDC merge does not support incremental predicates")
	}
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("ClickHouse CDC merge requires primary keys")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.validatePrimaryKey(ctx, "CDC target", opts.TargetTable, opts.PrimaryKeys); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	insertSQL := buildCDCMergeInsert(targetDB, targetName, stagingDB, stagingName, opts)
	config.Debug("[CLICKHOUSE CDC MERGE] Executing INSERT: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to merge ClickHouse CDC records: %w", err)
	}

	if opts.CDCHardDelete && !opts.CDCSnapshot {
		hasDeletes, err := d.stagingHasCDCDeletes(ctx, stagingDB, stagingName)
		if err != nil {
			return err
		}
		if hasDeletes {
			if err := d.applyCDCHardDeletes(ctx, targetDB, targetName, stagingDB, stagingName, opts.PrimaryKeys); err != nil {
				return err
			}
		}
	}

	optimizeSQL := fmt.Sprintf("OPTIMIZE TABLE %s.%s FINAL", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	if err := d.conn.Exec(ctx, optimizeSQL); err != nil {
		config.LogFailedQuery(optimizeSQL, err)
		config.Debug("[CLICKHOUSE CDC MERGE] OPTIMIZE FINAL completed with note: %v", err)
	}

	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	config.Debug("[CLICKHOUSE CDC MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *ClickHouseDestination) stagingHasCDCDeletes(ctx context.Context, stagingDB, stagingName string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s.%s WHERE %s = true",
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(destination.CDCDeletedColumn))
	var cnt uint64
	if err := d.conn.QueryRow(ctx, query).Scan(&cnt); err != nil {
		return false, fmt.Errorf("check ClickHouse CDC deletes in %s.%s: %w", stagingDB, stagingName, err)
	}
	return cnt > 0, nil
}

func (d *ClickHouseDestination) applyCDCHardDeletes(ctx context.Context, targetDB, targetName, stagingDB, stagingName string, primaryKeys []string) error {
	mutationSettings, err := d.cdcMutationSettings(ctx, targetDB, targetName, stagingDB, stagingName)
	if err != nil {
		return err
	}
	deleteSQL := buildCDCHardDeleteSQL(targetDB, targetName, stagingDB, stagingName, primaryKeys, d.cluster, mutationSettings)
	config.Debug("[CLICKHOUSE CDC MERGE] Executing hard DELETE: %s", deleteSQL)
	if err := d.conn.Exec(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to hard-delete ClickHouse CDC records: %w", err)
	}
	if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
		return fmt.Errorf("failed waiting for ClickHouse CDC hard-delete mutation: %w", err)
	}
	return nil
}

func (d *ClickHouseDestination) cdcMutationSettings(ctx context.Context, targetDB, targetName, stagingDB, stagingName string) (string, error) {
	replicated, err := d.isReplicatedTarget(ctx, targetDB, targetName)
	if err != nil || !replicated {
		return "", err
	}
	for _, table := range []string{
		fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName)),
		fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName)),
	} {
		syncSQL := fmt.Sprintf("SYSTEM SYNC REPLICA%s %s", onClusterClause(d.cluster), table)
		if err := d.conn.Exec(ctx, syncSQL); err != nil {
			config.LogFailedQuery(syncSQL, err)
			return "", fmt.Errorf("failed to sync replicas of %s: %w", table, err)
		}
	}
	return " SETTINGS allow_nondeterministic_mutations = 1, mutations_sync = 2", nil
}

func (d *ClickHouseDestination) isReplicatedTarget(ctx context.Context, database, tableName string) (bool, error) {
	if d.cluster == "" || !strings.EqualFold(d.engineType, "replicated_merge_tree") {
		return false, nil
	}
	var engine string
	if err := d.conn.QueryRow(ctx, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", database, tableName).Scan(&engine); err != nil {
		return false, fmt.Errorf("failed to read engine of %s.%s: %w", database, tableName, err)
	}
	return strings.HasPrefix(engine, "Replicated"), nil
}

func buildCDCHardDeleteSQL(targetDB, targetName, stagingDB, stagingName string, primaryKeys []string, cluster, mutationSettings string) string {
	target := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	staging := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	lsn := quoteIdentifier(destination.CDCLSNColumn)
	deleted := quoteIdentifier(destination.CDCDeletedColumn)

	pkExpr := pkTupleExpr(primaryKeys, "")
	targetPKExpr := pkTupleExpr(primaryKeys, "target")
	joinConds := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		qk := quoteIdentifier(pk)
		joinConds[i] = fmt.Sprintf("target.%s = s.%s", qk, qk)
	}

	return fmt.Sprintf(
		"ALTER TABLE %s%s DELETE WHERE %s IN ("+
			"SELECT %s FROM %s AS target FINAL "+
			"INNER JOIN %s AS s ON %s "+
			"WHERE target.%s = true AND s.%s = true AND target.%s = s.%s"+
			")%s",
		target, onClusterClause(cluster), pkExpr,
		targetPKExpr, target,
		staging, strings.Join(joinConds, " AND "),
		deleted, deleted, lsn, lsn,
		mutationSettings,
	)
}

func buildCDCMergeInsert(targetDB, targetName, stagingDB, stagingName string, opts destination.MergeOptions) string {
	cols := destination.DestinationColumns(opts.Columns)
	hasUnchanged := destination.HasCDCUnchangedColsColumn(opts.Columns)

	pkMap := make(map[string]bool, len(opts.PrimaryKeys))
	quotedPKs := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkMap[strings.ToLower(pk)] = true
		quotedPKs[i] = quoteIdentifier(pk)
	}
	pkList := strings.Join(quotedPKs, ", ")
	pkExpr := pkTupleExpr(opts.PrimaryKeys, "")

	joinOn := func(left, right string) string {
		conds := make([]string, len(quotedPKs))
		for i, pk := range quotedPKs {
			conds[i] = fmt.Sprintf("%s.%s = %s.%s", left, pk, right, pk)
		}
		return strings.Join(conds, " AND ")
	}
	aliased := func(alias string) string {
		parts := make([]string, len(cols))
		for i, col := range cols {
			qc := quoteIdentifier(col)
			parts[i] = alias + "." + qc + " AS " + qc
		}
		return strings.Join(parts, ", ")
	}
	unique := func(base string) string {
		for slices.ContainsFunc(opts.Columns, func(col string) bool { return strings.EqualFold(base, col) }) {
			base += "_"
		}
		return quoteIdentifier(base)
	}

	originCol := unique("__ingestr_origin")
	presentCol := unique("__ingestr_present")
	tombstoneCol := unique("__ingestr_tombstone")
	unchangedCol := unique("__ingestr_unchanged")

	lsn := quoteIdentifier(destination.CDCLSNColumn)
	deleted := quoteIdentifier(destination.CDCDeletedColumn)
	staging := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	target := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))

	targetScoped := ""
	if opts.CDCSnapshot {
		if pred := snapshotKeyRange(opts); pred != "" {
			targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL WHERE %s)", strings.Join(quoteColumns(cols), ", "), presentCol, target, pred)
		} else {
			targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL)", strings.Join(quoteColumns(cols), ", "), presentCol, target)
		}
	} else {
		targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL WHERE %s IN (SELECT %s FROM %s))",
			strings.Join(quoteColumns(cols), ", "), presentCol, target, pkExpr, pkExpr, staging)
	}

	unchangedSelect := "'[]'"
	if hasUnchanged {
		unchangedSelect = fmt.Sprintf("ifNull(s.%s, '[]')", quoteIdentifier(destination.CDCUnchangedColsColumn))
	}

	changesCTE := fmt.Sprintf(
		"changes AS ("+
			"SELECT %s, %s AS %s FROM %s AS s "+
			"LEFT JOIN %s AS t ON %s "+
			"LEFT JOIN (SELECT %s, %s, toUInt8(1) AS %s FROM %s WHERE %s = true GROUP BY %s, %s) AS __ingestr_tb ON %s AND s.%s = __ingestr_tb.%s "+
			"WHERE ifNull(t.%s, 0) = 0 OR t.%s IS NULL OR s.%s > t.%s "+
			"OR (s.%s = t.%s AND ifNull(t.%s, false) = false AND ifNull(__ingestr_tb.%s, 0) = 1)"+
			")",
		aliased("s"), unchangedSelect, unchangedCol, staging,
		targetScoped, joinOn("s", "t"),
		pkList, lsn, tombstoneCol, staging, deleted, pkList, lsn, joinOn("s", "__ingestr_tb"), lsn, lsn,
		presentCol, lsn, lsn, lsn,
		lsn, lsn, deleted, tombstoneCol,
	)

	quotedCols := strings.Join(quoteColumns(cols), ", ")
	if opts.CDCSnapshot {
		return fmt.Sprintf(
			"INSERT INTO %s (%s) WITH %s SELECT %s FROM (SELECT * FROM changes ORDER BY %s DESC, %s DESC LIMIT 1 BY %s) AS l",
			target, quotedCols, changesCTE, aliased("l"), lsn, deleted, pkList,
		)
	}

	historyCTE := fmt.Sprintf(
		"history AS ("+
			"SELECT %s, %s, toUInt8(1) AS %s FROM changes "+
			"UNION ALL "+
			"SELECT %s, '[]' AS %s, toUInt8(0) AS %s FROM (SELECT %s FROM %s FINAL WHERE %s IN (SELECT %s FROM changes)) AS t"+
			")",
		quotedCols, unchangedCol, originCol,
		aliased("t"), unchangedCol, originCol, quotedCols, target, pkExpr, pkExpr,
	)

	overallWeight := fmt.Sprintf("(ifNull(%s, ''), toUInt8(ifNull(%s, false)), %s)", lsn, deleted, originCol)
	payloadWeight := fmt.Sprintf("(ifNull(%s, ''), %s)", lsn, originCol)

	selectExprs := make([]string, len(cols))
	for i, col := range cols {
		qc := quoteIdentifier(col)
		switch {
		case pkMap[strings.ToLower(col)]:
			selectExprs[i] = qc
		case destination.IsCDCColumn(col):
			selectExprs[i] = fmt.Sprintf("argMax(tuple(%s), %s).1", qc, overallWeight)
		default:
			cond := fmt.Sprintf("(%s = 0 OR ifNull(%s, false) = false)", originCol, deleted)
			if hasUnchanged {
				cond = fmt.Sprintf("(%s = 0 OR (ifNull(%s, false) = false AND NOT has(arrayMap(x -> lowerUTF8(x), JSONExtract(ifNull(%s, '[]'), 'Array(String)')), %s)))",
					originCol, deleted, unchangedCol, sqlString(strings.ToLower(col)))
			}
			selectExprs[i] = fmt.Sprintf("argMaxIf(tuple(%s), %s, %s).1", qc, payloadWeight, cond)
		}
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) WITH %s, %s SELECT %s FROM history GROUP BY %s HAVING max(%s) = 1 AND max(if(%s = 0 OR ifNull(%s, false) = false, 1, 0)) = 1",
		target, quotedCols, changesCTE, historyCTE, strings.Join(selectExprs, ", "),
		pkList, originCol, originCol, deleted,
	)
}

func pkTupleExpr(primaryKeys []string, alias string) string {
	quoted := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		if alias != "" {
			quoted[i] = alias + "." + quoteIdentifier(pk)
		} else {
			quoted[i] = quoteIdentifier(pk)
		}
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func snapshotKeyRange(opts destination.MergeOptions) string {
	bounds := opts.CDCSnapshotRange
	if !opts.CDCSnapshot || bounds == nil || opts.Schema == nil || len(opts.PrimaryKeys) == 0 || len(bounds.Last) != len(opts.PrimaryKeys) || bounds.After != nil && len(bounds.After) != len(opts.PrimaryKeys) {
		return ""
	}
	last, after := make([]string, len(opts.PrimaryKeys)), make([]string, len(bounds.After))
	ordering := 0
	for i, key := range opts.PrimaryKeys {
		index := slices.IndexFunc(opts.Schema.Columns, func(col schema.Column) bool { return strings.EqualFold(col.Name, key) })
		if index < 0 {
			return ""
		}
		col := opts.Schema.Columns[index]
		bits := 0
		switch col.DataType {
		case schema.TypeInt8:
			bits = 8
		case schema.TypeInt16:
			bits = 16
		case schema.TypeInt32:
			bits = 32
		case schema.TypeInt64:
			bits = 64
		}
		if bits == 0 || col.Unsigned {
			return ""
		}
		upper, err := strconv.ParseInt(bounds.Last[i], 10, bits)
		if err != nil {
			return ""
		}
		last[i] = strconv.FormatInt(upper, 10)
		if bounds.After != nil {
			lower, err := strconv.ParseInt(bounds.After[i], 10, bits)
			if err != nil {
				return ""
			}
			after[i] = strconv.FormatInt(lower, 10)
			if ordering == 0 {
				if lower < upper {
					ordering = -1
				} else if lower > upper {
					ordering = 1
				}
			}
		}
	}
	upper := snapshotKeyComparison(opts.PrimaryKeys, last, true)
	if bounds.After == nil {
		return upper
	}
	if ordering != -1 {
		return ""
	}
	return snapshotKeyComparison(opts.PrimaryKeys, after, false) + " AND " + upper
}

func snapshotKeyComparison(keys, values []string, upper bool) string {
	terms := make([]string, len(keys))
	equal := make([]string, 0, len(keys))
	for i, key := range keys {
		operator := ">"
		if upper {
			operator = "<"
			if i == len(keys)-1 {
				operator = "<="
			}
		}
		comparison := quoteIdentifier(key) + " " + operator + " " + values[i]
		if len(equal) > 0 {
			comparison = "(" + strings.Join(equal, " AND ") + " AND " + comparison + ")"
		}
		terms[i] = comparison
		equal = append(equal, quoteIdentifier(key)+" = "+values[i])
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}

func (d *ClickHouseDestination) tableHasColumn(ctx context.Context, database, tableName, colName string) (bool, error) {
	var count uint64
	if err := d.conn.QueryRow(
		ctx,
		"SELECT count() FROM system.columns WHERE database = ? AND table = ? AND name = ?",
		database, tableName, colName,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect ClickHouse column %s on %s.%s: %w", colName, database, tableName, err)
	}
	return count > 0, nil
}

func (d *ClickHouseDestination) validatePrimaryKey(ctx context.Context, role, table string, expected []string) error {
	database, tableName := d.parseTableName(table)
	var engine, primaryKey, sortingKey string
	err := d.conn.QueryRow(
		ctx,
		"SELECT engine, primary_key, sorting_key FROM system.tables WHERE database = ? AND name = ?",
		database, tableName,
	).Scan(&engine, &primaryKey, &sortingKey)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("ClickHouse %s %s does not exist", role, table)
	}
	if err != nil {
		return fmt.Errorf("read ClickHouse %s metadata for %s: %w", role, table, err)
	}

	actual := parseClickHouseKeyList(primaryKey)
	if len(actual) == 0 {
		actual = parseClickHouseKeyList(sortingKey)
	}

	normalize := func(cols []string) []string {
		out := make([]string, len(cols))
		for i, col := range cols {
			out[i] = strings.ToLower(col)
		}
		slices.Sort(out)
		return out
	}

	if !strings.Contains(engine, "ReplacingMergeTree") || !slices.Equal(normalize(actual), normalize(expected)) {
		return fmt.Errorf("ClickHouse %s %s requires ReplacingMergeTree with primary key (%s); found %s (%s). If the key changed, rerun with --full-refresh to rebuild the table",
			role, table, strings.Join(expected, ", "), engine, strings.Join(actual, ", "))
	}
	return nil
}

func parseClickHouseKeyList(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "tuple()" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inBacktick := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		switch {
		case ch == '`':
			if inBacktick && i+1 < len(expr) && expr[i+1] == '`' {
				cur.WriteByte('`')
				i++
			} else {
				inBacktick = !inBacktick
			}
		case ch == ',' && !inBacktick:
			if part := strings.TrimSpace(cur.String()); part != "" {
				out = append(out, part)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if part := strings.TrimSpace(cur.String()); part != "" {
		out = append(out, part)
	}
	return out
}

func isMissingTableOrDB(err error) bool {
	if err == nil {
		return false
	}
	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) && (chErr.Code == 60 || chErr.Code == 81) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown_table") ||
		strings.Contains(msg, "unknown_database") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "unknown database") ||
		strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist")
}

func isTableExistsError(err error) bool {
	if err == nil {
		return false
	}
	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) && chErr.Code == 57 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "table_already_exists") || strings.Contains(msg, "already exists")
}

func sqlString(value string) string {
	return "'" + escapeClickHouseString(value) + "'"
}
