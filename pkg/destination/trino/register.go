package trino

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"trino"},
		func() interface{} { return NewTrinoDestination() },
	)
}
