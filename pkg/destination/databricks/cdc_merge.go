package databricks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *DatabricksDestination) SupportsCDCMerge() bool         { return true }
func (d *DatabricksDestination) SupportsCDCHardDelete() bool    { return true }
func (d *DatabricksDestination) SupportsCDCUnchangedCols() bool { return true }
func (d *DatabricksDestination) RequiresSerializedCDCRuns() bool {
	return true
}

func (d *DatabricksDestination) mergeCDC(ctx context.Context, opts destination.MergeOptions) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("databricks CDC merge requires at least one primary key")
	}
	if opts.CDCExpectedIncarnation != "" {
		return d.executeFencedMerges(ctx, []destination.CDCAtomicTableMerge{{Options: opts}})
	}

	mergeSQL := d.buildCDCMergeSQL(opts)
	config.Debug("[DATABRICKS] Executing CDC MERGE: %s", mergeSQL)

	if err := d.executeStatement(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute CDC merge: %w", err)
	}
	return nil
}

func (d *DatabricksDestination) buildCDCMergeSQL(opts destination.MergeOptions) string {
	return d.buildCDCMergeSQLForTarget(opts, d.quoteTableRef(opts.TargetTable))
}

func (d *DatabricksDestination) buildCDCMergeSQLForTarget(opts destination.MergeOptions, targetFull string) string {
	allColumns := opts.Columns
	destColumns := destination.DestinationColumns(allColumns)
	stagingFull := d.quoteTableRef(opts.StagingTable)

	hardDelete := opts.CDCHardDelete && !opts.CDCSnapshot
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(allColumns)

	pkMap := make(map[string]bool, len(opts.PrimaryKeys))
	onConditions := make([]string, len(opts.PrimaryKeys))
	quotedPKs := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkMap[strings.ToLower(pk)] = true
		qPK := quoteIdentifier(pk)
		quotedPKs[i] = qPK
		onConditions[i] = fmt.Sprintf("target.%s = source.%s", qPK, qPK)
	}
	primaryKeyOnClause := strings.Join(onConditions, " AND ")

	usedNames := make(map[string]struct{}, len(allColumns)+8)
	for _, col := range allColumns {
		usedNames[strings.ToLower(col)] = struct{}{}
	}
	uniqueInternalName := func(base string) string {
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := usedNames[strings.ToLower(candidate)]; !exists {
				usedNames[strings.ToLower(candidate)] = struct{}{}
				return candidate
			}
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
	}

	dedupRowNumber := quoteIdentifier(uniqueInternalName("__bruin_dedup_rn"))
	hasActiveColumn := quoteIdentifier(uniqueInternalName("__ingestr_has_active"))
	activeLSNColumn := quoteIdentifier(uniqueInternalName("__ingestr_active_lsn"))

	cdcLSNName := actualColumnName(allColumns, destination.CDCLSNColumn)
	cdcDeletedName := actualColumnName(allColumns, destination.CDCDeletedColumn)
	cdcSyncedAtName := actualColumnName(allColumns, destination.CDCSyncedAtColumn)
	cdcUnchangedName := actualColumnName(allColumns, destination.CDCUnchangedColsColumn)

	cdcLSN := quoteIdentifier(cdcLSNName)
	cdcDeleted := quoteIdentifier(cdcDeletedName)
	cdcSyncedAt := quoteIdentifier(cdcSyncedAtName)
	cdcUnchanged := quoteIdentifier(cdcUnchangedName)

	quoteActual := func(logical string) string {
		return quoteIdentifier(actualColumnName(allColumns, logical))
	}

	unchangedFlagByCol := make(map[string]string)
	if hasUnchangedCols {
		for _, col := range destColumns {
			if pkMap[strings.ToLower(col)] || destination.IsCDCMetaColumn(col) {
				continue
			}
			unchangedFlagByCol[strings.ToLower(col)] = quoteIdentifier(uniqueInternalName("__ingestr_unchanged_" + strings.ToLower(col)))
		}
	}

	var updateSets []string
	for _, col := range destColumns {
		if pkMap[strings.ToLower(col)] {
			continue
		}
		qCol := quoteIdentifier(col)
		if hasUnchangedCols && !destination.IsCDCMetaColumn(col) {
			flagCol := unchangedFlagByCol[strings.ToLower(col)]
			updateSets = append(updateSets, fmt.Sprintf(
				"target.%s = CASE WHEN source.%s THEN target.%s ELSE source.%s END",
				qCol, flagCol, qCol, qCol,
			))
		} else {
			updateSets = append(updateSets, fmt.Sprintf("target.%s = source.%s", qCol, qCol))
		}
	}

	stagingQuoted := quoteColumns(allColumns)
	destQuoted := quoteColumns(destColumns)
	destSourceCols := make([]string, len(destColumns))
	for i, col := range destColumns {
		destSourceCols[i] = "source." + quoteIdentifier(col)
	}

	var mergeSQL strings.Builder
	fmt.Fprintf(&mergeSQL, "MERGE INTO %s AS target\n", targetFull)

	dedup := func(where, orderBy string) string {
		return fmt.Sprintf(
			"(SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s%s) AS _numbered WHERE %s = 1)",
			strings.Join(stagingQuoted, ", "),
			strings.Join(stagingQuoted, ", "),
			strings.Join(quotedPKs, ", "),
			orderBy,
			dedupRowNumber,
			stagingFull,
			where,
			dedupRowNumber,
		)
	}

	buildActiveAggSource := func() string {
		actSelects := make([]string, 0, len(quotedPKs)+1+len(destColumns)*2)
		actSelects = append(actSelects, quotedPKs...)
		actSelects = append(actSelects, fmt.Sprintf("MAX(%s) AS %s", cdcLSN, cdcLSN))
		for _, col := range destColumns {
			if pkMap[strings.ToLower(col)] || strings.EqualFold(col, cdcLSNName) {
				continue
			}
			qCol := quoteIdentifier(col)
			if destination.IsCDCMetaColumn(col) {
				actSelects = append(actSelects, fmt.Sprintf("MAX_BY(%s, %s) AS %s", qCol, cdcLSN, qCol))
				continue
			}
			materialized := fmt.Sprintf(
				"NOT COALESCE(ARRAY_CONTAINS(FROM_JSON(LOWER(COALESCE(%s, '[]')), 'ARRAY<STRING>'), %s), TRUE)",
				cdcUnchanged, sqlString(strings.ToLower(col)),
			)
			actSelects = append(actSelects, fmt.Sprintf(
				"MAX_BY(%s, CASE WHEN %s THEN %s END) AS %s",
				qCol, materialized, cdcLSN, qCol,
			))
			flagCol := unchangedFlagByCol[strings.ToLower(col)]
			actSelects = append(actSelects, fmt.Sprintf(
				"(MAX(CASE WHEN %s THEN 1 ELSE 0 END) = 0) AS %s",
				materialized, flagCol,
			))
		}
		return fmt.Sprintf(
			"(SELECT %s FROM %s WHERE %s = false GROUP BY %s)",
			strings.Join(actSelects, ", "),
			stagingFull,
			cdcDeleted,
			strings.Join(quotedPKs, ", "),
		)
	}

	buildComposedSource := func(includeActiveMeta bool) string {
		laActJoin := make([]string, len(opts.PrimaryKeys))
		for i, pk := range opts.PrimaryKeys {
			qPK := quoteIdentifier(pk)
			laActJoin[i] = fmt.Sprintf("(la.%s = act.%s OR (la.%s IS NULL AND act.%s IS NULL))", qPK, qPK, qPK, qPK)
		}

		selectCols := make([]string, 0, len(destColumns)+2+len(unchangedFlagByCol))
		for _, col := range destColumns {
			alias := "act"
			if pkMap[strings.ToLower(col)] || destination.IsCDCMetaColumn(col) {
				alias = "la"
			}
			selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteIdentifier(col)))
		}
		if includeActiveMeta {
			selectCols = append(selectCols, fmt.Sprintf("act.%s IS NOT NULL AS %s", cdcLSN, hasActiveColumn))
			selectCols = append(selectCols, fmt.Sprintf("act.%s AS %s", cdcLSN, activeLSNColumn))
		}
		if hasUnchangedCols {
			for _, col := range destColumns {
				if flagCol, ok := unchangedFlagByCol[strings.ToLower(col)]; ok {
					selectCols = append(selectCols, fmt.Sprintf("COALESCE(act.%s, TRUE) AS %s", flagCol, flagCol))
				}
			}
		}

		actSubquery := dedup(fmt.Sprintf(" WHERE %s = false", cdcDeleted), cdcLSN+" DESC")
		if hasUnchangedCols {
			actSubquery = buildActiveAggSource()
		}

		return fmt.Sprintf(
			"(SELECT %s FROM %s AS la LEFT JOIN %s AS act ON %s)",
			strings.Join(selectCols, ", "),
			dedup("", destination.CDCLatestOverallOrderBy(quoteActual)),
			actSubquery,
			strings.Join(laActJoin, " AND "),
		)
	}

	if hardDelete && !hasUnchangedCols {
		fmt.Fprintf(&mergeSQL, "USING %s AS source\n", dedup("", destination.CDCLatestOverallOrderBy(quoteActual)))
	} else {
		fmt.Fprintf(&mergeSQL, "USING %s AS source\n", buildComposedSource(!hardDelete))
	}
	fmt.Fprintf(&mergeSQL, "ON %s\n", primaryKeyOnClause)

	if hardDelete {
		matchedGuard := func(condition string) string {
			if strings.TrimSpace(opts.IncrementalPredicate) != "" {
				return "(" + opts.IncrementalPredicate + ") AND " + condition
			}
			return condition
		}
		fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND %s THEN\n", matchedGuard(
			fmt.Sprintf("source.%s = true AND (target.%s IS NULL OR source.%s >= target.%s)", cdcDeleted, cdcLSN, cdcLSN, cdcLSN),
		))
		mergeSQL.WriteString("  DELETE\n")
		if len(updateSets) > 0 {
			fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND %s THEN\n", matchedGuard(
				fmt.Sprintf("source.%s = false AND (target.%s IS NULL OR source.%s > target.%s)", cdcDeleted, cdcLSN, cdcLSN, cdcLSN),
			))
			fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
		}
		fmt.Fprintf(&mergeSQL, "WHEN NOT MATCHED AND source.%s = false THEN\n", cdcDeleted)
		fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(destQuoted, ", "))
		fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(destSourceCols, ", "))
		return mergeSQL.String()
	}

	hasRowData := fmt.Sprintf(
		"(source.%s = false OR (source.%s AND (target.%s IS NULL OR source.%s >= target.%s)))",
		cdcDeleted, hasActiveColumn, cdcLSN, activeLSNColumn, cdcLSN,
	)
	newerChange := fmt.Sprintf(
		"(target.%s IS NULL OR source.%s > target.%s OR (source.%s = target.%s AND source.%s = true AND COALESCE(target.%s, false) = false))",
		cdcLSN, cdcLSN, cdcLSN, cdcLSN, cdcLSN, cdcDeleted, cdcDeleted,
	)
	matchedCondition := newerChange
	if strings.TrimSpace(opts.IncrementalPredicate) != "" {
		matchedCondition = fmt.Sprintf("(%s) AND %s", opts.IncrementalPredicate, matchedCondition)
	}

	if len(updateSets) > 0 {
		fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND %s AND %s THEN\n", matchedCondition, hasRowData)
		fmt.Fprintf(&mergeSQL, "  UPDATE SET %s\n", strings.Join(updateSets, ", "))
	}

	fmt.Fprintf(&mergeSQL, "WHEN MATCHED AND %s AND source.%s = true THEN\n", matchedCondition, cdcDeleted)
	fmt.Fprintf(&mergeSQL, "  UPDATE SET target.%s = true, target.%s = source.%s, target.%s = source.%s\n",
		cdcDeleted, cdcLSN, cdcLSN, cdcSyncedAt, cdcSyncedAt)

	mergeSQL.WriteString("WHEN NOT MATCHED THEN\n")
	fmt.Fprintf(&mergeSQL, "  INSERT (%s)\n", strings.Join(destQuoted, ", "))
	fmt.Fprintf(&mergeSQL, "  VALUES (%s)", strings.Join(destSourceCols, ", "))
	return mergeSQL.String()
}

func actualColumnName(columns []string, name string) string {
	for _, col := range columns {
		if strings.EqualFold(col, name) {
			return col
		}
	}
	return name
}
