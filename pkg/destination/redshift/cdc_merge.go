package redshift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *RedshiftDestination) MergeTable(ctx context.Context, opts destination.MergeOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("merge requires at least one primary key")
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}
	return d.withTx(ctx, func(tx redshiftTx) error {
		if err := d.mergeTableInTx(ctx, tx, opts, true); err != nil {
			return err
		}
		return source.ConnectorLeaseLoss(ctx)
	})
}

func (d *RedshiftDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(merges) == 0 {
		return nil
	}
	db, err := d.requireDB()
	if err != nil {
		return err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return err
	}

	return d.withTx(ctx, func(tx redshiftTx) error {
		type lockTarget struct {
			rawTable    string
			expectedInc string
		}
		byCanonical := make(map[string]lockTarget, len(merges))
		keys := make([]string, 0, len(merges))
		for _, m := range merges {
			if len(m.Options.PrimaryKeys) == 0 {
				return errors.New("merge requires at least one primary key")
			}
			s, t, err := d.resolveSchemaTable(ctx, tx, m.Options.TargetTable)
			if err != nil {
				return err
			}
			canon := s + "." + t
			if existing, ok := byCanonical[canon]; ok {
				if existing.expectedInc != "" && m.Options.CDCExpectedIncarnation != "" && existing.expectedInc != m.Options.CDCExpectedIncarnation {
					return fmt.Errorf("conflicting expected incarnations for Redshift CDC target %q", m.Options.TargetTable)
				}
				if existing.expectedInc == "" {
					existing.expectedInc = m.Options.CDCExpectedIncarnation
					byCanonical[canon] = existing
				}
				continue
			}
			byCanonical[canon] = lockTarget{
				rawTable:    m.Options.TargetTable,
				expectedInc: m.Options.CDCExpectedIncarnation,
			}
			keys = append(keys, canon)
		}
		sort.Strings(keys)

		for _, canon := range keys {
			lt := byCanonical[canon]
			if _, _, _, err := d.lockAndValidateCDCIncarnation(ctx, tx, lt.rawTable, lt.expectedInc); err != nil {
				return err
			}
		}

		for _, m := range merges {
			if m.Truncate {
				s, t, err := d.resolveSchemaTable(ctx, tx, m.Options.TargetTable)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "DELETE FROM "+quoteTable(s, t)); err != nil {
					return fmt.Errorf("failed to clear target table %s before atomic CDC merge: %w", m.Options.TargetTable, err)
				}
			}
			if err := d.mergeTableInTx(ctx, tx, m.Options, false); err != nil {
				return err
			}
		}
		return source.ConnectorLeaseLoss(ctx)
	})
}

func (d *RedshiftDestination) mergeTableInTx(ctx context.Context, tx redshiftTx, opts destination.MergeOptions, lockAndVerify bool) error {
	startMerge := time.Now()

	var targetRef string
	if lockAndVerify {
		var err error
		_, _, targetRef, err = d.lockAndValidateCDCIncarnation(ctx, tx, opts.TargetTable, opts.CDCExpectedIncarnation)
		if err != nil {
			return err
		}
	} else {
		ts, tt, err := d.resolveSchemaTable(ctx, tx, opts.TargetTable)
		if err != nil {
			return err
		}
		targetRef = quoteTable(ts, tt)
	}

	ss, st, err := d.resolveSchemaTable(ctx, tx, opts.StagingTable)
	if err != nil {
		return err
	}
	stagingRef := quoteTable(ss, st)

	if containsColFold(opts.Columns, destination.CDCDeletedColumn) {
		statements := buildCDCMergeStatements(stagingRef, targetRef, opts)
		for _, stmt := range statements {
			config.Debug("[REDSHIFT CDC MERGE] Executing: %s", stmt)
			if _, err := tx.Exec(ctx, stmt); err != nil {
				config.LogFailedQuery(stmt, err)
				return fmt.Errorf("failed to execute Redshift CDC merge statement: %w", err)
			}
		}
		config.Debug("[MERGE] CDC merge completed in %v", time.Since(startMerge))
		return nil
	}

	if predicate := strings.TrimSpace(opts.IncrementalPredicate); predicate != "" {
		statements := buildPredicateMergeStatements(stagingRef, targetRef, opts.PrimaryKeys, opts.Columns, predicate)
		for _, stmt := range statements {
			config.Debug("[REDSHIFT MERGE] Executing predicate merge statement: %s", stmt)
			if _, err := tx.Exec(ctx, stmt); err != nil {
				config.LogFailedQuery(stmt, err)
				return fmt.Errorf("failed to execute predicate merge statement: %w", err)
			}
		}
		config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
		return nil
	}

	mergeSQL := d.buildMergeSQL(stagingRef, targetRef, opts.PrimaryKeys, opts.Columns)
	config.Debug("[MERGE] Executing: %s", mergeSQL)
	if _, err := tx.Exec(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute merge: %w", err)
	}
	config.Debug("[MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func containsColFold(cols []string, target string) bool {
	for _, col := range cols {
		if strings.EqualFold(col, target) {
			return true
		}
	}
	return false
}

func actualColName(cols []string, target string) string {
	for _, col := range cols {
		if strings.EqualFold(col, target) {
			return col
		}
	}
	return target
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func buildJoinCondition(primaryKeys []string, leftAlias, rightAlias string) string {
	parts := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		qPK := destination.QuoteIdentifier(pk)
		parts[i] = fmt.Sprintf("%s.%s = %s.%s", leftAlias, qPK, rightAlias, qPK)
	}
	return strings.Join(parts, " AND ")
}

func buildCDCMergeStatements(stagingRef, targetRef string, opts destination.MergeOptions) []string {
	primaryKeys := opts.PrimaryKeys
	allColumns := opts.Columns
	destCols := destination.DestinationColumns(allColumns)

	lsnName := actualColName(allColumns, destination.CDCLSNColumn)
	deletedName := actualColName(allColumns, destination.CDCDeletedColumn)
	syncedAtName := actualColName(allColumns, destination.CDCSyncedAtColumn)
	unchangedName := actualColName(allColumns, destination.CDCUnchangedColsColumn)

	qLSN := destination.QuoteIdentifier(lsnName)
	qDeleted := destination.QuoteIdentifier(deletedName)
	qSyncedAt := destination.QuoteIdentifier(syncedAtName)
	qUnchanged := destination.QuoteIdentifier(unchangedName)
	hasUnchanged := containsColFold(allColumns, destination.CDCUnchangedColsColumn)

	pkSet := make(map[string]struct{}, len(primaryKeys))
	quotedPKs := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		pkSet[strings.ToLower(pk)] = struct{}{}
		quotedPKs[i] = destination.QuoteIdentifier(pk)
	}
	pkList := strings.Join(quotedPKs, ", ")

	allQuoted := make([]string, len(allColumns))
	for i, col := range allColumns {
		allQuoted[i] = destination.QuoteIdentifier(col)
	}
	destQuoted := make([]string, len(destCols))
	for i, col := range destCols {
		destQuoted[i] = destination.QuoteIdentifier(col)
	}

	var payloadCols []string
	for _, col := range destCols {
		if _, isPK := pkSet[strings.ToLower(col)]; !isPK && !destination.IsCDCMetaColumn(col) {
			payloadCols = append(payloadCols, col)
		}
	}

	orderOverall := destination.CDCLatestOverallOrderBy(destination.QuoteIdentifier)
	latestAllCTE := fmt.Sprintf(
		`latest_all AS (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS "__ingestr_rn" FROM %s) AS "_ingestr_ranked_all" WHERE "__ingestr_rn" = 1)`,
		strings.Join(allQuoted, ", "),
		strings.Join(allQuoted, ", "),
		pkList,
		orderOverall,
		stagingRef,
	)
	if opts.StagingPrimaryKeysUnique && !hasUnchanged {
		latestAllCTE = fmt.Sprintf(
			`latest_all AS (SELECT %s FROM %s)`,
			strings.Join(allQuoted, ", "),
			stagingRef,
		)
	}

	latestActiveCTE := fmt.Sprintf(
		`latest_active AS (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS "__ingestr_rn" FROM %s WHERE %s = false) AS "_ingestr_ranked_active" WHERE "__ingestr_rn" = 1)`,
		strings.Join(allQuoted, ", "),
		strings.Join(allQuoted, ", "),
		pkList,
		qLSN,
		stagingRef,
		qDeleted,
	)

	var valueCTEs []string
	var valueJoinsLA []string
	var valueJoinsLatest []string
	valueAliasByCol := make(map[string]string, len(payloadCols))
	if hasUnchanged {
		for i, col := range payloadCols {
			alias := fmt.Sprintf("__ingestr_v_%d", i)
			valueAliasByCol[strings.ToLower(col)] = alias
			qCol := destination.QuoteIdentifier(col)
			memberBytes, _ := json.Marshal(col)
			member := string(memberBytes)
			bracketPat := sqlStringLiteral("[" + member)
			commaPat := sqlStringLiteral("," + member)
			cte := fmt.Sprintf(
				`%s AS (SELECT %s, %s FROM (SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS "__ingestr_rn" FROM %s WHERE %s = false AND CHARINDEX(%s, COALESCE(%s, '')) = 0 AND CHARINDEX(%s, COALESCE(%s, '')) = 0) AS "_ingestr_col_%d" WHERE "__ingestr_rn" = 1)`,
				destination.QuoteIdentifier(alias),
				pkList, qCol,
				pkList, qCol, pkList, qLSN,
				stagingRef, qDeleted,
				bracketPat, qUnchanged,
				commaPat, qUnchanged,
				i,
			)
			valueCTEs = append(valueCTEs, cte)
			qAlias := destination.QuoteIdentifier(alias)
			valueJoinsLA = append(valueJoinsLA, fmt.Sprintf("LEFT JOIN %s ON %s", qAlias, buildJoinCondition(primaryKeys, "la", qAlias)))
			valueJoinsLatest = append(valueJoinsLatest, fmt.Sprintf("LEFT JOIN %s ON %s", qAlias, buildJoinCondition(primaryKeys, "latest", qAlias)))
		}
	}

	firstPKQuoted := quotedPKs[0]
	buildActiveUpdateSets := func(srcAlias string) string {
		var sets []string
		for _, col := range destCols {
			if _, isPK := pkSet[strings.ToLower(col)]; isPK {
				continue
			}
			qCol := destination.QuoteIdentifier(col)
			if strings.EqualFold(col, destination.CDCDeletedColumn) {
				sets = append(sets, fmt.Sprintf("%s = false", qCol))
				continue
			}
			if destination.IsCDCMetaColumn(col) {
				sets = append(sets, fmt.Sprintf("%s = %s.%s", qCol, srcAlias, qCol))
				continue
			}
			if hasUnchanged {
				qValAlias := destination.QuoteIdentifier(valueAliasByCol[strings.ToLower(col)])
				sets = append(sets, fmt.Sprintf(
					"%s = CASE WHEN %s.%s IS NOT NULL THEN %s.%s ELSE %s.%s END",
					qCol, qValAlias, firstPKQuoted, qValAlias, qCol, targetRef, qCol,
				))
			} else {
				sets = append(sets, fmt.Sprintf("%s = %s.%s", qCol, srcAlias, qCol))
			}
		}
		return strings.Join(sets, ", ")
	}

	buildInsertValues := func(srcAlias string, payloadFromActive bool) string {
		vals := make([]string, len(destCols))
		for i, col := range destCols {
			qCol := destination.QuoteIdentifier(col)
			if _, isPK := pkSet[strings.ToLower(col)]; isPK || destination.IsCDCMetaColumn(col) {
				vals[i] = srcAlias + "." + qCol
				continue
			}
			if hasUnchanged {
				qValAlias := destination.QuoteIdentifier(valueAliasByCol[strings.ToLower(col)])
				vals[i] = qValAlias + "." + qCol
			} else if payloadFromActive {
				vals[i] = "la." + qCol
			} else {
				vals[i] = srcAlias + "." + qCol
			}
		}
		return strings.Join(vals, ", ")
	}

	predicate := strings.TrimSpace(opts.IncrementalPredicate)
	predicateJoinFor := func(baseCond string) (extraFrom, whereCond string) {
		if predicate == "" {
			return "", baseCond
		}
		return ", " + targetRef + " AS target", fmt.Sprintf(
			"%s AND %s AND (%s)",
			buildJoinCondition(primaryKeys, targetRef, "target"),
			baseCond,
			predicate,
		)
	}

	joinTargetLA := buildJoinCondition(primaryKeys, targetRef, "la")
	joinTargetLatest := buildJoinCondition(primaryKeys, targetRef, "latest")
	lsnTargetLTE := func(srcAlias string) string {
		return fmt.Sprintf(
			"(%s.%s IS NULL OR %s.%s > %s.%s OR (%s.%s = %s.%s AND COALESCE(%s.%s, false) = false))",
			targetRef, qLSN,
			srcAlias, qLSN, targetRef, qLSN,
			srcAlias, qLSN, targetRef, qLSN, targetRef, qDeleted,
		)
	}

	if opts.CDCHardDelete {
		ctes := []string{latestAllCTE}
		ctes = append(ctes, valueCTEs...)
		cteBlock := "WITH " + strings.Join(ctes, ", ")

		fromSuffix, updateJoin := predicateJoinFor(joinTargetLA)
		updateSQL := fmt.Sprintf(
			`%s UPDATE %s SET %s FROM latest_all AS la %s%s WHERE %s AND la.%s = false AND %s`,
			cteBlock,
			targetRef,
			buildActiveUpdateSets("la"),
			strings.Join(valueJoinsLA, " "),
			fromSuffix,
			updateJoin,
			qDeleted,
			lsnTargetLTE("la"),
		)

		delSuffix, delJoin := predicateJoinFor(joinTargetLatest)
		deleteSQL := fmt.Sprintf(
			`WITH %s DELETE FROM %s USING latest_all AS latest%s WHERE %s AND latest.%s = true AND (%s.%s IS NULL OR latest.%s >= %s.%s)`,
			latestAllCTE,
			targetRef,
			delSuffix,
			delJoin,
			qDeleted,
			targetRef, qLSN,
			qLSN, targetRef, qLSN,
		)

		antiJoin := buildJoinCondition(primaryKeys, "target", "la")
		insertSQL := fmt.Sprintf(
			`INSERT INTO %s (%s) %s SELECT %s FROM latest_all AS la %s WHERE la.%s = false AND NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
			targetRef,
			strings.Join(destQuoted, ", "),
			cteBlock,
			buildInsertValues("la", false),
			strings.Join(valueJoinsLA, " "),
			qDeleted,
			targetRef,
			antiJoin,
		)

		return []string{updateSQL, deleteSQL, insertSQL}
	}

	// Soft-delete CDC merge:
	// 1. Upsert latest active rows (when newer than target, or on LSN tie where target is active and batch also has a tied delete).
	// 2. Mark matched rows deleted when latest overall is _cdc_deleted = true.
	// 3. Insert unmatched rows (active rows and delete tombstones) so stale replays cannot resurrect deleted keys.
	softCTEs := []string{latestAllCTE, latestActiveCTE}
	softCTEs = append(softCTEs, valueCTEs...)
	softCTEBlock := "WITH " + strings.Join(softCTEs, ", ")

	fromSuffix, activeJoin := predicateJoinFor(joinTargetLA)
	activeCondition := fmt.Sprintf(
		"(%s.%s IS NULL OR la.%s > %s.%s OR (la.%s = %s.%s AND COALESCE(%s.%s, false) = false AND EXISTS (SELECT 1 FROM latest_all AS latest WHERE %s AND latest.%s = la.%s AND latest.%s = true)))",
		targetRef, qLSN,
		qLSN, targetRef, qLSN,
		qLSN, targetRef, qLSN, targetRef, qDeleted,
		buildJoinCondition(primaryKeys, "la", "latest"), qLSN, qLSN, qDeleted,
	)
	upsertActiveSQL := fmt.Sprintf(
		`%s UPDATE %s SET %s FROM latest_active AS la %s%s WHERE %s AND %s`,
		softCTEBlock,
		targetRef,
		buildActiveUpdateSets("la"),
		strings.Join(valueJoinsLA, " "),
		fromSuffix,
		activeJoin,
		activeCondition,
	)

	delSuffix, deleteJoin := predicateJoinFor(joinTargetLatest)
	updateDeletedSQL := fmt.Sprintf(
		`WITH %s UPDATE %s SET %s = true, %s = latest.%s, %s = latest.%s FROM latest_all AS latest%s WHERE %s AND latest.%s = true AND %s`,
		latestAllCTE,
		targetRef,
		qDeleted, qLSN, qLSN, qSyncedAt, qSyncedAt,
		delSuffix,
		deleteJoin,
		qDeleted,
		lsnTargetLTE("latest"),
	)

	antiJoin := buildJoinCondition(primaryKeys, "target", "latest")
	insertUnmatchedSQL := fmt.Sprintf(
		`INSERT INTO %s (%s) %s SELECT %s FROM latest_all AS latest LEFT JOIN latest_active AS la ON %s %s WHERE NOT EXISTS (SELECT 1 FROM %s AS target WHERE %s)`,
		targetRef,
		strings.Join(destQuoted, ", "),
		softCTEBlock,
		buildInsertValues("latest", true),
		buildJoinCondition(primaryKeys, "latest", "la"),
		strings.Join(valueJoinsLatest, " "),
		targetRef,
		antiJoin,
	)

	return []string{upsertActiveSQL, updateDeletedSQL, insertUnmatchedSQL}
}
