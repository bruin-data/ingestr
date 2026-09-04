package postgres_cdc

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

func (s *PostgresCDCSource) getTableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if err := s.validateGeneratedColumns(ctx, table); err != nil {
		return nil, err
	}
	return getTableSchema(ctx, s.queryPool, table)
}

func (s *PostgresCDCSource) validateGeneratedColumns(ctx context.Context, table string) error {
	if s.serverVersion < 120000 {
		return nil
	}
	schemaName, tableName := parseTableName(table)
	rows, err := s.queryPool.Query(ctx, `
  SELECT a.attname, a.attgenerated::text
  FROM pg_attribute a
  JOIN pg_class c ON c.oid = a.attrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = $1 AND c.relname = $2
    AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated <> ''
  ORDER BY a.attnum`, schemaName, tableName)
	if err != nil {
		return fmt.Errorf("failed to inspect generated columns for %s: %w", table, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column, kind string
		if err := rows.Scan(&column, &kind); err != nil {
			return fmt.Errorf("failed to read generated column for %s: %w", table, err)
		}
		if s.serverVersion < 180000 || kind != "s" {
			return fmt.Errorf("cannot replicate generated column %q on %s: postgres+cdc requires PostgreSQL 18 or newer and a stored generated column published with publish_generated_columns = stored; materialize it as an ordinary column to use older servers or virtual expressions", column, table)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read generated columns for %s: %w", table, err)
	}
	rows.Close()
	if len(columns) > 0 {
		var mode string
		if err := s.queryPool.QueryRow(ctx, `SELECT pubgencols::text FROM pg_publication WHERE pubname = $1`, s.cdcConfig.Publication).Scan(&mode); err != nil {
			return fmt.Errorf("failed to inspect generated-column publication coverage: %w", err)
		}
		if mode != "s" {
			return fmt.Errorf("publication %q does not publish generated columns %v on %s; set publish_generated_columns = stored so the snapshot and WAL contain the same columns", s.cdcConfig.Publication, columns, table)
		}
	}
	s.generatedColumns.Store(schemaName+"."+tableName, columns)
	return nil
}

func (s *PostgresCDCSource) generatedColumnsFor(table string) []string {
	schemaName, tableName := parseTableName(table)
	value, ok := s.generatedColumns.Load(schemaName + "." + tableName)
	if !ok {
		return nil
	}
	return value.([]string)
}

func validateGeneratedRelation(rel *RelationInfo, columns []string) error {
	if rel.Stale {
		return nil
	}
	for _, column := range columns {
		if !rel.hasColumn(column) {
			return fmt.Errorf("generated column %q is missing from the WAL relation for %s.%s; ensure the publication still uses publish_generated_columns = stored and restart with --full-refresh", column, rel.Namespace, rel.Name)
		}
	}
	return nil
}
