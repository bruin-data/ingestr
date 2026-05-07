package synapse

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"synapse"},
		func() interface{} { return NewSynapseDestination() },
	)
}
