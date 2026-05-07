package csv

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"csv"},
		func() interface{} { return NewCSVDestination() },
	)
}
