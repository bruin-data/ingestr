package attio

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"attio"},
		func() interface{} { return NewAttioDestination() },
	)
}
