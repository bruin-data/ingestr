package iceberg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *Destination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if d.catalog == nil {
		return nil, errors.New("iceberg destination not connected")
	}

	ident, err := parseIdentifier(table)
	if err != nil {
		return nil, err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if isMissingTableOrNamespace(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("iceberg: failed to load table %s: %w", table, err)
	}

	if comparison != nil && comparison.HasChanges {
		txn := tbl.NewTransaction()
		changed, err := d.stageSchemaComparisonUpdate(txn, tbl, comparison)
		if err != nil {
			return nil, err
		}
		if changed {
			tbl, err = txn.Commit(ctx)
			if err != nil {
				tbl, err = d.reconcileSchemaEvolutionCommit(ctx, ident, comparison, err)
				if err != nil {
					return nil, fmt.Errorf("iceberg: failed to commit schema update: %w", err)
				}
			}
		}
	}

	prepared := d.lookupPrepared(table)
	if !prepared.replace && prepared.partitionBy != "" && layoutColumnsExist(tbl.Schema(), prepared.partitionBy, nil) {
		if err := d.updateExistingPartitionSpec(ctx, tbl, prepared.partitionBy); err != nil {
			return nil, err
		}
		tbl, err = d.catalog.LoadTable(ctx, ident)
		if err != nil {
			return nil, fmt.Errorf("iceberg: failed to reload table %s after partition evolution: %w", table, err)
		}
	}
	if !prepared.replace && len(prepared.clusterBy) > 0 && layoutColumnsExist(tbl.Schema(), "", prepared.clusterBy) {
		if _, err := d.ensureSortOrder(ctx, tbl, prepared.clusterBy); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (d *Destination) reconcileSchemaEvolutionCommit(
	ctx context.Context,
	ident icebergtable.Identifier,
	comparison *schemaevolution.SchemaComparison,
	commitErr error,
) (*icebergtable.Table, error) {
	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitReconcileTimeout)
	defer cancel()

	var reconcileErr error
	for attempt := 0; ; attempt++ {
		tbl, err := d.catalog.LoadTable(reconcileCtx, ident)
		if err == nil {
			landed, verifyErr := schemaEvolutionLanded(tbl.Schema(), comparison)
			if verifyErr != nil {
				return nil, errors.Join(commitErr, verifyErr)
			}
			if landed {
				return tbl, nil
			}
		} else {
			reconcileErr = fmt.Errorf("iceberg: failed to reload table %s while reconciling schema commit: %w", strings.Join(ident, "."), err)
		}

		wait := min(25*time.Millisecond<<min(attempt, 5), 500*time.Millisecond)
		timer := time.NewTimer(wait)
		select {
		case <-reconcileCtx.Done():
			timer.Stop()
			if reconcileErr != nil {
				return nil, errors.Join(commitErr, reconcileErr)
			}
			return nil, commitErr
		case <-timer.C:
		}
	}
}

func schemaEvolutionLanded(tableSchema *iceberggo.Schema, comparison *schemaevolution.SchemaComparison) (bool, error) {
	if tableSchema == nil || comparison == nil {
		return false, nil
	}
	for _, change := range comparison.Changes {
		fieldName := strings.Join(schemaChangePath(change), ".")
		field, exists := tableSchema.FindFieldByName(fieldName)
		switch change.Type {
		case schemaevolution.ChangeAddColumn, schemaevolution.ChangeWidenType, schemaevolution.ChangeOverrideType:
			if !exists {
				return false, nil
			}
			targetType, err := icebergTypeForColumn(change.NewColumn)
			if err != nil {
				return false, fmt.Errorf("iceberg: failed to verify reconciled column %q type: %w", fieldName, err)
			}
			expectedRequired := !change.NewColumn.Nullable || slices.Contains(tableSchema.IdentifierFieldIDs, field.ID)
			if !icebergTypesEquivalent(field.Type, targetType) || field.Required != expectedRequired {
				return false, nil
			}
		case schemaevolution.ChangeRemoveColumn:
			if exists && field.Required {
				return false, nil
			}
		case schemaevolution.ChangeRelaxNullability:
			if !exists {
				return false, nil
			}
			identifier := slices.Contains(tableSchema.IdentifierFieldIDs, field.ID)
			if field.Required != identifier {
				return false, nil
			}
		default:
			return false, fmt.Errorf("iceberg: cannot reconcile unsupported schema change %d for column %q", change.Type, fieldName)
		}
	}
	return true, nil
}

func (d *Destination) SupportsColumnTypeChanges() bool {
	return true
}

func (d *Destination) stageSchemaComparisonUpdate(txn *icebergtable.Transaction, tbl *icebergtable.Table, comparison *schemaevolution.SchemaComparison) (bool, error) {
	update := txn.UpdateSchema(true, false, icebergtable.WithNameMapping(tbl.NameMapping()))
	changed, err := stageSchemaComparisonChanges(update, tbl, comparison, false)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update table schema: %w", err)
	}
	return true, nil
}

func stageSchemaComparisonChanges(
	update *icebergtable.UpdateSchema,
	tbl *icebergtable.Table,
	comparison *schemaevolution.SchemaComparison,
	allowRequiredAdds bool,
) (bool, error) {
	changed := false

	for _, change := range comparison.Changes {
		switch change.Type {
		case schemaevolution.ChangeAddColumn:
			path := schemaChangePath(change)
			applied, err := stageIcebergAddColumnAtPathRequired(update, path, change.NewColumn, allowRequiredAdds)
			if err != nil {
				return false, err
			}
			changed = changed || applied

		case schemaevolution.ChangeWidenType, schemaevolution.ChangeOverrideType:
			applied, err := stageIcebergUpdateColumnAtPath(update, tbl, schemaChangePath(change), change.NewColumn)
			if err != nil {
				return false, err
			}
			changed = changed || applied

		case schemaevolution.ChangeRemoveColumn:
			applied, err := stageIcebergSoftRemoveColumnAtPath(update, tbl, schemaChangePath(change))
			if err != nil {
				return false, err
			}
			changed = changed || applied

		case schemaevolution.ChangeRelaxNullability:
			path := schemaChangePath(change)
			field, ok := tbl.Schema().FindFieldByName(strings.Join(path, "."))
			if !ok {
				return false, fmt.Errorf("iceberg: nested field %q not found for nullability relaxation", strings.Join(path, "."))
			}
			if field.Required && !slices.Contains(tbl.Schema().IdentifierFieldIDs, field.ID) {
				update.UpdateColumn(path, icebergtable.ColumnUpdate{
					Required: iceberggo.Optional[bool]{Valid: true, Val: false},
				})
				changed = true
			}
		}
	}

	return changed, nil
}

func stageIcebergAddColumnAtPath(update *icebergtable.UpdateSchema, path []string, col schema.Column) (bool, error) {
	return stageIcebergAddColumnAtPathRequired(update, path, col, false)
}

func stageIcebergAddColumnAtPathRequired(update *icebergtable.UpdateSchema, path []string, col schema.Column, allowRequired bool) (bool, error) {
	if err := validateIcebergColumnName(col.Name); err != nil {
		return false, err
	}
	if !col.Nullable && !allowRequired {
		return false, fmt.Errorf("iceberg: cannot add required column %q to an existing table without an initial default; make it nullable or use replace", col.Name)
	}
	targetType, err := icebergTypeForColumn(col)
	if err != nil {
		return false, fmt.Errorf("iceberg: failed to map column %q type: %w", col.Name, err)
	}
	update.AddColumn(path, targetType, "", allowRequired && !col.Nullable, nil)
	return true, nil
}

func stageIcebergUpdateColumnAtPath(update *icebergtable.UpdateSchema, tbl *icebergtable.Table, path []string, col schema.Column) (bool, error) {
	colName := strings.Join(path, ".")
	if err := validateIcebergColumnName(colName); err != nil {
		return false, err
	}
	targetType, err := icebergTypeForColumn(col)
	if err != nil {
		return false, fmt.Errorf("iceberg: failed to map column %q type: %w", colName, err)
	}
	field, ok := tbl.Schema().FindFieldByName(colName)
	if !ok {
		return stageIcebergAddColumnAtPath(update, path, col)
	}

	changed := false
	if !icebergTypesEquivalent(field.Type, targetType) {
		if _, err := iceberggo.PromoteType(field.Type, targetType); err != nil {
			return false, fmt.Errorf("iceberg: column %q type change from %s to %s is not supported: %w", colName, field.Type, targetType, err)
		}
		if _, nested := field.Type.(*iceberggo.StructType); nested {
			return false, fmt.Errorf("iceberg: replacing nested parent field %q is unsupported; evolve child fields instead", colName)
		}
		update.UpdateColumn(path, icebergtable.ColumnUpdate{
			FieldType: iceberggo.Optional[iceberggo.Type]{Valid: true, Val: targetType},
		})
		changed = true
	}
	if field.Required && col.Nullable && !slices.Contains(tbl.Schema().IdentifierFieldIDs, field.ID) {
		update.UpdateColumn(path, icebergtable.ColumnUpdate{
			Required: iceberggo.Optional[bool]{Valid: true, Val: false},
		})
		changed = true
	}
	return changed, nil
}

func schemaChangePath(change schemaevolution.SchemaChange) []string {
	if len(change.ColumnPath) > 0 {
		return append([]string(nil), change.ColumnPath...)
	}
	return []string{change.ColumnName}
}

func stageIcebergSoftRemoveColumnAtPath(update *icebergtable.UpdateSchema, tbl *icebergtable.Table, path []string) (bool, error) {
	colName := strings.Join(path, ".")
	field, ok := tbl.Schema().FindFieldByName(colName)
	if !ok || !field.Required {
		return false, nil
	}
	for _, identifierID := range tbl.Schema().IdentifierFieldIDs {
		if identifierID == field.ID {
			return false, fmt.Errorf("iceberg: cannot soft-remove identifier column %q; update the primary key or use replace", colName)
		}
	}
	update.UpdateColumn(path, icebergtable.ColumnUpdate{
		Required: iceberggo.Optional[bool]{Valid: true, Val: false},
	})
	return true, nil
}
