package redshift

import (
	"context"

	intredshift "github.com/bruin-data/ingestr/internal/redshift"
	"github.com/bruin-data/ingestr/pkg/source/postgres"
)

type RedshiftSource struct {
	*postgres.PostgresSource
}

const primaryKeyQuery = `
			SELECT kcu.column_name, kcu.ordinal_position
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_catalog = kcu.constraint_catalog
				AND tc.constraint_schema = kcu.constraint_schema
				AND tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
				AND tc.table_name = kcu.table_name
			WHERE tc.constraint_type = 'PRIMARY KEY'
				AND tc.table_schema = $1
				AND tc.table_name = $2
`

func NewRedshiftSource() *RedshiftSource {
	return &RedshiftSource{PostgresSource: postgres.NewPostgresSourceWithPrimaryKeyQuery(primaryKeyQuery)}
}

func (s *RedshiftSource) Schemes() []string {
	return []string{"redshift", "redshift+psycopg2"}
}

func (s *RedshiftSource) Connect(ctx context.Context, uri string) error {
	return s.PostgresSource.Connect(ctx, intredshift.NormalizeURI(uri))
}
