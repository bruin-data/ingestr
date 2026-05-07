package duckdb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"duckdb", "motherduck", "md"},
		func() interface{} { return NewDuckDBDestination() },
	)
}
