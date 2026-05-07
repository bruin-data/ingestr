package redshift

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"redshift", "redshift+psycopg2"},
		func() interface{} { return NewRedshiftSource() },
	)
}
