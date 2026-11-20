package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const cdcOrderColumn = "__ingestr_cdc_order"

// Shift the unsigned 64-bit WAL position into the signed range before
// appending the 64-bit event sequence, so every position fits in LARGEINT.
const cdcOrderExpression = "((CAST(CONV(CONCAT(SUBSTR(_cdc_lsn,1,8),SUBSTR(_cdc_lsn,10,8)),16,10) AS LARGEINT)-CAST('9223372036854775808' AS LARGEINT))*CAST('18446744073709551616' AS LARGEINT)+CAST(CONV(IF(LENGTH(_cdc_lsn)>17,SUBSTR(_cdc_lsn,19,16),'0'),16,10) AS LARGEINT))"

const cdcOrderRenderedExpression = "(((CAST((conv(concat(substr(_cdc_lsn, 1, 8), substr(_cdc_lsn, 10, 8)), 16, 10)) AS LARGEINT)) - (CAST('9223372036854775808' AS LARGEINT))) * (CAST('18446744073709551616' AS LARGEINT))) + (CAST((conv(if((length(_cdc_lsn)) > 17, substr(_cdc_lsn, 19, 16), '0'), 16, 10)) AS LARGEINT))"

func (d *StarRocksDestination) ensureCDCSnapshotOrder(ctx context.Context, table string, exists bool, expectedIncarnation string) (bool, error) {
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return false, err
	}
	previous, err := d.showStatus(ctx, d.schemaChangeQuery(table))
	if err != nil {
		return false, fmt.Errorf("read previous StarRocks CDC schema change: %w", err)
	}
	if state := previous["State"]; state != "" && state != "FINISHED" && state != "CANCELLED" {
		if err := d.waitForSchemaChange(ctx, table, ""); err != nil {
			return false, err
		}
		if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
			return false, err
		}
		database, name := d.tableParts(table)
		var count int
		if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?", database, name, cdcOrderColumn).Scan(&count); err != nil {
			return false, fmt.Errorf("read StarRocks CDC ordering column after schema change: %w", err)
		}
		exists = count != 0
	}
	if exists {
		if err := d.validateCDCSnapshotOrderColumn(ctx, table); err != nil {
			return false, err
		}
		return true, nil
	}
	var version, engine string
	if err := d.db.QueryRowContext(ctx, "SELECT current_version()").Scan(&version); err != nil {
		return false, fmt.Errorf("read StarRocks version for direct CDC snapshots: %w", err)
	}
	database, name := d.tableParts(table)
	if err := d.db.QueryRowContext(ctx, "SELECT TABLE_ENGINE FROM information_schema.tables_config WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?", database, name).Scan(&engine); err != nil {
		return false, fmt.Errorf("read StarRocks CDC snapshot table engine: %w", err)
	}
	if !supportsCDCOrderColumn(version, engine) {
		return false, nil
	}
	if expectedIncarnation == "" {
		var found bool
		var err error
		expectedIncarnation, found, err = d.CDCTargetIncarnation(ctx, table)
		if err != nil {
			return false, err
		}
		if !found {
			return false, fmt.Errorf("StarRocks CDC snapshot target %q disappeared", table)
		}
	}
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return false, err
	}
	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s LARGEINT AS %s", d.tableRef(table), quoteColumn(cdcOrderColumn), cdcOrderExpression)
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return false, fmt.Errorf("add StarRocks CDC ordering column: %w", err)
	}
	if err := d.waitForSchemaChange(ctx, table, previous["JobId"]); err != nil {
		return false, err
	}
	if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
		return false, err
	}
	if err := d.validateCDCSnapshotOrderColumn(ctx, table); err != nil {
		return false, err
	}
	return true, nil
}

func supportsCDCOrderColumn(version, engine string) bool {
	parts := strings.SplitN(strings.SplitN(version, "-", 2)[0], ".", 3)
	if len(parts) != 3 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	minimumMinor := 1
	switch strings.ToUpper(engine) {
	case "OLAP":
	case "CLOUD_NATIVE":
		minimumMinor = 5
	default:
		return false
	}
	return major > 3 || major == 3 && minor >= minimumMinor
}

func (d *StarRocksDestination) validateCDCSnapshotOrderColumn(ctx context.Context, table string) error {
	column, err := d.showStatus(ctx, "SHOW FULL COLUMNS FROM "+d.tableRef(table)+" LIKE "+sqlString(strings.ReplaceAll(cdcOrderColumn, "_", `\_`)))
	if err != nil {
		return fmt.Errorf("read StarRocks CDC ordering column: %w", err)
	}
	database, name := d.tableParts(table)
	var expression sql.NullString
	if err := d.db.QueryRowContext(ctx, "SELECT GENERATION_EXPRESSION FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?", database, name, cdcOrderColumn).Scan(&expression); err != nil {
		return fmt.Errorf("read StarRocks CDC ordering expression: %w", err)
	}
	if !strings.EqualFold(column["Field"], cdcOrderColumn) || !strings.EqualFold(column["Type"], "largeint") || !expression.Valid || !validCDCOrderExpression(expression.String) {
		return fmt.Errorf("StarRocks target %s has conflicting reserved CDC column %q", table, cdcOrderColumn)
	}
	return nil
}

func validCDCOrderExpression(expression string) bool {
	normalize := func(value string) string {
		var normalized strings.Builder
		for len(value) != 0 {
			if value[0] == '\'' || value[0] == '`' {
				quote := value[0]
				end := 1
				for end < len(value) {
					if value[end] == '\\' {
						end += 2
						continue
					}
					if value[end] == quote {
						end++
						if end < len(value) && value[end] == quote {
							end++
							continue
						}
						break
					}
					end++
				}
				if end > len(value) {
					return ""
				}
				token := value[:end]
				if strings.EqualFold(token, "`_cdc_lsn`") {
					normalized.WriteString("_cdc_lsn")
				} else {
					normalized.WriteString(token)
				}
				value = value[end:]
				continue
			}
			r := rune(value[0])
			if !unicode.IsSpace(r) {
				normalized.WriteRune(unicode.ToLower(r))
			}
			value = value[1:]
		}
		return normalized.String()
	}
	actual := normalize(expression)
	return actual == normalize(cdcOrderExpression) || actual == normalize(cdcOrderRenderedExpression)
}
