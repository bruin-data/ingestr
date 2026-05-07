package sqlite

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sqlite"},
		func() interface{} { return NewSQLiteSource() },
	)
}
