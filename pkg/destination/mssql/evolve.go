package mssql

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
)

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *MSSQLDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if hasTypeChanges(comparison) {
		keys, err := d.primaryKeyColumns(ctx, table)
		if err != nil {
			return nil, err
		}
		comparison = clampPrimaryKeyTypeChanges(comparison, keys)
	}
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *MSSQLDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}

func hasTypeChanges(comparison *schemaevolution.SchemaComparison) bool {
	if comparison == nil {
		return false
	}
	for _, change := range comparison.Changes {
		if change.Type == schemaevolution.ChangeWidenType || change.Type == schemaevolution.ChangeOverrideType {
			return true
		}
	}
	return false
}

// clampPrimaryKeyTypeChanges caps type changes on primary key columns at the
// indexable width used when the table was created (see mapColumnTypeForCreate)
// and drops the ones that become no-ops. Without this, an unbounded source
// string compared against an NVARCHAR(450) key asks for NVARCHAR(MAX), which
// SQL Server can neither index nor apply to a column its PK constraint uses.
func clampPrimaryKeyTypeChanges(comparison *schemaevolution.SchemaComparison, primaryKeys []string) *schemaevolution.SchemaComparison {
	if len(primaryKeys) == 0 {
		return comparison
	}

	dialect := &Dialect{}
	changes := make([]schemaevolution.SchemaChange, 0, len(comparison.Changes))
	for _, change := range comparison.Changes {
		isTypeChange := change.Type == schemaevolution.ChangeWidenType || change.Type == schemaevolution.ChangeOverrideType
		if !isTypeChange || change.OldColumn == nil || !containsFold(primaryKeys, change.ColumnName) {
			changes = append(changes, change)
			continue
		}
		change.NewColumn = clampKeyColumn(change.NewColumn)
		if dialect.TypeName(*change.OldColumn) == dialect.TypeName(change.NewColumn) {
			continue
		}
		changes = append(changes, change)
	}

	return &schemaevolution.SchemaComparison{Changes: changes, HasChanges: len(changes) > 0}
}

func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}

func (d *MSSQLDestination) primaryKeyColumns(ctx context.Context, table string) ([]string, error) {
	identity, err := d.resolveTargetIdentity(ctx, table)
	if err != nil {
		return nil, err
	}
	prefix := identity.catalogPrefix(d.server)
	if prefix != "" {
		prefix += "."
	}

	query := fmt.Sprintf(`SELECT c.name
	FROM %ssys.tables AS t
	JOIN %ssys.schemas AS s ON s.schema_id = t.schema_id
	JOIN %ssys.indexes AS i ON i.object_id = t.object_id AND i.is_primary_key = 1
	JOIN %ssys.index_columns AS ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id AND ic.key_ordinal > 0
	JOIN %ssys.columns AS c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
	WHERE s.name = @p1 AND t.name = @p2
	ORDER BY ic.key_ordinal`, prefix, prefix, prefix, prefix, prefix)

	rows, err := d.db.QueryContext(ctx, query, identity.schema, identity.table)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("failed to query primary key of %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan primary key column of %s: %w", table, err)
		}
		keys = append(keys, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read primary key of %s: %w", table, err)
	}
	return keys, nil
}
