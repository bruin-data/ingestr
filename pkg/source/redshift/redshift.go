package redshift

import (
	"context"

	intredshift "github.com/bruin-data/gong/internal/redshift"
	"github.com/bruin-data/gong/pkg/source/postgres"
)

type RedshiftSource struct {
	*postgres.PostgresSource
}

func NewRedshiftSource() *RedshiftSource {
	return &RedshiftSource{PostgresSource: postgres.NewPostgresSource()}
}

func (s *RedshiftSource) Schemes() []string {
	return []string{"redshift", "redshift+psycopg2"}
}

func (s *RedshiftSource) Connect(ctx context.Context, uri string) error {
	return s.PostgresSource.Connect(ctx, intredshift.NormalizeURI(uri))
}
