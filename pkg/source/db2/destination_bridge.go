package db2

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/schema"
)

// ExecSQL executes a SQL statement using the source's existing DRDA connection.
// It is intentionally parameterless because the current DB2 protocol client only
// supports immediate SQL execution.
func (s *Db2Source) ExecSQL(ctx context.Context, query string) error {
	if s.client == nil {
		return fmt.Errorf("Db2 source is not connected")
	}
	return s.client.Exec(ctx, query)
}

// TableSchema exposes DB2 catalog-backed schema inspection for components that
// share the existing DRDA connection implementation, such as the DB2 destination.
func (s *Db2Source) TableSchema(ctx context.Context, table string) (*schema.TableSchema, error) {
	if s.client == nil {
		return nil, fmt.Errorf("Db2 source is not connected")
	}
	return s.getSchema(ctx, table)
}
