package iceberg

import (
	"context"
	"errors"
	"fmt"

	iceberggo "github.com/apache/iceberg-go"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

func (d *Destination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if comparison == nil || !comparison.HasChanges {
		return nil, nil
	}
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

	txn := tbl.NewTransaction()
	changed, err := d.stageSchemaComparisonUpdate(txn, tbl, comparison)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	if _, err := txn.Commit(ctx); err != nil {
		return nil, fmt.Errorf("iceberg: failed to commit schema update: %w", err)
	}
	return nil, nil
}

func (d *Destination) SupportsColumnTypeChanges() bool {
	return true
}

func (d *Destination) stageSchemaComparisonUpdate(txn *icebergtable.Transaction, tbl *icebergtable.Table, comparison *schemaevolution.SchemaComparison) (bool, error) {
	update := txn.UpdateSchema(true, false, icebergtable.WithNameMapping(tbl.NameMapping()))
	changed := false

	for _, change := range comparison.Changes {
		switch change.Type {
		case schemaevolution.ChangeAddColumn:
			applied, err := stageIcebergAddColumn(update, change.NewColumn)
			if err != nil {
				return false, err
			}
			changed = changed || applied

		case schemaevolution.ChangeWidenType, schemaevolution.ChangeOverrideType:
			applied, err := stageIcebergUpdateColumn(update, tbl, change.ColumnName, change.NewColumn)
			if err != nil {
				return false, err
			}
			changed = changed || applied
		}
	}

	if !changed {
		return false, nil
	}
	if err := update.Commit(); err != nil {
		return false, fmt.Errorf("iceberg: failed to update table schema: %w", err)
	}
	return true, nil
}

func stageIcebergAddColumn(update *icebergtable.UpdateSchema, col schema.Column) (bool, error) {
	targetType, err := icebergTypeForColumn(col)
	if err != nil {
		return false, fmt.Errorf("iceberg: failed to map column %q type: %w", col.Name, err)
	}
	update.AddColumn([]string{col.Name}, targetType, "", !col.Nullable, nil)
	return true, nil
}

func stageIcebergUpdateColumn(update *icebergtable.UpdateSchema, tbl *icebergtable.Table, colName string, col schema.Column) (bool, error) {
	targetType, err := icebergTypeForColumn(col)
	if err != nil {
		return false, fmt.Errorf("iceberg: failed to map column %q type: %w", colName, err)
	}
	field, ok := tbl.Schema().FindFieldByName(colName)
	if !ok {
		update.AddColumn([]string{colName}, targetType, "", !col.Nullable, nil)
		return true, nil
	}

	changed := false
	if !field.Type.Equals(targetType) {
		if _, err := iceberggo.PromoteType(field.Type, targetType); err != nil {
			return false, fmt.Errorf("iceberg: column %q type change from %s to %s is not supported: %w", colName, field.Type, targetType, err)
		}
		update.UpdateColumn([]string{colName}, icebergtable.ColumnUpdate{
			FieldType: iceberggo.Optional[iceberggo.Type]{Valid: true, Val: targetType},
		})
		changed = true
	}
	if field.Required && col.Nullable {
		update.UpdateColumn([]string{colName}, icebergtable.ColumnUpdate{
			Required: iceberggo.Optional[bool]{Valid: true, Val: false},
		})
		changed = true
	}
	return changed, nil
}
