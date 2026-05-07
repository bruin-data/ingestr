package postgres

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"postgres", "postgresql", "postgresql+psycopg2"},
		func() interface{} { return NewPostgresDestination() },
	)
}
