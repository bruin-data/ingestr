package trino

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *TrinoDestination) SupportsCDCMerge() bool            { return true }
func (d *TrinoDestination) SupportsCDCHardDelete() bool       { return true }
func (d *TrinoDestination) SupportsCDCUnchangedCols() bool    { return true }
func (d *TrinoDestination) SupportsCDCConditionalMerge() bool { return true }

func (d *TrinoDestination) mergeCDC(ctx context.Context, opts destination.MergeOptions) error {
	if d.db == nil {
		return errors.New("trino destination is not connected")
	}
	if len(opts.PrimaryKeys) == 0 {
		return errors.New("trino CDC merge requires primary keys")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	startMerge := time.Now()
	mergeSQL := d.buildCDCMergeSQL(opts)
	config.Debug("[TRINO CDC MERGE] Executing: %s", mergeSQL)

	if _, err := d.db.ExecContext(ctx, mergeSQL); err != nil {
		config.LogFailedQuery(mergeSQL, err)
		return fmt.Errorf("failed to execute Trino CDC merge: %w", err)
	}
	if err := d.checkCDCIncarnation(ctx, opts.TargetTable, opts.CDCExpectedIncarnation); err != nil {
		return err
	}

	config.Debug("[TRINO CDC MERGE] Completed in %v", time.Since(startMerge))
	return nil
}

func (d *TrinoDestination) buildCDCMergeSQL(opts destination.MergeOptions) string {
	stagingCatalog, stagingSchema, stagingName := d.parseTableName(opts.StagingTable)
	targetCatalog, targetSchema, targetName := d.parseTableName(opts.TargetTable)
	stagingFQN := qualifyTrinoTable(stagingCatalog, stagingSchema, stagingName)
	targetFQN := qualifyTrinoTable(targetCatalog, targetSchema, targetName)

	allColumns := opts.Columns
	destColumns := destination.DestinationColumns(allColumns)
	hasUnchangedCols := destination.HasCDCUnchangedColsColumn(allColumns)

	pkMap := make(map[string]bool, len(opts.PrimaryKeys))
	onConditions := make([]string, len(opts.PrimaryKeys))
	pkPartition := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkMap[strings.ToLower(pk)] = true
		q := quoteIdentifier(pk)
		pkPartition[i] = q
		onConditions[i] = fmt.Sprintf("t.%s = s.%s", q, q)
	}
	pkList := strings.Join(pkPartition, ", ")

	var updateSets []string
	insertCols := make([]string, len(destColumns))
	insertVals := make([]string, len(destColumns))
	matAliases := make(map[string]string)
	var unchangedDataCols []string
	for i, col := range destColumns {
		q := quoteIdentifier(col)
		insertCols[i] = q
		insertVals[i] = "s." + q
		if pkMap[strings.ToLower(col)] {
			continue
		}
		if hasUnchangedCols && !destination.IsCDCMetaColumn(col) {
			matAlias := uniqueTrinoAlias(fmt.Sprintf("__ingestr_mat_%d", i), allColumns)
			matAliases[col] = matAlias
			unchangedDataCols = append(unchangedDataCols, col)
			updateSets = append(updateSets, fmt.Sprintf(`%s = CASE WHEN s.%s THEN s.%s ELSE t.%s END`, q, matAlias, q, q))
		} else {
			updateSets = append(updateSets, fmt.Sprintf("%s = s.%s", q, q))
		}
	}

	rnCol := uniqueTrinoAlias("__ingestr_cdc_rn", allColumns)
	allColsList := strings.Join(quoteColumns(allColumns), ", ")
	lsnCol := quoteIdentifier(destination.CDCLSNColumn)
	deletedCol := quoteIdentifier(destination.CDCDeletedColumn)
	syncedAtCol := quoteIdentifier(destination.CDCSyncedAtColumn)

	var sqlBuilder strings.Builder
	fmt.Fprintf(&sqlBuilder, "MERGE INTO %s AS t\n", targetFQN)

	incrementalPred := strings.TrimSpace(opts.IncrementalPredicate)

	if hasUnchangedCols {
		usingSubquery := buildCDCUnchangedUsingSubquery(
			stagingFQN,
			targetFQN,
			opts.PrimaryKeys,
			allColumns,
			unchangedDataCols,
			pkMap,
			matAliases,
			rnCol,
			opts.CDCHardDelete,
		)
		fmt.Fprintf(&sqlBuilder, "USING %s AS s\n", usingSubquery)
		fmt.Fprintf(&sqlBuilder, "ON %s\n", strings.Join(onConditions, " AND "))

		if opts.CDCHardDelete {
			matchedGuard := func(cond string) string {
				if incrementalPred != "" {
					return "(" + incrementalPred + ") AND " + cond
				}
				return cond
			}
			fmt.Fprintf(
				&sqlBuilder,
				"WHEN MATCHED AND %s THEN\n  DELETE\n",
				matchedGuard(fmt.Sprintf(`s.%s = true AND (t.%s IS NULL OR s.%s >= t.%s)`, deletedCol, lsnCol, lsnCol, lsnCol)),
			)
			if len(updateSets) > 0 {
				fmt.Fprintf(
					&sqlBuilder,
					"WHEN MATCHED AND %s THEN\n  UPDATE SET %s\n",
					matchedGuard(fmt.Sprintf(`s.%s = false AND (t.%s IS NULL OR s.%s > t.%s)`, deletedCol, lsnCol, lsnCol, lsnCol)),
					strings.Join(updateSets, ", "),
				)
			}
			fmt.Fprintf(
				&sqlBuilder,
				"WHEN NOT MATCHED AND s.%s = false THEN\n  INSERT (%s) VALUES (%s)",
				deletedCol,
				strings.Join(insertCols, ", "),
				strings.Join(insertVals, ", "),
			)
			return sqlBuilder.String()
		}

		hasActiveCol := uniqueTrinoAlias("__ingestr_has_active", allColumns)
		activeLSNCol := uniqueTrinoAlias("__ingestr_active_lsn", allColumns)
		newerLSN := fmt.Sprintf(
			`(t.%s IS NULL OR s.%s > t.%s OR (s.%s = t.%s AND s.%s = true AND COALESCE(t.%s, false) = false))`,
			lsnCol, lsnCol, lsnCol, lsnCol, lsnCol, deletedCol, deletedCol,
		)
		if incrementalPred != "" {
			newerLSN = "(" + incrementalPred + ") AND " + newerLSN
		}
		if len(updateSets) > 0 {
			hasFreshRowData := fmt.Sprintf(
				`(s.%s = false OR (s.%s AND (t.%s IS NULL OR s.%s >= t.%s)))`,
				deletedCol, hasActiveCol, lsnCol, activeLSNCol, lsnCol,
			)
			fmt.Fprintf(&sqlBuilder, "WHEN MATCHED AND %s AND %s THEN\n  UPDATE SET %s\n", newerLSN, hasFreshRowData, strings.Join(updateSets, ", "))
		}
		fmt.Fprintf(&sqlBuilder, "WHEN MATCHED AND %s AND s.%s = true THEN\n  UPDATE SET %s = true, %s = s.%s, %s = s.%s\n",
			newerLSN, deletedCol, deletedCol, lsnCol, lsnCol, syncedAtCol, syncedAtCol,
		)
		fmt.Fprintf(&sqlBuilder, "WHEN NOT MATCHED THEN\n  INSERT (%s) VALUES (%s)", strings.Join(insertCols, ", "), strings.Join(insertVals, ", "))
		return sqlBuilder.String()
	}

	if opts.CDCHardDelete {
		fmt.Fprintf(
			&sqlBuilder,
			"USING (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS _numbered WHERE %s = 1) AS s\n",
			allColsList,
			allColsList,
			pkList,
			destination.CDCLatestOverallOrderBy(quoteIdentifier),
			rnCol,
			stagingFQN,
			rnCol,
		)
		fmt.Fprintf(&sqlBuilder, "ON %s\n", strings.Join(onConditions, " AND "))

		matchedGuard := func(cond string) string {
			if incrementalPred != "" {
				return "(" + incrementalPred + ") AND " + cond
			}
			return cond
		}
		fmt.Fprintf(
			&sqlBuilder,
			"WHEN MATCHED AND %s THEN\n  DELETE\n",
			matchedGuard(fmt.Sprintf(`s.%s = true AND (t.%s IS NULL OR s.%s >= t.%s)`, deletedCol, lsnCol, lsnCol, lsnCol)),
		)
		if len(updateSets) > 0 {
			fmt.Fprintf(
				&sqlBuilder,
				"WHEN MATCHED AND %s THEN\n  UPDATE SET %s\n",
				matchedGuard(fmt.Sprintf(`s.%s = false AND (t.%s IS NULL OR s.%s > t.%s)`, deletedCol, lsnCol, lsnCol, lsnCol)),
				strings.Join(updateSets, ", "),
			)
		}
		fmt.Fprintf(
			&sqlBuilder,
			"WHEN NOT MATCHED AND s.%s = false THEN\n  INSERT (%s) VALUES (%s)",
			deletedCol,
			strings.Join(insertCols, ", "),
			strings.Join(insertVals, ", "),
		)
		return sqlBuilder.String()
	}

	hasActiveCol := uniqueTrinoAlias("__ingestr_has_active", allColumns)
	activeLSNCol := uniqueTrinoAlias("__ingestr_active_lsn", allColumns)

	laActJoin := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		q := quoteIdentifier(pk)
		laActJoin[i] = fmt.Sprintf("la.%s IS NOT DISTINCT FROM act.%s", q, q)
	}

	selectCols := make([]string, 0, len(allColumns)+2)
	for _, col := range allColumns {
		alias := "act"
		if pkMap[strings.ToLower(col)] || destination.IsCDCColumn(col) {
			alias = "la"
		}
		selectCols = append(selectCols, fmt.Sprintf("%s.%s", alias, quoteIdentifier(col)))
	}

	selectCols = append(selectCols, fmt.Sprintf("act.%s IS NOT NULL AS %s", lsnCol, hasActiveCol))
	selectCols = append(selectCols, fmt.Sprintf("act.%s AS %s", lsnCol, activeLSNCol))

	fmt.Fprintf(
		&sqlBuilder,
		"USING (SELECT %s FROM (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM %s) AS _la_numbered WHERE %s = 1) AS la LEFT JOIN (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS %s FROM %s WHERE %s = false) AS _act_numbered WHERE %s = 1) AS act ON %s) AS s\n",
		strings.Join(selectCols, ", "),
		allColsList,
		allColsList,
		pkList,
		destination.CDCLatestOverallOrderBy(quoteIdentifier),
		rnCol,
		stagingFQN,
		rnCol,
		allColsList,
		allColsList,
		pkList,
		lsnCol,
		rnCol,
		stagingFQN,
		deletedCol,
		rnCol,
		strings.Join(laActJoin, " AND "),
	)
	fmt.Fprintf(&sqlBuilder, "ON %s\n", strings.Join(onConditions, " AND "))

	newerLSN := fmt.Sprintf(
		`(t.%s IS NULL OR s.%s > t.%s OR (s.%s = t.%s AND s.%s = true AND COALESCE(t.%s, false) = false))`,
		lsnCol, lsnCol, lsnCol, lsnCol, lsnCol, deletedCol, deletedCol,
	)
	if incrementalPred != "" {
		newerLSN = "(" + incrementalPred + ") AND " + newerLSN
	}

	if len(updateSets) > 0 {
		hasFreshRowData := fmt.Sprintf(
			`(s.%s = false OR (s.%s AND (t.%s IS NULL OR s.%s >= t.%s)))`,
			deletedCol, hasActiveCol, lsnCol, activeLSNCol, lsnCol,
		)
		fmt.Fprintf(&sqlBuilder, "WHEN MATCHED AND %s AND %s THEN\n  UPDATE SET %s\n", newerLSN, hasFreshRowData, strings.Join(updateSets, ", "))
	}

	fmt.Fprintf(&sqlBuilder, "WHEN MATCHED AND %s AND s.%s = true THEN\n  UPDATE SET %s = true, %s = s.%s, %s = s.%s\n",
		newerLSN, deletedCol, deletedCol, lsnCol, lsnCol, syncedAtCol, syncedAtCol,
	)
	fmt.Fprintf(&sqlBuilder, "WHEN NOT MATCHED THEN\n  INSERT (%s) VALUES (%s)", strings.Join(insertCols, ", "), strings.Join(insertVals, ", "))
	return sqlBuilder.String()
}

func buildCDCUnchangedUsingSubquery(
	stagingFQN, targetFQN string,
	primaryKeys, allColumns, unchangedDataCols []string,
	pkMap map[string]bool,
	matAliases map[string]string,
	rnCol string,
	hardDelete bool,
) string {
	lsnCol := quoteIdentifier(destination.CDCLSNColumn)
	deletedCol := quoteIdentifier(destination.CDCDeletedColumn)
	unchangedCol := quoteIdentifier(destination.CDCUnchangedColsColumn)
	unchangedArrCol := uniqueTrinoAlias("__ingestr_unchanged_arr", allColumns)

	pkJoin := func(a, b string) string {
		parts := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			q := quoteIdentifier(pk)
			parts[i] = fmt.Sprintf("%s.%s IS NOT DISTINCT FROM %s.%s", a, q, b, q)
		}
		return strings.Join(parts, " AND ")
	}
	pkEqJoin := func(a, b string) string {
		parts := make([]string, len(primaryKeys))
		for i, pk := range primaryKeys {
			q := quoteIdentifier(pk)
			parts[i] = fmt.Sprintf("%s.%s = %s.%s", a, q, b, q)
		}
		return strings.Join(parts, " AND ")
	}

	quotedPKs := quoteColumns(primaryKeys)
	pkList := strings.Join(quotedPKs, ", ")
	allColsList := strings.Join(quoteColumns(allColumns), ", ")

	stgPrefixed := make([]string, len(allColumns))
	for i, col := range allColumns {
		stgPrefixed[i] = "stg." + quoteIdentifier(col)
	}

	ctes := []string{
		fmt.Sprintf(
			`__ingestr_changes AS (
  SELECT %s, TRY(CAST(json_parse(lower(COALESCE(NULLIF(stg.%s, ''), '[]'))) AS ARRAY(VARCHAR))) AS %s
  FROM %s AS stg
  LEFT JOIN %s AS tgt ON %s
  WHERE tgt.%s IS NULL OR stg.%s > tgt.%s OR (stg.%s = tgt.%s AND (stg.%s = true OR EXISTS (SELECT 1 FROM %s AS tb WHERE %s AND tb.%s = stg.%s AND tb.%s = true)))
)`,
			strings.Join(stgPrefixed, ", "),
			unchangedCol,
			unchangedArrCol,
			stagingFQN,
			targetFQN,
			pkEqJoin("tgt", "stg"),
			lsnCol, lsnCol, lsnCol, lsnCol, lsnCol, deletedCol,
			stagingFQN,
			pkEqJoin("tb", "stg"),
			lsnCol, lsnCol, deletedCol,
		),
		fmt.Sprintf(
			`__ingestr_la AS (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS %s FROM __ingestr_changes) AS _la_numbered WHERE %s = 1)`,
			allColsList,
			allColsList,
			pkList,
			destination.CDCLatestOverallOrderBy(quoteIdentifier),
			rnCol,
			rnCol,
		),
	}

	if !hardDelete {
		ctes = append(ctes, fmt.Sprintf(
			`__ingestr_act AS (SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS %s FROM __ingestr_changes WHERE %s = false) AS _act_numbered WHERE %s = 1)`,
			allColsList,
			allColsList,
			pkList,
			lsnCol,
			rnCol,
			deletedCol,
			rnCol,
		))
	}

	vAliases := make(map[string]string, len(unchangedDataCols))
	joins := make([]string, 0, len(unchangedDataCols)+1)
	if !hardDelete {
		joins = append(joins, fmt.Sprintf("LEFT JOIN __ingestr_act AS act ON %s", pkJoin("la", "act")))
	}
	for i, col := range unchangedDataCols {
		qCol := quoteIdentifier(col)
		matAlias := matAliases[col]
		cteName := fmt.Sprintf("__ingestr_v_%d", i)
		vAlias := fmt.Sprintf("v_%d", i)
		vAliases[col] = vAlias
		ctes = append(ctes, fmt.Sprintf(
			`%s AS (SELECT %s, %s, TRUE AS %s FROM (SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s DESC) AS %s FROM __ingestr_changes WHERE %s = false AND NOT COALESCE(contains(%s, %s), false)) AS _v_%d_numbered WHERE %s = 1)`,
			cteName, pkList, qCol, matAlias, pkList, qCol, pkList, lsnCol, rnCol, deletedCol, unchangedArrCol, escapeString(strings.ToLower(col)), i, rnCol,
		))
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s AS %s ON %s", cteName, vAlias, pkJoin("la", vAlias)))
	}

	selectCols := make([]string, 0, len(allColumns)+len(unchangedDataCols)+2)
	for _, col := range allColumns {
		qCol := quoteIdentifier(col)
		if vAlias, ok := vAliases[col]; ok {
			selectCols = append(selectCols, fmt.Sprintf("%s.%s AS %s", vAlias, qCol, qCol))
			selectCols = append(selectCols, fmt.Sprintf("COALESCE(%s.%s, false) AS %s", vAlias, matAliases[col], matAliases[col]))
			continue
		}
		selectCols = append(selectCols, "la."+qCol)
	}
	if !hardDelete {
		hasActiveCol := uniqueTrinoAlias("__ingestr_has_active", allColumns)
		activeLSNCol := uniqueTrinoAlias("__ingestr_active_lsn", allColumns)
		selectCols = append(selectCols, fmt.Sprintf("act.%s IS NOT NULL AS %s", lsnCol, hasActiveCol))
		selectCols = append(selectCols, fmt.Sprintf("act.%s AS %s", lsnCol, activeLSNCol))
	}

	return fmt.Sprintf(
		"(WITH %s\nSELECT %s FROM __ingestr_la AS la %s)",
		strings.Join(ctes, ",\n"),
		strings.Join(selectCols, ", "),
		strings.Join(joins, " "),
	)
}

func uniqueTrinoAlias(base string, columns []string) string {
	for slices.ContainsFunc(columns, func(col string) bool { return strings.EqualFold(base, col) }) {
		base += "_"
	}
	return quoteIdentifier(base)
}
