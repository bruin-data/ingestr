package sqlite

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"sqlite"},
		func() interface{} { return NewSQLiteDestination() },
	)
}
