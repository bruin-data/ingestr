package redshift

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *RedshiftDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *RedshiftDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

func (d *RedshiftDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, "", err
	}
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	db, err := d.requireDB()
	if err != nil {
		return nil, "", err
	}

	statements, warnings, err := destination.RenderEvolution(&Dialect{}, table, comparison)
	if err != nil {
		return nil, "", err
	}

	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return nil, "", err
	}

	hasAlterType := false
	for _, stmt := range statements {
		if strings.Contains(strings.ToUpper(stmt), " ALTER COLUMN ") && strings.Contains(strings.ToUpper(stmt), " TYPE ") {
			hasAlterType = true
			break
		}
	}

	if !hasAlterType {
		err = d.withTx(ctx, func(tx redshiftTx) error {
			if _, _, _, lockErr := d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation); lockErr != nil {
				return fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution: %w", table, lockErr)
			}
			for _, stmt := range statements {
				if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
					return fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
				}
			}
			return nil
		})
		if err != nil {
			return nil, "", err
		}
	} else {
		// Redshift disallows ALTER COLUMN TYPE inside explicit transactions.
		for _, stmt := range statements {
			inc, exists, incErr := d.targetIncarnationInTx(ctx, db, schemaName, tableName)
			if incErr != nil {
				return nil, "", incErr
			}
			if !exists || inc != expectedIncarnation {
				return nil, "", fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution", table)
			}
			if _, execErr := db.Exec(ctx, stmt); execErr != nil {
				return nil, "", fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
			}
		}
	}

	resultInc, exists, err := d.targetIncarnationInTx(ctx, db, schemaName, tableName)
	if err != nil {
		return nil, "", err
	}
	if !exists || resultInc == "" {
		return nil, "", fmt.Errorf("redshift CDC target %q missing after schema evolution", table)
	}
	return warnings, resultInc, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *RedshiftDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
