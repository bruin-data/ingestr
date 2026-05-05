package postgres

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"postgres", "postgresql", "postgresql+psycopg2"},
		func() interface{} { return NewPostgresSource() },
	)
}
