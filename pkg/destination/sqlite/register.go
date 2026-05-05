package sqlite

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"sqlite"},
		func() interface{} { return NewSQLiteDestination() },
	)
}
