package vertica

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"vertica"},
		func() interface{} { return NewVerticaDestination() },
	)
}
