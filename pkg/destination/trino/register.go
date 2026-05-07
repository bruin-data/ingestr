package trino

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"trino"},
		func() interface{} { return NewTrinoDestination() },
	)
}
