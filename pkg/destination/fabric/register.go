package fabric

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"fabric"},
		func() interface{} { return NewFabricDestination() },
	)
}
