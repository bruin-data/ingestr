package redshift

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

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

func newRedshiftEvolutionGuardName() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("failed to generate Redshift schema evolution guard name: %w", err)
	}
	return "__ingestr_evolve_" + hex.EncodeToString(token[:]), nil
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

	_, warnings, err := destination.RenderEvolution(&Dialect{}, table, comparison)
	if err != nil {
		return nil, "", err
	}

	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return nil, "", err
	}
	targetRef := quoteTable(schemaName, tableName)
	statements, _, err := destination.RenderEvolution(&Dialect{}, targetRef, comparison)
	if err != nil {
		return nil, "", err
	}

	hasAlterType := false
	alterColPrefix := "ALTER TABLE " + targetRef + " ALTER COLUMN "
	for _, stmt := range statements {
		if strings.HasPrefix(stmt, alterColPrefix) {
			hasAlterType = true
			break
		}
	}

	if !hasAlterType {
		var resultInc string
		err = d.withTx(ctx, func(tx redshiftTx) error {
			lockedSchema, lockedTable, lockedTargetRef, lockErr := d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
			if lockErr != nil {
				return fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution: %w", table, lockErr)
			}
			txStmts := statements
			if lockedTargetRef != targetRef {
				var renderErr error
				txStmts, _, renderErr = destination.RenderEvolution(&Dialect{}, lockedTargetRef, comparison)
				if renderErr != nil {
					return renderErr
				}
			}
			for _, stmt := range txStmts {
				if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
					return leaseErr
				}
				if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
					return fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
				}
			}
			inc, exists, incErr := d.targetIncarnationInTx(ctx, tx, lockedSchema, lockedTable)
			if incErr != nil {
				return incErr
			}
			if !exists || inc != expectedIncarnation {
				return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
			}
			if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
				return leaseErr
			}
			resultInc = inc
			return nil
		})
		if err != nil {
			return nil, "", err
		}
		return warnings, resultInc, nil
	}

	// Redshift disallows ALTER COLUMN TYPE inside explicit transactions.
	// Stage the verified table under a private name inside a locked transaction
	// so concurrent drop+recreate of the original target name can never be altered,
	// then restore the original name only if its slot remained unoccupied and the
	// physical incarnation is unchanged.
	guardName, err := newRedshiftEvolutionGuardName()
	if err != nil {
		return nil, "", err
	}

	var lockedSchema, lockedTable, guardRef string
	err = d.withTx(ctx, func(tx redshiftTx) error {
		var lockedTargetRef string
		var lockErr error
		lockedSchema, lockedTable, lockedTargetRef, lockErr = d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
		if lockErr != nil {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution: %w", table, lockErr)
		}
		exists, existsErr := tableExists(ctx, tx, lockedSchema, guardName)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return fmt.Errorf("temporary Redshift schema evolution table %s.%s already exists", lockedSchema, guardName)
		}
		if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
			return leaseErr
		}
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", lockedTargetRef, destination.QuoteIdentifier(guardName))
		if _, execErr := tx.Exec(ctx, renameSQL); execErr != nil {
			return fmt.Errorf("failed to stage Redshift CDC target %q for schema evolution: %w", table, execErr)
		}
		guardRef = quoteTable(lockedSchema, guardName)
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	guardStmts, _, applyErr := destination.RenderEvolution(&Dialect{}, guardRef, comparison)
	if applyErr == nil {
		for _, stmt := range guardStmts {
			if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
				applyErr = leaseErr
				break
			}
			inc, exists, incErr := d.targetIncarnationInTx(ctx, db, lockedSchema, guardName)
			if incErr != nil {
				applyErr = incErr
				break
			}
			if !exists || inc != expectedIncarnation {
				applyErr = fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
				break
			}
			if _, execErr := db.Exec(ctx, stmt); execErr != nil {
				applyErr = fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
				break
			}
		}
	}

	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var resultInc string
	restoreErr := d.withTx(restoreCtx, func(tx redshiftTx) error {
		inc, exists, incErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, guardName)
		if incErr != nil {
			return incErr
		}
		if !exists || inc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
		}
		if _, lockErr := tx.Exec(restoreCtx, "LOCK "+guardRef); lockErr != nil {
			return fmt.Errorf("failed to lock staged Redshift CDC target %s: %w", guardRef, lockErr)
		}
		inc, exists, incErr = d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, guardName)
		if incErr != nil {
			return incErr
		}
		if !exists || inc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
		}

		_, targetExists, targetErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, lockedTable)
		if targetErr != nil {
			return targetErr
		}
		if targetExists {
			return fmt.Errorf("redshift CDC target %q was replaced during schema evolution (original preserved as %s)", table, guardRef)
		}

		renameBackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", guardRef, destination.QuoteIdentifier(lockedTable))
		if _, execErr := tx.Exec(restoreCtx, renameBackSQL); execErr != nil {
			return fmt.Errorf("failed to restore Redshift CDC target %q after schema evolution: %w", table, execErr)
		}

		finalInc, finalExists, finalErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, lockedTable)
		if finalErr != nil {
			return finalErr
		}
		if !finalExists || finalInc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
		}
		resultInc = finalInc
		return nil
	})

	if applyErr != nil || restoreErr != nil {
		return nil, "", errors.Join(applyErr, restoreErr)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, "", err
	}
	return warnings, resultInc, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *RedshiftDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
