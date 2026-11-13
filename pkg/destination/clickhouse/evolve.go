package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
)

func (d *ClickHouseDestination) evolutionDialect(ctx context.Context, table string) (*Dialect, error) {
	dialect := &Dialect{cluster: d.cluster}
	if d.conn == nil {
		return dialect, nil
	}
	current, err := d.GetTableSchema(ctx, table)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return dialect, nil
	}
	dialect.columns = make(map[string]schema.Column, len(current.Columns))
	for _, col := range current.Columns {
		dialect.columns[strings.ToLower(col.Name)] = col
	}
	return dialect, nil
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *ClickHouseDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	dialect, err := d.evolutionDialect(ctx, table)
	if err != nil {
		return nil, err
	}
	return destination.ApplyEvolution(ctx, d, dialect, d.tableRef(table), comparison)
}

func (d *ClickHouseDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, "", err
	}
	current, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return nil, "", err
	}
	if !exists || current != expectedIncarnation {
		return nil, "", fmt.Errorf("ClickHouse CDC target %q physical incarnation changed before schema evolution", table)
	}

	dialect, err := d.evolutionDialect(ctx, table)
	if err != nil {
		return nil, "", err
	}
	statements, warnings, err := destination.RenderEvolution(dialect, d.tableRef(table), comparison)
	if err != nil {
		return nil, "", err
	}
	for _, stmt := range statements {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return warnings, "", err
		}
		if err := d.checkCDCIncarnation(ctx, table, expectedIncarnation); err != nil {
			return warnings, "", fmt.Errorf("ClickHouse CDC target %q physical incarnation changed during schema evolution: %w", table, err)
		}
		if err := d.Exec(ctx, stmt); err != nil {
			return warnings, "", fmt.Errorf("apply schema evolution: %s: %w", stmt, err)
		}
	}

	current, exists, err = d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return warnings, "", err
	}
	if !exists || current != expectedIncarnation {
		return warnings, "", fmt.Errorf("ClickHouse CDC target %q physical incarnation changed during schema evolution", table)
	}
	return warnings, current, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *ClickHouseDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
