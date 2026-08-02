package hana

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *HanaDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if comparison == nil || !comparison.HasChanges {
		return nil, nil
	}

	dialect := &Dialect{}
	var warnings []string
	for _, change := range comparison.Changes {
		if change.Type != schemaevolution.ChangeAddColumn || change.OldColumn != nil {
			continue
		}
		recovered, err := d.recoverInterruptedColumnRewrite(ctx, table, change.ColumnName)
		if err != nil {
			return warnings, err
		}
		if recovered {
			return warnings, fmt.Errorf("recovered interrupted HANA rewrite for column %q; retry ingestion so schema evolution can be replanned", change.ColumnName)
		}
	}

	for _, change := range comparison.Changes {
		if change.Type == schemaevolution.ChangeRemoveColumn && isRewriteArtifactColumn(change.ColumnName) {
			if err := d.dropColumn(ctx, table, change.ColumnName); err != nil {
				return warnings, fmt.Errorf("clean interrupted HANA rewrite artifact %q: %w", change.ColumnName, err)
			}
			continue
		}
		// HANA cannot store a primary key in a LOB column, so such columns are created as
		// NVARCHAR(hanaMaxInlineLength). Carry the flag over so the rendered types match and
		// the no-op widening back to NCLOB is suppressed instead of failing.
		if change.OldColumn != nil && change.OldColumn.IsPrimaryKey {
			change.NewColumn.IsPrimaryKey = true
		}
		singleChange := &schemaevolution.SchemaComparison{
			Changes:    []schemaevolution.SchemaChange{change},
			HasChanges: true,
		}
		statements, changeWarnings, err := destination.RenderEvolution(dialect, table, singleChange)
		if err != nil {
			return warnings, err
		}
		warnings = append(warnings, changeWarnings...)

		for _, statement := range statements {
			if err := d.Exec(ctx, statement); err != nil {
				if isTypeChange(change) && supportsColumnRewrite(change.NewColumn) {
					if rewriteErr := d.rewriteColumnType(ctx, table, change); rewriteErr == nil {
						continue
					} else {
						return warnings, fmt.Errorf("apply schema evolution: %s: %w; HANA column rewrite also failed: %v", statement, err, rewriteErr)
					}
				}
				return warnings, fmt.Errorf("apply schema evolution: %s: %w", statement, err)
			}
		}
	}
	return warnings, nil
}

func isRewriteArtifactColumn(column string) bool {
	name := canonicalIdentifier(column)
	return strings.HasPrefix(name, "__BRUIN_EVOLVE_NEW_") || strings.HasPrefix(name, "__BRUIN_EVOLVE_OLD_")
}

func (d *HanaDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}

func isTypeChange(change schemaevolution.SchemaChange) bool {
	return change.Type == schemaevolution.ChangeWidenType || change.Type == schemaevolution.ChangeOverrideType
}

func supportsColumnRewrite(column schema.Column) bool {
	switch column.DataType {
	case schema.TypeString, schema.TypeJSON, schema.TypeArray:
		return true
	default:
		return false
	}
}

func (d *HanaDestination) rewriteColumnType(ctx context.Context, table string, change schemaevolution.SchemaChange) error {
	if change.OldColumn == nil {
		return fmt.Errorf("column %q has no existing type to rewrite", change.ColumnName)
	}
	if change.OldColumn.IsPrimaryKey {
		return fmt.Errorf("cannot rewrite primary-key column %q", change.ColumnName)
	}

	temporaryName, backupName := rewriteArtifactColumnNames(table, change.ColumnName)
	if recovered, err := d.recoverInterruptedColumnRewrite(ctx, table, change.ColumnName); err != nil {
		return err
	} else if recovered {
		return fmt.Errorf("recovered an interrupted rewrite for column %q; retry schema evolution", change.ColumnName)
	}
	targetType := (&Dialect{}).TypeName(change.NewColumn)
	addSQL := fmt.Sprintf("ALTER TABLE %s ADD (%s %s)", quoteTable(table), quoteColumn(temporaryName), targetType)
	if err := d.Exec(ctx, addSQL); err != nil {
		return fmt.Errorf("add conversion column: %w", err)
	}

	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = d.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP (%s)", quoteTable(table), quoteColumn(temporaryName)))
		}
	}()

	conversion := fmt.Sprintf("CAST(%s AS %s)", quoteColumn(change.ColumnName), targetType)
	if targetType == "NCLOB" {
		conversion = fmt.Sprintf("TO_NCLOB(%s)", quoteColumn(change.ColumnName))
	}
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET %s = %s",
		quoteTable(table), quoteColumn(temporaryName), conversion,
	)
	if err := d.Exec(ctx, updateSQL); err != nil {
		return fmt.Errorf("convert column values: %w", err)
	}

	if !change.NewColumn.Nullable {
		notNullSQL := fmt.Sprintf("ALTER TABLE %s ALTER (%s NOT NULL)", quoteTable(table), quoteColumn(temporaryName))
		if err := d.Exec(ctx, notNullSQL); err != nil {
			return fmt.Errorf("apply NOT NULL to converted column: %w", err)
		}
	}

	if err := d.renameColumn(ctx, table, change.ColumnName, backupName); err != nil {
		_, recoveryErr := d.recoverInterruptedColumnRewrite(ctx, table, change.ColumnName)
		if recoveryErr != nil {
			return fmt.Errorf("preserve original column %q as %q: %w; recovery failed: %v", change.ColumnName, backupName, err, recoveryErr)
		}
		return fmt.Errorf("preserve original column %q as %q: %w", change.ColumnName, backupName, err)
	}
	if err := d.renameColumn(ctx, table, temporaryName, change.ColumnName); err != nil {
		exists, inspectErr := d.rewriteColumnExistence(ctx, table, change.ColumnName, temporaryName, backupName)
		if inspectErr == nil && exists[canonicalIdentifier(change.ColumnName)] && !exists[canonicalIdentifier(temporaryName)] {
			cleanupTemporary = false
			if dropErr := d.dropColumn(ctx, table, backupName); dropErr != nil {
				return fmt.Errorf("converted column %q was activated despite a connection error, but backup cleanup failed: %w", change.ColumnName, dropErr)
			}
			return nil
		}
		if restoreErr := d.renameColumn(ctx, table, backupName, change.ColumnName); restoreErr != nil {
			return fmt.Errorf("activate converted column %q: %w; original remains at %q and restore failed: %v; state inspection failed: %v", change.ColumnName, err, backupName, restoreErr, inspectErr)
		}
		return fmt.Errorf("activate converted column %q: %w; original column restored", change.ColumnName, err)
	}
	cleanupTemporary = false

	if err := d.dropColumn(ctx, table, backupName); err != nil {
		return fmt.Errorf("drop rewrite backup column %q after activating %q: %w", backupName, change.ColumnName, err)
	}
	return nil
}

func rewriteArtifactColumnNames(table, column string) (string, string) {
	seed := quoteTable(table) + "\x00" + canonicalIdentifier(column)
	digest := sha256.Sum256([]byte(seed))
	tag := canonicalIdentifier(hex.EncodeToString(digest[:4]))
	name := func(kind string) string {
		candidate := fmt.Sprintf("__BRUIN_EVOLVE_%s_%s_%s", kind, canonicalIdentifier(column), tag)
		return destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("hana"))
	}
	return name("NEW"), name("OLD")
}

func (d *HanaDestination) recoverInterruptedColumnRewrite(ctx context.Context, table, column string) (bool, error) {
	temporaryName, backupName := rewriteArtifactColumnNames(table, column)
	exists, err := d.rewriteColumnExistence(ctx, table, column, temporaryName, backupName)
	if err != nil {
		return false, err
	}
	originalExists := exists[canonicalIdentifier(column)]
	temporaryExists := exists[canonicalIdentifier(temporaryName)]
	backupExists := exists[canonicalIdentifier(backupName)]

	switch {
	case originalExists && temporaryExists && backupExists:
		return false, fmt.Errorf("ambiguous HANA rewrite state for column %q: both rewrite artifacts still exist", column)
	case originalExists && backupExists:
		if err := d.dropColumn(ctx, table, backupName); err != nil {
			return false, fmt.Errorf("clean completed rewrite backup %q: %w", backupName, err)
		}
	case originalExists && temporaryExists:
		if err := d.dropColumn(ctx, table, temporaryName); err != nil {
			return false, fmt.Errorf("clean abandoned rewrite column %q: %w", temporaryName, err)
		}
	case !originalExists && backupExists:
		if err := d.renameColumn(ctx, table, backupName, column); err != nil {
			return false, fmt.Errorf("restore interrupted rewrite backup %q to %q: %w", backupName, column, err)
		}
		if temporaryExists {
			if err := d.dropColumn(ctx, table, temporaryName); err != nil {
				return false, fmt.Errorf("clean converted rewrite column %q after restoring %q: %w", temporaryName, column, err)
			}
		}
		return true, nil
	case !originalExists && temporaryExists:
		if err := d.renameColumn(ctx, table, temporaryName, column); err != nil {
			return false, fmt.Errorf("activate recovered rewrite column %q as %q: %w", temporaryName, column, err)
		}
		return true, nil
	}
	return false, nil
}

func (d *HanaDestination) rewriteColumnExistence(ctx context.Context, table string, columns ...string) (map[string]bool, error) {
	schemaName, tableName := d.effectiveSchemaTable(table)
	query := `
		SELECT COLUMN_NAME
		FROM SYS.TABLE_COLUMNS
		WHERE SCHEMA_NAME = ? AND TABLE_NAME = ? AND COLUMN_NAME IN (?, ?, ?)`
	rows, err := d.db.QueryContext(
		ctx,
		query,
		schemaName,
		tableName,
		canonicalIdentifier(columns[0]),
		canonicalIdentifier(columns[1]),
		canonicalIdentifier(columns[2]),
	)
	if err != nil {
		return nil, fmt.Errorf("inspect HANA rewrite state for column %q: %w", columns[0], err)
	}
	defer func() { _ = rows.Close() }()

	exists := make(map[string]bool, len(columns))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan HANA rewrite state: %w", err)
		}
		exists[canonicalIdentifier(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate HANA rewrite state: %w", err)
	}
	return exists, nil
}

func (d *HanaDestination) renameColumn(ctx context.Context, table, from, to string) error {
	query := fmt.Sprintf("RENAME COLUMN %s.%s TO %s", quoteTable(table), quoteColumn(from), quoteColumn(to))
	return d.Exec(ctx, query)
}

func (d *HanaDestination) dropColumn(ctx context.Context, table, column string) error {
	query := fmt.Sprintf("ALTER TABLE %s DROP (%s)", quoteTable(table), quoteColumn(column))
	return d.Exec(ctx, query)
}
