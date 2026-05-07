package csv

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"csv"},
		func() interface{} { return NewCSVDestination() },
	)
}
