package athena

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"athena"},
		func() interface{} { return NewAthenaDestination() },
	)
}
