package athena

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"athena"},
		func() interface{} { return NewAthenaDestination() },
	)
}
