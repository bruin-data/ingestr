package postgres

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"postgres", "postgresql", "postgresql+psycopg2"},
		func() interface{} { return NewPostgresSource() },
	)
}
