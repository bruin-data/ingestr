package mssql

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// maxPrimaryKeyStringLength is the widest NVARCHAR a clustered primary key can
// index: 900 bytes of UTF-16.
const maxPrimaryKeyStringLength = 450

func (d *MSSQLDestination) NormalizeSchemaEvolutionSourceColumn(source, dest schema.Column) schema.Column {
	// An unbounded source widens a string key to the indexable maximum, never to NVARCHAR(MAX).
	if dest.IsPrimaryKey && dest.DataType == schema.TypeString && dest.MaxLength > 0 &&
		source.DataType == schema.TypeString && source.MaxLength <= 0 {
		source.MaxLength = max(dest.MaxLength, maxPrimaryKeyStringLength)
	}
	return source
}

func (d *MSSQLDestination) MaxDecimalPrecision() int {
	return 38
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MSSQLDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	return destination.ApplyEvolution(ctx, d, &Dialect{}, quoteTable(table), comparison)
}

func (d *MSSQLDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	statements, warnings, err := destination.RenderEvolution(&Dialect{}, quoteTable(table), comparison)
	if err != nil {
		return nil, "", err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return warnings, "", fmt.Errorf("failed to begin SQL Server conditional schema evolution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	identity, err := d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
	if err != nil {
		return warnings, "", err
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			config.LogFailedQuery(stmt, err)
			return warnings, "", fmt.Errorf("apply schema evolution: %s: %w", stmt, err)
		}
	}
	resultIncarnation, exists, err := d.cdcTargetIncarnationWith(ctx, tx, identity.qualifiedTable(d.server))
	if err != nil {
		return warnings, "", err
	}
	if !exists || resultIncarnation == "" {
		return warnings, "", fmt.Errorf("SQL Server CDC target %q has no durable physical incarnation after schema evolution", table)
	}
	if err := tx.Commit(); err != nil {
		return warnings, "", fmt.Errorf("failed to commit SQL Server conditional schema evolution: %w", err)
	}
	return warnings, resultIncarnation, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *MSSQLDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
