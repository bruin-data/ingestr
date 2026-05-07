package redshift

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"redshift", "redshift+psycopg2"},
		func() interface{} { return NewRedshiftDestination() },
	)
}
